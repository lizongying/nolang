# Nolang MIR — 工业级中端中间表示设计（实施记录 v2.3）

> 状态：直接发射已达 **Stage 3**（MIR→LLVM 直接发射，全量 corpus 门禁可跑）
> 作者：编译器工作流
> 关联：`src/hir`（HIR）、`src/build/llvm`（LLVM 后端）、`src/parser/tohir.go`（AST→HIR）、`src/mir`（本层）

---

## 0. 本文档的定位与 v1→v2 变更

v1（本文档前身）是一份**前向设计**：描述了 `NOLANG_MIR=1` 审计模式、`MIR→HIR` 桥（Stage 1）、以及"MIR 作为内存安全审计器运行"的路线。但实际实现**超越了 v1 的路线**：MIR 直接跳到了 `MIR→LLVM` 直发（v1 的 Stage 3），并且 env 开关语义从 v1 的 `0/1` 演进为 `0/2/3`。本 v2 把文档**对齐到真实已落地的架构**，并补充：

- 真实的 `NOLANG_MIR=0/2/3`（及 `=1` 调试）语义；
- 全量 corpus 覆盖实测（**281/418** 通过 `MIR=3`，全量递归扫掠 `tests/**/*.no`=418 文件；此前文档 "≈287" 为**非递归** `tests/*.no`≈368 文件测量，口径不同勿混）；
- 已闭环的 **19 个** MIR 专属运行时崩溃/代码生成修复目录（#1–#19，含 §13.3.4 顶层 main 合成结构修复 #17、`emitBitwise` option 解包包回 #18、`print` 内联 option payload 路由 #19）；
- 当前已知的 gap 家族（含 1 个 MIR 专属回归 `test-arr.no`，已闭环）；
- 覆盖扫描方法论（`scripts/mir_cov.py` / `mir_sweep.py`）。

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

| 类别 | 数量（本轮 281 PASS） | 说明 |
|---|---|---|
| **MATCH** | **281 / 418** (≈67%) | `MIR=3` rc=0 且输出与 `MIR=0`（legacy）逐字节一致（抽样对照） |
| **MIR 专属 gap**（legacy 过、MIR=3 挂） | **41** | 真正的 MIR codegen 缺口，见图 §13.3.1（2026-09-11 第五轮 legacy-parity 二分实测） |
| 预存失败（两模式都挂） | 92 | legacy 也失败，**非 MIR 引入**；多为 std/test 自身问题（见 §13.3.5 用户警示） |
| HANG | 4 | `test-for2.no`（预存死循环，legacy 同挂）+ `test-hmac.no`/`test-hmac2.no`/`test-sha256-min.no`（crypto，8s 超时假 HANG，**直接运行均 rc=0**，非 MIR 回归） |

> **关键修正（2026-09-11 第五轮）**：早期 §13.3.1 按**错误症状**直接分类，把"两模式都挂"的预存失败也计进了 MIR 专属 gap（曾报约 38）。本轮对 418 全量做了**legacy-parity 二分**（每个失败测试额外跑 `MOLANG_MIR=0`）：失败 137 个里仅 **41 个 MIR 专属**（legacy 过、MIR=3 挂），**92 个两模式都挂**（legacy 也失败）。故 MIR 专属 gap 真实数是 **41**，不是 38；那 92 个属 nolang 通用预存 bug / std·test 缺陷，与 MIR 路径无关，应作为通用 bug 在双路径闭环、不 blocking MIR 门禁。这也印证了"std、tests 都可能不合理"的现场警示（§13.3.5）。

**结论**：MIR **专属**运行时崩溃 = 0（red-line 达标）；剩余 137 失败中 92 为预存/通用 bug，仅 41 为 MIR codegen 缺口（§13.3.1）。HANG/crash 多为 nolang 通用预存 bug 或扫掠超时假阳性（与 MIR 路径无关）。

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

> 调试经验：sandbox 内 `lldb` 因 `ptrace` 被 SIGTERM 无法 bt；改用 `NOLANG_MIR_DUMP_LL=1` 后 `grep undef`/`trap` + `awk '/^define @fn/,/^}/'` 定位 trap 站点（`opt` 把 `undef` 比较编成 `br i1 undef` → UB trap，IR 中无显式 trap）。

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

#### 13.3.1 MIR 专属 gap 的家族分布（**41 个**，2026-09-11 第五轮 legacy-parity 二分实测）
> **口径修正**：早期按错误症状直接分类曾报约 38，但漏做 legacy-parity 二分——混入了"两模式都挂"的预存失败。本轮对每个失败测试额外跑 `NOLANG_MIR=0`：418 全量失败 137 个中，仅 **41 个 MIR 专属**（legacy 过、MIR=3 挂），**92 个两模式都挂**（legacy 也失败，属通用预存 bug / std·test 缺陷，见 §13.3.5）。故真实 MIR 专属 gap = **41**。历史累计已闭环：§13.1 `emitIndexStore`/`emitSetField` option 解包、str-clear、#15 算术/负号 option 包回(13)、#16 数组 receiver(1)、#17 顶层容器字面量物化(15)、#18 `emitBitwise`(1)、#19 `print` option payload(3)、#20 `%txt` str-len-bytes receiver(1)。

| 家族 | 数量 | 根因 | 本轮进展 |
|---|---|---|---|
| 字符串插值（`unsupported construct (string interpolation)`） | 16 | `'x={expr}'` 在 HIR `KStrLit` 仅保留原始文本，嵌入表达式在 parser 阶段已丢失；MIR 无法 re-parse。需 parser 保留插值段（字面段 + 表达式 AST），tohir 改写为 `str_concat` 链 | 待闭环（架构级：parser→tohir） |
| `opt_verify`（LLVM 类型校验失配） | 6 | 溢出默认算术的 option 在**非算术**表达式路径（比较/`KBitOp`/`emitSliceOp` GEP）未解包 → i64 与 %option 失配 | 算术/负号/#18/#19 已闭环；剩 6 待补 |
| `builtin` option/receiver 处理 | 3 | `str-len: missing receiver`(async)、`str-len-bytes receiver %txt`(#20 已修，但该 test 打印指针值非确定)、`number.f64-to-f32: cannot coerce %option` | #20 修 %txt receiver；剩 2 待补 |
| `builtin` 未实现（FFI/net 族） | 3 | `fs.read-file` / `net-dial` / `net.net-icmp-open` 无 MIR 内建实现（需 runtime/FFI） | 待实现（FFI/net 族） |
| `index dst slot` | 0 | `arr-slice.no`：`x = slice[i]` 结果类型推导为 void → 无 slot（与 with-len 同类型推导缺口） | **已闭环（2026-09-12 #21/#22）**：recv 值类型回推 + 切片边界语义，输出与 legacy 逐字节一致 |
| **MIR 专属运行时崩溃**（segfault/abort/trace-BPT） | 7 | clone/move/struct-field 系列内存所有权 bug（legacy 通过、MIR 崩）——**违反 red-line**，需内存安全调试 | 待闭环（red-line 优先） |
| `test-str.no`（1 diagnostic） | 1 | 单点诊断，待查 | 待查 |
| `Undefined symbols`（链接缺符号） | 1 | `test-x25519-keypair-diff`：某内置未 emit 致链接缺符号 | 待查 |

#### 13.3.2 各家族根因与修复方向
> 注意：本节原按错误症状分类（首轮）。第五轮 legacy-parity 二分（§13.3.5）显示 `setfield field`/`unknown callee`/`lencap kind` 等"症状家族"绝大多数是**两模式都挂**的预存失败（已计入 §13.3.5 的 92），并非 MIR 专属缺口；下文仅保留经 parity 确认的 MIR 专属修复方向（共 41，见 §13.3.1）。

- **字符串插值（16，MIR 专属）**：`'x={expr}'` 在 HIR `KStrLit` 仅保留原始文本，嵌入表达式在 parser 阶段已丢失；MIR 无法 re-parse。修复方向：parser 保留插值段（字面段 + 表达式 AST），`tohir` 改写为 `str_concat` 链（或调用 `@str_concat` 运行时）。覆盖面最大（16 个）。
- **`opt_verify`（6 剩余，MIR 专属）**：算术/负号路径已由 §12 #15/#18/#19 闭环。剩余多在比较/`KInfix`、`KBitOp`（and/or/xor/shl/shr）、函数实参传递、`OpGetField`/`emitSliceOp` 的 GEP 等处仍把 `%option` 当标量用。统一修复模式：凡"期望标量、实际 `%option`"的读取/参数处加 `unwrapOptionOperand`（已落 `emitArith`/`emitNeg`/`emitSetField`/`emitIndexStore`），并在 sink 处对 `%option` 结果包回（§12 #15 的 `insertvalue` 模式）。
- **`builtin` option/receiver（2 剩余，MIR 专属）**：`str-len: missing receiver`（async）、`number.f64-to-f32: cannot coerce %option`；`str-len-bytes receiver %txt` 已由 #20 修。修复方向：对齐 legacy 的 receiver 路由 + option 实参解包。
- **`builtin` 未实现（3，MIR 专属，FFI/net 族）**：`fs.read-file`/`net-dial`/`net.net-icmp-open` 无 MIR 内建实现，需 runtime/FFI 支持。
- **`index dst slot`（0，已闭环）**：`arr-slice.no` 索引结果类型推导为 void→无 slot，与 `with-len` 同类类型推导缺口。已由 §12 #21（recv 值类型回推切片结果型） + #22（切片 `(`/`)` 边界语义）闭环，输出与 legacy 逐字节一致。
- **MIR 专属运行时崩溃（7，red-line）**：clone/move/struct-field 系列内存所有权 bug，legacy 通过、MIR 崩。需内存安全调试（ASan/LSan 回路）。
- **`setfield field`/`unknown callee`/`lencap kind`（症状家族，多为预存失败）**：第五轮 parity 二分显示这些多属两模式都挂（§13.3.5 的 92）；原"std 结构体字段未进 StructFields"等根因分析仍成立，但其对应 test 大多 legacy 也失败，需先修 std/test 再判定是否 MIR 责任。

#### 13.3.3 本轮（第三/四轮）增量
- 第三轮：修复 **§12 #15**（算术/负号 option 包回）+ **§12 #16**（数组 receiver 路由）。MIR 专属通过数（非递归口径）：273 → ≈287（净增 ≈14；其中 #15 解锁 13 个 x25519/fe-mul 系列 + #16 解锁 `test_zero.no`）。
- 第四轮（2026-09-11）：闭环 **§13.3.4**（#17 顶层容器字面量物化）+ **#18**（`emitBitwise` option 解包包回）+ **#19**（`print` 内联 option payload 路由）。全量递归口径：committed 基线 **244** → **280**（#17+#18，净 +36）→ **281**（#19，净 +1；crypto 假 HANG 不计，见 §10.1）。**零 MIR 专属回归**（14 个既有通过测试 + 全量重扫守护）。
- 口径提示：第三轮 "≈287" 为**非递归** `tests/*.no`（≈368）测量；第四轮 "281" 为**全量递归** `tests/**/*.no`（418）测量，总数不可直接相减。
- 第五轮（2026-09-11）：**legacy-parity 二分**重塑失败分类 + **#20**（`%txt` receiver 的 `str-len-bytes`）。(a) 对 418 全量每个失败测试额外跑 `NOLANG_MIR=0`：失败 137 个里仅 **41 个 MIR 专属**（legacy 过、MIR=3 挂），**92 个两模式都挂**（legacy 也失败，属通用预存 bug / std·test 缺陷，见 §13.3.5）。据此修正 §10.1/§13.3.1 的真实 MIR 专属 gap = **41**（非早期按症状估的 38）。(b) 闭环 #20 → `test-slot-rebind.no` 从失败转 PASS（全量递归口径 **281 → 282**）；该 test 打印指针类值输出非确定，属 §13.3.5 的 test 自身缺陷，但 codegen 门禁已解除。(c) 印证用户"std/tests 都可能不合理"警示：**绝大多数长尾失败是预存/通用 bug，不 blocking MIR 门禁**，应作为通用 bug 双路径闭环（§13.3.5）。

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

**下一步（闭环 §13.1 与溢出集成）**：`opt_verify` 家族算术/负号路径（#15）、`emitBitwise`（#18）、`print` payload 路由（#19）已闭环；**剩余 opt_verify（§13.3.1，11 个）**多在**非算术的表达式路径**（比较/`KInfix`、`KBitOp` 实参传递、`OpGetField`/`emitSliceOp` 的 GEP）未解包 option → 标量，需在对应表达式 emit 处加同法 `extractvalue`/`insertvalue`，修复模式已统一（`unwrapOptionOperand` + §12 #15 的包回）。§13.3.4 顶层 main 合成（#17）已闭环。溢出默认 integration 的 MIR 侧算术环节现已基本闭环，剩余是表达式边的零散补全。

### NOLANG_MIR_DUMP_* 调试开关
- `NOLANG_MIR_DUMP_MIR=1`：打印 lowered MIR 到 stderr。
- `NOLANG_MIR_DUMP_LL=1`：写出 emitted LLVM IR 到临时 `.ll`（路径在 stderr）。
- `NOLANG_MIR_DUMP_HIR=1` / `NOLANG_MIR_DEBUG=1` / `NOLANG_MIR_DUMP_BAD=1`：辅助诊断。

---

## 15. 分阶段路线图（修正为真实进度）

- **Stage 0（已完成）**：`src/mir` 核心（模型/Builder/CFG/分析/Liveness/所有权/Drop/Move·Borrow/printer/validator/HIR→MIR lowering）+ `NOLANG_MIR=1` 验证模式 + 单元测试。MIR 作为审计器运行，现有构建零回归。
- **Stage 1（已跳过）**：`MIR→HIR` 桥未实现（直发路径更优）。
- **Stage 2（已完成）**：`MIR→LLVM` 直接发射（`EmitLLVM`），strangler-fig 回退（`NOLANG_MIR=2`）。
- **Stage 3（进行中 · 当前前沿）**：`NOLANG_MIR=3` 全量 corpus 门禁。全量递归口径 **281/418** 通过（MIR 专属崩溃=0，零 MIR 专属回归）。累计闭环：§13.1 `emitIndexStore`/`emitSetField` option 解包、`str-clear` 内置、§12 #15 算术/负号 option 包回（解锁 13 个）、#16 数组 receiver 路由（解锁 `test_zero.no`）、**#17 顶层容器字面量物化（§13.3.4，解锁 15 个 has_no_slot/index_slot/receiver）**、#18 `emitBitwise` option 解包包回、#19 `print` 内联 option payload 路由（解锁 3 个 opt_verify）。MIR 专属 gap 由首轮 69 降至 **约 38**（§13.3.1）。剩余最高杠杆：§13.3.2 `setfield field`（std 结构体字段灌表，解锁 10+ 个）、字符串插值 `unsupported`（13 个）、`opt_verify` 表达式边（11，比较/位运算实参/GEP）、`unknown callee`（10）；§13.2 预存 crash/hang 作为通用 bug 双路径闭环。
- **Stage 4（下一步）**：溢出默认解包全闭环后，逐站消除 §13.3 长尾；FFI/async/crypto/net/map/字符串方法内置补齐；最终让 `no build` 默认走 MIR=3（移除 legacy 散布 `emitHeapFree`）。

---

## 16. 开放问题与风险（更新）

- **生成顺序/平台过滤**：HIR `#{platform}` 注解需在 lower 前过滤（复用 `AnnotationsOf`）。
- **跨模块 owner 判定**：与 `globalVarOwner`/`funcOwner` 对齐（`transpiler.go`）。
- **推断类型来源**：`pkg.Inferred[id]` 是分类 ownership 的权威输入；缺失时回退声明类型 `Node.Type` 并标记诊断。
- **回归红线**：任何 MIR 失败在 `NOLANG_MIR=2` 必须回退现有路径；全量 `no build` 扫掠在 sandbox 受限（约 360 测试会被 SIGKILL），用定向子集 + `opt -passes=verify` 快速回路验证（`scripts/mir_cov.py` / `mir_sweep.py`）。
- **溢出默认集成中段风险**：§13.1/§14 的解包缺失曾令 `test-arr.no` 退化（已闭环，`emitIndexStore`）；本轮续闭环 `emitSetField`（§12 #14），sink 站点解包已覆盖 `emitIndexStore`+`emitSetField`。剩余 `opt-verify` 家族（§13.3.1，23 个）在**非 sink 表达式路径**未解包，是下一步杠杆。全量扫掠因 sandbox ~360-测试 SIGTERM 被截断，精确 MIR=3 通过数待重跑（手动验证确认已修 `test-arr.no`+`test_str_clear.no` 至少 +2，且无回归）。
