# pglens — Quick start

Questa guida porta da zero a dati visibili nell'interfaccia di pglens
monitorando **una singola istanza PostgreSQL 18 avviata con Docker**. Tutto gira
su un solo host Docker:

- **pglens-server**: ingest, API e interfaccia web, con il proprio storage
  TimescaleDB;
- **pglens-agent**: il client che legge l'istanza PostgreSQL 18 e invia i
  campioni al server;
- **pg18**: l'istanza monitorata.

```
                    rete docker "pglens-net"
 ┌──────────────┐  SQL read-only   ┌───────────────┐  HTTP push   ┌───────────────┐
 │ pg18         │ ◄─────────────── │ pglens-agent  │ ───────────► │ pglens-server │
 │ PostgreSQL 18│  pglens_monitor  │ :9187 healthz │  bearer token│ :8080 API + UI│
 └──────────────┘                  └───────────────┘              └───────┬───────┘
                                                                          │ rete del progetto
                                                                  ┌───────▼───────┐
                                                                  │ timescaledb   │
                                                                  │ storage pglens│
                                                                  └───────────────┘
```

L'agent lavora **sempre in sola lettura** sull'istanza monitorata. Si collega
con un ruolo dedicato (`pglens_monitor`), che non ha nessun privilegio di
scrittura e ha `default_transaction_read_only = on`. L'agent stesso apre ogni
connessione in sola lettura. Il server scrive solo nel proprio storage
TimescaleDB, mai nell'istanza monitorata.

Per i dettagli completi vedi il [README](README.md) e la
[guida ai livelli di permesso](deploy/sql/README.md).

## 0. Prerequisiti

- Docker Engine con il plugin Compose v2 (`docker compose version`).
- `curl` e `jq` per le verifiche. `psql` non serve sull'host: si usa quello
  dentro il container `pg18`.
- Una copia di questo repository, per lo script SQL
  `deploy/sql/monitoring_user.sql`. Nei comandi la si assume in `$PGLENS_REPO`.
- Le immagini di pglens. Le release alpha sono pubblicate su GHCR:

  ```sh
  docker pull ghcr.io/manprint/pglens-server:v0.1.0-alpha.6
  docker pull ghcr.io/manprint/pglens-agent:v0.1.0-alpha.6
  ```

  In alternativa si costruiscono dal repository: `make build-images` produce
  `ghcr.io/manprint/pglens-{server,agent}:dev`. In quel caso imposta
  `PGLENS_VERSION=dev` nei file `.env` qui sotto.

Scegli i segreti una volta sola e riusali a ogni avvio: un volume già
inizializzato tiene le password con cui è nato.

```sh
export PGLENS_REPO=/percorso/del/repo/pglens
export PG18_SUPERUSER_PASSWORD='<password-superuser-pg18>'
export PGLENS_MONITORING_PASSWORD='<password-ruolo-di-monitoraggio>'
export PGLENS_DB_PASSWORD='<password-storage-timescaledb>'   # solo lettere, cifre e - _ . ~
export PGLENS_BOOTSTRAP_TOKEN="$(openssl rand -hex 32)"     # segreto condiviso agent ↔ server
export PGLENS_UI_PASSWORD='<password-interfaccia-web>'
```

`PGLENS_DB_PASSWORD` finisce così com'è nel DSN del server. Usa quindi solo
lettere, cifre e `- _ . ~`: uno spazio o `/` vengono rifiutati, `@` sposta
l'host e `%` apre una sequenza di escape.

## 1. Rete e istanza PostgreSQL 18

Crea una rete Docker condivisa fra l'istanza, l'agent e il server:

```sh
docker network create pglens-net
```

Avvia PostgreSQL 18 con `pg_stat_statements` precaricato. È facoltativo, ma
senza di esso il check `stat_statements` e le regole advisor sulle query
restano `degraded`. Aggiungi anche `track_io_timing`, che l'advisor consiglia:

```sh
docker run -d --name pg18 --network pglens-net \
  -e POSTGRES_PASSWORD="$PG18_SUPERUSER_PASSWORD" \
  -v pg18-data:/var/lib/postgresql \
  -p 127.0.0.1:5432:5432 \
  postgres:18 \
  -c shared_preload_libraries=pg_stat_statements \
  -c compute_query_id=on \
  -c track_io_timing=on
```

Da PostgreSQL 18 l'immagine ufficiale tiene i dati in
`/var/lib/postgresql/18/docker`, quindi il volume va montato su
`/var/lib/postgresql`, non più su `/var/lib/postgresql/data`.

**Hai già un container PostgreSQL 18?** Collegalo alla rete con
`docker network connect pglens-net <nome-container>` e usa quel nome al posto
di `pg18` nei passi seguenti. Per precaricare `pg_stat_statements` su
un'istanza esistente lancia questi due comandi, poi riavvia il container:

```sh
docker exec <nome-container> psql -U postgres \
  -c "ALTER SYSTEM SET shared_preload_libraries = 'pg_stat_statements'"
docker restart <nome-container>
```

Verifica `SHOW shared_preload_libraries` prima di farlo: `ALTER SYSTEM`
sostituisce il valore, non lo estende.

Attendi che l'istanza accetti connessioni:

```sh
until docker exec pg18 pg_isready -h 127.0.0.1 -U postgres; do sleep 1; done
```

## 2. Ruoli e query sul database monitorato

Questo è l'unico passo che modifica l'istanza monitorata. Lo esegue un DBA
come superuser, una volta sola. Riesegue lo stesso script per ogni nuovo
database o per cambiare livello. Dopo questo passo pglens si limita a leggere.

### 2.1 Il ruolo `pglens_monitor` (livello T0, il default)

Lo script `deploy/sql/monitoring_user.sql` crea il ruolo di login e gli dà solo
privilegi di lettura. È idempotente e gira in un'unica transazione.

Prima di concedere qualsiasi privilegio, se il ruolo esiste già lo script
controlla che non possa scrivere. Lo rifiuta, senza cambiare nulla, in questi
casi:

- è superuser o ha `CREATEDB`, `CREATEROLE`, `REPLICATION` o `BYPASSRLS`;
- è membro di ruoli diversi da quelli predefiniti di sola lettura;
- possiede oggetti;
- ha un qualsiasi privilegio di scrittura.

Il rifiuto elenca lo statement che rimuove ciascun problema.

Lo script va eseguito **connesso a ogni database che accetta connessioni**. Il
`GRANT CONNECT` e le revoche da `PUBLIC` sono infatti per-database. Questo ciclo
lo esegue in tutti, compreso `template1`, così anche i database creati in
seguito ereditano le revoche:

```sh
for db in $(docker exec pg18 psql -U postgres -XAt \
    -c "SELECT datname FROM pg_database WHERE datallowconn ORDER BY datname"); do
  echo "== $db"
  docker exec -i pg18 psql -U postgres -X -v ON_ERROR_STOP=1 -d "$db" \
    -v pglens_password="$PGLENS_MONITORING_PASSWORD" \
    -v role=pglens_monitor \
    -v dbname="$db" \
    -f - < "$PGLENS_REPO/deploy/sql/monitoring_user.sql" || break
done
```

Ogni esecuzione termina con `COMMIT`. Le `NOTICE` del tipo
`pglens_monitor, like every role, can write outside a read-only transaction
through: temporary tables (...)` sono attese: sono i grant a `PUBLIC` descritti
più sotto, che lo script segnala ma non tocca.

Lo script, in sostanza, esegue queste istruzioni. Sono riportate per
trasparenza: usa lo script, perché fa anche i controlli di sicurezza descritti
sopra.

```sql
-- Ruolo di login, senza attributi privilegiati.
CREATE ROLE pglens_monitor LOGIN;
ALTER ROLE pglens_monitor LOGIN PASSWORD '<password>';

-- Livello T0: statistiche e viste di sistema in sola lettura.
GRANT pg_monitor TO pglens_monitor;      -- include pg_read_all_settings, pg_read_all_stats, pg_stat_scan_tables
GRANT CONNECT ON DATABASE <db> TO pglens_monitor;

-- Identità stabile del cluster (system_identifier), anche dove PUBLIC è stato revocato.
GRANT EXECUTE ON FUNCTION pg_control_system()     TO pglens_monitor;
GRANT EXECUTE ON FUNCTION pg_control_checkpoint() TO pglens_monitor;

-- Ogni sessione del ruolo nasce read-only e con limiti stretti.
ALTER ROLE pglens_monitor SET default_transaction_read_only = on;
ALTER ROLE pglens_monitor SET statement_timeout = '15s';
ALTER ROLE pglens_monitor SET lock_timeout = '1s';
ALTER ROLE pglens_monitor SET idle_in_transaction_session_timeout = '30s';
ALTER ROLE pglens_monitor SET application_name = 'pglens';

-- In ogni database: chiude le due scritture che il read-only non ferma.
REVOKE EXECUTE ON FUNCTION pg_catalog.lo_create(oid), pg_catalog.lo_creat(integer),
                           pg_catalog.lo_from_bytea(oid, bytea) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION pg_catalog.pg_logical_emit_message(boolean, text, text) FROM PUBLIC;
-- ... e lo stesso per ogni altra variante di pg_logical_emit_message.
```

Lo script **segnala**, senza modificarli, gli altri grant a `PUBLIC` che
permetterebbero a qualunque ruolo di scrivere fuori da una transazione
read-only: tabelle temporanee, `CREATE` su database o schema, scritture su
tabelle o sequence. Revocarli cambia ciò che possono fare tutti gli altri
ruoli: un'applicazione che usa tabelle temporanee, per esempio, avrebbe poi
bisogno di un grant esplicito. Su un'istanza dedicata o nuova puoi chiuderli
aggiungendo `-v harden_public=1` al comando del ciclo.

### 2.2 Estensioni (facoltative, livello T3)

pglens non crea mai estensioni: è un'azione esplicita del DBA. Con
`pg_stat_statements` precaricato (passo 1), crealo **in un solo database**:
quello del DSN dell'agent, qui `postgres`. Le sue statistiche coprono tutti i
database e l'agent se ne accorge entro 30 s, senza riavvio.

```sh
docker exec pg18 psql -U postgres -d postgres \
  -c "CREATE EXTENSION IF NOT EXISTS pg_stat_statements"
```

`pgstattuple` serve solo per la misura del bloat su richiesta, che richiede T1.
Va creato in ogni database le cui tabelle si vogliono misurare:
`CREATE EXTENSION IF NOT EXISTS pgstattuple;`.

### 2.3 Livelli superiori (facoltativi)

Il livello T0 basta per tutto il monitoraggio schedulato e per tutte le regole
advisor. I livelli superiori abilitano solo le operazioni su richiesta, e
restano spenti finché non li abiliti anche nella configurazione dell'agent:

| Livello | Flag dello script | Grant aggiunto | Abilita |
|---------|-------------------|----------------|---------|
| T1 | `-v tier1=1` | `pg_read_all_data` | `EXPLAIN` senza ANALYZE su richiesta, `pgstattuple`, stime di bloat su tutte le tabelle |
| T2 | `-v tier1=1 -v tier2=1` | `pg_signal_backend` | cancel/terminate di una sessione, solo con `allow_signal: true` |

`EXPLAIN ANALYZE` esegue davvero la query, quindi resta disabilitato a meno di
`allow_explain_analyze: true` sul target. Per una configurazione strettamente
in sola lettura lascia il ruolo a T0 e imposta `commands.enabled: false`
nell'agent (passo 4).

### 2.4 Verifica del ruolo

Controlla gli attributi del ruolo, le appartenenze e i default di sessione:

```sh
docker exec pg18 psql -U postgres -X -c "
  SELECT rolname, rolsuper, rolcreatedb, rolcreaterole, rolreplication, rolbypassrls, rolconfig
  FROM pg_roles WHERE rolname = 'pglens_monitor';" -c "
  SELECT r.rolname AS membro_di
  FROM pg_auth_members m JOIN pg_roles r ON r.oid = m.roleid
  WHERE m.member = 'pglens_monitor'::regrole;"
```

Atteso:

- tutti gli attributi a `f`;
- `rolconfig` con `default_transaction_read_only=on` e i timeout;
- unica appartenenza `pg_monitor`, più `pg_read_all_data` e
  `pg_read_all_stats` a T1 e `pg_signal_backend` a T2.

Prova poi che il ruolo non può scrivere:

```sh
docker exec -e PGPASSWORD="$PGLENS_MONITORING_PASSWORD" pg18 \
  psql -h 127.0.0.1 -U pglens_monitor -d postgres -X \
  -c "SHOW default_transaction_read_only" \
  -c "CREATE TABLE pglens_probe (id int)"
```

Il `SHOW` risponde `on` e il `CREATE TABLE` deve fallire con
`ERROR:  cannot execute CREATE TABLE in a read-only transaction`.

## 3. Deploy e configurazione del server

Il server usa uno storage **TimescaleDB separato**: non va mai puntato
sull'istanza monitorata. All'avvio applica le migrazioni (solo in avanti,
idempotenti) e ascolta su `:8080` con API e interfaccia web.

Crea una directory, per esempio `~/pglens-server`, con questi due file.

`~/pglens-server/.env`:

```sh
PGLENS_VERSION=v0.1.0-alpha.6
PGLENS_DB_PASSWORD=<come sopra>
PGLENS_BOOTSTRAP_TOKEN=<come sopra>
PGLENS_UI_PASSWORD=<come sopra>
```

`~/pglens-server/compose.yml`:

```yaml
name: pglens-server
services:
  timescaledb:
    image: timescale/timescaledb:2.31.0-pg17
    environment:
      POSTGRES_DB: pglens
      POSTGRES_USER: pglens
      POSTGRES_PASSWORD: ${PGLENS_DB_PASSWORD:?set PGLENS_DB_PASSWORD}
    volumes:
      - pglens-db-data:/var/lib/postgresql/data
    healthcheck:
      # TCP, non socket: durante l'initdb il server temporaneo non ascolta sulla porta.
      test: ["CMD-SHELL", "pg_isready -h 127.0.0.1 -U pglens -d pglens"]
      interval: 5s
      timeout: 5s
      retries: 20
      start_period: 90s

  pglens-server:
    image: ghcr.io/manprint/pglens-server:${PGLENS_VERSION:-v0.1.0-alpha.6}
    depends_on:
      timescaledb:
        condition: service_healthy
    environment:
      PGLENS_DSN: postgres://pglens:${PGLENS_DB_PASSWORD}@timescaledb:5432/pglens?sslmode=disable
      PGLENS_LISTEN: ":8080"
      PGLENS_BOOTSTRAP_TOKEN: ${PGLENS_BOOTSTRAP_TOKEN:?set PGLENS_BOOTSTRAP_TOKEN}
      PGLENS_UI_PASSWORD: ${PGLENS_UI_PASSWORD:?set PGLENS_UI_PASSWORD}
      PGLENS_ALERT_INTERVAL: 30s
      PGLENS_ADVISOR_INTERVAL: 15m
    networks: [default, pglens-net]
    ports:
      - "127.0.0.1:8080:8080"
    read_only: true
    tmpfs: [/tmp]
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    healthcheck:
      test: ["CMD", "/usr/local/bin/pglens-server", "--healthcheck"]
      interval: 5s
      timeout: 5s
      retries: 20
      start_period: 5s

networks:
  pglens-net:
    external: true

volumes:
  pglens-db-data:
```

Avvia e verifica:

```sh
cd ~/pglens-server
docker compose up -d
curl -fsS http://127.0.0.1:8080/readyz     # → ok (migrazioni applicate, storage raggiungibile)
```

### Configurazione del server

Le variabili principali:

| Variabile | Default | Significato |
|-----------|---------|-------------|
| `PGLENS_DSN` | — | DSN dello storage TimescaleDB (obbligatorio) |
| `PGLENS_LISTEN` | `:8080` | indirizzo HTTP di API e UI |
| `PGLENS_BOOTSTRAP_TOKEN` / `_FILE` | — | segreto condiviso con gli agent, obbligatorio: senza, il server non parte |
| `PGLENS_UI_PASSWORD` / `_FILE` | — | password dell'interfaccia; senza, il browser riceve `503 ui_password_not_configured` |
| `PGLENS_UI_SESSION_TTL` | `24h` | durata della sessione web (da `5m` a `720h`) |
| `PGLENS_UI_COOKIE_SECURE` | `auto` | attributo `Secure` del cookie: `auto` dietro TLS o `X-Forwarded-Proto: https` |
| `PGLENS_ALERT_INTERVAL` | `30s` | cadenza di valutazione degli alert |
| `PGLENS_ADVISOR_INTERVAL` | `15m` | cadenza dell'advisor |
| `PGLENS_COMMAND_TTL` | `5m` | durata di un comando su richiesta prima della scadenza |
| `PGLENS_ALERT_SLACK_WEBHOOK_URL` / `_FILE` | — | notifiche Slack |
| `PGLENS_WEBHOOK_URL` | — | notifiche su webhook generico |

La retention è una costante delle migrazioni, non una variabile: 30 giorni di
metriche grezze, con compressione TimescaleDB oltre le 48 h.

La porta è pubblicata solo su loopback. Per raggiungere il server da altri
host, o da agent su altre macchine, mettilo dietro un reverse proxy TLS:
token e cookie di sessione viaggiano in chiaro su HTTP.

## 4. Avvio dell'agent (client) per l'istanza PostgreSQL 18

### 4.1 Diagnostica preventiva

Prima di avviare l'agent, verifica connessione e permessi con il sottocomando
`check`, che non scrive nulla:

```sh
docker run --rm --network pglens-net \
  -e PGPASSWORD="$PGLENS_MONITORING_PASSWORD" \
  ghcr.io/manprint/pglens-agent:v0.1.0-alpha.6 \
  check --dsn 'postgres://pglens_monitor@pg18:5432/postgres?sslmode=disable'
```

Atteso:

- `server version : 18.x … supported`;
- `permission tier : T0 (pg_monitor)`;
- `monitoring role : pglens_monitor — read-only`;
- `pg_stat_statements=yes`, se hai eseguito il passo 2.2;
- exit code `0`.

L'avviso sul mount persistente di `/var/lib/pglens` qui è normale: riguarda
solo questo container usa-e-getta.

### 4.2 Configurazione e avvio

Crea una directory, per esempio `~/pglens-agent`, con tre file.

`~/pglens-agent/.env`:

```sh
PGLENS_VERSION=v0.1.0-alpha.6
PGLENS_BOOTSTRAP_TOKEN=<lo stesso del server>
PGLENS_MONITORING_PASSWORD=<password di pglens_monitor>
```

`~/pglens-agent/agent.yaml`:

```yaml
server:
  url: http://pglens-server:8080   # sovrascritto da PGLENS_SERVER_URL, se impostata
identity_path: /var/lib/pglens/identity.json   # deve stare su un volume persistente
push_interval: 15s

host:
  enabled: true
  interval: 30s

buffer:
  path: /var/lib/pglens/buffer   # buffer su disco: nessun dato perso se il server è giù
  max_size: 512MiB
  max_age: 6h

commands:
  enabled: true   # false = agent strettamente passivo: nessun comando su richiesta

targets:
  - name: pg18
    # La password non sta nel DSN: arriva da PGPASSWORD.
    dsn: postgres://pglens_monitor@pg18:5432/postgres?sslmode=disable
    cluster_name: pg18-quickstart   # etichetta di riserva se pg_control_system() non è leggibile
    host_local: false               # l'agent non vede il filesystem dell'istanza
    allow_explain_analyze: false    # EXPLAIN ANALYZE esegue la query: lascialo false
    allow_signal: false             # cancel/terminate: richiede T2 e approvazione
    databases:
      exclude: ["^template\\d$"]
      max: 10                       # al massimo 10 database, i più attivi
```

`~/pglens-agent/compose.yml`:

```yaml
name: pglens-agent
services:
  pglens-agent:
    image: ghcr.io/manprint/pglens-agent:${PGLENS_VERSION:-v0.1.0-alpha.6}
    environment:
      PGLENS_CONFIG: /etc/pglens/agent.yaml
      PGLENS_SERVER_URL: http://pglens-server:8080
      PGLENS_BOOTSTRAP_TOKEN: ${PGLENS_BOOTSTRAP_TOKEN:?set PGLENS_BOOTSTRAP_TOKEN}
      PGPASSWORD: ${PGLENS_MONITORING_PASSWORD:?set PGLENS_MONITORING_PASSWORD}
      PGLENS_HEALTHZ_LISTEN: ":9187"
      HOST_PROC: /host/proc
      HOST_SYS: /host/sys
    volumes:
      - pglens-agent-data:/var/lib/pglens     # identità e buffer: OBBLIGATORIO persistente
      - ./agent.yaml:/etc/pglens/agent.yaml:ro
      - /proc:/host/proc:ro
      - /sys:/host/sys:ro
    networks: [pglens-net]
    ports:
      - "127.0.0.1:9187:9187"
    read_only: true
    tmpfs: [/tmp]
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    healthcheck:
      test: ["CMD", "/usr/local/bin/pglens-agent", "--healthcheck"]
      interval: 5s
      timeout: 5s
      retries: 20
      start_period: 5s

networks:
  pglens-net:
    external: true

volumes:
  pglens-agent-data:
```

Avvia:

```sh
cd ~/pglens-agent
docker compose up -d
docker compose logs -f pglens-agent    # Ctrl-C per uscire
```

Il volume `pglens-agent-data` è indispensabile. Contiene `identity.json` con
l'UUID dell'istanza: senza, ogni riavvio crea una nuova istanza duplicata.

Per un agent su un altro host, imposta `PGLENS_SERVER_URL` all'indirizzo
raggiungibile del server, meglio `https://` dietro proxy, e il DSN del target
all'indirizzo dell'istanza.

## 5. Verifica che i dati arrivino

```sh
# Agent: stato, ultimo errore, ultimo push, buffer (acked cresce, dropped resta 0).
curl -fsS http://127.0.0.1:9187/healthz | jq .

# Server: il cluster (identificato dal system_identifier) e l'istanza
# PostgreSQL 18: role primary, pg_version 18xxxx, perm_tier T0, up true.
curl -fsS -H "Authorization: Bearer $PGLENS_BOOTSTRAP_TOKEN" \
  http://127.0.0.1:8080/api/v1/clusters | jq '.[0] | {cluster_id, id_source, health, instances}'
```

Apri `http://127.0.0.1:8080/` e accedi con `PGLENS_UI_PASSWORD`. L'istanza
compare in **Fleet**:

- **Instance** mostra attività, lock, tabelle, indici, sequence, partizioni,
  trigger ed estensioni, più i dati interni (WAL, SLRU, file temporanei);
- **Wait-event analysis** mostra l'ASH;
- **Queries** mostra `pg_stat_statements`;
- **Advisor** mostra i finding.

Alcuni check hanno cadenze lunghe (estensioni 1 h, bloat 6 h) e l'advisor gira
ogni 15 minuti: i primi finding arrivano dopo qualche minuto.

## 6. Problemi frequenti

| Sintomo | Causa probabile | Rimedio |
|---------|-----------------|---------|
| `check` esce con `1` e `monitoring role … COULD WRITE` | il ruolo esisteva con privilegi in più | esegui gli statement che lo script elenca, poi rilancia lo script |
| `system_identifier: N/A` | `EXECUTE` su `pg_control_system()` revocato | rilancia lo script: concede il grant esplicito |
| `stat_statements disabled — missing extension` | estensione non creata o non precaricata | passo 1 (`shared_preload_libraries`) e passo 2.2 |
| advisor con regole `degraded` | input mancante (estensione, check saltato) | leggi `degraded_reason` nell'interfaccia o nell'API |
| agent `401` sul push | token diverso fra agent e server | allinea `PGLENS_BOOTSTRAP_TOKEN` |
| istanze duplicate dopo un riavvio | `/var/lib/pglens` non persistente | monta il volume `pglens-agent-data` |
| il server esce all'avvio | storage non raggiungibile o token mancante | `docker compose logs pglens-server` |
| `password authentication failed` | `PGPASSWORD` diverso dalla password del ruolo | rilancia lo script con la password giusta |

## 7. Pulizia

```sh
(cd ~/pglens-agent && docker compose down -v)
(cd ~/pglens-server && docker compose down -v)    # -v cancella anche lo storage di pglens
docker rm -f pg18 && docker volume rm pg18-data    # solo se l'istanza era di prova
docker network rm pglens-net
```

Per rimuovere il ruolo da un'istanza che resta in uso, esegui come superuser
`DROP ROLE pglens_monitor;` dopo aver tolto il `CONNECT` in ogni database.
Il ruolo non possiede oggetti, quindi non serve `REASSIGN OWNED`.
