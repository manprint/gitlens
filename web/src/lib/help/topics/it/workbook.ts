import type { HelpTopic } from '../../types'

export const workbookTopic: HelpTopic = {
  id: 'workbook',
  group: 'instance',
  title: 'Quaderno di tuning',
  summary:
    'Prova impostazioni del planner, hint di pg_hint_plan e valori dei parametri su uno statement, affiancati, e confronta i piani con una Baseline.',
  page: {
    path: '/instances/:instanceId/queries',
    label: 'Apri l’ispettore delle query',
  },
  sections: [
    {
      id: 'overview',
      title: 'A cosa serve il quaderno',
      blocks: [
        {
          kind: 'text',
          text: 'Un quaderno appartiene a uno statement di un server. Apri uno statement dall’ispettore delle query, premi {{ui:workbook.button}} e creane uno con {{ui:workbook.new}}. Parte con una variante chiamata `Baseline`, senza impostazioni né hint: ogni altra variante si misura rispetto a lei.',
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'Ogni esecuzione è un EXPLAIN in una transazione di sola lettura. Le impostazioni si applicano con `SET LOCAL` e valgono solo per quella transazione; sul server non cambia nulla.',
        },
      ],
      subsections: [
        {
          id: 'columns',
          title: 'La tabella delle varianti',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:workbook.col.settings}}',
                  meaning:
                    'Solo le impostazioni che la variante cambia; le altre mantengono il valore del server.',
                },
                {
                  term: '{{ui:workbook.col.params}}',
                  meaning:
                    'Quanti valori dei parametri lega la variante; {{ui:workbook.showValues}} li elenca.',
                },
                {
                  term: '{{ui:workbook.col.costRatio}}',
                  meaning:
                    'Costo totale del piano diviso per il costo della Baseline: sotto 1 costa meno per il planner.',
                },
                {
                  term: '{{ui:workbook.col.timeRatio}}',
                  meaning:
                    'Tempo di esecuzione diviso per quello della Baseline quando entrambe sono eseguite con ANALYZE, altrimenti tempo di pianificazione.',
                },
                {
                  term: '{{ui:workbook.col.hash}}',
                  meaning:
                    'La forma del piano: due varianti con lo stesso hash hanno scelto lo stesso piano.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'settings',
      title: 'Impostazioni',
      blocks: [
        {
          kind: 'text',
          text: 'Una variante cambia al massimo 16 impostazioni, scelte da un elenco fisso: gli interruttori `enable_*` e `jit` (on oppure off), `work_mem` e `effective_cache_size` in kB, i costi del planner come `random_page_cost`, `hash_mem_multiplier`, `join_collapse_limit`, `from_collapse_limit`, `max_parallel_workers_per_gather` e `plan_cache_mode`. L’editor mostra l’intervallo ammesso di ogni valore e rifiuta un valore fuori intervallo prima di inviarlo.',
        },
      ],
    },
    {
      id: 'hints',
      title: 'Hint',
      blocks: [
        {
          kind: 'text',
          text: 'Hint come `SeqScan(t)` o `Leading((a b))` orientano il piano tramite pg_hint_plan. Deve stare in `shared_preload_libraries` o `session_preload_libraries`; pglens non lo carica mai. Senza, un’esecuzione fallisce con {{ui:workbook.hintPlanInactive}}',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Caratteri',
              meaning:
                'Solo lettere, cifre, spazio e `_ , ( ) # + - . "`, al massimo 1024 byte: niente marcatori di commento e niente `*`.',
            },
            {
              term: 'Niente hint Set',
              meaning:
                'Un hint `Set(enable_seqscan off)` viene rifiutato: cambia invece l’impostazione nella variante.',
            },
          ],
        },
      ],
    },
    {
      id: 'parameters',
      title: 'Valori dei parametri e ANALYZE',
      blocks: [
        {
          kind: 'text',
          text: 'Uno statement normalizzato ha i segnaposto `$1`, `$2`…; una variante può legare fino a 100 valori (un valore può essere NULL). I valori sono salvati con la variante nel database di pglens, quindi non scrivere segreti. {{ui:workbook.editor.analyze}} esegue lo statement, dentro la stessa transazione di sola lettura, e richiede il livello di permesso T2.',
        },
      ],
    },
    {
      id: 'compare',
      title: 'Esecuzione e confronto',
      blocks: [
        {
          kind: 'steps',
          items: [
            'Premi {{ui:workbook.run}} su una variante, oppure {{ui:workbook.runAll}} per accodare tutte le varianti una dopo l’altra.',
            'Lo stato diventa {{ui:workbook.status.pending}} finché l’agente risponde, poi {{ui:workbook.status.done}} o {{ui:workbook.status.failed}}.',
            'Spunta la casella {{ui:workbook.col.compare}} di due varianti eseguite: il confronto dei piani si apre sotto la tabella.',
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'Modificare una variante ne cancella il risultato; eseguila di nuovo per misurare la modifica.',
        },
      ],
    },
  ],
}
