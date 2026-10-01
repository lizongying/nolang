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

---
