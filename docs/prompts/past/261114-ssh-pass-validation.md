# SSH のパスワードの保存 (ssh-pass) 検証記録

検証 ID: 261114-ssh-pass

設計: 261114-ssh-pass-design.md

## 環境

- WSL2 (カーネル 6.6.87.2) の Oracle Linux 8.10。OpenSSH_8.0p1。go1.26.0。Podman 4.9.4 (rootless)。host-shell は feature/ssh-pass (36ae29c) に未コミットの変更を加えた作業ツリーからビルドした。
- 検証環境 (スクラッチパッドの `lab.sh`、`setup.sh`)。イメージは oracle-linux-10-dev に Squid 6.10 と httpd-tools を加えたもの。sshd はすべて Oracle Linux 10.2、OpenSSH_9.9p1、UsePAM yes。
    - `hs-pw-outer` (internal、10.89.60.0/24): Squid (10.89.60.10、接続元の `127.0.0.1:13128` に公開)、hop1 (10.89.60.11)
    - `hs-pw-inner` (internal、10.89.61.0/24): hop1 (10.89.61.11)、target2 (10.89.61.12)、target3 (10.89.61.13)
    - Squid: Basic 認証 (alice、`Proxy pass!`)、`CONNECT` は 22 番のみ
    - hop1: alice、`PasswordAuthentication yes`、`KbdInteractiveAuthentication no`。alice の `~/.ssh/id_ed25519` は target3 の carol に登録 (接続元にはない鍵)
    - target2: bob、`PasswordAuthentication no`、`KbdInteractiveAuthentication yes` (PAM の `Password:`)
    - target3: carol、公開鍵認証のみ (パスワードはロック)
- 接続元の ssh 設定は `-F` で指定する検証用のファイル (hop1 は `host-shell connect` で Squid 経由、target2 は `ProxyJump hop1`)。利用者の `~/.ssh/config` は変更していない。
- 接続元の版: Linux は WSL の OpenSSH 8.0p1 (8.4 より前) と、`--network host` のコンテナー (Oracle Linux 10.2、OpenSSH 9.9p1)。Windows は WSL の相互運用で実行した Windows 11 Pro for Workstations (ビルド 26300) の `host-shell.exe`、Windows の OpenSSH_for_Windows_9.5p2、Git for Windows の OpenSSH_10.6p1。
- 偽装の試験用サーバー: golang.org/x/crypto/ssh v0.43.0 で作成。keyboard-interactive で指定の文字列を問い合わせ、受け取った応答を記録して拒否する。

## 結果

| 項目 | 内容 | 結果 |
| --- | --- | --- |
| 偽装 (8.0p1、既定) | `alice@hop1's password:` を keyboard-interactive で送るサーバー | 有効にならず、既定の ssh-askpass (存在しない) が失敗。応答は空 |
| 偽装 (8.0p1、LEGACY=1) | 同上 | 保存した hop1 のパスワードを 1 回送った (再試行の検出で 2 回目以降は空)。既知の危険として文書化 |
| 偽装 (8.0p1、LEGACY=1、`KbdInteractiveAuthentication=no` または `ChallengeResponseAuthentication=no`) | 同上 | 8.0p1 では keyboard-interactive が行われ、1 回送った |
| 偽装 (8.0p1、LEGACY=1、`PreferredAuthentications=password` または `publickey,password`) | 同上 | keyboard-interactive を行わず、送らない |
| 偽装 (9.9p1、Windows 9.5p2、Git for Windows 10.6p1) | `alice@hop1's password:` と `(alice@hop1) Password:` | ssh が `(bob@127.0.0.1) ` を前置するため照合せず、すべて空の応答 |
| keyboard-interactive の正常系 | 問い合わせ `Password:` に対し `bob@127.0.0.1` を保存 | 9.9p1、Windows 9.5p2、Git 10.6p1 で保存したパスワードを送った |
| 8.0p1、LEGACY=1 | hop1 (パスワード認証、Squid 経由)、入れ子 hop1 → target3 | `alice`、`carol`。target2 (keyboard-interactive) は設計どおり答えず失敗 |
| 9.9p1 の構成 A・B | `host-shell ssh` で hop1、target2 (ProxyJump、keyboard-interactive)、`hop1 ssh target3` | `alice`、`bob`、`carol`。Squid のログに `CONNECT hop1:22 alice` |
| 拒否の検出 (初版) | `NumberOfPasswordPrompts=3` で誤ったパスワード | 誤ったパスワードは 1 回だけだが、ssh が残り 2 回を空の応答で試行し、PAM の失敗は 3 回 |
| 拒否の検出 (初版) | `NumberOfPasswordPrompts 1` | 2 回目の問い合わせが来ず検出できない。再接続のたびに誤ったパスワードを送る |
| 拒否の検出 (PID のみで識別) | 連続したコンテナーの実行 | PID の再利用で、hop1 の正しいパスワードを拒否と誤判定 |
| 拒否の検出 (最終) | 既定の `NumberOfPasswordPrompts`、誤ったパスワード → 再接続 → `set` → 再接続 | 1 回目: 拒否を記録し ssh を終了 (143)。2 回目: 送らずに ssh を終了。`set` の後に成功。target2 の PAM の失敗は合計 1 回 |
| 同 (Windows) | Windows 9.5p2、Git 10.6p1 で同じ手順。続けて、前の askpass として `.cmd` (正しいパスワードを返す) を設定して再接続 | 失敗は各 1 回。終了コード 255。`.cmd` が `(bob@target2) Password:` を受け取り、接続に成功 |
| Windows の ssh プロセスの識別 | Git for Windows の ssh での askpass の親 | `host-shell.exe ← ssh.exe (問い合わせごとに別) ← ssh.exe` の構造。連続する ssh.exe の最上位を使うよう修正し、再試行の検出が動作 |
| スキルのスクリプト (9.9p1) | `HOST_SHELL_SSH="host-shell ssh -F <cfg>"` で `remote.sh`・`detect.sh`・`deploy.sh`・`probe.sh`。経路 `target2` と `hop1,target3` | すべて成功 (`deployed`、`OK: serverInfo`) |
| bridge 経由の MCP | `initialize` と `exec_run` (`id -un` と `/etc/hostname`) | 9.9p1 (Linux)、Windows 9.5p2、Git 10.6p1 のいずれも、構成 A で `bob target2`、構成 B で `carol target3` |
| 8.0p1 の bridge (LEGACY=1) | `script` で疑似端末を与えた下で、構成 B の bridge 経由の MCP | `carol target3`。ssh は新しいセッションで起動され、端末から読み取らない |
| 別名での起動 | Linux はシンボリック リンク `host-shell-ssh`、Windows は `mklink /H host-shell-ssh.exe` | target2 に接続。`-V` は ssh の版を表示 |
| Windows のラッパーの強制終了 | `host-shell-ssh.exe -F <cfg> target2 sleep 60` (ProxyJump で ssh.exe が 2 つ) の実行中に `taskkill /F` | Windows 9.5p2、Git 10.6p1 とも、ssh.exe は 2 → 0。接続先の `sleep` は残る (端末なしの ssh の通常の動作) |
| DPAPI | Windows で `ssh-pass set --stdin` | ファイルは `dpapi:` で始まり、平文を含まない。`list` と接続で復号 |
| 自動テスト | `go test -race ./...` (Linux)、`go vet ./...` (Linux・Windows・macOS)、`internal/sshpass` のテスト バイナリを Windows で実行 | 成功。Windows の初回は `%TEMP%` の継承 ACL (名前を解決できない SID への書き込み許可) で権限確認に失敗したため、保存時に本人だけの保護された DACL を設定するよう変更した |

## VS Code Remote - SSH (Windows 側のセッションによる確認)

### 1 回目 (host-shell-ssh.exe を remote.SSH.path に指定)

- VS Code 1.145.3 (dcd3ad6e) の ZIP 版 (portable)、Remote-SSH 0.128.0。利用者の VS Code が保留中の自動更新 (`vscode-updating` ミューテックス) を持っていたため、インストール版の別データ ディレクトリでは起動できず、ZIP 版に切り替えた。
- パスワードの入力欄は出ず、target2 上のインストール スクリプトが「Installing to /home/bob/.vscode-server」まで進んだ。
- `remote.SSH.localServerDownload: always` で、Remote-SSH が `remote.SSH.path` と同じフォルダーの `scp.exe` を探し、「scp installation not found」で失敗した (extension.js では `-S` なしで `dirname(remote.SSH.path)\scp.exe` を呼ぶ)。
- PATH 上で Git for Windows の ssh (MSYS、10.6p1) が先にあり、ラッパーはそれを起動した。この ssh では `remote.SSH.configFile` をバックスラッシュで書くと、ProxyJump の内側の ssh が「Can't open user config file C:Userstetsuohs-sshpass-testssh_config」で失敗した (sh 経由の起動で `\` が失われると推定)。スラッシュでは成功した。
- `remote.SSH.remotePlatform` がないとプラットフォームの選択で止まる。新しい VS Code は最初に Copilot のサインイン画面を出すため、`chat.disableAIFeatures: true` を加えた。
- エラーのダイアログで閉じられず、プロセスを強制終了したところ、10 秒後に ssh.exe・host-shell-ssh.exe・host-shell.exe は残っていなかった。

対応: `ssh`・`scp` の名前で起動したときのラッパーに変えた (host-shell-ssh の名前は廃止)。専用のフォルダーに `ssh.exe`・`scp.exe` を置く手順にし、Windows では `remote.SSH.configFile` の区切りに `/` を使うこと、`remote.SSH.remotePlatform` の指定を文書に加えた。

| 項目 | 内容 | 結果 |
| --- | --- | --- |
| scp のラッパー (Linux) | シンボリック リンク `ssh`・`scp` を置いたフォルダーを PATH の先頭に置き、3 MB のファイルを target2 (ProxyJump) へ `scp` | 成功、SHA-256 一致 (scp が起動する `ssh` もラッパーを経由し、二重の設定にならない) |
| scp のラッパー (Windows) | ハード リンク `ssh.exe`・`scp.exe`。`HOST_SHELL_SSH_PATH`・`HOST_SHELL_SCP_PATH` で Windows の OpenSSH を指定した場合と、PATH (Git for Windows) の場合 | どちらも成功、SHA-256 一致 |

### 2 回目 (ssh.exe・scp.exe のラッパー)

既定の設定 (Windows ではターミナル方式。Remote-SSH は SSH_ASKPASS を設定せず、ターミナルの表示から問い合わせを読み取る):

- a: scp でサーバーを転送し、入力欄なしで接続した。リモートのターミナルで `id -un` は bob。
- c: 通常の閉じる操作の 15 秒後に host-shell・ssh (閉じる前は 3) は 0。
- b: 不成立。保存を削除して再接続すると、入力欄が出ずに約 9.5 秒で `Permission denied (publickey,keyboard-interactive)`。askpass を強制しているため ssh がターミナルに問い合わせを出さず、VS Code が入力欄を出せない。

`remote.SSH.useLocalServer: true` (VS Code が SSH_ASKPASS を設定する) を加えた場合:

- b: hop1 は保存したパスワードで自動、target2 は「Got askpass request: (bob@target2) Password:」で入力欄が出て、入力して接続。
- 3 (誤ったパスワードを保存): 「the stored password for bob@target2 was rejected and is not sent again」の後に入力欄が出て、正しいパスワードで接続。
- a: 保存し直した後、入力欄なしで hop1・target2 とも自動。vscode-server は bob で 10 プロセス。
- c: 各回とも閉じた後に ssh・scp・host-shell は 0。

対応: 前の askpass がなく標準エラーが端末の場合は、askpass が端末で問い合わせる (パスワードはエコーなし、`(yes/no` はエコーあり) ようにした。

| 項目 | 内容 | 結果 |
| --- | --- | --- |
| 端末での問い合わせ (9.9p1) | `script` の疑似端末で `host-shell ssh target2`。target2 のホスト鍵は未登録、bob@target2 は未保存 | ホスト鍵の確認に `yes` (表示される)、`(bob@target2) Password:` に入力 (表示されない) して `bob`。hop1 は保存したパスワードで自動 |
| 標準エラーがパイプの場合 | 同じ条件で端末なし | 問い合わせずに `Permission denied` ですぐ失敗 |

### 3 回目 (端末での問い合わせを加えた版、ターミナル方式)

`remote.SSH.useLocalServer` を外した既定の設定。パスワードの入力は Windows 側のセッションが SendKeys で行った。

- Git for Windows の ssh (PATH): b は「(bob@target2) Password:」→「Showing password prompt」で入力欄が出て接続 (askpass は git ssh.exe の子として `vscode-ssh\ssh.exe "(bob@target2) Password: "` で起動)。3 は拒否のメッセージの後に入力欄が出て接続。a は入力欄なしで接続し `id -un` が bob。c は閉じた 15 秒後に ssh・scp・host-shell・Code.exe が 0。
- `HOST_SHELL_SSH_PATH`・`HOST_SHELL_SCP_PATH` に System32 の OpenSSH: a は入力欄なしで接続 (ProxyJump の子は System32 ssh.exe、sh.exe は使われない)。b は入力欄が出て接続 (askpass は System32 ssh.exe の子)。サーバーは転送済みのため、System32 の scp.exe は VS Code からは使われていない (WSL からの scp の試験では成功している)。

## CI

PR #8 (マージ前の最終コミット 0c3be50) の CI ([run 43472040363](https://github.com/Hondarer/host-shell-mcp/actions/runs/43472040363)) で、test (ubuntu-latest・windows-latest・macos-latest)、dns-lib (amd64・arm64)、build のすべてが成功した。`internal/sshpass` のテストは macOS でも成功した。PR #8 は main にマージした (bf4c5e1)。

## 未実施

- macOS のクライアントでの接続 (自動テストは CI で成功)。
