---
sidebar_position: 1
---

# Nolang 简介

Nolang 是一门实验性的系统编程语言，采用引用传递模型，编译到 Go、LLVM IR 和原生 Nolang 三个后端。

## 核心特性

- **引用传递**：所有函数参数均为引用，函数通过修改参数来返回结果
- **无 GC**：无堆内存分配，所有变量在栈上分配
- **三个后端**：Go 源码、LLVM IR、Nolang 自托管
- **方法重载**：通过单态化（编译期名称修饰）支持
- **接口系统**：支持接口声明、默认实现、多接口实现

## 快速开始

```nolang
// 你好，世界！
println('Hello, Nolang!')

// 变量声明
x = 42
name = 'Nolang'

// 函数定义（通过参数返回结果）
add(a i64, b i64, result i64) {
    result = a + b
}

// 结构体
user {
    name str
    age i64
}

u = user { name: 'Alice', age: 30 }

// 方法
user.greet() {
    println('Hello, ' + self.name)
}

u.greet()
```
