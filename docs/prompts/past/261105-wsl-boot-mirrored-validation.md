# WSL の再起動後の接続とミラー モードの検証記録 (2026-11-05)

> [!IMPORTANT]
> 作業時点の記録です。現在の手順と検証済みの範囲は [利用ガイド](../../ja/user-guide.md) の「WSL の Linux への接続」を参照してください。推奨構成の準備は [WSL の Linux への接続の検証記録](261105-wsl-connection-validation.md) にあります。

## 背景

WSL の sshd と ProxyJump の推奨構成について、未検証だった 2 点を test-bed (チェックポイント `wsl-sshd` に続く状態) で確かめた。Windows の再起動後に接続できるかと、ミラー モードのネットワーク (`networkingMode=mirrored`) で動くかである。接続元の ssh は、Git for Windows の OpenSSH 10.6p1 (PATH で先に見つかる) と Windows の OpenSSH 9.5p1 の 2 つを使った。

## Windows の再起動後

- `shutdown /r /t 0` で再起動すると、Windows の sshd は約 17 秒で応答したが、`wsl -l -v` は `Stopped` のままで、ProxyJump の接続は `Connection closed by UNKNOWN port 65535` で失敗した。`.wslconfig` の自動停止の設定は、止まった WSL を起動するものではない。
- 手動で `wsl.exe -d ubuntu --exec true` を実行すると約 11 秒で戻り、その直後から 2222 番に接続できた。転送の準備を待つ必要はなかった。
- `ProxyCommand` で `ssh ... "wsl.exe -d ubuntu --exec true" && ssh -W %h:%p ...` とつなぐ方法は失敗した。Git for Windows の ssh は `exec ssh ... && ssh ...` として実行するため、最初の ssh の出力が SSH の通信として扱われ、`kex_exchange_identification: Connection closed by remote host` になった。Windows の OpenSSH では、`-F` に渡した長いパスが途中 (`.../scra`) で切れて `Can't open user config file` になった。原因は追っていない。
- `Match host test-bed-wsl exec "ssh -F <設定ファイル> -n test-bed-win wsl.exe -d ubuntu --exec true"` を `Host test-bed-wsl` の前に置くと、両方の ssh で、WSL が止まった状態から約 12 秒で接続できた。WSL が動いている場合の 2 回目の接続は約 1 秒だった。`Match` のあとに設定項目がなくても受け付けられた。
- 再起動直後に、この設定で `detect.sh` (`linux-amd64`、約 14 秒)、`deploy.sh`、`probe.sh` が成功した。
- `wsl --shutdown` の後に bridge で接続すると、`/tmp/host-shell-1000/host-shell` がなく `initialize` が応答しなかった。WSL の起動で `/tmp` が空になるためで、`deploy.sh` を実行し直すと、`exec_run`、pty ジョブ、約 13 MiB の `transfer_push`・`transfer_pull` (SHA-256 一致)、`svc_list`、リモート転送が成功した。

## ミラー モード

`C:\Users\tester\.wslconfig` の `[wsl2]` に `networkingMode=mirrored` を加え、`wsl --shutdown` で切り替えた。`wslinfo --networking-mode` は `mirrored`、WSL の `eth0` は Windows と同じ `192.168.250.10/24` になり、sshd は `0.0.0.0:2222` と `[::]:2222` で待ち受けた。

- `HostName localhost` の ProxyJump は失敗した。Windows の中から接続を試すと、`127.0.0.1:2222` はバナー (`SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.20`) を返したが、`::1:2222` は接続直後に読めず、`192.168.250.10:2222` はタイムアウトした。`localhost` が `::1` に解決されるためと判断した。
- `HostName 127.0.0.1` (known_hosts は `[127.0.0.1]:2222`) にすると、`detect.sh`、`deploy.sh`、`probe.sh` と bridge 経由の上記の操作が成功した (Git for Windows の ssh)。Windows の OpenSSH でもコマンドを実行できた。
- ホストから `192.168.250.10:2222` への直接の接続は失敗した。WSL の Hyper-V ファイアウォールの設定は `DefaultInboundAction` が `Block` で、受信の許可は ICMP と mDNS だけだった。`New-NetFirewallHyperVRule` で TCP 2222 番を `192.168.250.0/24` から許可すると、ホストから直接 ssh で接続できた。この規則は確認後に削除した。

## NAT モードに戻した後

`.wslconfig` を元に戻して `wsl --shutdown` すると、`wslinfo --networking-mode` は `nat` になり、Windows 側で `127.0.0.1:2222` と `[::1]:2222` が待ち受けた。`HostName 127.0.0.1` の設定で、スクリプト、bridge 経由の操作 (両方の ssh)、Windows の OpenSSH でのコマンド実行が成功した。ホストから 2222 番への直接の接続はできなかった。

## 気づいたこと

- Git for Windows の ssh に `-o UserKnownHostsFile=NUL` を渡すと、作業ディレクトリーに `NUL` という名前のファイルが作られた。リポジトリーに作られたため削除した。

## 終了時の状態

Hyper-V の規則は削除し、`.wslconfig` は NAT モードの内容に戻した。WSL の `/tmp` には検証で配置した host-shell が残る。
