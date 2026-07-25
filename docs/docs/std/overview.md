---
sidebar_position: 3
---

# 標準庫

Nolang 標準庫（`src/std/`）包含 60+ 個模組，涵蓋格式化、數學、字串、資料結構、編解碼、加密、壓縮、檔案操作、I/O 抽象等。

使用方式：`# std/xxx`（核心模組無需導入）。

> **舊式 `use std/xxx` 仍可使用，但已廢棄（deprecated），建議改用新式 `# std/xxx` 語法。**

> **注意：本文檔中的程式碼範例均遵守「每行一條語句」規則——禁止使用分號 `;` 或逗號 `,` 將多條語句寫在同一行。** 例如 `out = from-i64(v), out = from-u64(v)` 是錯誤寫法，應拆分為多行。

---

## 基礎型別

### types — 型別定義

Nolang 型別到 LLVM 的對映關係：

| Nolang           | LLVM                                               |
| ---------------- | -------------------------------------------------- |
| `bool`           | `i1`                                               |
| `byte`           | `i8`                                               |
| `char`           | `i32`                                              |
| `i8/i16/i32/i64` | `i8/i16/i32/i64`                                   |
| `u8/u16/u32/u64` | `i8/i16/i32/i64`                                   |
| `f32`            | `float`                                            |
| `f64`            | `double`                                           |
| `str`            | union（short: `[127]byte` / long: `{*byte, i64}`） |

**複合型別：**

- **變長數組 `[]t`**：底層 `{ t*, i64 }`（data, len）
- **定長數組 `[n]t`**：LLVM 固定大小陣列
- **字串 `str`**：union 型別（short ≤127 byte 存棧上 / long 存堆上），支援 `s[i]`、`s[i..j]`、`s + t`
- **列舉/Union**：`option` tagged enum（`ok t` / `nil` / `err str`）
- **結構體**：必須多行定義，欄位不加逗號
- **配列**：底層 linked-hash-map
- **迭代器**：`for iter.next() {}`（介面方法 `next() (ok bool)`）

### option — 選項型別

`option<t>` 標籤列舉（tag=0=val, 1=nil, 2=err）：

```no
x ?t                ; 宣告 option<t>
x = 42              ; 設為有值
x = nil             ; 設為空
x = err('msg')      ; 設為錯誤

; match
x: {
    val -> f(it)
    nil ->
    err -> g(it)
}  
```

**風格指引：** 函數可能失敗或返回空值時，應使用 `?t` option 而非 `(val, ok bool)`。`?t` 有三種狀態：`ok`（有值）、`nil`（空值/正常缺失）、`err`（錯誤）。正常值會隱性綁定。例如 `pop()` 返回 `?i64`（`nil` = 空）、`read-line()` 返回 `?str`（`nil` = EOF，`err` = 錯誤）、`lookup()` 返回 `?str`（`nil` = 未找到）。詳見語法文檔。

---

## 核心函式庫

### fmt — 格式化輸出

Nolang 使用**具名格式字串** `{name[:spec]}`，直接引用作用域變量，無需位置參數。輸出透過 `io.out`/`io.err` 系統調用，不依賴 libc `printf`。

```no
print('x={x}')                 ; 具名格式，自動換行（stdout）
eprint('err {x}')              ; 具名格式，自動換行（stderr）
print('編號 {id:06} 金額 {money:.2f}')  ; 支援對齊/填充/寬度/精度
s = format('x={x}')            ; 返回格式化字串（替代 sprintf）
io.out('no-newline-here')      ; 底層命令，輸出不換行（stdout）
io.err('err-no-newline')       ; 底層命令，輸出不換行（stderr）
; printf/eprintf/sprintf 已廢棄：printf→io.out、eprintf→io.err、sprintf→format
; io.err 明確模組前綴，不會與 Option 構造函數 err() 衝突
```

### math — 數學函數

**常量：** `math.PI`, `math.E`

**基礎：** `math.abs`, `math.sqrt`

**三角：** `math.sin`, `math.cos`, `math.tan`, `math.asin`, `math.acos`, `math.atan`, `math.atan2`, `math.degrees`, `math.radians`

**雙曲：** `math.sinh`, `math.cosh`, `math.tanh`

**取整：** `math.ceil`, `math.floor`, `math.round`, `math.trunc`

**指數/對數：** `math.exp`, `math.log`, `math.log10`, `math.log2`, `math.pow`, `math.hypot`, `math.cbrt`

**其他：** `math.fmod`, `math.max`, `math.min`

### char — 字元操作

char 本質為 i32（Unicode 碼點），所有操作以方法形式提供：

```no
c char = 'A'
c.is-digit()       ; 是否為數字 (0-9)（方法）
c.is-letter()      ; 是否為字母 (a-z, A-Z)（方法）
c.is-alpha()       ; is-letter 別名（方法）
c.is-alnum()       ; 是否為字母或數字（方法）
c.is-space()       ; 是否為空白字元（方法）
c.is-upper()       ; 是否為大寫字母（方法）
c.is-lower()       ; 是否為小寫字母（方法）
c.to-upper()       ; 轉大寫（ASCII）（方法）
c.to-lower()       ; 轉小寫（ASCII）（方法）
c.to-bytes()       ; Unicode → UTF-8 位元組（方法）
c.to-str()         ; Unicode → 字串（UTF-8，方法）
```

### str — 字串操作

```no
ok = a.eq(b, n)               ; 相等比較（方法）
dst = s.copy()                ; 字串複製（方法）
s.fill(val byte)              ; 填充 byte 值（方法）
pos = s.index(sub)            ; 子字串位置
ok = s.contains(sub)          ; 是否包含
ok = s.starts-with(sub)       ; 前綴判斷
ok = s.ends-with(sub)         ; 後綴判斷
s.to-upper()                  ; 轉大寫
s.to-lower()                  ; 轉小寫
out = s.trim()                ; 去首尾空白
out = s.repeat(n)             ; 重複
out = s.slice(start, end)     ; 切片
b = s.to-bytes()              ; 轉 []byte
s = b.to-str()                ; []byte 轉 str（方法）
v = s.to-i64()                ; 字串轉 i64（回傳 ?i64）
v = s.to-i8()                 ; 字串轉 i8（回傳 ?i8）
v = s.to-i16()                ; 字串轉 i16（回傳 ?i16）
v = s.to-i32()                ; 字串轉 i32（回傳 ?i32）
v = s.to-u8()                 ; 字串轉 u8（回傳 ?u8）
v = s.to-u16()                ; 字串轉 u16（回傳 ?u16）
v = s.to-u32()                ; 字串轉 u32（回傳 ?u32）
v = s.to-u64()                ; 字串轉 u64（回傳 ?u64）
v = s.to-byte()               ; 字串轉 byte（回傳 ?byte）
v = s.to-f64()                ; 字串轉 f64（回傳 ?f64）
v = s.to-bool()               ; 字串 "true"/"false" 轉 bool（回傳 ?bool）
s = v.to-str()                ; i64 轉字串（方法）
out = s.reverse()             ; 反轉
c = s.compare(b)              ; 字典序比較
n = s.count()                 ; code point 總數
val = s.replace-char(old, new) ; 取代字元（返回結果字串）
out = s.trim-char(c)          ; 去指定字元
ok = s.empty()                ; 是否為空
s.clear()                     ; 清空（len=0，原地修改）
s = with-cap(cap)            ; 內建語法：建立指定容量的新字串（len=0）
s = with-len(len)            ; 內建語法：建立指定長度的字串（len=cap）
s = with-cap-len(cap, len)   ; 內建語法：建立指定容量和長度的字串
parts = s.split(sep)          ; 用分隔符分割（返回 []str，方法）
out = ss.join(sep)            ; []str 用分隔符連接（方法）
```

### number — 數值操作

```no
number.max(a, b)                     ; 最大值
number.min(a, b)                     ; 最小值
r = num.clamp(lo, hi)         ; 限制範圍（方法）
r = number.abs(a)                    ; 絕對值（num 泛型）
r = num.sign()                ; 正負號（-1/0/1，方法）
number.even(v)                       ; 奇偶判斷
number.odd(v)
number.gcd(a, b)                     ; 最大公因數
number.lcm(a, b)                     ; 最小公倍數
r = number.pow(a, n)                 ; 整數冪
number.i64-to-f64(v)                 ; 數值轉換
number.f64-to-i64(v)
s = int.to-str()              ; i64 轉字串（方法）
q = number.div(a, b)                 ; 除法取商
r = number.mod(a, b)                 ; 取模
number.swap(a, b)                    ; 交換
yes = float.is-nan()          ; NaN 判斷（方法）
yes = float.is-inf()          ; Inf 判斷（方法）

; 範圍常數
i8.MIN / MAX                  ; -128 / 127
i16.MIN / MAX                 ; -32768 / 32767
i32.MIN / MAX                 ; -2147483648 / 2147483647
i64.MIN / MAX                 ; -2^63 / 2^63-1
u8.MIN / MAX                  ; 0 / 255
u16.MIN / MAX                 ; 0 / 65535
u32.MIN / MAX                 ; 0 / 4294967295
u64.MIN / MAX                 ; 0 / 2^64-1
```

### byte — 位元組操作

```no
out = i64.to-bytes-be()         ; i64 → big-endian [8]byte
out = i64.to-bytes-le()         ; i64 → little-endian [8]byte
v = []byte.to-i64-be()          ; big-endian []byte → i64（1~8 位元組）
v = []byte.to-i64-le()          ; little-endian []byte → i64（1~8 位元組）
s = []byte.to-str()             ; []byte 轉 str（方法）
s = []byte.to-hex()             ; []byte → 大寫十六進制字串
s = []byte.to-hex-lower()       ; []byte → 小寫十六進制字串
s = byte.to-str()               ; byte 轉 str（方法）
```

### vec — 切片操作

```no
v = vec.vec-create(n, val)         ; 建立長度 n 的切片，全部填充 val
ok = []t.eq(a, b, n)           ; 相等比較
n = []t.len()                  ; 長度
[]t.push(val)                   ; 追加（自動擴容）
[]t.clear()                     ; 清空（len=0，cap/data 不變）
v = with-cap(cap)             ; 內建語法：建立指定容量的新切片（len=0）
v = with-len(len)             ; 內建語法：建立指定長度的切片（len=cap）
v = with-cap-len(cap, len)    ; 內建語法：建立指定容量和長度的切片
val, new-n = []t.pop()         ; 彈出
found = []t.contains(n, val)   ; 是否包含（n 為長度）
[]t.reverse(n)                  ; 反轉前 n 個元素
[]t.clone(dst)                  ; 複製到 dst
[]t.fill(n, val)                ; 前 n 個元素填充
arr = []t.to-arr()             ; 轉陣列
[]t.sort-asc()                  ; 升序排序（方法）
[]t.sort-desc()                 ; 降序排序（方法）
```

### arr — 陣列操作

```no
out = [n]t.clone()             ; 複製
ok = [n]t.eq(b)                ; 相等比較
[n]t.fill(val)                  ; 填充
[n]t.reverse()                  ; 反轉
ok = [n]t.contains(val)        ; 是否包含
v = [n]t.to-vec()              ; 轉切片
v = [n]t.max()                 ; 最大值
v = [n]t.min()                 ; 最小值
v = [n]t.sum()                 ; 總和
i = [n]t.index-of(val)          ; 索引
v = [n]t.last()                ; 最後元素
v = [n]t.first()               ; 首元素
[n]t.sort-asc()                 ; 升序排序
[n]t.sort-desc()                ; 降序排序
```

### sort — 排序常量

```no
sort.ast                         ; 升序
sort.desc                        ; 降序
```

---

## 作業系統與檔案

### os — 作業系統介面

提供環境變數、目錄操作、行程管理、系統資訊、時間等功能。檔案讀寫相關功能請見 `fs` 模組。

```no
; 環境變數
val = os.get-env(key)
os.set-env(key, val)

; 目錄
dir = os.get-wd()
os.ch-dir(dir)
os.mkdir(path, mode)

; 行程
os.exit(code)
pid = os.get-pid()

; 系統資訊
name = os.host-name()
arch = os.get-arch()
msg = os.strerror(errnum)

; 時間
sec = os.now()
ms = os.now-ms()
us = os.now-us()
ns = os.now-ns()
os.sleep(sec)
os.sleep-us(us)
os.sleep-ns(ns)

; 命令列參數
count = os.args()
val = os.arg(idx)
```

### fs — 檔案系統工具

以 `file` 結構體封裝開啟中的檔案，以 `path` 結構體封裝路徑。

```no
; 檔案結構體
file {
    fd i64
    path str
}

; 標準檔案
stdin = file{
    fd: 0
    path: '<stdin>'
}
stdout = file{
    fd: 1
    path: '<stdout>'
}
stderr = file{
    fd: 2
    path: '<stderr>'
}

; 開啟檔案（帶選項）
file-mode {
    read,
    write,
    append,
    read-write,
}
file-perm {
    perm-600,
    perm-644,
    perm-664,
    perm-666,
    perm-755,
    perm-777,
}
file-opts {
    mode file-mode
    perm file-perm
    excl bool
    truncate bool
    append bool
}
f = fs.open(path, opts)             ; 開啟檔案，失敗返回 nil

; file 方法
read-n = f.read(buf, n)          ; 讀取最多 n 位元組
line = f.read-line()              ; 讀取一行（?str, nil=EOF）
content, n = f.read-all()        ; 讀取整個檔案
written = f.write(data, n)       ; 寫入 n 位元組
ok = f.write-all(data, n)        ; 寫入全部（覆寫）
ok = f.append(data, n)           ; 追加資料
ok = f.copy-to(dst-path)         ; 複製到目標路徑
ok = f.close()                   ; 關閉（標準檔案不自動關閉）
yes = f.is-open()                ; 是否已開啟
sz = f.size()                    ; 檔案大小

; 內建函數
fd = fs.open-read(path)             ; 唯讀開啟
fd = fs.open-write(path)            ; 寫入開啟（O_CREAT|O_TRUNC, 0644）
fd = fs.open-file(path, flags, mode) ; 自訂旗標開啟
n = fs.read(fd, buf, n)             ; 底層讀取
written = fs.write(fd, data, n)     ; 底層寫入
ok = fs.close(fd)                   ; 底層關閉
ok = fs.remove(path)                ; 刪除檔案
ok = fs.rename(old, new)            ; 重新命名
ok = fs.is-file(path)               ; 判斷是否為檔案
ok = fs.is-dir(path)                ; 判斷是否為目錄
sz = fs.stat-size(path)             ; 取得檔案大小
sz = fs.file-size(path)             ; 同 stat-size
line = fs.get-line()                ; 從標準輸入讀取一行（?str, nil=EOF）
ok = fs.copy-file(src, dst)         ; 複製檔案

; macOS open() 旗標常量
O-RDONLY = 0, O-WRONLY = 1, O-RDWR = 2
O-CREAT = 512, O-TRUNC = 1024, O-APPEND = 8, O-EXCL = 2048
```

### env — 環境變數（簡化封裝）

```no
val = env.get(key)
val = env.lookup(key)               ; 返回 ?str（nil=未找到）
env.set(key, val)
env.unset(key)
val = env.get-with-default(key, default)
ok = env.is-set(key)
```

### args — 命令列引數

```no
n = args.count()
arg = args.get(i)
name = args.program()
ok = args.has-flag(name)
val = args.get-option(name)
arg = args.get-positional(i)
```

### path — 路徑操作

以 `path` 結構體封裝路徑字串，所有操作以方法形式提供：

```no
SEP = 47     ; '/'（ASCII）
DOT = 46     ; '.'

; 結構體
path {
    p str
}

; 路徑拼接與分解（原地修改 .p）
p = path{
    p: '/a/b/c.txt'
}
p.join(b str)           ; 拼接兩個路徑（原地修改）
p.base() (out)           ; 取檔名
p.dir()                  ; 取目錄（原地修改 .p）
p.ext() (out)            ; 取副檔名
p.clean()                ; 正規化（原地修改 .p）
p.split() (f str)        ; 分割為目錄+檔名（.p 改為目錄，返回檔名）

; 路徑判斷
p.is-abs() (yes bool)    ; 是否為絕對路徑

; 檔案系統操作（委託 fs 內建函數）
p.exists() (yes bool)        ; 是否存在
p.is-dir() (yes bool)        ; 是否為目錄
p.is-file() (yes bool)       ; 是否為檔案
p.size() (sz i64)            ; 檔案大小
p.make-dir() (ok bool)       ; 建立目錄
p.remove() (ok bool)         ; 刪除
p.rename(new-p str) (ok bool)    ; 重新命名
p.change-dir() (ok bool)     ; 切換工作目錄

; 建構型方法
path.current() (out path)    ; 取得當前工作目錄
```

### bufio — 緩衝讀取

```no
r = reader.init(fd, buf)       ; 初始化緩衝讀取器（傳回 reader）
ok = reader.fill()              ; 填充緩衝區
b = reader.read-byte()          ; 讀取一個位元組（?byte, nil=EOF）
ok = reader.read-line(line)     ; 讀取一行到 line
reader.close()                  ; 關閉
```

### io — 輸入輸出抽象

提供 `io-reader` 和 `io-writer` 結構體，統一檔案、標準輸入輸出等資料流的讀寫操作：

```no
; 標準檔案描述符
STDIN-FD = 0, STDOUT-FD = 1, STDERR-FD = 2

; io-reader 結構體
io-reader {
    fd i64
}
r = io-reader.from-fd(fd)      ; 從 fd 建立
r = io-reader.from-stdin()     ; 從標準輸入建立
read-n = r.read(buf, n)        ; 讀取 n 位元組
b = r.read-byte()              ; 讀取一位元組（?byte, nil=EOF）
line = r.read-line()           ; 讀取一行（?str, nil=EOF）
total = r.read-all(buf, size)  ; 讀取全部

; io-writer 結構體
io-writer {
    fd i64
}
w = io-writer.from-fd(fd)      ; 從 fd 建立
w = io-writer.from-stdout()    ; 從標準輸出建立
w = io-writer.from-stderr()    ; 從標準錯誤建立
written = w.write(data, n)     ; 寫入 n 位元組
written = w.write-str(s)       ; 寫入整個字串
written = w.write-byte(b)      ; 寫入一位元組
written = w.write-line(s)      ; 寫入字串+換行

; 便捷函數
n = io.out(s)                   ; 寫入 stdout（不換行）
n = io.outln(s)                 ; 寫入 stdout（換行）
n = io.err(s)                   ; 寫入 stderr（不換行）
n = io.errln(s)                 ; 寫入 stderr（換行）
line = io.read-line()           ; 從 stdin 讀取一行（?str, nil=EOF）
```

### regexp — 正規表示式

以 `regexp` 結構體封裝 pattern，完全使用 Nolang 實作正規表示式引擎（指令式 VM + 回溯匹配），不依賴 C 標準庫 regex.h：

```no
; 結構體
regexp {
    pattern str
}

; 方法
re = regexp{
    pattern: '^hello'
}
matched = re.matches(text)        ; 判斷是否匹配
result = re.find(text)           ; 查找第一個匹配子串
```

支援 **正則字面量語法** `/pattern/flags`（JavaScript 風格），在代碼生成階段脫糖為 `regexp-compile` 函數調用：

```no
; 正則字面量（推薦寫法）
re = /\d+/
matched = re.matches('hello 123 world')  ; true

; 帶旗標
re = /[a-z]+/gi

; 等價的顯式調用
re = regexp-compile('\\d+')
```

### process — 進程操作

提供進程創建、標準流獲取、進程等待、進程信息查詢等功能。底層使用 POSIX fork/exec/pipe/waitpid：

```no
; 信號常量
SIG-TERM = 15, SIG-KILL = 9, SIG-INT = 2, SIG-STOP = 19, SIG-CONT = 18, SIG-CHLD = 17
WNOHANG = 1

; 結構體
process {
    pid i64
    stdin-fd i64
    stdout-fd i64
    stderr-fd i64
    exit-code i64
    running i64
}

; 進程創建
p = process{}
ok = p.start(program, arg)          ; fork + exec，捕獲 stdout
ok = p.start-with-stdin(program, arg) ; fork + exec，捕獲 stdin + stdout

; 進程等待
ok = p.wait()                       ; 阻塞等待子進程結束
ok = p.wait-nohang()                ; 非阻塞輪詢

; 進程控制
ok = p.kill(sig)                    ; 發送信號
ok = p.terminate()                  ; SIG-TERM
ok = p.force-kill()                 ; SIG-KILL

; 標準流操作
read-n = p.read(buf, n)             ; 從 stdout 讀取
line = p.read-line()               ; 讀取一行（?str, nil=EOF）
content, n = p.read-all()           ; 讀取全部 stdout
written = p.write(data, n)          ; 寫入 stdin
p.close-stdin()                    ; 關閉 stdin 管道
p.close-stdout()                   ; 關閉 stdout 管道
p.close-stderr()                   ; 關閉 stderr 管道

; 進程信息
pid = p.pid-of()                    ; 子進程 ID
code = p.exit-code-of()             ; 退出碼
yes = p.is-running()                ; 是否仍在執行
pid = process.parent-pid()          ; 父進程 ID

; 生命週期
p.close()                          ; 關閉所有管道並等待

; 便捷函數
status = process.process-run(cmd)           ; 執行 shell 命令
content, code = process.process-output(program, arg) ; 執行並捕獲輸出
```

### net — 網路操作

提供 TCP 網路編程能力，包括服務端監聽、客戶端連接、資料收發等。底層使用 POSIX socket API：

```no
; 網路常量
AF-INET = 2, SOCK-STREAM = 1, SOL-SOCKET = 65535, SO-REUSEADDR = 4, BACKLOG = 128

; listener 結構體
listener {
    fd i64
}

; 監聽操作
l = listener{}
ok = l.listen(host, port)            ; 建立 TCP 監聽（socket+setsockopt+bind+listen）
c = l.accept()                       ; 接受連接（?conn, nil=無連接）
l.close()                           ; 關閉監聽 socket
fd = l.fd-of()                       ; 取得 fd

; conn 結構體
conn {
    fd i64
}

; 連接操作
c = conn{}
ok = c.dial(host, port)              ; 建立 TCP 連接（socket+connect）
written = c.send(data)               ; 發送字串
read-n = c.recv(buf, n)              ; 接收資料到 buf
line = c.recv-line()                 ; 接收一行（?str, nil=EOF, 最多 4096 位元組）
content, total = c.recv-all()        ; 接收全部直到連接關閉
c.close()                           ; 關閉連接
fd = c.fd-of()                       ; 取得 fd

; 便捷函數
l = net.net-listen-on(host, port)        ; 建立監聽器並開始監聽（?listener）
c = net.net-dial-to(host, port)          ; 建立連接並撥號（?conn）
```

### net/ip — IP 地址操作

提供 IPv4 地址的解析、驗證、轉換與分類功能。純 Nolang 實作：

```no
; 預設地址常量
IP-ZERO       ; 0.0.0.0
IP-LOOPBACK   ; 127.0.0.1
IP-ANY        ; 0.0.0.0
IP-BROADCAST  ; 255.255.255.255

; ip-addr 結構體
ip-addr {
    a i64
    b i64
    c i64
    d i64
}

; 解析與轉換
ip = ip-addr{}
ok = ip.parse('192.168.1.1')         ; 從字串解析
s = ip.to-str()                      ; 轉為字串 '192.168.1.1'
v = ip.to-u32()                      ; 轉為 u32（大端序）
ip.from-u32(v)                      ; 從 u32 建立

; 地址分類
yes = ip.is-loopback()               ; 127.0.0.0/8
yes = ip.is-private()                ; 10/8, 172.16/12, 192.168/16
yes = ip.is-zero()                   ; 0.0.0.0
yes = ip.is-broadcast()              ; 255.255.255.255
yes = ip.is-multicast()              ; 224.0.0.0/4
yes = ip.is-link-local()             ; 169.254.0.0/16
yes = ip.is-class-a()                ; A 類（1~126）
yes = ip.is-class-b()                ; B 類（128~191）
yes = ip.is-class-c()                ; C 類（192~223）

; 比較與子網
yes = ip.equal(other)                ; 地址相等比較
yes = ip.in-subnet(base, prefix-len) ; 子網包含檢查

; 便捷函數
addr = ip.ip-parse(s)                   ; 快速解析（?ip-addr, nil=無效）
yes = ip.ip-is-loopback(s)              ; 快速判斷環回
yes = ip.ip-is-private(s)               ; 快速判斷私有
```

### net/sse — Server-Sent Events 客戶端

支援 W3C EventSource 規範的 SSE 串流接收。底層使用 HTTP/1.1 長連接，支援明文 HTTP 與 HTTPS（TLS）：

```no
; sse-event 結構體
sse-event {
    event str       ; 事件類型（預設 'message'）
    data str        ; 事件資料（多行 data 以 \n 連接）
    id str          ; 事件 ID
    retry i64       ; 重連等待毫秒數（-1=未設定）
}

; sse-client 結構體
sse-client {
    fd i64              ; TCP socket fd
    tls-c tls-conn      ; TLS 連線
    use-tls bool        ; 是否使用 TLS
    connected bool      ; 連線狀態
    host str            ; 伺服器主機名
    port i64            ; 埠號
    path str            ; 請求路徑
    last-event-id str   ; 最後收到的事件 ID
    recv-buf str        ; 接收緩衝區
    recv-buf-len i64    ; 緩衝區資料長度
}

; 連接與事件接收
client = sse.sse-connect('http://host:3000/events')  ; 返回 ?sse-client
client: {
    nil -> print('connect failed')
    ->
        ev = client.next-event()     ; 返回 ?sse-event（nil=EOF, err=錯誤）
        ev: {
            nil -> print('connection closed')
            err -> print('error: ' - it)
            -> print(ev.data)
        }
        client.close()
}

; 其他方法
yes = client.is-connected()         ; 檢查連線狀態
ok = client.reconnect()             ; 重新連線（使用 last-event-id）
```

### net/http — HTTP/1.1 客戶端

提供 HTTP/1.1 協議的客戶端，支援 GET、POST、PUT、DELETE、PATCH 等方法，可選 TLS：

```no
; 結構體
http-request {
    method str
    url str
    body str
    headers [16]str
    header-count i64
}
http-response {
    status-code i64
    status-text str
    headers str
    header-names [32]str
    header-values [32]str
    header-count i64
    body str
}

; 便捷函數
resp = http.http-get(url)                        ; GET 請求（?http-response）
resp = http.http-post(url, body)                  ; POST 請求（?http-response）
resp = http.http-do(method, url, body)            ; 自訂方法（?http-response）

; 使用 request 物件
req = http-request{}
req.init('POST', url, body)
req.add-header('Content-Type', 'application/json')
resp = http.http-do-req(req)                      ; 發送請求（?http-response）

; 解析回應標頭
resp.parse-headers()
```

### net/http2 — HTTP/2.0 客戶端（RFC 7540）

支援 HTTP/2 影格解析與連線管理，支援 h2c prior knowledge 模式：

```no
; 影格結構體
http2-frame {
    length i64
    frame-type i64
    flags i64
    stream-id i64
    payload str
}

; 連線結構體
http2-conn {
    fd i64
    next-stream-id i64
    send-window i64
    recv-window i64
    initialized bool
    use-tls bool
}

; 連線與請求
c = http2.http2-connect(host, port)                ; 建立連線（?http2-conn）
resp = http2.http2-do(method, url, body)           ; 發送請求（?http-response）

; 影格操作
frame = http2-frame{}
pos = frame.parse(data, pos)                 ; 解析影格（?i64）
pos = frame.serialize(buf, pos)              ; 序列化影格
ok = c.send-frame(frame)                     ; 發送影格
frame = c.recv-frame()                       ; 接收影格（?http2-frame）
```

### net/http3 — HTTP/3.0 客戶端（RFC 9114）

基於 QUIC 協議的 HTTP/3 客戶端：

```no
; 方法常量
HTTP3-METHOD-GET = 'GET'
HTTP3-METHOD-POST = 'POST'
HTTP3-METHOD-PUT = 'PUT'
HTTP3-METHOD-DELETE = 'DELETE'
HTTP3-METHOD-PATCH = 'PATCH'
HTTP3-METHOD-HEAD = 'HEAD'
HTTP3-METHOD-OPTIONS = 'OPTIONS'

; 便捷函數
c = http3.http3-connect(host, port)                ; 建立 QUIC 連線（?http3-conn）
resp = http3.http3-send-request(c, method, path, headers, body) ; 發送請求（?http-response）
resp = http3.http3-get(url)                        ; GET 請求（?http-response）
resp = http3.http3-post(url, body)                 ; POST 請求（?http-response）

; QPACK 標頭編解碼
buf, n = http3.qpack-encode-header(name, value)
buf, n = http3.qpack-encode-headers(names, values, count)
name, value, pos = http3.qpack-decode-header(buf, pos)
```

### net/ws — WebSocket 客戶端與服務端（RFC 6455）

支援 WebSocket 協議的全雙工通訊，可作為客戶端或服務端：

```no
; 訊息結構體
ws-message {
    opcode i64           ; 0=continuation, 1=text, 2=binary, 8=close, 9=ping, 10=pong
    data str
    fin bool
}

; 服務端
s = ws.ws-listen-on(host, port)                 ; 建立監聽（?ws-server）
c = s.accept()                               ; 接受連接（?ws-server-conn）
msg = c.recv()                               ; 接收訊息（?ws-message）
ok = c.send-text(text)                       ; 發送文字
ok = c.send-binary(data)                     ; 發送二進制
c.close()

; 客戶端
c = ws.ws-connect(url)                          ; 連接服務端（?ws-client）
msg = c.recv()                               ; 接收訊息（?ws-message）
ok = c.send-text(text)                       ; 發送文字
ok = c.send-binary(data)                     ; 發送二進制
c.close()
```

### net/tls — TLS 1.2/1.3 客戶端（純 Nolang 實現）

提供 TLS 加密連接，支援 TLS 1.2 和 1.3：

```no
; 連接
c = tls.tls-dial(host, port)                     ; 建立 TLS 連接（?tls-conn）
n = c.send(data)                             ; 發送加密資料（?i64）
n = c.recv(buf, n)                           ; 接收解密資料（?i64）
c.close()
```

### net/client — 高階 TCP 客戶端

封裝 `conn` 結構體，提供自動重連等功能：

```no
c = client.net-client(host, port)                   ; 建立客戶端（?client）
ok = c.connect(host, port)                   ; 連接
ok = c.reconnect()                           ; 重連
written = c.send(data)                       ; 發送
read-n = c.recv(buf, n)                      ; 接收
line = c.recv-line()                         ; 接收一行（?str）
response = c.request(data)                   ; 請求-回應模式（?str）
yes = c.is-connected()                       ; 連接狀態
c.close()
```

### net/quic — QUIC 協議（RFC 9000）

提供 QUIC 傳輸協議實現，作為 HTTP/3 的底層傳輸層：

```no
c = quic.quic-dial(host, port)                    ; 建立 QUIC 連接（?quic-conn）
n = c.send(data, n)                          ; 發送資料
n = c.recv(buf, n)                           ; 接收資料
c.close()
```

### net/server — HTTP 伺服器

提供 HTTP 伺服器功能：

```no
s = server{}
ok = s.listen(host, port)                    ; 開始監聽
ok = s.serve()                               ; 處理請求
s.close()
```

### net/dns — DNS 解析

提供 DNS 查詢功能：

```no
ip = dns.dns-resolve(host)                       ; 解析主機名（?str）
```

### net/url — URL 解析

提供 URL 解析與建構功能：

```no
u = url.url-parse(url)                           ; 解析 URL
s = u.to-str()                               ; 轉為字串
```

### net/cookie — HTTP Cookie

提供 HTTP Cookie 的解析與管理：

```no
c = cookie{}
c.parse(set-cookie-header)
s = c.to-str()
```

### net/multipart — Multipart 表單資料

提供 multipart/form-data 的解析與建構：

```no
out = multipart.multipart-encode(fields, boundary)
fields = multipart.multipart-parse(data, boundary)
```

### net/hpack — HPACK 標頭壓縮（HTTP/2）

提供 HPACK 演算法的編解碼，用於 HTTP/2 標頭壓縮：

```no
buf, n = hpack.hpack-encode(headers)
headers = hpack.hpack-decode(buf, n)
```

### net/proxy — 代理支援

提供 HTTP/SOCKS 代理連接功能：

```no
c = proxy.proxy-dial(proxy-url, target-host, target-port)
```

### net/pool — 連接池

提供網路連接的池化管理，重用連接以提升效能：

```no
p = pool{}
p.init(capacity)
c = p.get()                                  ; 從池中取得連接
p.put(c)                                     ; 歸還連接
p.close()
```

### net/unix — Unix 域套接字

提供 Unix 域套接字通訊：

```no
fd = unix.unix-listen(path)                       ; 監聽
fd = unix.unix-dial(path)                         ; 連接
fd = unix.unix-accept(listen-fd)                  ; 接受連接
```

---

## 時間與日期

### time — 時間操作

```no
sec = time.now-s()                   ; 目前 Unix 時間戳（秒）
ms = time.now-ms()                   ; 目前時間戳（毫秒）
us = time.now-us()                   ; 目前時間戳（微秒）
out = time.format-time(t, fmt)        ; 格式化時間
time.sleep-ms(ms)                    ; 睡眠（毫秒）
time.sleep-us(us)                    ; 睡眠（微秒）
d = time.duration-between(start, end) ; 耗時（秒）
d = time.duration-ms-between(s, e)    ; 耗時（毫秒）
```

---

## 日誌

### log — 分級日誌

```no
LEVEL-DEBUG = 0
LEVEL-INFO  = 1
LEVEL-WARN  = 2
LEVEL-ERROR = 3
LEVEL-FATAL = 4

log.set-level(lvl)
log.debug(msg)
log.info(msg)
log.warn(msg)
log.error(msg)
log.fatal(msg)
```

---

## 資料結構

### set — 集合（基於陣列）

```no
new-n = set.add(s, n, val)           ; 新增元素
new-n = set.set-remove(s, n, val)        ; 移除元素
ok = set.contains(s, n, val)         ; 是否包含
new-an = set.union(a, an, b, bn)     ; 聯集
out, n = set.intersection(a, an, b, bn); 交集
out, n = set.difference(a, an, b, bn)  ; 差集
v = set.to-vec(s, n)                 ; 轉切片
sz = set.set-size(s, n)                   ; 元素個數
yes = set.set-empty(s, n)                    ; 是否為空
```

### deque — 雙端佇列

使用循環緩衝區實作的雙端佇列，以 `deque` 結構體封裝：

```no
; 結構體
deque {
    buf []i64
    cap i64
    head i64
    tail i64
}

; 初始化
d = deque{
    buf: buf
    cap: 128
    head: 0
    tail: 0
}

; 方法
d.push-front(val)              ; 從前端推入
d.push-back(val)               ; 從後端推入
val = d.pop-front()             ; 從前端彈出
val = d.pop-back()              ; 從後端彈出
val = d.peek-front()            ; 查看前端元素（?i64, nil=空）
val = d.peek-back()             ; 查看後端元素（?i64, nil=空）
sz = d.size()                   ; 大小
yes = d.empty()                 ; 是否為空
d.clear()                      ; 清空
```

### heap — 最小堆

以 `heap` 結構體封裝的二元最小堆積：

```no
; 結構體
heap {
    data []i64
    n i64
}

; 初始化
h = heap.init(data)            ; 建立堆積

; 方法
h.push(val)                    ; 推入元素
val = h.pop()                  ; 彈出最小元素（?i64, nil=空）
val = h.peek()                 ; 查看最小元素（?i64, nil=空）
sz = h.size()                  ; 大小
yes = h.empty()                ; 是否為空
```

### stack — 堆疊

後進先出（LIFO）資料結構，以 `stack` 結構體封裝：

```no
; 結構體
stack {
    data []i64
    n i64
}

; 初始化
buf [128]i64 = [0:128]
s = stack{
    data: buf
    n: 0
}

; 方法
s.push(val)                    ; 推入元素
val = s.pop()                  ; 彈出頂端元素（?i64, nil=空）
val = s.peek()                 ; 查看頂端元素（?i64, nil=空）
sz = s.size()                  ; 大小
yes = s.empty()                ; 是否為空
s.clear()                      ; 清空
```

### map/linked-hash-map — 有序哈希表

固定容量 64（i64→i64），線性探測，雙向鏈表保持插入順序：

```no
m = linked-hash-map{}
m.init()
m.put(key, val)
result = m.get(key)   ; ?i64, nil=未找到
found = m.contains(key)
removed = m.remove(key)
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(key, val)
```

### map/hash-set — i64 哈希集合

固定容量 64，線性探測，O(1) 查找/插入/刪除：

```no
s = hash-set{}
s.init()
is-new = s.add(val)
found = s.contains(val)
removed = s.remove(val)
s.clear()
n = s.len()
empty = s.is-empty()
s.for-each(val)
```

### map/str-map — str→str 哈希映射表

固定容量 256，FNV-1a 雜湊，線性探測：

```no
m = str-map{}
m.init()
m.put('key', 'val')
result = m.get('key')   ; ?str, nil=未找到
found = m.contains('key')
removed = m.remove('key')
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(k, v)
```

### map/str-set — str 哈希集合

固定容量 256，FNV-1a 雜湊，字串去重：

```no
s = str-set{}
s.init()
is-new = s.add('hello')
found = s.contains('hello')
removed = s.remove('hello')
s.clear()
n = s.len()
empty = s.is-empty()
s.for-each(val)
```

### map/tree-map — 有序映射表（AVL 樹）

基於 AVL 自平衡二元搜尋樹實現的有序映射表（i64→i64），容量 64：

```no
m = tree-map{}
m.clear()                           ; 初始化
ok = m.put(key, val)                ; 插入或更新
val = m.get(key)                    ; 查找（?i64, nil=未找到）
yes = m.contains(key)               ; 檢查鍵是否存在
ok = m.remove(key)                  ; 刪除鍵
key = m.first()                     ; 最小鍵（?i64）
key = m.last()                      ; 最大鍵（?i64）
key = m.lower-bound(target)         ; 第一個 ≥ target 的鍵（?i64）
key = m.upper-bound(target)         ; 第一個 > target 的鍵（?i64）
m.for-each(k, v)                    ; 按鍵升序遍歷
sz = m.size()
yes = m.empty()
yes = m.full()
```

### map/tree-set — 有序集合（AVL 樹）

基於 AVL 自平衡二元搜尋樹實現的有序集合（i64），容量 64：

```no
s = tree-set{}
s.clear()                           ; 初始化
ok = s.add(key)                     ; 加入元素
yes = s.contains(key)               ; 檢查是否存在
ok = s.remove(key)                  ; 刪除元素
val = s.first()                     ; 最小值（?i64）
val = s.last()                      ; 最大值（?i64）
val = s.lower-bound(target)         ; 第一個 ≥ target 的元素（?i64）
val = s.upper-bound(target)         ; 第一個 > target 的元素（?i64）
s.for-each(val)                     ; 按升序遍歷
sz = s.size()
yes = s.empty()
yes = s.full()
```

### collection/queue — 泛型佇列（環形緩衝區）

基於定長陣列的環形緩衝區實現，緩衝區由 `[n]t` 接收者提供：

```no
buf [128]i64 = [0:128]
q = buf.queue-init()
ok = buf.queue-push(q, val)         ; 推入尾端
val = buf.queue-pop(q)              ; 從前端彈出（?t）
val = buf.queue-peek(q)             ; 查看隊首（?t）
sz = q.size()
yes = q.empty()
yes = q.full()
q.clear()
```

### collection/arr-stack — 泛型堆疊（基於定長陣列）

基於定長陣列的堆疊實現，緩衝區由 `[n]t` 接收者提供：

```no
buf [128]i64 = [0:128]
s = buf.arr-stack-init()
ok = buf.arr-stack-push(s, val)     ; 推入
val = buf.arr-stack-pop(s)          ; 彈出（?t）
val = buf.arr-stack-peek(s)         ; 查看頂端（?t）
sz = s.size()
yes = s.empty()
yes = s.full()
s.clear()
```

### collection/link — 泛型雙向鏈結串列

基於定長陣列節點池的雙向鏈結串列，值由 `[n]t` 接收者提供：

```no
buf [128]i64 = [0:128]
nxt [128]i64 = [0:128]
prv [128]i64 = [0:128]
l = buf.link-init(nxt, prv)
ok = buf.link-push-front(l, val)    ; 插入頭部
ok = buf.link-push-back(l, val)     ; 插入尾部
val = buf.link-pop-front(l)         ; 彈出頭部（?t）
val = buf.link-pop-back(l)          ; 彈出尾部（?t）
val = buf.link-peek-front(l)        ; 查看頭部（?t）
val = buf.link-peek-back(l)         ; 查看尾部（?t）
sz = l.size()
yes = l.empty()
yes = l.full()
```

---

## 資料庫

### database/sql — 資料庫存取介面

定義資料庫連線、查詢、預編譯陳述式的標準介面，由具體驅動實現：

```no
; 執行結果
result {
    last-id i64
    affected i64
}

; 連線介面（enter/leave 自動管理）
db enter, leave {
    close() (ok bool)
    exec(sql str) (r result)
    query(sql str) (rs rows)
    prepare(sql str) (s stmt)
}

; 結果集介面
rows enter, leave {
    next() (ok bool)                    ; 迭代下一行
    scan-int(col i64) (v i64)           ; 讀取整數
    scan-str(col i64) (v str)           ; 讀取字串
    scan-float(col i64) (v f64)         ; 讀取浮點數
    close() (ok bool)
}

; 預編譯陳述式介面
stmt enter, leave {
    bind-int(idx i64, v i64) (ok bool)
    bind-str(idx i64, v str) (ok bool)
    bind-bool(idx i64, v bool) (ok bool)
    exec() (r result)
    query() (rs rows)
    close() (ok bool)
}
```

---

## 編碼

### encoding/hex — 十六進制

```no
; 編碼（定義於 byte 模組）
out = data.to-hex()                  ; []byte → 大寫 hex str
out = data.to-hex-lower()            ; []byte → 小寫 hex str

; 解碼（定義於 str 模組）
out = s.from-hex()                   ; hex str → ?[]byte（nil=空, err=無效字元）
```

### encoding/base64 — Base64（RFC 4648）

```no
BASE64-STD = 'ABC...+/'
BASE64-URL = 'ABC...-_'
PAD = 61  ; '='

out-n = base64.encode(data, n, table, out)    ; Base64 編碼
out-n = base64.encode-std(data, n, out)       ; 標準編碼
out-n = base64.encode-url(data, n, out)       ; URL 安全編碼
out-n = base64.decode(s, n, table, out)   ; Base64 解碼（?i64, nil=無效輸入）
```

### encoding/csv — CSV 解析（RFC 4180）

```no
fn, new-pos = csv.parse-field(s, sn, pos, field)  ; 解析單個欄位
n = csv.parse-line(s, sn, fields, max)             ; 解析一行
out-n = csv.encode-field(field, fn, out)           ; 編碼欄位
```

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

---

## 密碼學與雜湊

### hash/aes — AES-128 加解密（ECB 模式）

```no
aes.aes-128-enc(plain, 16, key, out)   ; 加密 16-byte 區塊
aes.aes-128-dec(cipher, 16, key, out)  ; 解密 16-byte 區塊
```

另含獨立模組 `hash/aes-128-enc` 和 `hash/aes-128-dec`。

### hash/des — DES 加解密（ECB 模式）

```no
des.des-enc(plain, 8, key, out)        ; 加密 8-byte 區塊
des.des-dec(cipher, 8, key, out)       ; 解密 8-byte 區塊
```

另含獨立模組 `hash/des-enc` 和 `hash/des-dec`。

### hash/rsa — RSA 模冪運算

```no
rsa.rsa-modpow(base, bn, exp, en, mod, mn, result, rn)
```

不包含金鑰生成，支援 1024~4096-bit。

### hash/md5 — MD5（128-bit）

```no
out [16]byte = md5.md5(data)
```

### hash/sha1 — SHA-1（160-bit）

```no
hash = sha1.sha1(data []byte) (hash [20]byte)
hex = sha1.sha1-hex(data []byte) (hex str)
sha1.sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32)
```

`sha1` 計算完整雜湊（含填充與多區塊處理），返回 20 位元組。
`sha1-hex` 同上但返回 40 字元小寫 hex 字串。
`sha1-block` 為低階 API，處理單個 512-bit 區塊。

### hash/sha256 — SHA-256（256-bit）

```no
sha256.sha256(data []byte) (hash [32]byte)
sha256.sha256-hex(data []byte) (hex str)
sha256.sha256-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32, h5 u32, h6 u32, h7 u32)
```

`sha256` 計算完整雜湊（含填充與多區塊處理），返回 32 位元組。
`sha256-hex` 同上但返回 64 字元小寫 hex 字串。
`sha256-block` 為低階 API，處理單個 512-bit 區塊。

### hash/sha512 — SHA-512（512-bit）

```no
sha512.sha512(data []byte) (hash [64]byte)
sha512.sha512-hex(data []byte) (hex str)
sha512.sha512-block(s []u64, h0 u64, h1 u64, h2 u64, h3 u64, h4 u64, h5 u64, h6 u64, h7 u64)
```

`sha512` 計算完整雜湊（含填充與多區塊處理），返回 64 位元組。
`sha512-hex` 同上但返回 128 字元小寫 hex 字串。
`sha512-block` 為低階 API，處理單個 1024-bit 區塊。

### hash/crc-32 — CRC32 校驗

```no
crc-32.crc-32(s []byte, n, crc)
```

### hash/fnv-1a-32 — FNV-1a 非加密雜湊

```no
fnv-1a-32.fnv-1a-32(s []byte, n, h)
```

### hash/rand — 隨機數產生器（xorshift32）

```no
r = rand.rand(state)                     ; 32-bit 偽隨機數
rand.rand-str(state, n, s)              ; 隨機字母數字字串
```

### hash/x509 — X.509 憑證 DER 解析

```no
tag = x509.der-tag(data, pos)
len, adv = x509.der-len(data, pos)
x509.x509-fingerprint(cert, n, h0..h7)  ; SHA-256 憑證指紋
x509.x509-rsa-e(cert, n, e)             ; RSA 公鑰指數提取
```

### hash/aes-256 — AES-256 加解密（ECB 模式）

```no
aes-256.aes-256-enc(in [16]byte, key [32]byte) (out [16]byte)   ; 加密
aes-256.aes-256-dec(in [16]byte, key [32]byte) (out [16]byte)   ; 解密
```

### hash/aes-cbc — AES-CBC 模式（含 PKCS7 填充）

```no
out = aes-cbc.aes-128-cbc-enc(in []byte, key [16]byte, iv [16]byte)
out = aes-cbc.aes-128-cbc-dec(in []byte, key [16]byte, iv [16]byte)
out = aes-cbc.pkcs7-pad(in []byte)
n = aes-cbc.pkcs7-unpad(in []byte)
```

### hash/aes-256-cbc — AES-256-CBC 加解密

```no
out = aes-256-cbc.aes-256-cbc-enc(in []byte, key [32]byte, iv [16]byte)
out = aes-256-cbc.aes-256-cbc-dec(in []byte, key [32]byte, iv [16]byte)
```

### hash/aes-ctr — AES-CTR 計數器模式

```no
out = aes-ctr.aes-128-ctr(in []byte, key [16]byte, iv [16]byte)
out = aes-ctr.aes-256-ctr(in []byte, key [32]byte, iv [16]byte)
```

### hash/aes-gcm — AES-GCM AEAD

```no
; AES-128-GCM
sealed = aes-gcm.aes-128-gcm-seal(key [16]byte, iv [12]byte, aad []byte, plain []byte)
plain = aes-gcm.aes-128-gcm-open(key [16]byte, iv [12]byte, aad []byte, sealed []byte)
```

### hash/aes-256-gcm — AES-256-GCM AEAD（NIST SP 800-38D）

```no
sealed = aes-256-gcm.aes-256-gcm-seal(key [32]byte, iv [12]byte, aad []byte, plain []byte)
plain = aes-256-gcm.aes-256-gcm-open(key [32]byte, iv [12]byte, aad []byte, sealed []byte)
```

### hash/hmac — HMAC 訊息認證碼

```no
out = hmac.hmac(key []byte, key-n i64, msg []byte, msg-n i64, block-size i64) (out [32]byte)
```

### hash/hkdf — HKDF 金鑰推導（RFC 5869）

```no
ok = hkdf.hkdf-extract(salt []byte, salt-n i64, ikm []byte, ikm-n i64, prk []byte)
ok = hkdf.hkdf-expand(prk []byte, prk-n i64, info []byte, info-n i64, out []byte, out-n i64)
```

### hash/pbkdf2 — PBKDF2 金鑰推導（RFC 2898）

```no
pbkdf2.pbkdf2(password []byte, pw-n i64, salt []byte, salt-n i64, iter i64, out []byte, out-n i64)
```

### hash/argon2 — Argon2 記憶體硬金鑰推導

```no
argon2.argon2id(password []byte, pw-n i64, salt []byte, salt-n i64, time i64, memory i64, parallel i64, out []byte, out-n i64)
```

### hash/scrypt — scrypt 金鑰推導

```no
scrypt.scrypt(password []byte, pw-n i64, salt []byte, salt-n i64, n i64, r i64, p i64, out []byte, out-n i64)
```

### hash/sha224 — SHA-224（224-bit）

```no
hash = sha224.sha224(data []byte) (hash [28]byte)
hex = sha224.sha224-hex(data []byte) (hex str)
```

### hash/sha384 — SHA-384（384-bit）

```no
hash = sha384.sha384(data []byte) (hash [48]byte)
hex = sha384.sha384-hex(data []byte) (hex str)
```

### hash/sha3 — SHA-3（Keccak）

```no
hash = sha3.sha3-256(data []byte) (hash [32]byte)
hash = sha3.sha3-512(data []byte) (hash [64]byte)
```

### hash/blake2 — BLAKE2 雜湊

```no
hash = blake2.blake2b-256(data []byte) (hash [32]byte)
hash = blake2.blake2b-512(data []byte) (hash [64]byte)
```

### hash/crc-16 — CRC16 校驗

```no
crc = crc-16.crc-16(data []byte, n i64) (crc i64)
```

### hash/crc-64 — CRC64 校驗

```no
crc = crc-64.crc-64(data []byte, n i64) (crc i64)
```

### hash/fnv — FNV-1 雜湊

```no
h = fnv.fnv-1-32(data []byte, n i64) (h i64)
h = fnv.fnv-1a-64(data []byte, n i64) (h i64)
```

### hash/base32 — Base32 編解碼（RFC 4648）

```no
out = base32.base32-encode(data []byte, n i64) (out str)
out = base32.base32-decode(s str, n i64) (out []byte)
```

### hash/chacha20-poly1305 — ChaCha20-Poly1305 AEAD

```no
sealed = chacha20-poly1305.chacha20-poly1305-seal(key [32]byte, nonce [12]byte, aad []byte, plain []byte)
plain = chacha20-poly1305.chacha20-poly1305-open(key [32]byte, nonce [12]byte, aad []byte, sealed []byte)
```

### hash/rc4 — RC4 串流加密

```no
out = rc4.rc4(key []byte, key-n i64, data []byte, data-n i64) (out []byte)
```

### hash/tdes — 三重 DES（3DES）

```no
tdes.tdes-enc(plain, 8, key [24]byte, out)
tdes.tdes-dec(cipher, 8, key [24]byte, out)
```

### hash/ecdsa — ECDSA 數位簽章

```no
ok = ecdsa.ecdsa-sign(priv-key []byte, msg []byte, msg-n i64, r []byte, s []byte)
ok = ecdsa.ecdsa-verify(pub-key []byte, msg []byte, msg-n i64, r []byte, s []byte) (ok bool)
```

### hash/ed25519 — Ed25519 數位簽章

```no
pub = ed25519.ed25519-derive-public(priv [32]byte) (pub [32]byte)
sig = ed25519.ed25519-sign(priv [32]byte, msg []byte, msg-n i64) (sig [64]byte)
ok = ed25519.ed25519-verify(pub [32]byte, msg []byte, msg-n i64, sig [64]byte) (ok bool)
```

### hash/x25519 — X25519 金鑰交換

```no
pub = x25519.x25519-derive-public(priv [32]byte) (pub [32]byte)
shared = x25519.x25519-derive-shared(priv [32]byte, peer-pub [32]byte) (shared [32]byte)
```

### hash/rand-str — 隨機字串產生

```no
rand-str.rand-str(state i64, n i64, s str)   ; 產生長度 n 的隨機字母數字字串
```

---

## 資料交換

### json — JSON 解析與產生

```no
; 型別枚舉
json-kind {
    null,
    bool,
    num,
    str,
    arr,
    obj,
}

; 解析
v = json.parse(s, n)          ; 完整解析
v = json.parse-str(s, n)                 ; 解析字串值
v = json.parse-num(s, n)                 ; 解析數值值

; 產生
n = json.stringify(v, out)    ; 序列化

; 存取
val = json.get-key(v, key)    ; 取得物件屬性
json.set-key(v json-value, key, val)    ; 設定物件屬性
```

---

## 其他

### unicode — Unicode 支援

Unicode 相關功能已分散至 `char` 和 `str` 模組：

- 字元分類（`is-letter`, `is-digit`, `is-upper` 等）→ 見 `char` 模組
- UTF-8 編解碼（`char.to-bytes`, `char.to-str`）→ 見 `char` 模組
- 字串 rune 計數（`str.count`）→ 見 `str` 模組

### uuid — UUID v4 產生與解析

```no
out = uuid.new-v4(state)                  ; 產生 UUID v4
out-n = uuid.to-str(out)             ; 轉小寫字串（方法）
out-n = uuid.to-str-upper(out)       ; 轉大寫字串（方法）
ok = uuid.from-str(s, sn, out)            ; 從字串解析（支援連字號/不帶）
ok = uuid.parse-with-dashes(s, pos, out)  ; 含連字號解析
ok = uuid.parse-no-dashes(s, pos, out)    ; 無連字號解析
ok = uuid.validate()                 ; 驗證 UUID 格式（方法）
v = uuid.version()                   ; 取得版本（方法）
v = uuid.variant()                   ; 取得變體（方法）
yes = uuid.is-nil()                  ; 是否為 nil（方法）
yes = uuid.eq(b)                     ; 相等比較（方法）
r = uuid.cmp(b)                      ; 比較（方法）
uuid.nil-uuid(out)                        ; 回傳 nil UUID
```

### bigint — 任意精度整數

```no
; 型別
bigint {
    sign i64
    limbs []i64
    len i64
}

; 建構
out = bigint.from-i64(v)
out = bigint.from-u64(v)
out = bigint.zero()
out = bigint.one()
out = bigint.copy(a)

; 比較
r = bigint.cmp(a, b)
r = bigint.eq(a, b)
r = bigint.is-zero(a)
r = bigint.is-neg(a)
r = bigint.is-pos(a)

; 運算
c = bigint.add(a, b)
c = bigint.sub(a, b)
c = bigint.mul(a, b)
q, r = bigint.div-mod(a, b)
r = bigint.mod(a, b)
q = bigint.div-i64(a, v)
r = bigint.mod-i64(a, v)
c = bigint.pow(a, n)
r = bigint.mod-pow(base, exp, mod, r)

; 數論
bigint.gcd(a, b, g)
bigint.lcm(a, b, l)

; 移位
bigint.shl(a, n, c)
bigint.shr(a, n, c)

; 字串轉換
n = bigint.to-str(a, out)
out = bigint.from-str(s, sn)
n = bigint.to-hex(a, out)
out = bigint.from-hex(s, sn)

; 小整數輔助
bigint.add-i64(a, v, c)
bigint.mul-i64(a, v, c)
```

### err — 錯誤處理

結構化錯誤型別與工具函式：

```no
; 錯誤碼枚舉
code {
    ok,
    not-found,
    permission,
    io,
    timeout,
    parse,
    invalid,
    overflow,
}

; 結構體
error {
    code code
    msg str
}

; 函數
e = err.new(code.io, msg)            ; 建立錯誤
e = err.err-from-errno(errno)         ; 從 C errno 建立
yes = e.is(code.io)                  ; 判斷錯誤碼
msg = e.msg()                       ; 取得錯誤訊息
c = e.code()                        ; 取得錯誤碼
s = e.format()                       ; 格式化為字串
```

### bool — 布爾型別

```no
bool.to-str() (out str)     ; true→"true", false→"false"（方法）
```

### enter / leave — 生命週期鉤子

```no

; 啟動時執行
enter { 
    enter()
}     

; 退出時執行
leave {
    leave()
}     
```

---

## 模組一覽

| 模組                | 路徑   | 說明             |
| ------------------- | ------ | ---------------- |
| fmt                 | 核心   | 格式化輸出       |
| math                | 核心   | 數學函數         |
| str                 | 核心   | 字串操作         |
| vec                 | 核心   | 切片（[]t）操作  |
| arr                 | 核心   | 陣列（[n]t）操作 |
| number              | 核心   | 數值工具函數     |
| byte                | 核心   | 位元組操作       |
| char                | 核心   | 字元操作（方法） |
| os                  | 核心   | 作業系統介面     |
| env                 | 核心   | 環境變數封裝     |
| fs                  | 核心   | 檔案系統工具     |
| io                  | 核心   | 輸入輸出抽象     |
| args                | 核心   | 命令列引數       |
| path                | 核心   | 路徑處理（結構體）|
| bufio               | 核心   | 緩衝讀取         |
| time                | 核心   | 時間操作         |
| log                 | 核心   | 分級日誌         |
| json                | 核心   | JSON 解析/產生   |
| types               | 核心   | 型別定義文件     |
| option              | 核心   | 選項型別         |
| sort                | 核心   | 排序常量         |
| set                 | 核心   | 集合             |
| deque               | 核心   | 雙端佇列（結構體）|
| heap                | 核心   | 最小堆（結構體） |
| stack               | 核心   | 堆疊（結構體）   |
| regexp              | 核心   | 正規表示式       |
| process             | 核心   | 進程操作         |
| unicode             | 核心   | Unicode 說明     |
| uuid                | 核心   | UUID v4          |
| bigint              | 核心   | 任意精度整數     |
| bool                | 核心   | 布爾型別         |
| err                 | 核心   | 錯誤處理         |
| enter               | 核心   | 啟動鉤子         |
| leave               | 核心   | 退出鉤子         |
| net                 | 核心   | TCP 網路操作     |
| net/http            | 子模組 | HTTP/1.1 客戶端  |
| net/http2           | 子模組 | HTTP/2.0 客戶端  |
| net/http3           | 子模組 | HTTP/3.0 客戶端  |
| net/ws              | 子模組 | WebSocket        |
| net/quic            | 子模組 | QUIC 協議        |
| net/tls             | 子模組 | TLS 1.2/1.3     |
| net/sse             | 子模組 | SSE 客戶端       |
| net/client          | 子模組 | 高階 TCP 客戶端  |
| net/server          | 子模組 | HTTP 伺服器      |
| net/dns             | 子模組 | DNS 解析         |
| net/url             | 子模組 | URL 解析         |
| net/cookie          | 子模組 | HTTP Cookie      |
| net/multipart       | 子模組 | Multipart 表單   |
| net/hpack           | 子模組 | HPACK 標頭壓縮   |
| net/proxy           | 子模組 | 代理支援         |
| net/pool            | 子模組 | 連接池           |
| net/unix            | 子模組 | Unix 域套接字    |
| net/ip              | 子模組 | IP 地址操作      |
| encoding/hex        | 子模組 | 十六進制編解碼   |
| encoding/base64     | 子模組 | Base64 編解碼    |
| encoding/csv        | 子模組 | CSV 解析         |
| archive/tar         | 子模組 | TAR 歸檔         |
| archive/zip         | 子模組 | ZIP 歸檔         |
| archive/gzip        | 子模組 | GZIP 壓縮        |
| map/linked-hash-map | 子模組 | 有序哈希表       |
| map/hash-set        | 子模組 | i64 哈希集合     |
| map/str-map         | 子模組 | str→str 哈希映射 |
| map/str-set         | 子模組 | str 哈希集合     |
| map/tree-map        | 子模組 | AVL 有序映射     |
| map/tree-set        | 子模組 | AVL 有序集合     |
| collection/queue    | 子模組 | 泛型佇列         |
| collection/arr-stack| 子模組 | 泛型堆疊         |
| collection/link     | 子模組 | 泛型雙向鏈結串列 |
| database/sql        | 子模組 | 資料庫存取介面   |
| hash/aes            | 子模組 | AES-128 加解密   |
| hash/aes-128-enc    | 子模組 | AES-128 加密     |
| hash/aes-128-dec    | 子模組 | AES-128 解密     |
| hash/aes-256        | 子模組 | AES-256 加解密   |
| hash/aes-cbc        | 子模組 | AES-CBC 模式     |
| hash/aes-256-cbc    | 子模組 | AES-256-CBC     |
| hash/aes-ctr        | 子模組 | AES-CTR 模式     |
| hash/aes-gcm        | 子模組 | AES-GCM AEAD    |
| hash/aes-256-gcm    | 子模組 | AES-256-GCM     |
| hash/des            | 子模組 | DES 加解密       |
| hash/des-enc        | 子模組 | DES 加密         |
| hash/des-dec        | 子模組 | DES 解密         |
| hash/tdes           | 子模組 | 三重 DES         |
| hash/rsa            | 子模組 | RSA 模冪         |
| hash/md5            | 子模組 | MD5 雜湊         |
| hash/sha1           | 子模組 | SHA-1 雜湊       |
| hash/sha224         | 子模組 | SHA-224 雜湊     |
| hash/sha256         | 子模組 | SHA-256 雜湊     |
| hash/sha384         | 子模組 | SHA-384 雜湊     |
| hash/sha512         | 子模組 | SHA-512 雜湊     |
| hash/sha3           | 子模組 | SHA-3 雜湊       |
| hash/blake2         | 子模組 | BLAKE2 雜湊      |
| hash/crc-16         | 子模組 | CRC16 校驗       |
| hash/crc-32         | 子模組 | CRC32 校驗       |
| hash/crc-64         | 子模組 | CRC64 校驗       |
| hash/fnv            | 子模組 | FNV-1 雜湊       |
| hash/fnv-1a-32      | 子模組 | FNV-1a 雜湊      |
| hash/hmac           | 子模組 | HMAC 認證碼      |
| hash/hkdf           | 子模組 | HKDF 金鑰推導    |
| hash/pbkdf2         | 子模組 | PBKDF2 金鑰推導  |
| hash/argon2         | 子模組 | Argon2 金鑰推導  |
| hash/scrypt         | 子模組 | scrypt 金鑰推導  |
| hash/chacha20-poly1305 | 子模組 | ChaCha20-Poly1305 |
| hash/rc4            | 子模組 | RC4 串流加密     |
| hash/ecdsa          | 子模組 | ECDSA 簽章       |
| hash/ed25519        | 子模組 | Ed25519 簽章     |
| hash/x25519         | 子模組 | X25519 金鑰交換  |
| hash/base32         | 子模組 | Base32 編解碼    |
| hash/rand           | 子模組 | 隨機數產生器     |
| hash/rand-str       | 子模組 | 隨機字串產生     |
| hash/x509           | 子模組 | X.509 DER 解析   |
