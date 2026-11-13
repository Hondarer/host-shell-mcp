package sshpass

import (
	"encoding/base64"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// dpapiPrefix marks a password file whose content is a DPAPI blob for the current user.
const dpapiPrefix = "dpapi:"

// entropy binds the DPAPI blobs to this use, so other programs of the same user that call
// CryptUnprotectData without it cannot decrypt them by accident.
var entropy = []byte("host-shell ssh-pass")

func blob(b []byte) *windows.DataBlob {
	if len(b) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

// protect encrypts the password with DPAPI for the current user.
func protect(password string) (string, error) {
	var out windows.DataBlob
	if err := windows.CryptProtectData(blob([]byte(password)), nil, blob(entropy), 0, nil,
		windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return dpapiPrefix + base64.StdEncoding.EncodeToString(unsafe.Slice(out.Data, out.Size)), nil
}

// unprotect decrypts a DPAPI blob written by protect. A file without the prefix is plain text.
func unprotect(data string) (string, error) {
	enc, ok := strings.CutPrefix(data, dpapiPrefix)
	if !ok {
		return data, nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(blob(raw), nil, blob(entropy), 0, nil,
		windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return string(unsafe.Slice(out.Data, out.Size)), nil
}

// secureFile gives the file a protected DACL that allows only the current user, so it passes the
// permission check wherever the password directory is (its parent's ACL is not inherited).
func secureFile(path string) error {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return err
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}
