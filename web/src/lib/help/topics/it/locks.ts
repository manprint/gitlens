import type { HelpTopic } from '../../types'

export const locksTopic: HelpTopic = {
  id: 'locks',
  group: 'instance',
  title: 'Lock e attività',
  summary:
    'Le sessioni di un server PostgreSQL, i lock su cui attendono e le catene di blocco tra di esse, campionati ogni 10 secondi: l’albero dei blocchi, le sessioni applicative e pglens, l’attività dell’istanza, il blocco della pulizia più vecchio, gli incidenti catturati dall’agent, e le azioni sicure {{ui:locks.signal.cancelQuery}} e {{ui:locks.signal.terminateBackend}}.',
  page: {
    path: '/instances/:instanceId/locks',
    label: 'Apri {{ui:shell.nav.locks}}',
  },
  sections: [
    {
      id: 'overview',
      title: 'A cosa serve la pagina',
      blocks: [
        {
          kind: 'text',
          text: '{{ui:shell.nav.locks}} mostra le sessioni di un server PostgreSQL, i lock su cui attendono e le catene di blocco tra di esse. L’agent campiona lock e attività ogni 10 secondi, e la pagina mostra l’ultimo campione. Apri la pagina dalla tab {{ui:shell.nav.locks}} sotto l’intestazione dell’istanza. La vista aperta resta nell’indirizzo come `?view=`.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Blocking',
              meaning:
                'L’albero dei blocchi dell’ultimo campione dei lock, descritto in [{{ui:locks.blocking.title}}](/help/locks#blocking-tree).',
            },
            {
              term: '{{ui:locks.sessions.applicationTitle}}',
              meaning:
                'Le sessioni client delle applicazioni, una riga per sessione, descritte in [Sessions](/help/locks#sessions).',
            },
            {
              term: 'pglens sessions',
              meaning:
                'Le sessioni che pglens stesso apre sul server, mostrate con lo stesso layout di tabella, descritte in [Sessions](/help/locks#sessions).',
            },
            {
              term: 'Activity',
              meaning:
                'Conteggi delle connessioni, transazioni preparate, deadlock rate e il blocco della pulizia più vecchio, descritti in [Activity](/help/locks#activity-tiles).',
            },
            {
              term: '{{ui:locks.incidents.title}}',
              meaning:
                'I deadlock, le attese su lock, le transazioni lunghe o inattive e le query lunghe che l’agent ha catturato negli ultimi 7 giorni, ciascuno con le sue sessioni e i suoi statement, descritti in [{{ui:locks.incidents.title}}](/help/locks#captured-incidents).',
            },
          ],
        },
      ],
      subsections: [
        {
          id: 'freshness',
          title: 'Freschezza dei dati',
          blocks: [
            {
              kind: 'text',
              text: 'La pagina confronta l’età dell’ultimo campione con 20 secondi. I dati dei lock più vecchi mostrano l’avviso {{ui:locks.blocking.staleData}} I dati di attività più vecchi mostrano l’avviso {{ui:locks.activity.staleData}} Finché il campione dei lock è vecchio, le azioni sui segnali restano disabilitate e riportano {{ui:locks.signal.staleDisabled}}',
            },
          ],
        },
      ],
    },
    {
      id: 'blocking-tree',
      title: '{{ui:locks.blocking.title}}',
      blocks: [
        {
          kind: 'text',
          text: 'L’{{ui:locks.blocking.title}} mostra le sessioni dell’ultimo campione dei lock come una foresta. Una sessione che attende su un lock sta sotto la sessione che la blocca. Le radici, cioè le sessioni che bloccano altre sessioni, sono ordinate per numero di sessioni bloccate, poi per durata dell’attesa, quindi la catena più grande viene per prima. Il testo sopra l’albero indica l’ora dell’ultimo campione, la sua età e l’intervallo di campionamento di 10 secondi.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'User',
              meaning: 'Il ruolo della sessione.',
            },
            {
              term: '{{ui:locks.sessions.application}}',
              meaning: 'Il nome dell’applicazione della sessione.',
            },
            {
              term: '{{ui:locks.sessions.database}}',
              meaning: 'Il database a cui è connessa la sessione.',
            },
            {
              term: '{{ui:locks.detail.blockedBy}}',
              meaning: 'I PID delle sessioni che bloccano questa.',
            },
            {
              term: 'State',
              meaning:
                'Lo stato della sessione, come lo riporta PostgreSQL, per esempio `active` o `idle in transaction`.',
            },
            {
              term: '{{ui:locks.detail.waitEvent}}',
              meaning:
                'Il wait event in cui si trova la sessione, come lo riporta PostgreSQL. Per le attese su una finestra più lunga, vedi [{{ui:shell.nav.waitEvents}}](/help/wait-events).',
            },
            {
              term: '{{ui:locks.blocking.lockMode}}',
              meaning:
                'La modalità del lock che la sessione in attesa richiede, per esempio `ShareLock` o `AccessShareLock`.',
            },
            {
              term: '{{ui:locks.blocking.waitsOn}}',
              meaning:
                'Il lock richiesto dalla sessione, il suo tipo e la relazione su cui agisce. Una relazione in un database diverso da quello dell’agent non viene nominata.',
            },
            {
              term: '{{ui:locks.blocking.waitDuration}}',
              meaning:
                'Da quanto tempo la sessione attende, misurato sul server al momento del campione.',
            },
            {
              term: '{{ui:locks.blocking.waitingSince}}',
              meaning: 'L’ora in cui è iniziata l’attesa.',
            },
            {
              term: '{{ui:locks.sessions.query}}',
              meaning: 'Il testo della query della sessione.',
            },
            {
              term: '{{ui:locks.blocking.rootBlocker}}',
              meaning:
                'Una sessione che blocca altre e non è bloccata a sua volta. La riga riporta blocks the sessions below; not itself blocked.',
            },
            {
              term: '{{ui:locks.blocking.cycle}}',
              meaning:
                'La relazione di blocco forma un ciclo. L’albero smette di espandersi a questa sessione, e la riga riporta The blocking relationship forms a cycle; expansion stops at this marker.',
            },
            {
              term: 'Also waits on this blocker',
              meaning:
                'Una sessione che attende su un bloccante già mostrato altrove nell’albero. I suoi dettagli compaiono sotto il primo bloccante che la raggiunge.',
            },
            {
              term: '{{ui:locks.blocking.emptyTitle}}',
              meaning:
                'Lo stato vuoto quando l’ultimo campione non ha sessioni bloccate. Il suo testo dice No blocked sessions were present in the most recent 10-second sample.',
              action:
                'Non è un errore. Un episodio di blocco più breve dell’intervallo di campionamento può cadere tra due campioni, quindi controlla [{{ui:shell.nav.waitEvents}}](/help/wait-events) per le attese nel tempo.',
            },
          ],
        },
      ],
      subsections: [
        {
          id: 'keyboard',
          title: 'Tastiera',
          blocks: [
            {
              kind: 'text',
              text: 'Ogni nodo dell’albero è un elemento di albero. Freccia destra espande un nodo chiuso che ha figli, freccia sinistra chiude un nodo aperto, e freccia giù e freccia su passano al nodo visibile successivo e precedente. Un nodo con figli ha un pulsante Expand o Collapse, con nome Expand lock session o Collapse lock session seguito dal {{ui:locks.sessions.pid}}.',
            },
          ],
        },
      ],
    },
    {
      id: 'activity-tiles',
      title: 'Attività',
      blocks: [
        {
          kind: 'text',
          text: 'La vista Activity riassume le connessioni e le transazioni dell’istanza, campionate ogni 10 secondi. Le suddivisioni mostrano le connessioni per stato e per database, e per applicazione quando l’agent le riporta.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:locks.activity.connectionsUsed}}',
              meaning: 'Le connessioni in uso sull’istanza.',
            },
            {
              term: '{{ui:locks.activity.saturation}}',
              meaning:
                'Connessioni in uso come quota di `max_connections`. Lo stato riporta Near max_connections. sopra la soglia dell’80 % della regola dell’advisor `conn.saturation`, e Below the saturation threshold. sotto di essa. Senza un campione del limite la scheda riporta {{ui:locks.activity.noLimitSample}}',
              action:
                'Cerca le sessioni inattive nella vista {{ui:locks.sessions.applicationTitle}}. Quando ce ne sono molte, metti un pooler di connessioni davanti al server. Aumenta `max_connections` solo se la memoria del server lo consente.',
            },
            {
              term: '{{ui:locks.activity.longestTransaction}}',
              meaning: 'L’età della transazione aperta più vecchia dell’istanza.',
              action:
                'Ordina la vista {{ui:locks.sessions.applicationTitle}} per {{ui:locks.sessions.transactionAge}}, e verifica se l’applicazione conferma le proprie transazioni.',
            },
            {
              term: '{{ui:locks.activity.longestIdle}}',
              meaning:
                'L’età della sessione più lunga in stato idle in transaction. Una sessione così mantiene i propri lock e trattiene la pulizia mentre attende.',
              action:
                'Filtra la vista {{ui:locks.sessions.applicationTitle}} con {{ui:locks.filter.idleInTransactionOnly}}, e correggi il codice applicativo che lascia la transazione aperta.',
            },
            {
              term: '{{ui:locks.activity.oldestStateAge}}',
              meaning: 'Da quanto tempo la sessione più vecchia si trova nel suo stato attuale.',
            },
            {
              term: '{{ui:locks.activity.byDatabase}}',
              meaning:
                'Le connessioni dell’istanza raggruppate per database. Il blocco riporta {{ui:locks.activity.noSamples}} quando non è arrivata alcuna riga.',
            },
            {
              term: '{{ui:locks.activity.byState}}',
              meaning:
                'Le connessioni dell’istanza raggruppate per stato, come lo riporta PostgreSQL.',
            },
            {
              term: '{{ui:locks.activity.preparedTransactions}}',
              meaning:
                'Il numero di transazioni preparate aperte, cioè le transazioni a due fasi. Una transazione preparata mantiene i propri lock finché qualcuno non la conferma o non la annulla.',
              action:
                'Elencale con la query SQL qui sotto, e conferma o annulla quelle di cui nessun coordinatore ha più bisogno.',
            },
            {
              term: '{{ui:locks.activity.oldestPrepared}}',
              meaning: 'L’età della transazione preparata più vecchia.',
            },
            {
              term: '{{ui:locks.activity.sinceFreeze}}',
              meaning:
                'Gli ID di transazione usati da quando il database più vecchio è stato congelato l’ultima volta. La nota sotto la scheda dice che l’advisor segnala un problema critico a circa 1 miliardo di ID di transazione, prima che si esauriscano.',
              action:
                'Esegui `VACUUM` sulle tabelle dei database con l’età più alta, partendo dal più vecchio, e controlla di nuovo l’età dopo l’esecuzione.',
            },
            {
              term: '{{ui:locks.activity.deadlockRate}}',
              meaning:
                'Deadlock al secondo, la somma del tasso più recente di ogni database. Per identificare le query coinvolte serve l’analisi del log di PostgreSQL.',
              action:
                'Leggi i report dei deadlock nel log di PostgreSQL. [{{ui:shell.nav.instanceOverview}}](/help/instance-overview#summary-tiles) descrive la scheda {{ui:instance.overview.tile.deadlocksInRange}}.',
            },
            {
              term: '{{ui:locks.activity.byApplication}}',
              meaning:
                'Le connessioni dell’istanza raggruppate per nome applicazione. Quando l’agent non riporta i conteggi per applicazione, il blocco mostra Disabled, indica la funzione Per-application activity counts e l’impostazione `checks.activity.by_application`.',
            },
          ],
        },
        {
          kind: 'sql',
          label: '{{ui:locks.activity.preparedTransactions}}, dalla più vecchia',
          code: 'SELECT gid, prepared, owner, database FROM pg_prepared_xacts ORDER BY prepared;',
        },
        {
          kind: 'sql',
          label: 'Età degli ID di transazione di ogni database',
          code: 'SELECT datname, age(datfrozenxid) AS xid_age FROM pg_database ORDER BY xid_age DESC;',
        },
      ],
    },
    {
      id: 'xmin-holder',
      title: '{{ui:locks.activity.xminHolder}}',
      blocks: [
        {
          kind: 'text',
          text: 'Il blocco della pulizia più vecchio è la sessione o lo slot di replica che trattiene l’orizzonte xmin più vecchio. VACUUM non può rimuovere una riga morta più recente di quell’orizzonte, quindi finché questo holder resta aperto le sue tabelle continuano ad accumulare righe morte e bloat.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:locks.activity.holderKind}}',
              meaning:
                'Che cosa trattiene l’orizzonte: una Session, un Replication slot, uno Standby o una Prepared transaction.',
            },
            {
              term: '{{ui:locks.sessions.pid}}',
              meaning:
                'L’identità del holder, con il nome del suo tipo: {{ui:locks.sessions.pid}} per una Session, {{ui:cluster.slots.colName}} per un Replication slot, application_name per uno Standby, Transaction gid per una Prepared transaction.',
              action:
                'Termina la sessione da [Signal actions](/help/locks#signal-actions) quando la sua transazione può essere persa, oppure lascia che l’applicazione la confermi.',
            },
            {
              term: '{{ui:locks.activity.ageXids}}',
              meaning:
                'Quanti ID di transazione è vecchio l’orizzonte. Più il valore è alto, più il holder è vecchio.',
            },
            {
              term: '{{ui:locks.activity.timeSinceStart}}',
              meaning:
                'Da quanto tempo esiste il holder: per una Session l’inizio della sua transazione, per una Prepared transaction il momento della preparazione. La cella riporta not measured for this holder kind quando l’agent non invia un orario di inizio.',
            },
            {
              term: 'xmin holder unknown: no activity snapshot',
              meaning:
                'Il blocco mostra questo messaggio quando non c’è uno snapshot di attività che indichi il holder.',
            },
            {
              term: 'Replication slot',
              meaning:
                'Uno slot di replica trattiene l’orizzonte finché il suo consumer non ha confermato le modifiche. Quando il consumer non c’è più, lo slot trattiene orizzonte e WAL.',
              action:
                'Controlla il WAL che lo slot trattiene in [WAL directory](/help/instance-overview#wal-directory), e il bloat che causa in [Tables and indexes](/help/instance-overview#relations).',
            },
          ],
        },
        {
          kind: 'sql',
          label: 'Slot di replica e gli orizzonti che trattengono',
          code: 'SELECT slot_name, slot_type, active, xmin, catalog_xmin FROM pg_replication_slots;',
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'pg_drop_replication_slot rimuove lo slot in modo definitivo. Un consumer che si riconnette non trova più lo slot, e il WAL che lo slot tratteneva può poi essere rimosso. Eseguilo solo dopo aver verificato che nulla legge lo slot.',
        },
        {
          kind: 'sql',
          label: 'Elimina uno slot di replica che nessuno legge',
          code: "SELECT pg_drop_replication_slot('slot_name');",
        },
      ],
    },
    {
      id: 'sessions',
      title: 'Sessioni',
      blocks: [
        {
          kind: 'text',
          text: 'Le viste {{ui:locks.sessions.applicationTitle}} e pglens sessions mostrano ciascuna una tabella di sessioni, con le colonne {{ui:locks.sessions.pid}}, User, {{ui:locks.sessions.application}}, {{ui:locks.sessions.database}}, {{ui:locks.sessions.client}}, State, {{ui:locks.sessions.wait}}, {{ui:locks.sessions.transactionAge}}, {{ui:locks.sessions.queryAge}} e {{ui:locks.sessions.query}}. La tabella è ordinata per {{ui:locks.sessions.transactionAge}}, dal valore più alto, quindi le transazioni più vecchie vengono per prime. I suoi stati vuoti riportano {{ui:locks.sessions.applicationEmpty}} e {{ui:locks.sessions.pglensEmpty}}.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:locks.sessions.pid}}',
              meaning: 'Il process ID della sessione.',
            },
            {
              term: 'User',
              meaning: 'Il ruolo della sessione.',
            },
            {
              term: '{{ui:locks.sessions.application}}',
              meaning: 'Il nome dell’applicazione della sessione.',
            },
            {
              term: '{{ui:locks.sessions.database}}',
              meaning: 'Il database a cui è connessa la sessione.',
            },
            {
              term: '{{ui:locks.sessions.client}}',
              meaning: 'L’indirizzo client della sessione.',
            },
            {
              term: 'State',
              meaning: 'Lo stato della sessione, come lo riporta PostgreSQL.',
            },
            {
              term: '{{ui:locks.sessions.wait}}',
              meaning:
                'Il wait event della sessione. Una sessione che attende su un lock compare anche nell’{{ui:locks.blocking.title}}.',
            },
            {
              term: '{{ui:locks.sessions.transactionAge}}',
              meaning:
                'Da quanto tempo è aperta la transazione corrente. Un valore alto su una sessione in idle in transaction mantiene i suoi lock e trattiene la pulizia.',
              action:
                'Apri i dettagli della sessione e leggi il suo State. Usa [Signal actions](/help/locks#signal-actions) solo dopo aver controllato l’applicazione.',
            },
            {
              term: '{{ui:locks.sessions.queryAge}}',
              meaning: 'Da quanto tempo è in esecuzione la query corrente.',
            },
            {
              term: '{{ui:locks.sessions.query}}',
              meaning:
                'Il testo della query della sessione. I dettagli della sessione lo etichettano {{ui:locks.detail.queryTextRaw}} o {{ui:locks.detail.queryTextNormalised}}, secondo la sua origine.',
              action:
                'Trova la query in [{{ui:shell.nav.slowestQueries}}](/help/slowest-queries) quando è una query ricorrente.',
            },
          ],
        },
        {
          kind: 'text',
          text: 'Quando il campione è tagliato, una nota sopra la tabella indica quante connessioni di questa origine contiene, e che conserva le sessioni con le transazioni più vecchie.',
        },
        {
          kind: 'sql',
          label: 'Sessioni con le transazioni aperte più vecchie',
          code: "SELECT pid, usename, datname, state, wait_event_type, wait_event, now() - xact_start AS xact_age FROM pg_stat_activity WHERE backend_type = 'client backend' AND state <> 'idle' ORDER BY xact_start;",
        },
      ],
      subsections: [
        {
          id: 'filters',
          title: 'Filtri',
          blocks: [
            {
              kind: 'text',
              text: 'I filtri sopra la tabella restringono le righe. Cambiano solo ciò che la tabella mostra.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:locks.detail.waitEvent}} type',
                  meaning: 'Mantiene le sessioni con il tipo di wait event scelto.',
                },
                {
                  term: '{{ui:locks.sessions.database}}',
                  meaning: 'Mantiene le sessioni del database scelto.',
                },
                {
                  term: '{{ui:locks.filter.waiting}}',
                  meaning:
                    'L’opzione {{ui:locks.filter.waitingOnly}} mantiene le sessioni che attendono qualcosa.',
                },
                {
                  term: '{{ui:locks.filter.idleInTransaction}}',
                  meaning:
                    'L’opzione {{ui:locks.filter.idleInTransactionOnly}} mantiene le sessioni che sono in idle in transaction.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'connection-summary',
      title: '{{ui:locks.connections.title}}',
      blocks: [
        {
          kind: 'text',
          text: 'Il {{ui:locks.connections.title}} conta le connessioni client dell’istanza. Le tab scelgono uno dei sei gruppi: {{ui:locks.connections.tab.byOrigin}}, {{ui:locks.connections.tab.byApplication}}, {{ui:locks.connections.tab.byUser}}, {{ui:locks.connections.tab.byClient}}, {{ui:locks.connections.tab.byDatabase}} e {{ui:locks.connections.tab.byState}}. Ogni chiave del gruppo mostra il proprio conteggio.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:locks.connections.tab.byOrigin}}',
              meaning:
                'L’origine della connessione: application per le connessioni delle applicazioni, e pglens per le connessioni di pglens.',
            },
            {
              term: '{{ui:locks.connections.tab.byApplication}}',
              meaning: 'Conteggi per nome dell’applicazione.',
            },
            {
              term: '{{ui:locks.connections.tab.byUser}}',
              meaning: 'Conteggi per ruolo.',
            },
            {
              term: '{{ui:locks.connections.tab.byClient}}',
              meaning: 'Conteggi per indirizzo client.',
            },
            {
              term: '{{ui:locks.connections.tab.byDatabase}}',
              meaning: 'Conteggi per database.',
            },
            {
              term: '{{ui:locks.connections.tab.byState}}',
              meaning: 'Conteggi per stato della sessione.',
            },
            {
              term: '(empty name)',
              meaning:
                'La chiave delle connessioni che non hanno un nome nel gruppo, mostrata come (empty name).',
            },
          ],
        },
        {
          kind: 'text',
          text: 'Quando il campione dei lock è tagliato, una nota indica che il campione contiene solo le sessioni con le transazioni più vecchie, e che questi conteggi riguardano ogni connessione client dell’istanza.',
        },
      ],
    },
    {
      id: 'session-detail',
      title: 'Dettagli della sessione',
      blocks: [
        {
          kind: 'text',
          text: 'I dettagli della sessione mostrano questi campi di una sessione.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:locks.sessions.pid}}',
              meaning: 'Il process ID del backend.',
            },
            {
              term: '{{ui:locks.detail.origin}}',
              meaning:
                'Se la sessione è una connessione applicativa o una connessione pglens, mostrato dal badge di origine.',
            },
            {
              term: 'State',
              meaning: 'Lo stato della sessione, come lo riporta PostgreSQL.',
            },
            {
              term: '{{ui:locks.detail.waitEvent}} type',
              meaning: 'La classe del wait event, come la riporta PostgreSQL.',
            },
            {
              term: '{{ui:locks.detail.waitEvent}}',
              meaning:
                'Il wait event stesso, come lo riporta PostgreSQL. Vedi [{{ui:shell.nav.waitEvents}}](/help/wait-events).',
            },
            {
              term: '{{ui:locks.sessions.transactionAge}}',
              meaning: 'Tempo trascorso dall’inizio della transazione corrente.',
            },
            {
              term: '{{ui:locks.sessions.queryAge}}',
              meaning: 'Tempo trascorso dall’inizio della query corrente.',
            },
            {
              term: '{{ui:locks.detail.stateAge}}',
              meaning: 'Tempo trascorso da quando la sessione è entrata nel suo stato attuale.',
            },
            {
              term: '{{ui:locks.detail.backendStart}}',
              meaning: 'Quando è partito il processo backend, nel fuso orario della console.',
            },
            {
              term: '{{ui:locks.detail.clientAddress}}',
              meaning: 'L’indirizzo del client che si è connesso.',
            },
            {
              term: '{{ui:locks.sessions.application}}',
              meaning: 'Il nome dell’applicazione della sessione.',
            },
            {
              term: 'User',
              meaning: 'Il ruolo della sessione.',
            },
            {
              term: '{{ui:locks.sessions.database}}',
              meaning: 'Il database a cui è connessa la sessione.',
            },
            {
              term: '{{ui:locks.detail.queryId}}',
              meaning:
                'L’identificativo della query, come lo calcola PostgreSQL quando gli identificativi delle query sono attivi.',
            },
            {
              term: '{{ui:locks.detail.blockedBy}}',
              meaning: 'I PID delle sessioni che bloccano questa.',
            },
            {
              term: 'Blocking',
              meaning: 'I PID delle sessioni che questa blocca.',
            },
            {
              term: '{{ui:locks.detail.queryTextRaw}}',
              meaning: 'Il testo della query come lo riporta il server.',
            },
            {
              term: '{{ui:locks.detail.queryTextNormalised}}',
              meaning: 'Il testo della query dopo la normalizzazione dell’agent.',
            },
          ],
        },
      ],
    },
    {
      id: 'signal-actions',
      title: 'Azioni sui segnali',
      blocks: [
        {
          kind: 'text',
          text: 'Due azioni agiscono su una sessione. Compaiono nell’{{ui:locks.blocking.title}}, nei dettagli della sessione e sotto il blocco della pulizia più vecchio. {{ui:locks.signal.cancelQuery}} chiede al server di annullare la query che la sessione esegue, con `pg_cancel_backend`, e la sessione resta connessa. {{ui:locks.signal.terminateBackend}} termina la sessione, con `pg_terminate_backend`. Entrambe agiscono solo su client backend. Per un processo che non è un client backend le azioni non sono disponibili, e la nota riporta Signal actions unavailable: {{ui:locks.sessions.pid}} … is not a client backend.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:locks.signal.cancelQuery}}',
              meaning:
                'Chiama `pg_cancel_backend`. Interrompe la query in esecuzione e lascia la sessione connessa. Una sessione in idle in transaction non esegue alcuna query, quindi l’azione non ha effetto: usa {{ui:locks.signal.terminateBackend}}.',
            },
            {
              term: '{{ui:locks.signal.terminateBackend}}',
              meaning:
                'Chiama `pg_terminate_backend`. La connessione client viene chiusa e la sua transazione aperta viene annullata. La finestra di dialogo lo indica prima che il comando venga inviato.',
            },
            {
              term: '{{ui:locks.signal.cancelTitle}}',
              meaning:
                'Il titolo della finestra di conferma per {{ui:locks.signal.cancelQuery}}. Chiede a PostgreSQL di annullare la query corrente. Conferma con il pulsante {{ui:locks.signal.cancelQuery}}.',
            },
            {
              term: '{{ui:locks.signal.terminateTitle}}',
              meaning:
                'Il titolo della finestra di conferma per {{ui:locks.signal.terminateBackend}}. Conferma con il pulsante {{ui:locks.signal.terminateBackend}}.',
            },
            {
              term: '{{ui:locks.signal.keepRunning}}',
              meaning: 'Chiude la finestra senza inviare il comando.',
            },
            {
              term: 'allow_signal',
              meaning:
                'L’impostazione `allow_signal` del target continua ad applicarsi. I dati dei lock non la riportano, quindi un rifiuto dell’agent arriva come esito del comando, non come pulsante disabilitato.',
            },
            {
              term: '{{ui:locks.signal.staleDisabled}}',
              meaning:
                'I pulsanti sono disabilitati finché il campione dei lock è vecchio. Si riabilitano quando arriva il campione successivo.',
            },
            {
              term: '{{ui:locks.signal.viewAudit}}',
              meaning:
                'Un link mostrato dopo che un comando è terminato. Apre la tab audit della pagina {{ui:shell.nav.settings}}.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'Annullare o terminare sessioni richiede il livello di permessi T2. Concedilo con `monitoring_user.sql -v tier1=1 -v tier2=1`; il livello 2 richiede anche il livello 1.',
        },
        {
          kind: 'steps',
          items: [
            'Trova la sessione nell’{{ui:locks.blocking.title}}, e leggi il suo State, {{ui:locks.blocking.waitDuration}} e {{ui:locks.sessions.query}}.',
            'Scegli {{ui:locks.signal.cancelQuery}} per una query in esecuzione che vuoi fermare, oppure {{ui:locks.signal.terminateBackend}} quando la sessione stessa deve terminare.',
            'Leggi la finestra di dialogo, poi conferma con il pulsante con lo stesso nome, oppure scegli {{ui:locks.signal.keepRunning}}.',
            'Controlla l’esito in {{ui:locks.signal.viewAudit}}.',
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: '{{ui:locks.signal.terminateBackend}} annulla tutto il lavoro non confermato della sessione, e l’applicazione vede una connessione chiusa. Verifica che la transazione possa andare persa prima di confermare.',
        },
      ],
    },
    {
      id: 'captured-incidents',
      title: '{{ui:locks.incidents.title}}',
      blocks: [
        {
          kind: 'text',
          text: 'Il watcher dell’agent legge `pg_locks` e `pg_stat_activity`, in sola lettura, quando ASH vede un’attesa su lock, una transazione lunga o una query lunga, e registra ciò che trova come evento. La vista {{ui:locks.incidents.title}} elenca quegli eventi degli ultimi 7 giorni per questo server, dal più recente, al massimo 100. Selezionane uno per leggerlo per intero: la tabella e la riga (ctid), il lock mode, l’attesa più lunga, chi ha atteso chi, e ogni sessione con utente, applicazione, client, stato, tempi, la riga e le tabelle che teneva, e il suo statement, che Copy copia. La notifica dell’alert riporta la stessa tabella e i primi statement.',
        },
        {
          kind: 'text',
          text: 'Un deadlock dura solo `deadlock_timeout` (1 s per default) prima che PostgreSQL annulli una delle sue transazioni, quindi il watcher spesso non vede il ciclo stesso. Legge allora il contatore dei deadlock di `pg_stat_database`, e quando cresce registra un deadlock ricostruito: le sessioni che ha visto attendere un lock, o tenerlo, in quel database subito prima. Per un `INSERT` che attende su una chiave duplicata, la tabella è quella in cui scrive la sessione in attesa.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:locks.incidents.emptyTitle}}',
              meaning:
                'L’agent non ha registrato catture per questo server negli ultimi 7 giorni, o il suo watcher è spento.',
              action:
                'Controlla `checks.watch.enabled` nella configurazione dell’agent; le attese più brevi di mezzo secondo non vengono catturate.',
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
              term: '{{ui:locks.blocking.selectInstance}}',
              meaning: 'La pagina mostra questo messaggio quando non è selezionata alcuna istanza.',
              action:
                'Apri la pagina dalla tab {{ui:shell.nav.locks}} sotto l’intestazione di un’istanza.',
            },
            {
              term: 'No lock sample has been stored for this instance yet.',
              meaning:
                'L’albero dei blocchi non ha ancora alcun campione. I lock vengono campionati ogni 10 secondi, e gli episodi più brevi possono sfuggire.',
              action:
                'Verifica che l’agent sia in esecuzione e raggiunga il server. L’intestazione di [{{ui:shell.nav.instanceOverview}}](/help/instance-overview#header) mostra l’ora di Last seen.',
            },
            {
              term: '{{ui:locks.blocking.staleData}}',
              meaning:
                'L’ultimo campione dei lock è più vecchio di 20 secondi. Le azioni sui segnali restano disabilitate.',
              action:
                'Controlla il valore Last seen nell’intestazione dell’istanza. Un Last seen fermo significa che l’agent non sta riportando.',
            },
            {
              term: '{{ui:locks.activity.staleData}}',
              meaning: 'L’ultimo campione di attività è più vecchio di 20 secondi.',
              action:
                'Controlla l’agent come per i dati dei lock, in [{{ui:shell.nav.instanceOverview}}](/help/instance-overview#header).',
            },
            {
              term: '{{ui:locks.blocking.emptyTitle}}',
              meaning: 'L’ultimo campione non ha sessioni bloccate. È lo stato normale.',
              action:
                'Quando l’applicazione continua a bloccarsi, controlla le attese su una finestra più lunga in [{{ui:shell.nav.waitEvents}}](/help/wait-events).',
            },
            {
              term: '{{ui:locks.activity.noLimitSample}}',
              meaning:
                'La scheda {{ui:locks.activity.saturation}} non ha un valore del limite, quindi non può calcolare la quota di `max_connections`.',
              action:
                'Verifica che l’agent sia in esecuzione e riporti l’istanza. Vedi [{{ui:shell.nav.instanceOverview}}](/help/instance-overview#header).',
            },
            {
              term: '{{ui:locks.activity.noSamples}}',
              meaning: 'Una suddivisione della vista Activity non ha righe nel campione.',
              action:
                'Verifica che l’agent sia in esecuzione e riporti l’istanza. Vedi [{{ui:shell.nav.instanceOverview}}](/help/instance-overview#header).',
            },
            {
              term: '{{ui:locks.signal.staleDisabled}}',
              meaning: 'Il campione dei lock è vecchio, quindi i pulsanti sono disabilitati.',
              action: 'Attendi il campione successivo. I pulsanti si abilitano quando arriva.',
            },
          ],
        },
      ],
    },
  ],
}
