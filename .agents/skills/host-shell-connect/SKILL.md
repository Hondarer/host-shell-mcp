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
| `bin/host-shell-darwin-amd64`、`bin/host-shell-darwin-arm64` | macOS 用の実行ファイル (手元で `host-shell bridge` として使う) |
| `bin/VERSION` | 実行ファイルのバージョン |

Table: フォルダの構成

`bin/` は、host-shell-mcp のリポジトリで `make dist` を実行すると作られます (`dist/` と同じものの複製)。  
`bin/` がない場合や、利用者が新しい版を求めている場合は、リポジトリのルートで `make dist` を実行します (Go 1.26 以降が必要)。  
リポジトリがない環境では、GitHub の Release の `host-shell-connect-<tag>.zip` (または `.tar.gz`) を使います。`bin/` を含むこの skill のフォルダが入っています。用意は利用者に頼みます。

## スクリプト

いずれも SFTP とポート転送を使いません。

| スクリプト | 内容 |
|---|---|
| `remote.sh ROUTE COMMAND` | 接続先でコマンドを実行する。中継サーバーを経由する ssh の入れ子と引用符を組み立てる。標準入力は接続先に渡す |
| `detect.sh ROUTE` | 接続先の種類 (`linux-amd64`、`linux-arm64`、`windows-amd64`、`windows-arm64`) を出力する |
| `deploy.sh ROUTE TYPE [REMOTE_PATH]` | `bin/` の実行ファイルのうち `TYPE` に合うものを ssh の標準入力で送り、SHA-256 を照合し、接続先で起動できることを確かめる。同じ内容がすでにあれば送らない。起動できなければ送ったものを消す。`REMOTE_PATH` を省くと、既定の配置先を順に試す |
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
| 配置先 (`REMOTE_PATH`) | `/tmp/host-shell-1000/host-shell` | 指定しない (`deploy.sh` が下記の順に試す) |
| 登録の名前 | `host-shell-target` | `host-shell-<接続先のホスト名>` |
| host-shell のオプション | `--timeout 2m`、`--windows-shell cmd`、制限 (下記) | なし (制限なし) |
| bridge を使うか | 手元と接続先の間でファイルを送るなら使う | 使う |

Table: 必要な情報

利用者が制限を求めた場合は、次のオプションから選びます。求められなければ付けません (既定は制限なしで、何でもできます)。

| 求め | オプション |
|---|---|
| 読むだけにしたい | `--read-only` |
| 使うツールを絞りたい | `--tools files,search` (組は files、search、exec、net、admin。`all,-admin` のように除くこともできる) |
| 操作できる場所を限りたい | `--allow-path /srv/app`、`--allow-read-path /var/log` |
| 実行できるプログラムを限りたい | `--allow-command git,make,go` |
| 変更の前に確認したい | `--confirm destructive` (クライアントが elicitation に対応している必要がある) |
| 接続先を限りたい | `--allow-host localhost,10.0.0.0/8` |
| 秘密を出力に出したくない | `--redact`、`--unset-env 'AWS_*,*TOKEN*'` |

#### 配置先

接続先のシステムを汚さないよう、利用者のものである一時的な場所に置きます。`deploy.sh` は `REMOTE_PATH` を省くと、次の順に試し、置けて起動できた最初の場所を使います。

| 接続先 | 1. 一時的な場所 | 2. 利用者のホーム ディレクトリ |
|---|---|---|
| Linux | `/tmp/host-shell-<uid>/host-shell` | `.local/bin/host-shell` (ホーム ディレクトリからの相対パス) |
| Windows | `%TEMP%/host-shell/host-shell.exe` | `%LOCALAPPDATA%/Programs/host-shell/host-shell.exe` |

Table: 既定の配置先

- 2 番目に回るのは、`/tmp` が noexec でマウントされている、実行の制限 (AppLocker など) で起動できない、ディレクトリが安全でない、などの場合です。
- `/tmp` のディレクトリは、ほかの利用者が実行ファイルを差し替えられないよう、権限を 700 で作ります。シンボリック リンク、ほかの利用者が持ち主のもの、グループやほかの利用者が書き込めるものは使いません。
- `/opt`、`/usr/local`、`C:/` の直下などのシステムの場所には、利用者が明示的に求めた場合だけ置きます。`sudo` は使いません。
- `/tmp` と `%TEMP%` の中身は、再起動や定期的な掃除 (systemd-tmpfiles など) で消えることがあります。消えた場合は、この手順をもう一度実行すれば置き直せます (登録はそのまま使えます)。
- Linux の `/tmp` の配置先が消えている間は、接続先のほかの利用者が同じ名前のディレクトリを作り、偽の実行ファイルを置けます。登録したコマンドはそれを利用者の権限で起動してしまいます。信頼できない利用者もログインできる接続先では、`.local/bin/host-shell` を指定します (`deploy.sh ROUTE TYPE .local/bin/host-shell`)。
- 配置先は、空白と引用符を含まないパスにします。Windows で `%TEMP%` などが空白を含む場合は、その候補を飛ばします。
- `.local/bin/host-shell` は、sshd がホーム ディレクトリでコマンドを始めるため、相対パスのまま登録できます。`~` は手元のシェルが展開してしまうため使いません。

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

接続先にファイルを書き込むため、実行する前に、経路、送る版 (`bin/VERSION`) と種類、配置先 (既定なら上の表の順に試すこと) を示して利用者の了承を得ます。

```bash
"$SKILL"/scripts/deploy.sh hop1,target linux-amd64
```

最後の行の `deployed: <配置先>` または `up to date: <配置先>` の配置先を、以降の手順と登録に使います。以降の例では `/tmp/host-shell-1000/host-shell` とします。  
配置先を指定する場合は、3 番目の引数に付けます。

- `up to date` は、同じ内容がすでに配置されていたことを示します。送っていません。
- `deployed` は、送って SHA-256 が一致したことを示します。
- `could not install` は、その場所に置けなかったことを示します。既定の配置先を試している場合は、次の候補に進みます。Windows ですべて失敗する場合は、sshd の既定のシェルが cmd でない可能性があります。`deploy.sh` は、cmd から `powershell -Command "..."` を呼ぶ形で送ります。
- `cannot run` は、置けたが起動できなかったことを示します。送ったものは消しています。
- `no default location worked` の場合は、利用者に、自分のものである置き場所を尋ねて、3 番目の引数に指定します。
- Windows で、実行中の `host-shell.exe` は置き換えられません。接続中のエージェントのセッションを閉じてから再度実行します。

### 5. 起動を確かめる

```bash
"$SKILL"/scripts/remote.sh hop1,target '/tmp/host-shell-1000/host-shell --version'
"$SKILL"/scripts/probe.sh hop1,target /tmp/host-shell-1000/host-shell
```

`probe.sh` が `OK:` と、`sys_info` などのツールの名前を出力すれば、エージェントから使える状態です。  
登録するときに付けるオプションがあれば、`probe.sh` の後ろにも同じものを付けて確かめます。  
`NG:` の場合は、表示された理由に従います。経路が遅い場合は `PROBE_WAIT=15` を付けて再度確かめます。  
`open audit log` のエラーの場合は、操作ログの保存先に書き込めません。`--log-file` で書き込める場所を指定するか、記録が不要なら `--no-log` を付けます。

### 6. 登録のコマンドを示す

エージェントへの登録の設定は、利用者が管理するものです。コマンドと設定を示し、実行するかどうかは利用者に任せます。利用者が実行を求めた場合だけ実行します。

各段に `-T` を付けた ssh の入れ子が、接続先の host-shell を起動するコマンドです。
既定では、その前に手元の `host-shell bridge` を置きます。bridge は接続先のツールをそのまま中継し、手元と接続先の間でファイルを送るツール (`transfer_push`、`transfer_pull`) を加えます。

手元の実行ファイルは `$SKILL/bin/` のうち、手元の OS とアーキテクチャに合うものです (`uname -sm` で調べる。Linux x86_64 なら `host-shell-linux-amd64`、macOS arm64 なら `host-shell-darwin-arm64`、Windows なら `.exe`)。
登録には絶対パスを使います。skill のフォルダを更新しても登録が変わらないよう、利用者が望めば `~/.local/bin/host-shell` などに複製してから使います。

Claude Code (bridge を使う):

```bash
claude mcp add host-shell-target -- /path/to/host-shell bridge -- ssh -T -o BatchMode=yes hop1 ssh -T -o BatchMode=yes target /tmp/host-shell-1000/host-shell --mcp
```

Claude Code (bridge を使わない):

```bash
claude mcp add host-shell-target -- ssh -T -o BatchMode=yes hop1 ssh -T -o BatchMode=yes target /tmp/host-shell-1000/host-shell --mcp
```

VS Code (`.vscode/mcp.json`) など、JSON で設定するエージェント:

```json
{
  "servers": {
    "host-shell-target": {
      "type": "stdio",
      "command": "/path/to/host-shell",
      "args": ["bridge", "--", "ssh", "-T", "-o", "BatchMode=yes", "hop1", "ssh", "-T", "-o", "BatchMode=yes", "target", "/tmp/host-shell-1000/host-shell", "--mcp"]
    }
  }
}
```

制限のオプションは、接続先の host-shell の `--mcp` の後ろに付けます。手元で転送してよい場所を限る場合は、`bridge` の後ろに `--allow-local-path DIR` を付けます。

登録したあとは、エージェントが MCP サーバーを読み込み直す必要があります (Claude Code では `/mcp` で再接続するか、起動し直す)。  
読み込まれたら、`sys_info` を呼んで、接続先のホスト名と OS が返ることを確かめるよう伝えます。

## 取り除く

利用者が host-shell を取り除くよう求めた場合は、配置先のディレクトリ (`/tmp/host-shell-<uid>/`、`%TEMP%/host-shell/` など。`.local/bin` の場合は `host-shell` のファイルだけ) を消します。  
操作ログを残す設定の場合は、ログ (Linux は `~/.local/state/host-shell/`、Windows は `%LOCALAPPDATA%/host-shell/`) も消してよいか利用者に尋ねます。  
エージェントの登録は、利用者に外してもらいます (Claude Code では `claude mcp remove <名前>`)。

## 報告

最後に、次をまとめて利用者に伝えます。

- 経路と接続先の種類
- 配置先、版、SHA-256、送ったか (`deployed`) 既存のままか (`up to date`)
- `probe.sh` の結果 (バージョンとツールの数)
- 登録のコマンド (bridge を使うか、付けた制限) と、読み込み直しが必要なこと
