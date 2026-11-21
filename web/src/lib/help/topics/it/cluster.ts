import type { HelpTopic } from '../../types'

export const clusterTopic: HelpTopic = {
  id: 'cluster',
  group: 'monitoring',
  title: 'Dettaglio cluster',
  summary:
    'La pagina di dettaglio di un cluster: topologia di replica, replication lag nel tempo, slot di replica, drift di configurazione tra le sue istanze e i failover rilevati. Come leggere ogni tab e cosa fare con un lag o una replica mancante.',
  page: {
    path: '/',
    label: 'Apri Fleet',
  },
  sections: [
    {
      id: 'overview',
      title: 'A cosa serve la pagina',
      blocks: [
        {
          kind: 'text',
          text: 'Dettaglio cluster mostra un solo cluster: come replicano le sue istanze, quanto sono indietro gli standby, quali slot di replica trattengono WAL, dove la configurazione delle istanze differisce e quali failover sono stati rilevati. Si apre da una card di [{{ui:shell.nav.fleet}}](/help/fleet) e il suo indirizzo è `/clusters/<cluster_id>`. Ogni tab legge i propri dati dal server e si aggiorna da sola.',
        },
        {
          kind: 'steps',
          items: [
            'Apri il cluster dalla sua card in [{{ui:shell.nav.fleet}}](/help/fleet).',
            'Leggi la riga Cluster identity e, nel sottotitolo, il numero di istanze monitorate e la salute del cluster.',
            'Scegli la tab che risponde alla tua domanda: [{{ui:cluster.tabs.topology}}](/help/cluster#topology), [{{ui:cluster.tabs.lag}}](/help/cluster#lag), [{{ui:cluster.tabs.slots}}](/help/cluster#slots), [{{ui:cluster.tabs.drift}}](/help/cluster#drift) o [{{ui:cluster.tabs.events}}](/help/cluster#events).',
            'Se viene mostrato un problema, segui [Cosa fare in caso di problema](/help/cluster#what-to-do).',
          ],
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
                  term: 'Time range',
                  meaning:
                    "Il time range picker nell'header imposta la finestra dei grafici di replication lag.",
                },
                {
                  term: 'Cluster identity',
                  meaning:
                    "L'identificatore del cluster, mostrato come codice e seguito dalla sua fonte. Quando deriva dall'identificatore di sistema di PostgreSQL (`system_identifier`), pglens lo usa per distinguere i cluster. Quando la fonte è `manual`, deriva da un nome di cluster fornito dall'utente e non dall'identificatore di sistema.",
                  action:
                    "Se l'identità non deriva dall'identificatore di sistema, segui le istruzioni di concessione collegate sotto la card del cluster in [{{ui:shell.nav.fleet}}](/help/fleet).",
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'topology',
      title: 'Topologia',
      blocks: [
        {
          kind: 'text',
          text: 'La tab {{ui:cluster.tabs.topology}} disegna i collegamenti di replica del cluster: ogni freccia va da uno standby, una copia di sola lettura, al server da cui copia le modifiche. Sopra il grafico una riga conta le istanze e gli archi di replica. Selezionare un nodo istanza apre la sua pagina di istanza.',
        },
      ],
      subsections: [
        {
          id: 'graph-nodes',
          title: 'Nodi delle istanze',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'Badge del ruolo',
                  meaning:
                    "Ruolo dell'istanza. `primary` è il primary; `standby` è una copia di sola lettura. `unresolved` indica un estremo di arco che non è un'istanza nota, e `unknown` indica un'istanza di cui manca il ruolo.",
                },
                {
                  term: '{{ui:shell.topology.state.down}}, {{ui:shell.topology.state.up}} o {{ui:shell.topology.state.unknown}}',
                  meaning:
                    "Se l'agent dell'istanza ha inviato dati di recente: {{ui:shell.topology.state.up}} quando l'ultimo report ha al massimo 60 secondi, {{ui:shell.topology.state.down}} quando è più vecchio, {{ui:shell.topology.state.unknown}} quando il server non ha uno stato per essa. Un nodo {{ui:shell.topology.state.down}} ha un bordo nel colore destructive.",
                  action:
                    "Controlla l'agent dell'istanza; vedi [{{ui:shell.nav.fleet}}](/help/fleet).",
                },
                {
                  term: '{{ui:shell.topology.port}}',
                  meaning: "Porta su cui l'istanza è in ascolto.",
                },
                {
                  term: '{{ui:shell.topology.version}}',
                  meaning: "Versione di PostgreSQL eseguita dall'istanza.",
                },
                {
                  term: '{{ui:shell.topology.tier}}',
                  meaning:
                    "Livello di permesso che l'agent ha sull'istanza: T0 legge le statistiche, T1 esegue anche EXPLAIN per i piani delle query, T2 annulla o termina anche le sessioni.",
                },
                {
                  term: '{{ui:shell.topology.replayLag}}',
                  meaning:
                    'Su uno standby: il suo replay lag in secondi, con tre decimali, oppure Unknown quando non è stato riportato.',
                },
              ],
            },
          ],
        },
        {
          id: 'graph-arcs',
          title: 'Archi',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: "Etichetta dell'arco",
                  meaning:
                    'Lo sync state dello standby con il suo upstream, come lo riporta il primary. {{ui:cluster.common.unknown}} quando manca.',
                },
                {
                  term: 'Arco tratteggiato',
                  meaning:
                    "Confidenza bassa: lo standby non sta ricevendo dati da quell'upstream in questo momento, oppure non ha inviato report da più di 60 secondi.",
                  action: 'Vedi [Cosa fare in caso di problema](/help/cluster#what-to-do).',
                },
                {
                  term: 'Selezionare un arco',
                  meaning:
                    "Segna l'arco come arco selezionato. Il grafico non mostra altri dettagli: i valori sono nella tabella degli archi.",
                },
              ],
            },
          ],
        },
        {
          id: 'anomalies',
          title: 'Banner di anomalia',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:cluster.topology.anomalyTitle}}',
                  meaning:
                    'Banner di alert mostrato quando il grafico non ha primary o ne ha più di uno. Riporta Split brain: more than one primary is reported. oppure No primary is reported.',
                  action: 'Vedi [Cosa fare in caso di problema](/help/cluster#what-to-do).',
                },
                {
                  term: '{{ui:cluster.topology.warningTitle}}',
                  meaning:
                    'Banner di stato mostrato quando uno o più standby non hanno un upstream, oppure uno o più archi hanno confidenza bassa. Riporta One or more standbys have no upstream. e, quando serve, One or more edges have low confidence.',
                  action:
                    'Uno standby senza upstream non copia modifiche da nessun server; vedi [Cosa fare in caso di problema](/help/cluster#what-to-do).',
                },
              ],
            },
          ],
        },
        {
          id: 'edge-table',
          title: 'Tabella degli archi',
          blocks: [
            {
              kind: 'text',
              text: "Sotto il grafico, il pulsante {{ui:cluster.topology.showTable}} apre gli stessi collegamenti come tabella, per l'uso da tastiera e con lettori di schermo. {{ui:cluster.topology.hideTable}} la chiude.",
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:cluster.topology.colFrom}}',
                  meaning: 'Lo standby che copia le modifiche.',
                },
                {
                  term: '{{ui:cluster.topology.colTo}}',
                  meaning: 'Il server da cui lo standby copia le modifiche, il suo upstream.',
                },
                {
                  term: '{{ui:cluster.topology.colType}}',
                  meaning: "Tipo di collegamento di replica, come lo riporta l'agent.",
                },
                {
                  term: '{{ui:cluster.topology.colSync}}',
                  meaning:
                    'Sync state del collegamento. {{ui:cluster.common.unknown}} quando manca.',
                },
                {
                  term: '{{ui:cluster.topology.colConfidence}}',
                  meaning:
                    "`high` per un arco continuo, `low` per un arco tratteggiato: lo standby non riceve dati da quell'upstream in questo momento, oppure non invia report da più di 60 secondi.",
                },
                {
                  term: '{{ui:cluster.topology.colNote}}',
                  meaning:
                    'Una nota del server sul collegamento, quando ne ha una, per esempio su un collegamento a confidenza bassa.',
                },
                {
                  term: 'Filtri Type e Sync state',
                  meaning:
                    'Due select restringono la tabella per tipo di collegamento e per sync state. Il campo di ricerca cerca nelle colonne {{ui:cluster.topology.colFrom}}, {{ui:cluster.topology.colTo}} e {{ui:cluster.topology.colNote}}.',
                },
                {
                  term: '{{ui:cluster.topology.noEdges}}',
                  meaning: 'La tabella non ha collegamenti da mostrare.',
                  action:
                    'Controlla che ogni standby sia monitorato e invii report al server; vedi [Cosa fare in caso di problema](/help/cluster#what-to-do).',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'lag',
      title: 'Lag di replica',
      blocks: [
        {
          kind: 'text',
          text: "La tab {{ui:cluster.tabs.lag}} disegna un grafico per ogni standby e per il suo upstream, sull'intervallo di tempo selezionato. Ogni grafico indica il collegamento dallo standby al suo upstream, con il suo sync state, e traccia tre linee in secondi: write lag, flush lag e replay lag. I grafici usano campioni grezzi, presi ogni 10 secondi, e coprono al massimo 30 giorni.",
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'write_lag_sec',
              meaning:
                "Secondi tra il flush del WAL sull'upstream e la conferma di scrittura da parte dello standby.",
            },
            {
              term: 'flush_lag_sec',
              meaning:
                "Secondi tra il flush del WAL sull'upstream e la conferma da parte dello standby che lo ha scritto su disco.",
            },
            {
              term: 'replay_lag_sec',
              meaning:
                "Secondi tra il flush del WAL sull'upstream e la conferma da parte dello standby che lo ha replayato. Quando lo standby lo riporta da solo, il replay lag è una stima.",
              action:
                'Se il replay lag resta sopra 30 secondi, lo standby è indietro; vedi [Cosa fare in caso di problema](/help/cluster#what-to-do).',
            },
            {
              term: '{{ui:cluster.lag.noneTitle}}',
              meaning:
                "Nessun dato di lag in questo intervallo, quindi non c'è nulla da disegnare. Il lag viene registrato per ogni standby che copia le modifiche da un primary di questo cluster.",
              action:
                "Allarga il time range nell'header, oppure controlla lo standby; vedi [Cosa fare in caso di problema](/help/cluster#what-to-do).",
            },
            {
              term: '{{ui:cluster.lag.noData}}',
              meaning:
                'Il grafico di un collegamento non ha campioni: compare il messaggio No replication lag sample was returned for this edge.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: "Un buco in una linea indica un campione mancante, non misurato: nulla viene riempito. Quando l'intervallo selezionato è più lungo della finestra di conservazione grezza di 30 giorni, la tab mostra l'avviso Selected range exceeds the 30-day raw retention window. Scegli un intervallo più corto per vedere i campioni grezzi.",
        },
        {
          kind: 'sql',
          label: 'Lag di replica come lo vede il primary (eseguire sul primary)',
          code: 'SELECT application_name, client_addr, state, sync_state, write_lag, flush_lag, replay_lag FROM pg_stat_replication;',
        },
      ],
      subsections: [
        {
          id: 'replay-lag-bytes',
          title: 'Replay lag (bytes)',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:cluster.lag.replayBytes}}',
                  meaning:
                    'Lag dello standby in byte, come lo riporta il primary. Serve un campione degli ultimi 60 secondi. Altrimenti compare un trattino (—), il cui testo al passaggio del mouse dice No fresh byte lag sample: the primary has not reported this standby in the last 60 seconds.',
                  action:
                    'Se il trattino resta, verifica che il primary riporti questo standby; vedi [Cosa fare in caso di problema](/help/cluster#what-to-do).',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'slots',
      title: 'Slot di replica',
      blocks: [
        {
          kind: 'text',
          text: "La tab {{ui:cluster.tabs.slots}} elenca gli slot di replica del cluster: ognuno conserva il WAL che un consumer ancora richiede. Gli slot inattivi compaiono per primi, perché sono quelli che possono trattenere il WAL. La tabella mostra l'ultima lettura di ogni slot degli ultimi 5 minuti.",
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:cluster.slots.colName}}',
              meaning: 'Nome dello slot di replica, come lo mostra PostgreSQL.',
            },
            {
              term: '{{ui:cluster.slots.colInstance}}',
              meaning: "L'istanza che contiene lo slot.",
            },
            {
              term: 'Type',
              meaning: "Tipo dello slot come lo riporta l'agent. Unknown quando manca.",
            },
            {
              term: '{{ui:cluster.slots.filterActive}}',
              meaning:
                '{{ui:cluster.slots.stateActive}} quando un consumer è collegato allo slot, {{ui:cluster.slots.stateInactive}} quando nessuno lo è, {{ui:cluster.common.unknown}} quando non è riportato.',
              action:
                'Per uno slot inattivo, verifica se lo standby o il subscriber dietro di esso è down o disconnesso, e riconnettilo.',
            },
            {
              term: '{{ui:cluster.slots.filterWal}}',
              meaning:
                'Il valore `wal_status` riportato da PostgreSQL. Quando manca, per uno slot inattivo compare {{ui:cluster.slots.walRetaining}} e per uno attivo {{ui:cluster.slots.walStreaming}}.',
            },
            {
              term: '{{ui:cluster.slots.colRetained}}',
              meaning:
                'WAL conservato su disco per lo slot, a partire dalla sua posizione di riavvio. Sopra 1 GiB il valore è evidenziato: uno standby disconnesso trattiene il WAL e può riempire il disco del primary.',
              action:
                'Riporta in linea lo standby, oppure verifica se lo slot serve ancora prima di rimuoverlo. Rimuovere uno slot non può essere annullato.',
            },
            {
              term: '{{ui:cluster.slots.colSafeWal}}',
              meaning:
                'Spazio rimasto prima che lo slot perda il WAL, come impostato da `max_slot_wal_keep_size`. {{ui:cluster.slots.unlimited}} significa che non è riportato alcun valore, come quando `max_slot_wal_keep_size` è -1. None: required WAL already lost compare quando lo slot ha già perso il WAL che gli serve.',
            },
            {
              term: '{{ui:cluster.slots.colAge}}',
              meaning:
                "Tempo trascorso dall'ultima volta in cui lo slot era attivo, contato dalla sua ultima lettura attiva. Un trattino (—) indica che lo slot è attivo.",
            },
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'Il WAL trattenuto può riempire il disco del primary. Guarda prima i {{ui:cluster.slots.colRetained}} degli slot inattivi.',
        },
        {
          kind: 'sql',
          label: 'Slot e WAL trattenuto da ciascuno (eseguire sul primary)',
          code: 'SELECT slot_name, slot_type, active, wal_status, pg_size_pretty(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)) AS retained FROM pg_replication_slots;',
        },
      ],
    },
    {
      id: 'drift',
      title: 'Drift di configurazione',
      blocks: [
        {
          kind: 'text',
          text: 'La tab {{ui:cluster.tabs.drift}} confronta le impostazioni che ogni istanza del cluster ha riportato per ultima, con una colonna per istanza. Una cella è evidenziata quando il suo valore differisce dal riferimento, cioè il valore riportato dalla maggior parte delle istanze. Le impostazioni che devono differire, come port e listen_addresses, sono escluse.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:cluster.drift.colSetting}}',
              meaning:
                "Nome dell'impostazione di PostgreSQL. Il campo di ricerca filtra su di essa.",
            },
            {
              term: 'Colonne delle istanze',
              meaning:
                "Ogni colonna è un'istanza, indicata con la sua etichetta, e mostra il valore che ha riportato per ultimo. Sono incluse solo le istanze che hanno inviato dati nell'ultima ora.",
            },
            {
              term: 'Cella evidenziata',
              meaning:
                "Un valore diverso dal riferimento. In caso di parità il riferimento è il valore del primary; senza primary, il valore dell'istanza con l'id più basso.",
              action:
                "Verifica se la differenza è attesa per quell'istanza. Se non lo è, allinea l'impostazione su quell'istanza.",
            },
            {
              term: '{{ui:cluster.drift.noneAloneTitle}}',
              meaning:
                "Di questo cluster è monitorata una sola istanza, quindi non c'è ancora nulla da confrontare.",
              action: 'Monitora una seconda istanza del cluster per confrontarne le impostazioni.',
            },
            {
              term: '{{ui:cluster.drift.noneTitle}}',
              meaning:
                "Nessuna impostazione differisce tra le istanze di questo cluster che hanno inviato dati nell'ultima ora.",
            },
          ],
        },
        {
          kind: 'sql',
          label: "Leggi un'impostazione su un'istanza (eseguire su ogni istanza)",
          code: "SELECT name, setting, unit, source FROM pg_settings WHERE name = 'max_connections';",
        },
      ],
    },
    {
      id: 'events',
      title: 'Timeline degli eventi',
      blocks: [
        {
          kind: 'text',
          text: 'La tab {{ui:cluster.tabs.events}} elenca i failover rilevati in questo cluster, dal più recente, con il vecchio e il nuovo primary. Questa tab mostra solo eventi di failover, quindi il suo filtro {{ui:cluster.events.typeLabel}} contiene solo quel tipo. Per gli altri tipi di evento, apri la pagina {{ui:shell.nav.events}} su `/events`.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:cluster.events.colTime}}',
              meaning:
                "Quando è avvenuto l'evento, nel fuso orario della console. Compare {{ui:cluster.events.unknownTime}} quando l'orario non può essere letto.",
            },
            {
              term: '{{ui:cluster.events.colEvent}}',
              meaning: "Titolo dell'evento. Un failover è intitolato Failover detected.",
            },
            {
              term: '{{ui:cluster.events.colSeverity}}',
              meaning:
                "Gravità dell'evento, mostrata come pill: critical, warning o info. Un failover è un warning.",
            },
            {
              term: '{{ui:cluster.events.colType}}',
              meaning:
                "Il nome grezzo dell'evento, sempre mostrato, così un evento che la console non conosce resta visibile.",
            },
            {
              term: '{{ui:cluster.events.colDetails}}',
              meaning:
                'Indica il vecchio primary e il nuovo primary, ciascuno con un link alla sua istanza, seguiti da cosa verificare. Per un failover dice: Check whether the promotion was planned or caused by a primary failure.',
              action:
                'Verifica se la promozione era pianificata o causata da un guasto del primary, poi conferma nella tab Topology quale istanza è il primary.',
            },
            {
              term: '{{ui:cluster.events.noMatch}}',
              meaning:
                "Compare quando la lista è vuota. Se non c'è alcun failover registrato, la tab mostra questo messaggio.",
            },
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: "Uno switchover pianificato viene registrato anche come failover, e l'evento non distingue i due casi. Verifica la promozione per sapere se era pianificata.",
        },
      ],
    },
    {
      id: 'what-to-do',
      title: 'Cosa fare in caso di problema',
      blocks: [
        {
          kind: 'text',
          text: 'Usa questa sezione quando topologia, lag o slot di un cluster mostrano un problema. Ogni passo indica prima il controllo da fare. pglens non modifica il database: i comandi qui sotto sono da verificare ed eseguire tu.',
        },
        {
          kind: 'steps',
          items: [
            'Apri la tab {{ui:cluster.tabs.topology}} e leggi il banner, se ne compare uno.',
            'Zero primary: scopri perché nessuna istanza è primary, e promuovi uno standby se serve.',
            "Due o più primary: individua il primary autorevole, e retrocedi l'altro.",
            'Standby senza upstream, o arco tratteggiato: verifica che lo standby riceva dati dal suo upstream, poi controlla il suo agent in [{{ui:shell.nav.fleet}}](/help/fleet).',
            'Lag sopra 30 secondi: apri la tab {{ui:cluster.tabs.lag}} e verifica se il lag continua a crescere.',
            'Slot inattivo con {{ui:cluster.slots.colRetained}} elevati: controlla lo standby dietro di esso e riconnettilo.',
          ],
        },
        {
          kind: 'sql',
          label: 'Ruolo di questa istanza (eseguire su ogni istanza): false su un primary',
          code: 'SELECT pg_is_in_recovery();',
        },
        {
          kind: 'sql',
          label: 'Stato dello streaming di uno standby (eseguire sullo standby)',
          code: 'SELECT status, sender_host, sender_port, last_msg_receipt_time FROM pg_stat_wal_receiver;',
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'Conferma quale istanza è autorevole prima di promuovere o retrocedere qualcosa: una scelta sbagliata può scartare scritture.',
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
              term: '{{ui:cluster.notFound.title}}',
              meaning:
                "Nessun cluster monitorato corrisponde all'identificatore nell'indirizzo. La pagina offre un link per tornare alla panoramica della flotta.",
              action:
                "Apri il cluster da [{{ui:shell.nav.fleet}}](/help/fleet) e verifica che l'identificatore nell'indirizzo sia completo.",
            },
            {
              term: 'Cluster topology is showing the last successful snapshot while the latest refresh failed.',
              meaning:
                "L'ultimo aggiornamento della topologia è fallito, quindi la tab Topology mostra l'ultimo snapshot caricato. I suoi valori potrebbero essere vecchi.",
              action:
                'Attendi il prossimo aggiornamento, oppure ricarica la pagina. Se continua a fallire, controlla il server e i suoi log.',
            },
            {
              term: 'No replication observed for this cluster.',
              meaning:
                'Il grafico non ha archi, quindi non viene mostrato alcun collegamento di replica.',
              action:
                'Controlla che ogni standby sia monitorato e invii report al server; vedi [Cosa fare in caso di problema](/help/cluster#what-to-do).',
            },
          ],
        },
      ],
    },
  ],
}
