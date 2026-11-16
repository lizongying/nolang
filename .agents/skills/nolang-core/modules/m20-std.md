# Std — 标准库索引

`src/std/` 是随编译器内嵌的 std 源码（`no build` / `no test` 读的是**内嵌 blob**，不是磁盘）。
核心模块无需导入即可用；其余 `# std/<module>`。

⚠️ `# std/fmt` 不需要显式导入，`no fmt` 会自动移除它——`print` / `eprint` / `format` / `sprintf`
是编译器内建。只有刻意写别名或函数导入（`# std/fmt.sprintf fm`）才保留。

## 视图索引（按模块选档）

| 视图 | 含模块 |
|---|---|
| [`10-base.md`](std/10-base.md) | `types` 类型定义与 LLVM 映射、`option` |
| [`20-core.md`](std/20-core.md) | `fmt` `math` `char` `str` `number` / `num` / `int` / `float` `byte` `txt` `vec` `arr` `sort` |
| [`30-os-io.md`](std/30-os-io.md) | `os` `fs` `env` `args` `path` `bufio` `io` `regexp` `process` |
| [`40-net.md`](std/40-net.md) | `net` 与 `net/*`：`http` `http2` `http3` `tls` `ws` `quic` `server` `client` `dns` `url` `cookie` `multipart` `hpack` `proxy` `pool` `unix` `sse` `ip` |
| [`50-time-log.md`](std/50-time-log.md) | `time` `log` |
| [`60-data-structs.md`](std/60-data-structs.md) | `set` `deque` `heap` `map/*`（哈希/树/字符串键）`collection/*`（队列/栈/链表/静态表） |
| [`70-db-encode.md`](std/70-db-encode.md) | `database/sql` `encoding/*`（hex/base64/csv/pem）`archive/*`（tar/zip/gzip/bzip2/xz/zlib/zstd） |
| [`80-crypto.md`](std/80-crypto.md) | `crypto/*`：aes/des/tdes/rc4/chacha20、rsa/ecdsa/ed25519/x25519/x509、sha*/md5/blake2/sha3、hmac/hkdf/pbkdf2/argon2/scrypt、crc/fnv、rand、base32 |
| [`85-exchange.md`](std/85-exchange.md) | `json` `toml` `yaml` `dom` |
| [`90-misc.md`](std/90-misc.md) | `unicode` `uuid` `bigint`，以及 `err` / `bool` / `enter`·`leave` / `async` / `global` / `magic` 等 `###` 级小节 |

## 加载判据

- 只知道模块名、不知道归哪档：先看下面那张 Module List（原名索引），再回本表定位视图。
- 只想知道**函数签名**：加载对应视图即可，不需要 `m10-syntax`。
- 要判断**语言语义**（类型规则、运算符重载、注解、布局）：回 [`m10-syntax.md`](m10-syntax.md)，std 只描述 API 面。
- 改 std 源码（`src/std/**`，注释也算）⇒ 必须 `make no`，见 [`m30-build.md`](m30-build.md) 与 [`m40-verify.md`](m40-verify.md)。

## Module List（原名索引）


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
| toml                | TOML parse/generate |
| yaml                | YAML 1.2 parse/generate |
| dom                 | XML/HTML DOM parse, query, mutate, serialize |
| types               | Type definitions |
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
| aes                 | AES block core (128/256) |
| aes-cbc             | AES-CBC mode (128/256)   |
| aes-ctr             | AES-CTR mode (128/256)   |
| aes-gcm             | AES-GCM AEAD (128/256)   |
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

