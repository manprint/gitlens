import type { HelpTopic } from '../../types'

export const logsTopic: HelpTopic = {
  id: 'logs',
  group: 'instance',
  title: 'Log di PostgreSQL',
  summary:
    'Le righe rilevanti del log del server PostgreSQL di un’istanza (errori, crash, timeout, deadlock, checkpoint e altro), lette dall’agent, classificate e inviate al server, con le impostazioni che richiedono e ciò che non viene mai inviato.',
  page: {
    path: '/instances/:instanceId/logs',
    label: 'Apri {{ui:shell.nav.logs}}',
  },
  sections: [
    {
      id: 'overview',
      title: 'A cosa serve la pagina',
      blocks: [
        {
          kind: 'text',
          text: '{{ui:shell.nav.logs}} elenca le righe di log che l’agent ha inviato per un’istanza nell’intervallo di tempo, dalla più recente. Ogni riga ha una classe: l’agent scarta le righe ordinarie e invia solo le classi elencate in [Classi](/help/logs#classes).',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:logs.classes.title}}',
              meaning:
                'Il numero di righe di ogni classe nell’intervallo. Fai clic su una classe per filtrare l’elenco, su {{ui:logs.classes.all}} per togliere il filtro.',
            },
            {
              term: '{{ui:logs.events.title}}',
              meaning:
                'Ora, severità, classe, database, utente e messaggio di ogni riga. Espandi una riga per vedere applicazione, PID, SQLSTATE, query ID e i campi detail, hint, context e statement inviati dall’agent. {{ui:logs.row.query}} apre lo statement nell’ispettore delle query quando la riga ha un query ID.',
            },
            {
              term: '{{ui:logs.filter.label}}',
              meaning:
                'Classe, severità, database e un testo che il messaggio deve contenere. I filtri stanno nell’indirizzo, quindi una vista filtrata si può condividere. {{ui:logs.events.older}} va indietro di 100 righe alla volta.',
            },
          ],
        },
      ],
    },
    {
      id: 'setup',
      title: 'Attivare la lettura del log',
      blocks: [
        {
          kind: 'text',
          text: 'La lettura del log è spenta per impostazione predefinita. Legge solo log strutturati: jsonlog (PostgreSQL 15 o successivo) o csvlog. Finché manca un requisito, il report di readiness dell’istanza mostra `RDY-LOG-01` con la correzione.',
        },
        {
          kind: 'steps',
          items: [
            'Sul server attiva il logging collector e una destinazione strutturata, poi riavvia PostgreSQL.',
            'Lascia i messaggi del server in inglese (`lc_messages`): checkpoint, esecuzioni di autovacuum, statement lenti e file temporanei si riconoscono dal testo inglese.',
            'Nella configurazione dell’agent imposta `checks.logs.enabled: true` e scegli la sorgente (vedi [Sorgenti](/help/logs#sources)), poi riavvia l’agent.',
          ],
        },
        {
          kind: 'sql',
          label: 'Impostazioni del server (come superuser)',
          code: "ALTER SYSTEM SET logging_collector = on;\nALTER SYSTEM SET log_destination = 'jsonlog';\nALTER SYSTEM SET lc_messages = 'C';",
        },
      ],
      subsections: [
        {
          id: 'sources',
          title: 'Sorgenti',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`file`',
                  meaning:
                    'L’agent legge `log_directory` sull’host del database. L’utente dell’agent deve poterla leggere.',
                },
                {
                  term: '`sql`',
                  meaning:
                    'L’agent legge il file di log corrente via SQL, da qualsiasi host. Il ruolo di monitoraggio richiede i grant qui sotto.',
                },
              ],
            },
            {
              kind: 'sql',
              label: 'Grant per la sorgente sql',
              code: 'GRANT EXECUTE ON FUNCTION pg_read_binary_file(text, bigint, bigint, boolean) TO <monitoring_role>;\nGRANT EXECUTE ON FUNCTION pg_current_logfile(text) TO <monitoring_role>;',
            },
            {
              kind: 'text',
              text: 'L’agent ricorda fin dove ha letto ogni file, quindi un riavvio non ripete né salta righe; al primo avvio parte dalla fine del file corrente. A ogni intervallo legge al massimo `max_bytes_per_interval` byte e invia al massimo 200 righe.',
            },
          ],
        },
      ],
    },
    {
      id: 'privacy',
      title: 'Cosa non viene mai inviato',
      blocks: [
        {
          kind: 'text',
          text: 'Un messaggio viene tagliato a 2000 byte. I campi detail, hint, context e statement vengono inviati solo con `statement_text: true` nella configurazione dell’agent. Un testo che può contenere una credenziale, come una password, non viene mai inviato.',
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'Leggere il log non modifica mai il server: l’agent legge solo file o chiama funzioni di sola lettura.',
        },
      ],
    },
    {
      id: 'classes',
      title: 'Classi',
      blocks: [
        {
          kind: 'terms',
          items: [
            {
              term: '`panic`, `server.crash`, `corruption`, `disk.full`, `memory.out`',
              meaning:
                'Guasti gravi. Ognuno ha una regola di alert: vedi [Alert dal log](/help/logs#alerts).',
            },
            {
              term: '`server.start`, `server.recovery`',
              meaning: 'Il server è partito, o si è ripreso dopo un arresto non pulito.',
            },
            {
              term: '`lock.deadlock`, `lock.wait`, `lock.timeout`, `serialization.failure`',
              meaning: 'Conflitti di lock, con lo statement quando viene inviato.',
            },
            {
              term: '`statement.timeout`, `statement.canceled`, `idle_tx.timeout`, `slow.statement`',
              meaning:
                'Statement fermati da un timeout o da una cancellazione, sessioni chiuse in una transazione inattiva e statement più lunghi di `log_min_duration_statement`.',
            },
            {
              term: '`auth.failed`, `connection.limit`',
              meaning: 'Login falliti e connessioni rifiutate per mancanza di slot.',
            },
            {
              term: '`checkpoint`, `checkpoint.frequent`, `autovacuum.run`, `temp_file`',
              meaning:
                'Righe di manutenzione; richiedono `log_checkpoints`, `log_autovacuum_min_duration` o `log_temp_files`.',
            },
            {
              term: '`replication.conflict`, `wraparound.warning`',
              meaning:
                'Query cancellate su uno standby e gli avvisi che precedono il wraparound degli ID di transazione.',
            },
            {
              term: '`error.statement`, `fatal.other`, `warning.other`',
              meaning: 'Ogni altra riga ERROR, FATAL o WARNING.',
            },
          ],
        },
      ],
    },
    {
      id: 'alerts',
      title: 'Alert dal log',
      blocks: [
        {
          kind: 'text',
          text: 'Quattro regole di alert scattano alla prima riga corrispondente: `log.panic` e `log.corruption` e `log.disk_full` (critical), `log.out_of_memory` (warning). La loro notifica cita fino a tre delle righe corrispondenti più recenti. Vedi [Regole di alert](/help/alert-rules).',
        },
      ],
    },
  ],
}
