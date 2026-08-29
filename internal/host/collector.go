package host

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/shirou/gopsutil/v4/common"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
)

// Sample is one host observation. Nil fields mean the value was not measurable.
type Sample struct {
	MemTotalBytes        *uint64
	MemAvailableBytes    *uint64
	SwapTotalBytes       *uint64
	SwapUsedBytes        *uint64
	CPUCount             *int
	CPUUsedRatio         *float64
	Load1, Load5, Load15 *float64
	DiskTotalBytes       *uint64
	DiskFreeBytes        *uint64
	Source               string
}

// Collector reads host metrics using gopsutil's context-scoped path overrides.
type Collector struct {
	procPath, sysPath string
	mu                sync.Mutex
	previous          *cpu.TimesStat
}

func NewCollector(procPath, sysPath string) *Collector {
	return &Collector{procPath: procPath, sysPath: sysPath}
}

func (c *Collector) context(ctx context.Context) context.Context {
	return context.WithValue(ctx, common.EnvKey, common.EnvMap{common.HostProcEnvKey: c.procPath, common.HostSysEnvKey: c.sysPath})
}

func ptr[T any](v T) *T { return &v }

// Collect returns one sample. Disk fields are omitted when dataDir is empty.
func (c *Collector) Collect(ctx context.Context, dataDir string) (Sample, error) {
	ctx = c.context(ctx)
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return Sample{}, fmt.Errorf("read virtual memory: %w", err)
	}
	sw, err := mem.SwapMemoryWithContext(ctx)
	if err != nil {
		return Sample{}, fmt.Errorf("read swap memory: %w", err)
	}
	counts, err := cpu.CountsWithContext(ctx, true)
	if err != nil {
		return Sample{}, fmt.Errorf("read cpu count: %w", err)
	}
	times, err := cpu.TimesWithContext(ctx, false)
	if err != nil {
		return Sample{}, fmt.Errorf("read cpu times: %w", err)
	}
	la, err := load.AvgWithContext(ctx)
	if err != nil {
		return Sample{}, fmt.Errorf("read load average: %w", err)
	}
	s := Sample{MemTotalBytes: ptr(vm.Total), MemAvailableBytes: ptr(vm.Available), SwapTotalBytes: ptr(sw.Total), SwapUsedBytes: ptr(sw.Used), CPUCount: ptr(counts), Load1: ptr(la.Load1), Load5: ptr(la.Load5), Load15: ptr(la.Load15), Source: "host"}
	if len(times) > 0 {
		c.mu.Lock()
		if c.previous != nil {
			total := times[0].User + times[0].System + times[0].Idle + times[0].Nice + times[0].Iowait + times[0].Irq + times[0].Softirq + times[0].Steal
			old := c.previous.User + c.previous.System + c.previous.Idle + c.previous.Nice + c.previous.Iowait + c.previous.Irq + c.previous.Softirq + c.previous.Steal
			active := total - times[0].Idle - times[0].Iowait - old + c.previous.Idle + c.previous.Iowait
			elapsed := total - old
			if elapsed > 0 {
				ratio := active / elapsed
				s.CPUUsedRatio = ptr(ratio)
			} else {
				// A repeated fixture or a counter that did not advance is a
				// measurable idle interval after the first sample.
				zero := 0.0
				s.CPUUsedRatio = &zero
			}
		}
		c.previous = &times[0]
		c.mu.Unlock()
	}
	if dataDir != "" {
		if du, e := disk.UsageWithContext(ctx, filepath.Clean(dataDir)); e == nil {
			s.DiskTotalBytes, s.DiskFreeBytes = ptr(du.Total), ptr(du.Free)
		}
	}
	c.applyCgroup(&s)
	return s, nil
}
