---
sidebar_position: 3.7
---

## Base de dados

### database/sql — interface de acesso a base de dados

Define interfaces padrão para ligações a base de dados, consultas e instruções preparadas, implementadas por controladores concretos:

```no
; Resultado da execução
result {
    last-id i64
    affected i64
}

; Interface de ligação (enter/leave geridos automaticamente)
db {
    enter
    leave
    close() (ok bool)
    exec(sql str) (r result)
    query(sql str) (rs rows)
    prepare(sql str) (s stmt)
}

; Interface de conjunto de resultados
rows {
    enter
    leave
    next() (ok bool)
    scan-int(col i64) (v i64)
    scan-str(col i64) (v str)
    scan-float(col i64) (v f64)
    close() (ok bool)
}

; Iterar para a linha seguinte
; Ler um inteiro
; Ler uma string
; Ler um número de ponto flutuante

; Interface de instrução preparada
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
