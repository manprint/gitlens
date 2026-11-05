# HTTP API reference

Generated from [`api/openapi.yaml`](../api/openapi.yaml) by `make api-docs`. Do not edit manually.

## clusters

| METHOD | Path | operationId | Summary |
|---|---|---|---|
| GET | `/api/v1/clusters` | `getClusters` | List monitored clusters. |
| GET | `/api/v1/clusters/{id}/replication` | `getClusterReplication` | Return replication lag series for a cluster. |
| GET | `/api/v1/clusters/{id}/settings-drift` | `getClusterSettingsDrift` | Return settings that differ between instances in a cluster. |
| GET | `/api/v1/clusters/{id}/slots` | `getClusterSlots` | Return the current state of every replication slot in a cluster. |
| GET | `/api/v1/clusters/{id}/topology` | `getClusterTopology` | Return the replication topology and failover events for a cluster. |
| PATCH | `/api/v1/clusters/{id}` | `renameCluster` | Set or clear the name of a cluster. |

## instances

| METHOD | Path | operationId | Summary |
|---|---|---|---|
| GET | `/api/v1/instances` | `getInstances` | List monitored instances. |
| GET | `/api/v1/instances/{id}` | `getInstance` | Return one instance and its databases. |
| GET | `/api/v1/instances/{id}/activity` | `getInstanceActivity` | Return recent activity metrics grouped by metric name. |
| GET | `/api/v1/instances/{id}/bloat` | `getInstanceBloat` | Return relation bloat estimates. |
| GET | `/api/v1/instances/{id}/command-audit` | `getInstanceCommandAudit` | Return the immutable command audit stream for an instance. |
| GET | `/api/v1/instances/{id}/coverage` | `getCoverage` | Return how complete the data of an instance is since its agent started. |
| GET | `/api/v1/instances/{id}/databases` | `getInstanceDatabases` | List databases and monitoring status for an instance. |
| GET | `/api/v1/instances/{id}/extensions` | `getInstanceExtensions` | Return the extensions installed in each database. |
| GET | `/api/v1/instances/{id}/functions` | `getInstanceFunctions` | Return the busiest tracked functions and each database's function tracking. |
| GET | `/api/v1/instances/{id}/host` | `getInstanceHost` | Return measurable host metrics for an instance. |
| GET | `/api/v1/instances/{id}/index-suggestions` | `getIndexSuggestions` | Suggest candidate btree indexes from the tracked statements of the window. |
| GET | `/api/v1/instances/{id}/indexes` | `getInstanceIndexes` | Return index relation statistics. |
| GET | `/api/v1/instances/{id}/indexes/history` | `getIndexHistory` | Return the scan rate, tuple read rate and size of one index over time. |
| GET | `/api/v1/instances/{id}/internals` | `getInstanceInternals` | Return the WAL directory, database sizes, SLRU caches, temporary files and standby replay. |
| GET | `/api/v1/instances/{id}/logical-replication` | `getInstanceLogicalReplication` | Return the subscriptions and the logical slots' decoding rates. |
| GET | `/api/v1/instances/{id}/partitioned-tables` | `getInstancePartitionedTables` | Return the partitioned tables, each rolled up over its partitions. |
| GET | `/api/v1/instances/{id}/readiness` | `getReadiness` | Return whether the instance is monitored in full, and how to close each gap. |
| GET | `/api/v1/instances/{id}/search` | `searchInstance` | Search the statements, indexes, tables and columns of an instance. |
| GET | `/api/v1/instances/{id}/security-definer-functions` | `getInstanceSecurityDefinerFunctions` | Return the SECURITY DEFINER functions and procedures. |
| GET | `/api/v1/instances/{id}/sequences` | `getInstanceSequences` | Return the sequences and how much of their range each has used. |
| GET | `/api/v1/instances/{id}/settings` | `getInstanceSettings` | Return observed PostgreSQL settings. |
| GET | `/api/v1/instances/{id}/spatial-columns` | `getInstanceSpatialColumns` | Return the PostGIS geometry and geography columns. |
| GET | `/api/v1/instances/{id}/tables` | `getInstanceTables` | Return table relation statistics. |
| GET | `/api/v1/instances/{id}/tables/history` | `getInstanceTableHistory` | Return the dead and live tuple trend of the tables with the most dead tuples. |
| GET | `/api/v1/instances/{id}/triggers` | `getInstanceTriggers` | Return the triggers of the instance's databases. |
| GET | `/api/v1/instances/{id}/unindexed-foreign-keys` | `getInstanceUnindexedForeignKeys` | Return the foreign keys no index serves. |
| GET | `/api/v1/instances/{id}/workload/series` | `getWorkloadSeries` | Return time series of the tracked statements, tables or indexes. |
| GET | `/api/v1/locks` | `getLocks` | Return the latest sampled blocking tree, sessions with origin and the connection summary. |
| GET | `/api/v1/readiness/summary` | `getReadinessSummary` | Return the readiness level of every instance. |
| PATCH | `/api/v1/instances/{id}` | `renameInstance` | Set or clear the display name of an instance. |

## metrics

| METHOD | Path | operationId | Summary |
|---|---|---|---|
| GET | `/api/v1/metrics/query` | `queryMetrics` | Query a metric as a time series. |

## events

| METHOD | Path | operationId | Summary |
|---|---|---|---|
| GET | `/api/v1/events` | `getEvents` | List events newest first. |

## statements

| METHOD | Path | operationId | Summary |
|---|---|---|---|
| GET | `/api/v1/plans` | `getPlans` | Return plan history for a query. |
| GET | `/api/v1/statements` | `getStatements` | Return top statements joined to query text. |

## ash

| METHOD | Path | operationId | Summary |
|---|---|---|---|
| GET | `/api/v1/ash` | `getAsh` | Return activity samples grouped by the requested dimensions. |
| GET | `/api/v1/ash/top` | `getAshTop` | Return top ASH queries joined to their text. |

## commands

| METHOD | Path | operationId | Summary |
|---|---|---|---|
| GET | `/api/v1/commands/{id}` | `getCommand` | Read the state and result of a command. |
| POST | `/api/v1/instances/{id}/commands` | `createCommand` | Queue a command for an instance. |

## alerts

| METHOD | Path | operationId | Summary |
|---|---|---|---|
| GET | `/api/v1/alert-rules` | `getAlertRules` | List alert rules. |
| GET | `/api/v1/alerts` | `getAlerts` | List alerts and their suppression state. |
| GET | `/api/v1/alerts/{alert_key}` | `getAlert` | Return one alert by alert key. |
| GET | `/api/v1/silences` | `getSilences` | List active silences, or all silences when requested. |
| POST | `/api/v1/silences` | `createSilence` | Create an alert silence. |
| PUT | `/api/v1/alert-rules/{rule_id}` | `updateAlertRule` | Update a stored alert rule. |
| DELETE | `/api/v1/silences/{id}` | `deleteSilence` | End a silence immediately. |

## findings

| METHOD | Path | operationId | Summary |
|---|---|---|---|
| GET | `/api/v1/advisor/rules` | `getAdvisorRules` | List the advisor rule catalogue. |
| GET | `/api/v1/findings` | `getFindings` | List advisor findings. |
| GET | `/api/v1/findings/{finding-id}` | `getFinding` | Return one advisor finding. |
| POST | `/api/v1/findings/{finding-id}/mute` | `muteFinding` | Mute a finding until a future timestamp, at most 90 days ahead. |
| PUT | `/api/v1/advisor/rules/{id}/override` | `setAdvisorRuleOverride` | Enable or disable an advisor rule. |
| DELETE | `/api/v1/findings/{finding-id}/mute` | `unmuteFinding` | Remove a finding mute. |

## agent

| METHOD | Path | operationId | Summary |
|---|---|---|---|
| GET | `/api/v1/agents/{agent_id}/commands` | `pollCommands` | Poll and claim the next command for an agent. |
| POST | `/api/v1/commands/{id}/result` | `submitCommandResult` | Submit the result of a claimed command. |
| POST | `/api/v1/push` | `pushEnvelope` | Push an agent envelope to the server. |

## infrastructure

| METHOD | Path | operationId | Summary |
|---|---|---|---|
| GET | `/healthz` | `healthz` | Report process liveness. |
| GET | `/metrics` | `getMetrics` | Expose Prometheus self-monitoring metrics. |
| GET | `/readyz` | `readyz` | Report database and migration readiness. |

## session

| METHOD | Path | operationId | Summary |
|---|---|---|---|
| GET | `/api/v1/session` | `getSession` | Report the current browser UI session. |
| POST | `/api/v1/session` | `createSession` | Create a browser UI session. |
| DELETE | `/api/v1/session` | `deleteSession` | Delete the current browser UI session. |

## settings

| METHOD | Path | operationId | Summary |
|---|---|---|---|
| GET | `/api/v1/notification-channels` | `getNotificationChannels` | List the alert notification channels and whether each is configured. |
| GET | `/api/v1/settings/advisor-windows` | `getAdvisorWindows` | List the advisor observation windows. |
| GET | `/api/v1/settings/retention` | `getRetention` | List the data retention of each metrics family. |
| GET | `/api/v1/settings/retention/preview` | `previewRetention` | Preview what a retention of some days would delete. |
| POST | `/api/v1/notification-channels/{kind}/test` | `testNotificationChannel` | Send a test message through a notification channel. |
| PUT | `/api/v1/notification-channels/{kind}` | `setNotificationChannel` | Store the webhook address of a notification channel. |
| PUT | `/api/v1/settings/advisor-windows` | `setAdvisorWindows` | Set or reset advisor observation windows. |
| PUT | `/api/v1/settings/retention/{family}` | `setRetention` | Set the data retention of a metrics family. |
| DELETE | `/api/v1/notification-channels/{kind}` | `deleteNotificationChannel` | Remove the webhook address stored from the console. |
