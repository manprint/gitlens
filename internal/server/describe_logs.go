package server

import (
	"context"
	"time"

	"github.com/manprint/pglens/internal/alert"
	"github.com/manprint/pglens/internal/alert/notify"
	"github.com/manprint/pglens/internal/i18n"
	"github.com/manprint/pglens/internal/pglog"
)

// Log lines a notification quotes (plan 007 6.6).
const (
	maxLogLines     = 3
	maxLogLineRunes = 300
	logLookback     = 24 * time.Hour
)

// logClassesFor lists the log classes behind a rule on a log counter, from its
// metric, so a rule a user writes on the same counter gets them too.
func logClassesFor(r alert.Rule) []string {
	switch r.Metric {
	case "pg_log_panic_events":
		return []string{pglog.ClassPanic, pglog.ClassServerCrash}
	case "pg_log_corruption_events":
		return []string{pglog.ClassCorruption}
	case "pg_log_disk_full_events":
		return []string{pglog.ClassDiskFull}
	case "pg_log_oom_events":
		return []string{pglog.ClassMemoryOut}
	}
	return nil
}

// logEvidence quotes the newest log lines of the rule's classes.
func (d *AlertDescriber) logEvidence(ctx context.Context, ev *evidence, a alert.Alert, classes []string) {
	if a.InstanceID == nil {
		return
	}
	now := d.now()
	rows, err := d.db.Query(ctx, `SELECT ts, class, severity, message FROM log_events
 WHERE instance_id = $1 AND class = ANY($2) AND ts > $3 AND ts <= $4
 ORDER BY ts DESC LIMIT $5`, *a.InstanceID, classes, now.Add(-logLookback), now.Add(time.Minute), maxLogLines)
	if err != nil {
		return
	}
	defer rows.Close()
	s := notify.Section{Title: d.t("logs.section", nil)}
	for rows.Next() {
		var ts time.Time
		var class, severity, message string
		if rows.Scan(&ts, &class, &severity, &message) != nil {
			continue
		}
		s.Lines = append(s.Lines, d.t("logs.line", i18n.Args{"at": d.fmtTime(ts), "severity": severity, "class": class, "message": clipText(oneLine(message), maxLogLineRunes)}))
	}
	if rows.Err() != nil {
		return
	}
	if len(s.Lines) == 0 {
		s.Lines = []string{d.t("logs.none", nil)}
	}
	ev.add(s)
}
