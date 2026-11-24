---
description: Nolang 红线总入口（alwaysApply）：共用规则（Git 安全 / worktree / tmp / 二进制 / 黄金法则）+ 按任务路由到「编译器红线」「应用红线」两份子规则。改编译器或写 .no 应用代码前先按本入口选读子规则。
alwaysApply: true
enabled: true
---

# Nolang 硬红线（Red Lines）— 总入口

**权威顺序**：实测 > `AGENTS.md`（语法与编码权威）> 本规则族 > 记忆笔记。
本规则族只收录「违反了就会静默出错或不可逆丢东西」的条目；语法细节查 `AGENTS.md`，
std 未修缺陷查 `test/README.md §5.2`，名词查 `./glossary/dist/glossary`。

## 0. 按任务选读子规则

| 你在做什么 | 必读 |
| --- | --- |
| 改编译器 Go 代码（`src/lexer` / `parser` / `checker` / `hir` / `mir` / `build` / `fmt` / `lsp` / `cmd`）、改 `src/std/**`、自举、跑验收 | `.agents/rules/nolang-compiler-red-lines.md` |
| 写 / 改 `.no` 应用代码（`example/**`、`tests/**`、业务项目）、调用 std API、使用语言特性 | `.agents/rules/nolang-app-red-lines.md` |

两份子规则**各自完整**（都含下方共用规则），单独加载任意一份即可开工。
下方共用规则与两份子规则保持同步；修改时三处一起改。

## 1. Git：只读回看，绝不丢工作区

- 🔴 **禁止** `git checkout` / `reset` / `restore` / `stash` / `clean` / `push --force`。
- 本仓库常有**多个并发 session** 同时改同一棵树，上述命令会静默覆盖别人的在飞改动。
- 回看旧版：`git show <rev>:<path>`、`git diff <rev> -- <path>`。要比对/实验用隔离 worktree，
  目录放项目内 `tmp/` 下并按 `{type}/{short-id}` 命名（见下方 worktree 约定），不写系统 `/tmp`。
- 动工前后各跑一次 `git status`；**不属于本任务**的已修改文件一律不碰、不「顺手修复」。

### Worktree 约定（防孤悬）

- 目录名**短、稳定、可机器识别**：`{type}/{short-id}` 或 `{ticket}/{short-tag}`，例如
  `feat/option-3state`、`fix/verify-rule`、`refactor/hir-rebuild`；统一挂在 `tmp/` 下，如
  `tmp/feat/option-3state`。
- 🔴 每个 worktree **必须带元信息**，否则就是「谁建的、目的、能不能清理」都不明的孤悬树。
  worktree 内放 `worktree-meta.noml`（集中索引示例路径 `.worktree-meta/feat-option-3state.noml`，
  把 `/` 换成 `-`），至少写清**一句话 desc、创建人、时间、原因**，可选工单/PR 链接：

  ```noml
  {
    name: 'feat/option-3state'
    creator: 'lizongying'
    created-at: '2026-11-21 00:00:00'
    desc: '重构 Option 三态 ok/nil/err 校验逻辑，验证 ?= 上抛分支规则，临时验证，不合并到主分支'
    link: 'https://xxx/issue/123'
  }
  ```

- 🔴 清理 worktree 前**先读 `worktree-meta.noml`**：只删自己建的；他人建的、或归属/目的不明的
  一律留着（与 `tmp/` 清理红线一致）。用完 `git worktree remove` 及时收掉自己的树，每棵 100M+。

## 2. 路径与二进制纪律

- 🔴 **临时构建 / scratch 写项目根 `tmp/`**：ad-hoc `no build -o …`、一次性编译探针、临时可执行、
  AST/IR dump、`no fmt -d` 输出，一律写项目内 `tmp/`，不写系统 `/tmp` 或 `$TMPDIR`（随时被清、
  跨项目权限/碰撞）。`bin/no` / `bin/lsp` 是 `make` 产出的正式二进制，是唯一例外。
- 🔴 清理 `tmp/` 时**只删自己创建的文件**；归属不明的一律留着。
- 裸 `no`（`/usr/local/bin/no`）与 `./bin/no` **不是同一个二进制**；排查「你绿我红」先比 `version`。
- 🔴 `bin/no` 陈旧 ⇒ 整片假象（绿/红都不可信）：行为诡异先比 mtime；exit 137 或零输出先
  `rm -f bin/no && make gen no`。

## 3. 黄金法则：不改合法代码来「修」疑似 bug

即使怀疑 parser / checker / codegen 有缺陷，也**只报告 + 最小复现**，不改写语法正确的代码。
确属编译器缺陷时，写 Go 测试钉住，报告时附「最小复现 + 期望/实际」。
