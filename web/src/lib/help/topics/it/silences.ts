import type { HelpTopic } from '../../types'

export const silencesTopic: HelpTopic = {
  id: 'silences',
  group: 'alerting',
  title: 'Silenziamenti',
  summary:
    'I silenziamenti salvati che sopprimono le notifiche durante il lavoro pianificato: matcher, motivo e finestra, come crearne uno e controllarne l’{{ui:alerts.silenceEditor.preview}}, e come terminarne uno in anticipo.',
  page: {
    path: '/alerts/silences',
    label: 'Apri Silences',
  },
  sections: [
    {
      id: 'overview',
      title: 'A cosa serve la pagina',
      blocks: [
        {
          kind: 'text',
          text: 'La pagina {{ui:shell.nav.silences}} (titolo {{ui:alerts.silences.title}}) elenca i silenziamenti salvati sul server, e permette di crearne o terminarne uno. Un silenziamento sopprime solo le notifiche: l’alert resta visibile nella pagina [{{ui:alerts.section.alerts}}](/alerts) e la sua regola continua a valutarlo. Usa un silenziamento per lavori pianificati, come una finestra di manutenzione, quando gli alert sono attesi e non devono notificare. L’intestazione contiene i link a [{{ui:alerts.section.alerts}}](/alerts) e [{{ui:alerts.silences.rules}}](/alerts/rules), e il pulsante {{ui:alerts.silences.create}}, che apre l’editor e diventa {{ui:alerts.silences.closeEditor}} mentre è aperto.',
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
                  term: '{{ui:alerts.column.state}}',
                  meaning:
                    'Limita l’elenco ai silenziamenti {{ui:alerts.filter.pending}}, {{ui:alerts.silences.active}} o {{ui:alerts.silences.expired}}.',
                },
                {
                  term: '{{ui:shell.table.search}}',
                  meaning: 'Trova un silenziamento per i suoi matcher o per il suo motivo.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'matchers',
      title: 'Matcher',
      blocks: [
        {
          kind: 'text',
          text: 'Un matcher è un nome e un valore. Un alert è silenziato quando corrisponde a tutti i matcher del silenziamento, perché i matcher si combinano con AND. Il valore deve essere esattamente uguale al valore dell’alert. Il nome è uno tra rule_id, severity, cluster_id, instance_id o datname, oppure una qualsiasi altra etichetta dell’alert.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Reserved names',
              meaning:
                'rule_id, severity, cluster_id, instance_id e datname. Ognuno corrisponde al campo dell’alert con lo stesso nome.',
            },
            {
              term: 'Alert label',
              meaning:
                'Ogni altro nome corrisponde a un’etichetta dell’alert con lo stesso valore.',
            },
            {
              term: '`name=value`',
              meaning:
                'Come appare un matcher esatto nella colonna {{ui:alerts.silences.matchers}}. Più matcher sono separati da virgole.',
            },
            {
              term: '`name~value`',
              meaning: 'Un matcher regex. L’editor non crea matcher regex.',
            },
            {
              term: '`not name=value`',
              meaning: 'Un matcher negato. L’editor non crea matcher negati.',
            },
          ],
        },
      ],
    },
    {
      id: 'window',
      title: 'Finestra e stato',
      blocks: [
        {
          kind: 'text',
          text: 'Un silenziamento è in vigore da {{ui:alerts.silences.start}} fino a {{ui:alerts.silences.end}}. {{ui:alerts.silences.start}} è incluso, {{ui:alerts.silences.end}} no. Nell’editor {{ui:alerts.silences.start}} e {{ui:alerts.silences.end}} sono letti nel fuso orario della console e salvati in UTC. La tabella mostra gli orari salvati nel fuso orario della console. Un silenziamento è {{ui:alerts.filter.pending}} prima di {{ui:alerts.silences.start}}, {{ui:alerts.silences.active}} da {{ui:alerts.silences.start}} fino a {{ui:alerts.silences.end}}, e {{ui:alerts.silences.expired}} da {{ui:alerts.silences.end}} in poi.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.filter.pending}}',
              meaning:
                '{{ui:alerts.silences.start}} è nel futuro. La tabella mostra {{ui:alerts.filter.pending}} con il tempo che manca a {{ui:alerts.silences.start}}.',
            },
            {
              term: '{{ui:alerts.silences.active}}',
              meaning:
                'Il silenziamento sopprime adesso. La tabella mostra {{ui:alerts.silences.active}} con il tempo che manca a {{ui:alerts.silences.end}}.',
            },
            {
              term: '{{ui:alerts.silences.expired}}',
              meaning:
                '{{ui:alerts.silences.end}} è passata, oppure il silenziamento è stato terminato. La riga resta nell’elenco.',
            },
          ],
        },
      ],
    },
    {
      id: 'list',
      title: 'Silenziamenti salvati',
      blocks: [
        {
          kind: 'text',
          text: 'La tabella elenca ogni silenziamento salvato, compresi quelli con stato {{ui:alerts.silences.expired}}, ordinati per {{ui:alerts.silences.end}} dalla fine più vicina a quella più lontana. Colonne: {{ui:alerts.silences.matchers}}, {{ui:alerts.silences.reason}}, {{ui:alerts.silences.start}}, {{ui:alerts.silences.end}}, {{ui:alerts.column.state}} e {{ui:alerts.silences.actions}}. {{ui:alerts.silences.start}} e {{ui:alerts.silences.end}} sono mostrati nel fuso orario della console. Ogni riga ha il pulsante {{ui:alerts.silences.endButton}}.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.silences.matchers}}',
              meaning:
                'I matcher del silenziamento, mostrati come descritto in [Matchers](/help/silences#matchers).',
            },
            {
              term: '{{ui:alerts.silences.reason}}',
              meaning: 'Il motivo scritto alla creazione del silenziamento.',
            },
            {
              term: '{{ui:alerts.column.state}}',
              meaning:
                '{{ui:alerts.filter.pending}}, {{ui:alerts.silences.active}} o {{ui:alerts.silences.expired}}, con il tempo residuo, come descritto in [Finestra e stato](/help/silences#window).',
            },
            {
              term: '{{ui:alerts.silences.actions}}',
              meaning:
                '{{ui:alerts.silences.endButton}}, che termina il silenziamento in anticipo. Vedi [Terminare un silenziamento](/help/silences#end-silence).',
            },
          ],
        },
      ],
    },
    {
      id: 'create',
      title: 'Crea silenziamento',
      blocks: [
        {
          kind: 'text',
          text: '{{ui:alerts.silences.create}} apre l’editor sopra l’elenco, con un matcher vuoto, {{ui:alerts.silences.start}} impostato su adesso e {{ui:alerts.silences.end}} un’ora dopo. Il titolo dell’editor è {{ui:alerts.silenceEditor.title}}, e il sottotitolo dice che un silenziamento sopprime solo le notifiche. L’editor crea un silenziamento con matcher esatti, un motivo e una finestra.',
        },
        {
          kind: 'steps',
          items: [
            'Clicca {{ui:alerts.silences.create}} nell’intestazione della pagina. Il pulsante diventa {{ui:alerts.silences.closeEditor}} mentre l’editor è aperto.',
            'Compila Matcher 1 name e Matcher 1 value. {{ui:alerts.silenceEditor.addMatcher}} aggiunge una riga. {{ui:alerts.silenceEditor.remove}}, su una riga, la elimina; {{ui:alerts.silenceEditor.remove}} appare solo con due o più righe.',
            'Inserisci il {{ui:alerts.silences.reason}}: spiega perché le notifiche vanno soppresse.',
            'Imposta {{ui:alerts.silences.start}} e {{ui:alerts.silences.end}}, oppure clicca un preset: 1 hour, 4 hours o 24 hours imposta {{ui:alerts.silences.start}} su adesso e {{ui:alerts.silences.end}} quel numero di ore dopo.',
            'Leggi l’{{ui:alerts.silenceEditor.preview}}. Elenca gli alert firing che questo silenziamento sopprimerebbe.',
            'Se compare l’avviso, spunta la sua casella, con etichetta I understand this will suppress notifications for all N currently firing alerts, dove N è il numero di alert firing.',
            'Clicca {{ui:alerts.silenceEditor.submit}}. Mentre la richiesta è in corso il pulsante mostra {{ui:alerts.silenceEditor.creating}}. In caso di successo l’editor si chiude e sopra la tabella compare il messaggio {{ui:alerts.silences.created}}',
            'Per uscire senza salvare, clicca {{ui:alerts.silenceEditor.cancel}}.',
          ],
        },
        {
          kind: 'text',
          text: 'Messaggi che possono comparire prima del salvataggio di un silenziamento:',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.silenceEditor.matcherIncomplete}}',
              meaning:
                'Una riga di matcher ha un nome senza valore, oppure un valore senza nome. Compare anche quando tutte le righe sono vuote. Compila entrambi i campi di ogni riga, oppure rimuovi la riga con {{ui:alerts.silenceEditor.remove}}.',
            },
            {
              term: '{{ui:alerts.silenceEditor.reasonRequired}}',
              meaning: 'Il campo {{ui:alerts.silences.reason}} è vuoto. Inserisci un motivo.',
            },
            {
              term: '{{ui:alerts.silenceEditor.datesInvalid}}',
              meaning:
                '{{ui:alerts.silences.start}} o {{ui:alerts.silences.end}} non è una data e ora valida.',
            },
            {
              term: '{{ui:alerts.silenceEditor.endBeforeStart}}',
              meaning:
                '{{ui:alerts.silences.end}} non è successivo a {{ui:alerts.silences.start}}. Sposta {{ui:alerts.silences.end}} a un orario più tardi.',
            },
            {
              term: 'Confirm that this silence will suppress all N currently firing alerts.',
              meaning:
                'Il silenziamento sopprimerebbe tutti gli alert firing. Spunta la casella di avviso, poi clicca di nuovo {{ui:alerts.silenceEditor.submit}}. N è il numero di alert firing.',
            },
            {
              term: 'Complete a matcher to see which currently firing alerts would be silenced.',
              meaning: 'Mostrato finché ogni matcher compilato non ha sia il nome sia il valore.',
            },
            {
              term: 'Would suppress notifications for N currently firing alert(s):',
              meaning: 'Gli alert firing che questo silenziamento sopprimerebbe, elencati sotto.',
            },
            {
              term: '{{ui:alerts.silenceEditor.noMatch}}',
              meaning:
                'Nessun alert firing corrisponde. Il silenziamento viene comunque salvato e sopprime gli alert che corrispondono in seguito.',
            },
          ],
        },
      ],
    },
    {
      id: 'effect',
      title: 'Cosa cambia con un silenziamento',
      blocks: [
        {
          kind: 'text',
          text: 'Un silenziamento sopprime solo le notifiche. Un alert che il silenziamento corrisponde resta in firing e rimane nell’elenco di [{{ui:alerts.section.alerts}}](/alerts), dove è mostrato come [{{ui:alerts.status.suppressed}}](/help/alerts#suppression). La sua regola continua a valutarlo. La soppressione viene calcolata a ogni lettura degli alert, con i silenziamenti attivi in quel momento.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'FIRING',
              meaning:
                'Una notifica è inviata quando un alert inizia a firing, non a ogni valutazione. Mentre un silenziamento corrisponde all’alert, questa notifica non viene inviata.',
            },
            {
              term: 'RESOLVED',
              meaning:
                'Anche la notifica inviata quando un alert si risolve non viene inviata mentre un silenziamento corrisponde all’alert.',
            },
            {
              term: 'Notifications already sent',
              meaning: 'Un silenziamento non richiama una notifica già inviata.',
            },
          ],
        },
      ],
    },
    {
      id: 'end-silence',
      title: 'Terminare un silenziamento',
      blocks: [
        {
          kind: 'text',
          text: 'Un silenziamento non si può modificare. Per cambiarlo, terminalo e creane un altro. Terminare un silenziamento non elimina mai la sua riga: la riga resta nell’elenco, e la sua finestra cambia come descritto sotto.',
        },
        {
          kind: 'steps',
          items: [
            'In [{{ui:alerts.silences.stored}}](/help/silences#list) trova la riga e clicca {{ui:alerts.silences.endButton}}.',
            'Conferma la finestra di dialogo. Chiede di terminare il silenziamento e avvisa che le notifiche non saranno più soppresse.',
            'Leggi il messaggio sopra la tabella: Ended silence, seguito dall’id del silenziamento.',
          ],
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.silences.active}}',
              meaning:
                'Termina adesso. Smette subito di sopprimere, e la riga mostra {{ui:alerts.silences.expired}}.',
            },
            {
              term: '{{ui:alerts.filter.pending}}',
              meaning:
                '{{ui:alerts.silences.end}} passa a {{ui:alerts.silences.start}}, quindi non sopprime mai un alert. Mostra {{ui:alerts.filter.pending}} fino a {{ui:alerts.silences.start}}, poi {{ui:alerts.silences.expired}}.',
            },
            {
              term: '{{ui:alerts.silences.expired}}',
              meaning:
                'Non cambia nulla. La sua {{ui:alerts.silences.end}} resta quella originale.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'Terminare un silenziamento non reinvia le notifiche che aveva trattenuto.',
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
              term: '{{ui:alerts.silences.loading}}',
              meaning:
                'L’elenco si sta caricando. Attendi un momento, poi ricarica la pagina se resta così.',
            },
            {
              term: '{{ui:alerts.silences.emptyTitle}}',
              meaning:
                'Nessun silenziamento è ancora stato creato. Creane uno con [{{ui:alerts.silences.create}}](/help/silences#create).',
            },
            {
              term: 'Current firing alerts are unavailable; the silence list remains available but creation preview is disabled.',
              meaning:
                'Non è stato possibile leggere gli alert firing. Puoi ancora vedere e terminare i silenziamenti, ma l’{{ui:alerts.silenceEditor.preview}} non può verificare le corrispondenze e la casella di avviso non compare. Controlla i matcher nella pagina [{{ui:alerts.section.alerts}}](/alerts) prima di salvare, poi clicca {{ui:alerts.silences.retryFiring}}.',
            },
            {
              term: 'Il silenziamento non sopprime un alert',
              meaning:
                'Verifica che lo stato sia {{ui:alerts.silences.active}}, non {{ui:alerts.filter.pending}} o {{ui:alerts.silences.expired}}, e che ogni valore del matcher sia esattamente uguale al valore dell’alert. Confrontali con il dettaglio dell’alert nella pagina [{{ui:alerts.section.alerts}}](/alerts).',
            },
          ],
        },
      ],
    },
  ],
}
