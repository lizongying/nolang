# 当我们谈论内存安全时，我们在谈论什么——Nolang 与 Rust 的两条路径

## 引子：安全 ≠ 借用检查器

2015 年 Rust 1.0 发布以来，"内存安全"几乎被等价于"所有权 + 借用检查 + 生命周期"。这套体系确实在编译期消灭了 data race、use-after-free、double-free，代价是开发者要和编译器进行一场旷日持久的语义博弈——`'a`、`'static`、`Pin<Box<dyn Future + Send>>>`、`Arc<Mutex<RefCell<T>>>`，层层标注把一句简单的"把数据传过去"变成了类型系统的逻辑题。

Nolang 给出了另一个答案：**不要所有权，也不要 GC，靠"传引用 + 安全作用域 + 延迟 move"在语言层面直接消灭悬挂指针与 data race**。本文不做选边站队，只把两条路径放在同一张桌子上，逐项对照。

---

## 一、内存模型：所有权 vs 引用语义 + 延迟 move

### Rust：所有权是一切的底座

Rust 的核心契约有三条：

1. 每个值有且只有一个 owner
2. owner 离开作用域时值被 drop
3. 同一时刻要么有多个不可变借用，要么有一个可变借用

这三条规则让编译器能在编译期精确计算每块内存的释放点，但代价是：**所有数据流都必须显式表达"谁拥有、谁借用、借多久"**。一个简单的链表实现要靠 `Rc<RefCell<Node>>` 或 `unsafe` 才能写出来，这不是开发者能力问题，是模型的表达力边界。

### Nolang：传引用 + 安全作用域 + 延迟 move

Nolang 的契约有两条：

> 1. 所有函数参数都是引用，函数通过修改参数返回结果；变量进入作用域分配，离开作用域释放。
> 2. **延迟 move**：函数结束时，局部变量 move 到返回值（输出参数），避免无谓拷贝，又保证引用不逃逸。

```nolang
add = (a i64, b i64) (result i64) {
    result = a + b
}
```

没有 `&`、没有 `&mut`、没有 `Clone`、没有 `Copy` trait。函数签名里第二个括号 `(result i64)` 是**可写输出参数**，调用方传入的变量会被原地写入。底层机制仍是引用传递，但对开发者暴露的是一个"过程式 + 输出参数"的纯函数模型。

**延迟 move 是这套模型的关键补丁**。纯引用传递的理论问题是：函数内部构造的大对象（解析后的 AST、组装好的响应体、计算出的 bigint）如何安全地交还调用方？拷贝则性能不可接受；直接返回引用则引用逃逸出作用域，破坏安全保证。Nolang 的解法是**在函数返回的瞬间，把局部变量的所有权 move 到输出参数**——这一刻是确定性的、由编译器插入的，开发者无感。既拿到了 move 语义的零拷贝性能，又没有破坏"引用不逃逸作用域"的契约。

这套模型声称做到"绝对内存安全，无 GC"，本质是把 Rust 中"借用规则"替换成"作用域规则 + 延迟 move"——既然所有引用都活在自己的作用域内、且唯一的外带通道是函数结束时的 move，那引用就永远不会逃逸成悬挂指针。代价也明显：**不支持自引用结构、不支持图结构原生表达**，因为引用不能越过作用域逃逸。这和 Rust 的 `Box` + `unsafe` 是不同方向的妥协。

---

## 二、错误处理：`Result<T, E>` vs `?t` 三态

### Rust

```rust
fn read_file(path: &str) -> Result<String, io::Error> {
    let mut f = File::open(path)?;
    let mut s = String::new();
    f.read_to_string(&mut s)?;
    Ok(s)
}
```

`Result` 是代数类型，`?` 是语法糖。强大、显式、但啰嗦。每个可能失败的调用都要 `?`，每个错误类型都要 `From` impl 才能跨层传播。

### Nolang

```nolang
file.read = () (data ?str) {
    .fd < 0 -> {
        data = err('file not open')
        return
    }
    // ... read data
    data = buf
}
```

`?t` 是一个带 tag 的枚举，三态：`ok`（有值）、`nil`（空/正常缺席）、`err`（错误）。配合 match 解构：

```nolang
val = s.pop()
val: {
    nil -> print('empty')
    err -> print(it)
    -> print(it)              // it = the value
}
```

这里的设计哲学区别值得注意：**Rust 把"没有值"和"出错了"合并成 `Err`，Nolang 把它们拆开**。`nil` 表示"正常的空"（栈空、键不存在、EOF），`err` 表示"异常的空"（I/O 失败、解析错误）。对网络编程和容器操作来说，这种区分能省掉很多 `Option<Option<T>>` 的双层嵌套。

代价是：`?t` 没有 `?` 操作符那样的传播糖，每个调用点要么 match，要么显式 return。

---

## 三、并发：未开放多线程，data race 在语言层不存在

### Rust

Rust 的并发安全靠 `Send` 和 `Sync` trait 在编译期保证："无 data race"是语言级承诺。代价是 `async fn` 的 Future 默认不 `Send`，跨线程要 `Arc<Mutex<T>>`，async 生态分裂（tokio vs async-std vs smol），且 `Pin` 语义让自引用 Future 的处理成为资深开发者的专利。

### Nolang

```nolang
compute-async = (n i64) (r i64) {
    r = n * 2
}

h1 = run compute-async(10)
h2 = run compute-async(20)
r1 = awy h1        // r1 = 20
r2 = awy h2        // r2 = 40
```

`run` 派发 Future 到后台协程，`awy` 等待完成。async 函数名必须 `-async`