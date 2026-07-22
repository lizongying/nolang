---
sidebar_position: 6
---

# 記憶體管理

Nolang 是**無 GC** 語言，記憶體安全由編譯器自動插入 `free` 保證。本文描述已實現的記憶體設計與所有權語義。

## 核心原則

### 單一所有權
每個堆 `data` 緩衝區**只有一個所有者**。所有權可透過 move 轉移，轉移後原所有者放棄 free 責任。

### 淺拷貝語義
賦值即淺拷貝結構體欄位（len/cap/data），**不克隆 data 緩衝區**。因此賦值後兩個變數共享同一 data，必須透過 move/clone 機制明確所有權。

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
```nolang
get-slice = () (out []i64) {
    local = [1, 2, 3]
    out = local   ; local 標記為 moved，函數結束不 free；out 由呼叫者管理
}

v = get-slice()  ; v 擁有 data，函數結束時 free
```

### 多返回值 move（按參數位置順序）
```nolang
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

### vec.push 的隱式 move
```nolang
inner = [1, 2, 3]
outer.push(inner)
; inner 標記為 moved，data 所有權轉移給 outer
; 函數結束時 inner 跳過 free，outer 深層 free 釋放 inner 的 data
```

push 只淺拷貝 inner 的結構體到 outer 元素位置，**不克隆 data**。因此源變數和外部 vec 共享同一 data 指標，必須標記源為 moved 避免 double-free。

## 切片視圖

切片表達式 `arr[1..3]` 產生視圖（零拷貝），共享原數組 data。視圖的三種命運：

| 目標 | 行為 | 所有權 |
|------|------|--------|
| 局部變數 `v = arr[1..3]` | 零拷貝視圖 | 共享原數組 data |
| 輸出參數 `out = arr[1..3]` | clone（malloc+memcpy） | 獨立擁有 |
| 顯式 `[]T` 型別 `v []i64 = arr[1..3]` | clone | 獨立擁有 |

**原因**：輸出參數逃逸到呼叫者，原數組可能在函數結束前被 free，視圖必須 clone 為獨立 data。

## 重新賦值與舊值釋放

```nolang
s = 'hello'     ; malloc data 緩衝區
s = 'world'     ; 釋放 'hello' 的 data，malloc 新 data
```

重新賦值堆擁有型別時，編譯器在賦值前自動釋放舊值的 data，避免泄漏。

## 結構體欄位釋放

```nolang
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

```nolang
local [4]i64 = [100, 200, 300, 400]   ; local 是固定陣列（16 字節）
local = [100, 200, 300]                ; 重新賦值為切片（24 字節）
```

固定陣列（`%arr`，2 欄位）與切片（`%vec`，3 欄位）記憶體佈局不同。重新賦值時編譯器自動分配新的 `%vec` 變數並重定向所有後續存取，避免緩衝區越界。

## 已驗證的測試案例

測試位於 `tests/mem-safety/`：

| 測試 | 驗證內容 |
|------|---------|
| `deep-free-str.no` | `[]str` 深層 free |
| `deep-free-nested-vec.no` | `[][]i64` 深層 free + push moved |
| `deep-free-struct-vec.no` | `[]MyType` 深層 free（遞迴結構體） |
| `struct-field-leak.no` | 結構體欄位堆數據釋放 |
| `slice-view-escape.no` | 切片視圖賦值輸出參數的 clone |
| `reassign-leak.no` | 重新賦值舊值釋放 |
| `vec-push-leak.no` | vec.push 的 moved 標記 |

## 已知限制

### map 容器
hashmap 未實現 key/value 的深層 free，map 容器的堆數據會泄漏。

### 循環臨時變數
```nolang
loop {
    s = 'temp'   ; 每次迭代 malloc 新 data，舊 data 未釋放
}
```

### 切片視圖 + 原數組 move
```nolang
view = arr[1..3]   ; view 共享 arr.data
arr = [9, 8, 7]    ; 釋放舊 arr.data → view 懸空
```

### async 共享數據
異步線程與主線程共享堆數據時，free 順序不確定。

### 全局變數
模組級變數的堆數據依賴進程退出，長期運行的服務可能泄漏。
