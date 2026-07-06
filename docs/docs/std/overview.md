---
sidebar_position: 3
---

# 標準庫

Nolang 標準庫（`src/std/`）包含 60+ 個模組，涵蓋格式化、數學、字串、資料結構、編解碼、加密、壓縮、檔案操作、I/O 抽象等。

使用方式：`use std/xxx`（核心模組無需 `use`）。

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

- **切片 `[]t`**：底層 `{ t*, i64 }`（data, len）
- **陣列 `[N]t`**：LLVM 固定大小陣列
- **字串 `str`**：union 型別（short ≤127 byte 存棧上 / long 存堆上），支援 `s[i]`、`s[i..j]`、`s + t`
- **列舉/Union**：`option { ok t, nil bool, err str }`（tagged enum）
- **結構體**：`point { x i64, y i64 }`
- **配列**：底層 linked-hash-map
- **迭代器**：`iterator { next()(val i64, ok i64) }`

### option — 選項型別

`option<T>` 標籤列舉（tag=0=val, 1=nil, 2=err）：

```nolang
x ?t                // 宣告 option<t>
x = val(42)         // 設為有值
x = nil             // 設為空
x = err('msg')      // 設為錯誤
x { val-> f(.); nil->; err-> g(.) }  // match
!x                  // 強制解包（panic if nil/err）
```

**風格指引：** 函數可能失敗或返回空值時，應使用 `?t` option 而非 `(val, ok bool)`。`?t` 有三種狀態：`ok(v)`（有值）、`nil`（空值/正常缺失）、`err(s)`（錯誤）。例如 `pop()` 返回 `?i64`（`nil` = 空）、`read-line()` 返回 `?str`（`nil` = EOF，`err` = 錯誤）、`lookup()` 返回 `?str`（`nil` = 未找到）。詳見語法文檔。

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
str-to-i8(s), str-to-i16(s), str-to-i32(s), str-to-i64(s)
str-to-u8(s), str-to-u16(s), str-to-u32(s), str-to-u64(s)
str-to-f32(s), str-to-f64(s)
str-to-bool(s), str-to-byte(s), str-to-char(s)

// 數值 → 字串（也可用方法形式: v.to-str()）
i8-to-str(v), i16-to-str(v), i32-to-str(v), i64-to-str(v)
u8-to-str(v), u16-to-str(v), u32-to-str(v), u64-to-str(v)
f32-to-str(v), f64-to-str(v)
bool-to-str(v), byte-to-str(v), char-to-str(v)
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
a.eq(b, n)(ok)                // 相等比較（方法）
s.copy()(dst)                 // 字串複製（方法）
s.fill(val byte)              // 填充 byte 值（方法）
s.index(sub)(pos)             // 子字串位置
s.contains(sub)(ok)           // 是否包含
s.starts-with(sub)(ok)        // 前綴判斷
s.ends-with(sub)(ok)          // 後綴判斷
s.to-upper()                  // 轉大寫
s.to-lower()                  // 轉小寫
s.trim()(out)                 // 去首尾空白
s.repeat(n)(out)              // 重複
s.slice(start, end)(out)      // 切片
s.to-bytes()(b)               // 轉 []byte
b.to-str()(s)                 // []byte 轉 str（方法）
s.to-i64()(v)                 // 字串轉 i64（回傳 ?i64）
s.to-i8()(v)                  // 字串轉 i8（回傳 ?i8）
s.to-i16()(v)                 // 字串轉 i16（回傳 ?i16）
s.to-i32()(v)                 // 字串轉 i32（回傳 ?i32）
s.to-u8()(v)                  // 字串轉 u8（回傳 ?u8）
s.to-u16()(v)                 // 字串轉 u16（回傳 ?u16）
s.to-u32()(v)                 // 字串轉 u32（回傳 ?u32）
s.to-u64()(v)                 // 字串轉 u64（回傳 ?u64）
s.to-byte()(v)                // 字串轉 byte（回傳 ?byte）
s.to-f64()(v)                 // 字串轉 f64（回傳 ?f64）
s.to-bool()(v)                // 字串 "true"/"false" 轉 bool（回傳 ?bool）
v.to-str()(s)                 // i64 轉字串（方法）
s.reverse()(out)              // 反轉
s.compare(b)(c)               // 字典序比較
s.count()(n)                  // code point 總數
s.replace-char(old, new)(val) // 取代字元（返回結果字串）
s.trim-char(c)(out)           // 去指定字元
s.empty()(ok)                 // 是否為空
```

### number — 數值操作

```nolang
max(a, b), min(a, b)          // 大小值
num.clamp(lo, hi)(r)          // 限制範圍（方法）
abs(a)(r)                     // 絕對值（num 泛型）
num.sign()(r)                 // 正負號（-1/0/1，方法）
even(v), odd(v)               // 奇偶判斷
gcd(a, b), lcm(a, b)          // 最大公因數/最小公倍數
pow(a, n)(r)                  // 整數冪
i64-to-f64(v), f64-to-i64(v)  // 數值轉換
int.to-str()(s)               // i64 轉字串（方法）
div(a, b)(q), mod(a, b)(r)    // 除法取商 / 取模
swap(a, b)                    // 交換
float.is-nan()(yes)           // NaN 判斷（方法）
float.is-inf()(yes)           // Inf 判斷（方法）

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
i64.to-bytes-be()(out)          // i64 → big-endian [8]byte
i64.to-bytes-le()(out)          // i64 → little-endian [8]byte
[]byte.to-i64-be()(v)           // big-endian []byte → i64（1~8 位元組）
[]byte.to-i64-le()(v)           // little-endian []byte → i64（1~8 位元組）
[]byte.to-str()(s)              // []byte 轉 str（方法）
[]byte.to-hex()(s)              // []byte → 大寫十六進制字串
[]byte.to-hex-lower()(s)        // []byte → 小寫十六進制字串
byte.to-str()(s)                // byte 轉 str（方法）
```

### vec — 切片操作

```nolang
vec-create(n, val)(v)           // 建立長度 n 的切片，全部填充 val
[]t.eq(a, b, n)(ok)            // 相等比較
[]t.len()(n)                    // 長度
[]t.push(val)                   // 追加
[]t.pop()(val, new-n)           // 彈出
[]t.contains(n, val)(found)     // 是否包含（n 為長度）
[]t.reverse(n)                  // 反轉前 n 個元素
[]t.clone(dst)                  // 複製到 dst
[]t.fill(n, val)                // 前 n 個元素填充
[]t.to-arr()(arr)               // 轉陣列
[]t.sort-asc()                  // 升序排序（方法）
[]t.sort-desc()                 // 降序排序（方法）
```

### arr — 陣列操作

```nolang
[n]t.clone()(out)               // 複製
[n]t.eq(b)(ok)                  // 相等比較
[n]t.fill(val)                  // 填充
[n]t.reverse()                  // 反轉
[n]t.contains(val)(ok)          // 是否包含
[n]t.to-vec()(v)                // 轉切片
[n]t.max()(v)                   // 最大值
[n]t.min()(v)                   // 最小值
[n]t.sum()(v)                   // 總和
[n]t.index-of(val)(i)           // 索引
[n]t.last()(v)                  // 最後元素
[n]t.first()(v)                 // 首元素
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
get-env(key)(val)
set-env(key, val)

// 目錄
get-wd()(dir)
ch-dir(dir)
mkdir(path, mode)

// 行程
exit(code)
get-pid()(pid)

// 系統資訊
host-name()(name)
strerror(errnum)(msg)

// 時間
now()(sec)
now-ms()(ms)
now-us()(us)
now-ns()(ns)
sleep(sec)
sleep-us(us)
sleep-ns(ns)

// 命令列參數
args()(count)
arg(idx)(val)
```

### fs — 檔案系統工具

以 `file` 結構體封裝開啟中的檔案，以 `path` 結構體封裝路徑。

```nolang
// 檔案結構體
file { fd i64, path str }

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
file-mode { read, write, append, read-write }
file-perm { perm-600, perm-644, perm-664, perm-666, perm-755, perm-777 }
file-opts { mode file-mode, perm file-perm, excl bool, truncate bool, append bool }
open(path, opts)(f ?file)         // 開啟檔案，失敗返回 nil

// file 方法
f.read(buf, n)(read-n)            // 讀取最多 n 位元組
f.read-line()(line, ok)           // 讀取一行
f.read-all()(content, n)          // 讀取整個檔案
f.write(data, n)(written)         // 寫入 n 位元組
f.write-all(data, n)(ok)          // 寫入全部（覆寫）
f.append(data, n)(ok)             // 追加資料
f.copy-to(dst-path)(ok)           // 複製到目標路徑
f.close()(ok)                     // 關閉（標準檔案不自動關閉）
f.is-open()(yes)                  // 是否已開啟
f.size()(sz)                      // 檔案大小

// 內建函數
open-read(path)(fd)               // 唯讀開啟
open-write(path)(fd)              // 寫入開啟（O_CREAT|O_TRUNC, 0644）
open-file(path, flags, mode)(fd)  // 自訂旗標開啟
read(fd, buf, n)(n)               // 底層讀取
write(fd, data, n)(written)       // 底層寫入
close(fd)(ok)                     // 底層關閉
remove(path)(ok)                  // 刪除檔案
rename(old, new)(ok)              // 重新命名
is-file(path)(ok)                 // 判斷是否為檔案
is-dir(path)(ok)                  // 判斷是否為目錄
stat-size(path)(sz)               // 取得檔案大小
file-size(path)(sz)               // 同 stat-size
get-line()(line, ok)              // 從標準輸入讀取一行
copy-file(src, dst)(ok)           // 複製檔案

// macOS open() 旗標常量
O-RDONLY = 0, O-WRONLY = 1, O-RDWR = 2
O-CREAT = 512, O-TRUNC = 1024, O-APPEND = 8, O-EXCL = 2048
```

### env — 環境變數（簡化封裝）

```nolang
get(key)(val)
lookup(key)(val, ok)
set(key, val)
unset(key)
get-with-default(key, default)(val)
is-set(key)(ok)
```

### args — 命令列引數

```nolang
count()(n)
get(i)(arg)
program()(name)
has-flag(name)(ok)
get-option(name)(val)
get-positional(i)(arg)
```

### path — 路徑操作

以 `path` 結構體封裝路徑字串，所有操作以方法形式提供：

```nolang
SEP = 47     // '/'（ASCII）
DOT = 46     // '.'

// 結構體
path { p str }

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
reader.init(fd, buf)(r)         // 初始化緩衝讀取器（傳回 reader）
reader.fill()(ok)               // 填充緩衝區
reader.read-byte()(b, ok)       // 讀取一個位元組
reader.read-line(line)(ok)      // 讀取一行到 line
reader.close()                  // 關閉
```

### io — 輸入輸出抽象

提供 `io-reader` 和 `io-writer` 結構體，統一檔案、標準輸入輸出等資料流的讀寫操作：

```nolang
// 標準檔案描述符
STDIN-FD = 0, STDOUT-FD = 1, STDERR-FD = 2

// io-reader 結構體
io-reader { fd i64 }
io-reader.from-fd(fd)(r)        // 從 fd 建立
io-reader.from-stdin()(r)       // 從標準輸入建立
r.read(buf, n)(read-n)          // 讀取 n 位元組
r.read-byte()(b, ok)            // 讀取一位元組
r.read-line()(line, ok)         // 讀取一行
r.read-all(buf, size)(total)    // 讀取全部

// io-writer 結構體
io-writer { fd i64 }
io-writer.from-fd(fd)(w)        // 從 fd 建立
io-writer.from-stdout()(w)      // 從標準輸出建立
io-writer.from-stderr()(w)      // 從標準錯誤建立
w.write(data, n)(written)       // 寫入 n 位元組
w.write-str(s)(written)         // 寫入整個字串
w.write-byte(b)(written)        // 寫入一位元組
w.write-line(s)(written)        // 寫入字串+換行

// 便捷函數
io-print(s)(n)                  // 寫入 stdout（不換行）
io-println(s)(n)                // 寫入 stdout（換行）
io-err(s)(n)                    // 寫入 stderr（不換行）
io-errln(s)(n)                  // 寫入 stderr（換行）
io-read-line()(line, ok)        // 從 stdin 讀取一行
```

### regexp — 正規表示式

以 `regexp` 結構體封裝 pattern，底層使用 C 標準庫 `regex.h`：

```nolang
// 結構體
regexp { pattern str }

// 方法
re = regexp{
    pattern: '^hello'
}
re.matches(text)(matched)       // 判斷是否匹配
re.find(text)(result)           // 查找第一個匹配子串
```

### process — 進程操作

提供進程創建、標準流獲取、進程等待、進程信息查詢等功能。底層使用 POSIX fork/exec/pipe/waitpid：

```nolang
// 信號常量
SIG-TERM = 15, SIG-KILL = 9, SIG-INT = 2, SIG-STOP = 19, SIG-CONT = 18, SIG-CHLD = 17
WNOHANG = 1

// 結構體
process { pid i64, stdin-fd i64, stdout-fd i64, stderr-fd i64, exit-code i64, running i64 }

// 進程創建
p = process{}
p.start(program, arg)(ok)          // fork + exec，捕獲 stdout
p.start-with-stdin(program, arg)(ok) // fork + exec，捕獲 stdin + stdout

// 進程等待
p.wait()(ok)                       // 阻塞等待子進程結束
p.wait-nohang()(ok)                // 非阻塞輪詢

// 進程控制
p.kill(sig)(ok)                    // 發送信號
p.terminate()(ok)                  // SIG-TERM
p.force-kill()(ok)                 // SIG-KILL

// 標準流操作
p.read(buf, n)(read-n)             // 從 stdout 讀取
p.read-line()(line, ok)            // 讀取一行
p.read-all()(content, n)           // 讀取全部 stdout
p.write(data, n)(written)          // 寫入 stdin
p.close-stdin()                    // 關閉 stdin 管道
p.close-stdout()                   // 關閉 stdout 管道
p.close-stderr()                   // 關閉 stderr 管道

// 進程信息
p.pid-of()(pid)                    // 子進程 ID
p.exit-code-of()(code)             // 退出碼
p.is-running()(yes)                // 是否仍在執行
process.parent-pid()(pid)          // 父進程 ID

// 生命週期
p.close()                          // 關閉所有管道並等待

// 便捷函數
process-run(cmd)(status)           // 執行 shell 命令
process-output(program, arg)(content, code) // 執行並捕獲輸出
```

### net — 網路操作

提供 TCP 網路編程能力，包括服務端監聽、客戶端連接、資料收發等。底層使用 POSIX socket API：

```nolang
// 網路常量
AF-INET = 2, SOCK-STREAM = 1, SOL-SOCKET = 65535, SO-REUSEADDR = 4, BACKLOG = 128

// listener 結構體
listener { fd i64 }

// 監聽操作
l = listener{}
l.listen(host, port)(ok)            // 建立 TCP 監聽（socket+setsockopt+bind+listen）
l.accept()(c, ok)                   // 接受連接，返回 conn 結構體
l.close()                           // 關閉監聽 socket
l.fd-of()(fd)                       // 取得 fd

// conn 結構體
conn { fd i64 }

// 連接操作
c = conn{}
c.dial(host, port)(ok)              // 建立 TCP 連接（socket+connect）
c.send(data)(written)               // 發送字串
c.recv(buf, n)(read-n)              // 接收資料到 buf
c.recv-line()(line, ok)             // 接收一行（最多 4096 位元組）
c.recv-all()(content, total)        // 接收全部直到連接關閉
c.close()                           // 關閉連接
c.fd-of()(fd)                       // 取得 fd

// 便捷函數
net-listen-on(host, port)(l, ok)    // 建立監聽器並開始監聽
net-dial-to(host, port)(c, ok)      // 建立連接並撥號
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
ip-addr { a i64, b i64, c i64, d i64 }

// 解析與轉換
ip = ip-addr{}
ip.parse('192.168.1.1')(ok)         // 從字串解析
ip.to-str()(s)                      // 轉為字串 '192.168.1.1'
ip.to-u32()(v)                      // 轉為 u32（大端序）
ip.from-u32(v)                      // 從 u32 建立

// 地址分類
ip.is-loopback()(yes)               // 127.0.0.0/8
ip.is-private()(yes)                // 10/8, 172.16/12, 192.168/16
ip.is-zero()(yes)                   // 0.0.0.0
ip.is-broadcast()(yes)              // 255.255.255.255
ip.is-multicast()(yes)              // 224.0.0.0/4
ip.is-link-local()(yes)             // 169.254.0.0/16
ip.is-class-a()(yes)                // A 類（1~126）
ip.is-class-b()(yes)                // B 類（128~191）
ip.is-class-c()(yes)                // C 類（192~223）

// 比較與子網
ip.equal(other)(yes)                // 地址相等比較
ip.in-subnet(base, prefix-len)(yes) // 子網包含檢查

// 便捷函數
ip-parse(s)(addr, ok)               // 快速解析
ip-is-loopback(s)(yes)              // 快速判斷環回
ip-is-private(s)(yes)               // 快速判斷私有
```

---

## 時間與日期

### time — 時間操作

```nolang
now-s()(sec)                    // 目前 Unix 時間戳（秒）
now-ms()(ms)                    // 目前時間戳（毫秒）
now-us()(us)                    // 目前時間戳（微秒）
format-time(t, fmt)(out)        // 格式化時間
sleep-ms(ms)                    // 睡眠（毫秒）
sleep-us(us)                    // 睡眠（微秒）
duration-between(start, end)(d) // 耗時（秒）
duration-ms-between(s, e)(d)    // 耗時（毫秒）
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
debug(msg), info(msg), warn(msg), error(msg), fatal(msg)
```

---

## 資料結構

### set — 集合（基於陣列）

```nolang
add(s, n, val)(new-n)           // 新增元素
set-remove(s, n, val)(new-n)        // 移除元素
contains(s, n, val)(ok)         // 是否包含
union(a, an, b, bn)(new-an)     // 聯集
intersection(a, an, b, bn)(out, n)// 交集
difference(a, an, b, bn)(out, n)  // 差集
to-vec(s, n)(v)                 // 轉切片
set-size(s, n)(sz)                   // 元素個數
set-empty(s, n)(yes)                    // 是否為空
```

### deque — 雙端佇列

使用循環緩衝區實作的雙端佇列，以 `deque` 結構體封裝：

```nolang
// 結構體
deque { buf []i64, cap i64, head i64, tail i64 }

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
d.pop-front()(val)             // 從前端彈出
d.pop-back()(val)              // 從後端彈出
d.peek-front()(val, ok)        // 查看前端元素
d.peek-back()(val, ok)         // 查看後端元素
d.size()(sz)                   // 大小
d.empty()(yes)                 // 是否為空
d.clear()                      // 清空
```

### heap — 最小堆

以 `heap` 結構體封裝的二元最小堆積：

```nolang
// 結構體
heap { data []i64, n i64 }

// 初始化
h = heap.init(data)            // 建立堆積

// 方法
h.push(val)                    // 推入元素
h.pop()(val, ok)               // 彈出最小元素
h.peek()(val, ok)              // 查看最小元素
h.size()(sz)                   // 大小
h.empty()(yes)                 // 是否為空
```

### stack — 堆疊

後進先出（LIFO）資料結構，以 `stack` 結構體封裝：

```nolang
// 結構體
stack { data []i64, n i64 }

// 初始化
buf [128]i64 = [0:128]
s = stack{
    data: buf
    n: 0
}

// 方法
s.push(val)                    // 推入元素
s.pop()(val, ok)               // 彈出頂端元素
s.peek()(val, ok)              // 查看頂端元素
s.size()(sz)                   // 大小
s.empty()(yes)                 // 是否為空
s.clear()                      // 清空
```

### map/linked-hash-map — 有序哈希表

固定容量 64（i64→i64），線性探測，雙向鏈表保持插入順序：

```nolang
m = linked-hash-map{}
m.init()
m.put(key, val, is-new)
m.get(key, found, result)
m.contains(key, found)
m.remove(key, is-new)
m.clear()
m.len(n)
m.is-empty(empty)
m.for-each(key, val)
```

### map/hash-set — i64 哈希集合

固定容量 64，線性探測，O(1) 查找/插入/刪除：

```nolang
s = hash-set{}
s.init()
s.add(val, is-new)
s.contains(val, found)
s.remove(val, removed)
s.clear()
s.len(n)
s.is-empty(empty)
s.for-each(val)
```

### map/str-map — str→str 哈希映射表

固定容量 256，FNV-1a 雜湊，線性探測：

```nolang
m = str-map{}
m.init()
m.put('key', 'val', is-new)
m.get('key', found, result)
m.contains('key', found)
m.remove('key', removed)
m.clear()
m.len(n)
m.is-empty(empty)
m.for-each(k, v)
```

### map/str-set — str 哈希集合

固定容量 256，FNV-1a 雜湊，字串去重：

```nolang
s = str-set{}
s.init()
s.add('hello', is-new)
s.contains('hello', found)
s.remove('hello', removed)
s.clear()
s.len(n)
s.is-empty(empty)
s.for-each(val)
```

---

## 編碼

### encoding/hex — 十六進制

```nolang
// 編碼（定義於 byte 模組）
data.to-hex()(out)                  // []byte → 大寫 hex str
data.to-hex-lower()(out)            // []byte → 小寫 hex str

// 解碼（定義於 str 模組）
s.from-hex()(out)                   // hex str → ?[]byte（nil=空, err=無效字元）
```

### encoding/base64 — Base64（RFC 4648）

```nolang
BASE64-STD = 'ABC...+/'
BASE64-URL = 'ABC...-_'
PAD = 61  // '='

encode(data, n, table, out)(out-n)    // Base64 編碼
encode-std(data, n, out)(out-n)       // 標準編碼
encode-url(data, n, out)(out-n)       // URL 安全編碼
decode(s, n, table, out)(out-n, ok)   // Base64 解碼
```

### encoding/csv — CSV 解析（RFC 4180）

```nolang
parse-field(s, sn, pos, field)(fn, new-pos)  // 解析單個欄位
parse-line(s, sn, fields, max)(n)             // 解析一行
encode-field(field, fn, out)(out-n)           // 編碼欄位
```

---

## 歸檔

### archive/tar — TAR 歸檔（POSIX ustar）

```nolang
// 讀取普通 tar
archive = tar{
    data: raw-bytes
}
archive.count()(count)
archive.entry(idx)(e)
archive.name(idx)(name)
archive.size(idx)(sz)
archive.type(idx)(typ)              // "file" / "dir" / "unknown"
archive.is-dir(idx)(yes)
archive.is-file(idx)(yes)
archive.read(idx)(out)
archive.mode(idx)(mode)
archive.mtime(idx)(ts)

// 讀取 .tar.gz（自動解壓縮）
archive = tar-open-gz(gz-data)

// tar-entry 方法
e.name()(name)
e.size()(sz)
e.type()(typ)
e.read()(out)

// 寫入 tar
builder = tar-builder{}
builder.add-file(name, content)
builder.add-dir(name)
builder.finish()(archive)
```

### archive/zip — ZIP 歸檔解析

```nolang
archive = zip{
    data: raw-bytes
}
archive.count()(count)                        // 條目數
archive.entry(idx)(e)                         // 取得 zip-entry
archive.name(idx)(name)                       // 檔名
archive.size(idx)(sz)                         // 原始大小
archive.compressed-size(idx)(csz)             // 壓縮後大小
archive.method(idx)(method)                   // 0=stored, 8=deflate
archive.extract(idx)(out)                     // stored 和 deflate 模式

// zip-entry 方法
e.name()(name)
e.size()(sz)
e.compressed-size()(csz)
e.method()(method)
e.extract()(out)
```

### archive/gzip — GZIP 壓縮與原始 DEFLATE

```nolang
gzip-compress(data)(out)                      // zlib 壓縮
gzip-decompress(data)(out)                    // zlib 解壓縮
inflate-decompress(data, out-size)(out)       // 原始 DEFLATE 解壓縮（ZIP method 8）
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
md5(data)(out [16]byte)
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
rand(state)(r)                     // 32-bit 偽隨機數
rand-str(state, n, s)              // 隨機字母數字字串
```

### hash/x509 — X.509 憑證 DER 解析

```nolang
der-tag(data, pos)(tag)
der-len(data, pos)(len, adv)
x509-fingerprint(cert, n, h0..h7)  // SHA-256 憑證指紋
x509-rsa-e(cert, n, e)             // RSA 公鑰指數提取
```

---

## 資料交換

### json — JSON 解析與產生

```nolang
// 型別常量
KIND-NULL, KIND-BOOL, KIND-NUM, KIND-STR, KIND-ARR, KIND-OBJ
JSON-NULL, JSON-TRUE, JSON-FALSE

// 解析
parse(s, n)(v json-value)          // 完整解析
parse-str(s, n)(v)                 // 解析字串值
parse-num(s, n)(v)                 // 解析數值值

// 產生
stringify(v json-value, out)(n)    // 序列化

// 存取
get-key(v json-value, key)(val)    // 取得物件屬性
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
new-v4(state)(out)                  // 產生 UUID v4
uuid.to-str(out)(out-n)             // 轉小寫字串（方法）
uuid.to-str-upper(out)(out-n)       // 轉大寫字串（方法）
from-str(s, sn, out)(ok)            // 從字串解析（支援連字號/不帶）
parse-with-dashes(s, pos, out)(ok)  // 含連字號解析
parse-no-dashes(s, pos, out)(ok)    // 無連字號解析
uuid.validate()(ok)                 // 驗證 UUID 格式（方法）
uuid.version()(v)                   // 取得版本（方法）
uuid.variant()(v)                   // 取得變體（方法）
uuid.is-nil()(yes)                  // 是否為 nil（方法）
uuid.eq(b)(yes)                     // 相等比較（方法）
uuid.cmp(b)(r)                      // 比較（方法）
nil-uuid(out)                       // 回傳 nil UUID
```

### bigint — 任意精度整數

```nolang
// 型別
bigint { sign i64, limbs []i64, len i64 }

// 建構
from-i64(v)(out), from-u64(v)(out)
zero()(out), one()(out)
copy(a)(out)

// 比較
cmp(a, b)(r), eq(a, b)(r)
is-zero(a)(r), is-neg(a)(r), is-pos(a)(r)

// 運算
add(a, b)(c), sub(a, b, c)
mul(a, b)(c)
div-mod(a, b)(q, r), mod(a, b, r)
div-i64(a, v, q), mod-i64(a, v)(r)
pow(a, n)(c), mod-pow(base, exp, mod, r)

// 數論
gcd(a, b, g), lcm(a, b, l)

// 移位
shl(a, n, c), shr(a, n, c)

// 字串轉換
to-str(a, out)(n), from-str(s, sn, out)
to-hex(a, out)(n), from-hex(s, sn, out)

// 小整數輔助
add-i64(a, v, c), mul-i64(a, v, c)
```

### err — 錯誤處理

結構化錯誤型別與工具函式：

```nolang
// 錯誤碼枚舉
err-code {
    OK = 0, NOT-FOUND = 1, PERMISSION = 2, IO = 3,
    TIMEOUT = 4, PARSE = 5, INVALID = 6, OVERFLOW = 7,
}

// 結構體
error { code i64, msg str }

// 函數
err-new(code, msg)(e)            // 建立錯誤
err-from-errno(errno)(e)         // 從 C errno 建立
err-is(e, code)(yes)             // 判斷錯誤碼
err-msg(e)(msg)                  // 取得錯誤訊息
err-code-of(e)(code)             // 取得錯誤碼
err-format(e)(s, n)              // 格式化為字串
```

### bool — 布爾型別

```nolang
bool.to-str() (out str)     // true→"true", false→"false"（方法）
```

### enter / leave — 生命週期鉤子

```nolang
enter { enter() }     // 啟動時執行
leave { leave() }     // 退出時執行
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
