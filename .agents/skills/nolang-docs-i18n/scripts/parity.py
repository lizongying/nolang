#!/usr/bin/env python3
"""Report how far the English i18n docs lag behind the Chinese source docs.

Three independent checks, each printed as its own section:

  1. unsynced commits   -- ZH doc commits that never touched the EN doc
  2. structure parity   -- heading count / level distribution / h2 sequence
  3. code parity        -- per-ZH-section code blocks that are absent from EN

Usage:
    python3 parity.py                       # every docs/docs/**/*.md with an EN twin
    python3 parity.py docs/docs/lang/syntax.md
    python3 parity.py --quiet               # only files with findings
    python3 parity.py --code-diff docs/docs/std/global.md   # triage one file by hand

Exit code is 0 when nothing is missing, 1 when at least one check found a gap,
2 on a usage / path error.
"""

from __future__ import annotations

import argparse
import difflib
import re
import subprocess
import sys
from pathlib import Path

ZH_PREFIX = Path("docs/docs")
EN_PREFIX = Path("docs/i18n/en/docusaurus-plugin-content-docs/current")


# --------------------------------------------------------------------------- #
# paths
# --------------------------------------------------------------------------- #
def repo_root() -> Path:
    try:
        out = subprocess.run(
            ["git", "rev-parse", "--show-toplevel"],
            capture_output=True, text=True, check=True,
        )
        return Path(out.stdout.strip())
    except (subprocess.CalledProcessError, FileNotFoundError):
        return Path.cwd()


def en_twin(zh: Path, root: Path) -> Path | None:
    """docs/docs/lang/syntax.md -> docs/i18n/en/.../current/lang/syntax.md"""
    try:
        rel = zh.resolve().relative_to((root / ZH_PREFIX).resolve())
    except ValueError:
        return None
    return root / EN_PREFIX / rel


# --------------------------------------------------------------------------- #
# markdown helpers
# --------------------------------------------------------------------------- #
HEADING = re.compile(r"^(#{1,6})\s+(.*)$")
# A fence may sit inside a blockquote (`> ```no`). Strip the quote markers first,
# otherwise the pairing drifts by one and every later block is mis-parsed.
QUOTE_PREFIX = re.compile(r"^>\s?")


def strip_quote(line: str) -> str:
    return QUOTE_PREFIX.sub("", line)


def parse_headings(lines: list[str]) -> list[tuple[int, str, int]]:
    """Real markdown headings only - skip anything inside a code fence.

    Shell / `no` comments inside code blocks start with `#` and would otherwise
    be counted as headings (docs/docs/usage.md reports 82 that way vs 8 real h2s,
    and each one also splits a section in two for the code-parity check).
    """
    out = []
    in_fence = False
    for i, line in enumerate(lines):
        if strip_quote(line).startswith("```"):
            in_fence = not in_fence
            continue
        if in_fence:
            continue
        m = HEADING.match(line)
        if m:
            out.append((len(m.group(1)), m.group(2).strip(), i))
    return out


def sections(lines: list[str]) -> list[tuple[int, str, int, int]]:
    heads = parse_headings(lines)
    return [
        (lvl, title, start, heads[k + 1][2] if k + 1 < len(heads) else len(lines))
        for k, (lvl, title, start) in enumerate(heads)
    ]


def code_blocks(lines: list[str], start: int, end: int) -> list[str]:
    blocks: list[str] = []
    buf: list[str] | None = None
    for j in range(start, min(end, len(lines))):
        if strip_quote(lines[j]).startswith("```"):
            if buf is None:
                buf = []
            else:
                body = "\n".join(buf).strip()
                if body:
                    blocks.append(body)
                buf = None
        elif buf is not None:
            buf.append(lines[j])
    return blocks


def normalize(block: str) -> list[str]:
    """Reduce a code block to a language-independent skeleton.

    Three sources of noise are removed, in this order:

      * quote markers (`> `) from blockquoted fences
      * comments - `;`, `//` and `#` (but NOT `#{...}`, which is real syntax)
      * every non-ASCII character

    Without the comment strip, docs whose code blocks carry Chinese comments
    report a gap for almost every block, because EN translates those comments
    (`no build # 构建当前目录` vs `no build # Build current directory`). Without
    the non-ASCII strip the same happens for translated string literals.
    """
    out = []
    for line in block.splitlines():
        line = strip_quote(line)
        line = re.split(r";|//", line)[0]
        # `#` starts a comment, `#{` starts an annotation - keep the latter.
        line = re.split(r"#(?!\{)", line)[0]
        line = re.sub(r"[^\x20-\x7e]", "", line)
        line = re.sub(r"\s+", " ", line).strip().strip("`").strip()
        if line:
            out.append(line)
    return out


def block_set(path: Path) -> set[str]:
    lines = path.read_text().splitlines()
    out = set()
    for _, _, s, e in sections(lines):
        for b in code_blocks(lines, s, e):
            n = normalize(b)
            if n:
                out.add("\n".join(n))
    return out


# --------------------------------------------------------------------------- #
# checks
# --------------------------------------------------------------------------- #
def git(root: Path, *args: str) -> str:
    try:
        return subprocess.run(
            ["git", "-C", str(root), *args],
            capture_output=True, text=True, check=True,
        ).stdout
    except (subprocess.CalledProcessError, FileNotFoundError):
        return ""


def check_commits(root: Path, zh: Path, en: Path, limit: int = 40) -> list[str]:
    """ZH commits that changed the ZH doc without changing the EN doc."""
    rel_zh = zh.resolve().relative_to(root)
    rel_en = en.resolve().relative_to(root)
    shas = git(root, "log", f"-{limit}", "--format=%h", "--", str(rel_zh)).split()
    rows = []
    for sha in shas:
        numstat = git(root, "show", "--numstat", "--format=", sha, "--", str(rel_zh)).strip()
        en_stat = git(root, "show", "--numstat", "--format=", sha, "--", str(rel_en)).strip()
        subject = git(root, "log", "-1", "--format=%s", sha).strip()
        if not numstat:
            continue
        if not en_stat:
            added = numstat.split("\t")[0]
            rows.append(f"{sha}  ZH +{added:<5} EN  (missing)  {subject}")
    return rows


def check_structure(zh_lines: list[str], en_lines: list[str]) -> list[str]:
    zh_h = parse_headings(zh_lines)
    en_h = parse_headings(en_lines)
    problems = []
    if len(zh_h) != len(en_h):
        problems.append(f"heading count: ZH {len(zh_h)} vs EN {len(en_h)}")
    zh_lv = [l for l, _, _ in zh_h]
    en_lv = [l for l, _, _ in en_h]
    if zh_lv != en_lv:
        problems.append("heading level sequence differs")
    zh_h2 = [t for l, t, _ in zh_h if l == 2]
    en_h2 = [t for l, t, _ in en_h if l == 2]
    if len(zh_h2) != len(en_h2):
        problems.append(f"h2 count: ZH {len(zh_h2)} vs EN {len(en_h2)}")
    return problems


def check_code(zh_lines: list[str], en_set: set[str]) -> list[str]:
    problems = []
    for lvl, title, s, e in sections(zh_lines):
        blocks = ["\n".join(n) for n in (normalize(b) for b in code_blocks(zh_lines, s, e))]
        blocks = [b for b in blocks if b]
        if not blocks:
            continue
        missing = [b for b in blocks if b not in en_set]
        if not missing:
            continue
        tag = "ALL " if len(missing) == len(blocks) else "PART"
        problems.append(f"[{tag} h{lvl}] {title}  (ZH lines {s + 1}-{e})")
        if len(missing) != len(blocks):
            for m in missing:
                problems.append(f"          missing: {m.splitlines()[0][:80]}")
    return problems


def check_fences(lines: list[str]) -> list[str]:
    fences = [i + 1 for i, l in enumerate(lines) if strip_quote(l).startswith("```")]
    if len(fences) % 2:
        return [f"odd number of code fences ({len(fences)}) - pairing is broken"]
    return []


# --------------------------------------------------------------------------- #
# main
# --------------------------------------------------------------------------- #
def process(zh: Path, en: Path, root: Path, with_commits: bool) -> bool:
    zh_lines = zh.read_text().splitlines()
    en_lines = en.read_text().splitlines()
    findings = False

    print(f"### {zh.relative_to(root)}  ->  {en.relative_to(root)}")
    print(f"    lines: ZH {len(zh_lines)} / EN {len(en_lines)}")

    for label, problems in (
        ("fences", check_fences(en_lines)),
        ("structure", check_structure(zh_lines, en_lines)),
        ("code parity", check_code(zh_lines, block_set(en))),
    ):
        if problems:
            findings = True
            print(f"  -- {label}:")
            for p in problems:
                print(f"     {p}")

    if with_commits:
        rows = check_commits(root, zh, en)
        if rows:
            findings = True
            print("  -- ZH commits never mirrored into EN:")
            for r in rows:
                print(f"     {r}")

    if not findings:
        print("    ok - no gaps found")
    print()
    return findings


def code_diff(zh: Path, en: Path) -> int:
    """Whole-file unified diff of the normalized code skeleton.

    Use this to triage a file that `code parity` flagged: if the only deltas are
    translated string literals or EN being more explicit about types, it is not a
    real gap. A run of `-` lines with no `+` counterpart is a real gap.
    """
    zl = zh.read_text().splitlines()
    el = en.read_text().splitlines()
    z = [x for b in code_blocks(zl, 0, len(zl)) for x in normalize(b)]
    e = [x for b in code_blocks(el, 0, len(el)) for x in normalize(b)]
    print(f"### {zh} (ZH {len(z)} code lines)  vs  {en} (EN {len(e)} code lines)")
    diff = list(difflib.unified_diff(z, e, "zh", "en", lineterm="", n=1))
    if not diff:
        print("    identical code skeleton")
        return 0
    for line in diff:
        print("   ", line)
    return 1


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("files", nargs="*", help="ZH doc paths (default: every docs/docs/**/*.md with an EN twin)")
    ap.add_argument("--no-commits", action="store_true", help="skip the git commit-archaeology check")
    ap.add_argument("--quiet", action="store_true", help="only print files that have findings")
    ap.add_argument("--code-diff", action="store_true",
                    help="print the whole-file code-skeleton diff for triage, instead of the three checks")
    args = ap.parse_args()

    root = repo_root()
    if args.files:
        targets = [Path(f).resolve() for f in args.files]
    else:
        targets = sorted((root / ZH_PREFIX).resolve().rglob("*.md"))

    if args.code_diff:
        rc = 0
        for zh in targets:
            en = en_twin(zh, root)
            if en is None or not en.exists():
                print(f"{zh}: no EN twin", file=sys.stderr)
                rc = 2
                continue
            rc |= code_diff(zh, en)
        return rc

    pairs = []
    for zh in targets:
        en = en_twin(zh, root)
        if en is None or not en.exists():
            continue
        pairs.append((zh, en))

    if not pairs:
        print("no ZH/EN doc pairs found", file=sys.stderr)
        return 2

    any_gap = False
    for zh, en in pairs:
        zh_lines = zh.read_text().splitlines()
        en_lines = en.read_text().splitlines()
        quiet_ok = (
            not check_fences(en_lines)
            and not check_structure(zh_lines, en_lines)
            and not check_code(zh_lines, block_set(en))
        )
        if args.quiet and quiet_ok:
            continue
        any_gap |= process(zh, en, root, with_commits=not args.no_commits)

    return 1 if any_gap else 0


if __name__ == "__main__":
    sys.exit(main())
