---
sidebar_position: 4.0
---

## 密碼學與雜湊

### crypto/aes — AES 區塊加密核心（AES-128 / AES-256）

```no
; 單區塊（自帶金鑰擴展）
out = aes.enc-128(in [16]byte, key [16]byte)   ; AES-128 加密
out = aes.dec-128(in [16]byte, key [16]byte)   ; AES-128 解密
out = aes.enc-256(in [16]byte, key [32]byte)   ; AES-256 加密
out = aes.dec-256(in [16]byte, key [32]byte)   ; AES-256 解密

; 多區塊：金鑰只展開一次，之後逐區塊加解密
ek []byte = aes.key-expand(key [16]byte)       ; 176 位元組輪金鑰
ek []byte = aes.key-expand-256(key [32]byte)   ; 240 位元組輪金鑰
out = aes.enc-block(in [16]byte, ek)
out = aes.dec-block(in [16]byte, ek)
```

輪數由 `ek` 長度推導（`nr = ek.len() / 16 - 1`），AES-128 與 AES-256 共用同一份輪迴圈。
工作模式位於 `crypto/aes-cbc`、`crypto/aes-ctr`、`crypto/aes-gcm`。

### crypto/des — DES 加解密（ECB 模式）

```no
des.des-enc(plain, 8, key, out)        ; 加密 8-byte 區塊
des.des-dec(cipher, 8, key, out)       ; 解密 8-byte 區塊
```

### crypto/rsa — RSA 模冪運算

```no
rsa.rsa-modpow(base, bn, exp, en, mod, mn, result, rn)
```

不包含金鑰生成，支援 1024~4096-bit。

### crypto/md5 — MD5（128-bit）

```no
out [16]byte = md5.md5(data)
```

### crypto/sha1 — SHA-1（160-bit）

```no
hash = sha1.sha1(data []byte) (hash [20]byte)
hex = sha1.sha1-hex(data []byte) (hex str)
sha1.sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32)
```

`sha1` 計算完整雜湊（含填充與多區塊處理），返回 20 位元組。
`sha1-hex` 同上但返回 40 字元小寫 hex 字串。
`sha1-block` 為低階 API，處理單個 512-bit 區塊。

### crypto/sha256 — SHA-256（256-bit）

```no
sha256.sha256(data []byte) (hash [32]byte)
sha256.sha256-hex(data []byte) (hex str)
sha256.sha256-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32, h5 u32, h6 u32, h7 u32)
```

`sha256` 計算完整雜湊（含填充與多區塊處理），返回 32 位元組。
`sha256-hex` 同上但返回 64 字元小寫 hex 字串。
`sha256-block` 為低階 API，處理單個 512-bit 區塊。

### crypto/sha512 — SHA-512（512-bit）

```no
sha512.sha512(data []byte) (hash [64]byte)
sha512.sha512-hex(data []byte) (hex str)
sha512.sha512-block(s []u64, h0 u64, h1 u64, h2 u64, h3 u64, h4 u64, h5 u64, h6 u64, h7 u64)
```

`sha512` 計算完整雜湊（含填充與多區塊處理），返回 64 位元組。
`sha512-hex` 同上但返回 128 字元小寫 hex 字串。
`sha512-block` 為低階 API，處理單個 1024-bit 區塊。

### crypto/crc-32 — CRC32 校驗

```no
crc-32.crc-32(s []byte, n, crc)
```

### crypto/fnv-1a-32 — FNV-1a 非加密雜湊

```no
fnv-1a-32.fnv-1a-32(s []byte, n, h)
```

### crypto/rand — 隨機數產生器（xorshift32）

```no
r = rand.rand(state)                     ; 32-bit 偽隨機數
rand.rand-str(state, n, s)              ; 隨機字母數字字串
```

### crypto/x509 — X.509 憑證 DER 解析

```no
tag = x509.der-tag(data, pos)
len, adv = x509.der-len(data, pos)
x509.x509-fingerprint(cert, n, h0..h7)  ; SHA-256 憑證指紋
x509.x509-rsa-e(cert, n, e)             ; RSA 公鑰指數提取
```

### crypto/aes-cbc — AES-CBC 模式（含 PKCS7 填充，AES-128 / AES-256）

```no
out = aes-cbc.enc-128(in []byte, key [16]byte, iv [16]byte)
out = aes-cbc.dec-128(in []byte, key [16]byte, iv [16]byte)
out = aes-cbc.enc-256(in []byte, key [32]byte, iv [16]byte)
out = aes-cbc.dec-256(in []byte, key [32]byte, iv [16]byte)
out = aes-cbc.pkcs7-pad(in []byte)
n = aes-cbc.pkcs7-unpad(in []byte)
```

### crypto/aes-ctr — AES-CTR 計數器模式（AES-128 / AES-256）

```no
out = aes-ctr.crypt-128(in []byte, key [16]byte, iv [16]byte)
out = aes-ctr.crypt-256(in []byte, key [32]byte, iv [16]byte)
```

### crypto/aes-gcm — AES-GCM AEAD（NIST SP 800-38D，AES-128 / AES-256）

```no
sealed = aes-gcm.seal-128(key [16]byte, iv [12]byte, aad []byte, plain []byte)
plain = aes-gcm.open-128(key [16]byte, iv [12]byte, aad []byte, sealed []byte)
sealed = aes-gcm.seal-256(key [32]byte, iv [12]byte, aad []byte, plain []byte)
plain = aes-gcm.open-256(key [32]byte, iv [12]byte, aad []byte, sealed []byte)
```

### crypto/hmac — HMAC 訊息認證碼

```no
out = hmac.hmac-sha256(key []byte, msg []byte) (out [32]byte)
out = hmac.hmac-sha512(key []byte, msg []byte) (out [64]byte)
out = hmac.hmac-sha384(key []byte, msg []byte) (out [48]byte)
out = hmac.hmac-sha1(key []byte, msg []byte) (out [20]byte)
out = hmac.hmac-md5(key []byte, msg []byte) (out [16]byte)
vec = hmac.hmac-sha256-vec(key []byte, msg []byte) (out []byte)```

### crypto/hkdf — HKDF 金鑰推導（RFC 5869）

```no
ok = hkdf.hkdf-extract(salt []byte, salt-n i64, ikm []byte, ikm-n i64, prk []byte)
ok = hkdf.hkdf-expand(prk []byte, prk-n i64, info []byte, info-n i64, out []byte, out-n i64)
```

### crypto/pbkdf2 — PBKDF2 金鑰推導（RFC 2898）

```no
out = pbkdf2.pbkdf2-hmac-sha256(password []byte, salt []byte, iterations i64, key-len i64) (out []byte)
out = pbkdf2.pbkdf2-hmac-sha512(password []byte, salt []byte, iterations i64, key-len i64) (out []byte)
out = pbkdf2.pbkdf2-hmac-sha1(password []byte, salt []byte, iterations i64, key-len i64) (out []byte)```

### crypto/argon2 — Argon2 記憶體硬金鑰推導

```no
argon2.argon2id(password []byte, pw-n i64, salt []byte, salt-n i64, time i64, memory i64, parallel i64, out []byte, out-n i64)
```

### crypto/scrypt — scrypt 金鑰推導

```no
scrypt.scrypt(password []byte, pw-n i64, salt []byte, salt-n i64, n i64, r i64, p i64, out []byte, out-n i64)
```

### crypto/sha224 — SHA-224（224-bit）

```no
hash = sha224.sha224(data []byte) (hash [28]byte)
hex = sha224.sha224-hex(data []byte) (hex str)
```

### crypto/sha384 — SHA-384（384-bit）

```no
hash = sha384.sha384(data []byte) (hash [48]byte)
hex = sha384.sha384-hex(data []byte) (hex str)
```

### crypto/sha3 — SHA-3（Keccak）

```no
hash = sha3.sha3-256(data []byte) (hash [32]byte)
hash = sha3.sha3-512(data []byte) (hash [64]byte)
```

### crypto/blake2 — BLAKE2 雜湊

```no
hash = blake2.blake2b(data []byte) (hash [64]byte)
hash = blake2.blake2b-256(data []byte) (hash [32]byte)
hex = blake2.blake2b-hex(data []byte) (hex str)
hash = blake2.blake2s(data []byte) (hash [32]byte)
hex = blake2.blake2s-hex(data []byte) (hex str)```

### crypto/crc-16 — CRC16 校驗

```no
crc = crc-16.crc-16(data []byte, n i64) (crc i64)
```

### crypto/crc-64 — CRC64 校驗

```no
crc = crc-64.crc-64(data []byte, n i64) (crc i64)
```

### crypto/fnv — FNV-1 雜湊

```no
h = fnv.fnv-1a-32(data []byte) (hash i64)
h = fnv.fnv-1a-64(data []byte) (hash u64)```

### crypto/base32 — Base32 編解碼（RFC 4648）

```no
out = base32.base32-encode(data []byte, n i64) (out str)
out = base32.base32-decode(s str, n i64) (out []byte)
```

### crypto/chacha20-poly1305 — ChaCha20-Poly1305 AEAD

```no
sealed = chacha20-poly1305.chacha20-poly1305-seal(key [32]byte, nonce [12]byte, aad []byte, plain []byte)
plain = chacha20-poly1305.chacha20-poly1305-open(key [32]byte, nonce [12]byte, aad []byte, sealed []byte)
```

### crypto/rc4 — RC4 串流加密

```no
out = rc4.rc4(key []byte, key-n i64, data []byte, data-n i64) (out []byte)
```

### crypto/tdes — 三重 DES（3DES）

```no
tdes.tdes-enc(plain, 8, key [24]byte, out)
tdes.tdes-dec(cipher, 8, key [24]byte, out)
```

### crypto/ecdsa — ECDSA 數位簽章

```no
ok = ecdsa.ecdsa-sign(priv-key []byte, msg []byte, msg-n i64, r []byte, s []byte)
ok = ecdsa.ecdsa-verify(pub-key []byte, msg []byte, msg-n i64, r []byte, s []byte) (ok bool)
```

### crypto/ed25519 — Ed25519 數位簽章

```no
pub = ed25519.ed25519-publickey(seed [32]byte) (pub [32]byte)
sig = ed25519.ed25519-sign(seed [32]byte, msg []byte) (sig [64]byte)
ok = ed25519.ed25519-verify(pub [32]byte, msg []byte, sig [64]byte) (ok i64)```

### crypto/x25519 — X25519 金鑰交換

```no
priv, pub = x25519.x25519-gen-keypair(seed i64) (priv [32]byte, pub [32]byte)
pub = x25519.x25519-scalarmult-base-bytes(scalar []byte) (out []byte)
shared = x25519.x25519-scalarmult-bytes(scalar []byte, point []byte) (out []byte)
out = x25519.x25519-scalarmult(scalar [32]byte, point [32]byte) (out [32]byte)```

---
