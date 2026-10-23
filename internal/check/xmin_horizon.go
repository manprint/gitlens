package check

import (
	"context"
	"encoding/json"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/redact"
)

// xminHorizonFactKind and xminHorizonFactKey name the one fact that says which
// holder keeps the xmin horizon back the most. xminHorizonMetric is the gauge
// of the oldest holder of each kind.
const (
	xminHorizonFactKind = "xmin_horizon"
	xminHorizonFactKey  = "oldest"
	xminHorizonMetric   = "pg_xmin_horizon_age_xids"
)

// xminHolderKinds lists the holder kinds in the order their gauges are
// reported, and the order that breaks a tie between holders of equal age.
var xminHolderKinds = []string{"session", "slot", "standby", "prepared"}

// xminHolder is one holder of the xmin horizon: the oldest of its kind. The
// identity is the pid of a session, the name of a slot, the application_name
// of a standby, or the gid of a prepared transaction. AgeSeconds is nil when
// the holder has no start time to measure from.
type xminHolder struct {
	HolderKind string   `json:"holder_kind"`
	Identity   string   `json:"identity"`
	AgeXIDs    int64    `json:"age_xids"`
	AgeSeconds *float64 `json:"age_seconds,omitempty"`
}

// xminHoldersQuery reads the oldest holder of each kind across the four
// places a backend can hold the horizon: a client session's snapshot or own
// transaction, a replication slot's xmin and catalog_xmin, a standby's
// hot_standby_feedback xmin, and a prepared transaction. Only catalog views
// are read, and no query text. The agent's own session is left out, as the
// activity check leaves it out.
const xminHoldersQuery = `
SELECT holder_kind, identity, age_xids, age_seconds
FROM (
  SELECT holder_kind, identity, age_xids, age_seconds,
         row_number() OVER (PARTITION BY holder_kind ORDER BY age_xids DESC, identity) AS rank_in_kind
  FROM (
    SELECT 'session'::text AS holder_kind,
           pid::text AS identity,
           GREATEST(age(backend_xmin), age(backend_xid))::bigint AS age_xids,
           EXTRACT(epoch FROM now() - xact_start)::float8 AS age_seconds
    FROM pg_stat_activity
    WHERE backend_type = 'client backend'
      AND pid <> pg_backend_pid()
      AND (backend_xmin IS NOT NULL OR backend_xid IS NOT NULL)
    UNION ALL
    SELECT 'slot'::text, slot_name::text,
           GREATEST(age(xmin), age(catalog_xmin))::bigint, NULL::float8
    FROM pg_replication_slots
    WHERE xmin IS NOT NULL OR catalog_xmin IS NOT NULL
    UNION ALL
    SELECT 'standby'::text, COALESCE(application_name, '')::text,
           age(backend_xmin)::bigint, NULL::float8
    FROM pg_stat_replication
    WHERE backend_xmin IS NOT NULL
    UNION ALL
    SELECT 'prepared'::text, gid::text,
           age(transaction)::bigint,
           EXTRACT(epoch FROM now() - prepared)::float8
    FROM pg_prepared_xacts
  ) holders
) ranked
WHERE rank_in_kind = 1
`

// queryXminHolders runs xminHoldersQuery and returns one holder per kind that
// holds the horizon. An identity that is free text, a standby's application_name
// or a prepared transaction's gid, is shipped as redact.Identity gives it.
func queryXminHolders(ctx context.Context, conn Conn) ([]xminHolder, error) {
	rows, err := conn.Query(ctx, xminHoldersQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var holders []xminHolder
	for rows.Next() {
		var h xminHolder
		if err := rows.Scan(&h.HolderKind, &h.Identity, &h.AgeXIDs, &h.AgeSeconds); err != nil {
			return nil, err
		}
		holders = append(holders, sanitizeXminHolder(h))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return holders, nil
}

// sanitizeXminHolder clamps the ages at zero, as the activity ages are: a
// timestamp or an xid a few microseconds ahead of the collector's reading is
// not a negative age. It withholds the free-text identities.
func sanitizeXminHolder(h xminHolder) xminHolder {
	if h.AgeXIDs < 0 {
		h.AgeXIDs = 0
	}
	if h.AgeSeconds != nil && *h.AgeSeconds < 0 {
		zero := 0.0
		h.AgeSeconds = &zero
	}
	switch h.HolderKind {
	case "standby", "prepared":
		h.Identity = redact.Identity(h.Identity)
	}
	return h
}

// xminHorizonMetrics reports the age of the oldest holder of each kind. A kind
// with no holder reports 0, which is what its age is: the query answered, so
// the absence is known and not a missing sample.
func xminHorizonMetrics(holders []xminHolder) []pgtype.Metric {
	ages := make(map[string]int64, len(holders))
	for _, h := range holders {
		if h.AgeXIDs > ages[h.HolderKind] {
			ages[h.HolderKind] = h.AgeXIDs
		}
	}
	metrics := make([]pgtype.Metric, 0, len(xminHolderKinds))
	for _, kind := range xminHolderKinds {
		metrics = append(metrics, pgtype.Metric{
			Name:   xminHorizonMetric,
			Value:  float64(ages[kind]),
			Kind:   pgtype.KindGauge,
			Labels: map[string]string{"holder_kind": kind},
		})
	}
	return metrics
}

// oldestXminHolder returns the holder with the largest xid age. A tie goes to
// the kind listed first in xminHolderKinds.
func oldestXminHolder(holders []xminHolder) (xminHolder, bool) {
	var oldest xminHolder
	found := false
	rank := func(kind string) int {
		for i, k := range xminHolderKinds {
			if k == kind {
				return i
			}
		}
		return len(xminHolderKinds)
	}
	for _, h := range holders {
		if !found || h.AgeXIDs > oldest.AgeXIDs ||
			(h.AgeXIDs == oldest.AgeXIDs && rank(h.HolderKind) < rank(oldest.HolderKind)) {
			oldest = h
			found = true
		}
	}
	return oldest, found
}

// xminHorizonFacts returns the fact that names the oldest holder, or none when
// no holder keeps the horizon back. The activity check replaces the fact of its
// kind, so the fact of a previous sample goes when nothing holds the horizon.
func xminHorizonFacts(holders []xminHolder) ([]Fact, error) {
	oldest, ok := oldestXminHolder(holders)
	if !ok {
		return nil, nil
	}
	raw, err := json.Marshal(oldest)
	if err != nil {
		return nil, err
	}
	return []Fact{{Kind: xminHorizonFactKind, Key: xminHorizonFactKey, ValueJSON: raw}}, nil
}
