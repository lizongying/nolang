---
sidebar_position: 4.3
---

## 模组一览

| 模组                | 路径   | 说明             |
| ------------------- | ------ | ---------------- |
| fmt                 | 核心   | 格式化输出       |
| math                | 核心   | 数学函数         |
| str                 | 核心   | 字串操作         |
| vec                 | 核心   | 切片（[]t）操作  |
| arr                 | 核心   | 阵列（[n]t）操作 |
| number              | 核心   | 数值工具函数     |
| byte                | 核心   | 位元组操作       |
| char                | 核心   | 字元操作（方法） |
| os                  | 核心   | 作业系统介面     |
| env                 | 核心   | 环境变数封装     |
| fs                  | 核心   | 档案系统工具     |
| io                  | 核心   | 输入输出抽象     |
| args                | 核心   | 命令列引数       |
| path                | 核心   | 路径处理（结构体）|
| bufio               | 核心   | 缓冲读取         |
| time                | 核心   | 时间操作         |
| log                 | 核心   | 分级日志         |
| json                | 核心   | JSON 解析/产生   |
| toml                | 核心   | TOML 1.0 解析/产生 |
| yaml                | 核心   | YAML 1.2 解析/产生 |
| types               | 核心   | 型别定义文件     |
| sort                | 核心   | 排序常量         |
| set                 | 核心   | 集合             |
| deque               | 核心   | 双端伫列（结构体）|
| heap                | 核心   | 最小堆（结构体） |
| stack               | 核心   | 堆叠（结构体）   |
| regexp              | 核心   | 正规表示式       |
| process             | 核心   | 进程操作         |
| uuid                | 核心   | UUID v4          |
| bigint              | 核心   | 任意精度整数     |
| bool                | 核心   | 布尔型别         |
| err                 | 核心   | 错误处理         |
| enter               | 核心   | 启动钩子         |
| leave               | 核心   | 退出钩子         |
| async               | 核心   | 异步协程/取消     |
| global              | 核心   | 全域内建函数     |
| magic               | 核心   | 档案类型检测     |
| net                 | 核心   | TCP 网路操作     |
| net/http            | 子模组 | HTTP/1.1 客户端  |
| net/http2           | 子模组 | HTTP/2.0 客户端  |
| net/http3           | 子模组 | HTTP/3.0 客户端  |
| net/ws              | 子模组 | WebSocket        |
| net/quic            | 子模组 | QUIC 协议        |
| net/tls             | 子模组 | TLS 1.2/1.3     |
| net/sse             | 子模组 | SSE 客户端       |
| net/client          | 子模组 | 高阶 TCP 客户端  |
| net/server          | 子模组 | HTTP 伺服器      |
| net/dns             | 子模组 | DNS 解析         |
| net/url             | 子模组 | URL 解析         |
| net/cookie          | 子模组 | HTTP Cookie      |
| net/multipart       | 子模组 | Multipart 表单   |
| net/hpack           | 子模组 | HPACK 标头压缩   |
| net/proxy           | 子模组 | 代理支援         |
| net/pool            | 子模组 | 连接池           |
| net/unix            | 子模组 | Unix 域套接字    |
| net/ip              | 子模组 | IP 地址操作      |
| encoding/base64     | 子模组 | Base64 编解码    |
| encoding/csv        | 子模组 | CSV 解析         |
| encoding/pem        | 子模组 | PEM 编解码       |
| archive/tar         | 子模组 | TAR 归档         |
| archive/zip         | 子模组 | ZIP 归档         |
| archive/gzip        | 子模组 | GZIP 压缩        |
| archive/bzip2       | 子模组 | BZIP2 解压缩     |
| archive/xz          | 子模组 | XZ/LZMA 解压缩   |
| archive/zlib        | 子模组 | zlib 压缩（RFC 1950）|
| archive/zstd        | 子模组 | Zstandard 解压缩  |
| map/linked-hash-map | 子模组 | 有序哈希表       |
| map/hash-set        | 子模组 | i64 哈希集合     |
| map/str-map         | 子模组 | str→str 哈希映射 |
| map/str-set         | 子模组 | str 哈希集合     |
| map/tree-map        | 子模组 | AVL 有序映射     |
| map/tree-set        | 子模组 | AVL 有序集合     |
| collection/queue    | 子模组 | 泛型伫列         |
| collection/arr-stack| 子模组 | 泛型堆叠         |
| collection/link     | 子模组 | 泛型双向链结串列 |
| collection/map      | 子模组 | 泛型动态哈希映射 |
| collection/static-hashmap | 子模组 | 泛型固定容量哈希映射 |
| database/sql        | 子模组 | 资料库存取介面   |
| crypto/aes          | 子模组 | AES 区块核心（128/256） |
| crypto/aes-cbc      | 子模组 | AES-CBC 模式（128/256） |
| crypto/aes-ctr      | 子模组 | AES-CTR 模式（128/256） |
| crypto/aes-gcm      | 子模组 | AES-GCM AEAD（128/256） |
| crypto/des            | 子模组 | DES 加解密       |
| crypto/tdes           | 子模组 | 三重 DES         |
| crypto/rsa            | 子模组 | RSA 模幂         |
| crypto/md5            | 子模组 | MD5 杂凑         |
| crypto/sha1           | 子模组 | SHA-1 杂凑       |
| crypto/sha224         | 子模组 | SHA-224 杂凑     |
| crypto/sha256         | 子模组 | SHA-256 杂凑     |
| crypto/sha384         | 子模组 | SHA-384 杂凑     |
| crypto/sha512         | 子模组 | SHA-512 杂凑     |
| crypto/sha3           | 子模组 | SHA-3 杂凑       |
| crypto/blake2         | 子模组 | BLAKE2 杂凑      |
| crypto/crc-16         | 子模组 | CRC16 校验       |
| crypto/crc-32         | 子模组 | CRC32 校验       |
| crypto/crc-64         | 子模组 | CRC64 校验       |
| crypto/fnv            | 子模组 | FNV-1 杂凑       |
| crypto/fnv-1a-32      | 子模组 | FNV-1a 杂凑      |
| crypto/hmac           | 子模组 | HMAC 认证码      |
| crypto/hkdf           | 子模组 | HKDF 金钥推导    |
| crypto/pbkdf2         | 子模组 | PBKDF2 金钥推导  |
| crypto/argon2         | 子模组 | Argon2 金钥推导  |
| crypto/scrypt         | 子模组 | scrypt 金钥推导  |
| crypto/chacha20-poly1305 | 子模组 | ChaCha20-Poly1305 |
| crypto/rc4            | 子模组 | RC4 串流加密     |
| crypto/ecdsa          | 子模组 | ECDSA 签章       |
| crypto/ed25519        | 子模组 | Ed25519 签章     |
| crypto/x25519         | 子模组 | X25519 金钥交换  |
| crypto/base32         | 子模组 | Base32 编解码    |
| crypto/rand           | 子模组 | 随机数产生器     |
| crypto/x509           | 子模组 | X.509 DER 解析   |
