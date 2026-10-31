<!--
生成元: agree-before-implement スキル
文書種別: 最終仕様 (design)
最終更新エージェント: claude code / セッション ID: session_01KJc53TaExYCrwkQtAVdF7N
上流文書: 261031-port-forward-discussion.md (検討用 Markdown)。削除されている場合があります。
下流文書: 261031-port-forward-backlog.md (残課題一覧)。削除されている場合があります。
再開手順: このファイルを開いたエージェントは、作業の前に /home/user/c-modernization-kit/app/general/.agents/skills/agree-before-implement/SKILL.md を読み込んでください。上流文書が存在する場合は、上流文書の再開手順に従って進行位置を確認し、判断の経緯を上流文書の Q&A で確認します。存在しない場合は、この文書を単体の最終仕様として扱います。最終更新エージェントのセッション情報が参照可能であれば再開時のコンテキストとして利用し、別エージェントからの引き継ぎや担当変更によりセッションが見つからない場合も異常とせず、文書の記載から継続してください。
-->

# SSH stdio 経由の双方向ポート転送 最終仕様

検討 ID: 261031-port-forward

版: 1

状態: 承認済み

## 1. 目的・構成

SSH のポート転送 (`-L`、`-R`、`-D`) を使えない環境で、既存 SSH セッションの標準入出力 (stdio) を通じて TCP のポート転送を提供します。クライアント経由 HTTP プロキシと同じ多重化チャネルを共用し、MCP の通信と同じ経路に載せます。

| 機能 | 待ち受け側 | 発信側 | ssh の相当 |
| --- | --- | --- | --- |
| ローカル転送 | クライアント (bridge) | 接続先 (host-shell --mcp) | `-L` |
| リモート転送 | 接続先 (host-shell --mcp) | クライアント (bridge) | `-R` |
| SOCKS5 (クライアント側) | クライアント (bridge) | 接続先 (host-shell --mcp)、宛先は接続ごと | `-D` |

Table: 転送の種類

```text
ローカル転送・SOCKS5:
  クライアントのアプリ → bridge のループバック待ち受け
    → SSH stdio の多重化チャネル → 接続先 host-shell → 接続先から見える宛先

リモート転送:
  接続先のアプリ → 接続先 host-shell のループバック待ち受け
    → SSH stdio の多重化チャネル → bridge → クライアントから見える宛先 (直接接続)
```

CodeBlock: 各転送の経路

- 転送対象は TCP だけです。Unix ドメイン ソケットと UDP は扱いません。
- 対応 OS は既存構成と同じです (接続先は Linux/Windows、bridge は Linux/Windows/macOS)。
- 機能は既定で無効です。接続先の管理者の許可 (接続先のオプション) と、bridge 側の利用者の要求 (bridge のオプション) の両方がそろった方向だけが有効になります。

> [!NOTE]
> 本機能は sshd の転送機能を使わずに同等の経路を作るため、sshd の `no-port-forwarding` や `AllowTcpForwarding no` では止められません。既存の HTTP プロキシも同じ性質です。接続先の管理者は、`authorized_keys` の `command=` に許可オプションを書かないことで本機能を禁止できます。host-shell は sshd の設定を検出しません。

## 2. 起動オプション

### bridge (host-shell bridge)

| オプション | 値 | 意味 |
| --- | --- | --- |
| `--forward-local` | `[BIND:]PORT:HOST:HOSTPORT` | 常設のローカル転送。クライアントの PORT で待ち受け、接続先から HOST:HOSTPORT へ発信します。 |
| `--forward-remote` | `[BIND:]PORT:HOST:HOSTPORT` | 常設のリモート転送。接続先の PORT で待ち受け、クライアントから HOST:HOSTPORT へ発信します。 |
| `--forward-socks` | `[BIND:]PORT` | 常設の SOCKS5。クライアントの PORT で待ち受けます。 |
| `--forward-allow-local` | `HOST[:PORT]` | forward_open で作るローカル転送の宛先と、SOCKS5 (常設・ツール作成とも) の各接続の宛先の許可規則。 |
| `--forward-allow-remote` | `HOST[:PORT]` | forward_open で作るリモート転送の宛先の許可規則。 |

Table: bridge の転送オプション (すべて反復可能)

- BIND は `127.0.0.1` (省略時) または `[::1]` だけを受け付けます。それ以外 (`localhost`、`0.0.0.0` など) は起動エラーです。
- PORT は 0〜65535 で、0 は空きポートの自動割当です。実際のポートは forward_status で確認できます。HOSTPORT は 1〜65535 です。
- HOST が IPv6 アドレスの場合は `[` `]` で囲みます。HOST は空にできません。
- 許可規則の書式と照合は `--proxy-allow-host` と同じです (ホスト glob と任意のポート glob、大小文字を区別しない、CIDR と `unix:` は不可)。規則は要求された名前を照合し、名前解決後の IP の範囲は保証しません。
- 常設のローカル転送・リモート転送は、定義自体を利用者の許可とみなし、許可規則と照合しません。
- `--forward-socks` には 1 件以上の `--forward-allow-local` が必要です。すべての宛先を許可する場合は `--forward-allow-local '*'` を明示します。
- 常設転送の合計がセッションの転送上限 (16) を超える場合は起動エラーです。
- `--proxy`、`--no-transfer` とは独立に併用できます。

起動例:

```text
host-shell bridge \
  --forward-local 8080:localhost:3000 \
  --forward-remote 15432:db.client.example:5432 \
  --forward-socks 1080 --forward-allow-local '*.internal.example' \
  -- ssh -T target /path/to/host-shell --mcp --allow-forward-local --allow-forward-remote
```

CodeBlock: bridge と接続先の起動例

### 接続先 (host-shell --mcp)

| オプション | 値 | 意味 |
| --- | --- | --- |
| `--allow-forward-local` | なし | ローカル転送と SOCKS5 (接続先からの発信) を許可します。 |
| `--allow-forward-remote` | なし | リモート転送 (接続先での待ち受け) を許可します。 |

Table: 接続先の転送オプション

- 2 つのオプションは値を取りません。`--allow-` で始まる他のオプションと異なり値を取らないことを help に明記します。
- `--client-proxy` とは独立です。`--client-proxy` だけを指定した接続先では転送は許可されません。
- ローカル転送と SOCKS5 の発信先には、既存の `--allow-host` (ホスト glob または CIDR と任意のポート) を適用します。未指定時は接続先側では宛先を制限しません。`unix:` 規則は TCP の宛先に一致しません (既存のネットワーク ツールと同じ)。
- 接続先で待ち受けるポート番号は制限しません (OS の制約とポート使用中の失敗に従います)。
- `--read-only` と `--tools` は転送の許可と独立であり、転送を制限しません。転送の可否は上記 2 つのオプションだけで決まります。

> [!IMPORTANT]
> `--read-only` は MCP ツールの操作の制限であり、ローカル転送で接続先のサービス (DB など) へ書き込むことは防ぎません。`--read-only` を厳格な境界として運用する場合は、転送の許可オプションを指定しないでください。この関係を reference.md と port-forward.md に明記します。

### 要求と許可の照合

bridge の指定を「要求」、接続先の指定を「許可」として、チャネルの交渉で照合します。

| 機能 | bridge の要求 | 接続先の許可 | 照合の規則 |
| --- | --- | --- | --- |
| `http_proxy` | `--proxy` | `--client-proxy` | 両端必須 (既存どおり)。片側だけは起動エラー |
| `forward_local` | `--forward-local`、`--forward-socks`、`--forward-allow-local` のいずれか | `--allow-forward-local` | 要求があり許可がなければ起動エラー |
| `forward_remote` | `--forward-remote`、`--forward-allow-remote` のいずれか | `--allow-forward-remote` | 要求があり許可がなければ起動エラー |

Table: 機能の要求と許可

- 許可だけで要求のない機能は、無効のまま正常に起動します。接続先の管理者は許可オプションを `command=` に常設できます。
- bridge が何も要求しない場合はチャネルを構成せず、通常の MCP として動作します。
- 交渉で有効になった機能だけを使用できます。有効でない機能の open や listen を受信した場合はプロトコル違反としてセッションを終了します。

## 3. 転送ツール (bridge)

bridge 専用のツールです。接続先の `--tools` とは独立して公開します。

| ツール | 公開条件 |
| --- | --- |
| forward_status | 常設転送 (`--forward-local`、`--forward-remote`、`--forward-socks`) または許可規則 (`--forward-allow-local`、`--forward-allow-remote`) を 1 つでも指定した場合 |
| forward_open、forward_close | `--forward-allow-local` または `--forward-allow-remote` が 1 件以上ある場合 |

Table: 転送ツールの公開条件

### forward_open

| 入力 | 型 | 内容 |
| --- | --- | --- |
| direction | `local` / `remote` / `socks` | 必須 |
| listen_address | `127.0.0.1` / `::1` | 省略時 `127.0.0.1` |
| listen_port | 0〜65535 | 省略時 0 (自動割当) |
| host | 文字列 | `local`・`remote` で必須、`socks` では指定不可 |
| port | 1〜65535 | `local`・`remote` で必須、`socks` では指定不可 |

Table: forward_open の入力

出力は forward_id (セッション内で再利用しない正の整数)、direction、listen_address、listen_port (実際の待ち受けポート)、host、port です。

- `local` と `socks` は `--forward-allow-local`、`remote` は `--forward-allow-remote` が必要です。規則のない方向はエラーです。
- `local` と `remote` は、作成時に host と port を許可規則と照合します。`socks` は各接続の宛先を接続時に照合します。
- 転送上限 (常設とツール作成の合計 16) の超過、待ち受けの失敗 (ポート使用中など)、接続先でのリモート転送の待ち受け失敗はツールのエラーとして返します。

### forward_close

入力は forward_id です。待ち受けを閉じ、その転送で確立済みの接続もすべて切断します。常設転送と未知の forward_id はエラーです。リモート転送では接続先へ待ち受けの終了を指示します。

### forward_status

入力は after_seq (既定 0) と limit (既定 20、1〜100) で、proxy_status と同じです。出力は次のとおりです。

- forwards: forward_id、direction、persistent (常設か)、listen_address、listen_port、host、port、active_connections
- allow_local、allow_remote: 許可規則
- active_connections: チャネル全体の接続数 (HTTP プロキシを含む)
- events、next_seq、truncated: 接続結果の履歴 (proxy_status と同じページングと脱落の表示)

events の項目は seq、time (RFC3339)、forward_id、connection_id、direction、host、port、status、reason です。status は `allowed` (確立)、`denied` (規則・上限による拒否)、`failed` (発信の失敗) です。reason は次の固定の文言とし、生のネットワーク エラーは含めません。

| reason | status |
| --- | --- |
| `connection established` | allowed |
| `destination not allowed` (bridge の規則) | denied |
| `destination not allowed by target` (接続先の `--allow-host`) | denied |
| `connection limit reached` | denied |
| `forward closed` | denied |
| `unsupported SOCKS request` | denied |
| `connection refused` | failed |
| `host unreachable` (名前解決の失敗を含む) | failed |
| `connection timeout` | failed |
| `connection failed` | failed |

Table: forward_status の reason

履歴は proxy_status とは別に直近 100 件を保持します。同じ内容を stderr にも 1 行で記録します。転送の本文は記録しません。

bridge の Instructions に、転送の有効時は forward_status で状態と拒否理由を確認できること、forward_open/forward_close の公開時はツールで転送を作成できることを追記します。

## 4. 多重化チャネル (internal/channel)

`internal/clientproxy/channel.go` の多重化を汎用パッケージ `internal/channel` へ分離し、HTTP プロキシとポート転送が共用します。通信の性質 (標準 Base64、復号後 16KiB/フレーム、片方向 64KiB のウィンドウ、制御と通常 MCP の優先、各接続のバイト順序と片方向終了の維持、受信フレーム 24KiB の上限、終了の冪等性) は既存の定義を維持します。

### 通知

通知名を `host-shell/channel/*` とし、版は 1 のまま定義を次のとおり改めます。

| 通知 | 送信元 | 内容 |
| --- | --- | --- |
| hello | 両方 | version (1)、features (機能名の配列) |
| listen | bridge | forward、bind、port。リモート転送の待ち受けの指示 |
| listen_result | 接続先 | forward、port (実際の待ち受けポート)、error |
| unlisten | bridge | forward。リモート転送の待ち受けの終了 |
| ready | 両方 | 交渉の完了 |
| open | 両方 | id、kind、host、port、forward |
| open_result | 受信側 | id、error |
| data、window、half_close、close | 両方 | 既存どおり |
| limit | 接続先 | host、port、forward。接続先での接続上限による拒否の報告 |
| auth_failed | 接続先 | HTTP プロキシの認証失敗の報告 (既存どおり) |

Table: チャネルの通知

### 交渉

1. bridge は要求する機能が 1 つ以上ある場合だけ、hello で要求する機能 (`http_proxy`、`forward_local`、`forward_remote`) を送ります。
2. 接続先は、許可オプションの有無にかかわらず最初のメッセージを確認します。
    - `host-shell/channel/hello` の場合は、許可する機能を hello で返します。許可が何もない場合も空の一覧を返します。
    - 通常の MCP のメッセージの場合は、`--client-proxy` 指定時は起動エラー (HTTP プロキシの両端必須)、それ以外はそのメッセージから通常の MCP として処理します。
    - `host-shell/proxy/` で始まる通知の場合は、旧定義の相手として起動エラーにします。
3. 両端は [要求と許可の照合](#要求と許可の照合) の規則で照合します。不一致の場合は、bridge は不足するオプションを示す起動エラー (例: `client channel negotiation: target does not permit remote forwarding; start host-shell --mcp with --allow-forward-remote`) とし、接続先も stderr へ記録して終了します。
4. bridge は常設のリモート転送ごとに listen を送り、接続先は待ち受けの結果を listen_result で返します。1 件でも失敗した場合は起動エラーです。
5. 両端が ready を交換した後に、通常の MCP の初期化と各機能の待ち受けを開始します。

交渉全体の上限は既存と同じ 30 秒です。

旧定義の相手の扱いは次のとおりです。

| 組み合わせ | 動作 |
| --- | --- |
| 新しい接続先と旧定義の bridge | 接続先が `host-shell/proxy/hello` を検出し、`client channel negotiation: peer uses an older channel definition (host-shell/proxy); update host-shell on both ends` で終了します。この stderr は旧定義の bridge を通じて利用者に表示されます。 |
| 新しい bridge と旧定義の接続先 | 旧定義の接続先は自ら通知を送らないため、通知名では検出できません。接続先が終了した場合 (旧定義の `--client-proxy` 指定時) と、30 秒以内に応答がない場合 (指定なし) は、bridge は `client channel negotiation: target closed or did not answer; ensure host-shell is the same version on both ends and the target permits the requested features` の起動エラーとします。 |

Table: 旧定義の相手の検出

> [!NOTE]
> 接続先は許可オプションがなくても最初のメッセージを確認します。これにより、許可のない要求を 30 秒の待ち時間なしに不足オプション付きの起動エラーとして報告でき、旧定義の bridge も検出できます。通常の MCP のメッセージはそのまま処理するため、既存の動作は変わりません。

### 接続 ID と open

- 接続 ID は bridge が奇数 (1, 3, 5, ...)、接続先が偶数 (2, 4, 6, ...) を採番し、セッション内で再利用しません。受信側は偶奇と単調増加を検証し、違反はプロトコル違反とします。
- open の kind と送信元は次のとおりです。

| kind | 送信元 | 必要な機能 | 内容 |
| --- | --- | --- | --- |
| `http`、`connect` | 接続先 | http_proxy | 既存の HTTP プロキシ |
| `forward` | 接続先 | forward_remote | リモート転送の着信。forward だけを持ち、host と port は持ちません。 |
| `forward` | bridge | forward_local | ローカル転送の発信。forward、host、port を持ちます。 |
| `socks` | bridge | forward_local | SOCKS5 の発信。forward、host、port を持ちます。 |

Table: open の種類

- 接続先からの `forward` の open に対し、bridge は登録済みのリモート転送の宛先へだけ発信します。接続先は宛先を指定できません。bridge が発行していない forward、またはリモート転送でない forward はプロトコル違反です。発行済みで閉じた forward (forward_close との競合) は `forward closed` の open_result で拒否します。
- open_result の error は固定の理由コード (`not_allowed`、`limit`、`refused`、`unreachable`、`timeout`、`failed`、`closed`) とし、生のネットワーク エラーを含めません。HTTP プロキシの既存の理由も同じ方針です。

### 上限

- 同時接続は、HTTP プロキシとポート転送と SOCKS5 の全機能で共有してセッションあたり 64 です (接続待ちを含む)。既存の 32 から変更します。
- 転送 (待ち受け) は、常設とツール作成の合計でセッションあたり 16 です。接続先も待ち受け中のリモート転送を 16 までとし、超過する listen は `limit` で拒否します。

## 5. 各転送の動作

### ローカル転送

1. bridge は待ち受けで接続を受け付けると、`forward` の open (宛先の host と port) を送ります。
2. 接続先は `--allow-host` で宛先を検査し、30 秒を上限に TCP で発信します。結果を open_result で返し、接続先の監査ログに記録します。
3. 失敗した場合、bridge は受け付けた接続を閉じ、理由を forward_status と stderr に記録します。

### リモート転送

1. 接続先は listen を受けて `BIND:PORT` で待ち受け、開始と終了を接続先の監査ログに記録します。
2. 接続先は接続を受け付けると、`forward` の open (forward だけ) を送ります。接続上限に達している場合は接続を閉じ、`limit` を報告します。
3. bridge は登録済みの宛先へ、30 秒を上限に直接 TCP で発信します。HTTP_PROXY/HTTPS_PROXY/NO_PROXY は参照しません。結果を open_result で返し、forward_status と stderr に記録します。

### SOCKS5

- 対応は SOCKS5 (RFC 1928) の CONNECT だけです。認証方式は「認証なし」(0x00) だけを受け付け、提示されない場合は 0xFF を返して閉じます。
- アドレスの型は IPv4、IPv6、ドメイン名です。ドメイン名は接続先で名前解決します (`socks5h://` 相当)。
- bridge は宛先を `--forward-allow-local` と照合し、一致すれば `socks` の open を送ります。接続先は `--allow-host` を適用して発信します。
- 応答コードは次のとおりです。成功時の BND.ADDR と BND.PORT は 0.0.0.0:0 とします。

| 結果 | 応答コード |
| --- | --- |
| 成功 | 0x00 |
| bridge の規則または接続先の `--allow-host` による拒否 | 0x02 |
| 名前解決の失敗、ネットワーク・ホスト到達不可 | 0x04 |
| 接続拒否 | 0x05 |
| 確立待ち 30 秒の超過 | 0x06 |
| BIND、UDP ASSOCIATE | 0x07 |
| 未対応のアドレスの型 | 0x08 |
| 接続上限、その他の失敗 | 0x01 |

Table: SOCKS5 の応答コード

- SOCKS4/4a の要求には SOCKS4 の拒否応答 (0x5B) を返して閉じます。
- 待ち受けは無認証です。同一ホストの他ユーザーのプロセスも接続できる点は ssh の `-D` と同じであり、文書に明記します。

### 共通

- 待ち受けは両側ともループバック (127.0.0.1 または ::1) だけです。待ち受けポートへの接続は認証しません。同一ホストの他ユーザーのプロセスも接続できる点は ssh の `-L`/`-R` と同じであり、文書に明記します。
- 片方向の終了 (TCP の half-close) を相手側へ伝えます。両方向の終了またはエラーで接続を閉じます。
- ポート転送と SOCKS5 の接続には無通信タイムアウトを設けません。どちらかの終了、forward_close、セッションの終了まで維持します。HTTP プロキシの通信無進行 5 分は維持します。
- 常設のローカル転送と SOCKS5 は、bridge が接続先のコマンドを起動する前に待ち受けを確保します。失敗は起動エラーです。待ち受けで受け付けた接続の処理は交渉の完了後に開始します。
- セッションの終了 (EOF、キャンセル、異常終了) 時は、両側のすべての待ち受け、接続、待機処理、goroutine を終了・解放します。自動再試行は行いません。

## 6. 接続先の監査ログと sys_get_info

接続先の監査ログ (JSON Lines) に次のイベントを記録します。本文は記録しません。HTTP プロキシの通信の記録は今回追加しません。

| イベント | 項目 |
| --- | --- |
| `forward_listen` | forward_id、address (待ち受けアドレスとポート)、result (`ok`/`failed`)、reason |
| `forward_unlisten` | forward_id、address |
| `forward_connect` | direction (`local`/`socks`)、forward_id、host、port、result (`allowed`/`denied`/`failed`)、reason |

Table: 接続先の監査イベント

sys_get_info に次の項目を追加します。リモート転送の bridge 側の宛先は接続先へ送らないため含みません。

- `forward_local`、`forward_remote`: 接続先で許可された転送方向 (真偽値)
- `remote_forwards`: 待ち受け中のリモート転送 (forward_id と address) の一覧

## 7. 実装単位

| 対象 | 内容 |
| --- | --- |
| internal/channel (新規) | 交渉 (機能の照合、旧定義の検出、listen の交換)、双方向の open と偶奇の採番、フロー制御、優先キュー、終了処理、診断履歴のリング (100 件、ページング、脱落判定) の共通実装 |
| internal/clientproxy | gateway.go、target.go、listener.go を internal/channel の上へ移行し、channel.go を削除します。外部仕様 (proxy_status、接続先の HTTP プロキシ) は上限の 64 以外は変えません。 |
| internal/forward (新規) | 常設転送の書式の解析、bridge 側の転送管理 (待ち受け、ツールの処理、リモート転送の直接発信、forward_status)、SOCKS5、接続先側の発信 (`--allow-host` と監査) とリモート転送の待ち受け |
| internal/policy | `--proxy-allow-host` のホスト名規則の解析と照合を共通化し、HTTP プロキシと転送で共用します。 |
| internal/bridge/bridge.go | 転送オプション、要求する機能の決定、転送ツールの登録、Instructions |
| cmd/host-shell/main.go、internal/config/config.go | 許可オプション、接続先の transport の組み立て (常に最初のメッセージを確認) |
| internal/tools/system.go | sys_get_info の転送項目 |
| internal/tools/help/network.md | 埋め込み help のポート転送の節 |
| テスト | internal/channel、internal/forward、internal/clientproxy、internal/bridge の単体・インメモリ結合テスト、cmd/host-shell の実 stdio 子プロセスの結合テスト |
| .agents/skills/host-shell-connect/bin | `make dist` によるスキル同梱バイナリの更新 |

Table: 実装単位

作業は PR #1 (クライアント経由 HTTP プロキシ) が main へマージされた後に main から分岐して行います。コミットとプッシュは承認済みの作業として実施し、main へのマージは行いません。

## 8. 知見の反映計画

| 反映先 | 内容 |
| --- | --- |
| docs/ja/port-forward.md、docs/en/port-forward.md (新規) | 3 種類の転送の用途と経路、起動例、要求と許可の照合、許可規則と `--allow-host`、ツール、上限、診断、無認証の待ち受け、sshd の転送禁止との関係、`--read-only`・`--tools` との関係、`socks5h://` の案内 |
| docs/ja/client-proxy.md、docs/en/client-proxy.md | port-forward.md との相互リンク、sshd の転送禁止では止められないことの注記、同時接続数の上限 32 → 64 (全機能で共有) |
| docs/ja/operations.md、docs/en/operations.md | sshd の転送禁止との関係の注記、`command=` に許可オプションを書く例と書かない場合の禁止 |
| docs/ja/user-guide.md、docs/en/user-guide.md | 「SSH のポート転送は使用しません」の明確化と port-forward.md への案内 |
| docs/ja/reference.md、docs/en/reference.md | bridge と接続先のオプション、forward_open・forward_close・forward_status、sys_get_info の項目、`--read-only`・`--tools` が転送を制限しないこと |
| docs/ja/development.md、docs/en/development.md | internal/channel と internal/forward のパッケージ構成、通知と交渉の概要 |
| README.md | 特長とツールの一覧 |
| internal/tools/help/network.md | ポート転送の節と、HTTP プロキシの節の上限の更新 |
| .agents/skills/host-shell-connect/SKILL.md | 転送オプションの案内 |
| internal/channel、internal/forward のパッケージ コメント | 通知の役割、偶奇の採番、接続先が宛先を指定できないこと、要求と許可の照合 |

Table: 知見の反映計画

英訳はすべて日本語版に合わせて更新します。

## 9. 受入条件

1. ローカル転送・リモート転送・SOCKS5 (CONNECT、IPv4・IPv6・ドメイン名) で、バイナリ・大容量・双方向・片方向終了の往復を、ローカルの試験サーバーで確認します。
2. 常設転送 (ポート 0 の自動割当、`[::1]` を含む) と、forward_open・forward_close・forward_status (常設転送の close 拒否、close 時の既存接続の切断、公開条件) を確認します。
3. 要求と許可の照合を確認します。許可のない要求の起動エラー、許可だけでの正常起動、要求なしでの通常 MCP、HTTP プロキシの両端必須の維持、旧定義の相手の検出です。
4. 宛先の制限を確認します。接続先 `--allow-host` (ローカル転送・SOCKS)、bridge の規則 (ツール作成・SOCKS)、`--forward-socks` の規則必須、SOCKS の応答コード、診断履歴の理由・ページング・脱落です。
5. 待ち受けを確認します。ループバック以外の拒否、ポート使用中 (常設は起動エラー、ツールはエラー)、接続先でのリモート転送の待ち受け失敗です。
6. 上限を確認します。64 接続の共有と超過、16 転送と超過、ポート転送の無通信での維持と HTTP プロキシの 5 分の維持です。
7. 通信の安全性を確認します。双方向の open の採番が衝突しないこと、接続先が未登録の転送や任意の宛先への接続を bridge に開かせられないこと、不正フレーム、切断・キャンセル時の全資源の解放 (goroutine を含む) です。
8. 接続先の監査ログと sys_get_info の内容、および本文を記録しないことを確認します。
9. 既存 HTTP プロキシの全テスト、通常ツール・resources・ファイル転送が、各機能の有効・無効の組み合わせで維持されることを、インメモリと実 stdio 子プロセスの結合テストで確認します。
10. make、6 ターゲットのクロスビルド、全 OS の CI を確認します。未実施の検証は受入前に明示して扱いを確認します。

時間に関わる検証 (無通信の維持、確立待ち 30 秒) は、時計やタイムアウト値を注入して短時間で行います。
