# VS Code Remote - SSH (B-2 構成) の検証記録 (2026-11-03)

> [!IMPORTANT]
> 作業時点の記録です。残課題は [VS Code Remote - SSH (B-2 構成) の残課題一覧](261103-vscode-remote-ssh-backlog.md) で管理しています。仕様の文書は、この記録の時点では未作成です。

## 目的と前提

Windows の VS Code (Remote - SSH) から、隔離された Linux の接続先を編集できるかを確認した。想定した接続先の制約は次のとおり。

- 接続先からインターネットへ直接通信できない。
- システムの sshd では、ポート転送と scp (sftp) が禁止されている。
- 管理者が許可しているのは、SSH による host-shell の中継だけである。

経路の候補として次の 3 つを比べ、評価検証の基準を B-2 とした。

| 案 | 構成 | この記録での扱い |
|---|---|---|
| B-1 | 接続先のシステムの sshd へ直接接続する | ポート転送が禁止された前提では使えないため、対象外 |
| B-2 | host-shell の `job_start` で利用者権限の sshd を `127.0.0.1:2222` で起動し、`forward_open` で手元から届ける | 検証の対象 |
| B-3 | `ProxyCommand` で利用者権限の `sshd -i` を起動する | 未検証 |

B-2 と B-3 は、システムの sshd での転送の禁止を、内側の SSH で回り込む構成である。管理者が認めている環境でだけ使う前提とした。

## 環境

- **接続先:**
  - Podman コンテナー `hs-vsc-ol10` (`--network none`)、利用者 `dev`
  - Oracle Linux 10.2 / x86_64 / glibc 2.39、OpenSSH 9.9p1
  - host-shell v20261102.1.0-9-gfa1539d (`--mcp --allow-forward-local`、プロキシを使う段階では `--client-proxy` を追加)
- **手元:**
  - WSL2 上の bridge。`--forward-allow-local '127.0.0.1:2222'` を指定した。
  - 接続先へは `podman exec -i` の標準入出力で接続した。SSH の標準入出力の代わりである。
- **クライアント:**
  - Windows の VS Code 1.144.0 (commit `2c33b50b16c472efe58357d1d2a8a5414bc23fbf`)
  - Remote - SSH 0.128.0、OpenSSH_for_Windows 9.5p2
  - `~/.ssh/config` に検証用の `Host hs-vscode-ol10` を置いた。内容は `127.0.0.1:42222`、専用の鍵、専用の known_hosts、`StrictHostKeyChecking yes`。
  - Windows の `127.0.0.1:42222` には、WSL の localhost 転送を経由して、手元の `forward_open` の待ち受けが見える。
- **内側の sshd:**
  - 設定とホスト鍵は `~/.local/share/hs-vscode-sshd/` に置き、`job_start` で `/usr/sbin/sshd -D -e -f <設定>` を起動した。
  - 主な設定:
    - `ListenAddress 127.0.0.1`
    - `AllowUsers dev`
    - `AllowTcpForwarding local`、`AllowStreamLocalForwarding local`
    - エージェント転送・X11 転送・トンネル: 無効
    - `PasswordAuthentication no`、`UsePAM no`
    - `Subsystem sftp`: 記述しない
- **プロキシの許可先 (`--proxy-allow-host`):**
  - VS Code 用: `update.code.visualstudio.com:443`、`vscode.download.prss.microsoft.com:443`、`marketplace.visualstudio.com:443`、`*.gallerycdn.vsassets.io:443`、`*.gallery.vsassets.io:443`、`main.vscode-cdn.net:443`
  - Copilot 用 (段階 4 で追加): `github.com:443`、`api.github.com:443`、`*.githubcopilot.com:443`、`copilot-proxy.githubusercontent.com:443`
  - DNS の中継 (remote-dns) は使っていない。

## 段階ごとの結果

| 段階 | 状態 | 要点 |
|---|---|---|
| 1. 接続とサーバーの配置 | 検証済 | 事前配置 (オフライン) とプロキシ経由の取得の両方で接続できた |
| 2. 拡張機能 | 検証済 | Marketplace (プロキシ経由)、クライアントからの中継、キットの VSIX (オフライン) で導入できた |
| 3. 表示言語 | 検証済 | クライアントの言語に従う。オフラインでは言語パックの登録が必要 |
| 4. Copilot Chat | 検証済 | 接続先の agent host で動作した。Web Fetch は使えない |
| 5. 終了と後片付け | 一部検証済 | セッションを終えても VS Code Server が残る。host-shell からの後片付けで停止できた |

### 1. 接続とサーバーの配置

**scp の禁止:**
- 内側の sshd から `Subsystem sftp` を除くと、sftp と現行の scp は `subsystem request failed` で失敗した。
- Remote - SSH の導入用スクリプトは、接続先の `wget` で CLI を取得できないとき、クライアント側で取得して scp で送ろうとする。このため、scp を禁止した環境では、次の (a) か (b) が必要になる。
- 接続先には `/usr/bin/scp` があるため、従来方式の `scp -O` は塞がれていない。

**(a) 事前配置 (完全オフライン):**
- 手元で、commit を指定した配布物を取得した。
  - `https://update.code.visualstudio.com/commit:<C>/cli-alpine-x64/stable`: CLI、13 MB
  - `https://update.code.visualstudio.com/commit:<C>/server-linux-x64/stable`: サーバー、223,645,032 バイト、展開後 641 MB
- 照合した SHA-256 は、`/api/versions/commit:<C>/<種別>/stable` が返す値である。
  - CLI: `3ab9a8187d55433aca392fc0fa7e4e60a0450d4ae386a40dca639ddd59ccb1ee`
  - サーバー: `65a713d95bb598310ad86db4daea28f9c3f9687b59ee9e57b3e7060a810bf916`
- `transfer_push` で転送した。サーバーの転送は約 15 秒だった。
- 接続先には次の 3 つが必要だった。
  - `~/.vscode-server/code-<C>`: CLI の実行ファイル
  - `~/.vscode-server/cli/servers/Stable-<C>/server/`: サーバーの tar を `--strip-components=1` で展開したもの
  - `~/.vscode-server/cli/servers/lru.json`: 内容は `["Stable-<C>"]`
- `lru.json` がないと、agent host (`code-<C> agent host`) が `No cached server version` から最新版の取得を試みて失敗した。supervisor はその後に再試行しなかった。
- `lru.json` を置いて再読み込みすると、agent host は更新の確認に約 5 秒失敗したあと、キャッシュの版で起動した。

**(b) プロキシ経由:**
- 接続先の `wget` が CLI を取得し、CLI がサーバーを取得した。クライアント側での取得は行われなかった (`didLocalDownload==0`)。
- agent host の supervisor は、最新の stable 版を自分で調べる。今回は 1.144.1 (commit `261c84cb1eddf11235f9e2c921425e9663fbe556`) をさらに取得した。追加の容量は 641 MB で、`lru.json` も書き換えられた。
- 環境変数 `VSCODE_CLI_INITIAL_AH_VERSION=<C>` を渡すと、agent host は `Using initial agent host version override` を記録し、クライアントと同じ版で起動した。追加の取得は起きなかった。

**プロキシの引き継ぎ:**
- sshd は、セッションを作るときに環境変数を初期化する。このため、host-shell が `job_start` に渡すプロキシの環境変数は、VS Code Server まで届かない。
- 内側の sshd の設定に `SetEnv` を書いて渡した。渡した変数は次のとおり。
  - `HTTPS_PROXY`、`https_proxy`、`HTTP_PROXY`、`http_proxy`
  - `NO_PROXY=127.0.0.1,localhost,::1`、`no_proxy` (同じ値)
  - `VSCODE_CLI_INITIAL_AH_VERSION`
- `NO_PROXY` がないと、agent host への手元の接続もプロキシへ送られる。
- プロキシの URL にはセッションごとのトークンが含まれる。このため、host-shell のセッションごとに設定を作り直した。設定ファイルの権限は 600 にした。
- 作り直す前には、動いている VS Code Server を止める必要があった。止めないと、古い URL のまま使われる。

**DNS の中継を使わない理由:**
- VS Code Server は子プロセスの起動時に `LD_PRELOAD` を除く。
- 配布物を書き換えると、ライセンス上の懸念がある。

### 2. 拡張機能

| 方法 | 結果 |
|---|---|
| Marketplace (プロキシ経由) | Hex Editor を導入できた。署名の検証は `Success`。`*.gallery.vsassets.io:443` が未許可の間は、VSIX の取得が失敗した (`not a zip file`) |
| クライアントからの中継 | GitHub Pull Requests を導入できた |
| キットの VSIX (オフライン) | `server/bin/code-server --install-extension <vsix>` で C/C++ Themes を導入でき、画面の SSH の導入済み一覧に表示された |

署名の検証は、CRL・OCSP (`crl3.digicert.com:80`、`ocsp.digicert.com:80`) にも接続しようとする。プロキシの履歴には、拒否と認証の失敗として記録された。どちらも署名の検証の結果には影響しなかった。

### 3. 表示言語

- サーバー側の表示言語は、クライアントの言語に従った。
- オンラインでは、サーバーが Marketplace から言語パックを自動で導入した。
- オフラインでは、CLI で導入した言語パックが登録されなかった。`languagepacks.json` が `{}` のままで、拡張機能ホストは `en` で起動した。
- `language-packs.mjs ~/.vscode-server/data ~/.vscode-server/extensions` をサーバー同梱の node で実行すると、`ja` が登録された。`data/clp/<hash>.ja` が作られ、日本語で表示された。

### 4. Copilot Chat

- VS Code 1.144 では Copilot Chat が組み込まれている。接続先の agent host の下で `copilot-runtime --headless` が動いた。
- 認証はクライアントのサインインを使った。接続先の `~/.copilot` に、トークンのファイルは作られなかった。
- チャット、ファイルの読み取り、ターミナルでのコマンドの実行は、いずれも接続先で動作した。
- プロキシ経由の通信先は次のとおり。
  - `api.github.com`
  - `api.individual.githubcopilot.com`、`api.githubcopilot.com`
  - `proxy.individual.githubcopilot.com`、`exp.individual.githubcopilot.com`、`telemetry.individual.githubcopilot.com`
- 次の宛先は拒否され、動作には影響しなかった。
  - `default.exp-tas.com`
  - `copilot-telemetry.githubusercontent.com`
  - Application Insights
  - クラウドのメタデータ (`169.254.169.254`、`metadata.google.internal`)
- Web Fetch は `WebFetchBlockedUrlError: failed to lookup address information` で失敗した。DNS の中継を使わない構成なので、想定どおりである。

ターミナルで起動する Copilot CLI (`copilot`) は確認していない。

### 5. 終了と後片付け

- host-shell は、ジョブを止めるとき、プロセス グループに SIGKILL を送る。このため、ジョブを包むラッパーで後片付けすることはできない。
- host-shell のセッションを終えると、次のものが止まった。
  - host-shell
  - 内側の sshd
  - SSH のセッション
  - 転送
- 約 3 分後、導入用スクリプトの keepalive (`sleep 180`) が終わり、CLI の `command-shell` も終了した。
- 次のプロセスは、init の子として残った。
  - VS Code Server 一式
  - agent host の supervisor とそのサーバー
  - `copilot-runtime`
  - ターミナルの bash
- 再接続を待つ時間は `remote.SSH.reconnectionGraceTime` で決まり、既定は 3 時間である。
- 新しい host-shell のセッションから後片付けのスクリプトを実行した。スクリプトは、利用者の `/\.vscode-server/` を含むプロセスへ TERM を送り、残ったものへ KILL を送る。続けて `/tmp/code-*` と `/tmp/vscode-*` を削除する。
  - 結果は、残ったプロセスが 0 件、削除したファイルなどが 42 件だった。
  - 実行後に動いていたのは、host-shell だけだった。
- 後片付けの後も、次のものが残った。
  - `~/.vscode-server`: 696 MB
  - `~/.copilot`: 332 KB。`session-state`、`logs`、`ide`、`installed-plugins` がある。
  - `~/.local/share/hs-vscode-sshd`
  - 配布物の置き場所 `~/.cache/vscode-dist`: 227 MB

確認していないことは次のとおり。

- `--purge` によるファイルの削除
- `remote.SSH.reconnectionGraceTime` を短くしたときの効果
- 待ち時間が過ぎたあと、サーバーが自分で終了するか

## 検証で使ったスクリプト

次のスクリプトは、スクラッチ (リポジトリ外) に置いて使った。スキルには組み込んでいない。

| 名前 | 役割 |
|---|---|
| `sshd-setup.sh [公開鍵]` | 内側の sshd のホスト鍵・`authorized_keys`・設定を作り、`sshd -t` で検査する。`HTTPS_PROXY` と `VSCODE_CLI_INITIAL_AH_VERSION` が環境にあれば `SetEnv` に書く |
| `vsc-place.sh COMMIT DIR CLI_SHA SERVER_SHA` | SHA-256 を照合し、CLI とサーバーを配置する。`lru.json` は書かない |
| `vsc-cleanup.sh [--purge]` | VS Code Server を止め、`/tmp` の実行時ファイルを削除する。`--purge` は配置物と内側の sshd のファイルも削除する |

## Copilot CLI (BL-001)

段階 1 から 5 と同じ環境で、VS Code のターミナルから Copilot CLI を起動して確認した。

**配置:**
- Copilot CLI 1.0.83 (`copilot-linux-x64.tar.gz`、SHA-256 `ffbe1c429664b8a05efed67ecdb467123e40fcaa3c6c14ef9a98ba74da4687b7`) を使った。
- `transfer_push` で転送し、接続先で照合した。転送は約 6 秒だった。
- 展開した `copilot` の SHA-256 は `a3262c4513ef1fc2ca21485261ca73196977ad76bd5e7990fb572f6134aaeedd` だった。
- 置き場所は、版ごとのフォルダーとリンクの方式から、`~/.local/bin/copilot` に直接置く方式に変えた。
  - tar の中身は実行ファイル 1 つだけで、同梱物がないためである。
  - 版を戻すときは、手元の配布物から置き直す。

**VS Code の `copilot`:**
- VS Code 1.144 のターミナルでは、PATH の末尾に `server/bin/copilot-shim` が加わる。ここにある `copilot` は、本物の Copilot CLI を PATH から探す。
- 本物がないときの動作は次のとおり。
  - 対話中は `Install GitHub Copilot CLI? [y/N]` と聞き、npm の `@github/copilot` を導入しようとする。
  - 入力を受け取れない状態では、`GitHub Copilot CLI was not found` を表示して終了コード 127 で終わった。
- OL10 の `~/.bashrc` は `~/.local/bin` を PATH の先頭に加える。このため、`~/.local/bin/copilot` を置くと本物が使われた。`type -a copilot` の結果は `/home/dev/.local/bin/copilot` だった。

| 段階 | 状態 | 結果 |
|---|---|---|
| 配置 | 検証済 | 上記のとおり |
| 起動 | 検証済 | host-shell から `--version` が成功した。ターミナルで `copilot` の 1 語で起動した |
| ターミナル | 一部検証済 | 描画と入力は動作した。Ctrl-C とリサイズは未検証 |
| 認証 | 検証済 | `/login` でデバイスコード方式を使った。`System vault not available` では平文での保存を選んだ。`~/.copilot/config.json` (権限 600) に保存され、新しいターミナルで起動したときはログイン不要だった |
| 推論・操作 | 検証済 | `hello.txt` の作成とコマンド `wc -c` は、承認後に実行された。結果の 23 バイトは、接続先での確認と一致した |
| Copilot Chat との併用 | 検証済 | Copilot CLI を起動したまま、Copilot Chat (`copilot-runtime`) が応答した。`~/.copilot` を共有しても、互いの動作に影響はなかった |
| 自動更新 | 確認済の制限 | 起動時に `Error auto updating: TypeError: fetch failed` が表示された。プロキシでは `release-assets.githubusercontent.com:443` が拒否された |
| Web Fetch | 未検証 | DNS の中継を使わないため、失敗すると予想している |

プロキシの履歴 (このセッション全体で、Copilot Chat の通信も含む) の内容は次のとおり。

- **許可された宛先:**
  - `github.com` (1 件)
  - `api.github.com`
  - `api.individual.githubcopilot.com`、`api.githubcopilot.com`
  - `proxy.individual.githubcopilot.com`、`exp.individual.githubcopilot.com`、`telemetry.individual.githubcopilot.com`
  - `main.vscode-cdn.net`
- **拒否された宛先:**
  - `release-assets.githubusercontent.com`
  - `mobile.events.data.microsoft.com`
  - `default.exp-tas.com`
  - `copilot-telemetry.githubusercontent.com`
  - `westus-0.in.applicationinsights.azure.com`
  - `169.254.169.254`、`metadata.google.internal`

**終了の確認:**
- Copilot CLI (`~/work` で入力待ち) と Copilot Chat を動かしたまま、host-shell のセッションを終えた。
- 10 秒後の状態:
  - host-shell、内側の sshd、手元の転送 (`127.0.0.1:42222`) は止まっていた。
  - Copilot CLI と VS Code Server は動いていた。
- 220 秒後の状態:
  - keepalive と CLI の `command-shell` は終了していた。
  - agent host は init の子になっていた。
  - VS Code Server 一式、ターミナルの bash 2 つ、Copilot CLI は残っていた。
  - Copilot CLI の環境の `HTTPS_PROXY` は、止まった中継を指したままだった。
- 新しい host-shell のセッションから後片付けのスクリプトを実行した。
  - 結果は、残ったプロセスが 0 件、削除した `/tmp` のファイルなどが 8 件だった。
  - Copilot CLI はスクリプトの停止対象ではない。コマンドの引数に `/.vscode-server/` を含まないためである。それでも、VS Code Server と一緒に終了し、動いているのは host-shell だけになった。
