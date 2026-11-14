---
sidebar_position: 3
---

# 字串

Nolang 字串（`str`）為堆分配的位元組序列，支援多種運算符與方法。

## 內部佈局與對外 API

`str` 的描述符（`%str-long`）在 64 位元平台上是 **16 字節**：

```llvm
%str-long = type { i32, i32, i8* }   ; u32 len, u32 cap, data
```

| 欄位 | 內部型別 | 對外型別 | 說明 |
|---|---|---|---|
| `len` | `u32`（LLVM `i32`） | `i64` | 邏輯位元組長度；`s.len-bytes()`、`len(s)`、`s._len()` |
| `cap` | `u32`（LLVM `i32`） | `i64` | 實體容量（僅 str-long 有效）；`s.cap()`、`s._cap()` |
| `data` | `i8*` | `[]byte` / `*byte` | 指向堆緩衝區 |

內部把 `len` / `cap` 收窄成 `u32`，是為了讓描述符從 24 字節縮到 16 字節（`i32` + `i32` + 8 字節對齊的指標）。**對外 API 一律仍是 `i64`**：讀取時在邊界 `zext i32 -> i64`，寫入時 `trunc i64 -> i32`。標準庫與使用者程式不需要（也不應該）碰這層轉換；`s.len-bytes()`、`s.len()`、`s.cap()`、`s._len()` / `s._cap()` / `s._set-len()` / `s._set-cap()` 的簽名保持 `i64`。

> ⚠️ 內部計數器是 `u32`，因此單一 `str` 的長度／容量上限是 4 GiB（`4294967295`）。傳入超過此值的 `i64` 會在邊界被截斷；一般字串處理不受影響，但預分配 giant buffer 前應自行檢查。
>
> `txt` **不受這次變更影響**：它仍是固定 256 字節的 `{ [255 x i8] data, i8 len }`，沒有 `cap` 欄位，長度是 1 字節。

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

### 比較（`==` / `!=` / `str.compare`）

`str` **只實作相等比較**：`==` / `!=` 走 runtime 的 `@str_eq`，多字元字串完全合法。

關係運算符（<code>&lt;</code> `>` <code>&lt;=</code> `>=`）**沒有字串字典序語意**，對字串使用會是編譯錯誤：

- **合法**：**單字元**字串字面量 `'a'`，隱式視為 `char`（碼點）；可與另一個單字元字面量或 `char`（`"a"`）比較。
- **錯誤**：多字元字串 —— `'ab'`、`str` 變數、回傳 `str` 的呼叫（過去會**靜默回傳 false**）。

需要字典序時用 `str.compare(b)`：

```no
s = 'abc'
b = 'abd'
c = s.compare(b); -1（s < b）/ 0（相等）/ 1（s > b）

'a' <= 'z'; true — 單字元字串隱式 char
ch >= 'a'; ch 為 char，OK
s < b; 編譯錯誤：多字元字串
s.compare(b) < 0; 正確寫法
```

## 索引與切片

**索引**：`s[i]` 的推斷型別是 **`?char`**（Unicode 碼點），不是 `byte` —— 越界時回傳 `err`（`code.index-out`），在界內回傳 `ok(碼點)`（與整數 `+ - * / %` 同一套統一規則）。對於含多字節字符的串，`s[i]` 會從串首前向解碼 UTF-8 數到第 i 個碼點（見下方效能說明）。

```no
#{fmt-skip}
s = 'Hello World'
c = s[0]: { ok -> it  -> _ }   ; 'H' 的碼點（?char 必須顯式解包，空臂取 '\0'）
```

> 要把 `?char` 用掉只有三條路（**`#{index-out=DEF}` 已於 D11 廢棄並移除**，殘留註解是編譯錯誤；
> 也沒有「顯式 plain 型別拉回 plain」這條捷徑，`c char = s[i]` 同樣必須顯式消費）：
> **match 解包**（`x = s[i]: { ok -> it, -> _ }`，空臂取 `char` 零值 `'\0'`）、
> **place-match 寫入**（`s[i]: { ok -> it = x, -> _ }`）、或**簡寫 `?=` 上拋**（函式有 `?T` 結果參數，
> 或陳述位於塊 match 主體內）。細節與範例見〈syntax · 安全索引〉。

**切片**：`s[a..b]` 回傳底層字串的一個**視圖**，型別仍是 **`str`**（共享記憶體，不複製）。切片下標是**碼點位置**，與 `s[i]` 一致：

```no
sub = s[6..]; 'World'
sub = s[6..11]; 'World'
sub = s[0..5); 'Hello'（左閉右開）
```

> 切片的下標是**碼點位置**，不是字節偏移。若需字節偏移的切片（例如配合 `s.byte(i)` 遍歷），用 `s.slice-bytes(start, end)`。

```no
; 長度
n = s.len(); code point 數（Unicode 字符數）
n = s.count(); 同上（舊方法名，仍可用）
n = s.len-bytes(); byte 長度（UTF-8 位元組數）
; 注意：裸 s.len（結構體欄位）已不再支援，讀寫都會報錯；請改用 s.len() / s.len-bytes()
```

### 隱式轉換（char → str）

`char` 可以**隱式轉換**為 `str`：把該碼點 UTF-8 編碼成一個新字串。因此下列寫法都合法，**無需**手動呼叫 `char.to-str()`：

```no
s = 'héllo'; 'h' 'é' 'l' 'l' 'o'（é 為多字節）

; 1) s[i] 的 char 直接賦值給 str 變數 → 隱式轉成單字元字串
a str = s[0]; 'h'

; 2) 切片結果本身型別已是 str，直接賦值即可
b str = s[0..1]; 'hé'（下標 0..1，兩端皆含）

; 3) char 參與字串拼接（-）時隱式轉為 str
msg = 'first: ' - s[0]; 'first: h'

; 4) char 與 str 比較時隱式轉為 str
ok = s[0] == 'h'; true
```

> **`'...'` 是 `str` 字面量，`"..."` 是 `char` 字面量** —— 即使 `'x'` 只有一個碼點，它仍是 `str`。因此 `+` / `-` 的**兩側都是字面量**時（`'...'` 配 `"..."`），走上面的隱式轉換，結果是**拼接**而不是整數運算：
>
> ```no
> 'x' - "a"                ; "xa"（拼接；不是 120-97 = 23）
> "a" + 'x'                ; "ax"
> 'xy' + "a"               ; "xya"
> ```
>
> 一旦有一側**不是字面量**（char 變數、`s[i]`、整數），單字元 `'...'` 就按**位元組**參與整數運算 —— 這是刻意的，也是數字字元取值慣用法能成立的原因：
>
> ```no
> c char = "5"
> n = c - '0'              ; 5（位元組相減，不是 "50"）
> m = 'A' + 1              ; 66
> "z" - "a"                ; 25（兩側都是 char 字面量，char 相減）
> 'x' - c                  ; 23（c 是變數 → 位元組運算）
> ```
>
> 反過來說，位元組退化**只在對面是整數或字元時**發生。對面若是 `bool`、`f64`、結構體這類**沒有位元組語意**的值，`'...'` 仍然是字串，整個式子就是**拼接**，對面按 `to-str` 渲染：
>
> ```no
> b = true
> 'x' - b                  ; "xtrue"（不是 120 - 1 = 119）
> 'x' - 1.5                ; "x1.5"
> 'x' - 'y'                ; "xy"（兩側都是字串字面量）
> ```
>
> 因此 `'x' - b` 可以賦給 `str`：`s str = 'x' - b` 得到 `"xtrue"`；而 `s str = 'x' - 1` 仍會被拒絕，因為那是位元組運算（結果是數字）。

> 隱式轉換只把**單個碼點**編碼為 `str`。若想要的是數值，直接用 `char` 本身（例如 `print(c)` 會輸出該碼點的十進位值）；若想要顯式轉換，仍可用 `c.to-str()`（等價於隱式轉換）。
>
> `char` 代表單個 Unicode 標量碼點，底層存儲類型是 **`i32`**（合法範圍 `0 ..= 0x10FFFF`）。
>
> `char` **可以正常參與整數運算**（如 `z = a + 25`、`u = ch - 32`），按碼點值在 i32 寬度上計算；與整數家族不同，它不會預設回傳 `option<int>`，因此不會觸發「未處理整數溢出」的編譯錯誤。
>
> **渲染方式**：`char` 轉成文字時一律輸出**碼點的十進位值**（`"A"` → `65`，`"é"` → `233`），
> 且 `print(c)` 與字串插值 `print('{c}')` 的結果**必須一致**。char 算術按**有號 i32** 計，
> 所以 `"x" - "y"` 是 `-1`，兩條路徑都輸出 `-1`（不會有一邊輸出 `4294967295`）。
> 合法碼點恆在 `0 ..= 0x10FFFF`（第 31 位為 0），有號與無號擴展在那裡等價，
> 因此這個規則只影響 char 算術越界得到的負值。若要的是**字元本身**而非數字，用
> `c.to-str()`（`"A".to-str()` → `"A"`），那是另一個操作。

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
c = name[1]; O(1)，取字节 'e'

; 含多字节字符 → 无法证明 ASCII，s[i] 为 O(n) 码点迭代
s = 'héllo'
c = s[1]; O(n)，取第 1 个码点 'é'（而非字节）
```

### 性能告警（LSP / `no vet`）

当你在**循环里手动按码点下标遍历**未能证明为 ASCII 的 `str`/`txt` 时（即 `for i <- [0..s.count()): { s[i] }` 这类按码点下标逐个取字符的写法），`no vet` 与编辑器 LSP 会给出 **WARNING** 级静态告警（不禁止语法，仅提示）。这种写法每次 `s[i]` 都要从串首前向迭代 UTF-8 取第 i 个码点，循环整体退化为 **O(n²)**。

单独一次 `s[i]`（如 `s[5]`，只 O(n) 一次）**不会**告警；直接 `for c <- s` 遍历字符也**不会**告警（见下）。

建议改用更快的方式：

- 若需要遍历每个字符，直接用 `for c <- s` 前向遍历——这是首选的 O(n) 单次扫描写法，避免反复 `s[i]` 的 O(n²)。编译器会按 **UTF-8 码点（Unicode 字符）** 推进，每次把解码后的码点值存入循环变量 `c`，语义等价于 `for c <- s.to-chars()`：
  ```no
  c <- s: {
      ; c 为当前码点，类型 char（已按 UTF-8 解码，非字节）
  }
  ```
- 若只需按字节访问（如编解码、哈希、memcmp），使用逃生舱口 `s.byte(i)` —— 永远直接定址第 i 个字节，**恒为 O(1)**，且不会进入 std 函数体：
  ```no
  b = s.byte(0); 取第 0 个字节，类型 ?byte，O(1)
  out = 0
  b: {
      ok -> {
          out = it; 在界内：ok(byte)
      }

      -> {
          out = 0; 越界：nil（空臂合成零值）
      }
  }
  ```
  > `s.byte(i)` 的结果是 **`?byte`**（2026-10-21 起）：此前的裸字节读取不做边界检查，
  > 越界索引是未定义行为。现在越界返回 `nil`——注意这与容器索引 `arr[i]` / `s[i]` 越界回 `err`
  > （`code.index-out`）**不同**：`s.byte(i)` 是字节级逃生舱口，越界只表示「没有这个字节」，不是错误。
  > （见 design/option-unified.md §3.1 第 5 条）。因此 `b i64 = s.byte(0)` 会报
  > `15w45dqk`——明文 plain 标注配上 option 呼叫就是静默拆包，是编译硬错误；
  > 要用 `?=`、两段式 match（`b = s.byte(0): { ok -> it, -> _ }`）或 `_ =` 消费。
  > `txt.byte(i)` 语义相同（`txt` 的 `byte`  field 1 即字节长度）。

> 标准库（src/std）内部的字节访问已全部迁移为 `s.byte(i)`：含 `str.no`/`txt.no` 的 UTF-8 编解码（`decode-cp`）、比较（`compare`/`starts-with`/`ends-with`）、哈希、切片复制（`slice`/`repeat`/`trim`/`copy`）、`to-bytes`/`to-upper`/`to-lower`/`reverse`/`replace-char`/`count`/`index` 等。因此 `no vet` 不再需要跳过标准库文件；其余 `[]byte`/切片/数组类型的元素访问 `x[i]` 本就是按元素（字节）索引，语义不变，亦不告警。

| 写法 | 语义 | 复杂度 | 说明 |
|---|---|---|---|
| `s[i]`（已证明 ASCII） | 第 i 个字节 = 第 i 个字符 | O(1) | 自动证明，无需改动 |
| `s[i]`（未证明 ASCII，循环内） | 第 i 个码点 | O(n²) 反模式 | 触发 LSP 告警 |
| `s[i]`（未证明 ASCII，循环外） | 第 i 个码点 | O(n) 一次 | 不告警 |
| `s.byte(i)` | 第 i 个字节（`?byte`，越界为 `nil`） | O(1) | 字节访问逃生舱口 |
| `for c <- s` | 逐个码点 | O(n) 单次扫描 | 遍历首选，不告警 |

## 字串方法

```no
#{fmt-skip}
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
s = i64.to-str(); i64 轉字串
s = f64.to-str(); f64 轉字串
s = bool.to-str(); bool 轉 "true"/"false"
s = byte.to-str(); byte 轉字串
s = char.to-str(); char 轉字串

; 字串轉數值（返回 option）
v = s.to-i64(); 回傳 ?i64
v = s.to-i32(); 回傳 ?i32
v = s.to-u64(); 回傳 ?u64
v = s.to-f64(); 回傳 ?f64
```

## 下標寫入已禁用（`str` 2026-11-11 / `txt` 2026-11-12）

> **`s[i] = v` 是編譯錯誤 `idxwrite`**，`with-cap` / `with-len` / `with-cap-len`
> 三種基底一律不例外。**`txt` 的 `t[i] = c` 同樣禁用**（理由見下）。
>
> `str` 的原因：它是「越界寫入已配置緩衝區之外」的唯一來源 —— len=0 的緩衝靠直寫增長，
> 寫到 `i >= cap` 就跨出配置區。codegen 的 `ensureStrLongBuffer` 增長護欄原本就是
> 為此而設；禁用直寫後該護欄不再對用戶代碼可達，只保留作防禦性代碼。
>
> `txt` 的原因不同但同罪：`t[i] = c` 是按碼點**重編碼寫回**，長度會隨寫入隱式改變
> （`i` 等於碼點數時等價追加、超 255 的部分靜默丟棄）。txt 沒有堆、沒有擴容，
> 但「一次下標寫偷偷改了長度」是同一類危險。

四條仍然合法的寫法：

```no
; ① 逐位元組「構造」：[]byte 緩衝承接，收尾整體轉回 str
b []byte = with-cap-len(n, n)
b[0] = 48
b[i] = src.byte(i): {
    ok -> it

    -> 0
}
s = b.slice(0, k).to-bytes-str(); 也可 b.to-bytes-str()（整段）

; ② 原位「覆寫」既有內容：str.fill 把 len 個位元組全填成同一個值，長度不變
s = 'hello'
s.fill(72); 'HHHHH'

; ③ 下標寫只剩 place-match（以「讀」作閘門：i >= len 時靜默跳過，不增長、不越界）
s[i]: {
    ok -> it = v
    -> _
}

; ④ 要變長：用 push 家族（顯式、可審計的一次呼叫）
s.push("中");      按碼點追加（UTF-8）
s.push-str('de');  按位元組追加（原地擴容）
```

> ⚠️ **place-match 是「碼點」閘門，不是位元組閘門。** 它的閘門來路是 `s[i]` 這個
> **讀** —— 而 `str` 的下標讀取按碼點計。所以 ③ 只適合「一個碼點寫一格」的
> ASCII 場景（uuid 打連字號、time 打數字、hex 表查表）。要逐**位元組**拼內容
> （含 `>= 0x80` 的位元組、或任何可能非 ASCII 的來源字串）必須用 ① 的 `[]byte`：
> 實測 `f = with-len(8)` 後用 place-match 依次寫 224..231，只有前 4 格落盤，
> 其餘被閘門靜默跳過；`path.split` 的「非 ASCII 檔名」正是這樣被截斷的。

> **長度截斷**：裸 `s.len = n` 已不再支援，改用切片 —— `s.slice(0, n)` 是**碼點**切片，
> `s.slice-bytes(0, n)` 才是字節切片。（`[]byte` 緩衝上的 `b.slice(0, k)` 自然是字節切片。）
>
> **`with-cap(n)` 建 str 之後靠 `push` 增長**：`s str = with-cap(256)` 是 len=0 但已有
> 容量的緩衝，第一次 `push` 直接寫進去、不觸發擴容（`push-str` 只在 `len + 追加長度`
> 超出 `cap` 時才 realloc，並以倍增成長）。逐位元組的 `buf[i] = b` 對 str 仍然禁用。

### 增長：`push` 家族（2026-11-12）

直寫被禁之後，**變長必須是顯式的一次呼叫**：

| 方法 | 語義 |
|---|---|
| `s.push(c char)` | 追加一個碼點，UTF-8 編碼（內部委託 `push-str`） |
| `s.push-str(t str)` | 追加一個字串，**按位元組**拷貝，原地擴容 |

```no
s = 'ab'
s.push("中");      'ab中'，len-bytes 5、len 3
s.push-str('de');  'ab中de'，len-bytes 7

t = 'zz'
t.push-str(t);     'zzzz'（自我追加安全：先 retain 源、再用 memmove 拷貝）

u = ''
u.push-str('');    ''（空源不改變任何東西，也不分配）
```

> `push-str` 是編譯器內建（`#{buildin}`，MIR 展開為「原地擴容 + memmove + 改長度」），
> 不是 std 迴圈：一次追加 O(追加長度)，反覆追加均攤 O(1)（倍增）。
> `push` 走 UTF-8 編碼，所以 `len-bytes` 的增量等於該碼點的編碼長度，`len` 恆增 1。

> **`txt` 的寫入同樣已禁用（2026-11-12）**：`t[i] = c` 是**碼點級重編碼寫回**，長度會
> 隨寫入隱式改變 —— `i` 等於碼點數時等價追加、總字節長超過 255 的部分靜默丟棄。
> 「一次下標寫偷偷改了長度」正是禁令要消滅的東西，txt 與 str 同案處理。txt 的變長
> 入口是 `t.push(c)` / `t.push-str(s)` / `t.push-txt(u)`；就地改值用 place-match。
> 讀取 `t[i]` 仍回傳碼點 `char`，這一點沒變。
>
> 字節級原始寫入器 `str._set-byte` / `txt._set-byte` 自 2026-11-11 起是 **std 私有**
> （只有 std 內部的 `str.fill` / `txt.push` 等會呼叫），用戶代碼無法使用。
> 需要按字節拼 `txt` 時，先攢 `[]byte`，再用 `txt.from-bytes(b)` 一次拷入
> （超過 255 位元組自動截斷，長度按實際字節數設定）。

## 預分配（內建語法）

透過 `with-cap`、`with-len`、`with-cap-len` 三個內建語法可預先分配堆記憶體，避免後續 `push` 觸發反覆擴容：

```no
; with-cap(cap)：配置 cap 容量，len=0（需 push 後才能索引）
s = with-cap(256); str，len=0, cap=256

; with-len(len)：配置 len 容量，len=cap（可直接索引讀）
s = with-len(128); str，len=128, cap=128

; with-cap-len(cap, len)：同時指定容量與長度，適合「預留成長空間」場景
s = with-cap-len(512, 64); str，len=64, cap=512
```

| 內建語法 | 參數 | len | cap | 適用場景 |
|---|---|---|---|---|
| `with-cap(cap)` | 1 | 0 | cap | 只知道上限，長度由 push 增長 |
| `with-len(len)` | 1 | len | len | 固定長度，可直接索引讀 |
| `with-cap-len(cap, len)` | 2 | len | cap | 已知初始長度且需預留擴容空間 |

> 參數與 `s.cap()` / `s.len-bytes()` 的回傳值都是 **`i64`**（對外 API 不變），但描述符內部以
> **`u32`** 存放 len / cap。呼叫端看到的仍是 64 位元數值，轉換由編譯器在描述符邊界完成；
> 詳見〈內部佈局與對外 API〉。指派 `s.len = n` / `s.cap = n` 這類欄位寫入同樣走這層邊界，
> 因此不會覆寫相鄰的 header 欄位。

> ⚠️ **`str` 的緩衝一律不允許下標直寫**（2026-11-11 起，`with-cap` 基底也不再豁免）。
> `idxwrite` 的 with-cap 豁免現在只服務 **`[]byte` / `[]T` / 固定陣列**：它們的 len=0
> 緩衝仍靠 `buf[i] = b` 增長長度，沒有等價替代寫法。`str` 請改走 `[]byte` 緩衝 +
> `to-bytes-str()`（見〈下標寫入已禁用〉一節）。
> `with-len(n)` / `with-cap-len(cap, len)` 的 len 已就位，**寫入須 place-match
> `buf[i]: { ok -> it = v / -> _ }` 或 `buf[i] ?= v`**——與任何其他容器同罪，直寫是
> 編譯錯誤 `idxwrite`。（2026-11-10 起 `with-len` 不再豁免。）

> **型別推導**：三個內建語法的結果型別由賦值左側推導，可用於 `str` 或 `[]T` 切片。

