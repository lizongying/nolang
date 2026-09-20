---
sidebar_position: 4.5
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
| toml                | Core   | TOML 1.0 parsing/generation                 |
| yaml                | Core   | YAML 1.2 parsing/generation                 |
| types               | Core   | Type definitions document                    |
| option              | Core   | Option type                                  |
| sort                | Core   | Sort constants                               |
| set                 | Core   | Set                                          |
| deque               | Core   | Double-ended queue (struct)                  |
| heap                | Core   | Min heap (struct)                            |
| stack               | Core   | Stack (struct)                               |
| regexp              | Core   | Regular expressions                          |
| process             | Core   | Process operations                           |
| async              | Core   | Async coroutines/cancellation                |
| global             | Core   | Global built-in functions                    |
| magic              | Core   | File type detection                          |
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
| encoding/pem        | Submodule | PEM encoding/decoding (RFC 7468)         |
| archive/tar         | Submodule | TAR archive                             |
| archive/zip         | Submodule | ZIP archive                             |
| archive/gzip        | Submodule | GZIP compression                        |
| archive/bzip2       | Submodule | BZIP2 decompression                     |
| archive/xz          | Submodule | XZ/LZMA decompression                   |
| archive/zlib        | Submodule | zlib compression (RFC 1950)              |
| archive/zstd        | Submodule | Zstandard decompression                 |
| map/linked-hash-map | Submodule | Ordered hash map                        |
| map/hash-set        | Submodule | i64 hash set                            |
| map/str-map         | Submodule | str→str hash map                        |
| map/str-set         | Submodule | str hash set                            |
| map/tree-map        | Submodule | AVL ordered map                         |
| map/tree-set        | Submodule | AVL ordered set                         |
| collection/queue    | Submodule | Generic queue                           |
| collection/arr-stack| Submodule | Generic stack                           |
| collection/link     | Submodule | Generic doubly linked list              |
| collection/map      | Submodule | Generic dynamic hash map                |
| collection/static-hashmap | Submodule | Generic fixed-capacity hash map   |
| database/sql        | Submodule | Database access interface               |
| crypto/aes          | Submodule | AES block core (128/256)                |
| crypto/aes-cbc      | Submodule | AES-CBC mode (128/256)                 |
| crypto/aes-ctr      | Submodule | AES-CTR mode (128/256)                 |
| crypto/aes-gcm      | Submodule | AES-GCM AEAD (128/256)                 |
| crypto/des            | Submodule | DES encryption/decryption               |
| crypto/tdes           | Submodule | Triple DES                              |
| crypto/rsa            | Submodule | RSA modular exponentiation              |
| crypto/md5            | Submodule | MD5 hash                                |
| crypto/sha1           | Submodule | SHA-1 hash                              |
| crypto/sha224         | Submodule | SHA-224 hash                            |
| crypto/sha256         | Submodule | SHA-256 hash                            |
| crypto/sha384         | Submodule | SHA-384 hash                            |
| crypto/sha512         | Submodule | SHA-512 hash                            |
| crypto/sha3           | Submodule | SHA-3 hash                              |
| crypto/blake2         | Submodule | BLAKE2 hash                             |
| crypto/crc-16         | Submodule | CRC16 checksum                          |
| crypto/crc-32         | Submodule | CRC32 checksum                          |
| crypto/crc-64         | Submodule | CRC64 checksum                          |
| crypto/fnv            | Submodule | FNV-1 hash                              |
| crypto/fnv-1a-32      | Submodule | FNV-1a hash                             |
| crypto/hmac           | Submodule | HMAC authentication code                |
| crypto/hkdf           | Submodule | HKDF key derivation                     |
| crypto/pbkdf2         | Submodule | PBKDF2 key derivation                   |
| crypto/argon2         | Submodule | Argon2 key derivation                   |
| crypto/scrypt         | Submodule | scrypt key derivation                   |
| crypto/chacha20-poly1305 | Submodule | ChaCha20-Poly1305                    |
| crypto/rc4            | Submodule | RC4 stream cipher                       |
| crypto/ecdsa          | Submodule | ECDSA signature                         |
| crypto/ed25519        | Submodule | Ed25519 signature                       |
| crypto/x25519         | Submodule | X25519 key exchange                     |
| crypto/base32         | Submodule | Base32 encoding/decoding                |
| crypto/rand           | Submodule | Random number generator                 |
| crypto/x509           | Submodule | X.509 DER parsing                       |
