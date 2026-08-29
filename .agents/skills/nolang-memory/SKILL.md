---
name: nolang-memory
description: Nolang 内存设计与所有权模型参考。用于修改编译器堆释放逻辑（emitHeapFree/emitDeepContainerFree/emitStructFieldsFree/emitDeepClone/emitContainerClone/emitStructClone）、调整 move/clone 语义、修复 double-free 或内存泄漏、编写 mem-safety 测试时参考。涵盖 heapVars/heapVarIndex/outBindState/movedVarBitset/outputParamNames/arrayElemTypes/structTypes/varAlias 等追踪机制。
---

# Nolang Memory Design

Nolang 是**无 GC** 语言，内存安全完全依赖编译器在正确位置插入 `free`。本文档描述已实现的内存设计与编译器内部追踪机制。

> **檔名規則**：Nolang 檔名使用中連字符 `-`（如 `deep-free-str.no`），不使用下劃線 `_`。測試案例放在 `tests/mem-safety/` 目錄。

## 1. 核心原则

### 1.1 单一所有权
每个堆 `data` 缓冲区**只有一个所有者**。所有权可通过 move 转移，转移后原所有者放弃 free 责任。局部变量间的 `=` 则通过深層 clone 使两个变量各自独立拥有 data。

### 1.2 三种赋值语义
`b = a` 根据上下文选择三种语义之一：

| 语义 | 触发条件 | 行为 |
|------|---------|------|
| **值拷贝** | 基本型别（i64/f64/bool 等） | 直接拷贝数值，无堆数据 |
| **深層 clone** | 局部变量间 `b = a`，a 为堆拥有型别（vec/arr/str/可克隆结构体） | malloc 新 data + memcpy + 递回 clone 元素；a 和 b 各自独立拥有 data，函数结束各自 free |
| **move** | 输出参数 `out = x` | 浅拷贝结构体 + 标记源为 moved；源跳过 free |
| **深層 clone** | `vec.push(x)`（x 为堆拥有型别） | malloc 新 data + memcpy + 递回 clone 元素；源仍拥有独立 data，函数结束各自 free |

**`b = a` 判断规则**（在 `generateLet` 中）：
1. 若 a 是输出参数的源（`outputParamNames[a]` 为 true）→ move
2. 否则若 a 是堆拥有型别且 `canClone` 为 true → 深層 clone
3. 否则值拷贝

`vec.push(x)` 不在此判断规则内：push 是方法调用，对堆拥有元素执行深層 clone（见 §5.3）。

### 1.3 编译器插入 free
- 函数结束时：`emitHeapFree` 释放所有未 moved 的局部堆变量
- main 入口 ret 前：`emitHeapFree` 释放 top-level 局部堆变量 + `emitGlobalHeapFree` 释放模組級堆變數（globalVars 中的 vec/str/arr/结构体）
- 重新赋值前：`freeOldHeapValue` 释放旧值
- 结构体字段：`emitStructFieldsFree` 递归释放

## 2. LLVM 类型布局

| Nolang 类型 | LLVM 结构体 | 字段 | 分配策略 |
|------------|------------|------|---------|
| `[]T` (vec/slice) | `%vec = { i64, i64, i64 }` | len, cap, data | malloc（堆，方便扩容+逃逸） |
| `[N]T` (arr) | `%arr = { i64, i64 }` | len, data | alloca（栈，小尺寸）或 malloc |
| `str` (long) | `%str-long = { i64, i64, i64 }` | len, cap, data | malloc（堆） |
| 用户结构体 | `%Name = { ... }` | 各字段 | alloca（栈） |

**关键**：`%arr` 只有 2 个字段（16 字节），`%vec` 有 3 个字段（24 字节）。当 `%arr` 变量被 SliceLiteral 重新赋值时，必须通过 `varAlias` 重定向到新 alloca 的 `%vec`，否则 field 2 写入越界。

## 3. 编译器追踪机制

### 3.1 heapVars
`map[string]string`：局部堆变量名 → LLVM 类型（`%vec`/`%str-long`/`%arr`/用户结构体）。

- 函数进入时初始化（`stmt.go:487`）
- 通过 `trackLocalHeapVar(name, llvmType)` 注册，**跳过参数和输出参数**
- 函数结束时 `emitHeapFree` 遍历释放

### 3.2 堆变量下标与 move 追踪（按堆变量下标索引的位图）

编译期为每个局部堆变量分配唯一 `varIdx`，运行时位图的每个 bit 对应一个堆变量（而非输出参数）。

#### 3.2.1 相关 Generator 字段

| 字段 | 类型 | 用途 |
|------|------|------|
| `heapVarIndex` | `map[string]int` | 堆变量名 → `varIdx`（仅局部堆变量，输入参数和 out 参数不编号） |
| `nextHeapVarIdx` | `int` | 下一个可用 varIdx（`trackLocalHeapVar` 递增） |
| `outputParamOrder` | `[]string` | 输出参数按宣告顺序的列表（给 `outBindState` 索引） |
| `outBindState` | `[]int` | 每个输出参数当前绑定的堆变量下标（-1=无绑定，-2=不确定） |
| `movedVarBitset` | `[]uint64` | 编译期 moved 位图（无运行时位图时用） |
| `movedBitmapBase` | `string` | 运行时位图变量名前缀（如 `%__mb`，空=未分配） |
| `bitmapCount` | `int` | u64 位图块数（= maxVarIdx/64 + 1） |
| `hasBranchMove` | `bool` | 函数是否存在分支内 move（决定是否分配运行时位图） |

#### 3.2.2 下标映射规则

一块 `u64` 存 64 个标记位，堆变量下标 `varIdx`：
- 块号 = `varIdx / 64`
- 块内偏移 = `varIdx % 64`
- 掩码 = `1u64 << 偏移`

编译期直接算常量，运行时无计算开销。多块 `u64` 可支持任意数量堆变量，**无参数/返回值数量上限**。

#### 3.2.3 move 赋值处理（覆盖会清旧 bit）

`handleMoveToOut(sb, srcName, outName)` 处理 move 到输出参数：
1. 若该输出参数之前绑定过别的变量（`outBindState[outIdx] >= 0`），先清除旧变量对应的 bit（`emitClearMovedBitIR` 运行时 / `unmarkMovedVar` 编译期）
   - **例外**：`outBindState[outIdx] == -2`（分支汇合后不确定）时不清除旧 bit — 无法确定旧绑定是哪个变量，留待运行时位图在 free 阶段判定
2. 再把当前变量对应 bit 置 1（`emitSetMovedBitIR` 运行时 / `markMovedVar` 编译期）
3. 更新该输出参数绑定的变量下标（`outBindState[outIdx] = srcVarIdx`）

`handleMoveLocal(sb, srcName)` 处理局部间 move（`b = a`，不能深拷贝时）：仅设 bit。

`vec.push` 堆元素是深拷贝（`call.go:3340-3341`），不是 move，不需要处理。

#### 3.2.4 运行时位图按需分配（`detectBranchMoveToOut` 预扫描）

```no
cond-move = (flag i64) (out []i64) {
    x = [1, 2, 3]
    if flag == 1 {
        out = x   ; move 僅在 flag==1 時發生
    }
    ; flag==0 時 x 仍擁有 data，函數結束需 free
    ; flag==1 時 x 所有權已轉移，函數結束需跳過 free
}
```

| 場景 | `hasBranchMove` | 位圖分配 | free 行為 |
|------|-----------------|---------|----------|
| 無 move | false | 不分配 | 全部 free |
| move 不在分支（確定性 move） | false | 不分配 | 編譯期 `movedVarBitset` 直接跳過 free |
| move 在分支（條件 move） | true | 分配 | 運行時位圖檢查：bit=1 跳過，bit=0 free |

编译器在 `generateFunctionDefinition` 中生成函数体之前预扫描 AST（`detectBranchMoveToOut`），递迴遍历 `IfExpression`/`ForStatement`/`ConditionalExpression` 分支结构，检测是否存在对输出参数的 move 赋值（`out = ident`）。仅在此类模式存在时才分配运行时位图变量。

**关键**：位图 `alloca` 在函数体生成**之后**插入（此时 `nextHeapVarIdx` 已为最终值），写入 entry block（body 之前）。

#### 3.2.5 函数结尾释放

`emitHeapFree` 遍历全部堆变量，按优先级依次尝试以下路径：

1. **CFG 数据流分析**（`cfgMovedFacts != nil && curCFG != nil`）：
   - 求解 MovedFact 的 `triMust`/`triMay`/`triMustNot` 三態
   - `triMust`（所有路徑 moved）→ 靜態跳過 free
   - **安全網**（`triMustNot && isMovedVar(varIdx)` → 跳過 free）：CFG 可能不完整（見 §3.2.7），編譯期 `isMovedVar` 作為兜底
   - `triMustNot` 且 AssignedFact 確認持有堆 → `emitVarHeapFreeDirect`（繞過 NULL 守衛）
   - `triMustNot` 且 AssignedFact 確認不持有堆 → 跳過 free（data 恆為 NULL）
   - `triMay` → 回退到運行時位圖 / 編譯期檢查
2. **运行时位图**（`hasBranchMove && movedBitmapBase != ""`）：`emitBitCheckFree` 生成 IR 检查 bit — `bit=1` 跳过 free，`bit=0` 走 `emitVarHeapFree`
3. **编译期检查**（無位圖）：`isMovedVar(varIdx)` — moved 则跳过，否则走 `emitVarHeapFree`

**适用所有堆类型**：`vec`/`str-long`/`arr`/用户结构体统一使用。

#### 3.2.6 分支汇合 outBindState 合并

`generateIfExpression`（`expr.go`）在 `if/else` 分支前后管理 `outBindState`：

1. **进入分支前**：保存当前 `outBindState` 快照（`savedOutBindState`）
2. **then 分支**：生成完毕后捕获 `thenOutBindState`
3. **else 分支**：恢复 `savedOutBindState` 后生成 else 分支
4. **汇合取并集**：逐项比较 `thenOutBindState[i]` 与 `g.outBindState[i]`（else 结果）
   - 相同 → 保留该值（两分支绑定一致，确定）
   - 不同 → 设为 `-2`（不确定：运行时可能是 then 绑定，也可能是 else 绑定）

`-2` 状态后续传入 `handleMoveToOut` 时触发「不清除旧 bit」例外（§3.2.3），由运行时位图在 `emitHeapFree` 阶段对每个候选变量独立判定。

#### 3.2.7 CFG 不完整性與 isMovedVar 安全網

CFG 數據流分析依賴所有內部代碼生成路徑正確註冊 CFG 邊（`cfgEdge`/`cfgTerm`）。歷史上部分路徑（如 `vec.push` 的 `vp.fast.X`/`vp.expand.X`/`vp.end.X` 塊、`emitFFIExternStrClone`、`RetCStrToStr`、`emitRetInitZeroFill`、`emitOptionDeepClone`）創建基本塊但未註冊 CFG 邊，導致這些塊在 `computeReachableBlocks` 中不可達。

**已修復（2026-08）**：現已為所有已知內部代碼生成路徑統一添加 CFG 邊（見 §10.6），CFG 數據流分析結果不再受此問題影響。

**安全網**：`emitHeapFree` 中，當 CFG 結果為 `triMustNot` 但編譯期 `isMovedVar(varIdx)` 為 true 時，信任編譯期結果並跳過 free。此安全網仍保留作為兜底保護，防止未來新增的代碼生成路徑遺漏 CFG 邊註冊。

詳細說明見 §10.6。

### 3.3 outputParamNames
`map[string]bool`：当前函数的输出参数名（由调用者管理，本函数不 free）。

### 3.4 arrayElemTypes
`map[string]string`：变量名 → 元素 LLVM 类型。用于判断是否需要深层 free。

- `isHeapOwningType(elemType)` 为 true 时，`emitDeepContainerFree` 遍历元素递归释放
- **作用域隔离**：函数进入时备份/恢复 moduleArrayElemTypes，避免模块级与函数级同名变量冲突

### 3.5 structTypes
`map[string][]structField`：结构体名 → 字段信息。`structField = { name, typ, elemType string }`。

### 3.6 varAlias
`map[string]string`：变量名 → 实际 LLVM 变量名。用于 `%arr` 重新赋值为 `%vec` 时重定向。

```go
// varAddr 检查 alias
if alias, ok := g.varAlias[name]; ok {
    name = alias
}
```

### 3.7 sliceViews
`map[string]*sliceViewInfo`：slice 视图别名（零拷贝）。视图共享原数组 data，不独立拥有。

## 4. 释放函数层次

```
emitHeapFree (函数结束)
  ├─ emitBitCheckFree (有运行时位图时 hasBranchMove && movedBitmapBase != "")
  │    └─ 检查 %__mb{block} bit：bit=1 → 跳过 free（所有权转移）；bit=0 → 走 emitVarHeapFree
  ├─ isMovedVar 编译期检查 (无运行时位图时)
  │    └─ moved → 跳过 free；否则 → 走 emitVarHeapFree
  └─ emitVarHeapFree (直接路由：深/浅)
       ├─ emitShallowDataFree (只 free data 缓冲区)
       │    └─ emitNullCheckFree (icmp eq null → br → free/skip)
       ├─ emitDeepContainerFree (遍历元素 → emitElementFree → free data)
       │    └─ emitElementFree (释放单个元素)
       │         └─ emitStructFieldsFree (递归结构体)
       └─ emitStructFieldsFree (递归结构体字段)
            └─ emitStructFieldFree (依 fieldElemType 深层/浅层)

emitGlobalHeapFree (main 入口 ret i32 0 前，釋放模組級堆變數)
  └─ emitVarHeapFree (遍歷 moduleVarTypes 中的 globalVars 堆擁有型別)

freeOldHeapValue (重新赋值前释放旧值)
  └─ isMovedVar 编译期检查 (moved 时跳过) 或 emitVarHeapFree
```

### 4.1 深层 free 触发条件
```go
if g.isHeapOwningType(elemType) {
    g.emitDeepContainerFree(sb, varPtr, llvmType, dataFieldIdx, elemType)
}
```

`isHeapOwningType` 判断：
- `%vec`、`%str-long`、`%arr` → true
- 用户结构体（`isUserStructType`）→ true
- 其他（i64、double 等）→ false（浅层 free）

### 4.2 %str-long 永远浅层 free
字符串的 data 是字符缓冲区，无嵌套堆拥有元素，直接 free data。

### 4.3 NULL 检查
所有 free 前都检查 `icmp eq i8* %ptr, null`，避免 free(NULL) 或 free 未初始化指针。

### 4.4 emitDeepContainerFree 的 CFG back edge 修正

`emitDeepContainerFree` 的循環體內調用 `emitElementFree` → `emitNullCheckFree`，後者創建新的基本塊（`heapfree.free.X` / `heapfree.skip.X`）。返回後 `g.currentBlock` 已不是 `loopBodyLabel`，而是最後一個子塊（如 `heapfree.skip.X`）。

**修復（2026-08）**：back edge 的來源必須使用 `g.cfgBlockLabel()`（實際當前 block），而非 `loopBodyLabel`。否則 CFG 中會記錄不存在的幽靈邊（`loopBodyLabel → loopCondLabel`），導致數據流求解器產生錯誤的 moved-facts。

```go
// 修正前（錯誤）：
g.cfgTerm(loopBodyLabel, termBr)
g.cfgEdge(loopBodyLabel, loopCondLabel) // 幽靈 back edge

// 修正後（正確）：
backEdgeFrom := g.cfgBlockLabel() // emitElementFree 後的實際當前 block
g.cfgTerm(backEdgeFrom, termBr)
g.cfgEdge(backEdgeFrom, loopCondLabel) // 真實 back edge
```

## 5. 所有权转移语义

### 5.1 单返回值 move
```no
get-slice = () (out []i64) {
    local = [1, 2, 3]
    out = local   ; local 标记为 moved，不 free；out 由调用者管理
}
```

### 5.2 多返回值 move（按参数位置顺序）
```no
get-pair = () (a []i64, b []i64) {
    x = [1, 2]
    y = [3, 4]
    a = x   ; 第一个输出参数，x 标记为 moved
    b = y   ; 第二个输出参数，y 标记为 moved
}
```

**处理顺序**：按输出参数在函数签名的声明顺序逐个处理。每个 `out = src` 赋值通过 `handleMoveToOut` 设置 src 对应的 bitmap bit + 更新 `outBindState`。

**注意**：若 `a` 和 `b` 引用同一源变量（如 `a = x; b = x`），在被调用函数内只 move 一次（x 标记 moved），a 和 b 都获得 x 的浅拷贝（共享同一 data 指针）。但在上层函数中，a 和 b 是独立的局部变量，各自被 `heapVars` 追踪为 `%vec`，函数结束时都会执行 free → **double-free**。当前 Nolang 没有引用/借用语义，b 不会自动成为 a 的别名。**用户应避免这种模式**。

### 5.3 vec.push 的深層 clone
```no
inner = [1, 2, 3]
outer.push(inner)
; inner 的 data 被深層 clone 到 outer 新元素位置
; inner 仍拥有独立 data，函数结束时 inner 与 outer 各自 free
```

push 对堆拥有元素型别（`%str-long`/`%vec`/`%arr`/用户结构体）执行深層 clone：malloc 新 data + memcpy + 递回 clone 元素。源变量和外部 vec 拥有各自独立的 data，**不需要 move 标记**，避免 double-free。基本型别元素（i64/f64 等）则直接 store 值。

### 5.4 slice 表達式賦值（總是 clone）

**設計變更（2026-07）**：放棄 slice view 零拷貝機制，所有 `v = arr[1..3]` 切片表達式賦值都執行完全 clone（malloc + memcpy），使目標獨立擁有 data。

| 目標 | 行為 | 所有权 |
|------|------|--------|
| 局部变量 `v = arr[1..3]` | clone（malloc+memcpy） | 独立拥有 |
| 输出参数 `out = arr[1..3]` | clone | 独立拥有 |
| 显式 `[]T` 类型 `v []i64 = arr[1..3]` | clone | 独立拥有 |

**原因**：slice 是視圖，共享原數組 data。若後續原數組被修改或釋放，視圖會懸空（use-after-free）。總是 clone 確保目標獨立擁有 data。

**實現要點**：
- `slice_view.go: generateSliceViewAssignment` 中 `needClone := true`（始終）
- `parser.go` 不再為 `SliceExpression` RHS 自動推斷 `SliceType`（避免與總是 clone 的語義衝突）
- `stmt.go: collectVarDeclsFromStmtInner` 不再為 slice view 跳過 alloca（變量需要獨立存儲空間）
- `sliceViews map` 不再被填充，舊的 `materializeSliceView` / `isSliceViewVar` 成為死代碼（保留以維持向後相容性，未來可清理）

### 5.5 slice view Identifier 路徑（保留但實際不觸發）

`out = view`（view 是切片視圖別名）的 RHS 是 Identifier 而非 SliceExpression，繞過 `generateSliceViewAssignment` 的 clone 保護。修復在 `generateLet` 中新增 Identifier 路徑檢測：

```go
if ident, ok := stmt.Value.(*parser.Identifier); ok {
    if g.isSliceViewVar(ident.Value) {
        needClone := false
        if g.outputParamNames != nil && g.outputParamNames[name] {
            needClone = true
        }
        if _, isSliceType := stmt.Type.(*parser.SliceType); isSliceType {
            needClone = true
        }
        if needClone {
            // clone view data 到目標變數
            g.emitSliceClone(sb, name, view.dataPtrReg, view.viewLen, ...)
            g.trackLocalHeapVar(name, resultType)  // 僅局部變數
            return
        }
    }
}
```

**注意**：由於 §5.4 設計變更，`sliceViews` map 不再被填充，`isSliceViewVar` 永遠返回 false，此分支實際不會觸發。保留代碼以維持向後相容性。

### 5.6 needClone 路徑的 heapVars 追蹤

`emitSliceClone` 僅寫入 len/cap/data，但不會自動追蹤為 heapVars。在 `generateSliceViewAssignment` 和 `generateChainedSliceViewClone` 的 needClone 路徑（現在始終觸發）中呼叫 `trackLocalHeapVar`，使後續 `v []i64 = view` 賦值能走深層 clone 路徑，並在函數結束時正確 free。

### 5.7 SliceType + 非 SliceLiteral RHS 的 fall-through

`v []i64 = view`（顯式 []T 型別 + Identifier RHS） formerly 走入 SliceType 預設初始化路徑（malloc cap=1024 buf, len=0），**完全忽略 RHS**，導致 v.len=0 → index out of bounds。修復：僅當 `stmt.Value == nil`（純宣告如 `v []i64`）才執行預設初始化；有 RHS 值時 fall through 到深層 clone / 一般賦值路徑。

### 5.8 generateArrayRange varAddr 全局變數修復

`for i <- a: { ... }` 中，當 `a` 是全局變數（如 `a = [1, 2, 3]` 模組級聲明）時，`generateArrayRange` 原本硬編碼 `structPtr = fmt.Sprintf("%%%s", identName)`，產生 `%a` 而非 `@a`，導致 LLVM「use of undefined value '%a'」錯誤。

**修復**：改用 `g.varAddr(identName)` 正確處理全局（`@name`）vs 局部（`%name`）變數。

```go
// 使用 varAddr 以正確處理全域變數（@name）vs 局部變數（%name）。
// 直接拼 "%%%s" 會把全域變數誤當作局部，導致 LLVM「undefined value」錯誤。
structPtr = g.varAddr(identName)
```

**測試**：`tests/vec-range.no`、`tests/arr-range.no`（全局變數 range 迭代）。

### 5.9 DotExpression base slice 表達式 clone（bug19 修復）

**原問題**：`s = t.raw[6..11)` 中 slice 表達式的 base 是 `DotExpression`（`t.raw`），不是 `Identifier`。`generateSliceViewAssignment` 只處理 `Identifier` base（§5.4），對 `DotExpression` base 返回 false，回退到 `generateSliceExpression` 路徑。該路徑只建立指向原始 data 的視圖（不 clone），當原始 data 在後續被釋放時（如結構體在函數退出時被 `emitHeapFree` 釋放），結果變數成為懸空指標（use-after-free）。

**修復**：在 `generateLet` 的 slice 表達式回退路徑中，當目標是輸出參數或局部變數時，呼叫 `cloneSliceExprResult` 執行完全 clone（malloc + memcpy）：

1. 呼叫 `generateSliceExpression` 生成臨時 slice 結果（共享原始 data 的視圖）
2. 從結果中載入 data 指標和 len
3. 呼叫 `emitSliceClone`：malloc 新緩衝區 + memcpy，寫入目標變數的 len/cap/data
4. 呼叫 `trackLocalHeapVar` 追蹤目標為堆變數（僅局部變數，非輸出參數）

**同時修復**：struct literal 賦值（`t = data { raw: raw, n: 5 }`）後，若結構體含堆擁有欄位（str/vec/arr/用戶結構體），呼叫 `trackLocalHeapVar` 追蹤為堆變數。否則 `emitHeapFree` 不會釋放結構體欄位的堆數據，導致洩漏或 use-after-free（str-range 結果指向已釋放的结构体字段 data）。

**實現位置**：
- `src/build/llvm/stmt.go` — `generateLet` 中 slice 表達式回退路徑的 clone 邏輯
- `src/build/llvm/stmt.go` — struct literal 賦值後的 `trackLocalHeapVar` 呼叫
- `src/build/llvm/clone_slice.go` — `cloneSliceExprResult` 函數實現

**測試**：`tests/mem-safety/bug19-struct-field-corruption.no`。

### 5.10 Builtin 返回 []byte 賦值到 str 變數（bug12 修復）

**原問題**：`data str = fs.read-file(path)` 中 `read-file` 返回 `%vec*` 指標（`%rf.vec.N`），但目標變數 `data` 是 `%str-long`。`varLLVMType` 的 `DotExpression` 路徑（模組前綴 builtin 呼叫，如 `fs.read-file`）只檢查了 `TypeF64` 和 `TypeStr` 返回型別，遺漏了 `SliceType`（如 `[]byte`），導致 `varLLVMType` 回退到預設 `"i64"`。`generateLet` 走入 `default` 分支，生成 `store i64 %rf.vec.N, i64* %data`——將 `%vec*` 指標當作 `i64` 存入 `%str-long*` 變數，產生 LLVM 型別錯誤。

**根因**：
1. `varLLVMType` 的 `DotExpression` 路徑（行 ~3880）缺少 `SliceType` 返回型別檢查
2. `generateLet` 的 `%str-long` case 不認識 `%vec*` 指標前綴（如 `%rf.vec.`）

**修復**：
1. 在 `varLLVMType` 的 `DotExpression` builtin 路徑中新增 `SliceType` 檢查：若 builtin 返回 `[]T`，則 `varLLVMType` 返回 `"%vec"`
2. 在 `generateLet` 的 `%str-long` case 中新增 `isVecPtrReg` 檢查：若 `val` 是 `%vec*` 指標（如 `%rf.vec.N`），先 `load %str-long, %vec* val`（`%vec` 和 `%str-long` 的 LLVM 結構體布局相同：`{i64, i64, i64}`），再 store 為 `%str-long`

**實現位置**：
- `src/build/llvm/stmt.go` — `varLLVMType` 中 `DotExpression` builtin 路徑的 `SliceType` 檢查
- `src/build/llvm/stmt.go` — `generateLet` 的 `%str-long` case 中 `isVecPtrReg` 分支
- `src/build/llvm/stmt.go` — `isVecPtrReg` 函數實現

**測試**：`tests/mem-safety/bug12-builtin-slice-to-str.no`。

### 5.11 Bool 返回值的型別強轉修復（bug13 修復）

**原問題**：`ok bool = helper.write-ref-sim(...)` 中 `write-ref-sim` 返回 `bool`。`voidSingleOutput` 路徑分配 `alloca i64` + `load i64` 回傳 `i64` 值（因 `resolveOutputParamLLVMType` 將 `i1` 映射為 `i64`）。但 `varLLVMType` 返回 `"i1"`（因 `mapToLLVMType("bool")` = `"i1"`），而 `existingType`（`varTypes[name]`）是 `"i64"`（因 `collectVarDeclsFromStmtInner` 將 `i1` 擴展為 `i64`）。型別強轉邏輯檢測到 `llvmType=i1` < `existingType=i64`，生成 `zext i1 %call.tmp.N to i64`——但 `%call.tmp.N` 實際是 `i64` 型別，LLVM 報錯。

**根因**：型別強轉邏輯假設 `llvmType` 就是 `val` 的實際型別，但 `voidSingleOutput` 路徑中 `val` 的實際型別由 `resolveOutputParamLLVMType` 決定（`i64`），不是 `varLLVMType` 返回的 `i1`。

**修復**：在型別強轉邏輯中，當 `llvmType=i1` 且 `existingType=i64` 時，先檢查 `val` 的 SSA 型別（`g.ssaTypes[val]`）。若 `val` 已經是 `i64`（來自 `voidSingleOutput`），跳過 `zext`，直接使用 `existingType` 作為儲存型別。

**實現位置**：
- `src/build/llvm/stmt.go` — `generateLet` 中型別強轉邏輯的 `i1/i64` 特殊處理

**測試**：`tests/mem-safety/bug13-bool-coercion.no`。

### 5.12 DotExpression 賦值到 out/global 的深層 clone（UAF 修復）

**原問題**：`out = c.field`（c 是結構體，field 是堆擁有型別如 `[]T`/`str`）執行淺拷貝——`generateDotExpression` 載入欄位值（`{len, cap, data}` 結構體的淺拷貝），使 `out` 與 `c.field` 共享同一 data 指標。當 `c.field` 後續被重新賦值（釋放舊 data）或 `c` 被釋放時，`out` 的 data 指標懸空 → **use-after-free**。

**根因**：`generateLet` 中 DotExpression 的深層 clone 路徑僅對局部變數生效（`isLocal && !isOutput`），輸出參數和全域變數被排除，回退到淺拷貝路徑。

**修復**：將深層 clone 條件從 `isLocal && !isOutput` 改為 `isLocal || isOutput || isGlobal`，與 `IndexExpression + DotExpression` 路徑（§7156）保持一致。輸出參數和全域變數不走 `trackLocalHeapVar`（由呼叫者 / `emitGlobalHeapFree` 管理）。

```go
// 修改前（錯誤）：
if isLocal && !isOutput {

// 修改後（正確）：
isGlobal := g.globalVars != nil && g.globalVars[name] && (g.funcLocalNames == nil || !g.funcLocalNames[name])
if isLocal || isOutput || isGlobal {
    // ... 深層 clone ...
    if !isOutput && !isGlobal {
        g.trackLocalHeapVar(name, fieldType)
    }
}
```

**實現位置**：
- `src/build/llvm/stmt.go` — `generateLet` 中 DotExpression 深層 clone 路徑

**測試**：`tests/mem-safety/struct-field-uaf-bug.no`（UAF 驗證）、`tests/mem-safety/struct-field-shallow-copy-bug.no`（別名獨立性驗證）。

## 6. 深層 clone（局部變數間賦值）

### 6.1 觸發條件
`b = a` 在 `generateLet` 中，當滿足以下所有條件時走深層 clone 路徑：
- `g.heapVars != nil` 且 `!stmt.IsSynthetic`
- RHS 是 `*parser.Identifier`（源變數 a）
- `g.heapVars[a]` 存在（a 是堆擁有變數）
- `a != b`（不是自賦值）
- `g.funcLocalNames[b]` 為 true（b 是局部變數）
- `!g.outputParamNames[b]`（b 不是輸出參數，輸出參數走 move）
- `canClone` 為 true（見 §6.3）

### 6.2 深層 clone 流程
1. `freeOldHeapValue(sb, stmt, b)`：釋放目標變數 b 的舊值
2. `emitDeepClone(sb, varAddr(a), varAddr(b), srcHeapType, srcElemType)`：
   - 容器型別（`%vec`/`%arr`/`%str-long`）：呼叫 `emitContainerClone`
   - 用戶結構體：呼叫 `emitStructClone`
3. `trackLocalHeapVar(b, srcHeapType)`：追蹤 b 為堆變數
4. 傳播 `arrayElemTypes[b] = srcElemType`（保持元素型別資訊）
5. `return`（不走後續的 `generateExprWithSB` 路徑）

### 6.3 canClone 判斷
```go
canClone := true
// 巢狀容器（vec/arr 元素為 vec/arr）不可深層 clone
if (srcHeapType == "%vec" || srcHeapType == "%arr") &&
    (srcElemType == "%vec" || srcElemType == "%arr") {
    canClone = false
}
// 用戶結構體需遞迴檢查無巢狀容器欄位
if srcHeapType != "%vec" && srcHeapType != "%arr" && srcHeapType != "%str-long" {
    if !g.canDeepCloneStruct(srcHeapType) {
        canClone = false
    }
}
```

`canDeepCloneStruct` 遞迴檢查結構體欄位：若任一欄位是「容器元素為容器」的巢狀結構，返回 false。

### 6.4 emitContainerClone 流程
1. store zeros 到 dst（清空舊值）
2. load src 的 len/cap/data
3. NULL check src.data（若為 null，dst 保持 zeros）
4. `malloc` 新 data 緩衝區（cap * elemSize）
5. `memcpy` src.data → 新 data
6. 遞迴 clone 元素：`emitDeepElementClone`
   - `%str-long` 元素：逐元素 malloc+memcpy 字串 data
   - 用戶結構體元素：`emitStructElementsClone`（memcpy 結構體 + 遞迴 clone 堆欄位）
7. 將新 data、len、cap 寫入 dst

### 6.5 emitStructClone 流程
1. `memcpy` 整個結構體從 src 到 dst
2. 遍歷欄位，對含堆數據的欄位呼叫 `emitStructFieldClone`：
   - `%vec`/`%arr`/`%str-long` 欄位：malloc+memcpy data
   - 用戶結構體欄位：遞迴 `emitStructClone`

### 6.6 與 move 的區別
- **深層 clone**：源和目標各自獨立擁有 data，函數結束各自 free
- **move**：源放棄所有權（標記 moved），目標接管 data，源跳過 free

## 7. %arr → %vec 轉换（varAlias）

### 问题
```no
local [4]i64 = [100, 200, 300, 400]   ; local 是 %arr (16 字节)
local = [100, 200, 300]                ; SliceLiteral 当作 %vec 写入 3 字段 → 越界
```

### 修复
SliceLiteral 路径检测 `varTypes[name] == "%arr"`：
1. alloca 新的 `%vec` 变量（24 字节）
2. `varAlias[name] = vecVarName`
3. 后续所有 `varAddr(name)` 重定向到新变量
4. 从 `stackArrVars` 移除

```go
if g.varTypes[name] == "%arr" {
    vecVarName := fmt.Sprintf("%s.vec.%d", name, g.tmpIdx)
    sb.WriteString(alloca %vec)
    g.varAlias[name] = vecVarName
    g.funcLocalNames[vecVarName] = true
}
```

## 8. FFI extern str 返回值安全複製

FFI extern 函數（`#{c}` 標記）返回的 C 字串指標（`i8*`）可能指向：
- **靜態記憶體**：`getenv`、`strerror`、`sqlite3_errmsg` 等
- **外部 buffer**：`strchr`/`strstr` 返回的指標指向參數內部
- **NULL**：如 `getenv` 找不到變數時

直接包裝進 `%str-long` 會在 `emitHeapFree` 時 `free()` 非堆記憶體 → UB。

### 8.1 修復機制
編譯器在 `call.go` 的 FFI extern `str` 返回路徑呼叫 `emitFFIExternStrClone`：

1. **NULL 檢查**：`icmp eq i8* %callReg, null`
   - NULL → 構造 nil `%str-long`（data=0），使 `s == nil` 成立
   - 非 NULL → 進入 copy block
2. **copy block**：`strlen` + `malloc(len+1)` + `memcpy` + null 終止
3. **PHI 合併**：`phi i64 [0, nil], [len, copy]` + `phi i8* [null, nil], [buf, copy]`
4. 構造 `%str-long` 返回

### 8.2 對比：clib RetCStrToStr 路徑
clib 路徑（`generator.go:1763`）用於內建函數（`get-env`、`get-wd`、`host-name`），邏輯與 `emitFFIExternStrClone` 完全一致。兩條路徑現在都確保 C 字串返回值擁有獨立所有權。

### 8.3 標籤唯一性
`emitFFIExternStrClone` 使用單一 `tmpIdx` 作為所有暫存器與標籤（`fstr.nil.N`/`fstr.copy.N`/`fstr.merge.N`）的後綴，確保同函數內多次呼叫時 LLVM 基本塊標籤唯一。

## 9. 已验证的测试案例

位于 `tests/mem-safety/`（共 35 個測試，全部 RC=0）：

| 测试文件 | 验证内容 |
|---------|---------|
| `deep-clone.no` | `b = a` 深層 clone（[]i64/[]str/str/结构体）独立性 |
| `deep-free-str.no` | `[]str` 深层 free（vec 元素为 %str-long） |
| `deep-free-nested-vec.no` | `[][]i64` 深层 free（vec 元素为 %vec，push 深層 clone） |
| `deep-free-struct-vec.no` | `[]MyType` 深层 free（vec 元素为用户结构体，递归释放 name.data + items.data） |
| `struct-field-leak.no` | 结构体字段堆数据释放 |
| `slice-view-escape.no` | slice 视图逃逸（out=view 输出参数 / v []i64=view 显式型别 / 多次调用不 double-free / 固定陣列视图） |
| `vec-range.no` / `arr-range.no` | 全局變數 range 迭代（`for i <- a:`，驗證 varAddr 正確用 `@a`） |
| `reassign-leak.no` | 重新赋值旧值释放 |
| `vec-push-leak.no` | vec.push 扩容时释放旧 buffer + 堆拥有元素深層 clone |
| `if-branch-move-leak.no` | 条件分支中的 move |
| `ffi-str-return.no` | FFI extern str 返回值安全複製（strchr NULL/非 NULL/重複/傳遞） |
| `global-heap-free.no` | 模組級堆變數在 main 退出時釋放 |
| `cross-fn-str-return-dfree.no` | 跨函數返回 str 的雙重釋放（main out 參數正確傳遞指標） |
| `element-assign-clone.no` | vec[i]=str/struct field=str 深層 clone + 跨函數 vec 返回 move 安全（§3.2.7 安全網） |
| `map-key-leak.no` | hashmap key/value 深層 free + move/reassign/clone 場景 |
| `map-tombstone.no` | hashmap tombstone 機制 + str/int key 釋放 |
| `loop-temp-leak.no` | 循環內臨時變數洩漏（str concat） |
| `str-concat-leak.no` | 字串拼接臨時變數洩漏 |
| `option-heap-leak.no` | option 類型堆數據釋放（str/struct/scalar option） |
| `prologue-buf-leak.no` | 輸出參數 prologue buffer 洩漏（已解決） |
| `reverse-slice-clone.no` | slice 反轉 clone 獨立性 |
| `clone-reset-is-moved.no` | clone 後重置 moved 狀態 |
| `struct-move-is-moved.no` | 結構體 move 後 isMovedVar 正確 |
| `async-str-result.no` / `async-str-stress.no` / `async-module-awy.no` / `async-shared-race.no` / `async-alloca-escape.no` | async 場景記憶體安全 |
| `test-minimal-option-str.no` / `test-minimal-str-map.no` / `test-minimal-str-map2.no` / `test-option-str-match.no` | 最小化 option/map str 場景 |
| `bug19-struct-field-corruption.no` | 局部結構體 str 字段跨函數傳遞 + str-range clone（§5.9） |
| `bug12-builtin-slice-to-str.no` | Builtin 返回 []byte 賦值到 str 變數（§5.10） |
| `bug13-bool-coercion.no` + `bug13-helper.no` | Bool 返回值的型別強轉（§5.11） |
| `struct-field-uaf-bug.no` | `out = struct.field` 淺拷貝 UAF 修復（§5.12） |
| `struct-field-shallow-copy-bug.no` | `out = struct.field` 別名共享修復（§5.12） |
| `struct-field-move-test.no` | 結構體字段 move 到 out 參數（§5.12） |

## 10. 已知未修复的问题

### 10.1 ~~map 容器未实现深层 free~~（已解決）

hashmap 模板（`hashmap-str-tmpl` 等）已實現 key/value 的堆數據釋放。透過 `collectReferencedStdModules` 正確識別 `MapType`/`MapLiteral`，確保 `collection/map.no` std 模組被載入並觸發模板實例化和 LLVM 類型定義生成。測試見 `map-key-leak.no` 和 `map-tombstone.no`。

### 10.2 ~~循环临时变量泄漏~~（已解決）

循環內臨時變數的舊值在重賦值時由 `freeOldHeapValue` 釋放。測試見 `loop-temp-leak.no` 和 `str-concat-leak.no`。

### 10.3 ~~slice 视图 + 原数组 move~~（已解決）

**原問題**：
```no
view = arr[1..3]   ; view 共享 arr.data
arr = [9, 8, 7]    ; free 旧 arr.data → view 悬空
```

**已解決（2026-07 設計變更）**：`view = arr[1..3]` 現在總是 clone（malloc+memcpy），view 獨立擁有 data，原數組釋放不影響 view。`sliceViews` map 不再被填充，`freeOldHeapValue` 無需檢查視圖映射。測試見 `slice-view-escape.no` 測試 5。

### 10.4 ~~prologue buffer 洩漏~~（已解決）

**原問題**：每個 `%vec`/`%arr` 局部變數和 `%vec`/`%arr`/`%str-long` 輸出參數在函數 prologue 階段 `malloc(256*elemSize)` 預分配 buffer，使 `buf[i] = val` 不會因 data 為 null 而崩潰。但若變數隨後被 SliceLiteral 賦值（如 `v = [1,2,3]` 或 `out = [1,2,3]`），prologue buffer 被新 buffer 覆蓋丟失，每次函數調用洩漏 256*elemSize 字節。

**根因（設計層面）**：
1. **越權替調用方處理緩衝區**：`out` 是調用方傳入的參數，緩衝區有沒有分配、是否有效應全部由調用方負責。函數 prologue 主動 malloc 兜底等於編譯器擅自接管參數生命週期。
2. **無視 out 可能已攜帶有效緩衝區**：調用方（call.go `voidSingleOutput` 路徑）已為單輸出參數 malloc 緩衝區，prologue 無腦覆蓋分配新 buffer，直接丟棄調用方傳入的原有緩衝區指針，立刻觸發洩漏。
3. **單/多輸出邏輯割裂**：舊邏輯僅對 `len(fd.Results) == 1 && fd.Results[0].Name != ""` 的單命名輸出參數預分配，多輸出參數不處理，憑空多出特殊分支。

**已解決（2026-07 設計變更）**：徹底刪除 prologue 對輸出參數的預分配邏輯，緩衝區合法性責任完全上移給調用方。
1. **刪除 stmt.go prologue 預分配分支**：移除 `if len(fd.Results) == 1 && fd.Results[0].Name != ""` 整個分支（原 `%str-long`/`%vec`/`%arr` 三條 malloc 路徑），所有輸出參數（單/多）一視同仁，prologue 不再自動 malloc 任何兜底緩衝區。
2. **call.go `voidSingleOutput` 路徑補充 `%arr` 分配**：原路徑已處理 `%str-long`/`%vec`，補充 `%arr` 分支（解析 Nolang 類型 `[N]T` 得到 arrSize 和 elemSize，`malloc(arrSize*elemSize)` 並設置 len/data），使所有容器類型輸出參數的緩衝區都由調用方統一分配。
3. **刪除 `freeOutputParamPrologueBuf` 函數及調用點**：不再有 prologue buffer 需要釋放，該函數成為死代碼已移除。

局部變數的 prologue 預分配保留（局部變數沒有「調用方」概念，prologue 分配是合理的），並在 malloc 後呼叫 `trackLocalHeapVar` 追蹤，使 `freeOldHeapValue`/`emitHeapFree` 能正確釋放。

**預分配容量優化（2026-08）**：vec 局部變數的 prologue 預分配容量從 256 降為 4。原因：
1. 若變數隨後被 SliceLiteral 賦值（如 `v = [1,2,3]`），prologue buffer 被覆蓋丟棄，每次函數調用浪費 `256*elemSize` 字節。降為 4 後浪費僅 `4*elemSize` 字節。
2. `vec.push` 的擴容策略（`cap==0→4, cap<1024→cap*2, cap>=1024→cap*5/4`）會自動處理容量增長。
3. 對 `with-cap(N)` 語法不受影響（使用者顯式指定容量）。

測試見 `prologue-buf-leak.no`（涵蓋輸出參數和局部變數兩個場景，多次調用驗證不洩漏）。`leaks` 工具確認 prologue buffer（2048 字節）被正確釋放，僅剩 16 字節基線噪聲。

### 10.5 ~~async 共享数据竞态~~（已解決）

**原問題**：異步線程與主線程共享堆數據時，參數指針共享同一 data 緩衝區，子線程讀寫時調用端可能同時修改或釋放它，無同步保護 → 數據競爭（未定義行為）。此外，未 awy 的 task 容器在函數返回前被跳過釋放（泄漏但無 UAF）。

**根因**：
1. `prepareAsyncCall` 中 Identifier + 堆擁有類型參數直接傳遞調用方的 data 指針給子線程，無所有權隔離。
2. `emitLocalTasksFree` 對未完成（`done=false`）的 task 選擇「泄漏但不 UAF」策略——跳過釋放所有容器。
3. `awaitTaskVar` 和 wrapper 不釋放參數容器（cloneBuf），作為已知泄漏。

**修復（2026-08）**：

1. **參數所有權隔離**（`expr.go: prepareAsyncCall`）：對 Identifier + 堆擁有類型參數執行深拷貝（`emitDeepClone`），子線程不共享調用方的 data 指針。標量參數也拷貝到獨立堆緩衝區（`async.argscalar`）。所有參數最終通過 `allocForCoro`（malloc）分配獨立緩衝區。

2. **wrapper 參數容器釋放**（`expr.go: prepareAsyncCall` wrapper 生成邏輯）：wrapper 在目標函數執行後，釋放每個參數的容器（`free(cloneBuf)` / `free(async.argscalar)`），僅釋放容器本身（如 `%vec` 的 24 字節），不釋放 data（data 可能被目標函數 move 到 out 參數/result buffer，由 result buffer 所有者管理）。

3. **emitLocalTasksFree 改進**（`stmt.go: emitLocalTasksFree`）：
   - `done=false` 的 task：同步調用 `resume_fn(task)` 驅動到完成（與 `awaitTaskVar` not_done 路徑一致），然後釋放 args struct 和 result buffer 容器。
   - `done=true` 的 task：直接釋放 args struct 和 result buffer 容器。
   - **不釋放 task 結構體本身**：task 通過 `@nolang_async_enqueue` 入隊後，就緒隊列 `@nolang_ready_q` 仍持有 task 指針。釋放 task 會導致事件循環調度到該 task 時讀取已釋放內存（UAF）。task 結構體（24 字節）作為已知泄漏保留——事件循環取出 task 後，wrapper 檢查 `done=true` 直接返回，`done_handler` 調用 `nolang_async_done`（無等待者時直接返回），安全跳過。

**安全分析**：
- 單線程事件循環模型下，`run` 只是把 task 入隊，不會真正並發執行。task 在事件循環調度到時才執行。
- `emitLocalTasksFree` 同步驅動未完成 task 到完成是安全的——`resume_fn` 執行完畢後設置 `done=true`。
- 釋放 args 和 result 容器不影響就緒隊列（隊列只持有 task 指針，不直接訪問 args/result）。
- wrapper 釋放參數容器（cloneBuf）不影響 data——data 被 move 到 result buffer 後由其所有者管理。

**殘留泄漏**：task 結構體本身（24 字節）在事件循環場景下泄漏（因就緒隊列持有指針）。在非事件循環場景下（無 `@nolang_async_run`），就緒隊列不會被消費，task 結構體也泄漏。這是保守的安全選擇。

測試見 `async-shared-race.no`（參數所有權隔離 + 未 awy task 清理 + str 參數深拷貝）。

### 10.5a ~~main 函數 out 參數 UB 崩潰~~（已解決）

**原問題**：`main = () (out i64) { ... }` 時，`@main` 調用 `_nolang_main()` 缺少輸出參數指標，導致 LLVM UB。`-O3` 優化器將 `_nolang_main` 推斷為 `noreturn`，刪除全局變數釋放和 `ret i32 0`，程序在 `_nolang_main` 返回後立即崩潰（rc=133 SIGTRAP）。

**根因**：`generateMainFunction` 中調用 `_nolang_main()` 時未傳遞輸出參數。`_nolang_main` 的簽名是 `void @_nolang_main()`（無參數），但實際函數體中有 `store i64 0, ptr %out`（out 是參數指標）。缺少參數時 LLVM 將其視為 `null`（UB），`store i64 0, ptr null → unreachable`。

**修復（2026-08）**：`generateMainFunction` 中為每個 `main` 的輸出參數分配棧空間（`alloca` + `store zeroinitializer`），並將指標傳遞給 `_nolang_main()`：

```go
mainArgs := []string{}
if g.funcResultLLVMType != nil {
    if retTypes, ok := g.funcResultLLVMType["main"]; ok && len(retTypes) > 0 {
        for _, rt := range retTypes {
            g.tmpIdx++
            tmpName := fmt.Sprintf("%%main.out.%d", g.tmpIdx)
            sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpName, toLLVMType(rt)))
            sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), toLLVMType(rt), toLLVMType(rt), tmpName))
            mainArgs = append(mainArgs, toLLVMType(rt)+"* "+tmpName)
        }
    }
}
```

測試見 `cross-fn-str-return-dfree.no`。

## 10.6 CFG 數據流分析與內部代碼生成路徑的交互問題

### 問題描述

CFG 數據流分析依賴 `cfgEdge`/`cfgTerm`/`cfgAddEffect` 正確記錄所有基本塊和邊。但部分內部代碼生成路徑（如 `vec.push` 的 `vp.fast.X`/`vp.expand.X`/`vp.end.X` 塊）創建基本塊但未註冊 CFG 邊，導致這些塊在 `computeReachableBlocks` 中不可達。

當 `handleMoveToOut` 在這些不可達塊中記錄 `effAdd`（moved）effect 時，數據流求解器無法看到該 effect，將變數誤判為 `triMustNot`（從未 moved），進而走直接 free 路路徑，繞過編譯期 `isMovedVar` 檢查，導致雙重釋放。

### 已修復的路径

| 路径 | 修复方式 |
|------|---------|
| `emitNullCheckFree` | 正確記錄 CFG 邊：`fromBlock → freeLabel`, `fromBlock → skipLabel`, `freeLabel → skipLabel` |
| `emitDeepContainerFree` | back edge 使用 `cfgBlockLabel()`（實際當前 block）而非 `loopBodyLabel`（§4.4） |
| `emitOptionHeapFree` | 正確記錄 CFG 邊（與 `emitNullCheckFree` 同模式） |
| `vec.push`（`call.go`） | 為 `vp.fast.N`/`vp.expand.N`/`vp.end.N` 塊註冊 CFG 邊：條件分支 `fromBlock → fastLabel`/`expandLabel`，各自 `→ endLabel`（2026-08 修復） |
| `emitFFIExternStrClone`（`generator.go`） | 為 `fstr.nil.N`/`fstr.copy.N`/`fstr.merge.N` 塊註冊 CFG 邊（2026-08 修復） |
| `RetCStrToStr`（`generator.go`） | 為 `cstr.nil.N`/`cstr.copy.N`/`cstr.merge.N` 塊註冊 CFG 邊，改用 `emitLabel` 替代手動 `sb.WriteString(label)`（2026-08 修復） |
| `emitRetInitZeroFill`（`stmt.go`） | 為 `ri.zf.fill.N`/`ri.zf.skip.N` 塊註冊 CFG 邊（2026-08 修復） |
| `emitOptionDeepClone`（`stmt.go`） | 為 `optclone.do.N`/`optclone.skip.N`/`optclone.deep.N` 塊註冊 CFG 邊，包括內部 NULL check 條件分支（2026-08 修復） |

### 安全網機制

`emitHeapFree` 中新增編譯期 `isMovedVar` 安全網（§3.2.5）：當 CFG 結果為 `triMustNot` 但編譯期 `isMovedVar` 表示已 moved 時，信任編譯期結果並跳過 free。這解決了 CFG 不完整導致的誤判，作為兜底保護。

### 修復原則

所有內部代碼生成路徑（創建基本塊的函數）統一遵循以下模式：
1. 條件分支前：記錄 `fromBlock = g.cfgBlockLabel()`
2. 寫 `br` 指令後：`g.cfgTerm(fromBlock, termCondBr/termBr)` + `g.cfgEdge(fromBlock, target1)` + `g.cfgEdge(fromBlock, target2)`
3. `emitLabel` 後的塊結尾 `br`：`g.cfgTerm(currentLabel, termBr)` + `g.cfgEdge(currentLabel, nextLabel)`
4. 若塊內調用了可能創建子塊的函數（如 `emitDeepClone`/`emitNullCheckFree`），終結符的 fromBlock 應使用 `g.cfgBlockLabel()`（實際當前塊），而非固定的 label

現已為所有已知內部代碼生成路徑統一添加 CFG 邊，安全網機制仍保留作為兜底保護。

## 11. 修改堆释放逻辑的检查清单

修改 `stmt.go`/`call.go`/`generator.go` 中的堆释放逻辑后：

1. **运行 mem-safety 测试**：
   ```bash
   for f in tests/mem-safety/*.no; do ./bin/no run "$f"; done
   ```

2. **运行 slice/vec/struct 回归**：
   ```bash
   ./bin/no run tests/slice1.no tests/struct.no tests/vec.no tests/move.no
   ```

3. **运行 no vet**：
   ```bash
   cd src/std && ../../bin/no vet
   ```

4. **运行 Go 测试**：
   ```bash
   cd src && go test ./build/llvm/... ./parser/... ./fmt/...
   ```

5. **新增 mem-safety 测试**：新场景的测试放在 `tests/mem-safety/`，文件名用中連字符。

## 12. 核心文件位置

| 功能 | 文件 | 关键函数 |
|------|------|---------|
| 堆变量追踪 | `src/build/llvm/stmt.go` | `trackLocalHeapVar`, `emitHeapFree` |
| **move 追踪（按堆变量下标索引）** | `src/build/llvm/stmt.go` | `handleMoveToOut`, `handleMoveLocal`, `emitBitCheckFree`, `isMovedVar` |
| **编译期位图操作** | `src/build/llvm/stmt.go` | `markMovedVar`, `unmarkMovedVar`, `isMovedVar` |
| **运行时位图 IR** | `src/build/llvm/stmt.go` | `emitSetMovedBitIR`, `emitClearMovedBitIR`, `emitBitCheckFree` |
| **分支 move 预扫描** | `src/build/llvm/stmt.go` | `detectBranchMoveToOut` — 递迴遍历 AST 检测分支内 move |
| **位图变量按需分配** | `src/build/llvm/stmt.go` | `generateFunctionDefinition` 中 `hasBranchMove` 为 true 时 alloca `%__mb{block}` |
| **CFG 數據流分析** | `src/build/llvm/dataflow.go` | `FuncCFG`, `solveBitsetForward`, `movedTransfer`, `classifyMoved`, `computeReachableBlocks` |
| **CFG 安全網** | `src/build/llvm/stmt.go` | `emitHeapFree` 中 `triMustNot && isMovedVar` 檢查（§3.2.5, §10.6） |
| **back edge 修正** | `src/build/llvm/stmt.go` | `emitDeepContainerFree` 中 `backEdgeFrom = g.cfgBlockLabel()`（§4.4） |
| **main out 參數傳遞** | `src/build/llvm/stmt.go` | `generateMainFunction` 中為 main out 參數分配棧空間並傳遞指標（§10.5a） |
| **hashmap std 模組載入** | `src/build/transpiler.go` | `collectReferencedStdModules` 識別 `MapType`/`MapLiteral`（§10.1） |
| **模組級堆變數釋放** | `src/build/llvm/stmt.go` | `emitGlobalHeapFree` |
| 释放路由 | `src/build/llvm/stmt.go` | `emitVarHeapFree` |
| 深层 free | `src/build/llvm/stmt.go` | `emitDeepContainerFree`, `emitElementFree` |
| 结构体释放 | `src/build/llvm/stmt.go` | `emitStructFieldsFree`, `emitStructFieldFree` |
| 重新赋值释放 | `src/build/llvm/stmt.go` | `freeOldHeapValue` |
| **深層 clone** | `src/build/llvm/stmt.go` | `emitDeepClone`, `emitContainerClone`, `emitDeepElementClone`, `emitStructElementsClone`, `emitStructClone`, `emitStructFieldClone`, `canDeepCloneStruct` |
| **`b = a` clone 路徑** | `src/build/llvm/stmt.go` | `generateLet` 中的 Identifier + heapVars 深層 clone 路徑 |
| **slice view Identifier clone** | `src/build/llvm/stmt.go` | `generateLet` 中的 `isSliceViewVar` + needClone 路徑（§5.5，保留但不觸發） |
| **slice 總是 clone** | `src/build/llvm/slice_view.go` | `generateSliceViewAssignment` needClone 始終 true；`emitSliceClone`、`generateChainedSliceViewClone` 的 `trackLocalHeapVar`（§5.4, §5.6） |
| **SliceType fall-through** | `src/build/llvm/stmt.go` | SliceType 區塊僅 `stmt.Value == nil` 時預設初始化（§5.7） |
| **range 迭代全局變數** | `src/build/llvm/stmt.go` | `generateArrayRange` 中 `structPtr = g.varAddr(identName)`（§5.8） |
| slice 視圖註冊/克隆 | `src/build/llvm/slice_view.go` | `generateSliceViewAssignment`, `emitSliceClone`, `materializeSliceView`（部分為死代碼） |
| **DotExpression slice clone** | `src/build/llvm/clone_slice.go` | `cloneSliceExprResult`：對 base 是 DotExpression 的 slice 表達式執行 clone（§5.9） |
| **struct literal 堆追蹤** | `src/build/llvm/stmt.go` | struct literal 賦值後 `trackLocalHeapVar` 追蹤含堆欄位的結構體（§5.9） |
| **FFI extern str 安全複製** | `src/build/llvm/generator.go` | `emitFFIExternStrClone` |
| **FFI extern str 路徑入口** | `src/build/llvm/call.go` | `callExtern` 中的 `case "str"` |
| vec.push 深層 clone | `src/build/llvm/call.go` | vec-push case（`emitDeepClone` for heap-owning elements，扩容时 `emitNullCheckFree` 旧 buffer） |
| varAlias | `src/build/llvm/generator.go` | `varAddr` |
| SliceLiteral 初始化 | `src/build/llvm/stmt.go` | SliceLiteral 路径 |
| 类型判断 | `src/build/llvm/generator.go` | `isHeapOwningType`, `isUserStructType` |
| **async 參數所有權隔離** | `src/build/llvm/expr.go` | `prepareAsyncCall` 中 Identifier + 堆擁有類型參數深拷貝（§10.5） |
| **async wrapper 參數容器釋放** | `src/build/llvm/expr.go` | `prepareAsyncCall` wrapper 生成邏輯中 `for i := range argTypes { free }`（§10.5） |
| **async task 清理** | `src/build/llvm/stmt.go` | `emitLocalTasksFree`, `trackLocalTask`, `untrackLocalTask`（§10.5） |
| **async task await** | `src/build/llvm/expr.go` | `awaitTaskVar`, `awaitFutureCall`, `awaitFutureVar` |
| **Builtin []byte → str 賦值** | `src/build/llvm/stmt.go` | `varLLVMType` DotExpression builtin SliceType 檢查 + `isVecPtrReg` + `%str-long` case load 分支（§5.10） |
| **Bool 型別強轉** | `src/build/llvm/stmt.go` | `generateLet` 中 `i1/i64` 型別強轉的 SSA 型別檢查（§5.11） |
| **跨模組全局變量所有權** | `src/build/transpiler.go`, `src/build/llvm/generator.go` | `collectReassignedGlobals`, `collectReassignedGlobalNames`, `SetGlobalVarOwners`, `scanGlobalReassigns`（§12） |
| **跨模組全局變量 codegen** | `src/build/llvm/stmt.go`, `src/build/llvm/expr.go` | `collectVarDeclsFromStmtInner` 模組歸屬判斷, `generateDotExpression` module.VAR 解析（§12） |

## 12. 跨模組全局變量

### 12.1 問題背景

Nolang 模組（`# /path/to/module`）可以定義全局變量（如 `COUNTER = 0`）。其他模組的函數可以通過 `module.VAR` 語法讀取這些變量，同模組的函數可以直接用裸名 `VAR` 讀寫。

**歷史 bug**：跨模組全局變量不共享狀態——模組函數內的賦值（如 `COUNTER = COUNTER + 1`）被誤當作局部變量，創建了 `alloca` 而非寫入 `@COUNTER`，導致全局變量值永遠為初始值。

### 12.2 修復設計

修復分為三個層面：

1. **常量傳播排除**（`src/build/transpiler.go`）
   - `collectReassignedGlobals` 掃描合併後的程序（含函數體），找出 `Type==nil` 的 LetStatement（賦值）
   - 遞迴進入 `FunctionLiteral` body（`inc = () { COUNTER = COUNTER + 1 }`）
   - 從 `moduleConstants` 中刪除這些可變全局變量，避免被常量傳播替換為初始值
   - **必須在第一次 `ResolveModuleConstants` 之前執行**

2. **codegen 常量摺疊排除**（`src/build/llvm/generator.go`）
   - `collectReassignedGlobalNames` 在 `Generate` 方法早期掃描，收集被重新賦值的變量名
   - 從 `enumVariantIndex` 和 `moduleIntConsts` 中排除這些變量
   - 避免 `generateIdentifier` 將 `COUNTER` 常量摺疊為 `0`

3. **模組歸屬追蹤**（`src/build/transpiler.go` + `src/build/llvm/generator.go`）
   - `globalVarOwner`: 全局變量名 → 模組短名
   - `funcOwner`: 函數名 → 模組短名
   - `SetGlobalVarOwners` 在 `Generate` 之前設定
   - `scanGlobalReassigns` 使用這些映射判斷函數與全局變量是否來自同一模組
   - `collectVarDeclsFromStmtInner` 使用這些映射決定是否寫入全局 `@name`

### 12.3 別名機制

Nolang 的 `#` 導入語句支援別名：
```
# /path/to/module.VAR ALIAS   ; 導入 module 的全局變量 VAR，重命名為 ALIAS
```

這可用於解決跨模組變量名衝突。導入後可直接使用 `ALIAS` 訪問，無需模組前綴。

## See Also — Nolang References

- [nolang-syntax](file://../nolang-syntax/SKILL.md) — Nolang syntax, grammar, types, operators, and language features
- [nolang-std](file://../nolang-std/SKILL.md) — Standard library API reference (60+ modules)
- [nolang-build](file://../nolang-build/SKILL.md) — Building the Nolang project with `make`
- [nolang-debug](file://../nolang-debug/SKILL.md) — Debugging guide for compiler and LSP issues
