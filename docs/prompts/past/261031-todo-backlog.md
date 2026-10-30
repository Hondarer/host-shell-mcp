<!--
生成元: agree-before-implement スキル
文書種別: 残課題一覧 (backlog)
最終更新エージェント: claude code / セッション ID: d9875064-c62a-4b5f-9eb9-9962ce3d252f
上流文書: `261031-todo-design.md` (最終仕様)、`261031-todo-discussion.md` (検討用 Markdown)。いずれも削除されている場合があります。
再開手順: このファイルを開いたエージェントは、作業の前に agree-before-implement スキル (/home/user/c-modernization-kit/.agents/skills/agree-before-implement) を読み込んでください。「整理状態」が「随時追記中」であれば、案件の作業中です。気付いた事項を追記し、第 3 ゲートの前に文書を再構築して整理します。上流文書が存在する場合は、各課題の背景と見送りの判断を上流文書で確認します。存在しない場合は、この文書を単体の残課題一覧として扱います。このファイルの残課題を参照して新たな成果物の作成または更新を始めるときは、元案件の再開とは別の作業として扱います。その開始には agree-before-implement を原則として使い、新しい検討用 Markdown から開始してください。最終更新エージェントのセッション情報が参照可能であれば再開時のコンテキストとして利用し、別エージェントからの引き継ぎや担当変更によりセッションが見つからない場合も異常とせず、文書の記載から継続してください。
-->

# 残課題一覧 (バックログ)

ファイル名: `docs/prompts/past/261031-todo-backlog.md`

## 案件の識別

案件名: host-shell の TODO と制限事項の対策  
作成日: 2026-10-31  
整理状態: 整理済み

この文書は、作業中に潜在または別問題として判明したが、今回は対策しなかった最終的な残渣 (残課題) を記録したものです。作業中に解消した問題や、仕様・制約として容認した問題点は含みません。各項目はチケット管理システム (GitHub Issues、Jira など) への起票にそのまま利用できる詳細度・具体性で記載しています。

## 残課題一覧

| 課題 ID | 題名 | 区分 | 優先度 / 影響度 | 状態 |
| --- | --- | --- | --- | --- |
| [BL-001](#bl-001-powershell-の標準入力を-utf-8-で受け取らせる) | PowerShell の標準入力を UTF-8 で受け取らせる | 作業中摘出 | 影響度: 中 | 対応済み (`261031-ps-stdin-design.md`、コミット `1d5c7cf`、`6ac5bf0`) |

## BL-001 PowerShell の標準入力を UTF-8 で受け取らせる

### 概要と問題点

- **現状の事象**: Windows で PowerShell (`powershell`、`pwsh`) が実行するコマンドに `exec_run` や `job_start` の `stdin` を渡すと、`host-shell` は `stdin` をコンソールのコード ページに変換して渡す (`stdin_encoding` の既定が `system`)。コード ページで表せない文字は渡せず、`stdin: character '日' ... cannot be encoded in cp437 (set stdin_encoding ...)` のエラーになる。英語の環境 (CP437) での日本語、日本語の環境 (CP932) での絵文字などが該当する。
- **本来あるべき姿**: 接続先のコード ページによらず、任意の Unicode の文字を `stdin` で PowerShell に渡せる。

### 経緯と見送り理由

- **摘出契機**: 制限事項 L8 (PowerShell の標準入力の文字コード) の実装中に発見。スクリプトの先頭で `[Console]::InputEncoding` を UTF-8 にしても、`$input` には効かないことを Windows 11 (PowerShell 5.1、PowerShell 7) で確認した。PowerShell は、スクリプトの実行の前に標準入力の読み取りを準備するためである。最終仕様の代替策 (既定の `stdin_encoding` を `system` にする) を実装した結果、GitHub Actions の Windows (コンソールのコード ページが 437) で、日本語の `stdin` がエラーになった。
- **今回の見送り理由**: 最終仕様の範囲外 (最終仕様は、`InputEncoding` が効かない場合に `system` を既定にすることまでを定めている)。根本的な対策には、PowerShell の起動の仕方を変える別の設計が必要。

### 影響範囲とリスク

- **影響対象**: `internal/proc/proc_windows.go` (`shellCommand` の `powershell`、`pwsh`)、`internal/tools/exec.go` (`stdinDefault`)。英語の環境の Windows に、日本語などを `stdin` で渡す使い方。
- **放置した場合のリスク**: エージェントが日本語のテキストを PowerShell に `stdin` で渡すと、英語の環境の Windows ではエラーになる。エージェントは `stdin_encoding: "utf-8"` を指定できるが、その場合は PowerShell 側で文字化けする。代わりに `fs_write` でファイルに書いてから読ませる必要がある。

### 推奨される対応策

- **恒久対応案**: PowerShell を起動する前に、子プロセスのコンソールの入力のコード ページを UTF-8 (65001) にする。例えば、`cmd /d /c "chcp 65001 >nul & powershell.exe -NoProfile ... -EncodedCommand <base64>"` の形で起動し、cmd と同じコンソールを使う PowerShell が UTF-8 で標準入力を読むようにする。そのうえで、PowerShell の `stdin_encoding` の既定を `utf-8` に戻す。起動の時間 (cmd を 1 つ多く起動する) と、`taskkill /T` や Job Object によるプロセス ツリーの終了への影響を確かめる。
- **参考情報**: `internal/server/powershell_test.go` の `TestPowerShellStdin` (コード ページで日本語を表せない場合は飛ばしている)、`docs/reference.md` の「シェル」の節の標準入力の説明、コミット `3a550d1`、`960171c`。
