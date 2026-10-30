---
name: host-shell-connect
description: host-shell (SSH の先で動く MCP サーバー) を接続先に配置し、起動を確かめ、エージェントに登録するコマンドを用意する。「host-shell を target に入れて」「hop1 経由で target を MCP で操作できるようにして」「host-shell を更新して」のように、接続先のマシンを host-shell で使えるようにする、または版を上げるときに使う。Set up, deploy, update or verify host-shell on a remote host over SSH.
---

# host-shell の接続の準備

接続先のマシンに `host-shell` を配置し、SSH の経路で MCP サーバーとして起動できることを確かめ、エージェントに登録するコマンドを用意します。  
MCP サーバーの起動そのものは、登録したあとにエージェントが行います。この skill では起動しません。

この skill は、このフォルダだけで完結します。以降、この SKILL.md があるディレクトリを `$SKILL` と書きます。  
host-shell-mcp のリポジトリの中で使う場合は `.agents/skills/host-shell-connect` です。

## フォルダの構成

| パス | 内容 |
|---|---|
| `scripts/` | 経路の確認、判定、配置、起動の確認のスクリプト (下記) |
| `bin/host-shell-linux-amd64`、`bin/host-shell-linux-arm64` | Linux 用の実行ファイル |
| `bin/host-shell-windows-amd64.exe`、`bin/host-shell-windows-arm64.exe` | Windows 用の実行ファイル |
| `bin/VERSION` | 実行ファイルのバージョン |

Table: フォルダの構成

`bin/` は、host-shell-mcp のリポジトリで `make dist` を実行すると作られます (`dist/` と同じものの複製)。  
`bin/` がない場合や、利用者が新しい版を求めている場合は、リポジトリのルートで `make dist` を実行します (Go 1.25 以降が必要)。  
リポジトリがない環境では、GitHub の Release の `host-shell-connect-<tag>.zip` (または `.tar.gz`) を使います。`bin/` を含むこの skill のフォルダが入っています。用意は利用者に頼みます。

## スクリプト

いずれも SFTP とポート転送を使いません。

| スクリプト | 内容 |
|---|---|
| `remote.sh ROUTE COMMAND` | 接続先でコマンドを実行する。中継サーバーを経由する ssh の入れ子と引用符を組み立てる。標準入力は接続先に渡す |
| `detect.sh ROUTE` | 接続先の種類 (`linux-amd64`、`linux-arm64`、`windows-amd64`、`windows-arm64`) を出力する |
| `deploy.sh ROUTE TYPE REMOTE_PATH` | `bin/` の実行ファイルのうち `TYPE` に合うものを ssh の標準入力で送り、SHA-256 を照合する。同じ内容がすでにあれば送らない |
| `probe.sh ROUTE REMOTE_PATH [OPTIONS...]` | 接続先の host-shell に MCP の初期化とツールの一覧の取得を送り、応答を確かめる |

Table: スクリプト

`ROUTE` は、経由するホストと接続先を、手元に近い順にカンマで区切ったものです (例: `hop1,target`。直接なら `target`)。  
ホスト名は `~/.ssh/config` の名前も使えます。各段は `-T -o BatchMode=yes` で接続します (`HOST_SHELL_SSH_OPTS` で変更できます)。  
中継サーバーのシェルは POSIX sh 互換であることを前提とします。

## 手順

### 1. 必要な情報を確かめる

利用者の依頼から、次を決めます。分からないものだけを利用者に尋ねます。

| 項目 | 例 | 既定 |
|---|---|---|
| 経路 (`ROUTE`) | `hop1,target` | なし (必ず確かめる) |
| 配置先 (`REMOTE_PATH`) | `/opt/host-shell/host-shell`、`C:/tools/host-shell.exe` | Linux は `/opt/host-shell/host-shell`、Windows は `C:/tools/host-shell.exe` |
| 登録の名前 | `host-shell-target` | `host-shell-<接続先のホスト名>` |
| host-shell のオプション | `--timeout 2m`、`--windows-shell cmd` | なし |

Table: 必要な情報

配置先は、空白と引用符を含まないパスにします。  
Linux で `/opt` に書き込めないユーザーの場合は、ホーム ディレクトリからの相対パス (例: `.local/bin/host-shell`) を使います。sshd はホーム ディレクトリでコマンドを始めるため、相対パスのまま登録できます。`~` は手元のシェルが展開してしまうため使いません。

### 2. SSH の経路を確かめる

```bash
"$SKILL"/scripts/remote.sh hop1,target true | od -c | head
```

- 何も表示されなければ問題ありません。
- 何か表示される場合は、接続先か中継サーバーのログインのシェルが余計な出力をしています。MCP の通信が壊れるため、その出力を非対話のときに出さないよう利用者に伝えて止めます。bash では、`.bashrc` の先頭に `case $- in *i*) ;; *) return;; esac` を置くのが一般的です。
- 認証で失敗する場合 (`Permission denied`、`Host key verification failed`) は、パスワードなしで各段に入れるようにする必要があります。利用者に伝えて止めます。

### 3. 接続先の種類を調べる

```bash
"$SKILL"/scripts/detect.sh hop1,target
```

出力 (例: `linux-amd64`) を、次の手順の `TYPE` に使います。  
`detect.sh` が判定できない場合 (Windows で sshd の既定のシェルが PowerShell の場合など) は、利用者に OS とアーキテクチャを尋ねます。  
`$SKILL/bin/` に対応する実行ファイルがあることも確かめます。なければ [フォルダの構成](#フォルダの構成) に従って用意します。

### 4. 配置する

接続先にファイルを書き込むため、実行する前に、経路、送る版 (`bin/VERSION`) と種類、配置先を示して利用者の了承を得ます。

```bash
"$SKILL"/scripts/deploy.sh hop1,target linux-amd64 /opt/host-shell/host-shell
```

- `up to date` は、同じ内容がすでに配置されていたことを示します。送っていません。
- `deployed` は、送って SHA-256 が一致したことを示します。
- `SHA-256 mismatch` の場合は、配置できていません。Windows では、sshd の既定のシェルが cmd でない可能性があります。`deploy.sh` は、cmd から `powershell -Command "..."` を呼ぶ形で送ります。
- Windows で、実行中の `host-shell.exe` は置き換えられません。接続中のエージェントのセッションを閉じてから再度実行します。

### 5. 起動を確かめる

```bash
"$SKILL"/scripts/remote.sh hop1,target '/opt/host-shell/host-shell --version'
"$SKILL"/scripts/probe.sh hop1,target /opt/host-shell/host-shell
```

`probe.sh` が `OK:` と、`sys_info` などのツールの名前を出力すれば、エージェントから使える状態です。  
登録するときに付けるオプションがあれば、`probe.sh` の後ろにも同じものを付けて確かめます。  
`NG:` の場合は、表示された理由に従います。経路が遅い場合は `PROBE_WAIT=15` を付けて再度確かめます。  
`open audit log` のエラーの場合は、操作ログの保存先に書き込めません。`--log-file` で書き込める場所を指定するか、記録が不要なら `--no-log` を付けます。

### 6. 登録のコマンドを示す

エージェントへの登録の設定は、利用者が管理するものです。コマンドと設定を示し、実行するかどうかは利用者に任せます。利用者が実行を求めた場合だけ実行します。

各段に `-T` を付けた ssh の入れ子を、そのまま MCP サーバーのコマンドにします。

Claude Code:

```bash
claude mcp add host-shell-target -- ssh -T -o BatchMode=yes hop1 ssh -T -o BatchMode=yes target /opt/host-shell/host-shell --mcp
```

VS Code (`.vscode/mcp.json`) など、JSON で設定するエージェント:

```json
{
  "servers": {
    "host-shell-target": {
      "type": "stdio",
      "command": "ssh",
      "args": ["-T", "-o", "BatchMode=yes", "hop1", "ssh", "-T", "-o", "BatchMode=yes", "target", "/opt/host-shell/host-shell", "--mcp"]
    }
  }
}
```

登録したあとは、エージェントが MCP サーバーを読み込み直す必要があります (Claude Code では `/mcp` で再接続するか、起動し直す)。  
読み込まれたら、`sys_info` を呼んで、接続先のホスト名と OS が返ることを確かめるよう伝えます。

## 報告

最後に、次をまとめて利用者に伝えます。

- 経路と接続先の種類
- 配置先、版、SHA-256、送ったか (`deployed`) 既存のままか (`up to date`)
- `probe.sh` の結果 (バージョンとツールの数)
- 登録のコマンドと、読み込み直しが必要なこと
