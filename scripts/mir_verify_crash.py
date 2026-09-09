#!/usr/bin/env python3
import os, sys, subprocess, tempfile

ENV = dict(os.environ)
ENV["PATH"] = "/usr/bin:/opt/homebrew/opt/llvm/bin:" + ENV.get("PATH", "")
ROOT = "/Users/lizongying/IdeaProjects/no"
NO = "/tmp/no_mir"

# The 16 CRASH tests from the coverage scan
CRASH = [
    "match", "move2", "option", "slice1", "test-boundary.minimal", "test-call-heavy",
    "test-chain-copy", "test-fd-newtype", "test-i8-debug", "test-i8-debug2", "test-it-probe",
    "test-number-generic", "test-option-match", "test-rand-multiassign", "test-str-to-option",
    "test-x25519-keypair-diff",
]

def run_mir(f, mode):
    out = tempfile.NamedTemporaryFile(delete=False, suffix=".out")
    err = tempfile.NamedTemporaryFile(delete=False, suffix=".err")
    try:
        p = subprocess.run([NO, "run", f], env={**ENV, "NOLANG_MIR": str(mode)},
                           stdout=out, stderr=err, timeout=15)
        rc = p.returncode
        with open(out.name) as fh: o = fh.read()
        with open(err.name) as fh: e = fh.read()
        return rc, o, e
    except subprocess.TimeoutExpired:
        return 124, "", "TIMEOUT"
    finally:
        out.close(); err.close()

def main():
    overall = {"R": 0, "M": 0}
    for name in CRASH:
        f = os.path.join(ROOT, "tests", name + ".no")
        if not os.path.exists(f):
            print(f"[?] {name}: NO SUCH FILE"); continue
        rc2, o2, e2 = run_mir(f, 2)
        rc3, o3, e3 = run_mir(f, 3)
        # legacy baseline may also crash; only meaningful diff is MIR=3 output vs legacy
        if rc3 == 124:
            print(f"[HANG] {name}: rc3=124 (rc2={rc2})")
            overall["H"] = overall.get("H", 0) + 1
        elif rc3 != 0:
            print(f"[CRASH] {name}: rc3={rc3} (rc2={rc2})  err3={e3.strip()[:120]}")
            overall["R"] += 1
        elif o2 == o3 and rc2 == rc3:
            print(f"[MATCH] {name}: rc3=0 out==legacy")
            overall["M"] += 1
        else:
            # rc3==0 but differs -> silent error (REDLINE)
            print(f"[DIVERGE] {name}: rc3=0 but out!=legacy (rc2={rc2})")
            # show short diff
            a = o2.splitlines(); b = o3.splitlines()
            diff = []
            for i in range(max(len(a), len(b))):
                la = a[i] if i < len(a) else "<none>"
                lb = b[i] if i < len(b) else "<none>"
                if la != lb:
                    diff.append(f"  L{i+1}: legacy={la!r}  mir3={lb!r}")
            print("\n".join(diff[:15]))
            overall["D"] = overall.get("D", 0) + 1
    print("\n=== VERIFY 16 CRASH ===")
    print(f"MATCH={overall.get('M',0)} STILL_CRASH={overall.get('R',0)} "
          f"HANG={overall.get('H',0)} DIVERGE(silent)={overall.get('D',0)}")

if __name__ == "__main__":
    main()
