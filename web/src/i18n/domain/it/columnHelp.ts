import type { ColumnHelpId } from '@/lib/captions'

export const columnHelp: Readonly<Record<ColumnHelpId, string>> = {
  'statements.mean-time': 'Tempo medio di esecuzione di una chiamata, da pg_stat_statements.',
  'statements.total-time':
    'Tempo di esecuzione di tutte le chiamate nell’intervallo selezionato, da pg_stat_statements.',
  'slots.retained-bytes':
    'WAL trattenuto per questo slot: byte tra la posizione WAL attuale del server e la posizione più vecchia che lo slot può ancora richiedere.',
  'slots.wal-before-invalidation':
    'Byte di WAL che possono ancora essere scritti prima che PostgreSQL rimuova il WAL di cui questo slot ha bisogno. Illimitato: nessun valore, come quando max_slot_wal_keep_size è -1.',
  'slots.age-inactive':
    'Tempo dall’ultima lettura attiva dello slot nello storico memorizzato; un trattino indica che è attivo ora.',
  'logical.errors':
    'Errori contati da pg_stat_subscription_stats dall’ultimo reset: prima quelli dei worker di apply, poi quelli di sincronizzazione delle tabelle.',
  'logical.conflicts':
    'Conflitti rilevati dalla sottoscrizione, da pg_stat_subscription_stats dall’ultimo reset; non disponibili prima di PostgreSQL 18.',
  'catalog.sequence-limit':
    'Ultimo valore che la sequenza può generare e che la colonna accetta: il più vicino tra il limite della sequenza e quello del tipo di colonna.',
  'catalog.sequence-used':
    'Quota dell’intervallo dal valore iniziale al limite già assegnata, a partire dall’ultimo valore letto. Se non può essere letta, compare il motivo.',
  'catalog.partitions-reach':
    'Limite superiore più alto tra le partizioni di range della tabella, in UTC. Nessuna fine significa che una partizione non ha limite superiore.',
  'catalog.extension-packages':
    'Versione predefinita dell’estensione tra i pacchetti installati sul server, da pg_available_extensions.',
  'catalog.extension-update':
    'Disponibile quando il pacchetto è più recente di quanto installato; sconosciuta quando i pacchetti non hanno l’estensione o le versioni non sono confrontabili.',
  'catalog.function-own-time':
    'Millisecondi al secondo spesi nel codice proprio della funzione, non in quelle che chiama, mediati sull’ultima ora. Il tempo totale li include.',
  'internals.slru-zeroed':
    'Blocchi al secondo riempiti di zeri nella cache, mediati sull’ultima ora, da pg_stat_slru.blks_zeroed.',
  'internals.slru-flushes':
    'Scritture al secondo dei dati sporchi della cache su disco, mediate sull’ultima ora, da pg_stat_slru.flushes.',
  'internals.slru-truncations':
    'Volte al secondo in cui la cache è stata troncata, mediate sull’ultima ora, da pg_stat_slru.truncates.',
  'relations.relation':
    'Schema e nome della tabella; nella scheda Indici, la tabella a cui appartiene l’indice. Lo stesso nome può comparire una volta per database.',
  'relations.live-tuples':
    'Righe che le statistiche contano come vive, da pg_stat_user_tables.n_live_tup; una stima mantenuta da PostgreSQL.',
  'relations.dead-tuples':
    'Versioni di riga lasciate da aggiornamenti ed eliminazioni che il vacuum non ha ancora rimosso, da pg_stat_user_tables.n_dead_tup.',
  'relations.dead-ratio':
    'Righe morte divise per righe vive, quindi può superare il 100%; è zero quando la tabella non ha righe vive.',
  'relations.seq-scan-rate':
    'Volte al secondo in cui l’intera tabella è stata letta dall’inizio alla fine, mediate sull’ultimo intervallo di raccolta.',
  'relations.idx-scan-rate':
    'Volte al secondo in cui l’indice è stato usato per trovare righe, mediate sull’ultimo intervallo di raccolta.',
  'relations.last-vacuum':
    'Il più recente tra il vacuum manuale e quello automatico; auto indica un vacuum automatico.',
  'relations.last-analyze':
    'Il più recente tra l’analyze manuale e quello automatico; auto indica un analyze automatico.',
  'relations.last-scan':
    'Quando una query ha usato l’indice l’ultima volta dal reset delle statistiche; PostgreSQL 15 non lo riporta.',
  'relations.valid':
    'Un indice non valido non viene usato dalle query, per esempio dopo una costruzione concorrente fallita.',
  'relations.real-size':
    'Dimensione della tabella o dell’indice su disco al momento della misura dell’agent.',
  'relations.expected-size':
    'Dimensione che avrebbe senza spazio sprecato, stimata da conteggi di righe e statistiche delle colonne.',
  'relations.bloat':
    'Dimensione reale meno dimensione attesa: spazio che la stima indica non contenere dati vivi; zero quando non è più grande del previsto.',
  'relations.bloat-ratio': 'Bloat diviso per dimensione reale.',
  'relations.method':
    'Stima è una stima statistica dell’intera tabella o dell’indice; pgstattuple è una misura esatta di una sola tabella.',
  'instance.setting-source':
    'Dove è stato impostato il valore attuale, da pg_settings.source, come default o configuration file.',
  'instance.setting-context':
    'Da pg_settings.context: postmaster significa che una modifica richiede un riavvio, sighup un ricaricamento della configurazione.',
  'instance.setting-changed':
    'Quando pglens ha visto per l’ultima volta cambiare il valore di questa impostazione.',
  'instance.setting-state':
    'Riavvio in attesa: la configurazione contiene un nuovo valore che entra in vigore solo con un riavvio, da pg_settings.pending_restart.',
  'instance.durability-fsync':
    'off: PostgreSQL non forza le proprie scritture su disco, quindi un crash o una perdita di alimentazione può corrompere i dati.',
  'instance.durability-full-page-writes':
    'off: le pagine modificate non vengono scritte per intero nel WAL dopo un checkpoint, quindi un crash può lasciare una pagina che il recupero non riesce a riparare.',
  'instance.durability-archive-mode':
    'off: i file WAL completati non vengono copiati da nessuna parte, quindi non è possibile ripristinare un backup a un momento preciso.',
  'instance.durability-wal-level':
    'minimal: il WAL conserva solo ciò che serve al crash recovery, quindi non ci sono replica né point-in-time recovery.',
  'instance.durability-synchronous-commit':
    'off: un commit viene confermato prima che il suo WAL sia scaricato su disco, quindi un crash può far perdere gli ultimi commit.',
  'internals.temp-files-rate':
    'File temporanei creati al minuto in questo database, mediati sull’ultima ora, da pg_stat_database.temp_files.',
  'internals.recovery-cancelled':
    'Query annullate al minuto su questo standby per questo motivo, mediate sull’ultima ora, da pg_stat_database_conflicts.',
  'statements.share-of-listed':
    'Tempo totale di questo statement diviso per il tempo totale di tutti gli statement elencati nella tabella; quelli non elencati non vengono contati.',
  'statements.cache-hit-ratio':
    'Hit dei blocchi in shared buffers diviso per hit più letture, per questo statement nell’intervallo; una lettura può comunque venire dalla cache del sistema operativo.',
  'statements.wal':
    'Byte di WAL scritti da questo statement nell’intervallo selezionato, stimati dai tassi al secondo.',
  'statements.temp-bytes-per-call':
    'Byte temporanei scritti per chiamata nell’intervallo selezionato: blocchi temporanei scritti, 8 KiB ciascuno, divisi per le chiamate.',
  'indexes.scans':
    'Scansioni dell’indice al secondo nell’ultimo intervallo di raccolta, da pg_stat_user_indexes.idx_scan.',
  'indexes.flags-column':
    'Etichette dell’indice: il suo tipo, come primary, unique o partial, e le segnalazioni dell’advisor su di esso.',
  'indexes.cache-hit':
    'Quota delle letture di blocchi dell’indice servite dalla cache di memoria propria di PostgreSQL, da pg_statio_user_indexes.',
  'indexes.last-scan':
    'Quando una query ha scansionato l’indice l’ultima volta; PostgreSQL 16 o successivo lo registra, i server più vecchi mostrano Non disponibile.',
  'indexes.fk-columns':
    'Le colonne che referenziano della chiave esterna; nessun indice le ha come prime.',
  'indexes.fk-references':
    'La tabella a cui punta la chiave esterna; eliminare o rigenerare le sue righe scansiona la tabella che contiene la chiave.',
  'indexes.fk-severity-column':
    'Avviso quando la tabella che referenzia è di 10 MiB o più, info sotto questa dimensione.',
  'indexes.cleanup-reason-column':
    'Perché l’indice è elencato: segnalazioni dell’advisor per indici non validi, duplicati, con prefisso ridondante o inutilizzati; contatori grezzi solo per quelli mai scansionati.',
  'indexes.cleanup-writes':
    'Voci al secondo che inserimenti e aggiornamenti non HOT aggiungono a questo indice; un calo le risparmia.',
  'indexes.dead-rows-column':
    'Versioni di riga lasciate da aggiornamenti ed eliminazioni che il vacuum non ha ancora rimosso, con lo spazio stimato che occupano.',
  'indexes.dead-live':
    'Righe morte divise per righe vive; sopra il 100% la tabella contiene più versioni morte che vive.',
  'indexes.vacuum-due':
    'Righe morte divise per il conteggio a cui parte il vacuum automatico; 1,0× o più significa che il vacuum è dovuto.',
  'indexes.dead-last-vacuum':
    'Ora del vacuum manuale o automatico più recente; Mai significa che non ne risulta nessuno.',
  'indexes.hot-share':
    'Quota degli aggiornamenti della tabella che sono HOT: non hanno scritto alcuna voce in nessun indice.',
  'indexes.dead-writes':
    'Inserimenti, aggiornamenti ed eliminazioni al secondo sulla tabella, sommati.',
  'indexes.dead-trend':
    'Righe morte della tabella nell’intervallo di tempo selezionato; solo le 10 tabelle peggiori hanno un andamento.',
  'indexes.missing-recommendation':
    'Cosa fare: Crea, Prova prima, Verifica il carico (esegui il controllo, poi decidi) o Non vale la pena (le scritture superano le letture).',
  'indexes.missing-source':
    'Dove è stata trovata la lacuna: statement lenti, una chiave esterna, una colonna spaziale o scansioni complete di tabella.',
  'indexes.missing-columns':
    'Colonne dell’indice da creare, in ordine di chiave; un trattino quando prima bisogna leggere gli statement.',
  'indexes.missing-why': 'Perché serve l’indice, con i dati; espandi la riga per i passaggi.',
  'indexes.missing-command':
    'Copia lo statement che crea l’indice senza bloccare le scritture; pglens non lo esegue mai.',
  'indexes.tax-index-size':
    'Dimensione combinata degli indici della tabella riportati dall’agent; gli indici non riportati non vengono contati.',
  'indexes.tax-table-size':
    'Dimensione della sola heap della tabella, senza i suoi indici e i dati TOAST.',
  'indexes.tax-ratio':
    'Dimensione combinata degli indici in percentuale della dimensione della heap; sopra il 100% gli indici sono più grandi della tabella.',
  'indexes.tax-unused':
    'Spazio degli indici senza scansioni: advisor significa che l’advisor li ha giudicati inutilizzati, contatori grezzi significa nessuna scansione dal reset delle statistiche.',
  'indexes.tax-writes':
    'Inserimenti, eliminazioni e aggiornamenti non HOT, al secondo: ciascuno scrive una voce in ogni indice della tabella.',
  'indexes.tax-amplification':
    'Scritture al secondo per il numero di indici: le voci di indice che le scritture della tabella aggiungono ogni secondo, stimate.',
  'sessions.pid': 'ID di processo del processo server di PostgreSQL che serve questa sessione.',
  'sessions.state':
    'Stato della sessione in pg_stat_activity: active, idle, idle in transaction e così via.',
  'sessions.wait':
    'Ciò che la sessione sta aspettando ora, come tipo di wait event e wait event; vuoto quando non è in attesa.',
  'sessions.transaction-age':
    'Tempo dall’inizio della transazione corrente di questa sessione; zero quando non ne ha una aperta.',
  'sessions.query-age':
    'Tempo dall’inizio dell’ultima query di questa sessione; per una sessione inattiva, l’età della sua ultima query.',
  'advisor.enabled':
    'Disattivata: la regola non viene valutata e le sue segnalazioni si risolvono alla valutazione successiva. Riattivata: si riaprono se la condizione persiste.',
  'settings.permission-tier':
    'T0 legge solo le statistiche, T1 esegue anche EXPLAIN per i piani delle query, T2 annulla o termina anche altre sessioni.',
  'settings.instance-state':
    'Up: l’istanza ha inviato dati negli ultimi 60 secondi. Down: non ha inviato dati entro quel tempo.',
  'settings.agent-state':
    'Down quando l’istanza non ha inviato dati entro 60 secondi o un alert agent-down è aperto per essa; altrimenti In invio.',
  'settings.skip-reason':
    'Perché l’agent non monitora questo database; db_budget significa che monitora già tanti database quanti ne consente il budget. Un trattino: monitorato.',
  'settings.audit-requested-by':
    'La credenziale che ha chiesto: api-token, oppure ui-session con un breve hash della sessione, poi l’indirizzo client; non una persona.',
  'settings.audit-state':
    'requested, ok, error, rejected o expired. Una richiesta che l’agent non ha mai preso compare come expired una volta scaduta in coda.',
  'rules.rule': 'Cosa controlla la regola, in una frase, seguita dal suo id di regola.',
  'rules.needs': 'I dati che la regola legge, in parole semplici.',
  'rules.tier':
    'Livello di permessi richiesto dalla regola: T0 legge solo le statistiche. Le regole T0 sono sempre attive.',
  'rules.threshold':
    'Il valore che la metrica deve superare perché l’alert scatti, con la sua unità. Una regola generata da un evento non ha soglia.',
  'rules.duration':
    'Per quanto deve durare la condizione prima che l’alert scatti; 0 s fa scattare subito.',
  'findings.object':
    'Il testo dello statement, o la tabella, l’indice, l’impostazione o lo slot a cui si riferisce la segnalazione; poi il suo database, istanza e cluster, per nome.',
  'findings.scope':
    'A cosa si riferisce la segnalazione: un’istanza, un cluster, un database o un singolo oggetto del database, come una tabella o un indice.',
  'findings.state':
    'open: il problema è presente ora. muted: lo hai silenziato per un certo tempo. degraded: la regola non ha potuto essere valutata. resolved: il problema è risolto.',
  'events.details':
    'Cosa controllare per quell’evento; un failover mostra il primario vecchio e quello nuovo, un reset dei contatori mostra quanti contatori sono stati azzerati.',
  'alerts.notes':
    'Il silenziamento che sopprime l’alert, con il tempo che gli resta, e un link alla checklist per gli alert dell’agent.',
  'alerts.suppression':
    'Soppresso: un silenziamento ferma le sue notifiche. Sconosciuto: i silenziamenti non sono stati caricati.',
  'silences.matchers':
    'Un’etichetta dell’alert confrontata con un valore: = è esatto, ~ è un’espressione regolare, not ne inverte il risultato. Un alert deve corrispondere a tutti.',
}
