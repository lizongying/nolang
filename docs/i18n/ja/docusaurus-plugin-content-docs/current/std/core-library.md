---
sidebar_position: 3.2
---

## コアライブラリ

### fmt — 整形出力

Nolang は**名前付きフォーマット文字列** `{name[:spec]}` を使用し、スコープ内の変数を直接参照するため、位置引数は不要です。出力は `io.out`/`io.err` のシステムコールを通じて行われ、libc の `printf` には依存しません。

```no
print('x={x}'); 具名格式，自動換行（stdout）
print('result={val}', 42, 'result={val}'); 多參數：空格分隔；每個字面量各自是模板
eprint('err {x}'); 具名格式，自動換行（stderr）
print('編號 {id:06} 金額 {money:.2f}'); 支援對齊/填充/寬度/精度
s = format('x={x}'); 返回格式化字串（替代 sprintf）
io.out('no-newline-here'); 底層命令，輸出不換行（stdout）
io.err('err-no-newline'); 底層命令，輸出不換行（stderr）
; printf/eprintf 已移除（呼叫即編譯錯誤 [printf-depr]）：printf→print/io.out、eprintf→eprint/io.err
; sprintf 仍可用但已廢棄：sprintf→format
; io.err 明確模組前綴，不會與 Option 構造函數 err() 衝突
```

### math — 数学関数（関数形式 API）

std の数値系の「関数形式」API はすべて math モジュール（std/math.no）に集約されています。「メソッド形式」（`*.to-str`、f64/f32 の sqrt/sin/exp…、整数の sqrt/is-prime）はすべて number モジュールにあります（下の number 節を参照）。union 型とメンバーメソッド：num → std/num.no、int → std/int.no、float → std/float.no。

**定数：** `math.PI`、`math.E`、`math.LN10`（ln 10。f64-to-str の実装と対になるもの）

**比較値（num 総称型の可変長引数関数）：** `math.max`、`math.min`（`a ..num`。整数と浮動小数点数は同じ単相化パスを通ります）

**冪/根（関数）：** `math.pow`（f64 の a^b を求めるべき乗。純 .no 実装：`exp(y*ln|x|)` ＋ 負の底数に対する符号処理（整数の指数が奇数の場合は負、偶数の場合は正、非整数の指数は NaN を返す）。**libm に依存しません**）、`math.hypot`（sqrt(x*x+y*y)）

**その他の関数：** `math.atan2`、`math.fmod`（浮動小数点数の剰余）、`math.clamp`（i64 の飽和クランプ）

**整数数学（int 総称型関数）：** `math.even`、`math.odd`、`math.gcd`、`math.lcm`、`math.div`（商を取得）、`math.mod`（剰余を取得）

**型変換（旧関数形式 `math.i64-to-f64` / `math.f64-to-i64` / `math.f32-to-f64` / `math.f64-to-f32` は削除され、メソッド形式に変更）：** `v.to-f64()`、`v.to-i64()`、`f32.to-f64()` / `f64.to-f32()`（number モジュール。整数の各ビット幅に `.to-f64()` があり、i128 / u128 にも `.to-f64()` が提供されています）

**数値から文字列への変換の低層 API：** `math.char-to-str` のみが残っています（非推奨。`c.to-str()` の使用を推奨）。`math.i64-to-str` / `math.u64-to-str` / `math.f64-to-str` は削除されたため、メソッド形式の `v.to-str()` を使ってください。

**ビット操作（関数）：** `math.swap`、`math.arr-zero`、`math.rotate-left`、`math.rotate-right`、`math.store-le-u32`

> 整数冪：`math.pow-int`（int 総称型の快捷冪。f64 の `math.pow` とは異なるモジュールに分流されます）。`number.pow` は過去のアサーションに合わせて明確な i64 シグネチャを保持しています。

### number — 数値操作（メソッド形式 API）

std の数値系の「メソッド形式」API は number モジュール（std/number.no）に集約されています。関数形式 API は上の math 節を参照してください。union 型 int / float / num はそれぞれの union 節を参照してください。

**範囲定数：** i8.MIN / MAX … i128.MIN / MAX、u8.MIN / MAX … u128.MAX、`number.INF`

**整数冪（関数）：** `r = number.pow(base, n)`（base^n、n ≥ 0、快捷冪 O(log n)、i64 シグネチャ、オーバーフロー時はラップアラウンドします。int 総称型版は上の `math.pow-int` を参照）

**整数メソッド：** `n.sqrt()`（整数平方根 floor(√n)。負数は 0 を返します）、`n.is-prime()`（素数判定）。i8 / i16 / i32 / i64 / u8 / u16 / u32 / u64 の各ビット幅向けバージョンを提供しており、i128 / u128 版も提供済みです（128 ビット演算は LLVM の i128 経路を通り、利用可能です。明示的な型注釈が必要で、例えば `n i128 = .` のようにし、符号性に応じて `#{overflow=wrap}` アノテーションを正しく配置してください）

**具体型の to-str（メソッド）：** `v.to-str()`。i8 / i16 / i32 / i64 / u8 / u16 / u32 / u64 / byte / f32 / f64 の各ビット幅に対応。i128 / u128 もサポートしています（ネイティブの `@str_from_i128` / `@str_from_u128` 経由）

**浮動小数点数メソッド（f64 と同名の f32）：** 三角関数 `x.sin()`、`x.cos()`、`x.tan()`、`x.asin()`、`x.acos()`、`x.atan()`；双曲線関数 `x.sinh()`、`x.cosh()`、`x.tanh()`；丸め `x.ceil()`、`x.floor()`、`x.round()`、`x.trunc()`；指数/対数 `x.exp()`、`x.log()`、`x.log10()`、`x.log2()`；冪/根 `x.sqrt()`、`x.cbrt()`；角度/ラジアン `x.degrees()`、`x.radians()`。f32 は内部で double 精度で計算した後、f32 に狭めて返します。

### char — 文字操作

char は本質的に i32（Unicode コードポイント）であり、すべての操作はメソッド形式で提供されます：

```no
c char = 'A'
c.is-digit(); 是否為數字 (0-9)（方法）
c.is-letter(); 是否為字母 (a-z, A-Z)（方法）
c.is-alpha(); is-letter 別名（方法）
c.is-alnum(); 是否為字母或數字（方法）
c.is-space(); 是否為空白字元（方法）
c.is-upper(); 是否為大寫字母（方法）
c.is-lower(); 是否為小寫字母（方法）
c.to-upper(); 轉大寫（ASCII）（方法）
c.to-lower(); 轉小寫（ASCII）（方法）
c.to-bytes(); Unicode → UTF-8 位元組（方法）
c.to-str(); Unicode → 字串（UTF-8，方法）
```

### str — 文字列操作

`str` のディスクリプタは `{ u32 len, u32 cap, i8* data }`（64 ビット環境では 16 バイト）ですが、
`s.len()` / `s.len-bytes()` / `s.cap()` と `with-cap` / `with-len` / `with-cap-len`
の引数と戻り値は**すべて依然として `i64`** です。u32 ↔ i64 の変換はコンパイラがディスクリプタの境界で行います。
詳細は [文字列：内部レイアウトと対外 API](../lang/str.md) を参照してください。`txt` はこの変更の影響を受けません。

スライス `[]t` のディスクリプタも同型です：`%vec = { u32 len, u32 cap, i64 data }`（16 バイト。`data` は
`ptrtoint` でエンコードされたポインタです）。`v.len()` も依然として `i64` です。`[n]t` には実行時のディスクリプタがありません（要素は
`[n x t]` の中にインラインで配置され、`.len` はコンパイル時定数です）。

```no
#{fmt-skip}
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

### num — num union 型と num メソッド

`num = int | float`（std/num.no）。num 型のメソッドは num モジュールにあります（総称関数 max/min は上の math を参照）：

```no
r = num.abs(); 絕對值（方法）
r = num.clamp(lo, hi); 限制範圍（方法）
r = num.sign(); 正負號（-1/0/1，方法）
```

### int — int union 型と int メソッド

`int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128`（std/int.no）。int 型のメソッドは int モジュールにあります：

```no
s = v.to-str(); 整數轉字串（方法）
q = v.div(b); 除法取商（回傳 option）
r = v.mod(b); 取模（回傳 option）
```

### float — float union 型と float メソッド

`float = f32 | f64`（std/float.no）。float 型のメソッドは float モジュールにあります：

```no
q = v.div(b); 浮點除法
yes = v.is-nan(); NaN 判斷（方法）
yes = v.is-inf(); Inf 判斷（方法）
```

（float.to-str という union メソッドは定義しません。f64/f32 の変数は具体型のメソッド `v.to-str()` を直接使います。）

### number — メソッド使用例

（完全な API は上の「number — 数値操作（メソッド形式 API）」を参照。関数形式は「math」節を参照してください。）

```no
; 方法形式（number 模組）
s = v.to-str(); 整數/浮點轉字串（各寬度）
r = n.sqrt(); 整數平方根 floor(√n)
yes = n.is-prime(); 質數判斷（int/u8…）
q = x.sqrt(); 浮點平方根（f64/f32 方法）
q = x.floor(); 取整（f64/f32 方法）
r = number.pow(a, n); 整數冪（i64 簽名；int 泛型版為 math.pow-int）

; 函數形式（math 模組）
m = math.max(1, 2, 3); num 泛型變參
g = math.gcd(a, b); 最大公因數
l = math.lcm(a, b); 最小公倍數
pi = math.pow-int(a, n); 整數冪（int 泛型）
sw = math.swap(a, b); 交換

; 數值轉換（方法形式，舊 math.*-to-* 函數已移除）
f = v.to-f64(); 整數 → f64
n = x.to-i64(); f64 → 整數（截斷）
w = f32v.to-f64(); f32 → f64；反向 f64.to-f32()

; 範圍常數（number 模組）
i8.MIN / MAX; -128 / 127
i16.MIN / MAX; -32768 / 32767
i32.MIN / MAX; -2147483648 / 2147483647
i64.MIN / MAX; -2^63 / 2^63-1
i128.MIN / MAX; -2^127 / 2^127-1
u8.MIN / MAX; 0 / 255
u16.MIN / MAX; 0 / 65535
u32.MIN / MAX; 0 / 4294967295
u64.MIN / MAX; 0 / 2^64-1
u128.MIN / MAX; 0 / 2^128-1
number.INF; 浮點無窮大
```

### byte — バイト操作

```no
#{fmt-skip}
out = i64.to-bytes-be()         ; i64 → big-endian [8]byte
out = i64.to-bytes-le()         ; i64 → little-endian [8]byte
v = []byte.to-i64-be()          ; big-endian []byte → i64（1~8 位元組）
v = []byte.to-i64-le()          ; little-endian []byte → i64（1~8 位元組）
s = []byte.to-str()             ; []byte 轉 str（方法）
s = []byte.to-hex()             ; []byte → 大寫十六進制字串
s = []byte.to-hex-lower()       ; []byte → 小寫十六進制字串
s = byte.to-str()               ; byte 轉 str（方法）
```

### txt — 固定長の文字型

`txt` は固定 256 バイトの文字型で、短い文字列のケースに適しています：
- 先頭 255 バイトにデータを格納（`data [255]byte`）
- 最後の 1 バイトに長さを格納（`len byte`。単位は「バイト」、範囲は 0〜255）
- ヒープ割り当ては不要で、すべてスタック上に配置されます
- 型注釈が必須です

長さのセマンティクス（`str` に合わせたもの）：
- `t.len()` → 文字（コードポイント）数（`t.count()` と等価）
- `t.len-bytes()` → バイト数（内部の UTF-8 バッファの実際の長さ）
- インデックス / スライス / 追加などのバイト単位操作は `len-bytes()` を基準にします。ASCII では両者は等しく、マルチバイト UTF-8 を含む場合は `len() < len-bytes()` となります

```no
#{fmt-skip}
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
t.push(c char)              ; 追加一個碼點（整字元裝不下 255 上限則整條忽略）
t.push-str(s str)            ; 追加 str（按位元組截斷至 255）
t.push-txt(t2 txt)           ; 追加 txt（按位元組截斷至 255）
out = t.reverse()            ; 反轉
out = t.slice(start, end)   ; 截取 [start, end)
r = t.compare(b txt)        ; 字典序比較 (-1/0/1)
```

### vec — スライス操作

```no
#{fmt-skip}
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

### arr — 配列操作

```no
out = [n]
t.clone(); 複製
ok = [n]
t.eq(b); 相等比較
[n]
t.fill(val); 填充
[n]
t.reverse(); 反轉
ok = [n]
t.contains(val); 是否包含
v = [n]
t.to-vec(); 轉切片
v = [n]
t.max(); 最大值
v = [n]
t.min(); 最小值
v = [n]
t.sum(); 總和
i = [n]
t.index-of(val); 索引
v = [n]
t.last(); 最後元素
v = [n]
t.first(); 首元素
[n]
t.sort-asc(); 升序排序
[n]
t.sort-desc(); 降序排序
```

### sort — ソート定数

```no
sort.ast; 升序
sort.desc; 降序
```

---
