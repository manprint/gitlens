package pglog

import (
	"regexp"
	"sort"
	"strings"
)

// Event classes. ClassNone ("") means the line is not an event.
const (
	ClassNone                 = ""
	ClassDeadlock             = "lock.deadlock"
	ClassSerializationFailure = "serialization.failure"
	ClassStatementTimeout     = "statement.timeout"
	ClassStatementCanceled    = "statement.canceled"
	ClassLockTimeout          = "lock.timeout"
	ClassIdleTxTimeout        = "idle_tx.timeout"
	ClassAuthFailed           = "auth.failed"
	ClassConnectionLimit      = "connection.limit"
	ClassDiskFull             = "disk.full"
	ClassMemoryOut            = "memory.out"
	ClassCorruption           = "corruption"
	ClassAutoExplainPlan      = "autoexplain.plan"
	ClassSlowStatement        = "slow.statement"
	ClassAutovacuumRun        = "autovacuum.run"
	ClassCheckpointFrequent   = "checkpoint.frequent"
	ClassCheckpoint           = "checkpoint"
	ClassTempFile             = "temp_file"
	ClassLockWait             = "lock.wait"
	ClassServerCrash          = "server.crash"
	ClassReplicationConflict  = "replication.conflict"
	ClassWraparoundWarning    = "wraparound.warning"
	ClassServerStart          = "server.start"
	ClassServerRecovery       = "server.recovery"
	ClassPanic                = "panic"
	ClassFatalOther           = "fatal.other"
	ClassErrorStatement       = "error.statement"
	ClassWarningOther         = "warning.other"
)

// Rule 1: SQLSTATE codes, valid in any server language.
var bySQLState = map[string]string{
	"40P01": ClassDeadlock,
	"40001": ClassSerializationFailure,
	"55P03": ClassLockTimeout,
	"25P03": ClassIdleTxTimeout,
	"28P01": ClassAuthFailed,
	"28000": ClassAuthFailed,
	"53300": ClassConnectionLimit,
	"53100": ClassDiskFull,
	"53200": ClassMemoryOut,
	"XX001": ClassCorruption,
	"XX002": ClassCorruption,
}

type messageRule struct {
	match func(msg, firstLine string) bool
	class string
}

func prefix(p ...string) func(string, string) bool {
	return func(msg, _ string) bool {
		for _, s := range p {
			if strings.HasPrefix(msg, s) {
				return true
			}
		}
		return false
	}
}

func contains(p string) func(string, string) bool {
	return func(msg, _ string) bool { return strings.Contains(msg, p) }
}

var lockWait = regexp.MustCompile(`^process \d+ still waiting for`)

// Rule 2: English message texts, in order.
var byMessage = []messageRule{
	{func(msg, first string) bool {
		return strings.HasPrefix(msg, "duration: ") && strings.Contains(first, " plan:")
	}, ClassAutoExplainPlan},
	{prefix("duration: "), ClassSlowStatement},
	{prefix("automatic vacuum", "automatic aggressive vacuum", "automatic analyze"), ClassAutovacuumRun},
	{prefix("checkpoints are occurring too frequently"), ClassCheckpointFrequent},
	{prefix("checkpoint starting:", "checkpoint complete:"), ClassCheckpoint},
	{prefix("temporary file:"), ClassTempFile},
	{func(msg, _ string) bool { return lockWait.MatchString(msg) }, ClassLockWait},
	{contains("was terminated by signal"), ClassServerCrash},
	{contains("conflict with recovery"), ClassReplicationConflict},
	{contains("must be vacuumed within"), ClassWraparoundWarning},
	{contains("No space left on device"), ClassDiskFull},
	{prefix("out of memory"), ClassMemoryOut},
	{prefix("database system is ready to accept connections"), ClassServerStart},
	{prefix("database system was interrupted", "database system was not properly shut down"), ClassServerRecovery},
}

// Rule 3: severity.
var bySeverity = map[string]string{
	"PANIC":   ClassPanic,
	"FATAL":   ClassFatalOther,
	"ERROR":   ClassErrorStatement,
	"WARNING": ClassWarningOther,
}

// Classify gives the event class of a log line: SQLSTATE first, then the
// English message text, then the severity; the first match wins. ClassNone
// means the line is not an event.
func Classify(e Entry) string {
	if e.SQLState == "57014" {
		if strings.Contains(e.Message, "statement timeout") {
			return ClassStatementTimeout
		}
		return ClassStatementCanceled
	}
	if c, ok := bySQLState[e.SQLState]; ok {
		return c
	}
	msg := strings.TrimSpace(e.Message)
	first, _, _ := strings.Cut(msg, "\n")
	for _, r := range byMessage {
		if r.match(msg, first) {
			return r.class
		}
	}
	return bySeverity[e.Severity]
}

var allClasses = func() []string {
	seen := map[string]bool{ClassStatementTimeout: true, ClassStatementCanceled: true}
	for _, c := range bySQLState {
		seen[c] = true
	}
	for _, r := range byMessage {
		seen[r.class] = true
	}
	for _, c := range bySeverity {
		seen[c] = true
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}()

// Classes returns every class name, sorted.
func Classes() []string { return append([]string(nil), allClasses...) }

// Shipped reports whether a line of this class becomes a log_event fact.
func Shipped(class string) bool {
	return class != ClassNone && class != ClassAutoExplainPlan
}
