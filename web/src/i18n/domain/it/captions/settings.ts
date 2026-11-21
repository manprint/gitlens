import type { CaptionEntry } from '@/lib/captions'
import type { settingsCaptions as english } from '@/lib/captionsets/settings'

export const settings: Readonly<Record<keyof typeof english, CaptionEntry>> = {
  'settings.server-info': {
    what: 'La build di questa interfaccia della console, la versione del contratto per cui è stata creata e quando scade la tua sessione di accesso.',
    source:
      'Nome della build e riga di versione del documento OpenAPI sono fissati alla compilazione dell’interfaccia (PGLENS_VERSION, o dev); la scadenza è expires_at dell’endpoint della sessione del server.',
    window: 'I valori della build sono fissi; la scadenza è quella della sessione corrente.',
    howToRead: 'Le sessioni sono in memoria e terminano al riavvio del server.',
  },
  'settings.product-limits': {
    what: 'Scala supportata e limiti noti di questa versione: retention, versioni di PostgreSQL, replica e campionamento.',
    source:
      'Testo fisso di questa pagina; la maggior parte delle righe segue il documento dei limiti del repository, mentre le righe su replica e database per istanza non vi compaiono. La retention attiva di ogni famiglia di metriche si legge nella scheda Retention dei dati.',
    window:
      'Questa versione; i predefiniti di retention sono impostati sul server, il budget dei database nella configurazione dell’agent.',
    howToRead:
      'La riga Retention grezza rimanda alla scheda Retention dei dati; le altre rimandano al documento dei limiti. I valori di bloat sono stime e la cronologia delle sessioni attive è un campione, non una traccia completa. I limiti riguardano anche i database per istanza.',
  },
  'settings.authentication': {
    what: 'Come si autenticano la console e l’agent: una sola password condivisa, nessun account o ruolo utente e un token dell’agent separato.',
    source:
      'Il deposito delle sessioni del server (in memoria) e la password condivisa e il token dell’agent impostati nell’ambiente del server (PGLENS_UI_PASSWORD, PGLENS_BOOTSTRAP_TOKEN).',
    window: 'La configurazione corrente del server.',
    howToRead:
      'Chi conosce la password ha lo stesso accesso. Non esistono una coda di registrazione né un controllo di revoca dal browser, e le sessioni terminano al riavvio del server.',
  },
  'settings.references': {
    what: 'Link al riferimento dell’interfaccia, alle regole di alert e ai silenziamenti.',
    source: 'Link fissi di questa pagina: /docs/api.md, /alerts/rules e /alerts/silences.',
    window: 'Link statici; aprono i documenti e le pagine attuali.',
  },
  'settings.command-audit': {
    what: 'Ogni comando richiesto a un agent e il suo esito, per istanza monitorata, con la credenziale che lo ha richiesto, non la persona.',
    source:
      'Endpoint di audit dei comandi del server sulla tabella command_audit, in sola aggiunta; un elenco per istanza monitorata.',
    unit: 'voci di audit',
    window: 'Le 100 voci più recenti di ogni istanza.',
    howToRead:
      'Richiesto da mostra api-token oppure ui-session con un breve hash della sessione, seguito da from e dall’indirizzo del client; pglens non ha identità utente, account o ruoli.',
  },
  'settings.command-audit-instance': {
    what: 'Comandi richiesti a questa istanza, dal più recente: cosa è stato eseguito, chi ha chiesto, il suo stato e quando è stato richiesto, preso in carico o scaduto.',
    source:
      'Endpoint di audit dei comandi del server per questa istanza, righe di command_audit (kind, args, requested_by, executed_at, outcome, detail); filtri e ricerca vengono eseguiti nel browser.',
    unit: 'voci di audit',
    window: 'Le 100 voci più recenti; orari nel fuso orario della console.',
    howToRead:
      'Ogni voce è un evento: la richiesta, il suo esito o la sua scadenza. Lo stato è requested, ok, error, rejected o expired. Una richiesta che l’agent non ha mai preso in carico compare come expired quando scade in coda.',
  },
  'settings.command-audit-table': {
    what: 'Una riga per voce di audit di questa istanza; la ricerca e i filtri Stato e Comando restringono le righe.',
    source:
      'Righe della tabella command_audit tramite l’endpoint di audit dei comandi del server per questa istanza; ricerca e filtri vengono eseguiti nel browser.',
    unit: 'voci di audit',
    window: 'Le 100 voci più recenti; la colonna Orario è nel fuso orario della console.',
    howToRead:
      'Orario è quando la voce è stata registrata. Richiesto da è la credenziale che ha chiesto, non una persona. Tempi e dettaglio aggiunge quando è stato richiesto, preso in carico o scade, e il dettaglio dell’esito.',
  },
  'settings.inventory-instances': {
    what: 'Ogni istanza PostgreSQL monitorata, con cluster, ruolo, versione, livello di permessi, ultimo invio, stato e disponibilità delle metriche dell’host.',
    source:
      'Endpoint delle istanze del server sulla tabella instances, con i nomi dei cluster dalla lista dei cluster; metriche dell’host dall’endpoint host di ogni istanza.',
    unit: 'istanze',
    window: 'Ultimo invio di ogni istanza.',
    howToRead:
      'Attivo significa che l’istanza ha inviato dati negli ultimi 60 secondi. Non assegnata: nessun cluster la possiede.',
  },
  'settings.inventory-instances-table': {
    what: 'Una riga per istanza monitorata; la ricerca e i filtri Ruolo e Stato la restringono, e la ricerca restringe anche la vista Database.',
    source:
      'Righe della tabella instances tramite l’endpoint delle istanze del server; ricerca e filtri vengono eseguiti nel browser.',
    unit: 'istanze',
    window: 'Ultimo invio di ogni istanza.',
    howToRead:
      'Il livello di permessi è T0 (legge solo le statistiche), T1 (esegue anche EXPLAIN) o T2 (annulla o termina anche le sessioni). Ultimo contatto è nel fuso orario della console. Metriche dell’host indica se l’agent sullo stesso host invia i dati del sistema operativo: Disponibile; Disponibile (dati non aggiornati) se l’ultimo campione ha più di 90 secondi; Non disponibile, con il motivo al passaggio del mouse.',
  },
  'settings.permission-tiers': {
    what: 'Ogni istanza ha un livello di permessi: T0 legge solo statistiche, T1 esegue anche EXPLAIN per i piani, T2 annulla o termina anche altre sessioni.',
    source:
      'Istanze contate per perm_tier dalla tabella instances; l’agent imposta il livello dal suo ruolo di monitoraggio (membro di pg_read_all_data: T1, di pg_signal_backend: T2); regole dal catalogo attivo dell’advisor per livello minimo; le azioni sono testo fisso di questa pagina.',
    unit: 'istanze e regole',
    window: 'Istanze attuali e catalogo attivo delle regole dell’advisor.',
    howToRead:
      'Ogni scheda conta le istanze di quel livello ed elenca le regole dell’advisor che lo richiedono per prime; un livello più alto esegue anche le regole dei livelli inferiori. Un’azione è consentita solo se anche l’istanza di destinazione la permette.',
  },
  'settings.inventory-agents': {
    what: 'Una riga derivata per istanza che indica se il suo agent sta inviando dati; il server non tiene un elenco separato degli agent.',
    source:
      'La tabella instances (last_seen, con up calcolato da essa) combinata con gli alert agent_down e instance_unreachable in corso dall’endpoint degli alert del server.',
    unit: 'agent',
    window: 'Ultimo invio di ogni istanza; alert in corso adesso.',
    howToRead:
      'Non raggiungibile significa che l’istanza non ha inviato dati negli ultimi 60 secondi, o che per essa è aperto un alert agent_down o instance_unreachable. Un’istanza può risultare Attivo nella vista Istanze mentre quell’alert è ancora aperto: l’alert si chiude alla prossima esecuzione delle regole di alert. Motivo indica quale caso si applica.',
  },
  'settings.inventory-agents-table': {
    what: 'Una riga per istanza: un nome agent derivato, l’indirizzo dell’istanza, ultimo invio e se l’agent sta inviando dati.',
    source:
      'Costruita nel browser dalla lista delle istanze e dagli alert in corso; il nome dell’agent è agent- più l’ID dell’istanza.',
    unit: 'agent',
    window: 'Ultimo invio di ogni istanza; orari nel fuso orario della console.',
    howToRead:
      'Lo stato è In invio o Non raggiungibile. La ricerca confronta agent, indirizzo e motivo.',
  },
  'settings.inventory-databases': {
    what: 'I database di ogni istanza monitorata, prima quelli non monitorati, con il motivo per cui l’agent li ha esclusi.',
    source:
      'Endpoint dei database delle istanze del server sulla tabella databases, scritta da ogni inventario dell’agent; una sezione per ogni istanza trovata dalla ricerca Istanze.',
    unit: 'database',
    window: 'Ultimo inventario dell’agent di ogni istanza.',
    howToRead:
      'I database non monitorati vengono prima perché il loro motivo di esclusione richiede attenzione. La ricerca Istanze restringe le istanze elencate qui.',
  },
  'settings.inventory-databases-instance': {
    what: 'Database di questa istanza segnalati dal suo agent; Monitorato indica se pglens ne raccoglie le metriche e Motivo di esclusione dice perché no.',
    source:
      'Endpoint dei database delle istanze del server sulla tabella databases (datname, monitored, skip_reason); il conteggio dei database non monitorati viene dalla stessa risposta.',
    unit: 'database',
    window: 'Ultimo inventario dell’agent di questa istanza.',
    howToRead:
      'L’agent mantiene solo i database più attivi fino al suo budget, 10 come predefinito; gli altri vengono esclusi con db_budget. Il numero di database non monitorati è sopra la tabella.',
  },
  'settings.inventory-databases-table': {
    what: 'Una riga per database di questa istanza: nome, se è monitorato e il motivo di esclusione quando non lo è.',
    source:
      'Righe della tabella databases tramite l’endpoint dei database delle istanze del server; ricerca e filtro Monitorato vengono eseguiti nel browser.',
    unit: 'database',
    window: 'Ultimo inventario dell’agent di questa istanza.',
    howToRead:
      'Un trattino in Motivo di esclusione indica database monitorato. db_budget: l’agent ha raggiunto il suo budget di database.',
  },
}
