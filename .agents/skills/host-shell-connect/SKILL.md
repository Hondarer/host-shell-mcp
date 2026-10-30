---
name: host-shell-connect
description: host-shell (SSH 接続先で動作する MCP サーバー) を接続先に配置し、起動確認とエージェント登録コマンドの作成を行う。「host-shell を target に入れて」「hop1 経由で target を MCP で操作できるようにして」「host-shell を更新して」のように、接続先マシンへの host-shell 導入・更新時に使用する。Set up, deploy, update, or verify host-shell on a remote host over SSH.
---

# host-shell の接続準備

接続先マシンに `host-shell` を配置し、SSH 経路上で MCP サーバーとして起動できることを確認したうえで、エージェントへの登録コマンドを作成します。  
MCP サーバーの起動自体は登録後にエージェントが行うため、このスキルでは起動しません。

このスキルは単一ディレクトリで完結します。以降、`SKILL.md` があるディレクトリを `$SKILL` と表記します (リポジトリ内では `.agents/skills/host-shell-connect`)。

## フォルダーの構成

| パス | 内容 |
|---|---|
| `scripts/` | 経路確認、環境判定、配置、起動確認スクリプト |
| `bin/host-shell-linux-amd64`、`bin/host-shell-linux-arm64` | Linux 用実行ファイル |
| `bin/host-shell-windows-amd64.exe`、`bin/host-shell-windows-arm64.exe` | Windows 用実行ファイル |
| `bin/host-shell-darwin-amd64`、`bin/host-shell-darwin-arm64` | macOS 用実行ファイル (ローカルで `host-shell bridge` として使用) |
| `bin/VERSION` | 実行ファイルのバージョン |

Table: フォルダーの構成

`bin/` はリポジトリのルートで `make dist` を実行すると生成されます (`dist/` の複製)。  
`bin/` が存在しない場合や最新バージョンが必要な場合は、リポジトリのルートで `make dist` を実行します (Go 1.26 以降が必要)。  
リポジトリがない環境では、GitHub Release の `host-shell-mcp-<version>-bin.zip` (または `.tar.gz`) を展開した `host-shell-mcp-<version>/.agents/skills/host-shell-connect/` を使用します (アーカイブの取得は利用者に依頼します)。

## スクリプト

いずれのスクリプトも SFTP とポート転送を使用しません。

| スクリプト | 内容 |
|---|---|
| `remote.sh ROUTE COMMAND` | 中継サーバーを経由する `ssh` の入れ子と引用符を組み立てて接続先でコマンドを実行し、標準入力を接続先に渡す |
| `detect.sh ROUTE` | 接続先の環境種別 (`linux-amd64`、`linux-arm64`、`windows-amd64`、`windows-arm64`) を出力する |
| `deploy.sh ROUTE TYPE [REMOTE_PATH]` | `TYPE` に対応する `bin/` の実行ファイルを `ssh` の標準入力経由で転送し、SHA-256 照合と起動確認を行う。同一ファイルが配置済みなら転送せず、起動できなければ転送したファイルを削除する。`REMOTE_PATH` 省略時は既定の配置先を順に試す |
| `probe.sh ROUTE REMOTE_PATH [OPTIONS...]` | 接続先の `host-shell` に MCP 初期化とツール一覧取得リクエストを送り、応答を確認する |

Table: スクリプト

`ROUTE` には、経由ホストと接続先をローカルに近い順でカンマ区切りに指定します (例: `hop1,target`、直接接続なら `target`)。  
ホスト名には `~/.ssh/config` の定義名も使用できます。各段は `-T -o BatchMode=yes` で接続します (`HOST_SHELL_SSH_OPTS` で変更可能)。  
中継サーバーのシェルは POSIX sh 互換を前提とします。

## 手順

### 必要な情報の確認

利用者の依頼から次の項目を決定し、不明な項目のみ利用者に確認します。

| 項目 | 例 | 既定値 |
|---|---|---|
| 経路 (`ROUTE`) | `hop1,target` | なし (必須確認) |
| 配置先 (`REMOTE_PATH`) | `/tmp/host-shell-1000/host-shell` | 省略 (`deploy.sh` が既定の候補を順に試す) |
| 登録名 | `host-shell-target` | `host-shell-<接続先ホスト名>` |
| `host-shell` のオプション | `--timeout 2m`、`--windows-shell cmd`、制限オプション | なし (制限なし) |
| `bridge` の有無 | ローカル・接続先間でファイルを転送する場合に使用 | 使用する |

Table: 必要な情報

利用者が制限を求めた場合は、次のオプションから選択します (指定がなければ制限を付けません)。

| 要件 | オプション |
|---|---|
| 読み取り専用にする | `--read-only` |
| 公開ツールを絞る | `--tools files,search` (グループは `files`、`search`、`exec`、`net`、`admin`。`all,-admin` のように除外指定も可能) |
| 操作対象パスを制限する | `--allow-path /srv/app`、`--allow-read-path /var/log` |
| 実行可能プログラムを制限する | `--allow-command git,make,go` |
| 変更前に利用者が確認する | `--confirm destructive` (クライアントの elicitation 対応が必要) |
| ネットワーク接続先を制限する | `--allow-host localhost,10.0.0.0/8` |
| 機密情報を出力から伏せる | `--redact`、`--unset-env 'AWS_*,*TOKEN*'` |

Table: 制限オプション

#### 配置先

接続先のシステム領域を変更しないよう、接続ユーザー所有の一時領域またはホーム ディレクトリ配下に配置します。`deploy.sh` で `REMOTE_PATH` を省略すると、次の順に配置と起動確認を試みます。

| 接続先 | 1. 一時ディレクトリ | 2. ホーム ディレクトリ |
|---|---|---|
| Linux | `/tmp/host-shell-<uid>/host-shell` | `.local/bin/host-shell` (ホームからの相対パス) |
| Windows | `%TEMP%/host-shell/host-shell.exe` | `%LOCALAPPDATA%/Programs/host-shell/host-shell.exe` |

Table: 既定の配置先

- `/tmp` が `noexec` マウントされている場合、実行制限 (AppLocker など) で起動できない場合、またはディレクトリの権限が安全でない場合は、2 番目の候補へフォールバックします。
- `/tmp` 配下のディレクトリは、他利用者によるバイナリ差し替えを防ぐため権限 `700` で作成します。シンボリック リンク、他利用者所有、グループ・他利用者に書き込み権限があるディレクトリは使用しません。
- `/opt`、`/usr/local`、`C:/` 直下などのシステム領域には、利用者が明示的に指定した場合のみ配置します (`sudo` は使用しません)。
- `/tmp` や `%TEMP%` の内容は再起動や定期削除 (`systemd-tmpfiles` など) で消去されることがあります。消去された場合は同じ手順を再実行して再配置します (エージェント側の登録は変更不要です)。
- Linux で `/tmp` の配置先が消去されている間は、接続先の他利用者が同名ディレクトリを作成して偽の実行ファイルを配置できるため、信頼できない利用者がログインする環境では `.local/bin/host-shell` を明示指定します (`deploy.sh ROUTE TYPE .local/bin/host-shell`)。
- 配置先には空白や引用符を含まないパスを使用します。Windows で `%TEMP%` などに空白が含まれる場合、その候補はスキップされます。
- `.local/bin/host-shell` は、sshd がホーム ディレクトリを作業ディレクトリとしてコマンドを起動するため相対パスのまま登録できます (`~` はローカル シェルに展開されるため使用しません)。

### SSH 経路の確認

```bash
"$SKILL"/scripts/remote.sh hop1,target true | od -c | head
```

- 何も出力されなければ正常です。
- 出力がある場合は、接続先または中継サーバーのシェル初期化ファイルが非対話時にも標準出力へ書き込んでおり、MCP 通信が破損します。非対話時は出力しないよう初期化ファイルの修正を利用者に依頼して中断します (bash では `.bashrc` 先頭に `case $- in *i*) ;; *) return;; esac` を記述します)。
- 認証エラー (`Permission denied`、`Host key verification failed`) が発生した場合は、パスワード入力なしで各段を通過できるよう設定を利用者に依頼して中断します。

### 接続先環境の判定

```bash
"$SKILL"/scripts/detect.sh hop1,target
```

出力 (例: `linux-amd64`) を次の手順の `TYPE` に指定します。  
`detect.sh` で判定できない場合 (Windows で sshd の既定シェルが PowerShell の場合など) は、利用者に OS とアーキテクチャーを確認します。  
あわせて `$SKILL/bin/` に対応する実行ファイルが存在することを確認し、なければ [フォルダーの構成](#フォルダーの構成) に従って用意します。

### バイナリの配置

接続先へファイルを書き込む前に、経路、転送するバージョン (`bin/VERSION`)、環境種別、配置先を利用者に提示して了承を得ます。

```bash
"$SKILL"/scripts/deploy.sh hop1,target linux-amd64
```

末尾に出力される `deployed: <配置先>` または `up to date: <配置先>` のパスを、以降の手順と登録に使用します (以降の例では `/tmp/host-shell-1000/host-shell`)。  
配置先を明示する場合は第 3 引数に指定します。

- `up to date`: 同一内容のファイルが配置済みのため転送を省略したことを示します。
- `deployed`: 転送と SHA-256 照合が完了したことを示します。
- `could not install`: そのパスへ配置できなかったことを示します (既定候補の試行時は次の候補へ進みます)。Windows で全候補が失敗する場合は、sshd の既定シェルが `cmd` でない可能性があります (`deploy.sh` は `cmd` から `powershell -Command "..."` を呼び出して転送します)。
- `cannot run`: 配置後に起動できなかったため、転送したファイルを削除したことを示します。
- `no default location worked`: 利用者所有の配置先を確認し、第 3 引数に指定して再実行します。
- Windows では実行中の `host-shell.exe` を上書きできません。接続中のエージェント セッションを終了してから再実行します。

### 起動の確認

```bash
"$SKILL"/scripts/remote.sh hop1,target '/tmp/host-shell-1000/host-shell --version'
"$SKILL"/scripts/probe.sh hop1,target /tmp/host-shell-1000/host-shell
```

`probe.sh` が `OK:` と `sys_info` などのツール名を出力すれば利用可能です。  
登録時に指定するオプションがある場合は、`probe.sh` の末尾にも同じオプションを渡して確認します。  
`NG:` が出力された場合はメッセージに従って対処します。経路の遅延が大きい場合は `PROBE_WAIT=15` を指定して再試行します。  
`open audit log` エラーが発生した場合は、操作ログの出力先に書き込めないため、`--log-file` で書き込み可能なパスを指定するか `--no-log` を指定します。

### 登録コマンドの提示

エージェントへの登録設定は利用者が管理するため、コマンドと設定例を提示し、利用者から実行を求められた場合のみ実行します。

各段に `-T` を付けた `ssh` の入れ子が接続先の `host-shell` を起動するコマンドです。  
既定ではその手前にローカルの `host-shell bridge` を挟み、接続先ツールの中継に加えてファイル転送ツール (`transfer_push`、`transfer_pull`) を有効にします。

ローカルの実行ファイルには、`$SKILL/bin/` のうちローカルの OS・アーキテクチャーに対応するバイナリ (`uname -sm` で確認。Linux x86_64 は `host-shell-linux-amd64`、macOS arm64 は `host-shell-darwin-arm64`、Windows は `.exe`) を絶対パスで指定します。スキル フォルダーを更新しても登録パスが変わらないよう、必要に応じて `~/.local/bin/host-shell` などへ複製してから指定します。

Claude Code (`bridge` あり):

```bash
claude mcp add host-shell-target -- /path/to/host-shell bridge -- ssh -T -o BatchMode=yes hop1 ssh -T -o BatchMode=yes target /tmp/host-shell-1000/host-shell --mcp
```

Claude Code (`bridge` なし):

```bash
claude mcp add host-shell-target -- ssh -T -o BatchMode=yes hop1 ssh -T -o BatchMode=yes target /tmp/host-shell-1000/host-shell --mcp
```

VS Code (`.vscode/mcp.json`) など JSON 設定のエージェント:

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

制限オプションは接続先の `--mcp` の後ろに追加します。ローカル側の転送許可パスを制限する場合は、`bridge` の後ろに `--allow-local-path DIR` を追加します。

登録後はエージェント側で MCP サーバーの再読み込み (Claude Code では `/mcp` での再接続または再起動) を行い、`sys_info` で接続先のホスト名と OS が取得できることを確認するよう案内します。

## アンインストール

利用者から `host-shell` の削除を求められた場合は、配置先ディレクトリ (`/tmp/host-shell-<uid>/`、`%TEMP%/host-shell/` など。`.local/bin` の場合は `host-shell` ファイル単体) を削除します。  
操作ログ (Linux は `~/.local/state/host-shell/`、Windows は `%LOCALAPPDATA%/host-shell/`) の削除要否は利用者に確認し、エージェント側の登録解除 (Claude Code では `claude mcp remove <名前>`) を案内します。

## 報告

作業完了時に次の項目をまとめて報告します。

- 経路と接続先の環境種別
- 配置先、バージョン、SHA-256、新規配置 (`deployed`) か既存維持 (`up to date`) か
- `probe.sh` の確認結果 (バージョンと公開ツール数)
- 登録コマンド (`bridge` の有無、指定した制限オプション) と MCP サーバーの再読み込み手順
