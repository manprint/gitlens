import type { HelpTopic } from '../../types'

export const mcpTopic: HelpTopic = {
  id: 'mcp',
  group: 'admin',
  title: 'Server MCP',
  summary:
    'Come un assistente AI legge i dati di pglens tramite il Model Context Protocol: attivazione dell’endpoint, credenziale, configurazione del client, tool, limiti e cosa vede l’assistente.',
  page: {
    path: '/settings?tab=audit&view=server',
    label: 'Apri Settings',
  },
  sections: [
    {
      id: 'overview',
      title: 'Che cos’è',
      blocks: [
        {
          kind: 'text',
          text: 'Il server MCP permette a un assistente AI (un client di chat o un agente di sviluppo che parla il Model Context Protocol) di interrogare pglens: quali istanze esistono, su cosa attendevano le sessioni, quali statement erano più lenti, cosa dicono i log. Risponde a `POST /mcp` sul server pglens, con un tool per ogni pagina di dati in sola lettura.',
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'Ogni tool legge soltanto. Nessun tool crea silenziamenti, cambia regole dell’advisor, esegue comandi o modifica impostazioni.',
        },
      ],
    },
    {
      id: 'enable',
      title: 'Attivazione',
      blocks: [
        {
          kind: 'text',
          text: 'L’endpoint è spento per default; finché è spento, `/mcp` risponde 404. L’amministratore del server lo accende nell’ambiente del server.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '`PGLENS_MCP_ENABLED`',
              meaning: '`true` attiva l’endpoint; vuoto o `false` lo lascia spento.',
            },
            {
              term: '`PGLENS_MCP_ALLOWED_ORIGINS`',
              meaning:
                'Origini browser ammesse oltre a quella del server, separate da virgole. I client desktop e da riga di comando non inviano un’origine e non richiedono nulla qui.',
            },
          ],
        },
      ],
    },
    {
      id: 'connect',
      title: 'Collegare un client',
      blocks: [
        {
          kind: 'text',
          text: 'L’URL dell’endpoint è indicato in [{{ui:settings.tabs.audit}}](/help/settings#audit-server). La credenziale è quella della REST API: il token del server, inviato con l’header `Authorization: Bearer <token>`. Senza credenziale l’endpoint risponde 401 e nessun tool viene eseguito.',
        },
        {
          kind: 'steps',
          items: [
            'Chiedi all’amministratore il token del server.',
            'Nell’assistente aggiungi un server di tipo Streamable HTTP con l’URL dell’endpoint, per esempio `https://pglens.example.com/mcp`.',
            'Aggiungi l’header `Authorization` con valore `Bearer <token>`.',
            'Chiedi all’assistente l’elenco delle istanze pglens: chiama per primo `pglens_list_instances`.',
          ],
        },
      ],
    },
    {
      id: 'tools',
      title: 'Tool',
      blocks: [
        {
          kind: 'text',
          text: 'Ogni tool legge gli stessi dati di una pagina della console, tramite la stessa route REST. Gli orari sono in RFC 3339; quasi tutti i tool vogliono l’id di un’istanza preso da `pglens_list_instances`. Il README elenca ogni tool con la sua route.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Inventario',
              meaning:
                '`pglens_list_instances`, `pglens_get_instance`, `pglens_list_clusters`, `pglens_cluster_topology`.',
            },
            {
              term: 'Attività e statement',
              meaning:
                '`pglens_activity`, `pglens_activity_top`, `pglens_activity_compare`, `pglens_top_statements`, `pglens_statements_compare`, `pglens_query_plans`.',
            },
            {
              term: 'Salute',
              meaning:
                '`pglens_list_findings`, `pglens_list_alerts`, `pglens_list_events`, `pglens_locks`, `pglens_logs`, `pglens_autovacuum_runs`, `pglens_instance_host`.',
            },
          ],
        },
      ],
    },
    {
      id: 'limits',
      title: 'Limiti',
      blocks: [
        {
          kind: 'text',
          text: 'Ogni messaggio riceve una sola risposta JSON: niente streaming, niente sessione, niente prompt o resource. Un messaggio è al massimo 1 MiB. Un risultato oltre 262 144 byte non viene inviato; il tool restituisce un errore che chiede un intervallo di tempo più stretto o un limite più basso.',
        },
      ],
    },
    {
      id: 'security',
      title: 'Cosa vede l’assistente',
      blocks: [
        {
          kind: 'note',
          tone: 'warning',
          text: 'L’assistente vede tutto ciò che il token può leggere: nomi e indirizzi delle istanze, testi degli statement, piani, messaggi di log di PostgreSQL, processi dell’host e impostazioni. Collega solo un assistente a cui mostreresti la console e usa HTTPS.',
        },
      ],
    },
  ],
}
