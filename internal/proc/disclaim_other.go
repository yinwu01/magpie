//go:build !darwin || !cgo

package proc

import (
	"context"
	"os/exec"
)

func combinedOutputOwn(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	out, err := cmd.CombinedOutput()
	// a command the context's end killed waits with "signal: killed",
	// which exec.Cmd reports before the context's error; the run ended
	// because the context did, as macOS's path says
	if err != nil && ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, err
}

// Responsible is the process macOS holds responsible for pid, 0 when that
// can't be told (here, always).
func Responsible(pid int) int { return 0 }
