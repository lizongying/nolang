---
sidebar_position: 3.9
---

## Archives

### archive/tar — TAR Archive (POSIX ustar)

```no
; Read a regular tar
archive = tar{
    data: raw-bytes
}
count = archive.count()
e = archive.entry(idx)
name = archive.name(idx)
sz = archive.size(idx)
typ = archive.type(idx)              ; "file" / "dir" / "unknown"
yes = archive.is-dir(idx)
yes = archive.is-file(idx)
out = archive.read(idx)
mode = archive.mode(idx)
ts = archive.mtime(idx)

; Read .tar.gz (auto-decompress)
archive = tar.tar-open-gz(gz-data)

; tar-entry methods
name = e.name()
sz = e.size()
typ = e.type()
out = e.read()

; Write tar
builder = tar-builder{}
builder.add-file(name, content)
builder.add-dir(name)
archive = builder.finish()
```

### archive/zip — ZIP Archive Parsing

```no
archive = zip{
    data: raw-bytes
}
count = archive.count()                        ; Number of entries
e = archive.entry(idx)                         ; Get zip-entry
name = archive.name(idx)                       ; Filename
sz = archive.size(idx)                         ; Original size
csz = archive.compressed-size(idx)             ; Compressed size
method = archive.method(idx)                   ; 0=stored, 8=deflate
out = archive.extract(idx)                     ; stored and deflate modes

; zip-entry methods
name = e.name()
sz = e.size()
csz = e.compressed-size()
method = e.method()
out = e.extract()
```

### archive/gzip — GZIP Compression and Raw DEFLATE

```no
out = gzip.gzip-compress(data)                      ; zlib compression
out = gzip.gzip-decompress(data)                    ; zlib decompression
out = gzip.inflate-decompress(data, out-size)       ; Raw DEFLATE decompression (ZIP method 8)
```

### archive/bzip2 — BZIP2 Decompression (Pure Nolang)

Pure Nolang implementation of BZIP2 decompression, including BWT inverse transform, MTF inverse transform, Huffman decoding, and RLE decoding:

```no
out = bzip2.bzip2-decompress(data)                  ; Decompress .bz2 data
```

### archive/xz — XZ/LZMA Decompression (Pure Nolang)

Pure Nolang implementation of LZMA2 decompression, supporting both .xz container format and legacy .lzma format:

```no
out = xz.xz-decompress(data)                        ; Decompress .xz data
out = xz.lzma-decompress(data)                      ; Decompress legacy .lzma data
```

### archive/zlib — zlib Compression/Decompression (RFC 1950, Pure Nolang)

Pure Nolang implementation of RFC 1950 zlib compression and decompression, using stored blocks (BTYPE=00) for compression and supporting full DEFLATE for decompression:

```no
out = zlib.zlib-compress(data)                      ; Compress to zlib stream
out = zlib.zlib-decompress(data)                    ; Decompress zlib stream (returns []byte, ok bool)
sum = zlib.adler-32(data, n)                        ; Adler-32 checksum
```

### archive/zstd — Zstandard Decompression (Pure Nolang)

Pure Nolang implementation of Zstandard (zstd) decompression, including FSE decoding, Huffman decoding, LZ77 sequence decoding, and Zstandard frame format parsing:

```no
out = zstd.zstd-decompress(data)                    ; Decompress .zst data
```

---
