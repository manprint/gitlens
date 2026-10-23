package host

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const cgroupV1Unlimited = uint64(1 << 62)

// applyCgroup replaces host memory/CPU ceilings when a cgroup limit is visible.
func (c *Collector) applyCgroup(s *Sample) {
	root := filepath.Join(c.sysPath, "fs", "cgroup")
	if _, err := os.Stat(filepath.Join(root, "cgroup.controllers")); err == nil {
		own := c.ownCgroupDir(root)
		memDir := governingDir(own, root, hasMemoryLimit)
		cpuDir := governingDir(own, root, hasCPUQuota)
		applyV2(memDir, cpuDir, s)
		c.applyV2CPURatio(cpuDir, s)
		return
	}
	if _, err := os.Stat(filepath.Join(root, "memory", "memory.limit_in_bytes")); err == nil {
		applyV1(root, s)
	}
}

// governingDir is the directory whose limit binds own: the nearest one from own
// up to root that has such a limit, since the kernel enforces every ancestor's
// limit. The usage it is measured by is read from that same directory. It is ""
// when no directory up to root has one, and the host figures stand.
func governingDir(own, root string, hasLimit func(dir string) bool) string {
	for dir := own; ; dir = filepath.Dir(dir) {
		if hasLimit(dir) {
			return dir
		}
		if dir == root || !strings.HasPrefix(dir, root+string(filepath.Separator)) {
			return ""
		}
	}
}

// hasMemoryLimit reports whether dir holds a memory limit that the usage of the
// cgroup can be measured against.
func hasMemoryLimit(dir string) bool {
	limit, ok := readUint(filepath.Join(dir, "memory.max"))
	if !ok || limit >= cgroupV1Unlimited {
		return false
	}
	_, ok = readUint(filepath.Join(dir, "memory.current"))
	return ok
}

// hasCPUQuota reports whether dir holds a CPU quota.
func hasCPUQuota(dir string) bool {
	_, ok := readCPUQuota(dir)
	return ok
}

func readUint(path string) (uint64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	return v, err == nil
}

// ownCgroupDir is the directory, under root, of the cgroup this process belongs
// to. A container started with --cgroupns=host sees the host's root cgroup at
// its mount point, where its own limit files are absent: they sit in the
// directory /proc/self/cgroup names. Without that file, or when it names no
// directory below root, root is used, which is the answer for a private
// cgroup namespace.
//
// The path is followed only when PID 1 names the same cgroup. The processes of
// a container share its cgroup, so the container's init names it too. An agent
// run as a host service names its own unit's cgroup while PID 1 sits in
// init.scope; the unit's limit is then not the container's, and root is used.
func (c *Collector) ownCgroupDir(root string) string {
	own, ok := unifiedCgroupPath(filepath.Join(c.procPath, "self", "cgroup"))
	if !ok {
		return root
	}
	pid1, ok := unifiedCgroupPath(filepath.Join(c.procPath, "1", "cgroup"))
	if !ok || pid1 != own {
		return root
	}
	dir := filepath.Join(root, filepath.FromSlash(own))
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return root
}

// unifiedCgroupPath is the path of the unified (v2) hierarchy in a
// /proc/<pid>/cgroup file. A path that climbs out of the mount with ".." is
// refused.
func unifiedCgroupPath(file string) (string, bool) {
	b, err := os.ReadFile(file)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(b), "\n") {
		// The unified hierarchy is the single line "0::<path>".
		fields := strings.SplitN(line, ":", 3)
		if len(fields) != 3 || fields[0] != "0" || fields[1] != "" {
			continue
		}
		rel := strings.TrimSpace(fields[2])
		if strings.Contains(rel, "..") {
			return "", false
		}
		return rel, true
	}
	return "", false
}

// cgroupCPUReading is a cgroup's cumulative CPU time at one moment.
type cgroupCPUReading struct {
	usageUsec uint64
	at        time.Time
}

// applyV2CPURatio makes the CPU ratio the share of the cgroup's own quota used
// since the previous sample. The host ratio is host-wide, so next to a quota
// that describes the container it reads as an idle container: a container at
// its 2-CPU quota on a 16-core host showed 12.5%. Without a quota the host is
// the resource, and the host ratio stands. dir is the directory governingDir
// chose for the quota, "" when there is none.
func (c *Collector) applyV2CPURatio(dir string, s *Sample) {
	quotaCores, hasQuota := 0.0, false
	if dir != "" {
		quotaCores, hasQuota = readCPUQuota(dir)
	}
	var usage uint64
	hasUsage := false
	if hasQuota {
		usage, hasUsage = readStatField(filepath.Join(dir, "cpu.stat"), "usage_usec")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !hasQuota || !hasUsage {
		c.cgroupCPU = nil
		return
	}
	now := c.now()
	prev := c.cgroupCPU
	c.cgroupCPU = &cgroupCPUReading{usageUsec: usage, at: now}
	if prev == nil {
		s.CPUUsedRatio = nil
		return
	}
	// No time elapsed, or a counter that went backwards (the cgroup was
	// recreated), measures nothing. The ratio stays unknown rather than reading
	// as an idle container. A counter that did not move over a real interval is
	// idle and falls through to 0.
	elapsed := now.Sub(prev.at)
	if elapsed <= 0 || usage < prev.usageUsec {
		s.CPUUsedRatio = nil
		return
	}
	used := float64(usage - prev.usageUsec)
	available := elapsed.Seconds() * 1e6 * quotaCores
	ratio := math.Min(used/available, 1)
	s.CPUUsedRatio = &ratio
}

// readCPUQuota is the cgroup v2 CPU quota in cores, unrounded. ok is false when
// the cgroup has no quota.
func readCPUQuota(dir string) (float64, bool) {
	b, err := os.ReadFile(filepath.Join(dir, "cpu.max"))
	if err != nil {
		return 0, false
	}
	parts := strings.Fields(string(b))
	if len(parts) != 2 || parts[0] == "max" {
		return 0, false
	}
	q, e1 := strconv.ParseUint(parts[0], 10, 64)
	p, e2 := strconv.ParseUint(parts[1], 10, 64)
	if e1 != nil || e2 != nil || p == 0 || q == 0 {
		return 0, false
	}
	return float64(q) / float64(p), true
}

// readStatField reads the value of one "key value" line of a cgroup stat file.
func readStatField(path, key string) (uint64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == key {
			v, err := strconv.ParseUint(fields[1], 10, 64)
			return v, err == nil
		}
	}
	return 0, false
}

// cgroupAvailable is the memory a cgroup can still take: its limit less what it
// uses. Used is the current usage less the inactive file cache, which the
// kernel reclaims under pressure, as MemAvailable counts it. Without that stat
// the whole usage counts as used.
func cgroupAvailable(limit, current uint64, statPath, inactiveKey string) uint64 {
	used := current
	if inactive, ok := readStatField(statPath, inactiveKey); ok {
		used = 0
		if inactive < current {
			used = current - inactive
		}
	}
	if used >= limit {
		return 0
	}
	return limit - used
}

// applyV2 takes the memory limit from memDir and the CPU count from cpuDir, the
// directories governingDir chose for each. An empty one leaves its figures
// standing.
func applyV2(memDir, cpuDir string, s *Sample) {
	limited := false
	if memDir != "" {
		if limit, ok := readUint(filepath.Join(memDir, "memory.max")); ok && limit < cgroupV1Unlimited {
			if current, ok := readUint(filepath.Join(memDir, "memory.current")); ok {
				s.MemTotalBytes = &limit
				available := cgroupAvailable(limit, current, filepath.Join(memDir, "memory.stat"), "inactive_file")
				s.MemAvailableBytes = &available
				limited = true
			}
		}
	}
	if cpuDir != "" {
		if quota, ok := readCPUQuota(cpuDir); ok {
			n := int(math.Ceil(quota))
			s.CPUCount = &n
			limited = true
		}
	}
	if limited {
		s.Source = "cgroup_v2"
	}
}

func applyV1(root string, s *Sample) {
	limited := false
	if limit, ok := readUint(filepath.Join(root, "memory", "memory.limit_in_bytes")); ok && limit < cgroupV1Unlimited {
		if current, ok := readUint(filepath.Join(root, "memory", "memory.usage_in_bytes")); ok {
			s.MemTotalBytes = &limit
			available := cgroupAvailable(limit, current, filepath.Join(root, "memory", "memory.stat"), "total_inactive_file")
			s.MemAvailableBytes = &available
			limited = true
		}
	}
	quota, qok := readInt(filepath.Join(root, "cpu", "cpu.cfs_quota_us"))
	period, pok := readInt(filepath.Join(root, "cpu", "cpu.cfs_period_us"))
	if qok && pok && quota >= 0 && period > 0 {
		n := int(math.Ceil(float64(quota) / float64(period)))
		s.CPUCount = &n
		limited = true
	}
	if limited {
		s.Source = "cgroup_v1"
	}
}

func readInt(path string) (int64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	return v, err == nil
}
