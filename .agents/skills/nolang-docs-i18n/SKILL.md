---
name: nolang-docs-i18n
description: 让英文 i18n 文档（docs/i18n/en/docusaurus-plugin-content-docs/current/**）追上中文源文档（docs/docs/**）——先用脚本找出滞后点（结构差异、缺失小节、未同步的提交），再逐段翻译补齐，最后验证结构与代码块对齐。当用户说「英文文档滞后」「翻译补齐」「同步英文 docs」「i18n 缺内容」「EN docs 没跟上」或指出某个 docs/i18n/en 文件缺小节时使用。
agent_created: true
---

# nolang-docs-i18n

## 目的

中文文档是唯一真相源，`docs/i18n/en/...` 是它的英文翻译。翻译会随时间滞后（新增小节没翻、
语言语法改了英文没跟），这份 skill 把「找出滞后 → 补齐 → 验证」固化成可重复流程。

关键认识：**滞后不只是「缺小节」，还包括「已有小节里的代码示例用的是被删掉的旧语法」**。
后者更危险——读者照着写会编译不过。

## 何时使用

- 用户说「英文 i18n 文档滞后」「翻译补齐」「同步英文文档」「EN docs 没跟上」
- 用户点名 `docs/i18n/en/...` 下某个文件缺内容
- 改了 `docs/docs/**` 之后要同步英文版

下文 `<skill-dir>` = 本 SKILL.md 所在目录。

## 工作流

### 1. 找出滞后点（先量，再改）

```bash
python3 <skill-dir>/scripts/parity.py                    # 全量扫描，只列有问题的文件
python3 <skill-dir>/scripts/parity.py --quiet            # 同上（默认已按文件分组）
python3 <skill-dir>/scripts/parity.py docs/docs/lang/syntax.md
```

脚本给三份互相独立的证据：

| 检查 | 含义 | 可信度 |
| --- | --- | --- |
| `structure` | 标题数量 / 层级序列 / h2 序列是否一致 | **权威**。数量不等 = 一定有整节缺失 |
| `code parity` | 逐个小节比对代码骨架（已剥掉注释与非 ASCII） | **权威**。唯一真相是「这节的代码在 EN 里找不到」 |
| `ZH commits never mirrored into EN` | 改了 ZH 却没碰 EN 的提交 | **只是线索**，不是结论 |

> ⚠️ 标题解析**只认围栏外的 `#`**（`parse_headings` 会跳过代码块）。否则代码块里的
> `# 构建当前目录` 这类 shell 注释会被当成标题——`usage.md` 曾因此报「ZH 82 vs EN 62」，
> 真实数字是 **43 vs 35**；而且每个假标题还会把一个小节切成两半，连带污染 `code parity`。
> 同理 `normalize()` 会剥掉 `#` 开头的注释，**但保留 `#{...}`**（那是真语法，不是注释）。

> ⚠️ commit 清单会包含**很老的提交**，它们的內容可能早就通过后来的某次整体翻译进入了 EN。
> 别照着清单去补代码——用 `code parity` / `structure` 的结果定稿。

`code parity` 会留少量**假阳性**，逐个肉眼裁定即可（下面「判读」一节）。

### 2. 逐段翻译补齐

对每个确认的缺口：

1. **读 ZH 源段落**（连前后文一起读，确认插入位置），再读 EN 的对应区域。
2. 用 `Edit` 把译文插到**与 ZH 相同的位置**。EN 里已有同主题小节但顺序不同时，
   以 ZH 的顺序为准（可以整节搬运）。
3. 保持 EN 的既有风格：
   - 标题用 Title Case（`## Raw String`、`### Safe Indexing`）
   - 代码围栏统一用 ```` ```no ````（ZH 个别小节里还留着 ```` ```nolang ````，EN 不要跟）
   - 代码注释翻译成英文，但**编译器/工具的原文报错信息保持原样**（它们本来就是中文输出）；
     必要时补一句 “the message text itself is emitted in Chinese”
   - 术语沿用 EN 既有译法（view / out-parameter / nullable type / catch-all …）
4. **不要顺手改别的文件**。用户点名哪个文件就只改哪个；发现别的文件也滞后，写进最终报告，
   不要擅自扩大范围（仓库里常同时有并行 session 在改别的文件）。

### 3. 验证

```bash
python3 <skill-dir>/scripts/parity.py docs/docs/lang/syntax.md
```

要求：

- `structure` 无输出（标题数、层级序列一致）
- `code parity` 只剩假阳性
- 顺手核对锚点与相对链接是否有效：

```bash
python3 - <<'PY'
import re
from pathlib import Path
p = Path('docs/i18n/en/docusaurus-plugin-content-docs/current/lang/syntax.md')
lines = p.read_text().splitlines()
heads = [re.match(r'^(#{1,6})\s+(.*)$', l) for l in lines]
anchors = {re.sub(r'[^a-z0-9 -]', '', m.group(2).strip().lower()).replace(' ', '-')
           for m in heads if m}
links = set(re.findall(r'\]\(#([^)]+)\)', '\n'.join(lines)))
print('missing anchors:', sorted(links - anchors))
print('file links:', sorted(set(re.findall(r'\]\((?!http|#)([^)]+)\)', '\n'.join(lines)))))
PY
```

`file links` 里的每个文件名都要在同目录存在（`str.md`、`module.md` …）。

## 判读：什么是假阳性

下面这些**不是**缺口，别去「修」：

- **代码里的字符串字面量被翻译过**：ZH `print('不會執行')` vs EN `print('will not execute')`、
  `#{doc = ''}` vs `#{doc = 'has value'}` —— 语义相同，`code parity` 会误报整节。
- **注释语言不同**：`# 构建当前目录` vs `# Build current directory`、`; 這是註釋` vs `// this is a comment`。
  归一化已剥 `;` / `//` / `#` 注释，但如果整节**只有**注释差异（尤其注释在 EN 里被写得更长）仍会命中。
- **EN 给 ASCII 示意行补了英文注解**：ZH 的目录树/判定流程代码块只写 `workspace/`、`workspace.jsonc`、
  `判定规则` 这种裸行，EN 会写成 `workspace/ top-level workspace`、`workspace.jsonc package name -> path mapping`。
  这类**成对 `-`/`+`** 全是假阳性（`usage.md` 的「判定规则 / 递归工作区映射 / 工作区流程」都是这个）。
- **模块清单注释被翻译并展开**：`### crypto/hkdf HKDF RFC 5869` → `### crypto/hkdf HKDF Key Derivation (RFC 5869)`。
  整份 `std/crypto.md` 曾因此报 3 节，实际 `--code-diff` 是 `identical code skeleton`。
- **EN 比 ZH 更啰嗦**：例如 EN 的文件命名示例多写了 `✅ Recommended:` / `Avoid:` 标签。
- **ZH 的 `[ -ld...]` vs EN 的 `[more -ld...]`** 之类的措辞微调。

裁定方法：先跑 `--code-diff` 把整份文件的代码骨架差异打出来，再逐行看语义。

```bash
python3 <skill-dir>/scripts/parity.py --code-diff docs/docs/std/global.md
```

- 输出 `identical code skeleton` → 该文件代码层面完全对齐。
- 差异里 `-` 与 `+` **成对出现且语义等价**（`out = pem.pem-encode(...)` vs
  `pem-str = pem.pem-encode(...)`；`s = ''.with-cap(256)` vs `s str = ''.with-cap(256)`）
  → 不是缺口，EN 只是把类型写得更显式、或换了示例字面量。
- 出现**只有 `-` 没有 `+` 的整段** → 真缺口。**这是唯一的硬信号**：
  `usage.md` 的 JS 后端整块就是一段连续的 `-`（`no build --js main.no`、`dom.create-element`、
  `storage.set-item`、`#{js}`、`#{js-browser}`…）而没有任何 `+` 对应。
- 注意 `code parity` 是**按小节**报的，所以「整节标 ALL」也可能只是该节唯一的代码块里
  有一个字面量不同。**`--code-diff` 是逐行视角，更适合裁定。**

## 已知陷阱

- **BRE 交替静默失效**：这个 shell 里 `grep 'a\|b'` 会静默返回空。**一律 `grep -E 'a|b'`**，
  或用 Grep 工具。曾因此误判「符号不存在」。
- **围栏配对会错位**：ZH/EN 里都有 `> ```no` 这种**引用块内嵌围栏**，而它的结束围栏
  写成了裸 ```` ``` ````（少了 `> `）。朴素地按 `startswith('```')` 配对会从那里开始整体错位一格，
  于是后面所有代码块都被解析成散文。脚本已经先 `re.sub(r'^>\s?', '', line)` 再判断。
  （这个少 `> ` 的写法在两个文件里一致，Docusaurus 能正常渲染，**不要去改**。）
- **别用「行数差」当指标**：中文段落单行很长，英文段落换行方式不同，行数差没有意义。
  看标题数和代码骨架。
- **`docs/build/**` 是构建产物**，不要改、也不要拿它当基准。
- 大文件整份重写很危险（几千行）。用 `Edit` 分段插入，每段都带足上下文保证 `old_string` 唯一。

## 参考

- `scripts/parity.py` —— 三合一滞后报告（只读，不改任何文件）
- ZH 源：`docs/docs/**`；EN 目标：`docs/i18n/en/docusaurus-plugin-content-docs/current/**`
