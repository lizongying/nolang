---
sidebar_position: 3.7
---

## Base de datos

### database/sql — Interfaz de acceso a bases de datos

Define interfaces estándar para conexiones, consultas e instrucciones preparadas de bases de datos, implementadas por controladores concretos:

```no
; Resultado de ejecución
result {
    last-id i64
    affected i64
}

; Interfaz de conexión (enter/leave gestionados automáticamente)
db {
    enter
    leave
    close() (ok bool)
    exec(sql str) (r result)
    query(sql str) (rs rows)
    prepare(sql str) (s stmt)
}

; Interfaz de conjunto de resultados
rows {
    enter
    leave
    next() (ok bool)
    scan-int(col i64) (v i64)
    scan-str(col i64) (v str)
    scan-float(col i64) (v f64)
    close() (ok bool)
}

; Iterar a la siguiente fila
; Leer entero
; Leer cadena
; Leer flotante

; Interfaz de instrucción preparada
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
