import type { HelpTopic } from '../../types'

export const settingsTopic: HelpTopic = {
  id: 'settings',
  group: 'admin',
  title: 'Impostazioni',
  summary:
    'Tutto ciò che contiene la pagina Settings: l’inventario di istanze, agent e database con i loro permission tier, le preferenze del browser, la retention dei dati, le regole dell’advisor e le finestre di osservazione, i canali di notifica, i nomi, il command audit e le informazioni sul server.',
  page: {
    path: '/settings',
    label: 'Apri Settings',
  },
  sections: [
    {
      id: 'overview',
      title: 'A cosa serve la pagina',
      blocks: [
        {
          kind: 'text',
          text: 'Il titolo della pagina è {{ui:settings.page.title}}. Settings serve a controllare cosa monitora pglens e a impostare il comportamento della console e del server. La pagina ha sette tab. Il tab resta nell’indirizzo come `tab`; il tab predefinito, Inventory, non viene scritto. I tab con viste, cioè Inventory, Advisor rules e Audit & server, tengono la vista nell’indirizzo come `view`.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:settings.tabs.inventory}}',
              meaning:
                'Ciò che pglens monitora: istanze, permission tier, agent e database. Vedi [{{ui:settings.tabs.inventory}}](/help/settings#inventory).',
            },
            {
              term: '{{ui:settings.tabs.preferences}}',
              meaning:
                'Impostazioni di questo browser: tema, intervallo di tempo predefinito, aggiornamento, righe per pagina, fuso orario e lingua. Vedi [{{ui:settings.tabs.preferences}}](/help/settings#preferences).',
            },
            {
              term: '{{ui:settings.tabs.retention}}',
              meaning:
                'Quanti giorni di storico conserva ogni famiglia di metriche. Vedi [{{ui:settings.tabs.retention}}](/help/settings#retention).',
            },
            {
              term: '{{ui:settings.tabs.advisor}}',
              meaning:
                'Quali regole dell’advisor girano, e le finestre di osservazione di due controlli. Vedi [{{ui:settings.tabs.advisor}}](/help/settings#advisor-rules).',
            },
            {
              term: '{{ui:settings.tabs.notifications}}',
              meaning:
                'Dove vengono consegnati gli alert: Slack, Generic webhook e Google Chat. Vedi [{{ui:settings.tabs.notifications}}](/help/settings#notifications).',
            },
            {
              term: '{{ui:settings.tabs.names}}',
              meaning:
                'Nomi leggibili per cluster e istanze. Vedi [{{ui:settings.tabs.names}}](/help/settings#names).',
            },
            {
              term: '{{ui:settings.tabs.audit}}',
              meaning:
                'Il command audit di ogni istanza e le informazioni sul server. Vedi [{{ui:settings.tabs.audit}}](/help/settings#audit-server).',
            },
          ],
        },
      ],
    },
    {
      id: 'inventory',
      title: 'Inventario',
      blocks: [
        {
          kind: 'text',
          text: 'Il tab {{ui:settings.tabs.inventory}} elenca ciò che pglens monitora. Ha quattro viste, scelte con `view`: {{ui:settings.views.instances}} (la predefinita), {{ui:settings.views.tiers}}, {{ui:settings.views.agents}} e {{ui:settings.views.databases}}. L’inventario deriva dalle istanze che gli agent riportano; il server non tiene una lista separata di agent. L’avviso {{ui:settings.page.unavailableTitle}} compare quando una parte dell’inventario non può essere caricata.',
        },
      ],
      subsections: [
        {
          id: 'instances',
          title: 'Istanze',
          blocks: [
            {
              kind: 'text',
              text: 'Una riga per ogni istanza PostgreSQL monitorata, con {{ui:settings.inventory.colCluster}}, {{ui:settings.inventory.colRole}}, versione di PostgreSQL, {{ui:settings.inventory.colPermTier}}, {{ui:settings.inventory.colLastSeen}}, {{ui:settings.inventory.colState}} e {{ui:settings.inventory.colHostMetrics}}. La ricerca corrisponde a indirizzo, cluster, ruolo, tier e instance ID; i filtri Role e State restringono ulteriormente le righe. {{ui:settings.inventory.clearFilter}} ripristina tutte le righe.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:settings.inventory.colInstance}}',
                  meaning:
                    'Il nome dell’istanza, o il suo indirizzo quando non è impostato alcun nome. Vedi [{{ui:settings.tabs.names}}](/help/settings#names).',
                },
                {
                  term: '{{ui:settings.inventory.colAddress}}',
                  meaning: 'L’host e la porta che l’agent riporta per l’istanza.',
                },
                {
                  term: '{{ui:settings.inventory.colCluster}}',
                  meaning:
                    'Il cluster che possiede l’istanza. Not assigned significa che nessun cluster possiede ancora l’istanza.',
                },
                {
                  term: '{{ui:settings.inventory.colRole}}',
                  meaning: 'Il ruolo dell’istanza nel suo cluster.',
                },
                {
                  term: 'PostgreSQL',
                  meaning:
                    'La versione di PostgreSQL dell’istanza. Le versioni supportate vanno dalla 15 alla 19, vedi [{{ui:settings.views.server}}](/help/settings#server-info).',
                },
                {
                  term: '{{ui:settings.inventory.colPermTier}}',
                  meaning:
                    'Il tier che l’agent può usare su questa istanza: T0, T1 o T2. Vedi [{{ui:settings.views.tiers}}](/help/settings#permission-tiers).',
                },
                {
                  term: '{{ui:settings.inventory.colLastSeen}}',
                  meaning:
                    'Quando l’istanza ha riportato l’ultima volta, nel fuso orario della console.',
                  action:
                    'Se l’orario è vecchio, l’agent di questa istanza si è fermato: vedi [{{ui:settings.views.agents}}](/help/settings#agents).',
                },
                {
                  term: '{{ui:settings.inventory.colState}}',
                  meaning:
                    '{{ui:settings.inventory.up}}: l’istanza ha riportato negli ultimi 60 secondi. {{ui:settings.inventory.down}}: non lo ha fatto.',
                },
                {
                  term: '{{ui:settings.inventory.colHostMetrics}}',
                  meaning:
                    '{{ui:settings.inventory.hostAvailable}}: le host metrics sono state caricate. {{ui:settings.inventory.hostAvailableStale}}: sono state caricate, ma il server le segnala come stale. {{ui:settings.inventory.hostUnavailable}}: l’endpoint delle host metrics non è stato caricato.',
                  action:
                    'Per {{ui:settings.inventory.hostUnavailable}}, verifica l’agent sull’host: il suo endpoint delle host metrics non è stato caricato.',
                },
              ],
            },
            {
              kind: 'steps',
              items: [
                'Apri il tab {{ui:settings.tabs.inventory}}, vista {{ui:settings.views.instances}}.',
                'Digita un indirizzo, un cluster, un ruolo, un tier o un instance ID nella casella di ricerca.',
                'Restringi le righe con i filtri Role e State.',
                'Clicca {{ui:settings.inventory.clearFilter}} per mostrare di nuovo ogni riga.',
              ],
            },
          ],
        },
        {
          id: 'permission-tiers',
          title: 'Livelli di permessi',
          blocks: [
            {
              kind: 'text',
              text: 'Un permission tier stabilisce cosa può fare l’agent di un’istanza oltre a leggere le statistiche. La sezione {{ui:settings.views.tiers}} descrive i tre tier, T0, T1 e T2, ed elenca le istanze di ciascuno. Il pulsante {{ui:settings.inventory.showList}} le apre.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'T0',
                  meaning: 'Legge solo le statistiche.',
                },
                {
                  term: 'T1',
                  meaning: 'Esegue anche `EXPLAIN` per i piani di query.',
                },
                {
                  term: 'T2',
                  meaning: 'Annulla o termina anche sessioni.',
                  action:
                    'Tieni T2 solo sulle istanze dove è voluto annullare o terminare sessioni.',
                },
              ],
            },
            {
              kind: 'note',
              tone: 'info',
              text: '`EXPLAIN ANALYZE` esegue la query. Viene eseguito solo se il target lo consente nelle proprie impostazioni; l’interfaccia lo offre solo a T2.',
            },
          ],
        },
        {
          id: 'agents',
          title: 'Agent',
          blocks: [
            {
              kind: 'text',
              text: 'La vista {{ui:settings.views.agents}} è derivata dalla telemetria delle istanze: una riga per ogni istanza, con il nome dell’agent derivato da essa, l’indirizzo dell’istanza, l’orario dell’ultima segnalazione e lo stato. Il server non tiene una lista separata di agent. La ricerca corrisponde ad agent, indirizzo e motivo.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:settings.inventory.colAgent}}',
                  meaning: 'Il nome dell’agent, derivato dall’istanza.',
                },
                {
                  term: '{{ui:settings.inventory.colInstanceAddress}}',
                  meaning: 'L’indirizzo dell’istanza.',
                },
                {
                  term: '{{ui:settings.inventory.colLastSeen}}',
                  meaning:
                    'Quando l’agent ha riportato l’ultima volta, nel fuso orario della console.',
                },
                {
                  term: '{{ui:settings.inventory.colState}}',
                  meaning:
                    '{{ui:settings.inventory.down}} quando l’istanza non ha riportato entro 60 secondi, o quando è aperto un alert agent-down per essa. Altrimenti {{ui:settings.inventory.reporting}}.',
                  action:
                    'Verifica che l’agent sia in esecuzione sull’host e che raggiunga il server. L’alert `Agent down` è descritto in [{{ui:shell.nav.alerts}}](/help/alerts).',
                },
                {
                  term: '{{ui:settings.inventory.colReason}}',
                  meaning: 'Il motivo dello stato, in parole.',
                },
              ],
            },
          ],
        },
        {
          id: 'databases',
          title: 'Database per istanza',
          blocks: [
            {
              kind: 'text',
              text: 'La vista elenca i database che l’agent di un’istanza riporta. Scegli l’istanza con il selettore Databases instance, che compare con almeno due istanze; la scelta resta nell’indirizzo come `dbInstance`. I database non monitorati compaiono per primi, quindi quelli da verificare sono in cima.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:settings.inventory.colDatabase}}',
                  meaning: 'Il nome del database.',
                },
                {
                  term: '{{ui:settings.inventory.colMonitored}}',
                  meaning:
                    '{{ui:settings.inventory.yes}}: pglens raccoglie le metriche del database. {{ui:settings.inventory.no}}: non le raccoglie.',
                },
                {
                  term: '{{ui:settings.inventory.colSkipReason}}',
                  meaning:
                    'Perché il database non è monitorato. Un trattino significa che è monitorato. `db_budget` significa che l’agent ha raggiunto il suo budget di database: per impostazione predefinita mantiene i 10 database più attivi per istanza.',
                  action:
                    'Se un database che ti serve mostra `db_budget`, verifica il budget dei database nella configurazione dell’agent.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'preferences',
      title: 'Preferenze',
      blocks: [
        {
          kind: 'text',
          text: 'Le preferenze sono conservate solo in questo browser, con la chiave `pglens.preferences`. Altri browser e altri utenti hanno le proprie. Ogni controllo ha una riga che spiega cosa cambia. {{ui:settings.preferences.reset}} ripristina tutte le preferenze.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:settings.preferences.theme}}',
              meaning:
                '{{ui:settings.preferences.theme.light}}, {{ui:settings.preferences.theme.dark}} o {{ui:settings.preferences.theme.system}}; {{ui:settings.preferences.theme.system}} segue il sistema operativo. Il pulsante del tema nell’header cambia la stessa preferenza. Predefinito: {{ui:settings.preferences.theme.light}}.',
            },
            {
              term: '{{ui:settings.preferences.defaultTimeRange}}',
              meaning:
                'L’intervallo di tempo con cui si aprono le pagine quando l’indirizzo non ne indica uno: {{ui:shell.range.1h}}, {{ui:shell.range.2h}}, {{ui:shell.range.4h}}, {{ui:shell.range.6h}}, {{ui:shell.range.8h}}, {{ui:shell.range.24h}}, {{ui:shell.range.3d}} o {{ui:shell.range.7d}}. Un intervallo nell’indirizzo o in un link ne sceglie comunque un altro. Predefinito: {{ui:shell.range.1h}}.',
            },
            {
              term: '{{ui:settings.preferences.refreshInterval}}',
              meaning:
                '{{ui:settings.preferences.refreshInterval.auto}} mantiene l’intervallo di ogni pagina. {{ui:settings.preferences.refreshInterval.off}} oppure ogni 15, 30 o 60 secondi valgono per le pagine che si ricaricano ogni 15 secondi o più lentamente, e spostano con sé il loro avviso di dati non aggiornati; {{ui:settings.preferences.refreshInterval.off}} le ferma e nasconde l’avviso. {{ui:shell.nav.locks}}, grafici delle metriche, {{ui:shell.nav.events}} e comandi in esecuzione mantengono il ricaricamento più rapido. Predefinito: {{ui:settings.preferences.refreshInterval.auto}}.',
            },
            {
              term: '{{ui:settings.preferences.tablePageSize}}',
              meaning:
                'Le righe per pagina delle tabelle quando l’indirizzo non ne imposta alcuna: 20, 50 o 100. Il controllo {{ui:shell.table.rowsPerPage}} sotto una tabella cambia solo quella tabella. Predefinito: 20.',
            },
            {
              term: '{{ui:settings.preferences.timeZone}}',
              meaning:
                'Il fuso orario in cui la console mostra ogni orario: tabelle, grafici, tooltip, l’intervallo di tempo e i campi data degli editor. Predefinito del server (il fuso impostato sul server con `PGLENS_TZ`, UTC se non impostato), Questo browser (il fuso di questo computer) o UTC. Link, export e API restano in UTC. Predefinito: Predefinito del server.',
            },
            {
              term: '{{ui:settings.preferences.language}}',
              meaning:
                'La lingua della console: ogni pagina, questa guida e i testi dell’advisor e della readiness che il server scrive per questo browser. Predefinita del server (la lingua impostata sul server con `PGLENS_LANG`, inglese se non impostata), Questo browser (la prima tra le lingue del browser che la console ha, altrimenti inglese) o una lingua per nome. Vacuum, WAL, checkpoint, deadlock, ctid, xmin, `pg_*`, nomi dei setting e SQL restano in inglese in ogni lingua. Link, export e API non cambiano. Predefinito: Predefinita del server.',
            },
          ],
        },
        {
          kind: 'steps',
          items: [
            'Apri {{ui:shell.nav.settings}} e il tab {{ui:settings.tabs.preferences}}.',
            'Cambia il controllo. La sua riga dice cosa modifica.',
            'Per riportare ogni preferenza al suo default, clicca {{ui:settings.preferences.reset}}.',
          ],
        },
      ],
    },
    {
      id: 'retention',
      title: 'Conservazione dati',
      blocks: [
        {
          kind: 'text',
          text: 'Il tab {{ui:settings.tabs.retention}} imposta quanti giorni di storico conserva ogni famiglia di metriche. La tabella, {{ui:settings.retention.tableLabel}}, ha una riga per famiglia, con {{ui:settings.retention.colNow}}, il campo {{ui:settings.retention.colNew}}, {{ui:settings.retention.colPreview}} e {{ui:settings.retention.colApply}}. La cancellazione non è immediata: la policy di retention elimina i chunk interi più vecchi della retention alla sua esecuzione giornaliera, fino a un giorno dopo Apply.',
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'Accorciare la retention elimina lo storico in modo permanente alla prossima esecuzione della policy. Fai prima un backup. Una retention più corta chiede conferma prima che venga cancellato qualsiasi dato.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:settings.retention.colFamily}}',
              meaning:
                'Il gruppo di metriche controllato dalla riga. Le famiglie sono Instance and database metrics, Statement statistics, Active session history, Replication lag and slots, Table statistics, Index statistics e Bloat estimates.',
            },
            {
              term: '{{ui:settings.retention.colNow}}',
              meaning:
                'I giorni che la famiglia conserva ora, con un badge `default` o `custom`. Il default è 30 giorni, e 90 giorni per Bloat estimates; `custom` significa che il valore è diverso. Set outside pglens: la policy è stata impostata da qualcosa di diverso da pglens, e la riga la mostra. No policy: keeps all history: non viene cancellato nulla.',
            },
            {
              term: '{{ui:settings.retention.colNew}}',
              meaning:
                'Il numero intero di giorni digitato. Il server stabilisce l’intervallo accettato, e il valore massimo è 365 giorni. Gli errori spiegano il motivo: Enter a number of days, Enter a whole number of days oppure Enter between X and Y days.',
            },
            {
              term: '{{ui:settings.retention.colPreview}}',
              meaning:
                'Conta i chunk e i byte che il nuovo valore eliminerebbe ora, senza modificare nulla. Il risultato dice `Keeps more history: nothing is deleted.`, oppure `Nothing older than N days is stored now.`, oppure `Deletes N chunks (~X MiB) at the next policy run.`',
              action:
                'Usa sempre {{ui:settings.retention.preview}} prima di {{ui:settings.retention.apply}}: mostra quanto storico verrebbe eliminato.',
            },
            {
              term: '{{ui:settings.retention.colApply}}',
              meaning:
                'Salva la nuova retention. È abilitato quando la preview corrisponde al valore digitato. Una retention più lunga si applica subito; una più corta chiede prima una conferma.',
              action: 'Leggi la conferma prima di procedere.',
            },
          ],
        },
        {
          kind: 'steps',
          items: [
            'Apri {{ui:settings.tabs.retention}} e trova la riga della famiglia.',
            'Digita il nuovo numero di giorni in {{ui:settings.retention.colNew}}.',
            'Clicca {{ui:settings.retention.preview}} e leggi il risultato.',
            'Clicca {{ui:settings.retention.apply}}, spunta la casella I understand that this history is deleted permanently at the next policy run, poi clicca {{ui:settings.retention.confirmDelete}}.',
          ],
        },
      ],
    },
    {
      id: 'advisor-rules',
      title: 'Regole advisor',
      blocks: [
        {
          kind: 'text',
          text: 'Il tab {{ui:settings.tabs.advisor}} controlla quali regole dell’advisor girano e fino a quanto indietro guardano due controlli. Ha due viste, scelte con `view`: {{ui:settings.views.rules}} (la predefinita) e {{ui:settings.views.windows}}. Una regola che non gira non produce finding.',
        },
      ],
      subsections: [
        {
          id: 'rules',
          title: 'Regole',
          blocks: [
            {
              kind: 'text',
              text: 'Una riga per regola, con {{ui:settings.advisor.colRule}} (il suo id), {{ui:settings.advisor.colCategory}}, {{ui:settings.advisor.colSeverity}}, {{ui:settings.advisor.colScope}}, {{ui:settings.advisor.colMinTier}} ed {{ui:settings.advisor.enabled}}. La categoria è la parte dell’id della regola prima del primo punto. I filtri {{ui:settings.advisor.colCategory}}, {{ui:settings.advisor.colSeverity}} e {{ui:settings.advisor.filterRuns}} restringono la lista; {{ui:settings.advisor.filterRuns}} offre {{ui:settings.advisor.enabled}} e {{ui:settings.advisor.disabled}}. Un contatore mostra All rules run quando nessuna regola è disabilitata.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:settings.advisor.colRule}}',
                  meaning: 'L’id della regola.',
                },
                {
                  term: '{{ui:settings.advisor.colSeverity}}',
                  meaning: 'Quanto sono gravi i finding della regola, come li definisce l’advisor.',
                },
                {
                  term: '{{ui:settings.advisor.colScope}}',
                  meaning: 'A cosa si applica la regola, come la definisce l’advisor.',
                },
                {
                  term: '{{ui:settings.advisor.colMinTier}}',
                  meaning:
                    'Il tier minimo di permesso con cui la regola gira: T0, T1 o T2. Vedi [{{ui:settings.views.tiers}}](/help/settings#permission-tiers).',
                },
                {
                  term: '{{ui:settings.advisor.enabled}}',
                  meaning:
                    'Lo switch decide se la regola gira. Off: la regola non viene valutata, e i suoi finding vengono risolti alla valutazione successiva. Di nuovo On: la regola gira di nuovo alla valutazione successiva.',
                  action:
                    'Disattiva una regola solo se i suoi finding non valgono per questa flotta. I suoi finding aperti si risolvono alla valutazione successiva.',
                },
                {
                  term: '{{ui:settings.advisor.custom}}',
                  meaning:
                    'La regola è stata cambiata dalla console, quindi differisce dall’impostazione dell’advisor.',
                },
              ],
            },
            {
              kind: 'steps',
              items: [
                'Apri {{ui:settings.tabs.advisor}}; la vista {{ui:settings.views.rules}} è quella predefinita.',
                'Cerca la regola, o restringi la lista con {{ui:settings.advisor.colCategory}}, {{ui:settings.advisor.colSeverity}} o {{ui:settings.advisor.filterRuns}}.',
                'Porta {{ui:settings.advisor.enabled}} su Off. L’etichetta diventa {{ui:settings.advisor.disabled}} e la riga mostra {{ui:settings.advisor.custom}}.',
                'Per ripristinare la regola, riporta {{ui:settings.advisor.enabled}} su On.',
              ],
            },
          ],
        },
        {
          id: 'observation-windows',
          title: 'Finestre di osservazione',
          blocks: [
            {
              kind: 'text',
              text: 'Due controlli guardano indietro nello storico. {{ui:settings.lib.advisor.window.label.unusedIndex}} stabilisce da quanto tempo un indice deve restare senza scansioni prima che il controllo degli indici inutilizzati lo segnali. {{ui:settings.lib.advisor.window.label.backupHistory}} stabilisce da quanto tempo un’istanza deve essere monitorata prima che i controlli di backup leggano `no base backup seen` come finding. Una finestra corta segnala prima e può generare falsi positivi.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:settings.lib.advisor.window.label.unusedIndex}}',
                  meaning:
                    'Da quanto tempo un indice deve restare senza scansioni, nello storico dell’istanza o nelle statistiche, prima che il controllo degli indici inutilizzati lo segnali. Predefinito: 7 giorni.',
                },
                {
                  term: '{{ui:settings.lib.advisor.window.label.backupHistory}}',
                  meaning:
                    'Da quanto tempo un’istanza deve essere monitorata prima che i controlli di backup leggano `no base backup seen` come finding, e non come storico insufficiente. Predefinito: 7 giorni.',
                },
                {
                  term: 'Unit',
                  meaning: 'Hours o days. Il campo accetta solo numeri interi.',
                },
                {
                  term: 'Between 1 hour and 90 days',
                  meaning:
                    'L’intervallo mostrato sotto ogni campo. Un valore fuori da questo intervallo viene rifiutato con Enter window between 1 hour and 90 days.',
                },
                {
                  term: '{{ui:settings.advisor.saveWindows}}',
                  meaning:
                    'Salva le finestre modificate. Poi una riga di stato conferma i valori salvati.',
                },
                {
                  term: '{{ui:settings.advisor.resetDefault}}',
                  meaning: 'Elimina il valore salvato, quindi la finestra torna a 7 giorni.',
                },
              ],
            },
            {
              kind: 'note',
              tone: 'warning',
              text: 'Le finestre corte possono generare falsi positivi. Una finestra più corta del default di 7 giorni mostra: Shorter than the 7-day default: more findings, some of them false positives.',
            },
            {
              kind: 'steps',
              items: [
                'Apri {{ui:settings.tabs.advisor}} e la vista {{ui:settings.views.windows}}.',
                'Digita un numero intero nel campo della finestra e scegli hours o days.',
                'Clicca {{ui:settings.advisor.saveWindows}}.',
                'Per riportare una finestra a 7 giorni, clicca {{ui:settings.advisor.resetDefault}}.',
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'notifications',
      title: 'Notifiche',
      blocks: [
        {
          kind: 'text',
          text: 'Il tab {{ui:settings.tabs.notifications}} elenca i canali che consegnano gli alert: Slack, {{ui:alerts.channel.label.webhook}} e Google Chat. Per ogni canale mostra se è configurato, l’host a cui invia e chi lo ha impostato, {{ui:alerts.channel.source.ui}} o {{ui:alerts.channel.source.env}}.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Slack',
              meaning:
                'Invia gli alert a un webhook Slack. Si imposta con `PGLENS_ALERT_SLACK_WEBHOOK_URL`, oppure con `PGLENS_ALERT_SLACK_WEBHOOK_URL_FILE` per un file. Vengono letti anche gli alias `PGLENS_SLACK_WEBHOOK_URL` e `PGLENS_SLACK_WEBHOOK_URL_FILE`.',
            },
            {
              term: '{{ui:alerts.channel.label.webhook}}',
              meaning: 'Invia gli alert a un URL a scelta. Si imposta con `PGLENS_WEBHOOK_URL`.',
            },
            {
              term: 'Google Chat',
              meaning:
                'Invia gli alert a un webhook di Google Chat. Si imposta con `PGLENS_GOOGLE_CHAT_WEBHOOK_URL`, oppure con `PGLENS_GOOGLE_CHAT_WEBHOOK_URL_FILE` per un file.',
            },
            {
              term: '{{ui:settings.notifications.setBy}}',
              meaning:
                'Environment: l’URL arriva dall’ambiente del server. Console: un URL è stato salvato da questa pagina, e prevale sull’ambiente.',
            },
            {
              term: '{{ui:settings.notifications.host}}',
              meaning:
                'L’host a cui il canale invia. L’URL completo non viene più mostrato dopo essere stato salvato.',
            },
            {
              term: '{{ui:settings.notifications.sendTest}}',
              meaning:
                'Invia il messaggio `pglens test notification` attraverso il canale usato dagli alert, e mostra se è stato consegnato entro 10 secondi. Il risultato è {{ui:alerts.channel.result.delivered}}, No answer within 10 s oppure {{ui:alerts.channel.result.failed}} con il motivo. Non viene salvato nulla.',
            },
            {
              term: '{{ui:settings.notifications.setUrl}} / {{ui:settings.notifications.replaceUrl}}',
              meaning:
                'Salva un URL nel database di pglens. Prevale sull’ambiente del server e si applica subito al prossimo alert e al prossimo test. Il campo è solo in scrittura: la console non mostra più l’URL.',
            },
            {
              term: '{{ui:settings.notifications.removeUrl}}',
              meaning:
                'Elimina l’URL salvato. Il canale torna a usare l’URL dell’ambiente del server, oppure resta not configured se l’ambiente non ne ha uno.',
            },
            {
              term: '{{ui:settings.notifications.language}}',
              meaning:
                'La lingua dei messaggi di alert del canale: cosa significa l’alert, i dati che lo mostrano e cosa fare. Il predefinito del server è la lingua del server (`PGLENS_LANG`). Una modifica vale dal prossimo alert. Vacuum, WAL, SQL e i nomi dei setting restano in inglese, come il messaggio di prova.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'Un URL salvato resta nel database di pglens senza cifratura, quindi i backup del database lo contengono. Tratta quei backup come segreti.',
        },
        {
          kind: 'steps',
          items: [
            'Apri {{ui:settings.tabs.notifications}} e trova la riga del canale.',
            'Clicca {{ui:settings.notifications.setUrl}}, o {{ui:settings.notifications.replaceUrl}} se un URL è già salvato.',
            'Digita l’URL e salva. Serve https://; http:// è accettato solo per indirizzi loopback e privati. Non può contenere spazi, tabulazioni, a capo o un frammento (#), e può essere lungo al massimo 2048 caratteri.',
            'Clicca {{ui:settings.notifications.sendTest}} e leggi il risultato.',
          ],
        },
        {
          kind: 'steps',
          items: [
            'Imposta la variabile del canale nell’ambiente del server.',
            'Riavvia il server.',
            'Clicca {{ui:settings.notifications.sendTest}} per verificare la consegna.',
          ],
        },
      ],
    },
    {
      id: 'names',
      title: 'Nomi',
      blocks: [
        {
          kind: 'text',
          text: 'I nomi danno etichette leggibili a cluster e istanze. Compaiono nei titoli, nei breadcrumb, nei selettori dei server, nei filtri e nelle tabelle di alert ed eventi, al posto di identificativi e indirizzi. Senza un nome, l’etichetta ricade sull’identificativo o sull’indirizzo.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:settings.names.filterRow}}',
              meaning: 'Il filtro che restringe la tabella ai cluster o alle istanze.',
            },
            {
              term: '{{ui:settings.names.colClusterName}}',
              meaning: 'Il nome mostrato per un cluster, al posto del suo identificativo.',
            },
            {
              term: '{{ui:settings.names.colInstanceName}}',
              meaning: 'Il nome mostrato per un’istanza, al posto del suo indirizzo.',
            },
            {
              term: 'Cluster id / Address',
              meaning:
                'L’identificativo di un cluster e l’indirizzo di un’istanza. Rename non li modifica.',
            },
            {
              term: '{{ui:settings.names.colRename}}',
              meaning:
                'Apre la finestra di rinomina della riga, Cluster o Instance, con un campo di al massimo 64 caratteri. {{ui:settings.rename.save}} salva il nome; {{ui:settings.rename.cancel}} mantiene quello precedente. Un nome vuoto ripristina l’etichetta predefinita.',
              action: 'Se un nome viene rifiutato, usa al massimo 64 caratteri.',
            },
          ],
        },
        {
          kind: 'steps',
          items: [
            'Apri {{ui:shell.nav.settings}} e il tab {{ui:settings.tabs.names}}.',
            'Trova la riga, o restringi la tabella con il filtro {{ui:settings.names.filterRow}}.',
            'Clicca {{ui:settings.names.colRename}}, digita il nome e clicca {{ui:settings.rename.save}}.',
            'Per togliere un nome, svuota il campo e salva: torna l’etichetta predefinita.',
          ],
        },
      ],
    },
    {
      id: 'audit-server',
      title: 'Audit e server',
      blocks: [
        {
          kind: 'text',
          text: 'Il tab {{ui:settings.tabs.audit}} ha due viste, scelte con `view`: {{ui:settings.views.commands}} (la predefinita) e {{ui:settings.views.server}}. {{ui:settings.views.commands}} registra ogni comando che il server ha chiesto a un agent di eseguire, con il suo esito. {{ui:settings.views.server}} mostra build, API contract, scadenza della sessione, limiti del prodotto e come si accede alla console.',
        },
      ],
      subsections: [
        {
          id: 'command-audit',
          title: 'Audit dei comandi',
          blocks: [
            {
              kind: 'text',
              text: 'Ogni riga è un evento di un’istanza: una richiesta, il suo esito o la sua scadenza. Scegli l’istanza con il selettore Command audit instance, che compare con almeno due istanze; la scelta resta nell’indirizzo come `auditInstance`. La tabella mostra le ultime 100 voci, con orari nel fuso orario della console. La casella di ricerca e i filtri State e Command le restringono.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:settings.audit.colTime}}',
                  meaning: 'Quando l’evento è stato registrato, nel fuso orario della console.',
                },
                {
                  term: '{{ui:settings.audit.colCommand}}',
                  meaning: 'Il comando che il server ha chiesto all’agent di eseguire.',
                },
                {
                  term: '{{ui:settings.audit.colArgs}}',
                  meaning: 'Gli argomenti del comando.',
                },
                {
                  term: '{{ui:settings.audit.colRequestedBy}}',
                  meaning:
                    'La credenziale che ha chiesto: `api-token`, oppure `ui-session` con un breve hash della sessione. pglens non ha identità utente, quindi non mostra mai una persona.',
                },
                {
                  term: '{{ui:settings.audit.colState}}',
                  meaning:
                    'requested, ok, error, rejected o expired. requested: la richiesta attende l’agent. ok o error: l’esito restituito dall’agent. rejected: la richiesta è stata rifiutata. expired: è scaduta senza un esito.',
                  action:
                    'Una richiesta che resta requested, o finisce expired, non è stata presa in carico dall’agent. Verifica l’agent in [{{ui:settings.views.agents}}](/help/settings#agents).',
                },
                {
                  term: '{{ui:settings.audit.colTiming}}',
                  meaning:
                    'Quando la richiesta è stata fatta, quando l’agent l’ha presa in carico e quando scade, con il dettaglio dell’esito. Expired mostra quando la richiesta è scaduta.',
                },
              ],
            },
            {
              kind: 'note',
              tone: 'info',
              text: 'Quando l’istanza non ha voci, la tabella mostra No command activity.',
            },
          ],
        },
        {
          id: 'server-info',
          title: 'Info server',
          blocks: [
            {
              kind: 'text',
              text: '{{ui:settings.views.server}} descrive la release in uso: build, API contract e limiti, seguiti da come si accede alla console e dai link di riferimento.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:settings.server.interfaceBuild}}',
                  meaning: 'La build della console che stai usando.',
                },
                {
                  term: '{{ui:settings.server.apiContract}}',
                  meaning:
                    'La versione del contratto API per cui questa console è stata costruita.',
                },
                {
                  term: '{{ui:settings.server.sessionExpiry}}',
                  meaning:
                    'Quando scade la tua sessione di login. Se il server non indica una scadenza, il valore dice The session endpoint did not provide an expiry.',
                },
                {
                  term: '{{ui:settings.limits.rawRetention.label}}',
                  meaning:
                    'I dati grezzi sono conservati per 30 giorni per impostazione predefinita, e 90 giorni per le bloat estimates. I dati più vecchi della retention vengono eliminati, non condensati: pglens non conserva riassunti. Il valore attuale di ogni famiglia di metriche è in [{{ui:settings.tabs.retention}}](/help/settings#retention).',
                },
                {
                  term: '{{ui:settings.limits.postgresVersions.label}}',
                  meaning: 'Sono supportate le versioni di PostgreSQL dalla 15 alla 19.',
                },
                {
                  term: '{{ui:settings.limits.replication.label}}',
                  meaning:
                    'La streaming replication è supportata; le altre modalità di replica no.',
                },
                {
                  term: '{{ui:settings.limits.databasesPerInstance.label}}',
                  meaning:
                    'Per impostazione predefinita sono monitorati al massimo 10 database per istanza.',
                },
                {
                  term: '{{ui:settings.limits.bloat.label}}',
                  meaning:
                    'Il bloat, lo spazio sprecato dentro le tabelle dalle vecchie versioni delle righe, è una stima calcolata dalle statistiche. Può sbagliare in entrambe le direzioni.',
                },
                {
                  term: '{{ui:settings.limits.activeSessionHistory.label}}',
                  meaning:
                    'pglens campiona a intervalli regolari cosa fanno le sessioni attive. È un campione, non una traccia completa.',
                },
                {
                  term: '{{ui:settings.limits.connectionPoolers.label}}',
                  meaning:
                    'Un pooler come PgBouncer condivide le connessioni al database tra molti client. In questa release non esiste una vista dei pooler.',
                },
                {
                  term: '{{ui:settings.server.authTitle}}',
                  meaning:
                    'Una password condivisa protegge la sessione del browser. Non esistono account utente, ruoli o audit per utente. Le sessioni sono conservate nella memoria del server e non sopravvivono a un riavvio. Gli agent si autenticano con un proprio token. Non esistono un enrollment queue né un controllo di revoca dei browser.',
                },
                {
                  term: '{{ui:settings.server.refsTitle}}',
                  meaning:
                    'Link alla API reference, ad {{ui:shell.nav.alertRules}}, a {{ui:shell.nav.silences}} e alla documentazione dei Product limits.',
                },
              ],
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
              term: '{{ui:settings.page.unavailableTitle}}',
              meaning: 'Una parte dell’inventario non è stata caricata.',
              action: 'Ricarica la pagina. Se l’avviso resta, verifica il server.',
            },
            {
              term: '{{ui:settings.page.emptyFleetTitle}}',
              meaning: 'Nessun agent ha ancora riportato un’istanza, quindi l’inventario è vuoto.',
              action:
                'Installa e configura l’agent pglens su un host PostgreSQL per aggiungere la prima istanza.',
            },
            {
              term: '{{ui:settings.inventory.emptyAgentsTitle}}',
              meaning: 'La vista Agents non ha istanze monitorate da cui derivare un agent.',
            },
            {
              term: '{{ui:settings.inventory.noMatchTitle}}',
              meaning: 'Nessuna riga corrisponde alla ricerca e ai filtri.',
              action: 'Clicca {{ui:settings.inventory.clearFilter}}, oppure cambia la ricerca.',
            },
          ],
        },
        {
          kind: 'steps',
          items: [
            'Apri {{ui:settings.tabs.inventory}}, vista {{ui:settings.views.instances}}. Un’istanza non {{ui:settings.inventory.up}} non ha riportato negli ultimi 60 secondi.',
            'Apri {{ui:settings.views.agents}}: uno stato {{ui:settings.inventory.down}} mostra il motivo nella colonna {{ui:settings.inventory.colReason}}.',
            'Verifica {{ui:settings.inventory.colHostMetrics}}: {{ui:settings.inventory.hostUnavailable}} significa che l’endpoint delle host metrics non è stato caricato.',
            'Apri {{ui:settings.views.databases}}: un database non monitorato mostra il suo motivo nella colonna {{ui:settings.inventory.colSkipReason}}.',
          ],
        },
      ],
    },
  ],
}
