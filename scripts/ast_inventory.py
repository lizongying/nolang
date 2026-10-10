#!/usr/bin/env python3
"""Emit the complete Nolang AST node/grammar/error-message catalogue, derived
mechanically from the Go source.  No hand transcription.

  src/parser/ast.go        node structs, fields, interface markers
  src/parser/*.go          construction sites, error sites, parse functions
  src/parser/dump/dump.go  the L2 oracle's closed type switch
"""
import os
import re
import sys
from collections import OrderedDict, defaultdict

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
PAR = os.path.join(ROOT, "src/parser")
DUMP = os.path.join(PAR, "dump/dump.go")
OUT = os.path.join(ROOT, "docs/design/nolang-ast-grammar.md")


def read(p):
    with open(p, encoding="utf-8") as f:
        return f.read()


def strip_code(l):
    """Drop comments + literals (for brace counting / token scanning)."""
    l = re.sub(r"`[^`]*`", "", l)
    l = re.sub(r'"(?:[^"\\]|\\.)*"', '""', l)
    l = re.sub(r"'(?:[^'\\]|\\.)*'", "''", l)
    return re.sub(r"//.*$", "", l)


def strip_comment(l):
    """Drop only line comments (keeps literals readable)."""
    return re.sub(r"//.*$", "", l)


def unquote(s):
    """Best-effort Go string literal -> text (keeps the source spelling)."""
    return s


# ── 1. structs from ast.go ───────────────────────────────────────────────────
ast_lines = read(os.path.join(PAR, "ast.go")).split("\n")
ast_code = [strip_code(l) for l in ast_lines]

structs = OrderedDict()
i = 0
while i < len(ast_lines):
    m = re.match(r"^type\s+(\w+)\s+struct\s*\{", ast_lines[i])
    if not m:
        i += 1
        continue
    name = m.group(1)
    j, depth = i, 1
    while j < len(ast_lines):
        if j > i:
            depth += ast_code[j].count("{") - ast_code[j].count("}")
            if depth <= 0:
                break
        j += 1
    fields = []
    for k in range(i + 1, j):
        t = strip_code(ast_lines[k]).strip()
        t = re.sub(r'`[^`]*`', "", t).strip()
        if not t or t.startswith("//"):
            continue
        mm = re.match(r"^([A-Za-z_][\w,\s]*?)\s+(\S.*)$", t)
        if mm:
            ftype = mm.group(2).strip()
            for nm in [x.strip() for x in mm.group(1).split(",")]:
                fields.append((nm, ftype))
    structs[name] = {"line": i + 1, "fields": fields, "kind": "-"}
    i = j + 1

# ── 2. interface kind ────────────────────────────────────────────────────────
marker, posend = {}, defaultdict(set)
for fn in sorted(os.listdir(PAR)):
    if not fn.endswith(".go") or fn.endswith("_test.go"):
        continue
    src = read(os.path.join(PAR, fn))
    for m in re.finditer(
        r"func\s+\([^)]*?\*?(\w+)\)\s+(statementNode|expressionNode|typeNode)\(\)", src
    ):
        marker[m.group(1)] = {"statementNode": "Statement",
                              "expressionNode": "Expression",
                              "typeNode": "Type"}[m.group(2)]
    for m in re.finditer(r"func\s+\([^)]*?\*?(\w+)\)\s+(Pos|EndPos)\(\)", src):
        posend[m.group(1)].add(m.group(2))

for name in structs:
    if name in marker:
        structs[name]["kind"] = marker[name]
    elif {"Pos", "EndPos"} <= posend.get(name, set()):
        structs[name]["kind"] = "Node"

NODE_NAMES = set(structs)

# ── 3. dump coverage ─────────────────────────────────────────────────────────
dl = read(DUMP).split("\n")
dstart = next(i for i, l in enumerate(dl) if l.startswith("func (d *dumper) node("))
dend = next(i for i, l in enumerate(dl) if i > dstart and l.startswith("func (d *dumper) signature("))
cases = OrderedDict()
cur = None
for l in dl[dstart:dend]:
    m = re.match(r"^\tcase \*parser\.(\w+):", l)
    if m:
        cur = m.group(1)
        cases[cur] = []
        continue
    if re.match(r"^\tdefault:", l):
        cur = None
        continue
    if cur is not None:
        cases[cur].append(l)

nsstart = next(i for i, l in enumerate(dl) if l.startswith("func (d *dumper) nodeString("))
node_string_cases = [m.group(1) for m in
                     (re.match(r"^\tcase \*parser\.(\w+):", l) for l in dl[nsstart:nsstart + 40]) if m]

# ── 4. builders + error sites, with real func-body tracking ──────────────────
builders = defaultdict(set)
errors = []            # (func, file, line, channel, code, message)


def find_string_arg(text, start):
    """From `start` (index just after '('), return the argument source text."""
    depth, k = 1, start
    while k < len(text) and depth > 0:
        if text[k] == "(":
            depth += 1
        elif text[k] == ")":
            depth -= 1
        k += 1
    return text[start:k - 1]


def first_literal(arg):
    """Extract the first Go string literal from an expression."""
    m = re.search(r'"((?:[^"\\]|\\.)*)"', arg)
    return m.group(1) if m else None


for fn in sorted(os.listdir(PAR)):
    if not fn.endswith(".go") or fn.endswith("_test.go"):
        continue
    raw = read(os.path.join(PAR, fn))
    lines = raw.split("\n")
    depth, curfunc, funcline, pending = 0, None, 0, None
    for idx, l in enumerate(lines):
        code = strip_code(l)
        if depth == 0:
            fm = re.match(r"^func\s+(?:\([^)]*\)\s*)?(\w+)\s*\(", l)
            if fm:
                pending = fm.group(1)
            o, c = code.count("{"), code.count("}")
            if pending and o > 0:
                curfunc, funcline = pending, idx + 1
                pending = None
                depth += o - c
                if depth <= 0:
                    curfunc, depth = None, 0
            continue
        # inside a func body
        if curfunc:
            for m in re.finditer(r"&(\w+)\{", code):
                if m.group(1) in NODE_NAMES:
                    builders[m.group(1)].add((curfunc, fn))
        o, c = code.count("{"), code.count("}")
        depth += o - c
        if depth <= 0:
            curfunc, depth = None, 0
        continue

    # error sites: structured API (single/multi line) + saveError
    for m in re.finditer(r"\.(errorf|warnf|fatalf)\(\s*[^,]+,\s*\"([^\"]+)\"", raw):
        ln = raw[:m.start()].count("\n") + 1
        # enclosing func
        fnm = None
        for k in range(ln - 1, -1, -1):
            fm = re.match(r"^func\s+(?:\([^)]*\)\s*)?(\w+)\s*\(", lines[k])
            if fm:
                fnm = fm.group(1)
                break
        msg = first_literal(find_string_arg(raw, m.end() - 1))
        errors.append((fnm or "?", fn, ln, m.group(1), m.group(2), msg or ""))

    for m in re.finditer(r"saveError\(", raw):
        ln = raw[:m.start()].count("\n") + 1
        arg = find_string_arg(raw, m.end())
        fnm = None
        for k in range(ln - 1, -1, -1):
            fm = re.match(r"^func\s+(?:\([^)]*\)\s*)?(\w+)\s*\(", lines[k])
            if fm:
                fnm = fm.group(1)
                break
        lit = first_literal(arg)
        if lit is None:
            # arg is an identifier: search the enclosing func body backwards
            ident = arg.strip()
            if re.match(r"^\w+$", ident):
                for k in range(ln - 1, -1, -1):
                    mm = re.match(rf"^\s*{ident}\s*:?=\s*(.*)$", lines[k])
                    if mm:
                        lit = first_literal(mm.group(1)) or "(動態組裝)"
                        break
            if lit is None:
                lit = "(動態組裝)"
        errors.append((fnm or "?", fn, ln, "saveError", "E_GENERAL", lit))

# ── report ───────────────────────────────────────────────────────────────────
W = []
w = W.append
w("# Nolang AST 節點清單、文法與錯誤訊息")
w("")
w("> 由 `scripts/ast_inventory.py` **直接從 Go 原始碼機械抽取**，無人工轉錄。")
w("> 來源：`src/parser/ast.go`（節點定義）、`src/parser/*.go`（建構點、錯誤點）、")
w("> `src/parser/dump/dump.go`（L2 oracle 的封閉型別 switch）。")
w("")

kinds = defaultdict(int)
for v in structs.values():
    kinds[v["kind"]] += 1
total_nodes = kinds["Node"] + kinds["Statement"] + kinds["Expression"] + kinds["Type"]
uncovered = [k for k in structs if structs[k]["kind"] != "-" and k not in cases]
shallow = []
for k in cases:
    joined = "\n".join(cases[k])
    direct = re.findall(r"d\.node\(&?n\.(\w+)", joined)
    loops = re.findall(r"range\s+n\.(\w+)", joined) if "d.node(" in joined else []
    if not (direct or loops):
        shallow.append(k)

w("## 0. 總覽")
w("")
w("| 項目 | 數量 |")
w("|---|---|")
w(f"| `ast.go` 內 struct 總數 | {len(structs)} |")
w(f"| 實作 `Node`（Pos+EndPos） | {total_nodes} |")
w(f"| 　└ `Statement` | {kinds['Statement']} |")
w(f"| 　└ `Expression` | {kinds['Expression']} |")
w(f"| 　└ `Type` | {kinds['Type']} |")
w(f"| 　└ 純 `Node`（無 marker） | {kinds['Node']} |")
w(f"| 非 `Node` 輔助 struct | {kinds['-']} |")
w(f"| `dumpNode()` case 數 | {len(cases)} |")
w(f"| ❌ **oracle 未覆蓋的 Node** | **{len(uncovered)}** |")
w(f"| ⚠️ 只印「殼」的節點（無遞迴） | {len(shallow)} |")
w(f"| `nodeString()` case 數 | {len(node_string_cases)} |")
w(f"| 錯誤訊息站點（`saveError` + 結構化 API） | {len(errors)} |")
w("")

w("## 1. 未被 oracle 覆蓋的節點（預期為空 ⇒ 空表就是綠燈）")
w("")
w("> 這一節由 `dumpNode` 的 case 清單與 `ast.go` 的 Node 清單相減而得。")
w("> **空 = `no ast` 對每個節點型別都有 case**（2026-10-10 補齊，見設計文檔 §5.3.1）。")
w("> 非空 ⇒ 那些節點的**整個子樹**在 L2 差分中不可見。")
w("")
w("| # | 節點 | ast.go 行 | 種類 | 欄位 | 建構函式 |")
w("|---|---|---|---|---|---|")
for n, k in enumerate(uncovered, 1):
    v = structs[k]
    flds = ", ".join(f"`{x}`:{t}" for x, t in v["fields"])
    bs = ", ".join(sorted({f for f, _ in builders.get(k, set())})) or "—"
    w(f"| {n} | `{k}` | {v['line']} | {v['kind']} | {flds} | {bs} |")
w("")

w("## 2. 只印「殼」的節點（dump 有 case，但無遞迴欄位 ⇒ 子樹不可見）")
w("")
w("| 節點 | 行 | 種類 | dump 引用到的欄位 | **未印出的欄位** |")
w("|---|---|---|---|---|")
for k in shallow:
    joined = "\n".join(cases[k])
    refd = set(re.findall(r"n\.(\w+)", joined))
    allf = [n for n, _ in structs[k]["fields"]]
    missing = [n for n in allf if n not in refd and n != "Token"]
    w(f"| `{k}` | {structs[k]['line']} | {structs[k]['kind']} | {', '.join(sorted(refd)) or '—'} | {', '.join('`'+m+'`' for m in missing) or '—'} |")
w("")

w("## 3. 遞迴覆蓋完整的節點")
w("")
w("| 節點 | 遞迴欄位 |")
w("|---|---|")
for k in cases:
    j = "\n".join(cases[k])
    recursed = sorted(set(re.findall(r"d\.node\(&?n\.(\w+)", j)) |
                       (set(re.findall(r"range\s+n\.(\w+)", j)) if "d.node(" in j else set()))
    if recursed:
        w(f"| `{k}` | {', '.join('`'+r+'`' for r in recursed)} |")
w("")

w("## 4. 非 Node 輔助 struct（被節點欄位引用，本身不進 AST 走訪）")
w("")
w("| struct | 行 | 欄位 |")
w("|---|---|---|")
for k, v in structs.items():
    if v["kind"] == "-":
        w(f"| `{k}` | {v['line']} | {', '.join(f'`{x}`:{t}' for x, t in v['fields'])} |")
w("")

w("## 5. 節點 ← 建構函式（文法歸屬）")
w("")
w("| 節點 | 種類 | 建構它的函式 |")
w("|---|---|---|")
for k in sorted(builders, key=lambda x: (structs[x]["kind"], x)):
    fns = ", ".join(sorted({f for f, _ in builders[k]}))
    w(f"| `{k}` | {structs[k]['kind']} | {fns} |")
w("")

w("## 6. 錯誤訊息目錄（供 `noc` 逐字重現）")
w("")
w("| # | 函式 | 位置 | 通道 | 錯誤碼 | 訊息格式 |")
w("|---|---|---|---|---|---|")
for n, (fnm, f, ln, ch, code, msg) in enumerate(
        sorted(errors, key=lambda e: (e[1], e[2])), 1):
    safe = (msg or "").replace("|", "\\|")
    w(f"| {n} | `{fnm}` | {f}:{ln} | {ch} | `{code}` | `{safe}` |")
w("")

out = "\n".join(W)
with open(OUT, "w", encoding="utf-8") as f:
    f.write(out)
print(f"[written {OUT}, {len(out.splitlines())} lines]", file=sys.stderr)
print(f"[nodes={total_nodes} uncovered={len(uncovered)} shallow={len(shallow)} errors={len(errors)}]",
      file=sys.stderr)
