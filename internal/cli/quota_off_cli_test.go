package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/agentenv"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/testenv"
)

// magpie quota names a provider switched off as switched off, its card's
// kind the one it has when on, not as a key with no balance endpoint
// (#5741: an OpenCode Go plan read "balance · not configured"). One that
// is on and tells nothing keeps the old line.
func TestQuotaSaysASwitchedOffProviderIsOff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("DSH_HOME", "")
	for _, p := range []provider.Provider{
		{ID: "opencode-go", Name: "OpenCode Go", Key: "k1", Chat: "https://opencode.ai/zen/go/v1", Catalog: "opencode-go", Off: true},
		{ID: "relay", Name: "Relay", Key: "k2", Chat: "http://127.0.0.1:1/v1", Off: true},
		{ID: "relay-on", Name: "Relay On", Key: "k3", Chat: "http://127.0.0.1:1/v1"},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	qs, err := withUntold(nil, []string{"opencode-go", "relay", "relay-on"})
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 3 {
		t.Fatalf("%d lines, want 3: %+v", len(qs), qs)
	}
	for i, want := range []struct{ id, kind, says string }{
		{"opencode-go", "plan", "switched off · its allowance isn't read while it is off; turn it on: magpie provider on opencode-go"},
		{"relay", "balance", "switched off · its allowance isn't read while it is off; turn it on: magpie provider on relay"},
		{"relay-on", "balance", "not configured · no balance endpoint known for it"},
	} {
		q := qs[i]
		if q.Provider != want.id || q.Kind != want.kind || !strings.HasPrefix(q.Error, want.says) {
			t.Errorf("line %d = %s %s %q, want %s %s %q", i, q.Provider, q.Kind, q.Error, want.id, want.kind, want.says)
		}
	}
	if _, err := withUntold(nil, []string{"nobody"}); err == nil {
		t.Error("a name that is no provider gave no error")
	}
}

// magpie quota refresh of a provider switched off says it is off, and its
// vendor isn't asked, rather than "no subscription, plan or key balance".
func TestQuotaRefreshSaysASwitchedOffProviderIsOff(t *testing.T) {
	groupsHome(t)
	for _, k := range agentenv.Vars {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	bin := t.TempDir()
	for _, name := range []string{"security", "secret-tool", "claude", "codex", "cursor-agent", "devin", "grok", "kiro-cli"} {
		testenv.Program(t, filepath.Join(bin, name), "#!/bin/sh\nexit 1\n")
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var asked atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		fmt.Fprint(w, `{"balance":"7"}`)
	}))
	t.Cleanup(up.Close)
	p, err := provider.Find("a")
	if err != nil {
		t.Fatal(err)
	}
	p.BalanceURL, p.BalancePath, p.Off = up.URL+"/a", "balance", true
	if err := provider.Save(*p); err != nil {
		t.Fatal(err)
	}
	provider.ForgetBalances()
	_, err = stdoutOf(t, func() error { return quotaCmd([]string{"quota", "refresh", "a"}) })
	if want := "A is switched off · its allowance isn't read while it is off; turn it on: magpie provider on a"; err == nil || err.Error() != want {
		t.Fatalf("refresh of a provider switched off: %v, want %q", err, want)
	}
	if n := asked.Load(); n != 0 {
		t.Fatalf("its balance was asked %d times", n)
	}
}

// The reporter's step: magpie quota opencode-go --json with OpenCode Go
// switched off, through the whole read (#5741).
func TestQuotaJSONSaysASwitchedOffPlanIsOff(t *testing.T) {
	groupsHome(t)
	for _, k := range agentenv.Vars {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	bin := t.TempDir()
	for _, name := range []string{"security", "secret-tool", "claude", "codex", "cursor-agent", "devin", "grok", "kiro-cli"} {
		testenv.Program(t, filepath.Join(bin, name), "#!/bin/sh\nexit 1\n")
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := provider.Save(provider.Provider{ID: "opencode-go", Name: "OpenCode Go", Key: "k1", Chat: "https://opencode.ai/zen/go/v1", Catalog: "opencode-go", Off: true}); err != nil {
		t.Fatal(err)
	}
	provider.ForgetPlanQuotas()
	provider.ForgetBalances()
	out, err := stdoutOf(t, func() error { return quotaCmd([]string{"quota", "opencode-go", "--json"}) })
	if err != nil {
		t.Fatal(err)
	}
	var qs []provider.Quota
	if err := json.Unmarshal([]byte(out), &qs); err != nil {
		t.Fatal(err, out)
	}
	if len(qs) != 1 || qs[0].Provider != "opencode-go" || qs[0].Kind != "plan" || !strings.HasPrefix(qs[0].Error, "switched off · ") {
		t.Fatalf("magpie quota opencode-go --json = %s", out)
	}
}
