import type { HelpTopic } from '../../types'

export const fleetTopic: HelpTopic = {
  id: 'fleet',
  group: 'monitoring',
  title: 'Fleet',
  summary:
    'The Fleet page: every monitored PostgreSQL cluster with its health, instances and replication lag. How to read the summary tiles and the cluster cards, what to do with an unhealthy cluster, and why a value can be stale.',
  page: {
    path: '/',
    label: 'Open {{ui:shell.nav.fleet}}',
  },
  sections: [
    {
      id: 'overview',
      title: 'What the page is for',
      blocks: [
        {
          kind: 'text',
          text: '{{ui:shell.nav.fleet}} is the page at `/`, opened from the {{ui:shell.nav.fleet}} entry of the sidebar. It shows every monitored PostgreSQL cluster in one grid: its health, its primary, its instances and the largest replication lag. Use it to see at once which clusters work, which need attention and which agents have stopped reporting. The server builds the grid from the agent reports and the grid refreshes every 15 seconds; firing alerts come from the alert list.',
        },
        {
          kind: 'steps',
          items: [
            'Open {{ui:shell.nav.fleet}} from the sidebar. Read [Agents requiring attention](/help/fleet#agent-health) first: a silent agent makes the values of its cluster stale.',
            'Read the [summary tiles](/help/fleet#summary) for the clusters by health, the instances up and down and the firing alerts.',
            'Narrow the grid with the search field or a tile; see [Page controls](/help/fleet#page-controls).',
            'Open a cluster from its card. The cluster page is explained in [Cluster detail](/help/cluster).',
          ],
        },
      ],
      subsections: [
        {
          id: 'page-controls',
          title: 'Page controls',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:fleet.page.filterLabel}}',
                  meaning:
                    'Search field above the grid. It matches the cluster name, the instance address and the instance display name, and it narrows the grid. Press `/` to jump to it.',
                },
              ],
            },
            {
              kind: 'note',
              tone: 'info',
              text: 'The health filter (`health`) and the search (`q`) are kept in the page address, so a reload or a shared link keeps them. The filters set by the {{ui:fleet.summary.instancesUp}}, {{ui:fleet.summary.instancesDown}} and {{ui:fleet.summary.firingAlerts}} tiles are not: a reload clears them.',
            },
          ],
        },
      ],
    },
    {
      id: 'agent-health',
      title: 'Agents requiring attention',
      blocks: [
        {
          kind: 'text',
          text: 'This strip appears above the grid only when at least one instance needs attention. It lists each instance whose agent is down, has been silent for 45 seconds or more, or has a firing `agent_down` or `instance_unreachable` alert. The cluster values next to it may be stale until the agent reports again.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Reported reason',
              meaning:
                'The cause given by the agent or by the firing alert, in plain words: the `cause:` or `reason:` prefix is removed and underscores become spaces. Without a cause the console says: {{ui:fleet.health.notReported}}',
              action:
                'Check the agent logs on that host, the agent container, the network path to the server and the server itself. The same checks apply to an {{ui:settings.inventory.colAgent}} down or {{ui:alerts.column.instance}} unreachable alert.',
            },
            {
              term: '{{ui:findings.table.lastSeen}}',
              meaning:
                'Time since the instance last reported, shown as a duration. Past 45 seconds the instance is listed here; past 60 seconds the server counts it as down.',
              action:
                'If the value keeps growing, the agent is not reporting: follow the steps in Reported reason.',
            },
            {
              term: 'Instance link',
              meaning:
                'The instance name opens its instance page. The cluster name next to it is the cluster the instance belongs to.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'This strip flags an instance silent for 45 seconds, but the cluster health counts an instance as down only after 60 seconds without a report. In between, the instance is listed here while its cluster may still read ok.',
        },
      ],
    },
    {
      id: 'summary',
      title: 'Summary tiles',
      blocks: [
        {
          kind: 'text',
          text: 'The row of tiles above the grid counts clusters and instances and shows the firing alerts. Selecting a tile filters the grid to the clusters it refers to; the active tile is shown as pressed, and selecting it again clears the filter. The values refresh every 15 seconds.',
        },
      ],
      subsections: [
        {
          id: 'health-tiles',
          title: 'Health tiles',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:alerts.events.allClusters}}',
                  meaning:
                    'Number of monitored clusters, whatever their health. Selecting it clears the health filter.',
                },
                {
                  term: '{{ui:fleet.summary.healthy}}',
                  meaning:
                    'Clusters with health `ok`: exactly one primary, every instance up and a largest standby replay lag of 30 seconds or less. A cluster with no lag reading is not penalised. Selecting it lists only these clusters.',
                },
                {
                  term: 'Degraded',
                  meaning:
                    'Clusters with exactly one primary where an instance is down (no report for 60 seconds) or the largest standby replay lag is above 30 seconds. Selecting it lists only these clusters.',
                  action:
                    'Open the card to see which instance is down or how large the lag is. See [What to do with an unhealthy cluster](/help/fleet#unhealthy-cluster).',
                },
                {
                  term: 'Critical',
                  meaning:
                    'Clusters that report no primary, or more than one primary (split brain). Only the number of primaries counts here, not the lag.',
                  action:
                    'Open the cluster and count the primaries on the {{ui:cluster.tabs.topology}} tab; see [Cluster detail](/help/cluster).',
                },
              ],
            },
          ],
        },
        {
          id: 'instance-tiles',
          title: 'Instance tiles',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:fleet.summary.instancesUp}}',
                  meaning:
                    'Instances that reported in the last 60 seconds, counted over all clusters. Selecting it lists the clusters with at least one of them.',
                },
                {
                  term: '{{ui:fleet.summary.instancesDown}}',
                  meaning:
                    'Instances that did not report in the last 60 seconds, counted over all clusters. Selecting it lists the clusters with at least one of them. An instance replaced by a newer one at the same address and port stays counted as down here, but the cluster health ignores it.',
                  action:
                    'Check the agent of each listed instance; see [Agents requiring attention](/help/fleet#agent-health). An instance replaced by a newer one needs no action.',
                },
              ],
            },
          ],
        },
        {
          id: 'alert-tile',
          title: 'Firing alerts tile',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:fleet.summary.firingAlerts}}',
                  meaning:
                    'Alerts firing now across the fleet, counted from the alert list. Selecting it keeps the clusters with a firing alert, matched by cluster or by instance. When the list was cut, a notice appears and the number is a lower bound. When the alert list cannot be loaded, the tile shows Unavailable and cannot be selected.',
                  action:
                    'Open the [{{ui:shell.nav.alerts}}](/alerts) page to see the rule and the state of each alert.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'cluster-cards',
      title: 'Cluster cards',
      blocks: [
        {
          kind: 'text',
          text: 'One card per cluster, below the tiles. Critical cards come first, then degraded, then ok; cards with the same health are sorted by name. The title is a link, and the whole card opens the cluster detail. The health badge is the first element of each card; the values and badges follow.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'ok',
              meaning:
                'Healthy: a single primary, every instance up and replay lag within 30 seconds. Same rule as the Healthy tile.',
            },
            {
              term: 'degraded',
              meaning:
                'One instance is down, or the largest standby replay lag is above 30 seconds. Open the cluster to see which.',
              action: 'See [What to do with an unhealthy cluster](/help/fleet#unhealthy-cluster).',
            },
            {
              term: 'critical',
              meaning: 'No primary, or more than one primary (split brain).',
              action: 'See [What to do with an unhealthy cluster](/help/fleet#unhealthy-cluster).',
            },
          ],
        },
      ],
      subsections: [
        {
          id: 'card-values',
          title: 'Card values',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:fleet.card.primary}}',
                  meaning:
                    'Name of the instance reported as primary, with its address on hover. A dash (—) whose hover text says No primary address is available. means no primary address is known.',
                  action:
                    'With no primary the cluster is critical; see [What to do with an unhealthy cluster](/help/fleet#unhealthy-cluster).',
                },
                {
                  term: '{{ui:fleet.card.instances}}',
                  meaning: 'Number of instances in the cluster.',
                },
                {
                  term: 'Standbys',
                  meaning:
                    'Number of standby instances: read-only copies that receive changes from a primary. Shown as 0 when no count is reported.',
                },
                {
                  term: '{{ui:fleet.card.maxReplayLag}}',
                  meaning:
                    'Largest replay lag, in seconds, that any standby reported in the last 60 seconds. A dash (—) whose hover text says No standby has reported replay lag. means no standby reported one. Above 30 seconds the cluster is degraded.',
                  action:
                    'Open the cluster and read the {{ui:cluster.tabs.lag}} tab to see which standby is behind and whether the lag keeps growing; see [Cluster detail](/help/cluster).',
                },
                {
                  term: 'Permission tiers',
                  meaning:
                    'Permission tier the agent holds on each instance, listed once per cluster. T0 reads statistics, T1 also runs EXPLAIN for query plans, T2 also cancels or ends sessions. A higher tier includes the lower ones. A dash (—) means no tier has been reported.',
                  action:
                    'A feature that needs a higher tier is shown as not permitted. Grant the tier with `deploy/sql/monitoring_user.sql`.',
                },
              ],
            },
          ],
        },
        {
          id: 'card-badges',
          title: 'Card badges',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'Identity source',
                  meaning:
                    'Shown when the identity of the cluster does not come from the PostgreSQL system identifier (`system_identifier`). The value after it names the source. A link under the card leads to the monitoring role grant instructions.',
                  action:
                    'Follow the link under the card. For a `cluster_identity_conflict` event, apply `monitoring_user.sql`, then align `cluster_name`.',
                },
                {
                  term: 'N firing alert(s)',
                  meaning:
                    'Number of firing alerts that match this cluster, by cluster or by instance. Shown only when the number is above zero.',
                  action: 'Open [{{ui:shell.nav.alerts}}](/alerts) and check each firing alert.',
                },
                {
                  term: '{{ui:fleet.card.alertsUnavailable}}',
                  meaning:
                    'The alert list could not be loaded. The card shows no count, so a missing feed never reads as zero alerts.',
                  action: 'Use {{ui:fleet.page.retryAlerts}} in the banner above the grid.',
                },
                {
                  term: '{{ui:fleet.card.readiness}}',
                  meaning:
                    'Whether the agent can monitor every server of the cluster in full. `Full`: no warnings. `Partial · N warning(s)`: at least one server has a warning. The card shows no badge when the readiness is unknown.',
                  action:
                    'Click Fix to open the {{ui:fleet.card.readiness}} tab of the instance with the most warnings, where each missing item and its fix are listed.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'unhealthy-cluster',
      title: 'What to do with an unhealthy cluster',
      blocks: [
        {
          kind: 'text',
          text: 'Start from the health of the card. Critical means the number of primaries is not one; degraded means an instance is down or a standby lags more than 30 seconds. Check the cause in this order: the primaries, then the instances, then the lag.',
        },
        {
          kind: 'steps',
          items: [
            'Open the cluster from its card; see [Cluster detail](/help/cluster).',
            'Critical: on the {{ui:cluster.tabs.topology}} tab, count the reported primaries. Zero or more than one needs immediate attention.',
            'Instance down: find it under [Agents requiring attention](/help/fleet#agent-health) and check its agent.',
            'Lag above 30 seconds: open the {{ui:cluster.tabs.lag}} tab and check whether the lag keeps growing.',
            'Check the {{ui:shell.nav.events}} tab for a recent failover, and the [{{ui:shell.nav.events}}](/events) page for every other event type.',
          ],
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:fleet.card.primary}} —',
              meaning:
                'No primary is reported, so the cluster is critical. The event `no_primary_in_cluster` describes the same condition.',
              action: 'Find out why no instance is the primary, and promote a standby if needed.',
            },
            {
              term: 'More than one primary',
              meaning:
                'More than one instance reports the primary role: split brain, critical. The event `split_brain_detected` describes the same condition.',
              action: 'Identify the authoritative primary and demote the other one.',
            },
            {
              term: '{{ui:alerts.column.instance}} down',
              meaning: 'An instance has not reported for 60 seconds, so the cluster is degraded.',
              action:
                'Check the agent logs, the agent container, the network reachability and the server.',
            },
            {
              term: 'Standby lag above 30 seconds',
              meaning:
                'The largest standby replay lag is above 30 seconds, so the cluster is degraded. The alert rule `replica.lag_high` describes the same condition.',
              action:
                'Open the {{ui:cluster.tabs.lag}} tab of the cluster and check whether the lag keeps growing; see [Cluster detail](/help/cluster).',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'After a failover the new primary takes the role. The cluster reads ok again when exactly one primary remains, every instance is up and the lag is within 30 seconds. The failover stays listed in the {{ui:shell.nav.events}} tab of the cluster.',
        },
      ],
    },
    {
      id: 'freshness',
      title: 'Freshness and stale data',
      blocks: [
        {
          kind: 'text',
          text: 'The grid refreshes every 15 seconds. The page badge shows the age of the data on screen after the word Updated, and turns to Stale when that age exceeds 45 seconds. A card marks its health badge and its {{ui:fleet.card.maxReplayLag}} as Stale when an instance of the cluster has not reported for 45 seconds or more.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Updated … ago',
              meaning:
                'Age of the data on screen. It drops back at each refresh, which runs every 15 seconds.',
              action:
                'If the age keeps growing past 45 seconds, check that the server is reachable and reload the page.',
            },
            {
              term: 'Stale',
              meaning:
                'Shown on the page badge, or on a card badge and its {{ui:fleet.card.maxReplayLag}}, when the data is older than 45 seconds. The text names the age and the threshold. The value may no longer be current.',
              action:
                'Check the agent of the instance listed under [Agents requiring attention](/help/fleet#agent-health); the values come back once it reports again.',
            },
          ],
        },
      ],
    },
    {
      id: 'troubleshooting',
      title: 'Troubleshooting',
      blocks: [
        {
          kind: 'terms',
          items: [
            {
              term: 'No monitored clusters yet',
              meaning:
                'The grid is empty because no cluster has reported yet. The message reads: Set up the pglens agent on a PostgreSQL host to add your first cluster.',
              action:
                "Install and start the agent on a PostgreSQL host. The cluster appears after the agent's first report.",
            },
            {
              term: 'No clusters match these filters',
              meaning:
                'The search or a tile filter hides every cluster. The message suggests clearing the search or changing the active filters.',
              action:
                'Clear the search field, or select {{ui:alerts.events.allClusters}} to clear the health filter. Select the active {{ui:fleet.summary.instancesUp}}, {{ui:fleet.summary.instancesDown}} or {{ui:fleet.summary.firingAlerts}} tile again to clear it.',
            },
            {
              term: 'Alert counts unavailable.',
              meaning:
                'The firing alerts could not be loaded, but the cluster grid stays available. The alert tile reads Unavailable.',
              action:
                'Select {{ui:fleet.page.retryAlerts}} in the banner. Until the alerts load, the cards show {{ui:fleet.card.alertsUnavailable}}.',
            },
            {
              term: 'Error on the cluster list',
              meaning:
                'The cluster list could not be loaded. The error message names the endpoint `clusters` and offers a retry.',
              action:
                'Use the retry action of the message. If it keeps failing, check the server and its logs.',
            },
          ],
        },
      ],
    },
  ],
}
