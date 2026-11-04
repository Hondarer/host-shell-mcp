# Windows 接続先で SSH の標準入力が止まる問題の検証記録 (2026-11-05)

> [!IMPORTANT]
> 作業時点の記録です。現在の手順は [利用ガイド](../../ja/user-guide.md) の「SSH だけで送る」と、スキル host-shell-connect の `deploy.sh` を参照してください。前回の記録 [Windows 接続先の検証記録](261104-windows-target-validation.md) では原因を特定できず、base64 と `findstr`・`certutil` で回避していました。

## 目的

Windows PowerShell 5.1 が SSH の標準入力を読むと約 128 KiB で止まる原因を調べ、理由の分かっている回避策で配置手順を置き換える。根本原因 (カーネル内のどこで止まるか) は、回避策が決定論的に成り立つ範囲で追う。

## 環境

前回と同じ test-bed (Windows 11 Enterprise ビルド 26300、amd64、Windows PowerShell 5.1.26100、OpenSSH_for_Windows 9.5p2 の `sshd`、既定シェル `cmd`、閉じたネットワーク)。接続元は Windows 11 の Windows 版 `ssh`。

## 調べ方

接続元から 8 バイトの連番 (ほかにテキスト、ゼロ埋め) を `ssh -T` の標準入力で送り、受け手だけを変えて、受け取ったバイト数と欠落の有無を比べた。受け手は 20 秒で打ち切り、読み取りごとの大きさと時刻をファイルに記録させた。受け手として次を用意した。

- C# (.NET Framework 4、接続先の `csc.exe` でビルド): `Console.OpenStandardInput()` を同期で読むものと、`GetStdHandle(-10)` を `new FileStream(handle, FileAccess.Read, size, true)` で非同期に読むもの
- Go 1.26 (`os.Stdin.Read`)
- 標準入力ハンドルの種類を表示する Go の診断プログラム (`GetFileType`、`NtQueryInformationFile` の `FileModeInformation`、`GetNamedPipeInfo`)

## 結果

| 受け手 | 4 MiB の結果 |
|---|---|
| PowerShell 5.1 `[Console]::OpenStandardInput().CopyTo` | 135168 バイトで停止 |
| C# 同期読み取り (80 KiB ずつ) | 135168 バイトで停止。直前の 3 回の読み取りは同じミリ秒 |
| C# 同期読み取り (4 KiB ずつ) | 32768 バイトで停止 |
| C# 非同期 `FileStream` (4 KiB、80 KiB) | 完了 |
| PowerShell 5.1 で同じハンドルを非同期 `FileStream` で読む | 完了 (12 MiB も完了、約 0.9 秒) |
| Go 1.26 (4 KiB、80 KiB、3 秒待ってから読む) | 完了 |
| `findstr` | 完了 (12 MiB も完了) |

停止は内容に依存しなかった (連番、テキスト、ゼロ埋めで同じ位置)。受け取ったデータに欠落や順序の乱れはなく、先頭から途中までが正しく届いて止まる。

診断プログラムでは、`sshd` が渡す標準入力はパイプ (`GetFileType` が 3)、`FileModeInformation` が 0 (同期 I/O のフラグなし、つまり `FILE_FLAG_OVERLAPPED` 付きで開かれている)、パイプのバッファーは 4096 バイトだった。標準出力と標準エラーは `\W32PosixPipe.<pid>.<n>` という名前のパイプだった。Win32-OpenSSH の `contrib/win32/win32compat/fileio.c` の `fileio_pipe` は、パイプの両端を `FILE_FLAG_OVERLAPPED` 付きで作っている。

## 判断

原因は、非同期 I/O 用に開かれたパイプを同期的に読むことだと判断した。Microsoft の `ReadFile` の説明は、`FILE_FLAG_OVERLAPPED` で開いたハンドルには OVERLAPPED 構造体を渡す必要があるとしている。.NET Framework の `Console.OpenStandardInput()` は OVERLAPPED 構造体なしで `ReadFile` を呼ぶので、PowerShell 5.1 に限らず止まる。Go 1.25 以降は `os.NewFile` が非同期 I/O 用のハンドルに対応したため止まらないと考えられる。`findstr` が止まらない理由は分からない。

止まる仕組み (`sshd` の書き込みが再開しない理由) はカーネルまで追っていない。同じプロセス・同じハンドルのまま、非同期で読むだけで解消するため、回避策としてはそれで足りると判断した。135168 は 32768 + 102400 で、102400 は `fileio.c` の `WRITE_BUFFER_SIZE` と一致するが、関係は確かめていない。

## 回避策

PowerShell 5.1 だけで、非同期の読み取りに切り替える。`Add-Type` (C# のコンパイル) を避け、`__ConsoleStream` の非公開フィールド `_handle` をリフレクションで取り出す。.NET Framework 4.x はこれ以上機能を変えないため、フィールド名は変わらない見込みである。

```powershell
$s = [Console]::OpenStandardInput()
$h = $s.GetType().GetField('_handle', [Reflection.BindingFlags]'NonPublic,Instance').GetValue($s)
try { $i = New-Object IO.FileStream($h, [IO.FileAccess]::Read, 65536, $true) } catch { $i = $s }
try { $i.CopyTo($o) } finally { $o.Close() }
```

ハンドルが同期 I/O 用のときは、`FileStream` の生成が「ハンドルは非同期操作をサポートしません」で失敗し、通常の読み取りに戻る。

| 確認 | 結果 |
|---|---|
| `sshd` の標準入力 (12 MiB) | 非同期で読み、完了 |
| 同、`-InputFormat None` を付ける | 完了 (付けなくてよい) |
| 同、標準入力が 3 秒遅れて届く | 完了 |
| 接続先の `cmd` のパイプ (`type file \| powershell ...`) | 同期に戻り、完了 |
| 接続先でファイルからのリダイレクト (`powershell ... < file`) | 同期に戻った後、0 バイトのまま停止。回避策を含まない `[Console]::OpenStandardInput().CopyTo` だけなら完了するので、`FileStream` の生成の失敗が影響している。SSH 経由の手順では起きない条件のため、対象外とした |

## 反映

- `deploy.sh` の Windows 向けの転送を、base64 と `findstr`・`certutil` から、この回避策で標準入力のバイト列を直接書く方式に戻した。base64 の一時ファイルが要らなくなり、空き容量の確認は元のサイズになった。13 MiB の配置は約 4.4 秒 (base64 方式は約 5.7 秒)、2 回目は `up to date`、`probe.sh` は OK だった。Linux 向けの経路は変えていない。
- 利用ガイドの手動手順を同じ方式に変え、文書の手順をそのまま実行して 13 MiB の配置と SHA-256 の一致を確認した (約 0.9 秒)。
- SKILL.md の `deploy.sh` の説明を更新した。

## 未確認の項目

- カーネル内で止まる位置と、`findstr` が止まらない理由
- Windows Server、arm64、ほかの Win32-OpenSSH の版
- PowerShell 7 (`pwsh`) の `Console.OpenStandardInput()` が同じ条件で止まるか (手順は Windows PowerShell 5.1 を使う)

## 参考

- [Win32-OpenSSH の fileio.c](https://github.com/PowerShell/openssh-portable/blob/latestw_all/contrib/win32/win32compat/fileio.c)
- [ReadFile function (Microsoft Learn)](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-readfile)
- [Win32-OpenSSH #908 (PowerShell への標準入力が欠ける、2017 年に修正)](https://github.com/PowerShell/Win32-OpenSSH/issues/908)
- [bigfix-remote-client-relevance #101 (Windows の sshd でパイプが互いに待つ報告)](https://github.com/jgstew/bigfix-remote-client-relevance/issues/101)
