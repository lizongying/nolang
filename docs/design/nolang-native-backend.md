# Nolang 原生后端技术方案：nocg（对标 llc）+ nold（对标 lld）

> **文档定位**：这是一份**前向设计文档**（forward design），不是实施记录。当前代码库中尚无任何一行本文档描述的功能实现。所有引用的现有行号均取自 2026-10-09 工作树。
>
> **一句话目标**：为 Nolang 增加一条**不依赖外部 LLVM 工具链**的后端路径——从优化后的 MIR 直接产出目标机器码并完成链接，替代当前的 `llc` + `clang` 两段外部调用；初期覆盖 **macOS arm64 / Windows x64 / Linux x64** 三个平台；由**默认关闭**的编译开关控制。
>
> **命名**：`nocg` = Nolang Code Generator（对标 `llc`）；`nold` = Nolang Linker（对标 `lld`）。二者均为 **Go 库包**，不另建可执行文件，通过 `-native` 开关由现有 `no build/run/test` 驱动（是否额外暴露 `no nocg` / `no nold` 调试子命令，见开放问题 Q5）。
>
> **参考基准**：本地 LLVM 源码树 `/Users/lizongying/IdeaProjects/llvm-project`（HEAD `3171d08fc`），重点对照 `llvm/tools/llc/` 与 `lld/{ELF,MachO,COFF}/`。

---

## 0. 决策快照（Executive Summary）

| # | 决策 | 结论 | 论据 |
|---|---|---|---|
| DR-1 | native 后端**吃什么** | **吃优化后的 MIR（`*mir.Module`）内存对象**，不解析 LLVM IR 文本 | 免写 IR parser；`NOLANG_MIR_OPT` 优化已在 lowering 内完成（`hir2mir.go:1408`）；IR 文本里的 triple 是硬编码假值（`codegen.go:2523`）不可信 |
| DR-2 | 是否重写指令选择与寄存器分配 | **要，但分层降级落地**：先 SimpleISel + LinearScan，接口预留 SelectionDAG / Greedy | 全量照抄 LLVM CodeGen 不可行；但 ISel/RA 是产出可用机器码的**必经层**，无法绕过（§7.1 已论证哪些层可绕、哪些不可） |
| DR-3 | 链接器边界 | **纯链接器**：只认 `.o/.a/.so/.dylib/.tbd/.lib`，**不负责找 CRT/libc** | lld 三端口皆是如此——ELF 端口里 `crt1.o/crti.o` 零命中，全由 clang driver 负责（§7.2） |
| DR-4 | CRT/SDK 谁来找 | 独立的 **driver 层** `src/native/driver`，对标 clang driver 的子集 | 复刻 clang/lld 的职责切分，让 `src/link` 保持"哑工具"的可测试性 |
| DR-5 | 插入点 | **两处**：codegen 侧 `transpiler.go:2858`；链接侧 `builder.go:752` / `builder.go:791` | 唯一汇流点；改动面最小，不触碰前端/校验/单态化 |
| DR-6 | 开关范式 | 全局 CLI flag `-native[=mode]` → 环境变量 `NOLANG_NATIVE` → 单一读取器 `native.NativeMode()` | 沿用 `-opt` 已验证的范式（`main.go:64` + `mir/opt.go:74`），保证 `build/run/test` 三条路径单一真值来源 |
| DR-7 | 默认状态 | **默认关闭**；关闭时行为与今日**逐字节相同** | 与 `NOLANG_FIELD_PTR`/`NOLANG_MIR_SPAWN_GRAPH` 既有默认-OFF 开关一致；回归基线由现有 corpus 守护 |
| DR-8 | 正确性保障机制 | **双路差分**（external llc+clang vs native）+ **编码器对拍**（自研 encoder vs 外部 assembler 字节级比对） | 项目已有 `scripts/diff_tests.sh`、`mir_golden.sh` 与 `nolang-mir-parity-sweep` 技能，基建现成 |

---

## 1. 现状：流水线与精确插入点

### 1.1 当前后端路径（只有一条）

```
main.no
  ↓ src/lexer → src/parser → src/checker          前端 + 校验
  ↓ src/mir.LowerHIR()          (hir2mir.go:1056)  HIR → MIR
  ↓ mir.OptimizeMIR(MIROptLevel())                 ← 「opt 优化阶段」（默认 OFF，见 opt.go:74）
  ↓ transpiler.emitMIR()        (transpiler.go:2966，调用点 :2858)
      ↓ mod.EmitLLVM()          (codegen.go:413)   MIR → LLVM IR **字符串**
      ↓ verifyMIRIRViaOpt()     (transpiler.go:2894，调用点 :3032)   ← 外部 opt/llc 预检
  ↓ buildLLVMInternal()         (builder.go:662，调用点 :602)
      ↓ WriteFile(llPath)           :687           <tmp>/<name>.ll
      ↓ exec "opt"  -O2 -S         :713           <tmp>/<name>_opt.ll
      ↓ exec "llc"  -mtriple=…     :770           <tmp>/<name>.s      ← ★ 对标 llc 的位置
      ↓ exec clang/zig cc          :843 / :845    <outPath> 可执行     ← ★ 对标 lld 的位置（含汇编+链接）
      ↓ os.Chmod(outPath, 0755)    :868
```

### 1.2 三个必须先知道的现状陷阱

| 陷阱 | 位置 | 后果 | native 后端的处理 |
|---|---|---|---|
| **IR 里的 triple 是假的** | `codegen.go:2523` 硬编码 `target triple = "arm64-apple-macosx15.0.0"` | **不能**从 IR 文本读目标；真正目标只通过 `-mtriple=`（builder.go:768）/ `--target=`（:813）外传 | 必须从 `-target` → `parseTargetPlatform()`（builder.go:202）取 |
| **`toOpaquePointers` 是给外部 opt/llc 打的补丁** | `builder.go:653`（正则 :647-650），调用点 :686 | 正则把 `i8*`→`ptr`，纯文本替换，只为喂给外部 opt/llc | native 模式**必须跳过**：去正则解析被改写过的 IR 是自找麻烦 |
| **`verifyMIRIRViaOpt` 总会起外部进程** | `transpiler.go:2894`，调用点 :3032 | 开着 native 也仍在 call `opt`（默认仅 `-passes=verify`，`NOLANG_MIR_PREFLIGHT=full` 时跑完整 `-O2`+`llc`） | native 模式下**必须短路**，改用内部 MI verifier，否则"去 LLVM 依赖"是假的 |

### 1.3 平台相关的既有资产

| 资产 | 位置 | 用途 |
|---|---|---|
| `-target` triple ↔ (GOOS,GOARCH) | `builder.go:202` `parseTargetPlatform` | 目标解析的唯一既有实现 |
| host triple 推导 | `builder.go:25` `DetectTarget` | 缺省 target |
| 平台注解键表 | `src/package/platform.go:6` `PlatformKeys` | `mac-arm64` / `win-amd64` / `linux-amd64` … |
| lowering 侧目标平台单例 | `src/mir/platform.go:17` `mirTarget`（`atomic.Pointer`） | `targetGOOS()/targetGOARCH()` 是所有平台分支的唯一真值来源 |
| Windows msvcrt shim | `src/mir/builtin_win_shims.go`、`forward_call.go` | Windows 上 POSIX 符号替换表，**native 后端可直接复用** |

> ⚠️ `src/mir/tier.go` 与本文无关：它是所有权层级推断（S/C/R），不是 LLVM 的 target tier。

---

## 2. 目标 / 非目标 / 约束

### 2.1 目标（G）

- **G1** 默认关闭，开启后 `no build` 产出**可执行且在三个目标平台上行为与今日一致**。
- **G2** native 路径**不执行任何外部工具**：无 `opt`、无 `llc`、无 `clang`、无 `zig`。这是"去依赖"的硬指标，须有测试断言（拦截 `exec.Command`）。
- **G3** 目标三平台：macOS arm64（Mach-O）、Windows x64（PE/COFF）、Linux x64（ELF64）。三者共享 machine-IR 与 MC 层，仅 codegen/obj writer/linker 端口不同。
- **G4** 错误模型：**遇到不支持的结构就报清晰诊断，绝不产出静默错误的二进制**。宁可 fail，不可 emit garbage。
- **G5** 长期可演进：ISel/RA/编码器/obj writer/linker 全部是接口，允许后续插入更好的实现而不动上层。

### 2.2 非目标（NG）

- ❌ **不做 LTO**：三端口各自一套 bitcode 分支，需要链接整套 LLVM CodeGen+LTO 库，是最大的一块依赖。
- ❌ **不做 dSYM / PDB**：lld 本身就不生成 dSYM（由 `dsymutil` 后置生成），PDB 亦同；先做 debug info **透传**（把 DWARF/CodeView 段原样搬到输出），不自产。
- ❌ **不做 linker script**（ELF）：`LinkerScript.cpp` 61KB + `ScriptParser.cpp` 62KB。提供最小 stub 版 `Script` 满足 `assignAddresses()` 内部调用即可。
- ❌ **不做动态库产出**（dylib / DLL / .so 导出表）：MVP 只出可执行文件。
- ❌ **不做 ICF / GC / 符号版本**：属于优化与体积特性，后置。
- ❌ **不作为通用的目标文件工具**：不是一个替代完整 `ld` 的产品。它是"**服务 Nolang 自身 codegen 的专用后端**"。

### 2.3 约束（C）

- **C1** 不得改动 `src/mir` 的语义（同一份优化后 MIR 既是 legacy 路径的输入，也是 native 路径的输入）。
- **C2** 不得引入 CGO 或第三方重量依赖（`go.mod` 保持干净，禁止 `llvm.org/*` Go binding）。
- **C3** 遵守"一次一件事"节奏：每个 milestone 独立可合并、可回滚。
- **C4** macOS arm64 的**代码签名是硬门槛**，不可省略（见 §6.2）。

---

## 3. 架构总览

### 3.1 分层

```
                    ┌─────────────────────────────────────────┐
                    │ 优化后的 MIR（*mir.Module，内存对象）      │   ← DR-1
                    └────────────────────┬────────────────────┘
                                         │
╔════════════════════════════════════════▼════════════════════════════════╗
║  nocg（对标 llc）                                        src/native/     ║
║                                                                          ║
║  ┌─ P1 MI 构造 ──────────────────────────────────────────────────────┐   ║
║  │  MIR → MI：MachineFunction / MBB / MI / 虚拟寄存器 / 抽象栈槽        │   ║
║  │  完全目标无关。含 MI Verifier（替代 verifyMIRIRViaOpt）             │   ║
║  └───────────────────────────┬───────────────────────────────────────┘   ║
║  ┌─ P2 合法化 / 指令选择 ─────▼───────────────────────────────────────┐   ║
║  │  legalize（宽度/寻址模式合法化）→ SimpleISel（宏展开 + 局部树匹配）   │   ║
║  │  接口预留：SelectionDAG 可在同位置插入（DR-2）                       │   ║
║  └───────────────────────────┬───────────────────────────────────────┘   ║
║  ┌─ P3 寄存器分配 ───────────▼───────────────────────────────────────┐   ║
║  │  LiveIntervals → LinearScan + spill → VirtRegRewrite               │   ║
║  │  接口预留：Greedy / PBQP                                            │   ║
║  └───────────────────────────┬───────────────────────────────────────┘   ║
║  ┌─ P4 帧布局 / ABI ─────────▼───────────────────────────────────────┐   ║
║  │  Prolog/Epilog 插入、栈槽着色、调用约定 lowering、CSR 保存           │   ║
║  │  ── 目标相关：aarch64 / x86_64 两个实现                              │   ║
║  └───────────────────────────┬───────────────────────────────────────┘   ║
║  ┌─ P5 MC 层 ────────────────▼───────────────────────────────────────┐   ║
║  │  MI → MCInst → encode → **Fragment + Fixup**（可重定位字节流）       │   ║
║  │  section / symbol / Relaxation / layout                             │   ║
║  └───────────────────────────┬───────────────────────────────────────┘   ║
║  ┌─ P6 Object Writer ────────▼───────────────────────────────────────┐   ║
║  │  Mach-O(arm64) │ ELF64(x86-64) │ COFF/PE32+(x86-64)                │   ║
║  └───────────────────────────┬───────────────────────────────────────┘   ║
╚══════════════════════════════│═══════════════════════════════════════════╝
                               │  <tmp>/<name>.o
╔══════════════════════════════▼═══════════════════════════════════════════╗
║  driver（对标 clang driver 子集）                        src/native/driver ║
║  triple → CRT 对象 + 系统库搜索路径；xcrun / gcc -print-file-name / LIB    ║
╚══════════════════════════════│═══════════════════════════════════════════╝
                               │  .o + crt*.o + [-lc|-lSystem|-lmsvcrt …]
╔══════════════════════════════▼═══════════════════════════════════════════╗
║  nold（对标 lld）                                        src/link/         ║
║                                                                           ║
║  读输入 → 符号解析 → **分析遍**（GOT/PLT/stub/rebase/bind 需求）           ║
║        → 段布局 → **地址分配（迭代收敛）** → __LINKEDIT/dynamic 内容生成   ║
║        → **应用遍**（写字节）→ codesign/build-id/checksum → commit         ║
║                                                                           ║
║  端口：macho │ elf │ coff   （共享 aggregator/layout/symbol core）         ║
╚═══════════════════════════════════════════════════════════════════════════╝
                               │
                          <outPath> 可执行
```

### 3.2 与 LLVM 层的对应关系

| Nolang 新增层 | LLVM 对应物 | 说明 |
|---|---|---|
| `src/native/mi` | `llvm/lib/CodeGen/MachineFunction/MachineInstr` | 机器 IR |
| `src/native/passes/legalize` | `GlobalISel/Legalizer` | 合法化 |
| `src/native/passes/isel` | `SelectionDAG` / `FastISel` / `GlobalISel` | 三选一，MVP 取"SimpleISel"中间档 |
| `src/native/passes/ra` | `RegAllocGreedy` / `RegAllocFast` | LinearScan ≈ Fast~Greedy 之间 |
| `src/native/passes/frame` | `PrologEpilogInserter` | PEI |
| `src/native/mc` | `llvm/lib/MC/{MCContext,MCSection,MCFragment,MCAssembler}` | MC 核心 |
| `src/native/obj/*` | `MachObjectWriter` / `ELFObjectWriter` / `WinCOFFObjectWriter` | 对象写出 |
| `src/link/*` | `lld/{MachO,ELF,COFF}` | 链接器 |
| `src/native/driver` | clang driver 的 `-target`/sysroot/CRT 部分 | **新增层**：承担 lld 刻意不做、交给 driver 的那部分职责 |

---

## 4. 关键架构决策详述

### DR-2：为什么不能完全绕过指令选择与寄存器分配

这是本方案最需要讲清楚的一点。LLVM 的后端栈可裁剪性如下（对照 `llvm/lib/CodeGen/CodeGenTargetMachineImpl.cpp:164-227` 的三分支）：

| 层 | 能否绕过 | 理由 |
|---|---|---|
| llc 驱动层（`llcdriver.cpp`） | ✅ 可绕 | ≈1000 行胶水，我们自己有 `-native` 开关 |
| TargetMachine / TargetPassConfig | ✅ 可绕 | 只用于装管线 |
| **IR→MI 的 ISel** | ❌ **不可绕** | 除非复用已成型的 MI（我们只有 MIR，不是 MI） |
| **MI 的 RA / 调度 / PEI** | ⚠️ **RA 不可绕** | 虚拟寄存器必须映射到物理寄存器才能编码；**指令调度可绕**（MVP 不调度） |
| AsmPrinter / MCInstLower | ✅ 可绕 | 我们直接 MI→MCInst，不必走 `MachineInstr` 下沉钩子 |
| **MI→MCInst 编码** | ❌ **不可绕** | 每个架构一套 encoder，是纯工作量也是必经 |
| MCStreamer→fragment/fixup | ❌ 不可绕 | 布局与 relaxation 的载体 |
| MCAssembler layout + 不动点 relaxation | ❌ 不可绕 | 写 .o 必需 |
| ObjectWriter | ❌ 不可绕 | 写 .o 必需 |

**结论**：最小不可绕集 = `MI 构造 → ISel → RA → PEI → encode → fragment/fixup → layout → ObjectWriter`。这正是 §3.1 的 P1–P6。

**规模压缩的关键杠杆**：Nolang 不是 C 编译器。它只需要 lower 自己 emit 出来的那一小撮 MIR 算子。
- C 编译器要支持语言全部类型转换、结构体传参 ABI 的无穷组合；Nolang 的类型系统封闭且已知。
- 因此 **SimpleISel（宏展开 + 局部树匹配，无全局 DAG）足够**，不需要 SelectionDAG 的 30 万行。
- 同理，**线性扫描寄存器分配**在 Nolang 的函数形态（SSA-ish、生命周期短）下与 Greedy 差距很小。

### DR-3 / DR-4：链接器的职责边界

调研 lld 三端口的结论高度一致，且**反直觉但极省事**：

> **lld 完全不知道 CRT 的存在。** 在 `lld/ELF/` 全目录 grep `crt1|crti|crtn|Scrt1` 只有 4 处命中，**全是注释**。找 `crt1.o`、`-nostdlib`、`-nostartfiles` 都是 **clang/gcc driver** 的职责。
>
> MachO 端口同理：`crt` 仅在 `Options.td` 帮助文本与一处 `findLibrary` 注释（`Driver.cpp:101`）出现；`-lSystem` 由 clang 传入。
>
> **唯一例外是 Windows/COFF**：lld 必须自己解析 `.o` 的 `.drectve` 段里的 `/defaultlib:` 指令（`Driver.cpp:610-613`），并据此探测 VS / WinSDK 路径（`addWinSysRootLibSearchPaths`，`Driver.cpp:880-913`）——因为 MSVC 这条路径依赖目标文件自述。

因此本方案**如法炮制**：

| 组件 | 职责 | 不做什么 |
|---|---|---|
| `src/link` | 符号解析、GC（后置）、布局、地址分配、重定位、写文件 | ❌ 不知道 SDK / CRT / libc / `-nostdlib` |
| `src/native/driver` | 解析 triple → 定位 CRT 对象与系统库 → 组装输入列表和参数喂给 `src/link` | ❌ 不做符号解析 |

带来的好处：
1. `src/link` 可脱离文件系统做**纯单测**（喂内存中的 `.o` byte slice）。
2. macOS/Linux 的 MVP 链接可以用**硬编码 + 一条 `xcrun`/`gcc -print-file-name` 查询**跑通，不必实现 clang 那套 sysroot 版本矩阵。
3. Windows 差异被隔离在 driver 的 COFF 分支里。

### DR-5：为什么插入点是两处而非一处

可选插入点对比：

| 候选 | 优点 | 缺点 | 结论 |
|---|---|---|---|
| `transpiler.go:2858`（`emitMIR` 调用点） | 手上有 `*mir.Module` 内存对象；已知 MIR opt 是否运行 | 需要把产物（object bytes）穿出去给 builder | ✅ **采纳**（codegen 侧） |
| `builder.go:752`（llc 段） | 已有 target/tmpDir/verbose/sink 上下文 | 手上只有 IR 字符串 | ✅ **采纳**（链接侧，此处跳过 llc） |
| `builder.go:791`（cc 段） | 同上，且 `linkLibs` 在此拼装 | 同上 | ✅ **采纳**（链接侧，此处跳过 clang） |
| 新建一条完全平行的 pipeline | 干净 | 与构建缓存/包管理/workspace 并行全部要重做一遍 | ❌ 否决 |

**产物传递**：在 `Transpiler` 上增一个字段（不是改 `Compile` 签名，避免污染 `VetFile`→`Compile`→`CompileTarget` 三层调用链——这正是 `--reuse-std-ast` 当初选择走环境变量的原因，`main.go:3072-3074` 注释有明述）：

```go
// src/build/transpiler.go
type Transpiler struct {
    // … 既有字段
    nativeObj []byte // non-empty ⟹ native 后端已产出对象（绕过 IR 文本路径）
}
```

`builder.go:602` 调用 `buildLLVMInternal` 前读取该字段；非空则走 native 分支（跳过 opt 与 llc，直接进内部链接器）。

> 若后续需要多对象链接（workspace / 依赖包），再把 `[]byte` 升级为 `[]Artifact`。MVP 保持单对象。

### DR-6：开关设计（沿用已验证范式）

**三级结构**，与 `-opt` 完全同构：

```
用户输入 → main() 全局 flag 解析 → os.Setenv → 单一读取器 native.NativeMode() → 各消费点
```

| 层 | 位置 | 内容 |
|---|---|---|
| CLI flag | `src/cmd/no/main.go`（紧邻 `parseOptFlag`，约 :78 之后）新增 `parseNativeFlag(args)` | `-native` → `NOLANG_NATIVE=link`；`-native=asm|obj|link` → 透传 value |
| 环境变量 | `NOLANG_NATIVE` | 未设 / `0` / `off` / `false` → **关闭（默认）**；`asm` / `obj` / `link` → 启用对应档位 |
| 单一读取器 | `src/native/options.go: NativeMode() Mode` | **未知拼写一律按 off**（与 `MIROptLevel()` 的容错一致，`mir/opt.go:71-96`） |
| CLI help | `printUsage()`（`main.go:160`） | 一行说明 + 默认关闭 + 三个档位含义 |

**三档位的意义**（逐步放开，保证每档独立可测）：

| 档位 | codegen | assemble | link | 用途 |
|---|---|---|---|---|
| `off`（默认） | 现有 EmitLLVM | 外部 llc | 外部 clang | 今日行为，逐一字节不变 |
| `asm` | nocg → 汇编文本 | 外部 assembler | 外部 clang | 单独验证 ISel/ABI 正确性 |
| `obj` | nocg → 自研 .o | 自研 MC | 外部 clang | 单独验证 encoder/reloc（仍由外部 linker 兜底） |
| `link` | nocg → 自研 .o | 自研 MC | nold（自研） | **完全自举，零外部工具**（G2） |

**必须同步短路的三处**（否则"去依赖"是假的）：
1. `transpiler.go:3032` `verifyMIRIRViaOpt` → native 模式跳过，改内部 `mi.Verify()`。
2. `builder.go:686` `toOpaquePointers` → native 模式跳过。
3. `builder.go:713` `opt` 与 `builder.go:770` `llc` → native 模式跳过。

---

## 5. 模块划分

### 5.1 包结构与文件清单

```
src/native/                          ← nocg 总控
├── options.go            (~120)  NativeMode() 单一读取器；OptLevel / RelocModel / CodeModel
├── target.go             (~280)  Triple 解析；Target 注册表；Arch × OS × Format 三维组合
├── pipeline.go           (~200)  CompileToObject(mod *mir.Module, cfg) (*Object, error)
├── mi/
│   ├── mi.go             (~450)  MachineFunction / MBB / MI / MOperand / RegClass / FrameObject
│   ├── printer.go        (~120)  MI 转储（对标 -print-after-all）
│   └── verify.go         (~220)  MI Verifier：SSA/寄存器类/支配边界/块出口合法性
├── lower/
│   ├── lower.go          (~700)  mir.Module → MI（目标无关）：CFG、PHI、调用、栈槽、常量池
│   └── intrinsic.go      (~180)  内建 lowering（memcpy/memcmp/math 等）
├── passes/
│   ├── legalize.go       (~400)  宽度合法化、立即数合法化、寻址模式合法化
│   ├── isel.go           (~550)  ✅ SimpleISel：宏展开 + 局部树匹配（接口预留 DAG）
│   ├── ra.go             (~450)  接口 + LinearScan
│   ├── liveintervals.go  (~250)  live-in/live-out + interval 计算
│   ├── frame.go          (~300)  Prolog/Epilog、栈对象着色、CSR
│   └── branch.go         (~180)  块布局 + 跳转收束（falls through 优化）
├── ach/                            ← Architecture hooks（接口定义）
│   └── arch.go           (~260)  Arch 接口：RegInfo/InstrInfo/CallingConv/FrameLowering/ABI
├── aarch64/
│   ├── regs.go           (~180)  x0-x30/v0-v31/sp/fp/lr；CSR 集合（按 Darwin/AAPCS64）
│   ├── isel.go           (~1200) MI → AArch64 HI/LO 虚拟指令
│   ├── encode.go         (~1500) 32-bit 定长指令编码（branch/add/sub/ldr/str/fmov…）
│   ├── abi.go            (~350)  AAPCS64-Darwin：参数/返回值分配、16B 栈对齐、TLS
│   └── frame.go          (~220)  pair-wise save/restore、frame record (fp/lr)
├── x86_64/
│   ├── regs.go           (~180)  SysV 与 Win64 两套 CSR/参数寄存器表
│   ├── isel.go           (~1600) MI → x86 pseudo
│   ├── encode.go         (~2000) REX/ModRM/SIB + VEX；Win64 与 SysV 差异此处闭合
│   ├── abi.go            (~500)  SysV AMD64 vs Microsoft x64（含 32B shadow space）
│   └── frame.go          (~200)  push/pop + sub rsp；Windows 需生成 .pdata/.xdata
├── mc/
│   ├── context.go        (~180)  MCContext：符号/section uniquing 工厂（对标 MCContext.h:83）
│   ├── section.go        (~260)  Section + Fragment 容器（对标 MCSection.h:580/:45）
│   ├── fragment.go       (~200)  Fragment 类型体系：Data/Align/Fill/Relaxable/Dwarf
│   ├── fixup.go          (~180)  Fixup + value 表达（对标 MCFixup / MCValue）
│   ├── assembler.go      (~320)  layout + relaxation **不动点** + 写 section data
│   └── relax.go          (~160)  Relaxation 策略；AArch64 branch/jump + x86 jcc/jmp
├── obj/
│   ├── object.go         (~150)  统一的 Object IR：Section/Symbol/Relocation 表（形式无关）
│   ├── macho.go          (~900)  写 Mach-O arm64
│   ├── elf.go            (~1200) 写 ELF64 REL 可重定位对象
│   ├── coff.go           (~1200) 写 COFF PE32+ object
│   └── encode_testonly.go (~120) 测试用：从外部 objdump/otool 反解回来做断言
├── asm/                            ← 可选：汇编文本输出（配合 -native=asm 档位，用于调试）
│   └── printer.go        (~400)  AsmPrinter（对标 AsmPrinter.h，仅保留 6 个 hook）
└── driver/
    ├── driver.go         (~300)  对标 clang driver 子集：组装 nold 的输入与参数
    ├── crt_darwin.go     (~150)  xcrun --show-sdk-path → /usr/lib/crt1.o；-lSystem
    ├── crt_linux.go      (~180)  gcc/clang -print-file-name=crt1.o|crti.o|crtn.o；-lc
    └── crt_windows.go    (~280)  MSVC: .drectve /defaultlib + LIB + vswhere；MinGW: -lmsvcrt

src/link/                            ← nold（对标 lld）
├── driver.go             (~450)  link(cfg) 主流程 + 全局错误恢复模型
├── options.go            (~300)  link 配置 struct（对标 lld 各端口 Config）
├── input.go              (~500)  输入文件识别与读取分派（magic switch）
├── archive.go            (~280)  .a 归档 + lazy 成员抽取
├── dylib.go              (~350)  MachO .dylib/.tbd
├── shared.go             (~300)  ELF .so
├── importfile.go         (~250)  COFF import lib（短格式 → ImportFile）
├── symbol.go             (~450)  Symbol 层级 + SymbolUnion + replaceSymbol 语义
├── resolve.go            (~350)  符号表 + 符号分辨率 + 重复/undefined 诊断
├── scan.go               (~500)  **分析遍**：决定 GOT/PLT/stub/rebase/bind 需求
├── layout.go             (~450)  段聚合 + 优先级排序表
├── addresses.go          (~350)  地址/文件偏移分配 + **迭代收敛循环**
├── relocate.go           (~400)  **应用遍**：写字节
├── writer_macho.go       (~1100) MachO 端口：LC_* 拼装、__LINKEDIT、动态 fixup、codesign
├── writer_elf.go         (~1300) ELF 端口：Ehdr/Phdr/Shdr、.dynamic/.got/.plt/.rela
├── writer_pe.go          (~1300) PE 端口：DOS stub、PE32+、section table、.idata、.reloc
├── exporttrie.go         (~200)  （产出 dylib 时才需要，MVP 可留空实现）
└── arch/
    ├── arch.go           (~200)  LinkArch 接口：getRelExpr / inRange / relocateOne / writePlt
    ├── aarch64.go        (~450)  MachO+ELF arm64 重定位语义与 stub/thunk
    ├── x86_64_elf.go     (~400)  SysV x86-64 全部所需 RelExpr→写入
    └── x86_64_pe.go      (~400)  PE x86-64 IMAGE_REL_AMD64_* 处理
```

**规模估算**：nocg ≈ 20k 行，nold ≈ 10k 行，driver 层 ≈ 1k 行，测试 ≈ 10k 行 → **约 40–50k 行 Go**。加上"两架构而非一架构"的乘数，保守估计 **60k 行量级**。这是必须在路线图里诚实的数字（§9）。

### 5.2 Machine IR（MI）设计要点

```go
// src/native/mi/mi.go（示意）
type RegID uint32        // 虚拟寄存器；>= FirstPhysReg 为物理寄存器
type FrameIndex int32    // 抽象栈槽，PEI 之前不知道偏移

type OperandKind uint8
const (
    OpReg OperandKind = iota   // 虚拟/物理寄存器
    OpImm                      // 立即数
    OpFrame                    // FrameIndex（抽象栈槽）
    OpGlobal                   // 全局符号 + 可选偏移 → 需要重定位
    OpBlock                    // MBB 引用（分支目标）
    OpConstPool                // 常量池索引
)

type Inst struct {
    Op      Opcode      // 目标无关虚拟 opcode（如 Add64/Load/Store/Br/Call/Ret）
    Defs    []RegID
    Uses    []Operand
    Flags   InstFlags   // IsCall / IsReturn / MayLoad / MayStore / HasSideEffect
}
```

三条设计约束：
1. **虚拟 opcode 集合保持极小**：只够表达 Nolang 的语义即可，不追求通用性。
2. **抽象栈槽 FrameIndex 而不是具体 `[sp, #imm]`**：PEI 之前不提交帧布局，与 LLVM 一致（`PrologEpilogInserter`）。
3. **MI Verifier 必须前置**：替代 `verifyMIRIRViaOpt`。检查项：定义先于使用（支配性）、寄存器类一致、调用点不在延迟槽内（x86 无延迟槽，arm64 亦然）、块出口合法（非 fallthrough 到无后继）。**这是 C1 的守护者**。

### 5.3 MC 层设计要点

照抄 LLVM 最有价值的一个内存技巧：`MCSection` **持有** ContentStorage / FixupStorage / MCOperandStorage 三个 `SmallVector`，`MCFragment` **只存偏移索引**（`MCSection.h:101-109` / `:602-638`）。Fragment 不拥有内存。这消除了每个 fragment 一次堆分配，是汇编器吞吐的关键。

必须实现的核心循环（对标 `MCAssembler.cpp`）：

```
layout():
    1. 分配 section ordinal
    2. 首轮 layoutSection
    3. while ((firstStable = relaxOnce(firstStable)) > 0) { }   ← 不动点
    4. executePostLayoutBinding()     ← 符号索引在这里分配
    5. 遍历所有 fragment 的 fixup → evaluateFixup(RecordReloc=true)
       └─ 无法解析者 → writer.recordRelocation()
```

> ⚠️ **第 3 步的不动点必须有轮数上限**（LLVM 用 `-relax-all` 之外依赖 layout 内部 while；若我们不加上限会死循环）。建议：relax 轮上限 32，超出报 `relaxation did not converge`。这是防御式工程的体现——对应 §2.3 的 C4（fail loud）。

### 5.4 对象写出层

三格式公共 Object IR（形式无关）→ 三个 writer：

```go
type Object struct {
    Triple   Target
    Sections []*RawSection   // Name, Flags, Align, Data []byte
    Symbols  []*RawSymbol    // Name, Kind(Defined/Undefined/Common), Section, Value, Size, Scope
    Relocs   []*RawReloc     // Section, Offset, Kind, Symbol, Addend
}

type Writer interface { Write(o *Object, w io.Writer) error }
```

**就是这一层把三平台的差异收敛成一个 switch**，照抄 `llvm/lib/MC/MCAsmBackend.cpp:31-63` 的单一格式分派点——那 33 行是整个 LLVM MC 层最值得照抄的函数。

### 5.5 链接器核心：两遍 + 收敛

调研给出的最重要三条经验（lld 三端口一致）：

1. **"地址分配" 与 "内容生成" 必须分离**。
   MachO 端口把这个约定写进了注释（`OutputSection.h:61-72`）：`finalize()` 串行有顺序保证、在地址分配前调用；`finalizeContents()` 无顺序保证、可并行。
   → **收益：并行写盘**。地址算完之后每个 section 独立 memcpy，天然可 `parallelForEach`。

2. **重定位必须分「分析遍」和「应用遍」**。
   ELF：`scanRelocations`（`Relocations.cpp:1188`，只打标记，不改字节）→ `TargetInfo::relocateAlloc`（`Target.cpp:165-168`，写字节）。
   MachO：`Writer::scanRelocations`（`Writer.cpp:704-752`）→ `ConcatInputSection::writeTo`（`InputSection.cpp:224-284`）。
   → **如果合成一遍，会陷入"地址未定但需要知道大小"的死锁**。这是不可协商的架构约束。

3. **Mach-O 的 `__LINKEDIT` 是空的，必须先定其他段地址才能生成内容**（`Writer.cpp:1173-1175` 注释明确说明）。这正是上面第 1 条的最强论据。

**收敛循环**：ELF 端口里有显式的不动点循环（`Writer.cpp:1529-1611`），因为

```
thunk/relax 改变段大小 → 段地址变 → 分支是否越界变 → 需要新的 thunk → 大小又变 …
```

MachO arm64 不需要（只用 thunk 不用 branch island，插入是单向的，`ConcatOutputSection.cpp:48-53` 注释有说明）。
→ **本方案：ELF 端口实现收敛循环（thunk 轮上限 30 / 地址轮上限 5，同 LLVM）；MachO 端口用单趟流式算法 + `outOfRangeVA` hack（`Symbols.cpp:93-103`，未 finalize 的地址返回"必然越界"值，让 thunk 算法自洽）。** 这个 hack 很巧妙，值得照抄。

### 5.6 错误模型

照抄 `lld/Common/ErrorHandler.h:9-66` 的哲学：**报错后继续跑，到 checkpoint 再检查 `errorCount()`**。这样一次链接能报出所有未定义符号，而不是一条一条挤。

配 `-error-limit=N`（默认 20）。

---

## 6. 平台适配策略

### 6.1 三平台总表

| 维度 | macOS arm64 | Linux x86-64 | Windows x64 |
|---|---|---|---|
| **triple** | `arm64-apple-macosx<ver>` | `x86_64-unknown-linux-gnu` | `x86_64-pc-windows-msvc`（`/ -gnu`） |
| **目标文件** | Mach-O arm64 (MH_MAGIC_64) | ELF64 LSB REL | COFF object (PE32+ target) |
| **可执行文件** | Mach-O MH_EXECUTE | ELF64 EXEC/DYN | PE32+ `IMAGE_FILE_EXECUTABLE_IMAGE` |
| **符号前缀** | `_`（Mach-O 全局 C 符号前置下划线） | 无 | 无（MSVC 有自己的 mangling，此处不涉及） |
| **调用约定** | AAPCS64（Darwin 变体） | SysV AMD64 ABI | Microsoft x64 ABI |
| **参数寄存器** | x0–x7 | rdi,rsi,rdx,rcx,r8,r9 | rcx,rdx,r8,r9 |
| **返回值** | x0 (+ x1 128-bit) | rax (+ rdx) | rax (+ rdx 用于 `__m128i`/large struct via hidden ptr) |
| **栈对齐** | 16 B | 16 B（调用点保证） | 16 B + **32 B shadow space**（调用者预分配） |
| **红区** | ⚠️ **Darwin arm64 语义需按 Apple ABI 文档核实**（与 SysV 的 128 B 红区不同），见 §6.2 R3 | 128 B | 无（且禁止使用） |
| **callee-saved 整数寄存器** | x19–x28, fp(x29), lr(x30) | rbx, rbp, r12–r15 | rbx, rbp, rdi, rsi, r12–r15 |
| **PIC** | **强制 PIC**（Darwin 恒为 PIC_） | PIC/PIE（注意：现有路径强制 `-no-pie`，见 builder.go:821-824） | 位置相关 + base relocation 表 |
| **页大小** | **16 KiB（arm64）** | 4 KiB（默认） | SectionAlignment 4 KiB / FileAlignment 512 B |
| **unwind** | `__TEXT,__unwind_info`（**arm64 ABI 必需**） | `.eh_frame`（C++ 需要，C 可选） | `.pdata` / `.xdata`（**x64 ABI 要求每个函数都有项**） |
| **CRT 对象** | SDK 内 `/usr/lib/crt1.o` | `crt1.o` + `crti.o` + `crtn.o` | MSVC: 由 `.drectve` 自述；MinGW: `crt2.o` 等 |
| **系统库** | `-lSystem`（libSystem.B.dylib） | `-lc`（可按 `-static` 走 libc.a） | `msvcrt` / `libcmt` / `ucrt` + `kernel32` |
| **动态链接器** | `/usr/lib/dyld`（LC_LOAD_DYLINKER） | `/lib64/ld-linux-x86-64.so.2`（PT_INTERP） | n/a（PE loader） |
| **入口** | `LC_MAIN`（现代）或 `LC_UNIXTHREAD` | ELF `e_entry` → `_start` | `AddressOfEntryPoint` |
| **签名/校验** | **ad-hoc 代码签名（arm64 必需）** | 无 | PE Checksum（可选，但惯例會填） |
| **已有项目支持** | `PlatformKeys["mac-arm64"]` ✅ | `PlatformKeys["linux-amd64"]` ✅ | `PlatformKeys["win-amd64"]` ✅ |

### 6.2 macOS arm64

**必做清单**（对标 lld MachO Writer 的最小集）：

| # | 环节 | LLVM 参照 | 备注 |
|---|---|---|---|
| 1 | `__PAGEZERO` | `Writer.cpp:1087-1089`；`LP64::pageZeroSize = 1<<32` | arm64 是 **4 GiB**，不是 x86_64 的 4 KiB |
| 2 | Load Commands：`LC_SEGMENT_64`×N、`LC_SYMTAB`、`LC_DYSYMTAB`、`LC_LOAD_DYLINKER`、`LC_LOAD_DYLIB`×N、`LC_MAIN`、`LC_BUILD_VERSION`、`LC_UUID` | `createLoadCommands` `Writer.cpp:824-978` | 缺 `LC_BUILD_VERSION` 会被拒绝加载 |
| 3 | `__LINKEDIT`：symbol table + string table + indirect symtab | `SymtabSection::finalizeContents` `SyntheticSections.cpp:1323` | |
| 4 | **rebase + bind opcode** | `RebaseSection::finalizeContents` `SyntheticSections.cpp:277-297`；`BindingSection` `:626-655` | 链接 dylib 必不可少 |
| 5 | `LC_DYLD_INFO_ONLY`（先实现 classic dyld opcode） | `Writer.cpp:831-837` 二选一 | **chained fixups 后置**（macOS 13+ 默认，但 classic 仍被接受） |
| 6 | **ad-hoc 代码签名** | `CodeSignatureSection` `SyntheticSections.h:520-543`；`shouldAdhocSignByDefault` `Driver.cpp:1217-1225` | ⚠️ **arm64/arm64e + macOS 默认必须签**，否则进程无法启动。`blockSize = 1<<12`（4 KiB，与 16 KiB 段对齐页**不是同一概念**，勿混） |
| 7 | **16 KiB 段对齐** | `Target.h:100` `getPageSize()`；`ARM64Common.h:30` `return 16*1024`；使用点 `Writer.cpp:1186-1187` | 常见坑：用了 4 KiB 段会对齐失败或运行时崩溃 |
| 8 | arm64 **stub / thunk** | `ConcatOutputSection.cpp:66-197` | `bl` 范围 ±128 MiB，跨 dylib 调用必须走 `__stubs` + `__stub_helper` |
| 9 | `__TEXT,__unwind_info` | `UnwindInfoSection.cpp`（32 KB） | arm64 ABI **要求**；MVP 可为每个函数生成一条最小的 unwind 项 | 
| 10 | `xcrun --show-sdk-path` → SDKROOT/usr/lib | driver 层 | `/usr/lib/crt1.o`、`-lSystem` |

**R3 开放校验项（必须由 Apple 官方文档确认，不能猜）**：
- Darwin arm64 的 red zone 语义。Apple 的 arm64 ABI 与通用 AAPCS64 在此处的差异，以及信号处理器是否允许踩 frame 下方。
- → 处理方式：在 `aarch64/frame.go` 里加显式的 target hook `reserveRedZone() uint32`，Darwin 取值在 M2 阶段经实测（写一个 signal handler + leaf 函数的测试程序）确认后固定。**不猜**。

### 6.3 Linux x86-64

| # | 环节 | LLVM 参照 |
|---|---|---|
| 1 | `.interp` + `PT_INTERP` | `createInterpSection` `SyntheticSections.cpp:93-98`；`Writer.cpp:2348-2350` |
| 2 | `.dynamic` + `PT_DYNAMIC` | `DynamicSection` `SyntheticSections.h:478`；填充 `:1200-1447` |
| 3 | `.dynsym` / `.dynstr` / `.gnu.hash` | `createSyntheticSections` `:4477-...` |
| 4 | `.got` / `.got.plt` / `.plt` / `.rela.plt` / `.rela.dyn` | 同上 |
| 5 | `PT_LOAD` 生成（至少 R + RW 两个） | `createPhdrs` `Writer.cpp:2330-2509` |
| 6 | **`PT_GNU_STACK`**（否则可执行栈） | `Writer.cpp:2470-2479` |
| 7 | 地址分配 + **收敛循环** | `finalizeAddressDependentContent` `Writer.cpp:1503-1667` |
| 8 | CRT 查找（driver） | gcc/clang `-print-file-name=crt1.o` |

**⚠️ PIE 陷阱**：现有路径在 Linux 上强制 `-no-pie`（`builder.go:829-830`），因为 legacy codegen 产出 `R_X86_64_32` 绝对重定位。
- MVP 阶段：**保持 `-no-pie` 语义一致**（出 ET_EXEC），确保与今日二进制可比。
- 后续再实现 PIC 序列切到 PIE。这是**刻意的行为保全**，不是能力缺失。

### 6.4 Windows x64

| # | 环节 | LLVM 参照 |
|---|---|---|
| 1 | `.drectve` 解析（**MSVCRT 全靠它**） | `parseDirectives` `Driver.cpp:550-670`，关键三行 `:610-613` |
| 2 | `.idata` 生成（HintName / Lookup / ImportDirectory / IAT） | `IdataContents::create` `DLL.cpp:719-...` |
| 3 | import thunk（`__imp_` 跳转桩） | `appendImportThunks` `Writer.cpp:1348` |
| 4 | PE 头：DOS stub + COFF header + PE32+ optional header + **16 个 data directory** | `writeHeader<pe32plus_header>` `Writer.cpp:1834-...` |
| 5 | section table | `OutputSection::writeHeaderTo` `:398-411` |
| 6 | **`.reloc` 基址重定位表** | `addBaserels()`（在 `assignAddresses` `:1782`） |
| 7 | **`.pdata` / `.xdata` unwind**（每个函数一项） | x64 ABI 硬要求 |
| 8 | PE checksum | `writePEChecksum()` `Writer.cpp:735` |

**MSVC 的 `defaultlib` 自述机制是本平台最大差异**，务必隔离在 `driver/crt_windows.go`：
- 解析顺序照抄 lld：`LIB` 环境变量 → `/libpath` → VS/WinSDK 自动探测 → clang 资源目录（`Driver.cpp:916-926` / `880-913` / `857-878`）。
- MinGW 侧降级路径：`foo.lib` → `libfoo.a`（`findLibMinGW` `Driver.cpp:726-734`）。

### 6.5 重定位类型最小集

Nolang 自己发出的重定位只是一小撮。三个平台各取所需，**超出范围的一律报诊断而非静默放过**（C4）：

| 语义 | Mach-O arm64 | ELF x86-64 | COFF x86-64 |
|---|---|---|---|
| 64-bit 绝对地址（数据） | `ARM64_RELOC_UNSIGNED` | `R_X86_64_64` | `IMAGE_REL_AMD64_ADDR64` |
| 32-bit 绝对（不可 -fPIC） | — | `R_X86_64_32` / `32S` | `IMAGE_REL_AMD64_ADDR32` / `ADDR32NB` |
| PC-relative 32（`call`/`jmp`） | `ARM64_RELOC_BRANCH26` | `R_X86_64_PC32` / `PLT32` | `IMAGE_REL_AMD64_REL32` |
| 地址页（ADRP 等价）/ GOT | `ARM64_RELOC_PAGE21` + `PAGEOFF12`；GOT 版 `GOT_LOAD_PAGE21` / `GOT_LOAD_PAGEOFF12` | `R_X86_64_GOTPCREL` / `GOTPCRELX` | `IMAGE_REL_AMD64_REL32`（间接） |
| 符号差（用于 jump table） | `ARM64_RELOC_SUBTRACTOR` + `UNSIGNED` | `R_X86_64_PC32` 配 addend | `IMAGE_REL_AMD64_SECREL` |
| TLS | `ARM64_RELOC_TLVP_PAGE21` | `R_X86_64_TPOFF32` / `TLSGD` | `IMAGE_REL_AMD64_SECTION`（简化） |

> **原则**：支持约 8–10 种/平台即可覆盖 Nolang 的全部需求。剩下的保持"不支持即报错"。

### 6.6 Triple 解析树

统一的 `Target` 结构（比现有的 `parseTargetPlatform` 更 rich，但**不替换它**——MVP 阶段从它取值）：

```go
type Target struct {
    Arch   Arch    // ArchARM64 | ArchX8664
    OS     OS      // OSMacOSX | OSLinux | OSWindows
    Format Format  // MachO | ELF | COFF
    Vendor string  // "apple" | "pc" | "unknown"
    ABI    ABI     // Darwin | GNU | MSVC
    Triple string  // 原始串
}
```

解析 fallbacks：
1. `-target` 显式提供 → 解析。
2. 未提供 → `DetectTarget()`（`builder.go:25`）拿 host triple。
3. 解析失败（如未知 arch）→ **报明确错误**，不静默回退 host（避免"看起来在交叉编译其实没有"）。
4. 组合校验：`ArchARM64 + OSWindows` MVP 不支持；`ArchX8664 + Darwin` 不在初期范围 → 报 "unsupported target combination"。

---

## 7. 与 LLVM llc / lld 的对照

### 7.1 llc 侧：照抄什么、不照抄什么

| LLVM 组件 | 位置 | 本方案处理 |
|---|---|---|
| 命令选项注册宏 `CGOPT(TY,NAME)` | `CommandFlags.cpp:43-66` | ✅ **照抄思想**：延迟注册 + `std::optional` 区分"未指定"与"指定为默认"。Go 里用 `Option[T]` 泛型 + 解析标志位表达 |
| `TargetMachine` 接口契约 | `TargetMachine.h:84-583` | ✅ 照抄关键的六个虚函数：`getSubtargetImpl` / `addPassesToEmitFile` / `createMCStreamer` / `addAsmPrinter` / `createPassConfig` |
| **TargetRegistry 函数指针注册表** | `TargetRegistry.h:468-477`、`RegisterTargetMachine<T>` `:1278-1281` | ✅ **强烈建议照抄**：比继承树更轻，注册成本一个表达式 |
| RM/CM 归一化（`std::optional` → target 自决） | `AArch64TargetMachine.cpp:152-187` | ✅ 照抄：Darwin/Windows 强制 PIC_（`:155-156`）；AArch64 只允许 small/tiny/large（`:171-174`），JIT 默认 Large（`:184-185`） |
| **三出口（asm / obj / null）** | `CodeGenTargetMachineImpl.cpp:164-227` | ✅ **照抄**：正好对应我们的 `-native=asm/obj` 两档 + 内部 null 模式用于测试 |
| 管线装配顺序 | `TargetPassConfig.cpp:966-1161` | ⚠️ **大幅精简**：只留 `addISelPasses → RA → PEI → block placement → AsmPrinter`，每阶段留空 hook |
| **完整的 30+ pass 管线** | 同上 | ❌ 不抄：MVP 阶段只有 ISel+RA+PEI 三个阶段 |
| MI → MCInst lower + AsmPrinter hooks | `AsmPrinter.h:622-653` | ✅ 照抄 **6 个 hook** 的划分；不抄 DwarfDebug/CodeView 那套 Handlers 观察者（先只做 debug 透传） |
| **MC 层的存储布局** | `MCSection.h:101-109` / `:602-638` | ✅ **照抄**：Content / Fixup / MCOperand 三池 + Fragment 只存索引 |
| **单一格式分派** | `MCAsmBackend.cpp:31-63` | ✅ **照抄**（最值得抄的一个函数） |
| TableGen 生成的 matcher | `AArch64GenAsmMatcher.inc` | ❌ 不抄：改用手写表驱动 ISel，规模可控 |
| SelectionDAG / GlobalISel | `CodeGen/SelectionDAG` / `GlobalISel` | ⏸ 接口预留，MVP 用 SimpleISel |

**平台差异的隔离策略**——这是 LLVM 用二十年试错得出的一条重要经验：

> 优先用 **`TargetLoweringObjectFile` 子类 + `MCTargetStreamer`** 下沉格式差异，**而不是在 AsmPrinter 里到处 `if (isMachO)`**。
>
> 佐证：AArch64 有三个 TLOF 子类（`AArch64TargetMachine.cpp:137-144`：MachO / COFF / ELF）。而 X86AsmPrinter 的 `emitFunctionBodyStart`（`X86AsmPrinter.cpp:121-137`）虽然看起来是无条件发射的 FPO，实际是由"只在 COFF 路径置位的 `EmitFPOData` 状态标志"驱动，不是格式分支——**数据驱动优于分支驱动**。

→ **本方案**：`arch.go` 的 `Arch` 接口负责"架构差异"，`mc.Context` + target-specific streamer 负责"格式差异"，二者正交。禁止在 nocg 主体里出现 `if target.OS == Darwin`。

### 7.2 lld 侧：照抄什么、不照抄什么

| LLVM 组件 | 位置 | 本方案处理 |
|---|---|---|
| Flavor 枚举 + `DriverDef` 数组 + StringSwitch | `DriverDispatcher.cpp:31-38` / `:127-137` | ✅ 照抄：aarch64/x86_64 × 三格式 → 一个 `Format` 枚举 + 注册表 |
| **`CommonLinkerContext`**（全局状态容器） | `CommonLinkerContext.h:32-45` | ✅ **照抄**：MachO 端口至今仍是全局变量风格、必须靠 `cleanupCallback` 手动 reset（`Driver.cpp:1770-1796`），因而**不可重入、不可多实例**。这是它的技术债，**不要重蹈覆辙**——我们从第一天就用 Context 对象 |
| Arena 分配器 `make<T>()` | `Memory.h:36-63` | ⚠️ 部分采纳：Go 有 GC，不需要 arena；但"链接期只生不死"可以省掉很多显式释放逻辑 |
| **错误处理模型**（继续跑 + checkpoint） | `ErrorHandler.h:9-66` | ✅ 照抄 |
| **"地址分配 ↔ 内容生成" 分离** | `OutputSection.h:61-72` 的注释 | ✅ **照抄，这是最重要的两条之一** |
| **重定位「分析遍 / 应用遍」分离** | `Relocations.cpp:1188` / `Target.cpp:165-168` | ✅ **照抄，这是最重要的两条之二** |
| ELF `RelExpr` 归一化层 | `Relocations.h:42-119` | ✅ 照抄思想（上百种 RelType → ~80 个语义表达式）。MachO 用属性表 `RelocAttrBits`（`Relocations.h:26-52`），两种思路都可行：**MVP 用 MachO 的属性表风格**（更简单），ELF 端口若类型膨胀再引 RelExpr |
| TargetInfo 虚表 | ELF `Target.h:31-211`；MachO `Target.h:43-159` | ✅ 照抄 MachO 的精简形态（约 15 个虚函数），**不抄 COFF 的裸 `switch(getArch())`**（`Chunks.cpp:496-...`）——5 个架构已是失控边缘 |
| 段排序表 `segmentOrder` / `sectionOrder` | `OutputSegment.cpp:81-92` / `:94-179` | ✅ **照抄**：这是 Mach-O 端口经验的结晶（如 `__text` 必须为 `-6` 且最大最先，因为 thunk 算法依赖；`__LINKEDIT` 强制最后；签名必须最后；zerofill 必须在 segment 末尾因为 dyld 靠 `fileSize < vmSize` 识别） |
| ELF 收敛循环 | `Writer.cpp:1503-1667` | ✅ 照抄（含 thunk 上限 30 / 地址轮上限 5 双重保护） |
| MachO `outOfRangeVA` hack | `Symbols.cpp:93-103` | ✅ **照抄**（精巧：未 finalize 的地址返回"必然越界"值，让 thunk 单次流式算法自洽） |
| MachO 裸结构体解析 | `InputFiles.cpp:1044-1112`；`MachOStructs.h:22-43` | ✅ **照抄这条路线**：lld MachO 端口完全不用 `llvm/object`，裸 `reinterpret_cast` + 自家 endian-safe 结构。依赖少、性能好。**对我们尤其合适**——只需要支持 arm64 一种 Mach-O |
| ELF 用 `llvm::object::ELFFile<ELFT>` | `InputFiles.h:177-179` | ➡️ 我们没有 LLVM 可用 → **自写 `elf64.go` 的最小解析器**（只需 shdr/symtab/rela） |
| COFF 用 `COFFObjectFile` | `InputFiles.cpp:190-191` | ➡️ 同上，自写 `coff.go` |
| **linker script**（ELF 最大复杂度源） | `LinkerScript.cpp` 61 KB | ❌ **不抄**：提供最小 stub，`hasSectionsCommand` 恒 false |
| LTO 分支 | 三端口各一套 | ❌ **不抄**（§2.2） |
| dSYM | lld 根本不生成 | ❌ 无需理会 |

### 7.3 一句话对照

| | LLVM | Nolang 本方案 |
|---|---|---|
| IR→机器码 | `llc`（`llcdriver.cpp` 916 行 + CodeGen 数十万行） | `src/native` **nocg**（约 20k 行，砍掉 DAG/GlobalISel/TableGen/scheduler/macro-fusion 等） |
| 链接 | `lld`（ELF 端口 ~45k 行） | `src/link` **nold**（约 10k 行，砍掉 linker script/LTO/ICF/符号版本/符号品种矩阵） |
| 支撑语言的广度 | 通用 C/C++/Rust/… | **只有 Nolang**——这是规模能压缩 5–10 倍的根本原因 |

---

## 8. 编译开关（DR-6）落地清单

| 项 | 位置 | 改动 |
|---|---|---|
| 全局 flag 解析 | `src/cmd/no/main.go`（紧邻 `parseOptFlag`，:64-78 之后） | 新增 `parseNativeFlag(args []string) []string`；`-native` → `NOLANG_NATIVE=link`；`-native=asm|obj|link` → 透传 |
| usage 文本 | `main.go` `printUsage()`（:160） | 追加 `-native[=asm|obj|link]` 一行，注明**默认关闭**与三档含义 |
| 单一读取器 | `src/native/options.go` | `func NativeMode() Mode`，**未知拼写 → Off**（对齐 `MIROptLevel()`，`mir/opt.go:71-96`） |
| codegen 分支 | `transpiler.go`（调用点 :2858，函数体 :2966） | `Mode != Off` 时走 `native.CompileToObject(mod)` 并把结果存进 `t.nativeObj`，同时**跳过** :3032 的 `verifyMIRIRViaOpt`、改调 `mi.Verify()` |
| IR 补丁短路 | `builder.go:686` | `Mode != Off` 时跳过 `toOpaquePointers` |
| opt 短路 | `builder.go:713` | `Mode != Off` 时跳过外调 `opt` |
| llc 替换 | `builder.go:752-789` | `Mode == Off` 走原 llc；否则用 `t.nativeObj`（`asm` 档则落为 `<tmp>/<name>.s` 交给后续外部 cc） |
| link 替换 | `builder.go:791-863` | `Mode == Link` 时走 `native/driver` + `src/link`；`asm`/`obj` 档仍走外部 cc |
| 工具链检查放松 | `builder.go:67` `CheckToolchain` | `Mode == Link` 时**不再要求** `llvm-config` / `clang` 存在（G2 的一部分） |

> **`Link` 档下 `CheckToolchain` 必须放松**，否则开关表面上开了、实际上还是过不了 toolchain 预检。这一点极易遗漏。

---

## 9. 分阶段路线图

**前置作业**（不属于 native 后端，但必须先做）：

| 编号 | 任务 | 理由 |
|---|---|---|
| **PRE-1** | 修 `codegen.go:2523` 让 `emitPrelude` 写出真实 triple | IR 里的假 triple 是当前的设计债；native 绕过它不等于可以不管 |
| **PRE-2** | 将 `src/build/builder.go:317-329` CI 脚本里的 triple 表（`triple_for()`）**下沉进 Go**（对齐 `PlatformKeys`） | 目前 CI 脚本（`.github/actions/build-nolang/action.yml:317-329`）里有一张 triple 表、Go 里有两张不全的（builder.go:25 / :202）；必须先统一，否则 native 后端会引入第四张 |
| **PRE-3** | 建立双路差分基线（§10.1） | 没有基线就无法证明"行为不变" |

### 主体里程碑

| Milestone | 内容 | 交付物 | 门禁（必须通过才算完成） |
|---|---|---|---|
| **M0 — 插入点**（约 0.5k 行） | `-native` 开关 + 两处插入点 + `NativeMode()` + `Mode=Off` 时行为逐字节不变 | 可合并，无功能变化 | ① `go test ./...` 既有失败数不变 ② `-native` 未设时全 corpus 构建产物哈希与今日一致 ③ `Link` 档下 `CheckToolchain` 已放松、`PATH` 中移除 LLVM 后仍能编译 |
| **M1 — MI + Verifier**（约 1.5k 行） | `mir.Module → MI`（目标无关）+ `mi/verify.go` + `lower/` | MI dump 可打印 | ① 全 corpus lowering 无 fatal ② Verifier 在故意破坏的 MI 上必报错（**实测判别力**：把检查短路确认恰好该失败的失败） ③ 性能：MI 构造耗时 < 现有 `EmitLLVM` 的 30% |
| **M2 — arm64 ISel + encoder + PEI**（约 4k 行） | aarch64 全套 + LinearScan + frame；产出 `-native=asm` | macOS arm64 能出汇编 | ① **编码器对拍**：自研 encoder 产出的每条指令 vs LLVM `llvm-mc` 汇编结果**字节级相同**（覆盖 100% 使用到的 opcode） ② `-native=asm` 全 corpus 经外部 assembler + clang 产出可执行，stdout+rc 与外部 llc 路径**全等** ③ R3 红区实测结论入库 |
| **M3 — Mach-O writer + driver + linker MVP**（约 3.5k 行） | `obj/macho.go` + `driver/crt_darwin.go` + `link/macho` | `-native=link` 在 macOS arm64 产出可执行 | ① 全 corpus 双路差分 stdout+rc 全等 ② 二进制可被 `codesign -v` 通过、`otool -l` 结构健全、`nm` 符号正确 ③ **零外部工具**：拦截 `exec.Command` 断言 ④ `no run` 端到端通过 |
| **M4 — x86_64 ELF 全套**（约 5k 行） | x86_64 ISel/encode/abi(SysV) + `obj/elf.go` + `driver/crt_linux.go` + `link/elf` + **收敛循环** | Linux x64 `-native=link` | ① 同上的差分门禁 ② `readelf -a` 结构合规、`readelf -l` 有 PT_INTERP/PT_DYNAMIC/PT_GNU_STACK ③ PIE 行为与今日 `-no-pie` 一致 ④ 交叉构建验证（macOS 上交叉出 Linux ELF 并放到容器里跑） |
| **M5 — x86_64 Windows 全套**（约 5k 行） | x86_64 ABI(MSVC) + `obj/coff.go` + `driver/crt_windows.go`（`.drectve`） + `link/pe`（`.idata`/`.reloc`/`.pdata`） | Windows x64 `-native=link` | ① 同上差分门禁（含 `-lws2_32`，对齐 `builder.go:836-838`） ② `dumpbin` 结构合规 ③ MSVC 与 MinGW 两条 toolchain 各跑一遍 |
| **M6 — 水位提升**（~2k 行） | 块布局、栈槽着色、简单 peephole、MachO thunk/stub 完善、ELF TLS 松弛 | 性能贴近 `-O2` | ① `bench/` 现有 fib/md5 基准：native vs external，差距 ≤ 15% ② 体积差 ≤ 10% |
| **M7 — 内化 opt（真正零依赖）** | 把外部 `opt -O2` 也换成内部实现（`src/mir/opt.go` 扩展）；`-native` 不再需要任何 LLVM | **完全自举后端** | ① `PATH` 中完全无 LLVM 工具仍能 `no build/run/test` ② 全差分门禁绿 |

> **规模诚实提示**：M0–M5 估计 **20k–25k 行**（含测试），M6–M7 再加 5k–8k，总计 **25k–35k 行**（比 §5.1 的粗估低，因为 Nolang 的函数形态固定，大量通用 ISel 规则根本不必实现）。按"一次一件事"的节奏，这是一个**多季度**级别的工程，**不应在单个变更里推进多个 M**。

---

## 10. 验证策略（工业级正确性的核心）

### 10.1 双路差分 harness（CI 级）

对 `tests/**/*.no` 与 `test/**/*.no` 每个用例跑两条路径：

```
路径 A（基线）：external opt → llc → clang      ← 今日行为
路径 B（实验）：native nocg → nold              ← 新路径
断言：stdout 全等 && exit code 全等
```

基建现成：`scripts/diff_tests.sh`，以及 `.workbuddy` 技能 `nolang-mir-parity-sweep` 的**隔离二元组**（`tar` 复制 `src` + `git show HEAD:<f>` 还原自己档案）归因方法——直接复用它来做"native vs external"的归因。

> ⚠️ 已知项目陷阱，差分前**必须先处理**：`src/checker/stdsig_gen.go` 是大文件级噪音 diff（对齐列重排），**不许靠数行数**比，要用脚本解析 `"key": {…}` 表再比。

### 10.2 编码器字节级对拍（性价比最高的一招）

```
对每条测试指令：
    self.encode(inst)  ──┐
                         ├── 必须逐字节相同
    llvm-mc/llvm-as  ────┘
```

把使用到的 opcode 全空间对拍一遍，能一次性消灭 encoder 层绝大部分 bug，而且**外部工具只用于测试、不用于运行时**（不违反 G2）。

### 10.3 单元测试判别力要求

沿用项目既有铁律：
- `go test ./mir/` 必须全绿；新增测试要**实测判别力**——把被测检查短路成 `if false && …`，确认恰好该失败的测试真的失败，然后立即恢复 + `git diff` 干净。
- `go test ./...` 既有失败基线（非回归）：`build` 4 + `checker` 2 + `fmt` 1。归因前先 `git status` 看是否被并发会话改过。

### 10.4 新增门禁建议（针对 🔴 CI 无测试门禁这一已知风险）

在 `.github/workflows/build.yml` 补一条 job：

```
go test ./...  →  no test test/std/  →  语料 build 冒烟  →  双路差分  →  no vet src/std
```

并把 `-native=link` 的差分列为**独立 job**（跨平台 matrix：macOS arm64 / ubuntu x64 / windows x64）。

### 10.5 反向门禁：禁止 shell out（守护 G2）

一条负向测试，专门守护 G2：

```go
// 在 Link 档下构建，若任何环节 exec 了外部工具则失败
func TestNativeNeverShellsOut(t *testing.T) { … }
```

实现方式：`exec.Command` 走一个可替换的钩子（或 `PATH` 置空 + 拦截）。

---

## 11. 风险登记

| ID | 风险 | 等级 | 缓解 |
|---|---|---|---|
| **R1** | **规模失控**——低估了 ISel/encode 的工作量 | 🔴 高 | M2/M4/M5 是最大的三块。**每个 M 独立可交付、可回滚**；任一 M 发现失控立即收缩范围（如 M5 只做 MinGW 不做 MSVC） |
| **R2** | macOS arm64 **代码签名**漏做 → 二进制不能跑 | 🔴 高 | M3 门禁显式要求 `codesign -v` 通过；`shouldAdhocSignByDefault` 的逻辑（arm64+macOS 才默认签）照抄 `Driver.cpp:1217-1225` |
| **R3** | Darwin arm64 **红区语义**未核实 → 间歇性崩溃 | 🟠 中高 | 见 §6.2：做成 target hook + 实测固化，**不猜** |
| **R4** | 16 KiB vs 4 KiB **页对齐混淆**（段对齐 16K，codesign block 4K） | 🟠 中高 | 代码里用两个不同命名常量 `SegmentPageSize` / `CodesignBlockSize`，禁止共用；加注释直指 `ARM64Common.h:30` 与 `SyntheticSections.h:525-526` 的区别 |
| **R5** | Windows `.drectve` MSVC 生态依赖复杂 | 🟠 中高 | M5 先做 MinGW 路径（规则简单），MSVC 后置；`.drectve` 解析限定只处理 `defaultlib`/`nodefaultlib`/`subsystem` 三个指令，其余报 unsupported |
| **R6** | 并发工作区编辑导致归因错乱 | 🟠 中（项目已知问题） | 沿用既有铁律：A/B 归因前先 `git diff --stat` 存快照；"同一变量两次结论相反"先怀疑树变了；用 `nolang-mir-parity-sweep` 的隔离二元组方法 |
| **R7** | `no fmt` / `no vet` 口径与 build 不一致，引发"注解被删"死循环 | 🟡 中 | 新增代码严格加 `.gofmt` 检查；不改 `src/std`；**绝不对 `src/std` 跑 `no fmt -w`**（已知会丢 `#{index-out}`） |
| **R8** | 交叉编译验证不便（本机是 macOS arm64） | 🟡 中 | M4/M5 必须靠 CI matrix 验证；本地用容器/QEMU 做冒烟。**不要仅凭 macOS 上的交叉产物自证** |
| **R9** | 引入过早的抽象导致返工 | 🟡 中 | 接口一次到位（`ach.Arch` / `obj.Writer` / `link.WriteArch`），实现从简；但**不要**为尚未支持的三元组建抽象（如 `aarch64-windows`） |
| **R10** | 差分基线本身被并发 churn 污染 | 🟡 中 | PRE-3 建立的基线要 commit 冻结 + 记录 commit hash，并在 CI 里校验未漂移（对应已知 🔴 金标过期风险） |

---

## 12. 开放问题（需拍板后才能进 M1）

| # | 问题 | 我的倾向 | 影响 |
|---|---|---|---|
| **Q1** | `-native` 是**全局 flag**（跟 `-v`/`-opt` 同级，三条路径共享）还是 `build` 子命令 flag？ | **全局 flag**：要对 `build`/`run`/`test` 三条路径一致生效，跟 `-opt` 完全同构（`main.go:61-63` 注释已经论证过这个选择） | 决定代码放置位置 |
| **Q2** | `Link` 档下是否仍需 `CheckToolchain` 通过？ | **不需要**——这正是"去依赖"的意义（`builder.go:67` 要在 Link 档放松） | 影响用户体验与 G2 定义 |
| **Q3** | Windows 目标初期走 **MSVC** 还是 **MinGW**？ | **先 MinGW**（`x86_64-pc-windows-gnu`），规则简单；MSVC 需要整套 VS/WinSDK 探测 | M5 工作量差别很大（估计相差 1.5k 行） |
| **Q4** | Linux 初期是否保持 `-no-pie`？ | **是**——行为保全，确保差分可比；PIC/PIE 置到 M6 | 影响 M4 门禁写法 |
| **Q5** | 是否需要独立的 `no objdump` / `no nm` 子命令辅助调试？ | M2 之后**很有价值**（否则只能靠外部 `otool`/`readelf`），但不阻塞主体 | 排期 |
| **Q6** | 何时把 M7（内化 `opt`）提上日程？ | 完成 M5 且三平台稳定之后，再评估。M7 的价值是**真正零依赖**，但收益递减 | 决定是否追完全自举 |
| **Q7** | 是否需要把 `nocg`/`nold` 单独做成可执行命令？ | 建议**先只做库包**；后续按需暴露 `no nocg` / `no nold` 便于调试 | 影响接口设计自由度 |

---

## 附录 A：本报告引用的关键源码位置

**Nolang（`/Users/lizongying/IdeaProjects/no`）**

| 用途 | 位置 |
|---|---|
| 最终产物驱动的唯一汇流点 | `src/build/builder.go:662` `buildLLVMInternal` |
| IR 写入 / opaque 补丁 | `builder.go:686` / `toOpaquePointers` 定义 `:653`，正则 `:647-650` |
| 外部 opt | `builder.go:713` |
| 外部 llc（**对标 llc 的位置**） | `builder.go:770`，`-mtriple` `:768`，`--fp-contract` `:762-766` |
| 外部 cc（**对标 lld 的位置**） | `builder.go:843`（zig）/ `:845`（clang）；Linux `-no-pie` `:829-830`；Windows `-lws2_32` `:836-838` |
| codegen 侧插入点 | `src/build/transpiler.go:2858`（调用）/` :2966`（定义） |
| 需要短路的 opt/llc 预检 | `transpiler.go:2894`（定义）/` :3032`（调用） |
| Triple 处理（三张表） | `builder.go:25` `DetectTarget`；`builder.go:202` `parseTargetPlatform`；`src/package/platform.go:6` `PlatformKeys` |
| MIR 目标平台单例 | `src/mir/platform.go:17` `mirTarget`；`SetTargetPlatform` `:33` |
| **IR triple 硬编码（设计债）** | `src/mir/codegen.go:2523` |
| opt 阶段（内部 MIR 优化器） | `src/mir/opt.go:74` `MIROptLevel()`；调用点 `src/mir/hir2mir.go:1408` |
| 开关范式样板 | `src/cmd/no/main.go:64` `parseOptFlag`；`main.go:3072-3074`（为什么用环境变量而不是传参） |
| 默认-OFF 开关样板 | `src/mir/mir.go:38` `FieldPtrLayout`；`src/mir/spawn_graph.go:781` |
| Windows msvcrt shim | `src/mir/builtin_win_shims.go`、`src/mir/forward_call.go` |
| 既有差分基建 | `scripts/diff_tests.sh`、`scripts/mir_golden.sh`；`.workbuddy/skills/nolang-mir-parity-sweep` |

**LLVM（`/Users/lizongying/IdeaProjects/llvm-project`，HEAD `3171d08fc`）**

| 用途 | 位置 |
|---|---|
| llc 主驱动（**llc.cpp 仅 16 行 shim**） | `llvm/tools/llc/lib/llcdriver.cpp:375` `llcMain`；`:505` `compileModule` |
| llc 无 Options.td，手写 cl::opt | `llcdriver.cpp:85-263`；共享 flag `llvm/lib/CodeGen/CommandFlags.cpp:122-497` |
| Target 选择 | `llcdriver.cpp:643-645`；`llvm/lib/MC/TargetRegistry.cpp:124-159` |
| 建 TargetMachine | `llcdriver.cpp:652-653`；签名契约 `TargetRegistry.h:468-477` |
| **三出口（asm/obj/null）** | `llvm/lib/CodeGen/CodeGenTargetMachineImpl.cpp:164-227` |
| 管线装配 | `CodeGenTargetMachineImpl.cpp:115-140`；`llvm/lib/CodeGen/TargetPassConfig.cpp:966-1161` |
| 三种 ISel 的路径选择 | `TargetPassConfig.cpp:836-848` |
| RM/CM 归一化（Darwin/Windows 强制 PIC） | `AArch64TargetMachine.cpp:152-187` |
| TLOF 三子类（MachO/COFF/ELF） | `AArch64TargetMachine.cpp:137-144` |
| **MC 存储布局**（最值得照抄） | `llvm/include/llvm/MC/MCSection.h:101-109`、`:602-638` |
| emitInstruction → encode → fixup | `llvm/lib/MC/MCObjectStreamer.cpp:402-449`、`:451-487` |
| layout + 不动点 relax + fixup 求值 | `llvm/lib/MC/MCAssembler.cpp:668-730`（`layout`）、`:804-815`（`Finish`） |
| **单一格式分派**（最值得照抄的第二处） | `llvm/lib/MC/MCAsmBackend.cpp:31-63` |
| ObjectWriter 写出入口 | ELF `llvm/lib/MC/ELFObjectWriter.cpp:1000`/`:1412`；MachO `llvm/lib/MC/MachObjectWriter.cpp:795`；COFF `llvm/lib/MC/WinCOFFObjectWriter.cpp:1064`/`:1277` |
| lld 端口入口与分派 | `lld/include/lld/Common/Driver.h:25-67`；`lld/Common/DriverDispatcher.cpp:127-150` |
| **Context 容器**（MachO 端口的反面教材） | `lld/include/lld/Common/CommonLinkerContext.h:32-45`；`lld/MachO/Driver.cpp:1770-1796` |
| 错误模型 | `lld/include/lld/Common/ErrorHandler.h:9-66` |
| MachO link 全阶段 | `lld/MachO/Driver.cpp:1764-2562`；`lld/MachO/Writer.cpp:1370-1423` |
| MachO 裸结构体解析路线 | `lld/MachO/InputFiles.cpp:1044-1112`；`lld/MachO/MachOStructs.h:22-43` |
| **地址/内容分离约定** | `lld/MachO/OutputSection.h:61-72` |
| MachO `outOfRangeVA` hack | `lld/MachO/Symbols.cpp:93-103` |
| MachO 段排序表 | `lld/MachO/OutputSegment.cpp:81-92`（segment）、`:94-179`（section） |
| dyld opcode 生成 | `lld/MachO/SyntheticSections.cpp:277-297`（rebase）、`626-655`（bind）、`1001-1008`（lazy）、`SyntheticSections.h:520-543`（codesign） |
| **lld 不找 CRT**（实证） | `lld/ELF/` 内 `crt1|crti|crtn|Scrt1` 仅 4 处命中且全为注释 |
| ELF 收敛循环 | `lld/ELF/Writer.cpp:1503-1667` |
| ELF 分析遍 / 应用遍 | `lld/ELF/Relocations.cpp:1188`；`lld/ELF/Target.cpp:165-168` |
| ELF RelExpr 归一化 | `lld/ELF/Relocations.h:42-119` |
| COFF `.drectve` MSVCRT 机制 | `lld/COFF/Driver.cpp:610-613`；库探测 `880-926` |
| PE 头与 data directory | `lld/COFF/Writer.cpp:1834-...`；`:1970-2032` |
| `.idata` 生成 | `lld/COFF/DLL.cpp:719-...` |

---

## 附录 B：术语对照

| Nolang 新名 | LLVM 对应 | 含义 |
|---|---|---|
| **nocg** | llc | Nolang Code Generator：MIR → 目标文件/汇编 |
| **nold** | lld | Nolang Linker：目标文件 → 可执行文件 |
| **MI** | MachineInstr | 机器指令 IR（虚拟寄存器） |
| **MCInst** | MCInst | 已 lower 到物理寄存器的具体机器指令 |
| **Fragment** | MCFragment | section 内的字节片段（数据/对齐/可松弛指令） |
| **Fixup** | MCFixup | 片段内待解析的位置（可就地解析或转成 Relocation） |
| **legalize** | Legalizer | 把不支持的宽度/寻址模式拆成支持的 |
| **ISel** | Instruction Selector | 虚拟 opcode → 具体机器指令 |
| **RA** | Register Allocator | 虚拟寄存器 → 物理寄存器 |
| **PEI** | PrologEpilogInserter | 帧布局、序言尾声、CSR 保存 |
| **Relaxation** | relaxation | 短编码失败后切换长编码的不动点过程 |
| **driver** | clang driver | triple → CRT/SDK/系统库的责任方（nold 不管） |
