---
sidebar_position: 3.3
---

## Операционная система и файлы

### os — интерфейс операционной системы

Предоставляет переменные среды, операции с каталогами, управление процессами, информацию о системе, время и т. д. Для чтения/записи файлов см. модуль `fs`.

```no
; Переменные среды
val = os.get-env(key); возвращает ?str (nil = не задано)
os.set-env(key, 'value')

; Каталоги
dir = os.get-wd()
os.ch-dir(dir)
os.mkdir(path, mode)

; Процессы
exit(code); завершить процесс (повышено до глобального встроенного, префикс os не нужен, см. global)
pid = os.get-pid()

; Информация о системе
name = os.host-name()
arch = os.get-arch()
msg = os.strerror(errnum)
errno = os.get-errno()

; Атрибуты файла и пользователь (получение прав — в os, не в fs)
mode = os.stat-mode(path); получить режим файла (st_mode, возвращает ?i64, nil = не удалось получить)
uid = os.stat-uid(path); получить uid владельца файла (возвращает ?i64, nil = не удалось получить)
gid = os.stat-gid(path); получить gid группы файла (возвращает ?i64, nil = не удалось получить)
mtime = os.stat-mtime(path); получить время изменения (секунды Unix, возвращает ?i64, nil = не удалось получить)
self-uid = os.getuid(); текущий ID пользователя
self-gid = os.getgid(); текущий ID группы

; Время
sec = os.now()
ms = os.now-ms()
us = os.now-us()
ns = os.now-ns()
os.sleep(sec)
os.sleep-us(us)
os.sleep-ns(ns)

; Аргументы командной строки
argv = os.args(); все аргументы командной строки ([]str, включая имя программы)
val = os.arg(idx); получить idx-й аргумент (nil при выходе за границы)
```

### fs — утилиты файловой системы

Структура `file` инкапсулирует открытый файл, а структура `path` — путь. Определён newtype `fd` (`fd = i64`), чтобы файловые дескрипторы не путались с произвольными `i64`. Также предоставляется структура `embed` для встраивания файловой системы на этапе компиляции (через `#{embed='dir/path'}`).

```no
; newtype fd (базовый i64, в системе типов несовместим с i64)
fd = i64

; Структура файла
file {
    fd fd
    path str
}

; Стандартные файлы (целочисленные литералы можно использовать для инициализации fd)
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

; Стандартные файловые дескрипторы
STDIN-FD fd = 0
STDOUT-FD fd = 1
STDERR-FD fd = 2

; Открытие файла (с опциями)
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
f = fs.open(path, opts); открыть файл: при успехе ?file, при ошибке err

; Методы file
read-n = f.read(buf, n); прочитать не более n байт в buf
line = f.read-line(); прочитать строку (?str, nil = EOF или ошибка)
content = f.read-bytes(); прочитать весь файл как ?[]byte (nil = пусто, err = ошибка)
content = f.read-str(); прочитать весь файл как ?str (nil = пусто, err = ошибка)
written = f.write(data, n); записать n байт
ok = f.write-str(data); записать строку (перезапись), возвращает ?bool
ok = f.write-bytes(data); записать []byte (перезапись), возвращает ?bool
ok = f.append(data, n); добавить данные
ok = f.copy-to(dst-path); скопировать в целевой путь
ok = f.close(); закрыть (стандартные файлы stdin/stdout/stderr не закрываются)
yes = f.is-open(); проверить, открыт ли файл (fd >= 0)
sz = f.size(); размер файла (через fstat(fd), без TOCTOU)

; Встроенные функции (регистрируются в src/builtin/os.go, задокументированы комментариями в fs.no)
; Это встроенные функции ForwardFunc/CLibCall — реализация на .no не требуется.

; Чтение/запись файлов (устарело; рекомендуется read-bytes/write-bytes для поддержки обработки ошибок)
; [deprecated] read-file: используйте read-bytes (обработка ошибок, безопасность TOCTOU, циклический read)
;   пустой срез означает ошибку, невозможно отличить пустой файл от ошибки
; [deprecated] write-file: используйте write-bytes (обработка ошибок, циклический write)
;   невозможно отличить частичную запись от полного отказа
data = fs.read-file(path); [deprecated] прочитать весь файл как []byte (пустой срез = ошибка)
ok = fs.write-file(path, data); [deprecated] записать []byte в файл (перезапись). true при успехе
fd = fs.open-read(path); открыть только для чтения (O_RDONLY)
fd = fs.open-write(path); открыть для записи (O_WRONLY|O_CREAT|O_TRUNC, 0644)
fd = fs.open-file(path, flags, mode); открыть с заданными флагами и правами
n = fs.read(fd, buf, n); низкоуровневое чтение
written = fs.write(fd, data, n); низкоуровневая запись
ok = fs.close(fd); низкоуровневое закрытие

; Управление файлами
ok = fs.remove(path); удалить файл (unlink)
ok = fs.rename(old, new); переименовать
ok = fs.symlink(target, linkpath); создать символическую ссылку
ok = fs.link(oldpath, newpath); создать жёсткую ссылку
ok = fs.copy-file(src, dst); скопировать содержимое файла

; Информация о файле
ok = fs.exists(path); проверить существование пути (следовать символическим ссылкам)
ok = fs.is-file(path); проверить, является ли путь обычным файлом
ok = fs.is-dir(path); проверить, является ли путь каталогом
sz = fs.stat-size(path); получить размер файла (возвращает ?i64)
sz = fs.file-size(path); то же, что stat-size (возвращает ?i64)
sz = fs.fstat-size(fd); получить размер через fstat(fd), без TOCTOU (возвращает ?i64)

; Примечание: функции получения режима/владельца/времени изменения файла находятся в модуле `os`
;     (os.stat-mode / os.stat-uid / os.stat-gid / os.stat-mtime)
ok = fs.lstat(path); получить информацию о символической ссылке (не следовать цели)

; Операции с каталогами
dirp = fs.open-dir(path); открыть каталог (возвращает дескриптор, 0 = ошибка)
name = fs.read-dir(dirp); прочитать следующую запись каталога (возвращает ?str)
ok = fs.close-dir(dirp); закрыть дескриптор каталога
names = fs.list-dir(path); перечислить имена всех записей (включая . и ..)
names = fs.dir-entries(path); перечислить записи (без . и ..)
paths = fs.walk(root); рекурсивно обойти дерево каталогов

; Разрешение путей
abs = fs.realpath(path); получить абсолютный канонический путь
target = fs.readlink(path); прочитать цель символической ссылки (возвращает ?str)

; Временные файлы/каталоги
name, fd = fs.mkstemp(tmpl); создать временный файл (шаблон заканчивается на XXXXXX)
name = fs.mkdtemp(tmpl); создать временный каталог (шаблон заканчивается на XXXXXX, возвращает ?str)

; Специальные файлы
ok = fs.mkfifo(path, mode); создать именованный канал (FIFO)
ok = fs.mknod(path, mode, dev); создать специальный файл (узел устройства)
ok = fs.truncate(path, length); усечь или расширить файл до заданного размера
fs.sync(); сбросить буферы файловой системы на диск
ok = fs.touch-file(path); обновить метку времени файла до текущего
ok = fs.utime(path, atime, mtime); установить время доступа и изменения
ok = fs.rmdir(path); удалить пустой каталог

; Стандартный ввод
line = fs.get-line(); прочитать строку из стандартного ввода (?str, nil = EOF)

; Удобные обёртки (реализованы на Nolang в fs.no, внутри вызывают встроенные функции)
content = fs.read-str(path); прочитать весь файл как ?str (nil = пусто, err = ошибка)
content = fs.read-bytes(path); прочитать весь файл как ?[]byte (nil = пусто, err = ошибка)
ok = fs.write-str(path, data); записать строку в файл (перезапись), возвращает ?bool
ok = fs.write-bytes(path, data); записать []byte в файл (перезапись), возвращает ?bool

; embed: встроенная на этапе компиляции файловая система (инициализируется через #{embed='dir/path'})
;   #{embed='../frontend/dist'}
;   DIST fs.embed
;   data = DIST.read('index.html')   ; прочитать встроенный файл, возвращает ([]byte, bool)
;   ok = DIST.exists('css/style.css') ; проверить существование встроенного файла
embed {
    count i64
    paths str; все пути, объединённые (через \0)
    pathStarts []i64; начальное смещение каждого пути в paths
    pathLens []i64; длина каждого пути
    blob []byte; содержимое всех файлов, объединённое
    dataStarts []i64; начальное смещение каждого файла в blob
    dataLens []i64; длина каждого файла
}

; Только для Windows (доступно на win-amd64/win-arm64)
bufptr = os.win-find-first-file(path); FindFirstFileA, возвращает дескриптор (0 = ошибка)
name = os.win-find-next-file(bufptr); FindNextFileA, возвращает ?str (nil = больше нет записей)
ok = os.win-find-close(bufptr); FindClose

; Константы флагов open() (зависят от платформы, здесь показаны значения для macOS)
O-RDONLY = 0
O-WRONLY = 1
O-RDWR = 2
O-CREAT = 512; macOS=512, Linux=64, Windows=256
O-TRUNC = 1024; macOS=1024, Linux=512, Windows=512
O-APPEND = 8; macOS=8, Linux=1024, Windows=8
O-EXCL = 2048; macOS=2048, Linux=128, Windows=1024
```

### env — переменные среды (упрощённая обёртка)

```no
val = env.get(key); возвращает ?str (nil = не найдено)
val = env.lookup(key); возвращает ?str (nil = не найдено)
env.set(key, val)
env.unset(key)
val = env.get-with-default(key)
ok = env.is-set(key)
```

### args — аргументы командной строки

```no
n = args.count()
arg = args.get(i)
name = args.program()
ok = args.has-flag(name)
val = args.get-option(name)
arg = args.get-positional(i)
```

### path — операции с путями

Структура `path` инкапсулирует строку пути; все операции предоставляются в виде методов:

```no
#{fmt-skip}
SEP = 47     ; '/'（ASCII）
DOT = 46     ; '.'

; Структура
path {
    p str
}

; Объединение и разложение путей (изменение .p на месте)
p = path{
    p: '/a/b/c.txt'
}
p.join(b str)           ; объединить два пути (на месте)
p.base() (out)           ; получить имя файла
p.dir()                  ; получить каталог (изменить .p на месте)
p.ext() (out)            ; получить расширение
p.clean()                ; нормализовать (изменить .p на месте)
p.split() (f str)        ; разделить на каталог + имя файла (.p становится каталогом, возвращает имя файла)

; Проверки пути
p.is-abs() (yes bool)    ; является ли путь абсолютным

; Операции с файловой системой (делегируются встроенным функциям fs)
p.exists() (yes bool)        ; существует ли
p.is-dir() (yes bool)        ; является ли каталогом
p.is-file() (yes bool)       ; является ли файлом
p.size() (sz ?i64)           ; размер файла (nil = не существует или ошибка stat)
p.make-dir() (ok bool)       ; создать каталог
p.remove() (ok bool)         ; удалить
p.rename(new-p str) (ok bool)    ; переименовать
p.change-dir() (ok bool)     ; сменить рабочий каталог

; Методы-конструкторы
path.current() (out path)    ; получить текущий рабочий каталог
```

### bufio — буферизованное чтение

```no
r = reader.init(fd, buf); инициализировать буферизованный ридер (возвращает reader)
ok = reader.fill(); заполнить буфер
b = reader.read-byte(); прочитать один байт (?byte, nil = EOF)
line = reader.read-line(); прочитать строку (?str, nil = EOF или ошибка)
reader.close(); закрыть
```

### io — абстракция ввода/вывода

Предоставляет структуры `io-reader` и `io-writer`, унифицирующие операции чтения/записи файлов, стандартного ввода/вывода и других потоков данных:

```no
; Стандартные файловые дескрипторы
STDIN-FD = 0
STDOUT-FD = 1
STDERR-FD = 2

; Структура io-reader
io-reader {
    fd i64
}
r = io-reader.from-fd(fd); создать из fd
r = io-reader.from-stdin(); создать из стандартного ввода
read-n = r.read(buf, n); прочитать n байт
b = r.read-byte(); прочитать один байт (?byte, nil = EOF)
line = r.read-line(); прочитать строку (?str, nil = EOF)
total = r.read-all(buf, size); прочитать всё

; Структура io-writer
io-writer {
    fd i64
}
w = io-writer.from-fd(fd); создать из fd
w = io-writer.from-stdout(); создать из стандартного вывода
w = io-writer.from-stderr(); создать из стандартного потока ошибок
written = w.write(data, n); записать n байт
written = w.write-str(s); записать всю строку
written = w.write-byte(b); записать один байт
written = w.write-line(s); записать строку + перевод строки

; Удобные функции
n = io.out(s); записать в stdout (без перевода строки)
n = io.outln(s); записать в stdout (с переводом строки)
n = io.err(s); записать в stderr (без перевода строки)
n = io.errln(s); записать в stderr (с переводом строки)
line = io.read-line(); прочитать строку из stdin (?str, nil = EOF)
```

### regexp — регулярные выражения

Структура `regexp` инкапсулирует шаблон; движок регулярных выражений полностью реализован на Nolang (императивная VM + поиск с возвратом) и не зависит от C-библиотеки regex.h:

```no
; Структура
regexp {
    pattern str
}

; Методы
re = regexp {
    pattern: '^hello'
}
matched = re.matches(text); проверить совпадение
result = re.find(text); найти первую совпадающую подстроку
```

Поддерживается **синтаксис регулярных литералов** `/pattern/flags` (в стиле JavaScript), которые на этапе генерации кода разворачиваются в вызов `regexp-compile`:

```no
; Регулярный литерал (рекомендуемая форма)
re = /\d+/
matched = re.matches('hello 123 world'); true

; С флагами
re = /[a-z]+/gi

; Эквивалентный явный вызов
re = regexp-compile('\\d+')
```

### process — работа с процессами

Предоставляет создание процессов, получение стандартных потоков, ожидание процессов, запрос информации о процессах и др. В основе — POSIX fork/exec/pipe/waitpid:

```no
; Константы сигналов
SIG-TERM = 15
SIG-KILL = 9
SIG-INT = 2
SIG-STOP = 19
SIG-CONT = 18
SIG-CHLD = 17
WNOHANG = 1

; Структура
process {
    pid i64
    stdin-fd i64
    stdout-fd i64
    stderr-fd i64
    exit-code i64
    running i64
}

; Создание процесса
p = process.new()
ok = p.start-with-opts(program, args, dir, input, merge-err); запустить дочерний процесс (без ожидания)
ok = p.start(program, arg); удобный метод: fork + exec с перехватом stdout

; Ожидание процесса
status = p.wait(); блокирующее ожидание завершения дочернего процесса
status = p.wait-nohang(); неблокирующий опрос; nil = всё ещё выполняется

; Управление процессом
ok = p.kill(sig); отправить сигнал
ok = p.terminate(); SIG-TERM
ok = p.force-kill(); SIG-KILL

; Работа со стандартными потоками
read-n = p.read(buf, n); читать из stdout
line = p.read-line(); прочитать строку (?str, nil = EOF)
content, n = p.read-all(); прочитать весь stdout
written = p.write(data, n); записать в stdin
p.close-stdin(); закрыть канал stdin
p.close-stdout(); закрыть канал stdout
p.close-stderr(); закрыть канал stderr

; Информация о процессе
pid = p.pid-of(); ID дочернего процесса
code = p.exit-code-of(); код завершения
yes = p.is-running(); всё ещё выполняется ли
pid = process.parent-pid(); ID родительского процесса

; Жизненный цикл
p.close(); закрыть все каналы и дождаться завершения

; Кроссплатформенный одноразовый запуск (рекомендуется, плоские параметры): fork/exec + перехват stdout/stderr, с таймаутом и окружением
;   Входные параметры: program — путь к исполняемому файлу, args — срез аргументов, dir — рабочий каталог, input — данные, записываемые в stdin дочернего процесса,
;         env — срез переменных среды в формате K=V, timeout — миллисекунды (<=0 без ограничения), merge-err=true объединяет stderr с out
;   Выходные параметры: out — перехваченный stdout (stderr объединяется в зависимости от merge-err), stderr — перехваченный stderr, code — код завершения (-2 = убит по таймауту, -1 = не удалось запустить), err — сообщение об ошибке
out, stderr, code, err = process.cmd('echo', ['hello'], '', '', [], 0, false)

; Форма со структурой опций (рекомендуется, читаемее): демонстрация передачи структуры opts между модулями
;   cmdopts определена в этом модуле, вызывающая сторона в другом модуле создаёт process.cmdopts{...} и передаёт по ссылке,
;   что демонстрирует возможность nolang передавать определения структур между модулями.
cmdopts {
    dir str; рабочий каталог; пустая строка = наследовать каталог родительского процесса
    stdin str; данные для stdin дочернего процесса; пустая строка = stdin не предоставляется
    env []str; переменные среды (["K=V", ...]); пустой массив = наследовать окружение родителя
    timeout i64; таймаут (миллисекунды), <=0 = ждать неограниченно
    merge-err bool; true = объединить stderr с перехваченным out
}
o = process.cmdopts {
    dir: ''
    stdin: 'piped-data'
    env: ['K=V']
    timeout: 200
    merge-err: false
}

; exec-result { stdout str, stderr str, code i64 }
res = process.exec('echo', ['hello'], o); возвращает exec-result (res.stdout / res.code ...)

; Удобные функции
code = process.shell(cmd); выполнить команду через shell, вернуть код завершения
pid = process.spawn(cmd); создать дочерний процесс, вернуть pid
status = process.wait(pid); дождаться завершения заданного pid
content, code = process.new().output(program, arg); выполнить и перехватить вывод
```

### net — сетевые операции

Предоставляет возможности TCP-сетевого программирования: прослушивание сервером, подключение клиента, передача и приём данных и т. д. В основе — POSIX socket API:

```no
; Сетевые константы
AF-INET = 2
SOCK-STREAM = 1
SOL-SOCKET = 65535
SO-REUSEADDR = 4
BACKLOG = 128

; Структура listener
listener {
    fd i64
}

; Операции прослушивания
l = listener {}
ok = l.listen(host, port); создать TCP-прослушивание (socket+setsockopt+bind+listen)
c = l.accept(); принять соединение (?conn, nil = нет соединения)
l.close(); закрыть прослушивающий socket
fd = l.fd-of(); получить fd

; Структура conn
conn {
    fd i64
}

; Операции соединения
c = conn {}
ok = c.dial(host, port); установить TCP-соединение (socket+connect)
written = c.send(data); отправить строку
read-n = c.recv(buf, n); принять данные в buf
line = c.recv-line(); принять строку (?str, nil = EOF, не более 4096 байт)
content, total = c.recv-all(); принимать всё до закрытия соединения
c.close(); закрыть соединение
fd = c.fd-of(); получить fd

; Удобные функции
l = net.listen-on(host, port); создать слушатель и начать прослушивание (?listener)
c = net.dial-to(host, port); создать соединение и выполнить dial (?conn)
```

### net/ip — работа с IP-адресами

Предоставляет разбор, проверку, преобразование и классификацию IPv4-адресов. Реализация полностью на Nolang:

```no
; Предопределённые константы адресов
; 0.0.0.0
IP-ZERO

; 127.0.0.1
IP-LOOPBACK

; 0.0.0.0
IP-ANY

; 255.255.255.255
IP-BROADCAST

; Структура ip-addr
ip-addr {
    a i64
    b i64
    c i64
    d i64
}

; Разбор и преобразование
ip = ip-addr {}
ok = ip.parse('192.168.1.1'); разобрать из строки
s = ip.to-str(); преобразовать в строку '192.168.1.1'
v = ip.to-u32(); преобразовать в u32 (big-endian)
ip.from-u32(v); создать из u32

; Классификация адресов
yes = ip.is-loopback(); 127.0.0.0/8
yes = ip.is-private(); 10/8, 172.16/12, 192.168/16
yes = ip.is-zero(); 0.0.0.0
yes = ip.is-broadcast(); 255.255.255.255
yes = ip.is-multicast(); 224.0.0.0/4
yes = ip.is-link-local(); 169.254.0.0/16
yes = ip.is-class-a(); класс A (1–126)
yes = ip.is-class-b(); класс B (128–191)
yes = ip.is-class-c(); класс C (192–223)

; Сравнение и подсети
yes = ip.equal(other); сравнение адресов на равенство
yes = ip.in-subnet(base, prefix-len); проверить вхождение в подсеть

; Удобные функции
addr = ip.ip-parse(s); быстрый разбор (?ip-addr, nil = некорректный)
yes = ip.ip-is-loopback(s); быстро проверить loopback
yes = ip.ip-is-private(s); быстро проверить приватность
```

### net/sse — клиент Server-Sent Events

Поддерживает приём SSE-потоков по спецификации W3C EventSource. В основе — длительное HTTP/1.1-соединение, поддерживаются обычный HTTP и HTTPS (TLS):

```no
; Структура sse-event
sse-event {
    event str; тип события (по умолчанию 'message')
    data str; данные события (несколько строк data объединяются через \n)
    id str; ID события
    retry i64; задержка перед переподключением в миллисекундах (-1 = не задано)
}

; Структура sse-client
sse-client {
    fd i64; TCP socket fd
    tls-c tls.conn; TLS-соединение
    use-tls bool; используется ли TLS
    connected bool; состояние соединения
    host str; имя хоста сервера
    port i64; номер порта
    path str; путь запроса
    last-event-id str; ID последнего полученного события
    recv-buf str; буфер приёма
    recv-buf-len i64; длина данных в буфере
}

; Соединение и приём событий
client = sse.connect('http://host:3000/events'); возвращает ?sse-client
client: {
    nil -> print('connect failed')

    -> {
        ev = client.next-event(); возвращает ?sse-event (nil = EOF, err = ошибка)
        ev: {
            nil -> print('connection closed')

            err -> print('error: ' - it.msg())

            -> print(ev.data)
        }
        client.close()
    }
}

; Другие методы
yes = client.is-connected(); проверить состояние соединения
ok = client.reconnect(); переподключиться (используя last-event-id)
```

### net/http — клиент HTTP/1.1

Предоставляет клиент протокола HTTP/1.1 с поддержкой методов GET, POST, PUT, DELETE, PATCH и др.; TLS — по выбору:

```no
; Структуры (ниже — определения внутри модуля std/http; внутри модуля определения не имеют префикса модуля, иначе они читаются как литерал структуры)
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

; Удобные функции
resp = http.get(url); GET-запрос (?http.response)
resp = http.post(url, body); POST-запрос (?http.response)
resp = http.put(url, body); PUT-запрос (?http.response)
resp = http.delete(url); DELETE-запрос (?http.response)
resp = http.patch(url, body); PATCH-запрос (?http.response)
resp = http.do(method, url, body); произвольный метод (?http.response)
resp = http.get-auth(url, user, password); GET с Basic-аутентификацией (?http.response)
resp = http.post-auth(url, body, user, password); POST с Basic-аутентификацией (?http.response)

; Использование объекта request
req = http.request {}
req.init('POST', url, body)
req.add-header('Content-Type', 'application/json')
ok = http.set-basic-auth(req, user, password); установить заголовок Basic-аутентификации (bool)
ok = http.set-bearer-auth(req, token); установить заголовок Bearer (bool)
ok = http.set-api-key(req, header-name, api-key); установить заголовок API-key (bool)
resp = http.do-req(req); отправить запрос (?http.response)

; Разбор и чтение заголовков ответа
resp.parse-headers()
val = resp.get-header(name); получить значение заголовка (?str)
```

### net/http2 — клиент HTTP/2.0 (RFC 7540)

Поддерживает разбор кадров HTTP/2 и управление соединениями, включая режим h2c prior knowledge:

```no
; Структура кадра
http2-frame {
    length i64
    frame-type i64
    flags i64
    stream-id i64
    payload str
}

; Структура соединения
http2-conn {
    fd i64
    next-stream-id i64
    send-window i64
    recv-window i64
    initialized bool
    use-tls bool
}

; Соединение и запросы
c = http2.connect(host, port); установить соединение (?http2-conn)
resp = http2.do(method, url, body); отправить запрос (?http.response)

; Работа с кадрами
frame = http2-frame {}
pos = frame.parse(data, pos); разобрать кадр (?i64)
pos = frame.serialize(buf, pos); сериализовать кадр
ok = c.send-frame(frame); отправить кадр
frame = c.recv-frame(); принять кадр (?http2-frame)
```

### net/http3 — клиент HTTP/3.0 (RFC 9114)

Клиент HTTP/3, основанный на протоколе QUIC:

```no
; Константы методов
HTTP3-METHOD-GET = 'GET'
HTTP3-METHOD-POST = 'POST'
HTTP3-METHOD-PUT = 'PUT'
HTTP3-METHOD-DELETE = 'DELETE'
HTTP3-METHOD-PATCH = 'PATCH'
HTTP3-METHOD-HEAD = 'HEAD'
HTTP3-METHOD-OPTIONS = 'OPTIONS'

; Удобные функции
c = http3.connect(host, port); установить QUIC-соединение (?http3-conn)
resp = http3.send-request(c, method, path, host, body); отправить запрос (?http.response)
resp = http3.get(url); GET-запрос (?http.response)
resp = http3.post(url, body); POST-запрос (?http.response)

; Кодирование/декодирование заголовков QPACK
buf, n = http3.encode-header(name, value)
buf, n = http3.encode-headers(names, values, count)
name, value, pos = http3.decode-header(buf, pos)
```

### net/ws — клиент и сервер WebSocket (RFC 6455)

Поддерживает полнодуплексную связь по протоколу WebSocket; можно работать как клиентом, так и сервером:

```no
; Структура сообщения
ws-message {
    opcode i64; 0=continuation, 1=text, 2=binary, 8=close, 9=ping, 10=pong
    data str
    fin bool
}

; Сервер
s = ws.listen-on(host, port); создать прослушивание (?ws-server)
c = s.accept(); принять соединение (?ws-server-conn)
msg = c.recv(); принять сообщение (?ws-message)
ok = c.send-text(text); отправить текст
ok = c.send-binary(data); отправить двоичные данные
c.close()

; Клиент
c = ws.connect(url); подключиться к серверу (?ws-client)
msg = c.recv(); принять сообщение (?ws-message)
ok = c.send-text(text); отправить текст
ok = c.send-binary(data); отправить двоичные данные
c.close()
```

### net/tls — клиент TLS 1.2/1.3 (чистая реализация на Nolang)

Предоставляет шифрованные TLS-соединения с поддержкой TLS 1.2 и 1.3:

```no
; Соединение
c = tls.dial(host, port); установить TLS-соединение (?tls.conn)
n = c.send(data); отправить зашифрованные данные (?i64)
n = c.recv(buf, n); принять расшифрованные данные (?i64)
c.close()
```

### net/client — высокоуровневый TCP-клиент

Инкапсулирует структуру `conn` и предоставляет автоматическое переподключение и др.:

```no
c = client.client-create(host, port); создать клиент (?client)
ok = c.connect(host, port); подключиться
ok = c.reconnect(); переподключиться
written = c.send(data); отправить
read-n = c.recv(buf, n); принять
line = c.recv-line(); принять строку (?str)
written = c.send-line(line); отправить строку (i64)
response = c.request(data); режим запрос-ответ (?str)
yes = c.is-connected(); состояние соединения
c.close()
```

### net/quic — протокол QUIC (RFC 9000)

Предоставляет реализацию транспортного протокола QUIC как нижележащего транспортного уровня для HTTP/3:

```no
c = quic.dial(host, port); установить QUIC-соединение (?quic.conn)
n = c.send(data, n); отправить данные
n = c.recv(buf, n); принять данные
c.close()
```

### net/server — HTTP-сервер

Предоставляет функциональность HTTP-сервера:

```no
s = server {}
ok = s.listen(host, port); начать прослушивание
c = s.accept(); принять одно соединение (?server-conn)
s.close()
```

### net/dns — разрешение DNS

Предоставляет функции DNS-запросов:

```no
ip = dns.resolve(host); разрешить имя хоста (?str)
```

### net/url — разбор URL

Предоставляет разбор и построение URL:

```no
u = url.url-parse(url); разобрать URL
s = u.to-str(); преобразовать в строку
```

### net/cookie — HTTP Cookie

Предоставляет разбор и управление HTTP Cookie:

```no
c = cookie {}
c.parse(set-cookie-header)
s = c.to-str()
```

### net/multipart — данные формы Multipart

Предоставляет разбор и построение multipart/form-data:

```no
; Запись (построение multipart/form-data)
w = multipart.multipart-create(); создать writer (?multipart-writer)
w.write-field(name, value); записать поле
body = w.body-of(); получить закодированный body
ct = w.content-type(); получить content-type (с boundary)

; Разбор
r = multipart.multipart-parse(content-type, body); разобрать (?multipart-reader)
```

### net/hpack — сжатие заголовков HPACK (HTTP/2)

Предоставляет кодирование/декодирование алгоритма HPACK для сжатия заголовков HTTP/2:

```no
; Работа с таблицами заголовков
t = hpack.tables {}
t.init(); инициализировать статическую и динамическую таблицы
out = t.encode-header(name, value); закодировать один заголовок (str)
names, values, count, ok = t.decode-headers(data); декодировать заголовки ([]str)
t.dyn-add(name, value); добавить в динамическую таблицу
idx = t.find(name, value); искать по полному name/value
idx = t.find-name(name); искать только по name
name, value, ok = t.lookup(idx); искать по индексу

; Кодирование/декодирование примитивов int/str
out = hpack.encode-int(val, prefix-bits, prefix-byte)
val, next-pos = hpack.decode-int(data, pos, prefix-bits)
out = hpack.encode-str(s)
out, next-pos = hpack.decode-str(data, pos)
```

### net/proxy — поддержка прокси

Предоставляет подключение через HTTP/SOCKS-прокси:

```no
c = proxy.dial(proxy-url, target-host, target-port)
```

### net/pool — пул соединений

Предоставляет пулинг сетевых соединений с их переиспользованием для повышения производительности:

```no
p = pool {}
p.init(capacity)
c = p.get(); получить соединение из пула
p.put(c); вернуть соединение в пул
p.close()
```

### net/unix — доменные сокеты Unix

Предоставляет взаимодействие через доменные сокеты Unix:

```no
fd = unix.unix-listen(path); прослушивать (возвращает fd)
fd = unix.unix-dial(path); подключиться (возвращает fd)
l = unix.unix-listen-on(path); создать слушатель (?unix-listener)
c = l.accept(); принять соединение (?unix-conn)
```

---
