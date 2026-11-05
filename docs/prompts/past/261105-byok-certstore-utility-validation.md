# BYOK の証明書ストアと補助用モデルの検証記録 (2026-11-05)

> [!IMPORTANT]
> 作業時点の記録です。現在の手順と検証済みの範囲は [CLI 型 AI コーディングエージェント](../../ja/coding-agents.md) の「BYOK」を参照してください。前段の検証は [BYOK の HTTPS とプロキシの検証記録](261105-byok-https-proxy-validation.md) にあります。

## 背景

HTTPS とプロキシの検証で残った 2 点を、test-bed の Windows で確かめた。1 つは、Windows の画面内の Copilot Chat が、認証局を OS の証明書ストアに登録すると接続できるか。もう 1 つは、サインインなしの Chat で表示される「Set BYOK utility models」の設定で、補助処理が BYOK のモデルで動くか。証明書ストアは接続先に残る設定のため、test-bed に限って利用者の了承を得て行い、確認後に削除した。

## 環境

- 接続先: test-bed の Windows (接続ユーザー `tester`、Administrators)。code-server 4.144.0、GitHub Copilot Chat 0.72.0。
- エンドポイント: ホストの `127.0.0.1:18444` の HTTPS のダミー (検証用の認証局で発行した証明書)。
- 経路: `bridge --proxy --proxy-allow-host localhost.:18444`、接続先は `--client-proxy`。モデルの `url` は `https://localhost.:18444/...` (`localhost` だとプロキシを通らないため)。
- モデルは前段で画面から登録した Azure の提供元のもの (キーはセキュア ストレージの参照)。

## 証明書ストア

1. `certutil -addstore Root <test-ca.pem>` で、ローカル コンピューターの `Root` に登録した (`CN=host-shell test CA`、拇印 `26FCB7DA...A9D0`)。SSH の管理者セッションから、確認の表示なしで登録できた。
2. code-server を `NODE_EXTRA_CA_CERTS` と `SSL_CERT_FILE` を付けずに起動した。
3. メッセージを送ると、ダミーの文が表示された。`TOOLTEST` では、ワークスペース外のファイルの作成と読み取りで確認が 2 回出た (「このセッションで許可する」を選んだ)。その後、4 段階が実行され、ツールの結果を含む文が返った。
4. 確認後に `certutil -delstore Root <拇印>` で削除し、`Cert:\LocalMachine\Root` に残っていないことを確かめた。

## 補助用モデル

1. 設定前のログには、`No utility model is configured for 'copilot-utility-small' while the selected main agent model is BYOK. Configure setting 'chat.utilitySmallModel' or set 'chat.byokUtilityModelDefault' to 'mainAgent'.` が出ていた。
2. 「Set BYOK utility models」の「Configure」を選ぶと、設定画面が `chat.byokUtilityModelDefault` (既定値は「GitHub Copilot」、選択肢は「None」「Main Agent Model」「GitHub Copilot」) を開いた。「Main Agent Model」を選ぶと、ユーザー設定に `"chat.byokUtilityModelDefault": "mainAgent"` が書かれた。
3. ページを読み込み直すと、「Set BYOK utility models」の表示が消えた。
4. 新しいチャットでメッセージを送ると、ダミーには通常の要求 (ツール 37 件) に加えて、ツールなし・メッセージ 2 件の要求が 2 回届いた。内容は「edit code」と「generate code」の場面向けに、進行中の表示文を 10 個生成させるものだった。
5. 上記のログのエラーは設定前の時刻にだけ出ており、設定後は出ていない。

## 終了時の状態

- 証明書ストアの登録は削除した。ダミーのエンドポイントは停止した。
- test-bed の code-server のユーザー設定には `chat.byokUtilityModelDefault` が残る。

## 未検証

- `chat.utilityModel` と `chat.utilitySmallModel` による個別の指定
- Linux の画面内の Copilot Chat での補助用モデル
- 実際の Azure OpenAI との通信
