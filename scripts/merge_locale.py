#!/usr/bin/env python3
"""Merge a translation dict into a .noml locale catalog.

The catalog is a generated-key / hand-written-value file: keys come from
`make i18n`, values come from a translator. This script only ever touches
values, re-renders the file sorted by key, and refuses to run if a key is not in
en.noml (that would be an orphan translation of a message that no longer exists).

Values containing a newline are written as a backtick raw block, matching
`package.NomlQuoteValue` (the Go renderer), so a hand-edited catalog and a
generated one look the same:

    'w308jsag': `
    %s: validation failed:
    %s
    `

A value that cannot be represented that way (a line that is itself the
terminating backtick) falls back to the single-quoted form with \\n escapes.
"""
import re
import sys

ENTRY = re.compile(r"^\s*'([0-9a-z]{8})':\s*'(.*)'\s*$")
RAW_START = re.compile(r"^\s*'([0-9a-z]{8})':\s*`\s*$")
RAW_END = re.compile(r"^\s*`\s*$")


def unescape(v):
    out = []
    i = 0
    while i < len(v):
        c = v[i]
        if c == "\\" and i + 1 < len(v):
            n = v[i + 1]
            out.append({"n": "\n", "t": "\t", "r": "\r", "0": "\0",
                        "\\": "\\", "'": "'", "/": "/"}.get(n, n))
            i += 2
            continue
        out.append(c)
        i += 1
    return "".join(out)


def escape(v):
    out = []
    for c in v:
        if c == "\\":
            out.append("\\\\")
        elif c == "'":
            out.append("\\'")
        elif c == "\n":
            out.append("\\n")
        elif c == "\t":
            out.append("\\t")
        elif c == "\r":
            out.append("\\r")
        else:
            out.append(c)
    return "".join(out)


def raw_safe(v):
    return all(line.lstrip(" \t\r") != "`" for line in v.split("\n"))


def render_value(v):
    if "\n" in v and raw_safe(v):
        return "`\n" + v + "\n`"
    return "'" + escape(v) + "'"


def parse(path):
    header, entries = [], {}
    lines = open(path, encoding="utf-8").read().split("\n")
    i = 0
    while i < len(lines):
        line = lines[i]
        m = ENTRY.match(line)
        if m:
            entries[m.group(1)] = unescape(m.group(2))
            i += 1
            continue
        m = RAW_START.match(line)
        if m:
            body = []
            i += 1
            while i < len(lines) and not RAW_END.match(lines[i]):
                body.append(lines[i])
                i += 1
            entries[m.group(1)] = "\n".join(body)
            i += 1  # skip the closing backtick line
            continue
        if not entries:
            header.append(line)
        i += 1
    return header, entries


def render(path, header, entries):
    lines = list(header)
    for k in sorted(entries):
        lines.append("  '%s': %s" % (k, render_value(entries[k])))
    lines.append("}")
    with open(path, "w", encoding="utf-8") as f:
        f.write("\n".join(lines) + "\n")


def main():
    locale, trans_path = sys.argv[1], sys.argv[2]
    _, en = parse("i18n/catalog/locales/en.noml")
    path = "i18n/catalog/locales/%s.noml" % locale
    header, cur = parse(path)

    ns = {}
    with open(trans_path, encoding="utf-8") as f:
        exec(compile(f.read(), trans_path, "exec"), ns)
    add = ns["TRANSLATIONS"]

    bad = [k for k in add if k not in en]
    if bad:
        raise SystemExit("orphan keys (not in en.noml): %s" % bad)

    cur.update(add)
    render(path, header, cur)
    print("%s: %d entries (+%d new)" % (locale, len(cur), len(add)))


if __name__ == "__main__":
    main()
