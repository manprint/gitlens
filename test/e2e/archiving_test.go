//go:build e2e

package e2e

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/manprint/pglens/test/harness"
	"github.com/manprint/pglens/test/scenario"
)

const archiverTable = "pglens_e2e_archiver_probe"

func init() {
	scenario.Register(scenario.Scenario{
		ID:          "SYS-ARCH-001",
		Title:       "archiver failure is visible and resolves after repair",
		Topology:    scenario.TopologyStandalone,
		EstDuration: 3 * time.Minute,
		Covers:      []string{"phase_10.md#9.7", "IDEA.md#11"},
		Expect:      scenario.Expectations{Invariants: []string{"I-1", "I-2", "I-3", "I-4"}},
		Run:         runArchiving,
	})
}

func TestFull_Archiving(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping L3 archiving test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyStandalone,
		AgentMode: resolveAgentMode(harness.AgentModeContainer),
	})
	h.Scenario(t, "SYS-ARCH-001")
}

func runArchiving(ctx context.Context, e *scenario.Env) error {
	// This proves only what pglens can honestly observe: the PostgreSQL
	// archiver and its WAL statistics. It does not prove that pg_basebackup
	// succeeded, that a restore works, or that the archive destination is
	// readable.
	pg := e.PG("pg")
	if err := configureArchiver(ctx, pg); err != nil {
		return err
	}
	if err := e.Compose("restart", "pg"); err != nil {
		return fmt.Errorf("restart PostgreSQL with archive_mode enabled: %w", err)
	}
	// The restart invalidates every connection currently held by the pool.
	// Reset closes those connections while keeping the pool available for the
	// post-restart polling below.
	pg.Reset()
	if err := waitForArchiveMode(ctx, e); err != nil {
		return err
	}

	instanceID, err := advisorInstanceID(ctx, e, "pg")
	if err != nil {
		return err
	}
	before, err := archiverStats(e)
	if err != nil {
		return fmt.Errorf("read initial archiver statistics: %w", err)
	}
	if err := prepareArchiverWorkload(e, 0); err != nil {
		return err
	}
	if err := waitForArchiverFailure(ctx, e, before.Failed); err != nil {
		return err
	}
	if err := waitForObservedMetric(ctx, e, instanceID, "pg_archiver_failed_total", func(v float64) bool { return v > 0 }); err != nil {
		return fmt.Errorf("failed archiver counter was not observed: %w", err)
	}
	if err := waitForObservedMetric(ctx, e, instanceID, "pg_archiver_last_failed_age_seconds", func(v float64) bool { return v >= 0 && v < 60 }); err != nil {
		return fmt.Errorf("recent failed-archive age was not observed: %w", err)
	}
	if err := waitForArchiverFinding(ctx, e, instanceID, "open"); err != nil {
		return err
	}

	if err := setArchiveCommandInContainer(e, "/bin/true"); err != nil {
		return err
	}
	if err := prepareArchiverWorkload(e, 1000000); err != nil {
		return err
	}
	if err := waitForArchiverSuccess(ctx, e, before.Archived); err != nil {
		return err
	}
	if err := waitForObservedMetric(ctx, e, instanceID, "pg_archiver_archived_total", func(v float64) bool { return v > 0 }); err != nil {
		return fmt.Errorf("archived counter was not observed: %w", err)
	}
	if err := waitForArchiverFinding(ctx, e, instanceID, "resolved"); err != nil {
		return err
	}

	e.AssertInvariants(e.T)
	return nil
}

func configureArchiver(ctx context.Context, pg *pgxpool.Pool) error {
	if _, err := pg.Exec(ctx, "ALTER SYSTEM SET archive_mode = 'on'"); err != nil {
		return fmt.Errorf("enable archive_mode: %w", err)
	}
	return setArchiveCommand(ctx, pg, "/bin/false")
}

func setArchiveCommand(ctx context.Context, pg *pgxpool.Pool, command string) error {
	if command != "/bin/false" && command != "/bin/true" {
		return fmt.Errorf("unsupported archive command %q", command)
	}
	if _, err := pg.Exec(ctx, "ALTER SYSTEM SET archive_command = '"+command+"'"); err != nil {
		return fmt.Errorf("set archive_command to %s: %w", command, err)
	}
	if _, err := pg.Exec(ctx, "SELECT pg_reload_conf()"); err != nil {
		return fmt.Errorf("reload archive_command: %w", err)
	}
	return nil
}

func execPostgres(e *scenario.Env, sql string) (string, error) {
	output, err := e.Exec("pg", "psql", "-U", "postgres", "-d", "postgres", "-At", "-v", "ON_ERROR_STOP=1", "-c", sql)
	if err != nil {
		return output, fmt.Errorf("psql %q: %w, output: %s", sql, err, strings.TrimSpace(output))
	}
	return output, nil
}

func setArchiveCommandInContainer(e *scenario.Env, command string) error {
	if command != "/bin/false" && command != "/bin/true" {
		return fmt.Errorf("unsupported archive command %q", command)
	}
	if _, err := execPostgres(e, "ALTER SYSTEM SET archive_command = '"+command+"'"); err != nil {
		return fmt.Errorf("set archive_command to %s: %w", command, err)
	}
	if _, err := execPostgres(e, "SELECT pg_reload_conf()"); err != nil {
		return fmt.Errorf("reload archive_command: %w", err)
	}
	return nil
}

func waitForArchiveMode(ctx context.Context, e *scenario.Env) error {
	return pollMaintenance(ctx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		output, err := execPostgres(e, "SELECT current_setting('archive_mode')")
		if err != nil {
			return false, nil
		}
		mode := strings.TrimSpace(output)
		if mode != "on" {
			return false, fmt.Errorf("archive_mode after restart is %q, want on", mode)
		}
		return true, nil
	})
}

type archiverStatsSnapshot struct {
	Archived float64
	Failed   float64
}

func archiverStats(e *scenario.Env) (archiverStatsSnapshot, error) {
	output, err := execPostgres(e, "SELECT archived_count, failed_count FROM pg_stat_archiver")
	if err != nil {
		return archiverStatsSnapshot{}, err
	}
	values := strings.Split(strings.TrimSpace(output), "|")
	if len(values) != 2 {
		return archiverStatsSnapshot{}, fmt.Errorf("unexpected pg_stat_archiver output %q", output)
	}
	archived, err := strconv.ParseFloat(values[0], 64)
	if err != nil {
		return archiverStatsSnapshot{}, fmt.Errorf("parse archived_count %q: %w", values[0], err)
	}
	failed, err := strconv.ParseFloat(values[1], 64)
	if err != nil {
		return archiverStatsSnapshot{}, fmt.Errorf("parse failed_count %q: %w", values[1], err)
	}
	return archiverStatsSnapshot{Archived: archived, Failed: failed}, nil
}

func prepareArchiverWorkload(e *scenario.Env, idOffset int64) error {
	if _, err := execPostgres(e, "CREATE TABLE IF NOT EXISTS "+archiverTable+" (id bigint PRIMARY KEY, payload text)"); err != nil {
		return fmt.Errorf("create archiver workload: %w", err)
	}
	for batch := int64(0); batch < 6; batch++ {
		insertSQL := fmt.Sprintf("INSERT INTO %s (id, payload) SELECT %d + g, repeat('x', 1024) FROM generate_series(1, 4000) AS g", archiverTable, idOffset+batch*10000)
		if _, err := execPostgres(e, insertSQL); err != nil {
			return fmt.Errorf("write archiver workload: %w", err)
		}
		if _, err := execPostgres(e, "SELECT pg_switch_wal()"); err != nil {
			return fmt.Errorf("switch WAL for archiver workload: %w", err)
		}
	}
	return nil
}

func waitForArchiverFailure(ctx context.Context, e *scenario.Env, previous float64) error {
	return pollMaintenance(ctx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		stats, err := archiverStats(e)
		if err != nil {
			return false, nil
		}
		return stats.Failed > previous, nil
	})
}

func waitForArchiverSuccess(ctx context.Context, e *scenario.Env, previous float64) error {
	return pollMaintenance(ctx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		stats, err := archiverStats(e)
		if err != nil {
			return false, nil
		}
		return stats.Archived > previous, nil
	})
}

func waitForObservedMetric(ctx context.Context, e *scenario.Env, instanceID, metric string, wanted func(float64) bool) error {
	return pollMaintenance(ctx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		var value float64
		err := e.DB.QueryRow(ctx, `SELECT value FROM metrics
			WHERE instance_id=$1::uuid AND metric=$2
			ORDER BY ts DESC LIMIT 1`, instanceID, metric).Scan(&value)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return wanted(value), nil
	})
}

func waitForArchiverFinding(ctx context.Context, e *scenario.Env, instanceID, wantedState string) error {
	return pollMaintenance(ctx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		findings, err := advisorFindings(ctx, e, instanceID, "all")
		if err != nil {
			return false, err
		}
		for _, finding := range findings {
			if stringValue(finding["rule_id"]) != "archive.failing" || stringValue(finding["state"]) != wantedState {
				continue
			}
			if wantedState == "open" && !strings.Contains(strings.ToLower(stringValue(finding["remediation"])), "wal") {
				return false, fmt.Errorf("archive.failing remediation does not mention WAL: %v", finding["remediation"])
			}
			return true, nil
		}
		return false, nil
	})
}
