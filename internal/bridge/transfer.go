package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/policy"
	"github.com/hondarer/host-shell-mcp/internal/tools"
)

const transferInstructions = `This connection goes through host-shell bridge on your local machine.
- transfer_push copies a local file or directory to the remote host; transfer_pull copies a remote file or directory to this machine.
- Use them instead of reading a file with fs_read and writing it with fs_write: the contents are sent directly, are verified by SHA-256, and do not use your context. Unchanged files are skipped, so they also work for syncing.
- local_path is on your local machine (relative to the agent's working directory); remote_path is on the remote host.`

// partSuffix is appended to the name of a file being sent. The file is renamed after it is sent and verified.
const partSuffix = ".host-shell-part"

// maxListed caps the number of files listed in the result.
const maxListed = 500

// transfer provides the tools that send files between the local machine and the target.
type transfer struct {
	remote *mcp.ClientSession
	local  *policy.Policy
	chunk  int
}

func (t *transfer) init(ctx context.Context) error {
	t.chunk = tools.TransferChunkBytes
	need := map[string]bool{"fs_read": false, "fs_write": false, "fs_stat": false, "fs_manifest": false, "fs_move": false, "fs_remove": false}
	for tool, err := range t.remote.Tools(ctx, nil) {
		if err != nil {
			return err
		}
		if _, ok := need[tool.Name]; ok {
			need[tool.Name] = true
		}
	}
	var missing []string
	for n, ok := range need {
		if !ok {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return fmt.Errorf("the remote host-shell does not expose %s (needed for transfers; enable the files group or run bridge with --no-transfer)", strings.Join(missing, ", "))
	}
	return nil
}

func (t *transfer) register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:  "transfer_push",
		Title: "Upload to remote",
		Description: "Copy a local file or directory tree to the remote host, directly and verified by SHA-256, without passing the contents through the conversation. " +
			"Files that are already identical on the remote host are skipped, so it also syncs. remote_path is the full target path " +
			"(for a single file, an existing remote directory or a path ending in / receives the file inside it). Existing different files are replaced only with overwrite. " +
			"Executable permissions are kept. dry_run lists what would be sent.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(true), OpenWorldHint: ptr(false)},
	}, t.push)
	mcp.AddTool(srv, &mcp.Tool{
		Name:  "transfer_pull",
		Title: "Download from remote",
		Description: "Copy a remote file or directory tree to the local machine, directly and verified by SHA-256, without passing the contents through the conversation. " +
			"Files that are already identical locally are skipped. local_path is the full target path " +
			"(for a single file, an existing local directory or a path ending in / receives the file inside it). Existing different files are replaced only with overwrite. dry_run lists what would be fetched.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(true), OpenWorldHint: ptr(false)},
	}, t.pull)
}

// TransferInput is the input of transfer_push and transfer_pull.
type TransferInput struct {
	LocalPath  string   `json:"local_path" jsonschema:"path on the local machine (absolute, or relative to the agent's working directory)"`
	RemotePath string   `json:"remote_path" jsonschema:"path on the remote host"`
	Overwrite  bool     `json:"overwrite,omitempty" jsonschema:"replace existing files whose contents differ (identical files are always skipped)"`
	Exclude    []string `json:"exclude,omitempty" jsonschema:"names (globs) of files or directories to skip anywhere in the tree, e.g. [\".git\", \"node_modules\", \"*.pyc\"]"`
	DryRun     bool     `json:"dry_run,omitempty" jsonschema:"only report what would be transferred"`
	Parallel   int      `json:"parallel,omitempty" jsonschema:"files transferred at the same time, 1 to 16 (default 4). Lower it for a slow or busy link"`
}

// defaultParallel and maxParallel bound how many files are transferred at the same time.
const (
	defaultParallel = 4
	maxParallel     = 16
)

// parallelism returns the number of files to transfer at the same time.
func (in TransferInput) parallelism() (int, error) {
	switch {
	case in.Parallel == 0:
		return defaultParallel, nil
	case in.Parallel < 1 || in.Parallel > maxParallel:
		return 0, fmt.Errorf("parallel must be 1 to %d: %d", maxParallel, in.Parallel)
	}
	return in.Parallel, nil
}

// forEach runs fn for the indexes 0..n-1, at most parallel at a time, and stops starting new ones
// when ctx ends. Results are kept by index, so their order does not depend on timing.
func forEach(ctx context.Context, n, parallel int, fn func(i int)) error {
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			fn(i)
		}()
	}
	wg.Wait()
	return ctx.Err()
}

// FileResult is the result for one file.
type FileResult struct {
	Path   string `json:"path" jsonschema:"path relative to the source"`
	Bytes  int64  `json:"bytes"`
	Status string `json:"status" jsonschema:"sent, unchanged, exists (differs and overwrite is not set), would-send (dry_run), skipped (not a regular file) or error"`
	Error  string `json:"error,omitempty"`
}

// TransferOutput is the result of a transfer.
type TransferOutput struct {
	Source      string       `json:"source"`
	Destination string       `json:"destination"`
	Transferred int          `json:"transferred" jsonschema:"files sent (or that would be sent with dry_run)"`
	Unchanged   int          `json:"unchanged"`
	Failed      int          `json:"failed" jsonschema:"files with status exists or error"`
	Bytes       int64        `json:"bytes" jsonschema:"bytes transferred"`
	Files       []FileResult `json:"files" jsonschema:"per-file results (unchanged files are omitted when there are many)"`
	Truncated   bool         `json:"truncated,omitempty" jsonschema:"the file list was shortened"`
	DurationMs  int64        `json:"duration_ms"`
}

func (o *TransferOutput) add(r FileResult) {
	switch r.Status {
	case "sent", "would-send":
		o.Transferred++
		if r.Status == "sent" {
			o.Bytes += r.Bytes
		}
	case "unchanged":
		o.Unchanged++
	case "exists", "error":
		o.Failed++
	}
	o.Files = append(o.Files, r)
}

// finish drops unchanged files first when the list is too long.
func (o *TransferOutput) finish(start time.Time) {
	o.DurationMs = time.Since(start).Milliseconds()
	if len(o.Files) > maxListed {
		o.Files = slices.DeleteFunc(o.Files, func(r FileResult) bool { return r.Status == "unchanged" })
		o.Truncated = true
	}
	if len(o.Files) > maxListed {
		o.Files = o.Files[:maxListed]
	}
	if o.Files == nil {
		o.Files = []FileResult{}
	}
}

// localFile is a local file.
type localFile struct {
	rel  string // path relative to the source (/ separated)
	full string
	size int64
	mode fs.FileMode
	link bool
}

// remoteEntry is one entry of fs_manifest.
type remoteEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Mode   string `json:"mode"`
	SHA256 string `json:"sha256"`
}

func excluded(rel string, patterns []string) bool {
	for _, el := range strings.Split(rel, "/") {
		for _, p := range patterns {
			if ok, _ := path.Match(p, el); ok {
				return true
			}
		}
	}
	return false
}

func (t *transfer) push(ctx context.Context, req *mcp.CallToolRequest, in TransferInput) (*mcp.CallToolResult, TransferOutput, error) {
	start := time.Now()
	if in.LocalPath == "" || in.RemotePath == "" {
		return nil, TransferOutput{}, errors.New("local_path and remote_path are required")
	}
	parallel, err := in.parallelism()
	if err != nil {
		return nil, TransferOutput{}, err
	}
	if err := t.local.CheckPath(in.LocalPath, false); err != nil {
		return nil, TransferOutput{}, err
	}
	info, err := os.Stat(in.LocalPath)
	if err != nil {
		return nil, TransferOutput{}, err
	}
	dest := in.RemotePath
	rstat, rexists, err := t.remoteStat(ctx, dest)
	if err != nil {
		return nil, TransferOutput{}, err
	}
	if !info.IsDir() && (strings.HasSuffix(dest, "/") || strings.HasSuffix(dest, `\`) || (rexists && rstat == "dir")) {
		dest = strings.TrimRight(dest, `/\`) + "/" + filepath.Base(in.LocalPath)
		if rstat, rexists, err = t.remoteStat(ctx, dest); err != nil {
			return nil, TransferOutput{}, err
		}
	}
	switch {
	case rexists && info.IsDir() && rstat != "dir":
		return nil, TransferOutput{}, fmt.Errorf("remote %s exists and is not a directory", dest)
	case rexists && !info.IsDir() && rstat != "file":
		return nil, TransferOutput{}, fmt.Errorf("remote %s exists and is not a file", dest)
	}

	// Files to send
	var files []localFile
	if info.IsDir() {
		err = filepath.WalkDir(in.LocalPath, func(p string, de fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(in.LocalPath, p)
			rel = filepath.ToSlash(rel)
			if rel == "." {
				return nil
			}
			if excluded(rel, in.Exclude) {
				if de.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if de.IsDir() {
				return nil
			}
			fi, err := de.Info()
			if err != nil {
				return err
			}
			files = append(files, localFile{rel: rel, full: p, size: fi.Size(), mode: fi.Mode(), link: !fi.Mode().IsRegular()})
			return nil
		})
		if err != nil {
			return nil, TransferOutput{}, err
		}
	} else {
		files = []localFile{{rel: filepath.Base(in.LocalPath), full: in.LocalPath, size: info.Size(), mode: info.Mode()}}
	}

	// What the target already has
	remote := map[string]remoteEntry{}
	if rexists {
		entries, err := t.manifest(ctx, dest)
		if err != nil {
			return nil, TransferOutput{}, err
		}
		for _, e := range entries {
			if !info.IsDir() {
				e.Path = filepath.Base(in.LocalPath)
			}
			remote[e.Path] = e
		}
	}

	out := TransferOutput{Source: absLocal(in.LocalPath), Destination: dest}
	var total int64
	var done atomic.Int64
	for _, f := range files {
		total += f.size
	}
	pr := newProgress(ctx, req)
	results := make([]FileResult, len(files))
	err = forEach(ctx, len(files), parallel, func(i int) {
		f := files[i]
		r := FileResult{Path: f.rel, Bytes: f.size}
		defer func() { results[i] = r }()
		target := dest
		if info.IsDir() {
			target = strings.TrimRight(dest, `/\`) + "/" + f.rel
		}
		if f.link {
			r.Status = "skipped"
			return
		}
		sum, err := hashLocal(f.full)
		if err != nil {
			r.Status, r.Error = "error", err.Error()
			return
		}
		e, exists := remote[f.rel]
		switch {
		case exists && e.SHA256 == sum:
			r.Status = "unchanged"
		case exists && !in.Overwrite:
			r.Status, r.Error = "exists", "differs from the local file; set overwrite to replace it"
		case in.DryRun:
			r.Status = "would-send"
		default:
			if err := t.sendFile(ctx, f, target, sum, func(n int64) {
				pr.report(float64(done.Load()+n), float64(total), fmt.Sprintf("sending %s", f.rel))
			}); err != nil {
				r.Status, r.Error = "error", err.Error()
			} else {
				r.Status = "sent"
			}
		}
		done.Add(f.size)
	})
	if err != nil {
		return nil, TransferOutput{}, err
	}
	for _, r := range results {
		out.add(r)
	}
	out.finish(start)
	return nil, out, nil
}

// sendFile sends one file in chunks, verifies it and renames it to its final name.
func (t *transfer) sendFile(ctx context.Context, f localFile, target, sum string, onProgress func(int64)) error {
	src, err := os.Open(f.full)
	if err != nil {
		return err
	}
	defer src.Close()
	part := target + partSuffix
	buf := make([]byte, t.chunk)
	var off int64
	for first := true; ; first = false {
		n, rerr := io.ReadFull(src, buf)
		if n > 0 || first {
			args := map[string]any{"path": part, "content": base64.StdEncoding.EncodeToString(buf[:n]), "encoding": "base64"}
			if first {
				args["mode"] = "overwrite"
				args["make_dirs"] = true
				if runtime.GOOS != "windows" {
					args["perm"] = fmt.Sprintf("%04o", f.mode.Perm())
				}
			} else {
				args["offset"] = off
			}
			if err := callRemote(ctx, t.remote, "fs_write", args, nil); err != nil {
				t.cleanup(part)
				return err
			}
			off += int64(n)
			onProgress(off)
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			t.cleanup(part)
			return rerr
		}
	}
	entries, err := t.manifest(ctx, part)
	if err != nil || len(entries) != 1 {
		t.cleanup(part)
		return fmt.Errorf("verify: %v", err)
	}
	if entries[0].SHA256 != sum {
		t.cleanup(part)
		return errors.New("verify: SHA-256 of the remote copy does not match (the file may have changed while sending)")
	}
	if err := callRemote(ctx, t.remote, "fs_move", map[string]any{"source": part, "destination": target, "overwrite": true}, nil); err != nil {
		t.cleanup(part)
		return err
	}
	return nil
}

func (t *transfer) cleanup(remotePath string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = callRemote(ctx, t.remote, "fs_remove", map[string]any{"path": remotePath, "missing_ok": true}, nil)
}

func (t *transfer) pull(ctx context.Context, req *mcp.CallToolRequest, in TransferInput) (*mcp.CallToolResult, TransferOutput, error) {
	start := time.Now()
	if in.LocalPath == "" || in.RemotePath == "" {
		return nil, TransferOutput{}, errors.New("local_path and remote_path are required")
	}
	parallel, err := in.parallelism()
	if err != nil {
		return nil, TransferOutput{}, err
	}
	rstat, rexists, err := t.remoteStat(ctx, in.RemotePath)
	if err != nil {
		return nil, TransferOutput{}, err
	}
	if !rexists {
		return nil, TransferOutput{}, fmt.Errorf("remote %s does not exist", in.RemotePath)
	}
	if rstat != "file" && rstat != "dir" {
		return nil, TransferOutput{}, fmt.Errorf("remote %s is a %s, not a file or directory", in.RemotePath, rstat)
	}
	isDir := rstat == "dir"
	dest := in.LocalPath
	if !isDir {
		if fi, err := os.Stat(dest); (err == nil && fi.IsDir()) || strings.HasSuffix(dest, "/") || strings.HasSuffix(dest, string(filepath.Separator)) {
			dest = filepath.Join(dest, path.Base(filepath.ToSlash(in.RemotePath)))
		}
	}
	if err := t.local.CheckPath(dest, true); err != nil {
		return nil, TransferOutput{}, err
	}
	if fi, err := os.Stat(dest); err == nil && fi.IsDir() != isDir {
		return nil, TransferOutput{}, fmt.Errorf("local %s exists and is not a %s", dest, rstat)
	}

	entries, err := t.manifest(ctx, in.RemotePath)
	if err != nil {
		return nil, TransferOutput{}, err
	}
	out := TransferOutput{Source: in.RemotePath, Destination: absLocal(dest)}
	if isDir {
		entries = slices.DeleteFunc(entries, func(e remoteEntry) bool { return excluded(e.Path, in.Exclude) })
	}
	var total int64
	var done atomic.Int64
	for _, e := range entries {
		total += e.Size
	}
	pr := newProgress(ctx, req)
	results := make([]FileResult, len(entries))
	err = forEach(ctx, len(entries), parallel, func(i int) {
		e := entries[i]
		r := FileResult{Path: e.Path, Bytes: e.Size}
		defer func() { results[i] = r }()
		src, target := in.RemotePath, dest
		if isDir {
			src = strings.TrimRight(in.RemotePath, `/\`) + "/" + e.Path
			target = filepath.Join(dest, filepath.FromSlash(e.Path))
		}
		local, lerr := hashLocal(target)
		exists := lerr == nil
		switch {
		case exists && local == e.SHA256:
			r.Status = "unchanged"
		case exists && !in.Overwrite:
			r.Status, r.Error = "exists", "differs from the remote file; set overwrite to replace it"
		case in.DryRun:
			r.Status = "would-send"
		default:
			if err := t.fetchFile(ctx, src, target, e, func(n int64) {
				pr.report(float64(done.Load()+n), float64(total), fmt.Sprintf("fetching %s", e.Path))
			}); err != nil {
				r.Status, r.Error = "error", err.Error()
			} else {
				r.Status = "sent"
			}
		}
		done.Add(e.Size)
	})
	if err != nil {
		return nil, TransferOutput{}, err
	}
	for _, r := range results {
		out.add(r)
	}
	out.finish(start)
	return nil, out, nil
}

// fetchFile receives one file in chunks, verifies it and renames it to its final name.
func (t *transfer) fetchFile(ctx context.Context, src, target string, e remoteEntry, onProgress func(int64)) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	part := target + partSuffix
	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	fail := func(err error) error {
		f.Close()
		os.Remove(part)
		return err
	}
	h := sha256.New()
	var off int64
	for {
		var r struct {
			Content    string `json:"content"`
			EOF        bool   `json:"eof"`
			NextOffset int64  `json:"next_offset"`
		}
		if err := callRemote(ctx, t.remote, "fs_read", map[string]any{"path": src, "encoding": "base64", "offset": off, "length": t.chunk}, &r); err != nil {
			return fail(err)
		}
		b, err := base64.StdEncoding.DecodeString(r.Content)
		if err != nil {
			return fail(err)
		}
		if _, err := f.Write(b); err != nil {
			return fail(err)
		}
		h.Write(b)
		off += int64(len(b))
		onProgress(off)
		if r.EOF || len(b) == 0 {
			break
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(part)
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != e.SHA256 {
		os.Remove(part)
		return errors.New("verify: SHA-256 of the local copy does not match (the remote file may have changed while fetching)")
	}
	if runtime.GOOS != "windows" {
		var perm uint32
		if _, err := fmt.Sscanf(e.Mode, "%o", &perm); err == nil && perm != 0 {
			os.Chmod(part, fs.FileMode(perm))
		} else {
			os.Chmod(part, 0o644)
		}
	}
	if err := os.Rename(part, target); err != nil {
		os.Remove(part)
		return err
	}
	return nil
}

// remoteStat returns the kind of a path on the target (file, dir, symlink, other) and whether it exists.
func (t *transfer) remoteStat(ctx context.Context, p string) (string, bool, error) {
	var st struct {
		Entry struct {
			Type string `json:"type"`
		} `json:"entry"`
	}
	err := callRemote(ctx, t.remote, "fs_stat", map[string]any{"path": strings.TrimRight(p, `/\`)}, &st)
	var re *remoteError
	if errors.As(err, &re) && notExist(re.msg) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return st.Entry.Type, true, nil
}

// notExist reports whether an error from the target means the path does not exist.
func notExist(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "no such file") || strings.Contains(m, "cannot find the") || strings.Contains(m, "does not exist")
}

func (t *transfer) manifest(ctx context.Context, p string) ([]remoteEntry, error) {
	var m struct {
		Files     []remoteEntry `json:"files"`
		Truncated bool          `json:"truncated"`
		Skipped   int           `json:"skipped"`
	}
	if err := callRemote(ctx, t.remote, "fs_manifest", map[string]any{"path": p, "exclude_dirs": []string{}, "max_files": 1000000}, &m); err != nil {
		return nil, err
	}
	if m.Truncated {
		return nil, errors.New("too many files in the remote tree")
	}
	return m.Files, nil
}

func hashLocal(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", p)
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func absLocal(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// progress reports transfer progress when the agent asked for it.
type progress struct {
	ctx   context.Context
	ss    *mcp.ServerSession
	token any
	mu    sync.Mutex // files are transferred in parallel
	last  time.Time
}

func newProgress(ctx context.Context, req *mcp.CallToolRequest) *progress {
	if req == nil || req.Params == nil || req.Session == nil || req.Params.GetProgressToken() == nil {
		return nil
	}
	return &progress{ctx: ctx, ss: req.Session, token: req.Params.GetProgressToken()}
}

func (p *progress) report(value, total float64, msg string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Since(p.last) < time.Second {
		return
	}
	p.last = time.Now()
	_ = p.ss.NotifyProgress(p.ctx, &mcp.ProgressNotificationParams{ProgressToken: p.token, Progress: value, Total: total, Message: msg})
}

func remarshal(in, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func ptr[T any](v T) *T { return &v }
