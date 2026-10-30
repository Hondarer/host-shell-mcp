package proc

import (
	"testing"

	"golang.org/x/sys/windows"
)

// TestUTF8Console checks the console host-shell prepares for PowerShell: code pages 65001 and no
// window. It needs stdin and stdout that are not consoles, as under go test with redirected output.
func TestUTF8Console(t *testing.T) {
	if isConsole(windows.STD_INPUT_HANDLE) || isConsole(windows.STD_OUTPUT_HANDLE) {
		if UTF8Console() {
			t.Fatal("UTF8Console prepared a console although stdin or stdout is a console")
		}
		t.Skip("stdin or stdout is a console")
	}
	if !UTF8Console() {
		t.Fatalf("UTF8Console: %v", prepareConsole())
	}
	k := windows.NewLazySystemDLL("kernel32.dll")
	in, _, _ := k.NewProc("GetConsoleCP").Call()
	out, _, _ := k.NewProc("GetConsoleOutputCP").Call()
	wnd, _, _ := k.NewProc("GetConsoleWindow").Call()
	if in != cpUTF8 || out != cpUTF8 || wnd != 0 {
		t.Errorf("console: input code page %d, output code page %d, window %#x", in, out, wnd)
	}
	// The console is host-shell's own: its handle is a console, a pipe is not.
	h, err := windows.CreateFile(windows.StringToUTF16Ptr("CONOUT$"), windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatalf("open CONOUT$: %v", err)
	}
	defer windows.CloseHandle(h)
	var r, w windows.Handle
	if err := windows.CreatePipe(&r, &w, nil, 0); err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(r)
	defer windows.CloseHandle(w)
	if !isConsoleHandle(h) || isConsoleHandle(w) {
		t.Errorf("isConsoleHandle: console %v, pipe %v", isConsoleHandle(h), isConsoleHandle(w))
	}
}
