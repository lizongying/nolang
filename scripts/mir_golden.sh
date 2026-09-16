#!/usr/bin/env bash
# MIR behavioural-golden freeze / compare.
#
# WHY THIS EXISTS
# ---------------
# `MATCH` / `DIVERGE` / `MIR_GAP` (see scripts/mir_sweep_fast.sh) derive *all* of
# their information from a second backend: the run of the same file under
# NOLANG_MIR=0 (legacy). Once the legacy backend (src/build/llvm/) is deleted
# that comparison becomes impossible, and with it the only automatic detector for
# the DIVERGE class — two backends both exiting 0 while producing DIFFERENT
# output. That class is not hypothetical: §13.3.13 #77 (platform-variant
# filtering in script mode) was exactly a DIVERGE-class bug, invisible to a
# single-backend "did it exit 0?" scan.
#
# So before removing legacy we freeze its behaviour into a compact fingerprint
# file, and afterwards compare the remaining backend against that frozen file.
#
# FINGERPRINT
# -----------
# One TSV line per corpus file:  <rc> <sha256-of-stdout> <path>
# stdout only — the hash must be stable, and stderr embeds temp paths that
# differ every run. rc is kept because a "golden ok -> now fails" transition
# (the REGRESS bucket) is the post-deletion analogue of MIR_GAP.
#
# WHAT rc=124 MEANS — and why the timeout is 300s, not 90s
# -------------------------------------------------------
# 124 is "the harness's own alarm fired", i.e. "no verdict". It is NOT a
# property of the file. Three very different things land there:
#   (a) genuine infinite loop        -> 124 at ANY timeout  (tests/test-for2.no)
#   (b) slow-but-finite compile      -> 124 only below its cost
#   (c) contention                  -> 124 when -P parallelism starves the box
# (c) is not hypothetical, it is measured: tests/test-parse-min.no takes
# 32.9s / rc=0 run alone, yet recorded **124** in the frozen mir-baseline
# because the sweep runs 8 builds at once and LLVM opt is CPU-bound. Its
# fingerprint hash is the empty-stdout hash, which is the tell: a program
# that prints instantly never got to run.
#
# The consequence is worse than a wrong number: a file that times out cannot
# report a *behaviour* regression. The two json/parse cases compile in
# 111-120s and then FAIL AT RUNTIME (rc=1) — invisible as long as they sit
# above the cap, visible the moment they sit below it. So the cap is set at
# 300s and the job count lowered: the oracle exists to be accurate, and it is
# run by hand after backend changes, not in a loop.
#   MIR_GOLDEN_TIMEOUT=<sec>   (default 300) raise to separate (b) from (a)
#   MIR_GOLDEN_JOBS=<n>        (default 4)   lower to reduce (c)
#
# USAGE
#   GOLDEN=tests/golden/mir-baseline.tsv GOLDEN_MIR=default scripts/mir_golden.sh -update
#   GOLDEN=tests/golden/mir-baseline.tsv scripts/mir_golden.sh
#
# ⚠️ The -update line used to read `scripts/mir_golden.sh -update mir-baseline.tsv`.
# That was WRONG and dangerous: there is no positional argument — the path comes
# from $GOLDEN only — so the trailing `mir-baseline.tsv` was silently ignored and
# $GOLDEN fell back to its default, legacy-baseline.tsv. Following the documented
# command would therefore OVERWRITE the semantic oracle with a default-backend
# capture, and (because the label is derived from the basename) that file would
# then describe itself as a legacy capture while holding MIR output. Fixed here,
# and closed off by the "never -update a *legacy* golden" guard below.
#
# legacy-baseline.tsv can NO LONGER be regenerated (see the guards below) — it is
# a historical artefact, and that is the point of keeping it.
#
# Two goldens are worth keeping:
#   tests/golden/legacy-baseline.tsv  (GOLDEN_MIR=0)
#       what the pure legacy backend computes. The SEMANTIC oracle. Frozen
#       2026-09-16, before src/build/llvm/ was deleted — NOT regenerable (see
#       the -update guard). Beware: the
#       legacy backend is broken on ~19% of the corpus (it cannot even compile
#       `print(1+2)` — the `%addopt.final` option-not-unwrapped bug), so its
#       rc!=0 entries mean "no reference available here", not "expected failure".
#   tests/golden/mir-baseline.tsv     (GOLDEN_MIR=default)
#       what the default backend computes TODAY. The REGRESSION oracle: after
#       the legacy deletion this is the only thing that can tell you a change
#       made output worse.
#
# The compare mode is the replacement for the second backend in the sweep: it is
# always run with whatever `no` builds today (no NOLANG_MIR forced), so it keeps
# working after the legacy path is gone.
export PATH="/usr/bin:/opt/homebrew/opt/llvm/bin:$PATH"
cd /Users/lizongying/IdeaProjects/no || exit 1

NO=${NO:-./bin/no}
GOLDEN=${GOLDEN:-tests/golden/legacy-baseline.tsv}
GOLDEN_MIR=${GOLDEN_MIR:-0}
WORKDIR=/tmp/mir_golden
# 300s, not 90s: see the "WHAT rc=124 MEANS" block above. At 90s the three
# json/parse files never yield a verdict, so their rc=1 runtime failure (the
# thing a regression oracle exists to catch) is structurally unobservable.
TMO=${MIR_GOLDEN_TIMEOUT:-300}
JOBS=${MIR_GOLDEN_JOBS:-4}
# Files whose stdout is legitimately NONDETERMINISTIC, so a hash mismatch is
# not evidence of a regression. They are reported as UNSTABLE rather than
# DIVERGE: DIVERGE must stay a clean signal, and an entry that can never match
# would otherwise print on every single run and train the reader to ignore it.
#
# Each entry needs a REASON, or the list becomes a place to hide breakage.
#
# Currently EMPTY, and that is a deliberate state, not an oversight. The list was
# introduced (round 42) with exactly one entry:
#
#   tests/mem-safety/str-concat-leak.no
#     #85: printed 0..775 MB of garbage with rc=0, because `print('item' +
#     i.to-str())` in a count-for lowered the concat's right operand to `undef`
#     (byte count varied run to run, so the hash could not be frozen). The note
#     here said "Remove it once #85 is fixed."
#
# Round 43 added the loadVal guard, so the file now FAILS TO COMPILE (rc=1,
# deterministic, empty stdout) instead of printing an unpredictable amount of
# rubbish. Its fingerprint is therefore freezable again and the entry has been
# removed. Keep it that way: should the guard ever be reverted, the hash
# mismatch that reappears is a CORRECT DIVERGE signal for a real bug — not
# noise to be suppressed. Only re-add an entry together with (a) a concrete
# reason and (b) a bug number, and only when the nondeterminism is *inherent*
# to the program rather than a symptom of a defect.
UNSTABLE=${MIR_GOLDEN_UNSTABLE:-""}
MODE="compare"
[ "$1" = "-update" ] && MODE="update"

# GUARD: never re-freeze the legacy oracle from a machine that has no legacy
# backend. `src/build/llvm/` is deleted, so `NOLANG_MIR=0` is now a hard error
# — an `-update` with GOLDEN_MIR=0 would write a "capture" in which every one
# of the 422 files exits non-zero, silently destroying the only record of what
# legacy actually computed (the SEMANTIC oracle) and leaving a file that looks
# like a legitimate baseline. There is no override on purpose: if the legacy
# backend is ever reinstated, that change should also remove this guard.
if [ "$MODE" = "update" ] && [ "$GOLDEN_MIR" != "default" ]; then
  echo "ERROR: refusing to re-freeze a non-default oracle (GOLDEN_MIR=$GOLDEN_MIR)." >&2
  echo "       The legacy backend (NOLANG_MIR=0) no longer exists and is a hard" >&2
  echo "       error, so this capture would be all-fail garbage. The existing" >&2
  echo "       legacy-baseline.tsv is a historical artefact — keep it read-only." >&2
  echo "       To capture the CURRENT backend use: GOLDEN_MIR=default $0 -update <file>" >&2
  exit 2
fi

# GUARD 2: never -update a file whose NAME says "legacy", whatever GOLDEN_MIR is.
# The guard above only blocks `GOLDEN_MIR=0`. A plain
# `GOLDEN_MIR=default scripts/mir_golden.sh -update` with $GOLDEN left at its
# default would still land on legacy-baseline.tsv and overwrite the semantic
# oracle with a *default-backend* capture — worse than the case above, because
# the file is then mislabelled as a legacy capture (the label is derived from the
# basename, see the compare mode) and every later semantic comparison becomes
# self-referential. The name is the only marker of intent we have; honour it.
case "$(basename "$GOLDEN")" in
  *legacy*)
    echo "ERROR: refusing to overwrite '$GOLDEN' — its name marks it as the" >&2
    echo "       captured LEGACY backend, which can no longer be produced." >&2
    echo "       Capture the current backend into mir-baseline.tsv instead:" >&2
    echo "         GOLDEN=tests/golden/mir-baseline.tsv GOLDEN_MIR=default $0 -update" >&2
    exit 2 ;;
esac

# In COMPARE mode the backend under test is ALWAYS the current default — the
# golden file is the only thing that differs between a "semantic" run (vs the
# frozen legacy) and a "regression" run (vs the frozen default). Forcing
# NOLANG_MIR here would be a trap: after the legacy backend is deleted,
# `NOLANG_MIR=0` makes every single corpus file exit non-zero, which reads as
# "389 regressions" instead of "the harness is asking for a deleted backend".
# Only -update needs GOLDEN_MIR, to decide what to *capture*.
FORCE_MIR=""
# Unreachable since the -update guard above (it exits for any GOLDEN_MIR other
# than "default"), kept because it is the mechanism `fingerprint_one` still
# reads: empty means "the current default backend". The legacy capture path is
# intentionally gone, not forgotten.
if [ "$MODE" = "update" ] && [ "$GOLDEN_MIR" != "default" ]; then
  FORCE_MIR="$GOLDEN_MIR"
fi

mkdir -p "$WORKDIR" "$(dirname "$GOLDEN")"
rm -f "$WORKDIR"/*.txt "$WORKDIR"/*.out

# run_to <seconds> <cmd...> — alarm-based timeout with process-group kill.
#
# The naive `perl -e 'alarm shift; exec @ARGV'` only signals the DIRECT child:
# on expiry perl dies but the exec'd process (plus anything it spawned — clang,
# the compiled program, a `no run` grandchild) keeps running detached. Here the
# child gets its OWN process group (setpgid; macOS ships no `setsid`) and expiry
# does `kill -KILL -$pgid`, nuking the whole tree. Exit 142 on expiry.
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

# sha_of <file> — portable sha256 of a file's contents (macOS has shasum).
sha_of() {
  shasum -a 256 "$1" 2>/dev/null | awk '{print $1}'
}

# fingerprint_one — emit "<rc> <sha256> <path>" for one corpus file.
#
# $FORCE_MIR selects which backend is captured (empty = current default):
#   0        NOLANG_MIR=0 — the true (pure) legacy backend. This is the
#            SEMANTIC oracle: what legacy actually computes. Note it is broken
#            on ~19% of the corpus (rc=1), so it is not a pass/fail reference
#            for those files. Only meaningful for -update, since the backend no
#            longer exists after the deletion.
#   (empty)  no NOLANG_MIR set — the current default backend. The REGRESSION
#            oracle: after the legacy deletion this is the only thing that can
#            tell you a change made output worse.
fingerprint_one() {
  local f="$1"
  local tmp="$WORKDIR/$(echo "$f" | tr '/' '_').out"
  local rc
  if [ -z "$FORCE_MIR" ]; then
    run_to "$TMO" "$NO" run "$f" >"$tmp" 2>/dev/null
  else
    run_to "$TMO" env NOLANG_MIR="$FORCE_MIR" "$NO" run "$f" >"$tmp" 2>/dev/null
  fi
  rc=$?
  # Timeout is reported as rc 124/142; normalise to 124 so the fingerprint does
  # not depend on which wrapper fired.
  if [ "$rc" = "142" ]; then rc=124; fi
  local h
  h=$(sha_of "$tmp")
  [ -z "$h" ] && h="e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  echo "$rc $h $f" >> "$WORKDIR/fp.txt"
  rm -f "$tmp"
}
export -f fingerprint_one run_to sha_of
export NO WORKDIR TMO MODE GOLDEN_MIR FORCE_MIR

find tests -name '*.no' -print0 | xargs -0 -P "$JOBS" -I{} bash -c 'fingerprint_one "$@"' _ {}

sort -k3 "$WORKDIR/fp.txt" > "$WORKDIR/fp.sorted.tsv"

if [ "$MODE" = "update" ]; then
  cp "$WORKDIR/fp.sorted.tsv" "$GOLDEN"
  echo "=== GOLDEN FROZEN ==="
  echo "file: $GOLDEN"
  echo "entries: $(wc -l < "$GOLDEN" | tr -d ' ')"
  echo "rc=0 entries: $(awk '$1==0' "$GOLDEN" | wc -l | tr -d ' ')"
  echo "rc!=0 entries: $(awk '$1!=0' "$GOLDEN" | wc -l | tr -d ' ')"
  exit 0
fi

if [ ! -f "$GOLDEN" ]; then
  echo "ERROR: golden file $GOLDEN not found — run '$0 -update' first" >&2
  exit 2
fi

# join golden (rc/hash) with current (rc/hash) on the path column (field 3)
join -1 3 -2 3 -o 0,1.1,1.2,2.1,2.2 -t' ' \
  <(sort -k3 "$GOLDEN") "$WORKDIR/fp.sorted.tsv" > "$WORKDIR/join.txt" 2>/dev/null

: > "$WORKDIR/c_same.txt"; : > "$WORKDIR/c_diverge.txt"; : > "$WORKDIR/c_regress.txt"
: > "$WORKDIR/c_improved.txt"; : > "$WORKDIR/c_bothfail.txt"; : > "$WORKDIR/c_new.txt"
: > "$WORKDIR/c_unstable.txt"

while read -r path grc gh nrc nh; do
  if [ "$grc" = "0" ] && [ "$nrc" = "0" ]; then
    if [ "$gh" = "$nh" ]; then echo "$path" >> "$WORKDIR/c_same.txt"
    else
      case " $UNSTABLE " in
        *" $path "*) echo "$path" >> "$WORKDIR/c_unstable.txt" ;;
        *)           echo "$path" >> "$WORKDIR/c_diverge.txt" ;;
      esac
    fi
  elif [ "$grc" = "0" ] && [ "$nrc" != "0" ]; then
    echo "$path (now rc=$nrc)" >> "$WORKDIR/c_regress.txt"
  elif [ "$grc" != "0" ] && [ "$nrc" = "0" ]; then
    echo "$path (was rc=$grc)" >> "$WORKDIR/c_improved.txt"
  else
    echo "$path" >> "$WORKDIR/c_bothfail.txt"
  fi
done < "$WORKDIR/join.txt"

# files present now but absent from the golden (newly added tests)
cut -d' ' -f3 "$WORKDIR/fp.sorted.tsv" | sort > "$WORKDIR/now.paths"
cut -d' ' -f3 "$GOLDEN" | sort > "$WORKDIR/gold.paths"
comm -23 "$WORKDIR/now.paths" "$WORKDIR/gold.paths" > "$WORKDIR/c_new.txt"

cnt() { wc -l < "$1" | tr -d ' '; }

# Label the golden by what it actually froze, not by a hardcoded guess: the
# compare mode always runs the CURRENT default backend, so a legacy golden is a
# semantic check and a default golden is a regression check.
case "$(basename "$GOLDEN")" in
  *legacy*) GOLDEN_LABEL="frozen legacy backend (NOLANG_MIR=0)" ;;
  *)        GOLDEN_LABEL="frozen default backend" ;;
esac
echo "=== GOLDEN COMPARE (current default backend vs $GOLDEN_LABEL) ==="
echo "golden: $GOLDEN ($(cnt "$WORKDIR/gold.paths") entries)"
echo
echo "SAME=      $(cnt "$WORKDIR/c_same.txt")"
echo "DIVERGE=   $(cnt "$WORKDIR/c_diverge.txt")"
echo "UNSTABLE=  $(cnt "$WORKDIR/c_unstable.txt")     (known-nondeterministic output — see \$MIR_GOLDEN_UNSTABLE)"
echo "REGRESS=   $(cnt "$WORKDIR/c_regress.txt")     (golden ok -> now fails)"
echo "IMPROVED=  $(cnt "$WORKDIR/c_improved.txt")     (golden failed -> now ok)"
echo "BOTH_FAIL= $(cnt "$WORKDIR/c_bothfail.txt")"
echo "NEW=       $(cnt "$WORKDIR/c_new.txt")     (not in golden)"
for b in diverge unstable regress improved new; do
  if [ -s "$WORKDIR/c_$b.txt" ]; then
    # macOS ships bash 3.2 — `${b^^}` (uppercase expansion) is a bash 4 feature
    # and aborts the script with "bad substitution". Use tr instead.
    echo ""
    echo "=== $(echo "$b" | tr 'a-z' 'A-Z') ==="
    sort "$WORKDIR/c_$b.txt"
  fi
done
