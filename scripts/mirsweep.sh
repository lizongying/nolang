#!/usr/bin/env bash
# MIR coverage sweep: for each tests/*.no, run NOLANG_MIR=1 (dump mode) and
# aggregate the remaining [lower-gap] .../dot counts. Build-only (no run) to
# stay fast; fallback to legacy after dumping so failures don't abort the sweep.
set -u
cd "$(dirname "$0")"
TMP="${TMPDIR:-/tmp}"
rm -f "$TMP"/nolang-mir-*.txt
NO=./no
total=0
dot_total=0
files=0
for f in tests/*.no; do
  files=$((files+1))
  NOLANG_MIR=1 "$NO" build "$f" >/dev/null 2>&1
done
echo "scanned_files=$files"
echo "--- per-file dot gap counts (top 40) ---"
grep -h "lower-gap" "$TMP"/nolang-mir-*.txt 2>/dev/null | sed -E 's#.*/([^/]+\.no):#\1 #' | sort | uniq -c | sort -rn | head -40
echo "--- total dot gaps across corpus ---"
dot_total=$(grep -h "lower-gap" "$TMP"/nolang-mir-*.txt 2>/dev/null | grep -c "/dot:")
echo "dot_gap_total=$dot_total"
echo "--- other gap kinds (top 20) ---"
grep -h "lower-gap" "$TMP"/nolang-mir-*.txt 2>/dev/null | sed -E 's#.*/(.*):.*#\1#' | sort | uniq -c | sort -rn | head -20
