# host-shell 運用ガイド

> [!NOTE]
> この文書が正本です。英訳は [../en/operations.md](../en/operations.md) にあります。内容が一致しない場合は日本語版が優先されます。

`host-shell` を安全に運用するための権限設計、制限オプション、ログ管理の手順を説明します。  
導入手順は [利用ガイド](user-guide.md)、各機能の詳細仕様は [リファレンス](reference.md) を参照してください。

## 目次

- [安全性の考え方](#安全性の考え方)
- [専用のユーザーと鍵](#専用のユーザーと鍵)
- [制限のオプション](#制限のオプション)
- [起動するコマンドの固定](#起動するコマンドの固定)
- [sudo の設定](#sudo-の設定)
- [エージェント側の承認](#エージェント側の承認)
- [操作ログの管理](#操作ログの管理)
- [更新](#更新)
- [運用前の確認項目](#運用前の確認項目)

## 安全性の考え方

`host-shell` は既定で接続ユーザーの権限をそのまま行使できるため、制限オプションなしでエージェントに公開すると、そのユーザーでの SSH ログインを許可するのと同じ権限を与えます。

実運用では次の 4 層を組み合わせて安全性を確保します。

| 層 | 手段 | 保護対象 |
|---|---|---|
| 接続先 OS | 専用ユーザー、ファイル権限、sudoers 規則 | 操作可能範囲の強制境界 |
| host-shell | [制限のオプション](#制限のオプション) | 公開ツール、許可パス、許可コマンド、接続先ホスト、事前確認 |
| エージェント | 変更系ツールの実行前承認 | 意図しない変更操作の抑止 |
| 記録 | `host-shell` の操作ログ | 実行された操作の監査・追跡 |

Table: 安全性の層

## 専用のユーザーと鍵

接続先にエージェント専用ユーザーを作成し、最小限の権限を付与します。

- 調査用途では、ログやソースの読み取り権限のみを持つ一般ユーザーにします。
- 変更が必要なファイルやディレクトリにのみ、グループ権限または ACL で書き込みを許可します。
- 管理者権限を要する操作は [sudo の設定](#sudo-の設定) で対象コマンドを個別に許可します。

SSH 鍵もエージェント専用に作成し、他用途の鍵と分離します。鍵を失効させるだけでエージェントからのアクセスを即座に遮断できます。

```bash
ssh-keygen -t ed25519 -f ~/.ssh/id_host_shell -C "host-shell agent"
```

クライアントの `~/.ssh/config` で接続先ごとに専用鍵を指定します。

```{.text caption="~/.ssh/config (クライアント)"}
Host target
    HostName 192.0.2.10
    User hsagent
    IdentityFile ~/.ssh/id_host_shell
    IdentitiesOnly yes
    RequestTTY no
```

中継サーバーを経由する場合は、中継サーバー上の `~/.ssh/config` にも次段の接続設定を同様に記述します。

## 制限のオプション

`host-shell` の起動オプションで、エージェントに許可する操作範囲を制限できます (未指定の項目は制限されません。詳細は [リファレンスの制限 (方針)](reference.md#制限-方針) を参照してください)。

| 用途 | オプション指定例 |
|---|---|
| 障害調査のみ (変更禁止) | `--read-only` |
| ログと設定の読み取りのみ | `--tools files,search --read-only --allow-read-path /var/log --allow-read-path /etc/app` |
| 特定ディレクトリでの開発 | `--tools files,search,exec --allow-path /srv/app --allow-command git,make,go,npm,node` |
| 変更操作の前に利用者確認を必須化 | `--confirm destructive` |
| 外部ネットワーク通信の禁止 | `--allow-host localhost,127.0.0.1,::1,unix:/var/run/*.sock` |
| 機密情報の出力・継承の抑止 | `--redact --unset-env 'AWS_*,*TOKEN*,*SECRET*,*PASSWORD*'` |

Table: 制限のオプションの例

制限オプションを使用する際は、次の点に留意してください。

- 制限はツール引数に対する検査です。`exec` グループ (`exec_run` など) を公開すると、起動されたコマンドから任意のファイルやネットワークへアクセスできます。アクセス先を厳密に制限するには、`--tools` で `exec` を除外するか、`--allow-command` で実行プログラムを限定します (シェル `sh`、`powershell` 等を許可すると任意のコマンドを実行できます)。
- `--deny-command` はコマンド ライン文字列の正規表現照合であり、表記を変えると回避できるため、誤操作防止の補助として使用しセキュリティ境界にはしないでください。
- `--confirm` はクライアントが MCP の elicitation に対応している必要があります (非対応クライアントでは対象ツールがすべてエラーになります)。
- `--redact` は既知パターンの機密情報のみを伏せ字化します。読み取らせたくない機密ファイルは OS のファイル権限で保護してください。
- 指定したオプションを利用者やエージェントが解除できないよう、[起動するコマンドの固定](#起動するコマンドの固定) に従って接続先の `authorized_keys` 等で固定します。

## 起動するコマンドの固定

接続先の `authorized_keys` に `command=` を設定すると、専用鍵で実行できるコマンドを `host-shell` に固定できます。  
万一鍵が漏洩しても、`host-shell` を介さない対話ログインやポート転送を防ぎ、全操作を操作ログに記録できます。

```{.text caption="~hsagent/.ssh/authorized_keys (Linux の接続先)"}
command="/opt/host-shell/host-shell --mcp",no-pty,no-port-forwarding,no-agent-forwarding,no-X11-forwarding ssh-ed25519 AAAA... host-shell agent
```

```{.text caption="C:\Users\hsagent\.ssh\authorized_keys (Windows の接続先)"}
command="C:/tools/host-shell.exe --mcp",no-pty,no-port-forwarding,no-agent-forwarding,no-X11-forwarding ssh-ed25519 AAAA... host-shell agent
```

`command=` を設定するとクライアント側の指定コマンドは無視され、常に `host-shell` が起動します (エージェント側の登録コマンドも `ssh -T target` だけで動作します)。  
[制限のオプション](#制限のオプション) も `command=` 内に記述することで、クライアント側から制限を外せなくなります。

```{.text caption="~hsagent/.ssh/authorized_keys (読み取り専用に固定する例)"}
command="/opt/host-shell/host-shell --mcp --read-only --redact",no-pty,no-port-forwarding,no-agent-forwarding,no-X11-forwarding ssh-ed25519 AAAA... host-shell agent
```

`no-pty` は SSH セッションへの PTY 割り当てを禁止する指定であり、`host-shell` が `pty` ジョブ用に作成する疑似端末には影響しません。  
なお、制限オプションを付けない場合、`host-shell` 自身は接続ユーザーの権限で任意のコマンドを実行できます。権限の上限は OS ユーザーと sudoers で制御してください。

ユーザー単位で固定する場合は `sshd_config` の `ForceCommand` も使用できます。

```{.text caption="/etc/ssh/sshd_config (Linux の接続先)"}
Match User hsagent
    ForceCommand /opt/host-shell/host-shell --mcp
    AllowTcpForwarding no
    X11Forwarding no
    PermitTTY no
```

## sudo の設定

`exec_sudo` は、sudoers に `NOPASSWD` 設定がなくても `password` 引数でパスワードを渡せば実行できます。パスワードは `sudo` が要求したときのみ askpass 経由で渡され、実行コマンドの標準入力や操作ログには含まれません ([リファレンスの exec_sudo](reference.md#exec_sudo))。  
ただし、パスワード文字列はエージェントおよび LLM サービスを経由します。パスワードを入力せずに運用する場合は、sudoers に `NOPASSWD` 規則を設定して許可コマンドを限定します。  
なお、管理ツール (`proc_kill`、`svc_control`、`log_query`) の `sudo` オプションはパスワード不要の `sudo -n` のみに対応します。

### コマンドを絞って許可する (推奨)

許可するコマンドを絶対パスで列挙します。

```{.text caption="/etc/sudoers.d/hsagent"}
hsagent ALL=(root) NOPASSWD: /usr/bin/systemctl status *, /usr/bin/systemctl restart nginx, /usr/bin/journalctl *
```

エージェントには `exec_run` の `argv` で `sudo -n` を呼び出させます。

```json
{"argv": ["sudo", "-n", "/usr/bin/systemctl", "restart", "nginx"]}
```

`exec_sudo` ツールは内部で `sudo -n -- sh -c '<command>'` を実行するため、特定のコマンドのみを許可した sudoers 規則では使用できません。

### すべてのコマンドを許可する

`exec_sudo` を `NOPASSWD` で使用するには `sh` の実行許可が必要となり、実質的に全 root 権限を付与することになります。検証環境など影響範囲を許容できる場合に限定してください。

```{.text caption="/etc/sudoers.d/hsagent"}
hsagent ALL=(root) NOPASSWD: ALL
```

sudoers ファイルは必ず `visudo -f /etc/sudoers.d/hsagent` で編集し、構文を検証してください。

## エージェント側の承認

変更を伴うツールには `destructiveHint`、読み取り専用ツールには `readOnlyHint` 注釈が付与されています ([リファレンス](reference.md#ツールの注釈))。  
エージェント側で、少なくとも次のツールは実行前に利用者の承認を必須とするよう設定してください。

- `exec_run`、`job_start`、`job_write`、`job_stop`
- `fs_edit`、`fs_write`、`fs_move`、`fs_copy`、`fs_remove`
- `exec_sudo`
- `net_request_http`、`net_exchange` (接続先から外部へリクエストを送信できるため)
- `proc_kill`、`svc_control`、`reg_write`、`reg_delete`
- `transfer_push`、`transfer_pull` (`bridge` 使用時)

エージェント側で承認設定を行えない場合や、接続先管理者として承認を強制する場合は `--confirm destructive` を指定します。  
`fs_mkdir` は新規ディレクトリを作成するのみで既存データを変更しないため、`destructiveHint: false` としています。

Claude Code の承認設定例は [利用ガイド](user-guide.md#claude-code) を参照してください。  
読み取り専用ツールであっても機密ファイルを読み取れるため、読み取り可能な範囲は OS ユーザーの権限と `--allow-read-path` で制限してください。

## 操作ログの管理

### 保存先

操作ログの既定の基準パスは次のとおりです (`--log-file` で変更可能)。  
実際の出力ファイル名は、拡張子の直前にローカル日付を挿入した形式になります (例: `audit-2026-10-31.log`)。

| OS | 既定の基準パス |
|---|---|
| Linux | `~/.local/state/host-shell/audit.log` |
| Windows | `%LOCALAPPDATA%\host-shell\audit.log` |

Table: 操作ログの既定の基準パス

操作ログは接続ユーザーの権限で書き込まれます。エージェント自身によるログ改ざんを防ぐには、外部のログ収集基盤へ転送してください。

### 日ごとの切り替えと保持

`host-shell` は接続先のローカル日付ごとにログ ファイルを切り替え、当日分に加えて直近 5 日分を保持し、それより古いファイルを自動削除します (logrotate 等の設定は不要です)。  
1 レコードごとにファイルを開いて追記・クローズするため、複数セッションが同時に書き込んでも行が混在しません。  
5 日を超えて保持する場合は、外部のログ収集基盤へ転送してください。

日付なしの `audit.log` は自動削除されないため、不要であれば手動で削除してください。

### 転送と監視

操作ログは 1 行 1 レコードの JSON Lines 形式であり、rsyslog (`imfile`) や Fluent Bit 等のログ収集基盤で `audit-*.log` をそのまま取り込めます。

`jq` による抽出例は次のとおりです。

```bash
# エラーになった呼び出し
jq -c 'select(.msg == "tool_call" and .is_error)' audit-*.log

# 変更を伴うツールの呼び出し
jq -c 'select(.tool | IN("exec_run", "job_start", "job_write", "exec_sudo", "fs_edit", "fs_write", "fs_move", "fs_copy", "fs_remove"))' audit-*.log
```

### 記録される内容

コマンド ライン (`command`、`argv`) とファイル パスはそのまま記録されます。  
書き込み内容 (`fs_write` の `content`) は文字数と SHA-256 ハッシュのみ、置換文字列 (`fs_edit` の `old_string`、`new_string`) と標準入力 (`stdin`、`job_write` の `input`) は文字数のみを記録します。詳細は [リファレンスの操作ログ](reference.md#操作ログ) を参照してください。

## 更新

1. 新バージョンの実行ファイルを別名で接続先へ転送します ([利用ガイドの配置](user-guide.md#接続先への配置))。
2. SHA-256 ハッシュをローカルのファイルと照合します。
3. エージェントの MCP 接続を停止します (Windows では実行中の `host-shell.exe` を上書きできません)。
4. 実行ファイルを置き換えます。
5. `--version` で新バージョンを確認し、エージェントの接続を再開します。

## 運用前の確認項目

- [ ] エージェント専用の OS ユーザーと SSH 鍵を作成した
- [ ] 専用ユーザーのファイル読み書き権限を確認した
- [ ] 必要な制限オプション (`--read-only`、`--tools`、`--allow-path` など) を決定した
- [ ] sudoers の許可対象を必要なコマンドのみに限定した
- [ ] `authorized_keys` の `command=` または `sshd_config` の `ForceCommand` で起動コマンドと制限オプションを固定した
- [ ] SSH の各段に `-T` を指定し、非対話シェルが標準出力へ不要な出力を行わないことを確認した
- [ ] エージェント側で変更系ツールの実行前承認を設定した
- [ ] 操作ログの保存先と、長期保持が必要な場合の外部転送を設定した

## クライアント側ネットワークの制限

[クライアント経由 HTTP プロキシ](client-proxy.md) に起動設定、制限、proxy_status、環境変数の優先順位、上限を記載しています。
