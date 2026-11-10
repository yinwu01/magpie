package proc

import (
	"bytes"
	"errors"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

// lsofListeners reads what lsof wrote with -F ptn (lsofFields). lsof
// exits 1 when it finds nothing, with nothing written, and exits 1 when it
// fails too: those two read the same and are taken as none, as before.
// Any other ending is no answer and is given as the error. lsof killed at
// the caller's deadline ends with nothing written as well, and read as
// none it could let setPort move the gateway's port past a magpie of this
// computer's on a busy Mac (#5180). Only a Mac asks lsof; this is built
// everywhere so that its test runs on Linux too.
func lsofListeners(out []byte, err error) ([]listener, error) {
	var ee *exec.ExitError
	if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 1 && len(bytes.TrimSpace(out)) == 0) {
		return nil, err
	}
	return lsofFields(string(out)), nil
}

// lsofFields reads lsof's field output (-F ptn): a p line for each
// process, then for each of its sockets f, t (IPv4 or IPv6) and n, the
// address and port, "*" for every address.
func lsofFields(out string) []listener {
	var ls []listener
	pid, v6 := 0, false
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		switch v := l[1:]; l[0] {
		case 'p':
			pid, _ = strconv.Atoi(v)
			v6 = false
		case 'f':
			v6 = false
		case 't':
			v6 = v == "IPv6"
		case 'n':
			if pid <= 0 {
				continue
			}
			v, _, _ = strings.Cut(v, "->")
			i := strings.LastIndex(v, ":")
			if i < 0 {
				ls = append(ls, listener{pid: pid})
				continue
			}
			h := strings.TrimSuffix(strings.TrimPrefix(v[:i], "["), "]")
			var ip net.IP
			switch {
			case h == "*" && v6:
				ip = net.IPv6unspecified
			case h == "*":
				ip = net.IPv4zero
			default:
				h, _, _ = strings.Cut(h, "%")
				ip = net.ParseIP(h)
			}
			ls = append(ls, listener{ip: ip, pid: pid})
		}
	}
	return ls
}
