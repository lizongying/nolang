---
sidebar_position: 3.4
---

## Hora e data

### time — operações de tempo

```no
sec = time.now-s(); timestamp Unix atual (segundos)
ms = time.now-ms(); timestamp atual (milissegundos)
us = time.now-us(); timestamp atual (microsegundos)
ns = time.now-ns(); timestamp atual (nanossegundos)
sec = time.since-s(start); tempo decorrido desde start (segundos)
ms = time.since-ms(start); tempo decorrido desde start (milissegundos)
us = time.since-us(start); tempo decorrido desde start (microsegundos)
time.sleep-s(sec); dormir (segundos)
time.sleep-ms(ms); dormir (milissegundos)
time.sleep-us(us); dormir (microsegundos)
time.sleep-ns(ns); dormir (nanossegundos)
d = time.duration-between(start, end); duração (segundos)
d = time.duration-ms-between(s, e); duração (milissegundos)
d = time.duration-us-between(s, e); duração (microsegundos)

; Operações de data
yes = time.is-leap(year); é ano bissexto
days = time.days-in-month-fn(y, m); número de dias do mês
d = time.unix-to-date(ts); converte timestamp Unix em struct date
ts = time.date-to-unix(d); converte struct date em timestamp Unix
d = time.now-date(); obtém a struct date atual
s = time.format-date(d); formata uma data como string
s = time.format-time-str(ts); formata um timestamp como string de hora
ts = time.parse-date(s); analisa uma string de data（?i64）
s = time.format-duration(sec); formata uma duração como string legível

; Struct timer
t = timer {}
t.start(); iniciar cronómetro
t.stop(); parar cronómetro
us = t.elapsed-us(); microsegundos decorridos
ms = t.elapsed-ms(); milissegundos decorridos
s = t.elapsed-s(); segundos decorridos
```

---
