# BYOK の HTTPS とクライアント経由プロキシの検証記録 (2026-11-05)

> [!IMPORTANT]
> 作業時点の記録です。現在の手順と検証済みの範囲は [CLI 型 AI コーディングエージェント](../../ja/coding-agents.md) の「BYOK」を参照してください。HTTP とリモート転送での検証は [BYOK の検証記録](261105-copilot-byok-validation.md) と [WSL での BYOK の検証記録](261105-wsl-byok-validation.md) にあります。

## 背景

これまでの BYOK の検証は、平文 HTTP のダミーのエンドポイントにリモート転送で到達させていた。実際の Azure OpenAI は HTTPS で、外部のエンドポイントにはクライアント経由 HTTP プロキシを使う手順になっている。新しいダウンロードなしで test-bed だけで確かめられる範囲として、ダミーを HTTPS にしてプロキシ経由で到達させた。

## 準備

- Git for Windows の openssl で、検証用の認証局 (EC P-256、有効期間 7 日) と、`localhost` と `127.0.0.1` を SAN に持つサーバー証明書を作った。鍵と証明書は作業用の一時ディレクトリーに置いた。
- ダミーのエンドポイント (`dummy_aoai.py`) に、証明書と鍵を渡すと HTTPS で待ち受ける引数を加え、ホストの `127.0.0.1:18444` で動かした。
- `bridge` は `--proxy --proxy-allow-host localhost:18444`、接続先は `--client-proxy` で起動した。リモート転送は使っていない。認証局の証明書は `transfer_push` で接続先に置いた。
- 接続先: Windows は test-bed の Windows (接続ユーザー `tester`)、Linux は同じ仮想マシンの WSL 2 (Ubuntu 24.04) で、WSL の sshd と ProxyJump で接続した。

## Copilot CLI 1.0.105

`COPILOT_OFFLINE=true` で `-p` を実行した。

| 条件 | Windows 版 | Linux 版 |
|---|---|---|
| `NODE_EXTRA_CA_CERTS` なし | 5 回の再試行の後に失敗。`Failed native model HTTP request ... client error (Connect)` と、Windows の信頼されていないルート証明書のエラー (os error -2146762487) | 同様に失敗。OpenSSL の `certificate verify failed (unable to get local issuer certificate)` |
| `NODE_EXTRA_CA_CERTS` あり、Chat Completions | ダミーの文が返る (約 1 秒) | 同左 |
| 同、ツール呼び出し | `create` によるファイル作成と `powershell` の実行が接続先で行われた | `create` と `bash` の実行が行われた |
| 同、Responses API (`wireApi: responses`) | ダミーの文が返る | 同左 |

- エラーの文言から、CLI の推論の通信は Node.js の `fetch` ではなく独自の実装 (native) で行われているが、`NODE_EXTRA_CA_CERTS` は効いた。
- `proxy_status` には `localhost:18444` の `allowed` だけが記録された。接続先の `localhost:18444` では何も待ち受けていないため、プロキシを経由したことになる。
- Linux の `exec_run` では `HTTPS_PROXY` が設定され、`NO_PROXY` は空だった。

## 画面内の Copilot Chat (code-server 4.144.0、GitHub Copilot Chat 0.72.0)

### Linux (WSL)

WSL に残っていた code-server と、Azure の提供元で登録済みのモデル (キーは秘密情報として保存済み) を使った。`chatLanguageModels.json` の `url` だけを書き換え、`job_start` の `env` に `NODE_EXTRA_CA_CERTS` などを渡して `start.sh` を起動した。

1. `url` を `https://localhost:18444/...` にすると、`TypeError: fetch failed` (`connect ECONNREFUSED 127.0.0.1:18444`) で失敗した。`proxy_status` には `localhost` の記録がなく、拡張機能の通信がプロキシを通らず接続先の `localhost` に直接向かった。一方で、`main.vscode-cdn.net:443` や `copilot-telemetry.githubusercontent.com:443` などは `denied` として記録されており、拡張機能ホストはプロキシを使っている。`localhost` だけが直接接続の対象になったと判断した。
2. 手元で `localhost.` (末尾にドット) が `127.0.0.1` に解決されることを確かめ、`url` を `https://localhost.:18444/...`、許可先に `localhost.:18444` を加えた。今度はプロキシを経由したが、`unable to verify the first certificate; if the root CA is installed locally, try running Node.js with --use-system-ca` で失敗した。code-server の各 node プロセスの環境変数には `NODE_EXTRA_CA_CERTS` があったため、拡張機能の通信が独自の認証局の一覧を使っていると判断した。
3. `/etc/ssl/certs/ca-certificates.crt` と検証用の認証局をつないだファイル (147 個の証明書) を利用者の領域に作り、`SSL_CERT_FILE` に指定して起動し直すと、チャットにダミーの文が表示された。`TOOLTEST` ではツールが 2 段階実行され、結果を含む文が返った。ダミーの記録では、送り手は `GitHubCopilotChat/0.72.0`、キーは `Authorization` ヘッダーで一致、`Host` は `localhost.:18444` だった。

終了時に `chatLanguageModels.json` は元の内容に戻した。

### Windows

Windows 側の code-server は前回のチェックポイントの復元で消えていたため、スキルの手順で入れ直した。

1. `chatLanguageModels.json` の `apiKey` に `dummy-key` を直接書いて起動すると、要求が「作業中」のまま進まず、「拡張機能 'GitHub Copilot' が Microsoft を使用してサインインしようとしています」という確認が出た。キャンセルした。直接書いた値はキーとして扱われず、Entra ID の認証に切り替わると判断した。
2. 画面の「モデルの追加」から Azure を選び、キーを入力して登録し直した (ファイルには `${input:chat.lm.secret...}` の参照が書かれる)。`id`、`name`、`url` (`https://localhost.:18444/...`) を書いてページを読み込み直した。
3. `NODE_EXTRA_CA_CERTS` だけでは、`unable to verify the first certificate` で失敗した。
4. `SSL_CERT_FILE` に検証用の認証局を指定しても、同じエラーで失敗した。

Windows の証明書ストアへの登録は、接続先に残る設定のため試していない。

## 後片付け

- ダミーの HTTPS のエンドポイントは停止した。
- Windows 側には code-server (入れ直したもの) と、`dummy-gpt` を登録した `chatLanguageModels.json` が残る。WSL 側の `chatLanguageModels.json` は元に戻した。
- 認証局の証明書と束のファイルは、接続先の Copilot CLI の配置先 (`host-shell-coding-agents/copilot/1.0.105/`) に残る。OS の証明書ストアは変更していない。

## 未検証

- 実際の Azure OpenAI (公開の認証局の証明書) との通信
- Windows の画面内の Copilot Chat で、OS の証明書ストアに認証局を登録した場合
