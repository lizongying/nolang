---
name: nolang-memory
description: Nolang 内存设计与所有权模型参考。用于修改编译器堆释放逻辑（emitHeapFree/emitDeepContainerFree/emitStructFieldsFree/emitDeepClone/emitContainerClone/emitStructClone）、调整 move/clone 语义、修复 double-free 或内存泄漏、编写 mem-safety 测试时参考。涵盖 heapVars/movedVars/outputParamNames/arrayElemTypes/structTypes/varAlias 等追踪机制。
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
| **move** | 输出参数 `out = x`、`vec.push(x)` | 浅拷贝结构体 + 标记源为 moved；源跳过 free |

**`b = a` 判断规则**（在 `generateLet` 中）：
1. 若 a 是输出参数的源（`outputParamNames[a]` 为 true）→ move
2. 若 a 是 vec.push 的源（在 vec.push codegen 路径中）→ move
3. 否则若 a 是堆拥有型别且 `canClone` 为 true → 深層 clone
4. 否则值拷贝

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

### 3.2 movedVars
`map[string]bool`：已 move 的变量名（不应 free）。

标记 moved 的三种路径：
1. **赋值给输出参数**：`out = x`，源 `x` 是局部堆变量 → `movedVars[x] = true`
2. **vec.push 堆元素**：`outer.push(inner)`，inner 是堆拥有类型且是 Identifier → `movedVars[inner] = true`
3. **多返回值 move**：按**参数位置顺序**处理（见 §5.2）

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
  └─ emitVarHeapFree (路由：深/浅)
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
  └─ emitVarHeapFree
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

## 5. 所有权转移语义

### 5.1 单返回值 move
```nolang
get-slice = () (out []i64) {
    local = [1, 2, 3]
    out = local   ; local 标记为 moved，不 free；out 由调用者管理
}
```

### 5.2 多返回值 move（按参数位置顺序）
```nolang
get-pair = () (a []i64, b []i64) {
    x = [1, 2]
    y = [3, 4]
    a = x   ; 第一个输出参数，x 标记为 moved
    b = y   ; 第二个输出参数，y 标记为 moved
}
```

**处理顺序**：按输出参数在函数签名的声明顺序逐个处理。每个 `out = src` 赋值独立标记 `movedVars[src] = true`。

**注意**：若 `a` 和 `b` 引用同一源变量（如 `a = x; b = x`），在被调用函数内只 move 一次（x 标记 moved），a 和 b 都获得 x 的浅拷贝（共享同一 data 指针）。但在上层函数中，a 和 b 是独立的局部变量，各自被 `heapVars` 追踪为 `%vec`，函数结束时都会执行 free → **double-free**。当前 Nolang 没有引用/借用语义，b 不会自动成为 a 的别名。**用户应避免这种模式**。

### 5.3 vec.push 的隐式 move
```nolang
inner = [1, 2, 3]
outer.push(inner)
; inner 标记为 moved，data 所有权转移给 outer
; 函数结束时 inner 跳过 free，outer 深层 free 释放 inner 的 data
```

### 5.4 slice 视图的三种命运

| 目标 | 行为 | 所有权 |
|------|------|--------|
| 局部变量 `v = arr[1..3]` | 零拷贝视图 | 共享原数组 data |
| 输出参数 `out = arr[1..3]` | clone（malloc+memcpy） | 独立拥有 |
| 显式 `[]T` 类型 `v []i64 = arr[1..3]` | clone | 独立拥有 |

**原因**：输出参数逃逸到调用者，原数组可能在函数结束前被 free，视图必须 clone 为独立 data。

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
```nolang
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

位于 `tests/mem-safety/`：

| 测试文件 | 验证内容 |
|---------|---------|
| `deep-clone.no` | `b = a` 深層 clone（[]i64/[]str/str/结构体）独立性 |
| `deep-free-str.no` | `[]str` 深层 free（vec 元素为 %str-long） |
| `deep-free-nested-vec.no` | `[][]i64` 深层 free（vec 元素为 %vec，push moved 标记） |
| `deep-free-struct-vec.no` | `[]MyType` 深层 free（vec 元素为用户结构体，递归释放 name.data + items.data） |
| `struct-field-leak.no` | 结构体字段堆数据释放 |
| `slice-view-escape.no` | slice 视图赋值输出参数的 clone |
| `reassign-leak.no` | 重新赋值旧值释放 |
| `vec-push-leak.no` | vec.push 的 moved 标记 |
| `if-branch-move-leak.no` | 条件分支中的 move |
| `ffi-str-return.no` | FFI extern str 返回值安全複製（strchr NULL/非 NULL/重複/傳遞） |
| `global-heap-free.no` | 模組級堆變數在 main 退出時釋放 |

## 10. 已知未修复的问题

### 10.1 map 容器未实现深层 free
hashmap 模板（`hashmap-str-tmpl` 等）未实现 key/value 的堆数据释放。

### 10.2 循环临时变量泄漏
```nolang
loop {
    s = 'temp'   ; 每次迭代 malloc 新 data，旧 data 未释放
}
```

### 10.3 slice 视图 + 原数组 move
```nolang
view = arr[1..3]   ; view 共享 arr.data
arr = [9, 8, 7]    ; free 旧 arr.data → view 悬空
```

### 10.4 async 共享数据竞态
异步线程与主线程共享堆数据时，free 顺序不确定。

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
| **模組級堆變數釋放** | `src/build/llvm/stmt.go` | `emitGlobalHeapFree` |
| 释放路由 | `src/build/llvm/stmt.go` | `emitVarHeapFree` |
| 深层 free | `src/build/llvm/stmt.go` | `emitDeepContainerFree`, `emitElementFree` |
| 结构体释放 | `src/build/llvm/stmt.go` | `emitStructFieldsFree`, `emitStructFieldFree` |
| 重新赋值释放 | `src/build/llvm/stmt.go` | `freeOldHeapValue` |
| **深層 clone** | `src/build/llvm/stmt.go` | `emitDeepClone`, `emitContainerClone`, `emitDeepElementClone`, `emitStructElementsClone`, `emitStructClone`, `emitStructFieldClone`, `canDeepCloneStruct` |
| **`b = a` clone 路徑** | `src/build/llvm/stmt.go` | `generateLet` 中的 Identifier + heapVars 深層 clone 路徑 |
| **FFI extern str 安全複製** | `src/build/llvm/generator.go` | `emitFFIExternStrClone` |
| **FFI extern str 路徑入口** | `src/build/llvm/call.go` | `callExtern` 中的 `case "str"` |
| vec.push moved | `src/build/llvm/call.go` | vec-push case |
| varAlias | `src/build/llvm/generator.go` | `varAddr` |
| SliceLiteral 初始化 | `src/build/llvm/stmt.go` | SliceLiteral 路径 |
| 类型判断 | `src/build/llvm/generator.go` | `isHeapOwningType`, `isUserStructType` |
