/**
 * Stimulus → visible-in-the-UI response budgets (test-strategy.md §3). A budget
 * is a property of the system: a test never widens its own timeout.
 */
export const BUDGET = {
  /** activity/locks sampled every 10 s */
  activity: 45_000,
  /** database_stats every 30 s */
  databaseStats: 90_000,
  /** stat_statements every 60 s */
  statStatements: 150_000,
  /** readiness items from an hourly check (600 s first-run cap + run + 30 s cache) */
  readinessHourly: 660_000,
  /** readiness items from flags or the environment (2 host ticks + 30 s cache) */
  readinessFlags: 90_000,
  /** 15-minute checks after an agent restart (120 s first-run cap + run + 30 s cache) */
  check15m: 150_000,
  /** hourly checks (table_columns, extensions) after an agent restart */
  checkHourly: 660_000,
  /** tables/indexes every 5 minutes */
  tables: 480_000,
  /** ASH: 1 s sampling, 10 s flush */
  ash: 45_000,
  /** host metrics every 30 s */
  host: 90_000,
  /**
   * advisor findings: default cadence 15 min (measured at 0.4) + 50 %; 11.4 sets
   * PGLENS_ADVISOR_INTERVAL=60s on the dev stack and lowers this to 90_000.
   */
  findings: 1_350_000,
  /** container restart until /healthz and the first agent heartbeat */
  serviceRestart: 120_000,
  /** one deploy.sh run (measured 43 s at 0.2/0.3) + 50 % */
  rebuild: 65_000,
} as const

export type BudgetName = keyof typeof BUDGET

/** `test.setTimeout` value for a QA test: sequential budgets + restarts + one minute. */
export function qaTimeout(sequentialMs: readonly number[], restarts = 0): number {
  return sequentialMs.reduce((sum, ms) => sum + ms, 0) + restarts * BUDGET.serviceRestart + 60_000
}
