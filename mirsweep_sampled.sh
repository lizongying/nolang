#!/usr/bin/env bash
# Sampled MIR dot-gap sweep: every 8th tests/*.no, MIR=1 (dump mode).
set -u
cd "$(dirname "$0")"
TMP="${TMPDIR:-/tmp}"
rm -f "$TMP"/nolang-mir-*.txt
NO=./no
i=0
files=0
for f in tests/*.no; do
  i=$((i+1))
  if [ $((i % 8)) -ne 0 ]; then continue; fi
  files=$((files+1))
  NOLANG_MIR=1 "$NO" build "$f" >/dev/null 2>&1
done
echo "scanned_files=$files"
echo "=== per-source dot gap (top 30) ==="
grep -h "lower-gap" "$TMP"/nolang-mir-*.txt 2>/dev/null | grep "/dot:" | sed -E 's#.*/([^/]+\.no):#\1#' | sort | uniq -c | sort -rn | head -30
echo "=== TOTAL dot gaps (sampled) ==="
grep -h "lower-gap" "$TMP"/nolang-mir-*.txt 2>/dev/null | grep -c "/dot:"
echo "=== other gap kinds (top 15) ==="
grep -h "lower-gap" "$TMP"/nolang-mir-*.txt 2>/dev/null | sed -E 's#.*/([a-z0-9-]+\.no):#\1 #; s#/.*##' | sort | uniq -c | sort -rn | head -15
