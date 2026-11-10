package proc

import (
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Which listeners hold a connection to an address: a port number is shared
// by every address of the computer, so a process listening on the same
// number on another address isn't the one there (#5816: the take-over named
// an emulator's qemu on mote, while the fake gateway held 127.0.0.1). These
// are built on every system so that each system's reading is tested on any.

// listener is one listening socket: its local address (nil when it couldn't
// be read), and the process holding it, by pid or, from /proc, by its
// socket's inode.
type listener struct {
	ip    net.IP
	pid   int
	inode string
}

// holding are the listeners among ls that hold addr's host: those on the
// host itself, or when there are none, those on the wildcard address of its
// family (0.0.0.0, and :: unless it is IPv6 only, which isn't read), since a
// connection reaches the most specific listener. A listener whose address
// couldn't be read is counted as a wildcard one. A wildcard host (0.0.0.0
// or ::) is where a listen on every address is kept from, by every listener
// on the port: Go listens on "0.0.0.0:P" with a dual-stack [::] socket,
// which a listener on [::1]:P keeps out too. A host that names no address
// (empty, a name) is every listener as well, as before.
func holding(ls []listener, host string) []listener {
	want := net.ParseIP(host)
	if want == nil || want.IsUnspecified() {
		return ls
	}
	var exact, wild []listener
	for _, l := range ls {
		switch {
		case l.ip == nil:
			wild = append(wild, l)
		case l.ip.Equal(want):
			exact = append(exact, l)
		case l.ip.IsUnspecified() && (l.ip.To4() == nil || want.To4() != nil):
			wild = append(wild, l)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return wild
}

// holders are the ids of the processes of ls holding host, each once.
func holders(ls []listener, host string) []int {
	var ps []int
	seen := map[int]bool{}
	for _, l := range holding(ls, host) {
		if l.pid > 0 && !seen[l.pid] {
			seen[l.pid] = true
			ps = append(ps, l.pid)
		}
	}
	return ps
}

// procListeners are the sockets listening (st 0A) on port in a
// /proc/net/tcp or tcp6 table, by inode. Its addresses are hex, each 32-bit
// word in the machine's order: little-endian on the systems magpie is built
// for (amd64, arm64).
func procListeners(table string, port int) []listener {
	var out []listener
	want := fmt.Sprintf(":%04X", port)
	for _, l := range strings.Split(table, "\n")[1:] {
		f := strings.Fields(l)
		if len(f) < 10 || f[3] != "0A" || !strings.HasSuffix(strings.ToUpper(f[1]), want) || f[9] == "0" {
			continue
		}
		out = append(out, listener{ip: procIP(strings.TrimSuffix(strings.ToUpper(f[1]), want)), inode: f[9]})
	}
	return out
}

// procIP reads /proc/net/tcp's hex address; nil when it isn't one.
func procIP(h string) net.IP {
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != net.IPv4len && len(b) != net.IPv6len {
		return nil
	}
	for i := 0; i < len(b); i += 4 {
		b[i], b[i+1], b[i+2], b[i+3] = b[i+3], b[i+2], b[i+1], b[i]
	}
	return net.IP(b)
}

// lsofSockets reads lsof's field output (-F ptn): a p line for each
// process, then for each of its sockets f, t (IPv4 or IPv6) and n, the
// address and port, "*" for every address.
func lsofSockets(out string) []listener {
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

// psSockets reads the PowerShell lines listeningOn asks for on Windows:
// each listener's LocalAddress and OwningProcess.
func psSockets(out string) []listener {
	var ls []listener
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) != 2 {
			continue
		}
		pid, err := strconv.Atoi(f[1])
		if err != nil || pid <= 0 {
			continue
		}
		h, _, _ := strings.Cut(f[0], "%")
		ls = append(ls, listener{ip: net.ParseIP(h), pid: pid})
	}
	return ls
}
