---
sidebar_position: 3
---

# 字串

Nolang 字串（`str`）為堆分配的位元組序列 `{*byte, i64, i64}`（data, len, cap），支援多種運算符與方法。

## 字串運算符

### 拼接（`-`）

使用 `-` 運算符拼接字串：

```no
; 字面量拼接
s = 'Hello' - ' ' - 'World'

; 與變量拼接
greeting = 'Hello, ' - name
```

### 重複（`*`）

使用 `*` 運算符重複字串：

```no
s = 'Hello' * 3
```

## 索引與切片

**索引**：`s[i]` 的型別是 **`char`**（Unicode 碼點），不是 `byte`。對於含多字節字符的串，`s[i]` 會從串首前向解碼 UTF-8 數到第 i 個碼點（見下方效能說明）。

```no
s = 'Hello World'
c char = s[0]      ; 'H' 的碼點（型別 char）
```

**切片**：`s[a..b]` 回傳底層字串的一個**視圖**，型別仍是 **`str`**（共享記憶體，不複製）。切片下標是**碼點位置**，與 `s[i]` 一致：

```no
sub = s[6..]       ; 'World'
sub = s[6..11]     ; 'World'
sub = s[0..5)      ; 'Hello'（左閉右開）
```

> 切片的下標是**碼點位置**，不是字節偏移。若需字節偏移的切片（例如配合 `s.byte(i)` 遍歷），用 `s.slice-bytes(start, end)`。

```no
; 長度
n = s.len()          ; code point 數（Unicode 字符數）
n = s.count()        ; 同上（舊方法名，仍可用）
n = s.len-bytes()    ; byte 長度（UTF-8 位元組數）
; 注意：裸 s.len（結構體欄位）已不再支援，讀寫都會報錯；請改用 s.len() / s.len-bytes()
```

### 隱式轉換（char → str）

`char` 可以**隱式轉換**為 `str`：把該碼點 UTF-8 編碼成一個新字串。因此下列寫法都合法，**無需**手動呼叫 `char.to-str()`：

```no
s = 'héllo'              ; 'h' 'é' 'l' 'l' 'o'（é 為多字節）

; 1) s[i] 的 char 直接賦值給 str 變數 → 隱式轉成單字元字串
a str = s[0]             ; 'h'

; 2) 切片結果本身型別已是 str，直接賦值即可
b str = s[0..1]          ; 'hé'（下標 0..1，兩端皆含）

; 3) char 參與字串拼接（-）時隱式轉為 str
msg = 'first: ' - s[0]   ; 'first: h'

; 4) char 與 str 比較時隱式轉為 str
ok = s[0] == 'h'         ; true
```

> 隱式轉換只把**單個碼點**編碼為 `str`。若想要的是數值，直接用 `char` 本身（例如 `print(c)` 會輸出該碼點的十進位值）；若想要顯式轉換，仍可用 `c.to-str()`（等價於隱式轉換）。

## ASCII 优化与性能提示

`str`/`txt` 的下标 `s[i]` 取的是**第 i 个码点（Unicode 字符）**，而不是第 i 个字节。因此对于可能包含多字节 UTF-8 字符（如中文、emoji、`é`）的字符串，每次 `s[i]` 都必须从串首向前迭代 UTF-8，直到数到第 i 个码点——时间复杂度为 **O(n)**。

当编译器能**证明**该字符串全部由 ASCII 字符（码点 0–127）组成时，每个字节恰好就是一个字符，`s[i]` 直接定址底层字节，退化为 **O(1)**。编译器通过以下规则证明纯 ASCII：

- 显式注解：声明或赋值处加 `#{ascii}` 注解；
- 赋值为 ASCII 字符串字面量（如 `'hello'`）；
- 标识符传播：`b = a`，若 `a` 已证明 ASCII，则 `b` 也是；
- 拼接（`-`）两侧都已证明 ASCII。

```no
; ASCII 字面量 → 编译器证明为纯 ASCII，s[i] 为 O(1)
name = 'hello'
c = name[1]              ; O(1)，取字节 'e'

; 含多字节字符 → 无法证明 ASCII，s[i] 为 O(n) 码点迭代
s = 'héllo'
c = s[1]                 ; O(n)，取第 1 个码点 'é'（而非字节）
```

### 性能告警（LSP / `no vet`）

当你在**循环里手动按码点下标遍历**未能证明为 ASCII 的 `str`/`txt` 时（即 `for i <- [0..s.count()): { s[i] }` 这类按码点下标逐个取字符的写法），`no vet` 与编辑器 LSP 会给出 **WARNING** 级静态告警（不禁止语法，仅提示）。这种写法每次 `s[i]` 都要从串首前向迭代 UTF-8 取第 i 个码点，循环整体退化为 **O(n²)**。

单独一次 `s[i]`（如 `s[5]`，只 O(n) 一次）**不会**告警；直接 `for c <- s` 遍历字符也**不会**告警（见下）。

建议改用更快的方式：

- 若需要遍历每个字符，直接用 `for c <- s` 前向遍历——这是首选的 O(n) 单次扫描写法，避免反复 `s[i]` 的 O(n²)。编译器会按 **UTF-8 码点（Unicode 字符）** 推进，每次把解码后的码点值存入循环变量 `c`，语义等价于 `for c <- s.to-chars()`：
  ```no
  for c <- s {
      ; c 为当前码点，类型 char（已按 UTF-8 解码，非字节）
  }
  ```
- 若只需按字节访问（如编解码、哈希、memcmp），使用逃生舱口 `s.byte(i)` —— 永远直接定址第 i 个字节，**恒为 O(1)**，且不会进入 std 函数体：
  ```no
  b = s.byte(0)          ; 取第 0 个字节，O(1)
  ```

> 标准库（src/std）内部的字节访问已全部迁移为 `s.byte(i)`：含 `str.no`/`txt.no` 的 UTF-8 编解码（`decode-cp`）、比较（`compare`/`starts-with`/`ends-with`）、哈希、切片复制（`slice`/`repeat`/`trim`/`copy`）、`to-bytes`/`to-upper`/`to-lower`/`reverse`/`replace-char`/`count`/`index` 等。因此 `no vet` 不再需要跳过标准库文件；其余 `[]byte`/切片/数组类型的元素访问 `x[i]` 本就是按元素（字节）索引，语义不变，亦不告警。

| 写法 | 语义 | 复杂度 | 说明 |
|---|---|---|---|
| `s[i]`（已证明 ASCII） | 第 i 个字节 = 第 i 个字符 | O(1) | 自动证明，无需改动 |
| `s[i]`（未证明 ASCII，循环内） | 第 i 个码点 | O(n²) 反模式 | 触发 LSP 告警 |
| `s[i]`（未证明 ASCII，循环外） | 第 i 个码点 | O(n) 一次 | 不告警 |
| `s.byte(i)` | 第 i 个字节 | O(1) | 字节访问逃生舱口 |
| `for c <- s` | 逐个码点 | O(n) 单次扫描 | 遍历首选，不告警 |

## 字串方法

```no
; 比較
ok = a.eq(b, n)               ; 相等比較（方法）
c = s.compare(b)              ; 字典序比較

; 查找（index/rindex 返回「碼點（字符）位置」，與 s[i] 一致）
pos = s.index(sub)             ; 首次出現的碼點下標，未找到 -1
pos = s.index-from(sub, k)     ; 從第 k 個碼點開始查找
pos = s.rindex(sub)            ; 最後一次出現的碼點下標
pos = s.rindex-from(sub, k)    ; 從第 k 個碼點反向查找
pos = s.last-index(sub)        ; rindex 的別名
ok  = s.contains(sub)          ; 是否包含
ok  = s.starts-with(sub)       ; 前綴判斷
ok  = s.ends-with(sub)         ; 後綴判斷
ok  = s.empty()                ; 是否為空

; 注意：index 系列返回的是「碼點位置」，因此 s[s.index(sub)] 取到的是 sub 的首字符，
;       與 s[i] 的碼點索引語義一致。例如 '日本語'.index('語') == 2，且 s[s.index('語')] == '語' 的碼點。
;       若需「碼點位置」切片，s.slice(start, end) 即碼點語義（與 s[i] 一致）；
;       若需「字節偏移」切片（如配合 s.byte 遍歷），用 s.slice-bytes(start, end)。
;       str 內部提供字節級查找 str.find-byte-from（供 split 等複用）。
;       注意：txt 型別的 slice/at 仍為字節語義（txt 是裸字節緩衝），與 str 語義分叉。

; 轉換
out = s.to-upper()             ; 轉大寫
out = s.to-lower()             ; 轉小寫
out = s.trim()                 ; 去首尾空白
out = s.trim-char(c)           ; 去指定字元
out = s.repeat(n)              ; 重複
out = s.reverse()              ; 反轉
out = s.slice(start, end)      ; 切片（下標為碼點位置，與 s[i] 一致）
out = s.slice-bytes(b, e)      ; 切片（下標為字節偏移，內部使用）
val = s.replace-char(old, new) ; 取代字元

; 轉換為其他型別
b = s.to-bytes()               ; 轉 []byte
v = s.to-i64()                 ; 轉 ?i64
v = s.to-f64()                 ; 轉 ?f64
v = s.to-bool()                ; 轉 ?bool

; 分割與連接
parts = s.split(sep)           ; 分割（返回 []str）
out = ss.join(sep)             ; []str 連接

; 複製與填充
dst = s.copy()                 ; 字串複製
s.fill(val byte)               ; 填充 byte 值
```

## 字串與數值轉換

```no
; 數值轉字串（方法）
s = i64.to-str()               ; i64 轉字串
s = f64.to-str()               ; f64 轉字串
s = bool.to-str()              ; bool 轉 "true"/"false"
s = byte.to-str()              ; byte 轉字串
s = char.to-str()              ; char 轉字串

; 字串轉數值（返回 option）
v = s.to-i64()                 ; 回傳 ?i64
v = s.to-i32()                 ; 回傳 ?i32
v = s.to-u64()                 ; 回傳 ?u64
v = s.to-f64()                 ; 回傳 ?f64
```

## 自動長度追蹤

對 `s[i] = v` 賦值時，LLVM codegen 會自動更新 `len` 欄位為 `max(len, idx+1)`，不需手動設置：

```no
s = ''
s[0] = 72                      ; len 自動變為 1
s[1] = 105                     ; len 自動變為 2

; 截斷（縮短）：裸 s.len = n 已不再支援，改用切片
s = s.slice(0, 5)             ; 碼點截斷（與 s[i] 一致）
s = s.slice-bytes(0, 5)       ; 字節截斷
```

## 預分配（內建語法）

透過 `with-cap`、`with-len`、`with-cap-len` 三個內建語法可預先分配堆記憶體，避免後續 `push` / `s[i]=` 觸發反覆擴容：

```no
; with-cap(cap)：配置 cap 容量，len=0（需 push 後才能索引）
s = with-cap(256)               ; str，len=0, cap=256

; with-len(len)：配置 len 容量，len=cap（可直接索引讀寫）
s = with-len(128)               ; str，len=128, cap=128

; with-cap-len(cap, len)：同時指定容量與長度，適合「預留成長空間」場景
s = with-cap-len(512, 64)       ; str，len=64, cap=512
```

| 內建語法 | 參數 | len | cap | 適用場景 |
|---|---|---|---|---|
| `with-cap(cap)` | 1 | 0 | cap | 只知道上限，長度由 push 增長 |
| `with-len(len)` | 1 | len | len | 固定長度，直接索引讀寫 |
| `with-cap-len(cap, len)` | 2 | len | cap | 已知初始長度且需預留擴容空間 |

> **型別推導**：三個內建語法的結果型別由賦值左側推導，可用於 `str` 或 `[]T` 切片。

