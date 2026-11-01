#!/usr/bin/env python3
"""给生成的 .ll 的每个 define 补上 sanitize_address 函数属性。

⚠️ 为什么必须这一步（s33 实测）：直接 `clang -x ir -fsanitize=address x.ll`
   **不做任何插桩**——极小程序 malloc→free→load 实测 exit 0，二进制里只有
   __asan_init / __version_mismatch 两个 asan 符号。原因：ASan pass 只插桩
   带 `sanitize_address` **函数属性**的函数；clang 前端处理 .no 源码时自动加，
   处理 .ll 输入时**不加**。于是「ASan 干净」实际只覆盖 libc 拦截器
   （malloc/free/memcpy），裸 load/store 的 UAF 全部漏报。
   （用 `opt -passes=asan` 手动插桩亦可，但 LLVM22 在 MachO 上报
     "doesn't support COMDATs, 'asan.module_ctor'"，故改走前端。）
用法：python3 tmp/asan-instrument.py in.ll out.ll
"""
import re, sys

src, dst = sys.argv[1], sys.argv[2]
text = open(src).read()


def add_attr(m):
    head, tail = m.group(1), m.group(2)
    if 'sanitize_address' in tail:
        return m.group(0)
    return f'{head} sanitize_address {tail}'


new, n = re.subn(r'^(define [^@{]*@[A-Za-z0-9_.]+\([^)]*\))(.*)$', add_attr, text, flags=re.M)
open(dst, 'w').write(new)
print(f'{dst}: 打了 {n} 个 sanitize_address 属性')
