#!/usr/bin/env python3
"""把 src/std 裡的分割線式 banner 改成 `; 標題` + `...`（佔位陳述）。

三種形態：
  1. 三行框        ; ═════        →  ; 標題
                   ; 標題            ...
                   ; ═════
  2. 單行帶標題    ; ─── 標題 ───  →  ; 標題
                                      ...
  3. 多行框        ; ═════        →  （內容）...
                   ; 說明…
                   ; 說明…
                   ; ═════

例外（只刪線、不加 `...`）：檔案開頭那個「模組 doc」框——模組 doc 由
parser.takeModuleDoc 從檔案首行取到無點錨點，插進 `...` 會把它截斷。

用法：python3 scripts/unnest-banners.py [--write]
"""
import pathlib
import re
import sys

RULE = re.compile(r'^;\s*([─═=━])\1{2,}\s*$')
TITLED = re.compile(r'^;\s*[─═=━]{3,}\s+(\S.*?)\s*[─═=━]{2,}\s*$')
ANCHOR_NODOT = re.compile(r'^;\s*\[[^\]]*\]\(#[^).]*\)\s*$')


def transform(lines):
    """返回 (新行列表, 統計)。"""
    out, stats = [], {"box": 0, "titled": 0, "module_head": 0, "lone": 0, "empty_pair": 0}
    i, n = 0, len(lines)
    while i < n:
        line = lines[i]
        m = RULE.match(line)
        if m:
            ch = m.group(1)
            # 找同一字元的配對結束線：中間只允許註解與空行
            j = i + 1
            while j < n and j - i <= 40:
                if lines[j].startswith(";"):
                    m2 = RULE.match(lines[j])
                    if m2 and m2.group(1) == ch:
                        break
                    j += 1
                    continue
                if not lines[j].strip():
                    j += 1
                    continue
                j = -1
                break
            if j != -1 and j < n and j - i <= 40:
                middle = lines[i + 1:j]
                # 模組 doc 框：開在檔首，或收線後緊接無點錨點 ⇒ 只刪線
                is_module_head = (i == 0) or (
                    j + 1 < n and ANCHOR_NODOT.match(lines[j + 1])
                )
                if not middle:
                    # 兩條線緊挨著（noml.no 2140 那類殘留）：純噪音，直接丟。
                    stats["empty_pair"] += 1
                    i = j + 1
                    continue
                out.extend(middle)
                if is_module_head:
                    stats["module_head"] += 1
                else:
                    out.append("...")
                    stats["box"] += 1
                i = j + 1
                continue
            stats["lone"] += 1
            i += 1
            continue
        mt = TITLED.match(line)
        if mt:
            out.append("; " + mt.group(1))
            out.append("...")
            stats["titled"] += 1
            i += 1
            continue
        out.append(line)
        i += 1
    return out, stats


def main():
    write = "--write" in sys.argv
    total = {}
    for p in sorted(pathlib.Path("src/std").rglob("*.no")):
        raw = p.read_text(encoding="utf-8")
        lines = raw.split("\n")
        new, stats = transform(lines)
        if new != lines:
            for k, v in stats.items():
                total[k] = total.get(k, 0) + v
            if not write:
                d = len(lines) - len(new)
                print(f"{p}: 行數 {len(lines)} -> {len(new)} ({-d:+d})  {stats}")
            else:
                p.write_text("\n".join(new), encoding="utf-8")
    print("總計:", total)


if __name__ == "__main__":
    main()
