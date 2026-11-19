---
sidebar_position: 3.9
---

## Архивы

### archive/tar — архив TAR (POSIX ustar)

```no
; Чтение обычного tar
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

; Чтение .tar.gz (автоматическая распаковка)
archive = tar.open-gz(gz-data)

; методы entry
name = e.name()
sz = e.size()
typ = e.type()
out = e.read()

; запись tar
builder = builder {}
builder.add-file(name, content)
builder.add-dir(name)
archive = builder.finish()
```

### archive/zip — разбор архива ZIP

```no
archive = zip {
    data: raw-bytes
}
count = archive.count(); число элементов
e = archive.entry(idx); получить entry
name = archive.name(idx); имя файла
sz = archive.size(idx); исходный размер
csz = archive.compressed-size(idx); размер после сжатия
method = archive.method(idx); 0=stored, 8=deflate
out = archive.extract(idx); режимы stored и deflate

; методы entry
name = e.name()
sz = e.size()
csz = e.compressed-size()
method = e.method()
out = e.extract()
```

### archive/gzip — сжатие GZIP и «сырой» DEFLATE

```no
out = gzip.compress(data); сжатие zlib
out = gzip.decompress(data); распаковка zlib
out = gzip.inflate-decompress(data, out-size); распаковка «сырого» DEFLATE (ZIP method 8)
```

### archive/bzip2 — распаковка BZIP2 (чистая реализация на Nolang)

Чистая реализация на Nolang распаковки BZIP2, включающая обратное преобразование BWT, обратное преобразование MTF, декодирование Хаффмана и декодирование RLE:

```no
out = bzip2.decompress(data); распаковка данных .bz2
```

### archive/xz — распаковка XZ/LZMA (чистая реализация на Nolang)

Чистая реализация на Nolang распаковки LZMA2 с поддержкой контейнера .xz и устаревшего формата .lzma:

```no
out = xz.decompress(data); распаковка формата .xz
out = xz.lzma-decompress(data); распаковка устаревшего формата .lzma
```

### archive/zlib — сжатие/распаковка zlib (RFC 1950, чистая реализация на Nolang)

Формат потока zlib: 2-байтовый заголовок + «сырой» DEFLATE + 4-байтовая контрольная сумма Adler-32:

```no
out = zlib.zlib-compress(data); сжатие в формат zlib (stored blocks)
out = zlib.zlib-decompress(data); распаковка формата zlib (возвращает ?[]byte)
sum = zlib.adler-32(data, n); вычисление контрольной суммы Adler-32
```

### archive/zstd — распаковка Zstandard (чистая реализация на Nolang)

Чистая реализация на Nolang распаковки Zstandard (zstd), включающая декодирование FSE, декодирование Хаффмана и декодирование последовательностей LZ77:

```no
out = zstd.decompress(data); распаковка формата .zst
```

---
