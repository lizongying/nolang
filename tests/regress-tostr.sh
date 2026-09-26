#!/usr/bin/env bash
# regress-tostr.sh — deterministic regression for the auto to-str feature.
#
# Verifies that print / eprint / format fields auto-execute to-str for every
# aggregate type (struct, slice/array, built-in map), with NO hand-written
# .to-str required:
#   - struct  -> {field: value, ...}  (nested aggregates recursive)
#   - slice   -> [e0, e1, ...]
#   - map     -> {key: value, ...}    (built-in [k]v layout)
#
# The feature lives in the MIR backend (src/mir/hir2mir.go: printableValue /
# valueToStr / structFieldsToStr / sliceToStr / mapFieldsToStr). The legacy
# backend was removed, so only the MIR backend is exercised here.
#
# Map output order is hashmap-internal but stable per build, so each file is run
# N times and the output must ALWAYS equal the pinned expectation (catching both
# incorrect rendering AND any nondeterminism).
#
# Usage: ./tests/regress-tostr.sh [N]
#   N = iterations per file (default 50).

set -u
cd "$(dirname "$0")/.."

# Ensure the LLVM toolchain is on PATH — `no run` compiles & executes each test
# via clang/opt/llc and refuses to run ("未检测到 LLVM 工具鏈") without it.
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

# check <test-file> <expected-multiline-output>
check() {
    local tf="$1"; local expect="$2"
    local mism=0
    for i in $(seq 1 "$N"); do
        out=$("$NO" run "$tf" 2>/dev/null)
        rc=$?
        if [ "$rc" -ne 0 ] || [ "$out" != "$expect" ]; then
            mism=$((mism + 1))
            if [ "$mism" -le 3 ]; then
                echo "  MISMATCH $tf run=$i (rc=$rc):"
                echo "    got: [$out]"
                echo "    exp: [$expect]"
            fi
        fi
    done
    if [ "$mism" -ne 0 ]; then
        echo "FAIL: $tf — $mism/$N runs deviated from expected"
        fail=1
    else
        echo "OK: $tf — $N/$N runs correct & deterministic"
    fi
}

expect_struct=$(cat <<'EOF'
{x: 1, y: 2}
{name: team, pts: [{x: 1, y: 2}, {x: 3, y: 4}]}
p={x: 1, y: 2}
g={name: team, pts: [{x: 1, y: 2}, {x: 3, y: 4}]}
EOF
)
check "tests/struct-to-str.no" "$expect_struct"

expect_slice=$(cat <<'EOF'
[a, b, c]
[]
[1, 2, 3]
s=[a, b, c]
n=[1, 2, 3]
EOF
)
check "tests/slice-to-str.no" "$expect_slice"

expect_map=$(cat <<'EOF'
{c: 3, b: 2, a: 1}
{}
{y: 0, x: 1}
m={c: 3, b: 2, a: 1}
EOF
)
check "tests/map-to-str.no" "$expect_map"

if [ "$fail" -ne 0 ]; then
    echo "RESULT: regression detected"
    exit 1
fi
echo "RESULT: $N x 3 files deterministic & correct"
exit 0
