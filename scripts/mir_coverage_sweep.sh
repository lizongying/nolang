#!/usr/bin/env bash
# MIR coverage sweep (timeout-per-test to avoid hangs on stdin/network/infinite-loop tests).
set -u
export PATH="/usr/bin:/opt/homebrew/opt/llvm/bin:$PATH"
cd /Users/lizongying/IdeaProjects/no
pass=0; cerr=0; crash=0; diverge=0; hang=0
> /tmp/mir_cov_pass.txt
> /tmp/mir_cov_cerr.txt
> /tmp/mir_cov_crash.txt
> /tmp/mir_cov_div.txt
> /tmp/mir_cov_hang.txt
for f in tests/*.no; do
  [ -f "$f" ] || continue
  NOLANG_MIR=2 timeout 15 ./no run "$f" >/tmp/o2.txt 2>/dev/null; rc2=$?
  NOLANG_MIR=3 timeout 15 ./no run "$f" >/tmp/o3.txt 2>/tmp/e3.txt; rc3=$?
  if [ "$rc3" = "124" ]; then hang=$((hang+1)); echo "$f" >> /tmp/mir_cov_hang.txt; continue; fi
  if [ "$rc3" != "0" ]; then
    if grep -q "compilation error" /tmp/e3.txt; then
      cerr=$((cerr+1)); echo "$f" >> /tmp/mir_cov_cerr.txt
    else
      crash=$((crash+1)); echo "$f" >> /tmp/mir_cov_crash.txt
    fi
    continue
  fi
  if diff -q /tmp/o2.txt /tmp/o3.txt >/dev/null 2>&1 && [ "$rc2" = "$rc3" ]; then
    pass=$((pass+1)); echo "$f" >> /tmp/mir_cov_pass.txt
  else
    diverge=$((diverge+1)); echo "$f" >> /tmp/mir_cov_div.txt
  fi
done
echo "=== SUMMARY ==="
echo "MATCH=$pass COMPILE_ERR=$cerr RUNTIME_CRASH=$crash DIVERGE=$diverge HANG(rc3=124)=$hang"
