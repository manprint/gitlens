import type { HelpTopic } from '../../types'

export const eventsTopic: HelpTopic = {
  id: 'events',
  group: 'alerting',
  title: 'Eventi',
  summary:
    'La cronologia degli eventi della flotta registrati dal server: failover, agent che hanno smesso o ripreso a inviare dati, conflitti di identità, azzeramenti dei contatori. Ogni evento ha una severità e cosa controllare.',
  page: {
    path: '/events',
    label: 'Apri {{ui:alerts.section.events}}',
  },
  sections: [
    {
      id: 'overview',
      title: 'A cosa serve la pagina',
      blocks: [
        {
          kind: 'text',
          text: 'La pagina {{ui:alerts.section.events}} risponde a una domanda: cosa è cambiato nella flotta, e quando. Il server registra un evento quando rileva un cambiamento rilevante per l’esercizio, per esempio un nuovo primary o un agent che smette di inviare dati. La pagina mostra questi eventi dal più recente, accanto allo stato degli alert, così un cambiamento e gli alert che ha causato si leggono insieme. L’intervallo di tempo della pagina, salvato nell’indirizzo, seleziona gli eventi mostrati; il valore predefinito arriva dalle preferenze.',
        },
        {
          kind: 'steps',
          items: [
            'Apri {{ui:alerts.section.events}} dal gruppo {{ui:shell.nav.alerting}} della barra laterale. Ha due tab: {{ui:alerts.section.events}} e {{ui:alerts.section.alertStatus}}.',
            'Restringi la vista con {{ui:alerts.column.cluster}} ed {{ui:alerts.events.typeLabel}}, poi leggi la colonna {{ui:alerts.column.severity}} dall’alto.',
            'Per un evento importante, leggi i suoi {{ui:alerts.column.details}} e segui il link all’istanza nella colonna {{ui:alerts.column.instance}}.',
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
                  term: '{{ui:alerts.column.cluster}}',
                  meaning:
                    'Mostra gli eventi di un solo cluster. Il valore predefinito, {{ui:alerts.events.allClusters}}, mostra tutti i cluster.',
                },
                {
                  term: '{{ui:alerts.events.typeLabel}}',
                  meaning:
                    'Mostra un solo tipo di evento. Il valore predefinito, {{ui:alerts.events.allTypes}}, mostra tutti i tipi. L’elenco contiene i tipi documentati in [{{ui:alerts.events.timelineTitle}}](/help/events#event-kinds).',
                },
                {
                  term: '{{ui:alerts.events.limitLabel}}',
                  meaning:
                    'Quanti eventi caricare: 100 events (predefinito), 500 events o 1,000 events. Il server restituisce al massimo 1.000 eventi.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'timeline',
      title: 'Cronologia degli eventi',
      blocks: [
        {
          kind: 'text',
          text: 'La cronologia elenca gli eventi dal più recente, per momento e poi per id evento. Ogni riga è un evento. La colonna {{ui:alerts.column.event}} ne indica il tipo, la colonna {{ui:alerts.column.severity}} il peso, e la colonna {{ui:alerts.column.details}} cosa controllare.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.column.time}}',
              meaning: 'Quando il server ha registrato l’evento, nel fuso orario della console.',
            },
            {
              term: '{{ui:alerts.column.event}}',
              meaning:
                'Il titolo del tipo di evento, per esempio Failover detected. Un tipo che la console non documenta mostra Unknown event type.',
            },
            {
              term: '{{ui:alerts.column.severity}}',
              meaning:
                'info, warning o critical, impostato dal tipo di evento. Il colore del badge segue la severità.',
            },
            {
              term: '{{ui:alerts.column.type}}',
              meaning:
                'Il tipo grezzo dell’evento come lo salva il server, per esempio `failover_detected`. Usalo per cercare l’evento altrove.',
            },
            {
              term: '{{ui:alerts.column.cluster}}',
              meaning: 'Il cluster a cui appartiene l’evento.',
            },
            {
              term: '{{ui:alerts.column.instance}}',
              meaning:
                'L’istanza coinvolta, come link alla sua pagina. Unknown quando l’evento non indica un’istanza.',
            },
            {
              term: '{{ui:alerts.column.details}}',
              meaning:
                'Cosa controllare per questo tipo di evento. Alcuni eventi aggiungono dati: failover mostra Old primary e New primary; un azzeramento dei contatori mostra quanti contatori sono stati azzerati e in quale database; un evento di serie scartate spiega che alcune metriche sono state scartate.',
            },
          ],
        },
      ],
      subsections: [
        {
          id: 'event-kinds',
          title: 'Tipi di evento',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'Failover detected',
                  meaning:
                    'Il primary è cambiato: un’altra istanza è stata promossa. I dettagli indicano il vecchio e il nuovo primary.',
                  action:
                    'Verifica se la promozione era pianificata o causata da un guasto del primary. Conferma i ruoli con i controlli in [Controlli di sola lettura](/help/events#checks).',
                },
                {
                  term: 'Split brain detected',
                  meaning:
                    'Più di un’istanza del cluster dichiara il ruolo di primary. Due primary possono accettare scritture che non si possono unire.',
                  action: 'Individua il primary autorevole e degrada l’altro primary.',
                },
                {
                  term: 'Orphan standby',
                  meaning: 'Uno standby non è collegato al primary del suo cluster.',
                  action:
                    'Controlla il primary di riferimento, poi promuovi o ricollega lo standby se serve.',
                },
                {
                  term: 'Role changed',
                  meaning: 'Un’istanza ha cambiato ruolo, per esempio da standby a primary.',
                  action:
                    'Controlla il nuovo ruolo dell’istanza e conferma che il cambio fosse previsto.',
                },
                {
                  term: 'Replication slot inactive',
                  meaning:
                    'Uno slot di replica non ha un consumer attivo, e il primary conserva il WAL di cui lo slot ha bisogno.',
                  action:
                    'Verifica se lo standby è spento o disconnesso e va ricollegato. Esegui il controllo degli slot in [Controlli di sola lettura](/help/events#checks).',
                },
                {
                  term: 'Replication slot lost WAL',
                  meaning:
                    'Lo slot ha perso WAL di cui il suo consumer aveva ancora bisogno. Il consumer non può riprendere dallo slot.',
                  action:
                    'Ricostruisci uno standby fisico, oppure ricrea uno slot logico e risincronizza il suo subscriber. Poi dimensiona `max_slot_wal_keep_size` perché lo slot non si perda di nuovo.',
                },
                {
                  term: 'No primary in cluster',
                  meaning: 'Nessuna istanza del cluster dichiara il ruolo di primary.',
                  action:
                    'Indaga perché nessuna istanza è primary, e promuovi uno standby se serve.',
                },
                {
                  term: 'Counter reset detected',
                  meaning:
                    'I contatori cumulativi sono tornati a zero, per esempio dopo `pg_stat_reset()` o un riavvio dell’istanza. L’evento indica quanti contatori sono stati azzerati e in quale database.',
                  action:
                    'Verifica la presenza di `pg_stat_reset()` o di un riavvio dell’istanza. Di solito è previsto.',
                },
                {
                  term: 'Agent down',
                  meaning: 'L’agent di un’istanza ha smesso di inviare dati.',
                  action:
                    'Controlla i log dell’agent, il container, la raggiungibilità di rete e il server.',
                },
                {
                  term: 'Agent recovered',
                  meaning: 'Un agent che aveva smesso di inviare dati ha ripreso a farlo.',
                  action: 'Conferma che l’istanza funzioni normalmente dopo il vuoto di dati.',
                },
                {
                  term: 'Duplicate instance suspected',
                  meaning: 'Due agent potrebbero inviare dati con la stessa identità di istanza.',
                  action:
                    'Verifica che l’agent conservi il suo `instance_id` in una memoria persistente.',
                },
                {
                  term: '{{ui:alerts.column.cluster}} identity changed',
                  meaning:
                    'Violazione critica dell’invariante I-1: l’identità del cluster non deve mai cambiare.',
                  action:
                    'Indaga l’invariante I-1, e controlla l’identificatore di sistema dell’agent e i log.',
                },
                {
                  term: '{{ui:alerts.column.instance}} unreachable',
                  meaning: 'Il server ha smesso di ricevere report dall’istanza.',
                  action: 'Controlla il suo agent e il percorso di rete.',
                },
                {
                  term: '{{ui:alerts.column.instance}} reachable',
                  meaning: 'I report di un’istanza non raggiungibile sono ripresi.',
                  action: 'Conferma che l’istanza funzioni normalmente dopo il vuoto di dati.',
                },
                {
                  term: 'Series truncated by cardinality budget',
                  meaning:
                    'Alcune metriche sono state scartate perché l’istanza ha inviato più serie distinte del suo limite.',
                  action:
                    'I dettagli indicano il check e il database. Riduci gli oggetti che riporta, oppure rivedi il suo budget.',
                },
                {
                  term: '{{ui:alerts.column.cluster}} identity conflict',
                  meaning:
                    'L’istanza ha inviato il `cluster_name` configurato invece del suo identificatore di sistema.',
                  action: 'Applica `monitoring_user.sql`, poi allinea `cluster_name`.',
                },
                {
                  term: 'Lock wait captured',
                  meaning:
                    'L’agent ha visto sessioni in attesa di un lock oltre la sua soglia di sorveglianza. I dettagli indicano la tabella, la riga (`ctid`), il modo del lock e, per ogni sessione, utente, applicazione, statement e la sessione che la blocca.',
                  action:
                    'Trova il bloccante principale, la sessione che non aspetta nessuno. Terminala solo se il suo lavoro si può perdere; altrimenti attendi o correggi l’applicazione che tiene il lock.',
                },
                {
                  term: 'Deadlock captured',
                  meaning:
                    'Alcune sessioni si aspettavano a vicenda in un ciclo. PostgreSQL ne annulla una dopo `deadlock_timeout`; l’agent ha catturato il ciclo prima: tabelle, righe e statement.',
                  action:
                    'Non c’è nulla da terminare: il deadlock è già risolto. Fai bloccare le righe nello stesso ordine in ogni transazione, per esempio per chiave primaria.',
                },
                {
                  term: 'Long query captured',
                  meaning:
                    'Uno statement è in esecuzione da più della soglia di sorveglianza. I dettagli riportano lo statement, la sua durata e la sessione.',
                  action:
                    'Controlla il suo piano in Query plans. Annullalo solo se non deve terminare.',
                },
                {
                  term: 'Idle in transaction captured',
                  meaning:
                    'Una sessione ha aperto una transazione e poi non ha fatto nulla. Tiene i suoi lock e frena il vacuum.',
                  action:
                    'Correggi l’applicazione che non fa commit, oppure imposta `idle_in_transaction_session_timeout`.',
                },
                {
                  term: 'Long transaction captured',
                  meaning:
                    'Una transazione è rimasta aperta oltre la soglia di sorveglianza. I dettagli indicano la sessione, le tabelle che blocca e quanto frena il vacuum (età di xmin).',
                  action:
                    'Chiedi al responsabile della sessione di fare commit, oppure terminala se il suo lavoro si può perdere.',
                },
                {
                  term: 'Unknown event type',
                  meaning:
                    'Il tipo non è documentato nella console. La colonna {{ui:alerts.column.type}} mostra il tipo grezzo.',
                  action: 'Esamina il tipo grezzo dell’evento e il suo payload prima di agire.',
                },
              ],
            },
          ],
        },
        {
          id: 'checks',
          title: 'Controlli di sola lettura',
          blocks: [
            {
              kind: 'text',
              text: 'Queste istruzioni leggono soltanto lo stato. Eseguine ciascuna sull’istanza indicata nell’etichetta.',
            },
            {
              kind: 'sql',
              label: 'Conferma il ruolo di un’istanza (eseguire su quella istanza)',
              code: 'SELECT pg_is_in_recovery();',
            },
            {
              kind: 'sql',
              label: 'Elenca gli standby visti dal primary (eseguire sul primary)',
              code: 'SELECT application_name, state, sync_state FROM pg_stat_replication;',
            },
            {
              kind: 'sql',
              label: 'Controlla lo stato degli slot di replica (eseguire sul primary)',
              code: 'SELECT slot_name, active, restart_lsn, wal_status FROM pg_replication_slots;',
            },
          ],
        },
      ],
    },
    {
      id: 'alert-status',
      title: '{{ui:alerts.section.alertStatus}}',
      blocks: [
        {
          kind: 'text',
          text: 'La tab {{ui:alerts.section.alertStatus}} elenca gli alert caricati per la pagina, ciascuno con severità, stato e soppressione. Usala per vedere subito quali alert sono in firing e se sono silenziati. Usa la stessa lista di alert della pagina {{ui:alerts.section.alerts}}, quindi vale anche qui il limite di 500 alert.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.column.alert}}',
              meaning: 'La chiave dell’alert: la regola e l’oggetto per cui scatta.',
            },
            {
              term: '{{ui:alerts.column.severity}}',
              meaning: 'critical, warning o info, impostato dalla regola.',
            },
            {
              term: '{{ui:alerts.column.state}}',
              meaning: 'firing o resolved, come nella pagina {{ui:alerts.section.alerts}}.',
            },
            {
              term: '{{ui:alerts.filter.suppression}}',
              meaning:
                '{{ui:alerts.status.suppressed}} quando un silenziamento attivo corrisponde all’alert, {{ui:alerts.filter.notSuppressed}} altrimenti. Unknown quando non è stato possibile caricare i silenziamenti.',
              action:
                'Per un alert {{ui:alerts.status.suppressed}}, leggi il suo silenziamento in [{{ui:shell.nav.silences}}](/help/silences#overview) e decidi se vale ancora.',
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
              term: '{{ui:alerts.events.emptyTitle}}',
              meaning:
                'Nessun evento corrisponde all’intervallo di tempo e ai filtri scelti. Allarga l’intervallo, oppure rimuovi {{ui:alerts.column.cluster}} ed {{ui:alerts.events.typeLabel}}.',
            },
            {
              term: '{{ui:alerts.events.noAlerts}}',
              meaning:
                'La tab {{ui:alerts.section.alertStatus}} non ha alert da mostrare. Controlla la pagina {{ui:alerts.section.alerts}}: se è vuota anche lì, nessun alert è stato generato.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'L’elenco degli eventi è limitato a 1.000 risultati; eventi più vecchi possono esistere fuori da questa vista. Restringi l’intervallo di tempo per raggiungerli.',
        },
      ],
    },
  ],
}
