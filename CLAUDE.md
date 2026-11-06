# pglens (postgres-analyze)

Go PostgreSQL monitor: `cmd/pglens-agent` (collects, pushes) and `cmd/pglens-server` (TimescaleDB store, API, embedded console from `web/`, see `web/CLAUDE.md`). Logic in `internal/*` (advisor, alert, check, topology, server, …).

- Go gates: `make fmt-check lint vet-tags test`; integration `make test-integration` (`-tags=integration`, needs Docker).
- Plans: `docs/plans/<NNN>_plan-*/` (pev-v3); live status only in each plan's `STATE.md`.
- Live lab stack lives outside the repo: `/home/agent/pglens-odoo-stack` (its `README.md`; `.env` holds secrets, never print it). Since plan 005 7.2 it runs HA: 3 PostgreSQL 18 nodes + `hactl` (`test/ha/hactl`, admin `127.0.0.1:8008/status`); marker `ha/ACTIVE`; compose needs `-f compose.yml -f ha/compose.ha.yml`; never start a stopped DB node by hand (`ha/rejoin.sh`).
- Destructive stack steps need a VERIFIED backup < 24 h (`backup.sh` + `verify_backup.sh`, dir `/home/agent/odoo-backups`).
- Never commit `internal/webui/dist/index.html`, `.claude/`, `web/src/node_modules/`.
