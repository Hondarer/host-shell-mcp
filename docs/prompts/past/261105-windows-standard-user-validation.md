# 管理者でない Windows 接続ユーザーの検証記録 (2026-11-05)

> [!IMPORTANT]
> 作業時点の記録です。現在の仕様と検証済みの範囲は [利用ガイド](../../ja/user-guide.md) の「管理者でない接続ユーザー」、[code-server の導入](../../ja/code-server.md)、[接続元を使う名前解決](../../ja/remote-dns.md) を参照してください。

## 目的

これまでの Windows 接続先の検証は、接続ユーザー `tester` が管理者 (Administrators) の状態で行っていた。同じ操作が Users または Power Users の接続ユーザーでもできるかを確かめた。

## 環境と手順

test-bed をチェックポイント `closed-ssh` に戻し、管理者のうちに次を行ってから検証した。

- 公開鍵を `C:\Users\tester\.ssh\authorized_keys` に置き、所有者 (`tester`) と SYSTEM だけに権限を絞る。管理者でないユーザーには、sshd は `administrators_authorized_keys` を使わないためである。
- `tester` を Administrators (S-1-5-32-544) から外す。2 回目は `closed-ssh` に戻してから、Power Users (S-1-5-32-547) にも入れる。

新しい SSH ログオンのグループは、Users だけの場合が Users、NETWORK (S-1-5-2)、整合性レベル Medium だった。Power Users を加えた場合は、整合性レベルが Medium Plus になった。どちらにも INTERACTIVE は含まれない。

## 結果

Users だけの場合と Power Users を加えた場合で、結果は同じだった (`--remote-dns-configure` のエラー文を除く。下記)。

| 対象 | 結果 |
|---|---|
| `detect.sh`、`deploy.sh` (新規と `up to date`)、`probe.sh` | 成功 |
| `exec_run` (PowerShell、`cmd`、`argv`、`stdin`)、ConPTY の `job_write`、ファイル操作 | 成功 |
| `transfer_push`・`transfer_pull` (12.8 MiB) | 成功、SHA-256 一致 |
| `reg_read` (HKLM)、`reg_write`・`reg_delete` (HKCU) | 成功 |
| `reg_write` (HKLM) | `Access is denied.` |
| `svc_list`、`svc_control` | `open service control manager: Access is denied.` |
| `log_query` (`System`、`Application`、`Security`) | `wevtutil` が終了コード 5 (アクセス拒否) |
| `proc_list`、`net_list_listeners`、`disk_list` | 成功 |
| クライアント経由 HTTP プロキシ (curl.exe と PowerShell の回避策)、ローカル転送、リモート転送 | 成功 |
| code-server の `install.ps1 -Locale fr` と `-Locale ja` | 前回と同じ結果 (fr はエラー停止、ja は登録) |
| remote-dns のループバックの DNS サーバー | 待ち受けに成功。`Resolve-DnsName -Server 127.0.0.1` (UDP、TCP) と Node.js `dns.resolve4` が成功 |
| 手動設定の手順、`Add-DnsClientNrptRule`、`New-NetRoute` | 拒否される |

## 見つかった問題と修正

### プロセスを終了できない

一般ユーザーの SSH セッションでは、自分が起動したプロセスに対しても `taskkill` が `ERROR: Access denied` で失敗した (`/T` の有無によらない)。`taskkill` は WMI を使っており、ネットワーク ログオンの一般ユーザーは WMI に接続できないためと考えられる。host-shell の `job_stop`、`proc_kill`、タイムアウトとキャンセルによる終了は `taskkill` に頼っていたため、どれも止まらなかった。code-server を `job_stop` しても node.exe が動き続け、セッションを閉じたときにジョブ オブジェクトで終了するだけだった。

`internal/proc` の Windows 向けの `Signal` と `SignalGroup` を、`OpenProcess` と `TerminateProcess` で直接終了する実装に変えた。子孫は Toolhelp32 のスナップショットから親子関係をたどって求める。PID の再利用で無関係なプロセスをたどらないよう、親より後に作られたプロセスだけを子とみなす。終了中に起動した子を拾うため、スナップショットを最大 3 回取り直す。修正後は、Users と Power Users の両方で `job_stop` (孫プロセスを含む)、`exec_run` のタイムアウト、`proc_kill` が成功し、code-server も `job_stop` から 0.1 秒以内に止まった。管理者でも同じ結果だった。

### code-server の起動と実行中の判定

- `start.ps1` は、空いているポートを `Get-NetTCPConnection` (CIM) で探していた。CIM に接続できないため、起動時に `CIM サーバーに接続できません。アクセスは拒否されました` で停止した。`IPGlobalProperties.GetActiveTcpListeners()` に変えた。
- `common.ps1` の `Get-HscsRunningIds` は `Get-CimInstance Win32_Process` を使っていた。`-ErrorAction SilentlyContinue` のため、失敗しても「実行中のプロセスなし」と扱われ、`uninstall.ps1` の実行中チェックが働かなかった。CIM が使えない場合は、同梱の node.exe のパスを `Get-Process` で照合する形に変えた。修正後、実行中の `uninstall.ps1` は停止した。

### `--remote-dns-configure` のエラー

Users だけの場合は、トークンが昇格していないため、起動時に「昇格済みの管理者セッションが必要」というエラーで停止した。Power Users を加えた場合は、UAC が Power Users を管理者相当のグループとして扱うため、ネットワーク ログオンのトークンが「昇格済み」と判定された。そのまま進み、`DnsPolicyConfig` を開く段階で `Access is denied.` になった。設定は何も変わらないが、エラーの意味が分かりにくいため、アクセス拒否の場合も管理者権限が必要という文言を付けるようにした。

### 自動テストの失敗

ホストで `internal/remotedns` のテストが、UDP の bind 失敗で落ちた。テストはポート 0 で TCP を先に開き、得たポート番号で UDP を開いていた。Windows の TCP の動的ポートは連番で割り当てられるため、Hyper-V が UDP に予約した 100 個単位の範囲に続けて当たり、10 回の再試行でも抜けなかった。UDP を先に開き、得たポート番号で TCP を開く順に変えた。

## 管理者での回帰確認

`closed-ssh` (管理者) に戻し、修正後のビルドで、`svc_list`・`svc_control`・HKLM への書き込み、`job_stop`・タイムアウト・`proc_kill`、揮発性の NRPT 規則による `--remote-dns-configure` (`DnsPolicyConfig` がある場合とない場合、強制終了後の片付け) が成功することを確かめた。

`closed-ssh` の時点で `DnsPolicyConfig` キーは存在していた。このキーは Windows の初期状態から存在すると考えられる。

## 未確認の項目

- ドメイン ユーザー、Windows Server、arm64
- パスワード認証でログオンした場合 (今回は公開鍵認証のみ)
