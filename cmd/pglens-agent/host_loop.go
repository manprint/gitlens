package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/host"
)

// groupTargetsByDataDir buckets the local targets by their data directory,
// so a directory shared by several targets is measured once. A target whose
// directory is not known yet goes in the "" bucket: it still gets cpu, load
// and memory, without disk fields. Each bucket lists its targets sorted.
func groupTargetsByDataDir(targets []string, dirs map[string]string) map[string][]string {
	buckets := make(map[string][]string)
	for _, name := range targets {
		dir := dirs[name]
		buckets[dir] = append(buckets[dir], name)
	}
	for dir := range buckets {
		sort.Strings(buckets[dir])
	}
	return buckets
}

// diskWarner logs why a target has no disk metrics, once per target and
// reason, so a missing mount is reported without a line every tick.
type diskWarner struct {
	out  io.Writer
	mu   sync.Mutex
	seen map[string]string // target -> last reason logged
}

func newDiskWarner(out io.Writer) *diskWarner {
	return &diskWarner{out: out, seen: make(map[string]string)}
}

// warn logs reason for target unless it is the reason logged last. An empty
// reason means the disk was measured: the next failure is logged again.
func (w *diskWarner) warn(target, reason string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if reason == "" {
		delete(w.seen, target)
		return
	}
	if w.seen[target] == reason {
		return
	}
	w.seen[target] = reason
	_, _ = fmt.Fprintf(w.out, "warning: host disk metrics unavailable for target %s: %s (mount the data directory read-only at the same path in the agent container)\n", target, reason)
}

// hostDataDirs asks each local target for its data directory. A role that
// may not read it is reported once and left without disk metrics; any other
// failure (a target still starting) is retried on the next tick silently.
// The targets are asked in parallel, so unreachable ones delay the tick by
// one timeout, not one each.
func hostDataDirs(ctx context.Context, managers map[string]*agent.Manager, warner *diskWarner) map[string]string {
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	dirs := make(map[string]string, len(managers))
	for name, mgr := range managers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dir, err := mgr.DataDirectory(ctx)
			if err != nil {
				if errors.Is(err, agent.ErrDataDirNotReadable) {
					warner.warn(name, err.Error())
				}
				return
			}
			mu.Lock()
			dirs[name] = dir
			mu.Unlock()
		}()
	}
	wg.Wait()
	return dirs
}

// collectHostByDataDir collects one sample per tick (CPU, load, memory) and
// measures each data directory's filesystem on top of it, handing the result
// to every target of that directory. A failed collection skips the tick.
func collectHostByDataDir(ctx context.Context, collector *host.Collector, buckets map[string][]string, warner *diskWarner, errOut io.Writer, emit func(target string, s host.Sample)) {
	if len(buckets) == 0 {
		return
	}
	base, err := collector.Collect(ctx, "")
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "warning: host collection: %v\n", err)
		return
	}
	dirs := make([]string, 0, len(buckets))
	for dir := range buckets {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		sample := collector.WithDisk(ctx, base, dir)
		for _, target := range buckets[dir] {
			if dir != "" {
				warner.warn(target, sample.DiskUnavailable)
			}
			emit(target, sample)
		}
	}
}
