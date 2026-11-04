# WSL での BYOK の検証記録 (2026-11-05)

> [!IMPORTANT]
> 作業時点の記録です。現在の手順と検証済みの範囲は [CLI 型 AI コーディングエージェントの導入](../../ja/coding-agents.md) の「BYOK」を参照してください。Windows での検証は [Copilot CLI と Copilot Chat の BYOK の検証記録](261105-copilot-byok-validation.md) にあります。

## 目的

Windows の test-bed に WSL を入れ、その中の Linux を接続先として、Windows と同じ BYOK の検証 (Copilot CLI と画面内の Copilot Chat) を行った。

## WSL の導入

test-bed はインターネットへの経路がないため、資材は手元で取得して送った。

| 手順 | 内容 |
|---|---|
| 入れ子の仮想化 | `closed-ssh` に戻したあと、ホストで `Stop-VM`、`Set-VMProcessor -ExposeVirtualizationExtensions $true`、`Set-VMMemory -DynamicMemoryEnabled $false -StartupBytes 8GB` を実行して起動した (入れ子の仮想化では動的メモリーを使えないため固定にした) |
| Windows の機能 | `dism /online /enable-feature` で `Microsoft-Windows-Subsystem-Linux` と `VirtualMachinePlatform` を有効にし、再起動した |
| WSL 本体 | GitHub の microsoft/WSL 3.0.3 の `wsl.3.0.3.0.x64.msi` (367,575,040 バイト、公開の SHA-256 `ae977286...f211` と一致) を `transfer_push` で送り、`msiexec /i ... /qn` で導入した。`wsl --version` は WSL 3.0.3.0、カーネル 6.18.54.1-1 |
| ディストリビューション | Canonical の `ubuntu-noble-wsl-amd64-wsl.rootfs.tar.gz` (356,739,129 バイト、`SHA256SUMS` の値 `8251e27f...15ea` と一致。`SHA256SUMS` の GPG 署名は照合していない) を `wsl --import ubuntu C:\wsl\ubuntu ... --version 2` で取り込んだ |
| 既定の利用者 | イメージには利用者 `ubuntu` (UID 1000) があったため、`/etc/wsl.conf` に `[user] default=ubuntu` を書いた |

ここまでの状態を、チェックポイント `wsl-ubuntu` として保存した。

## 接続方法

接続先の Linux 版 host-shell は、`ssh tester@test-bed "wsl -d ubuntu -- /home/ubuntu/.local/bin/host-shell --mcp"` で起動した。

- ssh から `wsl.exe` へ渡した標準入力は、1,020,000 バイトが欠けずに WSL の中へ届いた。PowerShell 5.1 で見つかった、標準入力が止まる問題は起きなかった。
- 配置は、Windows 側の host-shell の `transfer_push` で `C:\wsl-setup` に送り、WSL の中で `/mnt/c/wsl-setup` から `install -D -m 755` で置いた。スキルの `remote.sh` と `deploy.sh` は WSL を経由する経路に対応していないため、使っていない。
- WSL の中の環境は Ubuntu 24.04 LTS、x86_64、glibc 2.39。

## 結果

推論先は Windows の検証と同じダミーのエンドポイント (ホストの `127.0.0.1:18443`) で、WSL の中の host-shell が開いたリモート転送 (WSL の `127.0.0.1:18443`) で到達させた。

| 対象 | 結果 |
|---|---|
| Copilot CLI 1.0.105 (`copilot-linux-x64.tar.gz`、129,108,148 バイト、SHA-256 `aba1f06f...6bd2` が手元と接続先で一致) | `--version` が `GitHub Copilot CLI 1.0.105.`。Chat Completions と Responses API のそれぞれで、`COPILOT_OFFLINE=true` の有無の両方で応答が返った。`--allow-all-tools` で `create` により `/home/ubuntu/byok-tooltest.txt` が作られ、`bash` で `cat`、`id -un`、`uname -s` を実行した結果 (`ubuntu`、`Linux`) が最終の文に入った |
| Linux 版 code-server 4.144.0 | `download.ps1 linux-amd64` で手元にキットを取得し、`install.sh --locale ja` で導入した。`?folder=/home/ubuntu` の URL で開き、Windows 版のようなフォルダーの問題はなかった |
| 画面内の Copilot Chat (GitHub Copilot Chat 0.72.0) | Azure の提供元で登録したダミーのモデルが応答し、`create_file` と `read_file` の 2 段階が完了した (`/home/ubuntu/byok-chat-tooltest.txt` を作成)。キーは `Authorization: Bearer` で届いた |

## 未確認の項目

- 実際の Azure OpenAI との通信、クライアント経由プロキシでの到達
- WSL を経由する経路への `remote.sh`・`deploy.sh` の対応
