import type { CaptionEntry } from '@/lib/captions'
import type { activityCaptions as english } from '@/lib/captionsets/activity'

export const activity: Readonly<Record<keyof typeof english, CaptionEntry>> = {
  'ash.page': {
    what: 'Active Session History (ASH): pglens campiona a intervalli regolari ciò che ogni processo server occupato (uno per connessione client) sta eseguendo o aspetta.',
    source:
      'Campioni ASH dell’agent: pg_stat_activity letto una volta al secondo e memorizzato come conteggi per finestra di 10 secondi; le sessioni pglens dell’agent sono escluse.',
    unit: 'backend medi',
    window: 'Intervallo di tempo selezionato.',
    howToRead:
      'Ogni grafico conta i backend non idle. Un backend senza wait event è conteggiato come CPU. Seleziona una serie per scendere dal tipo di wait event al wait event, fino alla query.',
  },
  'ash.breakdown-types': {
    what: 'Numero medio di backend non idle nel tempo, suddiviso per il tipo di attesa in cui si trovavano; la CPU indica i backend che non riportavano attesa.',
    source:
      'Campioni ASH: l’agent legge pg_stat_activity una volta al secondo (backend client e worker paralleli con stato diverso da idle, escluse le sessioni pglens/ dell’agent) e memorizza i conteggi per finestra di 10 secondi; un backend senza wait event è conteggiato come CPU.',
    unit: 'backend medi (campioni divisi per i tick di campionamento del bucket)',
    window:
      'Intervallo di tempo selezionato, in bucket da 10 secondi o più (al massimo 180 per grafico).',
    howToRead:
      'Il valore 2 significa che in media due backend non erano idle in quel bucket. Seleziona una serie per vedere i suoi wait event. Un vuoto: nessun campione memorizzato per quel bucket.',
  },
  'ash.breakdown-events': {
    what: 'Numero medio di backend non idle nel tempo, suddiviso per il wait event esatto in cui si trovavano; CPU indica l’assenza di wait event.',
    source:
      'Campioni ASH: l’agent legge pg_stat_activity una volta al secondo (backend client e worker paralleli con stato diverso da idle, escluse le sessioni pglens/ dell’agent) e memorizza i conteggi per finestra di 10 secondi; un backend senza wait event è conteggiato come CPU.',
    unit: 'backend medi (campioni divisi per i tick di campionamento del bucket)',
    window:
      'Intervallo di tempo selezionato, in bucket da 10 secondi o più (al massimo 180 per grafico).',
    howToRead:
      'Seleziona un evento per vedere le query campionate mentre aspettavano in esso. Gli eventi più piccoli sono raggruppati in altro, così il totale resta invariato. Un vuoto: nessun campione memorizzato per quel bucket.',
  },
  'ash.breakdown-queries': {
    what: 'Numero medio di backend non idle nel tempo, suddiviso per la query che stavano eseguendo, identificata dal suo query id.',
    source:
      'Campioni ASH: l’agent legge pg_stat_activity una volta al secondo e memorizza la colonna query_id per finestra di 10 secondi; è valorizzata solo con compute_query_id attivo, o auto con un modulo come pg_stat_statements caricato.',
    unit: 'backend medi (campioni divisi per i tick di campionamento del bucket)',
    window:
      'Intervallo di tempo selezionato, in bucket da 10 secondi o più (al massimo 180 per grafico).',
    howToRead:
      'Ogni serie è una query, non un gruppo di attesa; le query più piccole sono raggruppate in altro. Un vuoto: nessun campione memorizzato per quel bucket.',
  },
  'ash.stack-types': {
    what: 'Un solo grafico impilato: ogni serie è un tipo di wait event e l’altezza della pila è il numero medio di backend non idle.',
    source:
      'Campioni ASH: l’agent legge pg_stat_activity una volta al secondo (backend client e worker paralleli con stato diverso da idle, escluse le sessioni pglens/ dell’agent) e memorizza i conteggi per finestra di 10 secondi; un backend senza wait event è conteggiato come CPU.',
    unit: 'backend medi (campioni divisi per i tick di campionamento del bucket)',
    window:
      'Intervallo di tempo selezionato, in bucket da 10 secondi o più (al massimo 180 per grafico).',
    howToRead:
      'Seleziona una serie per vedere i suoi wait event. Un vuoto: nessun campione memorizzato per quel bucket.',
  },
  'ash.stack-events': {
    what: 'Un solo grafico impilato: ogni serie è un wait event e l’altezza della pila è il numero medio di backend non idle.',
    source:
      'Campioni ASH: l’agent legge pg_stat_activity una volta al secondo (backend client e worker paralleli con stato diverso da idle, escluse le sessioni pglens/ dell’agent) e memorizza i conteggi per finestra di 10 secondi; un backend senza wait event è conteggiato come CPU.',
    unit: 'backend medi (campioni divisi per i tick di campionamento del bucket)',
    window:
      'Intervallo di tempo selezionato, in bucket da 10 secondi o più (al massimo 180 per grafico).',
    howToRead:
      'Seleziona una serie per vedere le query campionate in quell’evento. Un vuoto: nessun campione memorizzato per quel bucket.',
  },
  'ash.breakdown-dimension': {
    what: 'Numero medio di backend non idle nel tempo, suddiviso per l’utente, l’indirizzo client o l’applicazione della loro sessione.',
    source:
      'Campioni ASH: l’agent legge usename, client_addr e application_name di pg_stat_activity una volta al secondo (solo le dimensioni che checks.ash.dimensions mantiene) e memorizza i conteggi per finestra di 10 secondi.',
    unit: 'backend medi (campioni divisi per i tick di campionamento del bucket)',
    window:
      'Intervallo di tempo selezionato, in bucket da 10 secondi o più (al massimo 180 per grafico).',
    howToRead:
      'Un valore vuoto è mostrato come (sconosciuto). Seleziona una serie per filtrare la pagina; i gruppi più piccoli sono raggruppati in altro. Un vuoto: nessun campione memorizzato per quel bucket.',
  },
  'ash.stack-dimension': {
    what: 'Un solo grafico impilato: ogni serie è un utente, un indirizzo client o un’applicazione e l’altezza della pila è il numero medio di backend non idle.',
    source:
      'Campioni ASH: usename, client_addr e application_name di pg_stat_activity, letti una volta al secondo e memorizzati per finestra di 10 secondi.',
    unit: 'backend medi (campioni divisi per i tick di campionamento del bucket)',
    window:
      'Intervallo di tempo selezionato, in bucket da 10 secondi o più (al massimo 180 per grafico).',
    howToRead:
      'Seleziona una serie per filtrare la pagina; il filtro appare come chip sopra il grafico. Un vuoto: nessun campione memorizzato per quel bucket.',
  },
  'ash.stack-queries': {
    what: 'Un solo grafico impilato: ogni serie è un query id e l’altezza della pila è il numero medio di backend non idle.',
    source:
      'Campioni ASH: la colonna query_id di pg_stat_activity, letta una volta al secondo e memorizzata per finestra di 10 secondi.',
    unit: 'backend medi (campioni divisi per i tick di campionamento del bucket)',
    window:
      'Intervallo di tempo selezionato, in bucket da 10 secondi o più (al massimo 180 per grafico).',
    howToRead:
      'Le query più frequenti sotto il grafico elencano il testo di ciascun id. Un vuoto: nessun campione memorizzato per quel bucket.',
  },
  'ash.chart-cpu': {
    what: 'Numero medio di backend non idle che non riportavano wait event, contati come in esecuzione sulla CPU.',
    source:
      'Campioni ASH: l’agent legge pg_stat_activity una volta al secondo (backend client e worker paralleli con stato diverso da idle, escluse le sessioni pglens/ dell’agent) e memorizza i conteggi per finestra di 10 secondi; un backend senza wait event è conteggiato come CPU.',
    unit: 'backend medi (campioni divisi per i tick di campionamento del bucket)',
    window:
      'Intervallo di tempo selezionato, in bucket da 10 secondi o più (al massimo 180 per grafico).',
    howToRead:
      'Questo grafico ha una scala propria, così un gruppo piccolo non viene appiattito da uno grande. Un vuoto: nessun campione memorizzato per quel bucket.',
  },
  'ash.chart-io': {
    what: 'Numero medio di backend non idle in attesa di una lettura o scrittura sullo storage.',
    source:
      'Campioni ASH: pg_stat_activity.wait_event_type di tipo I/O, letto una volta al secondo e memorizzato per finestra di 10 secondi.',
    unit: 'backend medi (campioni divisi per i tick di campionamento del bucket)',
    window:
      'Intervallo di tempo selezionato, in bucket da 10 secondi o più (al massimo 180 per grafico).',
    howToRead:
      'Questo grafico ha una scala propria. Seleziona una riga della legenda per isolare quella serie. Un vuoto: nessun campione memorizzato per quel bucket.',
  },
  'ash.chart-locks': {
    what: 'Numero medio di backend non idle in attesa di un lock o di una contesa interna, come un buffer pin.',
    source:
      'Campioni ASH: pg_stat_activity.wait_event_type Lock, LWLock, BufferPin o Buffer, letto una volta al secondo e memorizzato per finestra di 10 secondi.',
    unit: 'backend medi (campioni divisi per i tick di campionamento del bucket)',
    window:
      'Intervallo di tempo selezionato, in bucket da 10 secondi o più (al massimo 180 per grafico).',
    howToRead:
      'Lock è un lock pesante tenuto da un’altra transazione; LWLock è un lock interno leggero. Questo grafico ha una scala propria. Un vuoto: nessun campione memorizzato per quel bucket.',
  },
  'ash.chart-client': {
    what: 'Numero medio di backend non idle in attesa dell’applicazione client o di un altro processo del server.',
    source:
      'Campioni ASH: pg_stat_activity.wait_event_type Client o IPC, letto una volta al secondo e memorizzato per finestra di 10 secondi.',
    unit: 'backend medi (campioni divisi per i tick di campionamento del bucket)',
    window:
      'Intervallo di tempo selezionato, in bucket da 10 secondi o più (al massimo 180 per grafico).',
    howToRead:
      'Questo grafico ha una scala propria. Seleziona una riga della legenda per isolare quella serie. Un vuoto: nessun campione memorizzato per quel bucket.',
  },
  'ash.chart-other': {
    what: 'Numero medio di backend non idle in attese di timer, attività in background, estensioni e altre non mostrate negli altri grafici.',
    source:
      'Campioni ASH: pg_stat_activity.wait_event_type Timeout, Activity o Extension, qualsiasi altro tipo, e le serie raggruppate in altro.',
    unit: 'backend medi (campioni divisi per i tick di campionamento del bucket)',
    window:
      'Intervallo di tempo selezionato, in bucket da 10 secondi o più (al massimo 180 per grafico).',
    howToRead:
      'Questo grafico ha una scala propria. Un vuoto: nessun campione memorizzato per quel bucket.',
  },
  'ash.top-queries': {
    what: 'Le query campionate più spesso nell’ambito di attesa scelto, con il testo normalizzato e il numero medio di backend non idle.',
    source:
      'Campioni ASH sommati per query id nell’intervallo (primi 20), con il testo normalizzato più recente di quell’id da query_texts, raccolto da pg_stat_statements; ASH non legge mai il testo delle query in tempo reale.',
    unit: 'campioni; backend medi (campioni divisi per i tick di campionamento dell’intero intervallo)',
    window: 'Intervallo di tempo selezionato.',
    howToRead:
      'I valori letterali non sono disponibili: il testo è normalizzato. La riga dell’ambito indica il tipo o l’evento di wait event a cui è limitata la classifica.',
  },
  'ash.chart-layout': {
    what: 'Un grafico per gruppo di attesa, ciascuno con la propria scala, oppure un solo grafico impilato di tutti i gruppi.',
    source:
      'Una scelta di vista mantenuta nell’indirizzo della pagina (view=stacked); i dati e i bucket non cambiano.',
    unit: 'scelta di layout: separato o impilato',
    window: 'Questa vista della pagina.',
  },
  'activity.section': {
    what: 'Connessioni, età di transazioni e stati, transazioni preparate, distanza dal wraparound e deadlock dell’ultimo campione di attività, con le suddivisioni.',
    source:
      'Il controllo activity dell’agent (pg_stat_activity, pg_prepared_xacts, pg_database) e il contatore dei deadlock di pg_stat_database, letti tramite l’endpoint di attività dell’istanza.',
    window:
      'Ultima lettura degli ultimi 30 secondi, campionata ogni 10 secondi; la pagina si aggiorna ogni 5 secondi.',
    howToRead:
      'Una scheda mostra Sconosciuto quando l’agent non ha inviato alcun valore; zero è una lettura reale.',
  },
  'activity.longest-transaction': {
    what: 'Età della transazione aperta più vecchia tra le sessioni client.',
    source:
      'pg_stat_activity: il massimo di now() - xact_start sui backend client, escludendo la sessione dell’agent (pg_max_xact_age_seconds).',
    unit: 'secondi, mostrati come durata',
    window: 'Ultimo campione, preso ogni 10 secondi.',
    howToRead:
      'Zero significa che nessuna sessione client aveva una transazione aperta. Confronta con la scheda idle in transaction per vedere se la più vecchia sta ancora eseguendo uno statement.',
  },
  'activity.longest-idle-transaction': {
    what: 'Tempo massimo in cui una sessione client è rimasta dentro una transazione senza eseguire uno statement.',
    source:
      'pg_stat_activity: il massimo di now() - state_change delle sessioni nello stato idle in transaction o idle in transaction (aborted) (pg_max_idle_in_transaction_seconds).',
    unit: 'secondi, mostrati come durata',
    window: 'Ultimo campione, preso ogni 10 secondi.',
    howToRead: 'Zero: nessuna sessione è idle in una transazione.',
  },
  'activity.oldest-state-age': {
    what: 'Tempo massimo in cui una sessione client è rimasta nel proprio stato attuale, considerando tutti gli stati.',
    source:
      'pg_stat_activity: il massimo di now() - state_change di ciascuno stato (pg_max_state_age_seconds); la scheda mostra il più grande di questi.',
    unit: 'secondi, mostrati come durata',
    window: 'Ultimo campione, preso ogni 10 secondi.',
    howToRead:
      'Anche le connessioni idle tenute aperte da un pool la alzano, quindi di per sé non è un guasto.',
  },
  'activity.prepared-transactions': {
    what: 'Transazioni preparate per il commit in due fasi che non sono ancora confermate o annullate.',
    source: 'pg_prepared_xacts: numero di righe (pg_prepared_xacts).',
    unit: 'transazioni',
    window: 'Ultimo campione, preso ogni 10 secondi.',
    howToRead: 'Zero è normale quando nessuna applicazione usa il commit in due fasi.',
  },
  'activity.oldest-prepared-transaction': {
    what: 'Tempo dalla preparazione della transazione preparata più vecchia.',
    source:
      'pg_prepared_xacts: now() - min(prepared), zero se non ce n’è alcuna (pg_oldest_prepared_xact_seconds).',
    unit: 'secondi, mostrati come durata',
    window: 'Ultimo campione, preso ogni 10 secondi.',
  },
  'activity.frozen-xid-age': {
    what: 'ID di transazione consumati dall’ultimo congelamento del database più vecchio: la distanza verso il wraparound degli ID di transazione.',
    source:
      'pg_database: il massimo di age(datfrozenxid) su tutti i database (pg_max_datfrozenxid_age).',
    unit: 'transazioni',
    window: 'Ultimo campione, preso ogni 10 secondi.',
    thresholds: 'L’advisor valuta il rischio critico sopra 1.000.000.000 di ID di transazione.',
  },
  'activity.deadlock-rate': {
    what: 'Deadlock rilevati al secondo, sommati su tutti i database.',
    source:
      'pg_stat_database.deadlocks (pg_deadlocks_total), memorizzato come tasso al secondo; si somma l’ultima lettura di ciascun database.',
    unit: 'al secondo',
    window: 'Ultima lettura degli ultimi 90 secondi; raccolta ogni 30 secondi.',
    howToRead:
      'Un tasso piccolo mantiene due cifre significative, così alcuni deadlock all’ora non appaiono come zero. Gli statement coinvolti sono presenti solo nel log di PostgreSQL.',
  },
  'activity.saturation': {
    what: 'Indica se le connessioni client aperte superano la soglia di saturazione di max_connections.',
    source:
      'pg_connections_used_ratio (connessioni usate diviso max_connections), confrontato nel browser con 0,8.',
    unit: 'rapporto su max_connections',
    window: 'Ultimo campione.',
    thresholds: 'Saturo sopra l’80% di max_connections, il livello a cui l’advisor avvisa.',
  },
  'activity.by-database': {
    what: 'Connessioni client aperte ora, contate per database.',
    source:
      'pg_stat_activity: backend client raggruppati per datname (pg_connections_by_database); vengono elencati i dieci gruppi più grandi, il resto è sommato come altro.',
    unit: 'connessioni',
    window: 'Ultimo campione.',
  },
  'activity.by-state': {
    what: 'Connessioni client contate per stato della sessione, come active, idle e idle in transaction.',
    source:
      'pg_stat_activity: backend client raggruppati per stato e tipo di wait event (pg_backends); la scheda aggiunge i tipi di attesa di ciascuno stato.',
    unit: 'connessioni',
    window: 'Ultimo campione.',
  },
  'activity.xmin-holder': {
    what: 'Sessione, slot di replica, standby o transazione preparata che tiene lo snapshot più vecchio: il vacuum non rimuove righe morte più recenti.',
    source:
      'Il dato xmin_horizon del controllo activity: il più vecchio tra pg_stat_activity (backend_xmin, backend_xid), pg_replication_slots, pg_stat_replication e pg_prepared_xacts; non viene letto alcun testo di query.',
    unit: 'ID di transazione; tempo dall’avvio in secondi',
    window:
      'Ultimo campione, preso ogni 10 secondi; un dato più vecchio di 30 secondi non viene mostrato.',
    howToRead:
      'Il tempo dall’avvio non è misurato per uno slot o uno standby. Quando non è mostrato alcun responsabile, nessuno deteneva un xmin nell’ultimo campione.',
  },
  'activity.by-application': {
    what: 'Connessioni client aperte ora, contate per nome dell’applicazione.',
    source:
      'pg_stat_activity: backend client raggruppati per application_name (pg_connections_by_application); vengono elencati i dieci più grandi, il resto è sommato come altro.',
    unit: 'connessioni',
    window: 'Ultimo campione; riportato solo finché checks.activity.by_application è attivo.',
    howToRead: 'Attiva finché la configurazione dell’agent non la imposta a false.',
  },
  'activity.sessions-active': {
    what: 'Sessioni client dell’ultimo campione dei lock, con stato, database, utente e la prima riga della query quando è nota.',
    source:
      'Sessioni di getLocks: backend client di pg_stat_activity (al massimo 100); il testo della query è conservato solo per le sessioni in un albero di blocco.',
    unit: 'sessioni',
    window: 'Ultimo campione dei lock, preso ogni 10 secondi.',
    howToRead:
      'Questo elenco compare quando il server non distingue le sessioni applicative da quelle di pglens.',
  },
  'locks.blocking-tree': {
    what: 'Sessioni in attesa di un lock, ciascuna sotto la sessione che la blocca, partendo dai bloccanti radice che non aspettano nessuno.',
    source:
      'Nodi di getLocks: pg_stat_activity con pg_blocking_pids() per le sessioni in attesa di un lock pesante, unite al lock in attesa in pg_locks (mode, type, relation, wait start); l’albero è costruito nel browser.',
    unit: 'sessioni; durata dell’attesa in secondi',
    window:
      'Ultimo campione dei lock, preso ogni 10 secondi; la pagina si aggiorna ogni 5 secondi.',
    howToRead:
      'Attese più brevi dell’intervallo di campionamento possono sfuggire. Un bloccante radice non aspetta. Un ciclo: le sessioni si bloccano a vicenda.',
  },
  'locks.captured-incidents': {
    what: 'Deadlock, attese di lock, transazioni lunghe o idle e query lunghe catturate dall’agent su questo server, con ogni sessione coinvolta.',
    source:
      'getEvents di tipo *_captured per questa istanza: il watcher dell’agent legge pg_locks e pg_stat_activity in sola lettura quando ASH vede un’attesa di lock o una transazione lunga, e pg_stat_database.deadlocks.',
    unit: 'incidenti; sessioni per incidente',
    window: 'Ultimi 7 giorni, al massimo 100 catture, dalla più recente.',
    howToRead:
      'Seleziona una cattura per leggere la sua tabella, la riga, la modalità del lock, chi ha atteso chi e ogni statement. Un deadlock ricostruito è stato interrotto prima che l’agent vedesse il ciclo: le sue sessioni sono quelle viste in attesa in quel database poco prima.',
  },
  'readiness.severity': {
    what: 'Un avviso indica che una funzione di pglens è persa o degradata su questo server; una nota indica qualcosa di voluto così o facoltativo.',
    source:
      'getReadiness: la gravità della regola di Readiness di pglens che ha generato l’elemento.',
    unit: 'elementi',
    window: 'Ultimi valori; ricalcolati al massimo ogni 30 s.',
  },
  'readiness.requires-restart': {
    what: 'La correzione modifica un setting letto solo all’avvio, quindi l’agent o il server devono essere riavviati dopo.',
    source: 'getReadiness: il flag requires_restart che la regola imposta su ogni correzione.',
    window: 'Ultimi valori; ricalcolati al massimo ogni 30 s.',
    howToRead: 'Senza il riavvio la modifica non ha effetto.',
  },
}
