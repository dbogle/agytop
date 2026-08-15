#!/usr/bin/env bash
set -euo pipefail

# check-coverage.sh -- per-package coverage gate for agytop.
#
# A single repo-wide coverage threshold is the wrong instrument here:
# internal/ui carries a lot of Lipgloss theme constants and a keybinding
# table that exist to be read, not executed, and cmd/agytop's real coverage
# is structurally invisible to this tool (see below). Per-package floors,
# set just under the values measured when Phase 3 landed, catch regressions
# without inventing an unreachable target for any one package.
#
# See TESTING_ASSESSMENT.md, section 5 "Phase 3: Long-Term E2E & Hardening",
# for the rationale this script implements.

# --- Floors -----------------------------------------------------------
# Package -> minimum acceptable "go test -cover" percentage. Bump these up
# (never down, except in the same PR that explains why coverage regressed)
# as real coverage grows.
declare -A FLOORS=(
  [agytop/internal/config]=85
  [agytop/internal/supervisor]=70
  [agytop/internal/ui]=65
)

# --- Exemptions ---------------------------------------------------------
# cmd/agytop is deliberately exempt, not merely low. Its tests (main_test.go)
# build a real binary with `go build` and drive it with `exec.Command`, so
# that --version/--list/--dry-run's exit codes and stdout can be asserted
# against a hermetic HOME/CWD. The Go coverage instrument only records
# statements executed *inside the test binary's own process* -- it cannot
# see into a separate subprocess -- so cmd/agytop reads 0.0% no matter how
# thoroughly its CLI surface is tested. A floor here would be unsatisfiable
# by construction, so it is intentionally excluded from FLOORS and listed
# here instead, and this script fails loudly (see below) rather than
# silently if it ever goes missing from `go test`'s package list.
EXEMPT=(agytop/cmd/agytop)

echo "==> go test -cover ./..."
set +e
output=$(go test -cover ./... 2>&1)
test_status=$?
set -e
echo "$output"
echo

if [[ $test_status -ne 0 ]]; then
  echo "go test failed -- fix failing tests before the coverage gate can run."
  exit 1
fi

# --- Parse ---------------------------------------------------------------
# Lines look like (duration or "(cached)" in the middle varies):
#   ok  	agytop/internal/config	1.010s	coverage: 90.6% of statements
declare -A ACTUAL
while IFS= read -r line; do
  if [[ "$line" =~ ^ok[[:space:]]+([^[:space:]]+)[[:space:]].*coverage:\ ([0-9]+\.[0-9]+)%\ of\ statements ]]; then
    ACTUAL["${BASH_REMATCH[1]}"]="${BASH_REMATCH[2]}"
  fi
done <<< "$output"

# --- Fail loudly if an expected package is missing -----------------------
# A gate that passes when it cannot measure a package is worse than no gate:
# it looks green while checking nothing. This catches a renamed/removed
# package, a package with build tags that skip it on this runner, or a
# `go test` output format change -- any of which would otherwise make every
# floor below silently vacuous.
missing=()
for pkg in "${!FLOORS[@]}" "${EXEMPT[@]}"; do
  if [[ -z "${ACTUAL[$pkg]+x}" ]]; then
    missing+=("$pkg")
  fi
done
if [[ ${#missing[@]} -gt 0 ]]; then
  echo "ERROR: expected package(s) missing from 'go test -cover ./...' output:"
  for pkg in "${missing[@]}"; do
    echo "  - $pkg"
  done
  echo
  echo "This gate expects every package above to appear with a numeric"
  echo "coverage line (or, for cmd/agytop, at least to appear at all)."
  echo "Refusing to pass a gate that cannot measure what it's supposed to."
  exit 1
fi

# --- Report + evaluate -----------------------------------------------------
printf "\n%-32s %10s %10s %10s\n" "PACKAGE" "COVERAGE" "FLOOR" "STATUS"
printf '%s\n' "------------------------------------------------------------------------"

failures=()
for pkg in $(printf '%s\n' "${!ACTUAL[@]}" | sort); do
  pct="${ACTUAL[$pkg]}"

  is_exempt=false
  for e in "${EXEMPT[@]}"; do
    [[ "$pkg" == "$e" ]] && is_exempt=true
  done
  if $is_exempt; then
    printf "%-32s %9s%% %10s %10s\n" "$pkg" "$pct" "n/a" "EXEMPT"
    continue
  fi

  floor="${FLOORS[$pkg]:-}"
  if [[ -z "$floor" ]]; then
    printf "%-32s %9s%% %10s %10s\n" "$pkg" "$pct" "none" "UNGATED"
    continue
  fi

  if awk -v a="$pct" -v b="$floor" 'BEGIN { exit !(a >= b) }'; then
    printf "%-32s %9s%% %10s %10s\n" "$pkg" "$pct" "${floor}%" "PASS"
  else
    printf "%-32s %9s%% %10s %10s\n" "$pkg" "$pct" "${floor}%" "FAIL"
    failures+=("$pkg: ${pct}% is below its ${floor}% floor")
  fi
done
echo

if [[ ${#failures[@]} -gt 0 ]]; then
  echo "Coverage gate FAILED. Package(s) below their floor:"
  for f in "${failures[@]}"; do
    echo "  - $f"
  done
  exit 1
fi

echo "Coverage gate passed."
