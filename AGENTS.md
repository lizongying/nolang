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
| `glossary/` | **Nolang 词条化知识库**（244 条 / 10 域）。数据是 `glossary/data/*.noml`，前端是 Nolang 写的 CLI + MCP。**查语言名词、std 模块、已知坑先查它**：`./glossary/dist/glossary get <id>`、`search <关键词>`、`list --domain=traps`。详见 `glossary/README.md`。 |
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

no build glossary            # 构建词条库前端 -> glossary/dist/glossary
./glossary/dist/glossary check   # 词条一致性体检（基线 0 error / 0 warning）
```

⚠️ 修改 `src/std/*.no` 后**必须** `make no`（重生成签名表）；直接 `go build` 会因签名表与源码
不一致而被编译器**拒绝构建**。绝不要对 `src/std` 跑 `no fmt -w`：单档解析会丢注解与手写排版，
需要格式化时一律 `no fmt <f>` 输出到 /tmp → `diff -u` → 只 `patch` 目标 hunk。
（`#{index-out}` 已于 D11 全量移除，不得再新增。）

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
```

```no
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
| 区间遍历 | `i <- [a..b]: { }`（四种括号组合，start > end 自动递减） |
| 容器遍历 | `i <- xs: { }`（数组 / 字符串（逐码点 `char`）/ vec） |
| 条件匹配 | `x: { ... }` |
| 分支选择 | `{ cond -> body }`（短路链） |
| 跳出 / 继续 / 返回 | `*`（继续）`**`（跳出）`***`（返回）；**简写暂不推荐，推荐用关键字** |

**其他关键字**：`return`（别名 `rtn`）`break`（别名 `brk`，暂保留）`continue`（别名 `ctn`，暂保留）`true` `false` `nil`
`as`（**仅用于 FFI 指针类型转换**，如 `*byte` / `**byte` / `*i64`）`ptr` `chan` `map`
`co`（无色 async 衍生）`self` `super` `it`。

**`it`**：match 臂中的**隐式绑定**。`ok -> process(it)` 中 `it` 是解包值；`err ->` 臂中
`it` 始终是**结构化错误 `err.err`**（std/err：`code` / `msg` / `file` / `line` / `col`），与元素类型无关——
取纯消息用 `it.msg()`、带错误码与位置用 `it.format()`、比错误码用 `it.is(code.io)`；
直接把 `it` 当 `str` 用（拼接 / 赋值给 `str`）会编译失败。`nil`/`err` 未被同时显式认领时，
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

    err -> log.error(it.msg()); it 是 err.err，不是 str

    ok -> process(it); it 是解包值
}

; if/elif/else 写成 {} 包裹的短路链
{
    cmd == 'a' -> {
        fa()
    }

    cmd == 'b' -> {
        fb()
    }

    -> {
    }
}
```

### 3.3 类型系统

**基础类型**：`byte` `bool`（仅小写）`char`（双引号单码点，如 `"中"`）`str`（UTF-8 字符串，
单引号 `'hello'` 或反引号原始串）`txt`（定长 256 字节串，须显式标注 `t txt = 'abc'`）
`i8 i16 i32 i64`（默认整数，架构无关）`i128` `u8 u16 u32 u64 u128` `usize`（仅 FFI）
`f32 f64`。

> ⚠️ **溢出语义特例**：`byte` / `char` 算术**无 option 机制**、不受 `#{overflow}` 注解管辖——
> `byte` 溢出静默 2's 补数回绕；`char` 码点算术超出合法码点范围（`< 0` 或 `> 0x10FFFF`）得 `-1`。
> 对比：`i8`–`i128` / `u8`–`u128` 一般用于真实业务数值，默认溢出 `err`（option），回绕须 `#{overflow=wrap}`。
> 需要检测 byte/char 溢出时先转宽型别（如 `b.to-i64()`）再算。

**容器**：`obj` `map[k]v` `arr`（定长 `[n]t`、`[?]t` 自动推断长度）`vec`（变长 `[]t`）
`slice`（视图，无独立类型，须附着在 arr/vec/str 上）。

**特殊 / 高级**：`*t`（指针，仅 FFI `#{c}` 声明与标准库）`any`（仅标准库）`bigint` `err`。

**Option（可失败 / 可空）**：前缀 `?` —— `?i64` `?str` `?[]str`。`?t` 是三态标签枚举
`option { ok(v t), nil, err(e err.err) }`：`ok`（有值，隐式绑定）`nil`（空）`err`（结构化错误
`err.err`，由 `err('msg')` / `err('msg', code.io)` 构造，位置在构造点自动注入）。
解包优先用 `?t` 而非 `(val, ok bool)` 双返回。

**联合类型别名**：`name = t1 | t2 | ...`，编译期按实参单态化（每个成员生成一份具体函数）。
`int` / `float` / `num` / `string` 都是 std 用此语法定义的普通代码，**不是关键字**。

```no
int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128
string = str | txt
bytes = []byte; 单类型别名（newtype 语义：别名与底层类型互不相通）
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
- **别名规则**：输入只读引用与输出槽指向同一对象是合法的（输入只读、输出只写，编译器不做静态别名检查），
  因此接收者 / 输入变量可以同时绑为输出目标。

```no
add = (a i64, b i64) (result i64) {
    result = a + b
}

parse-line = (s str, max-fields i64 = 1024) (fields []str) {
}

user.greet = () {
    print('Hello, ' - .name)
}
```

**Option 错误传播 vs 就地捕获**：

```no
; ?= 向上抛：函数须有 ?T 结果参数，失败时设结果并提前 return
process-file = (path str) (result ?str) {
    f ?= open(path); 失败 → result = f; return
    line ?= f.read-line(); EOF/错误 → 自动传播
    result = line
}

; 就地捕获：用 = ；函数无需 ?T 结果参数，nil/err 存入该变量、流程继续
handle = (b i64, c i64, d i64) (r i64) {
    a = b + c / d; a 推断 ?i64；除零 → a = err
    a: {
        err -> {
            r = -1
        }

        nil -> {
            r = -1
        }

        -> {
            r = it
        }
    }
}
```

### 3.6 控制流

- **match**：`x: {` 换行 `nil -> ...` 换行 `err(e) -> ...` 换行 `ok(v) -> ...` 换行 `-> ...` 换行 `}`（臂**只能换行**分隔；同一行写 `;` 会把后面的臂变成注释——`x: { ok(v) -> ...; -> ... }` 实测丢掉兜底臂，报 non-exhaustive match）。表达式位置构造枚举变体
  `ok(42)`、`rect(3.0,4.0)`。多语句臂体**必须用花括号** `-> { ... }`；单语句可跟在 `->` 后。
  臂间以**换行**分隔，不以 `->` 分隔（`pat -> A -> B` 在同一行是「一条带管线体的臂」，不是两条臂）。
- **match 主语三种形态**：值 `x: { }`、表达式 `x + 1: { }`（前两种是表达式，可在值语境取值）、
  块 `{ ... }: { ... }`（**只是陈述**，不能赋值、沒有区塊尾值，`-> _` 丟棄結果）。第三种把主体内
  `?=` 的上抛收斂成一條 match，因此**函数无需 `?T` 结果参数**也能用 `?=`：
  ```no
  f = (d i64) {
      {
          v ?= 6 / d
          print(v.to-str())
      }: {
          err -> print(it.msg())

          nil -> _

          ok -> _
      }
  }
  ```
  最近的块 match 捕捉（巢狀取最內層，且優先於函式的 `?T` 結果參數）；捕捉遞迴到裸塊/if/迴圈/臂體，
  函式定義是邊界、函式字面量不是。主體走完匹配 `nil`，`?=` 失敗匹配該次 `nil`/`err`；
  **一次失敗停住整個主體**（後續陳述與迴圈 iteration 都不跑），arms 只跑一次。
  穷尽性沿用标准三态 `?T` 规则：无兜底臂时即使 `ok ->` 不可达也必须补齐。
  `no fmt` 原樣往返 `}: {`；詳見 `docs/docs/lang/syntax.md`〈Match 主語的三種形態〉。
- **表达式主语的值位 match（2026-11-08 起）**：`expr: { ... }` 的 `: {` **优先于 `=`**，
  在赋值右侧是整段 RHS，因此 option 算术可一段式解包，**不要**再写
  `x-opt = a % b` / `x = 0` / `x = x-opt: { ... }` 三段式：
  ```no
  #{overflow=wrap}
  rem = seconds % 3600: {
      ok -> it

      -> 0
  }

  #{overflow=wrap}
  sec = rem % 60: {
      ok -> it

      -> 0
  }
  ```
  首次赋值可带显式型别（`rem i64 = seconds % 3600: { ... }`），二次赋值直接
  `rem = ...: { ... }`。主语是 D6 option 算术时归一化为 `__match_subj_L_C ?T`
  合成暂存（恰好求值一次、`ok -> it` 绑到 payload）；match 结果是 **plain T**
  并登记进语义副表，所以连续多段值位 match 不会丢 payload。`no fmt` 原样往返且幂等。
  该附加只在**值语境**（`=` / `?=` / 复合赋值的右侧）且 `: { ... }` 确为 match 块时生效；
  `cond: { body }` 条件循环与 struct 字面量不受影响。块主语 `{r ?= a % b}: { ... }` 不变。
  **字段目标同样算值语境（2026-11-09 起）**：`b.n = b.n + 1: { ... }`、`b.n ?= ...: { ... }`
  的 `: {` 绑在 RHS 上；此前漏推 `CTX_EXPR`，`:` 被语句层捡走套在整个赋值外层
  （`match(主语 = (b.n = b.n + 1))`，`->` 臂是死码、字段拿到隐式拆包的裸值）。
  索引目标同形，但 D9 禁下标直写，仍须 place-match 或 `?=`。
  **`src/std` 已于 2026-11-09 完成折叠**（122 处 / 22 档）：三段式、两段式、显式型别
  首次赋值、同函数内多次成对声明的中间变量，以及呼叫／索引／算术主语都统一成一段式；
  `ok -> it`、`ok(v) -> v`、常量臂、`-> _` 均实测等价，`no fmt` 幂等。编译器对「语义副表
  查到 `?T` 但宣告其实是 `i64`」的全域同名残值会回退 AST 回放，`it` 不会丢绑定。
  **型别可见性边界**：option 算术主语的两个运算元型别必须能被 parser 静态看见；
  若运算元来自深层方法链（连 AST 回放都推不出接收者型别），在宣告上补显式型别
  （`e i64 = flr.to-i64()`），否则归一化跳过、`it` 无绑定、观测值恒为 0。
  `src/std/number.no::f64.to-str-general` 即此例，回归测试
  `TestValueMatchExprSubjectASTAnnotatedOperand`（拿掉标注即红）。
  **保留 9 处**：内层 match 会写回 `-opt` 本身、两层链式 option match、按值分派
  （errno→讯息）、宣告与 match 之间有其它陈述或中间变量跨分支累积 —— 折了会改变
  求值时机或语义，别折。
- **if/elif/else**：写成 `{}` 包裹的短路链，条件臂各占一行（`{` / `c1 -> b1` / `c2 -> b2` / `-> b` / `}`）；独立单行 `cond -> body`
  是独立 if，不与相邻 if 短路。**但 else 强制成组**：独占一行的 `->` 写在 `{}` 外是 **ERROR**——解析器只会把它
  折叠到**最近的一条**裸 if，上方兄弟 `cond ->` 臂静默失效（字面读起来像互斥，实际不是）。所以：有 `->` else 就
  把所有臂包进 `{ ... }`；**只有不带 else 的孤立 `cond -> body` 允许裸写**。
- **循环**（按条件 / 次数反复执行）：`!! { }` 或 `true { }`（无限）、`(cond) { }`（条件）、
  `! { }` / `false { }` / `() { }`（永不执行）、`n * { }`（计数，`n <= 0` 时体不执行）。
- **遍历**（逐个取元素，**不是循环**）：区间 `i <- [a..b]: { }`，括号有 `[a..b]` / `[a..b)` /
  `(a..b]` / `(a..b)` 四种组合、端点可省略为无界，`start > end` 时自动递减；容器 `i <- xs: { }`
  支持数组 / 字符串（逐码点，得到 `char`）/ vec。区间遍历与容器遍历都只是遍历，不能与上面的循环构造混用。
- **占位陈述 `...`**：合法但**什么都不做**，`build` 时整条丢弃（产物与没写这行完全相同）。只能
  **独占一行**（行尾可跟 `;` 注释），用于骨架 / `TODO` / 文档演示。它**不是** `return` 别名，也
  **不是**范围运算符——`fail -> ...`、`x = ...`、`[0...n)` 一律语法错误；范围写 `..`，空函数体写 `{ }`。

### 3.7 运算符与惯用法

- **字符串拼接用 `-`**（惯用）；`+` 在字符串上也拼接、在数值上是算术。`s[i]` 对 `str` 按**码点**索引。
- 算术 `+ - * / %`、位运算 `& | ^ &^ << >>`、比较 `== != < <= > >=`、逻辑 `&& || !`。
- `*` 是 `continue` 简写、`**` 是 `break` 简写、`***` 是 `return` 简写（仅语句起始；**这些简写暂不推荐使用，请写关键字 continue / break / return**）；`!!` 等于 `true`（表达式）。
- 关键字别名（lexer 级完整支持）：`ctn` ≡ `continue`、`brk` ≡ `break`、`rtn` ≡ `return`；`no fmt` 保留别名原拼写。
- `?=` option 解包 / 传播（见 §3.5）；`x ?T = expr` 显式标注 option 路径（含运行时溢出检查）。

### 3.8 接口

```no
json {
    to-json()
}

; 接口定义（方法签名，或合并父接口名）
user json {
    ; 类型实现接口（同模块无需前缀；跨模块须 module.prefix）
    name str
    age i64
}
user.to-json = () (s str) {
    s = 'json:' - .name
    to-json(); 调用父接口实现（super）
}
```

接口可实现多个（`user json, noml { ... }`）；可有默认实现；`enter` / `leave` 接口在作用域进入 / 退出时
自动调用。

### 3.9 注解 `#{...}`

注解是编译指令。**合法位置只有两种**：目标上方独占一行，或目标同行的尾部（trailing）。
**同行的行首前缀写法（`#{...} target`）是编译错误**（对所有注解目标一致）。

```no
#{overflow=wrap}
digits = digits + 1

; 整数溢出策略（wrap / clamp0 / min / max / saturate / 默认）；只作用于下一陈述：
; 置上方须独占一行，或写在同一行尾随（尾随同样合法）

#{buildin}
str.byte = (i i64) (c ?byte) {
}

; 内建方法 / 内建枚举（无 std 函数体，codegen 拦截展开）

#{inline}
holder {
    ; 结构体字段按值内联（默认）；#{inline=false} 强制指针（递归类型必需）
    inl pt #{inline=true}
    out pt #{inline=false}
}
```

> ⛔ `#{overflow=...}` **只支持行作用域**：独占一行置目标陈述上方（或同行尾随），一行一注解。
> 写在函数头 / 块头 / 文件头**不合法**（当前 `no vet` 可能不报，属待收紧漏洞），详见 §3.9.1。
> ⛔ `#{index-out=...}` **已废弃并移除（D11）**：索引读取恒返回 `?T`，只能显式用 match 或 `?=` 消费，
> 写回也须 place-match / `?=`。残留注解是编译错误，不存在「默认零值」这条捷径。

### 3.9.1 注解作用域（四层）

注解的作用域由**宿主节点的种类**决定，不由注解内容决定。位置规则仍是两条：上方独占
一行，或目标同行尾随（行首前缀 → 编译错误）。

| 层级 | 宿主 | 生效范围 | 现状 |
| --- | --- | --- | --- |
| **L0 模块** | 文件头（与后续内容隔至少一个空行） | 本文件全部声明与语句 | 未实现 |
| **L1 声明** | 常量/全局变量、结构体、接口、标签枚举、函数、方法 | 该声明整体（函数含体与全部嵌套块） | 平台键 / `buildin` / `inline` / `generic` / `track-caller` / `embed` / `c` 家族 ✓；**`overflow` ✗ 禁止** |
| **L2 块** | 协程组、if 组、`cond -> body`、match、`for`/计数/区间循环 | 该块体全部语句（含嵌套块，止于内层 L1 边界） | 平台键待实现；**`overflow` ✗ 禁止** |
| **L3 单行** | 单条陈述（`let`/赋值/`?=`/`return`/调用/`cond ->` 臂） | 该陈述及其表达式 | 全部键 ✓ |

规则：

1. **由外向内继承，就近覆盖**：L0 → L1 → L2 → L3；同一节点同名键后者胜
   （`#{mac-arm64} #{mac-arm64, win-amd64}` 以后者为准）。
2. **函数体是硬边界**：L2 块的作用域止于语法子树，不沿调用关系扩散，也不进入任何函数/
   方法/匿名函数的定义体。一个函数体用什么模式，只由它自己的注解决定。
3. **成员粒度**：结构体字段、枚举变体上的注解管该成员（`a pt #{inline=false}`）。
4. **失效必须报错**：非法的「键 × 层级」组合（如把 `#{buildin}` 写在 for 上方）应报错，
   不得静默忽略。`#{index-out}` 已移除，出现即报错。
5. ⛔ **`#{overflow}` 只支持 L3（行作用域），这是刻意的限制，防止滥用**：
   - 它是**逐运算**的语义开关。写宽了就变成「一行注解管一片」，读代码要向上追溯「这条运算
     到底用哪个模式」，而 `no fmt` / `no vet` / codegen 三方维持同一结论极难（现状已漂）。
   - 它**不改类型**：结果恒为 `?T`。块级/函数级 `wrap` 只会把「溢出即 `err`」静默换成
     「静默回绕」——错误从显式失败变成看起来成功的错值。
   - 所以：**一行一注解**。`no fmt --fix=overflow` 与 LSP quickfix 产出的正是这个形式。
   - ⚠️ 当前漏洞：写函数头会让 `no vet` 整函跳过（codegen 仍 `err`）；写块头会让 `no vet`
     向下继承而闭嘴（codegen 仍不认）。**别依赖这两种写法**，它们会被收成编译错误。

```no
; ✅ 唯一合法形式：注解紧跟它管辖的那条陈述
#{overflow=wrap}
c = a + b

; ✅ 尾随等价
c = a + b #{overflow=wrap}

; ⛔ 写在函数头 / 块头 / 文件头都不合法（当前 vet 可能不报，属待收紧漏洞）
; #{overflow=wrap}
; f = () (r i64) { ... }
```

完整模型、键 × 层级矩阵、实测证据与收紧顺序见 `docs/docs/lang/annotation-scope.md`。

### 3.10 导入

```no
# std/math.add; std 模块
# github.com/utils/math.add; 远程模块
# /utils/math.add; 本地模块（以 / 开头）
# std/math.add a; 别名
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
| `str` | `.len()`(码点数) `.len-bytes()` `.cap` `.byte(i) ?byte` `.to-upper()` `.to-lower()` `.index()` `.contains()` `.slice()` `.copy()` `.fill()` `.bytes()` **`.push(c char)` `.push-str(s)`**；`with-cap(n)`；`s[i]` 码点索引读；**`s[i] = v` 与 `set-byte` 均已禁用**（见 §5.4） |
| `txt` | `.len()`(码点数) `.len-bytes()` `.byte(i) ?byte` `.to-str()` `.to-bytes()` `.index()` `.contains()` `.slice()` `.fill()` **`.push(c char)` `.push-str(s)` `.push-txt(t)`**；`t[i]` 码点索引读；**`t[i] = v` 自 2026-11-12 起同样禁用**（增长只走 push 家族） |
| `vec` / `arr` | `vec-create(n, val)`；`[]t.eq(b)`；原地排序 `.asc()` `.desc()`；切片视图 `v[a..b)` |
| `int` / `float` / `num` | `int.to-str()` `div` `mod`；`f64.sin() cos() tan() degrees() radians()`；`math.PI` `math.E`；具体类型转换见 `number` |
| `err` | `err.new(msg, code=code.unknown)` `from-errno(errno)`；`code` 枚举（ok/not-found/permission/io/timeout/parse/invalid/overflow/unknown）；`e.is(ec)` `e.msg()` `e.code()` `e.format()`；**`?t` 的 `err ->` 臂绑定的 `it` 就是这个 `err.err`（消息靠 `it.msg()`）** |
| `fmt` / `log` | `print(...)` `format('{x}')`（仅认简单标识符，不解析 `.field`）；`log.debug/info/warn/error(msg)` `log.fatal(msg, code)`（**无**裸 `log(x)` 调用） |
| `json` / `yaml` / `toml` | `t.to-json()` `t.to-noml()` 等序列化方法（接口方法，由实现类型提供） |
| `fs` / `io` / `os` | `file` 结构体、`fs.list-dir`（**含 `.`/`..`，递归删除须用 `fs.dir-entries` 或显式跳过**）、`os.strerror(errno)` |
| `net/*` | http / http2 / http3 / ws / quic / tls / sse / server / dns / url 等 |

> 不确定时读对应模块文件，勿臆造 API。优先复用标准库（如 `str.to-bytes()`、`[n]t.to-vec()`、
> `[]byte.to-hex()`、`str.to-i64()`、`int.to-str()`），不要手写等价实现。

### 4.3 代码组织方式

- **方法挂载式**：类型相关行为写成 `类型.方法`，而非自由函数传类型参数。
- **buildin 方法**：性能敏感或需 codegen 拦截的方法（如 `str.byte`）用 `#{buildin}` 标注，函数体为空。
- **`#{buildin}` 的三种载体**：① 函数桩（实现由编译器或 FFI 提供）；② 标签枚举（如 `option`，变体供穷尽性检查）；
  ③ **布局结构体**——`src/std/{str,vec,txt,arr}.no` 开头那份 `#{buildin} txt { data [255]byte; len u8 }`
  **只是布局文档**（LSP hover / docs 用），语义上不存在，真实布局在 MIR 手写描述符或编译期常量里。
  因此：布局结构体**只能**用 `str` / `vec` / `txt` / `arr` 这四个名字，而**用这四个名字声明 struct 必须带 `#{buildin}`**。
  两条由 `checker.ValidateBuiltinStructDecls` 报错（`1a8m445h` 缺注解 / `3lu6n4np` 注解贴在别的 struct 上）；
  漏掉就会生成 `%txt_txt` 之类的影子结构体（症状：`unknown callee txt.to-str`、
  `unsupported receiver type %txt_txt`）。详见 `docs/docs/lang/annotation-scope.md` §4.0。
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
NO-LANG = 'nolang'; 全局常量，大写
MAX-SIZE = 1024
_PRIVATE-CONST = 42; 私有全局：_ + 大写
add = (a i64, b i64) (r i64); 函数名小写
user {
    ; 结构体名小写
    name str
    age i64
}
; 局部变量（函数内）小写、无 _ 前缀
;   x = 10 ; foo-bar = 42
```

```no
; ❌ 反例
x = 10; 全局不能小写 → 错误
foo-bar = 42; 全局不能小写 → 错误
_X = 10; 局部变量不要 _ 前缀
StringHelper.no; 文件名用 _ 而非 -
```

### 5.2 文档注释

函数文档注释须含完整参数名与类型、返回名与类型；模块顶部 API 摘要同样用完整签名。

```no
; ❌ 缺类型、缺返回名
; sha1(data) (hash)
```

```no
; ✅ 完整签名
; sha1(data []byte) (hash [20]byte) — full hash
; data []byte: input byte array
; returns hash [20]byte: 20-byte hash value
sha1 = (data []byte) (hash [20]byte) {
}
```

**不要用分割线 banner**（`; ═══════ 标题 ═══════`、`; ─── 标题 ───`、`; =====`）。理由不只是排版噪音：
注释在 AST 里只有一条归宿——成为**下一条声明**的 doc，所以 banner 会被并进下一个函数/类型的文档头，
读者拿到的 doc 开头就多了一段与它无关的章节名。章节标题写成普通注释行；若它确实是**独立注释**
（不是写给下一条声明的），紧跟一行 `...`（占位陈述）把它挂住。

```no
; ❌ 分割线 banner，且会被并进 to-str 的 doc
; ═══════════════════════════════════════
; 数值转字符串
; ═══════════════════════════════════════

; i64.to-str(v i64) (s str) — 十进制
```

```no
; ✅ 标题独立，`...` 把它挂在自己身上；下面才是 to-str 的 doc
; 数值转字符串
...

; i64.to-str(v i64) (s str) — 十进制
```

### 5.3 格式化

- **4 空格缩进，禁用 Tab**（格式化器 `src/fmt/api.go` 用 `strings.Repeat("    ", indent)`）。
- 以 `no fmt` 的输出为唯一权威；提交前对改动文件跑 `no fmt -w`。
- 语句以**换行**分隔（不是 `;`；`;` 是注释标记）。`{` 与构造同行，`}` 独占一行。
- 注解 `#{...}` 独占一行置于目标语句上方，或 trailing 于目标同行。
- `no fmt` 自动：去重导入、删除裸 `# std/fmt`、按 path→函数→别名排序导入；保留带注释 / 注解的导入块原序。

### 5.4 惯用写法

- 字符串拼接用 `-`；遍历用 `<-`（区间 / 容器）形态，不要用条件循环手动模拟索引递增；枚举 / option 解包用 `match`（`x: { ... }`）。
- 方法链式调用：`e.format()`、`s.to-upper()`、`v.asc()`。
- 需要预构建字符串缓冲时，先想清楚是「定长」还是「变长」：定长用 `x str = with-cap-len(n, n)`（len 已就位，可 place-match 写）；变长用 `x str = with-cap(n)` + `s.push(...)` / `s.push-str(...)`（原地扩容，倍增）。逐**字节**拼装仍推荐 `x []byte = with-cap-len(n, n)` + `x[i] = b` + `x.slice(0, k).to-bytes-str()`；原地覆写已有内容用 `s.fill(val byte)`；下标写只剩 place-match `s[i]: { ok -> it = v / -> _ }`（以「读」作闸门，`i >= len` 静默跳过，永不改长度）。
  - ⚠️ **place-match 的闸门是「码点」不是「字节」**（闸门就是 `s[i]` 这个读，而 `str` 下标读按码点计）。它只适合 ASCII、一格一码点的场景（打连字符 / 打数字 / hex 查表）；**任何可能非 ASCII 或 `>= 0x80` 的逐字节拼装一律走 `[]byte`**。实测 `f = with-len(8)` 后用 place-match 写 224..231，只有前 4 格落盘，其余被闸门静默跳过（`path.split` 的非 ASCII 文件名就是这样被截断的）。
  - **`str` 的 `with-cap` 下标直写豁免已于 2026-11-11 撤销**（全域禁 `s[i] = v`）。`idxwrite` 的 with-cap 豁免现在只服务 `[]byte` / `[]T` / 固定数组 —— 它们 len=0 时确实只能靠直写增长。
  - **`txt` 自 2026-11-12 起同案禁止 `t[i] = v`**：它按码点重编码写回，长度会随写入隐式改变（`i` == 码点数为追加、超 255 静默丢弃）。txt 无堆无扩容，也没有 `_set-byte` 之外的拼接通道，变长一律走 `.push(c)` / `.push-str(s)` / `.push-txt(t)`。
  - 「全域」靠**四个**登记点合起来成立，写新检查时别只补一个：① 函数内局部变量（`IdxLocalTypes`）；② 顶层脚本裸名（`collectTopLevelVarRawForIdx` → `topIndexBaseRaw`）；③ 顶层 struct 字段基底（同一条 `topIndexBaseRaw`，靠 `structFields` 解 `b.t`）；④ 结果参数（`resStrTxtParams`，`collectStrTxtResultParams`）。后三者存在是因为 `IdxLocalTypes` 按函数作用域登记，顶层键为空串、结果参数不带标注、struct 字面量名不登记时都查不到型别。
  - 🔴 顶层那两处**只能**用来判 `r == "str" || r == "txt"`，绝不能把返回值整体赋给 `lt`：一旦 `lt` 被填上，顶层 `[]byte` / `[]T` / `[N]T` 的直写就会掉进 `isContainerWriteBase` 末尾的 `return true`（顶层 `curFunc` 为空、`capBases` 无条目），大批语料会翻红。`TestIndexWriteTopLevelNonStrTxtNotReported` 就是这条边界的警报器。
  - `set-byte` 同日私有化为 `_set-byte`，只有 std 内部（`str.fill` / `txt._from-bytes`）可调用；按字节拼 `txt` 用 `txt.from-bytes(b []byte)`。
  - `with-len(n)` / `with-cap-len(cap, len)` 的 len 已就位，写入须 place-match 或 `?=`，直写报 `idxwrite`。
- 条件分支优先用行内 guard `cond -> stmt`，复杂分支用 `{}` 短路链。
- 可失败的容器操作 / 查找 / 解析优先返回 `?t`（空用 `nil`、错误用 `err('msg')`），而非 `(val, ok)`。
- option `?t` 解包优先用隐式绑定 `ok -> ...(it)...`，而非具名 `ok(v) -> ...(v)...`（少一个绑定名，规避 §6.3 串型坑）；用户自定义枚举变体的载荷仍须具名 `ok(v) ->`（见 §3.2 最佳实践）。
- **输出槽直接绑回已有变量，不要为「转存」新增一次性中间变量**：调用端的输出槽可以直接写入已存在的变量，即使该变量同时是方法接收者 / 输入（只读引用）也合法（§3.5 别名规则：输入只读、输出只写，编译器不做静态别名检查）。实测 `q, r = r.div-mod(m)` 与「先接临时变量再写回」完全等价（`no build` / `no vet` / `no fmt` 全绿）；用不到的输出用 `_` 丢弃，可以一个新变量都不引入。

```no
; ✅ 推荐：余数直接写回 r（r 同时是方法接收者）
qsq, r = r.div-mod(modulus)

; 商完全用不到时，连 qsq 都不必新增
_, r = r.div-mod(modulus)
```

```no
; ❌ 避免：新增一个只用一次的 msq 做转存
qsq, msq = r.div-mod(modulus)
r = msq
```

> 这是**新写代码**的推荐写法，不是重构令；既有合法代码不要为风格批量改写（§2 黄金法则 / §6.11）。

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
```

```no
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
```

```no
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

    err -> log.error(it.msg())
}
```

### 6.4 `#{overflow=...}` 只作用于下一陈述：上方独占一行，或同行尾随
两种写法等价。放在函数体里不会「整函生效」；行首前缀写法（`#{...} target`）是编译错误。
**这是刻意的设计限制（防止滥用），不是待补的实现缺陷**：`overflow` 只认行作用域，因为它是逐运算的
语义开关，放宽到块/函数级会让「某条运算用哪个模式」需要向上追溯，且 `no fmt` / `no vet` / codegen
三方极易漂移（现状已漂：函数头注解只让 vet 闭嘴、运行期照旧 `err`）。详见 §3.9.1。

```no
; ✅ 正例
#{overflow=wrap}
digits = digits + 1
```

```no
; ❌ 反例（误以为整函数生效 / 与语句同行 / 行首前缀）
; #{overflow=wrap} digits = digits + 1
; #{overflow=wrap} digits = digits + 1   ; 前缀写法 → 编译错误
```

存量已用 `no fmt --fix=overflow` 下推过三轮：它先摘掉写在函数头（L1）/ 块头（L2）上的注解
与「挂不上任何节点」的死行，再按 checker 的报告逐条补 L3，只动注解行（三轮 `diff` 逐行核对：
改动全部是 `#{...}` 行，0 行代码）。**判据是「摘掉会不会改变编译结果」——不会**：
L1/L2 的 `#{overflow}` 对 codegen 与编译期检查都无效（`for` 头加与不加，`15w45dqk` 逐字
相同），只让 `no vet` 少报 hint。现状：`overflow` 注解行 4080，死行 0；剩 51 处被 `no vet`
报告但当前语言标不了（`{` / `(` 起首的 L2 区块宿主——`overflow` 只认 L3，放宽它们等于把
L2 语义合法化，要先做「先收紧再放宽」的顺序）——成因、实测证据与待办见
`docs/docs/lang/annotation-scope.md` §7 与 §8 第 4 步。`.` 起首的陈述（`.n = .n + 1`）
已经可以挂注解（`ASTVersion 2026-11-12.1`）。

### 6.5 不要定义会遮蔽成员方法的 union 别名方法
为 `string = str | txt` 定义 `string.len` 之类的别名方法，会接管 `s.len()` 调用并递归分发回自身 → 段错误。
成员方法直接用 `str.*` / `txt.*` 原版。

### 6.6 绝不对 `src/std` 跑 `no fmt -w`
`no fmt` 单档解析会丢注解与手写排版，改 std 后只 `make no` 重编；需要格式化时走
`no fmt <f>` → /tmp → `diff -u` → 只 `patch` 目标 hunk。（D11 起 `#{index-out}` 已移除，不再有
「build 必需注解」这一层，但排版漂移的风险仍在。）

### 6.7 `?=` 简写：option 结果参数 **或** 块 match 主体，二者其一即可
`?=` 是 option 解包 + 向上传播的**简便写法**。它在下列**任一**情形下都可用，不必先给函数加 `?T` 结果参数：

1. **所在函数有 `?T` 结果参数** —— 失败时把该 option 写入结果参数并提前 `return`；
2. **陈述位于陈述层级块 match** `{ ... }: { ... }` **的主体内** —— 由**最近的**块 match 捕捉
   （巢状取最内层，优先于函数的 `?T` 结果参数）；失败时**整个主体停住**、arms 只跑一次，**不是** `return`。

两者皆非才是编译错误。`err -> _` / `-> _` 两臂可以把上抛的错误就地吞掉，因此「无 `?T` 结果参数的函数
也想用 `?=`」的标准兜底就是把函式体包进块 match。原地捕获（不中断流程）用 `=` 绑定 `?T` 后再 match / `_ =`。
结果参数自动初始化为 nil（实测空函数体 `q = () (result ?i64) { }` 返回 nil），**无需写 `result = nil` 样板**（见 §3.11「返回值延迟零值」）。

```no
; ✅ 正例 1：option 结果参数 —— 直接写简写
process = (path str) (result ?str) {
    f ?= open(path); 失败 → result = f; return
}

; ✅ 正例 2：函数返回值不是 option —— 用块 match 包住，照样能用简写 y ?= x+1
handle = (x i64) {
    {
        y ?= x + 1; x+1 回 ?i64；失败 → 主体停止、arms 跑一次
        print(1)
    }: {
        err -> _; 丢弃上抛的错误（it 是 err.err）

        -> _; 兜底臂，丢弃结果
    }
}

; ✅ 正例 3：索引读取同样适用（D11 起 arr[i] 恒 ?T）
pick = (a []i64, i i64) (out ?i64) {
    v ?= a[i]; 越界 → out = 该 option; return
    out = v
}
```

> 块 match 只陈述、不是表达式、不能赋值、没有区块尾值；`-> _` 丢弃结果。
> 索引**写入**同理：`a[i] ?= v` 是 place-match `a[i]: { ok -> it = v  -> _ }` 的简写。

```no
; ❌ 反例：既无 ?T 结果参数、也不在块 match 主体里
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
5. 循环用 `(cond) { }` / `!! { }` / `! { }` / `n * { }`；遍历用 `i <- [a..b]: { }`（区间）或
   `i <- xs: { }`（数组 / 字符串 / vec）——`<-` 是遍历不是循环，两者不要归为一类。
6. 协程衍生用 `co` 或协程组 `{}`；不用 `go`（非关键字）、不用手写 `run`/`awy`。
7. 注解 `#{...}` 独占一行置于目标上方（或 trailing）；作用域按 §3.9.1 四层判定
   （L0 模块 / L1 声明 / L2 块 / L3 单行），但 **`#{overflow}` 只支持 L3 行作用域**（防滥用）。
   不手写 `free`；字符串拼接用 `-`。
   **`#{index-out}` 已移除**：索引读 `a[i]` 恒 `?T`，必须 match / `?=` / 块 match 兜底消费；
   索引写用 place-match `a[i]: { ok -> it = v  -> _ }` 或 `a[i] ?= v`；map 写 `m['k'] = 1` 例外（put 无需解包）。
   显式 plain 型别标注（`v i64 = a[i]`）**不是**逃生口，同样要显式消费。
8. 跨模块调用用 `ShortName.` 前缀；标准库自动加载，不要重新加裸 `# std/fmt`。
9. 4 空格缩进、换行分隔语句、`{` 同行；提交前对改动文件跑 `no fmt`。
10. 引用标准库 API 以 `src/std/*.no` 实际签名为准，不臆造；优先复用既有标准库实现。
11. 改完 `src/std` 或编译器后，`make no` 重编（必要时 `make gen`），再 `make test` 验证。
