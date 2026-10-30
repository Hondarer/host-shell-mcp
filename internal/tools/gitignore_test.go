package tools

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// ignoreTree builds a repository with .gitignore rules of every kind and returns its root.
func ignoreTree(t *testing.T) string {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".git/info/exclude", "excluded-by-info.txt\n")
	write(".gitignore", strings.Join([]string{
		"# a comment",
		"",
		"*.log",
		"!keep.log",
		"build/",
		"/root-only.txt",
		"docs/**/*.tmp",
		`\#hash.txt`,
		"dir-only/",
		"a/**/z.txt",
		"[abc]x.dat",
		"[!d]y.dat",
		"node_modules",
		"deep/*/mid.txt",
		"CRLF.txt\r",
	}, "\n")+"\n")
	write("src/.gitignore", "!*.log\nsecret.txt\n")
	for _, f := range []string{
		"app.log", "keep.log", "src/app.log", "src/sub/app.log", "build/out.o", "build/keep.log",
		"src/build/x.o", "root-only.txt", "src/root-only.txt", "docs/a/b/c.tmp", "docs/c.tmp",
		"docs/c.txt", "#hash.txt", "dir-only", "sub/dir-only/f.txt", "a/z.txt", "a/b/c/z.txt", "b/z.txt",
		"ax.dat", "dx.dat", "ey.dat", "dy.dat", "src/secret.txt", "secret.txt", "excluded-by-info.txt",
		"node_modules/pkg/index.js", "src/node_modules", "deep/x/mid.txt", "deep/x/y/mid.txt", "CRLF.txt",
		"plain.txt",
	} {
		write(f, "x")
	}
	return root
}

// walked returns the files walkFiles visits with .gitignore, relative to root.
func walked(t *testing.T, root string) []string {
	var got []string
	_, err := walkFiles(context.Background(), root, nil, true, func(path, rel string, d fs.DirEntry) error {
		if !d.IsDir() {
			got = append(got, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	return got
}

// TestGitignoreLikeGit compares the files the search visits with the untracked files git does not ignore.
func TestGitignoreLikeGit(t *testing.T) {
	root := ignoreTree(t)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	if out, err := exec.Command(git, "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	for _, dir := range []string{"", "src", "docs"} {
		cmd := exec.Command(git, "-c", "core.excludesFile="+os.DevNull, "ls-files", "--others", "--exclude-standard")
		cmd.Dir = filepath.Join(root, dir)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Fields(strings.ReplaceAll(string(out), "\"", ""))
		slices.Sort(want)
		if got := walked(t, filepath.Join(root, dir)); !slices.Equal(got, want) {
			t.Errorf("from %q:\n got  %v\n want %v", dir, got, want)
		}
	}
}

// TestGitignoreRules checks the expected result without git.
func TestGitignoreRules(t *testing.T) {
	root := ignoreTree(t)
	got := walked(t, root)
	// The same result as git (TestGitignoreLikeGit), for hosts without git.
	want := []string{
		".gitignore", "b/z.txt", "deep/x/y/mid.txt", "dir-only", "docs/c.txt", "dx.dat", "dy.dat",
		"keep.log", "plain.txt", "secret.txt", "src/.gitignore", "src/app.log", "src/root-only.txt",
		"src/sub/app.log",
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}
