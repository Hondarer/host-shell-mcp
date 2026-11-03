# code-server の表示言語のオフライン化の検証記録 (2026-11-03)

> [!IMPORTANT]
> 作業時点の記録です。現在の仕様と検証済みの範囲は [code-server の導入の表示言語](../../ja/code-server.md#表示言語) を参照してください。

## 課題

日本語 Language Pack を既定の拡張機能として注入しても、初回利用時に日本語へ切り替えるにはネットワーク接続が必要だった。注入時に必要なファイルを配置し、完全オフラインで初回起動から日本語を使えるようにする。

## 環境

- 接続先: Oracle Linux 10.2 / x86_64 / glibc 2.39 の Podman コンテナー `hs-cs-ol10` (`--network none`)、利用者 `dev`
- code-server 4.144.0 (VS Code 1.144.0)、既定の拡張機能は `MS-CEINTL.vscode-language-pack-ja@1.131.0`、`ms-vscode.cpptools-themes@2.0.0`、`ms-vscode.hexeditor@1.11.1`
- ブラウザー: WSLg 上の Chrome for Testing 148 (言語設定 `ja`)。CDP で全要求を監視し、`127.0.0.1`・`localhost` 以外への要求を遮断して記録した
- 原因調査では、接続先の外部通信を観察するため、実在しない宛先 1 件だけを許可したクライアント経由プロキシを使用した。最終確認ではプロキシなし (`--client-proxy` なし) で接続した

## 原因調査

ユーザー データと拡張機能を削除して `install.sh` を再実行し、初回の注入直後の状態を作った。

- code-server は英語で起動した。サーバーの表示言語は `--locale`、`User/argv.json` の `locale`、クッキー、Accept-Language の順で決まるが、`argv.json` が存在しないと `en` が返り、以降は参照されない。
- 「Configure Display Language」の一覧は「English (Current), Installed」だけで、日本語は表示されなかった。一覧の作成時にブラウザーとサーバーの両方が `https://open-vsx.org/vscode/gallery/extensionquery` へ接続しようとし、遮断された。
- ユーザー データの `languagepacks.json` は `{}` だった。サーバーの言語パック一覧 (`LanguagePacksCache`) は、稼働中のサーバーが拡張機能をインストールしたときにだけ更新され、CLI の `--install-extension` やサーバー起動時には更新されない。翻訳ファイル (`clp/<hash>.<locale>/`) は、この一覧に登録された言語だけから生成される。
- オンラインでは、マーケットプレースの一覧から日本語を選ぶとサーバー上で Language Pack が再インストールされ、一覧が更新されて `argv.json` に `locale` が書かれる。初回利用時にネットワークが必要だったのはこのためと判断した。

## 対処

- `scripts/lib/language-packs.mjs` を追加した。`extensions.json` に記録された導入済み拡張機能のうち `contributes.localizations` を持つものを、サーバーと同じ形式・ハッシュ (拡張機能 ID と版の MD5) で `languagepacks.json` に書き出す。code-server 同梱の node で実行し、拡張機能ディレクトリ外を指す項目は無視する。
- `install.sh` と `install.ps1` は、実行のたびに上記を実行する。
- `defaults/User/argv.json` (`"locale": "ja"`) を追加し、`settings.json` と同じく配置先にファイルがない場合だけ配置する。
- Windows の接続先にも `scripts/lib` を転送するよう、スキルの転送表を変更した。

手作業で `languagepacks.json` と `argv.json` を用意して再起動した時点で、ページのタイトルが「ようこそ」になり、翻訳ファイルはすべて `127.0.0.1` から取得された。

## 最終確認 (完全オフライン)

クライアント経由プロキシなしで接続し直し、ユーザー データと拡張機能を削除してから、更新したキットで `install.sh` を実行した。出力に `installed default User/argv.json` と `registered language packs: ja` が表示された。code-server を起動し、未使用の転送ポート (新しいオリジン) で開いた。

| 確認 | 結果 |
|---|---|
| 初回表示 | タイトル「ようこそ - work - code-server」。エクスプローラー、ようこそ、チャットなどが日本語 |
| 組み込み拡張機能 | コマンド パレットで「Git: クローン」「表示言語を構成する」などが日本語 |
| 英語への切り替え | 「表示言語を構成する」で English を選ぶと「code-server を再起動して、English に切り替えますか?」が表示され、再起動後に英語。`argv.json` は `"locale": "en"` になり、コメントは保持された |
| 日本語への切り替え | English の状態から日本語を選び、再起動後に日本語。`argv.json` は `"locale": "ja"` |
| 遮断された要求 | `open-vsx.org/vscode/gallery/extensionquery` だけ (25 件)。接続先のログも同じ要求の `ENOTFOUND` だけで、言語関連のエラーはなかった |

一覧では、日本語表示中も「English (Current)」と表示された。日本語表示中に日本語を選んでも何も起きなかった。

## その他

- `tests/language-packs.test.mjs` を追加した (3 件)。既存の `remote-dns.test.mjs` と合わせて 10 件が成功した。
- `install.ps1` は Windows PowerShell 5.1 で構文検査だけを行った。Windows 接続先での実行は行っていない。
- ログイン画面は `--locale` だけに従うため英語のまま。`--locale` を起動オプションに加えると `argv.json` より優先され、画面からの切り替えができなくなるため採用しなかった。

## 「(Current)」表示の調査

「Configure Display Language」の一覧で、日本語表示中も「English (Current)」と表示される原因を調べた。

- ブラウザー側の `_VSCODE_NLS_LANGUAGE` は `ja` だった。
- 一覧の項目はサーバーの `getInstalledLanguages` が作り、「(Current)」は `createQuickPickItem` がサーバー プロセス自身の言語と比べて付けていた。サーバー プロセスの言語は、起動時に code-server が `VSCODE_NLS_CONFIG` を設定するため常に英語である。
- 設定や環境変数では変えられず、修正には配布物 (`server-main.js`) の変更が必要になる。表示だけの問題で選択は正しく動作するため、対処せず制限として文書に記載した。

## 既定を英語にする設計変更

ユーザーの指示により、リポジトリのグローバル対応を考慮して既定の表示言語を英語に戻し、指示があった場合だけ日本語にする方式に変更した。

- `defaults/User/argv.json` を削除し、`install.sh --locale <言語>` / `install.ps1 -Locale <言語>` を追加した。`language-packs.mjs --locale` が、Language Pack の有無を確認したうえで `User/argv.json` の `locale` だけを置き換える (なければ追加・作成する) 。`en` 以外で Language Pack がない場合は停止する。
- 言語パック一覧の登録は、指定の有無にかかわらず毎回行う。これにより、英語で導入した後でもオフラインで日本語へ切り替えられる。
- スキルでは、言語の指定がない場合に一度問い合わせ、明示がなければ英語で導入する手順とした。
- 自動テストは `--locale` 関連を加えて 8 件になり、`remote-dns.test.mjs` と合わせて 15 件が成功した。

完全オフライン (プロキシなし、ブラウザーの外部通信遮断) で、ユーザー データと拡張機能を削除して確認した。

| 確認 | 結果 |
|---|---|
| `install.sh` (言語指定なし) | `User/argv.json` は作られず、`registered language packs: ja`。起動すると英語 (Welcome) |
| 英語からの切り替え | 「Configure Display Language」の一覧に「日本語, (ja)」が表示され、選択・再起動で日本語になった。`User/argv.json` が `"locale": "ja"` で作成された |
| `install.sh --locale ja` | `display language: ja` と表示され、初回の起動から日本語 (「ようこそ」「Git: クローン」) |
| `install.sh --locale fr` | `no language pack for fr is installed; ...` で停止し、`locale` は `ja` のまま |
| 遮断された要求 | `open-vsx.org/vscode/gallery/extensionquery` だけ |
