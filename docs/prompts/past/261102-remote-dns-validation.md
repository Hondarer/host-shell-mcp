# remote-dns の検証記録 (2026-11-02〜2026-11-03)

> [!IMPORTANT]
> 作業時点の記録です。`docs/ja/remote-dns.md` から経緯の記述をここへ移しました。現在の仕様と検証済みの範囲は [remote-dns](../../ja/remote-dns.md) を参照してください。

## 名前解決フック単体の診断 (2026-11-02)

Oracle Linux 10.2 / x86_64 / glibc 2.39 のネットワーク隔離コンテナーで、code-server 4.144.0 と Copilot CLI 1.0.83 を使い、名前解決フック単体について次を確認しました。この結果は、製品構成でのブラウザー・CLI の再検証を示すものではありません。

| 確認項目 | 結果 |
|---|---|
| 単体 Copilot CLI の Web Fetch → `https://api.github.com/zen` | フックなしでは名前解決失敗、フックありでは成功。同じ許可済み URL と Web Fetch のみを使用 |
| code-server のブラウザーターミナル | ユーザーが追加の環境変数指定なしで、24件の並列問い合わせ・子プロセス・エラー処理を含む検証の成功を確認 |
| Node.js `dns.lookup()` | 成功 |
| Node.js `dns.resolve4()` | ECONNREFUSED。独自 DNS 経路はフック対象外 |
| VS Code の拡張ホスト・エージェントホスト・同梱 Copilot SDK | `LD_PRELOAD` が除去され、共有ライブラリーが読み込まれていないことを確認 |

VS Code 同梱 SDK の実行体は `lib/vscode/node_modules/@github/copilot-sdk-linux-x64/prebuilds/linux-x64/copilot-runtime` でした。`out/server-main.js`、`out/vs/workbench/api/node/extensionHostProcess.js`、`out/vs/platform/agentHost/node/agentHostMain.js` に `LD_PRELOAD` を除去する処理があり、親プロセスへの設定だけでは、この起動経路をカバーできません。

同日 13:11:49 (JST) の `https://hondarer-soft.com` 取得失敗は、プロキシ履歴では `hondarer-soft.com:443` が `destination not allowed` でした。フック付き Python での名前解決は成功しています。`a transport-level failure (DNS, connection, or TLS handshake)` という表示だけで DNS 障害と判断しないこととし、この調査で宛先の許可は追加していません。

## 製品組み込み版の検証 (2026-11-02)

開発版を Oracle Linux 10.2 / x86_64 / glibc 2.39 の `--network none` コンテナーに SSH で接続して検証しました。

| 項目 | 結果 |
|---|---|
| 接続先で同梱ソースを自動コンパイル | 成功（接続先 GCC の最適化時警告も修正） |
| `exec_run` の名前解決・canonical name・サービスのポート番号 | 成功 |
| 24件の並列問い合わせ・子プロセスへの継承 | 成功 |
| `job_start` で起動したプロセスからの名前解決 | 成功 |
| 許可済み `api.github.com:443` への HTTPS | 成功 |
| 未許可の `example.com:443` | 名前解決は成功し、プロキシは 403 で拒否 |
| セッション切断 | DNS 一時ディレクトリーと実行中の検証ジョブを回収 |
| 既存の2つの code-server | 引き続き HTTP 200、セッションを維持 |

製品組み込み版でのブラウザーからの再検証や内蔵 Copilot SDK への適用は、この時点の結果に含めていません。

## code-server 専用コピーモードの検証 (2026-11-02〜2026-11-03)

自動テストでは、元ファイルの保持、変更済みファイルの拒否、構文エラー時の後片付け、無効時の既存動作、2セッションの分離、危険な権限・リンク・期限切れソケットの拒否を確認しました。実際の共有ライブラリーを使い、環境変数の除去後に再注入した Node 子プロセスのライブラリー読み込みと、MCP チャネルを通じた名前解決も確認しています。

隔離コンテナーでは専用コピーの作成・再検証・HTTP サーバー起動まで確認しました。ユーザーがテスト用 code-server のターミナルから起動した Copilot CLI で Web Fetch の成功を確認しました。プロキシ履歴にも `api.github.com:443` の許可を確認しています。実機の拡張ホストと VS Code 同梱 `copilot-runtime` では `LD_PRELOAD` の継承と共有ライブラリーの読み込みを確認しました。

ユーザーは画面内の Copilot Chat のログイン成功を確認しました。取得も依頼しましたが、Web Fetch ツールがなく、チャットは curl を使用したと報告しています。内蔵 Web Fetch は未実施です。HTTP プロキシ経由の curl は接続元側で宛先を名前解決できるため、curl の実行だけでは SDK 自身の DNS フック動作を検証したことにはなりません。

その後、ユーザーは Copilot Chat のターミナル実行ツールから、専用コピーの Node.js で `dns.lookup("api.github.com", {all: true}, ...)` を実行し、IPv4 `20.27.177.116` を取得しました。HTTP プロキシを使用しない `getaddrinfo` 呼び出しの成功であり、Chat が起動した Node 子プロセスでの名前解決を確認できました。拡張ホスト自身または SDK 自身が直接 DNS API を呼び出した結果とは区別します。

## 継続利用時のログ確認 (2026-11-03)

ユーザーがブラウザーで code-server を開き、CLI と画面内 Copilot Chat の両方が正しく応答することを確認しました。05:42 JST 前後の保持されているプロキシ履歴では、`api.individual.githubcopilot.com:443` と `api.github.com:443` への接続が許可されています。実行中のエージェントホスト・拡張ホスト・同梱 `copilot-runtime` は、いずれも `LD_PRELOAD` と DNS アダプターの読み込みを確認できました。code-server の保持出力には `api.github.com` のフック通過が2件ありましたが、この出力には問い合わせ時刻がないため、各応答との対応は断定していません。

起動時には Copilot の未サインイン・トークンソース未準備、BYOK モデル一覧更新の `fetch failed`、custom agents 取得の `Canceled` が記録されていました。調査したログでは DNS 再注入の検査エラーや `ENOTFOUND`・`EAI_AGAIN` は見つかっていません。`mobile.events.data.microsoft.com`、`default.exp-tas.com`、Application Insights の宛先、`169.254.169.254:80` は宛先ルールで拒否されており、許可先は追加していません。プロキシ履歴は直近100件までのため、全期間の無障害を示す結果ではありません。

このブラウザー接続は従来の SSH セッションを使用していました。接続元ループバックの新しい TCP 入口を使う別 SSH セッションでも MCP 初期化・DNS 中継・HTTPS を確認しましたが、このブラウザーインスタンスをその新セッションへ切り替えた検証ではありません。

### BYOK カタログの許可追加

上記のモデル一覧更新について、取得先 `https://main.vscode-cdn.net/extensions/copilotChat.json` を接続先から直接確認すると、未許可時は CONNECT 403 でした。ユーザーの指示で `main.vscode-cdn.net:443` をブラウザー用・追加 TCP SSH 用の両プロキシセッションへ `proxy_rules_update` で追加し、カタログ JSON（形式バージョン1、5プロバイダー分、19,535バイト）の取得成功を確認しました。既存の許可・拒否ルールは保持しています。Copilot 拡張自身が再試行して警告が解消したことまでは、この確認に含めていません。

## ビルド済みアダプターの配布 (2026-11-03)

- CI (run 41522772072) で、AlmaLinux 8 (glibc 2.28) 上の amd64・arm64 ビルドが成功しました。必要な glibc シンボルの最高バージョンは amd64 が 2.2.5、arm64 が 2.17 です。両アーキテクチャーとも、glibc 2.28 (ビルド用コンテナー) とランナーの glibc で `TestHookThroughChannelAndCleanup` が成功しました。
- スキル host-shell-connect の `deploy.sh` を Podman コンテナーで確認しました。Oracle Linux 10 (glibc 2.39) と Oracle Linux 8 (glibc 2.28) では配置と読み込み確認が成功し、2回目は `up to date` でした。Alpine (musl) は glibc のローダーがないため配置しません。musl のローダーは glibc 用アダプターをエラーなく読み込むため、`LD_PRELOAD` の確認だけでは判別できないことが分かり、ローダーの有無の確認を加えました。壊れたアダプターは読み込み確認で検出し、削除しました。
- bridge から `podman exec` で Oracle Linux 8 のコンテナーへ接続し、`--remote-dns-library` に配置したアダプターを指定しました。`sys_get_info` は `remote_dns: true` を返し、`exec_run` のプロセスはアダプターを読み込んで名前解決に成功しました。
