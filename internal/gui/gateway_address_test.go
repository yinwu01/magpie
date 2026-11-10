package gui

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/gateway"
)

// #5816: TestGatewayFixQuitsOnlyAMagpie's take-over on mote named an
// emulator's qemu, which listened on the same port number as the fake
// gateway on 127.0.0.1, at another address. The take-over, Restart's
// "taken" and Settings' port look only at the process holding the address
// the gateway is reached at. Here a second copy of the test binary listens
// on the same port at another loopback address (127.0.0.2, or [::1] where
// that can't be had), a program of another name beside a magpie and then a
// magpie beside a program of another name.
func TestGatewayFixLooksOnlyAtTheGatewaysAddress(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	was := gateway.Version
	gateway.Version = "0.1.630"
	defer func() { gateway.Version = was }()
	served.Store(nil)
	waits := []*time.Duration{&quitWait, &killWait, &startWait}
	olds := []time.Duration{quitWait, killWait, startWait}
	quitWait, killWait, startWait = 3*time.Second, 2*time.Second, 5*time.Second
	defer func() {
		for i, w := range waits {
			*w = olds[i]
		}
	}()
	stopServed := func() {
		gatewayMu.Lock()
		defer gatewayMu.Unlock()
		if served.Load() != nil && servedRun.stop != nil {
			servedRun.stop()
			<-servedRun.done
		}
		served.Store(nil)
	}
	t.Cleanup(stopServed)

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// start runs the test binary under name, as a fake gateway at addr,
	// and waits until addr takes a connection
	start := func(name, addr string) (*exec.Cmd, chan struct{}) {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		bin := filepath.Join(t.TempDir(), name)
		if runtime.GOOS == "windows" || os.Link(self, bin) != nil {
			b, err := os.ReadFile(self)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(bin, b, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command(bin, "-test.run=^$")
		cmd.Env = append(os.Environ(), "MAGPIE_TEST_FAKE_GATEWAY="+addr)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		ended := make(chan struct{})
		go func() { cmd.Wait(); close(ended) }()
		t.Cleanup(func() { cmd.Process.Kill(); <-ended })
		for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
			if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
				c.Close()
				break
			}
			select {
			case <-ended:
				t.Fatalf("%s at %s exited", name, addr)
			default:
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s at %s didn't answer", name, addr)
			}
		}
		// the one at the gateway's address answers as magpie first, as
		// the take-over asks it
		for deadline := time.Now().Add(10 * time.Second); addr == gatewayAt() && gateway.ServedBy().Version != "0.1.550"; time.Sleep(50 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("%s at %s didn't answer as magpie", name, addr)
			}
		}
		return cmd, ended
	}
	// beside is another loopback address with port free on it
	beside := func(port string) string {
		for _, h := range []string{"127.0.0.2", "::1"} {
			if ln, err := net.Listen("tcp", net.JoinHostPort(h, port)); err == nil {
				ln.Close()
				return net.JoinHostPort(h, port)
			}
		}
		t.Skip("neither 127.0.0.2 nor ::1 can be listened on here")
		return ""
	}

	// a magpie at the gateway's address is quit, and the program of
	// another name on the same port elsewhere neither named nor ended
	port := strconv.Itoa(freePort(t))
	other := beside(port)
	t.Setenv("MAGPIE_ADDR", "127.0.0.1:"+port)
	_, oldEnded := start("magpie", "127.0.0.1:"+port)
	_, otherEnded := start("other-server", other)
	if out := takeOverGateway(); !out.OK {
		t.Fatalf("take-over with a program of another name at %s: %+v", other, out)
	}
	select {
	case <-oldEnded:
	case <-time.After(5 * time.Second):
		t.Fatal("the older magpie is still running")
	}
	select {
	case <-otherEnded:
		t.Fatalf("ended the program at %s", other)
	default:
	}

	stopServed()

	// a program of another name at the gateway's address is named, with a
	// magpie on the same port elsewhere, which Settings' port doesn't take
	// for one of this computer's on the gateway's port either
	port = strconv.Itoa(freePort(t))
	mag := beside(port)
	t.Setenv("MAGPIE_ADDR", "127.0.0.1:"+port)
	stranger, _ := start("other-server", "127.0.0.1:"+port)
	start("magpie", mag)
	if magpieOnPort(gatewayAt()) {
		t.Fatalf("the magpie at %s taken for the one at %s", mag, gatewayAt())
	}
	if out := takeOverGateway(); out.OK || out.Reason != "not-magpie" || out.PID != stranger.Process.Pid {
		t.Fatalf("take-over with a magpie at %s: %+v, want not-magpie, pid %d", mag, out, stranger.Process.Pid)
	}
}
