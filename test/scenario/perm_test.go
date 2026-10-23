//go:build e2e

package scenario

import (
	"reflect"
	"testing"
)

func TestPerm_ParseCheckCounter(t *testing.T) {
	exposition := `# HELP pglens_check_error_total check scrapes that reported an error, by check name
# TYPE pglens_check_error_total counter
pglens_check_error_total{check="activity"} 0
pglens_check_error_total{check="database_stats"} 3
pglens_check_ok_total{check="activity"} 12
pglens_check_ok_total{check="instance_info"} 4
`
	errs := parseCheckCounter(exposition, "pglens_check_error_total")
	if want := map[string]float64{"activity": 0, "database_stats": 3}; !reflect.DeepEqual(errs, want) {
		t.Fatalf("error counters = %v, want %v", errs, want)
	}
	oks := parseCheckCounter(exposition, "pglens_check_ok_total")
	if want := map[string]float64{"activity": 12, "instance_info": 4}; !reflect.DeepEqual(oks, want) {
		t.Fatalf("ok counters = %v, want %v", oks, want)
	}
}

func TestPerm_CheckCounterGrowth(t *testing.T) {
	before := map[string]float64{"activity": 1, "database_stats": 2}
	after := map[string]float64{"activity": 1, "database_stats": 3, "instance_info": 1}
	if got, want := checkCounterGrowth(before, after), []string{"database_stats", "instance_info"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("grown = %v, want %v", got, want)
	}
	if got := checkCounterGrowth(after, after); len(got) != 0 {
		t.Fatalf("unchanged counters reported growth: %v", got)
	}
	if got := checkCounterGrowth(map[string]float64{}, map[string]float64{"activity": 0}); len(got) != 0 {
		t.Fatalf("a zero sample for a new check reported growth: %v", got)
	}
}
