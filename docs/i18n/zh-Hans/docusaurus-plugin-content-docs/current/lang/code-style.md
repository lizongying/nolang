---
sidebar_position: 4
---

# 编码规范

## 档案末尾换行（Trailing Newline / EOF）

每个**非空**的 `.no` 原始档，**必须以恰好一个末尾换行符结尾**（也就是档案最后要有一个空行）。

- 档案**没有**末尾换行 → 补上一个。
- 档案末尾有**多个**空行 → 合并为单一末尾换行。
- **空档案**（0 位元组）→ 保持不动。

排除目录：`dist/`、`vscode-nolang/`、`node_modules/`。

理由：统一的 EOF 单一换行可避免 `git diff` 出现 "no newline at end of file"，让差异更干净，也让工具串接与拼接行为可预测。

### 自动化执行（工具链内建）

此规范由工具链**自动执行**，没有独立脚本：

- **`no fmt`**：`src/cmd/no/main.go` 的 `fmt` 子命令会原地格式化档案，并呼叫 `fmt.FormatFile`，保证恰好一个末尾换行。
- **LSP 存档格式化 / `textDocument/formatting`**：`src/lsp/server.go` 中的 `formatNolangCode` → `fmt.FormatFile`，在编辑器存档或手动格式化 `.no` 档案时，自动补上或合并 EOF 换行。

实作位于 `src/fmt/formatter.go`：

- `FormatFile(code)` 格式化整个档案，并呼叫 `ensureTrailingNewline`：去掉末尾所有 `\r` / `\n`（相容 CRLF 与多个末尾空行），再补上一个 `\n`。空档案或无法解析的内容会原样返回，避免破坏档案。
- `Format(code)` 是纯片段格式化函式（不加末尾换行），主要供单元测试使用；写入真实原始档时请改用 `FormatFile`。

## 导入语句自动整理（去重 / 移除无别名 std 导入 / 排序）

`no fmt`（及 LSP 存档格式化）会对**档案顶部连续的导入区段**（`# path[.fn] [alias]`）自动整理，实作位于 `src/fmt/imports.go`：

1. **去重**：完全相同（路径、函数名、别名逐字相同）的导入只保留第一条。
2. **自动移除所有无别名的 std 模组导入**：标准库模组由编译器隐式可载入（见 [跨模组调用前缀](./module)），`# std/noml`、`# std/math`、`# std/database/sql` 这类「无别名、无 `.函数`」的纯模组导入不绑定任何名称，写了也会被 formatter 移除。三个例外会保留：
   - 写了**别名**（`# std/noml n`）或**函数导入**（`# std/fmt.sprintf fm`）——这是刻意的名称绑定；
   - 导入行带**注释**或 `#{...}` 附着注解——用户写下的内容不可静默丢弃。
3. **自动排序**：按 路径 → 函数名 → 别名 字典序稳定排序。

保守边界：只要区段内任一导入带注释（上方独立行或同行尾随）或附着 `#{...}` 注解（如 `#{bind=ffi}` 原生绑定策略），整个区段**保留源序、不做移除**——排序会拆散注释与语句的对应关系，删除会连注释一起消失。此时仍会去掉「无任何附着内容的完全重复项」。

档案顶部之外（代码之后）出现的导入区段一律原样保留。

```no
#{fmt-skip}
; 格式化前                          ; 格式化后
# std/vec                           # aaa/lib
# std/noml                          # std/io
# std/io                            # std/vec
# std/noml n
# aaa/lib        ;→  无别名 std 导入（# std/noml、# std/vec、# std/io）移除；
                    ;   带别名的 # std/noml n 保留；其余按字典序排序
```

## 注释标记（Comment Markers）

Nolang 支援两种**单行注释**标记与一种**多行（块）注释**标记，语义如下：

- `//` —— 传统单行标记（注释到行尾）
- `;` —— 另类单行标记（2026-07-17 实作，注释到行尾）
- `;; ... ;;` —— 多行（块）注释（2026-07-18 实作，对称定界，未闭合则注释到文件尾）

```no
#{fmt-skip}
// 这是注释
; 这也是注释，语义相同
x = 1 ; 行内注释，注释到行尾

;;
   这是多行（块）注释
   可以跨多行
   以 ;; 结尾 ;;
y = 2 ;; 行内块注释 ;;
```

**块注释定界**：`;;` 开始、`;;` 结束，两者之间（含换行）都是注释内容；内容里的单一 `;` 不会结束块，只有 `;;` 才会。若找不到结尾 `;;`，则从 `;;` 开始一直注释到文件结尾。开始定界符 `;;` 之后**只允许空白直到换行**才算进入多行模式——同一行 `;;` 后面还有文字时，它是一条普通的单行注释（例：`;; 说明文字` 只注释该行，下一行仍是代码）。结束定界符同理，`;;` 之后必须只剩空白才算闭合。

**格式化保留原始标记**：`no fmt` 不会改变任何注释的标记——`;` 仍是 `;`、`//` 仍是 `//`、`;; ... ;;` 仍是 `;; ... ;;`。formatter 透过 `Comment.Marker` 记住原始标记并原样输出，块注释内容（含内部换行）也会逐字保留，因此格式化是幂等的。

**安全范围**：`;` / `;;` 出现在字串字面量（例如 `'text/plain; charset=utf-8'`、`index-from(';', pos)`）与 `//` 注释内部时，由 lexer 的字串扫描器与注释扫描器先行消费，不会被当成注释标记。

**注意 `cond -> X; Y`**：`;` 现在是注释，所以 `cond -> X; Y` 解析为「执行 `cond -> X`（求值 X 后丢弃）」加上行内注释 `; Y`——`X` **不会被赋值**。正确写法是 `cond -> X = Y`（stdlib 既有惯例，见 `arr.no`、`uuid.no`、`path.no`、`err.no`、`assert.no`）。若在原始码看到 `cond -> X; Y`，优先改为 `cond -> X = Y`。

**禁止单行多条语句写法**：`;` 只是注释标记，不能用来在同一行连接多条语句；逗号 `,` 同样不能连接语句。块 `{ ... }` 内的每条语句必须各自独占一行。把块压成一行是最危险的错误：

```no
#{fmt-skip}
; ❌ 错误：`; return }` 整段变成注释，块的 `}` 随之消失
cond -> { print('X'); return }
```

```no
; ✅ 正确：每条语句独占一行
cond -> {
    print('X')
    return
}
```

`cond -> { print('X'); return }` 实际只剩 `cond -> { print('X')`：块永远不闭合，`}` 之后的语句（往往是该函数剩余的整个函数体，甚至后续模块级函数）会被一并吞掉；编译器不一定报错，报错位置也可能严重偏移，极难定位。

## 布林字面量与 `== true` / `== false` 化简（2026-09-24 实作）

除了 `true` / `false` 关键字外，Nolang 还接受两种**布林字面量简写**：

- `!!` —— 等价于 `true` 的独立字面量。
- `!` —— 等价于 `false` 的独立字面量（仅当 `!` 后面不是可作为运算元起点的记号时；若后接 `换行` / `;` / 档尾 / `)` / `}` / `]` / `->`，即解析为 `false`）。

> 注意：这里说的是**表达式位置**的 `!!` / `!`，与回圈位置的 `!! { }`（恒执行）、`! { }`（不执行）不同。

这两种简写在**解析期**就会变成普通的 `BooleanLiteral`，因此 `f == true`、`f == !!`、`f == false`、`f == !` 四者在语法上完全等价，走同一套处理逻辑。

`no fmt` 对布林的处理规则（实作于 `src/fmt/expr.go`）：

- **独立字面量统一为关键字写法**：`c = !!` → `c = true`、`c = !` → `c = false`。
- **已知的化简，未知的不化简**：当 `==` / `!=` 的其中一侧是布林字面量，且另一侧
  运算元**能确证为 bool 值**时，输出最简形式。四条管道共用同一份签名**收集口径**
  （`collectModuleSignatures`：std 内嵌表、`use` / `#` 指向的本地模组、
  EnumVariants）与同一个 `SemRebuildConfig.Apply` 注入契约；差别只在供表者：
  `no fmt` / LSP 由 `build.FmtSigLoader` 供表，`no build` / `no vet` 仍走
  `preloadModuleSignatures` + `astParseConfig`（两者尚未合并成单一 loader）。
  `no fmt` 在 fresh `SkipLowering` parse 之前注入；LSP 则由 `ParseDocument`
  注入后发布 `parseSnapshot`，format-on-save 复用同一棵糖 AST 的深拷贝
  （`fmt.CloneProgramForFormat`），预设设定下不再额外 parse。可确证的来源有三类：

  1. 显式型别标注：`b bool = true`、参数 `(b bool)`；
  2. 布林字面量赋值：`flag = true` / `flag = false`；
  3. 呼叫宣告回传 `bool` 的函式：**本档定义**（如 `bytes-eq = ... (eq bool)`），
     或签名表查得到的 std / 本地模组函式（如 `args.has-flag`、本地模组
     `helper.is-ok`）。变数 receiver 的方法呼叫（`s.starts-with('a')`）会依
     Sem 已推断的 receiver 型别（`s: str`）查 `str.starts-with`；型别没推断
     出来、或同名变数在不同作用域被推成不同型别时仍视为未知（刻意不做
     「方法名全域唯一」的猜测，以免把 `?bool` 误当纯 bool）。签名表查不到
     结果型别时仍视为未知。

  | 原始写法（下表的 `b` 为上述「已知」变数） | 格式化后 |
  | ------------------------------------------ | -------- |
  | `b == true` / `b == !!`                    | `b`      |
  | `b == false` / `b == !`                    | `! b`    |
  | `b != true` / `b != !!`                    | `! b`    |
  | `b != false` / `b != !`                    | `b`      |
  | `true == b`（字面量在左侧）                | `b`      |

  `!=` 只是翻转极性；`true == false`（两侧都是字面量）保持原样不动。
  `f == true` 这类**没有 bool 证据**的裸名会原样保留。
- **  `?T` 与一切未知运算元保留原比较**：option（`?T`）、签名表查不到结果型别的
  跨模组呼叫、无赋值的裸名、型别未推断出来的点号存取、呼叫式、运算结果，一律保留
  `== true` / `== false`。折成裸 `x` / `! x` 当控制流条件会让 MIR 撞
  「option-as-i1」codegen crash —— v0.3.3 正是因此把整条规则 revert
  （`8a8ecb59`）；2026-11-15 改成「正向确证才化简」后恢复，不再用
  「查不到 `?T` 就当值」的反向推论。同日起 `no fmt` / LSP 也会注入跨模组
  签名，`fs.write-str(...)`（`?bool`）这类 option 回传**看得见**，由 option
  硬闸门挡下，而不是靠「看不到型别」。option 是**独立硬闸门**
  （`boolOperandIsOptionTyped`），先于 bool 值证据判定，因此不论型别标注
  是否留在原始档都不化简：`ok ?bool = fs.write-str(...)` 与
  `ok = fs.write-str(...)` 两种写法都保留比较。`no fmt` 依既有规范
  （`fixRedundantTypeSource`）移除冗余型别标注是**正确行为**，不要为了
  保住 option 判定去关闭这道移除。回归测试：`src/fmt/bool_comparison_test.go`、
  `src/fmt/signatures_test.go`、`src/lsp/fmt_gate_test.go`。
- **在 `->` 分支 / 独立 if-then 条件中同样生效**（对已知变数）：
  `b == ! -> return` → `! b -> return`。
- **运算优先顺序安全**：否定写为 `! `（带一个空格）以符合前缀运算子的既有惯例，
  保证 `no fmt` 幂等。目前可确证的运算元都是识别字，`!` 不会重新结合；
  实作仍对非原子运算元保留括号防护（`! (a + b)` 而非 `! a + b`），
  以备日后放宽判定。

## 返回值变数延迟零值（Deferred Zero-Init for Return Values）

Nolang 函式的具名结果参数（out 参数）采用**延迟零值**策略：函式 prologue **不会**对 out 参数做任何零值初始化，编译器改以一个 bitmap `%__ret_init_bitmap` 追踪每个 out 参数是否在函式体内被显式赋值过。当函式返回时，任何**未被显式赋值**的 out 参数会自动补上对应型别的零值 store（整数 → `0`、str-long → `zeroinitializer`、struct → `zeroinitializer`、option → `nil`）。此机制与既有的 `%__move_bitmap` 延迟释放机制对称。

### 为什么这样设计

- **效能**：避免 prologue 对每个 out 参数都做一次 `store zeroinitializer`，再被函式体内的赋值覆盖掉——这是纯粹的冗余 store。延迟策略只在「真的没赋值」时才补一次零。
- **可读性**：不需要在函式开头写样板 `found = false` / `result = nil` 等提前赋零值的程式码，读者可以直接看到「成功路径才会赋值」的意图。
- **一致性**：与 `%__move_bitmap` 的延迟释放机制采用相同的 bitmap 追踪模式，编译器内部实作对称。

### 推荐写法

不要在函式开头对 out 参数做提前赋零值——编译器会在返回时自动补零。只写「成功路径」的赋值即可：

```no
; Good：不写提前赋零值，编译器自动处理
hashmap-str-tmpl.contains = (key str) (found bool) {
    val ?v = .get(key)
    val: {
        ok -> found = true

        err -> {
        }

        nil -> {
        }
    }
}
```

### 反模式（避免）

不要写冗余的提前赋零值——这只是被后续赋值覆盖掉的无效 store：

```no
#{fmt-skip}
; Bad：冗余的提前赋零值
hashmap-str-tmpl.contains = (key str) (found bool) {
    found = false              ; 冗余——编译器会在返回时自动补零
    val ?v = .get(key)
    val: {
        ok -> found = true
        err -> {}
        nil -> {}
    }
}
```

### Option 预设为 nil

对 `?T` option 型别的 out 参数，编译器在补零时直接写入 `nil`（tag=1, data=0），等同于 `result = nil`。因此「找不到 / 空集合 / EOF」这类路径只需裸 `return`，不需要在原始码写 `result = nil`：

```no
#{fmt-skip}
; Good：未命中路径用裸 return，编译器自动补 result = nil
hashmap-str-tmpl.get = (key str) (result ?v) {
    .size == 0 -> return        ; 编译器补 result = nil
    ...
    ; fall-through：编译器补 result = nil
}
```

### 除错提示

若函式返回了非预期的零值（例如 `found` 应为 `true` 却得到 `false`，或 `result` 应有值却是 `nil`），检查函式体内**所有应返回非零值的路径**是否都对 out 参数做了显式赋值。编译器不会猜测意图——它只会对未被赋值的 out 参数补零。详见 `.agents/skills/nolang-debug/SKILL.md` 的除错提示。

## 输出槽直接绑回既有变数（避免中继变数）

**推荐写法**：新写程式码时，若呼叫结果只是要接续更新某个既有变数，就把输出槽**直接绑到那个变数**，
不要新增一个只用一次的转存变数。这在多元输出、且接收者本身就是待更新状态时特别常见。

```no
; ✅ 推荐：余数直接写回 r（r 同时是方法接收者）
qsq, r = r.div-mod(modulus)
```

```no
; ❌ 避免：msq 只被写回一次，没有其它用途
qsq, msq = r.div-mod(modulus)
r = msq
```

- 用不到的输出以 `_` 占位：`_, r = r.div-mod(modulus)` 连一个新变数都不引入。
- 输入唯读引用与输出槽指向同一物件是合法的〈别名规则〉；`src/std/bigint.no` 的 `r = r._mul(r)`
  即同一种就地更新，单一输出也适用。
- 这是**新写程式码**的推荐写法，不是重构令：既有合法程式码不要为了风格改写（见〈黄金法则〉）。
- 完整语意、实测结果与适用范围见[语言参考：函数定义](./syntax)。

## Markdown 代码块（`no fmt -md`）

`no fmt -md` 只排版 ```` ```no ```` / ```` ```nolang ```` 围栏**内部**的内容，围栏行与其余 Markdown 逐字保留。块的内容按 Nolang 源码解析；解析不了就整块原样留底并回报行号，不会改坏。（`-md` 与 `-fix` 互斥，只管排版。）

### 正确示例与错误示例各自独立成块

同一块里既写「正确写法」又写「错误写法」，两者会被当成同一段程式：正确的那半失去 `no fmt` 校验，错误的那半又可能被排版「顺手修正」成看不出问题的样子。所以：

- **正确示例**单独一块，不加注解，交给 `no fmt` 排版与校验；
- **错误示例**单独一块，首行写 `#{fmt-skip}`，既不校验也不修改。

```no
; ✅ 正确：每条语句独占一行
cond -> {
    print('X')
    return
}
```

```no
#{fmt-skip}
; ❌ 错误：`;` 之后整行都是注释，块的 `}` 随之消失
cond -> { print('X'); return }
```

### `#{fmt-skip}` 的三类用途

1. **错误示例 / 反例**——本来就不是合法程式，排版只会帮倒忙。
2. **伪码与签名速查**——只有形状、没有实作的片段（如 `f = (a i64) (r i64) { ... }` 的签名骨架）。
3. **对齐注释列的快查表**——`no fmt` 会把 `code      ; 说明` 压成 `code; 说明`，而列对齐正是这类表格的可读性来源，必须过滤。

### 不要为了「阻止展开」而加注解

`no fmt` 会把空块 `{ }`、单行臂 `-> { x() }` 展开成多行：

```no
ok = (b bool) (yes bool) {
}

dispatch = (cmd str) {
    cmd == 'a' -> {
        fa()
    }
}
```

这是**要接受**的规范形——展开后块内才有独立的行可以插入后续语句，否则每次加一行都得先手动拆块。同理，`true -> {}` 变成 `true -> {\n}` 也不必回退：真正需要 `#{fmt-skip}` 的只有上面三类（反例、伪码、对齐表格）。

### 纯注释块不需要注解

整块只写注释（把正确写法「注起来」）的块解析得到**零条陈述**，`no fmt` 视为「没有可排版的东西」，原样留底、不回报——它本来就不是语法错误，加 `#{fmt-skip}` 只会掩盖事实。

### 顺序：先加注解，再排版

`no fmt -md -w` 会就地改写所有未被过滤的块。错误示例一旦被排版工具「修正」，反例就不成立了，而原始写法无从还原。**先补 `#{fmt-skip}`，再跑 `-w`。**
