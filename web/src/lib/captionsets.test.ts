import { describe, expect, it } from 'vitest'

import { CAPTIONS } from './captions'
import { activityCaptions } from './captionsets/activity'
import { alertsCaptions } from './captionsets/alerts'
import { fleetCaptions } from './captionsets/fleet'
import { indexesCaptions } from './captionsets/indexes'
import { instanceCaptions } from './captionsets/instance'
import { queriesCaptions } from './captionsets/queries'
import { settingsCaptions } from './captionsets/settings'

describe('caption sets (D30)', () => {
  it('CAPTION-005 an id lives in one set only, so a spread never hides an entry', () => {
    const sets = [
      activityCaptions,
      alertsCaptions,
      fleetCaptions,
      indexesCaptions,
      instanceCaptions,
      queriesCaptions,
      settingsCaptions,
    ]
    const ids = sets.flatMap((set) => Object.keys(set))
    expect(new Set(ids).size).toBe(ids.length)
    for (const id of ids) expect(Object.keys(CAPTIONS)).toContain(id)
  })
})
