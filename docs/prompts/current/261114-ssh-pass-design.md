# SSH のパスワードの保存 (ssh-pass) 設計記録

## 要求

SSH のユーザー名・パスワード認証を、安全・標準的・永続的・多段対応で行えること。VS Code の標準機能 (Remote - SSH) と親和性があること。Linux と Windows で操作感を揃えること。想定する経路は次の 2 つ (証明書・公開鍵認証は既存の手段で実現済みのため対象外)。

1. 接続元 → HTTP プロキシ (Basic 認証) → 1 段目 (パスワード) → 2 段目 (1 段目の認証情報で自動認証)
2. 接続元 → HTTP プロキシ (Basic 認証) → 1 段目 (パスワード) → 2 段目 (パスワード)

「1 段目の認証情報で自動認証」は、1 段目のサーバー上の鍵や hostbased 認証で 2 段目に入ることを指し、接続元は 1 段目と 2 段目の間の認証に関与しない (利用者の定義)。

## 出発点 (コミット 36ae29c) のレビューで挙げた点

| 区分 | 内容 |
| --- | --- |
| 高 | OpenSSH 8.4 より前は keyboard-interactive の問い合わせをサーバーの文字列のまま askpass に渡すため、悪意あるサーバーが `alice@hop1's password:` を送ると別ホストのパスワードを取得できる。`DISPLAY` の補完はこの版を対象にしていた |
| 高 | VS Code Remote - SSH は自分で `ssh` を起動し `SSH_ASKPASS` を設定するため、保存したパスワードは使われない |
| 中 | `host-shell-connect` スキルのスクリプトは `bridge` を通さず `BatchMode=yes` の入れ子 ssh で動くため、配置の段階で使えない |
| 中 | 誤ったパスワードを `NumberOfPasswordPrompts` (既定 3) 回送り、再接続のたびに繰り返すためアカウントがロックされうる |
| 中 | keyboard-interactive (PAM) だけを提供する sshd で使えない |
| 低 | `bridge` が ssh 以外のコマンドにも `DISPLAY=host-shell` を設定する、目印の環境変数を継承した子プロセスの誤認、`HostKeyAlias` の未記載、ホスト名の大文字・小文字、平文保存 |

## 決定

| 項目 | 決定 | 理由 |
| --- | --- | --- |
| 方式 | OpenSSH 標準の `SSH_ASKPASS` と `SSH_ASKPASS_REQUIRE=force` を維持する | 標準の仕組みで、`ProxyJump` の子 ssh にも環境が引き継がれる。`sshpass` のような端末の偽装は使わない |
| 構成 2 | `ProxyJump` (接続元が両段を認証)。2 段目のパスワードは 1 段目に見えない | 入れ子では 2 段目の ssh が 1 段目で動き、接続元の保存を使えない |
| 構成 1 | 入れ子の `ssh hop1 ssh target` (2 段目は hop1 上の ssh が hop1 の鍵で認証)。VS Code は vscode-remote-ssh の構成 (MCP 経由のポート転送と専用の鍵) を使う | Remote - SSH は `RemoteCommand` を使えず、接続元の ssh が 2 段目を認証する形しか取れないため |
| 答える問い合わせ | `USER@HOST's password:` と `(USER@HOST) Password:` (8.4 以降のみ) | どちらも `USER@HOST` 部分を ssh 自身が作る。後者は PAM の keyboard-interactive に対応する |
| 版の判定 | `ssh -V` で 8.4 以降なら有効。8.4 より前は既定で無効で、`HOST_SHELL_SSH_ASKPASS_LEGACY=1` で有効 (パスワード形式のみ、`DISPLAY` を補う、bridge は ssh を setsid で起動) | 8.4 より前の偽装を防ぐ手段が askpass 側にない。8.0p1 では `KbdInteractiveAuthentication no` が効かず、`PreferredAuthentications publickey,password` が必要なことを検証で確認した |
| 端末での問い合わせ | 前の askpass がなく、askpass の標準エラーが端末の場合は、ssh と同じく端末で尋ねる (`(yes/no` はエコーあり)。どちらもなければ失敗 (拒否済みのときは ssh を終了) | VS Code Remote - SSH のターミナル方式 (Windows の既定) は SSH_ASKPASS を使わず、ターミナルの表示を読み取って入力欄を出す (実機確認で判明)。手元のターミナルでも通常の ssh と同じ操作感になる。エージェントの ssh は標準エラーがパイプなので尋ねない |
| askpass の連鎖 | 既存の `SSH_ASKPASS` を `HOST_SHELL_SSH_ASKPASS_NEXT` に退避し、答えない問い合わせはそちらへ渡す | VS Code の入力欄 (未保存のホスト、ホスト鍵の確認、拒否後の再入力) と共存させるため |
| VS Code | `host-shell ssh`・`host-shell scp` ラッパー。専用のフォルダーに `ssh`・`scp` の名前 (argv[0] で判定) で置き、その `ssh` を `remote.SSH.path` に指定する。本物は PATH から自身を除いて探す | `remote.SSH.path` は引数を取れない。Remote-SSH はサーバーの転送 (オフライン時) に `remote.SSH.path` と同じフォルダーの `scp` を `-S` なしで使う (実機確認で判明)。Windows ではシンボリック リンクに権限が要るため、ハード リンクかコピーを案内する |
| Windows のラッパー | 子プロセスとして ssh を起動し、Job Object (kill on close) に入れ、Ctrl+C は無視して終了コードを返す | exec がないため。VS Code がラッパーを終了させたときに ssh を残さない |
| 拒否の検出 | 1 つの ssh プロセスに 1 回だけ渡す。2 回目の問い合わせで拒否と判断して記録し、`ssh-pass set` まで送らない。次の問い合わせ先がなければ ssh を終了させる (SIGTERM / TerminateProcess) | ssh は askpass が失敗しても空のパスワードで試行を続け、PAM はそれも失敗として数える (検証で確認)。ssh を止めない限りサーバー側の失敗が増える |
| ssh プロセスの識別 | PID と起動時刻。Windows は連続する ssh.exe の最上位 | コンテナーで PID が再利用され誤検知した。Git for Windows の ssh は問い合わせごとに fork を模した ssh.exe を挟む |
| `NumberOfPasswordPrompts` | 既定 (3) のままにする | 1 にすると 2 回目の問い合わせが来ず、拒否を検出できない |
| 保存 | Linux・macOS は 0700/0600 の平文 1 行。Windows は DPAPI (エントロピー付き) と本人だけの保護された DACL。キーのホスト部は小文字に正規化し、ファイル名では大文字を `%XX` にする | Windows は利用者の資格情報で暗号化でき、`%TEMP%` など継承 ACL が緩い場所でも権限確認を通す。大文字・小文字を区別しないファイル システムでもユーザー名を区別する |
| 端末なしの入力 | `ssh-pass set --stdin` | 非対話のシェル (Windows 側のエージェントなど) から保存するため |
| スキル | `remote.sh` に `HOST_SHELL_SSH` (1 段目のコマンド) を追加し、そのときは 1 段目に `-o BatchMode=no` を先に付ける (ssh は最初の値を採用) | 中継サーバー上の ssh は `BatchMode=yes` のままにする |
| 誤認の防止 | `Serve` は目印の環境変数があり、引数が 1 つで、問い合わせらしい文字列 (`:`、`?` で終わる、改行を含む) のときだけ動作する | ssh の子プロセス (ProxyCommand など) が `host-shell` を 1 引数で起動した場合に誤認しない |

## 対象外

Linux・macOS の OS 資格情報ストア、鍵のパスフレーズ、`Password:` 以外の keyboard-interactive (ワンタイム パスワード)、パスワード変更の要求、ポート番号による区別 (`HostKeyAlias` で代替)。
