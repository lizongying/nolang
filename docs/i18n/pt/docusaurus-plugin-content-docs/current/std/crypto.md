---
sidebar_position: 4.0
---

## Criptografia e hashing

### crypto/aes — núcleo de cifra de bloco AES (AES-128 / AES-256)

```no
#{fmt-skip}
; Bloco único (expande a chave internamente)
out = aes.enc-128(in [16]byte, key [16]byte)   ; encriptação AES-128
out = aes.dec-128(in [16]byte, key [16]byte)   ; desencriptação AES-128
out = aes.enc-256(in [16]byte, key [32]byte)   ; encriptação AES-256
out = aes.dec-256(in [16]byte, key [32]byte)   ; desencriptação AES-256

; Vários blocos：expande a chave uma vez e depois processa bloco a bloco
ek []byte = aes.key-expand(key [16]byte)       ; chaves de ronda de 176 bytes
ek []byte = aes.key-expand-256(key [32]byte)   ; chaves de ronda de 240 bytes
out = aes.enc-block(in [16]byte, ek)
out = aes.dec-block(in [16]byte, ek)
```

O número de rondas é derivado do comprimento de `ek` (`nr = ek.len() / 16 - 1`), pelo que AES-128 e AES-256 partilham o mesmo ciclo de rondas.
Os modos de operação estão em `crypto/aes-cbc`, `crypto/aes-ctr` e `crypto/aes-gcm`.

### crypto/des — encriptação/desencriptação DES (modo ECB)

```no
des.des-enc(plain, 8, key, out); encripta um bloco de 8 bytes
des.des-dec(cipher, 8, key, out); desencripta um bloco de 8 bytes
```

### crypto/rsa — encriptação/desencriptação RSA (exponenciação modular de multiprecisão)

```no
; Interface de bytes de alto nível (recomendada)：a cadeia de bytes é interpretada como inteiro não assinado big-endian, a saída é a codificação big-endian mais curta
cipher = rsa.rsa-encrypt-bytes(msg, pub-exp, mod); c = m^e mod n
plain = rsa.rsa-decrypt-bytes(cipher, priv-exp, mod); m = c^d mod n

; Interface de limbs de baixo nível (base 2^32 little-endian, contagem de limbs gerida manualmente)
result, rn = rsa.rsa-modpow(base, bn, exp, en, mod, mn)
```

Não inclui geração de chaves (o chamador fornece n, e, d) e suporta 1024~4096 bits. Os símbolos com prefixo underscore para wrappers de limbs / utilitários de grandes números são internos ao módulo e rejeitados noutros módulos (a guarda private_std reporta ERROR).

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

`sha1` calcula o hash completo (incluindo preenchimento e processamento de vários blocos) e retorna 20 bytes.
`sha1-hex` faz o mesmo, mas retorna uma string hex de 40 caracteres em minúsculas.
`sha1-block` é uma API de baixo nível que processa um único bloco de 512 bits.

### crypto/sha256 — SHA-256 (256-bit)

```no
#{fmt-skip}
sha256.sha256(data []byte) (hash [32]byte)
sha256.sha256-hex(data []byte) (hex str)
sha256.sha256-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32, h5 u32, h6 u32, h7 u32)
```

`sha256` calcula o hash completo (incluindo preenchimento e processamento de vários blocos) e retorna 32 bytes.
`sha256-hex` faz o mesmo, mas retorna uma string hex de 64 caracteres em minúsculas.
`sha256-block` é uma API de baixo nível que processa um único bloco de 512 bits.

### crypto/sha512 — SHA-512 (512-bit)

```no
#{fmt-skip}
sha512.sha512(data []byte) (hash [64]byte)
sha512.sha512-hex(data []byte) (hex str)
sha512.sha512-block(s []u64, h0 u64, h1 u64, h2 u64, h3 u64, h4 u64, h5 u64, h6 u64, h7 u64)
```

`sha512` calcula o hash completo (incluindo preenchimento e processamento de vários blocos) e retorna 64 bytes.
`sha512-hex` faz o mesmo, mas retorna uma string hex de 128 caracteres em minúsculas.
`sha512-block` é uma API de baixo nível que processa um único bloco de 1024 bits.

### crypto/crc-32 — checksum CRC32

```no
#{fmt-skip}
crc-32.new(s []byte, n, crc)
```

### crypto/fnv-1a-32 — hash não criptográfico FNV-1a

```no
#{fmt-skip}
fnv-1a-32.fnv-1a-32(s []byte, n, h)
```

### crypto/rand — gerador de números aleatórios (xorshift32)

```no
r = rand.rand(state); número pseudoaleatório de 32 bits
rand.rand-str(state, n, s); string alfanumérica aleatória
```

### crypto/x509 — análise DER de certificados X.509

```no
#{fmt-skip}
tag = x509.der-tag(data, pos)
len, adv = x509.der-len(data, pos)
x509.x509-fingerprint(cert, n, h0..h7)  ; impressão digital SHA-256 do certificado
x509.x509-rsa-e(cert, n, e)             ; extração do expoente da chave pública RSA
```

### crypto/aes-cbc — modo AES-CBC (com preenchimento PKCS7, AES-128 / AES-256)

```no
#{fmt-skip}
out = aes-cbc.enc-128(in []byte, key [16]byte, iv [16]byte)
out = aes-cbc.dec-128(in []byte, key [16]byte, iv [16]byte)
out = aes-cbc.enc-256(in []byte, key [32]byte, iv [16]byte)
out = aes-cbc.dec-256(in []byte, key [32]byte, iv [16]byte)
out = aes-cbc.pkcs7-pad(in []byte)
n = aes-cbc.pkcs7-unpad(in []byte)
```

### crypto/aes-ctr — modo contador AES-CTR (AES-128 / AES-256)

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

### crypto/hmac — código de autenticação de mensagem HMAC

```no
#{fmt-skip}
out = hmac.hmac-sha256(key []byte, msg []byte) (out [32]byte)
out = hmac.hmac-sha512(key []byte, msg []byte) (out [64]byte)
out = hmac.hmac-sha384(key []byte, msg []byte) (out [48]byte)
out = hmac.hmac-sha1(key []byte, msg []byte) (out [20]byte)
out = hmac.hmac-md5(key []byte, msg []byte) (out [16]byte)
vec = hmac.hmac-sha256-vec(key []byte, msg []byte) (out []byte)
```

### crypto/hkdf — derivação de chave HKDF (RFC 5869)

```no
#{fmt-skip}
ok = hkdf.hkdf-extract(salt []byte, salt-n i64, ikm []byte, ikm-n i64, prk []byte)
ok = hkdf.hkdf-expand(prk []byte, prk-n i64, info []byte, info-n i64, out []byte, out-n i64)
```

### crypto/pbkdf2 — derivação de chave PBKDF2 (RFC 2898)

```no
#{fmt-skip}
out = pbkdf2.pbkdf2-hmac-sha256(password []byte, salt []byte, iterations i64, key-len i64) (out []byte)
out = pbkdf2.pbkdf2-hmac-sha512(password []byte, salt []byte, iterations i64, key-len i64) (out []byte)
out = pbkdf2.pbkdf2-hmac-sha1(password []byte, salt []byte, iterations i64, key-len i64) (out []byte)
```

### crypto/argon2 — derivação de chave Argon2 resistente a memória

```no
#{fmt-skip}
argon2.id(password []byte, pw-n i64, salt []byte, salt-n i64, time i64, memory i64, parallel i64, out []byte, out-n i64)
```

### crypto/scrypt — derivação de chave scrypt

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

### crypto/blake2 — hash BLAKE2

```no
#{fmt-skip}
hash = blake2.b(data []byte) (hash [64]byte)
hash = blake2.b-256(data []byte) (hash [32]byte)
hex = blake2.b-hex(data []byte) (hex str)
hash = blake2.s(data []byte) (hash [32]byte)
hex = blake2.s-hex(data []byte) (hex str)
```

### crypto/crc-16 — checksum CRC16

```no
#{fmt-skip}
crc = crc-16.new(data []byte, n i64) (crc i64)
```

### crypto/crc-64 — checksum CRC64

```no
#{fmt-skip}
crc = crc-64.new(data []byte, n i64) (crc i64)
```

### crypto/fnv — hash FNV-1

```no
#{fmt-skip}
h = fnv.fnv-1a-32(data []byte) (hash i64)
h = fnv.fnv-1a-64(data []byte) (hash u64)
```

### crypto/base32 — codificação/descodificação Base32 (RFC 4648)

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

### crypto/rc4 — cifra de fluxo RC4

```no
#{fmt-skip}
out = rc4.rc4(key []byte, key-n i64, data []byte, data-n i64) (out []byte)
```

### crypto/tdes — DES triplo (3DES)

```no
#{fmt-skip}
tdes.tdes-enc(plain, 8, key [24]byte, out)
tdes.tdes-dec(cipher, 8, key [24]byte, out)
```

### crypto/ecdsa — assinatura digital ECDSA

```no
#{fmt-skip}
ok = ecdsa.ecdsa-sign(priv-key []byte, msg []byte, msg-n i64, r []byte, s []byte)
ok = ecdsa.ecdsa-verify(pub-key []byte, msg []byte, msg-n i64, r []byte, s []byte) (ok bool)
```

### crypto/ed25519 — assinatura digital Ed25519

```no
#{fmt-skip}
pub = ed25519.ed25519-publickey(seed [32]byte) (pub [32]byte)
sig = ed25519.ed25519-sign(seed [32]byte, msg []byte) (sig [64]byte)
ok = ed25519.ed25519-verify(pub [32]byte, msg []byte, sig [64]byte) (ok i64)
```

### crypto/x25519 — troca de chaves X25519

```no
#{fmt-skip}
priv, pub = x25519.x25519-gen-keypair(seed i64) (priv [32]byte, pub [32]byte)
pub = x25519.x25519-scalarmult-base-bytes(scalar []byte) (out []byte)
shared = x25519.x25519-scalarmult-bytes(scalar []byte, point []byte) (out []byte)
out = x25519.x25519-scalarmult(scalar [32]byte, point [32]byte) (out [32]byte)
```

---
