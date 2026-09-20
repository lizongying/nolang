#!/bin/bash
# run-golden.sh — Run all .no tests and compare with baseline
#
# Usage:
#   NOLANG_MIR=3 ./run-golden.sh           # run with MIR backend
#   NOLANG_MIR=0 ./run-golden.sh           # run with legacy backend
#   ./run-golden.sh -u                     # update baseline
#
# Requires bash 4+ (it uses `declare -A`), so macOS's stock /bin/bash 3.2 cannot
# run it — use Homebrew bash, or drive the same comparison from Python.
#
# The baseline is SPACE-separated (`rc sha256 path`). Reading it with
# IFS=$'\t' matched nothing and reported every test as NEW, so the read below
# deliberately uses the default IFS.

set -e

NOLANG_CMD="${NOLANG_CMD:-../bin/no}"
BASELINE="mir-baseline.tsv"
if [ "${NOLANG_MIR:-0}" = "0" ]; then
    BASELINE="legacy-baseline.tsv"
fi

cd "$(dirname "$0")/.."

PASS=0
FAIL=0
SKIP=0
NEW=0

# Read baseline into associative array
declare -A BASE_RC
declare -A BASE_HASH
while read -r rc hash file; do
    BASE_RC["$file"]="$rc"
    BASE_HASH["$file"]="$hash"
done < "tests/golden/$BASELINE"

for testfile in tests/*.no; do
    [ -f "$testfile" ] || continue
    basename=$(basename "$testfile")
    relpath="tests/$basename"
    
    # Skip files in subdirs for now
    [[ "$testfile" == */*/* ]] && continue
    
    # Run the test
    output=$(NOLANG_MIR=${NOLANG_MIR:-3} "$NOLANG_CMD" run "$testfile" 2>&1)
    rc=$?
    
    # Compute hash of output
    hash=$(echo "$output" | sha256sum | awk '{print $1}')
    
    base_rc="${BASE_RC[$relpath]:-}"
    base_hash="${BASE_HASH[$relpath]:-}"
    
    if [ -z "$base_rc" ]; then
        NEW=$((NEW + 1))
        echo "NEW  $relpath (rc=$rc)"
    elif [ "$rc" = "$base_rc" ] && [ "$hash" = "$base_hash" ]; then
        PASS=$((PASS + 1))
    else
        FAIL=$((FAIL + 1))
        echo "FAIL $relpath (rc=$rc, expected rc=$base_rc)"
    fi
done

echo ""
echo "=== SUMMARY ==="
echo "Pass: $PASS"
echo "Fail: $FAIL"
echo "New:  $NEW"
echo "Total: $((PASS + FAIL + NEW))"
