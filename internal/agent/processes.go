package agent

import (
	"context"
	"time"
)

// processQueryTimeout bounds each of the two host process queries.
const processQueryTimeout = 3 * time.Second

const backendPIDQuery = `/* pglens: host processes, backend pid */ SELECT pg_backend_pid()`

const processSessionsQuery = `/* pglens: host processes, sessions */
SELECT pid, COALESCE(backend_type, ''), COALESCE(datname, ''), COALESCE(usename, ''),
       COALESCE(application_name, ''), COALESCE(state, ''), query_id
FROM pg_stat_activity`

// ProcessSession is the pg_stat_activity row of one server process.
type ProcessSession struct {
	BackendType, Datname, Usename, ApplicationName, State string
	QueryID                                               *int64
}

// BackendPID returns the pid of the agent's own backend; its parent in /proc
// is the postmaster.
func (m *Manager) BackendPID(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, processQueryTimeout)
	defer cancel()
	conn, err := m.Shared(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Release()
	var pid int32
	if err := conn.QueryRow(ctx, backendPIDQuery).Scan(&pid); err != nil {
		return 0, err
	}
	return int(pid), nil
}

// ProcessSessions returns the session of every server process by pid.
func (m *Manager) ProcessSessions(ctx context.Context) (map[int]ProcessSession, error) {
	ctx, cancel := context.WithTimeout(ctx, processQueryTimeout)
	defer cancel()
	conn, err := m.Shared(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	rows, err := conn.Query(ctx, processSessionsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int]ProcessSession)
	for rows.Next() {
		var (
			pid int32
			s   ProcessSession
		)
		if err := rows.Scan(&pid, &s.BackendType, &s.Datname, &s.Usename, &s.ApplicationName, &s.State, &s.QueryID); err != nil {
			return nil, err
		}
		out[int(pid)] = s
	}
	return out, rows.Err()
}
