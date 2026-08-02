---
sidebar_position: 99
---

# 路径解析约定：工作区根目录基准（Workspace-Root-Relative）

## 约定（Adopted Convention）

所有 **embed 路径** 与 **导入路径**（`use` / `#` 指令、embed 资源）一律解析为
**以工作区根目录（workspace root）为基准的规范化绝对路径**，**不再以"当前源码文件所在目录"
作为相对基准**。

- "工作区根目录" = `workspace.jsonc` 所在目录，即 `pkg.WorkspaceRoot()`。
  注意：`package.jsonc` 所在目录是**包根目录**（`pkg.RootDir`），二者不是同一层——工作区根在上层（含 `workspace.jsonc`，可包含多个包），包根是具体**包**的目录（含 `package.jsonc`），包内包含若干**模块**（`.no` 源文件）。不要混淆。
- 解析后做规范化（`filepath.Clean`、去 `.`/`..`、统一分隔符），得到**全局唯一**的规范路径，
  作为模块加载、缓存键、AST/语义归属的唯一标识。
- 该规范路径是 `lexer.tokenCache` 与 `checker.parseProgramFileCache` 缓存键的**路径分量**。
  缓存键实际为复合键 `(规范路径, 内容哈希)`（`cache.Key`，见 `src/cache/lru.go`）：
  同路径内容变化 → 哈希变化 → 自动失效，不再拿到过期 token/AST；且缓存是有容量上限的
  LRU，常驻进程（LSP / fmt）不会内存泄漏（原两遍架构审查中的第③项）。

## 为什么

1. **从根源消除缓存重名碰撞**。旧的 token/AST 缓存键是"路径字符串"，当不同源文件以相同
   相对字符串（如 `utils/no` 或 embed 名 `data.json`）导入/嵌入时，会命中同一条缓存 →
   串味、拿到错误 token/AST。改为工作区根基准的规范绝对路径后，**每个逻辑文件有唯一键、
   同一文件恒等键**，碰撞在构造上消失（原两遍架构审查中的第④项）。
2. **编译器逻辑大幅简化、更健壮**。路径解析从"散落在 parser / transpiler / checker 各处的
   相对当前文件目录解析"收敛为**单一规范化步骤**；前向引用、模块加载、缓存键、诊断信息中的
   路径都基于同一基准，不再依赖导入方文件的位置。
3. **为跨文件 / 并行构建缓存铺路**。规范绝对路径使缓存键稳定且全局可比，便于后续做
   deep-clone 复用（避免重复 `ParseProgram`）与惰性解析优化。

## 适用边界

- **绝对路径**（以 `/` 开头的本地模块）：已相对工作区根解析，保持。
- **`std/` 模块**：来自内嵌 `StdFS`，key 为 `std/<rel>.no`，保持，不与本地路径混淆。
- **embed 资源**：无论以何种相对名引用，内部一律归一为工作区根基准的规范 embed key。

## 已实现（Implemented, 2026-08-02）

统一 helper 已落地于 `src/package/paths.go`（package `pkg`，纯标准库，无循环依赖）：

- `FindWorkspaceRoot(start)`：从 `start` 向上查找 `workspace.jsonc` 所在目录。
- `FindPackageRoot(start)`：从 `start` 向上查找 `package.jsonc` 所在目录（无 workspace 时的退路）。
- `ResolveToWorkspaceRoot(wsRoot, rel)`：把任意导入 / embed 原始路径规范化为工作区根基准的
  绝对路径（去前导 `/`、绝对路径原样返回、`wsRoot` 为空时退路当前目录）。
- `ResolveEmbedBase(sourcePath)`：相对 embed 路径的解析基准——工作区根优先，退路包根 / 源文件目录。

已收敛的入口：

- `src/build/transpiler.go` `resolveUse`：分支 A（`/`-前缀）与分支 E（alias）统一改为
  `pkg.ResolveToWorkspaceRoot(t.workspaceRoot(), path)`，并新增 `t.workspaceRoot()`
  方法（优先 `pkg.WorkspaceRoot()`，否则从 `sourcePath` 向上查 `workspace.jsonc`）。
  旧的 `if t.pkg != nil { baseDir = pkg.RootDir; if wsRoot... }` 重复逻辑被删除，cwd-相对
  退路改为走 `FindWorkspaceRoot`。
- `src/build/transpiler.go` `processEmbeds`：相对 embed 路径改走 `pkg.ResolveEmbedBase(sourcePath)`
  （工作区根优先）。
- `src/checker/checker.go` `ValidateEmbedAnnotations`：embed 校验路径同样改走 `pkg.ResolveEmbedBase`。

缓存键（`tokenCache` / `parseProgramFileCache`）经由 `resolveFile(filePath)` 获得工作区根基准的
规范绝对路径作为路径分量；再与源文件内容哈希拼成复合键（`cache.Key`），碰撞在构造上消失，
且同路径编辑后自动失效、LRU 有上限防泄漏。

`std/` 与 `js/` 内嵌模块仍走 `StdFS`/`JsFS`（key 为 `std/<rel>.no` / `js/<rel>.no`），不受
影响；`processEmbeds` 仅对主程序（用户源码）生效，std 模块自身 embed 仍相对其自身位置解析。

> 状态：**已实现（2026-08-02）**。新代码请直接使用 `pkg.ResolveToWorkspaceRoot` /
> `pkg.ResolveEmbedBase`，不要再以 `filepath.Dir(t.sourcePath)` 之类的"当前文件目录"
> 做相对解析。

## 术语对照（统一，2026-08-02）

- **workspace 工作区** —— 一般就是一个 repo。根目录放 `workspace.jsonc`，可包含多个包。工作区根是
  所有 embed/导入路径解析的唯一基准。
- **package 包** —— 一个大的编译单元：一个库或一个可执行文件。根目录放 `package.jsonc`（即**包根**）。
  声明依赖、emit 后端等。一个包包含若干模块。
- **module 模块** —— 一般就是一个源文件（`.no`）。当前约定 **一个文件 = 一个模块**。加载一个模块
  = 加载一个 `.no` 文件。

三者层级：**工作区 ⊃ 包（一个或多个） ⊃ 模块（一个或多个）**。不要再拿 "module" 去指"包"
（编译单元）——那是 "package" 的职责。
