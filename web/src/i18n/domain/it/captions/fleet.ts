import type { CaptionEntry } from '@/lib/captions'
import type { fleetCaptions as english } from '@/lib/captionsets/fleet'

export const fleet: Readonly<Record<keyof typeof english, CaptionEntry>> = {
  'fleet.summary-all': {
    what: 'Numero di cluster monitorati nella flotta, a prescindere dalla salute; selezionare la card rimuove il filtro sulla salute.',
    source:
      'La lista dei cluster (handleClusters in internal/server/api.go), contata nel browser; un cluster senza istanze non compare.',
    unit: 'cluster',
    window: 'Lista più recente; aggiornata ogni 15 s.',
  },
  'fleet.summary-healthy': {
    what: 'Cluster con health ok: esattamente un primario, ogni istanza attiva e ritardo di replay pari o inferiore a 30 secondi.',
    source:
      'Il campo health che il server calcola per ogni cluster (healthForCluster in internal/server/api.go), contato nel browser.',
    unit: 'cluster',
    window: 'Lista più recente; aggiornata ogni 15 s.',
    howToRead:
      'Selezionare la card elenca solo questi cluster; selezionarla di nuovo rimuove il filtro. Un cluster senza lettura di lag non viene penalizzato.',
    thresholds: 'Un ritardo di replay sopra 30 secondi non è più ok.',
  },
  'fleet.summary-degraded': {
    what: 'Cluster con un solo primario in cui un’istanza è non raggiungibile o il ritardo di replay massimo tra gli standby supera 30 secondi.',
    source:
      'Il campo health che il server calcola per ogni cluster (healthForCluster in internal/server/api.go), contato nel browser.',
    unit: 'cluster',
    window: 'Lista più recente; aggiornata ogni 15 s.',
    howToRead:
      'Apri la card del cluster degradato: mostra quale istanza è non raggiungibile e quale ritardo.',
    thresholds: 'Non raggiungibile: nessun invio da 60 secondi. Soglia del ritardo: 30 secondi.',
  },
  'fleet.summary-critical': {
    what: 'Cluster senza primario o con più di un primario (split brain).',
    source:
      'Il campo health che il server calcola per ogni cluster (healthForCluster in internal/server/api.go), contato nel browser.',
    unit: 'cluster',
    window: 'Lista più recente; aggiornata ogni 15 s.',
    howToRead:
      'Il livello critico dipende solo dal numero di primari, non dal ritardo; la pagina del cluster mostra quali istanze rivendicano il ruolo.',
  },
  'fleet.summary-instances-up': {
    what: 'Istanze che hanno inviato dati negli ultimi 60 secondi, su tutti i cluster; selezionare la card elenca i cluster con almeno una.',
    source:
      'Il flag up di ogni istanza nella lista dei cluster: l’ultimo invio risale al massimo a 60 secondi fa (upThreshold in internal/server/api.go).',
    unit: 'istanze',
    window: 'Lista più recente; aggiornata ogni 15 s.',
  },
  'fleet.summary-instances-down': {
    what: 'Istanze che non hanno inviato dati negli ultimi 60 secondi, su tutti i cluster; selezionare la card elenca i cluster con almeno una.',
    source:
      'Il flag up di ogni istanza nella lista dei cluster: l’ultimo invio è più vecchio di 60 secondi (upThreshold in internal/server/api.go).',
    unit: 'istanze',
    window: 'Lista più recente; aggiornata ogni 15 s.',
    howToRead:
      'Un’istanza sostituita da una più recente allo stesso indirizzo e porta resta qui come non raggiungibile, ma la salute del cluster la ignora.',
  },
  'fleet.summary-firing-alerts': {
    what: 'Alert attivi ora in tutta la flotta; la card è disabilitata se la lista degli alert non si è potuta caricare.',
    source:
      'La lista degli alert (/api/v1/alerts) filtrata sullo stato firing, contata nel browser.',
    unit: 'alert',
    window: 'Lista più recente; aggiornata ogni 15 s.',
    howToRead:
      'Selezionare la card mantiene i cluster con un alert attivo, abbinati per cluster o per istanza. Se la lista è stata troncata compare un avviso e il conteggio è allora un limite inferiore.',
  },
  'fleet.clusters': {
    what: 'Una card per cluster monitorato, con salute, primario, conteggi di istanze e standby, ritardo di replay massimo e livelli di permessi.',
    source:
      'La lista dei cluster (handleClusters in internal/server/api.go): tabelle clusters e instances, archi di topologia e letture del ritardo di replay degli ultimi 60 secondi.',
    unit: 'una card per cluster; ritardo in secondi',
    window: 'Lista più recente; aggiornata ogni 15 s.',
    howToRead:
      'Il ritardo di replay massimo è il valore più alto segnalato da uno standby negli ultimi 60 secondi; è vuoto se nessuno ne ha segnalato. Il livello di permessi è l’accesso che l’agent ha su ogni istanza; il suggerimento spiega cosa consente ogni livello. Health ok, degradato e critico seguono le card di riepilogo.',
  },
  'fleet.permission-tiers': {
    what: 'Livello di permessi dell’agent su ogni istanza: T0 legge le statistiche, T1 esegue anche EXPLAIN, T2 anche annulla o termina sessioni.',
    source:
      'Il perm_tier che ogni istanza riporta con i propri invii (internal/pgtype/role.go), elencato una volta per cluster; deploy/sql/monitoring_user.sql assegna un livello.',
    unit: 'livelli',
    window: 'Lista più recente; aggiornata ogni 15 s.',
    howToRead:
      'Un livello più alto include quelli inferiori. Le azioni T2 richiedono anche che il target consenta i segnali. Un livello sotto quello richiesto da una funzione la mostra come non consentita.',
  },
  'fleet.agent-health': {
    what: 'Istanze non raggiungibili o con un alert attivo Agent down o Instance unreachable, con la causa probabile e il tempo dall’ultimo invio.',
    source:
      'deriveAgentHealth nel browser: istanze segnate come non raggiungibili o silenziose per 45 secondi, e alert Agent down o Instance unreachable attivi.',
    unit: 'istanze',
    window: 'Lista più recente; aggiornata ogni 15 s.',
    howToRead:
      'I valori dei cluster qui elencati possono non essere aggiornati. La causa probabile è il testo dell’alert, se un alert è scattato; altrimenti che l’agent non ha inviato dati di recente.',
  },
  'cluster.drift-table': {
    what: 'Impostazioni il cui valore differisce tra le istanze di questo cluster: una riga per impostazione e una colonna per istanza.',
    source:
      'handleSettingsDrift in internal/server/api_settings.go: le impostazioni che ogni agent ha riportato per ultimo (object_facts), per le istanze viste nell’ultima ora.',
    unit: 'valori impostazione',
    window:
      'L’ultima lettura di ogni istanza che ha inviato nell’ultima ora; aggiornata ogni 15 s.',
    howToRead:
      'Le celle evidenziate differiscono dal riferimento, cioè il valore più diffuso tra le istanze; a parità vale il valore del primario. Le impostazioni attese diverse, come port e listen_addresses, sono escluse.',
  },
  'cluster.slots-table': {
    what: 'Slot di replica di questo cluster con stato, WAL trattenuto da ciascuno e da quanto tempo gli slot inattivi sono fermi.',
    source:
      'handleSlots in internal/server/api_topology.go: pg_replication_slots letto dal controllo replication_slots, ultima rilevazione di ogni slot.',
    unit: 'byte e durate',
    window: 'Ultima rilevazione di ogni slot negli ultimi 5 minuti; aggiornata ogni 15 s.',
    howToRead:
      'Byte trattenuti è il WAL conservato dalla posizione di riavvio dello slot. WAL before invalidation è lo spazio che resta prima che lo slot perda WAL; unlimited significa che non è impostato alcun limite di dimensione. Age inactive conta dall’ultima lettura attiva.',
    thresholds: 'I byte trattenuti sopra 1 GiB sono evidenziati.',
  },
  'topology.graph': {
    what: 'Collegamenti di replica tra le istanze del cluster: ogni freccia va da uno standby (copia in sola lettura) al server da cui copia le modifiche.',
    source:
      'Gli archi di topologia salvati per il cluster (handleTopology in internal/server/api_topology.go), dal WAL receiver di ogni standby e dallo stato di sincronizzazione sul primario.',
    unit: 'istanze e archi di replica',
    window: 'Archi più recenti; aggiornati ogni 15 s.',
    howToRead:
      'Un arco tratteggiato ha bassa affidabilità: lo standby non riceve lo streaming da quell’upstream in questo momento, oppure non invia da più di 60 s. Compare un banner quando non c’è alcun primario o ce n’è più di uno.',
  },
  'topology.edges': {
    what: 'Gli stessi collegamenti di replica del grafo, una riga per collegamento, con tipo, stato di sincronizzazione e affidabilità.',
    source:
      'Gli archi di topologia salvati per il cluster (handleTopology in internal/server/api_topology.go), mostrati come tabella per tastiera e screen reader.',
    unit: 'archi di replica',
    window: 'Archi più recenti; aggiornati ogni 15 s.',
    howToRead:
      'From è lo standby e To il server da cui copia le modifiche. Bassa affidabilità significa che lo standby non riceve lo streaming da quel server in questo momento, oppure non invia da più di 60 s.',
  },
  'cluster.events': {
    what: 'Failover rilevati in questo cluster, dal più recente; ogni riga mostra il vecchio e il nuovo primario.',
    source:
      'handleTopology in internal/server/api_topology.go: eventi di tipo failover_detected, i 100 più recenti del cluster.',
    unit: 'eventi',
    window: 'I 100 eventi failover più recenti registrati.',
    howToRead:
      'Gravità e controllo suggerito derivano dalla descrizione che la console dà a ogni tipo di evento. Una lista vuota significa che non è stato rilevato alcun failover.',
  },
  'cluster.events-table': {
    what: 'Una riga per failover rilevato nel cluster: ora, titolo, gravità, tipo e Details.',
    source:
      'handleTopology in internal/server/api_topology.go: eventi di tipo failover_detected, i 100 più recenti del cluster. Gli altri tipi sono nella pagina Eventi.',
    unit: 'eventi',
    window:
      'I 100 eventi failover più recenti registrati, dal più recente, a pagine; orari nel fuso orario della console.',
    howToRead:
      'Type è il nome grezzo dell’evento, sempre mostrato così un evento sconosciuto resta visibile. Details indica cosa controllare; una riga troncata si legge per intero al passaggio del mouse.',
  },
  'lag.edge': {
    what: 'Lag di write, flush e replay in secondi tra uno standby e il suo upstream nell’intervallo scelto, una linea per ciascuno.',
    source:
      'pg_stat_replication: write_lag, flush_lag e replay_lag letti dal controllo replication_streaming ogni 10 secondi; uno standby da solo riporta un replay lag stimato.',
    unit: 'secondi',
    window: 'Intervallo scelto, campioni grezzi, al massimo 30 giorni.',
    howToRead:
      'Ogni lag è l’attesa tra il flush del WAL sull’upstream e la conferma dello standby di averlo scritto, scaricato o riprodotto. Un vuoto è un campione mancante e non viene riempito.',
  },
}
