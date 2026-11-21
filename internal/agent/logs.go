package agent

import (
	"context"
	"path"
	"slices"
	"strings"

	"github.com/manprint/pglens/internal/pglog"
)

// logSettingsQuery reads the settings the log reader needs, at start and
// hourly (plan 007 phase 6).
const logSettingsQuery = `/* pglens: logs, settings */ SELECT current_setting('data_directory'), current_setting('log_directory'),
       current_setting('log_destination'), current_setting('logging_collector'),
       current_setting('log_timezone'), current_setting('lc_messages')`

// LogSettings are the server settings the log reader reads.
type LogSettings struct {
	DataDirectory, LogDirectory, Destination, Collector, Timezone, Messages string
}

// Format picks jsonlog over csvlog; "" when the collector writes neither.
func (s LogSettings) Format() string {
	if s.Collector != "on" {
		return ""
	}
	dests := strings.Split(strings.ReplaceAll(s.Destination, " ", ""), ",")
	switch {
	case slices.Contains(dests, pglog.FormatJSON):
		return pglog.FormatJSON
	case slices.Contains(dests, pglog.FormatCSV):
		return pglog.FormatCSV
	}
	return ""
}

// LogDir is the log directory: log_directory, under data_directory when
// relative.
func (s LogSettings) LogDir() string {
	if path.IsAbs(s.LogDirectory) {
		return s.LogDirectory
	}
	return path.Join(s.DataDirectory, s.LogDirectory)
}

// EnglishMessages reports whether lc_messages keeps the English server
// messages the classifier's text rules read.
func (s LogSettings) EnglishMessages() bool {
	lc := s.Messages
	return lc == "" || lc == "C" || lc == "POSIX" || strings.HasPrefix(lc, "C.") || strings.HasPrefix(lc, "en_")
}

// ReadLogSettings reads the log settings on the shared connection.
func (m *Manager) ReadLogSettings(ctx context.Context) (LogSettings, error) {
	var s LogSettings
	err := m.LogQuerier().QueryRow(ctx, logSettingsQuery).Scan(&s.DataDirectory, &s.LogDirectory, &s.Destination, &s.Collector, &s.Timezone, &s.Messages)
	return s, err
}

// LogQuerier adapts the shared connection to pglog.Querier: each query
// acquires it and releases it once its row is scanned.
func (m *Manager) LogQuerier() pglog.Querier { return logQuerier{m} }

type logQuerier struct{ m *Manager }

type rowErr struct{ err error }

func (r rowErr) Scan(...any) error { return r.err }

type releasingRow struct {
	row     pglog.Row
	release func()
}

func (r releasingRow) Scan(dest ...any) error {
	defer r.release()
	return r.row.Scan(dest...)
}

func (q logQuerier) QueryRow(ctx context.Context, sql string, args ...any) pglog.Row {
	conn, err := q.m.Shared(ctx)
	if err != nil {
		return rowErr{err}
	}
	return releasingRow{row: conn.QueryRow(ctx, sql, args...), release: conn.Release}
}
