---
sidebar_position: 4.2
---

## 異步（Async）

Nolang 只有一種表面並發語法：`awy { ... }` 協程組，組內用 `co <call>` 衍生任務。
`run` 關鍵字已移除（現在是普通識別符）；`awy <handle>` 與語句位置裸塊協程組都已廢除；
**組外 `co` 是編譯錯誤**（`E_CO_OUTSIDE_GROUP`）。

### 協程組

```no
worker = (n i64) (r i64) {
    #{overflow=wrap}
    r = n + 1
}

; 並發：兩個任務一起跑，組尾統一等待
awy {
    r1 = co worker(1)
    r2 = co worker(2)
}

; 順序：r2 依賴 r1，退化為順序執行
awy {
    r3 = co worker(r1)
}
```

`r1` / `r2` / `r3` 都是結果值，不是 task 句柄。詳細見 `lang/syntax.md`「異步編程」。

### 無色 async：`co`

`co` 是無色（colorless）語法：你寫未染色的函式名 `worker`，編譯器自動單態化出
`worker-async` 變體並在底層以染色無棧協程執行。不要在應用碼手寫 `-async` 後綴。

規則：

- **單態化**：`co worker(1)` 會自動生成 `worker-async`（複製 `worker` 的本體、改名）。若 `worker` 同時也被同步呼叫，兩者共存。
- **傳遞性**：若某函式內部也有 `awy { c = co child(n) }`，該函式本身也會被單態為 `-async`。
- **底層 lowering**（不是使用者語法）：每條 `co` 變成 `OpRun`，組尾插入 `OpAwait`。

### 取消

`cancelled()` 可在協程函式內作協作式自我檢查。`cancel(h)` 是底層句柄操作，
新語法不暴露 task 句柄，應用碼不使用。

> **注意：** 取消是協作式而非搶占式。長阻塞調用（如網路請求）無法被強制中斷；
> 任務會在下一個協作檢查點（`cancelled()` 調用或事件迴圈下次調度）真正停止。

---
