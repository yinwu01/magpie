package stall

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func get(ctx context.Context, url string, after time.Duration) (int, error) {
	ctx, w := Start(ctx, after)
	defer w.Stop()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, w.Err(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(w.Reader(res.Body))
	return len(b), w.Err(err)
}

// A body that keeps coming, slowly, for many times the stall's while is
// read whole: only silence ends it (#5572). It runs in a synctest bubble,
// so the watch's timer and the sender's gaps are on the bubble's clock: a
// loaded machine that ran neither side for longer than the while read
// nothing in 250ms of real time (#5968). The body comes through a pipe
// that the watch's ctx breaks, as an HTTP client's transport breaks a
// response body when its request's ctx ends.
func TestSlowButMovingIsReadWhole(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const after, gap = 250 * time.Millisecond, 50 * time.Millisecond
		ctx, w := Start(context.Background(), after)
		defer w.Stop()
		pr, pw := io.Pipe()
		stop := context.AfterFunc(ctx, func() { pr.CloseWithError(context.Cause(ctx)) })
		defer stop()
		var sent atomic.Int64
		go func() {
			// three of the stall's whiles, ten bytes every gap
			for began := time.Now(); time.Since(began) < 3*after; time.Sleep(gap) {
				if _, err := pw.Write([]byte("0123456789")); err != nil {
					return
				}
				sent.Add(10)
			}
			pw.Close()
		}()
		b, err := io.ReadAll(w.Reader(pr))
		synctest.Wait() // the sender is done, or asleep with the pipe broken
		n := int(sent.Load())
		if err = w.Err(err); err != nil || len(b) != n || n < 3*int(after/gap)*10 {
			t.Fatalf("got %d bytes, err %v; want all %d sent (at least %d)", len(b), err, n, 3*int(after/gap)*10)
		}
	})
}

// A server that says nothing, before its answer or part way through it,
// ends with ErrStalled.
func TestSilenceIsAStall(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"no answer": func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() },
		"part way": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "100")
			w.Write([]byte("0123456789"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, err := get(ctx, srv.URL, 100*time.Millisecond)
			if !errors.Is(err, ErrStalled) || ctx.Err() != nil {
				t.Fatalf("err = %v, want a stall", err)
			}
		})
	}
}
