---
sidebar_position: 4.3
---

## Resumen de módulos

| Módulo              | Ruta   | Descripción                                  |
| ------------------- | ------ | -------------------------------------------- |
| fmt                 | Núcleo | Salida formateada                            |
| math                | Núcleo | Funciones matemáticas                        |
| str                 | Núcleo | Operaciones con cadenas                      |
| vec                 | Núcleo | Operaciones con segmentos ([]t)              |
| arr                 | Núcleo | Operaciones con arrays ([n]t)                |
| number              | Núcleo | Funciones de utilidad numérica               |
| byte                | Núcleo | Operaciones con bytes                        |
| char                | Núcleo | Operaciones con caracteres (métodos)         |
| os                  | Núcleo | Interfaz del sistema operativo               |
| env                 | Núcleo | Envoltorio de variables de entorno           |
| fs                  | Núcleo | Utilidades del sistema de archivos           |
| io                  | Núcleo | Abstracción de entrada/salida                |
| args                | Núcleo | Argumentos de línea de comandos              |
| path                | Núcleo | Manejo de rutas (estructura)                 |
| bufio               | Núcleo | Lectura con búfer                            |
| time                | Núcleo | Operaciones con el tiempo                    |
| log                 | Núcleo | Registro por niveles                         |
| json                | Núcleo | Análisis/generación de JSON                  |
| toml                | Núcleo | Análisis/generación de TOML 1.0              |
| yaml                | Núcleo | Análisis/generación de YAML 1.2              |
| types               | Núcleo | Documento de definiciones de tipos           |
| sort                | Núcleo | Constantes de ordenación                     |
| set                 | Núcleo | Conjunto                                     |
| deque               | Núcleo | Cola de doble extremo (estructura)           |
| heap                | Núcleo | Montón mínimo (estructura)                   |
| stack               | Núcleo | Pila (estructura)                            |
| regexp              | Núcleo | Expresiones regulares                        |
| process             | Núcleo | Operaciones con procesos                     |
| uuid                | Núcleo | UUID v4                                      |
| bigint              | Núcleo | Entero de precisión arbitraria               |
| bool                | Núcleo | Tipo booleano                                |
| err                 | Núcleo | Manejo de errores                            |
| enter               | Núcleo | Gancho de inicio                             |
| leave               | Núcleo | Gancho de salida                             |
| async               | Núcleo | Corrutinas/cancelación asíncronas            |
| global              | Núcleo | Funciones integradas globales                |
| magic               | Núcleo | Detección de tipos de archivo                |
| net                 | Núcleo | Operaciones de red TCP                       |
| net/http            | Submódulo | Cliente HTTP/1.1                          |
| net/http2           | Submódulo | Cliente HTTP/2.0                          |
| net/http3           | Submódulo | Cliente HTTP/3.0                          |
| net/ws              | Submódulo | WebSocket                                |
| net/quic            | Submódulo | Protocolo QUIC                            |
| net/tls             | Submódulo | TLS 1.2/1.3                              |
| net/sse             | Submódulo | Cliente SSE                              |
| net/client          | Submódulo | Cliente TCP de alto nivel                |
| net/server          | Submódulo | Servidor HTTP                            |
| net/dns             | Submódulo | Resolución DNS                           |
| net/url             | Submódulo | Análisis de URL                          |
| net/cookie          | Submódulo | Cookie HTTP                              |
| net/multipart       | Submódulo | Formulario Multipart                     |
| net/hpack           | Submódulo | Compresión de cabeceras HPACK            |
| net/proxy           | Submódulo | Compatibilidad con proxy                 |
| net/pool            | Submódulo | Pool de conexiones                       |
| net/unix            | Submódulo | Sockets de dominio Unix                  |
| net/ip              | Submódulo | Operaciones con direcciones IP           |
| encoding/base64     | Submódulo | Codificación/decodificación Base64       |
| encoding/csv        | Submódulo | Análisis de CSV                          |
| encoding/pem        | Submódulo | Codificación/decodificación PEM          |
| archive/tar         | Submódulo | Archivo TAR                              |
| archive/zip         | Submódulo | Archivo ZIP                              |
| archive/gzip        | Submódulo | Compresión GZIP                          |
| archive/bzip2       | Submódulo | Descompresión BZIP2                      |
| archive/xz          | Submódulo | Descompresión XZ/LZMA                    |
| archive/zlib        | Submódulo | Compresión zlib (RFC 1950)               |
| archive/zstd        | Submódulo | Descompresión Zstandard                  |
| map/linked-hash-map | Submódulo | Tabla hash ordenada                      |
| map/hash-set        | Submódulo | Conjunto hash de i64                     |
| map/str-map         | Submódulo | Mapa hash str→str                        |
| map/str-set         | Submódulo | Conjunto hash de str                     |
| map/tree-map        | Submódulo | Mapa ordenado AVL                        |
| map/tree-set        | Submódulo | Conjunto ordenado AVL                    |
| collection/queue    | Submódulo | Cola genérica                            |
| collection/arr-stack| Submódulo | Pila genérica                            |
| collection/link     | Submódulo | Lista doblemente enlazada genérica       |
| collection/map      | Submódulo | Tabla hash dinámica genérica             |
| collection/static-hashmap | Submódulo | Tabla hash genérica de capacidad fija |
| database/sql        | Submódulo | Interfaz de acceso a bases de datos      |
| crypto/aes          | Submódulo | Núcleo de bloques AES (128/256)          |
| crypto/aes-cbc      | Submódulo | Modo AES-CBC (128/256)                  |
| crypto/aes-ctr      | Submódulo | Modo AES-CTR (128/256)                  |
| crypto/aes-gcm      | Submódulo | AES-GCM AEAD (128/256)                  |
| crypto/des            | Submódulo | Cifrado/descifrado DES                |
| crypto/tdes           | Submódulo | Triple DES                            |
| crypto/rsa            | Submódulo | Potenciación modular RSA              |
| crypto/md5            | Submódulo | Hash MD5                              |
| crypto/sha1           | Submódulo | Hash SHA-1                            |
| crypto/sha224         | Submódulo | Hash SHA-224                          |
| crypto/sha256         | Submódulo | Hash SHA-256                          |
| crypto/sha384         | Submódulo | Hash SHA-384                          |
| crypto/sha512         | Submódulo | Hash SHA-512                          |
| crypto/sha3           | Submódulo | Hash SHA-3                            |
| crypto/blake2         | Submódulo | Hash BLAKE2                           |
| crypto/crc-16         | Submódulo | Suma de verificación CRC16            |
| crypto/crc-32         | Submódulo | Suma de verificación CRC32            |
| crypto/crc-64         | Submódulo | Suma de verificación CRC64            |
| crypto/fnv            | Submódulo | Hash FNV-1                            |
| crypto/fnv-1a-32      | Submódulo | Hash FNV-1a                           |
| crypto/hmac           | Submódulo | Código de autenticación HMAC          |
| crypto/hkdf           | Submódulo | Derivación de claves HKDF             |
| crypto/pbkdf2         | Submódulo | Derivación de claves PBKDF2           |
| crypto/argon2         | Submódulo | Derivación de claves Argon2           |
| crypto/scrypt         | Submódulo | Derivación de claves scrypt           |
| crypto/chacha20-poly1305 | Submódulo | ChaCha20-Poly1305                 |
| crypto/rc4            | Submódulo | Cifrado de flujo RC4                 |
| crypto/ecdsa          | Submódulo | Firma ECDSA                          |
| crypto/ed25519        | Submódulo | Firma Ed25519                        |
| crypto/x25519         | Submódulo | Intercambio de claves X25519         |
| crypto/base32         | Submódulo | Codificación/decodificación Base32   |
| crypto/rand           | Submódulo | Generador de números aleatorios      |
| crypto/x509           | Submódulo | Análisis X.509 DER                   |
