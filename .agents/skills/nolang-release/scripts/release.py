#!/usr/bin/env python3
"""Nolang release helper (read-only by default, mutation only on `apply`).

Repo-agnostic: the repo root is auto-detected from the current working
directory via `git rev-parse --show-toplevel`. Nothing about a specific
project or user is hardcoded; `--repo` overrides the detection.

Subcommands
-----------
plan     Resolve the version to release, list commits since the last tag and
         print the exact commands to run. Never mutates the repo.
         EXIT 2 if the working tree is dirty -- the user must commit first.
apply    Prepend a `## vX.Y.Z` section to HISTORY.md (the only file mutation).
         Enforces English, conventional-commit style bullets.
verify   Run ./history.sh with no arguments and print what GitHub Actions will
         publish as the release body.

Usage
-----
    release.py plan  [--repo DIR] [--version v0.2.33]
    release.py apply --version v0.2.33 --body-file /tmp/notes.md [--repo DIR]
    release.py verify [--repo DIR]

Exit codes
----------
0   ok
1   bad input / usage error / not a git repo
2   BLOCKED: working tree not clean, or unexpected files appeared
3   BLOCKED: changelog body is not English / not conventional-commit style
"""

from __future__ import annotations

import argparse
import os
import re
import subprocess
import sys

TAG_RE = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")
HEADER_RE = re.compile(r"^## ")
HISTORY_FILE = "HISTORY.md"
# Any CJK codepoint (CJK punctuation, kanji/hanzi ranges, fullwidth forms).
CJK_RE = re.compile(
    "[　-〿㐀-䶿一-鿿豈-﫿︰-﹏＀-￯]"
)
# Conventional commit: "- type(scope): description" (scope optional).
CONVENTIONAL_RE = re.compile(
    r"^- (feat|fix|refactor|perf|docs|test|build|ci|chore)"
    r"(\([a-z0-9_.,/+*-]+\))?: \S"
)
ALLOWED_TYPES = "feat|fix|refactor|perf|docs|test|build|ci|chore"


def detect_repo(explicit: str | None) -> str:
    """Resolve the repo root: --repo if given, else the git toplevel of cwd."""
    start = os.path.abspath(explicit) if explicit else os.getcwd()
    out = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"],
        cwd=start,
        capture_output=True,
        text=True,
    )
    if out.returncode != 0:
        where = explicit if explicit else "the current directory"
        raise SystemExit(f"not inside a git repository: {where}")
    return out.stdout.strip()


def remote_url(repo: str) -> str:
    """URL of `origin`, or '' when there is no origin."""
    out = subprocess.run(
        ["git", "remote", "get-url", "origin"],
        cwd=repo,
        capture_output=True,
        text=True,
    )
    return "" if out.returncode != 0 else out.stdout.strip()


def releases_url(repo: str, version: str) -> str:
    """Human-browsable URL for the tag on the hosting service, or '' if unknown."""
    url = remote_url(repo)
    if not url:
        return ""
    if url.startswith("git@") and ":" in url:
        host, path = url[4:].split(":", 1)
        url = f"https://{host}/{path}"
    url = url.rstrip("/")
    if url.endswith(".git"):
        url = url[: -len(".git")]
    return f"{url}/releases/tag/{version}"


def run(repo: str, *args: str, strip: bool = True) -> str:
    """Run git in `repo`.

    `strip=False` is required for porcelain output: the leading status space of
    the FIRST line is significant (` M path` vs `M  path`) and would be lost.
    """
    out = subprocess.run(
        ["git", *args],
        cwd=repo,
        capture_output=True,
        text=True,
        check=True,
    )
    return out.stdout.strip() if strip else out.stdout


def parse_version(v: str) -> tuple[int, int, int]:
    m = TAG_RE.match(v)
    if not m:
        raise SystemExit(f"invalid version {v!r}: expected vMAJOR.MINOR.PATCH, e.g. v0.2.33")
    return int(m.group(1)), int(m.group(2)), int(m.group(3))


def normalize_version(raw: str) -> str:
    """Expand a partial version into a full `vMAJOR.MINOR.PATCH` tag.

    0.2.33 -> v0.2.33    v0.2.33 -> v0.2.33
    0.2    -> v0.2.0     v0.2    -> v0.2.0
    1      -> v1.0.0     v1      -> v1.0.0
    """
    s = raw.strip()
    if s.startswith(("v", "V")):
        s = s[1:]
    if not s:
        raise SystemExit("empty version")
    parts = s.split(".")
    if len(parts) > 3:
        raise SystemExit(f"invalid version {raw!r}: too many dot-separated parts")
    nums: list[int] = []
    for p in parts:
        if not p.isdigit():
            raise SystemExit(
                f"invalid version {raw!r}: {p!r} is not a number; "
                "expected vMAJOR.MINOR.PATCH (minor and patch default to 0)"
            )
        nums.append(int(p))
    while len(nums) < 3:
        nums.append(0)
    return "v%d.%d.%d" % (nums[0], nums[1], nums[2])


def all_tags(repo: str) -> list[tuple[int, int, int]]:
    raw = run(repo, "tag", "--list", "v*").splitlines()
    vers = [parse_version(t.strip()) for t in raw if TAG_RE.match(t.strip())]
    return sorted(vers)


def resolve_version(repo: str, requested: str | None) -> tuple[str, str | None]:
    """Return (version, previous_version)."""
    vers = all_tags(repo)
    if not vers:
        raise SystemExit("no vX.Y.Z tag found in this repo; refuse to guess")
    prev_tuple = vers[-1]
    prev = "v%d.%d.%d" % prev_tuple
    if requested:
        # Expand partial input: "0.3" -> "v0.3.0", "0.2.33" -> "v0.2.33".
        requested = normalize_version(requested)
        if parse_version(requested) in vers:
            raise SystemExit(f"tag {requested} already exists; choose another version")
        return requested, prev
    nxt = "v%d.%d.%d" % (prev_tuple[0], prev_tuple[1], prev_tuple[2] + 1)
    if nxt in ["v%d.%d.%d" % v for v in vers]:
        raise SystemExit(f"computed next version {nxt} already exists; pass --version explicitly")
    return nxt, prev


def dirty_files(repo: str) -> list[str]:
    """Porcelain status lines (empty list == clean working tree)."""
    raw = run(repo, "status", "--porcelain", strip=False)
    return [s for s in raw.splitlines() if s.strip()]


def status_path(line: str) -> str:
    """Porcelain line -> path (status codes occupy the first 3 chars)."""
    return line[3:].strip().strip('"')


def lint_body(body: str) -> tuple[list[str], list[str]]:
    """Return (cjk_lines, non_conventional_lines)."""
    cjk, off = [], []
    for line in body.split("\n"):
        s = line.strip()
        if not s:
            continue
        if CJK_RE.search(line):
            cjk.append(line)
        if not CONVENTIONAL_RE.match(s):
            off.append(line)
    return cjk, off


def cmd_plan(args: argparse.Namespace) -> int:
    repo = args.repo

    dirty = dirty_files(repo)
    if dirty:
        print("BLOCKED: working tree is not clean -- commit or stash first, then re-run.")
        print(f"uncommitted changes: {len(dirty)}")
        for s in dirty[:40]:
            print(f"  {s}")
        if len(dirty) > 40:
            print(f"  ... {len(dirty) - 40} more")
        print()
        print("Deciding what to commit (and with which message) needs human judgment;")
        print("this skill will not commit on your behalf. Handle it, then re-run `plan`.")
        return 2

    version, prev = resolve_version(repo, args.version)

    print(f"repo:     {repo}")
    print(f"branch:   {run(repo, 'rev-parse', '--abbrev-ref', 'HEAD')}")
    print(f"origin:   {remote_url(repo) or '(none)'}")
    print(f"last tag: {prev}")
    print(f"next tag: {version}")
    if args.version and args.version.strip() != version:
        print(f"note:     {args.version.strip()} normalized to {version}")
    print()
    print("working tree: clean")
    print()

    commits = run(repo, "log", "--no-merges", "--format=%h %s", f"{prev}..HEAD").splitlines()
    print(f"commits since {prev}: {len(commits)}")
    for c in commits[:200]:
        print(f"  {c}")
    if len(commits) > 200:
        print(f"  ... {len(commits) - 200} more")
    print()

    branch = run(repo, "rev-parse", "--abbrev-ref", "HEAD")
    print("--- commands to run after confirmation ---")
    print(f"cd {repo}")
    print("git add -A")
    print(f'git commit -m "chore(release): {version}"')
    print(f"git tag {version}")
    print(f"git push origin {branch}")
    print(f"git push origin {version}")
    url = releases_url(repo, version)
    if url:
        print(f"# release page: {url}")
    return 0


def cmd_apply(args: argparse.Namespace) -> int:
    repo = args.repo
    version, _ = resolve_version(repo, args.version)
    if args.version.strip() != version:
        print(f"note: {args.version.strip()} normalized to {version}")

    if args.body_file:
        with open(args.body_file, encoding="utf-8") as fh:
            body = fh.read()
    else:
        body = sys.stdin.read()
    body = body.strip("\n")
    if not body.strip():
        raise SystemExit("refusing to write an empty changelog entry")

    # Changelog body must be English, conventional-commit style.
    cjk, off = lint_body(body)
    if cjk and not args.allow_non_english:
        print("BLOCKED: HISTORY.md entries must be written in English.", file=sys.stderr)
        for line in cjk[:20]:
            print(f"  non-English: {line}", file=sys.stderr)
        print("Rewrite them in English (or pass --allow-non-english).", file=sys.stderr)
        return 3
    if off and not args.allow_non_english:
        print("BLOCKED: every bullet must follow '- type(scope): description'.", file=sys.stderr)
        print(f"  allowed types: {ALLOWED_TYPES}", file=sys.stderr)
        for line in off[:20]:
            print(f"  off-format: {line}", file=sys.stderr)
        print("Rewrite them (or pass --allow-non-english).", file=sys.stderr)
        return 3

    path = os.path.join(repo, HISTORY_FILE)
    with open(path, encoding="utf-8") as fh:
        text = fh.read()

    if re.search(rf"^## {re.escape(version)}\s*$", text, re.M):
        raise SystemExit(f"{HISTORY_FILE} already has a section for {version}")

    lines = text.split("\n")
    insert_at = len(lines)
    for i, line in enumerate(lines):
        if HEADER_RE.match(line):
            insert_at = i
            break

    block = [f"## {version}", "", body, ""]
    new_lines = lines[:insert_at] + block + lines[insert_at:]
    out = "\n".join(new_lines)
    if not out.endswith("\n"):
        out += "\n"

    if args.dry_run:
        sys.stdout.write(out)
        return 0

    with open(path, "w", encoding="utf-8") as fh:
        fh.write(out)
    print(f"prepended {version} to {path}")

    # After this write the only expected dirty file is HISTORY.md itself.
    extra = [s for s in dirty_files(repo) if status_path(s) != HISTORY_FILE]
    if extra:
        print("BLOCKED: unexpected dirty files appeared -- do not commit yet.", file=sys.stderr)
        for s in extra[:20]:
            print(f"  {s}", file=sys.stderr)
        return 2
    print("working tree: only HISTORY.md modified -- safe to commit")
    return 0


def cmd_verify(args: argparse.Namespace) -> int:
    repo = args.repo
    script = os.path.join(repo, "history.sh")
    if not os.path.exists(script):
        raise SystemExit(f"missing {script}")
    out = subprocess.run(
        ["bash", script],
        cwd=repo,
        capture_output=True,
        text=True,
        check=True,
    )
    print("--- release body GitHub Actions will publish ---")
    print(out.stdout.strip())
    return 0


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument(
        "--repo",
        help="repo root; defaults to the git toplevel of the current directory",
    )
    sub = ap.add_subparsers(dest="cmd", required=True)

    p = sub.add_parser("plan", help="read-only release plan")
    p.add_argument("--version", help="explicit version; partial forms are expanded (0.3 -> v0.3.0)")
    p.set_defaults(func=cmd_plan)

    a = sub.add_parser("apply", help="prepend a version section to HISTORY.md")
    a.add_argument("--version", required=True, help="partial forms are expanded (0.3 -> v0.3.0)")
    a.add_argument("--body-file", help="file containing the changelog bullets")
    a.add_argument("--dry-run", action="store_true", help="print the resulting HISTORY.md instead of writing")
    a.add_argument(
        "--allow-non-english",
        action="store_true",
        help="skip the English / conventional-commit lint (do not use by default)",
    )
    a.set_defaults(func=cmd_apply)

    v = sub.add_parser("verify", help="show the release body CI will produce")
    v.set_defaults(func=cmd_verify)

    args = ap.parse_args()
    args.repo = detect_repo(args.repo)
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
