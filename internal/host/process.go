package host

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// ticksPerSecond is Linux USER_HZ, 100 on every supported platform.
const ticksPerSecond = 100

// ProcessSample is one reading of a process from /proc. ReadBytes and
// WriteBytes are nil when /proc/<pid>/io is not readable.
type ProcessSample struct {
	PID        int
	StartTicks uint64
	CPUTicks   uint64
	RSSBytes   uint64
	ReadBytes  *uint64
	WriteBytes *uint64
}

// ProcessRate is a process's usage between two samples. CPURatio 1.0 is one
// full core. The I/O rates are nil when either sample lacks I/O counters.
type ProcessRate struct {
	PID            int
	CPURatio       float64
	RSSBytes       uint64
	ReadBytesPerS  *float64
	WriteBytesPerS *float64
}

type procStat struct {
	comm       string
	ppid       int
	cpuTicks   uint64
	startTicks uint64
}

// parseProcStat reads /proc/<pid>/stat. The command name (field 2) is in
// parentheses and may contain spaces or parentheses, so fields are counted
// from the last ')'.
func parseProcStat(data []byte) (procStat, error) {
	start, end := bytes.IndexByte(data, '('), bytes.LastIndexByte(data, ')')
	if start < 0 || end < start {
		return procStat{}, errors.New("stat: no command name")
	}
	// fields[0] is field 3 (state).
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return procStat{}, fmt.Errorf("stat: %d fields after the command name", len(fields))
	}
	ppid, err := strconv.Atoi(fields[4-3])
	if err != nil {
		return procStat{}, fmt.Errorf("stat: parent pid: %w", err)
	}
	var vals [3]uint64
	for i, field := range []int{14, 15, 22} {
		v, err := strconv.ParseUint(fields[field-3], 10, 64)
		if err != nil {
			return procStat{}, fmt.Errorf("stat: field %d: %w", field, err)
		}
		vals[i] = v
	}
	return procStat{comm: string(data[start+1 : end]), ppid: ppid, cpuTicks: vals[0] + vals[1], startTicks: vals[2]}, nil
}

// parseProcStatus returns VmRSS of /proc/<pid>/status in bytes; a process
// without VmRSS (a kernel thread) has 0.
func parseProcStatus(data []byte) (uint64, error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		rest, ok := strings.CutPrefix(sc.Text(), "VmRSS:")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) != 2 || f[1] != "kB" {
			return 0, fmt.Errorf("status: VmRSS %q", rest)
		}
		kb, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("status: VmRSS: %w", err)
		}
		return kb * 1024, nil
	}
	return 0, sc.Err()
}

// parseProcIO returns read_bytes and write_bytes of /proc/<pid>/io.
func parseProcIO(data []byte) (read, write uint64, err error) {
	var seenRead, seenWrite bool
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		var dst *uint64
		switch key {
		case "read_bytes":
			dst, seenRead = &read, true
		case "write_bytes":
			dst, seenWrite = &write, true
		default:
			continue
		}
		v, err := strconv.ParseUint(strings.TrimSpace(val), 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("io: %s: %w", key, err)
		}
		*dst = v
	}
	if err := sc.Err(); err != nil {
		return 0, 0, err
	}
	if !seenRead || !seenWrite {
		return 0, 0, errors.New("io: read_bytes or write_bytes missing")
	}
	return read, write, nil
}

func (c *Collector) pidFile(pid int, name string) string {
	return filepath.Join(c.procPath, strconv.Itoa(pid), name)
}

func (c *Collector) readStat(pid int) (procStat, error) {
	data, err := os.ReadFile(c.pidFile(pid, "stat"))
	if err != nil {
		return procStat{}, err
	}
	return parseProcStat(data)
}

// ParentPID returns the parent pid of a process.
func (c *Collector) ParentPID(pid int) (int, error) {
	if pid <= 0 {
		return 0, fmt.Errorf("pid %d", pid)
	}
	st, err := c.readStat(pid)
	if err != nil {
		return 0, err
	}
	return st.ppid, nil
}

// ProcessName returns the executable name of a process (field 2 of stat, at
// most 15 bytes). It is used only to recognise PostgreSQL processes and is
// never shipped.
func (c *Collector) ProcessName(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("pid %d", pid)
	}
	st, err := c.readStat(pid)
	if err != nil {
		return "", err
	}
	return st.comm, nil
}

// errGone marks a process that ended while it was being read.
var errGone = errors.New("process gone")

func (c *Collector) processSample(pid int, st procStat) (ProcessSample, error) {
	s := ProcessSample{PID: pid, StartTicks: st.startTicks, CPUTicks: st.cpuTicks}
	data, err := os.ReadFile(c.pidFile(pid, "status"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
			return s, errGone
		}
		return s, err
	}
	if s.RSSBytes, err = parseProcStatus(data); err != nil {
		return s, err
	}
	if data, err := os.ReadFile(c.pidFile(pid, "io")); err == nil {
		if r, w, err := parseProcIO(data); err == nil {
			s.ReadBytes, s.WriteBytes = &r, &w
		}
	}
	return s, nil
}

// Processes returns the postmaster and every process whose parent is the
// postmaster, ordered by pid. A process that ends while being read is skipped.
func (c *Collector) Processes(postmaster int) ([]ProcessSample, error) {
	if postmaster <= 0 {
		return nil, fmt.Errorf("postmaster pid %d", postmaster)
	}
	entries, err := os.ReadDir(c.procPath)
	if err != nil {
		return nil, err
	}
	var out []ProcessSample
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 || strconv.Itoa(pid) != e.Name() {
			continue
		}
		st, err := c.readStat(pid)
		if err != nil {
			if pid == postmaster && !errors.Is(err, fs.ErrNotExist) {
				return nil, err
			}
			continue
		}
		if pid != postmaster && st.ppid != postmaster {
			continue
		}
		s, err := c.processSample(pid, st)
		if errors.Is(err, errGone) {
			continue
		}
		if err != nil {
			if pid == postmaster {
				return nil, err
			}
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out, nil
}

// ProcessRates pairs the processes present in both samples with the same start
// ticks. A counter that went backwards marks a new process, which is dropped.
func ProcessRates(prev, cur []ProcessSample, seconds float64) []ProcessRate {
	if seconds <= 0 {
		return nil
	}
	before := make(map[int]ProcessSample, len(prev))
	for _, p := range prev {
		before[p.PID] = p
	}
	var out []ProcessRate
	for _, c := range cur {
		p, ok := before[c.PID]
		if !ok || p.StartTicks != c.StartTicks || c.CPUTicks < p.CPUTicks {
			continue
		}
		r := ProcessRate{
			PID:      c.PID,
			CPURatio: float64(c.CPUTicks-p.CPUTicks) / ticksPerSecond / seconds,
			RSSBytes: c.RSSBytes,
		}
		if p.ReadBytes != nil && c.ReadBytes != nil && p.WriteBytes != nil && c.WriteBytes != nil {
			if *c.ReadBytes < *p.ReadBytes || *c.WriteBytes < *p.WriteBytes {
				continue
			}
			read := float64(*c.ReadBytes-*p.ReadBytes) / seconds
			write := float64(*c.WriteBytes-*p.WriteBytes) / seconds
			r.ReadBytesPerS, r.WriteBytesPerS = &read, &write
		}
		out = append(out, r)
	}
	return out
}

// TopProcesses returns the union of the topN processes by CPU and the topN by
// RSS, plus the postmaster, ordered by pid without duplicates.
func TopProcesses(rates []ProcessRate, topN int, postmaster int) []ProcessRate {
	if topN < 0 {
		topN = 0
	}
	keep := map[int]bool{postmaster: true}
	pick := func(less func(a, b ProcessRate) bool) {
		sorted := append([]ProcessRate(nil), rates...)
		sort.SliceStable(sorted, func(i, j int) bool {
			if less(sorted[i], sorted[j]) {
				return true
			}
			if less(sorted[j], sorted[i]) {
				return false
			}
			return sorted[i].PID < sorted[j].PID
		})
		for i := 0; i < len(sorted) && i < topN; i++ {
			keep[sorted[i].PID] = true
		}
	}
	pick(func(a, b ProcessRate) bool { return a.CPURatio > b.CPURatio })
	pick(func(a, b ProcessRate) bool { return a.RSSBytes > b.RSSBytes })
	var out []ProcessRate
	for _, r := range rates {
		if keep[r.PID] {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}
