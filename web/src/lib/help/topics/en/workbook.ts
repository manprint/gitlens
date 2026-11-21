import type { HelpTopic } from '../../types'

export const workbookTopic: HelpTopic = {
  id: 'workbook',
  group: 'instance',
  title: 'Tuning workbook',
  summary:
    'Try planner settings, pg_hint_plan hints and parameter values on one statement, side by side, and compare the plans against a Baseline.',
  page: {
    path: '/instances/:instanceId/queries',
    label: 'Open the query inspector',
  },
  sections: [
    {
      id: 'overview',
      title: 'What the workbook is for',
      blocks: [
        {
          kind: 'text',
          text: 'A workbook belongs to one statement of one server. Open a statement from the query inspector, press {{ui:workbook.button}} and create one with {{ui:workbook.new}}. It starts with a variant named `Baseline` with no setting and no hint: every other variant is measured against it.',
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'Every run is an EXPLAIN in a read-only transaction. Settings are applied with `SET LOCAL` and last only for that transaction; nothing changes on the server.',
        },
      ],
      subsections: [
        {
          id: 'columns',
          title: 'The variants table',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:workbook.col.settings}}',
                  meaning:
                    'Only the settings the variant changes; the others keep the server value.',
                },
                {
                  term: '{{ui:workbook.col.params}}',
                  meaning:
                    'How many parameter values the variant binds; {{ui:workbook.showValues}} lists them.',
                },
                {
                  term: '{{ui:workbook.col.costRatio}}',
                  meaning:
                    'Total cost of the plan divided by the Baseline cost: below 1 is cheaper for the planner.',
                },
                {
                  term: '{{ui:workbook.col.timeRatio}}',
                  meaning:
                    'Execution time divided by the Baseline when both ran with ANALYZE, else planning time.',
                },
                {
                  term: '{{ui:workbook.col.hash}}',
                  meaning:
                    'The shape of the plan: two variants with the same hash chose the same plan.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'settings',
      title: 'Settings',
      blocks: [
        {
          kind: 'text',
          text: 'A variant changes at most 16 settings, chosen from a fixed list: the `enable_*` switches and `jit` (on or off), `work_mem` and `effective_cache_size` in kB, the planner costs such as `random_page_cost`, `hash_mem_multiplier`, `join_collapse_limit`, `from_collapse_limit`, `max_parallel_workers_per_gather` and `plan_cache_mode`. The editor shows the allowed range of each value and refuses a value out of range before sending it.',
        },
      ],
    },
    {
      id: 'hints',
      title: 'Hints',
      blocks: [
        {
          kind: 'text',
          text: 'Hints such as `SeqScan(t)` or `Leading((a b))` steer the plan through pg_hint_plan. It must be in `shared_preload_libraries` or `session_preload_libraries`; pglens never loads it. Without it a run fails with {{ui:workbook.hintPlanInactive}}',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Characters',
              meaning:
                'Letters, digits, space and `_ , ( ) # + - . "` only, at most 1024 bytes: no comment markers and no `*`.',
            },
            {
              term: 'No Set hint',
              meaning:
                'A `Set(enable_seqscan off)` hint is refused: change the setting in the variant instead.',
            },
          ],
        },
      ],
    },
    {
      id: 'parameters',
      title: 'Parameter values and ANALYZE',
      blocks: [
        {
          kind: 'text',
          text: 'A normalized statement has placeholders `$1`, `$2`…; a variant can bind up to 100 values (a value can be NULL). The values are stored with the variant in the pglens database, so do not type secrets. {{ui:workbook.editor.analyze}} runs the statement, inside the same read-only transaction, and needs the T2 permission tier.',
        },
      ],
    },
    {
      id: 'compare',
      title: 'Running and comparing',
      blocks: [
        {
          kind: 'steps',
          items: [
            'Press {{ui:workbook.run}} on a variant, or {{ui:workbook.runAll}} to queue every variant one after the other.',
            'The status changes to {{ui:workbook.status.pending}} until the agent answers, then {{ui:workbook.status.done}} or {{ui:workbook.status.failed}}.',
            'Tick the {{ui:workbook.col.compare}} box of two variants that ran: the plan comparison opens under the table.',
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'Editing a variant clears its result; run it again to measure the change.',
        },
      ],
    },
  ],
}
