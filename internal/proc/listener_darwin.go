package proc

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// listeningOn asks lsof, which lists this user's processes, for each
// listener's address too (-F ptn: -t would say the pid alone).
func listeningOn(ctx context.Context, host string, port int) ([]int, error) {
	lsof := "/usr/sbin/lsof"
	if _, err := os.Stat(lsof); err != nil {
		lsof = "lsof"
	}
	ls, err := lsofListeners(CommandContext(ctx, lsof, "-nP", "-a", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-F", "ptn").Output())
	if err != nil {
		return nil, err
	}
	return holders(ls, host), nil
}

// executable reads kern.procargs2: the argument count, then the path the
// process was started from.
func executable(pid int) (string, error) {
	b, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", err
	}
	if len(b) < 5 {
		return "", errors.New("no program path")
	}
	b = b[4:]
	if i := bytes.IndexByte(b, 0); i > 0 {
		return string(b[:i]), nil
	}
	return "", errors.New("no program path")
}
