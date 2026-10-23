#!/usr/bin/env bash
# scripts/e2e_evidence.sh — run a command and retain its exit status and cleanup evidence.
set -euo pipefail

ARTIFACT_DIR="${E2E_ARTIFACTS_DIR:-test/e2e/_artifacts}"
mkdir -p "$ARTIFACT_DIR"

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
log_file="$ARTIFACT_DIR/e2e-full-${timestamp}.log"
command_line="$(printf '%q ' "$@")"

# Residue is what the run leaves behind: containers and networks that exist
# afterwards and did not exist before it. Snapshotting first keeps the stacks
# already running on the host, developer stacks included, out of the count,
# and the name filter keeps out what the run did not name. A snapshot that
# fails must not stop the run.
if command -v docker >/dev/null 2>&1; then
	containers_before="$(docker ps -aq --no-trunc 2>/dev/null | sort || true)"
	networks_before="$(docker network ls -q --no-trunc 2>/dev/null | sort || true)"
fi

set +e
"$@" >"$log_file" 2>&1
exit_status=$?
set -e

if command -v docker >/dev/null 2>&1; then
	residual_containers="$(comm -13 <(printf '%s\n' "$containers_before") <(docker ps -aq --no-trunc --filter 'name=pglens' | sort) | wc -l | tr -d ' ')"
	residual_networks="$(comm -13 <(printf '%s\n' "$networks_before") <(docker network ls -q --no-trunc --filter 'name=pglens' | sort) | wc -l | tr -d ' ')"
else
	residual_containers="unavailable"
	residual_networks="unavailable"
fi

finished_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
{
	printf 'RESIDUAL_CONTAINERS=%s\n' "$residual_containers"
	printf 'RESIDUAL_NETWORKS=%s\n' "$residual_networks"
	printf 'EXIT_STATUS=%s FINISHED_AT=%s COMMAND=%s\n' "$exit_status" "$finished_at" "$command_line"
} >>"$log_file"

exit "$exit_status"
