---
sidebar_position: 3.2
---

## 核心函式庫

### fmt — 格式化輸出

Nolang 使用**具名格式字串** `{name[:spec]}`，直接引用作用域變量，無需位置參數。輸出透過 `io.out`/`io.err` 系統調用，不依賴 libc `printf`。

```no
print('x={x}')                 ; 具名格式，自動換行（stdout）
print('result={val}', 42, 'result={val}')  ; 多參數：空格分隔；每個字面量各自是模板
eprint('err {x}')              ; 具名格式，自動換行（stderr）
print('編號 {id:06} 金額 {money:.2f}')  ; 支援對齊/填充/寬度/精度
s = format('x={x}')            ; 返回格式化字串（替代 sprintf）
io.out('no-newline-here')      ; 底層命令，輸出不換行（stdout）
io.err('err-no-newline')       ; 底層命令，輸出不換行（stderr）
; printf/eprintf 已移除（呼叫即編譯錯誤 [printf-depr]）：printf→print/io.out、eprintf→eprint/io.err
; sprintf 仍可用但已廢棄：sprintf→format
; io.err 明確模組前綴，不會與 Option 構造函數 err() 衝突
```

### math — 數學函數

**常量：** `math.PI`, `math.E`

**基礎：** `math.abs`, `math.sqrt`

**三角：** `math.sin`, `math.cos`, `math.tan`, `math.asin`, `math.acos`, `math.atan`, `math.atan2`, `math.degrees`, `math.radians`

**雙曲：** `math.sinh`, `math.cosh`, `math.tanh`

**取整：** `math.ceil`, `math.floor`, `math.round`, `math.trunc`

**指數/對數：** `math.exp`, `math.log`, `math.log10`, `math.log2`, `math.pow`, `math.hypot`, `math.cbrt`

**其他：** `math.fmod`, `math.max`, `math.min`

### char — 字元操作

char 本質為 i32（Unicode 碼點），所有操作以方法形式提供：

```no
c char = 'A'
c.is-digit()       ; 是否為數字 (0-9)（方法）
c.is-letter()      ; 是否為字母 (a-z, A-Z)（方法）
c.is-alpha()       ; is-letter 別名（方法）
c.is-alnum()       ; 是否為字母或數字（方法）
c.is-space()       ; 是否為空白字元（方法）
c.is-upper()       ; 是否為大寫字母（方法）
c.is-lower()       ; 是否為小寫字母（方法）
c.to-upper()       ; 轉大寫（ASCII）（方法）
c.to-lower()       ; 轉小寫（ASCII）（方法）
c.to-bytes()       ; Unicode → UTF-8 位元組（方法）
c.to-str()         ; Unicode → 字串（UTF-8，方法）
```

### str — 字串操作

```no
ok = a.eq(b, n)               ; 相等比較（方法）
dst = s.copy()                ; 字串複製（方法）
s.fill(val byte)              ; 填充 byte 值（方法）
pos = s.index(sub)            ; 子字串位置
ok = s.contains(sub)          ; 是否包含
ok = s.starts-with(sub)       ; 前綴判斷
ok = s.ends-with(sub)         ; 後綴判斷
s.to-upper()                  ; 轉大寫
s.to-lower()                  ; 轉小寫
out = s.trim()                ; 去首尾空白
out = s.repeat(n)             ; 重複
out = s.slice(start, end)     ; 切片（下標為碼點位置）
b = s.to-bytes()              ; 轉 []byte
s = b.to-str()                ; []byte 轉 str（方法）
v = s.to-i64()                ; 字串轉 i64（回傳 ?i64）
v = s.to-i8()                 ; 字串轉 i8（回傳 ?i8）
v = s.to-i16()                ; 字串轉 i16（回傳 ?i16）
v = s.to-i32()                ; 字串轉 i32（回傳 ?i32）
v = s.to-u8()                 ; 字串轉 u8（回傳 ?u8）
v = s.to-u16()                ; 字串轉 u16（回傳 ?u16）
v = s.to-u32()                ; 字串轉 u32（回傳 ?u32）
v = s.to-u64()                ; 字串轉 u64（回傳 ?u64）
v = s.to-byte()               ; 字串轉 byte（回傳 ?byte）
v = s.to-f64()                ; 字串轉 f64（回傳 ?f64）
v = s.to-bool()               ; 字串 "true"/"false" 轉 bool（回傳 ?bool）
s = v.to-str()                ; i64 轉字串（方法）
out = s.reverse()             ; 反轉
c = s.compare(b)              ; 字典序比較
n = s.count()                 ; code point 總數
val = s.replace-char(old, new) ; 取代字元（返回結果字串）
out = s.trim-char(c)          ; 去指定字元
ok = s.empty()                ; 是否為空
s.clear()                     ; 清空（len=0，原地修改）
s = with-cap(cap)            ; 內建語法：建立指定容量的新字串（len=0）
s = with-len(len)            ; 內建語法：建立指定長度的字串（len=cap）
s = with-cap-len(cap, len)   ; 內建語法：建立指定容量和長度的字串
parts = s.split(sep)          ; 用分隔符分割（返回 []str，方法）
out = ss.join(sep)            ; []str 用分隔符連接（方法）
```

### number — 數值操作

```no
number.max(a, b)                     ; 最大值
number.min(a, b)                     ; 最小值
r = num.clamp(lo, hi)         ; 限制範圍（方法）
r = number.abs(a)                    ; 絕對值（num 泛型）
r = num.sign()                ; 正負號（-1/0/1，方法）
number.even(v)                       ; 奇偶判斷
number.odd(v)
number.gcd(a, b)                     ; 最大公因數
number.lcm(a, b)                     ; 最小公倍數
r = number.pow(a, n)                 ; 整數冪
number.i64-to-f64(v)                 ; 數值轉換
number.f64-to-i64(v)
s = int.to-str()              ; i64 轉字串（方法）
q = number.div(a, b)                 ; 除法取商
r = number.mod(a, b)                 ; 取模
number.swap(a, b)                    ; 交換
yes = float.is-nan()          ; NaN 判斷（方法）
yes = float.is-inf()          ; Inf 判斷（方法）

; 範圍常數
i8.MIN / MAX                  ; -128 / 127
i16.MIN / MAX                 ; -32768 / 32767
i32.MIN / MAX                 ; -2147483648 / 2147483647
i64.MIN / MAX                 ; -2^63 / 2^63-1
i128.MIN / MAX                ; -2^127 / 2^127-1
u8.MIN / MAX                  ; 0 / 255
u16.MIN / MAX                 ; 0 / 65535
u32.MIN / MAX                 ; 0 / 4294967295
u64.MIN / MAX                 ; 0 / 2^64-1
u128.MIN / MAX                ; 0 / 2^128-1
```

### byte — 位元組操作

```no
out = i64.to-bytes-be()         ; i64 → big-endian [8]byte
out = i64.to-bytes-le()         ; i64 → little-endian [8]byte
v = []byte.to-i64-be()          ; big-endian []byte → i64（1~8 位元組）
v = []byte.to-i64-le()          ; little-endian []byte → i64（1~8 位元組）
s = []byte.to-str()             ; []byte 轉 str（方法）
s = []byte.to-hex()             ; []byte → 大寫十六進制字串
s = []byte.to-hex-lower()       ; []byte → 小寫十六進制字串
s = byte.to-str()               ; byte 轉 str（方法）
```

### txt — 固定長度文字類型

`txt` 是固定 256 字節的文字類型，適合短文字場景：
- 前 255 字節存儲數據（`data [255]byte`）
- 最後 1 字節存儲長度（`len byte`，單位是「字節」，範圍 0-255）
- 無需堆分配，全部在棧上
- 必須類型標註

長度語義（與 `str` 對齊）：
- `t.len()` → 字元（code point）數量（等價 `t.count()`）
- `t.len-bytes()` → 字節數量（底層 UTF-8 緩衝區實際長度）
- 索引 / 切片 / 追加等字節操作以 `len-bytes()` 為準；ASCII 下兩者相等，含多字節 UTF-8 時 `len() < len-bytes()`

```no
t txt = 'hello'              ; 必須類型標註
n = t.len()                  ; 字元（code point）數量（i64）
b = t.len-bytes()            ; 字節數量（i64）
c = t[0]                     ; 索引存取（byte）
ok = t.eq(b txt)             ; 相等比較
dst = t.copy()               ; 複製
s = t.to-str()              ; 轉 str
out = t.to-bytes()           ; 轉 []byte
pos = t.index(sub txt)       ; 查找子串
ok = t.contains(sub txt)     ; 是否包含
ok = t.starts-with(sub txt)  ; 前綴檢查
ok = t.ends-with(sub txt)    ; 後綴檢查
t.append(b byte)            ; 追加單字節
t.append-str(s str)          ; 追加 str
t.append-txt(t2 txt)         ; 追加 txt
out = t.reverse()            ; 反轉
out = t.slice(start, end)   ; 截取 [start, end)
r = t.compare(b txt)        ; 字典序比較 (-1/0/1)
```

### vec — 切片操作

```no
v = vec.vec-create(n, val)         ; 建立長度 n 的切片，全部填充 val
ok = []t.eq(a, b, n)           ; 相等比較
n = []t.len()                  ; 長度
[]t.push(val)                   ; 追加（自動擴容）
[]t.clear()                     ; 清空（len=0，cap/data 不變）
v = with-cap(cap)             ; 內建語法：建立指定容量的新切片（len=0）
v = with-len(len)             ; 內建語法：建立指定長度的切片（len=cap）
v = with-cap-len(cap, len)    ; 內建語法：建立指定容量和長度的切片
val, new-n = []t.pop()         ; 彈出
found = []t.contains(n, val)   ; 是否包含（n 為長度）
[]t.reverse(n)                  ; 反轉前 n 個元素
[]t.clone(dst)                  ; 複製到 dst
[]t.fill(n, val)                ; 前 n 個元素填充
arr = []t.to-arr()             ; 轉陣列
[]t.sort-asc()                  ; 升序排序（方法）
[]t.sort-desc()                 ; 降序排序（方法）
```

### arr — 陣列操作

```no
out = [n]t.clone()             ; 複製
ok = [n]t.eq(b)                ; 相等比較
[n]t.fill(val)                  ; 填充
[n]t.reverse()                  ; 反轉
ok = [n]t.contains(val)        ; 是否包含
v = [n]t.to-vec()              ; 轉切片
v = [n]t.max()                 ; 最大值
v = [n]t.min()                 ; 最小值
v = [n]t.sum()                 ; 總和
i = [n]t.index-of(val)          ; 索引
v = [n]t.last()                ; 最後元素
v = [n]t.first()               ; 首元素
[n]t.sort-asc()                 ; 升序排序
[n]t.sort-desc()                ; 降序排序
```

### sort — 排序常量

```no
sort.ast                         ; 升序
sort.desc                        ; 降序
```

---
