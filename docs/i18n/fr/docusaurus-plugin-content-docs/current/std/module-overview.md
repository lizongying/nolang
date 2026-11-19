---
sidebar_position: 4.3
---

## Aperçu des modules

| Module | Chemin | Description |
| ------ | ------ | ----------- |
| fmt | Noyau | Sortie formatée |
| math | Noyau | Fonctions mathématiques |
| str | Noyau | Opérations sur les chaînes |
| vec | Noyau | Opérations sur les tranches ([]t) |
| arr | Noyau | Opérations sur les tableaux ([n]t) |
| number | Noyau | Fonctions utilitaires numériques |
| byte | Noyau | Opérations sur les octets |
| char | Noyau | Opérations sur les caractères (méthodes) |
| os | Noyau | Interface du système d'exploitation |
| env | Noyau | Encapsulation des variables d'environnement |
| fs | Noyau | Utilitaires du système de fichiers |
| io | Noyau | Abstraction des entrées/sorties |
| args | Noyau | Arguments de la ligne de commande |
| path | Noyau | Traitement des chemins (structure) |
| bufio | Noyau | Lecture bufferisée |
| time | Noyau | Opérations sur l'heure |
| log | Noyau | Journalisation par niveaux |
| json | Noyau | Analyse/génération JSON |
| toml | Noyau | Analyse/génération TOML 1.0 |
| yaml | Noyau | Analyse/génération YAML 1.2 |
| types | Noyau | Document de définitions des types |
| sort | Noyau | Constantes de tri |
| set | Noyau | Ensemble |
| deque | Noyau | File à deux extrémités (structure) |
| heap | Noyau | Tas minimal (structure) |
| stack | Noyau | Pile (structure) |
| regexp | Noyau | Expressions régulières |
| process | Noyau | Opérations sur les processus |
| uuid | Noyau | UUID v4 |
| bigint | Noyau | Entier de précision arbitraire |
| bool | Noyau | Type booléen |
| err | Noyau | Gestion des erreurs |
| enter | Noyau | Hook de démarrage |
| leave | Noyau | Hook de sortie |
| async | Noyau | Coroutines asynchrones / annulation |
| global | Noyau | Fonctions intégrées globales |
| magic | Noyau | Détection du type de fichier |
| net | Noyau | Opérations réseau TCP |
| net/http | Sous-module | Client HTTP/1.1 |
| net/http2 | Sous-module | Client HTTP/2.0 |
| net/http3 | Sous-module | Client HTTP/3.0 |
| net/ws | Sous-module | WebSocket |
| net/quic | Sous-module | Protocole QUIC |
| net/tls | Sous-module | TLS 1.2/1.3 |
| net/sse | Sous-module | Client SSE |
| net/client | Sous-module | Client TCP de haut niveau |
| net/server | Sous-module | Serveur HTTP |
| net/dns | Sous-module | Résolution DNS |
| net/url | Sous-module | Analyse d'URL |
| net/cookie | Sous-module | Cookie HTTP |
| net/multipart | Sous-module | Formulaires Multipart |
| net/hpack | Sous-module | Compression d'en-têtes HPACK |
| net/proxy | Sous-module | Prise en charge des proxies |
| net/pool | Sous-module | Pool de connexions |
| net/unix | Sous-module | Sockets de domaine Unix |
| net/ip | Sous-module | Opérations sur les adresses IP |
| encoding/base64 | Sous-module | Encodage/décodage Base64 |
| encoding/csv | Sous-module | Analyse CSV |
| encoding/pem | Sous-module | Encodage/décodage PEM |
| archive/tar | Sous-module | Archive TAR |
| archive/zip | Sous-module | Archive ZIP |
| archive/gzip | Sous-module | Compression GZIP |
| archive/bzip2 | Sous-module | Décompression BZIP2 |
| archive/xz | Sous-module | Décompression XZ/LZMA |
| archive/zlib | Sous-module | Compression zlib (RFC 1950) |
| archive/zstd | Sous-module | Décompression Zstandard |
| map/linked-hash-map | Sous-module | Table de hachage ordonnée |
| map/hash-set | Sous-module | Ensemble de hachage i64 |
| map/str-map | Sous-module | Table de hachage str→str |
| map/str-set | Sous-module | Ensemble de hachage str |
| map/tree-map | Sous-module | Table ordonnée AVL |
| map/tree-set | Sous-module | Ensemble ordonné AVL |
| collection/queue | Sous-module | File générique |
| collection/arr-stack | Sous-module | Pile générique |
| collection/link | Sous-module | Liste doublement chaînée générique |
| collection/map | Sous-module | Table de hachage dynamique générique |
| collection/static-hashmap | Sous-module | Table de hachage générique à capacité fixe |
| database/sql | Sous-module | Interface d'accès à la base de données |
| crypto/aes | Sous-module | Noyau de bloc AES (128/256) |
| crypto/aes-cbc | Sous-module | Mode AES-CBC (128/256) |
| crypto/aes-ctr | Sous-module | Mode AES-CTR (128/256) |
| crypto/aes-gcm | Sous-module | AEAD AES-GCM (128/256) |
| crypto/des | Sous-module | Chiffrement/déchiffrement DES |
| crypto/tdes | Sous-module | Triple DES |
| crypto/rsa | Sous-module | Exponentiation modulaire RSA |
| crypto/md5 | Sous-module | Hachage MD5 |
| crypto/sha1 | Sous-module | Hachage SHA-1 |
| crypto/sha224 | Sous-module | Hachage SHA-224 |
| crypto/sha256 | Sous-module | Hachage SHA-256 |
| crypto/sha384 | Sous-module | Hachage SHA-384 |
| crypto/sha512 | Sous-module | Hachage SHA-512 |
| crypto/sha3 | Sous-module | Hachage SHA-3 |
| crypto/blake2 | Sous-module | Hachage BLAKE2 |
| crypto/crc-16 | Sous-module | Somme de contrôle CRC16 |
| crypto/crc-32 | Sous-module | Somme de contrôle CRC32 |
| crypto/crc-64 | Sous-module | Somme de contrôle CRC64 |
| crypto/fnv | Sous-module | Hachage FNV-1 |
| crypto/fnv-1a-32 | Sous-module | Hachage FNV-1a |
| crypto/hmac | Sous-module | Code d'authentification HMAC |
| crypto/hkdf | Sous-module | Dérivation de clé HKDF |
| crypto/pbkdf2 | Sous-module | Dérivation de clé PBKDF2 |
| crypto/argon2 | Sous-module | Dérivation de clé Argon2 |
| crypto/scrypt | Sous-module | Dérivation de clé scrypt |
| crypto/chacha20-poly1305 | Sous-module | ChaCha20-Poly1305 |
| crypto/rc4 | Sous-module | Chiffrement de flux RC4 |
| crypto/ecdsa | Sous-module | Signature ECDSA |
| crypto/ed25519 | Sous-module | Signature Ed25519 |
| crypto/x25519 | Sous-module | Échange de clés X25519 |
| crypto/base32 | Sous-module | Encodage/décodage Base32 |
| crypto/rand | Sous-module | Générateur de nombres aléatoires |
| crypto/x509 | Sous-module | Analyse X.509 DER |
