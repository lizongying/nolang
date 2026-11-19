---
sidebar_position: 3.5
---

## 日志

### log — 分级日志

```no
LEVEL-DEBUG = 0
LEVEL-INFO = 1
LEVEL-WARN = 2
LEVEL-ERROR = 3
LEVEL-FATAL = 4

log.set-level(lvl); 设定最低日志等级
log.debug(msg)
log.info(msg)
log.warn(msg)
log.error(msg)
log.fatal(msg, code); 记录并以状态码退出
```

### 输出格式

用 `set-format` 组合格式位（默认仅 `F-LEVEL`，贴近旧输出）：

```no
F-TIME = 1; 时间戳 YYYY-MM-DD HH:MM:SS（UTC）
F-LEVEL = 2; [INFO] / [ERROR] 等级标签
F-LOC = 4; 呼叫点 [档案:行:列]

log.set-format(log.F-TIME | log.F-LEVEL | log.F-LOC)
log.info('server started'); 2026-.. ..:.. [INFO] app.no:7:5 server started
```

`F-LOC` 的「档案:行:列」由编译器透过 `#{track-caller}` 在呼叫点自动注入，
无需手动传参；`debug / info / warn / error / fatal` 末位都有一个 `loc str`
位置槽（无默认值，呼叫方可省略；显式传入则以传入值为准，供封装转发使用）。

### 输出目标（stderr / 档案）

默认写入标准错误（fd=2）。可切换到档案或任意 `io.writer`：

```no
#{fmt-skip}
log.open-file(path)                    ; 以追加模式开启档案并设为输出目标，回传 ?bool
log.set-output-fd(fd)                  ; 切换到自订 fd（呼叫端负责开启/关闭）
log.set-writer(w io.writer)            ; 切换到 io.writer（取其 fd）
cur = log.get-output-fd()              ; 回传目前输出目标 fd
w = log.get-writer()                   ; 回传目前输出目标对应的 io.writer
```

```no
log.set-format(log.F-TIME | log.F-LEVEL | log.F-LOC)
log.open-file('app.log'); 之后的日志追加写入 app.log
log.error('disk full'); [ERROR] [main.no:12:5] disk full → app.log
```

---
