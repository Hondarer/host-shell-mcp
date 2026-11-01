package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/hondarer/host-shell-mcp/internal/textenc"
)

func TestGrepFileContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(path, []byte("a\nhit1\nb\nc\nhit2\nhit3\nd\ne\n"), 0o644)
	var got []Match
	opt := grepOptions{re: regexp.MustCompile("hit"), ch: textenc.Choice{Enc: textenc.UTF8}, before: 1, after: 1}
	if err := grepFile(path, opt, func(m Match) grepAction { got = append(got, m); return grepContinue }); err != nil {
		t.Fatal(err)
	}
	want := []struct {
		line          int
		before, after []string
	}{
		{2, []string{"a"}, []string{"b"}},
		{5, []string{"c"}, []string{"hit3"}}, // a following match also serves as trailing context
		{6, []string{"hit2"}, []string{"d"}},
	}
	if len(got) != len(want) {
		t.Fatalf("matches = %+v", got)
	}
	for i, w := range want {
		if got[i].Line != w.line || !slices.Equal(got[i].Before, w.before) || !slices.Equal(got[i].After, w.after) {
			t.Errorf("match %d = %+v, want line %d before %q after %q", i, got[i], w.line, w.before, w.after)
		}
	}

	// Without context, grepNextFile stops after the first match.
	got = nil
	opt.before, opt.after = 0, 0
	grepFile(path, opt, func(m Match) grepAction { got = append(got, m); return grepNextFile })
	if len(got) != 1 || got[0].Line != 2 {
		t.Errorf("first match only: %+v", got)
	}
}
