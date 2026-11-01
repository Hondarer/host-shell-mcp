<!--
生成元: agree-before-implement スキル
文書種別: 最終仕様 (design)
最終更新エージェント: claude code / セッション ID: session_01KJc53UTLFzqXyXoZpjPqCE
上流文書: 261101-code-server-followup-discussion.md (検討用 Markdown)。削除されている場合があります。
下流文書: 261101-code-server-followup-backlog.md (残課題一覧)。未作成の場合や、削除されている場合があります。
再開手順: このファイルを開いたエージェントは、作業の前に /home/user/c-modernization-kit/app/general/.agents/skills/agree-before-implement/SKILL.md を読み込んでください。上流文書が存在する場合は、上流文書の再開手順に従って進行位置を確認し、判断の経緯を上流文書の Q&A で確認します。存在しない場合は、この文書を単体の最終仕様として扱います。最終更新エージェントのセッション情報が参照可能であれば再開時のコンテキストとして利用し、別エージェントからの引き継ぎや担当変更によりセッションが見つからない場合も異常とせず、文書の記載から継続してください。
-->

# code-server の注入の残課題への対応 最終仕様

検討 ID: 261101-code-server-followup

版: 1

状態: 承認済み

## 1. 目的と範囲

元案件 261101-code-server の残課題一覧 (`docs/prompts/past/261101-code-server-backlog.md`) のうち、次の 3 件に対応します。

| 課題 ID | 題名 | 対応 |
| --- | --- | --- |
| BL-002 | HTTP プロキシ経由で、画面の拡張機能ビューから導入できるかの確認 | 確認し、結果と必要な許可先を文書に記載 (3 章) |
| BL-003 | 接続先のポート 8000 が使用中の場合の、空きポートの選定 | start が 8000〜8079 から空きポートを選ぶ (2 章) |
| BL-004 | ssh の代わりに podman exec で接続する方法と、その上での code-server の利用の整備 | 経路にコンテナーを加え、code-server の前提を整え、文書を新設 (4 章、5 章) |

Table: 対応する残課題

BL-001 (Linux arm64 の実機確認) は、arm64 の対応が実装済みで実機では未確認という現在の状態 (文書の「対象」に記載済み) を容認し、完了とします。

## 2. 空きポートの選定 (BL-003)

### 2.1. 候補の範囲

code-server が接続先で待ち受けるポートは、`127.0.0.1` の 8000〜8079 のうち、8000 から順に調べて最初に空いているものです。

`version.env` の `CODE_SERVER_PORT=8000` を、次の 2 つに置き換えます。

```sh
CODE_SERVER_PORT_FIRST=8000
CODE_SERVER_PORT_LAST=8079
```

環境変数 `CODE_SERVER_PORT` を指定した場合は、範囲を使わず、そのポートだけを使います。使用中ならエラーで止まります (従来と同じ)。

install が作る `config.yaml` の `bind-addr` は `127.0.0.1:<CODE_SERVER_PORT_FIRST>` とします。start はコマンド行の `--bind-addr` で実際のポートを指定するため、`config.yaml` の値は使われません。

> [!NOTE]
> 範囲は、許可規則の 1 つの glob `127.0.0.1:80[0-7]?` で表せます。接続先の `--allow-host` と bridge の `--forward-allow-local` は、ポートを glob で照合します。8080 は開発用の Web サーバーなどがよく使うため、範囲に含めません。ポート 0 (OS の割り当て) は、許可規則を全ポートに広げる必要があるため使いません。

### 2.2. start (Linux: `scripts/linux/start.sh`)

1. `CODE_SERVER_PORT` があれば、そのポートだけを候補にします。なければ `CODE_SERVER_PORT_FIRST` から `CODE_SERVER_PORT_LAST` までを順に候補にします。
2. 各候補に bash の `/dev/tcp/127.0.0.1/<port>` で接続を試み、接続できなければ空きとみなします。
3. 空きがなければ、`no free port in 8000-8079 on 127.0.0.1` (指定時は `port <port> on 127.0.0.1 is already in use`) で止まります。
4. `starting code-server <版> on 127.0.0.1:<port>` を出力し、`--bind-addr 127.0.0.1:<port>` で code-server を起動します。code-server は `HTTP server listening on http://127.0.0.1:<port>/` を出力します。

調べてから code-server が待ち受けるまでの間に他のプロセスが同じポートを取った場合、code-server は起動に失敗し、job が終わります。エージェントは start をもう一度実行します。

### 2.3. start (Windows: `scripts/windows/start.ps1`)

`Get-NetTCPConnection -State Listen` で待ち受け中のポートの一覧を 1 回取得し、2.2 と同じ順序と規則でポートを選びます。`common.ps1` の `Get-HscsConfig` は、`PortFirst`、`PortLast` と、`CODE_SERVER_PORT` の指定 (`Port`、未指定なら空) を返します。

### 2.4. エージェントの手順 (SKILL.md)

- 前提の確認と登録の例を、`--forward-allow-local '127.0.0.1:80[0-7]?'` (接続先で `--allow-host` を使う場合は `127.0.0.1:80[0-7]?` も許可) に改めます。glob はシェルに展開されないよう引用符で囲みます。
- 起動後、`job_read` の出力の `HTTP server listening on http://127.0.0.1:<port>/` から `<port>` を読み取り、`forward_open` の `port` に渡します。
- `forward_open` が許可規則で拒否された場合は、`job_stop` で code-server を止め、範囲の規則への変更と再接続を利用者に案内します (8000 だけを許可した登録で、8000 が使用中だった場合に起きます)。
- job が `HTTP server listening on` の前に終わり、出力がポートの使用中を示す場合は、start をもう一度実行します。

## 3. 拡張機能ビューからの導入の確認 (BL-002)

クライアント経由 HTTP プロキシを有効にした接続先 (接続先の `--client-proxy`、bridge の `--proxy` と `--proxy-allow-host open-vsx.org:443`、`--proxy-allow-host openvsx.eclipsecontent.org:443`) で、ヘッドレスの Chrome (puppeteer-core) から code-server の拡張機能ビューを開き、拡張機能を検索して導入します。

- bridge の `proxy_status` で、許可された接続と拒否された接続先を調べます。
- 拒否された接続先が検索や導入に必要であれば、許可を加えて再び確かめます。
- 結果 (成否と、必要な `--proxy-allow-host` の接続先) を `docs/*/code-server.md` の「個別の拡張機能の導入」に記載し、「画面からの導入は確認していません」の記載を改めます。

## 4. コンテナーへの接続 (BL-004)

### 4.1. 経路 (`ROUTE`) の書式

スキル host-shell-connect の `ROUTE` は、ssh の踏み台と接続先をカンマで並べたものです。その最後に、コンテナーの要素を 1 つだけ置けるようにします。

| 要素 | 意味 |
| --- | --- |
| `<ホスト>` | ssh で接続します (従来どおり) |
| `podman:<コンテナー>` | `podman exec -i` でコンテナーの中に入ります |
| `docker:<コンテナー>` | `docker exec -i` でコンテナーの中に入ります |
| `kubectl:<pod>` | `kubectl exec -i` で pod の中に入ります |

Table: ROUTE の要素

- コンテナーの要素が最後以外にある場合と、名前が空の場合は、使い方のエラーで止まります。
- 例: `podman:web` (手元のコンテナー)、`hop1,podman:web` (hop1 の上のコンテナー)、`kubectl:web-0`。

追加の指定は、実行環境ごとの環境変数で与えます。値は `HOST_SHELL_SSH_OPTS` と同じく空白で区切られます。

| 環境変数 | 例 | 用途 |
| --- | --- | --- |
| `HOST_SHELL_PODMAN_OPTS` | `--user dev` | 実行する利用者など |
| `HOST_SHELL_DOCKER_OPTS` | `--user dev` | 実行する利用者など |
| `HOST_SHELL_KUBECTL_OPTS` | `-n app -c web --context prod` | 名前空間、pod の中のコンテナー、クラスター |

Table: 追加の指定

### 4.2. `remote.sh` の組み立て

コンテナーの中では、コマンドを `sh -c` で実行します。

```text
podman:web           -> podman exec -i $HOST_SHELL_PODMAN_OPTS web sh -c 'COMMAND'
docker:web           -> docker exec -i $HOST_SHELL_DOCKER_OPTS web sh -c 'COMMAND'
kubectl:web-0        -> kubectl exec -i $HOST_SHELL_KUBECTL_OPTS web-0 -- sh -c 'COMMAND'
hop1,podman:web      -> ssh -T $HOST_SHELL_SSH_OPTS hop1 'podman exec -i ... web sh -c '\''COMMAND'\'''
```

- コンテナーの要素だけの `ROUTE` は、手元でそのコマンドを実行します。
- ssh の踏み台がある場合、コンテナーのコマンドは最後の ssh の段で実行されます。
- ssh だけの `ROUTE` で組み立てるコマンドは、従来と同じです。

`detect.sh`、`deploy.sh`、`probe.sh` はすべて `remote.sh` を通るため、処理は変えずに、使い方とコメントの「SSH」の記載を経路一般に改めます。`deploy.sh` の既定の配置先は、コンテナーの中でも `/tmp/host-shell-<uid>/host-shell`、次に `.local/bin/host-shell` です。

### 4.3. 登録コマンド

bridge の `--` の後に、`ROUTE` に対応するコマンドを書きます。コンテナーの中の host-shell は `sh -c` を介さず直接起動します。

```sh
host-shell bridge -- podman exec -i web /tmp/host-shell-0/host-shell --mcp
host-shell bridge -- docker exec -i --user dev web /tmp/host-shell-1000/host-shell --mcp
host-shell bridge -- kubectl exec -i -n app -c web web-0 -- /tmp/host-shell-1000/host-shell --mcp
host-shell bridge -- ssh -T -o BatchMode=yes hop1 podman exec -i web /tmp/host-shell-0/host-shell --mcp
```

`exec` には `-i` が必須で、`-t` を付けてはいけません (端末の制御で MCP のやり取りが壊れます)。スキル host-shell-connect は、`ROUTE` と追加の指定から登録コマンドを組み立てて示します。

### 4.4. 対象とする環境

| 項目 | 内容 |
| --- | --- |
| コンテナーの OS | Linux だけ。Windows コンテナーは対象外 |
| 実行環境 | podman (確認済み)、docker (podman の互換コマンドで確認。Docker Engine では未確認)、kubectl (未確認) |
| コンテナーの中のコマンド | `sh` と、`detect.sh` と `deploy.sh` が使う `uname`、`id`、`dirname`、`ls`、`sha256sum`、`mkdir`、`chmod`、`mv`、`cat`、`rm` |

Table: 対象とする環境

シェルのないコンテナー (distroless のイメージなど) はスクリプトの対象外です。文書に、`podman cp` / `docker cp` で手で配置し、登録コマンドで接続する方法を記載します。`kubectl cp` はコンテナーの中に `tar` を必要とするため、kubectl では配置できないと記載します。シェルがないため、`exec_run` でのシェルのコマンドと code-server は使えません。

## 5. コンテナーでの code-server (BL-004)

### 5.1. glibc の検査 (`scripts/linux/install.sh`)

install は、必要なコマンドの確認の直後、配布アーカイブの照合と展開の前に、配布物の node が使う動的リンカーがあるかを調べます。

| アーキテクチャー | 動的リンカー |
| --- | --- |
| x86_64 | `/lib64/ld-linux-x86-64.so.2` |
| aarch64 | `/lib/ld-linux-aarch64.so.1` |

Table: 調べる動的リンカー

なければ、`code-server needs glibc: <パス> not found (musl-based systems such as Alpine are not supported)` で止まります。

### 5.2. 配置先と HOME

配置先は従来どおり HOME から決めます。install は最初に、`HOME` が設定され、書き込めるディレクトリーであることを調べ、満たさなければ止まります。SKILL.md の前提の確認にも、この条件を記載します。

### 5.3. コンテナーでの注意 (文書)

- コンテナーを作り直すと、ボリュームに置いていない host-shell、code-server、設定、拡張機能は消えます。残す場合は HOME をボリュームにします。
- code-server と転送は、MCP セッションの終了で止まります (host-shell のジョブと同じ)。
- pod の中のコンテナーはネットワークを共有するため、`127.0.0.1` への転送で同じ pod の他のコンテナーのサービスにも届きます。

## 6. 正本文書の更新

| 文書 | 更新内容 |
| --- | --- |
| `docs/ja/container.md`、`docs/en/container.md` (新規) | コンテナーへの接続: 対象と確認状況、`ROUTE` の要素と追加の指定、配置、登録コマンド (`-i` と `-t`)、実行する利用者、コンテナーの寿命と配置先の制約 (読み取り専用のルート ファイル システム、noexec の `/tmp`)、pod のネットワーク、シェルのないコンテナー、code-server の利用 (glibc、HOME) |
| `docs/*/code-server.md` | 対象 (glibc が必要)、登録の例と許可規則 (`127.0.0.1:80[0-7]?`)、待ち受けポートの選び方、拡張機能ビューの確認結果、コンテナーでの利用への案内 |
| `docs/*/user-guide.md` | 「接続先への配置」と「エージェントへの登録」から container.md への案内 |
| `docs/*/port-forward.md` | 許可規則のポートの glob の例 (`127.0.0.1:80[0-7]?`) |
| `docs/README.md` | container.md の行 |
| `.agents/skills/host-shell-connect/SKILL.md` | `ROUTE` のコンテナーの要素、追加の指定の環境変数、コンテナーの登録コマンド、シェルのないコンテナー |
| `.agents/skills/host-shell-code-server/SKILL.md` | 前提の確認 (glibc、HOME、範囲の許可規則)、ポートの読み取り、拒否時と競合時の手順 |

Table: 正本文書の更新

日本語の文書を正本とし、英語の文書はその翻訳です。スクリプトのコメントは英語です。

## 7. 確認方法

確認はすべて作業環境 (WSL) で行います。ssh は使わず、bridge から接続先の host-shell を直接起動するか、`podman exec -i` で起動します。HOME、LOCALAPPDATA、USERPROFILE は一時フォルダーに向けます。確認用のスクリプトはスクラッチ領域に置き、リポジトリには含めません。

| 確認 | 方法 |
| --- | --- |
| BL-002 | ヘッドレスの Chrome で拡張機能ビューを操作し、bridge の `proxy_status` を記録 |
| BL-003 (Linux、Windows) | 8000 (と 8000〜8079 のすべて) を一時的な待ち受けで占有して start を実行。Windows は WSL の相互運用で起動した host-shell.exe |
| BL-003 (許可規則) | `127.0.0.1:8000` だけの bridge と `'127.0.0.1:80[0-7]?'` の bridge で forward_open |
| BL-004 (経路) | podman の要素と docker の要素 (podman の互換コマンド) で `detect.sh`、`deploy.sh`、`probe.sh` を実行。kubectl の要素と ssh の踏み台は、引数を記録してから podman で実行する偽の `kubectl` と `ssh` を PATH の先頭に置いて確認。ssh だけの `ROUTE` は、変更前後の `remote.sh` で組み立てたコマンドを比較 |
| BL-004 (code-server) | Oracle Linux 9 のコンテナーで、配置から code-server の表示 (ログイン、エディター、テーマ) まで |
| BL-004 (pod) | podman の pod に HTTP サーバーのコンテナーを加え、host-shell のコンテナーから `127.0.0.1` へ転送 |
| BL-004 (glibc、最小イメージ) | alpine と debian:stable-slim を取得して install を実行 |

Table: 確認方法

確認後、コンテナー、pod、取得したイメージ (alpine、debian:stable-slim)、一時フォルダーを削除します。

## 8. 元案件の残課題一覧の更新

`docs/prompts/past/261101-code-server-backlog.md` の各項目を削除せずに残し、状態と経緯を更新します。

| 課題 ID | 状態 | 経緯と対処に記載する内容 |
| --- | --- | --- |
| BL-001 | 容認 | 実装済み・実機未確認の状態を容認して完了とした (261101-code-server-followup) |
| BL-002 | 解消 | 確認の結果と、文書に記載した許可先 |
| BL-003 | 解消 | 8000〜8079 からの選定、許可規則の例の変更 |
| BL-004 | 解消 | `ROUTE` のコンテナーの要素、glibc と HOME の検査、container.md |

Table: 元案件の残課題一覧の更新

作業中に判明して対策しなかった事項は、本件の残課題一覧 `261101-code-server-followup-backlog.md` に記録します。

## 9. 知見の反映計画

| 知見 | 反映先 |
| --- | --- |
| 許可規則のポートは glob で範囲を表せる | `docs/*/port-forward.md`、`docs/*/code-server.md` |
| bridge はどの標準入出力のコマンドでも接続先にでき、コンテナーでは `exec -i` が必須で `-t` は不可 | `docs/*/container.md`、host-shell-connect の SKILL.md |
| code-server の配布物は glibc を必要とする | `docs/*/code-server.md`、`install.sh` のコメント |
| pod の中はネットワークを共有し、`127.0.0.1` への転送が同じ pod の他のコンテナーに届く | `docs/*/container.md` |
| 拡張機能ビューに必要な許可先 | `docs/*/code-server.md` |

Table: 知見の反映計画

## 10. 作業の進め方

- main (コミット a78a8d8 以降) から作業ブランチ `claude/code-server-followup` を作り、最初のコミットに、元案件の残課題一覧への BL-004 の追記と、検討用 Markdown、この最終仕様を含めます。
- main 向けのドラフト PR を作成します。
- 受入条件をすべて満たし、全 OS の CI が成功した時点で、第 3 ゲートを待たずにエージェントがマージ コミットでマージします。マージ前に検討記録を `docs/prompts/past/` へ移します。
- 第 3 ゲートの承認の記録は、承認後に main へ直接コミット・プッシュします。

## 11. 受入条件

1. BL-002: クライアント経由 HTTP プロキシを有効にした接続先で、ヘッドレスの Chrome から拡張機能ビューで検索と導入を行い、その成否と必要な `--proxy-allow-host` の接続先が日英の文書に記載されていること。
2. BL-003 (Linux): 8000 を使用中にすると、start が 8001 で起動してそのポートを出力し、エージェントがそのポートへ転送してログインできること。8000〜8079 がすべて使用中ならエラーで止まること。使用中のポートを `CODE_SERVER_PORT` で指定するとエラーで止まること。
3. BL-003 (Windows): 2 と同じことを Windows の接続先 (WSL の相互運用) で確認できること。
4. BL-003 (許可規則): `--forward-allow-local 127.0.0.1:8000` の登録で 8001 が選ばれた場合に forward_open が拒否され、スキルの手順が code-server を止めて規則の変更を案内する内容になっていること。`'127.0.0.1:80[0-7]?'` の登録では転送できること。
5. BL-004 (経路): `remote.sh` が `podman:`、`docker:`、`kubectl:` の要素と環境変数の追加の指定を受け付け、`detect.sh`、`deploy.sh`、`probe.sh` が podman の要素と docker の要素 (podman の互換コマンド) で成功すること。kubectl の要素と、ssh の踏み台との組み合わせは、引数を記録する偽の `kubectl` と `ssh` のコマンドで、組み立てたコマンドが正しいことを確認すること。ssh だけの `ROUTE` で組み立てるコマンドが従来と同じであること。
6. BL-004 (code-server): Oracle Linux 9 の単独のコンテナーに、スキル host-shell-connect で host-shell を配置し、`podman exec -i` の登録で接続して、スキル host-shell-code-server の手順で code-server を導入、起動、転送し、ログイン、エディターの表示、テーマの反映を確認できること。
7. BL-004 (pod): podman の pod で、host-shell を置いたコンテナーから、同じ pod の別のコンテナーのサービスへ `127.0.0.1` への転送で接続できること。
8. BL-004 (glibc と最小イメージ): alpine のコンテナーで、install が展開の前に glibc のエラーで止まること。debian:stable-slim のコンテナーで、install が成功するか、不足するコマンドの名前で止まり、その結果が文書に記載されていること。
9. 文書とスキル: コンテナーへの接続の説明 (`docs/*/container.md`) と、code-server の文書、port-forward の文書、両スキルの SKILL.md が更新され、日英の内容が一致していること。スクリプトのコメントが英語であること。元案件の残課題一覧で、BL-001 が「容認」、BL-002〜BL-004 が対応の結果に更新され、本件の検討 ID が記載されていること。
10. make、6 ターゲットのクロスビルド、全 OS の CI が成功すること。
11. 作業環境の保全: 確認用のコンテナー、pod、取得したイメージ、一時フォルダーを削除し、利用者のホームと Windows のプロファイルに変更がないこと。
