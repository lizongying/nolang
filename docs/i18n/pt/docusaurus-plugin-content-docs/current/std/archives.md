---
sidebar_position: 3.9
---

## Arquivos

### archive/tar — Arquivo TAR (POSIX ustar)

```no
; Ler um tar comum
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

; Ler .tar.gz (descomprime automaticamente)
archive = tar.open-gz(gz-data)

; Métodos de entry
name = e.name()
sz = e.size()
typ = e.type()
out = e.read()

; Escrever tar
builder = builder {}
builder.add-file(name, content)
builder.add-dir(name)
archive = builder.finish()
```

### archive/zip — Análise de arquivo ZIP

```no
archive = zip {
    data: raw-bytes
}
count = archive.count(); número de entradas
e = archive.entry(idx); obter uma entry
name = archive.name(idx); nome do ficheiro
sz = archive.size(idx); tamanho original
csz = archive.compressed-size(idx); tamanho comprimido
method = archive.method(idx); 0=stored, 8=deflate
out = archive.extract(idx); modos stored e deflate

; Métodos de entry
name = e.name()
sz = e.size()
csz = e.compressed-size()
method = e.method()
out = e.extract()
```

### archive/gzip — Compressão GZIP e DEFLATE bruto

```no
out = gzip.compress(data); compressão zlib
out = gzip.decompress(data); descompressão zlib
out = gzip.inflate-decompress(data, out-size); descompressão DEFLATE bruta (ZIP method 8)
```

### archive/bzip2 — Descompressão BZIP2 (implementação pura em Nolang)

Implementação pura em Nolang da descompressão BZIP2, incluindo transformação inversa BWT, transformação inversa MTF, descodificação Huffman e descodificação RLE:

```no
out = bzip2.decompress(data); descomprime dados .bz2
```

### archive/xz — Descompressão XZ/LZMA (implementação pura em Nolang)

Implementação pura em Nolang da descompressão LZMA2, com suporte para contentores .xz e para o formato .lzma tradicional:

```no
out = xz.decompress(data); descomprime o formato .xz
out = xz.lzma-decompress(data); descomprime o formato .lzma tradicional
```

### archive/zlib — Compressão/descompressão zlib (RFC 1950, implementação pura em Nolang)

Formato de fluxo zlib: cabeçalho de 2 bytes + DEFLATE bruto + código de verificação Adler-32 de 4 bytes:

```no
out = zlib.zlib-compress(data); comprime para o formato zlib (blocos stored)
out = zlib.zlib-decompress(data); descomprime o formato zlib (retorna ?[]byte)
sum = zlib.adler-32(data, n); calcula o código de verificação Adler-32
```

### archive/zstd — Descompressão Zstandard (implementação pura em Nolang)

Implementação pura em Nolang da descompressão Zstandard (zstd), incluindo descodificação FSE, descodificação Huffman e descodificação de sequências LZ77:

```no
out = zstd.decompress(data); descomprime o formato .zst
```

---
