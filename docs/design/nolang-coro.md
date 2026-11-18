# 无栈协程（`awy` / `co`）实现设计

> **文档定位**：**实施记录 + 不变量清单**，不是前向设计。描述的都是已经落地的代码
> （2026-11-18，`src/mir/coro.go`、`src/mir/codegen_coro.go`、`src/mir/codegen_chan.go`）。
> 行为面（用户看到的语义与顺序保证）见 `docs/docs/lang/syntax.md` 的「異步編程」一节；
> 缺陷与验收记录见 `test/README.md §5.2ae`。
>
> **一句话模型**：`co` 产生的 `-async` 克隆若含挂起点，就被改写成一台
> **堆帧 + 状态号**的状态机；调度是单线程协作式，就绪队列 FIFO，因此**顺序完全可断言**。

---

## 0. 术语与落点

| 术语 | 含义 | 代码 |
|---|---|---|
| 克隆 | `co f(...)` 时由 HIR 单态化出的 `f-async` | `src/hir`（`MonomorphizeGo`） |
| 协程 | 含挂起点、被 `PlanCoroutines` 改写过的克隆 | `Function.IsCoro` / `Function.Coro` |
| 帧 | 协程的堆上局部区，挂 `%task.frame` | `%frame.N` |
| park | 一次挂起：把状态号写进帧、把控制权交回调度器 | `OpCoroPark` |
| 挂起点 | `OpAwait` / `async-yield()` / `OpChanRecv` | `isSuspendInst` |

`co` 在 HIR 层降为 `KCall` + callee 改名 `-async`。也就是说 **HIR 里没有「协程」这个概念**，
「这是不是一个可挂起的克隆」只能在 MIR 层按**名字后缀**判定 —— 这是本节所有判据的来源。

---

## 0.1 `co` 的两种形态（2026-11-18 收口）

**一句话**：`co` 只启动**过程**，且本身不是值。两条都是硬规则，各有自己的落点。

| 形态 | 语法 | 落点 |
|---|---|---|
| 过程 spawn | `co f(args)` | AST `CoExpression{Call: *CallExpression}` |
| 闭包 spawn | `co { ... }` | AST `CoExpression{Call: *FunctionLiteral}`（零参数） |

**规则一：被调用者不得有宣告返回值。** spawn 的结果没有去处，所以带返回值的函数被
`co` 调用时报 `c0retval`。判据是 `parser.DeclaredResults(fd)`——它会剥掉方法隐式声明的
`self`，因此 `co obj.flush()` 合法（`self` 是接收者出参，不是结果）。该检查在
`src/checker/co_spawn.go`：索引全名与末段名两套（`obj.m` 的定义登记在 `Type.m` 下），
**只有当同名重载全部有返回值时才报**——部分重载是过程时，降级期解析到哪一个不可知，
宁可放过。结果一律走通道：过程 `ch <- v; ch.close()`，调用方在组外排空。

**规则二：`co` 不是值。** 它必须占满一个语句，所以赋值右侧、实参位、返回值位都在
parser 层被拒（`E_CO_NOT_STATEMENT` / `c0nostmt`）。判定靠**前一个 token**：

```go
p.prevToken.Type ∈ {NEWLINE, SEMICOLON, LBRACE, RBRACE, EOF, RARROW, COLON, ILLEGAL}
```

`parseExpression` 顶部的 NEWLINE 跳过大循环会吃掉行首换行，所以「`co` 在下一行」这种
写法由 `parseLetStatement` 对右值再补一道同码检查。

**闭包 spawn 的捕获按值。** `coClosure` 在 tohir 阶段合成一个普通函数 `__coN`，参数 =
块内提到、且在外层函数 `FuncVarType` 里查得到类型的**自由变量**（块内自己声明的名字先
收集再排除，形成 shadow 语义），然后照常登记进 `Package.GoSpawns`，后续与过程 spawn
走同一条单态化路径。之所以天然按值：spawn 协议本来就把每个堆拥有的实参深拷进任务自己的
buffer（见 §7），形参就是那份拷贝。也正因为如此，闭包里写捕获变量不会回传给外层——
需要回传就走通道。

**`co` 位置的正确性依赖 `parseBlockStatement` 的停位约定**：它停在 `}` 上**不消费**。
表达式位置的约定是「currentToken 停在最后一个 token 之后」，所以闭包分支必须自己补一次
`p.nextToken()`；漏掉会让后续语句被挤出外层函数体（实测症状：后续 `print` 完全不进 IR）。

---

## 1. 为什么必须是「帧 + 状态分派」

MIR 是 **无 phi 的 SSA-less IR**：跨基本块携带一个值只能靠 entry `alloca` 或内存。
挂起会把控制权交回调度器，栈帧随之失效，所以：

1. 局部变量不能留在栈上 ⇒ 搬进堆上的帧（`%frame.N`）；
2. 「从哪继续」不能留在栈上 ⇒ 写进帧的 `__state` 字段，恢复时由入口的比较链跳回；
3. 「当前帧在哪」也不能靠栈传递 ⇒ 协程 ABI 改成 `void (i8* %task_ptr)`，
   帧从 `%task.frame`（field 5）取出并 `bitcast` 成 `%frame.N*`。

第 3 点决定了 spawn 侧不再需要「wrapper + 参数缓冲」把形参逐字段传进去：
**帧本身就是参数容器**，spawn 时把深拷后的实参直接存进对应帧槽。

---

## 2. 帧布局

```
%frame.N = type { i32 __state, i32 __pad, <slot 0>, <slot 1>, ... }
```

| 字段 | 含义 |
|---|---|
| 0 `__state` | 恢复点编号；0 = 尚未开始（要跑零初始化） |
| 1 `__pad` | 对齐占位，让 slot 0 落在 8 字节边界 |
| `i+2` | slot `i`，`CoroFrameHeader = 2` |

**槽的来源**（顺序固定，`CoroInfo.SlotIdx` 记录 `ValueID → 槽号`）：

1. 全部形参，**方法 receiver 除外** —— receiver 指向调用方分配的结果缓冲，
   本身就活得比任何挂起都久；
2. 每条指令的 `Dst`；
3. 每条指令的 `Results`（多返回值）。

`allocaFor`（`codegen.go` 里 `emitFunc` 的闭包）是**唯一的槽分配点**。
协程分支只把它从「发 `alloca`」改成「发 `getelementptr` + 记 `c.valSlot`」，
下游所有 `load` / `store` 一字不改。

### 2.1 零初始化只在 state 0 跑

帧是 `rc_alloc` 出来的，**不保证清零**。槽的零值（option 的 nil tag、owned 指针的
`zeroinitializer`）必须写，但**只能在 state 0 那条路径上写**：恢复时再跑一遍会把
park 存下的值擦掉。实现上把这段发射重定向到缓冲 `c.coroInit`，再拼进 `cs.init` 块。

---

## 3. 切块与 park

对每个含挂起点的块：按挂起点切成 `n+1` 段，段 `k` 以第 `k` 个挂起点**末尾**结束。

- 段 0 留在原块 `bid`；
- 段 `k>0` 进新块 `cs.<bid>.<k-1>`；
- 原终结符搬到最后一个新块；
- 段 `k` 的终结符换成 `OpCoroPark`（`Term.Int` = 状态号，`Term.Sym` = `await|chan|yield`）。

`OpCoroPark` 刻意设计成**非 sink**：它的 `Targets[0]` 是到 continuation 的真实 CFG 边，
所以 `BuildCFG`、支配关系与所有基于可达性的分析仍然正确 —— 这也是
「`PlanCoroutines` 必须在 `Analyze()` 之后」的另一半理由。

生成的形状（await）：

```
cs.chk1.2:                                  ; park 块
  %cs.hdv.108 = load i1, i1* %cs.hdg.107
  br i1 %cs.hdv.108, label %<cont>, label %cs.park1.2
cs.park1.2:
  store i32 1, i32* %cs.statep              ; __state = 1
  call void @nolang_async_wait(i8* %cs.ht.105)
  ret void                                  ; 交回调度器
```

通道 park 同构，只把「检查 done」换成 `call i1 @nolang_chan_ready(i8*)`、
「登记为 waiter」换成 `call void @nolang_chan_park(i8*)`。

### 3.1 恢复点与 continuation 不是一回事

| 挂起类型 | `Cont`（正常前进） | `Resume`（恢复后跳到哪） |
|---|---|---|
| await | 段 `k+1` | 同 `Cont` |
| yield | 段 `k+1` | 同 `Cont` |
| chan recv | 段 `k+1`（recv 指令本身） | **park 块自己** |

通道必须回到 park 块：一次 `send` 只加一个元素，却会唤醒整条等待链，
被唤醒 ≠ 拿到值。回到 park 块重跑 `ready` 判断，才知道这次该取还是继续等。

---

## 4. 状态分派

`cs.dispatch` 是新的 entry，形状是一条 `icmp` 链（`OpSwitch` 在 codegen 没有发射器）：

```
f_async.bb0:
  %cs.fg.68 = getelementptr inbounds %task, %task* %task_ptr, i32 0, i32 5
  %cs.raw.69 = load i8*, i8** %cs.fg.68
  %cs = bitcast i8* %cs.raw.69 to %frame.2*
  %cs.statep = getelementptr %frame.2, %frame.2* %cs, i32 0, i32 0
  %cs.st.76 = load i32, i32* %cs.statep
  %cs.z.77 = icmp eq i32 %cs.st.76, 0
  br i1 %cs.z.77, label %cs.init.2, label %cs.d0.2
cs.init.2:
  store i64 zeroinitializer, i64* %cs.1     ; 帧槽零初始化（只此一次）
  br label %f_async.bb1
cs.d0.2:
  %cs.e.78 = icmp eq i32 %cs.st.76, 1
  br i1 %cs.e.78, label %f_async.bb2, label %cs.d1.2
cs.d1.2:
  br label %f_async.bb1                     ; 兜底 = state 0 的主路径
```

分派表在 MIR 里只是一条 `OpBr`（`cs.dispatch → body`），真正写出来的是 codegen：
这样 `BuildCFG` 早就能看到 body 可达，不必为 codegen 单独造边。

---

## 5. 调度器运行时

```
%task = type { void (i8*)*, i64, i1, i1, i8*, i8*, i8*, i8*, i32, i32, i8* }
;                 resume_fn   data done canc  waiters frame  next  waitnext depth pad join_top = 72B
```

| 字段 | 用途 |
|---|---|
| 0 `resume_fn` | 协程体入口（`@f_async`）；普通任务是 wrapper |
| 1 `data` | args 包装（`{ i8* result_ptr }`），协程只放结果指针 |
| 2 / 3 | `done` / `cancelled` |
| 4 `waiters` | await 这条任务的等待链**链头** |
| 5 `frame` | `%frame.N`（普通任务为 null） |
| 6 `next` | 通道等待链的链节 |
| 7 `waitnext` | await 等待链的链节 |
| 8 `depth` | spawn 时继承的逻辑嵌套深度（§9.2） |
| 9 `pad` | 对齐占位，`rc_alloc` 不清零，显式写 0 |
| 10 `join_top` | 该任务自己的 join-set frame 链头（§9.5；顶层 main 无任务，走全局栈） |

两条链用**不同字段**是刻意的：连接点相同的话，一次「既在通道链上、又是某人的
waiter」就会把两条链串在一起。等待者只能挂在一处，但这个不变量交给字段隔离比交给
纪律更可靠。

### 5.1 运行时入口

| 入口 | 语义 |
|---|---|
| `@nolang_async_enqueue(i8*)` | 入队（环形缓冲，满则增长）。**自带 `rc_retain`**；null 请求 no-op |
| `@nolang_async_yield()` | 把 `@nolang_current_task` 重新入队 |
| `@nolang_async_drain_until(i8* target)` | 驱动队列到 target `done`（target 为 null 则跑到队列空），每次 resume 前后换入/恢复 `@nolang_current_task` |
| `@nolang_task_retire(i8*)` | 读 `data`/`frame` → free 帧 → `rc_release` → 计数归零时释放 args / result 包装 |
| `@nolang_async_wait(i8*)` | 把当前任务**前插**到 `waited.waiters` |
| `@nolang_async_done(i8*)` | 摘下整条等待链，逐个入队 |

三个顺序敏感的细节：

1. **队列持有自己的引用**。awaiter 一读完结果就可能 `retire`，而任务还在队列里；
   没有 `rc_retain` 就是 UAF。配对的 `rc_release` 在 drain 循环的 `rel` 块。
2. **`task_retire` 必须先读后放**。`rc_release` 到 0 会 free 整个 `%task` 块，
   之后再读 `data` / `frame` 就是读已释放内存 —— 症状是首个 `awy` 必 SIGSEGV。
3. **唤醒要「全链」，不能只唤一个**。等待者被唤醒只是「可以再检查一次」，
   多唤醒无害（它们会重新 park），少唤醒则是永久丢任务（没有第二个完成事件）。

### 5.2 死锁判定

`drain_until` 的出口：target 仍未 `done` 而队列已空 ⇒ 没有任何任务能再推进，
印 `nolang: deadlock: no runnable task and all tasks are blocked` 到 stderr 后 `abort`。
替代方案是返回、让调用方读一个**从没人写过**的结果槽 —— 静默的 0 比崩溃更难查。

---

## 6. 通道挂起

`%chancb`（56B）：`{ len, cap, head, elemsize, buf, closed, waiters }`。

| 入口 | 语义 |
|---|---|
| `@nolang_chan_ready(i8*) -> i1` | null / `len > 0` / `closed` ⇒ 可读（null 与 closed 都要放行，否则空通道 park 后永不返回） |
| `@nolang_chan_park(i8*)` | 当前任务前插到 `chancb.waiters`（链节 = `%task.next`） |
| `@nolang_chan_wake(i8*)` | 摘下整条链、清 `next`、逐个入队（`send` 与 `close` 末尾调用） |
| `@nolang_chan_recv_block(i8*, i8*)` | 非协程的接收：recv → 空则 `drain_until(null)` → 再 recv → 仍空则判死锁 |

三态语义（codegen 直接折成 `?T` 的 tag）：

| tag | 含义 | 循环行为 |
|---|---|---|
| 0 `ok` | 取到一个元素 | 继续 |
| 1 `nil` | 已关闭且取空 | 结束（等价 Go 的 range 语义） |
| 2 `err` | **零值句柄**（未初始化） | 结束；这是唯一的真错误 |

「空」不再是 `err`：协程 park、普通函数阻塞驱动队列。因此**排空循环必须靠 `close()` 结束**，
忘了 `close()` 的程序会停在最后一轮（协程里挂起、普通函数里判死锁）。

---

## 7. 所有权与 pass 顺序

`LowerHIR` 的尾部：

```go
rep := l.mod.Analyze()          // 所有权 / drop / view liveness / spawn 实参分类
if os.Getenv("NOLANG_CORO") != "0" {
    l.mod.PlanCoroutines()      // 切块
}
```

顺序是承重的，两个方向都不能换：

- **在前**（先切块再 Analyze）：drop 点会落在 park 之后，挂起时栈上局部已丢；
  并且每个 dataflow pass 都得知道 `OpCoroPark` 是「非 sink + 有后继」。
- **在后**（本轮）：Analyze 看到的还是原始 CFG，挂起点只是普通指令，
  drop 插入位置与切块无关。

`NOLANG_CORO=0` 是应急开关，关掉后回到「spawn 后就地跑完」，只对不含循环/嵌套挂起
的形态等价 —— 它的用途是 A/B 对照（证明语料在测协程），不是长期选项。

---

## 8. 顺序保证（可断言的契约）

调度是单线程协作式、就绪队列 FIFO：

| 动作 | 顺序 |
|---|---|
| 组内多个 `co` | 按语句顺序入队，依次跑到第一个挂起点 |
| `async-yield()` | 自己回队尾 ⇒ 兄弟任务先跑一轮 |
| 子任务完成 | 等待链逐个入队，**LIFO**（后登记者先恢复） |
| `send` / `close` | 摘下整条通道等待链，链头先入队（也是 LIFO） |

`tests/coro-yield.no` 把事件码序列整个钉住（`a1 b1 a2 b2`、`a1 b1 c1 a2 b2 c2` 等），
任何调度顺序变化都会翻红。

---

## 9. 已知缺口

| # | 缺口 | 影响 |
|---|---|---|
| 1 | `@nolang_chan_free` 不摘 `waiters` 链 | **已守护**（§9.3）：释放前查链，非空即 `deadlock_abort()`，不等到 UAF |
| 2 | `drain_until(null)` 遇无限 `async-yield()` 不返回 | **已守护**（§9.3）：1,000,000 步上限，超限 abort |
| 3 | 屏障里的嵌套 `co`：循环体内的仍是「spawn 后立即 join」 | **已修**（§9.1 / §9.5）：直线嵌套块用组尾静态 await，循环/if/match 臂走运行时 join-set 组尾 join-all |
| 4 | 协程体不做栈溢出检查 | 已有**逻辑嵌套深度**守卫（§9.2）；不是原生栈溢出，但可以拦住递归 spawn 造成的任务链无限增长 |
| 5 | `%task` 无 TLS/线程亲和性 | **刻意约束**（§9.4）：单线程协作式是顺序可断言的前提；真并行需队列+chancb+current_task 一起改，不在本轮范围 |

### 9.1 嵌套 `co` 的句柄收集（§9 #3 的已修部分）

`awy` 组里的屏障（非 `co` 语句）此前会让其内部的 `co` 走 lowerer 的 #108
回退：每个 spawn 紧跟一个 await，因此串行。`src/hir/async_group.go` 现在会
**递归扫描「屏障本身是裸块 `{ ... }`」的情形**，把里面的 `co` 也登记成
`GroupPlan.Handles`：

- `KRun` 绑定留在原块内（实参仍在源码位置求值）；
- 屏障前该 flush 的旧句柄照旧 flush，不会因为嵌套改写而被吞掉；
- 该句柄的 `awy` 移到**组尾**，与同组其他 spawn 一起 await。

`TestAsyncGroupNestedBareBlockDefersAwaits` 钉住 `run run await await`，
`TestAsyncGroupNestedBareBlockStillFlushesEarlierSpawns` 钉住屏障前仍 flush。

循环体（以及 if/match 臂）里的 `co` 也曾走 #108 串行回退，原因是这些位置里的
句柄绑定会被重复执行（每轮覆盖同一槽）或根本不执行（分支未进入），组尾只读
一个槽会漏 await（任务泄漏）或读到未初始化值。现在它们改走**运行时 join-set**
（§9.5）：HIR pass 把每个臂内的 `co` 改写成 `KRun` + `__join-push`，组头/组尾
分别插入 `__join-enter` / `__join-leave`，句柄不再依赖编译期槽位，因而能跨
迭代累积并在组尾统一 join-all。`tests/coro-join-all.no` 用「等待者 park 在空
通道、后续任务才发信号」的互相依赖形状区分两种调度：串行会 deadlock abort，
join-all 通过。

### 9.2 嵌套深度守卫（§9 #4 的落地形式）

无栈协程的帧是堆块，**没有原生栈可以溢出**；真正会失控的是递归 spawn 拉出的
任务链——每个任务还带着 args buffer / result buffer / frame。`%task` 末尾加了
`i32 depth, i32 pad`（56B → 64B），两个 spawn emitters（`emitAsyncRun` 与
`emitCoroutineRun`）在分配任务之前调用 `emitSpawnDepthCheck`：

1. 读 `@nolang_current_task`（调度器在 resume 之前写入）；
2. 为 null（顶层 / 普通入口）时子进程 depth = 1，否则父 `field 8` + 1；
3. `depth > 1024` → `nolang_depth_abort()`：打印诊断并 `llvm.trap()`，
   **在分配下一帧之前**就停下；
4. 把 depth 写进子任务的 field 8。

这不是源码级递归检测：一个普通函数被协程调用时同样继承当前任务的 depth，
这符合「逻辑调用链」的定义。它也不能替代原生栈检查——普通（非协程）递归仍走
LLVM/clang 的栈保护，与本守卫无关。

`TestTaskDepthGuard` 钉住类型布局、父 depth 读取、子 depth 写回与 abort 入口。

### 9.3 通道 / drain 的两处失控保护（§9 #1/#2）

两处都不是「修好了语义」，而是**把原本会挂死或 UAF 的路径改成显式 abort**：

- `src/mir/codegen_chan.go` 的 `@nolang_chan_free` 在释放 `%chancb` 前读
  `field 6 (waiters)`；链非空说明还有协程挂在上面，直接调用
  `@nolang_deadlock_abort()`，避免释放后等待者恢复造成悬空访问。
- `src/mir/codegen.go` 的 `@nolang_async_drain_until` 在 **null-target 模式**
  （普通代码的通道空读）加 phi 步数计数，超过 `1_000_000` 就 abort。
  这拦的是「任务无限 `async-yield()` 把自己反复回队」的 livelock；
  target 非 null 的组尾 join 不受此限。

`TestChanFreeRejectsDanglingWaiters` 与 `TestDrainUntilNullHasStepBound`
分别钉住两处 IR。详见 `test/README.md §5.2ai`。

### 9.4 单线程模型是刻意约束（§9 #5 的处置）

调度器是**单线程协作式**：`@nolang_ready_q` / `@nolang_current_task` / `%chancb`
都是裸全局变量，没有锁，也没有 TLS 或线程亲和性。这不是漏实现，而是当前
**顺序完全可断言**（§8）的前提——`tests/coro-yield.no` 把事件码序列整个钉住，
一旦允许真并行，队列顺序、唤醒链顺序与 `current_task` 的语义都变成竞态结果。

因此现状是：**不支持跨 OS 线程调度协程**。要放开它需要同时改造
`@nolang_async_enqueue` / `drain_until` 的队列、`%chancb` 的缓冲和 waiters 链，
以及 `@nolang_current_task` 改为 TLS；三处必须一起做，单独加锁不足以成立。
这属于运行时模型的变更，不在本轮缺口收口范围内。当前唯一合法的并发来源仍是
单线程内的「spawn → 挂起 → 恢复」交错。

### 9.5 运行时 join-set（§9 #3 的完整落地）

三个编译器内部 builtin（不出现在语言层），由 `hir.DesugarAsyncGroups` 合成调用，
`src/mir/builtin_call.go` 内联成 LLVM 调用：

| builtin | 作用 |
|---|---|
| `__join-enter()` | 顶层 main 用固定全局栈；运行中的任务在 `%task.join_top` 上链一个新 frame（计数清 0，深度继承） |
| `__join-push(h)` | 把 `OpRun` 产出的句柄记入当前 frame |
| `__join-leave()` | 调 `@nolang_join_reap` 对每个句柄做**带 target 的** `@nolang_async_drain_until(i8* hp)` + `@nolang_task_retire(hp)`，再弹栈（任务路径同时释放 frame） |

运行时状态分两层：顶层 main（没有 `%task`）沿用三个全局 `@nolang_join_sp`、
`@nolang_join_n[64]`、`@nolang_join_h[64][256]`；每个任务则通过新增的
`%task` field 10 `join_top` 挂一条自己的 frame 链（`%joinframe = { prev, n,
depth, [256 x i64] }`，堆分配）。`%task` 因此 64B → 72B，两个 spawn emitter
已同步。嵌套组和单组 spawn 数超上限（64 帧 / 256 句柄）时打印诊断并 trap。

关键不变量：

- `OpRun` 产生的句柄只被 `__join-push` 传参，没有 SSA 别名需要 `OpTaskRetain`；
  组尾 `task_retire` 恰好释放一份引用，与 `OpAwait` 的释放纪律一致。
- **drain 必须逐句柄带 target**，不能用 `drain_until(null)`。null 模式在就绪队列
  空时直接返回，哪怕记录的任务仍 park 在通道上；随后的 `task_retire` 会释放它
  仍在使用的 frame，等外部再发信号就是 UAF（实测 `tests/_known-deadlock/join-parked.no`
  从 SIGSEGV 变为正确的 `nolang: deadlock` abort）。非 null 路径自带
  「无 runnable 任务且 target 未完成」的 deadlock 检查，与静态槽的 `OpAwait` 同构。
- 一个句柄 drain 完成后可能顺带跑完后续句柄的任务；后续那次 drain 在入口
  检查 `target.done` 后立即返回，不会重复执行。
- **frame 必须属于任务，不能只属于全局栈**：`awy` 组可能在 enter/leave 之间
   park。实测 `tests/coro-join-interleave.no` 的次序是 `a-enter b-enter
   a-leave b-leave`；单一全局栈下第二次 resume 会弹掉对方的 frame 并二次
   retire。现在 enter/push/leave 都先看 `@nolang_current_task`，任务走
   `join_top` 链，只有顶层 main 走全局栈，两者互不干扰。
- 不进入嵌套的 `awy` 组：内层组先被 desugar（innermost-first），自己拥有
  enter/leave 配对；任务内嵌套组由 `frame.depth` 守 64 层上限。

**已知边界**：如果 `awy` 组因为 `return` / `break` 提前退出，组尾的 `__join-leave`
不会执行，这一帧及其句柄会留在任务链/全局栈上（句柄引用泄漏，静态槽路径同样
漏掉组尾 `awy`）。任务被 retire 时 `@nolang_task_retire` 会把残留的 frame 链
释放掉，避免帧本身泄漏；引用泄漏仍存在，这不是 join-set 引入的新语义差异。

**按需发射（门控）**：以上 globals 与 5 个函数只在模块确实含 `__join-*`
调用时才写入 LLVM。`Module.scanJoinSetUse()` 在 `EmitLLVM` 前扫所有 MIR
`OpCall` 的 `Sym`；没有控制流臂 `co` 的程序连 `@nolang_join_h`（128 KiB BSS）
和 5 个死函数一起省掉（实测一个 hello-world 的 join 引用从 25 行降到 0）。
核心调度器 runtime 仍无条件发射——只有 join 这片叶子被门控，因为它单向引用
`drain_until` / `task_retire`，反过来无人引用它。`%joinframe` 类型声明仍在
prelude（未使用的类型声明不占运行时内存）。

覆盖点：`TestFlowArmCoUsesJoinSet`（HIR 形状）、`TestFlowArmCoIsNotAwaitedInline`
（MIR 形状：run + push，零 await）、`TestJoinSetRuntimeIR`（运行时 IR 齐备）、
`TestJoinLeaveDrainsEachTargetBeforeRetire`（join-leave 委托 per-handle reap、
无 null drain）、`TestJoinSetPerTaskFrames`（任务路径用 `join_top` 链）、
`TestJoinSetGatedWhenUnused` / `TestJoinSetScanDetectsRewrittenSpawns`（门控
双向），以及端到端 `tests/coro-join-all.no`、`tests/coro-join-interleave.no`、
`tests/coro-join-match.no`、`tests/coro-join-chaniter.no`（四种控制流臂，
每个都用互相依赖的两个任务在 kill switch 下 deadlock 来区分 join-all 与串行）。
