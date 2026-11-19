---
sidebar_position: 3.1
---

## 基础型别

### types — 型别定义

Nolang 型别到 LLVM 的对映关系：

| Nolang           | LLVM                                               |
| ---------------- | -------------------------------------------------- |
| `bool`           | `i1`                                               |
| `byte`           | `i8`                                               |
| `char`           | `i32`                                              |
| `i8/i16/i32/i64/i128` | `i8/i16/i32/i64/i128`                             |
| `u8/u16/u32/u64/u128` | `i8/i16/i32/i64/i128`                          |
| `f32`            | `float`                                            |
| `f64`            | `double`                                           |
| `str`            | `%str-long = { i32, i32, i8* }`（u32 len, u32 cap, data；64 位元下 16 字节，堆分配） |
| `[]t`            | `%vec = { i32, i32, i64 }`（u32 len, u32 cap, ptrtoint data；64 位元下 16 字节，堆分配） |
| `[n]t`           | `[n x t]`（裸聚合，元素内联；**无 len / cap 描述符**） |
| `txt`            | `{ [255 x i8], i8 }`（固定 256 字节） |

> **`str` 与 `[]t` 的 len / cap 内部都是 `u32`，对外一律 `i64`。** 描述符读取时 `zext i32 -> i64`、
> 写入时 `trunc i64 -> i32`；`s.len-bytes()`、`s.len()`、`s.cap()` 与私有 `_len` / `_cap` / `_set-len` /
> `_set-cap` 的签名都维持 `i64`，`v.len()` 亦然。详见 [字串：内部布局与对外 API](../lang/str.md)
> 与 [记忆体模型：型别布局](../lang/memory.md#型别布局)。
> `%vec` 的 `data` 栏位刻意保持 `i64`（`ptrtoint` 编码），与 `%str-long` 的 `i8*` 不同，因此两者
> **布局不再相同**，不可再互相 `getelementptr` 当成同一种描述符。
> `txt` 没有 `cap` 栏位，长度是 1 个 `i8`（0-255），**不随 `str` 改成 u32**。

**复合型别：**

- **变长数组 `[]t`**：底层 `%vec = { u32 len, u32 cap, i64 data }`（16 字节；`cap == 0` 表示借用视图，不拥有缓冲）
- **定长数组 `[n]t`**：LLVM 固定大小阵列 `[n x t]`，元素内联、无运行时描述符；`.len` 是编译期常量 `n`，没有 `cap`
- **字串 `str`**：堆分配的位元组序列，描述符 `{ u32 len, u32 cap, i8* data }`（64 位元下 16 字节）；支援 `s[i]`、`s[i..j]`、`s + t`。长度／容量对外以 `i64` 呈现（`s.len-bytes()` / `s.cap()`），内部以 `u32` 存放
- **固定字串 `txt`**：固定 256 字节结构 `{ [255]byte data, byte len }`，前 255 字节存储数据，最后 1 字节存储**字节**长度（0-255），无需堆分配，必须类型标注（`t txt = 'abc'`）；`t.len()` 回传 code point 数量（与 `str` 对齐，等价 `t.count()`），`t.len-bytes()` 回传字节数量；索引 `t[i]` **读取**以**码点**为单位（读回该码点的 `char`，越界回传 `err`，`code.index-out`）；**写入 `t[i] = c` 自 2026-11-12 起全域禁用**（编译错误 `idxwrite`）—— 它会把 `c` 重新 UTF-8 编码并后移尾部字节，长度随写入隐式改变（`i` 等于码点数时为追加、超过 255 的部分静默丢弃）；要变长请用 `t.push(c char)` / `t.push-str(s)` / `t.push-txt(u)`，要就地改值请用 place-match `t[i]: { ok -> it = c }`；字节级读取用 `t.byte(i)`（回传 `?byte`，越界回传 nil）；要按字节拼装就直接攒 `[]byte`，再 `txt.from-bytes(b)` 一次拷入（超过 255 字节截断，长度按实际字节数设定）。字节级写入器 `txt._set-byte` 自 2026-11-11 起是 **std 私有**，用户代码不可调用。`str` 的 `s[i] = v` 已于 2026-11-11 **全域禁用**（编译错误 `idxwrite`，`with-cap` 基底也不再豁免），`str` 侧只剩 `s.fill(val byte)`、place-match `s[i]: { ok -> it = v }` 与增长用的 `s.push(c)` / `s.push-str(s)`
- **列举/Union**：`option` tagged enum（`ok t` / `nil` / `err str`）
- **结构体**：必须多行定义，栏位不加逗号
- **配列**：底层 linked-hash-map
- **迭代器**：`for iter.next() {}`（介面方法 `next() (ok bool)`）

### option — 选项型别

`option<t>` 标签列举（tag=0=val, 1=nil, 2=err）：

```no
#{fmt-skip}
x ?t                ; 宣告 option<t>
x = 42              ; 设为有值
x = nil             ; 设为空
x = err('msg')      ; 设为错误

; match
x: {
    val -> f(it)
    nil ->
    err -> g(it.msg())
}  
```

**风格指引：** 函数可能失败或返回空值时，应使用 `?t` option 而非 `(val, ok bool)`。`?t` 有三种状态：`ok`（有值）、`nil`（空值/正常缺失）、`err`（错误）。正常值会隐性绑定。例如 `pop()` 返回 `?i64`（`nil` = 空）、`read-line()` 返回 `?str`（`nil` = EOF，`err` = 错误）、`lookup()` 返回 `?str`（`nil` = 未找到）。详见语法文档。

---
