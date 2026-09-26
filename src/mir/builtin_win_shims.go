package mir

// builtin_win_shims.go — Windows shims for POSIX builtins the CRT does not export.
//
// Why shims live in the MIR prelude
// ---------------------------------
// The builtin registry (src/builtin/os.go, process.go) marks POSIX-only entry
// points with CLibCall.AltFuncs{"windows": "nolang.win_*"}; emitBuiltinCLib
// then lowers `os.mkfifo` etc. into a plain call to that symbol (decl()
// deliberately skips `nolang.win_` declares so the prelude definition wins).
// Historically those symbols were only ever selected when the COMPILER ran on
// Windows (runtime.GOOS in init()) and — worse — nothing in the tree ever
// defined them, so both native and cross Windows builds died at lld-link with
// `undefined symbol: mkfifo/uname/kill/...`. Defining the family here, gated
// on the compilation TARGET (emitPrelude → windowsShimsIR), makes every
// AltFuncs reference resolvable.
//
// Conventions the shims must honour (see emitBuiltinCLib):
//   - CmpRet builtins compare the i32 result with `icmp eq ret, 0`, so 0 means
//     SUCCESS and -1 failure — the POSIX convention, not Win32's TRUE/FALSE.
//   - `os.get-priority` detects failure via errno == 0 around the call, so
//     nolang.win_getpriority must report through _errno. Other failure paths
//     set errno best-effort because tools like `nice`/`flock` print it.
//   - Handles are i64 (64-bit on both x86_64 and aarch64 Windows, matching
//     builtin_readlink_win.go).
//
// Only Win32 entry points from kernel32/msvcrt are used — the two libraries
// zig's `-windows-gnu` targets link by default (plus ws2_32, which the
// builder passes explicitly); no extra -l flags are needed.

import "strings"

// windowsShimsIR returns the prelude block. The machine field of win_uname
// depends on the target architecture, so the @MACHINE@ global line is the only
// rendered piece; keeping the rest as a raw constant avoids escaping every
// LLVM SSA `%` register through a format string.
func windowsShimsIR() string {
	machine := targetGOARCH()
	switch machine {
	case "amd64":
		machine = "x86_64"
	case "arm64":
		machine = "aarch64"
	case "386":
		machine = "x86"
	}
	machLen := len(machine) + 1 // NUL-terminated byte array
	line := "@.win.u.machine = private constant [" + itoa(machLen) + " x i8] c\"" + machine + "\\00\"\n"
	out := strings.Replace(windowsShimsConst, "@MACHINE@\n", line, 1)
	out = strings.ReplaceAll(out, "@MACHTY@", "["+itoa(machLen)+" x i8]")
	return strings.ReplaceAll(out, "@MACHN@", itoa(machLen))
}

// itoa keeps the import surface at "strings" for a one-line integer render.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

const windowsShimsConst = `
; --- Windows shims for POSIX builtins (target-gated, see builtin_win_shims.go)
declare i32* @_errno()
declare i32 @_isatty(i32)
declare i32 @_truncate(i8*, i32)
declare i64 @_get_osfhandle(i32)
declare i64 @GetCurrentProcess()
declare i32 @GetCurrentProcessId()
declare i64 @OpenProcess(i32, i32, i32)
declare i32 @CloseHandle(i64)
declare i32 @TerminateProcess(i64, i32)
declare i32 @CreateSymbolicLinkA(i8*, i8*, i32)
declare i32 @CreateHardLinkA(i8*, i8*, i8*)
declare i32 @SetPriorityClass(i64, i32)
declare i32 @GetPriorityClass(i64)
declare i32 @LockFileEx(i64, i32, i32, i32, i32, i8*)
declare i32 @UnlockFileEx(i64, i32, i32, i32, i8*)
declare i64 @CreateToolhelp32Snapshot(i32, i32)
declare i32 @Process32FirstW(i64, i8*)
declare i32 @Process32NextW(i64, i8*)
declare i32 @GetEnvironmentVariableA(i8*, i8*, i32)
; llvm.memcpy/memset are already declared by the main prelude (emitPrelude);
; repeating them here makes the two spellings (i8* vs ptr) collide in opt.

@.win.cn = private constant [13 x i8] c"COMPUTERNAME\00" ; env name for nodename/hostid
@.win.con = private constant [5 x i8] c"con:\00"
@.win.u.sys = private constant [8 x i8] c"Windows\00"
@.win.u.rel = private constant [5 x i8] c"10.0\00"
@.win.u.ver = private constant [7 x i8] c"nolang\00"
@MACHINE@

; win_seterrno centralises the UCRT errno location write.
define internal void @nolang.win_seterrno(i32 %v) {
entry:
  %e = call i32* @_errno()
  store i32 %v, i32* %e
  ret void
}

; Unsupported stubs: fail with ENOSYS(40)/EACCES(13), POSIX -1 convention.
define i32 @nolang.win_mkfifo(i8* %p, i32 %mode) {
entry:
  call void @nolang.win_seterrno(i32 40)
  ret i32 -1
}
define i32 @nolang.win_mknod(i8* %p, i32 %mode, i64 %dev) {
entry:
  call void @nolang.win_seterrno(i32 40)
  ret i32 -1
}
define i32 @nolang.win_chroot(i8* %p) {
entry:
  call void @nolang.win_seterrno(i32 40)
  ret i32 -1
}
define i32 @nolang.win_chown(i8* %p, i32 %uid, i32 %gid) {
entry:
  call void @nolang.win_seterrno(i32 13) ; EACCES
  ret i32 -1
}

; Windows has no POSIX credentials; every process "user" is the token owner.
define i32 @nolang.win_getuid() { ret i32 0 }
define i32 @nolang.win_getgid() { ret i32 0 }

; setsid(2) has no twin; a new session cannot be created, but reporting the
; current pid (>0, like POSIX's success return) keeps process.cmd detachers
; working instead of failing every spawn.
define i32 @nolang.win_setsid() {
entry:
  %p = call i32 @GetCurrentProcessId()
  ret i32 %p
}

; getppid(2): walk the toolhelp process snapshot for our pid and read the
; parent field. PROCESSENTRY32W on Win64: th32ProcessID @8,
; th32ParentProcessID @32, sizeof 568 (toolhelp.h, LLP64 alignment).
define i32 @nolang.win_getppid() {
entry:
  %snap = call i64 @CreateToolhelp32Snapshot(i32 2, i32 0)
  %bad = icmp eq i64 %snap, -1
  br i1 %bad, label %fail, label %init
init:
  %pe = alloca [568 x i8], align 8
  %pe8 = bitcast [568 x i8]* %pe to i8*
  %dsz = bitcast i8* %pe8 to i32*
  store i32 568, i32* %dsz
  %self = call i32 @GetCurrentProcessId()
  %first = call i32 @Process32FirstW(i64 %snap, i8* %pe8)
  %hasf = icmp ne i32 %first, 0
  br i1 %hasf, label %loop, label %fail
loop:
  %pidp = getelementptr i8, i8* %pe8, i64 8
  %pid32 = bitcast i8* %pidp to i32*
  %pid = load i32, i32* %pid32
  %hit = icmp eq i32 %pid, %self
  br i1 %hit, label %found, label %next
next:
  %more = call i32 @Process32NextW(i64 %snap, i8* %pe8)
  %hasmore = icmp ne i32 %more, 0
  br i1 %hasmore, label %loop, label %fail
found:
  %ppp = getelementptr i8, i8* %pe8, i64 32
  %pp32 = bitcast i8* %ppp to i32*
  %pp = load i32, i32* %pp32
  call i32 @CloseHandle(i64 %snap)
  ret i32 %pp
fail:
  ret i32 -1
}

; kill(2): OpenProcess + TerminateProcess. sig 0 is the POSIX "does it exist"
; probe, answered with a query-rights open only. Exit code 1 stands in for the
; 128+signal convention (Windows terminations have no waitpid status to match).
define i32 @nolang.win_kill(i32 %pid, i32 %sig) {
entry:
  %h = call i64 @OpenProcess(i32 4097, i32 0, i32 %pid) ; TERMINATE|QUERY_LIMITED
  %nul = icmp eq i64 %h, 0
  br i1 %nul, label %nogood, label %got
nogood:
  call void @nolang.win_seterrno(i32 3) ; ESRCH
  ret i32 -1
got:
  %istest = icmp eq i32 %sig, 0
  br i1 %istest, label %justprobe, label %justkill
justprobe:
  call i32 @CloseHandle(i64 %h)
  ret i32 0
justkill:
  %t = call i32 @TerminateProcess(i64 %h, i32 1)
  %okt = icmp ne i32 %t, 0
  call i32 @CloseHandle(i64 %h)
  br i1 %okt, label %ksucc, label %kfail
ksucc:
  ret i32 0
kfail:
  call void @nolang.win_seterrno(i32 13) ; EACCES
  ret i32 -1
}

; symlink(2): CreateSymbolicLinkA(linkpath, target, flags). BOOLEAN TRUE means
; success, inverted to the 0-on-success convention.
define i32 @nolang.win_symlink(i8* %target, i8* %linkpath) {
entry:
  %r = call i32 @CreateSymbolicLinkA(i8* %linkpath, i8* %target, i32 1) ; UNPRIVILEGED
  %ok = icmp ne i32 %r, 0
  br i1 %ok, label %succ, label %fail
succ:
  ret i32 0
fail:
  call void @nolang.win_seterrno(i32 13) ; EACCES (missing dev mode most often)
  ret i32 -1
}

; link(2): CreateHardLinkA(newpath, existingpath, NULL).
define i32 @nolang.win_link(i8* %existing, i8* %newpath) {
entry:
  %r = call i32 @CreateHardLinkA(i8* %newpath, i8* %existing, i8* null)
  %ok = icmp ne i32 %r, 0
  br i1 %ok, label %succ, label %fail
succ:
  ret i32 0
fail:
  call void @nolang.win_seterrno(i32 13) ; EACCES (cross-volume most often)
  ret i32 -1
}

; truncate(2): _truncate takes a 32-bit long on LLP64; larger lengths fail
; with EFBIG(27) instead of silently wrapping.
define i32 @nolang.win_truncate(i8* %path, i64 %len) {
entry:
  %big = icmp sgt i64 %len, 2147483647
  br i1 %big, label %toobig, label %doit
toobig:
  call void @nolang.win_seterrno(i32 27) ; EFBIG
  ret i32 -1
doit:
  %l32 = trunc i64 %len to i32
  %r = call i32 @_truncate(i8* %path, i32 %l32)
  ret i32 %r
}

; setpriority(2): map nice values onto the nearest priority class. who != 0
; opens that pid (PRIO_PROCESS); PRIO_PGRP/PRIO_USER fall back to self.
; Classes: IDLE 1, BELOW_NORMAL 16384, NORMAL 32, ABOVE_NORMAL 32768, HIGH 64.
define i32 @nolang.win_setpriority(i32 %which, i32 %who, i32 %prio) {
entry:
  %isself = icmp eq i32 %who, 0
  br i1 %isself, label %self, label %other
self:
  %h0 = call i64 @GetCurrentProcess()
  br label %have
other:
  %h1 = call i64 @OpenProcess(i32 512, i32 0, i32 %who) ; PROCESS_SET_INFORMATION
  %bad = icmp eq i64 %h1, 0
  br i1 %bad, label %fail, label %have
have:
  %h = phi i64 [ %h0, %self ], [ %h1, %other ]
  %opened = phi i1 [ false, %self ], [ true, %other ]
  %lt10neg = icmp slt i32 %prio, -10
  %neg = icmp slt i32 %prio, 0
  %zero = icmp eq i32 %prio, 0
  %le10 = icmp sle i32 %prio, 10
  %c1 = select i1 %le10, i32 16384, i32 1
  %c2 = select i1 %zero, i32 32, i32 %c1
  %c3 = select i1 %neg, i32 32768, i32 %c2
  %cls = select i1 %lt10neg, i32 64, i32 %c3
  %r = call i32 @SetPriorityClass(i64 %h, i32 %cls)
  %ok = icmp ne i32 %r, 0
  br i1 %opened, label %close, label %test
close:
  call i32 @CloseHandle(i64 %h)
  br label %test
test:
  br i1 %ok, label %succ, label %fail
succ:
  ret i32 0
fail:
  call void @nolang.win_seterrno(i32 3) ; ESRCH
  ret i32 -1
}

; getpriority(2) counterpart: class -> nearest nice value. The caller clears
; errno before the call and treats errno != 0 as failure, so a failing
; GetPriorityClass (returns 0) must set it.
define i32 @nolang.win_getpriority(i32 %which, i32 %who) {
entry:
  %isself = icmp eq i32 %who, 0
  br i1 %isself, label %self, label %other
self:
  %h0 = call i64 @GetCurrentProcess()
  br label %have
other:
  %h1 = call i64 @OpenProcess(i32 512, i32 0, i32 %who) ; PROCESS_QUERY_LIMITED? see below
  %bad = icmp eq i64 %h1, 0
  br i1 %bad, label %fail, label %have
have:
  %h = phi i64 [ %h0, %self ], [ %h1, %other ]
  %opened = phi i1 [ false, %self ], [ true, %other ]
  %c = call i32 @GetPriorityClass(i64 %h)
  %iszero = icmp eq i32 %c, 0
  br i1 %opened, label %close, label %post
close:
  call i32 @CloseHandle(i64 %h)
  br label %post
post:
  br i1 %iszero, label %fail, label %map
map:
  %e1 = icmp eq i32 %c, 1
  %e2 = icmp eq i32 %c, 16384
  %e3 = icmp eq i32 %c, 32
  %e4 = icmp eq i32 %c, 32768
  %e5 = icmp eq i32 %c, 64
  %e6 = icmp eq i32 %c, 256
  %s6 = select i1 %e6, i32 -20, i32 0
  %s5 = select i1 %e5, i32 -15, i32 %s6
  %s4 = select i1 %e4, i32 -5, i32 %s5
  %s3 = select i1 %e3, i32 0, i32 %s4
  %s2 = select i1 %e2, i32 5, i32 %s3
  %s1 = select i1 %e1, i32 19, i32 %s2
  ret i32 %s1
fail:
  call void @nolang.win_seterrno(i32 3) ; ESRCH
  ret i32 -1
}

; flock(2) over byte-range locks: LOCK_EX(2)→exclusive, LOCK_SH(1) is granted
; exclusively too (Windows byte locks do not track flock's shared semantics),
; LOCK_UN(8) unlocks, LOCK_NB(4) maps to LOCKFILE_FAIL_IMMEDIATELY.
; OVERLAPPED is zero-initialised on the stack (32 bytes on Win64).
define i32 @nolang.win_flock(i32 %fd, i32 %op) {
entry:
  %h = call i64 @_get_osfhandle(i32 %fd)
  %inv = icmp eq i64 %h, -1
  br i1 %inv, label %bad, label %work
bad:
  call void @nolang.win_seterrno(i32 9) ; EBADF
  ret i32 -1
work:
  %ovb = alloca [32 x i8], align 8
  call void @llvm.memset.p0i8.i64(i8* %ovb, i8 0, i64 32, i1 false)
  %un = and i32 %op, 8
  %isun = icmp ne i32 %un, 0
  br i1 %isun, label %unlock, label %lock
unlock:
  %ru = call i32 @UnlockFileEx(i64 %h, i32 0, i32 -1, i32 -1, i8* %ovb)
  %oku = icmp ne i32 %ru, 0
  br i1 %oku, label %succ, label %fail
lock:
  %ex = and i32 %op, 2
  %isex = icmp ne i32 %ex, 0
  %nb = and i32 %op, 4
  %isnb = icmp ne i32 %nb, 0
  %f0 = select i1 %isex, i32 2, i32 0 ; LOCKFILE_EXCLUSIVE_LOCK
  %f1 = select i1 %isnb, i32 1, i32 %f0 ; LOCKFILE_FAIL_IMMEDIATELY
  %rl = call i32 @LockFileEx(i64 %h, i32 %f1, i32 0, i32 -1, i32 -1, i8* %ovb)
  %okl = icmp ne i32 %rl, 0
  br i1 %okl, label %succ, label %fail
fail:
  call void @nolang.win_seterrno(i32 11) ; EAGAIN
  ret i32 -1
succ:
  ret i32 0
}

; ttyname(3): _isatty + the conventional "con:" device name.
define i8* @nolang.win_ttyname(i32 %fd) {
entry:
  %t = call i32 @_isatty(i32 %fd)
  %ok = icmp ne i32 %t, 0
  %p = select i1 %ok, i8* getelementptr inbounds ([5 x i8], [5 x i8]* @.win.con, i64 0, i64 0), i8* null
  ret i8* %p
}

; gethostid(3): FNV-1a over the COMPUTERNAME environment value.
define i32 @nolang.win_gethostid() {
entry:
  %vbuf = alloca [256 x i8], align 8
  call void @llvm.memset.p0i8.i64(i8* %vbuf, i8 0, i64 256, i1 false)
  %cn = getelementptr inbounds [13 x i8], [13 x i8]* @.win.cn, i64 0, i64 0
  %n = call i32 @GetEnvironmentVariableA(i8* %cn, i8* %vbuf, i32 256)
  %has = icmp sgt i32 %n, 0
  br i1 %has, label %hash, label %none
none:
  ret i32 0
hash:
  %clip0 = icmp slt i32 %n, 255
  %nclamped = select i1 %clip0, i32 %n, i32 254
  %n64 = zext i32 %nclamped to i64
  br label %loop
loop:
  %i = phi i64 [ 0, %hash ], [ %inext, %body ]
  %h = phi i32 [ 2166136261, %hash ], [ %hn, %body ]
  %more = icmp ult i64 %i, %n64
  br i1 %more, label %body, label %done
body:
  %cp = getelementptr i8, i8* %vbuf, i64 %i
  %cv = load i8, i8* %cp
  %c32 = zext i8 %cv to i32
  %hx = xor i32 %h, %c32
  %hn = mul i32 %hx, 16777619
  %inext = add i64 %i, 1
  br label %loop
done:
  ret i32 %h
}

; uname(2): five 256-byte fields (matching emitBuiltinUname's fieldLen for
; non-linux targets), in POSIX order sysname/nodename/release/version/machine
; at offsets 0/256/512/768/1024.
define i32 @nolang.win_uname(i8* %buf) {
entry:
  call void @llvm.memset.p0i8.i64(i8* %buf, i8 0, i64 1280, i1 false)
  %sys = getelementptr inbounds [8 x i8], [8 x i8]* @.win.u.sys, i64 0, i64 0
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %buf, i8* %sys, i64 8, i1 false)
  %g512 = getelementptr i8, i8* %buf, i64 512
  %rel = getelementptr inbounds [5 x i8], [5 x i8]* @.win.u.rel, i64 0, i64 0
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %g512, i8* %rel, i64 5, i1 false)
  %g768 = getelementptr i8, i8* %buf, i64 768
  %ver = getelementptr inbounds [7 x i8], [7 x i8]* @.win.u.ver, i64 0, i64 0
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %g768, i8* %ver, i64 7, i1 false)
  %g1024 = getelementptr i8, i8* %buf, i64 1024
  %mach = getelementptr inbounds @MACHTY@, @MACHTY@* @.win.u.machine, i64 0, i64 0
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %g1024, i8* %mach, i64 @MACHN@, i1 false)
  ; nodename: COMPUTERNAME (max 15 chars); the copy is clamped to 254 so the
  ; field always keeps a NUL from the memset above.
  %nb = alloca [256 x i8], align 8
  call void @llvm.memset.p0i8.i64(i8* %nb, i8 0, i64 256, i1 false)
  %cn = getelementptr inbounds [13 x i8], [13 x i8]* @.win.cn, i64 0, i64 0
  %n = call i32 @GetEnvironmentVariableA(i8* %cn, i8* %nb, i32 256)
  %has = icmp sgt i32 %n, 0
  %clip0 = icmp slt i32 %n, 255
  %nsafe = select i1 %clip0, i32 %n, i32 254
  %n64 = zext i32 %nsafe to i64
  %ncopy = select i1 %has, i64 %n64, i64 0
  %g256 = getelementptr i8, i8* %buf, i64 256
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %g256, i8* %nb, i64 %ncopy, i1 false)
  ret i32 0
}
`
