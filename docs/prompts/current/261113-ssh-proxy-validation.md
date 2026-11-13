# SSH 経路プロキシの検証記録

検証 ID: 261113-ssh-proxy

最終仕様: 261113-ssh-proxy-design.md (版 3)

## 環境 (Linux のコンテナー)

- 接続元: WSL2 (カーネル 6.6.87.2) の Oracle Linux 8.10、OpenSSH_8.0p1。host-shell はコミット 886b8e8 の作業ツリー (`host-shell connect` を追加した未コミットの状態) からビルドした (go1.26.0)。
- Podman 4.9.4 (rootless)。イメージは oracle-linux-10-dev に Squid 6.10 (squid-6.10-12.el10_2.1) と httpd-tools を加えたもの。
- ネットワーク:
    - `hs-sp-outer` (internal、10.89.50.0/24): Squid、target1、hop1
    - `hs-sp-inner` (internal、10.89.51.0/24): hop1、target2
    - Squid だけが既定のネットワーク (外部への経路あり) にもつながり、接続元の `127.0.0.1:13128` に公開される。接続元から sshd への経路は Squid だけである。
- Squid: Basic 認証 (`basic_ncsa_auth`、利用者 alice、パスワードに空白と `!` を含む)。`CONNECT` は 22 番と 443 番だけ許可し、`target-denied` は拒否する。`read_timeout` は 60 秒 (無通信の確認では 30 秒)。
- sshd: Oracle Linux 10.2、OpenSSH_9.9p1、glibc 2.39。root で公開鍵認証。
- 接続元の ssh 設定は `-F` で指定した検証用のファイル (利用者の `~/.ssh/config` は変更していない)。

```text
Host target1 hop1 target-denied
    ProxyCommand <lab>/bin/host-shell connect -H alice@127.0.0.1:13128 --password-file <lab>/proxy.pass %h %p
Host target2-jump
    HostName target2
    ProxyJump hop1
Host *
    User root
    IdentityFile <lab>/id_ed25519
    ...
```

## 結果

| 項目 | 確認内容 | 結果 |
| --- | --- | --- |
| 直接の経路がないこと | `ProxyCommand=none` で target1 (10.89.50.11) に接続 | タイムアウト (経路なし) |
| SSH 経路プロキシ越しの接続 | `ssh target1` | 成功。Squid のログに `TCP_TUNNEL/200 CONNECT target1:22 alice` |
| ProxyJump | `ssh target2-jump` (hop1 が経路プロキシ経由) | 成功 |
| 誤ったパスワード | `--password-file` に誤った値 | `rejected the credentials of alice (407)`、終了コード 1。Squid への接続は 1 回だけ。ssh からは `kex_exchange_identification: Connection closed by remote host` が続く |
| ユーザー名なし | `-H 127.0.0.1:13128` | `requires authentication (407); give USER@ in -H or HTTP_PROXY_USER` |
| 宛先の拒否 | `target-denied:22`、`target1:2222` | `refused CONNECT ... (403 Forbidden)` |
| 存在しない宛先 | `nosuch.invalid:22` | `refused CONNECT ... (503 Service Unavailable)` |
| プロキシに接続できない | 待ち受けていないポート | `cannot connect to SSH route proxy ...: connection refused` |
| パスワード ファイルの権限 | mode 644 と 664 | 644 は成功、664 は `bad owner or permissions on password file ...: mode 0664 lets the group or others write to it` |
| 診断 (`-d`) | 接続・送信・応答・中継開始を表示 | 資格情報は表示されない |
| host-shell-connect のスクリプト (1 段目) | `HOST_SHELL_SSH_OPTS="-F <lab>/ssh_config"` で `remote.sh target1 true`、`detect.sh`、`deploy.sh` (約 13 MiB と DNS アダプター)、`probe.sh` | すべて成功。経路の確認の出力は空 |
| 入れ子の多段 | `hop1,target2` で同じスクリプト | すべて成功 |
| ProxyJump の多段 | `target2-jump` で同じスクリプト | すべて成功 (`up to date`) |
| bridge 経由の MCP | `sys_get_info`、`exec_run`、20,000,000 バイトの `transfer_push`・`transfer_pull` | 成功。SHA-256 一致、取得したファイルも一致 |
| インターネット向けプロキシとの併用 | bridge の起動環境に `HTTPS_PROXY=http://alice:<URL エンコードしたパスワード>@127.0.0.1:13128`、`--proxy --proxy-allow-host api.github.com:443`、接続先 `--client-proxy` | 接続先からの直接の通信は名前解決できず失敗。`net_request_http` と接続先の `curl` は `api.github.com` から応答を取得。Squid のログに `CONNECT api.github.com:443 alice`。bridge で未許可の `example.com:443` は 403 |
| 用語の置き換え後の理由の文字列 | `bridge` で `forbidden.test:443` を許可し、インターネット向けプロキシ (Squid) がその `CONNECT` を拒否する | 接続先の `curl` は `CONNECT tunnel failed, response 502`。`proxy_status`、`bridge` の標準エラー、接続先の操作ログ (`proxy_connect`) の理由はいずれも `connection from bridge failed` |
| 無通信の切断 | Squid の `read_timeout 30 seconds`、`sleep 75` のコマンド | `ServerAliveInterval` なしは 30 秒で切断 (`client_loop: send disconnect: Broken pipe`、終了コード 255)。`ServerAliveInterval 10`、`ServerAliveCountMax 3` では 75 秒後に正常終了 |

## 自動テスト

`go test -race ./internal/routeproxy` が Linux で成功した (32 件)。用語の置き換えの後、`go test -race ./...` 全体が Linux で成功し、`go vet ./...` が Linux・Windows・macOS で成功した。Windows と macOS でのテストの実行は CI で確認する。

## Windows の実機 (手順。未実施)

Windows のクライアント (`host-shell.exe connect`) と Tera Term を確かめる手順である。Windows 側のセッションで実施する。Squid と sshd は、上の Linux の検証環境 (WSL の Podman) をそのまま使う。

1. **Squid に Windows から届くようにする**: WSL の `lab.sh` で Squid を `-p 127.0.0.1:13128:3128` で公開している。Windows の `localhost:13128` から届くか (WSL の localhost 転送) を `Test-NetConnection localhost -Port 13128` で確かめる。届かない場合は、公開を `0.0.0.0:13128` に変えて WSL の IP アドレスを使う。
2. **配置**: `dist/host-shell-windows-amd64.exe` を `C:/Users/<user>/bin/host-shell.exe` に置く。検証用の鍵 (`id_ed25519`) をコピーし、本人だけが読めるようにする (`icacls <鍵> /inheritance:r /grant:r "%USERNAME%:F"`)。
3. **パスワード ファイル**: `ssh-proxy.md` の PowerShell の手順で `%USERPROFILE%\.ssh\proxy-lab.pass` を作る (パスワードは `S3cret pass!`)。
4. **ssh の設定** (`%USERPROFILE%\.ssh\config-lab`。パスの区切りは `/`):

    ```text
    Host target1
        HostName target1
        User root
        IdentityFile C:/Users/<user>/.ssh/lab_ed25519
        IdentitiesOnly yes
        UserKnownHostsFile C:/Users/<user>/.ssh/known_hosts_lab
        StrictHostKeyChecking accept-new
        ProxyCommand C:/Users/<user>/bin/host-shell.exe connect -H alice@localhost:13128 --password-file C:/Users/<user>/.ssh/proxy-lab.pass %h %p
        ServerAliveInterval 30
    ```

5. **確認すること** (Windows の OpenSSH `C:\Windows\System32\OpenSSH\ssh.exe` と Git for Windows の `ssh.exe` の両方で):
    - `ssh -F <config-lab> target1 "id -un"` が `root` を返す。Squid のログに `CONNECT target1:22 alice`。
    - `host-shell.exe bridge -- ssh -F <config-lab> -T target1 /tmp/host-shell-0/host-shell --mcp` で MCP を操作できる (`exec_run`、`transfer_push`・`transfer_pull` の SHA-256 一致)。登録は検証用の名前で行う。
    - 誤ったパスワードのファイルで `rejected the credentials of alice (407)` になる。
    - パスワード ファイルの権限: プロファイル配下の既定の ACL で通る。`icacls <file> /grant "Users:(R)"` を加えても通る。`icacls <file> /grant "Users:(W)"` を加えると `BUILTIN\Users has write access` で拒否される。継承を外して本人だけにすると通る。
    - 標準エラーが端末のとき (PowerShell から `ssh` を直接実行し、`--password-file` と環境変数を外したとき) にパスワードを尋ね、入力が表示されない。
6. **Tera Term**: 「設定 > プロキシ」で種別 HTTP、ホスト `localhost`、ポート `13128`、ユーザー `alice`、パスワードを設定し、`target1:22` に SSH で接続できる (同じ Squid、同じ sshd)。`ssh-proxy.md` の対応表どおりの設定で、`host-shell connect` と同じ経路になることを確かめる。
7. **自動テスト**: Windows で `go test ./internal/routeproxy/` を実行する (アクセス許可の判定のテストを含む)。CI で実行される場合は、その結果で代える。
