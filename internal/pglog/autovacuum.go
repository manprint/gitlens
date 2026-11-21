package pglog

import (
	"regexp"
	"strconv"
	"strings"
)

// VacuumRun is one autovacuum or autoanalyze run as the server logs it with
// log_autovacuum_min_duration (plan 007 R5). A nil number was not in the
// message or did not match.
type VacuumRun struct {
	Kind                   string // vacuum or analyze
	Aggressive, Wraparound bool
	// Database, Schema and Relation split "database.schema.relation" at the
	// first two dots: a name that holds a dot is split wrongly.
	Database, Schema, Relation string

	IndexScans, PagesRemoved, PagesRemain, PagesScanned *int64
	TuplesRemoved, TuplesRemain, TuplesDeadNotRemovable *int64
	BufferHits, BufferReads, BufferDirtied              *int64
	WALRecords, WALFullPageImages, WALBytes             *int64
	CPUUserSeconds, CPUSystemSeconds, ElapsedSeconds    *float64
}

var (
	vacuumHeader  = regexp.MustCompile(`^automatic (aggressive )?(vacuum|analyze) (to prevent wraparound )?of table "([^"]+)"(?:: index scans: (\d+))?`)
	vacuumPages   = regexp.MustCompile(`^pages: (\d+) removed, (\d+) remain, (\d+) scanned`)
	vacuumTuples  = regexp.MustCompile(`^tuples: (\d+) removed, (\d+) remain, (\d+) are dead but not yet removable`)
	vacuumBuffers = regexp.MustCompile(`^buffer usage: (\d+) hits, (\d+) (?:misses|reads), (\d+) dirtied`)
	vacuumWAL     = regexp.MustCompile(`^WAL usage: (\d+) records, (\d+) full page images, (\d+) bytes`)
	vacuumCPU     = regexp.MustCompile(`^system usage: CPU: user: ([0-9.]+) s, system: ([0-9.]+) s, elapsed: ([0-9.]+) s`)
)

// ParseAutovacuum reads an autovacuum or autoanalyze message; false when its
// first line is not such a header. Each further line is matched alone.
func ParseAutovacuum(message string) (VacuumRun, bool) {
	lines := strings.Split(message, "\n")
	h := vacuumHeader.FindStringSubmatch(strings.TrimSpace(lines[0]))
	if h == nil {
		return VacuumRun{}, false
	}
	r := VacuumRun{Kind: h[2], Aggressive: h[1] != "", Wraparound: h[3] != ""}
	r.Database, r.Schema, r.Relation = splitTableName(h[4])
	if h[5] != "" {
		r.IndexScans = parseInt(h[5])
	}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if m := vacuumPages.FindStringSubmatch(line); m != nil {
			r.PagesRemoved, r.PagesRemain, r.PagesScanned = parseInt(m[1]), parseInt(m[2]), parseInt(m[3])
		} else if m := vacuumTuples.FindStringSubmatch(line); m != nil {
			r.TuplesRemoved, r.TuplesRemain, r.TuplesDeadNotRemovable = parseInt(m[1]), parseInt(m[2]), parseInt(m[3])
		} else if m := vacuumBuffers.FindStringSubmatch(line); m != nil {
			r.BufferHits, r.BufferReads, r.BufferDirtied = parseInt(m[1]), parseInt(m[2]), parseInt(m[3])
		} else if m := vacuumWAL.FindStringSubmatch(line); m != nil {
			r.WALRecords, r.WALFullPageImages, r.WALBytes = parseInt(m[1]), parseInt(m[2]), parseInt(m[3])
		} else if m := vacuumCPU.FindStringSubmatch(line); m != nil {
			r.CPUUserSeconds, r.CPUSystemSeconds, r.ElapsedSeconds = parseFloat(m[1]), parseFloat(m[2]), parseFloat(m[3])
		}
	}
	return r, true
}

func splitTableName(name string) (database, schema, relation string) {
	database, rest, _ := strings.Cut(name, ".")
	schema, relation, _ = strings.Cut(rest, ".")
	return database, schema, relation
}

func parseInt(s string) *int64 {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	return &v
}

func parseFloat(s string) *float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}
