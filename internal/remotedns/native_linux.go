//go:build linux

package remotedns

import (
	"bytes"
	"context"
	"debug/elf"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/hondarer/host-shell-mcp/internal/proc"
)

//go:embed assets/preload.c
var source []byte

func prepare(ctx context.Context, dir, library string) (string, error) {
	if library != "" {
		p, err := filepath.Abs(library)
		if err != nil {
			return "", err
		}
		// Copy the supplied adapter so session cleanup never removes the original.
		b, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		if err := validateLibrary(b); err != nil {
			return "", err
		}
		dest := filepath.Join(dir, "libhostshell_dns.so")
		return dest, os.WriteFile(dest, b, 0700)
	}
	src := filepath.Join(dir, "preload.c")
	dest := filepath.Join(dir, "libhostshell_dns.so")
	if err := os.WriteFile(src, source, 0600); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd, err := proc.Command(ctx, proc.Spec{Argv: []string{"cc", "-Wall", "-Wextra", "-Werror", "-O2", "-fPIC", "-shared", "-o", dest, src, "-ldl", "-pthread"}}, "sh")
	if err != nil {
		return "", fmt.Errorf("remote DNS adapter: %w; install cc and glibc headers or use --remote-dns-library", err)
	}
	if b, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("remote DNS adapter: %w (%s); install cc and glibc headers or use --remote-dns-library", err, strings.TrimSpace(string(b)))
	}
	return dest, os.Chmod(dest, 0700)
}

// Reject incompatible adapter versions and architectures before injection.
func validateLibrary(b []byte) error {
	f, err := elf.NewFile(bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("remote DNS library: %w", err)
	}
	defer f.Close()
	machine := map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64}[runtime.GOARCH]
	if f.Type != elf.ET_DYN || f.Machine != machine || f.Class != elf.ELFCLASS64 {
		return fmt.Errorf("remote DNS library does not match Linux/%s shared-library ABI", runtime.GOARCH)
	}
	symbols, err := f.DynamicSymbols()
	if err != nil {
		return fmt.Errorf("remote DNS library symbols: %w", err)
	}
	hook, marker := false, false
	for _, s := range symbols {
		if s.Section == elf.SHN_UNDEF {
			continue
		}
		if s.Name == "getaddrinfo" {
			hook = true
		}
		if s.Name == "host_shell_dns_adapter_v1" {
			marker = true
		}
	}
	if !hook || !marker {
		return fmt.Errorf("remote DNS library must be built from internal/remotedns/assets/preload.c (adapter v1)")
	}
	return nil
}
