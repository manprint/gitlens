package server

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var cmpNow = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

func cmpQuery(aFrom, aTo, bFrom, bTo string) url.Values {
	q := url.Values{}
	for k, v := range map[string]string{"a_from": aFrom, "a_to": aTo, "b_from": bFrom, "b_to": bTo} {
		if v != "" {
			q.Set(k, v)
		}
	}
	return q
}

// CMP-001: a reversed range, a 32-day period, a missing parameter and a
// period in the future are each refused naming the field; overlapping
// periods are accepted.
func TestCMP001(t *testing.T) {
	const (
		h10 = "2026-11-21T10:00:00Z"
		h11 = "2026-11-21T11:00:00Z"
		h12 = "2026-11-21T12:00:00Z"
	)
	for name, tc := range map[string]struct {
		q     url.Values
		field string
	}{
		"reversed a":     {cmpQuery(h11, h10, h10, h11), "a_to"},
		"empty b":        {cmpQuery(h10, h11, h11, h11), "b_to"},
		"32 days":        {cmpQuery("2026-10-20T10:00:00Z", h10, h10, h11), "a_to"},
		"missing a_from": {cmpQuery("", h11, h10, h11), "a_from"},
		"missing b_to":   {cmpQuery(h10, h11, h10, ""), "b_to"},
		"bad time":       {cmpQuery(h10, h11, "yesterday", h11), "b_from"},
		"future b":       {cmpQuery(h10, h11, h11, "2026-11-21T12:02:00Z"), "b_to"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := parseComparePeriods(tc.q, cmpNow)
			var pe *compareParamError
			require.ErrorAs(t, err, &pe)
			require.Equal(t, tc.field, pe.field)
		})
	}

	a, b, err := parseComparePeriods(cmpQuery(h10, h12, h11, "2026-11-21T12:00:30Z"), cmpNow)
	require.NoError(t, err)
	require.Equal(t, 2*time.Hour, a.To.Sub(a.From))
	require.Equal(t, time.Hour+30*time.Second, b.To.Sub(b.From), "a period may end less than a minute ahead")
	_, _, err = parseComparePeriods(cmpQuery("2026-10-21T10:00:00Z", "2026-11-21T10:00:00Z", h10, h11), cmpNow)
	require.NoError(t, err, "31 days is allowed")

	past10, past11 := "2026-01-10T10:00:00Z", "2026-01-10T11:00:00Z"
	api := NewAPI(nil)
	path := "/api/v1/statements/compare?instance_id=" + uuid.NewString() + "&a_from=" + past11 + "&a_to=" + past10 + "&b_from=" + past10 + "&b_to=" + past11
	requireAPIError(t, retentionRequest(t, api, http.MethodGet, path, ""), http.StatusBadRequest, "invalid a_to")
	path = "/api/v1/statements/compare?instance_id=" + uuid.NewString() + "&a_from=" + past10 + "&a_to=" + past11 + "&b_from=" + past10 + "&b_to=" + past11 + "&order_by=nope"
	requireAPIError(t, retentionRequest(t, api, http.MethodGet, path, ""), http.StatusBadRequest, "invalid order_by")
	path = "/api/v1/statements/compare?a_from=" + past10 + "&a_to=" + past11 + "&b_from=" + past10 + "&b_to=" + past11
	requireAPIError(t, retentionRequest(t, api, http.MethodGet, path, ""), http.StatusBadRequest, "missing instance_id")
}

func f64(v float64) *float64 { return &v }

// CMP-002: a statement in both periods, one only in A and one only in B get
// status both, gone and new; totals are compared per second of their
// period (A 1 h, B 2 h), delta is B − A with a missing side as 0, and ratio
// is B / A, null without A.
func TestCMP002(t *testing.T) {
	pa := period{From: cmpNow.Add(-3 * time.Hour), To: cmpNow.Add(-2 * time.Hour)}
	pb := period{From: cmpNow.Add(-2 * time.Hour), To: cmpNow}
	text := "SELECT 1"

	both := statementCompareRowOf("app", 1, &text,
		&statementTotals{calls: f64(3600), exec: f64(7200), mean: f64(2)},
		&statementTotals{calls: f64(14400), exec: f64(43200), mean: f64(3)}, pa, pb)
	require.Equal(t, "both", both.Status)
	require.Equal(t, "1", both.QueryID)
	require.InDelta(t, 1.0, *both.A.Calls, 1e-9)
	require.InDelta(t, 2.0, *both.B.Calls, 1e-9)
	require.InDelta(t, 1.0, *both.Delta.Calls, 1e-9)
	require.InDelta(t, 2.0, *both.Ratio.Calls, 1e-9)
	require.InDelta(t, 4.0, *both.Delta.TotalExec, 1e-9)
	require.InDelta(t, 3.0, *both.Ratio.TotalExec, 1e-9)
	require.InDelta(t, 1.5, *both.Ratio.MeanExec, 1e-9, "the mean is per call, not per second")
	require.Nil(t, both.Delta.Rows, "a metric neither side has stays null")

	gone := statementCompareRowOf("app", 2, nil, &statementTotals{exec: f64(3600)}, nil, pa, pb)
	require.Equal(t, "gone", gone.Status)
	require.Nil(t, gone.B)
	require.InDelta(t, -1.0, *gone.Delta.TotalExec, 1e-9)
	require.InDelta(t, 0.0, *gone.Ratio.TotalExec, 1e-9)

	fresh := statementCompareRowOf("app", 3, nil, nil, &statementTotals{exec: f64(7200)}, pa, pb)
	require.Equal(t, "new", fresh.Status)
	require.Nil(t, fresh.A)
	require.InDelta(t, 1.0, *fresh.Delta.TotalExec, 1e-9)
	require.Nil(t, fresh.Ratio.TotalExec, "no ratio without A")

	zero := statementCompareRowOf("app", 4, nil, &statementTotals{exec: f64(0)}, &statementTotals{exec: f64(10)}, pa, pb)
	require.Nil(t, zero.Ratio.TotalExec, "no ratio when A is 0")
}

// CMP-003: rows are ordered by the absolute change of the order_by metric,
// largest first, then by queryid.
func TestCMP003(t *testing.T) {
	row := func(id string, delta float64) statementCompareRow {
		return statementCompareRow{QueryID: id, Delta: statementMetrics{TotalExec: f64(delta), Calls: f64(-delta)}}
	}
	rows := []statementCompareRow{row("5", 1), row("2", -3), row("9", 2), row("1", 2), row("7", 0)}
	sortStatementCompare(rows, statementOrders["total_exec_time"].metric)
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.QueryID)
	}
	require.Equal(t, []string{"2", "1", "9", "5", "7"}, ids)
	for name, column := range map[string]string{"calls": "total_calls", "mean_time": "mean_exec", "wal_bytes_rate": "total_wal"} {
		require.Equal(t, column, statementOrders[name].column, name)
	}
}
