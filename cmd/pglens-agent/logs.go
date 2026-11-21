package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"time"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pglog"
	"github.com/manprint/pglens/internal/wire"
)

// Coverage notes of the logs check, sent as its skip reason.
const (
	logsNoStructuredFormat = "logs_no_structured_format"
	logsFileUnreadable     = "logs_file_unreadable"
	logsSQLDenied          = "logs_sql_denied"
	logsMessagesNotEnglish = "logs_messages_not_english"
	logsTimezoneUnknown    = "logs_timezone_unknown"
)

// logSettingsEvery is how often the loop reads the server settings again.
const logSettingsEvery = time.Hour

// logLoop is one target's log reader. settings and source are swapped in
// tests.
type logLoop struct {
	target   string
	cfg      agent.CheckConfig
	state    *logState
	clk      clock.Clock
	settings func(ctx context.Context) (agent.LogSettings, error)
	// source builds the reader of a format; nil and a coverage note when the
	// log cannot be read that way.
	source func(format string, s agent.LogSettings) (pglog.Source, string)
	emit   func(wire.Result)
	logf   func(format string, args ...any)

	cur      agent.LogSettings
	readAt   time.Time
	loc      *time.Location
	locKnown bool
	plans    planMemo
}

// logState holds the saved positions of every target in one file, shared by
// the targets' loops so a save never writes back another target's stale
// position.
type logState struct {
	path      string
	mu        sync.Mutex
	positions map[string]pglog.Position
}

var (
	logStatesMu sync.Mutex
	logStates   = map[string]*logState{}
)

// sharedLogState returns the one logState of a state file.
func sharedLogState(path string) *logState {
	logStatesMu.Lock()
	defer logStatesMu.Unlock()
	st, ok := logStates[path]
	if !ok {
		st = &logState{path: path}
		logStates[path] = st
	}
	return st
}

func (st *logState) get(target string, logf func(string, ...any)) (pglog.Position, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.positions == nil {
		m, err := pglog.LoadPositions(st.path)
		if err != nil {
			logf("warning: logs: %v; reading restarts at the end of the log", err)
		}
		st.positions = m
	}
	pos, ok := st.positions[target]
	return pos, ok
}

func (st *logState) set(target string, pos pglog.Position) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.positions[target] = pos
	return pglog.SavePositions(st.path, st.positions)
}

func (l *logLoop) note(reason string) {
	l.emit(wire.Result{Check: logsCheck, TS: l.clk.Now(), SkipReason: reason})
}

func (l *logLoop) refreshSettings(ctx context.Context) error {
	if !l.readAt.IsZero() && l.clk.Now().Sub(l.readAt) < logSettingsEvery {
		return nil
	}
	s, err := l.settings(ctx)
	if err != nil {
		return err
	}
	l.cur, l.readAt = s, l.clk.Now()
	l.loc, l.locKnown = time.UTC, true
	if loc, err := time.LoadLocation(s.Timezone); err == nil {
		l.loc = loc
	} else {
		l.locKnown = false
	}
	return nil
}

// scrape runs one interval: settings, read, parse, classify, emit, save.
func (l *logLoop) scrape(ctx context.Context) {
	if err := l.refreshSettings(ctx); err != nil {
		l.logf("warning: logs: settings of %s: %v", l.target, err)
		return
	}
	format := l.cur.Format()
	if format == "" {
		l.note(logsNoStructuredFormat)
		return
	}
	src, unreadable := l.source(format, l.cur)
	if src == nil {
		l.note(unreadable)
		return
	}
	pos, seen := l.state.get(l.target, l.logf)
	chunks, next, skipped, err := pglog.Advance(ctx, src, pos, !seen, l.cfg.LogBudget())
	if err != nil && len(chunks) == 0 {
		switch {
		case errors.Is(err, pglog.ErrLogReadDenied):
			l.note(logsSQLDenied)
		case errors.Is(err, pglog.ErrNoLogFile):
			l.note(logsNoStructuredFormat)
		case l.cfg.LogSource() == agent.LogSourceFile:
			l.logf("warning: logs: %s: cannot read the log directory %s: %v", l.target, l.logDir(), err)
			l.note(logsFileUnreadable)
		default:
			l.logf("warning: logs: read %s: %v", l.target, err)
		}
		return
	}
	scrape := logScrape{bytesSkipped: skipped, statementText: l.cfg.LogStatementText(), plans: &l.plans}
	for _, c := range chunks {
		recs, consumed := pglog.Split(format, c.Data, pglog.MaxLineBytes)
		scrape.bytesRead += int64(consumed)
		next = pglog.Settle(next, c, consumed)
		for _, line := range recs.JSON {
			// A line cut at MaxLineBytes is no JSON object but still an entry.
			if e, err := pglog.ParseJSONLine(line, l.loc); err == nil || e.Message != "" {
				scrape.entries = append(scrape.entries, e)
			}
		}
		for _, fields := range recs.CSV {
			if e, err := pglog.ParseCSVRecord(fields, l.loc); err == nil {
				scrape.entries = append(scrape.entries, e)
			}
		}
	}
	if skipped > 0 {
		l.logf("warning: logs: %s: backlog over 16 × max_bytes_per_interval, skipped %d bytes", l.target, skipped)
	}
	result, formatNotJSON := logResult(scrape, l.clk.Now())
	l.emit(result)
	if formatNotJSON {
		l.note(autoExplainFormatNotJSON)
	}
	switch {
	case !l.cur.EnglishMessages():
		l.note(logsMessagesNotEnglish)
	case !l.locKnown:
		l.note(logsTimezoneUnknown)
	}
	if err := l.state.set(l.target, next); err != nil {
		l.logf("warning: logs: save position: %v", err)
	}
}

func (l *logLoop) logDir() string {
	if l.cfg.Path != "" {
		return l.cfg.Path
	}
	return l.cur.LogDir()
}

// newLogSource builds the reader of checks.logs.source. The file source
// reads the log directory directly and needs the target on this host; both
// name the current file through pg_current_logfile.
func newLogSource(q pglog.Querier, cfg agent.CheckConfig, local bool, dir func() string) func(string, agent.LogSettings) (pglog.Source, string) {
	return func(format string, _ agent.LogSettings) (pglog.Source, string) {
		sql := pglog.NewSQLSource(q, format)
		if cfg.LogSource() == agent.LogSourceSQL {
			return sql, ""
		}
		if !local {
			return nil, logsFileUnreadable
		}
		return pglog.NewFileSource(filepath.Clean(dir()), func(ctx context.Context) (string, error) {
			name, _, err := sql.Current(ctx)
			return name, err
		}), ""
	}
}

// startLogs runs the log reader of one target every checks.logs.interval
// when checks.logs.enabled is true; its results join the target's pending
// results under mu (plan 007 A20).
func startLogs(ctx context.Context, mgr *agent.Manager, tc agent.TargetConfig, cfg *agent.Config, clk clock.Clock, mu *sync.Mutex, pending map[string][]wire.Result, stderr io.Writer) bool {
	logsCfg := cfg.Checks[agent.LogsCheck]
	if !logsCfg.LogsEnabled() {
		return false
	}
	l := &logLoop{
		target: tc.Name, cfg: logsCfg, state: sharedLogState(cfg.LogStatePath()), clk: clk,
		settings: mgr.ReadLogSettings,
		emit: func(r wire.Result) {
			mu.Lock()
			pending[tc.Name] = append(pending[tc.Name], r)
			mu.Unlock()
		},
		logf: func(format string, args ...any) { _, _ = fmt.Fprintf(stderr, format+"\n", args...) },
	}
	l.source = newLogSource(mgr.LogQuerier(), logsCfg, agent.TargetIsLocal(tc), l.logDir)
	go func() {
		ticker := clk.NewTicker(cfg.LogInterval())
		defer ticker.Stop()
		for {
			l.scrape(ctx)
			select {
			case <-ticker.C():
			case <-ctx.Done():
				return
			}
		}
	}()
	return true
}
