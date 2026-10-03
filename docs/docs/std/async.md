---
sidebar_position: 4.2
---

## 異步（Async）

> ⚠️ **已廢棄（手寫原語）**：`run` / `awy` / `async-cancel` / `async-cancelled` 已廢棄，不推薦在應用程式碼中手寫。原因：手動管理 task 句柄不安全——未 await 的任務會洩漏參數緩衝區、複製句柄後重複 await 會崩潰，而協作式取消無法強制中斷長阻塞調用。請改用**協程組**（見下方），語法更簡單且自動管理句柄與取消。這些原語僅保留作為底層參考。

### 協程組（coroutine group）— 推薦

語句位置的裸塊 `{ ... }` 是協程組，塊內的 `-async` 調用默認 spawn，遇到依賴則退化成 await：

```no
; 並發：兩個任務一起跑
{
    r1 = worker-async(1)
    r2 = worker-async(2)
}

; 順序：r2 依賴 r1，退化成 await
{
    r1 = worker-async(1)
    r2 = worker-async(r1)
}
```

詳細見 `lang/syntax.md`「協程組（coroutine group）」。

### 無色 async：`co` 關鍵字 — 推薦

`co` 是建立在協程組之上的**無色（colorless）**語法：你寫的是未染色的函式名 `worker`，編譯器自動單態化出 `worker-async` 變體並在底層以染色無棧協程執行。你不需要手寫 `-async` 後綴，也不需要手寫 `run` / `awy`。

```no
worker = (n i64) (r i64) {
    #{overflow=wrap}
    r = n + 1
}
main = () {
    r1 i64
    r2 i64
    {
        r1 = co worker(1)   ; 自動單態 worker-async 並 spawn
        r2 = co worker(2)   ; 與 r1 並發執行
    }
    print(r1.to-str() + ' ' + r2.to-str())
}
```

規則：

- **單態化**：`co worker(1)` 會自動生成 `worker-async`（複製 `worker` 的本體、改名）。若 `worker` 同時也被同步呼叫，則 `worker` 與 `worker-async` 兩個方法共存。
- **傳遞性**：若某函式內部呼叫了異步函式（例如 `worker` 內 `co child(...)`），則 `worker` 也會被單態為 `worker-async`，異步染色沿呼叫鏈向上傳遞。
- **底層仍是染色無棧協程**：`co` 只是語法糖，等價於 `run worker-async(1); r1 = awy <handle>`；在協程組內則由協程組統一調度 spawn / await 以獲得並發。

診斷開關：`NOLANG_ASYNC_GO=0` 可停用單態化（僅作對照）。

### async — 手寫原語（已廢棄，僅作底層參考）

Nolang 提供協作式、單執行緒、無棧的異步協程模型：

```no
; 啟動 -async 函數為後台任務，返回不透明 task 句柄
h = run worker-async(args)

; 等待後台任務完成，返回結果
r = awy h

; 取消後台任務（協作式，已廢棄）
async-cancel(h)                        ; 設置任務 h 的取消標誌

; 協作式自我取消檢查（在異步函數內調用，已廢棄）
yes = async-cancelled()                ; 返回當前任務是否已被取消
```

> **注意：** 取消是協作式而非搶占式。長阻塞調用（如網路請求）無法被強制中斷。任務會在下一個協作檢查點（`async-cancelled()` 調用或事件迴圈下次調度）真正停止。

---
