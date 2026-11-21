import type { HelpTopic } from '../../types'

export const fleetTopic: HelpTopic = {
  id: 'fleet',
  group: 'monitoring',
  title: 'Panoramica della flotta',
  summary:
    'La pagina Fleet: ogni cluster PostgreSQL monitorato con salute, istanze e replication lag. Come leggere le tile di riepilogo e le card dei cluster, cosa fare con un cluster non sano e perché un valore può essere stale.',
  page: {
    path: '/',
    label: 'Apri {{ui:shell.nav.fleet}}',
  },
  sections: [
    {
      id: 'overview',
      title: 'A cosa serve la pagina',
      blocks: [
        {
          kind: 'text',
          text: "{{ui:shell.nav.fleet}} è la pagina su `/`, aperta dalla voce {{ui:shell.nav.fleet}} della sidebar. Mostra in un'unica griglia ogni cluster PostgreSQL monitorato: salute, primary, istanze e il replication lag più alto. Serve a vedere a colpo d'occhio quali cluster funzionano, quali richiedono attenzione e quali agent hanno smesso di inviare dati. Il server costruisce la griglia dai report degli agent e la griglia si aggiorna ogni 15 secondi; gli alert attivi arrivano dalla lista degli alert.",
        },
        {
          kind: 'steps',
          items: [
            'Apri {{ui:shell.nav.fleet}} dalla sidebar. Leggi prima [Agent che richiedono attenzione](/help/fleet#agent-health): un agent silenzioso rende stale i valori del suo cluster.',
            'Leggi le [tile di riepilogo](/help/fleet#summary) per i cluster per salute, le istanze up e down e gli alert attivi.',
            'Restringi la griglia con il campo di ricerca o con una tile; vedi [Controlli della pagina](/help/fleet#page-controls).',
            'Apri un cluster dalla sua card. La pagina del cluster è spiegata in [Dettaglio cluster](/help/cluster).',
          ],
        },
      ],
      subsections: [
        {
          id: 'page-controls',
          title: 'Controlli della pagina',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:fleet.page.filterLabel}}',
                  meaning:
                    "Campo di ricerca sopra la griglia. Cerca nel nome del cluster, nell'indirizzo dell'istanza e nel nome visualizzato dell'istanza, e restringe la griglia. Premi `/` per arrivarci.",
                },
              ],
            },
            {
              kind: 'note',
              tone: 'info',
              text: "Il filtro di salute (`health`) e la ricerca (`q`) sono nell'indirizzo della pagina, quindi un reload o un link condiviso li mantiene. I filtri impostati dalle tile {{ui:fleet.summary.instancesUp}}, {{ui:fleet.summary.instancesDown}} e {{ui:fleet.summary.firingAlerts}} non lo sono: un reload li cancella.",
            },
          ],
        },
      ],
    },
    {
      id: 'agent-health',
      title: 'Agent che richiedono attenzione',
      blocks: [
        {
          kind: 'text',
          text: "Questa fascia compare sopra la griglia solo se almeno un'istanza richiede attenzione. Elenca ogni istanza il cui agent è down, è rimasta in silenzio per 45 secondi o più, oppure ha un alert `agent_down` o `instance_unreachable` attivo. I valori del cluster accanto possono essere stale finché l'agent non torna a inviare dati.",
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Reported reason',
              meaning:
                "La causa indicata dall'agent o dall'alert attivo, in parole semplici: il prefisso `cause:` o `reason:` viene tolto e i trattini bassi diventano spazi. Senza causa la console mostra: {{ui:fleet.health.notReported}}",
              action:
                "Controlla i log dell'agent su quell'host, il container dell'agent, il percorso di rete verso il server e il server stesso. Le stesse verifiche valgono per gli alert {{ui:settings.inventory.colAgent}} down e {{ui:alerts.column.instance}} unreachable.",
            },
            {
              term: '{{ui:findings.table.lastSeen}}',
              meaning:
                "Tempo trascorso dall'ultimo report dell'istanza, mostrato come durata. Oltre 45 secondi l'istanza compare qui; oltre 60 secondi il server la conta come down.",
              action:
                "Se il valore continua a crescere, l'agent non invia dati: segui i passi di Reported reason.",
            },
            {
              term: "Link dell'istanza",
              meaning:
                "Il nome dell'istanza apre la sua pagina di istanza. Accanto c'è il nome del cluster a cui appartiene.",
            },
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: "Questa fascia segnala un'istanza silenziosa da 45 secondi, ma la salute del cluster conta un'istanza come down solo dopo 60 secondi senza report. Nel frattempo l'istanza compare qui mentre il suo cluster può risultare ancora ok.",
        },
      ],
    },
    {
      id: 'summary',
      title: 'Tile di riepilogo',
      blocks: [
        {
          kind: 'text',
          text: 'La fila di tile sopra la griglia conta cluster e istanze e mostra gli alert attivi. Selezionare una tile filtra la griglia sui cluster a cui si riferisce; la tile attiva risulta premuta, e selezionarla di nuovo annulla il filtro. I valori si aggiornano ogni 15 secondi.',
        },
      ],
      subsections: [
        {
          id: 'health-tiles',
          title: 'Tile di salute',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:alerts.events.allClusters}}',
                  meaning:
                    'Numero di cluster monitorati, qualunque sia la loro salute. Selezionarla annulla il filtro di salute.',
                },
                {
                  term: '{{ui:fleet.summary.healthy}}',
                  meaning:
                    'Cluster con salute `ok`: esattamente un primary, tutte le istanze up e un replay lag massimo degli standby di 30 secondi o meno. Un cluster senza lettura di lag non è penalizzato. Selezionarla elenca solo questi cluster.',
                },
                {
                  term: 'Degraded',
                  meaning:
                    "Cluster con esattamente un primary in cui un'istanza è down (nessun report da 60 secondi) oppure il replay lag massimo degli standby è sopra 30 secondi. Selezionarla elenca solo questi cluster.",
                  action:
                    'Apri la card per vedere quale istanza è down o quanto è grande il lag. Vedi [Cosa fare con un cluster non sano](/help/fleet#unhealthy-cluster).',
                },
                {
                  term: 'Critical',
                  meaning:
                    'Cluster che non riportano alcun primary, o più di un primary (split brain). Qui conta solo il numero di primary, non il lag.',
                  action:
                    'Apri il cluster e conta i primary nella tab {{ui:cluster.tabs.topology}}; vedi [Dettaglio cluster](/help/cluster).',
                },
              ],
            },
          ],
        },
        {
          id: 'instance-tiles',
          title: 'Tile istanze',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:fleet.summary.instancesUp}}',
                  meaning:
                    'Istanze che hanno inviato dati negli ultimi 60 secondi, contate su tutti i cluster. Selezionarla elenca i cluster con almeno una di queste istanze.',
                },
                {
                  term: '{{ui:fleet.summary.instancesDown}}',
                  meaning:
                    "Istanze che non hanno inviato dati negli ultimi 60 secondi, contate su tutti i cluster. Selezionarla elenca i cluster con almeno una di queste istanze. Un'istanza sostituita da una più recente allo stesso indirizzo e porta resta contata come down qui, ma la salute del cluster la ignora.",
                  action:
                    "Controlla l'agent di ogni istanza elencata; vedi [Agent che richiedono attenzione](/help/fleet#agent-health). Un'istanza sostituita da una più recente non richiede azioni.",
                },
              ],
            },
          ],
        },
        {
          id: 'alert-tile',
          title: 'Tile alert attivi',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:fleet.summary.firingAlerts}}',
                  meaning:
                    'Alert attivi ora su tutta la flotta, contati dalla lista degli alert. Selezionarla mantiene i cluster con un alert attivo, associati per cluster o per istanza. Se la lista è stata troncata compare un avviso e il numero è un minimo. Se la lista non può essere caricata, la tile mostra Unavailable e non è selezionabile.',
                  action:
                    'Apri la pagina [{{ui:shell.nav.alerts}}](/alerts) per vedere la regola e lo stato di ogni alert.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'cluster-cards',
      title: 'Card dei cluster',
      blocks: [
        {
          kind: 'text',
          text: "Una card per cluster, sotto le tile. Prima le card critical, poi degraded, poi ok; a parità di salute l'ordine è per nome. Il titolo è un link e l'intera card apre il dettaglio del cluster. Il badge di salute è il primo elemento di ogni card; seguono i valori e i badge.",
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'ok',
              meaning:
                'Sano: un solo primary, tutte le istanze up e replay lag entro 30 secondi. Stessa regola della tile Healthy.',
            },
            {
              term: 'degraded',
              meaning:
                "Un'istanza è down, oppure il replay lag massimo degli standby supera 30 secondi. Apri il cluster per vedere quale.",
              action: 'Vedi [Cosa fare con un cluster non sano](/help/fleet#unhealthy-cluster).',
            },
            {
              term: 'critical',
              meaning: 'Nessun primary, oppure più di un primary (split brain).',
              action: 'Vedi [Cosa fare con un cluster non sano](/help/fleet#unhealthy-cluster).',
            },
          ],
        },
      ],
      subsections: [
        {
          id: 'card-values',
          title: 'Valori della card',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:fleet.card.primary}}',
                  meaning:
                    "Nome dell'istanza indicata come primary, con il suo indirizzo al passaggio del mouse. Un trattino (—) il cui testo al passaggio del mouse dice No primary address is available. significa che nessun indirizzo di primary è noto.",
                  action:
                    'Senza primary il cluster è critical; vedi [Cosa fare con un cluster non sano](/help/fleet#unhealthy-cluster).',
                },
                {
                  term: '{{ui:fleet.card.instances}}',
                  meaning: 'Numero di istanze del cluster.',
                },
                {
                  term: 'Standbys',
                  meaning:
                    'Numero di istanze standby: copie di sola lettura che ricevono le modifiche da un primary. Mostrato come 0 quando non è riportato alcun conteggio.',
                },
                {
                  term: '{{ui:fleet.card.maxReplayLag}}',
                  meaning:
                    'Replay lag più alto, in secondi, riportato da uno standby negli ultimi 60 secondi. Un trattino (—) il cui testo al passaggio del mouse dice No standby has reported replay lag. significa che nessuno standby ne ha riportato uno. Sopra 30 secondi il cluster è degraded.',
                  action:
                    'Apri il cluster e leggi la tab {{ui:cluster.tabs.lag}} per capire quale standby è indietro e se il lag continua a crescere; vedi [Dettaglio cluster](/help/cluster).',
                },
                {
                  term: 'Permission tiers',
                  meaning:
                    "Livello di permesso che l'agent ha su ogni istanza, elencato una volta per cluster. T0 legge le statistiche, T1 esegue anche EXPLAIN per i piani delle query, T2 annulla o termina anche le sessioni. Un livello superiore include quelli inferiori. Un trattino (—) indica che nessun livello è stato riportato.",
                  action:
                    'Una funzione che richiede un livello superiore compare come non consentita. Concedi il livello con `deploy/sql/monitoring_user.sql`.',
                },
              ],
            },
          ],
        },
        {
          id: 'card-badges',
          title: 'Badge della card',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'Identity source',
                  meaning:
                    "Compare quando l'identità del cluster non deriva dall'identificatore di sistema di PostgreSQL (`system_identifier`). Il valore accanto indica la fonte. Sotto la card c'è un link alle istruzioni per concedere il ruolo di monitoraggio.",
                  action:
                    'Segui il link sotto la card. Per un evento `cluster_identity_conflict`, applica `monitoring_user.sql`, poi allinea `cluster_name`.',
                },
                {
                  term: 'N firing alert(s)',
                  meaning:
                    'Numero di alert attivi associati a questo cluster, per cluster o per istanza. Compare solo se il numero è maggiore di zero.',
                  action: 'Apri [{{ui:shell.nav.alerts}}](/alerts) e controlla ogni alert attivo.',
                },
                {
                  term: '{{ui:fleet.card.alertsUnavailable}}',
                  meaning:
                    'La lista degli alert non è stata caricata. La card non mostra alcun conteggio, così un feed mancante non sembra zero alert.',
                  action: 'Usa {{ui:fleet.page.retryAlerts}} nel banner sopra la griglia.',
                },
                {
                  term: '{{ui:fleet.card.readiness}}',
                  meaning:
                    "Dice se l'agent può monitorare per intero ogni server del cluster. `Full`: nessun avviso. `Partial · N warning(s)`: almeno un server ha un avviso. La card non mostra il badge quando la readiness è sconosciuta.",
                  action:
                    "Clicca Fix per aprire la tab {{ui:fleet.card.readiness}} dell'istanza con più avvisi, dove sono elencati gli elementi mancanti e la loro correzione.",
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'unhealthy-cluster',
      title: 'Cosa fare con un cluster non sano',
      blocks: [
        {
          kind: 'text',
          text: "Parti dalla salute della card. Critical significa che il numero di primary non è uno; degraded significa che un'istanza è down o che uno standby è indietro di più di 30 secondi. Verifica la causa in questo ordine: primary, poi istanze, poi lag.",
        },
        {
          kind: 'steps',
          items: [
            'Apri il cluster dalla sua card; vedi [Dettaglio cluster](/help/cluster).',
            'Critical: nella tab {{ui:cluster.tabs.topology}} conta i primary riportati. Zero o più di uno richiede attenzione immediata.',
            'Istanza down: trovala in [Agent che richiedono attenzione](/help/fleet#agent-health) e controlla il suo agent.',
            'Lag sopra 30 secondi: apri la tab {{ui:cluster.tabs.lag}} e verifica se il lag continua a crescere.',
            'Controlla la tab {{ui:shell.nav.events}} per un failover recente, e la pagina [{{ui:shell.nav.events}}](/events) per tutti gli altri tipi di evento.',
          ],
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:fleet.card.primary}} —',
              meaning:
                "Nessun primary riportato, quindi il cluster è critical. L'evento `no_primary_in_cluster` descrive la stessa condizione.",
              action:
                'Scopri perché nessuna istanza è primary e promuovi uno standby se necessario.',
            },
            {
              term: 'Più di un primary',
              meaning:
                "Più di un'istanza riporta il ruolo di primary: split brain, critical. L'evento `split_brain_detected` descrive la stessa condizione.",
              action: "Individua il primary autorevole e retrocedi l'altro.",
            },
            {
              term: '{{ui:alerts.column.instance}} down',
              meaning: "Un'istanza non invia dati da 60 secondi, quindi il cluster è degraded.",
              action:
                "Controlla i log dell'agent, il container dell'agent, la raggiungibilità di rete e il server.",
            },
            {
              term: 'Lag dello standby sopra 30 secondi',
              meaning:
                'Il replay lag massimo degli standby è sopra 30 secondi, quindi il cluster è degraded. La regola di alert `replica.lag_high` descrive la stessa condizione.',
              action:
                'Apri la tab {{ui:cluster.tabs.lag}} del cluster e verifica se il lag continua a crescere; vedi [Dettaglio cluster](/help/cluster).',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'Dopo un failover il nuovo primary assume il ruolo. Il cluster torna ok quando resta un solo primary, tutte le istanze sono up e il lag è entro 30 secondi. Il failover resta elencato nella tab {{ui:shell.nav.events}} del cluster.',
        },
      ],
    },
    {
      id: 'freshness',
      title: 'Freschezza dei dati',
      blocks: [
        {
          kind: 'text',
          text: "La griglia si aggiorna ogni 15 secondi. Il badge della pagina mostra l'età dei dati a schermo dopo la parola Updated, e diventa Stale quando questa età supera 45 secondi. Una card segna come Stale il proprio badge di salute e il suo {{ui:fleet.card.maxReplayLag}} quando un'istanza del cluster non invia dati da 45 secondi o più.",
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Updated … ago',
              meaning:
                'Età dei dati a schermo. Torna bassa a ogni aggiornamento, che avviene ogni 15 secondi.',
              action:
                "Se l'età continua a crescere oltre 45 secondi, verifica che il server sia raggiungibile e ricarica la pagina.",
            },
            {
              term: 'Stale',
              meaning:
                "Compare sul badge della pagina, oppure su un badge di card e sul suo {{ui:fleet.card.maxReplayLag}}, quando i dati hanno più di 45 secondi. Il testo indica l'età e la soglia. Il valore potrebbe non essere più attuale.",
              action:
                "Controlla l'agent dell'istanza elencata in [Agent che richiedono attenzione](/help/fleet#agent-health); i valori tornano quando riprende a inviare dati.",
            },
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
              term: 'No monitored clusters yet',
              meaning:
                'La griglia è vuota perché nessun cluster ha ancora inviato dati. Il messaggio dice: Set up the pglens agent on a PostgreSQL host to add your first cluster.',
              action:
                "Installa e avvia l'agent su un host PostgreSQL. Il cluster compare dopo il primo report dell'agent.",
            },
            {
              term: 'No clusters match these filters',
              meaning:
                'La ricerca o un filtro da tile nasconde tutti i cluster. Il messaggio suggerisce di cancellare la ricerca o di cambiare i filtri attivi.',
              action:
                'Svuota il campo di ricerca, oppure seleziona {{ui:alerts.events.allClusters}} per annullare il filtro di salute. Seleziona di nuovo la tile attiva {{ui:fleet.summary.instancesUp}}, {{ui:fleet.summary.instancesDown}} o {{ui:fleet.summary.firingAlerts}} per annullarla.',
            },
            {
              term: 'Alert counts unavailable.',
              meaning:
                'Gli alert attivi non sono stati caricati, ma la griglia dei cluster resta disponibile. La tile alert mostra Unavailable.',
              action:
                'Seleziona {{ui:fleet.page.retryAlerts}} nel banner. Finché gli alert non si caricano, le card mostrano {{ui:fleet.card.alertsUnavailable}}.',
            },
            {
              term: "Errore nell'elenco dei cluster",
              meaning:
                "L'elenco dei cluster non è stato caricato. Il messaggio di errore indica l'endpoint `clusters` e offre un nuovo tentativo.",
              action:
                "Usa l'azione di nuovo tentativo del messaggio. Se continua a fallire, controlla il server e i suoi log.",
            },
          ],
        },
      ],
    },
  ],
}
