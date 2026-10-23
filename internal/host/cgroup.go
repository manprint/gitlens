package host

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const cgroupV1Unlimited = uint64(1 << 62)

// applyCgroup replaces host memory/CPU ceilings when a cgroup limit is visible.
func (c *Collector) applyCgroup(s *Sample) {
	root := filepath.Join(c.sysPath, "fs", "cgroup")
	if _, err := os.Stat(filepath.Join(root, "cgroup.controllers")); err == nil {
		applyV2(c.ownCgroupDir(root), s)
		return
	}
	if _, err := os.Stat(filepath.Join(root, "memory", "memory.limit_in_bytes")); err == nil {
		applyV1(root, s)
	}
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
func (c *Collector) ownCgroupDir(root string) string {
	b, err := os.ReadFile(filepath.Join(c.procPath, "self", "cgroup"))
	if err != nil {
		return root
	}
	for _, line := range strings.Split(string(b), "\n") {
		// The unified hierarchy is the single line "0::<path>".
		fields := strings.SplitN(line, ":", 3)
		if len(fields) != 3 || fields[0] != "0" || fields[1] != "" {
			continue
		}
		rel := strings.TrimSpace(fields[2])
		if strings.Contains(rel, "..") {
			return root
		}
		dir := filepath.Join(root, filepath.FromSlash(rel))
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
		return root
	}
	return root
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

func applyV2(root string, s *Sample) {
	limited := false
	if limit, ok := readUint(filepath.Join(root, "memory.max")); ok && limit < cgroupV1Unlimited {
		if current, ok := readUint(filepath.Join(root, "memory.current")); ok {
			s.MemTotalBytes = &limit
			available := cgroupAvailable(limit, current, filepath.Join(root, "memory.stat"), "inactive_file")
			s.MemAvailableBytes = &available
			limited = true
		}
	}
	if b, err := os.ReadFile(filepath.Join(root, "cpu.max")); err == nil {
		parts := strings.Fields(string(b))
		if len(parts) == 2 && parts[0] != "max" {
			if q, e1 := strconv.ParseUint(parts[0], 10, 64); e1 == nil {
				if p, e2 := strconv.ParseUint(parts[1], 10, 64); e2 == nil && p > 0 {
					n := int(math.Ceil(float64(q) / float64(p)))
					s.CPUCount = &n
					limited = true
				}
			}
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
