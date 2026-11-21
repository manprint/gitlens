package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/alert"
)

// PGL-027
func TestPGL027DescriberQuotesLogLines(t *testing.T) {
	long := "could not write to file \"base/16384/2619\": No space left on device " + strings.Repeat("x", 400)
	f := baseFake()
	f.rows["FROM log_events"] = [][]any{
		{descNow.Add(-time.Minute), "disk.full", "PANIC", long},
		{descNow.Add(-2 * time.Minute), "disk.full", "ERROR", "could not extend file\n  \"base/16384/16385\""},
		{descNow.Add(-3 * time.Minute), "disk.full", "ERROR", "third"},
	}
	seededRule(f, "critical", "instance", "pg_log_disk_full_events", "gt", 0, 0, "", "")
	a := alert.Alert{RuleID: "log.disk_full", Severity: alert.SeverityCritical, InstanceID: &descInst, Value: 2, StartedAt: descNow}

	det := newTestDescriber("en", f).Describe(context.Background(), a, "fire")
	text := allLines(det)
	require.Contains(t, det.Title, "Disk full in the PostgreSQL log")
	require.Contains(t, text, "Latest PostgreSQL log lines")
	require.Contains(t, text, "2026-11-20 14:59:00 UTC PANIC [disk.full] could not write to file")
	require.Contains(t, text, `2026-11-20 14:58:00 UTC ERROR [disk.full] could not extend file "base/16384/16385"`)
	require.Contains(t, text, "ERROR [disk.full] third")
	require.NotContains(t, text, strings.Repeat("x", 300))
	require.Equal(t, "PostgreSQL logs › pg1", fieldMap(det)["In the console"])
	for _, line := range det.Sections[0].Lines {
		require.LessOrEqual(t, len([]rune(line)), 300+len("2026-11-20 14:59:00 UTC PANIC [disk.full] "), line)
	}

	it := newTestDescriber("it", f).Describe(context.Background(), a, "fire")
	require.Contains(t, it.Title, "Disco pieno nel log PostgreSQL")
	require.Contains(t, allLines(it), "Ultime righe del log PostgreSQL")
	require.Contains(t, allLines(it), "[disk.full] third")

	f.rows["FROM log_events"] = nil
	require.Contains(t, allLines(newTestDescriber("en", f).Describe(context.Background(), a, "fire")), "No matching line")
	f.errs["FROM log_events"] = errors.New("x")
	require.NotContains(t, allLines(newTestDescriber("en", f).Describe(context.Background(), a, "fire")), "Latest PostgreSQL log lines")

	for metric, classes := range map[string][]string{
		"pg_log_panic_events":      {"panic", "server.crash"},
		"pg_log_corruption_events": {"corruption"},
		"pg_log_disk_full_events":  {"disk.full"},
		"pg_log_oom_events":        {"memory.out"},
		"pg_deadlocks_total":       nil,
	} {
		require.Equal(t, classes, logClassesFor(alert.Rule{Metric: metric}), metric)
	}
}
