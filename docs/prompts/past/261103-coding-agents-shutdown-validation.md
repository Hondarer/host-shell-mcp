# CLI 型 AI コーディングエージェントの終了段階の検証記録 (2026-11-03)

> [!IMPORTANT]
> 作業時点の記録です。現在の手順と検証済みの範囲は [CLI 型 AI コーディングエージェントの導入](../../ja/coding-agents.md) を参照してください。

MCP 切断後に code-server・CLI・補助プロセス・待ち受けがどうなるかを確認しました。

## 環境と手順

[Copilot CLI のコピー範囲の検証記録](261103-copilot-cli-copy-scope-validation.md) までと同じ環境を続けて使用しました。code-server のターミナルでは Copilot CLI 1.0.83 が入力待ちで、別のターミナルでは bash がプロンプト待ちでした。

1. 切断前の状態を、bridge とは別の経路 (`podman exec --user root`) で記録した。
2. MCP クライアント (検証用ドライバー `mcpd.py`) だけを SIGTERM で終了した。bridge は標準入力の終了で切断を検知する。
3. 手元のプロセスと待ち受けを PID とポートで確認し、接続先の状態を同じ別経路で記録した。

## 切断前

| 区分 | PID / PGID / SID | 内容 |
|---|---|---|
| 接続先 host-shell | 177 / 177 / 177 | `--mcp --remote-dns --client-proxy --allow-forward-local` |
| code-server 一式 | PGID 485、SID 177 | 親 (485)、`entry` (557)、`fileWatcher`、エージェントホスト、拡張ホスト、JSON 言語サーバー、`copilot-runtime` (18745)、ptyHost (20334) |
| ターミナル 1 | bash 20345 (SID 20345)、Copilot CLI 20442 (PGID 20442、SID 20345) | host-shell とは別セッション |
| ターミナル 2 | bash 23866 (SID 23866) | `seq` 実行後のプロンプト待ち |
| 待ち受け | 接続先 `127.0.0.1:8000` (code-server)、`127.0.0.1:44385` (プロキシ)。手元 `127.0.0.1:40589` (転送) | |
| 一時ディレクトリー | `/tmp/host-shell-dns-2313569773/` (ソケットとアダプター)、`/tmp/host-shell-proxy-80808028/` | |

## 切断後

| 区分 | 結果 |
|---|---|
| 手元 | MCP クライアント、bridge、`podman exec` が終了し、40589 が閉じた |
| 接続先 host-shell | 終了 |
| code-server 一式、`copilot-runtime` | すべて終了 |
| ターミナルの bash 2 つと Copilot CLI | すべて終了 |
| 接続先の待ち受け | なし |
| 一時ディレクトリー | DNS とプロキシの一時ディレクトリーが削除された |
| 残ったもの | `/tmp/host-shell-1000/` の host-shell とアダプター、Copilot CLI の配置物、`~/.copilot/config.json`、`~/work/cli-op/notes.txt` |

終了したプロセスは `<defunct>` として一覧に残った。コンテナーの PID 1 が `sleep infinity` で子プロセスを回収しないためであり、切断前から残っていた 3 件 (293、350、407) も同じ状態だった。

ターミナルのシェルと Copilot CLI は host-shell とは別のセッションにあったが終了した。ptyHost の終了で端末が閉じ、ハングアップが伝わったためと考えられる (仕組みの詳細は確認していない)。`nohup` や `setsid` で切り離したプロセスの扱いは確認していない。
