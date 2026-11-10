package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// rangeHost serves body and records the Range of each ask. Its first
// answer breaks off after cut bytes; a Range after that gets badRange as
// its Content-Range when badRange is set (usemagpie.ai's files on
// 2026-11-10: "bytes NaN-NaN/<size>" for any Range, #5984), a correct
// one otherwise.
type rangeHost struct {
	srv      *httptest.Server
	mu       sync.Mutex
	ranges   []string
	body     []byte
	cut      int
	badRange string
}

func newRangeHost(t *testing.T, body []byte, cut int, badRange string) *rangeHost {
	t.Helper()
	h := &rangeHost{body: body, cut: cut, badRange: badRange}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.ranges = append(h.ranges, r.Header.Get("Range"))
		first := len(h.ranges) == 1
		h.mu.Unlock()
		rg := r.Header.Get("Range")
		if rg == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			if first && cut > 0 {
				w.Write(body[:cut])
				w.(http.Flusher).Flush()
				conn, _, _ := w.(http.Hijacker).Hijack()
				conn.Close() // mid-stream
				return
			}
			w.Write(body)
			return
		}
		from, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rg, "bytes="), "-"))
		cr := fmt.Sprintf("bytes %d-%d/%d", from, len(body)-1, len(body))
		if badRange != "" {
			cr = badRange
		}
		w.Header().Set("Content-Range", cr)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)-from))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(body[from:])
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *rangeHost) asked() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.ranges)
}

func rangeBody() ([]byte, string) {
	body := make([]byte, 512<<10)
	for i := range body {
		body[i] = byte(i * 7)
	}
	sum := sha256.Sum256(body)
	return body, hex.EncodeToString(sum[:])
}

// The site's copy breaks off part way and answers the Range for the rest
// with a Content-Range that isn't it: the download goes on from
// github.com's address, from where it broke off, instead of failing
// "206 Partial Content" on the spot (#5984).
func TestDownloadLeavesASiteThatCantResumeForGitHub(t *testing.T) {
	quickRetries(t)
	body, sum := rangeBody()
	site := newRangeHost(t, body, 100<<10, fmt.Sprintf("bytes NaN-NaN/%d", len(body)))
	gh := newRangeHost(t, body, 0, "")
	a := Asset{
		URL:    site.srv.URL + "/files/v0.1.9/magpie-windows-amd64.exe",
		GitHub: gh.srv.URL + "/yetone/magpie-releases/releases/download/v0.1.9/magpie-windows-amd64.exe",
		SHA256: sum,
	}
	path := filepath.Join(t.TempDir(), "magpie.exe.new")
	if err := download(context.Background(), a, path); err != nil {
		t.Fatalf("download: %v", err)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, body) {
		t.Fatalf("file has %d bytes, not the release's", len(b))
	}
	if got, want := site.asked(), []string{"", "bytes=102400-"}; !slices.Equal(got, want) {
		t.Fatalf("site asked %q, want %q", got, want)
	}
	if got, want := gh.asked(), []string{"bytes=102400-"}; !slices.Equal(got, want) {
		t.Fatalf("github.com asked %q, want %q (the rest, from where the site broke off)", got, want)
	}
}

// With no other route to go to, a host that can't send the rest is asked
// for the whole file on the next try.
func TestDownloadAsksAHostThatCantResumeForTheWholeFile(t *testing.T) {
	quickRetries(t)
	body, sum := rangeBody()
	site := newRangeHost(t, body, 100<<10, fmt.Sprintf("bytes NaN-NaN/%d", len(body)))
	path := filepath.Join(t.TempDir(), "magpie.exe.new")
	a := Asset{URL: site.srv.URL + "/files/v0.1.9/magpie-windows-amd64.exe", SHA256: sum}
	if err := download(context.Background(), a, path); err != nil {
		t.Fatalf("download: %v", err)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, body) {
		t.Fatalf("file has %d bytes, not the release's", len(b))
	}
	if got, want := site.asked(), []string{"", "bytes=102400-", ""}; !slices.Equal(got, want) {
		t.Fatalf("site asked %q, want %q", got, want)
	}
}

// A host that can't send the rest, with the kept file one that can't be
// removed (another program has it open, as on Windows), ends the download
// saying so, rather than asking that host for the rest again.
func TestDownloadEndsWhenAFileThatCantResumeCantBeCleared(t *testing.T) {
	quickRetries(t)
	body, sum := rangeBody()
	site := newRangeHost(t, body, 100<<10, fmt.Sprintf("bytes NaN-NaN/%d", len(body)))
	path := filepath.Join(t.TempDir(), "magpie.exe.new")
	was := removeFile
	removeFile = func(p string) error {
		if _, err := os.Stat(p); err == nil && filepath.Clean(p) == filepath.Clean(path) {
			return &os.PathError{Op: "remove", Path: p, Err: errSharingViolation}
		}
		return was(p)
	}
	t.Cleanup(func() { removeFile = was })
	a := Asset{URL: site.srv.URL + "/files/v0.1.9/magpie-windows-amd64.exe", SHA256: sum}
	err := download(context.Background(), a, path)
	if err == nil || !strings.Contains(err.Error(), "couldn't clear the earlier download magpie.exe.new") {
		t.Fatalf("err = %v, want it to say the earlier download couldn't be cleared", err)
	}
	if strings.Contains(err.Error(), "on every try") {
		t.Fatalf("err = %q says \"on every try\" of a host asked once", err)
	}
	if got, want := site.asked(), []string{"", "bytes=102400-"}; !slices.Equal(got, want) {
		t.Fatalf("site asked %q, want %q", got, want)
	}
}
