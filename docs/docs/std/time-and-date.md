---
sidebar_position: 3.4
---

## 時間與日期

### time — 時間操作

```no
sec = time.now-s()                   ; 目前 Unix 時間戳（秒）
ms = time.now-ms()                   ; 目前時間戳（毫秒）
us = time.now-us()                   ; 目前時間戳（微秒）
out = time.format-time(t, fmt)        ; 格式化時間
time.sleep-ms(ms)                    ; 睡眠（毫秒）
time.sleep-us(us)                    ; 睡眠（微秒）
d = time.duration-between(start, end) ; 耗時（秒）
d = time.duration-ms-between(s, e)    ; 耗時（毫秒）
```

---
