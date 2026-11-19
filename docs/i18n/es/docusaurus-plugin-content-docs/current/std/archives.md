---
sidebar_position: 3.9
---

## Archivos comprimidos

### archive/tar — Archivo TAR (POSIX ustar)

```no
; Leer un tar normal
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

; Leer .tar.gz (descomprimir automáticamente)
archive = tar.open-gz(gz-data)

; Métodos de entry
name = e.name()
sz = e.size()
typ = e.type()
out = e.read()

; Escribir tar
builder = builder {}
builder.add-file(name, content)
builder.add-dir(name)
archive = builder.finish()
```

### archive/zip — Análisis de archivos ZIP

```no
archive = zip {
    data: raw-bytes
}
count = archive.count(); Número de entradas
e = archive.entry(idx); Obtener entry
name = archive.name(idx); Nombre de archivo
sz = archive.size(idx); Tamaño original
csz = archive.compressed-size(idx); Tamaño comprimido
method = archive.method(idx); 0=stored, 8=deflate
out = archive.extract(idx); modos stored y deflate

; Métodos de entry
name = e.name()
sz = e.size()
csz = e.compressed-size()
method = e.method()
out = e.extract()
```

### archive/gzip — Compresión GZIP y DEFLATE sin procesar

```no
out = gzip.compress(data); Compresión zlib
out = gzip.decompress(data); Descompresión zlib
out = gzip.inflate-decompress(data, out-size); Descompresión DEFLATE sin procesar (ZIP method 8)
```

### archive/bzip2 — Descompresión BZIP2 (implementación pura en Nolang)

Implementación pura en Nolang de la descompresión BZIP2, que incluye la transformada inversa BWT, la transformada inversa MTF, la decodificación Huffman y la decodificación RLE:

```no
out = bzip2.decompress(data); Descomprimir datos .bz2
```

### archive/xz — Descompresión XZ/LZMA (implementación pura en Nolang)

Implementación pura en Nolang de la descompresión LZMA2, compatible con el contenedor .xz y el formato .lzma tradicional:

```no
out = xz.decompress(data); Descomprimir formato .xz
out = xz.lzma-decompress(data); Descomprimir formato .lzma tradicional
```

### archive/zlib — Compresión/descompresión zlib (RFC 1950, implementación pura en Nolang)

Formato de flujo zlib: cabecera de 2 bytes + DEFLATE sin procesar + código de verificación Adler-32 de 4 bytes:

```no
out = zlib.zlib-compress(data); Comprimir a formato zlib (bloques stored)
out = zlib.zlib-decompress(data); Descomprimir formato zlib (devuelve ?[]byte)
sum = zlib.adler-32(data, n); Calcular código de verificación Adler-32
```

### archive/zstd — Descompresión Zstandard (implementación pura en Nolang)

Implementación pura en Nolang de la descompresión Zstandard (zstd), que incluye decodificación FSE, decodificación Huffman y decodificación de secuencias LZ77:

```no
out = zstd.decompress(data); Descomprimir formato .zst
```

---
