<!--
生成元: agree-before-implement スキル
文書種別: 残課題一覧 (backlog)
最終更新エージェント: claude code / セッション ID: session_01KJc53UTLFzqXyXoZpjPqCE
上流文書: 261101-code-server-followup-design.md (最終仕様)、261101-code-server-followup-discussion.md (検討用 Markdown)。いずれも削除されている場合があります。
再開手順: このファイルを開いたエージェントは、作業の前に /home/user/c-modernization-kit/app/general/.agents/skills/agree-before-implement/SKILL.md を読み込んでください。「整理状態」が「随時追記中」であれば、案件の作業中です。気付いた事項を追記し、第 3 ゲートの前に文書を再構築して整理します。上流文書が存在する場合は、各課題の背景と見送りの判断を上流文書で確認します。存在しない場合は、この文書を単体の残課題一覧として扱います。このファイルの残課題を参照して新たな成果物の作成または更新を始めるときは、元案件の再開とは別の作業として扱います。その開始には agree-before-implement を原則として使い、新しい検討用 Markdown から開始してください。継続作業によってこのファイルの残課題を対策する場合は、このファイルや各項目の記載を削除することなく、各項目の状態および経緯と対処を更新することを原則とします。最終更新エージェントのセッション情報が参照可能であれば再開時のコンテキストとして利用し、セッションが見つからない場合も異常とせず、文書の記載から継続してください。
-->

# 残課題一覧 (バックログ)

## 案件の識別

案件名: code-server の注入の残課題への対応 (261101-code-server-followup)  
作成日: 2026-11-01  
整理状態: 整理済み

この文書は、見送る決定がなされた問題点や、作業中に潜在または別問題として判明したが今回は対策しなかった最終的な残渣 (残課題) を記録したものです。作業期間中は気付いた時点で随時追記し、設計および実装・検証作業がすべて完了した後に、再構築して整理しています。当該作業中に解消した問題や、仕様・制約として容認した問題点は整理時に除外しています。各項目はチケット管理システム (GitHub Issues、Jira など) への起票にそのまま利用できる詳細度・具体性で記載されています。

元案件の残課題一覧 (同じフォルダーの `261101-code-server-backlog.md`) の 4 件は、この案件ですべて容認または解消したため、転記した項目はありません。

この一覧の 2 件は、継続案件 261101-session-socket-ci (同じフォルダーの `261101-session-socket-ci-discussion.md`、`261101-session-socket-ci-design.md`) で解消しました。各項目の「経緯と対処」に記録しています。

## 残課題一覧

| 課題 ID | 題名 | 区分 | 優先度 / 影響度 | 状態 |
| --- | --- | --- | --- | --- |
| BL-001 | HOME のパスが長い場合に、code-server のセッション用ソケットを作れない | 作業中摘出 | 低 | 解消 (2026-11-01、261101-session-socket-ci) |
| BL-002 | CI の ubuntu で TestPersistentForwards が断続的に失敗する | 作業中摘出 | 低 | 解消 (2026-11-01、261101-session-socket-ci) |

## BL-001 HOME のパスが長い場合に、code-server のセッション用ソケットを作れない

### 概要と問題点

- **現状の事象**: code-server は、ユーザー データのフォルダー (`~/.local/share/host-shell-code-server/user-data/`) に、セッション用の Unix ドメイン ソケット `code-server-ipc.sock` を作ります。HOME のパスが長く、ソケットのパスが Unix ドメイン ソケットの上限 (Linux では約 108 バイト) を超えると、起動時に `Could not create socket at ...` の警告が出て、拡張機能ホストのログに `connect EINVAL ... code-server-ipc.sock` が記録されます。確認環境 (HOME を長いパスの一時フォルダーに向けた場合) で発生しました。画面の表示、ログイン、拡張機能の導入には影響しませんでした。
- **本来あるべき姿**: HOME の長さにかかわらず、セッション用ソケットを使う機能 (code-server の内蔵端末から `code-server <ファイル>` で既存のウィンドウにファイルを開く操作など) が動くこと。少なくとも、制約が文書に記載されていること。

### 経緯と見送り理由

- **摘出契機**: 実装作業中の確認 (HOME を作業用の一時フォルダーに向けた Linux の接続先での code-server のログ)。
- **今回の見送り理由**: 通常の HOME (`/home/<利用者>` など) ではパスが上限に収まり、本案件の範囲 (空きポート、コンテナー、拡張機能ビュー) の外であるため。

### 影響範囲とリスク

- **影響対象**: スキル host-shell-code-server の start (`--user-data-dir` の指定)、ホーム ディレクトリーのパスが深い接続先。
- **放置した場合のリスク**: 内蔵端末から既存のウィンドウにファイルを開く操作などが、警告だけを残して動かない場合があります。

### 推奨される対応策

- **恒久対応案**: start で、ソケットのパスの長さを調べ、上限を超える場合は警告を出すか、code-server がソケットを置く場所を短いパス (`XDG_RUNTIME_DIR` など) に変えられるかを調べて対応します。対応しない場合は、文書の注意点に制約を記載します。
- **参考情報**: `.agents/skills/host-shell-code-server/scripts/linux/start.sh`、code-server の `--user-data-dir`。

### 経緯と対処

- **状態**: 解消 (2026-11-01、261101-session-socket-ci)
- **調査で分かったこと**: code-server 4.144.0 のソースを確認したところ、内蔵端末からの `code-server <ファイル>` は環境変数 `VSCODE_IPC_HOOK_CLI` の経路を使い、このソケットに依存しませんでした。ソケットを使うのは、拡張機能ホストがブラウザーの画面を登録する処理と、VS Code の端末の外から開いている画面にファイルを開く処理です。上記の「本来あるべき姿」の例 (内蔵端末からの操作) は、もともと影響を受けていませんでした。ソケットのパスは HOME の後ろに 67 文字が付くため、HOME が 41 バイト以上で上限 (107 バイト) を超えます。
- **対処**: Linux の `start.sh` で、既定のパスが 107 バイトを超える場合だけ、`--session-socket` で短いパスを指定するようにしました。置き場所は `$XDG_RUNTIME_DIR/host-shell-code-server-ipc.sock`、`XDG_RUNTIME_DIR` がなければ `/tmp/host-shell-code-server-<uid>/ipc.sock` です。`/tmp` のフォルダーは host-shell-connect の `deploy.sh` と同じ規則で確かめ、確認に失敗した場合は警告を出して既定のパスのまま起動します。`docs/*/code-server.md` の配置の表に記載しました。Windows は名前付きパイプのため変更していません。

## BL-002 CI の ubuntu で TestPersistentForwards が断続的に失敗する

### 概要と問題点

- **現状の事象**: PR #5 の CI (run 41280901078 の 1 回目、コミット c7b7b1e) で、ubuntu-latest の `internal/forward` の `TestPersistentForwards` が 1.00 秒で失敗しました。失敗したジョブだけを再実行すると成功し、macOS と Windows は 1 回目から成功しました。PR #5 は Go のコードを変更していません。CI の Test の段は `go test -v` の出力を `grep -E '^(---|ok|FAIL|PASS|panic)|_test\.go'` で絞り込んでいるため、失敗の理由を示すメッセージはログに残っていませんでした。
- **本来あるべき姿**: `TestPersistentForwards` が環境の負荷にかかわらず安定して成功すること。失敗した場合は、理由がログで分かること。

### 経緯と見送り理由

- **摘出契機**: 本案件の CI の実行中に発生。
- **今回の見送り理由**: 本案件はスキルと文書の変更で、Go のコードの範囲外であるため。原因の調査に失敗時のログが必要なため。

### 影響範囲とリスク

- **影響対象**: `internal/forward` のテスト (`TestPersistentForwards`)、CI の Test の段。
- **放置した場合のリスク**: 関係のない変更の PR で CI が失敗し、再実行が必要になります。

### 推奨される対応策

- **恒久対応案**: テストは、転送の往復の直後に操作ログ (`forward_connect` など) を読んで照合しています。ログの書き込みが往復の完了より遅れる可能性を調べ、期待する記録が現れるまで待つ形にします。あわせて、CI の Test の段で失敗時のメッセージ (`t.Fatalf` の出力) が残るよう、絞り込みの条件を見直します。
- **参考情報**: `internal/forward` の `TestPersistentForwards`、`.github/workflows/ci.yml` の Test の段、run 41280901078。

### 経緯と対処

- **状態**: 解消 (2026-11-01、261101-session-socket-ci)
- **原因**: 作業環境で 100 回の繰り返し実行により再現しました (100 回中 1〜2 回)。失敗はテスト本体ではなく後片付けで、`testing.go:1464: TempDir RemoveAll cleanup: ... directory not empty` でした。接続先の `Target.Close` が、接続の切断で別の goroutine が実行中の `unlisten` (操作ログ `forward_unlisten` の書き込み) を待たずに戻るため、一時フォルダーの削除と書き込みが重なっていました。上記の推測 (往復の直後のログの照合) は原因ではありませんでした。運用でも、終了時に `forward_unlisten` の記録が失われる場合がありました。CI のログに理由が残らなかったのは、絞り込みが `_test\.go` を含む行だけを残し、`testing.go` の行を消していたためです。
- **対処**: `Target` に `unlisten` を直列にする排他を加え、`Close` が実行中の `unlisten` の終了を待つようにしました。`Close` の後に `forward_unlisten` がそろうことを確かめるテスト `TestCloseRecordsUnlisten` を加えました (修正前のコードでは失敗します)。CI の Test の段の絞り込みを、字下げされた「ファイル名.go:行:」の行をすべて残す条件に改め、`docs/*/development.md` の説明を直しました。修正後、`TestPersistentForwards` を 200 回連続で実行して失敗は 0 回でした。
