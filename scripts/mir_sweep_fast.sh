#!/usr/bin/env bash
# Fast parallel MIR coverage sweep.
#
# For every tests/**/*.no it runs the file twice — NOLANG_MIR=2 (lowering with
# strangler-fig fallback to the proven legacy backend; the parity baseline) and
# NOLANG_MIR=3 (MIR-only, no fallback) — and classifies the pair:
#
#   MATCH    both rc=0 and stdout is byte-identical
#   DIVERGE  both rc=0 but stdout differs
#   MIR_GAP  MIR=3 fails while the baseline succeeds  <- the real MIR gap
#   CERR     both fail, MIR=3 error looks like a compile/link failure
#   CRASH    both fail, MIR=3 error is a runtime signal
#   HANG     MIR=3 exceeded the per-test timeout
#   HANG_LEGACY  the MIR=2 baseline itself timed out (reported separately so a
#                baseline hang is never miscounted as MIRBETTER)
#
# The MIR_GAP / (CERR|CRASH) split is the whole point: without running the
# baseline every pre-existing failure looks like a MIR regression and the sweep
# says nothing useful. Each run is wrapped in a portable alarm-based timeout
# (macOS has no coreutils `timeout`) so one hanging server test cannot wedge
# the sweep forever.
export PATH="/usr/bin:/opt/homebrew/opt/llvm/bin:$PATH"
cd /Users/lizongying/IdeaProjects/no || exit 1
NO=./bin/no
WORKDIR=/tmp/mir_sweep
TMO=${MIR_SWEEP_TIMEOUT:-90}
mkdir -p "$WORKDIR"
rm -f "$WORKDIR"/*.txt

# run_to <seconds> <cmd...> — alarm-based timeout (exit 142 on expiry).
#
# The naive form `perl -e 'alarm shift; exec @ARGV'` only signals the DIRECT
# child: on expiry the perl process dies but the exec'd process (and anything it
# spawned, e.g. clang / the compiled program / a `no run` grandchild) keeps
# running detached, so sweep_one can block forever and `xargs -P` never returns.
# Here the child is put in its OWN process group (setpgid, since macOS ships no
# `setsid`) and on expiry we `kill -KILL -$pgid` — nuking the entire tree.
run_to() {
  perl -MPOSIX -e '
    my $t = shift;
    my $pid = fork();
    exit 127 unless defined $pid;
    if ($pid == 0) { POSIX::setpgid(0, 0); exec @ARGV; exit 127; }
    my $timedout = 0;
    $SIG{ALRM} = sub {
      $timedout = 1;
      kill("-KILL", $pid);
      kill("KILL", $pid);
    };
    alarm $t;
    waitpid($pid, 0);
    alarm 0;
    my $st = $?;
    exit 142 if $timedout;
    exit(($st & 127) ? 128 + ($st & 127) : ($st >> 8));
  ' "$@"
}

sweep_one() {
  local f="$1"
  local name
  name=$(basename "$f" .no)
  local out2="$WORKDIR/${name}.o2"
  local out3="$WORKDIR/${name}.o3"
  local err3="$WORKDIR/${name}.e3"
  run_to "$TMO" env NOLANG_MIR=2 "$NO" run "$f" >"$out2" 2>/dev/null
  local rc2=$?
  run_to "$TMO" env NOLANG_MIR=3 "$NO" run "$f" >"$out3" 2>"$err3"
  local rc3=$?
  if [ "$rc3" = "124" ] || [ "$rc3" = "142" ]; then
    echo "HANG $f" >> "$WORKDIR/results.txt"
  elif [ "$rc2" = "124" ] || [ "$rc2" = "142" ]; then
    # Baseline itself hung: must NOT fall through to MIRBETTER (rc2 != 0,
    # rc3 == 0) which would report a hang as an MIR success.
    echo "HANG_LEGACY $f" >> "$WORKDIR/results.txt"
  elif [ "$rc3" != "0" ]; then
    if [ "$rc2" = "0" ]; then
      echo "MIR_GAP $f" >> "$WORKDIR/results.txt"
    # NOTE: must be `grep -E` with plain `|`. BSD grep's BRE does NOT treat
    # `\|` as alternation (it matches a literal '|'), so the original
    # `grep -q "compilation error\|Undefined symbols\|..."` NEVER matched and
    # every both-fail test was misreported as CRASH (CERR was silently always
    # 0). Use -E (extended regex) where `|` really is alternation.
    elif grep -Eq "compilation error|Undefined symbols|ld: |cannot find|error: linker|undefined reference" "$err3" 2>/dev/null; then
      echo "CERR $f" >> "$WORKDIR/results.txt"
    else
      echo "CRASH $f" >> "$WORKDIR/results.txt"
    fi
  elif [ "$rc2" != "0" ]; then
    echo "MIRBETTER $f" >> "$WORKDIR/results.txt"
  elif diff -q "$out2" "$out3" >/dev/null 2>&1; then
    echo "MATCH $f" >> "$WORKDIR/results.txt"
  else
    echo "DIVERGE $f" >> "$WORKDIR/results.txt"
  fi
}
export -f sweep_one run_to
export NO WORKDIR TMO

# Find all .no files
find tests -name '*.no' -print0 | xargs -0 -P 8 -I{} bash -c 'sweep_one "$@"' _ {}

# Summarize
echo "=== SUMMARY ==="
grep -c "^MATCH" "$WORKDIR/results.txt" 2>/dev/null | xargs echo "MATCH="
grep -c "^DIVERGE" "$WORKDIR/results.txt" 2>/dev/null | xargs echo "DIVERGE="
grep -c "^MIR_GAP" "$WORKDIR/results.txt" 2>/dev/null | xargs echo "MIR_GAP="
grep -c "^MIRBETTER" "$WORKDIR/results.txt" 2>/dev/null | xargs echo "MIRBETTER="
grep -c "^CERR" "$WORKDIR/results.txt" 2>/dev/null | xargs echo "CERR=(both fail)"
grep -c "^CRASH" "$WORKDIR/results.txt" 2>/dev/null | xargs echo "CRASH=(both fail)"
grep -c "^HANG " "$WORKDIR/results.txt" 2>/dev/null | xargs echo "HANG="
grep -c "^HANG_LEGACY" "$WORKDIR/results.txt" 2>/dev/null | xargs echo "HANG_LEGACY=(baseline hung)="
echo ""
echo "=== MIR_GAP (baseline ok, MIR=3 fails) ==="
grep "^MIR_GAP" "$WORKDIR/results.txt" 2>/dev/null | sed 's/^MIR_GAP //' | sort
echo ""
echo "=== DIVERGE ==="
grep "^DIVERGE" "$WORKDIR/results.txt" 2>/dev/null | sed 's/^DIVERGE //' | sort
echo ""
echo "=== HANG ==="
grep "^HANG " "$WORKDIR/results.txt" 2>/dev/null | sed 's/^HANG //' | sort
echo ""
echo "=== HANG_LEGACY ==="
grep "^HANG_LEGACY" "$WORKDIR/results.txt" 2>/dev/null | sed 's/^HANG_LEGACY //' | sort

# The both-fail buckets are the actionable backlog: they are pre-existing
# failures that BOTH backends hit, so they need root-cause triage (stale test
# vs. std/frontend bug vs. real MIR gap) before they can be counted as MIR
# work. Printing only the counts forces a second 40-minute sweep just to learn
# WHICH files they are — so always dump the names too.
echo ""
echo "=== CERR (both fail, compile/link) ==="
grep "^CERR " "$WORKDIR/results.txt" 2>/dev/null | sed 's/^CERR //' | sort
echo ""
echo "=== CRASH (both fail, runtime signal) ==="
grep "^CRASH " "$WORKDIR/results.txt" 2>/dev/null | sed 's/^CRASH //' | sort
