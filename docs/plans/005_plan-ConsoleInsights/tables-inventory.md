# Tables inventory (9.2 S1 design contract)

Plan 005, revision 1. Written by the 9.2 executor; approved by the supervisor
before the 9.2 integrations start (STATE §7 R-9.2-INV; revised after its first
round). It is the design
contract for 9.2 (the 21 `DataTable` call sites) and for 9.3 (the bespoke
tables). The worker does not invent filters beyond it.

## Rules applied (D12, D13, D23, phase_10 local design)

- **Search**: always, over the table's identity columns (names, addresses,
  ids, definitions). Case-insensitive substring (`matchesSearch`).
- **Select filters**: only for a low-cardinality column named
  state/status/kind/role/severity/type/scope/database/schema/source/method,
  and not when that column is the table's identity (Database sizes,
  Temporary files). A filter accessor returns the **text the cell displays**
  (mapped booleans, labels, `Unknown`/`missing` for empty values, never
  `''`, which equals the "All" value); options are the distinct accessor
  values (`filterOptions`) unless fixed options are listed. A spec that
  depends on render-time values (e.g. `readAt`) is `useMemo`'d on them.
- **Min filters**: none in 9.2: the Query inspector's rows are a
  server-ranked top-N, so a client-side minimum on calls would mislead, and
  no other DataTable page has a size/dead tuples threshold notion. In 9.3 the phase_10 9.3 contract lists the
  filters of each bespoke table and names no numeric threshold; that
  contract overrides D12 point 3 (recorded in STATE §8), so no min filter
  is added in phase 9.
- **No duplicates**: a column the page already filters gets no toolbar
  filter: the `Database` select of every CatalogSection tab
  (`DatabaseFilter`), the Triggers `Disabled only` checkbox, the Index
  analysis page `Database` select (`?db=`, IndexAnalysisPage ~437), the Rule
  catalogue's own
  `Rule level` / `Rule scope` / `Available at or below` selects and the
  Findings page selects, the Query inspector `Sort statements` select.
- **Labels**: `Search <ariaLabel>`, `Filter <ariaLabel> by <column>`; the
  table name is the existing `ariaLabel`, so no label collides with an
  existing control on the same page (existing tests use exact names).
- **tableId**: kebab case of the `ariaLabel`. Exception: the per-instance
  "Databases for <addr>:<port>" tables use `instance-databases-<instance_id>`
  (an address with dots and a port is a poor URL key, and several of these
  tables share a page).
- **Default sort**: the data's current order (server order) unless listed;
  sortable columns keep `enableSorting` as today. Sort accessors return raw
  values (numbers/strings), not the rendered text: a column whose
  `accessorFn` returns a formatted string gets a raw `accessorFn` and keeps
  its `cell` for display. Missing values sort last (D12): accessors that
  return stand-ins (`?? -1`, `?? ''`, `?? Number.NEGATIVE_INFINITY`) return
  `null` instead, unless the cell reads `getValue()` for display (then the
  cell handles `null`). Known: Sequences `used`/`last_value`/`remaining`,
  Partitioned tables `default`/`analyzed`/`covered`, Functions `mean`,
  SECURITY DEFINER `search_path`, InternalsSection `growth_*`/`hit_ratio`,
  Subscriptions `slot`/`last_message`/`conflicts`.
- **Toolbar specs** (`searchFields`, `filters`) are module-level constants or
  `useMemo`'d (R-9.1: `DataTable` memoises on them).
- **Unit tests**: one `TBL-UI-0NN <tableId>` per row below (TBL-UI-010…030):
  a 25-row fixture renders at most 20 rows; the search narrows by one listed
  field; the select filter narrows (only where the row lists one); the sort
  buttons of the listed sort column reorder the rows. Existing tests keep
  their IDs; queries scope with `within(table)`/`within(nav)` because
  `Sort …`, `Rows per page` and `Clear filters` repeat across tables on the
  same page.

## 9.2 — DataTable call sites

| # | File:line (approx.) | ariaLabel → tableId | Search fields | Select filters | Sort test column | Test ID | Notes |
|---|---------------------|---------------------|---------------|----------------|------------------|---------|-------|
| 1 | `features/cluster/DriftSection.tsx` ~110 | Configuration drift → `configuration-drift` | setting `name` | — | Setting | TBL-UI-010 | instance columns are values per instance, no categorical column |
| 2 | `features/cluster/SlotsSection.tsx` ~122 | Replication slots → `replication-slots` | `name`, `owningInstance` | `slotType` (Type; null → `Unknown`), `active` (Active state: fixed options `active`/`inactive`/`unknown` labelled `Active`/`Inactive`/`Unknown`, as the cell shows), `walStatus` (WAL status: `walStatusLabel(row)`, the displayed label) | Retained bytes | TBL-UI-011 | `SlotsSection.test` uses `data-table-rows` (kept) |
| 3 | `features/findings/RuleCatalogue.tsx` ~173 | Advisor rule catalogue → `advisor-rule-catalogue` | `ruleId`, `needs` | — (own selects exist) | Rule ID | TBL-UI-012 | |
| 4 | `features/instance/CatalogSection.tsx` ~202 | Triggers → `triggers` | trigger `name`, `table`, `function` | — (Database select, Disabled only exist) | Trigger | TBL-UI-013 | UI-CAT-001..003 helper `tableRows()` uses `screen.getByRole('table')` (one table: inactive tabs unmount): unchanged |
| 5 | same ~261 | Unindexed foreign keys → `unindexed-foreign-keys` | `table`, `constraint`, `columns`, `referenced` | — | Table size | TBL-UI-014 | |
| 6 | same ~325 | Extensions → `extensions` | extension `name`, `schema` | `schema` (Schema) | Extension | TBL-UI-015 | |
| 7 | same ~441 | Spatial columns → `spatial-columns` | `table`, `column` | `declared` (Type, the displayed type), `index_method` (Spatial index: `index_method \|\| 'missing'`) | Rows (est.) | TBL-UI-016 | UI-CAT-005 `getByText('geometry(MultiPolygon)')`/`getByText('missing')` (~376/378) also match the filter options: scoped with `within(screen.getByRole('table', { name: 'Spatial columns' }))` (ID/intent unchanged, STATE §8) |
| 8 | same ~602 | Sequences → `sequences` | `sequence`, `column` (Feeds) | `data_type` (Type) | Used | TBL-UI-017 | |
| 9 | same ~772 | Partitioned tables → `partitioned-tables` | `table`, `partition_key` | — | Size (est.) | TBL-UI-018 | |
| 10 | same ~866 | Functions → `functions` | `function` | — | Calls | TBL-UI-019 | |
| 11 | same ~973 | SECURITY DEFINER functions → `security-definer-functions` | `function`, `owner` | — | Function | TBL-UI-020 | |
| 12 | `features/instance/InternalsSection.tsx` ~129 | Recovery conflicts → `recovery-conflicts` | `database`, `reason` | `database` (Database) | Queries cancelled | TBL-UI-021 | InstancePage's `DatabaseSelector` (`?db`) scopes only the Overview, so a database filter is no duplicate; label `Filter Recovery conflicts by Database` (InstancePage tests use exact `'Database'`) |
| 13 | same ~219 | Database sizes → `database-sizes` | `database` | — (`database` is the identity column) | Size | TBL-UI-022 | |
| 14 | same ~236 | SLRU caches → `slru-caches` | cache `name` | — | Read from disk | TBL-UI-023 | |
| 15 | same ~249 | Temporary files → `temporary-files` | `database` | — (`database` is the identity column) | Written | TBL-UI-024 | |
| 16 | `features/instance/LogicalReplicationSection.tsx` ~150 | Subscriptions → `subscriptions` | `name`, `publications`, `slot` | `state` (State: fixed options `disabled`/`not running`/`silent`/`running`, the displayed state; the spec depends on `readAt`: `useMemo([readAt])`), `database` (Database) | Subscription | TBL-UI-025 | filter test covers both selects |
| 17 | same ~164 | Logical slots → `logical-slots` | `slot_name` | — | Decoded | TBL-UI-026 | |
| 18 | `features/queries/QueryListPage.tsx` ~350 (Query inspector, `/instances/:id/queries`) | Statement statistics → `statement-statistics` | query text, queryid | — (`Sort statements` server order stays; D20 server `q` in 12.2) | Calls | TBL-UI-027 | sort buttons enabled on Calls, Total time, Mean time, Rows, Cache hit ratio, WAL, Temporary bytes/call over the loaded rows: each gets a raw `accessorFn` (from `summariseStatement`, `rows`, `wal_bytes`; none has one today, so `getCanSort()` would stay false); `Query` and `Share of listed rows` stay `enableSorting: false` (UI-QRY-016 ~242 asserts its exact header name); changing `Sort statements` (server re-rank) also deletes `statement-statistics.page` (D23: back to page 1) |
| 19 | `features/settings/InventoryTables.tsx` ~205 | Databases for `<addr>:<port>` → `instance-databases-<instance_id>` | `datname`, `skip_reason` | `monitored` (Monitored: fixed options `yes`/`no` labelled `Yes`/`No`, as the cell shows) | Database | TBL-UI-028 | one table per instance; the section lists the instances matching `monitored-instances.q` only (see row 21) |
| 20 | same ~303 | Derived agent inventory → `derived-agent-inventory` | `agent_id`, `address`, `reason` | `state` (State) | Last seen | TBL-UI-029 | |
| 21 | same ~387 | Monitored instances → `monitored-instances` | `instance_id`, `addr`, `cluster_name`, `cluster_id`, `role`, `perm_tier` (the old `instanceMatchesFilter` fields; module constant `MONITORED_INSTANCE_SEARCH`) | `role` (Role), `up` (State: fixed options `up`/`down` labelled `Up`/`Down`, as the cell shows) | Last seen | TBL-UI-030 | the `Filter instances` box (URL `q`) is removed. The DataTable gets the **unfiltered** rows (its toolbar search is `monitored-instances.q`, label `Search Monitored instances`), so a no-match search keeps the toolbar and shows "No rows match" with Clear filters and `(filtered from N)` (I-9); its `emptyState` is only "No monitored instances". "Databases by instance" filters the same rows with `matchesSearch(row, q, MONITORED_INSTANCE_SEARCH)` reading `monitored-instances.q` only (not `f.role`/`f.up`), and shows "No instances match this filter" when the fleet is non-empty and nothing matches. UI-SET-005 changes as phase_10 9.2 states |

## 9.3 — bespoke tables (contract only; 9.3 re-verifies columns)

| Table | File | Prefix | Search | Filters | Notes |
|-------|------|--------|--------|---------|-------|
| Alert rules | `features/alerts/RulesPage.tsx` ~92 | `alert-rules` | rule name, expression/summary | severity, state | |
| Silences | `features/alerts/SilencesPage.tsx` ~174 | `silences` | matchers, comment, creator | active (`active`/`expired`) | |
| Replication topology edges | `components/charts/TopologyGraph.tsx` ~75 | `topology-edges` | upstream/downstream names | role, state | keeps the accessible name and row/cell roles (SYS-UI-001) |
| Relations (tables, indexes, bloat lists) | `features/instance/RelationsSection.tsx` ~194 | `relations-tables`, `relations-indexes`, `relations-table-bloat`, `relations-index-bloat` | relation name, schema | database, schema (no relkind filter: the four lists are already split by kind and the Tables/Indexes items carry no `object_kind`; R-9.3) | `Sort <caption> by <label>` buttons stay and drive `useTableState`; expanded row collapses on page change |
| PostgreSQL settings | `features/instance/SettingsSection.tsx` ~103 | `pg-settings` | name, category | category, restart required | |
| Plan history | `features/queries/PlanHistory.tsx` ~108 | `plan-history` | plan id / hash | source | |
| Command audit | `features/settings/CommandAudit.tsx` ~129 | `command-audit` | command, target | status, kind | keeps `Command audit for …` (SYS-UI-006) |
| Index analysis (Indexes tab) | `features/indexes/IndexAnalysisPage.tsx` ~687 | `indexes` | name, table, schema, definition, reason | state (valid/invalid), kind (unique/primary/plain/partial), advisor flag | search label `Search indexes`; no database filter: the page `Database` select (`?db=`) exists (deviation from the phase_10 9.3 text, STATE §8); IDX-UI-003 as phase_10 9.3 states |
| Cleanup | `features/indexes/CleanupPanel.tsx` | `cleanup` | name, table | reason, basis | |
| Dead rows | `features/indexes/DeadRowsPanel.tsx` | `dead-rows` | table, schema | `show` (all/dead/due/never/flagged) | search label `Search tables`; no database filter (page `Database` select; 9.3 confirms `DeadRowsPanel` follows `?db=`); `compareDeadRows` stays the default order; DEAD-UI-004/005 as phase_10 9.3 states |
| Slowest queries | `features/queries/SlowQueriesPage.tsx` (`/queries`) | `slow` | query text, queryid, database | — (Server, Database, Rank by selects exist) | becomes a `DataTable` (phase_10 9.3 contract) |
