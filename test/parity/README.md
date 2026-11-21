# Reference scenario (plan 007)

End-to-end check of the plan 007 features on an ephemeral local stack: one
monitored PostgreSQL 18 (`pglens-pgext:18`, with hypopg, pg_hint_plan and
auto_explain) and the server's TimescaleDB store in Docker, with the server and
the agent built from the working tree and run on the host. Everything binds
loopback only; nothing is sent outside the machine.

| File | Role |
|---|---|
| `compose.yml` | Store (`127.0.0.1:55432`) and monitored PostgreSQL (`127.0.0.1:55433`, jsonlog, auto_explain 200 ms, autovacuum naptime 5 s) |
| `init.sh` | First boot of the monitored PostgreSQL: database `parity`, users `app_a` and `app_b`, tables `orders` (300 000 rows), `churn`, `lock_a`, `lock_b`, the monitoring role at tier 1 and its log-reading grants |
| `endpoints/main.go` | Local sinks: a Teams-style webhook on `127.0.0.1:18081` and an SMTP sink on `127.0.0.1:12525`, each appending to a file |
| `scenario.sh` | `up`, `workload [minutes]`, `check`, `down` |
| `../../web/e2e/parity.spec.ts` | Console steps (PARITY-E2E-001) |

## Run

```sh
test/parity/scenario.sh up          # secrets, stack, binaries, sinks, server, agent, Teams and email channels
test/parity/scenario.sh workload 5  # one slow statement, churn, about 5 minutes of tagged traffic, then one deadlock
test/parity/scenario.sh check       # nine PASS / FAIL / DEVIATION lines; between 15 and 30 minutes after workload
test/parity/scenario.sh down        # stops the processes; removes containers, network and volumes
```

`check` reads the last 30 minutes. Step 2 needs the resolved deadlock
notification, which follows about 12 minutes after the deadlock, when its rate
has fallen back to 0. The deadlock comes last in `workload` because the alert
is the rise of a counter between two `database_stats` scrapes: one taken
before the deadlock is needed.

`PARITY_DIR` (default `/tmp/pglens-parity`) holds the generated secrets
(`env`, mode 0600), the token file, the agent state, the sink files and the
logs. Secrets are read from those files and never printed; `curl` takes the
`Authorization` header from a file. Remove the directory after `down`.

The console steps need the server address, the UI password and the token
from that directory:

```sh
cd web && PGLENS_UI_BASE_URL=http://127.0.0.1:18080 \
  PGLENS_UI_PASSWORD="$(sed -n 's/^PARITY_UI_PASSWORD=//p' "$PARITY_DIR/env")" \
  PGLENS_BOOTSTRAP_TOKEN="$(cat "$PARITY_DIR/token")" \
  pnpm exec playwright test e2e/parity.spec.ts e2e/i18n_it.spec.ts --project=desktop-fullhd
```

## Checks

| Step | What `check` asserts |
|---|---|
| 1 | `/api/v1/ash` grouped by `usename`, `client_addr`, `application_name`, `service` returns at least two groups each |
| 2 | The Teams file and the SMTP sink each hold a firing and a `RESOLVED` message of `db.deadlock_rate` (the resolved one follows when the deadlock rate falls back to 0) |
| 3 | `txn.multixact_wraparound_risk` is registered and enabled and has no finding at the default threshold. DEVIATION: the advisor override route only enables or disables a rule, so the threshold crossing is proved by the rule's unit tests |
| 4 | `/statements/compare` and `/ash/compare`, yesterday against today, answer 200 |
| 5 | A range 40–60 days old is served from hourly rollups: `resolution` is `1h` for `/statements` and `/ash`, and grouping ASH by `usename` there is refused with 400 |
| 6 | `/logs?class=lock.deadlock` has the deadlock, `/plans/samples` has auto_explain samples of the tagged statement, `/autovacuum/runs` has a run on `churn` |
| 7 | An `index_check` command for `orders(customer)` reaches `done` with `cost_after < cost_before` |
| 8 | A workbook variant with `enable_indexscan=off`, `enable_bitmapscan=off` and `max_parallel_workers_per_gather=0` plans differently from Baseline (plan hashes differ). The statement has no index to give up, so the serial setting is what changes its plan |
| 9 | MCP `tools/list` returns the 30 tools and `tools/call pglens_list_instances` returns the ids of `GET /api/v1/instances` |

PagerDuty is not configured here: its endpoint is the real service. Its
evidence is `go test ./internal/alert/notify ./internal/server -run 'TestNCH01[0-4]'`.
