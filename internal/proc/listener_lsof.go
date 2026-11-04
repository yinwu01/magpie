package proc

import (
	"bytes"
	"errors"
	"os/exec"
	"strconv"
	"strings"
)

// lsofListeners reads what `lsof -t` wrote: one process id a line. lsof
// exits 1 when it finds nothing, with nothing written, and exits 1 when it
// fails too: those two read the same and are taken as none, as before.
// Any other ending is no answer and is given as the error. lsof killed at
// the caller's deadline ends with nothing written as well, and read as
// none it could let setPort move the gateway's port past a magpie of this
// computer's on a busy Mac (#5180). Only a Mac asks lsof; this is built
// everywhere so that its test runs on Linux too.
func lsofListeners(out []byte, err error) ([]int, error) {
	var ee *exec.ExitError
	if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 1 && len(bytes.TrimSpace(out)) == 0) {
		return nil, err
	}
	return pids(string(out)), nil
}

// pids reads one id a line.
func pids(out string) []int {
	var ps []int
	seen := map[int]bool{}
	for _, l := range strings.Fields(out) {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && !seen[n] {
			seen[n] = true
			ps = append(ps, n)
		}
	}
	return ps
}
