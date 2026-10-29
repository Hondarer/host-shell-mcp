# host-shell-mcp

`host-shell` は、**接続先のマシン (Windows / Linux) に配置し、SSH などの標準入出力で中継して使う** MCP サーバーです。  
ポート転送 (`-L`、`-R`、`-D`、ProxyJump) も SFTP も使えない環境で、クライアントのコーディング エージェント (Claude Code、GitHub Copilot など) が、プレーンな SSH だけを経由して遠隔のマシンを調査、操作できるようにします。

手元で `ssh` を呼ぶ既存の SSH 系 MCP サーバーとは仕組みが逆で、`host-shell` 自身は接続先で動きます。  
設計の経緯は [docs/host-shell-mcp-design.md](docs/host-shell-mcp-design.md) を参照してください。

```text
エージェント --stdio(MCP)--> ssh -T hop1 ssh -T hop2 ssh -T target host-shell --mcp
```

## 状態

スケルトンです。主要なツールは動作しますが、[未決事項](#未決事項) と、ソース中の `TODO` が残っています。

## ビルド

Go 1.25 以降が必要です。`CGO_ENABLED=0` で、C ライブラリに依存しない単体の実行ファイルを作ります。

```bash
make dist   # dist/host-shell-{linux,windows}-{amd64,arm64}[.exe]
make test   # テスト
make vet    # Linux と Windows の両方を対象に go vet
```

## 配置

最終接続先に実行ファイルを 1 つ置くだけです (例: `/opt/host-shell/host-shell`、`C:/tools/host-shell.exe`)。  
転送手段がない場合は、`ssh target 'cat > host-shell' < dist/host-shell-linux-amd64` のように SSH のセッションで送れます。

注意点:

- 各段で `-T` を指定し、疑似端末を割り当てないでください。改行の変換やエコーで JSON-RPC の通信が壊れます。
- 接続先のシェル初期化ファイル (`.bashrc` など) が、非対話のときに標準出力へ何かを出力しないことを確認してください。
- 各中継サーバー上に、次の段へ接続するための鍵などの認証手段が必要です。

## エージェントへの登録

VS Code (`.vscode/mcp.json`):

```json
{
  "servers": {
    "host-shell": {
      "type": "stdio",
      "command": "ssh",
      "args": ["-T", "hop1", "ssh", "-T", "hop2", "ssh", "-T", "target", "C:/tools/host-shell.exe", "--mcp"]
    }
  }
}
```

Claude Code:

```bash
claude mcp add host-shell -- ssh -T hop1 ssh -T hop2 ssh -T target /opt/host-shell/host-shell --mcp
```

段数が多い場合や段ごとに認証方式が違う場合は、手元に接続用のラッパー スクリプトを置き、それを指定してください。

## オプション

| オプション | 既定値 | 内容 |
|---|---|---|
| `--mcp` | | 標準入出力で MCP サーバーとして動く (必須) |
| `--log-file` | Linux: `~/.local/state/host-shell/audit.log`<br>Windows: `%LOCALAPPDATA%\host-shell\audit.log` | 操作ログ (JSON Lines) の出力先 |
| `--no-log` | | 操作ログを記録しない |
| `--max-output` | `65536` | 1 つの出力の上限 (バイト)。超えた場合は先頭と末尾を残して切り詰める |
| `--timeout` | `60s` | コマンドのタイムアウトの既定値 |
| `--max-timeout` | `30m` | エージェントが指定できるタイムアウトの上限 |
| `--windows-shell` | `powershell` | Windows でのシェルの既定値 (`cmd`、`powershell`、`pwsh`) |
| `--version` | | バージョンを表示して終了する |

標準出力は MCP の JSON-RPC 専用です。診断メッセージは標準エラーに出力します。

## ツール

| 分類 | ツール | 内容 |
|---|---|---|
| コマンド実行 | `exec_run` | シェルのコマンド行 (`command`) または引数列 (`argv`) を、作業ディレクトリ、環境変数、標準入力、タイムアウトを指定して実行する |
| 長時間の実行 | `job_start`、`job_read`、`job_stop`、`job_list` | バックグラウンドで実行し、出力を位置指定で少しずつ取得する。`job_read` は新しい出力を待てる |
| ファイル操作 | `fs_list`、`fs_stat`、`fs_read`、`fs_write` | 行または範囲を指定した読み取り、base64 によるバイナリの読み書き |
| 検索 | `search_grep`、`search_files` | 正規表現 (RE2) による全文検索、glob によるファイル名の検索 |
| 差分の確認 | `fs_manifest` | パス、サイズ、SHA-256 の一覧。手元のソースと比べ、差分のあるファイルだけを読む |
| 特権実行 | `sudo_run` | `sudo -n` による実行 (Linux のみ。`NOPASSWD` の sudoers 規則が必要) |
| 環境情報 | `sys_info`、`sys_processes` | OS、ホスト名、ユーザー、既定のシェル、プロセス一覧 |

接続先を変更しうるツールには `destructiveHint`、読み取りだけのツールには `readOnlyHint` の注釈を付けています。  
エージェント側で、書き込みや特権実行のツールに承認を必須にする設定の目安にしてください。

Windows の PowerShell には、引用符の解釈の違いを避けるため、コマンドを `-EncodedCommand` で渡し、出力を UTF-8 にそろえます。  
タイムアウトや停止のときは、孫プロセスを含めて終了させます (Linux はプロセス グループ、Windows は `taskkill /T`)。

## 安全性

`host-shell` は任意のコマンドを実行できます。`authorized_keys` の `command="..."` や `ForceCommand` で起動を固定しても、権限を制限する効果はありません。  
次の組み合わせで安全性を確保してください。

- 専用の鍵と専用のユーザーを使い、ユーザーの権限と sudoers の規則で操作の範囲を決める
- `host-shell` の操作ログ (すべてのツール呼び出し) を確認する。書き込み内容と標準入力は、長さとハッシュだけを記録する
- エージェント側で、書き込みや特権実行のツールに承認を必須にする

## 構成

```text
cmd/host-shell/        エントリ ポイント (フラグの解析、stdio での起動)
internal/server/       MCP サーバーの組み立て
internal/tools/        各ツールの実装
internal/proc/         子プロセスの起動 (OS ごとのシェルとプロセス ツリーの終了)
internal/jobs/         長時間ジョブの管理
internal/limit/        出力サイズの上限 (先頭と末尾を残す切り詰め)
internal/audit/        操作ログ
internal/config/       設定
docs/                  設計メモ
```

## 未決事項

- 接続先に Windows 10、Windows Server 2016 より古い OS が含まれるか
- Windows の接続先で、コマンド実行の既定を cmd と PowerShell のどちらにするか (現状は PowerShell)
- sudo を `NOPASSWD` だけに限るか、`sudo -S` にも対応するか (現状は `sudo -n` のみ)
- 接続先での操作ログの保存先と保持期間 (現状はローテーションなし)
- SSH の切断時に、実行中の長時間ジョブを停止するか継続するか (現状は停止する)

## 名前について

同名の [funkyfunc/host-shell-mcp](https://github.com/funkyfunc/host-shell-mcp) は、VM の中のエージェントから利用者自身の Mac でコマンドを実行させるもので、目的が異なります。  
このリポジトリの `host` は「SSH の先にある接続先のマシン」を指します。
