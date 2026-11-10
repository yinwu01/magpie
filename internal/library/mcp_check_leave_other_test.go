//go:build !windows

package library

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/proc"
)

// The fake for TestServerExitLeavingItsStdout: a server that starts a
// process in a session of its own, out of reach of the tree's sweep, which
// keeps its stdout, and exits. MAGPIE_FAKE_MCP_LEAVE is where the left
// process's pid is written.
func TestFakeMCPLeaver(t *testing.T) {
	pids := os.Getenv("MAGPIE_FAKE_MCP_LEAVE")
	if pids == "" {
		t.Skip("run by TestServerExitLeavingItsStdout")
	}
	self, _ := os.Executable()
	c := exec.Command(self, "-test.run=^TestFakeMCPServer$")
	c.Env = append(os.Environ(), fakeMode+"=sleep")
	c.Stdout = os.Stdout
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	l, _ := os.OpenFile(pids, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	fmt.Fprintf(l, "%d\n", c.Process.Pid)
	l.Close()
	fmt.Fprintln(os.Stderr, "giving up")
	os.Exit(3)
}

// A server that exits while what it started keeps its stdout is still
// told as exited, by the check, the hub and Codex's run: its output is closed
// proc.OutputGrace after it is waited for, as Wait closing a StdoutPipe
// ended the read before (#5652).
func TestServerExitLeavingItsStdout(t *testing.T) {
	grace := proc.OutputGrace
	proc.OutputGrace = 200 * time.Millisecond
	t.Cleanup(func() { proc.OutputGrace = grace })
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pids := filepath.Join(t.TempDir(), "pids")
	t.Cleanup(func() {
		b, _ := os.ReadFile(pids)
		for _, p := range strings.Fields(string(b)) {
			if pid, err := strconv.Atoi(p); err == nil {
				syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	s := &Server{Name: "fake", Transport: "stdio", Command: self, Args: []string{"-test.run=^TestFakeMCPLeaver$"}, Env: map[string]string{"MAGPIE_FAKE_MCP_LEAVE": pids}}
	t.Run("check", func(t *testing.T) {
		short(t, 20*time.Second)
		if h := CheckServer(context.Background(), s); h.Why != "exited" || h.Code != 3 {
			t.Fatalf("got %+v, want exited 3", h)
		}
	})
	t.Run("hub", func(t *testing.T) {
		hubSandbox(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		c, err := dialHub(ctx, s, func() {})
		if err == nil {
			c.close()
		}
		if err == nil || !strings.HasPrefix(err.Error(), "it exited (3)") {
			t.Fatalf("got %v, want it exited (3)", err)
		}
	})
	t.Run("run", func(t *testing.T) {
		start := time.Now()
		_, _, err := runStart(&runSession{name: "fake"}, runSpec{Command: s.Command, Args: s.Args, Env: s.Env}, json.RawMessage(`{}`))
		if err == nil || err.Error() != "couldn't start fake: it ended: giving up" {
			t.Fatalf("got %v, want it ended: giving up", err)
		}
		if d := time.Since(start); d > 30*time.Second {
			t.Errorf("took %s", d)
		}
	})
}
