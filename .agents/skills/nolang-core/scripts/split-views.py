#!/usr/bin/env python3
"""把 nolang-syntax / nolang-std 两个超大 SKILL.md 按视图机械切分。

用法：
    python3 split-views.py --src <旧 skills 根目录> [--out <nolang-core/modules>] [--check]

设计约束：
- 按行号区间切，不重新排版、不改写任何正文 —— 保证「视图行数之和 + 丢弃区间 = 源文件行数」。
- 锚点（`### ^` 标题文本）原样保留，因此旧的 `#anchor` 链接在视图内依然有效。
- 每个视图顶部插入一行 H1 与一行来源注记，便于追溯。

退出码：0 正常；1 行数不守恒（说明映射表与源文件漂移）。
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

# (源文件, 输出相对路径, 视图标题, [(起始行, 结束行, 是否丢弃)])
# 行号沿用源文件 1-based 闭区间；边界取「标题行」到「下一个标题行 - 1」。
VIEWS: dict[str, list[tuple[str, str, list[tuple[int, int, bool]]]]] = {
    "nolang-syntax": [
        (
            "syntax/10-data-types.md",
            "Data types, variables, naming, layout",
            [
                (491, 1119, False),  # Data Types … Code Style
                (3256, 3348, False),  # Type Casting / Integer Assignment
                (4354, 4417, False),  # Option Payload Inline Threshold
                (4418, 4519, False),  # String Operations（语言语义侧）
            ],
        ),
        (
            "syntax/20-functions-control.md",
            "Functions, control flow, loops, early exit",
            [
                (1120, 1759, False),  # Functions … Placeholder statement
                (2381, 2436, False),  # Multi-Assignment
            ],
        ),
        (
            "syntax/30-match-branch.md",
            "Match, `it` binding, if/else short-circuit chains",
            [(1760, 2258, False)],
        ),
        (
            "syntax/40-structs-enums.md",
            "Structs, enums, methods, slices, indexing, interfaces, generics",
            [(2437, 3255, False)],
        ),
        (
            "syntax/50-modules-imports.md",
            "Import, module prefix, export",
            [(3349, 3855, False)],
        ),
        (
            "syntax/60-annotations-ffi.md",
            "Operators, FFI, annotation system, overflow",
            [
                (3856, 3940, False),  # Special Symbols & Operators
                (3941, 4219, False),  # FFI + Annotations
                (4333, 4334, False),  # #{embed} 两条（原文误置于 JS Backend 段尾）
                (4345, 4353, False),  # #{range} 示例 + AnnotationStatement（原文误置于 package.noml 段尾）
                (4520, 4605, False),  # Integer Arithmetic Overflow
            ],
        ),
        (
            "syntax/70-runtime-async.md",
            "Async, coroutine groups, JS backend",
            [
                (2259, 2380, False),  # Async/Await + Coroutine Groups
                (4220, 4332, False),  # JS Backend
            ],
        ),
        (
            "m93-cli.md",
            "CLI, workspace layout, packages, dependencies",
            [
                (117, 484, False),  # Installation & Usage
                (4335, 4344, False),  # package.noml Compiler Configuration
            ],
        ),
    ],
    "nolang-std": [
        (
            "std/10-base.md",
            "types, option（`###` 级小节）",
            [(153, 201, False)],
        ),
        (
            "std/20-core.md",
            "fmt, math, char, str, number/num/int/float, byte, txt, vec, arr, sort",
            [(202, 534, False)],
        ),
        (
            "std/30-os-io.md",
            "os, fs, env, args, path, bufio, io, regexp, process",
            [(535, 1014, False)],
        ),
        (
            "std/40-net.md",
            "net 与 net/* 全部协议客户端（http/tls/ws/quic/http2/http3/dns/url/cookie/multipart/hpack/proxy/pool/unix/sse/ip）",
            [(1015, 1642, False)],
        ),
        (
            "std/50-time-log.md",
            "time, log",
            [(1643, 1747, False)],
        ),
        (
            "std/60-data-structs.md",
            "set, map/* 与 collection/*（哈希/树/链表/静态表/队列/栈）, heap, deque",
            [(1748, 2070, False)],
        ),
        (
            "std/70-db-encode.md",
            "database/sql, encoding/*(hex/base64/csv/pem), archive/*(tar/zip/gzip/bzip2/xz/zlib/zstd)",
            [(2071, 2299, False)],
        ),
        (
            "std/80-crypto.md",
            "crypto/* 全部（aes/rsa/ecdsa/ed25519/x25519/sha*/md5/hmac/hkdf/pbkdf2/argon2/scrypt/chacha20/rand/x509…）",
            [(2300, 2622, False)],
        ),
        (
            "std/85-exchange.md",
            "json, toml, yaml, dom",
            [(2623, 2977, False)],
        ),
        (
            "std/90-misc.md",
            "unicode, uuid, bigint + err/bool/enter-leave/async/global/magic（`###` 级小节）",
            [(2978, 3176, False)],
        ),
    ],
}

# 不属于任何视图、且有明确去向/需要丢弃的区间（仅用于守恒校验）
DISCARDED: dict[str, list[tuple[int, int, str]]] = {
    "nolang-syntax": [
        (1, 63, "frontmatter + 旧 TOC（由 m10-syntax 路由替代）"),
        (64, 116, "Introduction → 收入 m10-syntax 路由（手工撰写，不参与机械切分）"),
        (485, 488, "Golden Rule → 已并入 m00-redlines，避免三处重复"),
        (489, 490, "Quick Reference 空标题"),
        (4606, 4621, "Standard Library 指针 + See Also（合并后失效）"),
    ],
    "nolang-std": [
        (1, 152, "frontmatter + 旧 TOC"),
        (3177, 3298, "Module List → 收入 m20-std 路由作索引"),
        (3299, 3305, "See Also（合并后失效）"),
    ],
}


def parse_args() -> argparse.Namespace:
    ap = argparse.ArgumentParser()
    ap.add_argument("--src", required=True, help="旧 skills 根目录（含 nolang-syntax/ 等）")
    ap.add_argument("--out", default=None, help="输出 modules/ 目录，默认 <skill>/modules")
    ap.add_argument("--check", action="store_true", help="只校验行数守恒，不写文件")
    return ap.parse_args()


def main() -> int:
    args = parse_args()
    src_root = Path(args.src).resolve()
    out_root = Path(args.out) if args.out else Path(__file__).resolve().parent.parent / "modules"

    failed = False
    for skill, views in VIEWS.items():
        source = src_root / skill / "SKILL.md"
        if not source.exists():
            print(f"SKIP {skill}: {source} 不存在（可能已退役）")
            continue
        lines = source.read_text(encoding="utf-8").splitlines(keepends=True)
        total = len(lines)

        covered = 0
        for rel, title, spans in views:
            chunks = []
            for start, end, drop in spans:
                chunks.append("".join(lines[start - 1 : end]))
                if not drop:
                    covered += end - start + 1
            body = "\n".join(c.rstrip("\n") for c in chunks) + "\n"
            router = "m10-syntax" if skill == "nolang-syntax" else "m20-std"
            href = f"../{router}.md" if "/" in rel else f"{router}.md"
            header = (
                f"# {title}\n\n"
                f"> 视图切分自原 `{skill}/SKILL.md`，正文未改写；"
                f"索引与加载判据见 [`{href}`]({href})。\n\n"
            )
            target = out_root / rel
            if not args.check:
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text(header + body, encoding="utf-8")
            print(f"  {'CHECK' if args.check else 'WROTE'} {rel:<32} {sum(e - s + 1 for s, e, _ in spans):>5} 行")

        dropped = sum(end - start + 1 for start, end, _ in DISCARDED.get(skill, []))
        accounted = covered + dropped
        status = "OK" if accounted == total else "MISMATCH"
        if accounted != total:
            failed = True
        print(f"{skill}: 源 {total} 行 = 视图 {covered} + 去向/丢弃 {dropped} = {accounted}  [{status}]")
        if accounted != total:
            missing = total - accounted
            print(f"  !! 未覆盖 {missing} 行，检查映射表是否与源文件漂移")
        print()

    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
