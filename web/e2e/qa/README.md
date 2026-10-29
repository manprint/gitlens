# Live QA harness (`web/e2e/qa/`)

Playwright helpers and self-tests that drive the **live** pglens + Odoo stack
(`/home/agent/pglens-odoo-stack`, compose project `pglens-odoo`). The strategy,
budgets and indicator matrix are in
`docs/plans/005_plan-ConsoleInsights/test-strategy.md`.

## Modules

| Module       | Purpose                                                                                                                        |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------ |
| `guard.ts`   | flags, compose project and verified-backup checks; integer/identifier SQL builders                                             |
| `stack.ts`   | `run()` (argument arrays only), `stackStatus`, `restart/stop/start/down/up/rebuild`, `waitReady`                               |
| `pg.ts`      | ground truth and stimulus on the monitored PostgreSQL through `docker exec … psql`; finalizers `registerCleanup`/`runCleanups` |
| `odoo.ts`    | Odoo JSON-RPC stimulus (writes only QA-category partners and their draft orders)                                               |
| `api.ts`     | pglens API reads with the bootstrap token                                                                                      |
| `observe.ts` | UI readers and `consoleGuard`                                                                                                  |
| `budget.ts`  | stimulus → UI response budgets and `qaTimeout()`                                                                               |
| `runid.ts`   | `qa-<yyyymmddhhmmss>-<4hex>` run identifiers                                                                                   |

## Running

Every command names its project: a bare `playwright test` would run a spec once
per browser project. Secrets come from the stack `.env` and are never printed.

```bash
cd web
set -a; . /home/agent/pglens-odoo-stack/.env; set +a
export PGLENS_UI_BASE_URL=http://127.0.0.1:9090 PGLENS_QA_ALLOW_STIMULUS=1
pnpm exec playwright test --project=harness                                   # node-only self-tests
pnpm exec playwright test --project=desktop-fullhd e2e/qa/harness-browser.spec.ts
```

Projects: `chromium` (legacy `smoke`/`ui_acceptance` specs only, driven by
`test/e2e/ui_test.go`), `harness` (node-only), `desktop-fullhd` 1920×1080,
`desktop-maximized` 1920×950, `laptop` 1440×900, `tablet` 768×1024, `phone` 390×844.

## Safety

- Stimulus needs `PGLENS_QA_ALLOW_STIMULUS=1`.
- Stopping, restarting, rebuilding or removing services needs
  `PGLENS_QA_ALLOW_LIFECYCLE=1`, the compose project `pglens-odoo` and a backup in
  `/home/agent/odoo-backups/` whose `VERIFIED` file (from `verify_backup.sh`) is
  younger than 24 h and matches its `SHA256SUMS`. `odoo-db` is never stopped by
  `down()`; volumes are never removed. `up()` starts only the lifecycle services with
  `--no-deps`, so `odoo-db` is never recreated; `upAll()` (`--no-recreate`) is
  reserved for LIFE-DOWN-UP-FULL and the HA drills.
- Stimulus objects live in schema `qa_tmp` and in sessions named `qa-…`; the
  finalizers remove them even after a failure, and `QA-CLEAN-001` (last test of
  the harness project) proves nothing is left.
- If a run dies, clean by hand:
  `docker exec pglens-odoo-odoo-db-1 psql -U odoo -d odoo -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name LIKE 'qa-%'; DROP SCHEMA IF EXISTS qa_tmp CASCADE;"`
  and delete the Odoo partners of category `QA`.
- Modules have no import-time side effects (no `.env` reads, no docker calls), so
  `playwright test --list` works anywhere.
