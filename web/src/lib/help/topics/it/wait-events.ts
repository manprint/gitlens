import type { HelpTopic } from '../../types'

export const waitEventsTopic: HelpTopic = {
  id: 'wait-events',
  group: 'instance',
  title: 'Wait events',
  summary:
    '{{ui:shell.nav.waitEvents}} mostra che cosa fanno i processi occupati del server e che cosa aspettano, dalla Active Session History ({{ui:shell.crumb.ash}}) campionata dall’agent. Serve a capire se il tempo va in {{ui:ash.waitGroup.cpu}}, {{ui:ash.waitGroup.io}}, lock, client o altre attese.',
  page: {
    path: '/instances/:instanceId/ash',
    label: 'Apri {{ui:shell.nav.waitEvents}}',
  },
  sections: [
    {
      id: 'overview',
      title: 'A cosa serve la pagina',
      blocks: [
        {
          kind: 'text',
          text: '{{ui:shell.nav.waitEvents}} risponde a una domanda: dove spendono tempo i processi occupati del server, e che cosa aspettano? Ogni connessione client è servita da un processo del server, detto backend. L’agent campiona i backend una volta al secondo e salva i conteggi per finestra di 10 secondi; la pagina mostra la media dei backend non idle. Il campionamento è statistico: mostra dove va il tempo, non durate esatte.',
        },
      ],
      subsections: [
        {
          id: 'how-to-open',
          title: 'Come aprirla',
          blocks: [
            {
              kind: 'steps',
              items: [
                'Apri {{ui:shell.nav.fleet}} su `/`, scegli il cluster, poi l’istanza.',
                'Apri la tab {{ui:shell.nav.waitEvents}} nell’intestazione dell’istanza, oppure la voce {{ui:shell.nav.waitEvents}} nel gruppo {{ui:alerts.column.instance}} della sidebar.',
                'Scegli l’intervallo con il selettore etichettato Time range preset. I grafici coprono quell’intervallo in bucket da 10 secondi o più, al massimo 180 per grafico.',
              ],
            },
          ],
        },
        {
          id: 'page-controls',
          title: 'Controlli della pagina',
          blocks: [
            {
              kind: 'text',
              text: 'I controlli che cambiano la pagina sono l’intervallo, il layout dei grafici, il percorso di drill-down e il parametro URL `db`. La sidebar e le tab dell’istanza portano `db` alle altre viste di questo server ed eliminano gli altri parametri URL di questa pagina.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Time range preset',
                  meaning:
                    'Imposta l’intervallo letto da ogni grafico e dalla lista {{ui:ash.top.title}}.',
                  action:
                    'Allarga l’intervallo quando un grafico mostra gap o l’avviso di campionamento. Vedi [Risoluzione dei problemi](/help/wait-events#troubleshooting).',
                },
                {
                  term: '{{ui:ash.layout.label}}',
                  meaning:
                    'Passa da un grafico per gruppo di attesa a un solo grafico impilato. Vedi [{{ui:ash.layout.label}}](/help/wait-events#chart-layout).',
                },
                {
                  term: 'Drill path',
                  meaning:
                    'Il breadcrumb {{ui:ash.drill.label}} mostra il livello in cui sei e permette di tornare indietro. Vedi [Percorso di drill-down](/help/wait-events#drill-path).',
                },
                {
                  term: 'db',
                  meaning: 'Parametro URL che limita i campioni a un solo database.',
                },
              ],
            },
          ],
        },
        {
          id: 'data-sources',
          title: 'Da dove arrivano i dati',
          blocks: [
            {
              kind: 'text',
              text: 'L’agent legge `pg_stat_activity` una volta al secondo. Conta i backend client e i worker paralleli il cui stato non è idle, escludendo le sue sessioni pglens. Un backend senza wait event è contato come {{ui:ash.waitGroup.cpu}}. L’agent salva i conteggi per finestra di 10 secondi, e per finestra si conservano al massimo 100 chiavi di attesa: il resto viene raggruppato in other, così il totale resta invariato.',
            },
            {
              kind: 'note',
              tone: 'info',
              text: '{{ui:shell.crumb.ash}} usa un campionamento statistico di 1 secondo, quindi le query più brevi di un secondo sono sottorappresentate. I grafici mostrano i gruppi più grandi dell’intervallo e raggruppano il resto in other.',
            },
          ],
        },
      ],
    },
    {
      id: 'drill-path',
      title: 'Percorso di drill-down',
      blocks: [
        {
          kind: 'text',
          text: 'Il breadcrumb etichettato {{ui:ash.drill.label}} mostra il livello in cui sei: {{ui:ash.group.waitEventType}}, poi {{ui:shell.nav.waitEvents}}, poi {{ui:shell.crumb.queries}}. Ogni livello restringe il precedente. Ogni voce del breadcrumb che non è il livello corrente è un pulsante: riporta a quel livello e cancella i filtri sotto di esso. La serie other non apre alcun livello.',
        },
        {
          kind: 'steps',
          items: [
            'Parti da {{ui:ash.group.waitEventType}} e trova il tipo di attesa che domina.',
            'Seleziona la serie di quel tipo. La pagina apre {{ui:shell.nav.waitEvents}} per quel tipo.',
            'Seleziona il wait event. La pagina apre {{ui:shell.crumb.queries}} per quell’evento.',
            'Leggi la lista {{ui:ash.top.title}} sotto il grafico {{ui:shell.crumb.queries}}.',
          ],
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:ash.group.waitEventType}}',
              meaning:
                'Livello superiore: una serie per tipo di wait event, per esempio {{ui:ash.waitGroup.cpu}}, IO o Lock.',
              action: 'Parti da qui per vedere quale tipo di attesa prende più tempo.',
            },
            {
              term: '{{ui:shell.nav.waitEvents}}',
              meaning:
                'Secondo livello: una serie per wait event dentro il tipo selezionato. Il breadcrumb riporta {{ui:shell.nav.waitEvents}}: seguito dal tipo selezionato, oppure all.',
              action:
                'Annota il nome dell’evento, poi selezionalo per vedere le query campionate mentre aspettano in quell’evento.',
            },
            {
              term: '{{ui:shell.crumb.queries}}',
              meaning:
                'Terzo livello, raggiunto selezionando un wait event: una serie per query id, disegnata come un grafico impilato, con la lista {{ui:ash.top.title}} sotto.',
              action:
                'Apri una query da {{ui:ash.top.title}} per vedere lo statement e i piani in [{{ui:shell.nav.queryPerformance}}](/help/query-performance#query-detail).',
            },
          ],
        },
      ],
    },
    {
      id: 'session-dimensions',
      title: 'Utenti, indirizzi client e applicazioni',
      blocks: [
        {
          kind: 'text',
          text: 'Il controllo {{ui:ash.groupBy.label}} passa il grafico dal percorso di {{ui:ash.groupBy.wait}} a tre dimensioni della sessione: {{ui:ash.group.usename}}, {{ui:ash.group.clientAddr}} e {{ui:ash.group.applicationName}}. Ognuna è disegnata in un solo grafico impilato. Una sessione con valore vuoto, per esempio un socket locale per l’indirizzo client, è mostrata come {{ui:ash.dimension.unknown}}.',
        },
        {
          kind: 'steps',
          items: [
            'Scegli {{ui:ash.group.usename}}, {{ui:ash.group.clientAddr}} o {{ui:ash.group.applicationName}} in {{ui:ash.groupBy.label}}.',
            'Seleziona una serie. La pagina filtra su quel valore e torna a {{ui:ash.group.waitEventType}}, così vedi cosa aspettano quelle sessioni.',
            'Il filtro appare come chip sopra il grafico. Restringe ogni grafico e {{ui:ash.top.title}}; il pulsante sulla chip lo rimuove.',
          ],
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'usename, client_addr, application_name',
              meaning:
                'Parametri URL dei filtri. Il valore `-` seleziona le sessioni con valore vuoto.',
            },
            {
              term: 'checks.ash.dimensions',
              meaning:
                'Impostazione dell’agent che elenca le dimensioni conservate, per default tutte e tre. Una dimensione esclusa è memorizzata vuota e appare come {{ui:ash.dimension.unknown}}.',
              action:
                'Escludi una dimensione quando i suoi valori non devono arrivare al server pglens.',
            },
          ],
        },
      ],
    },
    {
      id: 'chart-layout',
      title: '{{ui:ash.layout.label}}',
      blocks: [
        {
          kind: 'text',
          text: 'Il toggle etichettato {{ui:ash.layout.label}}, nell’intestazione della pagina, passa tra due viste degli stessi dati. {{ui:ash.layout.separate}} disegna un grafico per gruppo di attesa, ciascuno con la propria scala, così un gruppo piccolo non viene schiacciato da uno grande. {{ui:ash.layout.stacked}} disegna un solo grafico con tutte le serie impilate, quindi l’altezza è la media dei backend non idle. {{ui:ash.layout.separate}} è il valore predefinito e non viene scritto nell’URL; {{ui:ash.layout.stacked}} resta come `view=stacked`. Le query usano sempre il grafico impilato.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:ash.layout.separate}}',
              meaning:
                'Layout predefinito: una scheda per gruppo di attesa, ciascuna con scala, legenda e sottotitolo propri.',
              action:
                'Usalo per trovare il gruppo in cui cresce una serie, e per confrontare le serie dentro un gruppo.',
            },
            {
              term: '{{ui:ash.layout.stacked}}',
              meaning:
                'Un solo grafico: ogni fascia è una serie, e l’altezza totale è la media dei backend non idle. Tutti i gruppi condividono una scala, quindi una fascia grande può schiacciarne una piccola.',
              action:
                'Usalo per vedere il carico totale in una sola vista, poi passa a {{ui:ash.layout.separate}} per leggere i gruppi piccoli.',
            },
          ],
        },
      ],
    },
    {
      id: 'wait-group-cards',
      title: 'Schede dei gruppi di attesa',
      blocks: [
        {
          kind: 'text',
          text: 'Nel layout {{ui:ash.layout.separate}}, ogni gruppo di attesa è una scheda. Il titolo è il nome del gruppo, il sottotitolo riporta la media dei backend attivi nella scheda e la sua quota su tutte le attese. La scheda ha una scala, un grafico e una legenda propri. I gruppi sono: {{ui:ash.waitGroup.cpu}}, {{ui:ash.waitGroup.io}}, {{ui:ash.waitGroup.locks}}, {{ui:ash.waitGroup.client}}, {{ui:ash.waitGroup.other}}.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:ash.waitGroup.cpu}}',
              meaning:
                'Backend che non riportano un wait event, contati come in esecuzione su {{ui:ash.waitGroup.cpu}}. Tipo di serie: {{ui:ash.waitGroup.cpu}}. Un valore alto significa che il server è occupato a calcolare, non ad aspettare.',
              action:
                'Verifica gli statement che usano più tempo in [{{ui:shell.nav.queryPerformance}}](/help/query-performance#query-inspector).',
            },
            {
              term: '{{ui:ash.waitGroup.io}}',
              meaning:
                'Backend in attesa di una lettura o scrittura su storage. Tipo di serie: IO.',
              action:
                'Verifica gli statement con un {{ui:queries.overview.cacheHitRatio}} basso in [{{ui:shell.nav.queryPerformance}}](/help/query-performance#statement-statistics), poi gli indici delle tabelle che leggono in [{{ui:shell.nav.indexAnalysis}}](/help/indexes).',
            },
            {
              term: '{{ui:ash.waitGroup.locks}}',
              meaning:
                'Backend in attesa di un lock pesante (Lock), di un lock leggero interno (LWLock) o di un buffer (BufferPin, Buffer). Tipi di serie: Lock, LWLock, BufferPin, Buffer.',
              action:
                'Apri [{{ui:shell.nav.locks}}](/help/locks) per trovare la {{ui:locks.blocking.blockingSession}}.',
            },
            {
              term: '{{ui:ash.waitGroup.client}}',
              meaning:
                'Backend in attesa che l’applicazione client invii o riceva dati ({{ui:locks.sessions.client}}), oppure di un altro processo del server come un worker parallelo o la replica (IPC). Tipi di serie: {{ui:locks.sessions.client}}, IPC.',
              action:
                'Per {{ui:locks.sessions.client}}, verifica l’applicazione: può essere lenta a inviare richieste o a leggere i risultati. Per IPC, verifica i worker paralleli e la replica.',
            },
            {
              term: '{{ui:ash.waitGroup.other}}',
              meaning:
                'Attese di timer, attività in background, estensioni e altre: tipi di serie Timeout, {{ui:locks.activity.title}}, Extension, e ogni tipo non classificato sopra.',
              action:
                'Un tipo che la pagina non conosce finisce in questo gruppo, e la sua legenda riporta {{ui:locks.detail.waitEvent}} type seguito dal nome del tipo.',
            },
          ],
        },
      ],
    },
    {
      id: 'legend',
      title: 'Legenda',
      blocks: [
        {
          kind: 'text',
          text: 'Ogni scheda ha una legenda. Una riga mostra il nome della serie, un riquadro colorato e la media dei backend attivi di quella serie. Clicca una riga per mostrare solo quella serie; la riga resta marcata come premuta finché è isolata.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:ash.chart.axis}}',
              meaning:
                'Media dei backend non idle della serie, calcolata come campioni divisi per i tick di campionamento del bucket.',
            },
            {
              term: 'Legend row',
              meaning:
                'Clicca per mostrare solo quella serie. La riga è marcata come premuta finché è isolata.',
              action:
                'Isola la serie che cresce per leggerne la forma esatta, poi cliccala di nuovo per vedere tutte le serie.',
            },
          ],
        },
      ],
    },
    {
      id: 'wait-types',
      title: 'Riferimento dei {{ui:ash.group.waitEventType}}',
      blocks: [
        {
          kind: 'text',
          text: 'Le descrizioni seguenti sono quelle che la pagina usa per ogni tipo di wait event.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:ash.waitGroup.cpu}}',
              meaning:
                'Backend in esecuzione su {{ui:ash.waitGroup.cpu}}, senza wait event riportato.',
            },
            {
              term: 'IO',
              meaning: 'In attesa di una lettura o scrittura su storage.',
            },
            {
              term: 'Lock',
              meaning: 'In attesa di un lock pesante tenuto da un’altra transazione.',
            },
            {
              term: 'LWLock',
              meaning: 'In attesa di un lock leggero interno (contesa su buffer, WAL, catalogo).',
            },
            {
              term: 'BufferPin',
              meaning: 'In attesa dell’accesso esclusivo a un buffer dei dati.',
            },
            {
              term: 'Buffer',
              meaning: 'In attesa dell’accesso esclusivo a un buffer dei dati.',
            },
            {
              term: '{{ui:locks.sessions.client}}',
              meaning: 'In attesa che l’applicazione client invii o riceva dati.',
            },
            {
              term: 'IPC',
              meaning: 'In attesa di un altro processo del server (worker paralleli, replica).',
            },
            {
              term: 'Timeout',
              meaning: 'In pausa fino allo scatto di un timer.',
            },
            {
              term: '{{ui:locks.activity.title}}',
              meaning: 'Processo in background fermo nel suo ciclo principale.',
            },
            {
              term: 'Extension',
              meaning: 'In attesa all’interno di un’estensione.',
            },
            {
              term: 'other',
              meaning: '{{ui:shell.nav.waitEvents}} non classificati sopra.',
            },
          ],
        },
      ],
    },
    {
      id: 'queries',
      title: '{{ui:shell.crumb.queries}}',
      blocks: [
        {
          kind: 'text',
          text: 'Al livello {{ui:shell.crumb.queries}} ogni serie è un query id, disegnato come un unico grafico impilato qualunque sia il layout. La pagina mostra questa nota: {{ui:shell.crumb.queries}} are plotted in one stacked chart: each series is a query, not a wait group. Il query id arriva dalla colonna `query_id` di `pg_stat_activity`, valorizzata solo quando `compute_query_id` è `on`, oppure quando è `auto` e un modulo come `pg_stat_statements` è caricato.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:findings.evidence.queryid}}',
              meaning:
                'Una serie per query id, lo stesso id mostrato da {{ui:shell.nav.queryPerformance}}. Le query più piccole sono raggruppate in other.',
              action:
                'Apri la query da [{{ui:ash.top.title}}](/help/wait-events#top-queries) per vedere lo statement e i piani.',
            },
          ],
        },
      ],
    },
    {
      id: 'top-queries',
      title: '{{ui:ash.top.title}}',
      blocks: [
        {
          kind: 'text',
          text: 'Sotto il grafico {{ui:shell.crumb.queries}}, {{ui:ash.top.title}} elenca le query campionate più spesso nell’ambito di attesa scelto. Ogni voce è intitolata {{ui:locks.sessions.query}} con il suo query id e porta alla pagina della query; mostra la media dei backend attivi, il numero di campioni e il testo normalizzato della query. Le voci sono ordinate per campioni, e la lista tiene le prime 20. {{ui:shell.crumb.ash}} non legge mai il testo live delle query: il testo è quello normalizzato di `pg_stat_statements`, e i valori letterali non sono disponibili.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Ranked by samples',
              meaning:
                'La frase di ambito sopra la lista indica il tipo di evento o il wait event a cui la classifica è limitata, oppure che copre ogni wait event.',
            },
            {
              term: '{{ui:ash.chart.axis}}',
              meaning:
                'Backend medi della query: campioni divisi per i tick di campionamento dell’intero intervallo. Riporta Unknown average active backends quando manca.',
              action:
                'Confrontala con Mean time e Calls dello statement in [{{ui:shell.nav.queryPerformance}}](/help/query-performance#statement-statistics) prima di intervenire.',
            },
            {
              term: 'samples across ticks',
              meaning:
                'Numero di campioni presi per la query, e dei tick di campionamento dell’intervallo.',
            },
            {
              term: 'No top queries',
              meaning: 'Nessun query id è stato campionato nell’intervallo selezionato.',
              action:
                'Allarga l’intervallo, poi leggi l’avviso sui query id in [Risoluzione dei problemi](/help/wait-events#troubleshooting).',
            },
          ],
        },
      ],
    },
    {
      id: 'url-parameters',
      title: 'Parametri dell’URL',
      blocks: [
        {
          kind: 'text',
          text: 'Lo stato della pagina è nell’indirizzo, quindi una vista può essere condivisa. Il layout impilato è scritto come `view=stacked`; {{ui:ash.layout.separate}} è il valore predefinito e non viene scritto.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'group',
              meaning:
                'Livello dell’approfondimento: `wait_event_type` se assente, oppure `wait_event`, oppure `queryid`.',
            },
            {
              term: 'wait_event_type',
              meaning:
                'Il tipo di wait event selezionato al livello {{ui:shell.nav.waitEvents}}. Assente significa tutti i tipi.',
            },
            {
              term: 'wait_event',
              meaning:
                'Il wait event selezionato per arrivare al livello {{ui:shell.crumb.queries}}. Limita il grafico {{ui:shell.crumb.queries}} e {{ui:ash.top.title}} a quell’evento.',
            },
            {
              term: 'db',
              meaning: 'Limita i campioni a un solo database.',
            },
            {
              term: 'view',
              meaning:
                '`stacked` seleziona il layout impilato. Assente significa {{ui:ash.layout.separate}}.',
            },
          ],
        },
      ],
    },
    {
      id: 'compare-periods',
      title: 'Confrontare due periodi',
      blocks: [
        {
          kind: 'text',
          text: 'Passa la pagina a {{ui:ash.compare.modeCompare}} per vedere le sessioni attive medie di ogni gruppo in due periodi affiancati. Il periodo B è l’intervallo del selettore temporale; il periodo A è {{ui:ash.compare.preset.previous}}, {{ui:ash.compare.preset.yesterday}}, {{ui:ash.compare.preset.week}} o un intervallo {{ui:ash.compare.preset.custom}}. I filtri della pagina (database, tipo di wait event, utente, client, applicazione) valgono per entrambi i periodi.',
        },
        {
          kind: 'steps',
          items: [
            'Scegli il periodo B con il selettore temporale.',
            'Fai clic su {{ui:ash.compare.modeCompare}}, poi scegli {{ui:ash.compare.presetLabel}}.',
            'Scegli il {{ui:ash.compare.groupLabel}}: tipo di wait event, wait event, query o una dimensione di sessione.',
            'Leggi {{ui:ash.compare.colRatio}}: un rapporto di ×2,0 o più è evidenziato; i gruppi {{ui:ash.compare.new}} compaiono solo in B, quelli {{ui:ash.compare.gone}} solo in A.',
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'Il confronto legge `GET /api/v1/ash/compare`, con la formula della tabella delle query principali: campioni sui tick di campionamento. I filtri restringono i campioni, mai i tick.',
        },
      ],
    },
    {
      id: 'services',
      title: 'Servizi, endpoint e trace id',
      blocks: [
        {
          kind: 'text',
          text: "Le applicazioni strumentate con sqlcommenter aggiungono a ogni statement un commento come `/*application='shop',route='/cart',traceparent='00-…-01'*/`. L'agent legge i tag dello statement campionato, tiene solo i valori dei tag e non invia mai il testo dello statement. {{ui:ash.group.service}} è il tag `application`; {{ui:ash.group.endpoint}} è `route`, altrimenti `controller#action`. Le sessioni senza tag appaiono come {{ui:ash.dimension.untagged}}.",
        },
        {
          kind: 'steps',
          items: [
            'Scegli {{ui:ash.group.service}} o {{ui:ash.group.endpoint}} in {{ui:ash.groupBy.label}} per dividere le sessioni attive per applicazione o route.',
            'Fai clic su una serie per filtrare la pagina su quel valore; il filtro si combina con le altre dimensioni.',
            "Le righe di {{ui:shell.nav.logs}} e dei piani automatici mostrano i primi 8 caratteri del trace id. Con `PGLENS_TRACE_URL_TEMPLATE` impostato sul server aprono la traccia nello strumento di tracing; altrimenti copia l'id completo.",
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'PostgreSQL conserva solo i primi `track_activity_query_size` byte di uno statement (1024 di default), quindi uno statement lungo perde il commento finale e risulta senza tag. Aumenta il parametro (serve un riavvio) o tieni corto il commento. I rollup orari non conservano queste dimensioni.',
        },
      ],
    },
    {
      id: 'reading-together',
      title: 'Lettura dei gruppi insieme',
      blocks: [
        {
          kind: 'text',
          text: 'Un solo gruppo raramente spiega un server lento. Leggi i gruppi insieme, poi segui la pagina corrispondente.',
        },
        {
          kind: 'steps',
          items: [
            'Se {{ui:ash.waitGroup.locks}} cresce, apri [{{ui:shell.nav.locks}}](/help/locks) e trova la {{ui:locks.blocking.blockingSession}}.',
            'Se {{ui:ash.waitGroup.io}} cresce, verifica gli statement con un {{ui:queries.overview.cacheHitRatio}} basso in [{{ui:shell.nav.queryPerformance}}](/help/query-performance#statement-statistics).',
            'Se {{ui:ash.waitGroup.cpu}} cresce senza attese, leggi i piani degli statement principali in [{{ui:shell.nav.queryPerformance}}](/help/query-performance#execution-plan).',
            'Se {{ui:locks.sessions.client}} cresce, verifica l’applicazione che invia richieste o legge i risultati lentamente.',
          ],
        },
      ],
    },
    {
      id: 'troubleshooting',
      title: 'Risoluzione dei problemi',
      blocks: [
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:shell.crumb.ash}} sampling disabled.',
              meaning:
                'L’agent ha il campionamento {{ui:shell.crumb.ash}} disattivato, quindi la pagina non ha campioni. L’avviso dice: The configuration key checks.ash.enabled in the agent configuration file is off.',
              action:
                'Imposta `checks.ash.enabled` a true nel file di configurazione dell’agent. Tenere {{ui:shell.crumb.ash}} disattivato evita il suo costo di campionamento, ma toglie la visibilità sull’analisi delle attese.',
            },
            {
              term: '{{ui:shell.crumb.ash}} sampling warning:',
              meaning:
                'L’avviso del server per l’intervallo: compare quando l’intervallo contiene meno di 60 tick di campionamento, uno per ogni secondo in cui l’agent ha campionato. È seguito dal numero di campioni di backend raccolti, uno per backend non idle per tick. La pagina indica che le conclusioni non sono affidabili.',
            },
            {
              term: 'No non-null query_id was observed in this {{ui:shell.crumb.ash}} range',
              meaning:
                'Il livello {{ui:shell.crumb.queries}} non ha query id. L’avviso riporta il motivo e Required: `compute_query_id=on`. Quando `compute_query_id` è off, l’avviso rimanda alla Readiness tab dell’istanza, che elenca la voce e il suo rimedio.',
              action:
                'Imposta `compute_query_id` a on, oppure ad auto con un modulo come `pg_stat_statements` caricato.',
            },
            {
              term: 'No wait event types observed',
              meaning: 'Mostrato quando il livello selezionato non ha serie per l’intervallo.',
              action:
                'Allarga l’intervallo. Se resta vuoto, verifica gli avvisi di campionamento {{ui:shell.crumb.ash}} sopra.',
            },
            {
              term: 'No wait events observed',
              meaning:
                'Lo stesso avviso al livello {{ui:shell.nav.waitEvents}}, quando il tipo selezionato non ha serie per l’intervallo.',
            },
            {
              term: 'Gap',
              meaning: 'Un bucket senza campione salvato.',
              action:
                'Un gap è un’assenza di campioni, quindi verifica che l’agent sia stato in esecuzione in quel periodo.',
            },
          ],
        },
        {
          kind: 'sql',
          label: 'Attese in questo momento, da pg_stat_activity',
          code: "SELECT wait_event_type, wait_event, state, count(*) FROM pg_stat_activity WHERE state <> 'idle' GROUP BY wait_event_type, wait_event, state ORDER BY count(*) DESC;",
        },
        {
          kind: 'sql',
          label: 'Impostazione dei query id (PostgreSQL 14 o successivo)',
          code: 'SHOW compute_query_id;',
        },
      ],
    },
  ],
}
