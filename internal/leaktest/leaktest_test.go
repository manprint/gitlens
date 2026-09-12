package leaktest

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder is a TestingT substitute: it records failures instead of failing,
// so a test can assert that the detector both fires and stays quiet at the
// right times.
type recorder struct {
	mu       sync.Mutex
	failures []string
	cleanups []func()
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures = append(r.failures, strings.TrimSpace(fmt.Sprintf(format, args...)))
}

func (r *recorder) Cleanup(f func()) { r.cleanups = append(r.cleanups, f) }

func (r *recorder) failed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.failures) > 0
}

func (r *recorder) message() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.failures, "\n")
}

func TestCheck_PassesWhenEveryGoroutineExits(t *testing.T) {
	rec := &recorder{}
	check := Check(rec)

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-done
	}()
	close(done)
	wg.Wait()

	check()
	if rec.failed() {
		t.Errorf("clean shutdown reported a leak: %s", rec.message())
	}
}

func TestCheck_DetectsAGoroutineThatOutlivesTheTest(t *testing.T) {
	rec := &recorder{}
	check := Check(rec)

	// A loop that ignores its stop signal — the exact shape of a Stop() that
	// returns without actually stopping anything.
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	go func() {
		<-release
	}()

	// Give the goroutine a moment to be scheduled and appear in the snapshot.
	time.Sleep(20 * time.Millisecond)
	check()

	if !rec.failed() {
		t.Fatal("a goroutine still parked at the end of the test was not reported")
	}
	if !strings.Contains(rec.message(), "goroutine leak") {
		t.Errorf("failure message does not name the problem: %s", rec.message())
	}
}

func TestCheck_IsIdempotent(t *testing.T) {
	rec := &recorder{}
	check := Check(rec)
	check()
	check()
	if rec.failed() {
		t.Errorf("unexpected failure: %s", rec.message())
	}
}

func TestCheck_RegistersACleanup(t *testing.T) {
	rec := &recorder{}
	Check(rec)
	if len(rec.cleanups) != 1 {
		t.Fatalf("registered %d cleanups, want exactly 1", len(rec.cleanups))
	}
	rec.cleanups[0]()
	if rec.failed() {
		t.Errorf("cleanup reported a leak in a test that started none: %s", rec.message())
	}
}

func TestKey_PrefersTheCreationSite(t *testing.T) {
	stack := "goroutine 42 [chan receive]:\n" +
		"example.com/pkg.worker(...)\n\t/src/pkg/worker.go:10 +0x1c\n" +
		"created by example.com/pkg.Start in goroutine 1\n\t/src/pkg/start.go:20 +0x40"
	got := key(stack)
	if !strings.HasPrefix(got, "created by example.com/pkg.Start") {
		t.Errorf("key() = %q, want the creation site", got)
	}
}

func TestKey_FallsBackToTheTopFrame(t *testing.T) {
	stack := "goroutine 1 [running]:\nmain.main()\n\t/src/main.go:5 +0x10"
	if got := key(stack); got != "main.main()" {
		t.Errorf("key() = %q, want the top frame", got)
	}
}

func TestDiff_OnlyReportsGrowth(t *testing.T) {
	before := map[string]int{"a": 2, "b": 1}
	after := map[string]int{"a": 2, "b": 3, "c": 1}
	got := diff(before, after)
	want := []string{"b", "c"}
	if len(got) != len(want) {
		t.Fatalf("diff() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("diff()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if n := len(diff(map[string]int{"a": 3}, map[string]int{"a": 1})); n != 0 {
		t.Errorf("a shrinking population reported %d leaks, want 0", n)
	}
}
