---
sidebar_position: 3.4
---

## Time and Date

### time — Time Operations

```no
sec = time.now-s()                   ; Current Unix timestamp (seconds)
ms = time.now-ms()                   ; Current timestamp (milliseconds)
us = time.now-us()                   ; Current timestamp (microseconds)
ns = time.now-ns()                   ; Current timestamp (nanoseconds)
sec = time.since-s(start)            ; Elapsed time since start (seconds)
ms = time.since-ms(start)            ; Elapsed time since start (milliseconds)
us = time.since-us(start)            ; Elapsed time since start (microseconds)
time.sleep-s(sec)                    ; Sleep (seconds)
time.sleep-ms(ms)                    ; Sleep (milliseconds)
time.sleep-us(us)                    ; Sleep (microseconds)
time.sleep-ns(ns)                    ; Sleep (nanoseconds)
d = time.duration-between(start, end) ; Elapsed time (seconds)
d = time.duration-ms-between(s, e)    ; Elapsed time (milliseconds)
d = time.duration-us-between(s, e)    ; Elapsed time (microseconds)

; Date operations
yes = time.is-leap(year)              ; Whether the year is a leap year
days = time.days-in-month-fn(y, m)    ; Number of days in the month
d = time.unix-to-date(ts)             ; Convert a Unix timestamp to a date struct
ts = time.date-to-unix(d)             ; Convert a date struct to a Unix timestamp
d = time.now-date()                   ; Get the current date struct
s = time.format-date(d)               ; Format a date as a string
s = time.format-time-str(ts)          ; Format a timestamp as a time string
ts = time.parse-date(s)                ; Parse a date string (?i64)
s = time.format-duration(sec)         ; Format a duration as a readable string

; timer struct
t = timer{}
t.start()                             ; Start timing
t.stop()                              ; Stop timing
us = t.elapsed-us()                   ; Elapsed microseconds
ms = t.elapsed-ms()                   ; Elapsed milliseconds
s = t.elapsed-s()                     ; Elapsed seconds
```

---
