# 画面内 Copilot Chat と remote-dns の検証記録 (2026-11-03)

> [!IMPORTANT]
> 作業時点の記録です。現在の仕様と検証済みの範囲は [remote-dns](../../ja/remote-dns.md) を参照してください。

[remote-dns の検証記録](261102-remote-dns-validation.md) で未実施だった、画面内 Copilot Chat の内蔵 Web Fetch と、Copilot SDK 内部からの名前解決を確認しました。

## 環境

- 接続先: Oracle Linux 10.2 / x86_64 / glibc 2.39 の Podman コンテナー `hs-cs-ol10` (`--network none`、ループバックのみ)、利用者 `dev`
- host-shell: v20261102.1.0-9-gfa1539d。`deploy.sh` で本体と CI ビルドのアダプターを `/tmp/host-shell-1000/` に配置
- 接続: bridge から `podman exec -i` で接続先 `host-shell --mcp --remote-dns --remote-dns-library ... --client-proxy --allow-forward-local` を起動
- プロキシの許可先: `github.com:443`、`api.github.com:443`、`*.githubcopilot.com:443`、`copilot-proxy.githubusercontent.com:443`、`main.vscode-cdn.net:443`
- code-server 4.144.0 を専用コピーモード (`HOST_SHELL_CODE_SERVER_REMOTE_DNS=1`) と `HOST_SHELL_DNS_TRACE=1` で起動し、`forward_open` で手元へ転送
- 画面: WSLg 上の Chrome for Testing 148 (CDP 付き)。ログインと GitHub 認証はユーザーが同じウィンドウで行い、エージェントは `playwright-core` の `connectOverCDP` で操作した

## 画面表示の復旧

Chrome のウィンドウが Windows 側に表示されなかった。X サーバーは応答したが、Chrome 以外の最小の X11 ウィンドウも画面外座標 (`-32730,-32709`) のままで、WSLg 用 `msrdc.exe` に表示中のウィンドウがなかった。Windows にはリモートデスクトップ (mstsc) で接続しており、同日 02:16 と 05:35 に切断・再接続の記録があった。`taskkill /F` で WSLg 用 `msrdc.exe` を強制終了すると数秒で再起動し、Chrome のウィンドウが表示された (`/F` なしの終了要求は無視された)。手順は [開発ガイド](../../ja/development.md#wslg-のウィンドウが表示されない場合) に記載した。

表示の確認でデスクトップ全体を撮影した際、手前に開いていた code-server の `config.yaml` が写り込んだ。画像は直ちに削除し、以後はウィンドウ一覧と CDP のページ単位のスクリーンショットだけで確認した。

## サインイン

ユーザーがデバイスコード方式で GitHub 認証を完了し、Copilot Chat のログに `Got Copilot token` が記録された。GitHub Authentication のログにあった `Unable to resolve optional EDU details. Status: 403 Forbidden` は、プロキシ履歴では `education.github.com:443` の宛先拒否であり、任意情報の取得失敗だった。Copilot Chat のログには `Failed to create database. Falling back to in-memory db` も記録されたが、DNS とは関係しないため調査していない。

## 結果

| 確認 | 結果 |
|---|---|
| アダプターの読み込み | 親プロセス、`entry`、`fileWatcher`、エージェントホスト、拡張ホスト、JSON 言語サーバー、`copilot-runtime` (エージェントホストの子) のすべてで `/proc/<pid>/maps` と `LD_PRELOAD` を確認 |
| Local ハーネスのツール | `web` グループは `githubRepo` と `githubTextSearch` のみで、Web ページ取得ツールはなかった |
| Copilot ハーネスの `web_fetch` | `https://api.github.com/zen` の取得に成功 (「Design for failure.」)。初回は「Fetch URL?」の承認が必要だった |
| DNS トレース | 取得のたびに code-server の出力へ `[CLI subprocess] host-shell-dns: getaddrinfo api.github.com family=0` と、エージェントホストの中継分の同じ行が 1 組増えた |
| プロキシ履歴 | 取得のたびに `api.github.com:443` の allowed が 1 件記録された |
| 再現確認 | `https://api.github.com/octocat` の取得前後で出力の差分を取り、トレース 1 組とプロキシの allowed 1 件だけが増えることを確認した |

サインインから取得までの間、拡張ホスト側の DNS トレースは記録されなかった。拡張ホストの通信は HTTP プロキシ経由であり、接続先で名前解決していないと考えられる。

`web_fetch` は宛先のホスト名を接続先で名前解決したうえで、HTTPS 自体はプロキシ経由で行っている。単体の Copilot CLI で remote-dns なしのときに `WebFetchBlockedUrlError: failed to lookup address information` で失敗したのは、この事前の名前解決によるものと整合する。

## 残った未確認事項

- Copilot SDK の `web_fetch` 以外の内部処理による名前解決
- 拡張ホスト上の拡張機能が直接行う名前解決
