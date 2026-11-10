package middleware

import (
	"testing"
	"time"

	"github.com/yetone/magpie/internal/testenv"
)

// The tests run in a home of their own, never the user's real files, and
// give hooks room past the limits (#5965): the tests about a limit set
// their own.
func TestMain(m *testing.M) {
	LimitsForTest(time.Minute)
	testenv.Main(m)
}
