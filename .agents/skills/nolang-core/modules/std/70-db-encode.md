# database/sql, encoding/*(hex/base64/csv/pem), archive/*(tar/zip/gzip/bzip2/xz/zlib/zstd)

> 视图切分自原 `nolang-std/SKILL.md`，正文未改写；索引与加载判据见 [`../m20-std.md`](../m20-std.md)。

### Database

#### database/sql — Database Access Interface

Defines standard interfaces for database connections, queries, and prepared statements, implemented by concrete drivers:

```no
; Execution result
result {
    last-id i64
    affected i64
}

; Connection interface (enter/leave auto-management)
db {
    enter
    leave
    close() (ok bool)
    exec(sql str) (r result)
    query(sql str) (rs rows)
    prepare(sql str) (s stmt)
}

; Result set interface
rows {
    enter
    leave
    next() (ok bool)
    scan-int(col i64) (v i64)
    scan-str(col i64) (v str)
    scan-float(col i64) (v f64)
    close() (ok bool)
}

; Iterate to next row
; Read integer
; Read string
; Read float

; Prepared statement interface
stmt {
    enter
    leave
    bind-int(idx i64, v i64) (ok bool)
    bind-str(idx i64, v str) (ok bool)
    bind-bool(idx i64, v bool) (ok bool)
    exec() (r result)
    query() (rs rows)
    close() (ok bool)
}
```

---

### Encoding

#### encoding/hex — Hexadecimal

```no
; Encoding (defined in byte module)
out = data.to-hex(); []byte → uppercase hex str
out = data.to-hex-lower(); []byte → lowercase hex str

; Decoding (defined in str module)
out = s.from-hex(); hex str → ?[]byte (nil=empty, err=invalid character)
```

#### encoding/base64 — Base64 (RFC 4648)

```no
BASE64-STD = 'ABC...+/'
BASE64-URL = 'ABC...-_'
PAD = 61; '='

out-n = base64.encode(data, n, table, out); Base64 encode
out-n = base64.encode-std(data, n, out); Standard encoding
out-n = base64.encode-url(data, n, out); URL-safe encoding
out-n = base64.decode(s, n, table, out); Base64 decode (?i64, nil=invalid input)
```

#### encoding/csv — CSV Parsing (RFC 4180)

```no
fn, new-pos = csv.parse-field(s, sn, pos, field); Parse single field
n = csv.parse-line(s, sn, fields, max); Parse one line
out-n = csv.encode-field(field, fn, out); Encode field
```

#### encoding/pem — PEM Encoding/Decoding (RFC 7468)

PEM format is widely used for X.509 certificates, RSA/ECDSA keys, etc.

```no
; Struct
pem-block {
    label str
    data []byte
}

; Encode
out = pem.pem-encode(label, data); Encode raw bytes to PEM string

; Decode
result = pem.pem-decode(pem-str); Parse PEM string (?pem-block, nil=parse error)
; Access result.label and result.data on success
```

---

### Archive

#### archive/tar — TAR Archive (POSIX ustar)

```no
; Read regular tar
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

; Read .tar.gz (auto decompress)
archive = tar.tar-open-gz(gz-data)

; tar-entry methods
name = e.name()
sz = e.size()
typ = e.type()
out = e.read()
yes = e.is-dir()
yes = e.is-file()
mode = e.mode()
ts = e.mtime()

; Write tar
builder = tar-builder {}
builder.add-file(name, content)
builder.add-dir(name)
builder.add-bytes(name, content, typ)
builder.pre-alloc(size)
archive = builder.finish()
```

#### archive/zip — ZIP Archive Parsing

```no
archive = zip {
    data: raw-bytes
}
count = archive.count(); Entry count
e = archive.entry(idx); Get zip-entry
name = archive.name(idx); Filename
sz = archive.size(idx); Original size
csz = archive.compressed-size(idx); Compressed size
method = archive.method(idx); 0=stored, 8=deflate
out = archive.extract(idx); stored and deflate modes

; zip-entry methods
name = e.name()
sz = e.size()
csz = e.compressed-size()
method = e.method()
out = e.extract()

; Write zip
writer = zip-writer {}
writer.add-file(name, content)
writer.add-dir(name)
out = writer.finalize()
```

#### archive/gzip — GZIP Compression and Raw DEFLATE

```no
out = gzip.gzip-compress(data); zlib compression
out = gzip.gzip-decompress(data); zlib decompression
out = gzip.inflate-decompress(data, out-size); Raw DEFLATE decompression (ZIP method 8)
```

#### archive/bzip2 — BZIP2 Decompression (Pure Nolang)

Pure Nolang implementation of BZIP2 decompression (BWT inverse, MTF inverse, Huffman decode, RLE decode):

```no
out = bzip2.bzip2-decompress(data); Decompress .bz2 data
out = bzip2.bzip2-compress(data); Compress to .bz2 data
```

#### archive/xz — XZ/LZMA Decompression (Pure Nolang)

Pure Nolang implementation of LZMA2 decompression, supporting both .xz container and legacy .lzma formats:

```no
out = xz.xz-decompress(data); Decompress .xz format
out = xz.lzma-decompress(data); Decompress legacy .lzma format
out = xz.xz-compress(data); Compress to .xz format
```

#### archive/zlib — zlib Compression/Decompression (RFC 1950, Pure Nolang)

zlib stream format: 2-byte header + raw DEFLATE + 4-byte Adler-32 checksum:

```no
out = zlib.zlib-compress(data); Compress to zlib format (stored blocks)
out = zlib.zlib-decompress(data); Decompress zlib format
out = zlib.zlib-decompress-stored(data); Decompress stored blocks
consumed = zlib.zlib-consumed-stored(data); Consumed bytes in stored blocks
sum = zlib.adler-32(data, n); Compute Adler-32 checksum
```

#### archive/zstd — Zstandard Decompression (Pure Nolang)

Pure Nolang implementation of Zstandard (zstd) decompression (FSE decode, Huffman decode, LZ77 sequence decode):

```no
out = zstd.zstd-decompress(data); Decompress .zst format
out = zstd.zstd-compress(data); Compress to .zst format
```

---
