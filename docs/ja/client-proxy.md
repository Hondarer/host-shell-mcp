# クライアント経由 HTTP プロキシ

接続先から外部へ直接通信できない場合に、クライアント側 bridge のネットワークで HTTP/HTTPS 通信を中継します。既存 SSH セッションの標準入出力だけを使用し、SSH ポート転送は不要です。既定では無効です。HTTP 以外の TCP 通信や逆向きの通信には [ポート転送](port-forward.md) を使用します。

sshd の転送機能を使わないため、sshd の `no-port-forwarding` や `AllowTcpForwarding no` では止められません。接続先の `--client-proxy` を指定しなければ利用できません。

## 起動

両端を明示的に有効化し、bridge に宛先許可リストを指定します。

    host-shell bridge --proxy --proxy-allow-host 'packages.example.com:443' -- \
      ssh -T target /path/to/host-shell --mcp --client-proxy

bridge の --proxy と反復可能な --proxy-allow-host、接続先の --client-proxy を使用します。--proxy には許可規則が1件以上必要です。片側だけ有効、版不一致、無効な規則は起動エラーです。--no-transfer とも併用できます。

    接続先アプリ → 接続先127.0.0.1のHTTPプロキシ
      → SSH stdio (JSON/Base64) → bridge → 外部サーバー

HTTP 転送と CONNECT に対応し、TLS の復号・証明書差し替えは行いません。接続先は Linux/Windows、bridge は Linux/Windows/macOS に対応します。

## 経路と制限

許可規則はホスト名 glob と任意のポートです。例は packages.example.com:443、*.example.com:443 です。CIDR と Unix ソケット規則は使えません。これは要求先の名前の制限であり、名前解決後の IP を制限するものではありません。内部アドレスは一律禁止しません。

bridge はクライアント起動時の HTTP_PROXY/HTTPS_PROXY/NO_PROXY と小文字版を使用し、上流プロキシにも対応します。接続先の環境をクライアントへコピーしません。失敗時の直接通信への自動切替や再試行は行いません。

接続先の通常の exec_run/job_start は HTTP_PROXY/HTTPS_PROXY と小文字版を中継 URL に置換し、NO_PROXY は維持します。自動設定の後に --unset-env、最後に呼び出しの env を適用します。Windows の変数名は大小文字を区別しません。

net_request_http の proxy/none 明示指定は優先されます。省略時は中継設定と NO_PROXY を使用します。Unix ソケットは直接接続です。exec_sudo は sudo の環境規則に従い、自動伝播を保証しません。

この機能は OS 全体の通信隔離ではありません。CONNECT は許可されたポートへの TCP ストリームを運ぶため、必要な宛先とポートだけを許可してください。既存の接続先 --allow-host はネットワークツールの制限として引き続き適用されます。

## 認証と診断

待ち受けは IPv4 ループバックの動的ポートだけです。セッション別トークンでプロキシ認証を行います。同一ユーザーの外部プロセスには sys_get_info の proxy_url_file で示すファイルから URL を設定できます。ファイルの内容を会話や通常ログへ貼り付けないでください。

URL ファイルはユーザー限定権限で作成し、通常終了・切断時に削除します。権限を確保できなければ起動失敗にします。強制終了で残ったファイルのトークンは次のセッションで利用できません。通常のツール結果・監査・進捗ではセッション秘密をマスクします。

bridge の読み取り専用ツール proxy_status で拒否先と理由を取得できます。

接続先の操作ログには、次の記録を残します。

- `proxy_connect`: HTTP の要求 1 件、または CONNECT 1 件ごとに、種別 (`http`、`connect`)、宛先のホストとポート、結果 (`allowed`、`denied`、`failed`) と理由を記録します。URL のパスとクエリー、ヘッダー、本文、資格情報は記録しません。結果は bridge への中継路を開いた時点のものです。HTTP の要求で bridge から上流への接続が後で失敗した場合 (HTTP 502) も `allowed` になります。
- `proxy_auth_failed`: プロキシ認証の失敗 (HTTP 407) の件数を記録します。最初の失敗はすぐに 1 件として記録し、以後は 1 分ごとにその間の件数をまとめて記録します。セッションの終了時に未記録の件数があれば記録します。認証前の要求の宛先は記録しません。

    {"after_seq": 0, "limit": 20}

入力は after_seq (非負、既定0)、limit (1〜100、既定20) です。出力は enabled、state、allow_hosts、active_connections、events、next_seq、truncated です。

events は seq、time、connection_id、host、port、status (allowed/denied/failed)、reason、集約時の count を持ちます。直近100件を保持し、連番昇順で返します。next_seq を次回の after_seq に渡します。truncated は履歴脱落または未返却の結果を示します。stderr にもメタ情報を記録します。本文、URL パス・クエリー、資格情報は診断に含めません。

拒否された場合は host・port・reason を確認し、必要な通信だけを許可リストへ追加して再起動します。エージェントに許可を独断で広げさせる必要はありません。

## 上限と終了

最大64接続 ([ポート転送](port-forward.md) と共有)、復号後16KiB/フレーム、片方向64KiBウィンドウです。通常 MCP と中継制御をデータより優先し、送信待ちを制限します。接続と HTTP ヘッダーは30秒、通信無進行は5分、終了待ちは5秒です。

HTTP エラーは拒否403、認証407、上限503、接続失敗502、確立タイムアウト504を基本とします。CONNECT 成功後の障害は接続終了で通知します。セッション切断時には待ち受けと全接続を閉じます。


MCP の --tools/--read-only はプロキシ通信全体の制御ではありません。プロキシ経路は両端の明示有効化と bridge の宛先許可リストで管理します。
