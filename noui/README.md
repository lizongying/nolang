# noui — 用 Nolang 重写 GPUI 精神内核

本目录（仓库根 `noui/`）是「用 Nolang 重写 GPUI」的**可运行纵切面**交付物。
2026-11-20 从 `tmp/noui/` 迁入仓库根目录，纳入版本管理。

## 已交付

- **两个可运行版本**：
  - `main.no`：原始纵切面（顶层主循环 + 标量全局）
  - `tests/main-layered.no`：**分层内核版**（Entity / Element / Scene 大写全局池 + 函数封装 + Scene 遍历绘制），窗口跑通
- 一个真正能开的原生窗口（SDL3，底层 Metal/Vulkan），显示计数器 UI：
  - 标题 + 动态计数值 + 两个按钮（`-` / `+`）
  - 鼠标 hover 按钮高亮、点击改数值、ESC 退出
- 平台层 C shim（`src/shim/shim.c`）：把 SDL3 的 C API 摊平成标量函数供 Nolang `#{c}` 调用
- 交互逻辑单元测试（`tests/interaction.no`）：10 项断言全绿
- **全目录组件库**（`src/components.no`）：gpui-kit（原 gpui-component）86 模块的 Nolang API 层，
  83 个组件 kind 全可构造，核心 10 个有完整交互状态机（见下）
- 架构文档（`ARCHITECTURE.md`）：GPUI 概念映射 + 实测约束 + 分步计划

## 组件库（gpui-kit 全目录）

`src/components.no` 按 gpui-kit 的 86 个源码模块建全 Nolang API：83 个唯一组件 kind，
统一 SoA 大写全局池 + 构造函数 + 通用 setter/getter；核心 10 个
（button / input / checkbox / switch / select / tab / dialog / tooltip / progress / slider）
有完整状态机（hover/click/focus/勾选/值域/键盘式切换）。清单与映射见 `src/components-catalog.md`。

```sh
# 全目录 smoke：83 个组件逐一构造 + setter 断言
../bin/no run -cc ./ccwrap.sh tests/components-smoke.no   # ALL COMPONENT SMOKE TESTS PASSED
# 核心 10 组件深度交互
../bin/no run -cc ./ccwrap.sh tests/components-core.no     # ALL CORE COMPONENT TESTS PASSED
```

> 本轮是「全目录 API + 核心扎实」档：DataTable 虚拟滚动、Dock 可拆面板、代码编辑器等
> 功能对等是长期项（当前只做 API + 最小状态）。实现中踩到的两个编译器坑记在
> `ARCHITECTURE.md §5.2`。

## 编译器修复（2026-11-20）

用户指出「全局变量必须大写」后定位到一个真实编译器缺陷并修复：
`BUF []i64 = with-len(0)` 这类**带 call 初始化的大写全局**未注册为 module global，
导致函数内 push/index 读到空存储（`vec.push: needs receiver and element`）。

修复在 `src/mir/hir2mir.go`（3 处）+ 回归测试（`src/mir/global_call_init_test.go`,
`compiler/tests/probe-glob-call-init.no`），`make test` 全绿。
这让分层内核（函数封装全局池）成为可能 —— `tests/main-layered.no` 即基于修复后的编译器。

一键运行分层版：
```sh
export DYLD_LIBRARY_PATH=/opt/homebrew/opt/sdl3/lib:/opt/homebrew/opt/sdl3_ttf/lib
../bin/no build -cc ./ccwrap.sh -o dist/app-layered tests/main-layered.no
./dist/app-layered
```
启动打印 `scene=8 elements=3 entities=3`，窗口持续存活。

阶段6 —— SDL_GPU 后端（Metal，真 GPU 渲染）+ SDL_ttf 字体 shaping：
```sh
./build-shims.sh                                   # 编译 libnoui-shim.a + libnoui-gpu-shim.a
cd ../example/noui-gpu-demo
../../bin/no build -cc ../../noui/ccwrap.sh -o gpu-demo . && ./gpu-demo
```
GPU demo 源码已移入 `example/noui-gpu-demo/`（仓库 example 区）；noui 提供 shim 库与 `-cc` wrapper。
实测打印 `Metal API Validation Enabled` + 窗口句柄有效（macOS 走 Metal）。

阶段3/4 纯逻辑测试（无窗口）：
```sh
../bin/no run -cc ./ccwrap.sh tests/app-layer.no     # ALL APP-LAYER TESTS PASSED
../bin/no run -cc ./ccwrap.sh tests/widget.no        # ALL WIDGET TESTS PASSED
```

`no test` 覆盖 L1–L3 纯逻辑（实体注册 / flex 布局 / scene dump，11 项断言）：
```sh
cd tests/logic && ../../../bin/no test .       # ALL TESTS PASSED
```
该目录有独立 `package.noml`（无 link-libs），不依赖 SDL/shim，可在无图形环境跑。

## 一键运行

```sh
chmod +x run.sh && ./run.sh
```

前置：`brew install sdl3 sdl3_ttf`（阶段6字体依赖，已装 sdl3 3.4.18 / sdl3_ttf 3.2.2）；Nolang 编译器 `bin/no`。

## 技术路线（已按你的选择：原生 Vulkan/Metal/DX12）

GPUI 的核心是 Application/Entity/Context/View/Element 状态模型 + 三阶段渲染 +
flex 布局 + SDF 绘制 + 平台后端。本纵切面用 SDL3 作为统一的 C ABI 后端
（底层在 macOS 走 Metal、Linux 走 Vulkan、Windows 走 DX12），
Nolang 通过 `#{c}` FFI 驱动窗口、绘制与事件。

## 实测约束（诚实说明，详见 ARCHITECTURE.md §5.1）

早期把「函数内无法访问全局 vec」归为 MIR 限制，实际是**命名违规**：语言规范要求 module 级
全局用大写名开头，旧探针用小写所以函数读到另一份存储。修正命名 + 修复编译器的
with-len 全局注册缺陷后，**分层内核已可落地**（`tests/main-layered.no`）。

当前仍成立的约束：
- **頂層數據綁定四檔命名**（`build/global_case.go` 管同模組、`build/module_check.go` 管跨模組）：`BUF` 大寫=全局（跨函式/跨模組合法）；`buf` 小寫=局部（被函式引用→硬錯誤）；`_BUF`=私有全局（**同模組跨函式讀寫合法**，「_」是模組私有標記；只有跨模組 `mod._BUF` 訪問→硬錯誤）；`_buf`=私有局部（不開放，無條件硬錯誤）。單獨 `_` 通配放行。但**函數名、方法名、接口名、結構體名可小寫 / `_` 開頭**（`foo`、`_helper`），純 main 局部也合法。
- `with-len` 类 call-init 容器只有大写名才注册为 module global：小写名即使在纯 main 内 `push` 也会失败（`vec.push: needs receiver and element`）。共享数据一律大写。
- 不支持 vec-of-struct → 用 SoA 平行数组。
- 字符串用单引号；插值里的复杂表达式先算成局部变量。
- `i = i + 1` 的溢出注解在循环内不稳 → 用 match 解包 option 或区间迭代。
- 🔴 `(cond) { }` 是条件循环（while），`(cond) -> { }` 才是一次性 guard。旧内核的
  「死循环」根因是 3 处 guard 误写成循环（`el-add` + `scene-dump` 两处），
  已全部改为 `-> {}`，大写版 `tests/kernel2.no` 复活并输出 `elements: 2`。

## 文件清单

入口与配置（包根）：
| 文件 | 作用 |
|---|---|
| `main.no` | 唯一入口（`no build .` 约定）：FFI 声明 + 顶层主循环 + 命中测试 + 绘制 |
| `package.noml` | 包配置（link-libs）。工作区用仓库根 `workspace.noml`（`noui: '/noui'`），本目录不再单独放 `workspace.noml` |
| `run.sh` / `build-shims.sh` / `ccwrap.sh` | 一键运行 / 编译 shim 静态库 / `-cc` 链接包装器（注入 sdl3/sdl3_ttf 与 shim 路径） |
| `ARCHITECTURE.md` | 架构设计与实测约束全文 |

`src/` 源码：
| 文件 | 作用 |
|---|---|
| `src/shim/shim.c` `.h` | SDL_Render + SDL_ttf(HarfBuzz) 字体后端 |
| `src/shim/gpu_shim.c` `.h` | SDL_GPU 后端（MSL shader / Metal） |
| `src/entity.no` `src/geom.no` … | 早期库模块（被 `#` 导入）；`geom` 可用，`entity` 因遗留小写全局被四档命名诊断拒绝 |
| `src/components.no` | **全目录组件库**：83 个组件 kind + 核心 10 状态机（gpui-kit 86 模块映射） |
| `src/components-catalog.md` | 组件清单：86 模块 → Nolang API 映射 + 设计约束 |

`tests/` 演示与测试（独立可运行档）：
| 文件 | 作用 |
|---|---|
| `tests/main-layered.no` | **分层内核版**：Entity/Element/Scene 大写全局池 + 函数封装 + Scene 遍历绘制 |
| `tests/core-v4.no` | 分层内核 v4（Entity → Element → Scene） |
| `tests/app-layer.no` | 阶段3：Ctx / Action / 焦点系统 |
| `tests/widget.no` | 阶段4：widget 构造器 + 主题 |
| `tests/interaction.no` | 交互逻辑单元测试 |
| `tests/logic/` | `no test` L1–L3 纯逻辑（独立 package，无 shim 依赖） |
| `tests/kernel.no` `tests/kernel2.no` `tests/test-kernel.no` | 旧 470 行完整分层原型 / 大写修复版 / gen-test.sh 生成物 |
| `tests/probe.no` `tests/test-l1.no` `tests/sv.no` `tests/t1-3.no` | 早期探针 |

仓库 `example/` 区：
| 文件 | 作用 |
|---|---|
| `example/noui-gpu-demo/` | 阶段6：SDL_GPU 演示（独立 package） |
| `example/noui-pkgtest/` | 早期包结构探针（独立 package） |

## 验收记录

```
$ ./run.sh
noui on SDL 3.4.18
font shaping: 1
window handle: 506987857408
bye                          # ESC / 超时后正常退出

$ ../bin/no run -cc ./ccwrap.sh tests/interaction.no
all-ok=true
ALL INTERACTION TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/app-layer.no
ALL APP-LAYER TESTS PASSED

$ ../bin/no run -cc ./ccwrap.sh tests/widget.no
ALL WIDGET TESTS PASSED

$ cd ../example/noui-gpu-demo && ./gpu-demo
Metal API Validation Enabled      # SDL_GPU 在 macOS 走 Metal
window handle: 528093741056
```
