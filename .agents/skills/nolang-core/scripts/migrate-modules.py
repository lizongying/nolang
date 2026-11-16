#!/usr/bin/env python3
"""把 9 个旧 skill 迁入 nolang-core/modules/，并在迁移时消解已知冲突。

与 split-views.py 的分工：
- split-views.py 只做「按行区间机械切片」，不改写正文；
- 本脚本做「整档搬迁 + 定点修复」，每处替换都断言命中次数，未命中即失败（不允许静默空跑）。

用法：python3 migrate-modules.py --src <旧 skills 根目录> [--out <modules/>] [--check]
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

SEC = "## "

MODULES = [
    {
        "src": "nolang-build",
        "dest": "m30-build.md",
        "title": "Build — 只走 make",
        "banner": (
            "> 构建入口。构建后必须过 vet 门槛，但**门槛本身不在本档**，见 [`m40-verify.md`](m40-verify.md)。\n"
        ),
        "cut": ["## See Also"],
        "line_edits": [
            (
                89,
                101,
                """## Post-Build Verification (Mandatory)

构建后必须过 vet 门槛。**规则、命令与排错全部在 [`m40-verify.md`](m40-verify.md)**（单一来源，此处不重复）。

构建侧只需要记住两个事实：

- `make` 会顺带安装全局 `no`，所以 `PATH` 上的 `no` 与 `./bin/no` 恒为同一份最新构建；
- `no build` / `no test` 的 std 走**内嵌 blob**，不是磁盘 `src/std` —— 改过 std 必须 `make no` 才生效。""",
            )
        ],
        "repl": [],
    },
    {
        "src": "nolang-vet",
        "dest": "m40-verify.md",
        "title": "Verify — `no vet` 门槛（唯一来源）",
        "banner": (
            "> **本模块是 `no vet` 门槛的唯一来源。** 原 `nolang-vet` 已并入此处；\n"
            "> build / debug / memory / builtin 四处的重复表述一律改为指向本文件，不再各自复述。\n"
        ),
        "cut": ["## See Also"],
        "line_edits": [],
        "repl": [],
    },
    {
        "src": "nolang-debug",
        "dest": "m50-debug.md",
        "title": "Debug — 测试先行的编译器排错流程",
        "banner": (
            "> 流程：最小失败用例 → 修复 → 逐级验证。第 5 步的 vet 规则在 [`m40-verify.md`](m40-verify.md)。\n"
        ),
        "cut": ["## See Also"],
        "line_edits": [
            (
                146,
                146,
                "> 规则、命令与排错见 [`m40-verify.md`](m40-verify.md)（单一来源）。本节只保留**调试场景特有的差异**：`no vet` 与 LSP vet 的分工。",
            )
        ],
        "repl": [
            # 原档指向不存在的 `nolang-syntax-reference/`（合并前就是断链），归位到真实视图
            (
                "詳見 [nolang-syntax-reference](./nolang-syntax-reference/SKILL.md)。",
                "詳見 [`syntax/10-data-types.md`](syntax/10-data-types.md#file-naming)。",
            ),
            (
                "(see [nolang-syntax-reference](file://../nolang-syntax/SKILL.md) for details)",
                "(see [`syntax/40-structs-enums.md`](syntax/40-structs-enums.md) for details)",
            ),
            (
                "詳見 [nolang-syntax-reference](./nolang-syntax-reference/SKILL.md) 的 Control Flow 段落。",
                "詳見 [`syntax/20-functions-control.md`](syntax/20-functions-control.md) 的 Control Flow 段落。",
            ),
        ],
    },
    {
        "src": "nolang-memory",
        "dest": "m60-memory.md",
        "title": "Memory — 所有权模型与堆释放（quarantined）",
        "banner": (
            "> ⚠️ **本模块默认关闭（`quarantined`），且引用已部分失效。**\n"
            "> 它描述的是 **legacy LLVM 后端**（`src/build/llvm/`，c7febdb 整体删除）的实现。\n"
            "> **内存模型与所有权语义仍然成立**（单所有权 / move / clone / 延迟零初始化 / CFG moved-facts），\n"
            "> 但所有 `(legacy)` 标记的 `src/build/llvm/*.go` 路径与 `emit*` / `track*` 函数名都已不存在，\n"
            "> 活实现在 `src/mir/`（`codegen.go` / `hir2mir.go` / `analysis.go` / `builtin_call.go`）。\n"
            "> 加载时以 `src/mir/` 实测为准，不要照本文路径跳转。\n"
        ),
        "cut": ["## See Also"],
        "line_edits": [
            (
                759,
                759,
                "./bin/no vet src/std          # 从仓库根执行；路径一律 workspace-root 相对（见 m80-path-resolution.md）",
            )
        ],
        "repl": [],
    },
    {
        "src": "nolang-builtin",
        "dest": "m70-builtin.md",
        "title": "Builtin — 内建函数注册与查找",
        "banner": (
            "> 注册在 `src/builtin/`（名字解析），能否出 IR 取决于 `src/mir/` 有没有 lowering。\n"
        ),
        "cut": ["## See Also"],
        "line_edits": [],
        "repl": [
            (
                "4. 在标准库 skill 文档（`nolang-std/SKILL.md`）中添加 API 说明。",
                "4. 在标准库视图里添加 API 说明 —— 按模块选档，见 [`m20-std.md`](m20-std.md) 的索引。",
            ),
            (
                "5. 运行 `no vet src/std` 确保无错误。",
                "5. 运行 `no vet src/std` 确保无错误（门槛见 [`m40-verify.md`](m40-verify.md)）。",
            ),
            (
                "- 语法侧（`{name:spec}` 说明符、`{{`/`}}` 转义）见 skill `nolang-syntax` 的 Output/Formatting",
                "- 语法侧（`{name:spec}` 说明符、`{{`/`}}` 转义）见 [`m10-syntax.md`](m10-syntax.md) 路由下的 Formatting 小节",
            ),
        ],
    },
    {
        "src": "nolang-path-resolution",
        "dest": "m80-path-resolution.md",
        "title": "Path resolution — workspace-root 相对路径约定",
        "banner": (
            "> 所有 embed / import 路径一律解析为**相对 workspace 根的规范绝对路径**，\n"
            "> 绝不相对当前源文件目录 —— 这也是 vet / 缓存键必须从仓库根执行的原因。\n"
        ),
        "cut": ["## Related"],
        "line_edits": [],
        "repl": [],
    },
    {
        "src": "nolang-selfhost-perf",
        "dest": "m90-selfhost-perf.md",
        "title": "Selfhost perf — 自举编译器性能约定",
        "banner": (
            "> 面向 `compiler/src/*.no`（自举编译器）。改 Go 侧性能看 [`m30-build.md`](m30-build.md) 与 `test/README.md`。\n"
        ),
        "cut": [],
        "line_edits": [],
        "repl": [
            (
                "[`docs/design/nolang-selfhost.md`](../../../docs/design/nolang-selfhost.md)",
                "[`docs/design/nolang-selfhost.md`](../../../../docs/design/nolang-selfhost.md)",
            )
        ],
    },
    {
        "src": "nolang-docs-i18n",
        "dest": "m91-docs-i18n.md",
        "title": "Docs i18n — 英文文档追平中文源",
        "banner": (
            "> 中文 `docs/docs/**` 是唯一真相源，`docs/i18n/en/**` 是它的翻译。\n"
        ),
        "cut": [],
        "line_edits": [],
        "repl": [
            ("- `scripts/parity.py`", "- `../scripts/parity.py`"),
        ],
    },
    {
        "src": "nolang-release",
        "dest": "m92-release.md",
        "title": "Release — 发版流程",
        "banner": (
            "> ⚠️ **与红线冲突时以 [`m00-redlines.md`](m00-redlines.md) 为准。**\n"
            "> 关键差异：本仓库常有多个 session 同时改同一棵树，因此**禁止 `git add -A`** ——\n"
            "> 发布提交只包含 `HISTORY.md`、文档站历史镜像，以及用户**逐项确认**的改动。\n"
        ),
        "cut": [],
        "line_edits": [
            (
                150,
                155,
                """   python3 <skill-dir>/scripts/release.py body --version vX.Y.Z > /tmp/release-msg.txt
   git add HISTORY.md docs/docs/history.md docs/i18n/en/docusaurus-plugin-content-docs/current/history.md
   # 仅当用户逐项确认「这些改动属于本次发版」时，才把它们显式加入：
   # git add path/one.no path/two.go
   git commit -F /tmp/release-msg.txt
   python3 <skill-dir>/scripts/release.py tag --version vX.Y.Z
   git push origin main
   git push origin vX.Y.Z""",
            ),
            (
                33,
                42,
                """### 0. 前置检查：分清「本次发版的改动」与「并发 session 的在飞改动」

`plan` 会跑 `git status --porcelain`，脏工作树**不中断**流程。但这仓库经常有多个 session
同时改同一棵树，所以**禁止 `git add -A`**：它会把别人的在飞改动一起卷进发布提交。
发布提交只应包含 `HISTORY.md`、文档站历史镜像，以及用户**逐项确认过**的待发布改动。

`plan` / `apply` 会列出其它脏文件。逐个核对：属于本次发版的记下路径，第四步显式 `git add`；
不属于的（临时产物、别人正在改的文件）**留在工作树里不动**——别提、别删、别顺手提交。""",
            ),
        ],
        "repl": [
            (
                "写完后脚本还会再查一次工作树：除 HISTORY.md 与文档站历史镜像（CN `docs/docs/history.md` + EN `docs/i18n/en/docusaurus-plugin-content-docs/current/history.md`）之外的脏文件**不再报错**，`apply` 只会列出它们并说明这些待发布改动会一并用 `git add -A` 打进发布提交。",
                "写完后脚本还会再查一次工作树：除 HISTORY.md 与文档站历史镜像（CN `docs/docs/history.md` + EN `docs/i18n/en/docusaurus-plugin-content-docs/current/history.md`）之外的脏文件**不再报错**，`apply` 只列出它们，**不会**自动提交；这些改动是否进本次发版，由你在第 4 步显式 `git add` 决定。",
            ),
            (
                "`git add -A` 会把它们全部打包进同一个发布提交。若 `plan` / `apply` 列出的待提交清单里有**明显不该进这次发版的文件**（临时调试产物、无关大改动），在确认时向用户点出，由用户拍板是否继续。",
                "这些改动**不会**自动进提交。只有用户逐项确认后，才把对应路径显式 `git add`；若清单里有**明显不该进这次发版的文件**（临时调试产物、别人正在改的文件），在确认时向用户点出，默认**不提交**。",
            ),
            (
                "→ `git add -A` → `git commit -F /tmp/release-msg.txt`",
                "→ `git add <用户确认的路径>` → `git commit -F /tmp/release-msg.txt`",
            ),
            (
                "`git add -A` → `git commit` → `python3",
                "`git add <用户确认的路径>` → `git commit` → `python3",
            ),
            (
                "- **工作树不干净不中断**：待提交改动会与 HISTORY.md 一起用 `git add -A` 打进同一个发布提交。仅当 `plan` / `apply` 列出的清单里出现**明显不该进这次发版的文件**（临时调试产物、无关大改动）时，在确认环节向用户点出，由用户决定是否继续。",
                "- **工作树不干净不中断，但绝不 `git add -A`**：脏文件里只有用户逐项确认的才进发布提交，其余（尤其并发 session 的在飞改动）**留在工作树**。清单出现不该进发版的文件时，在确认环节点出，默认不提交。",
            ),
        ],
    },
]


def parse_args() -> argparse.Namespace:
    ap = argparse.ArgumentParser()
    ap.add_argument("--src", required=True)
    ap.add_argument("--out", default=None)
    ap.add_argument("--check", action="store_true")
    return ap.parse_args()


def strip_frontmatter(lines: list[str]) -> list[str]:
    if lines and lines[0].strip() == "---":
        for i in range(1, len(lines)):
            if lines[i].strip() == "---":
                return lines[i + 1 :]
    return lines


def cut_tail(lines: list[str], markers: list[str]) -> list[str]:
    for marker in markers:
        for i in range(len(lines) - 1, -1, -1):
            if lines[i].startswith(marker):
                return lines[:i]
    return lines


def apply_line_edits(lines: list[str], edits: list[tuple[int, int, str]]) -> list[str]:
    # 从后往前改，避免行号漂移
    for start, end, new in sorted(edits, key=lambda e: -e[0]):
        lines = lines[: start - 1] + new.splitlines() + lines[end:]
    return lines


def main() -> int:
    args = parse_args()
    src_root = Path(args.src).resolve()
    out_root = Path(args.out) if args.out else Path(__file__).resolve().parent.parent / "modules"
    failed = False

    for mod in MODULES:
        source = src_root / mod["src"] / "SKILL.md"
        if not source.exists():
            print(f"SKIP {mod['src']}: 源文件不存在")
            continue
        raw = source.read_text(encoding="utf-8").splitlines()
        # 行编辑用「原始文件行号」，必须先于剥 frontmatter 应用，否则整体偏移
        lines = apply_line_edits(raw, mod["line_edits"])
        lines = strip_frontmatter(lines)
        lines = cut_tail(lines, mod["cut"])
        while lines and not lines[0].strip():
            lines = lines[1:]
        if lines and lines[0].startswith("# "):
            lines = lines[1:]
        body = "\n".join(lines).strip("\n")

        for old, new in mod["repl"]:
            occurrences = body.count(old)
            if occurrences != 1:
                print(f"  !! {mod['dest']}: 替换命中 {occurrences} 次（期望 1）：{old[:50]}…")
                failed = True
                continue
            body = body.replace(old, new)

        header = f"# {mod['title']}\n\n"
        if mod["banner"]:
            header += mod["banner"] + "\n"
        out = header + body + "\n"

        # 守卫：允许「禁止 / 绝不 `git add -A`」这种禁令表述，其余一律视为残留
        for line in out.splitlines():
            if "git add -A" in line and "禁止" not in line and "绝不" not in line:
                print(f"  !! {mod['dest']}: 残留未加禁止语义的 `git add -A`：{line.strip()[:60]}")
                failed = True

        if not args.check:
            (out_root / mod["dest"]).parent.mkdir(parents=True, exist_ok=True)
            (out_root / mod["dest"]).write_text(out, encoding="utf-8")
        print(f"  {'CHECK' if args.check else 'WROTE'} {mod['dest']:<28} {len(out.splitlines()):>4} 行  ← {mod['src']}")

    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
