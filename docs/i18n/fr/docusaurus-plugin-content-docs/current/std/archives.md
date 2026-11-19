---
sidebar_position: 3.9
---

## Archives

### archive/tar — Archive TAR (POSIX ustar)

```no
; Lire un tar ordinaire
archive = tar {
    data: raw-bytes
}
count = archive.count()
e = archive.entry(idx)
name = archive.name(idx)
sz = archive.size(idx)
typ = archive.type(idx); "file" / "dir" / "unknown"
yes = archive.is-dir(idx)
yes = archive.is-file(idx)
out = archive.read(idx)
mode = archive.mode(idx)
ts = archive.mtime(idx)

; Lire un .tar.gz (décompression automatique)
archive = tar.open-gz(gz-data)

; Méthodes de entry
name = e.name()
sz = e.size()
typ = e.type()
out = e.read()

; Écrire un tar
builder = builder {}
builder.add-file(name, content)
builder.add-dir(name)
archive = builder.finish()
```

### archive/zip — Analyse d'archives ZIP

```no
archive = zip {
    data: raw-bytes
}
count = archive.count(); nombre d'entrées
e = archive.entry(idx); obtient une entry
name = archive.name(idx); nom de fichier
sz = archive.size(idx); taille d'origine
csz = archive.compressed-size(idx); taille compressée
method = archive.method(idx); 0=stored, 8=deflate
out = archive.extract(idx); modes stored et deflate

; Méthodes de entry
name = e.name()
sz = e.size()
csz = e.compressed-size()
method = e.method()
out = e.extract()
```

### archive/gzip — Compression GZIP et DEFLATE brut

```no
out = gzip.compress(data); compression zlib
out = gzip.decompress(data); décompression zlib
out = gzip.inflate-decompress(data, out-size); décompression DEFLATE brute (ZIP method 8)
```

### archive/bzip2 — Décompression BZIP2 (implémentation pure Nolang)

Implémentation pure Nolang de la décompression BZIP2, comprenant la transformation inverse BWT, la transformation inverse MTF, le décodage Huffman et le décodage RLE :

```no
out = bzip2.decompress(data); décompresse des données .bz2
```

### archive/xz — Décompression XZ/LZMA (implémentation pure Nolang)

Implémentation pure Nolang de la décompression LZMA2, prenant en charge le conteneur .xz et le format .lzma classique :

```no
out = xz.decompress(data); décompresse le format .xz
out = xz.lzma-decompress(data); décompresse le format .lzma classique
```

### archive/zlib — Compression/décompression zlib (RFC 1950, implémentation pure Nolang)

Format de flux zlib : en-tête de 2 octets + DEFLATE brut + somme de contrôle Adler-32 sur 4 octets :

```no
out = zlib.zlib-compress(data); compresse au format zlib (blocs stored)
out = zlib.zlib-decompress(data); décompresse le format zlib (retourne ?[]byte)
sum = zlib.adler-32(data, n); calcule la somme de contrôle Adler-32
```

### archive/zstd — Décompression Zstandard (implémentation pure Nolang)

Implémentation pure Nolang de la décompression Zstandard (zstd), comprenant le décodage FSE, le décodage Huffman et le décodage des séquences LZ77 :

```no
out = zstd.decompress(data); décompresse le format .zst
```

---
