import type { CaptionEntry, CaptionId } from '@/lib/captions'

import { activity } from './activity'
import { alerts } from './alerts'
import { core } from './core'
import { fleet } from './fleet'
import { indexes } from './indexes'
import { instance } from './instance'
import { queries } from './queries'
import { settings } from './settings'

export const captions: Readonly<Record<CaptionId, CaptionEntry>> = {
  ...core,
  ...instance,
  ...fleet,
  ...queries,
  ...alerts,
  ...indexes,
  ...activity,
  ...settings,
}
