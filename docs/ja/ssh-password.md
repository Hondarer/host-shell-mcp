# SSH のパスワードの保存 (ssh-pass)

パスワード認証のジャンプサーバーや接続先に、パスワード入力なしで接続する方法を説明します。`host-shell ssh-pass` でパスワードを保存しておくと、`host-shell bridge` が起動する `ssh` へ、OpenSSH 標準の `SSH_ASKPASS` の仕組みで渡します。経路 (`ProxyJump`、`ProxyCommand`、ユーザー名、ポート) は、VS Code の Remote - SSH と同じく、クライアントの `~/.ssh/config` に記述します。

## 使いどころ

- 公開鍵認証を使えない (使わせてもらえない) ジャンプサーバーや接続先を、MCP のエージェントから使う。
- 認証付き HTTP プロキシ ([SSH 経路プロキシ](ssh-proxy.md)) の先にあるパスワード認証のサーバーへ接続する。プロキシのパスワードは `--password-file` で、SSH のパスワードは `ssh-pass` で、それぞれ保存します。

## 手順

1. `~/.ssh/config` に経路を記述します。パスワード認証の段が複数ある場合は、入れ子の `ssh hop1 ssh target` ではなく `ProxyJump` を使います。

    ```text
    Host hop1
        HostName hop1.example.com
        User alice

    Host target
        HostName target.internal
        User alice
        ProxyJump hop1
    ```

2. 段ごとに、`ssh` がパスワードの入力時に表示する `USER@HOST` でパスワードを保存します。`HOST` は `~/.ssh/config` の `HostName` を適用した後のホスト名です。端末で、入力は表示されません。

    ```text
    host-shell ssh-pass set alice@hop1.example.com
    host-shell ssh-pass set alice@target.internal
    ```

3. エージェントの登録は通常と同じです。`bridge` が起動する `ssh` に、保存したパスワードが渡されます。

    ```text
    host-shell bridge -- ssh -T -o BatchMode=no target /path/to/host-shell --mcp
    ```

    `BatchMode=yes` を指定するとパスワード認証は行われないため、パスワードを保存した段の `ssh` には指定しません。

## サブコマンド

| コマンド | 動作 |
| --- | --- |
| `host-shell ssh-pass set USER@HOST` | 端末 (エコーなし) からパスワードを読み取って保存する。既存のものは置き換える |
| `host-shell ssh-pass delete USER@HOST` | 保存したパスワードを削除する |
| `host-shell ssh-pass list` | 保存済みの `USER@HOST` を表示する (パスワードは表示しない) |
| `host-shell ssh-pass dir` | 保存先のディレクトリを表示する |

Table: ssh-pass のサブコマンド

## 動作

- `bridge` は、起動する `ssh` の環境に `SSH_ASKPASS` (`host-shell` 自身)、`SSH_ASKPASS_REQUIRE=force` を設定します。OpenSSH 8.4 より前は `SSH_ASKPASS_REQUIRE` を解釈しないため、`DISPLAY` が未設定の場合は `DISPLAY` にも値を設定します。空でない `SSH_ASKPASS` がすでに設定されている場合は、何も変更しません。
- `ssh` から呼び出された `host-shell` は、「`USER@HOST's password:`」の問い合わせにだけ保存済みのパスワードを返します。ホスト鍵の確認や鍵のパスフレーズの問い合わせ、保存していない `USER@HOST` には何も返さず、`ssh` の認証は失敗します。ホスト鍵は、あらかじめ端末で一度 `ssh` を実行して `known_hosts` に登録してください。
- 保存先は、既定でユーザーの設定ディレクトリの `host-shell/ssh-passwords` です (Linux・macOS は `$XDG_CONFIG_HOME` または `~/.config`、Windows は `%AppData%`)。環境変数 `HOST_SHELL_SSH_PASSWORD_DIR` で変更できます。1 つの `USER@HOST` につき 1 つのファイルで、パスワードを 1 行で保存します。
- Linux・macOS は、ディレクトリを `0700`、ファイルを `0600` で作成します。Windows は、ユーザー プロファイルの ACL を継承します。読み取り時は、[パスワード ファイルの権限](ssh-proxy.md#パスワード-ファイルの権限の確認) と同じ条件を確認します。
- パスワードは暗号化せずに保存します (OS の資格情報ストアは使用しません)。保護の水準は、パスフレーズなしの秘密鍵と同等です。

## 検証済みの範囲

| 項目 | 方法・環境 | 状態 |
| --- | --- | --- |
| 保存・取得・削除・一覧、プロンプトの照合 (パスワード以外の問い合わせへの拒否)、`SSH_ASKPASS` の環境の組み立て | 自動テストを Linux で実行 | 検証済 |
| `ssh-pass set` の端末入力、`bridge` 経由の `ssh` の直接接続と `ProxyJump` (2 段、各段にパスワード認証) | Ubuntu 24.04 の OpenSSH 9.6p1 (クライアントと sshd は同一ホスト) | 検証済 |
| OpenSSH 8.4 より前 (`DISPLAY` による起動) | なし | 未検証 |
| Windows、macOS のクライアント (保存先の ACL、権限の判定、`ssh-pass set` の端末入力) | なし | 未検証 |

Table: 検証状況

## 制限

- 入れ子の `ssh hop1 ssh target` では、2 段目の `ssh` が中継サーバー上で動くため、クライアントに保存したパスワードを使えません。`ProxyJump` を使ってください。
- パスワードの照合はユーザー名とホスト名だけで行い、ポート番号は区別しません。
- 鍵のパスフレーズ、キーボード インタラクティブ認証 (ワンタイム パスワードなど) には対応していません。
- OS の資格情報ストア (Windows 資格情報マネージャー、Secret Service、キーチェーン) には対応していません。
