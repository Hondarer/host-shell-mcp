# Windows 向け remote-dns の設計 (2026-11-04)

> [!IMPORTANT]
> 作業時点の記録です。現在の仕様と検証済みの範囲は [接続元を使う名前解決 (remote-dns)](../../ja/remote-dns.md) を参照してください。検証環境は [Windows 接続先の検証記録](261104-windows-target-validation.md) と同じです。

## 背景

remote-dns は、Linux/glibc の接続先で `LD_PRELOAD` によって `getaddrinfo` を差し替え、セッション専用の Unix ソケット経由で接続元へ名前解決を中継する。Windows の接続先では `--remote-dns` が `--remote-dns target requires Linux/glibc` で起動できなかった。DNS サーバーもインターネットへの経路もない Windows の接続先で、接続先のプログラムが接続元の名前解決を使えるようにすることが目的である。

## Windows の名前解決の経路

| 経路 | 使うプログラムの例 |
|---|---|
| `getaddrinfo` / `GetAddrInfoW` / `GetAddrInfoExW` (ws2_32) から DNS Client サービス | .NET の `Dns`、Node.js の `dns.lookup` (libuv)、`curl.exe`、`ping`、Go の `net` (Windows では cgo なしで `GetAddrInfoW` を呼ぶ) |
| DNS サーバーへ自分で問い合わせる | Node.js の `dns.resolve*` (c-ares)、`nslookup` |

Windows には環境変数で読み込ませる仕組み (`LD_PRELOAD`) がなく、プロセスごとに名前解決を差し替える標準の方法もない。

## 候補

| 案 | 仕組み | 長所 | 短所 |
|---|---|---|---|
| A. DLL 注入 | 起動するプロセスに DLL を読み込ませ、`GetAddrInfoW` などと `CreateProcessW` をフックする | 利用者権限・セッション単位で閉じる。Linux 版と同じ性質 | C の DLL と注入処理が必要。32 ビット・arm64 の DLL も要る。ウイルス対策製品に検知されやすい。Go の `LoadDLL` 経由の呼び出しなど、フック対象が多い |
| B. ループバックの DNS サーバーと NRPT | host-shell が `127.0.0.1:53` で DNS サーバーを動かし、NRPT 規則 `.` でそこへ向ける | Go だけで実装できる。OS 全体の経路なので、子孫プロセスやサービスも対象になる | NRPT の追加に管理者権限が要る。マシン全体と全ユーザーに影響する |
| C. ネットワーク アダプターの DNS サーバーを 127.0.0.1 にする | B と同じ DNS サーバーを、アダプターの設定で使う | `nslookup` や c-ares も使う | 管理者権限が要る。既存の設定を書き換える必要がある |
| D. hosts ファイル | 名前を静的に書く | 単純 | 管理者権限が要る。事前に名前を決める必要がある |
| E. Winsock の名前空間プロバイダー | DLL を登録し `getaddrinfo` に組み込む | 公式の拡張点 | 管理者権限が要る。DLL が要る。マシン全体に影響する |

## 判断

案 B を採った。理由は次のとおり。

- Go だけで書け、既存の DNS 専用チャネル (`KindDNS`) と接続元のリゾルバーをそのまま使える。
- OS 全体の経路なので、VS Code の拡張ホストのように環境変数を消して子プロセスを起動する場合にも影響を受けない。Linux 版で必要になった code-server の専用コピーモードに当たる対応が要らない見込みである (未検証)。
- 案 A は実装・配布・検知の負担が大きく、検証できる範囲を超える。

OS の設定変更 (NRPT 規則の追加と削除) は host-shell に組み込まず、管理者が行う手順として文書に書いた。host-shell が自動で規則を追加すると、異常終了で規則が残った場合に、利用者の知らないところでマシン全体の名前解決が壊れるためである。

## 実装

- 接続先のオプション `--remote-dns-listen ADDR` を追加した。ループバックの `IP:ポート` だけを受け付け、UDP と TCP で待ち受ける。`--remote-dns` が必要で、Linux 以外では必須とした (省略すると起動時にエラー)。Linux では従来の `LD_PRELOAD` と併用できる。
- `remotedns.NewTarget` の引数を `TargetOptions{Library, Listen}` に変え、Unix ソケット側と DNS サーバー側が共通の `ask` (チャネル経由の問い合わせ) を使うようにした。
- DNS メッセージの解析と生成に `golang.org/x/net/dns/dnsmessage` (v0.61.0) を加えた。
- 回答の規則:
  - A と AAAA に答え、canonical name が異なれば CNAME を付ける。TTL は 30 秒。
  - その他の種別には NODATA を返す (HTTPS・SVCB の問い合わせで待たせないため)。
  - 片方のファミリーで見つからなかった場合は、ファミリーを問わず問い合わせ直し、それでも見つからない場合だけ NXDOMAIN にする。IPv6 だけの名前に A の NXDOMAIN を返すと、OS がその名前全体を「存在しない」とキャッシュするためである。
  - 接続元との接続前は SERVFAIL、名前として不正なものは REFUSED。
  - UDP は EDNS の大きさ (上限 4096) を尊重し、超える場合は TC ビットを立てる。
- `sys_get_info` に `remote_dns_listen`、ヘルプ (`windows` トピック) に手順を加えた。`HOST_SHELL_DNS_TRACE` を host-shell に設定すると、問い合わせと応答の要約を標準エラーへ記録する。

## 検証で分かったこと

- NRPT 規則 `.` → `127.0.0.1` で、DNS Client サービスはループバックの DNS サーバーに問い合わせる。`Resolve-DnsName` は規則だけで成功した。
- `getaddrinfo` は、回答を受け取ってキャッシュにも入っているのに WSANO_DATA を返した。ゲストにデフォルト ルート (次ホップはホスト。ホストは転送しない) を加えると、.NET、Node.js `dns.lookup`、`curl.exe`、`ping`、`net_check` が成功した。ルートを消すと再び WSANO_DATA になった。Windows の `getaddrinfo` は IPv4 のデフォルト ルートがないと IPv4 の結果を返さないと判断した。ルートを加えている間も、外部への TCP 接続は失敗した。
- Node.js の `dns.resolve4` (c-ares) は、NRPT 規則がなくても成功した。アダプターに DNS サーバーがないとき、c-ares が 127.0.0.1 を既定の問い合わせ先にするためと考えられる。
- 同じアドレスでの 2 つ目のセッションは bind エラーで起動時に停止した。
- 規則が残った状態で host-shell を止めると、名前解決は待たずにすぐ失敗した (`Resolve-DnsName` は接続のリセット、.NET は `No such host is known`)。
- 自動テストは Windows ホストの `go test ./...` と、WSL (Oracle Linux 8、glibc 2.28) での `internal/remotedns` のテスト (既存の `LD_PRELOAD` 経路と Node.js の再注入を含む) で成功した。

## 残課題

- NRPT 規則の追加と削除を、利用者が明示的に選んだ場合に限って host-shell が行う機能 (異常終了時の残留への対処を含む)
- code-server の拡張ホスト、Copilot CLI・Copilot Chat の Windows 接続先での確認
- `nslookup` と NRPT 規則の関係、Windows Server と arm64 での確認
- デフォルト ルートを持たない閉じたネットワークで `getaddrinfo` を成功させる方法
