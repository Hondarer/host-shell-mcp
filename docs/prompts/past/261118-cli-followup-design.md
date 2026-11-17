<!--
生成元: agree-before-implement スキル
文書種別: 最終仕様 (design)
最終更新日時: 2026-11-18 03:50:00 JST
最終更新エージェント: claude code / セッション ID: session_01KJc53SwZ5UXzt7kJWHJ48p
稼働 OS: Linux (WSL)
上流文書: 261118-cli-followup-discussion.md (検討用 Markdown、合意版: 版 2)。対策元: 261118-cli-session-backlog.md (BL-001〜BL-003)。削除されている場合があります。
下流文書: 261118-cli-followup-backlog.md (残課題一覧。残課題がある場合のみ)。未作成の場合や、削除されている場合があります。
再開手順: /home/user/c-modernization-kit/app/general/.agents/skills/agree-before-implement/SKILL.md を読み込んでください。上流文書の作業 WBS の `← **CURRENT**` の項目から再開します。
-->

# CLI による利用の残課題 (BL-001〜BL-003) の対策 最終仕様

版: 1

## 目的

261118-cli-session の残課題 3 件を対策します。

| 課題 | 対策 |
| --- | --- |
| BL-001 agy での未検証 | agy で 7 段階を実行し、検証済みの範囲に反映する |
| BL-002 クライアント側スクリプトの実行権限 | スキルの `.sh` をすべて実行可能にし、CI で検査する |
| BL-003 code-server スキルの不明確な記述 | 手順 1.4 と手順 5 の記述を明確にする |

Table: 課題と対策

## BL-002 スクリプトの実行権限

- `.agents/skills/**/*.sh` のうち 100644 の 11 個を、`git update-index --chmod=+x` で 100755 にします。内容は変えません。
- `.github/workflows/ci.yml` の `test` ジョブ (ubuntu-latest のみ) に、`git ls-files -s` で `.agents/skills` の `.sh` のモードを調べ、100755 でないものを列挙して失敗する手順を加えます。
- `docs/ja/development.md` と英訳に、「スキルの `.sh` は直接実行できるよう 100755 で登録し、CI で検査する」旨を記載します (設計規則またはテストの節)。

> [!NOTE]
> 接続先で `sh`・`bash` を付けて実行するスクリプトや読み込まれるスクリプトも対象に含めます。実行権限があっても、それらの動作は変わりません。規則を一律にすることで、新しいスクリプトを加えるときの見落としを CI で防ぎます。

## BL-003 code-server スキルの記述

### 手順 1.4 (承認が必要な制限)

`.agents/skills/host-shell-code-server/SKILL.md` の手順 1.4 を次の趣旨に改めます。

- `sys_get_info` の `restrictions` が空でなければ、項目の種類によらず (伏せ字 `--redact` と確認 `--confirm` を含む) 利用者の承認を求めます。
- 理由として、code-server の端末とファイル操作は `host-shell` を通らないため、制限、伏せ字、確認のいずれも働かず、操作ログにも残らないことを説明します。

`docs/ja/code-server.md` (制限の節) と英訳の対応する記述も、「ポリシー制限」に伏せ字と確認が含まれることが分かるように改めます。

### 手順 5 (設定の写しの保存先)

`<target name>` (文書では `<接続先名>`) を次のように定義します。スキル、`docs/ja/code-server.md`、英訳に記載します。

- MCP で使う場合は、エージェントに登録した MCP サーバーの名前 (例: `host-shell-target`)。
- CLI (`host-shell-cli` スキル) で使う場合は、セッションの名前 (例: `default`)。
- その名前で接続先を区別できない場合 (例: 複数の接続先を同じ名前で使い分けている) は、利用者と短い名前を決めます。

## BL-001 agy での検証

- agy 1.5.10 を `--dangerously-skip-permissions` で非対話 (`-p`) に実行し、他のエージェントと同じ指示 (`host-shell-cli` スキルを読み、MCP なしで 7 段階: `--detach` での開始、`tools`、`exec_run`、`fs_write`・`fs_read`、`job_start`・`job_read`、`transfer_push`、`session stop`) を与えます。接続先は前回と同じ検証用コンテナーです。
- 作業環境がこの実行を拒否した場合は、`--settings '{"agentMode": "", "permissions": {"allow": ["command(host-shell)"]}}'` で `host-shell` だけを許可して実行します。
- 結果を `docs/prompts/past/261118-cli-session-validation.md` の「エージェントからの操作」に追記します (記録の追記であり、対策元の検証記録を継続案件の結果で補います)。
- `docs/ja/cli-without-mcp.md` と英訳の「検証済みの範囲」を更新します。
    - 成功した場合: 「agy からの操作」の行を削除し、エージェントからの操作の行に agy 1.5.10 を加えます。許可の条件が他のエージェントと異なる場合は、その条件 (承認の省略) を併記します。
    - agy 固有の制約 (シェルの実行時間、子孫のプロセスの扱いなど) が見つかった場合は、制限の節と `host-shell-cli` スキルに記載します。
    - 失敗した場合や実行できない場合: 未検証のままとし、理由を検証記録と対策元の残課題に記録します。

## 更新する成果物

| 成果物 | 内容 |
| --- | --- |
| `.agents/skills/**/*.sh` (11 個) | モードを 100755 に変更 |
| `.github/workflows/ci.yml` | スクリプトのモードの検査 |
| `docs/ja/development.md`、`docs/en/development.md` | スクリプトのモードの規則 |
| `.agents/skills/host-shell-code-server/SKILL.md` | 手順 1.4、手順 5 |
| `docs/ja/code-server.md`、`docs/en/code-server.md` | 承認が必要な制限、`<接続先名>` の定義 |
| `docs/ja/cli-without-mcp.md`、`docs/en/cli-without-mcp.md` | 検証済みの範囲 (agy) |
| `.agents/skills/host-shell-cli/SKILL.md` | agy 固有の制約が見つかった場合のみ |
| `docs/prompts/past/261118-cli-session-validation.md` | agy での検証の結果 |
| `docs/prompts/past/261118-cli-session-backlog.md` | 各課題の状態と対処 |

Table: 更新する成果物

作業はブランチ `feature/cli-followup` で行い、プル リクエストの CI で確認します。

## 受入条件

1. `git ls-files -s .agents/skills` で、すべての `.sh` が 100755 である。
2. CI の検査が、100644 の `.sh` があると失敗し (手元でモードを戻して確認)、現状では成功する。
3. `"$SKILL"/scripts/client/fetch.sh` と `ssh-key.sh` を、スキルに書かれたとおり直接実行できる (引数の誤りで使い方を表示する程度の確認)。
4. code-server スキルと文書 (ja、en) で、手順 1.4 の対象に伏せ字と確認が含まれることと理由、手順 5 の `<target name>` の定義が読み取れる。ja と en の内容が一致する。
5. agy での 7 段階の結果が検証記録に記録され、文書の検証済みの範囲が結果と一致する。
6. プル リクエストの CI がすべて成功する。
7. 対策元の残課題一覧で、BL-001〜BL-003 の最終状態と対処が記録されている。残課題がなければ、両案件の記録を `docs/prompts/past/` へ移す。
