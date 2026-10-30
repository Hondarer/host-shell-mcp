# host-shell リファレンス

> [!NOTE]
> この文書が正本です。英訳は [../en/reference.md](../en/reference.md) にあります。内容が一致しない場合は日本語版が優先されます。

`host-shell` の起動オプション、MCP サーバー仕様、各ツールの入出力、プロセス管理と操作ログの仕様を定義します。  
導入手順と利用例は [利用ガイド](user-guide.md)、安全な運用設計は [運用ガイド](operations.md) を参照してください。

## 目次

- [起動](#起動)
- [制限 (方針)](#制限-方針)
- [MCP サーバーとしての振る舞い](#mcp-サーバーとしての振る舞い)
- [全ツール共通の仕様](#全ツール共通の仕様)
- [文字コードと改行](#文字コードと改行)
- [ツール一覧](#ツール一覧)
- [使い方の案内](#使い方の案内)
- [コマンド実行](#コマンド実行)
- [長時間の実行 (ジョブ)](#長時間の実行-ジョブ)
- [ファイル操作](#ファイル操作)
- [検索](#検索)
- [差分の確認](#差分の確認)
- [特権実行](#特権実行)
- [環境情報](#環境情報)
- [ネットワーク](#ネットワーク)
- [管理](#管理)
- [Windows のレジストリ](#windows-のレジストリ)
- [手元との転送 (bridge)](#手元との転送-bridge)
- [プロセスの起動と終了](#プロセスの起動と終了)
- [操作ログ](#操作ログ)
- [制限事項](#制限事項)

## 起動

### 書式

```text
host-shell --mcp [options]                      (接続先)
host-shell bridge [bridge-options] -- COMMAND...  (ローカル)
host-shell --version
```

`--mcp` 指定時のみ、MCP サーバーとして標準入出力で待ち受けます。  
`bridge` はローカル側で接続先の `host-shell` を中継するサブコマンドです ([手元との転送 (bridge)](#手元との転送-bridge) を参照)。  
`--mcp`、`bridge`、`--version` のいずれも指定しない場合は、使い方を標準エラーに出力して終了コード 2 で終了します。

### オプション

| オプション | 値 | 既定値 | 内容 |
|---|---|---|---|
| `--mcp` | なし | | MCP サーバーとして標準入出力で待ち受ける |
| `--version` | なし | | バージョンを標準出力に出力して終了する |
| `--log-file` | パス | [操作ログ](#操作ログ) を参照 | 操作ログの基準パス (拡張子の直前に日付を挿入したファイルへ出力) |
| `--no-log` | なし | | 操作ログを記録しない (`--log-file` より優先) |
| `--max-output` | バイト数 (1024 以上) | `65536` | 1 出力あたりのサイズ上限 |
| `--timeout` | 時間 (例: `90s`、`5m`) | `1m` | コマンド実行タイムアウトの既定値 |
| `--max-timeout` | 時間 | `30m` | ツール引数で指定できるタイムアウトの上限 (`--timeout` 以上) |
| `--windows-shell` | `cmd`、`powershell`、`pwsh` | `powershell` | Windows で `shell` 省略時に使用する既定シェル |
| `--tools` | グループ名・ツール名 (カンマ区切り) | `all` | 公開するツール ([制限 (方針)](#制限-方針) を参照) |
| `--read-only` | なし | | 接続先を変更するツールを非公開にする |
| `--confirm` | グループ名・ツール名、`destructive` | なし | 実行前に利用者の確認を求めるツール |
| `--allow-path` | ディレクトリ (複数指定可) | なし | ファイル系ツールで読み書きを許可するディレクトリ |
| `--allow-read-path` | ディレクトリ (複数指定可) | なし | ファイル系ツールで読み取りのみを許可するディレクトリ |
| `--allow-command` | プログラム名 glob (カンマ区切り、複数指定可) | なし | 起動を許可するプログラム |
| `--deny-command` | 正規表現 (複数指定可) | なし | 実行を拒否するコマンド ライン パターン |
| `--allow-host` | 宛先 (カンマ区切り、複数指定可) | なし | ネットワーク ツールで接続を許可する宛先 |
| `--unset-env` | 環境変数名 glob (カンマ区切り、複数指定可) | なし | 子プロセスへ引き継がない環境変数 |
| `--redact` | なし | | ツール出力に含まれる既知パターンの機密情報を伏せ字化する |
| `--redact-pattern` | 正規表現 (複数指定可) | なし | 追加で伏せ字化する正規表現パターン |

Table: 起動オプション

オプションは `-mcp` のように単一ハイフンでも指定できます。  
未知のオプションや型不一致の値を指定した場合は標準エラーに使い方を出力して終了コード 2 で終了し、値の範囲や正規表現が不正な場合は理由を標準エラーに出力して終了コード 1 で終了します。

### 入出力と終了

| 項目 | 振る舞い |
|---|---|
| 標準入力 | MCP の JSON-RPC メッセージを 1 行 1 件で受信する |
| 標準出力 | MCP の JSON-RPC メッセージのみを出力する (それ以外は一切出力しない) |
| 標準エラー | 起動失敗などの診断メッセージを出力する |
| 作業ディレクトリ | 起動時のカレント ディレクトリ (SSH 経由時は通常ユーザーのホーム ディレクトリ) |
| 正常終了 | 標準入力のクローズ (SSH 切断) または SIGINT・SIGTERM 受信時 (終了コード 0) |
| 終了時処理 | 実行中の全ジョブを停止する |

Table: 入出力と終了

1 つの SSH セッションが 1 つの `host-shell` プロセスに対応します。複数のエージェントから同時接続した場合は独立したプロセスとして動作し、ジョブ等の状態は共有しません。

## 制限 (方針)

制限オプションを指定しない場合、`host-shell` は全ツールを公開し、接続ユーザーの権限で制限なく動作します。  
オプションで指定した項目のみを制限し、適用中の制限は [`sys_get_info`](#sys_get_info) の `restrictions`、`help_get` 先頭の表、初期化応答の `instructions` でエージェントへ通知します。制限に違反する呼び出しは実行せず、`policy:` で始まるエラーを返します。

### ツールの選択 (--tools)

ツールは次のグループに分類されます。`help_get` と `sys_get_info` (`core` グループ) は常に公開されます。

| グループ | ツール |
|---|---|
| `files` | `fs_list`、`fs_stat`、`fs_read`、`fs_edit`、`fs_write`、`fs_mkdir`、`fs_move`、`fs_copy`、`fs_remove`、`fs_manifest` |
| `search` | `search_grep`、`search_find` |
| `exec` | `exec_run`、`exec_sudo`、`job_start`、`job_read`、`job_write`、`job_stop`、`job_list` |
| `net` | `net_request_http`、`net_exchange`、`net_check`、`net_list_listeners`、`net_list_interfaces` |
| `admin` | `proc_list`、`proc_kill`、`svc_list`、`svc_control`、`log_query`、`disk_list`、`reg_read`、`reg_write`、`reg_delete` |

Table: ツールのグループ

`--tools` にはグループ名、ツール名、`all`、`destructive` (変更系ツール)、`readonly` (読み取り系ツール) をカンマ区切りで指定し、左から順に評価します。先頭の `-` は除外、`+` (または接頭辞なし) は追加を表します。OS 固有ツール (`exec_sudo`、`reg_*`) の名前は非対応 OS 上でもエラーにならず指定できます (非対応 OS では公開されません)。

| 指定 | 公開されるツール |
|---|---|
| `all` (既定値) | 全ツール |
| `files,search` | ファイル操作・検索ツールのみ |
| `all,-admin,-exec_sudo` | 管理ツールと `exec_sudo` を除く全ツール |
| `files,exec,-fs_remove` | ファイル操作・コマンド実行ツールから `fs_remove` を除いたもの |

Table: --tools の例

### 読み取り専用 (--read-only)

`readOnlyHint` 注釈を持たないツールを非公開にします。ただし `net_request_http` と `net_exchange` は次の制限付きで公開します。

- `net_request_http`: メソッドは `GET`、`HEAD`、`OPTIONS` のみ許可し、`save_to` は使用不可
- `net_exchange`: `send` は使用不可 (接続先が先に送信するデータの受信のみ)

### 利用者の確認 (--confirm)

指定したツールの呼び出し時に MCP の elicitation で利用者に承認を求め、承認された場合のみ実行します (指定書式は `--tools` と同じ)。

- プロトコル バージョン `2026-07-28` のクライアントには、multi round-trip requests (SEP-2322) に従い確認要求 (`inputRequests`) を結果として返します。クライアントが回答を添えて同一呼び出しを再送すると実行します (起動ごとの秘密鍵で署名した `requestState` により、同一ツール・同一引数の再送のみを有効とします)。
- その他の対応プロトコル バージョンのクライアントには、SDK が `elicitation/create` を送信して回答を受け取り、処理を継続します。
- クライアントが elicitation 非対応の場合は実行せずにエラーを返します。
- 確認要求の発生は操作ログに `<ツール名> (confirmation requested)` として記録されます。

### ファイルの場所 (--allow-path、--allow-read-path)

ファイル パスを受け取る引数を検査します。

| 分類 | 読み取りとして検査 | 書き込みとして検査 |
|---|---|---|
| ファイル操作 | `fs_list`、`fs_stat`、`fs_read`、`fs_manifest` の `path`、`fs_copy` の `source` | `fs_edit`、`fs_write`、`fs_mkdir`、`fs_remove` の `path`、`fs_move` の `source`・`destination`、`fs_copy` の `destination` |
| 検索 | `search_grep`、`search_find` の `path` | |
| コマンド実行 | `exec_run`、`job_start`、`exec_sudo` の `cwd` | |
| ネットワーク | `net_request_http` の `body_file`、`ca_cert`、`client_cert`、`client_key` | `net_request_http` の `save_to` |

Table: 場所の検査の対象

- 読み取りは `--allow-path` または `--allow-read-path` の配下で許可し、書き込みは `--allow-path` の配下のみで許可します (`--allow-read-path` のみ指定時は全パスへの書き込みを拒否します)。
- 判定前に絶対パスへ変換し、既存部分のシンボリック リンクを解決します。許可ディレクトリ外を指すシンボリック リンク経由のアクセスは拒否します。
- Windows では大文字・小文字を区別せずに比較します。

### コマンド (--allow-command、--deny-command)

`exec_run`、`job_start`、`exec_sudo` で起動するプログラムを検査します。

- `--allow-command` 指定時は、起動プログラム名 (ベース名。Windows では小文字化し `.exe` 等の拡張子を除いた名前) がいずれかの glob に一致する必要があります。
- `command` (シェル コマンド ライン) を使用する場合は、シェル自体 (`sh`、`bash`、`cmd`、`powershell`、`pwsh`) が許可されている必要があります。シェルを許可するとシェル経由で任意のプログラムを起動できるため、実行プログラムを絞る場合はシェルを許可せず `argv` を使用させます。
- `exec_sudo` には `sudo` と `sh` の両方の許可が必要です。
- `--deny-command` はコマンド ライン (`argv` の場合は空白連結文字列) が正規表現に一致する呼び出しを拒否します (表記変更で回避可能なため、誤操作防止用です)。

### ネットワークの宛先 (--allow-host)

`net_request_http` (リダイレクト先を含む)、`net_exchange`、`net_check` の接続先を検査します。

| 書式 | 意味 |
|---|---|
| `localhost`、`*.internal` | ホスト名の glob (全ポート許可) |
| `api.internal:443`、`*:80` | ホスト名とポートの glob |
| `10.0.0.0/8`、`10.0.0.0/8:5432` | CIDR 範囲 (ホスト名指定時は解決された全 IP アドレスが範囲内であること) |
| `unix:/var/run/*.sock` | Unix ドメイン ソケット パスの glob |

Table: --allow-host の書き方

### 環境変数 (--unset-env)

`exec_run`、`job_start`、`exec_sudo`、および管理ツールが起動する子プロセスから、指定した glob に一致する環境変数を除外します (例: `--unset-env 'AWS_*,*TOKEN*'`)。ツール引数の `env` で明示指定された変数はそのまま渡されます。

### 伏せ字 (--redact、--redact-pattern)

ツール結果の全文字列 (`structuredContent` と `content`) から機密情報パターンを検出し、`[REDACTED]` に置換します。

| 種別 | 対象パターン例 |
|---|---|
| 秘密鍵 PEM | `-----BEGIN ... PRIVATE KEY-----` から `-----END ... PRIVATE KEY-----` までの本文 |
| 認証ヘッダー | `Authorization: Bearer ...` |
| キーと値のペア | `password=...`、`api_key: ...`、`DB_PASSWORD=...`、`"token": "..."` の値部分 |
| URL 認証情報 | `https://user:password@host` のパスワード部分 |
| 既知トークン | AWS アクセス キー ID、GitHub・Slack トークン、`sk-...`、JWT |

Table: 伏せ字にする形

`--redact-pattern` で追加の正規表現を指定できます (`--redact` 単独指定なしでも有効)。  
伏せ字化されたファイル内容をそのまま `fs_write` で書き戻すと `[REDACTED]` がファイルに書き込まれるため、`help_get` の `policy` トピックでエージェントに注意喚起しています。

### 制限の限界

これらの制限はツール引数に対する検査であり、OS の権限境界ではありません。

- `exec` グループを公開している場合、起動されたコマンドはファイルやネットワークへ直接アクセスできます。アクセス範囲を厳密に制限するには `--tools` で `exec` を除外するか、`--allow-command` で実行可能プログラムを限定してください。
- 強制力のある権限境界が必要な場合は、`host-shell` を最小権限の専用 OS ユーザーで実行してください ([運用ガイド](operations.md) を参照)。

## MCP サーバーとしての振る舞い

| 項目 | 内容 |
|---|---|
| 通信方式 | stdio (改行区切り JSON-RPC 2.0) |
| プロトコル バージョン | `2024-11-05`、`2025-03-26`、`2025-06-18`、`2025-11-25`、`2026-07-28` (クライアント要求に応じて選択) |
| サーバー名 | `host-shell` |
| 機能 (capabilities) | `tools`、`resources`、`logging` |
| 進捗通知 | リクエストに `progressToken` がある場合、`exec_run`・`exec_sudo` は 1 秒間隔、`net_request_http` の `save_to` は受信に応じて `notifications/progress` を送信 |
| キャンセル処理 | `notifications/cancelled` 受信時に要求処理を中断し、実行中コマンドをプロセス ツリーごと強制終了 |
| 利用者確認 | `--confirm` 対象ツールで elicitation を使用 ([利用者の確認](#利用者の確認---confirm)) |
| 受信メッセージ上限 | 1 メッセージあたり 16 MiB |
| 使用 SDK | [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) v1.8.0 |

Table: MCP サーバーの仕様

### エージェントへの案内

エージェントが外部ドキュメントを参照せずに MCP の情報だけで利用できるよう、4 層で案内を提供します。

| 層 | 内容 | 取得タイミング |
|---|---|---|
| 初期化応答の `instructions` | 接続先ホスト名、OS・アーキテクチャー、作業ディレクトリ、既定シェル・文字コード、出力上限・タイムアウト、公開ツールの選択方法、各呼び出しが独立プロセスである旨、および適用中の制限 | 接続時 |
| ツールの `title`・`description` | ツールの役割、使用場面、併用するツール | `tools/list` 取得時 |
| 入出力スキーマの `description` | 各引数の意味、既定値、分割読み取りの継続方法 | `tools/list` 取得時 |
| [`help_get`](#help_get) ツールとリソース | トピック別の手順、引数例、Windows の注意点、各種上限 | 必要に応じた呼び出し時 |

Table: エージェントへの案内

案内文とエラー メッセージはエージェント向けに英語で記述し、エラー発生時に次の対処を判断できる情報 (例: `fs_edit` で複数箇所に一致した際の行番号と `replace_all` の案内) を含めます。`help_get` の内容は MCP リソース (`host-shell://help/<topic>`、`text/markdown`) としても公開します。

## 全ツール共通の仕様

### 結果の形式

ツール結果は `structuredContent` に JSON オブジェクトとして返し、同一 JSON の文字列を `content` のテキストにも格納します。各ツールの出力スキーマは `tools/list` の `outputSchema` で公開されます。  
例外として、`help_get` は Markdown テキストのみを `content` に返し、`outputSchema` を持ちません。

### エラー

次の場合は結果の `isError` を `true` とし、`content` のテキストにエラー理由を返します。

- 引数がスキーマに適合しない場合 (必須引数の欠落、型不一致、未定義引数の指定)
- ファイル不在や権限不足など、操作自体が失敗した場合
- コマンドを起動できなかった場合 (実行ファイル不在、非対応シェルの指定など)

コマンド起動後に 0 以外の終了コードで終了した場合は `isError: false` となるため、結果の `exit_code` で成否を判定してください。

### パスの解釈

パスは接続先マシン上で解釈され、相対パスは `host-shell` の作業ディレクトリを起点とします。Windows では `/` と `\` の両方を使用できます。結果に含まれるパスは、特記がない限り絶対パスで返します。

### 出力の上限

すべてのツールに出力サイズまたは件数の上限を設けています。バイト上限の既定値は 64 KiB (`--max-output` で変更可能) です。

| ツール | 上限の単位 | 上限超過時の動作 |
|---|---|---|
| `exec_run`、`exec_sudo` | 標準出力・標準エラーの各ストリーム | 先頭と末尾を半分ずつ残して省略マーカーを挿入し、`stdout_truncated` 等を `true` にする |
| `job_read` | 1 回の読み取りバイト数 | `next_offset` から継続読み取り可能 |
| `fs_read` | 1 回の読み取りバイト数・行数 | `truncated: true` と次回読み取り位置 (`next_offset` / `next_line`) を返す |
| `proc_list` | 出力全体 | 先頭と末尾を残して省略マーカーを挿入する |
| `net_request_http` | レスポンス本文 | 先頭のみを返し `body_truncated: true` にする (`save_to` 使用時は上限なし) |
| `net_exchange` | 受信データ | 上限到達時点で受信を打ち切り `truncated: true` にする |
| `log_query` | 出力全体 | 古いログ項目から除外し `truncated: true` にする |
| `fs_list`、`search_*`、`fs_manifest` | 引数で指定した最大件数 | `truncated: true` にする |

Table: 出力の上限

省略マーカーの形式は次のとおりです。

```text

... [host-shell: 123456 bytes omitted] ...

```

### 文字コード

ツールの入出力テキストは常に UTF-8 です。接続先のコマンド出力やファイルが UTF-8 以外の場合は、`host-shell` が UTF-8 と相互変換します ([文字コードと改行](#文字コードと改行) を参照)。

### ツールの注釈

各ツールにはエージェント側の承認制御の目安として MCP ツール注釈を付与しています。

| 注釈 | 対象ツール |
|---|---|
| `readOnlyHint: true` | `help_get`、`job_read`、`job_list`、`fs_list`、`fs_stat`、`fs_read`、`search_grep`、`search_find`、`fs_manifest`、`sys_get_info`、`proc_list`、`net_check`、`net_list_listeners`、`net_list_interfaces`、`svc_list`、`log_query`、`disk_list`、`reg_read` |
| `destructiveHint: false`、`idempotentHint: true` | `fs_mkdir` |
| `destructiveHint: true` | `exec_run`、`job_start`、`job_write`、`job_stop`、`fs_edit`、`fs_write`、`fs_move`、`fs_copy`、`fs_remove`、`exec_sudo`、`net_request_http`、`net_exchange`、`proc_kill`、`svc_control`、`reg_write`、`reg_delete` |

Table: ツールの注釈

外部ネットワークへ作用しうる `net_request_http`、`net_exchange`、`net_check` のみ `openWorldHint: true`、それ以外は `false` です。

## 文字コードと改行

MCP 通信は常に UTF-8 で行い、接続先の文字コード (Shift_JIS、EUC-JP、UTF-16 など) や改行コードとの差異は `host-shell` が接続先側で吸収します。

### 接続先の既定の文字コード

自動判定 (`auto`) および `system` 指定では、用途に応じて次の 2 種類の既定文字コードを使用します。

| 種類 | 用途 | Windows | Linux |
|---|---|---|---|
| コンソール | コマンド出力と標準入力 | OEM コード ページ (`GetOEMCP`) | ロケールの文字コード |
| ファイル | テキスト ファイルの読み書き・検索 | ANSI コード ページ (`GetACP`) | ロケールの文字コード |

Table: 接続先の既定の文字コード

日本語 Windows では両方とも Shift_JIS (CP932) です (Windows の「ワールド ワイド言語サポートで Unicode UTF-8 を使用」が有効な場合は UTF-8)。  
Linux では環境変数 `LC_ALL`、`LC_CTYPE`、`LANG` の優先順で最初に設定されている値から `.` 以降の文字コード名を取得します (例: `ja_JP.eucJP` は EUC-JP。文字コード部がない `C`・`POSIX` や未設定時は UTF-8)。  
実際の値は [`sys_get_info`](#sys_get_info) の `console_encoding` と `file_encoding` で確認できます。

### 文字コードの指定

文字コード指定引数 (`output_encoding`、`stdin_encoding`、`charset`) には次の値を指定できます。

| 値 | 意味 |
|---|---|
| `auto` | 自動判定 (読み取り系引数のみ指定可能、省略時の既定値) |
| `utf-8` | UTF-8 (無変換) |
| `system` | 接続先の既定文字コード |
| エンコーディング名 | `shift_jis`、`euc-jp`、`iso-2022-jp`、`utf-16le`、`utf-16be`、`gbk`、`gb18030`、`big5`、`euc-kr`、`windows-1252` など |
| コード ページ番号 | `cp932`、`932`、`cp437`、`cp65001` など |

Table: 文字コードの指定

大文字・小文字は区別しません。[WHATWG Encoding Standard](https://encoding.spec.whatwg.org/#names-and-labels) の名称・別名 (`sjis`、`windows-31j`、`latin1` など) に加え、`eucjp`、`ujis` 等の慣用名に対応します (`shift_jis` は Windows-31J / CP932 として扱います)。

### 自動判定

読み取り時の自動判定は次の順序で行います。

1. 先頭に BOM (UTF-8、UTF-16LE、UTF-16BE) があればその文字コードを採用し、BOM 自体は結果から除きます。
2. BOM がなくても先頭バイト列が UTF-16 の特徴を満たせば UTF-16LE または UTF-16BE と判定します。
3. 有効な UTF-8 バイト列であれば UTF-8 とします。
4. いずれでもなければ接続先の既定文字コードとして変換します。

コマンド出力 (`exec_run`、`job_start`、`exec_sudo`)、`search_grep`、および `fs_read` の行範囲読み取りでは、手順 3・4 の判定を行単位で行うため、UTF-8 と Shift_JIS が混在する出力も正しく変換できます (`fs_read` のバイト範囲読み取りでは読み取り範囲全体で判定します)。

BOM なし UTF-16 の判定は、先頭最大 4096 バイト (コマンド出力では最初の 64 バイト到着時点または終了時点) を 2 バイト単位で検査し、次の全条件を満たす場合に UTF-16 とみなします。

- 両バイトが 0 の単位 (`U+0000`) が 1% 以下
- 片方のバイトが 0 の単位が 5% 以上 (0 が出現する側で LE / BE を決定)
- 反対側のバイトが 0 の単位が 2% 以下 (短文を考慮し 1 単位までは許容)
- サロゲート ペアがすべて正しい組み合わせになっている

ASCII 文字や改行を含む UTF-16 テキストはこの条件を満たし、バイナリや圧縮データは除外されます。改行や ASCII 文字を含まない短い和文のみの UTF-16 は自動判定できない場合があるため、`charset` を明示してください。

変換に使用した文字コードは `stdout_encoding` や `charset` フィールドで返します。いずれの文字コードでも解釈できない不正バイト列は置換文字 (`U+FFFD`) に置き換えます。

### 文字の分断の防止

分割読み取りを行うツールでは、チャンク境界でマルチバイト文字を分断しません。末尾の不完全なバイト列は次回読み取り位置 (`next_offset`) に回し、`exec_run` 等の出力切り詰め時も境界の分断文字を除去してから省略マーカーを挿入します。

### 改行

読み取り時は改行コードを変換せず、CRLF も LF もそのまま返します。`fs_read` は内容に含まれる改行種別を `newline` (`lf`、`crlf`、`cr`、`mixed`) で返します。  
`fs_write` の `newline` に `lf` または `crlf` を指定すると、書き込み時に改行コードを統一します。`fs_read` が返した `charset`、`bom`、`newline` を `fs_write` に渡すことで元のファイル形式を維持できます。

### 変換の対象

| ツール | 引数 | 既定値 |
|---|---|---|
| `exec_run`、`job_start`、`exec_sudo` | `output_encoding` (標準出力・標準エラー) | `auto` (コンソール) |
| | `stdin_encoding` (標準入力) | `utf-8` (Windows の PowerShell は [シェル](#シェル) を参照) |
| `job_write` | (`job_start` の `stdin_encoding` を継承) | `job_start` と同じ |
| `proc_list` | なし | `auto` (コンソール) |
| `fs_read` (`text`) | `charset` | `auto` (ファイル) |
| `fs_edit` | `charset` | `auto` (ファイル。読み取り時の文字コード・BOM・改行で書き戻す) |
| `fs_write` (`text`) | `charset`、`newline`、`bom` | `utf-8`、`keep` (無変換)、`false` (BOM なし) |
| `search_grep` | `charset` | `auto` (ファイル) |

Table: 文字コードの変換の対象

`base64` モードでの読み書き時は、文字コードも改行コードも変換しません。

## ツール一覧

| 分類 | ツール | 内容 |
|---|---|---|
| 使い方の案内 | [`help_get`](#help_get) | エージェント向けの使い方を返す |
| コマンド実行 | [`exec_run`](#exec_run) | コマンドを実行し、終了を待って結果を返す |
| 長時間の実行 | [`job_start`](#job_start) | コマンドをバックグラウンド ジョブとして開始する |
| | [`job_read`](#job_read) | ジョブの出力と状態を取得する |
| | [`job_write`](#job_write) | ジョブの標準入力へデータを送る |
| | [`job_stop`](#job_stop) | ジョブにシグナルを送るか強制終了する |
| | [`job_list`](#job_list) | ジョブ一覧を取得する |
| ファイル操作 | [`fs_list`](#fs_list) | ディレクトリ内の項目一覧を取得する |
| | [`fs_stat`](#fs_stat) | ファイルまたはディレクトリの属性を取得する |
| | [`fs_read`](#fs_read) | ファイルを読み取る |
| | [`fs_edit`](#fs_edit) | ファイル内の文字列を置換する |
| | [`fs_write`](#fs_write) | ファイルへ書き込む |
| | [`fs_mkdir`](#fs_mkdir) | ディレクトリを作成する |
| | [`fs_move`](#fs_move) | ファイルやディレクトリを移動・リネームする |
| | [`fs_copy`](#fs_copy) | ファイルやディレクトリをコピーする |
| | [`fs_remove`](#fs_remove) | ファイルやディレクトリを削除する |
| 検索 | [`search_grep`](#search_grep) | ファイル内容を正規表現または文字列で検索する |
| | [`search_find`](#search_find) | ファイル名・パスを glob で検索する |
| 差分の確認 | [`fs_manifest`](#fs_manifest) | ファイルの SHA-256 ハッシュ一覧を取得する |
| 特権実行 | [`exec_sudo`](#exec_sudo) | `sudo` でコマンドを実行する (Linux のみ) |
| 環境情報 | [`sys_get_info`](#sys_get_info) | 接続先の環境情報、公開ツール グループ、適用中の制限を取得する |
| ネットワーク | [`net_request_http`](#net_request_http) | 接続先から HTTP / HTTPS リクエストを送信する |
| | [`net_exchange`](#net_exchange) | 接続先から TCP / TLS / Unix ソケットで送受信する |
| | [`net_check`](#net_check) | 名前解決、TCP 接続、TLS 証明書を並行検査する |
| | [`net_list_listeners`](#net_list_listeners) | 待ち受け中のポートと所有プロセスを一覧する |
| | [`net_list_interfaces`](#net_list_interfaces) | ネットワーク インターフェイスを一覧する |
| 管理 | [`proc_list`](#proc_list) | プロセス一覧を取得する |
| | [`proc_kill`](#proc_kill) | プロセスへシグナルを送るか終了させる |
| | [`svc_list`](#svc_list) | サービス一覧を取得する |
| | [`svc_control`](#svc_control) | サービスの状態取得・起動・停止・再起動を行う |
| | [`log_query`](#log_query) | システム ログ (journald / イベント ログ) を検索・取得する |
| | [`disk_list`](#disk_list) | ディスク使用量を取得する |
| Windows のレジストリ | [`reg_read`](#reg_read) | レジストリ キー・値を読み取る (Windows のみ) |
| | [`reg_write`](#reg_write) | レジストリ キー・値を書き込む (Windows のみ) |
| | [`reg_delete`](#reg_delete) | レジストリ キー・値を削除する (Windows のみ) |
| 手元との転送 | [`transfer_push`](#transfer_push) | ローカルから接続先へファイルを転送する (`bridge` 経由時のみ) |
| | [`transfer_pull`](#transfer_pull) | 接続先からローカルへファイルを転送する (`bridge` 経由時のみ) |

Table: ツール一覧

[`--tools`](#ツールの選択---tools) および [`--read-only`](#読み取り専用---read-only) で公開されたツールのみが一覧に含まれます。以降の表で「必須」列が空欄の引数は省略可能です。

## 使い方の案内

### help_get

エージェント向けの使い方を Markdown テキストで返します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `topic` | string | | トピック名。省略時は `overview`、`all` 指定時は全トピックを連結して返す |

Table: help の引数

| topic | 内容 |
|---|---|
| `overview` | 仕組み、ツール早見表、推奨手順 (先頭に接続先サマリーを付与) |
| `files` | `fs_read`、`fs_write`、ファイル操作ツール、`fs_manifest` |
| `edit` | `fs_edit` の使い方と置換規則 |
| `search` | glob 構文、`search_grep` の出力モード、`search_find` |
| `commands` | `exec_run`、`exec_sudo`、ツールの使い分け |
| `jobs` | バックグラウンド ジョブ、待機条件 (`until`、`idle_ms`)、入力送信、疑似端末、シグナル |
| `network` | `net_*` ツールの使い方と引数例 |
| `admin` | プロセス、サービス、システム ログ、ディスク、レジストリ操作 |
| `transfer` | `transfer_push`、`transfer_pull`、および `bridge` なしでの分割転送 |
| `encoding` | 文字コードと改行コードの扱い |
| `windows` | Windows 接続先での注意点 |
| `policy` | 制限オプションとエージェント動作への影響 (先頭に接続先サマリーを付与) |
| `limits` | 出力上限、タイムアウト、エラー処理、操作ログ (先頭に接続先サマリーを付与) |

Table: help の topic

接続先サマリーには、ホスト名、OS・アーキテクチャー、ユーザー、作業ディレクトリ、既定シェル、既定文字コード、出力上限、タイムアウト、公開ツール グループ表、および適用中の制限表が含まれます。未知の `topic` を指定した場合は有効なトピック一覧を添えてエラーを返します。  
各トピックは MCP リソース `host-shell://help/<topic>` としても読み取れます。

## コマンド実行

### exec_run

コマンドを実行し、終了を待って結果を返します。シェル コマンド ラインを渡す `command` と、シェルを介さず直接起動する `argv` のいずれか一方を指定します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `command` | string | ※ | シェルに渡すコマンド ライン |
| `argv` | string[] | ※ | シェルを介さず起動するプログラムと引数 (先頭要素がプログラム) |
| `shell` | string | | `command` を解釈するシェル。省略時は OS 既定シェル ([シェル](#シェル) を参照) |
| `cwd` | string | | 作業ディレクトリ。省略時は `host-shell` の作業ディレクトリ |
| `env` | object | | 追加・上書きする環境変数 (キーと値のマップ) |
| `stdin` | string | | 標準入力へ渡すテキスト (書き込み後にクローズ。省略時は空入力) |
| `timeout_sec` | integer | | タイムアウト秒数。省略時は `--timeout`、上限は `--max-timeout` |
| `output_encoding` | string | | 標準出力・標準エラーの文字コード。省略時は `auto` ([文字コードの指定](#文字コードの指定)) |
| `stdin_encoding` | string | | `stdin` の変換先文字コード。省略時は `utf-8`。Windows の PowerShell に `utf-8` 以外を指定した場合は、PowerShell がそのコード ページで読む前提で渡す ([シェル](#シェル))。`auto` は指定不可 |

Table: exec_run の引数 (※ `command` と `argv` のいずれか一方が必須)

呼び出しごとに新しいプロセスを起動するため、前回の `cd` や環境変数変更は引き継がれません (`cwd` と `env` で指定してください)。

| 出力 | 型 | 内容 |
|---|---|---|
| `exit_code` | integer | 終了コード (シグナル終了時等は `-1`) |
| `stdout` | string | 標準出力 |
| `stderr` | string | 標準エラー |
| `stdout_truncated` | boolean | 標準出力が切り詰められた場合に `true` |
| `stderr_truncated` | boolean | 標準エラーが切り詰められた場合に `true` |
| `stdout_encoding` | string | 標準出力を UTF-8 以外から変換した場合の元文字コード |
| `stderr_encoding` | string | 標準エラーを UTF-8 以外から変換した場合の元文字コード |
| `timed_out` | boolean | タイムアウトにより強制終了した場合に `true` |
| `cancelled` | boolean | クライアントのキャンセル要求により強制終了した場合に `true` |
| `duration_ms` | integer | 実行時間 (ミリ秒) |

Table: exec_run の出力

タイムアウト時は子孫プロセスを含めて強制終了し、それまでの出力と `timed_out: true` を返します。  
リクエストに `progressToken` がある場合は 1 秒ごとに `notifications/progress` (経過秒数、出力バイト数、最終行) を送り、`notifications/cancelled` 受信時は即座にプロセス ツリーを強制終了します。

```json
{"command": "systemctl status nginx --no-pager", "timeout_sec": 10}
```

```json
{"argv": ["git", "-C", "/srv/app", "log", "-5", "--oneline"]}
```

```json
{"command": "Get-Service | Where-Object Status -eq Running", "shell": "powershell"}
```

```json
{"command": "dir C:\\work", "shell": "cmd"}
```

## 長時間の実行 (ジョブ)

ビルドやログ監視など長時間を要するコマンドは、バックグラウンド ジョブとして実行します。

```mermaid
---
title: ジョブの状態
---
stateDiagram-v2
    direction LR
    [*] --> running : job_start
    note right of running
        job_stop
        (kill 以外のシグナル)
        は running に戻る
    end note
    running --> ended : コマンド終了
    running --> ended : job_stop、タイムアウト、<br/>host-shell 終了
    ended --> [*] : 終了済みジョブが<br/>50 件を超過、または<br/>host-shell 終了
```

- ジョブ ID は `job-1`、`job-2` のように `host-shell` プロセスごとに連番で採番します。
- 入出力方式はパイプ接続 (既定) と疑似端末接続 (`pty: true`) の 2 種類です。
- 標準出力と標準エラーは単一ストリームに統合し、UTF-8 変換後に保持します (`offset` や `output_bytes` は変換後 UTF-8 のバイト数です)。
- 出力リング バッファーは直近の少なくとも `--max-output` の 4 倍 (既定 256 KiB) を保持し、それを超えた古い出力から破棄します。
- 終了済みジョブは直近 50 件まで保持し、超過分は古いものから破棄します。
- `host-shell` プロセス終了時 (SSH 切断時) には実行中の全ジョブが停止します。

### job_start

コマンドをバックグラウンドで起動し、ジョブ状態を即座に返します。引数は [`exec_run`](#exec_run) の引数に次の項目を追加したものです (`timeout_sec` 省略時は無制限、指定時も `--max-timeout` の制限を受けません)。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `stdin_open` | boolean | | 標準入力を開いたままにし、[`job_write`](#job_write) で後から送信できるようにする (`stdin` 指定時は初期入力として送信) |
| `pty` | boolean | | 疑似端末を割り当てて起動する |
| `cols` | integer | | `pty` の端末列数 (省略時は `120`) |
| `rows` | integer | | `pty` の端末行数 (省略時は `40`) |

Table: job_start に固有の引数

#### 疑似端末 (pty)

`pty: true` を指定すると、疑似端末を割り当ててコマンドを起動します。端末を要求するプログラム (`sudo`、`ssh`、REPL、`top`、パイプ接続時に出力をバッファリングするプログラムなど) に使用します。

| 項目 | Linux | Windows |
|---|---|---|
| 実装 | pty (新規セッションを作成し制御端末に設定) | ConPTY (Windows 10 1809 / Windows Server 2019 以降) |
| 環境変数 | `TERM` 未設定時に `xterm-256color` を設定 | 変更なし |
| PowerShell | | `-NonInteractive` を付けずに起動 (`Read-Host` 等が利用可能) |

Table: 疑似端末

- 標準出力・標準エラーは端末出力として統合され、入力は常に開いた状態で起動します。
- コマンド終了後も子孫プロセスが端末を開いている場合は、最大 2 秒待ってから端末を閉じます。
- 出力は仮想端末エミュレーター (`github.com/charmbracelet/x/vt`) にも入力され、`job_read` の `screen: true` で現在の画面状態を取得できます。カーソル位置照会などの端末クエリにも仮想端末が応答します。

出力スキーマは [ジョブの状態](#ジョブの状態) です。

### job_read

ジョブの出力を指定オフセットから読み取り、現在のジョブ状態とともに返します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `id` | string | ○ | ジョブ ID |
| `offset` | integer | | 読み取り開始位置 (開始からの累積バイト数。継続時は前回の `next_offset`、負値は末尾からのバイト数を指定) |
| `max_bytes` | integer | | 最大読み取りバイト数 (省略時および上限は `--max-output`) |
| `wait_sec` | integer | | 最大待機秒数 (上限 300 秒。ジョブ終了時は即座に戻る) |
| `until` | string | | `offset` 以降の出力がこの正規表現に一致するまで待機する |
| `idle_ms` | integer | | 出力到着後、このミリ秒間新しい出力が途絶えるまで待機する |
| `raw` | boolean | | `pty` ジョブで端末制御シーケンスを除去せずにそのまま返す |
| `screen` | boolean | | `pty` ジョブでストリーム出力ではなく現在の端末画面を返す |

Table: job_read の引数

`wait_sec` のみを指定した場合は、`offset` 以降に新しい出力が到着した時点で戻ります。  
`pty` ジョブの出力は既定で次の整形を行ってから返し (`until` の照合も整形後テキストに対して行います)、`raw: true` 指定時のみ未加工で返します。

- 色、カーソル移動、ウィンドウ タイトル等の ANSI エスケープ シーケンスを除去
- CRLF を LF に統一し、行途中の CR による上書き (プログレス バー等) は最終表示内容のみを残す
- バック スペースで直前文字を消去し、タブ・改行以外の制御文字を除去
- チャンク末尾で途切れたエスケープ シーケンスは手前までを返し、`next_offset` をそのシーケンスの先頭に設定

| 出力 | 型 | 内容 |
|---|---|---|
| `status` | object | [ジョブの状態](#ジョブの状態) |
| `matched` | boolean | `until` の正規表現に一致した場合に `true` |
| `output` | string | 読み取った出力 |
| `next_offset` | integer | 次回の読み取り開始位置 |
| `more` | boolean | `next_offset` 以降に未読出力が残っている場合に `true` |
| `dropped` | integer | `offset` より前でリング バッファーから破棄されたバイト数 |
| `cursor_row`、`cursor_col` | integer | `screen: true` の場合のカーソル行・列 (0 始まり) |

Table: job_read の出力

#### 画面の状態 (screen)

`screen: true` を指定すると、`top`、`vi`、`less` などの全画面プログラム向けに、現在の端末画面 (`rows` 行、末尾空白除去済み) を改行区切りで `output` に返し、カーソル位置を `cursor_row`、`cursor_col` に返します。`offset` と `max_bytes` は無視され、`wait_sec`、`until`、`idle_ms` の待機完了時点の画面と累積バイト数 (`next_offset`) を返します (`pty` 以外のジョブではエラーになります)。

```json
{"id": "job-3", "screen": true, "wait_sec": 5, "idle_ms": 500}
```

### job_write

`stdin_open` または `pty` で起動したジョブへ入力を送信します。入力のクローズや PTY の端末サイズ変更も行えます。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `id` | string | ○ | ジョブ ID |
| `input` | string | | 送信するテキスト (改行は自動付与されないため、行送信時は末尾に `\n` を含める) |
| `close` | boolean | | `input` 送信後に標準入力を閉じる (`pty` ジョブでは Linux は Ctrl-D、Windows は Ctrl-Z + Enter を送信) |
| `cols`、`rows` | integer | | `pty` ジョブの端末サイズを変更する (両方指定が必要) |

Table: job_write の引数 (`input`、`close`、`cols` と `rows` のいずれかが必須)

| 出力 | 型 | 内容 |
|---|---|---|
| `status` | object | [ジョブの状態](#ジョブの状態) |
| `bytes_written` | integer | 書き込んだバイト数 (文字コード変換後) |

Table: job_write の出力

`input` は `job_start` の `stdin_encoding` に変換して送信します。子プロセスが読み取らず 10 秒以内に書き込みが完了しない場合はエラーを返します (残りはバックグラウンドで送信を継続します)。  
`pty` ジョブでは制御キーを文字コードとして送信します (Ctrl-C は `\u0003`、Ctrl-D は `\u0004`、Esc は `\u001b`、上矢印は `\u001b[A`)。

```json
{"id": "job-3", "input": "y\n"}
```

```json
{"id": "job-4", "input": "\u0003"}
```

### job_stop

ジョブにシグナルを送るかプロセス ツリーごと強制終了し、終了を待ってから状態を返します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `id` | string | ○ | ジョブ ID |
| `signal` | string | | `kill` (既定)、または Linux で `interrupt`、`terminate`、`hangup`、`quit`、`stop`、`continue`、`usr1`、`usr2` (`pty` ジョブでは Windows でも `interrupt` が利用可能) |
| `wait_sec` | integer | | 終了待機秒数 (省略時は `5`) |

Table: job_stop の引数

| signal | 動作 |
|---|---|
| `kill` | 子孫プロセスを含めて強制終了する ([終了のさせ方](#終了のさせ方)) |
| `interrupt` (`pty` ジョブ) | 端末へ Ctrl-C を送信する (Linux / Windows 共通) |
| その他 (Linux) | ジョブのプロセス グループへシグナルを送信する |

Table: job_stop のシグナル

`kill` 以外のシグナルではプロセスが終了しない場合があるため、返却された `status.running` を確認してください。終了済みジョブに対する `kill` はエラーになりませんが、それ以外のシグナルはエラーになります。

### job_list

現在のセッションで開始した全ジョブの状態を開始順に返します (引数なし)。

| 出力 | 型 | 内容 |
|---|---|---|
| `jobs` | object[] | [ジョブの状態](#ジョブの状態) の配列 |

Table: job_list の出力

### ジョブの状態

| 項目 | 型 | 内容 |
|---|---|---|
| `id` | string | ジョブ ID |
| `command` | string | 実行コマンド (`argv` 指定時は空白連結文字列) |
| `pid` | integer | プロセス ID |
| `pty` | boolean | 疑似端末ジョブの場合に `true` |
| `running` | boolean | 実行中の場合に `true` |
| `stdin_open` | boolean | 標準入力が開いており `job_write` 可能な場合に `true` |
| `exit_code` | integer | 終了コード (終了時のみ) |
| `signal` | string | 直近の `job_stop` で送信したシグナル (`kill` を除く) |
| `output_encoding` | string | 出力を UTF-8 以外から変換した場合の元文字コード |
| `error` | string | プロセス待機処理で発生したエラー (通常は省略) |
| `started_at` | string | 開始時刻 (RFC 3339) |
| `ended_at` | string | 終了時刻 (RFC 3339、終了時のみ) |
| `output_bytes` | integer | これまでの累積出力バイト数 |

Table: ジョブの状態

## ファイル操作

### fs_list

ディレクトリ内の項目を名前順で返します。シンボリック リンクはリンク自体の情報を返し、リンク先はたどりません。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `path` | string | | ディレクトリのパス (省略時は作業ディレクトリ) |
| `max_entries` | integer | | 最大取得件数 (省略時は `1000`) |

Table: fs_list の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `path` | string | ディレクトリの絶対パス |
| `entries` | object[] | 項目配列 (`name`、`type` [`file`/`dir`/`symlink`/`other`]、`size`、`mode`、`mod_time` [RFC 3339]) |
| `truncated` | boolean | `max_entries` で打ち切った場合に `true` |

Table: fs_list の出力

### fs_stat

ファイルまたはディレクトリの属性を返します。対象がシンボリック リンクの場合はリンク自体の属性とリンク先 (`link_target`) を返します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `path` | string | ○ | 対象パス |

Table: fs_stat の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `path` | string | 絶対パス |
| `entry` | object | 属性 ([`fs_list`](#fs_list) の要素と同一形式) |
| `link_target` | string | シンボリック リンクのリンク先 |

Table: fs_stat の出力

### fs_read

ファイルを読み取ります。引数に応じて次の 3 モードで動作します。

| モード | 指定方法 | 用途 |
|---|---|---|
| 行範囲 (テキスト) | `line_start` を指定 (`encoding: "text"`) | ソース コードやログの特定行範囲を読む |
| バイト範囲 (テキスト) | `line_start` を省略 (`encoding: "text"`) | 先頭または指定オフセットから順に読む |
| バイト範囲 (base64) | `encoding: "base64"` を指定 | バイナリ ファイルをそのまま読み取る |

Table: fs_read の読み方

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `path` | string | ○ | ファイルのパス |
| `encoding` | string | | `text` (既定) または `base64` |
| `charset` | string | | `text` モード時の文字コード (省略時は `auto`、[文字コードの指定](#文字コードの指定)) |
| `line_start` | integer | | 読み取り開始行 (1 始まり。指定時は `offset` と `length` を無視) |
| `line_count` | integer | | 読み取る行数 (省略時は `2000`) |
| `line_numbers` | boolean | | 各行の先頭に 6 桁右寄せの行番号とタブを付与する (`line_start` 指定時のみ有効) |
| `offset` | integer | | 読み取り開始バイト位置 (省略時は `0`) |
| `length` | integer | | 最大読み取りバイト数 (省略時は上限まで) |

Table: fs_read の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `path` | string | 絶対パス |
| `size` | integer | ファイル サイズ (バイト) |
| `encoding` | string | `text` または `base64` |
| `charset` | string | `text` モードで使用した文字コード (`fs_write` に渡して形式を維持可能) |
| `bom` | boolean | 先頭に BOM がある場合に `true` (BOM 自体は `content` に含めない) |
| `newline` | string | `content` 内の改行種別 (`lf`、`crlf`、`cr`、`mixed`) |
| `content` | string | 読み取った内容 |
| `eof` | boolean | ファイル末尾まで到達した場合に `true` |
| `next_offset` | integer | バイト範囲モードでの次回読み取り開始位置 |
| `next_line` | integer | 行範囲モードでの次回読み取り開始行 (`eof: false` の場合のみ) |
| `truncated` | boolean | 要求範囲の途中で出力上限に達した場合に `true` |

Table: fs_read の出力

- `text` モードでは UTF-8 変換後のサイズで `--max-output` まで読み取ります。行範囲モードでは上限手前の行までを返し、1 行で上限を超える場合はその行を切り詰めて次行へ進めます。
- `base64` モードではエンコード後サイズが `--max-output` に収まるよう、`--max-output` の 3/4 (既定 48 KiB) まで読み取ります。
- `line_numbers: true` で付与される行番号接頭辞 (`     12\t`) はファイル内容ではないため、`fs_edit` や `fs_write` には渡さないでください。

```json
{"path": "/var/log/nginx/error.log", "line_start": 1, "line_count": 200}
```

```json
{"path": "C:/work/script.bat", "charset": "shift_jis", "line_start": 1}
```

```json
{"path": "C:/tools/app.exe", "encoding": "base64", "offset": 0}
```

### fs_write

ファイルへ書き込みます。既存ファイルの部分変更には、ファイル全体を再送する `fs_write` ではなく [`fs_edit`](#fs_edit) を使用してください。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `path` | string | ○ | ファイルのパス |
| `content` | string | ○ | 書き込む内容 |
| `encoding` | string | | `text` (既定) または `base64` |
| `charset` | string | | `text` モード時の文字コード (省略時は `utf-8`、`auto` は指定不可) |
| `newline` | string | | `text` モード時の改行変換: `keep` (既定、無変換)、`lf`、`crlf` |
| `bom` | boolean | | 先頭に BOM を付与する (`utf-8`、`utf-16le`、`utf-16be` かつファイル先頭への書き込み時のみ有効) |
| `mode` | string | | `overwrite` (既定)、`append`、`create` |
| `offset` | integer | | 書き込み開始バイト位置 (ファイルを切り詰めず指定範囲のみを上書き) |
| `make_dirs` | boolean | | 親ディレクトリが存在しない場合に権限 `0755` で作成する |
| `perm` | string | | 新規作成時の 8 進数パーミッション (例: `0600`、既定 `0644`。Windows では無視) |
| `atomic` | boolean | | 一時ファイルへ書き込んでからリネームで置き換える (`mode: "overwrite"` かつ `offset` 未指定時のみ有効) |

Table: fs_write の引数

| mode | `offset` 未指定時 | `offset` 指定時 |
|---|---|---|
| `overwrite` (既定) | なければ作成し、あれば切り詰めて全体を書き換える | なければ作成し、切り詰めずに `offset` から書き込み長のみを上書きする |
| `append` | なければ作成し、あれば末尾へ追記する | 指定不可 (エラー) |
| `create` | なければ作成し、既存ならエラーにする | なければ作成して `offset` から書き込み、既存ならエラーにする |

Table: fs_write の mode と offset

`offset` に `0` を指定した場合も、未指定時と異なりファイルを切り詰めません。ファイル末尾より後ろの `offset` を指定すると、末尾から `offset` までが 0 バイトで埋められます。  
`text` モードでは改行変換、文字コード変換、BOM 付与の順に処理し、`charset` で表現できない文字が含まれる場合は文字と位置を示してエラーとし、ファイルへは書き込みません。

#### 原子的な置き換え (atomic)

既定の `overwrite` は既存ファイルをその場で書き換えるため、所有者・権限・リンク関係は維持されますが、書き込み中や中断時に不完全な内容が見えることがあります。  
`atomic: true` を指定すると (`fs_write` および `fs_edit`)、次の手順で原子的に置き換えます。

1. 対象がシンボリック リンクの場合はリンク先の実体ファイルを対象とします。
2. 同一ディレクトリに一時ファイル (`.<名前>.host-shell-tmp-<乱数>`) を作成し、内容を書き込んで `fsync` します。
3. 既存ファイルのモード・所有者・グループを引き継ぎます (非 root で所有者が異なる等で引き継げない場合は一時ファイルを削除してエラーにします)。
4. リネームで対象ファイルを置き換えます (失敗時は一時ファイルを削除します)。

ACL、拡張属性、ハード リンクは引き継がれません。Windows では他プロセスが開いているファイルを置き換えられない場合があります。

| 出力 | 型 | 内容 |
|---|---|---|
| `path` | string | 絶対パス |
| `bytes_written` | integer | 書き込んだバイト数 |
| `size` | integer | 書き込み後のファイル サイズ (バイト) |

Table: fs_write の出力

```json
{"path": "/srv/data/image.bin", "content": "AAECAw==", "encoding": "base64", "offset": 1048576}
```

```json
{"path": "C:/work/run.bat", "content": "@echo off\necho 完了\n", "charset": "shift_jis", "newline": "crlf"}
```

### fs_edit

既存ファイル内の完全一致する文字列を置換します。ファイルの文字コード、BOM、改行コードを判定し、同じ形式で書き戻します。  
単一置換 (`old_string` と `new_string`) または複数置換 (`edits`) のいずれか一方を指定します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `path` | string | ○ | ファイルのパス |
| `old_string` | string | ※ | 置換対象の文字列 (空白、インデント、改行を含め完全一致が必要) |
| `new_string` | string | | 置換後の文字列 (空文字列で削除) |
| `replace_all` | boolean | | 全一致箇所を置換する (省略時は一意に 1 箇所のみ一致しなければエラー) |
| `edits` | object[] | ※ | 複数置換の配列 (`old_string`、`new_string`、`replace_all`)。先頭から順に前の置換結果へ適用する |
| `charset` | string | | ファイルの文字コード (省略時は `auto`) |
| `dry_run` | boolean | | 置換結果の検証と抜粋生成のみを行い、ファイルへは書き込まない |
| `atomic` | boolean | | 一時ファイル経由で原子的に置き換える ([`fs_write`](#原子的な置き換え-atomic) と同じ) |

Table: fs_edit の引数 (※ `old_string` と `edits` のいずれか一方が必須)

| 出力 | 型 | 内容 |
|---|---|---|
| `path` | string | 絶対パス |
| `replacements` | integer | 置換した総箇所数 |
| `lines` | integer[] | 置換後の各箇所の開始行番号 (最大 50 件) |
| `snippet` | string | 置換箇所と前後 2 行の行番号付き抜粋 (最大 80 行) |
| `charset` | string | 読み書きに使用した文字コード |
| `bom` | boolean | BOM の有無 (元の状態を維持) |
| `newline` | string | 置換後の改行種別 |
| `size` | integer | 置換後のファイル サイズ (バイト) |
| `sha256` | string | 置換後のファイルの SHA-256 |
| `dry_run` | boolean | `dry_run` 指定時に `true` |

Table: fs_edit の出力

- いずれかの置換が失敗した場合はファイルを一切変更しません。
- `old_string` が見つからない場合、行前後の空白を除けば一致する行があればその行番号をエラーに含めます。複数箇所に一致して `replace_all` が未指定の場合は一致行番号 (最大 10 件) を返します。
- CRLF ファイルでは `old_string`・`new_string` 内の LF を CRLF として照合・書き込みします。
- 文字コードはファイル全体で単一のものとして判定し、デコード後に再エンコードして元バイト列と一致しない場合 (混在エンコーディングなど) は破損防止のためエラーにします。
- 対象ファイルの上限は 32 MiB です。

```json
{"path": "/etc/app/config.yml", "old_string": "  port: 8080\n", "new_string": "  port: 9090\n"}
```

```json
{"path": "src/main.c", "edits": [
  {"old_string": "#include <stdio.h>\n", "new_string": "#include <stdio.h>\n#include <string.h>\n"},
  {"old_string": "MAX_LEN 64", "new_string": "MAX_LEN 256", "replace_all": true}
]}
```

### fs_mkdir

ディレクトリを作成します (親ディレクトリも権限 `0755` で再帰的に作成します)。すでにディレクトリが存在する場合はエラーにならず `created: false` を返し、同名ファイルが存在する場合はエラーにします。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `path` | string | ○ | 作成するディレクトリのパス |

Table: fs_mkdir の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `path` | string | 絶対パス |
| `created` | boolean | 新規作成した場合に `true`、既存だった場合に `false` |

Table: fs_mkdir の出力

### fs_move

ファイルまたはディレクトリを移動・リネームします。ファイル システム (Windows ではドライブ) をまたぐ場合はコピー後に移動元を削除します (コピー途中で失敗した場合は移動元を残します)。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `source` | string | ○ | 移動元のパス |
| `destination` | string | ○ | 移動先のパス (親ディレクトリではなく移動後の完全パス) |
| `overwrite` | boolean | | 移動先に既存ファイルがある場合に上書きする (移動先がディレクトリの場合はエラー) |
| `make_dirs` | boolean | | 移動先の親ディレクトリが存在しない場合に作成する |

Table: fs_move の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `source` | string | 移動元の絶対パス |
| `destination` | string | 移動先の絶対パス |
| `copied` | boolean | ファイル システムをまたぎコピー＆削除で移動した場合に `true` |

Table: fs_move の出力

### fs_copy

ファイルをコピーします。`recursive: true` を指定するとディレクトリを再帰的にコピーします。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `source` | string | ○ | コピー元のパス |
| `destination` | string | ○ | コピー先のパス (コピー後の完全パス) |
| `recursive` | boolean | | ディレクトリをコピーする場合に必須 |
| `overwrite` | boolean | | コピー先の既存ファイルを上書きする (ディレクトリ同士の場合は内容をマージする) |
| `make_dirs` | boolean | | コピー先の親ディレクトリが存在しない場合に作成する |

Table: fs_copy の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `source` | string | コピー元の絶対パス |
| `destination` | string | コピー先の絶対パス |
| `files` | integer | コピーしたファイル数 |
| `dirs` | integer | 作成したディレクトリ数 |
| `symlinks` | integer | 再作成したシンボリック リンク数 |
| `bytes` | integer | コピーした総バイト数 |

Table: fs_copy の出力

ファイルのパーミッションと更新時刻を引き継ぎ、シンボリック リンクは同じリンク先を指すリンクとして再作成します (通常ファイル・ディレクトリ・シンボリック リンク以外の特殊ファイルはスキップします)。コピー先がコピー元ディレクトリの内部にある場合はエラーにします。

### fs_remove

ファイル、シンボリック リンク、ディレクトリを削除します。ファイル システムのルート (`/`、`C:\` 等) および `host-shell` 実行ユーザーのホーム ディレクトリ自体の削除は拒否します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `path` | string | ○ | 削除対象のパス (シンボリック リンクはリンク自体を削除) |
| `recursive` | boolean | | 空でないディレクトリを削除する場合に必須 |
| `missing_ok` | boolean | | 対象が存在しない場合もエラーにしない |

Table: fs_remove の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `path` | string | 絶対パス |
| `removed` | boolean | 削除した場合に `true` (`missing_ok` で不在だった場合は `false`) |
| `type` | string | 削除した対象の種類 (`file`、`dir`、`symlink`、`other`) |

Table: fs_remove の出力

## 検索

`search_grep`、`search_find`、`fs_manifest` は指定ディレクトリ配下を名前順で再帰走査します。

- シンボリック リンクはたどりません。
- `exclude_dirs` に指定されたディレクトリ名 (省略時は `[".git", ".hg", ".svn"]`、空配列 `[]` 指定時は除外なし) には降りません。
- 権限不足等で読み取れなかった項目はスキップし、件数を `skipped` に返します。
- 結果のパスは引数の `path` を基準とした形式で返します (`path` が相対パスなら結果も相対パス)。

### glob

`search_grep`・`fs_manifest` の `include`、および `search_find` の `pattern` で使用する glob 構文は次のとおりです (Windows では大文字・小文字を区別せず、`\` も区切りとして扱います)。

| 書式 | 意味 |
|---|---|
| `/` を含まない (`*.go`) | ベース名 (パスの末尾要素) と照合 |
| `/` を含む (`src/**/*.ts`) | `path` からの相対パス (`/` 区切り、先頭の `./` は無視) と照合 |
| `*`、`?` | 単一パス要素内の任意文字列・任意 1 文字 |
| `**` | 0 個以上のディレクトリ階層 (`**/test/*.py`) |
| `[abc]`、`[a-z]` | 文字クラス |
| `{a,b}` | いずれかの文字列 (`*.{yml,yaml}`、入れ子不可) |

Table: glob の構文

### search_grep

ファイル内容を正規表現またはリテラル文字列で検索します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `pattern` | string | ○ | 正規表現 ([RE2 構文](https://github.com/google/re2/wiki/Syntax)、後方参照・先読みは非対応) |
| `path` | string | | 検索対象のディレクトリまたはファイル (省略時は作業ディレクトリ) |
| `include` | string | | 対象ファイルの [glob](#glob) (例: `*.go`、`src/**/*.ts`) |
| `exclude_dirs` | string[] | | 走査から除外するディレクトリ名 |
| `gitignore` | boolean | | `.gitignore` 対象を除外する (省略時は `true`、[.gitignore の扱い](#gitignore-の扱い)) |
| `ignore_case` | boolean | | 大文字・小文字を区別しない |
| `literal` | boolean | | `pattern` をメタ文字なしの固定文字列として検索する |
| `output_mode` | string | | `content` (既定、一致行と前後行)、`files` (一致ファイル パスのみ)、`count` (ファイル別一致行数) |
| `context` | integer | | `content` モードで一致行の前後に含める行数 (`grep -C`) |
| `before`、`after` | integer | | 一致行の前・後に含める行数 (`context` より優先、`grep -B` / `-A`) |
| `max_matches` | integer | | 最大取得件数 (`content` は一致行数、`files`・`count` はファイル数。省略時は `200`) |
| `max_file_bytes` | integer | | このサイズを超えるファイルをスキップする (省略時は 10 MiB) |
| `charset` | string | | ファイルの文字コード (省略時は `auto`) |
| `multiline` | boolean | | 複数行にまたがるパターンをファイル全体に対して照合する |

Table: search_grep の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `matches` | object[] | `content` モード時の一致配列 (`path`、`line`、`end_line` [`multiline` 時]、`text` [最大 500 バイト]、`before`、`after`、`charset`) |
| `files` | object[] | `files`・`count` モード時の一致ファイル配列 (`path`、`count`) |
| `files_scanned` | integer | 検査したファイル数 |
| `files_matched` | integer | 一致があったファイル数 |
| `skipped` | integer | 読み取れなかった項目数 |
| `skipped_large` | integer | サイズ上限超過でスキップしたファイル数 |
| `truncated` | boolean | `max_matches` で打ち切った場合に `true` |

Table: search_grep の出力

通常ファイル以外、先頭 8000 バイトに NUL を含むバイナリ ファイル (UTF-16 BOM 付きまたは `charset` に UTF-16 指定時を除く)、`max_file_bytes` 超過ファイル、および `gitignore: true` 時の無視対象ファイルはスキップします。1 MiB を超える行があるファイルはその行の手前までを検索します。

#### 複数の行にまたがる検索 (multiline)

`multiline: true` 指定時はファイル全体を UTF-8 に変換し、RE2 の `(?sm)` モード (`.` が改行にも一致、`^`・`$` が各行頭・行末に一致) で照合します。一致ごとに開始行 (`line`)、終了行 (`end_line`)、一致テキスト (`text`、最大 500 バイト) を返します (空文字一致は除外、32 MiB 超過ファイルは `skipped_large` に計上)。

```json
{"pattern": "func \\w+\\(ctx context\\.Context.*?\\n\\}", "path": "/srv/app", "include": "*.go", "multiline": true}
```

```json
{"pattern": "TODO", "path": "/srv/app", "include": "*.{c,h}", "output_mode": "count"}
```

```json
{"pattern": "connect(", "literal": true, "path": "/srv/app/src", "context": 3}
```

### .gitignore の扱い

`search_grep` と `search_find` は既定 (`gitignore: true`) で次の規則ファイルを順に評価し、無視対象をスキップします (グローバルの `core.excludesFile` は読み取りません)。

1. リポジトリの `.git/info/exclude` (検索起点がリポジトリ内の場合)
2. リポジトリ ルート (`.git` があるディレクトリ) から検索起点までの各階層の `.gitignore`
3. 走査中に降りる各ディレクトリの `.gitignore`

深い階層の規則および同一ファイル内の後方の行が優先されます (`!pattern` による否定、`pattern/` によるディレクトリ限定、`**` に対応。無視されたディレクトリ自体には降りないため、その内部のファイルを `!` で復活させることはできません)。

### search_find

ファイル名または相対パスが [glob](#glob) に一致する項目を検索します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `pattern` | string | | glob パターン (省略時は全項目) |
| `path` | string | | 検索起点ディレクトリ (省略時は作業ディレクトリ) |
| `type` | string | | `file`、`dir`、または省略 (両方) |
| `exclude_dirs` | string[] | | 走査から除外するディレクトリ名 |
| `gitignore` | boolean | | `.gitignore` 対象を除外する (省略時は `true`) |
| `sort` | string | | `path` (既定、走査順) または `mtime` (更新日時の新しい順、最大 10 万件まで収集してソート) |
| `max_results` | integer | | 最大取得件数 (省略時は `1000`) |

Table: search_find の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `paths` | string[] | 一致したパスの配列 (`path` 自体は含めない) |
| `skipped` | integer | 読み取れなかった項目数 |
| `truncated` | boolean | `max_results` (または `mtime` ソート時の 10 万件上限) で打ち切った場合に `true` |

Table: search_find の出力

## 差分の確認

### fs_manifest

指定ディレクトリ配下の通常ファイル (または単一ファイル) の相対パス、サイズ、パーミッション、SHA-256 ハッシュの一覧を返します。ローカルのソース ツリーとハッシュを比較し、差分ファイルのみを特定する用途に使用します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `path` | string | | 基準ディレクトリまたは単一ファイル (省略時は作業ディレクトリ) |
| `include` | string | | 対象ファイルの [glob](#glob) |
| `exclude_dirs` | string[] | | 走査から除外するディレクトリ名 (省略時は `[".git", ".hg", ".svn"]`) |
| `max_files` | integer | | 最大取得ファイル数 (省略時は `2000`) |

Table: fs_manifest の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `root` | string | 基準ディレクトリの絶対パス |
| `files` | object[] | ファイル情報の配列 (`path` [常に `/` 区切り]、`size`、`mode` [8 進数文字列]、`sha256` [小文字 16 進数]) |
| `skipped` | integer | 読み取れなかった項目数 |
| `truncated` | boolean | `max_files` で打ち切った場合に `true` |

Table: fs_manifest の出力

## 特権実行

### exec_sudo

Linux の接続先で `sudo` 経由のコマンドを実行します (Windows では公開されません)。出力スキーマは [`exec_run`](#exec_run) と同一です。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `command` | string | ○ | 実行するコマンド ライン |
| `user` | string | | 実行ユーザー (省略時は `root`) |
| `cwd` | string | | 作業ディレクトリ |
| `stdin` | string | | コマンドの標準入力へ渡すテキスト |
| `password` | string | | `sudo` パスワード (`sudo` が要求した場合のみ askpass 経由で渡す) |
| `timeout_sec` | integer | | タイムアウト秒数 (省略時は `--timeout`、上限は `--max-timeout`) |
| `output_encoding`、`stdin_encoding` | string | | 入出力の文字コード ([`exec_run`](#exec_run) と同じ) |

Table: exec_sudo の引数

`password` 省略時は `sudo -n [-u <user>] -- sh -c '<command>'` を実行します。パスワードが必要な設定の場合はエラー終了し、`stderr` にメッセージが返ります。特定コマンドのみを `NOPASSWD` で許可している環境では、`exec_sudo` ではなく `exec_run` の `argv` で `sudo -n` を直接呼び出してください ([運用ガイド](operations.md#sudo-の設定) を参照)。

```json
{"argv": ["sudo", "-n", "/usr/bin/systemctl", "restart", "nginx"]}
```

#### パスワードを渡す場合

`password` 指定時は、`host-shell` 自身を `SUDO_ASKPASS` ヘルパーとして `sudo -A [-u <user>] -- sh -c '<command>'` を実行します。

1. 呼び出しごとに権限 `0700` の一時ディレクトリと権限 `0600` の名前付きパイプ (FIFO) を作成し、パスワードを最大 3 回分書き込みます。
2. `SUDO_ASKPASS` に `host-shell` のパス、`HOST_SHELL_ASKPASS` に FIFO のパスを設定して `sudo` を起動します (環境変数自体にはパスワードを含めません)。
3. `sudo` がパスワードを要求したときのみヘルパーが起動して FIFO から 1 行読み取って返し、終了後に FIFO と一時ディレクトリを削除します。

これにより `NOPASSWD` 環境や認証キャッシュ有効時でも、実行コマンドの標準入力にパスワードが混入しません。操作ログには `password: "<redacted>"` と記録され、出力 (`stdout`、`stderr`) にパスワード文字列が含まれた場合も `--redact` の有無にかかわらず `[REDACTED]` に置換されます。

## 環境情報

### sys_get_info

接続先の環境情報、公開ツール グループ、適用中のポリシー制限を返します (引数なし)。

| 出力 | 型 | 内容 |
|---|---|---|
| `os`、`arch` | string | OS (`linux` / `windows`) とアーキテクチャー (`amd64` / `arm64`) |
| `hostname` | string | ホスト名 |
| `user`、`uid` | string | ユーザー名 (Windows は `DOMAIN\user`) と UID (Windows は SID) |
| `home`、`cwd` | string | ホーム ディレクトリと `host-shell` の作業ディレクトリ |
| `path_separator` | string | パス区切り文字 (`/` または `\`) |
| `default_shell` | string | `exec_run` の既定シェル |
| `console_encoding`、`file_encoding` | string | コンソールとテキスト ファイルの既定文字コード ([接続先の既定の文字コード](#接続先の既定の文字コード)) |
| `num_cpu` | integer | 論理 CPU 数 |
| `pid`、`executable` | integer、string | `host-shell` のプロセス ID と実行ファイル パス |
| `version`、`go_version` | string | `host-shell` のバージョンとビルド時の Go バージョン |
| `tool_groups` | string[] | 公開中のツール グループ一覧 |
| `restrictions` | string[] | 適用中の制限一覧 (`項目: 値`、制限がない場合は省略) |

Table: sys_get_info の出力

## ネットワーク

ネットワーク ツールはすべて接続先マシンから通信を発信します (`localhost` は接続先自身を指します)。宛先は [`--allow-host`](#ネットワークの宛先---allow-host) で制限できます。

### net_request_http

HTTP / HTTPS リクエストを送信し、ステータス、ヘッダー、レスポンス本文を返します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `url` | string | ○ | リクエスト URL (`http` または `https`) |
| `method` | string | | HTTP メソッド (省略時は `GET`) |
| `headers` | object | | リクエスト ヘッダー (`Host` も指定可能) |
| `body` | string | | リクエスト本文 (`body_file` と排他) |
| `body_encoding` | string | | `text` (既定、UTF-8) または `base64` |
| `body_file` | string | | リクエスト本文として送信する接続先ファイルのパス |
| `unix_socket` | string | | 接続先とする Unix ドメイン ソケットのパス (例: `/var/run/docker.sock`) |
| `timeout_sec` | integer | | タイムアウト秒数 (省略時は `--timeout`、上限は `--max-timeout`) |
| `no_redirects` | boolean | | リダイレクトを追跡せず 3xx 応答をそのまま返す (既定は最大 10 回追跡) |
| `insecure` | boolean | | TLS 証明書の検証をスキップする |
| `ca_cert` | string | | 追加で信頼する CA 証明書 (PEM) の接続先パス |
| `client_cert`、`client_key` | string | | mTLS 用クライアント証明書・秘密鍵 (PEM) の接続先パス |
| `proxy` | string | | プロキシ URL または `none` (省略時は環境変数 `HTTP_PROXY` 等に従う) |
| `response_encoding` | string | | `auto` (既定)、`text`、`base64` |
| `max_bytes` | integer | | レスポンス本文の最大取得バイト数 (省略時および上限は `--max-output`) |
| `save_to` | string | | レスポンス本文を結果に含めず接続先ファイルへ直接保存する (サイズ上限なし) |

Table: net_request_http の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `status`、`status_text`、`proto` | integer、string、string | ステータス コード、ステータス文字列、プロトコル (`HTTP/1.1` 等) |
| `headers` | object | レスポンス ヘッダー (複数値は `, ` で連結) |
| `body`、`body_encoding`、`body_charset` | string | 本文、エンコーディング (`text` / `base64`)、変換元文字コード |
| `body_bytes`、`body_truncated` | integer、boolean | 受信バイト数と切り詰め有無 |
| `saved_to` | string | `save_to` の保存先絶対パス |
| `final_url`、`redirects` | string、string[] | リダイレクト追跡時の最終 URL と経由 URL 一覧 |
| `tls` | object | TLS バージョンとサーバー証明書情報 (`subject`、`issuer`、`dns_names`、`not_before`、`not_after`、`days_left`、`verified`、`verify_error`) |
| `duration_ms` | integer | 所要時間 (ミリ秒) |

Table: net_request_http の出力

```json
{"url": "http://localhost:8080/health"}
```

```json
{"url": "http://localhost/v1.43/containers/json", "unix_socket": "/var/run/docker.sock"}
```

### net_exchange

TCP、TLS、または Unix ドメイン ソケットで接続してデータを送受信します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `host` | string | | ホスト名または IP アドレス (省略時は `localhost`) |
| `port` | integer | ※ | ポート番号 |
| `unix_socket` | string | ※ | `host`・`port` の代わりに接続する Unix ドメイン ソケットのパス |
| `tls`、`insecure` | boolean | | TLS 接続の使用有無と証明書検証スキップ |
| `send`、`send_encoding` | string | | 接続後に送信するデータと形式 (`text` [既定] / `base64`) |
| `close_write` | boolean | | 送信後に書き込み側を half-close する |
| `until` | string | | 受信データがこの正規表現に一致した時点で受信を終了する |
| `idle_ms` | integer | | 無通信待機ミリ秒数 (省略時は `1000`) |
| `timeout_sec` | integer | | 全体タイムアウト秒数 (省略時は `10`) |
| `max_bytes` | integer | | 最大受信バイト数 |
| `receive_encoding` | string | | `auto` (既定、有効な UTF-8 なら `text`、それ以外は `base64`)、`text`、`base64` |

Table: net_exchange の引数 (※ `port` と `unix_socket` のいずれか一方が必須)

出力は `remote_addr`、`sent_bytes`、`received`、`receive_encoding`、`received_bytes`、`matched`、`closed_by_peer`、`truncated`、`tls`、`duration_ms` です。

```json
{"port": 6379, "send": "PING\r\n", "until": "\r\n"}
```

### net_check

複数の宛先に対して名前解決、TCP 接続、TLS ハンドシェイク・証明書確認を並行実行します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `targets` | string[] | ○ | 宛先配列 (最大 100 件。`host:port`、`host` [名前解決のみ]、または URL [`https://` は TLS も検査]) |
| `tls` | boolean | | 全宛先で TLS ハンドシェイクと証明書検査を行う |
| `timeout_ms` | integer | | 宛先ごとのタイムアウト (ミリ秒、省略時は `5000`) |

Table: net_check の引数

出力の `results` 各要素には `target`、`host`、`port`、`addresses`、`dns_ms`、`connected`、`connect_ms`、`tls` ([`net_request_http`](#net_request_http) と同一形式)、`error` が含まれます。

### net_list_listeners

待ち受け中の TCP・UDP ソケットと所有プロセスを返します (Linux は `/proc/net/{tcp,tcp6,udp,udp6}` と `/proc/<pid>/fd`、Windows は `netstat -ano` と `tasklist` から取得)。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `port` | integer | | 指定ポート番号のみに絞り込む |
| `protocol` | string | | `tcp` または `udp` |
| `process` | string | | プロセス名にこの文字列を含むもののみ (大文字・小文字区別なし) |

Table: net_list_listeners の引数

出力の `listeners` 各要素は `protocol` (`tcp`、`tcp6`、`udp`、`udp6`)、`address`、`port`、`pid`、`process` です (Linux で非 root 実行時は他ユーザー所有プロセスの `pid` が空になり、`note` にその旨が入ります)。

### net_list_interfaces

ネットワーク インターフェイス一覧 (`name`、`index`、`mtu`、`mac`、`flags`、`addresses` [CIDR 表記]) を返します (引数なし)。

## 管理

プロセス、サービス、システム ログ、ディスクを OS 共通の形式で扱うツール群 (`admin` グループ) です。

### proc_list

プロセス一覧を返します (Linux は `/proc`、Windows は Win32 API、macOS は `ps` から取得)。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `filter` | string | | プロセス名またはコマンド ラインにこの文字列を含むもののみ (大文字・小文字区別なし) |
| `pid` | integer | | 指定 PID とその子孫プロセスのみを返す |
| `max` | integer | | 最大取得件数 (省略時は `500`) |

Table: proc_list の引数

| 項目 | 型 | 内容 | Linux | Windows |
|---|---|---|---|---|
| `pid`、`ppid` | integer | プロセス ID と親プロセス ID | `/proc/<pid>/stat` | Toolhelp |
| `name` | string | プロセス名 | `/proc/<pid>/stat` (`comm`) | 実行ファイル名 |
| `command_line` | string | コマンド ライン | `/proc/<pid>/cmdline` | `NtQueryInformationProcess` |
| `user` | string | 実行ユーザー | `/proc/<pid>/status` (UID 解決) | プロセス トークン (`DOMAIN\user`) |
| `state` | string | プロセス状態 (`R`、`S`、`D`、`Z`、`T` 等) | `/proc/<pid>/stat` | なし |
| `start_time` | string | 開始時刻 (RFC 3339) | `/proc/<pid>/stat` | `GetProcessTimes` |
| `memory_bytes` | integer | 物理メモリ使用量 (RSS / ワーキング セット) | `/proc/<pid>/statm` | `GetProcessMemoryInfo` |

Table: proc_list の項目

出力は `processes` (PID 順)、`total` (絞り込み後の総数)、`truncated` です。権限不足等で取得できないフィールドは省略されます。

### proc_kill

指定した PID のプロセスにシグナルを送るか終了させます (`job_start` で起動したジョブには [`job_stop`](#job_stop) を使用してください)。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `pid` | integer | ○ | プロセス ID |
| `signal` | string | | Linux: `terminate` (既定)、`kill`、`interrupt`、`hangup`、`quit`、`stop`、`continue`、`usr1`、`usr2`。Windows: `kill` (既定、これのみ) |
| `tree` | boolean | | 子孫プロセスも含めて対象にする (Linux は `/proc` 走査、Windows は `taskkill /T /F`) |
| `sudo` | boolean | | Linux で `sudo -n kill` を使用する |

Table: proc_kill の引数

出力は `signaled` (送信対象 PID 配列) と `exited` (3 秒以内にプロセスが消滅した場合に `true`) です。

### svc_list

サービス一覧を返します (最大 2000 件、超過時は `truncated: true`)。Linux は `systemctl list-units` と `list-unit-files`、Windows はサービス コントロール マネージャー (管理者権限不要の読み取り権のみ) から取得します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `filter` | string | | サービス名または説明にこの文字列を含むもののみ (大文字・小文字区別なし) |
| `state` | string | | `running`、`stopped`、`failed`、`all` (既定) |
| `user` | boolean | | Linux でユーザーの systemd ユニット (`systemctl --user`) を対象にする |

Table: svc_list の引数

| 項目 | Linux | Windows |
|---|---|---|
| `name`、`description` | ユニット名 (`nginx.service`) と説明 | サービス名と表示名 |
| `state` | `running`、`stopped`、`failed`、`starting`、`stopping` | `running`、`stopped`、`starting`、`stopping`、`paused` |
| `sub_state` | systemd の SUB 状態 (`running`、`exited`、`dead` 等) | |
| `start_mode` | `enabled`、`disabled`、`static`、`masked` 等 | `auto`、`delayed-auto`、`manual`、`disabled` |
| `pid` | | プロセス ID |

Table: svc_list の項目

### svc_control

単一サービスの状態取得、または起動・停止等の操作を行った後の状態を返します (`--read-only` 指定時は `status` のみ使用可能)。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `name` | string | ○ | サービス名 (Windows では表示名ではなくサービス名) |
| `action` | string | | `status` (既定)、`start`、`stop`、`restart`、`reload` (Linux のみ)、`enable`、`disable` |
| `user` | boolean | | Linux でユーザーのユニットを対象にする |
| `sudo` | boolean | | Linux で `sudo -n systemctl ...` を使用する |
| `wait_sec` | integer | | Windows で起動・停止完了を待つ秒数 (省略時は `30`) |

Table: svc_control の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `service` | object | [`svc_list`](#svc_list) と同一形式の状態 |
| `properties` | object | 詳細プロパティ (Linux は `FragmentPath`、`MainPID`、`Result` 等、Windows は `BinaryPath`、`Account`、`Dependencies`、`ExitCode` 等) |
| `status_text`、`output` | string | Linux の `systemctl status --lines=20` 出力と操作コマンド出力 |

Table: svc_control の出力

### log_query

システム ログ (Linux は `journalctl -o json`、Windows は `wevtutil qe /f:RenderedXml`) を構造化配列で返します (古い順、末尾が最新)。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `unit` | string | | Linux: systemd ユニット名。Windows: イベント プロバイダー (ソース) 名 |
| `log` | string | | Windows: チャネル名 (`System` [既定]、`Application`、`Security`、`Setup` 等)。Linux: `kernel` 指定でカーネル ログのみ |
| `since`、`until` | string | | 開始・終了時刻 (`30m`、`2h`、`1d` 等の相対時間、`2026-10-30 09:00`、または RFC 3339) |
| `priority` | string | | 最低重要度 (Linux: `emerg`〜`debug`、Windows: `critical`、`error`、`warning`、`information`、`verbose`) |
| `event_id` | integer | | Windows のイベント ID |
| `grep` | string | | メッセージ本文に対する正規表現フィルター (大文字・小文字区別なし) |
| `lines` | integer | | 最新からの最大取得件数 (省略時は `100`、上限 `2000`) |
| `user`、`sudo` | boolean | | Linux でユーザー ジャーナルの参照、`sudo -n journalctl` の使用 |

Table: log_query の引数

出力の `entries` 各要素は `time` (RFC 3339)、`priority`、`source`、`pid`、`event_id` (Windows)、`message` です。出力サイズ上限を超える場合は古い項目から除外し `truncated: true` にします。

### disk_list

ファイル システム (Linux) またはドライブ (Windows) のディスク使用量を返します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `all` | boolean | | Linux で疑似ファイル システム (`proc`、`sysfs`、`cgroup` 等) も含める |

Table: disk_list の引数

出力の `disks` 各要素は `mount`、`device` (Linux)、`type`、`total_bytes`、`used_bytes`、`free_bytes`、`used_percent`、`total`、`free` です。

## Windows のレジストリ

Windows 接続先でのみ公開されるツール群 (`admin` グループ) です。  
キーはルート (`HKLM`、`HKCU`、`HKCR`、`HKU`、`HKCC`、または `HKEY_LOCAL_MACHINE` 等) から記述し、区切り文字には `\` と `/` の両方を使用できます。`view` に `"64"` または `"32"` を指定すると、64 ビット Windows 上で対象の WOW64 レジストリ ビューにアクセスします。

### reg_read

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `key` | string | ○ | レジストリ キーのパス |
| `value` | string | | 読み取る値の名前 (空文字列は既定値、省略時はサブキー一覧と全値を返す) |
| `view` | string | | `"64"` または `"32"` |

Table: reg_read の引数

出力は `key`、`subkeys`、`values` (`name`、`type`、`data`、`expanded`) です。型ごとの `data` 表現は次のとおりです。

| type | data の形式 |
|---|---|
| `REG_SZ`、`REG_EXPAND_SZ` | 文字列 (`REG_EXPAND_SZ` は環境変数展開後の値を `expanded` にも格納) |
| `REG_MULTI_SZ` | 文字列配列 (`string[]`) |
| `REG_DWORD`、`REG_QWORD` | 数値 |
| その他 (`REG_BINARY` 等) | base64 文字列 |

Table: レジストリの値の形

### reg_write

レジストリ値を作成・更新します (キーが存在しない場合は作成します)。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `key` | string | ○ | レジストリ キーのパス |
| `value` | string | | 値の名前 (空文字列は既定値、省略時はキーの作成のみ) |
| `type` | string | | `REG_SZ` (既定)、`REG_EXPAND_SZ`、`REG_MULTI_SZ`、`REG_DWORD`、`REG_QWORD`、`REG_BINARY` |
| `data` | any | | 書き込む値 |
| `view` | string | | `"64"` または `"32"` |

Table: reg_write の引数

出力は `key`、`created_key`、`value` です。

### reg_delete

レジストリ値またはキーを削除します (ルート キー自体の削除は拒否します)。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `key` | string | ○ | レジストリ キーのパス |
| `value` | string | | 削除する値の名前 (空文字列は既定値、省略時はキー自体を削除) |
| `recursive` | boolean | | サブキーを含めてキーを再帰削除する (未指定時はサブキーがあるとエラー) |
| `view` | string | | `"64"` または `"32"` |

Table: reg_delete の引数

## 手元との転送 (bridge)

`host-shell bridge` はクライアント側で動作する中継サブコマンドです。エージェントに対して MCP サーバーとして振る舞い、子プロセスとして起動した接続先の `host-shell` と中継しつつ、ローカル・接続先間のファイル転送ツールを追加します。

```mermaid
---
title: bridge の構成
---
flowchart TB
    subgraph client ["クライアント"]
        agent["エージェント"]
        bridge["host-shell bridge"]
        local["ローカル ファイル"]
    end
    subgraph target ["接続先"]
        remote["host-shell --mcp"]
        files["接続先ファイル"]
    end
    agent -->|"MCP (stdio)"| bridge
    bridge -->|"MCP (ssh 入れ子の stdio)"| remote
    bridge -->|"transfer_push、transfer_pull"| local
    remote --> files
```

### 書式

```text
host-shell bridge [options] -- COMMAND [ARGS...]
```

`COMMAND` には接続先の `host-shell --mcp` を起動するコマンドを指定します。`COMMAND` の標準エラー出力はそのまま `bridge` の標準エラーへ転送されます。

| オプション | 値 | 内容 |
|---|---|---|
| `--allow-local-path` | ディレクトリ (複数指定可) | ファイル転送でローカル側の読み書きを許可するディレクトリ |
| `--allow-local-read-path` | ディレクトリ (複数指定可) | `transfer_push` での読み取りのみを許可するローカル ディレクトリ |
| `--no-transfer` | なし | 転送ツール (`transfer_push`、`transfer_pull`) を追加せず中継のみを行う |

Table: bridge のオプション

### 中継

- 接続先のツールとリソースを同一の名前・説明・スキーマで公開し、呼び出し・進捗通知 (`progressToken`)・キャンセル通知・ログ通知・確認要求 (`--confirm`) を透過的に中継します。
- 初期化応答の `instructions` には、接続先の案内文にファイル転送ツールの案内を追記して返します。
- 接続先へはクライアント名 `host-shell-bridge` で接続し、このクライアント名からの `fs_read` (`base64`) に限り 1 回あたり最大 1 MiB のチャンク読み取りが許可されます。
- 接続先とのセッションが切断された場合、`bridge` も終了します。

### transfer_push

ローカルのファイルまたはディレクトリを接続先へ転送します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `local_path` | string | ○ | ローカルのパス (相対パスは `bridge` の作業ディレクトリ基準) |
| `remote_path` | string | ○ | 接続先のパス (単一ファイル転送時に既存ディレクトリまたは末尾 `/` を指定した場合はその配下に同名で配置) |
| `overwrite` | boolean | | 内容が異なる既存ファイルを上書きする (同一内容のファイルは常に転送を省略) |
| `exclude` | string[] | | 除外するファイル・ディレクトリ名の glob 配列 (例: `[".git", "node_modules", "*.pyc"]`) |
| `dry_run` | boolean | | 転送を行わず対象ファイルの一覧のみを返す |
| `parallel` | integer | | 同時転送ファイル数 (1〜16、省略時は `4`) |

Table: transfer_push の引数

各ファイルは次の手順で転送されます (失敗時は `.host-shell-part` を削除し、空ディレクトリとシンボリック リンクはスキップします)。

1. 接続先の `fs_manifest` で既存ファイルの SHA-256 を確認し、一致すれば転送を省略 (`unchanged`)、不一致かつ `overwrite: false` ならスキップ (`exists`) します。
2. `<転送先>.host-shell-part` へ 1 MiB 単位で `fs_write` (`base64`、`offset`) を呼び出して書き込みます (初回書き込み時にローカルの実行権限 `perm` を反映)。
3. `fs_manifest` で SHA-256 を照合し、一致したら `fs_move` で本来のファイル名へリネームします。

| 出力 | 型 | 内容 |
|---|---|---|
| `source`、`destination` | string | 転送元と転送先のパス |
| `transferred`、`unchanged`、`failed` | integer | 転送ファイル数 (`dry_run` 時は予定数)、未変更スキップ数、失敗数 (`exists` + `error`) |
| `bytes` | integer | 転送した総バイト数 |
| `files` | object[] | ファイル別の結果 (`path`、`bytes`、`status` [`sent`/`unchanged`/`exists`/`would-send`/`skipped`/`error`]、`error`。500 件超過時は `unchanged` を省略し `truncated: true`) |
| `duration_ms` | integer | 所要時間 (ミリ秒) |

Table: transfer_push と transfer_pull の出力

### transfer_pull

接続先のファイルまたはディレクトリをローカルへ転送します。引数と出力は [`transfer_push`](#transfer_push) と同一です (`local_path` が転送先になります)。接続先の `fs_manifest` で SHA-256 を確認し、未変更ファイルを除いて `fs_read` (`base64`) で 1 MiB 単位に `<ローカル転送先>.host-shell-part` へ読み出し、SHA-256 照合と権限反映の後にリネームします。

## プロセスの起動と終了

### シェル

`command` 引数は `shell` で選択されたシェルへ次の形式で渡されます。

| shell | 対応 OS | 起動形式 |
|---|---|---|
| `sh` | Linux (既定) | `sh -c '<command>'` |
| `bash` | Linux | `bash -c '<command>'` |
| `cmd` | Windows | `"%ComSpec%" /d /s /c "<command>"` |
| `powershell` | Windows (既定) | `powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -OutputFormat Text -Command "$input \| . ([ScriptBlock]::Create(...<base64>...))"` |
| `pwsh` | Windows、Linux | `pwsh -NoProfile -NonInteractive -ExecutionPolicy Bypass -OutputFormat Text -Command "$input \| . ([ScriptBlock]::Create(...<base64>...))"` |

Table: シェルの起動の形

Windows の既定シェルを PowerShell にしているのは、コマンド ラインを UTF-16LE の base64 で渡すことで `cmd` や SSH の引用符解釈の影響を排除し、出力を UTF-8 に統一できるためです。  
`-Command` には base64 スクリプトを復元して実行する短いローダーのみを渡し、標準入力を `$input` として現在のスコープで実行します。Windows PowerShell 5.1 は `-EncodedCommand` 指定時にエラー・警告・進行状況を CLIXML (XML) で標準エラーへ出力するため、`-EncodedCommand` は使用しません。

スクリプトの前には次の処理が付加されます。

| 処理 | 目的 |
|---|---|
| `$ProgressPreference = 'SilentlyContinue'` | 進行状況 (初回のモジュール読み込み、`Invoke-WebRequest` など) を出力しない |
| `if ($PSStyle) { $PSStyle.OutputRendering = 'PlainText' }` | PowerShell 7 の色制御シーケンスを出力しない (5.1 では無処理) |
| `[Console]::OutputEncoding = [System.Text.Encoding]::UTF8` | PowerShell の出力を UTF-8 に統一する |

Table: PowerShell のスクリプトの前に付加する処理

エラーは標準エラーへ、`Write-Warning` の警告は標準出力へ、いずれもプレーン テキストで出力されます (`-Command` の動作)。  
PowerShell 実行時は末尾に終了コード決定処理が付加され、Linux の `sh -c` と同様の規則で終了コードを決定します。

| 最後に実行した文の結果 | 終了コード |
|---|---|
| 成功 (`$?` が真) | `0` |
| 失敗、かつネイティブ コマンドが `$LASTEXITCODE` に非 0 を設定 | `$LASTEXITCODE` |
| 失敗、かつ `$LASTEXITCODE` が 0 または未設定 (コマンドレット失敗) | `1` |
| `exit N` による終了 | `N` |
| 終了エラー (`throw` など) による停止 | `1` |

Table: PowerShell の終了コード

PowerShell から呼び出した Shift_JIS 出力のネイティブ コマンドが文字化けする場合は `"shell": "cmd"` を指定してください (`cmd` には `/d` を付けて AutoRun を無効化し、コマンド ラインをそのまま渡します)。

#### Windows の PowerShell の標準入力

PowerShell は標準入力 (`$input`) を起動時コンソールの入力コード ページで読み取るため、スクリプト内で `[Console]::InputEncoding` を変更しても `$input` には反映されません。  
そのため `host-shell` は非表示コンソールを作成して入出力コード ページを 65001 (UTF-8) に設定し、PowerShell をこのコンソール上で起動します。これにより、接続先のコード ページによらず任意の Unicode 文字を `stdin` で渡せます。

| 条件 | `stdin_encoding` の既定 | PowerShell が使用するコンソール |
|---|---|---|
| `stdin_encoding` 省略、または `utf-8` | `utf-8` | `host-shell` のコンソール (65001) |
| `stdin_encoding` に `utf-8` 以外を指定 | 指定値 | PowerShell 自身のコンソール (コード ページ不変) |
| `host-shell` の標準入出力がコンソール (端末から直接起動した場合) | `shell` 指定時は `system`、`argv` 指定時は `utf-8` | PowerShell 自身のコンソール |

Table: Windows の PowerShell の標準入力

- 対象は `shell` が `powershell` または `pwsh` の呼び出しと、`argv` の先頭が `powershell` または `pwsh` (`.exe` やディレクトリの有無を問わない) の呼び出しです。`argv` の引数は変更しません。`pty` ジョブは ConPTY を使用するため対象外です。
- 非表示コンソールは初回 PowerShell 起動時に 1 回だけ作成します (約 100 ms)。端末から直接起動した場合は、利用者のコンソールのコード ページを変更しないよう作成しません。
- `cmd` とその他のネイティブ コマンドは、各自のコンソール (OEM コード ページ) で動作します。
- 日本語などの環境では、`host-shell` のコンソール (コード ページ 65001) 上で動作する PowerShell やそこから起動したネイティブ コマンドのメッセージが英語になる場合があります (コンソールのコード ページで UI 言語を表現できない場合に Windows が英語メッセージへ切り替えるため)。

### argv による起動

`argv` 指定時はシェルを介さずに実行ファイルを直接起動します (`PATH` 検索に対応し、Windows では MSVC 標準規則で引数をエスケープします)。

### 環境変数と標準入力

子プロセスは `host-shell` の環境変数を引き継ぎ、`--unset-env` の除外処理を適用したうえで `env` 引数の変数を追加・上書きします。  
`stdin` 省略時の標準入力は空 (`/dev/null` または `NUL`) であり、`host-shell` 自身の標準入出力 (MCP 通信路) が子プロセスへ継承されることはありません。

### 終了のさせ方

タイムアウト、キャンセル、`job_stop` (`kill`)、および `host-shell` 終了時には、子孫プロセスを含めて強制終了します。

| OS | 強制終了の方法 | 捕捉できないプロセス |
|---|---|---|
| Linux | 新規プロセス グループ (`pty` は新規セッション) で起動し、グループ全体へ `SIGKILL` を送信 | `setsid` 等で別プロセス グループ・セッションへ離脱したプロセス |
| Windows | `taskkill /T /F` でプロセス ツリーを強制終了 | 親プロセスが先に終了してツリーから孤立した子孫プロセス (`host-shell` 終了時は Job Object により終了) |

Table: 終了のさせ方

`host-shell` 自体が強制終了された場合 (`kill -9`、SSH セッション断など) も、次の仕組みで子プロセスの残留を防ぎます。

| OS | 仕組み | 残留しうるケース |
|---|---|---|
| Linux | 直接の子プロセスに `PR_SET_PDEATHSIG(SIGKILL)` を設定 | 直接の子が終了しても停止しない孫プロセス |
| Windows | 起動時に `host-shell` 自身を `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` 付き Job Object に入れ、全子孫プロセスを自動所属させる | なし (Job Object からの離脱は不可) |

Table: host-shell が終了したときのコマンドの扱い

セッション終了後も継続させたいデーモンやサービスは、`host-shell` の子プロセスとして直接起動せず、`systemd-run` や Windows サービス / タスク スケジューラー経由で起動してください。  
コマンド終了後も子孫プロセスが出力パイプを開き続けている場合は、最大 2 秒待機してからパイプを閉じて結果を返します。

## 操作ログ

`host-shell` は全ツール呼び出しを接続先のログ ファイルへ 1 行 1 件の JSON Lines 形式で記録します。

### 保存先

| OS | 既定の基準パス |
|---|---|
| Linux | `$XDG_STATE_HOME/host-shell/audit.log` (`XDG_STATE_HOME` 未設定時は `~/.local/state/host-shell/audit.log`) |
| Windows | `%LOCALAPPDATA%\host-shell\audit.log` |

Table: 操作ログの既定の基準パス

ログは接続先のローカル日付ごとに `<基準名>-YYYY-MM-DD<拡張子>` (例: `audit-2026-10-31.log`) へ出力されます。

- 当日分に加えて新しい順に 5 日分を保持し、それより古い `<基準名>-YYYY-MM-DD<拡張子>` ファイルを起動時および日付切り替え時に自動削除します (日付なし `audit.log` は削除対象外です)。
- ディレクトリは権限 `0700`、ログ ファイルは権限 `0600` で作成し、1 レコードごとにオープン・追記・クローズするため複数セッションで同時書き込みしても行が混在しません。
- ログ ファイルを開けない場合、`host-shell` は起動に失敗します (記録不要な場合は `--no-log` を指定します)。

### 形式

```json
{"time":"2026-10-30T08:15:42.42+09:00","level":"INFO","msg":"tool_call","tool":"fs_read","args":{"path":"/nonexistent"},"duration_ms":0,"is_error":true,"error":"open /nonexistent: no such file or directory"}
```

| msg | 記録タイミング | フィールド |
|---|---|---|
| `server_start` | 起動時 | `version`、`pid` |
| `tool_call` | ツール呼び出し完了時 (ポリシー拒否時および `<ツール名> (confirmation requested)` を含む) | `tool`、`args`、`duration_ms`、`is_error`、`error` (エラー時) |
| `server_stop` | 終了時 | `error` (正常終了時は `<nil>`) |

Table: 操作ログの記録

### 記録しない値

`args` にはツール引数が記録されますが、機密保護とログ肥大化防止のため次のフィールドはマスクされます。

| ツール | 引数 | 記録される値 |
|---|---|---|
| `exec_run`、`job_start`、`exec_sudo` | `stdin` | `<N chars>` |
| `exec_sudo` | `password` | `<redacted>` |
| `job_write` | `input` | `<N chars>` |
| `fs_edit` | `old_string`、`new_string` (`edits` 内も含む) | `<N chars>` |
| `fs_write` | `content` | `<N chars, sha256 ...>` |
| `net_request_http` | `body`、および `headers` の `Authorization`、`Proxy-Authorization`、`Cookie`、`X-Api-Key` | `<N chars>` |
| `net_exchange` | `send` | `<N chars>` |

Table: 操作ログで置き換える値

`command` と `argv` はそのまま記録されるため、パスワード等の機密値はコマンド ラインに含めず `stdin` 等で渡してください。

## 制限事項

- 疑似端末は `job_start` の `pty: true` でのみ利用可能です (`exec_run` は非対話パイプ実行)。全画面プログラム (`vi`、`top` 等) の表示は `job_read` の `screen: true` で取得してください。
- 文字コードの自動判定は BOM、UTF-16 のバイト分布、および UTF-8 妥当性のみを検査し、Shift_JIS と EUC-JP 等の非 UTF-8 文字コード同士は区別しません。接続先の既定文字コードと異なるファイルを扱う場合は `charset` を明示してください。
- まれに非 UTF-8 の短いバイト列が UTF-8 として妥当なバイト並びになり文字化けする場合があります。その場合は `output_encoding` や `charset` を明示してください。
- `net_list_listeners`、`svc_list`、`svc_control`、`log_query`、`disk_list` は Linux と Windows のみ対応です (Linux のサービス・ログ操作は systemd 環境を前提とします)。
- 起動オプションによる制限 (ポリシー) はツール引数の検査であり、OS の権限境界ではありません ([制限の限界](#制限の限界) を参照)。
- 対応 OS は Go 1.26 の要件に基づき Windows 10 / Windows Server 2016 以降 (`pty` は Windows 10 1809 / Windows Server 2019 以降)、Linux カーネル 3.2 以降、macOS 12 以降 (`bridge` のみ) です。
