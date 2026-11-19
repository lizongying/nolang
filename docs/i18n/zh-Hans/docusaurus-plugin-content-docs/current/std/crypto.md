---
sidebar_position: 4.0
---

## 密码学与杂凑

### crypto/aes — AES 区块加密核心（AES-128 / AES-256）

```no
#{fmt-skip}
; 单区块（自带金钥扩展）
out = aes.enc-128(in [16]byte, key [16]byte)   ; AES-128 加密
out = aes.dec-128(in [16]byte, key [16]byte)   ; AES-128 解密
out = aes.enc-256(in [16]byte, key [32]byte)   ; AES-256 加密
out = aes.dec-256(in [16]byte, key [32]byte)   ; AES-256 解密

; 多区块：金钥只展开一次，之后逐区块加解密
ek []byte = aes.key-expand(key [16]byte)       ; 176 位元组轮金钥
ek []byte = aes.key-expand-256(key [32]byte)   ; 240 位元组轮金钥
out = aes.enc-block(in [16]byte, ek)
out = aes.dec-block(in [16]byte, ek)
```

轮数由 `ek` 长度推导（`nr = ek.len() / 16 - 1`），AES-128 与 AES-256 共用同一份轮回圈。
工作模式位于 `crypto/aes-cbc`、`crypto/aes-ctr`、`crypto/aes-gcm`。

### crypto/des — DES 加解密（ECB 模式）

```no
des.des-enc(plain, 8, key, out); 加密 8-byte 区块
des.des-dec(cipher, 8, key, out); 解密 8-byte 区块
```

### crypto/rsa — RSA 加解密（多精度模幂）

```no
; 高阶位元组介面（推荐）：位元组串按大端无符号整数解释，输出为最短大端编码
cipher = rsa.rsa-encrypt-bytes(msg, pub-exp, mod); c = m^e mod n
plain = rsa.rsa-decrypt-bytes(cipher, priv-exp, mod); m = c^d mod n

; 低阶 limb 介面（base 2^32 little-endian，手动管理 limb 计数）
result, rn = rsa.rsa-modpow(base, bn, exp, en, mod, mn)
```

不包含金钥生成（呼叫者自行提供 n、e、d），支援 1024~4096-bit。limb 包装/大数工具等底线开头符号为模组内部实作，跨模组禁用（private_std 守卫报 ERROR）。

### crypto/md5 — MD5（128-bit）

```no
out [16]byte = md5.md5(data)
```

### crypto/sha1 — SHA-1（160-bit）

```no
#{fmt-skip}
hash = sha1.sha1(data []byte) (hash [20]byte)
hex = sha1.sha1-hex(data []byte) (hex str)
sha1.sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32)
```

`sha1` 计算完整杂凑（含填充与多区块处理），返回 20 位元组。
`sha1-hex` 同上但返回 40 字元小写 hex 字串。
`sha1-block` 为低阶 API，处理单个 512-bit 区块。

### crypto/sha256 — SHA-256（256-bit）

```no
#{fmt-skip}
sha256.sha256(data []byte) (hash [32]byte)
sha256.sha256-hex(data []byte) (hex str)
sha256.sha256-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32, h5 u32, h6 u32, h7 u32)
```

`sha256` 计算完整杂凑（含填充与多区块处理），返回 32 位元组。
`sha256-hex` 同上但返回 64 字元小写 hex 字串。
`sha256-block` 为低阶 API，处理单个 512-bit 区块。

### crypto/sha512 — SHA-512（512-bit）

```no
#{fmt-skip}
sha512.sha512(data []byte) (hash [64]byte)
sha512.sha512-hex(data []byte) (hex str)
sha512.sha512-block(s []u64, h0 u64, h1 u64, h2 u64, h3 u64, h4 u64, h5 u64, h6 u64, h7 u64)
```

`sha512` 计算完整杂凑（含填充与多区块处理），返回 64 位元组。
`sha512-hex` 同上但返回 128 字元小写 hex 字串。
`sha512-block` 为低阶 API，处理单个 1024-bit 区块。

### crypto/crc-32 — CRC32 校验

```no
#{fmt-skip}
crc-32.new(s []byte, n, crc)
```

### crypto/fnv-1a-32 — FNV-1a 非加密杂凑

```no
#{fmt-skip}
fnv-1a-32.fnv-1a-32(s []byte, n, h)
```

### crypto/rand — 随机数产生器（xorshift32）

```no
r = rand.rand(state); 32-bit 伪随机数
rand.rand-str(state, n, s); 随机字母数字字串
```

### crypto/x509 — X.509 凭证 DER 解析

```no
#{fmt-skip}
tag = x509.der-tag(data, pos)
len, adv = x509.der-len(data, pos)
x509.x509-fingerprint(cert, n, h0..h7)  ; SHA-256 凭证指纹
x509.x509-rsa-e(cert, n, e)             ; RSA 公钥指数提取
```

### crypto/aes-cbc — AES-CBC 模式（含 PKCS7 填充，AES-128 / AES-256）

```no
#{fmt-skip}
out = aes-cbc.enc-128(in []byte, key [16]byte, iv [16]byte)
out = aes-cbc.dec-128(in []byte, key [16]byte, iv [16]byte)
out = aes-cbc.enc-256(in []byte, key [32]byte, iv [16]byte)
out = aes-cbc.dec-256(in []byte, key [32]byte, iv [16]byte)
out = aes-cbc.pkcs7-pad(in []byte)
n = aes-cbc.pkcs7-unpad(in []byte)
```

### crypto/aes-ctr — AES-CTR 计数器模式（AES-128 / AES-256）

```no
#{fmt-skip}
out = aes-ctr.crypt-128(in []byte, key [16]byte, iv [16]byte)
out = aes-ctr.crypt-256(in []byte, key [32]byte, iv [16]byte)
```

### crypto/aes-gcm — AES-GCM AEAD（NIST SP 800-38D，AES-128 / AES-256）

```no
#{fmt-skip}
sealed = aes-gcm.seal-128(key [16]byte, iv [12]byte, aad []byte, plain []byte)
plain = aes-gcm.open-128(key [16]byte, iv [12]byte, aad []byte, sealed []byte)
sealed = aes-gcm.seal-256(key [32]byte, iv [12]byte, aad []byte, plain []byte)
plain = aes-gcm.open-256(key [32]byte, iv [12]byte, aad []byte, sealed []byte)
```

### crypto/hmac — HMAC 讯息认证码

```no
#{fmt-skip}
out = hmac.hmac-sha256(key []byte, msg []byte) (out [32]byte)
out = hmac.hmac-sha512(key []byte, msg []byte) (out [64]byte)
out = hmac.hmac-sha384(key []byte, msg []byte) (out [48]byte)
out = hmac.hmac-sha1(key []byte, msg []byte) (out [20]byte)
out = hmac.hmac-md5(key []byte, msg []byte) (out [16]byte)
vec = hmac.hmac-sha256-vec(key []byte, msg []byte) (out []byte)
```

### crypto/hkdf — HKDF 金钥推导（RFC 5869）

```no
#{fmt-skip}
ok = hkdf.hkdf-extract(salt []byte, salt-n i64, ikm []byte, ikm-n i64, prk []byte)
ok = hkdf.hkdf-expand(prk []byte, prk-n i64, info []byte, info-n i64, out []byte, out-n i64)
```

### crypto/pbkdf2 — PBKDF2 金钥推导（RFC 2898）

```no
#{fmt-skip}
out = pbkdf2.pbkdf2-hmac-sha256(password []byte, salt []byte, iterations i64, key-len i64) (out []byte)
out = pbkdf2.pbkdf2-hmac-sha512(password []byte, salt []byte, iterations i64, key-len i64) (out []byte)
out = pbkdf2.pbkdf2-hmac-sha1(password []byte, salt []byte, iterations i64, key-len i64) (out []byte)
```

### crypto/argon2 — Argon2 记忆体硬金钥推导

```no
#{fmt-skip}
argon2.id(password []byte, pw-n i64, salt []byte, salt-n i64, time i64, memory i64, parallel i64, out []byte, out-n i64)
```

### crypto/scrypt — scrypt 金钥推导

```no
#{fmt-skip}
scrypt.scrypt(password []byte, pw-n i64, salt []byte, salt-n i64, n i64, r i64, p i64, out []byte, out-n i64)
```

### crypto/sha224 — SHA-224（224-bit）

```no
#{fmt-skip}
hash = sha224.sha224(data []byte) (hash [28]byte)
hex = sha224.sha224-hex(data []byte) (hex str)
```

### crypto/sha384 — SHA-384（384-bit）

```no
#{fmt-skip}
hash = sha384.sha384(data []byte) (hash [48]byte)
hex = sha384.sha384-hex(data []byte) (hex str)
```

### crypto/sha3 — SHA-3（Keccak）

```no
#{fmt-skip}
hash = sha3.sha3-256(data []byte) (hash [32]byte)
hash = sha3.sha3-512(data []byte) (hash [64]byte)
```

### crypto/blake2 — BLAKE2 杂凑

```no
#{fmt-skip}
hash = blake2.b(data []byte) (hash [64]byte)
hash = blake2.b-256(data []byte) (hash [32]byte)
hex = blake2.b-hex(data []byte) (hex str)
hash = blake2.s(data []byte) (hash [32]byte)
hex = blake2.s-hex(data []byte) (hex str)
```

### crypto/crc-16 — CRC16 校验

```no
#{fmt-skip}
crc = crc-16.new(data []byte, n i64) (crc i64)
```

### crypto/crc-64 — CRC64 校验

```no
#{fmt-skip}
crc = crc-64.new(data []byte, n i64) (crc i64)
```

### crypto/fnv — FNV-1 杂凑

```no
#{fmt-skip}
h = fnv.fnv-1a-32(data []byte) (hash i64)
h = fnv.fnv-1a-64(data []byte) (hash u64)
```

### crypto/base32 — Base32 编解码（RFC 4648）

```no
#{fmt-skip}
out = base32.encode(data []byte) (out str)
out = base32.decode(s str) (out []byte)
```

### crypto/chacha20-poly1305 — ChaCha20-Poly1305 AEAD

```no
#{fmt-skip}
sealed = chacha20-poly1305.chacha20-poly1305-seal(key [32]byte, nonce [12]byte, aad []byte, plain []byte)
plain = chacha20-poly1305.chacha20-poly1305-open(key [32]byte, nonce [12]byte, aad []byte, sealed []byte)
```

### crypto/rc4 — RC4 串流加密

```no
#{fmt-skip}
out = rc4.rc4(key []byte, key-n i64, data []byte, data-n i64) (out []byte)
```

### crypto/tdes — 三重 DES（3DES）

```no
#{fmt-skip}
tdes.tdes-enc(plain, 8, key [24]byte, out)
tdes.tdes-dec(cipher, 8, key [24]byte, out)
```

### crypto/ecdsa — ECDSA 数位签章

```no
#{fmt-skip}
ok = ecdsa.ecdsa-sign(priv-key []byte, msg []byte, msg-n i64, r []byte, s []byte)
ok = ecdsa.ecdsa-verify(pub-key []byte, msg []byte, msg-n i64, r []byte, s []byte) (ok bool)
```

### crypto/ed25519 — Ed25519 数位签章

```no
#{fmt-skip}
pub = ed25519.ed25519-publickey(seed [32]byte) (pub [32]byte)
sig = ed25519.ed25519-sign(seed [32]byte, msg []byte) (sig [64]byte)
ok = ed25519.ed25519-verify(pub [32]byte, msg []byte, sig [64]byte) (ok i64)
```

### crypto/x25519 — X25519 金钥交换

```no
#{fmt-skip}
priv, pub = x25519.x25519-gen-keypair(seed i64) (priv [32]byte, pub [32]byte)
pub = x25519.x25519-scalarmult-base-bytes(scalar []byte) (out []byte)
shared = x25519.x25519-scalarmult-bytes(scalar []byte, point []byte) (out []byte)
out = x25519.x25519-scalarmult(scalar [32]byte, point [32]byte) (out [32]byte)
```

---
