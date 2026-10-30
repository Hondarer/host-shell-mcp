package tools

import (
	"fmt"
	"path"
	"runtime"
	"strings"
)

// glob is a pattern matched against file names and paths.
//
// A pattern without "/" matches the base name; one with "/" matches the path relative to the start ("/" separated).
// "**" matches zero or more directories and {a,b} matches any of the alternatives.
type glob struct {
	patterns []string
	byPath   bool
}

// compileGlob validates the pattern and builds a glob. An empty pattern matches everything.
func compileGlob(pattern string) (*glob, error) {
	if pattern == "" {
		return nil, nil
	}
	p := pattern
	if runtime.GOOS == "windows" {
		// Accept Windows path separators and ignore case.
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

// match reports whether the relative path rel ("/" separated) and the base name name match.
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

// matchSegments matches path element by element. "**" matches zero or more elements.
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

// expandBraces expands {a,b}. Nesting is not supported.
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
