//go:build !linux && !windows

package sensors

import (
	"context"
	"os/exec"
)

func helper(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// NowPlaying has no source on other systems.
func NowPlaying(context.Context) Media { return Media{} }

func readUsage() (busy, idle uint64, ram int, ok bool) { return 0, 0, 0, false }
