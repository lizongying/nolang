---
sidebar_position: 3.4
---

## Время и дата

### time — работа со временем

```no
sec = time.now-s(); текущая метка времени Unix (секунды)
ms = time.now-ms(); текущая метка времени (миллисекунды)
us = time.now-us(); текущая метка времени (микросекунды)
ns = time.now-ns(); текущая метка времени (наносекунды)
sec = time.since-s(start); прошедшее время с start (секунды)
ms = time.since-ms(start); прошедшее время с start (миллисекунды)
us = time.since-us(start); прошедшее время с start (микросекунды)
time.sleep-s(sec); ожидание (секунды)
time.sleep-ms(ms); ожидание (миллисекунды)
time.sleep-us(us); ожидание (микросекунды)
time.sleep-ns(ns); ожидание (наносекунды)
d = time.duration-between(start, end); прошедшее время (секунды)
d = time.duration-ms-between(s, e); прошедшее время (миллисекунды)
d = time.duration-us-between(s, e); прошедшее время (микросекунды)

; Операции с датой
yes = time.is-leap(year); является ли год високосным
days = time.days-in-month-fn(y, m); число дней в месяце
d = time.unix-to-date(ts); преобразование метки времени Unix в структуру date
ts = time.date-to-unix(d); преобразование структуры date в метку времени Unix
d = time.now-date(); получить текущую структуру date
s = time.format-date(d); форматирование date в строку
s = time.format-time-str(ts); форматирование метки времени в строку времени
ts = time.parse-date(s); разбор строки даты (?i64)
s = time.format-duration(sec); форматирование длительности в читаемую строку

; Структура timer
t = timer {}
t.start(); начать отсчёт
t.stop(); остановить отсчёт
us = t.elapsed-us(); прошедшие микросекунды
ms = t.elapsed-ms(); прошедшие миллисекунды
s = t.elapsed-s(); прошедшие секунды
```

---
