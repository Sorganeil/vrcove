// Package buildinfo holds metadata injected by build.sh, without a runtime
// dependency on Git or on the release tooling.
package buildinfo

import (
	"fmt"
	"runtime"
)

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

func Summary() string {
	return fmt.Sprintf("VRCove Logger %s (%s; %s; %s/%s)", Version, Commit, Date, runtime.GOOS, runtime.GOARCH)
}

func UserAgent() string { return "VRCoveLogger/" + Version }
