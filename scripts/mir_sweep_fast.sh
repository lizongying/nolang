#!/usr/bin/env bash
# Fast parallel MIR coverage sweep
export PATH="/usr/bin:/opt/homebrew/opt/llvm/bin:$PATH"
cd /Users/lizongying/IdeaProjects/no
NO=./bin/no
WORKDIR=/tmp/mir_sweep
mkdir -p "$WORKDIR"
rm -f "$WORKDIR"/*.txt

sweep_one() {
  local f="$1"
  local name
  name=$(basename "$f" .no)
  local out2="$WORKDIR/${name}.o2"
  local out3="$WORKDIR/${name}.o3"
  local err3="$WORKDIR/${name}.e3"
  NOLANG_MIR=2 "$NO" run "$f" >"$out2" 2>/dev/null
  rc2=$?
  NOLANG_MIR=3 "$NO" run "$f" >"$out3" 2>"$err3"
  rc3=$?
  if [ "$rc3" = "124" ]; then
    echo "HANG $f" >> "$WORKDIR/results.txt"
  elif [ "$rc3" != "0" ]; then
    if grep -q "compilation error\|Undefined symbols\|ld: \|cannot find\|error: linker\|undefined reference" "$err3" 2>/dev/null; then
      echo "CERR $f" >> "$WORKDIR/results.txt"
    else
      echo "CRASH $f" >> "$WORKDIR/results.txt"
    fi
  elif diff -q "$out2" "$out3" >/dev/null 2>&1 && [ "$rc2" = "$rc3" ]; then
    echo "MATCH $f" >> "$WORKDIR/results.txt"
  else
    echo "DIVERGE $f" >> "$WORKDIR/results.txt"
  fi
}
export -f sweep_one
export NO WORKDIR

# Find all .no files
find tests -name '*.no' -print0 | xargs -0 -P 8 -I{} bash -c 'sweep_one "$@"' _ {}

# Summarize
echo "=== SUMMARY ==="
grep -c "^MATCH" "$WORKDIR/results.txt" 2>/dev/null | xargs echo "MATCH="
grep -c "^DIVERGE" "$WORKDIR/results.txt" 2>/dev/null | xargs echo "DIVERGE="
grep -c "^CERR" "$WORKDIR/results.txt" 2>/dev/null | xargs echo "CERR="
grep -c "^CRASH" "$WORKDIR/results.txt" 2>/dev/null | xargs echo "CRASH="
grep -c "^HANG" "$WORKDIR/results.txt" 2>/dev/null | xargs echo "HANG="
echo ""
echo "=== DIVERGE ==="
grep "^DIVERGE" "$WORKDIR/results.txt" 2>/dev/null | sed 's/^DIVERGE //' | sort
echo ""
echo "=== CRASH ==="
grep "^CRASH" "$WORKDIR/results.txt" 2>/dev/null | sed 's/^CRASH //' | sort
