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
            for line in read(full).splitlines():
                if line[:1] in (';', '#', '/', ' ', '\t') or not line:
                    continue
                code = strip_comment(line)
                if not code.strip():
                    continue
                m = STRUCT_RE.match(code)
                if m:
                    structs.add(m.group(1))
                    continue
                m = DECL_RE.match(code)
                if not m:
                    continue
                name = m.group(1)
                if '.' in name:
                    owner, meth = name.split('.', 1)
                    items.append({
                        'module': mp, 'alias': alias, 'kind': 'method',
                        'owner': owner, 'symbol': meth, 'decl': name,
                        'private': meth.startswith('_'),
                    })
                else:
                    items.append({
                        'module': mp, 'alias': alias, 'kind': 'func',
                        'owner': None, 'symbol': name, 'decl': name,
                        'private': name.startswith('_'),
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
        f = [i for i in its if i['kind'] == 'func' and not i['private']]
        m = [i for i in its if i['kind'] == 'method' and not i['private']]
        priv = [i for i in its if i['private']]
        rows.append({
            'module': mp,
            'alias': its[0]['alias'],
            'has_test': its[0]['alias'] in tests,
            'funcs': len(f), 'funcs_cov': sum(1 for i in f if i['covered']),
            'methods': len(m), 'methods_cov': sum(1 for i in m if i['covered']),
            'priv': len(priv),
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
        priv = [(r['module'], r['alias'], r['priv_missing']) for r in rows if r['priv_missing']]
        if priv:
            print()
            print('private (_ 前缀，用户码不可直接调用，不计入分母) 共 %d 个：' %
                  sum(len(p[2]) for p in priv))
            for mod, alias, names in priv:
                print('[%s] (%s) %d private:' % (mod, alias, len(names)))
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
        print('另有私有符号 %d 个（`_` 前缀，用户码不可调用，不计入分母）' % priv_n)
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
