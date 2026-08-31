# HTTP API reference

Generated from [`api/openapi.yaml`](../api/openapi.yaml) by `make api-docs`. Do not edit manually.

## clusters

| METHOD | Path | operationId | Summary |
|---|---|---|---|
| GET | `/api/v1/clusters` | `getClusters` | List monitored clusters. |
| GET | `/api/v1/clusters/{id}/replication` | `getClusterReplication` | Return replication lag series for a cluster. |
| GET | `/api/v1/clusters/{id}/settings-drift` | `getClusterSettingsDrift` | Return settings that differ between instances in a cluster. |
| GET | `/api/v1/clusters/{id}/topology` | `getClusterTopology` | Return the replication topology and failover events for a cluster. |

## instances

| METHOD | Path | operationId | Summary |
|---|---|---|---|
| GET | `/api/v1/instances` | `getInstances` | List monitored instances. |
| GET | `/api/v1/instances/{id}` | `getInstance` | Return one instance and its databases. |
| GET | `/api/v1/instances/{id}/activity` | `getInstanceActivity` | Return recent activity metrics grouped by metric name. |
| GET | `/api/v1/instances/{id}/bloat` | `getInstanceBloat` | Return relation bloat estimates. |
| GET | `/api/v1/instances/{id}/command-audit` | `getInstanceCommandAudit` | Return the immutable command audit stream for an instance. |
| GET | `/api/v1/instances/{id}/databases` | `getInstanceDatabases` | List databases and monitoring status for an instance. |
| GET | `/api/v1/instances/{id}/host` | `getInstanceHost` | Return measurable host metrics for an instance. |
| GET | `/api/v1/instances/{id}/indexes` | `getInstanceIndexes` | Return index relation statistics. |
| GET | `/api/v1/instances/{id}/settings` | `getInstanceSettings` | Return observed PostgreSQL settings. |
| GET | `/api/v1/instances/{id}/tables` | `getInstanceTables` | Return table relation statistics. |
| GET | `/api/v1/locks` | `getLocks` | Return the latest sampled blocking tree. |

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
| POST | `/api/v1/findings/{finding-id}/mute` | `muteFinding` | Mute a finding until a future timestamp. |
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
