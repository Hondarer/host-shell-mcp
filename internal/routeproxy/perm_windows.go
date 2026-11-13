package routeproxy

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// trustedInstallerSID is the service SID of NT SERVICE\TrustedInstaller, which OpenSSH accepts
// as an owner and trustee besides the user, Administrators and SYSTEM.
const trustedInstallerSID = "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"

// writeMask is the set of rights OpenSSH treats as write access (SSH_SECURE_WRITE_MASK in
// contrib/win32/win32compat/w32-sshfileperm.c).
const writeMask = windows.FILE_WRITE_DATA | windows.FILE_WRITE_ATTRIBUTES | windows.FILE_WRITE_EA |
	windows.FILE_APPEND_DATA | windows.WRITE_DAC | windows.WRITE_OWNER | windows.DELETE

// checkedOpen applies the check Windows OpenSSH uses for the user's ~/.ssh/config
// (check_secure_file_permission with read_ok set) and opens the password file:
//   - the owner is the current user, BUILTIN\Administrators, SYSTEM or TrustedInstaller;
//   - every access-allowed ACE for any other trustee grants no write right. Inherited ACEs count
//     the same; deny ACEs and other ACE types are not examined. A trustee whose SID differs but
//     whose account name, domain and type match the current user (SID history) is the user.
func checkedOpen(path string) (*os.File, error) {
	if err := checkPermissions(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open password file: %v", err)
	}
	return f, nil
}

func checkPermissions(path string) error {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("cannot identify the current user: %v", err)
	}
	user := tu.User.Sid
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("cannot open password file %s: %v", path, err)
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !owner.IsValid() {
		return fmt.Errorf("%w %s: cannot read its owner", errBadPermissions, path)
	}
	dacl, _, err := sd.DACL()
	if errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND) || (err == nil && dacl == nil) {
		return fmt.Errorf("%w %s: it has no access control list, so everyone may write to it", errBadPermissions, path)
	}
	if err != nil {
		return fmt.Errorf("%w %s: cannot read its access control list: %v", errBadPermissions, path, err)
	}
	ti, _ := windows.StringToSid(trustedInstallerSID)
	trusted := func(sid *windows.SID) bool {
		return sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) || sid.IsWellKnown(windows.WinLocalSystemSid) ||
			sid.Equals(user) || (ti != nil && sid.Equals(ti))
	}
	if !trusted(owner) {
		return fmt.Errorf("%w %s: it is owned by %s; the owner must be you, SYSTEM or Administrators", errBadPermissions, path, accountName(owner))
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return fmt.Errorf("%w %s: cannot read its access control list: %v", errBadPermissions, path, err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if trusted(sid) || uint32(ace.Mask)&writeMask == 0 || sameAccount(sid, user) {
			continue
		}
		return fmt.Errorf("%w %s: %s has write access; allow writing only to you, SYSTEM and Administrators", errBadPermissions, path, accountName(sid))
	}
	return nil
}

// accountName returns DOMAIN\name for sid, or the SID string when it cannot be resolved.
func accountName(sid *windows.SID) string {
	name, domain, _, err := sid.LookupAccount("")
	switch {
	case err != nil:
		return sid.String()
	case domain == "":
		return name
	default:
		return domain + `\` + name
	}
}

// sameAccount reports whether two SIDs resolve to the same account name, domain and type, as
// OpenSSH checks for SID history entries.
func sameAccount(a, b *windows.SID) bool {
	an, ad, at, err := a.LookupAccount("")
	if err != nil {
		return false
	}
	bn, bd, bt, err := b.LookupAccount("")
	if err != nil {
		return false
	}
	return strings.EqualFold(an, bn) && strings.EqualFold(ad, bd) && at == bt
}
