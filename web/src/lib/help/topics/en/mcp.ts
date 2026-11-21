import type { HelpTopic } from '../../types'

export const mcpTopic: HelpTopic = {
  id: 'mcp',
  group: 'admin',
  title: 'MCP server',
  summary:
    'How an AI assistant reads pglens data through the Model Context Protocol: turning the endpoint on, the credential, the client configuration, the tools, the limits and what the assistant can see.',
  page: {
    path: '/settings?tab=audit&view=server',
    label: 'Open Settings',
  },
  sections: [
    {
      id: 'overview',
      title: 'What it is',
      blocks: [
        {
          kind: 'text',
          text: 'The MCP server lets an AI assistant (a chat client or a coding agent that speaks the Model Context Protocol) ask pglens questions: which instances exist, what the sessions were waiting on, which statements were slowest, what the logs say. It answers at `POST /mcp` on the pglens server, with one tool per read-only page of data.',
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'Every tool only reads. No tool creates silences, changes advisor rules, runs commands or edits settings.',
        },
      ],
    },
    {
      id: 'enable',
      title: 'Turning it on',
      blocks: [
        {
          kind: 'text',
          text: 'The endpoint is off by default; while it is off, `/mcp` answers 404. The server administrator turns it on in the server environment.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '`PGLENS_MCP_ENABLED`',
              meaning: '`true` serves the endpoint; empty or `false` keeps it off.',
            },
            {
              term: '`PGLENS_MCP_ALLOWED_ORIGINS`',
              meaning:
                'Browser origins allowed to call the endpoint besides the server’s own, separated by commas. Desktop and command-line clients send no origin and need nothing here.',
            },
          ],
        },
      ],
    },
    {
      id: 'connect',
      title: 'Connecting a client',
      blocks: [
        {
          kind: 'text',
          text: 'The endpoint URL is shown in [{{ui:settings.tabs.audit}}](/help/settings#audit-server). The credential is the one of the REST API: the server token, sent as the header `Authorization: Bearer <token>`. Without it the endpoint answers 401 and no tool runs.',
        },
        {
          kind: 'steps',
          items: [
            'Ask the administrator for the server token.',
            'In the assistant, add a server of type Streamable HTTP with the endpoint URL, for example `https://pglens.example.com/mcp`.',
            'Add the header `Authorization` with the value `Bearer <token>`.',
            'Ask the assistant to list the pglens instances: it calls `pglens_list_instances` first.',
          ],
        },
      ],
    },
    {
      id: 'tools',
      title: 'Tools',
      blocks: [
        {
          kind: 'text',
          text: 'Each tool reads the same data as one console page, through the same REST route. Times are RFC 3339; most tools take an instance id from `pglens_list_instances`. The README lists every tool with its route.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Inventory',
              meaning:
                '`pglens_list_instances`, `pglens_get_instance`, `pglens_list_clusters`, `pglens_cluster_topology`.',
            },
            {
              term: 'Activity and statements',
              meaning:
                '`pglens_activity`, `pglens_activity_top`, `pglens_activity_compare`, `pglens_top_statements`, `pglens_statements_compare`, `pglens_query_plans`.',
            },
            {
              term: 'Health',
              meaning:
                '`pglens_list_findings`, `pglens_list_alerts`, `pglens_list_events`, `pglens_locks`, `pglens_logs`, `pglens_autovacuum_runs`, `pglens_instance_host`.',
            },
          ],
        },
      ],
    },
    {
      id: 'limits',
      title: 'Limits',
      blocks: [
        {
          kind: 'text',
          text: 'Each message gets one JSON answer: no streaming, no session, no prompts or resources. A message is at most 1 MiB. A result over 262 144 bytes is not sent; the tool returns an error asking for a narrower time range or a lower limit.',
        },
      ],
    },
    {
      id: 'security',
      title: 'What the assistant sees',
      blocks: [
        {
          kind: 'note',
          tone: 'warning',
          text: 'The assistant sees everything the token can read: instance names and addresses, statement texts, plans, PostgreSQL log messages, host processes and settings. Connect only an assistant you would show the console to, and use HTTPS.',
        },
      ],
    },
  ],
}
