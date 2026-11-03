# VS Code Remote - SSH (B-2 構成) の残課題一覧

## 案件の識別

案件名: VS Code Remote - SSH (B-2 構成) の評価検証 (261103-vscode-remote-ssh)  
作成日: 2026-11-03  
整理状態: 随時追記中

[検証記録](261103-vscode-remote-ssh-validation.md) の段階 1 から 5 の結果をもとに、残った課題をまとめています。作業中は、各項目の状態と「経緯と対処」を更新します。項目は削除しません。

## 残課題一覧

| 課題 ID | 題名 | 優先度 | 状態 |
| --- | --- | --- | --- |
| BL-001 | ターミナルで起動する Copilot CLI を B-2 で検証していない | 高 | 検証済 (Ctrl-C・リサイズ・Web Fetch は未検証) |
| BL-002 | agent host の版の固定が手作業で、内部の環境変数に依存している | 高 | 検証済 (ラッパー方式。内部の環境変数への依存は容認) |
| BL-003 | 新しい版への更新手順が整っておらず、検証していない | 高 | 検証済 (オフラインとプロキシ経由での切り替え、古い版の削除。接続中の更新要求は未検証) |
| BL-004 | 準備と後片付けの手順が、スクラッチのスクリプトのままである | 中 | 検証済 (スキル `host-shell-vscode-remote-ssh`。arm64 と macOS の手元は未検証) |
| BL-005 | セッションを終えても VS Code Server が残る | 中 | 一部検証済 |
| BL-006 | 従来方式の scp (`scp -O`) が塞がれていない | 低 | 未着手 |
| BL-007 | B-2 の仕様の文書がない | 中 | 対処済 (`docs/ja/vscode-remote-ssh.md` と英訳) |
| BL-008 | 検証用にクライアントへ加えた設定が残っている | 低 | 未着手 |

## BL-001 ターミナルで起動する Copilot CLI を B-2 で検証していない

- **現状:**
  - 段階 4 で確認したのは、VS Code に組み込まれた Copilot Chat (agent host の `copilot-runtime`) だけである。
  - Copilot CLI は、code-server の構成 (1.0.83) でだけ検証済である。
- **確認すること:**
  - 次の各段階が B-2 で動作するか: 配置、起動、ターミナル、認証 (デバイスコード方式)、推論 (プロキシ経由)、操作
  - Web Fetch が失敗すること (DNS の中継を使わないための制限)
  - セッションを終えたあとに Copilot CLI が残るか。B-2 では VS Code Server が残るため、残ると予想している。
  - `copilot-runtime` と Copilot CLI が `~/.copilot` を共有しても、互いに影響しないか。Copilot CLI の認証情報は `config.json` に保存される。
- **経緯と対処 (2026-11-03):**
  - 結果は、[検証記録の「Copilot CLI (BL-001)」](261103-vscode-remote-ssh-validation.md#copilot-cli-bl-001) に記録した。
  - 次の段階は検証済である: 配置、起動、認証、推論・操作、Copilot Chat との併用、終了
  - 置き場所は、`~/.local/bin/copilot` に直接置く方式とした。VS Code の `copilot` (npm から導入しようとする) より先に見つかるようにするためである。
  - セッションを終えても、Copilot CLI は VS Code Server と一緒に残った。後片付けのスクリプトで VS Code Server を止めると、Copilot CLI も終了した。
  - 未検証: Ctrl-C、リサイズ、Web Fetch (失敗すると予想)

## BL-002 agent host の版の固定が手作業で、内部の環境変数に依存している

- **現状:**
  - `VSCODE_CLI_INITIAL_AH_VERSION=<commit>` を `sshd-setup.sh` に手で渡し、`SetEnv` に書いている。
  - この変数は公開されていない。名前のとおり、最初に起動する版を指定するものである。
  - CLI には、起動後に版を上げる経路もある (`Upgrade request`、`Failed to restart agent host after upgrade` という文字列がある)。接続中に更新が要求されたときに、固定が保たれるかは未検証である。
- **固定が必要な範囲:**
  - プロキシ経由の構成だけで必要になる。サーバーの取得に `update.code.visualstudio.com:443` を許可するため、最新版の確認も通ってしまう。許可はホスト名単位なので、この確認だけを止めることはできない。
  - オフラインの構成では、確認が失敗して `lru.json` の先頭の版が使われる。
- **あるべき姿:**
  - クライアントの commit を 1 つ渡すだけで、配置と固定がそろうこと。
  - 固定が効かなくなったときに気付けること。たとえば、`Stable-*` が増えたことを後片付けのときに報告する。
- **経緯と対処 (2026-11-03):**
  - **版を上げる経路の調査:** VS Code 1.144.0 の配布物のコードを調べた。agent host が版を上げるのは、クライアントが接続したときにプロトコルの版が合わない (`incompatible`) 場合だけである。このときサーバーが `vscodeUpgradeMethod` を提示し、クライアントがそれを呼ぶと、agent host の管理用ソケットに `POST /upgrade` が送られる。固定した版がクライアントの commit と同じなら、この経路は使われない。実際に版の更新が要求されたときの動作は未検証である。
  - **スクリプトの作り直し:**
    - `vscode-fetch.sh COMMIT [x64|arm64] [OUT_DIR]` (手元): 公式 API (`/api/versions/commit:<C>/<種別>/stable`) から取得先と SHA-256 を得て、取得と照合を行う。結果として `SHA256SUMS` と `COMMIT` を書く。
    - `vscode-place.sh DIST_DIR` (接続先): 照合して配置し、`lru.json` の先頭に追加する。
    - `vscode-place.sh --prune COMMIT` (接続先): 古い版を削除する。
    - `sshd-setup.sh`: `--commit` を省くと、`lru.json` の先頭の版に固定する。
    - `vscode-status.sh`: 固定と異なる `Stable-*` や、動いているサーバーがあれば警告する。
  - **確認の結果:**
    - `~/.vscode-server/cli` と `code-*` を削除してから、1.144.0 を新しいスクリプトで配置し直した。
    - `--commit` を付けずに sshd の設定を作ると、固定の版は `lru.json` の先頭から決まった。
    - プロキシ経由で再接続すると、agent host は `Using initial agent host version override` を記録して 1.144.0 で起動した。
    - プロキシの履歴に `update.code.visualstudio.com` への接続はなかった。固定すると、最新版の確認そのものが行われない。
  - **その他:** 後片付けのスクリプトのプロセスの判定は、呼び出し元の `sh -c` の引数に含まれるパスにも一致していた。このため、実行ファイルが `~/.vscode-server/` の下にあるものと、VS Code のターミナルの bash だけに一致するように絞った。
  - **容認した事項:** 内部の環境変数 `VSCODE_CLI_INITIAL_AH_VERSION` に依存する点は容認する。将来の版で効かなくなった場合は、`vscode-status.sh` の警告で気付ける。
  - **ラッパー方式への変更:**
    - **変更の理由:** `lru.json` の先頭から固定の版を決める方式では、新しい版を先に配置すると、更新前のクライアントに対して agent host だけが新しい版になる。固定の版は、接続してきたクライアントの commit に合わせる必要がある。
    - **仕組み:**
      - Remote - SSH の導入用スクリプトは、`~/.vscode-server/code-<C>` があればそれを `command-shell` で起動し、`kill -0` で PID の生存だけを確認する。
      - そこで `vscode-place.sh` は、本体を `~/.vscode-server/hs-cli/code-<C>` に置き、`code-<C>` を次のラッパーにした。
        ```sh
        VSCODE_CLI_INITIAL_AH_VERSION=<C> exec <本体> "$@"
        ```
      - `sshd-setup.sh` は、既定では版を固定しない。`--commit` は、接続先が自分で CLI を取得する構成のための予備として残した。
      - `vscode-status.sh` は、次の場合に警告する。
        - ラッパーのない `Stable-*` がある。
        - 動いているサーバーに、ラッパーがない。
        - supervisor のプロセス グループのサーバーが、supervisor 自身の commit と異なる。
    - **確認の結果:**
      - 1.144.0、1.144.1 の順に配置し、`lru.json` の先頭を 1.144.1 にした。sshd の設定には版の固定もない。この状態で、プロキシ経由で 1.144.0 のクライアントが接続した。
      - agent host は `Using initial agent host version override: 2c33b50b…` を記録し、1.144.0 のサーバーを動かした。
      - CLI と supervisor は、ラッパーから exec された `hs-cli/code-<C>` として動いた。
      - Copilot Chat は応答した。プロキシの履歴に、`update.code.visualstudio.com` への接続はなかった。
      - agent host は、使った版が先頭になるように `lru.json` を書き換えた。

## BL-003 新しい版への更新手順が整っておらず、検証していない

- **現状:**
  - `vsc-place.sh` には、SHA-256 の値を手で渡している。
  - `vsc-place.sh` は `lru.json` を更新しない。
  - 古い版を削除する手順がない。
- **想定する手順:**
  1. クライアントの commit を確認する (Windows の `code --version` の 2 行目)。
  2. その commit の配布物を手元で取得し、公式の SHA-256 と照合する。
  3. 接続先へ転送して配置する。
  4. `lru.json` の先頭に新しい版を追加する。
  5. 版の固定を新しい commit に変え、内側の sshd の設定を作り直す。
  6. 古いサーバーを止めて、再接続する。
  7. 動作を確認してから、古い `Stable-<旧>` と `code-<旧>` を削除する。
- **運用上の注意:**
  - オフラインの構成では、クライアントが先に自動更新されると接続できなくなる。
  - 対策の候補は 2 つある。クライアントの自動更新を止める (`update.mode`)。または、接続先を先に更新する。
- **検証の案:** クライアントを 1.144.0 から 1.144.1 (commit `261c84cb1eddf11235f9e2c921425e9663fbe556`) へ更新し、オフラインで上記の手順を確認する。
- **未確認:** 導入済みの拡張機能と言語パックの登録を、新しい版でもそのまま使えるか。(確認の結果は「経緯と対処」にある)
- **経緯と対処 (2026-11-03):**
  - **配布物の取得:** `vscode-fetch.sh` で、1.144.1 の配布物を手元で取得して照合した。SHA-256 は、CLI が `a8f2213541c1b706ef482babbc168287d3f554e252fd14946fced1ed83988cff`、サーバーが `f97933958b8598ccde982342938b99e0e40cecf2c9373b510764e137f16fee02`。転送は約 15 秒だった。
  - **事前配置:** 1.144.0 のクライアントで接続している間に、1.144.1 を配置した。動いているサーバーには影響しなかった。ラッパー方式の確認 (BL-002) で、新しい版を配置したあとも、更新前のクライアントは 1.144.0 のサーバーと agent host でそのまま使えた。
  - **クライアントの更新の制約:** VS Code は、すべてのウィンドウを閉じたときにしか、取得済みの更新を適用しない。Claude Code を動かしている VS Code があるため、Claude Code をいったん終了し、更新してから再開する。
  - **再読み込みのダイアログ:** サーバーを止めたあとの再接続は失敗し、「再接続できません。ウィンドウを再読み込みしてください。」のダイアログが出た。「キャンセル」で閉じられる。
- **クライアントの切り替えの確認 (オフライン):**
  - **前提:**
    - クライアントを 1.144.1 (`261c84cb1eddf11235f9e2c921425e9663fbe556`) に更新した。
    - bridge はプロキシなしで起動し直した。
    - 接続先には、1.144.0 と 1.144.1 の両方をラッパー付きで配置していた。
    - `lru.json` の先頭は 1.144.0 だった。1.144.0 での前回の接続で、agent host が書き換えたためである。sshd の設定には、版の固定もプロキシもない。
  - **準備:** 後片付けのスクリプトで、更新前の VS Code Server を止めた。止めたプロセスは 0 件で、`/tmp` から 7 件を削除した。そのあと sshd の設定を作り直し、sshd と転送を開いた。
  - **1.144.1 で接続した結果:**
    - `vscode-status.sh` は警告を出さなかった。supervisor と、動いているサーバーは、どちらも 1.144.1 だった。
    - agent host は `Using initial agent host version override: 261c84cb…` を記録した。`lru.json` の先頭が古い版でも、ラッパーによってクライアントの commit が選ばれた。そのあと agent host は、`lru.json` の先頭を 1.144.1 に書き換えた。
    - CLI と agent host は、ラッパーから exec された `hs-cli/code-261c84…` として動いた。
    - 取得は起きなかった。`Stable-*` は増えず、CLI のログにも取得の記録はなかった。
    - 表示は日本語だった。1.144.0 のときに登録した `languagepacks.json` (言語パック 1.131.0) を、そのまま使えた。
    - 拡張機能ビューの「SSH: hs-vscode-ol10 - インストール済み」に、Hex Editor、C/C++ Themes、言語パック、GitHub Pull Requests が表示された。
    - 言語パックには「更新」のボタンが表示された。クライアントが Marketplace で新しい版を見つけたためと考えられる。
  - **古い版の削除:**
    - 後片付けのあと (止めたプロセスは 0 件、`/tmp` から 8 件を削除)、`vscode-place.sh --prune 261c84…` を実行した。
    - `Stable-2c33b50b…`、`code-2c33b50b…`、`hs-cli/code-2c33b50b…` が削除された。`lru.json` は 1.144.1 だけになり、サーバーの領域は 641M になった。
    - VS Code に「再接続できません」のダイアログが出た。ウィンドウを再読み込みすると、再接続できた。
    - 再接続したあとも、override は 1.144.1 で、取得は起きず、`vscode-status.sh` は警告を出さなかった。
- **言語パックの更新 (オフライン):**
  - 「更新」を押すと、サーバーはまず Marketplace から直接取得しようとして、`getaddrinfo ENOTFOUND` で失敗した。そのあと、クライアントが取得した VSIX が `CachedExtensionVSIXs` に届き、`ms-ceintl.vscode-language-pack-ja-1.131.2026090407` が導入された。
  - `languagepacks.json` の `ja` は、新しい版 (hash `c504…`) を指すように自動で書き換わった。CLI で導入したときと違い、登録の手当ては要らない。
  - 古い版の 1.131.0 は `.obsolete` に記録された。ウィンドウを閉じて開き直しても、サーバー本体は動き続ける。このため、拡張機能ホストは古い hash の翻訳 (`clp/3460….ja`) を使い続けた。
  - 後片付けのスクリプトでサーバーを止めて再接続すると、次のようになった。
    - `clp/c504….ja` が作られ、拡張機能ホストはそれを参照した (`resolvedLanguage: ja`)。表示は日本語だった。
    - 1.131.0 のファイルと `.obsolete` は削除された。古い `clp/3460….ja` は残った。
- **プロキシ経由での確認:**
  - 段階 4 と同じ許可先で bridge を起動し直し、後片付けのあと、sshd の設定を作り直した。設定には `SetEnv` でプロキシを渡し、版の固定は書かない。
  - 再接続すると、agent host は override を 1.144.1 として記録した。`Stable-*` は増えず、`vscode-status.sh` は警告を出さなかった。
  - Copilot Chat は応答した。`copilot-runtime` の環境には `HTTPS_PROXY` があった。
  - Copilot CLI (1.0.83) は、ターミナルからログインなしで起動して応答した。環境には `HTTPS_PROXY`、`NO_PROXY`、`TERM_PROGRAM=vscode` があった。起動時の `Error auto updating: TypeError: fetch failed` (`release-assets.githubusercontent.com` の拒否) は、BL-001 と同じである。入力欄の下には `still waiting on ide` が表示されていた。
  - **プロキシの履歴 (このセッション全体):**
    - 許可された宛先: `api.github.com`、`github.com`、`api.githubcopilot.com`、`api.individual.githubcopilot.com`、`exp.individual.githubcopilot.com`、`telemetry.individual.githubcopilot.com`、`main.vscode-cdn.net`
    - 拒否された宛先: `mobile.events.data.microsoft.com`、`default.exp-tas.com`、`release-assets.githubusercontent.com`、`169.254.169.254`、`metadata.google.internal`
    - `update.code.visualstudio.com` への接続は 0 件だった。プロキシ経由でも、固定すると最新版の確認は行われない。
- **未検証:**
  - 接続中に版の更新が要求されたときの動作 (BL-002)
  - Copilot CLI の `still waiting on ide` が解消するか (VS Code との連携)
- **手順として固まったもの (BL-004 でスキル化する):**
  1. 手元で `vscode-fetch.sh <新しい commit>` を実行し、接続先へ転送する。
  2. 接続先で `vscode-place.sh <配布物>` を実行する。古いクライアントで接続したままでもよい。
  3. クライアントを更新する。
  4. 後片付けのスクリプトを実行し、sshd の設定を作り直して、再接続する。
     言語パックを更新したときも、新しい版の翻訳を使わせるには、同じようにサーバーを止める。
  5. 動作を確認したら、後片付けのあとに `vscode-place.sh --prune <新しい commit>` を実行する。
- スクリプトは、手元のスクラッチ (`vsc2/`) と接続先の `~/hs-vscode/` にある。配布物は、手元の `vscode-dist2/` と接続先の `~/.cache/vscode-dist/` にある。

## BL-004 準備と後片付けの手順が、スクラッチのスクリプトのままである

- **現状:**
  - `sshd-setup.sh`、`vsc-place.sh`、`vsc-cleanup.sh` は、リポジトリの外に置いている。
  - `sshd-setup.sh` は、版の固定だけを指定したときにも `relay proxy passed to sessions` と表示する。
- **あるべき姿:**
  - スキルのスクリプト (ASCII のみ) として整える。BL-002・BL-003 の自動化を含める。
  - 毎回の手順を、host-shell の操作として 1 回で実行できるようにする。手順は次のとおり。
    1. VS Code Server を止める。
    2. 内側の sshd の設定を作り直す。
    3. sshd を起動する。
    4. 転送を開く。
- **経緯と対処 (2026-11-03):**
  - **方針 (利用者と決めたこと):**
    - クライアントの SSH の設定は、専用の鍵と known_hosts だけをスクリプトで作る。`~/.ssh/config` の Host ブロックは表示し、追記は利用者に任せる。
    - 手元での取得のスクリプトは、sh と PowerShell の両方を用意する。
    - Remote - SSH が接続先で自分で取得する経路 (`--commit` で版を固定) は、予備として残す。
    - 文書の正本とスキルには、構成の案の呼び名 (B-2 など) を書かない。
  - **スキルの構成:** `.agents/skills/host-shell-vscode-remote-ssh/` に置いた。
    - 手元: `scripts/client/fetch.sh`、`fetch.ps1` (取得と照合)、`ssh-key.sh`、`ssh-key.ps1` (鍵、known_hosts、Host ブロック)
    - 接続先: `scripts/linux/place.sh`、`authorize.sh`、`start.sh`、`cleanup.sh`、`status.sh`
    - 接続先の置き場所: キットと sshd の設定は `~/.local/share/host-shell-vscode-remote-ssh/{kit,sshd}`、CLI の本体は `~/.vscode-server/host-shell-cli/code-<C>`
    - Host の別名と鍵の名前: `host-shell-vscode-<NAME>`
  - **毎回の手順:** `start.sh` を `job_start` で起動する。`start.sh` は、後片付け、sshd の設定の作り直し、sshd の起動 (前面) を順に行う。そのあと `forward_open` で転送を開く。手順は 2 回の操作になった。
  - **スクラッチ版からの変更:**
    - **後片付けの範囲:** 停止の対象を、コマンドラインが `$HOME/.vscode-server` で始まるプロセスに限った。一時ファイルは `${TMPDIR:-/tmp}` だけを対象とし、プロセスが残っていないときだけ削除する。手元で試験を動かそうとしたとき、自動の安全確認で止められた。スクラッチ版の判定では、手元の WSL の VS Code (Remote - WSL) も対象になるためである。
    - **`place.sh` の再配置:** 配置済みの版を配置し直すときは、サーバーを残し、ラッパーと CLI だけを書き換える。サーバーの領域が不完全で、しかも使用中のときは止める。空き容量の確認 (768 MiB) は、展開するときだけ行う。
    - **`cleanup.sh --purge`:** `~/.copilot` は削除しない。Copilot CLI と共有しているためである。
    - **`authorize.sh`:** 公開鍵の形式を検査する。
    - **sshd の設定:** `UsePAM no` を削除した。OL10 の sshd は PAM なしでビルドされており、接続のたびに `'UsePAM no' is not supported in this build` という警告が出たためである。既定値はもともと `no` である。
  - **確認の結果:**
    - **自動試験 (`tests/scripts.test.sh`):** 偽の配布物と偽の sshd を使う 62 項目。使い捨てのコンテナー (`--network none`) で実行し、Oracle Linux 10 と 8 のイメージの両方で全項目が成功した。
    - **`fetch.sh` (WSL):** 1.144.1 を取得した (約 18 秒、226M)。SHA-256 は BL-003 の値と一致し、再実行では取得を省いた。
    - **`fetch.ps1` (Windows PowerShell 5.1):** 取得できた。`SHA256SUMS` と `COMMIT` は、`fetch.sh` の結果とバイト単位で一致した。
    - **`ssh-key.ps1 key`:** 鍵を作った。パスフレーズが空であることを確かめた。再実行では既存の鍵を使った。
    - **`ssh-key.ps1 trust`:** known_hosts (`[127.0.0.1]:42222`、末尾は LF) と Host ブロックが作られた。
    - **接続先での配置:** 1.144.1 のサーバーが動いている状態で `place.sh` を実行した。サーバーは残り、ラッパーは `host-shell-cli/` を指すように変わった。
    - **新しい手順での接続:**
      - `job_start` で `start.sh` を起動すると、後片付け (止めたプロセスは 0 件、`/tmp` から 7 件を削除)、設定の作り直し、`Server listening on 127.0.0.1 port 2222.` の順に進んだ。
      - 利用者が Host ブロックを追加したあと、`host-shell-vscode-ol10` で接続できた。sshd のログには、新しい鍵の指紋が記録された。
      - `status.sh` は警告を出さなかった。override は 1.144.1 だった。CLI と agent host は、`host-shell-cli/code-261c84…` として動いた。
      - 表示は日本語で、Copilot Chat は応答した。
      - プロキシの履歴に `update.code.visualstudio.com` への接続はなかった。
    - **古いファイルの撤去:** 接続先の検証用の古いファイル (`~/.vscode-server/hs-cli`、`~/hs-vscode`、`~/.local/share/hs-vscode-sshd`) と、キットの配布物を削除した。
  - **未検証:**
    - arm64 の接続先
    - macOS の手元 (`fetch.sh` の `shasum` と `ssh-key.sh`)
    - Linux の VS Code での `ssh-key.sh`。`ssh-key.sh` は、自動試験でだけ確認した。
    - スキルの手順書 (`SKILL.md`) にエージェントが従って、最初から最後まで行う導入
  - **開発者向けの文書:** `docs/ja/development.md` と `docs/en/development.md` に、ソースの構成と保守の説明 (試験の実行方法を含む) を追加した。

## BL-005 セッションを終えても VS Code Server が残る

- **現状:**
  - host-shell のセッションを終えたあと、VS Code Server 一式、agent host、`copilot-runtime`、ターミナルの bash は init の子として残る。
  - 再接続の待ち時間は、既定で 3 時間である。
  - 後片付けのスクリプト (`--purge` なし) で停止できることは検証済である。
- **未検証:**
  - `--purge` によるファイルの削除
  - `remote.SSH.reconnectionGraceTime` を短くしたときの効果
  - 待ち時間が過ぎたあと、サーバーが自分で終了するか
- **あるべき姿:** セッションの開始時と終了時の後片付けを、手順として固定する。

## BL-006 従来方式の scp (`scp -O`) が塞がれていない

- **現状:**
  - `Subsystem sftp` を除いても、接続先に `/usr/bin/scp` がある。このため、シェルを経由する従来方式の scp は使える可能性がある。
  - Remote - SSH が従来方式の scp を使うかどうかは、確認していない。
- **扱い:**
  - 内側の sshd はシェルを提供するので、ファイルの転送を完全に防ぐことはできない。
  - 「scp を使わずに接続できる」ことの確認と、「scp を禁止できる」ことの保証は、分けて文書に書く。

## BL-007 B-2 の仕様の文書がない

- 作成する文書: `docs/ja/vscode-remote-ssh.md` と英訳 (`docs/en/`)
- 内容:
  - 用途
  - 手順 (内側の sshd、事前配置、プロキシの許可先、版の固定と更新、後片付け)
  - 検証済みの範囲 (検証済 / 未検証)
  - 制限 (Web Fetch、管理者の方針、ライセンス、従来方式の scp、終了後に残るプロセス)
- BL-001 から BL-005 の結果を反映してから仕上げる。
- **経緯と対処 (2026-11-03):**
  - `docs/ja/vscode-remote-ssh.md` と英訳 `docs/en/vscode-remote-ssh.md` を作成した。構成は、用途、構成、対象環境、登録コマンド、手順の概要、配置される内容、プロキシ経由の通信、版の固定、VS Code の更新、拡張機能と表示言語、ターミナルと Copilot、終了と削除、検証済みの範囲、制限である。
  - 構成の案の呼び名 (B-2) は、文書の正本とスキルには書かない。案の比較は、検証記録にだけ残す。
  - 索引 (`docs/README.md`、`README.md`)、code-server の文書、スキルの `SKILL.md` から、この文書へのリンクを加えた。
  - BL-005 (終了後に残るプロセス) と BL-006 (従来方式の scp) の未検証の事項は、文書の「検証済みの範囲」と「制限」に未検証として書いた。

## BL-008 検証用にクライアントへ加えた設定が残っている

Windows 側に、次のものが残っている。

- `~/.ssh/config` の `Host hs-vscode-ol10` のブロック
- 専用の鍵 `hs_vscode_ed25519` と `hs_vscode_ed25519.pub`
- 専用の known_hosts `hs_vscode_known_hosts`
- VS Code の設定 `remote.SSH.remotePlatform` に追加された `hs-vscode-ol10`

検証が終わったら撤去する。
