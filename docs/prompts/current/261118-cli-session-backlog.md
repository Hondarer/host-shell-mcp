<!--
生成元: agree-before-implement スキル
文書種別: 残課題一覧 (backlog)
最終更新日時: 2026-11-18 05:50:00 JST
最終更新エージェント: claude code / セッション ID: session_01KJc53SwZ5UXzt7kJWHJ48p
稼働 OS: Linux (WSL)
上流文書: 261118-cli-session-design.md (最終仕様)、261118-cli-session-discussion.md (検討用 Markdown)。いずれも削除されている場合があります。
下流文書: 261118-cli-followup-discussion.md (継続案件 261118-cli-followup の検討用 Markdown。BL-001〜BL-003 を対策)
再開手順: /home/user/c-modernization-kit/app/general/.agents/skills/agree-before-implement/SKILL.md を読み込んでください。「整理状態」が「随時追記中」であれば、案件の作業中です。気付いた事項を追記し、第 3 ゲートの前に文書を再構築して整理します。
-->

# 残課題一覧 (バックログ)

## 案件の識別

案件名: MCP を登録できないエージェントからの CLI による利用 (261118-cli-session)  
作成日: 2026-11-18  
整理状態: 整理済み

## 残課題一覧

| 課題 ID | 題名 | 区分 | 優先度 / 影響度 | 状態 |
| --- | --- | --- | --- | --- |
| BL-001 | agy からの CLI での操作を検証していない | 作業中摘出 | 低 | 完了 |
| BL-002 | VS Code Remote - SSH スキルのクライアント側スクリプトに実行権限がない | 作業中摘出 | 中 | 対応済み |
| BL-003 | code-server スキルの手順に不明確な記述がある | 作業中摘出 | 低 | 対応済み |

## BL-001 agy からの CLI での操作を検証していない

### 概要と問題点

- **現状の事象**: Linux (WSL) の実機で、Claude Code、GitHub Copilot CLI、Codex CLI、Grok からの `host-shell session`・`call`・`tools` での操作 (7 段階) を確認したが、agy 1.5.10 では実行していない。文書の検証済みの範囲では未検証としている。
- **本来あるべき姿**: 手元で使えるすべてのエージェントについて、`host-shell-cli` スキルを読んで MCP なしで操作できることを確認し、検証済みの範囲に記載する。

### 経緯と見送り理由

- **摘出契機**: 実機での検証中。承認を一律に省略するオプション (`--dangerously-skip-permissions`) でのエージェントの非対話実行が、作業環境の権限の判定で拒否された。他のエージェントは `host-shell` だけを許可する限定的な設定で実行できたが、agy の非対話実行 (`-p`) には限定的な許可を与えるオプションが見当たらなかった。
- **今回の見送り理由**: 承認を一律に省略する実行は使えず、agy の限定的な許可の方法 (設定ファイルの許可規則など) を確認できていないため。Q-025 の回答 (実行できないエージェントは理由を記録して未検証とし先へ進む) に従った。
- **継続作業での経緯と対処**: 2026-11-18 に継続案件 261118-cli-followup (`261118-cli-followup-discussion.md`) で検討を開始した。 方針を合意し (agy を `--dangerously-skip-permissions` で非対話に実行して 7 段階を確認する)、最終仕様 `261118-cli-followup-design.md` を作成した。 第 2 ゲートの承認後、agy 1.5.10 を `--dangerously-skip-permissions` で非対話に実行し、7 段階すべての成功を確認した (接続先のファイルと停止後の `session list` を別に確認)。作業環境には拒否されなかった。agy 固有の制約は見つからなかった。結果を `261118-cli-session-validation.md` と `docs/ja/cli-without-mcp.md`・英訳の検証済みの範囲に反映した。

### 影響範囲とリスク

- **影響対象**: `docs/ja/cli-without-mcp.md` と英訳の「検証済みの範囲」、`host-shell-cli` スキル。
- **放置した場合のリスク**: agy 固有の挙動 (シェルの実行時間の上限、子孫のプロセスの扱い、許可規則の照合) による問題を見落とす。Codex では sandbox が `--detach` のセッションを終了させることが検証で分かっており、同種の問題が agy にもありうる。

### 推奨される対応策

- **恒久対応案**: agy の許可規則の設定方法 (設定ファイル、`--mode` の各値の意味) を公式の説明で確認し、`host-shell` だけを許可する設定で、他のエージェントと同じ 7 段階を実行する。利用者が対話的に承認しながら実行する方法でもよい。結果を検証記録と文書の「検証済みの範囲」に反映する。
- **参考情報**: `docs/prompts/current/261118-cli-session-validation.md` の「エージェントからの操作」。

## BL-002 VS Code Remote - SSH スキルのクライアント側スクリプトに実行権限がない

### 概要と問題点

- **現状の事象**: `.agents/skills/host-shell-vscode-remote-ssh/scripts/client/fetch.sh` と `ssh-key.sh` は、リポジトリでのモードが 100644 (実行権限なし) である。スキルは `"$SKILL"/scripts/client/fetch.sh COMMIT ARCH` のように直接実行する形で書いているため、`permission denied` (終了コード 126) で失敗する。他のスキルのクライアント側スクリプト (`host-shell-connect/scripts/*.sh`、`host-shell-code-server/scripts/linux/download.sh` など) は 100755。
- **本来あるべき姿**: スキルに書かれたとおりのコマンドで実行できる。

### 経緯と見送り理由

- **摘出契機**: CLI 経由での 3 つのスキルの実行の検証中に、エージェントが `fetch.sh` と `ssh-key.sh` を実行できなかった。MCP で使う場合も、手元のシェルで同じコマンドを実行するため同じく失敗する。
- **今回の見送り理由**: 今回の案件の範囲 (CLI での利用) の外にある既存の不具合で、最終仕様にない変更のため。検証は `sh` を付けて実行して行った。
- **継続作業での経緯と対処**: 2026-11-18 に継続案件 261118-cli-followup (`261118-cli-followup-discussion.md`) で検討を開始した。 方針を合意し (`.agents/skills` の `.sh` をすべて 100755 にそろえ、CI に検査を加える)、最終仕様 `261118-cli-followup-design.md` を作成した。 第 2 ゲートの承認後、100644 だった 11 個の `.sh` を 100755 にし、CI (`test (ubuntu-latest)`) にモードの検査を加え、規則を `docs/ja/development.md`・英訳に記載した。`fetch.sh` と `ssh-key.sh` を直接実行して使い方の表示 (終了コード 2) を確認し、検査が実行権限を外すと失敗し、付け直すと成功することを手元で確認した。

### 影響範囲とリスク

- **影響対象**: `host-shell-vscode-remote-ssh` スキルの手順 2 と 4 (Linux・macOS のクライアント)。
- **放置した場合のリスク**: エージェントが手順で止まるか、利用者の承認なしに `chmod` などで回避しようとする。

### 推奨される対応策

- **恒久対応案**: `git update-index --chmod=+x` で `scripts/client/*.sh` に実行権限を付ける。あわせて、接続先で `sh` を付けて実行する `scripts/linux/*.sh` との違いが分かるよう、スキルの記述を確認する。CI に、スキルの `scripts/**/*.sh` のうち直接実行するものの実行権限を確かめる検査を加えることも検討する。
- **参考情報**: `docs/prompts/current/261118-cli-session-validation.md` の「CLI 経由での 3 つのスキルの実行」。

## BL-003 code-server スキルの手順に不明確な記述がある

### 概要と問題点

- **現状の事象**: `host-shell-code-server` スキルの次の記述について、検証を実行したエージェントが判断に迷った。
    - 手順 1.4: `sys_get_info` の `restrictions` が空でない場合は利用者の承認を求めるが、伏せ字 (`--redact`) だけの制限もこれに当たるかが書かれていない。
    - 手順 5: 設定の写しの保存先に使う `<target name>` が定義されていない (エージェントは ssh の別名を使った)。
- **本来あるべき姿**: エージェントが利用者に尋ねずに判断できる、または尋ねるべき場面が明確な記述。

### 経緯と見送り理由

- **摘出契機**: CLI 経由での 3 つのスキルの実行の検証中に、エージェントが不明点として報告した。CLI での利用とは関係なく、MCP で使う場合も同じ。
- **今回の見送り理由**: 今回の案件の範囲外の既存の記述であり、意図の確認が必要なため。
- **継続作業での経緯と対処**: 2026-11-18 に継続案件 261118-cli-followup (`261118-cli-followup-discussion.md`) で検討を開始した。 方針を合意し (手順 1.4 は空でない `restrictions` をすべて承認の対象とし理由を明記する。手順 5 の `<target name>` は登録名またはセッションの名前とし、区別できなければ利用者と決める)、最終仕様 `261118-cli-followup-design.md` を作成した。 第 2 ゲートの承認後、`host-shell-code-server` スキルの手順 1.4 と手順 5、`docs/ja/code-server.md`・英訳 (パスワードの節、セキュリティ上の注意) を改めた。

### 影響範囲とリスク

- **影響対象**: `.agents/skills/host-shell-code-server/SKILL.md`。
- **放置した場合のリスク**: エージェントによって、不要な問い合わせをしたり、利用者の承認を得ずに進めたりする。設定の写しの保存先が接続先ごとにそろわない。

### 推奨される対応策

- **恒久対応案**: 手順 1.4 で承認が必要な制限の種類 (`--read-only`、`--tools`、`--allow-path` など、導入の手順に影響するもの) を列挙する。手順 5 で `<target name>` の決め方 (利用者と決める短い名前、または登録名) を定義する。
- **参考情報**: `docs/prompts/current/261118-cli-session-validation.md` の「CLI 経由での 3 つのスキルの実行」。

