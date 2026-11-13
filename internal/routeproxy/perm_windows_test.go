package routeproxy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func currentUser(t *testing.T) string {
	t.Helper()
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return tu.User.Sid.String()
}

// setDACL replaces the DACL of path with the one in sddl and blocks inheritance from the parent.
func setDACL(t *testing.T, path, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatalf("%s: %v", sddl, err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
}

// TestPasswordFilePermissions checks the rule of Windows OpenSSH for ~/.ssh/config: trustees
// other than the user, SYSTEM, Administrators and TrustedInstaller may read but not write.
func TestPasswordFilePermissions(t *testing.T) {
	me := currentUser(t)
	for _, tt := range []struct {
		name string
		sddl string
		bad  string // the trustee named in the error, or "" when the file is accepted
	}{
		{"user only", "D:P(A;;FA;;;%s)", ""},
		{"user, SYSTEM and Administrators", "D:P(A;;FA;;;%s)(A;;FA;;;SY)(A;;FA;;;BA)", ""},
		{"Users may read", "D:P(A;;FA;;;%s)(A;;FR;;;BU)", ""},
		{"Everyone may read and execute", "D:P(A;;FA;;;%s)(A;;0x1200a9;;;WD)", ""},
		{"deny entries are not examined", "D:P(D;;FA;;;BU)(A;;FA;;;%s)", ""},
		{"Users may write", "D:P(A;;FA;;;%s)(A;;FW;;;BU)", "Users"},
		{"Everyone may delete", "D:P(A;;FA;;;%s)(A;;0x10000;;;WD)", "Everyone"},
		{"Authenticated Users may change the DACL", "D:P(A;;FA;;;%s)(A;;0x40000;;;AU)", "Authenticated Users"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "p")
			if err := os.WriteFile(path, []byte("pw\r\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			sddl := tt.sddl
			if strings.Contains(sddl, "%s") {
				sddl = fmt.Sprintf(sddl, me)
			}
			setDACL(t, path, sddl)
			_, err := readPasswordFile(path)
			if tt.bad == "" && err != nil {
				t.Fatalf("rejected: %v", err)
			}
			if tt.bad != "" && (!errors.Is(err, errBadPermissions) || !strings.Contains(err.Error(), tt.bad)) {
				t.Fatalf("err = %v, want a permission error naming %s", err, tt.bad)
			}
		})
	}
}

// TestPasswordFileInheritedACE checks that inherited entries count the same as explicit ones.
func TestPasswordFileInheritedACE(t *testing.T) {
	me := currentUser(t)
	for _, tt := range []struct {
		rights string
		ok     bool
	}{
		{"FR", true},
		{"FW", false},
	} {
		dir := t.TempDir()
		setDACL(t, dir, fmt.Sprintf("D:P(A;OICI;FA;;;%s)(A;OICI;%s;;;BU)", me, tt.rights))
		path := filepath.Join(dir, "p")
		if err := os.WriteFile(path, []byte("pw"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := readPasswordFile(path)
		if tt.ok != (err == nil) {
			t.Errorf("inherited %s for Users: err = %v", tt.rights, err)
		}
	}
}

// TestPasswordFileNullDACL checks that a file without a DACL (full access for everyone) is rejected.
func TestPasswordFileNullDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p")
	if err := os.WriteFile(path, []byte("pw"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := readPasswordFile(path); !errors.Is(err, errBadPermissions) {
		t.Fatalf("err = %v", err)
	}
}
