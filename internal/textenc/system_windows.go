//go:build windows

package textenc

import "syscall"

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	getOEMCP = kernel32.NewProc("GetOEMCP")
	getACP   = kernel32.NewProc("GetACP")
)

// Console は、コマンドの入出力に使われる接続先の既定の文字コードを返します。
// Windows では OEM コード ページ (日本語の環境では 932) です。
func Console() Encoding { return codePage(getOEMCP) }

// File は、テキスト ファイルに使われる接続先の既定の文字コードを返します。
// Windows では ANSI コード ページ (日本語の環境では 932) です。
func File() Encoding { return codePage(getACP) }

func codePage(p *syscall.LazyProc) Encoding {
	cp, _, _ := p.Call()
	if e, ok := FromCodePage(int(cp)); ok {
		return e
	}
	return UTF8
}
