---
sidebar_position: 3
---

# 標準庫

Nolang 標準庫（`src/std/`）包含 50+ 個模組，涵蓋格式化、數學、字串、資料結構、編解碼、加密、壓縮、檔案操作等。

使用方式：`use std/xxx`（核心模組無需 `use`）。

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

- **切片 `[]t`**：底層 `{ t*, i64 }`（data, len）
- **陣列 `[N]t`**：LLVM 固定大小陣列
- **字串 `str`**：union 型別（short ≤127 byte 存棧上 / long 存堆上），支援 `s[i]`、`s[i..j]`、`s + t`
- **列舉/Union**：`option { ok t, nil bool, err str }`（tagged enum）
- **結構體**：`point { x i64, y i64 }`
- **配列**：底層 linked-hash-map
- **迭代器**：`iterator { next()(val i64, ok i64) }`

### option — 選項型別

`option<T>` 標籤列舉（tag=0=val, 1=nil, 2=err）：

```nolang
x ?t                // 宣告 option<t>
x = val(42)         // 設為有值
x = nil             // 設為空
x = err('msg')      // 設為錯誤
x { val-> f(.); nil->; err-> g(.) }  // match
!x                  // 強制解包（panic if nil/err）
```

---

## 核心函式庫

### fmt — 格式化輸出

```nolang
printf(fmt str, ...)    // 格式化輸出
print(...)              // 列印並換行
println-empty()         // 列印空行
```

### math — 數學函數

**常量：** `PI`, `E`

**基礎：** `abs`, `sqrt`

**三角：** `sin`, `cos`, `tan`, `asin`, `acos`, `atan`, `atan2`, `degrees`, `radians`

**雙曲：** `sinh`, `cosh`, `tanh`

**取整：** `ceil`, `floor`, `round`, `trunc`

**指數/對數：** `exp`, `log`, `log10`, `log2`, `pow`, `hypot`, `cbrt`

**其他：** `fmod`, `max`, `min`

### strconv — 字串轉換

```nolang
// 字串 → 數值
str-to-i8(s), str-to-i16(s), str-to-i32(s), str-to-i64(s)
str-to-u8(s), str-to-u16(s), str-to-u32(s), str-to-u64(s)
str-to-f32(s), str-to-f64(s)
str-to-bool(s), str-to-byte(s), str-to-char(s)

// 數值 → 字串（也可用方法形式: v.to-str()）
i8-to-str(v), i16-to-str(v), i32-to-str(v), i64-to-str(v)
u8-to-str(v), u16-to-str(v), u32-to-str(v), u64-to-str(v)
f32-to-str(v), f64-to-str(v)
bool-to-str(v), byte-to-str(v), char-to-str(v)
```

### char — 字元操作

```nolang
char.to-bytes()(out []byte)      // Unicode → UTF-8 位元組（方法）
char.to-str()(s)                 // Unicode → 字串（UTF-8，方法）
char.is-digit(c)(ok)             // 是否為數字 (0-9)
char.is-alpha(c)(ok)             // 是否為字母 (a-z, A-Z)
char.is-alnum(c)(ok)             // 是否為字母或數字
char.is-space(c)(ok)             // 是否為空白字元
char.to-upper(c)(r)              // 轉大寫（ASCII）
char.to-lower(c)(r)              // 轉小寫（ASCII）
```

### str — 字串操作

```nolang
a.eq(b, n)(ok)                // 相等比較（方法）
s.copy()(dst)                 // 字串複製（方法）
s.fill(val byte)              // 填充 byte 值（方法）
s.index(sub)(pos)             // 子字串位置
s.contains(sub)(ok)           // 是否包含
s.starts-with(sub)(ok)        // 前綴判斷
s.ends-with(sub)(ok)          // 後綴判斷
s.to-upper()                  // 轉大寫
s.to-lower()                  // 轉小寫
s.trim()(out)                 // 去首尾空白
s.repeat(n)(out)              // 重複
s.slice(start, end)(out)      // 切片
s.to-bytes()(b)               // 轉 []byte
b.to-str()(s)                 // []byte 轉 str（方法）
s.to-i64()(v)                 // 字串轉 i64（回傳 ?i64）
s.to-i8()(v)                  // 字串轉 i8（回傳 ?i8）
s.to-i16()(v)                 // 字串轉 i16（回傳 ?i16）
s.to-i32()(v)                 // 字串轉 i32（回傳 ?i32）
s.to-u8()(v)                  // 字串轉 u8（回傳 ?u8）
s.to-u16()(v)                 // 字串轉 u16（回傳 ?u16）
s.to-u32()(v)                 // 字串轉 u32（回傳 ?u32）
s.to-u64()(v)                 // 字串轉 u64（回傳 ?u64）
s.to-byte()(v)                // 字串轉 byte（回傳 ?byte）
s.to-f64()(v)                 // 字串轉 f64（回傳 ?f64）
s.to-bool()(v)                // 字串 "true"/"false" 轉 bool（回傳 ?bool）
v.to-str()(s)                 // i64 轉字串（方法）
s.reverse()(out)              // 反轉
s.compare(b)(c)               // 字典序比較
s.count()(n)                  // code point 總數
s.replace-char(old, new)(val) // 取代字元（返回結果字串）
s.trim-char(c)(out)           // 去指定字元
s.empty()(ok)                 // 是否為空
```

### number — 數值操作

```nolang
max(a, b), min(a, b)          // 大小值
num.clamp(lo, hi)(r)          // 限制範圍（方法）
abs(a)(r)                     // 絕對值（num 泛型）
num.sign()(r)                 // 正負號（-1/0/1，方法）
even(v), odd(v)               // 奇偶判斷
gcd(a, b), lcm(a, b)          // 最大公因數/最小公倍數
pow(a, n)(r)                  // 整數冪
i64-to-f64(v), f64-to-i64(v)  // 數值轉換
int.to-str()(s)               // i64 轉字串（方法）
div(a, b)(q), mod(a, b)(r)    // 除法取商 / 取模
swap(a, b)                    // 交換
float.is-nan()(yes)           // NaN 判斷（方法）
float.is-inf()(yes)           // Inf 判斷（方法）

// 範圍常數
i8.MIN / MAX                  // -128 / 127
i16.MIN / MAX                 // -32768 / 32767
i32.MIN / MAX                 // -2147483648 / 2147483647
i64.MIN / MAX                 // -2^63 / 2^63-1
u8.MIN / MAX                  // 0 / 255
u16.MIN / MAX                 // 0 / 65535
u32.MIN / MAX                 // 0 / 4294967295
u64.MIN / MAX                 // 0 / 2^64-1
```

### byte — 位元組操作

```nolang
i64.to-bytes-be()(out)          // i64 → big-endian [8]byte
i64.to-bytes-le()(out)          // i64 → little-endian [8]byte
[]byte.to-i64-be()(v)           // big-endian []byte → i64（1~8 位元組）
[]byte.to-i64-le()(v)           // little-endian []byte → i64（1~8 位元組）
[]byte.to-str()(s)              // []byte 轉 str（方法）
[]byte.to-hex()(s)              // []byte → 大寫十六進制字串
[]byte.to-hex-lower()(s)        // []byte → 小寫十六進制字串
byte.to-str()(s)                // byte 轉 str（方法）
```

### vec — 切片操作

```nolang
vec-create(n, val)(v)           // 建立長度 n 的切片，全部填充 val
[]t.eq(a, b, n)(ok)            // 相等比較
[]t.len()(n)                    // 長度
[]t.push(val)                   // 追加
[]t.pop()(val, new-n)           // 彈出
[]t.contains(n, val)(found)     // 是否包含（n 為長度）
[]t.reverse(n)                  // 反轉前 n 個元素
[]t.clone(dst)                  // 複製到 dst
[]t.fill(n, val)                // 前 n 個元素填充
[]t.to-arr()(arr)               // 轉陣列
[]t.sort-asc()                  // 升序排序（方法）
[]t.sort-desc()                 // 降序排序（方法）
```

### arr — 陣列操作

```nolang
[n]t.clone()(out)               // 複製
[n]t.eq(b)(ok)                  // 相等比較
[n]t.fill(val)                  // 填充
[n]t.reverse()                  // 反轉
[n]t.contains(val)(ok)          // 是否包含
[n]t.to-vec()(v)                // 轉切片
[n]t.max()(v)                   // 最大值
[n]t.min()(v)                   // 最小值
[n]t.sum()(v)                   // 總和
[n]t.index-of(val)(i)           // 索引
[n]t.last()(v)                  // 最後元素
[n]t.first()(v)                 // 首元素
[n]t.sort-asc()                 // 升序排序
[n]t.sort-desc()                // 降序排序
```

### sort — 排序常量

```nolang
sort.ast                         // 升序
sort.desc                        // 降序
```

---

## 作業系統與檔案

### os — 作業系統介面

```nolang
// 環境變數
get-env(key)(val)
set-env(key, val)

// 目錄
get-wd()(path)
ch-dir(path)

// 檔案/目錄操作
mkdir(path, mode)
remove(path)
rename(old, new)
is-file(path)(ok)

// 檔案 I/O
open-read(path)(fd)
open-write(path)(fd)
read(fd)(data)
write(fd, data)(n)
close(fd)

// 行程
exit(code)
get-pid()(pid)

// 系統
host-name()(name)
now()(sec)
sleep(sec)

// 其他
args()(v), arg(i)(v)
is-dir(path)(ok)
stat-size(path)(sz)
file-size(fd)(sz)
get-line()(line)
copy-file(src, dst)
```

### fs — 簡化檔案操作

```nolang
read-file(path)(data, n)
read-file-n(path, n)(data)
write-file(path, data, n)
append-file(path, data, n)
copy(src, dst)
move(src, dst)
delete(path)
exists(path)(ok)
is-directory(path)(ok)
file-size(path)(sz)
make-dir(path)
chdir(path)
getwd()(path)
```

### env — 環境變數（簡化封裝）

```nolang
get(key)(val)
lookup(key)(val, ok)
set(key, val)
unset(key)
get-with-default(key, default)(val)
is-set(key)(ok)
```

### args — 命令列引數

```nolang
count()(n)
get(i)(arg)
program()(name)
has-flag(name)(ok)
get-option(name)(val)
get-positional(i)(arg)
```

### path — 路徑操作

```nolang
SEP = 47     // '/'（ASCII）
DOT = 46     // '.'

// 接收者為 path（str 類型），所有方法以 `str.path-` 為前綴
path.join(b)                      // 拼接兩個路徑（原地修改）
path.base() (out)                 // 取檔名
path.dir()                        // 取目錄（原地修改）
path.ext() (out)                  // 取副檔名
path.is-abs() (yes)                // 是否為絕對路徑
path.clean()                        // 正規化（原地修改）
str.path-split() (d, f) (dn, fn)   // 分割為目錄 + 檔名
```

### bufio — 緩衝讀取

```nolang
reader.init(fd, buf)(r)         // 初始化緩衝讀取器（傳回 reader）
reader.fill()(ok)               // 填充緩衝區
reader.read-byte()(b, ok)       // 讀取一個位元組
reader.read-line(line)(ok)      // 讀取一行到 line
reader.close()                  // 關閉
```

---

## 時間與日期

### time — 時間操作

```nolang
now-s()(sec)                    // 目前 Unix 時間戳（秒）
now-ms()(ms)                    // 目前時間戳（毫秒）
now-us()(us)                    // 目前時間戳（微秒）
format-time(t, fmt)(out)        // 格式化時間
sleep-ms(ms)                    // 睡眠（毫秒）
sleep-us(us)                    // 睡眠（微秒）
duration-between(start, end)(d) // 耗時（秒）
duration-ms-between(s, e)(d)    // 耗時（毫秒）
```

---

## 日誌

### log — 分級日誌

```nolang
LEVEL-DEBUG = 0
LEVEL-INFO  = 1
LEVEL-WARN  = 2
LEVEL-ERROR = 3
LEVEL-FATAL = 4

set-level(lvl)
debug(msg), info(msg), warn(msg), error(msg), fatal(msg)
```

---

## 資料結構

### set — 集合（基於陣列）

```nolang
add(s, n, val)(new-n)           // 新增元素
set-remove(s, n, val)(new-n)        // 移除元素
contains(s, n, val)(ok)         // 是否包含
union(a, an, b, bn)(new-an)     // 聯集
intersection(a, an, b, bn)(out, n)// 交集
difference(a, an, b, bn)(out, n)  // 差集
to-vec(s, n)(v)                 // 轉切片
set-size(s, n)(sz)                   // 元素個數
set-empty(s, n)(yes)                    // 是否為空
```

### deque — 雙端佇列

```nolang
push-front(d, n, val)(new-n)
push-back(d, n, val)(new-n)
pop-front(d, n)(val, new-n)
pop-back(d, n)(val, new-n)
peek-front(d, n)(val)
peek-back(d, n)(val)
size(n)(n)
empty(n)(ok)
clear()(n)
```

### heap — 最小堆

```nolang
push(h, n, val)(new-n)
pop(h, n)(val, new-n)
peek(h, n)(val)
size(n)(n)
empty(n)(ok)
```

### map/linked-hash-map — 有序哈希表

固定容量 64（i64→i64），線性探測，雙向鏈表保持插入順序：

```nolang
m = linked-hash-map{}
m.init()
m.put(key, val, is-new)
m.get(key, found, result)
m.contains(key, found)
m.remove(key, is-new)
m.clear()
m.len(n)
m.is-empty(empty)
m.for-each(key, val)
```

### map/hash-set — i64 哈希集合

固定容量 64，線性探測，O(1) 查找/插入/刪除：

```nolang
s = hash-set{}
s.init()
s.add(val, is-new)
s.contains(val, found)
s.remove(val, removed)
s.clear()
s.len(n)
s.is-empty(empty)
s.for-each(val)
```

### map/str-map — str→str 哈希映射表

固定容量 256，FNV-1a 雜湊，線性探測：

```nolang
m = str-map{}
m.init()
m.put('key', 'val', is-new)
m.get('key', found, result)
m.contains('key', found)
m.remove('key', removed)
m.clear()
m.len(n)
m.is-empty(empty)
m.for-each(k, v)
```

### map/str-set — str 哈希集合

固定容量 256，FNV-1a 雜湊，字串去重：

```nolang
s = str-set{}
s.init()
s.add('hello', is-new)
s.contains('hello', found)
s.remove('hello', removed)
s.clear()
s.len(n)
s.is-empty(empty)
s.for-each(val)
```

---

## 編碼

### encoding/hex — 十六進制

```nolang
// 編碼（定義於 byte 模組）
data.to-hex()(out)                  // []byte → 大寫 hex str
data.to-hex-lower()(out)            // []byte → 小寫 hex str

// 解碼（定義於 str 模組）
s.from-hex()(out)                   // hex str → ?[]byte（nil=空, err=無效字元）
```

### encoding/base64 — Base64（RFC 4648）

```nolang
BASE64-STD = 'ABC...+/'
BASE64-URL = 'ABC...-_'
PAD = 61  // '='

encode(data, n, table, out)(out-n)    // Base64 編碼
encode-std(data, n, out)(out-n)       // 標準編碼
encode-url(data, n, out)(out-n)       // URL 安全編碼
decode(s, n, table, out)(out-n, ok)   // Base64 解碼
```

### encoding/csv — CSV 解析（RFC 4180）

```nolang
parse-field(s, sn, pos, field)(fn, new-pos)  // 解析單個欄位
parse-line(s, sn, fields, max)(n)             // 解析一行
encode-field(field, fn, out)(out-n)           // 編碼欄位
```

---

## 歸檔

### archive/tar — TAR 歸檔（POSIX ustar）

```nolang
// 讀取普通 tar
archive = tar{ data: raw-bytes }
archive.count()(count)
archive.entry(idx)(e)
archive.name(idx)(name)
archive.size(idx)(sz)
archive.type(idx)(typ)              // "file" / "dir" / "unknown"
archive.is-dir(idx)(yes)
archive.is-file(idx)(yes)
archive.read(idx)(out)
archive.mode(idx)(mode)
archive.mtime(idx)(ts)

// 讀取 .tar.gz（自動解壓縮）
archive = tar-open-gz(gz-data)

// tar-entry 方法
e.name()(name)
e.size()(sz)
e.type()(typ)
e.read()(out)

// 寫入 tar
builder = tar-builder{}
builder.add-file(name, content)
builder.add-dir(name)
builder.finish()(archive)
```

### archive/zip — ZIP 歸檔解析

```nolang
archive = zip{ data: raw-bytes }
archive.count()(count)                        // 條目數
archive.entry(idx)(e)                         // 取得 zip-entry
archive.name(idx)(name)                       // 檔名
archive.size(idx)(sz)                         // 原始大小
archive.compressed-size(idx)(csz)             // 壓縮後大小
archive.method(idx)(method)                   // 0=stored, 8=deflate
archive.extract(idx)(out)                     // stored 和 deflate 模式

// zip-entry 方法
e.name()(name)
e.size()(sz)
e.compressed-size()(csz)
e.method()(method)
e.extract()(out)
```

### archive/gzip — GZIP 壓縮與原始 DEFLATE

```nolang
gzip-compress(data)(out)                      // zlib 壓縮
gzip-decompress(data)(out)                    // zlib 解壓縮
inflate-decompress(data, out-size)(out)       // 原始 DEFLATE 解壓縮（ZIP method 8）
```

---

## 密碼學與雜湊

### hash/aes — AES-128 加解密（ECB 模式）

```nolang
aes-128-enc(plain, 16, key, out)   // 加密 16-byte 區塊
aes-128-dec(cipher, 16, key, out)  // 解密 16-byte 區塊
```

另含獨立模組 `hash/aes-128-enc` 和 `hash/aes-128-dec`。

### hash/des — DES 加解密（ECB 模式）

```nolang
des-enc(plain, 8, key, out)        // 加密 8-byte 區塊
des-dec(cipher, 8, key, out)       // 解密 8-byte 區塊
```

另含獨立模組 `hash/des-enc` 和 `hash/des-dec`。

### hash/rsa — RSA 模冪運算

```nolang
rsa-modpow(base, bn, exp, en, mod, mn, result, rn)
```

不包含金鑰生成，支援 1024~4096-bit。

### hash/md5 — MD5（128-bit）

```nolang
md5(data)(out [16]byte)
```

### hash/sha1 — SHA-1（160-bit）

```nolang
sha1(block [16]i64, n, h0, h1, h2, h3, h4)
```

處理單個 512-bit 區塊，多區塊需呼叫者自行填充累加。

### hash/sha256 — SHA-256（256-bit）

```nolang
sha256(block [16]i64, n, h0, h1, h2, h3, h4, h5, h6, h7)
```

### hash/sha512 — SHA-512（512-bit）

```nolang
sha512(block [16]i64, n, h0, h1, h2, h3, h4, h5, h6, h7)
```

### hash/crc-32 — CRC32 校驗

```nolang
crc-32(s []byte, n, crc)
```

### hash/fnv-1a-32 — FNV-1a 非加密雜湊

```nolang
fnv-1a-32(s []byte, n, h)
```

### hash/rand — 隨機數產生器（xorshift32）

```nolang
rand(state)(r)                     // 32-bit 偽隨機數
rand-str(state, n, s)              // 隨機字母數字字串
```

### hash/x509 — X.509 憑證 DER 解析

```nolang
der-tag(data, pos)(tag)
der-len(data, pos)(len, adv)
x509-fingerprint(cert, n, h0..h7)  // SHA-256 憑證指紋
x509-rsa-e(cert, n, e)             // RSA 公鑰指數提取
```

---

## 資料交換

### json — JSON 解析與產生

```nolang
// 型別常量
KIND-NULL, KIND-BOOL, KIND-NUM, KIND-STR, KIND-ARR, KIND-OBJ
JSON-NULL, JSON-TRUE, JSON-FALSE

// 解析
parse(s, n)(v json-value)          // 完整解析
parse-str(s, n)(v)                 // 解析字串值
parse-num(s, n)(v)                 // 解析數值值

// 產生
stringify(v json-value, out)(n)    // 序列化

// 存取
get-key(v json-value, key)(val)    // 取得物件屬性
set-key(v json-value, key, val)    // 設定物件屬性
```

---

## 其他

### unicode — Unicode 支援

```nolang
decode-rune(s, pos)(rune, size)     // 解碼 UTF-8 字元
encode-rune(rune, out)(n)           // 編碼 UTF-8 字元
rune-count(s, n)(count)            // 字元計數
is-letter(rune)(ok)                // 是否為字母
is-digit(rune)(ok)                 // 是否為數字
is-space(rune)(ok)                 // 是否為空白
is-alnum(rune)(ok)                 // 是否為字母數字
is-upper(rune), is-lower(rune)     // 大小寫判斷
to-upper-rune(rune), to-lower-rune(rune)  // 大小寫轉換
```

### uuid — UUID v4 產生與解析

```nolang
new-v4(state)(out)                  // 產生 UUID v4
uuid.to-str(out)(out-n)             // 轉小寫字串（方法）
uuid.to-str-upper(out)(out-n)       // 轉大寫字串（方法）
from-str(s, sn, out)(ok)            // 從字串解析（支援連字號/不帶）
parse-with-dashes(s, pos, out)(ok)  // 含連字號解析
parse-no-dashes(s, pos, out)(ok)    // 無連字號解析
uuid.validate()(ok)                 // 驗證 UUID 格式（方法）
uuid.version()(v)                   // 取得版本（方法）
uuid.variant()(v)                   // 取得變體（方法）
uuid.is-nil()(yes)                  // 是否為 nil（方法）
uuid.eq(b)(yes)                     // 相等比較（方法）
uuid.cmp(b)(r)                      // 比較（方法）
nil-uuid(out)                       // 回傳 nil UUID
```

### bigint — 任意精度整數

```nolang
// 型別
bigint { sign i64, limbs []i64, len i64 }

// 建構
from-i64(v)(out), from-u64(v)(out)
zero()(out), one()(out)
copy(a)(out)

// 比較
cmp(a, b)(r), eq(a, b)(r)
is-zero(a)(r), is-neg(a)(r), is-pos(a)(r)

// 運算
add(a, b)(c), sub(a, b, c)
mul(a, b)(c)
div-mod(a, b)(q, r), mod(a, b, r)
div-i64(a, v, q), mod-i64(a, v)(r)
pow(a, n)(c), mod-pow(base, exp, mod, r)

// 數論
gcd(a, b, g), lcm(a, b, l)

// 移位
shl(a, n, c), shr(a, n, c)

// 字串轉換
to-str(a, out)(n), from-str(s, sn, out)
to-hex(a, out)(n), from-hex(s, sn, out)

// 小整數輔助
add-i64(a, v, c), mul-i64(a, v, c)
```

### enter / leave — 生命週期鉤子

```nolang
enter { enter() }     // 啟動時執行
leave { leave() }     // 退出時執行
```

---

## 模組一覽

| 模組                | 路徑   | 說明             |
| ------------------- | ------ | ---------------- |
| fmt                 | 核心   | 格式化輸出       |
| math                | 核心   | 數學函數         |
| strconv             | 核心   | 字串/數值轉換    |
| str                 | 核心   | 字串操作         |
| vec                 | 核心   | 切片（[]t）操作  |
| arr                 | 核心   | 陣列（[n]t）操作 |
| number              | 核心   | 數值工具函數     |
| byte                | 核心   | 位元組操作       |
| char                | 核心   | 字元操作         |
| os                  | 核心   | 作業系統介面     |
| env                 | 核心   | 環境變數封裝     |
| fs                  | 核心   | 簡化檔案操作     |
| args                | 核心   | 命令列引數       |
| path                | 核心   | 路徑處理         |
| bufio               | 核心   | 緩衝讀取         |
| time                | 核心   | 時間操作         |
| log                 | 核心   | 分級日誌         |
| json                | 核心   | JSON 解析/產生   |
| types               | 核心   | 型別定義文件     |
| option              | 核心   | 選項型別         |
| sort                | 核心   | 排序常量         |
| set                 | 核心   | 集合             |
| deque               | 核心   | 雙端佇列         |
| heap                | 核心   | 最小堆           |
| unicode             | 核心   | Unicode 支援     |
| uuid                | 核心   | UUID v4          |
| bigint              | 核心   | 任意精度整數     |
| enter               | 核心   | 啟動鉤子         |
| leave               | 核心   | 退出鉤子         |
| encoding/hex        | 子模組 | 十六進制編解碼   |
| encoding/base64     | 子模組 | Base64 編解碼    |
| encoding/csv        | 子模組 | CSV 解析         |
| archive/tar         | 子模組 | TAR 歸檔         |
| archive/zip         | 子模組 | ZIP 歸檔         |
| map/linked-hash-map | 子模組 | 有序哈希表       |
| hash/aes            | 子模組 | AES-128 加解密   |
| hash/aes-128-enc    | 子模組 | AES-128 加密     |
| hash/aes-128-dec    | 子模組 | AES-128 解密     |
| hash/des            | 子模組 | DES 加解密       |
| hash/des-enc        | 子模組 | DES 加密         |
| hash/des-dec        | 子模組 | DES 解密         |
| hash/rsa            | 子模組 | RSA 模冪         |
| hash/md5            | 子模組 | MD5 雜湊         |
| hash/sha1           | 子模組 | SHA-1 雜湊       |
| hash/sha256         | 子模組 | SHA-256 雜湊     |
| hash/sha512         | 子模組 | SHA-512 雜湊     |
| hash/crc-32         | 子模組 | CRC32 校驗       |
| hash/fnv-1a-32      | 子模組 | FNV-1a 雜湊      |
| hash/rand           | 子模組 | 隨機數產生器     |
| hash/x509           | 子模組 | X.509 DER 解析   |
