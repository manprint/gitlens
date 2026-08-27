package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Report is the JSON output structure for all workload commands
type Report struct {
	Command   string                 `json:"command"`
	Seed      int64                  `json:"seed"`
	StartTime time.Time              `json:"start_time"`
	EndTime   time.Time              `json:"end_time"`
	Duration  time.Duration          `json:"duration"`
	Succeeded int                    `json:"succeeded"`
	Failed    int                    `json:"failed"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: workloadctl <command> [options]\n")
		fmt.Fprintf(os.Stderr, "commands: deadlock, lock-storm, slow-query, idle-in-txn, distinct-queries, oltp\n")
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var err error
	switch cmd {
	case "deadlock":
		err = cmdDeadlock(ctx, args)
	case "lock-storm":
		err = cmdLockStorm(ctx, args)
	case "slow-query":
		err = cmdSlowQuery(ctx, args)
	case "idle-in-txn":
		err = cmdIdleInTxn(ctx, args)
	case "distinct-queries":
		err = cmdDistinctQueries(ctx, args)
	case "oltp":
		err = cmdOLTP(ctx, args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func newReport(command string, seed int64) Report {
	return Report{
		Command:   command,
		Seed:      seed,
		StartTime: time.Now(),
		Details:   make(map[string]interface{}),
	}
}

func printReport(r Report) {
	r.EndTime = time.Now()
	r.Duration = r.EndTime.Sub(r.StartTime)
	data, _ := json.MarshalIndent(r, "", "  ")
	fmt.Println(string(data))
}

// cmdDeadlock creates deterministic deadlocks by having pairs of transactions
// update two rows in opposite order with a WaitGroup barrier to ensure timing.
func cmdDeadlock(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("deadlock", flag.ContinueOnError)
	dsn := fs.String("dsn", os.Getenv("PGLENS_TEST_DSN"), "PostgreSQL connection string")
	pairs := fs.Int("pairs", 10, "number of deadlock pairs to create")
	duration := fs.Duration("duration", 30*time.Second, "how long to run")
	seed := fs.Int64("seed", time.Now().UnixNano(), "random seed")
	if err := fs.Parse(args); err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, *dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	report := newReport("deadlock", *seed)
	rand.Seed(*seed) //nolint:staticcheck // deterministic replay via --seed is the point; threading a *rand.Rand through every workload helper is not worth it for a test-only tool

	// Create test table
	_, err = pool.Exec(ctx, `
		DROP TABLE IF EXISTS deadlock_test CASCADE;
		CREATE TABLE deadlock_test (
			id INT PRIMARY KEY,
			value INT
		)
	`)
	if err != nil {
		return err
	}

	// Insert rows
	for i := 1; i <= 2; i++ {
		_, err = pool.Exec(ctx, "INSERT INTO deadlock_test (id, value) VALUES ($1, 0)", i)
		if err != nil {
			return err
		}
	}

	// Run deadlock generator
	err = deadlock(ctx, pool, *pairs, *duration, &report)
	if err != nil {
		return err
	}

	printReport(report)
	return nil
}

// cmdLockStorm creates lock contention by having many sessions compete for
// SELECT...FOR UPDATE on a single row.
func cmdLockStorm(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("lock-storm", flag.ContinueOnError)
	dsn := fs.String("dsn", os.Getenv("PGLENS_TEST_DSN"), "PostgreSQL connection string")
	sessions := fs.Int("sessions", 50, "number of concurrent sessions")
	duration := fs.Duration("duration", 20*time.Second, "how long to run")
	seed := fs.Int64("seed", time.Now().UnixNano(), "random seed")
	if err := fs.Parse(args); err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, *dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	report := newReport("lock-storm", *seed)
	rand.Seed(*seed) //nolint:staticcheck // deterministic replay via --seed is the point; threading a *rand.Rand through every workload helper is not worth it for a test-only tool

	// Create test table
	_, err = pool.Exec(ctx, `
		DROP TABLE IF EXISTS lockstorm_test CASCADE;
		CREATE TABLE lockstorm_test (
			id INT PRIMARY KEY,
			value INT
		)
	`)
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, "INSERT INTO lockstorm_test (id, value) VALUES (1, 0)")
	if err != nil {
		return err
	}

	err = lockStorm(ctx, pool, *sessions, *duration, &report)
	if err != nil {
		return err
	}

	printReport(report)
	return nil
}

// cmdSlowQuery creates slow queries using pg_sleep.
func cmdSlowQuery(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("slow-query", flag.ContinueOnError)
	dsn := fs.String("dsn", os.Getenv("PGLENS_TEST_DSN"), "PostgreSQL connection string")
	sleep := fs.Duration("sleep", 30*time.Second, "how long each query sleeps")
	count := fs.Int("count", 3, "number of queries to run")
	seed := fs.Int64("seed", time.Now().UnixNano(), "random seed")
	if err := fs.Parse(args); err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, *dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	report := newReport("slow-query", *seed)
	rand.Seed(*seed) //nolint:staticcheck // deterministic replay via --seed is the point; threading a *rand.Rand through every workload helper is not worth it for a test-only tool

	err = slowQuery(ctx, pool, *sleep, *count, &report)
	if err != nil {
		return err
	}

	printReport(report)
	return nil
}

// cmdIdleInTxn creates long-running idle transactions.
func cmdIdleInTxn(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("idle-in-txn", flag.ContinueOnError)
	dsn := fs.String("dsn", os.Getenv("PGLENS_TEST_DSN"), "PostgreSQL connection string")
	sessions := fs.Int("sessions", 5, "number of idle transactions")
	hold := fs.Duration("hold", 2*time.Minute, "how long to hold the transactions")
	seed := fs.Int64("seed", time.Now().UnixNano(), "random seed")
	if err := fs.Parse(args); err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, *dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	report := newReport("idle-in-txn", *seed)
	rand.Seed(*seed) //nolint:staticcheck // deterministic replay via --seed is the point; threading a *rand.Rand through every workload helper is not worth it for a test-only tool

	err = idleInTxn(ctx, pool, *sessions, *hold, &report)
	if err != nil {
		return err
	}

	printReport(report)
	return nil
}

// cmdDistinctQueries generates many distinct queries to test queryid cardinality.
func cmdDistinctQueries(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("distinct-queries", flag.ContinueOnError)
	dsn := fs.String("dsn", os.Getenv("PGLENS_TEST_DSN"), "PostgreSQL connection string")
	count := fs.Int("count", 5000, "number of distinct queries to execute")
	seed := fs.Int64("seed", time.Now().UnixNano(), "random seed")
	if err := fs.Parse(args); err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, *dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	report := newReport("distinct-queries", *seed)
	rand.Seed(*seed) //nolint:staticcheck // deterministic replay via --seed is the point; threading a *rand.Rand through every workload helper is not worth it for a test-only tool

	// Ensure pg_stat_statements is enabled
	_, _ = pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements")

	err = distinctQueries(ctx, pool, *count, &report)
	if err != nil {
		return err
	}

	printReport(report)
	return nil
}

// cmdOLTP generates a pgbench-like workload.
func cmdOLTP(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("oltp", flag.ContinueOnError)
	dsn := fs.String("dsn", os.Getenv("PGLENS_TEST_DSN"), "PostgreSQL connection string")
	tps := fs.Int("tps", 200, "target transactions per second")
	duration := fs.Duration("duration", 60*time.Second, "how long to run")
	seed := fs.Int64("seed", time.Now().UnixNano(), "random seed")
	if err := fs.Parse(args); err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, *dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	report := newReport("oltp", *seed)
	rand.Seed(*seed) //nolint:staticcheck // deterministic replay via --seed is the point; threading a *rand.Rand through every workload helper is not worth it for a test-only tool

	// Create test table
	_, err = pool.Exec(ctx, `
		DROP TABLE IF EXISTS oltp_test CASCADE;
		CREATE TABLE oltp_test (
			id INT PRIMARY KEY,
			value INT
		)
	`)
	if err != nil {
		return err
	}

	// Insert initial rows
	for i := 1; i <= 100; i++ {
		_, err = pool.Exec(ctx, "INSERT INTO oltp_test (id, value) VALUES ($1, 0)", i)
		if err != nil {
			return err
		}
	}

	err = oltp(ctx, pool, *tps, *duration, &report)
	if err != nil {
		return err
	}

	printReport(report)
	return nil
}
