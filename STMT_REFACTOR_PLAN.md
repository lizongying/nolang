# stmt.go 重构方案

对象：`src/build/llvm/stmt.go`（9461 行 / 407 KB）
所有数字均为本仓库实测（`git log` + 源码扫描），非估算。

---

## 一、现状实测

### 规模与结构

| 指标 | 值 |
|---|---|
| 行数 / 函数数 | 9461 / 107 |
| `if` 语句 / `switch` 语句 | 1346 / 45 |
| 完整 `if` 行数 / 去重后 | 1244 / **759**（485 行是重复条件） |
| 最大函数 `generateLet` | **1891 行**，约 35 个顶层顺序分支 |
| `varLLVMType` | 869 行，208 个 `if`，116 个 `return` |

### 改动频次（近 6 个月，全仓库 top 5）

| 文件 | 提交数 |
|---|---|
| **src/build/llvm/stmt.go** | **181** ← #1 |
| src/build/transpiler.go | 166 |
| src/build/llvm/expr.go | 153 |
| src/build/llvm/call.go | 141 |
| src/build/llvm/generator.go | 128 |

stmt.go 的 181 次提交中：`fix` 52 次、`feat` 67 次、`refactor` 32 次；
其中 **26 次直接是内存安全**（double-free / UAF / clone / free）。
标题样本：`prevent double-free`、`fix use-after-free in out/global dot expression assignment`、
`handle variable move safety inside loops with back edges`。

> 这是本方案的核心事实：**stmt.go 是全仓库最热的文件，且热度主要来自修 bug，不是加功能。**

### 测试覆盖

| 项 | 值 |
|---|---|
| llvm 包测试文件 / 行数 | 29 / 6851 |
| 引用 `generateLet`/`varLLVMType`/`emitHeapFree` 的测试 | **0** |
| e2e `.no` 用例 | 350 |
| Makefile 中的全量跑测目标 | **无** |
| `dist/*.ll` | 123 个零散文件，非系统性基线 |

**1891 行最复杂的函数、869 行的类型推导、全部所有权决策——零单测。**
唯一的防线是 350 个 e2e，且失败形态是退出码 139/133，无法定位到决策点。
这就是"改一处、坏一处、再打补丁"的闭环成因。

---

## 二、四个根因

### R1｜`generateLet` 是追加式特例链，不是分派

1891 行是一个巨大的顺序 `if` 级联，每个分支头顶都挂着一段注释解释它在修哪个历史 bug：

```
6756  if !stmt.IsSynthetic {                    // task = run ...
6769  if stmt.IsSynthetic {                     // match err/nil arm
6855  if g.outputParamNames != nil && ...[name] // 返回值延迟零值
6865  if g.heapVars != nil && g.outputParamNames... // moved 追踪
6883  if _, isSliceExpr := stmt.Value.(*SliceExpression)  // 切片视图
6911  if ident, ok := stmt.Value.(*Identifier)  // view 赋给 out
6954  if call, ok := stmt.Value.(*CallExpression) // with-cap
7111  if llvmTypeCheck != "%option" && ...      // 特例：fd i64 vs %option
7127  if llvmTypeCheck == "%option" {
7242  if g.heapVars != nil {                    // 统一 clone/move ← 本该是唯一决策点
7499  if idxExpr, ok := ...(*IndexExpression)   // x = vec[i]
7563  if dotExpr, ok := ...(*DotExpression)     // x = s.field
7689  if dot, ok := ...(*DotExpression) && ...  // 又一个 DotExpression 分支
7739  if llvmType == "%str-long" {              // s = "" 优化
7763  if stmt.IsSynthetic && g.isStructLLVMType // ok arm 结构体
7861  if stmt.IsSynthetic && llvmType == "i64"  // it 预分配为 i64
7895  if _, isCall := ...(*CallExpression); ... // 结果参数模式
7900  if sl, ok := ...(*StructLiteral)
8130  if mt, ok := stmt.Type.(*MapType)
8165  if at, ok := stmt.Type.(*ArrayType)
8264  if existingType, ok := g.varTypes[name]... // 类型强制
8339  if !alreadyCoerced && (llvmType=="double"||...)
8362  if !alreadyCoerced && (llvmType=="i8"||...)
8368  if !alreadyCoerced && g.isIntegerLLVMType...
8391  if !alreadyCoerced && llvmIntBitWidth(...)==64
8405  if stmt.IsSynthetic {
8448  if g.outputParamNames != nil && ...[name] // out 立即 store
8457  if g.heapVars != nil {                    // 又一个堆追踪
8493  switch llvmType {
```

三个致命属性：

1. **顺序耦合**：分支体里有 `return`，所以"第 30 个分支能否执行"取决于前 29 个分支的
   判断结果。加新分支必须理解全部历史分支。
2. **同一节点类型被处理多次**：`*DotExpression` 在 7563、7689 各开一个分支；
   `*Identifier` 在 6911 和 7242 各处理一次；`*CallExpression` 在 6954、7895 各一次。
   两次处理之间状态已被修改，语义靠巧合成立。
3. **状态用裸布尔横向传递**：`oldValFreed`、`alreadyCoerced`、`isWithCapCall`
   在 35 个分支间手工维护，漏设一处就是 UAF 或 double-free。

### R2｜类型真相在 codegen 侧重建，且是 stringly-typed

`varLLVMType` 869 行在**代码生成阶段重新推导类型**——这本该是 checker 已经算完的东西。
更严重的是类型用字符串表示，全文件字符串比较：

| 字面量 | 出现次数 |
|---|---|
| `"i64"` | 88 |
| `"%str-long"` | 59 |
| `"%vec"` | 59 |
| `"%option"` | 33 |
| `"%arr"` | 27 |
| `"i1"` | 22 |
| **合计** | **288** |

字符串类型的后果：`switch llvmType` 漏一个 case 编译器不报错，拼错一个字符运行时静默走
default。所有"这个类型是不是堆拥有型"的判断都退化成字符串前缀匹配。

### R3｜同一概念被拆成 7 张并行 map

`generator.go:238-252` —— 全部以变量名为键，各持有"变量 X 的类型"的一个切面：

```go
varTypes          map[string]string  // 145 处引用  → LLVM 类型
arrayElemTypes    map[string]string  // 105 处      → 元素类型
optionInnerTypes  map[string]string  //  50 处      → option 内层
elemElemTypes     map[string]string  //  28 处      → 嵌套元素
heapVars          map[string]string  //  26 处      → 堆类型
arraySizes        map[string]int64   //  15 处      → 数组长度
itAllocTypes      map[string]string  //  13 处      → it 的 alloca 类型
```

声明一个 `[][]str` 要**手工同步写 3~4 张 map**，漏写一张不报错，只会在某个下游分支
静默退化成 `i64`——这正是"类型信息莫名其妙丢了"类 bug 的来源。

> **全包口径**（非仅 stmt.go，覆盖 42 个文件 / 35359 行非测试代码）：
> 7 张并行 map 被引用 **728 次**（`varTypes` 322 / `arrayElemTypes` 162 /
> `structTypes` 68 / `optionInnerTypes` 69 / `elemElemTypes` 37 / `arraySizes` 26 /
> `heapVars` 26 / `itAllocTypes` 18）；字符串类型字面量比较 **877 处**
> （`i64` 285 / `%str-long` 175 / `%vec` 136 / `double` 88 / `%arr` 75 / `i1` 57 / `%option` 52）。

由此还派生出两个噪声：

- **nil map 防御**：`g.arrayElemTypes != nil` 出现 40 次（其中 19 次守卫的是**读**，
  Go 里读 nil map 是合法的，纯冗余）；`g.varTypes != nil` 32 次（17 次守卫读）。
  真正的危害不是冗余，而是**每次 nil 检查都创造一条静默的 else 路径**。
- **散落的组合谓词**：`g.outputParamNames != nil && g.outputParamNames[name]` 出现
  **15 次**，没有 `isOutputParam(name)` 辅助方法；`os.Getenv("NOLANG_DEBUG_IT")`
  在代码生成热路径被调用 **15 次**。

### R4｜所有权策略没有单一决策点

stmt.go 里的 clone/free 助手函数族：

| 释放（12 个变体） | 拷贝（8 个变体） |
|---|---|
| `emitHeapFree` | `emitDeepClone` |
| `emitGlobalHeapFree` | `emitContainerClone` |
| `emitLocalTasksFree` | `emitDeepElementClone` |
| `emitVarHeapFree` | `emitStructClone` |
| `emitVarHeapFreeDirect` | `emitInlineArrayFieldClone` |
| `emitVarHeapFreeViaLocalCopy` | `emitStructFieldClone` |
| `emitShallowDataFree` / `emitShallowDataFreeDirect` | `emitStructElementsClone` |
| `emitNullCheckFree` | `emitOptionDeepClone` |
| `emitBitCheckFree` | |
| `emitDeepContainerFree` | |
| `emitElementFree` / `emitStructFieldsFree` / `emitInlineArrayFieldFree` / `emitStructFieldFree` / `emitOptionHeapFree` | |

每遇到一个新场景就加一个变体，而不是扩展一个按类型分派的函数。
`7242 行 if g.heapVars != nil` 那段注释写着"统一赋值逻辑"，但它只是 35 个分支中的一个，
前后还有多个分支在做同样的事。

### R5｜手工 CFG 记账 + 5 套手写循环脚手架

| 项 | 次数 |
|---|---|
| `g.cfgEdge(` | 73 |
| `g.cfgTerm(` | 56 |
| `g.emitLabel(sb,` | 74 |
| `loopExits` push / pop | 5 / 5 |

5 套循环骨架（`generateForStatement` / `generateRangeFor` / `generateArrayRange` /
`generateStringRange` / count 循环）各自手写 `for.cond / for.body / for.step / for.end`
+ CFG 边 + `loopExits` 压栈出栈。

> 补充实测：三套 range 实现的行级相似度只有 **17~21%**（我原本假设是复制粘贴，
> 实测推翻了）。所以问题不是"重复代码"，而是**同一套骨架协议被 5 次手工实现**。
> 漏一条 `cfgEdge` 不报错，只让 moved 数据流算错 → 错误的 move/free。
> 这直接对应那条提交：`handle variable move safety inside loops with back edges`。

---

## 三、关于"字段放到 AST 节点"——我的判断

**部分同意，但对核心部分我有不同意见。**

✅ **同意**：语法性/结构性的事实应该上 AST，且已有先例。
`LetStatement.IsSynthetic` / `ItArmType` / `IsModuleConst`（ast.go:481-497）以及
`57310fc refactor(module): embed module owner and source file in AST nodes` 都走的是这条路，
效果是对的。同类候选：`ForStatement` 的循环种类、`CallExpression` 的结果参数模式标记。

❌ **不同意**：**已解析的 LLVM 类型不该放进 AST 节点。** 三个理由：

1. **AST 是共享的**：`src/fmt`（格式化器）、LSP、`src/parser/dump` 都在消费同一批节点。
   把 `%str-long` 这类后端概念塞进 AST，等于让 LLVM 泄漏到前端。
2. **AST 会被合并与复用**：transpiler 的模块合并 pass 会复制/改写节点，
   挂在节点上的可变后端状态会在合并时被意外共享或丢失。
3. **混淆两类信息**：`IsSynthetic` 是"这句话从哪来"（语法事实，永不变）；
   `varTypes[name]` 是"这个变量现在是什么类型"（流敏感、随作用域变化）。
   后者塞进 AST 节点，等于把数据流分析的结果固化到语法树上。

✅ **替代方案：Sema sidecar（旁路表）—— 而且它已经存在**

> **修正（2026-08-31 复核）**：我最初把 sidecar 当作"新提案"，这是错的。
> 本仓库**早已有 sidecar**，且架构原则就写在源码注释里：
>
> - `src/parser/resolver.go:68` `type SemanticContext struct`
> - parser 持有 `sem *SemanticContext`（`parser.go:24`，注释原文即「語義副表」）
> - `nodeSem map[Node]*NodeSemantics` —— 正是指针键旁路表
> - `resolver.go:60-67` 明文原则：**「語義結果集中存放、AST 節點零語義字段」**
> - 已有 `Merge()` 处理模块合并（按节点指针键，不冲突）
> - codegen 已在消费：`stmt.go:3596` `g.sem.EmbedDataOf(ls)`
> - 旁证：表层语法标志位 `RTFlag uint8`（裸 match / wildcard / standalone if / elif）
>   放在 sidecar 里，**不在 AST 上**
>
> 唯一缺失：`NodeSemantics` 目前只有 annotations / platformKeys / genericParams /
> embedData / RTFlags，**没有类型信息**。类型仍在 `VarTypes map[string]string`，
> 且同样是 stringly-typed。

所以 Stage 2 **不是新建 sidecar，而是把 `TypeRef` 挂到已有的 `nodeSem` 上**：

```go
// 扩展已有的 NodeSemantics，不新建结构
type NodeSemantics struct {
    // ... 既有字段 ...
    Type TypeRef   // 该节点的已解析类型（取代 g.varTypes 等 7 张 map 的查表）
}
```

这同时说明：AST 上那 16 个 `Is*` 字段是**偏离项**，不是规范；
`IsModuleConst`（由数据流 pass 写入且可翻转）正是违反本项目自身原则的实例。

---

## 三·五、那要不要直接上 MIR？

症状清单（728 次并行 map 引用、877 处字符串类型比较、35 处分散的所有权决策、
181 次 churn 中 26 次内存安全）确实指向"该有 MIR 了"。但复核后发现，
**这个问题的性质不是"要不要新建 MIR"，而是"有一个没有指令的 MIR 已经躺在仓库里"。**

### 已有的 MIR 骨架

| 组件 | 位置 | 状态 |
|---|---|---|
| `BasicBlock`（label / Preds / Succs / Terminator / Order） | `dataflow.go:31` | ✅ 已有 |
| `FuncCFG`（Entry / Blocks / Order） | `dataflow.go:95` | ✅ 已有 |
| `effect` 副作用流（move / reassign / bind / init / assign） | `dataflow.go:60` | ✅ 已有 |
| `freeSite` 释放决策点 | `dataflow.go:83` | ✅ 已有 |
| bitset 数据流求解器（must / may / mustNot 三态） | `dataflow.go` | ✅ 已有 |
| **指令（Instr）** | — | ❌ **缺失** |

**`BasicBlock` 只存 label、边、terminator、effects、freeSites，不存指令。**
指令是 `strings.Builder` 里的文本（`stmt.go:3589` / `3643` 的两阶段 `bodyBuf` 发射）。
也就是说：CFG 是一张**挂在字符串打印机旁边的影子图**，不是程序的主表示。

两个佐证：

- `freeSite.Marker string` 注释写着"在 bodyBuf 中的占位标记"，但**全仓无人赋值、无人读取**
  → 死字段，是被放弃的"文本占位符回填"设计留下的化石。
- `freeSite.LLVMType` / `ElemType` 仍是 `string`
  → stringly-typed 已经从 codegen 泄漏进了数据流层。

### 成本面

llvm 包非测试 **35359 行**（`stmt` 9494 / `expr` 7543 / `call` 5306 /
`call_stdlib` 4710 / `generator` 3055 / `decl` 1272）。
MIR 真正昂贵的不是"写 IR 结构"，而是**把这 35359 行的发射器从遍历 AST 改为遍历 MIR**。

### 结论：不要 big-bang，要让 MIR 从 Stage 2 里长出来

| 做法 | 判断 |
|---|---|
| 现在停工重写成 MIR | ❌ 35359 行发射器 + 零单测 + 无 IR 黄金基线 = 不可控 |
| Stage 1~4 增量重构 | ✅ 拿下 MIR 约 70% 的收益，每步都有 IR 逐字节门禁 |
| 然后补上指令层 = 完整 MIR | ✅ 到那时只剩"把字符串换成 Instr 结构"这一步 |

**关键在于 Stage 2 与 Stage 4 的落地方式**：

- **Stage 2 的 `TypeRef` 不是"为重构做准备"，它就是 MIR 的类型系统。**
  把它挂到已有的 `nodeSem` 上（而非新建结构），MIR 的地基就铺好了。
- **Stage 4 的所有权单一决策点，就是 MIR 上的 drop-elaboration pass。**
  复用已有的 `FuncCFG` + bitset 求解器，把 `freeSite.Marker` 这个死字段
  复活为真正的指令插入点，而不是继续往字符串里塞文本。

走完这两步之后，"要不要 MIR"会变成一个很小的增量决策，而不是一场赌博。

---

## 四、重构方案（分 Stage，每级独立可回滚）

### 前置条件（必做，见第五节）

**没有 IR 黄金基线和全量 e2e 跑测脚本，以下所有 Stage 都不许开工。**
这是 Stage 0 的全部内容。

---

### Stage 0｜建立回归闸门（无行为改动）

| 项 | 内容 |
|---|---|
| 目标 | 让后续每一步重构都能被机器证明"行为未变" |
| 动作 | 1. `scripts/e2e.sh`：遍历 `tests/*.no`，记录 `no build` + 运行的退出码与 stdout，产出 `baseline.json`<br>2. `scripts/ir-golden.sh`：对每个用例保存 `*_raw.ll` 与 `*_opt.ll` 到 `tests/golden/ir/`，产出 SHA256 清单<br>3. `scripts/diff-ir.sh`：重构后对比清单，**逐字节**比对，差异非空即失败<br>4. 把已知失败列进 `tests/known-fail.json` 白名单（见 MEMORY.md 的"预存 bug"清单） |
| 验收 | 350 用例跑通，基线清单入库；`diff-ir.sh` 对未修改代码库返回"零差异" |
| 风险 | 无（纯新增脚本） |
| 收益 | 把"不敢动 9461 行"变成"每次改动都有机器背书" |

---

### Stage 1｜消除噪声（纯机械，无行为改动）

| 项 | 内容 |
|---|---|
| 目标 | -400~500 行，并消除静默 else 路径 |
| 动作 | 1. **初始化全部 7 张 map**，删掉守卫"读"的 nil 检查（19+17+4 = 40 处）。保留守卫"写"的（34 处，必要）<br>2. 抽出 `func (g *Generator) isOutputParam(name string) bool`（替掉 15 处组合谓词）<br>3. 抽出 `isLocalVar` / `isGlobalVar` / `isSyntheticLet` 同类辅助<br>4. `NOLANG_DEBUG_IT` 15 次 `os.Getenv` → 包级 `sync.OnceValue` 缓存<br>5. `g.indent()` 558 处调用不变，但确认无状态副作用 |
| 验收 | IR 逐字节零差异；`go build ./...` 通过；行数下降 |
| 风险 | 低。唯一注意：删 nil 检查前必须确认 map 已在 `newGenerator` 中初始化 |

> 这一步的价值不在行数，而在于：**40 条静默 else 路径消失**，
> 后续 Stage 的分支可穷举。

---

### Stage 2｜类型表示去字符串化

| 项 | 内容 |
|---|---|
| 目标 | 消灭全包 877 处字符串类型比较，让类型分支可被编译器检查 |
| 动作 | 1. 引入 `TypeRef` 值类型（见下方代码块），`Kind` 为枚举<br>2. **挂到已有的 `parser.SemanticContext.nodeSem` 上**（扩展 `NodeSemantics`，不新建结构），逐步取代 7 张并行 map<br>3. 所有 `if llvmType == "%str-long"` → `if t.Kind == TyStr`<br>4. `switch t.Kind` 配 `default: panic("unhandled")` 做穷举检查 |
| 验收 | IR 零差异；单测覆盖 `TypeRef` 的构造/渲染/嵌套 |
| 风险 | 中。`TypeRef.LLVM` 的字符串渲染必须与旧拼写**完全一致**，否则 IR 对不上 |
| 备注 | **这一步不是"为重构做准备"，它就是 MIR 的类型系统。** 详见「三·五」 |

```go
type TyKind uint8
const (
    TyInt TyKind = iota; TyFloat; TyBool; TyPtr
    TyStr; TyVec; TyArr; TyOption; TyStruct; TyMap; TyTask; TyVoid
)

type TypeRef struct {
    Kind   TyKind
    Bits   uint8     // 整型位宽；非整型为 0
    Elem   *TypeRef  // vec/arr/option 的元素类型（取代 arrayElemTypes）
    Elem2  *TypeRef  // 嵌套容器内层（取代 elemElemTypes）
    Struct string    // 结构体名
    Size   int64     // 定长数组长度（取代 arraySizes）
}

func (t TypeRef) IsHeapOwning() bool { return t.Kind == TyStr || t.Kind == TyVec || t.Kind == TyArr || (t.Kind == TyStruct && structHasHeapFields(t.Struct)) }
```

> **关键收益**：声明 `[][]str` 从"手工同步 4 张 map"变成构造一个 `TypeRef`。
> 整类"忘写一张 map"的 bug 从结构上消失。

---

### Stage 3｜赋值语义分派表化（拆解 `generateLet`）

| 项 | 内容 |
|---|---|
| 目标 | 1891 行 → 1 个 200 行调度器 + 12 个独立 handler |
| 动作 | 1. `classifyAssign(stmt, sema) assignKind`：**纯函数**，只看 AST + Sema，不碰 Generator 状态<br>2. `assignCtx` 聚合全部上下文（目标类型/是否 out/是否全局/是否合成/源活跃度），一次性算好<br>3. `var assignHandlers map[assignKind]handler` 表驱动分派<br>4. 每个 handler 独立、可单测；禁止 handler 之间靠裸布尔传递状态 |
| 验收 | IR 零差异 + 每个 handler 至少 1 个单测（这是首次给赋值逻辑加单测） |
| 风险 | **高**。顺序耦合是真实的，必须先靠 Stage 0 的 IR 基线锁定行为 |

```go
type assignKind uint8
const (
    assignScalar assignKind = iota
    assignStructLiteral
    assignSliceLiteral
    assignSliceView      // view = arr[0..4]
    assignIndexElem      // x = vec[i]
    assignStructField    // x = s.field   ← 目前散在 7563 和 7689 两处
    assignOption         // ?T 装箱/拆箱
    assignMapLiteral
    assignArrayLiteral
    assignSyntheticIt    // match 注入的 it = matched
    assignOutParamCall   // 结果参数模式
    assignRunTask        // task = run ...
)

type assignCtx struct {
    Name      string
    DstType   TypeRef
    SrcType   TypeRef
    IsOut     bool      // isOutputParam(name)
    IsGlobal  bool
    IsSynthetic bool
    SrcLiveAfter bool   // 源在赋值后是否仍被引用 → 决定 clone/move
}
```

---

### Stage 4｜所有权策略收敛为单一决策点

| 项 | 内容 |
|---|---|
| 目标 | 12 个 free 变体 + 8 个 clone 变体 → 各 1 个按 `TyKind` 分派的函数 |
| 动作 | 1. `decideOwnership(ctx assignCtx) ownershipAction`：**唯一**回答"要不要 clone/move"的地方<br>2. `emitFree(ptr, TypeRef)` / `emitClone(dst, src, TypeRef)` 内部 `switch t.Kind` 递归<br>3. 删掉 `oldValFreed` / `alreadyCoerced` 等横穿分支的裸布尔，改为 `assignCtx` 的字段<br>4. `emitHeapFree` 退化为"对函数内所有 heap vars 调 `emitFree`"<br>5. **做成 drop-elaboration pass**：复用已有的 `FuncCFG` + bitset 求解器（三态 must/may/mustNot），把 `freeSite.Marker` 这个死字段复活为真正的插入点，而不是继续往字符串里塞文本 |
| 验收 | IR 零差异；`tests/mem-safety/` 全绿；新增所有权决策表的单测（真值表形式） |
| 风险 | 高，但这是**根治内存安全回归**的一步，26 次 fix 提交的成本都在这里 |
| 备注 | **这一步就是 MIR 上的 drop elaboration。** 详见「三·五」 |

```go
type ownershipAction uint8
const (
    ownNone ownershipAction = iota  // 标量，无需处理
    ownBorrow                       // 借用：目标不拥有，源负责释放
    ownClone                        // 深拷贝：双方各自拥有
    ownMove                         // 转移：源标记 moved，跳过释放
)

func decideOwnership(ctx assignCtx) ownershipAction {
    if !ctx.DstType.IsHeapOwning() && !ctx.SrcType.IsHeapOwning() { return ownNone }
    if ctx.IsSynthetic { return ownBorrow }              // it 借用 option 的 box
    if ctx.IsOut || ctx.IsGlobal { return ownClone }     // 逃逸出作用域
    if ctx.SrcIsGlobal { return ownClone }               // 全局源不可移动
    if !ctx.SrcLiveAfter { return ownMove }              // 源已死 → 转移
    return ownClone
}
```

> 这张表就是目前散落在 35 个分支、12 个 free 变体里的策略的**完整形式化**。
> 写成表之后，回归从"某个用例退出码 139"变成"这张真值表有个格子错了"。

---

### Stage 5｜循环骨架收敛（可选，收益递减）

| 项 | 内容 |
|---|---|
| 目标 | 5 套手写循环脚手架 → 1 个 |
| 动作 | 抽出 `emitLoopScaffold(cfg loopSpec, bodyFn func())`，统一负责<br>`for.cond/body/step/end` 标签、CFG 边、`loopExits` 压栈出栈；<br>4 个 range 变体只提供"取元素"的回调 |
| 验收 | IR 零差异（标签编号会变，需允许标签重命名白名单，比对时用规范化后的标签） |
| 风险 | 中。收益低于 Stage 1-4，**建议前四步完成后再评估是否值得** |

---

## 五、Red Lines（不可触碰）

1. **不改语法**。任何 Stage 都不得要求用户写更多代码，也不得引入新关键字。
2. **不改生成的 LLVM IR 语义**。Stage 1~4 要求**逐字节零差异**；
   仅 Stage 5 允许标签重命名，且必须先规范化再比对。
3. **不并发化跨模块类型收集**，`SourceCache` 保持无锁访问 —— 维持现有并发契约。
4. **每 Stage 独立可合入**，不得出现"重构到一半"的长期分支。
5. **AST 只允许加语法性字段**（Stage 3 里的 `IsSynthetic` 那一类），
   **禁止加 LLVM 类型字段** —— 类型走 Sema sidecar。

## 六、合并门禁（每个 Stage 都要过）

| 门禁 | 命令 / 条件 |
|---|---|
| G1 编译 | `cd src && go build ./...` |
| G2 单测 | `cd src && go test ./...` |
| G3 竞态 | `cd src && go test -race -count=3 ./build/llvm/` 零竞态 |
| G4 e2e | `scripts/e2e.sh` 与 `baseline.json` 逐条一致（白名单内失败可接受） |
| G5 IR 黄金 | `scripts/diff-ir.sh` 零差异 |
| G6 重建二进制 | `cd src && go build -o ../no ./cmd/no`（改 std 后必须重新烘焙） |

> G5 是整个方案的关键。**没有它，Stage 3/4 就是赌博；有了它，就是机械劳动。**

---

## 七、执行顺序建议

```
Stage 0 (闸门)  →  Stage 1 (降噪)  →  Stage 2 (TypeRef)  →  Stage 3 (分派表)  →  Stage 4 (所有权)
   1~2 天         0.5 天             3~5 天                5~8 天               5~8 天
                  ↑ 立即收益          ↑ Stage 3/4 的前置    ↑ 最大杠杆           ↑ 根治回归
```

- **Stage 0 必须先做**，且做完就该合入。它本身零风险，且立刻让仓库多一层保护。
- **Stage 1 可以马上做**，半天换 400 行 + 40 条静默路径，性价比最高。
- **Stage 2 是 Stage 3/4 的前置**：不先有 `TypeRef`，分派表和所有权表都只能继续写字符串比较。
- **Stage 5 建议暂缓**，等前四步落地后重新评估。

---

## 八、预期收益

| 维度 | 现在 | 目标 |
|---|---|---|
| stmt.go 行数 | 9461 | ~6000（-35%，主要来自 Stage 1/2/3） |
| `generateLet` | 1891 行 / 35 分支 | ~200 行调度器 + 12 个 handler |
| `varLLVMType` | 869 行 / 208 `if` | 并入 Sema，codegen 侧仅剩查表 |
| 字符串类型比较 | 288 处 | 0（全走 `TyKind`） |
| 并行类型 map | 7 张 | 1 张 `map[string]TypeRef` |
| 所有权决策点 | 散在 35 分支 + 20 变体 | 1 张真值表 |
| 赋值逻辑单测 | **0** | 每 handler ≥1，真值表全覆盖 |
| 重构安全性 | 靠 350 个 e2e 退出码 | IR 逐字节比对 |
