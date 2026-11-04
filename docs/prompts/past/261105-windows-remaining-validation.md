# Windows 接続先の残りの自動検証の記録 (2026-11-05)

> [!IMPORTANT]
> 作業時点の記録です。現在の仕様と検証済みの範囲は [接続元を使う名前解決](../../ja/remote-dns.md)、[code-server の導入](../../ja/code-server.md)、スキル host-shell-connect の SKILL.md を参照してください。

## 目的

文書で未検証のまま残っていた Windows 接続先の項目のうち、追加の資材なしで test-bed とホストだけで自動化できるものを確かめた。test-bed はチェックポイント `closed-ssh` (接続ユーザーは管理者) から始めた。

## 結果

### `nslookup` と NRPT 規則

`--remote-dns-configure` で揮発性の NRPT 規則を入れた状態で確認した。

| コマンド | 結果 |
|---|---|
| `nslookup api.github.com` | `Server: UnKnown`、`Address: fec0:0:0:ffff::1` に問い合わせ、`No response from server` で失敗 |
| `nslookup api.github.com 127.0.0.1` | 成功 (20.27.177.116) |
| `Resolve-DnsName api.github.com` | 成功 |

`nslookup` は NRPT 規則を使わず、アダプターの DNS サーバー (この環境では未設定) に問い合わせる。

### code-server の子プロセスの名前解決

`HOST_SHELL_DNS_TRACE=1` を付けた host-shell で `--remote-dns-configure` と code-server を併用し、ブラウザーから操作した。

- 統合ターミナルから、code-server 同梱の node.exe で `dns.lookup('registry.npmjs.org')`、.NET で `example.org` を引き、どちらもアドレスが返った。問い合わせの記録にも両方が残った。
- 拡張機能ビューの検索は、ブラウザー (ホスト) が直接ギャラリーに問い合わせており、接続先からの問い合わせはなかった。
- 拡張機能のインストールは、発行元を信頼するかの確認が出た。依頼にない第三者の拡張機能の導入になるため、キャンセルした。拡張ホストからの名前解決は未検証のまま残した。
- 問い合わせの記録には、操作していない名前も多数あった (`wdcp.microsoft.com`、`wdcpalt.microsoft.com`、`ctldl.windowsupdate.com`、`self.events.data.microsoft.com`、`mobile.events.data.microsoft.com`、`config.edge.skype.com`、`officeclient.microsoft.com`、`www.msftncsi.com` など)。NRPT 規則 `.` が OS 全体に効くため、Defender、Windows Update、テレメトリー、接続状態の確認 (NCSI) の名前解決も接続元へ流れる。この点を文書の注意事項と、承諾を得る際に伝える内容に加えた。
- その後、利用者が Hex Editor (既定の拡張機能として導入済み) をテストに使うことを許可した。Hex Editor 自体は通信しないため、拡張ホストの確認には使えない。代わりに、拡張機能ビューの「特定のバージョンをインストール...」で 1.10.0 を選んだ。版の一覧はブラウザーが取得し、選んだ版の取得は code-server のサーバー側が行った。問い合わせの記録に `open-vsx.org` が残り、サーバー プロセスの名前解決が接続元経由で成功したことを確かめた。ダウンロードは外へ出られないため「インストール中にエラーが発生しました」で失敗し、元の版 (1.11.1) が残った。
- 続いて、利用者が GitLens をテストに使うことを許可した。プロキシを許可したセッション (`--proxy-allow-host open-vsx.org:443`、`openvsx.eclipsecontent.org:443`) で `install.ps1 -Extension eamodio.gitlens` を実行し、GitLens 19.5.0 を導入した。プロキシの記録は許可した 2 つの宛先だけだった。次に、プロキシなしで `--remote-dns-configure` と問い合わせの記録を有効にしたセッションで code-server を開いた。GitLens が起動すると (アクティビティ バーとステータス バーの Launchpad が表示された)、問い合わせの記録に `api.gitkraken.dev`、`configs.gitkraken.dev`、`otel.gitkraken.com` が残り、どれも解決に成功した。GitLens は接続先の拡張ホスト (Node.js) で動き、ブラウザー側の問い合わせはホストの DNS を使ってこの記録には出ないため、拡張ホストの名前解決が接続元経由で成功したと判断した。test-bed に git はないが、GitLens は起動し、Commit Graph と Welcome のビューを表示した。
- ターミナルの最初の表示に十数秒かかり、その間タブが切断表示になった。待つと表示された。原因は調べていない。

### code-server の表示言語の切り替え

コマンド パレットの「表示言語を構成する」から English を選び、再起動の確認を経て英語表示になった。同じ手順で日本語に戻り、`argv.json` は `"locale": "ja"` になった。一覧で「English (Current)」と付く点は、文書にある既知の挙動と同じだった。

### sshd の既定シェルが PowerShell の場合

`HKLM\SOFTWARE\OpenSSH\DefaultShell` を Windows PowerShell にして確認した。

| スクリプト | 結果 |
|---|---|
| `remote.sh ROUTE true` | PowerShell の「認識されません」エラーが標準エラーに出る (判定には影響しない) |
| `detect.sh` | `could not detect the OS of the host` と、既定シェルが PowerShell の場合は手動で判別するよう案内して停止 |
| `deploy.sh` (既定の配置先) | `could not determine %TEMP% and %LOCALAPPDATA%` で停止。既定シェルの手がかりがなかったため、`; the default shell of sshd must be cmd` を付けた |
| `deploy.sh` (配置先を指定) | `could not install` で停止 |
| `probe.sh` | 成功 (host-shell 自体は PowerShell の既定シェルでも動く) |

SKILL.md のトラブルシューティングに、このメッセージと対処を加えた。確認後、`DefaultShell` は削除して `cmd` に戻した。

### code-server の同時接続

2 つの MCP 接続でそれぞれ `start.ps1` を実行すると、ポート 8000 と 8001 で起動し、両方の `/login` が 200 を返した。片方の `job_stop` 後も、もう片方は 200 を返した。もう片方の MCP 接続を閉じると、node.exe は残らなかった。

## 未確認の項目

- Copilot CLI・Copilot Chat (サインインが必要)
- 統合ターミナルの最初の表示が遅い原因
