---
sidebar_position: 3.7
---

## Database

### database/sql — Database Access Interface

Defines standard interfaces for database connections, queries, and prepared statements, implemented by concrete drivers:

```no
; Execution result
result {
    last-id i64
    affected i64
}

; Connection interface (enter/leave auto-managed)
db enter, leave {
    close() (ok bool)
    exec(sql str) (r result)
    query(sql str) (rs rows)
    prepare(sql str) (s stmt)
}

; Result set interface
rows enter, leave {
    next() (ok bool)                    ; Iterate to next row
    scan-int(col i64) (v i64)           ; Read integer
    scan-str(col i64) (v str)           ; Read string
    scan-float(col i64) (v f64)         ; Read float
    close() (ok bool)
}

; Prepared statement interface
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
