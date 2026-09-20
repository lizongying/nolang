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
