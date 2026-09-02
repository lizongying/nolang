---
sidebar_position: 3.3
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

以 `file` 結構體封裝開啟中的檔案，以 `path` 結構體封裝路徑。定義 `fd` newtype（`fd = i64`），防止檔案描述符與任意 `i64` 混淆。另提供 `embed` 結構體用於編譯期嵌入檔案系統（透過 `#{embed='dir/path'}`）。

```no
; fd newtype（底層 i64，與 i64 在型別系統中互斥）
fd = i64

; 檔案結構體
file {
    fd fd
    path str
}

; 標準檔案（整數字面量可用於 fd 初始化）
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

; 標準檔案描述符
STDIN-FD fd = 0
STDOUT-FD fd = 1
STDERR-FD fd = 2

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
f = fs.open(path, opts)             ; 開啟檔案，成功返回 ?file，失敗返回 err

; file 方法
read-n = f.read(buf, n)              ; 讀取最多 n 位元組到 buf
line = f.read-line()                  ; 讀取一行（?str, nil=EOF 或錯誤）
content = f.read-bytes()             ; 讀取整個檔案為 ?[]byte（nil=空, err=失敗）
content = f.read-str()               ; 讀取整個檔案為 ?str（nil=空, err=失敗）
written = f.write(data, n)           ; 寫入 n 位元組
ok = f.write-str(data)               ; 寫入字串（覆寫），返回 ?bool
ok = f.write-bytes(data)             ; 寫入 []byte（覆寫），返回 ?bool
ok = f.append(data, n)               ; 追加資料
ok = f.copy-to(dst-path)             ; 複製到目標路徑
ok = f.close()                       ; 關閉（標準檔案 stdin/stdout/stderr 不關閉）
yes = f.is-open()                    ; 檢查是否已開啟（fd >= 0）
sz = f.size()                        ; 檔案大小（透過 fstat(fd)，消除 TOCTOU）

; 內建函數（註冊於 src/builtin/os.go，以註釋形式記錄於 fs.no）
; 這些是 ForwardFunc/CLibCall 內建函數——無需 .no 實作。

; 檔案讀寫（已廢棄，建議改用 read-bytes/write-bytes 以支援錯誤處理）
; [deprecated] read-file: 改用 read-bytes（支援錯誤處理、TOCTOU 安全、循環 read）
;   空切片表示失敗，無法區分空檔案與錯誤
; [deprecated] write-file: 改用 write-bytes（支援錯誤處理、循環 write）
;   無法區分部分寫入與完全失敗
data = fs.read-file(path)           ; [deprecated] 讀取整個檔案為 []byte（空切片表示失敗）
ok = fs.write-file(path, data)      ; [deprecated] 寫入 []byte 到檔案（覆寫）。成功返回 true
fd = fs.open-read(path)             ; 唯讀開啟（O_RDONLY）
fd = fs.open-write(path)            ; 寫入開啟（O_WRONLY|O_CREAT|O_TRUNC, 0644）
fd = fs.open-file(path, flags, mode) ; 自訂旗標和權限開啟
n = fs.read(fd, buf, n)             ; 底層讀取
written = fs.write(fd, data, n)     ; 底層寫入
ok = fs.close(fd)                   ; 底層關閉

; 檔案管理
ok = fs.remove(path)                ; 刪除檔案（unlink）
ok = fs.rename(old, new)            ; 重新命名
ok = fs.symlink(target, linkpath)   ; 建立符號連結
ok = fs.link(oldpath, newpath)      ; 建立硬連結
ok = fs.copy-file(src, dst)         ; 複製檔案內容

; 檔案資訊
ok = fs.exists(path)                ; 判斷路徑是否存在（跟隨符號連結）
ok = fs.is-file(path)               ; 判斷是否為普通檔案
ok = fs.is-dir(path)                ; 判斷是否為目錄
sz = fs.stat-size(path)             ; 取得檔案大小（返回 ?i64）
sz = fs.file-size(path)             ; 同 stat-size（返回 ?i64）
sz = fs.fstat-size(fd)              ; 透過 fstat(fd) 取得大小，消除 TOCTOU（返回 ?i64）
mode = fs.stat-mode(path)           ; 取得檔案模式（st_mode，返回 i64）
uid = fs.stat-uid(path)             ; 取得檔案所有者 uid（返回 i64）
gid = fs.stat-gid(path)             ; 取得檔案群組 gid（返回 i64）
mtime = fs.stat-mtime(path)         ; 取得修改時間（Unix 秒，返回 i64）
ok = fs.lstat(path)                 ; 取得符號連結資訊（不跟隨連結目標）

; 目錄操作
dirp = fs.open-dir(path)             ; 開啟目錄（返回控制代碼，0=失敗）
name = fs.read-dir(dirp)             ; 讀取下一個目錄條目（返回 str, ok bool）
ok = fs.close-dir(dirp)              ; 關閉目錄控制代碼
names = fs.list-dir(path)            ; 列出所有條目名稱（包含 . 和 ..）
names = fs.dir-entries(path)          ; 列出條目（不含 . 和 ..）
paths = fs.walk(root)                ; 遞迴遍歷目錄樹

; 路徑解析
abs = fs.realpath(path)              ; 解析為絕對規範路徑
target = fs.readlink(path)           ; 讀取符號連結目標（返回 str, ok bool）

; 臨時檔案/目錄
name, fd = fs.mkstemp(tmpl)          ; 建立臨時檔案（模板末尾為 XXXXXX）
name = fs.mkdtemp(tmpl)             ; 建立臨時目錄（模板末尾為 XXXXXX）

; 特殊檔案
ok = fs.mkfifo(path, mode)          ; 建立命名管道（FIFO）
ok = fs.mknod(path, mode, dev)      ; 建立特殊檔案（裝置節點）
ok = fs.truncate(path, length)      ; 截斷或擴展檔案到指定大小
fs.sync()                           ; 將檔案系統緩衝區排清到磁碟
ok = fs.touch-file(path)            ; 更新檔案時間戳為當前時間
ok = fs.utime(path, atime, mtime)   ; 設定存取與修改時間
ok = fs.rmdir(path)                 ; 刪除空目錄

; 標準輸入
line = fs.get-line()                ; 從標準輸入讀取一行（?str, nil=EOF）

; 便捷封裝函數（Nolang 實作於 fs.no，內部呼叫內建函數）
content = fs.read-str(path)          ; 讀取整個檔案為 ?str（nil=空, err=失敗）
content = fs.read-bytes(path)        ; 讀取整個檔案為 ?[]byte（nil=空, err=失敗）
ok = fs.write-str(path, data)        ; 寫入字串到檔案（覆寫），返回 ?bool
ok = fs.write-bytes(path, data)      ; 寫入 []byte 到檔案（覆寫），返回 ?bool

; embed: 編譯期嵌入的檔案系統（由 #{embed='dir/path'} 初始化）
;   #{embed='../frontend/dist'}
;   DIST fs.embed
;   data = DIST.read('index.html')   ; 讀取嵌入檔案，返回 ([]byte, bool)
;   ok = DIST.exists('css/style.css') ; 檢查嵌入檔案是否存在
embed {
    count i64
    paths str                        ; 所有路徑拼接（用 \0 分隔）
    pathStarts []i64                 ; 每個路徑在 paths 中的起始偏移
    pathLens []i64                   ; 每個路徑的長度
    blob []byte                      ; 所有檔案內容拼接
    dataStarts []i64                 ; 每個檔案在 blob 中的起始偏移
    dataLens []i64                   ; 每個檔案的長度
}

; Windows 平台特定（僅 win-amd64/win-arm64 可用）
bufptr = fs.win-find-first-file(path) ; FindFirstFileA，返回控制代碼（0=失敗）
name = fs.win-find-next-file(bufptr)  ; FindNextFileA，返回 (name, ok)
ok = fs.win-find-close(bufptr)        ; FindClose

; open() 旗標常量（平台特定，此處顯示 macOS 值）
O-RDONLY = 0
O-WRONLY = 1
O-RDWR = 2
O-CREAT = 512       ; macOS=512, Linux=64, Windows=256
O-TRUNC = 1024      ; macOS=1024, Linux=512, Windows=512
O-APPEND = 8        ; macOS=8, Linux=1024, Windows=8
O-EXCL = 2048       ; macOS=2048, Linux=128, Windows=1024
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
p = process.new()
ok = p.start-with-opts(program, args, dir, input, merge-err) ; 啟動子進程（不等待）
ok = p.start(program, arg)          ; 便捷方法：fork + exec，捕獲 stdout

; 進程等待
status = p.wait()                   ; 阻塞等待子進程結束
status = p.wait-nohang()            ; 非阻塞輪詢；nil=仍在執行

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

; 跨平台一次性執行（推薦，扁平參數）：fork/exec + 捕獲 stdout/stderr，帶超時與環境
;   入參：program 可執行檔路徑，args 參數切片，dir 工作目錄，input 寫入子進程 stdin 的內容，
;         env K=V 形式環境變數切片，timeout 毫秒（<=0 表示不限時），merge-err 為 true 時合併 stderr 到 out
;   出參：out 捕獲的 stdout（stderr 依 merge-err 決定是否合併），stderr 捕獲的 stderr 輸出，code 退出碼（-2=超時殺死，-1=啟動失敗），err 錯誤信息
out, stderr, code, err = process.cmd('echo', ['hello'], '', '', [], 0, false)

; 結構體選項形式（推薦，可讀性更好）：opts 結構體跨模組轉發示範
;   cmdopts 定義於本模組，呼叫端在另一模組構造 process.cmdopts{...} 並按引用傳入，
;   展示 nolang 的跨模組結構體定義轉發能力。
cmdopts {
    dir str        ; 工作目錄；空字串 = 繼承父進程工作目錄
    stdin str      ; 寫入子進程 stdin 的內容；空字串 = 不提供 stdin
    env []str      ; 環境變數（["K=V", ...]）；空陣列 = 繼承父進程環境
    timeout i64    ; 超時時間（毫秒），<=0 = 無限等待
    merge-err bool  ; true = 將 stderr 合併到捕獲的 out 中
}
o = process.cmdopts{
    dir: '',
    stdin: 'piped-data',
    env: ['K=V'],
    timeout: 200,
    merge-err: false
}
out, code, err = process.exec('echo', ['hello'], o)

; 便捷函數（舊，後續取捨）
status = process.process-run(cmd)           ; 執行 shell 命令
content, code = process.new().output(program, arg) ; 執行並捕獲輸出
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
    tls-c tls.conn      ; TLS 連線
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
client = sse.connect('http://host:3000/events')  ; 返回 ?sse-client
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
http.response {
    status-code i64
    status-text str
    headers str
    header-names [32]str
    header-values [32]str
    header-count i64
    body str
}

; 便捷函數
resp = http.get(url)                        ; GET 請求（?http.response）
resp = http.post(url, body)                  ; POST 請求（?http.response）
resp = http.do(method, url, body)            ; 自訂方法（?http.response）

; 使用 request 物件
req = http-request{}
req.init('POST', url, body)
req.add-header('Content-Type', 'application/json')
resp = http.do-req(req)                      ; 發送請求（?http.response）

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
c = http2.connect(host, port)                ; 建立連線（?http2-conn）
resp = http2.do(method, url, body)           ; 發送請求（?http.response）

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
c = http3.connect(host, port)                ; 建立 QUIC 連線（?http3-conn）
resp = http3.send-request(c, method, path, headers, body) ; 發送請求（?http.response）
resp = http3.get(url)                        ; GET 請求（?http.response）
resp = http3.post(url, body)                 ; POST 請求（?http.response）

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
s = ws.listen-on(host, port)                 ; 建立監聽（?ws-server）
c = s.accept()                               ; 接受連接（?ws-server-conn）
msg = c.recv()                               ; 接收訊息（?ws-message）
ok = c.send-text(text)                       ; 發送文字
ok = c.send-binary(data)                     ; 發送二進制
c.close()

; 客戶端
c = ws.connect(url)                          ; 連接服務端（?ws-client）
msg = c.recv()                               ; 接收訊息（?ws-message）
ok = c.send-text(text)                       ; 發送文字
ok = c.send-binary(data)                     ; 發送二進制
c.close()
```

### net/tls — TLS 1.2/1.3 客戶端（純 Nolang 實現）

提供 TLS 加密連接，支援 TLS 1.2 和 1.3：

```no
; 連接
c = tls.tls-dial(host, port)                     ; 建立 TLS 連接（?tls.conn）
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
c = quic.dial(host, port)                    ; 建立 QUIC 連接（?quic.conn）
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
ip = dns.resolve(host)                       ; 解析主機名（?str）
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
buf, n = hpack.encode(headers)
headers = hpack.decode(buf, n)
```

### net/proxy — 代理支援

提供 HTTP/SOCKS 代理連接功能：

```no
c = proxy.dial(proxy-url, target-host, target-port)
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
