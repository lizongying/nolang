#!/usr/bin/env python3
"""
Extract a machine-readable "dump spec" from the Go L2 oracle.

Inputs (read-only):
  src/parser/ast.go        -- node structs, field names, field types, markers
  src/parser/dump/dump.go  -- the closed type switch that defines the output

Outputs:
  compiler/src/asp.no        -- the spec as Nolang data tables
  docs/design/nolang-ast-spec.md  -- the same spec, human readable
  /tmp/ast_spec.json              -- the same spec, machine readable

Why this exists: M2 must reproduce `no ast` byte-for-byte. Hand-transcribing 71
emission sequences is exactly the kind of transcription this project forbids
(see docs/design/nolang-selfhost.md). So the sequence is extracted mechanically
and then *proved* faithful by a Go interpreter test that walks the spec over the
real AST and compares against dump.Dump() on the whole corpus. That test
decouples "is the spec right?" from "is the Nolang implementation right?".
"""

import re
import os
import json

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
AST = os.path.join(ROOT, "src/parser/ast.go")
DUMP = os.path.join(ROOT, "src/parser/dump/dump.go")


# ─────────────────────────────────────────────────────────────────────────────
# Go source utilities
# ─────────────────────────────────────────────────────────────────────────────

def strip_go_comments(s: str, keep_strings: bool = True) -> str:
    """Blank out Go comment contents.  Optionally also blank string contents.

    Offsets and newlines are preserved so line numbers stay valid.  We must keep
    strings for dump.go (the format strings ARE the spec) but blank them for
    ast.go (a `{` inside a string would corrupt brace counting)."""
    out = []
    i, n = 0, len(s)
    in_line = in_blk = False
    q = None
    while i < n:
        c = s[i]
        if in_line:
            if c == "\n":
                in_line = False
                out.append(c)
            else:
                out.append(" ")
            i += 1
            continue
        if in_blk:
            if c == "*" and i + 1 < n and s[i + 1] == "/":
                in_blk = False
                out.append("  ")
                i += 2
                continue
            out.append("\n" if c == "\n" else " ")
            i += 1
            continue
        if q:
            if c == "\\" and i + 1 < n:
                out.append(c if keep_strings else " ")
                out.append(s[i + 1] if keep_strings else " ")
                i += 2
                continue
            if c == q:
                q = None
            out.append(c if keep_strings else ("\n" if c == "\n" else " "))
            i += 1
            continue
        if c == "/" and i + 1 < n and s[i + 1] == "/":
            in_line = True
            out.append("  ")
            i += 2
            continue
        if c == "/" and i + 1 < n and s[i + 1] == "*":
            in_blk = True
            out.append("  ")
            i += 2
            continue
        if c in "\"'`":
            q = c
            out.append(c if keep_strings else " ")
            i += 1
            continue
        out.append(c)
        i += 1
    return "".join(out)


def split_top(s: str, sep: str = ",") -> list:
    parts, depth, cur = [], 0, []
    for c in s:
        if c in "([{":
            depth += 1
        elif c in ")]}":
            depth -= 1
        if c == sep and depth == 0:
            parts.append("".join(cur))
            cur = []
            continue
        cur.append(c)
    if cur:
        parts.append("".join(cur))
    return parts


# ─────────────────────────────────────────────────────────────────────────────
# 1. ast.go -> structs, field types, Node markers
# ─────────────────────────────────────────────────────────────────────────────

def parse_ast():
    code = strip_go_comments(open(AST).read(), keep_strings=False)

    structs = {}
    for m in re.finditer(r"type\s+([A-Za-z][A-Za-z0-9_]*)\s+struct\s*\{", code):
        name = m.group(1)
        line = code[: m.start()].count("\n") + 1
        i = code.index("{", m.start())
        depth, j = 0, i
        while j < len(code):
            if code[j] == "{":
                depth += 1
            elif code[j] == "}":
                depth -= 1
                if depth == 0:
                    break
            j += 1
        fields = []
        for ln in code[i + 1 : j].split("\n"):
            ln = ln.strip()
            fm = re.match(r"^([A-Za-z][A-Za-z0-9_]*)\s+([^\s]+)", ln)
            if fm:
                fields.append((fm.group(1), fm.group(2)))
        structs[name] = {"fields": fields, "line": line}

    nodish = set()
    for m in re.finditer(r"func\s*\([^)]*?\*?([A-Za-z][A-Za-z0-9_]*)\)\s*(Pos|EndPos)\s*\(", code):
        nodish.add(m.group(1))

    markers = {}
    for m in re.finditer(
        r"func\s*\([^)]*?\*?([A-Za-z][A-Za-z0-9_]*)\)\s*(stmtNode|exprNode|typeNode)\s*\(", code
    ):
        markers[m.group(1)] = {"stmtNode": "Statement", "exprNode": "Expression",
                               "typeNode": "Type"}[m.group(2)]

    return structs, nodish, markers, {k: dict(v["fields"]) for k, v in structs.items()}


# ─────────────────────────────────────────────────────────────────────────────
# 2. a small structural parser for Go statement bodies
# ─────────────────────────────────────────────────────────────────────────────

def parse_block(lines, i):
    """Parse statements until the closer `}` of the current block.

    Returns (statements, index_of_the_closer).  The closer is NOT consumed, so
    the caller can inspect `} else {`."""
    stmts = []
    while i < len(lines):
        s = lines[i].strip()
        if s == "" or s.startswith("//"):
            i += 1
            continue
        if s.startswith("}"):
            return stmts, i
        if s.startswith("if ") and s.endswith("{"):
            cond = s[3:-1].strip()
            body, i = parse_block(lines, i + 1)
            else_body = []
            while i < len(lines) and lines[i].strip().startswith("}"):
                cl = lines[i].strip()
                i += 1
                if cl == "} else {":
                    else_body, i = parse_block(lines, i)
                    continue
                if cl == "}" or cl == "} else":
                    break
                if cl.startswith("} else if"):
                    inner = cl[len("} else "):]  # "if ... {"
                    sub, i = parse_block(lines, i)
                    else_body = [{"t": "if", "cond": inner[3:-1].strip(),
                                  "body": sub, "else": []}]
                    continue
                break
            stmts.append({"t": "if", "cond": cond, "body": body, "else": else_body})
            continue
        if s.startswith("for ") and s.endswith("{"):
            body, i = parse_block(lines, i + 1)
            while i < len(lines) and lines[i].strip().startswith("}"):
                i += 1
                break
            stmts.append({"t": "for", "hdr": s[4:-1].strip(), "body": body})
            continue
        # simple statement: accumulate until paren depth returns to 0
        parts, depth = [], 0
        while i < len(lines):
            cur = lines[i].strip()
            parts.append(cur)
            depth += cur.count("(") - cur.count(")")
            i += 1
            if depth <= 0:
                break
        stmts.append({"t": "s", "text": re.sub(r"\s+", " ", " ".join(parts))})
    return stmts, i


# ─────────────────────────────────────────────────────────────────────────────
# 3. classify the leaves
# ─────────────────────────────────────────────────────────────────────────────

SCALAR_FN_POLICY = {"kv": "always", "kvs": "nonempty", "kvb": "true",
                    "kvn": "nonzero", "kvc": "count"}


def resolve_type(path, ftype, owner):
    cur, t = owner, None
    for step in path:
        if cur not in ftype:
            return None
        t = ftype[cur].get(step)
        if t is None:
            return None
        cur = t.lstrip("*")
    return t


def go_type_renderer(t):
    if t is None:
        return "str"
    t = t.lstrip("*")
    if t == "string":
        return "str"
    if t == "bool":
        return "bool"
    if t in ("int", "int64", "int32"):
        return "int"
    if t == "float64":
        return "float"
    return "str"


def classify_scalar(call, ftype, owner, locals_):
    m = re.match(r'^(kv|kvs|kvb|kvn|kvc)\(\s*"([^"]*)"\s*,\s*(.*)\)\s*$', call, re.S)
    if not m:
        return None
    fn, label, expr = m.group(1), m.group(2), m.group(3).strip()
    policy = SCALAR_FN_POLICY[fn]

    lm = re.match(r"^len\(\s*n\.([A-Za-z][A-Za-z0-9_]*)\s*\)$", expr)
    if lm:
        return {"label": label, "policy": policy, "src": lm.group(1),
                "render": "int", "of": "len"}

    tm = re.match(r"^typeString\(\s*n\.([A-Za-z][A-Za-z0-9_]*)\s*\)$", expr)
    if tm:
        return {"label": label, "policy": policy, "src": tm.group(1),
                "render": "type", "of": "typeString"}

    nm = re.match(r"^d\.nodeString\(\s*n\.([A-Za-z][A-Za-z0-9_]*)\s*\)$", expr)
    if nm:
        return {"label": label, "policy": policy, "src": nm.group(1),
                "render": "nodeString", "of": "nodeString"}

    km = re.match(r"^labelKindName\(\s*n\.([A-Za-z][A-Za-z0-9_]*)\s*\)$", expr)
    if km:
        return {"label": label, "policy": policy, "src": km.group(1),
                "render": "labelKind", "of": "labelKind"}

    sm = re.match(r'^fmt\.Sprintf\(\s*"%(d|v)"\s*,\s*n\.([A-Za-z][A-Za-z0-9_]*)\s*\)$', expr)
    if sm:
        return {"label": label, "policy": policy, "src": sm.group(2),
                "render": "int" if sm.group(1) == "d" else "float", "of": "sprintf"}

    pm = re.match(r"^n\.([A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z][A-Za-z0-9_]*)*)$", expr)
    if pm:
        path = pm.group(1).split(".")
        t = resolve_type(path, ftype, owner)
        return {"label": label, "policy": policy, "src": ".".join(path),
                "render": go_type_renderer(t), "of": "field", "gotype": t}

    # a prelude local (valStr / name) -- resolved separately
    if re.match(r"^[A-Za-z][A-Za-z0-9_]*$", expr) and expr in locals_:
        lo = locals_[expr]
        # A prelude local is "nodeString(f), or '' when f is nil" -- NOT the same
        # as a bare nodeString(f), which renders nil as "<nil>".  Keeping
        # `of: local` preserves that difference.
        return {"label": label, "policy": policy, "src": lo["src"],
                "render": lo["render"], "of": "local", "guard": lo["guard"],
                "nil_default": lo["nil_default"]}

    return None


def parse_fprintf(stmt):
    m = re.match(r'fmt\.Fprintf\(\s*&d\.buf\s*,\s*(".*?"|`.*?`)\s*(?:,\s*(.*))?\)\s*$',
                 stmt, re.S)
    if not m:
        return None
    fmt = m.group(1)
    args = split_top(m.group(2)) if m.group(2) else []
    return fmt, [a.strip() for a in args]


def parse_fields_call(expr):
    """`fields(a, b, c)` -> ['a','b','c']; anything else -> None."""
    expr = (expr or "").strip()
    if not expr.startswith("fields(") or not expr.endswith(")"):
        return None
    return [a.strip() for a in split_top(expr[len("fields("):-1])]


EXTRA_FORMS = {
    "FreeComments: %d group(s)": "FREE-COMMENTS",
    "BetweenComments: %d group(s)": "BETWEEN-COMMENTS",
    "TrailingComments": "TRAILING-COMMENTS",
    "ClosingBraceComment": "CLOSING-BRACE-COMMENT",
    "Implements: %v": "IMPLEMENTS",
}


def header_scalars(expr, ftype, owner, locals_, op, problems):
    """Classify the `{...}` body of a header Fprintf into op['scalars'] /
    op['node_of'] / op['absent'].

    The body is one of:
      fields(kv(...), kvs(...), ...)     -- the common case
      kv("op", n.Operator)               -- a single scalar, no fields() wrapper
      n.Value                            -- a bare field (AnnotationStringValue)
      d.nodeString(n.X)                  -- an inline node summary
    """
    expr = (expr or "").strip()
    fc = parse_fields_call(expr)
    if fc is not None:
        for call in fc:
            sc = classify_scalar(call, ftype, owner, locals_)
            if sc is None:
                problems.append(f"{owner}: unclassified scalar {call}")
            else:
                op["scalars"].append(sc)
        return
    sc = classify_scalar(expr, ftype, owner, locals_)
    if sc is not None:
        sc["bare"] = True
        op["scalars"].append(sc)
        return
    m2 = re.match(r"^d\.nodeString\(\s*n\.([A-Za-z][A-Za-z0-9_]*)\s*\)$", expr)
    if m2:
        op["node_of"] = m2.group(1)
        return
    m3 = re.match(r"^n\.([A-Za-z][A-Za-z0-9_]*)$", expr)
    if m3:
        t = resolve_type([m3.group(1)], ftype, owner)
        op["scalars"].append({"label": "", "policy": "always", "bare": True,
                              "render": go_type_renderer(t),
                              "src": m3.group(1), "of": "bare"})
        return
    problems.append(f"{owner}: unclassified header body {expr}")


def classify_stmt(stmt, ftype, owner, locals_, sink, problems):
    """Turn one simple Go statement into ops appended to `sink`."""
    # d.comments(n, depth+1)
    if re.match(r"^d\.comments\(\s*n\s*,", stmt):
        sink.append({"op": "COMMENTS"})
        return
    # d.signature(n.FuncSignature, depth+1)
    if re.match(r"^d\.signature\(", stmt):
        sink.append({"op": "SIG"})
        return
    # d.node(n.X, depth+1)
    cm = re.match(r"^d\.node\(\s*n\.([A-Za-z][A-Za-z0-9_]*)\s*,\s*depth\+1\s*\)$", stmt)
    if cm:
        sink.append({"op": "CHILD", "field": cm.group(1), "guard": False})
        return
    # d.annotationEntry(n, depth)
    if re.match(r"^d\.annotationEntry\(\s*n\s*,", stmt):
        sink.append({"op": "SELFHELPER", "helper": "annotationEntry"})
        return
    # d.commentGroup(n.X, depth+2): already folded into the OP-CLOSING /
    # OP-TRAILING instruction that precedes it, so it contributes no op.
    if re.match(r"^d\.commentGroup\(\s*n\.[A-Za-z][A-Za-z0-9_]*\s*,", stmt):
        return
    fp = parse_fprintf(stmt)
    if fp:
        fmt, args = fp
        for needle, opname in EXTRA_FORMS.items():
            if fmt.startswith('"%s' + needle):
                sink.append({"op": opname})
                return
        hm = re.match(r'^"%s([A-Za-z][A-Za-z0-9_]*)(\{(.*)\})?\\n"$', fmt)
        if hm:
            name, inner = hm.group(1), hm.group(3)
            op = {"op": "HDR", "name": name, "braces": inner is not None,
                  "scalars": [], "node_of": None, "absent": "", "literal": None}
            if inner is not None and inner != "":
                if inner in ("%v", "%d"):
                    expr = args[1] if len(args) > 1 else ""
                    op["scalars"].append({
                        "label": "", "policy": "always", "bare": True,
                        "render": "bool" if inner == "%v" else "int",
                        "src": expr.replace("n.", ""), "of": "bare"})
                elif "%" in inner:
                    header_scalars(args[1] if len(args) > 1 else "", ftype, owner,
                                   locals_, op, problems)
                else:
                    # a fixed literal body, e.g. "%sExpressionStatement{nil}\n"
                    op["literal"] = inner
            sink.append(op)
            return
        problems.append(f"{owner}: unclassified Fprintf {fmt}")
        return
    problems.append(f"{owner}: unclassified statement {stmt[:70]}")


# ─────────────────────────────────────────────────────────────────────────────
# 4. interpret the statement tree into an ordered op list
# ─────────────────────────────────────────────────────────────────────────────

def find_prelude_locals(stmts, ftype, owner):
    """Detect the `v := ""` + `if n.F != nil { v = <expr> }` idiom."""
    out = {}
    for st in stmts:
        if st["t"] != "s":
            continue
        m = re.match(r'^([A-Za-z][A-Za-z0-9_]*)\s*:=\s*""$', st["text"])
        if m:
            out[m.group(1)] = {"src": None, "render": "str", "guard": None,
                               "nil_default": ""}
    for st in stmts:
        if st["t"] != "if":
            continue
        gm = re.match(r"^n\.([A-Za-z][A-Za-z0-9_]*)\s*!=\s*nil$", st["cond"])
        if not gm:
            continue
        for inner in st["body"]:
            if inner["t"] != "s":
                continue
            am = re.match(r"^([A-Za-z][A-Za-z0-9_]*)\s*=\s*(.*)$", inner["text"])
            if not am or am.group(1) not in out:
                continue
            name, expr = am.group(1), am.group(2).strip()
            nm = re.match(r"^d\.nodeString\(\s*n\.([A-Za-z][A-Za-z0-9_]*)\s*\)$", expr)
            pm = re.match(r"^n\.([A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z][A-Za-z0-9_]*)*)$", expr)
            if nm:
                out[name].update(src=nm.group(1), render="nodeString",
                                 guard=gm.group(1))
            elif pm:
                path = pm.group(1).split(".")
                t = resolve_type(path, ftype, owner)
                out[name].update(src=".".join(path),
                                 render=go_type_renderer(t), guard=gm.group(1))
    return {k: v for k, v in out.items() if v["src"]}


def classify_loop_body(field, body, problems, owner, sink):
    """Classify the body of `for _, x := range n.<field> { ... }`.

    Three shapes occur, and the nil handling differs between them:
      d.node(x, depth+1)                       -> KIDS, a nil element prints <nil>
      if x != nil { d.node(x, depth+1) }       -> KIDS, a nil element is SKIPPED
      d.helper(x, depth+1)                     -> HELPER (same distinction)
    """
    inner, skip_nil = body, False
    if len(inner) == 1 and inner[0]["t"] == "if":
        gm = re.match(r"^([A-Za-z][A-Za-z0-9_]*)\s*!=\s*nil$", inner[0]["cond"])
        if gm:
            inner, skip_nil = inner[0]["body"], True
    if len(inner) == 1 and inner[0]["t"] == "s":
        t = re.sub(r"\s+", "", inner[0]["text"])
        m = re.match(r"^d\.node\((.+),depth\+1\)$", t)
        if m:
            arg = m.group(1)
            # accepted shapes: x  |  &x  |  n.F[i]  |  &n.F[i]
            if re.match(r"^&?[A-Za-z][A-Za-z0-9_]*$", arg) \
               or re.match(r"^&?n\.[A-Za-z][A-Za-z0-9_]*\[[A-Za-z]+\]$", arg):
                # `d.node(&n.Pairs[i], ...)` means the element type has its Node
                # methods on the *pointer* receiver (MapLiteral.Pairs is a value
                # slice).  The interpreter must take the address too.
                sink.append({"op": "KIDS", "field": field, "skip_nil": skip_nil,
                             "ptr": arg.startswith("&")})
                return True
        hmm = re.match(r"^d\.([A-Za-z][A-Za-z0-9_]*)\(&?[A-Za-z][A-Za-z0-9_]*,depth\+1\)$", t)
        if hmm:
            sink.append({"op": "HELPER", "field": field, "helper": hmm.group(1),
                         "skip_nil": skip_nil})
            return True
    problems.append(f"{owner}: unclassified for body over {field}")
    return False


def interpret(stmts, ftype, owner, locals_, sink, problems, prelude_texts):
    for st in stmts:
        if st["t"] == "s":
            if st["text"] in prelude_texts:
                continue
            classify_stmt(st["text"], ftype, owner, locals_, sink, problems)
        elif st["t"] == "for":
            hm = re.match(r"^(?:_,\s*)?[A-Za-z][A-Za-z0-9_]*\s*:=\s*range\s+n\.([A-Za-z][A-Za-z0-9_]*)$",
                          st["hdr"])
            if not hm:
                problems.append(f"{owner}: unclassified for {st['hdr']}")
                continue
            classify_loop_body(hm.group(1), st["body"], problems, owner, sink)
        elif st["t"] == "if":
            cond, body, els = st["cond"], st["body"], st["else"]
            gm = re.match(r"^n\.([A-Za-z][A-Za-z0-9_]*)\s*!=\s*nil$", cond)
            if gm and len(body) == 1 and body[0]["t"] == "s":
                t = re.sub(r"\s+", "", body[0]["text"])
                m2 = re.match(r"^d\.node\(n\.([A-Za-z][A-Za-z0-9_]*),depth\+1\)$", t)
                if m2 and m2.group(1) == gm.group(1):
                    sink.append({"op": "CHILD", "field": gm.group(1), "guard": True})
                    continue
                if re.match(r"^d\.(commentGroup|[A-Za-z][A-Za-z0-9_]*)\(n\.[A-Za-z][A-Za-z0-9_]*,depth\+2\)$", t):
                    continue  # folded into the *_COMMENTS op
                if re.match(r"^[A-Za-z][A-Za-z0-9_]*=[A-Za-z][A-Za-z0-9_.]*$", t):
                    continue  # a prelude local assignment
                if body[0]["text"] in prelude_texts:
                    continue  # a prelude local assignment (with a call on the RHS)
            # d.opts.Annotations { for ... n.Entries { d.annotationEntry } }
            if cond == "d.opts.Annotations":
                sink.append({"op": "ANNOTS"})
                continue
            # conditional header (ReturnStatement / ExpressionStatement): the
            # `if` arm prints the header AND recurses into the child, so every
            # statement in the arm must be classified, not just the Fprintf.
            if body and body[0]["t"] == "s" and body[0]["text"].startswith("fmt.Fprintf"):
                sub = []
                for b in body:
                    if b["t"] == "s":
                        classify_stmt(b["text"], ftype, owner, locals_, sub, problems)
                # The whole arm runs only when the field is non-nil, so every
                # CHILD it emits is guarded.
                if gm:
                    for op in sub:
                        if op["op"] == "CHILD" and op["field"] == gm.group(1):
                            op["guard"] = True
                alt = []
                if els and els[0]["t"] == "s":
                    classify_stmt(els[0]["text"], ftype, owner, locals_, alt, problems)
                for op in sub:
                    if op["op"] == "HDR" and alt and alt[0]["op"] == "HDR":
                        if alt[0].get("literal") is not None:
                            op["absent"] = alt[0]["literal"]
                        elif alt[0]["scalars"] and alt[0]["scalars"][0].get("bare"):
                            op["absent"] = alt[0]["scalars"][0].get("src") or ""
                    sink.append(op)
                continue
            # `if len(n.X) > 0 {` / `if n.X != nil {` guarding an EXTRA or a comment group
            lm = re.match(r"^len\(\s*n\.([A-Za-z][A-Za-z0-9_]*)\s*\)\s*>\s*0$", cond)
            if lm or gm:
                inner_ops = []
                for b in body:
                    if b["t"] == "s":
                        classify_stmt(b["text"], ftype, owner, locals_, inner_ops, problems)
                if inner_ops:
                    sink.extend(inner_ops)
                continue
            problems.append(f"{owner}: unclassified if `{cond}`")
        else:
            problems.append(f"{owner}: unclassified node {st['t']}")


# ─────────────────────────────────────────────────────────────────────────────
# 5. drive
# ─────────────────────────────────────────────────────────────────────────────

def extract_node_switch(code):
    m = re.search(r"func \(d \*dumper\) node\(node parser\.Node, depth int\) \{", code)
    if not m:
        raise SystemExit("could not find (*dumper).node")
    start = m.end()
    nxt = re.search(r"\n\}\n", code[start:])
    end = start + nxt.start() + 1 if nxt else len(code)
    return code[start:end]


# ─────────────────────────────────────────────────────────────────────────────
# 6. Go emitter: the spec as data + a reflect-driven interpreter + a corpus test
#
# The point of the test is to separate two questions that are easy to conflate:
#   (a) is the *spec* faithful to dump.go?          <- proved here, in Go
#   (b) is the *Nolang* implementation faithful?    <- proved later, by L2 diff
# If (a) fails there is no point writing the Nolang side at all.
# ─────────────────────────────────────────────────────────────────────────────

GO_OPS = ["opHdr", "opScalar", "opComments", "opChild", "opKids", "opHelper",
          "opSig", "opSelfHelper", "opFree", "opBetween", "opTrailing",
          "opClosing", "opImplements", "opAnnots"]

GO_OPMAP = {"COMMENTS": "opComments", "SIG": "opSig",
            "FREE-COMMENTS": "opFree", "BETWEEN-COMMENTS": "opBetween",
            "TRAILING-COMMENTS": "opTrailing",
            "CLOSING-BRACE-COMMENT": "opClosing",
            "IMPLEMENTS": "opImplements", "ANNOTS": "opAnnots",
            "HDR": "opHdr", "CHILD": "opChild", "KIDS": "opKids",
            "SCALAR": "opScalar",
            "HELPER": "opHelper", "SELFHELPER": "opSelfHelper"}

GO_RENDER_NAMES = ["rStr", "rInt", "rFloat", "rBool", "rType", "rNodeString",
                   "rLabelKind"]
GO_RENDER = {n: i for i, n in enumerate(
    ["str", "int", "float", "bool", "type", "nodeString", "labelKind"])}

GO_POLICY = {"always": "pAlways", "nonempty": "pNonEmpty", "true": "pTrue",
             "nonzero": "pNonZero", "count": "pCount"}


GO_OF = ["ofField", "ofLen", "ofType", "ofNodeString", "ofLabelKind",
         "ofSprintf", "ofBare", "ofLocal"]

GO_OFMAP = {n: i for i, n in enumerate(
    ["field", "len", "typeString", "nodeString", "labelKind", "sprintf",
     "bare", "local"])}

GO_OFMAP = {n: i for i, n in enumerate(
    ["field", "len", "typeString", "nodeString", "labelKind", "sprintf",
     "bare", "local"])}


def gop(name):
    """Index of an opcode in the Go tape."""
    return GO_OPS.index(GO_OPMAP[name])


def go_q(s):
    return '"' + s.replace("\\", "\\\\").replace('"', '\\"') + '"'


def emit_go(spec, path):
    kinds = sorted(spec.keys())
    kidx = {k: i + 1 for i, k in enumerate(kinds)}

    strs = []

    def sid(s):
        s = s or ""
        if s not in strs:
            strs.append(s)
        return strs.index(s) + 1     # 0 == empty

    tape = []          # list of (op, a, b, c, d)
    kind_rows = []
    for k in kinds:
        first = len(tape)
        # A header scalar that renders rType/rNodeString reads a *child node*.
        # The CHILD instructions come after the HDR instruction, so the ordinal
        # table must be built in a pre-pass -- computing it inline would leave
        # every reference pointing at child 0.
        child_ordinal = {}
        for n, op in enumerate(o for o in spec[k] if o["op"] == "CHILD"):
            child_ordinal[op["field"]] = n
        nchild = len(child_ordinal)
        for op in spec[k]:
            o = op["op"]
            if o == "HDR":
                flags = (1 if op["braces"] else 0)
                tape.append((gop("HDR"), sid(op["name"]), flags,
                             sid(op.get("node_of") or ""), sid(op.get("absent") or "")))
                for sc in op["scalars"]:
                    render = sc.get("render", "str")
                    of = sc.get("of", "field")
                    if render in ("type", "nodeString") or of in ("len", "typeString",
                                                                  "nodeString"):
                        ref = child_ordinal.get(sc.get("src", "").split(".")[0], 0)
                    else:
                        ref = 0
                    tape.append((gop("SCALAR"),
                                 GO_POLICY[sc.get("policy", "always")],
                                 GO_RENDER.get(render, 0) | (GO_OFMAP[of] << 8),
                                 sid(sc.get("src") or ""),
                                 sid(sc.get("label") or "") | (ref << 24)))
                continue
            if o == "CHILD":
                tape.append((gop("CHILD"), sid(op["field"]),
                             1 if op.get("guard") else 0, 0, 0))
                continue
            if o == "KIDS":
                tape.append((gop("KIDS"), sid(op["field"]),
                             1 if op.get("skip_nil") else 0,
                             1 if op.get("ptr") else 0, 0))
                continue
            if o == "HELPER":
                tape.append((gop("HELPER"), sid(op["field"]),
                             1 if op.get("skip_nil") else 0, sid(op["helper"]), 0))
                continue
            if o == "SELFHELPER":
                tape.append((gop("SELFHELPER"), sid(op["helper"]), 0, 0, 0))
                continue
            tape.append((gop(o), 0, 0, 0, 0))
        kind_rows.append((k, first, len(tape) - first))

    # ── render the Go file ──────────────────────────────────────────────────
    L = []
    A = L.append
    A("// Code generated by scripts/gen_ast_spec.py. DO NOT EDIT.")
    A("//")
    A("// This file is a *proof harness*, not part of the compiler. It re-implements")
    A("// the dump walker as a pure interpreter over a data table extracted from")
    A("// dump.go, and asserts the two agree byte-for-byte on the whole test corpus.")
    A("//")
    A("// Why: docs/design/nolang-selfhost.md §5.3 requires `noc ast` to reproduce")
    A("// `no ast` exactly. Rather than hand-transcribing 71 emission sequences into")
    A("// Nolang, the sequence is extracted mechanically and validated here. Once this")
    A("// test is green, the same table can be emitted for the Nolang side with the")
    A("// knowledge that the table -- not the transcription -- is correct.")
    A("")
    A("package dump")
    A("")
    A("import (")
    A('\t"fmt"')
    A('\t"reflect"')
    A('\t"sort"')
    A('\t"strings"')
    A('\t"testing"')
    A("")
    A('\t"github.com/lizongying/nolang/parser"')
    A(")")
    A("")
    A("// ── renderers ───────────────────────────────────────────────────────────")
    A("")

    def const_block(names):
        # gofmt aligns the '=' of every name in a contiguous const group, so
        # the generator has to pad too -- otherwise every regeneration leaves
        # the file dirty and `gofmt -l` reports it.
        w = max(len(n) for n in names)
        A("const (")
        for i, n in enumerate(names):
            A(f"\t{n:<{w}} = {i}")
        A(")")

    const_block(GO_RENDER_NAMES)
    A("")
    const_block(["pAlways", "pNonEmpty", "pTrue", "pNonZero", "pCount"])
    A("")
    A("// ofKind says *where* a scalar's text comes from.  It is a separate axis")
    A("// from the renderer: ofLen reads a slice length, ofType reads a child node.")
    const_block(GO_OF)
    A("")
    const_block(GO_OPS)
    A("")
    A("// ── the spec ────────────────────────────────────────────────────────────")
    A("")
    A("var specStrs = []string{")
    A('\t"",')
    for s in strs:
        A("\t" + go_q(s) + ",")
    A("}")
    A("")
    A("// specInstr is one 5-slot instruction.  Slot meanings depend on Op; for")
    A("// OP-SCALAR, D carries the label id in its low 24 bits and the ordinal of the")
    A("// CHILD instruction it reads (for rType/rNodeString) in the high bits.")
    A("type specInstr struct{ Op, A, B, C, D int }")
    A("")
    A("var specTape = []specInstr{")
    for (op, a, b, c, d) in tape:
        A(f"\t{{{op}, {a}, {b}, {c}, {d}}},")
    A("}")
    A("")
    A("type specKind struct {")
    A("\tName       string")
    A("\tFirst, Num int")
    A("}")
    A("")
    A("var specKinds = []specKind{")
    A('\t{"", 0, 0}, // 0 is reserved: it means "not a known kind"')
    for (k, first, num) in kind_rows:
        A(f"\t{{{go_q(k)}, {first}, {num}}},")
    A("}")
    A("")
    A("var specKindIndex = func() map[string]int {")
    A("\tm := map[string]int{}")
    A("\tfor i, k := range specKinds {")
    A("\t\tif i > 0 {")
    A("\t\t\tm[k.Name] = i")
    A("\t\t}")
    A("\t}")
    A("\treturn m")
    A("}()")
    A("")
    A("func specStr(id int) string {")
    A("\tif id <= 0 || id >= len(specStrs) {")
    A("\t\treturn \"\"")
    A("\t}")
    A("\treturn specStrs[id]")
    A("}")
    A("")
    A("// ── the interpreter ─────────────────────────────────────────────────────")
    A("")
    A("type specState struct {")
    A("\tbuf strings.Builder")
    A("}")
    A("")
    A("func specDump(node parser.Node) string {")
    A("\ts := &specState{}")
    A("\ts.node(node, 0)")
    A("\treturn strings.TrimRight(s.buf.String(), \"\\n\")")
    A("}")
    A("")
    A("func specIndent(d int) string { return strings.Repeat(\"    \", d) }")
    A("")
    A("// fieldOf reads a (possibly dotted) field by name off a node.")
    A("func fieldOf(node parser.Node, path string) reflect.Value {")
    A("\tv := reflect.ValueOf(node)")
    A("\tfor _, step := range strings.Split(path, \".\") {")
    A("\t\tif !v.IsValid() {")
    A("\t\t\treturn v")
    A("\t\t}")
    A("\t\tif v.Kind() == reflect.Ptr {")
    A("\t\t\tif v.IsNil() {")
    A("\t\t\t\treturn reflect.Value{}")
    A("\t\t\t}")
    A("\t\t\tv = v.Elem()")
    A("\t\t}")
    A("\t\tif v.Kind() != reflect.Struct {")
    A("\t\t\treturn reflect.Value{}")
    A("\t\t}")
    A("\t\tv = v.FieldByName(step)")
    A("\t}")
    A("\treturn v")
    A("}")
    A("")
    A("func isNilVal(v reflect.Value) bool {")
    A("\tswitch v.Kind() {")
    A("\tcase reflect.Invalid:")
    A("\t\treturn true")
    A("\tcase reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map:")
    A("\t\treturn v.IsNil()")
    A("\t}")
    A("\treturn false")
    A("}")
    A("")
    A("// renderScalar produces the value text for one scalar field.")
    A("//")
    A("// `of` decides where the text comes from; `render` decides how a raw field")
    A("// value is formatted.  Keeping them separate matters: `kv(\"args\", len(n.Arguments))`")
    A("// and `kv(\"type\", typeString(n.Type))` are both \"always printed\", but one")
    A("// reads a slice length and the other reads a child node.")
    A("func (s *specState) renderScalar(node parser.Node, of, render int, src string,")
    A("\tchildVal parser.Node) string {")
    A("\tv := fieldOf(node, src)")
    A("\tswitch of {")
    A("\tcase ofLen:")
    A("\t\tif !v.IsValid() || v.Kind() != reflect.Slice {")
    A("\t\t\treturn \"0\"")
    A("\t\t}")
    A("\t\treturn fmt.Sprintf(\"%d\", v.Len())")
    A("\tcase ofType:")
    A("\t\tif childVal == nil {")
    A("\t\t\treturn \"\"")
    A("\t\t}")
    A("\t\tif t, ok := childVal.(parser.Type); ok {")
    A("\t\t\treturn t.String()")
    A("\t\t}")
    A("\t\treturn \"\"")
    A("\tcase ofNodeString:")
    A("\t\t// No nil guard here: (*dumper).nodeString has none, and")
    A("\t\t// fmt.Sprintf(\"%T\", nil) is \"<nil>\", so a nil child renders as \"<nil>\".")
    A("\t\t// (typeString is different -- it checks for nil and returns \".\")")
    A("\t\treturn specNodeString(childVal)")
    A("\tcase ofLabelKind:")
    A("\t\tif !v.IsValid() {")
    A("\t\t\treturn \"none\"")
    A("\t\t}")
    A("\t\tswitch parser.LabelKind(v.Int()) {")
    A("\t\tcase parser.LabelNumeric:")
    A("\t\t\treturn \"numeric\"")
    A("\t\tcase parser.LabelText:")
    A("\t\t\treturn \"text\"")
    A("\t\tdefault:")
    A("\t\t\treturn \"none\"")
    A("\t\t}")
    A("\tcase ofLocal:")
    A("\t\t// A prelude local is \"nodeString(f), or \\\"\\\" when f is nil\".  A bare")
    A("\t\t// nodeString(f) renders nil as \"<nil>\", so the two are not the same.")
    A("\t\tif render == rNodeString {")
    A("\t\t\tif childVal == nil {")
    A("\t\t\t\treturn \"\"")
    A("\t\t\t}")
    A("\t\t\treturn specNodeString(childVal)")
    A("\t\t}")
    A("\t\treturn renderValue(v, render)")
    A("\tcase ofBare, ofSprintf, ofField:")
    A("\t\treturn renderValue(v, render)")
    A("\t}")
    A("\treturn \"\"")
    A("}")
    A("")
    A("// renderValue formats a raw reflect value according to `render`.")
    A("func renderValue(v reflect.Value, render int) string {")
    A("\tif !v.IsValid() {")
    A("\t\treturn \"\"")
    A("\t}")
    A("\tif v.Kind() == reflect.Interface || v.Kind() == reflect.Ptr {")
    A("\t\tif v.IsNil() {")
    A("\t\t\treturn \"\"")
    A("\t\t}")
    A("\t\tv = v.Elem()")
    A("\t}")
    A("\tswitch render {")
    A("\tcase rBool:")
    A("\t\tif v.Kind() == reflect.Bool {")
    A("\t\t\treturn fmt.Sprintf(\"%v\", v.Bool())")
    A("\t\t}")
    A("\tcase rInt:")
    A("\t\tif v.Kind() >= reflect.Int && v.Kind() <= reflect.Int64 {")
    A("\t\t\treturn fmt.Sprintf(\"%d\", v.Int())")
    A("\t\t}")
    A("\tcase rFloat:")
    A("\t\tif v.Kind() == reflect.Float64 || v.Kind() == reflect.Float32 {")
    A("\t\t\treturn fmt.Sprintf(\"%v\", v.Float())")
    A("\t\t}")
    A("\t}")
    A("\tif v.Kind() == reflect.String {")
    A("\t\treturn v.String()")
    A("\t}")
    A("\treturn fmt.Sprintf(\"%v\", v.Interface())")
    A("}")
    A("")
    A("func specNodeString(node parser.Node) string {")
    A("\tswitch n := node.(type) {")
    A("\tcase *parser.Identifier:")
    A("\t\treturn n.Value")
    A("\tcase *parser.IntegerLiteral:")
    A("\t\treturn fmt.Sprintf(\"%d\", n.Value)")
    A("\tcase *parser.FloatLiteral:")
    A("\t\treturn fmt.Sprintf(\"%v\", n.Value)")
    A("\tcase *parser.StringLiteral:")
    A("\t\treturn n.Value")
    A("\tcase *parser.CharLiteral:")
    A("\t\treturn n.Value")
    A("\tcase *parser.BooleanLiteral:")
    A("\t\treturn fmt.Sprintf(\"%v\", n.Value)")
    A("\tcase *parser.NilLiteral:")
    A("\t\treturn \"nil\"")
    A("\tdefault:")
    A("\t\treturn strings.TrimPrefix(fmt.Sprintf(\"%T\", n), \"*parser.\")")
    A("\t}")
    A("}")
    A("")
    A("// specSignature mirrors (*dumper).signature.")
    A("func (s *specState) signature(sig parser.FuncSignature, depth int) {")
    A("\tif len(sig.GenericParams) > 0 {")
    A("\t\tnames := make([]string, 0, len(sig.GenericParams))")
    A("\t\tfor _, g := range sig.GenericParams {")
    A("\t\t\tif g != nil {")
    A("\t\t\t\tnames = append(names, g.Value)")
    A("\t\t\t}")
    A("\t\t}")
    A("\t\tfmt.Fprintf(&s.buf, \"%sGenericParams: %s\\n\", specIndent(depth), strings.Join(names, \", \"))")
    A("\t}")
    A("\tif sig.IsVariadic {")
    A("\t\tvar parts []string")
    A("\t\tif sig.VariadicUnion != \"\" {")
    A("\t\t\tparts = append(parts, \"union: \"+sig.VariadicUnion)")
    A("\t\t}")
    A("\t\tfmt.Fprintf(&s.buf, \"%sVariadic{%s}\\n\", specIndent(depth), strings.Join(parts, \", \"))")
    A("\t}")
    A("\tfor _, p := range sig.Parameters {")
    A("\t\tif p != nil {")
    A("\t\t\ts.node(p, depth)")
    A("\t\t}")
    A("\t}")
    A("\tfor _, p := range sig.Results {")
    A("\t\tif p != nil {")
    A("\t\t\ts.node(p, depth)")
    A("\t\t}")
    A("\t}")
    A("}")
    A("")
    A("func (s *specState) commentGroup(cg *parser.CommentGroup, depth int) {")
    A("\tif cg == nil {")
    A("\t\treturn")
    A("\t}")
    A("\tfor _, c := range cg.List {")
    A("\t\tfmt.Fprintf(&s.buf, \"%s//%s\\n\", specIndent(depth), c.Text)")
    A("\t}")
    A("}")
    A("")
    A("func (s *specState) comments(node parser.Node, depth int) {")
    A("\tif doc, ok := node.(interface{ GetDoc() *parser.CommentGroup }); ok {")
    A("\t\tif g := doc.GetDoc(); g != nil && len(g.List) > 0 {")
    A("\t\t\tfmt.Fprintf(&s.buf, \"%sDoc: //%s\\n\", specIndent(depth), g.List[0].Text)")
    A("\t\t}")
    A("\t}")
    A("\tif c, ok := node.(interface{ GetComment() *parser.CommentGroup }); ok {")
    A("\t\tif g := c.GetComment(); g != nil && len(g.List) > 0 {")
    A("\t\t\tfmt.Fprintf(&s.buf, \"%sComment: //%s\\n\", specIndent(depth), g.List[0].Text)")
    A("\t\t}")
    A("\t}")
    A("}")
    A("")
    A("// ── the main walk ───────────────────────────────────────────────────────")
    A("")
    A("func (s *specState) node(node parser.Node, depth int) {")
    A("\tif node == nil {")
    A("\t\tfmt.Fprintf(&s.buf, \"%s<nil>\\n\", specIndent(depth))")
    A("\t\treturn")
    A("\t}")
    A("\tki, ok := specKindIndex[reflect.TypeOf(node).Elem().Name()]")
    A("\tif !ok {")
    A("\t\tfmt.Fprintf(&s.buf, \"%s<unknown %T>\\n\", specIndent(depth), node)")
    A("\t\treturn")
    A("\t}")
    A("\tk := specKinds[ki]")
    A("\t// A scalar that renders rType/rNodeString reads a *child node*, so the")
    A("\t// child list must be built in CHILD-instruction order before the header")
    A("\t// line is printed.")
    A("\tvar childNodes []parser.Node")
    A("\tfor i := k.First; i < k.First+k.Num; i++ {")
    A("\t\tif specTape[i].Op == opChild {")
    A("\t\t\tchildNodes = append(childNodes, nodeFromValue(fieldOf(node, specStr(specTape[i].A))))")
    A("\t\t}")
    A("\t}")
    A("\tchildAt := func(ord int) parser.Node {")
    A("\t\tif ord >= 0 && ord < len(childNodes) {")
    A("\t\t\treturn childNodes[ord]")
    A("\t\t}")
    A("\t\treturn nil")
    A("\t}")
    A("")
    A("\tfor i := k.First; i < k.First+k.Num; i++ {")
    A("\t\tt := specTape[i]")
    A("\t\tswitch t.Op {")
    A("\t\tcase opHdr:")
    A("\t\t\tname := specStr(t.A)")
    A("\t\t\tnodeOf := specStr(t.C)")
    A("\t\t\tabsent := specStr(t.D)")
    A("\t\t\tvar parts []string")
    A("\t\t\tfor j := i + 1; j < k.First+k.Num && specTape[j].Op == opScalar; j++ {")
    A("\t\t\t\tsc := specTape[j]")
    A("\t\t\t\tlabel := specStr(sc.D & 0xFFFFFF)")
    A("\t\t\t\trend := sc.B & 0xFF")
    A("\t\t\t\tofk := sc.B >> 8")
    A("\t\t\t\tsrc := specStr(sc.C)")
    A("\t\t\t\tref := sc.D >> 24")
    A("\t\t\t\tval := s.renderScalar(node, ofk, rend, src, childAt(ref))")
    A("\t\t\t\tif !scalarWanted(sc.A, rend, val) {")
    A("\t\t\t\t\tcontinue")
    A("\t\t\t\t}")
    A("\t\t\t\tif label == \"\" {")
    A("\t\t\t\t\tparts = append(parts, val)")
    A("\t\t\t\t} else if rend == rBool && val == \"true\" && sc.A == pTrue {")
    A("\t\t\t\t\tparts = append(parts, label+\": true\")")
    A("\t\t\t\t} else {")
    A("\t\t\t\t\tparts = append(parts, label+\": \"+val)")
    A("\t\t\t\t}")
    A("\t\t\t}")
    A("\t\t\tbody := strings.Join(parts, \", \")")
    A("\t\t\tif nodeOf != \"\" {")
    A("\t\t\t\tcv := fieldOf(node, nodeOf)")
    A("\t\t\t\tif isNilVal(cv) {")
    A("\t\t\t\t\tbody = absent")
    A("\t\t\t\t} else {")
    A("\t\t\t\t\tbody = s.renderScalar(node, ofNodeString, rNodeString, nodeOf, nodeFromValue(cv))")
    A("\t\t\t\t}")
    A("\t\t\t}")
    A("\t\t\tif t.B&1 != 0 {")
    A("\t\t\t\tfmt.Fprintf(&s.buf, \"%s%s{%s}\\n\", specIndent(depth), name, body)")
    A("\t\t\t} else {")
    A("\t\t\t\tfmt.Fprintf(&s.buf, \"%s%s\\n\", specIndent(depth), name)")
    A("\t\t\t}")
    A("\t\tcase opComments:")
    A("\t\t\ts.comments(node, depth+1)")
    A("\t\tcase opSig:")
    A("\t\t\tv := fieldOf(node, \"FuncSignature\")")
    A("\t\t\tif v.IsValid() {")
    A("\t\t\t\ts.signature(v.Interface().(parser.FuncSignature), depth+1)")
    A("\t\t\t}")
    A("\t\tcase opChild:")
    A("\t\t\tv := fieldOf(node, specStr(t.A))")
    A("\t\t\tif isNilVal(v) {")
    A("\t\t\t\tif t.B == 0 {")
    A("\t\t\t\t\tfmt.Fprintf(&s.buf, \"%s<nil>\\n\", specIndent(depth+1))")
    A("\t\t\t\t}")
    A("\t\t\t} else {")
    A("\t\t\t\ts.node(nodeFromValue(v), depth+1)")
    A("\t\t\t}")
    A("\t\tcase opKids:")
    A("\t\t\tfor _, el := range nodesFromSlice(fieldOf(node, specStr(t.A)), t.C != 0) {")
    A("\t\t\t\tif el == nil && t.B != 0 {")
    A("\t\t\t\t\tcontinue")
    A("\t\t\t\t}")
    A("\t\t\t\ts.node(el, depth+1)")
    A("\t\t\t}")
    A("\t\tcase opHelper:")
    A("\t\t\tfor _, el := range valsFromSlice(fieldOf(node, specStr(t.A))) {")
    A("\t\t\t\tif el == nil && t.B != 0 {")
    A("\t\t\t\t\tcontinue")
    A("\t\t\t\t}")
    A("\t\t\t\ts.helper(specStr(t.C), el, depth+1)")
    A("\t\t\t}")
    A("\t\tcase opSelfHelper:")
    A("\t\t\ts.helper(specStr(t.A), node, depth)")
    A("\t\tcase opFree:")
    A("\t\t\tif g := commentGroupOf(node, \"FreeComments\"); g != nil {")
    A("\t\t\t\tfmt.Fprintf(&s.buf, \"%sFreeComments: %d group(s)\\n\", specIndent(depth+1), len(g.List))")
    A("\t\t\t}")
    A("\t\tcase opBetween:")
    A("\t\t\tif g := commentGroupOf(node, \"BetweenComments\"); g != nil {")
    A("\t\t\t\tfmt.Fprintf(&s.buf, \"%sBetweenComments: %d group(s)\\n\", specIndent(depth+1), len(g.List))")
    A("\t\t\t}")
    A("\t\tcase opTrailing:")
    A("\t\t\tif g := commentGroupOf(node, \"TrailingComments\"); g != nil {")
    A("\t\t\t\tfmt.Fprintf(&s.buf, \"%sTrailingComments\\n\", specIndent(depth+1))")
    A("\t\t\t\ts.commentGroup(g, depth+2)")
    A("\t\t\t}")
    A("\t\tcase opClosing:")
    A("\t\t\tif g := commentGroupOf(node, \"ClosingBraceComment\"); g != nil {")
    A("\t\t\t\tfmt.Fprintf(&s.buf, \"%sClosingBraceComment\\n\", specIndent(depth+1))")
    A("\t\t\t\ts.commentGroup(g, depth+2)")
    A("\t\t\t}")
    A("\t\tcase opImplements:")
    A("\t\t\tif v := fieldOf(node, \"Implements\"); v.IsValid() && v.Len() > 0 {")
    A("\t\t\t\tfmt.Fprintf(&s.buf, \"%sImplements: %v\\n\", specIndent(depth+1), v.Interface())")
    A("\t\t\t}")
    A("\t\tcase opAnnots:")
    A("\t\t\t// AnnotationStatement entries are dumped by the caller (which holds")
    A("\t\t\t// Options); the spec only records that the block exists.")
    A("\t\t}")
    A("\t}")
    A("}")
    A("")
    A("func scalarWanted(policy, rend int, val string) bool {")
    A("\tswitch policy {")
    A("\tcase pNonEmpty:")
    A("\t\treturn val != \"\"")
    A("\tcase pTrue:")
    A("\t\treturn val == \"true\"")
    A("\tcase pNonZero:")
    A("\t\treturn val != \"0\"")
    A("\tcase pCount:")
    A("\t\treturn val != \"0\"")
    A("\t}")
    A("\treturn true")
    A("}")
    A("")
    A("func nodeFromValue(v reflect.Value) parser.Node {")
    A("\tif isNilVal(v) {")
    A("\t\treturn nil")
    A("\t}")
    A("\tif n, ok := v.Interface().(parser.Node); ok {")
    A("\t\treturn n")
    A("\t}")
    A("\treturn nil")
    A("}")
    A("")
    A("// nodesFromSlice converts a slice field to []parser.Node.")
    A("//")
    A("// addr must be set when dump.go wrote `d.node(&n.F[i], ...)`: the element")
    A("// type then has its Node methods on the pointer receiver (MapLiteral.Pairs")
    A("// is a []MapPair value slice), so the element itself does not satisfy Node.")
    A("func nodesFromSlice(v reflect.Value, addr bool) []parser.Node {")
    A("\tif !v.IsValid() || v.Kind() != reflect.Slice {")
    A("\t\treturn nil")
    A("\t}")
    A("\tout := make([]parser.Node, 0, v.Len())")
    A("\tfor i := 0; i < v.Len(); i++ {")
    A("\t\tel := v.Index(i)")
    A("\t\tif addr && el.CanAddr() {")
    A("\t\t\tel = el.Addr()")
    A("\t\t}")
    A("\t\tif isNilVal(el) {")
    A("\t\t\tout = append(out, nil)")
    A("\t\t\tcontinue")
    A("\t\t}")
    A("\t\tif n, ok := el.Interface().(parser.Node); ok {")
    A("\t\t\tout = append(out, n)")
    A("\t\t} else {")
    A("\t\t\tout = append(out, nil)")
    A("\t\t}")
    A("\t}")
    A("\treturn out")
    A("}")
    A("")
    A("// valsFromSlice returns a slice field's elements as interface values, so")
    A("// non-Node helper structs (InterfaceMethod, TaggedEnumField,")
    A("// AnnotationStructField) can be dispatched too.")
    A("func valsFromSlice(v reflect.Value) []any {")
    A("\tif !v.IsValid() || v.Kind() != reflect.Slice {")
    A("\t\treturn nil")
    A("\t}")
    A("\tout := make([]any, 0, v.Len())")
    A("\tfor i := 0; i < v.Len(); i++ {")
    A("\t\tel := v.Index(i)")
    A("\t\tif isNilVal(el) {")
    A("\t\t\tout = append(out, nil)")
    A("\t\t\tcontinue")
    A("\t\t}")
    A("\t\tout = append(out, el.Interface())")
    A("\t}")
    A("\treturn out")
    A("}")
    A("")
    A("func commentGroupOf(node parser.Node, field string) *parser.CommentGroup {")
    A("\tv := fieldOf(node, field)")
    A("\tif isNilVal(v) {")
    A("\t\treturn nil")
    A("\t}")
    A("\tif g, ok := v.Interface().(*parser.CommentGroup); ok {")
    A("\t\treturn g")
    A("\t}")
    A("\treturn nil")
    A("}")
    A("")
    A("// helper dispatches the three non-Node sub-structures.")
    A("func (s *specState) helper(name string, v any, depth int) {")
    A("\tswitch name {")
    A("\tcase \"interfaceMethod\":")
    A("\t\tm, _ := v.(*parser.InterfaceMethod)")
    A("\t\tif m == nil {")
    A("\t\t\tfmt.Fprintf(&s.buf, \"%s<nil>\\n\", specIndent(depth))")
    A("\t\t\treturn")
    A("\t\t}")
    A("\t\tvar parts []string")
    A("\t\tparts = append(parts, \"name: \"+m.Name)")
    A("\t\tif m.Receiver != \"\" {")
    A("\t\t\tparts = append(parts, \"receiver: \"+m.Receiver)")
    A("\t\t}")
    A("\t\tif m.IsVariadic {")
    A("\t\t\tparts = append(parts, \"variadic: true\")")
    A("\t\t}")
    A("\t\tif m.IsGenericReceiver {")
    A("\t\t\tparts = append(parts, \"genericReceiver: true\")")
    A("\t\t}")
    A("\t\tfmt.Fprintf(&s.buf, \"%sInterfaceMethod{%s}\\n\", specIndent(depth), strings.Join(parts, \", \"))")
    A("\t\tfor _, p := range m.Parameters {")
    A("\t\t\tif p != nil {")
    A("\t\t\t\ts.node(p, depth+1)")
    A("\t\t\t}")
    A("\t\t}")
    A("\t\tfor _, p := range m.Results {")
    A("\t\t\tif p != nil {")
    A("\t\t\t\ts.node(p, depth+1)")
    A("\t\t\t}")
    A("\t\t}")
    A("\tcase \"taggedEnumField\":")
    A("\t\tf, _ := v.(*parser.TaggedEnumField)")
    A("\t\tif f == nil {")
    A("\t\t\tfmt.Fprintf(&s.buf, \"%s<nil>\\n\", specIndent(depth))")
    A("\t\t\treturn")
    A("\t\t}")
    A("\t\tvar parts []string")
    A("\t\tparts = append(parts, \"name: \"+f.Name)")
    A("\t\tif f.Type != nil {")
    A("\t\t\tparts = append(parts, \"type: \"+f.Type.String())")
    A("\t\t}")
    A("\t\tfmt.Fprintf(&s.buf, \"%sTaggedEnumField{%s}\\n\", specIndent(depth), strings.Join(parts, \", \"))")
    A("\t\tif f.Type != nil {")
    A("\t\t\ts.node(f.Type, depth+1)")
    A("\t\t}")
    A("\tcase \"annotationStructField\":")
    A("\t\tf, _ := v.(*parser.AnnotationStructField)")
    A("\t\tif f == nil {")
    A("\t\t\tfmt.Fprintf(&s.buf, \"%s<nil>\\n\", specIndent(depth))")
    A("\t\t\treturn")
    A("\t\t}")
    A("\t\tfmt.Fprintf(&s.buf, \"%sAnnotationStructField{name: %s}\\n\", specIndent(depth), f.Name)")
    A("\t\tif f.Value != nil {")
    A("\t\t\ts.node(f.Value, depth+1)")
    A("\t\t}")
    A("\tcase \"annotationEntry\":")
    A("\t\te, _ := v.(*parser.AnnotationEntry)")
    A("\t\tif e == nil {")
    A("\t\t\tfmt.Fprintf(&s.buf, \"%s<nil>\\n\", specIndent(depth))")
    A("\t\t\treturn")
    A("\t\t}")
    A("\t\tvar parts []string")
    A("\t\tparts = append(parts, \"key: \"+e.Key)")
    A("\t\tif e.Trailing {")
    A("\t\t\tparts = append(parts, \"trailing: true\")")
    A("\t\t}")
    A("\t\tif e.Propagated {")
    A("\t\t\tparts = append(parts, \"propagated: true\")")
    A("\t\t}")
    A("\t\tfmt.Fprintf(&s.buf, \"%sAnnotationEntry{%s}\\n\", specIndent(depth), strings.Join(parts, \", \"))")
    A("\t\tif e.Value != nil {")
    A("\t\t\ts.node(e.Value, depth+1)")
    A("\t\t}")
    A("\tdefault:")
    A("\t\tpanic(\"spec: unknown helper \" + name)")
    A("\t}")
    A("}")
    A("")
    A("// ── the test ────────────────────────────────────────────────────────────")
    A("")
    A("// TestSpecMatchesDump is the gate that makes the generated spec trustworthy.")
    A("// It walks the whole test corpus twice -- once with the real dumper, once with")
    A("// the spec interpreter -- and requires byte-identical output.  A failure here")
    A("// means the *table* is wrong, and there is no point porting it to Nolang.")
    A("func TestSpecMatchesDump(t *testing.T) {")
    A("\tprograms := parseCorpus(t)")
    A("\tif len(programs) < 100 {")
    A("\t\tt.Fatalf(\"corpus too small (%d parseable files); the test would be vacuous\", len(programs))")
    A("\t}")
    A("\tpaths := make([]string, 0, len(programs))")
    A("\tfor p := range programs {")
    A("\t\tpaths = append(paths, p)")
    A("\t}")
    A("\tsort.Strings(paths)")
    A("\tbad := 0")
    A("\tfor _, path := range paths {")
    A("\t\tprog := programs[path]")
    A("\t\twant := Dump(prog)")
    A("\t\tgot := specDump(prog)")
    A("\t\tif want != got {")
    A("\t\t\tbad++")
    A("\t\t\tif bad <= 3 {")
    A("\t\t\t\tt.Errorf(\"%s: spec disagrees with dump\\n%s\", path, firstDiff(want, got))")
    A("\t\t\t}")
    A("\t\t}")
    A("\t}")
    A("\tif bad > 0 {")
    A("\t\tt.Fatalf(\"spec disagrees with dump on %d/%d files\", bad, len(paths))")
    A("\t}")
    A("\tt.Logf(\"spec/dump agreement: %d files, byte-identical\", len(paths))")
    A("}")
    A("")
    A("func firstDiff(want, got string) string {")
    A("\twl := strings.Split(want, \"\\n\")")
    A("\tgl := strings.Split(got, \"\\n\")")
    A("\tfor i := 0; i < len(wl) && i < len(gl); i++ {")
    A("\t\tif wl[i] != gl[i] {")
    A("\t\t\tlo := i - 2")
    A("\t\t\tif lo < 0 {")
    A("\t\t\t\tlo = 0")
    A("\t\t\t}")
    A("\t\t\tvar b strings.Builder")
    A("\t\t\tfor j := lo; j < i+3 && j < len(wl); j++ {")
    A("\t\t\t\tfmt.Fprintf(&b, \"  line %d want: %q\\n\", j+1, wl[j])")
    A("\t\t\t}")
    A("\t\t\tfor j := lo; j < i+3 && j < len(gl); j++ {")
    A("\t\t\t\tfmt.Fprintf(&b, \"  line %d got : %q\\n\", j+1, gl[j])")
    A("\t\t\t}")
    A("\t\t\treturn b.String()")
    A("\t\t}")
    A("\t}")
    A("\treturn fmt.Sprintf(\"  lengths differ: want %d lines, got %d lines\", len(wl), len(gl))")
    A("}")
    A("")
    A("// TestSpecKindCount pins the size of the table.  If dump.go gains or loses a")
    A("// case, this fails and forces a regenerate; it also catches a generator that")
    A("// silently drops kinds.")
    A("func TestSpecKindCount(t *testing.T) {")
    A(f"\tif got := len(specKinds) - 1; got != {len(kind_rows)} {{")
    A(f"\t\tt.Fatalf(\"spec has %d kinds, want {len(kind_rows)}\", got)")
    A("\t}")
    A("}")

    open(path, "w").write("\n".join(L) + "\n")
    print(f"wrote {os.path.relpath(path, ROOT)}  ({len(L)} lines, "
          f"{len(tape)} instrs, {len(strs)} strings)")


# ─────────────────────────────────────────────────────────────────────────────
# 7. Nolang emitter: the same tables, for compiler/src/asp.no
# ─────────────────────────────────────────────────────────────────────────────

NO_OPS = ["OP-HDR", "OP-SCALAR", "OP-COMMENTS", "OP-CHILD", "OP-KIDS", "OP-HELPER",
          "OP-SIG", "OP-SELF-HELPER", "OP-FREE", "OP-BETWEEN", "OP-TRAILING",
          "OP-CLOSING", "OP-IMPLEMENTS", "OP-ANNOTS"]
NO_OPS_KEYS = ["HDR", "SCALAR", "COMMENTS", "CHILD", "KIDS", "HELPER", "SIG",
               "SELFHELPER", "FREE-COMMENTS", "BETWEEN-COMMENTS",
               "TRAILING-COMMENTS", "CLOSING-BRACE-COMMENT", "IMPLEMENTS",
               "ANNOTS"]
NO_RENDER = ["R-STR", "R-INT", "R-FLOAT", "R-BOOL", "R-TYPE", "R-NODE-STRING",
             "R-LABEL-KIND"]
NO_RENDER_KEYS = ["str", "int", "float", "bool", "type", "nodeString", "labelKind"]
NO_POLICY = ["P-ALWAYS", "P-NON-EMPTY", "P-TRUE", "P-NON-ZERO", "P-COUNT"]
NO_POLICY_KEYS = ["always", "nonempty", "true", "nonzero", "count"]
NO_OF = ["OF-FIELD", "OF-LEN", "OF-TYPE", "OF-NODE-STRING", "OF-LABEL-KIND",
         "OF-SPRINTF", "OF-BARE", "OF-LOCAL"]
NO_OF_KEYS = ["field", "len", "typeString", "nodeString", "labelKind", "sprintf",
              "bare", "local"]


def no_q(s):
    """A Nolang str literal: single quotes, no backslashes, no newlines."""
    out = []
    for ch in s:
        if ch == "'":
            out.append("''")
        elif ch == "\\":
            out.append("\\\\")
        elif ch == "\n":
            out.append("\\n")
        elif ch == "\t":
            out.append("\\t")
        elif ch == "\r":
            out.append("\\r")
        else:
            out.append(ch)
    return "'" + "".join(out) + "'"


def emit_nolang(spec, path):
    kinds = sorted(spec.keys())
    strs = []

    def sid(s):
        s = s or ""
        if s not in strs:
            strs.append(s)
        return strs.index(s) + 1

    # ── the Nolang slot model ───────────────────────────────────────────────
    #
    # The Go interpreter can look a field up *by name* (reflection); Nolang
    # cannot.  So the Nolang tape replaces every field reference with an
    # ordinal into a fixed-size per-node slot bank, and the rule is:
    #
    #   * CHILD-derivable  -- OF-TYPE, OF-NODE-STRING, and OF-LOCAL with
    #     render nodeString -- carry a **CHILD ordinal** in `ref` and occupy no
    #     slot at all.  (Verified: every such scalar's `src` root is a CHILD
    #     field of its kind, so the ordinal is always resolvable.)
    #
    #   * everything else -- field / bare / local(render!=nodeString) / sprintf
    #     / len / labelKind -- occupies exactly one **string** slot, and `ref`
    #     is that slot's ordinal.  The parser pre-renders the value and stores
    #     the pool id, so the interpreter needs no per-render formatting.
    #
    # Why uniform string slots rather than str+int banks: `render` includes
    # `float`, and Go's `%v` on a float64 cannot be reconstructed from an i64.
    # Pre-rendering also makes `wanted(policy, val)` -- which is defined on the
    # rendered text -- the single decision point.
    #
    # ⚠️ Do NOT "unify" this with emit_go's encoding.  They are deliberately
    # different: emit_go's slot C is a *string id of a field path*; this one is
    # an ordinal.  emit_go is pinned by TestSpecMatchesDump over 528 files.
    CHILD_REF = ("typeString", "nodeString")

    tape = []
    kind_rows = []
    max_s = max_c = 0
    for k in kinds:
        first = len(tape)
        child_ordinal = {}
        for n, op in enumerate(o for o in spec[k] if o["op"] == "CHILD"):
            child_ordinal[op["field"]] = n
        max_c = max(max_c, sum(1 for o in spec[k] if o["op"] == "KIDS"))
        ns = 0
        for op in spec[k]:
            o = op["op"]
            if o == "HDR":
                nof = op.get("node_of") or ""
                tape.append((NO_OPS_KEYS.index("HDR"), sid(op["name"]),
                             1 if op["braces"] else 0,
                             (child_ordinal.get(nof, -1) + 1) if nof else 0,
                             sid(op.get("absent") or "")))
                for sc in op["scalars"]:
                    render = sc.get("render", "str")
                    of = sc.get("of", "field")
                    root = sc.get("src", "").split(".")[0]
                    if of in CHILD_REF or (of == "local" and render == "nodeString"):
                        ref = child_ordinal.get(root, 0)
                    else:
                        ref = ns
                        ns += 1
                    tape.append((NO_OPS_KEYS.index("SCALAR"),
                                 NO_POLICY_KEYS.index(sc.get("policy", "always")),
                                 NO_RENDER_KEYS.index(render),
                                 NO_OF_KEYS.index(of),
                                 sid(sc.get("label") or "") | (ref << 24)))
                continue
            if o == "CHILD":
                tape.append((NO_OPS_KEYS.index("CHILD"), sid(op["field"]),
                             1 if op.get("guard") else 0, 0, 0))
                continue
            if o == "KIDS":
                tape.append((NO_OPS_KEYS.index("KIDS"), sid(op["field"]),
                             1 if op.get("skip_nil") else 0,
                             1 if op.get("ptr") else 0, 0))
                continue
            if o == "HELPER":
                tape.append((NO_OPS_KEYS.index("HELPER"), sid(op["field"]),
                             1 if op.get("skip_nil") else 0, sid(op["helper"]), 0))
                continue
            if o == "SELFHELPER":
                tape.append((NO_OPS_KEYS.index("SELFHELPER"), sid(op["helper"]),
                             0, 0, 0))
                continue
            tape.append((NO_OPS_KEYS.index(o), 0, 0, 0, 0))
        max_s = max(max_s, ns)
        kind_rows.append((k, sid(k), first, len(tape) - first))

    L = []
    A = L.append
    A("; ═══════════════════════════════════════════════════════════════════")
    A("; asp — `no ast` dump 的規格表（module short name: asp）（**自動生成，不要手改**）")
    A("; ═══════════════════════════════════════════════════════════════════")
    A(";")
    A("; ⚠️ 模組短名 asp（<=3 字元，見設計文檔 §4.4）：跨模組寫 asp.tape()。\n;\n; 權威來源：src/parser/dump/dump.go 的 (*dumper).node 型別 switch。")
    A("; 生成器：scripts/gen_ast_spec.py")
    A(";")
    A("; ⚠️ 為什麼是「資料表 + 解譯器」而不是 71 個手寫函式：")
    A(";    71 段輸出序列手抄進 nolang，轉錄風險與 tok.no 的 92 個常量同級，")
    A(";    而錯法（少印一個欄位、把子節點順序寫反）肉眼極難發現。")
    A(";    改為機械抽取後，同一份表先在 Go 上重放並與 dump.go 對拍：")
    A(";    src/parser/dump/spec_gen_test.go 走全語料 528 檔，逐位元組一致。")
    A(";    ⇒ 這張表本身已被證明忠實，nolang 端只需要忠實「解譯」它。")
    A(";")
    A("; 表格式：")
    A(f";   tape  每 5 個 i64 一條指令：{{op, a, b, c, d}}")
    A(";   kinds 每 4 個 i64 一種節點：(name-id, first-instr, instr-count, reserved)")
    A(";   strs  字串池，id 從 1 起；0 = 空字串")
    A(";")
    A("; ⚠️ SCALAR 指令的 d 欄位：低 24 位是標籤字串 id，高位是 ref。")
    A(";    ref 的意義隨 of 而變：")
    A(";      OF-TYPE / OF-NODE-STRING / OF-LOCAL(render=nodeString)")
    A(";          -> CHILD 序號（該純量的值來自某個子節點）")
    A(";      其餘（OF-FIELD / OF-BARE / OF-LEN / OF-SPRINTF / OF-LABEL-KIND")
    A(";            / OF-LOCAL(render!=nodeString)）")
    A(";          -> 節點的字串槽序號 s0..sN（parser 已預先渲染好）")
    A(";")
    A("; ⚠️ 這一套編碼與 Go 端的 spec_gen_test.go **刻意不同**（Go 用反射按")
    A(";    欄位名取值，故那裡的 slot C 是「欄位路徑的字串 id」）。不要試圖統一。")
    A(";")
    A("; SHS 合規：無結構體、無一等函式值、無 map 欄位。")
    A("; ⚠️ 定義順序：本檔的函式彼此不呼叫，故順序無關；但呼叫方 ast.no")
    A(";    一律向後引用，故本檔必須先於 ast.no 被導入。")
    A("")
    A("; ── 指令 opcode ──")
    for i, n in enumerate(NO_OPS):
        A(f"{n} = {i}")
    A("")
    A("; ── 純量出處（of）──")
    for i, n in enumerate(NO_OF):
        A(f"{n} = {i}")
    A("")
    A("; ── 純量渲染器（render）──")
    for i, n in enumerate(NO_RENDER):
        A(f"{n} = {i}")
    A("")
    A("; ── 純量印出政策（policy）──")
    for i, n in enumerate(NO_POLICY):
        A(f"{n} = {i}")
    A("")
    A("; ── 槽位上限（由生成器實測，供 ast.no 宣告 arena 用）──")
    A(f"MAX-STR-SLOTS = {max_s}")
    A(f"MAX-COUNT-SLOTS = {max_c}")
    A(f"KIND-COUNT = {len(kind_rows)}")
    A(f"STR-COUNT = {len(strs) + 1}")
    A(f"INSTR-COUNT = {len(tape)}")
    A("")
    A("; strs: 字串池（id 0 = 空字串，不在此列）")
    A("strs = () (r []str) {")
    A(f"    out []str = with-cap({len(strs) + 1})")
    A("    out.push('')")
    for st in strs:
        A(f"    out.push({no_q(st)})")
    A("    r = out")
    A("}")
    A("")
    A("; tape: 扁平指令帶，每 5 個 i64 一條")
    A("tape = () (r []i64) {")
    A(f"    out []i64 = with-cap({len(tape) * 5})")
    for (op, a, b, c, d) in tape:
        A(f"    out.push({op})")
        A(f"    out.push({a})")
        A(f"    out.push({b})")
        A(f"    out.push({c})")
        A(f"    out.push({d})")
    A("    r = out")
    A("}")
    A("")
    A("; kinds: 每 4 個 i64 一種節點 (name-id, first, num, reserved)")
    A("kinds = () (r []i64) {")
    A(f"    out []i64 = with-cap({(len(kind_rows) + 1) * 4})")
    A("    out.push(0)")
    A("    out.push(0)")
    A("    out.push(0)")
    A("    out.push(0)")
    for (k, sid_, first, num) in kind_rows:
        A(f"    out.push({sid_})")
        A(f"    out.push({first})")
        A(f"    out.push({num})")
        A("    out.push(0)")
    A("    r = out")
    A("}")
    A("")
    A("; ── 節點種類 id ──")
    A("; 命名照 tok.no 的風格：大寫 + 底線，跨模組寫 asp.K_IDENTIFIER。")
    for i, (k, sid_, first, num) in enumerate(kind_rows):
        snake = re.sub(r"(?<!^)(?=[A-Z])", "_", k).upper()
        A(f"K_{snake} = {i + 1}")
    A("")
    A("; kind-name: 節點種類 id → 型別名（供 <unknown> 與除錯用）")
    A("kind-name = (id i64) (r str) {")
    A("    r = 'UNKNOWN-KIND'")
    for i, (k, sid_, first, num) in enumerate(kind_rows):
        A(f"    id == {i + 1} -> r = {no_q(k)}")
    A("}")
    A("")
    A("; kind-id: 型別名 → 節點種類 id（找不到回 0）")
    A("kind-id = (name str) (r i64) {")
    A("    r = 0")
    for i, (k, sid_, first, num) in enumerate(kind_rows):
        A(f"    name == {no_q(k)} -> r = {i + 1}")
    A("}")

    open(path, "w").write("\n".join(L) + "\n")
    print(f"wrote {os.path.relpath(path, ROOT)}  ({len(L)} lines, "
          f"slots: str<={max_s} cnt<={max_c})")


def main():
    structs, nodish, markers, ftype = parse_ast()
    code = strip_go_comments(open(DUMP).read(), keep_strings=True)
    switch = extract_node_switch(code)

    blocks = []
    for m in re.finditer(r"case\s+\*parser\.([A-Za-z][A-Za-z0-9_]*)\s*:", switch):
        kind = m.group(1)
        start = m.end()
        nxt = re.search(r"\n\tcase\s+\*parser\.|\n\tdefault\s*:", switch[start:])
        end = start + nxt.start() if nxt else len(switch)
        blocks.append((kind, switch[start:end]))

    spec, problems = {}, []
    for kind, body in blocks:
        lines = body.split("\n")
        stmts, _ = parse_block(lines, 0)
        locals_ = find_prelude_locals(stmts, ftype, kind)
        prelude_texts = set()
        for st in stmts:
            if st["t"] == "s":
                m = re.match(r'^([A-Za-z][A-Za-z0-9_]*)\s*:=\s*""$', st["text"])
                if m and m.group(1) in locals_:
                    prelude_texts.add(st["text"])
            elif st["t"] == "if":
                gm = re.match(r"^n\.([A-Za-z][A-Za-z0-9_]*)\s*!=\s*nil$", st["cond"])
                if gm:
                    for inner in st["body"]:
                        if inner["t"] == "s":
                            am = re.match(r"^([A-Za-z][A-Za-z0-9_]*)\s*=\s*(.*)$", inner["text"])
                            if am and am.group(1) in locals_:
                                prelude_texts.add(inner["text"])
        ops = []
        interpret(stmts, ftype, kind, locals_, ops, problems, prelude_texts)
        spec[kind] = ops

    print(f"ast.go structs        : {len(structs)}")
    print(f"Node impls            : {len(nodish)}")
    print(f"dump.go node cases    : {len(blocks)}")
    print(f"problems              : {len(problems)}")
    for p in problems:
        print(f"   ! {p}")
    print()

    json.dump(spec, open("/tmp/ast_spec.json", "w"), indent=1, sort_keys=True)
    print("wrote /tmp/ast_spec.json")

    emit_go(spec, os.path.join(ROOT, "src/parser/dump/spec_gen_test.go"))
    emit_nolang(spec, os.path.join(ROOT, "compiler/src/asp.no"))


if __name__ == "__main__":
    main()
