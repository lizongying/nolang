---
sidebar_position: 3.4
---

## 时间与日期

### time — 时间操作

```no
sec = time.now-s(); 目前 Unix 时间戳（秒）
ms = time.now-ms(); 目前时间戳（毫秒）
us = time.now-us(); 目前时间戳（微秒）
ns = time.now-ns(); 目前时间戳（奈秒）
sec = time.since-s(start); 自 start 起耗时（秒）
ms = time.since-ms(start); 自 start 起耗时（毫秒）
us = time.since-us(start); 自 start 起耗时（微秒）
time.sleep-s(sec); 睡眠（秒）
time.sleep-ms(ms); 睡眠（毫秒）
time.sleep-us(us); 睡眠（微秒）
time.sleep-ns(ns); 睡眠（奈秒）
d = time.duration-between(start, end); 耗时（秒）
d = time.duration-ms-between(s, e); 耗时（毫秒）
d = time.duration-us-between(s, e); 耗时（微秒）

; 日期操作
yes = time.is-leap(year); 是否闰年
days = time.days-in-month-fn(y, m); 该月天数
d = time.unix-to-date(ts); Unix 时间戳转 date 结构
ts = time.date-to-unix(d); date 结构转 Unix 时间戳
d = time.now-date(); 取得目前 date 结构
s = time.format-date(d); 格式化 date 为字串
s = time.format-time-str(ts); 格式化时间戳为时间字串
ts = time.parse-date(s); 解析日期字串（?i64）
s = time.format-duration(sec); 格式化时长为可读字串

; timer 结构
t = timer {}
t.start(); 开始计时
t.stop(); 停止计时
us = t.elapsed-us(); 经过微秒
ms = t.elapsed-ms(); 经过毫秒
s = t.elapsed-s(); 经过秒
```

---
