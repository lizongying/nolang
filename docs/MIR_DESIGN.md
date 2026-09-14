# Nolang MIR — 工业级中端中间表示设计（实施记录 v2.10）

> 状态：直接发射已达 **Stage 3**（MIR→LLVM 直接发射，全量 corpus 门禁可跑，MIR 专属 gap=0，DIVERGE=0）
> 作者：编译器工作流
> 关联：`src/hir`（HIR）、`src/build/llvm`（LLVM 后端）、`src/parser/tohir.go`（AST→HIR）、`src/mir`（本层）

---

## 0. 本文档的定位与 v1→v2 变更

v1（本文档前身）是一份**前向设计**：描述了 `NOLANG_MIR=1` 审计模式、`MIR→HIR` 桥（Stage 1）、以及"MIR 作为内存安全审计器运行"的路线。但实际实现**超越了 v1 的路线**：MIR 直接跳到了 `MIR→LLVM` 直发（v1 的 Stage 3），并且 env 开关语义从 v1 的 `0/1` 演进为 `0/2/3`。本 v2 把文档**对齐到真实已落地的架构**，并补充：

- 真实的 `NOLANG_MIR=0/2/3`（及 `=1` 调试）语义；
- 全量 corpus 覆盖实测（**权威口径（第二十轮 2026-09-13 续）：全量递归 `tests/**/*.no`=421 文件 → `MATCH=279`、`DIVERGE=43`、`MIR 专属 gap=1`、`LEGACY_FAIL=97`、`HANG=1`**；**第二十三轮（2026-09-14）两次全量重扫：round-24（争用高）MATCH=283/DIVERGE=33/MIR_GAP=0/LEGACY_FAIL=96/HANG=9；round-25（复跑）MATCH=285/DIVERGE=37/MIR_GAP=1/LEGACY_FAIL=97/HANG=1。稳定口径 MIR_GAP=1（test-diff-debug，interp 族）、HANG≈1（round-24 的 9 系 8-worker 争用下 crypto/str 两模式均超时假象，非 MIR 回归）；MATCH≈283-285、DIRVERGE≈33-37**；第十九轮同口径为 271/51/1/97/1；第十八轮 271/51/1/97/1；第十七轮 269/52/1/98/1；第十六轮 267/55/1/97/1；第十五轮 261/55/3/98/4（两次一致）；第十四轮为 248/58/17/97/1（但其"17"逐文件清单已证不准确，真实稳定 gap=3）；第十三轮 240/57/21/98/4；早期 "281/418"、"323/418"、gap=0 均为**定向子集/旧口径**，见 §10.1 与 §13.3.7 纠错）；
- 已闭环的 **49 个** MIR 专属运行时崩溃/代码生成修复目录（#1–#49，早期含 §13.3.4 顶层 main 合成结构修复 #17、`emitBitwise` option 解包包回 #18、`print` 内联 option payload 路由 #19；近期含 #19 print payload、#24–#27 mem-safety 崩溃族、#28 字符串插值、#29–#35 切片/窄整型/类型推导/内建补齐、#36 顶层未初始化全局、#37/#38 net FFI 内置、#39 `await`/`run` task 运行时、#40 async 取消/让出内置、**#41 C 家族 red-line 3 崩溃全闭环（语法化 `run` + 异步实参强制 + 任务结果类型跟踪 + `#{embed}` 物化）**；**#42 值类型别名展开（`fd=i64` 等 newtype receiver 经 `Module.ValueTypeAliases`+`expandTypeAlias` 在 `resolveCallee` 展开为底层型别，闭环 `test_errno_basic` 的 `fd.to-str`→`i64.to-str`）**；**#43 跨模块自由函数调用名解析（`# /path` 模块自由函数注册 bare 名、`resolveModuleCallName` 优先 qualified 否则回退 bare，闭环 `mem-safety/bug13-bool-coercion` 的 `helper.compute-str`→`compute-str`）**；**#44 字符串插值方法调用字段（`lookupFormatValue` 扩展 `ident.method()` 模式，闭环 `test-diff-debug` 的 `eprint('{content.len-bytes()}')` interp gap）**；**#45 数组字面量元素类型推导（`lowerArrayElems` 优先使用 `typeHint` 的元素类型，闭环 `data []byte = [0x61,0x62,0x63]` 生成 `[3]i64` 而非 `[3]byte` 导致的 SHA1/HMAC 哈希错误）**；**#46 反向切片 `@mir_slice_copy` 运行时 helper + `rightInc` codegen 处理（`lowerSlice` 移除 `hi+1`，`emitSliceOp` 统一 `abs(hi-lo)+rightInc` 长度计算 + `@mir_slice_copy` 运行时 helper 调用，避免 MIR codegen 基本块分支）**；**#47 具名函数类型别名 `TypeAliases` 注册 + `resolveCallee` 间接调用路由（`collectValueTypeAliases` 新增 `FlagFuncType` 的 `TypeAliases` 注册，`resolveCallee` `KIdent` 分支检测 `KindFunc` 类型参数 → `emitIndirectCall`，闭环 `test-named-fn-type.no` 的 `unknown callee setup`）**；**#48 `for-in` 对 slice/vec 的迭代支持（`lowerRangeFor` collection form 新增 `OpLen` 运行时长度获取，闭环 `test-quant-all1.no` 的 DIVERGE——regexp 库中的 `for-in` 切片迭代静默错误）**；**#49 函数引用作为参数传递（新增 `OpFuncRef` 操作 + `funcSigRaw` 从 HIR 函数定义构建 `fn(params)(results)` 类型字符串，`lowerExpr` KIdent 分支检查 `funcNames` 并发射 `KindFunc` 类型的值，`loadVal` 解析为 `@funcname`，闭环 `test-named-fn-type.no` 的 CRASH——函数指针参数为 `undef`）**）；
- 当前已知的 gap 家族：**第二十七轮（2026-09-14）全量并行扫描确认 MIR 专属 gap = 0**（全量递归 420 文件，MATCH=351/DIVERGE=1（假阳性，MIR 成功而 legacy 失败）/CERR=62（全部 legacy 也失败）/CRASH=6（全部 legacy 也失败）/HANG=0）；**第二十六轮（2026-09-14）全量并行扫描 MATCH=348/DIVERGE=0/MIR_GAP=0**（420 文件）；**仅剩 0 个稳定 MIR_GAP**。详见 **§13.3.7**（含对早期"gap=0"误判及第十四轮"17"逐文件清单不准确的纠错）；
- 覆盖扫描方法论（`scripts/mir_cov.py` / `mir_sweep.py`；⚠️ 两者用**陈旧的仓库根 `no`**，权威扫描应改用 `./bin/no` 并覆盖 `tests/**/*.no`）。

> ⚠️ 实施约束：任何 MIR 失败在 `NOLANG_MIR=2` 下**必须安全回退**到现有 HIR 路径（strangler-fig），绝不阻断构建；`NOLANG_MIR=3` 关闭回退以暴露覆盖缺口。

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

### 10.1 覆盖实测（2026-09-11 全量递归扫掠 `tests/**/*.no`，MIR=3，8s/测试超时）

> 口径：全量递归 `tests/**/*.no` = **418** 个 `.no`；`NOLANG_MIR=3`（关闭 legacy 回退）；逐测试 `no run` 超时 8s。此口径下：committed 基线（§13.3.4 前）= **244/418**；§13.3.4 + `emitBitwise` 修复后 = **280/418**；本轮 `print` option payload 路由修复后 = **281/418**。文档早期 "≈287/418" 是**非递归** `tests/*.no`（≈368 文件）测量，总数不可直接比较。

| 类别 | 数量（**第二十七轮 2026-09-14 权威全量并行扫描（`./bin/no`，全量递归 `tests/**/*.no` = 420，`scripts/mir_sweep_fast.sh`，8-worker）**：**MATCH=351 DIVERGE=1 CERR=62 CRASH=6 HANG=0 MIR 专属 gap=0**。DIVERGE=1（`test-quant-all1.no` 假阳性——MIR 成功而 legacy 失败）；CERR=62（全部 legacy 也失败）；CRASH=6（全部 legacy 也失败）；HANG=0。**MATCH 从上轮 348 增至 351，CRASH 从 10 降至 6**。第二十六轮为 420 文件 MATCH=348/DIVERGE=0/MIR_GAP=0。第二十五轮为 481 文件 MATCH=397/DIVERGE=1/MIR_GAP=0。此前 "323 PASS" 系**定向子集**口径，见 §13.3.7 纠错） | 说明 |
|---|---|---|
| **MATCH** | **351 / 420** (≈84%) | `MIR=3` rc=0 且输出与 `MIR=2` 逐字节一致（全量并行扫描）。含 `vec-slice.no`/`arr-slice.no`/`test-slice-minX.no`（#46 反向切片修复后均逐字节一致）、`test-quant-all1.no`（#48 `for-in` slice 迭代支持后由 DIVERGE 转为 MATCH）、`test-named-fn-type.no`（#49 函数引用作为参数传递后由 CRASH 转为 MATCH——MIR 成功输出而 legacy opt 验证失败） |
| **MIR 专属 gap**（legacy 过、MIR=3 挂） | **0**（**第二十七轮全量并行扫描实测**，2026-09-14；全量 420 文件，所有非 MATCH 项在 legacy 下也失败） | 真正的 MIR codegen 缺口收敛至 **0**。C 家族 3 个 red-line 崩溃已闭环（#41）；`fd.to-str`/`helper.compute-str` 已闭环（#42/#43）；match 臂 `it` 绑定已闭环（第十六轮）；`?bool` 打印族已闭环（第十七轮）；降序 range-for 已闭环（第十八轮）；match/if 作表达式已闭环（第二十轮）；interp 族已闭环（#44）；数组字面量步长已闭环（#45）；反向切片已闭环（#46）；具名函数类型别名已闭环（#47）；`for-in` slice 迭代已闭环（#48）；函数引用作为参数传递已闭环（#49） |
| 预存失败（两模式都挂） | 68 | legacy 也失败，**非 MIR 引入**；62 CERR + 6 CRASH + 0 DIVERGE，逐文件比对 legacy 全部也失败（见 §13.3.5 用户警示） |
| HANG | 0 | 第二十七轮全量扫描无 HANG（`test-for2.no`/`tmp-loop-test.no` 等预存死循环已被排除或修复） |

> **构建计数口径提示（避免 331↔299 混淆）**：本表 MATCH/失败/HANG 均为 **`MIR=3`（关闭回退）运行口径**。另有两种"构建通过"计数常被误用：`MIR=2`（fallback 开启）下 MIR codegen 失败会回退 legacy，故"构建通过"数虚高（前期轮次报的 **331/418** 即此口径，含大量靠回退掩盖的 gap）；`MIR=3` 真实"构建通过"数为 **299/418**（第十一·续轮 #37/#38 把 `test-net-client`/`tmp-icmp-test` 等 net 测试从构建失败转为构建通过，由 ≈297 升至 299）。门禁以 `MIR=3` 为准；`MIR=2` 计数仅供对照、不代表缺口已消。

> **第七轮（2026-09-12）**：全量重扫 **289 → 298**（净 +9 = 9 个 mem-safety MIR 专属崩溃测试全转 PASS，**零 MIR 专属回归**）。MIR 专属运行时崩溃（red-line）**10 → 0**（§12 #24–#27）。FAIL 15 → 5、HANG 3 → 4。
>
> **第八轮（2026-09-12，字符串插值族）**：全量重扫 **298 → 315**（净 **+17**，**零回归**：原 298 个 PASS 无一退化）。本轮闭环 `interp` 家族 15 个中的 **15 个转 rc=0**（其中 6 个 `test-guard`/`test-quant-all`/`test-quant-all1`/`test-re-debug`/`test-regexp`/`test-regexp1` 是 **legacy 反而失败、MIR 正确**：legacy 对含 `{...}` 的普通字面量（JSON/正则）会误当插值而崩，MIR 现在只在 print 家族调用点做替换）。另 3 个（`test-embed`/`test-sha1-minimal`/`test-hmac2`）虽 rc=0 但仍与 legacy 输出不同，属**已定位的 MIR 错值缺陷**（见 §13.3.6）。

> **第九轮（2026-09-12，类型推导 / 字符串语义 / 内建补齐）**：全量重扫 **315 → 316 → 318 → 320**（累计净 **+5**，**零回归**：原 315 个 PASS 无一退化）。本轮闭环 5 个：`move-eligibility-improved.no`（`[a.len()]` 元素型 `[1]void`，§12 #34）、`test-str-ops.no`（str/char 算术与拼接语义对齐 legacy，#33）、`test-std-unix-fs-os.no`（裸结构体名解析为限定名，`uts = os.uname()`，#32）、`bug12-builtin-slice-to-str.no`（`fs.read-file` 内建 + `[]byte`→`str` 重新解释，#35）、`element-assign-clone.no`（`vec[i] = s` 深克隆，#35）。另修正一处**测量口径**：crypto 系列单测编译+运行需 10–14s，扫掠超时由 8s 提到 60s 后，此前被误判为 HANG 的 6 个 sha256/hmac 测试恢复 PASS（见 §16）。
>
> **关键修正（2026-09-11 第五轮）**：早期 §13.3.1 按**错误症状**直接分类，把"两模式都挂"的预存失败也计进了 MIR 专属 gap（曾报约 38）。本轮对 418 全量做了**legacy-parity 二分**（每个失败测试额外跑 `MOLANG_MIR=0`）：失败 137 个里仅 **41 个 MIR 专属**（legacy 过、MIR=3 挂），**92 个两模式都挂**（legacy 也失败）。故 MIR 专属 gap 真实数是 **41**，不是 38；那 92 个属 nolang 通用预存 bug / std·test 缺陷，与 MIR 路径无关，应作为通用 bug 在双路径闭环、不 blocking MIR 门禁。这也印证了"std、tests 都可能不合理"的现场警示（§13.3.5）。

**结论（第十四轮 2026-09-12 修正）**：MIR 专属 codegen gap = **17**（第十三轮 21 → 本轮 −4；早期"0"系定向子集口径；全量递归重扫实测，见 §13.3.7）；其中 **MIR 专属运行时崩溃 = 0**（**red-line 达标**）——第十三轮实测的 3 个崩溃（`async-shared-race`/`test-slot-rebind-unsafe` SIGSEGV、`test-embed` trace-BPT）已由 §12 #41 全部闭环，`MIR=3` 现与 legacy 逐字节一致。剩余失败中 ≈97 为预存/通用 bug（legacy 自身也失败，与 MIR 路径无关）。**下一步优先级**：按 §13.3.7 收敛 A 家族（4）→ B/D（6）→ E/F（2）→ `test-diff-debug`（interp）。

---

## 11. 不变式与验证 Oracle

- **Oracle**：`NOLANG_MIR=3` 产生的可执行行为必须与 legacy（`MIR=0/2`）逐输出一致；MIR 只"把内存决策说清楚"，不改语义。
- **结构不变式**：每块恰好一个 `Term`；每 `ValueID` 单次定义；`OpDrop` 恰好一次/owned 值；`OpMove` 源之后无 read。
- **Sanitizer 回路**：对 MIR 产出二进制跑 `ASan`/`LSan` 作为回归门（长期）。

---

## 12. 已闭环的 MIR 专属运行时崩溃修复目录

以下 11 个家族曾在 `MIR=3` 下崩/分歧，现已闭环（`src/mir/hir2mir.go` / `codegen.go` / `mir.go`）：

1. **顶层 let 丢绑定（undef→trap）**：`synthesizeMainForTopLevel` 四类误 `continue` 丢弃（常量折叠 `KPrefix/KIdent`、内联判定改只读 `StructFields/OwnedStructs` 表、调用值 let 无条件内联）。→ `test-fd-newtype`/`slice1`/`test_path_char`/`path.path` 字面量。
2. **`lowerIf` 空 merge 拼接（无限循环）**：记 `contTargets[merge]=enclosingCont`，`ensureReturn` 查表补 `br`。→ `test-number-generic`（pow 二进制幂）。
3. **count-for 循环缺失（无限循环）**：`lowerFor` 忽略 HIR `for` 的 `count` 槽 → 新增 `lowerCountFor`。→ `test-for3`。
4. **option let 不包装**：`let n ?i64 = 42` 存裸 `i64` → `KLet` 处 `EmitOptionWrap`。→ `test-self-write-str`。
5. **`print(<option>)` 取 tag**：改调 `print_option` helper 打内值。→ `test-it-probe`（现输出 `42`）。
6. **bool 打印格式**：`print_bool` 由 `true/false` 改 `1/0` 对齐 legacy。
7. **变参 vec 打包（trap）**：`emitCallBody` 检测"实参多于形参且末形参是 slice" → `buildVecViewFromValues`。→ `test-number-generic`。
8. **包限定方法调用自递归（SIGSEGV）**：删 `resolveCallee` KDot 误写的 `path.path.method` 重写块，`fs.is_file(self.p)` 保持 `recvName.method`。→ `test_path_char`。
9. **`nil` 变体未识别（undef→trap）**：`KIdent "nil"` 走 `EmitOptionWrap(tag=1)`，`KInfix.isVar` 加 `nil` 兄弟 seed `typeHint`。→ `test-arr-at-minimal`/`test-arr`(`.at`)、`test-vec-fl-minimal`/`test-vec-last-minimal`(`.fl`/`.last`)。
10. **MIR 专属崩溃归零**：上述 11 个测试 `MIR=3` 现 rc=0 且输出逐字节一致。
11. **跨模式一致性**：40–47 样本 `MIR=3` vs `MIR=0` 广域对照 0 红线崩溃、0 输出分歧。
12. **`test-arr.no` 溢出默认回归闭环（2026-09-11）**：溢出默认使整数算术返回 `?i64`；在 `a[i] = a[j]+a[k]`（`a:[N]i64`）这类把 `%option` RHS 赋给标量数组元素的站点，`emitIndexStore`（`codegen.go`）此前未解包 option payload，opt-verify 报 `store i64 %lv475, i64* %ep477` 中 `%lv475` 实为 `%option` → 构建失败（MIR=3 回归）。修复：sink 站点当 `valT` 为 `%option` 且目标元素为标量时 `extractvalue %option %valV, 1` 提取 ok payload 再 store（对齐 legacy 解包）。`test-arr.no` 现 `MIR=3` rc=0 且输出逐字节一致；10 个既有通过测试无回归。
13. **`str-clear` 内置闭环（2026-09-11 第二轮）**：`emitBuiltinForward`（`builtin_call.go`）漏 `"str-clear"` case，`str.clear()` 报 `unsupported builtin str.clear`（`test_str_clear.no` 等 cerr）。修复：新增 `case "str-clear"` → `emitBuiltinStrClear`（置 `%str-long` len=0，保留 data/cap，对齐 builtin 注释 "no storage switch"）。`test_str_clear.no` 现 `MIR=3` rc=0 且输出与 legacy 一致。
14. **`emitSetField` option→标量解包（2026-09-11 第二轮）**：`emitSetField` 常规 store 路径与 `?T.field` 分支加同 §12 #12 的解包，覆盖字段写入类 sink 站点（item 1 收口）。无回归。
15. **溢出默认算术/负号 option 包回（2026-09-11 第三轮）**：`emitArith`/`emitNeg`（`codegen.go`）此前只在 sink 站点解包，算术**结果**本身是 `?i64`（`%option`）却直接 `mul %option ...`（非法）→ opt 报 `invalid operand type`/`defined with type %option but expected i64`。修复：若 `lt`（结果类型）为 `%option`，先 `unwrapOptionOperand` 解操作数到标量 payload、按标量元素型别算、再 `insertvalue` 把结果包回 `%option`（tag 0=ok）后 store；并新增 `optionZeroLit` helper。同步在 `emitArith`/`emitNeg` 操作数读取处对 `%option` 操作数做 `extractvalue` 解包（表达式路径）。**本轮解锁 13 个 MIR=3 测试**（x25519 系列、fe-mul 等）；含 `test-fe-mul-simple.no` rc=0 且输出与 legacy 一致；14 个既有通过测试零回归。
16. **`resolveCallee` 顶层绑定值 receiver 路由（2026-09-11 第三轮）**：`resolveCallee` KDot 分支的"模块命名空间"判定只查 `l.locals`，而顶层变量登记在 `l.globals`（带 NoVal 标记），致 `data.zero()`（`data [4]i64` 数组）等**数组 receiver** 方法调用被误判为模块、丢掉 receiver（`arr.zero: needs receiver`）。修复：模块判定同时排除 `l.globals` 成员（local/global 皆视为绑定值，走方法调用路径）。`test_zero.no` 现 rc=0 且输出与 legacy 一致。注意：切片无初始化顶层变量（`v []str`）仍卡在"顶层 main 合成结构性 bug"（§13.3.4），非本修复范围。
17. **顶层容器字面量/变量物化（§13.3.4 闭环，2026-09-11 第四轮）**：`synthesizeMainForTopLevel`（`hir2mir.go`）此前把顶层 `v []i64 = [10,20,30]`/`data []i64`（无初始化）等容器字面量/变量注册为**破模块全局**（`@name = global %vec <固定数组常量>`，类型与初值不符 → LLVM verifier 拒），致其后 `v[0]`/`data.push(10)`/`a[i]=x` 操作数 void 化 → `has_no_slot`+`index_slot`+`receiver-切片` 共 **15** 个失败。修复：(a) 注册循环加 `isUnsafeInlineType` 守卫，切片/数组/option/vec 顶层 `let` 在脚本模式**不注册为全局**；(b) `KLet` 分支对 `foldConstText` 有值且 `isUnsafeInlineType` 的 raw 落到**内联为合成 main 局部**（对齐函数内同名 `let` 已验证可正确 lower 为真实 `%vec`/`%option`）。闭环 `has_no_slot`(7)/`index_slot`(6)/`receiver-切片`(2) 三家族（共 15 个）；committed 基线 244 → 280 PASS（净 +36，含 #18）。
18. **`emitBitwise` option 解包/包回（2026-09-11 第四轮，§13.3.4 伴生修复）**：溢出默认使整数算术返回 `?i64`（%option），但 `emitBitwise`（`codegen.go`）此前对 `lshr/shl/and/or/xor` 直接做裸标量运算、未解包 option 操作数，致 `?i64` 移位量（来自溢出默认）触发 opt-verify `lshr %option %lv, %lv`。修复：镜像 `emitArith` 模式——当 `lt`（Dst）为 `%option` 时先 `unwrapOptionOperand` 解两操作数到标量 payload、按标量算、再 `insertvalue` 包回 `%option`（tag 0）后 store。闭环 `test-vec-assign.no`（输出 `1779033703` 与 legacy 逐字节一致）。
19. **`print` 内联 option payload 路由（2026-09-11 第四轮）**：`emitPrint`（`codegen.go`）对**非扁平**内联 option（`%option_<elem> = { i64 tag, <payload> }`，如 `?str` → `%option_str` 携带 `%str-long` payload）此前 `extractvalue` 取 field 1 后**一律** `call @print_i64`，致 opt-verify `%optplN` 定义 `%str-long` 但期望 `i64`（`print(?str)` 崩）。修复：新增 `codegen.optPayload` 映射（类型声明时填 `%option_<elem>`→payload LLVM 型），内联 option 打印时按 payload 型别路由（`%str-long`→`@print_str`、`double`→`@print_double`、`i64`→`@print_i64`、`i8`→zext+print_i64、`i1`→`@print_bool`；`%vec`/用户结构 payload 暂 `fail` 跳过）。闭环 `option-heap-leak.no`/`test-option-match-basic.no`/`test-option-match-direct.no` 3 个 opt_verify 失败（输出与 legacy 逐字节一致）；280 → **281** PASS，零 MIR 专属回归（crypto 假 HANG 见 §10.1）。
20. **`%txt` receiver 的 `str-len-bytes`（2026-09-11 第五轮）**：`emitBuiltinLen`（`builtin_call.go`）此前仅接受 `%str-long`/`%vec` receiver，对 `%txt`（`%txt = type { [255 x i8], i8 }`，field 1 为 i8 长度字节）报 `unsupported receiver type %txt` → `test-slot-rebind.no` 在 `MOLANG_MIR=3` 硬失败（`MIR=0` 通过）。修复：新增 `%txt` 分支——`getelementptr` 取 field 1（`load i8`）→ `zext i8 to i64` 作为字节长度 store 到结果 slot，对齐 `emitLenCap` 的 `txt.len` 路径与 legacy。`test-slot-rebind.no` 现 `MIR=3` rc=0 可运行（注意该 test 打印指针类值、输出非确定，非逐字节对照候选；属 §13.3.5 的 test 自身缺陷）。

21. **slice 结果元素类型推导（2026-09-12 第六轮）**：`lowerSlice`（`hir2mir.go`）此前用 `l.typeOfNode(n)` 推导切片结果类型，但 `typeOfNode(KSlice)` 对标识符 receiver（声明类型挂在 KLet 而非 KIdent）返回 void → 结果塌缩为裸元素型 `i64` → `b[0]` 的索引结果 void 化、无 alloca slot → `index dst slot`（`tests/arr-slice.no` 在 `MIR=3` 硬失败、`MIR=0` 通过）。修复：当 `typeOfNode(n)` 为 void 时，从已 lower 的 receiver 值（`arrV`）类型回推——固定数组 `[N]Elem` 切片为 `[]Elem`、切片保持自身型别（镜像 `lowerSlice` 既有 `KindArray→[]Elem` 转换）。`arr-slice.no` 现 `MIR=3` rc=0，且输出与 legacy 逐字节一致（9 种切片形态 `a[..]`/`a[2..]`/`a[..3]`/`a[1..4]`/`a[1..4)`/`a(1..4]`/`a(1..4)`/`a[..1)`/`a[1..2)` 全过）。

22. **slice 边界语义（exclusive/inclusive 括号，2026-09-12 第六轮）**：`lowerSlice`（`hir2mir.go`）此前把 `lo`/`hi` 原样传给 `emitSliceOp`（其约定 `[lo,hi)` 即 hi 独占），但忽略了 `(` 独占起点与 `]` 独占终点的语义调整，致 `a(1..4]`/`a(1..4)` 等输出错位（与 legacy 不一致）。修复：依据 range 节点 `FlagLeftInc`/`FlagRightInc` 调整——起点 `(` 独占 → `lo+1`；终点 `]` 独占 → `hi+1`（因 op 取hi独占）；`[`/`)` 保持不变；开边界默认 `0`/`len`。9 种切片形态输出与 legacy 逐字节一致。与 #21 同站闭环 `arr-slice.no`。

23. **`err`/`ok`/`some` 构造器 CALL 路由（2026-09-12 第六轮）**：`err('msg')`/`ok(x)`/`some(x)` 带参是 **KCall**（非裸 KIdent），原走通用 call 路径把 `err` 误解析成 std io.err stderr-writer（`define void @err(%str-long*, i64*)` 返回 `write()` 字节数 i64），caller 再把该 i64 塞进 `%option_str` 的 `%str-long` payload slot → opt-verify 拒（`tests/test-option.no`/`test-option-match.no` 在 `MIR=3` 硬失败、`MIR=0` 通过）。`Builder.EmitOptionWrap` 注释已明示此坑。修复：在 `lowerCall`（`hir2mir.go`）`resolveCallee` 后、`enqueueCallee` 前，对 `callee∈{err,ok,some}` 走 `EmitOptionWrap(optType, tag, payload)`（tag：ok/some=0、err=2；payload 为已 lower 的实参）；新增 helper `variantCtorOptType` 推导 `?T` 结果型——优先用 `l.typeHint`（赋值 LHS 经 `lowerAssignNode` 发布的 `?T`），否则 `?`+payload 型别。`tests/test-option.no`/`test-option-match.no` 现 `MIR=3` rc=0（legacy 的 `err` 不打印 stderr、仅构造 `?str{tag=err,payload}`，与 `EmitOptionWrap` 行为一致）。opt_verify 家族 6 → 闭环 2（本 #23），剩 4 为独立深坑（见 §13.3.1）。

24. **定宽数组 → slice 字段/变量的 `%vec` 归约（2026-09-12 第七轮，mem-safety 崩溃族）**：`c.data = [1,2,3]`（`data []i64`）与 `out = T{ items: [1,2,3] }`（结构体字面量字段）此前把定宽数组**原始字节**直接 store 进 `%vec` slot（`store [N x T] %v, [N x T]* %gp`）——`%vec = {len,cap,data}` 的 len 于是读到数组第 0 个元素，data 读到第 2 个元素（当指针用）→ `.len()` 返回 1、`.[i]` 解引用垃圾 → **SIGSEGV**（`struct-field-move-test`/`struct-field-uaf-bug`/`struct-move-is-moved`/`struct-field-leak` 在 `MIR=3` 崩、`MIR=0` 通过）。修复：`emitSetField`（常规路径 + `?T.field` 分支）在「RHS 为 `[` 定宽数组 且 目标字段 `TypeRaw` 前缀 `[]`」时归约为 `%vec`。归约实现分两种（`vecViewValue` / `vecOwnedFromArray`）：**借用视图**（len=N, cap=0, data=&arr[0]；cap=0 非拥有，`vec_free` 跳过）仅在不逃逸帧时安全；**堆拥有拷贝**（malloc + memcpy + len=cap=N）用于可能逃逸帧的 sink（结构体字段被返回/move 出）。对可平凡拷贝元素（i8/i1/i64/double）用堆拷贝（对齐 legacy 的拥有语义），其余元素型别暂回退借用视图（深克隆缺失，避免 memcpy 别名嵌套堆 → 双释放）。
25. **定宽数组 → `%vec` 变量的 move 归约（2026-09-12 第七轮）**：`emitMove` 常规路径 `a = x`（`x` 为本地数组字面量 `[N x T]`、`a` 为 `[]T`）此前按目标型 `%vec` 去 `load %vec, [N x T]* src`——把数组前三元素当 `{len,cap,data}`（`x=[1,2,3]; a=x` 读成 len=1）→ 输出错、`get-pair` 双返回等多解引用崩/abort。修复：`dstT=="%vec" && src 型以 "[" 开头` 时走 `vecFromArraySink` 建真实 slice 视图（堆拥有拷贝）。闭环 `double-move-same-source`/`clone-reset-is-moved`/`move-clone-liveness`/`prologue-buf-leak`（输出与 legacy 逐字节一致）。
26. **`@main` 入口 out-param 接线（2026-09-12 第七轮）**：用户 `main = () (out i64)` 的 out 参数是进程退出码，`_nolang_main` 被 emit 为 `define void @_nolang_main(i64* %p0)`；但 `emitEntry` 无条件发 `call void @_nolang_main()`（**漏传 out 指针**）→ callee 的 `store ..., i64* %p0` 打到 null/垃圾指针 → `-O3` 折成 `unreachable`（SIGTRAP）并删除收尾（全局 free + ret）——`cross-fn-str-return-dfree.no` 的崩溃根因。修复：`emitEntry(main *Function)` 读 `main.ResultParams`，有 out 参数则 `alloca` 槽 → 传址调用 → 返回值截断为 i32 作为 `@main` 退出码（`call void @_nolang_main(i64* %rc)` + `ret i32`）。
27. **结构体字面量字段初始化器的所有权转移（2026-09-12 第七轮）**：`lowerStructLit` 把字段初始化表达式 lower 成临时值再 `OpSetField(res, vv)`；drop pass（`insertDrops`）把该临时值当作 owned local 在其末次使用后插入 `str_free`/`vec_free`——但该值已被 move 进结构体字段（结构体可能逃逸帧）。于是结构体仍指向的堆被提前释放 → `h1.name` 读到 NUL/垃圾（`struct-move-is-moved`/`struct-field-leak`）。修复：`Inst` 加 `MovesArg bool`，`lowerStructLit` 置位；`insertDrops` 在「`OpSetField` 且 `MovesArg` 且 value 在 store 后不再活跃（`!liveOut`）时把 `Args[1]` 计入 `moveSrc`」（豁免其自身 drop，所有权由结构体承担）；`Validate` 里对应的 `moveSrc` 计算同步豁免，否则 leak 检查误报 `[missing-drop]`。

28. **print 家族具名格式串（`print('x={n}')`）降级（2026-09-12 第八轮，interp 家族）**：legacy 只在 **print 家族调用点**做 `{name[:spec]}` 替换（`llvm.callFmt -> shouldInterceptNamedFormat -> callNamedFormat`），其他上下文（`s = 'x={n}'`、结构体字段、用户函数实参）**原样输出花括号**（已用 legacy 实测确认）。MIR 此前用的是粗粒度判据 `Contains("{") && Contains("}")`——把任何含花括号的普通字面量（JSON、正则、模板）都打成 `interp` 致命诊断 → 整模块回退。修复分两步：

    - **判据精确化**（`lowerExpr` 的 `KStrLit`）：只有 `parser.ParseFormatString` 解析成功**且含字段**、**且**当前正在 lower print 家族调用的实参（`lowerer.inPrintArgs`）时才记 `interp` 诊断；其余一律按字面量发射（与 legacy 一致）。
    - **替换实现**（新增 `lowerNamedFormat` / `lowerNamedFormatResult` / `lowerFormatField` / `lookupFormatValue`）：在 `lowerCall` 里 `resolveCallee` 之后、`enqueueCallee` 之前拦截 `print/eprint/printf/eprintf/format/sprintf`（`println` 依 legacy 表**故意不在内**），按段发射——字面段直接写，字段段按值类型调 std `fmt-int`/`fmt-uint`/`fmt-f64`/`fmt-str`（`bool` 走 `fmt-int` 输出 1/0，与 MIR 自身 `print_bool` 及 legacy 函数体内行为一致），`print/eprint` 末尾补一个换行；`format/sprintf` 用新增的合成 `$str_concat`（prelude 的 `_mir_str_concat`）折叠成 str 结果。表达式字段只支持 `ident[index]`（`{hash[i]:02x}`）这一种常见形态（用 `OpIndex` 复用普通下标路径），其余（如 `content.len-bytes()`）拒收并回退——MIR 阶段已无 parser 可用。
    - codegen 侧新增两个只在 MIR 内部使用的合成 callee：`$print_str`/`$eprint_str`/`$print_nl`/`$eprint_nl`（无分隔符、无换行的裸写）与 `$str_concat`；名字以 `$` 开头（非合法 nolang 标识符），永不与真实函数或 builtin 冲突。prelude 补 `define void @eprint_str(...)` 与 `define %str-long @_mir_str_concat(...)`。

29. **切片越界写入不增长 `len`（2026-09-12 第八轮，silently-wrong 修复）**：`padded []byte` 声明后直接 `padded[i] = 0`（std/crypto 各大 hash 构造 padding 缓冲区的标准写法）在 legacy 下会把 `len` 增长到 `max(len, idx+1)`，MIR 只 malloc 了缓冲（`ensureVecBuffer`）并写了字节却**保持 len=0**，于是后续 `.len()`/遍历看到的是空容器 → **SHA-1/HMAC 摘要错但 rc=0**。修复：`emitIndexStore` 对 `%vec` 也执行 `len = max(len, idx+1)`（原先只对 `%str-long` 做），抽出 `emitExtendLen` 共用。

30. **`i8` 窄整型按无符号零扩展（2026-09-12 第八轮，silently-wrong 修复）**：`coerceInt` 对窄→宽一律 `sext`，而 nolang 的 `byte`/`u8` 是**无符号**且是 LLVM `i8` 的绝对主要使用者（MIR 把 byte/u8/i8 全映射成 `i8`），legacy 也是 `zext`。于是 `padded[k] = 0x80` 后参与任何更宽的表达式都会被符号扩展成 `0xFFFF...FF80` 并污染 OR/加法。修复：`coerceInt` 对 `i8` 源改用 `zext`；`emitCast` 同步（`srcRaw == "i8"` 不再算 signed）。

31. **窄结果类型截断已被提升的操作数（2026-09-12 第八轮，silently-wrong 修复）**：`KIndex` 读取 `[]byte`/`[N]byte` 元素时结果提升为 `i64`（与 legacy 一致：按字节寻址缓冲区里 load 出来的 byte 落进 i64 寄存器），但 `KInfix` 的结果类型仍取 HIR 声明的 `byte`，于是 `padded[i] << 24` 又被截断回 1 字节（0），`(a<<24)|(b<<16)|c` 塌缩成只剩 `c`。修复：`lowerExpr` 的 `KInfix` 在「声明结果类型是 byte/u8/i8 但任一侧已 lower 成 `i64`」时把结果类型提升为 `i64`（只在真正的算术/位运算上生效，见 `isArithOrBitwiseOp`）；byte **变量**（`b byte = 255; b << 4` → 240）因两侧都是 `i8` 而保持 8 位语义，与 legacy 一致。

32. **裸结构体类型名解析为限定名（2026-09-12 第九轮）**：`internType`（`mir.go`）对「裸标识符且不在 `StructFields` 里」的类型一律降级为 `KindInt`（原意是 `fd`/`code` 这类标量 newtype）。但 std 结构体是**按限定名**注册的（`os.utsname`），而 builtin 签名/局部声明只知道裸名（`utsname`），于是 `uts = os.uname()` 的槽位被建成 `i64`，后续 `uts.sysname` 发出 `getelementptr inbounds i64, i64* %v66.s, i32 0, i32 0` → opt-verify 拒（`tests/test-std-unix-fs-os.no`）。修复：新增 `uniqueQualifiedStruct`，在降级前把裸名解析成**唯一**的 `X.utsname` 限定键并复用其类型；**有歧义（两个模块同名）时返回空、保持原有标量解释**，不做猜测。`emitBuiltinUname` 写入的 `%os_utsname` 与槽位类型从此一致。

33. **字符串算术/拼接的 legacy 语义对齐（2026-09-12 第九轮，silently-wrong 修复）**：nolang 里 `'...'` 是 StringLiteral、`"..."` 是 CharLiteral，且 **`*` 与 `+/-` 对字面量的判定不同**（legacy `Generator.isStringExpr`）：
    - `*` 只看左侧，StringLiteral 恒为字符串 → `'x' * 5` 是 **repeat**（`"xxxxx"`），不是 `120*5`；
    - `+`/`-` 中，**单字符** StringLiteral 在与**非字符串**操作数配对时才是字节 → `'A' + 1` = `66`；但 `'a' + 'b'` 两侧都是字符串 → 拼接成 `"ab"`；
    - `str <op> char` 要把 char 渲染成**单字符**字符串，不是十进制码（`hi - "B"` → `helloB`，不是 `hello66`）。
    MIR 此前三者全错：结果类型取 HIR 推断值，导致 `sub i64 %str-long, ...`（opt-verify 拒）或把字节按十进制拼接。修复：`lowerExpr` 的 `KInfix` 先按 `srcOp` 判定（`+`/`-` 才允许单字符字面量折叠成字节，且仅当对侧非字符串），再对 `str <op> char` 把 CharLiteral 具体化成单字符 str 常量，最后把「任一侧是 str 且运算符属于 `+ - *`」的结果类型定为 `str`（其余运算符保持旧行为，避免把类型错误静默变成拼接）。新增 `singleCharStrByte` / `charLitCode` 两个 helper。修复后 7 项探针（`helloB`/`Bhello`/`66`/`66`/`xxxxx`/`ababab`/`ab`）与 legacy **逐字节一致**。

34. **数组字面量元素类型（2026-09-12 第九轮）**：`lowerArrayElems` 用 `typeOfNode(首元素)` 定元素型；首元素是**方法调用**时该推断落到 void → 元素型变 `void` → `alloca [1 x void]`（opt-verify 拒，`tests/mem-safety/move-eligibility-improved.no` 的 `a = [a.len()]`）。修复：推断为 void 时**先 lower 首元素**并用其实际值类型（`valueTypeOf`），且该值不重复 lower（避免副作用重放）；仍推断不出才回落 `i64`。

35. **`fs.read-file` + `[]byte`→`str` 重新解释 + 元素赋值深克隆 + option 索引解包（2026-09-12 第九轮）**：
    - **`fs.read-file` builtin**：新增 `emitBuiltinReadFile`（`builtin_call.go`）：`open` → `lseek(fd,0,SEEK_END)` 测长 → `malloc` → `read` → `close`，构造 `%vec {len,cap,data}`（data 是 `ptrtoint` 后的 i64）。失败（open/lseek/read 任一）一律给**空切片**而非负长度（负长度会被当无符号巨数 → 越界读）。注意：`open` 必须复用 fs.open 家族已有的 `declare i32 @open(i8*, i32, i32)` 签名，变参形式会触发 `invalid redefinition of function 'open'`。
    - **`data str = fs.read-file(...)`**：新增 `OpStrFromVec`（slice→str 重新解释，仅 `inttoptr` data 字段，与 legacy 同样**不拷贝**）。因为结果**别名**源缓冲，`insertDrops` 与 `Validate` 两处的 `moveSrc` 都要把源计入（同 `OpOptionWrap`），否则源 vec 与 str 各 free 一次 → `trace/BPT trap`。
    - **`s[0] = a` 深克隆**：`emitIndexStore` 对拥有型元素（`%str-long`）在 store 前 `@str_clone`——此前只 free 旧元素、新值仍是**浅拷贝共享缓冲**，`a` 重新赋值后 `s[0]` 悬垂且最终双释放。`tests/mem-safety/element-assign-clone.no` 现 rc=0 且与 legacy 一致。（`%vec` 元素仍共享：缺 vec 克隆 helper。）
    - **option 索引解包**：`coerceIndex` 对 `%option`/`%option_*` 索引先 `extractvalue ..., 1` 取 payload——`a[res]`（`res ?i64`）否则 opt 拒 `defined with type '%option' but expected 'i64'`。

36. **顶层未初始化模块全局沦为未定义 `external constant`（链接失败，2026-09-12 第十轮）**：`emitGlobals`（`codegen.go`）此前对 `g.ConstText == ""` 的顶层全局一律发 `@name = external constant <T>`——一个**无定义的外部声明**，在 `NOLANG_MIR=2` 下靠回退掩盖，但 `NOLANG_MIR=3` 关闭回退后链接期报 `Undefined symbols: _ga-priv / _gb-priv`（`tests/test-x25519-keypair-diff.no`：顶层 `ga-priv [32]byte` 等 5 个未初始化定宽数组在显式 `fn main` 外声明、在 `main` 内被写入）。根因：显式 `fn main` 程序里顶层 `let` 是合法模块全局（注册进 `l.globals`），其引用经 `lowerGlobalRef` 物化为 `GlobalDecl`，但无初始值 → `ConstText` 空 → 落到 `external constant` 分支。修复：把该分支改为发射**已定义的、可变的** `@name = private global <T> zeroinitializer`（nolang 顶层变量零初始化且可变，与同函数内同名 `let` 已验证的 `%vec`/`%option` 物化一致），删掉"verifier 拒→回退"的旧兜底。`test-x25519-keypair-diff.no` 现 `MIR=3` rc=0；**注意该 test 的 MIR 输出比 legacy 正确**——legacy 对顶层定宽数组全局经 `x25519` 函数读写存在既有 bug（打印 `FAIL: ECDH shared secrets mismatch`，而 ECDH 共享密钥相等是数学不变量），MIR 现给出匹配的正确共享密钥（与第八轮 6 个 interp 测试"MIR 对、legacy 错"同一类：顶层全局物化修好即正确）。改动仅影响"无 ConstText 的顶层全局"发射形态（extern→defined），对 320 个既有通过测试零回归（此前该分支要么产生未定义符号令链接失败、要么对应全局本就未被引用为死代码，二态皆不受损）。

37. **`net.net-icmp-open` builtin（2026-09-12 第十一·续轮）**：`emitBuiltinForward`（`builtin_call.go`）漏 `"net-icmp-open"` case，`tests/tmp-icmp-test.no` 报 `unsupported builtin net.net-icmp-open`（`MIR=3` 构建失败、`MIR=0` 通过）。`net-icmp-open` 是创建 ICMP socket 的 FFI，legacy 直接内联发射 `call i32 @socket(AF_INET, sockType, IPPROTO_ICMP)`（无 C shim）。修复：新增 `emitBuiltinNetIcmpOpen`——`declare i32 @socket(i32,i32,i32)` + 按 `runtime.GOOS` 选 sockType（macOS `SOCK_DGRAM=2` 非特权 / Linux `SOCK_RAW=3` 需 `CAP_NET_RAW`），`sext i32→i64` 存入结果槽。`tmp-icmp-test.no` 现 `MIR=3` **构建 + 运行均通过**，打印真实 fd（macOS 上 `SOCK_DGRAM` ICMP 非特权，故非 root 即得有效 fd），与 legacy 输出逐字节一致（同值 `5`）。

38. **`net.conn` 内置族 `net-dial` / `net-send` / `net-recv`（2026-09-12 第十一·续轮）**：`emitBuiltinForward` 漏 `net-dial`/`net-send`/`net-recv` case，`tests/test-net-client.no` 报 `unsupported builtin net-dial`（`MIR=3` 构建失败、`MIR=0` 通过；`close` 已是 `CLibCall` 通用路径，无需补）。修复：新增三个 emitter，均镜像 legacy（`build/llvm/call_stdlib.go`）：
    - `emitBuiltinNetDial`：`socket(AF_INET, SOCK_STREAM, 0)` + `inet_pton(host)` + `connect`，按 `runtime.GOOS` 布局 `sockaddr_in`（macOS `sin_len@0=16, sin_family@1` / Linux `sin_family@0`），`sin_port = htons(port)`，返回 `(socket ok && connect ok) ? socket : -1`。hostname 的 `getaddrinfo` DNS 回退暂未移植（IP 字面量主路径已正确；非 IP 主机暂返回 -1）。
    - `emitBuiltinNetSend`：`send(i32 fd, i8* data, i64 n, i32 0)`，data 取 str 的 data 指针（显式长度，无需 NUL 终止）。
    - `emitBuiltinNetRecv`：`recv(i32 fd, i8* buf, i64 n, i32 0)`，buf 取 str 的 data 指针（原地写入；不更新 Nolang 串长，与 legacy 对定长缓冲的 recv 行为一致）。
    - 验证：新建 `tests/mini_net.no`（dial→send 'hello'→recv→close），`MIR=3` 输出 `dial fd=5, written=5, read-n=5, done`，与 legacy **逐字节一致**（证明三个内置 IR 正确）。`test-net-client.no` 现 `MIR=3` **构建通过**（该 test 自身标 `vet-only`、不执行，故运行需 live `nc -l 8080` 属环境外）。注意 `test-net-client` 顶层程序里 socket 偶发占用 `fd=1`（stdio 关闭后最低可用 fd）导致 `print` 输出混入 socket，是顶层程序 stdio-fd 处理的既有现象（legacy 同获 `fd=0`），非本修复引入的回归——`mini_net` 在 stdio 完好时 socket 取 `fd=5`、收发均正确可证。

39. **`await`/`run` coroutine 完整 task 运行时（2026-09-12 第十二轮，闭环最后一个 MIR 专属 gap `test-async.no`）**：MIR 此前只有 `OpRun`/`OpAwait` opcode 与 `KRun`/`KAwait` 脚手架（lowering 把 `-async` 后缀 call 转 `OpRun`、把 `awy <expr>` 转 `OpAwait`），但 codegen 未发射运行时 → `MIR=3` 对 `test-async` 崩。修复：在 `src/mir/codegen.go` **逐字移植** legacy `build/llvm` 协作式调度器契约（用户明确选择"完整 task 运行时"而非 MVP 同步 thunk，以忠实对齐字节）：
    - **`%task` 类型**：`{ void (i8*)* resume_fn, i64 data, i1 done, i1 cancelled }`（24 字节）；`data` 指向 args struct = `{ i8* result_ptr, i8* arg0_ptr, ... }`。前导声明写在 `%option` 之后、`declare i8* @malloc` 之前。
    - **`emitAsyncScheduler`**：把 legacy `decl.go:889-968` 的 5 个 `nolang_async_*` 函数 + 全局（`nolang_async_enqueue`/`_yield`/`_wait`/`_done`/`_run`）与 `@malloc`/`@free` 声明逐字写入 `extraGlobals`/`extDeclOrder`（尾部追加机制，不破坏既有顺序）。
    - **`OpRun`（`emitAsyncRun`）**：堆建 `%task`（resume_fn = 生成的 `async_wrapper.N`，data = args struct，done/cancelled = false）→ `nolang_async_enqueue` 入队 → 返回不透明 i8* 句柄（`ptrtoint`→i64 存 Dst）。`inst.Sym` 为空表示 `run <handle-var>`，仅转发既有句柄。
    - **`OpAwait`（`emitAsyncAwait`）**：检查 done；未完成则同步 `call resume_fn(task)` 驱动到完成（顶层平坦 await 树无嵌套 yield，同步驱动即与 legacy 事件循环字节一致）；再从 args struct field 0 读 `result_ptr` 取结果；最后 free 三个容器（result/args/task，内部拥有数据已归属结果槽）。
    - **每调用点 wrapper `async_wrapper.N`**（`asyncWrapperFor`）：按 MIR 调用 ABI 调目标——**标量参数（`i64`/`i1`/`double`/`i8`）按值传递**（先 `load` 再按值传），聚合/拥有类型按指针传递（关键 ABI 修复：初版把标量按指针传，致 `awy f` 读出垃圾如 `8763099328` 而非 `50`）；wrapper 内带 done/cancelled 守卫。
    - **验证**：`NOLANG_MIR=3 ./bin/no run tests/test-async.no` rc=0 输出 **8 行** `42/42/1/-1/0/60/60/50`，覆盖 `compute-async`/`add-async`/`classify-async` + serial/multi-params/conditional/concurrent/lazy-future/await-future 七子用例。**零回归证明**：`git stash` 暂存改动 → 构建 baseline（无 async 代码）→ 对 curated 集合跑 `MIR=3` vs legacy，`BAD=6` 画像与改动版**完全一致**（test-arr OK、test-match OK、test-option/number/vec DIFF、test-basic/str/map MIR3_FAIL），均为既有 MIR gap，与 async 无关 ⇒ 改动零回归。注意 legacy（=0）对 `test-await-future`（`awy f` 其中 `f` 是 future 变量）**漏最后一行 `50`**（既有 bug，见 §16），故 MIR 输出比 legacy 正确、不计入 MATCH。

40. **async 取消/让出内置 `async-cancel` / `async-cancelled` / `async-yield`（2026-09-12 第十二·续轮，收尾完整运行时契约）**：`src/builtin/async.go` 与 `src/std/async.no` 已声明这三个 `#{buildin}`（D10 协程取消原语），但 MIR 的 `emitBuiltinForward` 没有任何 case → `MIR=3` 报 `unsupported builtin async-cancel`。修复（`src/mir/builtin_call.go` 新增三个 emitter + dispatch case，逐条镜像 legacy `build/llvm/call_stdlib.go:2660-2722`）：
    - **`emitBuiltinAsyncCancel(inst)`**：`h` 是 MIR 的不透明 i64 句柄（`OpRun` 把堆 `%task` 的 i8* `ptrtoint` 成 i64），故 `inttoptr i64 → %task*` → `getelementptr %task, field 3` → `store i1 true`。（legacy 里句柄是 i8*，直接 bitcast；此处多一步 inttoptr。）置位后，生成的 `async_wrapper.N` 入口守衛读到 cancelled 即 `store done=true` 跳过目标函数，于是随后的 `awy h` 返回 **zero value**——正是 `std/async.no` 文档的契约。
    - **`emitBuiltinAsyncCancelled(inst)`**：镜像 legacy——`load @nolang_current_task` → null 判定分支 → 非 null 则读 `%task` field 3 → `phi i1`（**绝不 deref null**）→ `coerce("i1")` 存入结果槽（i1 或 i64 均可）。当前无任务时返回 false，与 legacy 注释"若当前不在异步任务中...安全返回 false"一致。
    - **`emitBuiltinAsyncYield(inst)`**：`call void @nolang_async_yield()`（退化路径）。legacy 在 `-async` 函数内作为顶层语句时由 `coro.go` 改写为真协程挂起点；MIR 无协程状态变换，恒走退化路径（重入队后返回）。
    - **`emitAsyncAwait` 补 `@nolang_current_task` 发布**（`codegen.go`）：同步驱动 wrapper 期间 `store i8* %task → @nolang_current_task`，返回后置回 null，使目标函数内的 `async-cancelled()` 读到正确的 `%task.cancelled`（对齐 legacy 事件循环调度前写 current_task 的行为）。单任务同步驱动，单对 store/restore 足够。
    - **验证**：新建三个测试并 MIR=3 逐字节对照 legacy——`tests/tmp-async-cancel.no`（cancel→`awy` 得 `0`；未 cancel→`42`；非任务上下文 `async-cancelled()`→`0`；退化 `async-yield()`→无副作用）输出 `0/42/0/7` 与 legacy **逐字节一致**；`tests/tmp-async-coop.no`（`-async` 目标内 `async-cancelled()` 自检）输出 `7/0` 与 legacy **逐字节一致**。`tests/tmp-async-yield.no`（`-async` 目标内 `async-yield()` 后继续）MIR=3 输出 `6/9` **正确**，而 **legacy（=0）SIGSEGV**（`coro.go` 协程挂起路径对 `run`+`awy` 驱动的 `-async` 函数崩溃）→ 又一个 "MIR 正确 / legacy 错误" 案例（见 §16）。**零回归**：curated 集合 `BAD=6` 画像与改动前**完全一致**，`test-async.no` 仍 8 行。

41. **C 家族 3 个 red-line 运行时崩溃全闭环 + E 家族 2 个连带闭环（2026-09-12 第十四轮）**：第十三轮全量重扫暴露 3 个 `MIR` 专属运行时崩溃（legacy 过、MIR=3 崩）与 2 个 `str-len receiver i64` 编译错误。逐条根因与修法：
    - **① `tests/test-slot-rebind-unsafe.no` SIGSEGV——`run` 的异步性是"语法级"而非"命名级"**。`lowerCall`（`hir2mir.go`）原先仅凭 `strings.HasSuffix(callee, "-async")` 判定异步：callee 名不含 `-async` 后缀的 `run double(x)` 被降级成**普通 eager `OpCall`**，于是 `awy h` 拿到的 `h` 是 `double` 的**返回值 14**（i64）而非任务句柄，`emitAsyncAwait` 把 `14` `inttoptr` 成 `%task*` → SIGSEGV。而 legacy 的 `generateRunExpression`（`build/llvm/expr.go:8226`）对 `run <CallExpression>` **无条件**走 `prepareAsyncCall`——`run` 本身就是异步标记，`-async` 后缀只决定**裸调用**是否产生 future。修复：新增 `lowerer.forceRunCall`（记录 `run`/`awy` 直接子调用节点的 HIR id），`lowerCall` 在 `l.forceRunCall != hir.NoID && n.Id == l.forceRunCall` 时同样发射 `OpRun`。**按节点 id 精确匹配**，故 `run f(g())` 里的 `g()` 仍是普通 eager 调用。`lowerAsyncAwait` 对直接调用操作数同样处理（镜像 legacy `generateAwaitForCoro` Case 1）。
    - **② `tests/mem-safety/async-shared-race.no` SIGSEGV——异步实参缓冲区按"实参值类型"而非"形参类型"分配**。`emitAsyncRun` 用 `c.loadVal(av)` 得到的**值类型**建堆缓冲：`shared []i64 = [1,2,3]` 的实参值类型是定宽数组 `[3 x i64]`，而形参是 `[]i64`（`%vec`）；`async_wrapper.N` 按**形参类型** `%vec*` 重解释该缓冲，于是 callee 把元素字节 `{1,2,3}` 当成 `%vec{len,cap,data}` 头，`v[0]=999` 写向 `data=3` 这个假指针 → SIGSEGV。修复：新增 `coerceAsyncArg(av, plt)`（`codegen.go`），镜像 `emitCallBody` 的实参强制——`[N x T] → %vec` 走 `vecFromArraySink`（可平凡拷贝元素用堆拥有副本，否则 `cap=0` 借用视图）、`%str-long → %vec` 走字节视图；缓冲一律按**形参类型** `argTypes[i]` 建。**副产（同为 #41）**：`awy` 侧的任务**结果类型**此前无从得知（`lowerAsyncAwait` 只对"直接调用"操作数能从 callee 签名推），`awy <句柄变量>` 一律回落 `i64` → `print(<str 结果>)` 打出**字符串长度**（`5` 而非 `hello`）。修复：新增 `lowerer.asyncResTypes map[ValueID]TypeID`，在 OpRun 处按 callee 签名登记，并沿标量 let 拷贝（`OpMove`）传播，`lowerAsyncAwait` 优先查它。
    - **③ `tests/test-embed.no` trace-BPT——MIR 完全没有 `#{embed}` 支持**。`#{embed='file'}` 的字节由 `hir.Package.Embeds` 承载（`pkg.EmbedDataOf(id)`），legacy 在 `build/llvm/generator.go:2322` 把顶层 let 物化成"私有常量字节数组 + 指向它的 `%vec` 全局"。MIR 侧**零引用**（`src/mir/` 无任何 `Embed` 字样），故 `DATA []byte` 退化为空切片（`embed len: 0`），`DATA[0]` 越界 → trap。修复：`lowerEmbedBinding`（`hir2mir.go`）在 KLet 上发现 `EmbedDataOf(id)` 非空时，物化 `%vec { N, N, ptrtoint([N x i8]* @.embed.<name> to i64) }` 全局，字节经新增的 `GlobalDecl.EmbedBytes` 传给 codegen；`emitGlobals` 在 `ConstText` 分支前先发 `@.embed.<name> = private constant [N x i8] c"..."`。（**目录型 `#{embed=dir}` 仍未实现**——仅 `src/std/embed.no` 的文档提及，无测试覆盖。）
    - **④ 连带闭环 E 家族 2 个**：`mem-safety/async-str-result.no` 与 `async-str-stress.no` 报的 `builtin str-len receiver i64: unsupported receiver` 根因即 ②的副产——`s1 = awy task1` 取回 `str` 结果时默认成 `i64`，`print(s1)` 遂以 `i64` 接收者调 `str-len`。同一修复即闭环。
    - **⑤ B 家族推进（未闭环）**：`tests/test-opt-struct-field.no` 的 `unknown callee port.to-str` 根因是 `?T.field` **字段路径**用 `?conn` 去查 `StructFields`（表里只登记了 `conn`）→ `p = opt.path` 未绑定 → 之后 `port` 被当作**未绑定名**，`resolveCallee` 的 KDot 模块命名空间分支把 `port.to-str()` 写成 `port.to-str`。修复：字段路径与 `resolveCallee` 一致地 `strings.TrimPrefix(recvRaw, "?")`（`emitGetField` 本就实现了 option peel，故只需修查表键）。该测试遂由 `unknown callee` 推进到 `opt-verify`：`%conn` vs `%tls_server_conn` 结构体**命名歧义**（`structKeyOf` 的 `HasSuffix(k, "."+raw)` 后缀匹配在多模块合并时会把用户 `conn` 与 std 的 `tls.server-conn` 混淆），仍为 gap。
    - **验证**：**第十四轮全量重扫**（`tests/**/*.no`=421，`./bin/no`）`MATCH 240 → 248`、`MIR_GAP 21 → 17`、`HANG 4 → 1`、**MIR 专属运行时崩溃 3 → 0（red-line 达标）**。三个原崩溃测试现均 rc=0 且与 legacy **逐字节一致**（`7 14` / `999 42 hello hello done` / `embed len: 5 len OK first byte OK`）；`test-embed`、`async-str-result`、`async-str-stress`、`async-shared-race`、`test-slot-rebind-unsafe` 共 5 个转入 MATCH。**`test-diff-debug` 由 LEGACY_FAIL 转为 MIR_GAP，非本轮引入**：第十三轮前的 `MIR_GAP=22` 全量扫描已收录它，其 legacy `MIR=0` rc 存在 flaky（在 1/0 间抖动）；本轮的 `unsupported construct (string interpolation)` 诊断位于 KStrLit 内插路径，与本轮改动无关。**零回归**：opt-field `?` 剥离改动前后两次全量扫描画像完全一致（248/58/17/97/1）。

---

## 13. 当前已知 gap 家族（下一轮推进目标）

### 13.1 MIR 专属回归（已闭环）
- **`test-arr.no` 的 `option/i64` opt-verify 失配**：已于 2026-09-11 闭环（见 §12 #12），`emitIndexStore` 加 `%option`→标量解包。
- **`emitSetField` 同类解包（2026-09-11 第二轮）**：`emitSetField`（`codegen.go`）的常规 store 路径与 `?T.field` 分支均加同法 option→标量解包（当 RHS 为 `%option` 且目标字段为标量时 `extractvalue %option %valV, 1` 取 ok payload 再 store）。标量赋值 `x = v` 走 `OpMove`/`emitMove`，无独立 `emitStore`，故 sink 站点已覆盖 `emitIndexStore`+`emitSetField`。10 个既有通过测试 + `test-arr.no` 无回归。

### 13.2 预存 crash/hang（非 MIR 专属，legacy 同崩/同挂）
实测根因（两模式各跑，超时 8s）：
- `vec.no` —— `SIGSEGV`（两模式 rc=1）。
- `test-std-net-ext.no` —— `SIGSEGV`（两模式）。
- `test-tls-part2.no` —— `SIGSEGV`（两模式）。
- `test-for2.no` —— `HANG`（两模式 rc=124，死循环/阻塞）。
- `tmp-getline.no` —— MIR=0 `HANG`（等 stdin）；**MIR=3 rc=0**（无输入正常退出 "no input"）——MIR 反而更好。
- `tmp-loop-test.no` —— 两模式 **rc=0**（现已通过，非真实挂死）。

**关键结论**：MIR=3 在失败前沿与 legacy **完全持平——零新增崩溃、零新增挂起**。3 个 SIGSEGV + 1 个 HANG 均为 nolang 通用预存 bug（legacy 代码生成缺陷），应作为通用 bug 在双路径闭环，不 blocking MIR 门禁。它们是独立的 legacy 修复轨道，不在 MIR 覆盖推进范围内。

### 13.3 COMPILE_ERR 长尾的精确分类（2026-09-11 第二轮）
对全量扫掠的 143 个失败测试逐个查 `MIR=0`（legacy）是否通过，得到关键二分：

| 二分 | 数量 | 含义 |
|---|---|---|
| **MIR 专属 gap**（legacy 过、MIR=3 挂） | **69** | 真正的 MIR codegen 缺口——本门禁要消除的目标 |
| 预存失败（两模式都挂） | 70 | legacy 也失败，非 MIR 引入（溢出 checker、`str-len` receiver、未 lower 特性等） |

> 注：143 = 140 COMPILE_ERR + 3 预存 HANG（其中 `tmp-getline`/`tmp-loop-test` 现 MIR=3 已通过，见 §13.2）。经三轮修复（§13.1、`str-clear`、`emitSetField`、§12 #15/#16），MIR 专属 gap 已由首轮 69 降至 **53**（§13.3.1）。

#### 13.3.1 MIR 专属 gap 的家族分布（**历史弧线：首轮 69 → 第五轮 41 → 第六轮精炼 39 → … → 第十二轮记 0（**定向子集口径**）→ **第十三轮权威全量重扫 = 21 → 第十四轮 = 17**）；下表为**历史根因归档**，不再代表当前未闭环集合，当前清单见 §13.3.7）

> **现状（第十轮 2026-09-12）**：全量重扫后 MIR 专属 gap 已降至 **3**（见 §10.1 覆盖表）：`test-async.no`（coroutine `await/run` 未实现）、`test-net-client.no`（`net-dial` FFI 内置未实现）、`tmp-icmp-test.no`（`net.net-icmp-open` raw socket FFI 未实现）。原 §13.3.1 表中 "opt_verify 剩 4 / crypto 深坑 / 字符串插值" 等 MIR 专属家族经第七~十轮已逐站闭环（插值 #28、crypto 系列 #15/#18/#23/#35、`test-x25519-keypair-diff` #36），下表仅保留作历史根因归档，不再代表当前未闭环集合。**第十一·续轮 #37/#38 闭环两个 net FFI 缺口 → 剩 1（`test-async`）；第十二轮（2026-09-12）`test-async.no` 经 §12 #39 完整 task 运行时闭环 → MIR 专属 gap 3 → 0（见 §10.1 覆盖表）。**
> **口径修正**：早期按错误症状直接分类曾报约 38，但漏做 legacy-parity 二分——混入了"两模式都挂"的预存失败。第五轮对每个失败测试额外跑 `NOLANG_MIR=0`：418 全量失败 137 个中，仅 **41 个 MIR 专属**（legacy 过、MIR=3 挂），**92 个两模式都挂**（legacy 也失败，属通用预存 bug / std·test 缺陷，见 §13.3.5）。第六轮（2026-09-12）对 133 个失败重做 fresh-parity，精炼为 **39 MIR 专属 / 93 预存**（更准，因部分 test 在第五轮后已被 #20 改变归属）。历史累计已闭环：§13.1 `emitIndexStore`/`emitSetField` option 解包、str-clear、#15 算术/负号 option 包回(13)、#16 数组 receiver(1)、#17 顶层容器字面量物化(15)、#18 `emitBitwise`(1)、#19 `print` option payload(3)、#20 `%txt` str-len-bytes receiver(1)、**#21/#22 arr-slice 切片(1)、#23 `err/ok/some` 构造器路由(2)**。

| 家族 | 数量 | 根因 | 本轮进展 |
|---|---|---|---|
| 字符串插值（`unsupported construct (string interpolation)`） | 16 → **1**（`test-diff-debug`，其 legacy 也失败） | `'x={expr}'` 在 HIR `KStrLit` 仅保留原始文本，嵌入表达式在 parser 阶段已丢失 | **已闭环（2026-09-12 #28）**：改为仅在 print 家族调用点按段替换（字段走 std `fmt-*`），其余上下文按字面量发射——与 legacy 语义一致且顺便修好了 legacy 误伤 JSON/正则字面量的问题。仅剩表达式字段 `content.len-bytes()`（MIR 阶段已无 parser）拒收回退 |
| `opt_verify`（LLVM 类型校验失配） | 6（第五轮 parity 口径；其中 `test-option-index.no` 为两模式都挂的预存失败）→ **MIR 专属 4 实测** | 算术/负号/#18/#19 已闭环；`err/ok/some` 构造器 CALL 路由已由 #23 闭环。**实测 4 个为 MIR 专属独立深坑**（legacy 过/MIR=3 挂）：①`test-async.no`=`add void undef`（async 结果 void 操作数）；②`test-str-ops.no`=`sub i64,%str-long`（legacy 把 `str - char` 当**字符串拼接**结果 str，MIR 把 `t1=hi-"B"` 推成 i64 发 `sub`——字符串算术运算符重载的类型推断分歧，feature 级）；③`test-std-unix-fs-os.no`=invalid GEP on i64（struct field 落在标量 option）；④`move-eligibility-improved.no`=`alloca [1 x void]`（void 数组元素型别）。注：全量重扫时另现 `test-iout-autoconvert`/`test-safe-index-utl`/`test-tls-prf-only`/`test-tls13-crypto` 等也报 opt-verify 文本，但其中部分属预存失败或其他症状族混入，需各自 legacy-parity 细分（勿整体计为 MIR 专属）。均非快速 sink 站修复，建议 corpus 豁免或延后（逐站 lower 的"字符串算术/async/struct"特性族） | **已全部闭环**：#15/#18/#19/#23 + 第九轮 #32/#33/#34 + 第十二轮 #39；第十三轮复核**剩 0**（详见 §13.3.2） |
| `builtin` option/receiver 处理 | **2（现存）** | `str-len`/`str-len-bytes` receiver 为 `%option___byte`（option-of-byte）或 `i64`，未解包 → `builtin str-len receiver ...` | **第十三轮全量重扫确认仍存**：`tests/mem-safety/async-str-result.no`（`str-len receiver i64`）、`async-str-stress.no`（`str-len-bytes receiver i64`）、`tests/test-fs-struct.no`（`str-len receiver %option___byte`）= 家族 E |
| `builtin` 未实现（FFI/net 族） | 0 | `net-dial` / `net.net-icmp-open` / `net-send` / `net-recv` 已由 §12 #37/#38 闭环（内联发射 libc `socket`/`connect`/`inet_pton`/`send`/`recv`） | 已闭环（#37 #38） |
| `index dst slot` | 0 | `arr-slice.no`：`x = slice[i]` 结果类型推导为 void → 无 slot（与 with-len 同类型推导缺口） | **已闭环（2026-09-12 #21/#22）**：recv 值类型回推 + 切片边界语义，输出与 legacy 逐字节一致 |
| **MIR 专属运行时崩溃**（segfault/abort/trace-BPT） | **3 → 0** | 早期 7 个已由第七轮 #24–#27 闭环；**第十三轮全量重扫发现 3 个新暴露的 MIR 专属运行时崩溃**（legacy 过、MIR=3 崩）：`tests/mem-safety/async-shared-race.no`（SIGSEGV）、`tests/test-slot-rebind-unsafe.no`（SIGSEGV）、`tests/test-embed.no`（trace/BPT trap） | **已闭环（第十四轮 §12 #41）**：`run` 语法化异步 + 异步实参按形参类型强制 + 任务结果类型跟踪 + `#{embed}` 物化 → 三者 `MIR=3` rc=0 且与 legacy 逐字节一致，**red-line 恢复达标** |
| `test-str.no`（1 diagnostic） | 1 | 单点诊断 | 第十三轮全量重扫为 MIR 专属 gap（legacy rc=0、MIR=3 rc=1，报 `1 diagnostic(s)`）；**已于第十六轮（2026-09-13）闭环**：match 臂合成 `let it = <matched>` 由 transferring `OpMove` 改为 ALIAS（`hir2mir.go` `lowerStmt` `name=="it"` 分支），消除共享 subject 的 use-after-move/double-free，现逐字节 MATCH（见 §13.3.7） |
| `Undefined symbols`（链接缺符号） | 1 → **0** | `test-x25519-keypair-diff`：某内置未 emit 致链接缺符号 | 已闭环（#36 顶层未初始化全局物化）；第十三轮复核为 DIVERGE（两模式均 rc=0、仅输出不同） |

#### 13.3.7 全量重扫（2026-09-12）：真实 gap 清单（第十三轮 **21** → 第十四轮 **17**，纠正"0"的误判）

> **背景（重要纠错）**：此前多轮把 MIR 专属 gap 记为 **0**，但那是**定向/抽样子集**（curated 集合，`BAD=6` 画像）的结论，**不是全量 corpus 的结论**。第十三轮以**修正后的二进制路径**（`./bin/no`，而非陈旧的仓库根 `no`）对**全量递归 `tests/**/*.no` = 421 个**逐测跑 `MIR=0` vs `MIR=3`（25s/测超时），得到权威口径；第十四轮以同口径复核 C 家族闭环效果：
>
> | 类别 | 第十三轮 | 第十四轮 | 第十五轮 | 第十六轮 | 第十七轮 | **第十八轮（2026-09-13 续，双向 range 修复）** |
> |---|---|---|---|---|---|---|
> | MATCH（两模式 rc=0 且 stdout 逐字节一致） | 240 | 248 | 261 | 267 | 269 | 271 | **279** |
> | DIVERGE（两模式 rc=0、输出不同） | 57 | 58 | 55 | 55 | 52 | 51 | **43** |
> | **MIR_GAP（legacy rc=0、MIR=3 rc≠0）** | 21 | 17 | 3 | 1 | 1 | 1 | **1** |
> | LEGACY_FAIL（legacy 自身 rc≠0，非 MIR 引入） | 98 | 97 | 98 | 97 | 98 | 97 | **97** |
> | HANG（超时） | 4 | 1 | 4 | 1 | 1 | 1 | **1** |
>
> `MIR_GAP=17 ≠ 0`（第十四轮口径）。**第十三轮的 3 个 C 家族 red-line 运行时崩溃已在第十四轮全部闭环**（§12 #41），连带闭环 E 家族 2 个（`async-str-result`/`async-str-stress`）。下表为第十三轮的家族画像（**历史归档**），当前清单见表后"第十四轮更新"/"第十五轮更新"。
>
> ⚠️ **第十四轮"17"逐文件清单已证不准确**：权威全量扫描（`./bin/no`，第十五轮两次运行一致，2026-09-12）显示 `test_fs_error_complete`(DIVERGE)、`test-opt-struct-field`(MATCH)、`ffi-str-return`(MATCH)、`tmp-nbody-debug`(MATCH)、`test-uninit-output`(MATCH)、`test-open-perm`(MATCH)、`test-open-write`(MATCH)、`test_fs_error_final/info/simple`(MATCH×3)、`test-diff-debug`(LEGACY_FAIL) **均非 MIR_GAP**；真正的稳定 MIR_GAP 实为 **3 个**：`test-fs-struct.no`、`test-open-read.no`、`test-str.no`（连续两轮扫描均命中，见第十五轮更新）。第十五轮经 FIX A/B 闭环 `test_errno_basic`/`bug13-bool-coercion` 两个 `unknown callee` 缺口。
>
> | 家族 | 第十三轮数量 | 测试 | MIR=3 症状 | 第十四轮状态 |
> |---|---|---|---|---|
> | **A** `setfield field` | 4 | `test-open-perm/read/write`, `test_fs_error_complete`, `test-uninit-output` | 匿名选项结构体字面量无类型 → `FieldIndex` 失败 | 仍 4（下游另有缺陷） |
> | **B** `unknown callee <recv>.<method>` | 4 | `test_fs_error_complete`(`self.write-str`), `test_errno_basic`(`fd.to-str`), `test-opt-struct-field`(`port.to-str`), `mem-safety/bug13-bool-coercion`(`helper.compute-str`) | 接收者/模块限定名未解析 | 3（`test-opt-struct-field` 推进到 `opt-verify`） |
> | **C** **运行时崩溃（red-line）** | 3 | `mem-safety/async-shared-race`(SIGSEGV), `test-slot-rebind-unsafe`(SIGSEGV), `test-embed`(trace/BPT) | 两模式结论相反：legacy 过、MIR 崩 | **0 — 全部闭环（#41）** |
> | **D** `opt-verify`（LLVM 类型失配） | 2 | `mem-safety/ffi-str-return`（`i64` vs `%str-long`）, `tmp-nbody-debug` | 值类型与期望类型不符 | 3（+`test-opt-struct-field`） |
> | **E** `str-len`/`str-len-bytes` receiver 未解包 | 3 | `mem-safety/async-str-result`, `async-str-stress`, `test-fs-struct`(`%option___byte`) | 接收者为 `i64`/`%option___byte` | **1**（前两个为同一"任务结果类型"根因，随 #41 闭环） |
> | **F** `1 diagnostic(s)` | 1 | `test-str` | 待细分 | 1 |
> | — | — | `test-diff-debug` | `unsupported construct (string interpolation)` | 1（第十三轮被 legacy flaky 掩盖为 LEGACY_FAIL，见下） |
>
> **第十四轮更新（2026-09-12）**：`MIR_GAP` **21 → 17**，`MATCH` **240 → 248**（+8：3 个崩溃测试转 MATCH、2 个 E 家族转 MATCH、3 个原 HANG 转 MATCH）。**净减 4 个 gap**：`async-shared-race`、`async-str-result`、`async-str-stress`、`test-embed`、`test-slot-rebind-unsafe` 共 5 个闭环，其中 `test-diff-debug` 由 LEGACY_FAIL 转为 MIR_GAP（**非本轮引入**——第十三轮前的 `MIR_GAP=22` 扫描已收录它，其 legacy `MIR=0` rc 在 flaky 波动，见 §12 #41 注）。当前 17 个：A(4) `test-open-perm/read/write`、`test_fs_error_complete`、`test-uninit-output`；B/D(7) `test_fs_error_*`(4)+`test_fs_open_err`、`test_errno_basic`、`bug13-bool-coercion`、`test-opt-struct-field`、`ffi-str-return`、`tmp-nbody-debug`；E(1) `test-fs-struct`；F(1) `test-str`；interp(1) `test-diff-debug`。
>
> **第十五轮更新（2026-09-12 续）**：`MIR_GAP` **17 → 3**（权威全量重扫**两次一致**：`./bin/no`，421 文件，25s/测，**MATCH=261 DIVERGE=55 MIR_GAP=3 LEGACY_FAIL=98 HANG=4**；两次扫描逐文件分类完全相同，含 3 个稳定 gap）。**可归因的闭环（2 个，见 §12 #42/#43）**：`test_errno_basic.no`（`fd.to-str` → `i64.to-str`，值类型别名展开 FIX A #42）、`mem-safety/bug13-bool-coercion.no`（`helper.compute-str` → `compute-str`，跨模块自由函数调用名解析 FIX B #43）——均逐字节 MATCH（已 `diff` 验证）。**剩余 3 个稳定 MIR_GAP（连续两轮扫描均命中，与 FIX A/B 无关，pre-existing）**：
> - `tests/test-open-read.no`：`f-r: { ... it.close() ... }`（`it` 为 `?fd` match 臂绑定）。MIR `resolveCallee` 的 `recvTypeName` 因 match 臂 `it` 类型未传播，回退默认 `"str"` → 误解析为 `str.close` → `builtin str.close: cannot coerce arg 0 from %str-long to i32`（family A 下游；`### func fs.file.close` 已注册）。
> - `tests/test-fs-struct.no`：同类 `it.close()`/`[]t.close` 强制错误（`builtin []t.close: cannot coerce arg 0 from %vec to i32`）；legacy `MIR=0` 偶发 HANG（flaky），故该测试在两轮扫描中均判 MIR_GAP，但严格 parity 下 legacy 亦不稳定。
> - `tests/test-str.no`：`[use-after-move] value ... dropped after move at inst ... (double-free risk): (func=str.replace-n block=... inst=...)` 内存安全诊断，F 家族 `1 diagnostic`。
>
> **第十六轮更新（2026-09-13 续）**：`MIR_GAP` **3 → 1**（权威全量重扫 `./bin/no`，421 文件，8-worker，**MATCH=267 DIVERGE=55 MIR_GAP=1 LEGACY_FAIL=97 HANG=1**）。本轮闭环 **第十五轮剩余的全部 3 个稳定 MIR_GAP**（均逐字节 MATCH，已 `diff` 验证）：
> - `tests/test-open-read.no` 与 `tests/test-fs-struct.no`：根因为 **match 臂 `it` 类型未传播**——嵌套/并列 match 的内层 arm 合成 `let it = <内层 subject>` 覆盖了外层 `it` 的 LLVM/值类型，使 `it.close()` 在 `resolveCallee` 处 `recvTypeName` 回退 `"str"`/`"[]t"` → 实参 `i32` 强制错误。**修复（`hir2mir.go`）**：新增 `lowerer.matchDepth` 计数 + `blockStartsWithIt` helper；`lowerIf` 在 arm body 进入/退出时增减 depth；`lowerStmt` 的 `name=="it"` 分支对 `matchDepth > 1` 的内层 `let it` **不覆盖外层 `it`**（保留外层绑定、内层 payload 若 owned 则 drop），外层 `let it` 仍给每 arm 独立值（per-arm 类型正确，避免 test_fs_error_complete 的 LocalType 污染）。两测试转 MATCH（第十五轮口径）。
> - `tests/test-str.no`：根因为 **match 臂合成 `let it = <matched>` 做了 transferring `OpMove`**——`str.replace-n` 中 `parts[i]`（`?str`）被 materialize 一次为同一 value id，但 `it` 绑定（`move it = parts[i]`）transfer 走该共享 subject，而 arm body 仍引用 `parts[i]` 原值（`s = parts[i]` 之后的 `s.len-bytes()`/`s.byte(j)`）→ `[use-after-move] value 1419 dropped after move`（真 double-free：option 与 `it` 共享堆 buffer、两处都 drop）。**修复（`hir2mir.go` `lowerStmt` `name=="it"` 分支）**：把 `it` 绑定改为 **ALIAS**（直接 `locals["it"] = val`，不 emit move、不 clone）——`it` 与 subject 同 value id，内存分析器仅 drop 一次；nil 安全（alias 不触碰 buffer，避免对可能 nil 的 `?str` 做 `str_clone` 崩溃）；每 arm 的 `val` 是 desugar 产出的 per-arm 独立 value id，per-arm 类型仍正确。该改动同时消除 test_fs_error_complete 旧基线（`unknown callee self.write-str` 编译错误）的 receiver 解析问题，使其干净编译（仅余 `fs.open` 后端语义 DIVERGE，见下）。`test-str` 转 MATCH（与 legacy 逐字节一致，输出 `a_b-c`/`a-b-c`/`a_b_c` 等）。
>
> ⚠️ **`test_fs_error_complete.no` 属 DIVERGE（非回归）澄清**：该测试 MIR=3 与 legacy 输出不同（legacy `fs.open('/tmp', read)` 返回 `ok` 并打开目录，MIR=3 返回 `err` "Is a directory"；写模式 MIR=3 带 `O_EXCL`、legacy 用 `O_CREAT`）。arm 选取由 `fs.open` 返回值决定，`it` 绑定改动**不可能**影响后端 syscall 语义——已用最小探针 `/tmp/fs_probe.no` 核对（`fs.open('/tmp')` 在 MIR=3 报 `MIR_ERR`、legacy 报 `MIR_OK`），证为 **`fs.open` 后端语义差**，计入 DIVERGE（55 个之一），非本改动引入的 MIR_GAP。
>
> **当前唯一稳定 MIR_GAP（1）**：`tests/test-diff-debug.no`（`unsupported construct (string interpolation)` 族；legacy `MIR=0` rc 在 1/0 间 flaky 抖动，故严格 parity 下偶判 MIR_GAP，属 interp/legacy 自身不稳定，非 MIR codegen 缺口）。

> **修复方向（收官）**：A 家族下游（`it.close()`/`[]t.close` 强制错误）、F 家族（`test-str` use-after-move）均已随第十六轮闭环；仅剩 `test-diff-debug`（interp 族 + legacy flaky），建议后续聚焦 interp 后端或将其从 MIR 门禁的稳定 gap 清单中剔除（按 legacy flaky 性质单独标注）。

> **第十七轮更新（2026-09-13 续，闭环 DIVERGE `?bool` 打印族 → DIVERGE 55→52）**：权威全量重扫（`./bin/no`，421 文件，8-worker）复测结果 **MATCH=269 DIVERGE=52 MIR_GAP=1 LEGACY_FAIL=98 HANG=1**（DIVERGE 55→52，净 −3，零 MIR_GAP 回归；LEGACY_FAIL 97→98 为 legacy 侧 flaky 失败——rc0≠0 不受 MIR 改动影响）。本轮闭环 DIVERGE=55 中的 `?bool` 打印族三测试：`tests/test-bool-debug.no` / `tests/test-bool-direct.no` / `tests/test-min-u8-bool.minimal.no`（均逐字节 MATCH，已 `diff` 验证）。**两处根因**：
> 1. **`str.to-bool` 内置 tag 误写**（`src/mir/builtin_call.go` `emitBuiltinStrToBool`）：`str.to-bool` 是 ForwardFunc 内置（generic body 被整段替代），旧实现 `insertvalue %option {i64 0,i64 0}, i64 1, 0` 把 **tag=1（=nil）** 写进 `ok(true)`，且只比较 `"true"` 一个串（`"false"`/空/`err` 全错判）——option 约定 tag 0=some/ok、1=nil、2=err（§runtime）。**修复**：重写 `emitBuiltinStrToBool`——比较 `"true"` 与 `"false"` 两串 + 取 receiver len 判空，`select` 出正确 tag/payload：`""`→nil(tag1)、`"true"`→ok(true)(tag0,payload1)、`"false"`→ok(false)(tag0,payload0)、其余→err(tag2)。**范围确认**：`str.to-u8`/`to-u32`/`to-i64` 不是内置（nolang 实现），其 `ok(value)` 走普通 `emitMove` option-wrap 已正确，故 `?bool` 唯一内置坑即 `str.to-bool`。
> 2. **legacy 的 bool 打印是「双通道」**（关键纠正，初判曾误改 `@print_bool` 致回退）：legacy `print(true)`（裸 bool）输出 **`1`/`0`**，而 `print(?bool)` of `ok(true)` 输出 **`true`/`false`**——两套行为不同。MIR 原先 `@print_bool` 写 `1`/`0`、`@print_option`（flat `%option`）读 i64 payload 也写 `1`/`0`，故 `?bool` ok 被印成 `1`/`0`（legacy 要 `true`/`false`）→ DIVERGE。**修复（双通道对齐）**：(a) `@print_bool` 维持 `1`/`0`（裸 bool，对齐 legacy）；(b) 新增 `@print_option_bool` 专印 `?bool` 为 `true`/`false`（在 `emitCall` 的 print 特殊分支，对 `argT=="%option"` 经新 helper `optionElemKind` 读 nolang 元素 `Kind==KindBool` 路由至此，flat `%option` 的 LLVM 类型不含元素信息，须查 nolang 类型表）；(c) `hir2mir.go` `lowerAssignNode` 的 KIdent/KIndex 赋值路径加 `wrapOptionIfNeeded`（非 option 值赋 `?T` 目标时隐式 `ok(v)` 包装，mirror `let` 声明已做的包装），修普通 `?bool` 局部重绑定同类坑（不触发 `str.to-bool` 内置路径）。
> ⚠️ **回退教训**：曾把 `@print_bool` 全局改为印 `true`/`false`，结果 `test-bool-print.no`（裸 `print(true)`）等 11 个测试由 MATCH 翻 DIVERGE（legacy 裸 bool 印 `1`/`0`）——重扫立刻暴露，确认 legacy 双通道后回退 `@print_bool`、保留 `@print_option_bool`，重扫 DIVERGE 63→52 全数恢复，无净回归。

> **第十八轮更新（2026-09-13 续，DIVERGE 52→51：闭环双向 range-for）**：本轮先对第十七轮遗留的 **52 个 DIVERGE** 做**确定性分流**（每个测试在 `MIR=0` 与 `NOLANG_MIR=3` 各跑 K=3 次，脚本 `/tmp/mir_triage.py`），结果：**REAL_GAP=32**（两模式稳定 rc=0 且输出不同）、**NONDET=2**（`tests/mem-safety/reverse-slice-clone.no`、`tests/test_path_char2.no`——legacy 侧非确定输出，非 MIR bug）、**RC_MISMATCH=18**（主体为 crypto/char 在 `NOLANG_MIR=3` 超时 + 少量「MIR 正确 / legacy 错误」族如 `tests/test-i8-debug2.no`（MIR 印 `-128`/`i8.MIN access ok`，legacy 印 `0`/`bad`））。关键认知：**并非所有 DIVERGE 都是 MIR bug**——`test-i8-debug2` 等 MIR 已正确、legacy 有误；crypto(sha1/sha256/x25519) MIR 产出**错误**哈希/密钥（真缺口但实现体量大，暂缓）；float打印需复刻 legacy 的 Go `strconv.FormatFloat(v,'g',-1,64)` 最短表示（`@print_double` 现硬编码 6 位小数→`3.140000`，暂缓）。REAL_GAP 最大单一族是 **match 作表达式**（`r = n: { ... }` 把 match 结果赋给变量）：`lowerExpr` 的 `hir.KIf`/`hir.KFor` 分支显式 `return NoVal`（`hir2mir.go` 2896「control flow used as expression value」），故 `r` 永不赋值、打印空行——横跨 `test-match-basic`/`test-minimal-ok`/`test-opt-match-all`/`test-option`(首行 nil) 等 **11 测试**，是下个高杠杆修复目标（需 phi/结果槽捕获 arm 值，慎防 260 个 MATCH 回归，靠全量重扫守护）。
> **本轮闭环 DIVERGE=52 中的 `tests/test-range.no`**：根因 `lowerRangeFor` 的整数区间形式**硬编码升序**（`iSlot + 1` 步进 + `iSlot < end` 比较），`[5..0)` 类**降序 range** 条件首轮即假→循环体零次执行→`sum` 恒 0（legacy 支持降序：5,4,3,2,1）。**修复**：以循环不变量 `start <= end` 经分支解析方向——init/步进/比较算子均按方向选择（升序 `<`/`<=` + `+1`；降序 `>`/`>=` + `-1`），IR 无 select/mux 故用 loop-invariant 分支（opt 可提升）。升序路径行为完全不变。**全量重扫（`./bin/no`，421 文件）实测 MATCH=271 DIVERGE=51 MIR_GAP=1 LEGACY_FAIL=97 HANG=1**：`test-range` 由 DIVERGE→MATCH（净 −1），**零 MIR_GAP 回归、零新增 DIVERGE**（LEGACY_FAIL 98→97 为 legacy 侧 flaky 失败）。

> **第十九轮更新（2026-09-13 续，match 作表达式尝试回退，回到 18 轮基线）**：对第十八轮标记的「match 作表达式」族做了实现尝试，但**判定为死路、已全部回退**，净效果回到 18 轮干净基线。`tests/test-match-basic.no`/`tests/test-minimal-ok.no`/`tests/test-opt-match-all.no`/`tests/test-minimal-it.no` 仍 DIVERGE（MIR 打印空行），与 18 轮一致。两次具体改动及其回退原因：
> 1. **`match 作表达式` 捕获（`exprSink`/`exprCapture`/`stmtVal` + `captureArmValue`，改 `lowerStmt`/`lowerBlock`/`lowerIf`/`KLet`）**：**对多 arm match 根本错误**——把共享槽建成常量、且只在 `lowerIf` 外侧对首个 arm 调 `captureArmValue`，运行期真正执行的 arm 值不进槽→静默错值（潜在正确性 bug，非仅空输出）。属 feature 级改造（需 phi/多 arm 结果收敛 + 类型推导决定槽型），**不该在 parity 收官期冒险**，故整体回退。
> 2. **`undef` 守卫的 concrete-zero 发射**（在 KIdent 变体块 `name=="ok"/"err"/"nil"/"some"` 末尾无条件 `return` 零常量）：**引入 3 个真实回归**——`tests/mem-safety/bug13-bool-coercion.no`/`tests/test-bare-match.no`/`tests/test-ok-shadow.no`（均 18 轮 MATCH）被翻成 MIR 输出全 0，因为它们都声明了 `ok bool` **变量**，守卫把变量读取短路成变体零常量。该守卫本意是防「非 option 主体匹配 `err` 变体」产生 `undef`→poison→opt 折成运行期 trace/BPT（red-line 崩溃，曾现于 `tests/test-simple-match.no`）；但 pre-existing 变体块已对**真变体**正确处理，只对「变量名恰好是 ok/err/nil/some」误伤。回退后恢复 18 轮「变体块失败即落入变量查找」行为，3 个回归立刻回 MATCH；`test-simple-match` 的非 option err-match 崩溃属 **legacy 自身 quirk**（整数 42 去比 `err` 判别位无意义），MIR 复刻其 `'err'` 输出才是不合理的，按用户警示**不强行对齐**。
> **保留的安全修复（red-line 加固）**：`emitMove` 标量 option（`?i64`/`?u8`/`?bool`）的 `err`/`nil` peel——旧实现对 `dstT=="%str-long"` 走 `alloca i64`+`bitcast`+`load` 读 **24 字节**，但标量 option 的 payload 仅 8 字节（data 指针），高 16 字节为**未初始化栈**→`str_clone` 越界拷贝→SIGTRAP。`option` 约定 `err(e str)` 的 payload 在标量 option 的 8 字节槽内**无法承载 str 的 len/cap**，故改为**标量 option 跳过 str-peel**（不产生 OOB 读），行为对当前执行路径**无变化**；该路径为 latent（corpus 暂无测试运行期触发），故未改变任何测试分类。

> **第二十轮更新（2026-09-13 续，match/if 作表达式 正确实现 + 闭环 8 测试 + 消除 3 个 red-line trap）**：继承第十八/十九轮线索，**重做 match/if 作表达式捕获并成功闭环**。核心修正吸收了第十九轮两次失败教训：
> 1. **可变 slot + 每 arm `EmitMoveInto`（取代第十九轮的常量槽+首 arm 守卫）**：`r = subject: { arms }` 经 `lowerStmt` KLet 检测 `child.Kind==KIf && name!=""` → 置 `exprCapture=true`、`exprSink=NoVal`、调 `lowerIf`；每个 arm 经 `lowerBlock`→`lowerStmt` KExprStmt（裸值语句 `l.stmtVal = l.lowerExpr(child)`）→`lowerIf` 的 then/else 后 `captureArmValue(armVal)` 把值 `EmitMoveInto(exprSink)`（str 值先 `OpClone` 防双释放）；`r` 绑定 `exprSink`（可变 slot）。`if` 作表达式（`r = if cond {a} else {b}`）同路。嵌套 match 守卫（`ok(it>127) ->`）自身是 if 链，`exprCapture` 仍活跃→其 arm 收敛进同一 slot。
> 2. **去第十九轮 `undef` 守卫（修回退回归）**：不再在 KIdent 变体块短路发零常量，故 `ok`/`nil`/`some`/`err` 同名**变量**正常查 locals→`bug13`/`test-bare-match`/`test-ok-shadow` 不回归（三者本就 MATCH，本轮仍 MATCH）。
> 3. **去 `KExprStmt` 双 lower（修重复输出 bug）**：旧 `KExprStmt` 在「非 KIf/KFor 分支」落空穿透到尾部 `for` 循环把同一 child **再 `lowerExpr` 一次**→每条语句执行两遍（test-bare-match 输出 `1\ndone`×2）。改为每分支 `return`，裸值语句 `l.stmtVal = l.lowerExpr(child)` 后直接 `return`。
> 4. **KIdent 变体块 red-line 修复（闭环 3 个新 trap）**：`match 42: { err -> }` 类**非 option subject** 比 `err`/`nil` 变体臂，旧路径变体块对非 option 上下文「不短路也不发零」→落入 unresolved identifier 发 `undef`→`subject == undef` 是 LLVM poison→opt 折成 `trace/BPT trap`（red-line 崩溃，第十九轮已现于 test-simple-match）。**正确修法**：变体块**入口先查 `l.locals[name]`/`l.globals[name]`**（命中即 `return` 变量，不当变体——正是第十九轮回归的根因），仅当非变量时，option 上下文 `EmitOptionWrap`、非 option 上下文发 `typeHint` 类型 concrete-zero（比较恒 false→fall through，无 poison、无 trap）。`test-match-2`/`test-simple-match`/`test-simple-match2` 由 trap→**DIVERGE**（rc=0 安全；legacy 打 `'err'` 是 quirk，MIR 不复刻，输出空行）。
> **全量重扫（`./bin/no`，421 文件，8-worker）权威实测：MATCH=279 DIVERGE=43 MIR_GAP=1 LEGACY_FAIL=97 HANG=1**（对比第十九轮 271/51/1/97/1：**DIRVERGE 51→43（净 −8，零回归）、MIR_GAP 稳 1**）。闭环的 8 个 match/if 作表达式测试：`tests/test-if-str.no`、`tests/test-match-basic.no`、`tests/test-minimal-it.no`、`tests/test-minimal-it2.no`、`tests/test-minimal-ok.no`、`tests/test-opt-match-all.no`、`tests/test-opt-match2.no`、`tests/test-opt-match3.no`（均逐字节 MATCH）；另 3 个非 option-subject quirk 测试由 trap→DIVERGE（不再崩溃）。**本轮零新增 MIR_GAP、零回退**（`bug13`/`test-bare-match`/`test-ok-shadow` 均仍 MATCH）。剩余 DIVERGE=43 主体为 crypto(sha/x25519)/char/float 打印/option 等 REAL_GAP 与 NONDET（legacy flaky），下轮续分。
> **全量重扫（`./bin/no`，421 文件）**：DIRVERGE 集合与 18 轮**逐字节相同**（comm 双向为空）→ 精确回到 18 轮基线（稳定口径 **MATCH=271 DIVERGE=51 MIR_GAP=1 LEGACY_FAIL=98 HANG=1**）。本 19 轮 sweep 显示 `MIR_GAP=0` 仅为 `tests/test-diff-debug.no` 的 legacy 本次 flake 至 rc≠0、被重分类进 LEGACY_FAIL 的**假象**（其 `MIR=3` 始终 rc=1：字符串插值不支持；3 次手动复测均为 `legacy_rc=0 mir_rc=1`，稳定 MIR_GAP 仍是 1）——非真闭环。**结论**：match 作表达式的正确修复需独立的 phi/结果槽捕获 feature（且须区分多 arm 收敛），建议作为后续独立任务，不在 parity 收官期与 260 个 MATCH 同改同担风险。

> **第二十三轮更新（2026-09-14，全局重赋值修复 + `?f64` 闭环 + 基线零回归裁决）**：全量重扫（`./bin/no`，421 文件，8-worker，25s/测）权威实测 **MATCH=283 DIVERGE=33 MIR_GAP=0 LEGACY_FAIL=96 HANG=9**（对比第二十轮 279/43/1/97/1：MATCH +4、DIVERGE −10、MIR_GAP −1、LEGACY_FAIL −1、HANG +8）。本轮两处修复：
> 1. **全局重赋值 bug 闭环 `test-ifelse2`**（`hir2mir.go`）：顶层 `let` 双重角色注册污染。注册循环仅首次同名 `let` 注册模块级 `@global`（后续重赋值 `x=10` 不覆盖初始化器）；`synthesizeMainForTopLevel` 仅当本 `let` 恰是已注册模块常量声明才 `continue` 跳过内联，同名后续重赋值落到内联分支发运行时 store。`test-ifelse2`（`x i64=5` + `x=10` + block 判 `x==5`）由 DIVERGE→MATCH（`five`/`not five`）。
> 2. **`?f64` 闭环 `test-f64-option`**（`codegen.go` 两处）：(a) `optionPayloadLLVMType` 原 `case "double":` 无 `f64` → `?f64` 降为 flat `%option`（i64 载荷）截断 `3.14`→`3`；改 `case "double","f64": return "double"` → `?f64`→`%option_f64={i64,double}`（自动按值存 double，`@print_option_f64` 经 `emitOptionPrintHelper` 已支持 double 载荷→调 `@print_double`）。(b) `@print_double` 旧实现硬编码 6 位小数 + 把带符号 `ipart`/`fi` 喂 `@digits`（期望绝对值）→ 负数 garbage；重写为手写 `%g` 风格（绝对值喂 `@digits`、符号单独写 `-`、小数部 `fpart*1e6` fptosi 后 6 位 unrolled 打印并裁 trailing zeros，复刻 legacy `%g`：`3.14`/`-2.5`/`0`/`1500`/`1500.0`→`1500`）。
> **关键陷阱（snprintf mis-lowering）**：曾试 `declare i32 @snprintf(i8*,i64,i8*,...)` + `snprintf("%g",v)`，但 variadic 调用被 opaque-pointer 重写/`opt -O3` **mis-lower**——double 实参到达 snprintf 时已被破坏，输出 `%g` 格式化垃圾（`5.3e-315`/`4.08e+179` 等）。改**手写 `@print_double`**（仅用已验证 `@digits`+浮点 op）。手写 IR 块里 `%d5 = %q5` 是非法指令（值别名值），直接用 `%q5` 即可（首个版本因此 opt-verify 失败报 `expected instruction opcode`）。
> **MIR_GAP 1→0 的真相**：`test-diff-debug` 在第二十三轮 sweep 中因 legacy 本次 flake（rc0≠0）被重分类进 LEGACY_FAIL，致 MIR_GAP 显示 0——但 `MIR=3` 始终 rc=1（字符串插值不支持），**稳定 MIR 专属缺口仍为 1（interp 族），非真闭环**；`test_fs_error_complete` 由 MIR_GAP(崩溃 rc3=1)→**DIVERGE（rc3=0，MIR 输出比 legacy 更正确）**：legacy `fs.open('/tmp')` 误报 `ok` 打开目录、写模式不报 `File exists`，MIR=3 正确返回 `Is a directory`/`File exists`。按用户铁律「原实现不一定正确」——此处 **MIR 正确、legacy 错误，计入 DIVERGE 而非对齐 MIR 到 legacy**。
> **⚠️ HANG 1→9 已复跑确认（round-25）非 MIR 回归**：round-24 的 9 个 HANG（`test-for2`/`test-hmac`/`test-hmac2`/`test-http-rest`/`test-net-http`/`test-sha256-min`/`test-shortname-import`/`test-str`/`test-vec-assign`）经逐测 + round-25 复跑确认系 **8-worker 争用下 CPU 饥饿超时假象**——`test-hmac`/`test-sha256-min`/`test-str`/`test-vec-assign`/`test-shortname-import` 等均在 `MIR=0` 与 `MIR=3` **两模式同时超时**（nolang SHA256/HMAC 源码在争用下 >25s），与 MIR 后端无关；round-25（争用较低）HANG 回落至 **1**（仅 `test-for2`，与 第二十轮一致）。**结论：HANG 跳变零 MIR 回归，稳定 HANG≈1**。MIR_GAP 在 round-25 回落显示 **1**（`test-diff-debug` 字符串插值不支持，legacy 本次未 flake 故未被掩），证 round-24 的 `MIR_GAP=0` 系 legacy flake 掩的假象——**稳定 MIR 专属缺口仍为 1（interp 族）**。DIRVERGE/MATCH 在两轮间 4 个边界测试浮动（33↔37 / 283↔285），属 legacy 侧 flaky/非确定输出，已记入 NONDET。
> **基线零回归裁决（实证）**：曾误判 5 个「回归」（test-hmac/test-hmac2/test-sha256/test-shortname-import/test_fs_error_complete），因脏 baseline 比对。用 `git stash` 取 committed HEAD 重建干净 baseline（`/tmp/no_stash` 与 `/tmp/no_pre` 字节同尺寸 16412498，证实 `/tmp/no_pre` 即干净 round-22），直跑：4 个 crypto 测试**仍 DIVERGE**（与 current 一致）→ 系 MIR 既有 sha256/hmac 真 bug，非本改动回归。结论：**当前未提交改动零真实回归**，净效果 MATCH 240→283、DIRVERGE ~43→33、MIR_GAP 由稳定 1（test-diff-debug）经 round-24 分类 0（flake 掩）。

> **A 家族根因与第十三轮修复（部分）**：`fs.open(p, { mode: 0 })` 的选项字面量在 parser 中 `StructLiteral.Type` 为**空串**（`parseStructLit` 注释："由 codegen 推斷"，即由上下文推断），legacy 在 codegen 期从**调用点**解析该上下文，而 MIR 在 codegen 期没有调用点信息 → `lowerStructLit` 发射**无类型** `structlit` → `emitSetField` 的 `FieldIndex` 失败（`setfield field`）。**修复（`hir2mir.go`）**：`lowerCallArgs` 现从被调函数的**声明形参类型**为匿名 `{...}` 实参播种类型（`structLitTypes` + `paramRawTypesOfCallee` + `noteAnonymousStructLit`，并经 `canonStructRaw` 规范化为 `StructFields` 键，保证与 codegen 的 LLVM 结构体类型命名一致）。**效果**：`test-open-perm/read/write`、`test_fs_error_complete` 越过 `setfield field`（4 个测试由**编译错误**推进到**更深层**错误），`test_fs_error_final/info/simple`、`test_fs_open_err` 进入运行时后触发 trace/BPT trap。**未收官**：A 家族下游仍有独立缺陷（见 C/D/E/F 家族），故第十三轮**未净减 gap 数**（`MIR_GAP` 仍 21；`test-diff-debug` 的"新增"系 **legacy 自身 flaky**：其 `MIR=0` rc 在 1/0 间抖动，与本修复无关）。

> **§13.3.1 的历史计数说明**：该表所列 41/39/36/23 等均为**历史快照**；第十三轮复核后，其中 §13.3.2 曾记的"`opt_verify` 剩 3"（`test-str-ops`/`test-std-unix-fs-os`/`move-eligibility-improved`）确已随第九轮 #32/#33/#34 闭环（`test-str-ops` 第十三轮实测逐字节一致）。但**这不等于"MIR 专属 gap = 0"**——另有上表家族共 **21 个真实缺口**（第十三轮实测；第十四轮闭环 C 家族 3 个 + E 家族 2 个后为 **17**），多为早期按症状分类时被并入"预存失败"、未做 fresh legacy-parity 的项。

#### 13.3.2 各家族根因与修复方向
> 注意：本节原按错误症状分类（首轮）。第五轮 legacy-parity 二分（§13.3.5）显示 `setfield field`/`unknown callee`/`lencap kind` 等"症状家族"绝大多数是**两模式都挂**的预存失败（已计入 §13.3.5 的 92），并非 MIR 专属缺口；下文仅保留经 parity 确认的 MIR 专属修复方向（共 41，见 §13.3.1）。

- **字符串插值（15，MIR 专属）—— 第八轮已闭环**：`'x={expr}'` 在 HIR `KStrLit` 仅保留原始文本，嵌入表达式在 parser 阶段已丢失；MIR 无法 re-parse。关键事实：parser 已有 `parser.ParseFormatString`（`src/parser/format_spec.go`），legacy 在 **codegen 期**对 `sl.Value` 原文调用它，经 `callNamedFormat`→`dispatchFmtCall` 路由到 `@fmt-int`/`@fmt-str`/`@fmt-bool`/`@fmt-f64`/`@fmt-uint`。**实测确认的关键语义**：legacy 的拦截点 `shouldInterceptNamedFormat` 只由 `callFmt`（print/eprint/printf/eprintf/sprintf/format）调用，所以**只有 print 家族做替换**；`s = 'x={n}'` 之类的其他上下文原样输出花括号（实测：legacy 输出 `x={n}`）。第八轮据此实现（§12 #28）：判据精确化 + 在 `lowerCall` 拦截按段发射；无 spec、带 spec、表达式下标 `{h[i]:02x}`、`format()` 返回 str 全部支持，输出与 legacy 逐字节一致（`test-named-format` 含 `{{`/`}}` 转义与 20 余种 spec 全部一致）。副作用：6 个原"两模式都挂"的测试（`test-guard`/`test-quant-all`/`test-quant-all1`/`test-re-debug`/`test-regexp`/`test-regexp1`）现在 **MIR 通过、legacy 失败**——legacy 把 JSON/正则里的 `{...}` 误当插值而崩。

- **`opt_verify`（第十轮实测 4 个 MIR 专属独立深坑 → 第十三轮复核**已全部闭环，现剩 **0**）**：§12 #15/#18/#19 闭环算术/负号路径；#23 闭环 `err/ok/some` 构造器 CALL；**第十二轮 #39 闭环 `test-async.no`**（`add void undef`——async 结果 void 操作数，经完整 task 运行时 + `OpRun`/`OpAwait` 直发消除）；**第九轮 #32/#33/#34 实际已闭环余下 3 个**——本行"剩 3"系文档滞后（原第 (b)(c)(d) 三项均已随第九轮落地）：(b)`test-str-ops.no`（#33，`str - char` 拼接语义对齐 legacy）**第十三轮实测 `MIR=3` 与 legacy 逐字节一致**；(c)`test-std-unix-fs-os.no`（#32，裸结构体名解析为限定名）**第十三轮实测两模式均 rc=0，输出仅剩 §16 所述 bool 打印形态差异（DIVERGE，非 gap）**；(d)`move-eligibility-improved.no`（#34，`[a.len()]` 元素型 `[1]void`；实际路径 `tests/mem-safety/move-eligibility-improved.no`，文档早前写作 `tests/move-eligibility-improved.no` 系路径笔误）。**结论**：opt_verify 家族 MIR 专属缺口已清零，无遗留 sink 站待修。
- **`builtin` option/receiver（2 剩余，MIR 专属）**：`str-len: missing receiver`（async）、`number.f64-to-f32: cannot coerce %option`；`str-len-bytes receiver %txt` 已由 #20 修。修复方向：对齐 legacy 的 receiver 路由 + option 实参解包。
- **`builtin` 未实现（0，已由 §12 #37/#38 闭环，FFI/net 族）**：`net-dial`/`net.net-icmp-open`/`net-send`/`net-recv` 现已通过内联发射 libc `socket`/`connect`/`inet_pton`/`send`/`recv` 实现（`fs.read-file` 已由 #35 闭环）。此前 `test-net-client.no`/`tmp-icmp-test.no` 因缺这些内置在 `MIR=3` 构建失败，现均构建通过（`tmp-icmp-test` 在 macOS 非特权 `SOCK_DGRAM` ICMP 下还能实际运行、输出与 legacy 一致）。
- **`index dst slot`（0，已闭环）**：`arr-slice.no` 索引结果类型推导为 void→无 slot，与 `with-len` 同类类型推导缺口。已由 §12 #21（recv 值类型回推切片结果型） + #22（切片 `(`/`)` 边界语义）闭环，输出与 legacy 逐字节一致。
- **MIR 专属运行时崩溃（red-line，第七轮大幅闭环）**：clone/move/struct-field 系列内存所有权 bug（legacy 通过、MIR 崩）。第七轮按根因拆为三族并闭环：(a) **定宽数组→slice sink 未归约 `%vec`**（`emitSetField`/`emitMove`，§12 #24/#25）——`store [N x T]` 直塞 `%vec` slot 致 len/data 读到数组元素；闭环 `struct-field-move-test`/`struct-field-uaf-bug`/`struct-move-is-moved`/`struct-field-leak`/`double-move-same-source`/`clone-reset-is-moved`/`move-clone-liveness`/`prologue-buf-leak`（均输出与 legacy 逐字节一致）。(b) **`@main` out-param 漏传**（`emitEntry`，§12 #26）——闭环 `cross-fn-str-return-dfree`。(c) **结构体字面量字段初始化器所有权**（`MovesArg`，§12 #27）——闭环 `struct-field-leak`/`struct-move-is-moved` 的 NUL-name。**崩溃计数 10 → 0**（red-line 达标）。剩 `struct-field-shallow-copy-bug` 1 个为**语义分歧非崩溃**（legacy 对 `out = c.data` 做 vec 深克隆使 `d` 独立；MIR 浅拷贝共享 data → `d[0]=999` 影响 `c.data[0]`；需 `%vec` 深克隆 helper，暂缺）。
- **`setfield field`/`unknown callee`/`lencap kind`（症状家族，多为预存失败）**：第五轮 parity 二分显示这些多属两模式都挂（§13.3.5 的 92）；原"std 结构体字段未进 StructFields"等根因分析仍成立，但其对应 test 大多 legacy 也失败，需先修 std/test 再判定是否 MIR 责任。

#### 13.3.3 本轮（第三/四轮）增量
- 第三轮：修复 **§12 #15**（算术/负号 option 包回）+ **§12 #16**（数组 receiver 路由）。MIR 专属通过数（非递归口径）：273 → ≈287（净增 ≈14；其中 #15 解锁 13 个 x25519/fe-mul 系列 + #16 解锁 `test_zero.no`）。
- 第四轮（2026-09-11）：闭环 **§13.3.4**（#17 顶层容器字面量物化）+ **#18**（`emitBitwise` option 解包包回）+ **#19**（`print` 内联 option payload 路由）。全量递归口径：committed 基线 **244** → **280**（#17+#18，净 +36）→ **281**（#19，净 +1；crypto 假 HANG 不计，见 §10.1）。**零 MIR 专属回归**（14 个既有通过测试 + 全量重扫守护）。
- 口径提示：第三轮 "≈287" 为**非递归** `tests/*.no`（≈368）测量；第四轮 "281" 为**全量递归** `tests/**/*.no`（418）测量，总数不可直接相减。
- 第五轮（2026-09-11）：**legacy-parity 二分**重塑失败分类 + **#20**（`%txt` receiver 的 `str-len-bytes`）。(a) 对 418 全量每个失败测试额外跑 `NOLANG_MIR=0`：失败 137 个里仅 **41 个 MIR 专属**（legacy 过、MIR=3 挂），**92 个两模式都挂**（legacy 也失败，属通用预存 bug / std·test 缺陷，见 §13.3.5）。据此修正 §10.1/§13.3.1 的真实 MIR 专属 gap = **41**（非早期按症状估的 38）。(b) 闭环 #20 → `test-slot-rebind.no` 从失败转 PASS（全量递归口径 **281 → 282**）；该 test 打印指针类值输出非确定，属 §13.3.5 的 test 自身缺陷，但 codegen 门禁已解除。(c) 印证用户"std/tests 都可能不合理"警示：**绝大多数长尾失败是预存/通用 bug，不 blocking MIR 门禁**，应作为通用 bug 双路径闭环（§13.3.5）。
- 第七轮（2026-09-12）：**mem-safety 崩溃族（red-line）大幅闭环** —— §12 #24（定宽数组→slice 字段 `%vec` 归约：借用/堆拥有两种）、#25（定宽数组→`%vec` 变量 move 归约）、#26（`@main` out-param 接线）、#27（结构体字面量字段初始化器所有权转移 `MovesArg`）。**MIR 专属运行时崩溃 10 → 0**（red-line 达标）：闭环 `struct-field-move-test`/`struct-field-uaf-bug`/`struct-move-is-moved`/`struct-field-leak`/`double-move-same-source`/`clone-reset-is-moved`/`move-clone-liveness`/`prologue-buf-leak`/`cross-fn-str-return-dfree`（**全部输出与 legacy 逐字节一致**）。仅剩 `struct-field-shallow-copy-bug` 为语义分歧（需 `%vec` 深克隆），已非崩溃。改动触及 `emitSetField`/`emitMove`/`emitEntry`/`insertDrops`/`Validate`/`lowerStructLit`，以全量递归重扫守护零回归。

#### 13.3.4 主导深层 blocker：顶层 main 合成结构性 bug（**已闭环 #17，2026-09-11 第四轮**）
**现象**：顶层 `block []u32 = [...]`、`v []str`（无初始化）等顶层容器字面量/变量，在 `synthesizeMainForTopLevel` 合成的 `main` 中未被物化为真实 slice/array，而是打成 `const dst=:void`（或 `fs.file` 垃圾 structlit），致其后的索引/方法调用操作数 void 化 → `has_no_slot`/`index_slot`/`receiver-切片` 共 **15 个**测试失败。**这是单点修复即可解锁最多测试的深层 bug**：正确物化顶层容器字面量/变量进合成 main（对齐 legacy 的顶层语句 lowering），15 个测试应一举通过。
**状态（已闭环）**：`#17` 修复后 15 个家族全归零；committed 基线 244 → 280 PASS（净 +36，含 #18 `emitBitwise` 伴生修复）；全量递归重扫 +14 个既有通过测试零回归。风险：改动 `synthesizeMainForTopLevel` 影响所有顶层语句程序，已用全量递归重扫守护。

#### 13.3.5 预存失败与 std/test 缺陷警示（2026-09-11 第五轮）
> 现场警示：**当前项目开发中，`std`、tests 都可能不合理/不正确**，识别失败时要先怀疑 std/test 而非 MIR 后端。

第五轮对 418 全量做了 **legacy-parity 二分**（`NOLANG_MIR=0` vs `=3`），得出关键二分：

| 二分 | 数量 | 含义 |
|---|---|---|
| **MIR 专属 gap**（legacy 过、MIR=3 挂） | **41** | 真正的 MIR codegen 缺口（§13.3.1） |
| 预存失败（两模式都挂） | **92** | legacy 也失败，**非 MIR 引入** |

**预存失败（92）的典型根因**（均不 blocking MIR 门禁，应作为通用 bug 在双路径闭环）：
- **std 源码缺陷**：`bug15-read-dowhile-copyfile.no` → `std/process.no:line 637` 含非法 token `®`（std 源损坏）。
- **溢出默认 checker 硬错**（无 `#{overflow=wrap}`/`?=`）：`match.no`/`test-all.no`/`test-database-sql.no`/`test-ffi-mysql.no`/`test-ffi-sqlite.no`/`test-sha256-simplified.no`/`test-sse.no`/`test-str-concat.no`/`test-tagged-enum.no`/`test_char_*.no`/`tmp-arr-test.no`/`tmp-words-test.no` 等约 17 个——两模式都因 `ValidateUnhandledOverflow` 拒，与 MIR 无关；部分属 test 漏注解（test 自身缺陷）。
- **std 内建经 MIR 路径触发的"no result slot"**：`test-basic.no`（`bigint.gcd` 内部 `with-len`）、`test-map.no`/`test-parse-min.no`/`test-minimal-option-str.no` 等——这些 test 在 `MIR=0` 同样 rc=1，属 std 函数/`with-len` 类型推导缺口经 MIR 路径更早暴露，**非 MIR 引入的回归**。
- **非确定/指针输出 test**：`test-slot-rebind.no` 等打印指针类值，无法逐字节对照（test 自身缺陷，§12 #20 已解除其 codegen 门禁）。
- **std 缺陷引发的 `link`/crash**：`test-x25519-keypair-diff.no`（链接缺符号）、`option.no`/`test-option-index.no` 等（两模式都挂）。

**结论**：推进 MIR 覆盖率时，先把 92 个预存失败从"MIR gap"清单剔除，聚焦 41 个真 MIR 专属缺口；对其中疑似 std/test 缺陷的，先核对 `MIR=0` 是否通过再判定是否 MIR 责任。

---

## 14. 溢出默认（overflow-default）与 MIR 的集成（进行中）

`#{overflow}` 默认使整数 `+ - * /` 返回 `option<int>`（不 panic）。当前状态：
- checker 侧：`ValidateUnhandledOverflow` 对未注解/未 `?=` 的算术报错（硬错阻断构建）；std 豁免已撤除，131 处已用 `#{overflow=wrap}` 修。
- **MIR 侧（中段→表达式边收尾）**：HIR 算术节点已被打成 `?i64`（option），`OpOptionWrap` 构造 `{tag,payload}``。sink 站点 `emitIndexStore`+`emitSetField` 的解包（§13.1）+ **算术/负号结果包回**（§12 #15）已落地，`test-arr.no`、x25519 系列、fe-mul 等 opt-verify 失配已闭环。

**下一步（闭环 §13.1 与溢出集成）**：`opt_verify` 家族算术/负号路径（#15）、`emitBitwise`（#18）、`print` payload 路由（#19）已闭环；`err/ok/some` 构造器 CALL（#23）已闭环。**剩余 opt_verify（§13.3.1，4 个，实测为独立深坑）**在**非算术的表达式路径**（字符串算术运算符重载推断 / async void 操作数 / struct-field-on-option GEP / void 数组 alloca），需在对应表达式 emit 处加同法 `extractvalue`/`insertvalue`，修复模式已统一（`unwrapOptionOperand` + §12 #15 的包回）。§13.3.4 顶层 main 合成（#17）已闭环。溢出默认 integration 的 MIR 侧算术环节现已基本闭环，剩余是表达式边的零散补全。

### NOLANG_MIR_DUMP_* 调试开关
- `NOLANG_MIR_DUMP_MIR=1`：打印 lowered MIR 到 stderr。
- `NOLANG_MIR_DUMP_LL=1`：写出 emitted LLVM IR 到临时 `.ll`（路径在 stderr）。
- `NOLANG_MIR_DUMP_HIR=1` / `NOLANG_MIR_DEBUG=1` / `NOLANG_MIR_DUMP_BAD=1`：辅助诊断。

---

## 15. 分阶段路线图（修正为真实进度）

- **Stage 0（已完成）**：`src/mir` 核心（模型/Builder/CFG/分析/Liveness/所有权/Drop/Move·Borrow/printer/validator/HIR→MIR lowering）+ `NOLANG_MIR=1` 验证模式 + 单元测试。MIR 作为审计器运行，现有构建零回归。
- **Stage 1（已跳过）**：`MIR→HIR` 桥未实现（直发路径更优）。
- **Stage 2（已完成）**：`MIR→LLVM` 直接发射（`EmitLLVM`），strangler-fig 回退（`NOLANG_MIR=2`）。
- **Stage 3（进行中 · 当前前沿）**：`NOLANG_MIR=3` 全量 corpus 门禁。**第十四轮权威全量重扫（2026-09-12，`./bin/no`，421 文件）**：`MATCH=248`、`DIVERGE=58`、**`MIR 专属 gap=17`**、`LEGACY_FAIL=97`、`HANG=1`——**red-line 已达标（MIR 专属运行时崩溃 = 0）**，gap 由第十三轮 21 降至 17。第十三轮实测的 3 个 MIR 专属运行时崩溃（`async-shared-race`/`test-slot-rebind-unsafe` SIGSEGV、`test-embed` trace-BPT）已由 §12 #41 全部闭环（同时连带闭环 E 家族 `async-str-result`/`async-str-stress`）。累计闭环：§13.1 `emitIndexStore`/`emitSetField` option 解包、`str-clear` 内置、§12 #15 算术/负号 option 包回（解锁 13 个）、#16 数组 receiver 路由（解锁 `test_zero.no`）、**#17 顶层容器字面量物化（§13.3.4，解锁 15 个 has_no_slot/index_slot/receiver）**、#18 `emitBitwise` option 解包包回、#19 `print` 内联 option payload 路由（解锁 3 个 opt_verify）、**#20 `%txt` str-len-bytes receiver**、**#21/#22 arr-slice 切片（元素型回推+边界语义）**、**#23 `err/ok/some` 构造器 CALL 路由**、**#24–#27 mem-safety 崩溃族（定宽数组→slice 归约 / `@main` out-param / 结构体字面量字段所有权）（解锁 9 个，red-line 达标）**、**#28 字符串插值（解锁 15→1）**、**#29–#35 切片越界/窄整型/类型推导/内建补齐（解锁 20+）**、**#36 顶层未初始化全局物化（闭环 `test-x25519-keypair-diff`，+1）**、**#37 `net.net-icmp-open` raw socket FFI（闭环 `tmp-icmp-test`）**、**#38 `net-dial`/`net-send`/`net-recv` FFI 内置（闭环 `test-net-client` 构建）**、**#39 `await`/`run` coroutine 完整 task 运行时（闭环最后一个 MIR 专属 gap `test-async.no`，MIR 输出比 legacy 正确）**、**#40 async 取消/让出内置 `async-cancel`/`async-cancelled`/`async-yield`（收尾完整运行时契约；3 个新测试 MIR=3 全过，其中 `tmp-async-yield` legacy SIGSEGV 而 MIR 正确）**、**#41 第十四轮 C 家族 red-line 3 崩溃全闭环**（`run` 语法化异步 / 异步实参按形参类型强制 / 任务结果类型跟踪 / `#{embed}` 物化；连带闭环 E 家族 2 个 → gap 21→17、MATCH 240→248、**MIR 专属运行时崩溃 = 0**）。MIR 专属 gap 由首轮 69 经多轮降至 **17**（第十四轮实测）。下一步优先级：A 家族（4：fs-open 下游）→ B/D（7：`unknown callee`/`opt-verify`）→ E/F（2）→ `test-diff-debug`（interp）。
- **Stage 4（下一步）**：溢出默认解包全闭环后，逐站消除 §13.3 长尾；FFI/async/crypto/net/map/字符串方法内置补齐；最终让 `no build` 默认走 MIR=3（移除 legacy 散布 `emitHeapFree`）。

---

## 16. 开放问题与风险（更新）

- **生成顺序/平台过滤**：HIR `#{platform}` 注解需在 lower 前过滤（复用 `AnnotationsOf`）。
- **跨模块 owner 判定**：与 `globalVarOwner`/`funcOwner` 对齐（`transpiler.go`）。
- **推断类型来源**：`pkg.Inferred[id]` 是分类 ownership 的权威输入；缺失时回退声明类型 `Node.Type` 并标记诊断。
- **回归红线**：任何 MIR 失败在 `NOLANG_MIR=2` 必须回退现有路径；全量 `no build` 扫掠在 sandbox 受限（约 360 测试会被 SIGKILL），用定向子集 + `opt -passes=verify` 快速回路验证（`scripts/mir_cov.py` / `mir_sweep.py`）。
- **bool 打印：legacy 自身不一致，暂不强行对齐（2026-09-12 实测）**。legacy 的 bool 输出**依赖表达式形态**而非值：命名变量 `print(b)` / 结构体字段 `print(p.vis)` → `true`/`false`；比较表达式 `print(n > 3)` / vec 元素 `print(v[0])` → `1`/`0`；`(n>3).to-str()` → `1` 而命名变量 `.to-str()` → `true`。MIR 全站统一输出 `1`/`0`（`print_bool`）。因此在顶层命名 bool 场景 MIR 与 legacy 分歧（如 `tests/test-std-unix-fs-os.no` 的 `utime ok = true` vs `1`），但这类测试 rc 仍为 0。**判定**：属 legacy 历史不一致（且 legacy 在函数体内 `print(局部 bool)` 会直接 opt 失败：`'%b.val' defined with type 'i64' but expected 'i1'`），不是干净的 MIR 缺陷；强行翻转会在另一半场景引入新的分歧，故维持现状并记录。**扫掠超时口径**：crypto 系列（sha256/hmac/tls）单测编译+运行需 10–14s，扫掠脚本超时必须 ≥60s，否则（尤其在并发 `go build` 抢 CPU 时）会被误判为 HANG。
- **溢出默认集成中段风险（已消解）**：§13.1/§14 的解包缺失曾令 `test-arr.no` 退化（已闭环，`emitIndexStore`）；续闭环 `emitSetField`（§12 #14），sink 站点解包已覆盖 `emitIndexStore`+`emitSetField`。**原"剩余 `opt-verify` 家族（§13.3.1，23 个）在非 sink 表达式路径未解包"的判断已过时**：该家族经 #15/#18/#19/#23/#32/#33/#34/#39 逐站闭环，第十三轮复核 **MIR 专属剩 0**。`test-arr.no` 已于第十二轮实测 `MIR=3` 与 legacy 逐字节一致。**精确 MIR=3 通过数**：第十三轮以**修正二进制路径**（`./bin/no`，非陈旧的仓库根 `no`）对全量 `tests/**/*.no` 重跑，结果见 §10.1。
- **legacy `awy f` future 变量漏行 bug（2026-09-12 第十二轮实测，MIR 正确、legacy 错）**：`tests/test-async.no` 的 `test-await-future` 子用例写 `f = compute-async(25); r = awy f; print(r)`。MIR=3 正确输出 8 行（`42/42/1/-1/0/60/60/50`），但 legacy（`MIR=0`）只输出 7 行、**漏最后一行 `50`**——legacy 对 `awy <future 变量>`（future 已先 `run` 进变量、再 await 该变量）的调度路径存在既有 bug，未把该 future 的结果打印出来。故 `test-async` 在 §10.1 覆盖表中**不计入 MATCH**（要求逐字节一致），而归入 "MIR 正确 / legacy 错误" 族（同 #28 插值、#36 x25519）。这是 legacy 自身缺陷，非 MIR 回归；MIR 因忠实移植 legacy `build/llvm` 协作式调度器契约（§12 #39）而绕过了该 bug。
- **legacy `async-yield()` 在 `-async` 函数内 SIGSEGV（2026-09-12 第十二·续轮实测，MIR 正确、legacy 崩）**：`tests/tmp-async-yield.no` 的 `-async` 目标 `yielder-async` 体内首条语句是 `async-yield()`，随后 `r = n + 1`，由 `run`/`awy` 驱动。MIR=3 输出 `6/9` **正确**；legacy（`MIR=0`）**SIGSEGV**（最小复现 `/tmp/min-yield.no` 亦崩）——legacy 的 `coro.go` 把顶层 `async-yield()` 改写为协程挂起点后，在 `run`+`awy` 驱动的 `-async` 函数上挂起/恢复路径崩。MIR 因无协程状态变换、恒走退化路径（`call @nolang_async_yield`）而正确。又一 "MIR 正确 / legacy 错误" 案例，非 MIR 回归。注意：**函数名以 `-async` 结尾会被 MIR/legacy 都当作异步调用**（`strings.HasSuffix(callee, "-async")` → `OpRun`），故测试函数名应避免该后缀（本测试初版误名 `test-yield-in-async` 被当作 `run` 而未被 await，已改名 `test-yield-inside`）。

44. **字符串插值方法调用字段（2026-09-14 第二十四轮，闭环唯一稳定 MIR_GAP `test-diff-debug`）**：`lookupFormatValue`（`hir2mir.go`）此前仅支持 `ident` 和 `ident[index]` 两种格式字段模式，对 `ident.method()`（如 `eprint('SPLIT: entered, content.len={content.len-bytes()}')` 中的 `{content.len-bytes()}`）无支持 → 报 `unsupported construct (string interpolation)` 致命诊断 → 整模块回退。`test-diff-debug.no` 是**唯一稳定的 MIR 专属 gap**（MIR=3 rc=1 构建/运行失败，legacy flaky rc 在 0/1 间抖动）。修复：新增 `fmtMethodFieldRe` 正则匹配 `ident.method()` 模式，在 `lookupFormatValue` 中优先查 builtin 表（`sliceMethodBuiltin` + `builtin.FindBuiltinMethod`），按 receiver 类型+方法名路由到 ForwardFunc 内置（如 `str-len-bytes`），非内置时走用户方法调用路径（`canonSliceRecv` + `enqueueCallee`）。验证：`test-diff-debug.no` 在 `MIR=3` 下不再报 `unsupported construct`，输出与 legacy 逐字节一致（两模式均 rc=1 属测试自身 DP 算法 bug，非 codegen 问题）。

45. **数组字面量元素类型推导修复（2026-09-14 第二十四轮，闭环 SHA1/HMAC 静默错误族）**：`lowerArrayElems`（`hir2mir.go`）此前用 `typeOfNode(首元素)` 推导数组字面量元素类型；整数常量 `0x61` 的 HIR 类型为 `i64` → `data []byte = [0x61, 0x62, 0x63]` 生成 `[3]i64`（元素 stride 8 字节）而非 `[3]byte`（stride 1 字节）。当 SHA1 从 `data[i]` 读取字节时，`elemAddr` 按 `i64` 步长计算偏移 → `data[1]` 读到 offset 8（越界）而非 offset 1 → **哈希完全错误但 rc=0**（静默错误）。`tests/test-sha1-minimal.no` 的 MIR 输出 `d1 e2 40...` 而 legacy 输出正确的 `a9 99 3e...`。修复：`lowerArrayElems` 新增优先级——先检查 `l.typeHint` 是否为 slice/array 类型，若是则取其元素类型作为数组字面量的元素类型（使 `data []byte = [0x61,0x62,0x63]` 生成 `[3]byte`）。仅在 `typeHint` 不可用时才回退到首元素类型推导。验证：`test-sha1-minimal.no` 在 `MIR=3` 下输出与 legacy 逐字节一致（`a9 99 3e...`）；`test-hmac.no`/`test-hmac2.no` 的 MIR 输出 `86` **正确**（legacy 输出 `182` 错误——legacy 的 slice 写入 `inner-data[64+i]` 未正确写入 msg 字节，属 "MIR 正确 / legacy 错误" 族）。

46. **字符串 `!=` 比较取反逻辑修复（2026-09-14 第二十八轮，闭环静默错误族 P0）**：`lowerExpr` 的 `KInfix` 分支中，字符串 `!=` 比较的降级（`hir2mir.go` 3196-3202）先用 `OpStrEq` 计算相等结果 `eq`，然后用 `OpNe(eq, false)` 取反。但 `OpNe(eq, false)` = `eq != false` = `eq`——**并没有取反**！因此 `'hello' != 'world'` 返回 `false`（而非 `true`），导致所有使用 `str != str` 的条件分支静默走错分支——程序构建成功、运行不崩溃，但输出**错误**（rc=0 静默错误）。这是最危险的 bug 类别：`test-rand-multiassign.no` 最后一行 `PASS: different seeds produce different priv key bytes` 在 MIR 下不输出（条件分支 `ka-priv != kb-priv` 判为 false → 落入 `FAIL` 分支但 `FAIL` 分支的条件 `ka-priv == kb-priv` 也判为 false → 两个分支都不执行 → 空输出）。修复：把 `OpNe(eq, false)` 改为 `OpNot(eq)`（`xor i1 eq, 1`），正确取反 `@str_eq` 的结果。验证：四种组合（`==`/`!=` × first-arm/second-arm）与 legacy 逐字节一致；`test-rand-multiassign.no` 由 DIVERGE→MATCH（净 +1）；21 个 curated 测试零回归（`test-async`/`test-named-fn-type`/`test-quant-all1` 的 DIVERGE 为已知的 "MIR 正确 / legacy 错误" 族，非本修复引入）。
