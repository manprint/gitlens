/**
 * The only source of query keys in the web client. Keep arguments in the same
 * order as the corresponding operation's parameters so cache identity is
 * stable and inspectable.
 */
export const qk = {
  clusters: () => ['getClusters'] as const,
  clusterTopology: (id: string) => ['getClusterTopology', id] as const,
  clusterReplication: (id: string, from: string, to: string) =>
    ['getClusterReplication', id, from, to] as const,
  clusterSettingsDrift: (id: string) => ['getClusterSettingsDrift', id] as const,
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
    from: string,
    to: string,
    step: string,
    database?: string,
  ) => ['queryMetrics', metric, instanceId, database, from, to, step] as const,
  events: (clusterId?: string, type?: string, from?: string, to?: string, limit?: number) =>
    ['getEvents', clusterId, type, from, to, limit] as const,
  statements: (
    instanceId: string,
    database?: string,
    from?: string,
    to?: string,
    orderBy?: string,
    limit?: number,
  ) => ['getStatements', instanceId, database, from, to, orderBy, limit] as const,
  ash: (
    instanceId: string,
    from?: string,
    to?: string,
    groupBy?: string,
    database?: string,
    limit?: number,
  ) => ['getAsh', instanceId, from, to, groupBy, database, limit] as const,
  ashTop: (instanceId: string, from?: string, to?: string, database?: string, limit?: number) =>
    ['getAshTop', instanceId, from, to, database, limit] as const,
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
