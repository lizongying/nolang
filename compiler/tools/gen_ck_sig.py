#!/usr/bin/env python3
"""gen_ck_sig.py — M3-b: stdsig 表迁移（Go → nolang）。

Authority: src/checker/stdsig_gen.go（由 genstdsig 生成，'make gen' 重collect）。
Output:    compiler/src/ck-sig.no —— noc checker（M3-c）消费的 std 签名表。

一次性转换工具：stdsig_gen.go 变了（std 改动 + make gen）就要重跑本脚本。
noc 启动时【不】重新收集（§5.4 决策：与 no 的时序不同、且拖慢每次启动）。

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

    # 排序自检（bsearch 前置条件）
    for key in ("funcs", "methods", "struct_fields", "aliases", "struct_mod",
                "enum_variants", "modules"):
        ks = [p[0] for p in t[key]]
        assert ks == sorted(ks), f"{key} not sorted"
        assert len(ks) == len(set(ks)), f"{key} duplicate keys"

    out = []
    out.append("; ck-sig — std 签名/结构/别名/枚举/模块表（M3-b，生成物）")
    out.append(";")
    out.append("; ⚠️【生成檔，勿手改】再生成：python3 compiler/tools/gen_ck_sig.py")
    out.append(f"; Authority：{GO_FILE}（genstdsig/'make gen' 产物）；源 sha256 见 CK-SIG-DIGEST。")
    out.append(";")
    out.append("; 编码约定：")
    out.append(";   * 各表按 key ASCII 升序 ⇒ ck-sig-bsearch 二分查找")
    out.append(";   * Params/Results/变体清单以 ',' 连接（型串字符集实测不含 ','）")
    out.append(";   * structFields 拍平为 'Struct.field' -> type（标识符不含 '.'，无歧义）")
    out.append(";   * 全部键/值为纯 ASCII（ck-sig-lt 以 byte(i)/len-bytes() 逐字节比较）")
    out.append(";")
    out.append("; 表规模（生成时实测）：")
    out.append(f";   funcs {len(t['funcs'])} / methods {len(t['methods'])} / struct-fields {len(t['struct_fields'])}")
    out.append(f";   aliases {len(t['aliases'])} / struct-mod {len(t['struct_mod'])} / enums {len(t['enum_variants'])} / modules {len(t['modules'])}")
    out.append("")
    out.append("CK-SIG-VERSION = 1")
    out.append(f"CK-SIG-DIGEST = '{digest}'")
    out.append("")
    out.append("ck-sig {")
    for col in ("fk", "fp", "fr", "mk", "mp", "mr", "sfk", "sft", "alk", "alt",
                "smk", "smt", "evk", "evt", "mok", "moo"):
        out.append(f"    {col} []str")
    out.append("}")
    out.append("")

    fills = []
    fills += emit("fk", t["funcs"], "fk"); fills += value_col("fp", t["funcs"], 1, "fp")
    fills += value_col("fr", t["funcs"], 2, "fr")
    fills += emit("mk", t["methods"], "mk"); fills += value_col("mp", t["methods"], 1, "mp")
    fills += value_col("mr", t["methods"], 2, "mr")
    fills += emit("sfk", t["struct_fields"], "sfk"); fills += value_col("sft", t["struct_fields"], 1, "sft")
    fills += emit("alk", t["aliases"], "alk"); fills += value_col("alt", t["aliases"], 1, "alt")
    fills += emit("smk", t["struct_mod"], "smk"); fills += value_col("smt", t["struct_mod"], 1, "smt")
    fills += emit("evk", t["enum_variants"], "evk"); fills += value_col("evt", t["enum_variants"], 1, "evt")
    fills += emit("mok", t["modules"], "mok"); fills += value_col("moo", t["modules"], 1, "moo")

    out.append("; ── 表构造（一次性 push 链，仿 asp.no strs/tape 形制）──")
    out.append("")
    caps = dict(fk=len(t["funcs"]), fp=len(t["funcs"]), fr=len(t["funcs"]),
                mk=len(t["methods"]), mp=len(t["methods"]), mr=len(t["methods"]),
                sfk=len(t["struct_fields"]), sft=len(t["struct_fields"]),
                alk=len(t["aliases"]), alt=len(t["aliases"]),
                smk=len(t["struct_mod"]), smt=len(t["struct_mod"]),
                evk=len(t["enum_variants"]), evt=len(t["enum_variants"]),
                mok=len(t["modules"]), moo=len(t["modules"]))
    new_body = ["ck-sig.new = () (t ck-sig) {", "    t = ck-sig {", ]
    for col in ("fk", "fp", "fr", "mk", "mp", "mr", "sfk", "sft", "alk", "alt",
                "smk", "smt", "evk", "evt", "mok", "moo"):
        new_body.append(f"        {col}: with-cap({caps[col]})")
    new_body.append("    }")
    for name in ("fk", "fp", "fr", "mk", "mp", "mr", "sfk", "sft", "alk", "alt",
                 "smk", "smt", "evk", "evt", "mok", "moo"):
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
          f"modules={len(t['modules'])} digest={digest[:12]}")


if __name__ == "__main__":
    main()
