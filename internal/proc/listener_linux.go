package proc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// listeningOn reads /proc: the listening sockets on port that hold host,
// from /proc/net/tcp and tcp6, then the processes holding them, among those
// whose descriptors this user may read.
func listeningOn(ctx context.Context, host string, port int) ([]int, error) {
	var ls []listener
	read := false
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		read = true
		ls = append(ls, procListeners(string(b), port)...)
	}
	if !read {
		return nil, fmt.Errorf("can't read /proc/net/tcp")
	}
	inodes := map[string]bool{}
	for _, l := range holding(ls, host) {
		inodes[l.inode] = true
	}
	if len(inodes) == 0 {
		return nil, nil
	}
	dirs, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var ps []int
	for _, d := range dirs {
		pid, err := strconv.Atoi(d.Name())
		if err != nil {
			continue
		}
		if ctx.Err() != nil {
			return ps, ctx.Err()
		}
		fds, err := os.ReadDir(filepath.Join("/proc", d.Name(), "fd"))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			l, err := os.Readlink(filepath.Join("/proc", d.Name(), "fd", fd.Name()))
			if err == nil && strings.HasPrefix(l, "socket:[") && inodes[strings.TrimSuffix(strings.TrimPrefix(l, "socket:["), "]")] {
				ps = append(ps, pid)
				break
			}
		}
	}
	return ps, nil
}

func executable(pid int) (string, error) {
	return os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
}
