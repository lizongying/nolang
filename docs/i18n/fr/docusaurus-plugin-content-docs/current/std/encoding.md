---
sidebar_position: 3.8
---

## Encodage

### encoding/hex — Hexadécimal

```no
; Encodage (défini dans le module byte)
out = data.to-hex(); []byte → chaîne hex majuscule
out = data.to-hex-lower(); []byte → chaîne hex minuscule

; Décodage (défini dans le module str)
out = s.from-hex(); chaîne hex → ?[]byte (nil=vide, err=caractère invalide)
```

### encoding/base64 — Base64 (RFC 4648)

```no
BASE64-STD = 'ABC...+/'
BASE64-URL = 'ABC...-_'
PAD = 61; '='

out-n = base64.encode(data, n, table, out); encodage Base64
out-n = base64.encode-std(data, n, out); encodage standard
out-n = base64.encode-url(data, n, out); encodage sûr pour URL
out-n = base64.decode(s, n, table, out); décodage Base64 (?i64, nil=entrée invalide)
```

### encoding/csv — Analyse CSV (RFC 4180)

```no
fn, new-pos = csv.parse-field(s, sn, pos, field); analyse un seul champ
fields = csv.parse-line(s, nil); analyse une ligne (nil → limite 1024)
out-n = csv.encode-field(field, fn, out); encode un champ
```

### encoding/pem — Encodage/décodage PEM (RFC 7468)

Le format PEM est largement utilisé pour les certificats X.509, les clés RSA/ECDSA, etc.

```no
; Structure
block {
    label str
    data []byte
}

; Encodage
out = pem.encode(label, data); encode des octets bruts en chaîne PEM

; Décodage
result = pem.decode(pem-str); analyse une chaîne PEM (?block, nil=échec de l'analyse)
; en cas de succès, on peut accéder à result.label et result.data
```

---
