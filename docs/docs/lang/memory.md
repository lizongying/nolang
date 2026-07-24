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
| **值拷貝** | 基本型別（i64/f64/bool 等） | 直接拷貝數值，無堆數據 |
| **深層 clone** | 局部變數間 `b = a`，a 為堆擁有型別（vec/arr/str/可克隆結構體） | malloc 新 data + memcpy + 遞迴 clone 元素；a 和 b 各自獨立擁有 data，函數結束各自 free |
| **move** | 輸出參數 `out = x` | 淺拷貝結構體 + 標記源為 moved；源跳過 free |
| **深層 clone** | `vec.push(x)`（x 為堆擁有型別） | malloc 新 data + memcpy + 遞迴 clone 元素；源仍擁有獨立 data，函數結束各自 free |

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

**注意**：若 `a` 和 `b` 引用同一源變數（如 `a = x; b = x`），在被呼叫函數內只 move 一次（x 標記 moved），a 和 b 都獲得 x 的淺拷貝（共享同一 data 指標）。但在上層函數中，a 和 b 是獨立的局部變數，各自被追蹤為堆變數，函數結束時都會執行 free → **double-free**。當前 Nolang 沒有引用/借用語義，b 不會自動成為 a 的別名。**應避免這種模式**。

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
| `%vec` / `%arr`（元素為 %vec / %arr） | ❌ | 巢狀容器元素型別未知，回退為 move |
| `%str-long` | ✅ | malloc + memcpy 字串 data |
| 用戶結構體（無巢狀容器欄位） | ✅ | memcpy 結構體 + 遞迴 clone 堆欄位 |
| 用戶結構體（含巢狀容器欄位） | ❌ | 回退為 move |

### 與 move 的區別
- **深層 clone**：源和目標各自獨立擁有 data，函數結束各自 free
- **move**：源放棄所有權（標記 moved），目標接管 data，源跳過 free

`b = a` 的判斷規則：
1. 若 a 是輸出參數的源 → move
2. 否則若 a 是堆擁有型別且可深層 clone → 深層 clone
3. 否則值拷貝

`vec.push(x)` 不在此判斷規則內：push 是方法調用，不論 x 是否堆擁有型別，都對堆擁有元素執行深層 clone（見前節）。

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
