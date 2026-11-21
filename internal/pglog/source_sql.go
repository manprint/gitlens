package pglog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"sync"
)

// Row is one result row; pgx.Row satisfies it.
type Row interface {
	Scan(dest ...any) error
}

// Querier runs one query returning one row. pglog imports no database driver
// (D9): the agent adapts its connection to this interface.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) Row
}

const (
	currentLogFileSQL = `/* pglens: logs, current file */ SELECT pg_current_logfile($1)`
	logFileSizeSQL    = `/* pglens: logs, file size */ SELECT size FROM pg_ls_logdir() WHERE name = $1`
	logFileReadSQL    = `/* pglens: logs, read */ SELECT pg_read_binary_file($1, $2, $3, true)`
)

// privilegeSQLState is insufficient_privilege.
const privilegeSQLState = "42501"

type sqlSource struct {
	q      Querier
	format string

	mu  sync.Mutex
	dir string // directory part of pg_current_logfile, as the server names it
}

// NewSQLSource reads the log of format (FormatJSON or FormatCSV) through the
// server: pg_current_logfile, pg_ls_logdir and pg_read_binary_file. A
// permission error is ErrLogReadDenied.
func NewSQLSource(q Querier, format string) Source {
	return &sqlSource{q: q, format: format}
}

func denied(err error) error {
	var pe interface{ SQLState() string }
	if errors.As(err, &pe) && pe.SQLState() == privilegeSQLState {
		return fmt.Errorf("%w: %w", ErrLogReadDenied, err)
	}
	return err
}

func (s *sqlSource) Current(ctx context.Context) (string, int64, error) {
	var p *string
	if err := s.q.QueryRow(ctx, currentLogFileSQL, s.format).Scan(&p); err != nil {
		return "", 0, denied(err)
	}
	if p == nil || *p == "" {
		return "", 0, ErrNoLogFile
	}
	s.mu.Lock()
	s.dir = path.Dir(*p)
	s.mu.Unlock()
	name := path.Base(*p)
	size, ok, err := s.Size(ctx, name)
	if err != nil {
		return "", 0, err
	}
	if !ok {
		return "", 0, fmt.Errorf("%w: %s missing", ErrNoLogFile, name)
	}
	return name, size, nil
}

func (s *sqlSource) Size(ctx context.Context, name string) (int64, bool, error) {
	if err := checkName(name); err != nil {
		return 0, false, err
	}
	var size *int64
	err := s.q.QueryRow(ctx, logFileSizeSQL, name).Scan(&size)
	if err != nil {
		if isNoRows(err) {
			return 0, false, nil
		}
		return 0, false, denied(err)
	}
	if size == nil {
		return 0, false, nil
	}
	return *size, true, nil
}

func (s *sqlSource) ReadAt(ctx context.Context, name string, off, n int64) ([]byte, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	s.mu.Lock()
	dir := s.dir
	s.mu.Unlock()
	if dir == "" {
		return nil, ErrNoLogFile
	}
	var data []byte
	if err := s.q.QueryRow(ctx, logFileReadSQL, path.Join(dir, name), off, n).Scan(&data); err != nil {
		return nil, denied(err)
	}
	return data, nil
}

func checkName(name string) error {
	if name == "" || name == "." || name != path.Base(name) || name == ".." {
		return fmt.Errorf("%w: %q", ErrBadLogName, name)
	}
	return nil
}

// isNoRows reports a query that returned no row; pgx.ErrNoRows wraps
// sql.ErrNoRows.
func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
