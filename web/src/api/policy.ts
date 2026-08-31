export const REFRESH = {
  fleet: { interval: 15_000, staleAfter: 45_000 },
  cluster: { interval: 15_000, staleAfter: 45_000 },
  instance: { interval: 15_000, staleAfter: 45_000 },
  locks: { interval: 5_000, staleAfter: 20_000 },
  activity: { interval: 5_000, staleAfter: 20_000 },
  ash: { interval: 30_000, staleAfter: 90_000 },
  statements: { interval: 60_000, staleAfter: 180_000 },
  findings: { interval: 60_000, staleAfter: 300_000 },
  alerts: { interval: 15_000, staleAfter: 60_000 },
  command: { interval: 1_000, staleAfter: 0 },
  static: { interval: 0, staleAfter: 0 },
} as const

export type RefreshSurface = keyof typeof REFRESH
export type RefreshPolicy = (typeof REFRESH)[RefreshSurface]
