# Nolang 数据流三态分析框架

## 1. 全景概述

### 1.1 要解决什么问题

Nolang 的所有权模型要求：当局部堆变量 `x` 被 move 到输出参数 `out`（`out = x`）后，`x` 不再拥有数据，函数结束时必须**跳过** `x` 的 `free`，否则 double-free。

但 move 可能发生在 `if` 分支里：

```
foo = (x str) (res str) {
    if cond {
        res = x          // move 到 out：x 的所有权转移
    }
    // 函数结尾：x 是否应该 free？
    //   cond=true  → x 已 moved，跳过 free
    //   cond=false → x 仍拥有数据，需要 free
}
```

编译期无法确定运行时走哪条路径，因此需要一种分析框架来判断「变量在函数出口的 moved 状态」。

### 1.2 三态分类

| 三态 | 含义 | emitHeapFree 行为 |
|------|------|-------------------|
| `triMust` | 所有路径都 moved | 静态跳过 free |
| `triMay` | 部分路径 moved | 运行时 bitmap 检查 |
| `triMustNot` | 无路径 moved | 静态 free |

### 1.3 整体流程

```
┌──────────────────────────────────────────────────────┐
│ 1. 函数开始：初始化 CFG（curCFG = newFuncCFG()）        │
│    entry block 注册                                    │
├──────────────────────────────────────────────────────┤
│ 2. 函数体生成（generateStatement）：                    │
│    a. emitLabel → 注册 block + CFG 边                  │
│    b. cfgEdge / cfgTerm → 记录控制流转移               │
│    c. handleMoveToOut / handleMoveLocal / freeOldHeap  │
│       Value → cfgAddEffect 记录副作用                  │
├──────────────────────────────────────────────────────┤
│ 3. 函数体生成完毕：求解数据流                            │
│    cfgMovedFacts = solveBitsetForward(curCFG, ...)    │
├──────────────────────────────────────────────────────┤
│ 4. emitHeapFree：用三态分类做 free 决策                 │
│    triMust → 跳过                                      │
│    其他   → 回退旧逻辑（movedVarBitset / bitmap）       │
└──────────────────────────────────────────────────────┘
```

---

## 2. 数据结构

### 2.1 控制流图（CFG）

```go
type FuncCFG struct {
    Entry  string                      // entry block label
    Blocks map[string]*BasicBlock      // label → block
    Order  []string                    // 按出现顺序排列的 block label
}

type BasicBlock struct {
    Label      string          // block 标签名（如 "entry", "if.then.1"）
    Preds      []string        // 前驱 block 列表
    Succs      []string        // 后继 block 列表
    Terminator terminatorKind  // 终结符类型：termBr / termCondBr / termRet
    Effects    []effect        // 块内按顺序的数据流副作用
}
```

每个 BasicBlock 携带一个 `Effects` 列表，记录该块内发生的所有 move/reassign/bind 操作。

### 2.2 副作用（effect）

```go
type effect struct {
    Kind   effectKind  // 副作用类型
    VarIdx int         // heapVarIndex 下标（effAdd/effRemove 用）
    OutIdx int         // outputParamOrder 下标（effBind/effInit 用）
}
```

| effectKind | 含义 | 对 MovedFact 的影响 | 典型触发场景 |
|------------|------|---------------------|-------------|
| `effAdd` | 变量变为 moved（所有权转移） | `set(varIdx)` | `out = x` / `b = a`（move） |
| `effRemove` | 变量恢复 not-moved（重赋值获得新 data） | `clear(varIdx)` | `x = newval`（重赋值）/ `out` 重绑时旧变量恢复 |
| `effBind` | out 参数绑定到某 var | （MovedFact 忽略） | `out = x` |
| `effInit` | out 参数被显式赋值 | （MovedFact 忽略） | `res = 'hello'` |

### 2.3 Fact 表示

**bitsetFact**（位集 Fact，用于 MovedFact / InitFact）：

```go
type bitsetFact []uint64  // bit i 的块号 = i/64, 偏移 = i%64
```

- `set(i)` / `clear(i)` / `has(i)` — 位操作
- `meet(src)` — 交集（AND），用于 must 分析
- `join(src)` — 并集（OR），用于 may 分析

**blockFact**（每个 block 的求解结果）：

```go
type blockFact struct {
    inMeet  bitsetFact  // IN = ∩ pred OUT（must：所有前驱都满足）
    inJoin  bitsetFact  // IN = ∪ pred OUT（may：任一前驱满足）
    outMeet bitsetFact  // OUT = transfer(inMeet, effects)
    outJoin bitsetFact  // OUT = transfer(inJoin, effects)
}
```

双格设计：同时跑 meet（交集）和 join（并集），一次迭代得到 must 和 may 两个结果。

### 2.4 三态（triState）

```go
const (
    triMust    triState = iota // 所有路径成立（meet.has → must）
    triMustNot                 // 所有路径不成立（!join.has → mustNot）
    triMay                     // 部分路径成立（!meet.has && join.has → may）
)
```

分类逻辑：

```go
func classifyMoved(meet, join bitsetFact, varIdx int) triState {
    inMeet := meet.has(varIdx)   // 所有前驱都 moved？
    inJoin := join.has(varIdx)   // 任一前驱 moved？
    switch {
    case inMeet:
        return triMust    // 所有路径 moved
    case inJoin:
        return triMay     // 部分路径 moved
    default:
        return triMustNot // 无路径 moved
    }
}
```

---

## 3. CFG 构建

### 3.1 初始化

在 `generateFunctionDefinition` 中，`emitLabel(sb, "entry")` 之后：

```go
g.curCFG = newFuncCFG()
g.curCFG.Entry = "entry"
g.curCFG.getOrCreateBlock("entry")
g.cfgMovedFacts = nil
```

从此时起，所有 CFG 辅助方法（`cfgEdge`、`cfgTerm`、`cfgAddEffect` 等）开始生效。

### 3.2 Block 注册

每次 `emitLabel(sb, label)` 时自动注册 block：

```go
func (g *Generator) emitLabel(sb *strings.Builder, label string) {
    sb.WriteString(label + ":\n")
    g.currentBlock = label
    g.blockTerminated = false
    g.cfgRegisterBlock(label)  // ← 在 CFG 中创建 block
}
```

### 3.3 CFG 边记录

在 `if` 表达式、`for` 循环、`break`/`continue`、`return` 的代码生成中，通过 `cfgEdge` 和 `cfgTerm` 记录控制流转移。

#### if 表达式

```
                    ┌─────────┐
         ┌─────────►│ if.then │─────────┐
         │          └─────────┘          │
    ┌────┴───┐                           ▼
    │ cond   │                      ┌─────────┐
    │ block  │                      │ if.end  │
    └────┬───┘                      └─────────┘
         │          ┌─────────┐          ▲
         └─────────►│ if.else │──────────┘
                    └─────────┘
```

代码生成中记录：

```go
// 条件跳转：cond 块 → then / else
g.cfgEdge(condBlock, "if.then.1")
g.cfgEdge(condBlock, "if.else.1")
g.cfgTerm(condBlock, termCondBr)

// then 块 → if.end
g.cfgEdge(thenPredecessor, endLabel)
g.cfgTerm(thenPredecessor, termBr)  // 或 termRet（若 then 以 return 结尾）

// else 块 → if.end
g.cfgEdge(elsePredecessor, endLabel)
g.cfgTerm(elsePredecessor, termBr)  // 或 termRet
```

#### for 循环

```
    ┌──────────┐
    │ preheader │
    └────┬─────┘
         ▼
    ┌──────────┐
    │ for.cond │◄──────┐
    └────┬─────┘       │
    br i1│             │
    ┌────┴─────┐       │
    ▼          ▼       │
┌────────┐ ┌────────┐  │
│for.body│ │for.end │  │
└───┬────┘ └────────┘  │
    ▼                    │
┌────────┐               │
│for.step│───────────────┘
└────────┘   (回边)
```

```go
g.cfgEdge(preheaderBlock, "for.cond.1")     // preheader → cond
g.cfgEdge(condLabel, "for.body.1")          // cond → body
g.cfgEdge(condLabel, "for.end.1")           // cond → end
g.cfgEdge(bodyTail, "for.step.1")           // body → step
g.cfgEdge(stepLabel, condLabel)             // step → cond（回边）
```

#### break / continue / return

```go
// break: 当前块 → loop exit target
g.cfgEdge(curBlock, target)
g.cfgTerm(curBlock, termBr)

// continue: 当前块 → loop continue target
g.cfgEdge(curBlock, target)
g.cfgTerm(curBlock, termBr)

// return: 无后继
g.cfgTerm(g.cfgBlockLabel(), termRet)
```

### 3.4 Effect 记录

在函数体生成过程中，三个关键函数通过 `cfgAddEffect` 记录副作用：

#### handleMoveToOut — `out = x`（move 到输出参数）

```go
func (g *Generator) handleMoveToOut(sb *strings.Builder, srcName, outName string) {
    // 1. 清旧：若 out 之前绑定了别的变量 oldVar，oldVar 恢复所有权
    if oldVarIdx >= 0 {
        g.cfgAddEffect(effect{Kind: effRemove, VarIdx: oldVarIdx})  // oldVar 恢复
    }
    // 2. 设新：src 变量 moved
    g.cfgAddEffect(effect{Kind: effAdd, VarIdx: srcVarIdx})         // src moved
    g.cfgAddEffect(effect{Kind: effBind, OutIdx: outIdx, VarIdx: srcVarIdx})  // out 绑定 src
    // 3. 更新 outBindState
    g.outBindState[outIdx] = srcVarIdx
}
```

#### handleMoveLocal — `b = a`（局部间 move）

```go
func (g *Generator) handleMoveLocal(sb *strings.Builder, srcName string) {
    g.cfgAddEffect(effect{Kind: effAdd, VarIdx: srcVarIdx})  // src moved
}
```

#### freeOldHeapValue — `x = newval`（重赋值时释放旧值）

```go
func (g *Generator) freeOldHeapValue(sb *strings.Builder, stmt, name string) {
    // 释放旧值后，变量即将获得新值，清除 moved 状态
    g.cfgAddEffect(effect{Kind: effRemove, VarIdx: varIdx})  // var 恢复 not-moved
}
```

---

## 4. 数据流求解

### 4.1 传递函数（Transfer Function）

```go
func movedTransfer(in bitsetFact, effects []effect) bitsetFact {
    out := in
    for _, e := range effects {
        switch e.Kind {
        case effAdd:
            out.set(e.VarIdx)     // 变量 moved → 置位
        case effRemove:
            out.clear(e.VarIdx)   // 变量重赋值 → 清位
        }
    }
    return out
}
```

`effBind` 和 `effInit` 对 MovedFact 无影响，被忽略。

### 4.2 不动点迭代算法

```go
func solveBitsetForward(cfg *FuncCFG, nBits int, entryInit bitsetFact, transfer bitsetTransfer) map[string]*blockFact {
    // 1. 初始化所有 block 的 fact 为全 0
    // 2. entry 的 IN = entryInit（全 0）
    // 3. 工作表算法：前向迭代直至收敛
    for changed {
        for _, label := range cfg.Order {  // 按出现顺序遍历
            // a. 计算新 IN（entry 除外）
            //    newMeet = ∩ pred.outMeet
            //    newJoin = ∪ pred.outJoin
            // b. 计算 OUT = transfer(IN, effects)
            //    newOutMeet = transfer(inMeet, effects)
            //    newOutJoin = transfer(inJoin, effects)
            // c. 若 IN/OUT 变化 → 标记 changed
        }
    }
}
```

关键点：
- **meet（交集）**：多个前驱的 OUT 取交集 → must 语义（所有前驱都满足才满足）
- **join（并集）**：多个前驱的 OUT 取并集 → may 语义（任一前驱满足就满足）
- **回边**：for 循环的 `for.step → for.cond` 回边参与迭代，直到不动点收敛

### 4.3 可达性过滤

```go
func (g *Generator) computeReachableBlocks() map[string]bool {
    // BFS 从 entry 出发，计算所有可达 block
    // 排除内部 codegen 产生的孤立块（如 heapfree.skip.N）
}
```

某些内部 codegen 路径（如 `emitBitCheckFree` 生成的 `dc.free.N` / `dc.skip.N` 块）会通过 `emitLabel` 注册到 CFG，但它们的 CFG 边可能未被正确记录。这些块是孤立的（无前驱），在数据流求解中 IN 保持全 0，可能污染 meet/join 结果。`computeReachableBlocks` 确保只考虑从 entry 可达的 block。

---

## 5. 三态决策在 emitHeapFree 中的应用

### 5.1 决策流程

```go
func (g *Generator) emitHeapFree(sb *strings.Builder) {
    for _, name := range sortedHeapVars {
        // 1. 数据流优化：检查 triMust
        if g.cfgMovedFacts != nil && g.curCFG != nil {
            reachable := g.computeReachableBlocks()
            // 对所有可达 block 的 OUT 做 meet（must）和 join（may）
            allMeet := ∩ reachable.block.outMeet
            allJoin := ∪ reachable.block.outJoin
            tri := classifyMoved(allMeet, allJoin, varIdx)
            if tri == triMust {
                continue  // 静态跳过 free
            }
        }
        // 2. 回退到旧逻辑（triMay / triMustNot）
        if g.hasBranchMove && g.movedBitmapBase != "" {
            g.emitBitCheckFree(...)  // 运行时 bitmap 检查
        } else {
            if g.isMovedVar(varIdx) {
                continue              // 编译期位图：已 moved → 跳过
            }
            g.emitVarHeapFree(...)    // 未 moved → free
        }
    }
}
```

### 5.2 为什么只用 triMust 做静态优化

采用**保守兼容**策略：数据流分析只用于 `triMust`（确定安全地跳过 free），其余情况回退到旧的 `movedVarBitset` / 运行时 bitmap 逻辑。

原因：
- `triMay` 时旧逻辑的运行时 bitmap 已经能正确处理
- `triMustNot` 时旧逻辑的 `isMovedVar` 已经能正确处理
- 数据流分析作为**增量优化**，不会引入新的错误

---

## 6. 逐场景分析（含正反例）

### 场景 1：无条件 move（triMust）

**Nolang 代码：**

```nolang
foo = (x str) (res str) {
    res = x    // 无条件 move，x 在所有路径都 moved
    print(x)   // x 后续仍被引用 → 实际是 clone，不是 move
}
```

> 注意：根据活跃性分析 `moveEligible`，`x` 后续被 `print(x)` 引用，不会执行 move 而是 clone。以下用一个更准确的例子：

```nolang
foo = (s []str) (res []str) {
    res = s    // s 后续不再使用 → move
    // 函数结尾：s 已 moved → 跳过 free
}
```

**CFG：**

```
entry (effAdd: s moved)
  │
  ▼
epilogue (emitHeapFree)
```

**求解：**

| Block | IN(meet) | IN(join) | Effects | OUT(meet) | OUT(join) |
|-------|----------|----------|---------|-----------|-----------|
| entry | {} | {} | effAdd(s) | {s} | {s} |

**分类：** `classifyMoved(meet={s}, join={s}, s) = triMust` → 静态跳过 free

**反例对比 — 无 move：**

```nolang
bar = () (res str) {
    s = 'hello'
    res = s   // s 后续不再使用 → move
    // 但如果 s 是非堆变量（如 i64），不会有 move
}
```

若 `s` 是 `i64` 类型（非堆拥有），不在 `heapVarIndex` 中，`handleMoveToOut` 不会记录 effAdd。分类结果为 `triMustNot` → 静态 free（但 i64 无堆数据，`emitVarHeapFree` 是 no-op）。

---

### 场景 2：条件 move — 仅 then 分支 move（triMay）

**Nolang 代码：**

```nolang
foo = (x str, cond bool) (res str) {
    if cond {
        res = x    // move：仅在 cond=true 时发生
    }
    // 函数结尾：
    //   cond=true  → x moved → 应跳过 free
    //   cond=false → x 未 moved → 应 free
}
```

**CFG：**

```
          ┌──────────┐
     ┌───►│ if.then  │─── effAdd(x) ───┐
     │    └──────────┘                  │
┌────┴───┐                               ▼
│ entry  │                          ┌──────────┐
│ (cond) │                          │ if.end   │
└────┬───┘                          └──────────┘
     │    ┌──────────┐                  ▲
     └───►│ if.else  │──────────────────┘
          └──────────┘  (无 effect)
```

**求解：**

| Block | IN(meet) | IN(join) | Effects | OUT(meet) | OUT(join) |
|-------|----------|----------|---------|-----------|-----------|
| entry | {} | {} | - | {} | {} |
| if.then | {} | {} | effAdd(x) | {x} | {x} |
| if.else | {} | {} | - | {} | {} |
| if.end | {} (∩) | {x} (∪) | - | {} | {x} |

- `if.end` 的 `inMeet = if.then.outMeet ∩ if.else.outMeet = {x} ∩ {} = {}`
- `if.end` 的 `inJoin = if.then.outJoin ∪ if.else.outJoin = {x} ∪ {} = {x}`

**分类：** `classifyMoved(meet={}, join={x}, x)` → `!meet.has(x) && join.has(x)` → `triMay`

**处理：** 回退旧逻辑。`detectBranchMoveToOut` 检测到分支内 move → `hasBranchMove=true` → 分配运行时 bitmap → `emitBitCheckFree` 生成运行时检查 IR：

```llvm
  ; 加载 bitmap bit
  %bv = load i64, i64* %__mb0
  %masked = and i64 %bv, 1        ; varIdx=0, offset=0
  %moved = icmp ne i64 %masked, 0
  br i1 %moved, label %dc.skip, label %dc.free
dc.free:
  ; bit=0 → move 未发生 → free x
  call void @free(...)
  br label %dc.skip
dc.skip:
  ; bit=1 → move 已发生 → 跳过
```

**反例对比 — 两分支都 move（triMust）：**

```nolang
foo = (x str, cond bool) (res str) {
    if cond {
        res = x    // then 分支 move
    } else {
        res = x    // else 分支也 move
    }
    // 两分支都 move → triMust → 静态跳过 free
}
```

| Block | OUT(meet) | OUT(join) |
|-------|-----------|-----------|
| if.then | {x} | {x} |
| if.else | {x} | {x} |
| if.end | {x} (∩) | {x} (∪) |

`classifyMoved(meet={x}, join={x}, x) = triMust` → 静态跳过 free（无需运行时 bitmap）。

---

### 场景 3：move 后重赋值（triMustNot）

**Nolang 代码：**

```nolang
foo = (x str) (res str) {
    res = x       // move：x 的所有权转移
    x = 'new'     // 重赋值：x 获得新 data，恢复所有权
    // 函数结尾：x 拥有新 data → 需要 free
}
```

**CFG：**

```
entry:
  effAdd(x)      ; res = x → x moved
  effRemove(x)   ; x = 'new' → x 恢复 not-moved
  │
  ▼
epilogue (emitHeapFree)
```

**求解：**

| Block | IN(meet) | IN(join) | Effects | OUT(meet) | OUT(join) |
|-------|----------|----------|---------|-----------|-----------|
| entry | {} | {} | effAdd(x), effRemove(x) | {} | {} |

Transfer 按顺序应用：先 `set(x)`，再 `clear(x)` → 最终 `{}`。

**分类：** `classifyMoved(meet={}, join={}, x) = triMustNot` → 静态 free

**处理：** 回退旧逻辑。`isMovedVar(0)` 返回 false（`freeOldHeapValue` 中已 `unmarkMovedVar`）→ 执行 `emitVarHeapFree`。

**反例对比 — move 后未重赋值（triMust）：**

```nolang
foo = (x str) (res str) {
    res = x       // move
    // 不重赋值 x
    // 函数结尾：x 仍 moved → triMust → 跳过 free
}
```

---

### 场景 4：条件 move 后无条件重赋值（triMustNot）

**Nolang 代码：**

```nolang
foo = (x str, cond bool) (res str) {
    if cond {
        res = x    // 条件 move
    }
    x = 'new'      // 无条件重赋值
    // 函数结尾：x 在所有路径都恢复 not-moved → triMustNot → free
}
```

**CFG：**

```
          ┌──────────┐
     ┌───►│ if.then  │─── effAdd(x) ───┐
     │    └──────────┘                  │
┌────┴───┐                               ▼
│ entry  │                          ┌──────────┐
│ (cond) │                          │ if.end   │
└────┬───┘                          └────┬─────┘
     │    ┌──────────┐                   │
     └───►│ if.else  │───────────────────┘
          └──────────┘
                                           │
                                           ▼
                                    ┌──────────┐
                                    │ after    │
                                    │ effRemove│
                                    └────┬─────┘
                                         │
                                         ▼
                                    ┌──────────┐
                                    │ end      │
                                    └──────────┘
```

**求解：**

| Block | OUT(meet) | OUT(join) |
|-------|-----------|-----------|
| if.then | {x} | {x} |
| if.else | {} | {} |
| if.end | {} (∩) | {x} (∪) |
| after | transfer({}, effRemove) = {} | transfer({x}, effRemove) = {} |
| end | {} | {} |

`after` 的 IN：
- `inMeet = if.end.outMeet = {}` → transfer 后 `{}`
- `inJoin = if.end.outJoin = {x}` → transfer 后 `{}`（effRemove 清除 x）

**分类：** `classifyMoved(meet={}, join={}, x) = triMustNot` → 静态 free

**关键点：** 即使 move 是条件的（triMay），后续的无条件重赋值（effRemove）会将 meet 和 join 都清零，恢复为 triMustNot。

---

### 场景 5：循环内条件 move（triMay，含回边不动点）

**Nolang 代码：**

```nolang
foo = (x str) (res str) {
    for i = 0; i < 3; i++ {
        if i == 1 {
            res = x    // 循环内条件 move
        }
    }
    // 函数结尾：
    //   若 i==1 分支执行过 → x moved
    //   否则 → x 未 moved
    // → triMay
}
```

**CFG：**

```
┌──────────┐
│ entry    │
└────┬─────┘
     ▼
┌──────────┐◄─────────────┐
│ for.cond │              │
└────┬─────┘              │
  br i1│                    │
┌────┴─────┐              │
│          ▼              │
│     ┌──────────┐        │
│     │ for.body │        │
│     └────┬─────┘        │
│     br i1│              │
│     ┌────┴────────┐     │
│     ▼             ▼     │
│ ┌──────────┐ ┌────────┐ │
│ │loop.if.  │ │loop.if.│ │
│ │then      │ │else    │ │
│ │effAdd(x) │ │        │ │
│ └────┬─────┘ └───┬────┘ │
│      └─────┬─────┘      │
│            ▼            │
│     ┌──────────┐        │
│     │loop.if.  │        │
│     │end       │        │
│     └────┬─────┘        │
│          ▼              │
│     ┌──────────┐        │
│     │ for.step │────────┘  (回边)
│     └──────────┘
│
▼
┌──────────┐
│ for.end  │  ← emitHeapFree 在此
└──────────┘
```

**求解（不动点迭代）：**

迭代 1：
- `for.cond` IN = `entry.OUT` = `{}`
- `loop.if.then` OUT = `{x}`（effAdd）
- `loop.if.else` OUT = `{}`
- `loop.if.end` IN(meet) = `{x} ∩ {} = {}`, IN(join) = `{x} ∪ {} = {x}`
- `for.step` OUT = `{} (meet), {x} (join)`
- `for.cond` 新 IN = `entry.OUT ∩ for.step.OUT = {} ∩ {} = {} (meet), {} ∪ {x} = {x} (join)`

迭代 2：
- `for.cond` IN 变化（join 从 `{}` 变为 `{x}`）→ `changed = true`
- 重新传播...

收敛后：
- `for.end` IN(meet) = `{}`（因为 `for.cond` 的 meet 始终为 `{}`，loop.if.then 只是条件分支）
- `for.end` IN(join) = `{x}`（因为回边传播了 `x` 的 may 状态）

**分类：** `classifyMoved(meet={}, join={x}, x) = triMay` → 回退旧逻辑 → 运行时 bitmap 检查

**反例对比 — 循环内无条件 move（triMust）：**

```nolang
foo = (x str) (res str) {
    for i = 0; i < 3; i++ {
        res = x    // 每次迭代都 move（但 move 只发生一次有效，第二次 x 已 moved）
    }
    // 循环体无条件 move → triMust → 静态跳过 free
}
```

---

### 场景 6：break 边上的 move（triMay）

**Nolang 代码：**

```nolang
foo = (x str, cond bool) (res str) {
    if cond {
        res = x       // move
        // 隐式或显式 break/return 到函数尾
    }
    // else 分支不 move
    // → triMay
}
```

**CFG：**

```
          ┌──────────┐
     ┌───►│ if.then  │─── effAdd(x) ───┐
     │    │ + break  │                  │
     │    └──────────┘                  │
┌────┴───┐                               ▼
│ entry  │                          ┌──────────┐
│ (cond) │                          │ end      │
└────┬───┘                          └──────────┘
     │    ┌──────────┐                  ▲
     └───►│ if.else  │──────────────────┘
          └──────────┘  (无 effect)
```

**求解：**

| Block | OUT(meet) | OUT(join) |
|-------|-----------|-----------|
| if.then | {x} | {x} |
| if.else | {} | {} |
| end | {x} ∩ {} = {} (meet) | {x} ∪ {} = {x} (join) |

**分类：** `triMay` → 运行时 bitmap 检查

---

### 场景 7：out 参数重绑（effRemove + effAdd + effBind）

**Nolang 代码：**

```nolang
foo = (a str, b str) (res str) {
    res = a       // out 绑定 a：a moved
    res = b       // out 重绑 b：a 恢复所有权，b moved
    // 函数结尾：
    //   a 恢复 not-moved → 需要 free
    //   b moved → 跳过 free
}
```

**CFG：**

```
entry:
  effRemove(a)   ; res = b → 先清旧的 a 的 moved bit（a 恢复）
  effAdd(a)      ; res = a → a moved
  effBind(res,a) ; res 绑定 a
  
  effAdd(b)      ; res = b → b moved
  effBind(res,b) ; res 绑定 b
  
  (注意：handleMoveToOut 中先 effRemove oldVar(a)，再 effAdd newVar(b))
```

实际 effects 顺序：
1. `effAdd(a)` + `effBind(res, a)` — `res = a`
2. `effRemove(a)` + `effAdd(b)` + `effBind(res, b)` — `res = b`

**求解：**

Transfer 按顺序应用：
1. `set(a)` → `{a}`
2. `clear(a)` → `{}` — a 恢复
3. `set(b)` → `{b}` — b moved

最终 OUT(meet) = OUT(join) = `{b}`

**分类：**
- `a`: `classifyMoved(meet={b}, join={b}, a) = triMustNot` → 静态 free a
- `b`: `classifyMoved(meet={b}, join={b}, b) = triMust` → 静态跳过 b 的 free

**反例对比 — out 只绑一次：**

```nolang
bar = (a str) (res str) {
    res = a    // a moved，不重绑
    // a: triMust → 跳过 free
}
```

---

### 场景 8：无 move（triMustNot）

**Nolang 代码：**

```nolang
foo = () (res str) {
    s = 'hello'
    res = 'world'   // res 赋值字面量，非 move
    // s 从未 move → triMustNot → 静态 free s
}
```

**CFG：** 无 effAdd/effRemove effect。

**求解：** 所有 block 的 OUT(meet) = OUT(join) = `{}`

**分类：** `classifyMoved(meet={}, join={}, s) = triMustNot` → 静态 free

---

### 场景 9：move 后 return（triMust，return 块无后继）

**Nolang 代码：**

```nolang
foo = (x str, cond bool) (res str) {
    if cond {
        res = x     // move
        return      // return 块：termRet，无后继
    }
    // else 分支不 move，继续到 epilogue
    // 函数结尾 epilogue：
    //   cond=true  → 走 return 块，不到 epilogue
    //   cond=false → 走 epilogue，x 未 moved
    // → epilogue 处 x 是 triMustNot
}
```

**CFG：**

```
          ┌──────────┐
     ┌───►│ if.then  │─── effAdd(x)
     │    │ + return │   termRet（无后继）
     │    └──────────┘
┌────┴───┐
│ entry  │
│ (cond) │
└────┬───┘
     │    ┌──────────┐
     └───►│ if.else  │──────────┐
          └──────────┘          │
                                ▼
                          ┌──────────┐
                          │ if.end   │  ← epilogue 在此
                          └──────────┘
```

**求解：**

| Block | OUT(meet) | OUT(join) |
|-------|-----------|-----------|
| if.then | {x} | {x} |
| if.else | {} | {} |
| if.end | {} ∩ {} = {} (meet) | {} ∪ {} = {} (join) |

`if.then` 以 `termRet` 终结，无后继，不传播到 `if.end`。`if.end` 的前驱只有 `if.else`。

**分类：** `classifyMoved(meet={}, join={}, x) = triMustNot` → 静态 free

> 注意：`return` 路径上，`res = x` 已经 move，x 不需 free（return 后函数结束，不会执行 epilogue 的 emitHeapFree）。`if.then` 块内不会执行 emitHeapFree，所以 triMustNot 在 epilogue 是正确的。

---

### 场景 10：块内程序点（同一块内 effAdd 后 effRemove）

**Nolang 代码：**

```nolang
foo = (x str) (res str) {
    res = x       // effAdd(x)
    x = 'new'     // effRemove(x)
    // 同一 entry 块内：先 set(x) 再 clear(x)
}
```

**CFG：** 单一 entry 块，两个 effect。

**求解：**

| 程序点 | meet | join | 分类 |
|--------|------|------|------|
| PreEffects=0（effAdd 之前） | {} | {} | triMustNot |
| PreEffects=1（effAdd 之后，effRemove 之前） | {x} | {x} | triMust |
| PreEffects=2（effRemove 之后） | {} | {} | triMustNot |

`factAtPoint` 函数可以计算块内任意程序点的 fact，用于 `freeOldHeapValue` 等 free 决策点。

> 当前实现中 `emitHeapFree` 使用块 OUT（PreEffects = len(effects)），即最终状态。`factAtPoint` 主要供未来的 freeSite 回填使用。

---

### 场景 11：嵌套 if（多层分支）

**Nolang 代码：**

```nolang
foo = (x str, a bool, b bool) (res str) {
    if a {
        if b {
            res = x    // a=true, b=true → move
        }
        // a=true, b=false → 不 move
    }
    // a=false → 不 move
    // → triMay
}
```

**CFG：**

```
┌──────┐
│entry │
└──┬───┘
   │ br a
   ├──► if.then.1 ──► (br b) ──► if.then.2 (effAdd(x)) ──► if.end.2 ──► if.end.1
   │                                                          ▲
   │                          ┌──────────────┐               │
   └──────────────────────────│  if.else.2   │───────────────┘
                              └──────────────┘
   │
   ├──► if.else.1 ─────────────────────────────────────────► if.end.1
```

**求解（简化）：**

- `if.then.2` OUT = `{x}`
- `if.else.2` OUT = `{}`
- `if.end.2` IN(meet) = `{x} ∩ {} = {}`, IN(join) = `{x} ∪ {} = {x}`
- `if.end.2` OUT = `{} (meet), {x} (join)`
- `if.else.1` OUT = `{}`
- `if.end.1` IN(meet) = `if.end.2.outMeet ∩ if.else.1.outMeet = {} ∩ {} = {}`
- `if.end.1` IN(join) = `if.end.2.outJoin ∪ if.else.1.outJoin = {x} ∪ {} = {x}`

**分类：** `triMay` → 运行时 bitmap 检查

---

### 场景 12：全局变量赋值（不参与数据流分析）

**Nolang 代码：**

```nolang
gVar = 'hello'

foo = () {
    gVar = 'world'    // 全局变量重赋值
    // 全局变量不在 heapVarIndex 中 → 不参与数据流分析
}
```

全局变量不在 `heapVars` / `heapVarIndex` 中（`trackLocalHeapVar` 跳过全局变量），因此：
- `emitHeapFree` 中 `hasIdx = false` → 直接 `emitVarHeapFree`（无 moved 检查）
- 数据流分析中不出现全局变量的 effect

全局变量的释放由 `emitGlobalHeapFree` 在 `main` 函数结尾处理，不走三态分析。

---

### 场景 13：非堆拥有类型变量（不参与数据流分析）

**Nolang 代码：**

```nolang
foo = () {
    x = 42    // i64，非堆拥有类型
    // x 不在 heapVars 中 → 不参与数据流分析
}
```

`i64`、`i8`、`i1` 等纯量类型不是堆拥有类型，不会被 `trackLocalHeapVar` 追踪，不在 `heapVars` / `heapVarIndex` 中。`emitHeapFree` 跳过它们。

---

### 场景 14：clone 赋值（不记录 effAdd）

**Nolang 代码：**

```nolang
foo = (x str) {
    y = x    // x 后续仍被使用 → clone（深拷贝），非 move
    print(x)
    print(y)
    // x 未 moved → triMustNot → free x
    // y 拥有独立 data → free y
}
```

当 `moveEligible[stmt] = false`（源变量后续被引用）时，执行 `emitDeepClone` 而非 `handleMoveLocal`。clone 不转移所有权，不记录 `effAdd`。

**求解：** 无 effect → `triMustNot` → 两者都 free。

---

### 场景 15：forced move（嵌套容器无法 clone 时的退化路径）

**Nolang 代码：**

```nolang
foo = (matrix [][]i64) (res [][]i64) {
    res = matrix    // 嵌套容器无法 deep clone → forced move（浅拷贝 + 标记 moved）
}
```

当 `canClone = false`（嵌套容器等无法 deep clone）时，执行 forced move：
- 浅拷贝结构体
- 调用 `handleMoveToOut`（记录 `effAdd`）
- 标记源为 moved

**求解：** 与正常 move 相同，`effAdd(matrix)` → `triMust` → 跳过 free。

---

## 7. 与旧机制的协作

### 7.1 旧机制保留的原因

数据流分析作为**增量优化**叠加在旧机制之上，不替换旧机制：

| 机制 | 用途 | 何时使用 |
|------|------|---------|
| `movedVarBitset`（编译期位图） | 确定性 move 的编译期判断 | move 不在分支中 |
| `movedBitmapBase`（运行时 bitmap） | 分支内 move 的运行时检查 | `hasBranchMove = true` |
| `cfgMovedFacts`（数据流分析） | 三态分类优化 | 函数有局部堆变量时 |

### 7.2 detectBranchMoveToOut 的角色

`detectBranchMoveToOut` 预扫描函数体，检测是否存在分支内 move：

| 检测结果 | `hasBranchMove` | bitmap 分配 | 数据流分析 |
|---------|-----------------|-------------|-----------|
| 无 move | false | 不分配 | triMustNot → 旧逻辑 free |
| move 不在分支 | false | 不分配 | triMust → 跳过 free（优化） |
| move 在分支 | true | 分配 | triMay → 旧逻辑 bitmap 检查 |

### 7.3 emitHeapFree 中的优先级

```
1. 数据流分析 triMust → 静态跳过 free（优化）
2. hasBranchMove + bitmap → 运行时 bitmap 检查
3. movedVarBitset → 编译期位图检查
4. 以上都不命中 → 直接 free
```

数据流分析只负责第 1 步（triMust 优化），第 2-4 步完全由旧逻辑处理。

---

## 8. 边界情况与安全性

### 8.1 孤立块过滤

内部 codegen（如 `emitBitCheckFree` 的 `dc.free.N` / `dc.skip.N` 块）通过 `emitLabel` 注册到 CFG，但它们的 CFG 边可能不完整。`computeReachableBlocks` 通过 BFS 从 entry 计算可达块，排除这些孤立块，防止它们的全 0 fact 污染 meet/join。

### 8.2 nil 安全

- `factAtPoint` 对 `bf == nil`（block 不在求解结果中）和 `b == nil`（block 不在 CFG.Blocks 中）做了 nil 检查
- `emitHeapFree` 对 `exitBlock == nil` 和 `first == true`（无可达 block）做了 fallback

### 8.3 保守策略

数据流分析只用于 `triMust`（确定安全地跳过 free）。对于 `triMay` 和 `triMustNot`，完全回退到旧逻辑，确保不会因为数据流分析的不精确而导致错误地跳过 free（内存泄漏）或错误地 free 已 moved 的变量（double-free）。

---

## 9. 单元测试覆盖

| 测试 | 场景 | 预期结果 |
|------|------|---------|
| `TestMovedFactConditionalMove` | 仅 then 分支 move | triMay |
| `TestMovedFactBothBranchesMove` | 两分支都 move | triMust |
| `TestMovedFactNoMove` | 无 move | triMustNot |
| `TestMovedFactLoopConditionalMove` | 循环内条件 move | triMay |
| `TestMovedFactReassignReset` | move 后重赋值 | triMustNot |
| `TestMovedFactReassignAfterConditionalMove` | 条件 move 后无条件重赋值 | triMustNot |
| `TestMovedFactBreakEdge` | break 边上的 move | triMay |
| `TestMovedFactMidBlockPoint` | 块内 effAdd 后 effRemove | must→mustNot |
| `TestOutBindFactMeetTop` | 两分支绑不同 out | bindTop (⊤) |
| `TestOutBindFactMeetSame` | 两分支绑相同 out | 保留 |
| `TestInitFactConditional` | 条件赋值 out 参数 | may |
