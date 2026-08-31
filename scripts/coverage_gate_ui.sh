#!/usr/bin/env bash
# scripts/coverage_gate_ui.sh — fail when the included UI source falls below its floor.
# Floors are raised, never lowered, by later phases. If a phase cannot meet a
# floor, record a finding in STATE.md §9 instead of editing this script.
set -euo pipefail

SUMMARY="${1:-web/coverage/coverage-summary.json}"

if [[ ! -f "$SUMMARY" ]]; then
  echo "UI coverage summary not found: $SUMMARY"
  exit 1
fi

node - "$SUMMARY" <<'NODE'
const fs = require('node:fs')

const summaryPath = process.argv[2]
const summary = JSON.parse(fs.readFileSync(summaryPath, 'utf8'))
const directoryFloors = {
  'src/lib/': 95,
  'src/api/': 95,
  'src/components/state/': 100,
  'src/components/charts/': 90,
  'src/features/': 80,
}

const files = Object.entries(summary).filter(([name]) => name !== 'total')
const metrics = ['lines', 'branches']
const totals = Object.fromEntries(metrics.map((metric) => [metric, { covered: 0, total: 0 }]))
const directories = Object.fromEntries(
  Object.keys(directoryFloors).map((directory) => [
    directory,
    Object.fromEntries(metrics.map((metric) => [metric, { covered: 0, total: 0 }])),
  ]),
)

function normalizedSourcePath(path) {
  const normalized = path.replaceAll('\\', '/')
  const sourceMarker = normalized.lastIndexOf('/src/')
  return sourceMarker >= 0 ? normalized.slice(sourceMarker + 1) : normalized
}

function addMetric(target, metric, value) {
  const total = Number(value?.total ?? 0)
  const covered = Number(value?.covered ?? 0)
  target[metric].total += total
  target[metric].covered += covered
}

for (const [file, entry] of files) {
  const sourcePath = normalizedSourcePath(file)
  for (const metric of metrics) {
    addMetric(totals, metric, entry[metric])
    for (const directory of Object.keys(directoryFloors)) {
      if (sourcePath.startsWith(directory)) {
        addMetric(directories[directory], metric, entry[metric])
      }
    }
  }
}

function percentage(metric) {
  if (metric.total === 0) return 100
  return (metric.covered * 100) / metric.total
}

let failed = false
function report(label, metric, floor, skipEmpty = false) {
  if (skipEmpty && metric.total === 0) {
    console.log(`${label} n/a (no files; floor ${floor}%) SKIP`)
    return
  }
  const pct = percentage(metric)
  const measured = pct.toFixed(1)
  const result = pct < floor ? 'FAIL' : 'OK'
  console.log(`${label} ${measured}% (floor ${floor}%) ${result}`)
  failed ||= result === 'FAIL'
}

report('global lines', totals.lines, 85)
report('global branches', totals.branches, 80)
for (const [directory, floor] of Object.entries(directoryFloors)) {
  report(directory, directories[directory].lines, floor, true)
}

if (totals.lines.total === 0) {
  failed = true
}

console.log(`UI_COVERAGE_GATE=${failed ? 'fail' : 'pass'}`)
process.exitCode = failed ? 1 : 0
NODE
