package proc

import "testing"

func TestIsPowerShell(t *testing.T) {
	for _, c := range []struct {
		spec Spec
		def  string
		want bool
	}{
		{Spec{Command: "Get-Date"}, "powershell", true},
		{Spec{Command: "Get-Date"}, "cmd", false},
		{Spec{Command: "Get-Date", Shell: "pwsh"}, "cmd", true},
		{Spec{Command: "dir", Shell: "cmd"}, "powershell", false},
		{Spec{Command: "ls"}, "sh", false},
		{Spec{Argv: []string{"powershell"}}, "cmd", true},
		{Spec{Argv: []string{"PowerShell.EXE", "-Command", "x"}}, "cmd", true},
		{Spec{Argv: []string{`C:\Program Files\PowerShell\7\pwsh.exe`}}, "cmd", true},
		{Spec{Argv: []string{"/usr/bin/pwsh"}}, "sh", true},
		{Spec{Argv: []string{"git", "status"}}, "powershell", false},
		{Spec{Argv: []string{"pwsh-preview.exe"}}, "powershell", false},
	} {
		if got := IsPowerShell(c.spec, c.def); got != c.want {
			t.Errorf("IsPowerShell(%+v, %q) = %v, want %v", c.spec, c.def, got, c.want)
		}
	}
}
