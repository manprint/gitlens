import type { Catalog } from '../en'

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
import { logs } from './logs'
import { queries } from './queries'
import { readiness } from './readiness'
import { settings } from './settings'
import { shell } from './shell'

export const messages: Catalog = {
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
  ...logs,
  ...queries,
  ...readiness,
  ...settings,
  ...shell,
}
