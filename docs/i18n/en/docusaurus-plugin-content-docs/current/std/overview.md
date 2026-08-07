---
sidebar_position: 3
---

# Standard Library

The Nolang standard library (`src/std/`) contains 60+ modules, covering formatting, math, strings, data structures, encoding/decoding, encryption, compression, file operations, I/O abstractions, and more.

Usage: `# std/xxx` (core modules require no import).

> **The legacy `use std/xxx` syntax still works but is deprecated; the new `# std/xxx` syntax is recommended.**

> **Note: All code examples in this document follow the "one statement per line" rule—using semicolons `;` or commas `,` to put multiple statements on one line is forbidden.** For example, `out = from-i64(v), out = from-u64(v)` is incorrect and should be split across multiple lines.

---

## Basic Types

### types — Type Definitions

Mapping of Nolang types to LLVM:

| Nolang           | LLVM                                               |
| ---------------- | -------------------------------------------------- |
| `bool`           | `i1`                                               |
| `byte`           | `i8`                                               |
| `char`           | `i32`                                              |
| `i8/i16/i32/i64` | `i8/i16/i32/i64`                                   |
| `u8/u16/u32/u64` | `i8/i16/i32/i64`                                   |
| `f32`            | `float`                                            |
| `f64`            | `double`                                           |
| `str`            | union (short: `[127]byte` / long: `{*byte, i64}`) |

**Composite types:**

- **Variable-length array `[]t`**: underlying `{ t*, i64 }` (data, len)
- **Fixed-length array `[n]t`**: LLVM fixed-size array
- **String `str`**: union type (short ≤127 bytes stored on stack / long stored on heap), supports `s[i]`, `s[i..j]`, `s + t`
- **Enum/Union**: `option` tagged enum (`ok t` / `nil` / `err str`)
- **Struct**: must be defined across multiple lines; fields are not comma-separated
- **Map**: underlying linked-hash-map
- **Iterator**: `for iter.next() {}` (interface method `next() (ok bool)`)

### option — Option Type

`option<t>` tagged enum (tag=0=val, 1=nil, 2=err):

```no
x ?t                ; Declare option<t>
x = 42              ; Set to a value
x = nil             ; Set to nil
x = err('msg')      ; Set to an error

; match
x: {
    val -> f(it)
    nil ->
    err -> g(it)
}  
```

**Style guide:** When a function may fail or return an empty value, use the `?t` option instead of `(val, ok bool)`. `?t` has three states: `ok` (has value), `nil` (empty/normal absence), `err` (error). The normal value is bound implicitly. For example, `pop()` returns `?i64` (`nil` = empty), `read-line()` returns `?str` (`nil` = EOF, `err` = error), `lookup()` returns `?str` (`nil` = not found). See the syntax documentation for details.

---

## Core Library

### fmt — Formatted Output

Nolang uses **named format strings** `{name[:spec]}`, referencing variables directly from scope — no positional arguments. Output goes through `io.out`/`io.err` syscalls, without depending on libc `printf`.

```no
print('x={x}')                 ; Named format, auto-appends newline (stdout)
eprint('err {x}')              ; Named format, auto-appends newline (stderr)
print('id {id:06} amount {money:.2f}')  ; Supports align/fill/width/precision
s = format('x={x}')            ; Returns formatted string (replaces sprintf)
io.out('no-newline-here')      ; Low-level command, no newline (stdout)
io.err('err-no-newline')       ; Low-level command, no newline (stderr)
; printf/eprintf/sprintf are deprecated: printf→io.out, eprintf→io.err, sprintf→format
; io.err carries the module prefix and will not conflict with the Option constructor err()
```

### math — Math Functions

**Constants:** `math.PI`, `math.E`

**Basic:** `math.abs`, `math.sqrt`

**Trigonometric:** `math.sin`, `math.cos`, `math.tan`, `math.asin`, `math.acos`, `math.atan`, `math.atan2`, `math.degrees`, `math.radians`

**Hyperbolic:** `math.sinh`, `math.cosh`, `math.tanh`

**Rounding:** `math.ceil`, `math.floor`, `math.round`, `math.trunc`

**Exponential/Logarithm:** `math.exp`, `math.log`, `math.log10`, `math.log2`, `math.pow`, `math.hypot`, `math.cbrt`

**Others:** `math.fmod`, `math.max`, `math.min`

### char — Character Operations

char is essentially i32 (a Unicode code point); all operations are provided as methods:

```no
c char = 'A'
c.is-digit()       ; Whether it is a digit (0-9) (method)
c.is-letter()      ; Whether it is a letter (a-z, A-Z) (method)
c.is-alpha()       ; Alias for is-letter (method)
c.is-alnum()       ; Whether it is a letter or digit (method)
c.is-space()       ; Whether it is a whitespace character (method)
c.is-upper()       ; Whether it is an uppercase letter (method)
c.is-lower()       ; Whether it is a lowercase letter (method)
c.to-upper()       ; Convert to uppercase (ASCII) (method)
c.to-lower()       ; Convert to lowercase (ASCII) (method)
c.to-bytes()       ; Unicode -> UTF-8 bytes (method)
c.to-str()         ; Unicode -> string (UTF-8, method)
```

### str — String Operations

```no
ok = a.eq(b, n)               ; Equality comparison (method)
dst = s.copy()                ; String copy (method)
s.fill(val byte)              ; Fill with byte value (method)
pos = s.index(sub)            ; Substring position
ok = s.contains(sub)          ; Whether it contains substring
ok = s.starts-with(sub)       ; Prefix check
ok = s.ends-with(sub)         ; Suffix check
s.to-upper()                  ; Convert to uppercase
s.to-lower()                  ; Convert to lowercase
out = s.trim()                ; Trim leading/trailing whitespace
out = s.repeat(n)             ; Repeat
out = s.slice(start, end)     ; Slice
b = s.to-bytes()              ; Convert to []byte
s = b.to-str()                ; []byte to str (method)
v = s.to-i64()                ; String to i64 (returns ?i64)
v = s.to-i8()                 ; String to i8 (returns ?i8)
v = s.to-i16()                ; String to i16 (returns ?i16)
v = s.to-i32()                ; String to i32 (returns ?i32)
v = s.to-u8()                 ; String to u8 (returns ?u8)
v = s.to-u16()                ; String to u16 (returns ?u16)
v = s.to-u32()                ; String to u32 (returns ?u32)
v = s.to-u64()                ; String to u64 (returns ?u64)
v = s.to-byte()               ; String to byte (returns ?byte)
v = s.to-f64()                ; String to f64 (returns ?f64)
v = s.to-bool()               ; String "true"/"false" to bool (returns ?bool)
s = v.to-str()                ; i64 to string (method)
out = s.reverse()             ; Reverse
c = s.compare(b)              ; Lexicographic comparison
n = s.count()                 ; Total number of code points
val = s.replace-char(old, new) ; Replace character (returns resulting string)
out = s.trim-char(c)          ; Trim specified character
ok = s.empty()                ; Whether it is empty
s.clear()                     ; Clear (len=0, in-place)
s = with-cap(cap)            ; Builtin: create new string with specified capacity (len=0)
s = with-len(len)            ; Builtin: create new string with specified length (len=cap)
s = with-cap-len(cap, len)   ; Builtin: create new string with specified capacity and length
parts = s.split(sep)          ; Split by separator (returns []str, method)
out = ss.join(sep)            ; Join []str with separator (method)
```

### number — Numeric Operations

```no
number.max(a, b)                     ; Maximum
number.min(a, b)                     ; Minimum
r = num.clamp(lo, hi)         ; Clamp to range (method)
r = number.abs(a)                    ; Absolute value (number generic)
r = num.sign()                ; Sign (-1/0/1, method)
number.even(v)                       ; Even/odd check
number.odd(v)
number.gcd(a, b)                     ; Greatest common divisor
number.lcm(a, b)                     ; Least common multiple
r = number.pow(a, n)                 ; Integer power
number.i64-to-f64(v)                 ; Numeric conversion
number.f64-to-i64(v)
s = int.to-str()              ; i64 to string (method)
q = number.div(a, b)                 ; Integer division quotient
r = number.mod(a, b)                 ; Modulo
number.swap(a, b)                    ; Swap
yes = float.is-nan()          ; NaN check (method)
yes = float.is-inf()          ; Inf check (method)

; Range constants
i8.MIN / MAX                  ; -128 / 127
i16.MIN / MAX                 ; -32768 / 32767
i32.MIN / MAX                 ; -2147483648 / 2147483647
i64.MIN / MAX                 ; -2^63 / 2^63-1
u8.MIN / MAX                  ; 0 / 255
u16.MIN / MAX                 ; 0 / 65535
u32.MIN / MAX                 ; 0 / 4294967295
u64.MIN / MAX                 ; 0 / 2^64-1
```

### byte — Byte Operations

```no
out = i64.to-bytes-be()         ; i64 -> big-endian [8]byte
out = i64.to-bytes-le()         ; i64 -> little-endian [8]byte
v = []byte.to-i64-be()          ; big-endian []byte -> i64 (1~8 bytes)
v = []byte.to-i64-le()          ; little-endian []byte -> i64 (1~8 bytes)
s = []byte.to-str()             ; []byte to str (method)
s = []byte.to-hex()             ; []byte -> uppercase hex string
s = []byte.to-hex-lower()       ; []byte -> lowercase hex string
s = byte.to-str()               ; byte to str (method)
```

### vec — Slice Operations

```no
v = vec.vec-create(n, val)         ; Create a slice of length n, filled with val
ok = []t.eq(a, b, n)           ; Equality comparison
n = []t.len()                  ; Length
[]t.push(val)                   ; Append (auto-grow)
[]t.clear()                     ; Clear (len=0, cap/data unchanged)
v = with-cap(cap)             ; Builtin: create new slice with specified capacity (len=0)
v = with-len(len)             ; Builtin: create new slice with specified length (len=cap)
v = with-cap-len(cap, len)    ; Builtin: create new slice with specified capacity and length
val, new-n = []t.pop()         ; Pop
found = []t.contains(n, val)   ; Whether it contains (n is length)
[]t.reverse(n)                  ; Reverse first n elements
[]t.clone(dst)                  ; Copy to dst
[]t.fill(n, val)                ; Fill first n elements
arr = []t.to-arr()             ; Convert to array
[]t.sort-asc()                  ; Sort ascending (method)
[]t.sort-desc()                 ; Sort descending (method)
```

### arr — Array Operations

```no
out = [n]t.clone()             ; Copy
ok = [n]t.eq(b)                ; Equality comparison
[n]t.fill(val)                  ; Fill
[n]t.reverse()                  ; Reverse
ok = [n]t.contains(val)        ; Whether it contains
v = [n]t.to-vec()              ; Convert to slice
v = [n]t.max()                 ; Maximum
v = [n]t.min()                 ; Minimum
v = [n]t.sum()                 ; Sum
i = [n]t.index-of(val)          ; Index
v = [n]t.last()                ; Last element
v = [n]t.first()               ; First element
[n]t.sort-asc()                 ; Sort ascending
[n]t.sort-desc()                ; Sort descending
```

### sort — Sort Constants

```no
sort.ast                         ; Ascending
sort.desc                        ; Descending
```

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

Wraps an open file with the `file` struct and a path with the `path` struct.

```no
; File struct
file {
    fd i64
    path str
}

; Standard files
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
f = fs.open(path, opts)             ; Open file, returns nil on failure

; file methods
read-n = f.read(buf, n)          ; Read up to n bytes
line = f.read-line()              ; Read one line (?str, nil=EOF)
content, n = f.read-all()        ; Read entire file
written = f.write(data, n)       ; Write n bytes
ok = f.write-all(data, n)        ; Write all (overwrite)
ok = f.append(data, n)           ; Append data
ok = f.copy-to(dst-path)         ; Copy to target path
ok = f.close()                   ; Close (standard files are not auto-closed)
yes = f.is-open()                ; Whether it is open
sz = f.size()                    ; File size

; Built-in functions
fd = fs.open-read(path)             ; Open read-only
fd = fs.open-write(path)            ; Open for writing (O_CREAT|O_TRUNC, 0644)
fd = fs.open-file(path, flags, mode) ; Open with custom flags
n = fs.read(fd, buf, n)             ; Low-level read
written = fs.write(fd, data, n)     ; Low-level write
ok = fs.close(fd)                   ; Low-level close
ok = fs.remove(path)                ; Delete file
ok = fs.rename(old, new)            ; Rename
ok = fs.is-file(path)               ; Check if it is a file
ok = fs.is-dir(path)                ; Check if it is a directory
sz = fs.stat-size(path)             ; Get file size
sz = fs.file-size(path)             ; Same as stat-size
line = fs.get-line()                ; Read a line from standard input (?str, nil=EOF)
ok = fs.copy-file(src, dst)         ; Copy file

; macOS open() flag constants
O-RDONLY = 0, O-WRONLY = 1, O-RDWR = 2
O-CREAT = 512, O-TRUNC = 1024, O-APPEND = 8, O-EXCL = 2048
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
p = process{}
ok = p.start(program, arg)          ; fork + exec, captures stdout
ok = p.start-with-stdin(program, arg) ; fork + exec, captures stdin + stdout

; Process waiting
ok = p.wait()                       ; Block waiting for child process to end
ok = p.wait-nohang()                ; Non-blocking poll

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
;           env K=V environment-variable slice, timeout in ms (<=0 = no limit), merge-err nonzero merges stderr into out
;   Outputs: out captured stdout (stderr merged depending on merge-err), code exit code (-2=timeout kill, -1=start failure), err error message
out, code, err = process.cmd('echo', ['hello'], '', '', [], 0, 0)

; Struct-options form (recommended, more readable): demonstrates cross-module
; struct forwarding — cmdopts is defined in this module, while the caller
; constructs process.cmdopts{...} in another module and passes it by reference.
cmdopts {
    dir str        ; working directory; empty = inherit parent's
    stdin str      ; data written to child stdin; empty = no stdin
    env []str      ; environment variables (["K=V", ...]); empty = inherit parent's
    timeout i64    ; timeout in ms; <=0 = no limit
    merge-err i64  ; nonzero = merge stderr into captured out
}
o = process.cmdopts{
    dir: '',
    stdin: 'piped-data',
    env: ['K=V'],
    timeout: 200,
    merge-err: 0
}
out, code, err = process.exec('echo', ['hello'], o)

; Convenience functions (legacy, pending decision)
status = process.process-run(cmd)           ; Execute shell command
content, code = process.process-output(program, arg) ; Execute and capture output
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

## Time and Date

### time — Time Operations

```no
sec = time.now-s()                   ; Current Unix timestamp (seconds)
ms = time.now-ms()                   ; Current timestamp (milliseconds)
us = time.now-us()                   ; Current timestamp (microseconds)
out = time.format-time(t, fmt)        ; Format time
time.sleep-ms(ms)                    ; Sleep (milliseconds)
time.sleep-us(us)                    ; Sleep (microseconds)
d = time.duration-between(start, end) ; Elapsed time (seconds)
d = time.duration-ms-between(s, e)    ; Elapsed time (milliseconds)
```

---

## Logging

### log — Leveled Logging

```no
LEVEL-DEBUG = 0
LEVEL-INFO  = 1
LEVEL-WARN  = 2
LEVEL-ERROR = 3
LEVEL-FATAL = 4

log.set-level(lvl)
log.debug(msg)
log.info(msg)
log.warn(msg)
log.error(msg)
log.fatal(msg)
```

---

## Data Structures

### set — Set (Array-based)

```no
new-n = set.add(s, n, val)           ; Add element
new-n = set.set-remove(s, n, val)        ; Remove element
ok = set.contains(s, n, val)         ; Whether it contains
new-an = set.union(a, an, b, bn)     ; Union
out, n = set.intersection(a, an, b, bn); Intersection
out, n = set.difference(a, an, b, bn)  ; Difference
v = set.to-vec(s, n)                 ; Convert to slice
sz = set.set-size(s, n)                   ; Number of elements
yes = set.set-empty(s, n)                    ; Whether it is empty
```

### deque — Double-Ended Queue

A double-ended queue implemented with a circular buffer, wrapped in the `deque` struct:

```no
; Struct
deque {
    buf []i64
    cap i64
    head i64
    tail i64
}

; Initialization
d = deque{
    buf: buf
    cap: 128
    head: 0
    tail: 0
}

; Methods
d.push-front(val)              ; Push from front
d.push-back(val)               ; Push from back
val = d.pop-front()             ; Pop from front
val = d.pop-back()              ; Pop from back
val = d.peek-front()            ; Peek front element (?i64, nil=empty)
val = d.peek-back()             ; Peek back element (?i64, nil=empty)
sz = d.size()                   ; Size
yes = d.empty()                 ; Whether it is empty
d.clear()                      ; Clear
```

### heap — Min Heap

A binary min heap wrapped in the `heap` struct:

```no
; Struct
heap {
    data []i64
    n i64
}

; Initialization
h = heap.init(data)            ; Create heap

; Methods
h.push(val)                    ; Push element
val = h.pop()                  ; Pop minimum element (?i64, nil=empty)
val = h.peek()                 ; Peek minimum element (?i64, nil=empty)
sz = h.size()                  ; Size
yes = h.empty()                ; Whether it is empty
```

### stack — Stack

A last-in-first-out (LIFO) data structure, wrapped in the `stack` struct:

```no
; Struct
stack {
    data []i64
    n i64
}

; Initialization
buf [128]i64 = [0:128]
s = stack{
    data: buf
    n: 0
}

; Methods
s.push(val)                    ; Push element
val = s.pop()                  ; Pop top element (?i64, nil=empty)
val = s.peek()                 ; Peek top element (?i64, nil=empty)
sz = s.size()                  ; Size
yes = s.empty()                ; Whether it is empty
s.clear()                      ; Clear
```

### map/linked-hash-map — Ordered Hash Map

Fixed capacity 64 (i64→i64), linear probing, doubly-linked list preserves insertion order:

```no
m = linked-hash-map{}
m.init()
m.put(key, val)
result = m.get(key)   ; ?i64, nil=not found
found = m.contains(key)
removed = m.remove(key)
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(key, val)
```

### map/hash-set — i64 Hash Set

Fixed capacity 64, linear probing, O(1) lookup/insert/delete:

```no
s = hash-set{}
s.init()
is-new = s.add(val)
found = s.contains(val)
removed = s.remove(val)
s.clear()
n = s.len()
empty = s.is-empty()
s.for-each(val)
```

### map/str-map — str→str Hash Map

Fixed capacity 256, FNV-1a hash, linear probing:

```no
m = str-map{}
m.init()
m.put('key', 'val')
result = m.get('key')   ; ?str, nil=not found
found = m.contains('key')
removed = m.remove('key')
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(k, v)
```

### map/str-set — str Hash Set

Fixed capacity 256, FNV-1a hash, string deduplication:

```no
s = str-set{}
s.init()
is-new = s.add('hello')
found = s.contains('hello')
removed = s.remove('hello')
s.clear()
n = s.len()
empty = s.is-empty()
s.for-each(val)
```

### map/tree-map — Ordered Map (AVL Tree)

An ordered map (i64→i64) implemented on a self-balancing AVL binary search tree, capacity 64:

```no
m = tree-map{}
m.clear()                           ; Initialize
ok = m.put(key, val)                ; Insert or update
val = m.get(key)                    ; Lookup (?i64, nil=not found)
yes = m.contains(key)               ; Check whether key exists
ok = m.remove(key)                  ; Delete key
key = m.first()                     ; Minimum key (?i64)
key = m.last()                      ; Maximum key (?i64)
key = m.lower-bound(target)         ; First key >= target (?i64)
key = m.upper-bound(target)         ; First key > target (?i64)
m.for-each(k, v)                    ; Traverse in ascending key order
sz = m.size()
yes = m.empty()
yes = m.full()
```

### map/tree-set — Ordered Set (AVL Tree)

An ordered set (i64) implemented on a self-balancing AVL binary search tree, capacity 64:

```no
s = tree-set{}
s.clear()                           ; Initialize
ok = s.add(key)                     ; Add element
yes = s.contains(key)               ; Check whether it exists
ok = s.remove(key)                  ; Delete element
val = s.first()                     ; Minimum value (?i64)
val = s.last()                      ; Maximum value (?i64)
val = s.lower-bound(target)         ; First element >= target (?i64)
val = s.upper-bound(target)         ; First element > target (?i64)
s.for-each(val)                     ; Traverse in ascending order
sz = s.size()
yes = s.empty()
yes = s.full()
```

### collection/queue — Generic Queue (Ring Buffer)

Implemented on a fixed-length array ring buffer; the buffer is provided by the `[n]t` receiver:

```no
buf [128]i64 = [0:128]
q = buf.queue-init()
ok = buf.queue-push(q, val)         ; Push to tail
val = buf.queue-pop(q)              ; Pop from front (?t)
val = buf.queue-peek(q)             ; Peek front (?t)
sz = q.size()
yes = q.empty()
yes = q.full()
q.clear()
```

### collection/arr-stack — Generic Stack (Fixed-Length Array Based)

A stack implementation based on a fixed-length array; the buffer is provided by the `[n]t` receiver:

```no
buf [128]i64 = [0:128]
s = buf.arr-stack-init()
ok = buf.arr-stack-push(s, val)     ; Push
val = buf.arr-stack-pop(s)          ; Pop (?t)
val = buf.arr-stack-peek(s)         ; Peek top (?t)
sz = s.size()
yes = s.empty()
yes = s.full()
s.clear()
```

### collection/link — Generic Doubly Linked List

A doubly linked list based on a fixed-length array node pool; values are provided by the `[n]t` receiver:

```no
buf [128]i64 = [0:128]
nxt [128]i64 = [0:128]
prv [128]i64 = [0:128]
l = buf.link-init(nxt, prv)
ok = buf.link-push-front(l, val)    ; Insert at head
ok = buf.link-push-back(l, val)     ; Insert at tail
val = buf.link-pop-front(l)         ; Pop head (?t)
val = buf.link-pop-back(l)          ; Pop tail (?t)
val = buf.link-peek-front(l)        ; Peek head (?t)
val = buf.link-peek-back(l)         ; Peek tail (?t)
sz = l.size()
yes = l.empty()
yes = l.full()
```

---

## Database

### database/sql — Database Access Interface

Defines standard interfaces for database connections, queries, and prepared statements, implemented by concrete drivers:

```no
; Execution result
result {
    last-id i64
    affected i64
}

; Connection interface (enter/leave auto-managed)
db enter, leave {
    close() (ok bool)
    exec(sql str) (r result)
    query(sql str) (rs rows)
    prepare(sql str) (s stmt)
}

; Result set interface
rows enter, leave {
    next() (ok bool)                    ; Iterate to next row
    scan-int(col i64) (v i64)           ; Read integer
    scan-str(col i64) (v str)           ; Read string
    scan-float(col i64) (v f64)         ; Read float
    close() (ok bool)
}

; Prepared statement interface
stmt enter, leave {
    bind-int(idx i64, v i64) (ok bool)
    bind-str(idx i64, v str) (ok bool)
    bind-bool(idx i64, v bool) (ok bool)
    exec() (r result)
    query() (rs rows)
    close() (ok bool)
}
```

---

## Encoding

### encoding/hex — Hexadecimal

```no
; Encoding (defined in the byte module)
out = data.to-hex()                  ; []byte -> uppercase hex str
out = data.to-hex-lower()            ; []byte -> lowercase hex str

; Decoding (defined in the str module)
out = s.from-hex()                   ; hex str -> ?[]byte (nil=empty, err=invalid character)
```

### encoding/base64 — Base64 (RFC 4648)

```no
BASE64-STD = 'ABC...+/'
BASE64-URL = 'ABC...-_'
PAD = 61  ; '='

out-n = base64.encode(data, n, table, out)    ; Base64 encoding
out-n = base64.encode-std(data, n, out)       ; Standard encoding
out-n = base64.encode-url(data, n, out)       ; URL-safe encoding
out-n = base64.decode(s, n, table, out)   ; Base64 decoding (?i64, nil=invalid input)
```

### encoding/csv — CSV Parsing (RFC 4180)

```no
fn, new-pos = csv.parse-field(s, sn, pos, field)  ; Parse a single field
n = csv.parse-line(s, sn, fields, max)             ; Parse one line
out-n = csv.encode-field(field, fn, out)           ; Encode field
```

---

## Archives

### archive/tar — TAR Archive (POSIX ustar)

```no
; Read a regular tar
archive = tar{
    data: raw-bytes
}
count = archive.count()
e = archive.entry(idx)
name = archive.name(idx)
sz = archive.size(idx)
typ = archive.type(idx)              ; "file" / "dir" / "unknown"
yes = archive.is-dir(idx)
yes = archive.is-file(idx)
out = archive.read(idx)
mode = archive.mode(idx)
ts = archive.mtime(idx)

; Read .tar.gz (auto-decompress)
archive = tar.tar-open-gz(gz-data)

; tar-entry methods
name = e.name()
sz = e.size()
typ = e.type()
out = e.read()

; Write tar
builder = tar-builder{}
builder.add-file(name, content)
builder.add-dir(name)
archive = builder.finish()
```

### archive/zip — ZIP Archive Parsing

```no
archive = zip{
    data: raw-bytes
}
count = archive.count()                        ; Number of entries
e = archive.entry(idx)                         ; Get zip-entry
name = archive.name(idx)                       ; Filename
sz = archive.size(idx)                         ; Original size
csz = archive.compressed-size(idx)             ; Compressed size
method = archive.method(idx)                   ; 0=stored, 8=deflate
out = archive.extract(idx)                     ; stored and deflate modes

; zip-entry methods
name = e.name()
sz = e.size()
csz = e.compressed-size()
method = e.method()
out = e.extract()
```

### archive/gzip — GZIP Compression and Raw DEFLATE

```no
out = gzip.gzip-compress(data)                      ; zlib compression
out = gzip.gzip-decompress(data)                    ; zlib decompression
out = gzip.inflate-decompress(data, out-size)       ; Raw DEFLATE decompression (ZIP method 8)
```

---

## Cryptography and Hashing

### hash/aes — AES-128 Encryption/Decryption (ECB Mode)

```no
aes.aes-128-enc(plain, 16, key, out)   ; Encrypt 16-byte block
aes.aes-128-dec(cipher, 16, key, out)  ; Decrypt 16-byte block
```

Also includes standalone modules `hash/aes-128-enc` and `hash/aes-128-dec`.

### hash/des — DES Encryption/Decryption (ECB Mode)

```no
des.des-enc(plain, 8, key, out)        ; Encrypt 8-byte block
des.des-dec(cipher, 8, key, out)       ; Decrypt 8-byte block
```

Also includes standalone modules `hash/des-enc` and `hash/des-dec`.

### hash/rsa — RSA Modular Exponentiation

```no
rsa.rsa-modpow(base, bn, exp, en, mod, mn, result, rn)
```

Does not include key generation; supports 1024~4096-bit.

### hash/md5 — MD5 (128-bit)

```no
out [16]byte = md5.md5(data)
```

### hash/sha1 — SHA-1 (160-bit)

```no
hash = sha1.sha1(data []byte) (hash [20]byte)
hex = sha1.sha1-hex(data []byte) (hex str)
sha1.sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32)
```

`sha1` computes the complete hash (including padding and multi-block processing), returning 20 bytes.
`sha1-hex` is the same but returns a 40-character lowercase hex string.
`sha1-block` is a low-level API that processes a single 512-bit block.

### hash/sha256 — SHA-256 (256-bit)

```no
sha256.sha256(data []byte) (hash [32]byte)
sha256.sha256-hex(data []byte) (hex str)
sha256.sha256-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32, h5 u32, h6 u32, h7 u32)
```

`sha256` computes the complete hash (including padding and multi-block processing), returning 32 bytes.
`sha256-hex` is the same but returns a 64-character lowercase hex string.
`sha256-block` is a low-level API that processes a single 512-bit block.

### hash/sha512 — SHA-512 (512-bit)

```no
sha512.sha512(data []byte) (hash [64]byte)
sha512.sha512-hex(data []byte) (hex str)
sha512.sha512-block(s []u64, h0 u64, h1 u64, h2 u64, h3 u64, h4 u64, h5 u64, h6 u64, h7 u64)
```

`sha512` computes the complete hash (including padding and multi-block processing), returning 64 bytes.
`sha512-hex` is the same but returns a 128-character lowercase hex string.
`sha512-block` is a low-level API that processes a single 1024-bit block.

### hash/crc-32 — CRC32 Checksum

```no
crc-32.crc-32(s []byte, n, crc)
```

### hash/fnv-1a-32 — FNV-1a Non-Cryptographic Hash

```no
fnv-1a-32.fnv-1a-32(s []byte, n, h)
```

### hash/rand — Random Number Generator (xorshift32)

```no
r = rand.rand(state)                     ; 32-bit pseudo-random number
rand.rand-str(state, n, s)              ; Random alphanumeric string
```

### hash/x509 — X.509 Certificate DER Parsing

```no
tag = x509.der-tag(data, pos)
len, adv = x509.der-len(data, pos)
x509.x509-fingerprint(cert, n, h0..h7)  ; SHA-256 certificate fingerprint
x509.x509-rsa-e(cert, n, e)             ; RSA public key exponent extraction
```

### hash/aes-256 — AES-256 Encryption/Decryption (ECB Mode)

```no
aes-256.aes-256-enc(in [16]byte, key [32]byte) (out [16]byte)   ; Encrypt
aes-256.aes-256-dec(in [16]byte, key [32]byte) (out [16]byte)   ; Decrypt
```

### hash/aes-cbc — AES-CBC Mode (with PKCS7 Padding)

```no
out = aes-cbc.aes-128-cbc-enc(in []byte, key [16]byte, iv [16]byte)
out = aes-cbc.aes-128-cbc-dec(in []byte, key [16]byte, iv [16]byte)
out = aes-cbc.pkcs7-pad(in []byte)
n = aes-cbc.pkcs7-unpad(in []byte)
```

### hash/aes-256-cbc — AES-256-CBC Encryption/Decryption

```no
out = aes-256-cbc.aes-256-cbc-enc(in []byte, key [32]byte, iv [16]byte)
out = aes-256-cbc.aes-256-cbc-dec(in []byte, key [32]byte, iv [16]byte)
```

### hash/aes-ctr — AES-CTR Counter Mode

```no
out = aes-ctr.aes-128-ctr(in []byte, key [16]byte, iv [16]byte)
out = aes-ctr.aes-256-ctr(in []byte, key [32]byte, iv [16]byte)
```

### hash/aes-gcm — AES-GCM AEAD

```no
; AES-128-GCM
sealed = aes-gcm.aes-128-gcm-seal(key [16]byte, iv [12]byte, aad []byte, plain []byte)
plain = aes-gcm.aes-128-gcm-open(key [16]byte, iv [12]byte, aad []byte, sealed []byte)
```

### hash/aes-256-gcm — AES-256-GCM AEAD (NIST SP 800-38D)

```no
sealed = aes-256-gcm.aes-256-gcm-seal(key [32]byte, iv [12]byte, aad []byte, plain []byte)
plain = aes-256-gcm.aes-256-gcm-open(key [32]byte, iv [12]byte, aad []byte, sealed []byte)
```

### hash/hmac — HMAC Message Authentication Code

```no
out = hmac.hmac(key []byte, key-n i64, msg []byte, msg-n i64, block-size i64) (out [32]byte)
```

### hash/hkdf — HKDF Key Derivation (RFC 5869)

```no
ok = hkdf.hkdf-extract(salt []byte, salt-n i64, ikm []byte, ikm-n i64, prk []byte)
ok = hkdf.hkdf-expand(prk []byte, prk-n i64, info []byte, info-n i64, out []byte, out-n i64)
```

### hash/pbkdf2 — PBKDF2 Key Derivation (RFC 2898)

```no
pbkdf2.pbkdf2(password []byte, pw-n i64, salt []byte, salt-n i64, iter i64, out []byte, out-n i64)
```

### hash/argon2 — Argon2 Memory-Hard Key Derivation

```no
argon2.argon2id(password []byte, pw-n i64, salt []byte, salt-n i64, time i64, memory i64, parallel i64, out []byte, out-n i64)
```

### hash/scrypt — scrypt Key Derivation

```no
scrypt.scrypt(password []byte, pw-n i64, salt []byte, salt-n i64, n i64, r i64, p i64, out []byte, out-n i64)
```

### hash/sha224 — SHA-224 (224-bit)

```no
hash = sha224.sha224(data []byte) (hash [28]byte)
hex = sha224.sha224-hex(data []byte) (hex str)
```

### hash/sha384 — SHA-384 (384-bit)

```no
hash = sha384.sha384(data []byte) (hash [48]byte)
hex = sha384.sha384-hex(data []byte) (hex str)
```

### hash/sha3 — SHA-3 (Keccak)

```no
hash = sha3.sha3-256(data []byte) (hash [32]byte)
hash = sha3.sha3-512(data []byte) (hash [64]byte)
```

### hash/blake2 — BLAKE2 Hash

```no
hash = blake2.blake2b-256(data []byte) (hash [32]byte)
hash = blake2.blake2b-512(data []byte) (hash [64]byte)
```

### hash/crc-16 — CRC16 Checksum

```no
crc = crc-16.crc-16(data []byte, n i64) (crc i64)
```

### hash/crc-64 — CRC64 Checksum

```no
crc = crc-64.crc-64(data []byte, n i64) (crc i64)
```

### hash/fnv — FNV-1 Hash

```no
h = fnv.fnv-1-32(data []byte, n i64) (h i64)
h = fnv.fnv-1a-64(data []byte, n i64) (h i64)
```

### hash/base32 — Base32 Encoding/Decoding (RFC 4648)

```no
out = base32.base32-encode(data []byte, n i64) (out str)
out = base32.base32-decode(s str, n i64) (out []byte)
```

### hash/chacha20-poly1305 — ChaCha20-Poly1305 AEAD

```no
sealed = chacha20-poly1305.chacha20-poly1305-seal(key [32]byte, nonce [12]byte, aad []byte, plain []byte)
plain = chacha20-poly1305.chacha20-poly1305-open(key [32]byte, nonce [12]byte, aad []byte, sealed []byte)
```

### hash/rc4 — RC4 Stream Cipher

```no
out = rc4.rc4(key []byte, key-n i64, data []byte, data-n i64) (out []byte)
```

### hash/tdes — Triple DES (3DES)

```no
tdes.tdes-enc(plain, 8, key [24]byte, out)
tdes.tdes-dec(cipher, 8, key [24]byte, out)
```

### hash/ecdsa — ECDSA Digital Signature

```no
ok = ecdsa.ecdsa-sign(priv-key []byte, msg []byte, msg-n i64, r []byte, s []byte)
ok = ecdsa.ecdsa-verify(pub-key []byte, msg []byte, msg-n i64, r []byte, s []byte) (ok bool)
```

### hash/ed25519 — Ed25519 Digital Signature

```no
pub = ed25519.ed25519-derive-public(priv [32]byte) (pub [32]byte)
sig = ed25519.ed25519-sign(priv [32]byte, msg []byte, msg-n i64) (sig [64]byte)
ok = ed25519.ed25519-verify(pub [32]byte, msg []byte, msg-n i64, sig [64]byte) (ok bool)
```

### hash/x25519 — X25519 Key Exchange

```no
pub = x25519.x25519-derive-public(priv [32]byte) (pub [32]byte)
shared = x25519.x25519-derive-shared(priv [32]byte, peer-pub [32]byte) (shared [32]byte)
```

### hash/rand-str — Random String Generation

```no
rand-str.rand-str(state i64, n i64, s str)   ; Generate a random alphanumeric string of length n
```

---

## Data Exchange

### json — JSON Parsing and Generation

```no
; Type enum
json-kind {
    null,
    bool,
    num,
    str,
    arr,
    obj,
}

; Parsing
v = json.parse(s, n)          ; Full parse
v = json.parse-str(s, n)                 ; Parse string value
v = json.parse-num(s, n)                 ; Parse numeric value

; Generation
n = json.stringify(v, out)    ; Serialize

; Access
val = json.get-key(v, key)    ; Get object property
json.set-key(v json-value, key, val)    ; Set object property
```

---

## Others

### unicode — Unicode Support

Unicode-related functionality is distributed across the `char` and `str` modules:

- Character classification (`is-letter`, `is-digit`, `is-upper`, etc.) -> see `char` module
- UTF-8 encoding/decoding (`char.to-bytes`, `char.to-str`) -> see `char` module
- String rune counting (`str.count`) -> see `str` module

### uuid — UUID v4 Generation and Parsing

```no
out = uuid.new-v4(state)                  ; Generate UUID v4
out-n = uuid.to-str(out)             ; Convert to lowercase string (method)
out-n = uuid.to-str-upper(out)       ; Convert to uppercase string (method)
ok = uuid.from-str(s, sn, out)            ; Parse from string (with/without hyphens)
ok = uuid.parse-with-dashes(s, pos, out)  ; Parse with hyphens
ok = uuid.parse-no-dashes(s, pos, out)    ; Parse without hyphens
ok = uuid.validate()                 ; Validate UUID format (method)
v = uuid.version()                   ; Get version (method)
v = uuid.variant()                   ; Get variant (method)
yes = uuid.is-nil()                  ; Whether it is nil (method)
yes = uuid.eq(b)                     ; Equality comparison (method)
r = uuid.cmp(b)                      ; Compare (method)
uuid.nil-uuid(out)                        ; Return nil UUID
```

### bigint — Arbitrary Precision Integer

```no
; Type
bigint {
    sign i64
    limbs []i64
    len i64
}

; Construction
out = bigint.from-i64(v)
out = bigint.from-u64(v)
out = bigint.zero()
out = bigint.one()
out = bigint.copy(a)

; Comparison
r = bigint.cmp(a, b)
r = bigint.eq(a, b)
r = bigint.is-zero(a)
r = bigint.is-neg(a)
r = bigint.is-pos(a)

; Operations
c = bigint.add(a, b)
c = bigint.sub(a, b)
c = bigint.mul(a, b)
q, r = bigint.div-mod(a, b)
r = bigint.mod(a, b)
q = bigint.div-i64(a, v)
r = bigint.mod-i64(a, v)
c = bigint.pow(a, n)
r = bigint.mod-pow(base, exp, mod, r)

; Number theory
bigint.gcd(a, b, g)
bigint.lcm(a, b, l)

; Shifting
bigint.shl(a, n, c)
bigint.shr(a, n, c)

; String conversion
n = bigint.to-str(a, out)
out = bigint.from-str(s, sn)
n = bigint.to-hex(a, out)
out = bigint.from-hex(s, sn)

; Small integer helpers
bigint.add-i64(a, v, c)
bigint.mul-i64(a, v, c)
```

### err — Error Handling

Structured error type and utility functions:

```no
; Error code enum
code {
    ok,
    not-found,
    permission,
    io,
    timeout,
    parse,
    invalid,
    overflow,
}

; Struct
error {
    code code
    msg str
}

; Functions
e = err.new(code.io, msg)            ; Create error
e = err.err-from-errno(errno)         ; Create from C errno
yes = e.is(code.io)                  ; Check error code
msg = e.msg()                       ; Get error message
c = e.code()                        ; Get error code
s = e.format()                       ; Format as string
```

### bool — Boolean Type

```no
bool.to-str() (out str)     ; true->"true", false->"false" (method)
```

### enter / leave — Lifecycle Hooks

```no

; Run on startup
enter { 
    enter()
}     

; Run on exit
leave {
    leave()
}     
```

---

## Module Overview

| Module              | Path   | Description                                  |
| ------------------- | ------ | -------------------------------------------- |
| fmt                 | Core   | Formatted output                             |
| math                | Core   | Math functions                               |
| str                 | Core   | String operations                            |
| vec                 | Core   | Slice ([]t) operations                       |
| arr                 | Core   | Array ([n]t) operations                      |
| number              | Core   | Numeric utility functions                    |
| byte                | Core   | Byte operations                              |
| char                | Core   | Character operations (methods)               |
| os                  | Core   | Operating system interface                   |
| env                 | Core   | Environment variables wrapper                |
| fs                  | Core   | File system utilities                        |
| io                  | Core   | Input/output abstraction                     |
| args                | Core   | Command-line arguments                       |
| path                | Core   | Path handling (struct)                       |
| bufio               | Core   | Buffered reading                             |
| time                | Core   | Time operations                              |
| log                 | Core   | Leveled logging                              |
| json                | Core   | JSON parsing/generation                      |
| types               | Core   | Type definitions document                    |
| option              | Core   | Option type                                  |
| sort                | Core   | Sort constants                               |
| set                 | Core   | Set                                          |
| deque               | Core   | Double-ended queue (struct)                  |
| heap                | Core   | Min heap (struct)                            |
| stack               | Core   | Stack (struct)                               |
| regexp              | Core   | Regular expressions                          |
| process             | Core   | Process operations                           |
| unicode             | Core   | Unicode documentation                        |
| uuid                | Core   | UUID v4                                      |
| bigint              | Core   | Arbitrary precision integer                  |
| bool                | Core   | Boolean type                                 |
| err                 | Core   | Error handling                               |
| enter               | Core   | Startup hook                                 |
| leave               | Core   | Exit hook                                    |
| net                 | Core   | TCP network operations                       |
| net/http            | Submodule | HTTP/1.1 client                          |
| net/http2           | Submodule | HTTP/2.0 client                          |
| net/http3           | Submodule | HTTP/3.0 client                          |
| net/ws              | Submodule | WebSocket                                |
| net/quic            | Submodule | QUIC protocol                            |
| net/tls             | Submodule | TLS 1.2/1.3                              |
| net/sse             | Submodule | SSE client                              |
| net/client          | Submodule | High-level TCP client                    |
| net/server          | Submodule | HTTP server                             |
| net/dns             | Submodule | DNS resolution                          |
| net/url             | Submodule | URL parsing                             |
| net/cookie          | Submodule | HTTP Cookie                             |
| net/multipart       | Submodule | Multipart form                          |
| net/hpack           | Submodule | HPACK header compression                |
| net/proxy           | Submodule | Proxy support                           |
| net/pool            | Submodule | Connection pool                         |
| net/unix            | Submodule | Unix domain sockets                     |
| net/ip              | Submodule | IP address operations                   |
| encoding/hex        | Submodule | Hexadecimal encoding/decoding           |
| encoding/base64     | Submodule | Base64 encoding/decoding                |
| encoding/csv        | Submodule | CSV parsing                             |
| archive/tar         | Submodule | TAR archive                             |
| archive/zip         | Submodule | ZIP archive                             |
| archive/gzip        | Submodule | GZIP compression                        |
| map/linked-hash-map | Submodule | Ordered hash map                        |
| map/hash-set        | Submodule | i64 hash set                            |
| map/str-map         | Submodule | str→str hash map                        |
| map/str-set         | Submodule | str hash set                            |
| map/tree-map        | Submodule | AVL ordered map                         |
| map/tree-set        | Submodule | AVL ordered set                         |
| collection/queue    | Submodule | Generic queue                           |
| collection/arr-stack| Submodule | Generic stack                           |
| collection/link     | Submodule | Generic doubly linked list              |
| database/sql        | Submodule | Database access interface               |
| hash/aes            | Submodule | AES-128 encryption/decryption           |
| hash/aes-128-enc    | Submodule | AES-128 encryption                      |
| hash/aes-128-dec    | Submodule | AES-128 decryption                      |
| hash/aes-256        | Submodule | AES-256 encryption/decryption           |
| hash/aes-cbc        | Submodule | AES-CBC mode                            |
| hash/aes-256-cbc    | Submodule | AES-256-CBC                             |
| hash/aes-ctr        | Submodule | AES-CTR mode                            |
| hash/aes-gcm        | Submodule | AES-GCM AEAD                           |
| hash/aes-256-gcm    | Submodule | AES-256-GCM                            |
| hash/des            | Submodule | DES encryption/decryption               |
| hash/des-enc        | Submodule | DES encryption                          |
| hash/des-dec        | Submodule | DES decryption                          |
| hash/tdes           | Submodule | Triple DES                              |
| hash/rsa            | Submodule | RSA modular exponentiation              |
| hash/md5            | Submodule | MD5 hash                                |
| hash/sha1           | Submodule | SHA-1 hash                              |
| hash/sha224         | Submodule | SHA-224 hash                            |
| hash/sha256         | Submodule | SHA-256 hash                            |
| hash/sha384         | Submodule | SHA-384 hash                            |
| hash/sha512         | Submodule | SHA-512 hash                            |
| hash/sha3           | Submodule | SHA-3 hash                              |
| hash/blake2         | Submodule | BLAKE2 hash                             |
| hash/crc-16         | Submodule | CRC16 checksum                          |
| hash/crc-32         | Submodule | CRC32 checksum                          |
| hash/crc-64         | Submodule | CRC64 checksum                          |
| hash/fnv            | Submodule | FNV-1 hash                              |
| hash/fnv-1a-32      | Submodule | FNV-1a hash                             |
| hash/hmac           | Submodule | HMAC authentication code                |
| hash/hkdf           | Submodule | HKDF key derivation                     |
| hash/pbkdf2         | Submodule | PBKDF2 key derivation                   |
| hash/argon2         | Submodule | Argon2 key derivation                   |
| hash/scrypt         | Submodule | scrypt key derivation                   |
| hash/chacha20-poly1305 | Submodule | ChaCha20-Poly1305                    |
| hash/rc4            | Submodule | RC4 stream cipher                       |
| hash/ecdsa          | Submodule | ECDSA signature                         |
| hash/ed25519        | Submodule | Ed25519 signature                       |
| hash/x25519         | Submodule | X25519 key exchange                     |
| hash/base32         | Submodule | Base32 encoding/decoding                |
| hash/rand           | Submodule | Random number generator                 |
| hash/rand-str       | Submodule | Random string generation                |
| hash/x509           | Submodule | X.509 DER parsing                       |
