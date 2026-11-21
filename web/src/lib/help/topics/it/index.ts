import type { HelpTopic } from '../../types'
import { gettingStartedTopic } from './getting-started'
import { fleetTopic } from './fleet'
import { clusterTopic } from './cluster'
import { slowestQueriesTopic } from './slowest-queries'
import { indexesTopic } from './indexes'
import { findingsTopic } from './findings'
import { instanceOverviewTopic } from './instance-overview'
import { queryPerformanceTopic } from './query-performance'
import { waitEventsTopic } from './wait-events'
import { locksTopic } from './locks'
import { alertsTopic } from './alerts'
import { eventsTopic } from './events'
import { alertRulesTopic } from './alert-rules'
import { silencesTopic } from './silences'
import { settingsTopic } from './settings'

// One import per chapter (not `import.meta.glob`: Vitest's module cache keeps a stale
// glob when a file is added). HELP-008 checks that every catalog entry is here.
export const topics: readonly HelpTopic[] = [
  gettingStartedTopic,
  fleetTopic,
  clusterTopic,
  slowestQueriesTopic,
  indexesTopic,
  findingsTopic,
  instanceOverviewTopic,
  queryPerformanceTopic,
  waitEventsTopic,
  locksTopic,
  alertsTopic,
  eventsTopic,
  alertRulesTopic,
  silencesTopic,
  settingsTopic,
]
