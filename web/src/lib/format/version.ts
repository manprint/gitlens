import { UNKNOWN } from './shared'

/** Format PostgreSQL's server_version_num (for example, 170011 -> 17.11). */
export function formatPostgresVersion(version: number | null | undefined): string {
  if (version == null || !Number.isInteger(version) || version < 0) return UNKNOWN

  const major = Math.floor(version / 10_000)
  const minor = version % 100
  return `${major}.${minor}`
}
