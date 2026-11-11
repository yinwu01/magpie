package budget

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/usage"
)

// A request in flight when its key's limit is set again holds the window
// it began in, not the new one, when the two start in the same second as
// when they don't (#6082): the new window's status shows no reservation,
// and a cap of one token still lets a request in. Its start, reset and
// calls are checked too (8b2fb6b08).
func TestARequestInFlightHoldsOnlyItsOwnWindow(t *testing.T) {
	// a while ago, on the real clock the ledger reads, 100ms into a second
	at := time.Now().Truncate(time.Second).Add(100*time.Millisecond - 2*time.Hour)
	if at.Add(time.Minute).Day() != at.Day() {
		at = at.Add(-2 * time.Minute) // now, a minute on, is the same day
	}
	now := at.Add(time.Minute)
	y, m, d := at.Date()
	midnight := time.Date(y, m, d+1, 0, 0, 0, 0, time.Local)
	for _, c := range []struct {
		name      string
		old, next access.Limit
		reset     time.Time // of next, set at at+5ms
	}{
		{"hours", access.Limit{Period: "hours", Hours: 5, Tokens: 1e6}, access.Limit{Period: "hours", Hours: 5, Tokens: 1e6}, at.Add(5*time.Millisecond + 5*time.Hour)},
		{"days then hours", access.Limit{Period: "days", Days: 10, Cost: 800}, access.Limit{Period: "hours", Hours: 5, Tokens: 200_000}, at.Add(5*time.Millisecond + 5*time.Hour)},
		{"days", access.Limit{Period: "days", Days: 10, Tokens: 1e6}, access.Limit{Period: "days", Days: 10, Tokens: 1e6}, at.Add(5*time.Millisecond + 240*time.Hour)},
		{"day from Since", access.Limit{Period: "day", Tokens: 1e6}, access.Limit{Period: "day", Tokens: 1e6}, midnight},
	} {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
			Forget()
			t.Cleanup(Forget)
			old, next := c.old, c.next
			old.Since, next.Since = at, at.Add(5*time.Millisecond)
			Append(usage.Record{Time: at.Add(time.Millisecond), Provider: "p", Model: "m", Input: 1000, Output: 100, Status: 200, CallerKeyID: "k"})
			if st := Of(access.Key{ID: "k", Limit: &old}, now); st == nil || st.Calls != 1 {
				t.Fatalf("the window before: %+v", st)
			}
			// a request begun between the two
			release, refused := Reserve(access.Identity{KeyID: "k", Limit: &old}, 400, "m", at.Add(2*time.Millisecond))
			if refused != nil {
				t.Fatal(refused)
			}
			defer release()
			st := Of(access.Key{ID: "k", Limit: &next}, now)
			if st == nil || !st.Start.Equal(next.Since) || !st.Reset.Equal(c.reset) || st.Calls != 0 || st.Tokens != 0 || st.InFlight != 0 {
				t.Fatalf("the window from %v to %v: %+v", next.Since, c.reset, st)
			}
			// nor does the request in flight hold the new window's cap
			tight := next
			tight.Tokens, tight.Cost = 1, 0
			release2, refused := Reserve(access.Identity{KeyID: "k", Limit: &tight}, 400, "m", now)
			if refused != nil {
				t.Fatalf("refused in a window nothing was spent in: %+v", refused.Status)
			}
			release2()
		})
	}
}
