//go:build !windows

package textenc

import (
	"os"
	"strings"
)

// Console returns the target's default command I/O encoding from the locale (LC_ALL, LC_CTYPE, LANG).
func Console() Encoding { return locale() }

// File returns the target's default text file encoding from the locale.
func File() Encoding { return locale() }

func locale() Encoding {
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		v := os.Getenv(k)
		if v == "" {
			continue
		}
		// e.g. ja_JP.UTF-8, ja_JP.eucJP, ja_JP.SJIS@euro
		_, cs, ok := strings.Cut(v, ".")
		if !ok {
			return UTF8 // C, POSIX, etc. (ASCII is a subset of UTF-8)
		}
		cs, _, _ = strings.Cut(cs, "@")
		if e, err := Lookup(cs); err == nil {
			return e
		}
		return UTF8
	}
	return UTF8
}
