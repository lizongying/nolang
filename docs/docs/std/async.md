---
sidebar_position: 4.2
---

## 異步（Async）

### async — 異步協程與取消原語

Nolang 提供協作式、單執行緒、無棧的異步協程模型：

```no
; 啟動 -async 函數為後台任務，返回不透明 task 句柄
h = run worker-async(args)

; 等待後台任務完成，返回結果
r = awy h

; 取消後台任務（協作式）
async.async-cancel(h)                    ; 設置任務 h 的取消標誌

; 協作式自我取消檢查（在異步函數內調用）
yes = async.async-cancelled()            ; 返回當前任務是否已被取消
```

> **注意：** 取消是協作式而非搶占式。長阻塞調用（如網路請求）無法被強制中斷。任務會在下一個協作檢查點（`async-cancelled()` 調用或事件迴圈下次調度）真正停止。

---
