#!/usr/bin/env bash
# Plan 007 reference scenario on an ephemeral local stack (test/parity/README.md).
#
#   scenario.sh up        start the stack, the sinks, the server and the agent
#   scenario.sh workload  run the tagged workload, autovacuum churn, slow statement, deadlock
#   scenario.sh check     run the nine checks; prints one PASS/FAIL/DEVIATION line each
#   scenario.sh down      stop everything and remove the containers, network and volumes
#
# PARITY_DIR (default /tmp/pglens-parity) holds the generated secrets (env, mode
# 0600), the agent state, the sink files and the logs. Secrets are read from
# files and never printed.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
P=${PARITY_DIR:-/tmp/pglens-parity}
URL=${PARITY_URL:-http://127.0.0.1:18080}
COMPOSE=(docker compose --env-file "$P/env" -f "$ROOT/test/parity/compose.yml")
NET=pglens-parity_default
PG_IMAGE=postgres:18.6-alpine
TRACE=4bf92f3577b34da6a3ce929d0e0e4736

secret() { sed -n "s/^$1=//p" "$P/env"; }
rnd() { head -c 32 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 24; }

api() { # api METHOD PATH [JSON]: body on stdout, fails on HTTP >= 400
  local method=$1 path=$2 body=${3:-}
  if [[ -n $body ]]; then
    curl -fsS -X "$method" -H @"$P/auth.hdr" -H 'Content-Type: application/json' --data "$body" "$URL$path"
  else
    curl -fsS -X "$method" -H @"$P/auth.hdr" "$URL$path"
  fi
}

status() { # status METHOD PATH [JSON]: HTTP status only
  local method=$1 path=$2 body=${3:-}
  curl -sS -o /dev/null -w '%{http_code}' -X "$method" -H @"$P/auth.hdr" \
    ${body:+-H 'Content-Type: application/json' --data "$body"} "$URL$path"
}

psql_in() { # psql_in USER DB APPNAME SQL: run inside the monitored container
  docker exec -e PGPASSWORD="$(secret PARITY_APP_PASSWORD)" -e PGAPPNAME="$3" pglens-parity-pg-1 \
    psql -X -q -h 127.0.0.1 -U "$1" -d "$2" -c "$4" >/dev/null
}

psql_out() { # psql_out USER DB APPNAME SQL: run from a second container (another client_addr)
  docker run --rm --network "$NET" -e PGPASSWORD="$(secret PARITY_APP_PASSWORD)" -e PGAPPNAME="$3" \
    "$PG_IMAGE" psql -X -q -h pg -U "$1" -d "$2" -c "$4" >/dev/null
}

# detach NAME CMD...: run CMD in its own session, output in NAME.log, its pid in NAME.pid.
detach() {
  local name=$1
  shift
  setsid sh -c 'echo $$ >"$0.pid"; exec "$@" >>"$0.log" 2>&1 </dev/null' "$name" "$@" &
}

instance() { api GET /api/v1/instances | jq -r '.[0].instance_id'; }

cmd_up() {
  mkdir -p "$P/agent"
  chmod 700 "$P"
  if [[ ! -f $P/env ]]; then
    (umask 077 && for k in STORE_PASSWORD PG_PASSWORD MONITOR_PASSWORD APP_PASSWORD TOKEN UI_PASSWORD; do
      echo "PARITY_$k=$(rnd)"
    done >"$P/env")
  fi
  (umask 077 && printf 'Authorization: Bearer %s\n' "$(secret PARITY_TOKEN)" >"$P/auth.hdr" &&
    secret PARITY_TOKEN >"$P/token")
  "${COMPOSE[@]}" up -d --wait
  (cd "$ROOT" && go build -o "$P/pglens-server" ./cmd/pglens-server && go build -o "$P/pglens-agent" ./cmd/pglens-agent &&
    go build -o "$P/endpoints" ./test/parity/endpoints)

  (cd "$P" && detach endpoints ./endpoints -webhook-file "$P/webhook.jsonl" -mail-file "$P/mail.txt")

  (cd "$P" && detach server env -i PATH="$PATH" HOME="$HOME" \
    PGLENS_DSN="postgres://pglens:$(secret PARITY_STORE_PASSWORD)@127.0.0.1:55432/pglens?sslmode=disable" \
    PGLENS_BOOTSTRAP_TOKEN_FILE="$P/token" PGLENS_LISTEN=127.0.0.1:18080 \
    PGLENS_UI_PASSWORD="$(secret PARITY_UI_PASSWORD)" PGLENS_MCP_ENABLED=true \
    PGLENS_SMTP_HOST=127.0.0.1 PGLENS_SMTP_PORT=12525 PGLENS_SMTP_FROM=pglens@parity.test PGLENS_SMTP_TLS=none \
    PGLENS_TRACE_URL_TEMPLATE='https://traces.example.com/trace/{trace_id}' \
    PGLENS_ALERT_INTERVAL=10s ./pglens-server)
  for _ in $(seq 60); do curl -fsS "$URL/readyz" >/dev/null 2>&1 && break; sleep 1; done
  # Alert channels: the Teams webhook and the email recipient point at the local sinks.
  api PUT /api/v1/notification-channels/teams '{"url":"http://127.0.0.1:18081/teams"}' >/dev/null
  api PUT /api/v1/notification-channels/email '{"url":"oncall@parity.test"}' >/dev/null

  cat >"$P/agent.yaml" <<YAML
server:
  url: $URL
  token_file: $P/token
identity_path: $P/agent/identity.json
push_interval: 10s
host:
  enabled: true
  interval: 15s
  processes: { enabled: true, top_n: 20 }
buffer:
  path: $P/agent/buffer
  max_size: 64MiB
  max_age: 1h
commands:
  enabled: true
targets:
  - name: parity
    dsn: postgres://pglens_monitor@127.0.0.1:55433/postgres?sslmode=disable
    cluster_name: parity
    host_local: true # the agent and the container share this host: host metrics and processes are collected
    allow_explain_analyze: false
    allow_signal: false
    databases:
      include: ["^(postgres|parity)\$"]
      max: 10
checks:
  ash: { interval: 1s, dimensions: [usename, client_addr, application_name, service, endpoint] }
  locks: { interval: 5s, session_query_text: false }
  table_stats: { interval: 30s, top_n: 50 }
  index_stats: { interval: 30s, top_n: 50 }
  stat_statements: { interval: 30s, top_n: 50 }
  settings: { interval: 5m }
  database_stats: { interval: 15s }
  logs: { enabled: true, interval: 10s, source: sql, statement_text: false }
  watch: { enabled: true }
YAML
  (cd "$P" && detach agent env -i PATH="$PATH" HOME="$HOME" PGPASSWORD="$(secret PARITY_MONITOR_PASSWORD)" \
    ./pglens-agent run -config "$P/agent.yaml")
  echo "up: server $URL, sinks 127.0.0.1:18081 (webhook) 127.0.0.1:12525 (smtp)"
}

cmd_workload() {
  local minutes=${1:-5} end tag_a tag_b
  end=$(($(date +%s) + minutes * 60))
  tag_a="/*application='checkout',route='%2Fcart',traceparent='00-$TRACE-00f067aa0ba902b7-01'*/"
  tag_b="/*application='billing',route='%2Finvoice',traceparent='00-$TRACE-00f067aa0ba902b8-01'*/"
  # One slow statement for auto_explain (log_min_duration 200 ms).
  psql_in app_a parity checkout "SELECT pg_sleep(0.4), count(*) FROM orders WHERE note LIKE '%abc%' $tag_a"
  # Churn for autovacuum (naptime 5 s, log_autovacuum_min_duration 0).
  for _ in 1 2 3; do
    psql_in app_b parity billing "INSERT INTO churn (payload) SELECT md5(g::text) FROM generate_series(1, 20000) g; DELETE FROM churn;"
  done
  # Tagged traffic from two users, two application names, two client addresses.
  while (($(date +%s) < end)); do
    psql_in app_a parity checkout "SELECT pg_sleep(0.2), count(*) FROM orders WHERE customer = $((RANDOM % 5000)) $tag_a" &
    psql_out app_b parity billing "SELECT pg_sleep(0.2), count(*) FROM orders WHERE customer = $((RANDOM % 5000)) AND status = 'open' $tag_b" &
    wait
  done
  # Deadlock last: db.deadlock_rate is the rise of a counter between two
  # database_stats scrapes, so it needs a scrape taken before the deadlock.
  # Two sessions take lock_a and lock_b in opposite order.
  psql_in app_a parity checkout "BEGIN; UPDATE lock_a SET v = v + 1 WHERE id = 1; SELECT pg_sleep(1); UPDATE lock_b SET v = v + 1 WHERE id = 1; COMMIT;" &
  psql_out app_b parity billing "BEGIN; UPDATE lock_b SET v = v + 1 WHERE id = 1; SELECT pg_sleep(1); UPDATE lock_a SET v = v + 1 WHERE id = 1; COMMIT;" || true
  wait || true
  echo "workload: done"
}

result() { printf '%-10s step %s: %s\n' "$1" "$2" "$3"; }

check1() { # activity grouped by user, client address, application, service
  local inst=$1 dim n ok=1 detail=
  for dim in usename client_addr application_name service; do
    n=$(api GET "/api/v1/ash?instance_id=$inst&group_by=$dim&from=$FROM&to=$TO" | jq --arg d "$dim" '[.buckets[][$d] | select(. != null and . != "")] | unique | length')
    detail+="$dim=$n "
    ((n >= 2)) || ok=0
  done
  ((ok)) && result PASS 1 "$detail" || result FAIL 1 "$detail"
}

check2() { # firing and resolved db.deadlock_rate alert on the Teams webhook and the SMTP sink
  local hf hr mf mr
  hf=$(grep -c 'WARNING · Deadlock' "$P/webhook.jsonl" 2>/dev/null || true)
  hr=$(grep -c 'RESOLVED · Deadlock' "$P/webhook.jsonl" 2>/dev/null || true)
  mf=$(grep -c '^Subject: \[pglens\] WARNING Deadlocks' "$P/mail.txt" 2>/dev/null || true)
  mr=$(grep -c '^Subject: \[pglens\] RESOLVED Deadlocks' "$P/mail.txt" 2>/dev/null || true)
  hf=${hf:-0} hr=${hr:-0} mf=${mf:-0} mr=${mr:-0}
  local d="teams firing=$hf resolved=$hr, email firing=$mf resolved=$mr"
  if ((hf > 0 && hr > 0 && mf > 0 && mr > 0)); then result PASS 2 "$d"; else result FAIL 2 "$d"; fi
}

check3() { # multixact wraparound rule: registered; no finding at the default threshold
  local inst=$1 rule found
  rule=$(api GET /api/v1/advisor/rules | jq '[.[] | select(.id == "txn.multixact_wraparound_risk" and .enabled)] | length')
  found=$(api GET "/api/v1/findings?instance_id=$inst&rule_id=txn.multixact_wraparound_risk" | jq '[.[] | select(.rule_id == "txn.multixact_wraparound_risk")] | length')
  if ((rule >= 1 && found == 0)); then
    result DEVIATION 3 "rule registered, no finding at the default threshold; the override route only enables or disables a rule, so the crossing is proved by the advisor unit tests"
  else
    result FAIL 3 "rule=$rule findings=$found"
  fi
}

check4() { # yesterday against today on top SQL and on waits
  local inst=$1 q s1 s2
  q="instance_id=$inst&a_from=$(date -u -d '-1 day -1 hour' +%FT%TZ)&a_to=$(date -u -d '-1 day' +%FT%TZ)&b_from=$(date -u -d '-1 hour' +%FT%TZ)&b_to=$(date -u +%FT%TZ)"
  s1=$(status GET "/api/v1/statements/compare?$q")
  s2=$(status GET "/api/v1/ash/compare?$q&group_by=wait_event")
  if [[ $s1 == 200 && $s2 == 200 ]]; then result PASS 4 "statements/compare $s1, ash/compare $s2"; else result FAIL 4 "statements/compare $s1, ash/compare $s2"; fi
}

check5() { # a range older than raw retention is read from hourly rollups
  local inst=$1 q r1 r2 dropped
  q="instance_id=$inst&from=$(date -u -d '-60 days' +%FT%TZ)&to=$(date -u -d '-40 days' +%FT%TZ)"
  r1=$(api GET "/api/v1/statements?$q" | jq -r '.resolution')
  r2=$(api GET "/api/v1/ash?$q" | jq -r '.resolution')
  dropped=$(status GET "/api/v1/ash?$q&group_by=usename")
  local d="statements resolution=$r1, ash resolution=$r2, ash by usename on the rollup range=$dropped"
  if [[ $r1 == 1h && $r2 == 1h && $dropped == 400 ]]; then result PASS 5 "$d"; else result FAIL 5 "$d"; fi
}

check6() { # deadlock log line, auto_explain sample, autovacuum run
  local inst=$1 dl ae av
  dl=$(api GET "/api/v1/logs?instance_id=$inst&class=lock.deadlock&from=$FROM&to=$TO" | jq '[.[] | select(.class == "lock.deadlock")] | length')
  ae=$(api GET "/api/v1/plans/samples?instance_id=$inst&queryid=$QUERYID&from=$FROM&to=$TO" | jq '.samples | length')
  av=$(api GET "/api/v1/autovacuum/runs?instance_id=$inst&from=$FROM&to=$TO" | jq '[.runs[] | select(.relname == "churn")] | length')
  local d="deadlock lines=$dl auto_explain samples=$ae churn autovacuum runs=$av"
  if ((dl > 0 && ae > 0 && av > 0)); then result PASS 6 "$d"; else result FAIL 6 "$d"; fi
}

wait_command() { # wait_command ID: final command JSON
  local id=$1 body st
  for _ in $(seq 60); do
    body=$(api GET "/api/v1/commands/$id")
    st=$(jq -r '.state' <<<"$body")
    [[ $st == done || $st == failed || $st == expired ]] && break
    sleep 2
  done
  echo "$body"
}

check7() { # hypothetical index validated, cost before and after
  local inst=$1 id body st before after
  id=$(api POST "/api/v1/instances/$inst/commands" \
    '{"kind":"index_check","args":{"datname":"parity","queryid":"'"$QUERYID"'","index_schema":"public","index_relation":"orders","index_columns":["customer"]}}' | jq -r '.command_id')
  body=$(wait_command "$id")
  st=$(jq -r '.state' <<<"$body")
  before=$(jq -r '[.. | objects | .cost_before? // empty][0] // empty' <<<"$body")
  after=$(jq -r '[.. | objects | .cost_after? // empty][0] // empty' <<<"$body")
  if [[ $st == done && -n $before && -n $after ]] && awk -v a="$after" -v b="$before" 'BEGIN{exit !(a < b)}'; then
    result PASS 7 "status=$st cost_before=$before cost_after=$after"
  else
    result FAIL 7 "status=$st cost_before=$before cost_after=$after $(jq -r '.error // empty' <<<"$body")"
  fi
}

check8() { # workbook variant with one planner setting; two plans differ
  local inst=$1 wb base var b1 b2 h1 h2
  wb=$(api POST /api/v1/workbooks '{"instance_id":"'"$inst"'","database":"parity","queryid":"'"$QUERYID"'","name":"parity"}')
  local wid
  wid=$(jq -r '.workbook_id' <<<"$wb")
  base=$(jq -r '.variants[0].variant_id // empty' <<<"$wb")
  [[ -n $base ]] || base=$(api GET "/api/v1/workbooks/$wid" | jq -r '.variants[0].variant_id')
  var=$(api POST "/api/v1/workbooks/$wid/variants" '{"name":"no index scan, serial","settings":{"enable_indexscan":"off","enable_bitmapscan":"off","max_parallel_workers_per_gather":"0"}}' | jq -r '.variant_id // .variants[-1].variant_id')
  b1=$(api POST "/api/v1/workbooks/$wid/variants/$base/run" | jq -r '.command_id')
  b2=$(api POST "/api/v1/workbooks/$wid/variants/$var/run" | jq -r '.command_id')
  wait_command "$b1" >/dev/null
  wait_command "$b2" >/dev/null
  sleep 3
  local wbody
  wbody=$(api GET "/api/v1/workbooks/$wid")
  h1=$(jq -r --arg v "$base" '[.variants[] | select(.variant_id == $v) | .plan_hash // empty][0] // empty' <<<"$wbody")
  h2=$(jq -r --arg v "$var" '[.variants[] | select(.variant_id == $v) | .plan_hash // empty][0] // empty' <<<"$wbody")
  api DELETE "/api/v1/workbooks/$wid" >/dev/null || true
  if [[ -n $h1 && -n $h2 && $h1 != "$h2" ]]; then result PASS 8 "baseline=$h1 variant=$h2"; else result FAIL 8 "baseline=$h1 variant=$h2"; fi
}

check9() { # MCP tools/list and tools/call against REST
  local mcp rest list n
  mcp_call() {
    curl -fsS -X POST -H @"$P/auth.hdr" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2025-11-25' \
      --data "$1" "$URL/mcp"
  }
  list=$(mcp_call '{"jsonrpc":"2.0","id":1,"method":"tools/list"}')
  n=$(jq '.result.tools | length' <<<"$list")
  mcp=$(mcp_call '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"pglens_list_instances","arguments":{}}}' |
    jq -r '.result.content[0].text' | jq -c '[.[].instance_id] | sort')
  rest=$(api GET /api/v1/instances | jq -c '[.[].instance_id] | sort')
  if ((n == 30)) && [[ $mcp == "$rest" && $mcp != "[]" ]]; then result PASS 9 "tools=$n ids=$mcp"; else result FAIL 9 "tools=$n mcp=$mcp rest=$rest"; fi
}

cmd_check() {
  local inst
  inst=$(instance)
  FROM=$(date -u -d '-30 minutes' +%FT%TZ)
  TO=$(date -u -d '+1 minute' +%FT%TZ)
  QUERYID=$(api GET "/api/v1/statements?instance_id=$inst&database=parity&from=$FROM&to=$TO&limit=50" |
    jq -r '[.. | objects | select((.query_text? // .query? // "") | test("customer = "))][0].queryid // empty')
  echo "instance=$inst queryid=${QUERYID:-none}"
  check1 "$inst" || true
  check2 || true
  check3 "$inst" || true
  check4 "$inst" || true
  check5 "$inst" || true
  check6 "$inst" || true
  check7 "$inst" || true
  check8 "$inst" || true
  check9 || true
}

cmd_down() {
  local pid
  for f in agent server endpoints; do
    if [[ -f $P/$f.pid ]]; then
      pid=$(cat "$P/$f.pid")
      kill "$pid" 2>/dev/null || true
      rm -f "$P/$f.pid"
    fi
  done
  "${COMPOSE[@]}" down -v --remove-orphans
}

case ${1:-} in
up) cmd_up ;;
workload) cmd_workload "${2:-5}" ;;
check) cmd_check ;;
down) cmd_down ;;
*)
  echo "usage: $0 up|workload [minutes]|check|down" >&2
  exit 2
  ;;
esac
