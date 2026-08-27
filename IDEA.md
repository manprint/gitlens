# Postgres Monitor - Documento Idee v0.2

> Architettura Server + Agent per monitoraggio Postgres self-hosted e managed (RDS/Aurora)

---

## 0. Changelog v0.1 -> v0.2

Revisione critica del doc v0.1. Cosa è cambiato:

| # | Area | Cambiamento |
|---|------|-------------|
| 1 | **Identità** | Nuova sez. 2.2. `cluster_id` derivato da `system_identifier` (non da hash `primary_conninfo`, che è instabile). `instance_id` = UUID persistito. |
| 2 | **Multi-database** | Nuova sez. 4.2. Policy connessioni per-database esplicita (era assente: metà delle viste è per-DB). |
| 3 | **Schema TSDB** | Aggiunta label `database` (mancava). Tabella `query_texts` separata. |
| 4 | **Counter reset** | Nuova sez. 4.5. Detection reset via `stats_reset` (era assente = rate sbagliati garantiti). |
| 5 | **Cardinalità** | Nuova sez. 4.6. Budget serie esplicito + top-N lato agent. |
| 6 | **Permessi** | Nuova sez. 4.7. Tier di permessi. `EXPLAIN` e `kill query` NON fattibili con solo `pg_monitor`. |
| 7 | **Staleness** | Alert su assenza dati / agent muto (era assente: il monitor non si monitorava). |
| 8 | **ASH** | Nuova sez. 3.5. Active Session History a 1s = differenziatore principale, assente in v0.1. |
| 9 | **Aurora** | Declassato: `pg_stat_replication` non funziona su Aurora. Topology provider dedicato, decisione aperta. |
| 10 | **pgbouncer** | Aggiunto (era assente; presente in quasi ogni fleet reale). |
| 11 | **Check interface** | Rimossa ridondanza `Query()`/`Scrape()`. Aggiunti `Timeout()`, `Requires()`, override server-side dell'interval. |
| 12 | **Sampling adattivo** | Rimosso (anti-pattern: riduce risoluzione durante l'incidente). Sostituito con budget fisso + circuit breaker. |
| 13 | **Server HA** | Corretto: Alert Engine e Scheduler NON sono stateless. Leader election via advisory lock. |
| 14 | **Enrollment** | Nuova sez. 7.4 (era assente: nessun flusso di bootstrap/revoca token). |
| 15 | **Versioni** | Dichiarate PG 15-18 + matrice viste per versione. |
| 16 | **Testing** | Nuova sez. 12, spostato in Phase 0 (era assente dalla roadmap). |
| 17 | **Limiti noti** | Nuova sez. 11. |
| 18 | **Domande residue** | Da 10 a 3 bloccanti + resto rimandabile. |

---

## 1. Vision / Obiettivo

Sistema leggero, open, self-hostable per monitorare fleet Postgres eterogenea:
- Singola istanza su VM/bare metal (agent locale, binary o Docker)
- Fleet distribuita (N agent)
- DB containerizzati (Docker / Compose / K8s)
- DB managed remoti senza accesso host (RDS, Cloud SQL, Supabase, Neon) -> agent "remote collector"

Requisito trasversale: **sia Server che Agent devono girare nativi o in Docker senza perdita di funzionalità** (host monitoring e discovery inclusi).

**Versioni Postgres supportate: 15 -> 18.** PG 12 è EOL (nov 2024), fuori scope. Ogni check dichiara `MinPG`/`MaxPG` (vedi matrice sez. 3.6).

Obiettivi:
- Zero overhead su DB prod (query leggere, budget fisso, circuit breaker)
- Setup < 5 minuti (script SQL fornito, vedi sez. 7.3)
- Storico metriche + alerting + tuning advisor
- **Wait-event analysis (ASH) a 1s** - il differenziatore principale (sez. 3.5)
- Alternativa a pgwatch2 / Datadog / New Relic ma focalizzata solo su Postgres e DBA-friendly
- **Estensibile day-one:** architettura plugin per aggiungere check/tipologie replica senza refactor core

Non-obiettivi v0.1: APM applicativo generico, log aggregation universale, MySQL/altri engine.

**Principio Estensibilità (Replica-Ready):**
Ogni scelta (modello dati, API agent->server, check interface) deve supportare nativamente `Cluster > Instance > Database` e topologie replica eterogenee (streaming async/sync, cascata, logica, Patroni, Aurora). Non posticipare astrazione cluster: costa poco ora, carissimo dopo. Vedi sez. 2.1, 2.2 e 5.1.

**Nome:** `pglens` — https://github.com/manprint/pglens — nome definitivo scelto; verifica trademark e collisione con `pganalyze` completata.

---

## 2. Architettura High-Level

```
[ Postgres 1 ] --\
[ Postgres 2 ] ---- [ Agent (per host / per collector) ] --push--> [ Server Centrale ] --> [ GUI / API / Alerting ]
[ RDS ]       --/                                                    ^
                                                                     |
                                                             [ Storage: TimescaleDB ]
```

### 2.1 Concetti Core (estensibili da subito)

```
Fleet
 └─ Cluster (unità di replica/failover, identificata da system_identifier)
     ├─ Instance (singolo postmaster, ruolo dinamico: primary/standby)
     │   ├─ Database (postgres, app_db, ...)   <- scope di metà delle metriche
     │   └─ Host/Container (metriche OS/cgroup legate a Instance)
     └─ Topology (grafo: chi replica da chi, sync_state, lag, timeline)
```

- **Cluster è astrazione chiave:** anche singola istanza standalone = cluster di 1 nodo. Così replica non è caso speciale.
- **Instance role non statico:** `pg_is_in_recovery()` determina primary vs standby ad ogni scrape; Server ricostruisce topologia anche dopo failover/switchover.
- **Database è un livello reale, non decorativo:** `pg_stat_statements`, `pg_stat_user_tables`, `pg_statio_*` richiedono una connessione **per database**. Vedi sez. 4.2.
- **Estensibilità:** aggiungere nuova topologia (es. Citus, Patroni) = nuovo `TopologyProvider` + nuovi check, senza cambiare schema base.

### 2.2 Identità & Naming (DECISIONE BLOCCANTE - da fissare prima del primo schema)

*Problema:* v0.1 proponeva `cluster_id` = hash di `primary_conninfo`. **Non funziona:**
- `primary_conninfo` punta spesso a VIP / HAProxy / service DNS, non al primary reale
- cambia dopo failover
- è diverso visto da nodi diversi dello stesso cluster -> due standby dello stesso cluster ottengono due `cluster_id` distinti

**Soluzione: `system_identifier`.**

Tutti i nodi di uno streaming replication cluster condividono lo stesso `system_identifier` (ereditato via `pg_basebackup`/clone). Sopravvive a failover, promote, cambio IP, rename. Un cluster in logical replication ha identifier diverso — che è **corretto**: sono cluster distinti che si scambiano dati.

```sql
SELECT system_identifier FROM pg_control_system();
```

**Attenzione operativa:** `pg_control_system()` è **ristretta a superuser di default e NON è coperta da `pg_monitor`**. Serve grant esplicito:

```sql
GRANT EXECUTE ON FUNCTION pg_control_system() TO monitoring;
```

Su RDS lo esegue `rds_superuser`. Va nello script di setup (sez. 7.3).

**Fallback se il grant non è ottenibile:** `cluster_id` manuale in `agent.yaml` (`cluster_id: pg-prod-eu`). Il Server accetta entrambi; l'identifier ha precedenza quando disponibile. Se un cluster ha nodi con identifier e nodi con id manuale discordanti -> warning `cluster_identity_conflict`.

**Tabella identità:**

| Entità | Chiave stabile | Derivazione | Note |
|--------|----------------|-------------|------|
| `cluster_id` | `system_identifier` (uint64) | `pg_control_system()`, fallback config manuale | stabile a failover/rename/IP change |
| `instance_id` | UUID v4 generato dall'agent, **persistito** | primo avvio: genera e scrive su disco | **richiede volume per agent containerizzato** (vedi sez. 7.2) |
| `database` | `datname` | `pg_database` | label obbligatoria su tutte le metriche per-DB |
| `agent_id` | UUID assegnato dal Server all'enrollment | vedi sez. 7.4 | 1 agent può servire N instance |

**Perché `instance_id` non è `host:port`:** IP dei container cambiano a ogni restart, DHCP, K8s rescheduling, container rename. `host:port` come identità produce istanze fantasma e serie spezzate. L'UUID persistito è l'unica cosa affidabile.

Path identity file: `/var/lib/pglens/identity.json` (override via `IDENTITY_PATH`).

```json
{ "agent_id": "…", "instances": { "<dsn-fingerprint>": "<instance_uuid>" } }
```

Se il file viene perso, l'agent rigenera UUID -> l'istanza appare come nuova. Mitigazione: il Server fa merge su `(cluster_id, inet_server_addr, port)` e propone il collegamento in UI invece di duplicare silenziosamente.

### 2.3 Componenti

**A. Agent (Go - single binary + immagine Docker)**
- Ruolo: collector + esecutore query diagnostiche.
- Modalità:
  1. `local`: gira su stessa macchina di Postgres (binary systemd o container Docker). Metriche OS (CPU, RAM, Disk IO, net) + metriche Postgres via `pgx`.
  2. `local-docker`: variant di `local` quando agent è containerizzato ma deve monitorare host fisico + DB containerizzati sullo stesso host. Richiede mount/bind specifici (vedi sez. 4.9).
  3. `remote`: gira ovunque (accanto al Server, su bastion, come container). Solo metriche Postgres via rete. No metriche OS host DB (limitazione RDS).
  4. `docker-discovery`: agent container che scopre automaticamente DB Postgres containerizzati via Docker Socket / API su stesso host.
- Comunicazione verso Server: **push HTTPS + protobuf** (gRPC opzionale). Buffer locale su disco se Server down (policy sez. 4.8). Auth: mTLS o token bearer + TLS, entrambi emessi dall'enrollment (sez. 7.4).
- Config: YAML + auto-discovery DB. Pull di config dal Server via long-poll (vedi nota NAT sez. 5.2).
- Update: self-update opzionale (binary) o rolling image update (Docker).
- Distribuzione: binario statico + immagine Docker multi-arch (amd64/arm64) < 30MB.

**B. Server Centrale**
- Ingest API per agent (contratto versionato: `agent_payload v1` con `cluster_id`, `instance_id`, `database`, `role`, `topology_edges`, `stats_reset`)
- **Scheduler** (stateful, vedi HA sez. 5.5): decide cosa chiedere a quale agent e ogni quanto. Sovrascrive il `DefaultInterval()` dei check via config push.
- **Topology Engine:** ricostruisce grafo replica da segnali multipli (sez. 5.1), mantiene `cluster_state` storico (failover timeline)
- **Delta Engine:** converte counter cumulativi in rate, con reset detection (sez. 4.5)
- **Extension Registry:** registro check/metriche/plugin versionati (agent dichiara capabilities: `checks: [replication, bloat, ash]`)
- Storage: **TimescaleDB** (metriche + inventario + meta). Vedi sez. 5.
- **Alert Engine** (stateful, cluster-aware, include alert di staleness sez. 5.4)
- UI + API

**C. GUI / Dashboard** - vedi sez. 6.

---

## 3. Cosa Monitorare - Matrice Dati

### 3.1 Livello Postgres (via SQL)

Legenda `Conn`: **S** = vista shared (basta 1 connessione per instance) - **D** = per-database (serve connessione a ogni DB monitorato, vedi 4.2).

| Categoria | Sorgente | Freq | Scope | Conn | Note |
|-----------|----------|------|-------|------|------|
| Attività | `pg_stat_activity`, `pg_stat_database` | 10s | instance | S | connessioni, stati, idle in txn |
| **ASH / Wait events** | `pg_stat_activity` campionata | **1s** | instance | S | aggregato agent-side, vedi 3.5 |
| Query Perf | `pg_stat_statements` | 60s | any role | **D** | top-N, vedi 4.6. Funziona anche su standby |
| Lock & Wait | `pg_locks` + `pg_stat_activity` | 10s | instance | S | deadlock, lock tree. Timeout dedicato (4.4) |
| Replication - Streaming | `pg_stat_replication` (primary), `pg_stat_wal_receiver` / `pg_last_wal_replay_lag()` (standby) | 10s | cluster | S | lag bytes+sec, sync_state, replay/flush/write lag |
| Replication - Slots | `pg_replication_slots` | 15s | cluster | S | active, restart_lsn lag, `wal_status`, type physical/logical |
| Replication - Conflicts | `pg_stat_database_conflicts` | 30s | standby | S | conflitti recovery, query cancellate |
| Replication - Logical | `pg_stat_subscription`, `pg_stat_subscription_stats` (PG15+) | 30s | cluster | S | lag apply, errori worker |
| Replication - Patroni | API REST `/patroni` + DCS | 15s | cluster | - | plugin opzionale |
| Replication - Aurora | `aurora_replica_status()` | 30s | cluster | S | **provider dedicato, vedi 3.7** |
| Bloat & Vacuum | `pg_stat_user_tables`, `pg_stat_progress_vacuum` | vacuum 30s / **bloat 6h** | instance | **D** | vedi nota costo sotto |
| Config & GUC | `pg_settings`, version | 1h | instance | S | drift detection |
| Role | `pg_is_in_recovery()` | 10s | instance | S | role change instant |
| Cache / BGWriter | `pg_stat_bgwriter` (<=16), `pg_stat_checkpointer` (17+) | 30s | instance | S | **vista splittata in PG17** |
| IO | `pg_stat_io` (PG16+) | 30s | instance | S | IO per backend/context/object, no eBPF |
| Index/Table Stats | `pg_stat_user_indexes`, `pg_statio_*` | 5m | instance | **D** | unused indexes, seq scan. Top-N (4.6) |
| WAL | `pg_current_wal_lsn()`, `pg_stat_wal` (PG14+) | 15s | instance | S | WAL generation rate |
| Long Jobs | `pg_stat_progress_*` | 30s | instance | S | VACUUM, CREATE INDEX, CLUSTER, BASEBACKUP |
| **Pooler** | pgbouncer `SHOW POOLS/STATS/CLIENTS` | 15s | instance | - | vedi 3.4 |

**Nota costo bloat:** v0.1 metteva bloat a 5m. Sbagliato. Le due opzioni:
- **Stima statistica** (join su `pg_class`/`pg_attribute`/`pg_stats`): su schema con 5-10k tabelle è una query pesante (secondi). -> **6h, default on**
- **`pgstattuple`**: esatta ma fa full scan della tabella. -> **on-demand only, mai schedulata**, opt-in per singola tabella dalla UI

`pg_stat_progress_vacuum` invece è leggerissimo -> resta a 30s.

**Nota `pg_stat_statements` su standby:** v0.1 lo limitava a "primary/logical". Errato: `pg_stat_statements` funziona anche su standby e traccia le query read-only eseguite lì. Va raccolto ovunque. Il vincolo reale è che **`pg_stat_statements_reset()` non è eseguibile su standby** (read-only) - irrilevante, non facciamo reset (vedi 4.5).

### 3.2 Livello OS (mode `local` e `local-docker`)
- CPU, Load, RAM, Swap, Disk space + IOPS/latency (via `/proc`, `gopsutil`), Network
- cgroup v1 e v2 per container
- Logs Postgres (csvlog / jsonlog parsing) - Phase 2, opzionale
- **Nota Docker:** quando agent gira in container, metriche host raccolte via bind-mount `/proc:/host/proc:ro`, `/sys:/host/sys:ro`, `/:/host/root:ro`. Agent astrae path con `HOST_PROC` fallback automatico. Mount read-only preferito a `--privileged`.

### 3.3 Livello Container / Docker
- Per ogni container Postgres scoperto: CPU throttling, mem limit/usage, restart count, image version, volume usage (via Docker Engine API + cgroup)
- Correlazione host <-> container ("DB container X satura disco host Y")
- Richiede `/var/run/docker.sock:ro` (o Docker TCP / Podman socket). Opzionale, disabilitabile per hardening.

### 3.4 Livello Pooler (pgbouncer) - NUOVO

*Perché serve:* quasi ogni fleet Postgres reale ha un pooler davanti. Ignorarlo rende i dati fuorvianti:
- `pg_stat_activity.client_addr` mostra l'IP del **pooler**, non dell'applicazione
- `application_name` è quello impostato dal pooler
- `pg_stat_activity` in transaction pooling non mostra i client in attesa nella coda del pooler -> **il DB sembra idle mentre l'app è bloccata**

Il collo di bottiglia più comune in produzione è la coda pgbouncer, e in v0.1 era invisibile.

**Raccolta:** connessione al database amministrativo `pgbouncer` con utente `stats_users`.

| Comando | Metriche chiave |
|---------|-----------------|
| `SHOW POOLS` | `cl_waiting` (client in coda - **la metrica critica**), `sv_active`, `sv_idle`, `maxwait`, `maxwait_us` |
| `SHOW STATS` | `total_xact_count`, `total_query_time`, `total_wait_time` |
| `SHOW DATABASES` | `pool_size`, `current_connections`, `pool_mode` |
| `SHOW CLIENTS` / `SHOW SERVERS` | on-demand, per drill-down |

Alert day-one: `pgbouncer_maxwait > 5s`, `cl_waiting > 0 for 1m`, `sv_active == pool_size for 2m`.

Il protocollo admin di pgbouncer non è SQL standard (risposte non conformi) - usare connessione `simple query protocol`, non extended. Va testato: alcuni driver Go richiedono `default_query_exec_mode=simple_protocol` in pgx.

Supporto v0.1: pgbouncer. Pgcat/odyssey: Phase 2 (interfacce simili ma non identiche).

### 3.5 ASH - Active Session History (DIFFERENZIATORE) - NUOVO

*Il buco di v0.1:* campionare `pg_stat_activity` ogni 5-15s perde tutte le query brevi e rende inutile l'analisi wait-event. È esattamente ciò che pgwatch2 e PMM fanno male, ed è la prima domanda di ogni DBA in un incidente: **"dove sta passando il tempo il database, adesso?"**

**Design:**

```sql
-- campionata ogni 1s
SELECT pid, state, wait_event_type, wait_event, query_id, datname, usename,
       application_name, backend_type, xact_start, query_start
FROM pg_stat_activity
WHERE backend_type = 'client backend'
  AND state <> 'idle'
  AND pid <> pg_backend_pid();
```

- **Frequenza 1s**, query leggerissima (vista in memoria, nessun lock, nessun IO)
- **Aggregazione lato agent** su finestra di 10s: conteggio campioni per `(wait_event_type, wait_event, state, query_id, datname)`
- Push del solo **aggregato**, mai i campioni raw -> cardinalità controllata (vedi 4.6)
- `query_id` disponibile in `pg_stat_activity` da PG14 — su tutte le versioni supportate (15→18) è disponibile incondizionatamente con `compute_query_id = on|auto` e collega direttamente l'ASH a `pg_stat_statements`.

**Perché è il nostro angle:**
- `pg_wait_sampling` (l'alternativa esistente) richiede `shared_preload_libraries` -> **non installabile su RDS, né su nessun managed**, e richiede restart altrove
- Il nostro approccio è puro SQL: **funziona ovunque, RDS incluso, zero restart, zero extension**
- Abilita la visualizzazione che i DBA vogliono: stacked area "tempo DB per wait event" + drill-down a query

**Limiti da dichiarare (sez. 11):** è un metodo **statistico**, non un tracciamento esatto. Query più brevi di ~1s sono sottorappresentate. Con < 60 campioni (1 min) i numeri non sono significativi. Stesso trade-off di Oracle ASH e AWS Performance Insights - accettabile e standard, purché scritto in UI.

**Costo:** 1 query/s per instance. Su 50 instance = 50 query/s totali distribuite. Trascurabile. Configurabile a 5s per istanze sensibili, disattivabile.

### 3.6 Matrice viste per versione PG

Ogni check dichiara `MinPG`/`MaxPG`. Le differenze che contano nel range 15-18:

| Vista / feature | Disponibile da | Nota |
|---|---|---|
| `pg_stat_statements.total_exec_time` | 13 (disponibile su tutte le versioni supportate 15→18) | in PG12 era `total_time` - fuori scope |
| `pg_stat_wal` | 14 | WAL generation stats |
| `query_id` in `pg_stat_activity` / `pg_stat_statements` in core | 14 | richiede `compute_query_id = on\|auto`; prima era solo nell'extension |
| `pg_stat_statements_info.stats_reset` | 14 | **necessario per reset detection (4.5)** |
| `pg_read_all_data` (ruolo) | 14 | abilita tier EXPLAIN (4.7) |
| `pg_stat_subscription_stats` | 15 | errori apply logical |
| `pg_stat_io` | 16 | IO dettagliato senza eBPF |
| `pg_stat_checkpointer` | 17 | **`pg_stat_bgwriter` splittato**: colonne checkpoint migrate qui |
| `pg_stat_progress_*` varianti | 15-17 | `COPY` da 15 (supportato da 14), `BASEBACKUP` da 13 (supportato) |

Da PG15 `query_id` in core, `pg_stat_statements_info.stats_reset` e `pg_read_all_data` sono disponibili incondizionatamente su tutte le versioni supportate.

### 3.7 Livello Cloud (RDS / Aurora)

**RDS Postgres** - supportato v0.1:
- `pg_monitor` disponibile, la maggior parte dei check funziona
- Nessun accesso OS -> le metriche host arrivano solo da CloudWatch / Enhanced Monitoring
- `pg_ls_waldir()` e funzioni filesystem bloccate -> alcuni check WAL degradano
- Nessun superuser; il grant su `pg_control_system()` lo fa `rds_superuser`

**CloudWatch - correzione a v0.1:** il doc diceva "10s". Sbagliato per due motivi:
1. Le metriche CloudWatch **standard hanno granularità 60s** - fare polling a 10s non produce nessun dato aggiuntivo
2. `GetMetricData` è **a pagamento per metrica richiesta** -> polling a 10s moltiplica per 6 un costo inutile

-> **Intervallo CloudWatch: 60s.** Documentare la stima di costo in UI prima di abilitare. Per l'OS di RDS l'origine corretta non è CloudWatch base ma **Enhanced Monitoring** (granularità fino a 1s, costo separato) o **Performance Insights**.

**Aurora - DECLASSATO, decisione aperta:**

v0.1 citava Aurora nella prima riga della vision e lo metteva in Phase 2. Incoerente, e soprattutto **il Topology Engine di sez. 5.1 non funziona su Aurora**:
- Aurora non fa streaming replication: usa storage condiviso distribuito
- `pg_stat_replication` sul writer è vuoto nell'uso normale
- Non esistono slot fisici né lag WAL nel senso classico
- `pg_is_in_recovery()` = true sui reader, ma non sono standby streaming
- Il lag reale (millisecondi, non byte) si legge solo da `aurora_replica_status()`

-> **Aurora in v0.1 = metriche instance-level soltanto** (attività, query, lock, ASH funzionano tutti). Topologia e replica: `TopologyProvider` dedicato in Phase 2.
-> **Se Aurora è il target primario reale**, va promosso a Phase 0 e il provider va progettato in parallelo a quello streaming, non dopo. **Vedi domanda residua #2.**

---

## 4. Agent - Design Dettaglio

### 4.1 Plugin Architecture (interfaccia corretta)

v0.1 aveva sia `Query() (string, []any)` che `Scrape(conn)` - ridondanti. `Scrape` copre tutto. Inoltre `Interval()` era fisso nel check, in contraddizione con lo Scheduler server-side di sez. 2.

```go
type Check interface {
    Name() string                          // es. "replication_streaming"
    Scope() Scope                          // Instance | Cluster | Database
    Requires() Requirements                // ruolo, versione, extension, permessi, conn scope
    DefaultInterval() time.Duration        // il Server può sovrascriverlo (config push)
    Timeout() time.Duration                // per-check, NON globale (vedi 4.4)
    Scrape(ctx context.Context, t Target) (Result, error)
}

type Requirements struct {
    Roles      []Role     // Primary | Standby | Any
    MinPG      int        // formato server_version_num, es. 160000
    MaxPG      int        // 0 = nessun limite
    Extensions []string   // es. ["pg_stat_statements"]
    PermTier   PermTier   // ReadOnly | Explain | Signal | Extension (sez. 4.7)
    ConnScope  ConnScope  // Shared | PerDatabase (sez. 4.2)
}

type Result struct {
    Metrics    []Metric
    StatsReset *time.Time  // per reset detection, nil se non applicabile (sez. 4.5)
    Truncated  bool        // true se top-N ha tagliato righe (sez. 4.6)
}

// Registry: map[string]Check - nuovo check = registra, zero modifica core
```

**Risoluzione del conflitto Scheduler:** il check dichiara `DefaultInterval()`; il Server invia una `CheckPolicy{name, interval, enabled, topN}` nel config push; l'agent applica l'override. Interval effettivo = `policy.interval ?? check.DefaultInterval()`.

**Check v0.1:** `activity`, `ash`, `database_stats`, `stat_statements`, `locks`, `replication_streaming`, `replication_slots`, `replication_conflicts`, `wal`, `bgwriter`, `stat_io`, `settings`, `table_stats`, `index_stats`, `progress`, `pgbouncer`.
**Opzionali/plugin:** `bloat`, `patroni`, `aurora`, `cloudwatch`.

**Role-aware:** `Requires().Roles` filtra automaticamente in base a `pg_is_in_recovery()`, rivalutato ogni 10s.
**Version-aware:** `MinPG`/`MaxPG` come da matrice 3.6.
**Nessun `CREATE EXTENSION` obbligatorio**, ma sfrutta se presenti: `pg_stat_statements`, `pg_buffercache`, `pg_stat_kcache`, `hypopg`, `pg_qualstats`.

### 4.2 Policy connessioni multi-database (BLOCCANTE - assente in v0.1)

*Il problema ignorato:* un postmaster ospita N database. `pg_stat_statements`, `pg_stat_user_tables`, `pg_stat_user_indexes`, `pg_statio_*` restituiscono **solo i dati del database a cui sei connesso**. Per coprirli tutti serve una connessione per database.

Aritmetica: 50 instance × 8 database = **400 connessioni permanenti** solo per il monitoring. Su RDS con `max_connections` basso e un pooler già saturo, è un problema serio - il monitor diventa la causa dell'incidente.

**Policy adottata:**

| Aspetto | Scelta |
|---|---|
| Selezione DB | `include`/`exclude` regex in config. Default: esclude `template0`, `template1`, `rdsadmin`, `postgres` se vuoto |
| Tetto | `max_databases_per_instance` **default 10**. Oltre: warning + monitora i 10 più attivi per `xact_commit` |
| Modalità connessione | **connect-on-demand con pool a TTL**: apre la connessione al check per-DB, la tiene `idle_timeout` (default 5m), poi chiude |
| Connessione shared | **1 connessione persistente** per instance sul DB di manutenzione, usata da tutti i check `ConnScope: Shared` (che sono la maggioranza) |
| Budget | `max_connections_per_instance` default 3 (1 shared + 2 rotanti per-DB) |
| `application_name` | sempre `pglens-agent/<check>` - così è riconoscibile in `pg_stat_activity` e escludibile |

**Degradazione esplicita:** se il tetto viene raggiunto, l'agent emette `check_skipped{reason="db_budget"}` e la UI mostra "N database non monitorati" invece di far finta di coprire tutto.

**Alternativa v0.1 minima:** se questo è troppo per Phase 0, si può partire con **solo le viste shared** (attività, replica, WAL, ASH, settings, bgwriter, io) e aggiungere il per-database in Phase 1. Perdi query perf e table stats. Va deciso, non lasciato implicito.

### 4.3 Budget e circuit breaker (sostituisce il "sampling adattivo")

v0.1 proponeva: "se DB sotto carico, riduce frequenza / salta check pesanti". **Anti-pattern**: riduce la risoluzione proprio durante l'incidente, cioè quando i dati servono. Il risultato tipico è un buco nei grafici esattamente nel momento da analizzare.

**Sostituzione:**
- **Budget fisso e piccolo, sempre.** I check sono dimensionati per essere sostenibili al 100% del carico. Se un check non è sostenibile sotto carico, non deve essere schedulato affatto (-> on-demand).
- **Circuit breaker per errore, non per carico:** dopo 3 timeout/errori consecutivi su un check, l'agent lo sospende per `backoff` (esponenziale, max 15m) ed emette `check_error_total{check,reason}`. Riprende automaticamente.
- **Nessuna riduzione di frequenza basata su `active_connections`.**
- I check costosi (bloat esatto, EXPLAIN, `pgstattuple`) sono **on-demand**, mai nella pipeline periodica.

### 4.4 Timeout per-check

v0.1: `statement_timeout = 2s` uniforme. Problema: durante un lock storm, la query su `pg_locks` è lenta ed è **esattamente il dato che serve** -> con 2s uniformi la butti via quando conta.

| Check | `statement_timeout` |
|---|---|
| `ash` | 500ms (deve essere velocissimo o è inutile) |
| `activity`, `replication_*`, `wal`, `settings` | 2s |
| `locks` | **10s** (deve sopravvivere all'incidente che sta misurando) |
| `stat_statements`, `table_stats`, `index_stats` | 15s |
| `bloat` (stima) | 120s |
| on-demand (EXPLAIN, pgstattuple) | 300s, con cancel dall'UI |

Impostato per sessione: `SET LOCAL statement_timeout` + `SET LOCAL lock_timeout = 1s` (mai bloccarsi su un lock) + `SET LOCAL idle_in_transaction_session_timeout = 30s`.

### 4.5 Counter reset detection (assente in v0.1 - bug garantito)

Tutte le viste `pg_stat_*` espongono **counter cumulativi monotoni**. Si azzerano su:
- restart di Postgres (crash o pianificato)
- `pg_stat_reset()` / `pg_stat_statements_reset()` lanciati da un altro tool o da un DBA
- eviction dalla hash table di `pg_stat_statements` quando si supera `pg_stat_statements.max` (la query sparisce e può ricomparire da zero)
- failover/promote

Senza detection: rate negativi, o picchi enormi al riavvio, o - peggio - grafici plausibili ma sbagliati.

**Meccanismo:**

| Sorgente | Segnale di reset |
|---|---|
| `pg_stat_database` | colonna `stats_reset` |
| `pg_stat_statements` | `pg_stat_statements_info.stats_reset` (PG14+) |
| `pg_stat_bgwriter` / `pg_stat_checkpointer` | `stats_reset` |
| `pg_stat_wal`, `pg_stat_io` | `stats_reset` |
| ovunque (fallback) | `pg_postmaster_start_time()` cambiato |
| ultima difesa | `valore_nuovo < valore_precedente` -> reset implicito |

L'agent include `StatsReset` in ogni `Result`. Il Delta Engine del Server:
```
se stats_reset(t) != stats_reset(t-1)  -> scarta il delta, riparte dal nuovo baseline (nessun punto emesso)
se valore(t) < valore(t-1)             -> stesso trattamento + emette evento counter_reset_detected
altrimenti                             -> rate = (v(t) - v(t-1)) / (ts(t) - ts(t-1))
```

Un evento `counter_reset_detected` è visibile in timeline UI: spesso è il primo indizio di un restart non pianificato.

### 4.6 Cardinalità - budget serie esplicito (assente in v0.1)

v0.1 dimensionava TimescaleDB ("10K-50K samples/sec") **senza contare `pg_stat_statements`**, che è di gran lunga la sorgente dominante. Una singola istanza può avere 5.000 `queryid` distinti.

**Selezione top-N lato agent (obbligatoria):**

```sql
-- unione dei top-N per due criteri diversi, altrimenti si perdono
-- le query "tante e veloci" (che spesso sono il vero problema)
(SELECT ... ORDER BY total_exec_time DESC LIMIT :n)
UNION
(SELECT ... ORDER BY calls DESC LIMIT :n)
```

Default `n = 50` -> ~75 righe distinte per database. Configurabile per policy dal Server.

**Budget serie (target 50 instance × 8 DB monitorati = 400 database):**

| Sorgente | Serie per unità | Totale | Interval | Samples/s |
|---|---|---|---|---|
| `pg_stat_statements` | ~75 query × 6 metriche = 450 / DB | 180.000 | 60s | 3.000 |
| `table_stats` (top-100 tabelle) | 100 × 6 = 600 / DB | 240.000 | 5m | 800 |
| `index_stats` (top-100 indici) | 100 × 3 = 300 / DB | 120.000 | 5m | 400 |
| ASH aggregato | ~40 combinazioni wait × instance | 2.000 | 10s | 200 |
| attività / database / WAL / bgwriter / io | ~120 / instance | 6.000 | 10-30s | 400 |
| replica / slot | ~15 / edge | ~1.500 | 10s | 150 |
| OS + container | ~80 / host | 4.000 | 10s | 400 |
| pgbouncer | ~30 / pooler | 1.500 | 15s | 100 |
| **Totale** | | **~555.000 serie** | | **~5.500 samples/s** |

Rientra ampiamente nel dimensionamento TimescaleDB di sez. 5 - **ma solo con il top-N attivo**. Senza, `pg_stat_statements` da solo produrrebbe ~12M di serie.

**Guardrail:**
- `max_series_per_instance` (default 20.000): superato -> l'agent tronca, imposta `Truncated: true`, il Server alza `cardinality_budget_exceeded`
- Metrica `pglens_series_total{cluster,instance}` esposta come metrica di sistema
- **Problema del churn top-N:** le query che entrano/escono dal top-N producono serie intermittenti. Mitigazione: isteresi (una query resta nel set per 5 cicli dopo essere uscita dal top-N) + UI che disegna i gap come gap, non come zeri.

**Testo delle query - fuori dal TSDB:**

Il testo di `pg_stat_statements` può essere di KB. Non va nelle metriche. Tabella relazionale dedicata, deduplicata:

```sql
CREATE TABLE query_texts (
  cluster_id   bigint,
  database     text,
  queryid      bigint,
  query_text   text,
  pg_major     int,
  first_seen   timestamptz,
  last_seen    timestamptz,
  PRIMARY KEY (cluster_id, database, queryid, pg_major)
);
```

L'agent invia il testo **solo la prima volta** che vede un `queryid` (cache locale LRU); poi solo l'id.

**Limite di `queryid` da dichiarare in UI:** l'hash è calcolato sul parse tree e **include gli OID degli oggetti**. Conseguenze:
- **Confrontabile** tra primary e le sue repliche fisiche (stessi OID: la replica è un clone) -> il confronto primary/standby promesso funziona
- **NON confrontabile** tra cluster diversi (stessa query, OID diversi -> queryid diverso)
- **NON confrontabile** tra major version diverse (l'algoritmo è cambiato)

Sez. 6 prometteva "compare query trend tra 2 istanze" senza qualificarlo. La UI deve disabilitare il confronto cross-cluster per `queryid` e offrire invece il match per normalizzazione del testo (fuzzy, Phase 2).

### 4.7 Tier di permessi (contraddizione risolta)

v0.1 dichiarava utente `monitoring` con `pg_monitor`, **solo lettura**, e poi prometteva "kill query button", "EXPLAIN on-demand" e "hypopg". Non compatibili.

| Tier | Grant | Sblocca | Default |
|---|---|---|---|
| **T0 - read-only** | `pg_monitor` + `GRANT EXECUTE ON FUNCTION pg_control_system()` | tutte le metriche, ASH, topologia, replica, advisor statistico | **on** |
| **T1 - explain** | `pg_read_all_data` (PG14+) *oppure* `GRANT SELECT` mirati sugli schemi | `EXPLAIN` (senza ANALYZE) on-demand | off |
| **T2 - signal** | `pg_signal_backend` | cancel / terminate query dalla UI | off |
| **T3 - extension** | `CREATE EXTENSION hypopg`, `pg_buffercache`, `pg_stat_kcache` | advisor con simulazione indici, cache analysis | off |

**Perché serve:**
- `pg_terminate_backend()` / `pg_cancel_backend()` su backend di **altri utenti** richiede `pg_signal_backend`, che **non è incluso in `pg_monitor`**. Senza, il bottone kill fallisce sempre.
- `EXPLAIN` richiede il privilegio **SELECT sulle tabelle coinvolte**. `pg_monitor` non lo dà -> `permission denied` nel ~100% dei casi reali. È il tipo di bug che si scopre solo alla demo.

**Sicurezza `EXPLAIN ANALYZE`:** **esegue realmente la query**. Regole non negoziabili:
- Mai automatico, mai schedulato
- Conferma esplicita in UI, con avviso testuale
- **Bloccato su qualsiasi statement non-SELECT** (INSERT/UPDATE/DELETE/DDL) - il parse lato server rifiuta
- Timeout obbligatorio, cancellabile
- `EXPLAIN` semplice (piano stimato) resta sempre disponibile con T1 ed è sicuro

La UI mostra il tier attivo per istanza e disabilita (con tooltip "richiede tier T2") le azioni non concesse, invece di farle fallire.

### 4.8 Buffer locale e politica di drop (non specificata in v0.1)

| Parametro | Default | Nota |
|---|---|---|
| Storage | file WAL-like su disco, `/var/lib/pglens/buffer` | sopravvive a restart agent |
| `buffer_max_size` | 512 MB | |
| `buffer_max_age` | 6h | |
| Politica a esaurimento | **drop oldest** | le metriche recenti valgono più delle vecchie; e il drop è contato |
| Comportamento a disco pieno | stop scrittura buffer, continua a servire i check live, alza `buffer_full` | mai riempire il disco del DB host |
| Metrica | `agent_buffer_bytes`, `agent_samples_dropped_total` | visibile in UI - un drop silenzioso è peggio del drop |

**Rifiuto server-side:** il Server scarta i sample con `ts` più vecchio di `max_sample_age` (default 12h) -> `sample_too_old_total`.

**Collisione con la compressione TimescaleDB:** l'inserimento out-of-order in chunk già compressi ha un costo non banale. Mitigazione: **`compress_after` (48h) deve essere maggiore di `max_sample_age`** - così il backfill atterra sempre in chunk non compressi. Da verificare sperimentalmente in Phase 0.

**Clock skew:** il timestamp è **quello dell'agent** (l'unico corretto per i dati bufferizzati). Requisito NTP documentato. Il Server calcola `agent_clock_skew_seconds` confrontando `ts` del payload con l'ora di ricezione e alza un warning oltre 30s - lo skew silenzioso produce grafici incomprensibili.

### 4.9 Agent in Docker - Dettaglio Tecnico

**1. Monitoraggio Host da Container (`local-docker`):**
```yaml
agent:
  image: ghcr.io/manprint/pglens-agent:latest
  environment:
    - HOST_PROC=/host/proc
    - HOST_SYS=/host/sys
  volumes:
    - /proc:/host/proc:ro
    - /sys:/host/sys:ro
    - /:/host/root:ro                                 # disk usage host
    - /var/run/docker.sock:/var/run/docker.sock:ro    # opzionale, container stats
    - agent-data:/var/lib/pglens            # OBBLIGATORIO: identity + buffer (sez. 2.2, 4.8)
volumes:
  agent-data:
```
- Agent detecta `/host/proc` -> usa quello per `gopsutil`. Fallback a `/proc` se nativo.
- Disk: risolve mount host via `/host/proc/mounts` e stat su `/host/root`.
- Sicurezza: no `privileged` di default; `CAP_DAC_READ_SEARCH` se serve. `pid: host` solo se serve la vista processi completa.
- **Il volume `agent-data` non è opzionale:** senza, ad ogni ricreazione del container l'agent perde `instance_id` e le istanze si duplicano.

**2. Monitoraggio DB Dockerizzati (Discovery):**
- **Esplicito (raccomandato prod):**
  ```yaml
  targets:
    - name: pg-app
      dsn: postgres://monitoring@pg-app:5432/postgres?sslmode=require
      databases: { include: ["app_.*"], exclude: ["template.*"] }
  ```
- **Auto-discovery (dev):** `GET /containers/json` via docker.sock, filtro per label `pglens.monitor=true`, image `postgres:*`/`timescale/*`/`bitnami/postgresql*`, env `POSTGRES_DB`, porta 5432. Connessione via `NetworkSettings.Networks.*.IPAddress` o container name.
- **Compose/K8s:** agent come sidecar o DaemonSet nella stessa network -> discovery zero-config via service DNS.
- **Discovery replica-aware:** dopo la connessione, `cluster_id` viene da `system_identifier` (sez. 2.2) - il raggruppamento in cluster è automatico e corretto anche in discovery.

---

## 5. Server Centrale - Design

### 5.0 Stack

| Layer | Scelta | Perché per questo scale |
|-------|--------|-------------------------|
| **Agent + Server** | **Go 1.22+** | single binary <20MB, <30MB RAM, `pgx`/`gopsutil`/`docker SDK` maturi, build multi-arch, interfaccia `Check` estensibile |
| **Storage** | **TimescaleDB (Postgres 16+ / 17)** | ~5.5K samples/s a target (vedi 4.6), gestibile con hypertable + compressione. Unico DB SQL: query replica-aware complesse in SQL, nessuno stack VictoriaMetrics+ClickHouse da operare. Migrabile a VictoriaMetrics oltre i 500 DB senza cambiare l'API |
| **API Server** | Go `net/http` + `chi` + `pgx` + `sqlc` | zero framework pesante, query type-safe |
| **Coda interna** | Go channels + buffer WAL su agent | per 100 DB non serve Kafka/NATS day-one |

*Validato per 10-50 (max 100) DB: TimescaleDB resta sotto 100GB con 30gg retention raw + compressione, **a condizione che il top-N di 4.6 sia attivo**.*

### 5.1 Topology Engine

*Problema:* la replica non è una metrica ma un grafo dinamico che cambia a ogni failover.

**Sorgenti (agent raccoglie, server fonde):**
1. `system_identifier` -> **appartenenza al cluster** (sez. 2.2)
2. `pg_is_in_recovery()` -> ruolo
3. `pg_stat_replication` (primary -> lista standby: `application_name`, `client_addr`, `sync_state`, `write/flush/replay_lag`)
4. `pg_stat_wal_receiver` (standby -> primary a cui è connesso: `conninfo`, `status`, `latest_end_lsn`)
5. `pg_replication_slots` (slot attivi/inattivi, `active_pid`, `wal_status`)
6. `timeline` da `pg_control_checkpoint()` (stesso vincolo di grant di `pg_control_system()`)
7. Provider esterni opzionali: Patroni DCS, repmgr, **Aurora (`aurora_replica_status()`, modello diverso - vedi 3.7)**

**Fusione:**
```go
type Instance struct {
    ID        string   // UUID persistito (2.2)
    ClusterID uint64   // system_identifier
    Addr      string
    Role      Role
    Timeline  int
    ParentID  *string
}
type Edge struct {
    From, To  string
    Type      string   // "streaming" | "logical" | "aurora" | "citus"
    SyncState string
    LagBytes  float64
    LagSec    float64
}
```
Ogni scrape: upsert instance + edge. `pg_is_in_recovery` che cambia -> evento failover.

**Detection:**
- **cascata**: standby che è anche sorgente per altri
- **orphan**: standby senza primary visibile
- **split-brain**: due primary con lo stesso `system_identifier`
- **timeline divergence**: timeline diverse nello stesso cluster

Il matching primary<->standby usa `application_name` (impostabile via `primary_conninfo`) con fallback su `client_addr`. Se nessuno dei due è affidabile (NAT, proxy), l'edge è marcato `confidence: low` in UI invece di essere inventato.

### 5.2 Ingest

- Endpoint `POST /api/v1/push` (agent push). Payload protobuf versionato.
- **Correzione a v0.1:** il doc citava "Server pull mode per agent dietro NAT". Contraddittorio - se l'agent è dietro NAT il Server **non può** raggiungerlo. L'unico meccanismo valido è **agent-initiated**: l'agent apre una connessione persistente (long-poll su `GET /api/v1/config/stream` o WebSocket) e riceve comandi e config su quel canale. Non esiste "pull mode" lato server; esiste solo il push dell'agent più un canale di comando in senso inverso sulla stessa connessione uscente.
- Payload: `agent_id`, `cluster_id`, `instance_id`, `database`, `role`, `pg_version`, `capabilities[]`, `topology_edges[]`, `stats_reset`, `truncated`.

### 5.3 Schema TSDB

**Correzione a v0.1: la label `database` mancava.** Metà delle metriche è per-database; senza quella label sono inutilizzabili.

```
-- label set obbligatorio
{cluster_id, instance_id, database?, ...}

pg_replication_lag_bytes{cluster_id="7381…", instance_id="a1b2…", upstream="c3d4…"} 1234
pg_replication_slot_lag_bytes{cluster_id="7381…", slot_name="standby_2", slot_type="physical"} 524288000
pg_statements_total_exec_time_ms{cluster_id="7381…", instance_id="a1b2…", database="app", queryid="-4821…"} 91234
pg_ash_samples{cluster_id="7381…", instance_id="a1b2…", database="app", wait_event_type="LWLock", wait_event="WALWrite"} 37
pgbouncer_cl_waiting{cluster_id="7381…", instance_id="a1b2…", pool="app", user="app_rw"} 12
```

```sql
-- meta relazionali (non TSDB)
CREATE TABLE clusters        (cluster_id bigint PRIMARY KEY, name text, source text);  -- source: 'system_identifier'|'manual'
CREATE TABLE instances       (instance_id uuid PRIMARY KEY, cluster_id bigint, addr text,
                              agent_id uuid, pg_version int, perm_tier text, last_seen timestamptz);
CREATE TABLE databases       (instance_id uuid, datname text, monitored bool, PRIMARY KEY (instance_id, datname));
CREATE TABLE query_texts     (…);                                     -- vedi 4.6
CREATE TABLE topology_snapshots (cluster_id bigint, ts timestamptz, graph jsonb);
CREATE TABLE failover_events (cluster_id bigint, ts timestamptz, old_primary uuid, new_primary uuid, reason text);
CREATE TABLE counter_resets  (instance_id uuid, ts timestamptz, source text);   -- vedi 4.5
```

**Estensibilità senza migrazione breaking:**
- Nuova topologia (Citus) = nuovo `TopologyProvider` che emette `Edge{Type:"citus"}`; la UI filtra per `type`
- Sharding: `cluster_id` resta, si aggiunge label opzionale `shard_id`
- Logical replication: già modellata come `Edge{Type:"logical"}` + `pg_stat_subscription`

### 5.4 Alerting (cluster-aware + staleness)

**Correzione a v0.1: mancavano gli alert sull'assenza di dati** - che sono i più importanti. Un monitor che non si accorge di essere cieco fallisce esattamente durante l'incidente.

**Tier 0 - il monitor monitora sé stesso (obbligatorio, non disattivabile):**

| Alert | Condizione |
|---|---|
| `agent_down` | nessun payload da `agent_id` da > 3 × interval (default 90s) |
| `instance_unreachable` | agent vivo ma connessione al DB fallita da > 2m |
| `check_failing` | `check_error_total` in crescita su un check da > 5m |
| `no_primary_in_cluster` | cluster senza alcuna instance con role=primary da > 1m |
| `agent_buffer_full` / `samples_dropped` | l'agent sta perdendo dati |
| `clock_skew` | skew > 30s |
| `cardinality_budget_exceeded` | serie troncate |

Metriche di supporto: `up{cluster_id,instance_id}` (0/1), `agent_last_seen_seconds`, `check_last_success_seconds`.

**Tier 1 - alert Postgres, con scope `instance` vs `cluster`:**
- per-replica: `lag > 30s for 2m`
- aggregati di cluster: `count(standby with lag>30s) == count(standby)` (tutte laggano), `sync_standby_count == 0` (nessuna sync replica disponibile - rischio di blocco delle scritture)
- eventi replica come alert impliciti: `failover_detected`, `split_brain_detected`, `orphan_standby`, `slot_inactive > 10m`, `wal_status = lost`, `timeline_divergence`
- pooler: `pgbouncer_maxwait > 5s`, `cl_waiting > 0 for 1m`
- classici: `connections > 80% max_connections`, `idle_in_transaction > 5m`, `xid_age > 1e9` (wraparound), `disk_free < 15%`, `deadlocks rate`

Canali: Email, Slack, PagerDuty, Webhook.
Silenziamento per maintenance window e per `cluster_id` (es. durante uno switchover pianificato).

### 5.5 Alta disponibilità (correzione a v0.1)

v0.1 affermava "Server stateless, scalabile orizzontalmente". **Falso**: l'Alert Engine ha stato (quali alert sono firing, da quanto, silences, dedup) e lo Scheduler pure. Due repliche senza coordinamento = notifiche duplicate e policy in conflitto.

**Modello corretto:**

| Componente | Stateless? | Scaling |
|---|---|---|
| Ingest API | **sì** | N repliche dietro LB |
| Query API / GUI backend | **sì** | N repliche |
| Delta Engine | quasi (cache dell'ultimo valore) | N repliche, cache warm-up dal DB |
| **Scheduler** | **no** | **singolo leader** |
| **Alert Engine** | **no** | **singolo leader** |

**Leader election** senza dipendenze aggiuntive (no etcd/Consul), usando il Postgres che c'è già:

```sql
SELECT pg_try_advisory_lock(hashtext('pglens:alert-engine'));
```
Lock di sessione: se il processo muore la connessione cade e il lock si libera automaticamente. Heartbeat + rinuncia esplicita allo shutdown. Lo stato firing degli alert è comunque persistito in tabella, così il nuovo leader riprende senza rinotificare (dedup su `alert_key + started_at`).

### 5.6 Advisor / Analyze (replica-aware)

- Indici inutilizzati / duplicati - confronto primary vs replica (dovrebbero avere gli stessi indici; una divergenza è di per sé un finding)
- Query lente: pattern seq scan, missing index. La simulazione via `hypopg` richiede **tier T3** (4.7) - se assente, l'advisor degrada a euristiche su `pg_stat_user_tables`
- Config drift: `work_mem`, `shared_buffers` vs RAM; **alert su incoerenze primary/standby** (`max_standby_streaming_delay`, `hot_standby_feedback` off con query lunghe su standby)
- Vacuum/freeze: `xid_age` e wraparound risk
- Replica-specific: `wal_keep_size` troppo basso vs lag osservato, `max_replication_slots` saturi, `checkpoint_timeout` vs WAL generation rate sul primary, slot inattivi che trattengono WAL
- Pooler: `pool_size` vs `cl_waiting`, `pool_mode` incoerente con l'uso di prepared statement

---

## 6. Interfaccia Grafica (GUI)

**Stack:**

| Layer | Scelta | Perché |
|-------|--------|--------|
| **Framework** | Next.js 15 (App Router) + TypeScript 5 + React 19 | SSR/SSG ibrido, server components per dashboard pesanti |
| **UI Kit** | Tailwind CSS + shadcn/ui + Radix | componenti copiati, customizzabili, ottimi per tabelle dense e dark mode |
| **Grafici** | Apache ECharts + uPlot per sparkline | brush/zoom, dataZoom, 10K punti senza lag, heatmap per ASH/lock |
| **State/Data** | TanStack Query v5 + Zustand | cache, polling, dedup per live metrics |
| **Tabelle** | TanStack Table v8 | virtualizzazione per `pg_stat_activity` 1K+ righe |
| **Topologia** | React Flow (@xyflow/react) | grafo replica, edge lag animati |
| **Tempo** | date-fns + @internationalized/date | range picker tipo Grafana |

*Perché non Grafana embedded:* lock-in, UX limitata, difficile costruire advisor e grafo replica evoluto. Meglio UI custom su API Server; exporter Prometheus opzionale in Phase 2 per chi vuole Grafana esterno.

**Pagine v0.1:**
1. **Fleet Overview:** lista *cluster*, health aggregato (worst instance), role badge, PG version, lag max, QPS. **Include lo stato degli agent** (un agent down deve essere visibile qui, non sepolto in Settings).
2. **Cluster Detail:** grafo topologia (primary -> standby -> cascading), timeline, slot health, failover history. Edge `confidence: low` disegnati tratteggiati.
3. **DB Detail (per-instance):** QPS, latenza, hit ratio, WAL, connessioni. Banner `role: standby (read-only)` + lag gauge. **Selettore database** (nuovo: le metriche per-DB sono per-DB) + indicatore "N database non monitorati" se il budget di 4.2 è saturo.
4. **ASH / Wait Analysis (nuova):** stacked area del tempo DB per `wait_event_type`, drill-down a `wait_event`, drill-down a `queryid`. Nota permanente "campionamento statistico a 1s".
5. **Query Inspector:** top query da `pg_stat_statements`, sort per total/mean/calls, trend, `EXPLAIN` on-demand **se tier T1** (altrimenti bottone disabilitato con tooltip). Confronto tra istanze **abilitato solo intra-cluster** (limite `queryid`, sez. 4.6).
6. **Locks & Activity:** live `pg_stat_activity`, lock graph, cancel/kill **se tier T2**. Scope selezionabile instance o cluster.
7. **Pooler (nuova):** pool, `cl_waiting`, `maxwait`, utilizzo per database/utente.
8. **Advisor:** raccomandazioni con severity, filtro cluster vs instance. Indica quali finding sono degradati per mancanza di tier T3.
9. **Alerts & Events:** timeline con `failover`, `role_change`, `split_brain`, `counter_reset`, `agent_down`.
10. **Settings:** agent (con coda di approvazione enrollment), cluster/instance, tier permessi per istanza, utenti, RBAC.

**Principio di onestà UI:** dove un dato manca (agent giù, check in errore, budget saturo, tier insufficiente, top-N troncato) la UI mostra **esplicitamente il buco**. Mai zero al posto di "sconosciuto", mai interpolazione attraverso un gap. È la differenza tra un monitor su cui si può fare on-call e uno no.

---

## 7. Deployment & Ops

### 7.1 Server - 3 formati equivalenti
1. `docker compose up` (POC/small prod): `server` + `timescaledb` + `gui`. Compose ufficiale in `deploy/compose/`.
2. Immagine singola (`ghcr.io/manprint/pglens-server:latest`), configurazione via env var.
3. Helm chart K8s (Deployment server + StatefulSet TimescaleDB + ingress + PVC). Scheduler/Alert Engine in singolo leader via advisory lock (5.5) - `replicas: N` è sicuro.

### 7.2 Agent - 3 formati equivalenti
1. Binario statico + systemd unit (`curl | sh` + enrollment token). `StateDirectory=pglens`.
2. Container Docker single-host - **con volume persistente obbligatorio** (identity + buffer).
3. Sidecar Compose / DaemonSet K8s.

**Matrice compatibilità:**

| Agent run | PG run | Host metrics | Discovery auto | Identity persistente | Note |
|-----------|--------|--------------|----------------|---------------------|------|
| binary su host | binary su host | sì (diretto) | n/a | `/var/lib/...` | classico |
| binary su host | container | sì | sì via docker.sock | `/var/lib/...` | agent legge Docker API |
| container | binary su host | sì (via `/host/proc`) | n/a | **serve volume** | serve mount |
| container | container | sì (via `/host/proc`) | sì (stessa network) | **serve volume** | caso più comune dev |
| container remote | RDS | no (solo CloudWatch 60s) | no | **serve volume** | solo DSN remoti |
| container remote | Aurora | no | no | **serve volume** | topologia limitata (3.7) |

**Networking Docker:** documentare `host.docker.internal` (Docker Desktop) vs `172.17.0.1` (bridge Linux) vs `network_mode: host`. L'agent risolve `host` anche come service name Compose.

### 7.3 Setup utente monitoring (assente in v0.1)

È il primo attrito reale del "setup < 5 minuti". Script versionato fornito nel repo, con variante per RDS.

```sql
-- setup/monitoring_user.sql  (PG 15-18)
CREATE ROLE monitoring LOGIN PASSWORD :'pw';
GRANT pg_monitor TO monitoring;

-- necessario per cluster_id stabile (sez. 2.2) - NON incluso in pg_monitor
GRANT EXECUTE ON FUNCTION pg_control_system() TO monitoring;
GRANT EXECUTE ON FUNCTION pg_control_checkpoint() TO monitoring;

-- T1 opzionale: abilita EXPLAIN (PG14+)
-- GRANT pg_read_all_data TO monitoring;

-- T2 opzionale: abilita cancel/terminate da UI
-- GRANT pg_signal_backend TO monitoring;

-- consigliato: limita l'impatto del monitoring
ALTER ROLE monitoring SET statement_timeout = '15s';
ALTER ROLE monitoring SET lock_timeout = '1s';
ALTER ROLE monitoring SET idle_in_transaction_session_timeout = '30s';
ALTER ROLE monitoring SET application_name = 'pglens';
```

Variante RDS: identica ma eseguita da `rds_superuser`; nessun `CREATE EXTENSION` senza passare da `shared_preload_libraries` nel parameter group.
Comando di verifica: `pglens-agent check --dsn …` -> stampa tier rilevato, versione, estensioni presenti, check abilitati/disabilitati e **perché**.

### 7.4 Enrollment, auth e revoca agent (assente in v0.1)

v0.1 diceva solo "mTLS o token". Mancava tutto il ciclo di vita - ed è il primo minuto dell'esperienza utente.

**Flusso:**
1. **Server** genera un enrollment token (`TTL` default 1h, monouso o multiuso, scope opzionale a un cluster). Visibile in UI e via CLI.
2. **Agent**: `pglens-agent enroll --server https://… --token <t>`
3. **Server** valida, assegna `agent_id`, restituisce credenziale long-lived: certificato client (mTLS) oppure token bearer con scadenza.
4. **Agent** persiste in `/var/lib/pglens/identity.json` (perm 0600).
5. **Approvazione:** impostazione `auto_approve` on/off. Se off, l'agent resta in stato `pending` e appare in una coda in Settings.
6. **Rotazione:** l'agent rinnova la credenziale a 2/3 della vita. Se il rinnovo fallisce continua con quella corrente fino a scadenza, alzando un warning.
7. **Revoca:** revoca di `agent_id` dalla UI -> il prossimo push riceve 401 con `reason: revoked` -> l'agent smette di raccogliere e lo dice nei log. I dati storici restano.

**Enrollment token nel token dell'immagine Docker:** supportato via `ENROLL_TOKEN` env o file secret (`ENROLL_TOKEN_FILE`) per Compose/K8s secret. Mai in `docker run` in chiaro nella documentazione.

### 7.5 Aggiornamenti
Agent: auto-update opzionale da Server (binary) o rolling image (Docker). Il Server rifiuta i payload di agent con versione di protocollo non supportata, indicando la versione minima -> l'incompatibilità è visibile, non silenziosa. Server versionato con migrazioni schema idempotenti.

---

## 8. Differenziatori vs Esistente

| Tool | Limiti |
|------|--------|
| pgwatch2 | Agent pesante, UI datata, mode remote poco isolato, no advisor, replica vista solo per-instance, **no wait analysis** |
| Percona PMM | Completo ma pesante, focalizzato MySQL+PG, richiede VictoriaMetrics+ClickHouse |
| pganalyze | Ottimo prodotto, ma **commerciale/closed**, SaaS-first |
| Datadog/New Relic | Costo, vendor lock-in, no deep PG advisor, topologia replica opaca |
| check_postgres | Solo Nagios check, no storico |
| Powa / pgHero | Single instance, no fleet |
| Patroni tools | Solo Patroni, no fleet eterogenea |
| AWS Performance Insights | Ottimo ASH, **ma solo AWS** e non correla self-hosted |

**Il nostro angle, in ordine di forza:**
1. **ASH a 1s che funziona anche su managed** (nessuna extension, nessun restart) - la cosa che i DBA guardano per prima e che gli strumenti open fanno peggio
2. **Replica come cittadino di prima classe** (astrazione cluster su `system_identifier`, non su euristiche fragili)
3. Fleet eterogeneo self-host + managed nello stesso pannello
4. Advisor replica-aware
5. Single binary agent + plugin estensibile
6. UX moderna

---

## 9. Sicurezza & Compliance

- TLS obbligatorio agent->server; mTLS raccomandato
- Enrollment con token a TTL, approvazione opzionale, revoca (7.4)
- **Tier di permessi Postgres espliciti** (4.7): default read-only; `EXPLAIN` e kill sono opt-in consapevoli
- `EXPLAIN ANALYZE` mai automatico, mai su statement non-SELECT (4.7)
- RBAC su GUI (org/team, admin/operator/viewer). L'azione kill richiede ruolo operator **e** tier T2
- Audit log: chi ha visto/eseguito EXPLAIN/killato query, con `queryid` e istanza
- **PII:** solo query normalizzate da `pg_stat_statements` (valori dei bind già rimossi da Postgres). `pg_stat_activity.query` **contiene valori letterali** -> nell'ASH viene raccolto solo `query_id`, mai il testo. Il testo live nella pagina Locks & Activity è opt-in per istanza, con avviso
- Secrets: DSN cifrato a riposo, mai loggato, redatto negli errori
- Retention configurabile: default 7gg raw, 30gg rollup 1m, 1 anno rollup 1h. **Meta relazionali (topologia, failover, query_texts) con retention più lunga** - un failover di 6 mesi fa deve restare consultabile anche se le metriche sono scadute

---

## 10. Roadmap

**Phase 0 - POC (3-4 settimane)** *(era 2-3; il testing di sez. 12 è ora incluso)*
- **Identità:** `system_identifier` + UUID persistito + schema con `cluster_id`/`instance_id`/`database` (sez. 2.2) - **prima riga di codice, costo zero ora, migrazione carissima dopo**
- Agent Go: `activity`, `database_stats`, `stat_statements` (con top-N e reset detection), OS metrics base con astrazione `HOST_PROC`
- **ASH a 1s** (sez. 3.5) - è il differenziatore, non va rimandato
- Replica base: `pg_is_in_recovery()` + `pg_stat_replication` + `pg_stat_wal_receiver` + `pg_replication_slots`
- Delta Engine con reset detection (4.5)
- Server: ingest HTTP + TimescaleDB + API, containerizzati
- Enrollment minimo (token + approvazione manuale)
- **Alert di staleness (Tier 0, sez. 5.4)** - senza questi il POC mente
- Script setup utente monitoring (7.3) + comando `agent check`
- Harness di test: compose primary+standby, promote (sez. 12)
- GUI: Fleet overview (cluster list) + Cluster Detail (grafo 2 nodi) + DB detail + **pagina ASH**

**Phase 1 - MVP Fleet + Replica completa (5-6 settimane)**
- Policy multi-database completa (4.2) - connect-on-demand, budget, whitelist
- Agent remote mode multi-DB + `docker-discovery` label-based
- Host monitoring da container validato su cgroup v1/v2
- Topology Engine completo: cascata, orphan, split-brain, failover event, timeline
- Alerting cluster-aware + Slack + silenziamento
- Query Inspector + Locks view
- **pgbouncer** (3.4)
- Tier permessi T1/T2 con UI coerente (4.7)
- Matrice versioni PG 15-18 testata (sez. 12)
- Helm/Compose packaging finalizzato

**Phase 2 - Advisor + Cloud (6-8 settimane)**
- Bloat, unused indexes, config advisor con check replica
- **Aurora TopologyProvider** (3.7) - *da promuovere a Phase 0 se Aurora è il target primario*
- CloudWatch bridge RDS (60s) + Enhanced Monitoring
- Plugin Patroni opzionale
- Log tail (opzionale)
- Exporter Prometheus per chi vuole Grafana esterno

**Phase 3 - Scale & Hardening**
- eBPF per IO latency (local mode)
- EXPLAIN plan history + auto compare
- RBAC completo, SSO, audit
- Downsampling e retention avanzata
- **Extension SDK:** come aggiungere un Check/TopologyProvider in < 100 LOC (esempio Citus o logical replication avanzata)

---

## 11. Limiti Noti / Cosa NON facciamo (NUOVO)

Da scrivere in README e in UI. Un limite dichiarato è una scelta di design; un limite scoperto dall'utente è un bug.

**ASH**
- Campionamento statistico a 1s: le query più brevi di ~1s sono sottorappresentate. Non è un tracciamento esatto (stesso trade-off di Oracle ASH e AWS Performance Insights)
- Finestre con meno di ~60 campioni non sono statisticamente significative
- Correlazione con `queryid` richiede PG14+ e `compute_query_id = on|auto`

**Query**
- `queryid` non è confrontabile tra cluster diversi né tra major version (dipende dagli OID e dall'algoritmo)
- Query oltre `pg_stat_statements.max` vengono evictate: appaiono e scompaiono
- Solo le top-N query per database sono storicizzate (default 50+50); il resto è aggregato in "other"

**Multi-database**
- Default: massimo 10 database monitorati per istanza; oltre, i più attivi. Il numero di database non monitorati è mostrato in UI

**Managed**
- **RDS:** nessuna metrica OS senza Enhanced Monitoring (costo separato); funzioni filesystem WAL bloccate; CloudWatch a 60s minimo, a pagamento
- **Aurora:** in v0.1 solo metriche instance-level. Nessuna topologia replica (modello di replica diverso, sez. 3.7). Lag disponibile solo via provider dedicato
- **Neon / Supabase / Cloud SQL:** best effort. Non testati sistematicamente in v0.1
- **Aurora Serverless:** non supportato v0.1 (scale-to-zero rompe il campionamento continuo)

**Generale**
- PG 12 e precedenti: non supportati
- PG 13 e 14: non supportati (EOL) — PostgreSQL 13 (EOL 2025-11) e 14 (EOL 2026-11)
- `pgstattuple` e bloat esatto: on-demand, mai schedulati
- Pooler: solo pgbouncer in v0.1 (pgcat/odyssey in Phase 2)
- Non facciamo: APM applicativo, log aggregation universale, altri engine (MySQL/Oracle)
- Con un pooler in transaction pooling, `pg_stat_activity` non riflette i client applicativi ma le connessioni server del pooler - i dati vanno letti insieme a `SHOW POOLS`

---

## 12. Strategia di Test (NUOVA - era assente dalla roadmap)

*Perché è in Phase 0 e non dopo:* testare un monitor di replica è la parte più costosa del progetto. Un failover si rompe in modo silenzioso; senza harness ce ne si accorge in produzione. Il costo del test è la ragione principale per cui pgwatch2/PMM gestiscono la replica in modo superficiale.

**12.1 Matrice compose**

Versioni × topologie, generata da template:

| | standalone | primary + 1 standby | cascading (P -> S1 -> S2) | logical pub/sub | + pgbouncer |
|---|---|---|---|---|---|
| PG 15 | ✓ | ✓ | | ✓ | |
| PG 16 | ✓ | ✓ | ✓ | ✓ | ✓ |
| PG 17 | ✓ | ✓ | ✓ | ✓ | ✓ |
| PG 18 | ✓ | ✓ | ✓ | ✓ | ✓ |

CI: matrice ridotta (15, 18) su ogni PR; matrice completa nightly (15→18).

**12.2 Scenari da simulare (non solo "funziona a regime")**

| Scenario | Come | Cosa deve succedere |
|---|---|---|
| Promote / failover | `pg_ctl promote` su standby | evento `failover_detected`, topologia aggiornata entro 20s, `cluster_id` **invariato** |
| Split-brain | promote senza fencing del vecchio primary | `split_brain_detected` |
| Partizione di rete | `docker network disconnect` / `tc netem` | `instance_unreachable`, poi ricongiungimento senza serie duplicate |
| Slot inattivo | ferma lo standby, lascia lo slot | `slot_inactive`, `wal_status` monitorato, alert su crescita WAL |
| Restart Postgres | `docker restart` | `counter_reset_detected`, **nessun rate negativo o picco fasullo** |
| `pg_stat_statements_reset()` | eseguito da un client esterno | come sopra, delta scartato |
| Server giù | ferma il server 30m | agent bufferizza, poi replay senza duplicati e senza gap |
| Disco agent pieno | tmpfs piccola | `samples_dropped_total` cresce, agent non crasha, DB non impattato |
| Skew di clock | container con offset di 5m | `clock_skew` alzato |
| Agent riavviato | `docker restart` con e senza volume | con volume: stessa `instance_id`. **Senza volume: il test deve dimostrare la duplicazione** (regressione nota, documentata) |
| Lock storm | `SELECT pg_sleep` con lock in conflitto | il check `locks` completa comunque (timeout 10s, sez. 4.4) |
| 5.000 query distinte | pgbench custom | top-N attivo, cardinalità sotto budget, `Truncated: true` |

**12.3 Test dei permessi**

Suite eseguita con utente **solo T0**: nessun check deve fallire o loggare errori. Ogni funzione che richiede T1/T2/T3 deve essere disabilitata a monte, mai fallire a runtime. È il test che avrebbe intercettato il bug "EXPLAIN con `pg_monitor`" prima della demo.

**12.4 Golden test del payload**

Snapshot del payload agent per ogni versione PG. Una modifica al parsing che cambia il payload rompe il test -> il drift tra versioni diventa visibile in PR.

**12.5 RDS / Aurora**

Non simulabile. Job nightly opzionale su istanze reali `db.t4g.micro` (RDS) e Aurora Serverless v2 minima, dietro flag con credenziali dei maintainer. Se non disponibile: matrice di supporto dichiarata come "non testata automaticamente" in sez. 11 - meglio ammetterlo che simularlo male.

---

## 13. Decisioni Prese + Domande Residue

**DECISO (v0.1):**
- ✅ Stack Server/Agent: **Go**
- ✅ TSDB: **TimescaleDB** (valido per 10-100 DB, *a condizione che il top-N di 4.6 sia attivo*)
- ✅ Frontend: **Next.js 15 + TypeScript + Tailwind + shadcn/ui + ECharts + TanStack + React Flow**
- ✅ Scala target: **10-50 DB standard, 100 max**

**DECISO (v0.2):**
- ✅ **Identità cluster = `system_identifier`**, non hash di `primary_conninfo` (2.2)
- ✅ **`instance_id` = UUID persistito** -> volume obbligatorio per agent containerizzato
- ✅ **Label `database` obbligatoria** nello schema TSDB
- ✅ **Versioni supportate: PG 15-18**
- ✅ **Top-N lato agent obbligatorio** su `pg_stat_statements`/table/index stats
- ✅ **Reset detection** nel modello dati day-one
- ✅ **Tier di permessi T0-T3**, default read-only; EXPLAIN e kill opt-in
- ✅ **Alert di staleness Tier 0** non disattivabili
- ✅ **ASH a 1s in Phase 0** come differenziatore principale
- ✅ **Niente sampling adattivo**; budget fisso + circuit breaker su errore
- ✅ **Scheduler e Alert Engine sono stateful**; leader election via advisory lock
- ✅ **Nessun "pull mode"**: solo push agent + canale di comando long-poll
- ✅ **pgbouncer incluso** in Phase 1
- ✅ **Aurora declassato** a instance-level in v0.1 (salvo decisione contraria, vedi #2)
- ✅ **Testing in Phase 0**, non dopo
- ✅ Delta calcolati **lato Server** (agent invia counter raw + `stats_reset`)

**DOMANDE BLOCCANTI (rispondere prima della prima `CREATE TABLE`):**

1. **Self-host on-prem o SaaS multi-tenant?**
   Cambia lo schema alla radice: `tenant_id` su ogni tabella, RLS, retention e quota per tenant, isolamento delle credenziali DSN. Aggiungerlo dopo significa riscrivere ogni query. v0.1 lo lasciava con un punto interrogativo in sez. 6 - non è rimandabile.

2. **Aurora è target primario o accessorio?**
   Se primario, il `TopologyProvider` Aurora sale a Phase 0 e va progettato in parallelo a quello streaming (modelli di replica incompatibili, 3.7). Se accessorio, resta Phase 2 e Aurora è instance-level in v0.1. Corollario: quanti DB managed vs self-hosted nel caso reale?

3. **Licenza.** Apache 2.0 / MIT / AGPL / SSPL? Determina se si può accettare codice di terzi, se un competitor può forkare in SaaS, e la strategia di monetizzazione futura. Va decisa prima del primo commit pubblico, non dopo.

**DOMANDE RIMANDABILI (non bloccano lo schema):**

4. Quali topologie replica usi realmente oggi (streaming async 1-1, cascata, Patroni, repmgr)? Quante repliche per cluster al massimo? -> serve per la matrice di test 12.1, non per lo schema
5. Alert must-have day-one -> ordina la sez. 5.4, non la cambia
6. Retention desiderata -> default proposto 7gg raw / 30gg 1m / 1 anno 1h, configurabile
7. Prometheus/Grafana già in uso -> l'exporter è Phase 2 in ogni caso
8. Nome definitivo: `pglens` — https://github.com/manprint/pglens — scelto (D8); ex-placeholder risolto, vedi §1.
9. Estensioni future prioritarie (Citus, sharding) -> l'astrazione `Edge{Type}` di 5.3 le regge già
10. Query sampling / EXPLAIN automatico su slow query -> **raccomandazione: no**. Rischio PII e overhead; `EXPLAIN ANALYZE` esegue davvero la query (4.7). On-demand con conferma esplicita è sufficiente

---

*Prossimo step: rispondi alle 3 domande bloccanti, poi si trasforma questo doc in `overview.md` + piano fasato eseguibile con lo schema DB come primo deliverable.*
