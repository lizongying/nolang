---
sidebar_position: 3.5
---

## Логирование

### log — журналирование по уровням

```no
LEVEL-DEBUG = 0
LEVEL-INFO = 1
LEVEL-WARN = 2
LEVEL-ERROR = 3
LEVEL-FATAL = 4

log.set-level(lvl); задать минимальный уровень логирования
log.debug(msg)
log.info(msg)
log.warn(msg)
log.error(msg)
log.fatal(msg, code); записать и завершить с указанным кодом состояния
```

### Формат вывода

Комбинируйте биты формата с помощью `set-format` (по умолчанию только `F-LEVEL`, близко к старому выводу):

```no
F-TIME = 1; метка времени YYYY-MM-DD HH:MM:SS (UTC)
F-LEVEL = 2; метка уровня [INFO] / [ERROR]
F-LOC = 4; точка вызова [файл:строка:столбец]

log.set-format(log.F-TIME | log.F-LEVEL | log.F-LOC)
log.info('server started'); 2026-.. ..:.. [INFO] app.no:7:5 server started
```

«Файл:строка:столбец» для `F-LOC` автоматически внедряется компилятором в точке вызова через `#{track-caller}` — передавать аргументы вручную не требуется. У каждой из функций `debug / info / warn / error / fatal` в конце есть слот `loc str` для местоположения (без значения по умолчанию; вызывающая сторона может его опустить, а при явной передаче приоритет имеет переданное значение — это используется обёртками для проброса своего местоположения).

### Целевой вывод (stderr / файл)

По умолчанию запись идёт в стандартный поток ошибок (fd=2). Можно переключиться на файл или произвольный `io.writer`:

```no
#{fmt-skip}
log.open-file(path)                    ; открыть файл в режиме добавления и сделать его целевым выводом, возвращает ?bool
log.set-output-fd(fd)                  ; переключиться на пользовательский fd (вызывающая сторона открывает/закрывает его)
log.set-writer(w io.writer)            ; переключиться на io.writer (используется его fd)
cur = log.get-output-fd()              ; вернуть fd текущего целевого вывода
w = log.get-writer()                   ; вернуть io.writer, соответствующий текущему целевому выводу
```

```no
log.set-format(log.F-TIME | log.F-LEVEL | log.F-LOC)
log.open-file('app.log'); последующие записи добавляются в app.log
log.error('disk full'); [ERROR] [main.no:12:5] disk full → app.log
```

---
