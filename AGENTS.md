# Agent 指南 — Nolang 编译器项目

本文件面向 AI 编码代理，帮助其在阅读后能准确理解 Nolang 语言特性与本项目约定，
稳定生成风格一致、可编译、符合规范的 Nolang（`.no`）代码，以及维护其 Go 编译器。

> 范围：生成或修改 `src/std/**/*.no`（标准库）、`tests/**/*.no`（测试）、
> `example/**/*.no`（示例）以及编译器本身的 Go 代码时遵循本文件。
> 不含任何贡献流程 / 评审 / LLM 使用政策章节。
> 完整语法细节见 `.agents/skills/nolang-syntax/SKILL.md`（本文件是其面向代理的精简版）。

---

## 1. 仓库概览

Nolang 是一门**内存安全、无 GC、语法极简**的系统编程语言。编译器以 Go 实现，流水线为
`lexer → parser → checker → hir → mir → transpiler(生成 LLVM IR) → opt → llc → clang`。
资源管理依靠 **deferred move + 作用域退出自动销毁**，禁止手动 `free`。

### 1.1 目录结构与模块职责

| 路径 | 职责 |
| --- | --- |
| `src/lexer/` | 词法分析。`token.go` 定义 `TokenType` 与关键字；`lexer.go` 一次性预扫描全部 token。 |
| `src/parser/` | 递归下降解析（单文件 `parser.go` 约万行）。产出 AST（含 Surface 语法节点）。 |
| `src/checker/` | 类型检查、签名收集。`stdsig_gen.go` 是**内嵌二进位的 std 签名表**，由 `make gen` 从 `src/std` 生成。 |
| `src/hir/` `src/mir/` | 高层 / 中层 IR。MIR 是 codegen 主战场，内存安全审计聚焦此处。 |
| `src/build/` `src/build/llvm/` | transpiler 与 LLVM 后端 codegen（生成 IR → opt → llc → clang）。 |
| `src/fmt/` | `no fmt` 源码格式化器。4 空格缩进，是代码风格的唯一权威。 |
| `src/lsp/` `src/cmd/` | 语言服务器与 `no` CLI（`src/cmd/no`）。 |
| `src/std/` | **标准库**（`.no` 源）。被 `//go:embed` 烘焙进二进制，无磁盘回退。 |
| `src/builtin/` | 编译器内建（print、数值转换、str/vec 内建方法等）。 |
| `tests/` | 端到端测试。每个 `.no` 独立构建并运行，失败返回非零退出码。 |
| `example/` | 示例程序（如 `example/test1`）。 |
| `workspace.noml` | 工作区根定义，登记各包路径（如 `test: '/test'`）。 |
| `Makefile` | 构建入口。**改编译器或 std 一律走 `make`，勿 `cd src && go build`。** |

### 1.2 入口与包规则

- `main.no` — 程序入口。
- `lib.no` — 库入口，导出函数。
- `tests/` 目录下所有 `.no` 文件 — 含测试断言，每个文件独立构建。
- 每个包用 `package.noml` 描述，工作区用 `workspace.noml` 登记。
- 导入路径相对**包自身的 `package.noml` 目录**解析（非工作区根）；跨包解析链由 `workspace.noml` 递归映射。

### 1.3 构建 / 测试 / 格式化工作流

```bash
make            # 构建所有目标（bin/no、LSP server 等）
make no         # 构建 bin/no（会先 make gen 重生成 std 签名表）
make test       # 跑测试（注入 StdSigKey，走内嵌签名表快路）
make gen        # 仅重生成 src/checker/stdsig_gen.go

no fmt [-w] [-d] <file|dir>   # 格式化；-w 写回，-d 仅 diff
no build [-o out] <file|dir>  # 构建可执行文件
no run <pkg|dir|file>         # 构建并运行
no test [file|dir]            # 运行测试
no vet src/std               # 对标准库做 vet 检查
```

⚠️ 修改 `src/std/*.no` 后**必须** `make no`（重生成签名表）；直接 `go build` 会因签名表与源码
不一致而被编译器**拒绝构建**。绝不要对 `src/std` 跑 `no fmt -w`（会丢失 `#{index-out}` 等
`no build` 必需的注解）。

---

## 2. 黄金法则

**绝不修改合法、语法正确的 Nolang 代码**——包括标识符、变量声明、任何语言构造——即使你怀疑是
解析器 / 编译器 bug。遇到疑似解析或工具错误，提交 bug 报告或告知用户；不要改代码。

---

## 3. 语言参考（从 `.agents/skills/nolang-syntax` 提炼）

### 3.1 注释与文件组织

- 行注释用 `;`（**首选**）；`;;` 后紧跟换行进入块注释，`;;` 再换行结束。
- `;` / `;;` 是**注释标记**，绝**不是**语句分隔符；`,` 也不能连接语句。**每条语句独占一行**。
- 每行一条语句——单行多语句写法被禁止（`cond -> { print('X'); return }` 中 `; return }` 全成注释，
  块永不闭合，后续整段被吞掉）。
- 文档注释写在条目正上方，用 `;` 开头，附完整参数 / 返回名与类型（见 §4.3）。
- 每个非空白 `.no` 文件**必须以恰好一个末尾换行结尾**（`no fmt` 自动保证）。

```no
; ✅ 正例：每条语句独占一行
cond -> {
    print('X')
    return
}

; ❌ 反例：用 ; 当语句分隔符 —— 块永不闭合，return 被吞
; cond -> { print('X'); return }
```

### 3.2 关键字

Nolang 关键字极少、语法极简。**注意：`go` 不是关键字**——无色 async 衍生用 `co`；
旧式 `run` / `awy` 手写任务原语已废弃，改用协程组 / `co`（见 §3.11）。

**控制流构造**：

| 用途 | 写法 |
| --- | --- |
| 无限循环 | `!! { }` 或 `true { }` |
| 永不执行 | `! { }` 或 `false { }` 或 `() { }` |
| 条件循环 | `(cond) { }` |
| 计数循环 | `n * { }`（N ≤ 0 跳过） |
| 区间迭代 | `i <- [a..b]: { }`（四种括号组合，start > end 自动递减） |
| 条件匹配 | `x: { ... }` |
| 分支选择 | `{ cond -> body }`（短路链） |
| 跳出 / 继续 / 返回 | `*`（跳出）`**`（继续）`...`（返回） |

**其他关键字**：`return` `break`（暂保留）`continue`（暂保留）`true` `false` `nil`
`as`（**仅用于 FFI 指针类型转换**，如 `*byte` / `**byte` / `*i64`）`ptr` `chan` `map`
`co`（无色 async 衍生）`self` `super` `it`。

**`it`**：match 臂中的**隐式绑定**。`ok -> process(it)` 中 `it` 是解包值；`err -> log(it)` 中
`it` 始终是**错误消息字符串**（`str`），与元素类型无关。`nil`/`err` 未被同时显式认领时，
catch-all `->` 用 `it` 会编译错误（含义不唯一）。`it` 仅在 match 臂内可用，跨层级携带请复制到具名局部。

> **最佳实践（option 解包首选 `it`）**：解包 **option `?t`** 时优先写 `ok -> ...(it)...`，而非
> `ok(v) -> ...(v)...`——不引入额外绑定名，也就规避「臂间同名局部变量串型」的坑（见 §6.3）。
> 仅两种情形必须保留**具名绑定** `ok(v) ->`：(1) 被匹配值是**用户自定义枚举变体**（如
> `res { ok(v str) }`、`shape { rect(w, h) }`）——此时 `it` 是**整个枚举**而非载荷，取载荷只能具名；
> (2) 需在**嵌套 match 之后**继续用该值——内层 match 会重绑 `it`，请用具名绑定或先复制到具名局部。

**`self` / `super`**：接口方法体内，`self` 指接收者；`super.method()` 调用父接口实现（见 §3.8）。

```no
; 区间迭代 + 条件循环 + match
i <- [0..5): {
    print(i)
}
(n < 5) {
    n = n + 1
}
val: {
    nil -> print('empty')
    err -> log(it)          ; it 是错误消息 str
    ok -> process(it)       ; it 是解包值
}

; if/elif/else 写成 {} 包裹的短路链
{
    cmd == 'a' -> { fa() }
    cmd == 'b' -> { fb() }
    -> {}
}
```

### 3.3 类型系统

**基础类型**：`byte` `bool`（仅小写）`char`（双引号单码点，如 `"中"`）`str`（UTF-8 字符串，
单引号 `'hello'` 或反引号原始串）`txt`（定长 256 字节串，须显式标注 `t txt = 'abc'`）
`i8 i16 i32 i64`（默认整数，架构无关）`i128` `u8 u16 u32 u64 u128` `usize`（仅 FFI）
`f32 f64`。

**容器**：`obj` `map[k]v` `arr`（定长 `[n]t`、`[?]t` 自动推断长度）`vec`（变长 `[]t`）
`slice`（视图，无独立类型，须附着在 arr/vec/str 上）。

**特殊 / 高级**：`*t`（指针，仅 FFI `#{c}` 声明与标准库）`any`（仅标准库）`bigint` `err`。

**Option（可失败 / 可空）**：前缀 `?` —— `?i64` `?str` `?[]str`。`?t` 是三态标签枚举
`option { ok(v t), nil, err(e str) }`：`ok`（有值，隐式绑定）`nil`（空）`err(msg)`（错误）。
解包优先用 `?t` 而非 `(val, ok bool)` 双返回。

**联合类型别名**：`name = t1 | t2 | ...`，编译期按实参单态化（每个成员生成一份具体函数）。
`int` / `float` / `num` / `string` 都是 std 用此语法定义的普通代码，**不是关键字**。

```no
int    = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128
string = str | txt
bytes  = []byte          ; 单类型别名（newtype 语义：别名与底层类型互不相通）
```

> ⚠️ **union 别名方法遮蔽**：不要定义与成员同名的方法（如 `string.len`）——它会接管
> `a.len()` 调用并在自身体内递归分发 → 段错误。成员方法须直接调用 `str.*` / `txt.*`。

### 3.4 变量与字面量

- 默认类型推断：`i = 1`（i64）、`f = 1.0`（f64）、`b = 0x00`（byte）、`name = 'nolang'`、
  `flag = true`、字符 `"中"`、原始串反引号、正则 `/pattern/flags`（JS 风格，上下文敏感）。
- **十六进制字面量推断为 `byte`（u8）**：`PORT = 0x0303` 会被截断；超 255 须显式 `PORT i64 = 0x0303`。
- **整数赋值类型检查**：窄→宽隐式允许；宽→窄变量直接赋值**编译错误**（提示用位运算掩码 / 移位安全收窄）；
  右值为位运算（`& | ^ << >>`）且目标为**无符号**类型时允许隐式收窄。
- 显式标注：`a u64 = 10`。`i8 = 3`（变量名等于类型时可省标注）。
- 数组 `arr [3] = [1, 2, 3]`；向量 `v []u8 = [1, 2, 3]`；切片 `v[1..3]` 是**视图**，共享原缓冲。

### 3.5 函数与方法（只读输入 / 可写输出模型）

Nolang 函数是 **read-only input / writable output** 模型：`name = (inputs) (outputs) {}`。

- **输入只读**：标量按值、复合类型按只读引用传入；函数内禁止写输入参数或其子字段。
- **输出可写**：命名输出参数是可写的；调用方绑定已有变量则直接改其内存，留空则生成新值。
- **默认无返回值**：所有数据交互只经输出参数（系统函数有语法糖返回，底层仍是输出参数）。
- **参数默认值**：`name type = expr`，必须放在最后；变参 `..t`（如 `..i64`）。
- **参数 / 结果上限 64 个**（u64 位图限制 move 追踪）。
- **方法**：`type.method = (inputs) (rest-outputs...) {}` 糖化为 `(inputs) (self type, ...)`，
  体内用 `.` 指代 `self`，写 `self` 直接改原实例。

```no
add = (a i64, b i64) (result i64) {
    result = a + b
}
parse-line = (s str, max-fields i64 = 1024) (fields []str) { ... }
user.greet = () {
    print('Hello, ' - .name)
}
```

**Option 错误传播 vs 就地捕获**：

```no
; ?= 向上抛：函数须有 ?T 结果参数，失败时设结果并提前 return
process-file = (path str) (result ?str) {
    result = nil
    f ?= open(path)            ; 失败 → result = f; return
    line ?= f.read-line()      ; EOF/错误 → 自动传播
    result = line
}
; 就地捕获：用 = ；函数无需 ?T 结果参数，nil/err 存入该变量、流程继续
handle = (b i64, c i64, d i64) (r i64) {
    a = b + c / d              ; a 推断 ?i64；除零 → a = err
    a: { err -> { r = -1 }  nil -> { r = -1 }  -> { r = it } }
}
```

### 3.6 控制流

- **match**：`x: {` 换行 `nil -> ...` 换行 `err(e) -> ...` 换行 `ok(v) -> ...` 换行 `-> ...` 换行 `}`（臂**只能换行**分隔；同一行写 `;` 会把后面的臂变成注释——`x: { ok(v) -> ...; -> ... }` 实测丢掉兜底臂，报 non-exhaustive match）。表达式位置构造枚举变体
  `ok(42)`、`rect(3.0,4.0)`。多语句臂体**必须用花括号** `-> { ... }`；单语句可跟在 `->` 后。
  臂间以**换行**分隔，不以 `->` 分隔（`pat -> A -> B` 在同一行是「一条带管线体的臂」，不是两条臂）。
- **if/elif/else**：写成 `{}` 包裹的短路链，条件臂各占一行（`{` / `c1 -> b1` / `c2 -> b2` / `-> b` / `}`）；独立单行 `cond -> body`
  是独立 if，不与相邻 if 短路。
- **循环**：`i <- [a..b]: { }` 支持四种括号组合（开闭区间），`start > end` 时自动递减；其他循环构造
  见 §3.2 关键字表。

### 3.7 运算符与惯用法

- **字符串拼接用 `-`**（惯用）；`+` 在字符串上也拼接、在数值上是算术。`s[i]` 对 `str` 按**码点**索引。
- 算术 `+ - * / %`、位运算 `& | ^ &^ << >>`、比较 `== != < <= > >=`、逻辑 `&& || !`。
- `**` 是 `continue` 简写（仅语句起始），`!!` 等于 `true`（表达式）。
- `?=` option 解包 / 传播（见 §3.5）；`x ?T = expr` 显式标注 option 路径（含运行时溢出检查）。

### 3.8 接口

```no
json {                       ; 接口定义（方法签名，或合并父接口名）
    to-json()
}
user json {                  ; 类型实现接口（同模块无需前缀；跨模块须 module.prefix）
    name str
    age  i64
}
user.to-json = () (s str) {
    s = 'json:' - .name
    ..to-json()              ; 调用父接口实现（super）
}
```

接口可实现多个（`user json, noml { ... }`）；可有默认实现；`enter` / `leave` 接口在作用域进入 / 退出时
自动调用。

### 3.9 注解 `#{...}`

注解是编译指令。**合法位置只有两种**：目标上方独占一行，或目标同行的尾部（trailing）。
**同行的行首前缀写法（`#{...} target`）是编译错误**（对所有注解目标一致）。

```no
#{overflow=wrap}             ; 整数溢出策略（wrap / trap / 默认）；只作用于下一陈述，须独占一行
digits = digits + 1

#{index-out=zero}            ; 安全索引：越界返回元素零值（或字面量）
res = arr[i]  #{index-out=zero}

#{buildin}                  ; 内建方法 / 内建枚举（无 std 函数体，codegen 拦截展开）
str.byte = (i i64) (c ?byte) { }

#{inline}                   ; 结构体字段按值内联（默认）；#{inline=false} 强制指针（递归类型必需）
holder { inl pt #{inline=true}  out pt #{inline=false} }
```

> ⚠️ `#{overflow=...}` 只作用下一陈述，须独占一行置上方；函数级注解已不「整函生效」。
> `no fmt` 单档解析会丢 `#{index-out}`，故**绝不对 `src/std` 跑 `no fmt -w`**。

### 3.10 导入

```no
# std/math.add              ; std 模块
# github.com/utils/math.add ; 远程模块
# /utils/math.add          ; 本地模块（以 / 开头）
# std/math.add a           ; 别名
```

- 标准库模块由编译器自动加载，**无需显式导入**——跨模块调用直接用 `ShortName.` 前缀
  （如 `math.PI`、`fs.open`、`sha256.sha256`）。**不要重新添加裸 `# std/fmt`**（`no fmt` 会自动清理）。
- `ShortName` 是模块路径最后一段：`std/crypto/sha256.no` → `sha256`。
- 函数名**不要带模块名前缀**（`tail.no` 里定义 `tail` / `atoi`，而非 `tail-run` / `tail-atoi`）。
- 旧式 `use` 导入语法已废弃，新代码一律用 `#`。

### 3.11 内存安全模型（影响所有写法）

- **统一引用传递**：所有函数参数默认引用；函数内修改直接影响调用方原数据（可改不可销毁）。
- **禁止手动释放**：变量在函数 / 作用域退出时自动销毁。不要写 `free`。
- **值复制容器**：数组 / 切片存副本，与原变量分离；原变量销毁不影响容器内数据。
- **函数作用域内同名重复赋值 = 覆盖重赋值**（非新建栈变量），且类型必须相同（否则语法不允许）。
- **返回值延迟零值**：函数 prologue 不零初始化输出参数；未显式赋值的输出参数在 return 时自动零填充
  （整数→0、option→nil、struct→zeroinitializer）。无需写 `found = false` / `result = nil` 样板。

---

## 4. 标准库约定与常用 API

### 4.1 模块布局与命名

- 每个 std 模块是 `src/std/<name>.no`，文件内用 `;` 注释开头说明职责与「用法」。
- 模块内方法以 `类型.方法 = ...` 挂载；跨模块共享底层工具放 `src/builtin/*.go`。
- `//go:embed std` 把整个 `src/std` 烘焙进二进制；`no vet src/std` 审的是烘焙版，
  故改 `src/std` 前必须先重编 `bin/no`。

### 4.2 常用模块 API 速查（以 `src/std/*.no` 实际签名为准）

| 模块 | 常用 API |
| --- | --- |
| `str` | `.len()`(码点数) `.len-bytes()` `.cap` `.byte(i) ?byte` `.set-byte(i,b) ?bool` `.to-upper()` `.to-lower()` `.index()` `.contains()` `.slice()` `.copy()` `.fill()`；`with-cap(n)`；`s[i]` 码点索引 |
| `vec` / `arr` | `vec-create(n, val)`；`[]t.eq(b)`；原地排序 `.asc()` `.desc()`；切片视图 `v[a..b)` |
| `int` / `float` / `num` | `int.to-str()` `div` `mod`；`f64.sin() cos() tan() degrees() radians()`；`math.PI` `math.E`；具体类型转换见 `number` |
| `err` | `err.new(msg, code=code.unknown)` `from-errno(errno)`；`code` 枚举（ok/not-found/permission/io/timeout/parse/invalid/overflow/unknown）；`e.is(ec)` `e.msg()` `e.code()` `e.format()` |
| `fmt` | `print(...)` `format('{x}')`（仅认简单标识符，不解析 `.field`） |
| `json` / `yaml` / `toml` | `t.to-json()` `t.to-noml()` 等序列化方法（接口方法，由实现类型提供） |
| `fs` / `io` / `os` | `file` 结构体、`fs.list-dir`（**含 `.`/`..`，递归删除须用 `fs.dir-entries` 或显式跳过**）、`os.strerror(errno)` |
| `net/*` | http / http2 / http3 / ws / quic / tls / sse / server / dns / url 等 |

> 不确定时读对应模块文件，勿臆造 API。优先复用标准库（如 `str.to-bytes()`、`[n]t.to-vec()`、
> `[]byte.to-hex()`、`str.to-i64()`、`int.to-str()`），不要手写等价实现。

### 4.3 代码组织方式

- **方法挂载式**：类型相关行为写成 `类型.方法`，而非自由函数传类型参数。
- **buildin 方法**：性能敏感或需 codegen 拦截的方法（如 `str.byte`）用 `#{buildin}` 标注，函数体为空。
- **union 别名**：联合类型抽成 `alias = ...`，方法写在成员类型上，调用点编译期单态化。

---

## 5. 编码约定

### 5.1 命名（强制规则，非风格）

- **全局常量 / 变量必须大写开头**（如 `NO-LANG` `MAX-SIZE` `HEX-CHARS`）；私有全局用下划线前缀 + 大写
  （`_NOLANG` `_PRIVATE-CONST`）。小写顶层变量会被编译器当局部 → 未定义引用错误。
- **局部变量 / 函数参数小写**（如 `hex-chars` `data-len`）；局部变量**不要**用 `_` 前缀（`_` 仅保留给
  私有全局与 FFI 私有声明）。
- **函数名 / 结构体名小写**（如 `sha1-block` `db-mysql`）。
- 变量名可下划线开头，后接连字符 / 字母 / 数字；不能数字开头、不能连字符结尾、不能有连续连字符。
- **文件名（含目录名）用连字符 `-`**，不用下划线 `_`（如 `string-helper.no`）。

```no
; ✅ 正例
NO-LANG = 'nolang'           ; 全局常量，大写
MAX-SIZE = 1024
_PRIVATE-CONST = 42          ; 私有全局：_ + 大写
add = (a i64, b i64) (r i64) ; 函数名小写
user {                      ; 结构体名小写
    name str
    age i64
}
; 局部变量（函数内）小写、无 _ 前缀
;   x = 10 ; foo-bar = 42

; ❌ 反例
x = 10                       ; 全局不能小写 → 错误
foo-bar = 42                 ; 全局不能小写 → 错误
_X = 10                      ; 局部变量不要 _ 前缀
StringHelper.no              ; 文件名用 _ 而非 -
```

### 5.2 文档注释

函数文档注释须含完整参数名与类型、返回名与类型；模块顶部 API 摘要同样用完整签名。

```no
; ❌ 缺类型、缺返回名
; sha1(data) (hash)
; ✅ 完整签名
; sha1(data []byte) (hash [20]byte) — full hash
; data []byte: input byte array
; returns hash [20]byte: 20-byte hash value
sha1 = (data []byte) (hash [20]byte) { ... }
```

### 5.3 格式化

- **4 空格缩进，禁用 Tab**（格式化器 `src/fmt/api.go` 用 `strings.Repeat("    ", indent)`）。
- 以 `no fmt` 的输出为唯一权威；提交前对改动文件跑 `no fmt -w`。
- 语句以**换行**分隔（不是 `;`；`;` 是注释标记）。`{` 与构造同行，`}` 独占一行。
- 注解 `#{...}` 独占一行置于目标语句上方，或 trailing 于目标同行。
- `no fmt` 自动：去重导入、删除裸 `# std/fmt`、按 path→函数→别名排序导入；保留带注释 / 注解的导入块原序。

### 5.4 惯用写法

- 字符串拼接用 `-`；循环用区间 `<-` 形态；枚举 / option 解包用 `match`（`x: { ... }`）。
- 方法链式调用：`e.format()`、`s.to-upper()`、`v.asc()`。
- 需要预构建字符串缓冲时，先 `with-cap(n)` 再逐字节 `set-byte` / 下标写入。
- 条件分支优先用行内 guard `cond -> stmt`，复杂分支用 `{}` 短路链。
- 可失败的容器操作 / 查找 / 解析优先返回 `?t`（空用 `nil`、错误用 `err('msg')`），而非 `(val, ok)`。
- option `?t` 解包优先用隐式绑定 `ok -> ...(it)...`，而非具名 `ok(v) -> ...(v)...`（少一个绑定名，规避 §6.3 串型坑）；用户自定义枚举变体的载荷仍须具名 `ok(v) ->`（见 §3.2 最佳实践）。

---

## 6. 常见反模式与禁忌

每条配正例 / 反例，便于直接对照。

### 6.1 协程衍生用 `co` 或协程组
`go` 不是关键字；无色 async 衍生用 `co`（自动单态 `worker-async` 并 spawn）。手写的 `run` / `awy`
任务原语已废弃，改用协程组（语句位置的裸块 `{}`）。不要用 `go` 或手写的 `run` / `awy`。

```no
; ✅ 正例：co 衍生，或协程组 {}
{
    r1 = co worker(1)
    r2 = co worker(2)
}
```

### 6.2 禁止手动释放内存
Nolang 用 deferred move + 作用域退出自动销毁。写 `free` / 手动 `delete` 既不需要也危险。

```no
; ✅ 正例：离开作用域自动销毁
buf = with-cap(64)
keep(buf)
; ❌ 反例：手写 free
; free(buf)
```

### 6.3 match 臂间禁止同名局部变量（尤其不同类型）
不同臂里声明同名变量（尤其不同类型）会导致 codegen 串型、类型推断被污染，且 **checker 完全不报错、
编译通过但运行错乱**。每个臂的绑定变量一律加唯一后缀。

```no
; 枚举声明：state { a(v i64), b(s str) }；类型写在变体声明里，
; 臂上的绑定只写名字（写 `a(tn i64)` 是语法错误：expected comma or right parenthesis）
; ❌ 反例：两臂都用 tn（一为 i64、一为 str）→ 静默串型
st: {
    a(tn) -> keep(tn)
    b(tn) -> keep-str(tn)
    -> r = 0
}
; ✅ 正例：加唯一后缀
st: {
    a(tni) -> keep(tni)
    b(tns) -> keep-str(tns)
    -> r = 0
}
```

上面是**多**变体枚举——各臂都要具名取载荷（`it` 在枚举臂里是整个枚举，见 §3.2）。
但典型 **option `?t`** 只有一个解包臂时，更优写法是直接用隐式 `it`，根本不引入绑定名：

```no
; ✅ option 解包首选 it（无需具名，也就没有同名串型问题）
res ?i64 = compute()
res: {
    ok -> keep(it)
    nil -> r = 0
    err -> log(it)
}
```

### 6.4 `#{overflow=...}` 必须独占一行且只作用于下一陈述
放在函数体里不会「整函生效」；必须独立成行置于目标语句上方。同行的行首前缀写法（`#{...} target`）是编译错误。

```no
; ✅ 正例
#{overflow=wrap}
digits = digits + 1
; ❌ 反例（误以为整函数生效 / 与语句同行 / 行首前缀）
; #{overflow=wrap} digits = digits + 1
; #{overflow=wrap} digits = digits + 1   ; 前缀写法 → 编译错误
```

### 6.5 不要定义会遮蔽成员方法的 union 别名方法
为 `string = str | txt` 定义 `string.len` 之类的别名方法，会接管 `s.len()` 调用并递归分发回自身 → 段错误。
成员方法直接用 `str.*` / `txt.*` 原版。

### 6.6 绝不对 `src/std` 跑 `no fmt -w`
`no fmt` 单档解析会丢 `#{index-out}` 等 `no build` 必需的注解，造成死循环。改 std 后只 `make no` 重编。

### 6.7 `?=` 只能在拥有 option 结果参数的函数内使用
`?=` 是 option 解包 + 向上传播，函数无 `?T` 结果参数时使用是编译错误。原地捕获用 `=`（函数无需 `?T`）。

```no
; ✅ 正例
process = (path str) (result ?str) {
    result = nil
    f ?= open(path)            ; 失败 → result = f; return
}
; ❌ 反例：在无 ?T 结果参数的函数里用 ?=
; g = (x i64) { y ?= might-fail(x) }
```

### 6.8 构建 / 测试务必走 `make`，勿 `cd src && go build`
`make no` 会先 `make gen` 重生成内嵌 std 签名表；直接 `go build` 会因签名表未更新被拒或静默退化。
跑测试用 `make test`（注入 `StdSigKey`）。

### 6.9 修改 `src/std` 后必须重生成签名表
改了 `src/std/*.no` 的签名（增删函数 / 方法 / 字段 / 枚举变体），必须 `make no` 重生成
`src/checker/stdsig_gen.go`，否则编译器检测到不一致会拒绝构建。

### 6.10 全局变量必须大写；模块内避免全局可变状态
全局小写会被当局部 → 未定义引用错误。模块内优先用局部变量 / 结构体封装状态，避免跨函数全局地址不一致的
编译器已知缺陷（见 `.agents/skills/nolang-syntax` 的「Avoid Global Variables」）。

### 6.11 不要重写合法代码来「修复」疑似编译器 / 解析器 bug
遵循 §2 黄金法则：即使怀疑工具错误，也只报告，不修改合法代码。

---

## 7. 生成 Nolang 代码的检查清单

1. 全局常量 / 变量**大写**开头；局部变量 / 参数 / 函数名 / 结构体名**小写 kebab-case**；文件名用 `-`。
2. 每条语句独占一行；`;` 是注释标记，**不是**语句分隔符；文件以单个末尾换行结尾。
3. 函数用 `name = (inputs) (outputs) {}`；方法用 `type.method = ...`，接收者用 `.`；优先 `?t` 而非 `(val, ok)`。
4. 枚举 `name { a, b(v t) }`；匹配用 `x: { ... -> ...\n -> 兜底 }`；多语句臂体用 `-> { ... }`。
5. 循环用 `(cond) { }` / `!! { }` / `! { }` / `n * { }` / `i <- [a..b]: { }`。
6. 协程衍生用 `co` 或协程组 `{}`；不用 `go`（非关键字）、不用手写 `run`/`awy`。
7. 注解 `#{...}` 独占一行置于目标上方（或 trailing）；不手写 `free`；字符串拼接用 `-`。
8. 跨模块调用用 `ShortName.` 前缀；标准库自动加载，不要重新加裸 `# std/fmt`。
9. 4 空格缩进、换行分隔语句、`{` 同行；提交前对改动文件跑 `no fmt`。
10. 引用标准库 API 以 `src/std/*.no` 实际签名为准，不臆造；优先复用既有标准库实现。
11. 改完 `src/std` 或编译器后，`make no` 重编（必要时 `make gen`），再 `make test` 验证。
