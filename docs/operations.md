# host-shell 運用ガイド

この文書は、`host-shell` を安全に運用するための設定と、日常の管理の方法を説明します。  
導入の手順は [利用ガイド](user-guide.md)、仕様の詳細は [リファレンス](reference.md) を参照してください。

## 目次

- [安全性の考え方](#安全性の考え方)
- [専用のユーザーと鍵](#専用のユーザーと鍵)
- [起動するコマンドの固定](#起動するコマンドの固定)
- [sudo の設定](#sudo-の設定)
- [エージェント側の承認](#エージェント側の承認)
- [操作ログの管理](#操作ログの管理)
- [更新](#更新)
- [運用前の確認項目](#運用前の確認項目)

## 安全性の考え方

`host-shell` は、接続したユーザーの権限で任意のコマンドを実行できます。  
つまり、エージェントに `host-shell` を使わせることは、そのユーザーでの SSH のログインを許すことと同じです。

`host-shell` 自身は、実行できるコマンドを制限しません。  
安全性は、次の 3 つを組み合わせて確保します。

| 層 | 手段 | 守るもの |
|---|---|---|
| 接続先の OS | 専用のユーザー、ファイルの権限、sudoers の規則 | 操作できる範囲 |
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

ただし、`host-shell` 自体が任意のコマンドを実行できるため、これは権限を制限するものではありません。  
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

`host-shell` は、パスワードを尋ねない `sudo -n` だけに対応します。  
sudoers に `NOPASSWD` の規則を書いて、必要なコマンドだけを許可します。

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

`fs_mkdir` は、新しいディレクトリを作るだけで既存の内容を変えないため、`destructiveHint` を `false` にしています。

Claude Code の設定の例は [利用ガイド](user-guide.md#claude-code) にあります。  
読み取りだけのツールも、機密の情報を読める点に注意してください。読める範囲は、ユーザーの権限で制限します。

## 操作ログの管理

### 保存先

既定の保存先は次のとおりです。運用に合わせて `--log-file` で変更してください。

| OS | 既定の保存先 |
|---|---|
| Linux | `~/.local/state/host-shell/audit.log` |
| Windows | `%LOCALAPPDATA%\host-shell\audit.log` |

Table: 操作ログの既定の保存先

操作ログは、接続したユーザーの権限で書き込みます。  
エージェントがログを書き換えられないようにするには、ログを別の場所に転送してください (下記)。

### ローテーション

`host-shell` 自身はローテーションを行いません。OS の仕組みを使ってください。

```{.text caption="/etc/logrotate.d/host-shell (Linux の接続先)"}
/home/hsagent/.local/state/host-shell/audit.log {
    weekly
    rotate 12
    compress
    missingok
    notifempty
    copytruncate
}
```

`host-shell` はファイルを追記で開いたままにするため、`copytruncate` を指定します。

### 転送と監視

操作ログは 1 行 1 件の JSON (JSON Lines) なので、ログの収集基盤にそのまま取り込めます。  
Linux では、rsyslog の imfile や Fluent Bit などで、ファイルを読んで転送できます。

`jq` で次のように絞り込めます。

```bash
# エラーになった呼び出し
jq -c 'select(.msg == "tool_call" and .is_error)' audit.log

# 変更を伴うツールの呼び出し
jq -c 'select(.tool | IN("exec_run", "job_start", "job_write", "sudo_run", "fs_edit", "fs_write", "fs_move", "fs_copy", "fs_remove"))' audit.log
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
- [ ] sudoers の規則を、必要なコマンドだけに絞った
- [ ] `authorized_keys` の `command=` または `ForceCommand` で、起動を `host-shell` に固定した
- [ ] 各段で `-T` を指定し、接続先のシェルが余計な出力をしないことを確認した
- [ ] エージェント側で、変更を伴うツールに承認を必須にした
- [ ] 操作ログの保存先、ローテーション、転送を決めた
