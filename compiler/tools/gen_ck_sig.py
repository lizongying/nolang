#!/usr/bin/env python3
"""gen_ck_sig.py — M3-b: stdsig 表迁移（Go → nolang）；M3-c s13: 裸名门表（bk/bv）。

Authority: src/checker/stdsig_gen.go（由 genstdsig 生成，'make gen' 重collect）。
Output:    compiler/src/ck-sig.no —— noc checker（M3-c）消费的 std 签名表。

一次性转换工具：stdsig_gen.go 变了（std 改动 + make gen）就要重跑本脚本。
noc 启动时【不】重新收集（§5.4 决策：与 no 的时序不同、且拖慢每次启动）。

s13 追加的 bk/bv「裸名门」列（checker `'X' is not defined` 族的数据源）：
  Authority = **oracle 二进制行为本身**（./bin/no vet 批量探测），非任何 Go 源文件。
  机制（§5.3.4.ar 实测定型）：Go ValidateUndefinedVars 的裸标识符放行臂层层叠加
  （exports 常量/真函数、FindBuiltinMethod、multiOwner、curModule……），无法从
  stdsig 表静态复原；故对「表短名全集 U」逐名探测：
    bk = U ∪ builtin 注册名（ASCII 升序）
    bv = '-' → oracle 沉默（放行）；owner 模块名 → oracle 报错并带
         「call it as 'owner.name'」提示；'' → 报错但无提示（门控名无 unique owner）。
  不在 bk 的名字（如 'foo'、'fd'、'ord'）= 完全未知 ⇒ noc 报无提示的 not defined。
  ⚠️ 依赖构建产物 ./bin/no（或 env NOLANG_ORACLE 指定）；oracle 语义变了 = 行为变了，
  重跑本脚本即追平。探测批量：一个临时文件每名一行 `q{i} = NAME()`；若该文件出现
  【parse】错误（关键字撞名等）⇒ 逐名降级单测（parse 失败的名按沉默处理）。

编码约定（全部实测定死，见 docs §5.3.4.an）：
  * 每张表按 key 升序（ASCII 字节序，与 Go sort 一致；全部 key/值为纯 ASCII）。
  * Params/Results/变体清单：逗号连接（型串字符集实测不含 ','）。
  * structFields 拍平为 'Struct.field' -> type 平行数组（标识符不含 '.'，无歧义）。
  * 表规模与源文件 sha256 铸进 CK-SIG-DIGEST，L0 测试据此钉同步。
"""
import hashlib
import re
import sys
import os
import subprocess
import tempfile

GO_FILE = "src/checker/stdsig_gen.go"
OUT_FILE = "compiler/src/ck-sig.no"

ENTRY_RE = re.compile(
    r'"([^"]+)":\s*StdSigEntry\{Params:\s*\[\]string\{([^}]*)\},\s*Results:\s*\[\]string\{([^}]*)\}\}'
)
KV_RE = re.compile(r'"([^"]+)":\s*"([^"]*)"')
NESTED_RE = re.compile(r'"([^"]+)":\s*\{([^}]*)\}')
LISTLIT_RE = re.compile(r'"([^"]+)":\s*\{([^{}]*)\}')


def q_types(blob):
    """[]string{...} 内容 ⇒ 逗号连接（空 ⇒ ''）。"""
    items = re.findall(r'"([^"]*)"', blob)
    return ",".join(items)


def section(text, name):
    i = text.index(name)
    j = text.index("\n\t}", i) if "\n\t}" in text[i:] else text.index("\n}", i)
    # Go 源里每个 map 以 `\n\t}` 收尾（init 内缩进一层）
    return text[i:j]


def parse(text):
    funcs = [(k, q_types(p), q_types(r))
             for k, p, r in ENTRY_RE.findall(section(text, "embeddedStdFuncSigs"))]
    methods = [(k, q_types(p), q_types(r))
               for k, p, r in ENTRY_RE.findall(section(text, "embeddedStdMethodSigs"))]
    struct_fields = []
    for sname, inner in NESTED_RE.findall(section(text, "embeddedStdStructFields")):
        for fname, ftype in re.findall(r'"([^"]+)":\s*"([^"]*)"', inner):
            struct_fields.append((sname + "." + fname, ftype))
    aliases = KV_RE.findall(section(text, "embeddedStdAliases"))
    struct_mod = KV_RE.findall(section(text, "embeddedStdStructMod"))
    enum_variants = [(k, ",".join(re.findall(r'"([^"]*)"', v)))
                     for k, v in LISTLIT_RE.findall(section(text, "embeddedStdEnumVariants"))]
    modules = KV_RE.findall(section(text, "embeddedStdModules"))
    return dict(funcs=funcs, methods=methods, struct_fields=sorted(struct_fields),
                aliases=sorted(aliases), struct_mod=sorted(struct_mod),
                enum_variants=sorted(enum_variants), modules=sorted(modules))


def esc(s):
    assert all(ord(c) < 128 for c in s), f"non-ASCII key/value: {s!r}"
    assert "'" not in s, f"single quote in value: {s!r}"
    return s


def owner_map(funcs, modules):
    """复刻 checker.buildStdFuncOwner：fk 'mod.fn' 唯一 owner 表（多 owner 剔除）。"""
    mods = set(m for m, _ in modules)
    first, multi, globals_ = {}, set(), set()
    for k, _, _ in funcs:
        i = k.find(".")
        if i <= 0:
            continue
        mod, fn = k[:i], k[i + 1:]
        if "." in fn:
            continue  # struct method recorded in func table —— 非 plain fn
        if mod == "global":
            globals_.add(fn)
            continue
        if mod in mods:
            if fn in first and first[fn] != mod:
                multi.add(fn)
            elif fn not in first:
                first[fn] = mod
    owner = {f: m for f, m in first.items() if f not in multi}
    return owner, globals_


def probe_universe(t):
    """表短名全集 U：fk/mk/sfk 键短名 ∪ 枚举变体名 ∪ Go builtin 注册名。

    短名 = 最后一个 '.' 之后段（Go 1948-1953 对 dotted export 一律注册短名别名，
    多点键如 'sqlite.db-sqlite.query' → 'query' 也在内）。
    sfk（'Struct.field'）短名亦须入集：field 名可撞 gated 短名（实测 'query' 裸调用
    报无提示 not defined——不在表即丢提示形态）。
    """
    u = set()
    for k, _, _ in t["funcs"]:
        u.add(k.rsplit(".", 1)[-1])
    for k, _, _ in t["methods"]:
        u.add(k.rsplit(".", 1)[-1])
    for k, _ in t["struct_fields"]:
        u.add(k.rsplit(".", 1)[-1])
    for _, variants in t["enum_variants"]:
        u.update(v for v in variants.split(",") if v)
    root = os.getcwd()
    bdir = os.path.join(root, "src", "builtin")
    for fn in sorted(os.listdir(bdir)):
        if fn.endswith(".go") and not fn.endswith("_test.go"):
            src = open(os.path.join(bdir, fn)).read()
            u.update(re.findall(r'MethodName: *"([^"]+)"', src))
    u = {n for n in u if re.fullmatch(r"[a-zA-Z][a-zA-Z0-9_-]*", n or "")}
    return sorted(u)


def probe_bare_ok(universe):
    """对 oracle 批量探测每名裸调用沉默性。返回 dict name→'silent'|'error'。"""
    oracle = os.environ.get("NOLANG_ORACLE", "./bin/no")
    if not os.path.exists(oracle if not oracle.startswith(".") else os.path.join(os.getcwd(), oracle)):
        sys.exit(
            f"gen_ck_sig: oracle 不存在（{oracle}）；先 `make no` 或设 NOLANG_ORACLE")

    def batch_run(names, tag):
        lines = [f"q{i} = {n}()" for i, n in enumerate(names)]
        path = os.path.join(tempfile.gettempdir(), f"cksig-probe-{tag}.no")
        open(path, "w").write("\n".join(lines) + "\n")
        r = subprocess.run([oracle, "vet", path],
                           capture_output=True, text=True, timeout=600)
        out = r.stdout
        parse_bad = any(
            "nolang-compile: expected" in l for l in out.splitlines())
        err_names = set(re.findall(r"'([^']+)' is not defined", out))
        return parse_bad, err_names

    verdict = {}
    parse_bad, err_names = batch_run(universe, "batch")
    if not parse_bad:
        for n in universe:
            verdict[n] = "error" if n in err_names else "silent"
        return verdict
    # 降级：批量文件有 parse 错（关键字撞名等）⇒ 逐名单测
    print(f"gen_ck_sig: 批量探测命中 parse 错，逐名降级 {len(universe)} 次…")
    for n in universe:
        try:
            _, e1 = batch_run([n], "one")
            verdict[n] = "error" if n in e1 else "silent"
        except subprocess.TimeoutExpired:
            verdict[n] = "silent"  # 超时保守按沉默（FN 安全侧）
    return verdict


def chunked_fill(name, pairs, idx, label, chunk=400):
    """分塊 push 鏈：外殼 ck-sig-fill-{name} ＋ 各塊 ck-sig-fill-{name}-g{N}。

    ⚠️ 單函數 push 數必須有界（實測）：MIR 每個值表達式進函數入口一個 alloca，
    一條 push ≈2 alloca/49 行 IR；2,371 條 push 的單函數 ⇒ 11.6 萬行 IR，
    clang -O2 18 分鐘未完（asp.no tape 1,540 條可過 ⇒ 閾值在 1.5k~2.4k 之間）。
    """
    lines = []
    groups = [pairs[i:i + chunk] for i in range(0, len(pairs), chunk)]
    shell = [f"ck-sig-fill-{name} = (t ck-sig) {{"]
    for gi in range(len(groups)):
        shell.append(f"    ck-sig-fill-{name}-g{gi}(t)")
    shell.append("}")
    lines.extend(shell)
    lines.append("")
    for gi, g in enumerate(groups):
        lines.append(f"ck-sig-fill-{name}-g{gi} = (t ck-sig) {{")
        for p in g:
            lines.append(f"    t.{label}.push('{esc(p[idx])}')")
        lines.append("}")
        lines.append("")
    return lines


def emit(name, pairs, label):
    """一列一条 push 链的 fill 函数（键列 = 索引 0）。"""
    return chunked_fill(name, pairs, 0, label)


def value_col(name, pairs, idx, label):
    return chunked_fill(name, pairs, idx, label)


def main():
    root = os.getcwd()
    src = open(os.path.join(root, GO_FILE)).read()
    digest = hashlib.sha256(src.encode()).hexdigest()
    t = parse(src)

    # s13：裸名门表 bk/bv（oracle 探测 + owner 提示模块）
    #   bk = 探测全集（表短名 ∪ builtin 名），ASCII 升序，与 bv 平行；
    #   bv = '' → oracle 沉默放行；owner 模块名 → oracle 报错并带 call-it-as 提示。
    #   ⚠️ 必须【全量入表】：沉默名（如 'max'）若不入表，noc 会把它当【完全未知】
    #   而误报——正主 Go 侧经 exports/builtin/multiOwner 放行（§5.3.4.ar 实证）。
    #   bv 编码（名均不含 '-' 以外的合法 module 名撞哨兵）：
    #     '-'    = oracle 沉默放行
    #     owner  = 报错 + call-it-as 提示（fk unique owner）
    #     ''     = 报错但无提示（门控名无 unique owner，如仅方法短名 'from-bits'；
    #              实证 /tmp/s13-hint-check.no）；完全不在表亦裸报错（同一形态）
    universe = probe_universe(t)
    verdict = probe_bare_ok(universe)
    owner, globals_ = owner_map(t["funcs"], t["modules"])
    n_silent = 0
    bare_rows = []
    for n in sorted(universe):
        if verdict[n] == "error":
            bare_rows.append((n, owner.get(n, ""), owner.get(n, "")))
        else:
            n_silent += 1
            bare_rows.append((n, "-", owner.get(n, "")))

    # 排序自检（bsearch 前置条件）
    for key in ("funcs", "methods", "struct_fields", "aliases", "struct_mod",
                "enum_variants", "modules"):
        ks = [p[0] for p in t[key]]
        assert ks == sorted(ks), f"{key} not sorted"
        assert len(ks) == len(set(ks)), f"{key} duplicate keys"
    bks = [p[0] for p in bare_rows]
    assert bks == sorted(bks) and len(bks) == len(
        set(bks)), "bare-gate keys not sorted/unique"

    out = []
    out.append("; ck-sig — std 签名/结构/别名/枚举/模块表（M3-b，生成物）")
    out.append(";")
    out.append("; ⚠️【生成檔，勿手改】再生成：python3 compiler/tools/gen_ck_sig.py")
    out.append(
        f"; Authority：{GO_FILE}（genstdsig/'make gen' 产物）；源 sha256 见 CK-SIG-DIGEST。")
    out.append(
        "; bk/bv/bo 裸名门表 Authority = oracle 行为探测（./bin/no vet 批量），非 Go 源文件；")
    out.append(
        ";   bv='-' 沉默放行 / bv=owner 模块名 ⇒ 带 call-it-as 提示 / bv='' 或不在表 ⇒ 裸 not defined。")
    out.append(
        ";   bo = fk unique owner（可空）——正主 stdFuncModule 同值，供 curModule 自模块裸调放行臂。")
    out.append(";")
    out.append("; 编码约定：")
    out.append(";   * 各表按 key ASCII 升序 ⇒ ck-sig-bsearch 二分查找")
    out.append(";   * Params/Results/变体清单以 ',' 连接（型串字符集实测不含 ','）")
    out.append(";   * structFields 拍平为 'Struct.field' -> type（标识符不含 '.'，无歧义）")
    out.append(";   * 全部键/值为纯 ASCII（ck-sig-lt 以 byte(i)/len-bytes() 逐字节比较）")
    out.append(";")
    out.append("; 表规模（生成时实测）：")
    out.append(
        f";   funcs {len(t['funcs'])} / methods {len(t['methods'])} / struct-fields {len(t['struct_fields'])}")
    out.append(
        f";   aliases {len(t['aliases'])} / struct-mod {len(t['struct_mod'])} / enums {len(t['enum_variants'])} / modules {len(t['modules'])}")
    out.append(
        f";   bare-gate {len(bare_rows)} (silent {n_silent} / gated {len(bare_rows) - n_silent})")
    out.append("")
    out.append("CK-SIG-VERSION = 3")
    out.append(f"CK-SIG-DIGEST = '{digest}'")
    out.append("")
    out.append("ck-sig {")
    for col in ("fk", "fp", "fr", "mk", "mp", "mr", "sfk", "sft", "alk", "alt",
                "smk", "smt", "evk", "evt", "mok", "moo", "bk", "bv", "bo"):
        out.append(f"    {col} []str")
    out.append("}")
    out.append("")

    fills = []
    fills += emit("fk", t["funcs"], "fk")
    fills += value_col("fp", t["funcs"], 1, "fp")
    fills += value_col("fr", t["funcs"], 2, "fr")
    fills += emit("mk", t["methods"], "mk")
    fills += value_col("mp", t["methods"], 1, "mp")
    fills += value_col("mr", t["methods"], 2, "mr")
    fills += emit("sfk", t["struct_fields"], "sfk")
    fills += value_col("sft", t["struct_fields"], 1, "sft")
    fills += emit("alk", t["aliases"], "alk")
    fills += value_col("alt", t["aliases"], 1, "alt")
    fills += emit("smk", t["struct_mod"], "smk")
    fills += value_col("smt", t["struct_mod"], 1, "smt")
    fills += emit("evk", t["enum_variants"], "evk")
    fills += value_col("evt", t["enum_variants"], 1, "evt")
    fills += emit("mok", t["modules"], "mok")
    fills += value_col("moo", t["modules"], 1, "moo")
    fills += emit("bk", bare_rows, "bk")
    fills += value_col("bv", bare_rows, 1, "bv")
    fills += value_col("bo", bare_rows, 2, "bo")

    out.append("; ── 表构造（一次性 push 链，仿 asp.no strs/tape 形制）──")
    out.append("")
    caps = dict(fk=len(t["funcs"]), fp=len(t["funcs"]), fr=len(t["funcs"]),
                mk=len(t["methods"]), mp=len(t["methods"]), mr=len(t["methods"]),
                sfk=len(t["struct_fields"]), sft=len(t["struct_fields"]),
                alk=len(t["aliases"]), alt=len(t["aliases"]),
                smk=len(t["struct_mod"]), smt=len(t["struct_mod"]),
                evk=len(t["enum_variants"]), evt=len(t["enum_variants"]),
                mok=len(t["modules"]), moo=len(t["modules"]),
                bk=len(bare_rows), bv=len(bare_rows), bo=len(bare_rows))
    new_body = ["ck-sig.new = () (t ck-sig) {", "    t = ck-sig {", ]
    for col in ("fk", "fp", "fr", "mk", "mp", "mr", "sfk", "sft", "alk", "alt",
                "smk", "smt", "evk", "evt", "mok", "moo", "bk", "bv", "bo"):
        new_body.append(f"        {col}: with-cap({caps[col]})")
    new_body.append("    }")
    for name in ("fk", "fp", "fr", "mk", "mp", "mr", "sfk", "sft", "alk", "alt",
                 "smk", "smt", "evk", "evt", "mok", "moo", "bk", "bv", "bo"):
        new_body.append(f"    ck-sig-fill-{name}(t)")
    new_body.append("}")
    out.extend(new_body)
    out.append("")

    # fill 函数体大（>1000 条 push），按列分组放文件尾部，可读性无关
    out.append("; ── 各列数据（生成区）──")
    out.extend(fills)

    out.append("""
; ── 查找層 ──

; ck-sig-str-at：str 欄位安全取值。⚠️ 不用 []t.at——外掛 receiver 的泛型容器方法
; 單態化在當代 Go 工具鏈報 'EmitLLVM: unknown callee []t.at'（實測）；索引讀回
; ?str、三 tag 臂顯式窮盡（裸 -> 尾臂在 ok 走過還會再觸發——match 塊非排他）。
ck-sig-str-at = (v []str, i i64) (r str) {
    r = ''
    x = v[i]
    x: {
        ok -> {
            r = it
        }

        nil -> {
        }

        err -> {
        }
    }
}

; ck-sig-lt：ASCII 逐字节字典序比较；a < b ⇒ 1，否则 0。
; 全表键值实测纯 ASCII，故 byte 序 == Go string 排序。
; ⚠️ 工具链 bug（实测钉死）：`x = recv.call(): {match块}` 内联形——match 块直接挂在
;    调用表达式之后——所得 x 在后续关系比较臂中恒假（区域变量/函数内均复现）；
;    两步形 `o = recv.call()` + `x = o: {match块}` 正常。byte(i) 回 ?byte，
;    故此处必须两步写。ast.no:362 同款内联形仅用于算术（+/*）未触雷，勿照抄。
ck-sig-lt = (a str, b str) (lt i64) {
    lt = 0
    an = a.len-bytes()
    bn = b.len-bytes()
    n = an
    bn < n -> {
        n = bn
    }
    i = 0
    (i < n) {
        oa = a.byte(i)
        ob = b.byte(i)
        ca = oa: {
            ok -> it
            -> 0
        }
        cb = ob: {
            ok -> it
            -> 0
        }
        ca < cb -> {
            lt = 1
            return
        }

        cb < ca -> {
            return
        }
        i = i + 1
    }
    an < bn -> {
        lt = 1
    }
}

; ck-sig-bsearch：升序键表二分；命中回下标，未命中回 -1。
; 三條件臂互斥（lt 兩向 + eq），順序求值無串臂風險；鍵表無空串⇒miss 恒 -1。
ck-sig-bsearch = (keys []str, name str) (idx i64) {
    idx = -1
    lo = 0
    hi = keys.len()
    (lo < hi) {
        mid = lo + ((hi - lo) / 2)
        k = ck-sig-str-at(keys, mid)
        ck-sig-lt(k, name) == 1 -> {
            lo = mid + 1
        }

        ck-sig-lt(name, k) == 1 -> {
            hi = mid
        }

        k == name -> {
            idx = mid
            return
        }
    }
}

ck-sig-find-func = (t ck-sig, name str) (idx i64) {
    idx = ck-sig-bsearch(t.fk, name)
}

ck-sig-find-method = (t ck-sig, key str) (idx i64) {
    idx = ck-sig-bsearch(t.mk, key)
}

; 'Struct.field'（如 'arp-info.mac'）
ck-sig-find-struct-field = (t ck-sig, key str) (idx i64) {
    idx = ck-sig-bsearch(t.sfk, key)
}

ck-sig-find-alias = (t ck-sig, name str) (idx i64) {
    idx = ck-sig-bsearch(t.alk, name)
}

ck-sig-find-struct-mod = (t ck-sig, name str) (idx i64) {
    idx = ck-sig-bsearch(t.smk, name)
}

ck-sig-find-enum = (t ck-sig, name str) (idx i64) {
    idx = ck-sig-bsearch(t.evk, name)
}

ck-sig-find-module = (t ck-sig, name str) (idx i64) {
    idx = ck-sig-bsearch(t.mok, name)
}

; ck-sig-bare：裸名门表查询（s13 not-defined 族）。单串打包回值（SHS 单 out 约束）：
;   '-'          ⇒ 表内沉默名（oracle 经 exports/builtin/multiOwner 放行）
;   '@'          ⇒ 完全不在表内 ⇒ 裸 not defined（无提示）
;   owner - '&' - bo ⇒ 门控名；owner 段 = bv（提示模块，可空——无 unique owner 如
;              仅方法短名 'from-bits'）；bo 段 = fk unique owner（stdFuncModule 同值，
;              供 curModule 自模块裸调放行臂）。'&' 不在模块名字符集 ⇒ 分隔无歧义。
ck-sig-bare = (t ck-sig, name str) (r str) {
    r = '@'
    idx = ck-sig-bsearch(t.bk, name)
    idx >= 0 -> {
        v = ck-sig-str-at(t.bv, idx)
        v == '-' -> {
            r = '-'
            return
        }
        o = ck-sig-str-at(t.bo, idx)
        r = v - '&' - o
    }
}

; ck-sig-types：逗号串 ⇒ 型串 vec；'' ⇒ 空 vec。
ck-sig-types = (s str) (r []str) {
    r []str = with-cap(2)
    s.len-bytes() > 0 -> {
        r = s.split(',')
    }
}
""")
    open(os.path.join(root, OUT_FILE), "w").write("\n".join(out) + "\n")
    print(f"wrote {OUT_FILE}: funcs={len(t['funcs'])} methods={len(t['methods'])} "
          f"struct-fields={len(t['struct_fields'])} aliases={len(t['aliases'])} "
          f"struct-mod={len(t['struct_mod'])} enums={len(t['enum_variants'])} "
          f"modules={len(t['modules'])} "
          f"bare-gate={len(bare_rows)}(silent {n_silent}/gated {len(bare_rows) - n_silent}) "
          f"digest={digest[:12]}")


if __name__ == "__main__":
    main()
