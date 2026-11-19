---
sidebar_position: 3.2
---

## 核心函式库

### fmt — 格式化输出

Nolang 使用**具名格式字串** `{name[:spec]}`，直接引用作用域变量，无需位置参数。输出透过 `io.out`/`io.err` 系统调用，不依赖 libc `printf`。

```no
print('x={x}'); 具名格式，自动换行（stdout）
print('result={val}', 42, 'result={val}'); 多参数：空格分隔；每个字面量各自是模板
eprint('err {x}'); 具名格式，自动换行（stderr）
print('编号 {id:06} 金额 {money:.2f}'); 支援对齐/填充/宽度/精度
s = format('x={x}'); 返回格式化字串（替代 sprintf）
io.out('no-newline-here'); 底层命令，输出不换行（stdout）
io.err('err-no-newline'); 底层命令，输出不换行（stderr）
; printf/eprintf 已移除（呼叫即编译错误 [printf-depr]）：printf→print/io.out、eprintf→eprint/io.err
; sprintf 仍可用但已废弃：sprintf→format
; io.err 明确模组前缀，不会与 Option 构造函数 err() 冲突
```

### math — 数学函数（函数形式 API）

std 数字系全部「函数形式」API 集中于 math 模组（std/math.no）；「方法形式」（`*.to-str`、f64/f32 sqrt/sin/exp…、整数 sqrt/is-prime）全部在 number 模组（见下方 number 节）。联合型别与成员方法：num → std/num.no、int → std/int.no、float → std/float.no。

**常量：** `math.PI`, `math.E`, `math.LN10`（ln 10，f64-to-str 实现配套）

**比値（num 泛型变参函数）：** `math.max`, `math.min`（`a ..num`，整数与浮点走同一单态化路径）

**幂/根（函数）：** `math.pow`（f64 求幂 a^b，纯 .no 实现：`exp(y*ln|x|)` ＋ 负底数符号处理（整数指数奇数取负、偶数取正，非整数指数回 NaN），**不依赖 libm**）, `math.hypot`（sqrt(x*x+y*y)）

**其他函数：** `math.atan2`, `math.fmod`（浮点取余）, `math.clamp`（i64 饱和箝位）

**整数数学（int 泛型函数）：** `math.even`, `math.odd`, `math.gcd`, `math.lcm`, `math.div`（取商）, `math.mod`（取模）

**型别转换（旧函数形式 `math.i64-to-f64` / `math.f64-to-i64` / `math.f32-to-f64` / `math.f64-to-f32` 已移除，改用方法形式）：** `v.to-f64()`、`v.to-i64()`、`f32.to-f64()` / `f64.to-f32()`（number 模组；整数各宽度均有 `.to-f64()`，i128 / u128 亦提供 `.to-f64()`）

**数字转字串底层：** 仅剩 `math.char-to-str`（已弃用，建议改用 `c.to-str()`）；`math.i64-to-str` / `math.u64-to-str` / `math.f64-to-str` 已移除，请用方法形式 `v.to-str()`

**位元操作（函数）：** `math.swap`, `math.arr-zero`, `math.rotate-left`, `math.rotate-right`, `math.store-le-u32`

> 整数幂：`math.pow-int`（int 泛型快速幂，与 f64 的 `math.pow` 分流不同模组）；`number.pow` 保留明确 i64 签名以符合历史断言。

### number — 数值操作（方法形式 API）

std 数字系「方法形式」API 集中在 number 模组（std/number.no）；函数形式 API 见上方 math 节。联合型别 int / float / num 见各联合节。

**范围常量：** i8.MIN / MAX … i128.MIN / MAX、u8.MIN / MAX … u128.MAX；`number.INF`

**整数幂（函数）：** `r = number.pow(base, n)`（base^n，n ≥ 0，快速幂 O(log n)，i64 签名，溢出回绕；int 泛型版见上方 `math.pow-int`）

**整数方法：** `n.sqrt()`（整数平方根 floor(√n)，负数返回 0）、`n.is-prime()`（质数判断）——提供 i8 / i16 / i32 / i64 / u8 / u16 / u32 / u64 各宽度版本，并已提供 i128 / u128 版本（128 位算术走 LLVM i128 道，可用；须显式标型区域变数如 `n i128 = .`，并按符号性正确布局 `#{overflow=wrap}` 注解）

**具体型别 to-str（方法）：** `v.to-str()`——i8 / i16 / i32 / i64 / u8 / u16 / u32 / u64 / byte / f32 / f64 各宽度；i128 / u128 亦支持（经原生 `@str_from_i128` / `@str_from_u128`）

**浮点方法（f64 及同名 f32）：** 三角 `x.sin()`, `x.cos()`, `x.tan()`, `x.asin()`, `x.acos()`, `x.atan()`；双曲 `x.sinh()`, `x.cosh()`, `x.tanh()`；取整 `x.ceil()`, `x.floor()`, `x.round()`, `x.trunc()`；指数/对数 `x.exp()`, `x.log()`, `x.log10()`, `x.log2()`；幂/根 `x.sqrt()`, `x.cbrt()`；角度/弧度 `x.degrees()`, `x.radians()`。f32 内部以 double 精度计算后收窄回 f32。

### char — 字元操作

char 本质为 i32（Unicode 码点），所有操作以方法形式提供：

```no
c char = 'A'
c.is-digit(); 是否为数字 (0-9)（方法）
c.is-letter(); 是否为字母 (a-z, A-Z)（方法）
c.is-alpha(); is-letter 别名（方法）
c.is-alnum(); 是否为字母或数字（方法）
c.is-space(); 是否为空白字元（方法）
c.is-upper(); 是否为大写字母（方法）
c.is-lower(); 是否为小写字母（方法）
c.to-upper(); 转大写（ASCII）（方法）
c.to-lower(); 转小写（ASCII）（方法）
c.to-bytes(); Unicode → UTF-8 位元组（方法）
c.to-str(); Unicode → 字串（UTF-8，方法）
```

### str — 字串操作

`str` 描述符是 `{ u32 len, u32 cap, i8* data }`（64 位元下 16 字节），但
`s.len()` / `s.len-bytes()` / `s.cap()` 及 `with-cap` / `with-len` / `with-cap-len`
的参数与回传值**全部仍是 `i64`**：u32 ↔ i64 的转换由编译器在描述符边界完成。
详见 [字串：内部布局与对外 API](../lang/str.md)。`txt` 不受此变更影响。

切片 `[]t` 的描述符同构：`%vec = { u32 len, u32 cap, i64 data }`（16 字节，`data` 是
`ptrtoint` 编码的指标），`v.len()` 也仍是 `i64`。`[n]t` 没有运行时描述符（元素内联在
`[n x t]` 里，`.len` 是编译期常量）。

```no
#{fmt-skip}
ok = a.eq(b, n)               ; 相等比较（方法）
dst = s.copy()                ; 字串复制（方法）
s.fill(val byte)              ; 填充 byte 值（方法）
pos = s.index(sub)            ; 子字串位置
ok = s.contains(sub)          ; 是否包含
ok = s.starts-with(sub)       ; 前缀判断
ok = s.ends-with(sub)         ; 后缀判断
s.to-upper()                  ; 转大写
s.to-lower()                  ; 转小写
out = s.trim()                ; 去首尾空白
out = s.repeat(n)             ; 重复
out = s.slice(start, end)     ; 切片（下标为码点位置）
b = s.to-bytes()              ; 转 []byte
s = b.to-str()                ; []byte 转 str（方法）
v = s.to-i64()                ; 字串转 i64（回传 ?i64）
v = s.to-i8()                 ; 字串转 i8（回传 ?i8）
v = s.to-i16()                ; 字串转 i16（回传 ?i16）
v = s.to-i32()                ; 字串转 i32（回传 ?i32）
v = s.to-u8()                 ; 字串转 u8（回传 ?u8）
v = s.to-u16()                ; 字串转 u16（回传 ?u16）
v = s.to-u32()                ; 字串转 u32（回传 ?u32）
v = s.to-u64()                ; 字串转 u64（回传 ?u64）
v = s.to-byte()               ; 字串转 byte（回传 ?byte）
v = s.to-f64()                ; 字串转 f64（回传 ?f64）
v = s.to-bool()               ; 字串 "true"/"false" 转 bool（回传 ?bool）
s = v.to-str()                ; i64 转字串（方法）
out = s.reverse()             ; 反转
c = s.compare(b)              ; 字典序比较
n = s.count()                 ; code point 总数
val = s.replace-char(old, new) ; 取代字元（返回结果字串）
out = s.trim-char(c)          ; 去指定字元
ok = s.empty()                ; 是否为空
s.clear()                     ; 清空（len=0，原地修改）
s = with-cap(cap)            ; 内建语法：建立指定容量的新字串（len=0）
s = with-len(len)            ; 内建语法：建立指定长度的字串（len=cap）
s = with-cap-len(cap, len)   ; 内建语法：建立指定容量和长度的字串
parts = s.split(sep)          ; 用分隔符分割（返回 []str，方法）
out = ss.join(sep)            ; []str 用分隔符连接（方法）
```

### num — num 联合型别与 num 方法

`num = int | float`（std/num.no）。num 型别的方法在 num 模组（泛型函数 max/min 见上方 math）：

```no
r = num.abs(); 绝对值（方法）
r = num.clamp(lo, hi); 限制范围（方法）
r = num.sign(); 正负号（-1/0/1，方法）
```

### int — int 联合型别与 int 方法

`int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128`（std/int.no）。int 型别的方法在 int 模组：

```no
s = v.to-str(); 整数转字串（方法）
q = v.div(b); 除法取商（回传 option）
r = v.mod(b); 取模（回传 option）
```

### float — float 联合型别与 float 方法

`float = f32 | f64`（std/float.no）。float 型别的方法在 float 模组：

```no
q = v.div(b); 浮点除法
yes = v.is-nan(); NaN 判断（方法）
yes = v.is-inf(); Inf 判断（方法）
```

（不定义 float.to-str 联合方法；f64/f32 变数直接用具体型别方法 `v.to-str()`。）

### number — 方法用法范例

（完整 API 见上方「number — 数值操作（方法形式 API）」；函数形式见「math」节。）

```no
; 方法形式（number 模组）
s = v.to-str(); 整数/浮点转字串（各宽度）
r = n.sqrt(); 整数平方根 floor(√n)
yes = n.is-prime(); 质数判断（int/u8…）
q = x.sqrt(); 浮点平方根（f64/f32 方法）
q = x.floor(); 取整（f64/f32 方法）
r = number.pow(a, n); 整数幂（i64 签名；int 泛型版为 math.pow-int）

; 函数形式（math 模组）
m = math.max(1, 2, 3); num 泛型变参
g = math.gcd(a, b); 最大公因数
l = math.lcm(a, b); 最小公倍数
pi = math.pow-int(a, n); 整数幂（int 泛型）
sw = math.swap(a, b); 交换

; 数值转换（方法形式，旧 math.*-to-* 函数已移除）
f = v.to-f64(); 整数 → f64
n = x.to-i64(); f64 → 整数（截断）
w = f32v.to-f64(); f32 → f64；反向 f64.to-f32()

; 范围常数（number 模组）
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
number.INF; 浮点无穷大
```

### byte — 位元组操作

```no
#{fmt-skip}
out = i64.to-bytes-be()         ; i64 → big-endian [8]byte
out = i64.to-bytes-le()         ; i64 → little-endian [8]byte
v = []byte.to-i64-be()          ; big-endian []byte → i64（1~8 位元组）
v = []byte.to-i64-le()          ; little-endian []byte → i64（1~8 位元组）
s = []byte.to-str()             ; []byte 转 str（方法）
s = []byte.to-hex()             ; []byte → 大写十六进制字串
s = []byte.to-hex-lower()       ; []byte → 小写十六进制字串
s = byte.to-str()               ; byte 转 str（方法）
```

### txt — 固定长度文字类型

`txt` 是固定 256 字节的文字类型，适合短文字场景：
- 前 255 字节存储数据（`data [255]byte`）
- 最后 1 字节存储长度（`len byte`，单位是「字节」，范围 0-255）
- 无需堆分配，全部在栈上
- 必须类型标注

长度语义（与 `str` 对齐）：
- `t.len()` → 字元（code point）数量（等价 `t.count()`）
- `t.len-bytes()` → 字节数量（底层 UTF-8 缓冲区实际长度）
- 索引 / 切片 / 追加等字节操作以 `len-bytes()` 为准；ASCII 下两者相等，含多字节 UTF-8 时 `len() < len-bytes()`

```no
#{fmt-skip}
t txt = 'hello'              ; 必须类型标注
n = t.len()                  ; 字元（code point）数量（i64）
b = t.len-bytes()            ; 字节数量（i64）
c = t[0]                     ; 索引存取（byte）
ok = t.eq(b txt)             ; 相等比较
dst = t.copy()               ; 复制
s = t.to-str()              ; 转 str
out = t.to-bytes()           ; 转 []byte
pos = t.index(sub txt)       ; 查找子串
ok = t.contains(sub txt)     ; 是否包含
ok = t.starts-with(sub txt)  ; 前缀检查
ok = t.ends-with(sub txt)    ; 后缀检查
t.push(c char)              ; 追加一个码点（整字元装不下 255 上限则整条忽略）
t.push-str(s str)            ; 追加 str（按位元组截断至 255）
t.push-txt(t2 txt)           ; 追加 txt（按位元组截断至 255）
out = t.reverse()            ; 反转
out = t.slice(start, end)   ; 截取 [start, end)
r = t.compare(b txt)        ; 字典序比较 (-1/0/1)
```

### vec — 切片操作

```no
#{fmt-skip}
v = vec.vec-create(n, val)         ; 建立长度 n 的切片，全部填充 val
ok = []t.eq(a, b, n)           ; 相等比较
n = []t.len()                  ; 长度
[]t.push(val)                   ; 追加（自动扩容）
[]t.clear()                     ; 清空（len=0，cap/data 不变）
v = with-cap(cap)             ; 内建语法：建立指定容量的新切片（len=0）
v = with-len(len)             ; 内建语法：建立指定长度的切片（len=cap）
v = with-cap-len(cap, len)    ; 内建语法：建立指定容量和长度的切片
val, new-n = []t.pop()         ; 弹出
found = []t.contains(n, val)   ; 是否包含（n 为长度）
[]t.reverse(n)                  ; 反转前 n 个元素
[]t.clone(dst)                  ; 复制到 dst
[]t.fill(n, val)                ; 前 n 个元素填充
arr = []t.to-arr()             ; 转阵列
[]t.sort-asc()                  ; 升序排序（方法）
[]t.sort-desc()                 ; 降序排序（方法）
```

### arr — 阵列操作

```no
out = [n]
t.clone(); 复制
ok = [n]
t.eq(b); 相等比较
[n]
t.fill(val); 填充
[n]
t.reverse(); 反转
ok = [n]
t.contains(val); 是否包含
v = [n]
t.to-vec(); 转切片
v = [n]
t.max(); 最大值
v = [n]
t.min(); 最小值
v = [n]
t.sum(); 总和
i = [n]
t.index-of(val); 索引
v = [n]
t.last(); 最后元素
v = [n]
t.first(); 首元素
[n]
t.sort-asc(); 升序排序
[n]
t.sort-desc(); 降序排序
```

### sort — 排序常量

```no
sort.ast; 升序
sort.desc; 降序
```

---
