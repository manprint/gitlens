package server

import (
	"encoding/json"
	"strings"

	"github.com/manprint/pglens/internal/mcp"
)

// mcpArg is one tool argument: a path or query parameter of the tool's route,
// named, typed and enumerated exactly as in api/openapi.yaml.
type mcpArg struct {
	name     string
	typ      string
	desc     string
	enum     []string
	required bool
}

type mcpToolSpec struct {
	name, title, desc, path string
	args                    []mcpArg
}

var (
	mcpAshGroupBy       = []string{"wait_event_type", "wait_event", "queryid", "state", "datname", "usename", "client_addr", "application_name", "service", "endpoint"}
	mcpAshCompareGroup  = []string{"wait_event_type", "wait_event", "state", "queryid", "datname", "usename", "client_addr", "application_name", "service", "endpoint"}
	mcpStatementOrderBy = []string{"calls", "calls_rate", "total_calls", "total_exec_time", "total_exec_time_ms", "exec_time", "total_time", "total_rows", "rows", "rows_rate", "shared_blks_hit", "shared_blks_hit_rate", "shared_blks_read", "shared_blks_read_rate", "wal_bytes", "wal_bytes_rate", "temp_blks_written", "temp_blks_written_rate", "mean_exec_time", "mean_time"}
)

func mcpStr(name, desc string) mcpArg       { return mcpArg{name: name, typ: "string", desc: desc} }
func mcpNeed(a mcpArg) mcpArg               { a.required = true; return a }
func mcpChoice(a mcpArg, e []string) mcpArg { a.enum = e; return a }

var (
	mcpArgInstanceID = mcpStr("instance_id", "Instance id, from pglens_list_instances.")
	mcpArgClusterID  = mcpStr("cluster_id", "Cluster id, from pglens_list_clusters.")
	mcpArgPathInst   = mcpNeed(mcpStr("id", "Instance id, from pglens_list_instances."))
	mcpArgPathClus   = mcpNeed(mcpStr("id", "Cluster id, from pglens_list_clusters."))
	mcpArgFrom       = mcpStr("from", "Start of the time range, RFC 3339.")
	mcpArgTo         = mcpStr("to", "End of the time range, RFC 3339.")
	mcpArgLimit      = mcpArg{name: "limit", typ: "integer", desc: "Maximum number of rows returned."}
	mcpArgDatabase   = mcpStr("database", "Database name.")
	mcpArgQueryID    = mcpStr("queryid", "Statement queryid (signed 64-bit integer as text).")
	mcpArgAFrom      = mcpNeed(mcpStr("a_from", "Start of the first range, RFC 3339."))
	mcpArgATo        = mcpNeed(mcpStr("a_to", "End of the first range, RFC 3339."))
	mcpArgBFrom      = mcpNeed(mcpStr("b_from", "Start of the second range, RFC 3339."))
	mcpArgBTo        = mcpNeed(mcpStr("b_to", "End of the second range, RFC 3339."))
	mcpAshFilters    = []mcpArg{
		mcpArgDatabase,
		mcpStr("wait_event_type", "Only samples with this wait event type."),
		mcpStr("wait_event", "Only samples with this wait event."),
		mcpStr("usename", "Only samples of this role."),
		mcpStr("client_addr", "Only samples from this client address."),
		mcpStr("application_name", "Only samples with this application_name."),
		mcpStr("service", "Only samples of this service (trace attribute)."),
		mcpStr("endpoint", "Only samples of this endpoint (trace attribute)."),
	}
)

func mcpJoin(parts ...[]mcpArg) []mcpArg {
	var out []mcpArg
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// mcpToolSpecs is the read-only tool registry. Every tool is one GET route;
// nothing here may name a route that changes state (TestMCP006).
var mcpToolSpecs = []mcpToolSpec{
	{"pglens_list_instances", "Instances", "Lists every monitored PostgreSQL instance with its id, name, role and last push. Start here: other tools take the instance id.", "/api/v1/instances", nil},
	{"pglens_get_instance", "Instance", "Returns one instance: identity, version, role, cluster and the latest collection status.", "/api/v1/instances/{id}", []mcpArg{mcpArgPathInst}},
	{"pglens_list_clusters", "Clusters", "Lists the replication clusters pglens knows, with their member instances.", "/api/v1/clusters", nil},
	{"pglens_cluster_topology", "Cluster topology", "Returns the current topology of one cluster: primary, standbys and replication links.", "/api/v1/clusters/{id}/topology", []mcpArg{mcpArgPathClus}},
	{"pglens_cluster_replication", "Replication history", "Returns replication lag and state of one cluster over a time range.", "/api/v1/clusters/{id}/replication", []mcpArg{mcpArgPathClus, mcpNeed(mcpArgFrom), mcpNeed(mcpArgTo)}},
	{"pglens_list_findings", "Advisor findings", "Lists advisor findings (problems and recommendations) with severity and evidence, filtered by instance, state, severity or rule.", "/api/v1/findings", []mcpArg{
		mcpStr("state", "Finding state, for example open or resolved."), mcpStr("severity", "Finding severity."), mcpStr("rule_id", "Advisor rule id."),
		mcpStr("datname", "Database name."), mcpStr("scope", "Finding scope."), mcpArgInstanceID, mcpArgClusterID, mcpArgLimit, mcpStr("q", "Text search in the finding."),
	}},
	{"pglens_list_alerts", "Alerts", "Lists alerts with their rule, severity, state and timestamps.", "/api/v1/alerts", []mcpArg{
		mcpChoice(mcpStr("state", "Alert state."), []string{"pending", "firing", "resolved", "all"}), mcpStr("severity", "Alert severity."), mcpStr("rule_id", "Alert rule id."), mcpArgInstanceID, mcpArgClusterID,
	}},
	{"pglens_list_events", "Events", "Lists recorded events (failovers, restarts, captured lock incidents and more) over a time range.", "/api/v1/events", []mcpArg{
		mcpArgClusterID, mcpArgInstanceID, mcpStr("type", "Event types, comma-separated."), mcpArgFrom, mcpArgTo, mcpArgLimit,
	}},
	{"pglens_activity", "Active session history", "Returns active session history (sampled sessions) over a time range, grouped by one dimension, with optional filters.", "/api/v1/ash", mcpJoin([]mcpArg{
		mcpNeed(mcpArgInstanceID), mcpArgFrom, mcpArgTo, mcpChoice(mcpStr("group_by", "Dimension to group samples by."), mcpAshGroupBy),
	}, mcpAshFilters, []mcpArg{mcpArgLimit})},
	{"pglens_activity_top", "Top activity", "Returns the top statements, waits and sessions by sampled activity over a time range.", "/api/v1/ash/top", mcpJoin([]mcpArg{
		mcpNeed(mcpArgInstanceID), mcpArgFrom, mcpArgTo,
	}, mcpAshFilters, []mcpArg{mcpArgLimit})},
	{"pglens_activity_compare", "Activity comparison", "Compares sampled activity between two time ranges, grouped by one dimension.", "/api/v1/ash/compare", mcpJoin([]mcpArg{
		mcpNeed(mcpArgInstanceID), mcpArgAFrom, mcpArgATo, mcpArgBFrom, mcpArgBTo, mcpNeed(mcpChoice(mcpStr("group_by", "Dimension to group samples by."), mcpAshCompareGroup)),
	}, mcpAshFilters, []mcpArg{mcpArgLimit})},
	{"pglens_top_statements", "Top statements", "Returns the top statements from pg_stat_statements over a time range, ordered by the chosen measure.", "/api/v1/statements", []mcpArg{
		mcpNeed(mcpArgInstanceID), mcpArgDatabase, mcpArgFrom, mcpArgTo, mcpChoice(mcpStr("order_by", "Measure to order by."), mcpStatementOrderBy), mcpArgLimit, mcpStr("q", "Text search in the statement."),
	}},
	{"pglens_statements_compare", "Statements comparison", "Compares statement measures between two time ranges.", "/api/v1/statements/compare", []mcpArg{
		mcpNeed(mcpArgInstanceID), mcpArgDatabase, mcpArgAFrom, mcpArgATo, mcpArgBFrom, mcpArgBTo, mcpChoice(mcpStr("order_by", "Measure to order by."), mcpStatementOrderBy), mcpArgLimit,
	}},
	{"pglens_locks", "Locks", "Returns the current lock graph of one instance: blocked and blocking sessions.", "/api/v1/locks", []mcpArg{mcpNeed(mcpArgInstanceID)}},
	{"pglens_query_plans", "Query plans", "Returns the plans recorded for one statement (plan history).", "/api/v1/plans", []mcpArg{
		mcpNeed(mcpArgQueryID), mcpArgInstanceID, mcpStr("datname", "Database name."), mcpArgLimit,
	}},
	{"pglens_plan_samples", "Plan samples", "Returns sampled plans of one statement over a time range; parameters only when asked and when collected.", "/api/v1/plans/samples", []mcpArg{
		mcpNeed(mcpArgInstanceID), mcpNeed(mcpArgQueryID), mcpArgDatabase, mcpStr("plan_hash", "Plan hash."), mcpStr("trace_id", "Trace id."), mcpArgFrom, mcpArgTo, mcpArgLimit,
		{name: "include_parameters", typ: "boolean", desc: "Include the bind parameters of each sample."},
	}},
	{"pglens_plan_stats", "Plan statistics", "Returns per-plan statistics of one statement over a time range.", "/api/v1/plans/stats", []mcpArg{
		mcpNeed(mcpArgInstanceID), mcpNeed(mcpArgQueryID), mcpArgDatabase, mcpArgFrom, mcpArgTo,
	}},
	{"pglens_logs", "Logs", "Returns PostgreSQL log messages of one instance, filtered by class, severity, database or text.", "/api/v1/logs", []mcpArg{
		mcpNeed(mcpArgInstanceID), mcpArgFrom, mcpArgTo, mcpStr("class", "Log class."), mcpStr("severity", "Log severity."), mcpArgDatabase, mcpStr("q", "Text search in the message."),
		mcpStr("trace_id", "Trace id."), mcpArgLimit, mcpStr("before", "Cursor: only messages before this point."),
	}},
	{"pglens_log_classes", "Log classes", "Returns log message counts per class for one instance over a time range.", "/api/v1/logs/classes", []mcpArg{mcpNeed(mcpArgInstanceID), mcpArgFrom, mcpArgTo}},
	{"pglens_autovacuum_runs", "Autovacuum runs", "Lists autovacuum and autoanalyze runs of one instance, filtered by table and kind.", "/api/v1/autovacuum/runs", []mcpArg{
		mcpNeed(mcpArgInstanceID), mcpArgDatabase, mcpStr("schema", "Schema name."), mcpStr("relation", "Table name."), mcpChoice(mcpStr("kind", "Run kind."), []string{"vacuum", "analyze"}),
		mcpArgFrom, mcpArgTo, mcpChoice(mcpStr("order_by", "Order of the runs."), []string{"ts", "elapsed_seconds", "tuples_removed"}), mcpArgLimit,
	}},
	{"pglens_autovacuum_summary", "Autovacuum summary", "Returns autovacuum activity per table of one instance over a time range.", "/api/v1/autovacuum/summary", []mcpArg{mcpNeed(mcpArgInstanceID), mcpArgFrom, mcpArgTo, mcpArgLimit}},
	{"pglens_instance_tables", "Tables", "Returns table statistics of one instance: size, dead tuples and scans.", "/api/v1/instances/{id}/tables", []mcpArg{
		mcpArgPathInst, mcpChoice(mcpStr("order_by", "Order of the tables."), []string{"dead_tup", "dead_ratio", "size", "seq_scan"}), mcpArgLimit,
	}},
	{"pglens_instance_indexes", "Indexes", "Returns index statistics of one instance: size and usage.", "/api/v1/instances/{id}/indexes", []mcpArg{mcpArgPathInst, mcpArgLimit}},
	{"pglens_index_suggestions", "Index suggestions", "Returns index suggestions for one instance, derived from the recorded statements.", "/api/v1/instances/{id}/index-suggestions", []mcpArg{
		mcpArgPathInst, mcpStr("window", "Look-back window, for example 24h."), mcpArgLimit, mcpArgDatabase,
	}},
	{"pglens_instance_bloat", "Bloat", "Returns table and index bloat of one instance.", "/api/v1/instances/{id}/bloat", []mcpArg{
		mcpArgPathInst, mcpChoice(mcpStr("method", "Bloat measure."), []string{"estimate", "pgstattuple"}), mcpChoice(mcpStr("object_kind", "Tables or indexes."), []string{"table", "index"}), mcpArgLimit,
	}},
	{"pglens_instance_settings", "Settings", "Returns the PostgreSQL settings of one instance, optionally only those changed since a time.", "/api/v1/instances/{id}/settings", []mcpArg{
		mcpArgPathInst, mcpStr("changed_since", "Only settings changed since this time, RFC 3339."),
	}},
	{"pglens_instance_readiness", "Readiness", "Returns the collection readiness of one instance: which extensions and settings pglens needs and what is missing.", "/api/v1/instances/{id}/readiness", []mcpArg{mcpArgPathInst}},
	{"pglens_instance_host", "Host metrics", "Returns the latest host metrics of one instance: CPU, memory, disks and network.", "/api/v1/instances/{id}/host", []mcpArg{mcpArgPathInst}},
	{"pglens_host_processes", "Host processes", "Returns the PostgreSQL processes of one instance's host with CPU, memory and I/O.", "/api/v1/instances/{id}/host/processes", []mcpArg{
		mcpArgPathInst, mcpStr("at", "Point in time, RFC 3339; latest when absent."), mcpChoice(mcpStr("order_by", "Order of the processes."), []string{"cpu", "rss", "read", "write"}), mcpArgLimit,
	}},
	{"pglens_metrics_query", "Metric series", "Returns one metric series of one instance over a time range at a fixed step.", "/api/v1/metrics/query", []mcpArg{
		mcpNeed(mcpStr("metric", "Metric name.")), mcpNeed(mcpArgInstanceID), mcpArgDatabase, mcpNeed(mcpArgFrom), mcpNeed(mcpArgTo), mcpNeed(mcpStr("step", "Step between points, for example 60s.")),
	}},
}

// mcpTools builds the MCP tool list from mcpToolSpecs.
func mcpTools() []mcp.Tool {
	tools := make([]mcp.Tool, 0, len(mcpToolSpecs))
	for _, spec := range mcpToolSpecs {
		props := map[string]any{}
		required := []string{}
		tool := mcp.Tool{Name: spec.name, Title: spec.title, Description: spec.desc, Path: spec.path}
		for _, a := range spec.args {
			p := map[string]any{"type": a.typ, "description": a.desc}
			if len(a.enum) > 0 {
				p["enum"] = a.enum
			}
			props[a.name] = p
			if a.required {
				required = append(required, a.name)
			}
			if strings.Contains(spec.path, "{"+a.name+"}") {
				tool.PathParams = append(tool.PathParams, a.name)
			} else {
				tool.QueryParams = append(tool.QueryParams, a.name)
			}
		}
		schema, _ := json.Marshal(map[string]any{
			"type": "object", "properties": props, "required": required, "additionalProperties": false,
		})
		tool.InputSchema = schema
		tools = append(tools, tool)
	}
	return tools
}
