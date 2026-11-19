---
sidebar_position: 3.5
---

## Registro

### log — Registro por niveles

```no
LEVEL-DEBUG = 0
LEVEL-INFO = 1
LEVEL-WARN = 2
LEVEL-ERROR = 3
LEVEL-FATAL = 4

log.set-level(lvl); Establecer el nivel mínimo de registro
log.debug(msg)
log.info(msg)
log.warn(msg)
log.error(msg)
log.fatal(msg, code); Registrar y salir con el código de estado
```

### Formato de salida

Combine los bits de formato con `set-format` (por defecto solo `F-LEVEL`, cercano a la salida anterior):

```no
F-TIME = 1; Marca de tiempo YYYY-MM-DD HH:MM:SS (UTC)
F-LEVEL = 2; Etiqueta de nivel [INFO] / [ERROR]
F-LOC = 4; Punto de llamada [archivo:línea:columna]

log.set-format(log.F-TIME | log.F-LEVEL | log.F-LOC)
log.info('server started'); 2026-.. ..:.. [INFO] app.no:7:5 server started
```

El «archivo:línea:columna» de `F-LOC` lo inyecta automáticamente el compilador en el punto de llamada mediante `#{track-caller}`, sin necesidad de pasar argumentos manualmente; `debug / info / warn / error / fatal` tienen al final un espacio de posición `loc str` (sin valor por defecto; quien llama puede omitirlo; si se pasa explícitamente, prevalece el valor pasado, para que los envoltorios puedan reenviar su propia ubicación).

### Destino de salida (stderr / archivo)

Por defecto se escribe en la salida de error estándar (fd=2). Se puede cambiar a un archivo o a cualquier `io.writer`:

```no
#{fmt-skip}
log.open-file(path)                    ; Abrir archivo en modo de adición y establecerlo como destino de salida, devuelve ?bool
log.set-output-fd(fd)                  ; Cambiar a un fd personalizado (quien llama se encarga de abrirlo/cerrarlo)
log.set-writer(w io.writer)            ; Cambiar a un io.writer (usa su fd)
cur = log.get-output-fd()              ; Devuelve el fd del destino de salida actual
w = log.get-writer()                   ; Devuelve el io.writer correspondiente al destino de salida actual
```

```no
log.set-format(log.F-TIME | log.F-LEVEL | log.F-LOC)
log.open-file('app.log'); Los registros posteriores se añaden a app.log
log.error('disk full'); [ERROR] [main.no:12:5] disk full → app.log
```

---
