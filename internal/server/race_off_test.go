//go:build !race

package server

// raceEnabled says the tests run under the race detector, which slows them
// several times: time budgets are scaled by it so they keep their meaning.
const raceEnabled = false

// Only the integration tests read it; this keeps the plain build from
// reporting it unused.
var _ = raceEnabled
