//go:build e2e

package scenario

import (
	"reflect"
	"testing"
)

// The server writes one line per instance and check, instance_id first, as
// internal/server/metrics_handler.go does. Parsing a check-first shape found no
// sample at all and made SYS-PERM-001 wait out its poll on every run.
func TestPerm_ParseCheckCounter(t *testing.T) {
	exposition := `# HELP pglens_check_error_total check scrapes that reported an error, by instance and check name
# TYPE pglens_check_error_total counter
pglens_check_error_total{instance_id="11111111-1111-1111-1111-111111111111",check="activity"} 0
pglens_check_error_total{instance_id="11111111-1111-1111-1111-111111111111",check="database_stats"} 3
pglens_check_ok_total{instance_id="11111111-1111-1111-1111-111111111111",check="activity"} 12
pglens_check_ok_total{instance_id="11111111-1111-1111-1111-111111111111",check="instance_info"} 4
pglens_check_ok_total{instance_id="22222222-2222-2222-2222-222222222222",check="activity"} 5
`
	errs := parseCheckCounter(exposition, "pglens_check_error_total")
	if want := map[string]float64{"activity": 0, "database_stats": 3}; !reflect.DeepEqual(errs, want) {
		t.Fatalf("error counters = %v, want %v", errs, want)
	}
	oks := parseCheckCounter(exposition, "pglens_check_ok_total")
	if want := map[string]float64{"activity": 17, "instance_info": 4}; !reflect.DeepEqual(oks, want) {
		t.Fatalf("ok counters = %v, want %v (one total per check across instances)", oks, want)
	}
	if got := parseCheckCounter("# TYPE pglens_check_ok_total counter\n", "pglens_check_ok_total"); len(got) != 0 {
		t.Fatalf("an exposition with no samples produced counters: %v", got)
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
