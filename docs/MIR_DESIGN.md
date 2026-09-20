# Nolang MIR — 工业级中端中间表示设计（实施记录 v2.26）

> **当前状态（截至 2026-09-17）**：legacy 后端已删除，MIR 是唯一后端（`NOLANG_MIR` 未设置即 MIR-only；`=0`/`=2` 明确报错，`=1` 保留为 lowering 转储）。默认口径 `no build` 通过率 **92.2%**（388/421）；**红线已达标**：MIR 专属运行时崩溃 = 0、MIR 专属 gap = 0。最新全量扫描 `SAME=409 / DIVERGE=0 / REGRESS=0 / BOTH_FAIL=13`（约 11）。切换默认后端不改变任何测试的成败，只是停止用 legacy 掩盖 MIR 的缺口。
>
> **待修**：#84（json 运行期 SIGSEGV）、#85（静默错误编译底层根因，`loadVal` 护栏已落地但未修底层）。详见 §13.3 与 §16。
>
> **近期已修**：#83（`afa8d92`，sroa 大聚合爆炸）；5 处同形状空守卫体 bug（`regexp`/`x509`/`multipart`×2/`sse`）。
 —— 即切换默认不改变任何测试的成败，只是停止用 legacy 掩盖 MIR 的缺口。详见 §13.3。
> 作者：编译器工作流
> 关联：`src/hir`（HIR）、`src/parser/tohir.go`（AST→HIR）、`src/mir`（本层）。~~`src/build/llvm`（legacy LLVM 后端）~~ 已删除（见 §13.3）

---

## 0. 本文档的定位与 v1→v2 变更

v1（本文档前身）是一份**前向设计**：描述了 `NOLANG_MIR=1` 审计模式、`MIR→HIR` 桥（Stage 1）、以及"MIR 作为内存安全审计器运行"的路线。但实际实现**超越了 v1 的路线**：MIR 直接跳到了 `MIR→LLVM` 直发（v1 的 Stage 3），并且 env 开关语义从 v1 的 `0/1` 演进为 `0/2/3`。本 v2 把文档**对齐到真实已落地的架构**，并补充：

- 真实的 `NOLANG_MIR=0/2/3`（及 `=1` 调试）语义；
- 全量 corpus 覆盖实测（当前口径：`tests/**/*.no`=421 → `MATCH=367`、`DIVERGE=0`、`MIR 专属 gap=1`（`test-diff-debug`）、`LEGACY_FAIL` 多为预存/通用 bug；详见 §10.1）；
- 已闭环的 **63 个** MIR 专属运行时崩溃/代码生成修复（#1–#63），分类目录见 §12 摘要。red-line 里程碑：**#41** 闭环 C 家族 3 个运行时崩溃；**#42–#50** 覆盖 newtype 展开 / 跨模块调用 / 切片 / 窄整型 / 字符串方法 / 类型别名等常规语义缺口。
- 当前已知的 gap 家族与待修项：见 §13.3（BOTH_FAIL 余集约 11，含 #84/#85）与 §16（开放风险）。
- 覆盖扫描方法论（`scripts/mir_cov.py` / `mir_sweep.py`；⚠️ 两者用**陈旧的仓库根 `no`**，权威扫描应改用 `./bin/no` 并覆盖 `tests/**/*.no`）。

> ⚠️ 历史实施约束（**已随 legacy 删除失效，仅作追溯**）：MIR 成熟期曾要求任何 MIR 失败在 `NOLANG_MIR=2` 下**必须安全回退**到 legacy HIR 路径（strangler-fig），`NOLANG_MIR=3` 关闭回退以暴露覆盖缺口。删除 legacy 后端后，`=0`/`=2` 均为明确报错，回退机制不存在；回归保护改由冻结基线承担（§13.3⑥）。

---

## 1. 动机：为什么需要 MIR（不变）

当前 Nolang 的内存安全（无 GC、deferred move + scope-exit destruction）由 `src/build/llvm/` 下约 450KB 的 emitter 在生成 LLVM IR 时**手工散布**地实现，导致三大 bug 族：

1. **双重释放 / 使用已释放（double-free / UAF）**：move 后原变量仍被结尾 `emitHeapFree` 释放。
2. **值/指针混淆导致的越界（OOB）**：返回类型传播错误使 alloca 类型与 store 类型不一致（`%vec` 被当作 `%str-long`、定宽数组被当作 `[0 x i16]`）。
3. **生命周期/借用关系无法静态验证**：新增语言特性（coroutine、FFI、泛型）时极易引入回归。

**结论**：内存安全逻辑必须从一个"散布在 450KB emitter 中的隐含约定"，提升为一个**显式、可分析、可验证、可机械翻译**的中间表示——MIR。LLVM 后端退化为 MIR 的**机械翻译器**，不再自行决定释放点。

---

## 2. 在编译 pipeline 中的位置（真实）

```
        parser         checker           HIR                MIR                LLVM (+opt/llc/clang)
  .no ──────► AST ─────────► 标注类型 ───► hir.Package ───────► mir.Module ───────► LLVM IR ──► 可执行
                         (PopulateInferredTypes)   (ASTToHIR)      (LowerHIR)        (EmitLLVM)
```

- HIR（`src/hir`）：去指针化的 AST 扁平竞技场，携带声明/推断类型字符串，但**没有 CFG、没有内存语义、没有 SSA**。
- **MIR（`src/mir`）**：从 HIR 生成；显式建模 basic block、owned/borrowed 值、move/clone/drop。
- 开关策略（strangler-fig，`src/build/transpiler.go:2780`）：

| `NOLANG_MIR` | 行为 | 回退 |
|---|---|---|
| 未设置 / `0` | 现有 HIR 路径，`GenerateHIR` 直发（legacy，默认） | — |
| `1` | `LowerHIR`+`Analyze` 打印 MIR+内存诊断到临时文件，然后**总是回退** legacy（无行为变化，调试用） | 总是 |
| `2` | 从 MIR **直接发射** LLVM IR；任何 gap/panic/opt-verify 失败 → **回退** legacy | 是（strangler-fig） |
| `3` | 从 MIR **直接发射**，关闭回退（Stage 3 全量 corpus 门禁） | **否**——任何 gap 直接让构建失败，以暴露覆盖缺口 |

`scripts/mir_cov.py` 即以 `MIR=2`（对照，应全绿/回退）与 `MIR=3`（门禁，应 rc=0）对照 `tests/*.no` 全量扫掠。

### 2.1 可达性函数级 lazy lower

从 `main` 出发建函数 worklist；`KCall`/`KFuncDef`（方法）引用到的函数从 HIR `pkg.Top`（已 merge user+std）取出一并 lower；未被引用的 std 函数不进入 MIR（死代码消除）。天然实现"user 的 hir 生成 mir，用到的 std hir 才生成 mir"。

---

## 3. 设计原则（不变）

1. **显式内存所有权**：每个值携带 `Owned`/`Borrowed`，释放点由 MIR 分析唯一决定。
2. **CFG 一等公民**：basic block + 前驱/后继 + 支配树（`cfg.go` 迭代支配算法）。
3. **准 SSA**：局部值用 `ValueID` 编号、单次定义；参数/Alloc 是定义点；块汇合处 `OpPhi`。
4. **可验证**：`Validate` 结构校验 + `Analyze` 内存诊断（use-after-move / duplicate-drop / missing-drop / borrow-escape）。
5. **机械翻译**：`EmitLLVM` 逐 op 翻译 + 调用约定映射，不含语义判断。
6. **单向依赖**：`mir` 依赖 `hir`（`src/parser`→`hir`→`mir`），**不** import `build/llvm`。

---

## 4. 数据模型（已实现，`src/mir/mir.go`）

```go
type Type struct {
    ID    TypeID
    Raw   string   // nolang 类型串: "str","i64","[]byte","?db","[3]i64","vec", struct 名...
    Kind  TypeKind // Int/Float/Bool/Char/Str/Slice/Array/Map/Option/Ptr/Struct/Func/Void/Unknown
    Owned bool     // true ⇒ 堆拥有, 需要且只需一次 Drop
    Elem  TypeID   // Slice/Array/Option/Ptr 的元素类型
    Sizes []int64  // 定宽数组维度
    Func  *FuncType // KindFunc 的 by-value 参数/结果签名
}
```

**Owned 分类**（`ClassifyOwnership`）：`str`/`vec`/`[]T`/`map`/`?T(其中 T owned)` ⇒ `Owned=true`；struct 任一字段 Owned ⇒ 整体 Owned；`&T` ⇒ false；标量及纯标量 struct ⇒ false。

**值/指令/块/函数/模块**：见 v1 §4.2，已实现（`mir.go`）。每个 MIR 值由 codegen 阶段**统一背后一个 LLVM alloca slot**（`codegen.go:17-18`），ownership/drop 因此平凡：`Drop` → `call void @str_free(...)`。

### 4.1 运行期 LLVM 结构（codegen 约定）

- `%vec` = `{ i64 len, i64 cap, i64 data_ptr }`（堆 slice）
- `%str-long` = `{ i64 len, i64 cap, i8* data }`
- `%option` = `{ i64 tag, i64 payload }`（tag：0=some/ok，1=nil/none，2=err）
- `%txt` = `[255 x i8]`

---

## 5. 指令集（内存感知，已实现 `mir.go` Op 枚举）

| 类别 | Op | 语义 |
|---|---|---|
| 终结器 | `OpReturn`/`OpBr`/`OpCondBr`/`OpSwitch` | 控制流 |
| 内存 | `OpAlloc`/`OpLoad`/`OpStore`/`OpMove`/`OpClone`/`OpDrop`/`OpBorrow` | 所有权原语 |
| 数据 | `OpConst`/`OpGetField`/`OpSetField`/`OpStructLit`/`OpOptionWrap`/`OpIndex`/`OpIndexStore`/`OpSliceOp`/`OpLen`/`OpCap` | |
| 算术/逻辑 | `OpAdd`/`OpSub`/`OpMul`/`OpDiv`/`OpMod`/`OpNeg`/`OpNot`/`OpAnd`/`OpOr`/`OpBitAnd`/`OpBitOr`/`OpXor`/`OpShl`/`OpShr` | |
| 比较 | `OpEq`/`OpNe`/`OpLt`/`OpLe`/`OpGt`/`OpGe`/`OpStrEq` | `OpStrEq` 走 `@str_eq`（str 是 struct，不能 `icmp`） |
| 控制值 | `OpPhi` | 块汇合 SSA |
| 调用 | `OpCall`/`OpCallExtern`/`OpCallFFI` | 用户/外部/FFI |
| 转换 | `OpCast`/`OpTxtFromStr` | 类型转换（zext/sext 集中于此） |

**调用约定**：owned 返回值走 sret（调用方 `OpAlloc` 隐式首参），callee 写入后调用方持有；借用参数按指针传；owned 参数默认 move 或 clone。

---

## 6. 所有权与生命周期模型（不变，见 v1 §6）

- Owned 值一生：`OpAlloc(Owned)` → 使用 → 恰好一次 `OpDrop`（最后使用之后/块出口）。
- `OpMove` 转移释放责任，源不再 drop ⇒ 原生解决 double-free。
- `OpClone` 深拷贝，二者各自需一次 Drop。
- `OpBorrow` 取引用，生命周期限定在 owner 支配域内。

---

## 7. CFG 与（准）SSA（已实现 `src/mir/cfg.go`）

- `BuildCFG` 由 `Term.Targets` 推导每块的 `Preds/Succs`。
- `Dominators` 迭代支配算法（用于 borrow 生命周期界定）。
- SSA：局部值单次定义；条件分支汇合处 `OpPhi` 合并。

---

## 8. 分析 Pass（MIR 的核心价值，`src/mir/analysis.go`）

### 8.1 Liveness（反向数据流）
`liveOut(b)=∪liveIn(s)`；`liveIn(b)=use(b)∪(liveOut(b)−def(b))`。owned 值的"使用"含其释放前最后一次读；`OpDrop` 是 owned 值最后一次使用。

### 8.2 所有权分类
`ClassifyOwnership` 给每个值打 `Owned`/`Borrowed`。

### 8.3 Drop 插入算法（修复 double-free / 缺失释放）
对每函数每个 owned local，在其最后使用之后插入恰好一次 `OpDrop`；move 已转交释放责任 ⇒ 源不再 drop。

### 8.4 Move / Borrow 检查器（诊断，不直接改 IR）
- use-after-move：nolang `OpMove` 是按位拷贝（emitMove 做 load+store），源字节保持有效，故 moved 值其后 **read 安全、不诊断**（曾因此阻塞 test-std-hash.no：md5 把 `data []byte` move 进函数后多次读）。
- duplicate-drop / missing-drop / borrow-escape：分析后某 owned 值 0 或 >1 次 drop ⇒ 诊断。

### 8.5 OOB / 类型一致性（修复"值/指针混淆"）
- `Validate`（`validate.go`）结构校验：每块恰好一个 `Term`、操作数引用有效值、值单次定义。
- 定宽数组 `[N]T`（`Kind=Array`,带 `Sizes`）与 slice `[]T`（`Kind=Slice`）在 MIR **区分类型** ⇒ 消除 `[0 x i16]` 误判。
- `OpCast` 显式 zext/sext ⇒ 消除 `zext i1→i64`。

---

## 9. HIR → MIR Lowering（核心子集，按可达性，`src/mir/hir2mir.go`）

映射表见 v1 §9（已实现：`KFuncDef`→`Function`、`KLet`→`Alloc`+`Store`、`KIf`→`CondBr`+`Phi`、`KFor`→前置/条件/体/更新/出口块、`KCall`→`OpCall`（sret）、`KInfix/KPrefix`→`Op*`、`KIdent`→读 `ValueID`、`KStructLit/KArrayLit/KSliceLit`→构造指令、`KIndex/KDot`→`OpIndex/OpGetField`）。

unsupported kind ⇒ 记录 diagnostic 并安全终止该函数 lower（验证模式），不影响其他函数或现有构建。

---

## 10. MIR → LLVM 后端策略（真实进度）

- **v1 描述**：验证模式（Stage 0）→ `MIR→HIR` 桥（Stage 1）→ `MIR→LLVM` 直发（Stage 2）→ 全量切换（Stage 3）。
- **真实实现**：**跳过了 Stage 1（MIR→HIR 桥）**，直接实现 Stage 2/3 的 `MIR→LLVM` 直发（`EmitLLVM`，`codegen.go:182`）。理由：直发路径更短、oracle 对照更直接；`MIR→HIR` 桥作为历史方案保留在 v1 中但未实现。
- `EmitLLVM` 已是 `MIR → LLVM` 的直接机械翻译器：每个 `Op*` → 对应 LLVM；`OpDrop` → `emitHeapFree` 块；调用约定按 §5 映射。此时 `emitHeapFree` 的散布逻辑在 MIR 路径中已被删除，内存安全由 MIR 保证（legacy 路径仍保留散布逻辑以供回退）。

### 10.1 覆盖实测（当前口径）

> 口径：全量递归 `tests/**/*.no`（421 个）；`NOLANG_MIR=3`；逐测试 `no run` 超时 8s。门禁以 `MIR=3` 为准（`MIR=2` 回落口径会虚高“构建通过”数，不代表缺口已消）。

| 类别 | 数量 | 说明 |
|---|---|---|
| **MATCH** | **367 / 421**（≈87.2%） | `MIR=3` rc=0 且输出与 legacy 逐字节一致。 |
| **MIR 专属 gap** | **0**（抖动校正后稳定为 1：`test-diff-debug.no`） | 真值 gap 仍是 `test-diff-debug.no`（DP 表全 0，`[]str` 元素 `compare` 在两模式都错，属更底层的预存缺陷）。 |
| 预存失败（两模式都挂） | **53**（CERR 39 + CRASH 14） | legacy 也失败，**非 MIR 引入**。 |
| HANG | 1（`test-for2.no`） | 有意死循环（`while(true){}`），设计使然，不用修。 |

**结论**：MIR 专属 codegen gap = 1（`test-diff-debug`，预存非 MIR 引入）；MIR 专属运行时崩溃 = 0（red-line 持续达标）。剩余失败全部是预存/通用 bug。下一步见 §15。



---

## 11. 不变式与验证 Oracle

- **Oracle**：`NOLANG_MIR=3` 产生的可执行行为必须与 legacy（`MIR=0/2`）逐输出一致；MIR 只"把内存决策说清楚"，不改语义。
- **结构不变式**：每块恰好一个 `Term`；每 `ValueID` 单次定义；`OpDrop` 恰好一次/owned 值；`OpMove` 源之后无 read。
- **Sanitizer 回路**：对 MIR 产出二进制跑 `ASan`/`LSan` 作为回归门（长期）。

---

## 12. 已闭环目录（摘要）

MIR 专属运行时崩溃与 gap 的修复目录共 **#1–#86**，截至 2026-09-16 **全部闭环**。要点：

- **红线已达标**：MIR 专属运行时崩溃 = 0（自 #41 起）、MIR 专属 gap = 0（持续达标）。
- **默认后端已切 MIR-only**（#73），legacy 后端已删除（#79）。
- **#83 编译期 sroa 爆炸**根因由 `afa8d92` 结清（`shouldUseMemcpy` 对 >4KB 聚合改用 `llvm.memcpy` 搬运）。
- **#82** 预检去重、`#86` golden 骨架加固（超时/并发/`-update` 守卫/`UNSTABLE` 分流）已落地。

逐条闭环的过程档案（现象/根因/修法/验证）不在本文保留；完整分类卡见 git history。

---

## 13. 当前已知 gap 家族（待推进目标）

### 13.1 MIR 专属回归（已闭环）
- **`test-arr.no` 的 `option/i64` opt-verify 失配**：已于 2026-09-11 闭环，`emitIndexStore` 加 `%option`→标量解包。
- **`emitSetField` 同类解包**：`emitSetField`（`codegen.go`）的常规 store 路径与 `?T.field` 分支均加同法 option→标量解包（当 RHS 为 `%option` 且目标字段为标量时 `extractvalue %option %valV, 1` 取 ok payload 再 store）。标量赋值 `x = v` 走 `OpMove`/`emitMove`，无独立 `emitStore`，故 sink 站点已覆盖 `emitIndexStore`+`emitSetField`。

### 13.2 预存 crash/hang（非 MIR 专属，legacy 同崩/同挂）
实测根因（两模式各跑，超时 8s）：
- `vec.no` —— `SIGSEGV`（两模式 rc=1）。
- `test-std-net-ext.no` —— `SIGSEGV`（两模式）。
- `test-tls-part2.no` —— `SIGSEGV`（两模式）。
- `test-for2.no` —— `HANG`（两模式 rc=124，死循环/阻塞）。**（该文件全文两行 `{` `} (true)` —— 块调用语法写成的 `while (true) { }`，无限循环是**设计**而非缺陷；`no build` 1 秒完成、rc=0。与它同批记为 HANG 的三个 json/parse 文件**都不是**死循环，而是编译期爆炸。）**
- `tmp-getline.no` —— MIR=0 `HANG`（等 stdin）；**MIR=3 rc=0**（无输入正常退出 "no input"）——MIR 反而更好。
- `tmp-loop-test.no` —— 两模式 **rc=0**（现已通过，非真实挂死）。

**关键结论**：MIR=3 在失败前沿与 legacy **完全持平——零新增崩溃、零新增挂起**。3 个 SIGSEGV + 1 个 HANG 均为 nolang 通用预存 bug（legacy 代码生成缺陷），应作为通用 bug 在双路径闭环，不 blocking MIR 门禁。它们是独立的 legacy 修复轨道，不在 MIR 覆盖推进范围内。

### 13.3 当前状态与待修

**最新权威全量扫描（2026-09-17，421 文件）**：`SAME=409 / DIVERGE=0 / REGRESS=0 / BOTH_FAIL=13`；后续又拉出 `nested-container-clone`、`test-basic`，当前 `BOTH_FAIL` 约 **11**。默认口径 `no build` 通过率 **92.2%**（388/421）。

**红线已达标**：MIR 专属运行时崩溃 = 0、MIR 专属 gap = 0（持续达标）。

#### 待修问题（未闭环）

| 编号 | 问题 | 状态 / 下一步 |
|---|---|---|
| **#84** | `json.parse('')` 最简早返回路径运行期 SIGSEGV；`test-json-parse-option.no`、`test-json-nested-match.no` 编译成功但 rc=1，stdout 仅 `start`。触发因素在 `src/std/json.no` 自身（同形状 option/match/`err()` 早返回复刻均通过）。 | 未修；先定位 `json.parse('')` 早返回路径为何 segfault。 |
| **#85** | 静默错误编译根因未修：`hir2mir.go` 的 void 分支（`resTyp` 三档回退全落空）使 `i.to-str()` 的 `call` 无结果值；`codegen.go` 的 `i64-to-str` 快路径在 `Results` 为空时静默 `return nil`。`loadVal` 护栏已落地（响铃），但底层 lowering 缺陷未修。 | 未修底层；需给 MIR 转储补 `Sym` 字段才能定位 `callee` 确切拼写。 |

#### BOTH_FAIL 余集（约 11，按性质分类）

| 类别 | 文件 | 性质 |
|---|---|---|
| FFI / 外部库 | `test-database-sql`、`test-ffi-mysql`、`test-ffi-sqlite`、`test-sse` | 需实现 FFI，非 MIR 缺口 |
| 测试源与 std 漂移 | `test-json` | 修测试（`p.stringify` 3 参 vs 现行 `(node-idx i64)(out str)`），别改编译器 |
| #85 家族 | `test-std-hash` | `des_block` 的 `NoVal`；触发点是 `src/std/crypto/des.no` 把 `#{index-out}` 写在续行中缀表达式中间 |
| 深层运行时崩溃 | `test-json-parse-option`、`test-https-server`、`test-x25519-fe-diag` | SIGSEGV / abort trap，部分与 #84 同源 |
| 有意死循环 | `test-for2` | rc=124，设计使然，不用修 |
| 负测试 | `i.no` | `print(a.len())` 本就该编译失败 |

> ⚠️ **BOTH_FAIL 桶不比哈希**——“编译失败”与“编译成功但程序自己失败”被压成同一桶。判读必须直接 `diff` 指纹行，否则“编译失败”完全不可见。

#### 已修 / 近期闭环

- **#83**（`afa8d92`）：sroa 大聚合爆炸，根因已结清。
- **5 处同形状空守卫体 bug**（`src/std/{regexp,x509,multipart×2,sse}.no`，2026-09-21）：内联 guard arm 体为空、本应在其内的语句落在 arm 外，已修复并 `make no` 重建，std `no vet` 零 ERROR。

---


## 14. 溢出默认（overflow-default）与 MIR 的集成（进行中）

`#{overflow}` 默认使整数 `+ - * /` 返回 `option<int>`（不 panic）。

- **checker 侧**：`ValidateUnhandledOverflow` 对未注解/未 `?=` 的算术报错（硬错阻断构建）；std 豁免已撤除，131 处已用 `#{overflow=wrap}` 修。
- **MIR 侧**：HIR 算术节点已被打成 `?i64`，`OpOptionWrap` 构造 `{tag,payload}`；sink 站点（`emitIndexStore`/`emitSetField`）解包、算术/负号结果包回、`emitBitwise`、`print` payload 路由、`err/ok/some` 构造器 CALL 均已闭环。

**剩余**（非算术的表达式路径，需在对应 emit 处加 `unwrapOptionOperand` + 包回）：字符串算术运算符重载推断 / async void 操作数 / struct-field-on-option GEP / void 数组 alloca。

### NOLANG_MIR_DUMP_* 调试开关

- `NOLANG_MIR_DUMP_MIR=1`：打印 lowered MIR 到 stderr。
- `NOLANG_MIR_DUMP_LL=1`：写出 emitted LLVM IR 到临时 `.ll`（路径在 stderr）。
- `NOLANG_MIR_DUMP_HIR=1` / `NOLANG_MIR_DEBUG=1` / `NOLANG_MIR_DUMP_BAD=1`：辅助诊断。
- `NOLANG_MIR_DEBUG_UNDEF=1`：打印 `loadVal` 里“类型非 void 却无槽位”的每一个点；判断命中时**看 `value=` 字段**——`value=0`（`NoVal`）才是真洞，其余（真实值 id）是合法的 void 分支。

---

## 15. 路线图（真实进度）

- **已完成**：`src/mir` 核心（模型/CFG/分析/Liveness/所有权/Drop/Move·Borrow/validator/HIR→MIR lowering）；`MIR→LLVM` 直接发射；legacy 后端删除、默认 MIR-only；跨 corpus 门禁（red-line 达标：MIR 专属崩溃=0、gap=0，默认通过率 92.2%）。
- **进行中**：溢出默认与 MIR 的集成（§14）；逐站消除 BOTH_FAIL 长尾（§13.3）。
- **待做（Stage 4 剩余）**：
  1. 收敛最后一个 MIR_GAP `test-diff-debug`（DP 表全 0 / bus error；根因落在 `[]str` 元素的 `compare` 调用降级路径，两模式都 segfault）。
  2. `with-len`/`index dst slot` void 型别族（#54 同族，5）。
  3. `str-len receiver i64`（3）。
  4. 语料迁移 8 个真·未标注溢出运算。
  5. `net-dial` 非 IP 字面量 host 的 `getaddrinfo` 回落（3）。
  6. 逐站消除长尾；FFI/async/crypto/net/map/字符串方法内置补齐。
  7. MIR 单测补齐：`datalayout`/`triple`（无可测对象）、溢出注解读取（MIR 完全无此概念）。
  8. `DIVERGE=1`（地址差异假阳性）正式标记。

---

## 16. 开放问题与风险

- **平台过滤未完全**：HIR `#{platform}` 注解已在 `src/mir/platform.go` 实现并应用于 `KFuncDef`/`KExtern`/`KLet` 注册点与脚本模式顶层内联循环；`src/mir/platform_test.go` 钉住判据。**未做**：① `KStructDef`/`KConst`/`KTypeAlias` 等其余顶层节点类型尚未过滤；② **函数体/块体内**语句两后端都不过滤（共有既有限制，已由 `TestFunctionBodyPlatformVariantIsNotFiltered` 钉住）。

- **溢出模式策略未生效（两后端共有）**：`#{overflow = clamp0 | min | max | saturate}` 实际编译中与 `wrap` 行为完全一致，注解目前只起“关掉 `option<int>` 包装”的作用。MIR 侧完全没有溢出模式概念（`src/mir/*.go` 无 `nsw`/`OverflowMode`）。要让其真正生效，需先改语言实现，再在 MIR 侧引入 `curOverflowMode` 上下文与 `Inst` 级模式分派。

- **静默错误编译（最高优先风险）**：rc=0 但输出错误，比崩溃更危险——rc 类判据（`MIR_GAP`/`REGRESS`/`CRASH`）对它完全失效，只有逐字节哈希比对能看见。#85 的 `loadVal` 护栏已把“静默”属性消除（现以 rc=1 + 精确报文响铃），但底层 lowering 缺陷未修。处理原则：① `DIVERGE`/哈希不一致先当 bug，命名“假阳性”前必须看实际输出；② 已知会不确定的条目放进 `MIR_GOLDEN_UNSTABLE` 并写明原因与 bug 号；③ **`IMPROVED` 桶要人工过一遍**——MIR 从 rc=1 变 rc=0 可能正是“接受了 legacy 正确拒绝的非法输入”，不能当成绩；④ 同类风险下次可能以别的形式出现，判据是“是否存在没有产生者的值”，`NoVal` 检查点值得作为一类断言保留。

- **跨模块 owner 判定**：与 `globalVarOwner`/`funcOwner` 对齐（`transpiler.go`）。

- **bool 打印**：legacy 自身不一致（命名变量 `true/false` vs 比较表达式 `1/0`），MIR 全站统一 `1/0`；维持现状不强行对齐（会引入新分歧）。

- **工程纪律（可复用判读口径）**：
  1. 未提交的改动必须重扫才能计入结论；文档结论只覆盖“上次扫描时的二进制”。
  2. 判定“是否本次引入”用 `git worktree` + 干净 HEAD 基线 + 逐 hunk 二分，而非只看当前失败集。
  3. 注释里“因为某机制所以安全”的推理必须回代码核对。
  4. 一个持续的哈希不一致先当 bug，不要先当“假阳性”；命名“假阳性”前必须看实际输出。
  5. 口径矛盾（同文件 rc=0 却在基线记 124）是测量条件线索，不是数据噪声。
  6. 跨改动的逐条等值 > 事后跑一遍（用改动前冻结基线做预言机）。
  7. “输出稳定”不是“没有 undef”的证据。
  8. 给出“影响半径”必须写明扫描范围。
  9. 手工探针 `no build` 与预言机 `no run` 的 rc 不是一回事（编译器成败 vs 程序退出码）。
  10. 扫描期间不要重编译 `bin/no` / 编辑扫描脚本（会混用两个版本、污染计时）。
  11. BOTH_FAIL 桶不比哈希，需直接 `diff` 指纹行。
  12. 计数有歧义时不可用作判据，必须找语义标记（如 `FlagVariadic`）。
  13. 小改判据也必须全量重扫。
  14. “行为随代码布局漂移”是读未初始化内存的签名。
