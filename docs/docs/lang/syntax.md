---
sidebar_position: 2
---

# 语言参考

## 注释

```nolang
// 单行注释
```

## 变量声明

```nolang
x = 42              // 隐式类型推断 → i64
name = 'Nolang'     // 字符串
flag = true         // 布尔
pi = 3.14           // 浮点

a u64 = 10           // 显式类型标注
d byte = 100
e char = 中          // 裸字符（不用引号）

arr [3] = [1, 2, 3]          // 定长数组
vec = [4, 5, 6]             // 动态切片
typed []u8 = [1, 2, 3]        // 指定类型切片
typed [3]u16 = [1, 2, 3]      // 指定类型数组
```

## 函数定义

函数通过**修改入参**来传递结果，`return` 仅用于提前终止。

```nolang
add(a i64, b i64, result i64) {
    result = a + b             // 通过参数返回
    return                     // 提前终止（可选）
}

// 调用
add(1, 2, sum)                 // sum == 3
```

## 流程控制

```nolang
// if/elif/else
if x > 5 {
    a = 1
} elif x < 0 {
    b = 2
} else {
    c = 0
}

// 作为表达式
max = if x > y { x } else { y }

// for 循环
for i < 10 {
    println(i)
    i = i + 1
}

// range for
for i in [0..10) {
    println(i)
}

// 命名循环 + break/continue
outer for i in [0..10) {
    inner for j in [0..10) {
        if j == 5 {
            continue outer
        }
        if i == 8 {
            break outer
        }
    }
}

// match
result = match x {
    1: 10
    2: 20
    : 0             // 默认
}
```

## 数组与切片

```nolang
nums [5]u8 = [0, 1, 2, 3, 4]

// 数组/切片操作（返回 vec）
nums[..]    // [0, 1, 2, 3, 4]
nums[1..]   // [1, 2, 3, 4]
nums[..3]   // [0, 1, 2]
nums[1..3]  // [1, 2, 3]
nums[1..3)  // [1, 2]
```

## 结构体

```nolang
user {
    name str
    age i64
}

u = user { name: 'Alice', age: 30 }
u.name = 'Bob'
```

## 方法

```nolang
user.greet() {
    println('Hello, ' + self.name)
}
```

## 接口

```nolang
// 接口定义
json {
    to-json()
}

// 结构体实现接口
user json {
    name str
    age i64
}

// 默认实现
json.to-json() {
    return '{...}'
}

// 覆写 + 调用父实现
user.to-json() {
    super.to-json()
}
```

## 枚举

```nolang
color {
    red,
    green,
    blue,
}
// red=0, green=1, blue=2
```

## 自动 enter/leave

实现了 `enter` / `leave` 接口的类型，在作用域进入和离开时自动调用：

```nolang
file enter, leave {
    path str
}

file.enter() { open(self.path) }
file.leave() { close(self) }

read-file() {
    f = file{ path: 'data.txt' }  // 自动 f.enter()
    read(f)                         // 使用 f
}                                   // 自动 f.leave()
```
