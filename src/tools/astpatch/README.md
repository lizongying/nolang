# astpatch —— 基于 AST 的 Nolang 源码改写工具

`astpatch` 读取一个 `.no` 文件，解析成真正的语法树（AST），按一份 **JSON 补丁**
描述的结构化修改操作改写节点，再把改写后的 AST 用项目自带的 formatter
（`src/fmt`）重新打印输出为 `.no` 文件。

与正则/文本级改写（如 `add_modprefix`）不同，本工具只在 AST 节点上操作，
因此：

- 不会破坏语法结构；
- 不会误改字符串字面量、注释里的文本；
- 复杂结构（嵌套表达式、整段函数体、数组元素等）用 JSON 表达即可精确匹配。

## 位置

```
src/tools/astpatch/
├── main.go    # CLI、源码/代码片段解析、输出
├── op.go      # op 定义 + 基于反射的 AST 遍历/匹配/改写
├── dump.go    # -dump 调试输出（复用 parser/dump）
└── README.md  # 本文档
```

## 运行

模块根目录是 `src/`，须在 `src/` 下运行（与 `add_modprefix` 一致，用 `go run`）：

```bash
cd /Users/lizongying/IdeaProjects/no/src

# 补丁写在文件里
go run ./tools/astpatch -in /path/in.no -patch patch.json -out /path/out.no

# 补丁从 stdin 读取（-patch 省略时默认读 stdin）
cat patch.json | go run ./tools/astpatch -in /path/in.no -out /path/out.no

# 只看解析出的 AST，不改写（调试）
go run ./tools/astpatch -in /path/in.no -dump
```

### 命令行参数

| 参数      | 说明 |
|-----------|------|
| `-in`     | **必填**，输入 `.no` 文件路径。 |
| `-out`    | 输出 `.no` 文件路径。**省略则写到 stdout**。 |
| `-patch`  | 补丁 JSON 文件路径。**省略则从 stdin 读取**。 |
| `-dump`   | 只解析并打印 AST，不应用补丁。 |

处理流程：解析输入 → 若 `-dump` 则打印 AST 并退出 → 读补丁 → 依序应用每个 `op`
→ 用 formatter 打印 → 写出。每个 `op` 应用后会在 **stderr** 打印受影响节点数，
例如 `op[0] replace 影响 3 处`；便于确认匹配是否命中。

## 补丁 JSON 结构

顶层是一个对象，`ops` 是操作数组，**按顺序依次应用**（前一个 op 的结果是后一个的输入）：

```json
{
  "ops": [
    { "op": "rename", "from": "total", "to": "sum" },
    { "op": "replace", "match": { "kind": "StringLiteral", "value": "world" }, "code": "'earth'" }
  ]
}
```

空补丁（`{}` 或 `{"ops":[]}`）合法，等价于只做格式化。

> **注意 Nolang 字符串用单引号** `'...'`，双引号 `"..."` 是字符字面量。
> 因此 `code` 里的字符串要写成单引号；在 JSON 里单引号无需转义，直接 `"code": "'earth'"`。

## 节点匹配模式（match / anchor）

`match`（以及 `insert_stmt` 的锚点复用 `match`）是一个 JSON 对象，用来描述“要命中哪个
AST 节点”。规则：

- 特殊键 **`kind`**：节点的 Go 类型名（字符串），如 `"Identifier"`、`"CallExpression"`。
  省略 `kind` 表示不限类型，只按其它字段匹配。
- 其余每个键 = **节点上导出字段名**（大小写不敏感，精确名优先），值可以是：
  - **标量**（字符串 / 数字 / 布尔）：与该字段的标量值做相等比较；
  - **嵌套对象** `{...}`：把该字段当作子节点，递归匹配；
  - **数组** `[...]`：字段必须是切片，逐项按长度相等、逐元素匹配。
- 一个节点命中，当且仅当模式中**所有**键都满足。

匹配示例——命中调用 `print(...)` 的 `CallExpression`：

```json
{ "kind": "CallExpression", "function": { "kind": "Identifier", "value": "print" } }
```

命中 `1 + 2` 这个加法节点：

```json
{ "kind": "InfixExpression", "operator": "+",
  "left": { "kind": "IntegerLiteral", "value": 1 },
  "right": { "kind": "IntegerLiteral", "value": 2 } }
```

### 常用节点字段名对照

字段名即 `src/parser/ast.go` 中结构体的导出字段（去掉 `Token`/`CommentedNode` 等
非语义字段）。下表列出常用来匹配的键：

| 节点 `kind`        | 可匹配字段（键 → 含义） |
|--------------------|--------------------------|
| `Identifier`       | `value`（标识符名） |
| `IntegerLiteral`   | `value`（int64）、`raw` |
| `FloatLiteral`     | `value`（float64）、`raw` |
| `StringLiteral`    | `value`、`raw` |
| `CharLiteral`      | `value`、`raw` |
| `BooleanLiteral`   | `value`（bool） |
| `ByteLiteral`      | `value`、`raw` |
| `NilLiteral`       | （仅 `kind`） |
| `InfixExpression`  | `operator`、`left`、`right` |
| `PrefixExpression` | `operator`、`right` |
| `CallExpression`   | `function`、`arguments`（数组）、`genericArgs`（数组） |
| `DotExpression`    | `receiver`、`property` |
| `IndexExpression`  | `left`、`index` |
| `SliceExpression`  | `left`、`range` |
| `AssignExpression` | `left`、`value` |
| `ArrayLiteral`     | `elements`（数组）、`size` |
| `SliceLiteral`     | `elements`（数组） |
| `IfExpression`     | `condition`、`consequence`、`alternative` |
| `ReturnStatement`  | `returnValue` |
| `LetStatement`     | `name`（是 `*Identifier`，用嵌套对象匹配 `{ "kind":"Identifier","value":"x" }`）、`type`、`value` |
| `ExpressionStatement` | `expression` |
| `FunctionDefinition` | `name`（字符串）、`parameters`（数组）、`results`（数组）、`body` |
| `BlockStatement`   | `statements`（数组） |
| `Parameter`        | `name`（字符串）、`type`、`defaultExpr` |
| `StructDefinition` | `name`（字符串）、`fields`（数组） |
| `StructLiteral`    | `type`（字符串）、`fields`（数组） |
| `StructField`      | `name`（字符串）、`type`、`value` |
| `EnumDefinition`   | `name`（字符串）、`values`（数组） |

> 提示：`LetStatement.Name` 是 `*Identifier`，所以按变量名匹配 `let` 语句要用嵌套模式；
> 而 `FunctionDefinition.Name`、`Parameter.Name`、`StructField.Name` 本身就是字符串，
> 直接写 `"name": "greet"` 即可。

## 支持的 op

每个 op 必有 `"op"` 字段指明类型。以下为各 op 的字段与语义。

### 1. `rename` —— 按名字重命名标识符 / 函数 / 参数 / 方法调用属性

```json
{ "op": "rename", "from": "total", "to": "sum", "scope": "main" }
```

- `from` / `to`：原名 → 新名（必填，`from` 为空则不生效）。
- `scope`（可选）：只在**该名字函数的函数体内**重命名；省略则全文件。

命中的字段（当值等于 `from` 时改为 `to`）：`Identifier.Value`、
`FunctionDefinition.Name`、`Parameter.Name`、`DotExpression.Property`。
因为调用点的被调名也是 `Identifier`，所以对函数/变量的 `rename` 会同时更新**定义与调用**。

例：`{"op":"rename","from":"print","to":"println","scope":"main"}` 只改 `main` 里的 `print` 调用。

### 2. `replace` —— 替换匹配的**表达式**节点

```json
{ "op": "replace", "match": { "kind": "StringLiteral", "value": "world" }, "code": "'earth'" }
```

- `match`：节点模式（必填）。
- `code`：替换用的 Nolang **表达式**源码（必填），会经 parser 解析为表达式节点再放入原位置。
  `code` 必须是**单个表达式**。

遍历整棵树的所有表达式槽位（含二元/调用/索引等子表达式、参数列表、数组元素等），
命中即替换。可一次命中多处。

### 3. `replace_stmt` —— 替换匹配的**语句**节点

```json
{
  "op": "replace_stmt",
  "match": { "kind": "FunctionDefinition", "name": "helper" },
  "code": "helper = (x i64) (y i64) {\n    y = x * 2\n}"
}
```

- `match`：语句模式（必填）。
- `code`：替换用的**单条语句**源码（当前实现要求 `code` 解析后恰好为 1 条语句）。

用于整条语句级别的替换（函数体、`let`、`return` 等）。

### 4. `delete_node` —— 删除匹配的节点（仅在切片槽位生效）

```json
{ "op": "delete_node", "match": { "kind": "IntegerLiteral", "value": 2 } }
```

从**节点切片**里剔除命中元素，典型如：删掉数组/切片字面量里某个元素、删掉某实参。
（单个非切片的表达式槽位无法“删除”，因为没有可留空的位置，此时不生效。）

### 5. `delete_stmt` —— 删除匹配的语句

```json
{ "op": "delete_stmt", "match": { "kind": "FunctionDefinition", "name": "greet" } }
```

从 `Program` 顶层或任意 `BlockStatement` 语句列表中删除命中语句。

### 6. `insert_stmt` —— 插入语句

```json
{ "op": "insert_stmt", "code": "MAX = 100", "position": "top", "index": 0 }
```

- `code`：要插入的语句源码（必填，可为多行多条语句，将按解析结果依次插入）。
- `scope`（可选）：目标函数名。省略 = 插入到文件顶层；给定 = 插入到该函数体内。
- `position`（可选）：
  - `"top"`（默认）：按下标 `index` 插入（`index` 缺省或负数/越界 = 追加到末尾）。
  - `"before"` / `"after"`：在该作用域内**第一条命中 `match` 的语句**前/后插入；
    未命中锚点则退化为追加到末尾。
- `match`：`position` 为 `before`/`after` 时用作锚点模式。
- `index`：`position` 为 `top` 时的插入下标。

例：在 `greet` 函数体内、`print(msg)` 调用之后插入一行：

```json
{
  "op": "insert_stmt",
  "scope": "greet",
  "position": "after",
  "code": "println(msg)",
  "match": { "kind": "CallExpression", "function": { "kind": "Identifier", "value": "print" } }
}
```

## op 字段汇总

| 字段       | 用于 op | 含义 |
|------------|---------|------|
| `op`       | 全部 | op 类型（必填） |
| `match`    | replace / replace_stmt / delete_node / delete_stmt / insert_stmt | 节点匹配模式 |
| `code`     | replace / replace_stmt / insert_stmt | 替换/插入的 Nolang 源码片段 |
| `from` `to`| rename | 重命名源名与目标名 |
| `scope`    | rename / insert_stmt | 限定到某具名函数体 |
| `position` | insert_stmt | `top` / `before` / `after` |
| `index`    | insert_stmt | `top` 时的插入下标（负/省略=末尾） |

## 完整示例

输入 `in.no`：

```nolang
# std/fmt

greet = (name str) (msg str) {
    msg = 'hello ' + name
    print(msg)
}

main = () {
    total = 1 + 2
    greet('world')
    print(total)
}
```

补丁 `patch.json`：

```json
{
  "ops": [
    { "op": "rename", "from": "total", "to": "sum" },
    { "op": "replace",
      "match": { "kind": "InfixExpression", "operator": "+",
                 "left": { "kind": "IntegerLiteral", "value": 1 },
                 "right": { "kind": "IntegerLiteral", "value": 2 } },
      "code": "1 + 2 + 100" },
    { "op": "replace",
      "match": { "kind": "StringLiteral", "value": "world" },
      "code": "'earth'" }
  ]
}
```

命令：

```bash
cd /Users/lizongying/IdeaProjects/no/src
go run ./tools/astpatch -in ../tmp/in.no -patch patch.json -out ../tmp/out.no
```

输出 `out.no`：

```nolang
# std/fmt

greet = (name str) (msg str) {
    msg = 'hello ' + name
    print(msg)
}

main = () {
    sum = 1 + 2 + 100
    greet('earth')
    print(sum)
}
```

## 已知限制

- **输出格式**：结果始终经过 `src/fmt` 重排，缩进/空行遵循 formatter 规则，不逐字节保留原排版。
  新合成节点的 token 位置来自代码片段本身，个别相邻空行可能被规范化。
- **`replace_stmt`** 目前要求 `code` 恰好解析为 1 条语句。
- **`delete_node`** 只在节点位于切片槽位（数组元素、实参、语句列表等）时生效。
- **类型节点（`Type`）**：遍历会进入含 `Type` 字段的节点（如 `StructField.Type`、
  `Parameter.Type`）用于匹配，但不提供针对类型表达式的专用替换 op；如需改类型，建议
  用 `replace_stmt` 整条替换含该类型的语句。
- 匹配的键必须对应**真实导出字段名**；写错键名视为不匹配（该节点不命中），不会报错。
