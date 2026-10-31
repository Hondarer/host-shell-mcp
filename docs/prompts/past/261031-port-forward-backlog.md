<!--
生成元: agree-before-implement スキル
文書種別: 残課題一覧 (backlog)
最終更新エージェント: claude code / セッション ID: session_01KJc53TaExYCrwkQtAVdF7N
上流文書: 261031-port-forward-design.md (最終仕様)、261031-port-forward-discussion.md (検討用 Markdown)。いずれも未作成の場合や、削除されている場合があります。
再開手順: このファイルを開いたエージェントは、作業の前に /home/user/c-modernization-kit/app/general/.agents/skills/agree-before-implement/SKILL.md を読み込んでください。「整理状態」が「随時追記中」であれば、案件の作業中です。気付いた事項を追記し、第 3 ゲートの前に文書を再構築して整理します。上流文書が存在する場合は、各課題の背景と見送りの判断を上流文書で確認します。存在しない場合は、この文書を単体の残課題一覧として扱います。このファイルの残課題を参照して新たな成果物の作成または更新を始めるときは、元案件の再開とは別の作業として扱い、新しい検討用 Markdown から開始してください。最終更新エージェントのセッション情報が参照可能であれば再開時のコンテキストとして利用し、セッションが見つからない場合も異常とせず、文書の記載から継続してください。
-->

# 残課題一覧 (バックログ)

## 案件の識別

案件名: SSH stdio 経由の双方向ポート転送 (261031-port-forward)  
作成日: 2026-10-31  
整理状態: 整理済み

この文書は、見送る決定がなされた問題点や、作業中に潜在または別問題として判明したが今回は対策しなかった最終的な残渣 (残課題) を記録したものです。作業期間中は気付いた時点で随時追記し、設計および実装・検証作業がすべて完了した後に、再構築して整理しています。作業中に解消した問題や、仕様・制約として容認した問題点は含みません。各項目はチケット管理システム (GitHub Issues、Jira など) への起票にそのまま利用できる詳細度・具体性で記載されています。

## 残課題一覧

| 課題 ID | 題名 | 区分 | 優先度 / 影響度 | 状態 |
| --- | --- | --- | --- | --- |
| BL-001 | Unix ドメイン ソケットと UDP の転送 | 検討時見送り | 低 | 未着手 |
| BL-002 | 接続先で待ち受けるポート番号の許可範囲 | 検討時見送り | 低 | 未着手 |
| BL-003 | HTTP プロキシの接続の接続先監査ログへの記録 | 検討時見送り | 低 | 未着手 |
| BL-004 | CI で TestPTYScreen と TestJobStdin が断続的に失敗する | 作業中摘出 | 中 | 未着手 |

## BL-001 Unix ドメイン ソケットと UDP の転送

### 概要と問題点

- **現状の事象**: ポート転送 (`--forward-local`、`--forward-remote`、`--forward-socks`、forward_open) は TCP だけを扱います。Docker ソケット (`/var/run/docker.sock`) などの Unix ドメイン ソケットや、DNS などの UDP は転送できません。SOCKS5 も CONNECT だけで、UDP ASSOCIATE には 0x07 を返します。
- **本来あるべき姿**: ssh の `-L /path:host:port` や `-L port:/path` に相当する Unix ドメイン ソケットの転送を、必要に応じて利用できること。UDP は ssh も対応しないため、需要が確認できた場合に限り検討します。

### 経緯と見送り理由

- **摘出契機**: 前提検討時に、転送できる通信の種別を TCP だけとする決定に伴い見送りました。
- **今回の見送り理由**: Unix ドメイン ソケットは Windows の対応状況と、ソケット ファイルのパスの権限・後始末の設計が追加で必要です。UDP はデータグラムの境界の保持や関連付けのタイムアウトなど、ストリームとは別の多重化の設計が必要です。

### 影響範囲とリスク

- **影響対象**: internal/forward (書式の解析、待ち受けと発信)、internal/channel (open の種別、UDP の場合はフレームの種別)、接続先の `--allow-host` の `unix:` 規則の適用、文書。
- **放置した場合のリスク**: 接続先の Docker API などを手元のツールから使う場合に、`net_request_http` の `unix_socket` 指定など別の手段が必要になります。機能上の不具合は生じません。

### 推奨される対応策

- **恒久対応案**: Unix ドメイン ソケットは、open の kind に `unix` を追加し、接続先の発信に既存の `--allow-host` の `unix:` 規則 (`CheckUnixSocket`) を適用します。待ち受け側のソケット ファイルは作成者限定の権限で作り、終了時に削除します。方向ごとの許可は既存の `--allow-forward-local`、`--allow-forward-remote` を使うか、別オプションとするかを検討します。
- **参考情報**: 検討用 Markdown の Q-003、internal/policy/policy.go の `CheckUnixSocket`。

## BL-002 接続先で待ち受けるポート番号の許可範囲

### 概要と問題点

- **現状の事象**: リモート転送で接続先が待ち受けるポート番号を、接続先の管理者が制限する手段がありません。`--allow-forward-remote` を指定すると、ループバックの任意のポート (OS が許す範囲) で待ち受けられます。
- **本来あるべき姿**: 必要な場合は、ssh の `PermitListen` (OpenSSH 7.8 以降) のように、接続先の管理者が待ち受けを許すポート番号の範囲を指定できること。

### 経緯と見送り理由

- **摘出契機**: 前提検討時に、接続先側ではポート番号を制限せず OS の制約に従う決定に伴い見送りました。
- **今回の見送り理由**: 待ち受けはループバックに限られ、方向ごとの許可オプションで機能自体を拒否できるため、今回は不要と判断しました。

### 影響範囲とリスク

- **影響対象**: cmd/host-shell/main.go (オプション)、internal/forward/target.go (待ち受け前の検査)、internal/channel (listen_result の理由コード)、文書。
- **放置した場合のリスク**: 接続先で他のサービスが使う予定のポートを、先にリモート転送が使用する場合があります。転送はセッションの終了で解放されます。

### 推奨される対応策

- **恒久対応案**: 接続先に `--allow-forward-remote-port PORT[-PORT]` (複数指定可) などを追加し、`Target.listen` で照合します。拒否は listen_result の `not_allowed` で返し、bridge は起動エラーまたは forward_open のエラーとして報告します。ポート 0 (自動割当) の扱いも合わせて決めます。
- **参考情報**: 検討用 Markdown の Q-007、internal/forward/target.go の `listen`。

## BL-003 HTTP プロキシの接続の接続先監査ログへの記録

### 概要と問題点

- **現状の事象**: クライアント経由 HTTP プロキシの接続 (宛先、結果) は bridge の proxy_status と stderr にだけ記録され、接続先の監査ログには記録されません。ポート転送の待ち受けと発信 (`forward_listen`、`forward_unlisten`、`forward_connect`) は接続先の監査ログに記録するため、機能間で記録の範囲が異なります。
- **本来あるべき姿**: 接続先の管理者が、自身の許可した経路 (`--client-proxy`) の利用状況を、接続先の監査ログだけで確認できること。

### 経緯と見送り理由

- **摘出契機**: 前提検討時に、接続先の監査ログへ転送を記録する決定に伴い、既存機能の仕様変更として見送りました。
- **今回の見送り理由**: 既存 HTTP プロキシの外部仕様を変えない範囲で作業したためです。

### 影響範囲とリスク

- **影響対象**: internal/clientproxy/target.go (接続先の HTTP プロキシ)、internal/audit (イベント)、docs/ja/client-proxy.md と英訳、reference.md の操作ログの節。
- **放置した場合のリスク**: 接続先の監査ログだけでは、HTTP プロキシ経由の外部通信の有無を確認できません。bridge 側の記録は利用者の手元にあり、接続先の管理者は参照できません。

### 推奨される対応策

- **恒久対応案**: 接続先の HTTP プロキシで、要求ごとまたは CONNECT ごとに `proxy_connect` イベント (宛先の host・port、HTTP/CONNECT の区分、bridge の結果の理由コード) を記録します。URL のパス・クエリーと資格情報は記録しません。認証失敗は件数を集約して記録量を抑えます。
- **参考情報**: 検討用 Markdown の Q-035、internal/forward/target.go の `forward_connect` の記録。

## BL-004 CI で TestPTYScreen と TestJobStdin が断続的に失敗する

### 概要と問題点

- **現状の事象**: PR #2 の CI (run 41157336808) の macOS ジョブで、internal/server の 2 つのテストが失敗しました。再実行 (attempt 2) では TestPTYScreen だけが再び失敗し、次のコミットの CI (run 41157609819) ではどちらも成功しました。文書だけを変更したコミットの CI (run 41158029817) でも、macOS の TestPTYScreen と ubuntu の TestJobStdin が失敗しており、TestJobStdin は Linux でも起きます。
    - TestPTYScreen: `pty` ジョブで起動した `top` の画面に 20 秒以内に `PID` が現れず、画面が空のままでした (`screen_test.go:39`)。
    - TestJobStdin: `stdin` で渡した `first\n` が出力に現れず、`job_write` の `second\n` だけが出力されました (`tools_test.go:223`)。
- **本来あるべき姿**: CI がコードの変更と無関係に失敗しないこと。TestJobStdin の失敗が実装の競合 (job_start の初期入力が `cat` に渡る前に失われる) によるものであれば、初期入力が確実に渡ること。

### 経緯と見送り理由

- **摘出契機**: ポート転送の実装後、全 OS の CI の実行中に発見しました。
- **今回の見送り理由**: 失敗したテストと internal/server、internal/jobs、internal/pty、internal/proc は今回変更しておらず、main の CI では成功しているため、本件の範囲外です。

### 影響範囲とリスク

- **影響対象**: internal/server/screen_test.go、internal/server/tools_test.go、および TestJobStdin の失敗が実装起因の場合は internal/jobs の初期入力 (`stdin` と `stdin_open`) の処理。
- **放置した場合のリスク**: CI の失敗が変更と無関係に起き、再実行が必要になります。TestJobStdin の失敗が実装起因であれば、job_start の `stdin` の内容が失われる場合があります。

### 推奨される対応策

- **恒久対応案**: TestJobStdin は、`job_start` の `stdin` の書き込みと `stdin_open` の維持の順序を確認し、初期入力が失われる経路があれば修正します。TestPTYScreen は、macOS の runner で `top` が描画を始めるまでの待ち時間や端末設定 (TERM) を確認し、待機条件を見直すか、`top` に依存しない対象に置き換えます。
- **参考情報**: https://github.com/Hondarer/host-shell-mcp/actions/runs/41157336808 (attempt 1、2)、https://github.com/Hondarer/host-shell-mcp/actions/runs/41157609819、https://github.com/Hondarer/host-shell-mcp/actions/runs/41158029817
