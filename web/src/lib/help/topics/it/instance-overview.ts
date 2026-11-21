import type { HelpTopic } from '../../types'

export const instanceOverviewTopic: HelpTopic = {
  id: 'instance-overview',
  group: 'instance',
  title: 'Panoramica dell’istanza',
  summary:
    'La pagina {{ui:instance.overview.section}} mostra, per un server PostgreSQL, intestazione e stato, schede e grafici dell’intervallo selezionato, l’host su cui gira, impostazioni e durabilità, strutture interne, tabelle e indici, oggetti dello schema, replica logica e lacune di readiness. Ogni valore è spiegato con come leggerlo e cosa fare quando è preoccupante.',
  page: {
    path: '/instances/:instanceId',
    label: 'Apri {{ui:instance.overview.section}}',
  },
  sections: [
    {
      id: 'overview',
      title: 'A cosa serve la pagina',
      blocks: [
        {
          kind: 'text',
          text: 'La pagina Instance overview mostra un server PostgreSQL come lo riporta l’agent: intestazione e stato, schede e grafici dell’intervallo selezionato, l’host su cui gira, Settings and durability, Server internals, tabelle e indici, oggetti dello schema, replica logica e lacune di readiness. I valori arrivano dai report dell’agent salvati sul server. Le altre viste dello stesso server, {{ui:shell.nav.queryPerformance}}, {{ui:shell.nav.waitEvents}} e {{ui:shell.nav.locks}}, sono nel gruppo Instance della barra laterale.',
        },
      ],
      subsections: [
        {
          id: 'time-range',
          title: 'Intervallo di tempo',
          blocks: [
            {
              kind: 'text',
              text: 'Il selettore dell’intervallo imposta la finestra dei grafici e delle schede che coprono un intervallo. Il default è Last hour. I preset sono Last 15 minutes, Last hour, Last 2 hours, Last 4 hours, Last 6 hours, Last 8 hours, Last 24 hours, Last 3 days, Last 7 days, Last 14 days e Last 30 days. Custom time range imposta un intervallo assoluto nel fuso orario della console, e nessun intervallo può superare 30 giorni.',
            },
          ],
        },
        {
          id: 'database-scope',
          title: 'Ambito del database',
          blocks: [
            {
              kind: 'text',
              text: 'Il selettore Database imposta il database delle cifre per database. Quando sono disponibili i dati di attività, parte dal database monitorato più attivo. I grafici {{ui:instance.overviewCharts.group.workload.title}} e {{ui:instance.overviewCharts.group.memoryIo.title}} e la scheda {{ui:instance.overview.tile.transactions}} usano il database selezionato. I grafici {{ui:instance.overviewCharts.card.connections.title}} e {{ui:instance.overviewCharts.group.walCheckpoints.title}} riguardano l’intera istanza, e i grafici Queries e {{ui:instance.overviewCharts.group.tablesIndexes.title}} riguardano ogni database dell’istanza.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Database',
                  meaning: 'Il database di cui la pagina mostra le cifre per database.',
                },
                {
                  term: 'No monitored databases',
                  meaning:
                    'Lo stato vuoto mostrato al posto del selettore. Il suo testo dice: This instance has no database available for per-database metrics.',
                  action:
                    'Controlla il budget `databases.max` e il filtro `include` nella configurazione dell’agent.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'header',
      title: 'Intestazione dell’istanza',
      blocks: [
        {
          kind: 'text',
          text: 'L’intestazione inizia con l’etichetta Server, poi il titolo, il sottotitolo e il badge di freschezza. Il titolo è il nome visualizzato dell’istanza quando è impostato; in quel caso l’indirizzo passa nel sottotitolo. Il sottotitolo mostra il badge di stato, l’indirizzo quando è impostato un nome visualizzato, e un pulsante ID che copia l’id dell’istanza, mostrato nel suo tooltip. Sotto il titolo, cinque riquadri mostrano Role, PostgreSQL, {{ui:instance.header.label.tier}}, Last seen e {{ui:instance.header.label.cluster}}. Il pulsante {{ui:instance.header.slowest}} apre quella pagina filtrata su questa istanza.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'up',
              meaning: 'L’istanza è segnalata come up.',
            },
            {
              term: 'down',
              meaning:
                'L’istanza è segnalata come down. L’avviso sotto l’intestazione dice: {{ui:instance.header.downNotice}}',
              action:
                'Verifica che l’agent sia in esecuzione sull’host e raggiunga il server. I valori smettono di aggiornarsi finché l’agent non torna a riportare.',
            },
            {
              term: 'Role',
              meaning:
                'Il ruolo riportato dall’agent, come badge. Il valore unknown compare quando non viene riportato alcun ruolo.',
            },
            {
              term: 'standby (read-only)',
              meaning:
                'L’avviso mostrato quando il ruolo è standby. Il suo Replay lag mostra loading mentre il ritardo viene letto, unavailable quando non può essere letto, una durata quando è misurato, e Unknown con il motivo {{ui:instance.header.replayNotMeasured}}',
              action:
                'Quando il ritardo cresce, controlla la rete tra il primary e questo standby, e se il replay è in pausa, come descritto in [{{ui:instance.internals.walReplay}}](/help/instance-overview#wal-replay).',
            },
            {
              term: 'PostgreSQL',
              meaning: 'La versione di PostgreSQL del server, come la riporta l’agent.',
            },
            {
              term: '{{ui:instance.header.label.tier}}',
              meaning:
                'Il livello di permessi che l’agent ha su questa istanza. Annullare o terminare sessioni richiede il livello T2, concesso con `monitoring_user.sql -v tier1=1 -v tier2=1`, vedi [{{ui:shell.nav.locks}}](/help/locks).',
            },
            {
              term: 'Last seen',
              meaning:
                'L’ora dell’ultimo report ricevuto dal server dall’agent per questa istanza, nel fuso orario della console.',
            },
            {
              term: '{{ui:instance.header.label.cluster}}',
              meaning:
                'Il cluster a cui appartiene l’istanza, mostrato per nome quando è caricata la lista dei cluster, altrimenti per id. Il link apre la pagina di dettaglio del cluster.',
            },
            {
              term: '{{ui:instance.header.slowest}}',
              meaning:
                'Il pulsante nell’intestazione. Apre l’analisi delle query filtrata su questa istanza, descritta in [{{ui:instance.header.slowest}}](/help/slowest-queries).',
            },
          ],
        },
      ],
    },
    {
      id: 'views',
      title: 'Viste dell’istanza',
      blocks: [
        {
          kind: 'text',
          text: 'Il gruppo Instance della barra laterale passa tra quattro viste della stessa istanza; un database scelto in una vista resta l’ambito delle altre.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:instance.overview.section}}',
              meaning: 'Questa pagina, con le tab delle sezioni descritte sotto.',
            },
            {
              term: '{{ui:shell.nav.queryPerformance}}',
              meaning:
                'Apre l’analisi delle query di questa istanza, descritta in [{{ui:shell.nav.queryPerformance}}](/help/query-performance).',
            },
            {
              term: '{{ui:shell.nav.waitEvents}}',
              meaning:
                'Apre l’analisi dei wait event di questa istanza, descritta in [{{ui:shell.nav.waitEvents}}](/help/wait-events).',
            },
            {
              term: '{{ui:shell.nav.locks}}',
              meaning:
                'Apre le sessioni, i lock e le catene di blocco di questa istanza, descritte in [{{ui:shell.nav.locks}}](/help/locks).',
            },
          ],
        },
      ],
    },
    {
      id: 'section-tabs',
      title: 'Tab delle sezioni',
      blocks: [
        {
          kind: 'text',
          text: 'Sotto l’intestazione, le tab delle sezioni passano tra otto sezioni: {{ui:instance.section.overview}}, {{ui:instance.section.host}}, {{ui:instance.section.settings}}, {{ui:instance.relations.section}}, {{ui:instance.section.catalog}}, {{ui:instance.section.logical}}, {{ui:instance.section.internals}} e {{ui:instance.section.readiness}}.',
        },
      ],
    },
    {
      id: 'summary-tiles',
      title: 'Schede di riepilogo',
      blocks: [
        {
          kind: 'text',
          text: 'La tab {{ui:instance.section.overview}} si apre con una riga di schede di riepilogo. Le schede la cui etichetta termina con in range coprono l’intervallo di tempo selezionato.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:instance.overview.tile.connectionsUsed}}',
              meaning:
                'Connessioni in uso, rispetto al valore configurato `max_connections`. La regola dell’advisor `conn.saturation` avvisa sopra l’80 % delle connessioni che `max_connections` consente.',
              action:
                'Cerca le sessioni inattive in [{{ui:shell.nav.locks}}](/help/locks). Se ne sono molte, metti un pooler di connessioni davanti al server. Aumenta `max_connections` solo se la memoria del server lo consente.',
            },
            {
              term: '{{ui:instance.overview.tile.transactions}}',
              meaning:
                'Transazioni al secondo nel database selezionato, contando sia i commit sia i rollback. La regola dell’advisor `txn.high_rollback_ratio` segnala una quota alta di transazioni che finiscono in un rollback, con soglia al 10 %.',
              action:
                'Leggi gli errori nel log del server e nell’applicazione che interrompe le transazioni.',
            },
            {
              term: '{{ui:instance.overview.tile.cacheHitRatio}}',
              meaning:
                'Quota di letture di pagine trovate nei shared buffers, la cache di memoria di PostgreSQL, invece che lette da disco o dalla cache del sistema operativo. La regola dell’advisor `query.cache_miss_high` segnala una query con più di 1000 chiamate la cui quota di letture da shared block è sopra il 30 %.',
              action:
                'Quando il rapporto scende mentre le letture crescono, apri [{{ui:instance.header.slowest}}](/help/slowest-queries) per trovare le query che leggono di più, poi controlla i loro indici in [Index analysis](/help/indexes).',
            },
            {
              term: '{{ui:instance.overview.tile.walInRange}}',
              meaning:
                'Byte di WAL generati dall’intera istanza durante l’intervallo. Il WAL è il write-ahead log che PostgreSQL scrive prima di modificare i file dei dati.',
            },
            {
              term: '{{ui:instance.overview.tile.checkpointsInRange}}',
              meaning:
                'Checkpoint Timed e Requested durante l’intervallo. Il grafico {{ui:instance.overviewCharts.card.checkpoints.title}} nel gruppo {{ui:instance.overviewCharts.group.walCheckpoints.title}} li mostra nel tempo.',
              action:
                'Quando dominano i checkpoint Requested, il WAL si riempie più in fretta di quanto consenta `max_wal_size`. Controlla i grafici {{ui:instance.overviewCharts.group.walCheckpoints.title}} prima di aumentare `max_wal_size`.',
            },
            {
              term: '{{ui:instance.overview.tile.tempBytesInRange}}',
              meaning: 'Byte scritti nei file temporanei durante l’intervallo.',
              action:
                'Trova le query che scrivono su disco in [{{ui:instance.header.slowest}}](/help/slowest-queries), poi aumenta `work_mem` per loro nella loro sessione, oppure correggi i loro piani.',
            },
            {
              term: '{{ui:instance.overview.tile.tempFilesInRange}}',
              meaning: 'File temporanei creati durante l’intervallo.',
            },
            {
              term: '{{ui:instance.overview.tile.deadlocksInRange}}',
              meaning:
                'Deadlock durante l’intervallo. La regola dell’advisor `db.deadlock_rate` segnala che i deadlock si stanno verificando.',
              action:
                'Leggi i report dei deadlock nel log del server: ognuno indica le query e i lock coinvolti. Fai in modo che le transazioni acquisiscano i lock nello stesso ordine.',
            },
          ],
        },
      ],
    },
    {
      id: 'charts',
      title: 'Grafici',
      blocks: [
        {
          kind: 'text',
          text: 'Sotto le schede, i grafici sono in cinque gruppi: {{ui:instance.overviewCharts.group.workload.title}}, {{ui:instance.overviewCharts.group.memoryIo.title}}, {{ui:instance.overviewCharts.group.walCheckpoints.title}}, Queries e {{ui:instance.overviewCharts.group.tablesIndexes.title}}. Ogni grafico indica l’intervallo di campionamento con cui l’agent campiona i dati che lo alimentano.',
        },
      ],
      subsections: [
        {
          id: 'chart-workload',
          title: '{{ui:instance.overviewCharts.group.workload.title}}',
          blocks: [
            {
              kind: 'text',
              text: 'Il gruppo {{ui:instance.overviewCharts.group.workload.title}} mostra transazioni, connessioni, deadlock e recovery conflicts. Le cifre di transazioni e deadlock riguardano il database selezionato; le connessioni riguardano l’intera istanza.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.overviewCharts.card.transactions.title}}',
                  meaning:
                    'Transazioni al secondo nel database selezionato, contando sia i commit sia i rollback. Campionato ogni 30 s. Una quota alta di rollback è ciò che segnala la regola dell’advisor `txn.high_rollback_ratio`, con soglia al 10 %.',
                  action:
                    'Leggi gli errori nel log del server e nell’applicazione che interrompe le transazioni.',
                },
                {
                  term: 'Connections',
                  meaning:
                    'Connessioni dell’intera istanza, con la linea {{ui:instance.overviewCharts.line.configuredMaximum}}. Campionato ogni 10 s. La regola dell’advisor `conn.saturation` avvisa sopra l’80 % delle connessioni che `max_connections` consente.',
                  action:
                    'Segui l’azione della scheda [{{ui:instance.overview.tile.connectionsUsed}}](/help/instance-overview#summary-tiles).',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.deadlocksConflicts.title}}',
                  meaning:
                    '{{ui:instance.overviewCharts.line.deadlocks}} e {{ui:instance.internals.recoveryConflicts}} del database selezionato. Campionato ogni 30 s. La regola dell’advisor `db.deadlock_rate` segnala che i deadlock si stanno verificando.',
                  action:
                    'Per i deadlock, leggi i report nel log del server. Per i {{ui:instance.internals.recoveryConflicts}}, leggi [{{ui:instance.internals.recoveryConflicts}}](/help/instance-overview#recovery-conflicts) in {{ui:instance.section.internals}}.',
                },
              ],
            },
          ],
        },
        {
          id: 'chart-memory-io',
          title: '{{ui:instance.overviewCharts.group.memoryIo.title}}',
          blocks: [
            {
              kind: 'text',
              text: 'Il gruppo {{ui:instance.overviewCharts.group.memoryIo.title}} mostra quanto spesso le letture vengono servite dai shared buffers, e i file temporanei scritti dalle query, per il database selezionato.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.overview.tile.cacheHitRatio}}',
                  meaning:
                    'Quota di letture di pagine trovate nei shared buffers. Campionato ogni 30 s. Una lettura che manca i shared buffers può comunque essere servita dalla cache del sistema operativo, quindi un rapporto più basso non è sempre I/O su disco.',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.blocks.title}}',
                  meaning:
                    'Blocchi letti da fuori dei shared buffers, e blocchi trovati nei shared buffers, per il database selezionato. Campionato ogni 30 s.',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.temp.title}}',
                  meaning:
                    'File temporanei e byte temporanei scritti dalle query del database selezionato. Campionato ogni 30 s.',
                  action:
                    'Trova le query che scrivono file temporanei in [{{ui:instance.header.slowest}}](/help/slowest-queries), poi aumenta `work_mem` per loro, oppure correggi i loro piani.',
                },
              ],
            },
          ],
        },
        {
          id: 'chart-wal-checkpoints',
          title: 'WAL & checkpoint',
          blocks: [
            {
              kind: 'text',
              text: 'Il gruppo WAL & checkpoints mostra il write-ahead log generato dall’intera istanza, e i checkpoint che lo scaricano su disco.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.overviewCharts.card.wal.title}}',
                  meaning:
                    'Byte di WAL, record di WAL e {{ui:instance.overviewCharts.line.fullPageImages}} scritti dall’intera istanza. Campionato ogni 15 s. Un’immagine di pagina intera è una pagina scritta per intero nel WAL la prima volta che cambia dopo un checkpoint.',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.checkpoints.title}}',
                  meaning: 'Checkpoint Timed e Requested. Campionato ogni 30 s.',
                  action:
                    'Quando dominano i checkpoint Requested, il WAL si riempie più in fretta di quanto consenta `max_wal_size`. Controlla i grafici {{ui:instance.overviewCharts.group.walCheckpoints.title}} prima di aumentare `max_wal_size`.',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.checkpointTime.title}}',
                  meaning:
                    'Tempo speso a scrivere e a sincronizzare i file durante i checkpoint, dalle serie Write e Sync. Campionato ogni 30 s.',
                  action:
                    'Quando questi tempi crescono insieme ai checkpoint Requested, verifica la latenza dello storage con gli strumenti del sistema operativo.',
                },
              ],
            },
          ],
        },
        {
          id: 'chart-queries',
          title: 'Queries',
          blocks: [
            {
              kind: 'text',
              text: 'Il gruppo Queries mostra le query tracciate da `pg_stat_statements` in ogni database dell’istanza. Campionato ogni 60 s.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.overviewCharts.card.calls.title}}',
                  meaning: 'Esecuzioni di query al secondo, sulle query tracciate.',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.execTime.title}}',
                  meaning: 'Tempo di esecuzione delle query tracciate al secondo.',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.rows.title}}',
                  meaning: 'Righe restituite o modificate al secondo dalle query tracciate.',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.statementReads.title}}',
                  meaning:
                    'Blocchi condivisi letti al secondo dalle query tracciate, cioè blocchi non trovati nei shared buffers.',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.statementTemp.title}}',
                  meaning: 'Blocchi temporanei scritti al secondo dalle query tracciate.',
                  action:
                    'Per trovare la query dietro una linea, apri [{{ui:instance.header.slowest}}](/help/slowest-queries) dal pulsante nell’intestazione.',
                },
              ],
            },
          ],
        },
        {
          id: 'chart-tables-indexes',
          title: '{{ui:instance.overviewCharts.group.tablesIndexes.title}}',
          blocks: [
            {
              kind: 'text',
              text: 'Il gruppo {{ui:instance.overviewCharts.group.tablesIndexes.title}} riguarda le tabelle e gli indici tracciati dall’agent in ogni database dell’istanza. Questi grafici sono campionati ogni 5 minuti.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.overviewCharts.card.scans.title}}',
                  meaning:
                    'Scansioni sequenziali e {{ui:instance.overviewCharts.card.indexScans.title}} sulle tabelle tracciate.',
                  action:
                    'Quando le scansioni sequenziali crescono su una tabella grande, controlla i suoi indici in [Index analysis](/help/indexes) e le query che la scansionano in [{{ui:instance.header.slowest}}](/help/slowest-queries).',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.tupleChurn.title}}',
                  meaning:
                    'Righe {{ui:instance.overviewCharts.line.inserted}}, Updated, {{ui:instance.overviewCharts.line.hotUpdated}} e {{ui:instance.overviewCharts.line.deleted}} sulle tabelle tracciate. Un aggiornamento HOT mantiene la nuova versione della riga nella stessa pagina, quindi gli indici non richiedono una nuova voce per essa.',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.deadTuples.title}}',
                  meaning:
                    'Righe morte delle tabelle tracciate, cioè le vecchie versioni di riga che il vacuum non ha ancora rimosso.',
                  action:
                    'Una linea che continua a salire significa che il vacuum non sta al passo. Controlla la {{ui:instance.relations.col.deadRatio}} delle tabelle in [{{ui:instance.relations.section}}](/help/instance-overview#relations).',
                },
                {
                  term: 'Index size',
                  meaning: 'Dimensione degli indici tracciati, sommata su ogni database.',
                  action:
                    'La regola dell’advisor `index.unused` segnala un indice che non ha avuto scansioni nella storia osservata. Verificalo in [Index analysis](/help/indexes) prima di eliminarlo.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'host',
      title: 'Metriche dell’host',
      blocks: [
        {
          kind: 'text',
          text: 'La tab {{ui:instance.section.host}} legge la macchina su cui gira l’agent: CPU, memoria, swap, load average e il filesystem che contiene la directory dei dati di PostgreSQL. Il pannello {{ui:instance.host.section}} mostra le schede dell’host e i grafici dell’host.',
        },
      ],
      subsections: [
        {
          id: 'host-tiles',
          title: 'Schede dell’host',
          blocks: [
            {
              kind: 'text',
              text: 'Le schede dell’host mostrano l’ultima lettura di ogni valore. Le schede {{ui:instance.host.chart.filesystemTitle}} richiedono che la directory dei dati di PostgreSQL sia montata nel container dell’agent, come descrive [{{ui:instance.host.chart.group}}](/help/instance-overview#host-charts).',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.host.metric.cpuUsed}}',
                  meaning:
                    'Quota di CPU in uso. L’etichetta indica la fonte: {{ui:instance.host.cpuHost}} per l’intero host, oppure {{ui:instance.host.cpuCgroup}} quando l’agent legge un limite di CPU del container.',
                  action:
                    'Quando la CPU resta alta, controlla le query che usano più tempo in [{{ui:instance.header.slowest}}](/help/slowest-queries). Se il carico viene da altri processi, guardali direttamente sulla macchina.',
                },
                {
                  term: '{{ui:instance.host.metric.cpuCount}}',
                  meaning: 'Il numero di CPU.',
                },
                {
                  term: '{{ui:instance.host.metric.memTotal}}',
                  meaning: 'Memoria totale dell’host.',
                },
                {
                  term: '{{ui:instance.host.metric.memAvailable}}',
                  meaning: 'Memoria che il sistema operativo riporta come disponibile.',
                },
                {
                  term: '{{ui:instance.host.metric.swapTotal}}',
                  meaning: '{{ui:instance.host.chart.swapTitle}} configurato sull’host.',
                },
                {
                  term: '{{ui:instance.host.metric.swapUsed}}',
                  meaning: '{{ui:instance.host.chart.swapTitle}} in uso sull’host.',
                  action:
                    '{{ui:instance.host.chart.swapTitle}} in uso insieme a una {{ui:instance.host.metric.memAvailable}} bassa significa che alla macchina manca memoria. Trova i processi che la usano; se la causa è PostgreSQL, riduci le connessioni o `shared_buffers`.',
                },
                {
                  term: '{{ui:instance.host.metric.load1}}',
                  meaning: 'Il load average sull’ultimo minuto.',
                },
                {
                  term: '{{ui:instance.host.metric.load5}}',
                  meaning: 'Il load average sugli ultimi 5 minuti.',
                },
                {
                  term: '{{ui:instance.host.metric.load15}}',
                  meaning: 'Il load average sugli ultimi 15 minuti.',
                },
                {
                  term: '{{ui:instance.host.metric.diskTotal}}',
                  meaning:
                    'Dimensione totale del filesystem che contiene la directory dei dati di PostgreSQL.',
                },
                {
                  term: '{{ui:instance.host.metric.diskFree}}',
                  meaning:
                    'Spazio libero sul filesystem che contiene la directory dei dati di PostgreSQL.',
                },
                {
                  term: '{{ui:instance.host.metric.diskFreeRatio}}',
                  meaning:
                    'Spazio libero in quota del filesystem. La regola dell’advisor `disk.free_low` avvisa quando è libero meno del 15 % del disco dei dati.',
                  action:
                    'Controlla la [{{ui:instance.internals.walDirectory}}](/help/instance-overview#wal-directory) e le [{{ui:instance.internals.databaseSizes}}](/help/instance-overview#database-sizes), poi le tabelle più grandi. Libera spazio o aggiungi capacità prima che il disco si riempia.',
                },
              ],
            },
          ],
        },
        {
          id: 'host-charts',
          title: 'Grafici dell’host',
          blocks: [
            {
              kind: 'text',
              text: 'I grafici dell’host mostrano le letture dell’host nell’intervallo selezionato. I grafici {{ui:instance.host.chart.swapTitle}} e {{ui:instance.host.chart.filesystemTitle}} vengono sostituiti da una nota quando manca il loro dato.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.host.chart.cpuUsedTitle}}',
                  meaning: 'CPU in uso nell’intervallo, in percentuale.',
                },
                {
                  term: '{{ui:instance.host.chart.loadTitle}}',
                  meaning: 'Il load average su 1, 5 e 15 minuti.',
                },
                {
                  term: 'Memory',
                  meaning: 'Memoria dell’host nell’intervallo.',
                },
                {
                  term: '{{ui:instance.host.chart.swapTitle}}',
                  meaning:
                    '{{ui:instance.host.chart.swapTitle}} dell’host nell’intervallo. Quando non è configurato alcuno swap, il grafico è sostituito dalla nota No swap is configured.',
                },
                {
                  term: '{{ui:instance.host.chart.filesystemTitle}}',
                  meaning:
                    'Spazio libero del filesystem che contiene la directory dei dati. Quando il disco non viene misurato, il grafico è sostituito dalla nota {{ui:instance.host.chart.filesystemTitle}} metrics need the PostgreSQL data directory mounted read-only at the same path in the agent container.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'settings',
      title: '{{ui:instance.section.settings}}',
      blocks: [
        {
          kind: 'text',
          text: 'La tab {{ui:instance.section.settings}} mostra il pannello {{ui:instance.settings.section}}: prima il {{ui:instance.settings.durabilitySummary}}, poi la tabella {{ui:instance.settings.observed}}.',
        },
      ],
      subsections: [
        {
          id: 'durability',
          title: '{{ui:instance.settings.durabilitySummary}}',
          blocks: [
            {
              kind: 'text',
              text: 'Il {{ui:instance.settings.durabilitySummary}} elenca cinque impostazioni che decidono la sicurezza in caso di crash, con i loro valori. Un valore che indebolisce la sicurezza in caso di crash riceve una gravità: critical per `fsync` o `full_page_writes` impostati su `off`; warning per `archive_mode` impostato su `off`, `wal_level` impostato su `minimal` o `synchronous_commit` impostato su `off`.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'fsync',
                  meaning:
                    'Off: PostgreSQL non forza le proprie scritture su disco, quindi un crash o una perdita di alimentazione può corrompere i dati. Gravità critical quando è off.',
                  action:
                    'Imposta `fsync` a `on` con `ALTER SYSTEM SET fsync = on;` e poi `SELECT pg_reload_conf();`, oppure nel file di configurazione.',
                },
                {
                  term: 'full_page_writes',
                  meaning:
                    'Off: le pagine modificate non vengono scritte intere nel WAL dopo un checkpoint, quindi un crash può lasciare una pagina che il recovery non riesce a riparare. Gravità critical quando è off.',
                  action: 'Imposta `full_page_writes` a `on`, nello stesso modo di `fsync`.',
                },
                {
                  term: 'archive_mode',
                  meaning:
                    'Off: i file WAL completati non vengono archiviati, quindi un backup non può essere ripristinato a un punto nel tempo. Gravità warning quando è off.',
                  action:
                    'Imposta `archive_mode` a `on` con un `archive_command` funzionante. L’impostazione ha effetto dopo un riavvio del server.',
                },
                {
                  term: 'wal_level',
                  meaning:
                    'Minimal: il WAL conserva solo ciò che serve al recovery dopo un crash, quindi non c’è replica e non c’è recovery a un punto nel tempo. Gravità warning quando è minimal.',
                  action:
                    'Imposta `wal_level` a `replica`, o a `logical` se usi la replica logica. L’impostazione ha effetto dopo un riavvio del server.',
                },
                {
                  term: 'synchronous_commit',
                  meaning:
                    'Off: un commit viene confermato prima che il suo WAL sia scaricato su disco, quindi un crash può perdere gli ultimi commit. Gravità warning quando è off.',
                  action:
                    'Riporta `synchronous_commit` a `on` dove perdere commit non è accettabile. Può anche essere cambiato per sessione o per transazione.',
                },
              ],
            },
            {
              kind: 'sql',
              label: 'Leggi le impostazioni di durabilità sul server',
              code: "SELECT name, setting, source, context, pending_restart FROM pg_settings WHERE name IN ('fsync', 'full_page_writes', 'archive_mode', 'wal_level', 'synchronous_commit');",
            },
          ],
        },
        {
          id: 'settings-table',
          title: '{{ui:instance.settings.observed}}',
          blocks: [
            {
              kind: 'text',
              text: 'La tabella {{ui:instance.settings.observed}} elenca le impostazioni il cui valore è cambiato dalla data nel controllo {{ui:instance.settings.changedSince}}. Il campo {{ui:instance.settings.search}} e i filtri Source e State cambiano solo ciò che la tabella mostra. Una cella riporta {{ui:instance.settings.unitNotReported}}, Source was not reported. o {{ui:instance.settings.contextNotReported}} quando l’agent non ha inviato alcun valore.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Name',
                  meaning: 'Il nome dell’impostazione.',
                },
                {
                  term: 'Value',
                  meaning: 'Il valore dell’impostazione.',
                },
                {
                  term: 'Unit',
                  meaning: 'L’unità del valore.',
                },
                {
                  term: 'Source',
                  meaning:
                    'Dove è stato impostato il valore. Il filtro Source restringe la tabella a una sorgente.',
                },
                {
                  term: 'Context',
                  meaning:
                    'Quando una modifica ha effetto: alcune impostazioni richiedono un riavvio del server, alcune un ricaricamento della configurazione, altre valgono per le nuove sessioni.',
                },
                {
                  term: '{{ui:instance.settings.col.changedAt}}',
                  meaning: 'L’ora in cui il valore è cambiato per ultimo.',
                },
                {
                  term: '{{ui:instance.settings.state.pending}}',
                  meaning:
                    'Un valore di State: il valore è impostato ma richiede un riavvio del server prima di avere effetto.',
                  action:
                    'Riavvia il server in una finestra di manutenzione per rendere in vigore un valore in attesa.',
                },
                {
                  term: '{{ui:instance.settings.state.inForce}}',
                  meaning: 'Un valore di State: il valore è in vigore.',
                },
                {
                  term: '{{ui:instance.settings.changedSince}}',
                  meaning:
                    'Un controllo di data sopra la tabella. Il pulsante {{ui:instance.settings.useRange}} lo riempie con l’intervallo di tempo selezionato. La tabella elenca le impostazioni cambiate da questa data.',
                },
                {
                  term: 'No setting changed since the date in {{ui:instance.settings.changedSince}}.',
                  meaning:
                    'Lo stato vuoto mostrato quando nessuna impostazione è cambiata dalla data nel controllo {{ui:instance.settings.changedSince}}.',
                  action:
                    'Scegli una data precedente in {{ui:instance.settings.changedSince}}, oppure usa {{ui:instance.settings.useRange}}.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'internals',
      title: '{{ui:instance.section.internals}}',
      blocks: [
        {
          kind: 'text',
          text: 'La tab {{ui:instance.section.internals}} mostra il pannello {{ui:instance.internals.section}}: {{ui:instance.internals.walDirectory}}, {{ui:instance.internals.walReplay}}, {{ui:instance.internals.databaseSizes}}, {{ui:instance.internals.slru}}, {{ui:instance.internals.tempFiles}} e {{ui:instance.internals.recoveryConflicts}}.',
        },
      ],
      subsections: [
        {
          id: 'wal-directory',
          title: '{{ui:instance.internals.walDirectory}}',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.internals.walDirectory}}',
                  meaning:
                    'Dimensione totale e numero di file della directory `pg_wal`, letti con `pg_ls_waldir()`.',
                  action:
                    'La regola dell’advisor `wal.directory_large` segnala un `pg_wal` più grande del doppio di `max_wal_size` più `wal_keep_size`. Controlla gli slot di replica, l’archiviazione con `archive_mode` e `archive_command`, e `wal_keep_size`.',
                },
                {
                  term: 'pg_wal is not listed',
                  meaning:
                    'La nota mostrata al posto del valore quando l’agent non riesce a elencare `pg_wal`: pg_wal is not listed: the monitoring role needs EXECUTE on pg_ls_waldir(), which pg_monitor grants.',
                },
              ],
            },
            {
              kind: 'sql',
              label: 'Conta e misura la directory WAL',
              code: 'SELECT count(*) AS files, pg_size_pretty(sum(size)) AS total FROM pg_ls_waldir();',
            },
            {
              kind: 'sql',
              label: 'Slot di replica',
              code: 'SELECT slot_name, slot_type, active, restart_lsn FROM pg_replication_slots;',
            },
          ],
        },
        {
          id: 'wal-replay',
          title: 'WAL replay',
          blocks: [
            {
              kind: 'text',
              text: 'La vista {{ui:instance.internals.walReplay}} mostra lo stato del replay di uno standby, con i badge paused e running.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'paused',
                  meaning:
                    'Il badge paused indica che il replay del WAL è in pausa su questo standby: il WAL ricevuto non viene applicato finché il replay non riprende.',
                  action:
                    'Riprendi il replay con `SELECT pg_wal_replay_resume();` sullo standby, dopo aver capito perché è stato messo in pausa.',
                },
              ],
            },
            {
              kind: 'sql',
              label: 'Verifica lo stato di recovery e di replay su uno standby',
              code: 'SELECT pg_is_in_recovery(), pg_is_wal_replay_paused();',
            },
          ],
        },
        {
          id: 'database-sizes',
          title: '{{ui:instance.internals.databaseSizes}}',
          blocks: [
            {
              kind: 'text',
              text: 'La vista {{ui:instance.internals.databaseSizes}} elenca la dimensione di ogni database, con la sua variazione nelle ultime 24 ore e negli ultimi 7 giorni.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Size',
                  meaning: 'Dimensione su disco del database.',
                },
                {
                  term: 'Last 24 hours',
                  meaning: 'Variazione di dimensione nelle ultime 24 ore.',
                },
                {
                  term: 'Last 7 days',
                  meaning: 'Variazione di dimensione negli ultimi 7 giorni.',
                  action:
                    'Per un database che cresce in fretta, trova le sue tabelle più grandi in [{{ui:instance.relations.section}}](/help/instance-overview#relations) e controlla le loro righe morte.',
                },
              ],
            },
            {
              kind: 'sql',
              label: 'Dimensioni dei database, dal più grande',
              code: 'SELECT datname, pg_size_pretty(pg_database_size(datname)) AS size FROM pg_database ORDER BY pg_database_size(datname) DESC;',
            },
          ],
        },
        {
          id: 'slru-caches',
          title: '{{ui:instance.internals.slru}}',
          blocks: [
            {
              kind: 'text',
              text: 'La vista {{ui:instance.internals.slru}} elenca le piccole cache che PostgreSQL usa per lo stato delle transazioni, le subtransazioni e i multixact. Una pagina che manca in una di queste cache viene letta da disco, quindi una cache che manca spesso rallenta le sessioni che ne hanno bisogno.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Cache',
                  meaning: 'Il nome della cache SLRU, come lo riporta PostgreSQL.',
                },
                {
                  term: '{{ui:instance.internals.col.hitRatio}}',
                  meaning:
                    'Accessi trovati diviso accessi trovati più letture, in percentuale degli accessi alla cache.',
                  action:
                    'Un rapporto basso sulla cache subtransaction o multixact: trova le sessioni che usano savepoint o molti lock di riga in [{{ui:shell.nav.locks}}](/help/locks), e riducili.',
                },
                {
                  term: '{{ui:instance.internals.col.readFromDisk}}',
                  meaning: 'Pagine lette da disco, al secondo.',
                },
                {
                  term: 'Written',
                  meaning: 'Pagine scritte, al secondo.',
                },
                {
                  term: 'Zeroed',
                  meaning: 'Pagine azzerate, al secondo.',
                },
                {
                  term: 'Flushes',
                  meaning: 'Flush, al secondo.',
                },
                {
                  term: 'Truncations',
                  meaning: 'Troncamenti, al secondo.',
                },
              ],
            },
          ],
        },
        {
          id: 'temporary-files',
          title: '{{ui:instance.internals.tempFiles}}',
          blocks: [
            {
              kind: 'text',
              text: 'La vista {{ui:instance.internals.tempFiles}} elenca i database le cui query hanno scritto file temporanei, con i file creati e i byte scritti. Una query scrive file temporanei quando un suo sort o hash non entra in `work_mem`.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Database',
                  meaning: 'Il database a cui si riferisce la riga.',
                },
                {
                  term: 'Files',
                  meaning: 'File temporanei creati in questo database.',
                },
                {
                  term: 'Written',
                  meaning: 'Byte scritti nei file temporanei di questo database.',
                  action:
                    'Trova le query che scrivono su disco in [{{ui:instance.header.slowest}}](/help/slowest-queries). Imposta un `work_mem` più alto per loro nella loro sessione, oppure correggi i loro piani.',
                },
              ],
            },
            {
              kind: 'sql',
              label: 'File temporanei dall’ultimo azzeramento delle statistiche',
              code: 'SELECT datname, temp_files, pg_size_pretty(temp_bytes) AS temp_size FROM pg_stat_database ORDER BY temp_bytes DESC;',
            },
          ],
        },
        {
          id: 'recovery-conflicts',
          title: '{{ui:instance.internals.recoveryConflicts}}',
          blocks: [
            {
              kind: 'text',
              text: 'La vista {{ui:instance.internals.recoveryConflicts}} elenca le query che il replay del WAL ha annullato su uno standby, per database e motivo. Una lista vuota significa che il replay non ha annullato alcuna query nel periodo mostrato.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Database',
                  meaning: 'Il database in cui la query è stata annullata.',
                },
                {
                  term: 'Reason',
                  meaning: 'Il motivo del conflitto, come lo riporta l’agent.',
                },
                {
                  term: '{{ui:instance.internals.col.queriesCancelled}}',
                  meaning: 'Query annullate per quel motivo.',
                  action:
                    'Quando le annullazioni sono frequenti, valuta `hot_standby_feedback` (può gonfiare il primary) o un `max_standby_streaming_delay` più alto, e controlla le query lunghe sullo standby.',
                },
              ],
            },
            {
              kind: 'sql',
              label: 'Conflitti di recovery per database',
              code: 'SELECT datname, confl_snapshot, confl_lock, confl_bufferpin, confl_deadlock FROM pg_stat_database_conflicts;',
            },
          ],
        },
      ],
    },
    {
      id: 'other-tabs',
      title: 'Other tabs',
      blocks: [
        {
          kind: 'text',
          text: 'Le tab di sezione {{ui:instance.relations.section}}, {{ui:instance.section.catalog}}, {{ui:instance.section.logical}} e {{ui:instance.section.readiness}} elencano gli oggetti dell’istanza e ciò che richiede attenzione.',
        },
      ],
    },
    {
      id: 'relations',
      title: '{{ui:instance.relations.section}}',
      blocks: [
        {
          kind: 'text',
          text: 'La tab {{ui:instance.relations.section}} elenca le tabelle, gli indici e le stime di bloat di ogni database monitorato, come li ha riportati per ultimi l’agent. Le sue viste sono Tables, Indexes, {{ui:instance.relations.view.tableBloat}} e {{ui:instance.relations.view.indexBloat}}. Tabelle e indici vengono dagli ultimi 15 minuti, le stime di bloat dalle ultime 13 ore. Ogni elenco è limitato dal top_n del suo controllo, come `checks.table_stats.top_n`, `checks.index_stats.top_n` o `checks.bloat_estimate.top_n`.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Relation',
              meaning: 'Lo schema e il nome della tabella.',
            },
            {
              term: '{{ui:instance.relations.col.liveTuples}}',
              meaning: 'Righe vive della tabella.',
            },
            {
              term: '{{ui:instance.overviewCharts.card.deadTuples.title}}',
              meaning:
                'Righe morte della tabella, cioè le vecchie versioni di riga che il vacuum non ha ancora rimosso.',
            },
            {
              term: '{{ui:instance.relations.col.deadRatio}}',
              meaning:
                'Il rapporto tra righe morte e righe della tabella, come lo riporta l’agent.',
              action:
                'Controlla gli ultimi orari di vacuum e analyze, e se l’autovacuum sta al passo. Esegui `VACUUM (ANALYZE)` sulla tabella quando le sue righe morte continuano a crescere.',
            },
            {
              term: 'Size',
              meaning: 'Dimensione della tabella.',
            },
            {
              term: 'Sequential scans',
              meaning: 'Scansioni sequenziali della tabella.',
            },
            {
              term: 'Index scans',
              meaning: 'Scansioni su indice della tabella.',
            },
            {
              term: '{{ui:instance.relations.col.lastVacuum}}',
              meaning:
                'Quando la tabella è stata sottoposta a vacuum per l’ultima volta. Il testo Never vacuumed since the statistics were reset. compare quando non è stata sottoposta a vacuum dall’ultimo azzeramento delle statistiche.',
            },
            {
              term: '{{ui:instance.relations.col.lastAnalyze}}',
              meaning:
                'Quando la tabella è stata analizzata per l’ultima volta. Il testo Never analysed since the statistics were reset. compare quando non è stata analizzata dall’ultimo azzeramento delle statistiche.',
            },
            {
              term: 'Index',
              meaning: 'Il nome dell’indice, nella vista Indexes.',
            },
            {
              term: 'Last scan',
              meaning:
                'Quando l’indice è stato scansionato per l’ultima volta. La cella riporta never since reset quando l’indice non ha avuto scansioni dall’azzeramento delle statistiche, e not available su PostgreSQL 15, che non lo riporta.',
              action:
                'Un indice senza scansioni dall’azzeramento delle statistiche è un candidato all’eliminazione. Verificalo prima in [Index analysis](/help/indexes).',
            },
            {
              term: '{{ui:instance.relations.col.realSize}}',
              meaning: 'La dimensione reale della tabella o dell’indice.',
            },
            {
              term: '{{ui:instance.relations.col.expectedSize}}',
              meaning:
                'La dimensione che tabella o indice dovrebbero avere senza bloat, come stimata.',
            },
            {
              term: 'Bloat',
              meaning:
                'Spazio sprecato stimato in una tabella o in un indice B-tree. È una stima, non una misura esatta.',
            },
            {
              term: '{{ui:instance.relations.col.bloatRatio}}',
              meaning: 'Il bloat in quota della dimensione reale.',
              action:
                'Usa la stima per scegliere un candidato alla ricostruzione, poi misura con {{ui:instance.relations.exact.button}} prima di ricostruire.',
            },
            {
              term: 'Method',
              meaning: 'Come è stata calcolata la stima. pgstattuple misura solo le tabelle.',
            },
            {
              term: '{{ui:instance.relations.col.exactMeasurement}}',
              meaning:
                'Esegue pgstattuple sull’agent in una transazione di sola lettura. Legge l’intera tabella, limitata a 50 secondi, e richiede che l’estensione sia già installata nel database che contiene la tabella: pglens non installa mai estensioni.',
            },
          ],
        },
        {
          kind: 'sql',
          label: 'Tabelle con più righe morte',
          code: 'SELECT relname, n_live_tup, n_dead_tup, last_autovacuum, last_autoanalyze FROM pg_stat_user_tables ORDER BY n_dead_tup DESC LIMIT 20;',
        },
      ],
    },
    {
      id: 'catalog',
      title: '{{ui:instance.section.catalog}}',
      blocks: [
        {
          kind: 'text',
          text: 'La tab {{ui:instance.section.catalog}} controlla gli oggetti di ogni database monitorato. Le sue tab sono {{ui:instance.catalog.tabs.triggers}}, Sequences, {{ui:instance.catalog.tabs.partitions}}, Functions, {{ui:instance.catalog.tabs.securityDefiner}}, Unindexed foreign keys, {{ui:instance.catalog.tabs.extensions}} e {{ui:instance.catalog.tabs.postgis}}.',
        },
      ],
      subsections: [
        {
          id: 'triggers',
          title: '{{ui:instance.catalog.tabs.triggers}}',
          blocks: [
            {
              kind: 'text',
              text: 'La tab {{ui:instance.catalog.tabs.triggers}} elenca i trigger di ogni database monitorato, con Trigger, Table, State, {{ui:instance.catalog.triggers.enforces}}, Fires, Function e {{ui:instance.catalog.triggers.stateSince}}. Il filtro {{ui:instance.catalog.triggers.disabledOnly}} mostra solo i trigger disabilitati. Lo stato vuoto No triggers match these filters compare quando i filtri nascondono ogni trigger, e suggerisce di provare un altro database o di mostrare anche i trigger abilitati.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'State',
                  meaning: 'Se il trigger è abilitato o disabilitato.',
                  action:
                    'Se il trigger deve essere eseguito, abilitalo con `ALTER TABLE table_name ENABLE TRIGGER trigger_name;`. La regola dell’advisor `trigger.user_disabled` segnala un trigger utente disabilitato.',
                },
              ],
            },
          ],
        },
        {
          id: 'sequences',
          title: 'Sequences',
          blocks: [
            {
              kind: 'text',
              text: 'La tab Sequences confronta l’ultimo valore di ogni sequenza con il tipo intero della colonna che alimenta, nelle colonne Sequence, Feeds, {{ui:instance.catalog.seq.lastValue}}, Limit, Used e Remaining.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Remaining',
                  meaning: 'Quanti valori la sequenza può ancora produrre prima del suo limite.',
                  action:
                    'Quando Remaining scende, pianifica il passaggio della colonna a `bigint`.',
                },
                {
                  term: 'Not readable.',
                  meaning:
                    'La sequenza non può essere letta. Il motivo è The monitoring role has no SELECT on this sequence: grant it, or run the agent at tier 1., oppure Reading the sequence waited too long for a lock.',
                  action:
                    'Concedi SELECT sulla sequenza al ruolo di monitoraggio, oppure verifica se in quel momento la sequenza era bloccata da un lock.',
                },
              ],
            },
          ],
        },
        {
          id: 'partitions',
          title: '{{ui:instance.catalog.tabs.partitions}}',
          blocks: [
            {
              kind: 'text',
              text: 'La tab {{ui:instance.catalog.tabs.partitions}} elenca le tabelle partizionate con Key, {{ui:instance.catalog.tabs.partitions}}, {{ui:instance.catalog.part.sizeEstimate}}, {{ui:instance.catalog.part.defaultPartition}}, Invalid indexes e Analyzed.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.catalog.part.defaultPartition}}',
                  meaning:
                    'La partizione di default della tabella, che riceve le righe che non corrispondono ad alcuna altra partizione.',
                },
                {
                  term: 'Invalid indexes',
                  meaning: 'Il numero di indici non validi sulla tabella partizionata.',
                },
              ],
            },
          ],
        },
        {
          id: 'functions',
          title: 'Functions',
          blocks: [
            {
              kind: 'text',
              text: 'La tab Functions elenca le funzioni tracciate con Language, Calls, {{ui:instance.catalog.fn.ownTime}}, {{ui:instance.catalog.fn.totalTime}} e {{ui:instance.catalog.fn.meanPerCall}}. {{ui:instance.catalog.fn.ownTime}} è il tempo nel corpo della funzione, senza le funzioni che chiama. {{ui:instance.catalog.fn.totalTime}} le include.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Calls',
                  meaning: 'Chiamate della funzione.',
                },
                {
                  term: 'Language',
                  meaning: 'Il linguaggio in cui è scritta la funzione.',
                },
                {
                  term: '{{ui:instance.catalog.fn.ownTime}}',
                  meaning: 'Tempo speso nel corpo della funzione, senza le funzioni che chiama.',
                },
                {
                  term: '{{ui:instance.catalog.fn.totalTime}}',
                  meaning: 'Tempo speso nella funzione, incluse le funzioni che chiama.',
                },
                {
                  term: '{{ui:instance.catalog.fn.meanPerCall}}',
                  meaning:
                    'Il tempo medio di una chiamata. La cella riporta not called quando la funzione non ha avuto chiamate nella finestra.',
                },
                {
                  term: 'No tracked function was called in the window, or track_functions is none.',
                  meaning:
                    'Lo stato vuoto mostrato quando nessuna funzione tracciata è stata chiamata nella finestra, oppure quando l’impostazione `track_functions` è none.',
                  action:
                    'Imposta `track_functions` a `pl` per tracciare le funzioni scritte in un linguaggio procedurale.',
                },
              ],
            },
          ],
        },
        {
          id: 'security-definer',
          title: '{{ui:instance.catalog.tabs.securityDefiner}}',
          blocks: [
            {
              kind: 'text',
              text: 'La tab {{ui:instance.catalog.tabs.securityDefiner}} elenca le funzioni dichiarate SECURITY DEFINER, che girano con i privilegi del proprietario, con {{ui:instance.catalog.sd.runsAs}} ed {{ui:instance.catalog.sd.executableBy}}. {{ui:instance.catalog.sd.executableBy}} riporta every role (PUBLIC) quando PUBLIC può eseguire la funzione. La regola dell’advisor `function.secdef_no_search_path` segnala una funzione senza un search_path fisso: la gravità è critical quando il proprietario è un superuser e PUBLIC può eseguirla, altrimenti warning.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.catalog.sd.runsAs}}',
                  meaning: 'Il proprietario con i cui privilegi gira la funzione.',
                },
                {
                  term: '{{ui:instance.catalog.sd.executableBy}}',
                  meaning:
                    'Chi può eseguire la funzione: every role (PUBLIC) quando PUBLIC ha il diritto di esecuzione.',
                  action:
                    'Revoca il diritto di esecuzione da PUBLIC, concedi EXECUTE ai ruoli che lo richiedono, e fissa il search path.',
                },
              ],
            },
            {
              kind: 'sql',
              label: 'Funzioni SECURITY DEFINER e impostazioni fissate',
              code: 'SELECT n.nspname AS schema, p.proname AS function, pg_get_userbyid(p.proowner) AS owner, p.proconfig AS settings FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace WHERE p.prosecdef;',
            },
            {
              kind: 'sql',
              label: 'Revoca l’esecuzione pubblica di una funzione',
              code: 'REVOKE EXECUTE ON FUNCTION function_name FROM PUBLIC;',
            },
          ],
        },
        {
          id: 'foreign-keys',
          title: 'Unindexed foreign keys',
          blocks: [
            {
              kind: 'text',
              text: 'La tab Unindexed foreign keys elenca le foreign key senza un indice che inizi con le loro colonne, con Constraint, Columns, References e Table size. La regola dell’advisor `index.fk_unindexed` le segnala.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Constraint',
                  meaning: 'Il nome del vincolo di foreign key.',
                  action:
                    'Crea un indice sulle colonne di riferimento con `CREATE INDEX CONCURRENTLY`, così le scritture non vengono bloccate.',
                },
                {
                  term: 'Every foreign key has an index',
                  meaning:
                    'Lo stato vuoto, con la descrizione: No foreign key without an index leading with its columns was reported.',
                },
              ],
            },
          ],
        },
        {
          id: 'extensions',
          title: '{{ui:instance.catalog.tabs.extensions}}',
          blocks: [
            {
              kind: 'text',
              text: 'La tab {{ui:instance.catalog.tabs.extensions}} elenca le estensioni installate in ogni database monitorato, con Extension, Schema, {{ui:instance.catalog.ext.installed}}, {{ui:instance.catalog.ext.packages}} e Update. La regola dell’advisor `extension.upgrade_available` segnala un’estensione con una versione più recente disponibile.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.catalog.ext.installed}}',
                  meaning: 'La versione dell’estensione installata nel database.',
                },
                {
                  term: '{{ui:instance.catalog.ext.packages}}',
                  meaning: 'La versione disponibile nei pacchetti installati.',
                  action:
                    'Leggi le note di rilascio dell’estensione prima di aggiornarla con `ALTER EXTENSION extension_name UPDATE;`.',
                },
                {
                  term: 'Update',
                  meaning:
                    'Lo stato di aggiornamento: available quando esiste una versione più recente. Il testo The packages no longer carry it. significa che i pacchetti non forniscono più la versione installata, e The versions cannot be compared. significa che le due versioni non sono confrontabili.',
                },
              ],
            },
          ],
        },
        {
          id: 'postgis',
          title: 'Spatial columns',
          blocks: [
            {
              kind: 'text',
              text: 'La tab {{ui:instance.catalog.tabs.postgis}} elenca le colonne spaziali di ogni database monitorato, con Column, Type, SRID, {{ui:instance.catalog.common.rowsEstimate}} e {{ui:instance.catalog.common.spatialIndex}}. Declared without typmod: each value carries its own SRID.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.catalog.common.spatialIndex}}',
                  meaning:
                    'Il metodo dell’indice spaziale sulla colonna, oppure il valore missing quando non ce n’è uno.',
                  action:
                    'Crea un indice spaziale sulla colonna con `CREATE INDEX CONCURRENTLY` quando la tabella è grande e la colonna viene cercata.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'logical',
      title: '{{ui:instance.section.logical}}',
      blocks: [
        {
          kind: 'text',
          text: 'La tab {{ui:instance.section.logical}} mostra le sottoscrizioni di questa istanza, con {{ui:instance.logical.col.subscription}}, State, Database, {{ui:instance.logical.col.publications}}, Slot, {{ui:instance.logical.col.lastMessage}}, {{ui:instance.logical.col.errors}} e Conflicts, e gli slot logici, con {{ui:instance.logical.col.decoded}}, {{ui:instance.overview.tile.transactions}}, {{ui:instance.logical.col.spilled}} e {{ui:instance.logical.col.streamed}}. Le regole dell’advisor `subscription.disabled`, `subscription.errors` e `subscription.not_receiving` segnalano una sottoscrizione disabilitata, una con errori, e una abilitata che non riceve modifiche. Quando l’istanza non ha replica logica, compare lo stato vuoto {{ui:instance.logical.noReplication}}.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'State',
              meaning:
                'Lo stato di una sottoscrizione: running, not running, silent oppure disabled. Una sottoscrizione non abilitata mostra disabled.',
              action:
                'Per una sottoscrizione disabilitata, abilitala con `ALTER SUBSCRIPTION subscription_name ENABLE;`. Per una che non riceve modifiche, controlla il publisher e la rete tra i due server.',
            },
            {
              term: '{{ui:instance.logical.col.spilled}}',
              meaning:
                'Le modifiche decodificate che uno slot logico scrive su disco. La regola dell’advisor `logical_slot.spill_high` segnala uno slot che scrive circa 1 MiB al secondo su disco.',
              action:
                'Aumenta `logical_decoding_work_mem` così la decodifica entra in memoria, come suggerisce l’advisor.',
            },
          ],
        },
        {
          kind: 'sql',
          label: 'Sottoscrizioni e stato di abilitazione',
          code: 'SELECT subname, subenabled FROM pg_subscription;',
        },
      ],
    },
    {
      id: 'readiness',
      title: '{{ui:instance.section.readiness}}',
      blocks: [
        {
          kind: 'text',
          text: 'La tab {{ui:instance.section.readiness}} elenca ciò che non è configurato su questo server, quindi alcune funzioni di pglens mostrano meno dati di quanti potrebbero, e come chiudere ogni lacuna. Un warning significa che una funzione di pglens è persa o degradata su questo server. Una nota significa che qualcosa è previsto così o è opzionale. Ogni lacuna mostra What you lose ed Evidence:, e una correzione che richiede un riavvio è marcata Requires restart. Quando non manca nulla, la tab riporta Everything pglens can use is set up on this server.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'What you lose',
              meaning: 'I dati o la funzione che la lacuna sottrae a pglens su questo server.',
            },
            {
              term: 'Requires restart',
              meaning:
                'La correzione cambia un’impostazione letta solo all’avvio, quindi l’agent o il server devono riavviarsi dopo. Senza riavvio la modifica non ha effetto.',
            },
          ],
        },
      ],
    },
    {
      id: 'troubleshooting',
      title: 'Troubleshooting',
      blocks: [
        {
          kind: 'terms',
          items: [
            {
              term: 'Unknown',
              meaning:
                'Un valore manca: l’agent non ha inviato alcun valore per esso. Il motivo scritto accanto a Unknown spiega perché.',
              action:
                'Controlla Last seen nell’intestazione: un Last seen fermo significa che l’agent non sta riportando. Quando il motivo indica un permesso mancante, concedilo e attendi il controllo successivo.',
            },
            {
              term: '{{ui:instance.section.host}} collection is local-only.',
              meaning:
                'La nota nella sezione {{ui:instance.host.section}} quando l’host è raccolto solo in locale. Dice: {{ui:instance.section.host}} collection is local-only. Configure `host_local` in the agent configuration documentation.',
              action:
                'Imposta `host_local` nella configurazione dell’agent, come descrive la documentazione.',
            },
          ],
        },
      ],
    },
  ],
}
