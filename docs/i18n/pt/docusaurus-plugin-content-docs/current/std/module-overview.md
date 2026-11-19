---
sidebar_position: 4.3
---

## Visão geral dos módulos

| Módulo                | Caminho   | Descrição             |
| --------------------- | --------- | --------------------- |
| fmt                 | Núcleo   | Saída formatada       |
| math                | Núcleo   | Funções matemáticas   |
| str                 | Núcleo   | Operações com strings |
| vec                 | Núcleo   | Operações com slices ([]t)  |
| arr                 | Núcleo   | Operações com arrays ([n]t) |
| number              | Núcleo   | Funções utilitárias numéricas |
| byte                | Núcleo   | Operações com bytes   |
| char                | Núcleo   | Operações com caracteres (métodos) |
| os                  | Núcleo   | Interface do sistema operativo |
| env                 | Núcleo   | Encapsulação de variáveis de ambiente |
| fs                  | Núcleo   | Utilitários de sistema de ficheiros |
| io                  | Núcleo   | Abstração de entrada/saída |
| args                | Núcleo   | Argumentos da linha de comando |
| path                | Núcleo   | Processamento de caminhos (struct) |
| bufio               | Núcleo   | Leitura com buffer     |
| time                | Núcleo   | Operações de tempo     |
| log                 | Núcleo   | Registo com níveis     |
| json                | Núcleo   | Análise/geração de JSON |
| toml                | Núcleo   | Análise/geração de TOML 1.0 |
| yaml                | Núcleo   | Análise/geração de YAML 1.2 |
| types               | Núcleo   | Documento de definições de tipos |
| sort                | Núcleo   | Constantes de ordenação |
| set                 | Núcleo   | Conjunto               |
| deque               | Núcleo   | Fila de duas extremidades (struct) |
| heap                | Núcleo   | Heap mínimo (struct)   |
| stack               | Núcleo   | Pilha (struct)         |
| regexp              | Núcleo   | Expressões regulares   |
| process             | Núcleo   | Operações com processos |
| uuid                | Núcleo   | UUID v4          |
| bigint              | Núcleo   | Inteiro de precisão arbitrária |
| bool                | Núcleo   | Tipo booleano          |
| err                 | Núcleo   | Tratamento de erros    |
| enter               | Núcleo   | Hook de arranque       |
| leave               | Núcleo   | Hook de saída          |
| async               | Núcleo   | Corrotinas/cancelamento assíncronos |
| global              | Núcleo   | Funções incorporadas globais |
| magic               | Núcleo   | Deteção de tipo de ficheiro |
| net                 | Núcleo   | Operações de rede TCP  |
| net/http            | Submódulo | Cliente HTTP/1.1      |
| net/http2           | Submódulo | Cliente HTTP/2.0      |
| net/http3           | Submódulo | Cliente HTTP/3.0      |
| net/ws              | Submódulo | WebSocket             |
| net/quic            | Submódulo | Protocolo QUIC        |
| net/tls             | Submódulo | TLS 1.2/1.3           |
| net/sse             | Submódulo | Cliente SSE           |
| net/client          | Submódulo | Cliente TCP de alto nível |
| net/server          | Submódulo | Servidor HTTP         |
| net/dns             | Submódulo | Resolução DNS         |
| net/url             | Submódulo | Análise de URL        |
| net/cookie          | Submódulo | Cookie HTTP           |
| net/multipart       | Submódulo | Formulário Multipart  |
| net/hpack           | Submódulo | Compressão de cabeçalhos HPACK |
| net/proxy           | Submódulo | Suporte a proxy       |
| net/pool            | Submódulo | Pool de ligações      |
| net/unix            | Submódulo | Sockets de domínio Unix |
| net/ip              | Submódulo | Operações com endereços IP |
| encoding/base64     | Submódulo | Codificação/descodificação Base64 |
| encoding/csv        | Submódulo | Análise de CSV        |
| encoding/pem        | Submódulo | Codificação/descodificação PEM |
| archive/tar         | Submódulo | Arquivo TAR           |
| archive/zip         | Submódulo | Arquivo ZIP           |
| archive/gzip        | Submódulo | Compressão GZIP       |
| archive/bzip2       | Submódulo | Descompressão BZIP2   |
| archive/xz          | Submódulo | Descompressão XZ/LZMA |
| archive/zlib        | Submódulo | Compressão zlib (RFC 1950) |
| archive/zstd        | Submódulo | Descompressão Zstandard |
| map/linked-hash-map | Submódulo | Mapa hash ordenado    |
| map/hash-set        | Submódulo | Conjunto hash de i64  |
| map/str-map         | Submódulo | Mapa hash str→str     |
| map/str-set         | Submódulo | Conjunto hash de str  |
| map/tree-map        | Submódulo | Mapa ordenado AVL     |
| map/tree-set        | Submódulo | Conjunto ordenado AVL |
| collection/queue    | Submódulo | Fila genérica         |
| collection/arr-stack| Submódulo | Pilha genérica        |
| collection/link     | Submódulo | Lista duplamente ligada genérica |
| collection/map      | Submódulo | Mapa hash dinâmico genérico |
| collection/static-hashmap | Submódulo | Mapa hash de capacidade fixa genérico |
| database/sql        | Submódulo | Interface de acesso a base de dados |
| crypto/aes          | Submódulo | Núcleo de bloco AES (128/256) |
| crypto/aes-cbc      | Submódulo | Modo AES-CBC (128/256) |
| crypto/aes-ctr      | Submódulo | Modo AES-CTR (128/256) |
| crypto/aes-gcm      | Submódulo | AES-GCM AEAD (128/256) |
| crypto/des            | Submódulo | Encriptação/descodificação DES |
| crypto/tdes           | Submódulo | DES triplo            |
| crypto/rsa            | Submódulo | Exponenciação modular RSA |
| crypto/md5            | Submódulo | Hash MD5              |
| crypto/sha1           | Submódulo | Hash SHA-1            |
| crypto/sha224         | Submódulo | Hash SHA-224          |
| crypto/sha256         | Submódulo | Hash SHA-256          |
| crypto/sha384         | Submódulo | Hash SHA-384          |
| crypto/sha512         | Submódulo | Hash SHA-512          |
| crypto/sha3           | Submódulo | Hash SHA-3            |
| crypto/blake2         | Submódulo | Hash BLAKE2           |
| crypto/crc-16         | Submódulo | Checksum CRC16        |
| crypto/crc-32         | Submódulo | Checksum CRC32        |
| crypto/crc-64         | Submódulo | Checksum CRC64        |
| crypto/fnv            | Submódulo | Hash FNV-1            |
| crypto/fnv-1a-32      | Submódulo | Hash FNV-1a           |
| crypto/hmac           | Submódulo | Código de autenticação HMAC |
| crypto/hkdf           | Submódulo | Derivação de chave HKDF |
| crypto/pbkdf2         | Submódulo | Derivação de chave PBKDF2 |
| crypto/argon2         | Submódulo | Derivação de chave Argon2 |
| crypto/scrypt         | Submódulo | Derivação de chave scrypt |
| crypto/chacha20-poly1305 | Submódulo | ChaCha20-Poly1305   |
| crypto/rc4            | Submódulo | Cifra de fluxo RC4    |
| crypto/ecdsa          | Submódulo | Assinatura ECDSA      |
| crypto/ed25519        | Submódulo | Assinatura Ed25519    |
| crypto/x25519         | Submódulo | Troca de chaves X25519 |
| crypto/base32         | Submódulo | Codificação/descodificação Base32 |
| crypto/rand           | Submódulo | Gerador de números aleatórios |
| crypto/x509           | Submódulo | Análise X.509 DER     |
