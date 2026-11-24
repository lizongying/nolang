---
sidebar_position: 6
---

# 跨模组调用前缀

Nolang 采用强制性的模组命名空间规范：在一个 `.no` 档案中调用**其他模组**（其他 `.no` 档案）定义的函数或常量时，必须使用 `ShortName.` 前缀。这避免了跨模组的命名冲突。

标准库模组由编译器自动载入，**不需显式导入**（无需 `# std/...` 注解），直接使用 `ShortName.` 前缀即可调用。`no fmt` 会自动移除所有未写别名的纯 std 模组导入（如 `# std/noml`、`# std/math`、`# std/fmt`）；写了别名（`# std/noml n`）或函数导入（`# std/math.sqrt`）则保留（详见[编码规范 · 导入语句自动整理](./code-style#导入语句自动整理去重--移除无别名-std-导入--排序)）。

> **依赖与路径解析**：本地包的依赖键可透过 `workspace.noml` 映射为短名称或完整 URL 形式。详见 [依赖类型与版本号规则](../usage#依赖类型与版本号规则)。

## ShortName 定义

ShortName 是模组路径的最后一段，用作跨模组调用时的前缀。

| 档案路径 | FullPath | ShortName | 说明 |
| --- | --- | --- | --- |
| `std/math.no` | `math` | `math` | 顶层档案 |
| `std/fs.no` | `fs` | `fs` | 顶层档案 |
| `std/net/net.no` | `net/net` | `net` | 路径最后一段 |
| `std/net/client.no` | `net/client` | `client` | 路径最后一段 |
| `std/crypto/sha256.no` | `crypto/sha256` | `sha256` | 路径最后一段 |
| `std/archive/gzip.no` | `archive/gzip` | `gzip` | 路径最后一段 |

ShortName 即为 FullPath 以斜线分隔后的最后一段（如 `crypto/sha256` → `sha256`）。

## 需要前缀

调用其他模组定义的模组级函数或常量时，必须使用 `ShortName.` 前缀。

```no
; 模组级函数
sha256.sha256(data)
sha256.sha256-hex(data)
fs.open(path, opts)
gzip.gzip-decompress(data)
math.pow(x, y)

; 模组常量
net.NET-BUF-SIZE
math.PI
```

### 函数命名惯例

**不要在函数名前加上模组名前缀。** 模组内的函数只需用简短直观的名称即可，跨模组调用时的 `ShortName.` 前缀会自动带出模组资讯。

```no
; ✅ 正确：函数名简洁，不加模组名前缀
; tail.no
; 入口函数直接用模组名
tail = () {
    ...
}

; 辅助函数用简短名称
atoi = (s str) (v i64) {
    ...
}
```

```no
#{fmt-skip}
; ❌ 避免：函数名冗余带上模组名前缀
; tail-run = () { }
; tail-atoi = (s str) (v i64) { }
```

跨模组导入时也保持一致：

```no
; ✅ 简洁直观
# /src/tail.tail
# /src/mktemp.mktemp
```

```no
#{fmt-skip}
; ❌ 冗余
; # /src/tail.tail-run
; # /src/mktemp.mktemp-run
```

> **注意避开关键字**：`awy`（协程组关键字）、`match`（条件匹配关键字）等不能用作函数名。`run` 已移除，可作普通识别符。入口函数建议直接使用模组名本身（如 `ping.no` → `ping`）。

## 不需要前缀

以下情况不需要前缀：

### 1. 全局函数（`with-cap` / `with-len` / `with-cap-len` / `print` / `eprint` / `format` / `exit`）

这 7 个函数是语言级全局内置函数，可以直接使用，无需模组前缀。它们的注释宣告统一放在 `std/global.no` 中，方便开发者查看。

**容量/长度构造：**

- `with-cap(cap)` — 建立指定容量的字串或切片（len=0），型别由赋值左侧推断
- `with-len(len)` — 建立指定长度的字串或切片
- `with-cap-len(cap, len)` — 建立指定容量和长度的字串或切片

**输出/格式化：**

Nolang 使用**具名格式字串**语法 `{name[:spec]}`，直接引用作用域中的变量，无需位置参数。支援编译期验证。输出透过 `io.out`/`io.err` 系统调用直接写入，不依赖 libc `printf`。

- `print(s)` / `print(s0, s1, ...)` — 输出到标准输出，多参数以空格分隔，**自动换行**
- `eprint(s)` / `eprint(s0, s1, ...)` — 输出到标准错误，多参数以空格分隔，**自动换行**
- `format(s)` — 返回格式化字串（替代 `sprintf`），不自动换行

**行程控制：**

- `exit(code)` — 以状态码结束行程（原 `os.exit`，已提升为全局内建，无需 `os.` 前缀）

**多参数时，字面量默认是模板，不是 C 风格的格式串。** `print('result={val}', 42, 'result={val}')` 中，每个字串字面量**各自**是一个模板：它的 `{val}` 从**呼叫点作用域**取值，与同一调用里的其它参数无关。字面量**不会**去描述或消费后面的参数——它不是 `printf('%d', 42)` 那样的替换。非字面量的参数（变数、运算式）则按普通可变参数处理，以空格分隔，整行末尾只有一个换行。

```no
val = 42
print('result={val}', 42, 'result={val}'); result=42 42 result=42
tmpl = '{val}'
print(tmpl); {val} —— 变数是纯文字，不做替换
print('a={val}', val); a=42 42
```

`format(s)` 以及已废弃的 `sprintf(s)` 仍只接受**单一格式字串**，没有多参数形式。

> `printf`、`eprintf` 已**移除**：名字仍留在符号表中（呼叫仍能解析、编辑器仍能补全），
> 但**任何呼叫都是硬性编译错误**（`[printf-depr]`），讯息会直接引导迁移。`sprintf` 仍可用但已**废弃**。
> 替代关系如下：
> - `printf(s)` → `print(s)`（自动换行）或 `io.out(s)`（无换行，标准输出）
> - `eprintf(s)` → `eprint(s)`（自动换行）或 `io.err(s)`（无换行，标准错误）
> - `sprintf(s)` → `format(s)`（返回格式化字串）
>
> `io.out`/`io.err` 是底层命令，输出**不换行**。由于模组调用必须加模组名，`io.err` 明确了模组前缀，不会与 Option 构造函数 `err()` 冲突；即使同名，模组前缀也能区分。

```no
; 容量/长度构造（无前缀）
s str = with-cap(256); ✅ 预分配 256 位元组的 str
v []i64 = with-cap(100); ✅ 预分配 100 个元素的切片
v []i64 = with-cap-len(200, 100); ✅ 容量 200、长度 100
v []i64 = with-len(100); ✅ 长度 100 的切片

; 输出/格式化（无前缀）
print('hello {n}'); ✅ 自动换行
print(a, b, c); ✅ 多参数，空格分隔，自动换行
print('x={n}', n); ✅ 字面量是模板，变数是值
print('result={val}', 42, 'result={val}'); ✅ 每个字面量各自替换
print(); ✅ 空参数，只输出换行
s = format('x={x}'); ✅ 返回格式化字串（单一格式字串）
eprint('err: {n}'); ✅ 输出到 stderr 并换行
eprint('err:', a, b); ✅ 多参数，stderr 空格分隔
print('编号 {id:06} 金额 {money:.2f}'); ✅ 支援对齐、填充、宽度、精度

; 底层命令（需要模组前缀）
io.out('no-newline-here'); ✅ 输出不换行（替代 printf）
io.err('err-no-newline'); ✅ stderr 不换行（替代 eprintf）

; 其他所有跨模组调用都需要前缀
fs.open(path, opts); ✅ 带前缀（builtin 也需要）
```

#### 格式说明符

`{name[:spec]}` 中 `spec` 支援以下栏位（顺序固定，均可省略）：

```
[[fill]align][sign][#][0][width][.precision][type]
```

- `fill` — 填充字元（预设空格），需与 `align` 一起使用
- `align` — `<` 左对齐、`>` 右对齐、`^` 置中
- `sign` — `+` 正数显示加号、`-` 仅负数显示（预设）
- `#` — 进制前缀（`0x`/`0o`/`0b`）
- `0` — 数值前补零至指定宽度
- `width` — 最小栏位宽度
- `.precision` — 浮点数小数位数 / 字串截断长度
- `type` — `d`(整数)、`x`/`X`(十六进制)、`o`(八进制)、`b`(二进制)、`c`(字元)、`f`(定点)、`e`/`E`(科学记号)、`g`/`G`(通用)、`s`(字串，预设)、`t`(资料型别名，编译期确定)、`v`(字面量值，字串加单引号，vec 调用 to-str)

```no
x i64 = 42
u u64 = 255
pi f64 = 3.14159
s str = 'hello'
print('{x:06}'); 000042
print('{x:>10}'); 右对齐宽度 10
print('{u:#x}'); 0xff
print('{pi:.2f}'); 3.14
print('{pi:8.3e}'); 3.142e+00
print('{s:<10}'); hello     （左对齐）
print('{s:.3}'); hel（截断为 3 字元）
print('{x:t}'); i64（印出变数 x 的型别名）
print('{s:v}'); 'hello'（字串用单引号包裹）
```

`{{` 与 `}}` 用于输出字面 `{` 与 `}`，但**仅当同一字串中至少还有一个真正的 `{name}` 栏位时才会生效**。只含转义、不含栏位的字面量会被原样输出：`print('{{n}}')` 输出 `{{n}}`（而不是 `{n}`），`print('{{')` 输出 `{{`。要输出单独的大括号，直接用 `print('{')` / `print('}')` 即可。

### 2. 同档案定义

在同一 `.no` 档案内定义的函数、常量、方法，直接使用，不需前缀。

```no
; 在 sha256.no 中：
sha256(data); sha256 定义在本档案

; 常量定义在本档案
HMAC-BLOCK-SIZE
```

### 3. 内置类型方法

内置类型（`str`、`i64`、`vec`、`arr`、`byte`、`char`、`bool` 等）的方法调用不需前缀。方法已内建于型别，通过接收者型别直接解析。

```no
'hello'.starts-with('he'); str 方法
n.to-str(); int 方法
v.push(42); vec 方法
a.contains(3); arr 方法
c.is-digit(); char 方法
```

### 4. 结构体实例方法

对已建立的结构体实例调用方法不需模组前缀。方法通过实例的型别解析，编译器自动找到对应的 `struct.method` 定义。

```no
f = fs.open(path, opts); fs.open 是模组级函数，需前缀
f.read(buf, n); file.read 是结构体方法，不需前缀
f.close(); file.close 是结构体方法，不需前缀

p = path {
    p: '/tmp'
}
p.exists(); path.exists 是结构体方法，不需前缀
```

## 方法调用 vs 模组函数调用

方法调用是否需要前缀取决于**方法所有者**：

- **内置类型的方法**（`str.starts-with`、`i64.to-str` 等）—— 不需前缀
- **结构体实例的方法**（`f.read`、`p.exists` 等）—— 不需前缀
- **模组级函数**（`fs.open`、`sha256.sha256` 等）—— **需要前缀**

`fs.fil()` 中 `fs` 是模组的 ShortName，`fil` 是模组级函数名。`fs.` 前缀不可省略，因为 `fs` 在此处不是变数名，而是模组路径。

### `Name.Function` 的两种语义

`process.cmd(...)` 和 `p.start(...)` 写法都是 `xxx.yyy()`，但语义完全不同：

| 写法 | `xxx` | `yyy` | 语义 |
| --- | --- | --- | --- |
| `process.cmd(...)` | 模组 ShortName | 模组级函数 | `xxx` 是模组路径，`yyy` 是该模组定义的独立函数 |
| `p.start(...)` | 实例变数 | 结构体方法 | `xxx` 是 `process` 类型的变数，`yyy` 是定义为 `process.start = ...` 的方法 |

**定义时的差异**：
- 模组级函数定义时**不加前缀**：在模组内部直接写 `cmd = (program str, ...) { }`
- 结构体方法定义时**必须加 `struct.` 前缀：`process.start = (program str, ...) { }`

**调用时的差异**：
- 模组级函数在外部调用时用 `ModuleName.function()`：`process.cmd(...)`
- 结构体方法通过实例调用：`p = process.new()` → `p.start(...)`

> **注意**：即使在模组内部，调用同模组的模组级函数也不加前缀（`cmd(...)`），而调用结构体方法则通过 `self` 隐含或 `.method()` 语法。

## 跨模组型别引用

引用**其他模组**定义的型别（结构体、介面、列举等）时，必须使用 `ShortName.` 前缀。这适用于：

### 结构体实作介面

当一个结构体实作其他模组定义的介面时，介面名必须带模组前缀：

```no
#{fmt-skip}
; ❌ 错误：db、rows、stmt 是 sql 模组定义的介面，不能省略前缀
db-mysql db {
    fd i64
}
```

```no
; ✅ 正确：使用 sql.db、sql.rows、sql.stmt
db-mysql sql.db {
    fd i64
}

rows-mysql sql.rows {
    fd i64
}

stmt-mysql sql.stmt {
    fd i64
}
```

### 函数参数与返回型别

函数签名中的跨模组型别同样需要前缀：

```no
; ✅ 正确：返回型别使用 sql.result
db-mysql.exec = (sql str) (r sql.result) {
    ...
}
```

### 结构体栏位型别

```no
; ✅ 正确：栏位型别使用 sql.connection
conn-mysql sql.db {
    handle sql.connection
}
```

### 不需前缀的情况

- **同模组定义的型别**：在同一 `.no` 档案中定义的结构体、介面、列举，直接使用型别名
- **内置型别**：`str`、`i64`、`bool`、`byte` 等内置型别不需前缀
- **内置介面**：`enter`、`leave` 等语言内置介面不需前缀

```no
; 同档案定义的型别，不需前缀
result {
    last-id i64
    affected i64
}

; enter/leave 是内置介面，不需前缀
; result 是同档案定义的结构体，不需前缀
db {
    enter
    leave
    close() (ok bool)
    exec(sql str) (r result)
}
```

## 完整范例

```no
; 标准库模组自动载入，无需显式导入

; ─── 不需前缀 ───

; 全局函数（宣告在 global.no）
s str = with-cap(256)
v []i64 = with-len(100)
print('hello {n}')
s = format('x={x}')

; 同档案函数
sha256(data)

; 内置类型方法
'hello'.starts-with('he')
n.to-str()
v.push(42)

; 结构体实例方法
f = fs.open(path, opts)
f.read(buf, n)
f.close()

; ─── 需要前缀 ───

; 模组级函数
sha256.sha256(data)
sha256.sha256-hex(data)
fs.open(path, opts)
gzip.gzip-decompress(data)
math.pow(x, y)

; 模组常量
net.NET-BUF-SIZE
math.PI

; 跨模组型别引用（介面实作、参数型别、返回型别、栏位型别）
db-mysql sql.db {
    fd i64
}

r sql.result = d.exec('CREATE TABLE ...')
```
