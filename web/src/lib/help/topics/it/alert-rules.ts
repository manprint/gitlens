import type { HelpTopic } from '../../types'

export const alertRulesTopic: HelpTopic = {
  id: 'alert-rules',
  group: 'alerting',
  title: 'Regole di alert',
  summary:
    'Ogni regola che può generare un alert: cosa rileva, soglia e durata predefinite, cosa controllare quando scatta, e come un operatore può cambiarne soglia, durata, severità e stato di abilitazione.',
  page: {
    path: '/alerts/rules',
    label: 'Apri Alert rules',
  },
  sections: [
    {
      id: 'overview',
      title: 'A cosa serve la pagina',
      blocks: [
        {
          kind: 'text',
          text: 'La pagina {{ui:alerts.rules.title}} elenca ogni regola che può generare un alert, con livello, severità, soglia, durata e stato di abilitazione. Una regola è la condizione; un alert è la regola in firing per un cluster o una istanza. Ci sono due livelli. Le regole T0 sono integrate nel server: sono sempre attive e non si modificano. Le regole T1 sono inserite dal server con valori predefiniti e possono essere modificate da un operatore. Per impostazione predefinita le regole vengono valutate ogni 30 secondi.',
        },
        {
          kind: 'steps',
          items: [
            'Apri {{ui:alerts.rules.title}} dal gruppo {{ui:shell.nav.alerting}} della barra laterale, oppure dall’intestazione della pagina [{{ui:shell.nav.alerts}}](/help/alerts).',
            'Trova la regola che è scattata in [{{ui:alerts.rules.configured}}](/help/alert-rules#rules-table) e leggi il suo {{ui:alerts.rules.tier}}.',
            'Leggi la sua descrizione in [Regole integrate (T0)](/help/alert-rules#built-in-rules) o in [Regole gestite dall’operatore (T1)](/help/alert-rules#operator-rules), poi segui il suo controllo.',
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
                  term: '{{ui:alerts.column.severity}}',
                  meaning:
                    'Limita l’elenco alle regole di una sola severità: critical, warning o info.',
                },
                {
                  term: '{{ui:alerts.rules.enabled}}',
                  meaning: 'Limita l’elenco alle regole Yes (abilitate) o No (disabilitate).',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'rules-table',
      title: '{{ui:alerts.rules.configured}}',
      blocks: [
        {
          kind: 'text',
          text: 'Una riga per regola. Le colonne sono {{ui:alerts.column.rule}}, {{ui:alerts.rules.tier}}, {{ui:alerts.column.severity}}, {{ui:alerts.rules.threshold}}, {{ui:alerts.rules.duration}}, {{ui:alerts.rules.enabled}} e {{ui:alerts.column.action}}. L’intestazione della pagina indica che l’elenco mostra le soglie integrate e quelle gestite dall’operatore.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.column.rule}}',
              meaning:
                'L’id della regola, per esempio `agent_down` o `disk.free_low`. L’id è fisso: indica la condizione che il server valuta.',
            },
            {
              term: '{{ui:alerts.rules.tier}}',
              meaning:
                'T0 è una regola integrata, sempre attiva e non modificabile. T1 è una regola gestita dall’operatore che si può modificare.',
            },
            {
              term: '{{ui:alerts.column.severity}}',
              meaning: 'La severità che la regola assegna ai suoi alert: critical, warning o info.',
            },
            {
              term: '{{ui:alerts.rules.threshold}}',
              meaning:
                'La condizione in parole. Esempi: "above 30 s" per un ritardo in secondi; "above 80% max_connections" per le connessioni; "below 15% free" per lo spazio su disco; "any deadlock" per il tasso di deadlock; "on event" per le regole su evento, e "no report" per agent_down.',
            },
            {
              term: '{{ui:alerts.rules.duration}}',
              meaning:
                'Per quanto tempo la condizione deve valere prima che l’alert scatti, in secondi. Zero significa che l’alert scatta subito.',
            },
            {
              term: '{{ui:alerts.rules.enabled}}',
              meaning:
                'Yes o No. Una regola T1 disabilitata non viene valutata. Una regola T0 è sempre attiva.',
            },
            {
              term: '{{ui:alerts.column.action}}',
              meaning:
                'Per una regola T0 la cella riporta {{ui:alerts.rules.alwaysOn}}. Per una regola T1 è il pulsante Edit seguito dall’id della regola, che apre l’editor.',
            },
          ],
        },
      ],
    },
    {
      id: 'built-in-rules',
      title: 'Regole integrate (T0)',
      blocks: [
        {
          kind: 'text',
          text: 'Le regole integrate sono sempre attive, e la colonna {{ui:alerts.column.action}} riporta {{ui:alerts.rules.alwaysOn}}. Cinque regole leggono una metrica e sei leggono un evento. Una regola su evento può essere di due tipi. Una spell, come `slot_inactive`, resta in firing finché la sua condizione vale. Una pulse, come `failover_detected`, è una singola occorrenza: si risolve alla prima valutazione che non trova una nuova occorrenza, e una occorrenza successiva la riporta in firing. Una regola su metrica si chiude solo dopo 10 minuti senza campioni, quindi brevi vuoti nei suoi dati non la risolvono.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'agent_down',
              meaning:
                'Predefinito: critical, ambito istanza. La metrica `up` resta sotto 1 per 90 secondi. Non è arrivato alcun report dall’agent di un’istanza.',
              action:
                'Controlla i log dell’agent, il container, la raggiungibilità di rete e il server. Il tipo di evento è in [Event kinds](/help/events#event-kinds).',
            },
            {
              term: 'instance_unreachable',
              meaning:
                'Predefinito: critical, ambito istanza, generato dall’evento con lo stesso nome. Il server non ha ricevuto report da un’istanza entro l’intervallo atteso, e insieme viene generato `agent_down`.',
              action: 'Controlla l’agent e il percorso di rete verso l’istanza.',
            },
            {
              term: 'check_failing',
              meaning:
                'Predefinito: warning, ambito istanza. La metrica `pglens_check_error_rate` è sopra 0 per 5 minuti. Un check fallisce da più di cinque minuti.',
              action:
                'Apri l’istanza per vedere quale check fallisce, e correggi la causa che riporta, per esempio un privilegio mancante sul database monitorato.',
            },
            {
              term: 'no_primary_in_cluster',
              meaning:
                'Predefinito: critical, ambito cluster, generato dall’evento con lo stesso nome. Nessuna istanza del cluster dichiara il ruolo di primary.',
              action: 'Indaga perché nessuna istanza è primary, e promuovi uno standby se serve.',
            },
            {
              term: 'agent_buffer_full',
              meaning:
                'Predefinito: warning, ambito istanza. La metrica `pglens_samples_dropped_rate` è sopra 0 per 1 minuto. L’agent scarta campioni perché il suo buffer è pieno.',
              action:
                'Verifica che l’agent raggiunga il server e che il server accetti i suoi report.',
            },
            {
              term: 'clock_skew',
              meaning:
                'Predefinito: warning, ambito istanza. La metrica `pglens_agent_clock_skew_seconds` è sopra 30 per 1 minuto. L’orologio dell’agent differisce da quello del server di più di 30 secondi.',
              action:
                'Sincronizza l’orologio dell’host dell’agent con NTP, poi verifica che la differenza sia sparita.',
            },
            {
              term: 'cardinality_budget_exceeded',
              meaning:
                'Predefinito: warning, ambito istanza. La metrica `pglens_cardinality_truncated_rate` è sopra 0 per 5 minuti. Un’istanza ha inviato più serie distinte del suo limite, quindi alcune sono state scartate.',
              action:
                'Riduci gli oggetti che il check riporta, oppure rivedi il suo budget. L’evento indica il check e il database.',
            },
            {
              term: 'failover_detected',
              meaning:
                'Predefinito: critical, ambito cluster, generato dall’evento con lo stesso nome. È stato osservato un failover: un’altra istanza è diventata primary.',
              action:
                'Verifica se la promozione era pianificata o causata da un guasto del primary.',
            },
            {
              term: 'split_brain_detected',
              meaning:
                'Predefinito: critical, ambito cluster, generato dall’evento con lo stesso nome. Più di un’istanza del cluster dichiara il ruolo di primary.',
              action: 'Individua il primary autorevole e degrada l’altro primary.',
            },
            {
              term: 'slot_inactive',
              meaning:
                'Predefinito: warning, ambito istanza, generato dall’evento con lo stesso nome, come spell. Uno slot di replica è inattivo e trattiene WAL sul primary.',
              action:
                'Verifica se lo standby è spento o disconnesso e va ricollegato. Il controllo degli slot è in [Controlli di sola lettura](/help/alert-rules#checks).',
            },
            {
              term: 'slot_lost',
              meaning:
                'Predefinito: critical, ambito istanza, generato dall’evento con lo stesso nome, come spell. Uno slot di replica ha perso WAL di cui il suo consumer aveva ancora bisogno.',
              action:
                'Ricostruisci uno standby fisico, oppure ricrea uno slot logico e risincronizza il suo subscriber, poi dimensiona `max_slot_wal_keep_size` perché lo slot non si perda di nuovo.',
            },
          ],
        },
      ],
    },
    {
      id: 'operator-rules',
      title: 'Regole gestite dall’operatore (T1)',
      blocks: [
        {
          kind: 'text',
          text: 'Le regole T1 sono inserite dal server con valori predefiniti, e un operatore può modificarne soglia, durata, severità e stato di abilitazione. L’id della regola e ciò che misura restano fissi. Il server inserisce una regola solo se manca, quindi una modifica sopravvive ai riavvii e agli aggiornamenti. Ogni riga qui sotto indica il valore predefinito, cosa significa la regola e cosa controllare.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'replica.lag_high',
              meaning:
                'Predefinito: warning, ambito istanza. Ritardo sopra 30 secondi per 120 secondi. Uno standby è più di 30 secondi indietro rispetto al suo primary.',
              action:
                'Misura il ritardo di replay sullo standby con il controllo in [Controlli di sola lettura](/help/alert-rules#checks). Poi verifica la rete tra i nodi e le query lunghe sullo standby, che possono trattenere il replay.',
            },
            {
              term: 'replica.all_standbys_lagging',
              meaning:
                'Predefinito: critical, ambito cluster. Ritardo sopra 30 secondi per 120 secondi. Ogni standby del cluster è più di 30 secondi indietro.',
              action:
                'Un ritardo su ogni standby indica il primary o la rete, non un singolo standby. Controlla il carico di scrittura sul primary e il percorso di rete verso gli standby.',
            },
            {
              term: 'replica.no_sync_standby',
              meaning:
                'Predefinito: critical, ambito cluster. Meno di 1 standby sincrono per 60 secondi. Il cluster non ha alcuno standby sincrono.',
              action:
                'Controlla `synchronous_standby_names` sul primary e il `sync_state` degli standby con i controlli in [Controlli di sola lettura](/help/alert-rules#checks).',
            },
            {
              term: 'conn.near_max',
              meaning:
                'Predefinito: warning, ambito istanza. Connessioni sopra l’80% di `max_connections` per 120 secondi. Più dell’80% delle connessioni consentite da `max_connections` è in uso.',
              action:
                'Trova quali client occupano le connessioni con il controllo in [Controlli di sola lettura](/help/alert-rules#checks). Riduci prima la dimensione del pool di quei client; alza `max_connections` solo se la memoria del server lo permette.',
            },
            {
              term: 'conn.idle_in_transaction',
              meaning:
                'Predefinito: warning, ambito istanza. Una sessione inattiva in una transazione da più di 300 secondi, per 120 secondi. Una sessione è inattiva in una transazione da più di 5 minuti.',
              action:
                'Trova le sessioni con il controllo in [Controlli di sola lettura](/help/alert-rules#checks), e chiedi al proprietario dell’applicazione di fare commit o rollback. Termina una sessione solo dopo averla verificata.',
            },
            {
              term: 'txn.long_running',
              meaning:
                'Predefinito: warning, ambito istanza. Una transazione aperta da più di 900 secondi, per 120 secondi. Una transazione è aperta da molto tempo.',
              action:
                'Trova la transazione con il controllo in [Controlli di sola lettura](/help/alert-rules#checks). Una transazione lunga trattiene il vacuum, quindi chiedi al suo proprietario di concluderla.',
            },
            {
              term: 'txn.wraparound_risk',
              meaning:
                'Predefinito: critical, ambito istanza. L’età dell’ID di transazione più vecchio sopra 1.000.000.000 per 300 secondi. L’ID di transazione più vecchio di un database sta invecchiando: serve un vacuum di freeze.',
              action:
                'Trova il database con il controllo in [Controlli di sola lettura](/help/alert-rules#checks), rimuovi i blocchi (transazioni lunghe, sessioni inattive), poi esegui `VACUUM (FREEZE)` su quel database in una finestra di manutenzione.',
            },
            {
              term: 'db.deadlock_rate',
              meaning:
                'Predefinito: warning, ambito istanza. Qualsiasi deadlock: la soglia è 0 e la durata è 0 secondi. Si stanno verificando deadlock.',
              action:
                'Leggi il report del deadlock nel log del server, che indica le istruzioni coinvolte, e fai in modo che le transazioni prendano i lock nello stesso ordine.',
            },
            {
              term: 'archive.failing',
              meaning:
                'Predefinito: critical, ambito istanza. L’ultimo tentativo di archiviazione è fallito, per 300 secondi. L’archiviazione del WAL non funziona: i file WAL completati non vengono copiati.',
              action:
                'Controlla `archive_command` e la destinazione di archivio con il controllo dell’archiviatore in [Controlli di sola lettura](/help/alert-rules#checks). Risolvi la causa prima che la directory del WAL cresca.',
            },
            {
              term: 'disk.free_low',
              meaning:
                'Predefinito: critical, ambito istanza. Spazio libero sul disco dei dati sotto il 15% per 300 secondi. Meno del 15% del disco dei dati è libero.',
              action:
                'Trova cosa cresce, come tabelle, WAL o log, poi libera spazio o estendi il disco. Il controllo in [Controlli di sola lettura](/help/alert-rules#checks) indica dove si trova la data directory.',
            },
            {
              term: 'replica.redundancy_reduced',
              meaning:
                'Predefinito: warning, ambito cluster. Più di 0 standby mancanti per 60 secondi. Trasmettono meno standby di quanti membri ha il cluster: uno standby è spento, disconnesso o non ancora rientrato; un membro silenzioso da più di un’ora non viene più conteggiato.',
              action:
                'Trova lo standby che non trasmette. Verifica se è spento, disconnesso, o non è ancora rientrato dopo un failover.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'Ogni controllo qui sotto legge soltanto lo stato; terminare una sessione nella riga idle-in-transaction è invece un’azione separata e deliberata.',
        },
      ],
      subsections: [
        {
          id: 'checks',
          title: 'Controlli di sola lettura',
          blocks: [
            {
              kind: 'text',
              text: 'Queste istruzioni di sola lettura supportano le regole sopra. Eseguine ciascuna sull’istanza indicata nell’etichetta.',
            },
            {
              kind: 'sql',
              label: 'Ritardo di replay su uno standby (eseguire sullo standby)',
              code: 'SELECT now() - pg_last_xact_replay_timestamp() AS replay_lag;',
            },
            {
              kind: 'sql',
              label: 'Standby e loro stato (eseguire sul primary)',
              code: 'SELECT application_name, state, sync_state, replay_lag FROM pg_stat_replication;',
            },
            {
              kind: 'sql',
              label: 'Impostazione dello standby sincrono (eseguire sul primary)',
              code: 'SHOW synchronous_standby_names;',
            },
            {
              kind: 'sql',
              label: 'Connessioni in uso rispetto al limite',
              code: "SELECT count(*) AS connections, current_setting('max_connections') AS max_connections FROM pg_stat_activity;",
            },
            {
              kind: 'sql',
              label: 'Sessioni inattive in una transazione',
              code: "SELECT pid, usename, datname, now() - state_change AS idle_for FROM pg_stat_activity WHERE state = 'idle in transaction' ORDER BY idle_for DESC;",
            },
            {
              kind: 'sql',
              label: 'Transazioni aperte più vecchie',
              code: 'SELECT pid, usename, state, now() - xact_start AS xact_age FROM pg_stat_activity WHERE xact_start IS NOT NULL ORDER BY xact_start LIMIT 10;',
            },
            {
              kind: 'sql',
              label: 'Età dell’ID di transazione più vecchio per database',
              code: 'SELECT datname, age(datfrozenxid) AS xid_age FROM pg_database ORDER BY xid_age DESC;',
            },
            {
              kind: 'sql',
              label: 'Deadlock per database',
              code: 'SELECT datname, deadlocks FROM pg_stat_database WHERE deadlocks > 0;',
            },
            {
              kind: 'sql',
              label: 'Stato dell’archiviatore',
              code: 'SELECT archived_count, last_archived_wal, failed_count, last_failed_wal, last_failed_time FROM pg_stat_archiver;',
            },
            {
              kind: 'sql',
              label: 'Data directory',
              code: 'SHOW data_directory;',
            },
          ],
        },
      ],
    },
    {
      id: 'editing',
      title: 'Modificare una regola',
      blocks: [
        {
          kind: 'text',
          text: 'Solo le regole T1 si possono modificare. La cella {{ui:alerts.column.action}} di una riga T1 riporta Edit seguito dall’id della regola. L’editor si apre sopra l’elenco con i valori attuali della regola, e {{ui:alerts.ruleEditor.cancel}} lo chiude senza salvare. L’editor modifica quattro campi: {{ui:alerts.rules.threshold}}, {{ui:alerts.rules.duration}}, {{ui:alerts.column.severity}} ed {{ui:alerts.rules.enabled}}.',
        },
        {
          kind: 'steps',
          items: [
            'Trova la regola in [{{ui:alerts.rules.configured}}](/help/alert-rules#rules-table) e verifica che il suo {{ui:alerts.rules.tier}} sia T1.',
            'Clicca Edit seguito dall’id della regola nella colonna {{ui:alerts.column.action}}.',
            'Cambia {{ui:alerts.rules.threshold}}, {{ui:alerts.ruleEditor.duration}}, {{ui:alerts.column.severity}} o {{ui:alerts.rules.enabled}}.',
            'Clicca {{ui:alerts.ruleEditor.save}}. Una conferma indica la regola e la sua nuova soglia, durata e severità.',
            'Per uscire senza salvare, clicca {{ui:alerts.ruleEditor.cancel}}.',
          ],
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.rules.threshold}}',
              meaning:
                'Il valore con cui viene confrontata la metrica. Deve essere un numero finito, quindi inserisci un numero semplice.',
            },
            {
              term: '{{ui:alerts.ruleEditor.duration}}',
              meaning:
                'Per quanti secondi la condizione deve valere prima che l’alert scatti, in secondi interi da 0 a 2147483647.',
            },
            {
              term: '{{ui:alerts.column.severity}}',
              meaning: 'La severità che la regola assegna ai suoi alert: critical, warning o info.',
            },
            {
              term: '{{ui:alerts.rules.enabled}}',
              meaning:
                'Yes o No. Una regola T1 disabilitata non viene valutata, quindi non genera nuovi alert.',
            },
            {
              term: '{{ui:alerts.ruleEditor.thresholdInvalid}}',
              meaning:
                'Il campo {{ui:alerts.rules.threshold}} contiene un valore che non è un numero, oppure è infinito. Inserisci un numero finito.',
            },
            {
              term: '{{ui:alerts.ruleEditor.durationInvalid}}',
              meaning:
                'Il campo {{ui:alerts.rules.duration}} contiene un valore che non è un numero intero di secondi in quell’intervallo. Inserisci un numero intero.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'Una regola si applica a tutta la flotta, non a una sola istanza. Una modifica a una soglia o a {{ui:alerts.rules.enabled}} cambia ciò che scatta per ogni istanza coperta dalla regola.',
        },
      ],
    },
    {
      id: 'notification-channels',
      title: '{{ui:settings.notifications.listLabel}}',
      blocks: [
        {
          kind: 'text',
          text: 'Una regola decide quando un alert scatta; non sceglie dove va la notifica. I canali si impostano sul server per tutta l’installazione: vedi [{{ui:alerts.section.delivery}}](/help/alerts#delivery). [{{ui:alerts.delivery.settingsLink}}](/settings?tab=notifications) mostra quali canali sono configurati e invia un messaggio di prova.',
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
              term: '{{ui:alerts.rules.emptyDescription}}',
              meaning:
                'Il server non ha restituito regole. Ricarica la pagina; se l’elenco resta vuoto, controlla il log del server.',
            },
            {
              term: '{{ui:alerts.rules.alwaysOn}}',
              meaning:
                'La regola è una regola integrata T0. È sempre attiva e non si può modificare.',
            },
            {
              term: "Una modifica non cambia l'alert",
              meaning:
                'Le regole vengono valutate periodicamente, ogni 30 secondi per impostazione predefinita. Attendi una valutazione, poi ricarica la pagina {{ui:shell.nav.alerts}}. Se non cambia nulla, verifica che la regola sia {{ui:alerts.rules.enabled}}.',
            },
          ],
        },
      ],
    },
  ],
}
