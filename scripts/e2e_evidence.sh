#!/usr/bin/env bash
# scripts/e2e_evidence.sh — run a command and retain its exit status and cleanup evidence.
set -euo pipefail

ARTIFACT_DIR="${E2E_ARTIFACTS_DIR:-test/e2e/_artifacts}"
mkdir -p "$ARTIFACT_DIR"

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
log_file="$ARTIFACT_DIR/e2e-full-${timestamp}.log"
command_line="$(printf '%q ' "$@")"

set +e
"$@" >"$log_file" 2>&1
exit_status=$?
set -e

if command -v docker >/dev/null 2>&1; then
	residual_containers="$(docker ps -a --filter 'name=pglens' --format '{{.Names}}' | wc -l | tr -d ' ')"
	residual_networks="$(docker network ls --filter 'name=pglens' --format '{{.Name}}' | wc -l | tr -d ' ')"
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
