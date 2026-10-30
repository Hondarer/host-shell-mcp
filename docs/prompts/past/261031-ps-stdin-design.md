<!--
生成元: agree-before-implement スキル
文書種別: 最終仕様 (design)
最終更新エージェント: claude code / セッション ID: d9875064-c62a-4b5f-9eb9-9962ce3d252f
上流文書: `261031-ps-stdin-discussion.md` (検討用 Markdown)。削除されている場合があります。
再開手順: このファイルを開いたエージェントは、作業の前に agree-before-implement スキル (/home/user/c-modernization-kit/.agents/skills/agree-before-implement) を読み込んでください。上流文書が存在する場合は、上流文書の再開手順に従って進行位置を確認し、判断の経緯を上流文書の Q&A で確認します。存在しない場合は、この文書を単体の最終仕様として扱います。最終更新エージェントのセッション情報が参照可能であれば再開時のコンテキストとして利用し、別エージェントからの引き継ぎや担当変更によりセッションが見つからない場合も異常とせず、文書の記載から継続してください。
-->

# PowerShell の標準入力を UTF-8 で受け取らせる 最終仕様

ファイル名: `docs/prompts/past/261031-ps-stdin-design.md`

## 1. 概要

Windows で、PowerShell (`powershell`、`pwsh`) が実行するコマンドに、接続先のコード ページによらず、任意の Unicode の文字を `stdin` で渡せるようにします。あわせて、PowerShell のエラーと進み具合が CLIXML (XML) で標準エラーに出る問題をなくします。  
対象は、リポジトリ host-shell-mcp の main (`1424489`) の時点の内容です。`docs/prompts/past/261031-todo-backlog.md` の BL-001 に当たります。

### 1.1. 変更の要点

| 項目 | 現在 | 変更後 |
| --- | --- | --- |
| PowerShell が使うコンソール | PowerShell ごとに、ウィンドウのないコンソール (OEM のコード ページ) | `host-shell` のコンソール (入力と出力のコード ページが 65001) を共有する |
| PowerShell の `stdin_encoding` の既定 | コンソールのコード ページ (`system`) | `utf-8` |
| PowerShell へのスクリプトの渡し方 | `-EncodedCommand <base64>` | `-Command` で、base64 のスクリプトを戻して実行する読み込み役 |
| エラーと警告 (PowerShell 5.1) | CLIXML で標準エラー | ふつうの文字。エラーは標準エラー、警告は標準出力 |
| 進み具合 (`Write-Progress` など) | CLIXML で標準エラー | 出さない |
| PowerShell 7 の色の制御文字 | 出る場合がある | 出さない |

### 1.2. 進め方

- 2 つのコミットに分けます。各コミットで、実装、テスト、関係する文書 (help、`docs/`) を合わせて変えます。
    1. PowerShell の起動の形 ([3. PowerShell の起動の形](#3-powershell-の起動の形))。
    2. `host-shell` のコンソールと標準入力 ([4. host-shell のコンソール](#4-host-shell-のコンソール)、[5. コンソールの選択と標準入力の既定](#5-コンソールの選択と標準入力の既定))。
- main に push し、Linux、Windows、macOS の CI の成功を確かめます。
- この案件ではリリースしません。
- ソース コードのコメントは英語、文書は日本語で書きます (`docs/development.md` の「設計上の約束」)。

## 2. 用語

| 用語 | 意味 |
| --- | --- |
| `host-shell` のコンソール | `host-shell` が自分で用意し、加わっているコンソール。ウィンドウはなく、入力と出力のコード ページが 65001 (UTF-8) |
| 自分のコンソール | 子プロセスを `CREATE_NO_WINDOW` で起動したときに、子プロセスごとに作られるウィンドウのないコンソール。コード ページは OEM のコード ページ (日本語の環境では 932) |
| PowerShell の起動 | `shell` が `powershell` または `pwsh` (省略して既定のシェルがこれらの場合を含む) のコマンド行の実行と、`argv` の先頭が PowerShell (ファイル名が `powershell`、`powershell.exe`、`pwsh`、`pwsh.exe`。大文字と小文字は区別しない) の実行 |

## 3. PowerShell の起動の形

対象: `internal/proc/proc.go` の `powershellArgs`。Windows と Linux の `pwsh` に共通です。

### 3.1. 起動の引数

```text
<powershell.exe | pwsh> -NoProfile -NonInteractive -ExecutionPolicy Bypass -OutputFormat Text -Command
  "$input | . ([ScriptBlock]::Create([Text.Encoding]::Unicode.GetString([Convert]::FromBase64String('<base64>'))))"
```

- `<base64>` は、[3.2. スクリプトの前後の処理](#32-スクリプトの前後の処理) を加えたスクリプトを UTF-16LE にして base64 にしたものです (今の `-EncodedCommand` に渡しているものと同じ)。
- 読み込み役は、スクリプトを戻してスクリプト ブロックにし、現在のスコープで実行します (ドット ソース)。標準入力は `$input` としてスクリプトに渡します。
- `pty` のジョブ (`interactive`) は、今と同じく `-NonInteractive` だけを除きます。

> [!NOTE]
> PowerShell 5.1 は、`-EncodedCommand` で起動すると、エラー、警告、進み具合を CLIXML で標準エラーに出力します (`-OutputFormat Text` を付けても変わりません)。`-Command` で起動すると、ふつうの文字で出力します。スクリプトは base64 のまま渡すため、cmd や SSH の引用符の解釈に左右されない利点は変わりません。

> [!NOTE]
> `-Command` では、`Write-Warning` の警告は、標準エラーではなく標準出力に、`警告: ...` (英語の環境では `WARNING: ...`) の形で出ます。PowerShell の `-Command` の動作です。

### 3.2. スクリプトの前後の処理

スクリプトの前に次の 3 行を加えます。後の処理 (終了コードを決める行) は今と同じです。

```powershell
$ProgressPreference = 'SilentlyContinue'
if ($PSStyle) { $PSStyle.OutputRendering = 'PlainText' }
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
```

| 行 | 目的 |
| --- | --- |
| `$ProgressPreference` | 進み具合の表示 (初回のモジュールの読み込み、`Invoke-WebRequest` など) を出さない |
| `$PSStyle.OutputRendering` | PowerShell 7 の色の制御文字を出さない。PowerShell 5.1 には `$PSStyle` がないため何もしない |
| `[Console]::OutputEncoding` | PowerShell の出力を UTF-8 にそろえる (今と同じ) |

### 3.3. 変わらないこと

- 終了コードの決まり方 (最後に実行した文の成否、`exit N`、終了するエラー)。
- 標準入力 (`$input`)、変数、関数、引用符を含むスクリプトの動作。
- `pty` のジョブでの `Read-Host` などの入力。

## 4. host-shell のコンソール

対象: 新規 `internal/proc/console_windows.go`、新規 `internal/proc/console_other.go`。

### 4.1. 公開する関数

```go
// UTF8Console reports whether host-shell has its own console with code pages 65001 for PowerShell
// to share. The first call prepares the console; later calls return the same result.
func UTF8Console() bool
```

- Windows 以外は、常に偽を返します。
- Windows では、最初の呼び出しで [4.2. 準備の手順](#42-準備の手順) を 1 回だけ行い (`sync.Once`)、結果を返します。PowerShell を使わない場合は、準備しません。

### 4.2. 準備の手順

1. **標準入出力の確認**: 標準入力と標準出力のどちらかがコンソール (`GetConsoleMode` が成功する) なら、何もせずに偽を返します。利用者が端末で直接起動した場合で、利用者の端末のコード ページを変えないためです。
2. **今のコンソールから離れる**: `FreeConsole` を呼びます (コンソールにつながっていない場合は何も起きません)。
3. **ウィンドウのないコンソールを作る**: 補助のプロセス `%ComSpec% /d /c "set /p x="` (`ComSpec` がなければ `cmd.exe`) を、`CREATE_NO_WINDOW` と標準入力のパイプで起動します。補助のプロセスは、標準入力を閉じるまで待ちます。
4. **コンソールに加わる**: `AttachConsole(補助のプロセスの PID)` を、成功するまで 5 ms ごとに、最大 2 秒試します。
5. **補助のプロセスを終わらせる**: 標準入力のパイプを閉じ、終了を待ちます。コンソールは、`host-shell` が加わっているため残ります。
6. **コード ページを設定する**: `SetConsoleCP(65001)` と `SetConsoleOutputCP(65001)` を呼びます。
7. すべて成功すれば真を返します。どこかで失敗した場合は偽を返します (補助のプロセスは終わらせます)。

- `host-shell` の標準入出力 (MCP の通信路) は、コンソールに加わっても変わりません。子プロセスへの標準入出力は、今と同じくパイプで渡します。
- 準備にかかる時間は、約 100 ms です (1 回の起動で 1 回)。
- 偽の場合 (準備しなかった、または失敗した) は、PowerShell を今と同じく自分のコンソールで起動し、標準入力の既定も今と同じにします ([5. コンソールの選択と標準入力の既定](#5-コンソールの選択と標準入力の既定))。

> [!IMPORTANT]
> `AllocConsole` でコンソールを作ると、既定の端末が Windows Terminal の環境 (Windows 11 の既定) では、Windows Terminal のウィンドウが開いて表示されたままになり、隠せません。`CREATE_NO_WINDOW` で起動したプロセスのコンソールはウィンドウを持たないため、それに加わる方法をとります。

> [!NOTE]
> PowerShell は、標準入力 (`$input`) を、起動したときのコンソールの入力のコード ページで読みます。スクリプトの中で `[Console]::InputEncoding` を変えても、`$input` には効きません。このため、PowerShell を起動する前に、PowerShell が使うコンソールのコード ページを 65001 にしておく必要があります。

### 4.3. 影響

- `host-shell` のコンソールを使う PowerShell と、そこから呼んだネイティブ コマンドは、入力と出力のコード ページが 65001 になります。ネイティブ コマンドの出力は UTF-8 になることが多く、`output_encoding` の自動判定はそのまま受け取ります。
- 日本語などの環境では、PowerShell とネイティブ コマンドのメッセージが英語になる場合があります。コンソールのコード ページが UI の言語を表せないとき、Windows が英語のメッセージに切り替えるためです。
- cmd、ほかのネイティブ コマンド (`argv`)、`pty` のジョブは、今と同じく自分のコンソール (ConPTY) で動き、影響を受けません。
- `textenc.Console()` (OEM のコード ページ) は変わりません。cmd の出力の変換、`sys_info` の `console_encoding` などは今と同じです。
- 子プロセスの終了 (`taskkill /T`、Job Object) は、今と同じです。

## 5. コンソールの選択と標準入力の既定

対象: `internal/proc/proc.go` (`Spec`、`IsPowerShell`)、`internal/proc/proc_windows.go` (`setupProcess`)、`internal/tools/exec.go`。

### 5.1. proc の変更

- `Spec` に次のフィールドを加えます。

```go
// SharedConsole starts the process in host-shell's console (see UTF8Console) instead of a console
// of its own. Only for PowerShell; ignored outside Windows and for pty jobs.
SharedConsole bool
```

- `setupProcess` は、`SharedConsole` が真の場合に `CREATE_NO_WINDOW` を付けません。子プロセスは `host-shell` のコンソールを受け継ぎます。偽の場合は今と同じです。
- `IsPowerShell(spec Spec, defaultShell string) bool` を加え、[2. 用語](#2-用語) の「PowerShell の起動」に当たるかを返します。

### 5.2. 決まり (Windows、`pty` 以外)

`exec_run` と `job_start` (`pty` なし) は、PowerShell の起動について、次のとおり決めます。

| 条件 | `stdin_encoding` の既定 | 使うコンソール |
| --- | --- | --- |
| `UTF8Console()` が真、`stdin_encoding` を省略 | `utf-8` | `host-shell` のコンソール |
| `UTF8Console()` が真、`stdin_encoding` が `utf-8` | (指定どおり) | `host-shell` のコンソール |
| `UTF8Console()` が真、`stdin_encoding` が `utf-8` 以外 | (指定どおり) | 自分のコンソール (コード ページは変えない) |
| `UTF8Console()` が偽 | `shell` の場合はコンソールのコード ページ (`system`)、`argv` の場合は `utf-8` (今と同じ) | 自分のコンソール (今と同じ) |

Table: PowerShell の標準入力の既定と使うコンソール

- `stdin` を渡さない場合も、`stdin_encoding` の省略として扱い、`host-shell` のコンソールを使います。
- `job_write` は、今と同じく `job_start` の `stdin_encoding` (または既定) で変換します。
- `pty` のジョブ、PowerShell 以外のコマンド、Windows 以外の OS は、今と同じです (`stdin_encoding` の既定は `utf-8`)。
- `stdin` を変換できない場合のエラーの案内 (`set stdin_encoding to an encoding the command reads, e.g. utf-8`) は変えません。

> [!NOTE]
> `argv` で PowerShell を起動する場合も、使うコンソールだけを変え、引数には手を加えません。スクリプトの渡し方 ([3. PowerShell の起動の形](#3-powershell-の起動の形)) は、`shell` の場合だけに当たります。

## 6. テスト

対象: `internal/server/powershell_test.go`、`internal/proc` の単体テスト。PowerShell のテストは Windows で動かし、`pwsh` があれば `pwsh` でも動かします。

| テスト | 確かめること |
| --- | --- |
| `TestPowerShellStdin` (変更) | `日本語 😀 Ελληνικά` が、`stdin_encoding` の省略と `utf-8` の明示のどちらでも、そのまま届く。コンソールのコード ページによる飛ばしをなくし、`UTF8Console()` が偽の場合 (テストを端末で直接動かした場合) だけ飛ばす |
| `TestPowerShellStdinJob` (追加) | `job_start` の `stdin_open` と `job_write` で、同じ文字列が届く |
| `TestPowerShellArgvStdin` (追加) | `argv` で起動した PowerShell (`-Command "$input"` など) に、同じ文字列が届く |
| `TestPowerShellPlainErrors` (追加) | エラー (`Get-Item` の存在しないパス、`throw`) が標準エラーにふつうの文字で出て、`CLIXML` を含まない。`Write-Progress` が何も出さない |
| `TestPowerShellExitCode` (既存) | 終了コードの決まり方が変わらない |
| `TestPowerShellCmdConsole` (追加) | `"shell": "cmd"` の `chcp` が、OEM のコード ページを示す (`host-shell` のコンソールの影響を受けない) |
| `TestPowerShellPTY` (追加) | `pty` のジョブの PowerShell で、`Read-Host` が入力を受け取る |
| `internal/proc` の単体テスト (追加) | `IsPowerShell` の判定 (`shell`、既定のシェル、`argv` のファイル名とパス、大文字と小文字)。標準入出力がコンソールの場合に準備しない判定 |

あわせて、この PC で、`host-shell` がコンソールを準備してもウィンドウが開かないこと (`GetConsoleWindow` が 0、Windows Terminal が起動されない) を確かめます。

## 7. 受入条件

- Windows の CI (コード ページ 437) とこの PC (コード ページ 932) で、PowerShell 5.1 と 7 に `stdin` で渡した `日本語 😀 Ελληνικά` が、そのまま届く (テストを飛ばさない)。`stdin_encoding` に `utf-8` を明示した場合、`job_start` の `stdin_open` と `job_write`、`argv` の場合も同じ。
- PowerShell 5.1 と 7 のエラーと進み具合が、CLIXML ではなく、ふつうの文字で出る (進み具合は出ない)。
- 終了コードの決まり方が今と同じ (`TestPowerShellExitCode` の全件が成功する)。
- cmd とネイティブ コマンドは、今と同じく自分のコンソール (OEM のコード ページ) で動く。
- `host-shell` がコンソールを準備しても、ウィンドウが開かない。
- 標準入出力がコンソールの場合は、コンソールのコード ページを変えず、今と同じ扱いになる。
- `pty` のジョブの PowerShell で、`Read-Host` が動く。
- Linux、Windows、macOS の CI がすべて成功する。gofmt と go vet (3 つの OS 向け) に指摘がない。

## 8. 正本への反映

| 反映先 | 内容 |
| --- | --- |
| `docs/reference.md` の「シェル」の節 | 起動の形の表 (`-Command` と読み込み役)、前の処理 (3 行)、`host-shell` のコンソール (65001、ウィンドウなし、端末で直接起動した場合は使わない)、メッセージが英語になる場合があること、`Write-Warning` が標準出力に出ること、PowerShell の標準入力の説明の書き直し |
| `docs/reference.md` の `exec_run` の引数と文字コードの表 | `stdin_encoding` の既定を `utf-8` に。`utf-8` 以外を明示した場合は PowerShell が自分のコンソールで動くこと |
| `internal/tools/help/encoding.md` | `stdin_encoding` の既定 (`utf-8`) と PowerShell の扱い |
| `internal/tools/help/windows.md` | スクリプトの渡し方、メッセージが英語になる場合があること、警告が標準出力に出ること |
| コードのコメント (`console_windows.go`、`powershellArgs`、`setupProcess`、`stdinDefault`) | `$input` がコンソールの入力のコード ページで読まれること、`AllocConsole` を使わない理由、`-EncodedCommand` を使わない理由 |
| `docs/prompts/past/261031-todo-backlog.md` | BL-001 の状態を「対応済み」にする |
