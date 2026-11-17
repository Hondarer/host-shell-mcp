<!--
生成元: agree-before-implement スキル (検証記録)
文書種別: 検証記録 (validation)
最終更新日時: 2026-11-18 03:10:00 JST
最終更新エージェント: claude code / セッション ID: session_01KJc53SwZ5UXzt7kJWHJ48p
稼働 OS: Linux (WSL)
上流文書: 261118-cli-session-discussion.md (検討用 Markdown)、261118-cli-session-design.md (最終仕様)。削除されている場合があります。
-->

# MCP を登録できないエージェントからの CLI による利用 検証記録

最終仕様「検証と受入条件」に対する検証の記録です。対象のコミットは feature/cli-session の f866bc8 (実装)、921e74d (終了時のファイル削除の順序の修正)、およびスキルの文言の修正を含む作業ツリーです。

## 自動テスト

| 環境 | 結果 |
| --- | --- |
| Linux (WSL、Oracle Linux 8.10、go1.27.4)。`go test -race` を含む | 成功 |
| CI (GitHub Actions、PR #9) の ubuntu-latest、windows-latest、macos-latest | f866bc8 では windows-latest の `TestControl` が失敗 (`session stop` 直後に状態ファイルが残る)。921e74d で修正し、3 OS とも成功 |
| Windows 11 Pro for Workstations 10.0.26300.9974 (Windows 側のセッション `host-shell-mcp-5b`)。WSL でクロスビルドしたテスト バイナリ (f866bc8) | `session`、`cli`、`channel`、`ownerdir`、`bridge` は成功。`cmd/host-shell` の `TestForwardStdio`、`TestForwardStdioPermissions`、`TestProxyStdio` は実行時に `go build` を呼ぶため、Go のない機械で失敗 (今回の変更とは無関係のため対象外) |

Table: 自動テスト

## Linux (WSL) の実機

### 環境

- 手元: Windows 11 上の WSL2 (Oracle Linux 8.10、カーネル 6.6.87.2-microsoft-standard-WSL2)、OpenSSH_8.0p1、Podman 4.9.4 (rootless)。`XDG_RUNTIME_DIR=/run/user/1000`。
- 接続先: Podman のコンテナー (Oracle Linux 10.2、glibc 2.39、OpenSSH_9.9p1 の sshd)。利用者 `dev` の公開鍵認証で `127.0.0.1:12222` に接続。`host-shell-connect` の `deploy.sh` で `/tmp/host-shell-1000/host-shell` (v20261114.0.0-1-gf866bc8) と名前解決のアダプターを配置した。
- 経路: `ssh -F <検証用の ssh_config> -T hs-target` と `podman exec -i` (`probe.sh podman:hs-target`)。

### probe.sh と --finish-on-eof

- `probe.sh hs-target /tmp/host-shell-1000/host-shell` は `OK` を出力し、0.2 秒で終了した (固定の待ち時間を削除)。`podman:hs-target` の経路でも `OK`。
- ローカルの `host-shell --mcp` に 3 行 (initialize、notifications/initialized、tools/call) を流して標準入力を閉じた場合、`--finish-on-eof` ありでは応答 2 行と終了コード 0、なしでは応答なしと終了コード 1 (標準エラーに未応答の件数) となった。

### 手動での確認

- 切り離したセッション (接続先はローカルの `host-shell --mcp`) で、`tools`、`call` (`exec_run`、`fs_write` と `content=@FILE`、`fs_read`、`--json -`、`transfer_push` と進捗の表示)、未知のツール (終了コード 2)、未知の引数 (2。使える引数の一覧を表示)、型の誤り (2)、存在しないファイルの `fs_read` (1)、同じ名前のセッションの開始の拒否、`session stop` 後の `call` (3) を確認した。
- ソケットのパスが長すぎる場合は、`HOST_SHELL_SESSION_DIR` を案内するエラーになることを確認した。
- 疑似端末 (Python の `pty`) で前面のセッションを起動し、`--confirm fs_remove` の接続先に対して `y` で削除 (終了コード 0)、Enter で拒否 (終了コード 1、ファイルは残る) を確認した。`session status` の `confirm` は `terminal`。

### エージェントからの操作

各エージェントを非対話で実行し、MCP サーバーを使わない状態で、`host-shell-cli` スキルを読ませて次の 7 段階を行わせた: セッションの開始 (`--detach`)、ツールの一覧、`uname -a`、ファイルの書き込みと読み取り、ジョブの開始と完了の確認、`transfer_push` とサイズの確認、セッションの停止と一覧での確認。結果は、接続先のファイルとエージェントの報告で確認した。

承認を一律に省略するオプション (`--dangerously-skip-permissions` など) での実行は、この環境の権限の判定で拒否されたため、各エージェントに `host-shell` (とスキルのファイルの読み取り) だけを許可する設定で実行した。

| エージェント | 版 | 許可の設定 | 結果 |
| --- | --- | --- | --- |
| Claude Code | 2.1.326 | `--strict-mcp-config` (MCP なし)、`--allowedTools "Bash(<host-shell のパス>:*)" "Read"` | 7 段階すべて成功 |
| GitHub Copilot CLI | 1.0.112 | `--disable-builtin-mcps`、`host-shell` を `PATH` に置き `--allow-tool "shell(host-shell)"` | 7 段階すべて成功。実行ファイルのパスを直接許可する設定では、パスの承認が別に必要で失敗した。また、`sleep` を挟んだ呼び出しが許可の範囲外で止まったため、スキルに「`job_read` の `wait_sec` で待つ」「1 回の呼び出しで `host-shell` のコマンドを 1 つ」と追記して再実行した |
| Codex CLI | 0.169.0 | `codex exec -s workspace-write` (sandbox あり)、ネットワークと `HOST_SHELL_SESSION_DIR` への書き込みを許可 | 7 段階すべて成功。ただし、sandbox がコマンドの終了時に子孫のプロセスを終了させるため、`--detach` のセッションがシェルの呼び出しをまたいで残らず、Codex は全段階を 1 回の呼び出しで実行した (文書の制限に記載) |
| Grok | 1.0.77 | `--allow "Bash(<host-shell のパス>:*)"` | 7 段階すべて成功 |
| agy | 1.5.10 | なし | 未検証。非対話の実行で限定的な許可を与えるオプションがなく、承認を一律に省略するオプションは使えないため |

Table: エージェントからの操作

複数のエージェントが、最初に `host-shell call --session NAME TOOL` の順で実行して終了コード 2 (`TOOL must come first`) を受け取り、メッセージに従って順序を直した。

### CLI 経由での 3 つのスキルの実行

MCP を使わない Claude Code 2.1.326 (`--strict-mcp-config`) に、`host-shell-cli` スキルと 3 つのスキルを読ませ、各スキルの MCP ツールの手順を `host-shell call` で実行させた。許可は `host-shell`、スキルのクライアント側スクリプト、ループバックへの確認コマンドだけに限った。許可の範囲外で止まった確認は、このセッション (Claude Code、MCP なし) が `host-shell call` で同じ手順を実行して補った。セッションは `--forward-allow-local '127.0.0.1:80[0-7]?' --forward-allow-local '127.0.0.1:2222' --proxy --proxy-allow-host 'api.x.ai:443' --remote-dns` と、接続先の `--allow-forward-local --client-proxy --remote-dns --remote-dns-library` で開始した。

| スキル | 確認した内容 | 結果 |
| --- | --- | --- |
| `host-shell-code-server` | `sys_get_info` での前提の確認、`fs_stat` での空き容量、`transfer_push` (code-server 4.144.0 の 231 MB のアーカイブを含む 6 回)、`exec_run` での `install.sh` (既定の拡張機能 3 件と日本語の言語パック)、`transfer_pull` での設定の写し、`job_start` での `start.sh` と `job_read` の `until` での待ち、`forward_open`、手元の転送ポートへの HTTP 要求 (302 で `/login`、続けて 200)、`forward_close`、`job_stop`、`uninstall.sh` | 検証済。手元のポートへの HTTP 要求はエージェントの許可の範囲外で止まったため、このセッションが同じ手順で確認した |
| `host-shell-vscode-remote-ssh` | 前提の確認 (`sshd`、`ssh-keygen`、glibc 2.39)、`fetch.sh` での取得 (VS Code 1.146.0、commit 69047191fc42、x64)、`transfer_push` (252 MB)、`place.sh`、`fs_remove`、`ssh-key.sh key` と `authorize.sh` と `ssh-key.sh trust`、`job_start` での `start.sh`、`forward_open` (42222 → 2222)、出力された Host ブロックでの `ssh` 接続 (厳密なホスト鍵の確認、配置した `code-69047191fc42 --version` が 1.146.0 を返す)、`status.sh`、`cleanup.sh`、`job_stop`、`forward_close` | 検証済 (VS Code からの接続は `ssh` での接続に置き換え)。クライアント側スクリプト (`fetch.sh`、`ssh-key.sh`) はリポジトリで実行権限がなく、スキルに書かれた `"$SKILL"/scripts/client/fetch.sh` の形では実行できなかった (MCP で使う場合も同じ。残課題 BL-002)。エージェントは `sh` を付けた実行が許可の範囲外で止まったため、このセッションが `sh` を付けて実行した。`ssh-key.sh` は検証用の `HOME` で実行し、`ssh` の確認では Host ブロックの `~/.ssh` を絶対パスに置き換えた (OpenSSH は `~` を `HOME` ではなくアカウントのホーム ディレクトリから展開するため) |
| `host-shell-coding-agents` (grok 1.0.77) | `transfer_push` で専用のディレクトリへ配置 (202 MB)、接続先の SHA-256 が手元と一致 (b702950a…)、`exec_run` での `--version`、接続先の `curl https://api.x.ai/` がクライアント経由 HTTP プロキシを通ること (`proxy_status` に `api.x.ai:443` の `allowed`)、`getent hosts api.x.ai` と `sys_get_info` の `remote_dns: true`、`fs_remove` での削除 | 検証済。認証と推論は対象外。接続先のコンテナーは直接の外部通信と自前の DNS を持つため、プロキシと名前解決の中継だけで到達できることは示していない |

Table: CLI 経由での 3 つのスキルの実行

エージェントが挙げたスキルの不明点 (code-server の手順 1.4 の「制限があれば利用者の承認」に伏せ字だけの制限が含まれるか、手順 5 の `<target name>` の定義) は、CLI での利用とは関係のない既存の記述の問題として残課題 BL-003 に記録した。

## Windows の実機

Windows 側のセッション `host-shell-mcp-5b` に依頼した。

- 環境: Windows 11 Pro for Workstations 10.0.26300.9974、Intel Xeon Gold 6254 (2 ソケット、36 論理 CPU)、PowerShell 7.7.0 (pwsh)。Windows 機に Go がないため、WSL でクロスビルドした `host-shell.exe` (f866bc8、go1.27.4) を `%TEMP%` にコピーして使用 (SHA-256 は一致)。
- 接続先: 同じ機械の `host-shell.exe --mcp --no-log` を COMMAND とした (ssh は未使用)。

| 項目 | 結果 |
| --- | --- |
| `session start --detach --name w` が状態の JSON (`state: ready`、`mode: detached`) を出力して終了コード 0 で戻る。別のシェルの呼び出しから `call sys_get_info --session w` が成功し、セッションのプロセスが生存している | 検証済 |
| `tools --session w`、`session status`、`list`、`call exec_run 'command=Get-ChildItem C:\'`、`call fs_write content=@file` (`fs_read` で内容が一致)、`--json -` での `fs_stat` が終了コード 0。存在しないファイルの `fs_read` が 1。`session stop` が 0、停止後の `call` が 3 (`no session "w" is running`)。停止後のディレクトリには `w.log` だけが残る | 検証済 |
| `%LOCALAPPDATA%\host-shell\sessions` の ACL が本人の `(OI)(CI)(F)` だけで継承なし。`w.sock`、`w.json`、`w.log` も本人の `(I)(F)` だけ | 検証済 |
| 利用者が pwsh の端末で前面起動したセッション (`--confirm fs_remove` の接続先) で、確認の表示、`y` での削除 (終了コード 0)、`n` での拒否 (`Declined.`、終了コード 1、ファイルは残る)、Ctrl+C での終了 (`session "c" stopped`) | 検証済。Enter だけでの拒否は Windows では未確認 (Linux の疑似端末で確認済み) |
| `--finish-on-eof` の有無 (3 行をファイルから標準入力へリダイレクト) | 検証済 (ありで応答 2 行・終了コード 0、なしで応答なし・終了コード 1 と未応答の件数) |

Table: Windows の実機

補足: PowerShell からテスト バイナリを実行する場合、`-test.count=1` のようなフラグは引用符で囲む必要がある (囲まないと `flag provided but not defined: -test`)。

## 未検証

- macOS の実機 (自動テストは CI で成功)。
- agy からの操作。
- Windows の実機での ssh 経由の接続先 (同じ機械の `host-shell.exe --mcp` を接続先として確認)。Windows のエージェント (Windows 側の Claude Code) が自身のシェルから `--detach` で開始したセッションが、別のシェルの呼び出しでも使えることは確認済み。
