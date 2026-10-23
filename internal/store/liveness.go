package store

import "time"

// ExpectedPushInterval is how often an agent is expected to push its results.
const ExpectedPushInterval = 30 * time.Second

// InstanceStaleAfter is how long an instance may go without a push before it
// counts as down: three expected pushes. The staleness gauge, the staleness
// events and the agent_down alert rule all read this one value, so they agree
// on when an instance is down.
const InstanceStaleAfter = 3 * ExpectedPushInterval
