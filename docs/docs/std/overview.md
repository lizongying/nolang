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

```nolang
x ?t                // 宣告 option<t>
x = 42              // 設為有值
x = nil             // 設為空
x = err('msg')      // 設為錯誤

// match
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

```nolang
printf(fmt str, ...)    // 格式化輸出
print(...)              // 列印並換行
println-empty()         // 列印空行
```

### math — 數學函數

**常量：** `PI`, `E`

**基礎：** `abs`, `sqrt`

**三角：** `sin`, `cos`, `tan`, `asin`, `acos`, `atan`, `atan2`, `degrees`, `radians`

**雙曲：** `sinh`, `cosh`, `tanh`

**取整：** `ceil`, `floor`, `round`, `trunc`

**指數/對數：** `exp`, `log`, `log10`, `log2`, `pow`, `hypot`, `cbrt`

**其他：** `fmod`, `max`, `min`

### strconv — 字串轉換

```nolang
// 字串 → 數值
str.to-i8()
str.to-i16()
str.to-i32()
str.to-i64()
str.to-u8()
str.to-u16()
str.to-u32()
str.to-u64()
str.to-f32()
str.to-f64()
str.to-bool()
str.to-byte()
str.to-char()

// 數值 → 字串
i8.to-str()
i16.to-str()
i32.to-str()
i64.to-str()
u8.to-str()
u16.to-str()
u32.to-str()
u64.to-str()
f32.to-str()
f64.to-str()
bool.to-str()
byte.to-str()
char.to-str()
```

### char — 字元操作

char 本質為 i32（Unicode 碼點），所有操作以方法形式提供：

```nolang
c char = 'A'
c.is-digit()       // 是否為數字 (0-9)（方法）
c.is-letter()      // 是否為字母 (a-z, A-Z)（方法）
c.is-alpha()       // is-letter 別名（方法）
c.is-alnum()       // 是否為字母或數字（方法）
c.is-space()       // 是否為空白字元（方法）
c.is-upper()       // 是否為大寫字母（方法）
c.is-lower()       // 是否為小寫字母（方法）
c.to-upper()       // 轉大寫（ASCII）（方法）
c.to-lower()       // 轉小寫（ASCII）（方法）
c.to-bytes()       // Unicode → UTF-8 位元組（方法）
c.to-str()         // Unicode → 字串（UTF-8，方法）
```

### str — 字串操作

```nolang
ok = a.eq(b, n)               // 相等比較（方法）
dst = s.copy()                // 字串複製（方法）
s.fill(val byte)              // 填充 byte 值（方法）
pos = s.index(sub)            // 子字串位置
ok = s.contains(sub)          // 是否包含
ok = s.starts-with(sub)       // 前綴判斷
ok = s.ends-with(sub)         // 後綴判斷
s.to-upper()                  // 轉大寫
s.to-lower()                  // 轉小寫
out = s.trim()                // 去首尾空白
out = s.repeat(n)             // 重複
out = s.slice(start, end)     // 切片
b = s.to-bytes()              // 轉 []byte
s = b.to-str()                // []byte 轉 str（方法）
v = s.to-i64()                // 字串轉 i64（回傳 ?i64）
v = s.to-i8()                 // 字串轉 i8（回傳 ?i8）
v = s.to-i16()                // 字串轉 i16（回傳 ?i16）
v = s.to-i32()                // 字串轉 i32（回傳 ?i32）
v = s.to-u8()                 // 字串轉 u8（回傳 ?u8）
v = s.to-u16()                // 字串轉 u16（回傳 ?u16）
v = s.to-u32()                // 字串轉 u32（回傳 ?u32）
v = s.to-u64()                // 字串轉 u64（回傳 ?u64）
v = s.to-byte()               // 字串轉 byte（回傳 ?byte）
v = s.to-f64()                // 字串轉 f64（回傳 ?f64）
v = s.to-bool()               // 字串 "true"/"false" 轉 bool（回傳 ?bool）
s = v.to-str()                // i64 轉字串（方法）
out = s.reverse()             // 反轉
c = s.compare(b)              // 字典序比較
n = s.count()                 // code point 總數
val = s.replace-char(old, new) // 取代字元（返回結果字串）
out = s.trim-char(c)          // 去指定字元
ok = s.empty()                // 是否為空
parts = s.split(sep)          // 用分隔符分割（返回 []str，方法）
out = ss.join(sep)            // []str 用分隔符連接（方法）
```

### number — 數值操作

```nolang
max(a, b)                     // 最大值
min(a, b)                     // 最小值
r = num.clamp(lo, hi)         // 限制範圍（方法）
r = abs(a)                    // 絕對值（num 泛型）
r = num.sign()                // 正負號（-1/0/1，方法）
even(v)                       // 奇偶判斷
odd(v)
gcd(a, b)                     // 最大公因數
lcm(a, b)                     // 最小公倍數
r = pow(a, n)                 // 整數冪
i64-to-f64(v)                 // 數值轉換
f64-to-i64(v)
s = int.to-str()              // i64 轉字串（方法）
q = div(a, b)                 // 除法取商
r = mod(a, b)                 // 取模
swap(a, b)                    // 交換
yes = float.is-nan()          // NaN 判斷（方法）
yes = float.is-inf()          // Inf 判斷（方法）

// 範圍常數
i8.MIN / MAX                  // -128 / 127
i16.MIN / MAX                 // -32768 / 32767
i32.MIN / MAX                 // -2147483648 / 2147483647
i64.MIN / MAX                 // -2^63 / 2^63-1
u8.MIN / MAX                  // 0 / 255
u16.MIN / MAX                 // 0 / 65535
u32.MIN / MAX                 // 0 / 4294967295
u64.MIN / MAX                 // 0 / 2^64-1
```

### byte — 位元組操作

```nolang
out = i64.to-bytes-be()         // i64 → big-endian [8]byte
out = i64.to-bytes-le()         // i64 → little-endian [8]byte
v = []byte.to-i64-be()          // big-endian []byte → i64（1~8 位元組）
v = []byte.to-i64-le()          // little-endian []byte → i64（1~8 位元組）
s = []byte.to-str()             // []byte 轉 str（方法）
s = []byte.to-hex()             // []byte → 大寫十六進制字串
s = []byte.to-hex-lower()       // []byte → 小寫十六進制字串
s = byte.to-str()               // byte 轉 str（方法）
```

### vec — 切片操作

```nolang
v = vec-create(n, val)         // 建立長度 n 的切片，全部填充 val
ok = []t.eq(a, b, n)           // 相等比較
n = []t.len()                  // 長度
[]t.push(val)                   // 追加
val, new-n = []t.pop()         // 彈出
found = []t.contains(n, val)   // 是否包含（n 為長度）
[]t.reverse(n)                  // 反轉前 n 個元素
[]t.clone(dst)                  // 複製到 dst
[]t.fill(n, val)                // 前 n 個元素填充
arr = []t.to-arr()             // 轉陣列
[]t.sort-asc()                  // 升序排序（方法）
[]t.sort-desc()                 // 降序排序（方法）
```

### arr — 陣列操作

```nolang
out = [n]t.clone()             // 複製
ok = [n]t.eq(b)                // 相等比較
[n]t.fill(val)                  // 填充
[n]t.reverse()                  // 反轉
ok = [n]t.contains(val)        // 是否包含
v = [n]t.to-vec()              // 轉切片
v = [n]t.max()                 // 最大值
v = [n]t.min()                 // 最小值
v = [n]t.sum()                 // 總和
i = [n]t.index-of(val)          // 索引
v = [n]t.last()                // 最後元素
v = [n]t.first()               // 首元素
[n]t.sort-asc()                 // 升序排序
[n]t.sort-desc()                // 降序排序
```

### sort — 排序常量

```nolang
sort.ast                         // 升序
sort.desc                        // 降序
```

---

## 作業系統與檔案

### os — 作業系統介面

提供環境變數、目錄操作、行程管理、系統資訊、時間等功能。檔案讀寫相關功能請見 `fs` 模組。

```nolang
// 環境變數
val = get-env(key)
set-env(key, val)

// 目錄
dir = get-wd()
ch-dir(dir)
mkdir(path, mode)

// 行程
exit(code)
pid = get-pid()

// 系統資訊
name = host-name()
msg = strerror(errnum)

// 時間
sec = now()
ms = now-ms()
us = now-us()
ns = now-ns()
sleep(sec)
sleep-us(us)
sleep-ns(ns)

// 命令列參數
count = args()
val = arg(idx)
```

### fs — 檔案系統工具

以 `file` 結構體封裝開啟中的檔案，以 `path` 結構體封裝路徑。

```nolang
// 檔案結構體
file {
    fd i64
    path str
}

// 標準檔案
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

// 開啟檔案（帶選項）
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
f = open(path, opts)             // 開啟檔案，失敗返回 nil

// file 方法
read-n = f.read(buf, n)          // 讀取最多 n 位元組
line = f.read-line()              // 讀取一行（?str, nil=EOF）
content, n = f.read-all()        // 讀取整個檔案
written = f.write(data, n)       // 寫入 n 位元組
ok = f.write-all(data, n)        // 寫入全部（覆寫）
ok = f.append(data, n)           // 追加資料
ok = f.copy-to(dst-path)         // 複製到目標路徑
ok = f.close()                   // 關閉（標準檔案不自動關閉）
yes = f.is-open()                // 是否已開啟
sz = f.size()                    // 檔案大小

// 內建函數
fd = open-read(path)             // 唯讀開啟
fd = open-write(path)            // 寫入開啟（O_CREAT|O_TRUNC, 0644）
fd = open-file(path, flags, mode) // 自訂旗標開啟
n = read(fd, buf, n)             // 底層讀取
written = write(fd, data, n)     // 底層寫入
ok = close(fd)                   // 底層關閉
ok = remove(path)                // 刪除檔案
ok = rename(old, new)            // 重新命名
ok = is-file(path)               // 判斷是否為檔案
ok = is-dir(path)                // 判斷是否為目錄
sz = stat-size(path)             // 取得檔案大小
sz = file-size(path)             // 同 stat-size
line = get-line()                // 從標準輸入讀取一行（?str, nil=EOF）
ok = copy-file(src, dst)         // 複製檔案

// macOS open() 旗標常量
O-RDONLY = 0, O-WRONLY = 1, O-RDWR = 2
O-CREAT = 512, O-TRUNC = 1024, O-APPEND = 8, O-EXCL = 2048
```

### env — 環境變數（簡化封裝）

```nolang
val = get(key)
val = lookup(key)               // 返回 ?str（nil=未找到）
set(key, val)
unset(key)
val = get-with-default(key, default)
ok = is-set(key)
```

### args — 命令列引數

```nolang
n = count()
arg = get(i)
name = program()
ok = has-flag(name)
val = get-option(name)
arg = get-positional(i)
```

### path — 路徑操作

以 `path` 結構體封裝路徑字串，所有操作以方法形式提供：

```nolang
SEP = 47     // '/'（ASCII）
DOT = 46     // '.'

// 結構體
path {
    p str
}

// 路徑拼接與分解（原地修改 .p）
p = path{
    p: '/a/b/c.txt'
}
p.join(b str)           // 拼接兩個路徑（原地修改）
p.base() (out)           // 取檔名
p.dir()                  // 取目錄（原地修改 .p）
p.ext() (out)            // 取副檔名
p.clean()                // 正規化（原地修改 .p）
p.split() (f str)        // 分割為目錄+檔名（.p 改為目錄，返回檔名）

// 路徑判斷
p.is-abs() (yes bool)    // 是否為絕對路徑

// 檔案系統操作（委託 fs 內建函數）
p.exists() (yes bool)        // 是否存在
p.is-dir() (yes bool)        // 是否為目錄
p.is-file() (yes bool)       // 是否為檔案
p.size() (sz i64)            // 檔案大小
p.make-dir() (ok bool)       // 建立目錄
p.remove() (ok bool)         // 刪除
p.rename(new-p str) (ok bool)    // 重新命名
p.change-dir() (ok bool)     // 切換工作目錄

// 建構型方法
path.current() (out path)    // 取得當前工作目錄
```

### bufio — 緩衝讀取

```nolang
r = reader.init(fd, buf)       // 初始化緩衝讀取器（傳回 reader）
ok = reader.fill()              // 填充緩衝區
b = reader.read-byte()          // 讀取一個位元組（?byte, nil=EOF）
ok = reader.read-line(line)     // 讀取一行到 line
reader.close()                  // 關閉
```

### io — 輸入輸出抽象

提供 `io-reader` 和 `io-writer` 結構體，統一檔案、標準輸入輸出等資料流的讀寫操作：

```nolang
// 標準檔案描述符
STDIN-FD = 0, STDOUT-FD = 1, STDERR-FD = 2

// io-reader 結構體
io-reader {
    fd i64
}
r = io-reader.from-fd(fd)      // 從 fd 建立
r = io-reader.from-stdin()     // 從標準輸入建立
read-n = r.read(buf, n)        // 讀取 n 位元組
b = r.read-byte()              // 讀取一位元組（?byte, nil=EOF）
line = r.read-line()           // 讀取一行（?str, nil=EOF）
total = r.read-all(buf, size)  // 讀取全部

// io-writer 結構體
io-writer {
    fd i64
}
w = io-writer.from-fd(fd)      // 從 fd 建立
w = io-writer.from-stdout()    // 從標準輸出建立
w = io-writer.from-stderr()    // 從標準錯誤建立
written = w.write(data, n)     // 寫入 n 位元組
written = w.write-str(s)       // 寫入整個字串
written = w.write-byte(b)      // 寫入一位元組
written = w.write-line(s)      // 寫入字串+換行

// 便捷函數
n = io-print(s)                // 寫入 stdout（不換行）
n = io-println(s)              // 寫入 stdout（換行）
n = io-err(s)                  // 寫入 stderr（不換行）
n = io-errln(s)                // 寫入 stderr（換行）
line = io-read-line()          // 從 stdin 讀取一行（?str, nil=EOF）
```

### regexp — 正規表示式

以 `regexp` 結構體封裝 pattern，底層使用 C 標準庫 `regex.h`：

```nolang
// 結構體
regexp {
    pattern str
}

// 方法
re = regexp{
    pattern: '^hello'
}
matched = re.matches(text)        // 判斷是否匹配
result = re.find(text)           // 查找第一個匹配子串
```

### process — 進程操作

提供進程創建、標準流獲取、進程等待、進程信息查詢等功能。底層使用 POSIX fork/exec/pipe/waitpid：

```nolang
// 信號常量
SIG-TERM = 15, SIG-KILL = 9, SIG-INT = 2, SIG-STOP = 19, SIG-CONT = 18, SIG-CHLD = 17
WNOHANG = 1

// 結構體
process {
    pid i64
    stdin-fd i64
    stdout-fd i64
    stderr-fd i64
    exit-code i64
    running i64
}

// 進程創建
p = process{}
ok = p.start(program, arg)          // fork + exec，捕獲 stdout
ok = p.start-with-stdin(program, arg) // fork + exec，捕獲 stdin + stdout

// 進程等待
ok = p.wait()                       // 阻塞等待子進程結束
ok = p.wait-nohang()                // 非阻塞輪詢

// 進程控制
ok = p.kill(sig)                    // 發送信號
ok = p.terminate()                  // SIG-TERM
ok = p.force-kill()                 // SIG-KILL

// 標準流操作
read-n = p.read(buf, n)             // 從 stdout 讀取
line = p.read-line()               // 讀取一行（?str, nil=EOF）
content, n = p.read-all()           // 讀取全部 stdout
written = p.write(data, n)          // 寫入 stdin
p.close-stdin()                    // 關閉 stdin 管道
p.close-stdout()                   // 關閉 stdout 管道
p.close-stderr()                   // 關閉 stderr 管道

// 進程信息
pid = p.pid-of()                    // 子進程 ID
code = p.exit-code-of()             // 退出碼
yes = p.is-running()                // 是否仍在執行
pid = process.parent-pid()          // 父進程 ID

// 生命週期
p.close()                          // 關閉所有管道並等待

// 便捷函數
status = process-run(cmd)           // 執行 shell 命令
content, code = process-output(program, arg) // 執行並捕獲輸出
```

### net — 網路操作

提供 TCP 網路編程能力，包括服務端監聽、客戶端連接、資料收發等。底層使用 POSIX socket API：

```nolang
// 網路常量
AF-INET = 2, SOCK-STREAM = 1, SOL-SOCKET = 65535, SO-REUSEADDR = 4, BACKLOG = 128

// listener 結構體
listener {
    fd i64
}

// 監聽操作
l = listener{}
ok = l.listen(host, port)            // 建立 TCP 監聽（socket+setsockopt+bind+listen）
c = l.accept()                       // 接受連接（?conn, nil=無連接）
l.close()                           // 關閉監聽 socket
fd = l.fd-of()                       // 取得 fd

// conn 結構體
conn {
    fd i64
}

// 連接操作
c = conn{}
ok = c.dial(host, port)              // 建立 TCP 連接（socket+connect）
written = c.send(data)               // 發送字串
read-n = c.recv(buf, n)              // 接收資料到 buf
line = c.recv-line()                 // 接收一行（?str, nil=EOF, 最多 4096 位元組）
content, total = c.recv-all()        // 接收全部直到連接關閉
c.close()                           // 關閉連接
fd = c.fd-of()                       // 取得 fd

// 便捷函數
l = net-listen-on(host, port)        // 建立監聽器並開始監聽（?listener）
c = net-dial-to(host, port)          // 建立連接並撥號（?conn）
```

### net/ip — IP 地址操作

提供 IPv4 地址的解析、驗證、轉換與分類功能。純 Nolang 實作：

```nolang
// 預設地址常量
IP-ZERO       // 0.0.0.0
IP-LOOPBACK   // 127.0.0.1
IP-ANY        // 0.0.0.0
IP-BROADCAST  // 255.255.255.255

// ip-addr 結構體
ip-addr {
    a i64
    b i64
    c i64
    d i64
}

// 解析與轉換
ip = ip-addr{}
ok = ip.parse('192.168.1.1')         // 從字串解析
s = ip.to-str()                      // 轉為字串 '192.168.1.1'
v = ip.to-u32()                      // 轉為 u32（大端序）
ip.from-u32(v)                      // 從 u32 建立

// 地址分類
yes = ip.is-loopback()               // 127.0.0.0/8
yes = ip.is-private()                // 10/8, 172.16/12, 192.168/16
yes = ip.is-zero()                   // 0.0.0.0
yes = ip.is-broadcast()              // 255.255.255.255
yes = ip.is-multicast()              // 224.0.0.0/4
yes = ip.is-link-local()             // 169.254.0.0/16
yes = ip.is-class-a()                // A 類（1~126）
yes = ip.is-class-b()                // B 類（128~191）
yes = ip.is-class-c()                // C 類（192~223）

// 比較與子網
yes = ip.equal(other)                // 地址相等比較
yes = ip.in-subnet(base, prefix-len) // 子網包含檢查

// 便捷函數
addr = ip-parse(s)                   // 快速解析（?ip-addr, nil=無效）
yes = ip-is-loopback(s)              // 快速判斷環回
yes = ip-is-private(s)               // 快速判斷私有
```

### net/sse — Server-Sent Events 客戶端

支援 W3C EventSource 規範的 SSE 串流接收。底層使用 HTTP/1.1 長連接，支援明文 HTTP 與 HTTPS（TLS）：

```nolang
// sse-event 結構體
sse-event {
    event str       // 事件類型（預設 'message'）
    data str        // 事件資料（多行 data 以 \n 連接）
    id str          // 事件 ID
    retry i64       // 重連等待毫秒數（-1=未設定）
}

// sse-client 結構體
sse-client {
    fd i64              // TCP socket fd
    tls-c tls-conn      // TLS 連線
    use-tls bool        // 是否使用 TLS
    connected bool      // 連線狀態
    host str            // 伺服器主機名
    port i64            // 埠號
    path str            // 請求路徑
    last-event-id str   // 最後收到的事件 ID
    recv-buf str        // 接收緩衝區
    recv-buf-len i64    // 緩衝區資料長度
}

// 連接與事件接收
client = sse-connect('http://host:3000/events')  // 返回 ?sse-client
client: {
    nil -> println('connect failed')
    ->
        ev = client.next-event()     // 返回 ?sse-event（nil=EOF, err=錯誤）
        ev: {
            nil -> println('connection closed')
            err -> println('error: ' - it)
            -> println(ev.data)
        }
        client.close()
}

// 其他方法
yes = client.is-connected()         // 檢查連線狀態
ok = client.reconnect()             // 重新連線（使用 last-event-id）
```

---

## 時間與日期

### time — 時間操作

```nolang
sec = now-s()                   // 目前 Unix 時間戳（秒）
ms = now-ms()                   // 目前時間戳（毫秒）
us = now-us()                   // 目前時間戳（微秒）
out = format-time(t, fmt)        // 格式化時間
sleep-ms(ms)                    // 睡眠（毫秒）
sleep-us(us)                    // 睡眠（微秒）
d = duration-between(start, end) // 耗時（秒）
d = duration-ms-between(s, e)    // 耗時（毫秒）
```

---

## 日誌

### log — 分級日誌

```nolang
LEVEL-DEBUG = 0
LEVEL-INFO  = 1
LEVEL-WARN  = 2
LEVEL-ERROR = 3
LEVEL-FATAL = 4

set-level(lvl)
debug(msg)
info(msg)
warn(msg)
error(msg)
fatal(msg)
```

---

## 資料結構

### set — 集合（基於陣列）

```nolang
new-n = add(s, n, val)           // 新增元素
new-n = set-remove(s, n, val)        // 移除元素
ok = contains(s, n, val)         // 是否包含
new-an = union(a, an, b, bn)     // 聯集
out, n = intersection(a, an, b, bn)// 交集
out, n = difference(a, an, b, bn)  // 差集
v = to-vec(s, n)                 // 轉切片
sz = set-size(s, n)                   // 元素個數
yes = set-empty(s, n)                    // 是否為空
```

### deque — 雙端佇列

使用循環緩衝區實作的雙端佇列，以 `deque` 結構體封裝：

```nolang
// 結構體
deque {
    buf []i64
    cap i64
    head i64
    tail i64
}

// 初始化
d = deque{
    buf: buf
    cap: 128
    head: 0
    tail: 0
}

// 方法
d.push-front(val)              // 從前端推入
d.push-back(val)               // 從後端推入
val = d.pop-front()             // 從前端彈出
val = d.pop-back()              // 從後端彈出
val = d.peek-front()            // 查看前端元素（?i64, nil=空）
val = d.peek-back()             // 查看後端元素（?i64, nil=空）
sz = d.size()                   // 大小
yes = d.empty()                 // 是否為空
d.clear()                      // 清空
```

### heap — 最小堆

以 `heap` 結構體封裝的二元最小堆積：

```nolang
// 結構體
heap {
    data []i64
    n i64
}

// 初始化
h = heap.init(data)            // 建立堆積

// 方法
h.push(val)                    // 推入元素
val = h.pop()                  // 彈出最小元素（?i64, nil=空）
val = h.peek()                 // 查看最小元素（?i64, nil=空）
sz = h.size()                  // 大小
yes = h.empty()                // 是否為空
```

### stack — 堆疊

後進先出（LIFO）資料結構，以 `stack` 結構體封裝：

```nolang
// 結構體
stack {
    data []i64
    n i64
}

// 初始化
buf [128]i64 = [0:128]
s = stack{
    data: buf
    n: 0
}

// 方法
s.push(val)                    // 推入元素
val = s.pop()                  // 彈出頂端元素（?i64, nil=空）
val = s.peek()                 // 查看頂端元素（?i64, nil=空）
sz = s.size()                  // 大小
yes = s.empty()                // 是否為空
s.clear()                      // 清空
```

### map/linked-hash-map — 有序哈希表

固定容量 64（i64→i64），線性探測，雙向鏈表保持插入順序：

```nolang
m = linked-hash-map{}
m.init()
m.put(key, val)
result = m.get(key)   // ?i64, nil=未找到
found = m.contains(key)
removed = m.remove(key)
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(key, val)
```

### map/hash-set — i64 哈希集合

固定容量 64，線性探測，O(1) 查找/插入/刪除：

```nolang
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

```nolang
m = str-map{}
m.init()
m.put('key', 'val')
result = m.get('key')   // ?str, nil=未找到
found = m.contains('key')
removed = m.remove('key')
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(k, v)
```

### map/str-set — str 哈希集合

固定容量 256，FNV-1a 雜湊，字串去重：

```nolang
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

---

## 編碼

### encoding/hex — 十六進制

```nolang
// 編碼（定義於 byte 模組）
out = data.to-hex()                  // []byte → 大寫 hex str
out = data.to-hex-lower()            // []byte → 小寫 hex str

// 解碼（定義於 str 模組）
out = s.from-hex()                   // hex str → ?[]byte（nil=空, err=無效字元）
```

### encoding/base64 — Base64（RFC 4648）

```nolang
BASE64-STD = 'ABC...+/'
BASE64-URL = 'ABC...-_'
PAD = 61  // '='

out-n = encode(data, n, table, out)    // Base64 編碼
out-n = encode-std(data, n, out)       // 標準編碼
out-n = encode-url(data, n, out)       // URL 安全編碼
out-n = decode(s, n, table, out)   // Base64 解碼（?i64, nil=無效輸入）
```

### encoding/csv — CSV 解析（RFC 4180）

```nolang
fn, new-pos = parse-field(s, sn, pos, field)  // 解析單個欄位
n = parse-line(s, sn, fields, max)             // 解析一行
out-n = encode-field(field, fn, out)           // 編碼欄位
```

---

## 歸檔

### archive/tar — TAR 歸檔（POSIX ustar）

```nolang
// 讀取普通 tar
archive = tar{
    data: raw-bytes
}
count = archive.count()
e = archive.entry(idx)
name = archive.name(idx)
sz = archive.size(idx)
typ = archive.type(idx)              // "file" / "dir" / "unknown"
yes = archive.is-dir(idx)
yes = archive.is-file(idx)
out = archive.read(idx)
mode = archive.mode(idx)
ts = archive.mtime(idx)

// 讀取 .tar.gz（自動解壓縮）
archive = tar-open-gz(gz-data)

// tar-entry 方法
name = e.name()
sz = e.size()
typ = e.type()
out = e.read()

// 寫入 tar
builder = tar-builder{}
builder.add-file(name, content)
builder.add-dir(name)
archive = builder.finish()
```

### archive/zip — ZIP 歸檔解析

```nolang
archive = zip{
    data: raw-bytes
}
count = archive.count()                        // 條目數
e = archive.entry(idx)                         // 取得 zip-entry
name = archive.name(idx)                       // 檔名
sz = archive.size(idx)                         // 原始大小
csz = archive.compressed-size(idx)             // 壓縮後大小
method = archive.method(idx)                   // 0=stored, 8=deflate
out = archive.extract(idx)                     // stored 和 deflate 模式

// zip-entry 方法
name = e.name()
sz = e.size()
csz = e.compressed-size()
method = e.method()
out = e.extract()
```

### archive/gzip — GZIP 壓縮與原始 DEFLATE

```nolang
out = gzip-compress(data)                      // zlib 壓縮
out = gzip-decompress(data)                    // zlib 解壓縮
out = inflate-decompress(data, out-size)       // 原始 DEFLATE 解壓縮（ZIP method 8）
```

---

## 密碼學與雜湊

### hash/aes — AES-128 加解密（ECB 模式）

```nolang
aes-128-enc(plain, 16, key, out)   // 加密 16-byte 區塊
aes-128-dec(cipher, 16, key, out)  // 解密 16-byte 區塊
```

另含獨立模組 `hash/aes-128-enc` 和 `hash/aes-128-dec`。

### hash/des — DES 加解密（ECB 模式）

```nolang
des-enc(plain, 8, key, out)        // 加密 8-byte 區塊
des-dec(cipher, 8, key, out)       // 解密 8-byte 區塊
```

另含獨立模組 `hash/des-enc` 和 `hash/des-dec`。

### hash/rsa — RSA 模冪運算

```nolang
rsa-modpow(base, bn, exp, en, mod, mn, result, rn)
```

不包含金鑰生成，支援 1024~4096-bit。

### hash/md5 — MD5（128-bit）

```nolang
out [16]byte = md5(data)
```

### hash/sha1 — SHA-1（160-bit）

```nolang
hash = sha1(data []byte) (hash [20]byte)
hex = sha1-hex(data []byte) (hex str)
sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32)
```

`sha1` 計算完整雜湊（含填充與多區塊處理），返回 20 位元組。
`sha1-hex` 同上但返回 40 字元小寫 hex 字串。
`sha1-block` 為低階 API，處理單個 512-bit 區塊。

### hash/sha256 — SHA-256（256-bit）

```nolang
sha256(data []byte) (hash [32]byte)
sha256-hex(data []byte) (hex str)
sha256-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32, h5 u32, h6 u32, h7 u32)
```

`sha256` 計算完整雜湊（含填充與多區塊處理），返回 32 位元組。
`sha256-hex` 同上但返回 64 字元小寫 hex 字串。
`sha256-block` 為低階 API，處理單個 512-bit 區塊。

### hash/sha512 — SHA-512（512-bit）

```nolang
sha512(data []byte) (hash [64]byte)
sha512-hex(data []byte) (hex str)
sha512-block(s []u64, h0 u64, h1 u64, h2 u64, h3 u64, h4 u64, h5 u64, h6 u64, h7 u64)
```

`sha512` 計算完整雜湊（含填充與多區塊處理），返回 64 位元組。
`sha512-hex` 同上但返回 128 字元小寫 hex 字串。
`sha512-block` 為低階 API，處理單個 1024-bit 區塊。

### hash/crc-32 — CRC32 校驗

```nolang
crc-32(s []byte, n, crc)
```

### hash/fnv-1a-32 — FNV-1a 非加密雜湊

```nolang
fnv-1a-32(s []byte, n, h)
```

### hash/rand — 隨機數產生器（xorshift32）

```nolang
r = rand(state)                     // 32-bit 偽隨機數
rand-str(state, n, s)              // 隨機字母數字字串
```

### hash/x509 — X.509 憑證 DER 解析

```nolang
tag = der-tag(data, pos)
len, adv = der-len(data, pos)
x509-fingerprint(cert, n, h0..h7)  // SHA-256 憑證指紋
x509-rsa-e(cert, n, e)             // RSA 公鑰指數提取
```

---

## 資料交換

### json — JSON 解析與產生

```nolang
// 型別枚舉
json-kind {
    null,
    bool,
    num,
    str,
    arr,
    obj,
}

// 解析
v = parse(s, n)          // 完整解析
v = parse-str(s, n)                 // 解析字串值
v = parse-num(s, n)                 // 解析數值值

// 產生
n = stringify(v, out)    // 序列化

// 存取
val = get-key(v, key)    // 取得物件屬性
set-key(v json-value, key, val)    // 設定物件屬性
```

---

## 其他

### unicode — Unicode 支援

Unicode 相關功能已分散至 `char` 和 `str` 模組：

- 字元分類（`is-letter`, `is-digit`, `is-upper` 等）→ 見 `char` 模組
- UTF-8 編解碼（`char.to-bytes`, `char.to-str`）→ 見 `char` 模組
- 字串 rune 計數（`str.count`）→ 見 `str` 模組

### uuid — UUID v4 產生與解析

```nolang
out = new-v4(state)                  // 產生 UUID v4
out-n = uuid.to-str(out)             // 轉小寫字串（方法）
out-n = uuid.to-str-upper(out)       // 轉大寫字串（方法）
ok = from-str(s, sn, out)            // 從字串解析（支援連字號/不帶）
ok = parse-with-dashes(s, pos, out)  // 含連字號解析
ok = parse-no-dashes(s, pos, out)    // 無連字號解析
ok = uuid.validate()                 // 驗證 UUID 格式（方法）
v = uuid.version()                   // 取得版本（方法）
v = uuid.variant()                   // 取得變體（方法）
yes = uuid.is-nil()                  // 是否為 nil（方法）
yes = uuid.eq(b)                     // 相等比較（方法）
r = uuid.cmp(b)                      // 比較（方法）
nil-uuid(out)                        // 回傳 nil UUID
```

### bigint — 任意精度整數

```nolang
// 型別
bigint {
    sign i64
    limbs []i64
    len i64
}

// 建構
out = from-i64(v)
out = from-u64(v)
out = zero()
out = one()
out = copy(a)

// 比較
r = cmp(a, b)
r = eq(a, b)
r = is-zero(a)
r = is-neg(a)
r = is-pos(a)

// 運算
c = add(a, b)
c = sub(a, b)
c = mul(a, b)
q, r = div-mod(a, b)
r = mod(a, b)
q = div-i64(a, v)
r = mod-i64(a, v)
c = pow(a, n)
r = mod-pow(base, exp, mod, r)

// 數論
gcd(a, b, g)
lcm(a, b, l)

// 移位
shl(a, n, c)
shr(a, n, c)

// 字串轉換
n = to-str(a, out)
out = from-str(s, sn)
n = to-hex(a, out)
out = from-hex(s, sn)

// 小整數輔助
add-i64(a, v, c)
mul-i64(a, v, c)
```

### err — 錯誤處理

結構化錯誤型別與工具函式：

```nolang
// 錯誤碼枚舉
err-code {
    ok,
    not-found,
    permission,
    io,
    timeout,
    parse,
    invalid,
    overflow,
}

// 結構體
error {
    code err-code
    msg str
}

// 函數
e = err-new(err-code.io, msg)      // 建立錯誤
e = err-from-errno(errno)         // 從 C errno 建立
yes = err-is(e, err-code.io)      // 判斷錯誤碼
msg = err-msg(e)                  // 取得錯誤訊息
code = err-code-of(e)             // 取得錯誤碼
s, n = err-format(e)              // 格式化為字串
```

### bool — 布爾型別

```nolang
bool.to-str() (out str)     // true→"true", false→"false"（方法）
```

### enter / leave — 生命週期鉤子

```nolang

// 啟動時執行
enter { 
    enter()
}     

// 退出時執行
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
| strconv             | 核心   | 字串/數值轉換    |
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
| database/sql        | 子模組 | 資料庫存取介面   |
| hash/aes            | 子模組 | AES-128 加解密   |
| hash/aes-128-enc    | 子模組 | AES-128 加密     |
| hash/aes-128-dec    | 子模組 | AES-128 解密     |
| hash/des            | 子模組 | DES 加解密       |
| hash/des-enc        | 子模組 | DES 加密         |
| hash/des-dec        | 子模組 | DES 解密         |
| hash/rsa            | 子模組 | RSA 模冪         |
| hash/md5            | 子模組 | MD5 雜湊         |
| hash/sha1           | 子模組 | SHA-1 雜湊       |
| hash/sha256         | 子模組 | SHA-256 雜湊     |
| hash/sha512         | 子模組 | SHA-512 雜湊     |
| hash/crc-32         | 子模組 | CRC32 校驗       |
| hash/fnv-1a-32      | 子模組 | FNV-1a 雜湊      |
| hash/rand           | 子模組 | 隨機數產生器     |
| hash/x509           | 子模組 | X.509 DER 解析   |
