#!/usr/bin/env bash
# MIR codegen coverage sweep (NOLANG_MIR=2).
# For each tests/*.no: build with MIR=2 + DEBUG. A REAL fallback prints one of
# the emitMIR markers below to stderr (only when fallback actually happens).
# We grep for those specific substrings; the hir2mir.go "[MIR] N top-level funcs"
# debug line is NOT a fallback marker and is ignored.
set -u
cd "$(dirname "$0")"
NO=./bin/no
STRIDE=${1:-8}
STARTIDX=${2:-1}
mkdir -p /tmp/mircov
: > /tmp/mircov/emitted.txt
: > /tmp/mircov/fallback.txt
: > /tmp/mircov/fail.txt
: > /tmp/mircov/reasons.txt
i=0
total=0
emitted=0
fallback=0
fail=0
for f in tests/*.no; do
  i=$((i+1))
  if [ "$STRIDE" != "1" ]; then
    chk=$(( (i - STARTIDX) % STRIDE ))
    [ "$chk" -ne 0 ] && continue
  fi
  total=$((total+1))
  env -u NOLANG_DEBUG_IT NOLANG_MIR=2 NOLANG_MIR_DEBUG=1 "$NO" build "$f" >/dev/null 2>/tmp/mircov/err.txt
  rc=$?
  if [ "$rc" -ne 0 ]; then
    fail=$((fail+1)); echo "$f" >> /tmp/mircov/fail.txt; continue
  fi
  # Real fallback markers emitted by emitMIR only on fallback:
  if grep -Eq 'memory analysis found unsafe constructs|EmitLLVM failed|opt verification failed|recovered panic' /tmp/mircov/err.txt; then
    fallback=$((fallback+1))
    line=$(grep -E 'memory analysis found unsafe constructs|EmitLLVM failed|opt verification failed|recovered panic' /tmp/mircov/err.txt | head -1 | sed -E 's/.*\[MIR\] *//')
    key=$(echo "$line" | sed -E 's/:.*//')
    echo "$key" >> /tmp/mircov/reasons.txt
    echo "$f :: $line" >> /tmp/mircov/fallback.txt
  else
    emitted=$((emitted+1)); echo "$f" >> /tmp/mircov/emitted.txt
  fi
done
echo "total=$total emitted(MIR)=$emitted fallback(legacy)=$fallback buildfail=$fail"
echo "--- fallback reasons (top 25) ---"
sort /tmp/mircov/reasons.txt | uniq -c | sort -rn | head -25
