---
sidebar_position: 3.9
---

## 歸檔

### archive/tar — TAR 歸檔（POSIX ustar）

```no
; 讀取普通 tar
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

; 讀取 .tar.gz（自動解壓縮）
archive = tar.tar-open-gz(gz-data)

; tar-entry 方法
name = e.name()
sz = e.size()
typ = e.type()
out = e.read()

; 寫入 tar
builder = tar-builder{}
builder.add-file(name, content)
builder.add-dir(name)
archive = builder.finish()
```

### archive/zip — ZIP 歸檔解析

```no
archive = zip{
    data: raw-bytes
}
count = archive.count()                        ; 條目數
e = archive.entry(idx)                         ; 取得 zip-entry
name = archive.name(idx)                       ; 檔名
sz = archive.size(idx)                         ; 原始大小
csz = archive.compressed-size(idx)             ; 壓縮後大小
method = archive.method(idx)                   ; 0=stored, 8=deflate
out = archive.extract(idx)                     ; stored 和 deflate 模式

; zip-entry 方法
name = e.name()
sz = e.size()
csz = e.compressed-size()
method = e.method()
out = e.extract()
```

### archive/gzip — GZIP 壓縮與原始 DEFLATE

```no
out = gzip.gzip-compress(data)                      ; zlib 壓縮
out = gzip.gzip-decompress(data)                    ; zlib 解壓縮
out = gzip.inflate-decompress(data, out-size)       ; 原始 DEFLATE 解壓縮（ZIP method 8）
```

### archive/bzip2 — BZIP2 解壓縮（純 Nolang 實現）

純 Nolang 實現 BZIP2 解壓縮，包含 BWT 反變換、MTF 反變換、Huffman 解碼與 RLE 解碼：

```no
out = bzip2.bzip2-decompress(data)                   ; 解壓 .bz2 資料
```

### archive/xz — XZ/LZMA 解壓縮（純 Nolang 實現）

純 Nolang 實現 LZMA2 解壓縮，支援 .xz 容器與傳統 .lzma 格式：

```no
out = xz.xz-decompress(data)                        ; 解壓 .xz 格式
out = xz.lzma-decompress(data)                      ; 解壓傳統 .lzma 格式
```

### archive/zlib — zlib 壓縮/解壓縮（RFC 1950，純 Nolang 實現）

zlib 串流格式：2-byte 標頭 + 原始 DEFLATE + 4-byte Adler-32 校驗碼：

```no
out = zlib.zlib-compress(data)                       ; 壓縮為 zlib 格式（stored blocks）
out = zlib.zlib-decompress(data)                     ; 解壓 zlib 格式
sum = zlib.adler-32(data, n)                        ; 計算 Adler-32 校驗碼
```

### archive/zstd — Zstandard 解壓縮（純 Nolang 實現）

純 Nolang 實現 Zstandard (zstd) 解壓縮，包含 FSE 解碼、Huffman 解碼、LZ77 序列解碼：

```no
out = zstd.zstd-decompress(data)                     ; 解壓 .zst 格式
```

---
