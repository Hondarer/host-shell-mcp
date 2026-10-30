# host-shell リファレンス

この文書は、`host-shell` の起動オプション、MCP サーバーとしての振る舞い、各ツールの入出力、プロセスと操作ログの扱いを定義します。  
導入と使い方は [利用ガイド](user-guide.md)、安全な運用の方法は [運用ガイド](operations.md) を参照してください。

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
host-shell --mcp [options]                      (接続先で)
host-shell bridge [bridge-options] -- COMMAND...  (手元で)
host-shell --version
```

`--mcp` を指定したときだけ、MCP サーバーとして標準入出力で待ち受けます。  
`bridge` は、手元のマシンで動かして接続先の host-shell を中継するサブコマンドです。[手元との転送 (bridge)](#手元との転送-bridge) を参照してください。  
`--mcp` も `--version` も指定しない場合は、使い方を標準エラーに出力し、終了コード 2 で終了します。

### オプション

| オプション | 値 | 既定値 | 内容 |
|---|---|---|---|
| `--mcp` | なし | | MCP サーバーとして標準入出力で待ち受ける |
| `--version` | なし | | バージョンを標準出力に出力して終了する |
| `--log-file` | パス | 下記の [操作ログ](#操作ログ) を参照 | 操作ログの元の名前。日付を入れたファイルに書く |
| `--no-log` | なし | | 操作ログを記録しない。`--log-file` より優先する |
| `--max-output` | バイト数 (1024 以上) | `65536` | 1 つの出力の上限 |
| `--timeout` | 時間 (例: `90s`、`5m`) | `1m` | コマンドのタイムアウトの既定値 |
| `--max-timeout` | 時間 | `30m` | ツールの引数で指定できるタイムアウトの上限。`--timeout` 以上にする |
| `--windows-shell` | `cmd`、`powershell`、`pwsh` | `powershell` | Windows で `shell` を省略したときに使うシェル |
| `--tools` | 組とツールの名前 (カンマ区切り) | `all` | 公開するツール。[制限 (方針)](#制限-方針) を参照 |
| `--read-only` | なし | | 接続先を変更するツールを公開しない |
| `--confirm` | 組とツールの名前、`destructive` | なし | 呼び出しの前に利用者の確認を求めるツール |
| `--allow-path` | ディレクトリ (繰り返し可) | なし | ファイルのツールが読み書きできる場所 |
| `--allow-read-path` | ディレクトリ (繰り返し可) | なし | ファイルのツールが読み取りだけできる場所 |
| `--allow-command` | プログラム名の glob (カンマ区切り、繰り返し可) | なし | 実行できるプログラム |
| `--deny-command` | 正規表現 (繰り返し可) | なし | 実行を拒むコマンド行 |
| `--allow-host` | 宛先 (カンマ区切り、繰り返し可) | なし | ネットワークのツールが接続できる宛先 |
| `--unset-env` | 環境変数の名前の glob (カンマ区切り、繰り返し可) | なし | コマンドに渡さない環境変数 |
| `--redact` | なし | | 結果に含まれる秘密情報を伏せ字にする |
| `--redact-pattern` | 正規表現 (繰り返し可) | なし | 伏せ字にする追加の形 |

Table: 起動オプション

オプションは `-mcp` のようにハイフン 1 つでも指定できます。  
未知のオプションや、型の合わない値 (数値の位置に文字列など) を指定した場合は、使い方を標準エラーに出力し、終了コード 2 で終了します。  
値の範囲が不正な場合 (`--max-output` が 1024 未満、`--tools` に未知の名前、正規表現の誤りなど) は、理由を標準エラーに出力し、終了コード 1 で終了します。

### 入出力と終了

| 項目 | 振る舞い |
|---|---|
| 標準入力 | MCP の JSON-RPC メッセージを 1 行に 1 つずつ受け取る |
| 標準出力 | MCP の JSON-RPC メッセージだけを出力する。それ以外は一切出力しない |
| 標準エラー | 起動の失敗などの診断メッセージを出力する |
| 作業ディレクトリ | 起動したときのディレクトリ。SSH 経由の場合は通常、接続したユーザーのホーム ディレクトリ |
| 正常な終了 | 標準入力が閉じられた (SSH が切断された) とき、または SIGINT、SIGTERM を受けたとき。終了コード 0 |
| 終了時の後始末 | 実行中のジョブをすべて停止する |

Table: 入出力と終了

SSH の 1 つのセッションが 1 つの `host-shell` プロセスに対応します。  
複数のエージェントから同時に接続した場合は、それぞれ独立したプロセスとして動き、ジョブなどの状態は共有しません。

## 制限 (方針)

制限のオプションを何も指定しない場合、`host-shell` は制限なしで動きます。すべてのツールを公開し、接続したユーザーにできることは何でもできます。  
オプションで指定したものだけを制限します。指定した制限は、[`sys_info`](#sys_info) の `restrictions`、`help` の先頭の表、初期化の応答の `instructions` でエージェントに伝えます。  
制限に反する呼び出しは、`policy:` で始まるエラーの説明を返し、実行しません。

### ツールの選択 (--tools)

ツールを次の組に分けています。`help` と `sys_info` (組 `core`) は常に公開します。

| 組 | ツール |
|---|---|
| `files` | `fs_list`、`fs_stat`、`fs_read`、`fs_edit`、`fs_write`、`fs_mkdir`、`fs_move`、`fs_copy`、`fs_remove`、`fs_manifest` |
| `search` | `search_grep`、`search_files` |
| `exec` | `exec_run`、`sudo_run`、`job_start`、`job_read`、`job_write`、`job_stop`、`job_list` |
| `net` | `net_http`、`net_tcp`、`net_check`、`net_listen`、`net_interfaces` |
| `admin` | `sys_processes`、`proc_kill`、`svc_list`、`svc_control`、`log_query`、`sys_disks`、`reg_read`、`reg_write`、`reg_delete` |

Table: ツールの組

`--tools` は、組の名前、ツールの名前、`all` をカンマで区切って並べ、左から順に適用します。名前の前の `-` は除く、`+` (または何も付けない) は加えるという意味です。

| 指定 | 公開するツール |
|---|---|
| `all` (既定) | すべて |
| `files,search` | ファイルと検索のツールだけ |
| `all,-admin,-sudo_run` | 管理のツールと `sudo_run` を除くすべて |
| `files,exec,-fs_remove` | ファイルとコマンド実行のツールから `fs_remove` を除いたもの |

Table: --tools の例

`destructive` (接続先を変更しうるツール) と `readonly` (変更しないツール) も、組と同じように使えます。  
OS に固有のツール (`sudo_run`、`reg_*`) の名前は、その OS でなくても指定できます (公開はされません)。

### 読み取り専用 (--read-only)

`readOnlyHint` の注釈がないツールを公開しません。ただし、`net_http` と `net_tcp` は次の制限付きで公開します。

- `net_http`: `GET`、`HEAD`、`OPTIONS` だけ。`save_to` は使えない
- `net_tcp`: `send` は使えない (相手が先に送る内容を読むだけ)

### 利用者の確認 (--confirm)

指定したツールを呼ぶたびに、MCP の elicitation で利用者に許可を求め、許可された場合だけ実行します。指定の書き方は `--tools` と同じで、`destructive` で変更を伴うすべてのツールを指定できます。

- プロトコル バージョン `2026-07-28` のクライアントには、multi round-trip requests (SEP-2322) に従い、確認の問い合わせ (`inputRequests`) を結果として返します。クライアントが利用者の回答を付けて同じ呼び出しをやり直すと実行します。回答は、同じツールと同じ引数のやり直しにだけ有効です (サーバーの起動ごとの鍵で署名した `requestState` で結び付けます)。
- それより前のバージョンのクライアントには、SDK が `elicitation/create` を送って回答を受け取り、やり直しを代わりに行います。
- クライアントが elicitation に対応していない場合は、実行せずにエラーを返します。
- 確認を求めたことは、操作ログに `<ツール名> (confirmation requested)` として記録します。

### ファイルの場所 (--allow-path、--allow-read-path)

ファイルを扱うツールの、パスの引数を検査します。

| 対象 | 読み取りとして検査 | 書き込みとして検査 |
|---|---|---|
| ファイル操作 | `fs_list`、`fs_stat`、`fs_read`、`fs_manifest` の `path`、`fs_copy` の `source` | `fs_edit`、`fs_write`、`fs_mkdir`、`fs_remove` の `path`、`fs_move` の `source` と `destination`、`fs_copy` の `destination` |
| 検索 | `search_grep`、`search_files` の `path` | |
| コマンド実行 | `exec_run`、`job_start`、`sudo_run` の `cwd` | |
| ネットワーク | `net_http` の `body_file`、`ca_cert`、`client_cert`、`client_key` | `net_http` の `save_to` |

Table: 場所の検査の対象

- 読み取りは、`--allow-path` と `--allow-read-path` のどちらかの下で許します。書き込みは、`--allow-path` の下だけで許します。
- `--allow-read-path` だけを指定した場合、書き込みはどこにも許しません。
- 判定の前に、パスを絶対パスにし、存在する部分のシンボリック リンクを解決します。許可した場所の中から外を指すリンクを通る操作は拒みます。
- Windows では、大文字と小文字を区別せずに比べます。

### コマンド (--allow-command、--deny-command)

`exec_run`、`job_start`、`sudo_run` で起動するプログラムを検査します。

- `--allow-command` を指定した場合は、起動するプログラムの名前 (パスの最後の要素。Windows では小文字にし、`.exe` などの拡張子を除く) が、いずれかの glob に一致する必要があります。
- `command` (シェルのコマンド行) の場合は、シェル (`sh`、`bash`、`cmd`、`powershell`、`pwsh`) そのものが許可されている必要があります。シェルを許可すると、シェルの中では何でも実行できます。プログラムを絞る場合は、エージェントに `argv` を使わせます。
- `sudo_run` は、`sudo` と `sh` の両方が許可されている必要があります。
- `--deny-command` は、コマンド行 (`argv` の場合は空白でつないだもの) が正規表現に一致する呼び出しを拒みます。書き方を変えれば避けられるため、誤操作の防止のためのものです。

### ネットワークの宛先 (--allow-host)

`net_http` (リダイレクトの先も含む)、`net_tcp`、`net_check` の宛先を検査します。

| 書き方 | 意味 |
|---|---|
| `localhost`、`*.internal` | ホスト名の glob (すべてのポート) |
| `api.internal:443`、`*:80` | ホスト名とポート (ポートも glob) |
| `10.0.0.0/8`、`10.0.0.0/8:5432` | アドレスの範囲。名前の場合は、解決したすべてのアドレスが範囲に入る必要がある |
| `unix:/var/run/*.sock` | Unix ドメイン ソケットのパスの glob |

Table: --allow-host の書き方

### 環境変数 (--unset-env)

`exec_run`、`job_start`、`sudo_run`、管理のツールが起動するプロセスに、名前が glob に一致する環境変数を渡しません (例: `--unset-env 'AWS_*,*TOKEN*'`)。  
ツールの `env` で指定したものは渡します。

### 伏せ字 (--redact、--redact-pattern)

ツールの結果に含まれるすべての文字列 (`structuredContent` と `content`) について、秘密情報の形を `[REDACTED]` に置き換えます。

| 形 | 例 |
|---|---|
| 秘密鍵の PEM | `-----BEGIN ... PRIVATE KEY-----` から `-----END ... PRIVATE KEY-----` まで (本文だけを伏せる) |
| 認証のヘッダー | `Authorization: Bearer ...` |
| 名前と値の組 | `password=...`、`api_key: ...`、`DB_PASSWORD=...`、`"token": "..."` (値だけを伏せる) |
| URL の利用者情報 | `https://user:password@host` のパスワード |
| 既知のトークン | AWS のアクセス キー ID、GitHub、Slack のトークン、`sk-...`、JWT |

Table: 伏せ字にする形

`--redact-pattern` で、一致した部分を伏せる正規表現を加えられます (`--redact` なしでも使えます)。  
伏せ字にしたファイルの内容をそのまま `fs_write` で書き戻すと、`[REDACTED]` が書き込まれます。エージェントには、`help` の `policy` で注意を伝えています。

### 制限の限界

これらの制限は、ツールの引数に対する検査です。OS の権限の境界ではありません。

- コマンドを実行できる場合 (`exec` の組を公開している場合)、コマンドはファイルやネットワークに自由に触れられます。場所や宛先を厳密に制限するには、`--tools` で `exec` を外すか、`--allow-command` で絞ります。
- 確実な境界が必要な場合は、`host-shell` を権限の少ない専用のユーザーで動かします。[運用ガイド](operations.md) を参照してください。

## MCP サーバーとしての振る舞い

| 項目 | 内容 |
|---|---|
| 通信方式 | stdio (改行区切りの JSON-RPC 2.0) |
| プロトコル バージョン | `2024-11-05`、`2025-03-26`、`2025-06-18`、`2025-11-25`、`2026-07-28`。クライアントの要求に合わせて選ぶ |
| サーバー名 | `host-shell` |
| 機能 (capabilities) | `tools`、`resources`、`logging` |
| 進み具合の通知 | 要求に `progressToken` がある場合、`exec_run`、`sudo_run` は 1 秒ごとに、`net_http` の `save_to` は受信に合わせて `notifications/progress` を送る |
| 要求の中止 | `notifications/cancelled` を受けると、その要求の処理を止める。コマンドはプロセス ツリーごと強制終了する |
| 利用者への確認 | `--confirm` の対象のツールで elicitation を使う ([利用者の確認](#利用者の確認---confirm)) |
| 受信メッセージの上限 | 1 メッセージ 16 MiB |
| 実装 | [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) v1.8.0 |

Table: MCP サーバーの仕様

### エージェントへの案内

エージェントが、ほかの文書を読まずに MCP の情報だけで使い方を把握できるよう、次の 4 つの層で案内します。

| 層 | 内容 | エージェントが受け取る時点 |
|---|---|---|
| 初期化の応答の `instructions` | 接続先のホスト名、OS とアーキテクチャ、作業ディレクトリ、既定のシェルと文字コード、出力の上限とタイムアウト。公開しているツールの選び方と、守るべき約束 (呼び出しごとに新しいプロセスであることなど)。制限がある場合はその内容 | 接続したとき |
| ツールの `title` と `description` | 何をするか、いつ使うか、何と組み合わせるか | ツールの一覧を取得したとき |
| 引数と出力のスキーマの `description` | 各引数の意味、既定値、続きの読み方 | ツールの一覧を取得したとき |
| [`help`](#help) ツールとリソース | 作業の手順、ツールの選び方、引数の例、Windows での注意、上限 | 必要なときに呼ぶ |

Table: エージェントへの案内

案内はエージェントが読むものなので、英語で書いています。  
`help` の内容は、MCP のリソース (`host-shell://help/<topic>`、`text/markdown`) としても公開します。リソースに対応したクライアントでは、利用者が会話に添付することもできます。

エラーの説明も、エージェントが次に何をすればよいかが分かるように書いています。  
たとえば `fs_edit` で置換する文字列が複数の箇所に一致した場合は、その行番号と、文脈を加えるか `replace_all` を指定するよう返します。

## 全ツール共通の仕様

### 結果の形式

ツールの結果は、`structuredContent` に JSON のオブジェクトとして返します。  
同じ JSON を文字列にしたものを、`content` のテキストとしても返します。  
各ツールの出力の形は、`tools/list` の `outputSchema` で公開しています。  
例外として、`help` は Markdown のテキストだけを `content` に返し、`outputSchema` を持ちません。

### エラー

次の場合は、結果の `isError` を `true` にし、`content` のテキストに理由を返します。

- 引数がスキーマに合わない (必須の引数がない、型が違う、定義にない引数がある)
- ファイルが存在しない、権限がないなど、操作そのものが失敗した
- コマンドを起動できなかった (プログラムが見つからない、シェルがこの OS で使えないなど)

コマンドが起動したあとで 0 以外の終了コードで終わった場合は、エラーではありません。  
結果の `exit_code` で判断してください。

### パスの解釈

パスは接続先のマシン上で解釈します。  
相対パスは、`host-shell` の作業ディレクトリ (通常はホーム ディレクトリ) からの相対です。  
Windows では、`C:/work/a.txt` と `C:\work\a.txt` のどちらの書き方も使えます。  
結果に含めるパスは、特に記載がない限り絶対パスに変換したものです。

### 出力の上限

すべてのツールは、出力の大きさに上限を設けています。  
上限の既定値は 64 KiB で、`--max-output` で変更できます。

| ツール | 上限の単位 | 超えた場合 |
|---|---|---|
| `exec_run`、`sudo_run` | 標準出力と標準エラーのそれぞれ | 先頭と末尾を半分ずつ残し、間に省略の印を入れる。`stdout_truncated` などを `true` にする |
| `job_read` | 1 回の読み取り | `next_offset` から続きを読む |
| `fs_read` | 1 回の読み取り | `truncated` を `true` にし、続きの位置を返す |
| `sys_processes` | 出力全体 | 先頭と末尾を残して省略の印を入れる |
| `net_http` | 本文 | 先頭だけを返し、`body_truncated` を `true` にする。`save_to` を使えば大きさに上限はない |
| `net_tcp` | 受信 | 上限で受信をやめ、`truncated` を `true` にする |
| `log_query` | 出力全体 | 古い項目から除き、`truncated` を `true` にする |
| `fs_list`、`search_*`、`fs_manifest` | 件数 (引数で指定) | `truncated` を `true` にする |

Table: 出力の上限

省略の印は次の形式です。

```text

... [host-shell: 123456 bytes omitted] ...

```

### 文字コード

ツールの入出力のテキストは、常に UTF-8 です。  
接続先のコマンドの出力やファイルが UTF-8 以外の場合は、`host-shell` が UTF-8 との間で変換します。詳しくは [文字コードと改行](#文字コードと改行) を参照してください。

### ツールの注釈

各ツールに、MCP のツール注釈を付けています。  
エージェント側で、承認を必須にするツールを選ぶ目安になります。

| 注釈 | 対象のツール |
|---|---|
| `readOnlyHint: true` | `help`、`job_read`、`job_list`、`fs_list`、`fs_stat`、`fs_read`、`search_grep`、`search_files`、`fs_manifest`、`sys_info`、`sys_processes`、`net_check`、`net_listen`、`net_interfaces`、`svc_list`、`log_query`、`sys_disks`、`reg_read` |
| `destructiveHint: false`、`idempotentHint: true` | `fs_mkdir` |
| `destructiveHint: true` | `exec_run`、`job_start`、`job_write`、`job_stop`、`fs_edit`、`fs_write`、`fs_move`、`fs_copy`、`fs_remove`、`sudo_run`、`net_http`、`net_tcp`、`proc_kill`、`svc_control`、`reg_write`、`reg_delete` |

Table: ツールの注釈

`openWorldHint` は、ネットワークの先に作用しうる `net_http`、`net_tcp`、`net_check` だけ `true` で、ほかは `false` です。

## 文字コードと改行

MCP の通信は常に UTF-8 です。  
`host-shell` は、接続先の文字コード (Shift_JIS、EUC-JP、UTF-16 など) と UTF-8 の違い、改行の違いを、接続先の側で吸収します。

### 接続先の既定の文字コード

自動判定と `system` の指定では、接続先の既定の文字コードを使います。用途によって 2 種類あります。

| 種類 | 使う場面 | Windows | Linux |
|---|---|---|---|
| コンソール | コマンドの出力と標準入力 | OEM コード ページ (`GetOEMCP`) | ロケールの文字コード |
| ファイル | テキスト ファイルの読み書きと検索 | ANSI コード ページ (`GetACP`) | ロケールの文字コード |

Table: 接続先の既定の文字コード

日本語の Windows では、どちらも Shift_JIS (コード ページ 932) です。  
Windows で「ベータ: ワールドワイド言語サポートで Unicode UTF-8 を使用」を有効にしている場合は UTF-8 です。  
Linux では、環境変数 `LC_ALL`、`LC_CTYPE`、`LANG` の順に最初に設定されているものから、`.` の後ろの文字コードを使います (例: `ja_JP.eucJP` なら EUC-JP)。文字コードの部分がない場合 (`C`、`POSIX` など) と、どれも設定されていない場合は UTF-8 です。  
実際の値は、[`sys_info`](#sys_info) の `console_encoding` と `file_encoding` で確かめられます。

### 文字コードの指定

文字コードを指定する引数 (`output_encoding`、`stdin_encoding`、`charset`) には、次の値を指定できます。

| 値 | 意味 |
|---|---|
| `auto` | 自動判定 (下記)。読み取りの引数だけで使え、省略時の既定 |
| `utf-8` | UTF-8。変換しない |
| `system` | 接続先の既定の文字コード (上記) |
| 文字コードの名前 | `shift_jis`、`euc-jp`、`iso-2022-jp`、`utf-16le`、`utf-16be`、`gbk`、`gb18030`、`big5`、`euc-kr`、`windows-1252` など |
| コード ページの番号 | `cp932`、`932`、`cp437`、`cp65001` など |

Table: 文字コードの指定

名前は大文字と小文字を区別しません。[WHATWG Encoding Standard](https://encoding.spec.whatwg.org/#names-and-labels) の名前と別名 (`sjis`、`windows-31j`、`latin1` など) と、`eucjp`、`ujis` などのよく使われる別名を受け付けます。  
`shift_jis` は Windows のコード ページ 932 (CP932、Windows-31J) と同じ文字の集合です。

### 自動判定

読み取りの自動判定は、次の順に行います。

1. ファイルの先頭に BOM (UTF-8、UTF-16LE、UTF-16BE) があれば、その文字コードとします。BOM は内容に含めません。
2. BOM がなくても、先頭が UTF-16 の形であれば、UTF-16LE または UTF-16BE とします (下記)。
3. UTF-8 として正しければ、UTF-8 とします。
4. 正しくなければ、接続先の既定の文字コードとします。

コマンドの出力 (`exec_run`、`job_start`、`sudo_run`) と、`search_grep`、`fs_read` の行の範囲では、3 と 4 の判定を **行ごと** に行います。  
UTF-8 の出力と Shift_JIS の出力が混ざっていても、それぞれの行を正しく変換できます。  
`fs_read` のバイトの範囲では、読み取った範囲全体で判定します。

BOM のない UTF-16 は、ファイルやコマンドの出力の先頭 (最大 4096 バイト) を 2 バイトずつに分け、次のすべてを満たす場合に UTF-16 とみなします。コマンドの出力では、出力の最初の 64 バイトがそろった時点 (それより短い出力では終了時) に判定します。

- 両方のバイトが 0 の単位 (U+0000) が 1% 以下
- 片方のバイトが 0 の単位が 5% 以上 (0 の側で、LE か BE かを決める)
- 反対側のバイトが 0 の単位が 2% 以下 (短い文字列のため、1 単位までは超えてよい)
- サロゲート ペアが正しく組になっている

ASCII の文字は UTF-16 では必ず片方のバイトが 0 になるため、英語の文章や改行を含む日本語の文章は、この条件を満たします。実行ファイルやデータベースは両方が 0 の単位を多く含み、圧縮ファイルや画像は 0 のバイトが少ないため、UTF-16 とはみなしません。  
改行を含まない、漢字やかなだけの短い UTF-16 は判定できない場合があります。その場合は `charset` を指定してください。

変換に使った文字コードは、結果の `stdout_encoding`、`charset` などで返します。  
UTF-8 として正しくないバイト列のうち、どの文字コードとしても解釈できない部分は、置換文字 (U+FFFD) になります。

### 文字の分断の防止

範囲を区切って読むツールは、範囲の末尾で文字を分断しません。  
末尾の不完全な文字は返さずに、次の読み取りの位置 (`next_offset`) に含めます。  
`exec_run` の出力を切り詰めた場合も、境界で分断された文字は取り除いてから省略の印を入れます。

### 改行

改行は、読み取りでは変換しません。CRLF も LF もそのまま返します。  
`fs_read` は、返した内容に含まれる改行の種類を `newline` (`lf`、`crlf`、`cr`、`mixed`) で返します。  
`fs_write` の `newline` に `lf` または `crlf` を指定すると、書き込む前に改行をそろえます。

既存のファイルを書き換えるときは、`fs_read` が返した `charset`、`bom`、`newline` を `fs_write` に渡すと、元の文字コードと改行を保てます。

### 変換の対象

| ツール | 引数 | 既定 |
|---|---|---|
| `exec_run`、`job_start`、`sudo_run` | `output_encoding` (標準出力と標準エラー) | `auto` (コンソール) |
| | `stdin_encoding` (標準入力) | `utf-8` (Windows の PowerShell は `system`。下記) |
| `job_write` | `job_start` の `stdin_encoding` | `job_start` と同じ |
| `sys_processes` | なし | `auto` (コンソール) |
| `fs_read` (`text`) | `charset` | `auto` (ファイル) |
| `fs_edit` | `charset` | `auto` (ファイル)。読んだときの文字コード、BOM、改行で書き戻す |
| `fs_write` (`text`) | `charset`、`newline`、`bom` | `utf-8`、変換しない、BOM なし |
| `search_grep` | `charset` | `auto` (ファイル) |

Table: 文字コードの変換の対象

`base64` で読み書きする場合は、文字コードも改行も変換しません。

## ツール一覧

| 分類 | ツール | 内容 |
|---|---|---|
| 使い方の案内 | [`help`](#help) | 使い方の説明を返す |
| コマンド実行 | [`exec_run`](#exec_run) | コマンドを実行し、終了を待って結果を返す |
| 長時間の実行 | [`job_start`](#job_start) | コマンドをバックグラウンドで開始する |
| | [`job_read`](#job_read) | ジョブの出力と状態を取得する |
| | [`job_write`](#job_write) | ジョブの標準入力に送る |
| | [`job_stop`](#job_stop) | ジョブを停止する |
| | [`job_list`](#job_list) | ジョブの一覧を取得する |
| ファイル操作 | [`fs_list`](#fs_list) | ディレクトリの一覧を取得する |
| | [`fs_stat`](#fs_stat) | ファイルの属性を取得する |
| | [`fs_read`](#fs_read) | ファイルを読む |
| | [`fs_edit`](#fs_edit) | ファイルの一部を置換する |
| | [`fs_write`](#fs_write) | ファイルを書く |
| | [`fs_mkdir`](#fs_mkdir) | ディレクトリを作る |
| | [`fs_move`](#fs_move) | 移動する、名前を変える |
| | [`fs_copy`](#fs_copy) | コピーする |
| | [`fs_remove`](#fs_remove) | 削除する |
| 検索 | [`search_grep`](#search_grep) | ファイルの内容を正規表現で検索する |
| | [`search_files`](#search_files) | ファイル名やパスの glob で検索する |
| 差分の確認 | [`fs_manifest`](#fs_manifest) | ファイルのハッシュの一覧を取得する |
| 特権実行 | [`sudo_run`](#sudo_run) | sudo でコマンドを実行する (Linux のみ) |
| 環境情報 | [`sys_info`](#sys_info) | 接続先の環境と、公開しているツールの組、制限を取得する |
| ネットワーク | [`net_http`](#net_http) | 接続先から HTTP の要求を送る |
| | [`net_tcp`](#net_tcp) | 接続先から TCP で送受信する |
| | [`net_check`](#net_check) | 名前の解決、ポートへの接続、証明書を調べる |
| | [`net_listen`](#net_listen) | 待ち受け中のポートを一覧する |
| | [`net_interfaces`](#net_interfaces) | ネットワーク インターフェースを一覧する |
| 管理 | [`sys_processes`](#sys_processes) | プロセスの一覧を取得する |
| | [`proc_kill`](#proc_kill) | プロセスにシグナルを送る、終了させる |
| | [`svc_list`](#svc_list) | サービスを一覧する |
| | [`svc_control`](#svc_control) | サービスの状態を見る、起動、停止する |
| | [`log_query`](#log_query) | システムのログを読む |
| | [`sys_disks`](#sys_disks) | ディスクの使用量を取得する |
| Windows のレジストリ | [`reg_read`](#reg_read) | レジストリを読む (Windows のみ) |
| | [`reg_write`](#reg_write) | レジストリに書く (Windows のみ) |
| | [`reg_delete`](#reg_delete) | レジストリから削除する (Windows のみ) |
| 手元との転送 | [`transfer_push`](#transfer_push) | 手元から接続先へ送る (bridge 経由の場合だけ) |
| | [`transfer_pull`](#transfer_pull) | 接続先から手元へ送る (bridge 経由の場合だけ) |

Table: ツール一覧

ツールは、[`--tools`](#ツールの選択---tools) と [`--read-only`](#読み取り専用---read-only) で公開されたものだけが一覧に出ます。

以降の表で、「必須」の列が空の引数は省略できます。

## 使い方の案内

### help

エージェント向けの使い方の説明を、Markdown のテキストで返します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `topic` | string | | 説明の主題。省略時は `overview`。`all` ですべてをつなげて返す |

Table: help の引数

| topic | 内容 |
|---|---|
| `overview` | 仕組み、ツールの早見表、推奨する作業の手順。先頭に接続先の情報 (下記) を付ける |
| `files` | `fs_read` の読み方、`fs_write` のモード、ファイル操作のツール、`fs_manifest` |
| `edit` | `fs_edit` の使い方と規則 |
| `search` | glob の構文、`search_grep` の出力モード、`search_files` |
| `commands` | `exec_run`、`sudo_run`、ツールの選び方 |
| `jobs` | ジョブの使い方、待ち方 (`until`、`idle_ms`)、入力、疑似端末、シグナル |
| `network` | `net_*` の使い方と例 |
| `admin` | プロセス、サービス、ログ、ディスク、レジストリ |
| `transfer` | `transfer_push`、`transfer_pull` と、bridge がない場合の方法 |
| `encoding` | 文字コードと改行 |
| `windows` | Windows の接続先での注意 |
| `policy` | 制限のオプションと、エージェントへの影響。先頭に接続先の情報を付ける |
| `limits` | 出力の上限、タイムアウト、エラー、操作ログ。先頭に接続先の情報を付ける |

Table: help の topic

接続先の情報は、ホスト名、OS とアーキテクチャ、ユーザー、作業ディレクトリ、既定のシェル、既定の文字コード、出力の上限、タイムアウト、公開しているツールの組の表と、制限の表 (制限がない場合はその旨) です。  
未知の `topic` を指定した場合は、指定できる値を示してエラーにします。

各 topic は、リソース `host-shell://help/<topic>` としても読めます。内容は `help` の結果と同じです。

## コマンド実行

### exec_run

コマンドを実行し、終了を待って結果を返します。  
シェルのコマンド行を渡す `command` と、シェルを介さずにプログラムを起動する `argv` のどちらか一方を指定します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `command` | string | ※ | シェルに渡すコマンド行 |
| `argv` | string[] | ※ | シェルを介さずに起動するプログラムと引数。先頭がプログラム |
| `shell` | string | | `command` を解釈するシェル。省略時は OS の既定 ([シェル](#シェル) を参照) |
| `cwd` | string | | 作業ディレクトリ。省略時は `host-shell` の作業ディレクトリ |
| `env` | object | | 追加、上書きする環境変数 (名前と値の組) |
| `stdin` | string | | 標準入力に渡すテキスト。渡したあとに閉じる。省略時は空の入力 |
| `timeout_sec` | integer | | タイムアウト (秒)。省略時は `--timeout`、上限は `--max-timeout` |
| `output_encoding` | string | | 標準出力と標準エラーの文字コード。省略時は `auto` ([文字コードの指定](#文字コードの指定)) |
| `stdin_encoding` | string | | `stdin` を変換する文字コード。省略時は `utf-8`。ただし Windows で PowerShell (`powershell`、`pwsh`) が実行する場合は、コンソールのコード ページ (`system`)。`auto` は指定できない |

Table: exec_run の引数 (※ command と argv のどちらか一方が必須)

呼び出しごとに新しいプロセスを起動します。前の呼び出しの `cd` や環境変数の設定は引き継ぎません。`cwd` と `env` で指定してください。

| 出力 | 型 | 内容 |
|---|---|---|
| `exit_code` | integer | 終了コード。シグナルで終了した場合などは -1 |
| `stdout` | string | 標準出力 |
| `stderr` | string | 標準エラー |
| `stdout_truncated` | boolean | 標準出力を切り詰めた場合に `true` |
| `stderr_truncated` | boolean | 標準エラーを切り詰めた場合に `true` |
| `stdout_encoding` | string | 標準出力を UTF-8 以外から変換した場合に、その文字コード |
| `stderr_encoding` | string | 標準エラーを UTF-8 以外から変換した場合に、その文字コード |
| `timed_out` | boolean | タイムアウトで停止した場合に `true` |
| `cancelled` | boolean | クライアントが要求を取り消して停止した場合に `true` |
| `duration_ms` | integer | 実行にかかった時間 (ミリ秒) |

Table: exec_run の出力

タイムアウトになると、コマンドを子孫のプロセスも含めて強制終了し、それまでの出力を返します。  
このときの `exit_code` は OS によって異なります (Linux では -1)。`timed_out` で判断してください。

要求に `progressToken` がある場合は、実行中に 1 秒ごとに `notifications/progress` を送ります。`progress` は経過秒数、`message` は経過時間、出力のバイト数、出力の最後の行です。  
クライアントが要求を取り消した (`notifications/cancelled`) 場合は、すぐにプロセス ツリーを強制終了します。

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

日本語の Windows の cmd の出力は Shift_JIS ですが、`output_encoding` の既定の `auto` により UTF-8 に変換して返し、`stdout_encoding` は `shift_jis` になります。

## 長時間の実行 (ジョブ)

ビルドやログの監視など、終了を待つと時間がかかるコマンドは、ジョブとしてバックグラウンドで実行します。

```plantuml
@startuml ジョブの状態
    caption ジョブの状態
    [*] --> running : job_start
    running --> running : job_stop (kill 以外のシグナル)
    running --> ended : コマンドの終了
    running --> ended : job_stop、タイムアウト、host-shell の終了
    ended --> [*] : 終了したジョブが 50 を超えた、host-shell の終了
@enduml
```

ジョブの仕様は次のとおりです。

- 識別子は `job-1`、`job-2` のように、`host-shell` のプロセスごとに 1 から振ります。
- 入出力は、パイプにつなぐ方式 (既定) と、疑似端末 (PTY) につなぐ方式 (`pty`) があります。
- 標準出力と標準エラーは、1 つのストリームにまとめ、UTF-8 に変換してから保持します。`offset` や `output_bytes` は、変換後の UTF-8 のバイト数で数えます。
- 出力は、直近の少なくとも `--max-output` の 4 倍 (既定で 256 KiB) を保持し、それより古い部分は破棄します。
- 終了したジョブは、直近の 50 個まで一覧に残します。それを超えると、古いものから破棄します。
- `host-shell` が終了する (SSH が切断される) と、実行中のジョブはすべて停止します。切断後も動かし続けたい処理には、`nohup` やサービスとしての登録など、OS の仕組みを使ってください。

### job_start

コマンドをバックグラウンドで開始し、すぐにジョブの状態を返します。  
引数は [`exec_run`](#exec_run) の引数に、次のものを加えたものです。  
ただし、`timeout_sec` を省略した場合は時間制限なしで実行し、指定した場合も `--max-timeout` で制限しません。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `stdin_open` | boolean | | 標準入力を開いたままにし、[`job_write`](#job_write) で後から送れるようにする。`stdin` を指定した場合は、それを最初に送る |
| `pty` | boolean | | 疑似端末につないで起動する (下記) |
| `cols` | integer | | `pty` の端末の幅。省略時は 120 |
| `rows` | integer | | `pty` の端末の高さ。省略時は 40 |

Table: job_start に固有の引数

`stdin_open` も `pty` も指定しない場合、標準入力は `exec_run` と同じく、`stdin` を渡したあとに閉じるか、空の入力です。

#### 疑似端末 (pty)

`pty` を指定すると、コマンドを疑似端末につないで起動します。端末でないと動かないプログラム (パスワードを尋ねる `sudo` や `ssh`、プロンプトを出す REPL、`top`、パイプでは出力をためこむプログラムなど) に使います。

| 項目 | Linux | Windows |
|---|---|---|
| 仕組み | pty。新しいセッションを作り、端末を制御端末にする | ConPTY (Windows 10 1809 / Windows Server 2019 以降) |
| 環境変数 | `TERM` がなければ `xterm-256color` を加える | 変更しない |
| PowerShell | | `-NonInteractive` を付けずに起動する (`Read-Host` などが使える) |

Table: 疑似端末

- 標準出力と標準エラーは、端末の出力として 1 つにまとまります。入力は常に開いています。
- 出力の文字コードは、パイプの場合と同じく自動判定で UTF-8 に変換します。
- コマンドが終了したあとも子孫のプロセスが端末を開いている場合は、最大 2 秒待ってから端末を閉じます。

出力は、後述の [ジョブの状態](#ジョブの状態) です。

### job_read

ジョブの出力を、指定した位置から読みます。ジョブの状態も一緒に返します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `id` | string | ○ | ジョブの識別子 |
| `offset` | integer | | 読み始める位置 (ジョブの開始からのバイト数)。前回の `next_offset` を指定する。負の値は、末尾からのバイト数 (`-4000` で最後の 4000 バイト) |
| `max_bytes` | integer | | 読み取る最大のバイト数。省略時と、`--max-output` を超える場合は `--max-output` |
| `wait_sec` | integer | | 待つ最大の秒数 (最大 300)。何を待つかは `until` と `idle_ms` で決まる。ジョブが終了したら待つのをやめる |
| `until` | string | | `offset` より後の出力が、この正規表現に一致するまで待つ (プロンプトや「Listening on」など) |
| `idle_ms` | integer | | 出力が届き、その後この時間 (ミリ秒) 新しい出力がなくなるまで待つ |
| `raw` | boolean | | `pty` のジョブで、端末の制御のシーケンスを除かずに返す |

Table: job_read の引数

`wait_sec` だけを指定した場合は、`offset` より後の出力が届いた時点で戻ります。  
`until` は、`pty` のジョブでは制御のシーケンスを除いた出力と照合します。

`pty` のジョブの出力は、既定で次のように整えてから返します。`raw` を指定すると、端末が受け取ったとおりに返します。

- 色、カーソルの移動、ウィンドウのタイトルなどのエスケープ シーケンスを除く
- CR LF を LF にし、行の途中の CR で上書きされた行 (進み具合の表示など) は、最後に書かれた内容だけを残す
- バックスペースで直前の文字を消し、タブと改行以外の制御文字を除く
- 末尾のエスケープ シーケンスが途中で切れている場合は、その手前までを返し、`next_offset` をシーケンスの先頭にする

| 出力 | 型 | 内容 |
|---|---|---|
| `status` | object | [ジョブの状態](#ジョブの状態) |
| `matched` | boolean | `until` が一致した場合に `true` |
| `output` | string | 読み取った出力 |
| `next_offset` | integer | 次に読み始める位置 |
| `more` | boolean | `next_offset` より後の出力がすでにある場合に `true`。待たずに続けて読める |
| `dropped` | integer | `offset` の位置がすでに破棄されていて、読めなかったバイト数 |

Table: job_read の出力

出力を最後まで読むには、`next_offset` を渡しながら繰り返し呼び、`status.running` が `false` で `output` が空になったら終わります。  
`wait_sec` を指定すると、出力がない間に繰り返し呼ぶ回数を減らせます。

### job_write

`stdin_open` か `pty` を指定して開始したジョブの入力に、テキストを送ります。入力を閉じることや、端末の大きさを変えることもできます。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `id` | string | ○ | ジョブの識別子 |
| `input` | string | | 送るテキスト。改行は自動では付けない。行を送るときは末尾に `\n` を含める |
| `close` | boolean | | `input` を送ったあとに標準入力を閉じる (入力の終わりを伝える。端末の Ctrl-D に相当)。`pty` のジョブでは、Linux は Ctrl-D、Windows は Ctrl-Z と Enter を送る |
| `cols`、`rows` | integer | | `pty` のジョブの端末の大きさを変える。両方を指定する |

Table: job_write の引数 (input、close、cols と rows のいずれかが必須)

| 出力 | 型 | 内容 |
|---|---|---|
| `status` | object | [ジョブの状態](#ジョブの状態) |
| `bytes_written` | integer | 送ったバイト数 (文字コードの変換後) |

Table: job_write の出力

`input` は、`job_start` の `stdin_encoding` で変換してから送ります。  
子プロセスが入力を読まず、10 秒以内に送り終わらない場合はエラーを返します。送り残した分は裏で送り続けます。  
標準入力が開いていない場合 (`stdin_open` を指定していない、閉じた、ジョブが終了した) はエラーにします。

パイプの場合、端末でないと問い合わせをしないプログラムや、出力をためこむプログラムがあります。その場合は `pty` で開始してください。  
`pty` のジョブでは、制御のキーを文字として送ります。Ctrl-C は `\u0003`、Ctrl-D は `\u0004`、Esc は `\u001b`、上矢印は `\u001b[A` です。Enter に `\r` を求めるプログラムもあります。

```json
{"id": "job-3", "input": "y\n"}
```

```json
{"id": "job-4", "input": "\u0003"}
```

### job_stop

ジョブにシグナルを送るか、子孫のプロセスも含めて強制終了し、終了を待ってから状態を返します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `id` | string | ○ | ジョブの識別子 |
| `signal` | string | | `kill` (既定)、または Linux で `interrupt`、`terminate`、`hangup`、`quit`、`stop`、`continue`、`usr1`、`usr2` |
| `wait_sec` | integer | | 終了を待つ秒数。省略時は 5 |

Table: job_stop の引数

| signal | 動作 |
|---|---|
| `kill` | 子孫のプロセスも含めて強制終了する ([終了のさせ方](#終了のさせ方)) |
| `interrupt` (`pty` のジョブ) | 端末に Ctrl-C を送る。Windows でも使える |
| その他 (Linux) | ジョブのプロセス グループにシグナルを送る |

Table: job_stop のシグナル

`kill` 以外は、ジョブが終了しないこともあります。返した状態の `running` で確かめてください。  
`kill` で、すでに終了したジョブに対して呼んでも、エラーにはなりません。`kill` 以外のシグナルを、終了したジョブに送るとエラーになります。

### job_list

このセッションで開始したすべてのジョブの状態を、開始した順に返します。引数はありません。

| 出力 | 型 | 内容 |
|---|---|---|
| `jobs` | object[] | [ジョブの状態](#ジョブの状態) の配列 |

Table: job_list の出力

### ジョブの状態

| 項目 | 型 | 内容 |
|---|---|---|
| `id` | string | ジョブの識別子 |
| `command` | string | 実行したコマンド (`argv` の場合は空白でつないだもの) |
| `pid` | integer | プロセス ID |
| `pty` | boolean | 疑似端末で動いている場合に `true` |
| `running` | boolean | 実行中の場合に `true` |
| `stdin_open` | boolean | 入力が開いていて、`job_write` で送れる場合に `true` |
| `exit_code` | integer | 終了コード。終了した場合だけ含める |
| `signal` | string | 最後に `job_stop` で送ったシグナル (`kill` を除く) |
| `output_encoding` | string | 出力を UTF-8 以外から変換した場合に、その文字コード |
| `error` | string | 終了を待つ処理で起きたエラー。通常は含めない |
| `started_at` | string | 開始した時刻 (RFC 3339) |
| `ended_at` | string | 終了した時刻 (RFC 3339)。終了した場合だけ含める |
| `output_bytes` | integer | これまでに出力された合計のバイト数 |

Table: ジョブの状態

## ファイル操作

### fs_list

ディレクトリの中の項目を、名前の順に返します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `path` | string | | ディレクトリのパス。省略時は作業ディレクトリ |
| `max_entries` | integer | | 返す項目の最大数。省略時は 1000 |

Table: fs_list の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `path` | string | ディレクトリの絶対パス |
| `entries` | object[] | 項目の配列 (下記) |
| `truncated` | boolean | `max_entries` で打ち切った場合に `true` |

Table: fs_list の出力

各項目は次の形です。シンボリック リンクはたどらず、リンクそのものの情報を返します。

| 項目 | 型 | 内容 |
|---|---|---|
| `name` | string | 名前 |
| `type` | string | `file`、`dir`、`symlink`、`other` のいずれか |
| `size` | integer | サイズ (バイト) |
| `mode` | string | 権限の表記 (例: `-rw-r--r--`、`drwxr-xr-x`) |
| `mod_time` | string | 更新時刻 (RFC 3339) |

Table: ディレクトリの項目

### fs_stat

ファイルまたはディレクトリの属性を返します。  
パスの最後がシンボリック リンクの場合は、リンクそのものの属性と、リンク先を返します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `path` | string | ○ | パス |

Table: fs_stat の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `path` | string | 絶対パス |
| `entry` | object | 属性 ([fs_list](#fs_list) の項目と同じ形) |
| `link_target` | string | シンボリック リンクの場合のリンク先 |

Table: fs_stat の出力

### fs_read

ファイルを読みます。読み方は次の 3 とおりです。

| 読み方 | 指定 | 用途 |
|---|---|---|
| 行の範囲 | `line_start` を指定する (`encoding` は `text`) | ソース コードやログの一部を読む |
| バイトの範囲 (テキスト) | `line_start` を省略する | 先頭から順に読む |
| バイトの範囲 (base64) | `encoding` に `base64` を指定する | バイナリ ファイルを読む。バイト列をそのまま扱う |

Table: fs_read の読み方

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `path` | string | ○ | ファイルのパス |
| `encoding` | string | | `text` (既定) または `base64` |
| `charset` | string | | `text` の場合のファイルの文字コード。省略時は `auto` ([文字コードの指定](#文字コードの指定)) |
| `line_start` | integer | | 読み始める行 (1 から数える)。指定すると `offset` と `length` は無視する |
| `line_count` | integer | | 読む行数。省略時は 2000 |
| `line_numbers` | boolean | | 行の範囲の場合に、各行の先頭に行番号とタブを付ける (`cat -n` と同じ形)。`line_start` が必要 |
| `offset` | integer | | 読み始める位置 (バイト)。省略時は 0 |
| `length` | integer | | 読む最大のバイト数。省略時は上限まで |

Table: fs_read の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `path` | string | 絶対パス |
| `size` | integer | ファイルのサイズ (バイト) |
| `encoding` | string | `text` または `base64` |
| `charset` | string | `text` の場合に、変換に使った文字コード。`fs_write` の `charset` に渡すと文字コードを保てる |
| `bom` | boolean | ファイルの先頭に BOM がある場合に `true`。BOM は `content` に含めない |
| `newline` | string | `content` に含まれる改行の種類 (`lf`、`crlf`、`cr`、`mixed`)。改行がなければ含めない |
| `content` | string | 読み取った内容 |
| `eof` | boolean | ファイルの末尾まで読んだ場合に `true` |
| `next_offset` | integer | バイトの範囲の場合に、続きを読む位置 |
| `next_line` | integer | 行の範囲の場合に、続きを読む行。末尾まで読んだ場合は含めない |
| `truncated` | boolean | 要求した範囲より前に上限に達した場合に `true` |

Table: fs_read の出力

1 回に読む量の上限は次のとおりです。

- テキストは、UTF-8 に変換した後の大きさで `--max-output` まで読みます。行の範囲では、上限を超える前の行までを返します。1 行だけで上限を超える場合は、その行を切り詰めて返し、次の行へ進めます。
- base64 は、変換後の大きさが `--max-output` に収まるよう、その 4 分の 3 (既定で 48 KiB) まで読みます。

行の内容は改行文字を含めてそのまま返します。CRLF も変換しません。  
`line_numbers` を指定すると、各行を `     12\t内容` のように、6 桁に右寄せした行番号とタブで始めます。この接頭辞はファイルの内容ではないため、`fs_edit` や `fs_write` に渡さないでください。  
バイトの範囲をテキストで読む場合、`offset` と `next_offset` はファイルのバイト数です。範囲の末尾で分断された文字は、次の読み取りに回します。`offset` に文字の途中を指定した場合、その文字は置換文字になります。  
`offset` が 0 のときに BOM がある場合は、BOM の後ろから読みます。

```json
{"path": "/var/log/nginx/error.log", "line_start": 1, "line_count": 200}
```

```json
{"path": "C:/work/legacy.bat", "charset": "shift_jis", "line_start": 1}
```

```json
{"path": "C:/tools/app.exe", "encoding": "base64", "offset": 0}
```

### fs_write

ファイルを書きます。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `path` | string | ○ | ファイルのパス |
| `content` | string | ○ | 書き込む内容 |
| `encoding` | string | | `content` の形式。`text` (既定) または `base64` |
| `charset` | string | | `text` の場合に書き込む文字コード。省略時は `utf-8`。`auto` は指定できない |
| `newline` | string | | `text` の場合の改行の変換。`keep` (既定、変換しない)、`lf`、`crlf` |
| `bom` | boolean | | `text` の場合に、先頭に BOM を付ける。`utf-8`、`utf-16le`、`utf-16be` だけで使え、ファイルの先頭に書く場合 (`append` でなく、`offset` が 0 または省略) に限る |
| `mode` | string | | `overwrite` (既定)、`append`、`create` のいずれか (下記) |
| `offset` | integer | | この位置 (バイト) から書き込む。ファイルを切り詰めず、書き込んだ範囲だけを置き換える (下記) |
| `make_dirs` | boolean | | 親ディレクトリがない場合に作る (権限は `0755`) |
| `perm` | string | | 新しく作るファイルの権限を 8 進数で指定する (例: `0600`)。既定は `0644`。umask の影響を受ける。Windows では無視する |
| `atomic` | boolean | | 一時ファイルに書いてから名前を変えて置き換える (下記の [原子的な置き換え](#原子的な置き換え-atomic))。`mode` が `overwrite` で、`offset` がない場合だけ使える |

Table: fs_write の引数

| mode | 振る舞い |
|---|---|
| `overwrite` | ファイルがなければ作り、あれば内容を置き換える |
| `append` | ファイルがなければ作り、あれば末尾に追加する |
| `create` | ファイルがなければ作る。すでにある場合はエラーにする |

Table: fs_write の mode

`offset` を指定した場合の振る舞いは次のとおりです。

| mode | offset を指定したときの振る舞い |
|---|---|
| `overwrite` (既定) | ファイルがなければ作る。切り詰めずに、`offset` から `content` の長さの範囲だけを置き換える。それ以外の部分は変わらない |
| `append` | 指定できない (エラー) |
| `create` | ファイルがなければ作り、`offset` から書き込む。すでにある場合はエラーにする |

Table: fs_write の offset

#### 原子的な置き換え (atomic)

既定では、既存のファイルを切り詰めて、その場で書きます。所有者、権限、ハード リンク、シンボリック リンクの先は変わりませんが、書き込みの途中で失敗するとファイルは途中までの内容になり、書き込み中に読んだプロセスには途中の内容が見えます。

`atomic: true` を指定すると (`fs_write` と `fs_edit`)、次の手順で置き換えます。動いているサービスが読む設定ファイルなど、途中の内容を見せたくない場合に使います。

1. 対象がシンボリック リンクなら、リンクの先を対象にします。
2. 対象と同じディレクトリに一時ファイル (`.<名前>.host-shell-tmp-<乱数>`) を作り、内容を書いて同期 (fsync) します。
3. 既存のファイルの権限 (モード) を引き継ぎます。所有者とグループも引き継ぎ、引き継げない場合 (`host-shell` が root でなく、ファイルの持ち主でない場合など) は、一時ファイルを消してエラーにします。この場合は `atomic` なしで書いてください。
4. 名前を変えて置き換えます。失敗した場合は一時ファイルを消します。

ACL、拡張属性、ハード リンクの関係は引き継ぎません。ディレクトリに書き込み権がない場合は、ファイルに書き込めても置き換えられません。  
Windows では、ほかのプロセスがファイルを開いている間は置き換えられない場合があります。新しいファイルの ACL は、ディレクトリから受け継ぎます。

`offset` は 0 以上です。`0` を指定した場合も、省略した場合と違ってファイルを切り詰めません。  
ファイルの末尾より後ろの位置を指定すると、末尾から `offset` までの間は 0 のバイトで埋まります (ファイル システムによっては疎なファイルになります)。

| 出力 | 型 | 内容 |
|---|---|---|
| `path` | string | 絶対パス |
| `bytes_written` | integer | 書き込んだバイト数 |
| `size` | integer | 書き込んだ後のファイルのサイズ (バイト) |

Table: fs_write の出力

`text` の場合は、改行の変換、文字コードの変換、BOM の付加の順に処理します。  
`charset` で表せない文字 (Shift_JIS に絵文字など) が含まれる場合は、文字と位置を示してエラーにし、何も書き込みません。  
`base64` の場合に `charset`、`newline`、`bom` を指定するとエラーになります。

`offset` を省略した `overwrite` は、既存のファイルをその場で書き換えます。所有者と権限は維持しますが、書き込みの途中で失敗すると内容が不完全になります。  
1 回の呼び出しで送れる大きさは、MCP の受信メッセージの上限 (16 MiB) に収まる範囲です。大きなファイルは `offset` か `append` で分割して送ってください。

```json
{"path": "/srv/data/image.bin", "content": "AAECAw==", "encoding": "base64", "offset": 1048576}
```

```json
{"path": "C:/work/run.bat", "content": "@echo off\necho 完了\n", "charset": "shift_jis", "newline": "crlf"}
```

既存のファイルの一部を変更する場合は、ファイル全体を送り直す `fs_write` より、[`fs_edit`](#fs_edit) を使ってください。

### fs_edit

既存のファイルの中の文字列を、完全に一致する箇所だけ置換します。  
ファイルの文字コード、BOM、改行を判定し、置換したあとも同じ形式で書き戻します。

置換の指定は、`old_string` と `new_string` (1 つの置換) か、`edits` (複数の置換) のどちらか一方で行います。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `path` | string | ○ | ファイルのパス |
| `old_string` | string | ※ | 置換する文字列。空白、インデント、改行を含めて完全に一致させる |
| `new_string` | string | | 置換後の文字列。空の場合は `old_string` を削除する |
| `replace_all` | boolean | | 一致するすべての箇所を置換する。省略時は、一致する箇所がちょうど 1 つでなければエラー |
| `edits` | object[] | ※ | 複数の置換。各要素は `old_string`、`new_string`、`replace_all`。順に適用し、各置換は前の置換の結果に対して行う |
| `charset` | string | | ファイルの文字コード。省略時は `auto` ([自動判定](#自動判定)) |
| `dry_run` | boolean | | 置換を確かめて結果を返し、ファイルには書き込まない |
| `atomic` | boolean | | 一時ファイルに書いてから名前を変えて置き換える。[`fs_write`](#原子的な置き換え-atomic) と同じ |

Table: fs_edit の引数 (※ old_string と edits のどちらか一方が必須)

| 出力 | 型 | 内容 |
|---|---|---|
| `path` | string | 絶対パス |
| `replacements` | integer | 置換した箇所の合計 |
| `lines` | integer[] | 置換した箇所が始まる行 (置換後のファイルの行番号)。最大 50 件 |
| `snippet` | string | 置換した範囲と前後 2 行を、行番号付きで返す (最大 80 行)。離れた範囲の間は `...` |
| `charset` | string | 読み書きに使った文字コード |
| `bom` | boolean | ファイルに BOM がある場合に `true` (そのまま残す) |
| `newline` | string | 置換後のファイルの改行の種類 |
| `size` | integer | 置換後のファイルのサイズ (バイト) |
| `sha256` | string | 置換後のファイルの SHA-256 |
| `dry_run` | boolean | `dry_run` を指定した場合に `true` |

Table: fs_edit の出力

置換の規則は次のとおりです。

- 1 つでも置換に失敗した場合は、ファイルに何も書き込みません。
- `old_string` が見つからない場合は、エラーにします。各行の前後の空白を無視すると一致する箇所があれば、その行番号もエラーの説明に含めます。
- `old_string` が複数の箇所に一致し、`replace_all` を指定していない場合は、一致した行番号 (最大 10 件) を示してエラーにします。
- 改行が CRLF のファイルでは、`old_string` と `new_string` の LF を CRLF とみなして照合し、CRLF で書き込みます。エージェントは改行を LF で書けます。
- `old_string` と `new_string` が同じ場合はエラーにします。

文字コードは、BOM があればそれを使い、なければ UTF-8 として正しいかを調べ、正しくなければ接続先の既定の文字コードとみなします。  
`fs_read` と違い、行ごとではなくファイル全体で 1 つの文字コードに決めます。  
判定した文字コードで読んで書き戻したときに元のバイト列を再現できない場合は、文字コードの判定が誤っているとみなしてエラーにします。`charset` を指定してください。  
置換後の文字列が、その文字コードで表せない文字を含む場合もエラーにします。

扱えるファイルの大きさは 32 MiB までです。これより大きいファイルは、`fs_write` の `offset` で書き換えてください。  
ファイルはその場で書き換えます。所有者と権限は維持します。

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

ディレクトリを作ります。親のディレクトリがなければ、それも作ります (権限は `0755`)。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `path` | string | ○ | 作るディレクトリのパス |

Table: fs_mkdir の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `path` | string | 絶対パス |
| `created` | boolean | 作った場合に `true`。すでにあった場合は `false` |

Table: fs_mkdir の出力

ディレクトリがすでにある場合はエラーにしません。同じ名前のファイルがある場合はエラーにします。

### fs_move

ファイルやディレクトリを移動します。名前の変更にも使います。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `source` | string | ○ | 移動するファイルまたはディレクトリ |
| `destination` | string | ○ | 移動先のパス。移動先のディレクトリではなく、移動後のパスそのものを指定する |
| `overwrite` | boolean | | 移動先にファイルがある場合に置き換える。移動先がディレクトリの場合は指定してもエラー |
| `make_dirs` | boolean | | 移動先の親のディレクトリがなければ作る |

Table: fs_move の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `source` | string | 移動元の絶対パス |
| `destination` | string | 移動先の絶対パス |
| `copied` | boolean | ファイル システムをまたいだため、コピーしてから移動元を削除した場合に `true` |

Table: fs_move の出力

移動先の親のディレクトリがない場合は、`make_dirs` を指定しない限りエラーにします。  
ファイル システム (Windows ではドライブ) をまたぐ移動は、コピーしてから移動元を削除します。コピーの途中で失敗した場合、移動元は残します。

### fs_copy

ファイルをコピーします。`recursive` を指定すると、ディレクトリを中身ごとコピーします。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `source` | string | ○ | コピーするファイルまたはディレクトリ |
| `destination` | string | ○ | コピー先のパス。コピー先のディレクトリではなく、コピーそのもののパスを指定する |
| `recursive` | boolean | | ディレクトリをコピーする場合に必要 |
| `overwrite` | boolean | | コピー先にファイルがある場合に置き換える。ディレクトリどうしの場合は中身を統合する |
| `make_dirs` | boolean | | コピー先の親のディレクトリがなければ作る |

Table: fs_copy の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `source` | string | コピー元の絶対パス |
| `destination` | string | コピー先の絶対パス |
| `files` | integer | コピーしたファイルの数 |
| `dirs` | integer | 作ったディレクトリの数 |
| `symlinks` | integer | 作り直したシンボリック リンクの数 |
| `bytes` | integer | コピーしたバイト数 |

Table: fs_copy の出力

コピーの仕様は次のとおりです。

- ファイルの権限と更新時刻を引き継ぎます。所有者は引き継ぎません (コピーしたユーザーになります)。
- シンボリック リンクはたどらず、同じリンク先を指すリンクを作ります。
- 通常のファイル、ディレクトリ、シンボリック リンク以外 (デバイスなど) は飛ばします。
- コピー先がコピー元のディレクトリの中にある場合は、エラーにします。
- 途中で失敗した場合は、それまでにコピーしたものを残し、コピーしたファイルの数をエラーの説明に含めます。

### fs_remove

ファイル、シンボリック リンク、ディレクトリを削除します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `path` | string | ○ | 削除するパス。シンボリック リンクは、リンク先ではなくリンクそのものを削除する |
| `recursive` | boolean | | 空でないディレクトリを、中身ごと削除する場合に必要 |
| `missing_ok` | boolean | | パスが存在しない場合もエラーにしない |

Table: fs_remove の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `path` | string | 絶対パス |
| `removed` | boolean | 削除した場合に `true`。`missing_ok` でパスがなかった場合は `false` |
| `type` | string | 削除したものの種類 (`file`、`dir`、`symlink`、`other`) |

Table: fs_remove の出力

ファイル システムのルート (`/`、`C:\` など) と、`host-shell` を実行しているユーザーのホーム ディレクトリは削除しません。エラーにします。  
これは誤操作を防ぐためのもので、権限による制限の代わりにはなりません。

## 検索

`search_grep`、`search_files`、`fs_manifest` は、指定したディレクトリの下を再帰的にたどります。共通の仕様は次のとおりです。

- 名前の順にたどります。
- シンボリック リンクはたどりません。
- `exclude_dirs` に挙げた名前のディレクトリには降りません。省略時は `.git`、`.hg`、`.svn` です。空の配列を指定すると、どのディレクトリも除外しません。
- 権限がないなどで読めなかった項目は飛ばし、その数を `skipped` に返します。
- 結果のパスは、`path` に指定したパスにつなげたものです。`path` が相対パスなら、結果も相対パスです。

### glob

`search_grep` と `fs_manifest` の `include`、`search_files` の `pattern` は、次の glob です。

| 書き方 | 意味 |
|---|---|
| `/` を含まない (`*.go`) | 項目の名前 (パスの最後の要素) と照合する |
| `/` を含む (`src/**/*.ts`) | `path` からの相対パス (区切りは `/`) と照合する。先頭の `./` は無視する |
| `*`、`?` | パスの 1 つの要素の中の、任意の文字列と任意の 1 文字 |
| `**` | 0 個以上のディレクトリ。`/` で区切った要素として書く (`**/test/*.py`) |
| `[abc]`、`[a-z]` | 文字の集合 |
| `{a,b}` | いずれかの文字列 (`*.{yml,yaml}`)。入れ子にはできない |

Table: glob の構文

Windows の接続先では、大文字と小文字を区別せずに照合し、`\` も区切りとして受け付けます。  
不正な glob (閉じていない `[` など) はエラーにします。

### search_grep

ファイルの内容を、行ごとに正規表現で検索します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `pattern` | string | ○ | 正規表現 ([RE2 の構文](https://github.com/google/re2/wiki/Syntax))。後方参照と先読みは使えない |
| `path` | string | | 検索するディレクトリまたはファイル。省略時は作業ディレクトリ |
| `include` | string | | 対象のファイルの [glob](#glob) (例: `*.go`、`src/**/*.ts`) |
| `exclude_dirs` | string[] | | 降りないディレクトリの名前 |
| `gitignore` | boolean | | git が無視するファイルとディレクトリを飛ばす。省略時は `true` ([.gitignore の扱い](#gitignore-の扱い)) |
| `ignore_case` | boolean | | 大文字と小文字を区別しない |
| `literal` | boolean | | `pattern` を正規表現ではなく、そのままの文字列として検索する |
| `output_mode` | string | | `content` (既定、一致した行)、`files` (一致したファイルのパスだけ)、`count` (ファイルごとの一致した行の数) |
| `context` | integer | | `content` の場合に、一致した行の前後に返す行数 (`grep -C` と同じ) |
| `before` | integer | | `content` の場合に、一致した行の前に返す行数。`context` より優先する (`grep -B`) |
| `after` | integer | | `content` の場合に、一致した行の後に返す行数。`context` より優先する (`grep -A`) |
| `max_matches` | integer | | 返す最大数。`content` では行の数、`files` と `count` ではファイルの数。省略時は 200 |
| `max_file_bytes` | integer | | これより大きいファイルを飛ばす。省略時は 10 MiB |
| `charset` | string | | ファイルの文字コード。省略時は `auto` ([文字コードの指定](#文字コードの指定)) |
| `multiline` | boolean | | 行ごとではなく、ファイル全体に照合する (下記の [複数の行にまたがる検索](#複数の行にまたがる検索-multiline)) |

Table: search_grep の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `matches` | object[] | `content` の場合に、一致した行の配列 (下記)。一致がなければ含めない |
| `files` | object[] | `files` と `count` の場合に、一致したファイルの配列。各要素は `path` と、`count` の場合の `count` (一致した行の数) |
| `files_scanned` | integer | 検索したファイルの数 |
| `files_matched` | integer | 一致があったファイルの数 |
| `skipped` | integer | 読めなかった項目の数 |
| `skipped_large` | integer | 大きすぎるために飛ばしたファイルの数 (`max_file_bytes`、`multiline` では 32 MiB も) |
| `truncated` | boolean | `max_matches` で打ち切った場合に `true` |

Table: search_grep の出力

| 項目 | 型 | 内容 |
|---|---|---|
| `path` | string | ファイルのパス |
| `line` | integer | 行番号 (1 から数える)。`multiline` では一致の始まる行 |
| `end_line` | integer | `multiline` の場合に、一致の終わる行 |
| `text` | string | UTF-8 に変換した行の内容。改行は含めない。`multiline` では一致した範囲の内容 (改行を含む) |
| `before` | string[] | 前の行 (古い順)。`context` か `before` を指定した場合 |
| `after` | string[] | 後の行。`context` か `after` を指定した場合 |
| `charset` | string | 行を UTF-8 以外から変換した場合に、その文字コード |

Table: search_grep の一致

前後の行が別の一致と重なる場合も、まとめずにそれぞれの一致に含めます。  
`files` と `count` の場合は前後の行を返しません。`files` は、ファイルの最初の一致で次のファイルに進むため、`content` より速く終わります。

次のファイルは検索しません。

- 通常のファイル以外 (シンボリック リンク、デバイスなど)
- 先頭 8000 バイトに NUL を含むファイル (バイナリとみなす)。ただし、UTF-16 の BOM があるファイルと、`charset` に UTF-16 を指定した場合は検索する
- `max_file_bytes` より大きいファイル
- git が無視するファイル (`gitignore` が `true` の場合)

1 MiB を超える行があるファイルは、その行の手前までを検索します。  
正規表現は、UTF-8 に変換した後の行に対して照合します。日本語のパターンで、Shift_JIS や EUC-JP のファイルも検索できます。  
`text`、`before`、`after` は、行が 500 バイトを超える場合に先頭 500 バイト (文字の境界まで) に切り詰めます。

#### 複数の行にまたがる検索 (multiline)

`multiline: true` を指定すると、ファイル全体を UTF-8 に変換してから、パターンを照合します。

- `.` は改行にも一致します。`^` と `$` は各行の先頭と末尾に一致します (RE2 の `(?sm)`)。
- 一致ごとに、始まる行 (`line`)、終わる行 (`end_line`)、一致した範囲の内容 (`text`、500 バイトまで) を返します。`context`、`before`、`after` は、一致の範囲の前後の行を返します。
- 空の文字列への一致は返しません。
- ファイル全体を読むため、32 MiB (`fs_edit` の上限と同じ) を超えるファイルは飛ばし、`skipped_large` に数えます。`max_file_bytes` が小さい場合は、そちらを使います。
- 文字コードの自動判定は、行ごとではなく、ファイル全体で行います (全体が UTF-8 として正しくなければ、接続先の既定の文字コードとみなします)。

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

`search_grep` と `search_files` は、既定で、git が無視するファイルとディレクトリを飛ばします。`gitignore: false` を指定すると、それらも対象にします。`exclude_dirs` は、これとは別に働きます。

読む規則のファイルは次のとおりです。git の全体の設定 (`core.excludesFile`) は読みません。

1. リポジトリの `.git/info/exclude` (検索の起点がリポジトリの中の場合)
2. リポジトリのルートから検索の起点までの各ディレクトリの `.gitignore` (起点がリポジトリの外の場合は、起点の `.gitignore` から)
3. 検索で降りる各ディレクトリの `.gitignore`

git と同じく、後に読む規則 (深いディレクトリの規則) が優先し、同じファイルの中では後の行が優先します。リポジトリのルートは、`.git` のあるディレクトリです。

| 書式 | 意味 |
|---|---|
| 空行、`#` で始まる行 | 無視する (`\#` で始めると `#` で始まる名前) |
| 行末の空白 | 無視する (`\ ` で残す) |
| `!pattern` | 否定。それまでの規則で無視したものを戻す |
| `pattern/` | ディレクトリだけに一致する |
| 先頭または途中に `/` がある | その `.gitignore` のディレクトリからの相対パスに一致する |
| `/` がない | その `.gitignore` のディレクトリ以下の、どの深さの名前にも一致する |
| `*`、`?`、`[...]`、`[!...]` | `/` 以外の任意の文字列、任意の 1 文字、文字の集合 |
| `**` | 0 個以上のディレクトリ (`a/**/b`、`**/b`、`a/**`) |

Table: .gitignore の書式

無視したディレクトリには降りないため、その中のファイルを否定で戻すことはできません (git と同じ)。検索の起点そのものは、無視される場所でも検索します。Windows では、大文字と小文字を区別しません。

### search_files

名前やパスが [glob](#glob) に一致するファイルやディレクトリを探します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `pattern` | string | | glob (例: `*.log`、`**/test/*.py`)。省略時はすべて |
| `path` | string | | 探すディレクトリ。省略時は作業ディレクトリ |
| `type` | string | | `file` (ディレクトリ以外)、`dir`、または省略 (両方) |
| `exclude_dirs` | string[] | | 降りないディレクトリの名前 |
| `gitignore` | boolean | | git が無視するファイルとディレクトリを飛ばす。省略時は `true` ([.gitignore の扱い](#gitignore-の扱い)) |
| `sort` | string | | `path` (既定、たどった順) または `mtime` (更新時刻の新しい順) |
| `max_results` | integer | | 返すパスの最大数。省略時は 1000 |

Table: search_files の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `paths` | string[] | 一致したパス。`path` 自身は含めない |
| `skipped` | integer | 読めなかった項目の数 |
| `truncated` | boolean | `max_results` で打ち切った場合に `true` |

Table: search_files の出力

`sort` に `mtime` を指定した場合は、一致したものを最大 10 万件まで集めてから並べ、先頭の `max_results` 件を返します。10 万件を超えた場合は `truncated` を `true` にします。

## 差分の確認

### fs_manifest

ディレクトリの下の通常のファイル (または 1 つのファイル) について、相対パス、サイズ、権限、SHA-256 の一覧を返します。  
手元に同じ構成のソースがある場合に、ハッシュを比べて差分のあるファイルだけを `fs_read` で読むために使います。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `path` | string | | 基準のディレクトリ、または 1 つのファイル。省略時は作業ディレクトリ |
| `include` | string | | 対象のファイルの [glob](#glob) (例: `*.c`、`src/**/*.h`) |
| `exclude_dirs` | string[] | | 降りないディレクトリの名前。省略時は `.git`、`.hg`、`.svn`。空の配列を渡すと、すべてに降りる |
| `max_files` | integer | | 返すファイルの最大数。省略時は 2000 |

Table: fs_manifest の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `root` | string | 基準のディレクトリの絶対パス |
| `files` | object[] | 各要素は `path` (基準からの相対パス。区切りは常に `/`。`path` がファイルの場合はその名前)、`size`、`mode` (権限の 8 進数。例: `0644`)、`sha256` (16 進数の小文字) |
| `skipped` | integer | 読めなかった項目の数 |
| `truncated` | boolean | `max_files` で打ち切った場合に `true` |

Table: fs_manifest の出力

区切りを常に `/` にするため、Windows の接続先の結果も、Linux の手元のファイルと同じ形で比べられます。

## 特権実行

### sudo_run

Linux の接続先で、コマンドを sudo で実行します。Windows の接続先では、このツールは登録されません。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `command` | string | ○ | 実行するコマンド行 |
| `user` | string | | 実行するユーザー。省略時は root |
| `cwd` | string | | 作業ディレクトリ |
| `stdin` | string | | コマンドの標準入力に渡すテキスト |
| `password` | string | | 利用者の sudo のパスワード。sudo が尋ねた場合だけ sudo に渡す |
| `timeout_sec` | integer | | タイムアウト (秒)。省略時は `--timeout`、上限は `--max-timeout` |
| `output_encoding` | string | | 出力の文字コード。[`exec_run`](#exec_run) と同じ |
| `stdin_encoding` | string | | 標準入力の文字コード。[`exec_run`](#exec_run) と同じ |

Table: sudo_run の引数

出力は [`exec_run`](#exec_run) と同じです。

`password` を省略した場合は、次のコマンドを実行します。

```text
sudo -n [-u <user>] -- sh -c '<command>'
```

`-n` を指定するため、sudo はパスワードを尋ねません。パスワードが必要な設定の場合は、sudo がエラーで終了し、その内容が `stderr` に入ります。  
このツールを使うには、sudoers で `sh` の実行を `NOPASSWD` で許可する必要があります。これは実質的にすべてのコマンドの実行を許可することになります。  
許可するコマンドを絞る場合は、`sudo_run` を使わずに、`exec_run` の `argv` で `sudo -n` を呼んでください。設定の例は [運用ガイド](operations.md#sudo-の設定) にあります。

```json
{"argv": ["sudo", "-n", "/usr/bin/systemctl", "restart", "nginx"]}
```

`env` は指定できません。sudo は既定で環境変数を初期化します。

#### パスワードを渡す場合

`password` を指定した場合は、`sudo -A [-u <user>] -- sh -c '<command>'` を実行し、`host-shell` 自身をパスワードの補助 (askpass) として使います。

1. 呼び出しごとに、権限 `0700` の一時ディレクトリを作り、その中に権限 `0600` の名前付きパイプを作ります。パスワードを 3 回分 (sudo は誤りで最大 3 回尋ねます) 書き込みます。
2. 環境変数 `SUDO_ASKPASS` に `host-shell` の実行ファイル、`HOST_SHELL_ASKPASS` に名前付きパイプのパスを設定して sudo を起動します。環境変数にパスワードは入れません。
3. sudo がパスワードを尋ねるときだけ、`host-shell` が補助として起動され、名前付きパイプから 1 行を読んで sudo に返します。
4. コマンドの終了後に、名前付きパイプとディレクトリを消します。

このため、パスワードが不要な場合 (`NOPASSWD` の規則や、sudo が認証を覚えている場合) でも、パスワードがコマンドの標準入力に渡ることはありません。  
操作ログには `password` の値の代わりに `<redacted>` を記録し、結果 (`stdout`、`stderr`) にパスワードが現れた場合は、`--redact` の指定によらず `[REDACTED]` に置き換えます。  
パスワードは、エージェントと MCP の通信を通ります。利用者がパスワードを教えた場合にだけ使ってください。

管理のツール (`proc_kill`、`svc_control`、`log_query`) の `sudo` は、`password` を受け取りません。パスワードが必要な場合は、`sudo_run` で同じコマンドを実行してください。  
対話的にパスワードを尋ねるプログラムには、`job_start` の `pty` と `job_write` も使えます。`job_write` の `input` は操作ログに記録しません。

## 環境情報

### sys_info

接続先の環境を返します。引数はありません。  
接続した直後に呼び、OS、既定のシェル、ホーム ディレクトリを確かめてから操作することを推奨します。

| 出力 | 型 | 内容 |
|---|---|---|
| `os` | string | `linux` または `windows` |
| `arch` | string | `amd64` または `arm64` |
| `hostname` | string | ホスト名 |
| `user` | string | ユーザー名 (Windows では `ドメイン\ユーザー`) |
| `uid` | string | ユーザー ID (Windows では SID) |
| `home` | string | ホーム ディレクトリ |
| `cwd` | string | `host-shell` の作業ディレクトリ |
| `path_separator` | string | パスの区切り文字 (`/` または `\`) |
| `default_shell` | string | `exec_run` の既定のシェル |
| `console_encoding` | string | コマンドの入出力の既定の文字コード ([接続先の既定の文字コード](#接続先の既定の文字コード)) |
| `file_encoding` | string | テキスト ファイルの既定の文字コード |
| `num_cpu` | integer | 論理 CPU の数 |
| `pid` | integer | `host-shell` のプロセス ID |
| `executable` | string | `host-shell` の実行ファイルのパス |
| `version` | string | `host-shell` のバージョン |
| `go_version` | string | ビルドに使った Go のバージョン |
| `tool_groups` | string[] | 公開しているツールの組 |
| `restrictions` | string[] | 制限の一覧 (`項目: 値`)。制限がない場合は含めない |

Table: sys_info の出力

## ネットワーク

ネットワークのツールは、すべて接続先のマシンから接続します。`localhost` は接続先のマシン自身です。  
接続先からしか届かないサービス (接続先の上の Web サービス、内部の API、データベースなど) を、ポート転送なしで確かめられます。  
宛先は [`--allow-host`](#ネットワークの宛先---allow-host) で制限できます。

### net_http

HTTP または HTTPS の要求を送り、状態、ヘッダー、本文を返します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `url` | string | ○ | 要求の URL (`http` または `https`) |
| `method` | string | | メソッド。省略時は `GET` |
| `headers` | object | | 要求のヘッダー (名前と値の組)。`Host` も指定できる |
| `body` | string | | 要求の本文 |
| `body_encoding` | string | | `text` (既定。UTF-8 で送る) または `base64` |
| `body_file` | string | | 接続先のこのファイルの内容を本文として送る。`body` とは同時に指定できない |
| `unix_socket` | string | | URL のホストの代わりに、この Unix ドメイン ソケットに接続する (例: `/var/run/docker.sock`) |
| `timeout_sec` | integer | | 全体のタイムアウト (秒)。省略時は `--timeout`、上限は `--max-timeout` |
| `no_redirects` | boolean | | リダイレクトをたどらずに 3xx の応答を返す。既定では最大 10 回たどる |
| `insecure` | boolean | | TLS の証明書を検証しない |
| `ca_cert` | string | | 加えて信頼する CA 証明書 (PEM) の、接続先のパス |
| `client_cert`、`client_key` | string | | クライアント証明書と秘密鍵 (PEM) の、接続先のパス |
| `proxy` | string | | プロキシの URL、または `none`。省略時は接続先の環境変数 (`HTTP_PROXY`、`HTTPS_PROXY`、`NO_PROXY`) に従う |
| `response_encoding` | string | | `auto` (既定)、`text`、`base64` |
| `max_bytes` | integer | | 返す本文の最大のバイト数。省略時と上限は `--max-output` |
| `save_to` | string | | 本文を返さずに、接続先のこのファイルに書く。大きさに上限はない |

Table: net_http の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `status`、`status_text` | integer、string | 状態コードとその名前 |
| `proto` | string | `HTTP/1.1`、`HTTP/2.0` など |
| `headers` | object | 応答のヘッダー。同じ名前の値は `, ` でつなぐ |
| `body` | string | 本文 |
| `body_encoding` | string | `text` または `base64` |
| `body_charset` | string | 本文を UTF-8 以外の charset から変換した場合に、その文字コード |
| `body_bytes` | integer | 受け取った本文のバイト数 |
| `body_truncated` | boolean | 本文の一部だけを返した場合に `true` |
| `saved_to` | string | `save_to` の絶対パス |
| `final_url`、`redirects` | string、string[] | リダイレクトをたどった場合の、最後の URL とたどった URL |
| `tls` | object | HTTPS の場合の TLS のバージョンと、相手の証明書 (`subject`、`issuer`、`dns_names`、`not_before`、`not_after`、`days_left`、`verified`、`verify_error`) |
| `duration_ms` | integer | かかった時間 (ミリ秒) |

Table: net_http の出力

`response_encoding` が `auto` の場合は、Content-Type がテキストの種類 (`text/*`、JSON、XML など) か、Content-Type がなく UTF-8 として正しい場合にテキストとして返し、それ以外は base64 で返します。  
Content-Type の charset が UTF-8 以外の場合は、UTF-8 に変換します。  
`insecure` で検証を省いた場合も、`tls.verified` と `tls.verify_error` には、検証した場合の結果を入れます。  
`save_to` の場合、要求に `progressToken` があれば、受信に合わせて進み具合 (受信したバイト数と、分かる場合は全体のバイト数) を送ります。

```json
{"url": "http://localhost:8080/health"}
```

```json
{"url": "http://localhost/v1.43/containers/json", "unix_socket": "/var/run/docker.sock"}
```

### net_tcp

TCP (または TLS、Unix ドメイン ソケット) で接続し、データを送って、返ってきたものを返します。HTTP 以外のプロトコルの確認に使います。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `host` | string | | ホスト名または IP アドレス。省略時は `localhost` |
| `port` | integer | ※ | ポート番号 |
| `unix_socket` | string | ※ | `host` と `port` の代わりに、この Unix ドメイン ソケットに接続する |
| `tls` | boolean | | TLS で接続する |
| `insecure` | boolean | | TLS の証明書を検証しない |
| `send` | string | | 接続したあとに送るデータ |
| `send_encoding` | string | | `text` (既定) または `base64` |
| `close_write` | boolean | | 送ったあとに送信側を閉じる (これ以上送らないことを相手に伝える) |
| `until` | string | | 受信した内容がこの正規表現に一致したら受信をやめる |
| `idle_ms` | integer | | この時間 (ミリ秒) 何も届かなければ受信をやめる。省略時は 1000 |
| `timeout_sec` | integer | | 全体のタイムアウト (秒)。省略時は 10 |
| `max_bytes` | integer | | 受信する最大のバイト数 |
| `receive_encoding` | string | | `auto` (既定。UTF-8 として正しければテキスト、そうでなければ base64)、`text`、`base64` |

Table: net_tcp の引数 (※ port と unix_socket のどちらかが必須)

出力は、`remote_addr`、`sent_bytes`、`received`、`receive_encoding`、`received_bytes`、`matched` (`until` が一致した)、`closed_by_peer`、`truncated`、`tls`、`duration_ms` です。

```json
{"port": 6379, "send": "PING\r\n", "until": "\r\n"}
```

### net_check

複数の宛先について、名前の解決、TCP の接続、TLS の証明書を並行して調べます。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `targets` | string[] | ○ | 宛先 (最大 100)。`host:port` (解決と接続)、`host` (解決だけ)、URL (そのホストとポート。`https` は TLS も調べる)。IPv6 は `[::1]:443` |
| `tls` | boolean | | すべての宛先で TLS の握手もして、証明書を返す |
| `timeout_ms` | integer | | 宛先ごとのタイムアウト (ミリ秒)。省略時は 5000 |

Table: net_check の引数

出力の `results` の各要素は、`target`、`host`、`port`、`addresses` (解決したアドレス)、`dns_ms`、`connected` (接続できたか。解決だけの場合は含めない)、`connect_ms`、`tls` ([`net_http`](#net_http) と同じ)、`error` です。

### net_listen

待ち受け中の TCP と UDP のソケットを、所有するプロセスとともに返します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `port` | integer | | このポートだけ |
| `protocol` | string | | `tcp` または `udp` |
| `process` | string | | プロセスの名前にこの文字列を含むものだけ (大文字と小文字を区別しない) |

Table: net_listen の引数

出力の `listeners` の各要素は、`protocol` (`tcp`、`tcp6`、`udp`、`udp6`)、`address`、`port`、`pid`、`process` です。

| OS | 取得の方法 |
|---|---|
| Linux | `/proc/net/{tcp,tcp6,udp,udp6}` を読み、`/proc/<pid>/fd` からプロセスを特定する。root でない場合、ほかのユーザーのプロセスは `pid` が空になる (`note` で示す) |
| Windows | `netstat -ano` と `tasklist` の出力を読む |

Table: net_listen の取得の方法

### net_interfaces

ネットワーク インターフェースの一覧を返します。引数はありません。  
出力の `interfaces` の各要素は、`name`、`index`、`mtu`、`mac`、`flags`、`addresses` (プレフィックス長付きの IP アドレス) です。

## 管理

プロセス、サービス、システムのログ、ディスクを扱うツールです。組 `admin` に属します。  
Linux と Windows の違いは、ツールの中で吸収し、同じ形の結果を返します。

### sys_processes

プロセスの一覧を、OS の標準のコマンドで取得して返します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `filter` | string | | この文字列を含む行 (大文字と小文字を区別しない) と、見出しの行だけを返す |

Table: sys_processes の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `text` | string | コマンドの出力 |
| `truncated` | boolean | 上限で切り詰めた場合に `true` |

Table: sys_processes の出力

| OS | 実行するコマンド | 形式 |
|---|---|---|
| Linux | `ps -eo pid,ppid,user,stat,etime,args` | 空白区切りの表 |
| Windows | `tasklist /fo csv` | CSV |

Table: sys_processes のコマンド

出力は、コマンドの出力と同じく自動判定で UTF-8 に変換します。

### proc_kill

プロセス ID を指定して、シグナルを送るか終了させます。`job_start` で開始したジョブには [`job_stop`](#job_stop) を使ってください。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `pid` | integer | ○ | プロセス ID |
| `signal` | string | | Linux: `terminate` (既定)、`kill`、`interrupt`、`hangup`、`quit`、`stop`、`continue`、`usr1`、`usr2`。Windows: `kill` (既定。これだけ) |
| `tree` | boolean | | 子孫のプロセスにも送る |
| `sudo` | boolean | | Linux: `sudo -n kill` で root として送る (ほかのユーザーのプロセス向け) |

Table: proc_kill の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `signaled` | integer[] | シグナルを送ったプロセス ID |
| `exited` | boolean | 3 秒以内にプロセスがなくなった場合に `true` |

Table: proc_kill の出力

Linux の `tree` は、`/proc` の親子関係から子孫をたどります。Windows の `tree` は `taskkill /T /F` です。

### svc_list

サービスの一覧を返します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `filter` | string | | 名前か説明にこの文字列を含むものだけ (大文字と小文字を区別しない) |
| `state` | string | | `running`、`stopped`、`failed`、`all` (既定) |
| `user` | boolean | | Linux: ユーザーの systemd ユニット (`systemctl --user`) |

Table: svc_list の引数

出力の `services` の各要素は、次のとおりです。最大 2000 件で、超えた場合は `truncated` を `true` にします。

| 項目 | Linux | Windows |
|---|---|---|
| `name` | ユニットの名前 (`nginx.service`) | サービスの名前 |
| `description` | ユニットの説明 | 表示名 |
| `state` | `running`、`stopped`、`failed`、`starting`、`stopping` | `running`、`stopped`、`starting`、`stopping`、`paused` |
| `sub_state` | systemd の SUB (`running`、`exited`、`dead` など) | |
| `start_mode` | `enabled`、`disabled`、`static`、`masked` など | `auto`、`delayed-auto`、`manual`、`disabled` |
| `pid` | | プロセス ID |

Table: svc_list の項目

Linux では `systemctl list-units --type=service --all` と `systemctl list-unit-files --type=service` の結果を合わせます。  
Windows では、サービス コントロール マネージャーに、一覧と状態の取得に必要な権限だけで接続します (管理者でなくても使えます)。

### svc_control

1 つのサービスの状態を返すか、操作してから状態を返します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `name` | string | ○ | サービスの名前 (Windows では表示名ではなくサービスの名前) |
| `action` | string | | `status` (既定)、`start`、`stop`、`restart`、`reload` (Linux のみ)、`enable`、`disable` |
| `user` | boolean | | Linux: ユーザーのユニット |
| `sudo` | boolean | | Linux: `sudo -n systemctl ...` で実行する (root でない場合のシステムのユニットの操作) |
| `wait_sec` | integer | | Windows: 起動、停止の完了を待つ秒数。省略時は 30 |

Table: svc_control の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `service` | object | [`svc_list`](#svc_list) と同じ形の状態 |
| `properties` | object | 詳細。Linux は `systemctl show` の主な項目 (`FragmentPath`、`MainPID`、`ExecMainStartTimestamp`、`Result` など)。Windows は `BinaryPath`、`Account`、`Description`、`Dependencies`、`ExitCode` |
| `status_text` | string | Linux: `systemctl status --lines=20` の出力 (直近のログを含む) |
| `output` | string | Linux: 操作のコマンドの出力 |

Table: svc_control の出力

Windows の `enable` は起動の種類を自動にし (自動の遅延開始はそのまま)、`disable` は無効にします。`restart` は停止の完了を待ってから起動します。  
`--read-only` では、`action` は `status` だけを使えます。

### log_query

システムのログを、構造化した項目で返します。新しい項目が最後です。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `unit` | string | | Linux: systemd のユニット。Windows: イベントのプロバイダー (ソース) の名前 |
| `log` | string | | Windows: ログの名前 (`System` (既定)、`Application`、`Security`、`Setup`、またはチャネルの名前)。Linux: `kernel` でカーネルのメッセージだけ |
| `since`、`until` | string | | この時刻より新しい (古い) 項目だけ。`30m`、`2h`、`1d` のような経過時間か、`2026-10-30 09:00`、RFC 3339 の時刻 |
| `priority` | string | | 最低の重要度。Linux: `emerg`、`alert`、`crit`、`err`、`warning`、`notice`、`info`、`debug`。Windows: `critical`、`error`、`warning`、`information`、`verbose` |
| `event_id` | integer | | Windows: このイベント ID だけ |
| `grep` | string | | メッセージがこの正規表現に一致する項目だけ (大文字と小文字を区別しない) |
| `lines` | integer | | 最大の項目数 (新しいほうから)。省略時は 100、最大 2000 |
| `user` | boolean | | Linux: ユーザーのジャーナル |
| `sudo` | boolean | | Linux: `sudo -n journalctl` で読む |

Table: log_query の引数

出力の `entries` の各要素は、`time` (RFC 3339)、`priority`、`source` (ユニットか syslog の識別子、またはプロバイダー)、`pid`、`event_id` (Windows)、`message` です。出力の上限を超える場合は、古い項目から除き、`truncated` を `true` にします。

| OS | 読み方 |
|---|---|
| Linux | `journalctl -o json`。`grep` は、多めに読んでから host-shell が絞り込む (`journalctl -g` に依存しない) |
| Windows | `wevtutil qe <log> /q:<XPath> /f:RenderedXml`。条件は XPath にし、`grep` は読んでから絞り込む |

Table: log_query の読み方

Linux で、`systemd-journal` か `adm` のグループに属さないユーザーは、自分の項目だけを読めます。  
ジャーナルのない環境では、エラーの説明で、`fs_read` や `search_grep` でログ ファイルを読むよう案内します。

### sys_disks

ファイル システム (Linux) またはドライブ (Windows) の使用量を返します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `all` | boolean | | Linux: 疑似ファイル システム (`proc`、`sysfs`、`cgroup` など) も含める |

Table: sys_disks の引数

出力の `disks` の各要素は、`mount`、`device` (Linux)、`type` (Linux はファイル システムの種類、Windows はドライブの種類とファイル システム)、`total_bytes`、`used_bytes`、`free_bytes` (現在のユーザーが使える量)、`used_percent`、`total`、`free` (読みやすい表記) です。

## Windows のレジストリ

Windows の接続先だけで公開するツールです。組 `admin` に属します。  
キーは `HKLM\SOFTWARE\...` のように、ルート (`HKLM`、`HKCU`、`HKCR`、`HKU`、`HKCC`、または `HKEY_LOCAL_MACHINE` などの正式な名前) から書きます。区切りは `\` と `/` のどちらでも構いません。  
`view` に `64` か `32` を指定すると、64 ビットの Windows で、そのビュー (WOW64) を使います。

### reg_read

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `key` | string | ○ | キー |
| `value` | string | | この値だけを読む (空文字列は既定の値)。省略すると、サブキーとすべての値を返す |
| `view` | string | | `64` または `32` |

Table: reg_read の引数

| 出力 | 型 | 内容 |
|---|---|---|
| `key` | string | キー |
| `subkeys` | string[] | サブキーの名前 |
| `values` | object[] | 各要素は `name`、`type` (`REG_SZ` など)、`data`、`expanded` |

Table: reg_read の出力

| type | data の形 |
|---|---|
| `REG_SZ`、`REG_EXPAND_SZ` | 文字列。`REG_EXPAND_SZ` は、環境変数を展開したものを `expanded` に入れる |
| `REG_MULTI_SZ` | 文字列の配列 |
| `REG_DWORD`、`REG_QWORD` | 数値 |
| その他 (`REG_BINARY` など) | base64 |

Table: レジストリの値の形

### reg_write

値を作るか書き換えます。キーがなければ作ります。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `key` | string | ○ | キー |
| `value` | string | | 値の名前 (空文字列は既定の値)。省略すると、キーを作るだけ |
| `type` | string | | `REG_SZ` (既定)、`REG_EXPAND_SZ`、`REG_MULTI_SZ`、`REG_DWORD`、`REG_QWORD`、`REG_BINARY` |
| `data` | any | | 上の表の形の値 |
| `view` | string | | `64` または `32` |

Table: reg_write の引数

出力は、`key`、`created_key` (キーを作った場合に `true`)、`value` (書いたあとの値) です。

### reg_delete

値かキーを削除します。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `key` | string | ○ | キー |
| `value` | string | | この値を削除する (空文字列は既定の値)。省略するとキーを削除する |
| `recursive` | boolean | | サブキーを含めてキーを削除する。指定しない場合、サブキーのあるキーは削除しない |
| `view` | string | | `64` または `32` |

Table: reg_delete の引数

ルートのキーそのものは削除しません。

## 手元との転送 (bridge)

`host-shell bridge` は、エージェントが動く手元のマシンで動かすサブコマンドです。  
エージェントには MCP サーバーとして振る舞い、接続先の host-shell を子プロセス (ssh の入れ子) として起動して、その間を中継します。

```plantuml
@startuml bridge の構成
    caption bridge の構成
    rectangle "手元のマシン" {
        component "エージェント" as agent
        component "host-shell bridge" as bridge
        folder "手元のファイル" as local
    }
    rectangle "接続先" {
        component "host-shell --mcp" as remote
        folder "接続先のファイル" as files
    }
    agent --> bridge : MCP (stdio)
    bridge --> remote : MCP (ssh の入れ子の stdio)
    bridge --> local : transfer_push、transfer_pull
    remote --> files
@enduml
```

### 書式

```text
host-shell bridge [options] -- COMMAND [ARGS...]
```

`COMMAND` は、接続先の host-shell を MCP サーバーとして起動するコマンドです (例: `ssh -T hop1 ssh -T target /tmp/host-shell-1000/host-shell --mcp`)。  
`COMMAND` の標準エラーは、bridge の標準エラーにそのまま出します (ssh のエラーがエージェントのログに残ります)。

| オプション | 値 | 内容 |
|---|---|---|
| `--allow-local-path` | ディレクトリ (繰り返し可) | 転送で、手元のこの場所の下だけを読み書きする |
| `--allow-local-read-path` | ディレクトリ (繰り返し可) | `transfer_push` で読むことだけを許す手元の場所 |
| `--no-transfer` | なし | 転送のツールを加えず、中継だけを行う |

Table: bridge のオプション

### 中継

- 接続先のツールとリソースを、同じ名前、説明、スキーマでエージェントに見せ、呼び出しをそのまま中継します。接続先の制限も、そのまま効きます。
- 初期化の応答の `instructions` は、接続先のものに、転送のツールの案内を加えたものです。
- 進み具合の通知 (`_meta` の `progressToken`)、要求の取り消し、ログの通知を中継します。
- 利用者の確認 (`--confirm`) の問い合わせは、エージェントにそのまま返し、エージェントが回答を付けてやり直した呼び出しを接続先に中継します。
- 接続先との接続が切れた場合は、bridge も終了します。

接続先には、クライアントの名前 `host-shell-bridge` で接続します。接続先の host-shell は、この名前のクライアントからの `fs_read` (`base64`) に限り、1 回に 1 MiB まで読みます (エージェントの文脈を通らないため)。

### transfer_push

手元のファイルかディレクトリを、接続先へ送ります。

| 引数 | 型 | 必須 | 内容 |
|---|---|---|---|
| `local_path` | string | ○ | 手元のパス (相対パスは bridge の作業ディレクトリから) |
| `remote_path` | string | ○ | 接続先のパス。送り先そのもののパス。ファイルを送る場合に、既存のディレクトリか `/` で終わるパスを指定すると、その中に同じ名前で置く |
| `overwrite` | boolean | | 内容の違う既存のファイルを置き換える。同じ内容のファイルは常に送らない |
| `exclude` | string[] | | 送らないファイルやディレクトリの名前の glob (例: `[".git", "node_modules", "*.pyc"]`)。どの階層でも一致する |
| `dry_run` | boolean | | 送らずに、送るものを示す |

Table: transfer_push の引数

1 つのファイルは、次の手順で送ります。

1. 接続先の `fs_manifest` で、既存のファイルの SHA-256 を調べる。同じなら送らない (`unchanged`)。違っていて `overwrite` がなければ送らない (`exists`)。
2. `<送り先>.host-shell-part` に、1 MiB ずつ `fs_write` (`base64`、2 回目からは `offset`) で書く。最初の書き込みで、手元の権限 (`perm`) を付ける (手元が Windows の場合を除く)。
3. `fs_manifest` で SHA-256 を照合する。
4. `fs_move` で送り先の名前に変える。

失敗した場合は、`.host-shell-part` を削除します。空のディレクトリとシンボリック リンクは送りません (リンクは `skipped`)。

| 出力 | 型 | 内容 |
|---|---|---|
| `source`、`destination` | string | 送り元と送り先 |
| `transferred` | integer | 送ったファイルの数 (`dry_run` では送るファイルの数) |
| `unchanged` | integer | 同じ内容のため送らなかったファイルの数 |
| `failed` | integer | `exists` と `error` のファイルの数 |
| `bytes` | integer | 送ったバイト数 |
| `files` | object[] | ファイルごとの `path` (送り元からの相対パス)、`bytes`、`status` (`sent`、`unchanged`、`exists`、`would-send`、`skipped`、`error`)、`error`。500 件を超える場合は `unchanged` を省き、`truncated` を `true` にする |
| `duration_ms` | integer | かかった時間 (ミリ秒) |

Table: transfer_push と transfer_pull の出力

要求に `progressToken` がある場合は、送ったバイト数と全体のバイト数を、1 秒ごとに進み具合として送ります。

### transfer_pull

接続先のファイルかディレクトリを、手元へ送ります。引数と出力は [`transfer_push`](#transfer_push) と同じです。`local_path` が送り先です。

1. 接続先の `fs_manifest` で、ファイルの一覧と SHA-256 を得る。
2. 手元に同じ内容のファイルがあれば送らない。
3. `fs_read` (`base64`) で 1 MiB ずつ読み、手元の `<送り先>.host-shell-part` に書く。
4. SHA-256 を照合し、接続先の権限を付けて (手元が Windows の場合を除く)、送り先の名前に変える。

## プロセスの起動と終了

### シェル

`command` は、`shell` で選んだシェルに次の形で渡します。

| shell | OS | 起動の形 |
|---|---|---|
| `sh` | Linux (既定) | `sh -c '<command>'` |
| `bash` | Linux | `bash -c '<command>'` |
| `cmd` | Windows | `"%ComSpec%" /d /s /c "<command>"` |
| `powershell` | Windows (既定) | `powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand <base64>` |
| `pwsh` | Windows、Linux | `pwsh -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand <base64>` |

Table: シェルの起動の形

Windows で cmd を使うには、次のどちらかを使います。

- 呼び出しごと: `exec_run` と `job_start` の引数に `"shell": "cmd"` を指定する (エージェントが選ぶ)
- サーバー全体: `host-shell` を `--windows-shell cmd` で起動し、既定のシェルを cmd にする (利用者が登録のコマンドで選ぶ)

既定が PowerShell なのは、コマンド行を `-EncodedCommand` で渡すため cmd や SSH の引用符の解釈に左右されず、出力を UTF-8 にそろえられるためです。  
その OS で使えないシェルを指定した場合はエラーになります。

PowerShell には、コマンド行を UTF-16LE の base64 にして `-EncodedCommand` で渡します。  
このため、エージェントは cmd や SSH を経由する引用符の解釈を気にせずに、PowerShell の構文だけで書けます。  
また、コマンドの前後に次の処理を加えます。

- 前: `[Console]::OutputEncoding` を UTF-8 にし、PowerShell の出力を UTF-8 にそろえる
- 後: 最後に実行した文の成否から、終了コードを決める (Linux の `sh -c` と同じ考え方。下の表)

| 最後に実行した文 | 終了コード |
|---|---|
| 成功した (`$?` が真) | 0 |
| 失敗し、ネイティブ コマンドが `$LASTEXITCODE` に 0 以外を残している | `$LASTEXITCODE` |
| 失敗し、`$LASTEXITCODE` が 0 または未設定 (コマンドレットの失敗) | 1 |
| スクリプトが `exit N` で終わった | N |
| 終了するエラー (`throw` など) で止まった | 1 |

Table: PowerShell の終了コード

PowerShell は標準入力 (`$input`) をコンソールのコード ページで読み、スクリプトの中で設定を変えても効きません。このため、Windows で PowerShell が実行する場合は、`stdin` の既定の文字コードをコンソールのコード ページ (`system`) にします。コンソールのコード ページで表せない文字 (日本語の環境での絵文字など) は、`stdin` で渡せません。`pty` のジョブは、端末が UTF-8 で受け取るため対象外です。

途中の文が失敗しても、最後の文が成功すれば終了コードは 0 です。途中で止めたい場合は、スクリプトの先頭で `$ErrorActionPreference = 'Stop'` を設定してください。  
PowerShell の出力は UTF-8 になるため、`output_encoding` の自動判定では変換しません。  
PowerShell から呼んだネイティブ コマンドの出力は、PowerShell が受け取って出力し直す場合があります。Shift_JIS を出力するコマンドが文字化けする場合は、`"shell": "cmd"` で実行してください。

cmd には、Go の引数のエスケープを通さずに、コマンド行をそのまま渡します。`/d` により、レジストリの AutoRun は実行しません。

### argv による起動

`argv` を指定した場合は、シェルを介さずにプログラムを起動します。  
プログラム名にパスの区切りがない場合は、環境変数 `PATH` から探します。  
Windows では、各引数を Windows の標準の規則 (MSVC の規則) でエスケープしてコマンド行を組み立てます。

### 環境変数と標準入力

子プロセスは `host-shell` の環境変数を引き継ぎます。SSH 経由の場合は、sshd が用意したセッションの環境変数です。  
`env` を指定すると、同じ名前の環境変数を上書きし、ない名前は追加します。Windows では、名前の大文字と小文字を区別しません。

`stdin` を省略した場合、子プロセスの標準入力は空 (Linux の `/dev/null`、Windows の `NUL`) です。  
`job_start` で `stdin_open` を指定した場合は標準入力をパイプにして開いたままにし、`pty` を指定した場合は疑似端末につなぎます。  
`--unset-env` を指定した場合は、一致する環境変数を除いてから `env` を加えます。  
子プロセスが `host-shell` 自身の標準入出力 (MCP の通信路) を受け継ぐことはありません。

### 終了のさせ方

タイムアウト、要求の取り消し、`job_stop` (`kill`)、`host-shell` の終了のときは、コマンドを子孫のプロセスも含めて強制終了します。

| OS | 方法 | 終了させられないもの |
|---|---|---|
| Linux | コマンドを新しいプロセス グループ (`pty` の場合は新しいセッション) で起動し、グループ全体に SIGKILL を送る | 自分で別のプロセス グループやセッションに移ったプロセス (`setsid`、デーモン化したプロセスなど) |
| Windows | `taskkill /T /F` でプロセス ツリーを終了させる | 親プロセスがすでに終了していて、ツリーから外れた子孫のプロセス (下記の Job Object で、`host-shell` の終了時には終了する) |

Table: 終了のさせ方

`host-shell` 自身が強制終了された場合 (SSH のセッションが強制的に切られた、`kill -9` やタスク マネージャーで止めた、異常終了した) も、コマンドは残りません。

| OS | 方法 | 残る場合があるもの |
|---|---|---|
| Linux | コマンド (直接の子) に、親の終了で SIGKILL を受け取る設定 (`PR_SET_PDEATHSIG`) を付ける | 孫のプロセスのうち、直接の子が終了しても動き続けるもの |
| Windows | 起動時に `host-shell` 自身を Job Object に入れ、最後のハンドルが閉じたら全員を終了する設定 (`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`) にする。起動したプロセスと子孫は自動的に同じ Job Object に入る | なし (Job Object から抜けることは許していない) |

Table: host-shell が終了したときのコマンドの扱い

Windows で Job Object を作れない場合 (入れ子にできない Job Object の中で起動された場合など) は、標準エラーに警告を出して、`taskkill /T` による終了だけで動きます。

セッションより長く動かしたいプロセス (開発用のサーバーなど) は、`host-shell` の子として起動しないでください。Linux では systemd のユニット (`systemd-run --user --unit=myapp ...` など)、Windows ではサービスやタスク スケジューラ (`schtasks`、`Register-ScheduledTask`) で起動します。

コマンドが終了したあとも子孫のプロセスが出力のパイプを開いたままの場合は、最大 2 秒待ってからパイプを閉じ、結果を返します。  
Windows では、コンソール ウィンドウを作らずに起動します。

## 操作ログ

`host-shell` は、すべてのツールの呼び出しを、接続先のログ ファイルに 1 行ずつ JSON で記録します (JSON Lines)。

### 保存先

| OS | 既定の元の名前 |
|---|---|
| Linux | `$XDG_STATE_HOME/host-shell/audit.log`。`XDG_STATE_HOME` がない場合は `~/.local/state/host-shell/audit.log` |
| Windows | `%LOCALAPPDATA%\host-shell\audit.log` |

Table: 操作ログの既定の元の名前

操作ログは、接続先のローカル時刻の日付ごとに、別のファイルに書きます。ファイル名は、元の名前 (既定の値、または `--log-file` の値) の拡張子の前に `-YYYY-MM-DD` を入れたものです (例: `audit-2026-10-31.log`)。拡張子がない場合は末尾に付けます。

- 当日のファイルのほかに、日付の新しい順に 5 日分を残し、それより古いファイルを消します。消すのは `<元の名前>-YYYY-MM-DD<拡張子>` の形のファイルだけです。
- 古いファイルは、`host-shell` の起動時と、日付が変わって最初に記録するときに消します。
- 1 日のファイルの大きさに上限はありません。
- 切り替えと保持の設定を変えるオプションはありません。

ディレクトリがなければ権限 `0700` で作り、ファイルは権限 `0600` で作って追記します。  
ログ ファイルを開けない場合、`host-shell` は起動に失敗します。  
1 件を記録するたびにファイルを開き、1 回の書き込みで 1 行を追記して閉じます。このため、複数のセッションの `host-shell` が同じファイルに書いても、行が混ざりません。

> [!NOTE]
> 日付ごとのファイルに分ける前の版が書いた `audit.log` (日付のない名前) は、消す対象の形に一致しないため、そのまま残ります。不要であれば手で消してください。

### 形式

```json
{"time":"2026-10-30T08:15:42.42+09:00","level":"INFO","msg":"tool_call","tool":"fs_read","args":{"path":"/nonexistent"},"duration_ms":0,"is_error":true,"error":"open /nonexistent: no such file or directory"}
```

| msg | 記録する時点 | 項目 |
|---|---|---|
| `server_start` | 起動したとき | `version`、`pid` |
| `tool_call` | ツールの呼び出しが終わったとき。制限で拒んだ場合と、利用者の確認を求めた場合 (`tool` が `<名前> (confirmation requested)`) も記録する | `tool`、`args`、`duration_ms`、`is_error`、`error` (エラーの場合) |
| `server_stop` | 終了するとき | `error` (正常な終了では `<nil>`) |

Table: 操作ログの記録

すべての行に、`time` (RFC 3339) と `level` を含めます。

### 記録しない値

`args` には、ツールの引数をそのまま記録します。ただし次の値は置き換えます。

| ツール | 引数 | 記録する値 |
|---|---|---|
| `exec_run`、`job_start`、`sudo_run` | `stdin` | `<N chars>` (N は長さ) |
| `job_write` | `input` | `<N chars>` |
| `fs_edit` | `old_string`、`new_string` (`edits` の中も) | `<N chars>` |
| `fs_write` | `content` | `<N chars, sha256 ...>` (N は長さ、続けて内容の SHA-256) |
| `net_http` | `body`、`headers` の `Authorization`、`Proxy-Authorization`、`Cookie`、`X-Api-Key` | `<N chars>` |
| `net_tcp` | `send` | `<N chars>` |

Table: 操作ログで置き換える値

`command` と `argv` は、そのまま記録します。パスワードなどの秘密の値をコマンド行に含めないでください。  
秘密の値を渡す必要がある場合は、`stdin` を使ってください。

## 制限事項

- 疑似端末は `job_start` の `pty` だけで使えます。`exec_run` は端末なしで動きます。全画面のプログラム (`vi`、`top` など) は動きますが、`job_read` は画面の状態ではなく出力の流れを返すため、内容を読み取りにくい場合があります。
- 文字コードの自動判定は、UTF-8 として正しいかどうかと BOM だけで行います。UTF-8 以外の文字コードどうし (Shift_JIS と EUC-JP など) は区別しません。接続先の既定と異なる文字コードのファイルは、`charset` で指定してください。
- まれに、UTF-8 以外の短い文字列が、UTF-8 としても正しいバイト列になることがあります。その場合は UTF-8 とみなすため、文字化けします。`output_encoding` や `charset` を明示してください。
- `net_listen`、`svc_list`、`svc_control`、`log_query`、`sys_disks` は、Linux と Windows だけに対応します。Linux のサービスとログは systemd を前提にします。
- 制限 (方針) は、ツールの引数に対する検査です。OS の権限の境界ではありません。[制限の限界](#制限の限界) を参照してください。
- bridge の転送は、ファイルを 1 つずつ順に送ります。小さいファイルがとても多い場合は、接続先で `tar` などにまとめてから送るほうが速くなります。
- 動作する OS は、Go 1.26 の要件により、Windows 10 / Windows Server 2016 以降、Linux カーネル 3.2 以降、macOS 12 以降 (bridge) です。`pty` には、Windows 10 1809 / Windows Server 2019 以降が必要です。
