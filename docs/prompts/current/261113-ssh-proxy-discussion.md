<!--
生成元: agree-before-implement スキル
文書種別: 検討用 Markdown (discussion)
最終更新エージェント: claude code / セッション ID: session_01KJc53U8RTJvttMFzQ3TZ55
下流文書: 261113-ssh-proxy-design.md (最終仕様)、261113-ssh-proxy-backlog.md (残課題一覧)、261113-ssh-proxy-validation.md (検証記録)。未作成の場合や、削除されている場合があります。
再開手順: /home/user/c-modernization-kit/app/general/.agents/skills/agree-before-implement/SKILL.md を読み込んでください。ユーザーが編集した最新版を読み直し、作業 WBS の `← **CURRENT**` の項目から再開します。`← **CURRENT**` の項目は処理が中止された項目である場合があります。対象成果物の内容を確認して実施済みの範囲を特定してから再開してください。
-->

# SSH 経路上の HTTP プロキシ (認証付き CONNECT) の検討

## 検討の識別

検討 ID: 261113-ssh-proxy

版: 6

状態: 実施中 (第 2 ゲート通過)

改版: 版 2 では、origin の取り込み (886b8e8) に合わせて、参照資料と Q-003・Q-006・Q-007・Q-008・Q-011 の Context を更新した。版 3 では、Tera Term の原典 (TTProxy) の確認結果に合わせて、参照資料と Q-001・Q-002 の Context を訂正した。回答前のため、質問と選択肢は変えていない。版 4 では、Q-001〜Q-011 の回答を記録し、追加の確認項目 Q-012〜Q-014 を設けた。版 5 では、Q-012〜Q-014 の回答を記録し、追加の確認項目 Q-015 を設けた。版 6 では、Q-015 の回答を記録し、第 1 ゲートを通過した (合意版: 版 6)。

## 作業 WBS と進捗管理

凡例: ⏸️ 未着手、🔄 実施中、🚫 ブロック中、⚠️ 要注意、✅ 完了。現在取り組んでいる項目の末尾に `← **CURRENT**` を付けます。

- ✅ 1. 検討用 Markdown のための情報収集と質問の実施
    - ✅ 1.1. 前提条件と要求事項の調査 (既存の経路・スキル・文書・上流プロキシ実装)
    - ✅ 1.2. 論点の抽出と確認項目 ([Q-001](#q-001-プロキシ方式の範囲) 〜 [Q-011](#q-011-受入とする検証の範囲)) の作成
    - ✅ 1.3. 第 1 ターンの回答 (Q-001〜Q-011) の受付と記録 (2026-11-13 14:28:13 JST、Tetsuo Honda)
    - ✅ 1.4. 回答に基づく前提・制約の検証と WBS の見直し (追加の確認項目 [Q-012](#q-012-インターネット向けプロキシの指す範囲)〜[Q-014](#q-014-既存の用語の置き換え範囲))
    - ✅ 1.5. 第 2 ターンの回答 (Q-012〜Q-014) の受付と記録 (2026-11-13 14:32:06 JST、Tetsuo Honda)
    - ✅ 1.6. 第 3 ターンの回答 ([Q-015](#q-015-コードの識別子の置き換え)) の受付と記録 (2026-11-13 14:36:28 JST、Tetsuo Honda)
- ✅ 2. 前提合意と残懸念の解消 (第 1 ゲート)
    - ✅ 2.1. 全論点に対する未解決事項および質問の解消確認
    - ✅ 2.2. 方針・前提条件の合意確定 (合意版: 版 6)
- ✅ 3. 最終仕様の作成と承認の受付
    - ✅ 3.1. `261113-ssh-proxy-design.md` の作成 (connect 互換の仕様と原典 (connect.c、Tera Term の TTProxy) との照合を含む)
    - ✅ 3.2. 受入条件と整合性の確認
    - ✅ 3.3. 最終仕様の承認の受付 (第 2 ゲート。最終仕様の版 3 を 2026-11-13 に対話「承認」で承認)
- ✅ 4. 承認された最終仕様に従った作業の実施
    - ✅ 4.1. `host-shell connect` の実装 (`cmd/host-shell/main.go`、`internal/routeproxy/`)
    - ✅ 4.2. 自動テスト (試験用 CONNECT プロキシ、Basic 認証の成否、407・403・接続失敗、標準入出力の 8 ビット透過、終了処理)。Linux、Windows、macOS (CI) で成功
    - ✅ 4.3. 検証環境の構築 (Podman の Squid (Basic 認証) と、Squid 経由でしか到達できない sshd コンテナー)
    - ✅ 4.4. 経路にプロキシを含む構成の検証 (Linux のコンテナー) と `261113-ssh-proxy-validation.md` への記録 (MCP 接続、多段 (入れ子と `ProxyJump`)、bridge の `--proxy` との併用、無通信切断)
    - ✅ 4.5. Windows の実機での検証 (Windows 側のセッション。Git for Windows の OpenSSH と Windows の OpenSSH、Tera Term での同じ Squid への接続) と検証記録への追記。Windows 側のセッションで実施した。Tera Term はユーザーの判断で対象外とした
    - ✅ 4.6. 新規文書 `docs/ja/ssh-proxy.md` と英訳 `docs/en/ssh-proxy.md` の作成 (用語の正本、資格情報の章、Tera Term のダイアログとの対応表を含む)
    - ✅ 4.7. 用語の置き換えと文章の推敲 (`agy --dangerously-skip-permissions` に依頼し、差分を確認して取り込む。文書、`SKILL.md`、埋め込み help、スクリプトの出力、コードのコメント、ログ・`proxy_status` の文字列、および [Q-015](#q-015-コードの識別子の置き換え) による識別子。文書の候補は `client-proxy.md`、`code-server.md`、`user-guide.md`、`remote-dns.md`、`coding-agents.md`、`vscode-remote-ssh.md`、`README.md`、`docs/README.md` と各英訳)
    - ✅ 4.8. スキルの整理 (`host-shell-connect` に経路プロキシの扱い、全スキルに「プロキシ」がどちらを指すか不明な場合の問い合わせ)
    - ✅ 4.9. 埋め込み help (`help_get`) と `--help` の更新 (`--help` の使い方に `host-shell connect` を追加。埋め込み help は接続先のツールの案内であり、変更不要)
    - ✅ 4.10. 受入条件への適合確認 (自動テスト (Linux、Windows、macOS)、Linux のコンテナー、Windows の実機、文書の旧称の検索がすべて適合)
    - ✅ 4.11. 残課題一覧 (`261113-ssh-proxy-backlog.md`) の整理
- 🔄 5. 成果物の承認の受付 (第 3 ゲート)
    - 🔄 5.1. 成果物・検証結果・残課題の提示 ← **CURRENT**
    - ⏸️ 5.2. 成果物の承認の受付

## 対象と参照資料

対象成果物: connect 互換の中継コマンド、検証記録、新規文書 (日英)、既存文書とスキルの用語整理。

書き込み可能な検討用 Markdown: この文書のみ (第 1 ゲート前)。

調査基準: コミット 886b8e8 の作業ツリー (この検討文書のほかに変更なし)。

- [クライアント経由 HTTP プロキシ](../../ja/client-proxy.md): bridge はクライアント起動時の `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` を上流プロキシとして使う。上流プロキシは自動テスト ([proxy_test.go](../../../internal/clientproxy/proxy_test.go) の `TestUpstreamHTTPAndCONNECT`、`TestHTTPSUpstream`) のみで、実在のプロキシ製品での検証はない。検証の記載は「Windows の接続先」の節 (接続先側のクライアント) だけで、文書全体の「検証済みの範囲」の節はない。文書内では「上流プロキシ」(bridge 側の `HTTP_PROXY`) と「中継プロキシ」(接続先の `127.0.0.1` のプロキシ) の語をすでに使っている。
- [利用ガイド](../../ja/user-guide.md): host-shell 自身の多段接続は入れ子の `ssh -T` で行い、`ProxyJump` や `-W` は使わない (`AllowTcpForwarding no` の影響を受けないため)。一方、「WSL の Linux への接続」の節は、利用者の `~/.ssh/config` の Host ブロック (`ProxyJump`、`Match host ... exec`) で経路を定義する構成を推奨し、検証済である。その節には次の知見がある。
    - Windows の接続元の ssh として、Git for Windows の OpenSSH 10.6p1 と Windows の OpenSSH 9.5p1 の両方で確認している。
    - Windows の接続元では、設定ファイル内のパスの区切りを `/` にする (ProxyJump が中継用の ssh に渡すパスから `\` が失われる)。
    - OpenSSH は `ProxyCommand` の先頭に `exec` を付けるため、`ProxyCommand` で複数のコマンドを `&&` でつなぐ方法は使えない。
    - SSH 経路上のプロキシの記述はない。
- 検証環境: Windows 11 Enterprise (ビルド 26300、amd64) の Hyper-V 仮想マシンが、Windows の接続先・WSL の検証で使われている ([Windows の接続先の検証記録](../past/261104-windows-target-validation.md) ほか)。
- [host-shell-connect スキル](../../../.agents/skills/host-shell-connect/SKILL.md): `ROUTE` はカンマ区切りのホスト列で、各段を `ssh -T -o BatchMode=yes` で接続する。`~/.ssh/config` の Host 名は使える。プロキシの扱いはない。
- [VS Code Remote - SSH スキル](../../../.agents/skills/host-shell-vscode-remote-ssh/SKILL.md): 利用者の `~/.ssh/config` は変更せず、追加する Host ブロックを表示して利用者に追加させる方針である。
- connect.c (Git for Windows に `connect.exe` として同梱): `ssh` の `ProxyCommand` から呼ばれ、HTTP プロキシに `CONNECT host:port` を送って標準入出力と中継する。`-H proxy[:port]` で HTTP プロキシ、`-S` で SOCKS、`-h` で環境変数 `HTTP_PROXY` を使う。認証情報は環境変数 (`HTTP_PROXY_USER`、`HTTP_PROXY_PASSWORD` など) または対話入力で渡す。オプションと環境変数の正確な一覧は、最終仕様の作成時に原典で確認する。
- Tera Term (TTProxy、[ProxyWSockHook.h](https://github.com/TeraTermProject/teraterm/blob/main/TTProxy/ProxyWSockHook.h) の main ブランチ): プロキシ設定ダイアログで種別、ホスト、ポート、ユーザー名、パスワードを指定する。Tera Term 自身の SSH (TTSSH) が使うもので、OpenSSH の `ssh` には効かない。
    - ダイアログの種別は「プロキシなし」、HTTP、TELNET、SOCKS4、SOCKS5 である (原文で確認)。名前の表には `http+ssl` などの `+ssl` の種別もあるが、ダイアログには出ない。
    - 本検討の「HTTP CONNECT」は、ダイアログの種別「HTTP」に当たる (ユーザーの質問に対する確認、2026-11-13)。種別 HTTP では `CONNECT host:port HTTP/1.1` と `Host` ヘッダーを送り、ユーザー名があれば `Proxy-Authorization: Basic` を付ける。認証は Basic だけである。
    - 設定は `TERATERM.INI` の `ProxyType`、`ProxyHost`、`ProxyPort`、`ProxyUser`、`ProxyPass` などに保存され、パスワードは平文で保存される。
    - 上の 2 項目 (要求の形式、認証、保存) は、取得した原文のうち要約でしか読めなかった部分に基づく。最終仕様の作成時に原文で照合する。

## ユーザーが明示した要求

本対話の依頼 (2026-11-13) を根拠とします。

- 対象のプロキシは、SSH 接続に先立って HTTP CONNECT で Basic 認証を行うもの (Squid 相当) である。Tera Term のプロキシ設定で接続している環境をカバーする。
- Tera Term のプロキシ設定を中継する Go のバイナリを作成する。Git for Windows の `connect` コマンドの互換品とする。
- 接続経路に HTTP プロキシが含まれる場合の設定の扱いを定め、検証する。
- 「HTTP プロキシ」を次の 2 つに分けて扱う。
    - SSH 経路上の認証プロキシ (SSH 接続そのものが通る)
    - インターネットに出るためのプロキシ (接続先のアプリの通信が bridge を経て出ていく先)
- SSH 接続先ごとに、固有の HTTP プロキシ経路を定義できるようにする。
- 利用者が指すプロキシがどちらか分からない場合に、エージェントが利用者に問い合わせられるよう、各スキルと文書を整理する。
- `docs/ja/` に文書を 1 ファイル追加する。

## 現行の合意内容

Q-001〜Q-015 の回答 (2026-11-13 14:28:13 JST、14:32:06 JST、14:36:28 JST 確認) に基づく。

- **方式と認証**: HTTP CONNECT (Tera Term の種別「HTTP」) だけに対応し、認証は「なし」と Basic だけとする。SOCKS、NTLM・Negotiate・Digest、プロキシとの TLS 接続は対象外で、制限として文書に書く ([Q-001](#q-001-プロキシ方式の範囲)、[Q-002](#q-002-認証方式の範囲))。
- **実装形態**: `host-shell connect` サブコマンドとし、引数と環境変数を connect.c と互換にする。ただし `-h` (`HTTP_PROXY` を読む) には対応せず、経路プロキシは常に明示させる ([Q-003](#q-003-実装形態)、[Q-004](#q-004-環境変数-http_proxy-の扱い))。
- **資格情報**: connect.c 互換の環境変数と、権限を確認するパスワード ファイルに対応し、端末があるときだけ対話入力も使う。文書の案内を手厚くする ([Q-005](#q-005-資格情報の渡し方))。
- **経路の定義**: 接続先ごとに `~/.ssh/config` の Host ブロックの `ProxyCommand` で定義する。スキルはブロックを生成・表示し、追加は利用者が行う。対象はクライアントから 1 段目の区間だけとする ([Q-006](#q-006-経路の定義場所)、[Q-007](#q-007-プロキシを経由する段))。
- **用語**: 「SSH 経路プロキシ」(SSH route proxy、`~/.ssh/config` の `ProxyCommand`)、「インターネット向けプロキシ」(internet proxy、bridge の起動環境の `HTTP_PROXY` / `HTTPS_PROXY` だけ)、「接続先中継プロキシ」(target relay proxy、`--proxy` / `--client-proxy`) を使う ([Q-008](#q-008-用語)、[Q-012](#q-012-インターネット向けプロキシの指す範囲)、[Q-013](#q-013-英語の用語))。
- **置き換え**: 既存の「上流プロキシ」「中継プロキシ」(upstream proxy、relay proxy) と派生語を、文書、`SKILL.md`、埋め込み help、スクリプトの出力、コードのコメント、ログ・`proxy_status` の文字列で完全に置き換える。旧称への言及は `docs/prompts/` 以外に残さない。識別子 (定数名・テスト名) も置き換える。置き換えと文章の推敲は `agy --dangerously-skip-permissions` に依頼して進める ([Q-014](#q-014-既存の用語の置き換え範囲)、[Q-015](#q-015-コードの識別子の置き換え))。
- **文書**: `docs/ja/ssh-proxy.md`「SSH 経路プロキシ」を追加し、用語の正本、手順、資格情報、Tera Term のダイアログとの対応表、検証済みの範囲と制限を書く。Tera Term からの移行は対象外 ([Q-009](#q-009-新規文書の名前と範囲)、[Q-010](#q-010-tera-term-の設定の取り込み方))。
- **受入**: 自動テスト、Linux のコンテナー、Windows の実機のすべての検証を受入の条件とする ([Q-011](#q-011-受入とする検証の範囲))。

## 観点別の確認

| 観点 | 状態 | 根拠・関連 Q |
| --- | --- | --- |
| 目的・利用者 | 確認済み | ユーザーが明示した要求 |
| 範囲・境界 | 確認済み | [Q-001](#q-001-プロキシ方式の範囲)、[Q-002](#q-002-認証方式の範囲)、[Q-007](#q-007-プロキシを経由する段)、[Q-010](#q-010-tera-term-の設定の取り込み方) |
| 入力・根拠 | 確認済み | connect.c と TTProxy の原文との照合は最終仕様の作成時に行う (WBS 3.1)。[Q-004](#q-004-環境変数-http_proxy-の扱い) |
| 整合性・依存関係 | 確認済み | [Q-003](#q-003-実装形態)、[Q-006](#q-006-経路の定義場所)、[Q-008](#q-008-用語)、[Q-012](#q-012-インターネット向けプロキシの指す範囲)〜[Q-015](#q-015-コードの識別子の置き換え) |
| 実現性・例外 | 確認済み | [Q-003](#q-003-実装形態)、[Q-005](#q-005-資格情報の渡し方)、[Q-006](#q-006-経路の定義場所) |
| 受入条件・検証 | 確認済み | [Q-011](#q-011-受入とする検証の範囲) |

## 質問一覧

| Q-ID | 論点 | 状態 | 依存・関連 |
| --- | --- | --- | --- |
| [Q-001](#q-001-プロキシ方式の範囲) | プロキシ方式の範囲 | 解決済み | [Q-010](#q-010-tera-term-の設定の取り込み方) |
| [Q-002](#q-002-認証方式の範囲) | 認証方式の範囲 | 解決済み | [Q-005](#q-005-資格情報の渡し方) |
| [Q-003](#q-003-実装形態) | 実装形態 | 解決済み | [Q-006](#q-006-経路の定義場所) |
| [Q-004](#q-004-環境変数-http_proxy-の扱い) | 環境変数 HTTP_PROXY の扱い | 解決済み | [Q-008](#q-008-用語) |
| [Q-005](#q-005-資格情報の渡し方) | 資格情報の渡し方 | 解決済み | [Q-002](#q-002-認証方式の範囲) |
| [Q-006](#q-006-経路の定義場所) | 経路の定義場所 | 解決済み | [Q-003](#q-003-実装形態)、[Q-007](#q-007-プロキシを経由する段) |
| [Q-007](#q-007-プロキシを経由する段) | プロキシを経由する段 | 解決済み | [Q-006](#q-006-経路の定義場所) |
| [Q-008](#q-008-用語) | 用語 | 解決済み | [Q-004](#q-004-環境変数-http_proxy-の扱い)、[Q-009](#q-009-新規文書の名前と範囲) |
| [Q-009](#q-009-新規文書の名前と範囲) | 新規文書の名前と範囲 | 解決済み | [Q-008](#q-008-用語) |
| [Q-010](#q-010-tera-term-の設定の取り込み方) | Tera Term の設定の取り込み方 | 解決済み | [Q-001](#q-001-プロキシ方式の範囲) |
| [Q-011](#q-011-受入とする検証の範囲) | 受入とする検証の範囲 | 解決済み | なし |
| [Q-012](#q-012-インターネット向けプロキシの指す範囲) | インターネット向けプロキシの指す範囲 | 解決済み | [Q-008](#q-008-用語) |
| [Q-013](#q-013-英語の用語) | 英語の用語 | 解決済み | [Q-008](#q-008-用語)、[Q-012](#q-012-インターネット向けプロキシの指す範囲) |
| [Q-014](#q-014-既存の用語の置き換え範囲) | 既存の用語の置き換え範囲 | 解決済み | [Q-008](#q-008-用語)、[Q-015](#q-015-コードの識別子の置き換え) |
| [Q-015](#q-015-コードの識別子の置き換え) | コードの識別子の置き換え | 解決済み | [Q-014](#q-014-既存の用語の置き換え範囲) |

## 明確化のための確認項目

### Q-001 プロキシ方式の範囲

**Status**

解決済み

**Question**

connect 互換コマンドが対応するプロキシ方式を、HTTP CONNECT だけにしますか？

**Context**

connect.c は SOCKS4/4a/5 にも対応し、Tera Term のダイアログには HTTP のほかに TELNET、SOCKS4、SOCKS5 があります。今回の要求は、Tera Term の種別「HTTP」に当たる HTTP CONNECT の Basic 認証です。SOCKS まで含めると、実装・テスト・検証環境 (SOCKS サーバー) が増えます。対応しない方式を指定された場合は、明示的なエラーで終了させます。

```text
A: ProxyCommand host-shell connect -H proxy.example.com:8080 %h %p        # 対応
   ProxyCommand host-shell connect -S socks.example.com:1080 %h %p        # 「未対応の方式」で終了
B: 上の両方に対応
```

**Answer**

選択方式: 単一選択

- [x] A: (推奨) HTTP CONNECT だけに対応し、SOCKS などは未対応のエラーとする。
- [ ] B: HTTP CONNECT と SOCKS4/4a/5 に対応する。
- [ ] C: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足:

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-13 14:28:13 JST (記入日時は未取得)

**Assessment**

A の単一選択を確認した。[Q-010](#q-010-tera-term-の設定の取り込み方) の回答 (Tera Term のダイアログは例であり、移行は検討しない) とも矛盾しない。SOCKS など未対応の方式は、明示的なエラーで終了させる。

**Recommendation**

推奨: A  
理由: 要求の対象が HTTP CONNECT であり、検証できる範囲に限ることで「検証済」と言える範囲を明確にできます。SOCKS は必要になった時点で backlog から追加できます。

**Related**

[Q-010](#q-010-tera-term-の設定の取り込み方)

### Q-002 認証方式の範囲

**Status**

解決済み

**Question**

プロキシ認証は「認証なし」と「Basic」だけに対応しますか？

**Context**

Squid の Basic 認証 (`basic_ncsa_auth` など) が対象です。Tera Term の種別「HTTP」も Basic 認証だけに対応しています。企業のプロキシには NTLM や Negotiate (Kerberos) を要求するものもあります。Go の標準ライブラリはこれらに対応しておらず、対応には外部ライブラリと Windows の SSPI が必要です。Basic 認証は資格情報を Base64 で送るだけなので、プロキシまでの区間は平文と同等になります (プロキシとの間を TLS にする `https://` プロキシは connect.c も対応していません)。

**Answer**

選択方式: 単一選択

- [x] A: (推奨) 認証なしと Basic だけに対応する。NTLM・Negotiate・Digest と、プロキシとの TLS 接続は対象外とし、制限として文書に書く。
- [ ] B: A に加えて Digest に対応する。
- [ ] C: A に加えて NTLM・Negotiate に対応する。
- [ ] D: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足:

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-13 14:28:13 JST (記入日時は未取得)

**Assessment**

A の単一選択を確認した。Tera Term の種別「HTTP」も Basic だけであり、要求と一致する。NTLM 環境の迂回 (cntlm などのローカル中継) は文書で触れるが、検証は行わない。

**Recommendation**

推奨: A  
理由: 要求どおりで、標準ライブラリだけで実装できます。NTLM 環境は、cntlm などのローカル中継を手前に置けば Basic または認証なしとして扱えるため、文書にその旨を書けます (この迂回の検証は行いません)。

**Related**

[Q-005](#q-005-資格情報の渡し方)

### Q-003 実装形態

**Status**

解決済み

**Question**

connect 互換コマンドを、どの形で配布しますか？

**Context**

bridge を使う構成では、クライアントにはすでに `host-shell` があります (Linux / Windows / macOS 向けを `make dist` で生成)。`ssh` の `ProxyCommand` はクライアントで実行されるため、同じバイナリのサブコマンドにすれば配布物は増えません。一方、Git for Windows の `connect.exe` を前提に書かれた既存の `~/.ssh/config` をそのまま使うには、`connect` という名前で呼べる必要があります。

Windows の接続元では、利用ガイドの WSL の節と同じく、`ProxyCommand` に書く実行ファイルのパスも区切りを `/` で書く例とし、`\` の扱いは検証で確認します。

```text
A: ProxyCommand C:/Users/me/bin/host-shell.exe connect -H proxy.example.com:8080 %h %p
B: ProxyCommand C:/Users/me/bin/connect.exe -H proxy.example.com:8080 %h %p   (cmd/connect を別に配布)
C: A に加えて、host-shell.exe を connect.exe という名前でコピー・リンクすると B と同じ引数で動く
```

**Answer**

選択方式: 単一選択

- [x] A: (推奨) `host-shell connect` サブコマンドとして実装し、引数と環境変数を connect.c と互換にする。
- [ ] B: 独立したバイナリ (`connect`) として実装し、`dist/` に別途出力する。
- [ ] C: A に加えて、実行ファイル名が `connect` / `connect.exe` のときは connect として動作する。
- [ ] D: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足: このrepoの内容だけでsshを正しく接続すると考えれば、接続元の host-shell に HTTP 対応を導入することが接続形態としても綺麗である。

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-13 14:28:13 JST (記入日時は未取得)

**Assessment**

A の単一選択と補足を確認した。補足の「接続元の host-shell に HTTP 対応を導入する」は A と同じ方針であり、このリポジトリの成果物だけで経路プロキシ越しの SSH 接続を構成できることを目的として扱う。実行ファイル名で動作を切り替える案 (C) は採用しない。

**Recommendation**

推奨: A  
理由: 単一バイナリの配布方針を保てます。既存の `ssh_config` の書き換えは、`connect` を `host-shell connect` に置き換える 1 か所だけです。名前による切り替え (C) は便利ですが、ファイル名で動作が変わるのは分かりにくいため、要望があれば後から追加します。

**Related**

[Q-006](#q-006-経路の定義場所)

### Q-004 環境変数 HTTP_PROXY の扱い

**Status**

解決済み

**Question**

connect.c の `-h` (プロキシを環境変数 `HTTP_PROXY` から取る) に対応しますか？

**Context**

このリポジトリでは、`HTTP_PROXY` / `HTTPS_PROXY` はインターネットに出るためのプロキシを表します。bridge はこれをクライアント経由 HTTP プロキシの上流として読みます。connect の `-h` に対応すると、同じ `HTTP_PROXY` が SSH 経路のプロキシとしても使われ、要求にある「2 つのプロキシを分けて扱う」ことと衝突します。たとえば bridge の起動環境に `HTTP_PROXY` があると、SSH 経路とインターネット向けの両方が同じプロキシに向きます。両者がたまたま同じ Squid であっても、意図せずそうなる状態は避けたいところです。

```text
A: ProxyCommand host-shell connect -H proxy.example.com:8080 %h %p   # 経路プロキシは常に明示
   ProxyCommand host-shell connect -h %h %p                          # 「-h は未対応。-H で明示してください」で終了
B: -h も connect.c と同じく HTTP_PROXY を読む
```

**Answer**

選択方式: 単一選択

- [x] A: (推奨) `-h` には対応せず、経路プロキシは `-H` (または `-H` 相当の専用の指定) で明示させる。`HTTP_PROXY` は読まない。
- [ ] B: connect.c と同じく `-h` で `HTTP_PROXY` を読む。
- [ ] C: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足: 経路プロキシは経路ごとに設定可能とし、インターネットに出るためのプロキシとは別。

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-13 14:28:13 JST (記入日時は未取得)

**Assessment**

A の単一選択と補足を確認した。補足の「経路プロキシは経路ごとに設定可能」は [Q-006](#q-006-経路の定義場所) の A (Host ブロックごとの `ProxyCommand`) で満たせる。「インターネットに出るためのプロキシとは別」は、[Q-008](#q-008-用語) の用語の区別と一致する。

**Recommendation**

推奨: A  
理由: 2 種類のプロキシを、設定の段階で混ざらないようにできます。互換性は下がりますが、`-h` を使う既存の設定は `-H` への書き換えで済みます。

**Related**

[Q-008](#q-008-用語)

### Q-005 資格情報の渡し方

**Status**

解決済み

**Question**

プロキシのユーザー名とパスワードを、どの方法で渡せるようにしますか？

**Context**

MCP の構成では、エージェントが `host-shell bridge` → `ssh` → `ProxyCommand` を端末なしで起動します。connect.c のように対話でパスワードを尋ねる方法 (Unix の `/dev/tty`、Windows のダイアログ) は、この経路では使えません。

- 環境変数 (`HTTP_PROXY_USER` / `HTTP_PROXY_PASSWORD` など、connect.c と互換): エージェントの MCP 登録 (`claude mcp add -e` など) に書くと、その設定ファイルに平文で残ります。
- パスワード ファイル: 本人だけが読める権限 (Unix 0600、Windows は本人のみの ACL) を確認してから読みます。`ssh_config` の Host ブロックごとに別のファイルを指定でき、[Q-006](#q-006-経路の定義場所) の「接続先ごとの固有の経路」と相性がよい方法です。
- OS の資格情報ストア (Windows 資格情報マネージャー、libsecret、macOS キーチェーン): 平文ファイルを避けられますが、OS ごとの実装と検証が必要です。

```text
# A の例 (~/.ssh/config)
Host target-via-proxy
    HostName target.example.com
    ProxyCommand host-shell connect -H proxy-user@proxy.example.com:8080 --password-file ~/.config/host-shell/proxy-a.pass %h %p
```

**Answer**

選択方式: 単一選択

- [x] A: (推奨) connect.c 互換の環境変数と、権限を確認するパスワード ファイルの指定に対応する。端末があるときだけ対話入力も使う。
- [ ] B: connect.c 互換の環境変数と対話入力だけに対応する。
- [ ] C: A に加えて、OS の資格情報ストアに対応する。
- [ ] D: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足: ドキュメントへの案内は手厚く行うこと。

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-13 14:28:13 JST (記入日時は未取得)

**Assessment**

A の単一選択と補足を確認した。環境変数はプロセス全体に効くため、接続先ごとに異なる資格情報は、パスワード ファイルを Host ブロックごとに指定して扱う。補足の「文書への案内を手厚く」を受けて、新規文書 ([Q-009](#q-009-新規文書の名前と範囲)) に資格情報の章を設ける。章には、OS ごとのファイルの作り方と権限の設定、環境変数を MCP の登録に書いた場合に平文で残ること、パスワードを会話やログに貼らないことを書く。

**Recommendation**

推奨: A  
理由: 端末のない MCP の構成で動き、接続先ごとに別の資格情報を持てます。資格情報ストアは平文を避けられますが、3 OS の実装と検証が必要なため、backlog に残すのが妥当です。

**Related**

[Q-002](#q-002-認証方式の範囲)、[Q-006](#q-006-経路の定義場所)

### Q-006 経路の定義場所

**Status**

解決済み

**Question**

接続先ごとの SSH 経路プロキシを、どこに定義しますか？

**Context**

要求は「SSH 接続先ごとに固有の HTTP プロキシ経路を定義できること」です。候補は次の 3 つです。

- `~/.ssh/config` の Host ブロックに `ProxyCommand` を書く: OpenSSH 標準の方法です。`host-shell-connect` の `ROUTE` は Host 名を受け付けるので、スクリプトの変更なしで使えます。`ssh`、VS Code Remote - SSH、`scp` も同じ定義を共有します。スキルは Host ブロックを表示し、追加は利用者が行います (既存スキルの方針)。利用ガイドの「WSL の Linux への接続」も Host ブロック (`ProxyJump`、`Match host ... exec`) で経路を定義しており、同じ書き方で組み合わせられます (下の例の `win-target`)。
- `ROUTE` の記法を拡張し、プロキシを経路の要素として書く: `~/.ssh/config` を使わずに済みますが、MCP の登録コマンドに `-o ProxyCommand=...` が展開されて長くなり、他のツールとは共有できません。
- host-shell 独自の定義ファイル: 新しい形式と読み込み処理が必要です。

```text
# A: ~/.ssh/config
Host target-a
    HostName 10.0.0.5
    ProxyCommand host-shell connect -H proxy-a.example.com:8080 %h %p
Host target-b
    HostName 10.1.0.7
    ProxyCommand host-shell connect -H proxy-b.example.com:3128 %h %p
Host win-target                      # 利用ガイドの WSL の構成に経路プロキシを加えた場合
    HostName 192.0.2.10
    ProxyCommand host-shell connect -H proxy-a.example.com:8080 %h %p
Host wsl-target
    HostName 127.0.0.1
    Port 2222
    ProxyJump win-target

claude mcp add host-shell-a -- host-shell bridge -- ssh -T target-a /tmp/host-shell-1000/host-shell --mcp

# B: ROUTE 記法の拡張
remote.sh 'http://proxy-a.example.com:8080,target-a' true
claude mcp add host-shell-a -- host-shell bridge -- ssh -T -o 'ProxyCommand=host-shell connect -H proxy-a.example.com:8080 %h %p' 10.0.0.5 /tmp/host-shell-1000/host-shell --mcp
```

**Answer**

選択方式: 単一選択

- [x] A: (推奨) `~/.ssh/config` の Host ブロックの `ProxyCommand` で定義する。スキルはブロックを生成・表示し、追加は利用者が行う。
- [ ] B: `ROUTE` の記法を拡張し、登録コマンドに `-o ProxyCommand=...` を展開する。
- [ ] C: A を基本とし、`~/.ssh/config` を使えない場合の代替として B も用意する。
- [ ] D: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足:

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-13 14:28:13 JST (記入日時は未取得)

**Assessment**

A の単一選択を確認した。[Q-004](#q-004-環境変数-http_proxy-の扱い) の補足 (経路ごとの設定) と、既存スキルの「利用者の設定ファイルは変更しない」方針に整合する。

**Recommendation**

推奨: A  
理由: 標準の仕組みで、接続先ごとの定義が自然にできます。既存スキルの「利用者の設定ファイルは変更しない」方針とも整合します。

**Related**

[Q-003](#q-003-実装形態)、[Q-005](#q-005-資格情報の渡し方)、[Q-007](#q-007-プロキシを経由する段)

### Q-007 プロキシを経由する段

**Status**

解決済み

**Question**

SSH 経路プロキシを、クライアントから最初の段 (1 段目) にだけ置ける構成として扱いますか？

**Context**

多段接続には 2 つの形があります。

- 入れ子の `ssh -T hop1 ssh -T target ...` (host-shell の基本の形): 1 段目の `ProxyCommand` はクライアントで実行されます。2 段目以降 (hop1 → target) の間にプロキシがある場合は、hop1 の `~/.ssh/config` に `ProxyCommand` を書き、hop1 にも connect 互換コマンドを置く必要があります。資格情報も hop1 に置くことになります。
- `ProxyJump` (利用ガイドの WSL の構成): クライアントが 1 段目に接続し、その SSH 接続の TCP 転送で次の段へ届きます。1 段目の Host ブロックの `ProxyCommand` はそのまま効きます。hop1 から先は hop1 の TCP 転送で届くため、hop1 → target の間のプロキシはクライアントの `ProxyCommand` では扱えません。

どちらの形でも、クライアントから 1 段目への区間はクライアントの設定だけで扱えます。経路プロキシの区間は sshd の転送機能を使わないため、`AllowTcpForwarding no` の影響を受けません (`ProxyJump` の段は従来どおり転送が必要です)。

```text
A: client --(proxy)--> hop1 ----> target                 # 対象 (入れ子、ProxyJump の両方)
   client ----> hop1 --(proxy)--> target                 # 対象外 (制限として記載)
B: 上の両方を対象とし、host-shell-connect が hop1 へのコマンド配置も行う
```

**Answer**

選択方式: 単一選択

- [x] A: (推奨) 1 段目だけを対象とする。2 段目以降は対象外として制限に書く。
- [ ] B: 任意の段を対象とし、中継サーバーへのコマンド配置と設定の手順も用意する。
- [ ] C: 任意の段を対象とするが、中継サーバーへの配置と設定は利用者が行う (手順を文書に書くだけ)。
- [ ] D: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足:

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-13 14:28:13 JST (記入日時は未取得)

**Assessment**

A の単一選択を確認した。入れ子の `ssh -T` と `ProxyJump` のどちらでも、1 段目の区間を対象とする。2 段目以降のプロキシは制限として文書に書く。

**Recommendation**

推奨: A  
理由: Tera Term のプロキシ設定は手元の PC からの 1 段目の接続に相当します。中継サーバーへの資格情報の配置は、リスクと手順の両面で別の判断が必要です。

**Related**

[Q-006](#q-006-経路の定義場所)

### Q-008 用語

**Status**

解決済み

**Question**

文書とスキルで使う用語を、次の案 A で統一しますか？

**Context**

既存の文書には、すでに 2 つの「プロキシ」があります。`client-proxy.md` は「上流プロキシ」と「中継プロキシ」の語をすでに使っており、案 A はこれをそのまま生かします。今回 1 つ加わるため、3 つを区別する名前が必要です。

| 案 A の用語 | 指すもの | 設定する場所 |
| --- | --- | --- |
| SSH 経路プロキシ | SSH 接続そのものが通る HTTP プロキシ (Tera Term のプロキシ設定に相当) | `~/.ssh/config` の `ProxyCommand` |
| 上流プロキシ | bridge がインターネットに出るときに使うクライアント側のプロキシ | bridge の起動環境の `HTTP_PROXY` / `HTTPS_PROXY` |
| 中継プロキシ | 接続先の `127.0.0.1` で待ち受け、bridge へ中継する認証付きプロキシ (クライアント経由 HTTP プロキシ) | `--proxy` / `--client-proxy` |

実際の環境では、SSH 経路プロキシと上流プロキシが同じ Squid であることもあります。その場合も、設定は別々に行います。

**Answer**

選択方式: 単一選択

- [ ] A: (推奨) 上の表の 3 つの用語で統一する。
- [ ] B: 「SSH 経路プロキシ」と「インターネット向けプロキシ」(上流プロキシと中継プロキシの総称) の 2 つにし、中継プロキシは機能名「クライアント経由 HTTP プロキシ」で呼ぶ。
- [x] C: その他 (用語を指定)
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足: SSH 経路プロキシ, インターネット向けプロキシ, 接続先中継プロキシ

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-13 14:28:13 JST (記入日時は未取得)

**Assessment**

C の単一選択と、補足の用語 (SSH 経路プロキシ、インターネット向けプロキシ、接続先中継プロキシ) を確認した。「SSH 経路プロキシ」は案 A と同じものを指す。「接続先中継プロキシ」は案 A の「中継プロキシ」(接続先の `127.0.0.1` のプロキシ) に当たると解釈できる。一方、「インターネット向けプロキシ」がクライアント側のプロキシ (案 A の「上流プロキシ」) だけを指すか、インターネットへ出る経路全体の総称かは、補足からは決められないため [Q-012](#q-012-インターネット向けプロキシの指す範囲) で確認する。英語の用語は [Q-013](#q-013-英語の用語)、既存の文書の語の置き換えは [Q-014](#q-014-既存の用語の置き換え範囲) で確認する。

**Recommendation**

推奨: A  
理由: 設定する場所がそれぞれ異なるため、3 つに名前があるほうが、問い合わせと手順を対応付けやすくなります。

**Related**

[Q-004](#q-004-環境変数-http_proxy-の扱い)、[Q-009](#q-009-新規文書の名前と範囲)、[Q-012](#q-012-インターネット向けプロキシの指す範囲)、[Q-013](#q-013-英語の用語)、[Q-014](#q-014-既存の用語の置き換え範囲)

### Q-009 新規文書の名前と範囲

**Status**

解決済み

**Question**

追加する文書を `docs/ja/ssh-proxy.md`「SSH 経路プロキシ」とし、次の範囲を書きますか？

**Context**

ユーザーから `docs/ja/` に 1 ファイルを追加する方針が示されています。名前と範囲の案です。

- 用途: Tera Term のプロキシ設定で接続している環境で、host-shell を使う。
- 用語: 3 つのプロキシの区別 ([Q-008](#q-008-用語)) と、「プロキシ」がどちらか分からないときの確認事項。
- 手順: connect 互換コマンド、`~/.ssh/config` の Host ブロック、資格情報、MCP の登録、無通信切断への対策 (`ServerAliveInterval`)。
- Tera Term の設定項目との対応表。
- 検証済みの範囲と制限。

用語の定義はこの文書を正本とし、`client-proxy.md` などからは参照します。

**Answer**

選択方式: 単一選択

- [x] A: (推奨) `docs/ja/ssh-proxy.md`、題名「SSH 経路プロキシ」で、上の範囲を書く。
- [ ] B: 範囲は A と同じで、名前を変える (補足に記入)。
- [ ] C: 用語の定義は別の文書 (例: `user-guide.md`) に置き、新規文書は手順だけにする。
- [ ] D: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足:

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-13 14:28:13 JST (記入日時は未取得)

**Assessment**

A の単一選択を確認した。題名「SSH 経路プロキシ」は [Q-008](#q-008-用語) の用語と一致する。用語の正本はこの文書に置く。

**Recommendation**

推奨: A  
理由: 既存の文書名 (`client-proxy.md`、`port-forward.md`) と並べたときに、対象が分かる名前です。用語の正本を新規文書に置けば、どちらのプロキシかを確認するときに参照する先が 1 か所になります。

**Related**

[Q-008](#q-008-用語)

### Q-010 Tera Term の設定の取り込み方

**Status**

解決済み

**Question**

Tera Term のプロキシ設定は、文書の対応表で利用者が `~/.ssh/config` に書き写す方法で扱いますか？

**Context**

Tera Term の設定 (TTProxy) は、設定ファイル (`TERATERM.INI`) やコマンド ラインの指定に保存されます。扱い方は 2 通りあります。

- 対応表: 「Tera Term の項目 → `ProxyCommand` の引数」を文書に載せ、Host ブロックはスキルが生成します。
- 読み込み: `TERATERM.INI` などからプロキシ設定を読み取り、Host ブロックを生成するヘルパーを作ります。保存形式 (パスワードの保存方式を含む) の調査と、Tera Term の版ごとの検証が必要です。

**Answer**

選択方式: 単一選択

- [ ] A: (推奨) 文書の対応表とスキルによる Host ブロックの生成で扱う。Tera Term の設定ファイルは読まない。
- [ ] B: A に加えて、Tera Term の設定ファイルを読んで Host ブロックを生成するヘルパーを作る。
- [x] C: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足: Tera Term の設定ダイアログは例であり、移行そのものは検討しなくてよい。ダイアログの各項目がどの設定に対応するかは、`docs/ja/ssh-proxy.md` に記載があると良い。

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-13 14:28:13 JST (記入日時は未取得)

**Assessment**

C の単一選択と補足を確認した。Tera Term の設定からの移行 (設定ファイルの読み込みやヘルパー) は対象外とする。Tera Term のダイアログの各項目と、`ProxyCommand` の引数・資格情報の設定との対応表を `docs/ja/ssh-proxy.md` に載せる。Host ブロックの生成と表示は、[Q-006](#q-006-経路の定義場所) の A によりスキルが行う。

**Recommendation**

推奨: A  
理由: 設定ファイルの形式に依存しないため、Tera Term の版が変わっても影響を受けません。ヘルパーは、利用者の数が多い場合に backlog から追加できます。

**Related**

[Q-001](#q-001-プロキシ方式の範囲)

### Q-011 受入とする検証の範囲

**Status**

解決済み

**Question**

受入に必要な検証を、次の案 A の範囲としますか？

**Context**

この WSL 上の Podman で、外に出る道が Squid だけのネットワークを作れます。Windows の実機は、Windows の接続先・WSL の検証で使っている Hyper-V の仮想マシン (Windows 11 Enterprise) を使えますが、Windows 側のセッションでの作業が必要です。Windows の接続元の ssh には、Git for Windows の OpenSSH と Windows の OpenSSH の 2 つがあり、利用ガイドの WSL の節では両方で確認しています。

案 A の範囲:

- 自動テスト: 試験用の CONNECT プロキシを使い、認証の成否 (200 / 407)、拒否 (403)、接続失敗、8 ビット透過、終了処理を確認する (全 OS の CI)。
- コンテナー (Linux): Squid (Basic 認証) の内側の sshd に、`ProxyCommand` を使って host-shell を配置・登録し、MCP で操作する。多段 (1 段目がプロキシ経由。入れ子の `ssh -T` と `ProxyJump` の両方)、bridge の `--proxy` (上流プロキシも同じ Squid) との併用、Squid の無通信切断と `ServerAliveInterval` を確認する。
- Windows の実機: Git for Windows の OpenSSH と Windows の OpenSSH の両方から `host-shell.exe connect` を使った接続と、同じ Squid に Tera Term で接続できることを確認する。

**Answer**

選択方式: 単一選択

- [x] A: (推奨) 自動テスト、Linux のコンテナー、Windows の実機のすべてを受入の条件とする。
- [ ] B: 自動テストと Linux のコンテナーを受入の条件とし、Windows の実機は「未検証」と書いて backlog に残す。
- [ ] C: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足:

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-13 14:28:13 JST (記入日時は未取得)

**Assessment**

A の単一選択を確認した。Windows の実機での検証は Windows 側のセッションでの作業が必要なため、WBS に Windows 側での検証の項目を設ける。受入には、Linux と Windows の両方の結果がそろうことを要する。

**Recommendation**

推奨: A  
理由: 主な利用者は Tera Term を使う Windows 環境であり、Windows の OpenSSH と `ProxyCommand` の組み合わせは実機で確認しないと「検証済」と書けません。ただし、Windows 側のセッションでの作業が必要になるため、日程の都合で B を選ぶこともできます。

### Q-012 インターネット向けプロキシの指す範囲

**Status**

解決済み

**Question**

「インターネット向けプロキシ」は、bridge がインターネットに出るときに使うクライアント側のプロキシ (bridge の起動環境の `HTTP_PROXY` / `HTTPS_PROXY`) だけを指す語としますか？

**Context**

[Q-008](#q-008-用語) で、用語は「SSH 経路プロキシ」「インターネット向けプロキシ」「接続先中継プロキシ」に決まりました。「接続先中継プロキシ」は接続先の `127.0.0.1` のプロキシです。当初の要求の「インターネットに出るためのプロキシ経路」は、接続先のアプリの通信が外へ出る経路全体とも読めるため、「インターネット向けプロキシ」の範囲を確認します。

```text
A: 接続先のアプリ → 接続先中継プロキシ → (SSH) → bridge → インターネット向けプロキシ → インターネット
                                                   (bridge の HTTP_PROXY。ない場合は直接接続)
B: 接続先のアプリ → [ インターネット向けプロキシ (接続先中継プロキシ + bridge + クライアント側のプロキシ) ] → インターネット
   (B ではクライアント側のプロキシに別の名前が必要になる)
```

**Answer**

選択方式: 単一選択

- [x] A: (推奨) クライアント側のプロキシ (bridge の起動環境の `HTTP_PROXY` / `HTTPS_PROXY`) だけを指す。
- [ ] B: インターネットへ出る経路全体の総称とし、クライアント側のプロキシには別の名前を付ける (補足に記入)。
- [ ] C: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足:

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-13 14:32:06 JST (記入日時は未取得)

**Assessment**

A の単一選択を確認した。3 つの用語は、それぞれ次の設定場所に対応する: SSH 経路プロキシ (`~/.ssh/config` の `ProxyCommand`)、インターネット向けプロキシ (bridge の起動環境の `HTTP_PROXY` / `HTTPS_PROXY`)、接続先中継プロキシ (`--proxy` / `--client-proxy`)。

**Recommendation**

推奨: A  
理由: 3 つの語がそれぞれ 1 つの設定場所 (`~/.ssh/config` の `ProxyCommand`、bridge の起動環境、`--proxy` / `--client-proxy`) に対応し、手順と問い合わせを結び付けやすくなります。SSH 経路プロキシとインターネット向けプロキシが同じ Squid の場合も、設定は別々に書くことを文書で説明します。

**Related**

[Q-008](#q-008-用語)、[Q-013](#q-013-英語の用語)

### Q-013 英語の用語

**Status**

解決済み

**Question**

`docs/en/` とスキル (英語) で使う用語を、案 A にしますか？

**Context**

[Q-008](#q-008-用語) の 3 つの用語の英訳が必要です。現在の英語の文書では、`docs/en/client-proxy.md` が "upstream proxy" と "relay proxy" を使い、VS Code Remote - SSH のスキルとスクリプトが "relay proxy" を使っています。機能名は "client proxy" ("HTTP proxy through the client") です。

| 日本語 | 案 A | 案 B (既存の英語を残す) |
| --- | --- | --- |
| SSH 経路プロキシ | SSH route proxy | SSH route proxy |
| インターネット向けプロキシ | internet proxy | upstream proxy |
| 接続先中継プロキシ | target relay proxy | relay proxy |

**Answer**

選択方式: 単一選択

- [x] A: (推奨) SSH route proxy、internet proxy、target relay proxy とする。
- [ ] B: SSH route proxy を加え、既存の upstream proxy と relay proxy を残す。
- [ ] C: その他 (用語を指定)
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足:

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-13 14:32:06 JST (記入日時は未取得)

**Assessment**

A の単一選択を確認した。英語の用語は SSH route proxy、internet proxy、target relay proxy とする。

**Recommendation**

推奨: A  
理由: 日本語の用語と 1 対 1 に対応し、英語の文書だけを読む利用者やエージェントにも同じ区別が伝わります。"upstream" は、上流の CI や上流のプロジェクトなど、プロキシ以外の意味でも使われているためです。

**Related**

[Q-008](#q-008-用語)、[Q-012](#q-012-インターネット向けプロキシの指す範囲)、[Q-014](#q-014-既存の用語の置き換え範囲)

### Q-014 既存の用語の置き換え範囲

**Status**

解決済み

**Question**

既存の「上流プロキシ」「中継プロキシ」(英語の upstream proxy、relay proxy) を、どの範囲で新しい用語に置き換えますか？

**Context**

現在の使用箇所は次のとおりです (プロキシを指すものだけ)。

- 日本語の文書: `client-proxy.md` (上流プロキシ 3 か所、中継プロキシ 3 か所)、`code-server.md` (中継プロキシ 1 か所)
- 英語の文書: `docs/en/client-proxy.md` (upstream proxy、relay proxy、local relay proxy)
- スキル: `host-shell-vscode-remote-ssh/SKILL.md` (relay proxy 5 か所)
- スクリプト: `host-shell-vscode-remote-ssh/scripts/linux/start.sh` (コメントと、`relay proxy: passed to SSH sessions` などの出力の文字列)
- コード: `internal/clientproxy/` のコメントと定数 (`reasonUpstream` の `upstream connection failed` など)。`proxy_status` と操作ログに出る文字列も含む。

文書とスキルだけを置き換えると、出力の文字列やログの理由に古い語が残ります。ログの理由の文字列を変えると、それを照合する利用者の手順やテストに影響します。

**Answer**

選択方式: 単一選択

- [ ] A: (推奨) 文書 (`docs/ja/`、`docs/en/`、`README.md`、`docs/README.md`)、すべての `SKILL.md`、埋め込み help を置き換える。スクリプトの出力、コードのコメント、識別子、ログ・`proxy_status` の文字列は変えない。
- [ ] B: A に加えて、スクリプトの出力の文字列も置き換える。
- [x] C: A に加えて、スクリプトの出力、コードのコメント、ログ・`proxy_status` の文字列も置き換える。
- [ ] D: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足: 完全に置換し、経緯は prompts 以外に残さないこと。

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-13 14:32:06 JST (記入日時は未取得)

**Assessment**

C の単一選択と補足 (完全に置換し、経緯は prompts 以外に残さない) を確認した。文書、`SKILL.md`、埋め込み help、スクリプトの出力、コードのコメント、ログ・`proxy_status` の文字列を置き換える。旧称への言及や「旧名」の注記は、文書やコードには残さない (経緯はこの検討文書と、後続の design・validation だけに残す)。文書中の派生語 (「上流接続」、upstream connection など、プロキシの意味で使っているもの) も置き換えの対象とする。プロキシ以外の意味の「上流」(上流の CI、upstream のプロジェクト) は対象外とする。

C の選択肢はコードの識別子に触れておらず、補足の「完全に置換」が識別子 (定数名やテスト名) まで含むかは決められないため、[Q-015](#q-015-コードの識別子の置き換え) で確認する。

なお、ログの理由 `upstream connection failed` は、インターネット向けプロキシ経由の失敗だけでなく、bridge から宛先への直接接続の失敗も表す ([gateway.go](../../../internal/clientproxy/gateway.go) の `reasonUpstream`)。置き換え後の文字列は、この意味を保つものを最終仕様で定める。

**Recommendation**

推奨: A  
理由: 利用者とエージェントが読む説明を統一でき、実行時の出力やログの互換性には影響しません。`upstream connection failed` などの理由の文字列は、文書の中で「インターネット向けプロキシまたは宛先への接続の失敗」と説明します。

**Related**

[Q-008](#q-008-用語)、[Q-013](#q-013-英語の用語)、[Q-015](#q-015-コードの識別子の置き換え)

### Q-015 コードの識別子の置き換え

**Status**

解決済み

**Question**

用語の置き換えに、Go のコードの識別子 (定数名・関数名・テスト名) も含めますか？

**Context**

[Q-014](#q-014-既存の用語の置き換え範囲) で C (文書・スキル・help・スクリプトの出力・コメント・ログの文字列) と、補足「完全に置換」が選ばれました。C の選択肢は識別子に触れていないため、確認します。対象になる識別子は次のとおりで、いずれも `internal/clientproxy/` の中だけで使われています (外部に公開する API ではありません)。

```text
const reasonUpstream = "upstream connection failed"   // gateway.go (target.go からも参照)
func TestUpstreamHTTPAndCONNECT(t *testing.T)          // proxy_test.go
func TestHTTPSUpstream(t *testing.T)                   // proxy_test.go
```

テスト内のローカル変数 (`upstream` など) も同じ扱いにします。標準ライブラリや外部の名前 (`http.Transport.Proxy`、環境変数 `HTTP_PROXY`) は変えません。

```text
A: reasonUpstream → reasonInternet (仮)、TestUpstreamHTTPAndCONNECT → TestInternetProxyHTTPAndCONNECT (仮) のように置き換える
B: 識別子はそのまま残し、コメントと文字列だけを置き換える
```

**Answer**

選択方式: 単一選択

- [x] A: (推奨) 識別子も置き換える (名前は最終仕様で定める)。
- [ ] B: 識別子は置き換えない。
- [ ] C: その他
- [ ] X: 意図不明 (詳細な質問内容に再生成すること)

その他の回答 / 補足: 置換系、文章の推敲は、agy --dangerously-skip-permissions で依頼して推進させる。

回答者: Tetsuo Honda (文書内のチェック。対話「書いた」で通知)

回答確認日時: 2026-11-13 14:36:28 JST (記入日時は未取得)

**Assessment**

A の単一選択と補足を確認した。`internal/clientproxy/` の識別子 (定数名・テスト名・テスト内の変数) も置き換え、名前は最終仕様で定める。補足により、置き換えと文章の推敲は `agy --dangerously-skip-permissions` に依頼して進める。依頼の内容と結果の差分の確認は、この案件の担当エージェントが行う (WBS 4.7)。

**Recommendation**

推奨: A  
理由: 補足の「完全に置換」に沿い、コメントと識別子で語が食い違う状態を避けられます。パッケージの内部だけの名前なので、影響は `internal/clientproxy/` の中に限られます。

**Related**

[Q-014](#q-014-既存の用語の置き換え範囲)

## 対象成果物と合意版

最終仕様の設計書: `261113-ssh-proxy-design.md`

検討用 Markdown の合意版 (第 1 ゲート): 版 6

最終仕様の合意版 (第 2 ゲート): 版 3 (2026-11-13 承認)

更新予定の成果物: 最終仕様の「実装」「スキル」「文書」の節を参照

受入条件: 最終仕様の「受入条件」の節を参照

## 反映と検証の記録

未着手。
