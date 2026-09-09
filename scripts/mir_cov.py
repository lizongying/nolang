#!/usr/bin/env python3
import os, sys, subprocess, glob, tempfile, shutil

ENV = dict(os.environ)
ENV["PATH"] = "/usr/bin:/opt/homebrew/opt/llvm/bin:" + ENV.get("PATH", "")
ROOT = "/Users/lizongying/IdeaProjects/no"
NO = os.path.join(ROOT, "no")

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
    files = sorted(glob.glob(os.path.join(ROOT, "tests", "*.no")))
    cats = {"pass": [], "cerr": [], "crash": [], "diverge": [], "hang": []}
    for f in files:
        rc2, o2, _ = run_mir(f, 2)
        rc3, o3, e3 = run_mir(f, 3)
        name = os.path.basename(f)
        if rc3 == 124:
            cats["hang"].append(name); continue
        if rc3 != 0:
            # A build/link failure is SAFE: no broken binary ships. Only a
            # *runtime* crash (the binary ran and aborted) is a red-line
            # violation. Classify accordingly so the CRASH count reflects real danger.
            buildfail = ("compilation error" in e3 or "Undefined symbols" in e3
                         or "ld: " in e3 or "cannot find" in e3
                         or "error: linker" in e3 or "undefined reference" in e3)
            if buildfail:
                cats["cerr"].append(name)
            else:
                cats["crash"].append(name)
            continue
        if o2 == o3 and rc2 == rc3:
            cats["pass"].append(name)
        else:
            cats["diverge"].append(name)
    print("=== SUMMARY ===")
    print(f"MATCH={len(cats['pass'])} COMPILE_ERR={len(cats['cerr'])} "
          f"RUNTIME_CRASH={len(cats['crash'])} DIVERGE={len(cats['diverge'])} HANG={len(cats['hang'])}")
    for c in ["pass", "cerr", "crash", "diverge", "hang"]:
        if cats[c]:
            print(f"\n--- {c} ({len(cats[c])}) ---")
            print("\n".join(cats[c]))

if __name__ == "__main__":
    main()
