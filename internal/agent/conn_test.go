package agent

import (
	"regexp"
	"testing"
)

func TestManager_AddrPort_ParsedFromDSN(t *testing.T) {
	m := &Manager{target: &poolHolder{dsn: "postgres://user:pass@dbhost:6543/postgres?sslmode=disable"}}
	if got := m.Addr(); got != "dbhost" {
		t.Errorf("Addr() = %q, want %q", got, "dbhost")
	}
	if got := m.Port(); got != 6543 {
		t.Errorf("Port() = %d, want 6543", got)
	}
}

func TestManager_AddrPort_InvalidDSN(t *testing.T) {
	m := &Manager{target: &poolHolder{dsn: "not a dsn"}}
	if got := m.Addr(); got != "" {
		t.Errorf("Addr() = %q, want empty string on parse error", got)
	}
	if got := m.Port(); got != 0 {
		t.Errorf("Port() = %d, want 0 on parse error", got)
	}
}

func TestSelection_ExcludeDefaults(t *testing.T) {
	dbs := []dbRow{
		{"postgres", 1000},
		{"template0", 0},
		{"template1", 0},
		{"template2", 0},
		{"rdsadmin", 100},
		{"azure_sys", 50},
		{"app", 2000},
	}
	opts := DefaultConnOptions()
	result := selectDatabases(dbs, opts)

	monitored := map[string]bool{}
	for _, db := range result {
		monitored[db.Name] = db.Monitored
	}

	if monitored["template0"] || monitored["template1"] || monitored["template2"] || monitored["rdsadmin"] || monitored["azure_sys"] {
		t.Error("expected templates/rdsadmin/azure_* to be excluded")
	}
	if !monitored["postgres"] || !monitored["app"] {
		t.Error("expected postgres and app to be monitored")
	}
}

func TestSelection_IncludeFilters(t *testing.T) {
	dbs := []dbRow{
		{"app_prod", 1000},
		{"app_dev", 500},
		{"db_other", 800},
		{"test_app", 200},
	}
	opts := DefaultConnOptions()
	opts.Include = []*regexp.Regexp{regexp.MustCompile(`^app_`)}
	result := selectDatabases(dbs, opts)

	if len(result) != 2 {
		t.Errorf("expected 2 databases, got %d", len(result))
	}
	for _, db := range result {
		if !regexp.MustCompile(`^app_`).MatchString(db.Name) {
			t.Errorf("expected only app_* databases, got %s", db.Name)
		}
	}
}

func TestSelection_BudgetKeepsMostActive(t *testing.T) {
	dbs := []dbRow{
		{"db01", 100},
		{"db02", 200},
		{"db03", 150},
		{"db04", 300},
		{"db05", 50},
		{"db06", 250},
		{"db07", 175},
		{"db08", 75},
		{"db09", 125},
		{"db10", 225},
		{"db11", 160},
		{"db12", 110},
		{"db13", 80},
		{"db14", 190},
		{"db15", 220},
	}
	opts := DefaultConnOptions()
	opts.MaxDatabases = 10
	result := selectDatabases(dbs, opts)

	monitored := 0
	unmonitored := 0
	for _, db := range result {
		if db.Monitored {
			monitored++
		} else {
			if db.SkipReason != "db_budget" {
				t.Errorf("expected skip_reason=db_budget for %s, got %s", db.Name, db.SkipReason)
			}
			unmonitored++
		}
	}
	if monitored != 10 || unmonitored != 5 {
		t.Errorf("expected 10 monitored and 5 unmonitored, got %d and %d", monitored, unmonitored)
	}
}

func TestSelection_Deterministic(t *testing.T) {
	dbs := []dbRow{
		{"db_a", 100},
		{"db_b", 100},
		{"db_c", 100},
	}
	opts := DefaultConnOptions()
	opts.Exclude = nil
	opts.Include = nil

	result1 := selectDatabases(dbs, opts)
	result2 := selectDatabases(dbs, opts)

	order1 := []string{}
	order2 := []string{}
	for _, db := range result1 {
		order1 = append(order1, db.Name)
	}
	for _, db := range result2 {
		order2 = append(order2, db.Name)
	}

	if len(order1) != len(order2) {
		t.Error("orders have different lengths")
	}
	for i := range order1 {
		if order1[i] != order2[i] {
			t.Errorf("non-deterministic: run 1 had %v, run 2 had %v", order1, order2)
		}
	}
}
