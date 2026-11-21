import type { HelpTopic } from '../../types'

export const gettingStartedTopic: HelpTopic = {
  id: 'getting-started',
  group: 'start',
  title: 'Per iniziare',
  summary:
    'Cos’è pglens, come è organizzata la console e come leggere qualsiasi pagina: la sidebar, l’header, il time range, il badge di freschezza, la ricerca, le tabelle, il login e questa guida.',
  page: {
    path: '/',
    label: 'Apri Fleet',
  },
  sections: [
    {
      id: 'what-pglens-is',
      title: 'Cos’è pglens',
      blocks: [
        {
          kind: 'text',
          text: 'pglens monitora PostgreSQL. Un agent gira su ogni host PostgreSQL, legge le sue statistiche a intervalli regolari e invia gli snapshot al server pglens. Il server conserva gli snapshot, ne ricava finding e alert e serve questa console. La maggior parte delle pagine mostra questi snapshot conservati. Il server può anche chiedere a un agent di eseguire un comando, come un piano di query o un’azione su una sessione, ma solo sulle istanze il cui permission tier lo consente, e ogni richiesta viene registrata.',
        },
        {
          kind: 'text',
          text: 'La gerarchia è server, cluster, istanza, database. Un server pglens contiene molti cluster, un cluster contiene una o più istanze e un’istanza contiene i suoi database. Il tab {{ui:settings.tabs.names}} di {{ui:shell.nav.settings}} assegna nomi leggibili a cluster e istanze.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Agent',
              meaning:
                'Il collettore su un host PostgreSQL. Il suo nome deriva dall’istanza che riporta; il server non tiene una lista separata di agent. Lo stato, {{ui:settings.inventory.reporting}} o Down, è mostrato in {{ui:shell.nav.settings}}, tab {{ui:settings.tabs.inventory}}, vista {{ui:settings.views.agents}}.',
              action:
                'Se lo stato è Down, verifica che il processo dell’agent sia in esecuzione sull’host e che raggiunga il server.',
            },
            {
              term: 'Server',
              meaning:
                'Il server pglens. Conserva gli snapshot in TimescaleDB, calcola finding e alert e serve la console. Il login avviene su questo server.',
            },
            {
              term: 'Cluster',
              meaning:
                'Un gruppo di istanze mostrate insieme, per esempio un primary e le sue repliche. Ogni card di {{ui:shell.nav.fleet}} è un cluster. Un’istanza che nessun cluster possiede ancora è mostrata come {{ui:settings.inventory.notAssigned}}.',
            },
            {
              term: 'Instance',
              meaning:
                'Un server PostgreSQL riportato da un agent, identificato da indirizzo e porta. Le pagine di istanza mostrano una istanza alla volta. Dove è impostato un nome, il nome sostituisce l’indirizzo: vedi [{{ui:shell.nav.settings}}](/help/settings#names).',
            },
            {
              term: 'Database',
              meaning:
                'Un database all’interno di un’istanza. Per impostazione predefinita l’agent monitora al massimo 10 database per istanza; gli altri sono elencati con una skip reason: vedi [{{ui:shell.nav.settings}}](/help/settings#databases).',
            },
          ],
        },
      ],
    },
    {
      id: 'sidebar',
      title: 'Barra laterale',
      blocks: [
        {
          kind: 'text',
          text: 'La sidebar elenca le pagine nei gruppi {{ui:shell.nav.monitoring}}, Instance, {{ui:shell.nav.alerting}} e {{ui:shell.nav.inlineHelp}}, con {{ui:shell.nav.settings}} in fondo. Il gruppo Instance compare quando è scelto un server. {{ui:shell.nav.slowestQueries}} e {{ui:shell.nav.indexAnalysis}} indicano il loro server nell’indirizzo, con il parametro `instance`. Sugli schermi stretti la sidebar è nascosta: il pulsante {{ui:shell.header.openNavigation}} nell’header la apre.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:shell.nav.chooseServer}}',
              meaning:
                'Sceglie l’istanza PostgreSQL mostrata dalle pagine di istanza. L’ultima istanza scelta viene ricordata in questo browser.',
            },
            {
              term: '{{ui:shell.nav.fleet}}',
              meaning:
                'Pagina iniziale (`/`): una card per cluster, e una barra di riepilogo che filtra le card per salute: {{ui:fleet.summary.all}}, {{ui:fleet.summary.healthy}}, {{ui:fleet.summary.degraded}}, {{ui:fleet.summary.critical}}. Dettagli in [{{ui:shell.nav.fleet}}](/help/fleet).',
            },
            {
              term: '{{ui:shell.nav.slowestQueries}}',
              meaning:
                'Le 20 query con il tempo di esecuzione più alto sul server scelto, con piani e storico. Dettagli in [{{ui:shell.nav.slowestQueries}}](/help/slowest-queries).',
            },
            {
              term: '{{ui:shell.nav.indexAnalysis}}',
              meaning:
                'Indici mancanti, unused, duplicati e invalid, con le istruzioni psql da verificare ed eseguire a mano. Dettagli in [{{ui:shell.nav.indexAnalysis}}](/help/indexes).',
            },
            {
              term: '{{ui:shell.nav.findings}}',
              meaning:
                'Problemi rilevati dall’advisor, una riga per finding. Dettagli in [{{ui:shell.nav.findings}}](/help/findings).',
            },
            {
              term: '{{ui:shell.nav.instanceOverview}}',
              meaning:
                'Riepilogo dell’istanza scelta. Dettagli in [{{ui:shell.nav.instanceOverview}}](/help/instance-overview).',
            },
            {
              term: '{{ui:shell.nav.queryPerformance}}',
              meaning:
                'Statistiche delle query dell’istanza scelta. Dettagli in [{{ui:shell.nav.queryPerformance}}](/help/query-performance).',
            },
            {
              term: '{{ui:shell.nav.waitEvents}}',
              meaning:
                'Su cosa attendono le sessioni dell’istanza scelta, in tempo reale. Dettagli in [{{ui:shell.nav.waitEvents}}](/help/wait-events).',
            },
            {
              term: '{{ui:shell.nav.locks}}',
              meaning:
                'Chi blocca chi, e le sessioni attive ora. Dettagli in [{{ui:shell.nav.locks}}](/help/locks).',
            },
            {
              term: '{{ui:shell.nav.alerts}}',
              meaning:
                '{{ui:shell.nav.alerts}} generati dalle regole di alert. Dettagli in [{{ui:shell.nav.alerts}}](/help/alerts).',
            },
            {
              term: '{{ui:shell.nav.events}}',
              meaning:
                'La cronologia degli eventi. Dettagli in [{{ui:shell.nav.events}}](/help/events).',
            },
            {
              term: '{{ui:shell.nav.alertRules}}',
              meaning:
                'Le regole che generano alert, con le loro condizioni. Dettagli in [{{ui:shell.nav.alertRules}}](/help/alert-rules).',
            },
            {
              term: '{{ui:shell.nav.silences}}',
              meaning:
                'I {{ui:shell.nav.silences}} silenziano gli alert che corrispondono. Dettagli in [{{ui:shell.nav.silences}}](/help/silences).',
            },
            {
              term: 'User guide',
              meaning: 'Apre questa guida, con i capitoli di ogni pagina.',
            },
            {
              term: '{{ui:shell.nav.settings}}',
              meaning:
                '{{ui:settings.tabs.inventory}}, preferenze, data retention, regole dell’advisor, notifiche, nomi, command audit e informazioni sul server. Dettagli in [{{ui:shell.nav.settings}}](/help/settings).',
            },
          ],
        },
      ],
    },
    {
      id: 'header',
      title: 'Header',
      blocks: [
        {
          kind: 'text',
          text: 'L’header è sopra ogni pagina. Contiene il time range picker, il pulsante del tema, l’indicatore di connessione, le scorciatoie da tastiera e {{ui:shell.header.signOut}}.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:settings.preferences.theme}}',
              meaning:
                'Alterna i colori della console tra {{ui:settings.preferences.theme.light}}, {{ui:settings.preferences.theme.dark}} e {{ui:settings.preferences.theme.system}}. La stessa impostazione è in [{{ui:shell.nav.settings}}](/help/settings#preferences).',
            },
            {
              term: 'Connection indicator',
              meaning:
                'Mostra se il browser raggiunge il server: reachable, not reachable o browser is offline. Riporta anche l’orario dell’ultima risposta riuscita, oppure No successful response yet.',
              action:
                'Se dice not reachable, verifica che il server sia in esecuzione e che questo browser lo raggiunga.',
            },
            {
              term: '{{ui:shell.header.shortcuts}}',
              meaning:
                'Apre l’elenco delle scorciatoie, anche con `?`. Le sequenze sono `g` poi `f` per {{ui:shell.nav.fleet}}, `g` poi `q` per {{ui:shell.nav.slowestQueries}}, `g` poi `a` per {{ui:shell.nav.alerts}} e `g` poi `s` per {{ui:shell.nav.settings}}. `/` porta il focus sul filtro della pagina.',
            },
            {
              term: '{{ui:shell.header.signOut}}',
              meaning:
                'Termina la sessione in questo browser. Vedi [Login e sessioni](/help/getting-started#sign-in).',
            },
          ],
        },
      ],
    },
    {
      id: 'time-range',
      title: 'Intervallo di tempo',
      blocks: [
        {
          kind: 'text',
          text: 'Il time range picker nell’header imposta la finestra temporale che grafici e tabelle leggono. La finestra resta nell’indirizzo come `range` (un preset, per esempio `1h`), oppure come `from` e `to` (un intervallo assoluto, sempre in UTC), così un link condiviso apre la stessa finestra. Ogni orario a schermo è nel fuso orario della console, impostato in [{{ui:shell.nav.settings}}](/help/settings#preferences). Un range nell’indirizzo prevale sul default impostato in [{{ui:shell.nav.settings}}](/help/settings#preferences).',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:shell.range.preset}}',
              meaning:
                'Il select con i preset: {{ui:shell.range.15m}}, {{ui:shell.range.1h}} (il default), {{ui:shell.range.2h}}, {{ui:shell.range.4h}}, {{ui:shell.range.6h}}, {{ui:shell.range.8h}}, {{ui:shell.range.24h}}, {{ui:shell.range.3d}}, {{ui:shell.range.7d}}, {{ui:shell.range.14d}} e {{ui:shell.range.30d}}. L’elenco arriva fino a 30 giorni.',
            },
            {
              term: 'Custom time range',
              meaning:
                'Apre il modulo assoluto, intitolato con il fuso orario della console (per esempio Custom time range (Europe/Rome)), con From e To in quel fuso, applicati con {{ui:shell.range.apply}}. La fine deve essere successiva all’inizio, e l’intervallo non può superare 30 giorni.',
            },
            {
              term: '{{ui:shell.range.zoomOutTitle}}',
              meaning:
                'Per un range predefinito, passa al preset successivo più lungo, fino a {{ui:shell.range.30d}}.',
            },
          ],
        },
      ],
    },
    {
      id: 'freshness',
      title: 'Badge di freschezza',
      blocks: [
        {
          kind: 'text',
          text: 'L’header della pagina mostra quanto sono vecchi i dati della pagina. La soglia oltre la quale i dati sono stale dipende dalla pagina. Le pagine si ricaricano con il proprio intervallo, salvo che {{ui:settings.preferences.refreshInterval}} in [{{ui:shell.nav.settings}}](/help/settings#preferences) lo cambi.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Updated <age> ago',
              meaning:
                'Da quanto tempo la pagina ha ricevuto dati dal server l’ultima volta. Un’età piccola è quella attesa finché gli agent riportano.',
            },
            {
              term: '{{ui:shell.fresh.noData}}',
              meaning:
                'La pagina non ha ancora ricevuto dati: nessuno snapshot è arrivato al server per essa.',
              action:
                'Verifica gli agent in [{{ui:shell.nav.settings}}](/help/settings#agents), poi lo stato dell’istanza in [{{ui:shell.nav.settings}}](/help/settings#instances).',
            },
            {
              term: 'Stale',
              meaning:
                'I dati sono più vecchi della soglia della pagina. L’avviso indica età e soglia, come in `Stale — <age> old (threshold <t>)`.',
              action:
                'Verifica l’agent che alimenta la pagina e il server, poi ricarica la pagina.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'Un {{ui:settings.preferences.refreshInterval}} di Off ferma il ricaricamento delle pagine che si ricaricano ogni 15 secondi o più lentamente, e nasconde il loro avviso di stale. Locks, grafici delle metriche, events e comandi in esecuzione mantengono il loro ricaricamento più rapido.',
        },
      ],
    },
    {
      id: 'search',
      title: 'Ricerca e filtri',
      blocks: [
        {
          kind: 'text',
          text: 'La ricerca e i filtri sopra una tabella restringono le sue righe; non cambiano alcun dato. {{ui:shell.nav.indexAnalysis}} ha anche {{ui:indexes.search.box}}, che cerca indici, tabelle, colonne e query del server. Si trova nella pagina {{ui:shell.nav.indexAnalysis}}, non nell’header.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:indexes.search.box}}',
              meaning:
                'Trova gli indici, le tabelle, le colonne e le query che corrispondono a tutte le parole digitate. I risultati sono raggruppati come Indexes, Tables, Columns e Queries. {{ui:indexes.search.noMatch}} significa che nulla corrisponde; {{ui:indexes.search.clear}} svuota il campo.',
            },
            {
              term: '{{ui:settings.inventory.clearFilter}}',
              meaning:
                'Nelle tabelle di {{ui:settings.tabs.inventory}} in {{ui:shell.nav.settings}}, rimuove la ricerca e i filtri e mostra di nuovo tutte le righe.',
            },
          ],
        },
      ],
    },
    {
      id: 'tables',
      title: 'Tabelle',
      blocks: [
        {
          kind: 'text',
          text: 'Le tabelle condividono gli stessi controlli: ordinamento, paginazione, righe espandibili, menu delle colonne e suggerimenti sulle colonne. Il footer legge `Showing a–b of N` con il nome delle righe, per esempio missing indexes.',
        },
      ],
      subsections: [
        {
          id: 'sorting',
          title: 'Ordinamento',
          blocks: [
            {
              kind: 'text',
              text: 'Ogni intestazione ordinabile ha due pulsanti, ascendente e discendente. Premi di nuovo quello attivo per togliere l’ordinamento. Una tabella ordina per una colonna alla volta.',
            },
          ],
        },
        {
          id: 'paging',
          title: 'Paginazione e righe per pagina',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'Rows per page',
                  meaning:
                    'Può essere 20, 50 o 100. Il default arriva da [{{ui:shell.nav.settings}}](/help/settings#preferences), Table page size; il controllo sotto una tabella cambia solo quella tabella.',
                },
                {
                  term: 'First page, Previous page, Next page, Last page',
                  meaning: 'Spostano la visualizzazione tra le pagine della tabella.',
                },
              ],
            },
          ],
        },
        {
          id: 'expandable-rows',
          title: 'Righe espandibili',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'Show details',
                  meaning:
                    'Apre i dettagli di una riga sotto di essa; Hide details li chiude. Una riga aperta resta aperta quando la tabella viene riordinata. Cambiare pagina chiude tutte le righe aperte.',
                },
              ],
            },
          ],
        },
        {
          id: 'columns-and-hints',
          title: 'Colonne e suggerimenti',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'Columns',
                  meaning: 'Il menu che mostra o nasconde le colonne della tabella.',
                },
                {
                  term: 'Info button',
                  meaning:
                    'Apre la caption di un valore o di una colonna: cos’è e, dove previsti, Source, Unit, Window, How to read e Thresholds. Un clic o un tocco lo tiene aperto; Escape lo chiude.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'breadcrumbs',
      title: 'Breadcrumb',
      blocks: [
        {
          kind: 'text',
          text: 'Il percorso sopra il titolo della pagina mostra dove ti trovi. I {{ui:settings.tabs.names}} impostati in [{{ui:shell.nav.settings}}](/help/settings#names) sostituiscono gli identificativi nel percorso, e le pagine di help mostrano il titolo del loro capitolo.',
        },
      ],
    },
    {
      id: 'sign-in',
      title: 'Login e sessioni',
      blocks: [
        {
          kind: 'text',
          text: 'La console chiede una password condivisa, nella pagina Sign in to pglens. La password è impostata sul server con `PGLENS_UI_PASSWORD`. Non esistono account utente: chi conosce la password ha lo stesso accesso.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Password',
              meaning:
                'La password condivisa. Troppi tentativi bloccano il login per qualche minuto: la pagina chiede di attendere e riprovare. Una password errata mostra Invalid password.',
              action:
                'Se il login continua a fallire, verifica la password impostata con `PGLENS_UI_PASSWORD` sul server.',
            },
            {
              term: 'Session',
              meaning:
                'La sessione resta attiva in questo browser finché non esci. Le sessioni sono conservate nella memoria del server. La scadenza della sessione è mostrata in [{{ui:shell.nav.settings}}](/help/settings#server-info).',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'Le sessioni vivono nella memoria del server, quindi un riavvio del server disconnette ogni utente. Dopo un riavvio, effettua di nuovo il login.',
        },
      ],
    },
    {
      id: 'using-this-guide',
      title: 'Come usare questo {{ui:shell.nav.inlineHelp}}',
      blocks: [
        {
          kind: 'text',
          text: 'Questa è la {{ui:shell.nav.inlineHelp}} della console. Ogni pagina ha un capitolo, raggruppato nel menu come Per iniziare, {{ui:shell.nav.monitoring}}, Pagine di istanza, {{ui:shell.nav.alerting}} e Amministrazione. Ogni capitolo ha una panoramica, una sezione per ogni area visibile della sua pagina e, dove serve, una sezione di risoluzione dei problemi. Le pagine rimandano al loro capitolo dall’header della pagina.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Search the guide',
              meaning:
                'Filtra le sezioni di tutti i capitoli in base alle parole digitate. In italiano trova anche le etichette inglesi che la console mostra.',
            },
            {
              term: 'On this page',
              meaning: 'Elenca le sezioni del capitolo aperto, in ordine.',
            },
            {
              term: 'Link to this section',
              meaning:
                'L’icona # accanto a un titolo rimanda a quella sezione; il suo indirizzo può essere condiviso.',
            },
            {
              term: 'Help language',
              meaning:
                'Il selettore English / Italiano. Il testo dei capitoli cambia lingua, mentre le etichette della console restano in inglese.',
            },
          ],
        },
        {
          kind: 'steps',
          items: [
            'Apri il capitolo della pagina dal link nell’header, o dal menu di questa guida.',
            'Leggi il riepilogo del capitolo, poi la sezione dell’area che stai guardando.',
            'Verifica ogni termine: significato, unità, soglie e azione.',
            'Usa Search the guide per una parola che non è nel menu.',
          ],
        },
      ],
    },
    {
      id: 'troubleshooting',
      title: 'Risoluzione dei problemi',
      blocks: [
        {
          kind: 'steps',
          items: [
            'Leggi il badge di freschezza della pagina: [{{ui:shell.fresh.noData}}](/help/getting-started#freshness) o [Stale](/help/getting-started#freshness) indicano dati assenti o vecchi.',
            'Verifica il time range: un range breve può non contenere campioni. Vedi [Time range](/help/getting-started#time-range).',
            'Apri [{{ui:shell.nav.settings}}](/help/settings#agents), {{ui:settings.tabs.inventory}}, {{ui:settings.views.agents}}: lo stato Down significa nessuna segnalazione dell’agent entro l’intervallo atteso. La colonna Reason spiega il motivo.',
            'Apri [{{ui:shell.nav.settings}}](/help/settings#instances), {{ui:settings.tabs.inventory}}, Instances: un’istanza che non è Up non ha riportato negli ultimi 60 secondi.',
          ],
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Partial · N warnings',
              meaning:
                'La readiness di un’istanza è partial: manca parte dei dati che potrebbe raccogliere. Full significa che non manca nulla. Il banner Some features have less data than they could. See what is missing and how to fix it. compare dove una pagina ha istanze in questa condizione.',
              action:
                'Apri la sezione readiness dell’istanza. Ogni warning ha un fix con il suo snippet e un pulsante Copy. Un fix marcato Requires restart ha effetto solo dopo un riavvio.',
            },
          ],
        },
      ],
    },
  ],
}
