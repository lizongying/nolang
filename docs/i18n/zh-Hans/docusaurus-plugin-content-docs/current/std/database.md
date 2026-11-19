---
sidebar_position: 3.7
---

## 资料库

### database/sql — 资料库存取介面

定义资料库连线、查询、预编译陈述式的标准介面，由具体驱动实现：

```no
; 执行结果
result {
    last-id i64
    affected i64
}

; 连线介面（enter/leave 自动管理）
db {
    enter
    leave
    close() (ok bool)
    exec(sql str) (r result)
    query(sql str) (rs rows)
    prepare(sql str) (s stmt)
}

; 结果集介面
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
; 读取整数
; 读取字串
; 读取浮点数

; 预编译陈述式介面
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
