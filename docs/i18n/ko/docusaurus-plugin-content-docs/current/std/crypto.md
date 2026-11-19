---
sidebar_position: 4.0
---

## 암호학과 해시

### crypto/aes — AES 블록 암호화 핵심 (AES-128 / AES-256)

```no
#{fmt-skip}
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

라운드 수는 `ek` 길이에서 유도되며 (`nr = ek.len() / 16 - 1`), AES-128과 AES-256은 동일한 라운드 반복문을 공유합니다.
작업 모드는 `crypto/aes-cbc`, `crypto/aes-ctr`, `crypto/aes-gcm`에 있습니다.

### crypto/des — DES 암호화/복호화 (ECB 모드)

```no
des.des-enc(plain, 8, key, out); 加密 8-byte 區塊
des.des-dec(cipher, 8, key, out); 解密 8-byte 區塊
```

### crypto/rsa — RSA 암호화/복호화 (다정도 모듈러 거듭제곱)

```no
; 高階位元組介面（推薦）：位元組串按大端無符號整數解釋，輸出為最短大端編碼
cipher = rsa.rsa-encrypt-bytes(msg, pub-exp, mod); c = m^e mod n
plain = rsa.rsa-decrypt-bytes(cipher, priv-exp, mod); m = c^d mod n

; 低階 limb 介面（base 2^32 little-endian，手動管理 limb 計數）
result, rn = rsa.rsa-modpow(base, bn, exp, en, mod, mn)
```

키 생성은 포함되지 않으며(호출자가 n, e, d를 직접 제공), 1024~4096-bit를 지원합니다. limb 래퍼와 대수 도구 등 밑줄로 시작하는 심볼은 모듈 내부 구현이며, 모듈 간 사용이 금지됩니다(private_std 가드가 ERROR를 보고합니다).

### crypto/md5 — MD5 (128-bit)

```no
out [16]byte = md5.md5(data)
```

### crypto/sha1 — SHA-1 (160-bit)

```no
#{fmt-skip}
hash = sha1.sha1(data []byte) (hash [20]byte)
hex = sha1.sha1-hex(data []byte) (hex str)
sha1.sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32)
```

`sha1`은 패딩과 다중 블록 처리를 포함해 전체 해시를 계산하며 20바이트를 반환합니다.
`sha1-hex`는 동일하지만 40자의 소문자 hex 문자열을 반환합니다.
`sha1-block`은 단일 512-bit 블록을 처리하는 저수준 API입니다.

### crypto/sha256 — SHA-256 (256-bit)

```no
#{fmt-skip}
sha256.sha256(data []byte) (hash [32]byte)
sha256.sha256-hex(data []byte) (hex str)
sha256.sha256-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32, h5 u32, h6 u32, h7 u32)
```

`sha256`은 패딩과 다중 블록 처리를 포함해 전체 해시를 계산하며 32바이트를 반환합니다.
`sha256-hex`는 동일하지만 64자의 소문자 hex 문자열을 반환합니다.
`sha256-block`은 단일 512-bit 블록을 처리하는 저수준 API입니다.

### crypto/sha512 — SHA-512 (512-bit)

```no
#{fmt-skip}
sha512.sha512(data []byte) (hash [64]byte)
sha512.sha512-hex(data []byte) (hex str)
sha512.sha512-block(s []u64, h0 u64, h1 u64, h2 u64, h3 u64, h4 u64, h5 u64, h6 u64, h7 u64)
```

`sha512`는 패딩과 다중 블록 처리를 포함해 전체 해시를 계산하며 64바이트를 반환합니다.
`sha512-hex`는 동일하지만 128자의 소문자 hex 문자열을 반환합니다.
`sha512-block`은 단일 1024-bit 블록을 처리하는 저수준 API입니다.

### crypto/crc-32 — CRC32 검사

```no
#{fmt-skip}
crc-32.new(s []byte, n, crc)
```

### crypto/fnv-1a-32 — FNV-1a 비암호화 해시

```no
#{fmt-skip}
fnv-1a-32.fnv-1a-32(s []byte, n, h)
```

### crypto/rand — 난수 생성기 (xorshift32)

```no
r = rand.rand(state); 32-bit 偽隨機數
rand.rand-str(state, n, s); 隨機字母數字字串
```

### crypto/x509 — X.509 인증서 DER 파싱

```no
#{fmt-skip}
tag = x509.der-tag(data, pos)
len, adv = x509.der-len(data, pos)
x509.x509-fingerprint(cert, n, h0..h7)  ; SHA-256 憑證指紋
x509.x509-rsa-e(cert, n, e)             ; RSA 公鑰指數提取
```

### crypto/aes-cbc — AES-CBC 모드 (PKCS7 패딩 포함, AES-128 / AES-256)

```no
#{fmt-skip}
out = aes-cbc.enc-128(in []byte, key [16]byte, iv [16]byte)
out = aes-cbc.dec-128(in []byte, key [16]byte, iv [16]byte)
out = aes-cbc.enc-256(in []byte, key [32]byte, iv [16]byte)
out = aes-cbc.dec-256(in []byte, key [32]byte, iv [16]byte)
out = aes-cbc.pkcs7-pad(in []byte)
n = aes-cbc.pkcs7-unpad(in []byte)
```

### crypto/aes-ctr — AES-CTR 카운터 모드 (AES-128 / AES-256)

```no
#{fmt-skip}
out = aes-ctr.crypt-128(in []byte, key [16]byte, iv [16]byte)
out = aes-ctr.crypt-256(in []byte, key [32]byte, iv [16]byte)
```

### crypto/aes-gcm — AES-GCM AEAD (NIST SP 800-38D, AES-128 / AES-256)

```no
#{fmt-skip}
sealed = aes-gcm.seal-128(key [16]byte, iv [12]byte, aad []byte, plain []byte)
plain = aes-gcm.open-128(key [16]byte, iv [12]byte, aad []byte, sealed []byte)
sealed = aes-gcm.seal-256(key [32]byte, iv [12]byte, aad []byte, plain []byte)
plain = aes-gcm.open-256(key [32]byte, iv [12]byte, aad []byte, sealed []byte)
```

### crypto/hmac — HMAC 메시지 인증 코드

```no
#{fmt-skip}
out = hmac.hmac-sha256(key []byte, msg []byte) (out [32]byte)
out = hmac.hmac-sha512(key []byte, msg []byte) (out [64]byte)
out = hmac.hmac-sha384(key []byte, msg []byte) (out [48]byte)
out = hmac.hmac-sha1(key []byte, msg []byte) (out [20]byte)
out = hmac.hmac-md5(key []byte, msg []byte) (out [16]byte)
vec = hmac.hmac-sha256-vec(key []byte, msg []byte) (out []byte)
```

### crypto/hkdf — HKDF 키 유도 (RFC 5869)

```no
#{fmt-skip}
ok = hkdf.hkdf-extract(salt []byte, salt-n i64, ikm []byte, ikm-n i64, prk []byte)
ok = hkdf.hkdf-expand(prk []byte, prk-n i64, info []byte, info-n i64, out []byte, out-n i64)
```

### crypto/pbkdf2 — PBKDF2 키 유도 (RFC 2898)

```no
#{fmt-skip}
out = pbkdf2.pbkdf2-hmac-sha256(password []byte, salt []byte, iterations i64, key-len i64) (out []byte)
out = pbkdf2.pbkdf2-hmac-sha512(password []byte, salt []byte, iterations i64, key-len i64) (out []byte)
out = pbkdf2.pbkdf2-hmac-sha1(password []byte, salt []byte, iterations i64, key-len i64) (out []byte)
```

### crypto/argon2 — Argon2 메모리 하드 키 유도

```no
#{fmt-skip}
argon2.id(password []byte, pw-n i64, salt []byte, salt-n i64, time i64, memory i64, parallel i64, out []byte, out-n i64)
```

### crypto/scrypt — scrypt 키 유도

```no
#{fmt-skip}
scrypt.scrypt(password []byte, pw-n i64, salt []byte, salt-n i64, n i64, r i64, p i64, out []byte, out-n i64)
```

### crypto/sha224 — SHA-224 (224-bit)

```no
#{fmt-skip}
hash = sha224.sha224(data []byte) (hash [28]byte)
hex = sha224.sha224-hex(data []byte) (hex str)
```

### crypto/sha384 — SHA-384 (384-bit)

```no
#{fmt-skip}
hash = sha384.sha384(data []byte) (hash [48]byte)
hex = sha384.sha384-hex(data []byte) (hex str)
```

### crypto/sha3 — SHA-3 (Keccak)

```no
#{fmt-skip}
hash = sha3.sha3-256(data []byte) (hash [32]byte)
hash = sha3.sha3-512(data []byte) (hash [64]byte)
```

### crypto/blake2 — BLAKE2 해시

```no
#{fmt-skip}
hash = blake2.b(data []byte) (hash [64]byte)
hash = blake2.b-256(data []byte) (hash [32]byte)
hex = blake2.b-hex(data []byte) (hex str)
hash = blake2.s(data []byte) (hash [32]byte)
hex = blake2.s-hex(data []byte) (hex str)
```

### crypto/crc-16 — CRC16 검사

```no
#{fmt-skip}
crc = crc-16.new(data []byte, n i64) (crc i64)
```

### crypto/crc-64 — CRC64 검사

```no
#{fmt-skip}
crc = crc-64.new(data []byte, n i64) (crc i64)
```

### crypto/fnv — FNV-1 해시

```no
#{fmt-skip}
h = fnv.fnv-1a-32(data []byte) (hash i64)
h = fnv.fnv-1a-64(data []byte) (hash u64)
```

### crypto/base32 — Base32 인코딩/디코딩 (RFC 4648)

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

### crypto/rc4 — RC4 스트림 암호화

```no
#{fmt-skip}
out = rc4.rc4(key []byte, key-n i64, data []byte, data-n i64) (out []byte)
```

### crypto/tdes — 트리플 DES (3DES)

```no
#{fmt-skip}
tdes.tdes-enc(plain, 8, key [24]byte, out)
tdes.tdes-dec(cipher, 8, key [24]byte, out)
```

### crypto/ecdsa — ECDSA 디지털 서명

```no
#{fmt-skip}
ok = ecdsa.ecdsa-sign(priv-key []byte, msg []byte, msg-n i64, r []byte, s []byte)
ok = ecdsa.ecdsa-verify(pub-key []byte, msg []byte, msg-n i64, r []byte, s []byte) (ok bool)
```

### crypto/ed25519 — Ed25519 디지털 서명

```no
#{fmt-skip}
pub = ed25519.ed25519-publickey(seed [32]byte) (pub [32]byte)
sig = ed25519.ed25519-sign(seed [32]byte, msg []byte) (sig [64]byte)
ok = ed25519.ed25519-verify(pub [32]byte, msg []byte, sig [64]byte) (ok i64)
```

### crypto/x25519 — X25519 키 교환

```no
#{fmt-skip}
priv, pub = x25519.x25519-gen-keypair(seed i64) (priv [32]byte, pub [32]byte)
pub = x25519.x25519-scalarmult-base-bytes(scalar []byte) (out []byte)
shared = x25519.x25519-scalarmult-bytes(scalar []byte, point []byte) (out []byte)
out = x25519.x25519-scalarmult(scalar [32]byte, point [32]byte) (out [32]byte)
```

---
