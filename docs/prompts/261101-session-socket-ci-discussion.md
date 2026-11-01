<!--
生成元: agree-before-implement スキル
文書種別: 検討用 Markdown (discussion)
最終更新エージェント: claude code / セッション ID: session_01KJc53UTLFzqXyXoZpjPqCE
下流文書: 261101-session-socket-ci-design.md (最終仕様)、261101-session-socket-ci-backlog.md (残課題一覧)。未作成の場合や、削除されている場合があります。
再開手順: このファイルを開いたエージェントは、作業の前に /home/user/c-modernization-kit/app/general/.agents/skills/agree-before-implement/SKILL.md を読み込んでください。ユーザーが編集した最新版を読み直し、作業 WBS の `← **CURRENT**` の項目から再開します。最終更新エージェントのセッション情報が参照可能であれば再開時のコンテキストとして利用し、別エージェントからの引き継ぎや担当変更によりセッションが見つからない場合も異常とせず、文書の記載から継続してください。`← **CURRENT**` の項目は処理が中止された項目である場合があり、どこまで進んでいたかは文書だけでは分かりません。対象成果物の内容を確認して実施済みの範囲を特定してから、作業を再開してください。`← **CURRENT**` がなく、作業 WBS がすべて完了 (✅) であれば、この案件の合意フローは完了しています。
-->

# code-server の注入の残課題 (追補の BL-001、BL-002) への対応の検討

## 検討の識別

検討 ID: 261101-session-socket-ci

版: 2

状態: 合意済み

## 作業 WBS と進捗管理

この検討用 Markdown が管理する作業 WBS です。Q&A のターンごとに必要に応じて見直します。見送りの決定や範囲外の気付きは、生じた時点で `261101-session-socket-ci-backlog.md` に追記します。

凡例: ⏸️ 未着手、🔄 実施中、🚫 ブロック中、⚠️ 要注意、✅ 完了。現在取り組んでいる項目の末尾に `← **CURRENT**` を付けます。

- ✅ 1. 検討用 Markdown のための情報収集と質問の実施
    - ✅ 1.1. 前提条件と要求事項の調査 (`docs/prompts/past/261101-code-server-followup-backlog.md`、`internal/forward/`、`.github/workflows/ci.yml`、`.agents/skills/host-shell-code-server/scripts/linux/start.sh`、code-server 4.144.0 の `out/node/`、作業環境での再現)
    - ✅ 1.2. 第 1 群の確認項目 ([Q-001](#q-001-対応する残課題)〜[Q-008](#q-008-作業ブランチと-pr-とマージの運用)) の作成
    - ✅ 1.3. 第 1 群の回答の受付と記録 (2026-11-01 22:39:54 JST)
    - ✅ 1.4. 回答に基づく前提・制約の検証と WBS の見直し (すべて推奨どおりのため、WBS の変更なし)
- ✅ 2. 前提合意と残懸念の解消 (第 1 ゲート: 最終仕様作成への着手)
    - ✅ 2.1. 全論点に対する未解決事項および質問の解消確認
    - ✅ 2.2. 方針・前提条件の合意確定 (懸念がないことをもってフェーズ 2 へ着手、合意版: 版 2)
- ✅ 3. 最終決定事項 (最終仕様) の作成と承認の受付
    - ✅ 3.1. `261101-session-socket-ci-design.md` の作成 (版 1)
    - ✅ 3.2. 最終仕様における受入条件と整合性の確認
    - ✅ 3.3. 対話セッションにおける最終仕様の承認の受付 (第 2 ゲート: 作業着手の合意、Tetsuo Honda が対話「承認」で版 1 を承認、確認日時: 2026-11-01 22:42:25 JST)
- 🔄 4. 承認された最終仕様に従った作業の実施
    - 🔄 4.1. 作業ブランチの準備 ([Q-008](#q-008-作業ブランチと-pr-とマージの運用)) ← **CURRENT**
    - ⏸️ 4.2. BL-002: 転送の後片付けの修正 (`internal/forward/target.go`、`internal/forward/forward_test.go`) ([Q-002](#q-002-bl-002-の修正箇所))
    - ⏸️ 4.3. BL-002: CI の Test の段の絞り込みの見直し (`.github/workflows/ci.yml`、`docs/ja/development.md`、`docs/en/development.md`) ([Q-003](#q-003-ci-の-test-の段の絞り込み))
    - ⏸️ 4.4. BL-001: セッション用ソケットの場所 (`.agents/skills/host-shell-code-server/scripts/linux/start.sh`、`docs/ja/code-server.md`、`docs/en/code-server.md`) ([Q-004](#q-004-bl-001-の対処方針)、[Q-005](#q-005-短いパスの置き場所))
    - ⏸️ 4.5. 確認 (BL-002 の繰り返し実行、BL-001 の長い HOME と通常の HOME)
    - ⏸️ 4.6. 元案件の残課題一覧 (`docs/prompts/past/261101-code-server-followup-backlog.md`) の状態の更新
    - ⏸️ 4.7. make、main 向けドラフト PR の作成と全 OS CI の実行
    - ⏸️ 4.8. 受入条件への適合確認 (全完確認)
    - ⏸️ 4.9. 残課題一覧 (`261101-session-socket-ci-backlog.md`) の再構築と整理
    - ⏸️ 4.10. 検討記録の `docs/prompts/past/` への移動、全 OS の CI の成功の確認とマージ
- ⏸️ 5. 成果物の承認の受付 (第 3 ゲート)
    - ⏸️ 5.1. 成果物・検証結果・残課題の提示
    - ⏸️ 5.2. 対話セッションにおける成果物の承認の受付

## 対象と参照資料

対象成果物: 元案件 261101-code-server-followup の残課題一覧の 2 件への対応 (Go のコード、テスト、CI、スキルのスクリプト、文書)。

今回の対象範囲: BL-001 と BL-002 の対応 ([Q-001](#q-001-対応する残課題))

書き込み可能な検討用 Markdown: この文書 (`docs/prompts/261101-session-socket-ci-discussion.md`)

参照資料と版: main のコミット e1d8b7c を調査基準とします。

- [docs/prompts/past/261101-code-server-followup-backlog.md](past/261101-code-server-followup-backlog.md): 対象の残課題一覧 (BL-001、BL-002)。
- [internal/forward/target.go](../../internal/forward/target.go): 接続先の転送の処理。`Close` と `unlisten` が操作ログ `forward_unlisten` を書きます。
- [internal/forward/forward_test.go](../../internal/forward/forward_test.go): `TestPersistentForwards` と、テスト用の接続を作る `start`。
- [.github/workflows/ci.yml](../../.github/workflows/ci.yml): Test の段は `go test -count=1 -v ./... 2>&1 | grep -E '^(---|ok|FAIL|PASS|panic)|_test\.go'` です。
- [.agents/skills/host-shell-code-server/scripts/linux/start.sh](../../.agents/skills/host-shell-code-server/scripts/linux/start.sh): code-server を `--user-data-dir ~/.local/share/host-shell-code-server/user-data` で起動します。
- code-server 4.144.0 の `out/node/cli.js` と `out/node/vscodeSocket.js` (配布物 `code-server-4.144.0-linux-amd64.tar.gz` から、作業用の一時フォルダーに展開して確認)。

## ユーザーが明示した要求

本対話の依頼 (2026-11-01、Tetsuo Honda) を根拠とします。

> ブランチは削除して。次に、backlog消化セッションを開始

- 前案件の作業ブランチ `claude/code-server-followup` は、手元とリモートの両方から削除しました (main にマージ済みであることを確認済み)。
- 前案件の残課題一覧 (`261101-code-server-followup-backlog.md`) を消化する案件を開始します。どの項目を対象とするかは [Q-001](#q-001-対応する残課題) で確認します。

## 現行の合意内容

第 1 群 ([Q-001](#q-001-対応する残課題)〜[Q-008](#q-008-作業ブランチと-pr-とマージの運用)) の回答 (2026-11-01、Tetsuo Honda) による合意内容です。すべての質問が解決済みです (合意版: 版 2)。

- 範囲: BL-001 と BL-002 の両方に対応します ([Q-001](#q-001-対応する残課題))。
- BL-002: `Target.Close` が実行中の `unlisten` の終了を待つように製品のコードを直し、`Close` の後に `forward_unlisten` の記録がそろうことを確かめるテストを加えます ([Q-002](#q-002-bl-002-の修正箇所))。
- BL-002 の CI: Test の段の絞り込みを、字下げされた「ファイル名.go:行:」の行をすべて残す条件にし、`development.md` の説明を直します ([Q-003](#q-003-ci-の-test-の段の絞り込み))。
- BL-001: Linux の start で、既定のパスが 107 バイトを超える場合だけ、短いパスを `--session-socket` で指定します。置き場所は `XDG_RUNTIME_DIR`、なければ確認をした `/tmp/host-shell-code-server-<uid>/` です。確認に失敗した場合は、警告を出して既定のパスのまま起動します ([Q-004](#q-004-bl-001-の対処方針)、[Q-005](#q-005-短いパスの置き場所))。
- 確認: BL-001 は、起動のログと、ヘッドレスの Chrome で開いた画面の拡張機能ホストのログで確認します ([Q-006](#q-006-bl-001-の確認方法))。
- 受入条件: [Q-007](#q-007-受入条件) の 1〜10 です。
- 進め方: これまでと同じ運用とし、作業ブランチは `claude/session-socket-ci` です ([Q-008](#q-008-作業ブランチと-pr-とマージの運用))。

## 調査結果

### BL-002: 失敗の原因

作業環境で `go test -count=100 -run 'TestPersistentForwards$' ./internal/forward` を実行し、失敗を再現しました (100 回中 1〜2 回)。失敗のメッセージは次のとおりです。

```text
--- FAIL: TestPersistentForwards (0.76s)
    testing.go:1464: TempDir RemoveAll cleanup: unlinkat /tmp/TestPersistentForwards1657074667/001: directory not empty
```

テスト本体の確認はすべて通っており、失敗はテストの後片付けで起きています。原因は次のとおりです。

1. 接続先の `Target` は、リモート転送の待ち受けごとに、接続が切れたら待ち受けを閉じる処理 (`unlisten`) を別の goroutine で動かします。`unlisten` は待ち受けを閉じた後、操作ログに `forward_unlisten` を書きます。
2. テストの後片付けでは、接続を閉じた直後に `Target.Close` を呼びます。上記の goroutine が先に `unlisten` を始めると、`Close` は閉じる待ち受けがないため、`forward_unlisten` の書き込みを待たずに戻ります。
3. 続いて、テストの一時フォルダーの削除 (`t.TempDir` の後片付け) が始まります。削除の途中で `forward_unlisten` の書き込みが操作ログのファイルを作り直すと、フォルダーが空にならず、削除が失敗します。

作業用の一時フォルダーに複製したリポジトリで、`unlisten` を排他にし、`Close` が実行中の `unlisten` の終了を待つように変えたところ、同じテストを 200 回連続で実行して失敗は 0 回でした。

この競合は実際の運用にも関係します。接続先の host-shell は、終了時に `server_stop` を記録した後で `Target.Close` を呼びます ([cmd/host-shell/main.go](../../cmd/host-shell/main.go))。同じ競合で、`forward_unlisten` の記録がプロセスの終了に間に合わず、失われる場合があります。

### BL-002: CI のログに理由が残らなかった理由

`go test -v` は、後片付けでの失敗を `testing.go:<行>:` の形で出力します。CI の Test の段の絞り込み (`_test\.go` を含む行だけを残す) は、この行を消します。PR #5 の CI (run 41280901078 の 1 回目) のログにも、`--- FAIL: TestPersistentForwards (1.00s)` の行だけが残っていました。

### BL-001: セッション用ソケットの使われ方

code-server 4.144.0 は、`--session-socket` を指定しない場合、Linux では `<--user-data-dir>/code-server-ipc.sock` をセッション用ソケットにします (`cli.js` の `defaultSessionSocket`)。Windows では名前付きパイプ (`\\.\pipe\code-server-ipc-<ハッシュ>`) を使うため、パスの長さの問題は起きません。

- スキルの配置では、ソケットのパスは HOME の後ろに 67 文字が付きます (`/.local/share/host-shell-code-server/user-data/code-server-ipc.sock`)。Linux の Unix ドメイン ソケットのパスの上限は 107 バイトのため、HOME が 41 バイト以上になると作れません。
- ソケットを使うのは、(1) code-server の拡張機能ホストがブラウザーの画面 (セッション) を登録する処理と、(2) VS Code の端末の外から `code-server <ファイル>` を実行して、開いている画面でファイルを開く処理です。
- code-server の内蔵端末から `code-server <ファイル>` を実行した場合は、環境変数 `VSCODE_IPC_HOOK_CLI` の経路を使い、このソケットは使いません (`cli.js` の `shouldOpenInExistingInstance`)。元の残課題一覧の「本来あるべき姿」の例 (内蔵端末から開く操作) は、この調査で影響を受けないことが分かりました。
- (2) を手で行う場合も、`--user-data-dir` または `--session-socket` をスキルと同じに指定する必要があります。スキルの手順は (2) を使いません。
- したがって、ソケットを作れない場合の影響は、起動時の警告 (`Could not create socket at ...`) と、拡張機能ホストのログのエラー (`connect EINVAL ...`) だけです。前案件の確認でも、画面の表示、ログイン、拡張機能の導入に影響はありませんでした。

code-server は `--session-socket <パス>` でソケットの場所を変えられます。指定した値は、子プロセスにも環境変数 `CODE_SERVER_SESSION_SOCKET` で渡ります。

## 観点別の確認

| 観点 | 状態 | 根拠・関連 Q |
| --- | --- | --- |
| 目的・利用者 | 確認済み | [Q-001](#q-001-対応する残課題) |
| 範囲・境界 | 確認済み | [Q-001](#q-001-対応する残課題)、[Q-002](#q-002-bl-002-の修正箇所)、[Q-004](#q-004-bl-001-の対処方針) |
| 入力・根拠 | 確認済み | 上記の参照資料 (コミット e1d8b7c) と本対話の依頼、調査結果 |
| 整合性・依存関係 | 確認済み | [Q-003](#q-003-ci-の-test-の段の絞り込み)、[Q-005](#q-005-短いパスの置き場所)、[Q-008](#q-008-作業ブランチと-pr-とマージの運用) |
| 実現性・例外 | 確認済み | [Q-005](#q-005-短いパスの置き場所)、[Q-006](#q-006-bl-001-の確認方法) |
| 受入条件・検証 | 確認済み | [Q-006](#q-006-bl-001-の確認方法)、[Q-007](#q-007-受入条件) |

## 質問一覧

| Q-ID | 論点 | 状態 | 依存・関連 |
| --- | --- | --- | --- |
| [Q-001](#q-001-対応する残課題) | 対応する残課題 | 解決済み | [Q-002](#q-002-bl-002-の修正箇所)〜[Q-007](#q-007-受入条件) |
| [Q-002](#q-002-bl-002-の修正箇所) | BL-002 の修正箇所 | 解決済み | [Q-001](#q-001-対応する残課題) |
| [Q-003](#q-003-ci-の-test-の段の絞り込み) | CI の Test の段の絞り込み | 解決済み | [Q-001](#q-001-対応する残課題) |
| [Q-004](#q-004-bl-001-の対処方針) | BL-001 の対処方針 | 解決済み | [Q-001](#q-001-対応する残課題)、[Q-005](#q-005-短いパスの置き場所) |
| [Q-005](#q-005-短いパスの置き場所) | 短いパスの置き場所 | 解決済み | [Q-004](#q-004-bl-001-の対処方針) |
| [Q-006](#q-006-bl-001-の確認方法) | BL-001 の確認方法 | 解決済み | [Q-004](#q-004-bl-001-の対処方針)、[Q-007](#q-007-受入条件) |
| [Q-007](#q-007-受入条件) | 受入条件 | 解決済み | [Q-001](#q-001-対応する残課題)〜[Q-006](#q-006-bl-001-の確認方法) |
| [Q-008](#q-008-作業ブランチと-pr-とマージの運用) | 作業ブランチと PR とマージの運用 | 解決済み | なし |

## 質問

### Q-001 対応する残課題

**Status**

解決済み

**Question**

この案件で、前案件の残課題一覧のどの項目に対応しますか？

**Context**

前案件の残課題一覧 (`docs/prompts/past/261101-code-server-followup-backlog.md`) には、次の 2 件があります。

- BL-001: HOME のパスが長い場合に、code-server のセッション用ソケットを作れない。調査の結果、影響は警告とログのエラーだけで、操作への影響はないことが分かりました ([BL-001: セッション用ソケットの使われ方](#bl-001-セッション用ソケットの使われ方))。
- BL-002: CI の ubuntu で `TestPersistentForwards` が断続的に失敗する。原因を特定し、修正の見込みを確認しました ([BL-002: 失敗の原因](#bl-002-失敗の原因))。

対象にしない項目は、元の残課題一覧で「容認」とするか、この案件の残課題一覧に転記するかを、回答の補足で指定してください。

**Answer**

選択方式: 複数選択

- [x] A: (推奨) BL-001 に対応します。
- [x] B: (推奨) BL-002 に対応します。
- [ ] C: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足:

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-01 22:39:54 JST (ファイルの保存日時は 2026-11-01 22:39:36 JST、記入日時は未取得)

**Assessment**

A と B を確認しました。BL-001 と BL-002 の両方に対応します。対象外とする項目はありません。

**Recommendation**

推奨: A と B  
理由: どちらも原因と修正の方法が分かっており、変更は小さいためです。BL-002 は運用での操作ログの欠落にも関係します。BL-001 は影響が小さいため、対象外として容認することもできます。

**Related**

[Q-002](#q-002-bl-002-の修正箇所)〜[Q-007](#q-007-受入条件)

### Q-002 BL-002 の修正箇所

**Status**

解決済み

**Question**

BL-002 の競合を、どこで直しますか？

**Context**

失敗は、`Target.Close` が別の goroutine で実行中の `unlisten` (操作ログ `forward_unlisten` の書き込み) を待たずに戻るために起きます ([BL-002: 失敗の原因](#bl-002-失敗の原因))。直し方は 2 つあります。

A の案 (製品のコード) の概略は次のとおりです。`Close` が戻った時点で、すべての `forward_unlisten` が書き終わっていることを保証します。

```go
func (t *Target) unlisten(id uint64) {
	t.unlistenMu.Lock()
	defer t.unlistenMu.Unlock()
	// ... remove the listener, close it and record forward_unlisten ...
}

func (t *Target) Close() {
	// ... unlisten every remaining listener ...
	// Wait for an unlisten that a closed connection started.
	t.unlistenMu.Lock()
	t.unlistenMu.Unlock()
}
```

B の案 (テストだけ) は、テストの後片付けで `forward_unlisten` が記録されるまで待ちます。製品の動作は変わらず、運用時の終了で記録が失われる場合は残ります。

```go
t.Cleanup(func() {
	bridgeConn.Close(); targetConn.Close(); m.Close(); tg.Close()
	waitAudit(t, logFile, "forward_unlisten") // poll until recorded
})
```

**Answer**

選択方式: 単一選択

- [x] A: (推奨) 製品のコードを直し、`Target.Close` が実行中の `unlisten` の終了を待つようにします。`Close` の後に記録がそろうことを確かめるテストを加えます。
- [ ] B: テストだけを直し、後片付けで記録を待ちます。
- [ ] C: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足:

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-01 22:39:54 JST (ファイルの保存日時は 2026-11-01 22:39:36 JST、記入日時は未取得)

**Assessment**

A を確認しました。`Target.Close` が実行中の `unlisten` の終了を待つように製品のコードを直し、`Close` の後に `forward_unlisten` の記録がそろうことを確かめるテストを加えます。

**Recommendation**

推奨: A  
理由: 原因が製品のコードにあり、運用時の終了で `forward_unlisten` の記録が失われる場合も同時に防げるためです。作業用の複製で、200 回連続の成功を確認しています。

**Related**

[Q-001](#q-001-対応する残課題)、[Q-007](#q-007-受入条件)

### Q-003 CI の Test の段の絞り込み

**Status**

解決済み

**Question**

CI の Test の段の出力の絞り込みを、どのように直しますか？

**Context**

いまの絞り込みは `grep -E '^(---|ok|FAIL|PASS|panic)|_test\.go'` で、テストのファイル (`*_test.go`) の行を含むメッセージだけを残します。後片付けの失敗は `testing.go:<行>:` の形で出力されるため消えました ([BL-002: CI のログに理由が残らなかった理由](#bl-002-ci-のログに理由が残らなかった理由))。`docs/ja/development.md` の CI の説明 (「`-v` の出力から結果行、スキップ行、テスト ログのみを抽出して表示します」) も、変更に合わせて直します。

各案の絞り込みの条件は次のとおりです。

```text
A: grep -E '^(---|ok|FAIL|PASS|panic)|^[[:space:]]+[^[:space:]]+\.go:[0-9]+:'
   字下げされた「ファイル名.go:行:」の行をすべて残します (testing.go も、テスト以外のファイルも)。
B: grep -E '^(---|ok|FAIL|PASS|panic)|_test\.go|testing\.go'
   今回の testing.go だけを足します。
C: 絞り込みをやめ、go test -count=1 ./... (-v なし) にします。
   失敗したテストのメッセージはすべて出ますが、スキップの理由の行は出なくなります。
```

`[[:space:]]` は POSIX の文字クラスで、ubuntu (GNU grep)、macOS (BSD grep)、Windows (Git Bash の GNU grep) のいずれでも使えます。

**Answer**

選択方式: 単一選択

- [x] A: (推奨) 字下げされた「ファイル名.go:行:」の行をすべて残す条件にします。
- [ ] B: `testing\.go` だけを条件に足します。
- [ ] C: 絞り込みをやめ、`-v` なしで実行します。
- [ ] D: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足:

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-01 22:39:54 JST (ファイルの保存日時は 2026-11-01 22:39:36 JST、記入日時は未取得)

**Assessment**

A を確認しました。CI の Test の段の絞り込みを、字下げされた「ファイル名.go:行:」の行をすべて残す条件 (`^[[:space:]]+[^[:space:]]+\.go:[0-9]+:`) にします。`development.md` の CI の説明も合わせて直します。

**Recommendation**

推奨: A  
理由: 失敗の理由が、どのファイルから出力されても残るためです。いまの出力 (結果行、スキップの理由、テストのログ) は変わりません。B は別のファイルからの出力で同じ問題が再び起きます。C はスキップの理由が見えなくなります。

**Related**

[Q-001](#q-001-対応する残課題)、[Q-007](#q-007-受入条件)

### Q-004 BL-001 の対処方針

**Status**

解決済み

**Question**

BL-001 (セッション用ソケットのパスが長すぎる場合) に、どのように対処しますか？

**Context**

影響は、起動時の警告と拡張機能ホストのログのエラーだけです ([BL-001: セッション用ソケットの使われ方](#bl-001-セッション用ソケットの使われ方))。code-server の `--session-socket` で、ソケットを短いパスに置けます。この質問は Linux の接続先だけが対象です (Windows は名前付きパイプのため問題がありません)。

```text
A: start.sh が既定のパス (<HOME>/.local/share/host-shell-code-server/user-data/code-server-ipc.sock)
   の長さを調べ、107 バイトを超える場合だけ、短いパスを --session-socket で指定します。
   通常の HOME では、いまと同じ動作です。
B: 常に短いパスを --session-socket で指定します。
   すべての接続先で、ソケットの場所が HOME の外になります。
C: 動作は変えず、文書の注意点に制約を記載します。
D: 対処せず、仕様の制約として容認します (文書にも記載しません)。
```

**Answer**

選択方式: 単一選択

- [x] A: (推奨) 既定のパスが長すぎる場合だけ、短いパスを指定します。
- [ ] B: 常に短いパスを指定します。
- [ ] C: 動作は変えず、文書に制約を記載します。
- [ ] D: 対処せず、容認します。
- [ ] E: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足:

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-01 22:39:54 JST (ファイルの保存日時は 2026-11-01 22:39:36 JST、記入日時は未取得)

**Assessment**

A を確認しました。Linux の start で、既定のパスが 107 バイトを超える場合だけ、短いパスを `--session-socket` で指定します。通常の HOME の動作は変えません。

**Recommendation**

推奨: A  
理由: 通常の HOME の動作 (ソケットを HOME の配下に置く) を変えずに、警告とエラーをなくせるためです。変更は start.sh の数行です。

**Related**

[Q-001](#q-001-対応する残課題)、[Q-005](#q-005-短いパスの置き場所)、[Q-006](#q-006-bl-001-の確認方法)

### Q-005 短いパスの置き場所

**Status**

解決済み

**Question**

[Q-004](#q-004-bl-001-の対処方針) で短いパスを指定する場合、ソケットをどこに置きますか？

**Context**

候補は次の 2 つです。

- `XDG_RUNTIME_DIR` (例: `/run/user/1000`): ログインのセッションごとに用意される、利用者だけが使えるフォルダーです。systemd のある環境の SSH のログインでは通常設定されます。`podman exec` で入るコンテナーや、systemd のない環境では設定されない場合があります。
- `/tmp/host-shell-code-server-<uid>/`: どの環境にもあります。ほかの利用者に先に作られる恐れがあるため、host-shell-connect の `deploy.sh` が `/tmp/host-shell-<uid>/` に行っているのと同じ確認 (シンボリック リンクでない、自分か root の所有、グループとその他が書き込めない) が必要です。

`docs/ja/code-server.md` の「接続先の配置」の表には、短いパスを使う場合の場所を注記します。

```text
A: XDG_RUNTIME_DIR があれば ${XDG_RUNTIME_DIR}/host-shell-code-server-ipc.sock、
   なければ /tmp/host-shell-code-server-<uid>/ipc.sock (フォルダーは mkdir -m 700 で作り、上記の確認をします)。
   確認に失敗した場合は、警告を出して既定のパスのまま起動します (いまと同じ動作)。
B: XDG_RUNTIME_DIR があればそこに置き、なければ既定のパスのまま起動します (/tmp は使いません)。
C: 常に /tmp/host-shell-code-server-<uid>/ipc.sock に置きます (上記の確認をします)。
```

**Answer**

選択方式: 単一選択

- [x] A: (推奨) `XDG_RUNTIME_DIR`、なければ `/tmp/host-shell-code-server-<uid>/` に置きます。
- [ ] B: `XDG_RUNTIME_DIR` だけを使います。
- [ ] C: 常に `/tmp/host-shell-code-server-<uid>/` に置きます。
- [ ] D: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足:

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-01 22:39:54 JST (ファイルの保存日時は 2026-11-01 22:39:36 JST、記入日時は未取得)

**Assessment**

A を確認しました。`XDG_RUNTIME_DIR` があればそこに、なければ `/tmp/host-shell-code-server-<uid>/` に置きます。`/tmp` のフォルダーは `deploy.sh` と同じ確認をし、確認に失敗した場合は警告を出して既定のパスのまま起動します。[Q-004](#q-004-bl-001-の対処方針) の A と整合します。

**Recommendation**

推奨: A  
理由: 利用者専用の `XDG_RUNTIME_DIR` を優先し、それがないコンテナーなどでも警告を解消できるためです。`/tmp` の利用と確認の方法は、host-shell の配置と同じ考え方です。

**Related**

[Q-004](#q-004-bl-001-の対処方針)

### Q-006 BL-001 の確認方法

**Status**

解決済み

**Question**

BL-001 の対処を、どこまで確認しますか？

**Context**

作業環境の Linux で、HOME を一時フォルダーに向けて code-server を起動して確認できます (前案件と同じ方法)。HOME の長さを変えれば、長い場合と通常の場合の両方を確認できます。拡張機能ホストのログのエラーは、ブラウザーで画面を開いた後に出るため、確認にはヘッドレスの Chrome (前案件の確認に使ったもの) が必要です。

- 起動のログ: code-server は、ソケットを作れた場合に `Session server listening on <パス>` を、作れない場合に `Could not create socket at <パス>` を出力します。
- 拡張機能ホストのログ: ソケットに接続できない場合、`connect EINVAL ... code-server-ipc.sock` が記録されます。

**Answer**

選択方式: 単一選択

- [x] A: (推奨) 起動のログに加え、ヘッドレスの Chrome で画面を開き、拡張機能ホストのログにエラーがないことまで確認します。
- [ ] B: 起動のログだけを確認します。
- [ ] C: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足:

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-01 22:39:54 JST (ファイルの保存日時は 2026-11-01 22:39:36 JST、記入日時は未取得)

**Assessment**

A を確認しました。起動のログに加え、ヘッドレスの Chrome で画面を開き、拡張機能ホストのログにエラーがないことまで確認します。

**Recommendation**

推奨: A  
理由: 残課題で記録したエラー (拡張機能ホストのログ) が消えたことを、直接確かめられるためです。

**Related**

[Q-004](#q-004-bl-001-の対処方針)、[Q-007](#q-007-受入条件)

### Q-007 受入条件

**Status**

解決済み

**Question**

次の受入条件で、成果物を受け入れますか？

**Context**

推奨の回答 ([Q-001](#q-001-対応する残課題) A と B、[Q-002](#q-002-bl-002-の修正箇所) A、[Q-003](#q-003-ci-の-test-の段の絞り込み) A、[Q-004](#q-004-bl-001-の対処方針) A、[Q-005](#q-005-短いパスの置き場所) A、[Q-006](#q-006-bl-001-の確認方法) A) を前提にした案です。ほかの選択肢を選んだ場合は、合わせて直します。

1. `Target.Close` が戻った時点で、すべての待ち受けの `forward_unlisten` が操作ログに記録されていることを、新しいテストで確認します。
2. 作業環境で `go test -count=200 -run 'TestPersistentForwards$' ./internal/forward` を実行し、失敗が 0 回です (修正前は 100 回中 1〜2 回)。
3. CI の新しい絞り込みが、`testing.go:<行>:` の行 (修正前のテストの出力を保存したもの) と、いまの結果行、スキップの理由、テストのログを残すことを、手元で確認します。
4. 長い HOME (既定のパスが 107 バイトを超える) の Linux で、start が短いパスを指定し、起動のログに `Session server listening on <短いパス>` が出て、`Could not create socket` が出ません。`XDG_RUNTIME_DIR` がある場合とない場合の両方で確認します。
5. 4 の状態でヘッドレスの Chrome で画面を開き、拡張機能ホストのログに `code-server-ipc.sock` のエラーがありません。
6. 通常の HOME では、`--session-socket` を指定せず、ソケットは従来どおりユーザー データのフォルダーにできます。
7. `/tmp/host-shell-code-server-<uid>/` がほかの利用者の所有、またはグループかその他が書き込める状態のとき、警告を出して既定のパスのまま起動します。
8. `make` (vet、テスト、ビルド) が成功し、全 OS の CI が成功します。
9. 日英の文書 (`development.md` の CI の説明、`code-server.md` の配置の表) が変更に合っています。
10. 元の残課題一覧 (`261101-code-server-followup-backlog.md`) の BL-001 と BL-002 に、状態と経緯と対処を記録します。

**Answer**

選択方式: 単一選択

- [x] A: (推奨) 上記の 1〜10 で受け入れます。
- [ ] B: その他 (追加・削除する条件を補足に記載)
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足:

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-01 22:39:54 JST (ファイルの保存日時は 2026-11-01 22:39:36 JST、記入日時は未取得)

**Assessment**

A を確認しました。受入条件は 1〜10 です。前提とした回答 ([Q-001](#q-001-対応する残課題)〜[Q-006](#q-006-bl-001-の確認方法)) はすべて推奨どおりのため、条件の修正は不要です。

**Recommendation**

推奨: A  
理由: 2 件の残課題の事象 (テストの失敗、警告とログのエラー) が消えたことと、通常の場合の動作が変わらないことを、それぞれ確かめられるためです。

**Related**

[Q-001](#q-001-対応する残課題)〜[Q-006](#q-006-bl-001-の確認方法)

### Q-008 作業ブランチと PR とマージの運用

**Status**

解決済み

**Question**

作業ブランチ、PR、マージをどのように運用しますか？

**Context**

これまでの案件 (261031-forward-followup、261101-code-server、261101-code-server-followup) では、次の運用でした。

```text
- main から作業ブランチを作り、main 向けのドラフト PR を作成します。
- 受入条件をすべて満たし、全 OS の CI が成功した時点で、第 3 ゲートを待たずにエージェントがマージ コミットでマージします。
- マージ前に本件の検討記録を docs/prompts/past/ へ移します。
- 第 3 ゲートの承認の記録は、承認後に main へ直接コミット・プッシュします。
```

**Answer**

選択方式: 単一選択

- [x] A: (推奨) これまでと同じ運用とし、作業ブランチは `claude/session-socket-ci` とします。
- [ ] B: これまでと同じ運用だが、マージは第 3 ゲートの承認後とします。
- [ ] C: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足:

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-01 22:39:54 JST (ファイルの保存日時は 2026-11-01 22:39:36 JST、記入日時は未取得)

**Assessment**

A を確認しました。これまでと同じ運用とし、作業ブランチは `claude/session-socket-ci` です。

**Recommendation**

推奨: A  
理由: これまでの 3 件と同じ運用で、記録の場所と時機がそろうためです。
