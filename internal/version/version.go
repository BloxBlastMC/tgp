// Package version holds build metadata injected at link time.
package version

import (
	"fmt"
	"runtime"
)

// Set via -ldflags "-X git.taile67276.ts.net/BloxBlast/tgp/internal/version.Version=..."
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// Info is the structured build information.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"go_version"`
}

// Get returns the current build information.
func Get() Info {
	return Info{Version: Version, Commit: Commit, Date: Date, GoVersion: runtime.Version()}
}

// String renders a one-line human readable version for the named binary.
func String(binary string) string {
	return fmt.Sprintf("%s %s (commit %s, built %s, %s)", binary, Version, Commit, Date, runtime.Version())
}
