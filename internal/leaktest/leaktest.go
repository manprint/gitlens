// Package leaktest detects goroutines that outlive the test that started
// them.
//
// Every long-lived component in this codebase owns at least one background
// goroutine — the agent's scheduler and its per-entry ticker loops, the ASH
// sampler, the pusher's drain loop, and the server's staleness, alert and
// advisor engines. Each has a Stop() whose whole contract is "the goroutines
// are gone when this returns", and nothing asserted that contract: a Stop()
// that silently left its loop running still passed every test, because the
// leaked goroutine simply kept ticking against a now-unused clock while the
// test it belonged to reported success.
//
// The check is deliberately dependency-free (no go.uber.org/goleak) and
// deliberately tolerant of the runtime's own background goroutines, which
// come and go for reasons a test cannot control.
package leaktest

import (
	"runtime"
	"sort"
	"strings"
	"time"
)

// TestingT is the subset of *testing.T this package needs, so the detector
// itself can be unit tested with a substitute that records failures instead
// of aborting.
type TestingT interface {
	Helper()
	Errorf(format string, args ...any)
	Cleanup(func())
}

// settleTimeout is how long a goroutine that is on its way out is given to
// actually exit. A Stop() that signals a loop and returns is correct even if
// the loop's final scheduling slice has not run yet.
const settleTimeout = 2 * time.Second

// ignoredCreators name the functions that start goroutines for the life of the
// process, which no test can stop: the runtime's own workers, the signal
// dispatcher, the testing package's runners, and the read and write loops that
// net/http keeps on a persistent connection, which it reaps on its own
// schedule. A goroutine is ignored only when its creation site names one of
// them. The frames it is parked in are never matched: a sleep, a WaitGroup, a
// mutex and a socket read all park under runtime or net paths, so matching
// those hid exactly the leaks this package exists to catch.
var ignoredCreators = []string{
	"runtime.",
	"testing.",
	"os/signal.",
	"net/http.(*Transport).dialConn",
}

// Check snapshots the goroutines running now and registers a t.Cleanup that
// fails the test if any goroutine that was not running at snapshot time is
// still running when the test ends.
//
// Usage is one line at the top of a lifecycle test:
//
//	func TestScheduler_StopIsClean(t *testing.T) {
//	    defer leaktest.Check(t)()
//	    ...
//	}
//
// The returned func is also the check itself, so callers that prefer an
// explicit `defer check()` to t.Cleanup ordering can use it directly; calling
// it twice is harmless.
func Check(t TestingT) func() {
	t.Helper()
	before := snapshot()
	checked := false
	check := func() {
		if checked {
			return
		}
		checked = true
		verify(t, before)
	}
	t.Cleanup(check)
	return check
}

func verify(t TestingT, before map[string]int) {
	t.Helper()
	deadline := time.Now().Add(settleTimeout)
	var leaked []string
	for {
		leaked = diff(before, snapshot())
		if len(leaked) == 0 || time.Now().After(deadline) {
			break
		}
		// Yield rather than sleep a fixed slice: a goroutine that has already
		// been signalled usually exits within one scheduling round.
		runtime.Gosched()
		time.Sleep(10 * time.Millisecond)
	}
	if len(leaked) > 0 {
		t.Errorf("goroutine leak: %d goroutine(s) outlived the test:\n%s",
			len(leaked), strings.Join(leaked, "\n\n"))
	}
}

// snapshot maps each goroutine's identifying stack (its creation site plus
// its current top frame) to how many goroutines currently share it. Counting
// rather than collecting identities keeps the check meaningful for worker
// pools, where the individual goroutines are interchangeable but the
// population size is exactly what must return to its starting value.
func snapshot() map[string]int {
	out := make(map[string]int)
	for _, stack := range stacks() {
		if ignored(stack) {
			continue
		}
		out[key(stack)]++
	}
	return out
}

func diff(before, after map[string]int) []string {
	var leaked []string
	for k, n := range after {
		if extra := n - before[k]; extra > 0 {
			leaked = append(leaked, k)
		}
	}
	sort.Strings(leaked)
	return leaked
}

// key reduces a full stack to the lines that identify what the goroutine is,
// independent of where in its loop it happens to be parked.
func key(stack string) string {
	lines := strings.Split(strings.TrimSpace(stack), "\n")
	if len(lines) == 0 {
		return stack
	}
	// Line 0 is "goroutine N [state]:" — the id and the state are both noise.
	// The creation site ("created by ...") is the stable identity.
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "created by ") {
			return strings.TrimSpace(lines[i])
		}
	}
	if len(lines) > 1 {
		return strings.TrimSpace(lines[1])
	}
	return strings.TrimSpace(lines[0])
}

func ignored(stack string) bool {
	creator := creatorOf(stack)
	for _, prefix := range ignoredCreators {
		if strings.HasPrefix(creator, prefix) {
			return true
		}
	}
	return false
}

// creatorOf returns the function named by a goroutine's "created by" line,
// without the " in goroutine N" the runtime appends. It returns "" for the
// main goroutine, which has no creator.
func creatorOf(stack string) string {
	for _, line := range strings.Split(stack, "\n") {
		if rest, ok := strings.CutPrefix(line, "created by "); ok {
			creator, _, _ := strings.Cut(rest, " in goroutine ")
			return creator
		}
	}
	return ""
}

// stacks returns one string per live goroutine.
func stacks() []string {
	buf := make([]byte, 1<<16)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		if len(buf) >= 1<<24 {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	parts := strings.Split(string(buf), "\n\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}
