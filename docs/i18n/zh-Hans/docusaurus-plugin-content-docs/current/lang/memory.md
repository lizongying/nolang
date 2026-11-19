---
sidebar_position: 6
---

# 记忆体管理

Nolang 是**无 GC** 语言，记忆体安全由编译器自动插入 `free` 保证。本文描述已实现的记忆体设计与所有权语义。

## 核心原则

### 单一所有权
每个堆 `data` 缓冲区**只有一个所有者**。所有权可透过 move 转移，转移后原所有者放弃 free 责任。局部变数间的 `=` 则透过深层 clone 使两个变数各自独立拥有 data。

### 赋值语义
`b = a` 根据上下文选择以下语义之一：

| 语义 | 触发条件 | 行为 |
|------|---------|------|
| **值拷贝** | 基本型别（i64/f64/bool 等）且不满足 can_slot_rebind | 直接拷贝数值，无堆数据 |
| **栈槽重绑定** | 局部变数间 `b = a`，a 为栈类型（i64/u64/i128/u128/txt）且满足 can_slot_rebind | `g.varAlias[b] = a`，b 与 a 共享同一栈槽，0 拷贝（优于值拷贝）；否则降级为值拷贝 |
| **深层 clone** | 局部变数间 `b = a`，a 为堆拥有型别（vec/arr/str/可克隆结构体/**`?str`、`?[]T`**）且赋值后**仍被引用** | malloc 新 data + memcpy + 递回 clone 元素；a 和 b 各自独立拥有 data，函数结束各自 free |
| **move（源已死）** | 局部变数间 `b = a`，a 为堆拥有型别但赋值后**不再被引用** | 零拷贝转移所有权；源跳过 free。与深层 clone **可观测等价**，只省掉一次拷贝 |
| **move** | 输出参数 `out = x` | 浅拷贝结构体 + 标记源为 moved；源跳过 free |
| **深层 clone** | `vec.push(x)`（x 为堆拥有型别） | malloc 新 data + memcpy + 递回 clone 元素；源仍拥有独立 data，函数结束各自 free |

> **值对值赋值一律深拷贝，禁止零拷贝别名。** `b = a`（RHS 是整个具名变数）只有两种降低——
> **源仍活 ⇒ 深层 clone**、**源已死 ⇒ move**；**不允许**「两个名字共用一个 buffer、只留一个 free」
> （那种降低曾存在于只读前向优化，已移除）。**元素／栏位读取**（`b = a[i]`、`b = a.f`）维持
> **视图别名**语意，不受此限。

## 栈类型 move（栈槽重绑定 / slot-rebind）

`i64/u64/i128/u128/txt` 虽然都是**栈类型**（值直接存在 alloca 栈槽，无堆 data），但 `b = a` 在语义上仍预设走「值拷贝」（把 a 的栈值 memcpy 到 b 的栈槽）。为减少不必要的拷贝，编译器对满足 **can_slot_rebind** 约束的栈类型 `b = a` 执行**栈槽重绑定**：令 `g.varAlias[b] = a`，使 b 与 a 共享同一栈槽（0 拷贝，优于值拷贝）。

### can_slot_rebind 约束（保守正确）
重绑定后 b 与 a 指向同一栈槽，因此 a 后续任何对 b 的读都会读到「a 的存储」。为避免语义错误，重绑定仅在源 a **后续「完全无引用（读或写）」**时允许；否则**降级为完整值拷贝**（语义不变，仅多一次 memcpy）。

- 安全性由 `computeSlotRebindSafety`（主函数）/`computeMoveEligibility`（用户函数）透过 `stmtContainsVarRefAny` / `exprContainsVarRefAny` 静态求解：扫描 move 之后的所有语句（含分支/回圈回边），若源变数仍被任何方式引用则 `slotRebindSafe[stmt] = false` → 走拷贝。
- 引用扫描必须穷举**所有能引用变数的 AST 节点**，否则未覆盖的写引用会导致不安全的重绑定（别名槽被污染 → 错误输出甚至无限回圈）。已覆盖：语句层 `LetStatement` / `ExpressionStatement` / `ForStatement`（含 `Init`/`Update`/`Condition`/`CountExpr`/`Body` 与 **`IterRange` 迭代集合**）/ `ReturnStatement` / `MultiAssignStatement` / **`UnwrapAssignStatement`（`?=` 解包赋值）** / **`BlockStatement`（裸区块）**；表达式层 `Identifier` / `AssignExpression` / `Infix` / `Prefix` / `Call` / `Dot` / `Index` / `IfExpression`（含分支体）/ `Slice` / `Conditional` / `Grouped` / **`CoExpression`（`co <call>` 协程 spawn，防御性；整函数禁用仍由 `curHasUnsafeConstruct` 负责）** / **`CastExpression`** / **`RangeExpression`**。任何新增的变数引用构造都必须同步加入这两个扫描函数。
- `match` 已 desugar 为 `IfExpression`，其分支体内的引用由上述分析遍历覆盖，故 **match 不触发禁用**（经 `tests/match.no` / `tests/option.no` 验证与基线一致）。

### 禁用场景（一律降级为值拷贝）
| 禁用条件 | 原因 |
|---------|------|
| 目标为输出参数 / 全域变数 / 堆类型变数 | 输出参数由指标传递、全域跨函数可见、堆类型需深层 free，重绑定破坏所有权 |
| 源为参数 / 全域变数 | 参数按引用传递，重绑定会破坏呼叫方栈帧 |
| **stdlib 函数**（`curIsStdLib`，由 `SetStdModules` 判定） | stdlib 内部 alias 绑定（如 fmt 的 `it` 别名到局部 `n`）与引用分析难以完全建模 |
| **用户函数体含闭包（`FunctionLiteral`）或协程 spawn（`RunExpression`）**（`curHasUnsafeConstruct`，由 `bodyHasUnsafeConstruct` 递归扫描） | 闭包捕获变数在「独立函数上下文」求值，协程跨线程执行，二者变数生命周期超出当前函数的 `g.varAlias` 单函数别名作用域，重绑定会破坏共享栈槽 |

> 设计取舍：闭包/协程采「整函数禁用」而非「逐变数精算」，因为单函数别名分析本质上无法建模跨函数/跨线程的存储共享。整函数禁用只损失优化（降级为拷贝），绝不引入错误，符合「保守正确」原则。

### 别名失效（alias invalidation）
- 目标被重新赋值时，先 `delete(g.varAlias, name)` 清掉残留旧别名，否则通用赋值路径 `varAddr(name)` 仍指向旧源栈槽，污染旧源。
- 重绑定执行传递性解析：沿 `g.varAlias` 链找到最终源栈槽（`a → c → …`），避免多跳别名错位；并同步 `g.varTypes[name]` 使后续型别查询一致。

### 与堆类型 move 的正交性
栈槽重绑定只作用在栈类型 `b = a` 的**值拷贝**语义上，与堆拥有型别的「深层 clone / move（输出参数）」完全正交；堆类型路径不受 `slotRebindSafe` 影响。

### 测试参考
- `tests/slot-rebind.no`：i64/u64 重绑定、i128/u128 经 `==` 比较、txt 经 `.len-bytes()`、降级拷贝（`da` 复用 → `db`/`dc` 拷贝）、重赋值后（`ra`=10）、参数 move（`pm_fn` 源为参数 → 拷贝）、重绑定后重赋值（`rr_fn` → `rr`=99）、消费（`consume(m)` → `cm`=84）。期望输出 `42 7 1 1 17 5 5 10 100 99 84`。
- `tests/slot-rebind-unsafe.no`：协程（`awy { co ... }`）capture 栈变数，验证 `curHasUnsafeConstruct` 禁用重绑定后输出与基线一致。

### 编译器插入 free
- 函数结束时：释放所有未 moved 的局部堆变数
- 重新赋值前：释放旧值
- 结构体栏位：递回释放含堆数据的栏位

## 型别布局

| Nolang 型别 | 记忆体结构 | 栏位 | 分配策略 |
|------------|-----------|------|---------|
| `[]T`（切片） | **16 字节** | **u32 len, u32 cap, i64 data（ptrtoint）** | malloc（堆） |
| `[N]T`（固定阵列） | N × sizeof(T) | 元素内联，**无 len/cap 描述符** | alloca（栈）或 malloc |
| `str`（长字串） | **16 字节** | **u32 len, u32 cap, i8\* data** | malloc（堆） |
| `txt`（固定字串） | 256 字节 | `[255 x i8]` data, i8 len | alloca（栈） |
| `?T`（可空） | **按载荷定型**：`{ i8 tag, [K x i64] }` | 1 位元组标签 + 7 位元组填充 + 载荷（8 的倍数） | alloca（栈）；载荷超门槛时改存堆指标 |
| 标签列举 | `{ iN tag, [K x i64] }` | tag 宽度随变体数（`i8` 起），载荷是最宽变体的 union | alloca（栈） |
| 结构体 | 各栏位总和 | 各栏位 | alloca（栈） |

**`?T` 的大小是算出来的，不是固定的**：`?i64` = 16 字节、`?str` = 24、`?txt` = 264，超过
`option-inline-threshold`（预设 264 = 1 + 7 + 256，最小 16）的载荷改为堆装箱、整个 option
回到 16 字节。每个载荷型别各有自己的 `%option_<key>` 型别（`src/mir/layout.go`），因此
「型别相同 ⇔ 布局相同」。`err` 的载荷恒装箱，不占用载荷预算。详见
`docs/docs/lang/syntax.md`〈可空类型(option)・内存布局〉。

**标签列举**的 tag 宽度随变体个数变化（`<=256` → `i8`、`<=65536` → `i16`、`<2^32` → `i32`、
否则 `i64`），载荷 union 的宽度是「最宽的那一个变体」而不是全部相加，栏位按自然对齐摆放、
以**位元组偏移**寻址。所有变体皆无载荷时布局退化成 `{ iN }`（1 字节起）。

**`str` 的 u32 header**：`%str-long = type { i32, i32, i8* }`（64 位元平台 16 字节）。
`len` / `cap` 内部是 `u32`（LLVM `i32`），**所有对外 API 仍是 `i64`**：读取时 `zext i32 -> i64`，
写入时 `trunc i64 -> i32`。`s.len-bytes()`、`s.len()`、`s.cap()`、`s._len()` / `s._cap()` /
`s._set-len()` / `s._set-cap()`、`len(s)` 的签名都没有改变。这层边界由 codegen 的
`strLenI64` / `strCapI64` / `strHeaderLoadI64` / `strHeaderStoreI64` / `strLongFromParts` 统一处理：
**不可**对 descriptor 的第 0 / 1 栏位直接发 `load i64` / `store i64`，在 opaque pointer 下
LLVM verifier 不会拦截，但会一次读／写 8 字节而把相邻的 header 栏位一起改掉（实测
`s.len-bytes()` 曾回传 `len | cap<<32`）。

连带的尺寸常数也必须跟著 16 字节：`[]str` 的 element stride、`os.args()` 的 argv backing
store、以及全域字串常量的 `{ i32 len, i32 cap, i8* data }` 字面量。`?str` 的载荷正好是 16
字节（2 个 i64 word，第一个 word 打包 len 与 cap），所以 `?str` 是 24 字节的内联 option；
标签列举里 `str` 变体的载荷同样是 2 个 word。`txt` 维持原样，不随 `str` 改成 u32。

**`[]T` 的 u32 header**：`%vec = type { i32, i32, i64 }`（16 字节）。`len` / `cap` 与 `str`
同构，内部是 `u32`、对外 API 仍是 `i64`；差异在 **data 栏位仍是 `i64`**（`ptrtoint` 编码的
指标，而非 `i8*`），以保持 `inttoptr` / `ptrtoint` 的既有呼叫点不变。这层边界由
`vecLenI64` / `vecCapI64` / `vecHeaderLoadI64` / `vecHeaderStoreI64` / `vecFromParts`
（以及通用的 `hdrFieldI64` / `hdrTrunc` / `hdrWiden`）统一处理，`emitLenCap` / `emitGetField` /
`emitSetField` / `ensureVecBuffer` 全部走它们。与 `str` 相同的禁令也适用：**不可**对
`%vec` 的第 0 / 1 栏位直接发 `load i64` / `store i64`。

`cap == 0` 对 `%vec` 是**借用视图**标记（`s.bytes()`、`[N]T` → `[]T` 借用、str → `[]byte`
强制转换）：不拥有堆、`@vec_free` / `@vec_retain` 会跳过；`cap > 0` 才拥有 `data` 指向的块。

`[N]T` 没有任何运行时描述符：它在 LLVM 层就是裸聚合 `[N x T]`，`.len` 由编译器直接换成
编译期常量 `N`，没有 `cap`。

## 浅层 free 与深层 free

### 浅层 free
只释放容器的 data 缓冲区，不遍历元素。适用于：
- `%str-long`（字串 data 是字符缓冲区，无嵌套堆拥有元素）
- 元素为基本型别（i64、double 等）的 vec/arr

### 深层 free
先遍历每个元素递回释放其堆数据，再 free 容器的 data 缓冲区。适用于元素为堆拥有型别的 vec/arr：
- `[]str`（元素为 %str-long）
- `[][]i64`（元素为 %vec）
- `[]MyType`（元素为用户结构体，递回释放栏位）

### NULL 检查
所有 free 前都检查 `icmp eq i8* %ptr, null`，避免 free(NULL) 或 free 未初始化指标。

## 所有权转移（move）

### 单返回值 move
```no
get-slice = () (out []i64) {
    local = [1, 2, 3]
    out = local; local 标记为 moved，函数结束不 free；out 由呼叫者管理
}

v = get-slice(); v 拥有 data，函数结束时 free
```

### 多返回值 move（按参数位置顺序）
```no
get-pair = () (a []i64, b []i64) {
    x = [1, 2]
    y = [3, 4]
    a = x; 第一个输出参数，x 标记为 moved
    b = y; 第二个输出参数，y 标记为 moved
}

a, b = get-pair(); a 拥有 x 的 data，b 拥有 y 的 data
```

**处理顺序**：按输出参数在函数签名的**宣告顺序**逐个处理。每个 `out = src` 赋值独立标记源变数为 moved。

**同一源变数多次赋值**：若 `a` 和 `b` 引用同一源变数（如 `a = x; b = x`），编译器透过 **Liveness 预分析**（`moveEligible`）自动决定 clone 或 move：

- `a = x`：x 在后续被 `b = x` 引用 → **深层 clone**，a 独立拥有 data
- `b = x`：x 在后续未再被引用 → **move**，b 接管 x 的 data，x 标记为 moved 跳过 free

这避免了 double-free。规则：**最后一次引用走 move，之前的引用走 clone**。

条件分支场景（如 `if cond { out = x }`）透过运行时位图追踪 moved 状态，见下节。

### vec.push 的深层 clone
```no
inner = [1, 2, 3]
outer.push(inner)
; inner 的 data 被深层 clone 到 outer 新元素位置
; inner 仍拥有独立 data，函数结束时 inner 与 outer 各自 free
```

push 对堆拥有元素型别（`%str-long`/`%vec`/`%arr`/用户结构体）执行深层 clone：malloc 新 data + memcpy + 递回 clone 元素。源变数和外部 vec 拥有各自独立的 data，**不需要 move 标记**，避免 double-free。基本型别元素（i64/f64 等）则直接 store 值。

### 运行时 move 追踪（按堆变数下标索引的位图）

条件分支下的 move 带来一个挑战：编译期无法确定某个 move 是否真的发生。

```no
cond-move = (flag i64) (out []i64) {
    x = [1, 2, 3]
    if flag == 1 {
        out = x; move 仅在 flag==1 时发生
    }
    ; flag==0 时 x 仍拥有 data，函数结束需 free
    ; flag==1 时 x 所有权已转移，函数结束需跳过 free
}
```

Nolang 采用**按堆变数下标索引的位图**解决此问题。编译期为每个局部堆变数分配唯一 `varIdx`，运行时位图的每个 bit 对应一个堆变数（而非输出参数）。

#### 编译器状态

| 栏位 | 类型 | 用途 |
|------|------|------|
| `heapVarIndex` | `map[string]int` | 堆变数名 → `varIdx`（仅局部堆变数） |
| `outBindState` | `[]int` | 每个输出参数当前绑定的堆变数下标（-1=无绑定，-2=不确定） |
| `movedVarBitset` | `[]uint64` | 编译期 moved 位图（无运行时位图时用） |
| `movedBitmapBase` | `string` | 运行时位图变数名前缀（如 `%__mb`，空=未分配） |
| `bitmapCount` | `int` | u64 位图块数（= maxVarIdx/64 + 1） |

#### 下标映射规则

一块 `u64` 存 64 个标记位，堆变数下标 `varIdx`：

- 块号 = `varIdx / 64`
- 块内偏移 = `varIdx % 64`
- 掩码 = `1u64 << 偏移`

编译期直接算常量，运行时无计算开销。多块 `u64` 可支援任意数量堆变数，**无参数/返回值数量上限**。

#### move 赋值处理（覆盖会清旧 bit）

每次把堆变数 move 给输出参数：

1. 若该输出参数之前绑定过别的变数（`outBindState[outIdx] >= 0`），先清除旧变数对应的 bit
2. 再把当前变数对应 bit 置 1
3. 更新该输出参数绑定的变数下标（`outBindState[outIdx] = srcVarIdx`）

#### 函数结尾释放

遍历全部堆变数，平铺独立 `if`：对应 bit 为 0 就 free，bit 为 1 代表所有权移走，跳过释放。

#### 位图按需分配

位图变数仅在**必要时**分配，避免无分支场景的效能开销：

| 场景 | 位图分配 | free 行为 |
|------|---------|----------|
| 无 move | 不分配 | 全部 free |
| move 不在分支（确定性 move） | 不分配 | 编译期 `movedVarBitset` 直接跳过 free |
| move 在分支（条件 move） | 分配 | 运行时位图检查：bit=1 跳过，bit=0 free |

编译器在生成函数体之前预扫描 AST（`detectBranchMoveToOut`），检测是否存在 `IfExpression`/`ForStatement`/`ConditionalExpression` 分支内对输出参数的 move 赋值，仅在此类模式存在时才分配运行时位图变数。位图 `alloca` 在函数体生成之后插入（此时 `nextHeapVarIdx` 已为最终值），写入 entry block。

此机制适用于所有堆类型（`vec`/`str-long`/`arr`/用户结构体）。

## 深层 clone（局部变数间赋值）

```no
a []i64 = [10, 20, 30]
b = a; 深层 clone：malloc 新 data + memcpy + 递回 clone 元素
b[0] = 99
; a[0] == 10（a 不受影响）
; b[0] == 99（b 独立修改）
```

### 深层 clone 流程
1. 释放目标变数的旧值（若已有堆数据）
2. `malloc` 新 data 缓冲区，`memcpy` 源 data 到新 data
3. 递回 clone 每个堆拥有元素：
   - `%str-long` 元素：malloc + memcpy 字串 data
   - 用户结构体元素：memcpy 结构体 + 递回 clone 含堆数据的栏位
4. 将新 data 指标、len、cap 写入目标变数
5. 追踪目标为堆变数（函数结束时 free）

### 可克隆的型别
| 型别 | 可深层 clone | 说明 |
|------|-------------|------|
| `%vec` / `%arr`（元素为基本型别） | ✅ | memcpy data 即可 |
| `%vec` / `%arr`（元素为 %str-long） | ✅ | 逐元素 malloc+memcpy 字串 data |
| `%vec` / `%arr`（元素为可克隆结构体） | ✅ | 逐元素递回 clone 结构体栏位 |
| `%vec` / `%arr`（元素为 %vec / %arr） | ✅ | 透过 `elemElemType` 机制递回 clone 内层容器元素 |
| `%str-long` | ✅ | malloc + memcpy 字串 data |
| 用户结构体（无巢状容器栏位） | ✅ | memcpy 结构体 + 递回 clone 堆栏位 |
| 用户结构体（含巢状容器栏位） | ✅ | memcpy 结构体 + 递回 clone 含巢状容器的栏位（透过 `elemElemType`） |
| `?str` | ✅ | inline payload 的 option：clone 选项本身与其字串 data |
| `?[]T` | ✅ | inline payload 的 option：clone 选项本身与内层切片的 data（逐元素 clone，`?[]str` 等元素拥有堆者递回） |

> **`?str` / `?[]T` 是 inline payload，但拥有堆。** 它们的 payload 直接存在 option 自己的
> `%option_<key>` 里（`?str` 的结构是 `{ i8 tag, [2 x i64] }`，载荷 16 字节放
> `{ u32 len, u32 cap, i8* data }`），但其中的 `str` data／slice backing store 是堆配置
> ⇒ **clone 时要连 payload 一起深拷贝，free 时要连 payload 一起释放**，两侧必须成对。
> `?[]T` 的释放走与 `[]T` 相同的深层 free helper（依元素型别决定是否递回），所以
> 「深层 clone 配深层 free」这条对偶在这里同样成立。
> ⚠️ 一般（非 `?str`/`?[]T`）的 option，payload 是借用或纯值，**不拥有**堆，clone／free 都不碰它。
> 载荷大到超过 `option-inline-threshold` 时 option 改存堆指标，那时 free 要连箱子一起释放
> （由 `Boxed` 决定，见 `src/mir/layout.go`）。

### 与 move 的区别
- **深层 clone**：源和目标各自独立拥有 data，函数结束各自 free
- **move**：源放弃所有权（标记 moved），目标接管 data，源跳过 free

`b = a` 的判断规则：
1. 若 a 是输出参数的源 → move
2. 否则若 a 是堆拥有型别但**赋值后不再被引用** → move（零拷贝，与深层 clone 可观测等价）
3. 否则若 a 是堆拥有型别且可深层 clone → 深层 clone
4. 否则值拷贝

`vec.push(x)` 不在此判断规则内：push 是方法调用，不论 x 是否堆拥有型别，都对堆拥有元素执行深层 clone（见前节）。

## 栈类型 move（栈槽重绑定 / slot-rebind）

`i64`/`u64`/`i128`/`u128`/`txt` 虽是栈类型（非堆拥有），但 `b = a`（RHS 为变数）在源 `a` 后续无引用时，不再做值拷贝，而是执行**栈槽重绑定**（stack-slot rebind）：令 `g.varAlias[b] = a`，使 `b` 与 `a` 共享同一栈槽（0 拷贝）。这优于值拷贝（值拷贝仍要 emit load+store），对大栈类型（如 256 位元组的 `%txt`）收益尤其明显。

### can_slot_rebind 约束（保守正确性）

栈槽重绑定比堆 move 的 `moveEligible` **更严格**：

- `moveEligible`：源 `a` 后续**未读**即允许 move（适用于堆类型，move 后源跳过 free，不影响目标）。
- `can_slot_rebind`（`slotRebindSafe`）：源 `a` 后续**无任何引用（读或写）**才允许重绑定。因为重绑定后 `b` 与 `a` 共享同一栈槽，若 `a` 后续被写会破坏 `b` 的值。

计算：`generateFunctionDefinition` 呼叫 `computeMoveEligibility`（同时填充 `moveEligible` 与 `slotRebindSafe`）；`generateMainFunction` 呼叫 `computeSlotRebindSafety`（**仅**填充 `slotRebindSafe`，不触碰 `moveEligible`——HEAD 的主函数不启用堆 move，保持关闭以免 SEGFAULT）。两者均透过 `stmtContainsVarRefAny` 做分支/回圈感知的引用扫描（含 `AssignExpression` 左值、回圈回边）。

当 `can_slot_rebind` 不满足（源后续仍有引用）→ **降级为完整值拷贝**，绝不退化为不安全行为。

### 禁用场景（必须走普通赋值路径）

| 场景 | 原因 |
|------|------|
| stdlib 函数（`curIsStdLib`） | stdlib 含 match/closure/coroutine 等引用分析无法完全建模的构造，重绑定会破坏共享栈槽（如 fmt 内部 `it` 被别名绑定到局部 `n`）。检测：`g.stdModules`（由 `transpiler.go` `SetStdModules` 从 `checker.KnownStdModules()` 注入），回退 `g.funcOwner[fd.Name]` |
| 目标是输出参数 | 输出参数由呼叫方传指标，重绑定使其指向局部源栈槽，呼叫方读不到 |
| 目标是全域变数 | 重绑定使全域名解析到局部源栈槽，破坏全域语义 |
| 目标是堆类型变数 | 栈槽重绑定仅适用于栈类型 |
| 源是参数 | 参数按引用传递，重绑定破坏呼叫方栈帧 |
| 源是全域变数 | 全域位址被别名到局部源，语义错误 |

### 重新赋值后别名失效

变数被重新赋值时（`b = ...`），先 `delete(g.varAlias, name)` 清除可能残留的旧别名，避免通用赋值路径经 `varAddr(name)` 仍指向旧源栈槽、污染旧源。设定新别名时沿别名链做**传递性解析**（`src := ident.Value; for { if n2,ok := g.varAlias[src]; ok { src = n2 } else break }`），找到最终源栈槽，避免多跳别名错位。

### 与堆 move 的正交性

栈槽重绑定仅作用于栈类型，不涉及所有权转移或 `free`，与堆类型的 clone/move 机制完全独立。

**测试**：`tests/slot-rebind.no`（覆盖 i64/u64/i128/u128/txt 重绑定、降级为 copy、目标重赋值后别名失效、参数 move 降级、consume 传参，期望输出 `42 7 1 1 17 5 5 10 100 99 84`）。

## FFI extern str 返回值

FFI extern 函数（`#{c}` 标记）返回的 C 字串指标（`i8*`）可能指向静态记忆体（如 `getenv`、`strerror`）或外部 buffer（如 `strchr` 返回的指标指向参数内部）。直接包装进 `%str-long` 会在 `emitHeapFree` 时 `free()` 非堆记忆体 → UB。

编译器在 FFI extern `str` 返回路径插入安全复制：

1. **NULL 检查**：若 C 返回 NULL，构造空 `%str-long`（data=0、len=0）。注意 `str` 不是 option，**`s == nil` 是编译错误**（`q4nilcmp`）——要区分 NULL 与空字串，把 extern 宣告成回传 `?str`（NULL ⇒ `nil`），或改判 `s.len == 0`
2. **非 NULL**：`strlen` + `malloc` + `memcpy` + null 终止，复制到独立堆缓冲区
3. **PHI 合并**：两条路径合并，构造 `%str-long` 返回

```no
#{c}
strchr = (s str, c i64) (r str)

find = () (r str) {
    r = strchr('hello', 108); C 返回指向 'hello' 内部的指标
    ; 编译器自动 malloc+memcpy 复制，r 独立拥有 data
    ; 函数结束时 emitHeapFree 安全释放 r.data
}
```

此机制与 clib `RetCStrToStr` 路径（用于 `get-env`、`get-wd` 等内建函数）逻辑一致，确保所有 C 字串返回值都拥有独立所有权。

## 模组级变数释放

模组级堆变数（`vec`/`str`/`arr`/结构体）编译为 LLVM global（`@name`），其 `data` 缓冲区在 `main` 入口的 top-level 语句中 malloc 初始化。

编译器在 C 入口 `main` 的 `ret i32 0` 前调用：
1. `emitHeapFree` — 释放 top-level 局部堆变数（非 globalVars）
2. `emitGlobalHeapFree` — 遍历 `moduleVarTypes`，释放所有 `globalVars` 中的堆拥有型别

```no
GLOBAL-STR = 'hello'; LLVM @GLOBAL-STR = global %str-long zeroinitializer
GLOBAL-VEC = [1, 2, 3]; LLVM @GLOBAL-VEC = global %vec zeroinitializer
; top-level 语句 malloc data 并存入 global
; main ret 前 emitGlobalHeapFree 释放 data
```

这避免了长期运行服务（如带循环的 daemon）的记忆体累积泄漏。对一次性 CLI 工具无影响（进程退出由 OS 回收）。

## 切片视图

切片表达式 `arr[1..3]` 产生视图（零拷贝），共享原数组 data。视图的三种命运：

| 目标 | 行为 | 所有权 |
|------|------|--------|
| 局部变数 `v = arr[1..3]` | 零拷贝视图 | 共享原数组 data |
| 输出参数 `out = arr[1..3]` | clone（malloc+memcpy） | 独立拥有 |
| 显式 `[]T` 型别 `v []i64 = arr[1..3]` | clone | 独立拥有 |

**原因**：输出参数逃逸到呼叫者，原数组可能在函数结束前被 free，视图必须 clone 为独立 data。

## 重新赋值与旧值释放

```no
s = 'hello'; malloc data 缓冲区
s = 'world'; 释放 'hello' 的 data，malloc 新 data
```

重新赋值堆拥有型别时，编译器在赋值前自动释放旧值的 data，避免泄漏。

## 结构体栏位释放

```no
Node {
    name str
    items []i64
}

n = Node {
    name: 'hello'
    items: [1, 2, 3]
}
; 函数结束时递回释放：
;   - n.name.data（%str-long 栏位）
;   - n.items.data（%vec 栏位）
```

结构体释放时遍历所有栏位，对堆拥有型别栏位递回释放其 data。

## 固定阵列重新赋值为切片

```no
local [4]i64 = [100, 200, 300, 400]; local 是固定阵列（16 字节）
local = [100, 200, 300]; 重新赋值为切片（16 字节）
```

固定阵列（`%arr`，2 栏位）与切片（`%vec`，3 栏位）记忆体布局不同。重新赋值时编译器自动分配新的 `%vec` 变数并重定向所有后续存取，避免缓冲区越界。

## 已验证的测试案例

测试位于 `tests/mem-safety/`：

| 测试 | 验证内容 |
|------|---------|
| `deep-clone.no` | `b = a` 深层 clone（[]i64/[]str/str/结构体）独立性 |
| `deep-free-str.no` | `[]str` 深层 free |
| `deep-free-nested-vec.no` | `[][]i64` 深层 free + push 深层 clone |
| `deep-free-struct-vec.no` | `[]MyType` 深层 free（递回结构体） |
| `struct-field-leak.no` | 结构体栏位堆数据释放 |
| `slice-view-escape.no` | 切片视图赋值输出参数的 clone |
| `reassign-leak.no` | 重新赋值旧值释放 |
| `vec-push-leak.no` | vec.push 扩容时释放旧 buffer + 堆拥有元素深层 clone |
| `ffi-str-return.no` | FFI extern str 返回值安全复制 |
| `global-heap-free.no` | 模组级堆变数在 main 退出时释放 |
| `double-move-same-source.no` | `a=x; b=x` 同源多赋值：首次 clone + 末次 move |
| `move-clone-liveness.no` | Liveness 预分析决定 clone/move（含条件分支、三赋值） |
| `rule1-binding.no` | 规则一：`b = a` 的 `str`／`[]i64`／struct／`?str`／`?[]i64` 写入独立性、只读别名、源已死 move |
| `nested-container-clone.no` | 巢状容器的深层 clone／深层 free 对偶（`[][]str`、struct 内 vec、map value 为 vec 等 12 例） |

## 已知限制

### map 容器
hashmap 未实现 key/value 的深层 free，map 容器的堆数据会泄漏。

### 循环临时变数
```no
#{fmt-skip}
loop {
    s = 'temp'   ; 每次迭代 malloc 新 data，旧 data 未释放
}
```

### 切片视图 + 原数组 move
```no
view = arr[1..3]; view 共享 arr.data
arr = [9, 8, 7]; 释放旧 arr.data → view 悬空
```

### async 共享数据（2026-09-30 已修）

`awy { r = co f(x) }` 的 spawn 边界现在会**深拷贝**拥有堆的引数，任务不再与呼叫方共享 buffer。

协程是协作式、单执行绪的，所以「跨协程共享可变堆数据」在语言层不成立；唯一会共享的
是**呼叫方在 spawn 之后仍然持有**的那份值。修复前 argbuf 只是引数描述符的**位元复制**，
`data` 与呼叫方共享，而任务要等到等待点才执行 —— 呼叫方在那之前重赋值或离开作用域就会
把 buffer 释放掉，任务于是读到已释放的记忆体（实测：str 印出 40 个 NUL、`[]i64` 印出 0，
两者 rc 都是 0 且无任何诊断）。

现在 spawn 当下就深拷贝，wrapper 在呼叫结束后释放副本：

| 参数型别 | spawn 边界 | wrapper 释放 |
|---|---|---|
| `str` | `@str_clone` | `@str_free` |
| `[]T`（T 有 deep-clone helper） | `vecDeepClone` | `@vec_free` |
| 拥有堆的 struct（内联 `str` 叶子／指标栏位） | 就地叶子 clone ＋ pointee 复制 | 递回结构体解构子 |
| 其他（纯值型别） | 位元复制（正确） | 只释放容器 |

回归：`tests/async-ownership.no`。

### struct 重赋值泄漏（2026-09-30 已修）

```no
holder {
    s str
}
h holder = holder {
    s: 'first'
}
h = holder {
    s: 'second'
}
; 旧的 'first' buffer 曾泄漏
```

重绑定一个区域变数时，旧值只有在其型别是 `Type.Owned`（`str`/`vec`/`[]T`/`map`/`?owned`）
才会被释放。struct **刻意不算** `Type.Owned`（该标记同时是 lowerer 判断「绑定是否为别名」
的依据），所以 `h = holder { … }` 直接覆写整个结构体，旧的 `s` buffer 永远不会被释放。
但 drop 机制其实是认得 struct 的（`dropOwnsHeap` → `typeOwnsHeap`，且 `emitDrop` 用递回
解构子释放），只有 lowerer 的闸门太窄。修复后重绑定前会先释放旧栏位。

实测 2,000,000 次重赋值：峰值 RSS 66.1 MB → 33.8 MB，输出逐位元组相同。

### struct literal 首栏位值是方法呼叫（2026-09-30 已修）

```no
holder {
    s str
}
h = holder {
    s: n.to-str()
}
; 修复前：'holder' is not defined
```

`{ … }` 的归类是靠向前看几颗 token 决定的。match 的**臂分隔符也是 `:`**，所以
`s: n.to-str()` 同时长得像「pattern 为 `s` 的 match 臂」与「struct literal 的栏位」。
两者的唯一消歧符是 `ident.ident` **之后**那颗 token —— 方法呼叫只可能是栏位值。
旧的分支漏了 `(`，于是整块被判成 match，struct 名称在 match 语境下找不到而报未定义
（栏位值是一般函式呼叫则正常）。

修复后 `h = holder { s: recv.method(args) }` 正常解析。nolang 的 match 臂不可能以
`name : X(` 开头（臂分隔符是 `->`，`:` 开头的臂必为 wildcard/default，body 不会带括号呼叫），
所以放行 `(` 不会把任何合法的 match 误判成 struct literal。回归：
`src/parser/struct_literal_field_value_test.go`。

### await 结果型别（2026-11-17 语法简化）

旧语法把 task handle 暴露成 `i64` 变数，曾需要跨「handle 槽位重绑定」搬移结果型别
（`t i64 = 0; t = run f(...); v = awy t`）。`run` 与 `awy <handle>` 移除后，
`co` 表达式的结果型别直接由 callee 决定，这条路径已不存在。
回归改由 `src/mir/async_boundary_ownership_test.go` 钉住底层 MIR 的结果型别。

### 任务引用计数（2026-11-17 语法简化）

内部 `%task` 仍是唯一的引用计数物件：spawn 建立时 `rc = 1`，等待完成时 `release`。
旧语法暴露手写句柄，`h2 = h` / `run h` 等复制路径曾漏计 retain（void 结果任务尤其严重，
实测 SIGSEGV），并由 `checkRefBalance` 验证器拦截。新的 `awy { ... }` 不暴露句柄，
组尾对每个 `co` 恰好等待一次，因此表面语法不会产生清单外引用。
handle 别名 / RC 的 `.no` 与 Go 测试已随 `run` 移除；MIR 层的 `OpTaskRetain` /
`OpAwait` 配对仍由 `src/mir/opt_test.go` 的 MIR fixture 覆盖。

### spawn 参数的移动：来源已死时零拷贝（2026-10-01）

`co f(x)` 对每个**拥有堆**的参数会在 spawn 边界做一次深拷贝，因为任务可能到组尾才执行，
而呼叫端在那之前可以重赋值或离开作用域。当 x 在 spawn 之后**确定不再被使用**时，
那次拷贝是多余的：所有权直接**转移**进任务。

```no
s str = 'hello'
awy {
    v = co echo(s)      ; s 之后不再被读 -> 零拷贝转移
}
print(v)                ; hello

s str = 'hello'
awy {
    v = co echo(s)
}
print(s)                ; s 仍被读 -> 维持深拷贝（两边各自拥有）
print(v)                ; hello
```

判准三个，全部必要：

1. 呼叫端**本来就会**释放 x —— 借用（切片视图、borrow read）与参数都不会被呼叫端释放；
2. x 在 spawn 之后已死（**没有任何后续读取**）；
3. 被呼叫端该位置的参数是 `str` / `[]T` / 拥有堆的 `struct` —— wrapper 会释放其 payload。

对这三类，任务 wrapper 的释放与呼叫端原本的 drop **降成同一支 helper**，所以转移只是把
**同一次释放**从呼叫端搬到任务，既不多释放也不少释放。

**不变的使用者语义**：未被等待的任务其参数缓冲区本来就由 wrapper 在等待时才释放；
移动与拷贝对这点完全相同。

实作：`insertDrops` 记进 `Module.spawnArgMoves`，`emitAsyncRun` 据此跳过深拷贝。
回归：`tests/async-arg-move.no`、`src/mir/spawn_arg_move_test.go`。

### 协程等待者表别名（2026-09-30 已修）

排程器原本用 `@nolang_waiters = global [256 x i8*]`，以 `ptrtoint(task) & 255` 当索引。
task 由 `@malloc` 配置、**16 位元组对齐**，指标低位只有 4 个有效位 —— 256 个槽实际塌缩成
16 个（实测只有 8 个会被用到），两个并发等待的任务极容易互相覆写等待者、唤醒错误的任务。

现在整张表移除，等待者存进 `%task` 自己的第 5 个栏位（`%task` 由 24 位元组增为
32 位元组）：`@nolang_async_wait` 把当前任务写进**被等待者**的栏位，
`@nolang_async_done` 读该栏位、非空则入队后清空。查询因此是精确的，不再有位址别名。

> 注：MIR 后端目前是同步驱动 await，从不呼叫 `@nolang_async_run`／`@nolang_async_wait`，
> 所以这一条在今天是**潜伏**缺陷而非可观测故障。回归：
> `src/mir/async_boundary_ownership_test.go`。

### `awy` 不是区块第一条陈述时被吃掉（2026-09-30 已修；2026-11-17 改写）

`skipToStatementEnd()` 前进到「能**开始**下一条陈述」的 token 才停，而白名单
`isStatementBoundary()`（`src/parser/stmt.go`）曾漏了前缀关键字。旧语法的 `run` / `awy t`
因此可能在非第一条陈述时被连同换行吞掉，造成 `rc=0` 且无诊断的语意退化。

新语法中 `awy` 有自己的 `parseStatement` case（协程组），并与 `co` 一起列入
`isStatementBoundary()`；`run` 不再是关键字，从白名单移除。规则是：**`parseStatement`
的 `switch` 能分派的每一个 token，都必须同时列在 `isStatementBoundary()` 里**。

回归：`tests/stmt-boundary.no`、`src/parser/stmt_boundary_block_test.go`。

### 语句位置的裸 `{ ... }` 被当成宣告（2026-09-30 已修）

`parseStatement` 的 `LBRACE` 分支会呼叫 `classifyBlockAtCurrent()`（此时 `currentToken` 已是
`{`、没有名称可消耗）。它回传的 `blockEnum` / `blockIface` / `blockTaggedEnum` 在**语句位置**
毫无意义 —— 真正的列举／介面／标签列举宣告都需要前置名称，走的是较早的
`classifyBlock()` 分支。但语句分派只把 `blockUnknown` 导向语句区块，于是：

- `{ x = 2 }` 被分类成 `blockEnum`、`{ print(2) }` 被分类成 `blockIface`；
- 两者都送进 `parseExpressionStatement`，而 `expr.go` 的 `LBRACE` 分支只认
  `blockStruct` / `blockMatch`，其余走 `p.nextToken(); return nil`；
- `{` 被吃掉，区块内陈述变成外层主体的**兄弟**，区块自己的 `}` 反而关掉了**外层**区块。

结果不是「吞掉」而是**陈述外漏一层**：`f = () { print(1); { print(2) }; print(3) }` 印出
`3,1,2`；`f = () { x = 1; { x = 2 }; print(x) }` 什么都不印。同样 `rc=0`、无诊断。

修法是在语句分派把 `blockEnum` / `blockIface` / `blockTaggedEnum` 一并导向语句区块
（`blockStruct` 必须**留在**表达式路径 —— `{ field: value }` 是合法的匿名结构体字面量陈述）。
⚠️ **不要**改 `classifyBlockAtCurrent` 的 `ASSIGN` 分支来「修」这个问题：那只涵盖三种误分类
中的一种，而且把判断搬离了唯一知道「我在语句位置」的地方。

⚠️ **连带影响**：这个修复让语句位置的 `{ ... }` 第一次真正以 `*parser.BlockStatement` 存活，
而 `programUsesPrint`（`src/build/transpiler.go`，决定要不要载入 `fmt`/`io`/`str`/`byte` 的
手写 AST walker）的白名单里没有 `BlockStatement` ⇒ 只把 `print` 写在裸区块里的程式不再被侦测。
**动到语句层就要重跑 `go test ./build/ -run TestProgramUsesPrint`**，并把新的容器型别补进
`walkStmt`。回归：`src/parser/stmt_boundary_block_test.go`、`tests/stmt-boundary.no`。

### 全局变数首次赋值的 free 跳过判断不够精确

编译器使用编译期 map `globalFirstAssigned` 追踪全局变数是否已做过首次赋值：首次赋值跳过释放旧值（旧值是 `zeroinitializer`，非堆数据），后续重赋值才释放旧堆值。

此 map 在整个编译过程中只初始化一次，不按函数级别重置，且不区分条件分支路径。如果全局变数在条件分支中首次赋值，编译器按 AST 顺序处理：第一条赋值语句标记为「首次」（跳过 free），第二条赋值语句（即使在另一分支）走重赋值路径（尝试 free 旧值）。若运行时第二条分支先执行，全局仍是 `zeroinitializer`（data=NULL, len=0），会尝试 free 未初始化的旧值。

**当前缓解措施（有效）**：
- 浅容器（`%str-long`）：`emitNullCheckFree` 生成运行时 `icmp eq i8* dataPtr, null` 检查，NULL 时跳过 `call @free`
- 深容器（`%vec/%arr`）：`emitDeepContainerFree` 额外有 `len == 0` 短路检查，zeroinitializer 的 len 为 0，直接跳过整个释放循环

这两层运行时防护使得即使编译期判断不够精确，实际不会崩溃。但逻辑上依赖运行时 NULL 检查作为安全网，而非编译期精确判断。

**潜在改进方向**：将 `globalFirstAssigned` 从编译期 map 改为运行时追踪机制（类似 `movedVarBitset` 的 bitmap），但会增加运行时开销，且当前缓解措施已足够有效。

## 设计取舍

1. **深层 clone 的成本。** `b = a` 会配置并复制一份 data；`vec.push(x)` 也对堆拥有元素做一次深层 clone。
   这是刻意的：**用一次拷贝换掉一整类别名分析**——值对值之间只保留「拥有」与「搬移」，不保留共享，
   于是「谁拥有、谁释放、写入是否可被观测」不需要为每一种别名情形各写一组守则。
2. **spawn 边界的深拷贝（＝快照）。** `co f(x)` 对拥有堆的引数在 spawn 当下做一次深拷贝，所以任务看到的
   是 **spawn 当下的值**，与呼叫方在那之后的写入无关。**来源已死时改为零拷贝 move**。
   ⚠️ 不要为了省这份拷贝而改成「共用同一块 buffer ＋ 引用计数」：那会让任务看到呼叫方在 spawn 与组尾等待
   之间的**原位写入**（共享），违反第 4 条。要省拷贝的正确做法是「**写入时才拷贝**」（copy-on-write）。
3. **任务引用计数不留环。** 内部 `%task` 是唯一的引用计数物件；新的 `awy { ... }` 不暴露 handle，
   每条 `co` 在组尾恰好等待一次，因此不会形成「任务互相持有 handle」的环。
4. **协程之间不共享可变堆数据。** 协程是**协作式、单执行绪**调度，跨协程共享可变堆数据在语言层不成立；
   允许共享只会带来资料竞争与悬空指标。因此 spawn 边界一律传递**独立副本**（或转移所有权）。
