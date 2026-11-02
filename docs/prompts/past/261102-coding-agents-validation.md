# CLI 型 AI コーディングエージェントの検証記録 (2026-11-02〜2026-11-03)

> [!IMPORTANT]
> 作業時点の記録です。`docs/ja/coding-agents.md` と `docs/ja/code-server.md` から経緯の記述をここへ移しました。現在の手順と検証済みの範囲は [CLI 型 AI コーディングエージェントの導入](../../ja/coding-agents.md) を参照してください。

## 手元での起動確認 (2026-11-02)

手元の Linux x86_64 に配置済みの以下の実体で、単体実行とローカル host-shell の PTY 付き `job_start` の両方で `--version` が終了コード 0 になることを確認しました。配布元の照合、接続先への注入、認証・推論の成功を意味しません。

| CLI | 手元のバージョン | 単体 / host-shell PTY 起動 |
| --- | --- | --- |
| Copilot | 1.0.83 | 成功 / 成功 |
| agy | 1.4.12 | 成功 / 成功 |
| Claude Code | 2.1.316 | 成功 / 成功 |
| Codex | 0.166.0 | 成功 / 成功 |
| Grok | 1.0.70 | 成功 / 成功 |

Codex は読み取り専用ファイルシステムで PATH 用エイリアスを作成できない警告を出しましたが、バージョン表示は成功しました。`internal/clientproxy`、`internal/jobs`、`internal/bridge` の既存テストも成功しています。

## Copilot CLI の隔離コンテナーでの確認 (2026-11-02)

Oracle Linux 10.2 / x86_64 / glibc 2.39 の Podman コンテナーへ code-server 4.144.0 と Copilot CLI 1.0.83 を配置しました。host-shell は `fa2c855` のソースからビルドしました。コンテナーは `--network none` で起動し、ネットワーク機器はループバックのみ、外部へのルートはありません。SSH の `ProxyCommand` が `podman exec -i` による標準入出力中継を使用し、コンテナー内のループバックで待ち受ける sshd に接続しました。

- code-server と Copilot の公式配布アーカイブの SHA-256 を照合し、SSH 上の `transfer_push` で配置・再照合しました。Copilot の配布元は [github/copilot-cli の v1.0.83](https://github.com/github/copilot-cli/releases/tag/v1.0.83)、アーカイブは `copilot-linux-x64.tar.gz`、SHA-256 は `ffbe1c429664b8a05efed67ecdb467123e40fcaa3c6c14ef9a98ba74da4687b7` です。
- 接続先で Copilot の `--version`、`--help` と PTY 付きのバージョン表示が成功しました。
- ユーザーは Copilot CLI に表示されたコードを別のブラウザーに入力するデバイスコード方式で認証し、ログインに成功しました。
- コンテナーから外部 IP への直接 TCP 接続は失敗し、クライアント経由プロキシで `https://api.github.com` への HTTP 200 を確認しました。その後、ユーザーのスクリーンショットで Copilot の対話応答とツール実行を確認しました。
- 許可先は `github.com:443`、`api.github.com:443`、`*.githubcopilot.com:443`、`copilot-proxy.githubusercontent.com:443` でした。
- ターミナルの描画・入力・Ctrl-C・リサイズは、ユーザーが全項目良好と確認しました。

許可対象外ホストへの HTTP 403 (HTTPS では `CONNECT tunnel failed, response 403`) を確認しました。追加配布物の取得先 `release-assets.githubusercontent.com:443` も許可対象外として記録されていました。ユーザーのスクリーンショットでは、Copilot 内蔵の Web Fetch が `WebFetchBlockedUrlError: failed to lookup address information: Name or service not known` で失敗し、続けて実行した `curl` はプロキシから 403 を受けていました。この後、remote-dns の開発に進みました ([remote-dns の検証記録](261102-remote-dns-validation.md))。

## Copilot CLI のコピーの調査 (2026-11-02)

Microsoft Edge から code-server 4.144.0 を利用し、通常のターミナルではコピーできる一方、Copilot CLI 1.0.83 の対話画面内でだけコピーできませんでした。`Ctrl+C` で `Copied` と表示されても Windows のクリップボードは更新されず、エディターからターミナルへのペーストは動作していました。

- `Shift` を押しながらドラッグして `Ctrl+Shift+C` を押す方法は、マウス有効時はコピーできませんでした (以前の記述でできるとしていたのは誤りでした)。
- ユーザーは、コマンド パレットの `Terminal: Select All` と `Terminal: Copy Selection` で、マウス操作を有効にしたまま正しくコピーできることを確認しました。
- `copilot --continue --no-mouse` で再開するとコピーできましたが、ユーザーからホイールスクロールが効かないなどの問題が報告されました。
- `/share` で会話をファイルへ保存し、エディターで開いてコピーする方法も利用できることを確認しました。

Copilot 内のコピー経路を避けて改善したと考えられますが、内部のどの処理で失敗したかは確定していません。

## Edge の青いドット (2026-11-02)

code-server のターミナル入力欄に青いドットやペンが表示される件は、ユーザーの確認で、Edge の「文書作成支援」内の「文章作成のヒントを表示する」をオフにする設定が該当しました。

## Copilot Chat の BYOK カタログ (2026-11-03)

BYOK モデル一覧の取得先 `https://main.vscode-cdn.net/extensions/copilotChat.json` は、未許可時は CONNECT 403、`main.vscode-cdn.net:443` の許可後は形式バージョン1・5プロバイダー分の JSON（19,535バイト）を取得できました。なお、この確認は接続先からの直接取得であり、Copilot 拡張自身が再試行して警告が解消したことまでは含めていません。
