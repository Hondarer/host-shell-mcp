<!--
生成元: agree-before-implement スキル
文書種別: 最終仕様 (design)
最終更新エージェント: claude code / セッション ID: session_01KJc53UTLFzqXyXoZpjPqCE
上流文書: 261031-forward-followup-discussion.md (検討用 Markdown)。削除されている場合があります。
下流文書: 261031-forward-followup-backlog.md (残課題一覧)。未作成の場合や、削除されている場合があります。
再開手順: このファイルを開いたエージェントは、作業の前に /home/user/c-modernization-kit/app/general/.agents/skills/agree-before-implement/SKILL.md を読み込んでください。上流文書が存在する場合は、上流文書の再開手順に従って進行位置を確認し、判断の経緯を上流文書の Q&A で確認します。存在しない場合は、この文書を単体の最終仕様として扱います。最終更新エージェントのセッション情報が参照可能であれば再開時のコンテキストとして利用し、別エージェントからの引き継ぎや担当変更によりセッションが見つからない場合も異常とせず、文書の記載から継続してください。
-->

# ポート転送の残課題への対応 最終仕様

検討 ID: 261031-forward-followup

版: 1

状態: 承認済み

## 1. 目的と範囲

ポート転送 (案件 261031-port-forward) の残課題一覧の 4 件に対応します。

| 課題 ID | 題名 | 扱い |
| --- | --- | --- |
| BL-001 | Unix ドメイン ソケットと UDP の転送 | 対応しません。TCP だけを転送する現仕様を容認します |
| BL-002 | 接続先で待ち受けるポート番号の許可範囲 | 接続先のオプション `--allow-forward-remote-port` を追加します ([2.](#2-bl-002-接続先の待ち受けポートの許可範囲)) |
| BL-003 | HTTP プロキシの接続の接続先監査ログへの記録 | 接続先の監査ログに `proxy_connect` と `proxy_auth_failed` を記録します ([3.](#3-bl-003-http-プロキシの接続先監査ログ)) |
| BL-004 | CI で TestPTYScreen と TestJobStdin が断続的に失敗する | job_start の初期入力の順序を修正し、TestPTYScreen を `top` に依存しない確認に改めます ([4.](#4-bl-004-ci-の断続的な失敗)) |

Table: 対象の残課題と扱い

> [!NOTE]
> BL-001 の容認は、正本文書 (`docs/ja/port-forward.md` の「対象は TCP だけです」) にすでに記載されています。文書の変更は不要です。

## 2. BL-002 接続先の待ち受けポートの許可範囲

ssh の `PermitListen` に相当する制限を、接続先の管理者が設定できるようにします。

### 2.1. オプション

| オプション | 値 | 内容 |
| --- | --- | --- |
| `--allow-forward-remote-port` | `PORT` または `PORT-PORT` | リモート転送で接続先が待ち受けるポート番号を、指定した範囲に限ります。複数指定とカンマ区切りができます |

Table: 接続先の追加オプション

```sh
host-shell --mcp --allow-forward-remote --allow-forward-remote-port 15432,20000-20099
```

- ポート番号は 1〜65535 です。範囲は `開始-終了` で、開始 ≤ 終了とします。書式が誤っている場合は起動エラーです。
- `--allow-forward-remote` なしで指定した場合は起動エラーです (`--allow-forward-remote-port requires --allow-forward-remote`)。許可は `--allow-forward-remote` だけで行い、`--allow-forward-remote-port` は制限だけを表します。
- 未指定時は、現行どおり待ち受けるポート番号を制限しません。
- `--allow-forward-remote` の書式 (値を取らない) は変えません。

### 2.2. 待ち受けの判定

bridge からの待ち受けの要求 (常設の `--forward-remote` と forward_open の両方) に、許可範囲を次のとおり適用します。

| 要求するポート | 許可範囲あり | 許可範囲なし |
| --- | --- | --- |
| 範囲内のポート | そのポートで待ち受けます | そのポートで待ち受けます (現行どおり) |
| 範囲外のポート | `not_allowed` で拒否します | そのポートで待ち受けます (現行どおり) |
| 0 (自動割当) | 範囲内の空きポートで待ち受けます | OS が選ぶ空きポートで待ち受けます (現行どおり) |

Table: 待ち受けポートの判定

ポート 0 の要求には、指定された順の範囲ごとに、小さい番号から順に待ち受けを試します。ポートが使用中 (`address_in_use`) の場合は次の番号を試します。使用中以外の理由で失敗した場合は、その時点で失敗とします。範囲内のすべてのポートが使用中の場合は `address_in_use` で失敗します。試行は待ち受けの失敗だけで済み、外部との通信は生じません。

### 2.3. エラーの表示

bridge は接続先のオプション値を受け取らないため、エラーには範囲そのものではなく確認先を示します。

```text
forward_open:
  listen on target 127.0.0.1:8080: listen port not allowed by target (see sys_get_info restrictions)
常設の --forward-remote (起動エラー):
  --forward-remote on target 127.0.0.1:8080: listen failed: listen port not allowed by target (see sys_get_info restrictions)
```

`not_allowed` 以外の待ち受けの失敗の表示は変えません。

### 2.4. sys_get_info と監査ログ

- sys_get_info の `restrictions` に、許可範囲を指定の順で 1 件示します。未指定時は示しません。

  ```text
  Allowed remote forward ports: 15432, 20000-20099
  ```

- 範囲外のポートを拒否した場合は、接続先の監査ログに `forward_listen` を `result: "denied"`、`reason: "listen port not allowed"` で記録します。待ち受けの成功と、ポート 0 の要求で範囲内に空きがない場合を含むその他の失敗の記録は、現行の `forward_listen` と同じです。

## 3. BL-003 HTTP プロキシの接続先監査ログ

接続先の管理者が、`--client-proxy` の利用状況を接続先の監査ログで確認できるようにします。

### 3.1. 接続の記録 (proxy_connect)

接続先の HTTP プロキシが bridge へストリームを開くたびに (HTTP の要求 1 件、または CONNECT 1 件ごと)、`proxy_connect` を 1 件記録します。接続先の HTTP プロキシは要求ごとに 1 本のストリームを開く (keep-alive を使わない) ため、要求ごとの記録とストリームごとの記録は同じです。

```json
{"msg": "proxy_connect", "kind": "connect", "host": "packages.example.com", "port": 443, "result": "allowed", "reason": "connection established"}
{"msg": "proxy_connect", "kind": "http", "host": "denied.example", "port": 80, "result": "denied", "reason": "destination not allowed"}
```

| 項目 | 内容 |
| --- | --- |
| `kind` | `http` (通常の HTTP の要求) または `connect` (CONNECT) |
| `host`、`port` | 要求された宛先 |
| `result`、`reason` | ストリームを開いた結果 (次の表) |

Table: proxy_connect の項目

| ストリームを開いた結果 | `result` | `reason` |
| --- | --- | --- |
| 確立 | `allowed` | `connection established` |
| `not_allowed` (bridge の宛先制限) | `denied` | `destination not allowed` |
| `limit` (接続先または bridge の同時接続の上限) | `denied` | `connection limit reached` |
| `timeout` (確立待ち 30 秒の超過を含む) | `failed` | `connection timeout` |
| 上記以外 | `failed` | `upstream connection failed` |

Table: proxy_connect の result と reason

- URL のパスとクエリー、ヘッダー、本文、資格情報は記録しません。
- ストリームの確立後に bridge 側の上流への接続が失敗して HTTP 502 が返る場合、接続先からは通常の応答と区別できません。この場合は確立時の結果 (`allowed`) だけを記録します。
- ストリームを開く前に接続先が自ら拒否する要求 (宛先の書式の誤りによる HTTP 400 など) と、接続先の待ち受けの段階で上限により受け付けなかった接続は、ストリームを開かないため記録しません。

### 3.2. 認証失敗の記録 (proxy_auth_failed)

接続先の HTTP プロキシの認証失敗 (HTTP 407) を、件数に集約して記録します。認証前の要求の内容は信頼できないため、宛先は含めません。

```json
{"msg": "proxy_auth_failed", "count": 1}
{"msg": "proxy_auth_failed", "count": 37}
```

1. 集約期間外の最初の失敗は、すぐに `count: 1` で記録し、1 分の集約期間を始めます。
2. 集約期間中の失敗は件数だけを数えます。
3. 集約期間の終了時に件数が 1 以上であれば、その件数を 1 件記録し、次の 1 分の集約期間を始めます。件数が 0 であれば集約期間を終えます。
4. セッションの終了時に未記録の件数があれば、最後に 1 件記録します。

記録量は 1 分あたり 1 件に抑えられます。bridge の proxy_status による通知 (`auth_failed`) は変えません。

## 4. BL-004 CI の断続的な失敗

### 4.1. job_start の初期入力の順序

job_start の初期入力 (`stdin`) は、別の goroutine で書き込んでいます。直後の job_write (`close: true`) が先に stdin のロックを取得すると、後続の入力を書き込んで stdin を閉じてしまい、初期入力が失われます。

job_start の中で stdin のロックを取得してから、初期入力を書き込む goroutine に渡します。

```go
if opt.Stdin != "" && j.stdin != nil {
    j.stdinMu.Lock() // later writes and closes wait for the initial input
    go func() {
        defer j.stdinMu.Unlock()
        io.WriteString(j.stdin, opt.Stdin)
    }()
}
```

- job_start は従来どおりすぐに戻ります。後続の job_write と stdin の終了は、初期入力の書き込みの完了を待ちます。
- 初期入力の書き込み中は stdin_open が true のままです (ロックを取得できない状態を書き込み中とみなす既存の判定と同じです)。

### 4.2. TestPTYScreen の画面確認

全画面を描画するプログラムの画面状態 (`screen: true`) が行数どおりに再構成され、エスケープ シーケンスを含まないことの確認 (Windows 以外) に、`top` の代わりに sh のスクリプトを使います。OS の `top` の描画開始の遅れや端末設定に依存しなくなります。

```sh
printf '\033[2J\033[H'; printf 'PID  COMMAND\n'; printf '\033[5;1Hrow five'; sleep 30
```

画面が 12 行であること、1 行目が `PID  COMMAND`、5 行目が `row five` で始まること、エスケープ シーケンスを含まないことを確認します。`vi` の確認と Windows の確認は変えません。

## 5. 作業の進め方

| 順 | 内容 | ブランチ |
| --- | --- | --- |
| 1 | 元案件の検討記録 3 件 (`docs/prompts/261031-port-forward-*.md`) を `docs/prompts/past/` へ移し、検討用 Markdown の相対リンク (7 件、`../../` → `../../../`) を直してコミット・プッシュします | claude/port-forward |
| 2 | PR #2 の CI の失敗が BL-004 の既知の事象 (macOS の TestPTYScreen、TestJobStdin) だけであることを確認し、ドラフトを解除してマージ コミットで main へマージします。既知の事象以外の失敗があればマージせずに停止して確認します | claude/port-forward → main |
| 3 | マージ後の main が、調査基準のコミット 32e3ace に手順 1 の移動を加えた内容と一致することを確認します | main |
| 4 | main から作業ブランチを作成し、本件の検討記録を追加します。本件の検討用 Markdown から元の残課題一覧へのリンクを `past/` に合わせて直します | claude/forward-followup |
| 5 | [2.](#2-bl-002-接続先の待ち受けポートの許可範囲)〜[4.](#4-bl-004-ci-の断続的な失敗) を実装し、テストと文書を更新します。最初のプッシュ時に main 向けのドラフト PR を作成します | claude/forward-followup |
| 6 | 元の残課題一覧 (`docs/prompts/past/261031-port-forward-backlog.md`) の状態の列を更新します | claude/forward-followup |
| 7 | 受入条件 ([8.](#8-受入条件)) をすべて満たしたことを確認し、本件の残課題一覧を整理します (残渣がなければ作成しません) | claude/forward-followup |
| 8 | 本件の検討記録を `docs/prompts/past/` へ移し (成果物の承認の記録は未記入)、相対リンクを直します | claude/forward-followup |
| 9 | 全 OS の CI の成功を確認し、ドラフトを解除してマージ コミットで main へマージします。失敗した場合はマージせず、原因を調べて結果を示し、扱いを確認します | claude/forward-followup → main |
| 10 | 成果物と検証結果を提示し、承認を受けます。承認後、`past/` の検討用 Markdown に承認を記録し、main へ直接コミット・プッシュします | main |

Table: 作業の順序

元の残課題一覧の状態の列は、次のとおり更新日と検討 ID を併記して更新します。

```text
| BL-001 | ... | 容認 (2026-11-xx、261031-forward-followup) |
| BL-002 | ... | 解消 (2026-11-xx、261031-forward-followup) |
| BL-003 | ... | 解消 (2026-11-xx、261031-forward-followup) |
| BL-004 | ... | 解消 (2026-11-xx、261031-forward-followup) |
```

> [!IMPORTANT]
> 本件の PR は、成果物の承認を待たずにマージします。承認の段階で追加の課題や指摘を受けた場合は、マージ済みの main を起点に扱いを確認します。

## 6. 実装単位

| 対象 | 変更内容 |
| --- | --- |
| `cmd/host-shell/main.go` | `--allow-forward-remote-port` の定義と解析。`forward.NewTarget` への許可範囲の受け渡し。`clientproxy.NewTarget` への監査ログの受け渡し |
| `internal/config/config.go` | 許可範囲の保持と、`--allow-forward-remote` なしの指定の検証 |
| `internal/forward/spec.go` | 許可範囲の型、書式の解析と表示 |
| `internal/forward/target.go` | 待ち受けの判定 ([2.2.](#22-待ち受けの判定))、拒否の監査記録、`TargetInfo` への許可範囲の追加 |
| `internal/forward/bridge.go` | `not_allowed` の待ち受けエラーの表示 ([2.3.](#23-エラーの表示)) |
| `internal/tools/system.go` | sys_get_info の `restrictions` への許可範囲の追加 |
| `internal/clientproxy/target.go` | `proxy_connect` の記録と `proxy_auth_failed` の集約。集約の時計は試験で注入できるようにします |
| `internal/jobs/jobs.go` | 初期入力の順序の修正 ([4.1.](#41-job_start-の初期入力の順序)) |
| `internal/server/screen_test.go` | `top` の確認を sh のスクリプトに置き換え ([4.2.](#42-testptyscreen-の画面確認)) |
| `internal/forward/forward_test.go`、`internal/clientproxy/proxy_test.go`、`internal/jobs/` のテスト、`cmd/host-shell/forward_test.go` | 受入条件に対応するテストの追加 |

Table: 実装単位

## 7. 知見の反映計画

| 反映先 | 内容 |
| --- | --- |
| `docs/ja/port-forward.md`、`docs/en/port-forward.md` | 接続先のオプションの表に `--allow-forward-remote-port` を追加。「接続先で待ち受けるポート番号は制限しません」を、未指定時の扱いと許可範囲の判定 (ポート 0 を含む) に改めます。安全性の節の監査ログの記載に拒否の記録を追加します |
| `docs/ja/client-proxy.md`、`docs/en/client-proxy.md` | 認証と診断の節に、接続先の監査ログの `proxy_connect` と `proxy_auth_failed` (記録する項目、記録しない内容、集約) を追加します |
| `docs/ja/reference.md`、`docs/en/reference.md` | 接続先のオプションの表に `--allow-forward-remote-port` を追加します |
| `docs/ja/operations.md`、`docs/en/operations.md` | 転送を許可する場合の制限として、`command=` に `--allow-forward-remote-port` を書く例を追加します |
| `internal/tools/help/network.md` | 接続先の `--allow-forward-remote-port` と sys_get_info の restrictions での確認を追加します |
| `cmd/host-shell/main.go` の `--allow-forward-remote-port` の説明 | 書式、`--allow-forward-remote` が必要なこと |
| コード コメント | ポート 0 の要求で範囲内を順に試す理由、認証失敗を集約する理由 (記録量の抑制)、job_start で stdin のロックを先に取得する理由 (後続の書き込みと終了の順序の保証) |

Table: 知見の反映計画

## 8. 受入条件

1. BL-002: 許可範囲内の待ち受けの成功、範囲外の拒否 (常設は起動エラー、forward_open はエラー)、ポート 0 での範囲内の割当と範囲内に空きがない場合の失敗、`--allow-forward-remote` なしの起動エラー、未指定時の無制限、sys_get_info の restrictions と bridge のエラーの内容を確認します。
2. BL-003: 許可・拒否・上限・失敗のそれぞれで `proxy_connect` が記録されること、パス・クエリー・資格情報が記録されないこと、認証失敗の集約 (時計を注入して 1 分の経過を短時間で確認) とセッション終了時の記録を確認します。
3. BL-004: job_start の初期入力と直後の job_write (`close: true`) の順序を、繰り返し実行するテストで確認します。TestPTYScreen が `top` に依存せずに画面状態を確認することを確認します。
4. 既存のテスト (ポート転送、HTTP プロキシ、ジョブ、pty) が維持されることを確認します。
5. make、6 ターゲットのクロスビルド、全 OS の CI を確認します。未実施の検証は受入前に明示して扱いを確認します。
