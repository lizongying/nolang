---
sidebar_position: 4.2
---

## 异步（Async）

Nolang 只有一种表面并发语法：`awy { ... }` 协程组，组内用 `co` 衍生任务。
`run` 关键字已移除（现在是普通识别符）；`awy <handle>` 与语句位置裸块协程组都已废除；
**组外 `co` 是编译错误**（`E_CO_OUTSIDE_GROUP`）。

`co` 只启动**过程**——被呼叫者不得有宣告回传值（方法隐式返回的 `self` 不算），
所以结果一律走通道：过程把值发进通道再 `close()`，呼叫方在组外排空。

### 协程组

```no
; 过程：只写通道，没有宣告回传值
produce = (ch chan[i64], v i64) {
    ch <- v
    ch.close()
}

ch1 chan[i64] = with-cap(1)
ch2 chan[i64] = with-cap(1)

; 并发：两个任务一起跑，组尾统一等待
awy {
    co produce(ch1, 1)
    co produce(ch2, 2)
}

r1 = 0
<- ch1: { ok -> r1 = it }
r2 = 0
<- ch2: { ok -> r2 = it }

; 顺序：后一个 spawn 用前一个的结果当实参，靠「读通道」当屏障
awy {
    co produce(ch1, r1)
}
```

`ch1` / `ch2` 里拿到的是结果值，不是 task 句柄。详细见 `lang/syntax.md`「异步编程」。

### 无色 async：`co`

`co` 是无色（colorless）语法：你写未染色的函式名 `produce`，编译器自动单态化出
`produce-async` 变体并在底层以染色无栈协程执行。不要在应用码手写 `-async` 后缀。

两种形态：

- `co f(args)` — 过程 spawn，`f` 不得宣告回传值（方法隐式 `self` 除外）。
- `co { ... }` — 闭包 spawn，区块内的自由变数按值捕获（spawn 当刻的快照）。

`co` 不是值，必须独立成句：`r = co f()`、`print(co f())` 都报 `c0nostmt`。

规则：

- **单态化**：`co produce(ch, 1)` 会自动生成 `produce-async`（复制 `produce` 的本体、改名）。若 `produce` 同时也被同步呼叫，两者共存。
- **传递性**：若某过程内部也有 `awy { co child(n) }`，该过程本身也会被单态为 `-async`。
- **底层 lowering**（不是使用者语法）：每条 `co` 变成 `OpRun`，组尾插入 `OpAwait`。

### 取消

`cancelled()` 可在协程函式内作协作式自我检查。`cancel(h)` 是底层句柄操作，
新语法不暴露 task 句柄，应用码不使用。

> **注意：** 取消是协作式而非抢占式。长阻塞调用（如网路请求）无法被强制中断；
> 任务会在下一个协作检查点（`cancelled()` 调用或事件回圈下次调度）真正停止。

#### 用通道自己做取消（推荐）

新语法没有 `ch.cancel()`，通道也没有 `cancel` 变体。需要「外部发信号让 worker 收尾」时，
**自己维护一个通道，用 `close()` 当取消信号**——这也是 `chan[T]` 唯一的取消语义。
接收回圈的三态里，`nil` 表示「已关闭且取空」，正是取消分支；`err` 只表示未初始化句柄，
不要用来接取消。

```no
; 控制通道：关闭即取消
stop chan[i64] = with-cap(0)

worker = (stop chan[i64]) {
    <- stop: {
        ok  -> { }              ; 收到讯息（本例不用）
        nil -> print('cancel')  ; stop.close() 后落到这里
        err -> { }              ; 未初始化的句柄
    }
}

stop.close()
awy {
    co worker(stop)
}
; 输出 cancel
```

要在执行中由另一个任务触发取消，把关闭动作也放进组内（不能在 `awy` 之后才 close——
组尾会统一等待，worker 若已 park 在空通道上会死锁）。注意 `<- ch: { ... }` 是**接收回圈**：
收到一个值后会继续等下一个，所以通知方发完必须 `close()`，canceller 才会跨过回圈执行到
`stop.close()`：

```no
stop chan[i64] = with-cap(0)
awake chan[i64] = with-cap(1)

worker = (stop chan[i64]) {
    <- stop: { ok -> { }  nil -> print('cancel')  err -> { } }
}

canceller = (awake chan[i64], stop chan[i64]) {
    <- awake: { ok -> { }  nil -> { }  err -> { } }   ; 等通知，收到后还要等 close 才会离开回圈
    stop.close()                                       ; 这一句才是取消信号
}

notify = (awake chan[i64]) {
    awake <- 1
    awake.close()                                      ; 让 canceller 的接收回圈走到 nil
}

awy {
    co worker(stop)
    co canceller(awake, stop)
    co notify(awake)
}
; 输出 cancel
```

若只是想在 worker **自己的每个步骤开头**检查是否已请求停止，用 `cancelled()` 更直接；
但它只能自我检查，外部无句柄可通知——「由别人发信号」的场景就走上面的通道惯例。

---
