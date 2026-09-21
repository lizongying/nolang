---
name: nolang-builtin
description: Nolang 内建函数（builtin）注册与查找机制参考。用于理解 fs.read-file、os.get-env 等内建函数的注册位置、查找流程、模块前缀解析，以及编写或修改 builtin 注册代码、排查 builtin 找不到的问题。涵盖 src/builtin/ 下所有 Go 文件（os.go、fmt.go、math.go、str.go、net.go、process.go、async.go、bits.go、vec.go 等）。
---

# Nolang Builtin Function Mechanism

Nolang 的内建函数（builtin）是由编译器直接生成 LLVM IR 的函数，不需要在 `.no` 源文件中提供实现。理解 builtin 的注册、查找和调用机制对排查"函数找不到"类问题至关重要。

## 核心概念

### 1. Builtin 不是标准库 .no 文件中的函数

`fs.read-file`、`os.get-env`、`os.now` 等函数在标准库 `.no` 文件中**以注释形式存在**（如 `fs.no` 中 `; read-file = (p str) (content []byte) { ... }`），仅作为文档说明。它们真正的定义在 `src/builtin/` 下的 Go 文件中注册。

### 2. 注册位置

所有 builtin 在 `src/builtin/` 包中通过 `init()` 函数注册到全局 `BuiltinMethodList`：

```
src/builtin/
├── builtin.go      # BuiltinMethod 结构体定义 + FindBuiltinMethod()
├── os.go            # fs/os 相关 builtin（read-file, write-file, open-read, etc.）
├── fmt.go           # print, eprint, format, printf, sprintf, eprintf
├── math.go          # max, min, abs, clamp
├── math_f64.go      # sqrt, sin, cos, log, pow, etc.
├── str.go           # with-cap, with-len, with-cap-len
├── net.go           # net-listen, net-dial, net-accept, etc.
├── process.go       # process-exec, process-kill, process-dup2, etc.
├── async.go         # async-cancel, async-cancelled, async-yield
├── bits.go          # rotate-left, rotate-right, load-le-u16/u32/u64
└── vec.go           # vec 容器内建（push 等）
```

> 已删除（2026-09-21）：`database.go`（15 个 `db-*`）与 `ffi.go`（3 个 `ffi-cstr-*`）。
> 原因见下文「半条命陷阱」——只注册、无 lowering。

### 3. BuiltinMethod 结构体

每个 builtin 注册时包含：

```go
BuiltinMethod{
    ReceiverType: ReceiverGlobal,   // 全局函数 vs 类型方法（str/vec/arr）
    MethodName:   "read-file",      // 裸名（不带模块前缀）
    Params:       []parser.Type{...}, // 参数类型
    Return:       []parser.Type{...}, // 返回类型
    Doc:          "Read entire file...",
    ForwardFunc:  "read-file",      // LLVM codegen 的转发目标名
    // 或 CLibCall / LLVMIntrinsic / LLVMConv
}
```

三种 codegen 模式：
- **ForwardFunc**: 编译器内联生成 LLVM IR（如 `read-file` → stat+open+malloc+read+close）
- **CLibCall**: 直接调用 C 库函数（如 `rename` → `call i32 @rename`）
- **LLVMIntrinsic**: 使用 LLVM 内联函数（如 `llvm.sin.f64`）

## `#{buildin}` 註解（內建聲明）

`.no` 標準庫中以 `#{buildin}` 標註的聲明表示「真實實作在 Go runtime / codegen，本體不參與
校驗與 codegen」。`#{buildin}` **不帶值**（`#{buildin}`，而非 `#{buildin=NAME}`）；查找一律
以**函式/變體自身的裸名**為鍵（`builtin.FindBuiltinMethod(calleeName)`），故無需在註解中
重複函式名。

兩種用法：

1. **內建函式樁**（`#{buildin}` 在函式定義前）：

   ```no
   ; read-file = (p str) (content ?str) { ... }
   #{buildin}
   read-file = (p str) (content ?str) { }
   ```

   解析器在 `parser/annotation.go` 的 `attachAnnotations` 中把 `FunctionDefinition.BuiltinStub`
   置真；此類函式體被跳過校驗與 codegen，呼叫改由 Go 側 `BuiltinMethod` 生成 IR。

2. **內建標籤列舉**（`#{buildin}` 在標籤列舉前）：

   ```no
   ; src/std/option.no
   #{buildin}
   option {
       ok(v t),
       nil,
       err(e str),
   }
   ```

   置 `TaggedEnumDefinition.Builtin`。變體（名稱/順序/載荷型別）登記進 `sem.EnumVariants`，
   驅動匹配與窮盡性檢查；但底層表示與構造由 builtin runtime 提供（`?t` option 用 `%option`
   型別與 `ok(...)`/`nil`/`err(...)` 構造器），**不生成使用者可見的 struct/union**。

> 歷史：早期寫法為 `#{buildin=NAME}`（帶 Go 側 builtin 鍵值）。由於查找只依賴裸名，
> `=NAME` 屬冗餘，已統一簡化為 `#{buildin}`。

## 查找机制

### FindBuiltinMethod（按裸名查找）

```go
// src/builtin/builtin.go
func FindBuiltinMethod(name string) *BuiltinMethod {
    for i := range BuiltinMethodList {
        if BuiltinMethodList[i].MethodName == name {
            return &BuiltinMethodList[i]
        }
    }
    return nil
}
```

关键点：**查找始终使用裸名**（如 `"read-file"`），不带模块前缀（`"fs.read-file"` 查不到）。

### 模块前缀调用解析流程

当用户代码写 `fs.read-file('path')` 时，解析链如下：

```
1. parser 解析为 CallExpression { Function: DotExpression { Receiver: "fs", Property: "read-file" } }

2. checker/funcargs.go lookupReturnCount():
   - 识别 receiver "fs" 不是已知变量 → 是模块名
   - 用裸名 "read-file" 调用 builtin.FindBuiltinMethod("read-file")
   - 找到 → 返回 builtin.Return 长度

3. checker/checker.go resolveModuleCalls():
   - 识别 "fs" 是已知模块（modSet）
   - "read-file" 不是 moduleFns（不是 .no 文件中定义的函数）
   - 不改写，保持 DotExpression 原样

4. MIR codegen (`src/mir/builtins.go` `lookupBuiltin` → `src/mir/builtin_call.go` `emitCall`):
   - 遇到 DotExpression { Receiver: Identifier, Property: string }
   - Receiver 是模块名 → 用 Property 裸名查 FindBuiltinMethod
   - 找到 → 按 ForwardFunc/CLibCall/LLVMIntrinsic 生成 IR
   - 找不到 → 报 `'xxx' is not defined`
```

### 同名冲突处理

当 builtin 裸名与标准库 `.no` 文件中定义的函数同名时：

1. `resolveModuleCalls` 优先检查 `moduleFns[fnName]`（`.no` 文件中定义的函数）
2. 如果是 `.no` 文件中的函数 → 改写为直接函数调用 `Identifier{fnName}`
3. 如果不是 → 保持 DotExpression，由 codegen 查 builtin

例如：`fs.read-str` 在 `fs.no` 中有实际定义（`read-str = (p str) (content ?str) { ... }`），所以 `fs.read-str()` 会改写为 `read-str()` 直接调用。而 `fs.read-file` 在 `fs.no` 中只有注释，没有实际定义，所以保持 `fs.read-file` 的 DotExpression 形式，由 codegen 查 builtin。

### 验证某个 builtin 是否已注册

```bash
# 在 Go 测试中验证
go test ./src/builtin -run TestFindBuiltinMethod -v

# 或在代码中搜索
grep -r 'MethodName.*"read-file"' src/builtin/
```

## 添加新 Builtin 的步骤

1. 在 `src/builtin/` 下对应的 Go 文件中添加注册代码：
   ```go
   BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
       ReceiverType: ReceiverGlobal,
       MethodName:   "my-builtin",
       Params:       []parser.Type{parser.TypeStr},
       Return:       []parser.Type{parser.TypeBool},
       Doc:          "My builtin function",
       ForwardFunc:  "my-builtin",  // 或 CLibCall / LLVMIntrinsic
   })
   ```

2. 在对应的标准库 `.no` 文件中添加注释文档：
   ```no
   ; build-in (ForwardFunc: my-builtin)
   ; my-builtin: 我的内建函数
   ; p: 参数说明
   ; 成功返回 true
   ; my-builtin = (p str) (ok bool) {  // LLVM: ...
   ; }
   ```

3. 在 MIR 后端实现 lowering：`src/mir/forward_call.go` 的 `forwardCSpecs`（C 调用规格）加一项，
   或在 `src/mir/builtin_call.go` 写专用 emitter（`emitBuiltin*`）。
   ⚠️ **只做第 1 步不做第 3 步 = 半条命陷阱**，见下一节。legacy 后端 `src/build/llvm/` 已于
   c7febdb（2026-09-16）整体删除，不要再往那里加东西。

4. 在标准库 skill 文档（`nolang-std/SKILL.md`）中添加 API 说明。

5. 运行 `no vet src/std` 确保无错误。

## ⚠️ 半条命陷阱：只注册、无 lowering

`src/builtin/` 的注册是**名字解析**的唯一来源；能不能真正生成 IR，取决于 MIR 有没有对应的
lowering。两者脱钩时（注册在、lowering 没了）症状极坏：

```
$ ./bin/no vet x.no     # 0 error(s), 0 warning(s), 292 hint(s)   ← 看起来干净
$ ./bin/no run x.no     # Error: ... EmitLLVM: unsupported builtin db-open
```

机制：`lookupBuiltin`（`src/mir/builtins.go:62`）按名字命中 `BuiltinMethodList` ⇒ MIR 认定它是
内建、走内置 lowering ⇒ 没有 spec ⇒ 硬错。**lint/CI 给不出任何信号。**

已发生的实例：`db-*`（15 个）与 `ffi-cstr-*`（3 个）。二者由 cb24869（2026-07-03）注册，
lowering 写在 legacy `src/build/llvm/call_stdlib.go` 的 `callDatabase`；该目录随 c7febdb
（2026-09-16）整体删除后**从未移植到 MIR**（`forwardCSpecs` 无对应项）。2026-09-21 已把这两组
注册连同 `src/std/database/sql.no` 的 `#{buildin}` 桩一并删除并 `make gen` 重生 stdsig，
调用方现在得到干净的 `'db-open' is not defined`。

规则：

- 新增 builtin **必须**同时有 MIR lowering，否则不要注册；
- 判断某个 builtin 是否「活的」：`grep -rE '<ForwardFunc>' src/mir/`，空 = 死；
- 纯 FFI 封装（SQLite/MySQL 之类的 C API）**不应该做成 builtin** —— 在驱动里用 `#{c}` 直呼，
  见 `example/sqlite-driver`、`example/mysql-driver`；
- 删 builtin 注册时，同步删 `src/std` 里对应的 `#{buildin}` 桩，再 `make gen` 重生 stdsig
  （否则烘焙表里仍留着这些名字）。

## 常见问题排查

### "xxx is not defined" 错误

1. 检查 builtin 是否在 `src/builtin/` 中注册（`grep -r 'MethodName.*"xxx"' src/builtin/`）
2. 检查调用时是否带了正确的模块前缀（`fs.read-file` 而非裸 `read-file`，除非在同模块内）
3. 检查 `resolveModuleCalls` 是否误改写了 DotExpression

### Builtin 在 `.no` 文件中只有注释

这是**正确的设计**。Builtin 函数不需要 `.no` 实现，注释仅作为文档。`fs.no` 中的注释告诉开发者这些函数存在，但实际由编译器生成代码。

### 跨模块调用 builtin

在标准库的 `.no` 文件中调用其他模块的 builtin 时，使用模块前缀：
- `fs.read-file(path)` — 从 `process.no` 调用
- `fs.write(fd, data, n)` — 从 `io.no` 调用
- `os.get-errno()` — 从 `fs.no` 调用

在同模块内可以直接用裸名调用（如 `fs.no` 内部直接 `read-file(p)`）。

## See Also

- [nolang-std](file://../nolang-std/SKILL.md) — 标准库 API 参考（含所有 builtin 函数签名）
- [nolang-syntax](file://../nolang-syntax/SKILL.md) — 语言语法参考
- [nolang-memory](file://../nolang-memory/SKILL.md) — 内存设计与所有权模型
- `src/builtin/` — builtin 注册源码
- `src/checker/funcargs.go` — `lookupReturnCount` 函数（builtin 查找入口）
- `src/checker/checker.go` — `resolveModuleCallsInExpr` 函数（模块前缀改写）
