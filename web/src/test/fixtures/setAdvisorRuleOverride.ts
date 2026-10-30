import type { Schemas } from '@/api/types'

import { withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

type AdvisorRule = Schemas['AdvisorRule']

export const responseStatus = 200

/** `index.unused` after `{enabled: false}`: an override row now exists. */
export const base: AdvisorRule = {
  id: 'index.unused',
  severity: 'warning',
  scope: 'relation',
  needs: ['Indexes'],
  min_tier: 'T0',
  enabled: false,
  overridden: true,
}

/** The same rule after `{enabled: true}`: the override row is deleted. */
export const enabledAgain: AdvisorRule = { ...base, enabled: true, overridden: false }

export function makeSetAdvisorRuleOverride(overrides: DeepPartial<AdvisorRule> = {}) {
  return withOverrides(base, overrides)
}
