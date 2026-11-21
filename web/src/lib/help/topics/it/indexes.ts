import type { HelpTopic } from '../../types'

export const indexesTopic: HelpTopic = {
  id: 'indexes',
  group: 'monitoring',
  title: 'Analisi degli indici',
  summary:
    'Tutto sugli indici di un server: cosa esiste, cosa manca, cosa si può eliminare, cosa lascia indietro il vacuum e quanto costano gli indici a ogni scrittura. Ogni valore è spiegato con come leggerlo e cosa fare.',
  page: {
    path: '/indexes',
    label: 'Apri Index analysis',
  },
  sections: [
    {
      id: 'overview',
      title: 'A cosa serve la pagina',
      blocks: [
        {
          kind: 'text',
          text: "Un indice velocizza le letture che filtrano, uniscono (join) o ordinano sulle sue colonne, e rallenta ogni insert, update e delete della sua tabella, che deve aggiornare anche l'indice. Gestire gli indici significa fare due cicli: creare gli indici che mancano al carico di lavoro ed eliminare quelli che non usa.",
        },
        {
          kind: 'steps',
          items: [
            '[{{ui:indexes.page.tabMissing}}](/help/indexes#missing): crea ciò che è marcato {{ui:indexes.lib.missing.label.create}}, prova ciò che è marcato {{ui:indexes.lib.missing.label.test}}.',
            "[{{ui:indexes.page.tabCleanup}}](/help/indexes#cleanup): elimina gli indici invalid, duplicate e redundant, poi quelli unused confermati dall'advisor.",
            '[{{ui:indexes.page.tabDeadRows}}](/help/indexes#dead-rows): verifica che il vacuum stia al passo sulle tabelle più attive, altrimenti tabelle e indici si gonfiano (bloat).',
            '[{{ui:indexes.page.tabTax}}](/help/indexes#tax): sulle tabelle con molte scritture tieni solo gli indici che ripagano il loro costo in scrittura.',
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'pglens non modifica mai il database. Ogni CREATE e DROP mostrato in queste pagine è un comando che devi verificare ed eseguire tu, in psql, fuori dalle ore di punta, uno alla volta.',
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
                  term: '{{ui:indexes.search.box}}',
                  meaning:
                    'Trova indici, tabelle, colonne e statement il cui nome, definizione o testo contiene il termine, nel database selezionato. I risultati sono raggruppati in {{ui:indexes.search.groupIndexes}}, {{ui:indexes.search.groupTables}}, {{ui:indexes.search.groupColumns}} e {{ui:indexes.search.groupQueries}}; ognuno è un link alla pagina che lo mostra.',
                },
                {
                  term: 'Tab',
                  meaning:
                    '{{ui:indexes.page.tabIndexes}}, {{ui:indexes.page.tabMissing}}, {{ui:indexes.page.tabCleanup}}, {{ui:indexes.page.tabDeadRows}}, {{ui:indexes.page.tabTax}}: ognuna è spiegata più sotto.',
                },
                {
                  term: '{{ui:indexes.page.server}}',
                  meaning:
                    "L'istanza PostgreSQL analizzata. Le statistiche sono per server: uno standby ha i propri contatori di scansione.",
                },
                {
                  term: '{{ui:indexes.page.database}}',
                  meaning:
                    'Limita ogni tab a un database; {{ui:indexes.page.allDatabases}} li mostra tutti insieme.',
                },
                {
                  term: 'Updated … ago',
                  meaning: 'Età dei dati mostrati. La pagina si aggiorna da sola.',
                },
              ],
            },
          ],
        },
        {
          id: 'data-sources',
          title: 'Da dove vengono i numeri',
          blocks: [
            {
              kind: 'text',
              text: "L'agent legge `pg_stat_user_indexes`, `pg_statio_user_indexes`, `pg_stat_user_tables`, `pg_stat_statements` e il catalogo ogni pochi minuti. Un tasso (al secondo) è calcolato sull'ultimo intervallo di raccolta. I contatori di PostgreSQL ripartono da zero dopo un reset delle statistiche o un crash.",
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:indexes.cleanup.basisAdvisor}}',
                  meaning:
                    'Un verdetto dell’advisor di pglens su una finestra di tempo (unused, duplicate, invalid, foreign key senza indice…). È elencato anche nella pagina [{{ui:shell.nav.findings}}](/findings).',
                  action: 'Agisci dopo averne letto la remediation.',
                },
                {
                  term: 'Contatore grezzo',
                  meaning:
                    "Ciò che PostgreSQL ha contato dall'ultimo reset (per esempio never scanned). È un indizio, non un verdetto.",
                  action: 'Tienilo d’occhio; non eliminare basandoti solo su questo.',
                },
                {
                  term: 'Indici riportati',
                  meaning:
                    "L'agent riporta solo gli indici più usati e più grandi di ogni database (`checks.index_stats.top_n`, 50 di default) e la pagina ne mostra al massimo 500. Quando la lista è incompleta la tab {{ui:indexes.page.tabIndexes}} lo dice: un indice che non vedi può comunque esistere. Aumenta top_n nella configurazione dell'agent per vederne di più.",
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'inventory',
      title: 'Tab Indexes',
      blocks: [
        {
          kind: 'text',
          text: 'L\'inventario: una riga per indice. Usalo per vedere cosa esiste già su una tabella prima di aggiungere un indice, e per trovare quelli segnalati. Clicca una riga per espanderla: definizione, grafici, finding e "What to do".',
        },
      ],
      subsections: [
        {
          id: 'inventory-columns',
          title: 'Colonne',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:indexes.panel.colIndex}}, {{ui:indexes.panel.colTable}}, {{ui:indexes.panel.colColumns}}',
                  meaning:
                    "Nome, tabella (database · schema.tabella) e colonne chiave dell'indice. Le colonne INCLUDE e il predicato WHERE sono nel dettaglio della riga.",
                },
                {
                  term: '{{ui:indexes.panel.colMethod}}',
                  meaning:
                    'btree: uguaglianza, intervalli e ordinamento (il default). gist: dati spaziali e range. gin: array, jsonb, ricerca full-text. brin: tabelle molto grandi scritte in ordine (serie storiche). hash: solo uguaglianza.',
                },
                {
                  term: '{{ui:indexes.panel.colFlags}}',
                  meaning:
                    "Etichette sull'indice. Quelle grigie e bordate lo descrivono (primary, unique, partial…); quelle arancioni e rosse sono problemi. I problemi vengono mostrati per primi. Passa il mouse su un flag per significato e rimedio; sono tutti spiegati in [Flags](/help/indexes#flags).",
                },
                {
                  term: '{{ui:indexes.panel.colSize}}',
                  meaning:
                    "Dimensione su disco dell'indice (`pg_relation_size`). Ogni byte finisce anche in WAL, backup e repliche.",
                },
                {
                  term: '{{ui:indexes.panel.colScans}}',
                  meaning:
                    "Scansioni dell'indice al secondo nell'ultimo intervallo. {{ui:indexes.panel.scansNoneSinceReset}} significa che il contatore è ancora 0.",
                  action:
                    'Un indice grande fermo a 0 per giorni è candidato all’eliminazione: vedi [{{ui:indexes.page.tabCleanup}}](/help/indexes#cleanup).',
                },
                {
                  term: '{{ui:indexes.panel.colCacheHit}}',
                  meaning:
                    "Quota delle letture di blocchi dell'indice servite dalla memoria di PostgreSQL (shared_buffers). Sotto il 90% su un indice molto usato significa che non sta in memoria.",
                  action:
                    "Aumenta shared_buffers, oppure rendi l'indice più piccolo (indice parziale, meno colonne).",
                },
                {
                  term: '{{ui:indexes.panel.colLastScan}}',
                  meaning:
                    "Quando una query ha usato l'indice l'ultima volta. {{ui:indexes.panel.lastScanNever}}: nessuna scansione dal reset dei contatori. Solo PostgreSQL 16 e successivi; i server più vecchi mostrano {{ui:indexes.panel.notAvailable}}.",
                },
              ],
            },
          ],
        },
        {
          id: 'inventory-views',
          title: 'Viste della tab Indexes',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:indexes.view.inventory}}',
                  meaning: 'Ogni indice, uno per riga (il default).',
                },
                {
                  term: '{{ui:indexes.view.summary}}',
                  meaning:
                    'Conteggi per tipo, metodo di accesso, database e tabella. I conteggi per tipo si sovrappongono: un indice unique parziale è sia unique sia partial.',
                },
                {
                  term: '{{ui:indexes.view.findings}}',
                  meaning:
                    "Tutti i finding aperti dell'advisor sugli indici, con il dettaglio e la remediation scritti dall'advisor.",
                },
                {
                  term: '{{ui:indexes.view.foreignKeys}}',
                  meaning:
                    'Foreign key le cui colonne non sono all’inizio di nessun indice. I comandi pronti da eseguire sono in [{{ui:indexes.page.tabMissing}}](/help/indexes#missing-foreign-keys).',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'flags',
      title: 'I flag, uno per uno',
      blocks: [
        {
          kind: 'text',
          text: 'I flag della tab Indexes. Per primi i problemi: per ognuno è indicato cosa significa e cosa fare esattamente.',
        },
      ],
      subsections: [
        {
          id: 'flags-problems',
          title: 'Flag di problema',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'invalid',
                  meaning:
                    'Un CREATE INDEX CONCURRENTLY o REINDEX CONCURRENTLY è fallito e ha lasciato l’indice costruito a metà: le query non lo usano mai, ma ogni scrittura lo aggiorna comunque.',
                  action:
                    'Eliminalo e ricrealo (entrambi i comandi sono nel dettaglio della riga), oppure esegui REINDEX INDEX CONCURRENTLY.',
                },
                {
                  term: 'unused',
                  meaning:
                    'L’advisor non ha visto nessuna scansione di questo indice, su questo server e sui suoi standby, per tutta la finestra di osservazione (7 giorni di default).',
                  action:
                    'Verifica che nessun job raro (report di fine mese, batch annuale) lo usi, poi eliminalo dalla tab Cleanup. Conserva il suo CREATE per poter tornare indietro.',
                },
                {
                  term: 'duplicate',
                  meaning:
                    'Un altro indice della stessa tabella ha esattamente la stessa definizione: entrambi vengono aggiornati a ogni scrittura, ma ne serve uno solo.',
                  action:
                    'Tienine uno (la tab Cleanup indica quale) ed elimina l’altro. Se sono entrambi unique, elimina quello che non sostiene un constraint.',
                },
                {
                  term: 'redundant prefix',
                  meaning:
                    'Le sue colonne sono le colonne iniziali di un indice più ampio, che può servire le stesse ricerche.',
                  action:
                    'Eliminalo, a meno che sia unique, molto più piccolo e molto usato. Verifica con EXPLAIN che venga scelto l’indice più ampio.',
                },
                {
                  term: 'diverges',
                  meaning:
                    'La stessa tabella ha indici diversi sui server del cluster (per esempio su una replica logica).',
                  action:
                    'Crea o elimina l’indice in modo che ogni server che serve questa tabella abbia lo stesso insieme di indici.',
                },
                {
                  term: 'never scanned',
                  meaning:
                    'Nessuna scansione dall’ultimo reset delle statistiche. È il contatore grezzo di PostgreSQL, non un verdetto: il reset può essere recente, o l’indice troppo piccolo perché l’advisor lo giudichi.',
                  action:
                    'Non eliminarlo ancora: aspetta che l’advisor lo segnali come unused, oppure controlla quanto sono vecchi i contatori (pg_stat_database.stats_reset).',
                },
              ],
            },
          ],
        },
        {
          id: 'flags-kinds',
          title: 'Flag descrittivi',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'primary',
                  meaning: 'Sostiene la chiave primaria della tabella.',
                  action: 'Nessuna azione: non eliminarlo mai.',
                },
                {
                  term: 'unique',
                  meaning:
                    'Garantisce che due righe non abbiano la stessa chiave (constraint o indice UNIQUE).',
                  action:
                    'Tienilo: protegge i dati, non solo le prestazioni. Eliminalo solo se è il duplicato di un altro indice unique.',
                },
                {
                  term: 'partial',
                  meaning:
                    'Copre solo le righe che soddisfano la sua clausola WHERE, quindi è più piccolo.',
                  action:
                    'Una query lo usa solo se la sua WHERE implica la stessa condizione: il predicato è nel dettaglio della riga.',
                },
                {
                  term: 'expression',
                  meaning:
                    'Indicizza un’espressione, come lower(email), invece di una colonna semplice.',
                  action: 'Una query lo usa solo se scrive esattamente la stessa espressione.',
                },
                {
                  term: 'include',
                  meaning:
                    'Contiene colonne INCLUDE aggiuntive, così una query può essere risolta leggendo solo l’indice (index-only scan).',
                  action:
                    'Nessuna azione. Tieni poche colonne incluse: ognuna rende più pesante ogni scrittura.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'missing',
      title: 'Tab Missing indexes',
      blocks: [
        {
          kind: 'text',
          text: 'Dove manca un indice, perché, e il comando esatto per crearlo. Una sola lista, dalla più importante, alimentata da quattro fonti. Espandi una riga per i quattro passi {{ui:indexes.lib.missing.step.title.check}}, {{ui:indexes.lib.missing.step.title.create}}, {{ui:indexes.lib.missing.step.title.verify}} e {{ui:indexes.lib.missing.step.title.undo}}, ognuno con il suo SQL.',
        },
      ],
      subsections: [
        {
          id: 'missing-recommendation',
          title: 'Raccomandazione',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:indexes.lib.missing.label.create}}',
                  meaning:
                    'Evidenza forte: gli statement che lo richiedono leggono molto più di quanto la tabella venga scritta, oppure le righe referenziate vengono cancellate su una tabella grande.',
                  action: 'Crealo fuori dalle ore di punta con il comando indicato.',
                },
                {
                  term: '{{ui:indexes.lib.missing.label.test}}',
                  meaning:
                    'Con buona probabilità aiuta, ma la stima è approssimativa (per esempio mancano le statistiche delle colonne).',
                  action:
                    'Confronta il piano prima e dopo (su una copia, oppure crealo e verifica che venga usato); se non serve, annulla.',
                },
                {
                  term: '{{ui:indexes.lib.missing.label.check}}',
                  meaning:
                    'Aiuta solo se il carico di lavoro fa ciò che dice la motivazione: righe padre cancellate, ricerche spaziali, oppure statement lenti con un tempo totale piccolo.',
                  action:
                    'Esegui prima il passo {{ui:indexes.lib.missing.step.title.check}}, poi decidi.',
                },
                {
                  term: '{{ui:indexes.lib.missing.label.skip}}',
                  meaning:
                    "Le scritture che l'indice rallenterebbe superano le letture che velocizzerebbe.",
                  action: 'Non crearlo.',
                },
              ],
            },
          ],
        },
        {
          id: 'missing-statements',
          title: 'Fonte: statement',
          blocks: [
            {
              kind: 'text',
              text: "pglens analizza gli statement di `pg_stat_statements` e trova le colonne su cui filtrano (=, <, >, BETWEEN, LIKE 'abc%') o ordinano e che non sono all'inizio di nessun indice. Nella chiave suggerita vengono prima le colonne di uguaglianza, poi quella di intervallo o di ordinamento.",
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:indexes.missing.readCost}}',
                  meaning:
                    'Millisecondi al secondo spesi oggi da quegli statement: il massimo che un indice può far risparmiare (un limite superiore).',
                },
                {
                  term: '{{ui:indexes.missing.weight}}',
                  meaning:
                    'Lo stesso tempo, come quota di tutto il tempo degli statement del database.',
                },
                {
                  term: '{{ui:indexes.missing.verdict}} e affidabilità',
                  meaning:
                    '{{ui:indexes.missing.verdict}} confronta le chiamate al secondo con le scritture di indice al secondo della tabella: 10 o più strong, 1 o più moderate, sotto 1 write-heavy. Una prima colonna con 20 valori distinti o meno lo abbassa di un livello. {{ui:indexes.missing.confidence}} è bassa quando mancano statistiche o dimensioni delle colonne.',
                },
                {
                  term: '{{ui:indexes.missing.analysisWindow}}',
                  meaning:
                    'Il periodo di statistiche degli statement usato: 1 ora, 6 ore, 24 ore (default) o 7 giorni.',
                },
                {
                  term: 'Riga di copertura',
                  meaning:
                    'Quanti statement sono stati analizzati e perché gli altri sono stati scartati (non SELECT, errore di parsing, tabelle di catalogo, tabelle piccole…). Uno statement non analizzabile può comunque comparire come full table scan.',
                },
              ],
            },
          ],
        },
        {
          id: 'missing-foreign-keys',
          title: 'Fonte: foreign key',
          blocks: [
            {
              kind: 'text',
              text: 'PostgreSQL non indicizza da solo le colonne referenzianti di una foreign key. Senza indice, ogni DELETE di una riga referenziata (padre), e ogni modifica della sua chiave, scansiona tutta la tabella figlia per cercare le righe che la referenziano, tenendo i lock nel frattempo. Su una tabella figlia grande questo rende lenti i delete e blocca le altre sessioni.',
            },
            {
              kind: 'text',
              text: '{{ui:indexes.missing.recommendation}}: {{ui:indexes.lib.missing.label.create}} quando le righe padre vengono cancellate adesso e la tabella figlia è di 10 MiB o più; {{ui:indexes.lib.missing.label.test}} quando vengono cancellate e la tabella è più piccola; {{ui:indexes.lib.missing.label.check}} quando non si vedono delete. L’indice velocizza anche le join su quelle colonne.',
            },
          ],
        },
        {
          id: 'missing-spatial',
          title: 'Fonte: colonne spaziali',
          blocks: [
            {
              kind: 'text',
              text: 'Una colonna PostGIS geometry o geography senza indice GiST. Ogni ricerca spaziale su di essa (`ST_Intersects`, `ST_DWithin`, `ST_Contains`, `&&`) legge tutta la tabella. L’indice è inutile se nessuno cerca spazialmente sulla colonna, e può essere grande, quindi la raccomandazione è {{ui:indexes.lib.missing.label.check}}: il passo {{ui:indexes.lib.missing.step.title.check}} elenca gli statement che usano la colonna.',
            },
          ],
        },
        {
          id: 'missing-scans',
          title: 'Fonte: full table scan',
          blocks: [
            {
              kind: 'text',
              text: 'Tabelle di 8 MiB o più lette da sequential scan a 100 righe al secondo o più, e non già coperte da un’altra fonte. Qualche statement le filtra su una colonna che nessun indice serve, ma pglens non è riuscito ad analizzarlo. La riga non ha un comando: i suoi passi mostrano come trovare gli statement e leggerne il piano per scegliere le colonne.',
            },
            {
              kind: 'note',
              tone: 'info',
              text: 'Un sequential scan è il piano giusto per una tabella piccola, o per una query che legge gran parte della tabella. Indicizza solo ciò che le query filtrano fino a poche righe.',
            },
          ],
        },
        {
          id: 'missing-act',
          title: 'Come agire su una riga',
          blocks: [
            {
              kind: 'steps',
              items: [
                "{{ui:indexes.lib.missing.step.title.check}}: esegui la query di controllo e conservane l'output.",
                '{{ui:indexes.lib.missing.step.title.create}}: esegui il comando CREATE INDEX CONCURRENTLY in psql, fuori dalle ore di punta, da solo (non dentro BEGIN … COMMIT). Non blocca letture né scritture ma dura di più, e richiede spazio libero su disco circa pari alla dimensione dell’indice.',
                '{{ui:indexes.lib.missing.step.title.verify}}: esegui la query di verifica. Il piano deve mostrare un Index Scan o Bitmap Index Scan sul nuovo indice; dopo un giorno l’indice deve avere delle scansioni.',
                "{{ui:indexes.lib.missing.step.title.undo}}: se il piano non cambia, o le scritture rallentano, elimina l'indice con il comando Undo.",
              ],
            },
            {
              kind: 'note',
              tone: 'warning',
              text: 'Se CREATE INDEX CONCURRENTLY fallisce (deadlock, violazione di unicità, annullamento) lascia un indice INVALID che viene comunque scritto a ogni modifica. Eliminalo con DROP INDEX CONCURRENTLY e riesegui il comando.',
            },
            {
              kind: 'sql',
              label: 'Seguire una creazione di indice in corso (PostgreSQL 12+)',
              code: 'SELECT pid, phase, blocks_done, blocks_total, tuples_done, tuples_total\nFROM pg_stat_progress_create_index;',
            },
            {
              kind: 'sql',
              label: 'Trovare indici invalid lasciati da una creazione fallita',
              code: 'SELECT indexrelid::regclass AS index, indrelid::regclass AS table\nFROM pg_index WHERE NOT indisvalid;',
            },
          ],
        },
        {
          id: 'missing-script',
          title: 'Copia dello script',
          blocks: [
            {
              kind: 'text',
              text: 'Il pulsante Copy accanto a {{ui:indexes.missing.script}}, nell’intestazione della tab, copia uno script psql di tutte le righe della lista (per il database scelto nella pagina), raggruppato per database con `\\connect`. Le righe {{ui:indexes.lib.missing.label.create}} e {{ui:indexes.lib.missing.label.test}} sono attive; le righe {{ui:indexes.lib.missing.label.check}} sono commentate, da abilitare una a una dopo il controllo; le righe {{ui:indexes.lib.missing.label.skip}} e {{ui:indexes.lib.missing.source.scans}} (senza comando) sono escluse.',
            },
          ],
        },
      ],
    },
    {
      id: 'cleanup',
      title: 'Tab Cleanup',
      blocks: [
        {
          kind: 'text',
          text: "Indici che sembrano eliminabili, con lo spazio che un drop restituisce e le scritture che risparmia. Le chiavi primarie non compaiono mai; un indice unique compare solo come duplicato di un altro indice unique o primary, che mantiene l'unicità della colonna.",
        },
      ],
      subsections: [
        {
          id: 'cleanup-reasons',
          title: 'Motivi',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'invalid',
                  meaning:
                    'Un CREATE INDEX CONCURRENTLY o REINDEX CONCURRENTLY è fallito e ha lasciato l’indice costruito a metà: le query non lo usano mai, ma ogni scrittura lo aggiorna comunque.',
                  action:
                    'Eliminalo e ricrealo (entrambi i comandi sono nel dettaglio della riga), oppure esegui REINDEX INDEX CONCURRENTLY.',
                },
                {
                  term: 'duplicate',
                  meaning: 'Stessa definizione di un indice che resta (indicato nella riga).',
                  action:
                    'Eliminalo: le query continuano a usare l’altro. Se DROP INDEX fallisce con "constraint requires it", elimina invece il constraint (ALTER TABLE … DROP CONSTRAINT).',
                },
                {
                  term: 'redundant prefix',
                  meaning:
                    'Le sue colonne sono le colonne iniziali di un indice più ampio, che può servire le stesse ricerche.',
                  action:
                    'Eliminalo, a meno che sia unique, molto più piccolo e molto usato. Verifica con EXPLAIN che venga scelto l’indice più ampio.',
                },
                {
                  term: 'unused',
                  meaning:
                    'L’advisor non ha visto nessuna scansione di questo indice, su questo server e sui suoi standby, per tutta la finestra di osservazione (7 giorni di default).',
                  action:
                    'Verifica che nessun job raro (report di fine mese, batch annuale) lo usi, poi eliminalo dalla tab Cleanup. Conserva il suo CREATE per poter tornare indietro.',
                },
                {
                  term: 'never scanned',
                  meaning:
                    'Nessuna scansione dall’ultimo reset delle statistiche. È il contatore grezzo di PostgreSQL, non un verdetto: il reset può essere recente, o l’indice troppo piccolo perché l’advisor lo giudichi.',
                  action:
                    'Non eliminarlo ancora: aspetta che l’advisor lo segnali come unused, oppure controlla quanto sono vecchi i contatori (pg_stat_database.stats_reset).',
                },
              ],
            },
          ],
        },
        {
          id: 'cleanup-figures',
          title: 'Valori e colonne',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:indexes.cleanup.spaceBack}}',
                  meaning:
                    "Dimensione totale dei candidati confermati dall'advisor (invalid, duplicate, redundant, unused). I candidati never scanned si basano solo sui contatori grezzi e sono contati a parte, nel loro valore.",
                },
                {
                  term: '{{ui:indexes.cleanup.indexWrites}}',
                  meaning:
                    "Voci al secondo che insert e update non-HOT aggiungono all'indice: ciò che un drop fa risparmiare a ogni scrittura.",
                },
                {
                  term: '{{ui:indexes.cleanup.basis}}',
                  meaning:
                    '{{ui:indexes.cleanup.basisAdvisor}}: un verdetto sulla finestra. {{ui:indexes.cleanup.basisStatistics}}: solo contatori PostgreSQL dal loro reset.',
                },
                {
                  term: 'Script',
                  meaning:
                    "Tutti i drop in uno script: quelli confermati dall'advisor attivi, quelli da contatori grezzi commentati, e in fondo i comandi per ricreare gli indici e tornare indietro.",
                },
              ],
            },
            {
              kind: 'sql',
              label: 'Quanto sono vecchi i contatori delle statistiche?',
              code: 'SELECT datname, stats_reset FROM pg_stat_database WHERE datname = current_database();',
            },
            {
              kind: 'sql',
              label: 'Eliminare senza bloccare le scritture (non dentro una transazione)',
              code: 'DROP INDEX CONCURRENTLY IF EXISTS schema_name.index_name;',
            },
          ],
        },
      ],
    },
    {
      id: 'dead-rows',
      title: 'Tab Dead rows',
      blocks: [
        {
          kind: 'text',
          text: 'Ogni UPDATE e DELETE lascia la vecchia versione della riga (una dead row) nella tabella finché il vacuum non la rimuove. Le dead row gonfiano la tabella e tutti i suoi indici e rallentano le scansioni. Questa tab ordina le tabelle per dead row per mostrare dove il vacuum è indietro.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:indexes.deadRows.deadRows}}',
              meaning:
                'Vecchie versioni di riga non ancora rimosse, con lo spazio stimato (dimensione heap × quota dead).',
            },
            {
              term: '{{ui:indexes.deadRows.deadLive}}',
              meaning:
                'Dead row divise per le righe vive. Sopra il 20% il vacuum è indietro; sopra il 100% la tabella contiene più spazzatura che dati.',
              action:
                'Esegui VACUUM (VERBOSE) sulla tabella e leggi cosa non è riuscito a rimuovere.',
            },
            {
              term: '{{ui:indexes.deadRows.vacuumDue}}',
              meaning:
                'Dead row divise per la soglia a cui parte l’autovacuum (50 + 20% delle righe di default). 1.0× o più senza un vacuum recente significa che l’autovacuum non sta al passo o è bloccato.',
              action:
                'Cerca transazioni lunghe o replication slot che trattengono la pulizia (vedi SQL sotto), poi abbassa lo scale factor della tabella.',
            },
            {
              term: '{{ui:indexes.deadRows.lastVacuum}}',
              meaning:
                "L'ultimo vacuum manuale o automatico; {{ui:indexes.deadRows.never}} se non ne risulta nessuno.",
            },
            {
              term: 'HOT',
              meaning:
                'Quota di update che non hanno scritto voci negli indici (heap-only tuple). Bassa su una tabella con molti update significa che le colonne aggiornate sono indicizzate o le pagine sono piene.',
              action:
                'Elimina gli indici su colonne aggiornate spesso, oppure imposta fillfactor = 90 sulla tabella.',
            },
            {
              term: '{{ui:indexes.deadRows.writes}}, {{ui:indexes.deadRows.trend}}',
              meaning:
                'Insert + update + delete al secondo, e l’andamento delle dead row nel periodo selezionato (solo le 10 tabelle peggiori).',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: "L'agent riporta per prime le tabelle con più dead row, fino a `checks.table_stats.top_n` per database (50 di default). Quando la lista è incompleta la tab lo dice: le tabelle peggiori ci sono sempre, la coda lunga no.",
        },
        {
          kind: 'sql',
          label: 'Cosa blocca il vacuum: le transazioni aperte più vecchie',
          code: 'SELECT pid, now() - xact_start AS age, state, left(query, 80) AS query\nFROM pg_stat_activity\nWHERE xact_start IS NOT NULL\nORDER BY xact_start LIMIT 10;',
        },
        {
          kind: 'sql',
          label: 'Far partire prima l’autovacuum su una tabella grande',
          code: 'ALTER TABLE schema_name.table_name SET (autovacuum_vacuum_scale_factor = 0.02);',
        },
      ],
    },
    {
      id: 'tax',
      title: 'Tab Index tax',
      blocks: [
        {
          kind: 'text',
          text: 'Quanto costano gli indici di ogni tabella: quanti sono, la loro dimensione rispetto alla tabella, lo spazio che sembra inutilizzato e le voci di indice che ogni scrittura aggiunge (write amplification). Ogni valore è una stima.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:indexes.tax.indexes}}',
              meaning:
                'Numero di indici sulla tabella. Più di 6–8 su una tabella con molte scritture meritano una revisione.',
            },
            {
              term: '{{ui:indexes.tax.indexSize}}, {{ui:indexes.tax.tableSize}}, {{ui:indexes.tax.ratio}}',
              meaning:
                "Dimensione complessiva degli indici, dimensione dell'heap (senza TOAST) e il loro rapporto. Sopra il 100% gli indici sono più grandi dei dati.",
              action:
                'Apri {{ui:indexes.page.tabCleanup}} sulla tabella: cerca duplicati, prefissi ridondanti e indici inutilizzati.',
            },
            {
              term: '{{ui:indexes.tax.unusedSize}}',
              meaning:
                'Spazio degli indici senza scansioni: verdetti dell’advisor e contatori grezzi, indicati separatamente. Gli indici unique e primary non sono mai contati.',
            },
            {
              term: '{{ui:indexes.tax.amplification}}, {{ui:indexes.tax.writes}}',
              meaning:
                'Insert, delete e update non-HOT al secondo, moltiplicati per il numero di indici: le voci di indice che le scritture aggiungono.',
              action:
                'Su una tabella con amplificazione alta e poche letture, ogni indice deve giustificare la sua presenza.',
            },
          ],
        },
      ],
    },
  ],
}
