#!/usr/bin/env bash
# regress-print-eprint-tostr.sh — deterministic regression for the auto to-str
# feature on the print-family builtins.
#
# print  -> stdout (+ trailing newline)
# eprint -> stderr (+ trailing newline)
# (println / eprintln do NOT exist in the MIR backend — they were legacy-only —
# so they are intentionally not covered here.)
#
# Both must auto-execute to-str for struct / slice / map aggregates with no
# hand-written .to-str. The feature lives in the MIR backend
# (src/mir/hir2mir.go: printableValue / valueToStr / ...). The legacy backend
# was removed, so only the MIR backend is exercised.
#
# Map output order is hashmap-internal but stable per build, so each file is run
# N times and the output must ALWAYS equal the pinned expectation.
#
# Usage: ./tests/regress-print-eprint-tostr.sh [N]
#   N = iterations (default 50).

set -u
cd "$(dirname "$0")/.."

# Ensure the LLVM toolchain is on PATH — `no run` needs it to compile & execute.
for d in /opt/homebrew/opt/llvm/bin /usr/local/opt/llvm/bin; do
    if [ -d "$d" ]; then
        case ":$PATH:" in
            *":$d:"*) ;;
            *) PATH="$d:$PATH" ;;
        esac
    fi
done
export PATH

NO=./bin/no
N="${1:-50}"

if [ ! -x "$NO" ]; then
    echo "error: $NO not built (run 'make' / rebuild bin/no first)" >&2
    exit 2
fi

fail=0

# check <test-file> <expected-stdout> <expected-stderr>
check() {
    local tf="$1"; local exp_out="$2"; local exp_err="$3"
    local mism=0
    for i in $(seq 1 "$N"); do
        out=$("$NO" run "$tf" 2>/tmp/pe.err)
        rc=$?
        err=$(cat /tmp/pe.err)
        if [ "$rc" -ne 0 ] || [ "$out" != "$exp_out" ] || [ "$err" != "$exp_err" ]; then
            mism=$((mism + 1))
            if [ "$mism" -le 3 ]; then
                echo "  MISMATCH $tf run=$i (rc=$rc):"
                echo "    stdout got: [$out]"
                echo "    stdout exp: [$exp_out]"
                echo "    stderr got: [$err]"
                echo "    stderr exp: [$exp_err]"
            fi
        fi
    done
    if [ "$mism" -ne 0 ]; then
        echo "FAIL: $tf — $mism/$N runs deviated from expected"
        fail=1
    else
        echo "OK: $tf — $N/$N runs correct & deterministic (stdout + stderr)"
    fi
}

exp_out=$(cat <<'EOF'
{x: 1, y: 2}
[a, b]
{b: 2, a: 1}
EOF
)
exp_err=$(cat <<'EOF'
{x: 1, y: 2}
[a, b]
{b: 2, a: 1}
EOF
)
check "tests/print-eprint-to-str.no" "$exp_out" "$exp_err"

if [ "$fail" -ne 0 ]; then
    echo "RESULT: regression detected"
    exit 1
fi
echo "RESULT: $N runs deterministic & correct"
exit 0
