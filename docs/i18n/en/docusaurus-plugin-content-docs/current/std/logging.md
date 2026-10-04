---
sidebar_position: 3.5
---

## Logging

### log — Leveled Logging

```no
LEVEL-DEBUG = 0
LEVEL-INFO  = 1
LEVEL-WARN  = 2
LEVEL-ERROR = 3
LEVEL-FATAL = 4

log.set-level(lvl)                     // Set the minimum log level
log.debug(msg)
log.info(msg)
log.warn(msg)
log.error(msg)
log.fatal(msg, code)                   // Log and exit with the given status code
```

### Output format

Combine format bits with `set-format` (default is `F-LEVEL` only, close to the legacy output):

```no
F-TIME  = 1                            // Timestamp YYYY-MM-DD HH:MM:SS (UTC)
F-LEVEL = 2                            // [INFO] / [ERROR] level tag
F-LOC   = 4                            // Call site [file:line:col]

log.set-format(log.F-TIME | log.F-LEVEL | log.F-LOC)
log.info('server started')             // 2026-.. ..:.. [INFO] app.no:7:5 server started
```

The `F-LOC` "file:line:col" is injected automatically by the compiler at the call
site via `#{track-caller}` — no manual argument needed. Each of `debug / info /
warn / error / fatal` has a trailing optional `loc str = ''` slot (when passed
explicitly the given value wins, so wrappers can forward their own location).

### Output target (stderr / file)

Logs go to standard error (fd=2) by default. Switch to a file or any `io.writer`:

```no
log.open-file(path)                    // Open file in append mode and set as target; returns ?bool
log.set-output-fd(fd)                  // Switch to a custom fd (caller opens/closes it)
log.set-writer(w io.writer)            // Switch to an io.writer (uses its fd)
cur = log.get-output-fd()              // Current output-target fd
```

```no
log.set-format(log.F-TIME | log.F-LEVEL | log.F-LOC)
log.open-file('app.log')               // Subsequent lines append to app.log
log.error('disk full')                 // [ERROR] [main.no:12:5] disk full → app.log
```

---
