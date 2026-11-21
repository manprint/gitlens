import type { CaptionEntry } from '@/lib/captions'
import type { instanceCaptions as english } from '@/lib/captionsets/instance'

export const instance: Readonly<Record<keyof typeof english, CaptionEntry>> = {
  'instance.database-scope': {
    what: 'Il database monitorato di cui la pagina mostra le cifre, e i database dell’istanza che l’agent non monitora.',
    source:
      'Inventario dell’istanza inviato dall’agent (databases: datname, monitored, skip_reason), limitato dal budget databases.max dell’agent e dai filtri include.',
    unit: 'database',
    window: 'Ultimo inventario inviato dall’agent.',
    howToRead:
      'Il predefinito è il database monitorato più attivo, se è disponibile l’attività, altrimenti il primo. Un database non monitorato è un punto cieco in queste metriche.',
  },

  'overview.section': {
    what: 'Riquadri di riepilogo di un’istanza; le card sotto mostrano carico di lavoro, memoria e I/O, WAL e checkpoint, query, tabelle e indici.',
    source:
      'Metriche memorizzate dai controlli database_stats, activity, wal, checkpointer, statements, tables e indexes; le cifre per database usano il database selezionato.',
    window: 'Intervallo di tempo selezionato; passo di query per grafico.',
    howToRead:
      'Transazioni, cache hit ratio, file e byte temporanei e deadlock riguardano il database selezionato; connessioni, WAL e checkpoint riguardano tutta l’istanza; i grafici di query, tabelle e indici sommano quanto l’agent traccia in ogni database. I bucket nulli restano vuoti; la riga dell’intervallo di un grafico a contatore li conta.',
  },
  'overview.group-workload': {
    what: 'Transazioni, connessioni e gli errori che le interrompono. Transazioni, deadlock e conflitti riguardano il database selezionato; le connessioni sono a livello di istanza.',
    source:
      'Metriche memorizzate dai controlli database_stats e activity; ogni grafico indica la propria fonte.',
    window: 'Intervallo di tempo selezionato; passo di query per grafico.',
  },
  'overview.group-memory-io': {
    what: 'Quanto spesso le letture sono servite dagli shared buffer e quanto lavoro viene riversato su file temporanei, per il database selezionato.',
    source:
      'Metriche memorizzate dal controllo database_stats; ogni grafico indica la propria fonte.',
    window: 'Intervallo di tempo selezionato; passo di query per grafico.',
  },
  'overview.group-wal-checkpoints': {
    what: 'Write-ahead log generato da tutta l’istanza e i checkpoint che lo scrivono su disco.',
    source:
      'Metriche memorizzate dai controlli wal e checkpointer; ogni grafico indica la propria fonte.',
    window: 'Intervallo di tempo selezionato; passo di query per grafico.',
  },
  'overview.group-queries': {
    what: 'Totali sugli statement tracciati da pg_stat_statements (l’agent conserva gli statement principali di ogni rilevazione), per ogni database dell’istanza.',
    source: 'Serie di carico del controllo statements; ogni grafico indica la propria fonte.',
    window: 'Intervallo di tempo selezionato; passo di query per grafico.',
  },
  'overview.group-tables-indexes': {
    what: 'Totali sulle tabelle e sugli indici tracciati dall’agent, per ogni database dell’istanza. Tabelle e indici vengono campionati ogni 5 minuti.',
    source: 'Serie di carico dei controlli tables e indexes; ogni grafico indica la propria fonte.',
    window: 'Intervallo di tempo selezionato; passo di query per grafico.',
  },
  'overview.tx-rate': {
    what: 'Transazioni al secondo nel database selezionato, contando sia le confermate sia le annullate.',
    source:
      'pg_stat_database.xact_commit più xact_rollback (pg_xact_commit_total e pg_xact_rollback_total), memorizzati come tassi al secondo; il riquadro somma l’ultimo bucket di ciascuno.',
    unit: 'al secondo',
    window: 'Ultimo bucket dell’intervallo di tempo selezionato.',
    howToRead:
      'La cifra è confermate più annullate: non è mai sotto il tasso delle confermate. Un trattino indica nessun campione recente.',
  },
  'overview.wal-in-range': {
    what: 'WAL scritto da tutta l’istanza nell’intervallo selezionato, stimato dalla crescita della posizione del write-ahead log.',
    source:
      'pg_current_wal_lsn() (pg_last_wal_replay_lsn() su uno standby) come pg_wal_lsn_bytes; il riquadro somma ogni tasso di bucket per il tempo trascorso dal bucket misurato precedente.',
    unit: 'byte',
    window: 'Intervallo di tempo selezionato.',
    howToRead: 'Stimato: un tratto senza campioni viene riempito dal bucket misurato successivo.',
  },
  'overview.checkpoints-in-range': {
    what: 'Checkpoint avviati nell’intervallo selezionato, programmati nel tempo e richiesti insieme, stimati dai loro tassi al secondo.',
    source:
      'pg_stat_checkpointer num_timed più num_requested (pg_stat_bgwriter checkpoints_timed più checkpoints_req prima di PostgreSQL 17), memorizzati come pg_checkpoints_timed_total e pg_checkpoints_requested_total.',
    unit: 'checkpoint',
    window: 'Intervallo di tempo selezionato.',
    howToRead:
      'Stimato sommando ogni tasso di bucket per il tempo trascorso dal bucket precedente.',
  },
  'overview.temp-bytes-in-range': {
    what: 'Byte scritti nei file temporanei dalle query del database selezionato nell’intervallo selezionato, stimati dai tassi al secondo.',
    source:
      'pg_stat_database.temp_bytes (pg_temp_bytes_total), memorizzato come tasso al secondo e sommato sull’intervallo.',
    unit: 'byte',
    window: 'Intervallo di tempo selezionato.',
    howToRead: 'Zero: nessuna query ha scritto file temporanei nell’intervallo.',
  },
  'overview.temp-files-in-range': {
    what: 'File temporanei creati dalle query del database selezionato nell’intervallo selezionato, stimati dai tassi al secondo.',
    source:
      'pg_stat_database.temp_files (pg_temp_files_total), memorizzato come tasso al secondo e sommato sull’intervallo.',
    unit: 'file',
    window: 'Intervallo di tempo selezionato.',
    howToRead: 'Zero: nessuna query ha creato file temporanei nell’intervallo.',
  },
  'overview.deadlocks-in-range': {
    what: 'Deadlock rilevati nel database selezionato nell’intervallo selezionato, stimati dai tassi al secondo.',
    source:
      'pg_stat_database.deadlocks (pg_deadlocks_total), memorizzato come tasso al secondo e sommato sull’intervallo.',
    unit: 'deadlock',
    window: 'Intervallo di tempo selezionato.',
    howToRead: 'Zero: PostgreSQL non ha rilevato deadlock nell’intervallo.',
  },

  'overview.chart-transactions': {
    what: 'Transazioni confermate e annullate al secondo nel database selezionato, come due linee.',
    source:
      'pg_stat_database.xact_commit e xact_rollback (pg_xact_commit_total, pg_xact_rollback_total), differenze del contatore come tassi al secondo.',
    unit: 'al secondo',
    window: 'Intervallo di tempo selezionato, un punto per passo; campionato ogni 30 s.',
    howToRead:
      'Linea piatta a zero: nessuna transazione. Vuoto: nessun campione memorizzato o azzeramento del contatore; la riga dell’intervallo conta i vuoti.',
  },
  'overview.chart-connections': {
    what: 'Connessioni client aperte e il massimo configurato, per tutta l’istanza.',
    source:
      'Backend client contati da pg_stat_activity (pg_connections_used) e dall’impostazione max_connections (pg_connections_limit).',
    unit: 'connessioni',
    window: 'Intervallo di tempo selezionato; campionato ogni 10 s.',
    thresholds: 'La regola advisor conn.saturation avvisa sopra l’80% di max_connections.',
  },
  'overview.chart-deadlocks-conflicts': {
    what: 'Deadlock e conflitti di recovery al secondo nel database selezionato.',
    source:
      'pg_stat_database.deadlocks e conflicts (pg_deadlocks_total, pg_conflicts_total), differenze del contatore come tassi al secondo.',
    unit: 'al secondo',
    window: 'Intervallo di tempo selezionato; campionato ogni 30 s.',
    howToRead:
      'I conflitti di recovery si contano su uno standby, quindi quella linea resta a zero su un primario.',
  },
  'overview.chart-cache-hit-ratio': {
    what: 'Quota delle letture di pagina trovate nella cache di memoria di PostgreSQL (shared buffers) invece che richieste al sistema operativo, per passo di query.',
    source:
      'pg_stat_database blks_hit / (blks_hit + blks_read), dai tassi pg_blks_hit_total e pg_blks_read_total di ogni passo.',
    unit: '%',
    window: 'Intervallo di tempo selezionato; campionato ogni 30 s.',
    howToRead:
      'Le letture fuori dagli shared buffers possono venire dalla cache del sistema operativo: un rapporto basso non è sempre I/O su disco.',
  },
  'overview.chart-blocks': {
    what: 'Blocchi letti fuori dagli shared buffers e blocchi trovati in shared buffers al secondo, nel database selezionato.',
    source:
      'pg_stat_database.blks_read e blks_hit (pg_blks_read_total, pg_blks_hit_total), differenze del contatore come tassi al secondo.',
    unit: 'blocchi al secondo',
    window: 'Intervallo di tempo selezionato; campionato ogni 30 s.',
    howToRead:
      'Una lettura può venire dalla cache del sistema operativo, quindi non sempre da disco.',
  },
  'overview.chart-temp': {
    what: 'File temporanei creati e byte scritti al secondo nel database selezionato; i byte usano un secondo asse.',
    source:
      'pg_stat_database.temp_files e temp_bytes (pg_temp_files_total, pg_temp_bytes_total), differenze del contatore come tassi al secondo.',
    unit: 'file al secondo; byte al secondo sul secondo asse',
    window: 'Intervallo di tempo selezionato; campionato ogni 30 s.',
  },
  'overview.chart-wal': {
    what: 'Write-ahead log (WAL, il journal scritto prima dei file dati): byte, record e full-page image scritti al secondo da tutta l’istanza.',
    source:
      'pg_stat_wal wal_bytes, wal_records e wal_fpi (pg_wal_wal_bytes, pg_wal_wal_records, pg_wal_wal_fpi), differenze del contatore; si raccolgono solo le colonne che questo server ha.',
    unit: 'byte al secondo; record e full-page image al secondo sul secondo asse',
    window: 'Intervallo di tempo selezionato; campionato ogni 15 s.',
    howToRead:
      'Una full-page image è una pagina intera copiata nel WAL la prima volta che cambia dopo un checkpoint. Una linea manca quando la versione di PostgreSQL non ha quella colonna di pg_stat_wal.',
  },
  'overview.chart-checkpoints': {
    what: 'Checkpoint, i momenti in cui PostgreSQL scrive tutte le pagine modificate nei file dati, avviati in ogni passo: programmati dal tempo o richiesti prima.',
    source:
      'pg_stat_checkpointer num_timed e num_requested (pg_stat_bgwriter prima di PostgreSQL 17), come pg_checkpoints_timed_total e pg_checkpoints_requested_total.',
    unit: 'checkpoint per passo',
    window: 'Intervallo di tempo selezionato; campionato ogni 30 s.',
    howToRead:
      'Ogni punto è il conteggio di quel passo (tasso per durata del passo), quindi un evento raro vale 1.',
  },
  'overview.chart-checkpoint-time': {
    what: 'Tempo speso a scrivere e sincronizzare file durante i checkpoint, sommato in ogni passo di query.',
    source:
      'pg_stat_checkpointer write_time e sync_time (pg_stat_bgwriter prima di PostgreSQL 17), come pg_checkpoint_write_time_ms_total e pg_checkpoint_sync_time_ms_total.',
    unit: 'ms per passo',
    window: 'Intervallo di tempo selezionato; campionato ogni 30 s.',
  },
  'overview.chart-calls': {
    what: 'Esecuzioni di statement al secondo, totalizzate sugli statement che pg_stat_statements traccia in ogni database.',
    source:
      'pg_stat_statements.calls degli statement principali che l’agent conserva a ogni raccolta (calls_rate), sommati per raccolta, mediati per passo e sommati sui database.',
    unit: 'chiamate al secondo',
    window: 'Intervallo di tempo selezionato; raccolto ogni 60 s.',
    howToRead:
      'Gli statement fuori dalla lista principale dell’agent non vengono contati: la linea può restare sotto il totale del server.',
  },
  'overview.chart-exec-time': {
    what: 'Millisecondi di tempo di esecuzione usati al secondo dagli statement tracciati di ogni database.',
    source:
      'Tempo totale di esecuzione di pg_stat_statements degli statement principali che l’agent conserva, come tasso al secondo (exec_time_rate_ms).',
    unit: 'ms al secondo',
    window: 'Intervallo di tempo selezionato; raccolto ogni 60 s.',
    howToRead:
      'Gli statement fuori dalla lista principale dell’agent non vengono contati: la linea può restare sotto il totale del server.',
  },
  'overview.chart-rows': {
    what: 'Righe restituite o modificate al secondo dagli statement tracciati di ogni database.',
    source:
      'pg_stat_statements.rows degli statement principali che l’agent conserva, come tasso al secondo (rows_rate).',
    unit: 'righe al secondo',
    window: 'Intervallo di tempo selezionato; raccolto ogni 60 s.',
    howToRead:
      'Gli statement fuori dalla lista principale dell’agent non vengono contati: la linea può restare sotto il totale del server.',
  },
  'overview.chart-statement-reads': {
    what: 'Blocchi condivisi letti al secondo dagli statement tracciati di ogni database: blocchi che non erano negli shared buffers.',
    source:
      'pg_stat_statements.shared_blks_read degli statement principali che l’agent conserva, come tasso al secondo (shared_blks_read_rate).',
    unit: 'blocchi al secondo',
    window: 'Intervallo di tempo selezionato; raccolto ogni 60 s.',
    howToRead:
      'Gli statement fuori dalla lista principale dell’agent non vengono contati: la linea può restare sotto il totale del server.',
  },
  'overview.chart-statement-temp': {
    what: 'Blocchi temporanei scritti al secondo dagli statement tracciati di ogni database.',
    source:
      'pg_stat_statements.temp_blks_written degli statement principali che l’agent conserva, come tasso al secondo (temp_blks_written_rate).',
    unit: 'blocchi al secondo',
    window: 'Intervallo di tempo selezionato; raccolto ogni 60 s.',
    howToRead:
      'Gli statement fuori dalla lista principale dell’agent non vengono contati: la linea può restare sotto il totale del server.',
  },
  'overview.chart-scans': {
    what: 'Scansioni sequenziali e scansioni di indice avviate al secondo sulle tabelle tracciate di ogni database.',
    source:
      'pg_stat_user_tables.seq_scan e idx_scan delle tabelle che l’agent seleziona per dead tuple, dimensione ed età della transazione, come tassi al secondo.',
    unit: 'scansioni al secondo',
    window: 'Intervallo di tempo selezionato; campionato ogni 5 minuti.',
    howToRead: 'Le tabelle che l’agent non traccia non vengono contate.',
  },
  'overview.chart-tuple-churn': {
    what: 'Righe inserite, aggiornate, aggiornate HOT ed eliminate al secondo sulle tabelle tracciate di ogni database.',
    source:
      'pg_stat_user_tables.n_tup_ins, n_tup_upd, n_tup_hot_upd e n_tup_del delle tabelle tracciate dall’agent, come tassi al secondo.',
    unit: 'righe al secondo',
    window: 'Intervallo di tempo selezionato; campionato ogni 5 minuti.',
    howToRead:
      'Anche gli aggiornamenti HOT (nuova versione della riga nella stessa pagina, senza modifiche all’indice) sono contati in Aggiornate.',
  },
  'overview.chart-dead-tuples': {
    what: 'Dead tuple stimate (vecchie versioni lasciate da aggiornamenti ed eliminazioni non ancora rimosse da vacuum), sommate sulle tabelle tracciate di ogni database.',
    source:
      'pg_stat_user_tables.n_dead_tup delle tabelle tracciate dall’agent, sommato per campione.',
    unit: 'righe',
    window: 'Intervallo di tempo selezionato; campionato ogni 5 minuti.',
    howToRead: 'Un livello, non un tasso: una linea in calo significa meno dead tuple di prima.',
  },
  'overview.chart-index-scans': {
    what: 'Scansioni di indice avviate al secondo sugli indici tracciati di ogni database.',
    source:
      'pg_stat_user_indexes.idx_scan degli indici tracciati dall’agent, come tasso al secondo.',
    unit: 'scansioni al secondo',
    window: 'Intervallo di tempo selezionato; campionato ogni 5 minuti.',
    howToRead: 'Gli indici che l’agent non traccia non vengono contati.',
  },
  'overview.chart-index-size': {
    what: 'Dimensione degli indici tracciati di ogni database, sommata.',
    source:
      'pg_relation_size() di ogni indice tracciato dall’agent (controllo index_stats), sommato per campione.',
    unit: 'byte',
    window: 'Intervallo di tempo selezionato; campionato ogni 5 minuti.',
    howToRead: 'Un livello, non un tasso; gli indici che l’agent non traccia non vengono contati.',
  },

  'host.section': {
    what: 'Letture della macchina su cui gira l’agent: CPU, memoria, swap, load average e il filesystem della directory dei dati.',
    source:
      'Letture del sistema operativo raccolte dal collector host dell’agent e inviate come metriche host_*; un limite CPU o di memoria del container sostituisce la cifra dell’host quando l’agent lo trova.',
    window:
      'Riquadri: ultima lettura dell’ultimo giorno; grafici: intervallo di tempo selezionato.',
    howToRead:
      'Non vengono raccolti tassi I/O per dispositivo, latenza del disco e metriche di rete. La raccolta host è locale all’agent, quindi un server remoto non ha dati host.',
  },
  'host.processes': {
    what: 'CPU, memoria e I/O su disco di ogni processo PostgreSQL, uniti alla sua sessione: tipo di processo, database, utente, applicazione, stato e query.',
    source:
      'L’agent legge /proc per il postmaster e i suoi figli e unisce pg_stat_activity per pid; invia solo il pid, le etichette della sessione e i numeri, mai una riga di comando.',
    unit: '%',
    window:
      'La tabella mostra l’ultimo campione (preso ogni 30 s); le viste raggruppate fanno la media sull’intervallo di tempo selezionato. Conservate 7 giorni.',
    howToRead:
      'La CPU è una quota di un core, quindi un valore oltre il 100 % usa più di un core. Lettura e scrittura sono vuote quando l’agent gira con un utente diverso da quello di PostgreSQL.',
  },
  'host.cpu-used': {
    what: 'Quota del tempo CPU in uso dal campione precedente, sul limite CPU del container se impostato, altrimenti su tutto l’host.',
    source:
      'host_cpu_used_ratio: tempi CPU del sistema operativo tra due campioni, oppure l’uso CPU del cgroup rispetto alla sua quota.',
    unit: '%',
    window: 'Ultima lettura dell’ultimo giorno; campionato ogni 30 s.',
    howToRead:
      'Il primo campione dopo l’avvio dell’agent non ha valore. Il solo limite di memoria non lo cambia: il rapporto resta riferito all’intero host.',
  },
  'host.cpu-count': {
    what: 'CPU logiche disponibili all’agent: la quota CPU del container arrotondata per eccesso se impostata, altrimenti il numero dell’host.',
    source:
      'host_cpu_count: numero di CPU logiche del sistema operativo, oppure la quota CPU del cgroup.',
    unit: 'CPU',
    window: 'Ultima lettura dell’ultimo giorno; campionato ogni 30 s.',
  },
  'host.mem-total': {
    what: 'Memoria totale dell’host, o il limite di memoria del container se impostato.',
    source:
      'host_mem_total_bytes: memoria totale del sistema operativo, oppure il limite di memoria del cgroup.',
    unit: 'byte',
    window: 'Ultima lettura dell’ultimo giorno; campionato ogni 30 s.',
  },
  'host.mem-available': {
    what: 'Memoria disponibile per nuovo lavoro senza ricorrere allo swap: il dato del sistema operativo o, in un container, il limite meno l’uso attuale.',
    source:
      'host_mem_available_bytes: memoria disponibile del sistema operativo; in un container il limite meno l’uso, contando la cache dei file inattiva come disponibile.',
    unit: 'byte',
    window: 'Ultima lettura dell’ultimo giorno; campionato ogni 30 s.',
  },
  'host.swap-total': {
    what: 'Spazio di swap totale configurato sull’host.',
    source: 'host_swap_total_bytes: swap totale del sistema operativo.',
    unit: 'byte',
    window: 'Ultima lettura dell’ultimo giorno; campionato ogni 30 s.',
    howToRead: 'Zero: nessuno swap configurato.',
  },
  'host.swap-used': {
    what: 'Spazio di swap in uso sull’host.',
    source: 'host_swap_used_bytes: swap in uso del sistema operativo.',
    unit: 'byte',
    window: 'Ultima lettura dell’ultimo giorno; campionato ogni 30 s.',
  },
  'host.load-average': {
    what: 'Load average di Linux su 1, 5 o 15 minuti: processi pronti più quelli in attesa non interrompibile (di solito I/O su disco), come dice l’etichetta.',
    source: 'host_load1, host_load5 e host_load15: il load average del sistema operativo.',
    unit: 'processi',
    window: 'Ultima lettura dell’ultimo giorno; campionato ogni 30 s.',
    howToRead:
      'Confrontalo con il numero di CPU solo senza limite CPU sul container: il load average riguarda l’host intero; con un limite, le CPU sono la quota arrotondata per eccesso.',
  },
  'host.disk-total': {
    what: 'Dimensione totale del filesystem che contiene la directory dei dati di PostgreSQL.',
    source:
      'host_disk_total_bytes: uso del filesystem del percorso della directory dei dati, che l’agent deve vedere.',
    unit: 'byte',
    window: 'Ultima lettura dell’ultimo giorno; campionato ogni 30 s.',
    howToRead:
      'Non visibile all’agent significa che la directory dei dati non è montata dove l’agent la legge.',
  },
  'host.disk-free': {
    what: 'Spazio libero sul filesystem che contiene la directory dei dati di PostgreSQL.',
    source: 'host_disk_free_bytes: uso del filesystem del percorso della directory dei dati.',
    unit: 'byte',
    window: 'Ultima lettura dell’ultimo giorno; campionato ogni 30 s.',
    howToRead:
      'Non visibile all’agent significa che la directory dei dati non è montata dove l’agent la legge.',
  },
  'host.disk-free-ratio': {
    what: 'Spazio libero come quota del filesystem che contiene la directory dei dati di PostgreSQL.',
    source:
      'host_disk_free_ratio: byte liberi diviso byte totali del filesystem della directory dei dati.',
    unit: '%',
    window: 'Ultima lettura dell’ultimo giorno; campionato ogni 30 s.',
    howToRead:
      'Non visibile all’agent significa che la directory dei dati non è montata dove l’agent la legge.',
  },
  'host.charts': {
    what: 'Letture dell’host nell’intervallo selezionato, ogni grafico con il proprio puntatore e zoom; i bucket nulli restano vuoti.',
    source: 'Metriche host_* del collector host dell’agent; ogni grafico indica la propria fonte.',
    window: 'Intervallo di tempo selezionato; passo di query per grafico.',
  },
  'host.chart-cpu': {
    what: 'Quota del tempo CPU in uso nell’intervallo selezionato, sul limite CPU del container se impostato, altrimenti sull’intero host.',
    source:
      'host_cpu_used_ratio: tempi CPU del sistema operativo tra due campioni, oppure l’uso CPU del cgroup rispetto alla sua quota, mostrato in percentuale.',
    unit: '%',
    window: 'Intervallo di tempo selezionato; campionato ogni 30 s.',
    howToRead: 'L’asse va da 0 a 100%.',
  },
  'host.chart-load': {
    what: 'Load average su 1, 5 e 15 minuti.',
    source: 'host_load1, host_load5 e host_load15: il load average del sistema operativo.',
    unit: 'processi',
    window: 'Intervallo di tempo selezionato; campionato ogni 30 s.',
    howToRead:
      'Un load sopra il numero di CPU delle letture dell’host significa più processi pronti o in attesa di I/O rispetto alle CPU; con un limite CPU del container il numero di CPU è la quota, mentre il load riguarda tutto l’host.',
  },
  'host.chart-memory': {
    what: 'Memoria in uso e memoria totale dell’host, o del container quando ha un limite di memoria.',
    source:
      'host_mem_total_bytes meno host_mem_available_bytes per Usata, e host_mem_total_bytes per Totale.',
    unit: 'byte',
    window: 'Intervallo di tempo selezionato; campionato ogni 30 s.',
    howToRead:
      'Usata è totale meno disponibile, non il contatore di memoria del sistema operativo.',
  },
  'host.chart-swap': {
    what: 'Spazio di swap in uso e spazio di swap totale dell’host.',
    source: 'host_swap_used_bytes e host_swap_total_bytes: swap del sistema operativo.',
    unit: 'byte',
    window: 'Intervallo di tempo selezionato; campionato ogni 30 s.',
    howToRead: 'Senza swap configurato il grafico è sostituito da una nota.',
  },
  'host.chart-filesystem': {
    what: 'Byte usati e quota libera del filesystem che contiene la directory dei dati di PostgreSQL.',
    source:
      'host_disk_total_bytes meno host_disk_free_bytes per Usato, e host_disk_free_ratio per Libero, mostrato in percentuale sul secondo asse.',
    unit: 'byte; quota libera in% sul secondo asse',
    window: 'Intervallo di tempo selezionato; campionato ogni 30 s.',
    howToRead: 'Il grafico è sostituito da una nota quando l’agent non vede la directory dei dati.',
  },

  'catalog.section': {
    what: 'Oggetti dello schema di ogni database: trigger, sequenze, partizioni, funzioni, security definer, chiavi esterne senza indice, estensioni e colonne spaziali.',
    source:
      'Controlli di catalogo dell’agent: trigger_inventory, sequences, partitions, function_stats, function_security, fk_indexes, extensions e postgis_columns.',
    window: 'Ultimo risultato di ogni controllo; girano ogni 5 minuti o ogni ora.',
    howToRead:
      'Ogni scheda elenca un tipo di oggetto; il badge con il conteggio su una scheda è il numero di elementi da controllare.',
  },
  'catalog.triggers': {
    what: 'Trigger utente sulle tabelle utente di ogni database, più i trigger interni di vincolo solo se disabilitati.',
    source:
      'pg_trigger unito a pg_class, pg_proc e pg_constraint (controllo trigger_inventory); fino a 1000 per database, prima quelli disabilitati.',
    unit: 'trigger',
    window: 'Ultimo risultato del controllo; gira ogni 5 minuti.',
    howToRead:
      'Un trigger interno disabilitato significa che la sua chiave esterna o il suo vincolo non è più applicato. Un trigger ereditato da una partizione è elencato solo se il suo stato differisce da quello del genitore.',
  },
  'catalog.sequences': {
    what: 'Sequenze di ogni database con la colonna che ciascuna alimenta, l’ultimo valore assegnato e quanta parte del loro intervallo di valori è usata.',
    source:
      'Cataloghi pg_sequence e pg_class con l’ultimo valore di ogni sequenza e il tipo intero della colonna che alimenta (controllo sequences); fino a 500 per database, prima quelle più vicine al limite.',
    unit: 'valori; quota usata in%',
    window: 'Ultimo risultato del controllo; gira ogni 10 minuti.',
    howToRead:
      'Usata diventa un badge quando una sequenza che può esaurirsi raggiunge la quota di avviso. Una sequenza ciclica è a rischio solo se la sua colonna la limita.',
    thresholds: 'Avviso da 75% usato e critico da 90% usato, come nell’advisor.',
  },
  'catalog.partitions': {
    what: 'Tabelle partizionate di ogni database con l’intero albero di partizioni riassunto: partizioni, dimensione e righe stimate, partizione predefinita e copertura.',
    source:
      'Alberi di partizioni esplorati tramite pg_inherits; dimensioni e righe stimate da relpages e reltuples all’ultimo vacuum o analyze (controllo partitions); fino a 200 alberi, prima i più grandi.',
    unit: 'partizioni; byte e righe sono stimati',
    window: 'Ultimo risultato del controllo; gira ogni 10 minuti.',
    howToRead:
      'Copertura è la fine dell’ultimo intervallo di date coperto da una partizione: segnata come in scadenza entro 7 giorni e come terminata una volta passata.',
    thresholds:
      'Da controllare: la partizione predefinita contiene righe, un indice partizionato non è valido, o la copertura finisce entro 7 giorni (o è terminata negli ultimi 30 giorni).',
  },
  'catalog.functions': {
    what: 'Funzioni che track_functions conta: chiamate al secondo, tempo nel proprio corpo, tempo totale e tempo medio per chiamata.',
    source:
      'Contatori pg_stat_user_functions (controllo function_stats) convertiti in tassi al secondo; sono elencate le 200 più attive per tempo proprio e chiamate.',
    unit: 'chiamate al secondo; ms al secondo; ms per chiamata',
    window: 'Tassi mediati sull’ultima ora; il controllo gira ogni 5 minuti.',
    howToRead:
      'Il tempo proprio esclude le funzioni chiamate da una funzione, il tempo totale le include. Con track_functions impostato su none non viene contata alcuna funzione.',
  },
  'catalog.security-definer': {
    what: 'Funzioni e procedure dichiarate security definer, che girano con i privilegi del proprietario, con proprietario, search_path fissato e chi può eseguirle.',
    source:
      'Righe di pg_proc con prosecdef, il proprietario, il search_path in proconfig e la lista espansa degli accessi, escluse le estensioni e gli schemi di sistema (controllo function_security); fino a 200 per database, prima quelle senza search_path fissato.',
    unit: 'funzioni',
    window: 'Ultimo risultato del controllo; gira ogni 15 minuti.',
    howToRead:
      'Una funzione senza search_path fissato risolve i nomi tramite il search_path di chi la chiama.',
    thresholds:
      'Segnata come critica quando la funzione non ha search_path fissato, il suo proprietario è un superuser e ogni ruolo può eseguirla.',
  },
  'catalog.foreign-keys': {
    what: 'Chiavi esterne senza un indice valido che inizi con le loro colonne: modificare una riga referenziata scansiona l’intera tabella che la referenzia.',
    source:
      'Chiavi esterne di pg_constraint verificate contro pg_index per un indice btree o hash valido, non parziale, che inizi con le loro colonne (controllo fk_indexes); fino a 200 per database, prima le tabelle più grandi.',
    unit: 'chiavi esterne; dimensione tabella in byte (stimata)',
    window: 'Ultimo risultato del controllo; gira ogni 15 minuti.',
    howToRead:
      'La dimensione della tabella è stimata da relpages all’ultimo vacuum o analyze. Una chiave esterna ereditata da una partizione non è elencata da sola.',
  },
  'catalog.extensions': {
    what: 'Estensioni installate in ogni database, la loro versione installata e la versione predefinita disponibile nei pacchetti installati.',
    source:
      'pg_extension unito a pg_available_extensions() (controllo extensions); fino a 200 per database.',
    unit: 'estensioni',
    window: 'Ultimo risultato del controllo; gira ogni ora.',
    howToRead:
      'Aggiornamento mostra disponibile quando la versione predefinita dei pacchetti è più recente di quella installata. pglens non aggiorna mai un’estensione installata.',
  },
  'catalog.spatial-columns': {
    what: 'Colonne geometry e geography di ogni database, con tipo, sistema di coordinate e indice che serve le ricerche spaziali.',
    source:
      'Colonne di pg_attribute di tipo definito nello schema postgis con il metodo di accesso di un indice GiST, GiST partizionato nello spazio o BRIN valido, non parziale, che inizi con loro (controllo postgis_columns); fino a 500 per database, prima quelle senza indice.',
    unit: 'colonne; le righe sono stimate',
    window: 'Ultimo risultato del controllo; gira ogni ora.',
    howToRead:
      'Mancante: nessun indice valido di questo tipo inizia con la colonna. Un sistema di coordinate unknown o undefined non ne indica uno registrato.',
  },

  'internals.section': {
    what: 'Componenti interne del server riportate dall’agent: il WAL in pg_wal, le cache SLRU, i file temporanei e, su uno standby, il replay del WAL.',
    source:
      'Controlli wal_directory, database_size, slru, database_stats e recovery, letti dalle metriche memorizzate dell’istanza.',
    window: 'I tassi sono mediati sull’ultima ora.',
  },
  'internals.wal-directory': {
    what: 'Spazio su disco usato dalla directory pg_wal: segmenti WAL, segmenti parziali e file di cronologia.',
    source:
      'Dimensione totale e numero di file di pg_ls_waldir() (controllo wal_directory, pg_wal_dir_bytes e pg_wal_dir_files); richiede il ruolo pg_monitor o una concessione esplicita.',
    unit: 'byte',
    window: 'Ultimo valore dell’ultima ora; il controllo gira ogni minuto.',
    howToRead:
      'Può superare max_wal_size quando il WAL viene trattenuto; i punti da controllare sono gli slot di replica, l’archiviazione e wal_keep_size.',
  },
  'internals.wal-replay': {
    what: 'Se questo standby sta applicando il WAL che riceve o ha messo in pausa il replay.',
    source:
      'pg_get_wal_replay_pause_state() letta dal controllo recovery (pg_recovery_replay_paused); mostrato solo su un server in recovery.',
    window: 'Ultimo valore dell’ultima ora; il controllo gira ogni 30 s.',
    howToRead:
      'In pausa significa che il WAL ricevuto non viene applicato finché il replay non riprende.',
  },
  'internals.database-sizes': {
    what: 'Dimensione su disco di ogni database e quanto è cresciuta nelle ultime 24 ore e negli ultimi 7 giorni.',
    source:
      'pg_database_size() di ogni database (controllo database_size, pg_database_size_bytes); la crescita confronta con la dimensione misurata più vicina a 24 ore e 7 giorni prima, entro due ore.',
    unit: 'byte',
    window: 'Ultima dimensione misurata nelle ultime 2 ore; il controllo gira ogni 15 minuti.',
    howToRead: 'Trattino in una colonna di crescita: nessuna misura vicina a quel momento.',
  },
  'internals.slru': {
    what: 'Rapporto di hit e attività di ogni cache SLRU, le piccole cache di memoria dietro lo stato delle transazioni e i lock di riga.',
    source:
      'Contatori pg_stat_slru (controllo slru) come tassi al secondo mediati sull’ultima ora; il rapporto di hit è gli hit diviso gli hit più le letture.',
    unit: '% rapporto di hit; blocchi ed eventi al secondo',
    window: 'Ultima ora; il controllo gira ogni minuto.',
    howToRead:
      'Una lettura che manca la cache va su disco sotto il lock della cache e blocca ogni sessione che ne ha bisogno: tieni d’occhio le cache subtransaction e multixact. Le cache contengono lo stato delle transazioni, i gruppi di lock di riga (multixact), le subtransaction e l’isolamento serializable.',
  },
  'internals.temp-files': {
    what: 'Database le cui query hanno riversato dati nei file temporanei: file creati al minuto e byte scritti al secondo.',
    source:
      'pg_stat_database.temp_files e temp_bytes (controllo database_stats) come tassi mediati sull’ultima ora; i database con media zero sono esclusi.',
    unit: 'file al minuto; byte al secondo',
    window: 'Ultima ora; il controllo gira ogni 30 s.',
  },
  'internals.recovery-conflicts': {
    what: 'Query annullate su questo standby perché il replay del WAL andava in conflitto con loro, per database e motivo.',
    source:
      'pg_stat_database_conflicts (controllo recovery, pg_recovery_conflicts_total) come tasso mediato sull’ultima ora.',
    unit: 'query al minuto',
    window: 'Ultima ora; il controllo gira ogni 30 s.',
    howToRead: 'Elenco vuoto: il replay non ha annullato query nell’ultima ora.',
  },

  'logical.section': {
    what: 'Replica logica su questa istanza: le subscription che applica e gli slot logici per cui decodifica.',
    source:
      'Controllo logical_replication: pg_subscription, pg_stat_subscription, pg_stat_subscription_stats e pg_stat_replication_slots; le stringhe di connessione non vengono mai lette.',
    window: 'Subscription: ultimo risultato del controllo; slot: tassi mediati sull’ultima ora.',
  },
  'logical.subscriptions': {
    what: 'Subscription di replica logica applicate da questa istanza: stato, publication, slot, ultimo messaggio dal publisher, conteggi di errori e conflitti.',
    source:
      'pg_subscription (mai la stringa di connessione), pg_stat_subscription e pg_stat_subscription_stats (controllo logical_replication), memorizzati come fatti di subscription.',
    unit: 'subscription; errori e conflitti contati dal reset delle statistiche',
    window: 'Ultimo risultato del controllo; gira ogni minuto.',
    howToRead:
      'Lo stato è disabilitato, non in esecuzione (nessun apply worker), silenzioso (nessun messaggio dal publisher da 5 minuti o più) o in esecuzione. I conflitti sono disponibili da PostgreSQL 18.',
    thresholds: 'Silenzioso significa che l’ultimo messaggio ha 5 minuti o più, come nell’advisor.',
  },
  'logical.slots': {
    what: 'Decodifica di ogni slot logico su questa istanza: byte decodificati, transazioni, byte riversati su disco e byte trasmessi al secondo.',
    source:
      'pg_stat_replication_slots (controllo logical_replication), differenze dei contatori come tassi al secondo mediati sull’ultima ora.',
    unit: 'byte al secondo; transazioni al secondo',
    window: 'Ultima ora; il controllo gira ogni minuto.',
    thresholds:
      'Un tasso di riversamento di 1 MiB al secondo o più viene segnalato, come nell’advisor.',
  },
  'instance.settings': {
    what: 'Impostazioni di PostgreSQL lette dal server: prima le cinque di durabilità, poi quelle cambiate nell’intervallo selezionato.',
    source:
      'pg_settings letto dal controllo settings (name, setting, unit, source, context, pending_restart), memorizzato come fatti di impostazione con l’ora dell’ultima modifica.',
    unit: 'impostazioni',
    window:
      'Impostazioni di durabilità: ultima raccolta; tabella modifiche: impostazioni cambiate dalla data scelta, per impostazione predefinita dall’inizio dell’intervallo.',
    howToRead:
      'Un’impostazione di durabilità che indebolisce la sicurezza in caso di crash ha badge critico o di avviso; la sua ⓘ spiega il valore.',
  },
  'instance.settings-changes': {
    what: 'Impostazioni il cui valore è cambiato dalla data scelta, con dove è stato impostato, quando la modifica ha effetto e se un riavvio è in attesa.',
    source:
      'Fatti pg_settings dell’istanza (controllo settings): name, value, unit, source, context, pending_restart e l’ora dell’ultima modifica del valore.',
    window:
      'Modifiche dalla data nel campo Modificati dal; il valore mostrato è l’ultima raccolta. Usa intervallo selezionato riporta la data all’inizio dell’intervallo.',
    howToRead:
      'Riavvio in attesa significa che la configurazione contiene un nuovo valore che solo un riavvio rende attivo. Cerca e filtri non modificano alcuna impostazione.',
  },

  'relations.section': {
    what: 'Tabelle, indici e bloat stimato di ogni database monitorato, come riportati l’ultima volta dall’agent; una scheda per elenco.',
    source:
      'Controlli dell’agent table_stats, index_stats e bloat_estimate, letti dalle metriche delle relazioni memorizzate per l’istanza.',
    unit: 'righe, scansioni al secondo e byte',
    window:
      'Tabelle e indici: report degli ultimi 15 minuti. Stime di bloat: le ultime 13 ore; misure esatte: gli ultimi 7 giorni.',
    howToRead:
      'Ogni elenco è limitato dal budget dell’agent del suo controllo (checks.table_stats.top_n, checks.index_stats.top_n, checks.bloat_estimate.top_n), quindi le relazioni oltre quel budget non vengono riportate.',
  },
  'relations.tables': {
    what: 'Ogni tabella dei database monitorati con le sue righe vive e dead tuple, quante volte viene letta per intero e l’ultima esecuzione di vacuum e analyze.',
    source:
      'pg_stat_user_tables (controllo table_stats): n_live_tup, n_dead_tup, seq_scan e idx_scan come tassi, orari dell’ultimo vacuum e analyze; dimensione da pg_total_relation_size.',
    unit: 'righe; scansioni al secondo; byte',
    window: 'Report degli ultimi 15 minuti; il controllo gira ogni 5 minuti.',
    howToRead:
      'Il rapporto dead è dead tuple diviso righe vive. Le colonne di scansione sono tassi sull’ultimo intervallo di raccolta, non totali. L’agent riporta solo le tabelle principali di ogni database (checks.table_stats.top_n, 50 per impostazione predefinita).',
  },
  'relations.indexes': {
    what: 'Ogni indice dei database monitorati: quante volte viene usato, quando è stato usato l’ultima volta, dimensione, e se è unico, di chiave primaria e valido.',
    source:
      'pg_stat_user_indexes e pg_index (controllo index_stats): idx_scan come tasso, last_idx_scan (PostgreSQL 16 e successivi), pg_relation_size, indisunique, indisprimary e indisvalid.',
    unit: 'scansioni al secondo; byte',
    window: 'Report degli ultimi 15 minuti; il controllo gira ogni 5 minuti.',
    howToRead:
      'La colonna Relazione indica la tabella a cui appartiene l’indice. Le stime di bloat vengono da un altro controllo con una finestra più lunga, quindi possono elencare indici mentre questa scheda è vuota.',
  },
  'relations.table-bloat': {
    what: 'Spazio sprecato stimato in ogni tabella, occupato da versioni di riga eliminate o vecchie, che una ricostruzione restituirebbe.',
    source:
      'Controllo bloat_estimate: dimensione reale da pg_relation_size confrontata con una dimensione attesa calcolata da conteggi di righe e statistiche di colonna (pg_stats); le righe di pgstattuple sono esatte.',
    unit: 'byte; rapporto di bloat in%',
    window:
      'Stima più recente delle ultime 13 ore, aggiornata ogni 6 ore; una misura esatta viene conservata per 7 giorni.',
    howToRead:
      'Stimato, non misurato. Le tabelle senza statistiche di colonna (mai analizzate) e le tabelle che il ruolo di monitoraggio non può leggere per intero non vengono stimate; Misura esattamente esegue pgstattuple su una tabella.',
  },
  'relations.autovacuum': {
    what: 'Le tabelle su cui autovacuum e autoanalyze hanno speso più tempo, con le loro esecuzioni, le righe rimosse e le esecuzioni per prevenire il wraparound.',
    source:
      'Righe di log di autovacuum che l’agent legge dal log di PostgreSQL (controllo logs), memorizzate per esecuzione in autovacuum_runs.',
    unit: 'esecuzioni, secondi e righe',
    window: 'Gli ultimi 7 giorni; le esecuzioni restano 90 giorni.',
    howToRead:
      'Vanno nel log solo le esecuzioni più lunghe di log_autovacuum_min_duration. Apri una riga per le esecuzioni di quella tabella.',
  },
  'relations.autovacuum-runs': {
    what: 'Ogni esecuzione di autovacuum o autoanalyze di una tabella, dalla più recente.',
    source: 'La riga di log di autovacuum dell’esecuzione, letta dall’agent (controllo logs).',
    unit: 'secondi, righe, pagine e byte',
    window: 'Gli ultimi 7 giorni.',
    howToRead:
      'Le dead tuple non rimovibili sono righe ancora visibili a una transazione aperta; i byte WAL sono quanto ha scritto l’esecuzione. Per prevenire il wraparound indica un’esecuzione che PostgreSQL ha forzato per congelare vecchi id di transazione.',
  },
  'relations.index-bloat': {
    what: 'Spazio sprecato stimato in ogni indice btree, occupato da voci eliminate o vecchie, che ricostruire l’indice restituirebbe.',
    source:
      'Controllo bloat_estimate: dimensione reale da pg_relation_size confrontata con una dimensione attesa calcolata da conteggi di righe e statistiche di colonna (pg_stats).',
    unit: 'byte; rapporto di bloat in%',
    window: 'Stima più recente delle ultime 13 ore, aggiornata ogni 6 ore.',
    howToRead:
      'Stimato, non misurato. Vengono stimati solo gli indici btree validi, non parziali, senza espressione e posti su colonne con statistiche.',
  },
}
