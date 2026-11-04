# WSL の Linux への接続の検証記録 (2026-11-05)

> [!IMPORTANT]
> 作業時点の記録です。現在の手順と検証済みの範囲は [利用ガイド](../../ja/user-guide.md) の「WSL の Linux への接続」を参照してください。WSL の導入は [WSL での BYOK の検証記録](261105-wsl-byok-validation.md) にあります。

## 背景

WSL での BYOK の検証では、Windows の sshd から `wsl -d ubuntu -- host-shell --mcp` で接続した。この経路ではスキルの `remote.sh` などが使えない。WSL の中で sshd を動かせば通常の Linux の接続先になるため、この推奨構成を試し、例外的な経路はノウハウとして文書に残す方針にした。

## `wsl.exe` を挟む経路の確認

Windows の sshd (既定シェル `cmd`) から実行した結果。

| コマンド | 結果 |
|---|---|
| `wsl -d ubuntu -- sh -c 'echo a b \| tr a x'` | cmd が `\|` を解釈し、`'tr' は、内部コマンドまたは外部コマンド...として認識されていません` |
| `wsl -d ubuntu -- sh -c 'echo hi > /tmp/q.txt && cat /tmp/q.txt'` | cmd が `>` を解釈し、`指定されたパスが見つかりません` |
| `wsl -d ubuntu -- sh -c "echo a b \| tr a x"` | `x b` |
| `wsl -d ubuntu -- sh -c "echo <base64> \| base64 -d \| sh"` | 成功 |

`remote.sh` はコマンドを POSIX のシングル クォートで囲むため、この経路では使えない。

## 推奨構成の準備

test-bed (チェックポイント `wsl-ubuntu` に続く状態) で行った。

1. `openssh-server` の導入: WSL の中の host-shell を root (`wsl -d ubuntu -u root -- host-shell --mcp --client-proxy`) で起動し、bridge で `archive.ubuntu.com:80` と `security.ubuntu.com:80` だけを許可して、`apt-get update` と `apt-get install -y --no-install-recommends openssh-server` を実行した。40.3 MB の取得に約 1 分かかり、openssh-server 1:9.6p1-3ubuntu13.20 が入った。プロキシの記録は許可した 2 つの宛先だけだった。
2. `/etc/ssh/sshd_config.d/10-host-shell-wsl.conf` に `Port 2222`、`PasswordAuthentication no`、`KbdInteractiveAuthentication no` を書き、`/etc/wsl.conf` に `[boot] systemd=true` を加え、`/home/ubuntu/.ssh/authorized_keys` に公開鍵を置いた。
3. `wsl --terminate` の後、PID 1 が systemd になった。Ubuntu 24.04 の sshd はソケット起動で、`ssh.socket` が `Port` の値から 2222 番で待ち受けた (`ssh.service` を直接有効にする必要はなかった)。
4. 接続元に、作業用の ssh 設定ファイル (`Host test-bed-win` と `Host test-bed-wsl`、後者は `HostName localhost`、`Port 2222`、`ProxyJump test-bed-win`) を作り、WSL の sshd のホスト鍵を host-shell で読み出して `[localhost]:2222` として known_hosts に書いた。普段使いの `~/.ssh/config` は変更していない。

## 見つかったこと

- `-F` に `\` 区切りのパスを渡すと、ProxyJump が中継用に起動する ssh へのパスから `\` が失われ、`Can't open user config file C:UserstetsuoAppData...` で失敗した。`/` 区切りにすると通った。
- 最初の接続は、中継先の認証は通ったが、`localhost:2222` への転送が `kex_exchange_identification: Connection closed by remote host` で切れた。WSL のディストリビューションが、最後のセッションの終了後に止まっていた (`wsl -l -v` が `Stopped`) ためだった。
- `C:\Users\tester\.wslconfig` に `[general] instanceIdleTimeout=-1` と `[wsl2] vmIdleTimeout=-1` を書き、`wsl --shutdown` の後に一度起動すると、セッションを閉じて 30 秒以上たっても接続できた。

## スクリプトと MCP の確認

`HOST_SHELL_SSH_OPTS="-F <設定ファイル>"`、`ROUTE=test-bed-wsl` で、スクリプトには手を加えずに実行した。

| 確認 | 結果 |
|---|---|
| `remote.sh ROUTE true` | 出力なし |
| `detect.sh` | `linux-amd64` |
| `deploy.sh` | `/tmp/host-shell-1000/host-shell` に配置 (約 4.4 秒)、2 回目は `up to date`。DNS アダプターは `bin/` にないため `dns adapter unavailable` |
| `probe.sh` | OK |
| bridge 経由の `sys_get_info` | linux / amd64、利用者 `ubuntu` |
| `exec_run` | PID 1 が systemd、日本語の出力 |
| pty ジョブ | `read -p` への入力と応答 |
| `transfer_push`・`transfer_pull` (13,500,416 バイト) | 1.5 秒と 3.6 秒、SHA-256 一致 |
| `svc_list` | `ssh.service` が running |
| リモート転送 | 接続先の `curl` が手元のサーバーの応答を受信 |

## 未確認の項目

- ミラー モードのネットワークで WSL の sshd に直接接続する構成 (Windows ファイアウォールと Hyper-V ファイアウォールの規則が必要になる見込み)
- Windows の再起動後に WSL を自動で起動する方法 (タスク スケジューラーなど)
