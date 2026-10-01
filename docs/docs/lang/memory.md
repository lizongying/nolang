---
sidebar_position: 6
---

# 記憶體管理

Nolang 是**無 GC** 語言，記憶體安全由編譯器自動插入 `free` 保證。本文描述已實現的記憶體設計與所有權語義。

## 核心原則

### 單一所有權
每個堆 `data` 緩衝區**只有一個所有者**。所有權可透過 move 轉移，轉移後原所有者放棄 free 責任。局部變數間的 `=` 則透過深層 clone 使兩個變數各自獨立擁有 data。

### 三種賦值語義
`b = a` 根據上下文選擇三種語義之一：

| 語義 | 觸發條件 | 行為 |
|------|---------|------|
| **值拷貝** | 基本型別（i64/f64/bool 等）且不滿足 can_slot_rebind | 直接拷貝數值，無堆數據 |
| **棧槽重綁定** | 局部變數間 `b = a`，a 為棧類型（i64/u64/i128/u128/txt）且滿足 can_slot_rebind | `g.varAlias[b] = a`，b 與 a 共享同一棧槽，0 拷貝（優於值拷貝）；否則降級為值拷貝 |
| **深層 clone** | 局部變數間 `b = a`，a 為堆擁有型別（vec/arr/str/可克隆結構體） | malloc 新 data + memcpy + 遞迴 clone 元素；a 和 b 各自獨立擁有 data，函數結束各自 free |
| **move** | 輸出參數 `out = x` | 淺拷貝結構體 + 標記源為 moved；源跳過 free |
| **深層 clone** | `vec.push(x)`（x 為堆擁有型別） | malloc 新 data + memcpy + 遞迴 clone 元素；源仍擁有獨立 data，函數結束各自 free |

## 棧類型 move（棧槽重綁定 / slot-rebind）

`i64/u64/i128/u128/txt` 雖然都是**棧類型**（值直接存在 alloca 棧槽，無堆 data），但 `b = a` 在語義上仍預設走「值拷貝」（把 a 的棧值 memcpy 到 b 的棧槽）。為減少不必要的拷貝，編譯器對滿足 **can_slot_rebind** 約束的棧類型 `b = a` 執行**棧槽重綁定**：令 `g.varAlias[b] = a`，使 b 與 a 共享同一棧槽（0 拷貝，優於值拷貝）。

### can_slot_rebind 約束（保守正確）
重綁定後 b 與 a 指向同一棧槽，因此 a 後續任何對 b 的讀都會讀到「a 的存儲」。為避免語義錯誤，重綁定僅在源 a **後續「完全無引用（讀或寫）」**時允許；否則**降級為完整值拷貝**（語義不變，僅多一次 memcpy）。

- 安全性由 `computeSlotRebindSafety`（主函數）/`computeMoveEligibility`（用戶函數）透過 `stmtContainsVarRefAny` / `exprContainsVarRefAny` 靜態求解：掃描 move 之後的所有語句（含分支/迴圈回邊），若源變數仍被任何方式引用則 `slotRebindSafe[stmt] = false` → 走拷貝。
- 引用掃描必須窮舉**所有能引用變數的 AST 節點**，否則未覆蓋的寫引用會導致不安全的重綁定（別名槽被污染 → 錯誤輸出甚至無限迴圈）。已覆蓋：語句層 `LetStatement` / `ExpressionStatement` / `ForStatement`（含 `Init`/`Update`/`Condition`/`CountExpr`/`Body` 與 **`IterRange` 迭代集合**）/ `ReturnStatement` / `MultiAssignStatement` / **`UnwrapAssignStatement`（`?=` 解包賦值）** / **`BlockStatement`（裸區塊）**；表達式層 `Identifier` / `AssignExpression` / `Infix` / `Prefix` / `Call` / `Dot` / `Index` / `IfExpression`（含分支體）/ `Slice` / `Conditional` / `Grouped` / **`AwaitExpression`** / **`CastExpression`** / **`RangeExpression`** / **`RunExpression`（協程 spawn，防禦性；整函數禁用仍由 `curHasUnsafeConstruct` 負責）**。任何新增的變數引用構造都必須同步加入這兩個掃描函數。
- `match` 已 desugar 為 `IfExpression`，其分支體內的引用由上述分析遍歷覆蓋，故 **match 不觸發禁用**（經 `tests/match.no` / `tests/option.no` 驗證與基線一致）。

### 禁用場景（一律降級為值拷貝）
| 禁用條件 | 原因 |
|---------|------|
| 目標為輸出參數 / 全域變數 / 堆類型變數 | 輸出參數由指標傳遞、全域跨函數可見、堆類型需深層 free，重綁定破壞所有權 |
| 源為參數 / 全域變數 | 參數按引用傳遞，重綁定會破壞呼叫方棧幀 |
| **stdlib 函數**（`curIsStdLib`，由 `SetStdModules` 判定） | stdlib 內部 alias 綁定（如 fmt 的 `it` 別名到局部 `n`）與引用分析難以完全建模 |
| **用戶函數體含閉包（`FunctionLiteral`）或協程 spawn（`RunExpression`）**（`curHasUnsafeConstruct`，由 `bodyHasUnsafeConstruct` 遞歸掃描） | 閉包捕獲變數在「獨立函數上下文」求值，協程跨線程執行，二者變數生命週期超出當前函數的 `g.varAlias` 單函數別名作用域，重綁定會破壞共享棧槽 |

> 設計取捨：閉包/協程採「整函數禁用」而非「逐變數精算」，因為單函數別名分析本質上無法建模跨函數/跨線程的存儲共享。整函數禁用只損失優化（降級為拷貝），絕不引入錯誤，符合「保守正確」原則。

### 別名失效（alias invalidation）
- 目標被重新賦值時，先 `delete(g.varAlias, name)` 清掉殘留舊別名，否則通用賦值路徑 `varAddr(name)` 仍指向舊源棧槽，污染舊源。
- 重綁定執行傳遞性解析：沿 `g.varAlias` 鏈找到最終源棧槽（`a → c → …`），避免多跳別名錯位；並同步 `g.varTypes[name]` 使後續型別查詢一致。

### 與堆類型 move 的正交性
棧槽重綁定只作用在棧類型 `b = a` 的**值拷貝**語義上，與堆擁有型別的「深層 clone / move（輸出參數）」完全正交；堆類型路徑不受 `slotRebindSafe` 影響。

### 測試參考
- `tests/slot-rebind.no`：i64/u64 重綁定、i128/u128 經 `==` 比較、txt 經 `.len-bytes()`、降級拷貝（`da` 複用 → `db`/`dc` 拷貝）、重賦值後（`ra`=10）、參數 move（`pm_fn` 源為參數 → 拷貝）、重綁定後重賦值（`rr_fn` → `rr`=99）、消費（`consume(m)` → `cm`=84）。期望輸出 `42 7 1 1 17 5 5 10 100 99 84`。
- `tests/slot-rebind-unsafe.no`：協程（`run`/`awy`）capture 棧變數，驗證 `curHasUnsafeConstruct` 禁用重綁定後輸出與基線一致。

### 編譯器插入 free
- 函數結束時：釋放所有未 moved 的局部堆變數
- 重新賦值前：釋放舊值
- 結構體欄位：遞迴釋放含堆數據的欄位

## 型別佈局

| Nolang 型別 | 記憶體結構 | 欄位 | 分配策略 |
|------------|-----------|------|---------|
| `[]T`（切片） | 24 字節 | len, cap, data | malloc（堆） |
| `[N]T`（固定陣列） | 16 字節 | len, data | alloca（棧）或 malloc |
| `str`（長字串） | 24 字節 | len, cap, data | malloc（堆） |
| 結構體 | 各欄位總和 | 各欄位 | alloca（棧） |

## 淺層 free 與深層 free

### 淺層 free
只釋放容器的 data 緩衝區，不遍歷元素。適用於：
- `%str-long`（字串 data 是字符緩衝區，無嵌套堆擁有元素）
- 元素為基本型別（i64、double 等）的 vec/arr

### 深層 free
先遍歷每個元素遞迴釋放其堆數據，再 free 容器的 data 緩衝區。適用於元素為堆擁有型別的 vec/arr：
- `[]str`（元素為 %str-long）
- `[][]i64`（元素為 %vec）
- `[]MyType`（元素為用戶結構體，遞迴釋放欄位）

### NULL 檢查
所有 free 前都檢查 `icmp eq i8* %ptr, null`，避免 free(NULL) 或 free 未初始化指標。

## 所有权转移（move）

### 單返回值 move
```no
get-slice = () (out []i64) {
    local = [1, 2, 3]
    out = local   ; local 標記為 moved，函數結束不 free；out 由呼叫者管理
}

v = get-slice()  ; v 擁有 data，函數結束時 free
```

### 多返回值 move（按參數位置順序）
```no
get-pair = () (a []i64, b []i64) {
    x = [1, 2]
    y = [3, 4]
    a = x   ; 第一個輸出參數，x 標記為 moved
    b = y   ; 第二個輸出參數，y 標記為 moved
}

a, b = get-pair()  ; a 擁有 x 的 data，b 擁有 y 的 data
```

**處理順序**：按輸出參數在函數簽名的**宣告順序**逐個處理。每個 `out = src` 賦值獨立標記源變數為 moved。

**同一源變數多次賦值**：若 `a` 和 `b` 引用同一源變數（如 `a = x; b = x`），編譯器透過 **Liveness 預分析**（`moveEligible`）自動決定 clone 或 move：

- `a = x`：x 在後續被 `b = x` 引用 → **深層 clone**，a 獨立擁有 data
- `b = x`：x 在後續未再被引用 → **move**，b 接管 x 的 data，x 標記為 moved 跳過 free

這避免了 double-free。規則：**最後一次引用走 move，之前的引用走 clone**。

條件分支場景（如 `if cond { out = x }`）透過運行時位圖追蹤 moved 狀態，見下節。

### vec.push 的深層 clone
```no
inner = [1, 2, 3]
outer.push(inner)
; inner 的 data 被深層 clone 到 outer 新元素位置
; inner 仍擁有獨立 data，函數結束時 inner 與 outer 各自 free
```

push 對堆擁有元素型別（`%str-long`/`%vec`/`%arr`/用戶結構體）執行深層 clone：malloc 新 data + memcpy + 遞迴 clone 元素。源變數和外部 vec 擁有各自獨立的 data，**不需要 move 標記**，避免 double-free。基本型別元素（i64/f64 等）則直接 store 值。

### 運行時 move 追蹤（按堆變數下標索引的位圖）

條件分支下的 move 帶來一個挑戰：編譯期無法確定某個 move 是否真的發生。

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

Nolang 採用**按堆變數下標索引的位圖**解決此問題。編譯期為每個局部堆變數分配唯一 `varIdx`，運行時位圖的每個 bit 對應一個堆變數（而非輸出參數）。

#### 編譯器狀態

| 欄位 | 類型 | 用途 |
|------|------|------|
| `heapVarIndex` | `map[string]int` | 堆變數名 → `varIdx`（僅局部堆變數） |
| `outBindState` | `[]int` | 每個輸出參數當前綁定的堆變數下標（-1=無綁定，-2=不確定） |
| `movedVarBitset` | `[]uint64` | 編譯期 moved 位圖（無運行時位圖時用） |
| `movedBitmapBase` | `string` | 運行時位圖變數名前綴（如 `%__mb`，空=未分配） |
| `bitmapCount` | `int` | u64 位圖塊數（= maxVarIdx/64 + 1） |

#### 下標映射規則

一塊 `u64` 存 64 個標記位，堆變數下標 `varIdx`：

- 塊號 = `varIdx / 64`
- 塊內偏移 = `varIdx % 64`
- 掩碼 = `1u64 << 偏移`

編譯期直接算常量，運行時無計算開銷。多塊 `u64` 可支援任意數量堆變數，**無參數/返回值數量上限**。

#### move 賦值處理（覆蓋會清舊 bit）

每次把堆變數 move 給輸出參數：

1. 若該輸出參數之前綁定過別的變數（`outBindState[outIdx] >= 0`），先清除舊變數對應的 bit
2. 再把當前變數對應 bit 置 1
3. 更新該輸出參數綁定的變數下標（`outBindState[outIdx] = srcVarIdx`）

#### 函數結尾釋放

遍歷全部堆變數，平鋪獨立 `if`：對應 bit 為 0 就 free，bit 為 1 代表所有權移走，跳過釋放。

#### 位圖按需分配

位圖變數僅在**必要時**分配，避免無分支場景的效能開銷：

| 場景 | 位圖分配 | free 行為 |
|------|---------|----------|
| 無 move | 不分配 | 全部 free |
| move 不在分支（確定性 move） | 不分配 | 編譯期 `movedVarBitset` 直接跳過 free |
| move 在分支（條件 move） | 分配 | 運行時位圖檢查：bit=1 跳過，bit=0 free |

編譯器在生成函數體之前預掃描 AST（`detectBranchMoveToOut`），檢測是否存在 `IfExpression`/`ForStatement`/`ConditionalExpression` 分支內對輸出參數的 move 賦值，僅在此類模式存在時才分配運行時位圖變數。位圖 `alloca` 在函數體生成之後插入（此時 `nextHeapVarIdx` 已為最終值），寫入 entry block。

此機制適用於所有堆類型（`vec`/`str-long`/`arr`/用戶結構體）。

## 深層 clone（局部變數間賦值）

```no
a []i64 = [10, 20, 30]
b = a          ; 深層 clone：malloc 新 data + memcpy + 遞迴 clone 元素
b[0] = 99
; a[0] == 10（a 不受影響）
; b[0] == 99（b 獨立修改）
```

### 深層 clone 流程
1. 釋放目標變數的舊值（若已有堆數據）
2. `malloc` 新 data 緩衝區，`memcpy` 源 data 到新 data
3. 遞迴 clone 每個堆擁有元素：
   - `%str-long` 元素：malloc + memcpy 字串 data
   - 用戶結構體元素：memcpy 結構體 + 遞迴 clone 含堆數據的欄位
4. 將新 data 指標、len、cap 寫入目標變數
5. 追蹤目標為堆變數（函數結束時 free）

### 可克隆的型別
| 型別 | 可深層 clone | 說明 |
|------|-------------|------|
| `%vec` / `%arr`（元素為基本型別） | ✅ | memcpy data 即可 |
| `%vec` / `%arr`（元素為 %str-long） | ✅ | 逐元素 malloc+memcpy 字串 data |
| `%vec` / `%arr`（元素為可克隆結構體） | ✅ | 逐元素遞迴 clone 結構體欄位 |
| `%vec` / `%arr`（元素為 %vec / %arr） | ✅ | 透過 `elemElemType` 機制遞迴 clone 內層容器元素 |
| `%str-long` | ✅ | malloc + memcpy 字串 data |
| 用戶結構體（無巢狀容器欄位） | ✅ | memcpy 結構體 + 遞迴 clone 堆欄位 |
| 用戶結構體（含巢狀容器欄位） | ✅ | memcpy 結構體 + 遞迴 clone 含巢狀容器的欄位（透過 `elemElemType`） |

### 與 move 的區別
- **深層 clone**：源和目標各自獨立擁有 data，函數結束各自 free
- **move**：源放棄所有權（標記 moved），目標接管 data，源跳過 free

`b = a` 的判斷規則：
1. 若 a 是輸出參數的源 → move
2. 否則若 a 是堆擁有型別且可深層 clone → 深層 clone
3. 否則值拷貝

`vec.push(x)` 不在此判斷規則內：push 是方法調用，不論 x 是否堆擁有型別，都對堆擁有元素執行深層 clone（見前節）。

## 棧類型 move（棧槽重綁定 / slot-rebind）

`i64`/`u64`/`i128`/`u128`/`txt` 雖是棧類型（非堆擁有），但 `b = a`（RHS 為變數）在源 `a` 後續無引用時，不再做值拷貝，而是執行**棧槽重綁定**（stack-slot rebind）：令 `g.varAlias[b] = a`，使 `b` 與 `a` 共享同一棧槽（0 拷貝）。這優於值拷貝（值拷貝仍要 emit load+store），對大棧類型（如 256 位元組的 `%txt`）收益尤其明顯。

### can_slot_rebind 約束（保守正確性）

棧槽重綁定比堆 move 的 `moveEligible` **更嚴格**：

- `moveEligible`：源 `a` 後續**未讀**即允許 move（適用於堆類型，move 後源跳過 free，不影響目標）。
- `can_slot_rebind`（`slotRebindSafe`）：源 `a` 後續**無任何引用（讀或寫）**才允許重綁定。因為重綁定後 `b` 與 `a` 共享同一棧槽，若 `a` 後續被寫會破壞 `b` 的值。

計算：`generateFunctionDefinition` 呼叫 `computeMoveEligibility`（同時填充 `moveEligible` 與 `slotRebindSafe`）；`generateMainFunction` 呼叫 `computeSlotRebindSafety`（**僅**填充 `slotRebindSafe`，不觸碰 `moveEligible`——HEAD 的主函數不啟用堆 move，保持關閉以免 SEGFAULT）。兩者均透過 `stmtContainsVarRefAny` 做分支/迴圈感知的引用掃描（含 `AssignExpression` 左值、迴圈回邊）。

當 `can_slot_rebind` 不滿足（源後續仍有引用）→ **降級為完整值拷貝**，絕不退化為不安全行為。

### 禁用場景（必須走普通賦值路徑）

| 場景 | 原因 |
|------|------|
| stdlib 函數（`curIsStdLib`） | stdlib 含 match/closure/coroutine 等引用分析無法完全建模的構造，重綁定會破壞共享棧槽（如 fmt 內部 `it` 被別名綁定到局部 `n`）。檢測：`g.stdModules`（由 `transpiler.go` `SetStdModules` 從 `checker.KnownStdModules()` 注入），回退 `g.funcOwner[fd.Name]` |
| 目標是輸出參數 | 輸出參數由呼叫方傳指標，重綁定使其指向局部源棧槽，呼叫方讀不到 |
| 目標是全域變數 | 重綁定使全域名解析到局部源棧槽，破壞全域語義 |
| 目標是堆類型變數 | 棧槽重綁定僅適用於棧類型 |
| 源是參數 | 參數按引用傳遞，重綁定破壞呼叫方棧幀 |
| 源是全域變數 | 全域位址被別名到局部源，語義錯誤 |

### 重新賦值後別名失效

變數被重新賦值時（`b = ...`），先 `delete(g.varAlias, name)` 清除可能殘留的舊別名，避免通用賦值路徑經 `varAddr(name)` 仍指向舊源棧槽、污染舊源。設定新別名時沿別名鏈做**傳遞性解析**（`src := ident.Value; for { if n2,ok := g.varAlias[src]; ok { src = n2 } else break }`），找到最終源棧槽，避免多跳別名錯位。

### 與堆 move 的正交性

棧槽重綁定僅作用於棧類型，不涉及所有權轉移或 `free`，與堆類型的 clone/move 機制完全獨立。

**測試**：`tests/slot-rebind.no`（覆蓋 i64/u64/i128/u128/txt 重綁定、降級為 copy、目標重賦值後別名失效、參數 move 降級、consume 傳參，期望輸出 `42 7 1 1 17 5 5 10 100 99 84`）。

## FFI extern str 返回值

FFI extern 函數（`#{c}` 標記）返回的 C 字串指標（`i8*`）可能指向靜態記憶體（如 `getenv`、`strerror`）或外部 buffer（如 `strchr` 返回的指標指向參數內部）。直接包裝進 `%str-long` 會在 `emitHeapFree` 時 `free()` 非堆記憶體 → UB。

編譯器在 FFI extern `str` 返回路徑插入安全複製：

1. **NULL 檢查**：若 C 返回 NULL，構造 nil `%str-long`（data=0），使 `s == nil` 成立
2. **非 NULL**：`strlen` + `malloc` + `memcpy` + null 終止，複製到獨立堆緩衝區
3. **PHI 合併**：兩條路徑合併，構造 `%str-long` 返回

```no
#{c}
strchr = (s str, c i64) (r str)

find = () (r str) {
    r = strchr('hello', 108)   ; C 返回指向 'hello' 內部的指標
    ; 編譯器自動 malloc+memcpy 複製，r 獨立擁有 data
    ; 函數結束時 emitHeapFree 安全釋放 r.data
}
```

此機制與 clib `RetCStrToStr` 路徑（用於 `get-env`、`get-wd` 等內建函數）邏輯一致，確保所有 C 字串返回值都擁有獨立所有權。

## 模組級變數釋放

模組級堆變數（`vec`/`str`/`arr`/結構體）編譯為 LLVM global（`@name`），其 `data` 緩衝區在 `main` 入口的 top-level 語句中 malloc 初始化。

編譯器在 C 入口 `main` 的 `ret i32 0` 前調用：
1. `emitHeapFree` — 釋放 top-level 局部堆變數（非 globalVars）
2. `emitGlobalHeapFree` — 遍歷 `moduleVarTypes`，釋放所有 `globalVars` 中的堆擁有型別

```no
GLOBAL-STR = 'hello'      ; LLVM @GLOBAL-STR = global %str-long zeroinitializer
GLOBAL-VEC = [1, 2, 3]    ; LLVM @GLOBAL-VEC = global %vec zeroinitializer
; top-level 語句 malloc data 並存入 global
; main ret 前 emitGlobalHeapFree 釋放 data
```

這避免了長期運行服務（如帶循環的 daemon）的記憶體累積泄漏。對一次性 CLI 工具無影響（進程退出由 OS 回收）。

## 切片視圖

切片表達式 `arr[1..3]` 產生視圖（零拷貝），共享原數組 data。視圖的三種命運：

| 目標 | 行為 | 所有權 |
|------|------|--------|
| 局部變數 `v = arr[1..3]` | 零拷貝視圖 | 共享原數組 data |
| 輸出參數 `out = arr[1..3]` | clone（malloc+memcpy） | 獨立擁有 |
| 顯式 `[]T` 型別 `v []i64 = arr[1..3]` | clone | 獨立擁有 |

**原因**：輸出參數逃逸到呼叫者，原數組可能在函數結束前被 free，視圖必須 clone 為獨立 data。

## 重新賦值與舊值釋放

```no
s = 'hello'     ; malloc data 緩衝區
s = 'world'     ; 釋放 'hello' 的 data，malloc 新 data
```

重新賦值堆擁有型別時，編譯器在賦值前自動釋放舊值的 data，避免泄漏。

## 結構體欄位釋放

```no
Node {
    name str
    items []i64
}

n = Node{
    name: 'hello'
    items: [1, 2, 3]
}
; 函數結束時遞迴釋放：
;   - n.name.data（%str-long 欄位）
;   - n.items.data（%vec 欄位）
```

結構體釋放時遍歷所有欄位，對堆擁有型別欄位遞迴釋放其 data。

## 固定陣列重新賦值為切片

```no
local [4]i64 = [100, 200, 300, 400]   ; local 是固定陣列（16 字節）
local = [100, 200, 300]                ; 重新賦值為切片（24 字節）
```

固定陣列（`%arr`，2 欄位）與切片（`%vec`，3 欄位）記憶體佈局不同。重新賦值時編譯器自動分配新的 `%vec` 變數並重定向所有後續存取，避免緩衝區越界。

## 已驗證的測試案例

測試位於 `tests/mem-safety/`：

| 測試 | 驗證內容 |
|------|---------|
| `deep-clone.no` | `b = a` 深層 clone（[]i64/[]str/str/結構體）獨立性 |
| `deep-free-str.no` | `[]str` 深層 free |
| `deep-free-nested-vec.no` | `[][]i64` 深層 free + push 深層 clone |
| `deep-free-struct-vec.no` | `[]MyType` 深層 free（遞迴結構體） |
| `struct-field-leak.no` | 結構體欄位堆數據釋放 |
| `slice-view-escape.no` | 切片視圖賦值輸出參數的 clone |
| `reassign-leak.no` | 重新賦值舊值釋放 |
| `vec-push-leak.no` | vec.push 擴容時釋放舊 buffer + 堆擁有元素深層 clone |
| `ffi-str-return.no` | FFI extern str 返回值安全複製 |
| `global-heap-free.no` | 模組級堆變數在 main 退出時釋放 |
| `double-move-same-source.no` | `a=x; b=x` 同源多賦值：首次 clone + 末次 move |
| `move-clone-liveness.no` | Liveness 預分析決定 clone/move（含條件分支、三賦值） |

## 已知限制

### map 容器
hashmap 未實現 key/value 的深層 free，map 容器的堆數據會泄漏。

### 循環臨時變數
```no
loop {
    s = 'temp'   ; 每次迭代 malloc 新 data，舊 data 未釋放
}
```

### 切片視圖 + 原數組 move
```no
view = arr[1..3]   ; view 共享 arr.data
arr = [9, 8, 7]    ; 釋放舊 arr.data → view 懸空
```

### async 共享數據（2026-09-30 已修）

`run`/`awy` 的 spawn 邊界現在會**深拷貝**擁有堆的引數，任務不再與呼叫方共享 buffer。

協程是協作式、單執行緒的，所以「跨協程共享可變堆數據」在語言層不成立；唯一會共享的
是**呼叫方在 spawn 之後仍然持有**的那份值。修復前 argbuf 只是引數描述符的**位元複製**，
`data` 與呼叫方共享，而任務要等到 `awy` 才執行 —— 呼叫方在那之前重賦值或離開作用域就會
把 buffer 釋放掉，任務於是讀到已釋放的記憶體（實測：str 印出 40 個 NUL、`[]i64` 印出 0，
兩者 rc 都是 0 且無任何診斷）。

現在 spawn 當下就深拷貝，wrapper 在呼叫結束後釋放副本：

| 參數型別 | spawn 邊界 | wrapper 釋放 |
|---|---|---|
| `str` | `@str_clone` | `@str_free` |
| `[]T`（T 有 deep-clone helper） | `vecDeepClone` | `@vec_free` |
| 擁有堆的 struct（內聯 `str` 葉子／指標欄位） | 就地葉子 clone ＋ pointee 複製 | 遞迴結構體解構子 |
| 其他（純值型別） | 位元複製（正確） | 只釋放容器 |

另外兩個已修的邊界缺陷：同一 handle `awy` 兩次（修復前 SIGSEGV，現在是已定義的 no-op
並輸出診斷到 fd 2）、以及被取消的任務不再洩漏 argbuf。

迴歸：`tests/async-ownership.no`。

### struct 重賦值洩漏（2026-09-30 已修）

```no
holder { s str }
h holder = holder { s: 'first' }
h = holder { s: 'second' }   ; 舊的 'first' buffer 曾洩漏
```

重綁定一個區域變數時，舊值只有在其型別是 `Type.Owned`（`str`/`vec`/`[]T`/`map`/`?owned`）
才會被釋放。struct **刻意不算** `Type.Owned`（該標記同時是 lowerer 判斷「綁定是否為別名」
的依據），所以 `h = holder { … }` 直接覆寫整個結構體，舊的 `s` buffer 永遠不會被釋放。
但 drop 機制其實是認得 struct 的（`dropOwnsHeap` → `typeOwnsHeap`，且 `emitDrop` 用遞迴
解構子釋放），只有 lowerer 的閘門太窄。修復後重綁定前會先釋放舊欄位。

實測 2,000,000 次重賦值：峰值 RSS 66.1 MB → 33.8 MB，輸出逐位元組相同。

### struct literal 首欄位值是方法呼叫（2026-09-30 已修）

```no
holder { s str }
h = holder { s: n.to-str() }   ; 修復前：'holder' is not defined
```

`{ … }` 的歸類是靠向前看幾顆 token 決定的。match 的**臂分隔符也是 `:`**，所以
`s: n.to-str()` 同時長得像「pattern 為 `s` 的 match 臂」與「struct literal 的欄位」。
兩者的唯一消歧符是 `ident.ident` **之後**那顆 token —— 方法呼叫只可能是欄位值。
舊的分支漏了 `(`，於是整塊被判成 match，struct 名稱在 match 語境下找不到而報未定義
（欄位值是一般函式呼叫則正常）。

修復後 `h = holder { s: recv.method(args) }` 正常解析。nolang 的 match 臂不可能以
`name : X(` 開頭（臂分隔符是 `->`，`:` 開頭的臂必為 wildcard/default，body 不會帶括號呼叫），
所以放行 `(` 不會把任何合法的 match 誤判成 struct literal。迴歸：
`src/parser/struct_literal_field_value_test.go`。

### await 結果型別跨 handle 重賦值（2026-09-30 已修）

```no
holder { s str }
echo-holder-async = (h holder) (r str) { r = h.s }
t i64 = 0
t = run echo-holder-async(h)   ; 重賦值，不是新綁定
print(awy t)                   ; 修復前印出 40（str 的長度），rc 仍是 0
```

任務的結果型別記在以 MIR value id 為鍵的表裡。`awy <handle-var>` 是透過**區域變數當前的
value id** 去解析 handle 的，而那是 OpRun 結果被 move 進去的**新槽**，不是 OpRun 結果本身的
id —— 重賦值路徑沒有把型別搬過去，於是 await 被當成無型別的 `i64`，`str` 結果的 buffer 被
重新解讀，印出來的就是長度。

修復要三處同時到位，缺一都會留下症狀：

1. 降低階段在兩條 rebind 路徑（區域變數、模組級全局）把結果型別搬到目標槽；
   來源不是 handle 時**清除**目標槽的記錄，避免後續 `awy` 撿到上一個任務的陳舊型別。
2. 檢查器對 `*AwaitExpression` 不再一律回 `i64`：`awy <call>` 回 callee 的回傳型別，
   `awy <handle-var>` 回「未知」而跳過型別檢查（handle 是不透明的 `i64`，靜態無法還原）。
   舊行為會拒收完全合法的 `v str = awy t`。
3. 轉譯層的字串判定把 `*AwaitExpression` 視為可能是字串，否則合法的 `v str = awy t`
   會被「cannot assign non-string value to string variable」擋下。

迴歸：`tests/async-ownership.no` 的 test 9 / test 10。

### handle 複製的引用計數漏計：void 任務與 `run <handle>`（2026-10-01 已修）

協程 spawn 是唯一的引用計數邊界，所以 `run` 產生的 `%task` 是語言裡唯一的 R 檔物件。紀律是
**一個引用一個計數**：`run` 建立時 `rc = 1`，handle 的**每次複製** `retain`，每次 `awy` `release`。
有兩條路徑的複製沒有計數，因為「這個值是不是 handle」被誤判成「這個值有沒有已知的**任務結果型別**」：

| 形狀 | 修復前 | 修復後 |
|---|---|---|
| `h = run void-async(); h2 = h; awy h; awy h2` | **SIGSEGV（rc=139）** | 正常結束 |
| `h = run dbl(21); h2 = run h; awy h; awy h2` | 印出 `42 0` | 印出 `42 42` |

第一條是**結果型別為 void 的任務**：它有 handle，但沒有結果型別，於是複製不 retain，`rc` 停在 1。
第一次 `awy` 就把任務釋放掉，第二個引用仍指著它，第二次 `awy` 對已釋放的塊再 `release` 一次。
同一個形狀在任務回傳 `i64` 時完全正確 —— 這個**不對稱**就是病灶。

第二條是 `run <handle>`：它**不是** spawn 站點（運算元已經是 handle），而是「同一任務的第二個
引用」，所以必須跟 `h2 = h` 一樣 retain。修復前它直接回傳運算元、不建新值也不 retain，兩次 `awy`
因此落在同一個槽上，第一次把槽歸零後第二次讀到 0。

修法是把 **handle 的「身分」與任務的「結果型別」分開記**（`asyncHandles` 對上 `asyncResTypes`），
`run <handle>` 走與 `h2 = h` 相同的計數路徑。新增的 `checkRefBalance` 驗證器（MIR `Analyze` 內）
會把「有別名卻沒有 retain」變成**編譯錯誤**，而不是等到執行期才崩。

迴歸：`tests/async-rc.no`（修復前 rc=139）、`src/mir/async_rc_handle_test.go` 的
`TestVoidTaskHandleCopyRetains` / `TestRunHandleForwardCreatesFreshReference` /
`TestCheckRefBalanceFiresOnMissingRetain`。

### spawn 參數的移動：來源已死時零拷貝（2026-10-01）

`run f(x)` 對每個**擁有堆**的參數會在 spawn 邊界做一次深拷貝，因為任務可能到 `awy` 才執行，
而呼叫端在那之前可以重賦值或離開作用域（見下方「spawn 後重賦值參數」那條）。當 x 在 spawn 之後
**確定不再被使用**時，那次拷貝是多餘的：所有權直接**轉移**進任務。

```
s str = 'hello'
t = run echo-async(s)     ; s 之後不再被讀 -> 零拷貝轉移
print(awy t)              ; hello

s str = 'hello'
t = run echo-async(s)
print(s)                  ; s 仍被讀 -> 維持深拷貝（兩邊各自擁有）
print(awy t)              ; hello
```

判準三個，全部必要：

1. 呼叫端**本來就會**釋放 x —— 借用（切片視圖、borrow read）與參數都不會被呼叫端釋放，
   把它們轉移進任務等於讓任務去釋放別人的緩衝區；
2. x 在 spawn 之後已死（不是「活到區塊結束」，而是**沒有任何後續讀取**）；
3. 被呼叫端該位置的參數是 `str` / `[]T` / 擁有堆的 `struct` —— 也就是任務的 wrapper
   會負責釋放 payload 的那三類。其餘型別（`map`、owning tagged enum、元素不可深拷貝的
   切片）即使擁有堆也**不會**被轉移，因為 wrapper 只釋放容器、不釋放內容。

**為什麼這樣是安全的**：對那三類，任務 wrapper 的釋放與呼叫端原本的 drop **降成同一支 helper**
（`@str_free` / `@vec_free` / 該 struct 的解構子）。所以轉移只是把**同一次釋放**從呼叫端搬到
任務，不改變釋放了什麼——既不多釋放（double free），也不少釋放（洩漏）。

**不變的使用者語義**：從未被 `awy` 的任務，它的參數緩衝區本來就會洩漏（釋放它的是任務的
wrapper，而 wrapper 只在 await 時執行）。移動與拷貝在這點上**完全相同**，所以移動不會把
「一次拷貝」換成「一筆洩漏」。

實作：`insertDrops` 判定並記進 `Module.spawnArgMoves`，`emitAsyncRun` 據此跳過深拷貝，
`checkDropCount` 據此豁免 `missing-drop`。量測開關 `NOLANG_MIR_SPAWN_ARG_MOVE=1`。
迴歸：`tests/async-arg-move.no`、`src/mir/spawn_arg_move_test.go`。

### 協程等待者表別名（2026-09-30 已修）

排程器原本用 `@nolang_waiters = global [256 x i8*]`，以 `ptrtoint(task) & 255` 當索引。
task 由 `@malloc` 配置、**16 位元組對齊**，指標低位只有 4 個有效位 —— 256 個槽實際塌縮成
16 個（實測只有 8 個會被用到），兩個並發等待的任務極容易互相覆寫等待者、喚醒錯誤的任務。

現在整張表移除，等待者存進 `%task` 自己的第 5 個欄位（`%task` 由 24 位元組增為
32 位元組）：`@nolang_async_wait` 把當前任務寫進**被等待者**的欄位，
`@nolang_async_done` 讀該欄位、非空則入隊後清空。查詢因此是精確的，不再有位址別名。

> 註：MIR 後端目前是同步驅動 await，從不呼叫 `@nolang_async_run`／`@nolang_async_wait`，
> 所以這一條在今天是**潛伏**缺陷而非可觀測故障。迴歸：
> `src/mir/async_boundary_ownership_test.go`。

### `run` / `awy` 不是區塊第一條陳述時被吃掉（2026-09-30 已修）

`skipToStatementEnd()` 在每條陳述解析完後一路前進到「能**開始**下一條陳述」的 token 才停，
而這份白名單 `isStatementBoundary()`（`src/parser/stmt.go`）漏了 `run` 與 `awy`。兩者都是
**前綴關鍵字**（只能開啟一個表達式，`parseStatement` 沒有它們的 case ⇒ 落到
`parseExpressionStatement`），確實能開始一條陳述，因此必須在白名單裡。`NEWLINE` 刻意不是
邊界，於是當它們**不是區塊第一條陳述**時（第一條不經過 `skipToStatementEnd`），關鍵字連同
換行一起被吞掉。兩個後果都是 `rc=0`、無診斷：

| 原始碼 | 修復前 | 修復後 |
|---|---|---|
| `awy t` | 退化成裸 `t` ⇒ if 鏈臂體的值變成 **task handle**（堆指標），印出垃圾大數（如 `4367685392`） | 正確解出任務結果 |
| `run dbl(21)` | 退化成**同步** `dbl(21)` ⇒ 編譯照過、輸出照印，只是**不再 spawn** | 真的 spawn |

第二條特別危險：它是**語意**退化，不是崩潰，任何「跑起來有沒有報錯」的檢查都看不到。

修法是往 `isStatementBoundary()` 加入 `lexer.RUN, lexer.AWY`。規則是：**`parseStatement` 的
`switch` 能分派的每一個 token，都必須同時列在 `isStatementBoundary()` 裡**；漏一個就會出現
「只在非第一條陳述時發生」的靜默錯誤。迴歸：`tests/stmt-boundary.no`、
`src/parser/stmt_boundary_block_test.go`。

### 語句位置的裸 `{ ... }` 被當成宣告（2026-09-30 已修）

`parseStatement` 的 `LBRACE` 分支會呼叫 `classifyBlockAtCurrent()`（此時 `currentToken` 已是
`{`、沒有名稱可消耗）。它回傳的 `blockEnum` / `blockIface` / `blockTaggedEnum` 在**語句位置**
毫無意義 —— 真正的列舉／介面／標籤列舉宣告都需要前置名稱，走的是較早的
`classifyBlock()` 分支。但語句分派只把 `blockUnknown` 導向語句區塊，於是：

- `{ x = 2 }` 被分類成 `blockEnum`、`{ print(2) }` 被分類成 `blockIface`；
- 兩者都送進 `parseExpressionStatement`，而 `expr.go` 的 `LBRACE` 分支只認
  `blockStruct` / `blockMatch`，其餘走 `p.nextToken(); return nil`；
- `{` 被吃掉，區塊內陳述變成外層主體的**兄弟**，區塊自己的 `}` 反而關掉了**外層**區塊。

結果不是「吞掉」而是**陳述外漏一層**：`f = () { print(1); { print(2) }; print(3) }` 印出
`3,1,2`；`f = () { x = 1; { x = 2 }; print(x) }` 什麼都不印。同樣 `rc=0`、無診斷。

修法是在語句分派把 `blockEnum` / `blockIface` / `blockTaggedEnum` 一併導向語句區塊
（`blockStruct` 必須**留在**表達式路徑 —— `{ field: value }` 是合法的匿名結構體字面量陳述）。
⚠️ **不要**改 `classifyBlockAtCurrent` 的 `ASSIGN` 分支來「修」這個問題：那只涵蓋三種誤分類
中的一種，而且把判斷搬離了唯一知道「我在語句位置」的地方。

⚠️ **連帶影響**：這個修復讓語句位置的 `{ ... }` 第一次真正以 `*parser.BlockStatement` 存活，
而 `programUsesPrint`（`src/build/transpiler.go`，決定要不要載入 `fmt`/`io`/`str`/`byte` 的
手寫 AST walker）的白名單裡沒有 `BlockStatement` ⇒ 只把 `print` 寫在裸區塊裡的程式不再被偵測。
**動到語句層就要重跑 `go test ./build/ -run TestProgramUsesPrint`**，並把新的容器型別補進
`walkStmt`。迴歸：`src/parser/stmt_boundary_block_test.go`、`tests/stmt-boundary.no`。

### 全局變數首次賦值的 free 跳過判斷不夠精確

編譯器使用編譯期 map `globalFirstAssigned` 追蹤全局變數是否已做過首次賦值：首次賦值跳過釋放舊值（舊值是 `zeroinitializer`，非堆數據），後續重賦值才釋放舊堆值。

此 map 在整個編譯過程中只初始化一次，不按函數級別重置，且不區分條件分支路徑。如果全局變數在條件分支中首次賦值，編譯器按 AST 順序處理：第一條賦值語句標記為「首次」（跳過 free），第二條賦值語句（即使在另一分支）走重賦值路徑（嘗試 free 舊值）。若運行時第二條分支先執行，全局仍是 `zeroinitializer`（data=NULL, len=0），會嘗試 free 未初始化的舊值。

**當前緩解措施（有效）**：
- 淺容器（`%str-long`）：`emitNullCheckFree` 生成運行時 `icmp eq i8* dataPtr, null` 檢查，NULL 時跳過 `call @free`
- 深容器（`%vec/%arr`）：`emitDeepContainerFree` 額外有 `len == 0` 短路檢查，zeroinitializer 的 len 為 0，直接跳過整個釋放循環

這兩層運行時防護使得即使編譯期判斷不夠精確，實際不會崩潰。但邏輯上依賴運行時 NULL 檢查作為安全網，而非編譯期精確判斷。

**潛在改進方向**：將 `globalFirstAssigned` 從編譯期 map 改為運行時追踪機制（類似 `movedVarBitset` 的 bitmap），但會增加運行時開銷，且當前緩解措施已足夠有效。
