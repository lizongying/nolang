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
- `tests/test-slot-rebind.no`：i64/u64 重綁定、i128/u128 經 `==` 比較、txt 經 `.len-bytes()`、降級拷貝（`da` 複用 → `db`/`dc` 拷貝）、重賦值後（`ra`=10）、參數 move（`pm_fn` 源為參數 → 拷貝）、重綁定後重賦值（`rr_fn` → `rr`=99）、消費（`consume(m)` → `cm`=84）。期望輸出 `42 7 1 1 17 5 5 10 100 99 84`。
- `tests/test-slot-rebind-unsafe.no`：協程（`run`/`awy`）capture 棧變數，驗證 `curHasUnsafeConstruct` 禁用重綁定後輸出與基線一致。

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

**測試**：`tests/test-slot-rebind.no`（覆蓋 i64/u64/i128/u128/txt 重綁定、降級為 copy、目標重賦值後別名失效、參數 move 降級、consume 傳參，期望輸出 `42 7 1 1 17 5 5 10 100 99 84`）。

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

### async 共享數據
異步線程與主線程共享堆數據時，free 順序不確定。

### 全局變數首次賦值的 free 跳過判斷不夠精確

編譯器使用編譯期 map `globalFirstAssigned` 追蹤全局變數是否已做過首次賦值：首次賦值跳過釋放舊值（舊值是 `zeroinitializer`，非堆數據），後續重賦值才釋放舊堆值。

此 map 在整個編譯過程中只初始化一次，不按函數級別重置，且不區分條件分支路徑。如果全局變數在條件分支中首次賦值，編譯器按 AST 順序處理：第一條賦值語句標記為「首次」（跳過 free），第二條賦值語句（即使在另一分支）走重賦值路徑（嘗試 free 舊值）。若運行時第二條分支先執行，全局仍是 `zeroinitializer`（data=NULL, len=0），會嘗試 free 未初始化的舊值。

**當前緩解措施（有效）**：
- 淺容器（`%str-long`）：`emitNullCheckFree` 生成運行時 `icmp eq i8* dataPtr, null` 檢查，NULL 時跳過 `call @free`
- 深容器（`%vec/%arr`）：`emitDeepContainerFree` 額外有 `len == 0` 短路檢查，zeroinitializer 的 len 為 0，直接跳過整個釋放循環

這兩層運行時防護使得即使編譯期判斷不夠精確，實際不會崩潰。但邏輯上依賴運行時 NULL 檢查作為安全網，而非編譯期精確判斷。

**潛在改進方向**：將 `globalFirstAssigned` 從編譯期 map 改為運行時追踪機制（類似 `movedVarBitset` 的 bitmap），但會增加運行時開銷，且當前緩解措施已足夠有效。
