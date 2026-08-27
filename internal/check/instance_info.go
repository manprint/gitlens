package check

import (
	"context"
	"strings"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() {
	Register(&instanceInfoCheck{})
}

type instanceInfoCheck struct{}

func (c *instanceInfoCheck) Name() string { return "instance_info" }
func (c *instanceInfoCheck) Requires() Requirements {
	return Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly}
}
func (c *instanceInfoCheck) DefaultInterval() time.Duration { return 10 * time.Second }
func (c *instanceInfoCheck) Timeout() time.Duration         { return 2 * time.Second }

func (c *instanceInfoCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	row := conn.QueryRow(ctx, `
SELECT (SELECT system_identifier::text FROM pg_control_system()) AS system_identifier,
       pg_is_in_recovery() AS in_recovery,
       current_setting('server_version_num')::int AS version_num,
       pg_postmaster_start_time() AS start_time,
       inet_server_addr()::text AS addr,
       inet_server_port() AS port
`)
	var sysID *string
	var inRecovery bool
	var versionNum int
	var startTime time.Time
	var addr *string
	var port *int
	err = row.Scan(&sysID, &inRecovery, &versionNum, &startTime, &addr, &port)
	if err != nil {
		if isPermissionDenied(err) {
			row2 := conn.QueryRow(ctx, `
SELECT pg_is_in_recovery() AS in_recovery,
       current_setting('server_version_num')::int AS version_num,
       pg_postmaster_start_time() AS start_time,
       inet_server_addr()::text AS addr,
       inet_server_port() AS port
`)
			var inRec2 bool
			var ver2 int
			var st2 time.Time
			var addr2 *string
			var port2 *int
			if err2 := row2.Scan(&inRec2, &ver2, &st2, &addr2, &port2); err2 != nil {
				return Result{}, err2
			}
			return buildInstanceInfoResult(nil, inRec2, ver2, st2, addr2, port2, t.Clock().Now()), nil
		}
		return Result{}, err
	}
	return buildInstanceInfoResult(sysID, inRecovery, versionNum, startTime, addr, port, t.Clock().Now()), nil
}

func buildInstanceInfoResult(sysID *string, inRecovery bool, versionNum int, startTime time.Time, addr *string, port *int, now time.Time) Result {
	uptime := now.Sub(startTime).Seconds()
	if uptime < 0 {
		uptime = 0
	}
	labels := map[string]string{}
	if sysID != nil {
		labels["system_identifier"] = *sysID
		labels["cluster_id_source"] = "system_identifier"
	} else {
		labels["cluster_id_source"] = "manual"
	}
	role := pgtype.RoleFromRecovery(inRecovery)
	labels["role"] = string(role)
	var metrics []pgtype.Metric
	metrics = append(metrics, pgtype.Metric{Name: "pg_up", Value: 1, Kind: pgtype.KindGauge, Labels: labels})
	metrics = append(metrics, pgtype.Metric{Name: "pg_in_recovery", Value: boolToFloat(inRecovery), Kind: pgtype.KindGauge, Labels: labels})
	metrics = append(metrics, pgtype.Metric{Name: "pg_version_num", Value: float64(versionNum), Kind: pgtype.KindGauge, Labels: labels})
	metrics = append(metrics, pgtype.Metric{Name: "pg_uptime_seconds", Value: uptime, Kind: pgtype.KindGauge, Labels: labels})
	if addr != nil && *addr != "" {
		metrics = append(metrics, pgtype.Metric{Name: "pg_addr", Value: 1, Kind: pgtype.KindGauge, Labels: map[string]string{"addr": *addr}})
	}
	if port != nil {
		metrics = append(metrics, pgtype.Metric{Name: "pg_port", Value: float64(*port), Kind: pgtype.KindGauge, Labels: labels})
	}
	return Result{Metrics: metrics}
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func isPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "42501") || strings.Contains(s, "permission denied") || strings.Contains(s, "must be superuser")
}
