---
sidebar_position: 4.3
---

## Modulübersicht

| Modul               | Pfad  | Beschreibung             |
| ------------------- | ------ | ---------------- |
| fmt                 | Kern   | Formatierte Ausgabe       |
| math                | Kern   | Mathematische Funktionen         |
| str                 | Kern   | Zeichenkettenoperationen         |
| vec                 | Kern   | Slice-Operationen ([]t)  |
| arr                 | Kern   | Array-Operationen ([n]t) |
| number              | Kern   | Numerische Hilfsfunktionen     |
| byte                | Kern   | Byte-Operationen       |
| char                | Kern   | Zeichenoperationen (Methoden) |
| os                  | Kern   | Betriebssystem-Schnittstelle     |
| env                 | Kern   | Umgebungsvariablen-Wrapper     |
| fs                  | Kern   | Dateisystem-Werkzeuge     |
| io                  | Kern   | Ein-/Ausgabe-Abstraktion     |
| args                | Kern   | Kommandozeilenargumente       |
| path                | Kern   | Pfadbehandlung (Struktur) |
| bufio               | Kern   | Gepuffertes Lesen         |
| time                | Kern   | Zeitoperationen         |
| log                 | Kern   | Stufenbasierte Protokollierung         |
| json                | Kern   | JSON-Parsing/-Erzeugung   |
| toml                | Kern   | TOML-1.0-Parsing/-Erzeugung |
| yaml                | Kern   | YAML-1.2-Parsing/-Erzeugung |
| types               | Kern   | Dokumentation der Typdefinitionen     |
| sort                | Kern   | Sortierkonstanten         |
| set                 | Kern   | Menge             |
| deque               | Kern   | Doppelseitige Warteschlange (Struktur) |
| heap                | Kern   | Min-Heap (Struktur) |
| stack               | Kern   | Stapel (Struktur)   |
| regexp              | Kern   | Reguläre Ausdrücke       |
| process             | Kern   | Prozessoperationen         |
| uuid                | Kern   | UUID v4          |
| bigint              | Kern   | Ganzzahl beliebiger Genauigkeit     |
| bool                | Kern   | Boolescher Typ         |
| err                 | Kern   | Fehlerbehandlung         |
| enter               | Kern   | Start-Hook         |
| leave               | Kern   | Exit-Hook         |
| async               | Kern   | Asynchrone Coroutinen/Abbruch     |
| global              | Kern   | Globale Builtins     |
| magic               | Kern   | Dateityp-Erkennung     |
| net                 | Kern   | TCP-Netzwerkoperationen     |
| net/http            | Untermodul | HTTP/1.1-Client  |
| net/http2           | Untermodul | HTTP/2.0-Client  |
| net/http3           | Untermodul | HTTP/3.0-Client  |
| net/ws              | Untermodul | WebSocket        |
| net/quic            | Untermodul | QUIC-Protokoll        |
| net/tls             | Untermodul | TLS 1.2/1.3     |
| net/sse             | Untermodul | SSE-Client       |
| net/client          | Untermodul | High-Level-TCP-Client  |
| net/server          | Untermodul | HTTP-Server      |
| net/dns             | Untermodul | DNS-Auflösung         |
| net/url             | Untermodul | URL-Parsing         |
| net/cookie          | Untermodul | HTTP-Cookie      |
| net/multipart       | Untermodul | Multipart-Formular   |
| net/hpack           | Untermodul | HPACK-Header-Kompression   |
| net/proxy           | Untermodul | Proxy-Unterstützung         |
| net/pool            | Untermodul | Verbindungspool           |
| net/unix            | Untermodul | Unix-Domain-Sockets    |
| net/ip              | Untermodul | IP-Adressoperationen      |
| encoding/base64     | Untermodul | Base64-Kodierung/-Dekodierung    |
| encoding/csv        | Untermodul | CSV-Parsing         |
| encoding/pem        | Untermodul | PEM-Kodierung/-Dekodierung       |
| archive/tar         | Untermodul | TAR-Archive         |
| archive/zip         | Untermodul | ZIP-Archive         |
| archive/gzip        | Untermodul | GZIP-Kompression        |
| archive/bzip2       | Untermodul | BZIP2-Dekompression     |
| archive/xz          | Untermodul | XZ-/LZMA-Dekompression   |
| archive/zlib        | Untermodul | zlib-Kompression (RFC 1950) |
| archive/zstd        | Untermodul | Zstandard-Dekompression  |
| map/linked-hash-map | Untermodul | Geordnete Hash-Map       |
| map/hash-set        | Untermodul | i64-Hash-Menge     |
| map/str-map         | Untermodul | str→str-Hash-Map |
| map/str-set         | Untermodul | str-Hash-Menge     |
| map/tree-map        | Untermodul | AVL-geordnete Map     |
| map/tree-set        | Untermodul | AVL-geordnete Menge     |
| collection/queue    | Untermodul | Generische Warteschlange         |
| collection/arr-stack| Untermodul | Generischer Stapel         |
| collection/link     | Untermodul | Generische doppelt verkettete Liste |
| collection/map      | Untermodul | Generische dynamische Hash-Map |
| collection/static-hashmap | Untermodul | Generische Hash-Map fester Kapazität |
| database/sql        | Untermodul | Schnittstelle für Datenbankzugriff   |
| crypto/aes          | Untermodul | AES-Blockkern (128/256) |
| crypto/aes-cbc      | Untermodul | AES-CBC-Modus (128/256) |
| crypto/aes-ctr      | Untermodul | AES-CTR-Modus (128/256) |
| crypto/aes-gcm      | Untermodul | AES-GCM AEAD (128/256) |
| crypto/des            | Untermodul | DES-Ver-/Entschlüsselung       |
| crypto/tdes           | Untermodul | Triple DES         |
| crypto/rsa            | Untermodul | RSA-Modulpotenzierung         |
| crypto/md5            | Untermodul | MD5-Hash         |
| crypto/sha1           | Untermodul | SHA-1-Hash       |
| crypto/sha224         | Untermodul | SHA-224-Hash     |
| crypto/sha256         | Untermodul | SHA-256-Hash     |
| crypto/sha384         | Untermodul | SHA-384-Hash     |
| crypto/sha512         | Untermodul | SHA-512-Hash     |
| crypto/sha3           | Untermodul | SHA-3-Hash       |
| crypto/blake2         | Untermodul | BLAKE2-Hash      |
| crypto/crc-16         | Untermodul | CRC16-Prüfsumme       |
| crypto/crc-32         | Untermodul | CRC32-Prüfsumme       |
| crypto/crc-64         | Untermodul | CRC64-Prüfsumme       |
| crypto/fnv            | Untermodul | FNV-1-Hash       |
| crypto/fnv-1a-32      | Untermodul | FNV-1a-Hash      |
| crypto/hmac           | Untermodul | HMAC-Authentifizierungscode      |
| crypto/hkdf           | Untermodul | HKDF-Schlüsselableitung    |
| crypto/pbkdf2         | Untermodul | PBKDF2-Schlüsselableitung  |
| crypto/argon2         | Untermodul | Argon2-Schlüsselableitung  |
| crypto/scrypt         | Untermodul | scrypt-Schlüsselableitung  |
| crypto/chacha20-poly1305 | Untermodul | ChaCha20-Poly1305 |
| crypto/rc4            | Untermodul | RC4-Stromverschlüsselung     |
| crypto/ecdsa          | Untermodul | ECDSA-Signatur       |
| crypto/ed25519        | Untermodul | Ed25519-Signatur     |
| crypto/x25519         | Untermodul | X25519-Schlüsselaustausch  |
| crypto/base32         | Untermodul | Base32-Kodierung/-Dekodierung    |
| crypto/rand           | Untermodul | Zufallszahlengenerator     |
| crypto/x509           | Untermodul | X.509-DER-Parsing   |
