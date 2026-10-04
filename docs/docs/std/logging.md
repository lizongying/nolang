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

### 輸出格式

用 `set-format` 組合格式位（默認僅 `F-LEVEL`，貼近舊輸出）：

```no
F-TIME  = 1                            ; 時間戳 YYYY-MM-DD HH:MM:SS（UTC）
F-LEVEL = 2                            ; [INFO] / [ERROR] 等級標籤
F-LOC   = 4                            ; 呼叫點 [檔案:行:列]

log.set-format(log.F-TIME | log.F-LEVEL | log.F-LOC)
log.info('server started')             ; 2026-.. ..:.. [INFO] app.no:7:5 server started
```

`F-LOC` 的「檔案:行:列」由編譯器透過 `#{track-caller}` 在呼叫點自動注入，
無需手動傳參；`debug / info / warn / error / fatal` 末位都有一個可選的
`loc str = ''` 位置槽（顯式傳入則以傳入值為準，供封裝轉發使用）。

### 輸出目標（stderr / 檔案）

默認寫入標準錯誤（fd=2）。可切換到檔案或任意 `io.writer`：

```no
log.open-file(path)                    ; 以追加模式開啟檔案並設為輸出目標，回傳 ?bool
log.set-output-fd(fd)                  ; 切換到自訂 fd（呼叫端負責開啟/關閉）
log.set-writer(w io.writer)            ; 切換到 io.writer（取其 fd）
cur = log.get-output-fd()              ; 回傳目前輸出目標 fd
w = log.get-writer()                   ; 回傳目前輸出目標對應的 io.writer
```

```no
log.set-format(log.F-TIME | log.F-LEVEL | log.F-LOC)
log.open-file('app.log')               ; 之後的日誌追加寫入 app.log
log.error('disk full')                 ; [ERROR] [main.no:12:5] disk full → app.log
```

---
