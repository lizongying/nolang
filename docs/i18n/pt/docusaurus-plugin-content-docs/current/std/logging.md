---
sidebar_position: 3.5
---

## Registo de eventos (logging)

### log — registo com níveis

```no
LEVEL-DEBUG = 0
LEVEL-INFO = 1
LEVEL-WARN = 2
LEVEL-ERROR = 3
LEVEL-FATAL = 4

log.set-level(lvl); define o nível mínimo de registo
log.debug(msg)
log.info(msg)
log.warn(msg)
log.error(msg)
log.fatal(msg, code); regista e termina com o código de estado indicado
```

### Formato de saída

Combina os bits de formato com `set-format` (por predefinição apenas `F-LEVEL`, próximo da saída antiga):

```no
F-TIME = 1; timestamp YYYY-MM-DD HH:MM:SS (UTC)
F-LEVEL = 2; etiqueta de nível [INFO] / [ERROR]
F-LOC = 4; local da chamada [ficheiro:linha:coluna]

log.set-format(log.F-TIME | log.F-LEVEL | log.F-LOC)
log.info('server started'); 2026-.. ..:.. [INFO] app.no:7:5 server started
```

O "ficheiro:linha:coluna" de `F-LOC` é injetado automaticamente pelo compilador no local da chamada através de `#{track-caller}`,
sem necessidade de passar argumentos manualmente; `debug / info / warn / error / fatal` têm todos um slot final `loc str`
(sem valor predefinido, que o chamador pode omitir; se for passado explicitamente, o valor passado prevalece, para permitir que wrappers reencaminhem a sua própria localização).

### Destino da saída (stderr / ficheiro)

Por predefinição, os registos vão para a saída de erro padrão (fd=2). Podes mudar para um ficheiro ou para qualquer `io.writer`:

```no
#{fmt-skip}
log.open-file(path)                    ; abre o ficheiro em modo de acrescentar e define como destino, retorna ?bool
log.set-output-fd(fd)                  ; muda para um fd personalizado (o chamador abre/fecha)
log.set-writer(w io.writer)            ; muda para um io.writer (usa o seu fd)
cur = log.get-output-fd()              ; retorna o fd do destino de saída atual
w = log.get-writer()                   ; retorna o io.writer correspondente ao destino de saída atual
```

```no
log.set-format(log.F-TIME | log.F-LEVEL | log.F-LOC)
log.open-file('app.log'); os registos seguintes são acrescentados a app.log
log.error('disk full'); [ERROR] [main.no:12:5] disk full → app.log
```

---
