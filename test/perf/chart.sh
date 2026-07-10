#!/usr/bin/env bash
# chart.sh — mermaid trend chart of `md check` medium-corpus time across
# recent main runs, built from per-run perf-metrics artifacts.
#
# Usage: chart.sh <current-run-number> <current-medium-ms> [n-prev-runs]
# Needs: GH_TOKEN, GITHUB_REPOSITORY. Prints a mermaid block to stdout
# (pipe into $GITHUB_STEP_SUMMARY).
#
# Absolute times across shared runners bounce 2-3x — this chart is a trend
# indicator, not a gate. The gates (ratio, slope) stay runner-invariant.
set -euo pipefail

run_now=$1
ms_now=$2
n=${3:-19}

labels=()
values=()

# Last n successful main-push runs of this workflow, oldest first.
mapfile -t runs < <(gh api \
  "repos/$GITHUB_REPOSITORY/actions/workflows/perf.yml/runs?branch=main&event=push&status=success&per_page=$n" \
  --jq '.workflow_runs | sort_by(.run_number) | .[] | "\(.id) \(.run_number)"')

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

for r in "${runs[@]}"; do
  id=${r%% *}
  num=${r##* }
  # Runs predating the metrics artifact (or past retention) have nothing
  # to download — skip them.
  gh run download "$id" -R "$GITHUB_REPOSITORY" -n perf-metrics -D "$tmp/$id" 2>/dev/null || continue
  ms=$(jq -r '.medium_ms' "$tmp/$id/perf.json")
  labels+=("\"r$num\"")
  values+=("$ms")
done

labels+=("\"r$run_now\"")
values+=("$ms_now")

join() { local IFS=', '; echo "$*"; }

echo '```mermaid'
echo 'xychart-beta'
echo '    title "md check — medium corpus (10k docs), ms per main run"'
echo "    x-axis [$(join "${labels[@]}")]"
echo '    y-axis "ms"'
echo "    line [$(join "${values[@]}")]"
echo '```'
