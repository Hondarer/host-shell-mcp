<!--
生成元: agree-before-implement スキル
文書種別: 最終仕様 (design)
最終更新エージェント: claude code / セッション ID: session_01KJc53U8RTJvttMFzQ3TZ55
上流文書: 261113-ssh-proxy-discussion.md (検討用 Markdown)。削除されている場合があります。
再開手順: /home/user/c-modernization-kit/app/general/.agents/skills/agree-before-implement/SKILL.md を読み込んでください。上流文書が存在する場合は、上流文書の再開手順に従って進行位置を確認し、判断の経緯を上流文書の Q&A で確認します。存在しない場合は、この文書を単体の最終仕様として扱います。
-->

# SSH 経路プロキシ 最終仕様

版: 3

状態: 承認済み (2026-11-13)

## 目的と範囲

SSH 接続そのものが認証付きの HTTP プロキシ (HTTP CONNECT、Basic 認証。Squid 相当) を通る環境で、host-shell の接続を構成できるようにする。Tera Term のプロキシ設定 (種別「HTTP」) で接続している環境と同じ経路を、OpenSSH の `ssh` とこのリポジトリの成果物だけで組めることが目的である。

| 区分 | 内容 |
| --- | --- |
| 対象 | `host-shell connect` サブコマンド (connect.c と互換の `ProxyCommand` 用中継)、`~/.ssh/config` の Host ブロックによる接続先ごとの経路の定義、資格情報の扱い、3 種類のプロキシの用語の定義と全面的な置き換え、スキルの問い合わせ手順、新規文書 `ssh-proxy.md` (日英)、検証 |
| 対象外 | SOCKS4/4a/5・TELNET 形式のプロキシ、Basic 以外の認証 (NTLM、Negotiate、Digest)、プロキシとの TLS 接続 (`https://` プロキシ)、302 による転送先の追従、クライアントから 2 段目以降の区間のプロキシ、Tera Term の設定からの移行 (設定ファイルの読み込み)、OS の資格情報ストア、`SSH_ASKPASS` による入力 |

## 用語

文書・スキル・help・スクリプトの出力・コードのコメント・ログの文字列・識別子で、次の 3 つの用語を使う。用語の正本は `docs/ja/ssh-proxy.md` とする。

| 用語 | 英語 | 指すもの | 設定する場所 |
| --- | --- | --- | --- |
| SSH 経路プロキシ | SSH route proxy | SSH 接続そのものが通る HTTP プロキシ (Tera Term のプロキシ設定に相当) | クライアントの `~/.ssh/config` の Host ブロックの `ProxyCommand` |
| インターネット向けプロキシ | internet proxy | bridge がインターネットに出るときに使う、クライアント側のプロキシ | bridge の起動環境の `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` |
| 接続先中継プロキシ | target relay proxy | 接続先の `127.0.0.1` で待ち受け、bridge へ中継する認証付きプロキシ (機能名: クライアント経由 HTTP プロキシ) | bridge の `--proxy`、接続先の `--client-proxy` |

> [!NOTE]
> SSH 経路プロキシとインターネット向けプロキシが同じ Squid であっても、設定は別々に行う。`host-shell connect` は `HTTP_PROXY` を読まず、bridge は `ProxyCommand` を参照しないため、片方の設定がもう片方へ波及することはない。

## host-shell connect

### 呼び出し

```text
host-shell connect [-d] [-w SECONDS] [--password-file PATH] -H [http://][USER@]PROXY[:PORT] HOST PORT
host-shell connect -V
```

`~/.ssh/config` での使い方:

```text
Host target-a
    HostName 10.0.0.5
    ProxyCommand /home/alice/.local/bin/host-shell connect -H alice@proxy-a.example.com:8080 --password-file /home/alice/.config/host-shell/proxy-a.pass %h %p
    ServerAliveInterval 30
```

Windows の接続元では、実行ファイルとパスワード ファイルのパスの区切りを `/` で書く (例: `C:/Users/alice/bin/host-shell.exe`)。

### 引数

| 引数 | connect.c との関係 | 動作 |
| --- | --- | --- |
| `-H SPEC` | 互換 | SSH 経路プロキシ。`SPEC` は `[http://][USER@]HOST[:PORT]`。`USER@` は最後の `@` で区切る。`PORT` の既定は 80。IPv6 アドレスは `[...]` で囲む。`/` 以降は無視する。必須 |
| `-w SECONDS` | 互換 (connect.c の Windows 版にはない) | プロキシへの TCP 接続から `CONNECT` の応答までの上限。既定は 30 秒、`0` で上限なし。中継の開始後には適用しない |
| `-d` | 互換 | 診断を標準エラーに出す。繰り返し指定できる (2 回以上で応答ヘッダーも出す)。資格情報と `Proxy-Authorization` の値は出さない |
| `-V` | 互換 | 版を標準エラーに出して終了コード 0 で終了する |
| `--password-file PATH` | 独自 | パスワードを読むファイル ([資格情報](#資格情報)) |
| `HOST PORT` | 互換 | `CONNECT` の宛先 (`ProxyCommand` の `%h %p`)。IPv6 アドレスは `[...]` で囲んで送る |
| `-h`、`-S`、`-s`、`-T`、`-t`、`-c`、`-4`、`-5`、`-a`、`-R`、`-p`、`-P`、`-n`、`-D` | connect.c にあるが対応しない | `host-shell connect: option -X is not supported (only HTTP CONNECT through -H is supported)` を出して終了コード 1 で終了する。`-h` は「SSH 経路プロキシは `-H` で明示する」旨も出す |

> [!NOTE]
> connect.c は GNU GPL v2 以降で配布されている。`host-shell connect` は引数・環境変数・終了コードの互換だけを実装し、connect.c のコードは使わない。

### 環境変数

| 環境変数 | 用途 | 優先順位 |
| --- | --- | --- |
| `HTTP_PROXY_USER`、`CONNECT_USER` | ユーザー名 (`-H` に `USER@` がないとき) | `-H` の `USER@` > `HTTP_PROXY_USER` > `CONNECT_USER` |
| `HTTP_PROXY_PASSWORD`、`CONNECT_PASSWORD` | パスワード | `--password-file` > `HTTP_PROXY_PASSWORD` > `CONNECT_PASSWORD` > 対話入力 |

`HTTP_PROXY`、`HTTPS_PROXY`、`NO_PROXY`、`HTTP_DIRECT`、`CONNECT_DIRECT`、`SSH_ASKPASS`、および `~/.connectrc` は読まない。ユーザー名がどこにもない場合は認証を行わない (connect.c のように OS のユーザー名を補わない)。

### プロキシとの手順

1. ユーザー名があれば、接続の前にパスワードを [資格情報](#資格情報) の順に得る。得られなければ、接続せずに終了コード 1 で終了する。
2. `-H` のプロキシへ TCP で接続する (TCP keepalive を有効にする)。
3. 次の要求を送る。ユーザー名があれば、最初から `Proxy-Authorization` を付ける (Tera Term と同じ)。ユーザー名がなければ付けない。

    ```text
    CONNECT HOST:PORT HTTP/1.1
    Host: HOST:PORT
    User-Agent: host-shell/<版>
    Proxy-Authorization: Basic <base64(USER:PASSWORD)>
    ```

4. 応答の状態行とヘッダーを読む (ヘッダーの合計は 64 KiB まで)。
    - **200 番台**: ヘッダーの終わりまで読み、中継を始める。ヘッダーの後に続けて届いたバイト列も、そのまま標準出力へ渡す。
    - **407**: 再送せず、ただちに失敗とする。資格情報を送った場合は資格情報の誤り、送っていない場合は認証が必要である旨を出す。
    - **それ以外**: 失敗とする。
5. 中継は双方向で行う。標準入力の終わりで TCP の送信側を閉じ (half-close)、プロキシ側の終わりで標準出力を閉じて終了する。バイト列は変換しない。

### 終了コードと診断

成功時 (中継の終了) は 0、失敗時は 1。失敗時は標準エラーに 1 行で理由を出す。資格情報は出さず、プロキシのホスト・ポートとユーザー名だけを含める。

| 状況 | 標準エラーの例 |
| --- | --- |
| プロキシに接続できない | `host-shell connect: cannot connect to SSH route proxy proxy.example.com:8080: <OS のエラー>` |
| 407 (資格情報を送った場合) | `host-shell connect: SSH route proxy proxy.example.com:8080 rejected the credentials of alice (407)` |
| 407 (ユーザー名がない場合) | `host-shell connect: SSH route proxy proxy.example.com:8080 requires authentication (407); give USER@ in -H or HTTP_PROXY_USER` |
| 403 などの拒否 | `host-shell connect: SSH route proxy proxy.example.com:8080 refused CONNECT target.example.com:22 (403 Forbidden)` |
| 上限時間の超過 | `host-shell connect: timed out after 30s waiting for SSH route proxy proxy.example.com:8080` |
| パスワードが得られない | `host-shell connect: no password for alice@proxy.example.com:8080; use --password-file or HTTP_PROXY_PASSWORD` |

## 資格情報

パスワードは次の順に探す。

1. **`--password-file PATH`**: ファイルの 1 行目 (末尾の CR・LF を除く) をパスワードとする。読む前に [パスワード ファイルの権限](#パスワード-ファイルの権限) を確認し、満たさなければ理由を出して終了コード 1 で終了する。
2. **`HTTP_PROXY_PASSWORD`、`CONNECT_PASSWORD`**: プロセス全体に効くため、接続先ごとに異なるパスワードには使えない。
3. **対話入力**: 標準エラーが端末で、かつ端末 (Unix は `/dev/tty`、Windows は `CONIN$`) を開ける場合だけ、エコーを止めて `Enter password for SSH route proxy alice@proxy.example.com:8080: ` を表示して読む。MCP の構成 (エージェントが bridge と ssh を端末なしで起動し、標準エラーがパイプになる) では尋ねない。

### パスワード ファイルの権限

パスワード ファイルは、OpenSSH が利用者の `~/.ssh/config` に求めるものと同じ基準で確認する。基準は、`readconf.c` の `read_config_file_depth` (`SSHCONF_CHECKPERM`) と、Windows の OpenSSH (PowerShell/openssh-portable) がそこで呼ぶ `check_secure_file_permission` (`contrib/win32/win32compat/w32-sshfileperm.c`、`read_ok` = 1) である。ほかの利用者による書き換えは拒否し、読み取りは拒否しない。

| OS | 条件 |
| --- | --- |
| Linux・macOS | 所有者が `root` または実行ユーザーであり、グループとその他に書き込み権限がない (`mode & 022` が 0) |
| Windows | 所有者が実行ユーザー、`BUILTIN\Administrators`、`SYSTEM`、`NT SERVICE\TrustedInstaller` のいずれかである。DACL のアクセス許可エントリ (ACCESS_ALLOWED_ACE) のうち、対象がこの 4 つ以外のものは、書き込み系の権限 (`FILE_WRITE_DATA`、`FILE_WRITE_ATTRIBUTES`、`FILE_WRITE_EA`、`FILE_APPEND_DATA`、`WRITE_DAC`、`WRITE_OWNER`、`DELETE`) を含まない。継承されたエントリも同じに扱い、拒否エントリと、それ以外の種類のエントリは見ない。対象の SID が異なっても、名前・ドメイン・種別が実行ユーザーと一致すれば本人として扱う (SID 履歴への対応) |

満たさないときは、理由を出して終了コード 1 で終了する。Windows では、OpenSSH と同じく最初に見つかった対象を示す (例: `host-shell connect: bad owner or permissions on password file C:/tools/proxy-a.pass: BUILTIN\Users has write access`)。

文書の手順では、ほかの利用者が読めない状態でファイルを作る方法を案内する (Linux・macOS は `chmod 600`、Windows は `%USERPROFILE%\.ssh\` の下に作る。プロファイルの下のファイルは、既定の継承で `SYSTEM`、`Administrators`、本人だけになる)。確認の基準は満たすが、ほかの利用者が読める状態 (例: Linux の `644`、Windows の `Users` の読み取り) では、ほかの利用者にパスワードが読まれることも文書に書く。

> [!NOTE]
> OpenSSH は秘密鍵にはより厳しい確認 (Unix では `mode & 077`、Windows では `read_ok` = 0 で、ほかの利用者の読み取りだけの許可も拒否する) を使う。パスワード ファイルは、利用者がほかの設定と同じ扱いで置けるよう、`~/.ssh/config` の基準に合わせる。

> [!IMPORTANT]
> 環境変数をエージェントの MCP 登録 (`claude mcp add -e` など) に書くと、その設定ファイルに平文で残る。接続先ごとに異なる資格情報と MCP の構成には、`--password-file` を使う。Basic 認証はプロキシまでの区間を平文と同等に送る。

## 経路の定義

- 接続先ごとの SSH 経路プロキシは、クライアントの `~/.ssh/config` の Host ブロックに `ProxyCommand` で書く。`host-shell-connect` の `ROUTE`、MCP の登録コマンド、VS Code Remote - SSH は Host 名で参照する。
- 対象はクライアントから 1 段目の区間だけとする。入れ子の `ssh -T hop1 ssh -T target` と `ProxyJump` のどちらでも、1 段目の Host ブロックの `ProxyCommand` が効く。2 段目以降の区間のプロキシは対象外とし、制限として書く。
- SSH 経路プロキシの区間は sshd の転送機能を使わないため、`AllowTcpForwarding no` の影響を受けない (`ProxyJump` の段は従来どおり転送が必要)。
- 無通信で CONNECT のトンネルを切るプロキシへの対策として、Host ブロックに `ServerAliveInterval 30` を書くことを推奨設定とする (無通信の間も接続が維持される設定例として示す)。

## スキル

| スキル | 変更 |
| --- | --- |
| `host-shell-connect` | 「SSH route proxy」の節を設ける。経路にプロキシがある場合に、プロキシのホスト・ポート・ユーザー名、パスワード ファイルの置き場所を確認し、Host ブロックを生成して表示する (追加は利用者が行う)。パスワードを会話・コマンド ラインに書かせない。パスワード ファイルの作り方 (OS ごとの権限の設定) を案内する。`remote.sh` などのスクリプトは変更しない |
| すべてのスキル | 利用者が「プロキシ」と言い、SSH 接続そのものが通るものか、接続先のアプリがインターネットへ出るためのものかが文脈から決まらない場合は、作業の前に利用者に尋ねる。質問には 3 つの用語と設定場所を添える |
| `host-shell-code-server`、`host-shell-vscode-remote-ssh`、`host-shell-coding-agents` | 接続先中継プロキシ・インターネット向けプロキシの用語への置き換え |

## 用語の置き換え

旧称 (上流プロキシ、中継プロキシ、upstream proxy、relay proxy、local relay proxy) と、プロキシの意味で使っている派生語 (上流接続、upstream connection など) を、次の範囲で完全に置き換える。旧称への言及や注記は `docs/prompts/` 以外に残さない。プロキシ以外の意味の「上流」(上流の CI、upstream のプロジェクト) は対象外とする。

| 対象 | 置き換え |
| --- | --- |
| 文書 (`docs/ja/`、`docs/en/`、`README.md`、`docs/README.md`) とすべての `SKILL.md`、埋め込み help | 用語の表のとおり |
| スクリプト (`host-shell-vscode-remote-ssh/scripts/linux/start.sh` のコメントと出力) | `relay proxy` → `target relay proxy` |
| コード (`internal/clientproxy/`、`internal/forward/spec.go` のコメント) | 用語の表のとおり |
| ログ・`proxy_status`・HTTP エラー本文の文字列 | 下の表 |
| 識別子 | 下の表 (テスト内の変数も同様に置き換える) |

| 現在 | 置き換え後 | 意味 |
| --- | --- | --- |
| `reasonUpstream` / `upstream connection failed` | `reasonBridgeConnect` / `connection from bridge failed` | bridge から宛先、またはインターネット向けプロキシへの接続の失敗 |
| `HTTP upstream failed` | `HTTP request from bridge failed` | bridge から送った HTTP 要求の失敗 |
| `unsupported upstream proxy` | `unsupported internet proxy` | インターネット向けプロキシの URL の方式が未対応 |
| `upstream CONNECT rejected` | `internet proxy rejected CONNECT` | インターネット向けプロキシが CONNECT を拒否 |
| `invalid upstream proxy configuration` | `invalid internet proxy configuration` | インターネット向けプロキシの設定が不正 |
| `proxy upstream failed` (接続先中継プロキシの 502 の本文) | `connection from bridge failed` | 同上 |
| `TestUpstreamHTTPAndCONNECT`、`TestHTTPSUpstream` | `TestInternetProxyHTTPAndCONNECT`、`TestHTTPSInternetProxy` | テスト名 |

> [!NOTE]
> `proxy_status` の `reason` と操作ログ (`proxy_connect`) の文字列が変わる。両端の版の一致は bridge の起動時に確認されるため、版の混在による解釈の食い違いは起きない。

置き換えと文章の推敲は、`agy --dangerously-skip-permissions` に依頼して進める。依頼の内容 (対象ファイル、用語の表、置き換えない語、この節の文字列の表) を渡し、結果の差分を確認したうえで取り込む。

## 文書

### 新規文書 `docs/ja/ssh-proxy.md` (英訳 `docs/en/ssh-proxy.md`)

題名は「SSH 経路プロキシ」(英訳は「SSH route proxy」)。構成は次のとおり。

1. 用途 (Tera Term のプロキシ設定で接続している環境で host-shell を使う)
2. 3 つのプロキシの用語と設定場所 (用語の正本)、および「プロキシ」がどれを指すかの確かめ方
3. `host-shell connect` の配置 (クライアントの bridge と同じバイナリ) と引数・環境変数
4. Host ブロックの書き方 (接続先ごと、多段、`ProxyJump`、無通信切断への対策)
5. 資格情報 (パスワード ファイルの作り方と権限の設定を OS ごとに、環境変数の注意、MCP 登録との関係)
6. MCP の登録とスキルによる手順
7. Tera Term の「Proxy Setup」ダイアログとの対応表 (Type、Hostname、Port number、Username、Password、Other setup の Connection Timeout → `-H`、`--password-file`、`-w` など。対応しない項目も明記する)
8. トラブルシューティング (407、403、接続不可、時間超過、無通信切断)
9. 検証済みの範囲と制限

### 既存文書

| 文書 | 変更 |
| --- | --- |
| `README.md` (日英の節) | 構成の表と図に SSH 経路プロキシを加える |
| `docs/README.md` | 文書一覧に `ssh-proxy.md` を加える |
| `docs/ja/user-guide.md` | 接続経路の説明から `ssh-proxy.md` へ案内する。用語の置き換え |
| `docs/ja/reference.md` | `host-shell connect` の引数と環境変数を加える |
| `docs/ja/client-proxy.md` | 用語の置き換え。インターネット向けプロキシの検証済みの範囲 (Squid での確認) を加える |
| `docs/ja/development.md` | パッケージ一覧に新しいパッケージを加える |
| `code-server.md`、`coding-agents.md`、`remote-dns.md`、`vscode-remote-ssh.md` | 用語の置き換え |

日本語版を先に編集し、同じコミットで英訳に反映する。

## 実装

| 対象 | 内容 |
| --- | --- |
| `cmd/host-shell/main.go` | `os.Args[1] == "connect"` で `routeproxy.Main` を呼ぶ。使い方の表示に `host-shell connect` を加える |
| `internal/routeproxy/` (新規) | 引数の解析、資格情報の取得、CONNECT の手順、中継。パスワード ファイルの権限の確認は OS ごとのファイル (`_unix.go`、`_windows.go`) に分ける |
| `internal/clientproxy/`、`internal/forward/spec.go` | 用語の置き換え (コメント、文字列、識別子) |
| `.agents/skills/` | [スキル](#スキル) のとおり |
| 埋め込み help (`internal/tools/help/`) | 用語の置き換え |

コードのコメントは英語で書く。

## 受入条件

| 区分 | 条件 |
| --- | --- |
| 自動テスト (全 OS の CI) | 試験用の CONNECT プロキシで次を確認する: 最初からの Basic 認証、誤ったパスワードの 407 で再送せずに失敗すること、ユーザー名なしの 407、パスワードが得られない場合に接続しないこと、403、200 以外の状態、ヘッダーの直後に届くバイト列、全 256 値を含む大きなバイト列の往復、half-close、時間超過、パスワード ファイルの権限の確認 (Unix: 所有者と `mode & 022`。Windows: 所有者、ほかの利用者の書き込み系の権限、読み取りだけの許可を通すこと、継承されたエントリ)、IPv6 の宛先、対応しない引数、資格情報が標準エラーに出ないこと。置き換え後の既存テストがすべて成功する |
| Linux のコンテナー | Podman で、外への経路が Squid (Basic 認証) だけのネットワークを作り、Squid 経由でしか届かない sshd に対して次を確認する: `host-shell-connect` のスクリプト (`detect.sh`、`deploy.sh`、`probe.sh`)、bridge 経由の MCP の操作、入れ子の多段と `ProxyJump` の 1 段目、bridge の `--proxy` (インターネット向けプロキシも同じ Squid) との併用、Squid の無通信切断と `ServerAliveInterval` の効果、誤った資格情報と 403 の診断 |
| Windows の実機 | Windows 11 の実機で、Git for Windows の OpenSSH と Windows の OpenSSH の両方から `host-shell.exe connect` を使った接続と MCP の操作、パスワード ファイルの権限の確認 (プロファイル配下の既定の ACL と `Users` の読み取りを継承したファイルが通ること、`Users` に書き込みを許したファイルが拒否されること)を確認する。Tera Term は例として示したもので、Tera Term での接続は検証の対象外とする |
| 文書 | 新規文書と既存文書の日英が一致し、旧称が `docs/prompts/` 以外に残っていない (検索で確認する) |

検証の経緯は `docs/prompts/current/261113-ssh-proxy-validation.md` に記録し、文書には結果 (検証済 / 未検証、方法、環境) だけを書く。
