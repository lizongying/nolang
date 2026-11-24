#!/usr/bin/env python3
"""nolang-core 的开关引擎。

子命令：
    gen      从 skill-config.noml 渲染开关表，写回 SKILL.md 的标记区间
    check    （默认）校验 SKILL.md 是否与配置漂移 + 路径是否存在 + 是否有孤儿模块
    links    校验本 skill 内所有相对 markdown 链接是否可达

设计要点：没有宿主会解析 skill-config.noml，所以「开关」是 in-band 协议 ——
SKILL.md 内嵌的表才是 agent 实际读到的内容。本脚本保证「配置 → 内嵌表」单向生成，
使配置保持唯一源（与 `make gen` → stdsig_gen.go 同一模式）。
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CONFIG = ROOT / "skill-config.noml"
ENTRY = ROOT / "SKILL.md"
BEGIN = "<!-- BEGIN:switch-table (由 scripts/core.py gen 生成，勿手改) -->"
END = "<!-- END:switch-table -->"

BARE = re.compile(r"[A-Za-z0-9_\-./#?<>=+*]+")


class Parser:
    """只覆盖本仓库 .noml 用到的那一小撮语法：{} [] 字符串 布尔 数字 裸词。

    注释只按 `;` 到行尾处理 —— 与 src/package/noml.go 的 .noml 方言一致
    （`;; ... ;;` 多行注释是 .no 词法器专有，配置文件刻意不用，以免两边解析结果不同）。
    """

    def __init__(self, text: str) -> None:
        self.s = text
        self.i = 0

    def skip(self) -> None:
        while self.i < len(self.s):
            c = self.s[self.i]
            if c in " \t\r\n,":
                self.i += 1
            elif c == ";":  # 单行注释
                while self.i < len(self.s) and self.s[self.i] != "\n":
                    self.i += 1
            else:
                return


    def parse(self):
        self.skip()
        return self.value()

    def value(self):
        self.skip()
        c = self.s[self.i]
        if c == "{":
            return self.obj()
        if c == "[":
            return self.arr()
        if c == "'":
            return self.string()
        m = BARE.match(self.s, self.i)
        if not m:
            raise ValueError(f"无法解析位置 {self.i}: {self.s[self.i:self.i + 20]!r}")
        self.i = m.end()
        word = m.group(0)
        if word == "true":
            return True
        if word == "false":
            return False
        if re.fullmatch(r"-?\d+", word):
            return int(word)
        return word

    def string(self) -> str:
        self.i += 1
        out = []
        while self.s[self.i] != "'":
            if self.s[self.i] == "\\":
                self.i += 1
            out.append(self.s[self.i])
            self.i += 1
        self.i += 1
        return "".join(out)

    def obj(self) -> dict:
        self.i += 1
        out = {}
        while True:
            self.skip()
            if self.s[self.i] == "}":
                self.i += 1
                return out
            key = self.string() if self.s[self.i] == "'" else BARE.match(self.s, self.i).group(0)
            if self.s[self.i] == "'":
                pass
            else:
                self.i = BARE.match(self.s, self.i).end()
            self.skip()
            if self.s[self.i] != ":":
                raise ValueError(f"期望 ':' 于 {self.i}")
            self.i += 1
            out[key] = self.value()

    def arr(self) -> list:
        self.i += 1
        out = []
        while True:
            self.skip()
            if self.s[self.i] == "]":
                self.i += 1
                return out
            out.append(self.value())


def load() -> dict:
    return Parser(CONFIG.read_text(encoding="utf-8")).parse()


def render(cfg: dict) -> str:
    mods = cfg["modules"]
    order = ["build", "verify", "syntax", "std", "cli", "debug", "builtin",
             "path-resolution", "selfhost-perf", "docs-i18n", "release", "memory"]

    def state(m: dict) -> str:
        if m.get("quarantined"):
            return "`false` ⚠️ 隔离"
        d = m.get("default")
        if d is True:
            return "`true` 常驻"
        if d == "auto":
            return "`auto` 按需"
        return "`false` 点名"

    lines = [
        "| 模块 | 默认 | 触发（任一命中即载） | 依赖 / 约束 | 篇幅 |",
        "|---|---|---|---|---|",
        f"| `redlines` | inline | — 无条件内联 | 权威入口见 `.agents/rules/nolang-red-lines.md`（按任务路由到编译器 / 应用两份） | {_size('modules/m00-redlines.md')} |",
    ]
    for name in order:
        m = mods[name]
        trig = m.get("trigger") or []
        trig_s = "、".join(f"`{t}`" for t in trig[:6])
        if len(trig) > 6:
            trig_s += f" …（{len(trig)} 项）"
        cons = []
        if m.get("requires"):
            cons.append("requires " + " + ".join(f"`{r}`" for r in m["requires"]))
        if m.get("confirm"):
            cons.append("**执行前必须用户确认**")
        if m.get("conflicts"):
            cons.append("冲突时以 " + " / ".join(f"`{c}`" for c in m["conflicts"]) + " 优先")
        if m.get("quarantined"):
            cons.append("加载必须先打失效横幅")
        path = ROOT / m["path"]
        lines.append(
            f"| `{name}` → [`{Path(m['path']).name}`]({m['path']}) | {state(m)} | "
            f"{trig_s or '—'} | {'；'.join(cons) or '—'} | {_size(m['path'])} |"
        )

    children = [(n, c) for n in ("syntax", "std") for c in (mods[n].get("children") or {}).items()]
    if children:
        lines += [
            "",
            "子视图（父模块命中后，再按 trigger 挑一档，**不要整目录读**）：",
            "",
            "| 子视图 | 触发 | 篇幅 |",
            "|---|---|---|",
        ]
        for _, (cname, c) in children:
            trig_s = "、".join(f"`{t}`" for t in (c.get("trigger") or [])[:8])
            lines.append(f"| [`{Path(c['path']).name}`]({c['path']}) | {trig_s or '—'} | {_size(c['path'])} |")

    lines += [
        "",
        f"预算：单次加载正文上限 **{cfg['budget']} 行**；超限时先只读路由，正文按需追加。",
    ]
    return "\n".join(lines)


def _size(rel: str) -> str:
    p = ROOT / rel
    if not p.exists():
        return "缺失"
    return f"{len(p.read_text(encoding='utf-8').splitlines())} 行"


def all_paths(cfg: dict) -> list[str]:
    out = ["modules/m00-redlines.md"]
    for m in cfg["modules"].values():
        out.append(m["path"])
        for c in (m.get("children") or {}).values():
            out.append(c["path"])
    return out


def cmd_gen(cfg: dict) -> int:
    body = render(cfg)
    text = ENTRY.read_text(encoding="utf-8")
    if BEGIN not in text or END not in text:
        print(f"ERROR: SKILL.md 缺少标记 {BEGIN!r} / {END!r}")
        return 1
    new = re.sub(
        re.escape(BEGIN) + r".*?" + re.escape(END),
        lambda _: f"{BEGIN}\n{body}\n{END}",
        text,
        flags=re.S,
    )
    if new != text:
        ENTRY.write_text(new, encoding="utf-8")
        print("gen: SKILL.md 开关表已更新")
    else:
        print("gen: 无需改动")
    return 0


def cmd_check(cfg: dict) -> int:
    rc = 0
    text = ENTRY.read_text(encoding="utf-8")
    m = re.search(re.escape(BEGIN) + r"(.*?)" + re.escape(END), text, flags=re.S)
    if not m:
        print("ERROR: SKILL.md 缺少开关表标记")
        return 1
    if m.group(1).strip() != render(cfg).strip():
        print("ERROR: SKILL.md 的开关表与 skill-config.noml 漂移 —— 跑 `python3 scripts/core.py gen`")
        rc = 1
    else:
        print("check: 开关表与配置一致")

    for rel in all_paths(cfg):
        if not (ROOT / rel).exists():
            print(f"ERROR: 配置指向的文件不存在：{rel}")
            rc = 1

    referenced = {Path(p).resolve() for p in all_paths(cfg)}
    referenced.add(CONFIG.resolve())
    for f in sorted((ROOT / "modules").rglob("*.md")):
        if f.resolve() not in referenced:
            print(f"WARN: 孤儿模块（未被配置引用）：{f.relative_to(ROOT)}")
    print(f"check: {len(all_paths(cfg))} 个模块路径全部存在" if rc == 0 else "check: 有失败项")
    return rc


def cmd_links() -> int:
    bad = 0
    for md in sorted(ROOT.rglob("*.md")):
        for target in re.findall(r"\]\(([^)]+)\)", md.read_text(encoding="utf-8")):
            if target.startswith(("http://", "https://", "mailto:", "#", "file://")):
                continue
            path_part = target.split("#")[0]
            if not path_part:
                continue
            if not (md.parent / path_part).exists():
                print(f"BROKEN {md.relative_to(ROOT)} → {target}")
                bad += 1
    print("links: 全部可达" if not bad else f"links: {bad} 条断链")
    return 1 if bad else 0


def main() -> int:
    cmd = sys.argv[1] if len(sys.argv) > 1 else "check"
    cfg = load()
    if cmd == "gen":
        return cmd_gen(cfg)
    if cmd == "check":
        return cmd_check(cfg)
    if cmd == "links":
        return cmd_links()
    print(__doc__)
    return 2


if __name__ == "__main__":
    sys.exit(main())
