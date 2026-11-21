package pglog

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return data
}

// PGL-001: every line of the PG 15 and 18 jsonlog fixtures parses; the unique
// violation carries its SQLSTATE and statement.
func TestPGL001_JSONFixturesParse(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"pg15.jsonl", "pg18.jsonl"} {
		data := readFixture(t, name)
		recs, consumed := Split(FormatJSON, data, MaxLineBytes)
		require.Equal(t, len(data), consumed, name)
		require.Equal(t, bytes.Count(data, []byte("\n")), recs.Len(), name)
		var unique *Entry
		for _, line := range recs.JSON {
			e, err := ParseJSONLine(line, time.UTC)
			require.NoError(t, err, "%s: %s", name, line)
			require.False(t, e.Time.IsZero())
			require.NotEmpty(t, e.Severity)
			if e.SQLState == "23505" {
				unique = &e
			}
		}
		require.NotNil(t, unique, name)
		require.Equal(t, "ERROR", unique.Severity)
		require.Equal(t, "INSERT INTO fx_items VALUES (1, 'b');", unique.Statement)
		require.Equal(t, "postgres", unique.Database)
		require.NotZero(t, unique.PID)
		require.NotEmpty(t, unique.SessionID)
	}
}

// PGL-002: the csvlog fixture keeps a multi-line statement in one record of 26
// columns.
func TestPGL002_CSVFixtureMultiLine(t *testing.T) {
	t.Parallel()
	data := readFixture(t, "pg18.csv")
	recs, consumed := Split(FormatCSV, data, MaxLineBytes)
	require.Equal(t, len(data), consumed)
	require.Zero(t, recs.Bad)
	multi := 0
	for _, fields := range recs.CSV {
		require.Len(t, fields, csvColumns)
		e, err := ParseCSVRecord(fields, time.UTC)
		require.NoError(t, err)
		if strings.Contains(e.Message, "SELECT id,\n") {
			multi++
		}
	}
	require.Positive(t, multi, "the multi-line statement stays one record")
	require.Less(t, len(recs.CSV), bytes.Count(data, []byte("\n")))
}

// PGL-003: the zone word is ignored and the time is read in log_timezone;
// unknown JSON keys are ignored.
func TestPGL003_TimeInLogTimezone(t *testing.T) {
	t.Parallel()
	rome, err := time.LoadLocation("Europe/Rome")
	require.NoError(t, err)
	e, err := ParseJSONLine([]byte(`{"timestamp":"2026-11-21 07:08:11.123 CET","error_severity":"LOG","message":"m","future_key":{"x":1}}`), rome)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 11, 21, 6, 8, 11, 123e6, time.UTC), e.Time)
	require.Equal(t, time.UTC, e.Time.Location())

	got, err := ParseTime("2026-11-21 07:08:11 GMT", nil)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 11, 21, 7, 8, 11, 0, time.UTC), got)
}

// PGL-004: a half-written last line stays unread and consumed counts only
// complete records.
func TestPGL004_SplitLeavesHalfLine(t *testing.T) {
	t.Parallel()
	full := `{"timestamp":"2026-11-21 07:08:11.123 UTC","error_severity":"LOG","message":"a"}` + "\n"
	data := []byte(full + full + `{"timestamp":"2026-11-21 07:08`)
	recs, consumed := Split(FormatJSON, data, MaxLineBytes)
	require.Len(t, recs.JSON, 2)
	require.Equal(t, 2*len(full), consumed)

	csvFull := "2026-11-21 07:08:11.123 UTC,u,d,1,,s.1,1,,,,0,LOG,00000,\"ok\",,,,,,,,,app,client backend,,0\n"
	csvData := []byte(csvFull + "2026-11-21 07:08:12.000 UTC,u,d,1,,s.1,2,,,,0,LOG,00000,\"open quote\nstill")
	crecs, ccons := Split(FormatCSV, csvData, MaxLineBytes)
	require.Len(t, crecs.CSV, 1)
	require.Equal(t, len(csvFull), ccons)

	noNewline := []byte(strings.TrimSuffix(csvFull, "\n"))
	crecs, ccons = Split(FormatCSV, noNewline, MaxLineBytes)
	require.Zero(t, crecs.Len())
	require.Zero(t, ccons)
}

// PGL-005: a 23-column record is accepted with the missing columns empty; a
// broken line is counted and skipped.
func TestPGL005_CSVShortRecordAndGarbage(t *testing.T) {
	t.Parallel()
	short := "2026-11-21 07:08:11.123 UTC,u,d,7,,s.1,3,,,,0,ERROR,42P01,\"relation missing\",,,,,,\"SELECT 1\",,,app\n"
	data := []byte(short + "garbage \"x\" here\n" + short)
	recs, consumed := Split(FormatCSV, data, MaxLineBytes)
	require.Equal(t, len(data), consumed)
	require.Equal(t, 1, recs.Bad)
	require.Len(t, recs.CSV, 2)
	e, err := ParseCSVRecord(recs.CSV[0], time.UTC)
	require.NoError(t, err)
	require.Equal(t, "42P01", e.SQLState)
	require.Equal(t, "SELECT 1", e.Statement)
	require.Equal(t, "app", e.ApplicationName)
	require.Empty(t, e.BackendType)
	require.Zero(t, e.QueryID)

	_, err = ParseCSVRecord([]string{"too", "few"}, time.UTC)
	require.True(t, errors.Is(err, ErrBadRecord))
}

// PGL-015 (I-9): a 300 000-byte line is cut at 262 144 bytes and still gives
// an entry.
func TestPGL015_LongLineCut(t *testing.T) {
	t.Parallel()
	long := `{"timestamp":"2026-11-21 07:08:11 UTC","error_severity":"ERROR","message":"` + strings.Repeat("x", 300000) + `"}`
	recs, consumed := Split(FormatJSON, []byte(long+"\n"), MaxLineBytes)
	require.Equal(t, len(long)+1, consumed)
	require.Len(t, recs.JSON, 1)
	require.Len(t, recs.JSON[0], MaxLineBytes)
	e, err := ParseJSONLine(recs.JSON[0], time.UTC)
	require.ErrorIs(t, err, ErrBadRecord)
	require.Equal(t, "LOG", e.Severity)
	require.Len(t, e.Message, MaxLineBytes)

	field := strings.Repeat("y", 300000)
	row := "2026-11-21 07:08:11 UTC,u,d,1,,s.1,1,,,,0,LOG,00000,\"" + field + "\",,,,,,,,,app,client backend,,0\n"
	crecs, _ := Split(FormatCSV, []byte(row), MaxLineBytes)
	require.Len(t, crecs.CSV, 1)
	require.Len(t, crecs.CSV[0][csvMessage], MaxLineBytes)
}
