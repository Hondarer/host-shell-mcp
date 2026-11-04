# Windows 接続先の code-server のブラウザー検証記録 (2026-11-05)

> [!IMPORTANT]
> 作業時点の記録です。現在の手順と検証済みの範囲は [code-server の導入](../../ja/code-server.md) を参照してください。

## 目的

Windows 接続先の code-server について、ブラウザーでの日本語表示が未検証だった。ログインにパスワードの入力が必要なため、これまでは確認していなかった。test-bed は隔離された検証環境なので、利用者の許可を得て、エージェントがブラウザーを操作し、パスワードを入力して確かめた。

## 環境

- 接続先: test-bed (チェックポイント `closed-ssh` から戻した Windows 11 Enterprise ビルド 26300、amd64、接続ユーザーは管理者)
- code-server 4.144.0、`install.ps1 -Locale ja`
- クライアント: ホストの Chrome (Claude in Chrome で操作)。`forward_open` でホストの `127.0.0.1:18765` を接続先の `127.0.0.1:8000` へ転送した

## 結果

| 確認 | 結果 |
|---|---|
| ログイン | `config.yaml` のパスワードでログインできた |
| 表示言語 | エクスプローラー、チャット、ウェルカム ページ、コマンド パレット (日本語名と英語名の併記)、ターミナルのパネルが日本語で表示された |
| ワークスペース (修正前) | `start.ps1` が `C:\Users\tester` を code-server に渡すと、`/` は `?folder=C%3A%5CUsers%5Ctester` (`C:\Users\tester`) へ転送された。エクスプローラーのフォルダーに警告が付き、「相対ファイル パス 'C:\Users\tester' の filesystem プロバイダーを解決できません」となって中身を表示しなかった |
| URL の形を変える | `?folder=/c:/Users/tester` ではフォルダーの中身が表示された |
| 渡し方を変える | `C:/Users/tester` を渡しても転送先は `C:\Users\tester` になった。`/c:/Users/tester` は `start.ps1` がフォルダーとして見つけられなかった |
| 統合ターミナル | PowerShell 5.1.26100 が起動し、`whoami` と日本語の文字列が正しく表示された |

## 判断と修正

code-server の Windows 版は、起動時に渡したフォルダーを Windows 形式のパスのまま URL の `folder` に入れるため、ブラウザー側が解決できない。code-server 側は変えず、スキルで回避した。

- `start.ps1` はフォルダーを code-server に渡さない。代わりに、フォルダーを `/C:/...` の形にして URL エンコードした `workspace URL path: /?folder=...` を出力する。UNC パスはエラーにする。
- SKILL.md の起動手順で、Windows ではこのパスをクライアントの URL に付けて案内するようにした。

修正後、`/?folder=%2FC%3A%2FUsers%2Ftester` を開くとエクスプローラーに `C:\Users\tester` の中身が表示され、表示は日本語だった。終了後は `job_stop` で node.exe が残らなかった。

## 管理者でない接続ユーザー

修正後の `start.ps1` で、`closed-ssh` から接続ユーザーを Administrators から外した状態でも確かめた ([管理者でない Windows 接続ユーザーの検証記録](261105-windows-standard-user-validation.md) と同じ手順)。Users だけの場合 (ターミナルの `whoami /groups` は S-1-5-32-545) と Power Users にも入れた場合 (S-1-5-32-547 と S-1-5-32-545) の両方で、ログイン、日本語表示、`workspace URL path` の URL でのフォルダーの表示、統合ターミナルでの PowerShell 5.1 の起動と日本語の入出力が管理者の場合と同じ結果だった。終了後は node.exe が残らなかった。

「問題」パネルに出た 3 件は、同梱の拡張機能 (git-base、git、github) の `package.json` に対する情報 (`*` のアクティブ化) で、権限とは関係しない。

## 未確認の項目

- Copilot Chat などの拡張機能の動作
- 英語への表示言語の切り替え (Linux では確認済み)
