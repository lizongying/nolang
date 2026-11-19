---
sidebar_position: 3.3
---

## Sistema operativo y archivos

### os — interfaz del sistema operativo

Ofrece variables de entorno, operaciones con directorios, gestión de procesos, información del sistema, tiempo, etc. Para lectura y escritura de archivos, véase el módulo `fs`.

```no
; variables de entorno
val = os.get-env(key); devuelve ?str (nil=no definida)
os.set-env(key, 'value')

; directorios
dir = os.get-wd()
os.ch-dir(dir)
os.mkdir(path, mode)

; procesos
exit(code); termina el proceso (ya es intrínseco global, sin prefijo os, véase global)
pid = os.get-pid()

; información del sistema
name = os.host-name()
arch = os.get-arch()
msg = os.strerror(errnum)
errno = os.get-errno()

; atributos de archivo y usuario (la obtención de permisos está en os, no en fs)
mode = os.stat-mode(path); obtiene el modo del archivo (st_mode, devuelve ?i64, nil=fallo)
uid = os.stat-uid(path); obtiene el uid del propietario (devuelve ?i64, nil=fallo)
gid = os.stat-gid(path); obtiene el gid del grupo (devuelve ?i64, nil=fallo)
mtime = os.stat-mtime(path); obtiene la hora de modificación (segundos Unix, devuelve ?i64, nil=fallo)
self-uid = os.getuid(); ID del usuario actual
self-gid = os.getgid(); ID del grupo actual

; tiempo
sec = os.now()
ms = os.now-ms()
us = os.now-us()
ns = os.now-ns()
os.sleep(sec)
os.sleep-us(us)
os.sleep-ns(ns)

; argumentos de línea de comandos
argv = os.args(); todos los argumentos de línea de comandos ([]str, incluye el nombre del programa)
val = os.arg(idx); obtiene el argumento idx (fuera de límites devuelve nil)
```

### fs — utilidades del sistema de archivos

Encapsula los archivos abiertos en el struct `file` y las rutas en el struct `path`. Define el newtype `fd` (`fd = i64`) para evitar confusiones entre descriptores de archivo y `i64` arbitrarios. También ofrece el struct `embed` para incrustar un sistema de archivos en tiempo de compilación (mediante `#{embed='dir/path'}`).

```no
; newtype fd (i64 subyacente, mutuamente excluyente con i64 en el sistema de tipos)
fd = i64

; struct de archivo
file {
    fd fd
    path str
}

; archivos estándar (los literales enteros se pueden usar para inicializar fd)
stdin = file {
    fd: 0
    path: '<stdin>'
}
stdout = file {
    fd: 1
    path: '<stdout>'
}
stderr = file {
    fd: 2
    path: '<stderr>'
}

; descriptores estándar
STDIN-FD fd = 0
STDOUT-FD fd = 1
STDERR-FD fd = 2

; abrir archivo (con opciones)
file-mode {
    read,
    write,
    append,
    read-write,
}
file-perm {
    perm-600,
    perm-644,
    perm-664,
    perm-666,
    perm-755,
    perm-777,
}
file-opts {
    mode file-mode
    perm file-perm
    excl bool
    truncate bool
    append bool
}
f = fs.open(path, opts); abre un archivo; éxito devuelve ?file, fallo devuelve err

; métodos de file
read-n = f.read(buf, n); lee hasta n bytes en buf
line = f.read-line(); lee una línea (?str, nil=EOF o error)
content = f.read-bytes(); lee todo el archivo como ?[]byte (nil=vacío, err=fallo)
content = f.read-str(); lee todo el archivo como ?str (nil=vacío, err=fallo)
written = f.write(data, n); escribe n bytes
ok = f.write-str(data); escribe una cadena (sobrescribe), devuelve ?bool
ok = f.write-bytes(data); escribe []byte (sobrescribe), devuelve ?bool
ok = f.append(data, n); añade datos
ok = f.copy-to(dst-path); copia a la ruta de destino
ok = f.close(); cierra (los archivos estándar stdin/stdout/stderr no se cierran)
yes = f.is-open(); comprueba si está abierto (fd >= 0)
sz = f.size(); tamaño del archivo (mediante fstat(fd), elimina TOCTOU)

; funciones integradas (registradas en src/builtin/os.go, documentadas como comentarios en fs.no)
; son intrínsecos ForwardFunc/CLibCall — no necesitan implementación en .no.

; lectura/escritura de archivos (obsoletas; use read-bytes/write-bytes para gestión de errores)
; [deprecated] read-file: use read-bytes (gestiona errores, seguro ante TOCTOU, read en bucle)
;   una rebanada vacía significa fallo, no se distingue entre archivo vacío y error
; [deprecated] write-file: use write-bytes (gestiona errores, write en bucle)
;   no se distingue entre escritura parcial y fallo total
data = fs.read-file(path); [deprecated] lee todo el archivo como []byte (rebanada vacía=fallo)
ok = fs.write-file(path, data); [deprecated] escribe []byte al archivo (sobrescribe). Éxito devuelve true
fd = fs.open-read(path); apertura de solo lectura (O_RDONLY)
fd = fs.open-write(path); apertura para escritura (O_WRONLY|O_CREAT|O_TRUNC, 0644)
fd = fs.open-file(path, flags, mode); apertura con banderas y permisos personalizados
n = fs.read(fd, buf, n); lectura de bajo nivel
written = fs.write(fd, data, n); escritura de bajo nivel
ok = fs.close(fd); cierre de bajo nivel

; gestión de archivos
ok = fs.remove(path); elimina un archivo (unlink)
ok = fs.rename(old, new); renombra
ok = fs.symlink(target, linkpath); crea un enlace simbólico
ok = fs.link(oldpath, newpath); crea un enlace duro
ok = fs.copy-file(src, dst); copia el contenido de un archivo

; información de archivos
ok = fs.exists(path); comprueba si existe la ruta (sigue enlaces simbólicos)
ok = fs.is-file(path); comprueba si es un archivo normal
ok = fs.is-dir(path); comprueba si es un directorio
sz = fs.stat-size(path); obtiene el tamaño (devuelve ?i64)
sz = fs.file-size(path); ídem que stat-size (devuelve ?i64)
sz = fs.fstat-size(fd); obtiene el tamaño mediante fstat(fd), elimina TOCTOU (devuelve ?i64)

; nota: las funciones para obtener modo/propiedad/hora de modificación están en el módulo `os`
;     (os.stat-mode / os.stat-uid / os.stat-gid / os.stat-mtime)
ok = fs.lstat(path); obtiene información del enlace simbólico (no sigue el destino)

; operaciones con directorios
dirp = fs.open-dir(path); abre un directorio (devuelve un manejador, 0=fallo)
name = fs.read-dir(dirp); lee la siguiente entrada (devuelve ?str)
ok = fs.close-dir(dirp); cierra el manejador de directorio
names = fs.list-dir(path); lista todos los nombres de entradas (incluye . y ..)
names = fs.dir-entries(path); lista entradas (excluye . y ..)
paths = fs.walk(root); recorre recursivamente el árbol de directorios

; resolución de rutas
abs = fs.realpath(path); resuelve a una ruta absoluta canónica
target = fs.readlink(path); lee el destino del enlace simbólico (devuelve ?str)

; archivos/directorios temporales
name, fd = fs.mkstemp(tmpl); crea un archivo temporal (la plantilla termina en XXXXXX)
name = fs.mkdtemp(tmpl); crea un directorio temporal (la plantilla termina en XXXXXX, devuelve ?str)

; archivos especiales
ok = fs.mkfifo(path, mode); crea una tubería con nombre (FIFO)
ok = fs.mknod(path, mode, dev); crea un archivo especial (nodo de dispositivo)
ok = fs.truncate(path, length); trunca o expande un archivo al tamaño indicado
fs.sync(); vuelca los búferes del sistema de archivos al disco
ok = fs.touch-file(path); actualiza la marca de tiempo del archivo a la hora actual
ok = fs.utime(path, atime, mtime); fija los tiempos de acceso y modificación
ok = fs.rmdir(path); elimina un directorio vacío

; entrada estándar
line = fs.get-line(); lee una línea de la entrada estándar (?str, nil=EOF)

; funciones envoltorio de conveniencia (implementadas en Nolang en fs.no, llaman a intrínsecos)
content = fs.read-str(path); lee todo el archivo como ?str (nil=vacío, err=fallo)
content = fs.read-bytes(path); lee todo el archivo como ?[]byte (nil=vacío, err=fallo)
ok = fs.write-str(path, data); escribe una cadena al archivo (sobrescribe), devuelve ?bool
ok = fs.write-bytes(path, data); escribe []byte al archivo (sobrescribe), devuelve ?bool

; embed: sistema de archivos incrustado en compilación (inicializado por #{embed='dir/path'})
;   #{embed='../frontend/dist'}
;   DIST fs.embed
;   data = DIST.read('index.html')   ; lee el archivo incrustado, devuelve ([]byte, bool)
;   ok = DIST.exists('css/style.css') ; comprueba si existe el archivo incrustado
embed {
    count i64
    paths str; todas las rutas concatenadas (separadas por \0)
    pathStarts []i64; desplazamiento de inicio de cada ruta en paths
    pathLens []i64; longitud de cada ruta
    blob []byte; contenido de todos los archivos concatenado
    dataStarts []i64; desplazamiento de inicio de cada archivo en blob
    dataLens []i64; longitud de cada archivo
}

; específico de Windows (solo disponible en win-amd64/win-arm64)
bufptr = os.win-find-first-file(path); FindFirstFileA, devuelve un manejador (0=fallo)
name = os.win-find-next-file(bufptr); FindNextFileA, devuelve ?str (nil=no hay más entradas)
ok = os.win-find-close(bufptr); FindClose

; constantes de banderas para open() (específicas de plataforma, aquí valores de macOS)
O-RDONLY = 0
O-WRONLY = 1
O-RDWR = 2
O-CREAT = 512; macOS=512, Linux=64, Windows=256
O-TRUNC = 1024; macOS=1024, Linux=512, Windows=512
O-APPEND = 8; macOS=8, Linux=1024, Windows=8
O-EXCL = 2048; macOS=2048, Linux=128, Windows=1024
```

### env — variables de entorno (envoltorio simplificado)

```no
val = env.get(key); devuelve ?str (nil=no encontrada)
val = env.lookup(key); devuelve ?str (nil=no encontrada)
env.set(key, val)
env.unset(key)
val = env.get-with-default(key)
ok = env.is-set(key)
```

### args — argumentos de línea de comandos

```no
n = args.count()
arg = args.get(i)
name = args.program()
ok = args.has-flag(name)
val = args.get-option(name)
arg = args.get-positional(i)
```

### path — operaciones con rutas

Encapsula la cadena de ruta en el struct `path`; todas las operaciones se ofrecen en forma de método:

```no
#{fmt-skip}
SEP = 47     ; '/'（ASCII）
DOT = 46     ; '.'

; struct
path {
    p str
}

; unión y descomposición de rutas (modifica .p in situ)
p = path{
    p: '/a/b/c.txt'
}
p.join(b str)           ; une dos rutas (modificación in situ)
p.base() (out)           ; obtiene el nombre de archivo
p.dir()                  ; obtiene el directorio (modifica .p in situ)
p.ext() (out)            ; obtiene la extensión
p.clean()                ; normaliza (modifica .p in situ)
p.split() (f str)        ; divide en directorio+archivo (.p pasa a ser el directorio, devuelve el archivo)

; comprobaciones de ruta
p.is-abs() (yes bool)    ; si es una ruta absoluta

; operaciones de sistema de archivos (delega en intrínsecos de fs)
p.exists() (yes bool)        ; si existe
p.is-dir() (yes bool)        ; si es directorio
p.is-file() (yes bool)       ; si es archivo
p.size() (sz ?i64)           ; tamaño (nil = no existe o falla stat)
p.make-dir() (ok bool)       ; crea el directorio
p.remove() (ok bool)         ; elimina
p.rename(new-p str) (ok bool)    ; renombra
p.change-dir() (ok bool)     ; cambia el directorio de trabajo

; métodos constructores
path.current() (out path)    ; obtiene el directorio de trabajo actual
```

### bufio — lectura con búfer

```no
r = reader.init(fd, buf); inicializa el lector con búfer (devuelve reader)
ok = reader.fill(); rellena el búfer
b = reader.read-byte(); lee un byte (?byte, nil=EOF)
line = reader.read-line(); lee una línea (?str, nil=EOF o error)
reader.close(); cierra
```

### io — abstracción de entrada/salida

Proporciona los structs `io-reader` e `io-writer` para unificar la lectura y escritura de flujos de datos como archivos, entrada/salida estándar, etc.:

```no
; descriptores estándar
STDIN-FD = 0
STDOUT-FD = 1
STDERR-FD = 2

; struct io-reader
io-reader {
    fd i64
}
r = io-reader.from-fd(fd); crea desde fd
r = io-reader.from-stdin(); crea desde la entrada estándar
read-n = r.read(buf, n); lee n bytes
b = r.read-byte(); lee un byte (?byte, nil=EOF)
line = r.read-line(); lee una línea (?str, nil=EOF)
total = r.read-all(buf, size); lee todo

; struct io-writer
io-writer {
    fd i64
}
w = io-writer.from-fd(fd); crea desde fd
w = io-writer.from-stdout(); crea desde la salida estándar
w = io-writer.from-stderr(); crea desde la salida de error
written = w.write(data, n); escribe n bytes
written = w.write-str(s); escribe toda la cadena
written = w.write-byte(b); escribe un byte
written = w.write-line(s); escribe cadena + salto de línea

; funciones de conveniencia
n = io.out(s); escribe a stdout (sin salto de línea)
n = io.outln(s); escribe a stdout (con salto de línea)
n = io.err(s); escribe a stderr (sin salto de línea)
n = io.errln(s); escribe a stderr (con salto de línea)
line = io.read-line(); lee una línea de stdin (?str, nil=EOF)
```

### regexp — expresiones regulares

Encapsula el patrón en el struct `regexp`; el motor de expresiones regulares está implementado íntegramente en Nolang (VM imperativo + coincidencia con retroceso), sin depender de `regex.h` de la biblioteca estándar de C:

```no
; struct
regexp {
    pattern str
}

; métodos
re = regexp {
    pattern: '^hello'
}
matched = re.matches(text); comprueba si coincide
result = re.find(text); busca la primera subcadena coincidente
```

Admite **sintaxis de literal regex** `/pattern/flags` (estilo JavaScript), que se desazucara en una llamada a `regexp-compile` durante la generación de código:

```no
; literal regex (forma recomendada)
re = /\d+/
matched = re.matches('hello 123 world'); true

; con banderas
re = /[a-z]+/gi

; llamada explícita equivalente
re = regexp-compile('\\d+')
```

### process — operaciones con procesos

Ofrece creación de procesos, obtención de flujos estándar, espera de procesos, consulta de información de procesos, etc. Por debajo usa POSIX fork/exec/pipe/waitpid:

```no
; constantes de señales
SIG-TERM = 15
SIG-KILL = 9
SIG-INT = 2
SIG-STOP = 19
SIG-CONT = 18
SIG-CHLD = 17
WNOHANG = 1

; struct
process {
    pid i64
    stdin-fd i64
    stdout-fd i64
    stderr-fd i64
    exit-code i64
    running i64
}

; creación de procesos
p = process.new()
ok = p.start-with-opts(program, args, dir, input, merge-err); inicia un subproceso (sin esperar)
ok = p.start(program, arg); método conveniencia: fork + exec, captura stdout

; espera de procesos
status = p.wait(); espera bloqueante a que termine el subproceso
status = p.wait-nohang(); sondeo no bloqueante; nil=sigue ejecutando

; control de procesos
ok = p.kill(sig); envía una señal
ok = p.terminate(); SIG-TERM
ok = p.force-kill(); SIG-KILL

; operaciones con flujos estándar
read-n = p.read(buf, n); lee desde stdout
line = p.read-line(); lee una línea (?str, nil=EOF)
content, n = p.read-all(); lee todo el stdout
written = p.write(data, n); escribe a stdin
p.close-stdin(); cierra la tubería de stdin
p.close-stdout(); cierra la tubería de stdout
p.close-stderr(); cierra la tubería de stderr

; información del proceso
pid = p.pid-of(); ID del subproceso
code = p.exit-code-of(); código de salida
yes = p.is-running(); si sigue ejecutándose
pid = process.parent-pid(); ID del proceso padre

; ciclo de vida
p.close(); cierra todas las tuberías y espera

; ejecución única multiplataforma (recomendada, argumentos planos): fork/exec + captura de stdout/stderr, con tiempo de espera y entorno
;   entrada: program=ruta del ejecutable, args=rebanada de argumentos, dir=directorio de trabajo, input=contenido a escribir en stdin del subproceso,
;            env=rebanada de variables de entorno en forma K=V, timeout=milisegundos (<=0 sin límite), merge-err=true fusiona stderr en out
;   salida: out=stdout capturado (stderr se fusiona según merge-err), stderr=salida de stderr capturada, code=código de salida (-2=muerto por tiempo de espera, -1=fallo al iniciar), err=mensaje de error
out, stderr, code, err = process.cmd('echo', ['hello'], '', '', [], 0, false)

; forma de struct de opciones (recomendada, más legible): demostración de reenvío de struct entre módulos
;   cmdopts se define en este módulo; el llamador, en otro módulo, construye process.cmdopts{...} y lo pasa por referencia,
;   mostrando la capacidad de reenvío de definiciones de struct entre módulos de nolang.
cmdopts {
    dir str; directorio de trabajo; cadena vacía = hereda el directorio del proceso padre
    stdin str; contenido a escribir en stdin del subproceso; cadena vacía = no proporciona stdin
    env []str; variables de entorno (["K=V", ...]); arreglo vacío = hereda el entorno del padre
    timeout i64; tiempo de espera (milisegundos), <=0 = espera infinita
    merge-err bool; true = fusiona stderr en el out capturado
}
o = process.cmdopts {
    dir: ''
    stdin: 'piped-data'
    env: ['K=V']
    timeout: 200
    merge-err: false
}

; exec-result { stdout str, stderr str, code i64 }
res = process.exec('echo', ['hello'], o); devuelve exec-result (res.stdout / res.code ...)

; funciones de conveniencia
code = process.shell(cmd); ejecuta un comando mediante shell, devuelve el código de salida
pid = process.spawn(cmd); genera un subproceso, devuelve el pid
status = process.wait(pid); espera a que termine el pid indicado
content, code = process.new().output(program, arg); ejecuta y captura la salida
```

### net — operaciones de red

Ofrece capacidades de programación de red TCP, incluidas escucha de servidor, conexión de cliente, envío y recepción de datos, etc. Por debajo usa la API de sockets POSIX:

```no
; constantes de red
AF-INET = 2
SOCK-STREAM = 1
SOL-SOCKET = 65535
SO-REUSEADDR = 4
BACKLOG = 128

; struct listener
listener {
    fd i64
}

; operaciones de escucha
l = listener {}
ok = l.listen(host, port); crea una escucha TCP (socket+setsockopt+bind+listen)
c = l.accept(); acepta una conexión (?conn, nil=no hay conexión)
l.close(); cierra el socket de escucha
fd = l.fd-of(); obtiene el fd

; struct conn
conn {
    fd i64
}

; operaciones de conexión
c = conn {}
ok = c.dial(host, port); crea una conexión TCP (socket+connect)
written = c.send(data); envía una cadena
read-n = c.recv(buf, n); recibe datos en buf
line = c.recv-line(); recibe una línea (?str, nil=EOF, máximo 4096 bytes)
content, total = c.recv-all(); recibe todo hasta que se cierra la conexión
c.close(); cierra la conexión
fd = c.fd-of(); obtiene el fd

; funciones de conveniencia
l = net.listen-on(host, port); crea un listener y empieza a escuchar (?listener)
c = net.dial-to(host, port); crea una conexión y marca el número (?conn)
```

### net/ip — operaciones con direcciones IP

Ofrece análisis, validación, conversión y clasificación de direcciones IPv4. Implementado íntegramente en Nolang:

```no
; constantes de direcciones predefinidas
; 0.0.0.0
IP-ZERO

; 127.0.0.1
IP-LOOPBACK

; 0.0.0.0
IP-ANY

; 255.255.255.255
IP-BROADCAST

; struct ip-addr
ip-addr {
    a i64
    b i64
    c i64
    d i64
}

; análisis y conversión
ip = ip-addr {}
ok = ip.parse('192.168.1.1'); analiza desde una cadena
s = ip.to-str(); convierte a cadena '192.168.1.1'
v = ip.to-u32(); convierte a u32 (big-endian)
ip.from-u32(v); crea desde u32

; clasificación de direcciones
yes = ip.is-loopback(); 127.0.0.0/8
yes = ip.is-private(); 10/8, 172.16/12, 192.168/16
yes = ip.is-zero(); 0.0.0.0
yes = ip.is-broadcast(); 255.255.255.255
yes = ip.is-multicast(); 224.0.0.0/4
yes = ip.is-link-local(); 169.254.0.0/16
yes = ip.is-class-a(); clase A (1~126)
yes = ip.is-class-b(); clase B (128~191)
yes = ip.is-class-c(); clase C (192~223)

; comparación y subredes
yes = ip.equal(other); comparación de igualdad de direcciones
yes = ip.in-subnet(base, prefix-len); comprobación de inclusión en subred

; funciones de conveniencia
addr = ip.ip-parse(s); análisis rápido (?ip-addr, nil=inválida)
yes = ip.ip-is-loopback(s); comprobación rápida de loopback
yes = ip.ip-is-private(s); comprobación rápida de privada
```

### net/sse — cliente de Server-Sent Events

Admite la recepción de flujos SSE según la especificación W3C EventSource. Por debajo usa conexiones HTTP/1.1 persistentes y admite HTTP en claro y HTTPS (TLS):

```no
; struct sse-event
sse-event {
    event str; tipo de evento (por defecto 'message')
    data str; datos del evento (varias líneas data unidas con \n)
    id str; ID del evento
    retry i64; milisegundos de espera antes de reconectar (-1=no definido)
}

; struct sse-client
sse-client {
    fd i64; fd del socket TCP
    tls-c tls.conn; conexión TLS
    use-tls bool; si usa TLS
    connected bool; estado de la conexión
    host str; nombre de host del servidor
    port i64; número de puerto
    path str; ruta de la petición
    last-event-id str; ID del último evento recibido
    recv-buf str; búfer de recepción
    recv-buf-len i64; longitud de los datos en el búfer
}

; conexión y recepción de eventos
client = sse.connect('http://host:3000/events'); devuelve ?sse-client
client: {
    nil -> print('connect failed')

    -> {
        ev = client.next-event(); devuelve ?sse-event (nil=EOF, err=error)
        ev: {
            nil -> print('connection closed')

            err -> print('error: ' - it.msg())

            -> print(ev.data)
        }

        client.close()
    }
}

; otros métodos
yes = client.is-connected(); comprueba el estado de la conexión
ok = client.reconnect(); reconecta (usa last-event-id)
```

### net/http — cliente HTTP/1.1

Proporciona un cliente del protocolo HTTP/1.1 con métodos GET, POST, PUT, DELETE, PATCH, etc., con TLS opcional:

```no
; structs (las definiciones siguientes son del módulo std/http; dentro del módulo no se añade prefijo; hacerlo se leería como literal de struct)
request {
    method str
    url str
    body str
    headers [16]str
    header-count i64
}
response {
    status-code i64
    status-text str
    headers str
    header-names [32]str
    header-values [32]str
    header-count i64
    body str
}

; funciones de conveniencia
resp = http.get(url); petición GET (?http.response)
resp = http.post(url, body); petición POST (?http.response)
resp = http.put(url, body); petición PUT (?http.response)
resp = http.delete(url); petición DELETE (?http.response)
resp = http.patch(url, body); petición PATCH (?http.response)
resp = http.do(method, url, body); método personalizado (?http.response)
resp = http.get-auth(url, user, password); GET con autenticación Basic (?http.response)
resp = http.post-auth(url, body, user, password); POST con autenticación Basic (?http.response)

; usar el objeto request
req = http.request {}
req.init('POST', url, body)
req.add-header('Content-Type', 'application/json')
ok = http.set-basic-auth(req, user, password); fija la cabecera de autenticación Basic (bool)
ok = http.set-bearer-auth(req, token); fija la cabecera Bearer (bool)
ok = http.set-api-key(req, header-name, api-key); fija la cabecera de API-key (bool)
resp = http.do-req(req); envía la petición (?http.response)

; analizar y leer las cabeceras de la respuesta
resp.parse-headers()
val = resp.get-header(name); consulta el valor de una cabecera (?str)
```

### net/http2 — cliente HTTP/2.0 (RFC 7540)

Admite análisis de tramas HTTP/2 y gestión de conexiones, con modo h2c prior knowledge:

```no
; struct de trama
http2-frame {
    length i64
    frame-type i64
    flags i64
    stream-id i64
    payload str
}

; struct de conexión
http2-conn {
    fd i64
    next-stream-id i64
    send-window i64
    recv-window i64
    initialized bool
    use-tls bool
}

; conexión y peticiones
c = http2.connect(host, port); establece la conexión (?http2-conn)
resp = http2.do(method, url, body); envía una petición (?http.response)

; operaciones con tramas
frame = http2-frame {}
pos = frame.parse(data, pos); analiza una trama (?i64)
pos = frame.serialize(buf, pos); serializa una trama
ok = c.send-frame(frame); envía una trama
frame = c.recv-frame(); recibe una trama (?http2-frame)
```

### net/http3 — cliente HTTP/3.0 (RFC 9114)

Cliente HTTP/3 basado en el protocolo QUIC:

```no
; constantes de método
HTTP3-METHOD-GET = 'GET'
HTTP3-METHOD-POST = 'POST'
HTTP3-METHOD-PUT = 'PUT'
HTTP3-METHOD-DELETE = 'DELETE'
HTTP3-METHOD-PATCH = 'PATCH'
HTTP3-METHOD-HEAD = 'HEAD'
HTTP3-METHOD-OPTIONS = 'OPTIONS'

; funciones de conveniencia
c = http3.connect(host, port); establece una conexión QUIC (?http3-conn)
resp = http3.send-request(c, method, path, host, body); envía una petición (?http.response)
resp = http3.get(url); petición GET (?http.response)
resp = http3.post(url, body); petición POST (?http.response)

; codificación/decodificación de cabeceras QPACK
buf, n = http3.encode-header(name, value)
buf, n = http3.encode-headers(names, values, count)
name, value, pos = http3.decode-header(buf, pos)
```

### net/ws — cliente y servidor WebSocket (RFC 6455)

Admite comunicación full-duplex del protocolo WebSocket, actuando como cliente o como servidor:

```no
; struct de mensaje
ws-message {
    opcode i64; 0=continuación, 1=texto, 2=binario, 8=cierre, 9=ping, 10=pong
    data str
    fin bool
}

; servidor
s = ws.listen-on(host, port); crea una escucha (?ws-server)
c = s.accept(); acepta una conexión (?ws-server-conn)
msg = c.recv(); recibe un mensaje (?ws-message)
ok = c.send-text(text); envía texto
ok = c.send-binary(data); envía binario
c.close()

; cliente
c = ws.connect(url); conecta al servidor (?ws-client)
msg = c.recv(); recibe un mensaje (?ws-message)
ok = c.send-text(text); envía texto
ok = c.send-binary(data); envía binario
c.close()
```

### net/tls — cliente TLS 1.2/1.3 (implementación pura en Nolang)

Proporciona conexiones cifradas TLS, con compatibilidad con TLS 1.2 y 1.3:

```no
; conexión
c = tls.dial(host, port); establece una conexión TLS (?tls.conn)
n = c.send(data); envía datos cifrados (?i64)
n = c.recv(buf, n); recibe datos descifrados (?i64)
c.close()
```

### net/client — cliente TCP de alto nivel

Encapsula el struct `conn` y ofrece funciones como reconexión automática:

```no
c = client.client-create(host, port); crea un cliente (?client)
ok = c.connect(host, port); conecta
ok = c.reconnect(); reconecta
written = c.send(data); envía
read-n = c.recv(buf, n); recibe
line = c.recv-line(); recibe una línea (?str)
written = c.send-line(line); envía una línea (i64)
response = c.request(data); modo petición-respuesta (?str)
yes = c.is-connected(); estado de la conexión
c.close()
```

### net/quic — protocolo QUIC (RFC 9000)

Ofrece una implementación del protocolo de transporte QUIC, que sirve de capa de transporte subyacente de HTTP/3:

```no
c = quic.dial(host, port); establece una conexión QUIC (?quic.conn)
n = c.send(data, n); envía datos
n = c.recv(buf, n); recibe datos
c.close()
```

### net/server — servidor HTTP

Ofrece funcionalidad de servidor HTTP:

```no
s = server {}
ok = s.listen(host, port); empieza a escuchar
c = s.accept(); acepta una conexión (?server-conn)
s.close()
```

### net/dns — resolución DNS

Ofrece consultas DNS:

```no
ip = dns.resolve(host); resuelve un nombre de host (?str)
```

### net/url — análisis de URL

Ofrece análisis y construcción de URL:

```no
u = url.url-parse(url); analiza una URL
s = u.to-str(); convierte a cadena
```

### net/cookie — cookies HTTP

Ofrece análisis y gestión de cookies HTTP:

```no
c = cookie {}
c.parse(set-cookie-header)
s = c.to-str()
```

### net/multipart — datos de formulario Multipart

Ofrece análisis y construcción de `multipart/form-data`:

```no
; escritura (construir multipart/form-data)
w = multipart.multipart-create(); crea un escritor (?multipart-writer)
w.write-field(name, value); escribe un campo
body = w.body-of(); obtiene el body codificado
ct = w.content-type(); obtiene el content-type (incluye el boundary)

; análisis
r = multipart.multipart-parse(content-type, body); analiza (?multipart-reader)
```

### net/hpack — compresión de cabeceras HPACK (HTTP/2)

Ofrece codificación y decodificación del algoritmo HPACK, usado en la compresión de cabeceras HTTP/2:

```no
; operaciones con la tabla de cabeceras
t = hpack.tables {}
t.init(); inicializa las tablas estática y dinámica
out = t.encode-header(name, value); codifica una sola cabecera (str)
names, values, count, ok = t.decode-headers(data); decodifica cabeceras ([]str)
t.dyn-add(name, value); añade a la tabla dinámica
idx = t.find(name, value); busca por name/value completo
idx = t.find-name(name); busca solo por name
name, value, ok = t.lookup(idx); consulta por índice

; codificación/decodificación de primitivas enteras/cadenas
out = hpack.encode-int(val, prefix-bits, prefix-byte)
val, next-pos = hpack.decode-int(data, pos, prefix-bits)
out = hpack.encode-str(s)
out, next-pos = hpack.decode-str(data, pos)
```

### net/proxy — compatibilidad con proxies

Ofrece conexiones a través de proxies HTTP/SOCKS:

```no
c = proxy.dial(proxy-url, target-host, target-port)
```

### net/pool — agrupación de conexiones

Ofrece gestión agrupada de conexiones de red, reutilizándolas para mejorar el rendimiento:

```no
p = pool {}
p.init(capacity)
c = p.get(); obtiene una conexión del grupo
p.put(c); devuelve la conexión al grupo
p.close()
```

### net/unix — sockets de dominio Unix

Ofrece comunicación mediante sockets de dominio Unix:

```no
fd = unix.unix-listen(path); escucha (devuelve fd)
fd = unix.unix-dial(path); conecta (devuelve fd)
l = unix.unix-listen-on(path); crea un listener (?unix-listener)
c = l.accept(); acepta una conexión (?unix-conn)
```

---
