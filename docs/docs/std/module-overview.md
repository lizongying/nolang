---
sidebar_position: 4.3
---

## 模組一覽

| 模組                | 路徑   | 說明             |
| ------------------- | ------ | ---------------- |
| fmt                 | 核心   | 格式化輸出       |
| math                | 核心   | 數學函數         |
| str                 | 核心   | 字串操作         |
| vec                 | 核心   | 切片（[]t）操作  |
| arr                 | 核心   | 陣列（[n]t）操作 |
| number              | 核心   | 數值工具函數     |
| byte                | 核心   | 位元組操作       |
| char                | 核心   | 字元操作（方法） |
| os                  | 核心   | 作業系統介面     |
| env                 | 核心   | 環境變數封裝     |
| fs                  | 核心   | 檔案系統工具     |
| io                  | 核心   | 輸入輸出抽象     |
| args                | 核心   | 命令列引數       |
| path                | 核心   | 路徑處理（結構體）|
| bufio               | 核心   | 緩衝讀取         |
| time                | 核心   | 時間操作         |
| log                 | 核心   | 分級日誌         |
| json                | 核心   | JSON 解析/產生   |
| toml                | 核心   | TOML 1.0 解析/產生 |
| types               | 核心   | 型別定義文件     |
| option              | 核心   | 選項型別         |
| sort                | 核心   | 排序常量         |
| set                 | 核心   | 集合             |
| deque               | 核心   | 雙端佇列（結構體）|
| heap                | 核心   | 最小堆（結構體） |
| stack               | 核心   | 堆疊（結構體）   |
| regexp              | 核心   | 正規表示式       |
| process             | 核心   | 進程操作         |
| unicode             | 核心   | Unicode 說明     |
| uuid                | 核心   | UUID v4          |
| bigint              | 核心   | 任意精度整數     |
| bool                | 核心   | 布爾型別         |
| err                 | 核心   | 錯誤處理         |
| enter               | 核心   | 啟動鉤子         |
| leave               | 核心   | 退出鉤子         |
| async               | 核心   | 異步協程/取消     |
| global              | 核心   | 全域內建函數     |
| magic               | 核心   | 檔案類型檢測     |
| net                 | 核心   | TCP 網路操作     |
| net/http            | 子模組 | HTTP/1.1 客戶端  |
| net/http2           | 子模組 | HTTP/2.0 客戶端  |
| net/http3           | 子模組 | HTTP/3.0 客戶端  |
| net/ws              | 子模組 | WebSocket        |
| net/quic            | 子模組 | QUIC 協議        |
| net/tls             | 子模組 | TLS 1.2/1.3     |
| net/sse             | 子模組 | SSE 客戶端       |
| net/client          | 子模組 | 高階 TCP 客戶端  |
| net/server          | 子模組 | HTTP 伺服器      |
| net/dns             | 子模組 | DNS 解析         |
| net/url             | 子模組 | URL 解析         |
| net/cookie          | 子模組 | HTTP Cookie      |
| net/multipart       | 子模組 | Multipart 表單   |
| net/hpack           | 子模組 | HPACK 標頭壓縮   |
| net/proxy           | 子模組 | 代理支援         |
| net/pool            | 子模組 | 連接池           |
| net/unix            | 子模組 | Unix 域套接字    |
| net/ip              | 子模組 | IP 地址操作      |
| encoding/hex        | 子模組 | 十六進制編解碼   |
| encoding/base64     | 子模組 | Base64 編解碼    |
| encoding/csv        | 子模組 | CSV 解析         |
| encoding/pem        | 子模組 | PEM 編解碼       |
| archive/tar         | 子模組 | TAR 歸檔         |
| archive/zip         | 子模組 | ZIP 歸檔         |
| archive/gzip        | 子模組 | GZIP 壓縮        |
| archive/bzip2       | 子模組 | BZIP2 解壓縮     |
| archive/xz          | 子模組 | XZ/LZMA 解壓縮   |
| archive/zlib        | 子模組 | zlib 壓縮（RFC 1950）|
| archive/zstd        | 子模組 | Zstandard 解壓縮  |
| map/linked-hash-map | 子模組 | 有序哈希表       |
| map/hash-set        | 子模組 | i64 哈希集合     |
| map/str-map         | 子模組 | str→str 哈希映射 |
| map/str-set         | 子模組 | str 哈希集合     |
| map/tree-map        | 子模組 | AVL 有序映射     |
| map/tree-set        | 子模組 | AVL 有序集合     |
| collection/queue    | 子模組 | 泛型佇列         |
| collection/arr-stack| 子模組 | 泛型堆疊         |
| collection/link     | 子模組 | 泛型雙向鏈結串列 |
| collection/map      | 子模組 | 泛型動態哈希映射 |
| collection/static-hashmap | 子模組 | 泛型固定容量哈希映射 |
| database/sql        | 子模組 | 資料庫存取介面   |
| hash/aes            | 子模組 | AES-128 加解密   |
| hash/aes-128-enc    | 子模組 | AES-128 加密     |
| hash/aes-128-dec    | 子模組 | AES-128 解密     |
| hash/aes-256        | 子模組 | AES-256 加解密   |
| hash/aes-cbc        | 子模組 | AES-CBC 模式     |
| hash/aes-256-cbc    | 子模組 | AES-256-CBC     |
| hash/aes-ctr        | 子模組 | AES-CTR 模式     |
| hash/aes-gcm        | 子模組 | AES-GCM AEAD    |
| hash/aes-256-gcm    | 子模組 | AES-256-GCM     |
| hash/des            | 子模組 | DES 加解密       |
| hash/des-enc        | 子模組 | DES 加密         |
| hash/des-dec        | 子模組 | DES 解密         |
| hash/tdes           | 子模組 | 三重 DES         |
| hash/rsa            | 子模組 | RSA 模冪         |
| hash/md5            | 子模組 | MD5 雜湊         |
| hash/sha1           | 子模組 | SHA-1 雜湊       |
| hash/sha224         | 子模組 | SHA-224 雜湊     |
| hash/sha256         | 子模組 | SHA-256 雜湊     |
| hash/sha384         | 子模組 | SHA-384 雜湊     |
| hash/sha512         | 子模組 | SHA-512 雜湊     |
| hash/sha3           | 子模組 | SHA-3 雜湊       |
| hash/blake2         | 子模組 | BLAKE2 雜湊      |
| hash/crc-16         | 子模組 | CRC16 校驗       |
| hash/crc-32         | 子模組 | CRC32 校驗       |
| hash/crc-64         | 子模組 | CRC64 校驗       |
| hash/fnv            | 子模組 | FNV-1 雜湊       |
| hash/fnv-1a-32      | 子模組 | FNV-1a 雜湊      |
| hash/hmac           | 子模組 | HMAC 認證碼      |
| hash/hkdf           | 子模組 | HKDF 金鑰推導    |
| hash/pbkdf2         | 子模組 | PBKDF2 金鑰推導  |
| hash/argon2         | 子模組 | Argon2 金鑰推導  |
| hash/scrypt         | 子模組 | scrypt 金鑰推導  |
| hash/chacha20-poly1305 | 子模組 | ChaCha20-Poly1305 |
| hash/rc4            | 子模組 | RC4 串流加密     |
| hash/ecdsa          | 子模組 | ECDSA 簽章       |
| hash/ed25519        | 子模組 | Ed25519 簽章     |
| hash/x25519         | 子模組 | X25519 金鑰交換  |
| hash/base32         | 子模組 | Base32 編解碼    |
| hash/rand           | 子模組 | 隨機數產生器     |
| hash/rand-str       | 子模組 | 隨機字串產生     |
| hash/x509           | 子模組 | X.509 DER 解析   |
