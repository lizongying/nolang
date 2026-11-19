---
sidebar_position: 3.9
---

## アーカイブ

### archive/tar — TAR アーカイブ（POSIX ustar）

```no
; 讀取普通 tar
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

; 讀取 .tar.gz（自動解壓縮）
archive = tar.open-gz(gz-data)

; entry 方法
name = e.name()
sz = e.size()
typ = e.type()
out = e.read()

; 寫入 tar
builder = builder {}
builder.add-file(name, content)
builder.add-dir(name)
archive = builder.finish()
```

### archive/zip — ZIP アーカイブ解析

```no
archive = zip {
    data: raw-bytes
}
count = archive.count(); 條目數
e = archive.entry(idx); 取得 entry
name = archive.name(idx); 檔名
sz = archive.size(idx); 原始大小
csz = archive.compressed-size(idx); 壓縮後大小
method = archive.method(idx); 0=stored, 8=deflate
out = archive.extract(idx); stored 和 deflate 模式

; entry 方法
name = e.name()
sz = e.size()
csz = e.compressed-size()
method = e.method()
out = e.extract()
```

### archive/gzip — GZIP 圧縮と生の DEFLATE

```no
out = gzip.compress(data); zlib 壓縮
out = gzip.decompress(data); zlib 解壓縮
out = gzip.inflate-decompress(data, out-size); 原始 DEFLATE 解壓縮（ZIP method 8）
```

### archive/bzip2 — BZIP2 展開（純 Nolang 実装）

BZIP2 の展開を純 Nolang で実装しており、BWT 逆変換、MTF 逆変換、Huffman デコード、RLE デコードを含みます：

```no
out = bzip2.decompress(data); 解壓 .bz2 資料
```

### archive/xz — XZ/LZMA 展開（純 Nolang 実装）

LZMA2 の展開を純 Nolang で実装しており、.xz コンテナと従来の .lzma 形式をサポートします：

```no
out = xz.decompress(data); 解壓 .xz 格式
out = xz.lzma-decompress(data); 解壓傳統 .lzma 格式
```

### archive/zlib — zlib 圧縮/展開（RFC 1950、純 Nolang 実装）

zlib ストリーム形式：2 バイトのヘッダ + 生の DEFLATE + 4 バイトの Adler-32 チェックサム：

```no
out = zlib.zlib-compress(data); 壓縮為 zlib 格式（stored blocks）
out = zlib.zlib-decompress(data); 解壓 zlib 格式（回傳 ?[]byte）
sum = zlib.adler-32(data, n); 計算 Adler-32 校驗碼
```

### archive/zstd — Zstandard 展開（純 Nolang 実装）

Zstandard (zstd) の展開を純 Nolang で実装しており、FSE デコード、Huffman デコード、LZ77 シーケンスデコードを含みます：

```no
out = zstd.decompress(data); 解壓 .zst 格式
```

---
