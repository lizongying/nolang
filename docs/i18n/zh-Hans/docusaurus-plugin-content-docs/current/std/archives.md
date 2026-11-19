---
sidebar_position: 3.9
---

## 归档

### archive/tar — TAR 归档（POSIX ustar）

```no
; 读取普通 tar
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

; 读取 .tar.gz（自动解压缩）
archive = tar.open-gz(gz-data)

; entry 方法
name = e.name()
sz = e.size()
typ = e.type()
out = e.read()

; 写入 tar
builder = builder {}
builder.add-file(name, content)
builder.add-dir(name)
archive = builder.finish()
```

### archive/zip — ZIP 归档解析

```no
archive = zip {
    data: raw-bytes
}
count = archive.count(); 条目数
e = archive.entry(idx); 取得 entry
name = archive.name(idx); 档名
sz = archive.size(idx); 原始大小
csz = archive.compressed-size(idx); 压缩后大小
method = archive.method(idx); 0=stored, 8=deflate
out = archive.extract(idx); stored 和 deflate 模式

; entry 方法
name = e.name()
sz = e.size()
csz = e.compressed-size()
method = e.method()
out = e.extract()
```

### archive/gzip — GZIP 压缩与原始 DEFLATE

```no
out = gzip.compress(data); zlib 压缩
out = gzip.decompress(data); zlib 解压缩
out = gzip.inflate-decompress(data, out-size); 原始 DEFLATE 解压缩（ZIP method 8）
```

### archive/bzip2 — BZIP2 解压缩（纯 Nolang 实现）

纯 Nolang 实现 BZIP2 解压缩，包含 BWT 反变换、MTF 反变换、Huffman 解码与 RLE 解码：

```no
out = bzip2.decompress(data); 解压 .bz2 资料
```

### archive/xz — XZ/LZMA 解压缩（纯 Nolang 实现）

纯 Nolang 实现 LZMA2 解压缩，支援 .xz 容器与传统 .lzma 格式：

```no
out = xz.decompress(data); 解压 .xz 格式
out = xz.lzma-decompress(data); 解压传统 .lzma 格式
```

### archive/zlib — zlib 压缩/解压缩（RFC 1950，纯 Nolang 实现）

zlib 串流格式：2-byte 标头 + 原始 DEFLATE + 4-byte Adler-32 校验码：

```no
out = zlib.zlib-compress(data); 压缩为 zlib 格式（stored blocks）
out = zlib.zlib-decompress(data); 解压 zlib 格式（回传 ?[]byte）
sum = zlib.adler-32(data, n); 计算 Adler-32 校验码
```

### archive/zstd — Zstandard 解压缩（纯 Nolang 实现）

纯 Nolang 实现 Zstandard (zstd) 解压缩，包含 FSE 解码、Huffman 解码、LZ77 序列解码：

```no
out = zstd.decompress(data); 解压 .zst 格式
```

---
