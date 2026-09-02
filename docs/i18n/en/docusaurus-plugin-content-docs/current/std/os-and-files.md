---
sidebar_position: 3.3
---

## Operating System and Files

### os — Operating System Interface

Provides environment variables, directory operations, process management, system information, time, and more. For file read/write functionality, see the `fs` module.

```no
; Environment variables
val = os.get-env(key)
os.set-env(key, val)

; Directory
dir = os.get-wd()
os.ch-dir(dir)
os.mkdir(path, mode)

; Process
os.exit(code)
pid = os.get-pid()

; System information
name = os.host-name()
arch = os.get-arch()
msg = os.strerror(errnum)

; Time
sec = os.now()
ms = os.now-ms()
us = os.now-us()
ns = os.now-ns()
os.sleep(sec)
os.sleep-us(us)
os.sleep-ns(ns)

; Command-line arguments
count = os.args()
val = os.arg(idx)
```

### fs — File System Utilities

Wraps an open file with the `file` struct and a path with the `path` struct. Defines the `fd` newtype (`fd = i64`) to prevent file descriptors from being confused with arbitrary `i64` values. Also provides an `embed` struct for compile-time embedded filesystems (via `#{embed='dir/path'}`).

```no
; fd newtype (underlying i64, type-distinct from i64)
fd = i64

; File struct
file {
    fd fd
    path str
}

; Standard files (integer literals are allowed for fd initialization)
stdin = file{
    fd: 0
    path: '<stdin>'
}
stdout = file{
    fd: 1
    path: '<stdout>'
}
stderr = file{
    fd: 2
    path: '<stderr>'
}

; Standard file descriptors
STDIN-FD fd = 0
STDOUT-FD fd = 1
STDERR-FD fd = 2

; Open file (with options)
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
f = fs.open(path, opts)             ; Open file, returns ?file on success, err on failure

; file methods
read-n = f.read(buf, n)              ; Read up to n bytes into buf
line = f.read-line()                  ; Read one line (?str, nil=EOF or error)
content = f.read-bytes()             ; Read entire file as ?[]byte (nil=empty, err=failure)
content = f.read-str()               ; Read entire file as ?str (nil=empty, err=failure)
written = f.write(data, n)           ; Write n bytes from data
ok = f.write-str(data)               ; Write string to file (overwrite), returns ?bool
ok = f.write-bytes(data)             ; Write []byte to file (overwrite), returns ?bool
ok = f.append(data, n)               ; Append data to file
ok = f.copy-to(dst-path)             ; Copy file to target path
ok = f.close()                       ; Close (standard files stdin/stdout/stderr are not closed)
yes = f.is-open()                    ; Check if file is open (fd >= 0)
sz = f.size()                        ; File size via fstat(fd), eliminates TOCTOU

; Built-in functions (registered in src/builtin/os.go, documented as comments in fs.no)
; These are ForwardFunc/CLibCall builtins — no .no implementation needed.

; File read/write (deprecated, prefer read-bytes/write-bytes for error handling)
; [deprecated] read-file: use read-bytes instead (supports error handling, TOCTOU safe, loop read)
;   empty slice means failure, cannot distinguish empty file from error
; [deprecated] write-file: use write-bytes instead (supports error handling, loop write)
;   cannot distinguish partial write from total failure
data = fs.read-file(path)           ; [deprecated] Read entire file as []byte (empty on error)
ok = fs.write-file(path, data)      ; [deprecated] Write []byte to file (overwrite). Returns true on success
fd = fs.open-read(path)             ; Open read-only (O_RDONLY)
fd = fs.open-write(path)            ; Open for writing (O_WRONLY|O_CREAT|O_TRUNC, 0644)
fd = fs.open-file(path, flags, mode) ; Open with custom flags and permissions
n = fs.read(fd, buf, n)             ; Low-level read from fd into buf
written = fs.write(fd, data, n)     ; Low-level write to fd
ok = fs.close(fd)                   ; Low-level close

; File management
ok = fs.remove(path)                ; Delete file (unlink)
ok = fs.rename(old, new)            ; Rename file
ok = fs.symlink(target, linkpath)   ; Create symbolic link
ok = fs.link(oldpath, newpath)      ; Create hard link
ok = fs.copy-file(src, dst)         ; Copy file content

; File information
ok = fs.exists(path)                ; Check if path exists (follows symlinks)
ok = fs.is-file(path)               ; Check if regular file
ok = fs.is-dir(path)                ; Check if directory
sz = fs.stat-size(path)             ; Get file size (returns ?i64)
sz = fs.file-size(path)             ; Same as stat-size (returns ?i64)
sz = fs.fstat-size(fd)              ; Get file size via fstat(fd), eliminates TOCTOU (returns ?i64)
mode = fs.stat-mode(path)           ; Get file mode (st_mode, returns i64)
uid = fs.stat-uid(path)             ; Get file owner uid (returns i64)
gid = fs.stat-gid(path)             ; Get file group gid (returns i64)
mtime = fs.stat-mtime(path)         ; Get file modification time (Unix seconds, returns i64)
ok = fs.lstat(path)                 ; Get symlink info (does not follow link target)

; Directory operations
dirp = fs.open-dir(path)             ; Open directory (returns handle, 0 on failure)
name = fs.read-dir(dirp)             ; Read next dir entry (returns str, ok bool)
ok = fs.close-dir(dirp)              ; Close directory handle
names = fs.list-dir(path)            ; List all entry names (includes . and ..)
names = fs.dir-entries(path)          ; List entries (excludes . and ..)
paths = fs.walk(root)                ; Recursively walk directory tree

; Path resolution
abs = fs.realpath(path)              ; Resolve to absolute canonical path
target = fs.readlink(path)           ; Read symbolic link target (returns str, ok bool)

; Temp file/directory
name, fd = fs.mkstemp(tmpl)          ; Create temp file (template ending XXXXXX)
name = fs.mkdtemp(tmpl)             ; Create temp directory (template ending XXXXXX)

; Special files
ok = fs.mkfifo(path, mode)          ; Create named pipe (FIFO)
ok = fs.mknod(path, mode, dev)      ; Create special file (device node)
ok = fs.truncate(path, length)      ; Truncate/extend file to length
fs.sync()                           ; Flush filesystem buffers to disk
ok = fs.touch-file(path)            ; Update file timestamps to current time
ok = fs.utime(path, atime, mtime)   ; Set file access and modification times
ok = fs.rmdir(path)                 ; Remove empty directory

; Standard input
line = fs.get-line()                ; Read one line from stdin (?str, nil=EOF)

; Convenience wrappers (Nolang-implemented in fs.no, call builtins internally)
content = fs.read-str(path)          ; Read entire file as ?str (nil=empty, err=failure)
content = fs.read-bytes(path)        ; Read entire file as ?[]byte (nil=empty, err=failure)
ok = fs.write-str(path, data)        ; Write string to file (overwrite), returns ?bool
ok = fs.write-bytes(path, data)      ; Write []byte to file (overwrite), returns ?bool

; embed: compile-time embedded filesystem (initialized by #{embed='dir/path'})
;   #{embed='../frontend/dist'}
;   DIST fs.embed
;   data = DIST.read('index.html')   ; Read embedded file, returns ([]byte, bool)
;   ok = DIST.exists('css/style.css') ; Check if embedded file exists
embed {
    count i64
    paths str                        ; All paths concatenated (\0 separated)
    pathStarts []i64                 ; Start offset of each path in paths
    pathLens []i64                   ; Length of each path
    blob []byte                      ; All file contents concatenated
    dataStarts []i64                 ; Start offset of each file in blob
    dataLens []i64                   ; Length of each file
}

; Windows-specific (only available on win-amd64/win-arm64)
bufptr = fs.win-find-first-file(path) ; FindFirstFileA, returns handle (0=failure)
name = fs.win-find-next-file(bufptr)  ; FindNextFileA, returns (name, ok)
ok = fs.win-find-close(bufptr)        ; FindClose

; open() flag constants (platform-specific, shown for macOS)
O-RDONLY = 0
O-WRONLY = 1
O-RDWR = 2
O-CREAT = 512       ; macOS=512, Linux=64, Windows=256
O-TRUNC = 1024      ; macOS=1024, Linux=512, Windows=512
O-APPEND = 8        ; macOS=8, Linux=1024, Windows=8
O-EXCL = 2048       ; macOS=2048, Linux=128, Windows=1024
```

### env — Environment Variables (Simplified Wrapper)

```no
val = env.get(key)
val = env.lookup(key)               ; Returns ?str (nil=not found)
env.set(key, val)
env.unset(key)
val = env.get-with-default(key, default)
ok = env.is-set(key)
```

### args — Command-Line Arguments

```no
n = args.count()
arg = args.get(i)
name = args.program()
ok = args.has-flag(name)
val = args.get-option(name)
arg = args.get-positional(i)
```

### path — Path Operations

Wraps a path string with the `path` struct; all operations are provided as methods:

```no
SEP = 47     ; '/' (ASCII)
DOT = 46     ; '.'

; Struct
path {
    p str
}

; Path join and split (modifies .p in place)
p = path{
    p: '/a/b/c.txt'
}
p.join(b str)           ; Join two paths (modifies in place)
p.base() (out)           ; Get filename
p.dir()                  ; Get directory (modifies .p in place)
p.ext() (out)            ; Get extension
p.clean()                ; Normalize (modifies .p in place)
p.split() (f str)        ; Split into directory + filename (.p becomes directory, returns filename)

; Path checks
p.is-abs() (yes bool)    ; Whether it is an absolute path

; File system operations (delegated to fs built-in functions)
p.exists() (yes bool)        ; Whether it exists
p.is-dir() (yes bool)        ; Whether it is a directory
p.is-file() (yes bool)       ; Whether it is a file
p.size() (sz i64)            ; File size
p.make-dir() (ok bool)       ; Create directory
p.remove() (ok bool)         ; Delete
p.rename(new-p str) (ok bool)    ; Rename
p.change-dir() (ok bool)     ; Change working directory

; Constructor methods
path.current() (out path)    ; Get current working directory
```

### bufio — Buffered Reading

```no
r = reader.init(fd, buf)       ; Initialize buffered reader (returns reader)
ok = reader.fill()              ; Fill buffer
b = reader.read-byte()          ; Read one byte (?byte, nil=EOF)
ok = reader.read-line(line)     ; Read one line into line
reader.close()                  ; Close
```

### io — Input/Output Abstraction

Provides `io-reader` and `io-writer` structs to unify read/write operations across files, standard input/output, and other streams:

```no
; Standard file descriptors
STDIN-FD = 0, STDOUT-FD = 1, STDERR-FD = 2

; io-reader struct
io-reader {
    fd i64
}
r = io-reader.from-fd(fd)      ; Create from fd
r = io-reader.from-stdin()     ; Create from standard input
read-n = r.read(buf, n)        ; Read n bytes
b = r.read-byte()              ; Read one byte (?byte, nil=EOF)
line = r.read-line()           ; Read one line (?str, nil=EOF)
total = r.read-all(buf, size)  ; Read all

; io-writer struct
io-writer {
    fd i64
}
w = io-writer.from-fd(fd)      ; Create from fd
w = io-writer.from-stdout()    ; Create from standard output
w = io-writer.from-stderr()    ; Create from standard error
written = w.write(data, n)     ; Write n bytes
written = w.write-str(s)       ; Write entire string
written = w.write-byte(b)      ; Write one byte
written = w.write-line(s)      ; Write string + newline

; Convenience functions
n = io.out(s)                   ; Write to stdout (no newline)
n = io.outln(s)                 ; Write to stdout (with newline)
n = io.err(s)                   ; Write to stderr (no newline)
n = io.errln(s)                 ; Write to stderr (with newline)
line = io.read-line()           ; Read one line from stdin (?str, nil=EOF)
```

### regexp — Regular Expressions

Wraps a pattern with the `regexp` struct, backed by the C standard library `regex.h`:

```no
; Struct
regexp {
    pattern str
}

; Methods
re = regexp{
    pattern: '^hello'
}
matched = re.matches(text)        ; Check whether it matches
result = re.find(text)           ; Find the first matching substring
```

### process — Process Operations

Provides process creation, standard stream access, process waiting, and process information querying. Backed by POSIX fork/exec/pipe/waitpid:

```no
; Signal constants
SIG-TERM = 15, SIG-KILL = 9, SIG-INT = 2, SIG-STOP = 19, SIG-CONT = 18, SIG-CHLD = 17
WNOHANG = 1

; Struct
process {
    pid i64
    stdin-fd i64
    stdout-fd i64
    stderr-fd i64
    exit-code i64
    running i64
}

; Process creation
p = process.new()
ok = p.start-with-opts(program, args, dir, input, merge-err) ; Start child process (no wait)
ok = p.start(program, arg)          ; Convenience: fork + exec, captures stdout

; Process waiting
status = p.wait()                   ; Block waiting for child process to end
status = p.wait-nohang()            ; Non-blocking poll; nil=still running

; Process control
ok = p.kill(sig)                    ; Send signal
ok = p.terminate()                  ; SIG-TERM
ok = p.force-kill()                 ; SIG-KILL

; Standard stream operations
read-n = p.read(buf, n)             ; Read from stdout
line = p.read-line()               ; Read one line (?str, nil=EOF)
content, n = p.read-all()           ; Read all stdout
written = p.write(data, n)          ; Write to stdin
p.close-stdin()                    ; Close stdin pipe
p.close-stdout()                   ; Close stdout pipe
p.close-stderr()                   ; Close stderr pipe

; Process information
pid = p.pid-of()                    ; Child process ID
code = p.exit-code-of()             ; Exit code
yes = p.is-running()                ; Whether it is still running
pid = process.parent-pid()          ; Parent process ID

; Lifecycle
p.close()                          ; Close all pipes and wait

; Cross-platform one-shot execution (recommended): fork/exec + capture stdout/stderr, with timeout and environment
;   Inputs: program path, args slice, dir working directory, input written to child stdin,
;           env K=V environment-variable slice, timeout in ms (<=0 = no limit), merge-err true merges stderr into out
;   Outputs: out captured stdout (stderr merged depending on merge-err), stderr captured stderr output, code exit code (-2=timeout kill, -1=start failure), err error message
out, stderr, code, err = process.cmd('echo', ['hello'], '', '', [], 0, false)

; Struct-options form (recommended, more readable): demonstrates cross-module
; struct forwarding — cmdopts is defined in this module, while the caller
; constructs process.cmdopts{...} in another module and passes it by reference.
cmdopts {
    dir str        ; working directory; empty = inherit parent's
    stdin str      ; data written to child stdin; empty = no stdin
    env []str      ; environment variables (["K=V", ...]); empty = inherit parent's
    timeout i64    ; timeout in ms; <=0 = no limit
    merge-err bool  ; true = merge stderr into captured out
}
o = process.cmdopts{
    dir: '',
    stdin: 'piped-data',
    env: ['K=V'],
    timeout: 200,
    merge-err: false
}
out, code, err = process.exec('echo', ['hello'], o)

; Convenience functions (legacy, pending decision)
status = process.process-run(cmd)           ; Execute shell command
content, code = process.new().output(program, arg) ; Execute and capture output
```

### net — Network Operations

Provides TCP networking capabilities, including server listening, client connections, and data sending/receiving. Backed by the POSIX socket API:

```no
; Network constants
AF-INET = 2, SOCK-STREAM = 1, SOL-SOCKET = 65535, SO-REUSEADDR = 4, BACKLOG = 128

; listener struct
listener {
    fd i64
}

; Listen operations
l = listener{}
ok = l.listen(host, port)            ; Create TCP listener (socket+setsockopt+bind+listen)
c = l.accept()                       ; Accept connection (?conn, nil=no connection)
l.close()                           ; Close listening socket
fd = l.fd-of()                       ; Get fd

; conn struct
conn {
    fd i64
}

; Connection operations
c = conn{}
ok = c.dial(host, port)              ; Establish TCP connection (socket+connect)
written = c.send(data)               ; Send string
read-n = c.recv(buf, n)              ; Receive data into buf
line = c.recv-line()                 ; Receive one line (?str, nil=EOF, up to 4096 bytes)
content, total = c.recv-all()        ; Receive all until connection closed
c.close()                           ; Close connection
fd = c.fd-of()                       ; Get fd

; Convenience functions
l = net.net-listen-on(host, port)        ; Create listener and start listening (?listener)
c = net.net-dial-to(host, port)          ; Create connection and dial (?conn)
```

### net/ip — IP Address Operations

Provides parsing, validation, conversion, and classification of IPv4 addresses. Pure Nolang implementation:

```no
; Default address constants
IP-ZERO       ; 0.0.0.0
IP-LOOPBACK   ; 127.0.0.1
IP-ANY        ; 0.0.0.0
IP-BROADCAST  ; 255.255.255.255

; ip-addr struct
ip-addr {
    a i64
    b i64
    c i64
    d i64
}

; Parsing and conversion
ip = ip-addr{}
ok = ip.parse('192.168.1.1')         ; Parse from string
s = ip.to-str()                      ; Convert to string '192.168.1.1'
v = ip.to-u32()                      ; Convert to u32 (big-endian)
ip.from-u32(v)                      ; Create from u32

; Address classification
yes = ip.is-loopback()               ; 127.0.0.0/8
yes = ip.is-private()                ; 10/8, 172.16/12, 192.168/16
yes = ip.is-zero()                   ; 0.0.0.0
yes = ip.is-broadcast()              ; 255.255.255.255
yes = ip.is-multicast()              ; 224.0.0.0/4
yes = ip.is-link-local()             ; 169.254.0.0/16
yes = ip.is-class-a()                ; Class A (1~126)
yes = ip.is-class-b()                ; Class B (128~191)
yes = ip.is-class-c()                ; Class C (192~223)

; Comparison and subnet
yes = ip.equal(other)                ; Address equality comparison
yes = ip.in-subnet(base, prefix-len) ; Subnet containment check

; Convenience functions
addr = ip.ip-parse(s)                   ; Quick parse (?ip-addr, nil=invalid)
yes = ip.ip-is-loopback(s)              ; Quick loopback check
yes = ip.ip-is-private(s)               ; Quick private check
```

### net/sse — Server-Sent Events Client

Supports SSE streaming reception compliant with the W3C EventSource specification. Backed by HTTP/1.1 long connections, supporting both plaintext HTTP and HTTPS (TLS):

```no
; sse-event struct
sse-event {
    event str       ; Event type (default 'message')
    data str        ; Event data (multiple data lines joined with \n)
    id str          ; Event ID
    retry i64       ; Reconnect wait milliseconds (-1=not set)
}

; sse-client struct
sse-client {
    fd i64              ; TCP socket fd
    tls-c tls.conn      ; TLS connection
    use-tls bool        ; Whether TLS is used
    connected bool      ; Connection state
    host str            ; Server hostname
    port i64            ; Port number
    path str            ; Request path
    last-event-id str   ; Last received event ID
    recv-buf str        ; Receive buffer
    recv-buf-len i64    ; Buffer data length
}

; Connection and event reception
client = sse.connect('http://host:3000/events')  ; Returns ?sse-client
client: {
    nil -> print('connect failed')
    ->
        ev = client.next-event()     ; Returns ?sse-event (nil=EOF, err=error)
        ev: {
            nil -> print('connection closed')
            err -> print('error: ' - it)
            -> print(ev.data)
        }
        client.close()
}

; Other methods
yes = client.is-connected()         ; Check connection state
ok = client.reconnect()             ; Reconnect (using last-event-id)
```

### net/http — HTTP/1.1 Client

Provides an HTTP/1.1 protocol client supporting GET, POST, PUT, DELETE, PATCH and other methods, with optional TLS:

```no
; Structs
http-request {
    method str
    url str
    body str
    headers [16]str
    header-count i64
}
http.response {
    status-code i64
    status-text str
    headers str
    header-names [32]str
    header-values [32]str
    header-count i64
    body str
}

; Convenience functions
resp = http.get(url)                        ; GET request (?http.response)
resp = http.post(url, body)                  ; POST request (?http.response)
resp = http.do(method, url, body)            ; Custom method (?http.response)

; Using a request object
req = http-request{}
req.init('POST', url, body)
req.add-header('Content-Type', 'application/json')
resp = http.do-req(req)                      ; Send request (?http.response)

; Parse response headers
resp.parse-headers()
```

### net/http2 — HTTP/2.0 Client (RFC 7540)

Supports HTTP/2 frame parsing and connection management, supporting h2c prior knowledge mode:

```no
; Frame struct
http2-frame {
    length i64
    frame-type i64
    flags i64
    stream-id i64
    payload str
}

; Connection struct
http2-conn {
    fd i64
    next-stream-id i64
    send-window i64
    recv-window i64
    initialized bool
    use-tls bool
}

; Connection and request
c = http2.connect(host, port)                ; Establish connection (?http2-conn)
resp = http2.do(method, url, body)           ; Send request (?http.response)

; Frame operations
frame = http2-frame{}
pos = frame.parse(data, pos)                 ; Parse frame (?i64)
pos = frame.serialize(buf, pos)              ; Serialize frame
ok = c.send-frame(frame)                     ; Send frame
frame = c.recv-frame()                       ; Receive frame (?http2-frame)
```

### net/http3 — HTTP/3.0 Client (RFC 9114)

HTTP/3 client based on the QUIC protocol:

```no
; Method constants
HTTP3-METHOD-GET = 'GET'
HTTP3-METHOD-POST = 'POST'
HTTP3-METHOD-PUT = 'PUT'
HTTP3-METHOD-DELETE = 'DELETE'
HTTP3-METHOD-PATCH = 'PATCH'
HTTP3-METHOD-HEAD = 'HEAD'
HTTP3-METHOD-OPTIONS = 'OPTIONS'

; Convenience functions
c = http3.connect(host, port)                ; Establish QUIC connection (?http3-conn)
resp = http3.send-request(c, method, path, headers, body) ; Send request (?http.response)
resp = http3.get(url)                        ; GET request (?http.response)
resp = http3.post(url, body)                 ; POST request (?http.response)

; QPACK header encoding/decoding
buf, n = http3.qpack-encode-header(name, value)
buf, n = http3.qpack-encode-headers(names, values, count)
name, value, pos = http3.qpack-decode-header(buf, pos)
```

### net/ws — WebSocket Client and Server (RFC 6455)

Supports full-duplex communication over the WebSocket protocol, usable as either client or server:

```no
; Message struct
ws-message {
    opcode i64           ; 0=continuation, 1=text, 2=binary, 8=close, 9=ping, 10=pong
    data str
    fin bool
}

; Server
s = ws.listen-on(host, port)                 ; Create listener (?ws-server)
c = s.accept()                               ; Accept connection (?ws-server-conn)
msg = c.recv()                               ; Receive message (?ws-message)
ok = c.send-text(text)                       ; Send text
ok = c.send-binary(data)                     ; Send binary
c.close()

; Client
c = ws.connect(url)                          ; Connect to server (?ws-client)
msg = c.recv()                               ; Receive message (?ws-message)
ok = c.send-text(text)                       ; Send text
ok = c.send-binary(data)                     ; Send binary
c.close()
```

### net/tls — TLS 1.2/1.3 Client (Pure Nolang Implementation)

Provides TLS encrypted connections, supporting TLS 1.2 and 1.3:

```no
; Connection
c = tls.tls-dial(host, port)                     ; Establish TLS connection (?tls.conn)
n = c.send(data)                             ; Send encrypted data (?i64)
n = c.recv(buf, n)                           ; Receive decrypted data (?i64)
c.close()
```

### net/client — High-level TCP Client

Wraps the `conn` struct, providing features such as automatic reconnection:

```no
c = client.net-client(host, port)                   ; Create client (?client)
ok = c.connect(host, port)                   ; Connect
ok = c.reconnect()                           ; Reconnect
written = c.send(data)                       ; Send
read-n = c.recv(buf, n)                      ; Receive
line = c.recv-line()                         ; Receive one line (?str)
response = c.request(data)                   ; Request-response pattern (?str)
yes = c.is-connected()                       ; Connection state
c.close()
```

### net/quic — QUIC Protocol (RFC 9000)

Provides an implementation of the QUIC transport protocol, serving as the underlying transport layer for HTTP/3:

```no
c = quic.dial(host, port)                    ; Establish QUIC connection (?quic.conn)
n = c.send(data, n)                          ; Send data
n = c.recv(buf, n)                           ; Receive data
c.close()
```

### net/server — HTTP Server

Provides HTTP server functionality:

```no
s = server{}
ok = s.listen(host, port)                    ; Start listening
ok = s.serve()                               ; Handle requests
s.close()
```

### net/dns — DNS Resolution

Provides DNS query functionality:

```no
ip = dns.resolve(host)                       ; Resolve hostname (?str)
```

### net/url — URL Parsing

Provides URL parsing and construction functionality:

```no
u = url.url-parse(url)                           ; Parse URL
s = u.to-str()                               ; Convert to string
```

### net/cookie — HTTP Cookie

Provides parsing and management of HTTP cookies:

```no
c = cookie{}
c.parse(set-cookie-header)
s = c.to-str()
```

### net/multipart — Multipart Form Data

Provides parsing and construction of multipart/form-data:

```no
out = multipart.multipart-encode(fields, boundary)
fields = multipart.multipart-parse(data, boundary)
```

### net/hpack — HPACK Header Compression (HTTP/2)

Provides encoding/decoding of the HPACK algorithm, used for HTTP/2 header compression:

```no
buf, n = hpack.encode(headers)
headers = hpack.decode(buf, n)
```

### net/proxy — Proxy Support

Provides HTTP/SOCKS proxy connection functionality:

```no
c = proxy.dial(proxy-url, target-host, target-port)
```

### net/pool — Connection Pool

Provides pooled management of network connections, reusing connections to improve performance:

```no
p = pool{}
p.init(capacity)
c = p.get()                                  ; Get connection from pool
p.put(c)                                     ; Return connection to pool
p.close()
```

### net/unix — Unix Domain Sockets

Provides Unix domain socket communication:

```no
fd = unix.unix-listen(path)                       ; Listen
fd = unix.unix-dial(path)                         ; Connect
fd = unix.unix-accept(listen-fd)                  ; Accept connection
```

---
