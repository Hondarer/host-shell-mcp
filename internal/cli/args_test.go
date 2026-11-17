package cli

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

var testSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"path":    map[string]any{"type": "string"},
		"count":   map[string]any{"type": "integer"},
		"ratio":   map[string]any{"type": "number"},
		"force":   map[string]any{"type": "boolean"},
		"argv":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"ports":   map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
		"env":     map[string]any{"type": "object"},
		"maybe":   map[string]any{"type": []any{"null", "integer"}},
		"anyting": map[string]any{},
	},
}

func TestBuildArgs(t *testing.T) {
	s, err := parseSchema(testSchema)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"a.txt": "line 1\nline 2\n", "env.json": `{"A":"1"}`, "bad.json": "x"}
	read := func(name string) ([]byte, error) {
		if v, ok := files[name]; ok {
			return []byte(v), nil
		}
		return nil, errors.New("no such file")
	}
	tests := []struct {
		base  map[string]any
		pairs []string
		want  map[string]any
		err   string
	}{
		{pairs: []string{"path=/tmp/a b", "count=3", "ratio=0.5", "force=true", "maybe=7"},
			want: map[string]any{"path": "/tmp/a b", "count": int64(3), "ratio": 0.5, "force": true, "maybe": int64(7)}},
		{pairs: []string{"path=a=b"}, want: map[string]any{"path": "a=b"}},
		{pairs: []string{"path="}, want: map[string]any{"path": ""}},
		{pairs: []string{"argv=sh", "argv=-c", "argv=echo [1]"}, want: map[string]any{"argv": []any{"sh", "-c", "echo [1]"}}},
		{pairs: []string{"ports=[80,443]", "env={\"A\":\"1\"}"}, want: map[string]any{"ports": []any{80.0, 443.0}, "env": map[string]any{"A": "1"}}},
		{pairs: []string{"path=@a.txt", "env=@env.json", "argv=@a.txt"}, want: map[string]any{"path": "line 1\nline 2\n", "env": map[string]any{"A": "1"}, "argv": []any{"line 1\nline 2\n"}}},
		{pairs: []string{"path=@@literal"}, want: map[string]any{"path": "@literal"}},
		{pairs: []string{"anyting=12", "maybe=1"}, want: map[string]any{"anyting": 12.0, "maybe": int64(1)}},
		{pairs: []string{"anyting=plain words"}, want: map[string]any{"anyting": "plain words"}},
		{base: map[string]any{"path": "x", "count": 1.0}, pairs: []string{"count=2"}, want: map[string]any{"path": "x", "count": int64(2)}},
		{pairs: []string{"bogus=1"}, err: "unknown argument \"bogus\"; the tool takes: anyting, argv, count"},
		{pairs: []string{"novalue"}, err: "not KEY=VALUE"},
		{pairs: []string{"=x"}, err: "not KEY=VALUE"},
		{pairs: []string{"count=x"}, err: "not an integer"},
		{pairs: []string{"ratio=x"}, err: "not a number"},
		{pairs: []string{"force=yes"}, err: "not true or false"},
		{pairs: []string{"env=x"}, err: "not JSON (object expected)"},
		{pairs: []string{"env=@bad.json"}, err: "the file is not JSON"},
		{pairs: []string{"path=@missing"}, err: "no such file"},
		{pairs: []string{"path=a", "path=b"}, err: "more than once"},
	}
	for _, tc := range tests {
		got, err := buildArgs(s, tc.base, tc.pairs, read)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%v: error %v, want %q", tc.pairs, err, tc.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%v: %v", tc.pairs, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%v: got %#v, want %#v", tc.pairs, got, tc.want)
		}
	}
}

func TestFirstSentence(t *testing.T) {
	for in, want := range map[string]string{
		"Read a file. Line mode: more.": "Read a file.",
		"No period":                     "No period",
		"Spans\n  lines. Next.":         "Spans lines.",
		"Version 1.2 is fine. Next.":    "Version 1.2 is fine.",
	} {
		if got := firstSentence(in); got != want {
			t.Errorf("firstSentence(%q) = %q, want %q", in, got, want)
		}
	}
}
