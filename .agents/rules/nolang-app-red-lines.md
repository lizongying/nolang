---
description: Nolang 应用红线：写 / 改 .no 代码的语言禁令（索引写 / 空串 / 越界读 / free / 协程 / match 绑定 / overflow / 全局命名 / guard vs while / 格式），含共用 Git / 路径 / 黄金法则。写 example/、tests/ 或业务 .no 代码前必读。
alwaysApply: false
enabled: true
---

# Nolang 应用红线（Application Red Lines）

> 总入口：`.agents/rules/nolang-red-lines.md`（`alwaysApply`，含任务路由）。
> 本文件含共用规则全文，单独加载即可开工；与总入口、编译器红线的共用节同步维护。
> **权威顺序**：实测 > `AGENTS.md`（语法与编码权威）> 本规则族 > 记忆笔记。
> 语法细节查 `AGENTS.md` / `nolang-core` skill，名词查 `./glossary/dist/glossary`。

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

## 4. 语言禁令（写 .no 前必读）

- 🔴 **索引写全域禁用**：`s[i] = v` / `t[i] = c`（str / txt）。变长用 `push` 家族
  （`str.push(c)` / `str.push-str(s)`；`txt.push` / `push-str` / `push-txt`），原位覆写用 `fill` 或
  place-match，逐字节构造用 `[]byte` + `to-str`。
- 🔴 **空 `str` 的 `data == NULL`**：`eq(x, n)` 在 `n > 0` 直接崩（含 `str.slice` 提前 return 的空视图）
  ⇒ 空串断言一律 `n = 0`。
- 🔴 `t[i]` / `s[i]` **越界读是 `err` 不是 `-1`**：断言 tag，不要断言兜底值。
- 🔴 不写 `free`（deferred move + 作用域退出自动销毁）；协程只能写成 `awy { r = co f() }`，`co` 组外非法
  （`E_CO_OUTSIDE_GROUP`）；`go` **不是**关键字，`run` 已移除，`awy <handle>` 已废除。
- 🔴 match 各臂绑定变量**不得同名**（不同类型同名会静默串型，checker 不报）；`?t` 解包优先用 `it`。
- 🔴 `#{overflow=...}` **只作用于下一行陈述**（L3），挂在函数头 / 块头上对 codegen 无效。
- 🔴 **顶层数据绑定（全局/常量）按首字符四档**（`build/global_case.go` / `build/module_check.go`，
  訊息含原因 + 改名建議）：
  - `BUF` 大寫 = **全局**：跨函式 / 跨模組都合法。
  - `buf` 小寫 = **局部**：被函式引用 → 硬錯誤（改 `Buf`）；跨模組 `mod.buf` 也硬錯誤。純 main 局部合法。
  - `_BUF` = **私有全局**：**同模組跨函式讀寫合法**（`_` 是「模組私有」標記，不是「不可被函式引用」）；
    只有**跨模組** `mod._BUF` 訪問 → 硬錯誤（去底線改 `BUF`）。
  - `_buf` = **私有局部**：Nolang **不開放**，無條件硬錯誤（改 `Buf` 或去前綴）。單獨 `_` 是通配綁定，放行。
  - **函數名、方法名、接口名、結構體名不受限**，可小寫 / `_` 開頭（如 `foo`、`_helper`、`_rsa-bn-add`）。
    別把合法的小寫函數名 / main 局部 / `_` 通配誤判成錯誤。
- 🔴 `with-len(0)` 这类 call-init 容器**只有大写名才注册为 module global**；小写名即使在同模块函数内
  `push` 也会失败（`vec.push: needs receiver and element`）。跨函数共享的数据一律大写。
- 🔴 `(cond) { }` 是**条件循环（while）**，`(cond) -> { }` 才是一次性守卫（guard）。把 guard 写成
  `(cond) { }` 且 body 不改条件会**死循环**（noui 内核 `el-add` 曾因此卡死）；见
  `docs/docs/lang/syntax.md` 循环章节。
- 🔴 模块内避免全局可变状态。
- 每条语句独占一行（`;` 是注释标记，不是语句分隔符）；4 空格缩进；文件以单个换行结尾。

## 5. 应用侧验证

- 写完 `.no` 用 `./bin/no build <file>` / `./bin/no run <file>` / `./bin/no test <file>` 验证；
  临时产物写项目 `tmp/`（见 §2）。
- 判据与避坑同编译器红线 §8：只认 `ALL TESTS PASSED` / `timed out` / `SOME TESTS FAILED`，
  其余按构建失败排查；未修缺陷见 `test/README.md §5.2`。
- 遇到疑似编译器缺陷，**不要改合法代码绕过**（见 §3 黄金法则）。
