//go:build windows

package textenc

import "syscall"

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	getOEMCP = kernel32.NewProc("GetOEMCP")
	getACP   = kernel32.NewProc("GetACP")
)

// Console returns the target's default command I/O encoding (the Windows OEM code page, e.g. CP932 on Japanese systems).
func Console() Encoding { return codePage(getOEMCP) }

// File returns the target's default text file encoding (the Windows ANSI code page, e.g. CP932 on Japanese systems).
func File() Encoding { return codePage(getACP) }

func codePage(p *syscall.LazyProc) Encoding {
	cp, _, _ := p.Call()
	if e, ok := FromCodePage(int(cp)); ok {
		return e
	}
	return UTF8
}
