---
name: nolang-core
description: Nolang 编译器与 .no 语言的统一入口（原 nolang-syntax / nolang-std / nolang-build / nolang-vet / nolang-debug / nolang-memory / nolang-builtin / nolang-path-resolution / nolang-selfhost-perf / nolang-docs-i18n / nolang-release 十一个 skill 合并而来，旧名仍可作为触发词）。内部按「子模块开关」按需加载：语言语法、标准库 API、make 构建、no vet 验收、编译器排错、内存与所有权、内建函数注册、路径解析约定、自举性能、英文文档 i18n、发版流程。当用户写或修改 .no 文件、查 Nolang 语法/类型/注解/FFI、查 std 模块签、构建 bin/no、跑 no vet 或 no test、修 parser/fmt/LSP 缺陷、排查 double-free 或内存泄漏、新增内建函数、处理 embed/import 路径与缓存键、优化自举编译器性能、同步英文文档、打 tag 发版时使用。
---

# nolang-core

Nolang 是**无 GC、内存安全、语法极简**的系统语言：deferred move + 作用域退出自动销毁，禁止手动 `free`；
编译器以 Go 实现，流水线 `lexer → parser → checker → hir → mir → transpiler(LLVM IR) → opt → llc → clang`。

**权威顺序**：实测 > `AGENTS.md`（语法与编码权威）> `.agents/rules/nolang-red-lines.md`（红线总入口，按任务路由到编译器 / 应用两份）> 本 skill > 记忆笔记。

## 加载协议

本 skill 的正文按**开关**分档，不要整目录读。三步：

1. **先看开关表**，`default: true` 的模块（`build`、`verify`）默认就要走——它们是日常循环的固定部分。
2. **命中任务的关键词才加载**：一次只载命中的那几个，命中多个就先载 `requires` 链上的。
3. **子视图再挑一档**：`syntax/` 与 `std/` 各有 7～10 个视图，按 trigger 选一两个，**不要整目录读**。

单次加载正文上限 **1200 行**；快超了就只读路由（`m10-syntax` / `m20-std`），正文按需追加。

常见组合：

| 任务 | 加载 |
|---|---|
| 写 / 改 `.no` | `build` + `verify` + `syntax/` 对应视图 |
| 改 std 的 API | `build` + `verify` + `std/` 对应视图（+ `syntax/10-data-types`） |
| 修编译器缺陷 | `debug` + `build` + `verify`（内存相关再加 `memory`，注意已隔离） |
| 加内建函数 | `builtin` + `std/` + `build` + `verify` |
| 发版 | `build` + `verify` + `release`（执行前必须用户确认） |

## 开关表

<!-- BEGIN:switch-table (由 scripts/core.py gen 生成，勿手改) -->
| 模块 | 默认 | 触发（任一命中即载） | 依赖 / 约束 | 篇幅 |
|---|---|---|---|---|
| `redlines` | inline | — 无条件内联 | 权威入口见 `.agents/rules/nolang-red-lines.md`（按任务路由到编译器 / 应用两份） | 51 行 |
| `build` → [`m30-build.md`](modules/m30-build.md) | `true` 常驻 | `make`、`build`、`bin/no`、`stdsig`、`构建`、`编译` | — | 93 行 |
| `verify` → [`m40-verify.md`](modules/m40-verify.md) | `true` 常驻 | `vet`、`验收`、`ERROR` | requires `build` | 146 行 |
| `syntax` → [`m10-syntax.md`](modules/m10-syntax.md) | `auto` 按需 | `.no`、`语法`、`syntax`、`parser`、`fmt`、`类型` …（12 项） | — | 82 行 |
| `std` → [`m20-std.md`](modules/m20-std.md) | `auto` 按需 | `std/`、`标准库`、`API`、`函数签名`、`模块` | — | 153 行 |
| `cli` → [`m93-cli.md`](modules/m93-cli.md) | `false` 点名 | `no build`、`no run`、`no test`、`no install`、`workspace`、`package.noml` …（7 项） | requires `build` | 380 行 |
| `debug` → [`m50-debug.md`](modules/m50-debug.md) | `false` 点名 | `bug`、`调试`、`debug`、`复现`、`诊断`、`LSP` …（7 项） | requires `build` + `verify` | 461 行 |
| `builtin` → [`m70-builtin.md`](modules/m70-builtin.md) | `false` 点名 | `builtin`、`内建`、`src/builtin` | requires `std` | 312 行 |
| `path-resolution` → [`m80-path-resolution.md`](modules/m80-path-resolution.md) | `false` 点名 | `path`、`embed`、`缓存键`、`tokenCache`、`stale` | — | 93 行 |
| `selfhost-perf` → [`m90-selfhost-perf.md`](modules/m90-selfhost-perf.md) | `false` 点名 | `性能`、`perf`、`O(N²)`、`自举`、`compiler/src` | requires `syntax` | 126 行 |
| `docs-i18n` → [`m91-docs-i18n.md`](modules/m91-docs-i18n.md) | `false` 点名 | `i18n`、`英文文档`、`翻译`、`docs/i18n` | — | 142 行 |
| `release` → [`m92-release.md`](modules/m92-release.md) | `false` 点名 | `发版`、`release`、`publish`、`tag`、`HISTORY.md` | requires `build` + `verify`；**执行前必须用户确认**；冲突时以 `redlines.git` 优先 | 225 行 |
| `memory` → [`m60-memory.md`](modules/m60-memory.md) | `false` ⚠️ 隔离 | `free`、`double-free`、`泄漏`、`所有权`、`move`、`clone` …（7 项） | requires `syntax`；加载必须先打失效横幅 | 891 行 |

子视图（父模块命中后，再按 trigger 挑一档，**不要整目录读**）：

| 子视图 | 触发 | 篇幅 |
|---|---|---|
| [`10-data-types.md`](modules/syntax/10-data-types.md) | `类型`、`变量`、`命名`、`代码风格`、`格式化`、`str`、`字符串`、`转换` | 888 行 |
| [`20-functions-control.md`](modules/syntax/20-functions-control.md) | `函数`、`fn`、`循环`、`for`、`while`、`return`、`多赋值` | 698 行 |
| [`30-match-branch.md`](modules/syntax/30-match-branch.md) | `match`、`it`、`option`、`解包`、`if`、`else` | 502 行 |
| [`40-structs-enums.md`](modules/syntax/40-structs-enums.md) | `struct`、`enum`、`接口`、`interface`、`泛型`、`slice`、`索引`、`方法` | 822 行 |
| [`50-modules-imports.md`](modules/syntax/50-modules-imports.md) | `import`、`use`、`模块`、`package.noml`、`导出`、`export` | 510 行 |
| [`60-annotations-ffi.md`](modules/syntax/60-annotations-ffi.md) | `#{`、`注解`、`FFI`、`#{c}`、`overflow`、`embed`、`运算符` | 461 行 |
| [`70-runtime-async.md`](modules/syntax/70-runtime-async.md) | `async`、`co`、`协程`、`并发`、`JS`、`wasm`、`browser` | 164 行 |
| [`10-base.md`](modules/std/10-base.md) | `types`、`option`、`布局` | 52 行 |
| [`20-core.md`](modules/std/20-core.md) | `fmt`、`math`、`str`、`number`、`num`、`int`、`float`、`byte` | 336 行 |
| [`30-os-io.md`](modules/std/30-os-io.md) | `os`、`fs`、`file`、`env`、`args`、`path`、`bufio`、`io` | 483 行 |
| [`40-net.md`](modules/std/40-net.md) | `net`、`http`、`tcp`、`tls`、`ws`、`websocket`、`quic`、`dns` | 631 行 |
| [`50-time-log.md`](modules/std/50-time-log.md) | `time`、`log`、`时间`、`日志` | 108 行 |
| [`60-data-structs.md`](modules/std/60-data-structs.md) | `set`、`map`、`deque`、`heap`、`stack`、`queue`、`tree`、`collection` | 326 行 |
| [`70-db-encode.md`](modules/std/70-db-encode.md) | `database`、`sql`、`hex`、`base64`、`csv`、`pem`、`tar`、`zip` | 232 行 |
| [`80-crypto.md`](modules/std/80-crypto.md) | `crypto`、`aes`、`rsa`、`sha`、`md5`、`hmac`、`rand`、`hash` | 326 行 |
| [`85-exchange.md`](modules/std/85-exchange.md) | `json`、`toml`、`yaml`、`dom`、`xml` | 358 行 |
| [`90-misc.md`](modules/std/90-misc.md) | `unicode`、`uuid`、`bigint`、`err`、`async`、`magic` | 201 行 |

预算：单次加载正文上限 **1200 行**；超限时先只读路由，正文按需追加。
<!-- END:switch-table -->

## 冲突裁决（合并时定下的口径）

原十一个 skill 之间有重复与相互矛盾的表述，合并时按下面这张表裁定，**不要再按旧文档的说法执行**：

| 冲突 | 裁定 |
|---|---|
| `no vet` 规则在 build / vet / debug / memory / builtin **五处**重复且措辞不一 | [`m40-verify.md`](modules/m40-verify.md) 是唯一来源，其余四档只留指针 |
| 黄金法则（不改合法代码"修" bug）在 syntax / debug / 红线三处 | 只留 [`m00-redlines.md`](modules/m00-redlines.md) |
| `nolang-memory` 描述的是**已删除**的 legacy LLVM 后端（`src/build/llvm/`，c7febdb） | [`m60-memory.md`](modules/m60-memory.md) 标 `quarantined`：默认关 + 加载先打横幅；活实现在 `src/mir/` |
| `nolang-release` 用 `git add -A` 打包整棵脏树 vs 红线「非本任务改动不碰」+ 并发 session | **禁 `git add -A`**；发布提交只含 `HISTORY.md` + 文档站镜像 + 用户逐项确认的路径 |
| `#{overflow}` 措辞两处（语法机制 / 红线禁令） | 机制归 [`syntax/60-annotations-ffi.md`](modules/syntax/60-annotations-ffi.md)，禁令归红线 |
| String 操作在 syntax 与 std 各有一份 | **语义**归 `syntax/10-data-types.md`，**API 面**归 `std/20-core.md`，交叉处只留指针 |
| 语言语义 vs std API 的整体边界 | `syntax/` 掌语言语义（类型规则、运算符、注解、布局），`std/` 只掌 API 面 |

## 退役的旧 skill

十一个旧目录已移出 `skills/`（内容全部迁入本 skill，git 历史仍可 `git show <rev>:<旧路径>`）。
旧名保留为**触发词**，说 "用 nolang-std 查一下" 等价于加载 `std`。

| 旧 skill | 现在的位置 |
|---|---|
| nolang-syntax | [`m10-syntax.md`](modules/m10-syntax.md) + `syntax/` 七视图；CLI 部分独立为 [`m93-cli.md`](modules/m93-cli.md) |
| nolang-std | [`m20-std.md`](modules/m20-std.md) + `std/` 十视图 |
| nolang-build | [`m30-build.md`](modules/m30-build.md) |
| nolang-vet | [`m40-verify.md`](modules/m40-verify.md)（收编 build / debug / memory / builtin 的重复条款） |
| nolang-debug | [`m50-debug.md`](modules/m50-debug.md) |
| nolang-memory | [`m60-memory.md`](modules/m60-memory.md)（quarantined） |
| nolang-builtin | [`m70-builtin.md`](modules/m70-builtin.md) |
| nolang-path-resolution | [`m80-path-resolution.md`](modules/m80-path-resolution.md) |
| nolang-selfhost-perf | [`m90-selfhost-perf.md`](modules/m90-selfhost-perf.md) |
| nolang-docs-i18n | [`m91-docs-i18n.md`](modules/m91-docs-i18n.md)（脚本 → `scripts/parity.py`） |
| nolang-release | [`m92-release.md`](modules/m92-release.md)（脚本 → `scripts/release.py`，说明 → `references/release-pipeline.md`） |

## 自检

```bash
python3 scripts/core.py check    # 开关表与配置是否漂移 + 路径存在 + 孤儿模块
python3 scripts/core.py links    # 内部相对链接是否全通
```

改了 `skill-config.noml` 之后跑 `python3 scripts/core.py gen` 重渲染开关表——**不要手改表**。
视图切分是机械的，行数守恒：`python3 scripts/split-views.py --src ../../../../tmp/skills-retired-2026-11-16 --check`
（需要退役副本仍在；副本已进 git 历史，可用 `git show` 取回）。
