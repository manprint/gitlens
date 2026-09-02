# Testing Strategy - pglens

> Linee guida di testing per lo sviluppo. Documento normativo: le regole qui dentro valgono per ogni PR.
>
> Versione 1.0 · complementare a [`IDEA.md`](./IDEA.md) (in particolare sez. 12)

---

## 0. Principi

Questo progetto è uno strumento di **osservabilità**. Ha un vincolo che la maggior parte delle applicazioni non ha:

> **Se il monitor mente, è peggio che se non esiste.**
> Un grafico plausibile ma sbagliato porta a decisioni sbagliate durante un incidente. Un monitor spento almeno è onesto.

Da qui discendono i principi non negoziabili:

1. **La correttezza numerica è una feature testabile, non un dettaglio.** Rate, delta, reset detection, lag: ognuno ha un test che dimostra il valore atteso, non solo che "non crasha".
2. **Testiamo contro Postgres reale, non contro mock.** Ogni mock di una vista `pg_stat_*` è una bugia che diverge dal comportamento reale a ogni major version. I mock si usano solo per l'IO di rete e per il tempo.
3. **Testiamo il fallimento, non il caso felice.** Il caso felice si scopre rotto in 5 minuti di uso manuale. Il failover a mezzanotte no.
4. **Le asserzioni si fanno sulle API pubbliche, mai sui log.** I log servono a diagnosticare un test fallito, non a decidere se è passato.
5. **Zero `sleep`.** Ogni attesa è una condizione con timeout. Uno `sleep` è un flake che deve ancora manifestarsi.
6. **Un test flaky è un bug aperto, non un fastidio.** Vedi sez. 10.
7. **Ogni limite dichiarato in `IDEA.md` sez. 11 ha un test che lo dimostra.** Un limite non verificato è una speranza.

**Definizione di "fatto":** una feature è completa quando ha (a) i test del livello appropriato, (b) almeno uno scenario di fallimento, (c) la voce di tracciabilità in sez. 14.

---

## 1. I cinque livelli

> **Nota:** L4 e L5 sono i livelli frontend; la loro architettura normativa è
> descritta in [`web/docs/testing.md`](web/docs/testing.md).

| L | Nome | Cosa verifica | Dove | Stack | Postgres reale? | Gira su | Budget |
|---|------|---------------|------|-------|-----------------|---------|--------|
| **L1** | Unit Go | Logica pura: aggregazione ASH, calcolo delta, reset detection, parsing LSN, selezione top-N, valutazione regole alert, matching topologia | `**/*_test.go` accanto al codice | `testing` + `testify/require` | ❌ no IO | ogni push | **< 60s** |
| **L2** | Integration Go | Le query SQL fanno davvero quello che crediamo, su ogni versione PG. Permessi. Differenze di vista per versione. Reset reali | `internal/**/it_*_test.go`, tag `integration` | `testcontainers-go` | ✅ container effimero | ogni PR | **< 5 min** |
| **L3** | E2E sistema | Il sistema completo (agent + server + TSDB) sotto guasti reali: failover, partizioni, deadlock, lock storm, slow query, cardinalità, buffer, skew | `test/e2e/` | Go + `docker compose` + Toxiproxy | ✅ topologie multi-nodo | PR (smoke) / nightly (full) | **10 min / 45 min** |
| **L4** | Component frontend | Componenti React con logica: formattazione, stati vuoto/errore/gap, form validation, riduttori di filtro | `web/src/**/*.test.{ts,tsx}` | Vitest + Testing Library + MSW | ❌ API mockata | ogni PR | **< 90s** |
| **L5** | E2E frontend | Il browser vede la verità: click, form, drill-down, grafici, permessi UI, stati degradati | `web/e2e/` | Playwright | ✅ contro stack L3 | PR (smoke) / nightly (full) | **8 min / 20 min** |

### Frontend

I quattro tipi di test frontend si innestano nella tassonomia L1–L5 già usata
dal repository. La regola è scegliere il livello più basso che dimostra il
comportamento, lasciando all'accettazione solo i flussi che richiedono un
browser e lo stack reale.

| Tipo | Livello | Scopo | Dove | Comando principale |
|---|---|---|---|---|
| **pure** | L1 | Funzioni deterministiche di derivazione, senza React o DOM | `web/src/lib/**/*.test.ts` | `make web-test` |
| **component** | L1 | Un albero React con jsdom, Testing Library e MSW | `web/src/components/**/*.test.tsx`, `web/src/features/**/*.test.tsx` | `make web-test` |
| **route** | L2 UI | Una pagina completa con router, provider e query client | `web/src/features/**/<page>.route.test.tsx` | `make web-test` |
| **acceptance** | L3 | Un browser reale contro lo stack reale | `web/e2e/**/*.spec.ts` | `make test-ui-e2e` |

I test `pure`, `component` e `route` usano il clock deterministico, fixture
validate contro `api/openapi.yaml`, MSW per l'IO HTTP e `expectNoA11yViolations`
per le route. Le regole vincolanti sono in
[`web/docs/testing.md`](web/docs/testing.md).

#### Comandi frontend

```sh
make web-test             # tutti i test Vitest L1/L2 UI
make web-coverage-gate    # test Vitest + floor V8
make test-ui-e2e           # Playwright L3, browser reale contro immagini locali
```

`make test-ui-e2e` richiede Docker, costruisce gli asset web e le immagini
locali, quindi esegue l'accettazione con Chromium contro lo stack reale. È un
gate mirato alle modifiche che toccano il flusso UI o alla chiusura di un
milestone dell'interfaccia; non è necessario dopo ogni test unitario.

#### Floor di copertura frontend

| Ambito | Linee | Branch | Note |
|---|---:|---:|---|
| Globale incluso | 85% | 80% | sul set incluso nel report V8 |
| `web/src/lib/` | 95% | — | tutta la derivazione che determina i numeri mostrati |
| `web/src/api/` | 95% | — | client, 401 e identificatori |
| `web/src/components/state/` | 100% | — | primitive dell'onestà UI |
| `web/src/components/charts/` | 90% | — | option builder dei grafici |
| `web/src/features/` | 80% | — | composizione delle pagine |

I floor si alzano, non si abbassano, nelle fasi successive. Una fase che non
li raggiunge apre un finding in `STATE.md` §9; non modifica il gate.

**Il ponte L3 ↔ L5** è la parte più importante di questa architettura: gli scenari che L3 usa per rompere il sistema sono gli **stessi** che L5 usa come precondizione per verificare che la UI mostri il problema. Un failover generato una volta viene verificato due volte: nei dati (L3) e negli occhi dell'utente (L5). Vedi sez. 8.

### Perché cinque e non tre

I due livelli intermedi (L2, L4) esistono per una ragione economica precisa:

- **Senza L2**, ogni bug in una query SQL o in una differenza `pg_stat_bgwriter`/`pg_stat_checkpointer` tra PG16 e PG17 si scopre in L3, dove serve un ambiente a 4 container per capire quale `SELECT` è sbagliata. L2 lo trova in 3 secondi con un messaggio chiaro.
- **Senza L4**, ogni stato di errore o formattazione richiede un test Playwright. Il risultato tipico è una suite E2E da 300 test, lenta e flaky. Con L4 la suite Playwright resta piccola (~40 test) e testa solo **flussi**, non dettagli.

Regola operativa: **scrivi il test al livello più basso che può dimostrare il comportamento.**

---

## 2. Struttura monorepo

```
pglens/
├── cmd/pglens-agent/            # main + wiring agent
├── cmd/pglens-server/            # main + wiring server
├── internal/
│   ├── pgtype/                  # core value types (ClusterID, InstanceID, Role, Metric, Sample)
│   ├── clock/                   # injectable clock
│   ├── identity/                # persisted agent and instance identity
│   ├── delta/                   # delta + reset detection  -> L1 (gate 90%)
│   ├── cardinality/             # top-N, isteresi, budget  -> L1 (gate 90%)
│   ├── check/                   # registry + Requires()    -> L1 + L2
│   ├── wire/                    # agent<->server JSON envelope
│   ├── agent/                   # agent runtime: scheduler, connections, buffer, pusher
│   ├── server/                  # server runtime: ingest, store, api
│   ├── store/                   # TimescaleDB access and migrations
│   ├── topology/                # fusione grafo replica    -> L1 (gate 90%)
│   ├── ash/                     # ASH aggregation          -> L1 (gate 90%)
├── web/                         # Vite + React frontend
│   ├── src/ components/ lib/ test/
│   └── **/*.test.tsx            # L4
├── deploy/
│   └── compose/                 # compose di produzione
├── test/
│   ├── fixtures/
│   │   ├── sql/                 # schemi, seed, workload SQL
│   │   ├── golden/              # payload golden per versione PG
│   │   └── dataset/             # dump TimescaleDB deterministico per L5 fixture mode
│   ├── compose/                 # topologie di test (sez. 5.1)
│   │   ├── base.yml
│   │   ├── topo-standalone.yml
│   │   ├── topo-primary-standby.yml
│   │   ├── topo-cascading.yml
│   │   ├── topo-logical.yml
│   │   ├── agent-container.yml
│   │   ├── agent-binary.yml
│   │   ├── pgbouncer.yml
│   │   └── toxiproxy.yml
│   ├── harness/                 # libreria Go: compose up/down, wait, client API
│   ├── scenario/                # LIBRERIA SCENARI (sez. 8) + scenariod
│   ├── workload/                # generatore di carico e guasti (sez. 9)
│   ├── e2e/                     # L3
│   └── web-e2e/                 # L5 (Playwright)
├── .github/workflows/
├── Makefile
├── IDEA.md
└── TESTING.md
```

**Regola di collocazione:** un test sta accanto al codice che testa (L1, L2, L4) oppure in `test/` se attraversa più componenti (L3, L5). Non esistono directory `tests/` parallele che duplicano l'albero dei sorgenti.

---

## 3. L1 — Unit test Go

### 3.1 Cos'è un unit test qui

**È L1** se non tocca rete, filesystem, orologio di sistema o database. Input in memoria, output in memoria.

**Non è L1** (→ L2 o L3): apre una connessione, legge un file di config, usa `time.Now()`, avvia una goroutine che aspetta.

### 3.2 Convenzioni

- **Table-driven + subtest**, sempre. Un caso per riga, nome parlante.
- `t.Parallel()` in ogni test e sottotest che non condivide stato. La suite L1 deve stare sotto il minuto.
- **`-race` obbligatorio in CI.** L'agent è concorrente per costruzione (scheduler, buffer, N target): senza race detector i bug si manifestano solo in produzione.
- Niente `assert`, solo **`require`**: un'asserzione fallita ferma il test invece di produrre dieci errori a cascata da un solo difetto.
- **Nessuna libreria di mock generati.** Se serve un doppio, si scrive a mano una fake che implementa l'interfaccia, nel package `_test`. Le fake generate invecchiano male e nascondono i cambi di contratto.
- **Il tempo è un parametro, mai globale.** Ogni componente che misura durate riceve un `clock.Clock`; nei test si usa `clock.NewFake(t0)`. Questo non è un dettaglio di stile: metà della logica del progetto è "calcola un rate tra due istanti", e non è testabile deterministicamente se il tempo è implicito.

```go
func TestDeltaEngine_ResetDetection(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reset1 := t0.Add(-time.Hour)
	reset2 := t0.Add(30 * time.Second) // stats azzerate tra i due sample

	tests := []struct {
		name      string
		prev, cur Sample
		want      *float64 // nil = nessun punto emesso
		wantEvent bool
	}{
		{
			name: "rate normale",
			prev: Sample{Value: 100, TS: t0, StatsReset: reset1},
			cur:  Sample{Value: 160, TS: t0.Add(time.Minute), StatsReset: reset1},
			want: ptr(1.0), // 60 eventi / 60 s
		},
		{
			name:      "stats_reset cambiato: delta scartato, nessun punto",
			prev:      Sample{Value: 100, TS: t0, StatsReset: reset1},
			cur:       Sample{Value: 5, TS: t0.Add(time.Minute), StatsReset: reset2},
			want:      nil,
			wantEvent: true,
		},
		{
			name:      "counter arretrato senza stats_reset: reset implicito",
			prev:      Sample{Value: 100, TS: t0, StatsReset: reset1},
			cur:       Sample{Value: 4, TS: t0.Add(time.Minute), StatsReset: reset1},
			want:      nil,
			wantEvent: true,
		},
		{
			name: "counter fermo: rate zero, non nil",
			prev: Sample{Value: 100, TS: t0, StatsReset: reset1},
			cur:  Sample{Value: 100, TS: t0.Add(time.Minute), StatsReset: reset1},
			want: ptr(0.0),
		},
		{
			name: "intervallo zero: nessun punto, nessuna divisione per zero",
			prev: Sample{Value: 100, TS: t0, StatsReset: reset1},
			cur:  Sample{Value: 120, TS: t0, StatsReset: reset1},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := delta.New()
			e.Observe(tt.prev)
			got, ev := e.Observe(tt.cur)

			if tt.want == nil {
				require.Nil(t, got, "non deve emettere un punto")
			} else {
				require.NotNil(t, got)
				require.InDelta(t, *tt.want, got.Rate, 1e-9)
			}
			require.Equal(t, tt.wantEvent, ev != nil)
		})
	}
}
```

### 3.3 Golden file

Per parser e serializzatori (payload protobuf, aggregato ASH, snapshot topologia) si usano golden file in `testdata/`, con flag di aggiornamento:

```go
var update = flag.Bool("update", false, "aggiorna i golden file")

func TestASHAggregate_Golden(t *testing.T) {
	in := loadSamples(t, "testdata/ash_samples_60s.json")
	got := ash.Aggregate(in, 10*time.Second)
	golden.Assert(t, got, "testdata/ash_aggregate_60s.golden.json", *update)
}
```

`make golden` rigenera. **Un golden aggiornato in una PR va guardato riga per riga in review**: è lì che si vede un cambiamento di comportamento non intenzionale. Un diff golden non spiegato nella descrizione della PR è motivo di richiesta di modifica.

### 3.4 Fuzzing

Il fuzzing nativo di Go è obbligatorio su tutto ciò che parsa input non fidato o semi-strutturato. Non è un extra: un agent che va in panic su una riga malformata di `pg_stat_activity` fa cadere il monitoraggio dell'intero host.

| Target | Perché |
|---|---|
| Parsing LSN (`0/16B3748`) e differenza tra LSN | overflow, wrap, formati non validi |
| Decoder payload protobuf lato server | input da rete, potenzialmente ostile |
| Parser `primary_conninfo` | stringa arbitraria scritta dall'utente |
| Parser csvlog/jsonlog Postgres | righe troncate, multilinea, encoding |
| Aggregatore ASH | `wait_event` nullo, stringhe unicode, cardinalità estrema |
| Valutatore regole alert (YAML) | regola scritta a mano dall'utente |

```go
func FuzzParseLSN(f *testing.F) {
	f.Add("0/16B3748")
	f.Add("FFFFFFFF/FFFFFFFF")
	f.Add("0/0")
	f.Fuzz(func(t *testing.T, s string) {
		lsn, err := pg.ParseLSN(s)
		if err != nil {
			return // rifiutare è sempre lecito
		}
		// invariante: round-trip stabile, nessun panic
		require.Equal(t, s, strings.ToUpper(lsn.String()))
	})
}
```

CI: 30s per target su ogni PR; 10 minuti per target nel job nightly. Il corpus che trova un crash viene **committato** in `testdata/fuzz/` come test di regressione permanente.

### 3.5 Gate di copertura L1+L2

| Package | Gate | Perché |
|---|---|---|
| `internal/delta` | **90%** | se sbaglia, ogni grafico mente |
| `internal/topology` | **90%** | se sbaglia, il failover non viene visto |
| `internal/ash` | **90%** | è il differenziatore del prodotto |
| `internal/cardinality` | **90%** | se sbaglia, il TSDB esplode in produzione |
| `internal/alert` | **90%** | un alert che non parte è un incidente non visto |
| Complessivo Go | **75%** | |
| Escluso dal calcolo | `main.go`, codice generato (`sqlc`, protobuf), migrazioni | |

La copertura è un **pavimento, non un obiettivo**. Nessuno scrive test per alzare una percentuale: si scrivono test per i comportamenti, e il gate impedisce le regressioni silenziose. Una PR che alza la copertura senza aggiungere asserzioni significative viene respinta.

---

## 4. L2 — Integration test Go (Postgres reale)

### 4.1 Cosa vive qui

Tutto ciò che dipende dal **comportamento reale di Postgres**:

- ogni `Scrape()` di ogni check, eseguito contro un DB vero
- differenze di vista tra major version (`pg_stat_wal` da 14, `pg_stat_io` da 16, `pg_stat_checkpointer` da 17)
- **permessi**: la suite completa gira con un utente **solo T0** e nessun check deve fallire (sez. 4.4)
- reset reali (`pg_stat_reset()`, restart) e loro effetto sul Delta Engine
- migrazioni dello schema TimescaleDB (up, e idempotenza di un doppio up)
- query dell'API server contro dati seminati

### 4.2 Container effimeri con testcontainers-go

```go
//go:build integration

func newPG(t *testing.T, version string) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	c, err := postgres.Run(ctx,
		"postgres:"+version+"-alpine",
		postgres.WithDatabase("app"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("test"),
		postgres.WithInitScripts("../../test/fixtures/sql/init.sql"),
		testcontainers.WithConfigModifier(func(cfg *container.Config) {
			cfg.Cmd = []string{"postgres",
				"-c", "shared_preload_libraries=pg_stat_statements",
				"-c", "compute_query_id=on",       // necessario per correlare ASH <-> statements
				"-c", "track_io_timing=on",
				"-c", "log_statement=none",
			}
		}),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	...
}
```

**Velocità.** Avviare un container per test costa 2-4s: con 200 test L2 la suite sfora il budget. Contromisure, in ordine:

1. Un container per (versione PG × profilo permessi) per package, condiviso via `TestMain`. I test ottengono un **database isolato** (`CREATE DATABASE test_<n>`), non un container isolato.
2. `testcontainers` **Reuse** in locale (`TESTCONTAINERS_REUSE_ENABLE=true`) per non ripagare l'avvio a ogni `go test`. Mai in CI: il riuso nasconde le dipendenze dall'ordine.
3. `fsync=off`, `full_page_writes=off`, `synchronous_commit=off`, volume su tmpfs. Sono container usa-e-getta: la durabilità non serve e costa.
4. Matrice PG **ridotta su PR** (15, 18), **completa nightly** (15→18). Le tre versioni scelte coprono i tre regimi: minimo supportato, mediano, ultimo.

### 4.3 Matrice versioni

```go
var pgVersions = versionsFromEnv() // PR: "15,18" · nightly: "15,16,17,18"

func TestCheck_StatIO(t *testing.T) {
	forEachPG(t, func(t *testing.T, pg *PG) {
		c := check.Get("stat_io")
		if !c.Requires().SupportsVersion(pg.VersionNum) {
			// L'esclusione DEVE essere esplicita e verificata:
			// un check che si auto-esclude in silenzio è indistinguibile da uno rotto.
			require.Less(t, pg.VersionNum, 160000, "stat_io deve essere supportato da PG16")
			t.Skip("pg_stat_io richiede PG16+")
		}
		res, err := c.Scrape(ctx, pg.Target())
		require.NoError(t, err)
		require.NotEmpty(t, res.Metrics)
		require.Contains(t, metricNames(res), "pg_io_reads_total")
	})
}
```

### 4.4 Profilo permessi — la suite che protegge dal bug più costoso

`IDEA.md` sez. 4.7 documenta il difetto trovato in v0.1: `EXPLAIN` e `kill query` erano promessi con il solo ruolo `pg_monitor`, dove non funzionano. Questa suite esiste perché quel bug non si ripeta mai più.

**Regola:** l'intera suite L2 gira **di default con utente T0**. Un check che richiede più permessi deve essere **escluso a monte da `Requires().PermTier`**, mai fallire a runtime.

```go
func TestAllChecks_WorkOnT0(t *testing.T) {
	forEachPG(t, func(t *testing.T, pg *PG) {
		target := pg.TargetAs(T0) // solo pg_monitor + pg_control_system()
		for _, c := range check.All() {
			c := c
			t.Run(c.Name(), func(t *testing.T) {
				if c.Requires().PermTier > check.TierReadOnly {
					t.Skip("richiede tier " + c.Requires().PermTier.String())
				}
				_, err := c.Scrape(ctx, target)
				require.NoError(t, err,
					"check %q dichiara tier T0 ma fallisce con soli permessi T0", c.Name())
			})
		}
	})
}
```

Test simmetrico, altrettanto importante:

```go
// Un check che dichiara un tier alto DEVE davvero fallire senza quel tier.
// Altrimenti il tier è sovradimensionato e stiamo chiedendo permessi inutili.
func TestHighTierChecks_ActuallyNeedTheirTier(t *testing.T) { … }
```

**Profilo `rds-like`** (niente AWS, ma i vincoli di RDS riprodotti):

```sql
-- test/fixtures/sql/rds_like.sql
CREATE ROLE rds_superuser_sim NOSUPERUSER CREATEDB CREATEROLE;
-- niente superuser: nessun accesso al filesystem del server
REVOKE EXECUTE ON FUNCTION pg_read_file(text)      FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION pg_ls_dir(text)         FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION pg_ls_waldir()          FROM PUBLIC;
-- CREATE EXTENSION solo dalla whitelist del parameter group
REVOKE CREATE ON DATABASE app FROM PUBLIC;
```

La suite gira due volte: `PROFILE=vanilla` e `PROFILE=rds-like`. Ogni check deve degradare in modo **dichiarato** (metrica assente + `check_skipped{reason}`), mai andare in errore. Questo copre la maggior parte dei bug che si manifesterebbero su RDS senza avere un account AWS.

### 4.5 Golden payload per versione

Per ogni versione PG viene salvato lo snapshot del payload dell'agent:

```
test/fixtures/golden/payload_pg16.json
```

Un cambio di parsing che altera il payload rompe il test e **rende il drift tra versioni visibile in review**. Senza questo, una colonna rinominata in una major version passa inosservata finché un utente non apre una issue.

---

## 5. L3 — E2E di sistema (Docker)

Il cuore della strategia. Qui si verifica che il sistema **completo** dica la verità mentre il mondo si rompe.

### 5.1 Topologie

Ogni topologia è un `docker compose` componibile da `test/compose/`:

| Topologia | Servizi | Copre |
|---|---|---|
| `standalone` | 1 PG | baseline, cardinalità, reset, carico |
| `primary-standby` | PG primary + 1 standby (streaming) | replica, lag, slot, failover |
| `cascading` | P → S1 → S2 | topologia ad albero, orphan detection |
| `logical` | 2 cluster + publication/subscription | `system_identifier` diversi = cluster distinti |
| `pgbouncer` | + pooler davanti a PG | `cl_waiting`, transaction pooling |
| `toxiproxy` | proxy tra agent e PG, e tra agent e server | latenza, timeout, partizione |
| `rds-like` | PG con vincoli RDS (sez. 4.4) | permessi degradati |

Composizione:
```bash
docker compose \
  -f test/compose/base.yml \
  -f test/compose/topo-primary-standby.yml \
  -f test/compose/agent-container.yml \
  -f test/compose/toxiproxy.yml up -d
```

### 5.2 Matrice di deploy dell'agent (esplicitamente richiesta)

L'agent deve funzionare identicamente in tre modalità. **La stessa suite di scenari gira su tutte e tre.**

| `AGENT_MODE` | Come | Cosa verifica in più |
|---|---|---|
| `container` | servizio Docker con volume `agent-data`, mount `/proc:/host/proc:ro` | metriche host da container, persistenza identità, cgroup v2 |
| `binary` | binario compilato, eseguito come processo sul runner, connesso ai port pubblicati | percorso nativo, systemd-like, `HOST_PROC` non impostato |
| `container-no-volume` | come `container` ma **senza** volume | **test negativo:** al restart l'`instance_id` cambia e le istanze si duplicano. Deve fallire in modo *dichiarato* e documentato, non silenzioso |

Il terzo caso non è un test "che deve passare": è un test che **congela una regressione nota** descritta in `IDEA.md` sez. 2.2. Se un giorno l'agent imparasse a recuperare l'identità senza volume, questo test fallirebbe e ci costringerebbe ad aggiornare la documentazione. È esattamente ciò che vogliamo.

### 5.3 Harness

`test/harness` è una libreria Go che nasconde tutto il rumore di Docker:

```go
func TestFailoverDetected(t *testing.T) {
	h := harness.Start(t, harness.Config{
		Topology:  harness.PrimaryStandby,
		PGVersion: harness.PGFromEnv(),
		AgentMode: harness.AgentModeFromEnv(),
	})
	defer h.Dump(t) // su fallimento: log di tutti i container + dump API + stato TSDB

	clusterID := h.WaitForCluster(t, 60*time.Second)
	h.RequireRole(t, "pg-primary", api.RolePrimary)
	h.RequireRole(t, "pg-standby", api.RoleStandby)

	h.Scenario(t, "SYS-REPL-001") // promote dello standby

	// Asserzione sull'API pubblica, mai sui log
	harness.Eventually(t, 30*time.Second, func() error {
		st, err := h.API().Cluster(clusterID)
		if err != nil {
			return err
		}
		if st.Primary != "pg-standby" {
			return fmt.Errorf("primary atteso pg-standby, trovato %s", st.Primary)
		}
		return nil
	})

	ev := h.API().Events(clusterID, api.FilterType("failover_detected"))
	require.Len(t, ev, 1)

	// L'invariante che conta davvero: il cluster_id NON cambia dopo un failover.
	// Se cambiasse, tutto lo storico del cluster si spezzerebbe in due.
	require.Equal(t, clusterID, h.API().Cluster(clusterID).ID)
}
```

**`h.Dump(t)`** su fallimento raccoglie log dei container, stato API, contenuto rilevante del TSDB e li salva come artefatto CI. Un E2E che fallisce in CI senza artefatti costa mezza giornata di debug: l'harness lo rende non-negoziabile.

### 5.4 Determinismo

Il nemico degli E2E su un sistema di monitoraggio è che **tutto è asincrono e temporizzato**. Contromisure obbligatorie:

| Problema | Contromisura |
|---|---|
| Intervalli di scrape lunghi rendono i test lentissimi | modalità test: intervalli compressi via config push (`ash=1s`, `activity=1s`, `replication=1s`) |
| Jitter randomizzato dello scheduler | `--testing.no-jitter` disattiva la randomizzazione |
| "Quando è pronto il dato?" | mai `sleep`: `harness.Eventually` con condizione esplicita e timeout |
| Orologio | l'agent e il server accettano `--testing.clock=<rfc3339>` per i test che verificano finestre temporali |
| Ordine di avvio | ogni servizio ha healthcheck; l'harness attende `healthy`, non "avviato" |
| Dati residui tra test | ogni test ha il proprio namespace `cluster_id`; nessuno stack condiviso tra test paralleli |

### 5.5 Fault injection

**Rete — Toxiproxy** (scelto rispetto a `tc netem`: ha un client Go, non richiede `NET_ADMIN`, è deterministico e funziona identico su ogni runner):

```go
h.Toxic("agent->server", toxic.Latency{Latency: 2000, Jitter: 500})
h.Toxic("agent->pg",     toxic.Timeout{Timeout: 0})   // black hole: connessione appesa
h.Toxic("agent->pg",     toxic.LimitData{Bytes: 1024}) // troncamento a metà risposta
h.RemoveToxics("agent->pg")
```

`toxic.Timeout` con `Timeout: 0` (connessione che non risponde mai) è il caso peggiore e il più realistico: distingue un client che gestisce i timeout da uno che si appende per sempre.

**Container:** `docker pause` / `unpause` (freeze senza chiusura connessioni — simula una VM sospesa o un GC stop-the-world), `stop`/`start`, `network disconnect`, `kill -9`.

**Disco:** volume su tmpfs dimensionata (es. 16MB) per riempirla e verificare il comportamento del buffer dell'agent.

**Orologio:** container avviato con offset (`libfaketime` o `--testing.clock`) per validare il rilevamento di clock skew.

### 5.6 Cosa si asserisce

Regole di asserzione E2E:

- ✅ **API pubblica del server** (`/api/v1/...`) — la fonte di verità
- ✅ **query SQL dirette al TSDB** per invarianti che l'API non espone (es. assenza di punti duplicati)
- ✅ **metriche di self-monitoring** (`up`, `samples_dropped_total`, `check_error_total`)
- ❌ **mai i log** come condizione di successo
- ❌ **mai lo stato interno dell'agent** via canali privati

**Invarianti globali**, verificate al termine di *ogni* scenario da un helper comune:

```go
func (h *Harness) AssertInvariants(t *testing.T) {
	h.NoDuplicateSamples(t)    // stessa (serie, ts) mai inserita due volte
	h.NoNegativeRates(t)       // un rate negativo = reset detection rotta
	h.NoOrphanMetrics(t)       // ogni metrica ha cluster_id/instance_id risolvibili in `instances`
	h.NoUnexpectedErrors(t)    // check_error_total invariato, salvo quelli attesi dallo scenario
	h.CardinalityWithinBudget(t)
	h.NoGoroutineLeak(t)       // conteggio goroutine agent stabile a fine test
}
```

Queste invarianti sono la rete di sicurezza più efficace dell'intera suite: catturano bug che nessuno scenario cercava esplicitamente.

---

## 6. L4 — Component test frontend

Il frontend attuale è una Vite + React application. Le regole dettagliate e i
helper condivisi sono in [`web/docs/testing.md`](web/docs/testing.md).

### 6.1 Stack

**Vitest** + **@testing-library/react** + **MSW** (mock a livello di rete, non
di modulo) + **jsdom**.

### 6.2 Separazione della logica e dei componenti

Con Vite + React tutto il codice applicativo gira nel client; la separazione da
rispettare scrivendo il codice è quindi:

- La **logica** (formattazione, aggregazione, decisione su stato vuoto/errore/gap) sta in **funzioni pure** in `web/src/lib/` → testate a fondo con Vitest, senza React
- I **componenti** (grafici, tabelle, form, filtri) → testati con Testing Library e MSW
- Le **route** → testate con router e query client reali; i flussi completi → verificati in L5 (Playwright)

Questa separazione va difesa in review: se la logica finisce dentro un componente, diventa testabile solo con un browser, ed è un costo permanente.

### 6.3 Cosa si testa qui

| Area | Esempi |
|---|---|
| Formattazione | byte → `1.2 GB`, lag → `2m 14s`, LSN, durate, numeri grandi nelle tabelle |
| **Stati degradati** | `agent down`, `dato assente`, `troncato per budget`, `tier insufficiente`, `gap nella serie` |
| Form | validazione regola alert, DSN, finestra di silenziamento, campi obbligatori, messaggi d'errore |
| Riduttori di filtro | selezione cluster/instance/database/timerange, stato in URL |
| Tabelle | sort, filtri, virtualizzazione, stato vuoto |

**Il test più importante di tutto L4:**

```tsx
it("mostra un gap, non uno zero, quando mancano i dati", () => {
  render(<MetricChart series={[{ t: 0, v: 10 }, { t: 60, v: null }, { t: 120, v: 12 }]} />);
  expect(screen.getByTestId("gap-indicator")).toBeInTheDocument();
  expect(screen.queryByText("0")).not.toBeInTheDocument();
});
```

`IDEA.md` sez. 6 stabilisce il "principio di onestà UI": dove un dato manca, la UI deve mostrare il buco. Disegnare uno zero al posto di "sconosciuto" è il modo più diretto per far prendere una decisione sbagliata a chi è di turno alle 3 di notte. Questo comportamento è un requisito, quindi ha un test.

**Gate copertura frontend:** floor globale 85% linee e 80% branch, più i floor
per-directory descritti nella sezione Frontend sopra. Il gate include tutte le
fonti applicative e mantiene fuori solo test, primitive shadcn generate,
output OpenAPI e bootstrap Vite.

---

## 7. L5 — E2E frontend (Playwright)

Il bootstrap Playwright è in `web/e2e/`; l'esecuzione contro lo stack reale è il
livello L5 e il target `make test-ui-e2e` esegue gli scenari di accettazione.

### 7.1 Playwright, non Selenium

| Criterio | Playwright | Selenium |
|---|---|---|
| Auto-waiting | integrato su ogni locator → elimina la causa #1 di flake | attese esplicite scritte a mano |
| Debug di un fallimento in CI | **Trace Viewer**: timeline, DOM, rete, screenshot per step | log + screenshot |
| Setup | un pacchetto npm, browser gestiti | Grid/driver da installare e versionare |
| Linguaggio | TypeScript nativo, stesso stack del frontend | binding esterni |
| Intercettazione rete | nativa (`page.route`) — essenziale per simulare errori API | tramite proxy esterno |
| Parallelismo | worker nativi, isolamento per contesto | Grid da gestire |

**Scelto Playwright.** Il Trace Viewer da solo giustifica la decisione: un E2E frontend che fallisce in CI e non è riproducibile in locale è debito puro, e la trace lo rende diagnosticabile in due minuti.

Browser: **Chromium** su ogni PR; **Chromium + Firefox + WebKit** nightly. Headless sempre (`--headed` solo in locale).

### 7.2 Due modalità di esecuzione

Distinzione centrale, che tiene la suite veloce **e** vera:

| Modalità | Suffisso | Dati | Quando | Verifica |
|---|---|---|---|---|
| **Fixture** | `*.fixture.spec.ts` | dump TimescaleDB deterministico, ripristinato prima della suite; clock del server fissato | ogni PR | rendering, interazioni, form, permessi UI, a11y, visual regression |
| **Live** | `*.live.spec.ts` | stack L3 reale, scenari eseguiti in tempo reale | PR (smoke) + nightly (full) | il percorso completo evento → agent → server → UI |

Il **fixture dataset** (`test/fixtures/dataset/`) è generato da una esecuzione L3 reale e committato come dump compresso (~5MB). Contiene di proposito: un cluster sano, un cluster con lag, un failover storico, uno slot inattivo, un agent down, una serie con un gap, query con testo lungo, un cluster in stato troncato per budget. Rigenerabile con `make fixture-dataset`.

Questo dà il meglio dei due mondi: la maggior parte dei test è deterministica e finisce in secondi; una minoranza dimostra che la catena reale funziona davvero.

### 7.3 Il ponte con L3: `scenariod`

La richiesta — *"script che generano eventi e che poi vanno verificati a livello di click sul frontend"* — è risolta da un servizio di controllo nello stack di test.

`scenariod` è un piccolo servizio HTTP (Go, parte di `test/scenario/`) presente nello stack L3, che espone la **stessa libreria di scenari** usata dai test Go:

```
GET  /scenarios                → catalogo (id, descrizione, durata stimata, precondizioni)
POST /scenarios/SYS-REPL-001/run   → esegue, ritorna { runId, clusterId, startedAt }
GET  /runs/{runId}             → { status: running|done|failed, artifacts }
POST /reset                    → riporta lo stack allo stato pulito
```

Playwright lo invoca via `fetch`, senza bisogno del toolchain Go nel container dei test:

```ts
test.describe("failover visibile nella UI", () => {
  let clusterId: string;

  test.beforeAll(async () => {
    const run = await scenariod.run("SYS-REPL-001"); // promote dello standby
    clusterId = run.clusterId;
    await scenariod.waitDone(run.runId);
  });

  test("il grafo di topologia inverte i ruoli e registra l'evento", async ({ page }) => {
    await page.goto(`/clusters/${clusterId}`);

    // il badge di ruolo si aggiorna senza reload manuale (polling TanStack Query)
    await expect(page.getByTestId("node-pg-standby").getByTestId("role-badge"))
      .toHaveText("primary", { timeout: 30_000 });
    await expect(page.getByTestId("node-pg-primary").getByTestId("role-badge"))
      .toHaveText("standby");

    // l'evento compare nella timeline
    await page.getByRole("tab", { name: "Events" }).click();
    await expect(page.getByRole("row", { name: /failover_detected/ })).toBeVisible();

    // drill-down: click sul nodo porta al dettaglio della nuova primary
    await page.getByTestId("node-pg-standby").click();
    await expect(page).toHaveURL(/\/instances\//);
    await expect(page.getByTestId("instance-role")).toHaveText("primary");
  });
});
```

**Ogni scenario L3 è così verificato due volte**: nei dati (L3, Go) e negli occhi dell'utente (L5, Playwright). Se un failover è corretto nel database ma la UI continua a mostrare il vecchio primary, il bug viene trovato — ed è esattamente il tipo di bug che sfugge a entrambi i livelli presi da soli.

### 7.4 Page Object

Selettori: **solo `data-testid`** e ruoli ARIA. Mai selettori CSS legati alla struttura o classi Tailwind: si rompono a ogni restyling e producono suite che nessuno vuole mantenere.

```ts
export class ClusterDetailPage {
  constructor(private page: Page) {}

  node(name: string)      { return this.page.getByTestId(`node-${name}`); }
  roleBadge(name: string) { return this.node(name).getByTestId("role-badge"); }
  lagChart()              { return this.page.getByTestId("lag-chart"); }
  eventsTab()             { return this.page.getByRole("tab", { name: "Events" }); }

  async setTimeRange(range: "15m" | "1h" | "24h" | "7d") {
    await this.page.getByTestId("time-range-picker").click();
    await this.page.getByRole("option", { name: range }).click();
    await this.page.getByTestId("chart-loading").waitFor({ state: "detached" });
  }
}
```

### 7.5 Cosa deve coprire L5

**Flussi di navigazione**
- Fleet → Cluster → Instance → Query Inspector, con lo stato dei filtri preservato
- Deep link: aprire un URL con filtri e timerange ricostruisce esattamente la stessa vista (è ciò che si incolla in un canale di incident)

**Form e pulsanti** — ognuno testato in successo *e* in fallimento:

| Elemento | Casi |
|---|---|
| Enrollment agent | genera token, TTL, copia negli appunti, revoca, agent in coda `pending` → approvazione |
| Form regola alert | YAML valido → salvata · YAML invalido → errore inline, niente salvataggio · duplicato → conflitto |
| Silenziamento | crea finestra, verifica che l'alert diventi silenziato, scadenza |
| **Kill query** | con tier T2: conferma → query terminata (verificata via API) · **senza T2: pulsante disabilitato con tooltip** |
| **EXPLAIN** | con T1: piano mostrato · senza T1: disabilitato · `EXPLAIN ANALYZE`: modale di conferma, **bloccato su statement non-SELECT** |
| Time range picker | preset, range custom, persistenza in URL |
| Selettore database | cambio DB ricarica le metriche per-DB, banner "N database non monitorati" |
| Filtri e sort tabelle | `pg_stat_activity` con 1000+ righe: virtualizzazione, sort stabile, filtro testuale |
| Dark/light mode | toggle, persistenza |

I due test sui permessi (`kill` ed `EXPLAIN` disabilitati senza tier) sono la controparte UI del bug di `IDEA.md` sez. 4.7. Verificano il comportamento corretto: **disabilitare a monte**, non far fallire l'azione.

**Grafici** — non si asserisce sui pixel, si asserisce sul comportamento:
- il grafico rende punti (nodi SVG/canvas presenti, non contenitore vuoto)
- il tooltip su hover mostra un valore coerente con l'API
- brush/zoom modifica il range e ricarica
- una serie con un gap mostra il gap (controparte L5 del test L4 di sez. 6.3)

**Stati degradati** — con intercettazione di rete, senza rompere il backend:
```ts
await page.route("**/api/v1/clusters/*", r => r.fulfill({ status: 500 }));
await expect(page.getByTestId("error-state")).toBeVisible();
await expect(page.getByTestId("error-state")).toContainText(/riprova/i);
```
Coperti: API 500, timeout, risposta vuota, agent down, tenant senza cluster (empty state di primo avvio).

**Accessibilità** — `@axe-core/playwright` su ogni pagina principale, gate su violazioni `serious` e `critical`. Non è burocrazia: una dashboard di monitoring si usa sotto stress, spesso su schermi pessimi, e il contrasto e la navigazione da tastiera sono ergonomia reale.

**Visual regression** — **solo in fixture mode**, solo su un insieme ristretto di pagine stabili, con animazioni disabilitate e clock fissato. In live mode è garantito flake: i dati cambiano a ogni run.
```ts
await expect(page).toHaveScreenshot("cluster-detail.png", {
  maxDiffPixelRatio: 0.01,
  animations: "disabled",
  mask: [page.getByTestId("last-updated")],  // il timestamp cambia sempre
});
```

### 7.6 Configurazione

```ts
// web/playwright.config.ts
export default defineConfig({
  testDir: ".",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,   // vedi politica anti-flake, sez. 10
  workers: process.env.CI ? 4 : undefined,
  timeout: 60_000,
  expect: { timeout: 15_000 },       // le dashboard fanno polling: serve margine
  reporter: [["html"], ["github"], ["json", { outputFile: "results.json" }]],
  use: {
    baseURL: process.env.APP_URL ?? "http://localhost:3000",
    trace: "retain-on-failure",
    video: "retain-on-failure",
    screenshot: "only-on-failure",
    testIdAttribute: "data-testid",
  },
  projects: [
    { name: "chromium", use: devices["Desktop Chrome"] },
    ...(process.env.FULL_MATRIX ? [
      { name: "firefox", use: devices["Desktop Firefox"] },
      { name: "webkit",  use: devices["Desktop Safari"] },
    ] : []),
  ],
});
```

### 7.7 Scenari di accettazione `SYS-UI-*`

Gli scenari Playwright coprono l'interfaccia servita dal server e vengono
eseguiti con `make test-ui-e2e`. Il catalogo corrente è:

| ID | Comportamento verificato |
|---|---|
| `SYS-UI-000` | Il server serve la shell applicativa autenticata |
| `SYS-UI-001` | Il failover conserva l'identità del cluster e genera l'alert |
| `SYS-UI-002` | La navigazione senza autenticazione non ripristina i dati fleet |
| `SYS-UI-003` | Un agent fuori servizio appare come dati fleet obsoleti |
| `SYS-UI-004` | Il replay lag di un'istanza standalone resta sconosciuto |
| `SYS-UI-005` | ASH disabilitato mostra la relativa configurazione |
| `SYS-UI-006` | L'EXPLAIN plan-only è auditato senza inviare query text |
| `SYS-UI-007` | I grafici di replica e ASH visualizzano dati |
| `SYS-UI-008` | L'albero dei lock mostra radice e figlio |
| `SYS-UI-009` | I comandi di segnalazione rispettano tier e conferma |
| `SYS-UI-010` | Mute e unmute di un finding aggiornano lo stato |
| `SYS-UI-011` | Le route documentate superano i controlli di accessibilità |

La suite usa Chromium per la verifica locale. Le esecuzioni complete sono
costose: eseguirle quando cambia il flusso di accettazione o alla chiusura di
un traguardo dell'interfaccia, non dopo ogni modifica isolata.

---

## 8. Libreria degli scenari (il ponte L3 ↔ L5)

Ogni scenario ha un **ID stabile**, è implementato una sola volta in `test/scenario/`, ed è invocabile da Go (L3) e via HTTP (L5).

```go
// test/scenario/repl_failover.go
var _ = Register(Scenario{
    ID:          "SYS-REPL-001",
    Title:       "Promote dello standby",
    Topology:    PrimaryStandby,
    EstDuration: 25 * time.Second,
    Covers:      []string{"IDEA.md#5.1", "IDEA.md#12.2"},
    Run: func(ctx context.Context, e *Env) error {
        if err := e.Exec("pg-standby", "pg_ctl", "promote", "-D", "/var/lib/postgresql/data"); err != nil {
            return err
        }
        return e.WaitSQL(ctx, "pg-standby", "SELECT NOT pg_is_in_recovery()", 30*time.Second)
    },
    Expect: Expectations{
        Events:     []string{"failover_detected"},
        Invariants: []string{"cluster_id_stable", "no_negative_rates"},
    },
})
```

### 8.1 Catalogo

| ID | Scenario | Topologia | Cosa deve accadere | Verifica UI |
|---|---|---|---|---|
| **Replica** | | | | |
| `SYS-REPL-001` | Promote standby | primary-standby | `failover_detected`, ruoli invertiti < 20s, **`cluster_id` invariato** | `WEB-TOPO-001` |
| `SYS-REPL-002` | Split-brain (promote senza fencing) | primary-standby | `split_brain_detected`, entrambi i nodi marcati primary | `WEB-ALERT-002` |
| `SYS-REPL-003` | Standby orfano (primary fermato) | cascading | `orphan_standby`, edge marcato `confidence: low` | `WEB-TOPO-002` |
| `SYS-REPL-004` | Lag indotto (`recovery_min_apply_delay`) | primary-standby | lag cresce monotono, alert `lag > 30s` dopo 2m | `WEB-CLUSTER-003` |
| `SYS-REPL-005` | Replica in cascata a 3 livelli | cascading | grafo ad albero corretto, S1 sia sorgente sia destinazione | `WEB-TOPO-003` |
| `SYS-REPL-006` | Logical replication tra due cluster | logical | **due `cluster_id` distinti**, edge `type=logical` | `WEB-TOPO-004` |
| **Slot e WAL** | | | | |
| `SYS-SLOT-001` | Slot inattivo (standby fermo, slot conservato) | primary-standby | `slot_inactive` dopo 10m (compresso a 30s in test), WAL in crescita | `WEB-CLUSTER-004` |
| `SYS-SLOT-002` | Slot che raggiunge `wal_status=lost` | primary-standby | alert critico, standby non recuperabile | `WEB-ALERT-003` |
| `SYS-WAL-001` | Alto tasso di generazione WAL | standalone | `wal_bytes` rate coerente con il carico ±10% | — |
| **Reset counter** | | | | |
| `SYS-RESET-001` | Restart di Postgres | standalone | `counter_reset_detected`, **nessun rate negativo, nessun picco** | `WEB-EVENT-001` |
| `SYS-RESET-002` | `pg_stat_statements_reset()` esterno | standalone | delta scartato, serie riprende dal nuovo baseline | — |
| `SYS-RESET-003` | Eviction da `pg_stat_statements.max` | standalone | query che sparisce e riappare non produce rate assurdi | — |
| **Rete e resilienza** | | | | |
| `SYS-NET-001` | Server irraggiungibile 10 min | standalone | agent bufferizza, poi replay **senza duplicati né gap** | `WEB-FLEET-002` |
| `SYS-NET-002` | Latenza 2s agent→PG | standalone | check con timeout coerente, `check_error_total` per i soli check a timeout basso |  — |
| `SYS-NET-003` | Black hole agent→PG (Toxiproxy timeout) | standalone | `instance_unreachable` < 2m, l'agent **non si appende** | `WEB-FLEET-001` |
| `SYS-NET-004` | Partizione e ricongiungimento | primary-standby | nessuna serie duplicata dopo il ricongiungimento | — |
| `SYS-NET-005` | `docker pause` del container PG | standalone | trattato come irraggiungibile, ripresa pulita | — |
| **Carico e patologie DB** | | | | |
| `SYS-LOAD-001` | Deadlock ripetuti | standalone | `deadlocks` rate > 0, coppia di query visibile in Locks | `WEB-LOCK-001` |
| `SYS-LOAD-002` | Lock storm (50 sessioni sulla stessa riga) | standalone | il check `locks` **completa** (timeout 10s), albero dei lock corretto | `WEB-LOCK-002` |
| `SYS-LOAD-003` | Slow query (`pg_sleep(30)`) | standalone | compare in ASH e in Query Inspector, wait event corretto | `WEB-ASH-001` |
| `SYS-LOAD-004` | `idle in transaction` per 10 min | standalone | alert `idle_in_transaction`, età della transazione corretta | `WEB-ACT-001` |
| `SYS-LOAD-005` | Race: 20 worker in UPDATE concorrente | standalone | wait event `Lock:transactionid` dominante in ASH | `WEB-ASH-002` |
| `SYS-LOAD-006` | Bloat indotto (UPDATE massivi, autovacuum off) | standalone | `n_dead_tup` cresce, stima bloat coerente, advisor segnala | `WEB-ADV-001` |
| `SYS-LOAD-007` | Seq scan su tabella grande senza indice | standalone | advisor propone l'indice mancante | `WEB-ADV-002` |
| `SYS-LOAD-008` | 5000 query strutturalmente distinte | standalone | **top-N attivo**, `Truncated: true`, cardinalità sotto budget | `WEB-QUERY-002` |
| **Agent** | | | | |
| `SYS-AGENT-001` | Restart agent **con** volume | tutte | stessa `instance_id`, serie continua | — |
| `SYS-AGENT-002` | Restart agent **senza** volume | container-no-volume | **istanza duplicata** — regressione nota documentata | — |
| `SYS-AGENT-003` | Disco buffer pieno (tmpfs 16MB) | standalone | `samples_dropped_total` cresce, agent vivo, DB non impattato | `WEB-FLEET-003` |
| `SYS-AGENT-004` | Clock skew di 5 minuti | standalone | `clock_skew` alzato, sample non rifiutati sotto la soglia | — |
| `SYS-AGENT-005` | Revoca dell'agent dal server | standalone | 401 al push successivo, l'agent si ferma e lo dichiara | `WEB-SET-002` |
| `SYS-AGENT-006` | Agent con versione di protocollo obsoleta | standalone | rifiuto esplicito con versione minima richiesta | — |
| **Permessi** | | | | |
| `SYS-PERM-001` | Solo T0 | rds-like | tutti i check T0 verdi, funzioni T1/T2 disattivate a monte | `WEB-PERM-001` |
| `SYS-PERM-002` | T0 senza `pg_control_system()` | rds-like | fallback a `cluster_id` manuale, warning dichiarato | `WEB-PERM-002` |
| `SYS-PERM-003` | T2 concesso | standalone | kill query funziona davvero | `WEB-LOCK-003` |
| **Pooler** | | | | |
| `SYS-POOL-001` | Saturazione pgbouncer (`pool_size` superato) | pgbouncer | `cl_waiting` > 0, `maxwait` cresce, alert | `WEB-POOL-001` |
| `SYS-POOL-002` | Transaction pooling attivo | pgbouncer | `pg_stat_activity` mostra il pooler; la UI lo segnala | `WEB-POOL-002` |
| **Multi-DB** | | | | |
| `SYS-DB-001` | 15 database, budget 10 | standalone | i 10 più attivi monitorati, "5 non monitorati" esposto | `WEB-DB-001` |
| `SYS-DB-002` | Database creato a caldo | standalone | rilevato entro un ciclo di discovery | — |
| `SYS-DB-003` | Database eliminato a caldo | standalone | nessun errore, serie terminate pulitamente | — |

**Smoke set per PR** (~10 min): `SYS-REPL-001`, `SYS-RESET-001`, `SYS-NET-001`, `SYS-NET-003`, `SYS-LOAD-002`, `SYS-LOAD-008`, `SYS-PERM-001`, `SYS-AGENT-001`.
**Full set**: nightly, tutte le topologie × `AGENT_MODE` × versioni PG.

---

## 9. Generatore di carico e guasti

`test/workload/` è un binario Go (`workloadctl`) che produce patologie **deterministiche** e verificabili.

```bash
workloadctl deadlock     --dsn … --pairs 10 --duration 30s
workloadctl lock-storm   --dsn … --sessions 50 --table t --duration 20s
workloadctl slow-query   --dsn … --sleep 30s --count 3
workloadctl idle-in-txn  --dsn … --sessions 5 --hold 10m
workloadctl race         --dsn … --workers 20 --rows 100 --isolation repeatable-read
workloadctl bloat        --dsn … --table t --updates 500000
workloadctl distinct-queries --dsn … --count 5000
workloadctl oltp         --dsn … --tps 200 --duration 5m   # rumore di fondo realistico
```

**Note di implementazione che contano:**

- **`deadlock`**: due transazioni che aggiornano due righe in ordine opposto, con barriera di sincronizzazione tra le goroutine. Senza la barriera il deadlock si verifica in modo intermittente e il test diventa flaky.
- **`distinct-queries`**: per ottenere 5000 `queryid` distinti servono query **strutturalmente** diverse — `pg_stat_statements` normalizza i letterali, quindi `SELECT 1`, `SELECT 2`… collassano in un solo id. Il generatore emette espressioni di lunghezza crescente (`SELECT 1+1`, `SELECT 1+1+1`, …): parse tree diversi, id distinti garantiti su **tutte** le versioni supportate. (Le liste `IN` di lunghezza variabile non vanno bene: PG18 le comprime.)
- **`race`**: worker concorrenti in `REPEATABLE READ` sulle stesse righe → errori di serializzazione + attese di lock reali, non simulate.
- **`bloat`**: `ALTER TABLE … SET (autovacuum_enabled = false)` prima degli UPDATE, altrimenti autovacuum ripulisce e il test non osserva nulla.
- **`oltp`**: usa `pgbench` con uno script custom quando basta; il binario Go serve dove serve controllo fine sulla concorrenza.

Ogni comando emette su stdout un **report JSON** (quanti deadlock effettivi, quanti errori di serializzazione, quante query distinte): il test asserisce sul report, non sulla speranza che il carico abbia funzionato.

**Wraparound XID:** consumare un miliardo di transazioni non è praticabile in CI. Si testa la **logica** dell'advisor a L1 con `xid_age` iniettato, e si dichiara nei limiti noti che la soglia di wraparound non è verificata end-to-end.

---

## 10. Determinismo e politica anti-flake

### 10.1 Regole

1. **Mai `sleep`.** Sempre attesa su condizione con timeout. Un `time.Sleep` in un test è motivo di richiesta di modifica in review, senza discussione.
2. **Mai dipendenze dall'ordine.** Ogni test crea e distrugge il proprio stato. `go test -shuffle=on` è attivo in CI.
3. **Mai risorse condivise tra test paralleli.** Porte assegnate dinamicamente, `cluster_id` per test, database per test.
4. **Il tempo è iniettabile** a ogni livello (`clock.Clock` in Go, clock fissato del server per le fixture Playwright).
5. **Casualità con seed esplicito**, stampato all'inizio del test così un fallimento è riproducibile.

### 10.2 Retry

| Livello | Retry | Motivo |
|---|---|---|
| L1, L2, L4 | **0** | non c'è niente di legittimamente non deterministico |
| L3 | **0** | un E2E backend flaky segnala una race reale nel prodotto, non nel test |
| L5 | **1** in CI | i browser hanno una quota irriducibile di non determinismo — ma ogni retry viene **registrato** |

### 10.3 Gestione della flakiness

- Ogni retry o fallimento intermittente viene registrato in un report aggregato (`results.json` → job di raccolta).
- **> 2 flake in 7 giorni** su uno stesso test → apertura automatica di una issue con label `flaky-test` e **priorità pari a un bug funzionale**.
- Il test viene spostato in `quarantine/`: continua a girare nightly, non blocca più le PR, e ha un **owner e una scadenza**.
- **Un test in quarantena per più di 14 giorni viene cancellato.** Un test disabilitato che nessuno ripara è peggio di nessun test: dà l'illusione di copertura. La cancellazione è deliberatamente sgradevole, così qualcuno lo ripara.

### 10.4 Anti-pattern esplicitamente vietati

- ❌ `time.Sleep` / `page.waitForTimeout`
- ❌ asserzioni su ordinamento non deterministico (mappe Go, righe SQL senza `ORDER BY`)
- ❌ asserzioni su timestamp assoluti (`ts == "2026-01-01T00:00:00Z"`) invece che su intervalli
- ❌ selettori CSS strutturali o classi Tailwind in Playwright
- ❌ test che dipendono dall'esecuzione di un test precedente
- ❌ `t.Skip()` senza una condizione verificata (vedi il pattern in sez. 4.3: lo skip afferma *perché* salta)

---

## 11. CI — GitHub Actions

### 11.1 Strategia

| Trigger | Esegue | Budget |
|---|---|---|
| push su branch | L1 + lint | < 3 min |
| PR | L1, L2 (PG 15/18), L3 smoke, L4, L5 smoke (chromium, fixture) | **< 15 min wall clock**, job paralleli |
| merge su `main` | tutto quanto sopra + build e push immagini `:edge` | < 20 min |
| nightly | matrice completa: PG 15→18 × topologie × `AGENT_MODE`, L5 full su 3 browser, fuzzing esteso, visual regression | ~90 min |
| tag di release | tutto + build multi-arch + smoke test sugli artefatti pubblicati | ~2 h |

### 11.2 Workflow PR

```yaml
# .github/workflows/pr.yml
name: PR
on: pull_request

concurrency:
  group: pr-${{ github.event.pull_request.number }}
  cancel-in-progress: true

jobs:
  changes:
    runs-on: ubuntu-latest
    outputs:
      go:  ${{ steps.f.outputs.go }}
      web: ${{ steps.f.outputs.web }}
    steps:
      - uses: actions/checkout@v4
      - uses: dorny/paths-filter@v3
        id: f
        with:
          filters: |
            go:  ['**/*.go', 'go.mod', 'go.sum', 'test/**']
            web: ['web/**']

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.26', cache: true }
      - uses: golangci/golangci-lint-action@v6
      - run: make lint-web

  unit-go:                       # L1
    needs: changes
    if: needs.changes.outputs.go == 'true'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.26', cache: true }
      - run: go test -race -shuffle=on -coverprofile=cover.out ./...
      - run: make coverage-gate       # fallisce sotto le soglie di sez. 3.5
      - run: go test -run=Fuzz -fuzz=. -fuzztime=30s ./internal/pg/...

  integration-go:                # L2
    needs: changes
    if: needs.changes.outputs.go == 'true'
    runs-on: ubuntu-latest
    strategy:
      fail-fast: false
      matrix:
        pg: ['15', '18']
        profile: ['vanilla', 'rds-like']
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.26', cache: true }
      - run: go test -tags=integration -race ./internal/... 
        env:
          PG_VERSION: ${{ matrix.pg }}
          PG_PROFILE: ${{ matrix.profile }}

  component-web:                 # L4
    needs: changes
    if: needs.changes.outputs.web == 'true'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: '22', cache: 'pnpm', cache-dependency-path: web/pnpm-lock.yaml }
      - run: pnpm install --frozen-lockfile
      - run: make web-test
      - run: make web-coverage-gate

  e2e-system:                    # L3 smoke
    needs: [unit-go]
    runs-on: ubuntu-latest
    timeout-minutes: 20
    strategy:
      fail-fast: false
      matrix:
        agent_mode: ['container', 'binary']
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.26', cache: true }
      - run: make build-images
      - run: go test -tags=e2e -timeout=20m ./test/e2e/... -run 'Smoke'
        env:
          AGENT_MODE: ${{ matrix.agent_mode }}
          PG_VERSION: '16'
      - uses: actions/upload-artifact@v4
        if: failure()
        with:
          name: e2e-dump-${{ matrix.agent_mode }}
          path: test/e2e/_artifacts/

  e2e-web:                       # L5 smoke
    needs: [unit-go, component-web]
    runs-on: ubuntu-latest
    timeout-minutes: 20
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: '22', cache: 'pnpm', cache-dependency-path: web/pnpm-lock.yaml }
      - run: make build-images
      - run: make e2e-stack-up            # stack L3 + scenariod + web
      - run: pnpm exec playwright install --with-deps chromium
        working-directory: web
      - run: pnpm exec playwright test --project=chromium
        working-directory: web
        env: { APP_URL: 'http://localhost:3000', SCENARIOD_URL: 'http://localhost:9900' }
      - uses: actions/upload-artifact@v4
        if: failure()
        with:
          name: playwright-report
          path: test/e2e/_artifacts/ui/     # include le trace
      - run: make e2e-stack-down
        if: always()
```

### 11.3 Note operative

- **`fail-fast: false`** su tutte le matrici: sapere che 3 versioni PG su 6 falliscono è informazione diagnostica; fermarsi alla prima la butta via.
- **Artefatti su fallimento sempre** (dump E2E, report Playwright con trace). Un fallimento CI senza artefatti non è azionabile.
- **`concurrency` con `cancel-in-progress`**: un push nuovo annulla il run vecchio della stessa PR.
- **Cache**: moduli Go, `node_modules`, layer Docker (buildx con cache GHA). Senza, i job E2E raddoppiano la durata.
- **Immagini**: costruite una volta in `make build-images` con tag basato sullo SHA, riusate da tutti i job E2E.

---

## 12. Comandi locali

Tutto ciò che gira in CI deve girare in locale con **un solo comando**, senza configurazione preliminare. Se un fallimento CI non è riproducibile in locale, è un bug dell'infrastruttura di test.

```makefile
make test                # L1 (default: veloce, si usa in loop mentre si sviluppa)
make test-integration    # L2, PG_VERSION=16 di default
make test-e2e            # L3 smoke
make test-e2e-full       # L3 completo (lungo)
make web-test            # L4/L2 UI con Vitest
make web-coverage-gate   # L4/L2 UI con floor V8
make test-ui-e2e         # L5, browser reale contro lo stack locale
make test-all            # tutto, come su main

make e2e-stack-up        # alza lo stack e lo lascia in piedi per esplorazione manuale
make e2e-stack-down
make scenario ID=SYS-REPL-001   # esegue uno scenario sullo stack attivo
make ui                  # Playwright in modalità UI interattiva
make trace F=…           # apre una trace Playwright scaricata da CI

make golden              # rigenera i golden file
make fixture-dataset     # rigenera il dump deterministico per L5 fixture mode
make coverage            # report HTML
make lint
```

**Flusso di sviluppo consigliato:** `make e2e-stack-up` una volta, poi `make scenario ID=…` e `make ui` per lavorare contro uno stack vivo, senza ripagare l'avvio a ogni iterazione.

---

## 13. Quality gate

Una PR è mergiabile solo se:

- [ ] `lint` verde (golangci-lint, eslint, tsc, `gofmt`)
- [ ] L1 verde con `-race` e `-shuffle=on`
- [ ] Gate di copertura rispettati (sez. 3.5, 6.3)
- [ ] L2 verde su PG 15/18 × profili `vanilla` e `rds-like`
- [ ] L3 smoke verde su `AGENT_MODE` `container` e `binary`
- [ ] L4 e L5 smoke verdi
- [ ] Nessun test aggiunto in `quarantine/` senza una issue collegata
- [ ] Ogni golden file modificato è **spiegato nella descrizione della PR**
- [ ] Nuove feature: voce di tracciabilità aggiunta in sez. 14
- [ ] Nuovi limiti o degradazioni: aggiunti a `IDEA.md` sez. 11 **con il test che li dimostra**

**Un fallimento in `main` blocca i merge** finché non è risolto o il commit è revertito. Nessuna eccezione: una suite rossa tollerata per due giorni smette di essere letta, e da lì in poi non protegge più niente.

---

## 14. Tracciabilità requisito → test

Ogni requisito critico di `IDEA.md` ha un test identificabile. Tabella mantenuta a ogni PR che tocca il comportamento.

| Requisito (`IDEA.md`) | Livello | Test |
|---|---|---|
| §2.2 `cluster_id` da `system_identifier`, stabile a failover | L2, L3 | `INT-IDENT-001`, `SYS-REPL-001` |
| §2.2 `instance_id` persistito, volume obbligatorio | L3 | `SYS-AGENT-001`, `SYS-AGENT-002` |
| §3.5 ASH a 1s, aggregazione, correlazione `query_id` | L1, L2, L3 | `TestASHAggregate_Golden`, `INT-ASH-001`, `SYS-LOAD-003/005` |
| §3.6 Matrice viste per versione | L2 | `TestCheck_*` su matrice PG |
| §4.2 Budget database per istanza | L3 | `SYS-DB-001` |
| §4.4 Timeout per-check (locks sopravvive al lock storm) | L3 | `SYS-LOAD-002` |
| §4.5 Reset detection | L1, L2, L3 | `TestDeltaEngine_ResetDetection`, `INT-RESET-001`, `SYS-RESET-001/002/003` |
| §4.6 Top-N e budget di cardinalità | L1, L3 | `TestTopN_Hysteresis`, `SYS-LOAD-008` |
| §4.6 `queryid` non confrontabile cross-cluster | L2, L5 | `INT-QID-001`, `WEB-QUERY-003` |
| §4.7 Tier permessi, EXPLAIN/kill disabilitati senza tier | L2, L3, L5 | `TestAllChecks_WorkOnT0`, `SYS-PERM-001/003`, `WEB-PERM-001` |
| §4.8 Buffer, drop policy, clock skew | L3 | `SYS-NET-001`, `SYS-AGENT-003/004` |
| §5.1 Cascata, orphan, split-brain | L1, L3 | `TestTopology_*`, `SYS-REPL-002/003/005` |
| §5.4 Alert di staleness (Tier 0) | L3 | `SYS-NET-003`, `SYS-AGENT-005` |
| §5.5 Leader election, nessuna doppia notifica | L3 | `SYS-HA-001` |
| §6 Onestà UI: gap ≠ zero | L4, L5 | `MetricChart gap`, `WEB-CHART-001` |
| §7.4 Enrollment, rotazione, revoca | L3, L5 | `SYS-AGENT-005`, `WEB-SET-001/002` |
| §11 Ogni limite noto | vari | una riga per limite, aggiunta insieme al limite |

---

## 15. Cosa NON testiamo (e perché)

Dichiarato per evitare che qualcuno lo scopra e lo consideri una dimenticanza.

| Non testato | Perché | Mitigazione |
|---|---|---|
| RDS / Aurora reali | nessuna credenziale AWS al momento | profilo `rds-like` (sez. 4.4) copre i vincoli di permessi. Dichiarato in `IDEA.md` §11 |
| XID wraparound end-to-end | consumare 10⁹ transazioni non è praticabile in CI | logica dell'advisor testata a L1 con `xid_age` iniettato |
| Scala reale (100 DB, 50 agent) | costo infrastrutturale | test di carico dedicato con agent sintetici che generano payload realistici |
| Postgres ≤ 12 | fuori supporto (§1) | il gate di versione è testato: un PG12 viene rifiutato con messaggio chiaro |
| Browser mobili | la dashboard è desktop-first | layout responsive verificato solo a due viewport in L5 |
| Upgrade da versione precedente del prodotto | non esiste ancora una v1 | da introdurre alla prima release stabile: test di migrazione schema N-1 → N |
| Backup/restore del TimescaleDB | responsabilità dell'operatore | documentato, non testato |

---

## 16. Come aggiungere un test

**Nuovo check Postgres**
1. L1 per il parsing e l'aggregazione (fixture di righe in memoria)
2. L2 con la query reale sulla matrice di versioni, incluso lo skip **verificato** per le versioni non supportate
3. L2 nel profilo permessi: verificare che il tier dichiarato sia quello davvero necessario
4. Golden payload aggiornato (`make golden`), diff spiegato in PR
5. L3 solo se il check ha comportamento sotto guasto (timeout, reset, ruolo che cambia)

**Nuovo scenario di guasto**
1. Registrarlo in `test/scenario/` con ID, topologia, `Covers`, `Expect`
2. Aggiungerlo al catalogo di sez. 8
3. Test L3 che lo esegue e asserisce sull'API
4. Test L5 che verifica la controparte visiva, se lo scenario ha una manifestazione in UI
5. Decidere se entra nello smoke set (criterio: è un rischio ricorrente e costa meno di 60s?)

**Nuova pagina o componente frontend**
1. Logica estratta in `web/src/lib/` → test Vitest puri
2. Client Component → L4 con Testing Library, inclusi gli stati vuoto/errore/gap
3. L5 solo per i **flussi**: navigazione, form completo, interazione con dati reali
4. `data-testid` solo nei tre casi ammessi dalla regola T-3 nel documento frontend
5. Controllo axe sulla nuova pagina

---

*Documento vivo. Ogni volta che un bug sfugge ai test, la domanda in post-mortem è: **a quale livello sarebbe dovuto essere intercettato, e perché non c'era?** La risposta diventa un test e, se serve, una modifica a questo documento.*

---

## 17. Architettura dei test frontend

Le regole normative per i test in `web/` sono in [`web/docs/testing.md`](web/docs/testing.md).
Sono dieci regole, T-1…T-10, e si applicano a ogni nuova pagina o componente.

Nel frontend i quattro tipi di test si mappano sui livelli del repository così:

| Tipo frontend | Livello | Scopo |
|---|---|---|
| **pure** | L1 | Funzioni deterministiche in `src/lib/`, senza React o DOM |
| **component** | L1 | Un albero React con jsdom, Testing Library e MSW |
| **route** | L2 UI | Una pagina completa con router e query client reali |
| **acceptance** | L3 | Browser reale contro uno stack reale con Playwright |

Le attese usano condizioni (`findBy…`, `waitFor`) o avanzamento esplicito dei
fake timer; non si aggiungono `sleep` o timeout arbitrari. Le route test devono
inoltre verificare l'accessibilità tramite `expectNoA11yViolations`.
