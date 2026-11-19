---
sidebar_position: 4.0
---

## Cryptographie et hachage

### crypto/aes — Noyau de chiffrement par blocs AES (AES-128 / AES-256)

```no
#{fmt-skip}
; Bloc unique (développe la clé lui-même)
out = aes.enc-128(in [16]byte, key [16]byte)   ; chiffrement AES-128
out = aes.dec-128(in [16]byte, key [16]byte)   ; déchiffrement AES-128
out = aes.enc-256(in [16]byte, key [32]byte)   ; chiffrement AES-256
out = aes.dec-256(in [16]byte, key [32]byte)   ; déchiffrement AES-256

; Plusieurs blocs : la clé n'est développée qu'une fois, puis chiffrement/déchiffrement bloc par bloc
ek []byte = aes.key-expand(key [16]byte)       ; clés de tour de 176 octets
ek []byte = aes.key-expand-256(key [32]byte)   ; clés de tour de 240 octets
out = aes.enc-block(in [16]byte, ek)
out = aes.dec-block(in [16]byte, ek)
```

Le nombre de tours est déduit de la longueur de `ek` (`nr = ek.len() / 16 - 1`) ; AES-128 et AES-256 partagent la même boucle de tours.
Les modes de fonctionnement se trouvent dans `crypto/aes-cbc`, `crypto/aes-ctr` et `crypto/aes-gcm`.

### crypto/des — Chiffrement/déchiffrement DES (mode ECB)

```no
des.des-enc(plain, 8, key, out); chiffre un bloc de 8 octets
des.des-dec(cipher, 8, key, out); déchiffre un bloc de 8 octets
```

### crypto/rsa — Chiffrement/déchiffrement RSA (exponentiation modulaire multiprécision)

```no
; Interface octets de haut niveau (recommandée) : les chaînes d'octets sont interprétées comme des entiers non signés big-endian, la sortie est l'encodage big-endian minimal
cipher = rsa.rsa-encrypt-bytes(msg, pub-exp, mod); c = m^e mod n
plain = rsa.rsa-decrypt-bytes(cipher, priv-exp, mod); m = c^d mod n

; Interface limb de bas niveau (base 2^32 little-endian, comptage des limbs manuel)
result, rn = rsa.rsa-modpow(base, bn, exp, en, mod, mn)
```

La génération de clés n'est pas incluse (l'appelant fournit n, e, d) ; prend en charge 1024~4096 bits. Les symboles commençant par un tiret bas (wrappers de limbs / utilitaires grands nombres) sont internes au module et interdits depuis un autre module (la garde `private_std` signale une ERREUR).

### crypto/md5 — MD5 (128 bits)

```no
out [16]byte = md5.md5(data)
```

### crypto/sha1 — SHA-1 (160 bits)

```no
#{fmt-skip}
hash = sha1.sha1(data []byte) (hash [20]byte)
hex = sha1.sha1-hex(data []byte) (hex str)
sha1.sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32)
```

`sha1` calcule le hachage complet (y compris le remplissage et le traitement de plusieurs blocs) et retourne 20 octets.
`sha1-hex` fait de même mais retourne une chaîne hexadécimale minuscule de 40 caractères.
`sha1-block` est une API de bas niveau qui traite un seul bloc de 512 bits.

### crypto/sha256 — SHA-256 (256 bits)

```no
#{fmt-skip}
sha256.sha256(data []byte) (hash [32]byte)
sha256.sha256-hex(data []byte) (hex str)
sha256.sha256-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32, h5 u32, h6 u32, h7 u32)
```

`sha256` calcule le hachage complet (y compris le remplissage et le traitement de plusieurs blocs) et retourne 32 octets.
`sha256-hex` fait de même mais retourne une chaîne hexadécimale minuscule de 64 caractères.
`sha256-block` est une API de bas niveau qui traite un seul bloc de 512 bits.

### crypto/sha512 — SHA-512 (512 bits)

```no
#{fmt-skip}
sha512.sha512(data []byte) (hash [64]byte)
sha512.sha512-hex(data []byte) (hex str)
sha512.sha512-block(s []u64, h0 u64, h1 u64, h2 u64, h3 u64, h4 u64, h5 u64, h6 u64, h7 u64)
```

`sha512` calcule le hachage complet (y compris le remplissage et le traitement de plusieurs blocs) et retourne 64 octets.
`sha512-hex` fait de même mais retourne une chaîne hexadécimale minuscule de 128 caractères.
`sha512-block` est une API de bas niveau qui traite un seul bloc de 1024 bits.

### crypto/crc-32 — Somme de contrôle CRC32

```no
#{fmt-skip}
crc-32.new(s []byte, n, crc)
```

### crypto/fnv-1a-32 — Hachage non cryptographique FNV-1a

```no
#{fmt-skip}
fnv-1a-32.fnv-1a-32(s []byte, n, h)
```

### crypto/rand — Générateur de nombres aléatoires (xorshift32)

```no
r = rand.rand(state); nombre pseudo-aléatoire 32 bits
rand.rand-str(state, n, s); chaîne alphanumérique aléatoire
```

### crypto/x509 — Analyse DER de certificats X.509

```no
#{fmt-skip}
tag = x509.der-tag(data, pos)
len, adv = x509.der-len(data, pos)
x509.x509-fingerprint(cert, n, h0..h7)  ; empreinte SHA-256 du certificat
x509.x509-rsa-e(cert, n, e)             ; extraction de l'exposant public RSA
```

### crypto/aes-cbc — Mode AES-CBC (avec remplissage PKCS7, AES-128 / AES-256)

```no
#{fmt-skip}
out = aes-cbc.enc-128(in []byte, key [16]byte, iv [16]byte)
out = aes-cbc.dec-128(in []byte, key [16]byte, iv [16]byte)
out = aes-cbc.enc-256(in []byte, key [32]byte, iv [16]byte)
out = aes-cbc.dec-256(in []byte, key [32]byte, iv [16]byte)
out = aes-cbc.pkcs7-pad(in []byte)
n = aes-cbc.pkcs7-unpad(in []byte)
```

### crypto/aes-ctr — Mode compteur AES-CTR (AES-128 / AES-256)

```no
#{fmt-skip}
out = aes-ctr.crypt-128(in []byte, key [16]byte, iv [16]byte)
out = aes-ctr.crypt-256(in []byte, key [32]byte, iv [16]byte)
```

### crypto/aes-gcm — AEAD AES-GCM (NIST SP 800-38D, AES-128 / AES-256)

```no
#{fmt-skip}
sealed = aes-gcm.seal-128(key [16]byte, iv [12]byte, aad []byte, plain []byte)
plain = aes-gcm.open-128(key [16]byte, iv [12]byte, aad []byte, sealed []byte)
sealed = aes-gcm.seal-256(key [32]byte, iv [12]byte, aad []byte, plain []byte)
plain = aes-gcm.open-256(key [32]byte, iv [12]byte, aad []byte, sealed []byte)
```

### crypto/hmac — Code d'authentification de message HMAC

```no
#{fmt-skip}
out = hmac.hmac-sha256(key []byte, msg []byte) (out [32]byte)
out = hmac.hmac-sha512(key []byte, msg []byte) (out [64]byte)
out = hmac.hmac-sha384(key []byte, msg []byte) (out [48]byte)
out = hmac.hmac-sha1(key []byte, msg []byte) (out [20]byte)
out = hmac.hmac-md5(key []byte, msg []byte) (out [16]byte)
vec = hmac.hmac-sha256-vec(key []byte, msg []byte) (out []byte)
```

### crypto/hkdf — Dérivation de clé HKDF (RFC 5869)

```no
#{fmt-skip}
ok = hkdf.hkdf-extract(salt []byte, salt-n i64, ikm []byte, ikm-n i64, prk []byte)
ok = hkdf.hkdf-expand(prk []byte, prk-n i64, info []byte, info-n i64, out []byte, out-n i64)
```

### crypto/pbkdf2 — Dérivation de clé PBKDF2 (RFC 2898)

```no
#{fmt-skip}
out = pbkdf2.pbkdf2-hmac-sha256(password []byte, salt []byte, iterations i64, key-len i64) (out []byte)
out = pbkdf2.pbkdf2-hmac-sha512(password []byte, salt []byte, iterations i64, key-len i64) (out []byte)
out = pbkdf2.pbkdf2-hmac-sha1(password []byte, salt []byte, iterations i64, key-len i64) (out []byte)
```

### crypto/argon2 — Dérivation de clé Argon2 résistante à la mémoire

```no
#{fmt-skip}
argon2.id(password []byte, pw-n i64, salt []byte, salt-n i64, time i64, memory i64, parallel i64, out []byte, out-n i64)
```

### crypto/scrypt — Dérivation de clé scrypt

```no
#{fmt-skip}
scrypt.scrypt(password []byte, pw-n i64, salt []byte, salt-n i64, n i64, r i64, p i64, out []byte, out-n i64)
```

### crypto/sha224 — SHA-224 (224 bits)

```no
#{fmt-skip}
hash = sha224.sha224(data []byte) (hash [28]byte)
hex = sha224.sha224-hex(data []byte) (hex str)
```

### crypto/sha384 — SHA-384 (384 bits)

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

### crypto/blake2 — Hachage BLAKE2

```no
#{fmt-skip}
hash = blake2.b(data []byte) (hash [64]byte)
hash = blake2.b-256(data []byte) (hash [32]byte)
hex = blake2.b-hex(data []byte) (hex str)
hash = blake2.s(data []byte) (hash [32]byte)
hex = blake2.s-hex(data []byte) (hex str)
```

### crypto/crc-16 — Somme de contrôle CRC16

```no
#{fmt-skip}
crc = crc-16.new(data []byte, n i64) (crc i64)
```

### crypto/crc-64 — Somme de contrôle CRC64

```no
#{fmt-skip}
crc = crc-64.new(data []byte, n i64) (crc i64)
```

### crypto/fnv — Hachage FNV-1

```no
#{fmt-skip}
h = fnv.fnv-1a-32(data []byte) (hash i64)
h = fnv.fnv-1a-64(data []byte) (hash u64)
```

### crypto/base32 — Encodage/décodage Base32 (RFC 4648)

```no
#{fmt-skip}
out = base32.encode(data []byte) (out str)
out = base32.decode(s str) (out []byte)
```

### crypto/chacha20-poly1305 — AEAD ChaCha20-Poly1305

```no
#{fmt-skip}
sealed = chacha20-poly1305.chacha20-poly1305-seal(key [32]byte, nonce [12]byte, aad []byte, plain []byte)
plain = chacha20-poly1305.chacha20-poly1305-open(key [32]byte, nonce [12]byte, aad []byte, sealed []byte)
```

### crypto/rc4 — Chiffrement de flux RC4

```no
#{fmt-skip}
out = rc4.rc4(key []byte, key-n i64, data []byte, data-n i64) (out []byte)
```

### crypto/tdes — Triple DES (3DES)

```no
#{fmt-skip}
tdes.tdes-enc(plain, 8, key [24]byte, out)
tdes.tdes-dec(cipher, 8, key [24]byte, out)
```

### crypto/ecdsa — Signature numérique ECDSA

```no
#{fmt-skip}
ok = ecdsa.ecdsa-sign(priv-key []byte, msg []byte, msg-n i64, r []byte, s []byte)
ok = ecdsa.ecdsa-verify(pub-key []byte, msg []byte, msg-n i64, r []byte, s []byte) (ok bool)
```

### crypto/ed25519 — Signature numérique Ed25519

```no
#{fmt-skip}
pub = ed25519.ed25519-publickey(seed [32]byte) (pub [32]byte)
sig = ed25519.ed25519-sign(seed [32]byte, msg []byte) (sig [64]byte)
ok = ed25519.ed25519-verify(pub [32]byte, msg []byte, sig [64]byte) (ok i64)
```

### crypto/x25519 — Échange de clés X25519

```no
#{fmt-skip}
priv, pub = x25519.x25519-gen-keypair(seed i64) (priv [32]byte, pub [32]byte)
pub = x25519.x25519-scalarmult-base-bytes(scalar []byte) (out []byte)
shared = x25519.x25519-scalarmult-bytes(scalar []byte, point []byte) (out []byte)
out = x25519.x25519-scalarmult(scalar [32]byte, point [32]byte) (out [32]byte)
```

---
