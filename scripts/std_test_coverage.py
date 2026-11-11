#!/usr/bin/env python3
"""std_test_coverage.py — 盘点 src/std 的 API 面与 test/std 的实际覆盖。

API 面（inventory）来源是 `src/std/**/*.no` 的**顶层声明**：
  - `name = (...)`            → 模块函数，调用形式 `<alias>.<name>(...)`
  - `type.method = (...)`     → 方法，调用形式 `<recv>.<method>(...)`

覆盖判定（保守口径，宁可漏报也不虚报）：
  - 模块函数：**任意** `test/std/*.no` 出现 `<alias>.<name>` 即算覆盖，
    右侧要求不是标识符字符（Nolang 标识符含 `-`，所以边界是 `[^A-Za-z0-9_-]`），
    否则 `sha256.sha256-hex` 会被 `sha256.sha256` 假命中。
  - 方法：**该模块自己的** `test/std/<alias>.no` 出现 `.<method>(` 即算覆盖。
    不在别模块的测试里找，避免 `.len(` / `.init(` 这种通用名把上千个方法一起点亮。
    这是近似：同一模块内有多个结构体共用同名方法时可能高估（`--verbose` 会标 `~`）。
  - **例外：全局内建模块**（见 UNQUALIFIED_CALL_MODULES）。`global` 里的
    `print` / `eprint` / `format` / `with-cap` / `with-len` / `with-cap-len` / `exit`
    是语言级内建，文档规定的调用形态**不带模块前缀**，带前缀反而不通
    （`global.print(...)` ⇒ `unsupported builtin`；`global.with-cap(...)` ⇒ LLVM
    校验失败）。故这些模块的顶层函数按「裸调用 `name(`」判定。
    `async` 不在此列——`async.cancel(h)` / `async.cancelled()` 是通的。

私有符号（`_` 前缀）**不计入分母**：checker 的 private_std 守卫（诊断码 `raqvyltn`）
禁止测试档（用户码）调用 `<alias>._foo(...)`，它们在设计上就无法被直接调用，
只经公开 API 间接执行。强行计入会让分母含永远填不满的项，掩盖真实缺口。
`--missing` 会把它们单独列在 "private (不可直接调用)" 段，便于确认没有漏掉公开包装。

**平台不适用**的声明同样不计入分母（自动判定，无需手工登记）：
`src/std` 用 `#{mac-arm64, linux-amd64, ...}` 标注平台变体。在某个宿主上，
标签集合与该宿主标签集合**无交集**的声明根本编不进来（例如 macOS 上
`#{win-amd64, win-arm64}` 的 `process.list-all`），计入分母等于要求
"在 macOS 上测 Windows 代码"。判定规则：无平台标签 = 全平台适用；
有标签但含非平台标记（`buildin` / `track-caller` / `overflow=...`）= 忽略该标记。
`--missing` 会单列 "platform N/A" 段，`--verbose` 可看宿主标签。

另有一条**显式**的"结构上不可达"清单 UNREACHABLE（同样不计入分母）：
  - （已清空）曾经唯一的条目 `str.eq-raw` 于 2026-11-11 **从 `src/std/str.no`
    直接删除**了，不再是"不可达"而是"不存在"，故此处不再登记。
    历史背景：它是 `src/builtin/str.go` 里 `BuiltinMethod{MethodName:"str.eq",
    ForwardFunc:"eq-raw"}` 的**内部 forward 目标**；用户码只能写 `a.eq(b, n)`，
    `a.eq-raw(b, n)` 被 checker 解析成方法但 MIR 报 `unknown callee str.eq-raw`，
    `str.eq-raw(a, b, n)` 报 `module 'str' has no top-level function`。
    那个 `#{buildin}` 空壳声明全仓零调用点，属于死符号。
    新增同类条目时**必须**附上实测的两条报错原文，避免把"懒得测"混进这个清单。

模块级例外 DEAD_MODULES（整个模块的符号都不计入分母）：判定标准与 UNREACHABLE 一样严，
必须同时满足「转译器/构建链里确实没有通往它的路径」与「实测报错原文」两条。
目前只有一个：`collection/static-hashmap`（详见该常量上的注释）。

用法：
  python3 scripts/std_test_coverage.py                # 汇总 + 缺口清单
  python3 scripts/std_test_coverage.py --missing      # 只列缺口符号
  python3 scripts/std_test_coverage.py --module sha256
  python3 scripts/std_test_coverage.py --module archive     # 也吃目录前缀：archive/*
  python3 scripts/std_test_coverage.py --markdown     # 输出 markdown 表
  python3 scripts/std_test_coverage.py --strict       # 有缺口则 rc=1
"""

import argparse
import os
import platform
import re
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), '..'))
STD = os.path.join(ROOT, 'src', 'std')
TEST = os.path.join(ROOT, 'test', 'std')
SIGS = os.path.join(ROOT, 'src', 'checker', 'stdsig_gen.go')

# 顶层声明：`name = (` / `type.method = (`
DECL_RE = re.compile(r'^([A-Za-z_][A-Za-z0-9_.\-]*)\s*=\s*\(')
# 结构体定义：`name {`
STRUCT_RE = re.compile(r'^([A-Za-z_][A-Za-z0-9_\-]*)\s*\{\s*$')
# 模块别名表：`"alias": "path/to/module",`
MODULE_RE = re.compile(r'^\s*"([^"]+)":\s*"([^"]+)",\s*$')

ID_TAIL = r'(?![A-Za-z0-9_\-])'


def read(path):
    with open(path, encoding='utf-8', errors='replace') as f:
        return f.read()


def load_module_aliases():
    """从 stdsig_gen.go 的 embeddedStdModules 读 path -> alias。"""
    text = read(SIGS) if os.path.exists(SIGS) else ''
    aliases = {}
    in_block = False
    for line in text.splitlines():
        if 'embeddedStdModules = map[string]string{' in line:
            in_block = True
            continue
        if in_block:
            if line.strip() == '}':
                break
            m = MODULE_RE.match(line)
            if m:
                alias, path = m.group(1), m.group(2)
                aliases[path] = alias
    return aliases


def strip_comment(line):
    """去掉行尾注释（`;` 起）与字符串字面量的内容。

    字符串内容必须一并抹掉：测试档常把符号名写进断言说明
    （如 `runner.test('zip.u16le/u32le', …)`），若保留就会被当成调用而虚报覆盖。
    引号本身保留，仅把引号内的字符换成空格，行号与列位置不受影响。
    """
    out = []
    i = 0
    quote = None
    while i < len(line):
        c = line[i]
        if quote:
            if c == quote:
                out.append(c)
                quote = None
            else:
                out.append(' ')
        else:
            if c in '\'"`':
                quote = c
                out.append(c)
            elif c == ';':
                break
            else:
                out.append(c)
        i += 1
    return ''.join(out)


# 结构上不可达的公开符号（不计入分母）。键是 (模块路径, 声明名)。
# 见文件头 docstring 的说明：每一项都必须实测过两种调用形态都失败。
UNREACHABLE = {
    # str.eq-raw 曾在此，已于 2026-11-11 从 src/std/str.no 删除（死符号），见 docstring。
    # process 的 9 个 Win32 内建：声明在 .no 里只标了 `#{buildin}`、没标平台，
    # 但 Go 侧只在 Windows 注册实现。macOS 上实测（逐个跑过）统一报：
    #   EmitLLVM: unsupported builtin process.win-create-pipe [2kqboupo]
    # 换符号名报同一条，仅末段不同：
    #   process.win-close-handle / win-write-pipe / win-read-pipe /
    #   win-create-process / win-wait-process / win-get-exit-code /
    #   win-terminate-process / win-get-std-handle
    # 注意：不能靠给 src/std 加 `#{win-amd64, win-arm64}` 标签来修——
    #   genstdsig 不解析 `#{...}`（stdsig_gen.go 是跨平台共用扁平表），
    #   加了标签既改变不了解析行为，也让声明与实际签名表不一致。
    ('process', 'win-create-pipe'),
    ('process', 'win-close-handle'),
    ('process', 'win-write-pipe'),
    ('process', 'win-read-pipe'),
    ('process', 'win-create-process'),
    ('process', 'win-wait-process'),
    ('process', 'win-get-exit-code'),
    ('process', 'win-terminate-process'),
    ('process', 'win-get-std-handle'),
    # math.arr-zero：`#{buildin}` 的 ForwardFunc，只以**接收者形式**存在
    # （`[N]num.zero()` / `[]num.zero()`），签名 `() {}` 没有任何形参，
    # 因此不存在 `math.arr-zero(...)` 这种全限定调用。实测 `arr.zero()` /
    # `sl.zero()` 正常（test/std/math.no 的 t-arr-zero 盖的就是它），
    # 覆盖率工具按「函数名 + 别名」匹配，看不见方法形式 ⇒ 与 str.eq-raw 同类。
    ('math', 'arr-zero'),
    # math.swap：签名 `() (a t, b t)`——两个都是**结果参数**，泛型 t 只能由
    # 实参推断，而实参不存在。两种调用形态实测：
    #   a, b = math.swap()      ⇒ 能编过但不交换（a=11/b=0、a=0/b=0 非确定）
    #   a, b = math.swap(a, b)  ⇒ EmitLLVM: unknown callee math.swap in func main [4qhyyund]
    # 全仓库也没有可工作的调用点（`vec.swap` / `arr.swap` 是数组方法，与它无关）。
    ('math', 'swap'),
    # fmt.printf / fmt.eprintf：源码里明确标注「已移除（名称保留供解析）」，
    # 调用即编译错误，两种形态都过不去：
    #   fmt.printf('x')   ⇒ [ERROR] deprecated: printf() is removed; use print(...)
    #                        for stdout with a newline, or io.out(...) ... [oflgmsef]
    #   fmt.eprintf('x')  ⇒ [ERROR] deprecated: eprintf() is removed; use eprint(...)
    #                        for stderr with a newline, or io.err(...) ... [oflgmsef]
    # （fmt.sprintf 是例外：它真的还能用，已在 test/std/fmt.no 覆盖。）
    ('fmt', 'printf'),
    ('fmt', 'eprintf'),
    # vec.vec-push / vec.vec-clear：`#{buildin}` 声明，是 `[]t.push` / `[]t.clear`
    # 内建方法的**实现名**，没有模块级函数。实测：
    #   vec.vec-push(v, 1)  ⇒ unknown function 'vec.vec-push': module 'vec' has no
    #                         top-level function 'vec-push' [trdj5rp8]
    #   vec.vec-clear()     ⇒ unknown function 'vec.vec-clear': module 'vec' has no
    #                         top-level function 'vec-clear' [trdj5rp8]
    # 对应的用户形态 `.push(` / `.clear(` 已在 test/std/vec.no 覆盖。
    ('vec', 'vec-push'),
    ('vec', 'vec-clear'),
    # err.code.to-name：枚举 `code` 上的方法，用户码取不到。
    #   code.io.to-name()            ⇒ EmitLLVM: unknown callee code.io.to-name [4qhyyund]
    #   c code = code.io; c.to-name() ⇒ EmitLLVM: unknown callee i64.to-name [4qhyyund]
    # 枚举在 MIR 里是 i64，方法表按 i64 查不到 `to-name`；std 内部
    # （err.format → `.code.to-name()`）能编过，因为它走的是同一函数体内的
    # 静态解析路径。是真实缺陷，已记进 MEMORY-RISKS。
    ('err', 'to-name'),
    # embed.read / embed.exists：`fs.embed` 上的方法被**同名全局内建**顶掉——
    # `read` / `exists` 都在 src/builtin/os.go 注册为内置 MethodName，
    # 调用点优先命中内建，接收者 `fs.embed` 被当成普通实参处理。实测：
    #   D.exists('a.txt')  ⇒ EmitLLVM: builtin fs.embed.exists: cannot marshal
    #                         arg 0 as C string [z68vr2e2]
    #   D.read('a.txt')    ⇒ 能编过，但返回的 ?[]byte 标签非法：三分支 ok/nil/err
    #                         一个都不匹配（hit 计数停在 0），内容永远取不到。
    #   全限定写法 fs.embed.read(D, 'a.txt') 同样命中内建，报同一条。
    # 这是真实缺陷（embed 模块实际不可用），已记进 MEMORY-RISKS。
    ('embed', 'read'),
    ('embed', 'exists'),
    # os.syslog：src/builtin/os.go 只注册了 ForwardFunc:"syslog"，没有 CLibCall，
    # 等于「声明了但没实现」。两种形态实测：
    #   syslog(6, 'x')        ⇒ [ERROR] 'syslog' is not defined; call it as
    #                            'os.syslog' ... [4bek3xc6]
    #   os.syslog(6, 'x')     ⇒ 编过，运行期直接 SIGSEGV（无输出、rc=139）
    # 是真实缺陷（不是环境问题），已记进 MEMORY-RISKS。
    ('os', 'syslog'),
    # os 的 3 个 Win32 目录查找内建：与 process 的 win-* 同一类，Go 侧只在
    # Windows 注册实现，macOS 上统一报：
    #   EmitLLVM: unsupported builtin os.win-find-first-file [2kqboupo]
    # （win-find-next-file / win-find-close 同一条，仅末段符号不同。）
    ('os', 'win-find-first-file'),
    ('os', 'win-find-next-file'),
    ('os', 'win-find-close'),
}

# 模块级「结构性不可调用」清单：源码被刻意保留、但构建链里没有任何路径能特化/调用它。
# 与 UNREACHABLE 的区别只是**粒度**（整模块 vs 单符号），判定标准一样严 ——
# 必须指出构建链断点 + 附实测报错，否则不收。
#
# collection/static-hashmap（31 个符号）：SPEC
#   `.trae/specs/split-map-to-static-and-vec-generics` 的产物 ——
#   "将旧实现搬移至 static-hashmap.no…**BREAKING**：static-hashmap.no 的模板不会被
#    转译器自动特化（转译器仅寻找 hashmap-{cat}-tmpl）；静态版本暂为『保留原始码』"。
# 断点在 `src/build/generic_structs.go:403`：特化入口写死
#   `tmplName := "hashmap-" + cat + "-tmpl"`，从不查 `static-hashmap-*`
#   ⇒ 三个模板永远不生成具体结构与方法。实测（macOS，2026-11-11）：
#   `m static-hashmap.static-hashmap-str-tmpl = static-hashmap.static-hashmap-str-tmpl{}`
#     ⇒ `no vet` 只给 tcpoxtfd（"型别标注可省略"）hint，0 error / 0 warning；
#        `no run` ⇒ EmitLLVM: unknown callee
#        static-hashmap.static-hashmap-str-tmpl.init in func main [4qhyyund]
#   `-int-tmpl` / `-bool-tmpl` 一字不差（仅末段符号名不同）。
#   改写特化名 `static-hashmap-str-i64{}` ⇒ 同一条报错（没有特化器认识这个名字）。
# 用户真正在用的 `[str]i64` 走的是 `hashmap-*-tmpl`（`collection/map.no`），已 100% 覆盖；
# 本模块是**死代码**，等后续 Spec 给它一条显式特化路径后再回来测（届时删掉这条登记）。
DEAD_MODULES = {'collection/static-hashmap'}

# 少数模块的顶层声明是**全局内建**：文档规定的调用形态不带模块前缀
# （`print(...)` / `with-cap(n)` / `format(...)` / `exit(n)`），带前缀反而过不去：
#   global.print('x')    ⇒ EmitLLVM: unsupported builtin global.print [2kqboupo]
#   global.eprint('x')   ⇒ 同上（eprint 走同一条 unsupported 分支）
#   global.with-cap(8)   ⇒ LLVM 校验失败：'%tenum_global_option' 与 i64 不符，
#                          build error: linking failed [wjju8z98]
# （global.exit 是唯一两个形态都通的，但它会终止进程，不适合当断言。）
# 因此这些模块的顶层函数按「裸调用文本」判定覆盖。
UNQUALIFIED_CALL_MODULES = {'global'}

# 平台标签：`#{mac-arm64, linux-amd64, wasi-wasm32}`。行内也可能混有
# `buildin` / `track-caller` / `overflow=wrap` 这类非平台标记，只取平台形态。
PLATFORM_TOK_RE = re.compile(r'\b(?:mac|linux|win|wasi)-[A-Za-z0-9]+\b')


def host_tags():
    """当前宿主的平台标签集合，与 src/std 的 `#{...}` 注解同词表。"""
    sysname = platform.system()
    machine = (platform.machine() or '').lower()
    arch = 'arm64' if machine in ('arm64', 'aarch64') else 'amd64'
    if sysname == 'Darwin':
        return {'mac-' + arch}
    if sysname == 'Windows':
        return {'win-' + arch}
    if sysname == 'Linux':
        return {'linux-' + arch}
    return set()


HOST = host_tags()


def collect_inventory():
    """返回 {(module_alias, kind, symbol, owner)} 与模块元信息。"""
    aliases = load_module_aliases()
    items = []
    modules = {}
    for dirpath, _dirs, files in os.walk(STD):
        for fn in sorted(files):
            if not fn.endswith('.no'):
                continue
            full = os.path.join(dirpath, fn)
            rel = os.path.relpath(full, STD).replace(os.sep, '/')
            mp = rel[:-3]
            alias = aliases.get(mp)
            if alias is None:
                base = os.path.basename(mp)
                alias = base
                cands = [p for p in aliases if p.endswith('/' + mp) or p == mp]
                if len(cands) == 1:
                    alias = aliases[cands[0]]
            modules[mp] = alias
            structs = set()
            pending_tags = None  # None = 无平台标签（全平台适用）
            for line in read(full).splitlines():
                stripped = line.strip()
                if stripped.startswith('#{') and stripped.endswith('}'):
                    toks = PLATFORM_TOK_RE.findall(stripped[2:-1])
                    if toks:
                        pending_tags = set(toks)
                    continue
                if line[:1] in (';', '#', '/', ' ', '\t') or not line:
                    continue
                code = strip_comment(line)
                if not code.strip():
                    continue
                tags, pending_tags = pending_tags, None
                on_host = tags is None or bool(tags & HOST)
                m = STRUCT_RE.match(code)
                if m:
                    structs.add(m.group(1))
                    continue
                m = DECL_RE.match(code)
                if not m:
                    continue
                name = m.group(1)
                unreachable = (mp in DEAD_MODULES
                               or (mp, name.split('.', 1)[-1]) in UNREACHABLE)
                if '.' in name:
                    owner, meth = name.split('.', 1)
                    items.append({
                        'module': mp, 'alias': alias, 'kind': 'method',
                        'owner': owner, 'symbol': meth, 'decl': name,
                        'private': meth.startswith('_') or unreachable,
                        'platform': on_host, 'tags': tags,
                    })
                else:
                    items.append({
                        'module': mp, 'alias': alias, 'kind': 'func',
                        'owner': None, 'symbol': name, 'decl': name,
                        'private': name.startswith('_') or unreachable,
                        'platform': on_host, 'tags': tags,
                    })
    return items, modules


def load_tests():
    """{basename_without_ext: text}（注释与字符串字面量内容已剥离，
    避免档头说明或断言说明里的名字被算成覆盖）"""
    out = {}
    if not os.path.isdir(TEST):
        return out
    for fn in sorted(os.listdir(TEST)):
        if fn.endswith('.no'):
            lines = [strip_comment(ln) for ln in read(os.path.join(TEST, fn)).splitlines()]
            out[fn[:-3]] = '\n'.join(lines)
    return out


def func_covered(tests, alias, symbol):
    pat = re.compile(re.escape('%s.%s' % (alias, symbol)) + ID_TAIL)
    hits = [name for name, text in tests.items() if pat.search(text)]
    if not hits and alias in UNQUALIFIED_CALL_MODULES:
        # 全局内建只能裸调用（带前缀编不过），按裸调用文本判定
        bare = re.compile(r'(?<![A-Za-z0-9_.\-])' + re.escape(symbol) + r'\s*\(')
        hits = [name for name, text in tests.items() if bare.search(text)]
        hits = [h + '(bare)' for h in hits[:1]]
    return hits


def method_covered(tests, alias, owner, symbol):
    own = tests.get(alias)
    hits = []
    if own is not None and ('.' + symbol + '(') in own:
        hits.append(alias)
    elif own is not None and owner == alias:
        # 联合型别别名上的零参方法（num.abs / num.sign / int.to-str …）
        # 点式 `x.abs()` 尚未支持，用户只能写裸函数形式 `abs(x)`。
        if re.search(r'(?<![A-Za-z0-9_.\-])' + re.escape(symbol) + r'\s*\(', own):
            hits.append(alias + '(bare)')
    # 也接受全限定 `alias.owner.symbol(` / `owner.symbol(` 跨档写法
    for name, text in tests.items():
        if name == alias:
            continue
        if ('%s.%s.%s(' % (alias, owner, symbol)) in text or \
           ('%s.%s(' % (owner, symbol)) in text:
            hits.append(name)
    return hits


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--missing', action='store_true', help='只列缺口符号')
    ap.add_argument('--module', help='只看某个模块（路径或别名）')
    ap.add_argument('--markdown', action='store_true', help='markdown 表格输出')
    ap.add_argument('--strict', action='store_true', help='有缺口则 rc=1')
    ap.add_argument('--verbose', action='store_true', help='含完整符号名')
    args = ap.parse_args()

    items, modules = collect_inventory()
    tests = load_tests()

    by_module = {}
    for it in items:
        it['hits'] = (func_covered(tests, it['alias'], it['symbol'])
                      if it['kind'] == 'func'
                      else method_covered(tests, it['alias'], it['owner'], it['symbol']))
        it['covered'] = bool(it['hits'])
        by_module.setdefault(it['module'], []).append(it)

    only = None
    prefix = None
    if args.module:
        only = args.module.strip('/')
        if only not in by_module:
            cands = [m for m in by_module if modules[m] == args.module]
            if cands:
                only = cands[0]
            else:
                # 目录前缀：`--module archive` 等同 archive/*
                pre = only + '/'
                if any(m.startswith(pre) for m in by_module):
                    prefix = pre
                    only = None
        if only is None and prefix is None:
            sys.exit('unknown module: %s' % args.module)

    rows = []
    for mp in sorted(by_module):
        if only and mp != only:
            continue
        if prefix and not mp.startswith(prefix):
            continue
        its = by_module[mp]
        na = [i for i in its if not i['platform']]
        its = [i for i in its if i['platform']]
        f = [i for i in its if i['kind'] == 'func' and not i['private']]
        m = [i for i in its if i['kind'] == 'method' and not i['private']]
        priv = [i for i in its if i['private']]
        rows.append({
            'module': mp,
            'alias': its[0]['alias'] if its else by_module[mp][0]['alias'],
            'has_test': by_module[mp][0]['alias'] in tests,
            'funcs': len(f), 'funcs_cov': sum(1 for i in f if i['covered']),
            'methods': len(m), 'methods_cov': sum(1 for i in m if i['covered']),
            'priv': len(priv),
            'na': len(na),
            'na_names': ['%s [%s]' % (i['decl'], ', '.join(sorted(i['tags'] or ())))
                         for i in na],
            'missing': [i['decl'] for i in its if not i['covered'] and not i['private']],
            'priv_missing': [i['decl'] for i in priv if not i['covered']],
            'items': its,
        })

    tot = sum(r['funcs'] + r['methods'] for r in rows)
    cov = sum(r['funcs_cov'] + r['methods_cov'] for r in rows)
    no_test = [r for r in rows if not r['has_test']]

    if args.missing:
        for r in rows:
            if r['missing']:
                print('[%s] (%s) %d missing:' % (r['module'], r['alias'], len(r['missing'])))
                for name in r['missing']:
                    print('    ' + name)
        na = [(r['module'], r['alias'], r['na_names']) for r in rows if r['na']]
        if na:
            print()
            print('platform N/A（宿主 %s，标签无交集，本机编不出来，不计入分母）共 %d 个：' %
                  ('/'.join(sorted(HOST)) or '?', sum(len(p[2]) for p in na)))
            for mod, alias, names in na:
                print('[%s] (%s) %d platform-N/A:' % (mod, alias, len(names)))
                for name in names:
                    print('    ' + name)
        priv = [(r['module'], r['alias'], r['priv_missing']) for r in rows if r['priv_missing']]
        if priv:
            print()
            print('private / unreachable（`_` 前缀或 UNREACHABLE，用户码不可调用，不计入分母）共 %d 个：' %
                  sum(len(p[2]) for p in priv))
            for mod, alias, names in priv:
                print('[%s] (%s) %d private/unreachable:' % (mod, alias, len(names)))
                for name in names:
                    print('    ' + name)
        return 1 if (args.strict and tot != cov) else 0

    if args.markdown:
        print('| 模块 | 测试档 | 函数 | 函数覆盖 | 方法 | 方法覆盖 |')
        print('|---|---|---:|---:|---:|---:|')
        for r in rows:
            print('| `%s` | %s | %d | %d | %d | %d |' % (
                r['module'], '`%s.no`' % r['alias'] if r['has_test'] else '**缺**',
                r['funcs'], r['funcs_cov'], r['methods'], r['methods_cov']))
        print('| **合计** | | **%d** | **%d** | **%d** | **%d** |' % (
            sum(r['funcs'] for r in rows), sum(r['funcs_cov'] for r in rows),
            sum(r['methods'] for r in rows), sum(r['methods_cov'] for r in rows)))
        return 0

    print('std 文件 %d / 模块别名 %d / 测试档 %d' % (len(modules), len(set(modules.values())), len(tests)))
    print('API 面 %d（函数 %d，方法 %d），已覆盖 %d（%.1f%%）' % (
        tot, sum(r['funcs'] for r in rows), sum(r['methods'] for r in rows),
        cov, 100.0 * cov / tot if tot else 0.0))
    priv_n = sum(r['priv'] for r in rows)
    if priv_n:
        print('另有私有/不可达符号 %d 个（`_` 前缀或 UNREACHABLE，用户码不可调用，不计入分母）' % priv_n)
    na_n = sum(r['na'] for r in rows)
    if na_n:
        print('另有平台不适用声明 %d 个（宿主 %s，`#{...}` 标签无交集，本机编不出来，不计入分母）' % (
            na_n, '/'.join(sorted(HOST)) or '?'))
    print('无专用测试档的模块 %d 个：%s' % (
        len(no_test), ' '.join('`%s`' % r['module'] for r in no_test) or '（无）'))
    print()
    print('%-34s %6s %6s %7s %7s' % ('module', 'func', 'cov', 'method', 'cov'))
    for r in rows:
        flag = '' if r['has_test'] else '  <-- no test file'
        print('%-34s %6d %6d %7d %7d%s' % (
            r['module'], r['funcs'], r['funcs_cov'], r['methods'], r['methods_cov'], flag))
    if tot != cov:
        print()
        print('缺口明细（--missing 看全量）：')
        worst = sorted(rows, key=lambda r: -(len(r['missing'])))
        for r in worst:
            if not r['missing']:
                continue
            head = r['missing'][:8]
            more = '' if len(r['missing']) <= 8 else ' …(+%d)' % (len(r['missing']) - 8)
            print('  %-30s %3d  %s%s' % (r['module'], len(r['missing']), ' '.join(head), more))
    return 1 if (args.strict and tot != cov) else 0


if __name__ == '__main__':
    sys.exit(main())
