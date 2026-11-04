# Windows 接続先の検証記録 (2026-11-04)

> [!IMPORTANT]
> 作業時点の記録です。現在の仕様と検証済みの範囲は [利用ガイド](../../ja/user-guide.md)、[クライアント経由 HTTP プロキシ](../../ja/client-proxy.md)、[code-server の導入](../../ja/code-server.md)、[接続元を使う名前解決](../../ja/remote-dns.md) を参照してください。Windows 向け remote-dns の設計は [Windows 向け remote-dns の設計](261104-windows-remote-dns-design.md) にあります。

## 目的

Windows の接続先について、文書で対応をうたっているのに実機の記録がない項目と、未検証と書かれていた項目を、閉じたネットワークの仮想マシンで順に確認した。

## 環境

| 項目 | 内容 |
|---|---|
| ホスト (接続元) | Windows 11 Pro for Workstations ビルド 26300、Hyper-V。`bridge` はこのホストで実行 |
| 接続先 | Hyper-V 仮想マシン test-bed。Windows 11 Enterprise ビルド 26300、amd64、Windows PowerShell 5.1.26100、OpenSSH_for_Windows 9.5p2 の sshd (既定シェル `cmd`)、ローカル管理者 `tester` |
| ネットワーク | Internal スイッチ `host-shell-test` のみ。ホスト 192.168.250.1/24、ゲスト 192.168.250.10/24 (固定)。ゲートウェイと IPv4 の DNS サーバーなし。ホストは IP 転送を行わない |
| host-shell | 作業ツリーからビルド (v20261104.0.0、Go 1.26.10) |
| MCP クライアント | このセッションには host-shell の MCP を登録していないため、Python で書いた検証用ハーネスが JSON-RPC を直接やり取りした |

ゲストのインストール直後と、閉じたネットワークの設定後に、それぞれチェックポイント `clean-install` と `closed-ssh` を作った。

## 配置 (SSH の標準入力だけで送る)

利用ガイドの手順 (Windows PowerShell 5.1 の `[Console]::OpenStandardInput()` で標準入力をファイルへ書く) と、スキル host-shell-connect の `deploy.sh` は、どちらも転送の途中で止まった。

| 確認 | 結果 |
|---|---|
| PowerShell の `CopyTo` で 64 KiB を送る | 0.6 秒で完了 |
| 同じく 128 KiB | 全バイトが書き込まれた後も EOF を受け取らず停止 |
| 同じく 4 MiB、12 MiB | 135168 バイト (132 KiB) で停止。2 回とも同じ位置 |
| `-InputFormat None` を付ける、`Read` のループに変える | 変わらず停止 |
| ゲストからの取り出し (PowerShell の標準出力へ 4 MiB) | 1.2 秒で完了 |
| `findstr` への 4 MiB の送り込み | 0.4 秒で完了 |
| base64 にして `findstr` で書き出し、`certutil -decode` で戻す (4 MiB、12 MiB) | 完了し、SHA-256 が一致 |
| 接続元の ssh を Windows 版から MSYS2 の OpenSSH 10.6p1 に変える | 変わらず停止 |

ネットワークと sshd は正常で、PowerShell 5.1 が標準入力を読む部分だけが止まると判断した。原因は特定していない。Windows Defender のリアルタイム保護を一時的に止めて切り分けようとしたが、その操作は実施しなかったため、Defender の関与は未確認である。

対応として、`deploy.sh` の Windows 向け転送と利用ガイドの手順を、base64 と `findstr`・`certutil` の方式に変えた。`deploy.sh` は Windows の接続先で 12 MiB を約 5.7 秒で配置し、2 回目は `up to date` になった。Linux 向けの経路は、WSL の Podman コンテナー (Oracle Linux 10、`--network none`) で `deployed`、`up to date`、`probe.sh` の OK を確認した。

`remote.sh ROUTE true` による経路確認は、Windows では `cmd` に `true` がないため標準エラーに警告が出るが、標準出力は空で判定に影響しない。

## MCP の基本動作

`bridge` 経由で次を確認した。

| 項目 | 結果 |
|---|---|
| `sys_get_info` | `windows`/`amd64`、既定シェル `powershell`、文字コード `shift_jis` |
| `exec_run` (PowerShell、`cmd`、`argv`、`stdin`) | 日本語の出力を含めて成功。`cmd` の終了コード 3 も返る |
| `job_start` の `pty` (ConPTY) と `job_write` | `Read-Host` への入力と応答が成功 |
| `fs_mkdir`、`fs_write`、`fs_read`、`fs_edit`、`search_grep`、`fs_remove` | 成功 |
| `transfer_push` (12.7 MiB) | 1.2 秒。SHA-256 一致 |
| `transfer_pull` (12.7 MiB) | 3.7 秒。SHA-256 一致 |
| `reg_read`、`svc_list`、`proc_list`、`net_list_listeners`、`disk_list` | 成功 |
| 切断時の後片付け | pty ジョブのプロセスと host-shell が残らない |

Go 製の host-shell が SSH の標準入力を読む経路では、PowerShell で起きた停止は起きなかった。

## クライアント経由 HTTP プロキシとポート転送

| 項目 | 結果 |
|---|---|
| 子プロセスの `HTTP_PROXY`・`HTTPS_PROXY` | 自動設定される |
| `curl.exe https://api.github.com/zen` | `CONNECT` は許可されるが、Schannel の失効確認が `CRYPT_E_REVOCATION_OFFLINE` で失敗 (終了コード 35) |
| 同 `--ssl-revoke-best-effort`、`--ssl-no-revoke` | 成功 |
| 未許可の `example.com` | `CONNECT` に対する 403 |
| Windows PowerShell 5.1 の `Invoke-WebRequest` | `HTTPS_PROXY` を読まず、名前解決に失敗 |
| 同 `-Proxy` と `-ProxyCredential` | 成功 |
| ローカル転送 (`forward_open local`) | 接続先の 127.0.0.1:18080 の応答を手元で受信 |
| リモート転送 (`forward_open remote`) | 接続先の `curl.exe` から手元のサーバーの応答を受信 |

## code-server

スキル host-shell-code-server の手順どおりに、`download.ps1 windows-amd64` で取得したキットを `transfer_push` で送り、導入・起動・転送を行った。

| 項目 | 結果 |
|---|---|
| キットの転送 | 配布アーカイブ 212 MB を 17.5 秒 |
| `install.ps1 -Locale fr` | 本体と既定の拡張機能 3 件を導入した後、`no language pack for fr is installed` で停止。`argv.json` は作られない (25 秒) |
| `install.ps1 -Locale ja` | `registered language packs: ja`、`display language: ja`。`argv.json` に `"locale": "ja"` |
| `config.yaml` | 内容を読まずに `transfer_pull` で手元へコピー |
| `start.ps1` | `127.0.0.1:8000` で待ち受け |
| `forward_open` と `/login` | HTTP 200、タイトル `code-server login` |
| `job_stop` | `node.exe` が残らない |
| ディスク使用量 (ファイル サイズの合計) | キット 203.5 MiB、本体 611 MiB、拡張機能 4.7 MiB、ユーザー データ 1.2 MiB。ドライブの空きは 836.4 MiB 減少 |

ブラウザーでの日本語表示は、ログインにパスワードの入力が必要で、スキルの規則によりパスワードを読まないため確認していない。

## 未確認の項目

- 原因: PowerShell 5.1 の標準入力が止まる理由 (Defender の関与を含む)
- Copilot CLI など CLI 型コーディングエージェントの Windows 接続先での動作 (認証に利用者の操作が必要なため対象外とした)
- Windows Server、arm64
