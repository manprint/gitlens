#!/usr/bin/env bash
# scripts/coverage_gate.sh — fail when a package falls below its floor.
set -euo pipefail

PROFILE="${1:-coverage.out}"
MOD="github.com/manprint/pglens"
GLOBAL_MIN=75

# Packages whose correctness the whole product depends on.
declare -A FLOORS=(
  ["$MOD/internal/delta"]=90
  ["$MOD/internal/cardinality"]=90
  ["$MOD/internal/topology"]=90
  ["$MOD/internal/ash"]=90
  ["$MOD/internal/identity"]=85
)

if [ ! -f "$PROFILE" ]; then
  echo "coverage profile not found: $PROFILE"
  exit 1
fi

# Parse coverage profile: aggregate per package
# Format: mode: atomic
# github.com/manprint/pglens/internal/delta/engine.go:60.18,62.2 1 1
# We compute per package: covered statements / total statements *100

declare -A pkg_total
declare -A pkg_covered

while IFS= read -r line; do
  # Skip mode line
  if [[ "$line" == mode:* ]]; then
    continue
  fi
  # line: file:range statements count
  # Extract file path and count
  file=$(echo "$line" | cut -d: -f1)
  # pkg is directory
  pkg=$(dirname "$file")
  # Need to map file pkg to import path: file starts with github.com/manprint/pglens/...
  # Our pkg_total keys will be full import paths
  # For files not under MOD, skip
  if [[ "$file" != "$MOD"* ]]; then
    continue
  fi
  # Extract statements count and covered count
  # Use awk to parse: third field is number of statements, fourth is count
  stmts=$(echo "$line" | awk '{print $2}')
  cnt=$(echo "$line" | awk '{print $3}')
  # pkg should be normalized: remove file part, keep dir import path
  # file is like github.com/manprint/pglens/internal/delta/engine.go -> pkg is github.com/manprint/pglens/internal/delta
  pkg=${file%/*}
  pkg_total["$pkg"]=$((${pkg_total["$pkg"]:-0} + stmts))
  if [ "$cnt" != "0" ]; then
    pkg_covered["$pkg"]=$((${pkg_covered["$pkg"]:-0} + stmts))
  else
    # ensure key exists for total
    pkg_covered["$pkg"]=${pkg_covered["$pkg"]:-0}
  fi
done < "$PROFILE"

# Filter to internal/... only for global, and compute display
failed=0
# For global, sum over internal only
global_total=0
global_covered=0

# First collect all packages to report
declare -A all_pkgs
for k in "${!pkg_total[@]}"; do
  all_pkgs["$k"]=1
done
for k in "${!FLOORS[@]}"; do
  all_pkgs["$k"]=1
done

# Sort keys for deterministic output
sorted_keys=$(printf "%s\n" "${!all_pkgs[@]}" | sort)

# Prepare failing list to print last
declare -a failing_lines
declare -a passing_lines

for pkg in $sorted_keys; do
  # Only consider internal/... for reporting; but FLOORS may include packages not yet present
  total=${pkg_total["$pkg"]:-0}
  covered=${pkg_covered["$pkg"]:-0}
  # Determine if package exists/has statements
  if [ "$total" -eq 0 ]; then
    # Check if floor exists -> skip, not failed (package not yet implemented)
    if [[ -v FLOORS["$pkg"] ]]; then
      # Skip packages that don't exist yet or have no statements
      continue
    else
      # If not in FLOORS and no statements, still skip (e.g., internal with no code)
      continue
    fi
  fi
  # Compute percentage
  pct=$(awk "BEGIN {printf \"%.1f\", $covered*100/$total}")
  pct_int=$(awk "BEGIN {printf \"%d\", $covered*100/$total}")
  floor=${FLOORS["$pkg"]:-0}
  # For display, floor may be 0 if not in list -> show global floor? But spec says global floor computed over ./internal/... only;
  # For per-package display, show floor if exists else "-"
  floor_display="$floor"
  if [ "$floor" -eq 0 ]; then
    floor_display="-"
  fi
  line=$(printf "%-50s %5s%% (floor %s%%) statements %d/%d" "$pkg" "$pct" "$floor_display" "$covered" "$total")
  # Determine failure: if floor >0 and pct < floor => fail
  if [ "$floor" -ne 0 ] && [ "$pct_int" -lt "$floor" ]; then
    failing_lines+=("$line  FAIL")
    failed=1
  else
    passing_lines+=("$line  OK")
  fi

  # Accumulate global if pkg is under internal
  if [[ "$pkg" == "$MOD/internal/"* ]]; then
    global_total=$((global_total + total))
    global_covered=$((global_covered + covered))
  fi
done

# Print passing first, failing last so actionable
for l in "${passing_lines[@]}"; do
  echo "$l"
done
for l in "${failing_lines[@]}"; do
  echo "$l"
done

# Global floor check (over ./internal/... only)
if [ "$global_total" -gt 0 ]; then
  global_pct=$(awk "BEGIN {printf \"%.1f\", $global_covered*100/$global_total}")
  global_pct_int=$(awk "BEGIN {printf \"%d\", $global_covered*100/$global_total}")
  echo ""
  printf "global (./internal/...) %5s%% (floor %d%%) statements %d/%d" "$global_pct" "$GLOBAL_MIN" "$global_covered" "$global_total"
  if [ "$global_pct_int" -lt "$GLOBAL_MIN" ]; then
    echo "  FAIL"
    failed=1
  else
    echo "  OK"
  fi
else
  echo "no internal packages found for global coverage"
fi

exit $failed
