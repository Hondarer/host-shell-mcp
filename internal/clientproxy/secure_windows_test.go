package clientproxy

import (
	"golang.org/x/sys/windows"
	"testing"
)

func TestCredentialDACL(t *testing.T) {
	target, e := NewTarget()
	if e != nil {
		t.Fatal(e)
	}
	defer target.Close()
	sd, e := windows.GetNamedSecurityInfo(target.File, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if e != nil {
		t.Fatal(e)
	}
	acl, _, e := sd.DACL()
	if e != nil {
		t.Fatal(e)
	}
	if acl == nil {
		t.Fatal("unrestricted DACL")
	}
	if acl.AceCount != 1 {
		t.Fatalf("expected one user ACE, got %d", acl.AceCount)
	}
}
