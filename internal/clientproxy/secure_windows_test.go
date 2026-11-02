package clientproxy

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestCredentialDACL(t *testing.T) {
	target, err := NewTarget()
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	sd, err := windows.GetNamedSecurityInfo(target.File, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if acl == nil {
		t.Fatal("unrestricted DACL")
	}
	if acl.AceCount != 1 {
		t.Fatalf("expected one user ACE, got %d", acl.AceCount)
	}
}
