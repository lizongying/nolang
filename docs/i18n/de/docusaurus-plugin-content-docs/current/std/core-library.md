---
sidebar_position: 3.2
---

## Kernbibliothek

### fmt — Formatierte Ausgabe

Nolang verwendet **benannte Formatstrings** `{name[:spec]}`, die Variablen direkt aus dem Gültigkeitsbereich referenzieren — positionsabhängige Argumente sind nicht nötig. Die Ausgabe erfolgt über die Syscalls `io.out`/`io.err`, ohne Abhängigkeit von libc `printf`.

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

### math — Mathematische Funktionen (API in Funktionsform)

Alle numerischen APIs der std in „Funktionsform" sind im math-Modul gebündelt (std/math.no); alle APIs in „Methodenform" (`*.to-str`, sqrt/sin/exp… von f64/f32, Integer-sqrt/is-prime) befinden sich im number-Modul (siehe Abschnitt number unten). Union-Typen und ihre Elementmethoden: num → std/num.no, int → std/int.no, float → std/float.no.

**Konstanten:** `math.PI`, `math.E`, `math.LN10` (ln 10, Begleiter von f64-to-str)

**Vergleich (num-generische variadische Funktionen):** `math.max`, `math.min` (`a ..num`, Integer und Gleitkommazahlen laufen über denselben Monomorphisierungspfad)

**Potenz/Wurzel (Funktionen):** `math.pow` (f64-Potenz a^b, in reinem Nolang implementiert als `exp(y*ln|x|)` plus explizite Vorzeichenbehandlung negativer Basen — ungerader ganzzahliger Exponent behält das Vorzeichen, gerader verwirft es, nicht-ganzzahliger Exponent ergibt NaN; **keine libm-Abhängigkeit**), `math.hypot` (sqrt(x*x+y*y))

**Weitere Funktionen:** `math.atan2`, `math.fmod` (Gleitkomma-Rest), `math.clamp` (i64-Sättigungs-Clamp)

**Integer-Mathematik (int-generische Funktionen):** `math.even`, `math.odd`, `math.gcd`, `math.lcm`, `math.div` (Quotient), `math.mod` (Modulo)

**Typkonvertierung (die alten Funktionsformen `math.i64-to-f64` / `math.f64-to-i64` / `math.f32-to-f64` / `math.f64-to-f32` wurden entfernt, verwende die Methodenform):** `v.to-f64()`, `v.to-i64()`, `f32.to-f64()` / `f64.to-f32()` (number-Modul; jede Integer-Breite besitzt `.to-f64()`, auch i128 / u128 bieten `.to-f64()`)

**Zahl-zu-String auf niedriger Ebene:** Nur `math.char-to-str` bleibt (veraltet, bevorzuge `c.to-str()`); `math.i64-to-str` / `math.u64-to-str` / `math.f64-to-str` wurden entfernt, bitte die Methodenform `v.to-str()` verwenden

**Bitoperationen (Funktionen):** `math.swap`, `math.arr-zero`, `math.rotate-left`, `math.rotate-right`, `math.store-le-u32`

> Ganzzahlpotenz: `math.pow-int` (int-generische Schnellpotenz, von der f64-Variante `math.pow` in ein anderes Modul aufgeteilt); `number.pow` behält aus Gründen historischer Zusicherungen eine explizite i64-Signatur.

### number — Numerische Operationen (API in Methodenform)

Die numerischen APIs der std in „Methodenform" sind im number-Modul gebündelt (std/number.no); die APIs in Funktionsform siehe Abschnitt math oben. Die Union-Typen int / float / num werden in ihren eigenen Abschnitten behandelt.

**Bereichskonstanten:** i8.MIN / MAX … i128.MIN / MAX, u8.MIN / MAX … u128.MAX; `number.INF`

**Ganzzahlpotenz (Funktion):** `r = number.pow(base, n)` (base^n, n ≥ 0, Schnellpotenz O(log n), i64-Signatur, Überlauf wird umgebrochen; int-generische Version siehe `math.pow-int` oben)

**Integer-Methoden:** `n.sqrt()` (ganzzahlige Quadratwurzel floor(√n), negative Zahlen ergeben 0), `n.is-prime()` (Primzahlprüfung) — verfügbar in den Breiten i8 / i16 / i32 / i64 / u8 / u16 / u32 / u64 und jetzt auch i128 / u128 (128-Bit-Arithmetik läuft über den LLVM-i128-Pfad, nutzbar; erfordert explizit typisierte Lokalvariablen wie `n i128 = .` und eine korrekt platzierte `#{overflow=wrap}`-Annotation entsprechend der Vorzeichenbehaftigkeit)

**to-str für konkrete Typen (Methoden):** `v.to-str()` — Breiten i8 / i16 / i32 / i64 / u8 / u16 / u32 / u64 / byte / f32 / f64; i128 / u128 werden ebenfalls unterstützt (über native `@str_from_i128` / `@str_from_u128`)

**Gleitkomma-Methoden (f64 und gleichnamig f32):** trigonometrisch `x.sin()`, `x.cos()`, `x.tan()`, `x.asin()`, `x.acos()`, `x.atan()`; hyperbolisch `x.sinh()`, `x.cosh()`, `x.tanh()`; Runden `x.ceil()`, `x.floor()`, `x.round()`, `x.trunc()`; exponentiell/logarithmisch `x.exp()`, `x.log()`, `x.log10()`, `x.log2()`; Potenz/Wurzel `x.sqrt()`, `x.cbrt()`; Grad/Bogenmaß `x.degrees()`, `x.radians()`. f32 rechnet intern mit double-Präzision und verengt danach auf f32 zurück.

### char — Zeichenoperationen

char ist im Wesentlichen ein i32 (ein Unicode-Codepunkt); alle Operationen werden in Methodenform bereitgestellt:

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

### str — Zeichenkettenoperationen

Der `str`-Descriptor ist `{ u32 len, u32 cap, i8* data }` (16 Bytes auf 64-Bit), aber
`s.len()` / `s.len-bytes()` / `s.cap()` sowie die Parameter und Rückgabewerte von `with-cap` / `with-len` / `with-cap-len`
**bleiben durchgängig `i64`**: Die Umwandlung u32 ↔ i64 führt der Compiler an der Descriptor-Grenze durch.
Siehe [Zeichenketten: internes Layout und öffentliche API](../lang/str.md). `txt` ist von dieser Änderung nicht betroffen.

Der Descriptor des Slices `[]t` ist isomorph: `%vec = { u32 len, u32 cap, i64 data }` (16 Bytes, `data` ist ein
über `ptrtoint` kodierter Zeiger), und `v.len()` bleibt ebenfalls `i64`. `[n]t` besitzt keinen Laufzeit-Descriptor (die Elemente sind inline in
`[n x t]` enthalten, `.len` ist eine Kompilierzeitkonstante).

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

### num — num-Uniontyp und num-Methoden

`num = int | float` (std/num.no). Die Methoden des num-Typs liegen im num-Modul (die generischen Funktionen max/min siehe math oben):

```no
r = num.abs(); 絕對值（方法）
r = num.clamp(lo, hi); 限制範圍（方法）
r = num.sign(); 正負號（-1/0/1，方法）
```

### int — int-Uniontyp und int-Methoden

`int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128` (std/int.no). Die Methoden des int-Typs liegen im int-Modul:

```no
s = v.to-str(); 整數轉字串（方法）
q = v.div(b); 除法取商（回傳 option）
r = v.mod(b); 取模（回傳 option）
```

### float — float-Uniontyp und float-Methoden

`float = f32 | f64` (std/float.no). Die Methoden des float-Typs liegen im float-Modul:

```no
q = v.div(b); 浮點除法
yes = v.is-nan(); NaN 判斷（方法）
yes = v.is-inf(); Inf 判斷（方法）
```

(Es wird keine Union-Methode `float.to-str` definiert; bei f64/f32-Variablen verwende direkt die Methode des konkreten Typs `v.to-str()`.)

### number — Beispiele für die Methodenverwendung

(Die vollständige API steht oben im Abschnitt „number — Numerische Operationen (API in Methodenform)"; die Funktionsform steht im Abschnitt „math".)

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

### byte — Byte-Operationen

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

### txt — Texttyp fester Länge

`txt` ist ein Texttyp fester Länge von 256 Bytes, geeignet für kurze Zeichenketten:
- Die ersten 255 Bytes speichern die Daten (`data [255]byte`)
- Das letzte Byte speichert die Länge (`len byte`, Einheit „**Bytes**", Bereich 0–255)
- Keine Heap-Allokation nötig, alles liegt auf dem Stack
- Typannotation erforderlich

Längensemantik (an `str` angeglichen):
- `t.len()` → Anzahl der Zeichen (Codepunkte, entspricht `t.count()`)
- `t.len-bytes()` → Anzahl der Bytes (tatsächliche Länge des zugrunde liegenden UTF-8-Puffers)
- Indexierung / Slicing / Anhängen und andere Byte-Operationen richten sich nach `len-bytes()`; bei reinem ASCII sind beide gleich, bei mehrbyteigem UTF-8 gilt `len() < len-bytes()`

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

### vec — Slice-Operationen

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

### arr — Array-Operationen

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

### sort — Sortierkonstanten

```no
sort.ast; 升序
sort.desc; 降序
```

---
