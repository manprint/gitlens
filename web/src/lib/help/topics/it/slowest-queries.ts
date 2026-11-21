import type { HelpTopic } from '../../types'

export const slowestQueriesTopic: HelpTopic = {
  id: 'slowest-queries',
  group: 'monitoring',
  title: 'Query più lente',
  summary:
    'Le statement di un server che richiedono più tempo, in classifica per tempo medio per chiamata o per tempo totale, con le loro chiamate e la loro quota sul tempo elencato.',
  page: {
    path: '/queries',
    label: 'Apri Slowest queries',
  },
  sections: [
    {
      id: 'overview',
      title: 'A cosa serve la pagina',
      blocks: [
        {
          kind: 'text',
          text: 'Di solito poche statement concentrano gran parte del tempo di esecuzione di un server. Questa pagina elenca le statement di un server in ordine di classifica, così puoi scegliere quale ottimizzare per prima.',
        },
        {
          kind: 'text',
          text: 'La pagina legge le statement del server da `metrics_statements`, ricavato da `pg_stat_statements`. Carica al massimo 200 statement per richiesta.',
        },
        {
          kind: 'steps',
          items: [
            'Scegli il {{ui:queries.slow.serverLabel}} e, se vuoi, un Database.',
            'Scegli la classifica in {{ui:queries.slow.rankBy}}: {{ui:queries.slow.rankMean}} elenca le statement che impiegano più tempo a ogni esecuzione; {{ui:queries.common.totalTime}} elenca le statement che occupano più tempo nel complesso.',
            'Apri una statement dalla colonna {{ui:queries.common.query}} per leggerne il dettaglio e il piano: vedi [Pagina della statement](/help/slowest-queries#statement-detail).',
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'La pagina elenca al massimo 200 statement, quelle in testa per la classifica scelta. Una statement fuori da quella testa non compare, anche se è lenta con l’altra classifica.',
        },
      ],
      subsections: [
        {
          id: 'page-controls',
          title: 'Controlli della pagina',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:queries.slow.serverLabel}}',
                  meaning:
                    'Il server di cui sono elencate le statement. Nella lista {{ui:queries.slow.serverLabel}}, un server non attivo è segnato con (down).',
                },
                {
                  term: 'Database',
                  meaning:
                    'Limita la lista a un database. {{ui:queries.slow.allDatabases}} elenca le statement di tutti i database del server.',
                },
                {
                  term: '{{ui:queries.slow.rankBy}}',
                  meaning:
                    'Sceglie la classifica: {{ui:queries.slow.rankMean}}, per le statement che impiegano più tempo a ogni esecuzione, oppure {{ui:queries.common.totalTime}}, per le statement che occupano più tempo nel complesso. La scelta resta nell’indirizzo della pagina.',
                },
                {
                  term: '{{ui:queries.list.searchLabel}}',
                  meaning:
                    'Filtra le statement in base al termine digitato e lo evidenzia nella colonna {{ui:queries.common.query}}. La ricerca copre tutte le statement raccolte per questo server e database nell’intervallo, non solo quelle in classifica.',
                },
                {
                  term: 'Time range',
                  meaning:
                    'Si imposta nell’intestazione, con preset come Last 15 minutes e Last 30 days, oppure {{ui:shell.range.custom}}. I valori della pagina coprono l’intervallo selezionato.',
                },
                {
                  term: 'Updated',
                  meaning:
                    'Da quanto tempo i dati delle statement sono stati aggiornati l’ultima volta.',
                },
              ],
            },
          ],
        },
        {
          id: 'calculation',
          title: 'Come sono calcolati i numeri',
          blocks: [
            {
              kind: 'text',
              text: '{{ui:queries.common.meanTime}} è `total_exec_time` diviso per `calls`. {{ui:queries.common.calls}} e total time sono salvati come tassi per campione e moltiplicati per la durata del campione; un campione senza durata conta come 60 secondi.',
            },
          ],
        },
      ],
    },
    {
      id: 'summary-tiles',
      title: 'Schede di riepilogo',
      blocks: [
        {
          kind: 'text',
          text: 'Le quattro schede sopra la tabella descrivono le statement caricate dalla pagina, non tutto il server.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:queries.slow.slowestMean}}',
              meaning:
                'Il tempo medio di esecuzione per chiamata più alto tra le statement in classifica, in millisecondi o nell’unità più adatta.',
            },
            {
              term: '{{ui:queries.slow.totalTimeTile}}',
              meaning:
                'Il tempo di esecuzione sommato sulle statement in classifica, non su tutto il server.',
            },
            {
              term: '{{ui:queries.common.calls}}',
              meaning: 'Il numero di chiamate sommato sulle statement in classifica.',
            },
            {
              term: '{{ui:queries.slow.statementsRanked}}',
              meaning:
                'Il numero di statement che la pagina ha caricato e classificato, fino a 200. Il dettaglio aggiunge `some missing` quando il server ha restituito una lista troncata.',
              action: 'Restringi il filtro Database per elencare le statement di un solo database.',
            },
          ],
        },
      ],
    },
    {
      id: 'table',
      title: 'Tabella Slowest queries',
      blocks: [
        {
          kind: 'text',
          text: 'Una riga per statement, nell’ordine della classifica scelta: il rank 1 è il valore più alto. La tabella mostra 20 righe per pagina. La colonna {{ui:queries.slow.rank}} non si può ordinare; l’ordine segue {{ui:queries.slow.rankBy}}.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:queries.slow.rank}}',
              meaning: 'Posizione nella classifica scelta; 1 è il valore più alto.',
            },
            {
              term: '{{ui:queries.common.query}}',
              meaning:
                'Il testo della statement, con il termine di ricerca evidenziato. Il link apre la pagina della statement. Il pulsante di copia copia il testo della statement restituito dal server.',
              action:
                'Apri la pagina della statement per leggerne il dettaglio e il piano: vedi [Pagina della statement](/help/slowest-queries#statement-detail).',
            },
            {
              term: 'Database',
              meaning: 'Il database in cui è stata eseguita la statement.',
            },
            {
              term: '{{ui:queries.common.calls}}',
              meaning: 'Il numero di chiamate della statement nell’intervallo selezionato.',
            },
            {
              term: '{{ui:queries.common.meanTime}}',
              meaning: 'Tempo medio di esecuzione di una chiamata, da pg_stat_statements.',
              action:
                'Apri la pagina della statement e leggi il suo piano. Se un indice mancante può essere la causa, vedi [{{ui:indexes.page.tabMissing}}](/help/indexes#missing).',
            },
            {
              term: '{{ui:queries.common.totalTime}}',
              meaning:
                'Tempo di esecuzione di tutte le chiamate nell’intervallo selezionato, da pg_stat_statements.',
              action:
                'Un tempo totale alto è il primo candidato da ottimizzare, anche con un tempo medio basso, perché la statement occupa quel tempo nel complesso. Riduci il tempo per chiamata, o il numero di chiamate.',
            },
            {
              term: '{{ui:queries.list.shareOfListed}}',
              meaning:
                'Il tempo totale della statement diviso per il tempo totale di tutte le statement elencate nella tabella. Le statement non elencate non sono conteggiate.',
              action:
                'Usa la quota per confrontare le statement elencate. Non è una quota di tutto il tempo del database.',
            },
          ],
        },
      ],
    },
    {
      id: 'statement-detail',
      title: 'Pagina della statement',
      blocks: [
        {
          kind: 'text',
          text: 'Il link {{ui:queries.common.query}} apre la pagina della statement, con le sue metriche, il suo testo e il suo storico dei piani. Vedi [{{ui:shell.nav.queryPerformance}}](/help/query-performance).',
        },
        {
          kind: 'steps',
          items: [
            'Leggi {{ui:queries.common.meanTime}} e {{ui:queries.common.totalTime}} nell’intervallo selezionato, e confrontali con le altre statement della lista.',
            'Per leggere un piano con i tempi, usa {{ui:queries.explain.planAnalyze}}: la pagina chiede {{ui:queries.explain.analyzeTitle}} ed esegue solo dopo la tua conferma.',
            'Verifica se un indice mancante può aiutare: vedi [{{ui:indexes.page.tabMissing}}](/help/indexes#missing).',
            'Cambia una cosa alla volta, poi confronta la stessa statement su un intervallo che inizia dopo la modifica.',
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: '`EXPLAIN ANALYZE` esegue la statement. Per `INSERT`, `UPDATE`, `DELETE` o altre statement con effetti collaterali, verifica l’effetto prima di confermare.',
        },
        {
          kind: 'sql',
          label: 'Statement con il tempo totale più alto (PostgreSQL 13 o successivo)',
          code: 'SELECT queryid, calls, round(mean_exec_time::numeric, 2) AS mean_ms, round(total_exec_time::numeric, 2) AS total_ms, left(query, 120) AS query FROM pg_stat_statements ORDER BY total_exec_time DESC LIMIT 20;',
        },
      ],
    },
    {
      id: 'where-to-start',
      title: 'Da dove iniziare',
      blocks: [
        {
          kind: 'steps',
          items: [
            'Parti da {{ui:queries.common.totalTime}}: le statement in testa usano il tempo maggiore del server nel complesso.',
            'Poi controlla {{ui:queries.slow.rankMean}}: sono le statement per cui ogni chiamante attende a ogni esecuzione.',
            'Per ogni candidata, leggi il piano e la sua quota sul tempo elencato prima di cambiare qualsiasi cosa.',
            'Applica una modifica alla volta, e confronta i valori su un intervallo che inizia dopo la modifica.',
          ],
        },
      ],
    },
    {
      id: 'troubleshooting',
      title: 'Pagina vuota o valori mancanti',
      blocks: [
        {
          kind: 'terms',
          items: [
            {
              term: 'pg_stat_statements is not available',
              meaning:
                'L’istanza non riporta `pg_stat_statements`, quindi non c’è nulla da classificare.',
              action:
                'Aggiungi `pg_stat_statements` a `shared_preload_libraries`, crea l’estensione, poi riavvia l’agent.',
            },
            {
              term: 'No monitored servers yet',
              meaning:
                'Nessuna istanza è ancora monitorata, quindi non viene raccolta nessuna statement.',
              action:
                'Installa l’agent pglens su un host PostgreSQL per iniziare a raccogliere le statement.',
            },
            {
              term: '{{ui:queries.slow.noStatementsYet}}',
              meaning:
                'pg_stat_statements non ha voci per il server selezionato, per il database selezionato se presente, e per l’intervallo selezionato.',
              action:
                'Scegli un altro {{ui:queries.slow.serverLabel}} o Database, oppure allarga l’intervallo nell’intestazione.',
            },
            {
              term: 'No statement matches the search',
              meaning:
                'Il titolo mostra il termine digitato tra virgolette, seguito da “in this range”: il termine non corrisponde a nessuna statement nell’intervallo selezionato.',
              action:
                'Prova un altro termine o un intervallo più ampio, oppure usa {{ui:queries.list.showAll}}.',
            },
            {
              term: '{{ui:queries.slow.textTruncated}}',
              meaning:
                'Il testo della statement è segnato come troncato. Il pulsante di copia copia comunque il testo restituito dal server.',
            },
          ],
        },
      ],
    },
  ],
}
