package host

import (
	"reflect"
	"testing"
)

func u64(v uint64) *uint64 { return &v }

func pids[T ProcessSample | ProcessRate](items []T) []int {
	out := make([]int, 0, len(items))
	for _, it := range items {
		switch v := any(it).(type) {
		case ProcessSample:
			out = append(out, v.PID)
		case ProcessRate:
			out = append(out, v.PID)
		}
	}
	return out
}

func TestPROC001_StatCommandWithSpacesAndParentheses(t *testing.T) {
	for _, line := range []string{
		"104 (post gres) S 100 104 104 0 -1 4194304 10 0 0 0 400 100 0 0 20 0 1 0 90 123456789 2048 0 0",
		"104 (post (g) res)) S 100 104 104 0 -1 4194304 10 0 0 0 400 100 0 0 20 0 1 0 90 123456789 2048 0 0",
	} {
		st, err := parseProcStat([]byte(line))
		if err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		if st != (procStat{ppid: 100, cpuTicks: 500, startTicks: 90}) {
			t.Fatalf("%q: got %+v", line, st)
		}
	}
	c := NewCollector("testdata/proc", "testdata/sys")
	ppid, err := c.ParentPID(104)
	if err != nil || ppid != 100 {
		t.Fatalf("ParentPID(104) = %d, %v", ppid, err)
	}
	if _, err := parseProcStat([]byte("1 (x) S 0")); err == nil {
		t.Fatal("short stat line accepted")
	}
}

func TestPROC002_ProcessesOfThePostmaster(t *testing.T) {
	c := NewCollector("testdata/proc", "testdata/sys")
	got, err := c.Processes(100)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{100, 101, 102, 103, 104, 105}; !reflect.DeepEqual(pids(got), want) {
		t.Fatalf("pids = %v, want %v", pids(got), want)
	}
	p := got[0]
	if p.CPUTicks != 1500 || p.StartTicks != 50 || p.RSSBytes != 1000*1024 || *p.ReadBytes != 100*4096 || *p.WriteBytes != 100*8192 {
		t.Fatalf("postmaster sample %+v", p)
	}
	// A postmaster that is gone yields no processes, not an error.
	if gone, err := c.Processes(999); err != nil || len(gone) != 0 {
		t.Fatalf("gone postmaster: %v, %v", pids(gone), err)
	}
}

func TestPROC003_UnreadableIOLeavesIONil(t *testing.T) {
	c := NewCollector("testdata/proc", "testdata/sys")
	got, err := c.Processes(100)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range got {
		if p.PID == 103 {
			if p.ReadBytes != nil || p.WriteBytes != nil {
				t.Fatalf("103 I/O = %v/%v, want nil", p.ReadBytes, p.WriteBytes)
			}
			if p.RSSBytes != 1030*1024 {
				t.Fatalf("103 RSS = %d", p.RSSBytes)
			}
			return
		}
	}
	t.Fatal("103 missing")
}

func TestPROC004_ProcessRates(t *testing.T) {
	prev := []ProcessSample{
		{PID: 1, StartTicks: 10, CPUTicks: 100, RSSBytes: 1, ReadBytes: u64(0), WriteBytes: u64(1000)},
		{PID: 2, StartTicks: 20, CPUTicks: 100},
		{PID: 3, StartTicks: 30, CPUTicks: 100},
	}
	cur := []ProcessSample{
		{PID: 1, StartTicks: 10, CPUTicks: 350, RSSBytes: 2, ReadBytes: u64(5000), WriteBytes: u64(6000)},
		{PID: 2, StartTicks: 99, CPUTicks: 500}, // pid reused
		{PID: 3, StartTicks: 30, CPUTicks: 50},  // counter went back
		{PID: 4, StartTicks: 40, CPUTicks: 10},  // first seen
	}
	got := ProcessRates(prev, cur, 5)
	if len(got) != 1 || got[0].PID != 1 {
		t.Fatalf("rates for %v, want [1]", pids(got))
	}
	r := got[0]
	if r.CPURatio != 0.5 || r.RSSBytes != 2 || *r.ReadBytesPerS != 1000 || *r.WriteBytesPerS != 1000 {
		t.Fatalf("rate %+v", r)
	}
	noIO := ProcessRates([]ProcessSample{{PID: 7, CPUTicks: 0}}, []ProcessSample{{PID: 7, CPUTicks: 100, ReadBytes: u64(1), WriteBytes: u64(1)}}, 1)
	if len(noIO) != 1 || noIO[0].ReadBytesPerS != nil || noIO[0].CPURatio != 1 {
		t.Fatalf("without previous I/O: %+v", noIO)
	}
	if ProcessRates(prev, cur, 0) != nil {
		t.Fatal("zero interval gave rates")
	}
}

func TestPROC005_TopProcesses(t *testing.T) {
	var rates []ProcessRate
	for pid := 1; pid <= 10; pid++ {
		// CPU grows with pid, RSS shrinks with pid.
		rates = append(rates, ProcessRate{PID: pid, CPURatio: float64(pid) / 10, RSSBytes: uint64(100 - pid)})
	}
	got := TopProcesses(rates, 2, 5)
	if want := []int{1, 2, 5, 9, 10}; !reflect.DeepEqual(pids(got), want) {
		t.Fatalf("top = %v, want %v", pids(got), want)
	}
	rates[0].CPURatio = 5 // pid 1 now tops both lists
	got = TopProcesses(rates, 2, 5)
	if want := []int{1, 2, 5, 10}; !reflect.DeepEqual(pids(got), want) {
		t.Fatalf("top with overlap = %v, want %v", pids(got), want)
	}
}
