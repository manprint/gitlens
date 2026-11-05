import type { CaptionEntry } from '../captions'

/** Captions of the settings route group (D30); rules in the plan, phase 17. */
export const settingsCaptions = {
  'settings.server-info': {
    what: 'The build of this console interface, the contract version it was built for, and when your sign-in session expires.',
    source:
      'The build name and the version line of the OpenAPI document are fixed when the interface is built (PGLENS_VERSION, or dev); the expiry is expires_at of the server session endpoint.',
    window: 'Build values are fixed for this interface; the expiry is the current session.',
    howToRead: 'Sessions are held in server memory and end at a server restart.',
  },
  'settings.product-limits': {
    what: 'The supported scale and the known limits of this release: retention, PostgreSQL versions, replication, databases per instance, estimates and sampling.',
    source:
      'Fixed text of this page; most rows follow the limits document of the repository, while the replication and databases-per-instance rows are not in it. The live retention of each metrics family is read on the Data retention tab.',
    window:
      'This release; retention defaults are set on the server, the database budget in the agent configuration.',
    howToRead:
      'Raw retention links to the Data retention tab; the other rows link to the limits document. Bloat figures are estimates and active session history is a sample, not a full trace.',
  },
  'settings.authentication': {
    what: 'How the console and the agent authenticate: one shared password, no user accounts or roles, and a separate agent token.',
    source:
      'The server session store (kept in memory) and the shared password and agent token set in the server environment (PGLENS_UI_PASSWORD, PGLENS_BOOTSTRAP_TOKEN).',
    window: 'The current server configuration.',
    howToRead:
      'Everyone who knows the password has the same access. There is no enrollment queue or browser revocation control, and sessions end at a server restart.',
  },
  'settings.references': {
    what: 'Links to the interface reference and to the alert rule and silence pages.',
    source: 'Fixed links of this page: /docs/api.md, /alerts/rules and /alerts/silences.',
    window: 'Static links; they open the current documents and pages.',
  },
  'settings.command-audit': {
    what: 'Every command asked of an agent and its result, per monitored instance, with the credential that requested it, not the person.',
    source:
      'Server command-audit endpoint over the command_audit table, which is append-only; one list per monitored instance.',
    unit: 'audit entries',
    window: 'The newest 100 entries of each instance.',
    howToRead:
      'Requested by reads api-token, or ui-session with a short hash of the session, then from and the client address; pglens has no user identity, accounts or roles.',
  },
  'settings.command-audit-instance': {
    what: 'Commands asked of this instance, newest first: what was run, who asked, its state, and when it was requested, claimed or expired.',
    source:
      'Server command-audit endpoint for this instance, rows of command_audit (kind, args, requested_by, executed_at, outcome, detail); the filters and search run in the browser.',
    unit: 'audit entries',
    window: 'The newest 100 entries; times are UTC.',
    howToRead:
      'Each entry is one event: the request, its result or its expiry. State is requested, ok, error, rejected or expired. A request the agent never claimed is shown as expired when it ran out in the queue.',
  },
  'settings.command-audit-table': {
    what: 'One row per audit entry of this instance; the search and the State and Command filters narrow the rows.',
    source:
      'Rows of the command_audit table through the server command-audit endpoint for this instance; the search and filters run in the browser.',
    unit: 'audit entries',
    window: 'The newest 100 entries; the Time column is UTC.',
    howToRead:
      'Time is when the entry was recorded. Requested by is the credential that asked, not a person. Timing and detail adds when it was requested, claimed or expires, and the result detail.',
  },
  'settings.inventory-instances': {
    what: 'Every monitored PostgreSQL instance with its cluster, role, version, permission tier, last report time, state and host metrics availability.',
    source:
      'Server instances endpoint over the instances table, with the cluster names from the cluster list; host metrics from the host endpoint of each instance.',
    unit: 'instances',
    window: 'Latest report of each instance.',
    howToRead:
      'Up means the instance reported within the last 60 seconds. Not assigned means no cluster owns the instance yet.',
  },
  'settings.inventory-instances-table': {
    what: 'One row per monitored instance; the search and the Role and State filters narrow it, and the search also narrows the Databases view.',
    source:
      'Rows of the instances table through the server instances endpoint; the search and filters run in the browser.',
    unit: 'instances',
    window: 'Latest report of each instance.',
    howToRead:
      'Permission tier is T0 (reads statistics only), T1 (also runs EXPLAIN) or T2 (also cancels or ends sessions). Last seen is a UTC time. Host metrics says whether the agent on the same host sends operating system figures: Available, Available (stale) when the last sample is over 90 seconds old, or Unavailable with the reason on hover.',
  },
  'settings.permission-tiers': {
    what: 'Each instance has a permission tier: T0 reads statistics only, T1 also runs EXPLAIN for query plans, T2 also cancels or ends other sessions.',
    source:
      'Instances counted by perm_tier from the instances table; the agent sets the tier from its monitoring role (member of pg_read_all_data: T1, of pg_signal_backend: T2); rules from the live advisor rule catalogue by minimum tier; the actions are fixed text of this page.',
    unit: 'instances and rules',
    window: 'Current instances and the live advisor rule catalogue.',
    howToRead:
      'Each card counts the instances in that tier and lists what the tier unlocks. A tier unlocks its own rules and those of the lower tiers. A tier only allows an action when the target also permits it; signals need allow_signal on the target.',
  },
  'settings.inventory-agents': {
    what: 'One derived row per instance saying whether its agent is reporting; the server keeps no separate agent list.',
    source:
      'The instances table (last_seen, with up computed from it) combined with the firing agent_down and instance_unreachable alerts from the server alerts endpoint.',
    unit: 'agents',
    window: 'Latest report of each instance; alerts firing now.',
    howToRead:
      'Down means the instance has not reported within the last 60 seconds, or an agent-down or instance-unreachable alert is open for it. An instance can read Up on the Instances view while such an alert is still open: the alert closes when the alert rules next run. Reason says which case applies.',
  },
  'settings.inventory-agents-table': {
    what: 'One row per instance: a derived agent name, the instance address, last report time, and whether the agent is reporting.',
    source:
      'Built in the browser from the instances list and the firing alerts; the agent name is agent- plus the instance ID.',
    unit: 'agents',
    window: 'Latest report of each instance; times are UTC.',
    howToRead: 'State is Reporting or Down. The search matches the agent, address and reason.',
  },
  'settings.inventory-databases': {
    what: 'The databases of each monitored instance, unmonitored ones first, with the reason the agent left them out.',
    source:
      'Server instance databases endpoint over the databases table, written from each agent inventory; one section per instance of the Instances search.',
    unit: 'databases',
    window: 'Latest agent inventory of each instance.',
    howToRead:
      'Unmonitored databases come first because their skip reason needs attention. The Instances search narrows which instances are listed here.',
  },
  'settings.inventory-databases-instance': {
    what: 'Databases of this instance reported by its agent; Monitored says whether pglens collects their metrics and Skip reason says why not.',
    source:
      'Server instance databases endpoint over the databases table (datname, monitored, skip_reason); the count of unmonitored databases comes from the same response.',
    unit: 'databases',
    window: 'Latest agent inventory of this instance.',
    howToRead:
      'The agent keeps only the most active databases up to its budget, 10 by default; the rest are skipped with db_budget. The number of unmonitored databases is above the table.',
  },
  'settings.inventory-databases-table': {
    what: 'One row per database of this instance: its name, whether it is monitored, and the skip reason when it is not.',
    source:
      'Rows of the databases table through the server instance databases endpoint; the search and the Monitored filter run in the browser.',
    unit: 'databases',
    window: 'Latest agent inventory of this instance.',
    howToRead:
      'A dash in Skip reason means the database is monitored. db_budget means the agent reached its database budget.',
  },
} as const satisfies Record<string, CaptionEntry>
