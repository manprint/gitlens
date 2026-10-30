export const responseStatus = 200
export const base = []
export const empty = []

/** A small live-like catalogue: one rule disabled from the console. */
export const catalogue = [
  {
    id: 'config.checkpoints_too_frequent',
    severity: 'warning',
    scope: 'instance',
    needs: ['settings'],
    min_tier: 'T0' as const,
    enabled: false,
    overridden: true,
  },
  {
    id: 'index.unused',
    severity: 'warning',
    scope: 'relation',
    needs: ['Indexes'],
    min_tier: 'T0' as const,
    enabled: true,
    overridden: false,
  },
  {
    id: 'query.slow_mean',
    severity: 'warning',
    scope: 'instance',
    needs: ['Statements'],
    min_tier: 'T0' as const,
    enabled: true,
    overridden: false,
  },
  {
    id: 'txn.long_running',
    severity: 'critical',
    scope: 'instance',
    needs: ['pg_max_xact_age_seconds'],
    min_tier: 'T1' as const,
    enabled: true,
    overridden: false,
  },
]
