# Captions inventory (17.2)

One row per element: route | element | caption id | verified against. Fragments by route group, merged at 17.2 closure.


## activity

Caption set: `web/src/lib/captionsets/activity.ts`. Smoke tests: `web/src/features/ash/CaptionPage.test.tsx`, `web/src/features/locks/CaptionPage.test.tsx`, `web/src/features/readiness/CaptionPage.test.tsx`.

| route | element | caption id | verified against |
|---|---|---|---|
| /instances/:id/ash | Section Wait types (separate or stacked view) | ash.breakdown-types | `handleASH`, `queryASHGrouped`, `ashWindowSeconds`/`ashMaxPoints`/`ashMaxGroups`, `avgActive` (internal/server/api_ash.go); `Sampler.doSample` (internal/ash/sampler.go) |
| /instances/:id/ash | Section Wait events | ash.breakdown-events | `queryASHGrouped`, `foldASHGroups` (internal/server/api_ash.go: smallest groups folded into other) |
| /instances/:id/ash | Section Queries | ash.breakdown-queries | `queryASHGrouped` with group `queryid`, `formatQueryID` (internal/server/api_ash.go) |
| /instances/:id/ash | Stacked ChartCard, wait types (dynamic) | ash.stack-types | `STACK_CAPTIONS` (AshBreakdown.tsx); `queryASHGrouped` |
| /instances/:id/ash | Stacked ChartCard, wait events (dynamic) | ash.stack-events | `STACK_CAPTIONS` (AshBreakdown.tsx); `queryASHGrouped` |
| /instances/:id/ash | Stacked ChartCard, queries (dynamic) | ash.stack-queries | `STACK_CAPTIONS` (AshBreakdown.tsx); `queryASHGrouped` |
| /instances/:id/ash | Chart CPU (dynamic) | ash.chart-cpu | `GROUP_CAPTIONS` (AshChartGrid.tsx); `Sampler.doSample` (no wait event counted as CPU) |
| /instances/:id/ash | Chart I/O (dynamic) | ash.chart-io | `GROUP_CAPTIONS` (AshChartGrid.tsx); `lib/ashGroups.ts` wait type mapping |
| /instances/:id/ash | Chart Locks (dynamic) | ash.chart-locks | `GROUP_CAPTIONS` (AshChartGrid.tsx); `lib/ashGroups.ts` |
| /instances/:id/ash | Chart Client (dynamic) | ash.chart-client | `GROUP_CAPTIONS` (AshChartGrid.tsx); `lib/ashGroups.ts`; IPC in glossary.ts |
| /instances/:id/ash | Chart Other (dynamic) | ash.chart-other | `GROUP_CAPTIONS` (AshChartGrid.tsx); `lib/ashGroups.ts` |
| /instances/:id/ash | Section Top queries | ash.top-queries | `handleASHTop`, `queryASHTop` (limit 20, newest `query_texts` text) (internal/server/api_ash.go); query_texts origin per wire envelope comment only |
| /instances/:id/ash | Chart layout toggle (ⓘ) | ash.chart-layout | `ChartLayoutToggle`, URL param `view` (AshPage.tsx) |
| /instances/:id/locks | Section Activity | activity.section | `activityCheck.Scrape`, `activityCheck.DefaultInterval` 10 s (internal/check/activity.go); `handleActivity` (internal/server/api_locks.go); `REFRESH` (api/policy.ts) |
| /instances/:id/locks | Tile Connections used / max_connections | activity.connections-used | existing id in captions.ts, reused; `buildActivityResult` (internal/check/activity.go) |
| /instances/:id/locks | Tile Longest transaction | activity.longest-transaction | `pg_max_xact_age_seconds` in `activityCheckResult` (internal/check/activity.go) |
| /instances/:id/locks | Tile Longest idle transaction | activity.longest-idle-transaction | `pg_max_idle_in_transaction_seconds` in `activityCheckResult` (internal/check/activity.go) |
| /instances/:id/locks | Tile Oldest state age | activity.oldest-state-age | `pg_max_state_age_seconds` in `activityCheckResult` (internal/check/activity.go) |
| /instances/:id/locks | Tile Prepared transactions | activity.prepared-transactions | `prepared` argument of `activityCheckResult` (internal/check/activity.go) |
| /instances/:id/locks | Tile Oldest prepared transaction | activity.oldest-prepared-transaction | `oldestPrepared` argument of `activityCheckResult` (internal/check/activity.go) |
| /instances/:id/locks | Tile Frozen XID age | activity.frozen-xid-age | `frozenAge` of `activityCheckResult` (internal/check/activity.go); rule `txn.wraparound_risk` (internal/advisor/rules_query.go) |
| /instances/:id/locks | Tile Deadlock rate | activity.deadlock-rate | `pg_deadlocks_total` (internal/check/database_stats.go); 90 s read window in `handleActivity` (internal/server/api_locks.go) |
| /instances/:id/locks | Block Connection saturation (ⓘ) | activity.saturation | `pg_connections_used_ratio` (internal/check/activity.go); rule `conn.saturation` 0.80 (internal/advisor/rules_config.go) |
| /instances/:id/locks | Block Connections by database (ⓘ) | activity.by-database | `pg_connections_by_database`, top 10 plus other (internal/check/activity.go) |
| /instances/:id/locks | Block Backends by state (ⓘ) | activity.by-state | `pg_backends` (internal/check/activity.go) |
| /instances/:id/locks | Block Oldest xmin holder (ⓘ) | activity.xmin-holder | `xminHorizonFacts`, `oldestXminHolder`, `queryXminHolders` (internal/check/xmin_horizon.go); 30 s freshness in `handleActivity` |
| /instances/:id/locks | Block Connections by application (ⓘ) | activity.by-application | `pg_connections_by_application`, `activityCheck.SetByApplication` (internal/check/activity.go); key `checks.activity.by_application` |
| /instances/:id/locks | Block Active sessions (ⓘ) | activity.sessions-active | `handleLocks` sessions, limit 100 (internal/server/api_locks.go); `locksCheck.Scrape` (internal/check/locks.go) |
| /instances/:id/locks | Connection origin summary | locks.connection-summary | existing id (captions.ts); not changed beyond `data-card` |
| /instances/:id/locks | Application sessions table | locks.sessions-application | existing id (captions.ts); not changed |
| /instances/:id/locks | pglens sessions table | locks.sessions-pglens | existing id (captions.ts); not changed |
| /instances/:id/locks | Section Blocking tree (both states) | locks.blocking-tree | `locksCheck.Scrape` (`pg_blocking_pids`, `pg_locks` join), `locksCheck.DefaultInterval` 10 s (internal/check/locks.go); `buildLocksResultText` |
| /instances/:id?section=readiness | Section Readiness | instance.readiness | existing id (captions.ts); `internal/readiness/readiness.go` |
| /instances/:id?section=readiness | Severity badge | readiness.severity | `Severity` field of the item (internal/readiness/readiness.go) |
| /instances/:id?section=readiness | Requires restart badge | readiness.requires-restart | `RequiresRestart` field of the fix (internal/readiness/readiness.go) |
| /instances/:id | Coverage banner | instance.coverage | existing id (captions.ts), verified against `internal/server/coverage.go`; unchanged |

Disabled and Degraded callouts: the ASH page names `checks.ash.enabled`; the Activity per-application note names `checks.activity.by_application` and links to RDY-ACT-01; the ASH query-id note links to the Readiness tab (RDY-SET-01, `compute_query_id`).

## alerts

Caption set: `web/src/lib/captionsets/alerts.ts`. Smoke tests: `web/src/features/alerts/CaptionPage.test.tsx`.

| route | element | caption id | verified against |
|---|---|---|---|
| /alerts | Section Alerts | alerts.list | `listAlerts` (internal/server/api_alerts.go, 500-row cap, `X-Result-Truncated`); `Active` ORDER BY last_eval_at DESC (internal/alert/store.go); `rankAlerts` (lib/alerts.ts); `defaultEngineInterval` 30 s (internal/alert/engine.go) |
| /alerts | Section Alert detail | alerts.detail | `alertDetailSQL` (internal/server/api_alerts.go); `Detail` in AlertsPage.tsx |
| /alerts?section=delivery | Section Notification delivery | alerts.delivery | `NOTIFICATION_CHANNELS` (lib/notificationChannels.ts); variables in internal/server/config.go |
| /alerts | Alert summary counts (ⓘ beside the five tiles) | alerts.summary | `summariseAlerts` (lib/alerts.ts) |
| /alerts | Filter State (ⓘ) | alerts.filter-state | `matchesFilter` (AlertsPage.tsx); `eval.go` pending and firing (internal/alert/eval.go) |
| /alerts | Filter Severity (ⓘ) | alerts.filter-severity | `matchesFilter`; `Builtin` severities (internal/alert/builtin.go) |
| /alerts | Filter Cluster, free text "Cluster ID" (ⓘ) | alerts.filter-cluster | `matchesFilter` (exact `String(cluster_id)`); `ParseClusterID` decimal (internal/pgtype/id.go) |
| /alerts | Filter Suppression (ⓘ) | alerts.filter-suppression | `alert.FirstMatch` in `listAlerts`; `matchesFilter` |
| /events | Section Event timeline | events.timeline | `handleEvents`, `defaultEventsLimit` 100, `maxEventsLimit` 1000 (internal/server/api.go); `eventRetention` 90 days (internal/server/staleness.go); `describeEvent` (lib/events.ts) |
| /events?section=alerts | Section Alert status | events.alert-status | `listAlerts`; `alertStatusColumns`, `suppressionLabel` (EventsPage.tsx) |
| /events | Filter Cluster, free text (ⓘ) | events.filter-cluster | `handleEvents` cluster_id via `ParseClusterID` (400 when not a number) |
| /events | Filter Event type (ⓘ) | events.filter-type | `handleEvents` type filter; `typeOptions` (EventsPage.tsx) |
| /events | Filter Event limit (ⓘ) | events.filter-limit | `parseLimitParam`, `MAX_EVENT_LIMIT` (EventsPage.tsx) |
| /alerts/rules | Section Configured rules | rules.list | `listRules`, `Builtin`, `Rules` (enabled rows only, `Tier` never set) (internal/alert); `ruleColumns` (RulesPage.tsx) |
| /alerts/rules | Section Edit rule | rules.edit | `updateRule` UPDATE alert_rules (internal/server/api_alerts.go); `RuleEditor.tsx` |
| /alerts/silences | Section Stored silences | silences.list | `listSilences` ORDER BY ends_at DESC LIMIT 501 (internal/server/api_alerts.go); `silenceWindow` (lib/alerts.ts) |

Fields with helper text instead of a caption (visible text, `aria-describedby`): rule Threshold, Duration, Severity and Enabled (RuleEditor.tsx; zero duration fires at once, `eval.go`); silence Matchers, Start and End, presets (SilenceEditor.tsx; matcher names from `reservedValue`, lib/alerts.ts).

Columns and badges covered inside the section captions (no `columnHelp` key added): alert State and Severity badges and Notes (alerts.list); event Severity, Type and Details (events.timeline); rule Tier, Threshold, Duration (rules.list); silence State (silences.list).

## fleet

Caption set: `web/src/lib/captionsets/fleet.ts`. Smoke tests: `web/src/features/fleet/CaptionPage.test.tsx`, `web/src/features/cluster/CaptionPage.test.tsx`.

| route | element | caption id | verified against |
|---|---|---|---|
| / (fleet) | Summary card All clusters | fleet.summary-all | `handleClusters`, `assembleClusters` (internal/server/api.go): clusters without instance are skipped; `summarize` in FleetPage.tsx |
| / | Summary card Healthy | fleet.summary-healthy | `healthForCluster`, `healthLagThresholdSeconds` = 30 (internal/server/api.go) |
| / | Summary card Degraded | fleet.summary-degraded | `healthForCluster` (anyDown, lag above 30), `upThreshold` 60 s, `isUp` (internal/server/api.go) |
| / | Summary card Critical | fleet.summary-critical | `healthForCluster` (`primaryCount != 1`) |
| / | Summary card Instances up | fleet.summary-instances-up | `isUp`, `upThreshold`; `summarize` counts `instance.up` |
| / | Summary card Instances down | fleet.summary-instances-down | `isUp`; `superseded` rows in `assembleClusters` stay listed with up=false |
| / | Summary card Firing alerts | fleet.summary-firing-alerts | `useAlerts({state:'firing'})` (web/src/api/queries.ts), `REFRESH.alerts` 15 s, `AlertListTruncated` |
| / | Section Clusters (cards: health badge, primary, instances, standbys, max replay lag, permission tiers) | fleet.clusters | `assembleClusters`, `loadMaxReplayLags` (60 s window, MAX replay_lag_sec), `PermTier.String` (internal/pgtype/role.go), `REFRESH.fleet` 15 s |
| / | Agents requiring attention strip | fleet.agent-health | `deriveAgentHealth`, `instanceStaleness`, `AGENT_STALE_AFTER_SECONDS` (web/src/lib/fleet.ts) |
| /clusters/:id Topology tab | Section Replication topology (graph, edge table toggle, anomaly banners) | topology.graph | `handleTopology` (topology_edges query), `currentSyncStates`, low-confidence note (internal/server/api_topology.go); `detectAnomalies`, dashed edge in TopologyGraph.tsx |
| /clusters/:id Lag tab | One ChartCard per replication edge | lag.edge | `handleReplication` (metrics_replication, sender wins over receiver), replication_streaming.go (pg_stat_replication write_lag/flush_lag/replay_lag, 10 s), replication_receiver.go (estimated replay lag), `LAG_SAMPLE_SECONDS`, 30-day Degraded in LagSection.tsx |
| /clusters/:id Slots tab | DataTable Replication slots | cluster.slots-table | `slotsSQL`, `handleSlots` (5 min window); replication_slots.go (pg_replication_slots, retained_bytes, safe_wal_size); `DEFAULT_RETAINED_BYTES_THRESHOLD` and `toSlotRows` (lib/replication.ts) |
| /clusters/:id Drift tab | DataTable Configuration drift | cluster.drift-table | `handleSettingsDrift` (internal/server/api_settings.go), `SettingDriftExcluded` / `DriftExcludedSettings` (internal/advisor/rules_config.go), `referenceValue` in DriftSection.tsx |
| /clusters/:id Events tab | Section Event timeline (type filter, severity pills) | cluster.events | `handleTopology` events query (`type='failover_detected'`, `topologyFailoverLimit` 100), `describeEvent` (web/src/lib/events.ts) |

Not captioned by design: the cluster filter search box (its helper text already states the effect), the readiness badge on each cluster card (owned by the readiness group, `fleet.readiness`), the SimpleTable edge table behind the topology toggle (no `captionId` support; covered by `topology.graph`), the login page (no cards).

Findings against code, not fixed here:
- The Events tab can render every documented event type, but `handleTopology` returns only `failover_detected` events (newest 100); the caption says so.
- The Instances down card counts superseded instances (re-registered at the same address and port), which the server's cluster health ignores.

## indexes

Route `/indexes`. Catalogue: `web/src/lib/captionsets/indexes.ts`. Verified against Go or TS code at the time of writing.

| route | element | caption id | verified against |
| --- | --- | --- | --- |
| /indexes | Index inventory panel (Indexes tab, inventory view) | indexes.inventory | IndexesPanel.tsx `VIEW_CAPTION`; Go `getInstanceIndexes` handler (metrics_indexes, index_def fact) |
| /indexes?view=summary | Index summary panel | indexes.summary | IndexesPanel.tsx `summariseIndexes`; indexAnalysis.ts |
| /indexes?view=summary | Primary key indexes tile | indexes.summary-primary | IndexesPanel.tsx `MetricTile`; `is_primary` (pg_index.indisprimary) |
| /indexes?view=summary | Unique indexes tile | indexes.summary-unique | IndexesPanel.tsx; `is_unique` without `is_primary` |
| /indexes?view=summary | Partial indexes tile | indexes.summary-partial | indexAnalysis.ts predicate parsing of the index definition |
| /indexes?view=summary | Expression indexes tile | indexes.summary-expression | indexAnalysis.ts key column parsing |
| /indexes?view=summary | Composite indexes tile | indexes.summary-composite | indexAnalysis.ts key column parsing |
| /indexes?view=summary | Invalid indexes tile | indexes.summary-invalid | `is_valid` (pg_index.indisvalid); Go rule `index.invalid` |
| /indexes?view=summary | Exact duplicate indexes tile | index.duplicate | Go rule `index.duplicate` (rules_relation.go): one finding per group |
| /indexes?view=summary | Redundant prefix indexes tile | index.redundant-prefix | Go rule `index.redundant_prefix`: one finding per shorter index |
| /indexes?view=summary | Foreign keys without an index tile | indexes.foreign-keys | Go rule `index.fk_unindexed`; internal/check/fk_indexes.go |
| /indexes?view=summary | Indexes by access method | indexes.breakdown-method | IndexesPanel.tsx `Breakdown`; `summariseIndexes` |
| /indexes?view=summary | Indexes by database | indexes.breakdown-database | IndexesPanel.tsx `Breakdown`; `summariseIndexes` |
| /indexes?view=summary | Tables with the most indexes | indexes.breakdown-tables | IndexesPanel.tsx `Breakdown`; `groupByTable` |
| /indexes | Flags badges (inventory) | indexes.flags | IndexesPanel.tsx `rowFlags`; `VIEW_BADGE_HINT` |
| /indexes?view=findings | Index findings panel | indexes.findings | Go rules index.* in rules_relation.go, rules_catalog.go |
| /indexes?view=findings | Rule and severity badges | indexes.finding-rule | getFindings `rule_id`, `severity`; IndexesPanel.tsx `VIEW_BADGE_HINT` |
| /indexes?view=foreign-keys | Unindexed foreign keys panel | indexes.foreign-keys | Go rule `index.fk_unindexed`; internal/check/fk_indexes.go |
| /indexes?view=foreign-keys | Severity badges | indexes.fk-severity | Go `fkUnindexedWarnBytes` (10 MiB) in the advisor |
| /indexes?tab=cleanup | Indexes that look removable panel | indexes.cleanup | CleanupPanel.tsx; indexCleanup.ts `buildCleanup` |
| /indexes?tab=cleanup | Reason badges | indexes.cleanup-reason | indexCleanup.ts `classify` |
| /indexes?tab=dead-rows | Tables by dead rows panel | indexes.dead-rows | DeadRowsPanel.tsx; getInstanceTables (n_dead_tup, n_live_tup) |
| /indexes?tab=suggestions | Index suggestions panel | indexes.suggestions | SuggestionsPanel.tsx; Go internal/indexadvice |
| /indexes?tab=suggestions | Verdict badge | indexes.verdict | Go `verdictFor` (internal/indexadvice/verdict.go) |
| /indexes?tab=tax | Index tax panel | indexes.tax | IndexTaxPanel.tsx |
| /indexes?q=... | Search results | indexes.search | SearchEverything.tsx; searchInstance endpoint |
| index detail (expanded row) | Index scans chart | index.history-scans | IndexDetail.tsx; getIndexHistory `idx_scan_rate` |
| index detail (expanded row) | Index size chart | index.history-size | IndexDetail.tsx; getIndexHistory `index_bytes` |

## instance

| route | element | caption id | verified against |
| --- | --- | --- | --- |
| /instances/:id (Overview) | Database scope section | instance.database-scope | web/src/lib/databases.ts (partitionDatabases, defaultDatabase); api databases inventory |
| /instances/:id (Overview) | Section Instance overview | overview.section | OverviewSection.tsx derivations; api_metrics scopes per database vs instance |
| /instances/:id (Overview) | Tile Connections in use | activity.connections-used (existing) | internal/check activity.go pg_connections_used/limit; internal/advisor/rules_config.go conn.saturation 0.80 |
| /instances/:id (Overview) | Tile Transactions per second | overview.tx-rate | OverviewSection.tsx sumSeries(pg_xact_commit_total, pg_xact_rollback_total); database_stats xact_commit, xact_rollback |
| /instances/:id (Overview) | Tile Cache hit ratio | overview.cache-hit-ratio (existing) | OverviewSection.tsx cacheHitRatioValue(blks_hit, blks_read); database_stats blks_hit, blks_read |
| /instances/:id (Overview) | Tile WAL generated in range | overview.wal-in-range | wal check pg_wal_lsn_diff(pg_current_wal_lsn()); lib/metrics integrateRate |
| /instances/:id (Overview) | Tile Checkpoints in range | overview.checkpoints-in-range | checkpointer check num_timed + num_requested; integrateRate |
| /instances/:id (Overview) | Tile Temporary bytes in range | overview.temp-bytes-in-range | database_stats temp_bytes; integrateRate |
| /instances/:id (Overview) | Tile Temporary files in range | overview.temp-files-in-range | database_stats temp_files; integrateRate |
| /instances/:id (Overview) | Tile Deadlocks in range | overview.deadlocks-in-range | database_stats deadlocks; integrateRate |
| /instances/:id (Overview) | Chart Transactions per second | overview.chart-transactions (dynamic) | overviewCharts.ts card; database_stats xact_commit, xact_rollback |
| /instances/:id (Overview) | Chart Connections | overview.chart-connections (dynamic) | overviewCharts.ts card; activity check pg_connections_used, pg_connections_limit; rules_config.go conn.saturation |
| /instances/:id (Overview) | Chart Deadlocks and conflicts | overview.chart-deadlocks-conflicts (dynamic) | overviewCharts.ts card; database_stats deadlocks, conflicts (standby only) |
| /instances/:id (Overview) | Chart Cache hit ratio | overview.chart-cache-hit-ratio (dynamic) | overviewCharts.ts derive cache-hit-ratio; database_stats blks_hit, blks_read |
| /instances/:id (Overview) | Chart Blocks read vs hit | overview.chart-blocks (dynamic) | overviewCharts.ts card; database_stats blks_read, blks_hit |
| /instances/:id (Overview) | Chart Temp files and bytes | overview.chart-temp (dynamic) | overviewCharts.ts card (second axis for bytes); database_stats temp_files, temp_bytes |
| /instances/:id (Overview) | Chart WAL generation | overview.chart-wal (dynamic) | overviewCharts.ts card; wal check pg_stat_wal columns probed per server |
| /instances/:id (Overview) | Chart Checkpoints | overview.chart-checkpoints (dynamic) | overviewCharts.ts perStep card; checkpointer check |
| /instances/:id (Overview) | Chart Checkpoint write and sync time | overview.chart-checkpoint-time (dynamic) | overviewCharts.ts perStep card; checkpointer write_time, sync_time |
| /instances/:id (Overview) | Chart Calls per second | overview.chart-calls (dynamic) | overviewCharts.ts workload statements; internal/server/workload_series.go (top-N bias) |
| /instances/:id (Overview) | Chart Execution time per second | overview.chart-exec-time (dynamic) | overviewCharts.ts workload statements; workload_series.go exec_time_rate_ms |
| /instances/:id (Overview) | Chart Rows per second | overview.chart-rows (dynamic) | overviewCharts.ts workload statements; workload_series.go rows_rate |
| /instances/:id (Overview) | Chart Statement block reads | overview.chart-statement-reads (dynamic) | overviewCharts.ts workload statements; workload_series.go shared_blks_read_rate |
| /instances/:id (Overview) | Chart Statement temp blocks | overview.chart-statement-temp (dynamic) | overviewCharts.ts workload statements; workload_series.go temp_blks_written_rate |
| /instances/:id (Overview) | Chart Sequential vs index scans | overview.chart-scans (dynamic) | overviewCharts.ts workload tables (300 s); workload_series.go seq_scan, idx_scan |
| /instances/:id (Overview) | Chart Tuple churn | overview.chart-tuple-churn (dynamic) | overviewCharts.ts workload tables; workload_series.go n_tup_ins/upd/hot_upd/del |
| /instances/:id (Overview) | Chart Dead tuples | overview.chart-dead-tuples (dynamic) | overviewCharts.ts workload tables gauge; workload_series.go n_dead_tup |
| /instances/:id (Overview) | Chart Index scans | overview.chart-index-scans (dynamic) | overviewCharts.ts workload indexes; workload_series.go idx_scan |
| /instances/:id (Overview) | Chart Index size | overview.chart-index-size (dynamic) | overviewCharts.ts workload indexes gauge; workload_series.go index_bytes (pg_relation_size) |
| /instances/:id?section=host | Section Host metrics | host.section | cmd/pglens-agent/run.go; internal/host/collector.go; api_host.go |
| /instances/:id?section=host | Tile CPU used | host.cpu-used (dynamic) | collector.go CPU ratio active/elapsed, cgroup quota |
| /instances/:id?section=host | Tile CPU count | host.cpu-count (dynamic) | collector.go ceil(quota) |
| /instances/:id?section=host | Tile Memory total | host.mem-total (dynamic) | collector.go cgroup limit replaces total |
| /instances/:id?section=host | Tile Memory available | host.mem-available (dynamic) | collector.go limit - (current - inactive_file) |
| /instances/:id?section=host | Tile Swap total | host.swap-total (dynamic) | collector.go swap |
| /instances/:id?section=host | Tile Swap used | host.swap-used (dynamic) | collector.go swap |
| /instances/:id?section=host | Tile Load average (1m, 5m, 15m) | host.load-average (dynamic) | collector.go load1/5/15; HostSection.tsx METRICS |
| /instances/:id?section=host | Tile Filesystem total | host.disk-total (dynamic) | collector.go data directory only when visible |
| /instances/:id?section=host | Tile Filesystem free | host.disk-free (dynamic) | collector.go |
| /instances/:id?section=host | Tile Filesystem free ratio | host.disk-free-ratio (dynamic) | run.go host_disk_free_ratio = free/total |
| /instances/:id?section=host | Chart CPU used (%) | host.chart-cpu (dynamic) | HostCharts.tsx toPercent, 0-100 axis |
| /instances/:id?section=host | Chart Load average | host.chart-load (dynamic) | HostCharts.tsx loadOption, CPU count reference line |
| /instances/:id?section=host | Chart Memory | host.chart-memory (dynamic) | HostCharts.tsx subtractSeries(total, available) |
| /instances/:id?section=host | Chart Swap | host.chart-swap (dynamic) | HostCharts.tsx; NO_SWAP_TEXT replacement |
| /instances/:id?section=host | Chart Filesystem | host.chart-filesystem (dynamic) | HostCharts.tsx filesystemOption, second axis in %; FILESYSTEM_HINT replacement |
| /instances/:id?section=catalog | Panel Schema objects (registry) | catalog.section | instanceSections.ts captionId; panel is InstancePanel, not scanned |
| /instances/:id?section=catalog | Table Triggers | catalog.triggers (dynamic via CatalogTable) | internal/check trigger_inventory: 5 min, 1000/db, disabled first |
| /instances/:id?section=catalog | Table Sequences | catalog.sequences (dynamic via CatalogTable) | check sequences: 10 min, 500/db; UI warn 0.75, critical 0.9 as in advisor |
| /instances/:id?section=catalog | Table Partitions | catalog.partitions (dynamic via CatalogTable) | check partitions: 10 min, 200 trees/db, relpages/reltuples estimates; UI flags in CatalogSection.tsx |
| /instances/:id?section=catalog | Table Functions | catalog.functions (dynamic via CatalogTable) | check function_stats: 5 min, rates averaged 1 h, limit 200 |
| /instances/:id?section=catalog | Table Security definer | catalog.security-definer (dynamic via CatalogTable) | check function_security: 15 min, 200/db; UI critical rule in CatalogSection.tsx |
| /instances/:id?section=catalog | Table Unindexed foreign keys | catalog.foreign-keys (dynamic via CatalogTable) | check fk_indexes: 15 min, 200/db, relpages estimate |
| /instances/:id?section=catalog | Table Extensions | catalog.extensions (dynamic via CatalogTable) | check extensions: 1 h, 200/db, pg_available_extensions() |
| /instances/:id?section=catalog | Table Spatial columns | catalog.spatial-columns (dynamic via CatalogTable) | check postgis_columns: 1 h, 500/db, rows estimated |
| /instances/:id?section=internals | Panel Server internals (registry) | internals.section | instanceSections.ts; api_internals.go window 1 h |
| /instances/:id?section=internals | Tile WAL directory | internals.wal-directory | check wal_directory pg_ls_waldir(), 1 min, pg_monitor |
| /instances/:id?section=internals | Tile WAL replay | internals.wal-replay | check recovery pg_get_wal_replay_pause_state(), 30 s |
| /instances/:id?section=internals | Table Database sizes | internals.database-sizes | check database_size pg_database_size(), 15 min, listed if newer than 2 h |
| /instances/:id?section=internals | Table SLRU caches | internals.slru | check slru pg_stat_slru, 1 min |
| /instances/:id?section=internals | Table Temporary files | internals.temp-files | database_stats temp_files/temp_bytes; InternalsSection.tsx spilling filter |
| /instances/:id?section=internals | Table Recovery conflicts | internals.recovery-conflicts | pg_stat_database_conflicts via recovery check |
| /instances/:id?section=logical | Panel Logical replication (registry) | logical.section | api_logical.go; logical_replication check |
| /instances/:id?section=logical | Table Subscriptions | logical.subscriptions | pg_subscription, pg_stat_subscription, pg_stat_subscription_stats (1 min); UI state rules in LogicalReplicationSection.tsx |
| /instances/:id?section=logical | Table Logical slots | logical.slots | pg_stat_replication_slots averaged 1 h; spill badge 1 MiB/s |
| /instances/:id?section=readiness | Registry entry Readiness | instance.readiness (existing) | not rendered by this group |

## queries

Catalogue: `web/src/lib/captionsets/queries.ts` (`queriesCaptions`, 23 ids). Server statements endpoint = `handleStatements` in `internal/server/api.go`. Findings endpoint = `listFindings` in `internal/server/api_findings.go`. Rules endpoint = `listAdvisorRules`/`renderAdvisorRule` in `internal/server/advisor_settings.go`.

| route | element | caption id | verified against |
|---|---|---|---|
| /instances/:id/queries | Section "Statements" | queries.statements-section | `handleStatements` (metrics_statements grouped by queryid, datname; ranked and limited server side; `STATEMENT_LIMIT` 50 in QueryListPage.tsx); `statementsQuery` in `internal/check/stat_statements.go` (one row per queryid) |
| /instances/:id/queries | DataTable "Statement statistics" | queries.statements-table | `handleStatements` (rate x sample_seconds summed; mean_exec = total_exec_time / total_calls); `summariseStatement`, `shareOfTotal` in `web/src/lib/statements.ts` (cache hit ratio, temp bytes x 8192 / calls) |
| /instances/:id/queries | Sort statements select (ⓘ) | queries.ranking | `handleStatements` order_by switch (total_exec_time, mean_exec_time, calls, rows, shared_blks_read, temp_blks_written, wal_bytes; DESC NULLS LAST); `SORT_OPTIONS` in QueryListPage.tsx |
| /instances/:id/queries | columns Mean time, Total time | (columnHelp) statements.mean-time, statements.total-time | existing `columnHelp` entries, checked against `handleStatements` |
| /queries (Slowest queries) | MetricTile Slowest mean time | slow.slowest-mean | `SlowQueriesPage.tsx` `slowestMean` = max of `summariseStatement().mean`; `handleStatements` mean_exec |
| /queries | MetricTile Time spent by these queries | slow.total-time | `SlowQueriesPage.tsx` `totalTime` sum of `summaries[].total`; `handleStatements` total_exec_time |
| /queries | MetricTile Calls | slow.calls | `SlowQueriesPage.tsx` `totalCalls`; `handleStatements` total_calls |
| /queries | MetricTile Statements ranked (unit statements, detail names the 200 limit) | slow.statements-ranked | `STATEMENT_FETCH_LIMIT` = 200 in SlowQueriesPage.tsx; `handleStatements` limit param |
| /queries | DataTable "Slowest queries" | slow.table | `slowColumns`, `shareOfTotal` (`web/src/lib/statements.ts`); `handleStatements` |
| /queries | Rank by radio group (ⓘ) | slow.rank-by | `ORDER_BY` map in SlowQueriesPage.tsx; `handleStatements` order_by |
| /queries | columns Mean time, Total time | (columnHelp) statements.mean-time, statements.total-time | as above |
| /instances/:id/queries/:queryid | MetricTile Mean time | queries.detail-mean-time | `QueryOverview.tsx` `StatementMetrics`; `handleStatements` mean_exec |
| /instances/:id/queries/:queryid | MetricTile Total time | queries.detail-total-time | `statementEntry.TotalExec` in `internal/server/api.go`; `internal/check/stat_statements.go` total_exec_time |
| /instances/:id/queries/:queryid | MetricTile Calls | queries.detail-calls | `statementEntry.Calls`; `stat_statements.go` calls |
| /instances/:id/queries/:queryid | MetricTile Rows per call (unit rows) | queries.detail-rows-per-call | `QueryOverview.tsx` `rowsPerCall` = rows / calls rounded to one decimal; `statementEntry.Rows` |
| /instances/:id/queries/:queryid | MetricTile Cache hit ratio | queries.detail-cache-hit-ratio | `QueryOverview.tsx` `formatPercent(hit, hit + read)`; `statementEntry.SharedBlksHit/SharedBlksRead` |
| /instances/:id/queries/:queryid | MetricTile WAL generated | queries.detail-wal | `statementEntry.WALBytes`; `stat_statements.go` wal_bytes |
| /instances/:id/queries/:queryid | Statement text card, Plan history, Plan comparison, EXPLAIN panel | (none) | plain `<section>` prose with its own description; not Section/DataTable/ChartCard/MetricTile |
| /findings | Section "Findings" | findings.list | `listFindings` (findings table; ORDER BY state IN muted/resolved, severity, last_seen; limit 1000 = `FINDINGS_LIMIT`); `internal/advisor/rule.go` states; `store.go` upsert (muted_until keeps state muted) |
| /findings | Finding summary buttons (ⓘ) | findings.summary | `summariseFindings` in `web/src/lib/findings.ts` |
| /findings | State filter (ⓘ) | findings.filter-state | `matchesFilter` in FindingsPage.tsx; `internal/advisor/engine.go` degraded findings (tier, input); `store.go` mute CASE |
| /findings | Severity filter (ⓘ) | findings.filter-severity | `matchesFilter`; `internal/advisor/rule.go` Severity; `renderAdvisorRule` |
| /findings | Scope filter (ⓘ) | findings.filter-scope | `matchesFilter`; `internal/advisor/rule.go` Scope (instance, cluster, database, relation) |
| /findings?view=rules | Section "Rule catalogue" | findings.rule-catalogue | `listAdvisorRules`; `RuleCatalogue.tsx` `makeRow`, Disabled badge title |
| /findings?view=rules | DataTable "Advisor rule catalogue" | findings.rule-catalogue-table | `renderAdvisorRule`; `RuleCatalogue.tsx` `countTargets`, `findingTarget` |
| /findings?view=rules | Available at or below filter (ⓘ) | findings.catalogue-tier | `internal/pgtype/role.go` tiers T0 to T3; `internal/advisor/engine.go` `PermTier < MinTier()` degraded finding; `RuleCatalogue.tsx` `permissionTiers` T0 to T2 |
| /findings | Findings table (SimpleTable), finding card, severity/state/scope chips, Mute dialog | (none) | `SimpleTable` and the finding card are not scanned tags; chip meanings are covered by findings.list and findings.filter-* |

## settings

Caption set: `web/src/lib/captionsets/settings.ts`. Smoke tests: `web/src/features/settings/CaptionPage.test.tsx`. The `settings.*` ids of the Names, Preferences, Data retention, Advisor rules and Notifications tabs predate 17.2 and live in `web/src/lib/captions.ts`.

| route | element | caption id | verified against |
|---|---|---|---|
| /settings (Inventory, Instances) | Section Instances | settings.inventory-instances | `handleInstances` over instances (internal/server/api.go), `isUp`, `upThreshold` 60 s; `handleHost` (internal/server/api_host.go) |
| /settings (Instances) | DataTable Monitored instances | settings.inventory-instances-table | `instanceColumns`, `INSTANCE_TOOLBAR` (InventoryTables.tsx); `hostMetricStaleAfter` 90 s (internal/server/api_host.go) |
| /settings?view=tiers | Section Permission tiers | settings.permission-tiers | `TierSummary` (InventoryTables.tsx); `perm_tier` in instances SELECT; advisor `min_tier` (internal/server/advisor_settings.go) |
| /settings?view=agents | Section Agents (derived) | settings.inventory-agents | `AgentInventory`, `isAgentDownAlert` (InventoryTables.tsx) |
| /settings?view=agents | DataTable Derived agent inventory | settings.inventory-agents-table | `AgentInventory` row builder (agent_id is agent- plus instance ID) |
| /settings?view=databases | Section Databases by instance | settings.inventory-databases | `INSTANCE_SEARCH_PARAM` filter; instance databases endpoint (internal/server/api_locks.go) |
| /settings?view=databases | Section Databases, per instance (3 usages: error, loading, loaded) | settings.inventory-databases-instance | `SELECT datname, monitored, skip_reason FROM databases` (internal/server/api_locks.go); `db_budget`, `MaxDatabases` 10 (internal/agent/conn.go); inventory upsert (internal/server/inventory.go) |
| /settings?view=databases | DataTable Databases for instance | settings.inventory-databases-table | `databaseColumns`, `DATABASE_TOOLBAR` (InventoryTables.tsx) |
| /settings?tab=audit | Section Command audit | settings.command-audit | `listCommandAudit`, limit 100 (internal/server/api_audit.go); migration 0015 (append-only, requested_by) |
| /settings?tab=audit | Section per instance | settings.command-audit-instance | `listCommandAudit` SELECT; outcomes requested, expired, ok, error, rejected (migration 0015); `AUDIT_COLUMNS` (CommandAudit.tsx) |
| /settings?tab=audit&view=server | Section Server information | settings.server-info | `__PGLENS_BUILD__`, `__PGLENS_API_CONTRACT_VERSION__` (web/vite.config.ts); session `expires_at` (internal/server/session.go) |
| /settings?tab=audit&view=server | Section Product limits | settings.product-limits | `limits` (ServerInfo.tsx); docs/LIMITS.md sections 12 (poolers) and the version range |
| /settings?tab=audit&view=server | Section Authentication | settings.authentication | `SessionStore` in memory (internal/server/session.go); `PGLENS_UI_PASSWORD`, `PGLENS_BOOTSTRAP_TOKEN` (internal/server/config.go) |
| /settings?tab=audit&view=server | Section References | settings.references | `nav` links (ServerInfo.tsx); docs/api.md exists |

Helper text of the settings controls: the Names, Preferences, Data retention, Advisor rules and Notifications panels already state each control's effect (unchanged). Inventory and Audit have no controls beyond table search and filters.

Not in the catalogue: the Agents view keeps the sentence "The API has no separate agent listing endpoint…" as a note above the table, because a test asserts it.
