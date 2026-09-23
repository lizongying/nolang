---
name: nolang-release
description: 给当前 git 仓库发版——检测工作树是否干净（不干净就中断交还给用户）、根据已有 tag 推导版本号（默认末位 +1）、在仓库根 HISTORY.md 顶部新增英文变更日志条目（GitHub Actions 会读取它作为 Release 正文）、打 tag，最后在用户确认后才执行 git push。当用户说「publish」「发版」「发布」「release」「打个 tag」「发 v0.3.0」或提到版本号与 HISTORY.md 时使用。
agent_created: true
---

# nolang-release

## 目的

把「发版」这件容易出错的事固化为固定流程：推导版本号 → 写 HISTORY.md → **停下来问用户** → 打 tag 并推送。

**不绑定任何具体仓库**：仓库根、remote、分支都由脚本在运行时用 `git rev-parse --show-toplevel` /
`git remote get-url origin` 探测，脚本从仓库内任意子目录执行都可以。只对「仓库根有 `HISTORY.md`
+ `history.sh`、push tag 触发 CI」这一类布局有效；换仓库只需改脚本里的 `HISTORY_FILE` 常量。

关键机制：GitHub Actions 在 tag push 时**不带参数**执行 `history.sh`，它会抓取 HISTORY.md 中
**第一个 `## ` 段落**作为 Release 正文。所以新条目必须插在最顶部，且标题要与 tag 完全一致。
详见 `references/release-pipeline.md`。

## 何时使用

- 用户说 `publish`、`发版`、`发布`、`release`、`打个 tag`、`发个版本`
- 用户直接给版本号（完整或残缺都可），如 `发版 v0.3.0`、`publish 0.3`、`发 1`
- 用户要求更新/补写 HISTORY.md 的发布条目

下文把本 skill 所在目录记作 `<skill-dir>`（即这份 SKILL.md 所在的目录）。
所有命令都在**目标 git 仓库内**执行，无需 `cd` 到根目录。

## 工作流

### 0. 前置检查：工作树必须干净（不干净就中断）

`plan` 会先跑 `git status --porcelain`。**只要有任何未提交改动，它就打印清单并以 exit code 2 退出。**

此时**立刻中断整个发版流程**，把清单转给用户，请用户自己先提交（或 stash），然后重新发起发版。
不要替用户提交——选哪些文件、拆成几个 commit、写什么 message 都需要人的判断，
仓库里可能还混着并行 session 的未提交改动，机器无从判断归属。

不在这一步代劳，也不要「先暂存起来发完版再恢复」。中断就是中断。

### 1. 解析版本

```bash
python3 <skill-dir>/scripts/release.py plan [--version vX.Y.Z]
```

只读，输出：仓库根、当前分支、origin、工作树状态、上一个 tag、本次 tag、
`commits since <last tag>`、待执行命令（含发布页 URL，由 origin 推出）。
不带 `--version` 时按惯例**末位 +1**（`vX.Y.Z` → `vX.Y.(Z+1)`）。

用户给的版本号可以是**不完整的**，脚本会自动补全成 `vMAJOR.MINOR.PATCH`，并在输出里打一行 `note:` 说明补全结果：

| 用户输入 | 实际使用 |
| --- | --- |
| `0.2.33` / `v0.2.33` | `v0.2.33` |
| `0.3` / `v0.3` | `v0.3.0` |
| `1` / `v1` | `v1.0.0` |

**给用户看的版本号一律用补全后的完整形式**（`v0.3.0`，不是 `0.3`），包括确认信息、
HISTORY.md 标题、tag 名和 commit message。

### 2. 起草变更日志（英文、conventional commit 风格）

以 `plan` 输出的 `commits since <last tag>` 为依据归纳条目。**正文一律用英文**，格式严格为：

```
- feat(mir): implement tagged enums and fix option unpacking
- fix(checker): resolve self in flattened method call receivers
- docs(syntax): document enum and struct field annotations
```

规范：

- 每条 `- <type>(<scope>): <description>`；type 取自
  `feat|fix|refactor|perf|docs|test|build|ci|chore`；scope 沿用 `plan` 输出里已有提交的模块名，别凭空编
- description 用**小写祈使动词**开头（add / fix / implement / remove …），句尾不加句号
- 合并同类项，把几十条提交压成 5–10 条读得懂的变更；不要把 commit 列表原样搬上去
- 只用 bullet，不要子标题；不要写中文

拿不准时把草稿给用户看，别自己拍板。

### 3. 写入 HISTORY.md

```bash
python3 <skill-dir>/scripts/release.py apply --version vX.Y.Z --body-file /tmp/notes.md
```

（`--body-file` 省略则从 stdin 读；加 `--dry-run` 只打印结果不落盘。）

脚本会挡住三类问题：重复版本、空正文、以及**非英文 / 不符合 conventional 格式的条目**
（exit 3，逐行报 offending line）。看到 exit 3 就按提示重写，不要用 `--allow-non-english` 绕过。
写完后脚本还会再查一次工作树：若除 HISTORY.md 之外还有别的脏文件，报 exit 2 并阻止提交。

写完立刻验证 CI 视角：

```bash
python3 <skill-dir>/scripts/release.py verify
```

输出必须正好是刚写的那一段。若打印的是旧版本内容，说明插入位置错了，先修再继续。

### 4. 停下来，向用户确认（强制）

在动手 push 之前，把下面三样一次性列给用户并等明确答复：

1. 版本号（含上一个 tag，便于核对）
2. HISTORY.md 新增段落全文
3. 将依次执行的命令：
   ```bash
   git add -A
   git commit -m "chore(release): vX.Y.Z"
   git tag vX.Y.Z
   git push origin main
   git push origin vX.Y.Z
   ```

此时工作树里理论上只剩 HISTORY.md 一个改动（`apply` 已校验过）。
`git add -A` 只是为了省事，若提交前 `git status` 冒出别的路径，停下来问用户。

**未获确认前，不得执行 add / commit / tag / push 中的任何一步。**

### 5. 确认后执行（每步前先重查真实状态）

仓库里可能有并行 session 在动 git。**不要假设几分钟前看到的状态还是当前状态**，
在提交前和推送前各查一次：

```bash
git status --porcelain            # 应该只有 M HISTORY.md
git log -1 --oneline              # HEAD 是否是预期的最后一个业务提交
git tag --list vX.Y.Z             # 本地 tag 是否已存在
git ls-remote --tags origin vX.Y.Z   # 远端 tag 是否已存在
```

据此分三种情况处理：

- **正常**：`git add -A` → `git commit -m "chore(release): vX.Y.Z"` → `git tag vX.Y.Z`
  → `git push origin <branch>` → `git push origin vX.Y.Z`
- **commit 已存在**（`git commit` 回 `nothing to commit`）：说明别处已经提交了。
  不要重试、不要 `--amend`，直接核对 `git log -1 --stat` 确认内容对得上，然后只补 `git tag` 和 push。
- **tag 已存在**（本地或远端）：停下来告诉用户，改用更高的版本号重新走流程。**不要** `-f` 覆盖已有 tag。

顺序固定：先提交、再 `push origin <branch>`、最后 `push origin <tag>`。
（分支名以 `plan` 输出的 `branch:` 为准，不要写死 `main`。）
只推 tag 不推分支会导致 CI checkout 拿不到 HISTORY.md 更新，Release 正文会出错。

推送后核对远端确实收到（`git ls-remote origin refs/tags/vX.Y.Z`），
再告知用户 CI 已触发，并把 `plan` 输出的发布页 URL 给出去便于跟进。

## 硬性规则

- **工作树不干净就中断**：把未提交清单交给用户，等用户自己提交完再从头走流程。代提交是禁区。
- 不确认不推送。版本号、日志内容、命令三者都要用户点头。
- 只改写 `HISTORY.md` 一个文件的内容；不要顺手改代码、不要改版本号常量（版本由 CI 用 ldflag 注入）。
- HISTORY.md 正文**只写英文**，且必须是 `- type(scope): description`；中文条目直接重写，不要绕过 lint。
- 新条目永远是 HISTORY.md 中第一个 `## ` 段落。
- 已在远端存在的 tag 不要复用；脚本会拒绝，此时改用更高的版本号并告知用户。永不 `-f` 覆盖 tag。
- **提交前与推送前各重查一次真实状态**（`git status` / `git log -1` / `git tag --list`）——
  仓库常有并行 session，`git commit` 报 `nothing to commit` 就是它已经替你提交了。
- commit message 用 `chore(release): vX.Y.Z`。
- 若用户只要「写日志、先别发」，执行到第 3 步为止，明确说明尚未提交、未打 tag。

## 退出码

| code | 含义 | 应对 |
| --- | --- | --- |
| 0 | ok | 继续 |
| 2 | 工作树不干净 / 出现预期外的脏文件 | 交给用户处理，流程中断 |
| 3 | 日志正文非英文或格式不合规范 | 重写条目 |

## 参考

- `references/release-pipeline.md` —— CI 细节、`history.sh` 行为、回滚 tag 的方法
- `scripts/release.py` —— `plan` / `apply` / `verify` 三个子命令，`plan` 与 `verify` 只读
