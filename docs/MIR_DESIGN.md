# Nolang MIR — 工业级中端中间表示设计（完整方案）

> 状态：方案 v1（设计完成，核心执行中）
> 作者：编译器工作流
> 关联：`src/hir`（HIR）、`src/build/llvm`（LLVM 后端）、`src/parser/tohir.go`（AST→HIR）

---

## 1. 动机：为什么需要 MIR

当前 Nolang 的内存安全（无 GC、deferred move + scope-exit destruction）完全由
`src/build/llvm/` 下约 450KB 的 emitter 在生成 LLVM IR 时**手工散布**地实现：

- `emitHeapFree` 在每个函数结尾/移动点对 owned 局部（`str`、`[]T`、`map`、`?T`、
  含 owned 字段的 struct）插入 `getelementptr → load .data → icmp null → call @free`；
- 移动语义、克隆、借用关系分散在 `stmt.go` / `expr.go` / `call.go` / `call_stdlib.go` 的数百处特判里。

这导致了当前暴露出的**大量内存问题**家族（UAF、double-free、OOB、flaky abort）：

1. **双重释放 / 使用已释放（double-free / UAF）**：当一个 owned 值被 move 到另一个
   变量后，原变量仍被结尾的 `emitHeapFree` 释放一次，而目标变量又被释放一次；或
   跨分支 move 后某条路径对源变量再次 free。
2. **值/指针混淆导致的越界（OOB）**：返回类型传播错误使 alloca 类型与 store 类型
   不一致（如 `%vec` 被当作 `%str-long`、定宽数组被当作 `[0 x i16]`），`opt` 阶段
   即报 `defined X but expected Y`，或在运行期 memcpy 越界。
3. **生命周期/借用关系无法静态验证**：emitter 没有统一的“谁拥有、何时释放、借用在
   何处结束”的数据结构，只能逐点 patch，新增语言特性（coroutine、FFI、泛型）时极易
   引入回归。

**结论**：内存安全逻辑必须从一个“散布在 450KB emitter 中的隐含约定”，提升为一个
**显式、可分析、可验证、可机械翻译**的中间表示——MIR。LLVM 后端退化为 MIR 的
机械翻译器，不再自己决定释放点。

---

## 2. 在编译 pipeline 中的位置

```
        parser         checker           HIR                MIR                LLVM (+opt/llc/clang)
  .no ──────► AST ─────────► 标注类型 ───► hir.Package ───────► mir.Module ───────► LLVM IR ──► 可执行
                         (PopulateInferredTypes)   (ASTToHIR)      (HIR→MIR)        (MIR→LLVM)
```

- HIR 已存在（`src/hir`）：结构是“去指针化的 AST 扁平竞技场”，携带声明/推断类型字符串，
  但**没有控制流图、没有内存语义、没有 SSA**。它是 lowering 的源，不是内存分析的载体。
- **MIR 是新增层**：从 HIR 生成；显式建模 basic block、owned/borrowed 值、move/clone/drop。
- 开关策略（strangler-fig，与现有 `NOLANG_HIR` 同构）：
  - `NOLANG_MIR=1`：在 `transpiler.go` 中 `ASTToHIR` 之后插入 `mir.FromHIR` →
    `mir.Analyze` →（验证模式：打印 MIR + 内存诊断，回退到现有 `GenerateHIR`；
    后续阶段：`mir.ToLLVM` 直接发射）。
  - 默认（未设置 / `NOLANG_MIR=0`）：行为完全不变，现有 HIR 路径照常。
  - 任何 MIR 阶段失败（unsupported kind / 分析错误）都**安全回退**到现有路径，绝不
    让构建崩溃——这是工业级渐次替换的红线。

### 2.1 与用户初定方案的对应

用户初定方案：“第一遍解析 std 和 user 的 ast，生成 hir；user 的 hir 生成 mir；
过程中根据 user 的引用情况，把用到的 std hir，生成 mir”。

MIR 设计为**按可达性函数级 lazy lower**：

- 入口：从 `main`（`KFuncDef` 名为 `main`）出发，建立函数 worklist。
- 凡 `KCall`/`KFuncDef`（方法）引用到的函数名，从 HIR `pkg.Top`（已 merge 进 user+std）
  中取出对应 `KFuncDef`/`KExtern` 一并 lower。
- 未被引用的 std 函数不进入 MIR（死代码消除，与现有 `alwaysAutoLoadStd` 整模块
  codegen 形成对比，是 HIR 优化计划的 Stage 2 根因之一的解法）。
- 这天然实现“user 的 hir 生成 mir，用到的 std hir 才生成 mir”。

---

## 3. 设计原则

1. **显式内存所有权（Explicit Ownership）**：每个值携带 `Owned`/`Borrowed` 标记；
   释放点由 MIR 分析唯一决定，emitter 不再自行决定。
2. **CFG 一等公民**：basic block + 前驱/后继 + 支配树，所有数据流分析建在 CFG 上。
3. **准 SSA**：函数内局部值用 `ValueID` 编号、单次定义（phi 在块汇合处）；参数/Alloc
   是定义点。便于 liveness / 所有权数据流。
4. **可验证**：`mir.Validate` 做结构校验；`mir.Analyze` 产出内存诊断（use-after-move、
   duplicate-drop、missing-drop、borrow-escape）。这些是“大量内存问题”的系统性探测器。
5. **机械翻译**：MIR→LLVM 应是逐 op 翻译 + 调用约定映射，不含语义判断。
6. **单向依赖**：`mir` 依赖 `hir`（parser→hir→mir），不反向；`mir` 不 import
   `build/llvm`（保持可独立测试）。

---

## 4. 数据模型

### 4.1 类型与所有权

```go
type Type struct {
    ID    TypeID
    Raw   string   // nolang 类型串: "str","i64","[]byte","?db","[3]u16","vec", struct 名...
    Kind  TypeKind // Int/Float/Bool/Char/Str/Slice/Array/Map/Option/Ptr/Struct/Func/Void/Unknown
    Owned bool     // true ⇒ 堆拥有, 需要且只需一次 Drop
    Elem  TypeID   // Slice/Array/Option/Ptr 的元素类型
    Sizes []int64  // 定宽数组维度
}
```

**Owned 分类规则**（`mir.ClassifyOwnership`）：
- `str`、`[]T`(`vec`)、`map[K]V`、`?T`(其中 T owned) ⇒ `Owned=true`；
- struct：任一字段 `Owned` ⇒ 整体 `Owned`；
- `&T`（指针/借用）⇒ `Owned=false`（借用，不释放）；
- 标量（`i64/u8/f64/bool/char`）及纯标量 struct ⇒ `Owned=false`。

### 4.2 值、指令、块、函数、模块

```go
type Value struct { ID ValueID; Name string; Type TypeID }   // 准 SSA 值

type Inst struct {
    ID    InstID
    Op    Op
    Dst   ValueID       // 结果；NoVal 表示无结果
    Args  []ValueID     // 操作数
    Type  TypeID        // 结果类型
    Block BlockID
    Int   int64         // 整型/布尔/枚举载荷
    Flt   float64
    Str   string        // 常量文本 / 标签 / 字段名
    Sym   string        // 被调用函数名 / 外部符号
    Line, Col int32
}

type Block struct {
    ID     BlockID
    Name   string
    Insts  []InstID
    Term   *Term        // 终结器（必须恰好一个）
    Preds  []BlockID
    Succs  []BlockID
}

type Term struct {            // 终结器
    Op      Op               // OpReturn / OpBr / OpCondBr / OpSwitch
    Args    []ValueID        // Return 的操作数 / CondBr 的条件
    Targets []BlockID        // Br/CondBr 的目标块
}

type Function struct {
    ID        FuncID
    Name      string
    Params    []ValueID
    Results   []TypeID
    Blocks    []BlockID
    Entry     BlockID
    LocalTypes map[ValueID]TypeID  // 值 → 类型（含所有权）
    IsExtern  bool
    IsMethod  bool
    Receiver  TypeID
}

type Module struct {
    Name      string
    Funcs     []Function
    Types     []Type
    TypeMap   map[string]TypeID
    Consts    []Const
    Externs   []ExternDecl
    Globals   []GlobalDecl
    FuncByName map[string]FuncID
    Lowered   map[string]bool   // 可达性 lower 记忆化（std 纳管）
}
```

---

## 5. 指令集（内存感知）

| 类别 | Op | 语义 |
|---|---|---|
| 终结器 | `OpReturn` / `OpBr` / `OpCondBr` / `OpSwitch` | 控制流 |
| 内存 | `OpAlloc` | 栈/堆槽（带 `Owned` 标记） |
| | `OpLoad` / `OpStore` | 读写槽 |
| | `OpMove` | 转移所有权：源值失效，释放责任转交目标 |
| | `OpClone` | 深拷贝（堆数据复制） |
| | `OpDrop` | 析构 + `@free`（owned，恰好一次） |
| | `OpBorrow` | 取引用（不转移所有权） |
| 数据 | `OpConst` | 整/浮/布尔/字符串字面量（字符串指向 `@.str` 全局） |
| | `OpGetField` / `OpSetField` | 结构体字段 |
| | `OpIndex` / `OpSliceOp` | 数组/切片索引与切片 |
| 算术/逻辑 | `OpAdd/Sub/Mul/Div/Mod/Neg/Not/And/Or/Xor/Shl/Shr` | |
| 比较 | `OpEq/Ne/Lt/Le/Gt/Ge` | |
| 控制值 | `OpPhi` | 块汇合处 SSA |
| 调用 | `OpCall` / `OpCallExtern` / `OpCallFFI` | 用户/外部/FFI；返回 owned 时按调用约定 |
| 转换 | `OpCast` | 类型转换（含 zext/sext 修复点） |

**调用约定（MIR 层统一，修复“返回类型传播”bug 族）**：
- owned 返回值走 **sret**：调用方 `OpAlloc` 一个 owned 槽作为隐式首参（out-param），
  callee 写入后调用方持有所有权；**不再依赖 emitter 对返回值 LLVM 类型的猜测**。
- 借用参数（`&T`）按指针传；owned 参数默认 move（调用后源失效）或 clone（按需）。

---

## 6. 所有权与生命周期模型

- **Owned 值的一生**：`OpAlloc(Owned)` → 若干 `Load/Store/Call` 使用 → 恰好一次 `OpDrop`
  （在最后使用之后、或块/函数出口，受 move 影响）。
- **Move**：`v2 = OpMove v1` ⇒ `v1` 进入 moved 集合（`v1` 此后不可再 read，否则诊断
  use-after-move）；`v2` 继承 `v1` 的所有权与释放责任。`Drop` 跟随 `v2` 而非 `v1`
  ⇒ **原生解决 double-free**（源与目标不会都被 free）。
- **Clone**：`v2 = OpClone v1` ⇒ 深拷贝，二者各自独立 owned，各自需一次 `Drop`。
- **Borrow**：`r = OpBorrow v`（owned）⇒ `r` 是 `&T`，生命周期被限定在 `v` 存活的
  支配域内；borrow 逃逸（存到更长寿命位置）⇒ 诊断 borrow-escape。

---

## 7. CFG 与（准）SSA

- `mir.BuildCFG` 计算每块的 `Preds/Succs`（由 `Term.Targets` 推导）。
- 支配树（简易 Lengauer-Tarjan 或迭代支配前沿）用于：borrow 生命周期界定、phi 放置。
- SSA：每个局部值单次定义；条件分支汇合处用 `OpPhi` 合并。MIR 不强制全局 SSA（LLVM
  后端可做 mem2reg），但 owned 值的“定义-最后使用-释放”链路必须是显式且可分析的。

---

## 8. 分析 Pass（MIR 的核心价值）

### 8.1 Liveness（反向数据流）
`liveOut(b) = ∪ liveIn(s), s∈succ(b)`；`liveIn(b) = use(b) ∪ (liveOut(b) − def(b))`。
注意：**owned 值的“使用”含其释放前的最后一次读**；`OpDrop` 是 owned 值的最后一次使用。

### 8.2 所有权分类
`ClassifyOwnership` 给每个值打 `Owned`/`Borrowed`。

### 8.3 Drop 插入算法（修复 double-free / 缺失释放）
```
for each function f:
  for each owned local v (defined by Alloc/Move/Clone/Call-sret):
    lastUse = last instruction that reads v before v's lexical scope exit / move
    if v was moved: drop responsibility already transferred → do NOT drop v
    else: insert OpDrop v immediately after lastUse
            (if v never read after def: insert at scope/function exit)
  ensure: exactly one OpDrop per owned value (assert, never zero, never two)
```
- **move 正确性**：`OpMove` 把释放责任从源移到目标，源不再 drop ⇒ 消除 double-free。
- **跨分支**：liveness 在 CFG 上计算，某分支 move、另一分支不 move 时，drop 点按
  各分支最后使用分别插入 ⇒ 消除 UAF/漏释放。

### 8.4 Move / Borrow 检查器（诊断，不直接改 IR）
- use-after-move：moved 值在其后被 read ⇒ 诊断。
- duplicate-drop / missing-drop：分析后某 owned 值 0 或 >1 次 drop ⇒ 诊断。
- borrow-escape：borrow 被存入超出 owner 支配域的位置 ⇒ 诊断。
- 这些诊断直接对应“大量内存问题”，是修复前的系统性定位工具。

### 8.5 OOB / 类型一致性（修复“值/指针混淆”bug 族）
- 所有 `OpIndex`/`OpGetField`/`OpStore` 的操作数类型在 `Validate` 中一致性校验；
- 定宽数组 `[N]T` 与 slice `[]T` 在 MIR 中**区分类型**（数组 `Kind=Array` 带 `Sizes`，
  slice `Kind=Slice`）⇒ 消除 `test-fmt-heuristic`/`test-all` 的 `[0 x i16]` 误判；
- `OpCast` 显式表示 zext/sext，集中修复 `test-str` 的 `zext i1→i64`。

---

## 9. HIR → MIR Lowering（核心子集，按可达性）

映射表（节选）：

| HIR | MIR |
|---|---|
| `KFuncDef`(name, params, results, body) | `Function`（entry block + body blocks） |
| `KLet name = value` | `Alloc(Owned?) + Store` 或 直接 bind（值由 RHS 指令产出） |
| `KAssign` | `Store` / `Move`（按 RHS 是否 owned + 是否复用） |
| `KReturn` | `OpReturn` |
| `KIf` (cond/then/else) | `Alloc` 结果 + `OpCondBr` + 两个分支块 + `OpPhi` |
| `KFor` (init/cond/update/body) | 前置块 + 条件块(`OpCondBr`) + 体块 + 更新块 + 出口块 |
| `KCall`(fn, args) | `OpCall`（owned 返回走 sret 槽） |
| `KInfix`/`KPrefix` | 算术/比较 `Op*` |
| `KIntLit/KStrLit/...` | `OpConst` |
| `KIdent` | 读 `ValueID`（load 或 SSA 值） |
| `KStructLit`/`KArrayLit`/`KSliceLit` | 构造指令（owned，需 Drop） |
| `KIndex`/`KDot` | `OpIndex`/`OpGetField` |

unsupported kind ⇒ 记录 diagnostic 并安全终止该函数的 lower（验证模式），
不影响其他函数或现有构建。

---

## 10. MIR → LLVM 后端策略

**v1（本阶段）**：验证模式——`FromHIR` + `Analyze` + `Print` 到 debug 文件，回退现有
`GenerateHIR`。MIR 作为**内存安全审计器**运行，输出每个函数的 drop 计划与诊断。

**v2（后续）**：`MIR → HIR` 桥（把 MIR 的 `Move/Drop/Clone` 表达为 HIR 的合成节点，
复用现有 `GenerateHIR` 的成熟 LLVM 发射），让已经过 MIR 验证的内存决策直接落到现有
后端——低风险、可增量。

**v3（目标）**：`MIR → LLVM` 直接发射。由于 MIR 已显式拥有所有权与 drop 计划，后端
退化为：每个 `Op*` → 对应 LLVM；`OpDrop` → `emitHeapFree` 块；调用约定按 §5 映射。
此时 `emitHeapFree` 的散布逻辑被删除，内存安全由 MIR 保证。

---

## 11. 不变式与验证 Oracle

- **Oracle（与现有路径等价）**：`NOLANG_MIR` 开启时，最终产生的可执行行为必须与
  现有 HIR 路径逐字节/逐输出一致；MIR 只是“把内存决策说清楚”，不改变语义。
- **结构不变式**：每块恰好一个 `Term`；每个 `ValueID` 单次定义；`OpDrop` 恰好一次/
  owned 值；`OpMove` 源之后无 read。
- **Sanitizer 回路**：对 MIR 产出的二进制跑 `ASan`/`LSan` 作为回归门（长期）。

---

## 12. 对本轮暴露 bug 族的正确性论证

| Bug 族 | 现状根因 | MIR 如何根治 |
|---|---|---|
| double-free / UAF | move 后源仍被 `emitHeapFree` | `OpMove` 转移释放责任，源不再 drop（§6/§8.3） |
| 值/指针混淆 / OOB | 返回类型传播错误、数组/slice 误判 | 类型在 MIR 显式 + `Validate` 一致性（§8.5） |
| zext i1→i64 | 隐式转换散落 | `OpCast` 集中（§5/§8.5） |
| flaky abort（test-fields/test-basic） | 生命周期/借用未静态约束 | borrow 支配域检查（§6/§8.4） |
| FFI 字符串编组（test-std-new） | 传结构体地址而非 `.data` | 调用约定在 MIR 统一 sret/参数（§5） |

---

## 13. 分阶段路线图

- **Stage 0（本阶段 · 执行中）**：完整设计 + `src/mir` 核心（模型/Builder/CFG/
  liveness/ownership/drop/move·borrow/printer/validator/HIR→MIR lowering）+ 接线
  `NOLANG_MIR=1` 验证模式 + 单元测试。MIR 作为审计器运行，现有构建零回归。
- **Stage 1**：`MIR→HIR` 桥，把 drop/move/clone 计划落到现有后端；逐步把有把握的函数
  切到 MIR 路径，用 oracle 逐测试对比。
- **Stage 2**：`MIR→LLVM` 直接发射（核心子集先跑通 hello/算术/控制流/调用/字符串）。
- **Stage 3**：覆盖 builtins / FFI / 泛型 / coroutine；全量测试切换；删除 emitter 中
  散布的 `emitHeapFree` 逻辑。
- **Stage 4**：修复全部既有 bug 族（在 MIR 成熟后，按 §12 逐项闭环）。

---

## 14. 开放问题与风险

- **生成顺序/平台过滤**：HIR 的 `#{platform}` 注解需在 lower 前过滤（复用 `AnnotationsOf`）。
- **跨模块 owner 判定**：与现有 `globalVarOwner`/`funcOwner` 映射对齐（transpiler.go:2300）。
- **推断类型来源**：`pkg.Inferred[id]` 是 MIR 分类 ownership 的权威输入；缺失时回退到
  声明类型 `Node.Type` 并标记诊断。
- **回归红线**：任何 MIR 失败必须回退现有路径；全量 `no build` 扫掠在沙箱受限（约 360
  测试会被 SIGKILL），用定向子集 + `opt -passes=verify` 快速回路验证。
