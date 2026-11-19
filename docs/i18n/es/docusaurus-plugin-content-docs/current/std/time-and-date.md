---
sidebar_position: 3.4
---

## Hora y fecha

### time — Operaciones con el tiempo

```no
sec = time.now-s(); Marca de tiempo Unix actual (segundos)
ms = time.now-ms(); Marca de tiempo actual (milisegundos)
us = time.now-us(); Marca de tiempo actual (microsegundos)
ns = time.now-ns(); Marca de tiempo actual (nanosegundos)
sec = time.since-s(start); Tiempo transcurrido desde start (segundos)
ms = time.since-ms(start); Tiempo transcurrido desde start (milisegundos)
us = time.since-us(start); Tiempo transcurrido desde start (microsegundos)
time.sleep-s(sec); Dormir (segundos)
time.sleep-ms(ms); Dormir (milisegundos)
time.sleep-us(us); Dormir (microsegundos)
time.sleep-ns(ns); Dormir (nanosegundos)
d = time.duration-between(start, end); Tiempo transcurrido (segundos)
d = time.duration-ms-between(s, e); Tiempo transcurrido (milisegundos)
d = time.duration-us-between(s, e); Tiempo transcurrido (microsegundos)

; Operaciones con fechas
yes = time.is-leap(year); Si es año bisiesto
days = time.days-in-month-fn(y, m); Número de días del mes
d = time.unix-to-date(ts); Convertir marca de tiempo Unix a estructura date
ts = time.date-to-unix(d); Convertir estructura date a marca de tiempo Unix
d = time.now-date(); Obtener la estructura date actual
s = time.format-date(d); Formatear date como cadena
s = time.format-time-str(ts); Formatear marca de tiempo como cadena de hora
ts = time.parse-date(s); Analizar cadena de fecha (?i64)
s = time.format-duration(sec); Formatear duración como cadena legible

; Estructura timer
t = timer {}
t.start(); Iniciar cronómetro
t.stop(); Detener cronómetro
us = t.elapsed-us(); Microsegundos transcurridos
ms = t.elapsed-ms(); Milisegundos transcurridos
s = t.elapsed-s(); Segundos transcurridos
```

---
