import type { CaptionEntry } from '@/lib/captions'
import type { alertsCaptions as english } from '@/lib/captionsets/alerts'

export const alerts: Readonly<Record<keyof typeof english, CaptionEntry>> = {
  'alerts.list': {
    what: 'Alert generati dalle regole di alert, con stato, gravità, cluster, istanza e ora di inizio.',
    source:
      'Endpoint alert del server sulla tabella alerts, riempita dal motore di alert a partire dalle regole; i 500 valutati più di recente. La soppressione è letta dai silenziamenti salvati.',
    unit: 'alert',
    window:
      'Ogni stato conservato dal server; i risolti restano in elenco. Per impostazione predefinita il motore rivaluta le regole ogni 30 secondi.',
    howToRead:
      'Ordinati: prima Firing, poi Resolved; dentro ogni stato critical prima di warning, poi info. I filtri restringono le righe caricate nel browser e restano nell’indirizzo. Notes indica il silenziamento che sopprime un alert, con il tempo che gli resta.',
  },
  'alerts.detail': {
    what: 'Un alert per intero: regola, stato, etichette e quando è iniziato, è stato valutato l’ultima volta e si è risolto.',
    source:
      'Endpoint del server per una chiave di alert: la riga più recente della tabella alerts con quella chiave.',
    window:
      'Letto all’apertura; inizio, ultima valutazione e risoluzione nel fuso orario della console.',
    howToRead:
      'Le etichette sono gli identificativi dell’alert. Un silenziamento corrisponde all’alert tramite queste etichette.',
  },
  'alerts.delivery': {
    what: 'Le variabili d’ambiente del server che configurano ogni canale di invio degli alert; da questa pagina non si cambia nulla.',
    source:
      'La lista dei canali della console (Slack, webhook generico, Google Chat) e le variabili lette dal server all’avvio: PGLENS_ALERT_SLACK_WEBHOOK_URL, PGLENS_WEBHOOK_URL e PGLENS_GOOGLE_CHAT_WEBHOOK_URL, ciascuna con la variante file.',
    window:
      'Una modifica dell’ambiente richiede il riavvio del server; un indirizzo salvato da Impostazioni › Notifiche si applica subito e lo sovrascrive.',
    howToRead:
      'Senza un canale configurato gli alert restano salvati ma non vengono inviati. Un silenziamento blocca solo la notifica; l’alert resta visibile e continua a essere valutato.',
  },
  'alerts.summary': {
    what: 'Conteggi degli alert caricati: Firing per gravità, Suppressed da un silenziamento e Resolved.',
    source:
      'Conteggiato nel browser sugli alert caricati dall’endpoint alert del server (tutti gli stati, i 500 valutati più di recente).',
    unit: 'alert',
    window: 'Gli alert caricati; i filtri dell’elenco non cambiano i conteggi.',
    howToRead:
      'Un alert soppresso è contato solo in Suppressed e non in Firing, quindi i conteggi Firing sono gli alert che notificano. Un alert risolto che era silenziato è contato in entrambi.',
  },
  'alerts.filter-state': {
    what: 'Mostra solo gli alert nello stato scelto; Firing è il predefinito e All states rimuove il filtro.',
    source:
      'La colonna stato della tabella alerts (firing o resolved; gli episodi pending restano nella memoria del motore e non vengono mai salvati) sugli alert caricati; il filtro è eseguito nel browser.',
    unit: 'stato dell’alert',
    window: 'Gli alert caricati.',
    howToRead:
      'Firing: la condizione della regola è rimasta vera per tutta la durata. Resolved: non vale più, oppure il soggetto ha smesso di inviare dati. Pending non viene mai salvato, quindi il filtro Pending non mostra alcun alert.',
  },
  'alerts.filter-severity': {
    what: 'Mostra solo gli alert della gravità scelta: critical, warning o info.',
    source:
      'La colonna gravità della tabella alerts, impostata dalla regola che ha generato l’alert; il filtro è eseguito nel browser.',
    unit: 'gravità dell’alert',
    window: 'Gli alert caricati.',
  },
  'alerts.filter-cluster': {
    what: 'Mostra solo gli alert il cui ID cluster coincide con il numero digitato; nomi e numeri parziali non corrispondono a nulla.',
    source:
      'La colonna cluster_id della tabella alerts, un numero decimale; il filtro lo confronta nel browser con il testo digitato.',
    unit: 'ID cluster',
    window: 'Gli alert caricati.',
    howToRead: 'Lascialo vuoto per tutti i cluster. Un alert senza cluster non corrisponde mai.',
  },
  'alerts.filter-suppression': {
    what: 'Mostra solo gli alert soppressi da un silenziamento, o solo quelli non soppressi.',
    source:
      'Il flag soppresso che il server calcola per ogni alert dai silenziamenti attivi ora; il filtro è eseguito nel browser.',
    unit: 'soppresso o no',
    window: 'Silenziamenti attivi ora.',
  },
  'events.timeline': {
    what: 'Eventi della flotta nell’intervallo di tempo scelto, come failover, riavvii e cambi di identità.',
    source:
      'Endpoint eventi del server sulla tabella events, scritta dall’ingest e dal motore di alert; filtrata per cluster, tipo e intervallo di tempo sul server.',
    unit: 'eventi',
    window:
      'L’intervallo scelto, dal più recente, col tetto di eventi (100 per impostazione predefinita, massimo 1.000); oltre 90 giorni gli eventi vengono eliminati.',
    howToRead:
      'La gravità è assegnata dalla console in base al tipo di evento. Details indica cosa controllare per quel tipo. Un tipo sconosciuto alla console mostra il nome originale. Cluster, tipo, intervallo e limite restano nell’indirizzo della pagina, così una vista si può condividere; il piè di tabella conta gli eventi.',
  },
  'events.alert-status': {
    what: 'Ogni alert con gravità, stato e l’indicazione se un silenziamento lo sopprime, separato dalla cronologia eventi.',
    source:
      'Endpoint alert del server sulla tabella alerts con i silenziamenti salvati; una riga per chiave di alert.',
    unit: 'alert',
    window: 'Tutti gli stati conservati dal server, i 500 valutati più di recente.',
    howToRead:
      'Suppression è Unknown se i silenziamenti non si sono potuti caricare. Un alert soppresso continua a essere valutato.',
  },
  'events.filter-cluster': {
    what: 'Elenca solo gli eventi del cluster il cui ID è digitato; il server rifiuta tutto ciò che non è un numero.',
    source:
      'Il parametro cluster_id dell’endpoint eventi del server, confrontato con la colonna cluster_id della tabella events.',
    unit: 'ID cluster',
    window: 'L’intervallo di tempo scelto.',
    howToRead: 'Lascialo vuoto per tutti i cluster.',
  },
  'events.filter-type': {
    what: 'Elenca solo gli eventi di un tipo, come failover_detected o slot_lost.',
    source:
      'Il parametro type dell’endpoint eventi del server, confrontato con la colonna type della tabella events; le scelte sono i tipi presenti negli eventi caricati.',
    unit: 'tipo di evento',
    window: 'L’intervallo di tempo scelto.',
  },
  'events.filter-limit': {
    what: 'Il massimo di eventi restituiti dal server per l’intervallo: 100, 500 o 1.000.',
    source:
      'Il parametro limit dell’endpoint eventi del server; il server tiene gli eventi più recenti e limita a 1.000 ogni valore più alto.',
    unit: 'eventi',
    window: 'L’intervallo di tempo scelto.',
    howToRead:
      'Quando l’elenco raggiunge 1.000 la pagina segnala che possono esistere eventi più vecchi fuori dalla vista; restringi l’intervallo o i filtri.',
  },
  'rules.list': {
    what: 'Regole di alert predefinite e salvate, con gravità, soglia, durata e se sono attive.',
    source:
      'Endpoint regole di alert del server: le regole predefinite compilate nel server più le righe attive della tabella alert_rules.',
    unit: 'regole',
    window: 'Catalogo live; una modifica si applica alla valutazione successiva.',
    howToRead:
      'Le regole del livello T0 su salute di agent e cluster sono sempre attive e non modificabili. La soglia è nell’unità della metrica della regola, e una regola generata da un tipo di evento non ne ha. La durata è per quanto tempo la condizione deve restare vera prima che l’alert scatti.',
  },
  'rules.edit': {
    what: 'Modifica soglia, durata, gravità e stato attivo di una regola; il server valida e salva la modifica.',
    source:
      'Endpoint regole di alert del server per una regola: aggiorna la riga alert_rules e restituisce i valori salvati.',
    unit: 'impostazioni',
    window: 'Si applica alla prossima valutazione delle regole di alert.',
    howToRead:
      'Ogni campo indica il suo effetto sotto di sé. Non cambia nulla finché Save rule non va a buon fine; Cancel scarta il modulo.',
  },
  'silences.list': {
    what: 'Silenziamenti che bloccano le notifiche degli alert: in attesa, attivi e scaduti, con matcher, motivo e finestra temporale.',
    source:
      'Endpoint silenziamenti del server sulla tabella silences, ordinati per fine dalla più recente e con lo stesso tetto della lista alert; lo stato è calcolato nel browser dalla finestra.',
    unit: 'silenziamenti',
    window:
      'In attesa prima di Start, attivo da Start a End, scaduto dopo End; orari nel fuso orario della console.',
    howToRead:
      'Un silenziamento sopprime gli alert che corrispondono a tutti i matcher. Blocca solo le notifiche; l’alert resta in elenco e continua a essere valutato. End silence lo ferma in anticipo o annulla uno in attesa.',
  },
  'alerts.table': {
    what: 'Una riga per alert: chiave, gravità, stato, riepilogo, cluster, istanza, ora di inizio e Notes.',
    source:
      'Endpoint alert del server sulla tabella alerts, riempita dal motore di alert a partire dalle regole; la soppressione è letta dai silenziamenti salvati.',
    unit: 'alert',
    window: 'Gli alert rimasti dopo i filtri sopra; orari nel fuso orario della console.',
    howToRead:
      'La chiave dell’alert apre il dettaglio. Notes indica il silenziamento che sopprime un alert, con il tempo che gli resta. Un alert dell’agent rimanda a una checklist.',
  },
  'events.table': {
    what: 'Una riga per evento della flotta: ora, titolo, gravità, tipo, cluster, istanza e Details.',
    source:
      'Endpoint eventi del server sulla tabella events, scritta dall’ingest e dal motore di alert.',
    unit: 'eventi',
    window:
      'L’intervallo di tempo scelto, dal più recente, limitato al tetto di eventi; orari nel fuso orario della console.',
    howToRead:
      'Type è il nome grezzo dell’evento, sempre mostrato così un evento sconosciuto alla console resta visibile. Details indica cosa controllare per quel tipo.',
  },
  'events.alert-status-table': {
    what: 'Una riga per alert con gravità, stato e se un silenziamento lo sopprime.',
    source:
      'Endpoint alert del server sulla tabella alerts, con i silenziamenti salvati letti per la soppressione.',
    unit: 'alert',
    window: 'Tutti gli stati conservati dal server, i 500 valutati più di recente.',
    howToRead:
      'Suppression è Unknown se i silenziamenti non si sono potuti caricare. Un alert soppresso continua a essere valutato.',
  },
  'rules.table': {
    what: 'Una riga per regola: cosa controlla, il suo livello di permessi, gravità, soglia, durata e se è attiva.',
    source:
      'Endpoint regole di alert del server: le regole predefinite compilate nel server più le righe attive della tabella alert_rules.',
    unit: 'regole; soglie nell’unità mostrata accanto al numero',
    window: 'Catalogo live; una modifica si applica alla valutazione successiva.',
    howToRead:
      'Threshold indica il valore che la metrica deve superare, con la sua unità: secondi, una quota di max_connections, una quota di disco libero o transazioni. Una regola generata da un evento non ha soglia.',
  },
  'silences.table': {
    what: 'Una riga per silenziamento: matcher, motivo, inizio, fine e stato.',
    source:
      'Endpoint silenziamenti del server sulla tabella silences; lo stato è calcolato nel browser dalla finestra.',
    unit: 'silenziamenti',
    window:
      'In attesa prima di Start, attivo da Start a End, scaduto dopo End; orari nel fuso orario della console.',
    howToRead:
      'Un silenziamento sopprime gli alert che corrispondono a tutti i matcher. Blocca solo le notifiche; l’alert resta in elenco e continua a essere valutato.',
  },
}
