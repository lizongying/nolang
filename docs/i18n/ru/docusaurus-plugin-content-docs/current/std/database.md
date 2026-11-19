---
sidebar_position: 3.7
---

## База данных

### database/sql — интерфейс доступа к базе данных

Определяет стандартные интерфейсы для подключения к базе данных, запросов и подготовленных выражений; реализуются конкретными драйверами:

```no
; Результат выполнения
result {
    last-id i64
    affected i64
}

; Интерфейс подключения (enter/leave управляются автоматически)
db {
    enter
    leave
    close() (ok bool)
    exec(sql str) (r result)
    query(sql str) (rs rows)
    prepare(sql str) (s stmt)
}

; Интерфейс набора результатов
rows {
    enter
    leave
    next() (ok bool)
    scan-int(col i64) (v i64)
    scan-str(col i64) (v str)
    scan-float(col i64) (v f64)
    close() (ok bool)
}

; Переход к следующей строке
; Чтение целого числа
; Чтение строки
; Чтение числа с плавающей точкой

; Интерфейс подготовленного выражения
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
