#!/usr/bin/env bash
# scripts/id_audit.sh — report INT-* and SYS-* identifiers missing on either side.
set -euo pipefail

if [ "$#" -ne 1 ]; then
	printf 'usage: %s STATE_FILE\n' "$0" >&2
	exit 2
fi

state_file=$1
source_root=${ID_AUDIT_ROOT:-.}

if [ ! -f "$state_file" ]; then
	printf 'state file not found: %s\n' "$state_file" >&2
	exit 2
fi

pattern='\b(INT|SYS)-[A-Z0-9]+(-[A-Z0-9]+)+\b'
source_ids=$(
	grep -rhoE --include='*.go' "$pattern" \
		"$source_root/internal" "$source_root/test" "$source_root/cmd" 2>/dev/null \
		| grep -oE "$pattern" | sort -u || true
)
state_ids=$(grep -hoE "$pattern" "$state_file" | sort -u || true)

source_list=$(printf '%s\n' "$source_ids" | sed '/^$/d')
state_list=$(printf '%s\n' "$state_ids" | sed '/^$/d')

printf 'ONLY_IN_SOURCE\n'
comm -23 <(printf '%s\n' "$source_list") <(printf '%s\n' "$state_list")

printf 'ONLY_IN_STATE\n'
comm -13 <(printf '%s\n' "$source_list") <(printf '%s\n' "$state_list")

source_only=$(comm -23 <(printf '%s\n' "$source_list") <(printf '%s\n' "$state_list") | sed '/^$/d' | wc -l | tr -d ' ')
state_only=$(comm -13 <(printf '%s\n' "$source_list") <(printf '%s\n' "$state_list") | sed '/^$/d' | wc -l | tr -d ' ')
printf 'DIFF_COUNT=%s\n' "$((source_only + state_only))"
