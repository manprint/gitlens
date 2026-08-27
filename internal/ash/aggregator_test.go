package ash

import (
	"sync"
	"testing"
	"time"

	"github.com/manprint/pglens/internal/clock"
)

func TestAggregator_CountsPerKey(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	a := New(clk, 10*time.Second, 100)

	for i := 0; i < 10; i++ {
		a.Add("app", "active", "Lock", "transactionid", nil)
	}
	for i := 0; i < 15; i++ {
		a.Add("app", "active", "CPU", "CPU", nil)
	}
	for i := 0; i < 5; i++ {
		a.Add("other_db", "idle in transaction", "CPU", "CPU", nil)
	}

	w := a.Flush(clk.Now().Add(10 * time.Second))
	if len(w.Buckets) != 3 {
		t.Fatalf("expected 3 buckets, got %d: %+v", len(w.Buckets), w.Buckets)
	}
	total := 0
	for _, b := range w.Buckets {
		total += b.Samples
	}
	if total != 30 {
		t.Fatalf("expected 30 total samples, got %d", total)
	}
}

// NULL wait_event_type -> "CPU" is handled upstream in the sampler (7.1);
// this asserts the value round-trips through the key unchanged.
func TestAggregator_NullWaitBecomesCPU(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	a := New(clk, 10*time.Second, 100)
	a.Add("app", "active", "CPU", "CPU", nil)
	w := a.Flush(clk.Now())
	if len(w.Buckets) != 1 || w.Buckets[0].Key.WaitEventType != "CPU" {
		t.Fatalf("expected one CPU bucket, got %+v", w.Buckets)
	}
}

func TestAggregator_QueryIDNilIsNotZero(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	a := New(clk, 10*time.Second, 100)

	zero := int64(0)
	a.Add("app", "active", "CPU", "CPU", nil)
	a.Add("app", "active", "CPU", "CPU", &zero)

	w := a.Flush(clk.Now())
	if len(w.Buckets) != 2 {
		t.Fatalf("expected nil query_id and query_id=0 to be distinct keys, got %d buckets: %+v", len(w.Buckets), w.Buckets)
	}
	var sawNil, sawZero bool
	for _, b := range w.Buckets {
		if !b.Key.HasQueryID {
			sawNil = true
		} else if b.Key.QueryID == 0 {
			sawZero = true
		}
	}
	if !sawNil || !sawZero {
		t.Fatalf("expected one nil-query-id bucket and one query-id=0 bucket, got %+v", w.Buckets)
	}
}

func TestAggregator_WindowBoundaries(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	a := New(clk, 10*time.Second, 100)

	a.Add("app", "active", "CPU", "CPU", nil)
	w1 := a.Flush(clk.Now().Add(10 * time.Second))
	if len(w1.Buckets) != 1 {
		t.Fatalf("expected 1 bucket in first window, got %d", len(w1.Buckets))
	}

	// Nothing added after Flush yet: an empty window must still be well-formed.
	w2 := a.Flush(clk.Now().Add(20 * time.Second))
	if len(w2.Buckets) != 0 {
		t.Fatalf("expected zero buckets in the empty second window, got %+v", w2.Buckets)
	}
	if w2.Ticks != 0 {
		t.Fatalf("expected 0 ticks in the empty window, got %d", w2.Ticks)
	}

	// A sample added after the second Flush belongs to the third window, not
	// leaking back into w2.
	a.Add("app", "active", "CPU", "CPU", nil)
	w3 := a.Flush(clk.Now().Add(30 * time.Second))
	if len(w3.Buckets) != 1 {
		t.Fatalf("expected the post-flush sample to land in the next window, got %d buckets", len(w3.Buckets))
	}
}

func TestAggregator_TicksCounted(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	a := New(clk, 10*time.Second, 100)

	for i := 0; i < 10; i++ {
		a.TickSucceeded()
	}
	for i := 0; i < 2; i++ {
		a.TickFailed()
	}

	w := a.Flush(clk.Now())
	if w.Ticks != 10 {
		t.Fatalf("expected Ticks == 10, got %d", w.Ticks)
	}
}

func TestAggregator_CapConservesTotal(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	a := New(clk, 10*time.Second, 100)

	totalAdds := 0
	for i := 0; i < 500; i++ {
		n := (i % 7) + 1 // vary counts so the top-99 selection is meaningful
		for j := 0; j < n; j++ {
			a.Add("app", "active", "Lock", waitEventName(i), nil)
			totalAdds++
		}
	}

	w := a.Flush(clk.Now())
	if len(w.Buckets) != 100 {
		t.Fatalf("expected exactly 100 buckets (99 kept + 1 other), got %d", len(w.Buckets))
	}
	if !w.Truncated {
		t.Fatal("expected Truncated == true")
	}
	sum := 0
	for _, b := range w.Buckets {
		sum += b.Samples
	}
	if sum != totalAdds {
		t.Fatalf("conservation violated: sum(Samples) = %d, want %d (number of Add calls)", sum, totalAdds)
	}
}

func TestAggregator_CapKeepsTopKeys(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	a := New(clk, 10*time.Second, 100)

	// 150 keys, count = index so the top 99 are deterministic (150..52).
	for i := 1; i <= 150; i++ {
		for j := 0; j < i; j++ {
			a.Add("app", "active", "Lock", waitEventName(i), nil)
		}
	}

	w := a.Flush(clk.Now())
	if len(w.Buckets) != 100 {
		t.Fatalf("expected 100 buckets, got %d", len(w.Buckets))
	}

	kept := map[string]bool{}
	otherSamples := 0
	for _, b := range w.Buckets {
		if b.Key == otherKey {
			otherSamples = b.Samples
			continue
		}
		kept[b.Key.WaitEvent] = true
	}
	if len(kept) != 99 {
		t.Fatalf("expected 99 individually kept buckets, got %d", len(kept))
	}
	for i := 150; i >= 52; i-- {
		if !kept[waitEventName(i)] {
			t.Fatalf("expected top key %s (count %d) to survive individually", waitEventName(i), i)
		}
	}
	wantOther := 0
	for i := 1; i <= 51; i++ {
		wantOther += i
	}
	if otherSamples != wantOther {
		t.Fatalf("other bucket: got %d samples, want %d", otherSamples, wantOther)
	}
}

func TestAggregator_NoCapNoTruncated(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	a := New(clk, 10*time.Second, 100)

	for i := 0; i < 50; i++ {
		a.Add("app", "active", "Lock", waitEventName(i), nil)
	}
	w := a.Flush(clk.Now())
	if w.Truncated {
		t.Fatal("expected Truncated == false under the cap")
	}
	for _, b := range w.Buckets {
		if b.Key == otherKey {
			t.Fatal("did not expect an 'other' bucket under the cap")
		}
	}
}

func TestAggregator_Concurrent(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	a := New(clk, 10*time.Second, 50)

	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if i%10 == 0 {
					a.TickSucceeded()
				}
				a.Add("app", "active", "Lock", waitEventName(g), nil)
			}
		}(g)
	}
	wg.Wait()

	w := a.Flush(clk.Now())
	sum := 0
	for _, b := range w.Buckets {
		sum += b.Samples
	}
	if sum != 2000 {
		t.Fatalf("expected 2000 total samples under -race, got %d", sum)
	}
}

func FuzzAggregator_ConservesTotal(f *testing.F) {
	f.Add(3, 5, 10)
	f.Add(0, 0, 0)
	f.Add(200, 1, 1000)
	f.Fuzz(func(t *testing.T, numKeys, maxKeys, adds int) {
		if numKeys < 0 || numKeys > 2000 || adds < 0 || adds > 5000 {
			t.Skip()
		}
		if maxKeys < 1 {
			maxKeys = 1
		}
		if maxKeys > 5000 {
			maxKeys = 5000
		}
		if numKeys == 0 {
			numKeys = 1
		}

		clk := clock.NewFake(time.Unix(0, 0))
		a := New(clk, 10*time.Second, maxKeys)

		total := 0
		for i := 0; i < adds; i++ {
			k := i % numKeys
			a.Add("app", "active", "Lock", waitEventName(k), nil)
			total++
		}

		w := a.Flush(clk.Now())
		sum := 0
		for _, b := range w.Buckets {
			sum += b.Samples
			if b.Samples < 0 {
				t.Fatalf("negative sample count: %+v", b)
			}
		}
		if sum != total {
			t.Fatalf("conservation violated: sum=%d, want %d (numKeys=%d maxKeys=%d adds=%d)", sum, total, numKeys, maxKeys, adds)
		}
		if len(w.Buckets) > maxKeys {
			t.Fatalf("bucket count %d exceeds maxKeys %d", len(w.Buckets), maxKeys)
		}
	})
}

func waitEventName(i int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	if i < 0 {
		i = -i
	}
	s := make([]byte, 0, 8)
	if i == 0 {
		return "a0"
	}
	for i > 0 {
		s = append(s, letters[i%len(letters)])
		i /= len(letters)
	}
	return "we_" + string(s)
}
