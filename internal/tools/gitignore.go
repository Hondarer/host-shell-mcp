package tools

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// gitignore decides which files git ignores, for the search tools.
//
// It reads the .gitignore files of the search root and of every directory below it, and, inside a
// repository, those of the directories from the repository root down to the search root and the
// repository's .git/info/exclude. git's global settings (core.excludesFile) are not read.
// Rules are applied in git's order: .git/info/exclude first, then .gitignore from the top
// directory down, the last matching rule deciding. Directories that are ignored are not entered,
// so a negated rule cannot bring back a file inside them, as in git.
type gitignore struct {
	top   string                  // repository root, or the search root outside a repository
	extra []ignoreRule            // rules of .git/info/exclude (based at top)
	rules map[string][]ignoreRule // .gitignore rules by directory, loaded on demand
}

// ignoreRule is one line of a .gitignore file.
type ignoreRule struct {
	base     string   // directory of the file that has the rule
	segs     []string // pattern split at "/" (anchored rules), or the single name pattern
	anchored bool     // the pattern has a "/" before its end: match the path relative to base
	negate   bool     // "!pattern"
	dirOnly  bool     // "pattern/"
}

// newGitignore prepares the rules for a search from root, an existing directory.
func newGitignore(root string) *gitignore {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil
	}
	g := &gitignore{top: abs, rules: map[string][]ignoreRule{}}
	for d := abs; ; {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			g.top = d
			g.extra = parseIgnoreFile(filepath.Join(d, ".git", "info", "exclude"), d)
			break
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return g
}

// ignored reports whether the entry at p (an absolute path below top) is ignored.
func (g *gitignore) ignored(p string, isDir bool) bool {
	rel, err := filepath.Rel(g.top, p)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	// The directories whose .gitignore apply, from top down to the parent of p.
	dirs := []string{g.top}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := 1; i < len(parts); i++ {
		dirs = append(dirs, filepath.Join(g.top, filepath.FromSlash(strings.Join(parts[:i], "/"))))
	}
	ignored := false
	check := func(rules []ignoreRule) {
		for _, r := range rules {
			if r.matches(p, isDir) {
				ignored = !r.negate
			}
		}
	}
	check(g.extra)
	for _, d := range dirs {
		rules, ok := g.rules[d]
		if !ok {
			rules = parseIgnoreFile(filepath.Join(d, ".gitignore"), d)
			g.rules[d] = rules
		}
		check(rules)
	}
	return ignored
}

func (r ignoreRule) matches(p string, isDir bool) bool {
	if r.dirOnly && !isDir {
		return false
	}
	rel, err := filepath.Rel(r.base, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return false
	}
	rel = filepath.ToSlash(rel)
	if runtime.GOOS == "windows" {
		rel = strings.ToLower(rel) // git on Windows ignores case (core.ignorecase)
	}
	if r.anchored {
		return matchSegments(r.segs, strings.Split(rel, "/"))
	}
	ok, _ := path.Match(r.segs[0], path.Base(rel))
	return ok
}

// parseIgnoreFile reads the rules of a .gitignore (or info/exclude) file. A missing file has none.
func parseIgnoreFile(file, base string) []ignoreRule {
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	var rules []ignoreRule
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if r, ok := parseIgnoreLine(sc.Text(), base); ok {
			rules = append(rules, r)
		}
	}
	return rules
}

// parseIgnoreLine parses one line of a .gitignore file. ok is false for blank lines, comments and
// patterns that cannot be used.
func parseIgnoreLine(line, base string) (ignoreRule, bool) {
	line = strings.TrimSuffix(line, "\r")
	// Trailing spaces are dropped unless escaped with a backslash.
	trimmed := strings.TrimRight(line, " ")
	if len(trimmed) < len(line) && strings.HasSuffix(trimmed, `\`) {
		trimmed += " "
	}
	line = trimmed
	if line == "" || strings.HasPrefix(line, "#") {
		return ignoreRule{}, false
	}
	r := ignoreRule{base: base}
	if strings.HasPrefix(line, "!") {
		r.negate, line = true, line[1:]
	}
	if strings.HasSuffix(line, "/") {
		r.dirOnly, line = true, strings.TrimRight(line, "/")
	}
	if line == "" {
		return ignoreRule{}, false
	}
	// "[!...]" in git is "[^...]" for path.Match.
	line = strings.ReplaceAll(line, "[!", "[^")
	if runtime.GOOS == "windows" {
		line = strings.ToLower(line)
	}
	if strings.Contains(line, "/") {
		r.anchored = true
		r.segs = strings.Split(strings.TrimPrefix(line, "/"), "/")
	} else {
		r.segs = []string{line}
	}
	for _, s := range r.segs {
		if _, err := path.Match(s, ""); err != nil {
			return ignoreRule{}, false
		}
	}
	return r, true
}
