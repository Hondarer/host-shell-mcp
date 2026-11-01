<!--
生成元: agree-before-implement スキル
文書種別: 最終仕様 (design)
最終更新エージェント: claude code / セッション ID: session_01KJc53UTLFzqXyXoZpjPqCE
上流文書: 261101-socks-linger-close-discussion.md (検討用 Markdown)。削除されている場合があります。
下流文書: 261101-socks-linger-close-backlog.md (残課題一覧)。未作成の場合や、削除されている場合があります。
再開手順: このファイルを開いたエージェントは、作業の前に /home/user/c-modernization-kit/app/general/.agents/skills/agree-before-implement/SKILL.md を読み込んでください。上流文書が存在する場合は、上流文書の再開手順に従って進行位置を確認し、判断の経緯を上流文書の Q&A で確認します。存在しない場合は、この文書を単体の最終仕様として扱います。最終更新エージェントのセッション情報が参照可能であれば再開時のコンテキストとして利用し、別エージェントからの引き継ぎや担当変更によりセッションが見つからない場合も異常とせず、文書の記載から継続してください。
-->

# SOCKS の拒否の応答の後の閉じ方 最終仕様

検討 ID: 261101-socks-linger-close

版: 1

状態: 承認済み

## 1. 目的と範囲

前案件 261101-session-socket-ci の残課題一覧 (`docs/prompts/past/261101-session-socket-ci-backlog.md`) の BL-001「Windows で、SOCKS4 の拒否の応答がクライアントに届かない場合がある」に対応します。

bridge の SOCKS の処理 (`internal/forward/socks.go` の `Manager.socks`) が拒否の応答を書いた後、クライアントが OS にかかわらず応答を受け取れるようにします。`docs/*/port-forward.md` の「BIND、UDP ASSOCIATE、SOCKS4/4a はエラー応答を返します」という記載どおりの動作になります。

## 2. 原因

`Manager.socks` は、要求の一部だけを読んで拒否の応答を書き、そのまま接続を閉じる経路を持ちます。受信して読んでいないデータが残った接続を閉じると、OS は FIN ではなく RST を送ります。Windows のクライアントは、RST を受けると未読の受信データ (拒否の応答) を捨てます。

必ず読み残しが出る経路は、SOCKS4/4a の要求 (先頭の 2 バイトだけを読む) と、SOCKS5 の未対応のアドレス種別 (アドレスと port を読まない) です。ほかの拒否の経路でも、クライアントが応答を待たずにデータを送った場合は同じことが起きます。

## 3. 閉じ方 (lingering close)

### 3.1. 手順

拒否の応答を書いた後、次の順に接続を閉じます。

1. 書き込み側を閉じます (`CloseWrite`)。クライアントには、応答の後に FIN が届きます。
2. 読み取りの期限を 1 秒後にし、残りの受信データを最大 64 KiB まで読み捨てます。クライアントが閉じる (EOF)、期限に達する、量の上限に達する、のいずれかで終わります。
3. 接続を閉じます。

読み残しを読み捨ててから閉じるため、RST は送られません。通常のクライアントは応答と FIN を受けて接続を閉じるため、2 はすぐに終わります。期限と量の上限は、閉じないクライアントや送り続けるクライアントのために、拒否した接続を保持する時間と処理の量を区切るものです。量の上限に達した場合は、読み残しがあっても閉じます。

### 3.2. 実装

- `internal/forward/socks.go` に、3.1 の手順を行う関数 `lingerClose(c net.Conn)` を加えます。期限 (1 秒) と量の上限 (64 KiB) は名前付きの定数にします。関数のコメントに、Windows で応答が捨てられるのを防ぐ目的を書きます。
- `Manager.socks` で応答を書いて閉じるすべての経路で、応答を書いた後に `lingerClose` を呼びます。

| 経路 | 応答 |
| --- | --- |
| SOCKS4/4a の要求 | `0x5b` (拒否) |
| 認証なしの方式を含まない | `0xff` |
| 未対応のアドレス種別 | `0x08` |
| 未対応のコマンド (BIND、UDP ASSOCIATE) | `0x07` |
| 宛先の形式の誤り | `0x01` |
| `--forward-allow-local` による拒否 | `0x02` |
| 接続先での失敗 (拒否、到達不能、時間切れなど) | 各コード |
| 接続数の上限 | `0x01` |

Table: lingerClose を適用する経路

- 応答を書かずに閉じる経路 (未知の版、要求の読み取りの失敗) と、成功して転送を始めた接続は変えません。
- 呼び出し元 (`internal/forward/bridge.go`) の `defer c.Close()` は残します。2 回目の `Close` は何もしません。

## 4. テスト (`internal/forward/forward_test.go`)

| テスト | 内容 |
| --- | --- |
| `TestSOCKSRejectReplyDelivered` (新規) | SOCKS4 の要求と、未対応のアドレス種別の SOCKS5 の要求のそれぞれで、要求を送った後に 100 ミリ秒待ってから応答を読み、拒否のコード (`0x5b`、`0x08`) を受け取り、続いて EOF になることを確かめます。修正前のコードでは、Windows で接続のリセットにより失敗します |
| `TestLingerClose` (新規) | 接続した TCP の組で、`lingerClose` が、相手が閉じない場合は期限 (約 1 秒) の後に戻ること、相手がデータを送り続ける場合は量の上限で期限より前に戻ることを確かめます |

Table: 追加するテスト

既存の `TestSOCKS` は変更しません。

## 5. 確認方法

- 作業環境の Linux で `make` (vet、テスト、ビルド) を実行します。
- テストを Windows 向けに作り (`GOOS=windows go test -c`)、作業環境の Windows で実行します。
    - `TestSOCKSRejectReplyDelivered` が、修正前の `socks.go` では失敗し、修正後は成功することを確かめます。
    - `TestSOCKS` と `TestSOCKSRejectReplyDelivered` を各 300 回繰り返し、失敗が 0 回であることを確かめます。
- main 向けの PR で、全 OS の CI が成功することを確かめます。

## 6. 元の残課題一覧の更新

`docs/prompts/past/261101-session-socket-ci-backlog.md` の項目と記載は削除しません。BL-001 の状態を「解消 (2026-11-01、261101-socks-linger-close)」にし、「経緯と対処」の節を加えます。一覧表の状態の列と冒頭の説明も合わせて更新します。

## 7. 知見の反映計画

| 知見 | 反映先 |
| --- | --- |
| 読み残しのある接続を閉じると RST になり、Windows では未読の応答が捨てられるため、拒否の応答の後は lingering close で閉じること | `internal/forward/socks.go` の `lingerClose` のコメント |

Table: 知見の反映計画

`docs/*/port-forward.md` は、記載どおりの動作になるため変更しません。

## 8. 作業の進め方

- main から作業ブランチ `claude/socks-linger-close` を作り、最初のコミットに検討用 Markdown と最終仕様を含めます。main 向けのドラフト PR を作成します。
- 受入条件をすべて満たし、全 OS の CI が成功した時点で、第 3 ゲートを待たずにマージ コミットでマージします。マージ前に本件の検討記録を `docs/prompts/past/` へ移します。
- 第 3 ゲートの承認の記録は、承認後に main へ直接コミット・プッシュします。

## 9. 受入条件

1. 新しいテストを Windows 向けに作り、作業環境の Windows で、修正前のコードでは失敗し、修正後のコードでは成功することを確認します。
2. 作業環境の Windows で、`TestSOCKS` と新しいテストを繰り返し実行 (各 300 回) して、失敗が 0 回です。
3. 拒否した接続が、クライアントが閉じない場合でも、期限 (1 秒) の後に閉じられることをテストで確認します。
4. `make` (vet、テスト、ビルド) が成功し、全 OS の CI が成功します。
5. 元の残課題一覧 (`261101-session-socket-ci-backlog.md`) の BL-001 に、状態と経緯と対処を記録します。
