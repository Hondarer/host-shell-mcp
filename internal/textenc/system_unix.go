//go:build !windows

package textenc

import (
	"os"
	"strings"
)

// Console は、コマンドの入出力に使われる接続先の既定の文字コードを返します。
// Linux ではロケール (LC_ALL、LC_CTYPE、LANG) の文字コードです。
func Console() Encoding { return locale() }

// File は、テキスト ファイルに使われる接続先の既定の文字コードを返します。
// Linux ではロケールの文字コードです。
func File() Encoding { return locale() }

func locale() Encoding {
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		v := os.Getenv(k)
		if v == "" {
			continue
		}
		// 例: ja_JP.UTF-8、ja_JP.eucJP、ja_JP.SJIS@euro
		_, cs, ok := strings.Cut(v, ".")
		if !ok {
			return UTF8 // C、POSIX など。ASCII は UTF-8 の部分集合として扱う。
		}
		cs, _, _ = strings.Cut(cs, "@")
		if e, err := Lookup(cs); err == nil {
			return e
		}
		return UTF8
	}
	return UTF8
}
