package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/manprint/pglens/internal/store"
	"github.com/manprint/pglens/internal/watch"
	"github.com/manprint/pglens/internal/wire"
)

// incidentFactKind is the fact kind the agent's watcher sends a capture as.
const incidentFactKind = "incident"

// The events a capture is filed as, one per watcher incident type.
const (
	lockWaitEventType          = "lock_wait_captured"
	deadlockEventType          = "deadlock_captured"
	longQueryEventType         = "long_query_captured"
	idleInTransactionEventType = "idle_in_transaction_captured"
	longTransactionEventType   = "long_transaction_captured"
)

// incidentEventTypes maps the type of a capture to the event it is filed as.
var incidentEventTypes = map[string]string{
	watch.TypeLockWait:   lockWaitEventType,
	watch.TypeDeadlock:   deadlockEventType,
	watch.TypeLongQuery:  longQueryEventType,
	watch.TypeIdleInXact: idleInTransactionEventType,
	watch.TypeLongXact:   longTransactionEventType,
}

// incidentEvent turns an incident fact into an event whose payload is the
// capture: the sessions, their statements, the table and the row. A statement
// that may carry a credential is withheld again, for an agent that did not.
func incidentEvent(f wire.Fact, ts time.Time, tenantID string, clusterID int64, instanceID uuid.UUID) (store.EventRow, bool) {
	kind := f.Labels["type"]
	typ, ok := incidentEventTypes[kind]
	if !ok || len(f.ValueJSON) == 0 {
		return store.EventRow{}, false
	}
	dec := json.NewDecoder(bytes.NewReader(f.ValueJSON))
	dec.UseNumber()
	var payload map[string]any
	if err := dec.Decode(&payload); err != nil || payload == nil {
		return store.EventRow{}, false
	}
	withholdQueries(payload)
	payload["id"] = f.Key
	return store.EventRow{TenantID: tenantID, TS: ts, Type: typ, ClusterID: &clusterID, InstanceID: &instanceID, Payload: payload}, true
}

// incidentRecorded reports whether the event of an incident is already stored
// or queued in this envelope, so an envelope sent again adds no copy. The
// store is read inside the transaction, as truncationEventDue does.
func incidentRecorded(ctx context.Context, tx pgx.Tx, ev store.EventRow, queued map[string]bool) (bool, error) {
	id, _ := ev.Payload["id"].(string)
	if queued[id] {
		return true, nil
	}
	queued[id] = true
	var found bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM events WHERE tenant_id=$1 AND cluster_id=$2 AND instance_id=$3 AND ts=$4 AND type=$5 AND payload->>'id'=$6)`,
		ev.TenantID, *ev.ClusterID, *ev.InstanceID, ev.TS, ev.Type, id).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("look up incident %s: %w", id, err)
	}
	return found, nil
}
