import { readFileSync } from 'fs'
import { resolve } from 'path'
import { cwd } from 'process'
import { describe, expect, it } from 'vitest'
import { parse } from 'yaml'

import type { Workbook, WorkbookVariant } from '@/api/workbooks'

import {
  WORKBOOK_SETTINGS,
  draftProblems,
  hintsProblem,
  isHintPlanProblem,
  runnableIds,
  settingProblem,
  variantRows,
} from './workbook'

function variant(patch: Partial<WorkbookVariant> & { name: string }): WorkbookVariant {
  return {
    variant_id: `v-${patch.name}`,
    position: 0,
    settings: {},
    hints: '',
    params: [],
    analyze: false,
    status: 'never',
    command_id: null,
    error: null,
    plan_hash: null,
    total_cost: null,
    planning_ms: null,
    execution_ms: null,
    ran_at: null,
    ...patch,
  } as WorkbookVariant
}

function book(variants: WorkbookVariant[]): Workbook {
  return {
    workbook_id: 'w-1',
    instance_id: 'i-1',
    database: 'app',
    queryid: '42',
    name: 'Tuning 42',
    notes: '',
    created_at: '2026-11-21T10:00:00Z',
    updated_at: '2026-11-21T10:00:00Z',
    variants,
  }
}

describe('workbook lib', () => {
  it('UI-WBK-007 ratios are measured against the Baseline; time uses execution only when both ran with ANALYZE', () => {
    const rows = variantRows(
      book([
        variant({ name: 'Fast', position: 1, status: 'done', total_cost: 50, planning_ms: 4 }),
        variant({ name: 'Baseline', position: 0, status: 'done', total_cost: 200, planning_ms: 2 }),
        variant({ name: 'Never', position: 2, total_cost: 10 }),
      ]),
    )
    expect(rows.map((row) => row.variant.name)).toEqual(['Baseline', 'Fast', 'Never'])
    expect(rows[0]).toMatchObject({ isBaseline: true, costRatio: null, timeRatio: null })
    expect(rows[1]).toMatchObject({ costRatio: 0.25, timeRatio: 2 })
    expect(rows[2]).toMatchObject({ costRatio: null, timeRatio: null })

    const analyzed = variantRows(
      book([
        variant({
          name: 'Baseline',
          status: 'done',
          total_cost: 10,
          planning_ms: 1,
          execution_ms: 100,
        }),
        variant({
          name: 'B',
          position: 1,
          status: 'done',
          total_cost: 10,
          planning_ms: 9,
          execution_ms: 25,
        }),
      ]),
    )
    expect(analyzed[1]?.timeRatio).toBe(0.25)
  })

  it('UI-WBK-008 the settings list is the OpenAPI enum, and values follow the server rules', () => {
    const document = parse(readFileSync(resolve(cwd(), '../api/openapi.yaml'), 'utf8')) as {
      components: { schemas: { WorkbookSettingName: { enum: string[] } } }
    }
    expect(WORKBOOK_SETTINGS.map((rule) => rule.name).sort()).toEqual(
      [...document.components.schemas.WorkbookSettingName.enum].sort(),
    )
    expect(settingProblem('work_mem', '64')).toBeNull()
    expect(settingProblem('work_mem', '63')?.key).toBe('workbook.problem.settingRange')
    expect(settingProblem('work_mem', '1e3')?.key).toBe('workbook.problem.settingRange')
    expect(settingProblem('random_page_cost', '1.1')).toBeNull()
    expect(settingProblem('enable_seqscan', 'yes')?.key).toBe('workbook.problem.settingChoice')
    expect(settingProblem('shared_buffers', '1')?.key).toBe('workbook.problem.settingUnknown')
    expect(hintsProblem('SeqScan(t) Leading((a b))')).toBeNull()
    expect(hintsProblem('SeqScan(t) */')?.key).toBe('workbook.problem.hintsCharacters')
    expect(hintsProblem('Set(work_mem 1MB)')?.key).toBe('workbook.problem.hintsSet')
    expect(hintsProblem('x'.repeat(1025))?.key).toBe('workbook.problem.hintsLength')
    expect(
      draftProblems({
        name: ' ',
        settings: {},
        hints: '',
        params: [null, 'a'],
        analyze: false,
      }).map((problem) => problem.field),
    ).toEqual(['name'])
  })

  it('UI-WBK-009 hint_plan errors are recognized; Run all skips pending variants', () => {
    expect(isHintPlanProblem('hint_plan_inactive: pg_hint_plan is not loaded')).toBe(true)
    expect(isHintPlanProblem('hint_plan_disabled')).toBe(true)
    expect(isHintPlanProblem('statement_timeout')).toBe(false)
    expect(
      runnableIds(
        book([
          variant({ name: 'B', position: 1, status: 'pending' }),
          variant({ name: 'C', position: 2, status: 'failed' }),
          variant({ name: 'Baseline', position: 0, status: 'done' }),
        ]),
      ),
    ).toEqual(['v-Baseline', 'v-C'])
  })
})
