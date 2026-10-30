# host-shell-mcp

`host-shell` は、**接続先のマシン (Windows / Linux) に配置し、SSH などの標準入出力で中継して使う** MCP サーバーです。  
ポート転送も SFTP も使えない環境で、Claude Code や GitHub Copilot などのコーディング エージェントが、プレーンな SSH だけを経由して遠隔のマシンを調査、操作できるようにします。

```text
エージェント --stdio(MCP)--> ssh -T hop1 ssh -T target host-shell --mcp
```

手元で `ssh` を呼ぶ一般的な SSH 系 MCP サーバーとは仕組みが逆で、`host-shell` 自身は接続先で動きます。

## 特長

- **ポート転送と SFTP が不要。** SSH のセッションの標準入出力だけで動きます。中継サーバーで `AllowTcpForwarding no` でも、`ssh` の入れ子で多段に接続できます。
- **接続は 1 本。** MCP のセッションの間、SSH の接続を使い続けるため、多段でもツールを呼ぶたびの接続の遅延がありません。
- **Windows と Linux を同じツールで。** シェル、引用符、パスの違いを `host-shell` が吸収します。PowerShell のコマンドは引用符の解釈を気にせずに渡せます。
- **コーディング エージェントの定番の操作をそろえる。** 文字列の置換による編集 (`fs_edit`)、`**` を使える glob、前後の行や件数を返す grep、移動、コピー、削除、対話的なジョブへの入力に対応します。
- **疑似端末。** パスワードを尋ねる `sudo` や `ssh`、REPL など、端末が必要なプログラムを `pty` のジョブで動かせます (Linux の pty、Windows の ConPTY)。プロンプトが出るまで待つ (`until`)、出力が落ち着くまで待つ (`idle_ms`) こともできます。
- **ポート転送の代わりのネットワークの道具。** 接続先から HTTP の要求 (`net_http`。Unix ソケットの Docker API も)、TCP の送受信 (`net_tcp`)、名前の解決と疎通と証明書の確認 (`net_check`)、待ち受け中のポートの一覧 (`net_listen`) を使えます。
- **OS の管理。** サービス (systemd、Windows のサービス)、システムのログ (journald、イベント ログ)、プロセスの停止、ディスクの使用量、Windows のレジストリを、OS によらない同じ形で扱えます。
- **手元と接続先の間のファイルの転送。** 手元で `host-shell bridge` を挟むと、ファイルやディレクトリを、エージェントの文脈を通さずに直接送れます (`transfer_push`、`transfer_pull`)。SHA-256 で照合し、変化のないファイルは送りません。
- **進み具合の通知と中止。** 長いコマンドやダウンロード、転送の進み具合を MCP の通知で伝え、エージェントが要求を取り消すと処理を止めます。
- **既定は何でもできる。必要なら制限できる。** 公開するツールの組、読み取り専用、操作できる場所、実行できるプログラム、接続できる宛先、実行前の利用者の確認、秘密情報の伏せ字を、起動オプションで指定できます。
- **エージェントが自分で使い方を知る。** 初期化の案内、ツールの説明、`help` ツールで、利用者が教えなくてもエージェントがツールを選べます。
- **文字コードと改行の違いを吸収。** Shift_JIS や EUC-JP などのコマンドの出力やファイルを、接続先の側で UTF-8 に変換します。書き込むときの文字コード、BOM、改行も指定できます。
- **依存のない単体の実行ファイル。** 接続先にファイルを 1 つ置くだけです。
- **出力の上限、タイムアウト、操作ログ。** 大きな出力は先頭と末尾を残して切り詰め、すべての操作を接続先のログに記録します。

## ツール

| 分類 | ツール |
|---|---|
| 使い方の案内 | `help` |
| コマンド実行 | `exec_run` |
| 長時間の実行 | `job_start`、`job_read`、`job_write`、`job_stop`、`job_list` |
| ファイル操作 | `fs_list`、`fs_stat`、`fs_read`、`fs_edit`、`fs_write`、`fs_mkdir`、`fs_move`、`fs_copy`、`fs_remove` |
| 検索 | `search_grep`、`search_files` |
| 差分の確認 | `fs_manifest` |
| 特権実行 | `sudo_run` (Linux のみ) |
| 環境情報 | `sys_info` |
| ネットワーク | `net_http`、`net_tcp`、`net_check`、`net_listen`、`net_interfaces` |
| 管理 | `sys_processes`、`proc_kill`、`svc_list`、`svc_control`、`log_query`、`sys_disks` |
| Windows のレジストリ | `reg_read`、`reg_write`、`reg_delete` (Windows のみ) |
| 手元との転送 | `transfer_push`、`transfer_pull` (`host-shell bridge` を経由する場合) |

Table: 提供するツール

## クイック スタート

1. ビルドします (Go 1.26 以降)。

    ```bash
    make dist
    ```

2. 接続先に実行ファイルを置きます。SFTP がなくても SSH で送れます。システムを汚さないよう、既定では自分専用の `/tmp/host-shell-<uid>/` に置きます (`1000` は接続先で `id -u` が返す番号)。

    ```bash
    ssh target 'mkdir -m 700 /tmp/host-shell-1000 && cat > /tmp/host-shell-1000/host-shell && chmod 755 /tmp/host-shell-1000/host-shell' \
        < dist/host-shell-linux-amd64
    ssh -T target /tmp/host-shell-1000/host-shell --version
    ```

3. エージェントに登録します。手元の実行ファイルを `bridge` として挟むと、手元と接続先の間でファイルを送るツールも使えます。

    ```bash
    claude mcp add host-shell -- /path/to/host-shell bridge -- ssh -T hop1 ssh -T target /tmp/host-shell-1000/host-shell --mcp
    ```

    bridge を使わない場合は、`ssh` から始まるコマンドをそのまま登録します。

    ```bash
    claude mcp add host-shell -- ssh -T hop1 ssh -T target /tmp/host-shell-1000/host-shell --mcp
    ```

4. エージェントに「host-shell の sys_info で接続先の OS を確認して」と頼みます。

各段で `-T` を指定し、疑似端末を割り当てないでください。詳しい手順は利用ガイドにあります。

エージェントに手順 2 から 3 を任せることもできます。`.agents/skills/host-shell-connect` の skill が、経路の確認、接続先の判定、配置、起動の確認を行い、登録のコマンドを示します。`make dist` を実行すると、skill の `bin/` に実行ファイルが複製されます。  
GitHub の Release には、`bin/` を含む skill のアーカイブと、各 OS の実行ファイルを添付しています。

## ドキュメント

| 文書 | 内容 |
|---|---|
| [利用ガイド](docs/user-guide.md) | 仕組み、配置、SSH の経路の確認、エージェントへの登録、使い方、トラブルシューティング |
| [運用ガイド](docs/operations.md) | 安全性の考え方、制限のオプションの使い方、専用のユーザーと鍵、コマンドの固定、sudo の設定、操作ログの管理 |
| [リファレンス](docs/reference.md) | 起動オプション、制限、MCP の仕様、各ツールの入出力、bridge、プロセスの扱い、操作ログの形式、制限事項 |
| [開発ガイド](docs/development.md) | ビルド、ソースの構成、設計上の約束、ツールの追加、テスト |

Table: ドキュメント

## 安全性

`host-shell` は、既定では接続したユーザーの権限で何でもできます。  
専用のユーザーと鍵を使い、権限と sudoers の規則で操作の範囲を決めてください。必要に応じて、起動オプション (`--read-only`、`--tools`、`--allow-path`、`--confirm` など) で、公開するツールと操作の範囲を絞れます。  
詳しくは [運用ガイド](docs/operations.md) を参照してください。
