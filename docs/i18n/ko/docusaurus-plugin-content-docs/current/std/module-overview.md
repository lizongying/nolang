---
sidebar_position: 4.3
---

## 모듈 목록

| 모듈                | 경로   | 설명             |
| ------------------- | ------ | ---------------- |
| fmt                 | 핵심   | 형식화된 출력       |
| math                | 핵심   | 수학 함수         |
| str                 | 핵심   | 문자열 조작         |
| vec                 | 핵심   | 슬라이스([]t) 조작  |
| arr                 | 핵심   | 배열([n]t) 조작 |
| number              | 핵심   | 수치 도구 함수     |
| byte                | 핵심   | 바이트 조작       |
| char                | 핵심   | 문자 조작 (메서드) |
| os                  | 핵심   | 운영 체제 인터페이스     |
| env                 | 핵심   | 환경 변수 캡슐화     |
| fs                  | 핵심   | 파일 시스템 도구     |
| io                  | 핵심   | 입출력 추상화     |
| args                | 핵심   | 명령줄 인자       |
| path                | 핵심   | 경로 처리 (구조체)|
| bufio               | 핵심   | 버퍼 읽기         |
| time                | 핵심   | 시간 조작         |
| log                 | 핵심   | 레벨별 로그         |
| json                | 핵심   | JSON 파싱/생성   |
| toml                | 핵심   | TOML 1.0 파싱/생성 |
| yaml                | 핵심   | YAML 1.2 파싱/생성 |
| types               | 핵심   | 타입 정의 문서     |
| sort                | 핵심   | 정렬 상수         |
| set                 | 핵심   | 집합             |
| deque               | 핵심   | 양방향 큐 (구조체)|
| heap                | 핵심   | 최소 힙 (구조체) |
| stack               | 핵심   | 스택 (구조체)   |
| regexp              | 핵심   | 정규 표현식       |
| process             | 핵심   | 프로세스 조작         |
| uuid                | 핵심   | UUID v4          |
| bigint              | 핵심   | 임의 정밀도 정수     |
| bool                | 핵심   | 불리언 타입         |
| err                 | 핵심   | 오류 처리         |
| enter               | 핵심   | 시작 훅         |
| leave               | 핵심   | 종료 훅         |
| async               | 핵심   | 비동기 코루틴/취소     |
| global              | 핵심   | 전역 내장 함수     |
| magic               | 핵심   | 파일 유형 감지     |
| net                 | 핵심   | TCP 네트워크 조작     |
| net/http            | 하위 모듈 | HTTP/1.1 클라이언트  |
| net/http2           | 하위 모듈 | HTTP/2.0 클라이언트  |
| net/http3           | 하위 모듈 | HTTP/3.0 클라이언트  |
| net/ws              | 하위 모듈 | WebSocket        |
| net/quic            | 하위 모듈 | QUIC 프로토콜        |
| net/tls             | 하위 모듈 | TLS 1.2/1.3     |
| net/sse             | 하위 모듈 | SSE 클라이언트       |
| net/client          | 하위 모듈 | 고급 TCP 클라이언트  |
| net/server          | 하위 모듈 | HTTP 서버      |
| net/dns             | 하위 모듈 | DNS 파싱         |
| net/url             | 하위 모듈 | URL 파싱         |
| net/cookie          | 하위 모듈 | HTTP Cookie      |
| net/multipart       | 하위 모듈 | Multipart 폼   |
| net/hpack           | 하위 모듈 | HPACK 헤더 압축   |
| net/proxy           | 하위 모듈 | 프록시 지원         |
| net/pool            | 하위 모듈 | 연결 풀           |
| net/unix            | 하위 모듈 | Unix 도메인 소켓    |
| net/ip              | 하위 모듈 | IP 주소 조작      |
| encoding/base64     | 하위 모듈 | Base64 인코딩/디코딩    |
| encoding/csv        | 하위 모듈 | CSV 파싱         |
| encoding/pem        | 하위 모듈 | PEM 인코딩/디코딩       |
| archive/tar         | 하위 모듈 | TAR 아카이브         |
| archive/zip         | 하위 모듈 | ZIP 아카이브         |
| archive/gzip        | 하위 모듈 | GZIP 압축        |
| archive/bzip2       | 하위 모듈 | BZIP2 압축 해제     |
| archive/xz          | 하위 모듈 | XZ/LZMA 압축 해제   |
| archive/zlib        | 하위 모듈 | zlib 압축 (RFC 1950)|
| archive/zstd        | 하위 모듈 | Zstandard 압축 해제  |
| map/linked-hash-map | 하위 모듈 | 정렬된 해시 테이블       |
| map/hash-set        | 하위 모듈 | i64 해시 집합     |
| map/str-map         | 하위 모듈 | str→str 해시 맵 |
| map/str-set         | 하위 모듈 | str 해시 집합     |
| map/tree-map        | 하위 모듈 | AVL 정렬된 맵     |
| map/tree-set        | 하위 모듈 | AVL 정렬된 집합     |
| collection/queue    | 하위 모듈 | 범용 큐         |
| collection/arr-stack| 하위 모듈 | 범용 스택         |
| collection/link     | 하위 모듈 | 범용 양방향 연결 리스트 |
| collection/map      | 하위 모듈 | 범용 동적 해시 맵 |
| collection/static-hashmap | 하위 모듈 | 범용 고정 용량 해시 맵 |
| database/sql        | 하위 모듈 | 데이터베이스 접근 인터페이스   |
| crypto/aes          | 하위 모듈 | AES 블록 핵심 (128/256) |
| crypto/aes-cbc      | 하위 모듈 | AES-CBC 모드 (128/256) |
| crypto/aes-ctr      | 하위 모듈 | AES-CTR 모드 (128/256) |
| crypto/aes-gcm      | 하위 모듈 | AES-GCM AEAD (128/256) |
| crypto/des            | 하위 모듈 | DES 암호화/복호화       |
| crypto/tdes           | 하위 모듈 | 트리플 DES         |
| crypto/rsa            | 하위 모듈 | RSA 모듈러 거듭제곱         |
| crypto/md5            | 하위 모듈 | MD5 해시         |
| crypto/sha1           | 하위 모듈 | SHA-1 해시       |
| crypto/sha224         | 하위 모듈 | SHA-224 해시     |
| crypto/sha256         | 하위 모듈 | SHA-256 해시     |
| crypto/sha384         | 하위 모듈 | SHA-384 해시     |
| crypto/sha512         | 하위 모듈 | SHA-512 해시     |
| crypto/sha3           | 하위 모듈 | SHA-3 해시       |
| crypto/blake2         | 하위 모듈 | BLAKE2 해시      |
| crypto/crc-16         | 하위 모듈 | CRC16 검사       |
| crypto/crc-32         | 하위 모듈 | CRC32 검사       |
| crypto/crc-64         | 하위 모듈 | CRC64 검사       |
| crypto/fnv            | 하위 모듈 | FNV-1 해시       |
| crypto/fnv-1a-32      | 하위 모듈 | FNV-1a 해시      |
| crypto/hmac           | 하위 모듈 | HMAC 인증 코드      |
| crypto/hkdf           | 하위 모듈 | HKDF 키 유도    |
| crypto/pbkdf2         | 하위 모듈 | PBKDF2 키 유도  |
| crypto/argon2         | 하위 모듈 | Argon2 키 유도  |
| crypto/scrypt         | 하위 모듈 | scrypt 키 유도  |
| crypto/chacha20-poly1305 | 하위 모듈 | ChaCha20-Poly1305 |
| crypto/rc4            | 하위 모듈 | RC4 스트림 암호화     |
| crypto/ecdsa          | 하위 모듈 | ECDSA 서명       |
| crypto/ed25519        | 하위 모듈 | Ed25519 서명     |
| crypto/x25519         | 하위 모듈 | X25519 키 교환  |
| crypto/base32         | 하위 모듈 | Base32 인코딩/디코딩    |
| crypto/rand           | 하위 모듈 | 난수 생성기     |
| crypto/x509           | 하위 모듈 | X.509 DER 파싱   |
