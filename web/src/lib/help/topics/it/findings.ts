import type { HelpTopic } from '../../types'

export const findingsTopic: HelpTopic = {
  id: 'findings',
  group: 'monitoring',
  title: 'Segnalazioni dell’advisor',
  summary:
    'I finding dell’advisor per istanze e cluster: ogni problema trovato da una regola, con la sua severity, il suo stato e la remediation da verificare. Il {{ui:findings.ruleCatalogue.title}} elenca ogni regola.',
  page: {
    path: '/findings',
    label: 'Apri {{ui:findings.advisorFindings}}',
  },
  sections: [
    {
      id: 'overview',
      title: 'A cosa serve la pagina',
      blocks: [
        {
          kind: 'text',
          text: 'L’advisor controlla le istanze e i cluster monitorati con un insieme di regole. Ogni problema trovato è un finding: una regola, un oggetto, una severity e uno stato.',
        },
        {
          kind: 'text',
          text: 'La pagina elenca i finding di tutte le istanze e di tutti i cluster. La vista predefinita è {{ui:findings.page.stateActive}}, che nasconde i finding muted e resolved.',
        },
        {
          kind: 'steps',
          items: [
            'Leggi i pulsanti di riepilogo in alto: {{ui:findings.page.severityCritical}}, {{ui:findings.page.severityWarning}}, {{ui:findings.page.severityInfo}} e {{ui:findings.page.notEvaluated}}. Cliccane uno per filtrare la tabella.',
            'Restringi l’elenco con {{ui:findings.page.state}}, {{ui:findings.table.severity}}, {{ui:findings.page.scope}}, {{ui:findings.page.cluster}}, {{ui:findings.page.instance}} o con la casella {{ui:findings.page.searchLabel}}.',
            'Apri i dettagli di una riga per leggere la sua finding card. Vedi [Finding card](/help/findings#finding-card).',
            'Per silenziare un finding per un periodo definito, usa {{ui:findings.mute.mute}}. Vedi [Mute dialog](/help/findings#mute-dialog).',
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'La remediation è un testo da verificare da parte dell’operatore. pglens la mostra e non la esegue.',
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'Una regola non valutata non equivale a un esito sano “nessun finding”. Il suo problema resta sconosciuto finché la regola non può girare.',
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
                  term: 'Findings e {{ui:findings.ruleCatalogue.title}}',
                  meaning:
                    'Due tab: Findings elenca i finding; {{ui:findings.ruleCatalogue.title}} elenca ogni regola dell’advisor. Vedi [{{ui:findings.ruleCatalogue.title}}](/help/findings#rule-catalogue).',
                },
                {
                  term: 'Summary buttons',
                  meaning:
                    '{{ui:findings.page.severityCritical}}, {{ui:findings.page.severityWarning}} e {{ui:findings.page.severityInfo}} filtrano la tabella per severity. {{ui:findings.page.notEvaluated}} imposta {{ui:findings.page.state}} su {{ui:findings.page.stateDegraded}} e cancella {{ui:findings.table.severity}}.',
                },
                {
                  term: '{{ui:findings.page.state}}',
                  meaning:
                    'Filtra i finding per stato: {{ui:findings.page.stateActive}} (il default), {{ui:findings.page.allStates}}, Open, Degraded, Muted o Resolved. Vedi [States](/help/findings#states).',
                },
                {
                  term: '{{ui:findings.table.severity}}',
                  meaning:
                    'Filtra per severity: {{ui:findings.ruleCatalogue.allSeverities}}, Critical, Warning o Info. Vedi [Severities](/help/findings#severities).',
                },
                {
                  term: '{{ui:findings.page.scope}}',
                  meaning:
                    'Filtra per scope: {{ui:findings.ruleCatalogue.allScopes}}, Cluster o Instance.',
                },
                {
                  term: '{{ui:findings.page.cluster}}',
                  meaning: '{{ui:findings.page.allClusters}}, oppure un solo cluster.',
                },
                {
                  term: '{{ui:findings.page.instance}}',
                  meaning: '{{ui:findings.page.allInstances}}, oppure una sola istanza.',
                },
                {
                  term: '{{ui:findings.page.searchLabel}}',
                  meaning:
                    'Ricerca libera. Il placeholder indica {{ui:findings.page.searchPlaceholder}}. Lo stato vuoto elenca i campi cercati: rule, title, detail, remediation, object, database, mute reason ed evidence.',
                },
              ],
            },
            {
              kind: 'note',
              tone: 'info',
              text: 'Vengono caricati solo i primi 1000 finding. Quando si raggiunge il limite, la pagina dice: “Only the first 1000 findings were returned. {{ui:findings.page.stateActive}} findings come first, so muted and resolved findings may be missing from the counts and the list.”',
            },
            {
              kind: 'text',
              text: 'La riga dei risultati sopra la tabella può terminare con “N muted and M resolved hidden.”, contando i finding che la vista corrente nasconde.',
            },
          ],
        },
      ],
    },
    {
      id: 'severities',
      title: 'Severity',
      blocks: [
        {
          kind: 'text',
          text: 'Ogni regola ha una severity. La colonna {{ui:findings.table.severity}} e il filtro {{ui:findings.table.severity}} usano {{ui:findings.page.severityCritical}}, {{ui:findings.page.severityWarning}} e {{ui:findings.page.severityInfo}}.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:findings.page.severityCritical}}',
              meaning:
                'I finding più gravi, come `config.fsync_off`, `archive.failing` e `txn.wraparound_risk`.',
            },
            {
              term: '{{ui:findings.page.severityWarning}}',
              meaning: 'Un problema da verificare, come `table.dead_tuples_high` e `index.unused`.',
            },
            {
              term: '{{ui:findings.page.severityInfo}}',
              meaning:
                'Un finding da conoscere, con una priorità più bassa, come `archive.disabled` e `table.seq_scan_heavy`.',
            },
            {
              term: '{{ui:findings.page.notEvaluated}}',
              meaning:
                'Il pulsante di riepilogo. Conta i finding degraded, non le regole: una regola non valutata per un’istanza. I finding muted e resolved non vengono contati.',
            },
          ],
        },
        {
          kind: 'text',
          text: 'Alcune regole fissano la severity in base a dimensione, righe o età: `index.fk_unindexed` è Info sotto 10 MiB di dimensione della tabella e Warning da 10 MiB in su; `postgis.spatial_unindexed` è Info fino a 10.000 righe e Warning sopra; `partition.future_missing` è Warning, oppure Critical quando gli inserimenti già falliscono; `function.secdef_no_search_path` è Warning, oppure Critical quando il proprietario è un superuser e `PUBLIC` ha `EXECUTE`; `sequence.exhaustion_risk` è Warning dal 75% dell’intervallo e Critical dal 90%.',
        },
      ],
    },
    {
      id: 'states',
      title: 'Stati',
      blocks: [
        {
          kind: 'text',
          text: 'Ogni finding ha un solo stato. Il filtro State e la colonna State usano questi valori.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:findings.page.stateOpen}}',
              meaning: 'Il problema è presente ora.',
            },
            {
              term: '{{ui:findings.page.stateDegraded}}',
              meaning:
                'La regola non è stata valutata per un’istanza. I finding degraded sono conteggiati in {{ui:findings.page.notEvaluated}}.',
              action:
                'Leggi la riga della regola nel {{ui:findings.ruleCatalogue.title}}: {{ui:findings.ruleCatalogue.cannotEvaluate}} elenca le istanze in cui non può girare.',
            },
            {
              term: '{{ui:findings.page.stateMuted}}',
              meaning:
                'Hai silenziato il finding per un periodo. Vedi [Mute dialog](/help/findings#mute-dialog).',
              action:
                'Silenzialo di nuovo con un motivo se serve ancora, oppure risolvi il problema.',
            },
            {
              term: '{{ui:findings.page.stateResolved}}',
              meaning: 'Il problema non c’è più.',
            },
          ],
        },
      ],
    },
    {
      id: 'findings-table',
      title: '{{ui:findings.page.tableHelp}}',
      blocks: [
        {
          kind: 'text',
          text: 'Una riga per finding, con quattro colonne. {{ui:findings.table.severity}}, {{ui:findings.table.finding}} e {{ui:findings.table.lastSeen}} possono ordinare la tabella. Nulla è tagliato: un titolo lungo va a capo e il testo dello statement occupa due righe, completo nella card.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:findings.table.severity}}',
              meaning:
                'Critical, Warning o Info, fissato dalla regola. Vedi [Severities](/help/findings#severities). Uno stato diverso da open compare sotto: muted, degraded o resolved. Vedi [States](/help/findings#states).',
            },
            {
              term: '{{ui:findings.table.finding}}',
              meaning: 'Il titolo del finding, poi in grigio cosa controlla la regola.',
            },
            {
              term: '{{ui:findings.table.object}}',
              meaning:
                'L’oggetto del finding: il testo dello statement, oppure tabella, indice, setting, subscription o replication slot per nome; mai un id nudo. La riga grigia sotto indica database, istanza e cluster. Uno statement di cui il server non conserva più il testo mostra “text no longer kept”.',
            },
            {
              term: '{{ui:findings.table.lastSeen}}',
              meaning:
                'L’ultima volta in cui l’advisor ha rilevato il finding, nel fuso orario della console.',
            },
          ],
        },
      ],
    },
    {
      id: 'finding-card',
      title: 'Scheda della segnalazione',
      blocks: [
        {
          kind: 'text',
          text: 'Apri i dettagli di una riga per mostrare la sua finding card: severity, stato e scope, il titolo completo, cosa controlla la regola e il suo id, poi il detail, lo statement, dove è stato trovato, cosa fare, come verificarlo sul database e l’evidence. Un finding muted mostra una riga Reason.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:findings.card.statement}}',
              meaning:
                'Per un finding su una query: il testo completo con evidenziazione SQL, un pulsante di copia, il query id esatto e {{ui:findings.card.openQuery}}, che apre la query in Query performance. Un testo che termina con … è stato tagliato da PostgreSQL a `track_activity_query_size` byte.',
            },
            {
              term: '{{ui:findings.card.affectedObject}}',
              meaning:
                'Per ogni altro finding, l’oggetto a cui si riferisce: una tabella, un indice, un setting, una subscription o un replication slot. {{ui:findings.card.wholeInstance}} quando la regola copre tutta l’istanza.',
            },
            {
              term: '{{ui:findings.card.where}}',
              meaning: 'Database, istanza e cluster, per nome.',
            },
            {
              term: '{{ui:findings.card.firstSeen}}',
              meaning: 'Quando l’advisor ha visto il finding per la prima volta.',
            },
            {
              term: '{{ui:findings.table.lastSeen}}',
              meaning: 'Quando l’advisor l’ha visto l’ultima volta.',
            },
            {
              term: '{{ui:findings.card.whatToDo}}',
              meaning:
                'L’azione suggerita, quando il finding ne ha una. La remediation di ogni regola è elencata più sotto in Advisor rules.',
            },
            {
              term: '{{ui:findings.card.verifyTitle}}',
              meaning:
                'I passi e l’SQL per controllare il finding da soli: il database a cui collegarsi, cosa guardare e uno script con pulsante di copia. Lo script apre una transazione read-only e termina con `ROLLBACK`: legge cataloghi e viste di statistica e non modifica nulla. Legge le statistiche di adesso, non del momento del finding.',
            },
            {
              term: '{{ui:findings.mute.reason}}',
              meaning:
                'Per un finding muted, il motivo indicato quando è stato silenziato. Se non ne è salvato alcuno, la card mostra “{{ui:findings.card.noReason}}”',
            },
            {
              term: '{{ui:findings.card.catalogue}}',
              meaning:
                'Compare {{ui:findings.card.metadataUnavailable}} quando al finding non corrisponde alcuna voce del {{ui:findings.ruleCatalogue.title}}.',
            },
            {
              term: '{{ui:findings.card.evidence}}',
              meaning:
                'I valori letti dalla regola, con le loro unità: tempi, dimensioni, percentuali e date nel fuso orario della console. Passa sopra un’etichetta per vedere la chiave scritta dall’advisor. Compare {{ui:findings.card.noEvidence}} quando la regola non ne riporta.',
            },
          ],
        },
      ],
    },
    {
      id: 'mute-dialog',
      title: 'Finestra Silenzia segnalazione',
      blocks: [
        {
          kind: 'text',
          text: 'Silenziare nasconde un finding dalla vista predefinita fino alla scadenza che scegli. Quando il silenziamento scade e la condizione persiste, il finding torna open.',
        },
        {
          kind: 'steps',
          items: [
            'Scegli {{ui:findings.mute.mute}} sulla riga. Si apre la finestra {{ui:findings.mute.mute}}.',
            'Inserisci il motivo in {{ui:findings.mute.reason}}. {{ui:findings.mute.reasonRequired}}',
            'Scegli la scadenza: {{ui:findings.mute.preset1h}}, {{ui:findings.mute.preset8h}}, {{ui:findings.mute.preset24h}} (il default), {{ui:findings.mute.preset7d}}, oppure {{ui:findings.mute.presetCustom}}.',
            'Per {{ui:findings.mute.presetCustom}}, imposta data e ora in {{ui:findings.mute.until}}. Un silenziamento può durare fino a 90 giorni.',
            'Conferma con {{ui:findings.mute.mute}}. Usa {{ui:findings.mute.unmute}} per togliere un silenziamento.',
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'Un silenziamento nasconde un problema ancora presente. Indica un motivo chiaro e la scadenza più breve possibile.',
        },
      ],
    },
    {
      id: 'rule-catalogue',
      title: '{{ui:findings.ruleCatalogue.title}}',
      blocks: [
        {
          kind: 'text',
          text: 'La tab {{ui:findings.ruleCatalogue.title}} elenca ogni regola dell’advisor con cosa controlla e cosa le serve. Leggila prima di silenziare i finding di una regola, o quando una regola mostra istanze in {{ui:findings.ruleCatalogue.cannotEvaluate}}.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:findings.ruleCatalogue.rule}}',
              meaning: 'La regola, con il suo id e la sua descrizione.',
            },
            {
              term: '{{ui:findings.table.severity}}',
              meaning: 'La severity dei finding della regola.',
            },
            {
              term: '{{ui:findings.ruleCatalogue.scope}}',
              meaning: 'Cosa coprono i finding della regola.',
            },
            {
              term: '{{ui:findings.ruleCatalogue.needs}}',
              meaning: 'Gli input da cui dipende la regola.',
            },
            {
              term: '{{ui:findings.ruleCatalogue.minTier}}',
              meaning:
                'Tier minimo della regola: T0 pg_monitor e pg_control_system(), T1 aggiunge pg_read_all_data, T2 aggiunge pg_signal_backend.',
            },
            {
              term: '{{ui:findings.ruleCatalogue.firingInstances}}',
              meaning: 'Le istanze in cui la regola è attualmente attiva.',
            },
            {
              term: '{{ui:findings.ruleCatalogue.cannotEvaluate}}',
              meaning: 'Le istanze in cui la regola non può essere valutata.',
            },
            {
              term: '{{ui:findings.ruleCatalogue.severityFilter}}',
              meaning: 'Filtra le regole per il loro livello.',
            },
            {
              term: '{{ui:findings.ruleCatalogue.scopeFilter}}',
              meaning: 'Filtra le regole per il loro scope.',
            },
          ],
        },
      ],
    },
    {
      id: 'advisor-rules',
      title: 'Regole dell’advisor',
      blocks: [
        {
          kind: 'text',
          text: 'Le regole sono raggruppate per area. Ogni voce riporta l’id della regola, il suo significato, la soglia dove il codice ne fissa una, la severity, e la remediation che il finding suggerisce.',
        },
      ],
      subsections: [
        {
          id: 'rules-config',
          title: 'Configurazione e memoria',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`config.checkpoints_too_frequent`',
                  meaning:
                    'Più del 20% dei checkpoint è richiesto prima dell’orario previsto, nelle ultime 24 ore. Severity: Warning.',
                  action: 'Aumenta `max_wal_size` dopo aver dimensionato lo storage.',
                },
                {
                  term: '`config.drift`',
                  meaning:
                    'La stessa impostazione ha valori diversi sulle istanze di questo cluster. Severity: Warning.',
                  action:
                    'Allinea le impostazioni condivise, mantenendo i valori voluti per ogni istanza.',
                },
                {
                  term: '`config.effective_cache_size_mismatch`',
                  meaning:
                    '`effective_cache_size` è lontano dalla memoria del server. Soglia: sotto il 25% o sopra il 150% della memoria utilizzabile. Severity: Info.',
                  action: 'Allinea la stima alla memoria utilizzabile e al carico di lavoro.',
                },
                {
                  term: '`config.fsync_off`',
                  meaning:
                    '`fsync` è disattivato: un crash può corrompere i dati. Severity: Critical.',
                  action: 'Attiva `fsync` in produzione per preservare la durabilità.',
                },
                {
                  term: '`config.full_page_writes_off`',
                  meaning:
                    '`full_page_writes` è disattivato: un crash può lasciare pagine parziali e corrompere i dati. Severity: Critical.',
                  action:
                    'Attiva `full_page_writes`, a meno che lo storage garantisca la sostituzione.',
                },
                {
                  term: '`config.maintenance_work_mem_low`',
                  meaning:
                    '`maintenance_work_mem` è sotto 256 MiB su un host con molta memoria. Soglia: più di 8 GiB di memoria e meno di 256 MiB. Severity: Info.',
                  action:
                    'Verifica la memoria di manutenzione per vacuum e per la costruzione degli indici.',
                },
                {
                  term: '`config.max_connections_high`',
                  meaning:
                    '`max_connections` è molto più alto delle connessioni effettivamente in uso. Soglia: sopra 200, con meno del 30% delle connessioni in uso. Severity: Warning.',
                  action:
                    'Usa un pooler, oppure riduci `max_connections` dopo aver verificato il carico di lavoro.',
                },
                {
                  term: '`config.shared_buffers_high`',
                  meaning:
                    '`shared_buffers` è più del 40% della memoria utilizzabile. Severity: Warning.',
                  action: 'Lascia memoria per le connessioni e per il sistema operativo.',
                },
                {
                  term: '`config.shared_buffers_low`',
                  meaning:
                    '`shared_buffers` è piccolo rispetto alla memoria del server. Soglia: sotto il 15% della memoria utilizzabile. Severity: Warning.',
                  action: 'Verifica `shared_buffers` rispetto al carico di lavoro.',
                },
                {
                  term: '`config.track_io_timing_off`',
                  meaning:
                    '`track_io_timing` è disattivato, quindi i tempi di lettura e scrittura non vengono misurati. Severity: Info.',
                  action: 'Attivalo quando serve la diagnosi dell’I/O.',
                },
                {
                  term: '`config.work_mem_low`',
                  meaning:
                    '`work_mem` è piccolo e le statement scrivono file temporanei. Soglia: `work_mem` sotto 4 MiB, mentre la regola `query.temp_bytes_high` ha finding. Severity: Info.',
                  action: 'Verifica `work_mem` con l’uso concorrente della memoria.',
                },
                {
                  term: '`config.work_mem_oversized`',
                  meaning:
                    '`work_mem` per `max_connections` è una quota grande della memoria. Soglia: sopra il 25% della memoria utilizzabile. Severity: Warning.',
                  action: 'Riduci `work_mem` o limita le connessioni per proteggere la memoria.',
                },
                {
                  term: '`conn.idle_share_high`',
                  meaning:
                    'Una quota grande delle connessioni è inattiva. Soglia: più del 70% inattive, con più di 100 connessioni dei client. Severity: Info.',
                  action: 'Usa il pooling e verifica la durata delle connessioni dei client.',
                },
                {
                  term: '`conn.saturation`',
                  meaning:
                    'La maggior parte delle connessioni consentite da `max_connections` è in uso. Soglia: sopra l’80%. Severity: Warning.',
                  action: 'Aggiungi capacità oppure usa un pooler.',
                },
              ],
            },
          ],
        },
        {
          id: 'rules-backup',
          title: 'Backup e archiviazione WAL',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`archive.disabled`',
                  meaning:
                    'L’archiviazione WAL è disattivata, quindi i backup non possono essere ripristinati a un punto nel tempo. Severity: Info.',
                  action: 'Conferma una strategia di backup alternativa.',
                },
                {
                  term: '`archive.failing`',
                  meaning:
                    'L’archiviazione WAL non funziona: i file WAL completati non vengono copiati. Severity: Critical.',
                  action: 'Ripara `archive_command` e verifica la conservazione dei WAL.',
                },
                {
                  term: '`archive.stalled`',
                  meaning:
                    'Nessun file WAL è stato archiviato da molto tempo, anche se il server scrive WAL. Soglia: l’ultimo WAL archiviato è più vecchio di un’ora, oppure di dieci volte `archive_timeout` se è più lungo, a meno che l’archiver non segnali alcun segmento in attesa. Severity: Critical.',
                  action: 'Ripara l’archiviazione prima che la conservazione dei WAL si esaurisca.',
                },
                {
                  term: '`backup.no_basebackup_seen`',
                  meaning:
                    'Per questo cluster non è stato visto nessun base backup. Soglia: l’ultimo base backup visto ha almeno 7 giorni, oppure non se ne è visto nessuno durante il monitoraggio. Severity: Warning.',
                  action:
                    'Definisci e verifica una pianificazione regolare dei base backup. I backup copiati fuori dal protocollo di replica (strumenti a livello di file, snapshot dello storage) non sono visibili qui.',
                },
                {
                  term: '`backup.no_strategy`',
                  meaning:
                    'Non è visibile alcuna strategia di backup: i WAL non vengono archiviati e nessun base backup è stato visto. Soglia: nessun base backup visto negli ultimi 30 giorni, e nessun membro del cluster archivia i WAL. Severity: Critical.',
                  action:
                    'Configura l’archiviazione oppure definisci una strategia di base backup verificata.',
                },
                {
                  term: '`config.wal_keep_size_low`',
                  meaning:
                    '`wal_keep_size` può essere troppo piccolo per conservare i WAL a uno standby che resta indietro. Soglia: uno standby è in ritardo di più di `wal_keep_size` e di più di un segmento WAL. Severity: Warning.',
                  action: 'Aumenta `wal_keep_size` oppure configura uno slot di replica.',
                },
                {
                  term: '`wal.directory_large`',
                  meaning:
                    'La directory WAL è più grande di quanto spiegano le impostazioni WAL. Soglia: `pg_wal` sopra il doppio di `max_wal_size` più `wal_keep_size`. Severity: Warning.',
                  action:
                    'Controlla il WAL trattenuto da ogni slot di replica (`pg_replication_slots`), e rimuovi con `pg_drop_replication_slot()` gli slot che nessun consumer usa. Correggi `archive_command` se i segmenti lo attendono, cosa che `pg_stat_archiver.last_failed_wal` mostra.',
                },
              ],
            },
          ],
        },
        {
          id: 'rules-tables',
          title: 'Tabelle, vacuum e bloat',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`bloat.estimate_blind`',
                  meaning:
                    'La stima dello spazio sprecato non può essere calcolata per alcune tabelle o alcuni indici.',
                  action:
                    'Concedi al ruolo di monitoraggio `SELECT` sulle colonne di queste tabelle e l’accesso alle righe che la row security non nasconde, oppure accetta che non vengano stimate.',
                },
                {
                  term: '`table.autoanalyze_stale`',
                  meaning:
                    'Più del 20% delle righe di una tabella è cambiato dall’ultima analisi. Severity: Info.',
                  action: 'Analizza la tabella e verifica le impostazioni di autoanalyze.',
                },
                {
                  term: '`table.bloat_high`',
                  meaning:
                    'Una tabella occupa molto spazio che una ricostruzione restituirebbe. Soglia: rapporto di bloat sopra il 30% e dimensione sopra 100 MiB. Severity: Warning.',
                  action:
                    'Pianifica la manutenzione dopo aver verificato l’impatto sul carico di lavoro.',
                },
                {
                  term: '`table.dead_tuples_high`',
                  meaning:
                    'Una tabella ha molte righe morte che vacuum non ha ancora rimosso. Soglia: righe morte sopra il 20% delle righe attive, e almeno 10.000 righe morte. Severity: Warning.',
                  action: 'Verifica autovacuum ed esegui vacuum su questa tabella.',
                },
                {
                  term: '`table.never_autovacuumed`',
                  meaning:
                    'Una tabella grande non è mai stata sottoposta a vacuum. Soglia: più di 100.000 righe attive, e nessun timestamp di vacuum o autovacuum. La regola non viene valutata su uno standby. Severity: Warning.',
                  action: 'Verifica la copertura di autovacuum per questa tabella.',
                },
                {
                  term: '`table.seq_scan_heavy`',
                  meaning:
                    'Una tabella grande viene letta in gran parte per intero, non tramite un indice. Soglia: più di 100.000 righe attive, e un tasso di scansioni con indice al massimo il 10% di quello delle sequential scan. È un candidato per un indice mancante, non una certezza. Severity: Info.',
                  action: 'Verifica i predicati delle query e i piani di esecuzione.',
                },
                {
                  term: '`table.wraparound_risk`',
                  meaning:
                    'Una tabella contiene ID di transazione vecchi, quindi serve un vacuum di freezing. Soglia: età di `relfrozenxid` sopra 1.000.000.000. Severity: Critical.',
                  action: 'Esegui subito il vacuum di questa relazione.',
                },
                {
                  term: '`vacuum.disabled`',
                  meaning: 'Il vacuum automatico (autovacuum) è disattivato. Severity: Critical.',
                  action:
                    'Attiva autovacuum per prevenire il bloat di tabelle e ID di transazione.',
                },
                {
                  term: '`vacuum.starvation`',
                  meaning:
                    'Molte tabelle hanno righe morte mentre ogni worker di vacuum è occupato. Soglia: più di 10 tabelle con righe morte sopra il 20% delle loro righe attive, e tutti i worker di autovacuum occupati. Severity: Critical.',
                  action: 'Verifica i worker di autovacuum bloccati o sottodimensionati.',
                },
              ],
            },
          ],
        },
        {
          id: 'rules-indexes',
          title: 'Indici',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`index.bloat_high`',
                  meaning:
                    'Un indice occupa molto spazio che una ricostruzione restituirebbe. Soglia: rapporto di bloat sopra il 40% e dimensione sopra 50 MiB. Severity: Warning.',
                  action:
                    'Pianifica la manutenzione dopo aver verificato l’impatto sul carico di lavoro.',
                },
                {
                  term: '`index.divergence`',
                  meaning:
                    'La stessa tabella ha indici diversi sulle istanze di questo cluster. Severity: Warning.',
                  action:
                    'Allinea le definizioni degli indici dopo aver verificato lo stato della replica.',
                },
                {
                  term: '`index.duplicate`',
                  meaning: 'Due o più indici hanno la stessa definizione. Severity: Warning.',
                  action: 'Elimina il duplicato che non è primario né unico, dopo la verifica.',
                },
                {
                  term: '`index.fk_unindexed`',
                  meaning:
                    'Una chiave esterna non ha un indice sulle sue colonne. Severity: Info sotto 10 MiB di dimensione della tabella, Warning da 10 MiB in su.',
                  action:
                    'Se le righe della tabella referenziata vengono cancellate, o le loro chiavi aggiornate, crea l’indice con `CREATE INDEX CONCURRENTLY`. Se non succede mai, l’indice non serve.',
                },
                {
                  term: '`index.invalid`',
                  meaning:
                    'Un indice è segnato come non valido, quindi le query non lo usano. Severity: Critical.',
                  action: 'Ricostruisci o elimina l’indice non valido.',
                },
                {
                  term: '`index.redundant_prefix`',
                  meaning:
                    'Un indice ripete le colonne iniziali di un altro indice. Severity: Info.',
                  action: 'Verifica se l’indice più corto e non unico può essere rimosso.',
                },
                {
                  term: '`index.unused`',
                  meaning:
                    'Un indice non ha avuto scansioni nello storico osservato. Soglia: nessuna scansione nella finestra predefinita di 7 giorni. Lo valuta solo il primary, e solo gli indici sopra 10 MiB che non sono chiavi primarie né unici. Severity: Warning.',
                  action:
                    'Prima di eliminare questo candidato, conferma l’intento del carico di lavoro, incluse le query servite dagli standby. Vedi [Index analysis](/help/indexes).',
                },
              ],
            },
          ],
        },
        {
          id: 'rules-statements',
          title: 'Statement, transazioni e lock',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`lock.long_wait`',
                  meaning:
                    'Una sessione attende un lock da molto tempo. Soglia: 300 secondi (5 minuti). Severity: Warning.',
                  action:
                    'Apri la pagina Locks e guarda la sessione indicata nel finding: il suo stato e l’età della transazione mostrano se è inattiva in transazione o ancora al lavoro. Se la sua transazione può terminare, chiedi al suo proprietario di chiuderla. Se il lavoro può andare perso, annullalo o terminalo da lì, e la sessione in attesa prosegue.',
                },
                {
                  term: '`query.cache_miss_high`',
                  meaning:
                    'Una statement frequente legge la maggior parte delle sue pagine dal disco, non dalla memoria. Soglia: più di 1.000 chiamate e un rapporto di miss sopra il 30%. Severity: Info.',
                  action:
                    'Verifica la dimensione del working set e l’indicizzazione. Vedi [Query performance](/help/query-performance).',
                },
                {
                  term: '`query.regression`',
                  meaning:
                    'Una statement è più lenta della sua baseline. Soglia: tempo medio almeno tre volte la baseline, e almeno 100 ms. Severity: Critical.',
                  action:
                    'Verifica la regressione del piano della query. Vedi [Query performance](/help/query-performance).',
                },
                {
                  term: '`query.slow_mean`',
                  meaning:
                    'Una statement ha un tempo medio di esecuzione alto. Soglia: media sopra 500 ms, con almeno 10 chiamate. Severity: Warning.',
                  action:
                    'Ottimizza o indicizza la query. Vedi [Slowest queries](/help/slowest-queries).',
                },
                {
                  term: '`query.temp_bytes_high`',
                  meaning:
                    'Una statement scrive molti file temporanei. Soglia: byte temporanei medi per chiamata sopra `work_mem`. Severity: Warning.',
                  action: 'Verifica la query e il valore attuale di `work_mem`.',
                },
                {
                  term: '`query.total_time_share`',
                  meaning:
                    'Una statement occupa una quota grande del tempo totale di esecuzione. Soglia: più del 30% del totale. Severity: Warning.',
                  action:
                    'Ottimizza la query dominante. Vedi [Slowest queries](/help/slowest-queries).',
                },
                {
                  term: '`txn.high_rollback_ratio`',
                  meaning:
                    'Una quota grande delle transazioni termina con un rollback. Soglia: più del 10% delle transazioni annullate. Severity: Info.',
                  action: 'Verifica i fallimenti delle transazioni e il comportamento dei retry.',
                },
                {
                  term: '`txn.idle_in_transaction`',
                  meaning:
                    'Una sessione è rimasta inattiva dentro una transazione aperta per molto tempo. Soglia: 300 secondi (5 minuti). Severity: Warning.',
                  action: 'Chiudi o conferma la transazione inattiva.',
                },
                {
                  term: '`txn.long_running`',
                  meaning:
                    'Una transazione è aperta da molto tempo. Soglia: 900 secondi (15 minuti). Severity: Warning.',
                  action: 'Conferma, annulla o verifica la transazione.',
                },
                {
                  term: '`txn.prepared_orphan`',
                  meaning:
                    'Una transazione preparata è rimasta aperta per molto tempo. Soglia: 3.600 secondi (1 ora). Severity: Critical.',
                  action: 'Risolvi la transazione preparata, così vacuum può procedere.',
                },
                {
                  term: '`txn.wraparound_risk`',
                  meaning:
                    'Il più vecchio ID di transazione di un database sta invecchiando. Soglia: età di 1.000.000.000 di transazioni. Severity: Critical.',
                  action: 'Esegui subito la manutenzione e risolvi lo stallo di vacuum.',
                },
                {
                  term: '`txn.multixact_wraparound_risk`',
                  meaning:
                    'Il più vecchio ID multixact di un database sta invecchiando. Soglia: età di 1.000.000.000 di multixact. Severity: Critical.',
                  action:
                    'Esegui un vacuum di congelamento sulle tabelle con gli ID multixact più vecchi.',
                },
              ],
            },
          ],
        },
        {
          id: 'rules-replication',
          title: 'Replica e standby',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`standby.recovery_conflicts`',
                  meaning:
                    'Le query su uno standby vengono annullate dal replay dei WAL. Severity: Warning.',
                  action:
                    'Aumenta `max_standby_streaming_delay` (e `max_standby_archive_delay`) sullo standby, così le query terminano prima che il replay le annulli, a costo di un ritardo dello standby mentre girano. Oppure rimuovi la causa sul primary.',
                },
                {
                  term: '`standby.replay_paused`',
                  meaning: 'Il replay dei WAL è in pausa su uno standby. Severity: Warning.',
                  action:
                    'Quando il motivo della pausa è cessato, riprendi il replay come superuser: `SELECT pg_wal_replay_resume();`.',
                },
                {
                  term: '`subscription.disabled`',
                  meaning: 'Una subscription di replica logica è disattivata. Severity: Warning.',
                  action:
                    'Scopri perché è stata disattivata (con `disable_on_error`, il log del subscriber indica la modifica che ha fallito), correggi la causa, poi riattivala con `ALTER SUBSCRIPTION … ENABLE`. Se la subscription non serve più, `DROP SUBSCRIPTION` elimina anche il suo slot sul publisher.',
                },
                {
                  term: '`subscription.errors`',
                  meaning: 'Una subscription di replica logica segnala errori. Severity: Warning.',
                  action:
                    'Leggi l’errore nel log del subscriber. La maggior parte degli errori è una riga in conflitto con una già presente sul subscriber, una tabella o una colonna mancante, oppure un permesso che manca al proprietario della subscription.',
                },
                {
                  term: '`subscription.not_receiving`',
                  meaning:
                    'Una subscription attiva non riceve modifiche. Soglia: silenziosa per 5 minuti. Severity: Warning.',
                  action:
                    'Il finding indica uno di due passi. Se nessun apply worker è in esecuzione, leggi il suo errore nel log del subscriber: per una riga in conflitto, correggi la riga, oppure salta la transazione con `ALTER SUBSCRIPTION … SKIP` quando sai che le sue modifiche non servono. Se il publisher è rimasto silenzioso, controlla il walsender per lo slot in `pg_stat_replication` e la rete tra i due. `ALTER SUBSCRIPTION … DISABLE` e poi `ENABLE` riavvia l’apply worker.',
                },
                {
                  term: '`logical_slot.spill_high`',
                  meaning:
                    'Uno slot logico scrive molte modifiche decodificate su disco. Soglia: sopra 1 MiB al secondo. Severity: Info.',
                  action:
                    'Aumenta `logical_decoding_work_mem` (64MB di default), oppure fai in modo che la subscription trasmetta in streaming le transazioni grandi con `ALTER SUBSCRIPTION … SET (streaming = parallel)`.',
                },
              ],
            },
          ],
        },
        {
          id: 'rules-partitions',
          title: 'Partizioni e sequence',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`partition.default_has_rows`',
                  meaning:
                    'La partizione di default contiene righe che non corrispondono a nessun’altra partizione. Severity: Warning.',
                  action:
                    'Crea le partizioni a cui appartengono quelle righe: in un’unica transazione, esegui `ALTER TABLE … DETACH PARTITION` sulla partizione di default, crea le partizioni mancanti, sposta le righe con `INSERT INTO … SELECT` e `DELETE`, poi esegui `ATTACH PARTITION … DEFAULT`.',
                },
                {
                  term: '`partition.future_missing`',
                  meaning:
                    'Una tabella partizionata non ha partizioni pronte per i dati in arrivo. Severity: Warning, oppure Critical quando gli inserimenti già falliscono.',
                  action:
                    'Crea le partizioni successive in anticipo (`CREATE TABLE … PARTITION OF … FOR VALUES FROM (…) TO (…)`) e pianificane la creazione, con pg_partman o con un cron job, così la tabella non resta senza partizioni.',
                },
                {
                  term: '`partition.index_invalid`',
                  meaning: 'Un indice su una tabella partizionata non è valido. Severity: Warning.',
                  action:
                    'Per ogni partizione senza l’indice, esegui `CREATE INDEX CONCURRENTLY` sulla partizione, poi collega il nuovo indice a quello partizionato con `ALTER INDEX … ATTACH PARTITION`. L’indice partizionato diventa valido quando ogni partizione ha il suo indice collegato.',
                },
                {
                  term: '`partition.parent_not_analyzed`',
                  meaning:
                    'Una tabella partizionata non è mai stata analizzata. Soglia: almeno 10.000 righe stimate. Severity: Info.',
                  action:
                    'Esegui `ANALYZE` sulla tabella e di nuovo dopo grandi modifiche ai dati, per esempio da un job pianificato.',
                },
                {
                  term: '`sequence.column_narrower`',
                  meaning:
                    'Una sequence può contare oltre la colonna che memorizza i suoi valori. Severity: Warning.',
                  action:
                    'Allarga la colonna con `ALTER TABLE … ALTER COLUMN … TYPE bigint`, operazione che riscrive la tabella sotto un lock ACCESS EXCLUSIVE. Se la colonna resta com’è, assegna alla sequence il limite della colonna con `ALTER SEQUENCE … AS`.',
                },
                {
                  term: '`sequence.exhaustion_risk`',
                  meaning:
                    'Una sequence è vicina al suo valore massimo. Severity: Warning dal 75% del suo intervallo, Critical dal 90%.',
                  action:
                    'La remediation del finding indica gli statement per questa sequence: vedi la finding card.',
                },
                {
                  term: '`sequence.value_hidden`',
                  meaning:
                    'pglens non può leggere il valore attuale di una sequence. Severity: Info.',
                  action:
                    'Esegui l’agent al tier 1 (`pg_read_all_data`), oppure `GRANT SELECT ON SEQUENCE` al ruolo di monitoraggio per le sequence da controllare. Non concedere mai `USAGE`: consente `nextval()`, che fa avanzare la sequence.',
                },
              ],
            },
          ],
        },
        {
          id: 'rules-security',
          title: 'Sicurezza, funzioni e trigger',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`function.secdef_no_search_path`',
                  meaning:
                    'Una funzione `SECURITY DEFINER` non ha un `search_path` fisso. Severity: Warning, oppure Critical quando il proprietario è un superuser e `PUBLIC` ha `EXECUTE`.',
                  action:
                    'Imposta un `search_path` fisso con `ALTER FUNCTION`, metti `pg_temp` per ultimo e qualifica con lo schema gli oggetti usati dal corpo della funzione. Se `PUBLIC` ha `EXECUTE`, esegui `REVOKE EXECUTE … FROM PUBLIC`, poi concedi `EXECUTE` ai ruoli che ne hanno bisogno.',
                },
                {
                  term: '`function.untracked`',
                  meaning:
                    'Le chiamate di funzione non sono tracciate, quindi le statistiche delle funzioni sono vuote. Severity: Info.',
                  action:
                    "Imposta `track_functions` su `pl` con `ALTER SYSTEM SET track_functions = 'pl'; SELECT pg_reload_conf();`, oppure solo per questo database con `ALTER DATABASE … SET track_functions = 'pl';`. `pl` costa poco; `all` traccia anche le funzioni SQL e C, a un costo più alto, e una funzione SQL che il planner inlinea non viene mai tracciata. pglens non cambia mai l’impostazione.",
                },
                {
                  term: '`security.monitoring_role_can_write`',
                  meaning: 'Il ruolo di monitoraggio può scrivere dati. Severity: Critical.',
                  action:
                    'Esegui di nuovo `deploy/sql/monitoring_user.sql` come amministratore: rifiuta il ruolo ed elenca lo statement che rimuove ogni problema.',
                },
                {
                  term: '`security.monitoring_role_read_only_off`',
                  meaning:
                    'Il ruolo di monitoraggio non usa per default transazioni in sola lettura. Severity: Warning.',
                  action:
                    'Esegui di nuovo `deploy/sql/monitoring_user.sql`: imposta il default del ruolo e rimuove le eccezioni per database.',
                },
                {
                  term: '`trigger.constraint_disabled`',
                  meaning:
                    'I trigger che fanno rispettare un vincolo sono disattivati. Severity: Critical.',
                  action:
                    'Esegui `ALTER TABLE … ENABLE TRIGGER ALL`, poi cerca le righe scritte nel frattempo che violano il vincolo: riattivare i trigger non verifica le righe esistenti.',
                },
                {
                  term: '`trigger.user_disabled`',
                  meaning: 'Un trigger utente è disattivato. Severity: Warning.',
                  action:
                    'Se il trigger deve girare, esegui `ALTER TABLE … ENABLE TRIGGER …`. Se è dismesso, eliminalo, così il suo stato non viene scambiato per un incidente.',
                },
              ],
            },
          ],
        },
        {
          id: 'rules-extensions',
          title: 'Estensioni e PostGIS',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`extension.upgrade_available`',
                  meaning:
                    'Un’estensione installata ha una versione più recente disponibile. Severity: Info.',
                  action:
                    'Leggi le note di rilascio dell’estensione, poi, connesso come suo proprietario, esegui `ALTER EXTENSION … UPDATE`.',
                },
                {
                  term: '`postgis.spatial_unindexed`',
                  meaning:
                    'Una colonna geometrica PostGIS non ha un indice spaziale. Severity: Info fino a 10.000 righe, Warning sopra 10.000 righe.',
                  action:
                    'Se la colonna viene cercata in modo spaziale, crea un indice GiST con `CREATE INDEX CONCURRENTLY … USING gist`.',
                },
                {
                  term: '`postgis.srid_mixed`',
                  meaning:
                    'Le colonne geometriche usano più di un sistema di coordinate. Severity: Info.',
                  action:
                    'Se le colonne vengono confrontate tra loro, memorizzale in un solo SRID, oppure tieni una copia trasformata e indicizzata di quelle che vengono unite.',
                },
                {
                  term: '`postgis.srid_unknown`',
                  meaning:
                    'Una colonna geometrica non ha un sistema di coordinate dichiarato. Severity: Warning.',
                  action:
                    'Trova il sistema di coordinate in cui sono stati rilevati i dati e dichiaralo con `ALTER TABLE … ALTER COLUMN … TYPE … USING ST_SetSRID(column, <srid>)`, oppure aggiungi la definizione mancante a `spatial_ref_sys`.',
                },
                {
                  term: '`postgis.upgrade_available`',
                  meaning:
                    'L’estensione PostGIS è più vecchia della libreria installata. Severity: Warning.',
                  action:
                    'Leggi le note di upgrade di PostGIS, poi, connesso come superuser, esegui `SELECT postgis_extensions_upgrade();`.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'troubleshooting',
      title: 'Viste vuote o inattese',
      blocks: [
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:findings.page.noActive}}',
              meaning:
                'Compare quando l’advisor non ha alcun finding. La descrizione indica “No findings are currently firing” e quante regole dell’advisor sono state valutate con successo.',
              action:
                'Se un problema è atteso, controlla il {{ui:findings.ruleCatalogue.title}} per le regole con istanze in {{ui:findings.ruleCatalogue.cannotEvaluate}}.',
            },
            {
              term: '{{ui:findings.page.noMatchFilters}}',
              meaning:
                'Compare quando esistono finding, ma i filtri correnti non ne mostrano nessuno. La descrizione dice: “Try clearing one or more filters or choose a different state.”',
              action: 'Scegli un altro State, oppure cancella i filtri.',
            },
            {
              term: 'No finding matches “…”',
              meaning:
                'Compare quando la ricerca non corrisponde a nessun finding in nessuno stato. La descrizione elenca i campi cercati. Il pulsante {{ui:findings.page.showAll}} cancella la ricerca.',
            },
            {
              term: 'No finding matches “…”',
              meaning:
                'Compare quando la ricerca corrisponde a finding solo in altri stati, e il filtro State corrente li nasconde. La descrizione indica quanti finding corrispondono in altri stati. Il pulsante {{ui:findings.page.searchAllStates}} imposta State su {{ui:findings.page.allStates}}.',
            },
            {
              term: 'No finding matches “…”',
              meaning:
                'Compare quando la ricerca corrisponde a finding, ma i filtri {{ui:findings.table.severity}}, {{ui:findings.page.scope}}, {{ui:findings.page.cluster}} o {{ui:findings.page.instance}} li nascondono. Il pulsante {{ui:findings.page.clearFilters}} riporta {{ui:findings.page.state}}, {{ui:findings.table.severity}}, {{ui:findings.page.scope}}, {{ui:findings.page.cluster}} e {{ui:findings.page.instance}} ai valori predefiniti.',
              action: 'Scegli {{ui:findings.page.clearFilters}} per ampliare la vista.',
            },
            {
              term: 'Un finding silenziato torna open',
              meaning:
                'Un silenziamento termina alla sua scadenza. Se la condizione persiste, il finding torna open.',
              action: 'Silenzialo di nuovo con un motivo, oppure risolvi il problema.',
            },
          ],
        },
      ],
    },
  ],
}
