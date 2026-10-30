# host-shell 開発ガイド

この文書は、`host-shell` をビルド、テスト、拡張する開発者向けの情報をまとめたものです。

## 開発環境

| 項目 | 要件 |
|---|---|
| Go | 1.25 以降 (`go.mod` の `go` 指令) |
| make | GNU make |
| OS | Linux、macOS、Windows のいずれか。クロスビルドのため、1 台で全ターゲットを作れる |

Table: 開発環境

外部の C ライブラリには依存しません。ビルドは常に `CGO_ENABLED=0` で行います。

## ビルド

| コマンド | 内容 |
|---|---|
| `make` | `vet`、`test`、`build` を順に実行する |
| `make build` | 開発機の OS とアーキテクチャ向けに `dist/host-shell` を作る |
| `make dist` | 配布用の 4 ファイル (`dist/host-shell-{linux,windows}-{amd64,arm64}[.exe]`) を作り、`host-shell-connect` skill の `bin/` にも複製する |
| `make release` | Release に添付するファイルを `dist/release/` に作る ([リリース](#リリース)) |
| `make test` | テストを実行する |
| `make vet` | Linux と Windows の両方を対象に `go vet` を実行する |
| `make clean` | `dist/` と skill の `bin/` を削除する |

Table: make のターゲット

バージョンは `git describe --tags --always --dirty` から埋め込みます。Git の情報がない場合は `dev` です。  
明示する場合は `make dist VERSION=1.0.0` のように指定します。

## ソースの構成

```text
cmd/host-shell/        エントリ ポイント。フラグの解析、操作ログの準備、stdio での起動
internal/server/       MCP サーバーの組み立て (ツールの登録、初期化の案内)
internal/tools/        各ツールの実装
  tools.go             ツールの登録 (説明と注釈)、操作ログを挟む共通の処理
  help.go              help ツール、help のリソース、初期化の応答の案内
  help/*.md            help の各 topic の本文 (実行ファイルに埋め込む)
  exec.go              exec_run、job_*
  files.go             fs_list、fs_stat、fs_read、fs_write
  edit.go              fs_edit
  fsops.go             fs_mkdir、fs_move、fs_copy、fs_remove
  search.go            search_grep、search_files、fs_manifest
  glob.go              glob の照合 (**、{a,b})
  system.go            sudo_run、sys_info、sys_processes
internal/proc/         子プロセスの起動。OS ごとのシェルとプロセス ツリーの終了
  proc_unix.go         Linux (プロセス グループ、SIGKILL)
  proc_windows.go      Windows (cmd のコマンド行、taskkill /T)
internal/jobs/         長時間ジョブの管理
internal/limit/        出力の上限 (先頭と末尾を残す Buffer、位置で読み出す Ring)
internal/textenc/      文字コードの変換 (名前の解決、自動判定、出力の変換、接続先の既定の文字コード)
  system_unix.go       Linux (ロケールから判定)
  system_windows.go    Windows (GetOEMCP、GetACP)
internal/audit/        操作ログ (JSON Lines)
internal/config/       設定と既定値
internal/version/      バージョン情報
.agents/skills/host-shell-connect/
                       接続先への配置と起動の確認を行う skill。bin/ は make dist で作る (Git では管理しない)
```

## 設計上の約束

実装を変更するときは、次の約束を守ってください。

- **標準出力に書かない。** 標準出力は MCP の JSON-RPC 専用です。診断は標準エラー (`log` パッケージ) に、操作の記録は `audit` に出します。
- **子プロセスに標準入出力を渡さない。** `exec.Cmd` の `Stdin`、`Stdout`、`Stderr` に `os.Stdin`、`os.Stdout` を設定しないでください。子プロセスの起動は必ず `proc.Command` か `proc.Run` を通します。
- **出力に上限を設ける。** ツールの出力は、`limit` パッケージか件数の上限で、`--max-output` 程度に収めます。上限に達したことは `truncated` などで示します。
- **秘密をログに残さない。** 書き込む内容や標準入力のように記録すべきでない引数は、入力の型に `AuditArgs() any` を実装して置き換えます (`audit.Redactor`)。
- **文字コードはツールの内側で変換する。** ツールの入出力は常に UTF-8 です。接続先の文字コードとの変換は `textenc` を通し、文字コードを指定する引数は `textenc.Parse` で解決します。範囲を区切って返す処理では、文字を分断しないようにします。
- **エージェントが MCP の情報だけで使えるようにする。** ツールの説明、引数の説明、`help` の本文、エラーの説明は、エージェントが次に何をすればよいかが分かるように書きます。
- **OS の差はツールの内側で吸収する。** OS ごとの処理は `runtime.GOOS` の分岐か、`_unix.go`、`_windows.go` のファイルに分けます。

## ツールの追加

1. 入力と出力の型を定義します。JSON の名前は `json` タグで、説明は `jsonschema` タグで付けます。省略できる引数には `omitempty` を付けます。付けない引数は必須になります。
2. `func (d *Deps) xxx(ctx context.Context, req *mcp.CallToolRequest, in XxxInput) (*mcp.CallToolResult, XxxOutput, error)` の形でハンドラーを書きます。操作の失敗は `error` で返すと、`isError` の結果になります。
3. `tools.Register` で `add` を使って登録します。読み取りだけなら `readOnly()`、変更を伴うなら `destructive()` の注釈を付けます。
4. `internal/server` にテストを追加します。
5. 使い方の説明が必要なら、`internal/tools/help/*.md` の該当する topic と、`Instructions` の案内に追記します。
6. [リファレンス](reference.md) にツールの仕様を追記します。

```go
add(s, d, &mcp.Tool{
    Name:        "fs_example",
    Description: "One-line description for agents.",
    Annotations: readOnly(),
}, d.fsExample)
```

ツールの説明 (`Description`) と引数の説明は、エージェントが読むものです。英語で、何をするか、いつ使うか、ほかのどのツールと組み合わせるかを短く書きます。  
`Title` には、利用者が承認の画面などで目にする短い名前を付けます。  
エラーは、何が悪く、どう直せばよいかを 1 文で返します (例: `old_string occurs 3 times ... Include more surrounding lines to make it unique, or set replace_all`)。

## テスト

`internal/server` のテストは、メモリ上の通信路 (`mcp.NewInMemoryTransports`) でサーバーとクライアントをつなぎ、実際にツールを呼びます。  
OS によって振る舞いが違うツールは、`runtime.GOOS` でコマンドを切り替えてテストします。

Windows の実装は、Linux の開発機では `make vet` (`GOOS=windows go vet`) でコンパイルできることまでしか確かめられません。  
`proc_windows.go` などを変更した場合は、Windows の実機で `go test ./...` を実行してください。

標準入出力を通した確認は、実行ファイルに JSON-RPC を流し込んで行えます ([利用ガイドの動作の確認](user-guide.md#動作の確認))。

## CI

GitHub Actions で、`main` への push とプル リクエストごとに次を実行します (`.github/workflows/ci.yml`)。

| ジョブ | 内容 |
|---|---|
| `test (ubuntu-latest)` | `go vet` (Linux と Windows 向け)、`go test` |
| `test (windows-latest)` | `go vet`、`go test`。Windows の実機での唯一の自動テスト |
| `build` | `make release` を実行し、`dist/release/` を成果物 (14 日間保持) として残す |

Table: CI のジョブ

## リリース

GitHub で Release を公開すると、そのタグでビルドし、次のファイルを Release に添付します (`.github/workflows/release.yml`)。  
タグの名前 (例: `v1.0.0`) が、そのままバージョンになります。

| ファイル | 内容 |
|---|---|
| `host-shell-connect-<tag>.tar.gz`、`host-shell-connect-<tag>.zip` | `host-shell-connect` skill のフォルダ。`bin/` に 4 種類の実行ファイルと `VERSION` を含む |
| `host-shell-linux-amd64` など 4 ファイル | 実行ファイル |
| `SHA256SUMS` | 上のファイルの SHA-256 |

Table: Release に添付するファイル

アーカイブは `host-shell-connect/` を最上位に持つため、`.agents/skills/` の下に展開すれば、リポジトリを clone せずに skill を使えます。  
手元で同じものを作るには `make release VERSION=v1.0.0` を実行します。

## 依存

| モジュール | 用途 |
|---|---|
| `github.com/modelcontextprotocol/go-sdk` | MCP の公式の Go SDK。stdio、JSON-RPC、スキーマの生成と検証 |
| `golang.org/x/text` | 文字コードの変換。Go だけで書かれており、静的リンクの方針を崩さない |

Table: 直接の依存

依存を更新したら、`go mod tidy` のあとに `make` を実行してください。

## 動作する OS

Go の要件により、次の OS で動作します。

| OS | 要件 |
|---|---|
| Windows | Windows 10、Windows Server 2016 以降 |
| Linux | カーネル 3.2 以降 |

Table: 動作する OS

これより古い OS には対応しません。要件は Go の [MinimumRequirements](https://go.dev/wiki/MinimumRequirements) で確認できます。  
Windows では、ウイルス対策ソフトが Go 製の実行ファイルを誤検知することがあります。配布の際はコード署名を検討してください。
