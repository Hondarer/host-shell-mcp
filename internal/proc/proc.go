// Package proc は Windows と Linux の差を吸収して子プロセスを起動します。
//
// 子プロセスの標準入力に host-shell 自身の標準入力 (MCP の通信路) を渡さないこと、
// 標準出力に host-shell 自身の標準出力を渡さないことが重要です。
package proc

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/hondarer/host-shell-mcp/internal/limit"
)

// Spec は起動するコマンドの指定です。Command と Argv はどちらか一方だけを指定します。
type Spec struct {
	// Command はシェルに渡すコマンド行です。
	Command string
	// Argv はシェルを介さずに起動する引数列です。Argv[0] がプログラムです。
	Argv []string
	// Shell は Command を解釈するシェルです。空の場合は OS の既定を使います。
	Shell string
	// Dir は作業ディレクトリです。
	Dir string
	// Env は既存の環境変数に追加、上書きする環境変数です。
	Env map[string]string
}

// waitDelay は、終了後も孫プロセスがパイプを握っている場合に待つ時間です。
const waitDelay = 2 * time.Second

// Command は spec から exec.Cmd を作ります。標準入出力は呼び出し側で設定します。
func Command(ctx context.Context, spec Spec, defaultShell string) (*exec.Cmd, error) {
	var cmd *exec.Cmd
	switch {
	case len(spec.Argv) > 0 && spec.Command != "":
		return nil, errors.New("command and argv are mutually exclusive")
	case len(spec.Argv) > 0:
		cmd = exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	case spec.Command != "":
		sh := spec.Shell
		if sh == "" {
			sh = defaultShell
		}
		var err error
		if cmd, err = shellCommand(ctx, sh, spec.Command); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("either command or argv is required")
	}
	cmd.Dir = spec.Dir
	if len(spec.Env) > 0 {
		cmd.Env = mergeEnv(os.Environ(), spec.Env)
	}
	setupProcess(cmd)
	cmd.WaitDelay = waitDelay
	return cmd, nil
}

// Result は Run の結果です。
type Result struct {
	ExitCode        int    `json:"exit_code" jsonschema:"process exit code (-1 if killed or unknown)"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	StdoutTruncated bool   `json:"stdout_truncated,omitempty"`
	StderrTruncated bool   `json:"stderr_truncated,omitempty"`
	TimedOut        bool   `json:"timed_out,omitempty"`
	DurationMs      int64  `json:"duration_ms"`
}

// Options は Run の動作を指定します。
type Options struct {
	DefaultShell string
	Stdin        string
	Timeout      time.Duration
	MaxOutput    int
}

// Run はコマンドを実行し、終了を待って結果を返します。
// 終了コードが 0 以外でもエラーにはしません。起動できなかった場合だけエラーを返します。
func Run(ctx context.Context, spec Spec, opt Options) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, opt.Timeout)
	defer cancel()

	cmd, err := Command(ctx, spec, opt.DefaultShell)
	if err != nil {
		return Result{}, err
	}
	stdout, stderr := limit.NewBuffer(opt.MaxOutput), limit.NewBuffer(opt.MaxOutput)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if opt.Stdin != "" {
		cmd.Stdin = strings.NewReader(opt.Stdin)
	}

	start := time.Now()
	runErr := cmd.Run()
	res := Result{
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		StdoutTruncated: stdout.Truncated(),
		StderrTruncated: stderr.Truncated(),
		TimedOut:        errors.Is(ctx.Err(), context.DeadlineExceeded),
		DurationMs:      time.Since(start).Milliseconds(),
	}
	if cmd.ProcessState == nil {
		return res, runErr
	}
	res.ExitCode = cmd.ProcessState.ExitCode()
	return res, nil
}

// DefaultShell はこの OS で shell が省略されたときに使うシェルを返します。
func DefaultShell(windowsShell string) string {
	if runtime.GOOS == "windows" {
		return windowsShell
	}
	return "sh"
}

// powershellArgs は、引用符の解釈の違いを避けるため、スクリプトを -EncodedCommand で渡す引数を作ります。
func powershellArgs(script string) []string {
	// 出力を UTF-8 にそろえ、ネイティブ コマンドの終了コードを引き継ぐ。
	// TODO: スクリプトの途中で失敗したコマンドレットの扱い ($? の反映) を検討する。
	script = "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8\n" +
		script + "\nif ($LASTEXITCODE) { exit $LASTEXITCODE }"
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[i*2:], c)
	}
	return []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(b)}
}

func mergeEnv(base []string, add map[string]string) []string {
	out := make([]string, 0, len(base)+len(add))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if !hasKey(add, k) {
			out = append(out, kv)
		}
	}
	for k, v := range add {
		out = append(out, k+"="+v)
	}
	return out
}

func hasKey(m map[string]string, key string) bool {
	for k := range m {
		// Windows の環境変数名は大文字と小文字を区別しない。
		if k == key || (runtime.GOOS == "windows" && strings.EqualFold(k, key)) {
			return true
		}
	}
	return false
}
