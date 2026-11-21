import { alerts } from './alerts'
import { ash } from './ash'
import { auth } from './auth'
import { cluster } from './cluster'
import { findings } from './findings'
import { fleet } from './fleet'
import { help } from './help'
import { indexes } from './indexes'
import { instance } from './instance'
import { locks } from './locks'
import { queries } from './queries'
import { readiness } from './readiness'
import { settings } from './settings'
import { shell } from './shell'

/** The reference catalog (plan 006 D2): its keys are the keys of every language. */
export const messages = {
  ...alerts,
  ...ash,
  ...auth,
  ...cluster,
  ...findings,
  ...fleet,
  ...help,
  ...indexes,
  ...instance,
  ...locks,
  ...queries,
  ...readiness,
  ...settings,
  ...shell,
} as const

export type MessageKey = keyof typeof messages
export type Catalog = Readonly<Record<MessageKey, string>>
