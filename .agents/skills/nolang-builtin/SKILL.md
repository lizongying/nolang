---
name: nolang-builtin
description: Nolang 内建函数（builtin）注册与查找机制参考。用于理解 fs.read-file、os.get-env 等内建函数的注册位置、查找流程、模块前缀解析，以及编写或修改 builtin 注册代码、排查 builtin 找不到的问题。涵盖 src/builtin/ 下所有 Go 文件（os.go、fmt.go、math.go、str.go、net.go、process.go、database.go、async.go、bits.go、ffi.go 等）。
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
├── database.go      # db-open, db-exec, db-query, etc.
├── async.go         # async-cancel, async-cancelled, async-yield
├── bits.go          # rotate-left, rotate-right, load-le-u16/u32/u64
└── ffi.go           # ffi-cstr-at, ffi-cstr-at-int, ffi-cstr-at-float
```

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

4. build/llvm/expr.go codegen:
   - 遇到 DotExpression { Receiver: Identifier, Property: string }
   - Receiver 是模块名 → 用 Property 裸名查 FindBuiltinMethod
   - 找到 → 按 ForwardFunc/CLibCall/LLVMIntrinsic 生成 IR
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

3. 在 `src/build/llvm/expr.go` 或 `transpiler.go` 中实现 codegen 逻辑（ForwardFunc 的 case 分支）。

4. 在标准库 skill 文档（`nolang-std/SKILL.md`）中添加 API 说明。

5. 运行 `no vet src/std` 确保无错误。

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
