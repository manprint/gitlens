import type { HelpTopic } from '../../types'

export const queryPerformanceTopic: HelpTopic = {
  id: 'query-performance',
  group: 'instance',
  title: 'Prestazioni delle query',
  summary:
    'L’ispettore delle query ordina gli statement di un’istanza da `pg_stat_statements`. Il dettaglio mostra uno statement con le metriche, il piano di esecuzione e lo storico dei piani.',
  page: {
    path: '/instances/:instanceId/queries',
    label: 'Apri {{ui:shell.nav.queryPerformance}}',
  },
  sections: [
    {
      id: 'overview',
      title: 'A cosa serve la pagina',
      blocks: [
        {
          kind: 'text',
          text: '{{ui:shell.nav.queryPerformance}} risponde a una domanda: quali statement SQL di questa istanza usano più tempo, chiamate, righe, I/O o WAL, e com’è fatto il loro piano di esecuzione. La pagina ha due livelli. L’ispettore delle query ordina gli statement dell’istanza. Il dettaglio mostra uno statement con le metriche, il piano di esecuzione e lo storico dei piani. Le statistiche vengono da `pg_stat_statements`, raccolte dall’agent e salvate dal server. I piani arrivano dai comandi EXPLAIN che richiedi dalla pagina e, con `auto_explain`, dal log di PostgreSQL.',
        },
      ],
      subsections: [
        {
          id: 'how-to-open',
          title: 'Come aprirla',
          blocks: [
            {
              kind: 'steps',
              items: [
                'Apri {{ui:shell.nav.fleet}} su `/`, scegli il cluster, poi l’istanza.',
                'Nella pagina dell’istanza apri la tab {{ui:shell.nav.queryPerformance}}. Il gruppo {{ui:shell.nav.instance}} della sidebar elenca la stessa pagina.',
                'Clicca `Query <queryid>` nell’ispettore per aprire uno statement con i suoi piani.',
              ],
            },
          ],
        },
        {
          id: 'page-controls',
          title: 'Controlli della pagina',
          blocks: [
            {
              kind: 'text',
              text: 'I controlli che cambiano la pagina sono l’intervallo, il selettore di ordinamento, la casella di ricerca e il parametro URL `db`. Le tab dell’intestazione dell’istanza e le voci della sidebar di questo server portano `db` nella vista successiva, così l’ambito del database resta lo stesso. Non portano l’ordinamento né la ricerca: ripartono dai valori predefiniti.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:shell.range.preset}}',
                  meaning:
                    'Sceglie l’intervallo letto dalla pagina, dal selettore di intervallo (il select etichettato {{ui:shell.range.preset}}). Contatori come chiamate, tempo, righe, blocchi e byte sono sommati su quell’intervallo, e la classifica usa le stesse somme.',
                  action:
                    'Allarga l’intervallo se manca uno statement che ti aspetti: può essere stato eseguito prima dell’intervallo o essere stato rimosso (eviction).',
                },
                {
                  term: '{{ui:queries.list.sortLabel}}',
                  meaning:
                    'La misura con cui gli statement sono ordinati prima di tenere i primi 50. Dettagli: [{{ui:queries.list.sortLabel}}](/help/query-performance#sort-statements).',
                },
                {
                  term: '{{ui:queries.list.searchLabel}}',
                  meaning:
                    'Cerca nel testo della query, nel `queryid` o nel database su tutto l’intervallo. Dettagli: [{{ui:queries.list.searchLabel}}](/help/query-performance#search-statements).',
                },
              ],
            },
          ],
        },
        {
          id: 'data-sources',
          title: 'Da dove arrivano i dati',
          blocks: [
            {
              kind: 'text',
              text: 'Il check `stat_statements` dell’agent scrive sul server i contatori di `pg_stat_statements`: una riga per `queryid` e database. Il server somma questi contatori sull’intervallo selezionato, ordina gli statement e ne tiene i primi 50. La quota Share of listed time è calcolata nel browser. L’agent esegue un EXPLAIN solo quando lo richiedi dal dettaglio della query; con `auto_explain` legge anche dal log i piani degli statement lenti. Il server conserva entrambi nello storico dei piani.',
            },
          ],
        },
      ],
    },
    {
      id: 'query-inspector',
      title: '{{ui:queries.list.inspectorTitle}}',
      blocks: [
        {
          kind: 'text',
          text: 'Il percorso `/instances/:instanceId/queries` mostra la pagina {{ui:queries.list.inspectorTitle}}. Il suo sottotitolo dice: ogni statement registrato da `pg_stat_statements` su questa istanza nell’intervallo selezionato. Cambia l’ordinamento per trovare le query più lente, più frequenti o più pesanti in I/O, e apri una query per vederne il piano. La lista mostra al massimo 50 statement.',
        },
      ],
      subsections: [
        {
          id: 'sort-statements',
          title: '{{ui:queries.list.sortLabel}}',
          blocks: [
            {
              kind: 'text',
              text: 'Il selettore di ordinamento, accanto all’aiuto {{ui:queries.list.sortLabel}}, sceglie la misura con cui il server ordina gli statement prima di tenere i primi 50. La scelta resta nell’URL come `sort`. Il valore predefinito è {{ui:queries.common.totalTime}}. Il clic su un’intestazione di colonna riordina solo le righe già caricate: non cambia quali statement compaiono nella lista.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:queries.common.totalTime}}',
                  meaning:
                    'Ordina per tempo di esecuzione di tutte le chiamate nell’intervallo, da `pg_stat_statements`. È il valore predefinito: trova gli statement che costano più al database nel complesso.',
                  action: 'Parti dalle righe in alto. Apri ciascuna e richiedi un piano.',
                },
                {
                  term: '{{ui:queries.common.meanTime}}',
                  meaning:
                    'Ordina per il tempo medio di esecuzione di una chiamata. Trova gli statement lenti a ogni chiamata, anche quando girano raramente.',
                  action:
                    'Apri lo statement e richiedi [{{ui:queries.explain.planOnly}} (EXPLAIN)](/help/query-performance#plan-only).',
                },
                {
                  term: '{{ui:queries.common.calls}}',
                  meaning:
                    'Ordina per numero di esecuzioni nell’intervallo. Trova gli statement più frequenti: un tempo medio basso moltiplicato per molte chiamate può pesare comunque sul database.',
                  action:
                    'Verifica se l’applicazione esegue lo stesso statement in un ciclo. Ridurre i round trip spesso aiuta più che ottimizzare una singola chiamata.',
                },
                {
                  term: '{{ui:queries.common.rows}}',
                  meaning:
                    'Ordina per righe restituite o modificate nell’intervallo. Trova gli statement che spostano grandi insiemi di risultati.',
                  action:
                    'Verifica i filtri e l’eventuale LIMIT della query dell’applicazione: lo statement può restituire più righe di quelle che l’applicazione usa.',
                },
                {
                  term: '{{ui:queries.list.sortSharedRead}}',
                  meaning:
                    'Ordina per i blocchi condivisi che gli statement hanno dovuto leggere nell’intervallo. Una lettura può arrivare ancora dalla cache del sistema operativo, quindi non è sempre I/O su disco.',
                  action:
                    'Apri lo statement e leggi {{ui:queries.overview.cacheHitRatio}}. Verifica gli indici delle tabelle che legge in [{{ui:shell.nav.indexAnalysis}}](/help/indexes).',
                },
                {
                  term: '{{ui:queries.list.sortTempWritten}}',
                  meaning:
                    'Ordina per blocchi temporanei scritti nell’intervallo, di solito quando un sort o un hash non entra in `work_mem`.',
                  action:
                    'Verifica {{ui:queries.list.tempBytesPerCall}} nella tabella e la regola dell’advisor `query.temp_bytes_high`. Rimedi: un indice che restituisce le righe già ordinate, una query riscritta, o un `work_mem` più alto per quella sessione.',
                },
                {
                  term: '{{ui:queries.list.sortWal}}',
                  meaning:
                    'Ordina per byte di write-ahead log generati nell’intervallo. Trova gli statement che scrivono di più.',
                  action:
                    'Verifica se lo statement scrive più righe di quelle necessarie, e se gira in un ciclo o in lotti grandi.',
                },
              ],
            },
          ],
        },
        {
          id: 'statement-statistics',
          title: 'Statistiche degli statement',
          blocks: [
            {
              kind: 'text',
              text: 'La tabella Statement statistics ha una riga per statement: un `queryid` in un database. Colonne, da sinistra: Query, {{ui:queries.common.calls}}, {{ui:queries.common.totalTime}}, {{ui:queries.common.meanTime}}, {{ui:queries.list.shareOfListed}}, Rows, {{ui:queries.overview.cacheHitRatio}}, WAL e {{ui:queries.list.tempBytesPerCall}}. Un’intestazione di colonna con suggerimento spiega la colonna; i termini sotto ne danno lo stesso significato.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:queries.common.query}}',
                  meaning:
                    '`Query <queryid>` porta al dettaglio dello statement. Sotto il link, il testo normalizzato si apre al clic. `queryid` è l’id che `pg_stat_statements` assegna allo statement.',
                  action:
                    'Apri lo statement per vedere il piano. Il link porta `db`, il database dello statement, così la richiesta del piano gira lì.',
                },
                {
                  term: '{{ui:queries.common.calls}}',
                  meaning:
                    'Esecuzioni dello statement nell’intervallo, sommate. {{ui:shell.hint.unit}}à: chiamate. Leggila insieme a {{ui:queries.common.meanTime}}.',
                  action:
                    'Verifica se l’applicazione esegue lo stesso statement in un ciclo. Ridurre i round trip spesso aiuta più che ottimizzare una singola chiamata.',
                },
                {
                  term: '{{ui:queries.common.totalTime}}',
                  meaning:
                    'Tempo di esecuzione di tutte le chiamate nell’intervallo selezionato, in millisecondi, da `pg_stat_statements`.',
                  action:
                    'Parti dagli statement in alto: sono quelli che costano di più al database nel complesso.',
                },
                {
                  term: '{{ui:queries.common.meanTime}}',
                  meaning:
                    'Tempo medio di esecuzione di una chiamata, in millisecondi: {{ui:queries.common.totalTime}} diviso {{ui:queries.common.calls}}, da `pg_stat_statements`.',
                  action:
                    'Un tempo medio alto con poche chiamate indica una singola esecuzione costosa. Richiedi [{{ui:queries.explain.planOnly}} (EXPLAIN)](/help/query-performance#plan-only) e leggi il nodo con il costo totale più alto.',
                },
                {
                  term: '{{ui:queries.list.shareOfListed}}',
                  meaning:
                    '{{ui:queries.common.totalTime}} dello statement diviso per il tempo totale di tutti gli statement elencati nella tabella, in percentuale. Gli statement non elencati non vengono contati.',
                  action:
                    'Una quota alta significa che lo statement domina il carico elencato. La regola dell’advisor `query.total_time_share` segnala quando uno statement supera il 30% del tempo di esecuzione totale caricato dall’advisor.',
                },
                {
                  term: '{{ui:queries.common.rows}}',
                  meaning:
                    'Righe restituite o modificate dallo statement nell’intervallo, sommate sulle sue chiamate. La pagina di dettaglio mostra {{ui:queries.overview.rowsPerCall}}.',
                  action:
                    'Un conteggio alto con poche chiamate indica grandi insiemi di risultati. Verifica i filtri e l’eventuale LIMIT nella query dell’applicazione.',
                },
                {
                  term: '{{ui:queries.overview.cacheHitRatio}}',
                  meaning:
                    'Hit nello shared buffer diviso per hit più letture, per questo statement nell’intervallo, in percentuale. Una lettura può arrivare ancora dalla cache del sistema operativo, quindi un valore basso non è sempre I/O su disco.',
                  action:
                    'La regola dell’advisor `query.cache_miss_high` è di tipo info: scatta quando più del 30% dei blocchi letti arriva da fuori dello shared buffer, su uno statement con più di 1000 chiamate. Verifica gli indici delle tabelle che legge in [{{ui:shell.nav.indexAnalysis}}](/help/indexes).',
                },
                {
                  term: '{{ui:queries.list.wal}}',
                  meaning:
                    '{{ui:queries.list.sortWal}} scritti dallo statement nell’intervallo, stimati dai tassi al secondo.',
                  action:
                    'Verifica se lo statement scrive più righe di quelle necessarie, e se gira in un ciclo o in lotti grandi.',
                },
                {
                  term: '{{ui:queries.list.tempBytesPerCall}}',
                  meaning:
                    'Byte temporanei scritti per chiamata nell’intervallo: blocchi temporanei scritti, da 8 KiB ciascuno, diviso le chiamate. I file temporanei compaiono quando un sort o un hash non entra in `work_mem`.',
                  action:
                    'La regola dell’advisor `query.temp_bytes_high` segnala quando la media supera `work_mem`, o 4 MiB quando l’impostazione non viene letta. Rimedi: un indice che restituisce le righe già ordinate, una query riscritta, o un `work_mem` più alto per quella sessione.',
                },
              ],
            },
          ],
        },
        {
          id: 'search-statements',
          title: '{{ui:queries.list.searchLabel}}',
          blocks: [
            {
              kind: 'text',
              text: 'La casella {{ui:queries.list.searchLabel}} cerca nel testo della query, nel `queryid` o nel database su tutto l’intervallo. La ricerca considera ogni statement raccolto su questa istanza nell’intervallo, non solo le 50 righe elencate: uno statement può comparire anche se l’ordinamento predefinito lo nasconderebbe. Il termine resta nell’URL come `q`.',
            },
          ],
        },
        {
          id: 'limit-and-eviction',
          title: 'Limite, eviction e troncamento',
          blocks: [
            {
              kind: 'note',
              tone: 'info',
              text: 'La lista tiene i primi 50 statement. Quando il server ne ha di più, un avviso riporta Showing N of 50 statements (truncated; budget 50). Il budget top-N è configurato da `checks.stat_statements.top_n`.',
            },
            {
              kind: 'note',
              tone: 'info',
              text: 'PostgreSQL conserva al massimo `pg_stat_statements.max` statement. Le voci oltre questo limite vengono rimosse (eviction) e possono comparire e sparire tra un intervallo e l’altro.',
            },
          ],
        },
        {
          id: 'list-states',
          title: 'Stati disabilitato e vuoto',
          blocks: [
            {
              kind: 'note',
              tone: 'warning',
              text: 'Quando `pg_stat_statements` non è disponibile, un banner riporta “pg_stat_statements disabled.” e il motivo: “The agent skips the stat_statements check: <reason>.” Senza motivo riporta “The agent does not collect statements for this instance.” Se sono ancora elencati statement più vecchi, il banner dice che sono stati raccolti in precedenza.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'No statements',
                  meaning:
                    'La lista è vuota per l’istanza, il database e l’intervallo selezionati.',
                  action:
                    'Allarga l’intervallo. Verifica il parametro URL `db`: limita la lista a un solo database.',
                },
                {
                  term: 'Nessuno statement corrisponde a “<term>” in questo intervallo',
                  meaning:
                    'La ricerca non trova statement nell’intervallo. Il pulsante {{ui:queries.list.showAll}} cancella la ricerca.',
                  action:
                    'Prova un altro termine o allarga l’intervallo. La ricerca copre ogni statement raccolto nell’intervallo, non solo quelli elencati.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'query-detail',
      title: 'Dettaglio della query e storico dei piani',
      blocks: [
        {
          kind: 'text',
          text: 'Il percorso `/instances/:instanceId/queries/:queryid` si apre da una riga dell’ispettore delle query. La pagina ha tre parti: lo statement con i riquadri delle metriche, il piano di esecuzione che puoi richiedere ora e lo storico dei piani. Il link dalla lista porta `db`, il database dello statement, quando è noto. Senza `db`, lo storico dei piani elenca i piani di tutti i database.',
        },
      ],
      subsections: [
        {
          id: 'detail-header',
          title: 'Intestazione e aggiornamento',
          blocks: [
            {
              kind: 'text',
              text: 'L’intestazione riporta `Query <queryid>`, con il database come badge quando è noto. Il pulsante All queries torna all’ispettore della stessa istanza. Il badge di aggiornamento indica quanto sono vecchie le statistiche: si aggiornano ogni 60 secondi, e il badge le segnala come stale dopo 180 secondi senza aggiornamenti.',
            },
          ],
        },
        {
          id: 'statement-text',
          title: '{{ui:queries.overview.statementText}}',
          blocks: [
            {
              kind: 'text',
              text: 'La sezione {{ui:queries.overview.statementText}} mostra lo statement come lo salva `pg_stat_statements`: i valori letterali sono sostituiti da segnaposto, quindi i valori dei parametri non compaiono. Il pulsante Copy copia il testo e poi mostra Copied. Se il browser lo rifiuta, mostra Copy failed.',
            },
          ],
        },
        {
          id: 'metric-tiles',
          title: 'Riquadri delle metriche',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:queries.common.meanTime}}',
                  meaning: 'Tempo medio di esecuzione di una chiamata nell’intervallo.',
                  action:
                    'Confrontalo con il piano. Un valore alto con poche chiamate indica un piano costoso: vedi [{{ui:queries.explain.planOnly}} (EXPLAIN)](/help/query-performance#plan-only).',
                },
                {
                  term: '{{ui:queries.common.totalTime}}',
                  meaning:
                    'Tempo di esecuzione di tutte le chiamate nell’intervallo. È la misura che fa pesare uno statement sul database.',
                  action:
                    'Confrontalo con gli altri statement: nell’ispettore ordina per {{ui:queries.common.totalTime}} e leggi {{ui:queries.list.shareOfListed}}.',
                },
                {
                  term: '{{ui:queries.common.calls}}',
                  meaning: 'Numero di esecuzioni nell’intervallo.',
                  action:
                    'Un conteggio alto con una media bassa suggerisce un ciclo nell’applicazione che ripete lo statement.',
                },
                {
                  term: '{{ui:queries.overview.rowsPerCall}}',
                  meaning: 'Righe restituite o modificate per chiamata.',
                  action:
                    'Un valore alto indica risultati grandi a ogni chiamata. Verifica i filtri e l’eventuale LIMIT nella query dell’applicazione.',
                },
                {
                  term: '{{ui:queries.overview.cacheHitRatio}}',
                  meaning:
                    'Quota dei blocchi condivisi acceduti da questo statement che erano nello shared buffer, in percentuale. Una lettura può arrivare ancora dalla cache del sistema operativo, quindi un valore più basso non è sempre I/O su disco.',
                  action:
                    'Verifica gli indici delle tabelle che legge in [{{ui:shell.nav.indexAnalysis}}](/help/indexes).',
                },
                {
                  term: 'WAL generated',
                  meaning:
                    'Write-ahead log generato da tutte le chiamate dello statement, in byte.',
                  action:
                    'Verifica se scrive più righe del necessario, e se gira in un ciclo o in lotti grandi.',
                },
              ],
            },
          ],
        },
        {
          id: 'missing-statement',
          title: 'Statement assente nell’intervallo',
          blocks: [
            {
              kind: 'text',
              text: 'La pagina cerca lo statement tra i primi 1000 dell’intervallo, ordinati per {{ui:queries.common.totalTime}}, dentro `db` quando è impostato. Se non compare, i riquadri delle metriche sono sostituiti da un messaggio che indica che per questa query non è stata registrata attività di `pg_stat_statements` nell’intervallo, quindi sono mostrati solo i suoi piani. I piani restano disponibili sotto. Allarga l’intervallo o verifica lo statement nell’ispettore.',
            },
          ],
        },
      ],
    },
    {
      id: 'execution-plan',
      title: 'Piano di esecuzione',
      blocks: [
        {
          kind: 'text',
          text: 'La sezione Execution plan mostra come PostgreSQL esegue questo statement: le operazioni che sceglie, le righe che ognuna tratta e dove va il tempo. L’agent esegue `EXPLAIN` sul database monitorato, sempre in una transazione in sola lettura annullata al termine, e il server conserva il risultato nello storico dei piani qui sotto. La richiesta è un comando: mentre è in attesa i pulsanti sono disabilitati e il pannello mostra Command pending: waiting for the agent.',
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'Prima di pianificare, l’agent legge il codice delle funzioni utente che lo statement può raggiungere, anche tramite viste e altre funzioni. Rifiuta solo quando quel codice può scrivere o eseguire SQL dinamico (`EXECUTE`, `COPY`, `DO`) o è scritto in C o internal; il rifiuto nomina la funzione. Le funzioni semplici, come un controllo di conversione, vengono pianificate normalmente.',
        },
      ],
      subsections: [
        {
          id: 'plan-only',
          title: '{{ui:queries.explain.planOnly}} (EXPLAIN)',
          blocks: [
            {
              kind: 'text',
              text: 'Il pulsante {{ui:queries.explain.planOnly}} (nome accessibile Run plan only) chiede un piano al planner e non esegue nulla: costi e righe sono stime. Richiede il livello di permesso T1. Sotto T1 il pulsante resta disabilitato e il pannello mostra Requires T1 permission; current tier is T0.',
            },
          ],
        },
        {
          id: 'plan-with-analyze',
          title: '{{ui:queries.explain.planAnalyze}}',
          blocks: [
            {
              kind: 'text',
              text: 'Il pulsante {{ui:queries.explain.planAnalyze}} esegue lo statement per misurare tempi e righe reali, con EXPLAIN ANALYZE. La console lo offre solo al livello T2, e l’agent deve anche consentirlo con `allow_explain_analyze`. Prima della richiesta, una finestra intitolata Run EXPLAIN ANALYZE? descrive l’impatto. Cancel annulla; Run EXPLAIN ANALYZE invia la richiesta.',
            },
            {
              kind: 'note',
              tone: 'warning',
              text: 'EXPLAIN ANALYZE esegue lo statement. Gira in una transazione in sola lettura annullata al termine, ma tempo, risorse e lock che usa sono reali. Su un primario carico preferisci {{ui:queries.explain.planOnly}}, oppure esegui ANALYZE quando il carico è basso.',
            },
          ],
        },
        {
          id: 'plan-parameters',
          title: 'Valori dei parametri',
          blocks: [
            {
              kind: 'text',
              text: 'pg_stat_statements sostituisce le costanti di uno statement con `$1`, `$2`, …. Quando lo statement li contiene, il riquadro Parameter values offre un campo per segnaposto. Scrivi ogni valore come in SQL, senza apici; spunta NULL per il NULL SQL. Compilali tutti o nessuno: se ne manca qualcuno, entrambi i pulsanti restano disabilitati.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Values you entered',
                  meaning:
                    'L’agent lega i tuoi valori come parametri, mai scritti nel testo dello statement, e PostgreSQL pianifica per quei valori.',
                },
                {
                  term: 'Values of a recent execution',
                  meaning:
                    'Con i campi vuoti, ANALYZE prende lo statement di una sessione che pg_stat_activity mostra mentre lo esegue nello stesso database. Se nessuna sessione lo esegue, l’agent rifiuta e chiede i valori.',
                },
                {
                  term: 'Generic plan, no values',
                  meaning:
                    'Con i campi vuoti, {{ui:queries.explain.planOnly}} chiede un piano generico, valido per qualsiasi valore (PostgreSQL 16 e successivi). Con valori reali PostgreSQL può scegliere un altro piano.',
                },
              ],
            },
          ],
        },
        {
          id: 'plan-result',
          title: 'Piano della query',
          blocks: [
            {
              kind: 'text',
              text: 'Quando l’agent risponde, la sezione Query plan mostra un riepilogo (costo totale, tempo di esecuzione e di pianificazione, righe, buffer, provenienza dei valori dei parametri), il riquadro What to look at e il piano. Il selettore Tree, Text, JSON cambia vista: Tree è la tabella Query plan tree; Text stampa il piano come psql, da incollare dove si legge il testo di EXPLAIN; JSON è il piano esattamente come l’ha restituito l’agent. Text e JSON hanno un pulsante di copia.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'What to look at',
                  meaning:
                    'I problemi che pglens legge nel piano, dal più grave: l’operazione che prende più tempo, stime di righe sbagliate di 10 volte o più, un Seq Scan che scarta gran parte delle righe che legge, un sort o un hash finiti su disco, una scansione ripetuta migliaia di volte, operazioni mai eseguite.',
                  action:
                    'Parti da qui. {{ui:queries.planView.showInPlan}} apre l’operazione nell’albero, con i suoi dettagli.',
                },
                {
                  term: 'Operation',
                  meaning:
                    'Il passo come lo chiama PostgreSQL nel suo formato testo (Seq Scan, Hash Left Join, HashAggregate, …), la tabella o l’indice su cui lavora e le sue condizioni (Index Cond, Filter, Hash Cond, …). I figli sono rientrati sotto il padre. La freccia mostra i dettagli: righe scartate da un filtro, metodo e memoria del sort, batch dell’hash, worker, costo di avvio, larghezza della riga, colonne in uscita.',
                },
                {
                  term: 'Rows (actual / est.)',
                  meaning:
                    'Righe prodotte dall’operazione in tutti i suoi cicli, e sotto la stima del planner ({{ui:queries.common.rowsEstimated}} in un plan only). Un badge ×N under o ×N over segnala una stima sbagliata di N volte.',
                  action:
                    'Uno scarto grande indica statistiche vecchie o mancanti: esegui `ANALYZE` sulla tabella, o `CREATE STATISTICS` per colonne correlate, poi richiedi un nuovo piano.',
                },
                {
                  term: 'Loops',
                  meaning:
                    'Quante volte l’operazione è stata eseguita, di solito una volta per riga del lato esterno di un Nested Loop. Righe e tempo sono già totali su tutti i cicli.',
                },
                {
                  term: '{{ui:queries.common.timeInNode}}',
                  meaning:
                    'Tempo speso nell’operazione stessa, senza quelle sotto di essa, e la sua quota del tempo di esecuzione; la barra diventa ambra dal 30% e rossa dal 60%. Sotto Gather, il tempo dei worker paralleli è il tempo reale, non la loro somma. In un plan only la colonna è {{ui:queries.planView.costInNode}}, in unità del planner, non un tempo.',
                  action:
                    'La barra più larga è dove va il tempo: ottimizza prima quell’operazione.',
                },
                {
                  term: 'Buffers',
                  meaning:
                    'Pagine di dati usate dall’operazione, in blocchi da 8 kB: cached trovate negli shared buffers, read lette dal sistema operativo o dal disco.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'plan-history',
      title: 'Storico dei piani',
      blocks: [
        {
          kind: 'text',
          text: 'Lo storico dei piani conserva ogni piano richiesto qui sopra per questo statement, dal più recente, al massimo 20 voci. Una voce nasce da un piano richiesto da qualcuno o, con `auto_explain`, da un’esecuzione lenta (vedi Piani automatici). Risponde a una domanda: PostgreSQL ha cambiato il modo in cui esegue questo statement, e dove? pglens calcola un’impronta della forma di ogni piano: operazioni, tabelle, indici e condizioni, non costi o tempi. Una forma nuova compare dopo un indice creato o eliminato, statistiche aggiornate, un parametro di configurazione cambiato o valori dei parametri diversi.',
        },
      ],
      subsections: [
        {
          id: 'plan-captures',
          title: 'Piani acquisiti',
          blocks: [
            {
              kind: 'text',
              text: 'Sopra la tabella, N captures · M distinct shapes conta le voci e le loro forme. La tabella Plan history entries ha una riga per acquisizione. Spunta una riga per leggere quel piano sotto la tabella, con la stessa vista di Query plan; spuntane due per confrontarle. Spuntando una terza riga si toglie la più vecchia delle due.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Kind',
                  meaning: 'EXPLAIN per un plan only, ANALYZE per un piano con tempi reali.',
                },
                {
                  term: 'Shape',
                  meaning:
                    'I primi caratteri dell’impronta della forma (`plan_hash`): due acquisizioni con lo stesso codice eseguono lo statement allo stesso modo. {{ui:queries.planHistory.newShape}} segnala un’acquisizione con forma diversa da quella precedente.',
                  action:
                    'Quando compare {{ui:queries.planHistory.newShape}}, confronta quell’acquisizione con quella precedente.',
                },
                {
                  term: '{{ui:queries.common.totalCost}}, Execution time, Rows',
                  meaning:
                    'Costo del planner dell’intero piano, tempo di esecuzione misurato (solo ANALYZE) e righe restituite, o previste (est.) per un plan only.',
                  action:
                    'Confronta un EXPLAIN con un EXPLAIN per costo, e un ANALYZE con un ANALYZE per tempo.',
                },
                {
                  term: 'Top operation',
                  meaning:
                    'L’operazione alla radice del piano, con la sua tabella quando ne ha una.',
                },
              ],
            },
          ],
        },
        {
          id: 'plan-stats',
          title: 'Statistiche per piano',
          blocks: [
            {
              kind: 'text',
              text: 'La pagina confronta i piani di uno statement, ciascuno identificato dall’impronta della sua forma (`plan_hash`). Con `auto_explain` attivo ogni esecuzione lenta è un campione: la tabella {{ui:queries.planStats.entriesLabel}} raggruppa per piano i campioni degli ultimi sette giorni, dal più recente. Sotto, una riga per ogni momento in cui il piano è cambiato: un campione con un piano diverso dal campione precedente nello stesso database. Spunta due piani per confrontarli: gli alberi come in Confronto dei piani, più le loro statistiche affiancate.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:queries.planStats.samples}}',
                  meaning: 'Quante esecuzioni `auto_explain` ha registrato con questo piano.',
                },
                {
                  term: '{{ui:queries.planStats.avg}}, {{ui:queries.planStats.p95}}',
                  meaning:
                    'Durata media e durata sotto cui resta il 95% delle esecuzioni (`percentile_cont(0.95)`).',
                  action:
                    'Dopo un cambio di piano, un p95 più alto sul nuovo piano indica una regressione; confronta i due piani.',
                },
                {
                  term: '{{ui:queries.planStats.firstSeen}}, {{ui:queries.planStats.lastSeen}}',
                  meaning: 'La prima e l’ultima esecuzione registrata con questo piano.',
                },
                {
                  term: '{{ui:queries.planStats.noBody}}',
                  meaning:
                    'Esistono campioni ma nessun albero memorizzato per questa forma, quindi non si può spuntare né confrontare.',
                },
              ],
            },
          ],
        },
        {
          id: 'plan-comparison',
          title: 'Confronto dei piani',
          blocks: [
            {
              kind: 'text',
              text: 'Con due acquisizioni spuntate, la sezione Plan comparison mette la più vecchia contro la più recente, in qualunque ordine le hai spuntate. Una frase dice se la forma è la stessa, o quante operazioni sono cambiate. Seguono {{ui:queries.common.totalCost}}, Execution time e Rows dei due piani, come vecchio → nuovo con la variazione in percentuale: rossa quando cresce, verde quando cala. La tabella Plan comparison elenca le operazioni dei due piani, abbinate per posizione nell’albero.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Change',
                  meaning:
                    '{{ui:queries.planHistory.changeSame}}: stessa operazione, oggetto e condizioni in quella posizione. {{ui:queries.planHistory.changeChanged}}: un’altra operazione, tabella, indice o condizione; la vecchia è barrata sopra la nuova. {{ui:queries.planHistory.changeAdded}} e {{ui:queries.planHistory.changeRemoved}}: l’operazione esiste solo nel piano più recente o solo in quello più vecchio.',
                },
                {
                  term: '{{ui:queries.common.totalCost}}, Rows, {{ui:queries.common.timeInNode}}',
                  meaning:
                    'Ogni valore come vecchio → nuovo. {{ui:queries.common.timeInNode}} compare quando entrambi i piani sono ANALYZE; Rows sono reali quando entrambi sono ANALYZE, stime altrimenti.',
                },
                {
                  term: '{{ui:queries.planHistory.difference}}',
                  meaning:
                    'Filtro con due opzioni: {{ui:queries.planHistory.differs}} mostra solo le operazioni cambiate, aggiunte o rimosse; {{ui:queries.planHistory.changeSame}} mostra solo quelle uguali.',
                  action:
                    'Parti da {{ui:queries.planHistory.differs}} per trovare dove i piani divergono: un Seq Scan al posto di un Index Scan, o un metodo di join cambiato, è la causa tipica di uno statement più lento.',
                },
              ],
            },
          ],
        },
        {
          id: 'automatic-plans',
          title: 'Piani automatici',
          blocks: [
            {
              kind: 'text',
              text: 'Con `auto_explain` caricato, PostgreSQL scrive nel log il piano di ogni statement più lento di `auto_explain.log_min_duration`. Il lettore del log dell’agent (`checks.logs`) raccoglie questi piani e il server salva ogni forma nuova una sola volta nello storico dei piani, con origine {{ui:queries.planHistory.sourceAuto}}; i piani che richiedi hanno origine {{ui:queries.planHistory.sourceCommand}}. Seleziona una cattura automatica per vedere {{ui:queries.planSamples.title}}: una riga per ogni esecuzione lenta con quella forma, con ora e durata.',
            },
            {
              kind: 'sql',
              label: 'Carica auto_explain nelle nuove sessioni, piani solo in JSON',
              code: "ALTER SYSTEM SET session_preload_libraries = 'auto_explain';\nALTER SYSTEM SET auto_explain.log_min_duration = '500ms';\nALTER SYSTEM SET auto_explain.log_format = 'json';\nSELECT pg_reload_conf();",
            },
            {
              kind: 'note',
              tone: 'warning',
              text: '`auto_explain.log_analyze` misura ogni statement, non solo quelli lenti, e può rallentare tutto il server: attivalo solo per un’indagine breve. Senza, i campioni contengono piani con le sole stime.',
            },
            {
              kind: 'text',
              text: 'I valori dei parametri (`checks.logs.plan_parameters`) sono spenti per impostazione predefinita: possono contenere dati personali. Quando l’agent li invia, {{ui:queries.planSamples.showParameters}} li mostra, e `DELETE /api/v1/plans/samples/parameters` li cancella per un’istanza mantenendo i campioni. Un piano senza `auto_explain.log_format = json` non viene letto: il report di readiness mostra `RDY-AEX-01`.',
            },
          ],
        },
      ],
    },
    {
      id: 'trends-over-time',
      title: 'Andamento nel tempo',
      blocks: [
        {
          kind: 'text',
          text: 'Le pagine delle query non hanno grafici: i loro valori sono totali o medie sull’intervallo selezionato, non serie temporali. Per l’andamento, apri i grafici di [{{ui:shell.nav.instanceOverview}}](/help/instance-overview), sezione Queries: Calls per second, Execution time per second, Rows per second, Statement block reads e Statement temp blocks. Scegli l’intervallo con il selettore di intervallo, poi torna qui per trovare lo statement.',
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'I grafici della {{ui:shell.nav.instanceOverview}} mostrano l’istanza nel suo insieme, non un singolo statement.',
        },
      ],
    },
    {
      id: 'compare-periods',
      title: 'Confrontare due periodi',
      blocks: [
        {
          kind: 'text',
          text: 'Passa la pagina a {{ui:queries.compare.modeCompare}} per vedere ogni statement in due periodi affiancati. Il periodo B è l’intervallo del selettore temporale; il periodo A è {{ui:queries.compare.preset.previous}}, {{ui:queries.compare.preset.yesterday}}, {{ui:queries.compare.preset.week}} o un intervallo {{ui:queries.compare.preset.custom}}. Ogni valore è al secondo del proprio periodo (il tempo medio resta per chiamata), quindi periodi di durata diversa si confrontano correttamente.',
        },
        {
          kind: 'steps',
          items: [
            'Scegli il periodo B con il selettore temporale, per esempio l’ora del rallentamento.',
            'Fai clic su {{ui:queries.compare.modeCompare}}, poi scegli {{ui:queries.compare.presetLabel}}.',
            'Scegli la {{ui:queries.compare.metricLabel}}. Le righe seguono l’ampiezza della variazione, dalla più grande.',
            'Leggi {{ui:queries.compare.colRatio}}: ×3,0 significa il triplo del periodo A, e un rapporto di ×2,0 o più è evidenziato. {{ui:queries.compare.new}} è stato eseguito solo in B, {{ui:queries.compare.gone}} solo in A.',
            'Apri uno statement dal suo link per esaminarlo.',
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'Il confronto legge `GET /api/v1/statements/compare`; il suo URL conserva `mode=compare`, `compare` e, per un periodo personalizzato, `a_from` e `a_to`. Ogni periodo può durare al massimo 31 giorni.',
        },
      ],
    },
    {
      id: 'act-on-slow-query',
      title: 'Come intervenire su una query lenta o in regressione',
      blocks: [
        {
          kind: 'steps',
          items: [
            'Apri l’ispettore delle query e tieni {{ui:queries.common.totalTime}} per trovare gli statement che costano di più al database. Poi scegli {{ui:queries.common.meanTime}} per trovare quelli lenti a ogni chiamata.',
            'Apri lo statement. Leggi {{ui:queries.common.meanTime}} e {{ui:queries.common.calls}} nei riquadri, poi il riquadro {{ui:queries.overview.statementText}}.',
            'Clicca {{ui:queries.explain.planOnly}}. Se è disabilitato, il livello dell’istanza è sotto T1.',
            'Leggi l’albero del piano dall’alto. Verifica il nodo con il {{ui:queries.common.totalCost}} più alto, i grandi scarti tra Estimated rows e Actual rows, e i sort o gli hash che finiscono su disco, che si vedono da un valore alto di {{ui:queries.list.tempBytesPerCall}}.',
            'Se servono tempi misurati, clicca {{ui:queries.explain.planAnalyze}}, leggi la finestra e conferma solo se l’impatto è accettabile.',
            'Confronta con lo storico dei piani: seleziona un piano più vecchio e quello nuovo, poi imposta il filtro {{ui:queries.planHistory.difference}} su {{ui:queries.planHistory.differs}}. Un tipo di nodo o un costo cambiato indica la modifica che ha rallentato lo statement.',
            'Verifica le tabelle che legge in [{{ui:shell.nav.indexAnalysis}}](/help/indexes). Applica il correttivo tu, prima su una copia di test.',
            'Verifica i finding in [{{ui:shell.nav.findings}}](/help/findings): la regola dell’advisor `query.regression` è critica quando il tempo medio di uno statement è almeno 3 volte la sua baseline e almeno 100 ms.',
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'L’ispettore non mostra da solo nessun segnale di regressione. La regressione è segnalata dalla regola dell’advisor qui sotto.',
        },
      ],
      subsections: [
        {
          id: 'advisor-rules',
          title: 'Regole dell’advisor per le query',
          blocks: [
            {
              kind: 'text',
              text: 'L’advisor valuta le statistiche raccolte e scrive i risultati nella pagina {{ui:shell.nav.findings}}. Queste sono le regole per le query; ogni finding ha una gravità e uno stato.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Slow query',
                  meaning:
                    'Regola `query.slow_mean`, severità warning: uno statement con almeno 10 chiamate e un tempo medio di esecuzione sopra 500 ms.',
                  action:
                    'Richiedi [{{ui:queries.explain.planOnly}} (EXPLAIN)](/help/query-performance#plan-only) e leggi il nodo con il costo totale più alto.',
                },
                {
                  term: 'Dominant query',
                  meaning:
                    'Regola `query.total_time_share`, severità warning: uno statement assorbe più del 30% del tempo di esecuzione totale caricato dall’advisor.',
                  action: 'Parti da questo statement: domina il carico.',
                },
                {
                  term: 'Query regression',
                  meaning:
                    'Regola `query.regression`, severità critical: il tempo medio è almeno 3 volte la baseline dello statement e almeno 100 ms. La baseline è la mediana del suo tempo medio nello storico precedente, circa otto giorni, escludendo l’ultima ora. Senza baseline il finding è degraded, con il motivo insufficient query history.',
                  action: 'Confronta i piani nello storico, prima e dopo il cambio di tempo medio.',
                },
                {
                  term: 'High temporary write volume',
                  meaning:
                    'Regola `query.temp_bytes_high`, severità warning: byte temporanei medi per chiamata sopra `work_mem`, o sopra 4 MiB quando l’impostazione non viene letta.',
                  action:
                    'Vedi [{{ui:queries.list.tempBytesPerCall}}](/help/query-performance#statement-statistics).',
                },
                {
                  term: 'High cache miss ratio',
                  meaning:
                    'Regola `query.cache_miss_high`, severità info: più del 30% dei blocchi letti arriva da fuori dello shared buffer, su uno statement con più di 1000 chiamate.',
                  action:
                    'Verifica gli indici delle tabelle che legge in [{{ui:shell.nav.indexAnalysis}}](/help/indexes).',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'troubleshooting',
      title: 'Risoluzione dei problemi',
      blocks: [
        {
          kind: 'terms',
          items: [
            {
              term: 'pg_stat_statements disabled',
              meaning:
                'L’agent ha saltato il controllo stat_statements, quindi non vengono raccolte nuove statistiche sugli statement. Il banner riporta il motivo.',
              action:
                'Aggiungi `pg_stat_statements` a `shared_preload_libraries`, riavvia PostgreSQL, poi crea l’estensione in un database monitorato. Verifica prima con i controlli SQL qui sotto.',
            },
            {
              term: '{{ui:queries.explain.planAnalyze}} disabled',
              meaning:
                'Il pulsante è disabilitato, oppure il pannello mostra Requires T2 permission. La console richiede il livello T2, e l’agent deve consentire `allow_explain_analyze`.',
              action:
                'Verifica il livello di permesso dell’istanza e la configurazione dell’agent. Usa ANALYZE solo per gli statement il cui impatto accetti.',
            },
            {
              term: 'No plan history yet',
              meaning: 'Per questo statement non è ancora stato richiesto alcun piano.',
              action: 'Clicca {{ui:queries.explain.planOnly}} per creare la prima voce.',
            },
            {
              term: 'Command pending: waiting for the agent.',
              meaning:
                'La richiesta attende che l’agent la prenda in carico, oppure è ancora in esecuzione.',
              action:
                'Attendi. Se il messaggio resta, verifica che l’agent sia in esecuzione e connesso.',
            },
            {
              term: 'Database not monitored.',
              meaning:
                'Il parametro `db` indica un database non monitorato su questa istanza. Il banner riporta “Database not monitored.” e lo statement non viene spiegato né pianificato qui.',
              action:
                'Riapri lo statement dall’ispettore, oppure verifica la lista dei database in [{{ui:shell.nav.settings}}](/help/settings).',
            },
          ],
        },
        {
          kind: 'sql',
          label: 'Verifica che l’estensione sia installata in questo database',
          code: "SELECT extname, extversion FROM pg_extension WHERE extname = 'pg_stat_statements';",
        },
        {
          kind: 'sql',
          label: 'Verifica l’impostazione di preload',
          code: 'SHOW shared_preload_libraries;',
        },
        {
          kind: 'sql',
          label: 'Statement con più tempo totale (PostgreSQL 13 o successivo)',
          code: 'SELECT queryid, calls, round(total_exec_time::numeric, 1) AS total_ms, round(mean_exec_time::numeric, 1) AS mean_ms, left(query, 80) AS query_start FROM pg_stat_statements ORDER BY total_exec_time DESC LIMIT 20;',
        },
        {
          kind: 'sql',
          label: 'Eviction dall’ultimo reset (PostgreSQL 14 o successivo)',
          code: 'SELECT dealloc, stats_reset FROM pg_stat_statements_info;',
        },
      ],
    },
  ],
}
