package tools

import (
	"strings"
	"testing"
)

func TestExecSudoAuditArgsHidePassword(t *testing.T) {
	args := ExecSudoInput{Command: "id", Stdin: "secret stdin", Password: "secret password"}.AuditArgs().(ExecSudoInput)
	if args.Password != "<redacted>" || strings.Contains(args.Stdin, "secret") {
		t.Errorf("audit args = %+v", args)
	}
}
