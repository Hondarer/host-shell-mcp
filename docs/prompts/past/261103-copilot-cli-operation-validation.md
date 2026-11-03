# Copilot CLI の操作段階の検証記録 (2026-11-03)

> [!IMPORTANT]
> 作業時点の記録です。現在の手順と検証済みの範囲は [CLI 型 AI コーディングエージェントの導入](../../ja/coding-agents.md) を参照してください。

[CLI 型 AI コーディングエージェントの検証記録](261102-coding-agents-validation.md) で未検証だった「操作」段階 (検証用ファイルの編集と小さなコマンドの実行) を確認しました。

## 環境

[画面内 Copilot Chat と remote-dns の検証記録](261103-copilot-chat-remote-dns-validation.md) と同じ環境を続けて使用しました。

- 接続先: Oracle Linux 10.2 / x86_64 / glibc 2.39 の Podman コンテナー `hs-cs-ol10` (`--network none`)、利用者 `dev`
- 接続: bridge から `podman exec -i` で接続先 host-shell を起動 (SSH は使用していない)
- code-server 4.144.0 (remote-dns の専用コピーモード)、Chrome for Testing 148 から CDP で操作
- Copilot CLI 1.0.83: 公式リリースから `copilot-linux-x64.tar.gz` を手元で取得し、SHA-256 `ffbe1c429664b8a05efed67ecdb467123e40fcaa3c6c14ef9a98ba74da4687b7` を照合。`transfer_push` で `/home/dev/.local/share/host-shell-coding-agents/copilot/1.0.83/` へ転送し、接続先でも照合して展開した。`--version` は成功

## 起動とサインイン

- code-server のターミナルで `~/work/cli-op` を作成し、絶対パスで CLI を起動した。フォルダーの信頼は `1. Yes` (このセッションのみ) を選んだ。
- 起動時に `Error auto updating: TypeError: fetch failed` が表示された。プロキシ履歴では、起動直後に `api.github.com`・`github.com` への接続に続いて `release-assets.githubusercontent.com:443` が拒否されていた。
- 画面内 Copilot Chat のサインインとは別に、CLI のサインインが必要だった。`/login` → `GitHub.com` → `Sign in with a device code` を選び、ユーザーが表示コードを GitHub に入力して承認した。
- 承認後に `System vault not available` と表示され、トークンを平文の設定ファイルへ保存するかを問われた。ユーザーの判断で `1. Yes` を選び、今後も原則としてこれを選ぶ方針とした。トークンは `~/.copilot/config.json` (パーミッション 600、所有者 `dev`) に保存された (内容は表示していない)。
- `Signed in successfully as Hondarer!` と `GitHub MCP Server: Connected` が表示された。

## 操作

依頼した内容: `notes.txt` を `alpha` の 1 行で作成し、`beta` に書き換え、`wc -c notes.txt` の出力を報告する。

| 手順 | 結果 |
|---|---|
| ファイル作成 | `Create file` の差分と `Do you want to create ...?` が表示され、`1. Yes` で承認後に作成された |
| ファイル編集 | `Update file` の差分 (`alpha` → `beta`) が表示され、`1. Yes` で承認後に更新された |
| コマンド実行 | `wc -c notes.txt` は承認の確認なしで実行され、`5 notes.txt` と報告された |
| 接続先での確認 | `exec_run` で `notes.txt` の内容が `beta\n` (5 バイト)、所有者 `dev` であることを確認 |

推論の通信は `api.individual.githubcopilot.com:443` などの許可済み宛先を経由した。プロキシ履歴の拒否は `mobile.events.data.microsoft.com`、`westus-0.in.applicationinsights.azure.com`、`169.254.169.254:80` などのテレメトリー・メタデータ宛先だけで、許可先は追加していない。
