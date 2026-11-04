#!/usr/bin/env python3
"""Fail loudly when Nolang's internal ABI no longer matches the C shim.

`no build` emits user functions with an INTERNAL by-reference ABI: inputs are
passed by value, results are written through a caller-supplied out-pointer.
That ABI is not a public contract, so this script pins the exact signatures
that shim/nolang_shim.c declares. Run it after every `no build -v` and before
compiling the shim.

Exit 1 => do NOT patch the shim blindly: re-read the .ll, re-probe, and update
both EXPECTED below and shim/nolang_shim.c together.
"""

import re
import sys
from pathlib import Path

# symbol -> exact LLVM parameter type sequence the shim is written against.
EXPECTED = {
    "nl_add": ["i64", "i64", "i64*"],
    "nl_scale": ["double", "double", "double*"],
    "nl_sum3": ["i64", "i64", "i64", "i64*"],
}

DEFINE = re.compile(
    r"^define\s+(?P<linkage>(?:internal|private|linkonce|weak)\s+)?"
    r"void\s+@(?P<name>[A-Za-z0-9_.$]+)\s*\((?P<params>.*?)\)\s*\{",
    re.M,
)
ATTRIBUTE = re.compile(
    r"\b(?:nocapture|readonly|writeonly|nonnull|noalias|noundef|signext|"
    r"zeroext|align\s+\d+|dereferenceable(?:_or_null)?\([^)]*\)|"
    r"captures\([^)]*\)|initializes\([^)]*\)|byval\([^)]*\)|sret\([^)]*\))",
)


def type_of(param: str) -> str:
    """Extract the LLVM type from one parameter declaration."""
    param = param.strip()
    # Everything before the first " %name" token is the type (plus attrs).
    match = re.match(r"^(.*?)\s+%\S", param)
    ty = match.group(1) if match else param
    ty = ATTRIBUTE.sub("", ty)
    return re.sub(r"\s+", " ", ty).strip()


def main() -> int:
    if len(sys.argv) not in (2, 3):
        print("usage: abi_guard.py <libnolang.ll> [signature-report.txt]", file=sys.stderr)
        return 2

    ll_path = Path(sys.argv[1])
    if not ll_path.exists():
        print(f"[abi-guard] FAIL: {ll_path} does not exist", file=sys.stderr)
        return 1
    text = ll_path.read_text()

    found = {}
    for match in DEFINE.finditer(text):
        name = match.group("name")
        types = [type_of(p) for p in match.group("params").split(",") if p.strip()]
        found.setdefault(name, []).append((match.group("linkage") or "", types))

    problems = []
    for name, want in EXPECTED.items():
        entries = found.get(name, [])
        if not entries:
            problems.append(
                f"@{name} is MISSING. Usually this means (a) the top-level "
                f"reachability anchors were folded away by comptime, or "
                f"(b) the symbol gained a collision suffix / was renamed."
            )
            continue
        if len(entries) != 1:
            problems.append(f"@{name} is defined {len(entries)} times")
            continue
        linkage, got = entries[0]
        if linkage:
            problems.append(f"@{name} is '{linkage.strip()}' and cannot be linked from C")
        if any(t == "ptr" or "ptr" in t for t in got):
            problems.append(
                f"@{name} uses opaque `ptr`; the compiler switched pointer style. "
                f"Re-probe and update EXPECTED, or use the unoptimized .ll."
            )
        if got != want:
            problems.append(
                f"@{name} ABI DRIFT\n      actual   {got}\n      expected {want}"
            )

    has_main = bool(re.search(r"^define\s+i32\s+@main\s*\(", text, re.M))
    triple = re.search(r'^target triple\s*=\s*"([^"]+)"', text, re.M)

    if problems:
        print("=== NOLANG ABI GUARD FAILED ===", file=sys.stderr)
        for problem in problems:
            print(f"  - {problem}", file=sys.stderr)
        print(f"  define i32 @main present: {has_main}", file=sys.stderr)
        print(
            "  Inspect with: grep -nE '^define .*@nl_' " + str(ll_path),
            file=sys.stderr,
        )
        return 1

    print(
        f"[abi-guard] ok | @main present={has_main} | "
        f"triple={triple.group(1) if triple else 'n/a'}"
    )
    report_lines = []
    for name in EXPECTED:
        got = found[name][0][1]
        print(f"[abi-guard]   define void @{name}({', '.join(got)})")
        report_lines.append(f"{name}: {' '.join(got)}")
    if len(sys.argv) == 3:
        Path(sys.argv[2]).write_text("\n".join(report_lines) + "\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
