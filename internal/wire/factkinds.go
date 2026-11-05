package wire

import "sort"

// BloatSkippedTablesMetric is the gauge of the tables bloat_estimate cannot
// estimate because the monitoring role cannot read their column statistics.
// The server stores it as a generic metric of the database, not as a row of
// metrics_bloat.
const BloatSkippedTablesMetric = "pg_bloat_skipped_tables"

// BloatUnreadableFact is the fact kind that names those tables. A scrape
// rewrites the one fact of each database, so readers judge it by its age.
const BloatUnreadableFact = "bloat_unreadable"

// BloatSkippedValue is the value of a BloatUnreadableFact: how many tables of
// the database the bloat estimate cannot read, and the largest of them.
type BloatSkippedValue struct {
	Count  int64               `json:"count"`
	Tables []BloatSkippedTable `json:"tables"`
}

// BloatSkippedTable names one table of a BloatSkippedValue.
type BloatSkippedTable struct {
	SchemaName string `json:"schemaname"`
	RelName    string `json:"relname"`
}

// AgentFlagsFact is the fact kind of the agent's own configuration for one
// target, which the monitoring readiness view reads (D25). The agent sends it
// in a result of its own (check AgentFlagsCheck) at start, hourly, and when
// the data directory becomes readable or stops being so. A server that does
// not know the kind drops the fact and counts it as an invalid fact; the rest
// of the push is stored.
const (
	AgentFlagsFact  = "agent_flags"
	AgentFlagsCheck = "agent_flags"
	// AgentFlagsKey is the key of the one AgentFlagsFact of a target.
	AgentFlagsKey = "agent"
)

// AgentFlags is the value of an AgentFlagsFact: switches of the agent's
// configuration, with no secret and no path. A nil field is unknown: an agent
// that does not report it, or one that cannot know it yet.
type AgentFlags struct {
	// CommandsEnabled is commands.enabled: the agent polls the server for
	// on-demand commands (EXPLAIN, pgstattuple, cancel, terminate).
	CommandsEnabled *bool `json:"commands_enabled,omitempty"`
	// AllowSignal is the target's allow_signal: cancel and terminate are
	// allowed.
	AllowSignal *bool `json:"allow_signal,omitempty"`
	// ByApplication is checks.activity.by_application: the activity check
	// counts connections per application.
	ByApplication *bool `json:"by_application,omitempty"`
	// SessionQueryText says whether the raw text of every active session is
	// collected. No agent setting controls it yet, so the agent leaves it nil.
	SessionQueryText *bool `json:"session_query_text,omitempty"`
	// HostLocal says the agent collects host metrics for the target: host
	// collection is enabled and the target runs on the agent's host
	// (host_local, or a local address when host_local is not set).
	HostLocal *bool `json:"host_local,omitempty"`
	// DataDirectoryReadable says whether the agent could measure the
	// filesystem of the target's data directory on its last host collection.
	// It stays nil until a collection knew the data directory.
	DataDirectoryReadable *bool `json:"data_directory_readable,omitempty"`
}

// FactKind describes one kind of fact the server accepts.
type FactKind struct {
	// Check names the check that reports the kind; empty when the kind does
	// not come from a check's scrape (plan, check_skip).
	Check string
	// Replace means a complete result of Check lists every fact of the kind
	// for its database. The server then removes the facts of the kind that
	// result no longer lists: the object was dropped, or the problem the
	// fact reported was fixed. A check that can report nothing at all for a
	// database (no disabled trigger left) needs Replace, or its last fact
	// would stay forever.
	Replace bool
}

// factKinds lists every fact kind. Validate refuses any other, and the agent
// drops a fact Validate refuses before sending it: a check that reports a
// new kind must add it here.
var factKinds = map[string]FactKind{
	"index_def":                 {Check: "index_stats", Replace: true},
	"setting":                   {Check: "settings", Replace: true},
	"lock_tree":                 {Check: "locks"},
	"plan":                      {},
	"check_skip":                {},
	"monitoring_role":           {Check: "monitoring_role", Replace: true},
	"trigger":                   {Check: "trigger_inventory", Replace: true},
	"fk_unindexed":              {Check: "fk_indexes", Replace: true},
	"table_columns":             {Check: "table_columns", Replace: true},
	"table_indexes":             {Check: "table_columns", Replace: true},
	"extension":                 {Check: "extensions", Replace: true},
	"spatial_column":            {Check: "postgis_columns", Replace: true},
	"sequence":                  {Check: "sequences", Replace: true},
	"partitioned_table":         {Check: "partitions", Replace: true},
	"function_tracking":         {Check: "function_stats", Replace: true},
	"security_definer_function": {Check: "function_security", Replace: true},
	"subscription":              {Check: "logical_replication", Replace: true},
	BloatUnreadableFact:         {Check: "bloat_estimate"},
	"xmin_horizon":              {Check: "activity", Replace: true},
	AgentFlagsFact:              {Check: AgentFlagsCheck, Replace: true},
}

// LookupFactKind returns the description of a fact kind, and whether the
// kind exists.
func LookupFactKind(kind string) (FactKind, bool) {
	k, ok := factKinds[kind]
	return k, ok
}

// ReplacedFactKinds returns, sorted, the kinds a complete result of the check
// replaces for its database.
func ReplacedFactKinds(check string) []string {
	var kinds []string
	for name, k := range factKinds {
		if k.Replace && k.Check == check {
			kinds = append(kinds, name)
		}
	}
	sort.Strings(kinds)
	return kinds
}
