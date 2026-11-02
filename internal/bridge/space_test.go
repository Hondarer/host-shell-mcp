package bridge

import (
	"testing"

	"github.com/hondarer/host-shell-mcp/internal/wire"
)

func TestPushBytes(t *testing.T) {
	files := []localFile{
		{rel: "new", size: 100},
		{rel: "grown", size: 50},
		{rel: "same", size: 30},
		{rel: "same2", size: 70},
		{rel: "link", size: 999, link: true},
	}
	remote := map[string]wire.ManifestEntry{
		"grown": {Path: "grown", Size: 10},
		"same":  {Path: "same", Size: 30},
		"same2": {Path: "same2", Size: 70},
	}
	// new and grown are written in full; the largest same-size file may be replaced beside itself.
	if got := pushBytes(files, remote); got != 100+50+70 {
		t.Errorf("pushBytes = %d, want 220", got)
	}
}

func TestRemoteParent(t *testing.T) {
	for p, want := range map[string]string{
		"/srv/app/dist":   "/srv/app",
		"/srv":            "/",
		"/":               "/",
		`C:\Users\me\kit`: `C:\Users\me`,
		`C:\Users`:        `C:\`,
		"relative":        "",
		"dir/sub":         "dir",
	} {
		if got := remoteParent(p); got != want {
			t.Errorf("remoteParent(%q) = %q, want %q", p, got, want)
		}
	}
}
