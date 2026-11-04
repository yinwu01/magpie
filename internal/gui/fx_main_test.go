package gui

import (
	"net"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/fx"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/testenv"
)

// TestMain keeps the rate off the network: a state read with no rate cached
// asks for one behind it (#541), which would otherwise reach the real
// endpoint and land in whatever test runs next. The tests run in a home of
// their own, never the user's real files, and off the network
// (testenv.Offline): an account a test signs in has its Codex models
// asked for behind the save, which went to chatgpt.com with the
// fixture's token (TestAccountSaveKeepsPriceRate).
func TestMain(m *testing.M) {
	// started by TestGatewayFixQuitsOnlyAMagpie as an older magpie
	if addr := os.Getenv("MAGPIE_TEST_FAKE_GATEWAY"); addr != "" {
		fakeOlderGateway(addr)
		return
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err == nil {
		addr := l.Addr().String()
		l.Close() // refused at once from here on
		fx.SetRateURL("http://" + addr + "/")
	}
	os.Exit(testenv.RunIn(m, func(string) { testenv.Offline() }))
}

// A signed-in account's Codex models are refused at once, never asked of
// chatgpt.com.
func TestStaysOffTheNetwork(t *testing.T) {
	resp, err := http.Get(provider.CodexBase + "/models")
	if err == nil {
		resp.Body.Close()
		t.Fatalf("%s answered %s: the package's tests reach the network", provider.CodexBase, resp.Status)
	}
	if !strings.Contains(err.Error(), "tests stay off the network") {
		t.Fatalf("%s: %v, not testenv's refusal", provider.CodexBase, err)
	}
}
