#!/usr/bin/env bash
# bench.sh — time `md check` over a corpus, print the minimum milliseconds.
#
# Usage: bench.sh <md-binary> <corpus-dir> [runs]
#
# Timing comes from md check's own summary line ("N rules, ... , Xms") — the
# engine's measured evaluation time, immune to shell/process startup noise.
# Min of N runs (default 3): minimum is the stable estimator under CI jitter;
# mean/max absorb scheduler noise.
set -euo pipefail

bin=$1
corpus=$2
runs=${3:-3}

min=""
for _ in $(seq "$runs"); do
  # Findings are expected (the corpus deliberately violates rules); only a
  # missing summary line is a failure.
  # NOTE lines can follow the summary on stdout, so scan the whole output
  # for the last "<N>ms" token rather than assuming it's the final line.
  output=$(cd "$corpus" && "$bin" check 2>/dev/null < /dev/null) || true
  ms=$(grep -Eo '[0-9]+ms' <<<"$output" | tail -1 | tr -d 'ms')
  if [[ -z "$ms" ]]; then
    echo "bench: no timing in md check output" >&2
    exit 1
  fi
  if [[ -z "$min" || "$ms" -lt "$min" ]]; then min=$ms; fi
done

echo "$min"
