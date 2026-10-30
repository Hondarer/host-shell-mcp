# host-shell 運用ガイド

この文書は、`host-shell` を安全に運用するための設定と、日常の管理の方法を説明します。  
導入の手順は [利用ガイド](user-guide.md)、仕様の詳細は [リファレンス](reference.md) を参照してください。

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

`host-shell` は、既定では接続したユーザーの権限で何でもできます。  
つまり、制限のオプションなしでエージェントに `host-shell` を使わせることは、そのユーザーでの SSH のログインを許すことと同じです。

安全性は、次の 4 つを組み合わせて確保します。

| 層 | 手段 | 守るもの |
|---|---|---|
| 接続先の OS | 専用のユーザー、ファイルの権限、sudoers の規則 | 操作できる範囲 (確実な境界) |
| host-shell | [制限のオプション](#制限のオプション) | 公開するツール、操作する場所、実行するプログラム、接続する宛先、実行前の確認 |
| エージェント | 変更を伴うツールの承認 | 意図しない変更の実行 |
| 記録 | `host-shell` の操作ログ | 何が行われたかの追跡 |

Table: 安全性の層

## 専用のユーザーと鍵

エージェント用に、接続先に専用のユーザーを作り、必要な範囲だけの権限を与えます。

- 調査だけの用途なら、ログやソースを読める一般ユーザーにします。
- 変更が必要なファイルやディレクトリだけ、グループや ACL で書き込みを許可します。
- 管理者の権限が必要な操作は、[sudo の設定](#sudo-の設定) で個別に許可します。

鍵もエージェント専用のものを作り、ほかの用途と共有しません。  
鍵を無効にすれば、エージェントからの操作だけを止められます。

```bash
ssh-keygen -t ed25519 -f ~/.ssh/id_host_shell -C "host-shell agent"
```

手元の `~/.ssh/config` で、接続先ごとにこの鍵を指定します。

```{.text caption="~/.ssh/config (クライアント)"}
Host target
    HostName 192.0.2.10
    User hsagent
    IdentityFile ~/.ssh/id_host_shell
    IdentitiesOnly yes
    RequestTTY no
```

中継サーバーを経由する場合は、中継サーバー上の `~/.ssh/config` にも同じように次の段の設定を書きます。

## 制限のオプション

`host-shell` の起動オプションで、エージェントに使わせる範囲を絞れます。指定しなかったものは制限しません。詳しくは [リファレンスの制限 (方針)](reference.md#制限-方針) を参照してください。

| 用途 | オプションの例 |
|---|---|
| 障害の調査だけ (何も変えない) | `--read-only` |
| ログと設定を読むだけ | `--tools files,search --read-only --allow-read-path /var/log --allow-read-path /etc/app` |
| アプリのディレクトリだけで開発 | `--tools files,search,exec --allow-path /srv/app --allow-command git,make,go,npm,node` |
| 変更の前に必ず人が確認 | `--confirm destructive` |
| 外への通信を止める | `--allow-host localhost,127.0.0.1,::1,unix:/var/run/*.sock` |
| 秘密を出力に出さない | `--redact --unset-env 'AWS_*,*TOKEN*,*SECRET*,*PASSWORD*'` |

Table: 制限のオプションの例

注意する点は次のとおりです。

- 制限は、ツールの引数に対する検査です。`exec` の組 (`exec_run` など) を公開すると、コマンドはファイルやネットワークに自由に触れられます。場所や宛先を厳密に絞るには、`--tools` で `exec` を外すか、`--allow-command` でプログラムを絞ります。シェル (`sh`、`powershell` など) を許可すると、シェルの中では何でもできます。
- `--deny-command` は、書き方を変えれば避けられます。誤操作の防止に使い、境界としては使わないでください。
- `--confirm` は、クライアントが MCP の elicitation に対応している必要があります。対応していない場合、対象のツールはすべて失敗します。
- `--redact` は、既知の形の秘密だけを伏せます。秘密を読ませたくないファイルは、OS の権限で読めないようにしてください。
- オプションは、次の [起動するコマンドの固定](#起動するコマンドの固定) で `authorized_keys` に書き、利用者やエージェントが外せないようにします。

## 起動するコマンドの固定

接続先の `authorized_keys` で、この鍵で起動できるコマンドを `host-shell` に固定できます。  
この鍵が漏れても、`host-shell` を経由しない操作 (対話的なログイン、ポート転送など) はできなくなり、すべての操作が操作ログに残ります。

```{.text caption="~hsagent/.ssh/authorized_keys (Linux の接続先)"}
command="/opt/host-shell/host-shell --mcp",no-pty,no-port-forwarding,no-agent-forwarding,no-X11-forwarding ssh-ed25519 AAAA... host-shell agent
```

```{.text caption="C:\Users\hsagent\.ssh\authorized_keys (Windows の接続先)"}
command="C:/tools/host-shell.exe --mcp",no-pty,no-port-forwarding,no-agent-forwarding,no-X11-forwarding ssh-ed25519 AAAA... host-shell agent
```

`command=` を指定すると、クライアントが指定したコマンドは無視され、常に `host-shell` が起動します。  
このため、エージェントに登録するコマンドは `ssh -T target` だけでも動きます。

[制限のオプション](#制限のオプション) も、ここに書きます。エージェントの登録で指定したオプションは無視されるため、利用者やエージェントが制限を外すことはできません。

```{.text caption="~hsagent/.ssh/authorized_keys (読み取り専用に固定する例)"}
command="/opt/host-shell/host-shell --mcp --read-only --redact",no-pty,no-port-forwarding,no-agent-forwarding,no-X11-forwarding ssh-ed25519 AAAA... host-shell agent
```

`no-pty` は SSH のセッションに端末を割り当てないための指定です。`host-shell` が `pty` のジョブで作る疑似端末には影響しません。

制限のオプションを付けない場合、`host-shell` 自体が任意のコマンドを実行できるため、これは権限を制限するものではありません。  
権限の範囲は、ユーザーと sudoers の規則で決めてください。

ユーザー単位で固定する場合は、sshd の `ForceCommand` も使えます。

```{.text caption="/etc/ssh/sshd_config (Linux の接続先)"}
Match User hsagent
    ForceCommand /opt/host-shell/host-shell --mcp
    AllowTcpForwarding no
    X11Forwarding no
    PermitTTY no
```

## sudo の設定

`sudo_run` は、`NOPASSWD` の規則がなくても、利用者がエージェントにパスワードを教えれば、`password` の引数で使えます。パスワードは sudo が尋ねたときだけ sudo に渡り、コマンドや操作ログには渡りません ([リファレンスの sudo_run](reference.md#sudo_run))。  
ただし、パスワードはエージェントと、その先の LLM のサービスを通ります。パスワードを渡したくない場合は、sudoers に `NOPASSWD` の規則を書いて、必要なコマンドだけを許可します。  
管理のツールの `sudo` は、パスワードを尋ねない `sudo -n` だけに対応します。

### コマンドを絞って許可する (推奨)

許可するコマンドを絶対パスで列挙します。

```{.text caption="/etc/sudoers.d/hsagent"}
hsagent ALL=(root) NOPASSWD: /usr/bin/systemctl status *, /usr/bin/systemctl restart nginx, /usr/bin/journalctl *
```

エージェントには、`exec_run` の `argv` で `sudo -n` を呼ばせます。

```json
{"argv": ["sudo", "-n", "/usr/bin/systemctl", "restart", "nginx"]}
```

`sudo_run` ツールは内部で `sudo -n -- sh -c '<command>'` を実行するため、この規則では使えません。

### すべてのコマンドを許可する

`sudo_run` を使うには、`sh` の実行を許可する必要があります。これは root の権限をすべて与えることと同じです。  
検証用の環境など、影響を受け入れられる場合に限ってください。

```{.text caption="/etc/sudoers.d/hsagent"}
hsagent ALL=(root) NOPASSWD: ALL
```

sudoers のファイルは、必ず `visudo -f /etc/sudoers.d/hsagent` で編集し、構文を確認してください。

## エージェント側の承認

変更を伴うツールには `destructiveHint`、読み取りだけのツールには `readOnlyHint` の注釈が付いています ([リファレンス](reference.md#ツールの注釈))。  
エージェント側で、少なくとも次のツールは実行の前に人が承認するよう設定してください。

- `exec_run`、`job_start`、`job_write`、`job_stop`
- `fs_edit`、`fs_write`、`fs_move`、`fs_copy`、`fs_remove`
- `sudo_run`
- `net_http`、`net_tcp` (接続先から外部に送れるため)
- `proc_kill`、`svc_control`、`reg_write`、`reg_delete`
- bridge を使う場合は `transfer_push`、`transfer_pull`

エージェント側で承認を設定できない場合や、接続先の管理者として確認を強制したい場合は、`--confirm destructive` を使います。

`fs_mkdir` は、新しいディレクトリを作るだけで既存の内容を変えないため、`destructiveHint` を `false` にしています。

Claude Code の設定の例は [利用ガイド](user-guide.md#claude-code) にあります。  
読み取りだけのツールも、機密の情報を読める点に注意してください。読める範囲は、ユーザーの権限で制限します。

## 操作ログの管理

### 保存先

既定の保存先は次のとおりです。運用に合わせて `--log-file` で変更してください。  
実際のファイルは、日付ごとに、元の名前の拡張子の前に日付を入れたものになります (例: `audit-2026-10-31.log`)。

| OS | 既定の元の名前 |
|---|---|
| Linux | `~/.local/state/host-shell/audit.log` |
| Windows | `%LOCALAPPDATA%\host-shell\audit.log` |

Table: 操作ログの既定の元の名前

操作ログは、接続したユーザーの権限で書き込みます。  
エージェントがログを書き換えられないようにするには、ログを別の場所に転送してください (下記)。

### 日ごとの切り替えと保持

`host-shell` は、接続先のローカル時刻の日付ごとにファイルを分け、当日のほかに 5 日分を残して、それより古いファイルを消します。logrotate などの設定は要りません。  
1 件ごとにファイルを開いて閉じるため、複数のセッションが同時に書いても行は混ざりません。  
5 日より長く残す必要がある場合は、下記の転送で、ログを別の場所に集めてください。

日付ごとに分ける前の版を使っていた場合は、日付のない `audit.log` が残ります。不要であれば消してください。

### 転送と監視

操作ログは 1 行 1 件の JSON (JSON Lines) なので、ログの収集基盤にそのまま取り込めます。  
Linux では、rsyslog の imfile や Fluent Bit などで、ファイル (`audit-*.log`) を読んで転送できます。

`jq` で次のように絞り込めます。

```bash
# エラーになった呼び出し
jq -c 'select(.msg == "tool_call" and .is_error)' audit-*.log

# 変更を伴うツールの呼び出し
jq -c 'select(.tool | IN("exec_run", "job_start", "job_write", "sudo_run", "fs_edit", "fs_write", "fs_move", "fs_copy", "fs_remove"))' audit-*.log
```

### 記録される内容

コマンド行 (`command`、`argv`) とファイルのパスは、そのまま記録されます。  
書き込んだ内容 (`fs_write` の `content`) は長さとハッシュだけを、置換の文字列 (`fs_edit` の `old_string`、`new_string`) と標準入力 (`stdin`、`job_write` の `input`) は長さだけを記録します。  
形式の詳細は [リファレンスの操作ログ](reference.md#操作ログ) を参照してください。

## 更新

1. 新しい版の実行ファイルを、別の名前で接続先に送ります ([利用ガイドの配置](user-guide.md#接続先への配置))。
2. ハッシュを手元と比べます。
3. エージェントの MCP の接続を止めます。Windows では、実行中の `host-shell.exe` は置き換えられません。
4. 実行ファイルを置き換えます。
5. `--version` で新しい版を確認し、エージェントの接続を再開します。

## 運用前の確認項目

- [ ] エージェント専用のユーザーと鍵を用意した
- [ ] ユーザーの権限で、読める範囲と書ける範囲を確認した
- [ ] 必要に応じて、制限のオプション (`--read-only`、`--tools`、`--allow-path` など) を決めた
- [ ] sudoers の規則を、必要なコマンドだけに絞った
- [ ] `authorized_keys` の `command=` または `ForceCommand` で、起動を `host-shell` (と制限のオプション) に固定した
- [ ] 各段で `-T` を指定し、接続先のシェルが余計な出力をしないことを確認した
- [ ] エージェント側で、変更を伴うツールに承認を必須にした
- [ ] 操作ログの保存先と、5 日より長く残す場合の転送を決めた
