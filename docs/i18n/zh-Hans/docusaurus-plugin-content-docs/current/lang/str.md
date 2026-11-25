---
sidebar_position: 3
---

# 字串

Nolang 字串（`str`）为堆分配的位元组序列，支援多种运算符与方法。

## 内部布局与对外 API

`str` 的描述符（`%str-long`）在 64 位元平台上是 **16 字节**：

```llvm
%str-long = type { i32, i32, i8* }   ; u32 len, u32 cap, data
```

| 栏位 | 内部型别 | 对外型别 | 说明 |
|---|---|---|---|
| `len` | `u32`（LLVM `i32`） | `i64` | 逻辑位元组长度；`s.len-bytes()`、`len(s)`、`s._len()` |
| `cap` | `u32`（LLVM `i32`） | `i64` | 实体容量（仅 str-long 有效）；`s.cap()`、`s._cap()` |
| `data` | `i8*` | `[]byte` / `*byte` | 指向堆缓冲区 |

内部把 `len` / `cap` 收窄成 `u32`，是为了让描述符从 24 字节缩到 16 字节（`i32` + `i32` + 8 字节对齐的指标）。**对外 API 一律仍是 `i64`**：读取时在边界 `zext i32 -> i64`，写入时 `trunc i64 -> i32`。标准库与使用者程式不需要（也不应该）碰这层转换；`s.len-bytes()`、`s.len()`、`s.cap()`、`s._len()` / `s._cap()` / `s._set-len()` / `s._set-cap()` 的签名保持 `i64`。

> ⚠️ 内部计数器是 `u32`，因此单一 `str` 的长度／容量上限是 4 GiB（`4294967295`）。传入超过此值的 `i64` 会在边界被截断；一般字串处理不受影响，但预分配 giant buffer 前应自行检查。
>
> `txt` **不受这次变更影响**：它仍是固定 256 字节的 `{ [255 x i8] data, i8 len }`，没有 `cap` 栏位，长度是 1 字节。

## 字串运算符

### 拼接（`-`）

使用 `-` 运算符拼接字串：

```no
; 字面量拼接
s = 'Hello' - ' ' - 'World'

; 与变量拼接
greeting = 'Hello, ' - name
```

### 重复（`*`）

使用 `*` 运算符重复字串：

```no
s = 'Hello' * 3
```

### 比较（`==` / `!=` / `str.compare`）

`str` **只实作相等比较**：`==` / `!=` 走 runtime 的 `@str_eq`，多字元字串完全合法。

关系运算符（<code>&lt;</code> `>` <code>&lt;=</code> `>=`）**没有字串字典序语意**，对字串使用会是编译错误：

- **合法**：**单字元**字串字面量 `'a'`，隐式视为 `char`（码点）；可与另一个单字元字面量或 `char`（`"a"`）比较。
- **错误**：多字元字串 —— `'ab'`、`str` 变数、回传 `str` 的呼叫（过去会**静默回传 false**）。

需要字典序时用 `str.compare(b)`：

```no
s = 'abc'
b = 'abd'
c = s.compare(b); -1（s < b）/ 0（相等）/ 1（s > b）

'a' <= 'z'; true — 单字元字串隐式 char
ch >= 'a'; ch 为 char，OK
s < b; 编译错误：多字元字串
s.compare(b) < 0; 正确写法
```

## 索引与切片

**索引**：`s[i]` 的推断型别是 **`?char`**（Unicode 码点），不是 `byte` —— 越界时回传 `err`（`code.index-out`），在界内回传 `ok(码点)`（与整数 `+ - * / %` 同一套统一规则）。对于含多字节字符的串，`s[i]` 会从串首前向解码 UTF-8 数到第 i 个码点（见下方效能说明）。

```no
#{fmt-skip}
s = 'Hello World'
c = s[0]: { ok -> it  -> _ }   ; 'H' 的码点（?char 必须显式解包，空臂取 '\0'）
```

> 要把 `?char` 用掉只有三条路（**`#{index-out=DEF}` 已于 D11 废弃并移除**，残留注解是编译错误；
> 也没有「显式 plain 型别拉回 plain」这条捷径，`c char = s[i]` 同样必须显式消费）：
> **match 解包**（`x = s[i]: { ok -> it, -> _ }`，空臂取 `char` 零值 `'\0'`）、
> **place-match 写入**（`s[i]: { ok -> it = x, -> _ }`）、或**简写 `?=` 上抛**（函式有 `?T` 结果参数，
> 或陈述位于块 match 主体内）。细节与范例见〈syntax · 安全索引〉。

**切片**：`s[a..b]` 回传底层字串的一个**视图**，型别仍是 **`str`**（共享记忆体，不复制）。切片下标是**码点位置**，与 `s[i]` 一致：

```no
sub = s[6..]; 'World'
sub = s[6..11]; 'World'
sub = s[0..5); 'Hello'（左闭右开）
```

> 切片的下标是**码点位置**，不是字节偏移。若需字节偏移的切片（例如配合 `s.byte(i)` 遍历），用 `s.slice-bytes(start, end)`。

```no
; 长度
n = s.len(); code point 数（Unicode 字符数）
n = s.count(); 同上（旧方法名，仍可用）
n = s.len-bytes(); byte 长度（UTF-8 位元组数）
; 注意：裸 s.len（结构体栏位）已不再支援，读写都会报错；请改用 s.len() / s.len-bytes()
```

### 隐式转换（char → str）

`char` 可以**隐式转换**为 `str`：把该码点 UTF-8 编码成一个新字串。因此下列写法都合法，**无需**手动呼叫 `char.to-str()`：

```no
s = 'héllo'; 'h' 'é' 'l' 'l' 'o'（é 为多字节）

; 1) s[i] 的 char 直接赋值给 str 变数 → 隐式转成单字元字串
a str = s[0]; 'h'

; 2) 切片结果本身型别已是 str，直接赋值即可
b str = s[0..1]; 'hé'（下标 0..1，两端皆含）

; 3) char 参与字串拼接（-）时隐式转为 str
msg = 'first: ' - s[0]; 'first: h'

; 4) char 与 str 比较时隐式转为 str
ok = s[0] == 'h'; true
```

> **`'...'` 是 `str` 字面量，`"..."` 是 `char` 字面量** —— 即使 `'x'` 只有一个码点，它仍是 `str`。因此 `+` / `-` 的**两侧都是字面量**时（`'...'` 配 `"..."`），走上面的隐式转换，结果是**拼接**而不是整数运算：
>
> ```no
> 'x' - "a"                ; "xa"（拼接；不是 120-97 = 23）
> "a" + 'x'                ; "ax"
> 'xy' + "a"               ; "xya"
> ```
>
> 一旦有一侧**不是字面量**（char 变数、`s[i]`、整数），单字元 `'...'` 就按**位元组**参与整数运算 —— 这是刻意的，也是数字字元取值惯用法能成立的原因：
>
> ```no
> c char = "5"
> n = c - '0'              ; 5（位元组相减，不是 "50"）
> m = 'A' + 1              ; 66
> "z" - "a"                ; 25（两侧都是 char 字面量，char 相减）
> 'x' - c                  ; 23（c 是变数 → 位元组运算）
> ```
>
> 反过来说，位元组退化**只在对面是整数或字元时**发生。对面若是 `bool`、`f64`、结构体这类**没有位元组语意**的值，`'...'` 仍然是字串，整个式子就是**拼接**，对面按 `to-str` 渲染：
>
> ```no
> b = true
> 'x' - b                  ; "xtrue"（不是 120 - 1 = 119）
> 'x' - 1.5                ; "x1.5"
> 'x' - 'y'                ; "xy"（两侧都是字串字面量）
> ```
>
> 因此 `'x' - b` 可以赋给 `str`：`s str = 'x' - b` 得到 `"xtrue"`；而 `s str = 'x' - 1` 仍会被拒绝，因为那是位元组运算（结果是数字）。

> 隐式转换只把**单个码点**编码为 `str`。若想要的是数值，直接用 `char` 本身（例如 `print(c)` 会输出该码点的十进位值）；若想要显式转换，仍可用 `c.to-str()`（等价于隐式转换）。
>
> `char` 代表单个 Unicode 标量码点，底层存储类型是 **`i32`**（合法范围 `0 ..= 0x10FFFF`）。
>
> `char` **可以正常参与整数运算**（如 `z = a + 25`、`u = ch - 32`），按码点值在 i32 宽度上计算；与整数家族不同，它不会预设回传 `option<int>`，因此不会触发「未处理整数溢出」的编译错误。
>
> **渲染方式**：`char` 转成文字时一律输出**码点的十进位值**（`"A"` → `65`，`"é"` → `233`），
> 且 `print(c)` 与字串插值 `print('{c}')` 的结果**必须一致**。char 算术按**有号 i32** 计，
> 所以 `"x" - "y"` 是 `-1`，两条路径都输出 `-1`（不会有一边输出 `4294967295`）。
> 合法码点恒在 `0 ..= 0x10FFFF`（第 31 位为 0），有号与无号扩展在那里等价，
> 因此这个规则只影响 char 算术越界得到的负值。若要的是**字元本身**而非数字，用
> `c.to-str()`（`"A".to-str()` → `"A"`），那是另一个操作。

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
; 比较
ok = a.eq(b, n)               ; 相等比较（方法）
c = s.compare(b)              ; 字典序比较

; 查找（index/rindex 返回「码点（字符）位置」，与 s[i] 一致）
pos = s.index(sub)             ; 首次出现的码点下标，未找到 -1
pos = s.index-from(sub, k)     ; 从第 k 个码点开始查找
pos = s.rindex(sub)            ; 最后一次出现的码点下标
pos = s.rindex-from(sub, k)    ; 从第 k 个码点反向查找
pos = s.last-index(sub)        ; rindex 的别名
ok  = s.contains(sub)          ; 是否包含
ok  = s.starts-with(sub)       ; 前缀判断
ok  = s.ends-with(sub)         ; 后缀判断
ok  = s.empty()                ; 是否为空

; 注意：index 系列返回的是「码点位置」，因此 s[s.index(sub)] 取到的是 sub 的首字符，
;       与 s[i] 的码点索引语义一致。例如 '日本语'.index('语') == 2，且 s[s.index('语')] == '语' 的码点。
;       若需「码点位置」切片，s.slice(start, end) 即码点语义（与 s[i] 一致）；
;       若需「字节偏移」切片（如配合 s.byte 遍历），用 s.slice-bytes(start, end)。
;       str 内部提供字节级查找 str.find-byte-from（供 split 等复用）。
;       注意：txt 型别的 slice/at 仍为字节语义（txt 是裸字节缓冲），与 str 语义分叉。

; 转换
out = s.to-upper()             ; 转大写
out = s.to-lower()             ; 转小写
out = s.trim()                 ; 去首尾空白
out = s.trim-char(c)           ; 去指定字元
out = s.repeat(n)              ; 重复
out = s.reverse()              ; 反转
out = s.slice(start, end)      ; 切片（下标为码点位置，与 s[i] 一致）
out = s.slice-bytes(b, e)      ; 切片（下标为字节偏移，内部使用）
val = s.replace-char(old, new) ; 取代字元

; 转换为其他型别
b = s.to-bytes()               ; 转 []byte
v = s.to-i64()                 ; 转 ?i64
v = s.to-f64()                 ; 转 ?f64
v = s.to-bool()                ; 转 ?bool

; 分割与连接
parts = s.split(sep)           ; 分割（返回 []str）
out = ss.join(sep)             ; []str 连接

; 复制与填充
dst = s.copy()                 ; 字串复制
s.fill(val byte)               ; 填充 byte 值
```

## 字串与数值转换

```no
; 数值转字串（方法）
s = i64.to-str(); i64 转字串
s = f64.to-str(); f64 转字串
s = bool.to-str(); bool 转 "true"/"false"
s = byte.to-str(); byte 转字串
s = char.to-str(); char 转字串

; 字串转数值（返回 option）
v = s.to-i64(); 回传 ?i64
v = s.to-i32(); 回传 ?i32
v = s.to-u64(); 回传 ?u64
v = s.to-f64(); 回传 ?f64
```

## 下标写入已禁用（`str` 2026-11-11 / `txt` 2026-11-12）

> **`s[i] = v` 是编译错误 `idxwrite`**，`with-cap` / `with-len` / `with-cap-len`
> 三种基底一律不例外。**`txt` 的 `t[i] = c` 同样禁用**（理由见下）。
>
> `str` 的原因：它是「越界写入已配置缓冲区之外」的唯一来源 —— len=0 的缓冲靠直写增长，
> 写到 `i >= cap` 就跨出配置区。codegen 的 `ensureStrLongBuffer` 增长护栏原本就是
> 为此而设；禁用直写后该护栏不再对用户代码可达，只保留作防御性代码。
>
> `txt` 的原因不同但同罪：`t[i] = c` 是按码点**重编码写回**，长度会随写入隐式改变
> （`i` 等于码点数时等价追加、超 255 的部分静默丢弃）。txt 没有堆、没有扩容，
> 但「一次下标写偷偷改了长度」是同一类危险。

四条仍然合法的写法：

```no
; ① 逐位元组「构造」：[]byte 缓冲承接，收尾整体转回 str
b []byte = with-cap-len(n, n)
b[0] = 48
b[i] = src.byte(i): {
    ok -> it

    -> 0
}
s = b.slice(0, k).to-str(); 也可 b.to-str()（整段）

; ② 原位「覆写」既有内容：str.fill 把 len 个位元组全填成同一个值，长度不变
s = 'hello'
s.fill(72); 'HHHHH'

; ③ 下标写只剩 place-match（以「读」作闸门：i >= len 时静默跳过，不增长、不越界）
s[i]: {
    ok -> it = v
    -> _
}

; ④ 要变长：用 push 家族（显式、可审计的一次呼叫）
s.push("中");      按码点追加（UTF-8）
s.push-str('de');  按位元组追加（原地扩容）
```

> ⚠️ **place-match 是「码点」闸门，不是位元组闸门。** 它的闸门来路是 `s[i]` 这个
> **读** —— 而 `str` 的下标读取按码点计。所以 ③ 只适合「一个码点写一格」的
> ASCII 场景（uuid 打连字号、time 打数字、hex 表查表）。要逐**位元组**拼内容
> （含 `>= 0x80` 的位元组、或任何可能非 ASCII 的来源字串）必须用 ① 的 `[]byte`：
> 实测 `f = with-len(8)` 后用 place-match 依次写 224..231，只有前 4 格落盘，
> 其余被闸门静默跳过；`path.split` 的「非 ASCII 档名」正是这样被截断的。

> **长度截断**：裸 `s.len = n` 已不再支援，改用切片 —— `s.slice(0, n)` 是**码点**切片，
> `s.slice-bytes(0, n)` 才是字节切片。（`[]byte` 缓冲上的 `b.slice(0, k)` 自然是字节切片。）
>
> **`with-cap(n)` 建 str 之后靠 `push` 增长**：`s str = with-cap(256)` 是 len=0 但已有
> 容量的缓冲，第一次 `push` 直接写进去、不触发扩容（`push-str` 只在 `len + 追加长度`
> 超出 `cap` 时才 realloc，并以倍增成长）。逐位元组的 `buf[i] = b` 对 str 仍然禁用。

### 增长：`push` 家族（2026-11-12）

直写被禁之后，**变长必须是显式的一次呼叫**：

| 方法 | 语义 |
|---|---|
| `s.push(c char)` | 追加一个码点，UTF-8 编码（内部委托 `push-str`） |
| `s.push-str(t str)` | 追加一个字串，**按位元组**拷贝，原地扩容 |

```no
s = 'ab'
s.push("中");      'ab中'，len-bytes 5、len 3
s.push-str('de');  'ab中de'，len-bytes 7

t = 'zz'
t.push-str(t);     'zzzz'（自我追加安全：先 retain 源、再用 memmove 拷贝）

u = ''
u.push-str('');    ''（空源不改变任何东西，也不分配）
```

> `push-str` 是编译器内建（`#{buildin}`，MIR 展开为「原地扩容 + memmove + 改长度」），
> 不是 std 回圈：一次追加 O(追加长度)，反复追加均摊 O(1)（倍增）。
> `push` 走 UTF-8 编码，所以 `len-bytes` 的增量等于该码点的编码长度，`len` 恒增 1。

> **`txt` 的写入同样已禁用（2026-11-12）**：`t[i] = c` 是**码点级重编码写回**，长度会
> 随写入隐式改变 —— `i` 等于码点数时等价追加、总字节长超过 255 的部分静默丢弃。
> 「一次下标写偷偷改了长度」正是禁令要消灭的东西，txt 与 str 同案处理。txt 的变长
> 入口是 `t.push(c)` / `t.push-str(s)` / `t.push-txt(u)`；就地改值用 place-match。
> 读取 `t[i]` 仍回传码点 `char`，这一点没变。
>
> 字节级原始写入器 `str._set-byte` / `txt._set-byte` 自 2026-11-11 起是 **std 私有**
> （只有 std 内部的 `str.fill` / `txt.push` 等会呼叫），用户代码无法使用。
> 需要按字节拼 `txt` 时，先攒 `[]byte`，再用 `txt.from-bytes(b)` 一次拷入
> （超过 255 位元组自动截断，长度按实际字节数设定）。

## 预分配（内建语法）

透过 `with-cap`、`with-len`、`with-cap-len` 三个内建语法可预先分配堆记忆体，避免后续 `push` 触发反复扩容：

```no
; with-cap(cap)：配置 cap 容量，len=0（需 push 后才能索引）
s = with-cap(256); str，len=0, cap=256

; with-len(len)：配置 len 容量，len=cap（可直接索引读）
s = with-len(128); str，len=128, cap=128

; with-cap-len(cap, len)：同时指定容量与长度，适合「预留成长空间」场景
s = with-cap-len(512, 64); str，len=64, cap=512
```

| 内建语法 | 参数 | len | cap | 适用场景 |
|---|---|---|---|---|
| `with-cap(cap)` | 1 | 0 | cap | 只知道上限，长度由 push 增长 |
| `with-len(len)` | 1 | len | len | 固定长度，可直接索引读 |
| `with-cap-len(cap, len)` | 2 | len | cap | 已知初始长度且需预留扩容空间 |

> 参数与 `s.cap()` / `s.len-bytes()` 的回传值都是 **`i64`**（对外 API 不变），但描述符内部以
> **`u32`** 存放 len / cap。呼叫端看到的仍是 64 位元数值，转换由编译器在描述符边界完成；
> 详见〈内部布局与对外 API〉。指派 `s.len = n` / `s.cap = n` 这类栏位写入同样走这层边界，
> 因此不会覆写相邻的 header 栏位。

> ⚠️ **`str` 的缓冲一律不允许下标直写**（2026-11-11 起，`with-cap` 基底也不再豁免）。
> `idxwrite` 的 with-cap 豁免现在只服务 **`[]byte` / `[]T` / 固定阵列**：它们的 len=0
> 缓冲仍靠 `buf[i] = b` 增长长度，没有等价替代写法。`str` 请改走 `[]byte` 缓冲 +
> `to-str()`（见〈下标写入已禁用〉一节）。
> `with-len(n)` / `with-cap-len(cap, len)` 的 len 已就位，**写入须 place-match
> `buf[i]: { ok -> it = v / -> _ }` 或 `buf[i] ?= v`**——与任何其他容器同罪，直写是
> 编译错误 `idxwrite`。（2026-11-10 起 `with-len` 不再豁免。）

> **型别推导**：三个内建语法的结果型别由赋值左侧推导，可用于 `str` 或 `[]T` 切片。

