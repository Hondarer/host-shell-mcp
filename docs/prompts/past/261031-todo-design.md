<!--
生成元: agree-before-implement スキル
文書種別: 最終仕様 (design)
最終更新エージェント: claude code / セッション ID: d9875064-c62a-4b5f-9eb9-9962ce3d252f
上流文書: `261031-todo-discussion.md` (検討用 Markdown)。削除されている場合があります。
再開手順: このファイルを開いたエージェントは、作業の前に agree-before-implement スキル (/home/user/c-modernization-kit/.agents/skills/agree-before-implement) を読み込んでください。上流文書が存在する場合は、上流文書の再開手順に従って進行位置を確認し、判断の経緯を上流文書の Q&A で確認します。存在しない場合は、この文書を単体の最終仕様として扱います。最終更新エージェントのセッション情報が参照可能であれば再開時のコンテキストとして利用し、別エージェントからの引き継ぎや担当変更によりセッションが見つからない場合も異常とせず、文書の記載から継続してください。
-->

# host-shell の TODO と制限事項の対策 最終仕様

ファイル名: `docs/prompts/past/261031-todo-design.md`

## 1. 概要

host-shell-mcp のソース コードにある TODO の注記 8 件 (T1 から T8) と、`docs/reference.md` の「制限事項」13 項目 (L1 から L13) について、対策を定めます。  
対象は、リポジトリ host-shell-mcp の main (`17c8fd1`) の時点の内容です。

### 1.1. 進め方

- 項目ごとに 1 つのコミットにします。コミットの順は、[7. 作業の順序](#7-作業の順序) に従います。
- 各コミットで、実装、テスト、関係する文書 (help、`docs/`) を合わせて変えます。
- すべての項目を終えた後、CI の成功を確かめてから 1 回リリースします。版の名前は、リリースの前に確認します。
- ソース コードのコメントは英語、文書は日本語で書きます (`docs/development.md` の「設計上の約束」)。

### 1.2. 対策の一覧

| 項目 | 内容 | 対策 | 節 |
| --- | --- | --- | --- |
| T1 | Windows の既定のシェル | 既定は PowerShell のまま。TODO を消し、cmd の選び方を文書で示す | [3.1](#31-t1-windows-の既定のシェル) |
| T2、L12 | 操作ログの保存と保持 | 日ごとのファイルと 5 世代の保持を組み込む | [3.2](#32-t2-操作ログの日ごとの切り替え) |
| T3 | PowerShell の終了コード | 最後に実行した文の成否で決める | [3.3](#33-t3-powershell-の終了コード) |
| T4 | cmd の出力の文字コード | 対応済みとして TODO を消す | [3.4](#34-t4-cmd-の出力の文字コード) |
| T5 | host-shell の終了時の子孫プロセス | Windows は Job Object、Linux は Pdeathsig | [3.5](#35-t5-host-shell-の終了時の子孫プロセス) |
| T6、L4 | パスワードが必要な sudo | `sudo_run` に `password` を加え、`sudo -A` で渡す | [3.6](#36-t6-sudo_run-のパスワード) |
| T7 | プロセスの一覧 | 構造化した一覧を自前で作る | [3.7](#37-t7-sys_processes-の構造化) |
| T8 | 上書きの原子性 | `fs_write` と `fs_edit` に `atomic` を加える | [3.8](#38-t8-fs_write-と-fs_edit-の原子的な置き換え) |
| L1 | 全画面のプログラムの読み取り | 仮想端末で画面の状態を返す | [4.1](#41-l1-job_read-の画面の状態) |
| L2 | `.gitignore` | 検索で既定で従う | [4.2](#42-l2-検索での-gitignore) |
| L3 | 複数の行にまたがる検索 | `search_grep` に `multiline` を加える | [4.3](#43-l3-search_grep-の-multiline) |
| L7 | BOM のない UTF-16 | NUL の並びによる判定を共通化する | [4.4](#44-l7-bom-のない-utf-16-の判定) |
| L8 | PowerShell の標準入力の文字コード | UTF-8 にそろえる | [4.5](#45-l8-powershell-の標準入力の文字コード) |
| L11 | bridge の転送 | 複数のファイルを同時に送る | [4.6](#46-l11-bridge-の並列の転送) |
| L5、L6、L9、L10、L13 | 文字コードの推定、対応 OS、制限の境界、OS の要件 | 仕様として容認し、制限事項に残す | [5](#5-仕様として容認する制限事項) |

Table: 対策の一覧

## 2. 共通の方針

- **既定の動作を壊さない。** 新しい指定 (`atomic`、`multiline`、`screen`、`parallel`、`gitignore` など) は、既存の呼び出しの結果を変えない既定値を持たせます。ただし、`.gitignore` に既定で従うこと (L2)、PowerShell の終了コード (T3)、子孫プロセスの終了 (T5)、操作ログの書き方 (T2)、`sys_processes` の結果の形 (T7) は、意図して既定の動作を変えます。リリース ノートに変更点として書きます。
- **エージェントが MCP の情報だけで使えるようにする。** 新しい引数と変わった動作は、ツールの説明、引数の説明、`help` の該当する topic に書きます。
- **OS の差はツールの内側で吸収する。** OS ごとの処理は、`_linux.go`、`_unix.go`、`_windows.go`、`_other.go` のファイルに分けます。

## 3. TODO の対策

### 3.1. T1: Windows の既定のシェル

**仕様**

- `--windows-shell` の既定は `powershell` のままとします。
- `internal/config/config.go` の TODO を消し、既定を PowerShell にしている理由をコメントに書きます: コマンド行を `-EncodedCommand` で渡すため、cmd や SSH の引用符の解釈に左右されないこと、出力を UTF-8 にそろえられること。
- cmd を選ぶ手段は、既存の 2 つとします。追加のインターフェースは設けません。
    - 起動オプション `--windows-shell cmd`: 接続先の既定のシェルを cmd にする。
    - `exec_run` と `job_start` の引数 `"shell": "cmd"`: 呼び出しごとに cmd を選ぶ。

**文書**

- `internal/tools/help/windows.md`: 2 つの選び方を並べて書きます。起動オプションは利用者が登録のコマンドで設定するもの、`shell` はエージェントが呼び出しごとに選ぶものであることを示します。
- `docs/reference.md` の「シェル」、`docs/user-guide.md` の「Windows の接続先」: 同じ内容をそろえます。

**検証**

- 既存のテストが通ること。

### 3.2. T2: 操作ログの日ごとの切り替え

**仕様**

| 項目 | 仕様 |
| --- | --- |
| 切り替えの単位 | 接続先のローカル時刻の日付。1 日 1 ファイル |
| ファイル名 | `--log-file` の値 (既定は `~/.local/state/host-shell/audit.log`、`%LOCALAPPDATA%\host-shell\audit.log`) の拡張子の前に `-YYYY-MM-DD` を入れたもの。例: `audit-2026-10-31.log`。拡張子がない場合は末尾に付ける |
| 保持 | 当日のファイルのほかに、日付の新しい順に 5 ファイルを残す。それより古い、同じ形の名前のファイルを消す |
| 大きさの上限 | 設けない |
| 設定 | 切り替えと保持を変える起動オプションは設けない。`--log-file` (元の名前) と `--no-log` は現状のまま |

Table: 操作ログの保存の仕様

- 1 件を書くたびに、その時点の日付のファイルを `O_APPEND|O_CREATE` (権限 0600) で開き、1 回の書き込みで 1 行を書いて閉じます。ファイルを開いたままにしません。
- 古いファイルの削除は、host-shell の起動時と、そのプロセスが前回書いたときから日付が変わったときに行います。削除の対象は、`<元の名前>-YYYY-MM-DD<拡張子>` の形に一致し、日付として正しいものだけです。
- 複数の host-shell (SSH のセッションごとに 1 つ) が同じディレクトリに書いても、名前を変える処理がないため競合しません。削除で、すでにほかのプロセスが消したファイルや、Windows でほかのプロセスが一瞬開いているファイルに当たった場合は、エラーにせず、次の機会に再度試します。
- 時刻を差し替えられるよう、`audit.Logger` は内部で現在時刻を返す関数を持ちます (テスト用)。

> [!NOTE]
> 以前の版が書いた `audit.log` (日付のない名前) は、削除の対象の形に一致しないため、そのまま残ります。リリース ノートに、不要なら手で消すよう書きます。

**文書**

- `docs/operations.md` の「操作ログの管理」: 「保存先」に日付を付けた名前を、「ローテーション」を「日ごとの切り替えと保持」に改め、logrotate の例 (`copytruncate`) を削除します。転送と監視の例は、`audit-*.log` を対象にする形に直します。
- `docs/reference.md`: 起動オプションの `--log-file` の説明 (日付を入れる元の名前)、「操作ログ」の「保存先」、制限事項の「操作ログのローテーションは行いません」を削除します。
- skill (`SKILL.md`) の「取り除く」: ログのファイル名の例を直します。

**検証**

- 時刻を差し替えたテストで、日付ごとに別のファイルに書くこと、7 日分を書いた後に当日と 5 世代だけが残ること、日付の形に一致しないファイルを消さないことを確かめます。
- 2 つの `Logger` から同時に多数の行を書き、行が混ざらず、すべての行が JSON として読めることを確かめます。

### 3.3. T3: PowerShell の終了コード

**仕様**

終了コードを、スクリプトの最後に実行した文の成否で決めます。Linux の `sh -c` と同じ考え方です。

| 最後に実行した文 | 終了コード |
| --- | --- |
| 成功した (`$?` が真) | 0 |
| 失敗した (`$?` が偽) で、`$LASTEXITCODE` が 0 以外 | `$LASTEXITCODE` |
| 失敗した (`$?` が偽) で、`$LASTEXITCODE` が 0 または未設定 | 1 |
| スクリプトが `exit N` で終わった | N (後ろの処理に到達しない) |
| 終了するエラー (`throw` など) で止まった | PowerShell の既定 (1) |

Table: PowerShell の終了コード

- `powershellArgs` は、スクリプトの後ろに、改行に続けて「直前の `$?` と `$LASTEXITCODE` から終了する」処理を加えます。スクリプトの末尾に改行がなくても、加える処理が最後の文の一部にならないようにします。
- これまでの `if ($LASTEXITCODE) { exit $LASTEXITCODE }` (途中のネイティブ コマンドの失敗が残る) は使いません。

> [!NOTE]
> 途中のネイティブ コマンドが失敗し、最後のコマンドレットも失敗した場合は、`$LASTEXITCODE` (途中の値) を返します。最後の文が失敗したことは正しく伝わり、値だけが 1 と異なります。

**文書**

- `docs/reference.md` の「シェル」: 「後: ...」の説明を上の表に置き換え、「コマンドレットのエラーだけでは、終了コードが 0 のままになる場合があります」を削除します。
- `internal/tools/help/windows.md`: 終了コードの決まり方を 1 行で書きます。

**検証** (Windows の CI)

| スクリプト | 期待する終了コード |
| --- | --- |
| `Get-Item C:\no-such-file` | 1 |
| `cmd /c exit 3; Get-Date` | 0 |
| `Get-Item C:\no-such-file -ErrorAction SilentlyContinue; Get-Date` | 0 |
| `cmd /c exit 3` | 3 |
| `Write-Output ok` (末尾の改行なし) | 0 |
| `exit 5` | 5 |

Table: PowerShell の終了コードの検証

### 3.4. T4: cmd の出力の文字コード

**仕様**

- `internal/proc/proc_windows.go` の TODO を消します。cmd の内部コマンドの OEM コード ページの出力は、`output_encoding` の自動判定 (行ごとに、UTF-8 でなければコンソールのコード ページとみなす) で UTF-8 に変換されます。コメントには、変換はツールの側 (`textenc`) で行うことを書きます。

**検証**

- 既存のテストが通ること。

### 3.5. T5: host-shell の終了時の子孫プロセス

**仕様 (Windows)**

- `--mcp` で起動したとき、最初に Job Object を作り、「最後のハンドルが閉じたら全員を終了する」(JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE) を設定して、host-shell 自身をその Job Object に入れます。ハンドルは終了まで閉じません。
- host-shell が起動するプロセス (ConPTY の conhost を含む) と、その子孫は、自動的に同じ Job Object に入ります。host-shell がどのように終了しても (正常な終了、SSH の切断、強制終了、異常終了)、OS がそれらを終了させます。
- 抜け道 (JOB_OBJECT_LIMIT_BREAKAWAY_OK) は設定しません。
- Job Object の作成や登録に失敗した場合 (古い OS や、入れ子にできない Job Object の中で起動された場合) は、標準エラーに警告を出して続けます。従来の `taskkill /T` による終了の処理はそのまま残します。
- タイムアウト、取り消し、`job_stop` のときの `taskkill /T /F` は、これまでどおり使います。

**仕様 (Linux)**

- コマンドを起動するとき、`SysProcAttr.Pdeathsig` に SIGKILL を設定します。host-shell が終了すると、直接の子が OS によって終了されます。PTY のジョブ (`internal/pty/pty_unix.go`) も同じです。
- Pdeathsig は Linux だけの機能のため、`internal/proc/proc_unix.go` から Linux の部分を分けます (macOS では設定しません。macOS は手元の bridge としてだけ使います)。

> [!NOTE]
> Go の Pdeathsig は、子を起動した OS のスレッドの終了に結び付いています。host-shell は OS のスレッドを固定する処理 (`runtime.LockOSThread`) を使わないため、スレッドが途中で終了して子が誤って終了されることはありません。この前提をコメントに書きます。

**文書**

- `docs/reference.md` の「終了のさせ方」: 表に「host-shell が強制終了された場合」を加えます。Windows はすべての子孫が終了すること、Linux は直接の子が終了し、孫が残る場合があることを書きます。
- `docs/reference.md` と `internal/tools/help/jobs.md`: セッションより長く動かしたいプロセスは、Windows ではサービスやタスク スケジューラ、Linux では systemd のユニット (`systemd-run --user` など) で起動するよう書きます。

**検証**

- テストのプロセスを、テストの実行ファイル自身を子として起動する形で作ります。子は、終了の仕組みを有効にして長く動くコマンド (Windows は `ping -n 60 127.0.0.1`、Linux は `sleep 60`) を起動し、その PID を出力してから強制終了します。親は、そのコマンドが数秒以内に終了したことを確かめます。Windows と Linux の CI で実行します。

### 3.6. T6: sudo_run のパスワード

**仕様**

- `sudo_run` に `password` (文字列、省略可) を加えます。`proc_kill`、`svc_control`、`log_query` の `sudo: true` には加えず、それらの説明に「パスワードが必要な場合は `sudo_run` を使う」と書きます。
- `password` を省略した場合は、これまでどおり `sudo -n` で実行します。
- `password` を指定した場合は、次の手順で実行します。
    1. 呼び出しごとに、権限 700 の一時ディレクトリを作り、その中に権限 600 の名前付きパイプ (FIFO) を作ります。
    2. 名前付きパイプを読み書き両用で開き (書き手として待たずに開くため)、パスワードと改行を 3 回分書き込みます (sudo が誤りで最大 3 回尋ねるため)。
    3. `sudo -A [-u user] -- sh -c <command>` を、環境変数 `SUDO_ASKPASS` に host-shell の実行ファイルの絶対パス、`HOST_SHELL_ASKPASS` に名前付きパイプのパスを設定して起動します。環境変数にパスワードは入れません。
    4. コマンドの終了後、名前付きパイプを閉じ、一時ディレクトリごと消します。
- **askpass としての動作**: host-shell は、環境変数 `HOST_SHELL_ASKPASS` があるときは、ほかの引数を見ずに askpass として動きます。名前付きパイプから 1 行 (改行まで 1 バイトずつ) を読み、標準出力に書いて終了します。sudo は、パスワードを尋ねるときだけ askpass を起動するため、パスワードが不要な場合はだれも読まず、パスワードがコマンドに渡ることはありません。
- **記録と伏せ字**: 操作ログには、`password` の値の代わりに `<redacted>` を記録します。結果 (標準出力、標準エラー) にパスワードの文字列が現れた場合は、`--redact` の指定によらず `[REDACTED]` に置き換えます。
- Windows では `sudo_run` は登録されないため、変更はありません。

**文書**

- `internal/tools/help/commands.md` の「sudo_run」: `password` の使い方と、パスワードがエージェントを通ることを書きます。PTY のジョブで sudo を使う方法も、代わりの手段として残します。
- `docs/reference.md` の「sudo_run」: 引数の表、上の手順の概要、記録と伏せ字を書きます。制限事項の「`sudo_run` は `-n` だけに対応」を削除します。
- `docs/operations.md` の「sudo の設定」: NOPASSWD の規則がなくても、利用者がパスワードを渡せば使えることを書きます。NOPASSWD の規則と比べて、パスワードがエージェントを通る点を注意として書きます。

**検証** (Linux の CI。GitHub の ubuntu の環境は、パスワードなしの sudo が使えます)

- テスト用の利用者とパスワード、その利用者にだけパスワードを求める sudoers の規則を CI の中で作り、`password` を付けた `sudo_run` が成功すること、誤ったパスワードで失敗すること、`password` なしで「a password is required」のエラーになることを確かめます。CI の権限がない環境では飛ばします。
- パスワードが不要な場合 (NOPASSWD の規則がある場合) に `password` を付けて `cat` を実行し、出力にパスワードが現れないことを確かめます。
- askpass の動作 (名前付きパイプから 1 行を返す) は、単体のテストでも確かめます。
- 操作ログにパスワードが記録されないことを確かめます。

### 3.7. T7: sys_processes の構造化

**仕様**

- `sys_processes` の結果を、次の項目の一覧に変えます。OS によって取れない項目は空 (省略) にします。

| 項目 | 内容 | Linux | Windows |
| --- | --- | --- | --- |
| `pid` | プロセスの ID | `/proc/<pid>` | Toolhelp |
| `ppid` | 親のプロセスの ID | `/proc/<pid>/stat` | Toolhelp |
| `name` | プロセスの名前 | `/proc/<pid>/stat` の comm | Toolhelp の実行ファイル名 |
| `command_line` | コマンド行 | `/proc/<pid>/cmdline` | NtQueryInformationProcess (権限がなければ空) |
| `user` | 実行している利用者 | `/proc/<pid>/status` の Uid から名前 | プロセスのトークン (権限がなければ空) |
| `state` | 状態 | `/proc/<pid>/stat` の state | なし |
| `start_time` | 開始時刻 (RFC 3339) | `/proc/<pid>/stat` の starttime と起動時刻 | GetProcessTimes (権限がなければ空) |
| `memory_bytes` | 使用中の物理メモリ | `/proc/<pid>/statm` の RSS | GetProcessMemoryInfo (権限がなければ空) |

Table: sys_processes の項目

- 入力は、`filter` (名前またはコマンド行に含まれる文字列。大文字と小文字を区別しない) を残し、`pid` (1 つのプロセスとその子孫だけ) と `max` (件数の上限。既定 500) を加えます。結果には、全体の件数と、上限で打ち切ったかを含めます。
- OS の標準のコマンド (`ps`、`tasklist`) は使いません。
- macOS (bridge として動く場合) は、`ps` の出力を同じ形に読み替えます。

**文書**

- ツールの説明、`internal/tools/help/admin.md`、`docs/reference.md` の「sys_processes」を、構造化した結果の形に改めます。

**検証**

- Linux と Windows の CI で、テストのプロセスが自分自身を一覧に含むこと、`ppid` が親を指すこと、`pid` の指定で子孫だけが返ること、`filter` と `max` が効くことを確かめます。

### 3.8. T8: fs_write と fs_edit の原子的な置き換え

**仕様**

- `fs_write` と `fs_edit` に `atomic` (真偽値、既定は偽) を加えます。
- `atomic` が偽の場合は、これまでどおり、既存のファイルをその場で書き換えます。
- `atomic` が真の場合は、次の手順で置き換えます。
    1. 対象がシンボリック リンクなら、リンクの先を対象にします。
    2. 対象と同じディレクトリに一時ファイル (`.<名前>.host-shell-tmp-<乱数>`) を作り、内容を書いて同期 (fsync) します。
    3. 既存のファイルがある場合は、権限 (モード) を引き継ぎます。所有者が host-shell を動かしている利用者と異なり、引き継げない場合 (root で動いていない場合) は、一時ファイルを消してエラーにし、`atomic` なしで書くよう案内します。root で動いている場合は所有者とグループも引き継ぎます。
    4. 名前を変えて置き換えます。失敗した場合は一時ファイルを消します。
- ACL、拡張属性、ハード リンクの関係は引き継ぎません。
- `fs_write` の `atomic` は、`mode` が `overwrite` (既定) のときだけ使えます。`append`、`create`、`offset` と組み合わせた場合はエラーにします。
- Windows では、置き換えに `MoveFileEx` (MOVEFILE_REPLACE_EXISTING) の働きを使います (Go の `os.Rename`)。ほかのプロセスが開いているために置き換えられない場合は、エラーの説明にその可能性を書きます。

**文書**

- ツールの説明、`internal/tools/help/files.md` と `internal/tools/help/edit.md`: `atomic` を使う場面 (動いているサービスが読む設定ファイルなど、途中の状態を見せたくない場合) と、引き継がないもの (ACL、拡張属性、ハード リンク) を書きます。
- `docs/reference.md` の「fs_write」「fs_edit」: 引数の表と、上の手順を書きます。

**検証**

- Linux と Windows で、`atomic` で置き換えた後の内容、権限の引き継ぎ、シンボリック リンクの先が置き換わること (Linux)、一時ファイルが残らないことを確かめます。
- Linux で、自分以外が持ち主のファイルを `atomic` で書こうとするとエラーになり、ファイルが変わらないことを確かめます (CI では sudo でほかの利用者のファイルを作ります)。
- `append`、`create`、`offset` との組み合わせがエラーになることを確かめます。

## 4. 制限事項の対策

### 4.1. L1: job_read の画面の状態

**仕様**

- PTY のジョブは、出力を仮想端末にも流し、端末の大きさ (`cols`、`rows`) の画面の状態を保ちます。`job_write` の `cols`、`rows` による大きさの変更は、仮想端末にも反映します。
- `job_read` に `screen` (真偽値、既定は偽) を加えます。真の場合、`output` の代わりに、今の画面に表示されている文字を、行ごとに改行で区切って返します。行の末尾の空白は除きます。カーソルの位置 (`cursor_row`、`cursor_col`、0 から) も返します。`offset` と `max_bytes` は無視し、`wait_sec`、`until`、`idle_ms` は、出力の流れに対してこれまでどおり働きます (待った後の画面を返します)。
- PTY でないジョブで `screen` を指定した場合はエラーにします。

**実装の手段**

- 既存のライブラリを使います。第 1 の候補は `github.com/charmbracelet/x/vt` (MIT ライセンス。Charm が保守) です。次の条件を満たすかを、実装の最初に確かめます。
    - Go だけで書かれ、`CGO_ENABLED=0` で Linux、Windows、macOS 向けにビルドできる。
    - ライセンスが MIT、BSD、Apache-2.0 のいずれか。
    - `top -b` でない `top`、`vi`、`less` の画面を、テストで期待どおりに再現できる。
- 第 1 の候補が条件を満たさない場合は、ほかの候補 (`github.com/ActiveState/vt10x` など) を同じ条件で確かめます。どれも満たさない場合は、`internal/tools/ansi.go` の解析を広げて自前で実装します。
- 採用したモジュールは、`docs/development.md` の「依存」の表に用途とともに加えます。

**文書**

- ツールの説明、`internal/tools/help/jobs.md`、`docs/reference.md` の「job_read」と「疑似端末 (pty)」: `screen` の使い方 (全画面のプログラムの状態を読むとき) を書きます。制限事項の L1 を、「画面の状態は `screen` で読める」内容に改めます (流れのままでは読みにくいことは残します)。

**検証**

- Linux の CI で、`top` と `vi` を PTY のジョブで起動し、`screen` の結果に期待する文字 (`top` の見出し、`vi` で開いたファイルの内容) が、期待する行に現れることを確かめます。Windows の CI で、PowerShell の画面の消去 (`Clear-Host`) の後に、消去前の内容が残らないことを確かめます。

### 4.2. L2: 検索での .gitignore

**仕様**

- `search_grep` と `search_files` に `gitignore` (真偽値、既定は真) を加えます。真の場合、走査の対象から、git が無視するファイルとディレクトリを除きます。
- 対象とする規則のファイルは、走査の起点からその上のリポジトリのルート (`.git` のあるディレクトリ) までの各ディレクトリと、走査で降りる各ディレクトリの `.gitignore`、およびリポジトリの `.git/info/exclude` です。git の全体の設定 (`core.excludesFile`) は読みません。
- 対応する書式: 空行と `#` のコメント、行末の空白 (`\ ` で残す)、否定 (`!`)、末尾の `/` (ディレクトリだけ)、先頭や途中の `/` (その `.gitignore` のディレクトリからの相対)、`*`、`?`、`[...]`、`**`。
- 無視されたディレクトリには降りません。無視されたディレクトリの中のファイルを否定で戻すことはできません (git と同じ)。
- 既存の `exclude_dirs` (既定は `.git`、`.hg`、`.svn`) は、`gitignore` と別に、これまでどおり働きます。
- 走査の起点がリポジトリの外の場合は、起点より下の `.gitignore` だけを使います。
- 実装は自前で行い、`internal/tools/glob.go` の照合の処理を基にします。

**文書**

- ツールの説明、`internal/tools/help/search.md`、`docs/reference.md` の「search_grep」「search_files」: `gitignore` の既定と、読む規則のファイルを書きます。制限事項の L2 を削除します。

**検証**

- 書式の各要素を含む `.gitignore` を持つリポジトリを、テストの中で作ります。`git` がある環境では、`git check-ignore` の結果と、自前の判定の結果が、すべてのファイルについて一致することを確かめます。`git` がない環境では、期待する結果の表と比べます。

### 4.3. L3: search_grep の multiline

**仕様**

- `search_grep` に `multiline` (真偽値、既定は偽) を加えます。真の場合、ファイル全体を UTF-8 に変換してから照合し、`.` が改行にも一致するようにします (正規表現の `(?s)` の扱い)。
- 一致ごとに、開始の行番号と、一致した範囲の内容 (500 バイトで切る) を返します。`context`、`before`、`after` は、一致の範囲の前後の行として働きます。
- 対象のファイルの大きさの上限は 32 MiB (`fs_edit` の上限と同じ) です。これを超えるファイルは飛ばし、飛ばした数を結果に含めます。`max_file_bytes` が小さい場合は、そちらが優先します。

**文書**

- ツールの説明、`internal/tools/help/search.md`、`docs/reference.md` の「search_grep」: `multiline` の使い方と上限を書きます。制限事項の L3 を削除します。

**検証**

- 複数の行にまたがる一致、行番号、`context`、上限を超えるファイルが飛ばされることを確かめます。

### 4.4. L7: BOM のない UTF-16 の判定

**仕様**

- `internal/textenc` に、BOM のない UTF-16LE と UTF-16BE を、NUL の並びから判定する共通の処理を加えます。判定は、先頭から決まった長さ (最大 4096 バイト) を調べ、奇数番目 (UTF-16LE) または偶数番目 (UTF-16BE) のバイトのうち、NUL の割合が一定以上で、反対側の NUL の割合が一定以下であることを条件にします。割合の値は、次の検証で、英語と日本語を含む UTF-16 の実際のファイルと、NUL を含むバイナリのファイル (実行ファイル、画像、圧縮ファイル) を使って決め、コメントに根拠とともに書きます。
- この判定を、次の 3 か所で使います。
    - `fs_read` と `fs_edit` の文字コードの自動判定 (BOM の確認の次)
    - `search_grep` の自動判定 (BOM のない UTF-16 をバイナリとみなす前)
    - コマンドの出力の自動判定 (`output_encoding` の `auto`。出力の先頭で判定する)
- Windows の `log_query` の `decodeToolOutput` は、この共通の処理を使う形に改めます。

**文書**

- `docs/reference.md` の「自動判定」: BOM のない UTF-16 の判定の条件を書きます。制限事項の L7 を削除します。

**検証**

- BOM のない UTF-16LE と UTF-16BE (英語のみ、日本語を含む) を正しく判定すること、バイナリのファイルの見本を UTF-16 と誤らないことを確かめます。`fs_read`、`search_grep`、`exec_run` (UTF-16 を出力するコマンド) で確かめます。

### 4.5. L8: PowerShell の標準入力の文字コード

**仕様**

- `powershellArgs` が加えるスクリプトの前の処理に、`[Console]::InputEncoding = [System.Text.Encoding]::UTF8` を加えます。
- Windows の CI で、日本語を含む `stdin` を PowerShell が正しく受け取ることを確かめます。効かないことが分かった場合は、この設定を加えず、代わりに `shell` が `powershell` または `pwsh` のときだけ、`stdin_encoding` の既定をコンソールのコード ページ (`system`) にします。どちらにしたかを、コメントと文書に書きます。

**文書**

- `docs/reference.md` の「シェル」: 標準入力の文字コードの扱いを書きます。制限事項の L8 を削除します。

**検証** (Windows の CI)

- `exec_run` で `shell: "powershell"`、`stdin: "日本語\n"`、コマンド `$input | ForEach-Object { $_ }` を実行し、`stdout` が `日本語` になることを確かめます。`pwsh` がある場合は `pwsh` でも確かめます。

### 4.6. L11: bridge の並列の転送

**仕様**

- `transfer_push` と `transfer_pull` に `parallel` (整数、1 から 16、既定 4) を加えます。範囲外の値はエラーにします。
- 送るファイルの一覧を作るまで (`fs_manifest`、`fs_stat`) は、これまでどおり 1 回ずつ行います。その後、ファイルごとの処理 (分割した書き込みまたは読み取り、照合、名前の変更) を、最大 `parallel` 個まで同時に行います。1 つのファイルの分割した書き込みは、これまでどおり順に行います。
- 結果の `files` は、同時に送っても、送り元の一覧の順に並べます。
- 進み具合の通知は、送り終えたファイルの数と大きさで、これまでどおり送ります。
- 取り消された場合は、送っている途中のファイルの `.host-shell-part` を残さないよう、これまでと同じく片付けます。

**文書**

- `docs/reference.md` の「transfer_push」: `parallel` を書きます。制限事項の L11 を削除します。

**検証**

- 多数の小さいファイルと大きいファイルを含むディレクトリを `parallel` 1 と 4 で送り、結果と内容が同じであること、結果の順が一覧の順であることを確かめます。範囲外の値がエラーになることを確かめます。

## 5. 仕様として容認する制限事項

次の項目は、対策せず、`docs/reference.md` の制限事項に残します。

| 項目 | 内容 | 容認の理由 |
| --- | --- | --- |
| L5 | 自動判定は UTF-8 以外の文字コードどうしを区別しない | 統計による推定は確実でなく、結果が予測しにくくなるため。`charset` の指定で対応する |
| L6 | UTF-8 以外の短い文字列を UTF-8 と誤ることがある | 判定の方式に由来し、材料が足りない場合は避けられないため。明示の指定で対応する |
| L9 | 管理のツールは Linux と Windows だけ。Linux は systemd が前提 | 主な Linux の配布物は systemd を使い、そうでない環境では `exec_run` で同じことができるため |
| L10 | 制限 (方針) は OS の権限の境界ではない | 引数の検査は意図しない操作を防ぐためのもので、強い隔離は専用のユーザーなど OS の側で行うため |
| L13 | 動作する OS は Go 1.26 の要件による | Go の版を下げる負担が、得られる効果に比べて大きいため |

Table: 容認する制限事項

## 6. 受入条件

- T1 から T8 の TODO の注記が、リポジトリから消えていること (`git grep -n TODO -- '*.go'` で該当がない)。
- [3](#3-todo-の対策) と [4](#4-制限事項の対策) の各節の「検証」が、テストとして実装され、Linux、Windows、macOS の CI で成功すること。OS に依存するものは、その OS で実行されること。
- `go vet` が Linux、Windows、macOS 向けに通り、`gofmt` の差分がないこと。
- `docs/reference.md` の制限事項に、L5、L6、L9、L10、L13 と、L1 の残りの内容だけが残ること。
- 新しい引数と変わった動作が、ツールの説明、`help`、`docs/` に書かれていること。
- 新しく加えた依存 (L1 の仮想端末のライブラリ) が、`docs/development.md` の「依存」の表にあること。
- リリース ノートに、既定の動作が変わる項目 (T2、T3、T5、T7、L2) と、古い `audit.log` の扱いが書かれていること。

## 7. 作業の順序

依存の関係から、次の順に進めます。共通の処理を変える項目を先に、それを使う項目を後にします。

1. T4 (TODO の削除)
2. T1 (TODO の削除と文書)
3. L7 (文字コードの判定の共通化。L8 とコマンドの出力の判定の前提)
4. T3 と L8 (PowerShell のスクリプトの前後の処理。同じ関数を変えるため続けて行う。コミットは分ける)
5. T5 (Windows の Job Object、Linux の Pdeathsig。コミットは OS ごとに分ける)
6. T2 (操作ログ)
7. T6 (sudo_run の password)
8. T8 (atomic)
9. L3 (multiline)
10. L2 (.gitignore)
11. T7 (sys_processes)
12. L1 (仮想端末)
13. L11 (bridge の並列の転送)
14. 制限事項の節の最終的な整理、リリース ノートの作成、リリース

## 8. 正本への反映

この最終仕様の内容のうち、将来にわたって必要なものを、次の正本に反映します。検討の経緯は反映しません。

| 反映先 | 内容 |
| --- | --- |
| ソース コードのコメント (英語) | T1 の既定を PowerShell にする理由、T3 の終了コードの決まり方、T5 の Job Object と Pdeathsig の働きと前提 (`LockOSThread` を使わないこと)、T6 の askpass の流れ、L7 の判定の条件の値の根拠 |
| `docs/reference.md` | 各節の「文書」に書いた引数、動作、制限事項の更新 |
| `docs/operations.md` | 操作ログの日ごとの切り替えと保持、sudo のパスワード |
| `docs/user-guide.md` | Windows の接続先での cmd の選び方 |
| `docs/development.md` | 依存の表 (仮想端末のライブラリ) |
| `internal/tools/help/*.md` | エージェント向けの新しい引数の使い方 |
| skill (`SKILL.md`) | 操作ログのファイル名 |

Table: 正本への反映
