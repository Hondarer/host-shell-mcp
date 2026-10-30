# host-shell-mcp

[English](#english) | [日本語](#日本語)

## English

`host-shell` is an MCP server **deployed on the target machine (Windows / Linux) and accessed over the standard input and output of SSH or a similar channel**.  
Even where port forwarding and SFTP are unavailable, coding agents such as Claude Code and GitHub Copilot can inspect and operate remote machines over a plain SSH session.

```text
agent --stdio(MCP)--> ssh -T hop1 ssh -T target host-shell --mcp
```

Unlike typical SSH MCP servers that invoke `ssh` per operation on the client side, `host-shell` runs directly on the target machine.

| | Client-side SSH MCP | host-shell |
|---|---|---|
| Where it runs | On the machine that runs the agent | On the target machine |
| SFTP | Usually used for file transfer | Not used |
| Port forwarding | Often required for tunnels and some transfers | Not required |
| `AllowTcpForwarding no` | Routes that depend on forwarding stop there | Nested `ssh -T` still reaches the target |
| Windows and encodings | Quoting and encodings stay with the caller | Normalized on the target (PowerShell quoting, Shift_JIS, EUC-JP) |
| Session | Often opens a new SSH connection per call | Keeps one SSH connection for the whole MCP session |

Table: Comparison with client-side SSH MCP servers

### Features

- **No port forwarding or SFTP required**: Operates solely over the standard input and output of an SSH session. Multi-hop routes work via nested `ssh` even when jump hosts set `AllowTcpForwarding no`.
- **Persistent single session**: Maintains one SSH connection for the entire MCP session, avoiding per-call connection overhead even across multi-hop routes.
- **Cross-platform abstraction**: Normalizes shell, quoting, and path differences between Windows and Linux. PowerShell commands can be passed without extra quote escaping.
- **Built-in agent primitives**: Provides string replacement (`fs_edit`), `**` globs, grep with context lines and match counts, file move/copy/delete, and input to interactive jobs.
- **Pseudo-terminal (PTY) support**: Runs terminal-dependent programs (such as password-prompting `sudo`/`ssh` and REPLs) as `pty` jobs (Linux pty / Windows ConPTY), supporting regex match (`until`) and output-idle (`idle_ms`) waits.
- **Network diagnostics and requests**: Supports HTTP requests from the target (`net_request_http`, including the Docker API over Unix sockets), TCP send/receive (`net_exchange`), DNS/connectivity/certificate checks (`net_check`), and listening port inspection (`net_list_listeners`).
- **Unified OS administration**: Manages services (systemd / Windows services), system logs (journald / Event Log), process termination, disk usage, and the Windows registry through a consistent interface.
- **Direct file transfer**: With local `host-shell bridge`, transfers files and directories directly without passing through the agent context (`transfer_push`, `transfer_pull`), verifying SHA-256 hashes and skipping unchanged files.
- **Progress notifications and cancellation**: Reports progress for long commands, downloads, and file transfers via MCP notifications, and terminates execution when the agent cancels a request.
- **Policy restrictions via startup options**: Runs with the connecting user's permissions by default; startup options can restrict published tool groups, enforce read-only mode, limit allowed paths/commands/hosts, require user confirmation, and mask secrets.
- **Self-describing agent guidance**: Initialize instructions, tool descriptions, and the `help_get` tool allow agents to choose appropriate tools without user instructions.
- **Automatic encoding and newline conversion**: Converts Shift_JIS, EUC-JP, and other encodings to UTF-8 on the target, and supports specifying encoding, BOM, and line endings when writing.
- **Single-binary deployment**: Deploys as a single static executable with no external dependencies.
- **Output limits, timeouts, and audit logging**: Truncates large outputs while preserving head and tail, and logs every tool call on the target.

### Tools

| Category | Tools |
|---|---|
| Usage guidance | `help_get` |
| Command execution | `exec_run` |
| Long-running jobs | `job_start`, `job_read`, `job_write`, `job_stop`, `job_list` |
| File operations | `fs_list`, `fs_stat`, `fs_read`, `fs_edit`, `fs_write`, `fs_mkdir`, `fs_move`, `fs_copy`, `fs_remove` |
| Search | `search_grep`, `search_find` |
| Diff inspection | `fs_manifest` |
| Privileged execution | `exec_sudo` (Linux only) |
| Environment info | `sys_get_info` |
| Network | `net_request_http`, `net_exchange`, `net_check`, `net_list_listeners`, `net_list_interfaces` |
| Administration | `proc_list`, `proc_kill`, `svc_list`, `svc_control`, `log_query`, `disk_list` |
| Windows registry | `reg_read`, `reg_write`, `reg_delete` (Windows only) |
| File transfer | `transfer_push`, `transfer_pull` (via `host-shell bridge`) |

Table: Tools provided

### Quick start

1. Build the binaries (requires Go 1.26 or later).

    ```bash
    make dist
    ```

2. Deploy the executable to the target. It can be transferred over SSH standard input without SFTP. To avoid modifying system directories, place it under `/tmp/host-shell-<uid>/` by default (`1000` is the UID returned by `id -u` on the target).

    ```bash
    ssh target 'mkdir -m 700 /tmp/host-shell-1000 && cat > /tmp/host-shell-1000/host-shell && chmod 755 /tmp/host-shell-1000/host-shell' \
        < dist/host-shell-linux-amd64
    ssh -T target /tmp/host-shell-1000/host-shell --version
    ```

3. Register the server with your agent. Placing the local binary in front as `bridge` enables the file transfer tools (`transfer_push`, `transfer_pull`).

    ```bash
    claude mcp add host-shell -- /path/to/host-shell bridge -- ssh -T hop1 ssh -T target /tmp/host-shell-1000/host-shell --mcp
    ```

    Without `bridge`, register the `ssh` command directly.

    ```bash
    claude mcp add host-shell -- ssh -T hop1 ssh -T target /tmp/host-shell-1000/host-shell --mcp
    ```

4. Ask the agent to "check the target's OS with sys_get_info of host-shell".

Pass `-T` to every SSH hop to disable pseudo-terminal allocation. See the [User Guide](docs/en/user-guide.md) for details.

Steps 2 and 3 can also be automated with the `.agents/skills/host-shell-connect` skill, which verifies the route, detects the target OS and architecture, deploys the binary, checks startup, and outputs the registration command. `make dist` copies the binaries into the skill's `bin/` directory.  
GitHub Releases include archives named with the tag, including the leading `v` (`host-shell-mcp-<tag>-bin.zip` and `.tar.gz`, for example `host-shell-mcp-v20261031.1.0-bin.zip`). Each archive contains the source tree along with prebuilt binaries in `dist/` and `.agents/skills/host-shell-connect/bin/`.

### Documentation

See [`docs/`](docs/README.md). **The Japanese documents (`docs/ja/`) are the master**, and `docs/en/` contains English translations.

| Document | Contents |
|---|---|
| [User Guide](docs/en/user-guide.md) | Architecture, binary deployment, SSH route checks, agent registration, usage examples, troubleshooting |
| [Operations Guide](docs/en/operations.md) | Security model, restriction options, dedicated user and key, pinning the startup command, sudo configuration, audit log management |
| [Reference](docs/en/reference.md) | Startup options, policy restrictions, MCP specification, tool inputs and outputs, bridge, process management, audit log format, limitations |
| [Development Guide](docs/en/development.md) | Building, source layout, design rules, adding tools, testing |

Table: Documentation

### Security

By default, `host-shell` runs with the full permissions of the connecting user.  
Use a dedicated OS user and SSH key, and restrict access via OS file permissions and `sudoers`. You can also narrow published tools and allowed targets with startup options (`--read-only`, `--tools`, `--allow-path`, `--confirm`, etc.).  
See the [Operations Guide](docs/en/operations.md) for details.

## 日本語

`host-shell` は、**接続先マシン (Windows / Linux) に配置し、SSH などの標準入出力経由で中継して使用する** MCP サーバーです。  
ポート転送や SFTP が使えない環境でも、Claude Code や GitHub Copilot などのコーディング エージェントから、プレーンな SSH セッションだけで遠隔マシンを調査・操作できます。

```text
エージェント --stdio(MCP)--> ssh -T hop1 ssh -T target host-shell --mcp
```

クライアント側で `ssh` を起動する一般的な SSH 系 MCP サーバーとは異なり、`host-shell` 自体は接続先マシン上で動作します。

| | クライアント側の SSH 系 MCP | host-shell |
|---|---|---|
| 実行場所 | エージェントと同じマシン | 接続先マシン |
| SFTP | ファイル転送に使うことが多い | 使わない |
| ポート転送 | トンネルや一部の転送に必要になることが多い | 不要 |
| `AllowTcpForwarding no` | 転送に依存する経路はそこで止まる | `ssh -T` の入れ子で接続先まで届く |
| Windows と文字コード | 引用符と文字コードの差は呼び側に残る | 接続先で吸収する (PowerShell の引用符、Shift_JIS、EUC-JP) |
| セッション | 呼び出しのたびに SSH 接続を開くことが多い | MCP セッションの間、SSH は 1 本のまま |

Table: クライアント側の SSH 系 MCP との違い

### 特長

- **ポート転送・SFTP 不要**: SSH セッションの標準入出力だけで動作します。中継サーバーが `AllowTcpForwarding no` でも、`ssh` の入れ子で多段接続できます。
- **単一セッションの維持**: MCP セッション中は 1 本の SSH 接続を維持するため、多段接続でもツール呼び出しごとの接続遅延が発生しません。
- **Windows / Linux の差異吸収**: シェル、引用符、パスの違いを `host-shell` が吸収します。PowerShell コマンドも引用符の解釈を意識せずに渡せます。
- **エージェント向け基本操作の網羅**: 文字列置換 (`fs_edit`)、`**` 対応 glob、前後行・件数付き grep、ファイル移動・コピー・削除、対話ジョブへの入力を備えます。
- **疑似端末 (PTY) 対応**: パスワード入力を求める `sudo` や `ssh`、REPL など端末を要するプログラムを `pty` ジョブで実行できます (Linux pty / Windows ConPTY)。正規表現の一致 (`until`) や出力鎮静 (`idle_ms`) による待機にも対応します。
- **ネットワーク診断・通信ツール**: 接続先からの HTTP リクエスト (`net_request_http`、Unix ソケットの Docker API を含む)、TCP 送受信 (`net_exchange`)、名前解決・疎通・証明書確認 (`net_check`)、待ち受けポート一覧 (`net_list_listeners`) を提供します。
- **OS 管理の統一インターフェイス**: サービス (systemd / Windows サービス)、システム ログ (journald / イベント ログ)、プロセス停止、ディスク使用量、Windows レジストリを共通の形式で操作できます。
- **ローカル・接続先間のファイル転送**: ローカル側に `host-shell bridge` を挟むと、エージェントのコンテキストを経由せずにファイルやディレクトリを直接転送できます (`transfer_push`、`transfer_pull`)。SHA-256 で照合し、同一内容のファイルは転送を省略します。
- **進捗通知とキャンセル**: 長時間コマンド、ダウンロード、ファイル転送の進捗を MCP 通知で送り、エージェントが要求をキャンセルすると処理を停止します。
- **起動オプションによるポリシー制限**: 既定では接続ユーザーの全権限で動作し、必要に応じて公開ツール グループ、読み取り専用、許可パス、許可コマンド、接続先ホスト、事前確認、機密情報の伏せ字化を起動オプションで制限できます。
- **エージェント向けの自己記述案内**: 初期化応答、ツール説明、`help_get` ツールにより、利用者が手順を指示しなくてもエージェント自身が適切なツールを選択できます。
- **文字コードと改行の自動変換**: Shift_JIS や EUC-JP などのコマンド出力やファイルを接続先側で UTF-8 に変換します。書き込み時の文字コード、BOM、改行コードも指定できます。
- **単一バイナリ配布**: 外部依存のない単一の実行ファイルを接続先に配置するだけで動作します。
- **出力上限・タイムアウト・操作ログ**: 大きな出力は先頭と末尾を残して切り詰め、全ツール呼び出しを接続先のログに記録します。

### ツール

| 分類 | ツール |
|---|---|
| 使い方の案内 | `help_get` |
| コマンド実行 | `exec_run` |
| 長時間ジョブ | `job_start`、`job_read`、`job_write`、`job_stop`、`job_list` |
| ファイル操作 | `fs_list`、`fs_stat`、`fs_read`、`fs_edit`、`fs_write`、`fs_mkdir`、`fs_move`、`fs_copy`、`fs_remove` |
| 検索 | `search_grep`、`search_find` |
| 差分確認 | `fs_manifest` |
| 特権実行 | `exec_sudo` (Linux のみ) |
| 環境情報 | `sys_get_info` |
| ネットワーク | `net_request_http`、`net_exchange`、`net_check`、`net_list_listeners`、`net_list_interfaces` |
| 管理 | `proc_list`、`proc_kill`、`svc_list`、`svc_control`、`log_query`、`disk_list` |
| Windows レジストリ | `reg_read`、`reg_write`、`reg_delete` (Windows のみ) |
| ファイル転送 | `transfer_push`、`transfer_pull` (`host-shell bridge` 経由時) |

Table: 提供するツール

### クイック スタート

1. ビルドします (Go 1.26 以降)。

    ```bash
    make dist
    ```

2. 接続先に実行ファイルを配置します。SFTP がなくても SSH の標準入力経由で転送できます。システム領域を変更しないよう、既定では自分専用の `/tmp/host-shell-<uid>/` に配置します (`1000` は接続先で `id -u` が返す UID)。

    ```bash
    ssh target 'mkdir -m 700 /tmp/host-shell-1000 && cat > /tmp/host-shell-1000/host-shell && chmod 755 /tmp/host-shell-1000/host-shell' \
        < dist/host-shell-linux-amd64
    ssh -T target /tmp/host-shell-1000/host-shell --version
    ```

3. エージェントに登録します。ローカルの実行ファイルを `bridge` として挟むと、ファイル転送ツール (`transfer_push`、`transfer_pull`) も利用できます。

    ```bash
    claude mcp add host-shell -- /path/to/host-shell bridge -- ssh -T hop1 ssh -T target /tmp/host-shell-1000/host-shell --mcp
    ```

    `bridge` を使わない場合は、`ssh` から始まるコマンドを直接登録します。

    ```bash
    claude mcp add host-shell -- ssh -T hop1 ssh -T target /tmp/host-shell-1000/host-shell --mcp
    ```

4. エージェントに「host-shell の sys_get_info で接続先の OS を確認して」と指示します。

SSH の各段には `-T` を指定し、疑似端末を割り当てないでください。詳細な手順は [利用ガイド](docs/ja/user-guide.md) を参照してください。

手順 2〜3 は `.agents/skills/host-shell-connect` スキルでエージェントに任せることもできます。このスキルは経路確認、接続先の OS・アーキテクチャー判定、バイナリ配置、起動確認を行い、登録コマンドを提示します。`make dist` を実行すると、スキルの `bin/` に実行ファイルが複製されます。  
GitHub Release には、タグ名の先頭の `v` を残したアーカイブ (`host-shell-mcp-<tag>-bin.zip`、`.tar.gz`。例: `host-shell-mcp-v20261031.1.0-bin.zip`) を添付しています。中身はソース ツリーに、実行ファイル (`dist/` とスキルの `bin/`) を同梱したものです。

### ドキュメント

文書は [`docs/`](docs/README.md) にあります。**日本語版 (`docs/ja/`) が正本** であり、`docs/en/` に英訳を配置しています。

| 文書 | 内容 |
|---|---|
| [利用ガイド](docs/ja/user-guide.md) | 仕組み、バイナリ配置、SSH 経路の確認、エージェントへの登録、利用例、トラブルシューティング |
| [運用ガイド](docs/ja/operations.md) | セキュリティ設計、制限オプション、専用ユーザーと鍵、起動コマンドの固定、sudo 設定、操作ログ管理 |
| [リファレンス](docs/ja/reference.md) | 起動オプション、制限 (方針)、MCP 仕様、各ツールの入出力、bridge、プロセス管理、操作ログ形式、制限事項 |
| [開発ガイド](docs/ja/development.md) | ビルド、ソース構成、設計規約、ツールの追加手順、テスト |

Table: ドキュメント

### 安全性

`host-shell` は、既定では接続ユーザーの権限で任意の操作を実行できます。  
専用ユーザーと専用 SSH 鍵を用意し、OS のファイル権限と sudoers で操作範囲を制限してください。あわせて起動オプション (`--read-only`、`--tools`、`--allow-path`、`--confirm` など) で公開ツールと操作対象を絞り込めます。  
詳細は [運用ガイド](docs/ja/operations.md) を参照してください。
