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
| `i8/i16/i32/i64` | `i8/i16/i32/i64`                                   |
| `u8/u16/u32/u64` | `i8/i16/i32/i64`                                   |
| `f32`            | `float`                                            |
| `f64`            | `double`                                           |
| `str`            | union（short: `[127]byte` / long: `{*byte, i64}`） |

**複合型別：**

- **變長數組 `[]t`**：底層 `{ t*, i64 }`（data, len）
- **定長數組 `[n]t`**：LLVM 固定大小陣列
- **字串 `str`**：union 型別（short ≤127 byte 存棧上 / long 存堆上），支援 `s[i]`、`s[i..j]`、`s + t`
- **列舉/Union**：`option` tagged enum（`ok t` / `nil` / `err str`）
- **結構體**：必須多行定義，欄位不加逗號
- **配列**：底層 linked-hash-map
- **迭代器**：`for iter.next() {}`（介面方法 `next() (ok bool)`）

### option — 選項型別

`option<t>` 標籤列舉（tag=0=val, 1=nil, 2=err）：

```no
x ?t                ; 宣告 option<t>
x = 42              ; 設為有值
x = nil             ; 設為空
x = err('msg')      ; 設為錯誤

; match
x: {
    val -> f(it)
    nil ->
    err -> g(it)
}  
```

**風格指引：** 函數可能失敗或返回空值時，應使用 `?t` option 而非 `(val, ok bool)`。`?t` 有三種狀態：`ok`（有值）、`nil`（空值/正常缺失）、`err`（錯誤）。正常值會隱性綁定。例如 `pop()` 返回 `?i64`（`nil` = 空）、`read-line()` 返回 `?str`（`nil` = EOF，`err` = 錯誤）、`lookup()` 返回 `?str`（`nil` = 未找到）。詳見語法文檔。

---
