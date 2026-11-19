---
sidebar_position: 1
---

# Nolang 简介

Nolang 是一门实验性的系统编程语言：内存安全无 GC、语义符合直觉、语法极简。

## 核心特性

- **内存安全**：不依赖垃圾回收，自动内存管理
- **性能强劲**：编译期优化，无 GC
- **语法极简**：更少关键字，没有指针、所有权、生命周期等隐式心智负担，语法更简单
- **意图驱动**: 充分尊重开发者意图，语义符合人类直觉
- **永不崩溃**：消除隐式崩溃路径

## 快速开始

```no
; 你好，世界！
; 不需要main入口
print('Hello, Nolang!')

; 变量声明
count i64

; 函数定义
add = (a i64, b i64) (result i64) {
    result = a + b
}

; 标准库方法，可以直接调用
c = math.max(a, b)

; 结构体
user {
    name str
    age i64
}

; 方法
user.greet = () {
    print('Hello, {.name}')
}

u = user {
    name: 'Alice'
    age: 30
}

u.greet()
```

## 应用案例

- [notools](https://github.com/lizongying/notools) — 使用 Nolang 语言实现的 Unix 常用命令行工具集，涵盖 cat、ls、grep、wc、head、tail 等经典工具，展示了 Nolang 在系统编程领域的实际应用能力。
