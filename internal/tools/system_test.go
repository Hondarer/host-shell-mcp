package tools

import (
	"strings"
	"testing"
)

func TestSudoRunAuditArgsHidePassword(t *testing.T) {
	args := SudoRunInput{Command: "id", Stdin: "secret stdin", Password: "secret password"}.AuditArgs().(SudoRunInput)
	if args.Password != "<redacted>" || strings.Contains(args.Stdin, "secret") {
		t.Errorf("audit args = %+v", args)
	}
}
