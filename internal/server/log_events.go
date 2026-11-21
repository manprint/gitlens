package server

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/manprint/pglens/internal/pglog"
	"github.com/manprint/pglens/internal/redact"
	"github.com/manprint/pglens/internal/store"
	"github.com/manprint/pglens/internal/wire"
)

// logEventFactKind is the fact the agent's log reader ships per log line; it
// goes to log_events, never to object_facts (plan 007 6.5).
const logEventFactKind = "log_event"

// logWithheld replaces a log text that may hold a credential. The agent
// withholds first; the server checks again (I-4).
const logWithheld = "[withheld: may hold a credential]"

func withholdLogText(s string) string {
	if redact.MayHoldCredential(s) {
		return logWithheld
	}
	return s
}

// logEventRow maps a log_event fact to its row; false when the fact lacks a
// valid ts, class or severity.
func logEventRow(f wire.Fact, tenantID string, clusterID int64, instanceID uuid.UUID) (store.LogEventRow, bool) {
	ts, err := time.Parse(time.RFC3339Nano, f.Labels["ts"])
	if err != nil {
		return store.LogEventRow{}, false
	}
	class, severity := f.Labels["class"], f.Labels["severity"]
	if severity == "" || !slices.Contains(pglog.Classes(), class) {
		return store.LogEventRow{}, false
	}
	var body map[string]string
	if err := json.Unmarshal(f.ValueJSON, &body); err != nil {
		return store.LogEventRow{}, false
	}
	row := store.LogEventRow{
		TS: ts.UTC(), TenantID: tenantID, ClusterID: clusterID, InstanceID: instanceID,
		Datname: f.Labels["datname"], Usename: f.Labels["usename"],
		ApplicationName: withholdLogText(f.Labels["application_name"]),
		Severity:        severity, SQLState: f.Labels["sqlstate"], Class: class,
		Message: withholdLogText(body["message"]),
	}
	if i := strings.LastIndexByte(f.Key, '/'); i >= 0 {
		row.SessionID = f.Key[:i]
		row.LineNum, _ = strconv.ParseInt(f.Key[i+1:], 10, 64)
	}
	if pid, err := strconv.ParseInt(f.Labels["pid"], 10, 32); err == nil {
		v := int32(pid)
		row.PID = &v
	}
	if q, err := strconv.ParseInt(f.Labels["queryid"], 10, 64); err == nil {
		row.QueryID = &q
	}
	extra := map[string]string{}
	for k, v := range body {
		if k != "message" && v != "" {
			extra[k] = withholdLogText(v)
		}
	}
	if len(extra) > 0 {
		row.Extra, _ = json.Marshal(extra)
	}
	return row, true
}
