// Package version holds version information embedded at build time.
package version

// Version is overridden in release builds with -ldflags "-X github.com/hondarer/host-shell-mcp/internal/version.Version=...".
var Version = "dev"
