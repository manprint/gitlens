import { queryWindowKey, type QueryWindow } from '@/lib/timerange'

/**
 * The only source of query keys in the web client. Keep arguments in the same
 * order as the corresponding operation's parameters so cache identity is
 * stable and inspectable.
 */
export const qk = {
  clusters: () => ['getClusters'] as const,
  clusterTopology: (id: string) => ['getClusterTopology', id] as const,
  clusterReplication: (id: string, window: QueryWindow) =>
    ['getClusterReplication', id, queryWindowKey(window)] as const,
  clusterSettingsDrift: (id: string) => ['getClusterSettingsDrift', id] as const,
  clusterSlots: (id: string) => ['getClusterSlots', id] as const,
  instances: () => ['getInstances'] as const,
  instance: (id: string) => ['getInstance', id] as const,
  instanceActivity: (id: string) => ['getInstanceActivity', id] as const,
  instanceDatabases: (id: string) => ['getInstanceDatabases', id] as const,
  instanceHost: (id: string) => ['getInstanceHost', id] as const,
  instanceSettings: (id: string, changedSince?: string) =>
    ['getInstanceSettings', id, changedSince] as const,
  instanceTables: (id: string, limit?: number) => ['getInstanceTables', id, limit] as const,
  instanceIndexes: (id: string, limit?: number) => ['getInstanceIndexes', id, limit] as const,
  instanceBloat: (id: string, limit?: number) => ['getInstanceBloat', id, limit] as const,
  instanceCommandAudit: (id: string) => ['getInstanceCommandAudit', id] as const,
  locks: (instanceId: string) => ['getLocks', instanceId] as const,
  queryMetrics: (
    metric: string,
    instanceId: string,
    window: QueryWindow,
    step: string,
    database?: string,
  ) => ['queryMetrics', metric, instanceId, database, queryWindowKey(window), step] as const,
  events: (
    clusterId: string | undefined,
    type: string | undefined,
    window: QueryWindow,
    limit?: number,
  ) => ['getEvents', clusterId, type, queryWindowKey(window), limit] as const,
  statements: (
    instanceId: string,
    database: string | undefined,
    window: QueryWindow,
    orderBy?: string,
    limit?: number,
  ) => ['getStatements', instanceId, database, queryWindowKey(window), orderBy, limit] as const,
  ash: (
    instanceId: string,
    window: QueryWindow,
    groupBy?: string,
    database?: string,
    limit?: number,
    waitEventType?: string,
    waitEvent?: string,
  ) =>
    [
      'getAsh',
      instanceId,
      queryWindowKey(window),
      groupBy,
      database,
      limit,
      waitEventType,
      waitEvent,
    ] as const,
  ashTop: (
    instanceId: string,
    window: QueryWindow,
    database?: string,
    limit?: number,
    waitEventType?: string,
    waitEvent?: string,
  ) =>
    [
      'getAshTop',
      instanceId,
      queryWindowKey(window),
      database,
      limit,
      waitEventType,
      waitEvent,
    ] as const,
  plans: (queryId: string | number, instanceId?: string, datname?: string, limit?: number) =>
    ['getPlans', queryId, instanceId, datname, limit] as const,
  alerts: (
    state?: string,
    severity?: string,
    ruleId?: string,
    instanceId?: string,
    clusterId?: string,
  ) => ['getAlerts', state, severity, ruleId, instanceId, clusterId] as const,
  alert: (alertKey: string) => ['getAlert', alertKey] as const,
  alertRules: () => ['getAlertRules'] as const,
  silences: (all?: boolean) => ['getSilences', all] as const,
  findings: (
    state?: string,
    severity?: string,
    ruleId?: string,
    datname?: string,
    scope?: string,
    instanceId?: string,
    clusterId?: string,
    limit?: number,
  ) =>
    ['getFindings', state, severity, ruleId, datname, scope, instanceId, clusterId, limit] as const,
  finding: (findingId: string) => ['getFinding', findingId] as const,
  advisorRules: () => ['getAdvisorRules'] as const,
  command: (id: string) => ['getCommand', id] as const,
  session: () => ['getSession'] as const,
  healthz: () => ['healthz'] as const,
  readyz: () => ['readyz'] as const,
  metrics: () => ['getMetrics'] as const,
} as const
