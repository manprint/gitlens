package pglog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// PGL-006: one entry per class gives that class, and the table covers every
// name Classes returns.
func TestPGL006_OneEntryPerClass(t *testing.T) {
	t.Parallel()
	cases := map[string]Entry{
		ClassDeadlock:             {Severity: "ERROR", SQLState: "40P01", Message: "deadlock detected"},
		ClassSerializationFailure: {Severity: "ERROR", SQLState: "40001", Message: "could not serialize access"},
		ClassStatementTimeout:     {Severity: "ERROR", SQLState: "57014", Message: "canceling statement due to statement timeout"},
		ClassStatementCanceled:    {Severity: "ERROR", SQLState: "57014", Message: "canceling statement due to user request"},
		ClassLockTimeout:          {Severity: "ERROR", SQLState: "55P03", Message: "canceling statement due to lock timeout"},
		ClassIdleTxTimeout:        {Severity: "FATAL", SQLState: "25P03", Message: "terminating connection due to idle-in-transaction timeout"},
		ClassAuthFailed:           {Severity: "FATAL", SQLState: "28P01", Message: "password authentication failed for user \"u\""},
		ClassConnectionLimit:      {Severity: "FATAL", SQLState: "53300", Message: "sorry, too many clients already"},
		ClassDiskFull:             {Severity: "ERROR", SQLState: "53100", Message: "could not extend file"},
		ClassMemoryOut:            {Severity: "ERROR", SQLState: "53200", Message: "out of memory"},
		ClassCorruption:           {Severity: "ERROR", SQLState: "XX001", Message: "invalid page in block 7"},
		ClassAutoExplainPlan:      {Severity: "LOG", Message: "duration: 12.3 ms  plan:\nQuery Text: SELECT 1"},
		ClassSlowStatement:        {Severity: "LOG", Message: "duration: 12.3 ms  statement: SELECT 1"},
		ClassAutovacuumRun:        {Severity: "LOG", Message: "automatic vacuum of table \"d.public.t\": index scans: 0"},
		ClassCheckpointFrequent:   {Severity: "LOG", Message: "checkpoints are occurring too frequently (9 seconds apart)"},
		ClassCheckpoint:           {Severity: "LOG", Message: "checkpoint complete: wrote 3 buffers"},
		ClassTempFile:             {Severity: "LOG", Message: "temporary file: path \"base/pgsql_tmp/x\", size 1024"},
		ClassLockWait:             {Severity: "LOG", Message: "process 4242 still waiting for ShareLock on transaction 7 after 1000.1 ms"},
		ClassServerCrash:          {Severity: "LOG", Message: "server process (PID 9) was terminated by signal 9: Killed"},
		ClassReplicationConflict:  {Severity: "FATAL", Message: "terminating connection due to conflict with recovery"},
		ClassWraparoundWarning:    {Severity: "WARNING", Message: "database \"d\" must be vacuumed within 9 transactions"},
		ClassServerStart:          {Severity: "LOG", Message: "database system is ready to accept connections"},
		ClassServerRecovery:       {Severity: "LOG", Message: "database system was interrupted; last known up at x"},
		ClassPanic:                {Severity: "PANIC", Message: "could not locate a valid checkpoint record"},
		ClassFatalOther:           {Severity: "FATAL", SQLState: "57P01", Message: "terminating connection due to administrator command"},
		ClassErrorStatement:       {Severity: "ERROR", SQLState: "23505", Message: "duplicate key value violates unique constraint \"k\""},
		ClassWarningOther:         {Severity: "WARNING", SQLState: "01000", Message: "something odd"},
	}
	for class, e := range cases {
		require.Equal(t, class, Classify(e), "%+v", e)
	}
	require.Len(t, cases, len(Classes()))
	for _, c := range Classes() {
		_, ok := cases[c]
		require.True(t, ok, "class %q has no case", c)
	}
	require.IsIncreasing(t, Classes())
	require.False(t, Shipped(ClassNone))
	require.False(t, Shipped(ClassAutoExplainPlan))
	require.True(t, Shipped(ClassErrorStatement))
}

// PGL-007: SQLSTATE wins over severity; 57014 splits on the message.
func TestPGL007_Precedence(t *testing.T) {
	t.Parallel()
	require.Equal(t, ClassDeadlock, Classify(Entry{Severity: "ERROR", SQLState: "40P01", Message: "deadlock detected"}))
	require.Equal(t, ClassStatementCanceled, Classify(Entry{Severity: "ERROR", SQLState: "57014", Message: "canceling statement due to user request"}))
	require.Equal(t, ClassDiskFull, Classify(Entry{Severity: "ERROR", SQLState: "58030", Message: "could not write: No space left on device"}))
	require.Equal(t, ClassSlowStatement, Classify(Entry{Severity: "LOG", Message: "  duration: 1 ms  statement: SELECT 'plan:'\n plan:"}))
}

// PGL-008: an unmatched LOG line is no event; a German message is still
// classified by its SQLSTATE.
func TestPGL008_NoEventAndOtherLanguage(t *testing.T) {
	t.Parallel()
	require.Equal(t, ClassNone, Classify(Entry{Severity: "LOG", SQLState: "00000", Message: "connection received: host=[local]"}))
	require.Equal(t, ClassDiskFull, Classify(Entry{Severity: "FEHLER", SQLState: "53100", Message: "konnte Datei nicht erweitern: Auf dem Gerät ist kein Speicherplatz mehr verfügbar"}))
}
