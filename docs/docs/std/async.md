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

詳見 `lang/syntax.md`「協程組（coroutine group）」。

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
