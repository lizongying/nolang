---
sidebar_position: 3.5
---

## 日誌

### log — 分級日誌

```no
LEVEL-DEBUG = 0
LEVEL-INFO  = 1
LEVEL-WARN  = 2
LEVEL-ERROR = 3
LEVEL-FATAL = 4

log.set-level(lvl)                     ; 設定最低日誌等級
log.debug(msg)
log.info(msg)
log.warn(msg)
log.error(msg)
log.fatal(msg, code)                   ; 記錄並以狀態碼退出
```

---
