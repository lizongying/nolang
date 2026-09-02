---
sidebar_position: 3.7
---

## 資料庫

### database/sql — 資料庫存取介面

定義資料庫連線、查詢、預編譯陳述式的標準介面，由具體驅動實現：

```no
; 執行結果
result {
    last-id i64
    affected i64
}

; 連線介面（enter/leave 自動管理）
db enter, leave {
    close() (ok bool)
    exec(sql str) (r result)
    query(sql str) (rs rows)
    prepare(sql str) (s stmt)
}

; 結果集介面
rows enter, leave {
    next() (ok bool)                    ; 迭代下一行
    scan-int(col i64) (v i64)           ; 讀取整數
    scan-str(col i64) (v str)           ; 讀取字串
    scan-float(col i64) (v f64)         ; 讀取浮點數
    close() (ok bool)
}

; 預編譯陳述式介面
stmt enter, leave {
    bind-int(idx i64, v i64) (ok bool)
    bind-str(idx i64, v str) (ok bool)
    bind-bool(idx i64, v bool) (ok bool)
    exec() (r result)
    query() (rs rows)
    close() (ok bool)
}
```

---
