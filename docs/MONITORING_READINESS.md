# Monitoring readiness

pglens tells you, per instance, whether it monitors that server **in full**, and when it does not, what you lose and how to close the gap. The console shows it as a banner on the instance pages, a badge on the fleet cards and a "Monitoring readiness" section; the same report is served by `GET /api/v1/instances/{id}/readiness`, and `GET /api/v1/readiness/summary` lists every instance.

## How it works

- The level is `full` when there is no warning and `partial` when there is at least one. Infos never change it: they are features that are off, often by design.
- The server evaluates the report on read from facts and series it already stores (the last 24 hours) and caches it for 30 seconds per instance. Nothing is written.
- pglens never changes the monitored server or the agent configuration (I-2): every fix is a change **you** make, through the channel shown on it:
  `console`, `agent_config` (agent.yaml), `sql` (run by a DBA), `postgres_config` (postgresql.conf), `host` (the machine or container the agent runs on).
- A fix marked *restart required* needs a restart of PostgreSQL or of the agent; the others take effect on a reload or at the next collection.
- Snippets name the monitoring role (`<monitoring_role>` when it is not known) and never hold a credential.
- An item is omitted, never guessed, when its input is unknown: an agent older than this feature does not report `agent_flags`, and a setting is judged only once the agent has reported its settings.

## The `agent_flags` fact

The agent reports what its own configuration says in one `agent_flags` fact per target, at start, hourly and when the readability of the data directory changes: `commands_enabled`, `allow_signal`, `by_application`, `session_query_text`, `host_local` and `data_directory_readable`. It carries no address, path or secret. A server that does not know the kind counts it as an invalid fact and ignores it.

## Rules

Warnings first, then infos; each by rule id.

### RDY-ACT-01 — Per-application activity is off

*Warning.* The agent does not count connections per application.

**What you lose:** Per-application connection counts.

**Fix 1 — Turn it on in agent.yaml (restart required)** (`agent_config`)

1. Set the key below in agent.yaml.
2. Restart the agent.

```
checks:
  activity:
    by_application: true
```

### RDY-CHK-01 — A check is failing

*Warning.* The agent reports errors for at least one check, so its data is missing or old.

**What you lose:** Data of the failing check: <check>.

**Fix 1 — Read the agent's error** (`console`)

1. Open Findings and read the entry of the failing check; the agent log has the full error.
2. Fix the cause it names (a missing privilege or extension is the usual one), then wait one collection interval.

Console: `/findings`

### RDY-CHK-02 — A check is skipped

*Warning.* The agent does not run a check on this server for a reason that can be fixed.

**What you lose:** Data of the <check> check.

**Fix 1 — Find why the agent skips the check** (`host`)

1. Search the agent log for the check name: the line says what the agent found missing.
2. Give the monitoring role what the check needs (a predefined role, a privilege or an extension), then wait one collection interval.

### RDY-DB-01 — Some databases are not monitored

*Warning.* The agent found more databases than its budget lets it monitor.

**What you lose:** Databases beyond the budget are not analysed.

**Fix 1 — Raise the per-target database budget (restart required)** (`agent_config`)

1. Raise databases.max of the target in agent.yaml to at least the number of databases found, or narrow the list with include and exclude.
2. Restart the agent.

```
targets:
  - name: <target>
    databases:
      max: 12
```

### RDY-EXT-01 — pg_stat_statements is not enabled

*Warning.* The server does not load or create pg_stat_statements, so there are no per-statement statistics.

**What you lose:** Slowest queries; Statement charts; Index suggestions.

**Fix 1 — Preload the library (restart required)** (`postgres_config`)

1. Add pg_stat_statements to shared_preload_libraries in postgresql.conf (keep the libraries already listed).
2. Restart PostgreSQL: the setting is read at start.

```
shared_preload_libraries = 'pg_stat_statements'
```

**Fix 2 — Create the extension in each monitored database** (`sql`)

1. Connect to each monitored database as a DBA (pglens never creates extensions).
2. Run the statement below.

```
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
```

### RDY-HOST-01 — The host filesystem is not measured

*Warning.* The agent runs next to this server but cannot read its data directory.

**What you lose:** Host filesystem total and free charts.

**Fix 1 — Mount the data directory read-only into the agent** (`host`)

1. Mount the PostgreSQL data directory into the agent container read-only, at the same path as on the server.
2. Restart the agent.

```
# compose: under the agent service
volumes:
  - /var/lib/postgresql/data:/var/lib/postgresql/data:ro
```

### RDY-NOTIF-01 — No notification channel is configured

*Warning.* Alerts are raised but nothing delivers them.

**What you lose:** Alert delivery (Slack, Google Chat, webhook).

**Fix 1 — Add a channel in the console** (`console`)

1. Open Settings, Notifications, and add a channel.

Console: `/settings?tab=notifications`

**Fix 2 — Configure it in the server environment** (`host`)

1. Set the notification variables of the server as documented in the README, then restart the server.

### RDY-ROLE-01 — The monitoring role lacks predefined roles

*Warning.* The role cannot see other users' sessions and statements, nor every setting.

**What you lose:** Statistics of other users' sessions; Statistics of other users' statements; Settings.

**Fix 1 — Grant the missing membership** (`sql`)

1. Run the statements below as a superuser, or run deploy/sql/monitoring_user.sql as documented in deploy/sql/README.md.

```
GRANT pg_monitor TO <monitoring_role>;
GRANT pg_read_all_stats TO <monitoring_role>;
```

### RDY-SET-01 — compute_query_id is off

*Warning.* Without a query identifier an active session cannot be matched to its statement.

**What you lose:** Link between active sessions and statements; Normalised text for active sessions.

**Fix 1 — Let PostgreSQL compute the identifier** (`postgres_config`)

1. Set compute_query_id to auto (or on) in postgresql.conf.
2. Reload the configuration (SELECT pg_reload_conf();): no restart is needed.

```
compute_query_id = auto
```

### RDY-SET-02 — track_io_timing is off

*Warning.* PostgreSQL does not time block reads and writes.

**What you lose:** I/O time charts; Per-statement I/O time.

**Fix 1 — Turn it on without a restart** (`sql`)

1. Run the statements below as a superuser (or the managed service's equivalent parameter group change).
2. No restart is needed.

```
ALTER SYSTEM SET track_io_timing = on;
SELECT pg_reload_conf();
```

### RDY-CMD-01 — On-demand commands are off

*Info.* The agent does not poll the server for commands.

**What you lose:** On-demand EXPLAIN; Command audit.

**Fix 1 — Turn commands on (restart required)** (`agent_config`)

1. Set the key below in agent.yaml (the default is on).
2. Restart the agent.

```
commands:
  enabled: true
```

### RDY-EXT-02 — pgstattuple is not installed

*Info.* Exact bloat measurement on demand needs the pgstattuple extension.

**What you lose:** Exact bloat on demand (the pgstattuple command).

**Fix 1 — Create the extension** (`sql`)

1. Connect to the database as a DBA (pglens never creates extensions).
2. Run the statement below.

```
CREATE EXTENSION IF NOT EXISTS pgstattuple;
```

### RDY-SESS-01 — Raw text of active sessions is off

*Info.* The console shows the normalised text of active sessions, not the raw text.

**What you lose:** Raw text of active sessions (normalised text is still shown).

**Fix 1 — Collect the raw text (restart required)** (`agent_config`)

1. Raw text may contain literal values such as names or identifiers: turn it on only if that is acceptable.
2. Set the key below in agent.yaml, then restart the agent.

```
checks:
  locks:
    session_query_text: true
```

### RDY-SET-03 — Long statements are cut in activity

*Info.* track_activity_query_size is below 4096 bytes, so long statements are truncated.

**What you lose:** Full text of long statements in activity.

**Fix 1 — Raise the size (restart required)** (`postgres_config`)

1. Set the value below in postgresql.conf.
2. Restart PostgreSQL: the setting is read at start.

```
track_activity_query_size = 4096
```

### RDY-SIG-01 — Cancel and terminate are off (by design)

*Info.* allow_signal is off by default: the agent never signals another session unless you turn it on for the target.

**What you lose:** Cancel and terminate buttons.

**Fix 1 — Allow signals for the target (restart required)** (`agent_config`)

1. Needs tier 2 (see RDY-T2-01). Set the key below on the target in agent.yaml.
2. Restart the agent.

```
targets:
  - name: <target>
    allow_signal: true
```

### RDY-T2-01 — The monitoring role cannot signal other sessions

*Info.* Tier 2 is the pg_signal_backend grant. Cancel and terminate also need allow_signal in the agent configuration.

**What you lose:** Cancel and terminate of other sessions from the console.

**Fix 1 — Grant tier 2** (`sql`)

1. Run the statements below as a superuser, or run deploy/sql/monitoring_user.sql with tier1=1 and tier2=1 as documented in deploy/sql/README.md.

```
GRANT pg_read_all_data TO <monitoring_role>;
GRANT pg_signal_backend TO <monitoring_role>;
```

