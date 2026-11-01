<!--
生成元: agree-before-implement スキル
文書種別: 最終仕様 (design)
最終更新エージェント: claude code / セッション ID: session_01KJc53UTLFzqXyXoZpjPqCE
上流文書: 261101-session-socket-ci-discussion.md (検討用 Markdown)。削除されている場合があります。
下流文書: 261101-session-socket-ci-backlog.md (残課題一覧)。未作成の場合や、削除されている場合があります。
再開手順: このファイルを開いたエージェントは、作業の前に /home/user/c-modernization-kit/app/general/.agents/skills/agree-before-implement/SKILL.md を読み込んでください。上流文書が存在する場合は、上流文書の再開手順に従って進行位置を確認し、判断の経緯を上流文書の Q&A で確認します。存在しない場合は、この文書を単体の最終仕様として扱います。最終更新エージェントのセッション情報が参照可能であれば再開時のコンテキストとして利用し、別エージェントからの引き継ぎや担当変更によりセッションが見つからない場合も異常とせず、文書の記載から継続してください。
-->

# セッション用ソケットと転送の後片付けの修正 最終仕様

検討 ID: 261101-session-socket-ci

版: 1

状態: 承認済み

## 1. 目的と範囲

前案件 261101-code-server-followup の残課題一覧 (`docs/prompts/past/261101-code-server-followup-backlog.md`) の 2 件に対応します。

| 課題 ID | 題名 | 対応 |
| --- | --- | --- |
| BL-001 | HOME のパスが長い場合に、code-server のセッション用ソケットを作れない | 既定のパスが長すぎる場合だけ、ソケットを短いパスに置く (3 章) |
| BL-002 | CI の ubuntu で TestPersistentForwards が断続的に失敗する | 接続先の転送の後片付けの競合を直し、CI のログに失敗の理由が残るようにする (2 章) |

Table: 対応する残課題

## 2. 転送の後片付け (BL-002)

### 2.1. 事象と原因

接続先の `Target` (`internal/forward/target.go`) は、リモート転送の待ち受けごとに、チャネルの接続が切れたら待ち受けを閉じる goroutine を動かします。この goroutine と `Target.Close` は、どちらも `unlisten` を呼びます。`unlisten` は待ち受けを一覧から外し、閉じ、操作ログに `forward_unlisten` を記録します。

goroutine が先に待ち受けを一覧から外すと、`Close` は閉じるものがないため、goroutine の記録を待たずに戻ります。テストでは、この後に始まる一時フォルダーの削除と記録の書き込みが重なり、削除が失敗します (`TempDir RemoveAll cleanup: ... directory not empty`)。運用では、接続先の host-shell の終了処理で `Close` の後にプロセスが終わるため、`forward_unlisten` の記録が失われる場合があります。

### 2.2. `Target` の変更 (`internal/forward/target.go`)

`Close` が戻った時点で、すべての待ち受けの `forward_unlisten` が記録済みであることを保証します。

- `Target` に、`unlisten` を直列にする排他 (`unlistenMu sync.Mutex`) を加えます。
- `unlisten` は、一覧から外す、閉じる、記録する、の全体をこの排他の中で行います。
- `Close` は、残っている待ち受けをすべて `unlisten` した後、この排他を一度取得して解放します。これにより、goroutine が実行中の `unlisten` の終了を待ちます。
- goroutine が `unlisten` を始める前に `Close` が一覧を処理した場合、goroutine の `unlisten` は対象がなく、何も記録しません。

`Close` の説明のコメントに、戻った時点で `forward_unlisten` がすべて記録済みであることを書きます。

### 2.3. テスト (`internal/forward/forward_test.go`)

新しいテストで、チャネルの接続を閉じた直後に `Target.Close` を呼び、`Close` が戻った時点で、操作ログに待ち受けの数だけ `forward_unlisten` があることを確かめます。競合の起きる時機をずらすため、同じ確認を複数回 (20 回) 繰り返します。

`TestPersistentForwards` と、テスト用の接続を作る `start` は変更しません。

### 2.4. CI の Test の段 (`.github/workflows/ci.yml`)

`go test -v` の出力の絞り込みを、次の条件に変えます。

```sh
go test -count=1 -v ./... 2>&1 | grep -E '^(---|ok|FAIL|PASS|panic)|^[[:space:]]+[^[:space:]]+\.go:[0-9]+:'
```

結果行に加え、字下げされた「ファイル名.go:行:」の行をすべて残します。テストのファイルからの失敗の理由、スキップの理由、テストのログに加え、`testing.go` (後片付けの失敗など) やテスト以外のファイルからの出力も残ります。`[[:space:]]` は POSIX の文字クラスで、ubuntu、macOS、Windows (Git Bash) の grep のいずれでも使えます。ステップの前のコメントも合わせて直します。

## 3. セッション用ソケット (BL-001)

### 3.1. 事象と影響

code-server は、`--session-socket` を指定しない場合、Linux では `<--user-data-dir>/code-server-ipc.sock` をセッション用ソケットにします。スキルの配置では、このパスは HOME の後ろに 67 文字が付くため、HOME が 41 バイト以上になると、Unix ドメイン ソケットのパスの上限 (107 バイト) を超えます。この場合、起動時に `Could not create socket at ...` の警告が出て、拡張機能ホストのログに `connect EINVAL ... code-server-ipc.sock` が記録されます。

> [!NOTE]
> セッション用ソケットは、拡張機能ホストがブラウザーの画面を登録する処理と、VS Code の端末の外から `code-server <ファイル>` で開いている画面にファイルを開く処理に使われます。code-server の内蔵端末からの `code-server <ファイル>` は、環境変数 `VSCODE_IPC_HOOK_CLI` の経路を使うため、このソケットに依存しません。ソケットを作れない場合も、画面の表示、ログイン、拡張機能の導入には影響しません。

Windows の接続先は名前付きパイプを使うため、対象外です。`scripts/windows/` は変更しません。

### 3.2. start の変更 (`.agents/skills/host-shell-code-server/scripts/linux/start.sh`)

1. 既定のパス `${HSCS_USER_DATA_DIR}/code-server-ipc.sock` のバイト数を数えます。107 バイト以下であれば、`--session-socket` を指定しません (従来と同じ動作)。
2. 107 バイトを超える場合は、次の順に短いパスを決めます。
    1. `XDG_RUNTIME_DIR` が設定され、ディレクトリーであれば、`${XDG_RUNTIME_DIR}/host-shell-code-server-ipc.sock` とします。
    2. そうでなければ、`/tmp/host-shell-code-server-<uid>/ipc.sock` とします。フォルダーがなければ `mkdir -m 700` で作ります。フォルダーは、シンボリック リンクでないこと、自分か root の所有であること、グループとその他が書き込めないことを確かめます (host-shell-connect の `deploy.sh` が `/tmp/host-shell-<uid>/` に行う確認と同じ規則です)。
3. 短いパスを決められた場合は、`--session-socket <短いパス>` を付けて起動し、`session socket: <短いパス> (the default path exceeds the Unix socket limit)` を出力します。
4. フォルダーを作れない場合や確認に失敗した場合は、`warning: ...` で理由を出力し、`--session-socket` を付けずに起動します (従来と同じ動作で、code-server の警告が出ます)。start は止まりません。

code-server は、待ち受けの前に同じパスの古いソケットを削除するため、前回の起動で残ったソケットは問題になりません。`/tmp/host-shell-code-server-<uid>/` のフォルダーは、終了後も残します (uninstall の `--purge` の対象にもしません)。

スクリプトの冒頭のコメントに、ソケットの場所の決め方と理由 (Unix ドメイン ソケットのパスの上限) を書きます。

### 3.3. 文書 (`docs/ja/code-server.md`、`docs/en/code-server.md`)

「接続先の配置」の表に、セッション用ソケットの行を加えます。

| 用途 | Linux | Windows |
| --- | --- | --- |
| セッション用ソケット | ユーザー データの中の `code-server-ipc.sock`。パスが 107 バイトを超える場合は `$XDG_RUNTIME_DIR/host-shell-code-server-ipc.sock`、なければ `/tmp/host-shell-code-server-<uid>/ipc.sock` | 名前付きパイプ (ファイルを作りません) |

Table: 加える行

## 4. 正本文書の更新

| 文書 | 変更 |
| --- | --- |
| `docs/ja/development.md`、`docs/en/development.md` | CI の節の「`-v` の出力から結果行、スキップ行、テスト ログのみを抽出して表示します」を、結果行と、字下げされた「ファイル名.go:行:」の行 (失敗とスキップの理由、テストのログ) を残す説明に改めます |
| `docs/ja/code-server.md`、`docs/en/code-server.md` | 3.3 のとおり、配置の表にセッション用ソケットの行を加えます |

Table: 正本文書の更新

日本語版を正本とし、英語版はその翻訳とします。

## 5. 確認方法

- BL-002 の修正: 作業環境で `go test -count=200 -run 'TestPersistentForwards$' ./internal/forward` を実行します (修正前は 100 回中 1〜2 回失敗)。2.3 の新しいテストも実行します。
- CI の絞り込み: 修正前のテストの出力 (`testing.go:1464: TempDir RemoveAll cleanup: ...` を含むもの) と、いまの `go test -v ./...` の出力に新しい条件を適用し、失敗の理由、結果行、スキップの理由、テストのログが残ることを確かめます。
- BL-001: 作業環境の Linux で、HOME を一時フォルダーに向けて install と start を実行します。
    - HOME を長くした場合 (既定のパスが 107 バイトを超える) を、`XDG_RUNTIME_DIR` がある場合とない場合の両方で確認します。起動のログに `Session server listening on <短いパス>` が出て、`Could not create socket` が出ないことを確かめます。
    - 上記の状態でヘッドレスの Chrome で画面を開いてログインし、拡張機能ホストのログに `code-server-ipc.sock` のエラーがないことを確かめます。
    - 通常の長さの HOME では、`--session-socket` を付けず、ソケットがユーザー データのフォルダーにできることを確かめます。
    - `/tmp/host-shell-code-server-<uid>/` をグループが書き込める状態にした場合に、警告を出して既定のパスのまま起動することを確かめます。
- 全体: `make` (vet、テスト、ビルド) と、全 OS の CI。

確認で作った一時フォルダー、`/tmp/host-shell-code-server-<uid>/`、起動したプロセスは、確認後に削除します。

## 6. 元の残課題一覧の更新

`docs/prompts/past/261101-code-server-followup-backlog.md` の項目と記載は削除しません。

- BL-001、BL-002 の状態を「解消 (2026-11-01、261101-session-socket-ci)」にし、それぞれに「経緯と対処」の節を加えます。BL-001 では、内蔵端末からの操作はこのソケットに依存しないことが分かった点も記録します。
- 一覧表の状態の列と、冒頭の説明を合わせて更新します。

## 7. 知見の反映計画

| 知見 | 反映先 |
| --- | --- |
| `Target.Close` は、戻った時点で `forward_unlisten` がすべて記録済みであること | `internal/forward/target.go` の `Close` のコメント |
| CI の絞り込みは「ファイル名.go:行:」の行をすべて残すこと | `.github/workflows/ci.yml` のコメント、`development.md` の CI の節 |
| セッション用ソケットの場所の決め方と、Unix ドメイン ソケットのパスの上限 | `start.sh` のコメント、`code-server.md` の配置の表 |

Table: 知見の反映計画

## 8. 作業の進め方

- main から作業ブランチ `claude/session-socket-ci` を作り、最初のコミットに検討用 Markdown と最終仕様を含めます。main 向けのドラフト PR を作成します。
- 受入条件をすべて満たし、全 OS の CI が成功した時点で、第 3 ゲートを待たずにマージ コミットでマージします。マージ前に本件の検討記録を `docs/prompts/past/` へ移します。
- 第 3 ゲートの承認の記録は、承認後に main へ直接コミット・プッシュします。

## 9. 受入条件

1. `Target.Close` が戻った時点で、すべての待ち受けの `forward_unlisten` が操作ログに記録されていることを、新しいテストで確認します。
2. 作業環境で `go test -count=200 -run 'TestPersistentForwards$' ./internal/forward` を実行し、失敗が 0 回です。
3. CI の新しい絞り込みが、`testing.go:<行>:` の行 (修正前のテストの出力を保存したもの) と、いまの結果行、スキップの理由、テストのログを残すことを、手元で確認します。
4. 長い HOME (既定のパスが 107 バイトを超える) の Linux で、start が短いパスを指定し、起動のログに `Session server listening on <短いパス>` が出て、`Could not create socket` が出ません。`XDG_RUNTIME_DIR` がある場合とない場合の両方で確認します。
5. 4 の状態でヘッドレスの Chrome で画面を開き、拡張機能ホストのログに `code-server-ipc.sock` のエラーがありません。
6. 通常の HOME では、`--session-socket` を指定せず、ソケットは従来どおりユーザー データのフォルダーにできます。
7. `/tmp/host-shell-code-server-<uid>/` がほかの利用者の所有、またはグループかその他が書き込める状態のとき、警告を出して既定のパスのまま起動します。
8. `make` (vet、テスト、ビルド) が成功し、全 OS の CI が成功します。
9. 日英の文書 (`development.md` の CI の説明、`code-server.md` の配置の表) が変更に合っています。
10. 元の残課題一覧 (`261101-code-server-followup-backlog.md`) の BL-001 と BL-002 に、状態と経緯と対処を記録します。
