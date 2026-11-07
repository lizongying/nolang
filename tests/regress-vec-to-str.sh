#!/usr/bin/env bash
# regress-vec-to-str.sh — deterministic regression for the flaky []i64.to-str bug.
#
# The original bug was nondeterministic: it only appeared when Go's generic-method
# map iteration order happened to let []byte.to-str win over []t.to-str for a
# []i64 receiver. A single run could therefore pass or fail depending on process
# ordering, so a one-shot check was insufficient. This runner executes the test
# program N times PER backend (each `./no run` is a fresh process with a fresh,
# potentially-different map order), asserting the output is ALWAYS the exact
# correct string.
#
# Backend:
#   3 — MIR backend (the ONLY backend; the legacy HIR backend referenced by the
#       original NOLANG_MIR=0 leg was removed, so that leg now errors and is gone).
# The fix lives in the shared transpiler (resolveMethodCall), so the output must
# be deterministic.
#
# Usage: ./tests/regress-vec-to-str.sh [N]
#   N = iterations per backend (default 50).

set -u
cd "$(dirname "$0")/.."

NO=./no
# vec-to-str-flaky.no was merged into tests/vec-more.no (heavy test dedup);
# probe the []i64.to-str line as a substring of the merged program's output.
TEST=tests/vec-more.no
EXPECT='[1, 2, 3, 10, 20, 256]'
N="${1:-50}"

if [ ! -x "$NO" ]; then
    echo "error: $NO not built (run 'make' first)" >&2
    exit 2
fi

fail=0
for mir in 3; do
    mism=0
    for i in $(seq 1 "$N"); do
        out=$(NOLANG_MIR=$mir "$NO" run "$TEST" 2>/dev/null)
        case "$out" in
            *"$EXPECT"*) ;;
            *)
                mism=$((mism + 1))
                if [ "$mism" -le 3 ]; then
                    echo "  MISMATCH MIR=$mir run=$i: got [$out]"
                fi
                ;;
        esac
    done
    if [ "$mism" -ne 0 ]; then
        echo "FAIL: MIR=$mir — $mism/$N runs deviated from expected [$EXPECT]"
        fail=1
    else
        echo "OK: MIR=$mir — $N/$N runs deterministic & correct"
    fi
done

if [ "$fail" -ne 0 ]; then
    echo "RESULT: ❌ regression detected"
    exit 1
fi
echo "RESULT: ✅ $N runs deterministic & correct (MIR backend)"
exit 0
