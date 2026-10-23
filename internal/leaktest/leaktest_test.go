package leaktest

import (
	"context"
	"fmt"
	"net"
	"net/http"
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

// A goroutine that replaces an exited one of the same function, started from
// another parent, is growth. The identity keeps the parent, so the replacement
// is not netted against the goroutine that exited.
func TestKey_ReplacementFromAnotherParentIsGrowth(t *testing.T) {
	stack := func(parent string) string {
		return "goroutine 42 [chan receive]:\n" +
			"example.com/pkg.worker(...)\n\t/src/pkg/worker.go:10 +0x1c\n" +
			"created by example.com/pkg.Start in goroutine " + parent + "\n\t/src/pkg/start.go:20 +0x40"
	}
	before := map[string]int{key(stack("1")): 1}
	after := map[string]int{key(stack("9")): 1}
	if leaked := diff(before, after); len(leaked) != 1 {
		t.Errorf("diff() = %v, want the replacement reported as growth", leaked)
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

// expectLeak runs park, which must leave a goroutine blocked, and asserts that
// the detector reports it. park releases what it blocks through t.Cleanup, so
// the process is clean again when the test ends.
func expectLeak(t *testing.T, park func()) {
	t.Helper()
	rec := &recorder{}
	check := Check(rec)
	park()
	time.Sleep(100 * time.Millisecond)
	check()
	if !rec.failed() {
		t.Fatal("a goroutine blocked at the end of the test was not reported")
	}
	if !strings.Contains(rec.message(), "goroutine leak") {
		t.Errorf("failure message does not name the problem: %s", rec.message())
	}
}

// The detector must report a goroutine whatever it is blocked on. These are
// the blocking calls the earlier filter hid, because their frames sit under
// runtime and net paths.

func TestCheck_ReportsAGoroutineInTimeSleep(t *testing.T) {
	// time.Sleep cannot be woken, and the detector reports only a goroutine
	// still running after its settle window, so the goroutine sleeps past that
	// window. The test waits for it before it returns, so it does not leak
	// into the tests that follow.
	done := make(chan struct{})
	t.Cleanup(func() { <-done })
	expectLeak(t, func() {
		go func() {
			defer close(done)
			time.Sleep(2 * settleTimeout)
		}()
	})
}

func TestCheck_ReportsAGoroutineWaitingOnAWaitGroup(t *testing.T) {
	expectLeak(t, func() {
		var wg sync.WaitGroup
		wg.Add(1)
		go wg.Wait()
		t.Cleanup(wg.Done)
	})
}

func TestCheck_ReportsAGoroutineWaitingOnAMutex(t *testing.T) {
	expectLeak(t, func() {
		var mu sync.Mutex
		mu.Lock()
		go mu.Lock()
		t.Cleanup(mu.Unlock)
	})
}

func TestCheck_ReportsAGoroutineReadingASocket(t *testing.T) {
	expectLeak(t, func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		client, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Close() })
		// The connection is already queued, so Accept returns without a second
		// goroutine that would itself be blocked.
		server, err := ln.Accept()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = server.Close() })
		go func() {
			_, _ = client.Read(make([]byte, 1))
		}()
	})
}

func TestCheck_ReportsAnHTTPRoundTripThatNeverAnswers(t *testing.T) {
	expectLeak(t, func() {
		// The listener never accepts, so the request is sent and no response
		// comes back. The transport's own read and write loops are not the
		// leak; the round trip waiting on them is.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		transport := &http.Transport{}
		t.Cleanup(transport.CloseIdleConnections)
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+ln.Addr().String()+"/", nil)
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			client := &http.Client{Transport: transport}
			if resp, err := client.Do(req); err == nil {
				_ = resp.Body.Close()
			}
		}()
	})
}

// ignored must pass over the goroutines the runtime and the standard library
// own for the life of the process, and must report every goroutine a test can
// leave behind. The stacks are the ones this toolchain prints.
func TestIgnored_OnlyTheProcessLifetimeGoroutines(t *testing.T) {
	const (
		transportLoop = "goroutine 24 [select]:\n" +
			"net/http.(*persistConn).writeLoop(0x70f35fc280)\n" +
			"\t/usr/local/go/src/net/http/transport.go:2833 +0xe6\n" +
			"created by net/http.(*Transport).dialConn in goroutine 22\n" +
			"\t/usr/local/go/src/net/http/transport.go:1 +0x1"
		signalLoop = "goroutine 19 [syscall]:\n" +
			"os/signal.signal_recv()\n" +
			"\t/usr/local/go/src/runtime/sigqueue.go:152 +0x98\n" +
			"os/signal.loop()\n" +
			"created by os/signal.Notify.func2.1 in goroutine 1\n" +
			"\t/usr/local/go/src/os/signal/signal.go:1 +0x1"
		gcWorker = "goroutine 7 [GC worker (idle)]:\n" +
			"runtime.gopark(0x1)\n" +
			"\t/usr/local/go/src/runtime/proc.go:1 +0x1\n" +
			"created by runtime.gcBgMarkStartWorkers in goroutine 1\n" +
			"\t/usr/local/go/src/runtime/mgc.go:1 +0x1"
		sleeping = "goroutine 8 [sleep]:\n" +
			"time.Sleep(0x34630b8a000)\n" +
			"\t/usr/local/go/src/runtime/time.go:368 +0x165\n" +
			"main.main.func1()\n" +
			"created by main.main in goroutine 1\n" +
			"\t/src/main.go:1 +0x1"
		waitGroup = "goroutine 9 [sync.WaitGroup.Wait]:\n" +
			"sync.(*WaitGroup).Wait(0x70f3524250)\n" +
			"\t/usr/local/go/src/sync/waitgroup.go:1 +0x1\n" +
			"created by main.main in goroutine 1\n" +
			"\t/src/main.go:1 +0x1"
		roundTrip = "goroutine 16 [select]:\n" +
			"net/http.(*persistConn).roundTrip(0x70f35fc280, 0x70f372c050)\n" +
			"\t/usr/local/go/src/net/http/transport.go:3092 +0x84b\n" +
			"created by main.main in goroutine 1\n" +
			"\t/src/main.go:1 +0x1"
		mainGoroutine = "goroutine 1 [running]:\n" +
			"main.main()\n" +
			"\t/src/main.go:5 +0x10"
	)
	tests := []struct {
		name  string
		stack string
		want  bool
	}{
		{"transport read and write loop", transportLoop, true},
		{"signal dispatcher", signalLoop, true},
		{"runtime gc worker", gcWorker, true},
		{"goroutine in time.Sleep", sleeping, false},
		{"goroutine in WaitGroup.Wait", waitGroup, false},
		{"round trip waiting for a response", roundTrip, false},
		{"main goroutine without a creator", mainGoroutine, false},
	}
	for _, tt := range tests {
		if got := ignored(tt.stack); got != tt.want {
			t.Errorf("%s: ignored() = %v, want %v", tt.name, got, tt.want)
		}
	}
}
