# host-shell-mcp

`host-shell` は、**接続先マシン (Windows / Linux) に配置し、SSH などの標準入出力経由で中継して使用する** MCP サーバーです。  
ポート転送や SFTP が使えない環境でも、Claude Code や GitHub Copilot などのコーディング エージェントから、プレーンな SSH セッションだけで遠隔マシンを調査・操作できます。

```text
エージェント --stdio(MCP)--> ssh -T hop1 ssh -T target host-shell --mcp
```

クライアント側で `ssh` を起動する一般的な SSH 系 MCP サーバーとは異なり、`host-shell` 自体は接続先マシン上で動作します。

## 特長

- **ポート転送・SFTP 不要**: SSH セッションの標準入出力だけで動作します。中継サーバーが `AllowTcpForwarding no` でも、`ssh` の入れ子で多段接続できます。
- **単一セッションの維持**: MCP セッション中は 1 本の SSH 接続を維持するため、多段接続でもツール呼び出しごとの接続遅延が発生しません。
- **Windows / Linux の差異吸収**: シェル、引用符、パスの違いを `host-shell` が吸収します。PowerShell コマンドも引用符の解釈を意識せずに渡せます。
- **エージェント向け基本操作の網羅**: 文字列置換 (`fs_edit`)、`**` 対応 glob、前後行・件数付き grep、ファイル移動・コピー・削除、対話ジョブへの入力を備えます。
- **疑似端末 (PTY) 対応**: パスワード入力を求める `sudo` や `ssh`、REPL など端末を要するプログラムを `pty` ジョブで実行できます (Linux pty / Windows ConPTY)。正規表現の一致 (`until`) や出力鎮静 (`idle_ms`) による待機にも対応します。
- **ネットワーク診断・通信ツール**: 接続先からの HTTP リクエスト (`net_http`、Unix ソケットの Docker API を含む)、TCP 送受信 (`net_tcp`)、名前解決・疎通・証明書確認 (`net_check`)、待ち受けポート一覧 (`net_listen`) を提供します。
- **OS 管理の統一インターフェイス**: サービス (systemd / Windows サービス)、システム ログ (journald / イベント ログ)、プロセス停止、ディスク使用量、Windows レジストリを共通の形式で操作できます。
- **ローカル・接続先間のファイル転送**: ローカル側に `host-shell bridge` を挟むと、エージェントのコンテキストを経由せずにファイルやディレクトリを直接転送できます (`transfer_push`、`transfer_pull`)。SHA-256 で照合し、同一内容のファイルは転送を省略します。
- **進捗通知とキャンセル**: 長時間コマンド、ダウンロード、ファイル転送の進捗を MCP 通知で送り、エージェントが要求をキャンセルすると処理を停止します。
- **起動オプションによるポリシー制限**: 既定では接続ユーザーの全権限で動作し、必要に応じて公開ツール グループ、読み取り専用、許可パス、許可コマンド、接続先ホスト、事前確認、機密情報の伏せ字化を起動オプションで制限できます。
- **エージェント向けの自己記述案内**: 初期化応答、ツール説明、`help` ツールにより、利用者が手順を指示しなくてもエージェント自身が適切なツールを選択できます。
- **文字コードと改行の自動変換**: Shift_JIS や EUC-JP などのコマンド出力やファイルを接続先側で UTF-8 に変換します。書き込み時の文字コード、BOM、改行コードも指定できます。
- **単一バイナリ配布**: 外部依存のない単一の実行ファイルを接続先に配置するだけで動作します。
- **出力上限・タイムアウト・操作ログ**: 大きな出力は先頭と末尾を残して切り詰め、全ツール呼び出しを接続先のログに記録します。

## ツール

| 分類 | ツール |
|---|---|
| 使い方の案内 | `help` |
| コマンド実行 | `exec_run` |
| 長時間ジョブ | `job_start`、`job_read`、`job_write`、`job_stop`、`job_list` |
| ファイル操作 | `fs_list`、`fs_stat`、`fs_read`、`fs_edit`、`fs_write`、`fs_mkdir`、`fs_move`、`fs_copy`、`fs_remove` |
| 検索 | `search_grep`、`search_files` |
| 差分確認 | `fs_manifest` |
| 特権実行 | `sudo_run` (Linux のみ) |
| 環境情報 | `sys_info` |
| ネットワーク | `net_http`、`net_tcp`、`net_check`、`net_listen`、`net_interfaces` |
| 管理 | `sys_processes`、`proc_kill`、`svc_list`、`svc_control`、`log_query`、`sys_disks` |
| Windows レジストリ | `reg_read`、`reg_write`、`reg_delete` (Windows のみ) |
| ファイル転送 | `transfer_push`、`transfer_pull` (`host-shell bridge` 経由時) |

Table: 提供するツール

## クイック スタート

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

    bridge を使わない場合は、`ssh` から始まるコマンドを直接登録します。

    ```bash
    claude mcp add host-shell -- ssh -T hop1 ssh -T target /tmp/host-shell-1000/host-shell --mcp
    ```

4. エージェントに「host-shell の sys_info で接続先の OS を確認して」と指示します。

SSH の各段には `-T` を指定し、疑似端末を割り当てないでください。詳細な手順は [利用ガイド](docs/user-guide.md) を参照してください。

手順 2〜3 は `.agents/skills/host-shell-connect` スキルでエージェントに任せることもできます。このスキルは経路確認、接続先の OS・アーキテクチャー判定、バイナリ配置、起動確認を行い、登録コマンドを提示します。`make dist` を実行すると、スキルの `bin/` に実行ファイルが複製されます。  
GitHub Release には、ソース ツリーに実行ファイル (`dist/` とスキルの `bin/`) を同梱したアーカイブ (`host-shell-mcp-<version>-bin.zip`、`.tar.gz`) を添付しています。

## ドキュメント

| 文書 | 内容 |
|---|---|
| [利用ガイド](docs/user-guide.md) | 仕組み、バイナリ配置、SSH 経路の確認、エージェントへの登録、利用例、トラブルシューティング |
| [運用ガイド](docs/operations.md) | セキュリティ設計、制限オプション、専用ユーザーと鍵、起動コマンドの固定、sudo 設定、操作ログ管理 |
| [リファレンス](docs/reference.md) | 起動オプション、制限 (ポリシー)、MCP 仕様、各ツールの入出力、bridge、プロセス管理、操作ログ形式、制限事項 |
| [開発ガイド](docs/development.md) | ビルド、ソース構成、設計規約、ツールの追加手順、テスト |

Table: ドキュメント

## 安全性

`host-shell` は、既定では接続ユーザーの権限で任意の操作を実行できます。  
専用ユーザーと専用 SSH 鍵を用意し、OS のファイル権限と sudoers で操作範囲を制限してください。あわせて起動オプション (`--read-only`、`--tools`、`--allow-path`、`--confirm` など) で公開ツールと操作対象を絞り込めます。  
詳細は [運用ガイド](docs/operations.md) を参照してください。
