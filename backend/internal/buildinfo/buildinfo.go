// Package buildinfo carries version metadata injected at build time via
// -ldflags. It has no dependencies so any package can import it.
package buildinfo

// These are overridden at build time:
//
//	-ldflags "-X github.com/oglimmer/coding-agent/backend/internal/buildinfo.Version=1.2.3 ..."
var (
	Version = "dev"
	Commit  = "none"
	Time    = "unknown"
)
