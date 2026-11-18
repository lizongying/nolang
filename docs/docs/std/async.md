---
sidebar_position: 4.2
---

## 異步（Async）

Nolang 只有一種表面並發語法：`awy { ... }` 協程組，組內用 `co` 衍生任務。
`run` 關鍵字已移除（現在是普通識別符）；`awy <handle>` 與語句位置裸塊協程組都已廢除；
**組外 `co` 是編譯錯誤**（`E_CO_OUTSIDE_GROUP`）。

`co` 只啟動**過程**——被呼叫者不得有宣告回傳值（方法隱式返回的 `self` 不算），
所以結果一律走通道：過程把值發進通道再 `close()`，呼叫方在組外排空。

### 協程組

```no
; 過程：只寫通道，沒有宣告回傳值
produce = (ch chan[i64], v i64) {
    ch <- v
    ch.close()
}

ch1 chan[i64] = with-cap(1)
ch2 chan[i64] = with-cap(1)

; 並發：兩個任務一起跑，組尾統一等待
awy {
    co produce(ch1, 1)
    co produce(ch2, 2)
}

r1 = 0
<- ch1: { ok -> r1 = it }
r2 = 0
<- ch2: { ok -> r2 = it }

; 順序：後一個 spawn 用前一個的結果當實參，靠「讀通道」當屏障
awy {
    co produce(ch1, r1)
}
```

`ch1` / `ch2` 裡拿到的是結果值，不是 task 句柄。詳細見 `lang/syntax.md`「異步編程」。

### 無色 async：`co`

`co` 是無色（colorless）語法：你寫未染色的函式名 `produce`，編譯器自動單態化出
`produce-async` 變體並在底層以染色無棧協程執行。不要在應用碼手寫 `-async` 後綴。

兩種形態：

- `co f(args)` — 過程 spawn，`f` 不得宣告回傳值（方法隱式 `self` 除外）。
- `co { ... }` — 閉包 spawn，區塊內的自由變數按值捕獲（spawn 當刻的快照）。

`co` 不是值，必須獨立成句：`r = co f()`、`print(co f())` 都報 `c0nostmt`。

規則：

- **單態化**：`co produce(ch, 1)` 會自動生成 `produce-async`（複製 `produce` 的本體、改名）。若 `produce` 同時也被同步呼叫，兩者共存。
- **傳遞性**：若某過程內部也有 `awy { co child(n) }`，該過程本身也會被單態為 `-async`。
- **底層 lowering**（不是使用者語法）：每條 `co` 變成 `OpRun`，組尾插入 `OpAwait`。

### 取消

`cancelled()` 可在協程函式內作協作式自我檢查。`cancel(h)` 是底層句柄操作，
新語法不暴露 task 句柄，應用碼不使用。

> **注意：** 取消是協作式而非搶占式。長阻塞調用（如網路請求）無法被強制中斷；
> 任務會在下一個協作檢查點（`cancelled()` 調用或事件迴圈下次調度）真正停止。

#### 用通道自己做取消（推薦）

新語法沒有 `ch.cancel()`，通道也沒有 `cancel` 變體。需要「外部發信號讓 worker 收尾」時，
**自己维护一个通道，用 `close()` 當取消信號**——這也是 `chan[T]` 唯一的取消語義。
接收迴圈的三態裡，`nil` 表示「已關閉且取空」，正是取消分支；`err` 只表示未初始化句柄，
不要用來接取消。

```no
; 控制通道：關閉即取消
stop chan[i64] = with-cap(0)

worker = (stop chan[i64]) {
    <- stop: {
        ok  -> { }              ; 收到訊息（本例不用）
        nil -> print('cancel')  ; stop.close() 後落到這裡
        err -> { }              ; 未初始化的句柄
    }
}

stop.close()
awy {
    co worker(stop)
}
; 輸出 cancel
```

要在執行中由另一個任務觸發取消，把關閉動作也放進組內（不能在 `awy` 之後才 close——
組尾會統一等待，worker 若已 park 在空通道上會死鎖）。注意 `<- ch: { ... }` 是**接收迴圈**：
收到一個值後會繼續等下一個，所以通知方發完必須 `close()`，canceller 才會跨過迴圈執行到
`stop.close()`：

```no
stop chan[i64] = with-cap(0)
awake chan[i64] = with-cap(1)

worker = (stop chan[i64]) {
    <- stop: { ok -> { }  nil -> print('cancel')  err -> { } }
}

canceller = (awake chan[i64], stop chan[i64]) {
    <- awake: { ok -> { }  nil -> { }  err -> { } }   ; 等通知，收到後還要等 close 才會離開迴圈
    stop.close()                                       ; 這一句才是取消信號
}

notify = (awake chan[i64]) {
    awake <- 1
    awake.close()                                      ; 讓 canceller 的接收迴圈走到 nil
}

awy {
    co worker(stop)
    co canceller(awake, stop)
    co notify(awake)
}
; 輸出 cancel
```

若只是想在 worker **自己的每個步驟開頭**檢查是否已請求停止，用 `cancelled()` 更直接；
但它只能自我檢查，外部無句柄可通知——「由別人發信號」的場景就走上面的通道惯例。

---
