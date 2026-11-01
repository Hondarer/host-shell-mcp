# host-shell documentation

[English](#english) | [日本語](#日本語)

## English

**The Japanese documents in [`ja/`](ja/) are the single source of truth (SSOT)**, and [`en/`](en/) contains English translations. If they differ, the Japanese version takes precedence.

| Document | English | Japanese (SSOT) | Contents |
|---|---|---|---|
| User Guide | [en/user-guide.md](en/user-guide.md) | [ja/user-guide.md](ja/user-guide.md) | Architecture, binary deployment, SSH route checks, agent registration, usage examples, troubleshooting |
| Operations Guide | [en/operations.md](en/operations.md) | [ja/operations.md](ja/operations.md) | Security model, restriction options, dedicated user and key, pinning the startup command, sudo configuration, audit log management |
| Reference | [en/reference.md](en/reference.md) | [ja/reference.md](ja/reference.md) | Startup options, policy restrictions, MCP specification, tool inputs and outputs, bridge, process management, audit log format, limitations |
| Development Guide | [en/development.md](en/development.md) | [ja/development.md](ja/development.md) | Building, source layout, design rules, adding tools, testing, releases |
| HTTP proxy through the client | [en/client-proxy.md](en/client-proxy.md) | [ja/client-proxy.md](ja/client-proxy.md) | Routing target HTTP/HTTPS traffic through the bridge's network |
| Port forwarding | [en/port-forward.md](en/port-forward.md) | [ja/port-forward.md](ja/port-forward.md) | Local forwards, remote forwards, and SOCKS5 over SSH stdio |
| Injecting code-server | [en/code-server.md](en/code-server.md) | [ja/code-server.md](ja/code-server.md) | Installing code-server with default settings and extensions on a closed-network target and opening it via a local forward |
| Connecting into containers | [en/container.md](en/container.md) | [ja/container.md](ja/container.md) | Deploying and registering `host-shell` inside a running Podman, Docker, or Kubernetes container via `exec -i` |

Table: Documents

When updating documentation, edit the Japanese version first and update the English version in the same change.

## 日本語

**[`ja/`](ja/) の日本語版を正本** とし、[`en/`](en/) に英訳を配置しています。日本語版と英語版で内容が一致しない場合は、日本語版が優先されます。

| 文書 | 日本語 (正本) | 英語 | 内容 |
|---|---|---|---|
| 利用ガイド | [ja/user-guide.md](ja/user-guide.md) | [en/user-guide.md](en/user-guide.md) | 仕組み、バイナリ配置、SSH 経路の確認、エージェントへの登録、利用例、トラブルシューティング |
| 運用ガイド | [ja/operations.md](ja/operations.md) | [en/operations.md](en/operations.md) | セキュリティ設計、制限オプション、専用ユーザーと鍵、起動コマンドの固定、sudo 設定、操作ログ管理 |
| リファレンス | [ja/reference.md](ja/reference.md) | [en/reference.md](en/reference.md) | 起動オプション、制限 (方針)、MCP 仕様、各ツールの入出力、bridge、プロセス管理、操作ログ形式、制限事項 |
| 開発ガイド | [ja/development.md](ja/development.md) | [en/development.md](en/development.md) | ビルド、ソース構成、設計規約、ツールの追加手順、テスト、リリース |
| クライアント経由 HTTP プロキシ | [ja/client-proxy.md](ja/client-proxy.md) | [en/client-proxy.md](en/client-proxy.md) | 接続先の HTTP/HTTPS 通信を bridge のネットワークで中継 |
| ポート転送 | [ja/port-forward.md](ja/port-forward.md) | [en/port-forward.md](en/port-forward.md) | SSH stdio 経由のローカル転送・リモート転送・SOCKS5 |
| code-server の導入 | [ja/code-server.md](ja/code-server.md) | [en/code-server.md](en/code-server.md) | 閉鎖環境の接続先への code-server・既定設定・拡張機能の導入とローカル転送での利用 |
| コンテナーへの接続 | [ja/container.md](ja/container.md) | [en/container.md](en/container.md) | 稼働中の Podman、Docker、Kubernetes コンテナー内への `exec -i` による `host-shell` の配置と登録 |

Table: 文書

文書を変更する際は正本の日本語版を変更し、同じ変更で英語版へ反映します。
