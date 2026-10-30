import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import { SETTINGS_TABS } from '@/features/settings/settingsTabs'

/**
 * Drift guard of the Settings inventory (11.0, decision T1): the tab registry
 * table of `settings-inventory.md`, between its two markers, lists the same
 * tab ids and labels, in the same order, as `SETTINGS_TABS`.
 */

const DOCUMENT = join(
  __dirname,
  '../../..',
  'docs/plans/005_plan-ConsoleInsights/settings-inventory.md',
)

/** The id (backticked first column) and label (second column) of every table row between the markers. */
function registryRows(markdown: string): { id: string; label: string }[] {
  const start = markdown.indexOf('<!-- settings-tabs:start -->')
  const end = markdown.indexOf('<!-- settings-tabs:end -->')
  if (start < 0 || end < start) throw new Error('settings-tabs markers not found')
  return markdown
    .slice(start, end)
    .split('\n')
    .map((line) => /^\|\s*`([^`]+)`\s*\|\s*([^|]+?)\s*\|/.exec(line.trim()))
    .filter((match): match is RegExpExecArray => match !== null)
    .map((match) => ({ id: match[1] ?? '', label: match[2] ?? '' }))
}

describe('Settings inventory document', () => {
  it('SETI-001 the tab registry of settings-inventory.md matches SETTINGS_TABS', () => {
    const rows = registryRows(readFileSync(DOCUMENT, 'utf8'))

    expect(rows.length).toBeGreaterThan(0)
    expect(rows).toEqual(SETTINGS_TABS.map(({ id, label }) => ({ id, label })))
  })
})
