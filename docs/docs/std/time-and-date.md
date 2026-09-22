---
sidebar_position: 3.4
---

## 時間與日期

### time — 時間操作

```no
sec = time.now-s()                   ; 目前 Unix 時間戳（秒）
ms = time.now-ms()                   ; 目前時間戳（毫秒）
us = time.now-us()                   ; 目前時間戳（微秒）
ns = time.now-ns()                   ; 目前時間戳（奈秒）
sec = time.since-s(start)            ; 自 start 起耗時（秒）
ms = time.since-ms(start)            ; 自 start 起耗時（毫秒）
us = time.since-us(start)            ; 自 start 起耗時（微秒）
time.sleep-s(sec)                    ; 睡眠（秒）
time.sleep-ms(ms)                    ; 睡眠（毫秒）
time.sleep-us(us)                    ; 睡眠（微秒）
time.sleep-ns(ns)                    ; 睡眠（奈秒）
d = time.duration-between(start, end) ; 耗時（秒）
d = time.duration-ms-between(s, e)    ; 耗時（毫秒）
d = time.duration-us-between(s, e)    ; 耗時（微秒）

; 日期操作
yes = time.is-leap(year)              ; 是否閏年
days = time.days-in-month-fn(y, m)    ; 該月天數
d = time.unix-to-date(ts)             ; Unix 時間戳轉 date 結構
ts = time.date-to-unix(d)             ; date 結構轉 Unix 時間戳
d = time.now-date()                   ; 取得目前 date 結構
s = time.format-date(d)               ; 格式化 date 為字串
s = time.format-time-str(ts)          ; 格式化時間戳為時間字串
ts = time.parse-date(s)                ; 解析日期字串（?i64）
s = time.format-duration(sec)         ; 格式化時長為可讀字串

; timer 結構
t = timer{}
t.start()                             ; 開始計時
t.stop()                              ; 停止計時
us = t.elapsed-us()                   ; 經過微秒
ms = t.elapsed-ms()                   ; 經過毫秒
s = t.elapsed-s()                     ; 經過秒
```

---
