---
sidebar_position: 3.1
---

## 基礎型別

### types — 型別定義

Nolang 型別到 LLVM 的對映關係：

| Nolang           | LLVM                                               |
| ---------------- | -------------------------------------------------- |
| `bool`           | `i1`                                               |
| `byte`           | `i8`                                               |
| `char`           | `i32`                                              |
| `i8/i16/i32/i64/i128` | `i8/i16/i32/i64/i128`                             |
| `u8/u16/u32/u64/u128` | `i8/i16/i32/i64/i128`                          |
| `f32`            | `float`                                            |
| `f64`            | `double`                                           |
| `str`            | `%str-long = { i32, i32, i8* }`（u32 len, u32 cap, data；64 位元下 16 字節，堆分配） |
| `txt`            | `{ [255 x i8], i8 }`（固定 256 字節） |

> **`str` 的 len / cap 內部是 `u32`，對外一律 `i64`。** 描述符讀取時 `zext i32 -> i64`、寫入時
> `trunc i64 -> i32`；`s.len-bytes()`、`s.len()`、`s.cap()` 與私有 `_len` / `_cap` / `_set-len` /
> `_set-cap` 的簽名都維持 `i64`。詳見 [字串：內部佈局與對外 API](../lang/str.md)。
> `txt` 沒有 `cap` 欄位，長度是 1 個 `i8`（0-255），**不隨 `str` 改成 u32**。

**複合型別：**

- **變長數組 `[]t`**：底層 `{ t*, i64 }`（data, len）
- **定長數組 `[n]t`**：LLVM 固定大小陣列
- **字串 `str`**：堆分配的位元組序列，描述符 `{ u32 len, u32 cap, i8* data }`（64 位元下 16 字節）；支援 `s[i]`、`s[i..j]`、`s + t`。長度／容量對外以 `i64` 呈現（`s.len-bytes()` / `s.cap()`），內部以 `u32` 存放
- **固定字串 `txt`**：固定 256 字節結構 `{ [255]byte data, byte len }`，前 255 字節存儲數據，最後 1 字節存儲**字節**長度（0-255），無需堆分配，必須類型標註（`t txt = 'abc'`）；`t.len()` 回傳 code point 數量（與 `str` 對齊，等價 `t.count()`），`t.len-bytes()` 回傳字節數量；索引 `t[i]` **讀取**以**碼點**為單位（讀回該碼點的 `char`，越界回傳 `-1`）；**寫入 `t[i] = c` 自 2026-11-12 起全域禁用**（編譯錯誤 `idxwrite`）—— 它會把 `c` 重新 UTF-8 編碼並後移尾部字節，長度隨寫入隱式改變（`i` 等於碼點數時為追加、超過 255 的部分靜默丟棄）；要變長請用 `t.push(c char)` / `t.push-str(s)` / `t.push-txt(u)`，要就地改值請用 place-match `t[i]: { ok -> it = c }`；字節級讀取用 `t.byte(i)`（回傳 `?byte`，越界回傳 nil）；要按字節拼裝就直接攢 `[]byte`，再 `txt.from-bytes(b)` 一次拷入（超過 255 字節截斷，長度按實際字節數設定）。字節級寫入器 `txt._set-byte` 自 2026-11-11 起是 **std 私有**，用戶代碼不可調用。`str` 的 `s[i] = v` 已於 2026-11-11 **全域禁用**（編譯錯誤 `idxwrite`，`with-cap` 基底也不再豁免），`str` 側只剩 `s.fill(val byte)`、place-match `s[i]: { ok -> it = v }` 與增長用的 `s.push(c)` / `s.push-str(s)`
- **列舉/Union**：`option` tagged enum（`ok t` / `nil` / `err str`）
- **結構體**：必須多行定義，欄位不加逗號
- **配列**：底層 linked-hash-map
- **迭代器**：`for iter.next() {}`（介面方法 `next() (ok bool)`）

### option — 選項型別

`option<t>` 標籤列舉（tag=0=val, 1=nil, 2=err）：

```no
#{fmt-skip}
x ?t                ; 宣告 option<t>
x = 42              ; 設為有值
x = nil             ; 設為空
x = err('msg')      ; 設為錯誤

; match
x: {
    val -> f(it)
    nil ->
    err -> g(it.msg())
}  
```

**風格指引：** 函數可能失敗或返回空值時，應使用 `?t` option 而非 `(val, ok bool)`。`?t` 有三種狀態：`ok`（有值）、`nil`（空值/正常缺失）、`err`（錯誤）。正常值會隱性綁定。例如 `pop()` 返回 `?i64`（`nil` = 空）、`read-line()` 返回 `?str`（`nil` = EOF，`err` = 錯誤）、`lookup()` 返回 `?str`（`nil` = 未找到）。詳見語法文檔。

---
