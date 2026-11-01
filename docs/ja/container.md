# コンテナーの中への接続

host-shell は、SSH の接続先だけでなく、稼働中の Linux コンテナー (podman、docker、Kubernetes の pod) の中でも動きます。bridge は `--` の後に書いたコマンドを、標準入出力の通り道として使うだけです。そのため、ssh の代わりに `podman exec -i` などを書けば、コンテナーの中の host-shell に接続できます。

```mermaid
flowchart LR
    A[エージェント] -->|stdio| B[host-shell bridge]
    B -->|"podman exec -i (stdio)"| C[コンテナーの中の host-shell]
    B -->|"ssh -T hop1 podman exec -i (stdio)"| C
```

この文書は、コンテナーに固有の配置、登録、注意点を説明します。SSH の接続先と共通の内容は [利用ガイド](user-guide.md) を参照してください。

## 対象と確認状況

| 実行環境 | 確認状況 |
| --- | --- |
| podman | 確認済み (podman 4.9.4、rootless) |
| docker | podman の互換コマンド (`docker`) で確認済み。Docker Engine では未確認 |
| kubectl (Kubernetes) | 未確認 (組み立てるコマンドだけを確認) |

Table: 対象の実行環境

- 対象は Linux のコンテナーです。Windows コンテナーは対象外です。
- host-shell は静的リンクのため、glibc のイメージでも musl のイメージ (Alpine など) でも動きます。
- 配置と確認のスクリプト (`.agents/skills/host-shell-connect/scripts/`) は、コンテナーの中に `sh` と、`uname`、`id`、`dirname`、`ls`、`sha256sum`、`mkdir`、`chmod`、`mv`、`cat`、`rm` を必要とします。シェルのないコンテナーは [シェルのないコンテナー](#シェルのないコンテナー) を参照してください。

## 経路の指定

スキル host-shell-connect のスクリプトは、経路 `ROUTE` (SSH の踏み台と接続先をカンマで並べたもの) の最後に、コンテナーを 1 つ置けます。

| 最後の要素 | 実行するコマンド | 追加の指定 (環境変数) |
| --- | --- | --- |
| `podman:<コンテナー>` | `podman exec -i ... <コンテナー> sh -c <コマンド>` | `HOST_SHELL_PODMAN_OPTS` (例: `--user dev`) |
| `docker:<コンテナー>` | `docker exec -i ... <コンテナー> sh -c <コマンド>` | `HOST_SHELL_DOCKER_OPTS` (例: `--user dev`) |
| `kubectl:<pod>` | `kubectl exec -i ... <pod> -- sh -c <コマンド>` | `HOST_SHELL_KUBECTL_OPTS` (例: `-n app -c web --context prod`) |

Table: コンテナーの要素

- `podman:web` は手元のコンテナー、`hop1,podman:web` は hop1 の上のコンテナーです。hop1 までは SSH で接続し、hop1 で `podman exec -i` を実行します。
- コンテナーの要素は、経路の最後にだけ置けます。
- 環境変数の値は、`HOST_SHELL_SSH_OPTS` と同じく空白で区切られます。

```sh
scripts/detect.sh podman:web
scripts/deploy.sh podman:web linux-amd64
scripts/probe.sh podman:web /tmp/host-shell-0/host-shell
HOST_SHELL_KUBECTL_OPTS='-n app -c web' scripts/deploy.sh kubectl:web-0 linux-amd64
```

## 配置

`deploy.sh` は、SSH の接続先と同じく、`/tmp/host-shell-<uid>/host-shell`、`.local/bin/host-shell` (ホームからの相対パス) の順に配置を試みます。コンテナーの既定の利用者が root の場合は `/tmp/host-shell-0/host-shell` です。

- ルート ファイル システムが読み取り専用の場合や、`/tmp` が `noexec` の場合は、書き込めて実行もできるボリュームのパスを 3 番目の引数で指定します。
- コンテナーを作り直すと、ボリュームに置いていないファイルは消えます (`podman restart` などの再起動では消えません)。作り直した後は、もう一度配置します。

## 登録コマンド

bridge の `--` の後に、`exec -i` でコンテナーの中の host-shell を直接起動するコマンドを書きます。経路の追加の指定 (利用者、名前空間など) も同じように含めます。

```sh
claude mcp add host-shell-web -- ~/.local/bin/host-shell bridge -- podman exec -i web /tmp/host-shell-0/host-shell --mcp
claude mcp add host-shell-web -- ~/.local/bin/host-shell bridge -- docker exec -i --user dev web /tmp/host-shell-1000/host-shell --mcp
claude mcp add host-shell-web -- ~/.local/bin/host-shell bridge -- kubectl exec -i -n app -c web web-0 -- /tmp/host-shell-1000/host-shell --mcp
claude mcp add host-shell-web -- ~/.local/bin/host-shell bridge -- ssh -T -o BatchMode=yes hop1 podman exec -i web /tmp/host-shell-0/host-shell --mcp
```

> [!IMPORTANT]
> `exec` には `-i` が必須です。`-t` を付けてはいけません。端末を割り当てると、端末の制御で MCP のやり取り (JSON-RPC) が壊れます。

制限のオプション (`--read-only`、`--allow-path` など) は、SSH の接続先と同じく `--mcp` の後に付けます。

## 注意点

- **実行する利用者**: host-shell は `exec` の既定の利用者 (イメージの `USER`、多くは root) で動きます。権限を下げる場合は `--user` (kubectl はコンテナーの `securityContext`) で指定します。
- **プロセスの寿命**: MCP セッションが終わると、host-shell と、job_start で起動したプロセスは終わります。コンテナーの中には残りません。
- **pod のネットワーク**: pod の中のコンテナーはネットワークを共有します。`forward_open` で `127.0.0.1:<port>` へ転送すると、同じ pod の他のコンテナーのサービスにも届きます (podman の pod で確認済み)。PID の名前空間は共有しないため、他のコンテナーのプロセスは `proc_list` に現れません。
- **操作ログ**: 既定の操作ログはコンテナーの中 (`~/.local/state/host-shell/`) に書かれ、コンテナーを作り直すと消えます。残す場合は `--log-file` でボリュームのパスを指定します。
- **権限の範囲**: `podman exec` などを実行できる利用者は、もともとコンテナーの中で任意のコマンドを実行できます。host-shell は新たな権限を与えません。

## シェルのないコンテナー

distroless のイメージなど、`sh` のないコンテナーでは、配置と確認のスクリプトは使えません。次のように手で配置して登録します。

```sh
podman cp .agents/skills/host-shell-connect/bin/host-shell-linux-amd64 web:/tmp/host-shell
podman exec web /tmp/host-shell --version
claude mcp add host-shell-web -- ~/.local/bin/host-shell bridge -- podman exec -i web /tmp/host-shell --mcp
```

- docker では `docker cp` を使います。`kubectl cp` はコンテナーの中に `tar` を必要とするため、kubectl では配置できません。
- ファイル操作や検索のツールは使えますが、`exec_run` でシェルのコマンドは使えません。code-server も使えません。

## code-server を使う

コンテナーの中にも、スキル host-shell-code-server で [code-server を注入](code-server.md) できます。手順は SSH の接続先と同じです。コンテナーに固有の条件は次のとおりです。

| 条件 | 内容 |
| --- | --- |
| glibc | code-server の node は glibc を必要とします。Alpine など musl のイメージでは使えません (install が展開の前に止まります) |
| コマンド | `bash`、`tar`、`gzip`、`sha256sum`、`od` が必要です。Alpine には標準で `bash` もありません |
| HOME | 設定、ユーザー データ、拡張機能は HOME の下に置きます。HOME が設定され、書き込めるディレクトリーである必要があります |
| 残す場合 | コンテナーを作り直しても残す場合は、HOME をボリュームにします |

Table: コンテナーで code-server を使う条件

| イメージ | 結果 |
| --- | --- |
| Oracle Linux 9 | 導入、起動、転送、ログイン、既定の設定と拡張機能の反映を確認 |
| debian:stable-slim | 必要なコマンドがそろっており、同じく確認 |
| alpine:3.22 | `bash` がないため install を起動できません。`bash` を入れても glibc がないため、展開の前に止まります |

Table: 確認したイメージ

code-server は、コンテナーの中の `127.0.0.1` の 8000〜8079 のうち、最初の空きで待ち受けます。コンテナーごとにネットワークが分かれるため、通常は 8000 です。
