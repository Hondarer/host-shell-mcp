# Copilot CLI と Copilot Chat の BYOK の検証記録 (2026-11-05)

> [!IMPORTANT]
> 作業時点の記録です。現在の手順と検証済みの範囲は [CLI 型 AI コーディングエージェントの導入](../../ja/coding-agents.md) の「BYOK」を参照してください。

## 目的

Copilot CLI と画面内の Copilot Chat の Windows 接続先での動作は、GitHub へのサインインが必要なため未検証だった。BYOK (利用者が用意するモデル) なら GitHub の認証が要らないため、ホストに Azure OpenAI 形式のダミーのエンドポイントを置いて、自動で確かめた。

## 調べたこと

- Copilot CLI の文書 ([Authenticating Copilot CLI](https://docs.github.com/en/copilot/how-tos/copilot-cli/set-up-copilot-cli/authenticate-copilot-cli)) は、BYOK では GitHub の認証が要らず、`COPILOT_OFFLINE=true` で GitHub のサーバーへ通信しないとしている。設定は `providers.json` (`COPILOT_PROVIDERS_CONFIG` で指定) に書く ([Using BYOK models](https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/use-byok-models))。
- VS Code の文書 ([AI language models in VS Code](https://code.visualstudio.com/docs/agent-customization/language-models)) は、BYOK のモデルによるチャットはサインインなしで使えるとしている。

## 構成

- ダミーのエンドポイント: ホストの `127.0.0.1:18443` で動く Python のサーバー。末尾が `/chat/completions` と `/responses` の要求に、ストリーミングと非ストリーミングで固定の文 (`BYOK-DUMMY-OK: ...`) を返す。キーは検証用の値 `dummy-key` を要求し、`api-key` ヘッダーと `Authorization: Bearer` の両方を受け付ける。記録にはキーの値を残さず、どのヘッダーで届いたかと一致したかだけを残す。利用者のメッセージに `TOOLTEST` を含むと、ツール呼び出し (Copilot CLI では `create` と `powershell`、Chat では `create_file` と `read_file`) を順に返し、最後にツールの結果を含めた文を返す。
- 経路: `bridge --forward-allow-remote 127.0.0.1:18443` と接続先の `--allow-forward-remote` で、接続先の `127.0.0.1:18443` から手元のダミーへのリモート転送を開いた。
- 接続先: test-bed (`closed-ssh`、接続ユーザーは管理者)。

## Copilot CLI

`gh release download` で v1.0.105 の `copilot-win32-x64.zip` (112,637,935 バイト) を取得し、GitHub が公開する SHA-256 `34e16e9c...061a` と一致することを確かめた。中身は `copilot.exe` (172,637,984 バイト、SHA-256 `ab6d710d...8624`) 1 つで、`transfer_push` で送り、接続先でも SHA-256 が一致した。

`providers.json` は `type: azure`、`baseUrl: http://127.0.0.1:18443/openai`、`azure.apiVersion: 2024-10-21`、モデルは `id: dummy-gpt`、`wireModel: dummy-gpt`、`modelId: gpt-4.1` とした。`COPILOT_HOME` は検証用の場所に分けた。

| 確認 | 結果 |
|---|---|
| `--version` | `GitHub Copilot CLI 1.0.105.` |
| `COPILOT_OFFLINE=true`、`--model dummyaz/dummy-gpt -p ...` | ダミーの文が返った。要求は `/openai/deployments/dummy-gpt/chat/completions?api-version=2024-10-21`、`api-key` ヘッダー、ストリーミング、ツール 19 件、送り手 `OpenAI/JS 5.20.1` |
| `COPILOT_OFFLINE` なし (GitHub の認証なし) | 同じく応答した (ツール 21 件) |
| `"wireApi": "responses"` | `/openai/responses?api-version=2024-10-21` へ要求し、応答した |
| `--allow-all-tools` と `TOOLTEST` | `create` で `C:\Users\tester\byok-tooltest.txt` が作られ、`powershell` で読み出した内容と `whoami` の結果 (`desktop-4uk6qvf\tester`) がダミーへ戻り、最終の文に入った。2 回目はファイルが既にあるため `create` が `Path already exists` で失敗し、その後の `powershell` は実行された |

CLI が送ったツールは、`powershell`、`read_powershell`、`stop_powershell`、`list_powershell`、`view`、`create`、`edit`、`grep`、`glob`、`task` など 19 件だった。

## 画面内の Copilot Chat

code-server 4.144.0 を `closed-ssh` に導入した状態で、ローカル転送とリモート転送を同時に開き、ホストの Chrome から操作した。

1. チャット欄のモデル選択には「GitHub Copilot」のタブ (サインインを求める表示) と「+」があった。「+」から言語モデルの画面を開き、「モデルの追加」の一覧 (GitHub Copilot、Anthropic、Azure、Google、OpenAI、OpenRouter、xAI、Ollama、Custom Endpoint) から Azure を選んだ。
2. グループ名 (`Azure`) と API キー (`dummy-key`) を入力すると、`User/chatLanguageModels.json` が開いた。キーは `${input:chat.lm.secret.-738e3fc4}` の参照として書かれた。
3. `models` の `id`、`name`、`url` (`http://127.0.0.1:18443/openai/deployments/dummy-gpt/chat/completions?api-version=2024-10-21`) を `fs_edit` で埋めた。モデル選択にはすぐには出ず、ページを読み込み直すと「Dummy Azure GPT」が選ばれた状態になった。
4. メッセージを送ると、ダミーの文が表示された。要求の送り手は `GitHubCopilotChat/0.72.0`、キーは `Authorization: Bearer`、ツールは 37 件 (`create_file`、`read_file`、`run_in_terminal` など)。
5. `TOOLTEST` のメッセージで、`create_file` により `C:\Users\tester\byok-chat-tooltest.txt` が作られ、`read_file` の結果 (`created through BYOK dummy (chat)`) を含む文が表示された (「Completed 2 steps」)。最初の試みでは、Chat が利用者のメッセージの前に長い文脈を付けるため、ダミーの `TOOLTEST` の判定 (先頭 120 文字) に掛からず、通常の文が返った。判定を全文に変えて解消した。

サインインしていないため、チャット欄に「Set BYOK utility models」の通知が出続けた。

## 未確認の項目

- 実際の Azure OpenAI との通信、クライアント経由プロキシ (HTTPS) での到達
- Chat の補助モデル (utility models) の設定
- 対話モードの Copilot CLI (code-server のターミナルでの操作)
