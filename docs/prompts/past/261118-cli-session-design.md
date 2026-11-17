<!--
生成元: agree-before-implement スキル
文書種別: 最終仕様 (design)
最終更新日時: 2026-11-18 02:25:00 JST
最終更新エージェント: claude code / セッション ID: session_01KJc53SwZ5UXzt7kJWHJ48p
稼働 OS: Linux (WSL)
上流文書: 261118-cli-session-discussion.md (検討用 Markdown、合意版: 版 3)。削除されている場合があります。
下流文書: 261118-cli-session-backlog.md (残課題一覧)、261118-cli-session-validation.md (検証記録)。未作成の場合や、削除されている場合があります。
再開手順: /home/user/c-modernization-kit/app/general/.agents/skills/agree-before-implement/SKILL.md を読み込んでください。上流文書の作業 WBS の `← **CURRENT**` の項目から再開します。
-->

# MCP を登録できないエージェントからの CLI による利用 最終仕様

版: 1

## 目的と前提

MCP サーバーを登録できないエージェントが、シェルのコマンドだけで `host-shell` の機能を使えるようにします。

- 対象は、シェルのコマンドを実行できるエージェントです。
- エージェントとの接点だけを CLI にします。`host-shell` の内部 (CLI とセッションの間、セッションと接続先の間) は MCP の通信のままです。接続先の `host-shell --mcp` は、この用途のための変更を必要としません。
- 利用の前に、組織のポリシーがシェル経由の利用を許すことを利用者が確認します。この前提を文書の冒頭と制限、およびスキルに記載します。

## 構成

```text
agent --シェル--> host-shell call TOOL ...        (呼び出しごとのプロセス)
                      | Unix ドメイン ソケット (MCP)
                 host-shell session start ...      (手元で常駐。bridge と同じ中継)
                      | 標準入出力 (MCP と多重化チャネル。1 本の接続)
                 ssh -T ... host-shell --mcp       (接続先)
```

手元で常駐して接続先との接続を保持するプロセスを「セッション」と呼びます。セッションは `bridge` と同じ中継を行い、接続先のツール、`bridge` のツール (`transfer_push`、`transfer_pull`、`forward_*`、`proxy_*`)、ポート転送、クライアント経由 HTTP プロキシ、名前解決の中継を提供します。`bridge` の動作は変えません。

セッションが終了すると接続先との接続が閉じ、接続先はこれまでと同じく、開始したコマンドとジョブ、ポート転送、プロキシを片付けます。

## コマンド

### host-shell session start

```text
host-shell session start [--name NAME] [--detach] [--idle-timeout DURATION] [bridge のオプション] -- COMMAND [ARGS...]
```

- `COMMAND` は接続先の `host-shell --mcp` を起動するコマンドで、`bridge` と同じです。`bridge` のオプション (`--proxy`、`--forward-*`、`--allow-local-path`、`--remote-dns`、`--no-transfer` など) をすべて受け付けます。`host-shell ssh-pass` で保存したパスワードも `bridge` と同じく `ssh` に渡します。
- `--name NAME` はセッションの名前です。省略時は `default` です。使える文字は英数字、`.`、`_`、`-` で、長さは 1〜32 文字です。同じ名前のセッションが動作中の場合はエラーにします。
- 既定では前面で動作し、診断ログを標準エラーに出力します。接続先に接続してソケットを開いた時点で、準備ができたことを標準エラーに 1 行出力します。Ctrl+C または SIGTERM で終了します。
- `--detach` を指定すると、端末から切り離したプロセスとしてセッションを起動します。接続先への接続に成功してソケットが使えるようになるまで待ち、セッションの状態 (`session status` と同じ JSON) を標準出力に出力して終了コード 0 で戻ります。接続に失敗した場合は、セッションのログの内容を標準エラーに出力して 0 以外で戻ります。切り離したセッションの診断ログは、ソケットと同じディレクトリの `NAME.log` に書きます。
- `--idle-timeout DURATION` (例: `30m`) を指定すると、CLI からの接続がない状態がその時間続いたときにセッションを終了します。省略時は自動終了しません。ポート転送やプロキシの通信は、この判定に含めません。

> [!NOTE]
> 切り離したセッションには端末がないため、パスワードや多要素認証の対話的な入力はできません。鍵認証、または `host-shell ssh-pass` で保存したパスワードを使います。対話的な認証が必要な経路では、利用者が端末で前面起動します。

### host-shell session status、stop、list

```text
host-shell session status [--name NAME]
host-shell session stop [--name NAME]
host-shell session list
```

- `status` は、セッションの状態を JSON で標準出力に出力します。内容は、名前、プロセス ID、開始時刻、前面か切り離しか、`host-shell` の版、接続先の版、接続先を起動したコマンドです。
- `stop` は、セッションに終了を要求し、終了するまで待ちます。
- `list` は、動作中のセッションを 1 行に 1 つ、JSON で出力します。
- `--name` を省略した場合は、環境変数 `HOST_SHELL_SESSION`、それもなければ `default` を使います。

### host-shell call

```text
host-shell call TOOL [--session NAME] [--json FILE] [--quiet] [KEY=VALUE ...]
```

- `TOOL` は必ず `call` の直後に置きます。エージェントの許可設定で、`host-shell call fs_read` のような前方一致によりツールごとに許可を分けられるようにするためです。
- セッションは、`--session NAME`、環境変数 `HOST_SHELL_SESSION`、`default` の順で選びます。
- 引数は次の形で渡します。同じキーを複数の形で指定した場合は、`KEY=VALUE` を優先します。

| 形 | 意味 |
| --- | --- |
| `KEY=VALUE` | ツールの入力スキーマの型に合わせて変換する。文字列はそのまま、数値と真偽値はその型へ、配列とオブジェクトは JSON として解釈する。型が文字列の配列であるキーは、繰り返し指定すると要素を追加する |
| `KEY=@FILE` | 手元のファイルの内容を値にする。型が文字列の場合は内容をそのまま、それ以外は JSON として解釈する。`@` で始まる文字列そのものを渡す場合は `@@` と書く |
| `--json FILE` | 引数全体の JSON オブジェクトをファイルから読む。`-` は標準入力 |

Table: 引数の渡し方

- 入力スキーマにないキー、型に変換できない値は、ツールを呼ばずにエラーにし、使えるキーを標準エラーに示します。
- 結果は、ツールが返した JSON を 1 行で標準出力に出力します。MCP 登録時にエージェントが受け取る内容と同じです。
- 進捗の通知は、標準エラーに 1 行ずつ出力します。`--quiet` で抑止します。
- 割り込み (Ctrl+C、SIGTERM) を受けると、接続先へキャンセルを伝えて終了します。CLI のプロセスが強制終了された場合も、セッションが接続の切断を検知して同じくキャンセルを伝えます。
- 複数の `call` を同時に実行できます。進捗は、要求した CLI にだけ届きます。

| 終了コード | 意味 |
| --- | --- |
| 0 | 成功 |
| 1 | ツールがエラーを返した (エラーの内容を標準エラーに出力) |
| 2 | 使い方の誤り (未知のツール、引数の誤りを含む) |
| 3 | セッションに接続できない、または呼び出しの途中でセッションか接続先との接続が切れた |

Table: host-shell call の終了コード

### host-shell tools

```text
host-shell tools [--session NAME] [--schema] [--instructions] [TOOL ...]
```

- 既定では、ツール名と説明の最初の文を 1 行に 1 つ出力します。
- `--schema` は、指定したツール (省略時はすべて) の説明の全文と入力スキーマを JSON で出力します。
- `--instructions` は、MCP 登録時にエージェントへ渡る利用の手引き (接続先の情報、`bridge` の機能の案内) を出力します。
- 利用の詳しい説明は、MCP 登録時と同じく `host-shell call help_get` で取得します。

## セッションの動作

### ソケットと保存場所

- CLI とセッションの間は、すべての OS で Unix ドメイン ソケットを使い、その上で MCP を話します。
- セッションのディレクトリは次のとおりです。環境変数 `HOST_SHELL_SESSION_DIR` で変更できます。

| OS | ディレクトリ |
| --- | --- |
| Linux、macOS | `$XDG_RUNTIME_DIR/host-shell/sessions`。`XDG_RUNTIME_DIR` がない場合は、一時ディレクトリの `host-shell-<UID>/sessions` |
| Windows | `%LOCALAPPDATA%\host-shell\sessions` |

Table: セッションのディレクトリ

- ディレクトリは本人だけが使える権限 (Linux・macOS は 0700、Windows は本人だけの保護された DACL) で作ります。Linux・macOS では、既存のディレクトリの所有者が本人でない場合、または他の利用者に権限がある場合は、使わずにエラーにします。
- ディレクトリには、ソケット `NAME.sock`、状態 `NAME.json`、切り離したセッションのログ `NAME.log` を置きます。セッションは終了時にソケットと状態を削除します。異常終了で残ったソケットは、次の `session start` が接続できないことを確かめてから削除します。

> [!NOTE]
> ソケットに接続できるのは本人のプロセスだけです。本人のプロセスであれば誰でもツールを呼べます。これは、本人が `ssh` で接続先を操作できることと同じ権限です。

### 確認が必要なツール

接続先の `--confirm` で利用者の確認が必要なツールは、次のように扱います。

- セッションを前面で起動し、標準入力と標準エラーが端末である場合は、その端末に確認の文面 (ツール名と引数) を表示し、利用者の入力を待ちます。承認された場合だけツールを実行します。既定の答えは拒否です。複数の確認は 1 つずつ順に尋ねます。呼び出しがキャンセルされた場合は、その確認を取り下げます。
- 切り離したセッション、または端末のないセッションでは、ツールを実行せずにエラーを返します。エラーの文面で、セッションを前面で起動する必要があることを示します。
- 確認の答えは、セッションだけが接続先へ渡します。CLI やソケットの側から渡された確認の答えは、接続先へ中継しません。

> [!IMPORTANT]
> CLI を実行するのはエージェント自身であるため、CLI の側では確認を尋ねません。確認に答えるのが利用者であることを保つための仕様です。

### 終了

セッションは、次のいずれかで終了します。`session stop`、前面での Ctrl+C、SIGTERM、`--idle-timeout` の経過、接続先との接続の切断。接続先との接続が切れて終了した場合の終了コードは 0 以外です。

## 入力の終端後の応答 (host-shell --mcp)

`host-shell --mcp` に、起動オプション `--finish-on-eof` を追加します。

- `--finish-on-eof` を指定した場合、標準入力が終端に達しても、受信済みの要求の応答をすべて書き終えてから終了します。待つ時間の上限は、各ツールのタイムアウトです。
- 指定しない場合の動作は、これまでと同じです。標準入力の終端で、実行中の要求をキャンセルして終了します。
- どちらの場合も、応答を書いていない要求を残して終了するときは、その件数を標準エラーに出力し、終了コードを 0 以外にします。

> [!NOTE]
> `host-shell` は標準入力の終端を SSH の切断として扱い、実行中のコマンドとジョブを止めます。この約束を既定で保つため、応答を待つ動作は起動オプションを付けたときだけにします。

`host-shell-connect` の `probe.sh` は、`--finish-on-eof` を付けて起動し、応答を待つための固定の待ち時間 (`PROBE_WAIT`) をなくします。

CLI からの利用のための単発実行のコマンドや手順は、成果物に含めません。

## 成果物

### コード

| 対象 | 内容 |
| --- | --- |
| `cmd/host-shell/main.go` | `session`、`call`、`tools` の振り分け、`--finish-on-eof`、使い方の表示 |
| `internal/bridge/` | 中継の共用。エージェント側の口を、標準入出力 1 本と、ソケットで受ける複数の接続の両方で使えるようにする。確認の扱いと進捗の届け先を、口ごとに切り替える |
| `internal/session/` (新規) | セッションの起動、切り離し、ソケットと保存場所、状態、停止、一覧、自動終了、端末での確認 |
| `internal/cli/` (新規) | `call` と `tools`。引数の変換、出力、終了コード、キャンセル |
| `internal/channel/` | 入力の終端後の応答の扱い |
| `.agents/skills/host-shell-connect/scripts/probe.sh` | `--finish-on-eof` の使用 |

Table: コードの成果物

パッケージの分け方は、実装の過程で既存の設計上の約束 (`internal/bridge` は `internal/tools` を import しない、OS の差は `_unix.go` と `_windows.go` に分ける、bool 引数で動作を切り替えない) に合わせて調整します。

### 文書

| 対象 | 内容 |
| --- | --- |
| `docs/ja/cli-without-mcp.md`、`docs/en/cli-without-mcp.md` (新規) | ユース ケース、利用の前提 (組織のポリシー)、構成、手順 (セッションの開始、ツールの一覧、呼び出し、終了)、引数と出力、確認が必要なツール、エージェントの許可設定の例、トラブルシューティング、検証済みの範囲、制限 |
| `docs/ja/reference.md`、`docs/en/reference.md` | `session`、`call`、`tools` のコマンドとオプション、`--finish-on-eof` |
| `docs/ja/user-guide.md`、`docs/en/user-guide.md` | エージェントへの登録の節から、MCP を登録できない場合の文書への案内 |
| `docs/ja/development.md`、`docs/en/development.md` | ソース構成、設計上の約束の更新 (標準出力は、`--mcp` と `bridge` では MCP の通信専用、`call`・`tools`・`session` では結果の出力に使う) |
| `README.md`、`docs/README.md` | 対応する構成、特長、スキルの表、文書の表への追加 (英語と日本語) |

Table: 文書の成果物

文書は最終仕様として書き、日本語版を正本として同じ変更で英語版へ反映します。文書から `docs/prompts/` へのリンクは設けません。

### スキル

| 対象 | 内容 |
| --- | --- |
| `.agents/skills/host-shell-cli/SKILL.md` (新規) | 利用の前提 (組織のポリシーの確認)、セッションの開始 (前面と `--detach` の使い分け)、`tools` と `help_get` での使い方の把握、`call` での操作 (引数の渡し方、長い処理は `job_start` と `job_read` を使う、終了コード)、確認が必要なツールの扱い、作業の終わりの `session stop`。手元の `host-shell` は `host-shell-connect` の `bin/` のものを使う |
| `.agents/skills/host-shell-connect/SKILL.md` | 名前と範囲は変えない。最後の段階を「MCP で登録する」と「CLI で利用する (`host-shell-cli` へ進む)」の分岐にする。スキルの説明 (frontmatter) に CLI での利用の準備を含める |
| `host-shell-code-server`、`host-shell-vscode-remote-ssh`、`host-shell-coding-agents` の `SKILL.md` | 冒頭に、ツール名の読み替え (CLI では `host-shell call TOOL`、詳細は `host-shell-cli`) を加える。手順の本文は変えない |

Table: スキルの成果物

## 検証と受入条件

### 自動テスト

CI (Linux、Windows、macOS) で次を確認します。

- 引数の変換 (型ごとの変換、配列の繰り返し、`@FILE`、`@@`、`--json`、未知のキー、変換できない値)
- セッションの開始、`call`、`tools`、`status`、`list`、`stop`、同名のセッションの拒否、残ったソケットの片付け
- 複数の `call` の並行と、進捗の届け先
- CLI の割り込みと強制終了によるキャンセル
- 確認が必要なツール (承認、拒否、端末なしでの拒否、ソケットの側から渡した確認の答えを中継しないこと)
- `--idle-timeout`
- セッションのディレクトリの権限と、条件を満たさないディレクトリの拒否
- `--finish-on-eof` の有無による動作と終了コード
- 終了コード

### Linux (WSL) の実機

接続先は、この機械の Podman の Linux コンテナーです。経路は `ssh -T` と `podman exec -i` の両方を使います。

1. エージェントからの操作: `claude`、`copilot`、`codex`、`agy`、`grok` を、`host-shell` を MCP サーバーとして登録していない状態で、非対話で実行します。各エージェントに `host-shell-cli` スキルを読ませ、シェルだけで、セッションの開始 (`--detach`)、ツールの一覧の取得、コマンドの実行、ファイルの書き込みと読み取り、ジョブの開始と読み取り、ファイル転送、セッションの終了を行えることを確認します。実行できなかったエージェントは、理由を検証記録に残し、文書に未検証と記載します。
2. 利用者の確認: 前面で起動したセッションの端末での承認と拒否、切り離したセッションでの拒否を確認します。
3. CLI 経由での 3 つのスキルの実行: MCP を使わない Claude Code が、各スキルの手順を `host-shell call` で実行します。

| スキル | 確認する内容 | 人の操作が必要なため置き換える確認 |
| --- | --- | --- |
| `host-shell-code-server` | 取得、転送、導入、起動、`forward_open`、停止と削除 | ブラウザーでの画面表示は、転送したポートへの HTTP 要求で code-server の応答を確認することに置き換える |
| `host-shell-vscode-remote-ssh` | 取得、配置、鍵の登録、`sshd` の起動、`forward_open`、片付け | 手元の VS Code からの接続は、転送したポートへの `ssh` での接続と `status.sh` の確認に置き換える。VS Code の版に合う VS Code Server を取得できない場合は、理由を記録して未検証とする |
| `host-shell-coding-agents` | CLI の配置、`--version`、クライアント経由 HTTP プロキシと名前解決の中継を通した到達の確認 | 対話的な認証が必要な確認は行わず、未検証とする |

Table: CLI 経由での 3 つのスキルの検証

置き換えた確認は、文書の「検証済みの範囲」に、確認した方法と未検証の事項として記載します。

### Windows の実機

Windows 側のセッション `host-shell-mcp-5b` に依頼します。作業ブランチを origin へ push して渡し、Windows 側でビルドして確認します。

- セッションの前面起動と `--detach`、`call`、`tools`、`status`、`list`、`stop`
- Unix ドメイン ソケットの動作と、セッションのディレクトリの権限
- 前面で起動したセッションの端末での確認
- Windows を接続先とした `--finish-on-eof`

接続先は、Windows 側のセッションが到達できる機械とします。到達できる接続先がない場合は、同じ機械の `host-shell.exe --mcp` を `COMMAND` にして確認し、その旨を記録します。

Windows で Unix ドメイン ソケットが動作しない場合は、作業を中断して利用者に確認します。

### 受入条件

- 自動テストが CI の Linux、Windows、macOS で通ること。
- Linux (WSL) の実機と Windows の実機の検証を終え、結果を `261118-cli-session-validation.md` に記録していること。
- 文書とスキルが「成果物」のとおり作成・更新され、日本語版と英語版の内容が一致していること。
- 検証していない事項 (macOS の実機、実行できなかったエージェント、置き換えた確認) を、文書の「検証済みの範囲」に未検証として記載していること。

## 作業の進め方

- Go のツールチェーン (1.26 以降) を `/tmp/go` に再導入します。
- 作業ブランチ `feature/cli-session` を作成し、このブランチにコミットします。Windows の実機での検証と CI のため、作業ブランチを origin へ push し、プル リクエストを作成します。`main` への取り込みは、成果物の承認の後、利用者の指示で行います。
- 記録 (検討、最終仕様、残課題、検証) は `docs/prompts/current/` に置き、案件が終わって残課題がない場合に `docs/prompts/past/` へ移します。

## 知見の反映計画

| 知見 | 反映先 |
| --- | --- |
| 確認の答えはセッションだけが接続先へ渡し、ソケットの側から渡された答えを中継しない理由 | `internal/bridge/` または `internal/session/` のコード コメント、`docs/ja/cli-without-mcp.md` の「確認が必要なツール」、`docs/ja/operations.md` の `--confirm` の説明 |
| `TOOL` を `call` の直後に置く理由 (エージェントの許可設定での前方一致) | `internal/cli/` のコード コメント、`docs/ja/cli-without-mcp.md` の許可設定の例 |
| 標準入力の終端で SDK が実行中の要求をキャンセルすることと、既定でそれを保つ理由 | `internal/channel/` のコード コメント、`docs/ja/reference.md` の `--finish-on-eof` |
| セッションのディレクトリの所有者と権限を確かめる理由 (一時ディレクトリを使う場合の先回りの作成への対処) | `internal/session/` のコード コメント |
| 標準出力の使い分け (`--mcp` と `bridge` は MCP の通信専用、`call` などは結果の出力) | `docs/ja/development.md` の設計上の約束 |

Table: 知見の反映計画

## 対象外と制限

- シェルを使えず、ファイルの読み書きだけができるエージェントは対象外です。
- 接続先での単発実行のコマンド (`host-shell call` を接続先で直接実行する形) は設けません。
- `exec_run` 専用の短縮コマンドは設けません。
- MCP の通信そのものを禁じる環境は対象外です (`host-shell` の内部に MCP の通信が残ります)。
- macOS の実機は検証しません。
- `--confirm` は、`host-shell` を通る操作に対する確認です。シェルを使えるエージェントが `ssh` で接続先を直接操作することは、この仕組みでは防げません。これは MCP 登録時も同じです。
