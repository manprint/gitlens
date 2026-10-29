package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v5"
	"github.com/stretchr/testify/require"
)

func newDataDirMock(t *testing.T) pgxmock.PgxConnIface {
	t.Helper()
	mock, err := pgxmock.NewConn()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()) })
	return mock
}

// TestHOSTDD001_DataDirectoryReturnsTheCleanedPath: a trailing slash is
// dropped, and the answer is cached so no further query is made.
func TestHOSTDD001_DataDirectoryReturnsTheCleanedPath(t *testing.T) {
	mock := newDataDirMock(t)
	mock.ExpectQuery("SHOW data_directory").
		WillReturnRows(pgxmock.NewRows([]string{"data_directory"}).AddRow("/var/lib/postgresql/18/docker/"))

	m := &Manager{}
	dir, err := m.readDataDirectory(context.Background(), mock)
	require.NoError(t, err)
	require.Equal(t, "/var/lib/postgresql/18/docker", dir)

	// Cached: DataDirectory answers without a pool or a query.
	cached, err := m.DataDirectory(context.Background())
	require.NoError(t, err)
	require.Equal(t, dir, cached)
}

// TestHOSTDD002_DataDirectoryErrorIsNotCached: a failed read is returned and
// the next call queries again.
func TestHOSTDD002_DataDirectoryErrorIsNotCached(t *testing.T) {
	mock := newDataDirMock(t)
	mock.ExpectQuery("SHOW data_directory").WillReturnError(errors.New("connection reset"))
	mock.ExpectQuery("SHOW data_directory").
		WillReturnRows(pgxmock.NewRows([]string{"data_directory"}).AddRow("/pgdata"))

	m := &Manager{}
	_, err := m.readDataDirectory(context.Background(), mock)
	require.ErrorContains(t, err, "connection reset")
	require.Empty(t, m.dataDir)

	dir, err := m.readDataDirectory(context.Background(), mock)
	require.NoError(t, err)
	require.Equal(t, "/pgdata", dir)
}

// TestHOSTDD003_DataDirectoryRejectsRelativeOrEmpty: only an absolute path
// can be measured on the agent's side; a denied read names the role.
func TestHOSTDD003_DataDirectoryRejectsRelativeOrEmpty(t *testing.T) {
	for _, value := range []string{"", "pgdata", "./data"} {
		mock := newDataDirMock(t)
		mock.ExpectQuery("SHOW data_directory").
			WillReturnRows(pgxmock.NewRows([]string{"data_directory"}).AddRow(value))
		m := &Manager{}
		_, err := m.readDataDirectory(context.Background(), mock)
		require.ErrorContains(t, err, "not an absolute path", value)
		require.Empty(t, m.dataDir)
	}

	mock := newDataDirMock(t)
	mock.ExpectQuery("SHOW data_directory").WillReturnError(&pgconn.PgError{Code: "42501", Message: "permission denied"})
	_, err := (&Manager{}).readDataDirectory(context.Background(), mock)
	require.ErrorIs(t, err, ErrDataDirNotReadable)
	require.EqualError(t, err, "data_directory is not readable by the monitoring role")
}
