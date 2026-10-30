package tools

import "testing"

func TestGlob(t *testing.T) {
	for _, c := range []struct {
		pattern, rel string
		want         bool
	}{
		{"*.go", "cmd/main.go", true},
		{"*.{yml,yaml}", "a/b.yaml", true},
		{"*.{yml,yaml}", "a/b.json", false},
		{"**/*.go", "main.go", true},
		{"**/*.go", "a/b/c.go", true},
		{"src/**/*.ts", "src/x.ts", true},
		{"src/**/*.ts", "src/a/b/x.ts", true},
		{"src/**/*.ts", "lib/src/x.ts", false},
		{"cmd/*/main.go", "cmd/host-shell/main.go", true},
		{"cmd/*/main.go", "cmd/a/b/main.go", false},
		{"**/test/*.py", "pkg/test/a.py", true},
		{"./docs/*.md", "docs/a.md", true},
	} {
		g, err := compileGlob(c.pattern)
		if err != nil {
			t.Fatal(err)
		}
		name := c.rel[len(c.rel)-len(baseName(c.rel)):]
		if got := g.match(c.rel, name); got != c.want {
			t.Errorf("%q vs %q = %v, want %v", c.pattern, c.rel, got, c.want)
		}
	}
	if _, err := compileGlob("[a"); err == nil {
		t.Error("invalid glob should fail")
	}
}

func baseName(rel string) string {
	for i := len(rel) - 1; i >= 0; i-- {
		if rel[i] == '/' {
			return rel[i+1:]
		}
	}
	return rel
}

func TestReplaceTracked(t *testing.T) {
	text, spans := replaceTracked("a X b X c", "X", "YY", nil)
	if text != "a YY b YY c" || len(spans) != 2 || spans[1] != (span{7, 9}) {
		t.Fatalf("got %q %v", text, spans)
	}
	// Ranges recorded by an earlier replacement move with the shift caused by later replacements.
	text, spans = replaceTracked(text, "a", "AAA", spans)
	if text != "AAA YY b YY c" || spans[0] != (span{4, 6}) || spans[2] != (span{0, 3}) {
		t.Fatalf("got %q %v", text, spans)
	}
}

func TestFuzzyLine(t *testing.T) {
	text := "func f() {\n\treturn 1\n}\n"
	if got := fuzzyLine(text, "func f() {\n    return 1\n}"); got != 1 {
		t.Fatalf("fuzzyLine = %d", got)
	}
	if got := fuzzyLine(text, "return 2"); got != 0 {
		t.Fatalf("fuzzyLine = %d", got)
	}
}
