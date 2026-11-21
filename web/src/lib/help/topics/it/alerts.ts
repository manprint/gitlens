import type { HelpTopic } from '../../types'

export const alertsTopic: HelpTopic = {
  id: 'alerts',
  group: 'alerting',
  title: 'Alert',
  summary:
    'Gli alert generati dalle regole di alert su tutta la flotta: cosa è in firing ora, cosa si è risolto, cosa è silenziato e cosa fare per ciascuno.',
  page: {
    path: '/alerts',
    label: 'Apri Alerts',
  },
  sections: [
    {
      id: 'overview',
      title: 'A cosa serve la pagina',
      blocks: [
        {
          kind: 'text',
          text: 'Un alert è una regola che vale per un cluster o per una istanza. Il server valuta le regole ogni 30 secondi per impostazione predefinita, usando le metriche e gli eventi inviati dagli agent, e conserva il risultato. Un alert è in firing finché la sua condizione vale, e resolved quando non vale più. La pagina li elenca, così vedi cosa richiede attenzione ora, cosa è silenziato e cosa si è già chiuso.',
        },
        {
          kind: 'steps',
          items: [
            'Apri la pagina dal gruppo {{ui:shell.nav.alerting}} della barra laterale. Ha due tab: {{ui:alerts.section.alerts}} e {{ui:alerts.section.delivery}}.',
            'Parti da {{ui:alerts.column.state}} impostato su {{ui:alerts.filter.firing}} e dalle tile di riepilogo: sono gli alert che richiedono un intervento ora.',
            'Seleziona una riga per aprire Alert detail, poi segui la sua regola in [{{ui:shell.nav.alertRules}}](/help/alert-rules#rules-table).',
            'Silenzia un alert solo per fermare un rumore che conosci e accetti: vedi [{{ui:shell.nav.silences}}](/help/silences#create).',
          ],
        },
      ],
      subsections: [
        {
          id: 'page-controls',
          title: 'Controlli della pagina',
          blocks: [
            {
              kind: 'text',
              text: 'I filtri stanno sopra l’elenco. Ciascuno è salvato nell’indirizzo, quindi una vista filtrata si può ricaricare o condividere.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:alerts.column.state}}',
                  meaning:
                    '{{ui:alerts.filter.firing}} (predefinito), {{ui:alerts.filter.allStates}}, {{ui:alerts.filter.pending}} o {{ui:alerts.status.resolved}}. Il motore non salva mai gli alert pending, quindi l’opzione {{ui:alerts.filter.pending}} in pratica non mostra nulla.',
                },
                {
                  term: '{{ui:alerts.column.severity}}',
                  meaning:
                    '{{ui:alerts.filter.allSeverities}}, {{ui:alerts.filter.critical}}, {{ui:alerts.filter.warning}} o {{ui:alerts.filter.info}}. La severità è impostata dalla regola che ha generato l’alert.',
                },
                {
                  term: '{{ui:alerts.filter.suppression}}',
                  meaning:
                    '{{ui:alerts.filter.allAlerts}} (predefinito), {{ui:alerts.status.suppressed}} o {{ui:alerts.filter.notSuppressed}}. Un alert è soppresso quando un silenziamento attivo lo corrisponde: vedi [{{ui:alerts.filter.suppression}}](/help/alerts#suppression).',
                },
                {
                  term: '{{ui:alerts.column.cluster}}',
                  meaning: 'Mostra gli alert di un solo cluster. Vuoto significa tutti i cluster.',
                },
              ],
            },
            {
              kind: 'note',
              tone: 'info',
              text: 'I link della colonna {{ui:alerts.column.notes}} aprono solo gli alert che un silenziamento corrisponde, tramite il filtro `silence_id`. Il pulsante {{ui:alerts.list.showAll}} rimuove quel filtro.',
            },
          ],
        },
      ],
    },
    {
      id: 'summary-tiles',
      title: 'Riepilogo degli alert',
      blocks: [
        {
          kind: 'text',
          text: 'Cinque conteggi sugli alert dell’elenco. I conteggi firing escludono gli alert soppressi; {{ui:alerts.status.suppressed}} e {{ui:alerts.status.resolved}} sono contati a parte.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.summary.criticalFiring}}',
              meaning:
                'Alert firing di severità Critical che nessun silenziamento sopprime. Sopra zero richiede un intervento subito.',
              action:
                'Imposta {{ui:alerts.column.state}} su {{ui:alerts.filter.firing}} e {{ui:alerts.column.severity}} su {{ui:alerts.filter.critical}}, apri ogni riga e segui la sua regola in [{{ui:shell.nav.alertRules}}](/help/alert-rules#rules-table).',
            },
            {
              term: '{{ui:alerts.summary.warningFiring}}',
              meaning:
                'Alert firing di severità Warning che nessun silenziamento sopprime. Controllali dopo quelli Critical.',
              action:
                'Gli stessi passi di {{ui:alerts.summary.criticalFiring}}, con {{ui:alerts.column.severity}} impostato su {{ui:alerts.filter.warning}}.',
            },
            {
              term: '{{ui:alerts.summary.infoFiring}}',
              meaning:
                'Alert firing di severità Info che nessun silenziamento sopprime. Informano; di rado richiedono un’azione diretta.',
            },
            {
              term: '{{ui:alerts.status.suppressed}}',
              meaning:
                'Alert corrisposti da un silenziamento attivo. Restano in firing e continuano a essere valutati, ma le loro notifiche non vengono inviate.',
              action:
                'Apri il silenziamento indicato nella colonna {{ui:alerts.column.notes}}. Terminalo in [{{ui:shell.nav.silences}}](/help/silences#end-silence) se non è più valido.',
            },
            {
              term: '{{ui:alerts.status.resolved}}',
              meaning:
                'Alert la cui condizione non vale più. Restano nell’elenco con il loro momento di risoluzione.',
            },
          ],
        },
      ],
    },
    {
      id: 'alert-table',
      title: 'Elenco degli alert',
      blocks: [
        {
          kind: 'text',
          text: 'Gli alert che corrispondono ai filtri, in quest’ordine: firing, poi pending, poi resolved; Critical, poi Warning, poi Info; prima quelli non soppressi; poi per cluster e chiave. La pagina carica i 500 alert valutati più di recente. Se ce ne sono di più, un avviso lo segnala, e restringere i filtri fa vedere gli altri.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.column.alert}}',
              meaning:
                'La chiave dell’alert: la regola e l’oggetto per cui scatta. Le sue parti sono mostrate in {{ui:alerts.detail.labels}} nel dettaglio.',
            },
            {
              term: '{{ui:alerts.column.severity}}',
              meaning:
                'Critical, Warning o Info, impostato dalla regola. Critical è il più urgente.',
            },
            {
              term: '{{ui:alerts.column.state}}',
              meaning:
                'firing: la condizione vale ora. resolved: non vale più. pending non viene mai salvato.',
              action:
                'Per gli alert firing, segui la regola in [{{ui:shell.nav.alertRules}}](/help/alert-rules#rules-table).',
            },
            {
              term: '{{ui:alerts.column.summary}}',
              meaning: 'Cosa ha rilevato la regola, in una frase.',
            },
            {
              term: '{{ui:alerts.column.cluster}}',
              meaning: 'Il cluster a cui appartiene l’alert. Unknown se l’alert non ne ha uno.',
            },
            {
              term: '{{ui:alerts.column.instance}}',
              meaning:
                'L’istanza per cui scatta l’alert. Unknown se l’alert non è legato a una istanza.',
            },
            {
              term: '{{ui:alerts.column.started}}',
              meaning: 'Quando è iniziato l’alert, nel fuso orario della console.',
            },
            {
              term: '{{ui:alerts.column.notes}}',
              meaning:
                'Possono comparire due cose: "{{ui:alerts.row.suppressedBy}}" con il motivo del silenziamento e il tempo residuo, collegato a quel silenziamento; e "{{ui:alerts.row.troubleshooting}}", mostrato per `agent_down` e `instance_unreachable`.',
              action:
                'Per `agent_down` e `instance_unreachable`, apri per primo la troubleshooting checklist.',
            },
          ],
        },
      ],
    },
    {
      id: 'alert-detail',
      title: 'Dettaglio dell’alert',
      blocks: [
        {
          kind: 'text',
          text: 'Seleziona una riga per aprire Alert detail sopra l’elenco. {{ui:alerts.detail.close}} lo nasconde. Il dettaglio ha quattro campi: {{ui:alerts.detail.rule}}, {{ui:alerts.column.state}}, {{ui:alerts.detail.labels}} e {{ui:alerts.detail.history}}.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.detail.rule}}',
              meaning:
                'L’id della regola che ha generato l’alert, per esempio `agent_down`. Soglia e durata sono in [{{ui:shell.nav.alertRules}}](/help/alert-rules#rules-table).',
            },
            {
              term: '{{ui:alerts.column.state}}',
              meaning: 'Lo stato dell’alert, come nell’elenco.',
            },
            {
              term: '{{ui:alerts.detail.labels}}',
              meaning:
                'Le etichette che identificano l’alert, mostrate come JSON. Un matcher di silenziamento con un nome diverso dai nomi riservati confronta queste etichette.',
            },
            {
              term: '{{ui:alerts.detail.history}}',
              meaning:
                'I momenti di {{ui:alerts.column.started}}, last evaluated e resolved. Resolved mostra {{ui:alerts.detail.notYet}} finché l’alert è aperto.',
            },
          ],
        },
        {
          kind: 'steps',
          items: [
            'Leggi {{ui:alerts.detail.rule}} e {{ui:alerts.detail.labels}} per trovare l’oggetto a cui è legato l’alert.',
            'Prendi il momento di {{ui:alerts.column.started}} da {{ui:alerts.detail.history}} e cerca la causa nella cronologia [{{ui:shell.nav.events}}](/help/events#timeline) intorno a quel momento.',
            'Se {{ui:alerts.column.state}} è resolved, leggi il momento di risoluzione per sapere quando si è chiuso.',
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'La pagina non ha azioni di acknowledge o di assegnazione e non elenca le notifiche inviate per un alert. I canali di notifica si impostano sul server: vedi [{{ui:alerts.section.delivery}}](/help/alerts#delivery).',
        },
      ],
    },
    {
      id: 'suppression',
      title: 'Soppressione',
      blocks: [
        {
          kind: 'text',
          text: 'Un alert è {{ui:alerts.status.suppressed}} quando un silenziamento attivo lo corrisponde. La pagina calcola questo dato a ogni lettura degli alert, usando i silenziamenti attivi in quel momento. La soppressione non cambia lo stato: un alert soppresso resta in firing, continua a essere valutato e rimane nell’elenco. Ciò che blocca è la notifica. Il server decide su ogni notifica quando un alert entra in firing o si risolve; se in quel momento un silenziamento lo corrisponde, la notifica non viene inviata. Terminare un silenziamento rimuove la soppressione alla lettura successiva, ma le notifiche già trattenute non vengono inviate dopo.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.status.suppressed}}',
              meaning:
                'Un silenziamento attivo corrisponde all’alert. Le sue notifiche non sono state inviate al momento della transizione.',
              action:
                'Leggi il silenziamento indicato in {{ui:alerts.column.notes}}. Se vale ancora, lascialo; se non vale più, terminalo in [{{ui:shell.nav.silences}}](/help/silences#end-silence).',
            },
            {
              term: '{{ui:alerts.filter.notSuppressed}}',
              meaning:
                'Nessun silenziamento attivo corrisponde. Le sue transizioni generano notifiche quando è configurato un canale di consegna.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'Un silenziamento blocca le notifiche delle transizioni che avvengono mentre è attivo. Non richiama una notifica già inviata.',
        },
      ],
    },
    {
      id: 'delivery',
      title: 'Consegna delle notifiche',
      blocks: [
        {
          kind: 'text',
          text: 'Il server invia una notifica quando un alert entra in firing e quando si risolve, a meno che un silenziamento non lo corrisponda in quel momento. La consegna si imposta sul server con variabili d’ambiente: il browser non riceve mai un indirizzo di webhook. La tab elenca i canali supportati e le loro variabili. [{{ui:shell.nav.settings}}, {{ui:settings.tabs.notifications}}](/settings?tab=notifications) mostra quali canali sono configurati, può salvare un indirizzo che sostituisce quello dell’ambiente e invia un messaggio di prova.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Slack',
              meaning:
                'Un webhook di Slack. Variabile: `PGLENS_ALERT_SLACK_WEBHOOK_URL`, oppure `PGLENS_ALERT_SLACK_WEBHOOK_URL_FILE` per leggere l’indirizzo da un file. Vengono letti anche gli alias `PGLENS_SLACK_WEBHOOK_URL` e `PGLENS_SLACK_WEBHOOK_URL_FILE`.',
              action:
                'Imposta una delle variabili sul server, poi invia una prova da [{{ui:shell.nav.settings}}, {{ui:settings.tabs.notifications}}](/settings?tab=notifications).',
            },
            {
              term: '{{ui:alerts.channel.label.webhook}}',
              meaning: 'Qualsiasi webhook HTTP. Variabile: `PGLENS_WEBHOOK_URL`.',
              action:
                'Imposta la variabile sul server, poi invia una prova da [{{ui:shell.nav.settings}}, {{ui:settings.tabs.notifications}}](/settings?tab=notifications).',
            },
            {
              term: 'Google Chat',
              meaning:
                'Un webhook di uno space di Google Chat. Variabile: `PGLENS_GOOGLE_CHAT_WEBHOOK_URL`, oppure `PGLENS_GOOGLE_CHAT_WEBHOOK_URL_FILE` per leggere l’indirizzo da un file.',
              action:
                'Imposta una delle variabili sul server, poi invia una prova da [{{ui:shell.nav.settings}}, {{ui:settings.tabs.notifications}}](/settings?tab=notifications).',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'Se non è configurato alcun canale di consegna, gli alert vengono salvati ma non consegnati, e la tab mostra una nota che lo dice. Silenziare un alert sopprime solo le sue notifiche: l’alert resta visibile e continua a essere valutato.',
        },
      ],
    },
    {
      id: 'handling',
      title: 'Gestire un alert in firing',
      blocks: [
        {
          kind: 'steps',
          items: [
            'Imposta {{ui:alerts.column.state}} su {{ui:alerts.filter.firing}} e {{ui:alerts.column.severity}} su {{ui:alerts.filter.critical}}. Poi scendi a {{ui:alerts.filter.warning}} e {{ui:alerts.filter.info}}.',
            'Apri la riga e leggi il suo {{ui:alerts.column.summary}}. Poi apri [{{ui:shell.nav.alertRules}}](/help/alert-rules#rules-table) per la soglia della regola e cosa controllare.',
            'Guarda lo stesso oggetto nella cronologia [{{ui:shell.nav.events}}](/help/events#timeline) intorno al momento di {{ui:alerts.column.started}}. Un evento spesso indica la causa, per esempio un failover o un agent che ha smesso di inviare dati.',
            'Risolvi la causa, poi verifica che l’alert passi a resolved e che {{ui:alerts.detail.history}} mostri un momento di risoluzione.',
            'Silenzia l’alert solo quando il suo rumore è noto e accettato: vedi [{{ui:shell.nav.silences}}](/help/silences#create).',
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
              term: '{{ui:alerts.empty.none}}',
              meaning:
                'La flotta non ha emesso alcun alert. Se ne aspetti uno, controlla gli agent in [{{ui:shell.nav.events}}](/help/events#timeline).',
            },
            {
              term: '{{ui:alerts.empty.noMatch}}',
              meaning:
                'I filtri nascondono tutti gli alert. Rimuovili, oppure imposta {{ui:alerts.column.state}} su {{ui:alerts.filter.allStates}}.',
            },
            {
              term: 'Suppression details are unavailable; alerts remain visible without silence context.',
              meaning:
                'Non è stato possibile caricare i silenziamenti, quindi gli alert sono mostrati senza il contesto dei silenziamenti. {{ui:alerts.callout.retry}} li carica di nuovo.',
            },
            {
              term: 'Only the 500 most recently evaluated alerts are loaded.',
              meaning:
                'L’elenco si ferma a 500 alert. Restringilo con {{ui:alerts.column.cluster}}, {{ui:alerts.column.severity}} o {{ui:alerts.filter.suppression}} per vedere gli altri.',
            },
            {
              term: 'The silence named in this link is not in the current silences list.',
              meaning:
                'Il link punta a un silenziamento non caricato, quindi l’elenco è vuoto. {{ui:alerts.list.showAll}} rimuove il filtro.',
            },
            {
              term: 'Le notifiche non arrivano',
              meaning:
                'Controlla in quest’ordine: una variabile di canale è impostata sul server; una prova inviata da [{{ui:shell.nav.settings}}, {{ui:settings.tabs.notifications}}](/settings?tab=notifications) arriva; l’alert è {{ui:alerts.filter.notSuppressed}}; e la sua transizione è avvenuta dopo che il canale è stato impostato, perché le notifiche partono solo sulle transizioni.',
            },
          ],
        },
      ],
    },
  ],
}
