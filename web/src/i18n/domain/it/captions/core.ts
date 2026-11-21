import type { CaptionEntry, CaptionId } from '@/lib/captions'
import type { activityCaptions } from '@/lib/captionsets/activity'
import type { alertsCaptions } from '@/lib/captionsets/alerts'
import type { fleetCaptions } from '@/lib/captionsets/fleet'
import type { indexesCaptions } from '@/lib/captionsets/indexes'
import type { instanceCaptions } from '@/lib/captionsets/instance'
import type { queriesCaptions } from '@/lib/captionsets/queries'
import type { settingsCaptions } from '@/lib/captionsets/settings'

export const core: Readonly<
  Record<
    Exclude<
      CaptionId,
      | keyof typeof activityCaptions
      | keyof typeof alertsCaptions
      | keyof typeof fleetCaptions
      | keyof typeof indexesCaptions
      | keyof typeof instanceCaptions
      | keyof typeof queriesCaptions
      | keyof typeof settingsCaptions
    >,
    CaptionEntry
  >
> = {
  'overview.tps': {
    what: 'Transazioni confermate al secondo nel database selezionato.',
    source: 'pg_stat_database.xact_commit (pg_xact_commit_total), come tasso al secondo.',
    unit: 'al secondo',
    window: 'Intervallo di tempo selezionato.',
    howToRead:
      'Linea piatta a zero: nessun lavoro confermato. Un vuoto: nessun campione memorizzato per quell’istante.',
  },
  'overview.cache-hit-ratio': {
    what: 'Quota delle letture di pagina trovate nella cache di memoria di PostgreSQL (shared buffers) invece che richieste al sistema operativo.',
    source:
      'pg_stat_database.blks_hit / (blks_hit + blks_read), da pg_blks_hit_total e pg_blks_read_total.',
    unit: '%',
    window: 'Campione più recente dell’intervallo selezionato.',
    howToRead:
      'Le letture che non trovano shared buffers possono venire dalla cache del sistema operativo: un rapporto più basso non è sempre I/O su disco.',
  },
  'index.duplicate': {
    what: 'Indici con una definizione identica a quella di un altro indice della stessa tabella; ogni gruppo spreca spazio e costo di scrittura di tutti tranne uno.',
    source:
      'Regola advisor di pglens index.duplicate: indici di una tabella con lo stesso metodo di accesso e lo stesso testo normalizzato tra la prima e l’ultima parentesi (colonne, espressioni, predicato); l’unicità non viene confrontata.',
    unit: 'indici, in gruppi di due o più',
    window: 'Ultima valutazione dell’advisor.',
  },
  'index.redundant-prefix': {
    what: 'Indici le cui colonne sono la parte iniziale di un indice più lungo sulla stessa tabella; quello più lungo può servire le stesse ricerche.',
    source:
      'Regola advisor di pglens index.redundant_prefix: indici btree non unici le cui colonne chiave sono la parte iniziale stretta di un altro indice btree sulla stessa tabella, con lo stesso predicato.',
    unit: 'indici',
    window: 'Ultima valutazione dell’advisor.',
    howToRead:
      'Verifica che l’indice più lungo copra le stesse query prima di rimuovere quello più corto.',
  },
  'activity.connections-used': {
    what: 'Connessioni client aperte ora, confrontate con max_connections.',
    source:
      'Backend contati da pg_stat_activity (pg_connections_used) e impostazione max_connections (pg_connections_limit).',
    unit: 'connessioni',
    window: 'Ultimo campione.',
    thresholds: 'L’advisor avvisa quando le connessioni superano l’80% di max_connections.',
  },
  'settings.names': {
    what: 'Nomi di cluster e istanze, mostrati in titoli, breadcrumb, selettori dei server, filtri e tabelle di alert ed eventi al posto di ID e indirizzi.',
    source:
      'clusters.name e instances.display_name, impostati con renameCluster e renameInstance e letti dalle liste di cluster e istanze.',
    unit: 'nomi di al massimo 64 caratteri',
    window: 'Nomi attuali; le liste si aggiornano dopo ogni rinomina.',
    howToRead:
      'In questa tabella un’etichetta attenuata con scritto predefinito indica che non è impostato alcun nome: il cluster appare come Cluster e con il suo ID, l’istanza con indirizzo e porta.',
  },
  'settings.names-table': {
    what: 'Una riga per cluster, seguita da una riga per ogni sua istanza; Rinomina apre l’editor del nome.',
    source: 'La lista dei cluster (getClusters) e la lista delle istanze (getInstances).',
    window: 'Nomi attuali.',
    howToRead:
      'I nomi sono unici senza distinzione tra maiuscole e minuscole; un nome vuoto ripristina l’etichetta predefinita.',
  },
  'settings.preferences': {
    what: 'Preferenze della console salvate in questo browser: tema, intervallo di tempo predefinito, aggiornamento, righe per pagina, fuso orario e lingua.',
    source:
      'La voce pglens.preferences nel local storage di questo browser; non viene inviato nulla al server.',
    window:
      'Applicate subito a ogni pagina aperta di questo browser e mantenute dopo un ricaricamento.',
    howToRead:
      'Ogni controllo dichiara il suo effetto. Un intervallo o un numero di righe per pagina nell’indirizzo prevale sul predefinito; Ripristina i predefiniti ripristina tutte le preferenze.',
  },
  'settings.retention': {
    what: 'Giorni di storico che pglens conserva per ogni gruppo di metriche; i dati più vecchi vengono eliminati una volta al giorno.',
    source:
      'Le policy di retention e compressione di ogni hypertable (timescaledb_information.jobs), lette con getRetention e sostituite con setRetention.',
    unit: 'giorni',
    window:
      'Policy attive. Ogni policy gira una volta al giorno ed elimina interi blocchi di dati memorizzati (chunk da 6 ore; 1 giorno per il bloat) più vecchi della sua retention.',
    howToRead:
      'Anteprima mostra cosa eliminerebbe un valore; una retention più breve chiede conferma ed elimina quello storico alla successiva esecuzione della policy.',
  },
  'settings.retention-table': {
    what: 'Una riga per gruppo di metriche: la retention attuale, i giorni ammessi e un nuovo valore da provare in anteprima e applicare.',
    source:
      'getRetention per le righe, previewRetention per l’anteprima, setRetention per applicare.',
    unit: 'giorni',
    window: 'Policy attive.',
    howToRead:
      'La retention più breve è un giorno oltre l’orizzonte di compressione della famiglia e mai sotto i 3 giorni; la più lunga è 365 giorni.',
  },
  'settings.advisor-rules-table': {
    what: 'Le regole dell’advisor, una per riga con categoria, gravità, ambito e livello minimo di permessi; un interruttore fa eseguire o saltare ciascuna regola.',
    source:
      'getAdvisorRules per le righe; l’interruttore chiama setAdvisorRuleOverride, salvato in advisor_rule_overrides.',
    window: 'Catalogo attivo. Una modifica ha effetto alla successiva valutazione dell’advisor.',
    howToRead:
      'Quali regole dell’advisor girano. Il livello minimo è il livello di permessi (da T0 a T2) che un’istanza deve avere perché la regola venga valutata. Disattivata, una regola non viene valutata e le sue segnalazioni si risolvono alla valutazione successiva. Riattivata, la regola gira alla valutazione successiva e le sue segnalazioni si riaprono se la condizione persiste. custom indica un override salvato; riportarlo allo stato originale lo elimina. Ricerca e filtri non modificano alcuna regola; le soglie non sono modificabili.',
  },
  'settings.advisor-windows': {
    what: 'Quanto indietro guardano i controlli su indici inutilizzati e storico dei backup prima di emettere una segnalazione.',
    source:
      'Le finestre salvate da setAdvisorWindows (server_settings), lette con getAdvisorWindows; senza un valore salvato, 7 giorni.',
    unit: 'ore o giorni',
    window: 'Tra 1 ora e 90 giorni.',
    howToRead:
      'Una finestra più corta segnala prima e può aumentare i falsi positivi. L’advisor applica una finestra salvata alla sua valutazione successiva. Un indice risulta inutilizzato quando lo storico conservato da pglens copre l’intera finestra e pglens non ha visto alcuna scansione al suo interno; una scansione più vecchia della finestra non conta. Su PostgreSQL 16 e successivi i contatori di scansione del server possono giudicare subito un indice, senza quello storico: uno mai scansionato dall’ultimo reset del contatore, più vecchio di una finestra (o di data ignota), oppure ultimo scansionato prima della finestra senza alcuna scansione vista da pglens al suo interno. Anche gli indici che il controllo non segnala mai (10 MiB o meno, chiave primaria, unici) non ricevono una segnalazione di storico insufficiente. pglens conserva lo storico degli indici per la sua retention (30 giorni di default): una finestra di indice inutilizzato più lunga non è mai coperta da quello storico, quindi un indice giudicato solo su di esso resta degradato, con una motivazione che nomina entrambe le durate; le scansioni di uno standby vengono lette dallo stesso storico conservato. La finestra dello storico dei backup stabilisce solo per quanto tempo un’istanza deve essere monitorata prima che venga segnalato un base backup mancante (una copia completa dei file dei dati, il punto di partenza per il ripristino); qualunque sia la finestra, un base backup conta come recente per 7 giorni (Nessun base backup recente) e come strategia di backup per 30 giorni (Nessuna strategia di backup visibile), e uno visto più tempo fa viene segnalato subito.',
  },
  'settings.notifications': {
    what: 'Quali canali di invio degli alert sono configurati, dall’ambiente del server o dalla console, l’host a cui ciascuno invia e un messaggio di prova per canale.',
    source:
      'getNotificationChannels legge l’ambiente e gli indirizzi salvati dalla console (un indirizzo non viene mai inviato); setNotificationChannel e deleteNotificationChannel salvano e rimuovono un canale; Invia test chiama testNotificationChannel.',
    window:
      'Letto all’apertura della scheda. Un indirizzo salvato dalla console si applica subito e prevale sull’ambiente; una modifica dell’ambiente richiede il riavvio del server.',
    howToRead:
      'Invia test pubblica "pglens test notification" sullo stesso canale usato dagli alert e attende al massimo 10 s; in caso di errore mostra il messaggio con l’indirizzo rimosso.',
  },
  'instance.readiness': {
    what: 'Cosa non è configurato su questo server, quindi alcune funzioni di pglens mostrano meno del possibile, e come chiudere ogni lacuna.',
    source:
      'getReadiness: regole di pglens su impostazioni, estensioni, appartenenza ai ruoli, controlli saltati e interruttori dell’agent già memorizzati (fatti delle impostazioni, check_skip, agent_flags).',
    window: 'Ultimi valori; ricalcolati al massimo ogni 30 s.',
    howToRead:
      'Un avviso indica una funzione persa o degradata; una nota è disattivata per scelta o facoltativa. Ogni correzione indica dove intervenire e se serve un riavvio.',
  },
  'fleet.readiness': {
    what: 'Se l’agent può monitorare per intero ogni server del cluster, oppure cosa manca.',
    source:
      'getReadinessSummary: il livello di readiness di ogni istanza, contato per cluster (avvisi e note sommati).',
    window: 'Ultimi valori; aggiornati ogni 15 s.',
    howToRead:
      'Parziale significa che almeno un server ha un avviso: fai clic sul badge per aprire la sua scheda Readiness e le correzioni.',
  },
  'instance.coverage': {
    what: 'Quanti dei controlli pianificati dall’agent per questa istanza hanno memorizzato un risultato dall’ultimo avvio dell’agent.',
    source:
      'getCoverage: l’ora di avvio e gli intervalli dei controlli che l’agent invia a ogni push, uniti al risultato più recente memorizzato di ogni controllo (check_results).',
    unit: 'controlli',
    window: 'Dall’avvio dell’agent; aggiornato ogni 15 s.',
    howToRead:
      'Dopo un avvio ogni controllo gira una volta entro 30 s o 10 min, a seconda del suo intervallo; fino ad allora le pagine possono mostrare vuoti. Un controllo fallito ha restituito un errore all’ultima esecuzione, quindi non ha un risultato attuale: il banner resta e lo nomina finché un’esecuzione non riesce.',
  },
  'indexes.search': {
    what: 'Indici, tabelle, colonne e statement il cui nome, tabella, definizione, tipo, testo, queryid o database contiene il termine, nel database scelto se c’è.',
    source:
      'searchInstance: le definizioni degli indici e le colonne delle tabelle riportate dall’agent (fatti index_def e table_columns) e i testi degli statement di pg_stat_statements (query_texts), confrontati ignorando le maiuscole.',
    window:
      'Oggetti visti e statement eseguiti nell’intervallo di tempo selezionato. Ogni gruppo elenca al massimo 50 corrispondenze e le conta tutte.',
    howToRead:
      'Il testo evidenziato è ciò che ha corrisposto. Un link apre l’indice o la tabella, espansa, nella sua scheda, oppure la ricerca di statement in Query più lente, nello stesso intervallo di tempo.',
  },
  'indexes.missing': {
    what: 'Dove manca un indice, perché, e lo statement per crearlo, dal migliore: da statement, chiavi esterne, colonne spaziali e scansioni complete di tabella.',
    source:
      'buildMissingIndexes (missingIndexes.ts) su getIndexSuggestions, le segnalazioni index.fk_unindexed e postgis.spatial_unindexed di getFindings e i tassi di scansione e scrittura di getInstanceTables.',
    unit: 'indici',
    window:
      'Statement: l’intervallo scelto nella scheda (da 1 ora a 7 giorni). Segnalazioni e tassi delle tabelle: ultima valutazione e raccolta.',
    howToRead:
      'La raccomandazione indica cosa fare: Crea, Prova prima, Verifica il carico o Non vale la pena. Espandi una riga per gli statement di controllo, creazione, verifica e annullamento. pglens non li esegue mai.',
    thresholds:
      'Chiave esterna: crea quando si eliminano righe della tabella padre e la tabella è di 10 MiB o più. Scansioni complete: tabelle di 8 MiB o più lette a 100 righe al secondo o più.',
  },
  'locks.sessions-application': {
    what: 'Sessioni aperte dalle tue applicazioni: una riga per sessione client dell’ultimo campione di lock, con la transazione più vecchia per prima.',
    source:
      'getLocks sessions: backend client di pg_stat_activity (al massimo 100), classificati come applicazione dal server in base ad application_name e al ruolo di monitoraggio.',
    unit: 'sessioni',
    window: 'Ultimo campione di lock, preso ogni 10 s.',
    howToRead:
      'Espandi una riga per vedere ogni campo della sessione e le sue azioni di segnale. Query è lo statement normalizzato, salvo se marcato raw.',
  },
  'locks.sessions-pglens': {
    what: 'Connessioni aperte da pglens stesso per raccogliere queste metriche; non sono il tuo carico di lavoro.',
    source:
      'getLocks sessions classificate come pglens dal server: application_name pglens, pglens/… o pglens-agent/…, oppure il ruolo di monitoraggio.',
    unit: 'sessioni',
    window: 'Ultimo campione di lock, preso ogni 10 s.',
    howToRead:
      'Una sessione della tua applicazione non compare mai qui, e una sessione pglens non compare mai nella tabella delle applicazioni.',
  },
  'locks.connection-summary': {
    what: 'Connessioni client contate per origine, applicazione, utente, indirizzo client, database e stato, con la quota di ciascuno sul totale.',
    source:
      'getLocks connections: contate dalle stesse sessioni delle due tabelle; quando il campione è troncato, i conteggi dello scrape di attività (pg_connections_* e pg_backends).',
    unit: 'connessioni',
    window: 'Ultimo campione di lock, preso ogni 10 s.',
    howToRead:
      'Un indirizzo client local indica un socket Unix. I conteggi per applicazione richiedono checks.activity.by_application.',
  },
  'locks.origin': {
    what: 'Chi ha aperto la sessione: app è una connessione della tua applicazione, pglens una connessione aperta da pglens stesso.',
    source: 'L’origine che il server imposta su ogni sessione di getLocks.',
    window: 'Ultimo campione di lock.',
  },
  'index.history-scans': {
    what: 'Quante volte le query hanno scansionato questo indice al secondo, mediato su ogni bucket dell’intervallo selezionato.',
    source:
      'getIndexHistory: pg_stat_user_indexes.idx_scan, memorizzato come tasso al secondo ogni 5 minuti e mediato per bucket.',
    unit: 'scansioni al secondo',
    window: 'Intervallo di tempo selezionato, al massimo 30 giorni; bucket da 10 minuti o più.',
    howToRead:
      'Un vuoto significa che per quel bucket non è stato memorizzato alcun campione; non viene riempito nulla. Una linea piatta a zero indica nessuna scansione, e non è un giudizio che l’indice sia inutilizzato.',
  },
  'index.history-size': {
    what: 'Dimensione di questo indice alla fine di ogni bucket dell’intervallo selezionato.',
    source:
      'getIndexHistory: pg_relation_size dell’indice (index_bytes) memorizzato ogni 5 minuti; l’ultimo valore di ogni bucket.',
    unit: 'byte',
    window: 'Intervallo di tempo selezionato, al massimo 30 giorni; bucket da 10 minuti o più.',
    howToRead:
      'Un vuoto significa che per quel bucket non è stato memorizzato alcun campione. Una ricostruzione appare come un calo; la crescita indica che la tabella cresce o che l’indice va in bloat.',
  },
  'indexes.tax': {
    what: 'Quanto costano gli indici di ogni tabella: numero, dimensione, spazio inutilizzato e voci di indice che ogni scrittura aggiunge. Ogni valore è una stima.',
    source:
      'Le statistiche degli indici (pg_stat_user_indexes) e delle tabelle (pg_stat_user_tables) dell’ultima raccolta, combinate nel browser; la dimensione inutilizzata segue il giudizio dell’advisor o il contatore grezzo di scansioni.',
    unit: 'byte, scritture al secondo, voci di indice al secondo',
    window: 'Ultima raccolta; i tassi sono al secondo sull’ultimo intervallo.',
    howToRead:
      'L’amplificazione di scrittura sono inserimenti, eliminazioni e aggiornamenti non heap-only al secondo, per il numero di indici. Gli indici unici e di chiave primaria non vengono mai contati come inutilizzati. La dimensione inutilizzata è un candidato da rivedere, non un giudizio.',
  },
}
