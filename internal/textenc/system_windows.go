//go:build windows

package textenc

import "golang.org/x/sys/windows"

// getOEMCP is not wrapped by x/sys/windows. The system DLL loader searches only System32.
var getOEMCP = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetOEMCP")

// Console returns the target's default command I/O encoding (the Windows OEM code page, e.g. CP932 on Japanese systems).
func Console() Encoding {
	cp, _, _ := getOEMCP.Call()
	return codePage(uint32(cp))
}

// File returns the target's default text file encoding (the Windows ANSI code page, e.g. CP932 on Japanese systems).
func File() Encoding { return codePage(windows.GetACP()) }

func codePage(cp uint32) Encoding {
	if e, ok := fromCodePage(int(cp)); ok {
		return e
	}
	return UTF8
}
