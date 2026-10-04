#!/usr/bin/env python3
import os
import re
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), '..'))
STD = os.path.join(ROOT, 'src', 'std')
DOCS = os.path.join(ROOT, 'docs')
AGENTS = os.path.join(ROOT, '.agents', 'skills')

# top-level LetStatement at column 0: NAME [TYPE...] = VALUE
DECL_RE = re.compile(r'^([A-Za-z][A-Za-z0-9_-]*)\s*([^=]*?)=(.*)$')
ASSIGN_RE = re.compile(r'^\s+([A-Za-z][A-Za-z0-9_-]*)\s*=[^=]')
FIELD_ASSIGN_RE = re.compile(
    r'^\s+([A-Za-z][A-Za-z0-9_-]*)\.[A-Za-z][A-Za-z0-9_-]*\s*=[^=]')
# a newtype/type declaration: NAME = SomeTypeName  (single token, starts uppercase or is a known scalar type)
TYPE_WORDS = {'i8', 'i16', 'i32', 'i64', 'u8', 'u16', 'u32',
              'u64', 'f32', 'f64', 'str', 'bool', 'byte', 'char'}


def is_func(value_rest: str) -> bool:
    v = value_rest.strip()
    # function literal: starts with '(' (params) OR is a block/lambda
    return v.startswith('(')


def name_is_upper_const(name: str) -> bool:
    if not name:
        return False
    if not ('A' <= name[0] <= 'Z'):
        return False
    return not any('a' <= c <= 'z' for c in name)


def is_newtype(value_rest: str) -> bool:
    v = value_rest.strip()
    # `fd = i64` or `name = SomeType` (single identifier, no operators/call)
    if re.fullmatch(r'[A-Za-z][A-Za-z0-9_.-]*', v):
        return v in TYPE_WORDS or (v[:1].isupper())
    return False


def scan_lines(lines, base=0):
    globals_decl = {}
    for i, ln in enumerate(lines, 1):
        if not ln or ln[0] in ' \t':
            continue
        if ln.lstrip().startswith(('#', ';', '//')):
            continue
        m = DECL_RE.match(ln)
        if not m:
            continue
        name, _typ, val = m.group(1), m.group(2), m.group(3)
        if is_func(val) or is_newtype(val):
            continue
        globals_decl[name] = i + base
    if not globals_decl:
        return []
    reassigned = set()
    for ln in lines:
        if ln and ln[0] not in ' \t':
            continue
        m = ASSIGN_RE.match(ln)
        if m:
            reassigned.add(m.group(1))
        m2 = FIELD_ASSIGN_RE.match(ln)
        if m2:
            reassigned.add(m2.group(1))
    out = []
    for name, ln_no in globals_decl.items():
        if name not in reassigned:
            continue
        if name_is_upper_const(name):
            continue
        out.append((name, ln_no))
    return out


def scan(path):
    with open(path, encoding='utf-8') as f:
        return scan_lines(f.read().split('\n'))


def scan_md(path):
    # extract ```no ... ``` fenced blocks, keep relative line numbers
    with open(path, encoding='utf-8') as f:
        lines = f.read().split('\n')
    blocks = []
    in_block = False
    cur = []
    for idx, ln in enumerate(lines):
        s = ln.strip()
        if s.startswith('```') and not in_block:
            lang = s[3:].strip()
            in_block = (lang in ('no', 'nolang', ''))
            cur = []
            continue
        if s.startswith('```') and in_block:
            blocks.append(list(cur))
            in_block = False
            continue
        if in_block:
            cur.append(ln)
    out = []
    for b in blocks:
        out.extend(scan_lines(b))
    return out


def main():
    # ---- std: list ALL lowercase top-level variable-like globals (mandatory rule) ----
    if '--all' in sys.argv:
        rows = []
        for dirpath, _, files in os.walk(STD):
            for fn in files:
                if not fn.endswith('.no'):
                    continue
                p = os.path.join(dirpath, fn)
                with open(p, encoding='utf-8') as f:
                    lines = f.read().split('\n')
                for i, ln in enumerate(lines, 1):
                    if not ln or ln[0] in ' \t' or ln.lstrip().startswith(('#', ';', '//')):
                        continue
                    m = DECL_RE.match(ln)
                    if not m:
                        continue
                    name, val = m.group(1), m.group(3)
                    if is_func(val) or is_newtype(val):
                        continue
                    if name_is_upper_const(name):
                        continue
                    rows.append((os.path.relpath(p, ROOT), i, name))
        for rel, ln, name in sorted(rows):
            print(f"{rel}:{ln}: lowercase top-level global '{name}'")
        print(f"\nTOTAL (all lowercase top-level globals in std): {len(rows)}")
        return

    findings = {}
    # std .no
    for dirpath, _, files in os.walk(STD):
        for fn in files:
            if fn.endswith('.no'):
                p = os.path.join(dirpath, fn)
                r = scan(p)
                if r:
                    findings[os.path.relpath(p, ROOT)] = r
    # docs + agents .md
    for root in (DOCS, AGENTS):
        for dirpath, dnames, files in os.walk(root):
            dnames[:] = [d for d in dnames if d not in (
                'node_modules', '.docusaurus', 'build')]
            for fn in files:
                if fn.endswith('.md'):
                    p = os.path.join(dirpath, fn)
                    r = scan_md(p)
                    if r:
                        findings[os.path.relpath(p, ROOT)] = r
    if not findings:
        print("No lowercase mutable module-level globals found.")
        return
    total = 0
    for rel in sorted(findings):
        for name, ln in findings[rel]:
            print(f"{rel}:{ln}: lowercase mutable global '{name}'")
            total += 1
    print(
        f"\nTOTAL: {total} lowercase mutable globals across {len(findings)} files")


if __name__ == '__main__':
    main()
