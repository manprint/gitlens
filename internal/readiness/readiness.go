// Package readiness decides whether a monitored server is monitored in full,
// and when it is not, which features the gaps cost and how to close them.
//
// The evaluator is pure (D25): it reads only Input, performs no I/O and reads
// no clock but Input.Now, so the same input always gives the same Report. The
// server assembles Input from the facts and series it already stores.
package readiness

import (
	"sort"
	"strings"
	"time"

	"github.com/manprint/pglens/internal/wire"
)

// Levels of a Report.
const (
	LevelFull    = "full"
	LevelPartial = "partial"
)

// Severities of an Item.
const (
	SeverityWarning = "warning"
	SeverityInfo    = "info"
)

// Channels of a Fix: where the operator makes the change.
const (
	ChannelConsole        = "console"
	ChannelAgentConfig    = "agent_config"
	ChannelSQL            = "sql"
	ChannelPostgresConfig = "postgres_config"
	ChannelHost           = "host"
)

// RoleLiteral stands for the monitoring role in a snippet when its name is
// not known.
const RoleLiteral = "<monitoring_role>"

// Instance is what the server knows about the monitored instance.
type Instance struct {
	ID string
	// PGMajor is the major version (14, 15, ...); 0 when unknown.
	PGMajor   int
	IsStandby bool
	// PermTier is the permission tier the agent runs at: 0 (T0) to 3 (T3);
	// -1 when unknown.
	PermTier       int
	MonitoringRole string
}

// Role is the monitoring role's membership of the two predefined roles that
// show other users' sessions and statements. A nil field is unknown (an older
// agent does not report it).
type Role struct {
	HasPgMonitor, HasReadAllStats *bool
}

// CheckSkip is a check_skip fact: a check the agent does not run, and why.
type CheckSkip struct{ Check, Reason string }

// HostSeries says which host series the instance has in the window.
type HostSeries struct{ HasDiskTotal, HasCPU bool }

// Input is everything the evaluator reads.
type Input struct {
	Instance Instance
	// Settings and SettingSources are the latest `setting` facts: value and
	// pg_settings.source by name. The agent ships a setting only when it
	// differs from its boot value (or is on a curated list), so with
	// SettingsKnown an absent setting is its default; without it nothing is
	// known and the items that depend on a setting are omitted.
	Settings       map[string]string
	SettingSources map[string]string
	SettingsKnown  bool
	// Extensions are the extension names installed per monitored database.
	// ExtensionsKnown says the extensions check reported at all.
	Extensions      map[string][]string
	ExtensionsKnown bool
	Role            Role
	Skips           []CheckSkip
	FailingChecks   []string
	HostSeries      HostSeries
	Flags           wire.AgentFlags
	// DatabasesDiscovered is the connectable databases found, DatabasesBudget
	// the agent's per-target cap; 0 budget means unknown.
	DatabasesDiscovered, DatabasesBudget int
	// NotificationChannels is the configured channel count.
	NotificationChannels int
	Now                  time.Time
}

// Fix is one way to close a gap.
type Fix struct {
	Channel         string   `json:"channel"`
	Title           string   `json:"title"`
	Steps           []string `json:"steps"`
	Snippet         string   `json:"snippet,omitempty"`
	RequiresRestart bool     `json:"requires_restart"`
	Route           string   `json:"route,omitempty"`
}

// Item is one gap.
type Item struct {
	ID       string   `json:"id"`
	Severity string   `json:"severity"`
	Title    string   `json:"title"`
	Summary  string   `json:"summary"`
	Impact   []string `json:"impact"`
	Evidence string   `json:"evidence"`
	Fixes    []Fix    `json:"fixes"`
}

// Counts of a Report.
type Counts struct {
	Warning int `json:"warning"`
	Info    int `json:"info"`
}

// Report is the readiness of one instance.
type Report struct {
	InstanceID  string    `json:"instance_id"`
	Level       string    `json:"level"`
	GeneratedAt time.Time `json:"generated_at"`
	Counts      Counts    `json:"counts"`
	Items       []Item    `json:"items"`
}

// Rule is one check of the table in rules.go.
type Rule struct {
	ID       string
	Severity string
	Applies  func(Input) bool
	Build    func(Input) Item
}

// Evaluate returns the Report of in. Warnings come first, each group sorted
// by rule id; the level is "full" when there is no warning (I-14).
func Evaluate(in Input) Report {
	report := Report{InstanceID: in.Instance.ID, Level: LevelFull, GeneratedAt: in.Now, Items: []Item{}}
	for _, rule := range Rules() {
		if !rule.Applies(in) {
			continue
		}
		item := rule.Build(in)
		item.ID, item.Severity = rule.ID, rule.Severity
		if in.Instance.IsStandby {
			item.Fixes = standbySteps(item.Fixes)
		}
		report.Items = append(report.Items, item)
		if rule.Severity == SeverityWarning {
			report.Counts.Warning++
		} else {
			report.Counts.Info++
		}
	}
	sort.SliceStable(report.Items, func(i, j int) bool {
		a, b := report.Items[i], report.Items[j]
		if a.Severity != b.Severity {
			return a.Severity == SeverityWarning
		}
		return a.ID < b.ID
	})
	if report.Counts.Warning > 0 {
		report.Level = LevelPartial
	}
	return report
}

// standbyStep opens the steps of an SQL fix that a standby cannot take.
const standbyStep = "This server is a standby: run the statements on its primary, they reach the standby by replication."

// standbySteps returns fixes with standbyStep in front of every SQL fix that
// changes the shared catalogue (a grant, an extension). ALTER SYSTEM changes
// one server's own file and stays as it is. fixes is not modified.
func standbySteps(fixes []Fix) []Fix {
	out := make([]Fix, len(fixes))
	for i, f := range fixes {
		if f.Channel == ChannelSQL && !strings.Contains(f.Snippet, "ALTER SYSTEM") {
			f.Steps = append([]string{standbyStep}, f.Steps...)
		}
		out[i] = f
	}
	return out
}
