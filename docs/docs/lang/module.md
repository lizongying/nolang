---
sidebar_position: 6
---

# 跨模組調用前綴

Nolang 採用強制性的模組命名空間規範：在一個 `.no` 檔案中調用**其他模組**（其他 `.no` 檔案）定義的函數或常量時，必須使用 `ShortName.` 前綴。這避免了跨模組的命名衝突。

標準庫模組由編譯器自動載入，**不需顯式導入**（無需 `# std/...` 註解），直接使用 `ShortName.` 前綴即可調用。

## ShortName 定義

ShortName 是模組路徑的最後一段，用作跨模組調用時的前綴。

| 檔案路徑 | FullPath | ShortName | 說明 |
| --- | --- | --- | --- |
| `std/math.no` | `math` | `math` | 頂層檔案 |
| `std/fs.no` | `fs` | `fs` | 頂層檔案 |
| `std/net/net.no` | `net/net` | `net` | 路徑最後一段 |
| `std/net/client.no` | `net/client` | `client` | 路徑最後一段 |
| `std/hash/sha256.no` | `hash/sha256` | `sha256` | 路徑最後一段 |
| `std/archive/gzip.no` | `archive/gzip` | `gzip` | 路徑最後一段 |

ShortName 即為 FullPath 以斜線分隔後的最後一段（如 `hash/sha256` → `sha256`）。

## 需要前綴

調用其他模組定義的模組級函數或常量時，必須使用 `ShortName.` 前綴。

```nolang
; 模組級函數
sha256.sha256(data)
sha256.sha256-hex(data)
fs.open(path, opts)
gzip.gzip-decompress(data)
math.degrees(rad)

; 模組常量
net.NET-BUF-SIZE
math.PI
```

## 不需要前綴

以下情況不需要前綴：

### 1. `printf` / `sprintf` / `print`

這三個函數依規定免除前綴，直接使用。**這僅僅是針對這三個函數的特例**，並非因為它們是 builtin —— 其他 builtin 函數（如 `open`、`close`、`read`、`write` 等）仍需使用模組前綴。

```nolang
printf('hello %d', n)        ; ✅ 無前綴
s = sprintf('x=%d', x)       ; ✅ 無前綴
print('hello')               ; ✅ 無前綴

fs.open(path, opts)          ; ✅ 帶前綴（builtin 也需要）
```

### 2. 同檔案定義

在同一 `.no` 檔案內定義的函數、常量、方法，直接使用，不需前綴。

```nolang
; 在 sha256.no 中：
sha256(data)              ; sha256 定義在本檔案
HMAC-BLOCK-SIZE           ; 常量定義在本檔案
```

### 3. 內置類型方法

內置類型（`str`、`i64`、`vec`、`arr`、`byte`、`char`、`bool` 等）的方法調用不需前綴。方法已內建於型別，通過接收者型別直接解析。

```nolang
'hello'.starts-with('he')  ; str 方法
n.to-str()                 ; int 方法
v.push(42)                 ; vec 方法
a.contains(3)              ; arr 方法
c.is-digit()               ; char 方法
```

### 4. 結構體實例方法

對已建立的結構體實例調用方法不需模組前綴。方法通過實例的型別解析，編譯器自動找到對應的 `struct.method` 定義。

```nolang
f = fs.open(path, opts)    ; fs.open 是模組級函數，需前綴
f.read(buf, n)             ; file.read 是結構體方法，不需前綴
f.close()                  ; file.close 是結構體方法，不需前綴

p = path{
    p: '/tmp'
}
p.exists()                 ; path.exists 是結構體方法，不需前綴
```

## 方法調用 vs 模組函數調用

方法調用是否需要前綴取決於**方法所有者**：

- **內置類型的方法**（`str.starts-with`、`i64.to-str` 等）—— 不需前綴
- **結構體實例的方法**（`f.read`、`p.exists` 等）—— 不需前綴
- **模組級函數**（`fs.open`、`sha256.sha256` 等）—— **需要前綴**

`fs.fil()` 中 `fs` 是模組的 ShortName，`fil` 是模組級函數名。`fs.` 前綴不可省略，因為 `fs` 在此處不是變數名，而是模組路徑。

## 跨模組型別引用

引用**其他模組**定義的型別（結構體、介面、列舉等）時，必須使用 `ShortName.` 前綴。這適用於：

### 結構體實作介面

當一個結構體實作其他模組定義的介面時，介面名必須帶模組前綴：

```nolang
; ❌ 錯誤：db、rows、stmt 是 sql 模組定義的介面，不能省略前綴
db-mysql db {
    fd i64
}

; ✅ 正確：使用 sql.db、sql.rows、sql.stmt
db-mysql sql.db {
    fd i64
}

rows-mysql sql.rows {
    fd i64
}

stmt-mysql sql.stmt {
    fd i64
}
```

### 函數參數與返回型別

函數簽名中的跨模組型別同樣需要前綴：

```nolang
; ✅ 正確：返回型別使用 sql.result
db-mysql.exec = (sql str) (r sql.result) {
    ...
}
```

### 結構體欄位型別

```nolang
; ✅ 正確：欄位型別使用 sql.connection
conn-mysql sql.db {
    handle sql.connection
}
```

### 不需前綴的情況

- **同模組定義的型別**：在同一 `.no` 檔案中定義的結構體、介面、列舉，直接使用型別名
- **內置型別**：`str`、`i64`、`bool`、`byte` 等內置型別不需前綴
- **內置介面**：`enter`、`leave` 等語言內置介面不需前綴

```nolang
; 同檔案定義的型別，不需前綴
result {
    last-id i64
    affected i64
}

; enter/leave 是內置介面，不需前綴
; result 是同檔案定義的結構體，不需前綴
db enter, leave {
    close() (ok bool)
    exec(sql str) (r result)
}
```

## 完整範例

```nolang
; 標準庫模組自動載入，無需顯式導入

; ─── 不需前綴 ───

; 同檔案函數
sha256(data)

; 內置類型方法
'hello'.starts-with('he')
n.to-str()
v.push(42)

; 結構體實例方法
f = fs.open(path, opts)
f.read(buf, n)
f.close()

; printf/sprintf/print
printf('hello %d', n)
s = sprintf('x=%d', x)

; ─── 需要前綴 ───

; 模組級函數
sha256.sha256(data)
sha256.sha256-hex(data)
fs.open(path, opts)
gzip.gzip-decompress(data)
math.degrees(rad)

; 模組常量
net.NET-BUF-SIZE
math.PI

; 跨模組型別引用（介面實作、參數型別、返回型別、欄位型別）
db-mysql sql.db {
    fd i64
}

r sql.result = d.exec('CREATE TABLE ...')
```
