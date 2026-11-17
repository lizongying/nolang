# Syntax — 语言参考索引

Nolang 是**无 GC、内存安全、语法极简**的系统语言：资源靠 **deferred move + 作用域退出自动销毁**，
禁止手动 `free`；关键字极少，符号重载取代大量语法糖。

正文按"你要改什么"切成 7 个视图，**不要整目录读完**——按下面的表挑一档。

## 视图索引

| 视图 | 覆盖内容 | 何时读 |
|---|---|---|
| [`10-data-types.md`](syntax/10-data-types.md) | 基础/容器类型、类型别名与 union、newtype 语义、变量、正则字面量、注释、命名规则、文档注释约定、代码风格、类型转换、整型赋值检查、`?T` 内联阈值、字符串语义（字面量/运算符/索引/切片） | 写任何 `.no` 之前；类型、布局或格式化有疑问 |
| [`20-functions-control.md`](syntax/20-functions-control.md) | 函数与方法、union 类型上的方法、控制流、`break`/`skip`/early return、占位语句 `...`、多赋值 | 写函数体、循环、提前返回 |
| [`30-match-branch.md`](syntax/30-match-branch.md) | match（`x: { ... }`）、`it` 隐式绑定与可证唯一性、if/else 短路链 | 解包 `?t` / 枚举、写分支 |
| [`40-structs-enums.md`](syntax/40-structs-enums.md) | struct 与方法、字段 inline tag、enum 与内存布局、方法约定、slice（视图非新类型）、索引与安全索引、标准库 struct 模式、字段方法调用、接口、泛型 | 定义数据结构或方法 |
| [`50-modules-imports.md`](syntax/50-modules-imports.md) | `#` / `use` 导入系统、模块前缀规则、跨模块类型引用、导出 | 跨文件、跨包、拆分模块 |
| [`60-annotations-ffi.md`](syntax/60-annotations-ffi.md) | 特殊符号与运算符、FFI（`#{c}`）、注解系统（`#{...}`、`#{embed}`、`#{range}`、平台注解）、`#{overflow}` | 调 C、写或改注解、处理溢出 |
| [`70-runtime-async.md`](syntax/70-runtime-async.md) | `awy { ... }` 协程组 + 组内 `co`、协程取消、JS 后端（`--js` / `--browser`） | 并发、协程取消、JS target |
| [`m93-cli.md`](m93-cli.md) | `no` 子命令、workspace 与包布局、`package.noml` 编译配置、依赖管理 | 用命令行、配工程、发版前的工程结构 |

## 加载判据

- **只改 Go 代码**（`src/**/*.go`）：不加载本模块，直接 `m30-build` / `m40-verify`。
- **只改 std 的 API 面**：先 `m20-std` 定位模块，再按需回本模块查语法。
- **语法语义 vs std API 的分界**：语言的**语义**（类型规则、运算符、注解、布局）在 `syntax/`；
  **某个 std 模块有哪些函数/签名**在 `std/`。两者交叉处只保留一处正文，另一处给指针。

## 语言速查（原文 Introduction）

## Introduction

Nolang is an experimental systems programming language: memory-safe with no GC, semantically intuitive, and minimally syntactic. It adopts a read-only-input / writable-output parameter model and a safe scope model to achieve absolute memory safety.

### Core Features

- **Memory-safe, no GC**: No garbage collector; automatic, safe memory management. Through the safe scope model, memory is automatically freed when leaving scope — no dangling pointers or memory leaks. Heap allocation is batched up-front, and a single batch free runs when the scope exits.
- **Semantically intuitive**: Respects developer intent; no pointers, ownership, or lifetimes as hidden mental overhead.
- **Minimal syntax**: Fewer keywords, simpler syntax.
- **Read-only inputs, writable outputs**: Input parameters are read-only (scalars pass by value, composites pass by read-only reference); outputs are named writable parameters that the function modifies in place.
- **Performance-first**: Small strings require no heap allocation; variables can be allocated once and freed once.
- **Method overloading**: Achieves high performance through monomorphization.
- **Interfaces**: Supports interface function declarations, default function implementations, and multiple interface inheritance.
- **Generics**: Supports type and numeric generics.
- **Match**: Unique match design, simpler to use.

### Quick Start

```no
; Hello, World!
; No main entry needed
print('Hello, Nolang!')

; Variable declaration
i64 i64

; Function definition
add = (a i64, b i64) (result i64) {
    result = a + b
}

; Standard library methods can be called directly
c = math.max(a, b)

; Struct
user {
    name str
    age i64
}

u = user {
    name: 'Alice'
    age: 30
}

; Method
user.greet = () {
    print('Hello, ' - .name)
}

u.greet()
```

