#!/usr/bin/env python3
"""Add real signature bodies to std builtin stubs.

For every `#{buildin=NAME...}` annotation, find the following commented
signature line (`; NAME2 = (params) (results) { ...`) in the same block and
replace it (and any trailing `; }` line) with a real `NAME = (params) (results) { }`
line. The function name comes from the annotation value; the parameter/result
types are extracted from the commented signature so the body is a syntactically
valid nolang declaration. The compiler strips these (they carry #{buildin=...}),
so only the documentation/declaration form matters.

Usage: add_builtin_bodies.py [--apply] FILE [FILE ...]
"""
import re
import sys

ANNOT_RE = re.compile(r'^(\s*)#\{buildin=([^}]*)\}\s*$')
SIG_COMMENT_RE = re.compile(r'^\s*;\s*.*=\s*\(')          # comment line containing `= (`
STANDALONE_CLOSE_RE = re.compile(r'^\s*;\s*\}\s*$')        # lone `; }` comment line
GROUP_RE = re.compile(r'\([^()]*\)')


def convert(text):
    lines = text.split('\n')
    out = []
    n = len(lines)
    i = 0
    pending = None            # annotation value (builtin name)
    pending_out_idx = -1      # index in `out` where the annotation was appended
    pending_body_added = False
    changes = []
    while i < n:
        line = lines[i]
        m = ANNOT_RE.match(line)
        if m:
            # flush any previous pending that never got a body
            if pending is not None and not pending_body_added:
                fallback = f"{pending} = () {{ }}"
                out.insert(pending_out_idx + 1, fallback)
                changes.append((i + 1, '<no signature comment>', fallback, pending))
            raw = m.group(2)
            name = raw.split(',')[0].strip()  # buildin=NAME, possibly with extra keys
            pending = name
            pending_body_added = False
            out.append(line)
            pending_out_idx = len(out) - 1
            i += 1
            continue

        if pending is not None:
            # blank line ends the current builtin block without a body
            if line.strip() == '':
                if not pending_body_added:
                    fallback = f"{pending} = () {{ }}"
                    out.insert(pending_out_idx + 1, fallback)
                    changes.append((i + 1, '<no signature comment>', fallback, pending))
                pending = None
                pending_body_added = False
                out.append(line)
                i += 1
                continue
            if SIG_COMMENT_RE.match(line):
                rest = line.split('//')[0]
                groups = GROUP_RE.findall(rest)
                if groups:
                    params = groups[0]
                    results = groups[1] if len(groups) > 1 else ''
                    sig = f"{pending} = {params}"
                    if results:
                        sig += f" {results}"
                    sig += " { }"
                    out.append(sig)
                    changes.append((i + 1, line, sig, pending))
                    pending_body_added = True
                    # consume a trailing `; }` comment line
                    if i + 1 < n and STANDALONE_CLOSE_RE.match(lines[i + 1]):
                        i += 2
                    else:
                        i += 1
                    pending = None
                    continue
                # has `= (` but no parenthesized group: keep as-is, drop pending
                pending = None
                pending_body_added = False
                out.append(line)
                i += 1
                continue
            # ordinary comment / other line: keep, stay pending
            out.append(line)
            i += 1
            continue

        out.append(line)
        i += 1

    # flush trailing pending at EOF
    if pending is not None and not pending_body_added:
        fallback = f"{pending} = () {{ }}"
        out.insert(pending_out_idx + 1, fallback)
        changes.append((n + 1, '<no signature comment>', fallback, pending))

    return '\n'.join(out), changes


def main():
    args = sys.argv[1:]
    apply = False
    if args and args[0] == '--apply':
        apply = True
        args = args[1:]
    if not args:
        print("usage: add_builtin_bodies.py [--apply] FILE ...", file=sys.stderr)
        sys.exit(1)
    for fp in args:
        with open(fp, encoding='utf-8') as f:
            text = f.read()
        new_text, changes = convert(text)
        if not changes:
            print(f"{fp}: no changes")
            continue
        print(f"=== {fp} ({len(changes)} bodies added) ===")
        for ln, old, new, name in changes:
            print(f"  L{ln}: {old.strip()!r} -> {new!r}   [name={name}]")
        if apply:
            with open(fp, 'w', encoding='utf-8') as f:
                f.write(new_text)


if __name__ == '__main__':
    main()
