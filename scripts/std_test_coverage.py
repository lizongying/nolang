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
  - `str.eq-raw`：`src/builtin/str.go` 里 `BuiltinMethod{MethodName:"str.eq",
    ForwardFunc:"eq-raw"}` 的**内部 forward 目标**。用户码只能写 `a.eq(b, n)`；
    `a.eq-raw(b, n)` 会被 checker 解析成方法但 MIR 报 `unknown callee
    str.eq-raw`（`src/mir/builtin_call.go` 只按 ForwardFunc 名内联，不注册
    该方法），`str.eq-raw(a, b, n)` 则报 `module 'str' has no top-level
    function`。两种形态都调不到，只能列在这里。
    新增同类条目时**必须**附上实测的两条报错原文，避免把"懒得测"混进这个清单。

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
    ('str', 'eq-raw'),  # str.eq 的内部 forward 目标，见 docstring
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
}

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
                unreachable = (mp, name.split('.', 1)[-1]) in UNREACHABLE
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
