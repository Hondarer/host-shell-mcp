# NRPT 規則の揮発化の設計 (2026-11-05)

> [!IMPORTANT]
> 作業時点の記録です。現在の仕様と検証済みの範囲は [接続元を使う名前解決 (remote-dns)](../../ja/remote-dns.md) の「Windows の接続先」を参照してください。前提の設計は [Windows の名前解決設定の自動化の設計](261105-windows-remote-dns-configure-design.md) にあります。

## 背景

`--remote-dns-configure` の最初の実装は、`Add-DnsClientNrptRule` で NRPT 規則を追加していた。この規則はレジストリーに永続し、接続先の `host-shell` が強制終了されると、再起動しても残る。host-shell はこれまで、システム設定とユーザー設定に永続する変更を加えずに目的を達成してきた。この方針に合わせるため、規則をセッションに限定して揮発させる方法を調べた。

## 調べたこと

| 確認 | 結果 |
|---|---|
| `Add-DnsClientNrptRule` が書く値 | `HKLM\SYSTEM\CurrentControlSet\Services\Dnscache\Parameters\DnsPolicyConfig\{GUID}` に `Version` (DWORD 2)、`ConfigOptions` (DWORD 8)、`GenericDNSServers`、`IPSECCARestriction`、`DisplayName`、`Comment`、`Name` (MULTI_SZ `.`) |
| 同じ値を揮発性キー (`REG_OPTION_VOLATILE`) として書く | `Get-DnsClientNrptRule` に表示され、DNS Client は書いた直後から従う (.NET の名前解決が成功)。キーを消すと直後から元に戻る |
| 揮発性キーの規則を置いたまま再起動 | キーは消える |
| `DnsPolicyConfig` キーがない状態 | 親も揮発性キーとして作れ、DNS Client は従う |
| sshd がコマンドを入れるジョブ オブジェクト | `KILL_ON_JOB_CLOSE` と `BREAKAWAY_OK` が立っている |

NRPT には、プロセスやログオン セッションに結び付く保存先はない。レジストリー キーにも、ハンドルを閉じると消える仕組みはない。揮発性キーで実現できるのは「ディスクに書かず、遅くとも再起動で消える」までである。

## 判断

- NRPT 規則を、Go から `RegCreateKeyExW` (`REG_OPTION_VOLATILE`) で揮発性キーとして作る。値は `Add-DnsClientNrptRule` と同じにする。`DnsPolicyConfig` キーがなければ、それも揮発性で作り、終了時に空なら消す。
- ルートは前回と同じく ActiveStore に置く。これで、永続するレジストリーと永続するルートの保存先は変更しない。
- 起動時の片付けは、規則のコメントで判断する点は前回と同じで、レジストリーを直接列挙して行う。空の揮発性 `DnsPolicyConfig` キーも片付ける。揮発性かどうかは `NtQueryKey` の `KeyFlagsInformation` で判定する。
- DNS Client のキャッシュの消去には `DnsFlushResolverCache` を呼ぶ。PowerShell はルートの操作にだけ使う。
- 手動手順も `Add-DnsClientNrptRule` をやめ、.NET の `RegistryOptions.Volatile` で同じキーを作る形にした。`DnsPolicyConfig` を作った場合は目印の値を置き、削除の手順では、目印があって空のときだけ消す。
- 強制終了への対策として、host-shell の終了を待って規則を消す監視プロセスを、ジョブから抜けさせて (`CREATE_BREAKAWAY_FROM_JOB`) 起動する案も検討した。sshd のジョブは抜けることを許しているので実現はできる。ただし、host-shell より長く生きるプロセスを残さない方針と衝突するため、今回は採らなかった。

揮発性の親キーの下には、永続するキーを作れない。host-shell が `DnsPolicyConfig` を揮発性で作っている間は、管理者が `Add-DnsClientNrptRule` で規則を追加できない。この制約は文書に書いた。

## 検証

test-bed で、bridge 経由で次を確認した。

| 確認 | 結果 |
|---|---|
| 永続する `DnsPolicyConfig` があり、手動手順の永続する規則と動いていない PID の揮発性の規則が残っている | 両方を片付け、揮発性の規則とループバックのルートを追加。.NET、Node.js `dns.lookup`、`curl.exe`、`ping` の名前解決が成功。終了後は親キーが永続のまま残り、規則とルートは消える |
| `DnsPolicyConfig` がない | 揮発性の親キーと規則を作り、名前解決が成功。終了後は親キーごと消える |
| 接続先の host-shell を強制終了 | 揮発性の親キー、規則、ルートが残る。次の起動で片付けてから設定し直し、終了時に消える |
| 強制終了したまま再起動 | 親キー、規則、ルートがすべて消える |
| 接続元の bridge と ssh を強制終了 | 3 秒以内に規則とルートが消える |
| 文書の手動手順 (親キーがない場合とある場合) | 記載のとおりに実行して追加と削除ができ、親キーがなかった場合は作ったキーも消える |

確認用に、キーの揮発性は「永続する子キーを作れるか」で判定した。この判定は検証用のスクリプトだけで行い、実装は `NtQueryKey` を使う。

## 残課題

- 監視プロセスによる強制終了時の即時の片付け (方針の判断が必要)
- 昇格していないセッションでのエラーの確認
- Windows Server と arm64 での確認
