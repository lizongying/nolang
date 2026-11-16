# time, log

> 视图切分自原 `nolang-std/SKILL.md`，正文未改写；索引与加载判据见 [`../m20-std.md`](../m20-std.md)。

### Time & Date

#### time — Time Operations

```no
sec = time.now-s(); Current Unix timestamp (seconds)
ms = time.now-ms(); Current timestamp (milliseconds)
us = time.now-us(); Current timestamp (microseconds)
ns = time.now-ns(); Current timestamp (nanoseconds)
sec = time.since-s(start); Elapsed since start (seconds)
ms = time.since-ms(start); Elapsed since start (milliseconds)
us = time.since-us(start); Elapsed since start (microseconds)
time.sleep-s(sec); Sleep (seconds)
time.sleep-ms(ms); Sleep (milliseconds)
time.sleep-us(us); Sleep (microseconds)
time.sleep-ns(ns); Sleep (nanoseconds)
d = time.duration-between(start, end); Elapsed (seconds)
d = time.duration-ms-between(s, e); Elapsed (milliseconds)
d = time.duration-us-between(s, e); Elapsed (microseconds)

; Date operations
yes = time.is-leap(year); Check if year is a leap year
days = time.days-in-month-fn(year, month); Get days in month
d = time.unix-to-date(ts); Convert Unix timestamp to date struct
ts = time.date-to-unix(d); Date struct to Unix timestamp
d = time.now-date(); Get current date struct
s = time.format-date(d); Format date struct to string
s = time.format-time-str(ts); Format Unix timestamp to time string
ts = time.parse-date(s); Parse date string (?i64)
s = time.format-duration(sec); Format duration to human-readable string

; Timer struct
t = timer {}
t.start(); Start timer
t.stop(); Stop timer
us = t.elapsed-us(); Elapsed microseconds
ms = t.elapsed-ms(); Elapsed milliseconds
s = t.elapsed-s(); Elapsed seconds

; time struct (absolute point in time: Unix sec + nsec, UTC)
t = time.unix-to-time(ts); Build time from Unix seconds (nsec=0)
nt = time.now-time(); Current time point (from now-ns)
ts = time.duration-time(start, end); Difference in seconds (end - start)
ts = time.since-time(start); Seconds elapsed from start to now
tp = time.parse-time(s); Parse date string (?time)
sec = t.unix(); Unix seconds
ms = t.unix-ms(); Unix milliseconds
d = t.date(); Convert to date struct
s = t.format(); Format as 'YYYY-MM-DD HH:MM:SS'
t2 = t.add-seconds(n); Return a new time shifted by n seconds
diff = t.sub(other); Seconds difference (t - other)
```

---

### Logging

#### log — Leveled Logging

```no
LEVEL-DEBUG = 0
LEVEL-INFO = 1
LEVEL-WARN = 2
LEVEL-ERROR = 3
LEVEL-FATAL = 4

log.set-level(lvl); Set minimum log level
log.debug(msg)
log.info(msg)
log.warn(msg)
log.error(msg)
log.fatal(msg, code); Log and exit with status code
```

Format bits (combine with `set-format`; default is `F-LEVEL` only):

```no
F-TIME = 1; Timestamp YYYY-MM-DD HH:MM:SS (UTC)
F-LEVEL = 2; [INFO] / [ERROR] level tag
F-LOC = 4; Call site [file:line:col]
log.set-format(log.F-TIME | log.F-LEVEL | log.F-LOC)
```

Output target (default stderr fd=2). The API speaks `io.writer`, but the persisted
state is a scalar fd (in std merged modules, struct globals do not retain writes —
only scalar globals are mutable), so a writer is reconstructed from the fd at emit time:

```no
log.open-file(path)                    ; Append-open a file and set as target; returns ?bool
log.set-output-fd(fd)                  ; Switch to a custom fd (caller opens/closes)
log.set-writer(w io.writer)            ; Switch to an io.writer (stores its fd)
cur = log.get-output-fd()              ; Current output-target fd
w = log.get-writer()                   ; Current output target as an io.writer
```

> The `F-LOC` "file:line:col" is auto-injected by the compiler at each call site
> via the `#{track-caller}` function annotation: `debug/info/warn/error/fatal`
> declare a trailing `loc str` slot (no default value — parameter defaults do not
> exist); when the caller omits it, the MIR lowerer bakes a compile-time
> `"file:line:col"` string from the call node.
> Passing `loc` explicitly overrides the injection (for wrapper forwarding).
> Any user function may use `#{track-caller}` the same way.

---
