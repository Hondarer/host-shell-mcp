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
| 環境情報 | `sys_info`、`sys_processes` |

Table: 提供するツール

## クイック スタート

1. ビルドします (Go 1.25 以降)。

    ```bash
    make dist
    ```

2. 接続先に実行ファイルを置きます。SFTP がなくても SSH で送れます。

    ```bash
    ssh target 'mkdir -p /opt/host-shell && cat > /opt/host-shell/host-shell && chmod 755 /opt/host-shell/host-shell' \
        < dist/host-shell-linux-amd64
    ssh -T target /opt/host-shell/host-shell --version
    ```

3. エージェントに登録します。

    ```bash
    claude mcp add host-shell -- ssh -T hop1 ssh -T target /opt/host-shell/host-shell --mcp
    ```

4. エージェントに「host-shell の sys_info で接続先の OS を確認して」と頼みます。

各段で `-T` を指定し、疑似端末を割り当てないでください。詳しい手順は利用ガイドにあります。

## ドキュメント

| 文書 | 内容 |
|---|---|
| [利用ガイド](docs/user-guide.md) | 仕組み、配置、SSH の経路の確認、エージェントへの登録、使い方、トラブルシューティング |
| [運用ガイド](docs/operations.md) | 安全性の考え方、専用のユーザーと鍵、コマンドの固定、sudo の設定、操作ログの管理 |
| [リファレンス](docs/reference.md) | 起動オプション、MCP の仕様、各ツールの入出力、プロセスの扱い、操作ログの形式、制限事項 |
| [開発ガイド](docs/development.md) | ビルド、ソースの構成、設計上の約束、ツールの追加、テスト |

Table: ドキュメント

## 安全性

`host-shell` は、接続したユーザーの権限で任意のコマンドを実行できます。  
専用のユーザーと鍵を使い、権限と sudoers の規則で操作の範囲を決め、エージェント側で変更を伴うツールに承認を必須にしてください。  
詳しくは [運用ガイド](docs/operations.md) を参照してください。
