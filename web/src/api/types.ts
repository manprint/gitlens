import type { components, paths } from './generated'

export type Schemas = components['schemas']
export type Cluster = Schemas['Cluster']
export type SeriesPoint = Schemas['SeriesPoint']
export type APIError = Schemas['Error']
export type PermTier = Schemas['PermTier']
export type Operation<K extends keyof paths> = paths[K]
