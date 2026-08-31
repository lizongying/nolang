---
sidebar_position: 6
---

# 跨模組調用前綴

Nolang 採用強制性的模組命名空間規範：在一個 `.no` 檔案中調用**其他模組**（其他 `.no` 檔案）定義的函數或常量時，必須使用 `ShortName.` 前綴。這避免了跨模組的命名衝突。

標準庫模組由編譯器自動載入，**不需顯式導入**（無需 `# std/...` 註解），直接使用 `ShortName.` 前綴即可調用。

> **依賴與路徑解析**：本地包的依賴鍵可透過 `workspace.jsonc` 映射為短名稱或完整 URL 形式。詳見 [依賴類型與版本號規則](../usage#依賴類型與版本號規則)。

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

```no
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

### 函數命名慣例

**不要在函數名前加上模組名前綴。** 模組內的函數只需用簡短直觀的名稱即可，跨模組調用時的 `ShortName.` 前綴會自動帶出模組資訊。

```no
; ✅ 正確：函數名簡潔，不加模組名前綴
; tail.no
tail = () { ... }              ; 入口函數直接用模組名
atoi = (s str) (v i64) { ... } ; 輔助函數用簡短名稱

; ❌ 避免：函數名冗餘帶上模組名前綴
; tail-run = () { ... }
; tail-atoi = (s str) (v i64) { ... }
```

跨模組導入時也保持一致：

```no
; ✅ 簡潔直觀
# /src/tail.tail
# /src/mktemp.mktemp

; ❌ 冗餘
; # /src/tail.tail-run
; # /src/mktemp.mktemp-run
```

> **注意避開關鍵字**：`run`（async 關鍵字）、`match`（條件匹配關鍵字）等不能用作函數名。入口函數建議直接使用模組名本身（如 `ping.no` → `ping`）。

## 不需要前綴

以下情況不需要前綴：

### 1. 全局函數（`with-cap` / `with-len` / `with-cap-len` / `print` / `eprint` / `format`）

這 6 個函數是語言級全局內置函數，可以直接使用，無需模組前綴。它們的註釋宣告統一放在 `std/global.no` 中，方便開發者查看。

**容量/長度構造：**

- `with-cap(cap)` — 建立指定容量的字串或切片（len=0），型別由賦值左側推斷
- `with-len(len)` — 建立指定長度的字串或切片
- `with-cap-len(cap, len)` — 建立指定容量和長度的字串或切片

**輸出/格式化：**

Nolang 使用**具名格式字串**語法 `{name[:spec]}`，直接引用作用域中的變量，無需位置參數。支援編譯期驗證。輸出透過 `io.out`/`io.err` 系統調用直接寫入，不依賴 libc `printf`。

- `print(s)` — 輸出到標準輸出，**自動換行**
- `eprint(s)` — 輸出到標準錯誤，**自動換行**
- `format(s)` — 返回格式化字串（替代 `sprintf`），不自動換行

> `printf`、`eprintf`、`sprintf` 已**廢棄**，保留僅為向後兼容。替代關係如下：
> - `printf(s)` → `io.out(s)`（無換行，標準輸出）
> - `eprintf(s)` → `io.err(s)`（無換行，標準錯誤）
> - `sprintf(s)` → `format(s)`（返回格式化字串）
>
> `io.out`/`io.err` 是底層命令，輸出**不換行**。由於模組調用必須加模組名，`io.err` 明確了模組前綴，不會與 Option 構造函數 `err()` 衝突；即使同名，模組前綴也能區分。

```no
; 容量/長度構造（無前綴）
s str = with-cap(256)            ; ✅ 預分配 256 位元組的 str
v []i64 = with-cap(100)          ; ✅ 預分配 100 個元素的切片
v []i64 = with-cap-len(200, 100) ; ✅ 容量 200、長度 100
v []i64 = with-len(100)          ; ✅ 長度 100 的切片

; 輸出/格式化（無前綴）
print('hello {n}')               ; ✅ 自動換行
print(a, b, c)                   ; ✅ 多參數，空格分隔，自動換行
print()                          ; ✅ 空參數，只輸出換行
s = format('x={x}')              ; ✅ 返回格式化字串
eprint('err: {n}')               ; ✅ 輸出到 stderr 並換行
eprint('err:', a, b)             ; ✅ 多參數，stderr 空格分隔
print('編號 {id:06} 金額 {money:.2f}')  ; ✅ 支援對齊、填充、寬度、精度

; 底層命令（需要模組前綴）
io.out('no-newline-here')        ; ✅ 輸出不換行（替代 printf）
io.err('err-no-newline')         ; ✅ stderr 不換行（替代 eprintf）

; 其他所有跨模組調用都需要前綴
fs.open(path, opts)              ; ✅ 帶前綴（builtin 也需要）
```

#### 格式說明符

`{name[:spec]}` 中 `spec` 支援以下欄位（順序固定，均可省略）：

```
[[fill]align][sign][#][0][width][.precision][type]
```

- `fill` — 填充字元（預設空格），需與 `align` 一起使用
- `align` — `<` 左對齊、`>` 右對齊、`^` 置中
- `sign` — `+` 正數顯示加號、`-` 僅負數顯示（預設）
- `#` — 進制前綴（`0x`/`0o`/`0b`）
- `0` — 數值前補零至指定寬度
- `width` — 最小欄位寬度
- `.precision` — 浮點數小數位數 / 字串截斷長度
- `type` — `d`(整數)、`x`/`X`(十六進制)、`o`(八進制)、`b`(二進制)、`c`(字元)、`f`(定點)、`e`/`E`(科學記號)、`g`/`G`(通用)、`s`(字串，預設)、`t`(資料型別名，編譯期確定)、`v`(字面量值，字串加單引號，vec 調用 to-str)

```no
x i64 = 42
u u64 = 255
pi f64 = 3.14159
s str = 'hello'
print('{x:06}')              ; 000042
print('{x:>10}')             ; 右對齊寬度 10
print('{u:#x}')              ; 0xff
print('{pi:.2f}')            ; 3.14
print('{pi:8.3e}')           ; 3.142e+00
print('{s:<10}')             ; hello     （左對齊）
print('{s:.3}')              ; hel（截斷為 3 字元）
print('{x:t}')              ; i64（印出變數 x 的型別名）
print('{s:v}')              ; 'hello'（字串用單引號包裹）
```

`{{` 與 `}}` 用於輸出字面 `{` 與 `}`。

### 2. 同檔案定義

在同一 `.no` 檔案內定義的函數、常量、方法，直接使用，不需前綴。

```no
; 在 sha256.no 中：
sha256(data)              ; sha256 定義在本檔案
HMAC-BLOCK-SIZE           ; 常量定義在本檔案
```

### 3. 內置類型方法

內置類型（`str`、`i64`、`vec`、`arr`、`byte`、`char`、`bool` 等）的方法調用不需前綴。方法已內建於型別，通過接收者型別直接解析。

```no
'hello'.starts-with('he')  ; str 方法
n.to-str()                 ; int 方法
v.push(42)                 ; vec 方法
a.contains(3)              ; arr 方法
c.is-digit()               ; char 方法
```

### 4. 結構體實例方法

對已建立的結構體實例調用方法不需模組前綴。方法通過實例的型別解析，編譯器自動找到對應的 `struct.method` 定義。

```no
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

### `Name.Function` 的兩種語義

`process.cmd(...)` 和 `p.start(...)` 寫法都是 `xxx.yyy()`，但語義完全不同：

| 寫法 | `xxx` | `yyy` | 語義 |
| --- | --- | --- | --- |
| `process.cmd(...)` | 模組 ShortName | 模組級函數 | `xxx` 是模組路徑，`yyy` 是該模組定義的獨立函數 |
| `p.start(...)` | 實例變數 | 結構體方法 | `xxx` 是 `process` 類型的變數，`yyy` 是定義為 `process.start = ...` 的方法 |

**定義時的差異**：
- 模組級函數定義時**不加前綴**：在模組內部直接寫 `cmd = (program str, ...) { ... }`
- 結構體方法定義時**必須加 `struct.` 前綴：`process.start = (program str, ...) { ... }`

**調用時的差異**：
- 模組級函數在外部調用時用 `ModuleName.function()`：`process.cmd(...)`
- 結構體方法通過實例調用：`p = process.new()` → `p.start(...)`

> **注意**：即使在模組內部，調用同模組的模組級函數也不加前綴（`cmd(...)`），而調用結構體方法則通過 `self` 隱含或 `.method()` 語法。

## 跨模組型別引用

引用**其他模組**定義的型別（結構體、介面、列舉等）時，必須使用 `ShortName.` 前綴。這適用於：

### 結構體實作介面

當一個結構體實作其他模組定義的介面時，介面名必須帶模組前綴：

```no
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

```no
; ✅ 正確：返回型別使用 sql.result
db-mysql.exec = (sql str) (r sql.result) {
    ...
}
```

### 結構體欄位型別

```no
; ✅ 正確：欄位型別使用 sql.connection
conn-mysql sql.db {
    handle sql.connection
}
```

### 不需前綴的情況

- **同模組定義的型別**：在同一 `.no` 檔案中定義的結構體、介面、列舉，直接使用型別名
- **內置型別**：`str`、`i64`、`bool`、`byte` 等內置型別不需前綴
- **內置介面**：`enter`、`leave` 等語言內置介面不需前綴

```no
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

```no
; 標準庫模組自動載入，無需顯式導入

; ─── 不需前綴 ───

; 全局函數（宣告在 global.no）
s str = with-cap(256)
v []i64 = with-len(100)
print('hello {n}')
s = format('x={x}')

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
