package proc

import (
	"context"
	"net"
	"os"
	"slices"
	"sort"
	"strconv"
	"testing"
)

// #5816: the take-over named an emulator's qemu on mote, listening on the
// same port number as the test's fake gateway on 127.0.0.1, at another
// address. A listener on another address of the same port isn't the one
// at 127.0.0.1; one on the wildcard address is, of its family. Here this
// process listens, so its own pid is what is looked for.
func TestListeningOnTheAddressAlone(t *testing.T) {
	ctx := context.Background()
	self := os.Getpid()
	// another loopback address: 127.0.0.2 on Linux, [::1] where 127/8
	// isn't all loopback (a Mac)
	var ln net.Listener
	for _, at := range []string{"127.0.0.2:0", "[::1]:0"} {
		if l, err := net.Listen("tcp", at); err == nil {
			ln = l
			break
		}
	}
	if ln == nil {
		t.Skip("neither 127.0.0.2 nor ::1 can be listened on here")
	}
	defer ln.Close()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	pids, err := ListeningOn(ctx, ln.Addr().String())
	if err != nil || !slices.Contains(pids, self) {
		t.Fatalf("at its own address %s: %v, %v", ln.Addr(), pids, err)
	}
	pids, err = ListeningOn(ctx, net.JoinHostPort("127.0.0.1", port))
	if err != nil || slices.Contains(pids, self) {
		t.Fatalf("a listener on %s named for 127.0.0.1:%s: %v, %v", ln.Addr(), port, pids, err)
	}

	// the wildcard address of IPv4 holds 127.0.0.1 too, and not ::1
	wild, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer wild.Close()
	port = strconv.Itoa(wild.Addr().(*net.TCPAddr).Port)
	pids, err = ListeningOn(ctx, net.JoinHostPort("127.0.0.1", port))
	if err != nil || !slices.Contains(pids, self) {
		t.Fatalf("0.0.0.0:%s not named for 127.0.0.1: %v, %v", port, pids, err)
	}
	pids, err = ListeningOn(ctx, net.JoinHostPort("::1", port))
	if err != nil || slices.Contains(pids, self) {
		t.Fatalf("0.0.0.0:%s named for ::1: %v, %v", port, pids, err)
	}
}

// A shared gateway listens on 0.0.0.0, which Go makes a dual-stack [::]
// socket, and a listener on [::1] at its port keeps it out: Restart's
// "taken" names that one too.
func TestListeningOnTheWildcardIsEveryListener(t *testing.T) {
	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skip("::1 can't be listened on here")
	}
	defer ln.Close()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	if wild, err := net.Listen("tcp", "0.0.0.0:"+port); err == nil {
		wild.Close()
		t.Logf("0.0.0.0:%s could be listened on beside [::1] here", port)
	}
	pids, err := ListeningOn(context.Background(), net.JoinHostPort("0.0.0.0", port))
	if err != nil || !slices.Contains(pids, os.Getpid()) {
		t.Fatalf("[::1]:%s not named for 0.0.0.0: %v, %v", port, pids, err)
	}
}

// /proc/net/tcp and tcp6 as this Linux box wrote them (5.15, amd64; the uid
// made 1000), with
// listeners on port 49510 (C166) at 127.0.0.2, 0.0.0.0 and 127.0.0.1, and
// at ::1, :: (not IPv6 only) and ::ffff:127.0.0.1; the line for port 3425
// (0D61) is added by hand, to be left out.
const (
	procTCP = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
  87: 0200007F:C166 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 1338080098 1 0000000000000000 100 0 0 10 0
 118: 00000000:C166 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 1338080099 1 0000000000000000 100 0 0 10 0
 125: 0100007F:C166 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 1338080097 1 0000000000000000 100 0 0 10 0
 126: 0100007F:0D61 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 1338080001 1 0000000000000000 100 0 0 10 0
`
	procTCP6 = `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
  11: 00000000000000000000000001000000:C166 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 1338080100 1 0000000000000000 100 0 0 10 0
  12: 00000000000000000000000000000000:C166 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 1338080101 1 0000000000000000 100 0 0 10 0
  13: 0000000000000000FFFF00000100007F:C166 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 1338080102 1 0000000000000000 100 0 0 10 0
`
)

// Which of /proc's listeners hold each address: the address itself when
// one listens there (::ffff:127.0.0.1 is 127.0.0.1), else the wildcard of
// its family; a wildcard host is kept from by every listener on the port,
// and a host that isn't an address matches every listener, as before.
func TestProcListenersHolding(t *testing.T) {
	ls := append(procListeners(procTCP, 49510), procListeners(procTCP6, 49510)...)
	if len(ls) != 6 {
		t.Fatalf("read %d listeners on 49510, want 6: %v", len(ls), ls)
	}
	for host, want := range map[string][]string{
		"127.0.0.1": {"1338080097", "1338080102"},
		"127.0.0.2": {"1338080098"},
		"127.0.0.3": {"1338080099", "1338080101"},
		"::1":       {"1338080100"},
		"fe80::1":   {"1338080101"},
		"0.0.0.0":   {"1338080097", "1338080098", "1338080099", "1338080100", "1338080101", "1338080102"},
		"::":        {"1338080097", "1338080098", "1338080099", "1338080100", "1338080101", "1338080102"},
		"":          {"1338080097", "1338080098", "1338080099", "1338080100", "1338080101", "1338080102"},
		"localhost": {"1338080097", "1338080098", "1338080099", "1338080100", "1338080101", "1338080102"},
	} {
		var got []string
		for _, l := range holding(ls, host) {
			got = append(got, l.inode)
		}
		sort.Strings(got)
		if !slices.Equal(got, want) {
			t.Errorf("holding %q: %v, want %v", host, got, want)
		}
	}
}

// lsof's field output (-F ptn), as its manual gives the fields: a p line a
// process, then f, t and n for each socket, "*" for every address. Not
// recorded on a Mac. qemu (14122) on *:49510, as on mote, isn't the one at
// 127.0.0.1 while a process (501) listens there itself.
func TestLsofFieldsHolding(t *testing.T) {
	out := "p501\nf7\ntIPv4\nn127.0.0.1:49510\n" +
		"p14122\nf12\ntIPv6\nn*:49510\nf13\ntIPv4\nn*:49510\n" +
		"p900\nf3\ntIPv6\nn[::1]:49510\n" +
		"p901\nf4\ntIPv6\nn[fe80::1%lo0]:49510\n"
	ls := lsofFields(out)
	for host, want := range map[string][]int{
		"127.0.0.1": {501},
		"127.0.0.2": {14122},
		"::1":       {900},
		"fe80::1":   {901},
		"::2":       {14122},
		"":          {501, 14122, 900, 901},
	} {
		if got := holders(ls, host); !slices.Equal(got, want) {
			t.Errorf("holders %q: %v, want %v", host, got, want)
		}
	}
	// with nobody on 127.0.0.1 itself, the wildcard is who answers there
	if got := holders(lsofFields("p14122\nf12\ntIPv6\nn*:49510\n"), "127.0.0.1"); !slices.Equal(got, []int{14122}) {
		t.Errorf("holders of 127.0.0.1 by *:49510 (IPv6): %v", got)
	}
	if got := holders(lsofFields("p14122\nf13\ntIPv4\nn*:49510\n"), "::1"); len(got) != 0 {
		t.Errorf("holders of ::1 by *:49510 (IPv4): %v", got)
	}
}

// What listeningOn asks PowerShell for on Windows: LocalAddress and
// OwningProcess a line, ended \r\n. Written from Get-NetTCPConnection's
// properties, not recorded on Windows.
func TestPSSocketsHolding(t *testing.T) {
	out := "127.0.0.1 4242\r\n0.0.0.0 77\r\n:: 77\r\n::1 99\r\nfe80::1%12 55\r\n\r\n"
	ls := psSockets(out)
	for host, want := range map[string][]int{
		"127.0.0.1": {4242},
		"127.0.0.2": {77},
		"::1":       {99},
		"fe80::1":   {55},
		"":          {4242, 77, 99, 55},
	} {
		if got := holders(ls, host); !slices.Equal(got, want) {
			t.Errorf("holders %q: %v, want %v", host, got, want)
		}
	}
}
