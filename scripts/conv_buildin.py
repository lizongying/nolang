import re
import sys

# Transform `; build-in (...)` marker lines into `#{buildin=<nolangname>}` real lines.
# nolangname is extracted from the following commented signature line `; NAME = (...)`
# when it is a clean identifier (letters/digits/hyphen only); otherwise we fall back to
# a sanitized version of the marker key (strip quotes, `->`, spaces, take last `.`-component
# and convert `_` to `-`).

BUILDIN_RE = re.compile(r'^\s*;\s*build-in\s*\((.*?)\)\s*$')
SIG_RE = re.compile(r'^\s*;\s*([A-Za-z_][\w\-]*)\s*=\s*\(')
# a "clean" nolang ident: letters/digits/hyphen, no dots/brackets
CLEAN_RE = re.compile(r'^[A-Za-z_][\w\-]*$')


def sanitize_key(inner):
    # inner e.g. "ForwardFunc: nolang.now_ms" or "CLibCall: sprintf \"%hhu\"" or
    #           "LLVMIntrinsic: llvm.ceil.f64" or "arr-zero -> @llvm.memset.p0i8.i64" or "sitofp"
    # remove everything from a quote
    s = inner.split('"')[0]
    # remove everything from an arrow
    s = s.split('->')[0]
    s = s.split('→')[0]
    # take part after last ':'
    if ':' in s:
        s = s.rsplit(':', 1)[1]
    s = s.strip()
    # if contains '.', take last component
    if '.' in s:
        s = s.rsplit('.', 1)[1]
    # convert underscores to hyphens
    s = s.replace('_', '-')
    s = s.strip()
    return s


def transform(path, dry_run=True):
    with open(path, 'r', encoding='utf-8') as f:
        lines = f.readlines()
    out = []
    changed = []
    i = 0
    n = len(lines)
    while i < n:
        line = lines[i]
        m = BUILDIN_RE.match(line)
        if not m:
            out.append(line)
            i += 1
            continue
        inner = m.group(1).strip()
        # find next signature name
        nolang = None
        for j in range(i + 1, n):
            nl = lines[j]
            if nl.strip() == '':
                continue
            if not nl.lstrip().startswith(';'):
                # reached real code, stop
                break
            # Stop at the next built-in marker — it begins a new block.
            if BUILDIN_RE.match(nl):
                break
            sm = SIG_RE.match(nl)
            if sm:
                cand = sm.group(1)
                if CLEAN_RE.match(cand):
                    nolang = cand
                break
            # else keep scanning comment lines
        if nolang is None:
            nolang = sanitize_key(inner)
        if nolang == '':
            nolang = sanitize_key(inner)
        new_line = f"#{{buildin={nolang}}}\n"
        if dry_run:
            changed.append((i + 1, line.rstrip('\n'), new_line.rstrip('\n'), nolang))
        out.append(new_line)
        i += 1
    if not dry_run:
        with open(path, 'w', encoding='utf-8') as f:
            f.writelines(out)
    return changed


if __name__ == '__main__':
    files = sys.argv[1:]
    apply = False
    if files and files[0] == '--apply':
        apply = True
        files = files[1:]
    for fp in files:
        ch = transform(fp, dry_run=not apply)
        if ch:
            print(f"=== {fp} ({len(ch)} changes) ===")
            for ln, old, new, name in ch:
                print(f"  L{ln}: {old!r}  ->  {new!r}   [name={name}]")
