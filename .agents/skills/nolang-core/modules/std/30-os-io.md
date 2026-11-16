# os, fs, env, args, path, bufio, io, regexp, process

> 视图切分自原 `nolang-std/SKILL.md`，正文未改写；索引与加载判据见 [`../m20-std.md`](../m20-std.md)。

### OS & Files

#### os — Operating System Interface

Provides environment variables, directory operations, process management, system information, time, etc. For file read/write functionality, see the `fs` module.

```no
; Environment variables
val = os.get-env(key)
os.set-env(key, val)

; Directory
dir = os.get-wd()
os.ch-dir(dir)
os.mkdir(path, mode)

; Process
exit(code); End the process (global builtin, no `os.` prefix — see global)
pid = os.get-pid()

; System information
name = os.host-name()
arch = os.get-arch()
msg = os.strerror(errnum); Convert errno to human-readable message
errno = os.get-errno(); Get last errno value

; User and group
uid = os.getuid(); Current user ID
gid = os.getgid(); Current group ID
euid = os.geteuid(); Effective user ID
egid = os.getegid(); Effective group ID
gids = os.getgroups(); Supplementary group IDs
login = os.get-login(); Login user name
hostid = os.get-host-id(); Host ID (32-bit)
uname = os.getpwuid-name(uid); Resolve UID to login name (empty if unknown)
gname = os.getgrgid-name(gid); Resolve GID to group name (empty if unknown)
gids, n = os.getgrouplist(user, basegid); Full group list for user (getgrouplist)

; File permissions and info
ok = os.ch-mod(path, mode); Change file permissions
ok = os.chown(path, uid, gid); Change file owner/group
mode = os.stat-mode(path); Get file mode bits
uid = os.stat-uid(path); Get file owner uid
gid = os.stat-gid(path); Get file group gid
mtime = os.stat-mtime(path); Get file modification time
nlink = os.stat-nlink(path); Hard link count (st_nlink)
ino = os.stat-ino(path); Inode number (st_ino)
atime = os.stat-atime(path); Access time (Unix seconds, st_atime)
ctime = os.stat-ctime(path); Inode change time (Unix seconds, st_ctime)
blocks = os.stat-blocks(path); Allocated 512-byte block count (st_blocks)
blksize = os.stat-blksize(path); Optimal I/O block size (st_blksize)

; System configuration
val = os.sysconf(name); Query system config limit
n = os.num-cpu(); Number of online CPUs
u = os.uname(); Kernel and architecture info (utsname)

; Process control
ok = os.set-priority(which, who, prio); Set process priority
prio = os.get-priority(which, who); Get process priority
pid = os.set-sid(); Create new session, return pgid
ok = os.signal(sig, handler); Set signal handler (0=DFL, 1=IGN)
ok = os.flock(fd, op); File advisory lock (1=SH,2=EX,4=UN,8=NB)

; Misc system
val = os.sysctl(name); Query sysctl parameter (returns str, ok)
name = os.get-domain-name(); NIS domain name
os.syslog(priority, msg); Write to system log
ok = os.chroot(path); Change root directory (needs root)
name = os.ttyname(fd); Get terminal name (returns str)

; Time (builtins, map to nolang.now_s/ms/us/ns and nolang.sleep_s/us/ns)
sec = os.now()
ms = os.now-ms()
us = os.now-us()
ns = os.now-ns()
os.sleep(sec)
os.sleep-us(us)
os.sleep-ns(ns)

; Command-line arguments
count = os.args()
val = os.arg(idx)

; utsname struct (returned by os.uname())
utsname {
    sysname str
    nodename str
    release str
    version str
    machine str
}

; Windows-specific
ok = os.win-wsa-startup(); Initialize Winsock 2.2 (Windows only)
```

#### fs — File System Tools

Wraps open files with the `file` struct and paths with the `path` struct. Defines the `fd` newtype (`fd = i64`) to prevent file descriptors from being confused with arbitrary `i64` values. Also provides an `embed` struct for compile-time embedded filesystems (via `#{embed='dir/path'}`).

```no
; fd newtype (underlying i64, type-distinct from i64)
fd = i64

; File struct
file {
    fd fd
    path str
}

; Standard files (integer literals are allowed for fd initialization)
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

; Open file (with options)
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
f = fs.open(path, opts); Open file, returns ?file on success, err on failure

; file methods
read-n = f.read(buf, n); Read up to n bytes into buf
line = f.read-line(); Read one line (?str, nil=EOF or error)
content = f.read-bytes(); Read entire file as ?[]byte (nil=empty, err=failure)
content = f.read-str(); Read entire file as ?str (nil=empty, err=failure)
written = f.write(data, n); Write n bytes from data
ok = f.write-str(data); Write string to file (overwrite), returns ?bool
ok = f.write-bytes(data); Write []byte to file (overwrite), returns ?bool
ok = f.append(data, n); Append data to file
ok = f.copy-to(dst-path); Copy file to target path
ok = f.close(); Close (standard files stdin/stdout/stderr are not closed)
yes = f.is-open(); Check if file is open (fd >= 0)
sz = f.size(); File size via fstat(fd), eliminates TOCTOU

; Built-in functions (registered in src/builtin/os.go, documented as comments in fs.no)
; These are ForwardFunc/CLibCall builtins — no .no implementation needed.

; File read/write (deprecated, prefer read-bytes/write-bytes for error handling)
; [deprecated] read-file: 改用 read-bytes（支援錯誤處理、TOCTOU 安全、循環 read）
;   空切片表示失敗，無法區分空檔案與錯誤
; [deprecated] write-file: 改用 write-bytes（支援錯誤處理、循環 write）
;   無法區分部分寫入與完全失敗
data = fs.read-file(path); [deprecated] Read entire file as []byte (empty on error, cannot distinguish empty from failure)
ok = fs.write-file(path, data); [deprecated] Write []byte to file (overwrite). Returns true on success
fd = fs.open-read(path); Open read-only (O_RDONLY)
fd = fs.open-write(path); Open for writing (O_WRONLY|O_CREAT|O_TRUNC, 0644)
fd = fs.open-file(path, flags, mode); Open with custom flags and permissions
n = fs.read(fd, buf, n); Low-level read from fd into buf
written = fs.write(fd, data, n); Low-level write to fd
ok = fs.close(fd); Low-level close

; File management
ok = fs.remove(path); Delete file (unlink)
ok = fs.rename(old, new); Rename file
ok = fs.symlink(target, linkpath); Create symbolic link
ok = fs.link(oldpath, newpath); Create hard link
ok = fs.copy-file(src, dst); Copy file content

; File information
ok = fs.exists(path); Check if path exists (follows symlinks)
ok = fs.is-file(path); Check if regular file
ok = fs.is-dir(path); Check if directory
sz = fs.stat-size(path); Get file size (returns ?i64)
sz = fs.file-size(path); Same as stat-size (returns ?i64)
sz = fs.fstat-size(fd); Get file size via fstat(fd), eliminates TOCTOU (returns ?i64)

; Other stat fields available via os.stat-{nlink,ino,atime,ctime,blocks,blksize}
ok = fs.lstat(path); Get symlink info (does not follow link target)

; note: file mode/owner/mtime getters live in the `os` package
; (os.stat-mode / os.stat-uid / os.stat-gid / os.stat-mtime)

; Directory operations
dirp = fs.open-dir(path); Open directory (returns handle, 0 on failure)
name = fs.read-dir(dirp); Read next dir entry (returns ?str)
ok = fs.close-dir(dirp); Close directory handle
names = fs.list-dir(path); List all entry names (includes . and ..)
names = fs.dir-entries(path); List entries (excludes . and ..)
paths = fs.walk(root); Recursively walk directory tree

; ⚠ fs.list-dir 遵循 POSIX readdir，返回值包含 "." 和 ".."。任何「递归遍历 +
; 破坏性删除(fs.remove/fs.rmdir)」必须用 fs.dir-entries（已自动过滤 "."/".."），
; 否则递归进入 ".." 会向上逃逸出目标目录、误删整棵父树。`no vet` 对此组合报
; WARNING [nolang-list-dir-unfiltered]。

; Path resolution
abs = fs.realpath(path); Resolve to absolute canonical path
target = fs.readlink(path); Read symbolic link target (returns ?str)

; Temp file/directory
name, fd = fs.mkstemp(tmpl); Create temp file (template ending XXXXXX)
name = fs.mkdtemp(tmpl); Create temp directory (template ending XXXXXX, returns ?str)

; Special files
ok = fs.mkfifo(path, mode); Create named pipe (FIFO)
ok = fs.mknod(path, mode, dev); Create special file (device node)
ok = fs.truncate(path, length); Truncate/extend file to length
fs.sync(); Flush filesystem buffers to disk
ok = fs.touch-file(path); Update file timestamps to current time
ok = fs.utime(path, atime, mtime); Set file access and modification times
ok = fs.rmdir(path); Remove empty directory

; Standard input
line = fs.get-line(); Read one line from stdin (?str, nil=EOF)

; Convenience wrappers (Nolang-implemented in fs.no, call builtins internally)
content = fs.read-str(path); Read entire file as ?str (nil=empty, err=failure)
content = fs.read-bytes(path); Read entire file as ?[]byte (nil=empty, err=failure)
ok = fs.write-str(path, data); Write string to file (overwrite), returns ?bool
ok = fs.write-bytes(path, data); Write []byte to file (overwrite), returns ?bool

; embed: compile-time embedded filesystem (initialized by #{embed='dir/path'})
;   #{embed='../frontend/dist'}
;   DIST fs.embed
;   data = DIST.read('index.html')   ; Read embedded file, returns ([]byte, bool)
;   ok = DIST.exists('css/style.css') ; Check if embedded file exists
embed {
    count i64
    paths str; All paths concatenated (\0 separated)
    pathStarts []i64; Start offset of each path in paths
    pathLens []i64; Length of each path
    blob []byte; All file contents concatenated
    dataStarts []i64; Start offset of each file in blob
    dataLens []i64; Length of each file
}

; Windows-specific (only available on win-amd64/win-arm64)
; (these directory-scan helpers are registered in the `os` package)
bufptr = os.win-find-first-file(path); FindFirstFileA, returns handle (0=failure)
name = os.win-find-next-file(bufptr); FindNextFileA, returns (name, ok)
ok = os.win-find-close(bufptr); FindClose

; open() flag constants (platform-specific, shown for macOS)
O-RDONLY = 0
O-WRONLY = 1
O-RDWR = 2
O-CREAT = 512; macOS=512, Linux=64, Windows=256
O-TRUNC = 1024; macOS=1024, Linux=512, Windows=512
O-APPEND = 8; macOS=8, Linux=1024, Windows=8
O-EXCL = 2048; macOS=2048, Linux=128, Windows=1024
```

#### env — Environment Variables (simplified wrapper)

```no
val = env.get(key)
val = env.lookup(key); Returns ?str (nil=not found)
env.set(key, val)
env.unset(key)
val = env.get-with-default(key)
ok = env.is-set(key)
```

#### args — Command-line Arguments

```no
n = args.count()
arg = args.get(i)
name = args.program()
ok = args.has-flag(name)
val = args.get-option(name)
arg = args.get-positional(i)
```

#### path — Path Operations

Wraps path strings with the `path` struct; all operations are provided as methods:

```no
SEP = 47     ; '/' (ASCII)
DOT = 46     ; '.'

; Struct
path {
    p str
}

; Path join and split (modifies .p in place)
p = path{
    p: '/a/b/c.txt'
}
p.join(b str)           ; Join two paths (modifies in place)
p.base() (out)           ; Get filename
p.dir()                  ; Get directory (modifies .p in place)
p.ext() (out)            ; Get file extension
p.clean()                ; Normalize (modifies .p in place)
p.split() (f str)        ; Split into directory + filename (.p becomes directory, returns filename)

; Path checks
p.is-abs() (yes bool)    ; Whether it is an absolute path

; File system operations (delegates to fs built-in functions)
p.exists() (yes bool)        ; Whether it exists
p.is-dir() (yes bool)        ; Whether it is a directory
p.is-file() (yes bool)       ; Whether it is a file
p.size() (sz ?i64)           ; File size (nil = not found / stat failure)
p.make-dir() (ok bool)       ; Create directory
p.remove() (ok bool)         ; Delete
p.rename(new-p str) (ok bool)    ; Rename
p.change-dir() (ok bool)     ; Change working directory

; Constructor methods
path.current() (out path)    ; Get current working directory
```

#### bufio — Buffered Reading

```no
r = reader.init(fd, buf); Initialize buffered reader (returns reader)
ok = reader.fill(); Fill buffer
b = reader.read-byte(); Read one byte (?byte, nil=EOF)
ok = reader.read-line(line); Read one line into line
reader.close(); Close
```

#### io — I/O Abstraction

Provides `io-reader` and `io-writer` structs to unify read/write operations across files, standard I/O, and other streams:

```no
; Standard file descriptors
STDIN-FD = 0
STDOUT-FD = 1
STDERR-FD = 2

; io-reader struct
io-reader {
    fd i64
}
r = io-reader.from-fd(fd); Create from fd
r = io-reader.from-stdin(); Create from standard input
read-n = r.read(buf, n); Read n bytes
b = r.read-byte(); Read one byte (?byte, nil=EOF)
line = r.read-line(); Read one line (?str, nil=EOF)
total = r.read-all(buf, size); Read all

; io-writer struct
io-writer {
    fd i64
}
w = io-writer.from-fd(fd); Create from fd
w = io-writer.from-stdout(); Create from standard output
w = io-writer.from-stderr(); Create from standard error
written = w.write(data, n); Write n bytes
written = w.write-str(s); Write entire string
written = w.write-byte(b); Write one byte
written = w.write-line(s); Write string + newline

; Convenience functions
n = io.out(s); Write to stdout (no newline)
n = io.outln(s); Write to stdout (with newline)
n = io.err(s); Write to stderr (no newline)
n = io.errln(s); Write to stderr (with newline)
line = io.read-line(); Read one line from stdin (?str, nil=EOF)
```

#### regexp — Regular Expression

Wraps pattern with the `regexp` struct; underlying implementation uses C standard library `regex.h`:

```no
; Struct
regexp {
    pattern str
}

; Methods
re = regexp {
    pattern: '^hello'
}
matched = re.matches(text); Check whether it matches
result = re.find(text); Find first matching substring
pos = re.match-at(text, start); Match at specific position
ok = re.compile(); Compile pattern (method)

; Top-level convenience functions
re = regexp-compile(pattern); Compile regex pattern
result = regexp-find(pattern, text); Find first match
yes = regexp-match(pattern, text); Check if pattern matches text
```

#### process — Process Operations

Provides process creation, standard stream access, process waiting, and process information query. Underlying implementation uses POSIX fork/exec/pipe/waitpid:

```no
; Signal constants
SIG-TERM = 15
SIG-KILL = 9
SIG-INT = 2
SIG-STOP = 19
SIG-CONT = 18
SIG-CHLD = 17
WNOHANG = 1

; Struct
process {
    pid i64
    stdin-fd i64
    stdout-fd i64
    stderr-fd i64
    exit-code i64
    running i64
}

; Process creation
p = process.new()
ok = p.start-with-opts(program, args, dir, input, merge-err); Start child process (no wait)
ok = p.start(program, arg); Convenience: fork + exec, capture stdout

; Process waiting
status = p.wait(); Block waiting for child process to end
status = p.wait-nohang(); Non-blocking polling; nil=still running

; Process control
ok = p.kill(sig); Send signal
ok = p.terminate(); SIG-TERM
ok = p.force-kill(); SIG-KILL

; Standard stream operations
read-n = p.read(buf, n); Read from stdout
line = p.read-line(); Read one line (?str, nil=EOF)
content, n = p.read-all(); Read all stdout
written = p.write(data, n); Write to stdin
p.close-stdin(); Close stdin pipe
p.close-stdout(); Close stdout pipe
p.close-stderr(); Close stderr pipe

; Process information
pid = p.pid-of(); Child process ID
code = p.exit-code-of(); Exit code
yes = p.is-running(); Whether still running
pid = process.parent-pid(); Parent process ID

; Lifecycle
p.close(); Close all pipes and wait

; Convenience functions
code = process.shell(cmd); Execute via shell, return exit code
pid = process.spawn(cmd); Spawn child process, return pid
status = process.wait(pid); Wait for specific PID
content, code = process.new().output(program, arg); Execute and capture stdout
out, stderr, code, err = process.cmd(program, args, dir, input, env, timeout, merge-err); Run command
res = process.exec(program, args, opts); opts cmdopts, returns exec-result

; Process listing
pids = process.list-all(); List all processes
pids = process.find-by-name(name); Find by process name
pids = process.find-match(pattern); Find matching pattern
```
