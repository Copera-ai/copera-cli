// Package build holds version information injected at build time via ldflags.
package build

// These variables are set by the Makefile via:
//
//	-X github.com/copera/copera-cli/internal/build.Version=$(VERSION)
//	-X github.com/copera/copera-cli/internal/build.Time=$(BUILD_TIME)
var (
	Version = "dev"
	Time    = "unknown"
)
