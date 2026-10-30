package tools

import (
	"fmt"
	"path"
	"runtime"
	"strings"
)

// glob はファイルの名前やパスとの照合に使うパターンです。
//
// パターンに "/" を含まない場合は基本名と、含む場合は起点からの相対パス ("/" 区切り) と照合します。
// "**" は 0 個以上のディレクトリに、{a,b} はいずれかの文字列に一致します。
type glob struct {
	patterns []string
	byPath   bool
}

// compileGlob はパターンを検査して glob を作ります。空のパターンはすべてに一致します。
func compileGlob(pattern string) (*glob, error) {
	if pattern == "" {
		return nil, nil
	}
	p := pattern
	if runtime.GOOS == "windows" {
		// Windows のパスの区切りを受け付け、大文字と小文字を区別しない。
		p = strings.ToLower(strings.ReplaceAll(p, `\`, "/"))
	}
	p = strings.TrimPrefix(p, "./")
	g := &glob{byPath: strings.Contains(p, "/")}
	for _, e := range expandBraces(p) {
		for _, seg := range strings.Split(e, "/") {
			if _, err := path.Match(seg, ""); err != nil {
				return nil, fmt.Errorf("invalid glob %q: %w", pattern, err)
			}
		}
		g.patterns = append(g.patterns, e)
	}
	return g, nil
}

// match は相対パス rel ("/" 区切り) と基本名 name が一致するかを返します。
func (g *glob) match(rel, name string) bool {
	if g == nil {
		return true
	}
	target := name
	if g.byPath {
		target = rel
	}
	if runtime.GOOS == "windows" {
		target = strings.ToLower(target)
	}
	for _, p := range g.patterns {
		if g.byPath {
			if matchSegments(strings.Split(p, "/"), strings.Split(target, "/")) {
				return true
			}
		} else if ok, _ := path.Match(p, target); ok {
			return true
		}
	}
	return false
}

// matchSegments はパスの要素ごとに照合します。"**" は 0 個以上の要素に一致します。
func matchSegments(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(segs); i++ {
				if matchSegments(pat[1:], segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], segs[0]); !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}

// expandBraces は {a,b} を展開します。入れ子には対応しません。
func expandBraces(p string) []string {
	open := strings.IndexByte(p, '{')
	if open < 0 {
		return []string{p}
	}
	end := strings.IndexByte(p[open:], '}')
	if end < 0 {
		return []string{p}
	}
	end += open
	var out []string
	for _, alt := range strings.Split(p[open+1:end], ",") {
		out = append(out, expandBraces(p[:open]+alt+p[end+1:])...)
	}
	return out
}
