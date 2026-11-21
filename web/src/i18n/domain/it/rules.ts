import type { NeedName, RuleTextId } from '@/lib/ruleText'

export const rules: Readonly<Record<RuleTextId, string>> = {
  'archive.disabled':
    'L’archiviazione del WAL è disattivata, quindi i backup non si possono ripristinare a un momento preciso.',
  'archive.failing':
    'L’archiviazione del WAL non funziona: i file WAL completati non vengono copiati.',
  'archive.stalled':
    'Da molto tempo non viene archiviato alcun file WAL, anche se il server scrive WAL.',
  'backup.no_basebackup_seen': 'Per questo cluster non risulta alcun base backup.',
  'backup.no_strategy':
    'Non si vede alcuna strategia di backup: il WAL non è archiviato e non risulta alcun base backup.',
  'bloat.estimate_blind':
    'La stima dello spazio sprecato non si può calcolare per alcune tabelle o alcuni indici.',
  'config.checkpoints_too_frequent':
    'Più del 20% dei checkpoint viene richiesto prima dell’orario previsto.',
  'config.drift': 'La stessa impostazione ha valori diversi sulle istanze di questo cluster.',
  'config.effective_cache_size_mismatch':
    'effective_cache_size è molto distante dalla memoria del server.',
  'config.fsync_off': 'fsync è disattivato: un crash può corrompere i dati.',
  'config.full_page_writes_off':
    'full_page_writes è disattivato: un crash può lasciare pagine scritte solo in parte e corrompere i dati.',
  'config.maintenance_work_mem_low':
    'maintenance_work_mem è sotto 256 MiB su un host con molta memoria.',
  'config.max_connections_high':
    'max_connections è molto più alto delle connessioni effettivamente in uso.',
  'config.shared_buffers_high': 'shared_buffers supera il 40% della memoria utilizzabile.',
  'config.shared_buffers_low': 'shared_buffers è piccolo rispetto alla memoria del server.',
  'config.track_io_timing_off':
    'track_io_timing è disattivato: i tempi di lettura e scrittura non vengono misurati.',
  'config.wal_keep_size_low':
    'wal_keep_size potrebbe essere troppo piccolo per conservare il WAL di uno standby che resta indietro.',
  'config.work_mem_low': 'work_mem è piccolo e gli statement scrivono file temporanei.',
  'config.work_mem_oversized': 'work_mem per max_connections è una quota grande della memoria.',
  'conn.idle_share_high': 'Una quota elevata delle connessioni è inattiva.',
  'conn.saturation': 'La maggior parte delle connessioni consentite da max_connections è in uso.',
  'extension.upgrade_available':
    'Un’estensione installata ha una versione più recente disponibile.',
  'function.secdef_no_search_path': 'Una funzione SECURITY DEFINER non ha un search_path fisso.',
  'function.untracked':
    'Le chiamate di funzione non vengono tracciate, quindi le statistiche delle funzioni sono vuote.',
  'index.bloat_high': 'Un indice occupa molto spazio che una ricostruzione recupererebbe.',
  'index.divergence': 'La stessa tabella ha indici diversi sulle istanze di questo cluster.',
  'index.duplicate': 'Due o più indici hanno la stessa definizione.',
  'index.fk_unindexed': 'Una chiave esterna non ha un indice sulle sue colonne.',
  'index.invalid': 'Un indice è marcato come non valido, quindi le query non lo usano.',
  'index.redundant_prefix': 'Un indice ripete le colonne iniziali di un altro indice.',
  'index.unused': 'Un indice non ha avuto scansioni nello storico osservato.',
  'lock.long_wait': 'Una sessione ha atteso a lungo un lock.',
  'logical_slot.spill_high': 'Uno slot logico scrive su disco molte modifiche decodificate.',
  'partition.default_has_rows':
    'La partizione di default contiene righe che non corrispondono a nessun’altra partizione.',
  'partition.future_missing':
    'Una tabella partizionata non ha partizioni pronte per i dati futuri.',
  'partition.index_invalid': 'Un indice su una tabella partizionata non è valido.',
  'partition.parent_not_analyzed':
    'Una tabella partizionata non è mai stata sottoposta ad analyze.',
  'postgis.spatial_unindexed': 'Una colonna geometry di PostGIS non ha un indice spaziale.',
  'postgis.srid_mixed': 'Le colonne geometry usano più di un sistema di coordinate.',
  'postgis.srid_unknown': 'Una colonna geometry non ha un sistema di coordinate dichiarato.',
  'postgis.upgrade_available': 'L’estensione PostGIS è più vecchia della libreria installata.',
  'query.cache_miss_high':
    'Uno statement frequente legge la maggior parte delle sue pagine dal disco, non dalla memoria.',
  'query.regression': 'Uno statement è più lento del proprio storico degli ultimi sette giorni.',
  'query.slow_mean': 'Uno statement ha un tempo medio di esecuzione elevato.',
  'query.temp_bytes_high': 'Uno statement scrive molti file temporanei.',
  'query.total_time_share':
    'Uno statement assorbe una quota grande del tempo totale di esecuzione.',
  'security.monitoring_role_can_write': 'Il ruolo di monitoraggio può scrivere dati.',
  'security.monitoring_role_read_only_off':
    'Il ruolo di monitoraggio non imposta di default transazioni di sola lettura.',
  'sequence.column_narrower':
    'Una sequenza può contare più in alto della colonna che ne memorizza i valori.',
  'sequence.exhaustion_risk': 'Una sequenza è vicina al suo valore massimo.',
  'sequence.value_hidden': 'pglens non può leggere il valore corrente di una sequenza.',
  'standby.recovery_conflicts': 'Le query su uno standby vengono annullate dal replay del WAL.',
  'standby.replay_paused': 'Il replay del WAL è in pausa su uno standby.',
  'subscription.disabled': 'Una sottoscrizione di replica logica è disattivata.',
  'subscription.errors': 'Una sottoscrizione di replica logica segnala errori.',
  'subscription.not_receiving': 'Una sottoscrizione attiva non riceve modifiche.',
  'table.autoanalyze_stale':
    'Più del 20% delle righe di una tabella è cambiato dall’ultimo analyze.',
  'table.bloat_high': 'Una tabella occupa molto spazio che una ricostruzione recupererebbe.',
  'table.dead_tuples_high': 'Una tabella ha molte dead tuple che il vacuum non ha ancora rimosso.',
  'table.never_autovacuumed': 'Una tabella grande non è mai stata sottoposta a vacuum.',
  'table.seq_scan_heavy':
    'Una tabella grande viene letta per lo più per intero, non tramite un indice.',
  'table.wraparound_risk':
    'Una tabella contiene ID di transazione vecchi: serve un vacuum di congelamento.',
  'trigger.constraint_disabled': 'I trigger che applicano un vincolo sono disattivati.',
  'trigger.user_disabled': 'Un trigger utente è disattivato.',
  'txn.high_rollback_ratio': 'Una quota grande delle transazioni termina con un rollback.',
  'txn.idle_in_transaction':
    'Una sessione è rimasta inattiva dentro una transazione aperta per molto tempo.',
  'txn.long_running': 'Una transazione è aperta da molto tempo.',
  'txn.prepared_orphan': 'Una transazione preparata è rimasta aperta per molto tempo.',
  'txn.wraparound_risk':
    'L’ID di transazione più vecchio di un database sta invecchiando: serve un vacuum di congelamento.',
  'vacuum.disabled': 'Il vacuum automatico (autovacuum) è disattivato.',
  'vacuum.starvation':
    'Molte tabelle hanno dead tuple mentre tutti i worker del vacuum sono occupati.',
  'wal.directory_large':
    'La directory del WAL è più grande di quanto spiegano le impostazioni del WAL.',
  agent_down: 'Non è arrivato alcun report dall’agent di un’istanza.',
  instance_unreachable:
    'Il server non ha ricevuto report da un’istanza entro l’intervallo previsto.',
  check_failing: 'Un controllo fallisce da oltre cinque minuti.',
  no_primary_in_cluster: 'Nessuna istanza del cluster segnala il ruolo di primario.',
  agent_buffer_full: 'L’agent scarta campioni perché il suo buffer è pieno.',
  clock_skew: 'L’orologio dell’agent differisce da quello del server di più di 30 secondi.',
  cardinality_budget_exceeded:
    'Un’istanza ha inviato più serie distinte del suo limite, quindi alcune sono state scartate.',
  failover_detected: 'È stato rilevato un failover: un’altra istanza è diventata primario.',
  split_brain_detected: 'Più di un’istanza del cluster segnala il ruolo di primario.',
  slot_inactive: 'Un replication slot è inattivo e trattiene il WAL sul primario.',
  slot_lost: 'Un replication slot ha perso WAL che il suo consumatore richiedeva ancora.',
  'replica.lag_high': 'Uno standby è indietro di più di 30 secondi rispetto al suo primario.',
  'replica.all_standbys_lagging': 'Ogni standby del cluster è indietro di più di 30 secondi.',
  'replica.no_sync_standby': 'Il cluster non ha uno standby sincrono.',
  'replica.redundancy_reduced':
    'Sono in streaming meno standby dei membri del cluster: uno è giù, disconnesso o non ancora rientrato; un membro silenzioso da oltre un’ora non viene più contato.',
  'conn.near_max': 'Più dell’80% delle connessioni consentite da max_connections è in uso.',
  'conn.idle_in_transaction': 'Una sessione è inattiva in una transazione da oltre 5 minuti.',
  'db.deadlock_rate': 'Si stanno verificando deadlock.',
  'disk.free_low': 'Meno del 15% del disco dei dati è libero.',
}

export const needs: Readonly<Record<NeedName, string>> = {
  Baseline: 'storico delle query di sette giorni',
  Bloat: 'stime dello spazio sprecato',
  Indexes: 'statistiche degli indici',
  LockTree: 'attese sui lock',
  Replication: 'stato della replica',
  Siblings: 'altre istanze del cluster',
  Statements: 'statistiche delle query',
  Tables: 'statistiche delle tabelle',
  extensions: 'estensioni installate',
  fk_indexes: 'indici delle chiavi esterne',
  function_security: 'impostazioni di sicurezza delle funzioni',
  function_stats: 'statistiche delle chiamate di funzione',
  function_tracking: 'impostazione di tracciamento delle funzioni',
  fk_unindexed: 'indici delle chiavi esterne',
  extension: 'estensioni installate',
  host: 'metriche dell’host',
  logical_replication: 'stato della replica logica',
  monitoring_role: 'impostazioni del ruolo di monitoraggio',
  partitioned_table: 'tabelle partizionate',
  partitions: 'tabelle partizionate',
  pg_connections_idle_share: 'quota di connessioni inattive',
  pg_connections_used: 'connessioni in uso',
  pg_connections_used_ratio: 'quota di connessioni in uso',
  pg_vacuum_jobs_running: 'job di vacuum in esecuzione',
  pg_xact_rollback_ratio: 'quota di transazioni con rollback',
  postgis_columns: 'colonne PostGIS',
  'query.temp_bytes_high': 'uso di file temporanei di ogni statement',
  recovery: 'stato di recovery dello standby',
  security_definer_function: 'funzioni SECURITY DEFINER',
  sequence: 'sequenze',
  sequences: 'sequenze',
  settings: 'impostazioni del server',
  spatial_column: 'colonne PostGIS',
  subscription: 'sottoscrizioni di replica logica',
  trigger: 'trigger',
  trigger_inventory: 'trigger e vincoli',
  wal_directory: 'dimensione della directory del WAL',
  xmin_horizon: 'transazione più vecchia che trattiene il vacuum',
}

export const noNeeds = 'Nessuno'
