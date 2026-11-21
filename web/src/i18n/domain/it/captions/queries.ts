import type { CaptionEntry } from '@/lib/captions'
import type { queriesCaptions as english } from '@/lib/captionsets/queries'

export const queries: Readonly<Record<keyof typeof english, CaptionEntry>> = {
  'queries.statements-section': {
    what: 'Righe di pg_stat_statements di questa istanza, ordinate per la misura scelta e limitate alle prime 50.',
    source:
      'Endpoint statement del server su metrics_statements, scritto dal controllo stat_statements da pg_stat_statements; una riga per queryid e database, ordinata e limitata sul server.',
    unit: 'statement; ogni riga riporta chiamate, ms, righe, blocchi e byte.',
    window: 'Intervallo di tempo selezionato.',
    howToRead:
      'PostgreSQL elimina le voci oltre pg_stat_statements.max, quindi uno statement può comparire e sparire tra un intervallo e l’altro. La ricerca confronta il testo della query, il queryid o il database su tutto l’intervallo.',
  },
  'queries.statements-table': {
    what: 'Totali per statement nell’intervallo selezionato: chiamate, tempo di esecuzione, righe, cache hit ratio, WAL scritto e blocchi temporanei scritti.',
    source:
      'Endpoint statement del server: chiamate, tempo, righe, hit e letture dei blocchi condivisi, WAL e blocchi temporanei sono tassi sommati sull’intervallo; quote e rapporti sono calcolati nel browser.',
    unit: 'chiamate, ms, righe, %, byte.',
    window: 'Intervallo di tempo selezionato.',
    howToRead:
      'Tempo medio: tempo totale diviso per le chiamate. Quota del tempo elencato: tempo totale dello statement diviso il totale delle righe elencate. Cache hit ratio: hit dei blocchi condivisi diviso hit più letture. Byte temporanei per chiamata: blocchi temporanei scritti (8192 byte ciascuno) diviso per le chiamate. I pulsanti di ordinamento riordinano solo le righe già caricate.',
  },
  'queries.ranking': {
    what: 'La misura con cui il server ordina gli statement prima di tenere i primi 50; i pulsanti di ordinamento delle colonne riordinano solo le righe già caricate.',
    source:
      'Parametro order_by dell’endpoint statement del server: total_exec_time, mean_exec_time, calls, rows, shared_blks_read, temp_blks_written o wal_bytes, sommati sull’intervallo.',
    unit: 'misura (ms, chiamate, righe, blocchi o byte), dal valore più alto.',
    window: 'Intervallo di tempo selezionato.',
  },
  'slow.slowest-mean': {
    what: 'Tempo medio di esecuzione per chiamata più alto tra gli statement ordinati.',
    source:
      'Massimo, sugli statement elencati, del tempo totale di esecuzione diviso le chiamate, dall’endpoint statement del server su metrics_statements.',
    unit: 'ms, nell’unità più grande adatta.',
    window: 'Intervallo di tempo selezionato.',
    howToRead:
      'Vengono confrontati solo gli statement caricati dalla pagina; con la classifica Tempo totale uno statement più lento può trovarsi fuori dalla lista.',
  },
  'slow.total-time': {
    what: 'Tempo di esecuzione sommato su tutti gli statement ordinati, non sull’intero server.',
    source:
      'Somma del tempo totale di esecuzione di ogni statement elencato (pg_stat_statements.total_exec_time, memorizzato come tasso e moltiplicato per i secondi del campione; stimato con 60 s quando un campione non ha durata).',
    unit: 'ms, nell’unità più grande adatta.',
    window: 'Intervallo di tempo selezionato.',
  },
  'slow.calls': {
    what: 'Numero di chiamate sommato sugli statement ordinati.',
    source:
      'Somma delle chiamate di ogni statement elencato (pg_stat_statements.calls, memorizzato come tasso e moltiplicato per i secondi del campione; stimato con 60 s quando un campione non ha durata).',
    unit: 'chiamate.',
    window: 'Intervallo di tempo selezionato.',
  },
  'slow.statements-ranked': {
    what: 'Numero di statement che la pagina ha caricato e ordinato, fino a 200.',
    source:
      'Conteggio delle righe restituite dall’endpoint statement del server, richiesto con un limite di 200.',
    unit: 'statement.',
    window: 'Intervallo di tempo selezionato.',
    howToRead:
      'A 200 la lista viene tagliata al limite della pagina. Se l’agent ha conservato solo il suo budget top-N per questa finestra (checks.stat_statements.top_n), alcuni statement potrebbero mancare.',
  },
  'slow.table': {
    what: 'Statement ordinati per tempo medio per chiamata o per tempo totale, con chiamate, tempo medio, tempo totale e quota del tempo elencato.',
    source:
      'Endpoint statement del server su metrics_statements; la quota del tempo elencato è calcolata nel browser come tempo totale di uno statement diviso il totale degli statement ordinati.',
    unit: 'chiamate, ms, %.',
    window: 'Intervallo di tempo selezionato.',
    howToRead:
      'La posizione 1 ha il valore più alto della classifica scelta. La quota è relativa agli statement elencati, non a tutto il tempo del database. Apri una query per eseguire EXPLAIN e vedere la cronologia dei suoi piani.',
  },
  'slow.rank-by': {
    what: 'Sceglie se gli statement sono ordinati per tempo medio per chiamata o per tempo totale.',
    source:
      'order_by dell’endpoint statement: mean_exec_time (tempo totale diviso per le chiamate) oppure total_exec_time.',
    unit: 'ms, dal valore più alto.',
    window: 'Intervallo di tempo selezionato.',
  },
  'queries.detail-mean-time': {
    what: 'Tempo medio di esecuzione di una chiamata di questo statement.',
    source:
      'Tempo totale di esecuzione diviso le chiamate per questo queryid e database, dall’endpoint statement del server su metrics_statements.',
    unit: 'ms, nell’unità più grande adatta.',
    window: 'Intervallo di tempo selezionato.',
  },
  'queries.detail-total-time': {
    what: 'Tempo di esecuzione di tutte le chiamate dello statement.',
    source:
      'pg_stat_statements.total_exec_time per questo queryid e database, memorizzato come tasso e sommato sull’intervallo dall’endpoint statement del server.',
    unit: 'ms, nell’unità più grande adatta.',
    window: 'Intervallo di tempo selezionato.',
  },
  'queries.detail-calls': {
    what: 'Numero di esecuzioni di questo statement.',
    source:
      'pg_stat_statements.calls per questo queryid e database, memorizzato come tasso e sommato sull’intervallo dall’endpoint statement del server.',
    unit: 'chiamate.',
    window: 'Intervallo di tempo selezionato.',
  },
  'queries.detail-rows-per-call': {
    what: 'Righe restituite o modificate per chiamata di questo statement, a una cifra decimale.',
    source:
      'pg_stat_statements.rows diviso le chiamate per questo queryid e database, dall’endpoint statement del server.',
    unit: 'righe per chiamata.',
    window: 'Intervallo di tempo selezionato.',
  },
  'queries.detail-cache-hit-ratio': {
    what: 'Quota dei blocchi condivisi usati da questo statement che erano in shared buffers.',
    source:
      'pg_stat_statements.shared_blks_hit diviso shared_blks_hit più shared_blks_read, calcolato nel browser dall’endpoint statement del server.',
    unit: '%',
    window: 'Intervallo di tempo selezionato.',
    howToRead:
      'Le letture fuori dai shared buffers possono venire dalla cache del sistema operativo: un rapporto basso non è sempre I/O su disco.',
  },
  'queries.detail-wal': {
    what: 'Write-ahead log generato da tutte le chiamate di questo statement.',
    source:
      'pg_stat_statements.wal_bytes per questo queryid e database, memorizzato come tasso e sommato sull’intervallo dall’endpoint statement del server.',
    unit: 'byte.',
    window: 'Intervallo di tempo selezionato.',
  },
  'findings.list': {
    what: 'Problemi trovati dalle regole dell’advisor sulle statistiche raccolte, una riga per segnalazione con gravità, stato ed evidenze.',
    source:
      'Endpoint segnalazioni del server: righe della tabella delle segnalazioni scritta dal motore advisor dopo ogni valutazione di regola; prima le segnalazioni aperte e degradate, al massimo 1000.',
    unit: 'segnalazioni.',
    window: 'Stato all’ultima valutazione dell’advisor di ogni regola.',
    howToRead:
      'Aperta: la condizione della regola era vera alla sua ultima valutazione. Degradata: la regola non ha potuto essere valutata, e non è un risultato sano. Silenziata: esclusa fino alla scadenza. Risolta: la condizione non è più vera. Le segnalazioni derivano da statistiche raccolte, non da piani delle query; le raccomandazioni sugli indici sono candidate da rivedere, non modifiche automatiche.',
  },
  'findings.summary': {
    what: 'Conteggi delle segnalazioni aperte per gravità e di quelle la cui regola non ha potuto essere valutata; ogni pulsante filtra la lista.',
    source:
      'Contati nel browser su ogni segnalazione restituita dal server per la ricerca corrente, prima dei filtri di stato, ambito, cluster e istanza.',
    unit: 'segnalazioni.',
    window: 'Ultima valutazione dell’advisor.',
    howToRead:
      'Le segnalazioni silenziate e risolte non vengono contate. Non valutabili conta segnalazioni degradate, non regole.',
  },
  'findings.filter-state': {
    what: 'Mostra le segnalazioni in uno stato; il predefinito elenca quelle aperte e degradate e nasconde quelle silenziate e risolte.',
    source:
      'findings.state, impostato dal motore advisor e dalle azioni di silenziamento, filtrato nel browser.',
    unit: 'segnalazioni.',
    window: 'Stato all’ultima valutazione dell’advisor.',
    howToRead:
      'Degradata significa che la regola non ha potuto girare, per mancanza di un livello di permessi o di un input. Una segnalazione silenziata torna aperta alla scadenza, se la condizione persiste.',
  },
  'findings.filter-severity': {
    what: 'Mostra le segnalazioni di una gravità, critico, avviso o info, come impostata dalla regola dell’advisor che le ha generate.',
    source:
      'Gravità dichiarata della regola nel registro dell’advisor (internal/advisor) e memorizzata in findings.severity, filtrata nel browser.',
    unit: 'segnalazioni.',
    window: 'Stato all’ultima valutazione dell’advisor.',
  },
  'findings.filter-scope': {
    what: 'Mostra le segnalazioni la cui regola giudica un intero cluster o una singola istanza.',
    source:
      'findings.scope, impostato dalla regola dell’advisor che ha generato la segnalazione, filtrato nel browser.',
    unit: 'segnalazioni.',
    window: 'Stato all’ultima valutazione dell’advisor.',
    howToRead:
      'Le segnalazioni su un database o una tabella hanno un ambito proprio e compaiono solo con Tutti gli ambiti.',
  },
  'findings.rule-catalogue': {
    what: 'Ogni regola dell’advisor con gravità, ambito, input e livello di permessi, e quanti target segnala o non riesce a valutare.',
    source:
      'Endpoint regole dell’advisor del server, il registro delle regole in internal/advisor; i conteggi di quelle attive e non valutabili sono calcolati nel browser dalla tabella delle segnalazioni.',
    unit: 'regole; conteggi in target.',
    window: 'Registro com’è costruito; conteggi all’ultima valutazione dell’advisor.',
    howToRead:
      'Una regola disattivata non viene valutata e i suoi conteggi restano a zero. Si attiva o disattiva in Impostazioni → Regole advisor.',
  },
  'findings.rule-catalogue-table': {
    what: 'Una riga per regola dell’advisor: gravità, ambito, input richiesti, livello minimo di permessi e i target con una segnalazione aperta o degradata.',
    source:
      'Endpoint regole dell’advisor del server per le colonne delle regole; i conteggi sono target distinti nelle segnalazioni caricate dalla pagina, al massimo 1000.',
    unit: 'regole; conteggi in target.',
    window: 'Registro com’è costruito; conteggi all’ultima valutazione dell’advisor.',
    howToRead:
      'Istanze attive conta i target distinti con una segnalazione aperta della regola: istanze per le regole di istanza e cluster per le regole di cluster. Non valutabili conta i target la cui segnalazione è degradata.',
  },
  'findings.catalogue-tier': {
    what: 'Mostra le regole che girano al livello di permessi del ruolo di monitoraggio scelto o sotto di esso.',
    source:
      'Livello minimo della regola dal registro dell’advisor: T0 pg_monitor e pg_control_system(), T1 aggiunge pg_read_all_data, T2 aggiunge pg_signal_backend (internal/pgtype/role.go).',
    unit: 'livello di permessi, da T0 a T2.',
    window: 'Registro così com’è costruito.',
    howToRead:
      'Su un’istanza il cui livello è inferiore al minimo della regola, la regola riporta una segnalazione degradata invece di essere valutata.',
  },
  'queries.explain-panel': {
    what: 'Come PostgreSQL esegue questo statement: le operazioni scelte, le righe che gestisce ciascuna e dove vanno tempo e costo.',
    source:
      'L’agent esegue EXPLAIN sul database monitorato per questo query id, in una transazione di sola lettura, e restituisce il piano; il server lo conserva nella cronologia dei piani.',
    unit: 'operazioni del piano',
    window: 'Un piano per richiesta; descrive il database nel momento della richiesta.',
    howToRead:
      'Solo piano richiede il livello T1 e non esegue nulla: le sue cifre sono stime del planner. Piano con ANALYZE richiede T2 e allow_explain_analyze sull’agent: esegue lo statement, poi lo annulla con rollback, e misura tempi e righe reali.',
  },
  'queries.explain-params': {
    what: 'Valori per i $1…$n che pg_stat_statements mette al posto delle costanti; ANALYZE ne ha bisogno per eseguire lo statement.',
    source:
      'Li inserisci tu; l’agent li lega come parametri e non li scrive mai nello statement. Se vuoti, l’agent prende un’esecuzione recente da pg_stat_activity.',
    unit: 'un valore per segnaposto',
    window: 'Solo per la richiesta successiva.',
    howToRead:
      'Compila tutti o nessuno. Scrivi ogni valore come in SQL, senza apici; spunta NULL per il NULL SQL. Solo piano senza valori dà un piano generico (PostgreSQL 16+).',
  },
  'queries.plan-tree': {
    what: 'Il piano come albero di operazioni, con righe, cicli e il tempo o il costo che ciascuna aggiunge senza i suoi figli.',
    source:
      'L’output di EXPLAIN restituito dall’agent. Tempo e righe sono totali su tutti i cicli; i buffer sono pagine da 8 kB, in cache o letti dal disco.',
    unit: 'ms, righe, unità di costo del planner, byte',
    window: 'Un’esecuzione, o la stima del planner quando ANALYZE non è stato eseguito.',
    howToRead:
      'Parti da Cosa osservare, poi dalla barra più larga: l’operazione in cui va il tempo. Un badge ×N segna una stima di righe sbagliata di N volte, segno di statistiche non aggiornate. Il testo mostra il piano come lo stampa psql.',
  },
  'queries.plan-history': {
    what: 'Ogni piano richiesto per questo statement, dal più recente, con la sua forma: selezionane uno per leggerlo, due per confrontarli.',
    source:
      'Endpoint dei piani del server sulla tabella query_plans, riempita solo dai piani richiesti sopra; i 20 più recenti per questo query id.',
    unit: 'piani',
    window: 'Ogni richiesta conservata dal server, dalla più recente; al massimo 20 mostrati.',
    howToRead:
      'La forma è un’impronta di operazioni, tabelle, indici e condizioni, non di costi o tempi. Una nuova forma significa che PostgreSQL ora esegue lo statement in un altro modo: dopo una modifica a indice, statistiche o parametro.',
  },
  'queries.plan-captures': {
    what: 'Una riga per piano catturato: quando, di che tipo, la sua forma, costo, tempo e righe, e l’operazione principale.',
    source: 'I piani memorizzati dal server per questo query id, dal più recente.',
    unit: 'piani; costo in unità del planner, tempo in ms',
    window: 'Le 20 catture più recenti.',
    howToRead:
      'Nuova forma segna una cattura la cui forma differisce da quella precedente. Confronta un EXPLAIN con un EXPLAIN per costo e un ANALYZE con un ANALYZE per tempo.',
  },
  'queries.plan-comparison': {
    what: 'Le operazioni di due piani catturati affiancate, abbinate per posizione nell’albero, con ciò che è cambiato.',
    source:
      'I due piani selezionati nella cronologia dei piani: operazione, oggetto, condizioni, costo, righe e tempo di ciascuno.',
    unit: 'operazioni del piano; costo in unità del planner, tempo in ms',
    window: 'I due piani memorizzati, come catturati.',
    howToRead:
      'Modificato: operazione, oggetto o condizione diversi nella stessa posizione. Aggiunto e Rimosso esistono in un solo piano. Valori: dal meno recente al più recente.',
  },
  'findings.table': {
    what: 'Una riga per segnalazione dell’advisor: gravità, cosa è stato trovato, lo statement o l’oggetto interessato e quando è stata vista l’ultima volta.',
    source:
      'Endpoint segnalazioni del server sulla tabella delle segnalazioni scritta dal motore advisor, con il testo dello statement conservato dal server; le frasi delle regole vengono dal catalogo delle regole.',
    unit: 'segnalazioni',
    window:
      'Le segnalazioni che corrispondono ai filtri sopra, al massimo 1000; ultima vista nel fuso orario della console.',
    howToRead:
      'Uno stato diverso da aperta compare sotto la gravità. Apri una riga per lo statement completo, cosa fare, la SQL per verificarla sul database e le evidenze.',
  },
  'findings.verify': {
    what: 'I passi e la SQL di sola lettura che mostrano sul database ciò che riporta la segnalazione, per controllarla da te.',
    source:
      'Costruiti dalla console a partire da regola e segnalazione: database, oggetto, id dello statement e soglia. Da qui non si esegue nulla.',
    unit: 'passi e uno script SQL',
    window: 'Eseguilo ora: legge le statistiche attuali, non il momento della segnalazione.',
    howToRead:
      'Connettiti al database indicato nel passo 1 ed esegui lo script. Gira in una transazione di sola lettura annullata con rollback, quindi non modifica nulla. Confronta i suoi numeri con le evidenze qui sotto.',
  },
}
