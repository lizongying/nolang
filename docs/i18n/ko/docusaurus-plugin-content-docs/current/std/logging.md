---
sidebar_position: 3.5
---

## 로그

### log — 레벨별 로그

```no
LEVEL-DEBUG = 0
LEVEL-INFO = 1
LEVEL-WARN = 2
LEVEL-ERROR = 3
LEVEL-FATAL = 4

log.set-level(lvl); 設定最低日誌等級
log.debug(msg)
log.info(msg)
log.warn(msg)
log.error(msg)
log.fatal(msg, code); 記錄並以狀態碼退出
```

### 출력 형식

`set-format`으로 형식 비트를 조합합니다(기본값은 `F-LEVEL`만 있으며, 기존 출력에 가깝습니다):

```no
F-TIME = 1; 時間戳 YYYY-MM-DD HH:MM:SS（UTC）
F-LEVEL = 2; [INFO] / [ERROR] 等級標籤
F-LOC = 4; 呼叫點 [檔案:行:列]

log.set-format(log.F-TIME | log.F-LEVEL | log.F-LOC)
log.info('server started'); 2026-.. ..:.. [INFO] app.no:7:5 server started
```

`F-LOC`의 "파일:행:열"은 컴파일러가 `#{track-caller}`를 통해 호출 지점에 자동으로 주입하므로 수동으로 인자를 전달할 필요가 없습니다. `debug / info / warn / error / fatal`의 마지막에는 모두 `loc str` 위치 슬롯이 하나씩 있습니다(기본값이 없어 호출자가 생략할 수 있으며, 명시적으로 전달하면 전달된 값이 우선합니다. 래퍼에서 전달할 때 사용합니다).

### 출력 대상 (stderr / 파일)

기본적으로 표준 오류(fd=2)에 기록합니다. 파일이나 임의의 `io.writer`로 전환할 수 있습니다:

```no
#{fmt-skip}
log.open-file(path)                    ; 以追加模式開啟檔案並設為輸出目標，回傳 ?bool
log.set-output-fd(fd)                  ; 切換到自訂 fd（呼叫端負責開啟/關閉）
log.set-writer(w io.writer)            ; 切換到 io.writer（取其 fd）
cur = log.get-output-fd()              ; 回傳目前輸出目標 fd
w = log.get-writer()                   ; 回傳目前輸出目標對應的 io.writer
```

```no
log.set-format(log.F-TIME | log.F-LEVEL | log.F-LOC)
log.open-file('app.log'); 之後的日誌追加寫入 app.log
log.error('disk full'); [ERROR] [main.no:12:5] disk full → app.log
```

---
