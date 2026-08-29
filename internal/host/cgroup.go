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
		applyV2(root, s)
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

func applyV2(root string, s *Sample) {
	limited := false
	if limit, ok := readUint(filepath.Join(root, "memory.max")); ok && limit < cgroupV1Unlimited {
		if current, ok := readUint(filepath.Join(root, "memory.current")); ok {
			s.MemTotalBytes = &limit
			available := uint64(0)
			if current < limit {
				available = limit - current
			}
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
			available := uint64(0)
			if current < limit {
				available = limit - current
			}
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
