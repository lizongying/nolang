---
sidebar_position: 3.7
---

## 데이터베이스

### database/sql — 데이터베이스 접근 인터페이스

데이터베이스 연결, 쿼리, 준비된 문장(statement)에 대한 표준 인터페이스를 정의하며, 구체적인 드라이버가 구현합니다:

```no
; 執行結果
result {
    last-id i64
    affected i64
}

; 連線介面（enter/leave 自動管理）
db {
    enter
    leave
    close() (ok bool)
    exec(sql str) (r result)
    query(sql str) (rs rows)
    prepare(sql str) (s stmt)
}

; 結果集介面
rows {
    enter
    leave
    next() (ok bool)
    scan-int(col i64) (v i64)
    scan-str(col i64) (v str)
    scan-float(col i64) (v f64)
    close() (ok bool)
}

; 迭代下一行
; 讀取整數
; 讀取字串
; 讀取浮點數

; 預編譯陳述式介面
stmt {
    enter
    leave
    bind-int(idx i64, v i64) (ok bool)
    bind-str(idx i64, v str) (ok bool)
    bind-bool(idx i64, v bool) (ok bool)
    exec() (r result)
    query() (rs rows)
    close() (ok bool)
}
```

---
