---
name: nolang-std
description: Nolang 标准库参考。用于查找标准库模块 API、数据结构、加密/压缩/编码函数签名，以及编写或修改 .no 文件时确定应该使用哪些标准库函数。涵盖 fmt/math/str/vec/arr/number/byte/char/os/fs/io/path/bufio/time/log/json/types/option/sort/set/deque/heap/stack/regexp/process/net/database/encoding/archive/crypto/uuid/bigint/err 等全部模块。
---

# Nolang Standard Library Reference

The Nolang standard library (`src/std/`) contains 80+ modules, covering formatting, math, strings, data structures, encoding/decoding, encryption, compression, file operations, I/O abstractions, async coroutines, file type detection, etc.

Usage: `# std/xxx` (core modules do not need to be imported).

> **The old-style `use std/xxx` still works but is deprecated; using the new-style `# std/xxx` syntax is recommended.**

> **Note: All code examples follow the "one statement per line" rule — `;` and `//` are both comment markers; commas `,` must not join multiple statements on one line.**

> **Rule: If the standard library already provides corresponding functionality, reimplementing it yourself is not recommended.** Developers should carefully review the standard library documentation below to avoid reinventing the wheel.

## Table of Contents

- [Base Types](#base-types)
  - [types — Type Definitions](#types--type-definitions)
  - [option — Option Type](#option--option-type)
- [Core Library](#core-library)
  - [fmt — Formatted Output](#fmt--formatted-output)
  - [math — Math Functions](#math--math-functions)
  - [char — Character Operations](#char--character-operations)
  - [str — String Operations](#str--string-operations)
  - [number — Numeric Operations](#number--numeric-operations)
  - [byte — Byte Operations](#byte--byte-operations)
  - [vec — Slice Operations](#vec--slice-operations)
  - [arr — Array Operations](#arr--array-operations)
  - [sort — Sort Constants](#sort--sort-constants)
- [OS & Files](#os--files)
  - [os — Operating System Interface](#os--operating-system-interface)
  - [fs — File System Tools](#fs--file-system-tools)
  - [env — Environment Variables](#env--environment-variables-simplified-wrapper)
  - [args — Command-line Arguments](#args--command-line-arguments)
  - [path — Path Operations](#path--path-operations)
  - [bufio — Buffered Reading](#bufio--buffered-reading)
  - [io — I/O Abstraction](#io--io-abstraction)
  - [regexp — Regular Expression](#regexp--regular-expression)
  - [process — Process Operations](#process--process-operations)
  - [net — Network Operations](#net--network-operations)
  - [net/ip — IP Address Operations](#netip--ip-address-operations)
  - [net/sse — Server-Sent Events Client](#netsse--server-sent-events-client)
  - [net/http — HTTP/1.1 Client](#nethttp--http11-client)
  - [net/http2 — HTTP/2.0 Client](#nethttp2--http20-client-rfc-7540)
  - [net/http3 — HTTP/3.0 Client](#nethttp3--http30-client-rfc-9114)
  - [net/ws — WebSocket Client and Server](#netws--websocket-client-and-server-rfc-6455)
  - [net/tls — TLS 1.2/1.3 Client](#nettls--tls-1213-client-pure-nolang-implementation)
  - [net/client — High-level TCP Client](#netclient--high-level-tcp-client)
  - [net/quic — QUIC Protocol](#netquic--quic-protocol-rfc-9000)
  - [net/server — HTTP Server](#netserver--http-server)
  - [net/dns — DNS Resolution](#netdns--dns-resolution)
  - [net/url — URL Parsing](#neturl--url-parsing)
  - [net/cookie — HTTP Cookie](#netcookie--http-cookie)
  - [net/multipart — Multipart Form Data](#netmultipart--multipart-form-data)
  - [net/hpack — HPACK Header Compression](#nethpack--hpack-header-compression-http2)
  - [net/proxy — Proxy Support](#netproxy--proxy-support)
  - [net/pool — Connection Pool](#netpool--connection-pool)
  - [net/unix — Unix Domain Socket](#netunix--unix-domain-socket)
- [Time & Date](#time--date)
  - [time — Time Operations](#time--time-operations)
- [Logging](#logging)
  - [log — Leveled Logging](#log--leveled-logging)
- [Data Structures](#data-structures)
  - [set — Set (Array-based)](#set--set-array-based)
  - [deque — Double-ended Queue](#deque--double-ended-queue)
  - [heap — Min Heap](#heap--min-heap)
  - [stack — Stack](#stack--stack)
  - [map/linked-hash-map — Ordered Hash Map](#maplinked-hash-map--ordered-hash-map)
  - [map/hash-set — i64 Hash Set](#maphash-set--i64-hash-set)
  - [map/str-map — str→str Hash Map](#mapstr-map--strstr-hash-map)
  - [map/str-set — str Hash Set](#mapstr-set--str-hash-set)
  - [map/tree-map — Ordered Map (AVL Tree)](#maptree-map--ordered-map-avl-tree)
  - [map/tree-set — Ordered Set (AVL Tree)](#maptree-set--ordered-set-avl-tree)
  - [collection/queue — Generic Queue (Ring Buffer)](#collectionqueue--generic-queue-ring-buffer)
  - [collection/arr-stack — Generic Stack (Array-based)](#collectionarr-stack--generic-stack-array-based)
  - [collection/link — Generic Doubly-linked List](#collectionlink--generic-doubly-linked-list)
  - [collection/map — Generic Dynamic Hash Map](#collectionmap--generic-dynamic-hash-map)
  - [collection/static-hashmap — Generic Fixed-capacity Hash Map](#collectionstatic-hashmap--generic-fixed-capacity-hash-map)
- [Database](#database)
  - [database/sql — Database Access Interface](#databasesql--database-access-interface)
- [Encoding](#encoding)
  - [encoding/hex — Hexadecimal](#encodinghex--hexadecimal)
  - [encoding/base64 — Base64 (RFC 4648)](#encodingbase64--base64-rfc-4648)
  - [encoding/csv — CSV Parsing (RFC 4180)](#encodingcsv--csv-parsing-rfc-4180)
  - [encoding/pem — PEM Encoding/Decoding (RFC 7468)](#encodingpem--pem-encodingdecoding-rfc-7468)
- [Archive](#archive)
  - [archive/tar — TAR Archive (POSIX ustar)](#archivetar--tar-archive-posix-ustar)
  - [archive/zip — ZIP Archive Parsing](#archivezip--zip-archive-parsing)
  - [archive/gzip — GZIP Compression and Raw DEFLATE](#archivegzip--gzip-compression-and-raw-deflate)
  - [archive/bzip2 — BZIP2 Decompression (Pure Nolang)](#archivebzip2--bzip2-decompression-pure-nolang)
  - [archive/xz — XZ/LZMA Decompression (Pure Nolang)](#archivexz--xzlzma-decompression-pure-nolang)
  - [archive/zlib — zlib Compression/Decompression (RFC 1950, Pure Nolang)](#archivezlib--zlib-compressiondecompression-rfc-1950-pure-nolang)
  - [archive/zstd — Zstandard Decompression (Pure Nolang)](#archivezstd--zstandard-decompression-pure-nolang)
- [Cryptography & Hash](#cryptography--hash)
  - [hash/aes — AES-128 Encryption/Decryption (ECB mode)](#hashaes--aes-128-encryptiondecryption-ecb-mode)
  - [hash/des — DES Encryption/Decryption (ECB mode)](#hashdes--des-encryptiondecryption-ecb-mode)
  - [hash/rsa — RSA Modular Exponentiation](#hashrsa--rsa-modular-exponentiation)
  - [hash/md5 — MD5 (128-bit)](#hashmd5--md5-128-bit)
  - [hash/sha1 — SHA-1 (160-bit)](#hashsha1--sha-1-160-bit)
  - [hash/sha256 — SHA-256 (256-bit)](#hashsha256--sha-256-256-bit)
  - [hash/sha512 — SHA-512 (512-bit)](#hashsha512--sha-512-512-bit)
  - [hash/crc-32 — CRC32 Checksum](#hashcrc-32--crc32-checksum)
  - [hash/fnv-1a-32 — FNV-1a Non-cryptographic Hash](#hashfnv-1a-32--fnv-1a-non-cryptographic-hash)
  - [hash/rand — Random Number Generator (xorshift32)](#hashrand--random-number-generator-xorshift32)
  - [hash/x509 — X.509 Certificate DER Parsing](#hashx509--x509-certificate-der-parsing)
  - [hash/aes-256 — AES-256 Encryption/Decryption (ECB mode)](#hashaes-256--aes-256-encryptiondecryption-ecb-mode)
  - [hash/aes-cbc — AES-CBC Mode (with PKCS7 Padding)](#hashaes-cbc--aes-cbc-mode-with-pkcs7-padding)
  - [hash/aes-256-cbc — AES-256-CBC Encryption/Decryption](#hashaes-256-cbc--aes-256-cbc-encryptiondecryption)
  - [hash/aes-ctr — AES-CTR Counter Mode](#hashaes-ctr--aes-ctr-counter-mode)
  - [hash/aes-gcm — AES-GCM AEAD](#hashaes-gcm--aes-gcm-aead)
  - [hash/aes-256-gcm — AES-256-GCM AEAD (NIST SP 800-38D)](#hashaes-256-gcm--aes-256-gcm-aead-nist-sp-800-38d)
  - [hash/hmac — HMAC Message Authentication Code](#hashhmac--hmac-message-authentication-code)
  - [hash/hkdf — HKDF Key Derivation (RFC 5869)](#hashhkdf--hkdf-key-derivation-rfc-5869)
  - [hash/pbkdf2 — PBKDF2 Key Derivation (RFC 2898)](#hashpbkdf2--pbkdf2-key-derivation-rfc-2898)
  - [hash/argon2 — Argon2 Memory-hard Key Derivation](#hashargon2--argon2-memory-hard-key-derivation)
  - [hash/scrypt — scrypt Key Derivation](#hashscrypt--scrypt-key-derivation)
  - [hash/sha224 — SHA-224 (224-bit)](#hashsha224--sha-224-224-bit)
  - [hash/sha384 — SHA-384 (384-bit)](#hashsha384--sha-384-384-bit)
  - [hash/sha3 — SHA-3 (Keccak)](#hashsha3--sha-3-keccak)
  - [hash/blake2 — BLAKE2 Hash](#hashblake2--blake2-hash)
  - [hash/crc-16 — CRC16 Checksum](#hashcrc-16--crc16-checksum)
  - [hash/crc-64 — CRC64 Checksum](#hashcrc-64--crc64-checksum)
  - [hash/fnv — FNV-1 Hash](#hashfnv--fnv-1-hash)
  - [hash/base32 — Base32 Encoding/Decoding (RFC 4648)](#hashbase32--base32-encodingdecoding-rfc-4648)
  - [hash/chacha20-poly1305 — ChaCha20-Poly1305 AEAD](#hashchacha20-poly1305--chacha20-poly1305-aead)
  - [hash/rc4 — RC4 Stream Cipher](#hashrc4--rc4-stream-cipher)
  - [hash/tdes — Triple DES (3DES)](#hashtdes--triple-des-3des)
  - [hash/ecdsa — ECDSA Digital Signature](#hashecdsa--ecdsa-digital-signature)
  - [hash/ed25519 — Ed25519 Digital Signature](#hashed25519--ed25519-digital-signature)
  - [hash/x25519 — X25519 Key Exchange](#hashx25519--x25519-key-exchange)
  - [hash/rand-str — Random String Generation](#hashrand-str--random-string-generation)
- [Data Exchange](#data-exchange)
  - [json — JSON Parsing and Generation](#json--json-parsing-and-generation)
- [Others](#others)
  - [unicode — Unicode Support](#unicode--unicode-support)
  - [uuid — UUID v4 Generation and Parsing](#uuid--uuid-v4-generation-and-parsing)
  - [bigint — Arbitrary Precision Integer](#bigint--arbitrary-precision-integer)
  - [err — Error Handling](#err--error-handling)
  - [bool — Boolean Type](#bool--boolean-type)
  - [enter / leave — Lifecycle Hooks](#enter--leave--lifecycle-hooks)
  - [async — Async Coroutine and Cancellation Primitives](#async--async-coroutine-and-cancellation-primitives)
  - [global — Global Built-in Functions](#global--global-built-in-functions)
  - [magic — File Type Detection](#magic--file-type-detection)
- [Module List](#module-list)

---

## Base Types

### types — Type Definitions

Nolang type to LLVM mapping:

| Nolang           | LLVM                                               |
| ---------------- | -------------------------------------------------- |
| `bool`           | `i1`                                               |
| `byte`           | `i8`                                               |
| `char`           | `i32`                                              |
| `i8/i16/i32/i64` | `i8/i16/i32/i64`                                   |
| `u8/u16/u32/u64` | `i8/i16/i32/i64`                                   |
| `f32`            | `float`                                            |
| `f64`            | `double`                                           |
| `str`            | union (short: `[127]byte` / long: `{*byte, i64}`)  |

**Composite types:**

- **Variable-length array `[]t`**: underlying `{ t*, i64 }` (data, len)
- **Fixed-length array `[n]t`**: LLVM fixed-size array
- **String `str`**: union type (short ≤127 bytes stored on stack / long stored on heap), supports `s[i]`, `s[i..j]`, `s + t`
- **Enum/Union**: `option` tagged enum (`ok t` / `nil` / `err str`)
- **Struct**: must be multi-line definition, fields without commas
- **Map**: underlying linked-hash-map
- **Iterator**: `for iter.next() {}` (interface method `next() (ok bool)`)

### option — Option Type

`option<t>` tagged enum (tag=0=val, 1=nil, 2=err):

```no
x ?t                // Declare option<t>
x = 42              // Set to has-value
x = nil             // Set to empty
x = err('msg')      // Set to error

// match
x: {
    val -> f(it)
    nil ->
    err -> g(it)
}
```

**Style guide:** When a function may fail or return an empty value, use `?t` option instead of `(val, ok bool)`. `?t` has three states: `ok` (has value), `nil` (empty/normal absence), `err` (error). Normal values are implicitly bound. For example, `pop()` returns `?i64` (`nil` = empty), `read-line()` returns `?str` (`nil` = EOF, `err` = error), `lookup()` returns `?str` (`nil` = not found).

---

### Core Library

#### fmt — Formatted Output

```no
print('x={x}')                 // Named format, auto-appends newline (stdout)
eprint('err {x}')              // Named format, auto-appends newline (stderr)
print('id {id:06} amt {money:.2f}')  // Supports align/fill/width/precision
s = format('x={x}')            // Returns formatted string (replaces sprintf)
io.out('no-newline-here')      // Low-level command, no newline (stdout)
io.err('err-no-newline')       // Low-level command, no newline (stderr)
// printf/eprintf/sprintf are deprecated: printf→io.out, eprintf→io.err, sprintf→format.
// io.err carries the module prefix and will not conflict with the Option constructor err().
// Output via io.out/io.err syscalls, no libc printf dependency
```

#### math — Math Functions

**Constants:** `math.PI`, `math.E`

**Basic:** `math.abs`, `math.sqrt`

**Trigonometric:** `math.sin`, `math.cos`, `math.tan`, `math.asin`, `math.acos`, `math.atan`, `math.atan2`, `math.degrees`, `math.radians`

**Hyperbolic:** `math.sinh`, `math.cosh`, `math.tanh`

**Rounding:** `math.ceil`, `math.floor`, `math.round`, `math.trunc`

**Exponential/Logarithm:** `math.exp`, `math.log`, `math.log10`, `math.log2`, `math.pow`, `math.hypot`, `math.cbrt`

**Others:** `math.fmod`, `math.max`, `math.min`

#### char — Character Operations

char is essentially i32 (Unicode code point), all operations are provided as methods:

```no
c char = 'A'
c.is-digit()       // Is digit (0-9) (method)
c.is-letter()      // Is letter (a-z, A-Z) (method)
c.is-alpha()       // Alias for is-letter (method)
c.is-alnum()       // Is letter or digit (method)
c.is-space()       // Is whitespace character (method)
c.is-upper()       // Is uppercase letter (method)
c.is-lower()       // Is lowercase letter (method)
c.to-upper()       // Convert to uppercase (ASCII) (method)
c.to-lower()       // Convert to lowercase (ASCII) (method)
c.to-bytes()       // Unicode → UTF-8 bytes (method)
c.to-str()         // Unicode → string (UTF-8, method)
```

#### str — String Operations

```no
ok = a.eq(b, n)               // Equality comparison (method)
ok = a.eq-full(b)             // Full string equality (method)
dst = s.copy()                // String copy (method)
s.fill(val byte)              // Fill with byte value (method)
pos = s.index(sub)            // Substring position
pos = s.index-from(sub, from) // Substring position from index
pos = s.last-index(sub)       // Last index of substring
pos = s.rindex(sub)           // Reverse index of substring
pos = s.rindex-from(sub, from) // Reverse index from position
ok = s.contains(sub)          // Contains
ok = s.starts-with(sub)       // Prefix check
ok = s.ends-with(sub)         // Suffix check
s.to-upper()                  // Convert to uppercase
s.to-lower()                  // Convert to lowercase
out = s.trim()                // Trim leading/trailing whitespace
out = s.repeat(n)             // Repeat
out = s.slice(start, end)     // Slice
b = s.to-bytes()              // Convert to []byte
s = b.to-str()                // []byte to str (method)
hex = s.to-hex()              // Convert to hex string
out = s.from-hex()            // Parse hex string to []byte (returns ?[]byte)
v = s.to-i64()                // String to i64 (returns ?i64)
v = s.to-i8()                 // String to i8 (returns ?i8)
v = s.to-i16()                // String to i16 (returns ?i16)
v = s.to-i32()                // String to i32 (returns ?i32)
v = s.to-u8()                 // String to u8 (returns ?u8)
v = s.to-u16()                // String to u16 (returns ?u16)
v = s.to-u32()                // String to u32 (returns ?u32)
v = s.to-u64()                // String to u64 (returns ?u64)
v = s.to-byte()               // String to byte (returns ?byte)
v = s.to-f64()                // String to f64 (returns ?f64)
v = s.to-f32()                // String to f32 (returns ?f32)
v = s.to-bool()               // String "true"/"false" to bool (returns ?bool)
s = v.to-str()                // i64 to string (method)
out = s.reverse()             // Reverse
c = s.compare(b)              // Lexicographic comparison
n = s.count()                 // Total code point count
cp = s.decode-cp()            // Decode first Unicode code point (method)
val = s.replace-char(old, new) // Replace character (returns result string)
val = s.replace-str(old, new) // Replace substring (returns result string)
val = s.replace-n(old, new, n) // Replace up to n occurrences
out = s.trim-char(c)          // Trim specified character
ok = s.empty()                // Is empty
s.clear()                     // Clear (len=0, in-place)
out = s.with-cap(cap)         // Create new string with specified capacity (len=0)
out = s.with-len(len)         // Create new string with specified length (len=cap)
out = s.with-len-cap(len, cap) // Create new string with specified length and capacity
parts = s.split(sep)          // Split by separator (returns []str, method)
parts = s.split-n(sep, n)     // Split with max n parts (method)
lines = s.lines()             // Split into lines (method)
fields = s.fields()           // Split into whitespace-delimited fields (method)
parts = ss.join(sep)          // Join []str with separator (method)
cp = s.at(idx)                // Get character at index (method)
```

#### number — Numeric Operations

```no
number.max(a, b)                     // Maximum
number.min(a, b)                     // Minimum
r = num.clamp(lo, hi)         // Clamp to range (method)
r = number.abs(a)                    // Absolute value (num generic)
r = num.sign()                // Sign (-1/0/1, method)
number.even(v)                       // Even/odd check
number.odd(v)
number.gcd(a, b)                     // Greatest common divisor
number.lcm(a, b)                     // Least common multiple
r = number.pow(a, n)                 // Integer power
number.i64-to-f64(v)                 // Numeric conversion
number.f64-to-i64(v)
s = int.to-str()              // i64 to string (method)
s = i8.to-str()                      // i8 to string (method)
s = i16.to-str()                     // i16 to string (method)
s = i32.to-str()                     // i32 to string (method)
s = u8.to-str()                      // u8 to string (method)
s = u16.to-str()                     // u16 to string (method)
s = u32.to-str()                     // u32 to string (method)
s = u64.to-str()                     // u64 to string (method)
s = byte.to-str()                    // byte to string (method)
s = char-to-str(c)                   // char to string
s = f64.to-str()                     // f64 to string (method)
s = f32.to-str()                     // f32 to string (method)
q = number.div(a, b)                 // Integer division quotient
r = number.mod(a, b)                 // Modulo
number.swap(a, b)                    // Swap
yes = float.is-nan()          // NaN check (method)
yes = float.is-inf()          // Inf check (method)

// Constants
LN10                            // ln(10) ≈ 2.302585

// Range constants
i8.MIN / MAX                  // -128 / 127
i16.MIN / MAX                 // -32768 / 32767
i32.MIN / MAX                 // -2147483648 / 2147483647
i64.MIN / MAX                 // -2^63 / 2^63-1
u8.MIN / MAX                  // 0 / 255
u16.MIN / MAX                 // 0 / 65535
u32.MIN / MAX                 // 0 / 4294967295
u64.MIN / MAX                 // 0 / 2^64-1
```

#### byte — Byte Operations

```no
out = i64.to-bytes-be()         // i64 → big-endian [8]byte
out = i64.to-bytes-le()         // i64 → little-endian [8]byte
v = []byte.to-i64-be()          // big-endian []byte → i64 (1~8 bytes)
v = []byte.to-i64-le()          // little-endian []byte → i64 (1~8 bytes)
s = []byte.to-str()             // []byte to str (method)
s = []byte.to-hex()             // []byte → uppercase hex string
s = []byte.to-hex-lower()       // []byte → lowercase hex string
s = byte.to-str()               // byte to str (method)

// Constants
HEX-UPPER = '0123456789ABCDEF'
HEX-LOWER = '0123456789abcdef'
```

#### vec — Slice Operations

```no
v = vec.vec-create(n, val)         // Create slice of length n, filled with val
ok = []t.eq(a, b, n)           // Equality comparison
n = []t.len()                  // Length
[]t.push(val)                   // Append (auto-grow)
[]t.clear()                     // Clear (len=0, cap/data unchanged)
v = []t.with-cap(cap)          // Create new slice with specified capacity (len=0)
v = []t.with-len(len)          // Create new slice with specified length (len=cap)
v = []t.with-cap-len(cap, len) // Create new slice with specified capacity and length
val, new-n = []t.pop()         // Pop
found = []t.contains(n, val)   // Contains (n is length)
[]t.reverse(n)                  // Reverse first n elements
[]t.clone(dst)                  // Copy to dst
[]t.fill(n, val)                // Fill first n elements
arr = []t.to-arr()             // Convert to array
[]t.sort-asc()                  // Ascending sort (method)
[]t.sort-desc()                 // Descending sort (method)
```

#### arr — Array Operations

```no
out = [n]t.clone()             // Copy
ok = [n]t.eq(b)                // Equality comparison
[n]t.fill(val)                  // Fill
[n]t.reverse()                  // Reverse
ok = [n]t.contains(val)        // Contains
v = [n]t.to-vec()              // Convert to slice
v = [n]t.max()                 // Maximum
v = [n]t.min()                 // Minimum
v = [n]t.sum()                 // Sum
i = [n]t.index-of(val)          // Index
v = [n]t.last()                // Last element
v = [n]t.first()               // First element
[n]t.sort-asc()                 // Ascending sort
[n]t.sort-desc()                // Descending sort
```

#### sort — Sort Constants

```no
sort.asc                         // Ascending
sort.desc                        // Descending
```

---

### OS & Files

#### os — Operating System Interface

Provides environment variables, directory operations, process management, system information, time, etc. For file read/write functionality, see the `fs` module.

```no
// Environment variables
val = os.get-env(key)
os.set-env(key, val)

// Directory
dir = os.get-wd()
os.ch-dir(dir)
os.mkdir(path, mode)
os.rmdir(path)                         // Remove empty directory

// Process
os.exit(code)
pid = os.get-pid()
ppid = os.get-parent-pid()             // Parent process ID

// System information
name = os.host-name()
arch = os.get-arch()
msg = os.strerror(errnum)              // Convert errno to human-readable message
errno = os.get-errno()                 // Get last errno value

// User and group
uid = os.getuid()                      // Current user ID
gid = os.getgid()                      // Current group ID
euid = os.geteuid()                    // Effective user ID
egid = os.getegid()                    // Effective group ID
gids = os.getgroups()                  // Supplementary group IDs
login = os.get-login()                 // Login user name
hostid = os.get-host-id()              // Host ID (32-bit)

// File permissions and info
ok = os.ch-mod(path, mode)             // Change file permissions
ok = os.chown(path, uid, gid)          // Change file owner/group
mode = os.stat-mode(path)              // Get file mode bits
uid = os.stat-uid(path)                // Get file owner uid
gid = os.stat-gid(path)                // Get file group gid
mtime = os.stat-mtime(path)            // Get file modification time

// System configuration
val = os.sysconf(name)                 // Query system config limit
n = os.num-cpu()                       // Number of online CPUs
u = os.uname()                         // Kernel and architecture info (utsname)

// Process control
ok = os.set-priority(which, who, prio) // Set process priority
prio = os.get-priority(which, who)     // Get process priority
pid = os.set-sid()                     // Create new session, return pgid
ok = os.signal(sig, handler)           // Set signal handler (0=DFL, 1=IGN)
ok = os.flock(fd, op)                  // File advisory lock (1=SH,2=EX,4=UN,8=NB)

// Misc system
val = os.sysctl(name)                  // Query sysctl parameter (returns str, ok)
name = os.get-domain-name()            // NIS domain name
os.syslog(priority, msg)               // Write to system log
ok = os.chroot(path)                   // Change root directory (needs root)
name = os.ttyname(fd)                  // Get terminal name (returns str)

// Time (builtins, map to nolang.now_s/ms/us/ns and nolang.sleep_s/us/ns)
sec = os.now()
ms = os.now-ms()
us = os.now-us()
ns = os.now-ns()
os.sleep(sec)
os.sleep-us(us)
os.sleep-ns(ns)

// Command-line arguments
count = os.args()
val = os.arg(idx)

// utsname struct (returned by os.uname())
utsname {
    sysname str
    nodename str
    release str
    version str
    machine str
}

// Windows-specific
ok = os.win-wsa-startup()              // Initialize Winsock 2.2 (Windows only)
```

#### fs — File System Tools

Wraps open files with the `file` struct and paths with the `path` struct. Defines the `fd` newtype (`fd = i64`) to prevent file descriptors from being confused with arbitrary `i64` values. Also provides an `embed` struct for compile-time embedded filesystems (via `#{embed='dir/path'}`).

```no
// fd newtype (underlying i64, type-distinct from i64)
fd = i64

// File struct
file {
    fd fd
    path str
}

// Standard files (integer literals are allowed for fd initialization)
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

// Open file (with options)
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
f = fs.open(path, opts)             // Open file, returns ?file on success, err on failure

// file methods
read-n = f.read(buf, n)              // Read up to n bytes into buf
line = f.read-line()                  // Read one line (?str, nil=EOF or error)
content = f.read-bytes()             // Read entire file as ?[]byte (nil=empty, err=failure)
content = f.read-str()               // Read entire file as ?str (nil=empty, err=failure)
written = f.write(data, n)           // Write n bytes from data
ok = f.write-str(data)               // Write string to file (overwrite), returns ?bool
ok = f.write-bytes(data)             // Write []byte to file (overwrite), returns ?bool
ok = f.append(data, n)               // Append data to file
ok = f.copy-to(dst-path)             // Copy file to target path
ok = f.close()                       // Close (standard files stdin/stdout/stderr are not closed)
yes = f.is-open()                    // Check if file is open (fd >= 0)
sz = f.size()                        // File size via fstat(fd), eliminates TOCTOU

// Built-in functions (registered in src/builtin/os.go, documented as comments in fs.no)
// These are ForwardFunc/CLibCall builtins — no .no implementation needed.

// File read/write (deprecated, prefer read-bytes/write-bytes for error handling)
// [deprecated] read-file: 改用 read-bytes（支援錯誤處理、TOCTOU 安全、循環 read）
//   空切片表示失敗，無法區分空檔案與錯誤
// [deprecated] write-file: 改用 write-bytes（支援錯誤處理、循環 write）
//   無法區分部分寫入與完全失敗
data = fs.read-file(path)           // [deprecated] Read entire file as []byte (empty on error, cannot distinguish empty from failure)
ok = fs.write-file(path, data)      // [deprecated] Write []byte to file (overwrite). Returns true on success
fd = fs.open-read(path)             // Open read-only (O_RDONLY)
fd = fs.open-write(path)            // Open for writing (O_WRONLY|O_CREAT|O_TRUNC, 0644)
fd = fs.open-file(path, flags, mode) // Open with custom flags and permissions
n = fs.read(fd, buf, n)             // Low-level read from fd into buf
written = fs.write(fd, data, n)     // Low-level write to fd
ok = fs.close(fd)                   // Low-level close

// File management
ok = fs.remove(path)                // Delete file (unlink)
ok = fs.rename(old, new)            // Rename file
ok = fs.symlink(target, linkpath)   // Create symbolic link
ok = fs.link(oldpath, newpath)      // Create hard link
ok = fs.copy-file(src, dst)         // Copy file content

// File information
ok = fs.exists(path)                // Check if path exists (follows symlinks)
ok = fs.is-file(path)               // Check if regular file
ok = fs.is-dir(path)                // Check if directory
sz = fs.stat-size(path)             // Get file size (returns ?i64)
sz = fs.file-size(path)             // Same as stat-size (returns ?i64)
sz = fs.fstat-size(fd)              // Get file size via fstat(fd), eliminates TOCTOU (returns ?i64)
mode = fs.stat-mode(path)           // Get file mode (st_mode, returns i64)
uid = fs.stat-uid(path)             // Get file owner uid (returns i64)
gid = fs.stat-gid(path)             // Get file group gid (returns i64)
mtime = fs.stat-mtime(path)         // Get file modification time (Unix seconds, returns i64)
ok = fs.lstat(path)                 // Get symlink info (does not follow link target)

// Directory operations
dirp = fs.open-dir(path)             // Open directory (returns handle, 0 on failure)
name = fs.read-dir(dirp)             // Read next dir entry (returns str, ok bool)
ok = fs.close-dir(dirp)              // Close directory handle
names = fs.list-dir(path)            // List all entry names (includes . and ..)
names = fs.dir-entries(path)          // List entries (excludes . and ..)
paths = fs.walk(root)                // Recursively walk directory tree

// Path resolution
abs = fs.realpath(path)              // Resolve to absolute canonical path
target = fs.readlink(path)           // Read symbolic link target (returns str, ok bool)

// Temp file/directory
name, fd = fs.mkstemp(tmpl)          // Create temp file (template ending XXXXXX)
name = fs.mkdtemp(tmpl)             // Create temp directory (template ending XXXXXX)

// Special files
ok = fs.mkfifo(path, mode)          // Create named pipe (FIFO)
ok = fs.mknod(path, mode, dev)      // Create special file (device node)
ok = fs.truncate(path, length)      // Truncate/extend file to length
fs.sync()                           // Flush filesystem buffers to disk
ok = fs.touch-file(path)            // Update file timestamps to current time
ok = fs.utime(path, atime, mtime)   // Set file access and modification times
ok = fs.rmdir(path)                 // Remove empty directory

// Standard input
line = fs.get-line()                // Read one line from stdin (?str, nil=EOF)

// Convenience wrappers (Nolang-implemented in fs.no, call builtins internally)
content = fs.read-str(path)          // Read entire file as ?str (nil=empty, err=failure)
content = fs.read-bytes(path)        // Read entire file as ?[]byte (nil=empty, err=failure)
ok = fs.write-str(path, data)        // Write string to file (overwrite), returns ?bool
ok = fs.write-bytes(path, data)      // Write []byte to file (overwrite), returns ?bool

// embed: compile-time embedded filesystem (initialized by #{embed='dir/path'})
//   #{embed='../frontend/dist'}
//   DIST fs.embed
//   data = DIST.read('index.html')   // Read embedded file, returns ([]byte, bool)
//   ok = DIST.exists('css/style.css') // Check if embedded file exists
embed {
    count i64
    paths str                        // All paths concatenated (\0 separated)
    pathStarts []i64                 // Start offset of each path in paths
    pathLens []i64                   // Length of each path
    blob []byte                      // All file contents concatenated
    dataStarts []i64                 // Start offset of each file in blob
    dataLens []i64                   // Length of each file
}

// Windows-specific (only available on win-amd64/win-arm64)
bufptr = fs.win-find-first-file(path) // FindFirstFileA, returns handle (0=failure)
name = fs.win-find-next-file(bufptr)  // FindNextFileA, returns (name, ok)
ok = fs.win-find-close(bufptr)        // FindClose

// open() flag constants (platform-specific, shown for macOS)
O-RDONLY = 0
O-WRONLY = 1
O-RDWR = 2
O-CREAT = 512       // macOS=512, Linux=64, Windows=256
O-TRUNC = 1024      // macOS=1024, Linux=512, Windows=512
O-APPEND = 8        // macOS=8, Linux=1024, Windows=8
O-EXCL = 2048       // macOS=2048, Linux=128, Windows=1024
```

#### env — Environment Variables (simplified wrapper)

```no
val = env.get(key)
val = env.lookup(key)               // Returns ?str (nil=not found)
env.set(key, val)
env.unset(key)
val = env.get-with-default(key, default)
ok = env.is-set(key)
```

#### args — Command-line Arguments

```no
n = args.count()
arg = args.get(i)
name = args.program()
ok = args.has-flag(name)
val = args.get-option(name)
arg = args.get-positional(i)
```

#### path — Path Operations

Wraps path strings with the `path` struct; all operations are provided as methods:

```no
SEP = 47     // '/' (ASCII)
DOT = 46     // '.'

// Struct
path {
    p str
}

// Path join and split (modifies .p in place)
p = path{
    p: '/a/b/c.txt'
}
p.join(b str)           // Join two paths (modifies in place)
p.base() (out)           // Get filename
p.dir()                  // Get directory (modifies .p in place)
p.ext() (out)            // Get file extension
p.clean()                // Normalize (modifies .p in place)
p.split() (f str)        // Split into directory + filename (.p becomes directory, returns filename)

// Path checks
p.is-abs() (yes bool)    // Whether it is an absolute path

// File system operations (delegates to fs built-in functions)
p.exists() (yes bool)        // Whether it exists
p.is-dir() (yes bool)        // Whether it is a directory
p.is-file() (yes bool)       // Whether it is a file
p.size() (sz i64)            // File size
p.make-dir() (ok bool)       // Create directory
p.remove() (ok bool)         // Delete
p.rename(new-p str) (ok bool)    // Rename
p.change-dir() (ok bool)     // Change working directory

// Constructor methods
path.current() (out path)    // Get current working directory
```

#### bufio — Buffered Reading

```no
r = reader.init(fd, buf)       // Initialize buffered reader (returns reader)
ok = reader.fill()              // Fill buffer
b = reader.read-byte()          // Read one byte (?byte, nil=EOF)
ok = reader.read-line(line)     // Read one line into line
reader.close()                  // Close
```

#### io — I/O Abstraction

Provides `io-reader` and `io-writer` structs to unify read/write operations across files, standard I/O, and other streams:

```no
// Standard file descriptors
STDIN-FD = 0
STDOUT-FD = 1
STDERR-FD = 2

// io-reader struct
io-reader {
    fd i64
}
r = io-reader.from-fd(fd)      // Create from fd
r = io-reader.from-stdin()     // Create from standard input
read-n = r.read(buf, n)        // Read n bytes
b = r.read-byte()              // Read one byte (?byte, nil=EOF)
line = r.read-line()           // Read one line (?str, nil=EOF)
total = r.read-all(buf, size)  // Read all

// io-writer struct
io-writer {
    fd i64
}
w = io-writer.from-fd(fd)      // Create from fd
w = io-writer.from-stdout()    // Create from standard output
w = io-writer.from-stderr()    // Create from standard error
written = w.write(data, n)     // Write n bytes
written = w.write-str(s)       // Write entire string
written = w.write-byte(b)      // Write one byte
written = w.write-line(s)      // Write string + newline

// Convenience functions
n = io.out(s)                   // Write to stdout (no newline)
n = io.outln(s)                 // Write to stdout (with newline)
n = io.err(s)                   // Write to stderr (no newline)
n = io.errln(s)                 // Write to stderr (with newline)
line = io.read-line()           // Read one line from stdin (?str, nil=EOF)
```

#### regexp — Regular Expression

Wraps pattern with the `regexp` struct; underlying implementation uses C standard library `regex.h`:

```no
// Struct
regexp {
    pattern str
}

// Methods
re = regexp{
    pattern: '^hello'
}
matched = re.matches(text)        // Check whether it matches
result = re.find(text)           // Find first matching substring
pos = re.match-at(text, start)    // Match at specific position
ok = re.compile()               // Compile pattern (method)

// Top-level convenience functions
re = regexp-compile(pattern)      // Compile regex pattern
result = regexp-find(pattern, text) // Find first match
yes = regexp-match(pattern, text)   // Check if pattern matches text
```

#### process — Process Operations

Provides process creation, standard stream access, process waiting, and process information query. Underlying implementation uses POSIX fork/exec/pipe/waitpid:

```no
// Signal constants
SIG-TERM = 15
SIG-KILL = 9
SIG-INT = 2
SIG-STOP = 19
SIG-CONT = 18
SIG-CHLD = 17
WNOHANG = 1

// Struct
process {
    pid i64
    stdin-fd i64
    stdout-fd i64
    stderr-fd i64
    exit-code i64
    running i64
}

// Process creation
p = process.new()
ok = p.start-with-opts(program, args, dir, input, merge-err) // Start child process (no wait)
ok = p.start(program, arg)          // Convenience: fork + exec, capture stdout

// Process waiting
status = p.wait()                   // Block waiting for child process to end
status = p.wait-nohang()            // Non-blocking polling; nil=still running

// Process control
ok = p.kill(sig)                    // Send signal
ok = p.terminate()                  // SIG-TERM
ok = p.force-kill()                 // SIG-KILL

// Standard stream operations
read-n = p.read(buf, n)             // Read from stdout
line = p.read-line()               // Read one line (?str, nil=EOF)
content, n = p.read-all()           // Read all stdout
written = p.write(data, n)          // Write to stdin
p.close-stdin()                    // Close stdin pipe
p.close-stdout()                   // Close stdout pipe
p.close-stderr()                   // Close stderr pipe

// Process information
pid = p.pid-of()                    // Child process ID
code = p.exit-code-of()             // Exit code
yes = p.is-running()                // Whether still running
pid = process.parent-pid()          // Parent process ID

// Lifecycle
p.close()                          // Close all pipes and wait

// Convenience functions
status = process.process-run(cmd)           // Execute shell command
content, code = process.new().output(program, arg) // Execute and capture output
status = process.cmd(cmd)                     // Execute command string
status = process.shell(cmd)                   // Execute via shell
status = process.exec(program, args)          // Execute with args
status = process.spawn(program, args)         // Spawn child process
status = process.wait(pid)                    // Wait for specific PID

// Process listing
pids = process.list-all()                     // List all processes
pids = process.find-by-name(name)             // Find by process name
pids = process.find-match(pattern)             // Find matching pattern
```

#### net — Network Operations

Provides TCP networking capabilities, including server listening, client connections, and data send/receive. Underlying implementation uses POSIX socket API:

```no
// Network constants
AF-INET = 2
SOCK-STREAM = 1
SOCK-DGRAM = 2
SOL-SOCKET = 65535
SO-REUSEADDR = 4
SO-RCVTIMEO = 18
BACKLOG = 128
NET-BUF-SIZE = 4096

// listener struct
listener {
    fd i64
}

// Listening operations
l = listener{}
ok = l.listen(host, port)            // Establish TCP listener (socket+setsockopt+bind+listen)
c = l.accept()                       // Accept connection (?conn, nil=no connection)
c = l.accept-with-timeout(ms)        // Accept with timeout (?conn)
fd = l.fd-of()                       // Get fd
l.set-timeout(ms)                   // Set accept timeout
l.close()                           // Close listening socket

// conn struct
conn {
    fd i64
}

// Connection operations
c = conn{}
ok = c.dial(host, port)              // Establish TCP connection (socket+connect)
written = c.send(data)               // Send string
read-n = c.recv(buf, n)              // Receive data into buf
line = c.recv-line()                 // Receive one line (?str, nil=EOF, max 4096 bytes)
content, total = c.recv-all()        // Receive all until connection closed
read-n = c.recv-with-timeout(buf, n, ms) // Receive with timeout
written = c.send-with-timeout(data, ms) // Send with timeout
c.set-recv-timeout(ms)             // Set receive timeout
c.set-timeout(ms)                 // Set send/receive timeout
c.close()                           // Close connection
fd = c.fd-of()                       // Get fd

// Convenience functions
l = net.listen-on(host, port)        // Create listener and start listening (?listener)
c = net.dial-to(host, port)          // Create connection and dial (?conn)
c = net.dial-timeout(host, port, ms) // Dial with timeout (?conn)

// UDP operations
uc = udp-conn{}
ok = uc.open(host, port)             // Open UDP connection
uc.send(data)                      // Send string
uc.send-bytes(data)                // Send []byte
data, n = uc.recv(buf, n)           // Receive
uc.recv-bytes(buf, n)              // Receive []byte
uc.set-timeout(ms)                 // Set timeout
uc.close()

// ICMP/ping operations
ic = icmp-conn{}
ok = ic.open()                      // Open ICMP socket
ok = ic.ping-host(host, count)       // Ping host
ok = ic.ping-once(host)              // Single ping
rtt = ping(host)                     // Ping and get RTT
rtts = ping-count(host, count)      // Ping multiple times

// System info (macOS)
arps = arp-list()                    // ARP table entries
routes = route-list()               // Route table entries
ifs = ifconfig-list()               // Network interfaces

// Encrypted connection support
ec = enc-conn{}
ok = ec.init(key)                    // Initialize encryption
ec.send-encrypted(data)             // Send encrypted
ec.recv-encrypted(buf)              // Receive encrypted
ec.gen-iv()                        // Generate IV
c = enc-dial(host, port, key)        // Dial encrypted
ec = enc-accept(fd, key)            // Accept encrypted

// WebSocket helper
parts = split-ws(url)                // Split WebSocket URL into host/port/path
```

#### net/ip — IP Address Operations

Provides IPv4 address parsing, validation, conversion, and classification. Pure Nolang implementation:

```no
// Default address constants
IP-ZERO       // 0.0.0.0
IP-LOOPBACK   // 127.0.0.1
IP-ANY        // 0.0.0.0
IP-BROADCAST  // 255.255.255.255

// ip-addr struct
ip-addr {
    a i64
    b i64
    c i64
    d i64
}

// Parsing and conversion
ip = ip-addr{}
ok = ip.parse('192.168.1.1')         // Parse from string
s = ip.to-str()                      // Convert to string '192.168.1.1'
v = ip.to-u32()                      // Convert to u32 (big-endian)
ip.from-u32(v)                      // Create from u32

// Address classification
yes = ip.is-loopback()               // 127.0.0.0/8
yes = ip.is-private()                // 10/8, 172.16/12, 192.168/16
yes = ip.is-zero()                   // 0.0.0.0
yes = ip.is-broadcast()              // 255.255.255.255
yes = ip.is-multicast()              // 224.0.0.0/4
yes = ip.is-link-local()             // 169.254.0.0/16
yes = ip.is-class-a()                // Class A (1~126)
yes = ip.is-class-b()                // Class B (128~191)
yes = ip.is-class-c()                // Class C (192~223)

// Comparison and subnet
yes = ip.equal(other)                // Address equality comparison
yes = ip.in-subnet(base, prefix-len) // Subnet containment check

// Convenience functions
addr = ip.ip-parse(s)                   // Quick parse (?ip-addr, nil=invalid)
yes = ip.ip-is-loopback(s)              // Quick loopback check
yes = ip.ip-is-private(s)               // Quick private check
```

#### net/sse — Server-Sent Events Client

Supports SSE streaming reception conforming to W3C EventSource spec. Underlying implementation uses HTTP/1.1 long connections, supporting both plain HTTP and HTTPS (TLS):

```no
// sse-event struct
sse-event {
    event str       // Event type (default 'message')
    data str        // Event data (multi-line data joined with \n)
    id str          // Event ID
    retry i64       // Reconnect wait milliseconds (-1=not set)
}

// sse-client struct
sse-client {
    fd i64              // TCP socket fd
    tls-c tls.conn      // TLS connection
    use-tls bool        // Whether to use TLS
    connected bool      // Connection state
    host str            // Server hostname
    port i64            // Port number
    path str            // Request path
    last-event-id str   // Last received event ID
    recv-buf str        // Receive buffer
    recv-buf-len i64    // Buffer data length
}

// Connection and event reception
client = sse.sse-connect('http://host:3000/events')  // Returns ?sse-client
client: {
    nil -> print('connect failed')
    ->
        ev = client.next-event()     // Returns ?sse-event (nil=EOF, err=error)
        ev: {
            nil -> print('connection closed')
            err -> print('error: ' - it)
            -> print(ev.data)
        }
        client.close()
}

// Other methods
yes = client.is-connected()         // Check connection state
ok = client.reconnect()             // Reconnect (uses last-event-id)
```

#### net/http — HTTP/1.1 Client

Provides an HTTP/1.1 protocol client, supporting GET, POST, PUT, DELETE, PATCH and other methods, with optional TLS:

```no
// Structs
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

// Convenience functions
resp = http.http-get(url)                        // GET request (?http.response)
resp = http.http-post(url, body)                  // POST request (?http.response)
resp = http.http-do(method, url, body)            // Custom method (?http.response)

// Using request object
req = http-request{}
req.init('POST', url, body)
req.add-header('Content-Type', 'application/json')
resp = http.http-do-req(req)                      // Send request (?http.response)

// Parse response headers
resp.parse-headers()
```

#### net/http2 — HTTP/2.0 Client (RFC 7540)

Supports HTTP/2 frame parsing and connection management, supports h2c prior knowledge mode:

```no
// Frame struct
http2-frame {
    length i64
    frame-type i64
    flags i64
    stream-id i64
    payload str
}

// Connection struct
http2-conn {
    fd i64
    next-stream-id i64
    send-window i64
    recv-window i64
    initialized bool
    use-tls bool
}

// Connection and request
c = http2.connect(host, port)                // Establish h2c connection (?http2-conn)
c = http2.connect-tls(host, port)            // Establish TLS connection (?http2-conn)
resp = http2.do(method, url, body)           // Send request (?http.response)
resp = http2.get(url)                        // GET request
resp = http2.post(url, body)                 // POST request
resp = http2.put(url, body)                   // PUT request
resp = http2.delete(url)                     // DELETE request
resp = http2.patch(url, body)                // PATCH request

// Connection methods
ok = c.send-data(data, stream-id)           // Send data on stream
ok = c.send-headers(headers, count, stream-id, end-stream) // Send headers
ok = c.send-settings()                      // Send SETTINGS frame
frame = c.recv-frame()                       // Receive frame
c.close()

// Frame operations
frame = http2-frame{}
pos = frame.parse(data, pos)                 // Parse frame (?i64)
pos = frame.serialize(buf, pos)              // Serialize frame
```

#### net/http3 — HTTP/3.0 Client (RFC 9114)

HTTP/3 client based on QUIC protocol:

```no
// Method constants
HTTP3-METHOD-GET = 'GET'
HTTP3-METHOD-POST = 'POST'
HTTP3-METHOD-PUT = 'PUT'
HTTP3-METHOD-DELETE = 'DELETE'
HTTP3-METHOD-PATCH = 'PATCH'
HTTP3-METHOD-HEAD = 'HEAD'
HTTP3-METHOD-OPTIONS = 'OPTIONS'

// Convenience functions
c = http3.connect(host, port)                // Establish QUIC connection (?http3-conn)
resp = http3.http3-do(method, url, body)     // Send request (?http.response)
resp = http3.get(url)                        // GET request
resp = http3.post(url, body)                 // POST request

// Frame operations
frame, pos = http3.parse-frame(data, pos)    // Parse HTTP/3 frame
pos = http3.encode-frame(buf, pos, type, flags, payload) // Encode frame
pos = http3.encode-data-frame(buf, pos, data) // Encode DATA frame
pos = http3.encode-headers-frame(buf, pos, headers) // Encode HEADERS frame
pos = http3.encode-settings-frame(buf, pos, settings) // Encode SETTINGS frame

// Header operations
pos = http3.encode-header(buf, pos, name, value) // Encode single header
pos = http3.encode-headers(buf, pos, names, values, count) // Encode multiple headers
name, value, pos = http3.decode-header(buf, pos) // Decode single header

// URL and response
parts = http3.parse-url(url)                // Parse URL
val = resp.get-header(name)                  // Get response header
```

#### net/ws — WebSocket Client and Server (RFC 6455)

Supports full-duplex communication over WebSocket protocol, can act as client or server:

```no
// Message struct
ws-message {
    opcode i64           // 0=continuation, 1=text, 2=binary, 8=close, 9=ping, 10=pong
    data str
    fin bool
}

// Server
s = ws.listen-on(host, port)                    // Create listener (?ws-server)
c = s.accept()                               // Accept connection (?ws-server-conn)
msg = c.recv()                               // Receive message (?ws-message)
ok = c.send-text(text)                       // Send text
ok = c.send-binary(data)                     // Send binary
ok = c.send-ping()                           // Send ping
ok = c.send-pong()                           // Send pong
ok = c.send-close()                          // Send close
c.close()

// Client
c = ws.connect(url)                             // Connect to server (?ws-client)
msg = c.recv()                               // Receive message (?ws-message)
ok = c.send-text(text)                       // Send text
ok = c.send-binary(data)                     // Send binary
ok = c.send-ping()                           // Send ping
ok = c.send-pong()                           // Send pong
ok = c.send-close()                          // Send close
yes = c.is-connected()                       // Connection state
c.close()

// WebSocket utilities
frame = build-frame(op, data)                // Build WebSocket frame
accept = compute-accept(key)                // Compute Sec-WebSocket-Accept
client-key = gen-client-key()                // Generate client key
mask-key = gen-mask-key()                    // Generate mask key
```

#### net/tls — TLS 1.2/1.3 Client (Pure Nolang Implementation)

Provides TLS encrypted connections, supporting TLS 1.2 and 1.3:

```no
// Connection
c = tls.dial(host, port)                        // Establish TLS connection (?tls.conn)
c = tls.dial-12(host, port)                     // TLS 1.2 connection (?tls.conn)
n = c.send(data)                             // Send encrypted data (?i64)
n = c.recv(buf, n)                           // Receive decrypted data (?i64)
c.close()

// Server support
ok = tls.server-listen(host, port, cert, key)    // Start TLS server
ok = tls.server-accept(fd)                       // Accept TLS connection
ok = tls.https-serve-once(fd, cert, key)        // Handle one HTTPS request
```

#### net/client — High-level TCP Client

Wraps the `conn` struct, providing auto-reconnect and other features:

```no
c = client.net-client(host, port)                   // Create client (?client)
ok = c.connect(host, port)                   // Connect
ok = c.reconnect()                           // Reconnect
written = c.send(data)                       // Send
read-n = c.recv(buf, n)                      // Receive
line = c.recv-line()                         // Receive one line (?str)
response = c.request(data)                   // Request-response mode (?str)
yes = c.is-connected()                       // Connection state
c.close()
```

#### net/quic — QUIC Protocol (RFC 9000)

Provides QUIC transport protocol implementation, serving as the underlying transport layer for HTTP/3:

```no
c = quic.dial(host, port)                    // Establish QUIC connection (?quic.conn)
n = c.send(data, n)                          // Send data
n = c.recv(buf, n)                           // Receive data
c.close()
```

#### net/server — HTTP Server

```no
// HTTP server
s = server{}
ok = s.listen(host, port)                    // Start listening
c = s.accept()                               // Accept connection (?server-conn)
c = s.accept-nb()                            // Non-blocking accept
fd = s.fd-of()                               // Get server fd
s.close()

// server-conn methods
ok = c.parse()                              // Parse HTTP request
val = c.get-header(name)                    // Get request header
val = c.get-cookie(name)                    // Get request cookie
val = c.get-signed-cookie(name, key)        // Get signed cookie
c.set-signed-cookie(name, val, key)         // Set signed cookie
ok = c.verify-basic-auth(user, pass)         // Verify basic auth
c.recv-more()                               // Receive more data
c.reset()                                   // Reset connection state
ok = c.write-status(code)                   // Write status line
ok = c.write-header(name, value)            // Write header
ok = c.write-headers-done()                // End headers
ok = c.write-body(data)                     // Write body
ok = c.write-text(text)                     // Write text response
ok = c.write-html(html)                     // Write HTML response
ok = c.write-json(json-str)                 // Write JSON response
ok = c.write-error(code, msg)               // Write error response
ok = c.write-redirect(url)                  // Write redirect response
c.close()

// HTTPS server
hs = https-server{}
ok = hs.listen(host, port, cert, key)       // Start HTTPS listening
c = hs.accept()                             // Accept HTTPS connection (?https-server-conn)
hs.close()

// Utility
yes = secure-compare(a, b)                   // Constant-time string comparison
```

#### net/dns — DNS Resolution

```no
ip = dns.resolve(host)                       // Resolve hostname (?str)
ips = dns.resolve-all(host)                  // Resolve all IPs ([]str)
ip = dns.resolve-from(server, host)          // Resolve from specific DNS server
```

#### net/url — URL Parsing

```no
u = url.url-parse(url)                           // Parse URL
s = u.to-str()                               // Convert to string

// URL operations
encoded = url.url-encode(s)                     // URL encode
encoded = url.url-encode-component(s)           // URL encode component
decoded = url.url-decode(s)                     // URL decode
query = url.url-build-query(params, values, count) // Build query string
joined = url.url-join(base, ref)                // Join URLs
user, pass = url.url-parse-basic-auth(url)      // Parse basic auth from URL
auth = url.url-basic-auth(user, pass)           // Build basic auth header

// url-info methods
q = u.get-query()                        // Get query parameters
u.set-query(q)                          // Set query parameters
```

#### net/cookie — HTTP Cookie

```no
c = cookie{}
c.parse(set-cookie-header)
s = c.to-str()

// Cookie parsing and building
parsed = cookie.parse-header(header)           // Parse Cookie header
parsed = cookie.parse-set(set-cookie)          // Parse Set-Cookie header
header = cookie.build-header(c)                // Build Cookie header
set = cookie.build-set(c)                     // Build Set-Cookie header

// Signed cookies
ok = cookie.sign(c, key)                      // Sign cookie
ok = cookie.verify(c, key)                    // Verify signed cookie

// Cookie jar
jar = cookie.jar{}
jar.parse-response(set-cookie-headers)          // Parse response cookies
val = jar.get(name)                           // Get cookie value
val = jar.get-verified(name, key)             // Get and verify signed cookie
jar.set(name, val)                            // Set cookie
jar.set-signed(name, val, key)                // Set signed cookie
jar.delete(name)                             // Delete cookie
jar.clear()                                 // Clear all
n = jar.count-of()                           // Cookie count
header = jar.build-header()                  // Build Cookie header
```

#### net/multipart — Multipart Form Data

```no
// Reader
r = multipart-reader{}
r.init(data, boundary)
ok = r.next()                                // Move to next part
name = r.field-name()                        // Current field name
filename = r.field-filename()                 // Current filename
data = r.field-data()                         // Current field data
ct = r.field-content-type()                  // Current content-type
yes = r.is-file()                            // Whether current part is a file
yes = r.has-current-part()                  // Whether has current part

// Writer
w = multipart-writer{}
w.init()
w.init-boundary(boundary)
boundary = w.boundary-of()                   // Get boundary
ct = w.content-type()                        // Get content-type header
w.write-field(name, value)                   // Write text field
w.write-file(name, filename, data)           // Write file field
w.write-encrypted-field(name, value, key)    // Write encrypted field
w.write-encrypted-file(name, filename, data, key) // Write encrypted file
w.close-form()                               // Close form

// Top-level functions
boundary = multipart-create-boundary()        // Create random boundary
boundary = multipart-gen-boundary()           // Generate boundary
boundary = multipart-extract-boundary(content-type) // Extract boundary from content-type
data = multipart-create(fields, count, boundary) // Create multipart data
fields = multipart-parse(data, boundary)       // Parse multipart data
data = multipart-decrypt-field(encrypted, key) // Decrypt field
```

#### net/hpack — HPACK Header Compression (HTTP/2)

```no
buf, n = hpack.hpack-encode(headers)
headers = hpack.hpack-decode(buf, n)

// Table operations
t = hpack-tables{}
t.init()
pos = t.encode-header(buf, pos, name, value)
pos = t.decode-headers(buf, pos)              // Decode headers
pos = t.dyn-add(name, value)                  // Add to dynamic table
idx = t.find(name)                          // Find in table
idx = t.find-name(name)                      // Find by name only
name, value = t.lookup(idx)                  // Lookup by index
```

#### net/proxy — Proxy Support

```no
c = proxy.dial(proxy-url, target-host, target-port)    // Dial via proxy
c = proxy.connect(proxy-url, host, port)               // Connect via proxy
c = proxy.dial-auth(proxy-url, host, port, user, pass)  // Dial with auth
c = proxy.socks5-dial(host, port, target-host, target-port) // SOCKS5 dial
c = proxy.socks5-connect(host, port, target)           // SOCKS5 connect

// Config-based
cfg = proxy.config{}
c = cfg.connect(host, port)                  // Connect via config
c = cfg.tls-connect(host, port)              // TLS connect via config
c = proxy.proxy-tls-dial(host, port, target)    // TLS proxy dial
```

#### net/pool — Connection Pool

```no
p = pool.create(capacity)                     // Create pool
p = pool.with-size(n)                         // Create with size
p.init(capacity)
c = p.get()                                  // Get connection from pool
p.put(c)                                     // Return connection
p.close-conn(c)                              // Close single connection
p.close-all()                                // Close all connections
n = p.total-count()                          // Total connections
n = p.active-count()                         // Active connections
n = p.idle-count()                           // Idle connections
```

#### net/unix — Unix Domain Socket

```no
l = unix.unix-listen-on(path)                  // Listen on Unix socket
l = unix-listener{}
ok = l.listen(path)                           // Listen
c = l.accept()                                // Accept (?unix-conn)
c = l.accept-with-timeout(ms)                  // Accept with timeout
fd = l.fd-of()                                // Get fd
p = l.path-of()                               // Get socket path
l.set-timeout(ms)                             // Set timeout
l.close()

c = unix.unix-dial-to(path)                   // Dial Unix socket
c = unix-dial-timeout(path, ms)               // Dial with timeout
c = unix-conn{}
ok = c.dial(path)                             // Connect
n = c.send(data)                              // Send
n = c.recv(buf, n)                            // Receive
n = c.recv-all(buf, n)                        // Receive all
n = c.recv-with-timeout(buf, n, ms)           // Receive with timeout
c.set-recv-timeout(ms)                        // Set recv timeout
c.set-timeout(ms)                             // Set timeout
c.close()
```

---

### Time & Date

#### time — Time Operations

```no
sec = time.now-s()                   // Current Unix timestamp (seconds)
ms = time.now-ms()                   // Current timestamp (milliseconds)
us = time.now-us()                   // Current timestamp (microseconds)
sec = time.since-s(start)             // Elapsed since start (seconds)
ms = time.since-ms(start)             // Elapsed since start (milliseconds)
us = time.since-us(start)             // Elapsed since start (microseconds)
out = time.format-time(t, fmt)        // Format time
time.sleep-ms(ms)                    // Sleep (milliseconds)
time.sleep-s(sec)                    // Sleep (seconds)
d = time.duration-between(start, end) // Elapsed (seconds)
d = time.duration-ms-between(s, e)    // Elapsed (milliseconds)
d = time.duration-us-between(s, e)    // Elapsed (microseconds)

// Date operations
yes = time.is-leap(year)               // Check if year is a leap year
unix = time.date-to-unix(year, month, day, hour, min, sec) // Date to Unix timestamp
d = time.now-date()                   // Get current date struct
ok = time.parse-date(s, str)          // Parse date string (?date)
s = time.format-date(d)               // Format date to string
d = time.unix-to-date(ts)             // Convert Unix timestamp to date struct
days = time.days-in-month-fn(year, month) // Get days in month
s = time.format-duration(sec)         // Format duration to human-readable string
s = time.format-time-str(t, fmt)      // Format time string

// Timer struct
t = timer{}
t = t.start()                         // Start timer
us = t.elapsed-us()                   // Elapsed microseconds
ms = t.elapsed-ms()                   // Elapsed milliseconds
s = t.elapsed-s()                     // Elapsed seconds
```

---

### Logging

#### log — Leveled Logging

```no
LEVEL-DEBUG = 0
LEVEL-INFO  = 1
LEVEL-WARN  = 2
LEVEL-ERROR = 3
LEVEL-FATAL = 4

log.set-level(lvl)
lvl = log.level()                      // Get current log level
log.debug(msg)
log.info(msg)
log.warn(msg)
log.error(msg)
log.fatal(msg)
```

---

### Data Structures

#### set — Set (Array-based)

```no
new-n = set.add(s, n, val)           // Add element
new-n = set.set-remove(s, n, val)        // Remove element
ok = set.contains(s, n, val)         // Whether it contains
new-an = set.union(a, an, b, bn)     // Union
out, n = set.intersection(a, an, b, bn)// Intersection
out, n = set.difference(a, an, b, bn)  // Difference
v = set.to-vec(s, n)                 // Convert to slice
sz = set.set-size(s, n)                   // Element count
yes = set.set-empty(s, n)                    // Whether empty
```

#### deque — Double-ended Queue

Double-ended queue implemented with a circular buffer, wrapped in the `deque` struct:

```no
// Struct
deque {
    buf []i64
    cap i64
    head i64
    tail i64
}

// Initialization
d = deque{
    buf: buf
    cap: 128
    head: 0
    tail: 0
}

// Methods
d.push-front(val)              // Push from front
d.push-back(val)               // Push from back
val = d.pop-front()             // Pop from front
val = d.pop-back()              // Pop from back
val = d.peek-front()            // Peek front element (?i64, nil=empty)
val = d.peek-back()             // Peek back element (?i64, nil=empty)
sz = d.size()                   // Size
yes = d.empty()                 // Whether empty
d.clear()                      // Clear
```

#### heap — Min Heap

Binary min heap wrapped in the `heap` struct:

```no
// Struct
heap {
    data []i64
    n i64
}

// Initialization
h = heap.init(data)            // Build heap

// Methods
h.push(val)                    // Push element
val = h.pop()                  // Pop minimum element (?i64, nil=empty)
val = h.peek()                 // Peek minimum element (?i64, nil=empty)
sz = h.size()                  // Size
yes = h.empty()                // Whether empty
```

#### stack — Stack

Last-in-first-out (LIFO) data structure, wrapped in the `stack` struct:

```no
// Struct
stack {
    data []i64
    n i64
}

// Initialization
buf [128]i64 = [0:128]
s = stack{
    data: buf
    n: 0
}

// Methods
s.push(val)                    // Push element
val = s.pop()                  // Pop top element (?i64, nil=empty)
val = s.peek()                 // Peek top element (?i64, nil=empty)
sz = s.size()                  // Size
yes = s.empty()                // Whether empty
s.clear()                      // Clear
```

#### map/linked-hash-map — Ordered Hash Map

Fixed capacity 64 (i64→i64), linear probing, doubly-linked list preserves insertion order:

```no
m = linked-hash-map{}
m.init()
m.put(key, val)
result = m.get(key)   // ?i64, nil=not found
found = m.contains(key)
removed = m.remove(key)
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(key, val)
```

#### map/hash-set — i64 Hash Set

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

#### map/str-map — str→str Hash Map

Fixed capacity 256, FNV-1a hash, linear probing:

```no
m = str-map{}
m.init()
m.put('key', 'val')
result = m.get('key')   // ?str, nil=not found
found = m.contains('key')
removed = m.remove('key')
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(k, v)
```

#### map/str-set — str Hash Set

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

#### map/tree-map — Ordered Map (AVL Tree)

Ordered map (i64→i64) implemented based on AVL self-balancing binary search tree, capacity 64:

```no
m = tree-map{}
m.clear()                           // Initialize
ok = m.put(key, val)                // Insert or update
val = m.get(key)                    // Lookup (?i64, nil=not found)
yes = m.contains(key)               // Check whether key exists
ok = m.remove(key)                  // Delete key
key = m.first()                     // Minimum key (?i64)
key = m.last()                      // Maximum key (?i64)
key = m.lower-bound(target)         // First key ≥ target (?i64)
key = m.upper-bound(target)         // First key > target (?i64)
m.for-each(k, v)                    // Traverse in ascending key order
sz = m.size()
yes = m.empty()
yes = m.full()
```

#### map/tree-set — Ordered Set (AVL Tree)

Ordered set (i64) implemented based on AVL self-balancing binary search tree, capacity 64:

```no
s = tree-set{}
s.clear()                           // Initialize
ok = s.add(key)                     // Add element
yes = s.contains(key)               // Check whether it exists
ok = s.remove(key)                  // Delete element
val = s.first()                     // Minimum value (?i64)
val = s.last()                      // Maximum value (?i64)
val = s.lower-bound(target)         // First element ≥ target (?i64)
val = s.upper-bound(target)         // First element > target (?i64)
s.for-each(val)                     // Traverse in ascending order
sz = s.size()
yes = s.empty()
yes = s.full()
```

#### collection/queue — Generic Queue (Ring Buffer)

Ring buffer implementation based on fixed-length array, buffer provided by the `[n]t` receiver:

```no
buf [128]i64 = [0:128]
q = buf.queue-init()
ok = buf.queue-push(q, val)         // Push to tail
val = buf.queue-pop(q)              // Pop from head (?t)
val = buf.queue-peek(q)             // Peek queue head (?t)
sz = q.size()
yes = q.empty()
yes = q.full()
q.clear()
```

#### collection/arr-stack — Generic Stack (Array-based)

Stack implementation based on fixed-length array, buffer provided by the `[n]t` receiver:

```no
buf [128]i64 = [0:128]
s = buf.arr-stack-init()
ok = buf.arr-stack-push(s, val)     // Push
val = buf.arr-stack-pop(s)          // Pop (?t)
val = buf.arr-stack-peek(s)         // Peek top (?t)
sz = s.size()
yes = s.empty()
yes = s.full()
s.clear()
```

#### collection/link — Generic Doubly-linked List

Doubly-linked list based on fixed-length array node pool, values provided by the `[n]t` receiver:

```no
buf [128]i64 = [0:128]
nxt [128]i64 = [0:128]
prv [128]i64 = [0:128]
l = buf.link-init(nxt, prv)
ok = buf.link-push-front(l, val)    // Insert at head
ok = buf.link-push-back(l, val)     // Insert at tail
val = buf.link-pop-front(l)         // Pop head (?t)
val = buf.link-pop-back(l)          // Pop tail (?t)
val = buf.link-peek-front(l)        // Peek head (?t)
val = buf.link-peek-back(l)         // Peek tail (?t)
sz = l.size()
yes = l.empty()
yes = l.full()
```

#### collection/map — Generic Dynamic Hash Map

Dynamic-capacity generic hash maps with automatic rehashing (load factor > 0.75 triggers capacity doubling). Three templates specialized by key type:

```no
// str-key map (V is generic)
m = hashmap-str-tmpl{}
m.init()
m.put('key', val)
result = m.get('key')   // ?V, nil=not found
found = m.contains('key')
m.remove('key')
n = m.size()
yes = m.empty()
m.clear()

// int-key map (K, V both generic)
m2 = hashmap-int-tmpl{}
m2.init()
m2.put(k, v)

// bool-key map (V is generic)
m3 = hashmap-bool-tmpl{}
m3.init()
m3.put(flag, v)
```

#### collection/static-hashmap — Generic Fixed-capacity Hash Map

Fixed-capacity (256 slots) generic hash maps with linear probing. Three templates specialized by key type:

```no
// str-key static map (V is generic)
m = static-hashmap-str-tmpl{}
m.init()
m.put('key', val)
result = m.get('key')   // ?V, nil=not found
found = m.contains('key')
m.remove('key')
n = m.size()

// int-key static map (K, V both generic)
m2 = static-hashmap-int-tmpl{}

// bool-key static map (V is generic, 2 slots)
m3 = static-hashmap-bool-tmpl{}
```

---

### Database

#### database/sql — Database Access Interface

Defines standard interfaces for database connections, queries, and prepared statements, implemented by concrete drivers:

```no
// Execution result
result {
    last-id i64
    affected i64
}

// Connection interface (enter/leave auto-management)
db enter, leave {
    close() (ok bool)
    exec(sql str) (r result)
    query(sql str) (rs rows)
    prepare(sql str) (s stmt)
}

// Result set interface
rows enter, leave {
    next() (ok bool)                    // Iterate to next row
    scan-int(col i64) (v i64)           // Read integer
    scan-str(col i64) (v str)           // Read string
    scan-float(col i64) (v f64)         // Read float
    close() (ok bool)
}

// Prepared statement interface
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

### Encoding

#### encoding/hex — Hexadecimal

```no
// Encoding (defined in byte module)
out = data.to-hex()                  // []byte → uppercase hex str
out = data.to-hex-lower()            // []byte → lowercase hex str

// Decoding (defined in str module)
out = s.from-hex()                   // hex str → ?[]byte (nil=empty, err=invalid character)
```

#### encoding/base64 — Base64 (RFC 4648)

```no
BASE64-STD = 'ABC...+/'
BASE64-URL = 'ABC...-_'
PAD = 61  // '='

out-n = base64.encode(data, n, table, out)    // Base64 encode
out-n = base64.encode-std(data, n, out)       // Standard encoding
out-n = base64.encode-url(data, n, out)       // URL-safe encoding
out-n = base64.decode(s, n, table, out)   // Base64 decode (?i64, nil=invalid input)
```

#### encoding/csv — CSV Parsing (RFC 4180)

```no
fn, new-pos = csv.parse-field(s, sn, pos, field)  // Parse single field
n = csv.parse-line(s, sn, fields, max)             // Parse one line
out-n = csv.encode-field(field, fn, out)           // Encode field
```

#### encoding/pem — PEM Encoding/Decoding (RFC 7468)

PEM format is widely used for X.509 certificates, RSA/ECDSA keys, etc.

```no
// Struct
pem-block {
    label str
    data []byte
}

// Encode
out = pem.pem-encode(label, data)                  // Encode raw bytes to PEM string

// Decode
result = pem.pem-decode(pem-str)                    // Parse PEM string (?pem-block, nil=parse error)
// Access result.label and result.data on success
```

---

### Archive

#### archive/tar — TAR Archive (POSIX ustar)

```no
// Read regular tar
archive = tar{
    data: raw-bytes
}
count = archive.count()
e = archive.entry(idx)
name = archive.name(idx)
sz = archive.size(idx)
typ = archive.type(idx)              // "file" / "dir" / "unknown"
yes = archive.is-dir(idx)
yes = archive.is-file(idx)
out = archive.read(idx)
mode = archive.mode(idx)
ts = archive.mtime(idx)

// Read .tar.gz (auto decompress)
archive = tar.tar-open-gz(gz-data)

// tar-entry methods
name = e.name()
sz = e.size()
typ = e.type()
out = e.read()
yes = e.is-dir()
yes = e.is-file()
mode = e.mode()
ts = e.mtime()

// Write tar
builder = tar-builder{}
builder.add-file(name, content)
builder.add-dir(name)
builder.add-bytes(name, content, typ)
builder.pre-alloc(size)
archive = builder.finish()
```

#### archive/zip — ZIP Archive Parsing

```no
archive = zip{
    data: raw-bytes
}
count = archive.count()                        // Entry count
e = archive.entry(idx)                         // Get zip-entry
name = archive.name(idx)                       // Filename
sz = archive.size(idx)                         // Original size
csz = archive.compressed-size(idx)             // Compressed size
method = archive.method(idx)                   // 0=stored, 8=deflate
out = archive.extract(idx)                     // stored and deflate modes

// zip-entry methods
name = e.name()
sz = e.size()
csz = e.compressed-size()
method = e.method()
out = e.extract()

// Write zip
writer = zip-writer{}
writer.add-file(name, content)
writer.add-dir(name)
out = writer.finalize()
```

#### archive/gzip — GZIP Compression and Raw DEFLATE

```no
out = gzip.gzip-compress(data)                      // zlib compression
out = gzip.gzip-decompress(data)                    // zlib decompression
out = gzip.inflate-decompress(data, out-size)       // Raw DEFLATE decompression (ZIP method 8)
```

#### archive/bzip2 — BZIP2 Decompression (Pure Nolang)

Pure Nolang implementation of BZIP2 decompression (BWT inverse, MTF inverse, Huffman decode, RLE decode):

```no
out = bzip2.bzip2-decompress(data)                   // Decompress .bz2 data
out = bzip2.bzip2-compress(data)                   // Compress to .bz2 data
```

#### archive/xz — XZ/LZMA Decompression (Pure Nolang)

Pure Nolang implementation of LZMA2 decompression, supporting both .xz container and legacy .lzma formats:

```no
out = xz.xz-decompress(data)                        // Decompress .xz format
out = xz.lzma-decompress(data)                      // Decompress legacy .lzma format
out = xz.xz-compress(data)                          // Compress to .xz format
```

#### archive/zlib — zlib Compression/Decompression (RFC 1950, Pure Nolang)

zlib stream format: 2-byte header + raw DEFLATE + 4-byte Adler-32 checksum:

```no
out = zlib.zlib-compress(data)                       // Compress to zlib format (stored blocks)
out = zlib.zlib-decompress(data)                     // Decompress zlib format
out = zlib.zlib-decompress-stored(data)               // Decompress stored blocks
consumed = zlib.zlib-consumed-stored(data)            // Consumed bytes in stored blocks
sum = zlib.adler-32(data, n)                        // Compute Adler-32 checksum
```

#### archive/zstd — Zstandard Decompression (Pure Nolang)

Pure Nolang implementation of Zstandard (zstd) decompression (FSE decode, Huffman decode, LZ77 sequence decode):

```no
out = zstd.zstd-decompress(data)                     // Decompress .zst format
out = zstd.zstd-compress(data)                       // Compress to .zst format
```

---

### Cryptography & Hash

#### hash/aes — AES-128 Encryption/Decryption (ECB mode)

```no
aes.aes-128-enc(plain, 16, key, out)   // Encrypt 16-byte block
aes.aes-128-dec(cipher, 16, key, out)  // Decrypt 16-byte block
```

Also includes standalone modules `hash/aes-128-enc` and `hash/aes-128-dec`.

#### hash/des — DES Encryption/Decryption (ECB mode)

```no
des.des-enc(plain, 8, key, out)        // Encrypt 8-byte block
des.des-dec(cipher, 8, key, out)       // Decrypt 8-byte block
```

Also includes standalone modules `hash/des-enc` and `hash/des-dec`.

#### hash/rsa — RSA Modular Exponentiation

```no
rsa.rsa-modpow(base, bn, exp, en, mod, mn, result, rn)

// Additional functions
out = rsa.rsa-encrypt(plain, n, pub-e, pub-n, mod, mn)    // RSA encrypt
out = rsa.rsa-decrypt(cipher, n, priv-d, dn, mod, mn)    // RSA decrypt
```

Does not include key generation, supports 1024~4096-bit.

#### hash/md5 — MD5 (128-bit)

```no
out [16]byte = md5.md5(data)
```

#### hash/sha1 — SHA-1 (160-bit)

```no
hash = sha1.sha1(data []byte) (hash [20]byte)
hex = sha1.sha1-hex(data []byte) (hex str)
sha1.sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32)
```

`sha1` computes the complete hash (including padding and multi-block processing), returns 20 bytes.
`sha1-hex` same as above but returns a 40-character lowercase hex string.
`sha1-block` is a low-level API that processes a single 512-bit block.

#### hash/sha256 — SHA-256 (256-bit)

```no
sha256.sha256(data []byte) (hash [32]byte)
sha256.sha256-hex(data []byte) (hex str)
sha256.sha256-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32, h5 u32, h6 u32, h7 u32)
```

`sha256` computes the complete hash (including padding and multi-block processing), returns 32 bytes.
`sha256-hex` same as above but returns a 64-character lowercase hex string.
`sha256-block` is a low-level API that processes a single 512-bit block.

#### hash/sha512 — SHA-512 (512-bit)

```no
sha512.sha512(data []byte) (hash [64]byte)
sha512.sha512-hex(data []byte) (hex str)
sha512.sha512-block(s []u64, h0 u64, h1 u64, h2 u64, h3 u64, h4 u64, h5 u64, h6 u64, h7 u64)
```

`sha512` computes the complete hash (including padding and multi-block processing), returns 64 bytes.
`sha512-hex` same as above but returns a 128-character lowercase hex string.
`sha512-block` is a low-level API that processes a single 1024-bit block.

#### hash/crc-32 — CRC32 Checksum

```no
crc-32.crc-32(s []byte, n, crc)
```

#### hash/fnv-1a-32 — FNV-1a Non-cryptographic Hash

```no
fnv-1a-32.fnv-1a-32(s []byte, n, h)
```

#### hash/rand — Random Number Generator (xorshift32)

```no
r = rand.rand(state)                     // 32-bit pseudo-random number
rand.rand-str(state, n, s)              // Random alphanumeric string
```

#### hash/x509 — X.509 Certificate DER Parsing

```no
tag = x509.der-tag(data, pos)
len, adv = x509.der-len(data, pos)
x509.x509-fingerprint(cert, n, h0..h7)  // SHA-256 certificate fingerprint
x509.x509-rsa-e(cert, n, e)             // RSA public key exponent extraction

// Certificate building
cert = x509.x509-build-self-signed-cert(rsa-n, rsa-e, priv-d, subject-cn, serial, not-before, not-after)
tbs = x509.x509-build-tbs(rsa-n, rsa-e, subject-cn, serial, not-before, not-after)
spki = x509.x509-build-spki(rsa-n, rsa-e)
validity = x509.x509-build-validity(not-before, not-after)
n = x509.x509-serial(cert, cert-n)                       // Extract serial number
ts, te = x509.x509-tbs-range(cert, cert-n)               // Extract TBS validity range
ok = x509.rsa-pkcs1-sign-sha256(priv-d, dn, msg, mn, sig, sn) // RSA PKCS#1 sign
ok = x509.sha256-compress(h, w, k)                       // SHA-256 compression

// DER encoding helpers
pos = x509.der-enc-tlv(buf, pos, tag, data, data-n)
pos = x509.der-enc-sequence(buf, pos, data, data-n)
pos = x509.der-enc-set(buf, pos, data, data-n)
pos = x509.der-enc-integer-i64(buf, pos, val)
pos = x509.der-enc-integer-bytes(buf, pos, data, n)
pos = x509.der-enc-null(buf, pos)
pos = x509.der-enc-octetstring(buf, pos, data, n)
pos = x509.der-enc-printablestring(buf, pos, s)
pos = x509.der-enc-utctime(buf, pos, s)
pos = x509.der-enc-bitstring(buf, pos, data, n)
pos = x509.der-enc-explicit(buf, pos, tag, data, n)
pos = x509.der-enc-oid-raw(buf, pos, oid, n)
len, adv = x509.der-skip(data, pos)                     // Skip TLV
```

#### hash/aes-256 — AES-256 Encryption/Decryption (ECB mode)

```no
aes-256.aes-256-enc(in [16]byte, key [32]byte) (out [16]byte)   // Encrypt
aes-256.aes-256-dec(in [16]byte, key [32]byte) (out [16]byte)   // Decrypt
```

#### hash/aes-cbc — AES-CBC Mode (with PKCS7 Padding)

```no
out = aes-cbc.aes-128-cbc-enc(in []byte, key [16]byte, iv [16]byte)
out = aes-cbc.aes-128-cbc-dec(in []byte, key [16]byte, iv [16]byte)
out = aes-cbc.pkcs7-pad(in []byte)
n = aes-cbc.pkcs7-unpad(in []byte)
```

#### hash/aes-256-cbc — AES-256-CBC Encryption/Decryption

```no
out = aes-256-cbc.aes-256-cbc-enc(in []byte, key [32]byte, iv [16]byte)
out = aes-256-cbc.aes-256-cbc-dec(in []byte, key [32]byte, iv [16]byte)
```

#### hash/aes-ctr — AES-CTR Counter Mode

```no
out = aes-ctr.aes-128-ctr(in []byte, key [16]byte, iv [16]byte)
out = aes-ctr.aes-256-ctr(in []byte, key [32]byte, iv [16]byte)
```

#### hash/aes-gcm — AES-GCM AEAD

```no
// AES-128-GCM
sealed = aes-gcm.aes-128-gcm-seal(key [16]byte, iv [12]byte, aad []byte, plain []byte)
plain = aes-gcm.aes-128-gcm-open(key [16]byte, iv [12]byte, aad []byte, sealed []byte)
```

#### hash/aes-256-gcm — AES-256-GCM AEAD (NIST SP 800-38D)

```no
sealed = aes-256-gcm.aes-256-gcm-seal(key [32]byte, iv [12]byte, aad []byte, plain []byte)
plain = aes-256-gcm.aes-256-gcm-open(key [32]byte, iv [12]byte, aad []byte, sealed []byte)
```

#### hash/hmac — HMAC Message Authentication Code

```no
out = hmac.hmac(key []byte, key-n i64, msg []byte, msg-n i64, block-size i64) (out [32]byte)

// Hash-specific HMAC variants
out = hmac.hmac-md5(key, key-n, msg, msg-n)                    // HMAC-MD5
out = hmac.hmac-sha1(key, key-n, msg, msg-n)                   // HMAC-SHA1
out = hmac.hmac-sha256(key, key-n, msg, msg-n)                 // HMAC-SHA256
out = hmac.hmac-sha384(key, key-n, msg, msg-n)                 // HMAC-SHA384
out = hmac.hmac-sha512(key, key-n, msg, msg-n)                 // HMAC-SHA512

// Vec-based variants (return []byte instead of fixed array)
out = hmac.hmac-md5-vec(key, key-n, msg, msg-n)                // HMAC-MD5 ([]byte)
out = hmac.hmac-sha1-vec(key, key-n, msg, msg-n)               // HMAC-SHA1 ([]byte)
out = hmac.hmac-sha256-vec(key, key-n, msg, msg-n)             // HMAC-SHA256 ([]byte)
out = hmac.hmac-sha384-vec(key, key-n, msg, msg-n)             // HMAC-SHA384 ([]byte)
out = hmac.hmac-sha512-vec(key, key-n, msg, msg-n)             // HMAC-SHA512 ([]byte)
```

#### hash/hkdf — HKDF Key Derivation (RFC 5869)

```no
ok = hkdf.hkdf-extract(salt []byte, salt-n i64, ikm []byte, ikm-n i64, prk []byte)
ok = hkdf.hkdf-expand(prk []byte, prk-n i64, info []byte, info-n i64, out []byte, out-n i64)
ok = hkdf.hkdf-expand-hmac(prk, prk-n, info, info-n, out, out-n) // Expand with HMAC
ok = hkdf.hkdf-expand-label(secret, secret-n, label, label-n, context, ctx-n, out, out-n) // TLS 1.3 label
ok = hkdf.hkdf-expand-label-vec(secret, secret-n, label, label-n, context, ctx-n) // Returns []byte
ok = hkdf.hkdf-derive-secret(prk, prk-n, label, label-n, out, out-n) // Derive secret
```

#### hash/pbkdf2 — PBKDF2 Key Derivation (RFC 2898)

```no
pbkdf2.pbkdf2(password []byte, pw-n i64, salt []byte, salt-n i64, iter i64, out []byte, out-n i64)

// Hash-specific variants
pbkdf2.pbkdf2-hmac-sha1(password, pw-n, salt, salt-n, iter, out, out-n)     // PBKDF2-HMAC-SHA1
pbkdf2.pbkdf2-hmac-sha256(password, pw-n, salt, salt-n, iter, out, out-n)   // PBKDF2-HMAC-SHA256
pbkdf2.pbkdf2-hmac-sha512(password, pw-n, salt, salt-n, iter, out, out-n)  // PBKDF2-HMAC-SHA512
```

#### hash/argon2 — Argon2 Memory-hard Key Derivation

```no
argon2.argon2id(password []byte, pw-n i64, salt []byte, salt-n i64, time i64, memory i64, parallel i64, out []byte, out-n i64)
```

#### hash/scrypt — scrypt Key Derivation

```no
scrypt.scrypt(password []byte, pw-n i64, salt []byte, salt-n i64, n i64, r i64, p i64, out []byte, out-n i64)
```

#### hash/sha224 — SHA-224 (224-bit)

```no
hash = sha224.sha224(data []byte) (hash [28]byte)
hex = sha224.sha224-hex(data []byte) (hex str)
```

#### hash/sha384 — SHA-384 (384-bit)

```no
hash = sha384.sha384(data []byte) (hash [48]byte)
hex = sha384.sha384-hex(data []byte) (hex str)
```

#### hash/sha3 — SHA-3 (Keccak)

```no
hash = sha3.sha3-256(data []byte) (hash [32]byte)
hash = sha3.sha3-512(data []byte) (hash [64]byte)
hash = sha3.sha3-224(data []byte) (hash [28]byte)
hash = sha3.sha3-384(data []byte) (hash [48]byte)

// Hex variants
hex = sha3.sha3-224-hex(data)               // SHA3-224 hex string
hex = sha3.sha3-256-hex(data)               // SHA3-256 hex string
hex = sha3.sha3-384-hex(data)               // SHA3-384 hex string
hex = sha3.sha3-512-hex(data)               // SHA3-512 hex string
```

#### hash/blake2 — BLAKE2 Hash

```no
hash = blake2.blake2b-256(data []byte) (hash [32]byte)
hash = blake2.blake2b-512(data []byte) (hash [64]byte)
hash = blake2.blake2b(data []byte) (hash [64]byte)        // BLAKE2b full hash
hex = blake2.blake2b-hex(data []byte) (hex str)          // BLAKE2b hex string
hash = blake2.blake2s(data []byte) (hash [32]byte)        // BLAKE2s hash
hex = blake2.blake2s-hex(data []byte) (hex str)           // BLAKE2s hex string
```

#### hash/crc-16 — CRC16 Checksum

```no
crc = crc-16.crc-16(data []byte, n i64) (crc i64)
hex = crc-16.crc-16-hex(data []byte, n i64) (hex str)    // CRC16 hex string
```

#### hash/crc-64 — CRC64 Checksum

```no
crc = crc-64.crc-64(data []byte, n i64) (crc i64)
hex = crc-64.crc-64-hex(data []byte, n i64) (hex str)    // CRC64 hex string
```

#### hash/fnv — FNV-1 Hash

```no
h = fnv.fnv-1-32(data []byte, n i64) (h i64)
h = fnv.fnv-1a-64(data []byte, n i64) (h i64)
h = fnv.fnv-1a-32(data []byte, n i64) (h i64)             // FNV-1a 32-bit
```

#### hash/base32 — Base32 Encoding/Decoding (RFC 4648)

```no
out = base32.base32-encode(data []byte, n i64) (out str)
out = base32.base32-decode(s str, n i64) (out []byte)
```

#### hash/chacha20-poly1305 — ChaCha20-Poly1305 AEAD

```no
sealed = chacha20-poly1305.chacha20-poly1305-seal(key [32]byte, nonce [12]byte, aad []byte, plain []byte)
plain = chacha20-poly1305.chacha20-poly1305-open(key [32]byte, nonce [12]byte, aad []byte, sealed []byte)
```

#### hash/rc4 — RC4 Stream Cipher

```no
out = rc4.rc4(key []byte, key-n i64, data []byte, data-n i64) (out []byte)
```

#### hash/tdes — Triple DES (3DES)

```no
tdes.tdes-enc(plain, 8, key [24]byte, out)
tdes.tdes-dec(cipher, 8, key [24]byte, out)
out = tdes.tdes-cbc-enc(in []byte, key [24]byte, iv [8]byte)  // 3DES-CBC encrypt
out = tdes.tdes-cbc-dec(in []byte, key [24]byte, iv [8]byte)  // 3DES-CBC decrypt
```

#### hash/ecdsa — ECDSA Digital Signature

```no
ok = ecdsa.ecdsa-sign(priv-key []byte, msg []byte, msg-n i64, r []byte, s []byte)
ok = ecdsa.ecdsa-verify(pub-key []byte, msg []byte, msg-n i64, r []byte, s []byte) (ok bool)
pub = ecdsa.ecdsa-publickey(priv-key []byte) (pub []byte)    // Derive public key from private
```

#### hash/ed25519 — Ed25519 Digital Signature

```no
pub = ed25519.ed25519-derive-public(priv [32]byte) (pub [32]byte)
pub = ed25519.ed25519-publickey(priv [32]byte) (pub [32]byte) // Derive public key
sig = ed25519.ed25519-sign(priv [32]byte, msg []byte, msg-n i64) (sig [64]byte)
ok = ed25519.ed25519-verify(pub [32]byte, msg []byte, msg-n i64, sig [64]byte) (ok bool)
```

#### hash/x25519 — X25519 Key Exchange

```no
pub = x25519.x25519-derive-public(priv [32]byte) (pub [32]byte)
shared = x25519.x25519-derive-shared(priv [32]byte, peer-pub [32]byte) (shared [32]byte)

// Additional functions
pub, priv = x25519.x25519-gen-keypair()                        // Generate keypair
bp = x25519.x25519-get-basepoint()                             // Get basepoint
pub = x25519.x25519-scalarmult-base(priv [32]byte) (pub [32]byte)      // Scalar mult base
shared = x25519.x25519-scalarmult(priv [32]byte, peer [32]byte) (shared [32]byte) // Scalar mult
pub = x25519.x25519-scalarmult-base-bytes(priv []byte) (pub []byte)    // Scalar mult base (bytes)
shared = x25519.x25519-scalarmult-bytes(priv []byte, peer []byte) (shared []byte) // Scalar mult (bytes)
```

#### hash/rand-str — Random String Generation

```no
rand-str.rand-str(state i64, n i64, s str)   // Generate random alphanumeric string of length n
```

---

### Data Exchange

#### json — JSON Parsing and Generation

```no
// Type enum
json-kind {
    null,
    bool,
    num,
    str,
    arr,
    obj,
}

// Parsing
v = json.parse(s, n)          // Full parse
v = json.parse-str(s, n)                 // Parse string value
v = json.parse-num(s, n)                 // Parse numeric value

// Generation
n = json.stringify(v, out)    // Serialize

// Access
val = json.get-key(v, key)    // Get object property
json.set-key(v json-value, key, val)    // Set object property
val = json.get(v, key)                  // Get by key (generic)
result = json.get-bool(v, key)         // Get bool value
result = json.get-num(v, key)           // Get numeric value
result = json.get-str(v, key)           // Get string value
result = json.get-i64(v, key)           // Get i64 value

// Type checks
yes = json.is-null(v)                  // Is null?
yes = json.is-bool(v)                  // Is bool?
yes = json.is-num(v)                   // Is number?
yes = json.is-str(v)                   // Is string?
yes = json.is-arr(v)                   // Is array?
yes = json.is-obj(v)                   // Is object?

// Setters
json.set(v, key, val)                  // Set by key (generic)
json.set-bool(v, key, val)             // Set bool
json.set-num(v, key, val)              // Set number
json.set-str(v, key, val)              // Set string
json.set-i64(v, key, val)              // Set i64
json.set-null(v, key)                  // Set null

// Array operations
val = json.arr-get(v, idx)             // Get array element by index
n = json.arr-len(v)                    // Get array length
json.arr-push(v, val)                  // Push to array
json.arr-push-bool(v, val)             // Push bool
json.arr-push-num(v, val)              // Push number
json.arr-push-str(v, val)              // Push string

// Direct value access
b = json.bool(v)                       // Get bool value
n = json.num(v)                        // Get numeric value
s = json.str(v)                        // Get string value
n = json.i64(v)                        // Get i64 value

// Pool-based JSON (for memory efficiency)
pool = json.new-pool()                 // Create json-pool
ok = pool.parse(s, n)                  // Parse into pool
s = pool.stringify()                   // Serialize from pool
val = pool.get-key(key)                // Get by key
```

---

### Others

#### unicode — Unicode Support

Unicode-related features have been distributed across the `char` and `str` modules:

- Character classification (`is-letter`, `is-digit`, `is-upper`, etc.) -> see `char` module
- UTF-8 encoding/decoding (`char.to-bytes`, `char.to-str`) -> see `char` module
- String rune counting (`str.count`) -> see `str` module

#### uuid — UUID v4 Generation and Parsing

```no
out = uuid.new-v4(state)                  // Generate UUID v4
out-n = uuid.to-str(out)             // Convert to lowercase string (method)
out-n = uuid.to-str-upper(out)       // Convert to uppercase string (method)
ok = uuid.from-str(s, sn, out)            // Parse from string (supports with/without hyphens)
ok = uuid.parse-with-dashes(s, pos, out)  // Parse with hyphens
ok = uuid.parse-no-dashes(s, pos, out)    // Parse without hyphens
ok = uuid.validate()                 // Validate UUID format (method)
v = uuid.version()                   // Get version (method)
v = uuid.variant()                   // Get variant (method)
yes = uuid.is-nil()                  // Whether it is nil (method)
yes = uuid.eq(b)                     // Equality comparison (method)
r = uuid.cmp(b)                      // Compare (method)
uuid.nil-uuid(out)                        // Return nil UUID
```

#### bigint — Arbitrary Precision Integer

```no
// Type
bigint {
    sign i64
    limbs []i64
    len i64
}

// Construction
out = bigint.from-i64(v)
out = bigint.from-u64(v)
out = bigint.zero()
out = bigint.one()
out = bigint.copy(a)

// Comparison
r = bigint.cmp(a, b)
r = bigint.abs-cmp(a, b)              // Absolute value comparison
r = bigint.eq(a, b)
r = bigint.is-zero(a)
r = bigint.is-neg(a)
r = bigint.is-pos(a)

// Arithmetic
c = bigint.add(a, b)
c = bigint.sub(a, b)
c = bigint.abs-add(a, b)              // Absolute addition
c = bigint.abs-sub(a, b)              // Absolute subtraction
c = bigint.mul(a, b)
q, r = bigint.div-mod(a, b)
r = bigint.mod(a, b)
q = bigint.div-i64(a, v)
r = bigint.mod-i64(a, v)
c = bigint.pow(a, n)
r = bigint.mod-pow(base, exp, mod, r)

// Number theory
bigint.gcd(a, b, g)
bigint.lcm(a, b, l)

// Shifting
bigint.shl(a, n, c)
bigint.shr(a, n, c)

// String conversion
n = bigint.to-str(a, out)
out = bigint.from-str(s, sn)
n = bigint.to-hex(a, out)
out = bigint.from-hex(s, sn)

// Small integer helpers
bigint.add-i64(a, v, c)
bigint.mul-i64(a, v, c)
```

### err — Error Handling

Structured error type and utility functions:

```no
// Error code enum
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

// Struct
error {
code code
msg str
}

// Functions
e = err.new(code.io, msg)            // Create error
e = err.err-from-errno(errno)         // Create from C errno
c = err.errno-to-code(errno)          // Convert C errno to error code
yes = e.is(code.io)                  // Check error code (method)
msg = e.msg()                       // Get error message (method)
c = e.code()                        // Get error code (method)
s = e.format()              // Format as string (method)
```

### bool — Boolean Type

```no
bool.to-str() (out str)     // true->"true", false->"false" (method)
```

### enter / leave — Lifecycle Hooks

```no

// Execute on startup
enter { 
    enter()
}     

// Execute on exit
leave {
    leave()
}     
```

### async — Async Coroutine and Cancellation Primitives

Nolang provides a cooperative, single-threaded, stackless async coroutine model:

```no
// Start an -async function as a background task, returns opaque task handle
h = run worker-async(args)

// Await a background task, returns its result
r = awy h

// Cancel a background task (cooperative)
async.async-cancel(h)                    // Set cancellation flag on task h

// Cooperative self-cancellation check (call inside async functions)
yes = async.async-cancelled()            // Returns true if current task has been cancelled
```

> **Note:** Cancellation is cooperative, not preemptive. Long blocking calls (e.g. network requests) cannot be force-interrupted. The task stops at the next cooperative checkpoint (`async-cancelled()` call or next event loop scheduling).

### global — Global Built-in Functions

Functions callable without module prefix. Only these 6 global functions exist; all other cross-module calls require module prefix (e.g. `fs.read`, `os.exit`).

```no
// Capacity/length constructors (type inferred from assignment target)
s str = with-cap(256)                   // Pre-allocate 256-byte str (len=0)
v []i64 = with-cap(100)                 // Pre-allocate 100-element slice (len=0)
s str = with-len(10)                    // str with length 10
v []i64 = with-len(100)                 // slice with length 100
v []i64 = with-cap-len(200, 100)        // capacity 200, length 100

// Also available as methods on str/vec:
s = ''.with-cap(256)
v = [].with-cap(100)
v = [].with-len-cap(100, 200)           // length 100, capacity 200

// Output/formatting
print('x={x}')                          // Named format, stdout + newline
eprint('err {x}')                       // Named format, stderr + newline
s = format('x={x}')                      // Returns formatted string
```

### magic — File Type Detection

Simplified file type detection based on extension and magic bytes, no libmagic dependency:

```no
kind = magic.detect-type(path)                  // Detect file type
// Returns type description string, e.g. 'PNG image', 'ELF executable', 'ASCII text', 'directory', 'unknown', 'data'

// Additional detection methods
kind = magic.detect-by-extension(path)          // Detect by file extension
kind = magic.detect-by-magic(data)             // Detect by magic bytes
ext = magic.get-extension(path)                // Extract file extension
```

---

## Module List

| Module              | Description      |
| ------------------- | ---------------- |
| fmt                 | Formatted output |
| math                | Math functions   |
| str                 | String operations|
| vec                 | Slice ([]t) ops  |
| arr                 | Array ([n]t) ops |
| number              | Numeric utilities|
| byte                | Byte operations  |
| char                | Character ops (methods) |
| os                  | OS interface     |
| env                 | Environment variables wrapper |
| fs                  | File system tools|
| io                  | I/O abstraction  |
| args                | Command-line arguments |
| path                | Path handling (struct) |
| bufio               | Buffered reading |
| time                | Time operations  |
| log                 | Leveled logging  |
| json                | JSON parse/generate |
| types               | Type definitions |
| option              | Option type      |
| sort                | Sort constants   |
| set                 | Set              |
| deque               | Double-ended queue (struct) |
| heap                | Min heap (struct)|
| stack               | Stack (struct)   |
| regexp              | Regular expression |
| process             | Process operations |
| unicode             | Unicode notes    |
| uuid                | UUID v4          |
| bigint              | Arbitrary precision integer |
| bool                | Boolean type     |
| err                 | Error handling   |
| enter               | Startup hook     |
| leave               | Exit hook        |
| net                 | TCP network ops  |
| http                | HTTP/1.1 client  |
| http2               | HTTP/2.0 client  |
| http3               | HTTP/3.0 client  |
| ws                  | WebSocket        |
| quic                | QUIC protocol    |
| tls                 | TLS 1.2/1.3      |
| sse                 | SSE client       |
| client              | High-level TCP client |
| server              | HTTP server      |
| dns                 | DNS resolution   |
| url                 | URL parsing      |
| cookie              | HTTP Cookie      |
| multipart           | Multipart form   |
| hpack               | HPACK header compression |
| proxy               | Proxy support    |
| pool                | Connection pool  |
| unix                | Unix domain socket |
| ip                  | IP address operations |
| hex                 | Hex encode/decode |
| base64              | Base64 encode/decode |
| csv                 | CSV parsing      |
| pem                 | PEM encoding/decoding |
| tar                 | TAR archive      |
| zip                 | ZIP archive      |
| gzip                | GZIP compression |
| bzip2               | BZIP2 decompression |
| xz                  | XZ/LZMA decompression |
| zlib                | zlib compression (RFC 1950) |
| zstd                | Zstandard decompression |
| linked-hash-map     | Ordered hash map |
| hash-set            | i64 hash set     |
| str-map             | str->str hash map |
| str-set             | str hash set     |
| tree-map            | AVL ordered map  |
| tree-set            | AVL ordered set  |
| queue               | Generic queue    |
| arr-stack           | Generic stack    |
| link                | Generic doubly-linked list |
| hashmap             | Generic dynamic hash map |
| static-hashmap      | Generic fixed-capacity hash map |
| sql                 | Database access interface |
| aes                 | AES-128 enc/dec  |
| aes-128-enc         | AES-128 encrypt  |
| aes-128-dec         | AES-128 decrypt  |
| aes-256             | AES-256 enc/dec  |
| aes-cbc             | AES-CBC mode     |
| aes-256-cbc         | AES-256-CBC      |
| aes-ctr             | AES-CTR mode     |
| aes-gcm             | AES-GCM AEAD     |
| aes-256-gcm         | AES-256-GCM      |
| des                 | DES enc/dec      |
| des-enc             | DES encrypt      |
| des-dec             | DES decrypt      |
| tdes                | Triple DES       |
| rsa                 | RSA modpow       |
| md5                 | MD5 hash         |
| sha1                | SHA-1 hash       |
| sha224              | SHA-224 hash     |
| sha256              | SHA-256 hash     |
| sha384              | SHA-384 hash     |
| sha512              | SHA-512 hash     |
| sha3                | SHA-3 hash       |
| blake2              | BLAKE2 hash      |
| crc-16              | CRC16 checksum   |
| crc-32              | CRC32 checksum   |
| crc-64              | CRC64 checksum   |
| fnv                 | FNV-1 hash       |
| fnv-1a-32           | FNV-1a hash      |
| hmac                | HMAC auth code   |
| hkdf                | HKDF key derivation |
| pbkdf2              | PBKDF2 key derivation |
| argon2              | Argon2 key derivation |
| scrypt              | scrypt key derivation |
| chacha20-poly1305   | ChaCha20-Poly1305 |
| rc4                 | RC4 stream cipher |
| ecdsa               | ECDSA signature  |
| ed25519             | Ed25519 signature |
| x25519              | X25519 key exchange |
| base32              | Base32 encode/decode |
| rand                | Random number generator |
| rand-str            | Random string generation |
| x509                | X.509 DER parsing |
| async               | Async coroutine/cancellation |
| global              | Global built-in functions |
| magic               | File type detection |

## See Also — Nolang References

- [nolang-syntax](file://../nolang-syntax/SKILL.md) — Nolang syntax, grammar, types, operators, and language features
- [nolang-build](file://../nolang-build/SKILL.md) — Building the Nolang project with `make`
- [nolang-debug](file://../nolang-debug/SKILL.md) — Debugging guide for compiler and LSP issues
- [nolang-memory](file://../nolang-memory/SKILL.md) — Memory design and ownership model
- `src/std/` — standard library source files