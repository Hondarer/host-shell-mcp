//go:build windows

package textenc

import "syscall"

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	getOEMCP = kernel32.NewProc("GetOEMCP")
	getACP   = kernel32.NewProc("GetACP")
)

// Console returns the target's default encoding for command input and output.
// On Windows, it is the OEM code page (932 on Japanese systems).
func Console() Encoding { return codePage(getOEMCP) }

// File returns the target's default encoding for text files.
// On Windows, it is the ANSI code page (932 on Japanese systems).
func File() Encoding { return codePage(getACP) }

func codePage(p *syscall.LazyProc) Encoding {
	cp, _, _ := p.Call()
	if e, ok := FromCodePage(int(cp)); ok {
		return e
	}
	return UTF8
}
