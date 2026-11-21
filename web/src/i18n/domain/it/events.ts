import type { DomainTexts } from '../../domainTypes'

export const events: DomainTexts['events'] = {
  failover_detected: {
    title: 'Failover rilevato',
    whatToCheck: 'Verifica se la promozione era pianificata o causata da un guasto del primario.',
  },
  split_brain_detected: {
    title: 'Split brain rilevato',
    whatToCheck: 'Individua il primario autorevole e retrocedi l’altro primario.',
  },
  orphan_standby: {
    title: 'Standby orfano',
    whatToCheck: 'Controlla il primario a monte e, se serve, promuovi o ricollega lo standby.',
  },
  role_change: {
    title: 'Ruolo cambiato',
    whatToCheck: 'Controlla il nuovo ruolo dell’istanza e conferma che il cambio era previsto.',
  },
  slot_inactive: {
    title: 'Slot di replica inattivo',
    whatToCheck: 'Verifica se lo standby è fermo o scollegato e va ricollegato.',
  },
  slot_lost: {
    title: 'Slot di replica senza WAL',
    whatToCheck:
      'Chi consuma questo slot non può più riprendere da lì. Ricostruisci lo standby fisico, oppure ricrea lo slot logico e risincronizza il suo subscriber, poi dimensiona max_slot_wal_keep_size perché lo slot non si perda di nuovo.',
  },
  no_primary_in_cluster: {
    title: 'Nessun primario nel cluster',
    whatToCheck: 'Indaga perché nessuna istanza è primaria e, se serve, promuovi uno standby.',
  },
  counter_reset_detected: {
    title: 'Azzeramento dei contatori rilevato',
    whatToCheck:
      'Cerca un pg_stat_reset() o un riavvio dell’istanza; di solito è un evento previsto.',
  },
  agent_down: {
    title: 'Agent non raggiungibile',
    whatToCheck:
      'Controlla i log dell’agent, il container, la raggiungibilità di rete e il server.',
  },
  agent_up: {
    title: 'Agent ripristinato',
    whatToCheck: 'Conferma che l’istanza funziona normalmente dopo il periodo senza report.',
  },
  duplicate_instance_suspected: {
    title: 'Possibile istanza duplicata',
    whatToCheck:
      'Verifica la memorizzazione persistente dell’identità, perché l’agent mantenga il suo instance_id.',
  },
  cluster_id_changed: {
    title: 'Identità del cluster cambiata',
    whatToCheck: 'Indaga l’invariante I-1 e controlla il system_identifier e i log dell’agent.',
  },
  instance_unreachable: {
    title: 'Istanza non raggiungibile',
    whatToCheck:
      'Il server non riceve più report dall’istanza. Controlla il suo agent e il percorso di rete.',
  },
  instance_reachable: {
    title: 'Istanza raggiungibile',
    whatToCheck: 'Conferma che l’istanza funziona normalmente dopo il periodo senza report.',
  },
  cardinality_truncated: {
    title: 'Serie troncate dal budget di cardinalità',
    whatToCheck:
      'Il payload indica il check e il database. Riduci gli oggetti che riporta o rivedi il suo budget.',
  },
  cluster_identity_conflict: {
    title: 'Conflitto di identità del cluster',
    whatToCheck:
      'L’istanza ha riportato il cluster_name configurato, non il suo system identifier. Applica monitoring_user.sql, poi allinea cluster_name.',
  },
  lock_wait_captured: {
    title: 'Attesa di lock catturata',
    whatToCheck:
      'Il payload indica la tabella, la riga (ctid) e le sessioni bloccanti. Termina il bloccante radice solo se il suo lavoro si può perdere.',
  },
  deadlock_captured: {
    title: 'Deadlock catturato',
    whatToCheck:
      'PostgreSQL ha già annullato una transazione. Leggi le sessioni e gli statement nel payload, poi fai in modo che ogni transazione blocchi le righe nello stesso ordine.',
  },
  long_query_captured: {
    title: 'Query lunga catturata',
    whatToCheck:
      'Il payload contiene lo statement e da quanto è in esecuzione. Controlla il suo piano, o annullalo se non deve finire.',
  },
  idle_in_transaction_captured: {
    title: 'Idle in transaction catturato',
    whatToCheck:
      'Una sessione ha lasciato una transazione aperta e trattiene i suoi lock. Correggi l’applicazione che non fa commit, o imposta idle_in_transaction_session_timeout.',
  },
  long_transaction_captured: {
    title: 'Transazione lunga catturata',
    whatToCheck:
      'Una transazione resta aperta, trattiene lock e blocca vacuum. Il payload indica la sessione, le sue tabelle e la sua età xmin.',
  },
  unknown: {
    title: 'Tipo di evento sconosciuto',
    whatToCheck: 'Esamina il tipo e il payload grezzi dell’evento prima di agire.',
  },
}
