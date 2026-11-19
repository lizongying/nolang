---
sidebar_position: 3.3
---

## 作业系统与档案

### os — 作业系统介面

提供环境变数、目录操作、行程管理、系统资讯、时间等功能。档案读写相关功能请见 `fs` 模组。

```no
; 环境变数
val = os.get-env(key); 返回 ?str（nil=未设定）
os.set-env(key, 'value')

; 目录
dir = os.get-wd()
os.ch-dir(dir)
os.mkdir(path, mode)

; 行程
exit(code); 结束行程（已提升为全域内建，无需 os 前缀，见 global）
pid = os.get-pid()

; 系统资讯
name = os.host-name()
arch = os.get-arch()
msg = os.strerror(errnum)
errno = os.get-errno()

; 档案属性与使用者（权限取得在 os，非 fs）
mode = os.stat-mode(path); 取得档案模式（st_mode，返回 ?i64，nil=取得失败）
uid = os.stat-uid(path); 取得档案所有者 uid（返回 ?i64，nil=取得失败）
gid = os.stat-gid(path); 取得档案群组 gid（返回 ?i64，nil=取得失败）
mtime = os.stat-mtime(path); 取得修改时间（Unix 秒，返回 ?i64，nil=取得失败）
self-uid = os.getuid(); 目前使用者 ID
self-gid = os.getgid(); 目前群组 ID

; 时间
sec = os.now()
ms = os.now-ms()
us = os.now-us()
ns = os.now-ns()
os.sleep(sec)
os.sleep-us(us)
os.sleep-ns(ns)

; 命令列参数
argv = os.args(); 全部命令列参数（[]str，含程式名）
val = os.arg(idx); 取得第 idx 个参数（越界回传 nil）
```

### fs — 档案系统工具

以 `file` 结构体封装开启中的档案，以 `path` 结构体封装路径。定义 `fd` newtype（`fd = i64`），防止档案描述符与任意 `i64` 混淆。另提供 `embed` 结构体用于编译期嵌入档案系统（透过 `#{embed='dir/path'}`）。

```no
; fd newtype（底层 i64，与 i64 在型别系统中互斥）
fd = i64

; 档案结构体
file {
    fd fd
    path str
}

; 标准档案（整数字面量可用于 fd 初始化）
stdin = file {
    fd: 0
    path: '<stdin>'
}
stdout = file {
    fd: 1
    path: '<stdout>'
}
stderr = file {
    fd: 2
    path: '<stderr>'
}

; 标准档案描述符
STDIN-FD fd = 0
STDOUT-FD fd = 1
STDERR-FD fd = 2

; 开启档案（带选项）
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
f = fs.open(path, opts); 开启档案，成功返回 ?file，失败返回 err

; file 方法
read-n = f.read(buf, n); 读取最多 n 位元组到 buf
line = f.read-line(); 读取一行（?str, nil=EOF 或错误）
content = f.read-bytes(); 读取整个档案为 ?[]byte（nil=空, err=失败）
content = f.read-str(); 读取整个档案为 ?str（nil=空, err=失败）
written = f.write(data, n); 写入 n 位元组
ok = f.write-str(data); 写入字串（覆写），返回 ?bool
ok = f.write-bytes(data); 写入 []byte（覆写），返回 ?bool
ok = f.append(data, n); 追加资料
ok = f.copy-to(dst-path); 复制到目标路径
ok = f.close(); 关闭（标准档案 stdin/stdout/stderr 不关闭）
yes = f.is-open(); 检查是否已开启（fd >= 0）
sz = f.size(); 档案大小（透过 fstat(fd)，消除 TOCTOU）

; 内建函数（注册于 src/builtin/os.go，以注释形式记录于 fs.no）
; 这些是 ForwardFunc/CLibCall 内建函数——无需 .no 实作。

; 档案读写（已废弃，建议改用 read-bytes/write-bytes 以支援错误处理）
; [deprecated] read-file: 改用 read-bytes（支援错误处理、TOCTOU 安全、循环 read）
;   空切片表示失败，无法区分空档案与错误
; [deprecated] write-file: 改用 write-bytes（支援错误处理、循环 write）
;   无法区分部分写入与完全失败
data = fs.read-file(path); [deprecated] 读取整个档案为 []byte（空切片表示失败）
ok = fs.write-file(path, data); [deprecated] 写入 []byte 到档案（覆写）。成功返回 true
fd = fs.open-read(path); 唯读开启（O_RDONLY）
fd = fs.open-write(path); 写入开启（O_WRONLY|O_CREAT|O_TRUNC, 0644）
fd = fs.open-file(path, flags, mode); 自订旗标和权限开启
n = fs.read(fd, buf, n); 底层读取
written = fs.write(fd, data, n); 底层写入
ok = fs.close(fd); 底层关闭

; 档案管理
ok = fs.remove(path); 删除档案（unlink）
ok = fs.rename(old, new); 重新命名
ok = fs.symlink(target, linkpath); 建立符号连结
ok = fs.link(oldpath, newpath); 建立硬连结
ok = fs.copy-file(src, dst); 复制档案内容

; 档案资讯
ok = fs.exists(path); 判断路径是否存在（跟随符号连结）
ok = fs.is-file(path); 判断是否为普通档案
ok = fs.is-dir(path); 判断是否为目录
sz = fs.stat-size(path); 取得档案大小（返回 ?i64）
sz = fs.file-size(path); 同 stat-size（返回 ?i64）
sz = fs.fstat-size(fd); 透过 fstat(fd) 取得大小，消除 TOCTOU（返回 ?i64）

; 注：档案模式/拥有者/修改时间的取得函式位于 `os` 模组
;     （os.stat-mode / os.stat-uid / os.stat-gid / os.stat-mtime）
ok = fs.lstat(path); 取得符号连结资讯（不跟随连结目标）

; 目录操作
dirp = fs.open-dir(path); 开启目录（返回控制代码，0=失败）
name = fs.read-dir(dirp); 读取下一个目录条目（返回 ?str）
ok = fs.close-dir(dirp); 关闭目录控制代码
names = fs.list-dir(path); 列出所有条目名称（包含 . 和 ..）
names = fs.dir-entries(path); 列出条目（不含 . 和 ..）
paths = fs.walk(root); 递回遍历目录树

; 路径解析
abs = fs.realpath(path); 解析为绝对规范路径
target = fs.readlink(path); 读取符号连结目标（返回 ?str）

; 临时档案/目录
name, fd = fs.mkstemp(tmpl); 建立临时档案（模板末尾为 XXXXXX）
name = fs.mkdtemp(tmpl); 建立临时目录（模板末尾为 XXXXXX，返回 ?str）

; 特殊档案
ok = fs.mkfifo(path, mode); 建立命名管道（FIFO）
ok = fs.mknod(path, mode, dev); 建立特殊档案（装置节点）
ok = fs.truncate(path, length); 截断或扩展档案到指定大小
fs.sync(); 将档案系统缓冲区排清到磁碟
ok = fs.touch-file(path); 更新档案时间戳为当前时间
ok = fs.utime(path, atime, mtime); 设定存取与修改时间
ok = fs.rmdir(path); 删除空目录

; 标准输入
line = fs.get-line(); 从标准输入读取一行（?str, nil=EOF）

; 便捷封装函数（Nolang 实作于 fs.no，内部呼叫内建函数）
content = fs.read-str(path); 读取整个档案为 ?str（nil=空, err=失败）
content = fs.read-bytes(path); 读取整个档案为 ?[]byte（nil=空, err=失败）
ok = fs.write-str(path, data); 写入字串到档案（覆写），返回 ?bool
ok = fs.write-bytes(path, data); 写入 []byte 到档案（覆写），返回 ?bool

; embed: 编译期嵌入的档案系统（由 #{embed='dir/path'} 初始化）
;   #{embed='../frontend/dist'}
;   DIST fs.embed
;   data = DIST.read('index.html')   ; 读取嵌入档案，返回 ([]byte, bool)
;   ok = DIST.exists('css/style.css') ; 检查嵌入档案是否存在
embed {
    count i64
    paths str; 所有路径拼接（用 \0 分隔）
    pathStarts []i64; 每个路径在 paths 中的起始偏移
    pathLens []i64; 每个路径的长度
    blob []byte; 所有档案内容拼接
    dataStarts []i64; 每个档案在 blob 中的起始偏移
    dataLens []i64; 每个档案的长度
}

; Windows 平台特定（仅 win-amd64/win-arm64 可用）
bufptr = os.win-find-first-file(path); FindFirstFileA，返回控制代码（0=失败）
name = os.win-find-next-file(bufptr); FindNextFileA，返回 ?str（nil=无更多条目）
ok = os.win-find-close(bufptr); FindClose

; open() 旗标常量（平台特定，此处显示 macOS 值）
O-RDONLY = 0
O-WRONLY = 1
O-RDWR = 2
O-CREAT = 512; macOS=512, Linux=64, Windows=256
O-TRUNC = 1024; macOS=1024, Linux=512, Windows=512
O-APPEND = 8; macOS=8, Linux=1024, Windows=8
O-EXCL = 2048; macOS=2048, Linux=128, Windows=1024
```

### env — 环境变数（简化封装）

```no
val = env.get(key); 返回 ?str（nil=未找到）
val = env.lookup(key); 返回 ?str（nil=未找到）
env.set(key, val)
env.unset(key)
val = env.get-with-default(key)
ok = env.is-set(key)
```

### args — 命令列引数

```no
n = args.count()
arg = args.get(i)
name = args.program()
ok = args.has-flag(name)
val = args.get-option(name)
arg = args.get-positional(i)
```

### path — 路径操作

以 `path` 结构体封装路径字串，所有操作以方法形式提供：

```no
#{fmt-skip}
SEP = 47     ; '/'（ASCII）
DOT = 46     ; '.'

; 结构体
path {
    p str
}

; 路径拼接与分解（原地修改 .p）
p = path{
    p: '/a/b/c.txt'
}
p.join(b str)           ; 拼接两个路径（原地修改）
p.base() (out)           ; 取档名
p.dir()                  ; 取目录（原地修改 .p）
p.ext() (out)            ; 取副档名
p.clean()                ; 正规化（原地修改 .p）
p.split() (f str)        ; 分割为目录+档名（.p 改为目录，返回档名）

; 路径判断
p.is-abs() (yes bool)    ; 是否为绝对路径

; 档案系统操作（委托 fs 内建函数）
p.exists() (yes bool)        ; 是否存在
p.is-dir() (yes bool)        ; 是否为目录
p.is-file() (yes bool)       ; 是否为档案
p.size() (sz ?i64)           ; 档案大小（nil = 不存在或 stat 失败）
p.make-dir() (ok bool)       ; 建立目录
p.remove() (ok bool)         ; 删除
p.rename(new-p str) (ok bool)    ; 重新命名
p.change-dir() (ok bool)     ; 切换工作目录

; 建构型方法
path.current() (out path)    ; 取得当前工作目录
```

### bufio — 缓冲读取

```no
r = reader.init(fd, buf); 初始化缓冲读取器（传回 reader）
ok = reader.fill(); 填充缓冲区
b = reader.read-byte(); 读取一个位元组（?byte, nil=EOF）
line = reader.read-line(); 读取一行（?str, nil=EOF 或错误）
reader.close(); 关闭
```

### io — 输入输出抽象

提供 `io-reader` 和 `io-writer` 结构体，统一档案、标准输入输出等资料流的读写操作：

```no
; 标准档案描述符
STDIN-FD = 0
STDOUT-FD = 1
STDERR-FD = 2

; io-reader 结构体
io-reader {
    fd i64
}
r = io-reader.from-fd(fd); 从 fd 建立
r = io-reader.from-stdin(); 从标准输入建立
read-n = r.read(buf, n); 读取 n 位元组
b = r.read-byte(); 读取一位元组（?byte, nil=EOF）
line = r.read-line(); 读取一行（?str, nil=EOF）
total = r.read-all(buf, size); 读取全部

; io-writer 结构体
io-writer {
    fd i64
}
w = io-writer.from-fd(fd); 从 fd 建立
w = io-writer.from-stdout(); 从标准输出建立
w = io-writer.from-stderr(); 从标准错误建立
written = w.write(data, n); 写入 n 位元组
written = w.write-str(s); 写入整个字串
written = w.write-byte(b); 写入一位元组
written = w.write-line(s); 写入字串+换行

; 便捷函数
n = io.out(s); 写入 stdout（不换行）
n = io.outln(s); 写入 stdout（换行）
n = io.err(s); 写入 stderr（不换行）
n = io.errln(s); 写入 stderr（换行）
line = io.read-line(); 从 stdin 读取一行（?str, nil=EOF）
```

### regexp — 正规表示式

以 `regexp` 结构体封装 pattern，完全使用 Nolang 实作正规表示式引擎（指令式 VM + 回溯匹配），不依赖 C 标准库 regex.h：

```no
; 结构体
regexp {
    pattern str
}

; 方法
re = regexp {
    pattern: '^hello'
}
matched = re.matches(text); 判断是否匹配
result = re.find(text); 查找第一个匹配子串
```

支援 **正则字面量语法** `/pattern/flags`（JavaScript 风格），在代码生成阶段脱糖为 `regexp-compile` 函数调用：

```no
; 正则字面量（推荐写法）
re = /\d+/
matched = re.matches('hello 123 world'); true

; 带旗标
re = /[a-z]+/gi

; 等价的显式调用
re = regexp-compile('\\d+')
```

### process — 进程操作

提供进程创建、标准流获取、进程等待、进程信息查询等功能。底层使用 POSIX fork/exec/pipe/waitpid：

```no
; 信号常量
SIG-TERM = 15
SIG-KILL = 9
SIG-INT = 2
SIG-STOP = 19
SIG-CONT = 18
SIG-CHLD = 17
WNOHANG = 1

; 结构体
process {
    pid i64
    stdin-fd i64
    stdout-fd i64
    stderr-fd i64
    exit-code i64
    running i64
}

; 进程创建
p = process.new()
ok = p.start-with-opts(program, args, dir, input, merge-err); 启动子进程（不等待）
ok = p.start(program, arg); 便捷方法：fork + exec，捕获 stdout

; 进程等待
status = p.wait(); 阻塞等待子进程结束
status = p.wait-nohang(); 非阻塞轮询；nil=仍在执行

; 进程控制
ok = p.kill(sig); 发送信号
ok = p.terminate(); SIG-TERM
ok = p.force-kill(); SIG-KILL

; 标准流操作
read-n = p.read(buf, n); 从 stdout 读取
line = p.read-line(); 读取一行（?str, nil=EOF）
content, n = p.read-all(); 读取全部 stdout
written = p.write(data, n); 写入 stdin
p.close-stdin(); 关闭 stdin 管道
p.close-stdout(); 关闭 stdout 管道
p.close-stderr(); 关闭 stderr 管道

; 进程信息
pid = p.pid-of(); 子进程 ID
code = p.exit-code-of(); 退出码
yes = p.is-running(); 是否仍在执行
pid = process.parent-pid(); 父进程 ID

; 生命周期
p.close(); 关闭所有管道并等待

; 跨平台一次性执行（推荐，扁平参数）：fork/exec + 捕获 stdout/stderr，带超时与环境
;   入参：program 可执行档路径，args 参数切片，dir 工作目录，input 写入子进程 stdin 的内容，
;         env K=V 形式环境变数切片，timeout 毫秒（<=0 表示不限时），merge-err 为 true 时合并 stderr 到 out
;   出参：out 捕获的 stdout（stderr 依 merge-err 决定是否合并），stderr 捕获的 stderr 输出，code 退出码（-2=超时杀死，-1=启动失败），err 错误信息
out, stderr, code, err = process.cmd('echo', ['hello'], '', '', [], 0, false)

; 结构体选项形式（推荐，可读性更好）：opts 结构体跨模组转发示范
;   cmdopts 定义于本模组，呼叫端在另一模组构造 process.cmdopts{...} 并按引用传入，
;   展示 nolang 的跨模组结构体定义转发能力。
cmdopts {
    dir str; 工作目录；空字串 = 继承父进程工作目录
    stdin str; 写入子进程 stdin 的内容；空字串 = 不提供 stdin
    env []str; 环境变数（["K=V", ...]）；空阵列 = 继承父进程环境
    timeout i64; 超时时间（毫秒），<=0 = 无限等待
    merge-err bool; true = 将 stderr 合并到捕获的 out 中
}
o = process.cmdopts {
    dir: ''
    stdin: 'piped-data'
    env: ['K=V']
    timeout: 200
    merge-err: false
}

; exec-result { stdout str, stderr str, code i64 }
res = process.exec('echo', ['hello'], o); 回传 exec-result（res.stdout / res.code ...）

; 便捷函数
code = process.shell(cmd); 透过 shell 执行命令，回传退出码
pid = process.spawn(cmd); 产生子进程，回传 pid
status = process.wait(pid); 等待指定 pid 结束
content, code = process.new().output(program, arg); 执行并捕获输出
```

### net — 网路操作

提供 TCP 网路编程能力，包括服务端监听、客户端连接、资料收发等。底层使用 POSIX socket API：

```no
; 网路常量
AF-INET = 2
SOCK-STREAM = 1
SOL-SOCKET = 65535
SO-REUSEADDR = 4
BACKLOG = 128

; listener 结构体
listener {
    fd i64
}

; 监听操作
l = listener {}
ok = l.listen(host, port); 建立 TCP 监听（socket+setsockopt+bind+listen）
c = l.accept(); 接受连接（?conn, nil=无连接）
l.close(); 关闭监听 socket
fd = l.fd-of(); 取得 fd

; conn 结构体
conn {
    fd i64
}

; 连接操作
c = conn {}
ok = c.dial(host, port); 建立 TCP 连接（socket+connect）
written = c.send(data); 发送字串
read-n = c.recv(buf, n); 接收资料到 buf
line = c.recv-line(); 接收一行（?str, nil=EOF, 最多 4096 位元组）
content, total = c.recv-all(); 接收全部直到连接关闭
c.close(); 关闭连接
fd = c.fd-of(); 取得 fd

; 便捷函数
l = net.listen-on(host, port); 建立监听器并开始监听（?listener）
c = net.dial-to(host, port); 建立连接并拨号（?conn）
```

### net/ip — IP 地址操作

提供 IPv4 地址的解析、验证、转换与分类功能。纯 Nolang 实作：

```no
; 预设地址常量
; 0.0.0.0
IP-ZERO

; 127.0.0.1
IP-LOOPBACK

; 0.0.0.0
IP-ANY

; 255.255.255.255
IP-BROADCAST

; ip-addr 结构体
ip-addr {
    a i64
    b i64
    c i64
    d i64
}

; 解析与转换
ip = ip-addr {}
ok = ip.parse('192.168.1.1'); 从字串解析
s = ip.to-str(); 转为字串 '192.168.1.1'
v = ip.to-u32(); 转为 u32（大端序）
ip.from-u32(v); 从 u32 建立

; 地址分类
yes = ip.is-loopback(); 127.0.0.0/8
yes = ip.is-private(); 10/8, 172.16/12, 192.168/16
yes = ip.is-zero(); 0.0.0.0
yes = ip.is-broadcast(); 255.255.255.255
yes = ip.is-multicast(); 224.0.0.0/4
yes = ip.is-link-local(); 169.254.0.0/16
yes = ip.is-class-a(); A 类（1~126）
yes = ip.is-class-b(); B 类（128~191）
yes = ip.is-class-c(); C 类（192~223）

; 比较与子网
yes = ip.equal(other); 地址相等比较
yes = ip.in-subnet(base, prefix-len); 子网包含检查

; 便捷函数
addr = ip.ip-parse(s); 快速解析（?ip-addr, nil=无效）
yes = ip.ip-is-loopback(s); 快速判断环回
yes = ip.ip-is-private(s); 快速判断私有
```

### net/sse — Server-Sent Events 客户端

支援 W3C EventSource 规范的 SSE 串流接收。底层使用 HTTP/1.1 长连接，支援明文 HTTP 与 HTTPS（TLS）：

```no
; sse-event 结构体
sse-event {
    event str; 事件类型（预设 'message'）
    data str; 事件资料（多行 data 以 \n 连接）
    id str; 事件 ID
    retry i64; 重连等待毫秒数（-1=未设定）
}

; sse-client 结构体
sse-client {
    fd i64; TCP socket fd
    tls-c tls.conn; TLS 连线
    use-tls bool; 是否使用 TLS
    connected bool; 连线状态
    host str; 伺服器主机名
    port i64; 埠号
    path str; 请求路径
    last-event-id str; 最后收到的事件 ID
    recv-buf str; 接收缓冲区
    recv-buf-len i64; 缓冲区资料长度
}

; 连接与事件接收
client = sse.connect('http://host:3000/events'); 返回 ?sse-client
client: {
    nil -> print('connect failed')

    -> {
        ev = client.next-event(); 返回 ?sse-event（nil=EOF, err=错误）
        ev: {
            nil -> print('connection closed')

            err -> print('error: ' - it.msg())

            -> print(ev.data)
        }
        client.close()
    }
}

; 其他方法
yes = client.is-connected(); 检查连线状态
ok = client.reconnect(); 重新连线（使用 last-event-id）
```

### net/http — HTTP/1.1 客户端

提供 HTTP/1.1 协议的客户端，支援 GET、POST、PUT、DELETE、PATCH 等方法，可选 TLS：

```no
; 结构体（以下为 std/http 模组内的定义；模组内定义不加模组前缀，加了会被读成 struct literal）
request {
    method str
    url str
    body str
    headers [16]str
    header-count i64
}
response {
    status-code i64
    status-text str
    headers str
    header-names [32]str
    header-values [32]str
    header-count i64
    body str
}

; 便捷函数
resp = http.get(url); GET 请求（?http.response）
resp = http.post(url, body); POST 请求（?http.response）
resp = http.put(url, body); PUT 请求（?http.response）
resp = http.delete(url); DELETE 请求（?http.response）
resp = http.patch(url, body); PATCH 请求（?http.response）
resp = http.do(method, url, body); 自订方法（?http.response）
resp = http.get-auth(url, user, password); 带 Basic 认证的 GET（?http.response）
resp = http.post-auth(url, body, user, password); 带 Basic 认证的 POST（?http.response）

; 使用 request 物件
req = http.request {}
req.init('POST', url, body)
req.add-header('Content-Type', 'application/json')
ok = http.set-basic-auth(req, user, password); 设定 Basic 认证标头（bool）
ok = http.set-bearer-auth(req, token); 设定 Bearer 标头（bool）
ok = http.set-api-key(req, header-name, api-key); 设定 API-key 标头（bool）
resp = http.do-req(req); 发送请求（?http.response）

; 解析与读取回应标头
resp.parse-headers()
val = resp.get-header(name); 查询标头值（?str）
```

### net/http2 — HTTP/2.0 客户端（RFC 7540）

支援 HTTP/2 影格解析与连线管理，支援 h2c prior knowledge 模式：

```no
; 影格结构体
http2-frame {
    length i64
    frame-type i64
    flags i64
    stream-id i64
    payload str
}

; 连线结构体
http2-conn {
    fd i64
    next-stream-id i64
    send-window i64
    recv-window i64
    initialized bool
    use-tls bool
}

; 连线与请求
c = http2.connect(host, port); 建立连线（?http2-conn）
resp = http2.do(method, url, body); 发送请求（?http.response）

; 影格操作
frame = http2-frame {}
pos = frame.parse(data, pos); 解析影格（?i64）
pos = frame.serialize(buf, pos); 序列化影格
ok = c.send-frame(frame); 发送影格
frame = c.recv-frame(); 接收影格（?http2-frame）
```

### net/http3 — HTTP/3.0 客户端（RFC 9114）

基于 QUIC 协议的 HTTP/3 客户端：

```no
; 方法常量
HTTP3-METHOD-GET = 'GET'
HTTP3-METHOD-POST = 'POST'
HTTP3-METHOD-PUT = 'PUT'
HTTP3-METHOD-DELETE = 'DELETE'
HTTP3-METHOD-PATCH = 'PATCH'
HTTP3-METHOD-HEAD = 'HEAD'
HTTP3-METHOD-OPTIONS = 'OPTIONS'

; 便捷函数
c = http3.connect(host, port); 建立 QUIC 连线（?http3-conn）
resp = http3.send-request(c, method, path, host, body); 发送请求（?http.response）
resp = http3.get(url); GET 请求（?http.response）
resp = http3.post(url, body); POST 请求（?http.response）

; QPACK 标头编解码
buf, n = http3.encode-header(name, value)
buf, n = http3.encode-headers(names, values, count)
name, value, pos = http3.decode-header(buf, pos)
```

### net/ws — WebSocket 客户端与服务端（RFC 6455）

支援 WebSocket 协议的全双工通讯，可作为客户端或服务端：

```no
; 讯息结构体
ws-message {
    opcode i64; 0=continuation, 1=text, 2=binary, 8=close, 9=ping, 10=pong
    data str
    fin bool
}

; 服务端
s = ws.listen-on(host, port); 建立监听（?ws-server）
c = s.accept(); 接受连接（?ws-server-conn）
msg = c.recv(); 接收讯息（?ws-message）
ok = c.send-text(text); 发送文字
ok = c.send-binary(data); 发送二进制
c.close()

; 客户端
c = ws.connect(url); 连接服务端（?ws-client）
msg = c.recv(); 接收讯息（?ws-message）
ok = c.send-text(text); 发送文字
ok = c.send-binary(data); 发送二进制
c.close()
```

### net/tls — TLS 1.2/1.3 客户端（纯 Nolang 实现）

提供 TLS 加密连接，支援 TLS 1.2 和 1.3：

```no
; 连接
c = tls.dial(host, port); 建立 TLS 连接（?tls.conn）
n = c.send(data); 发送加密资料（?i64）
n = c.recv(buf, n); 接收解密资料（?i64）
c.close()
```

### net/client — 高阶 TCP 客户端

封装 `conn` 结构体，提供自动重连等功能：

```no
c = client.client-create(host, port); 建立客户端（?client）
ok = c.connect(host, port); 连接
ok = c.reconnect(); 重连
written = c.send(data); 发送
read-n = c.recv(buf, n); 接收
line = c.recv-line(); 接收一行（?str）
written = c.send-line(line); 发送一行（i64）
response = c.request(data); 请求-回应模式（?str）
yes = c.is-connected(); 连接状态
c.close()
```

### net/quic — QUIC 协议（RFC 9000）

提供 QUIC 传输协议实现，作为 HTTP/3 的底层传输层：

```no
c = quic.dial(host, port); 建立 QUIC 连接（?quic.conn）
n = c.send(data, n); 发送资料
n = c.recv(buf, n); 接收资料
c.close()
```

### net/server — HTTP 伺服器

提供 HTTP 伺服器功能：

```no
s = server {}
ok = s.listen(host, port); 开始监听
c = s.accept(); 接受一条连接（?server-conn）
s.close()
```

### net/dns — DNS 解析

提供 DNS 查询功能：

```no
ip = dns.resolve(host); 解析主机名（?str）
```

### net/url — URL 解析

提供 URL 解析与建构功能：

```no
u = url.url-parse(url); 解析 URL
s = u.to-str(); 转为字串
```

### net/cookie — HTTP Cookie

提供 HTTP Cookie 的解析与管理：

```no
c = cookie {}
c.parse(set-cookie-header)
s = c.to-str()
```

### net/multipart — Multipart 表单资料

提供 multipart/form-data 的解析与建构：

```no
; 写入（建构 multipart/form-data）
w = multipart.multipart-create(); 建立写入器（?multipart-writer）
w.write-field(name, value); 写入栏位
body = w.body-of(); 取得编码后的 body
ct = w.content-type(); 取得 content-type（含 boundary）

; 解析
r = multipart.multipart-parse(content-type, body); 解析（?multipart-reader）
```

### net/hpack — HPACK 标头压缩（HTTP/2）

提供 HPACK 演算法的编解码，用于 HTTP/2 标头压缩：

```no
; 标头表操作
t = hpack.tables {}
t.init(); 初始化静态+动态表
out = t.encode-header(name, value); 编码单一标头（str）
names, values, count, ok = t.decode-headers(data); 解码标头（[]str）
t.dyn-add(name, value); 加入动态表
idx = t.find(name, value); 查完整 name/value
idx = t.find-name(name); 仅按 name 查
name, value, ok = t.lookup(idx); 按索引查

; 整数/字串原语编解码
out = hpack.encode-int(val, prefix-bits, prefix-byte)
val, next-pos = hpack.decode-int(data, pos, prefix-bits)
out = hpack.encode-str(s)
out, next-pos = hpack.decode-str(data, pos)
```

### net/proxy — 代理支援

提供 HTTP/SOCKS 代理连接功能：

```no
c = proxy.dial(proxy-url, target-host, target-port)
```

### net/pool — 连接池

提供网路连接的池化管理，重用连接以提升效能：

```no
p = pool {}
p.init(capacity)
c = p.get(); 从池中取得连接
p.put(c); 归还连接
p.close()
```

### net/unix — Unix 域套接字

提供 Unix 域套接字通讯：

```no
fd = unix.unix-listen(path); 监听（回传 fd）
fd = unix.unix-dial(path); 连接（回传 fd）
l = unix.unix-listen-on(path); 建立监听器（?unix-listener）
c = l.accept(); 接受连接（?unix-conn）
```

---
