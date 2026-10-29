package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// dataDirTimeout bounds the SHOW data_directory round trip, so a slow target
// never delays the host collection loop by more than this.
const dataDirTimeout = 3 * time.Second

// ErrDataDirNotReadable is returned when the monitoring role may not read the
// data_directory setting. The agent never asks for more privileges: the host
// disk metrics are left out instead.
var ErrDataDirNotReadable = errors.New("data_directory is not readable by the monitoring role")

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// DataDirectory returns the target's data directory, cleaned. The first
// answer is cached for the life of the manager; a failure is not cached, so
// the next host tick asks again.
func (m *Manager) DataDirectory(ctx context.Context) (string, error) {
	m.dataDirMu.Lock()
	cached := m.dataDir
	m.dataDirMu.Unlock()
	if cached != "" {
		return cached, nil
	}
	ctx, cancel := context.WithTimeout(ctx, dataDirTimeout)
	defer cancel()
	conn, err := m.Shared(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Release()
	return m.readDataDirectory(ctx, conn)
}

func (m *Manager) readDataDirectory(ctx context.Context, conn queryRower) (string, error) {
	dir, err := queryDataDirectory(ctx, conn)
	if err != nil {
		return "", err
	}
	m.dataDirMu.Lock()
	m.dataDir = dir
	m.dataDirMu.Unlock()
	return dir, nil
}

func queryDataDirectory(ctx context.Context, conn queryRower) (string, error) {
	var dir string
	if err := conn.QueryRow(ctx, "SHOW data_directory").Scan(&dir); err != nil {
		var pgErr *pgconn.PgError
		// 42501 insufficient_privilege: a role without pg_read_all_settings.
		if errors.As(err, &pgErr) && pgErr.Code == "42501" {
			return "", ErrDataDirNotReadable
		}
		return "", fmt.Errorf("show data_directory: %w", err)
	}
	if dir == "" || !filepath.IsAbs(dir) {
		return "", fmt.Errorf("data_directory %q is not an absolute path", dir)
	}
	return filepath.Clean(dir), nil
}
