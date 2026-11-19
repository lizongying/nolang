---
sidebar_position: 3.9
---

## 아카이브

### archive/tar — TAR 아카이브(POSIX ustar)

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

### archive/zip — ZIP 아카이브 파싱

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

### archive/gzip — GZIP 압축 및 원시 DEFLATE

```no
out = gzip.compress(data); zlib 壓縮
out = gzip.decompress(data); zlib 解壓縮
out = gzip.inflate-decompress(data, out-size); 原始 DEFLATE 解壓縮（ZIP method 8）
```

### archive/bzip2 — BZIP2 압축 해제(순수 Nolang 구현)

순수 Nolang으로 구현한 BZIP2 압축 해제이며, BWT 역변환, MTF 역변환, Huffman 디코딩과 RLE 디코딩을 포함합니다:

```no
out = bzip2.decompress(data); 解壓 .bz2 資料
```

### archive/xz — XZ/LZMA 압축 해제(순수 Nolang 구현)

순수 Nolang으로 구현한 LZMA2 압축 해제이며, .xz 컨테이너와 기존 .lzma 형식을 지원합니다:

```no
out = xz.decompress(data); 解壓 .xz 格式
out = xz.lzma-decompress(data); 解壓傳統 .lzma 格式
```

### archive/zlib — zlib 압축/압축 해제(RFC 1950, 순수 Nolang 구현)

zlib 스트림 형식: 2바이트 헤더 + 원시 DEFLATE + 4바이트 Adler-32 검사합:

```no
out = zlib.zlib-compress(data); 壓縮為 zlib 格式（stored blocks）
out = zlib.zlib-decompress(data); 解壓 zlib 格式（回傳 ?[]byte）
sum = zlib.adler-32(data, n); 計算 Adler-32 校驗碼
```

### archive/zstd — Zstandard 압축 해제(순수 Nolang 구현)

순수 Nolang으로 구현한 Zstandard(zstd) 압축 해제이며, FSE 디코딩, Huffman 디코딩, LZ77 시퀀스 디코딩을 포함합니다:

```no
out = zstd.decompress(data); 解壓 .zst 格式
```

---
