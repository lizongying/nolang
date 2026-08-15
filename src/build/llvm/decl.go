package llvm

import (
	"runtime"
	"strings"
)

func (g *Generator) writeDeclarations(sb *strings.Builder) {
	// 使用編譯目標平台（g.targetGoos）而非宿主平台（runtime.GOOS）。
	// 未設定目標時回退到宿主平台，保持向後相容。
	goos := g.targetGoos
	if goos == "" {
		goos = runtime.GOOS
	}
	sb.WriteString("declare double @llvm.fabs.f64(double)\n")
	sb.WriteString("declare double @llvm.sqrt.f64(double)\n")
	sb.WriteString("declare double @llvm.sin.f64(double)\n")
	sb.WriteString("declare double @llvm.cos.f64(double)\n")
	sb.WriteString("declare double @llvm.pow.f64(double, double)\n")
	sb.WriteString("declare double @llvm.ceil.f64(double)\n")
	sb.WriteString("declare double @llvm.floor.f64(double)\n")
	sb.WriteString("declare double @llvm.round.f64(double)\n")
	sb.WriteString("declare double @llvm.trunc.f64(double)\n")
	sb.WriteString("declare double @llvm.exp.f64(double)\n")
	sb.WriteString("declare double @llvm.log.f64(double)\n")
	sb.WriteString("declare double @llvm.log10.f64(double)\n")
	sb.WriteString("declare double @llvm.log2.f64(double)\n")
	sb.WriteString("declare double @llvm.atan.f64(double)\n")
	sb.WriteString("declare double @llvm.atan2.f64(double, double)\n")
	sb.WriteString("declare double @llvm.maxnum.f64(double, double)\n")
	sb.WriteString("declare double @llvm.minnum.f64(double, double)\n")
	sb.WriteString("declare double @llvm.asin.f64(double)\n")
	sb.WriteString("declare double @llvm.acos.f64(double)\n")
	sb.WriteString("declare double @llvm.sinh.f64(double)\n")
	sb.WriteString("declare double @llvm.cosh.f64(double)\n")
	sb.WriteString("declare double @llvm.tanh.f64(double)\n")
	// Funnel shift intrinsics for rotate_left/rotate_right.
	// llvm.fshl rotates LEFT, llvm.fshr rotates RIGHT.
	// On ARM64, LLVM backend lowers these to the ROR instruction.
	sb.WriteString("declare i32 @llvm.fshl.i32(i32, i32, i32)\n")
	sb.WriteString("declare i64 @llvm.fshl.i64(i64, i64, i64)\n")
	// strtod/strtoll replaced by internal @nolang.strtod/@nolang.strtoll
	// (FFI ffi-cstr-at-float/ffi-cstr-at-int use these internal implementations).
	// malloc 參數型別依平台而異：
	// - WASI/wasm32: size_t 為 i32，wasi-libc 的 malloc 宣告為 i32*
	//   直接以 i64 呼叫會造成 LLVM 型別不匹配（IR verifier 失敗）。
	//   因此為 WASI 目標宣告 @malloc(i32)，並提供 @nolang.malloc(i64)
	//   內部 wrapper 做截斷（trunc），所有呼叫端仍用 i64 引數。
	// - 其他平台：沿用 i64 宣告（既有行為）。
	if goos == "wasi" {
		sb.WriteString("declare i8* @malloc(i32)\n")
	} else {
		sb.WriteString("declare i8* @malloc(i64)\n")
	}
	sb.WriteString("declare void @free(i8*)\n")
	sb.WriteString("declare void @llvm.memcpy.p0i8.p0i8.i64(i8* nocapture writeonly, i8* nocapture readonly, i64, i1 immarg)\n")
	sb.WriteString("declare void @llvm.memset.p0i8.i64(i8* nocapture writeonly, i8, i64, i1 immarg)\n")
	sb.WriteString("declare ptr @llvm.stacksave.p0()\n")
	sb.WriteString("declare void @llvm.stackrestore.p0(ptr)\n")
	sb.WriteString("declare i8* @getenv(i8*)\n")
	sb.WriteString("declare i32 @setenv(i8*, i8*, i32)\n")
if goos == "windows" {
		sb.WriteString("declare i8* @_getcwd(i8*, i64)\n")
		sb.WriteString("declare i32 @_chdir(i8*)\n")
	} else if goos == "wasi" {
		// WASI: wasi-libc 提供 getcwd（包裝 path_get）。
		// chdir 在 wasi-libc 中並非原生支援，但仍宣告為 external，
		// 讓 IR parser 接受 call site；若用戶實際呼叫 ch-dir，
		// 連結階段會產生 "undefined symbol: chdir" 錯誤（spec 行為）。
		// 用戶應使用 #{wasi-wasm32} 標註提供替代方案。
		sb.WriteString("declare i8* @getcwd(i8*, i64)\n")
		sb.WriteString("declare i32 @chdir(i8*)\n")
	} else {
		sb.WriteString("declare i8* @getcwd(i8*, i64)\n")
		sb.WriteString("declare i32 @chdir(i8*)\n")
	}
	sb.WriteString("declare void @exit(i32)\n")
	sb.WriteString("declare i32 @getpid()\n")
	sb.WriteString("declare i32 @gethostname(i8*, i64)\n")
	if goos == "windows" {
		sb.WriteString("declare i32 @_mkdir(i8*, i32)\n")
		sb.WriteString("declare i32 @_chmod(i8*, i32)\n")
		sb.WriteString("declare i32 @_rmdir(i8*)\n")
		// chown/getuid/getgid/getppid/kill/utimensat not declared on Windows:
		// routed to nolang.win_* stubs (see writeWindowsStubs).
		sb.WriteString("declare i32 @_unlink(i8*)\n")
		sb.WriteString("declare i32 @_stat64(i8*, i8*)\n")
	} else if goos == "wasi" {
		// WASI: single-user — skip chown/getuid/getgid (no concept of users).
		sb.WriteString("declare i32 @mkdir(i8*, i32)\n")
		sb.WriteString("declare i32 @chmod(i8*, i32)\n")
		sb.WriteString("declare i32 @unlink(i8*)\n")
		sb.WriteString("declare i32 @stat(i8*, i8*)\n")
	} else {
		sb.WriteString("declare i32 @mkdir(i8*, i32)\n")
		sb.WriteString("declare i32 @chmod(i8*, i32)\n")
		sb.WriteString("declare i32 @chown(i8*, i32, i32)\n")
		sb.WriteString("declare i32 @getuid()\n")
		sb.WriteString("declare i32 @getgid()\n")
		sb.WriteString("declare i32 @unlink(i8*)\n")
		sb.WriteString("declare i32 @stat(i8*, i8*)\n")
	}
	sb.WriteString("declare i32 @rename(i8*, i8*)\n")
	if goos == "windows" {
		// symlink/link not declared on Windows: routed to nolang.win_* stubs.
	} else if goos == "wasi" {
		// WASI: symlink/link have limited support and may fail — skip declarations.
		// User code should use #{wasi-wasm32} annotations to provide alternatives.
	} else {
		sb.WriteString("declare i32 @symlink(i8*, i8*)\n")
		sb.WriteString("declare i32 @link(i8*, i8*)\n")
	}
	// Directory operations: POSIX uses <dirent.h>, Windows uses FindFirstFileA/FindNextFileA/FindClose.
	// WIN32_FIND_DATAA layout (320 bytes):
	//   dwFileAttributes@0(i32,4) ftCreationTime@4(8) ftLastAccessTime@12(8)
	//   ftLastWriteTime@20(8) nFileSizeHigh@28(4) nFileSizeLow@32(4)
	//   dwReserved0@36(4) dwReserved1@40(4) cFileName@44(char[260])
	//   cAlternateFileName@304(char[14]) — total 320 bytes.
	if goos == "windows" {
		sb.WriteString("declare i8* @FindFirstFileA(i8*, i8*)\n")
		sb.WriteString("declare i32 @FindNextFileA(i8*, i8*)\n")
		sb.WriteString("declare i32 @FindClose(i8*)\n")
		// Win32 process/pipe API — used by win-create-process, win-create-pipe,
		// win-read-pipe, win-write-pipe, win-wait-process, win-get-exit-code,
		// win-terminate-process, win-close-handle ForwardFunc builtins.
		// These replace the C runtime (process.c) on Windows with pure Nolang.
		sb.WriteString("declare i32 @CreateProcessA(i8*, i8*, i8*, i8*, i32, i32, i8*, i8*, i8*, i8*)\n")
		sb.WriteString("declare i32 @CreatePipe(i8*, i8*, i8*, i32)\n")
		sb.WriteString("declare i32 @ReadFile(i8*, i8*, i32, i8*, i8*)\n")
		sb.WriteString("declare i32 @WriteFile(i8*, i8*, i32, i8*, i8*)\n")
		sb.WriteString("declare i32 @WaitForSingleObject(i8*, i32)\n")
		sb.WriteString("declare i32 @GetExitCodeProcess(i8*, i32*)\n")
		sb.WriteString("declare i32 @TerminateProcess(i8*, i32)\n")
		sb.WriteString("declare i32 @CloseHandle(i8*)\n")
		sb.WriteString("declare i8* @GetStdHandle(i32)\n")
		// STARTUPINFOA (68 bytes): cb@0, lpReserved@4, lpDesktop@8, lpTitle@12,
		//   dwX@16, dwY@20, dwXSize@24, dwYSize@28, dwXCountChars@32,
		//   dwYCountChars@36, dwFillAttribute@40, dwFlags@44, wShowWindow@48,
		//   cbReserved2@52, lpReserved2@56, hStdInput@60, hStdOutput@64, hStdError@68.
		// PROCESS_INFORMATION (24 bytes): hProcess@0, hThread@8, dwProcessId@16, dwThreadId@20.
	} else {
		sb.WriteString("declare i8* @opendir(i8*)\n")
		sb.WriteString("declare i8* @readdir(i8*)\n")
		sb.WriteString("declare i32 @closedir(i8*)\n")
	}
	// File timestamp update (from <sys/stat.h>)
	if goos != "windows" && goos != "wasi" {
		// Windows: routed to @nolang.win_utimensat stub
		// WASI: utimensat has limited support — skip declaration.
		sb.WriteString("declare i32 @utimensat(i32, i8*, i8*, i32)\n")
	}
	// notools Unix 工具集擴展：新增 POSIX 系統調用宣告
	// Windows 平台對應的函式透過 nolang.win_* stub 提供（見 writeWindowsStubs）；
	// WASI 平台不支援這些 POSIX 函式，跳過宣告（用戶需用 #{wasi-wasm32} 提供替代）。
	if goos != "windows" && goos != "wasi" {
		sb.WriteString("declare i8* @realpath(i8*, i8*)\n")
		sb.WriteString("declare i64 @readlink(i8*, i8*, i64)\n")
		sb.WriteString("declare i32 @mkstemp(i8*)\n")
		sb.WriteString("declare i8* @mkdtemp(i8*)\n")
		sb.WriteString("declare i32 @mkfifo(i8*, i16)\n")
		sb.WriteString("declare i32 @utimes(i8*, i8*)\n")
		sb.WriteString("declare i32 @geteuid()\n")
		sb.WriteString("declare i32 @getegid()\n")
		sb.WriteString("declare i32 @getgroups(i32, i32*)\n")
		sb.WriteString("declare i32 @uname(i8*)\n")
		sb.WriteString("declare i64 @sysconf(i32)\n")
		sb.WriteString("declare i8* @ttyname(i32)\n")
		sb.WriteString("declare i64 @signal(i32, i64)\n")
		// notools Unix 工具集擴展：fs / os 底層函數
		sb.WriteString("declare i32 @rmdir(i8*)\n")
		sb.WriteString("declare i32 @mknod(i8*, i32, i64)\n")
		sb.WriteString("declare i32 @truncate(i8*, i64)\n")
		sb.WriteString("declare void @sync()\n")
		sb.WriteString("declare i32 @lstat(i8*, i8*)\n")
		sb.WriteString("declare i8* @getlogin()\n")
		sb.WriteString("declare i32 @gethostid()\n")
		sb.WriteString("declare i32 @setpriority(i32, i32, i32)\n")
		sb.WriteString("declare i32 @getpriority(i32, i32)\n")
		sb.WriteString("declare i32 @setsid()\n")
		sb.WriteString("declare i32 @flock(i32, i32)\n")
		sb.WriteString("declare i32 @getdomainname(i8*, i32)\n")
		sb.WriteString("declare void @syslog(i32, i8*, ...)\n")
		// chroot: change root directory (POSIX chroot(2))
		sb.WriteString("declare i32 @chroot(i8*)\n")
		// macOS/BSD sysctlbyname (Linux uses /proc/sys instead)
		if goos == "darwin" {
			sb.WriteString("declare i32 @sysctlbyname(i8*, i8*, i64*, i8*, i64)\n")
		}
	}
	// libc @time 已移除：now 內建改用內部 @nolang.now_s（gettimeofday）
	// libc @sleep 已移除：sleep 內建改用內部 @nolang.sleep_s（nanosleep）
	if goos == "windows" {
		sb.WriteString("declare i32 @_open(i8*, i32, ...)\n")
	} else {
		sb.WriteString("declare i32 @open(i8*, i32, ...)\n")
	}
	sb.WriteString("declare i32 @gettimeofday(i8*, i8*)\n")
	sb.WriteString("declare i32 @usleep(i32)\n")
	sb.WriteString("declare i32 @nanosleep(i8*, i8*)\n")
	sb.WriteString("declare i32 @clock_gettime(i32, i8*)\n")
	// errno access: macOS uses __error(), Linux uses __errno_location(),
	// Windows (MinGW-w64) uses _errno().
	if goos == "darwin" {
		sb.WriteString("declare i32* @__error()\n")
	} else if goos == "windows" {
		sb.WriteString("declare i32* @_errno()\n")
	} else {
		sb.WriteString("declare i32* @__errno_location()\n")
	}
	if goos == "windows" {
		sb.WriteString("declare i32 @_dup2(i32, i32)\n")
		// kill/getppid not declared on Windows: routed to nolang.win_* stubs.
		// fork not declared on Windows: POSIX-only (process module needs win path).
		sb.WriteString("declare i32 @_execlp(i8*, i8*, ...)\n")
		sb.WriteString("declare i32 @_pipe(i32*)\n")
		// _cwait actual signature: int _cwait(int* termstat, int procid, int action).
		// Parameter order differs from POSIX waitpid(pid, status*, options);
		// process-waitpid routes to @nolang.win_waitpid wrapper (see below).
		sb.WriteString("declare i32 @_cwait(i32*, i32, i32)\n")
	} else if goos == "wasi" {
		// WASI 不支援 POSIX 進程控制（fork/execlp/pipe/waitpid/dup2/kill/getppid）。
		// 但仍宣告為 external，讓 IR parser 接受 std 函式庫中的 call site
		// （如 process.start 內部呼叫 fork/dup2/execlp）。
		// 若用戶實際呼叫這些函式，連結階段會產生 "undefined symbol" 錯誤（spec 行為）。
		// 用戶應使用 #{wasi-wasm32} 標註提供替代方案。
		sb.WriteString("declare i32 @dup2(i32, i32)\n")
		sb.WriteString("declare i32 @kill(i32, i32)\n")
		sb.WriteString("declare i32 @getppid()\n")
		sb.WriteString("declare i32 @execlp(i8*, i8*, ...)\n")
		sb.WriteString("declare i32 @fork()\n")
		sb.WriteString("declare i32 @pipe(i32*)\n")
		sb.WriteString("declare i32 @waitpid(i32, i32*, i32)\n")
	} else {
		sb.WriteString("declare i32 @dup2(i32, i32)\n")
		sb.WriteString("declare i32 @kill(i32, i32)\n")
		sb.WriteString("declare i32 @getppid()\n")
		sb.WriteString("declare i32 @execlp(i8*, i8*, ...)\n")
		sb.WriteString("declare i32 @fork()\n")
		sb.WriteString("declare i32 @pipe(i32*)\n")
		sb.WriteString("declare i32 @waitpid(i32, i32*, i32)\n")
	}
	sb.WriteString("declare i32 @system(i8*)\n")
	// zlib @compress2 / @uncompress 已移除：archive/gzip.no 以純 Nolang 實現
	// gzip-compress / gzip-decompress，不再使用 zlib 高階 API。

	// sincos: provide an implementation for platforms where libm does not
	// export sincos (e.g. macOS). llc's DAG combiner merges sin(x)+cos(x)
	// with the same argument into a single sincos call; without this
	// definition the linker fails with "undefined symbol: _sincos".
	// Marked optnone+noinline to prevent llc from re-combining sin/cos
	// inside this very function into another sincos call (infinite recursion).
	sb.WriteString("define void @sincos(double %x, double* %sin_out, double* %cos_out) #0 {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%s = call double @llvm.sin.f64(double %x)\n")
	sb.WriteString("\t%c = call double @llvm.cos.f64(double %x)\n")
	sb.WriteString("\tstore double %s, double* %sin_out\n")
	sb.WriteString("\tstore double %c, double* %cos_out\n")
	sb.WriteString("\tret void\n")
	sb.WriteString("}\n")
	sb.WriteString("attributes #0 = { optnone noinline }\n\n")

	// nolang.malloc: 平台感知的 malloc wrapper。
	// 所有 Nolang 內部產生的 malloc 呼叫都在生成時直接 emit @nolang.malloc(i64 ...)，
	// wrapper 內部依目標平台決定是否需要 trunc 至 i32：
	// - WASI/wasm32: wasi-libc malloc 簽名為 (i32)，需 trunc i64 → i32
	// - 其他平台: libc malloc 簽名為 (i64)，直接轉發
	// 此設計讓呼叫端維持單一 i64 介面，避免 45+ 處呼叫端個別處理平台差異。
	// wrapper 內部直接呼叫真實 libc @malloc：WASI 為 @malloc(i32 %sz32)，
	// 非 WASI 為 @malloc(i64 %sz)；因呼叫端在生成時已使用 @nolang.malloc，
	// 無需後處理替換，wrapper 內部的 @malloc 呼叫不會被誤改。
	if goos == "wasi" {
		sb.WriteString("define internal i8* @nolang.malloc(i64 %sz) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\t%sz32 = trunc i64 %sz to i32\n")
		sb.WriteString("\t%p = call i8* @malloc(i32 %sz32)\n")
		sb.WriteString("\tret i8* %p\n")
		sb.WriteString("}\n\n")
	} else {
		sb.WriteString("define internal i8* @nolang.malloc(i64 %sz) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\t%p = call i8* @malloc(i64 %sz)\n")
		sb.WriteString("\tret i8* %p\n")
		sb.WriteString("}\n\n")
	}

	// nolang.read / nolang.write: WASI 專屬的 read/write wrapper。
	// 呼叫端在生成時透過 g.readSymbol()/g.writeSymbol() 決定符號：
	// - WASI: emit @nolang.read/@nolang.write（wrapper 內部 trunc i64→i32 並 sext i32→i64，
	//   匹配 wasi-libc 的 (i32, i8*, i32) -> i32 簽名）
	// - 其他平台: emit @read/@write（直接使用 libc 的 i64 介面，無需 wrapper）
	// Windows 使用 @_read/@_write（名稱不同），不受影響。
	if goos == "wasi" {
		sb.WriteString("define internal i64 @nolang.read(i32 %fd, i8* %buf, i64 %count) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\t%c32 = trunc i64 %count to i32\n")
		sb.WriteString("\t%r = call i32 @read(i32 %fd, i8* %buf, i32 %c32)\n")
		sb.WriteString("\t%r64 = sext i32 %r to i64\n")
		sb.WriteString("\tret i64 %r64\n")
		sb.WriteString("}\n\n")
		sb.WriteString("define internal i64 @nolang.write(i32 %fd, i8* %buf, i64 %count) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\t%c32 = trunc i64 %count to i32\n")
		sb.WriteString("\t%r = call i32 @write(i32 %fd, i8* %buf, i32 %c32)\n")
		sb.WriteString("\t%r64 = sext i32 %r to i64\n")
		sb.WriteString("\tret i64 %r64\n")
		sb.WriteString("}\n\n")
	}

	// nolang.strlen: runtime C-string length (replaces libc @strlen).
	// Loops over i8* until null terminator. Used when converting C strings
	// from libc functions (getenv, readdir, fgets, etc.) to %str-long.
	// Handles NULL input by returning 0 (avoids UB from libc strlen(NULL)).
	sb.WriteString("define internal i64 @nolang.strlen(i8* %s) {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%null.cond = icmp eq i8* %s, null\n")
	sb.WriteString("\tbr i1 %null.cond, label %done, label %loop\n")
	sb.WriteString("loop:\n")
	sb.WriteString("\t%i = phi i64 [0, %entry], [%i.next, %loop]\n")
	sb.WriteString("\t%ptr = getelementptr i8, i8* %s, i64 %i\n")
	sb.WriteString("\t%c = load i8, i8* %ptr\n")
	sb.WriteString("\t%cond = icmp eq i8 %c, 0\n")
	sb.WriteString("\t%i.next = add i64 %i, 1\n")
	sb.WriteString("\tbr i1 %cond, label %done, label %loop\n")
	sb.WriteString("done:\n")
	sb.WriteString("\t%len = phi i64 [0, %entry], [%i, %loop]\n")
	sb.WriteString("\tret i64 %len\n")
	sb.WriteString("}\n\n")

	// nolang.strtoll: parse C string as base-10 i64 (replaces libc @strtoll).
	// Used by FFI ffi-cstr-at-int to parse C string arrays.
	// Skips leading whitespace, handles optional sign, reads decimal digits.
	sb.WriteString("define internal i64 @nolang.strtoll(i8* %s) {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%ptr.addr = alloca i8*\n")
	sb.WriteString("\tstore i8* %s, i8** %ptr.addr\n")
	sb.WriteString("\t%neg.flag = alloca i1\n")
	sb.WriteString("\tstore i1 false, i1* %neg.flag\n")
	sb.WriteString("\t%acc = alloca i64\n")
	sb.WriteString("\tstore i64 0, i64* %acc\n")
	sb.WriteString("\tbr label %skip.ws\n")
	// skip whitespace
	sb.WriteString("skip.ws:\n")
	sb.WriteString("\t%p = load i8*, i8** %ptr.addr\n")
	sb.WriteString("\t%c = load i8, i8* %p\n")
	sb.WriteString("\t%is.sp = icmp eq i8 %c, 32\n")
	sb.WriteString("\t%is.ht = icmp eq i8 %c, 9\n")
	sb.WriteString("\t%is.lf = icmp eq i8 %c, 10\n")
	sb.WriteString("\t%is.cr = icmp eq i8 %c, 13\n")
	sb.WriteString("\t%a1 = or i1 %is.sp, %is.ht\n")
	sb.WriteString("\t%a2 = or i1 %a1, %is.lf\n")
	sb.WriteString("\t%a3 = or i1 %a2, %is.cr\n")
	sb.WriteString("\tbr i1 %a3, label %ws.next, label %check.sign\n")
	sb.WriteString("ws.next:\n")
	sb.WriteString("\t%p.next = getelementptr i8, i8* %p, i64 1\n")
	sb.WriteString("\tstore i8* %p.next, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %skip.ws\n")
	// check sign
	sb.WriteString("check.sign:\n")
	sb.WriteString("\t%is.minus = icmp eq i8 %c, 45\n")
	sb.WriteString("\tbr i1 %is.minus, label %set.neg, label %check.plus\n")
	sb.WriteString("set.neg:\n")
	sb.WriteString("\tstore i1 true, i1* %neg.flag\n")
	sb.WriteString("\t%p.am = getelementptr i8, i8* %p, i64 1\n")
	sb.WriteString("\tstore i8* %p.am, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %digit.loop\n")
	sb.WriteString("check.plus:\n")
	sb.WriteString("\t%is.plus = icmp eq i8 %c, 43\n")
	sb.WriteString("\tbr i1 %is.plus, label %skip.plus, label %digit.loop\n")
	sb.WriteString("skip.plus:\n")
	sb.WriteString("\t%p.ap = getelementptr i8, i8* %p, i64 1\n")
	sb.WriteString("\tstore i8* %p.ap, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %digit.loop\n")
	// digit loop
	sb.WriteString("digit.loop:\n")
	sb.WriteString("\t%p2 = load i8*, i8** %ptr.addr\n")
	sb.WriteString("\t%c2 = load i8, i8* %p2\n")
	sb.WriteString("\t%ge.0 = icmp sge i8 %c2, 48\n")
	sb.WriteString("\t%le.9 = icmp sle i8 %c2, 57\n")
	sb.WriteString("\t%is.dig = and i1 %ge.0, %le.9\n")
	sb.WriteString("\tbr i1 %is.dig, label %dig.proc, label %done\n")
	sb.WriteString("dig.proc:\n")
	sb.WriteString("\t%cur = load i64, i64* %acc\n")
	sb.WriteString("\t%mul10 = mul i64 %cur, 10\n")
	sb.WriteString("\t%dv = sext i8 %c2 to i64\n")
	sb.WriteString("\t%dv.sub = sub i64 %dv, 48\n")
	sb.WriteString("\t%new.acc = add i64 %mul10, %dv.sub\n")
	sb.WriteString("\tstore i64 %new.acc, i64* %acc\n")
	sb.WriteString("\t%p3 = getelementptr i8, i8* %p2, i64 1\n")
	sb.WriteString("\tstore i8* %p3, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %digit.loop\n")
	sb.WriteString("done:\n")
	sb.WriteString("\t%final = load i64, i64* %acc\n")
	sb.WriteString("\t%is.neg = load i1, i1* %neg.flag\n")
	sb.WriteString("\tbr i1 %is.neg, label %negate, label %ret.pos\n")
	sb.WriteString("negate:\n")
	sb.WriteString("\t%negated = sub i64 0, %final\n")
	sb.WriteString("\tret i64 %negated\n")
	sb.WriteString("ret.pos:\n")
	sb.WriteString("\tret i64 %final\n")
	sb.WriteString("}\n\n")

	// nolang.strtod: parse C string as f64 (replaces libc @strtod).
	// Used by FFI ffi-cstr-at-float to parse C string arrays.
	// Handles: optional sign, integer digits, optional fractional part,
	// optional exponent (e/E + sign + digits).
	// Uses @llvm.pow.f64 for exponentiation.
	sb.WriteString("define internal double @nolang.strtod(i8* %s) {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%ptr.addr = alloca i8*\n")
	sb.WriteString("\tstore i8* %s, i8** %ptr.addr\n")
	sb.WriteString("\t%neg.flag = alloca i1\n")
	sb.WriteString("\tstore i1 false, i1* %neg.flag\n")
	sb.WriteString("\t%acc = alloca double\n")
	sb.WriteString("\tstore double 0.0, double* %acc\n")
	sb.WriteString("\t%frac.div = alloca double\n")
	sb.WriteString("\tstore double 1.0, double* %frac.div\n")
	sb.WriteString("\t%has.dot = alloca i1\n")
	sb.WriteString("\tstore i1 false, i1* %has.dot\n")
	sb.WriteString("\tbr label %skip.ws\n")
	// skip whitespace
	sb.WriteString("skip.ws:\n")
	sb.WriteString("\t%p = load i8*, i8** %ptr.addr\n")
	sb.WriteString("\t%c = load i8, i8* %p\n")
	sb.WriteString("\t%is.sp = icmp eq i8 %c, 32\n")
	sb.WriteString("\t%is.ht = icmp eq i8 %c, 9\n")
	sb.WriteString("\t%is.lf = icmp eq i8 %c, 10\n")
	sb.WriteString("\t%is.cr = icmp eq i8 %c, 13\n")
	sb.WriteString("\t%a1 = or i1 %is.sp, %is.ht\n")
	sb.WriteString("\t%a2 = or i1 %a1, %is.lf\n")
	sb.WriteString("\t%a3 = or i1 %a2, %is.cr\n")
	sb.WriteString("\tbr i1 %a3, label %ws.next, label %check.sign\n")
	sb.WriteString("ws.next:\n")
	sb.WriteString("\t%p.next = getelementptr i8, i8* %p, i64 1\n")
	sb.WriteString("\tstore i8* %p.next, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %skip.ws\n")
	// check sign
	sb.WriteString("check.sign:\n")
	sb.WriteString("\t%is.minus = icmp eq i8 %c, 45\n")
	sb.WriteString("\tbr i1 %is.minus, label %set.neg, label %check.plus\n")
	sb.WriteString("set.neg:\n")
	sb.WriteString("\tstore i1 true, i1* %neg.flag\n")
	sb.WriteString("\t%p.am = getelementptr i8, i8* %p, i64 1\n")
	sb.WriteString("\tstore i8* %p.am, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %main.loop\n")
	sb.WriteString("check.plus:\n")
	sb.WriteString("\t%is.plus = icmp eq i8 %c, 43\n")
	sb.WriteString("\tbr i1 %is.plus, label %skip.plus, label %main.loop\n")
	sb.WriteString("skip.plus:\n")
	sb.WriteString("\t%p.ap = getelementptr i8, i8* %p, i64 1\n")
	sb.WriteString("\tstore i8* %p.ap, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %main.loop\n")
	// main loop: digits and dot
	sb.WriteString("main.loop:\n")
	sb.WriteString("\t%p2 = load i8*, i8** %ptr.addr\n")
	sb.WriteString("\t%c2 = load i8, i8* %p2\n")
	sb.WriteString("\t%ge.0 = icmp sge i8 %c2, 48\n")
	sb.WriteString("\t%le.9 = icmp sle i8 %c2, 57\n")
	sb.WriteString("\t%is.dig = and i1 %ge.0, %le.9\n")
	sb.WriteString("\tbr i1 %is.dig, label %dig.proc, label %check.dot\n")
	sb.WriteString("dig.proc:\n")
	sb.WriteString("\t%dv = sext i8 %c2 to i64\n")
	sb.WriteString("\t%dv.sub = sub i64 %dv, 48\n")
	sb.WriteString("\t%dv.f = sitofp i64 %dv.sub to double\n")
	sb.WriteString("\t%in.frac = load i1, i1* %has.dot\n")
	sb.WriteString("\tbr i1 %in.frac, label %frac.dig, label %int.dig\n")
	sb.WriteString("int.dig:\n")
	sb.WriteString("\t%cur.i = load double, double* %acc\n")
	sb.WriteString("\t%mul10 = fmul double %cur.i, 10.0\n")
	sb.WriteString("\t%add.i = fadd double %mul10, %dv.f\n")
	sb.WriteString("\tstore double %add.i, double* %acc\n")
	sb.WriteString("\tbr label %next.char\n")
	sb.WriteString("frac.dig:\n")
	sb.WriteString("\t%cur.f = load double, double* %acc\n")
	sb.WriteString("\t%cur.div = load double, double* %frac.div\n")
	sb.WriteString("\t%new.div = fmul double %cur.div, 10.0\n")
	sb.WriteString("\tstore double %new.div, double* %frac.div\n")
	sb.WriteString("\t%divided = fdiv double %dv.f, %new.div\n")
	sb.WriteString("\t%add.f = fadd double %cur.f, %divided\n")
	sb.WriteString("\tstore double %add.f, double* %acc\n")
	sb.WriteString("\tbr label %next.char\n")
	sb.WriteString("next.char:\n")
	sb.WriteString("\t%p3 = getelementptr i8, i8* %p2, i64 1\n")
	sb.WriteString("\tstore i8* %p3, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %main.loop\n")
	// check for decimal point
	sb.WriteString("check.dot:\n")
	sb.WriteString("\t%is.dot = icmp eq i8 %c2, 46\n")
	sb.WriteString("\tbr i1 %is.dot, label %set.dot, label %check.exp\n")
	sb.WriteString("set.dot:\n")
	sb.WriteString("\tstore i1 true, i1* %has.dot\n")
	sb.WriteString("\t%p.ad = getelementptr i8, i8* %p2, i64 1\n")
	sb.WriteString("\tstore i8* %p.ad, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %main.loop\n")
	// check for exponent
	sb.WriteString("check.exp:\n")
	sb.WriteString("\t%is.e = icmp eq i8 %c2, 101\n")
	sb.WriteString("\t%is.E = icmp eq i8 %c2, 69\n")
	sb.WriteString("\t%is.exp = or i1 %is.e, %is.E\n")
	sb.WriteString("\tbr i1 %is.exp, label %exp.setup, label %done\n")
	// exponent parsing
	sb.WriteString("exp.setup:\n")
	sb.WriteString("\t%p.ae = getelementptr i8, i8* %p2, i64 1\n")
	sb.WriteString("\tstore i8* %p.ae, i8** %ptr.addr\n")
	sb.WriteString("\t%exp.neg = alloca i1\n")
	sb.WriteString("\tstore i1 false, i1* %exp.neg\n")
	sb.WriteString("\t%exp.acc = alloca i64\n")
	sb.WriteString("\tstore i64 0, i64* %exp.acc\n")
	sb.WriteString("\t%pe0 = load i8*, i8** %ptr.addr\n")
	sb.WriteString("\t%ce0 = load i8, i8* %pe0\n")
	sb.WriteString("\t%exp.is.minus = icmp eq i8 %ce0, 45\n")
	sb.WriteString("\tbr i1 %exp.is.minus, label %exp.set.neg, label %exp.check.plus\n")
	sb.WriteString("exp.set.neg:\n")
	sb.WriteString("\tstore i1 true, i1* %exp.neg\n")
	sb.WriteString("\t%p.aem = getelementptr i8, i8* %pe0, i64 1\n")
	sb.WriteString("\tstore i8* %p.aem, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %exp.dig.loop\n")
	sb.WriteString("exp.check.plus:\n")
	sb.WriteString("\t%exp.is.plus = icmp eq i8 %ce0, 43\n")
	sb.WriteString("\tbr i1 %exp.is.plus, label %exp.skip.plus, label %exp.dig.loop\n")
	sb.WriteString("exp.skip.plus:\n")
	sb.WriteString("\t%p.aep = getelementptr i8, i8* %pe0, i64 1\n")
	sb.WriteString("\tstore i8* %p.aep, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %exp.dig.loop\n")
	sb.WriteString("exp.dig.loop:\n")
	sb.WriteString("\t%pe2 = load i8*, i8** %ptr.addr\n")
	sb.WriteString("\t%ce2 = load i8, i8* %pe2\n")
	sb.WriteString("\t%ege.0 = icmp sge i8 %ce2, 48\n")
	sb.WriteString("\t%ele.9 = icmp sle i8 %ce2, 57\n")
	sb.WriteString("\t%eis.dig = and i1 %ege.0, %ele.9\n")
	sb.WriteString("\tbr i1 %eis.dig, label %exp.dig.proc, label %exp.apply\n")
	sb.WriteString("exp.dig.proc:\n")
	sb.WriteString("\t%ec = load i64, i64* %exp.acc\n")
	sb.WriteString("\t%emul = mul i64 %ec, 10\n")
	sb.WriteString("\t%edv = sext i8 %ce2 to i64\n")
	sb.WriteString("\t%edv.sub = sub i64 %edv, 48\n")
	sb.WriteString("\t%enew = add i64 %emul, %edv.sub\n")
	sb.WriteString("\tstore i64 %enew, i64* %exp.acc\n")
	sb.WriteString("\t%pe3 = getelementptr i8, i8* %pe2, i64 1\n")
	sb.WriteString("\tstore i8* %pe3, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %exp.dig.loop\n")
	// apply exponent: result = acc * 10^exp (or / 10^exp if negative)
	sb.WriteString("exp.apply:\n")
	sb.WriteString("\t%exp.val = load i64, i64* %exp.acc\n")
	sb.WriteString("\t%exp.f = sitofp i64 %exp.val to double\n")
	sb.WriteString("\t%pow10 = call double @llvm.pow.f64(double 10.0, double %exp.f)\n")
	sb.WriteString("\t%exp.is.neg = load i1, i1* %exp.neg\n")
	sb.WriteString("\tbr i1 %exp.is.neg, label %exp.div, label %exp.mul\n")
	sb.WriteString("exp.div:\n")
	sb.WriteString("\t%before.div = load double, double* %acc\n")
	sb.WriteString("\t%after.div = fdiv double %before.div, %pow10\n")
	sb.WriteString("\tstore double %after.div, double* %acc\n")
	sb.WriteString("\tbr label %done\n")
	sb.WriteString("exp.mul:\n")
	sb.WriteString("\t%before.mul = load double, double* %acc\n")
	sb.WriteString("\t%after.mul = fmul double %before.mul, %pow10\n")
	sb.WriteString("\tstore double %after.mul, double* %acc\n")
	sb.WriteString("\tbr label %done\n")
	// done: apply sign and return
	sb.WriteString("done:\n")
	sb.WriteString("\t%final = load double, double* %acc\n")
	sb.WriteString("\t%is.neg = load i1, i1* %neg.flag\n")
	sb.WriteString("\tbr i1 %is.neg, label %negate, label %ret.pos\n")
	sb.WriteString("negate:\n")
	sb.WriteString("\t%negated = fneg double %final\n")
	sb.WriteString("\tret double %negated\n")
	sb.WriteString("ret.pos:\n")
	sb.WriteString("\tret double %final\n")
	sb.WriteString("}\n\n")

	// nolang.now_ms: gettimeofday → sec*1000 + usec/1000
	sb.WriteString("define internal i64 @nolang.now_ms() {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%tv = alloca [16 x i8]\n")
	sb.WriteString("\t%tv.ptr = bitcast [16 x i8]* %tv to i8*\n")
	sb.WriteString("\tcall i32 @gettimeofday(i8* %tv.ptr, i8* null)\n")
	sb.WriteString("\t%sec.ptr = bitcast [16 x i8]* %tv to i64*\n")
	sb.WriteString("\t%sec = load i64, i64* %sec.ptr\n")
	sb.WriteString("\t%usec.ptr = getelementptr i64, i64* %sec.ptr, i64 1\n")
	sb.WriteString("\t%usec = load i64, i64* %usec.ptr\n")
	sb.WriteString("\t%sec.ms = mul i64 %sec, 1000\n")
	sb.WriteString("\t%usec.ms = sdiv i64 %usec, 1000\n")
	sb.WriteString("\t%result = add i64 %sec.ms, %usec.ms\n")
	sb.WriteString("\tret i64 %result\n")
	sb.WriteString("}\n\n")

	// nolang.now_s: gettimeofday → sec（取代 libc @time(NULL)）
	// 使用 gettimeofday 取得 struct timeval { tv_sec, tv_usec }，直接回傳 tv_sec。
	// 與 @time(NULL) 等價，但避免依賴 libc @time。
	sb.WriteString("define internal i64 @nolang.now_s() {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%tv = alloca [16 x i8]\n")
	sb.WriteString("\t%tv.ptr = bitcast [16 x i8]* %tv to i8*\n")
	sb.WriteString("\tcall i32 @gettimeofday(i8* %tv.ptr, i8* null)\n")
	sb.WriteString("\t%sec.ptr = bitcast [16 x i8]* %tv to i64*\n")
	sb.WriteString("\t%sec = load i64, i64* %sec.ptr\n")
	sb.WriteString("\tret i64 %sec\n")
	sb.WriteString("}\n\n")

	// nolang.now_us: gettimeofday → sec*1000000 + usec
	sb.WriteString("define internal i64 @nolang.now_us() {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%tv = alloca [16 x i8]\n")
	sb.WriteString("\t%tv.ptr = bitcast [16 x i8]* %tv to i8*\n")
	sb.WriteString("\tcall i32 @gettimeofday(i8* %tv.ptr, i8* null)\n")
	sb.WriteString("\t%sec.ptr = bitcast [16 x i8]* %tv to i64*\n")
	sb.WriteString("\t%sec = load i64, i64* %sec.ptr\n")
	sb.WriteString("\t%usec.ptr = getelementptr i64, i64* %sec.ptr, i64 1\n")
	sb.WriteString("\t%usec = load i64, i64* %usec.ptr\n")
	sb.WriteString("\t%sec.us = mul i64 %sec, 1000000\n")
	sb.WriteString("\t%result = add i64 %sec.us, %usec\n")
	sb.WriteString("\tret i64 %result\n")
	sb.WriteString("}\n\n")

	// nolang.now_ns: clock_gettime(CLOCK_REALTIME=0) → sec*1000000000 + nsec
	sb.WriteString("define internal i64 @nolang.now_ns() {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%ts = alloca [16 x i8]\n")
	sb.WriteString("\t%ts.ptr = bitcast [16 x i8]* %ts to i8*\n")
	sb.WriteString("\tcall i32 @clock_gettime(i32 0, i8* %ts.ptr)\n")
	sb.WriteString("\t%sec.ptr = bitcast [16 x i8]* %ts to i64*\n")
	sb.WriteString("\t%sec = load i64, i64* %sec.ptr\n")
	sb.WriteString("\t%nsec.ptr = getelementptr i64, i64* %sec.ptr, i64 1\n")
	sb.WriteString("\t%nsec = load i64, i64* %nsec.ptr\n")
	sb.WriteString("\t%sec.ns = mul i64 %sec, 1000000000\n")
	sb.WriteString("\t%result = add i64 %sec.ns, %nsec\n")
	sb.WriteString("\tret i64 %result\n")
	sb.WriteString("}\n\n")

	// nolang.sleep_us: usleep(i32)
	sb.WriteString("define internal void @nolang.sleep_us(i64 %us) {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%us.trunc = trunc i64 %us to i32\n")
	sb.WriteString("\tcall i32 @usleep(i32 %us.trunc)\n")
	sb.WriteString("\tret void\n")
	sb.WriteString("}\n\n")

	// nolang.sleep_s: nanosleep(sec, 0)（取代 libc @sleep(i32)）
	// 使用 nanosleep 系統呼叫，避免依賴 libc @sleep。
	// 與 @sleep(sec) 等價，不關心剩餘時間（被中斷時不重試）。
	// 返回 i64 0（成功）以符合 CLibCall LLVMI64 返回型別；
	// Nolang time.sleep-s 丟棄返回值，固定 0 不影響行為。
	sb.WriteString("define internal i64 @nolang.sleep_s(i64 %sec) {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%req = alloca [16 x i8]\n")
	sb.WriteString("\t%sec.ptr = bitcast [16 x i8]* %req to i64*\n")
	sb.WriteString("\tstore i64 %sec, i64* %sec.ptr\n")
	sb.WriteString("\t%nsec.ptr = getelementptr i64, i64* %sec.ptr, i64 1\n")
	sb.WriteString("\tstore i64 0, i64* %nsec.ptr\n")
	sb.WriteString("\t%req.ptr = bitcast [16 x i8]* %req to i8*\n")
	sb.WriteString("\tcall i32 @nanosleep(i8* %req.ptr, i8* null)\n")
	sb.WriteString("\tret i64 0\n")
	sb.WriteString("}\n\n")

	// nolang.sleep_ns: nanosleep
	sb.WriteString("define internal void @nolang.sleep_ns(i64 %ns) {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%req = alloca [16 x i8]\n")
	sb.WriteString("\t%sec = sdiv i64 %ns, 1000000000\n")
	sb.WriteString("\t%nsec = srem i64 %ns, 1000000000\n")
	sb.WriteString("\t%sec.ptr = bitcast [16 x i8]* %req to i64*\n")
	sb.WriteString("\tstore i64 %sec, i64* %sec.ptr\n")
	sb.WriteString("\t%nsec.ptr = getelementptr i64, i64* %sec.ptr, i64 1\n")
	sb.WriteString("\tstore i64 %nsec, i64* %nsec.ptr\n")
	sb.WriteString("\t%req.ptr = bitcast [16 x i8]* %req to i8*\n")
	sb.WriteString("\tcall i32 @nanosleep(i8* %req.ptr, i8* null)\n")
	sb.WriteString("\tret void\n")
	sb.WriteString("}\n\n")

	if goos == "windows" {
		sb.WriteString("declare i64 @_read(i32, i8*, i64)\n")
		sb.WriteString("declare i64 @_write(i32, i8*, i64)\n")
		sb.WriteString("declare i32 @_close(i32)\n")
	} else if goos == "wasi" {
		// WASI/wasm32: size_t = i32, ssize_t = i32。
		// wasi-libc 的 read/write 簽名為 (i32, i8*, i32) -> i32。
		// 若仍以 i64 宣告，呼叫端傳 i64 引數會在 WASM 驗證階段產生
		// "type mismatch: expected i32, found i64" 錯誤。
		sb.WriteString("declare i32 @read(i32, i8*, i32)\n")
		sb.WriteString("declare i32 @write(i32, i8*, i32)\n")
		sb.WriteString("declare i32 @close(i32)\n")
	} else {
		sb.WriteString("declare i64 @read(i32, i8*, i64)\n")
		sb.WriteString("declare i64 @write(i32, i8*, i64)\n")
		sb.WriteString("declare i32 @close(i32)\n")
	}
	sb.WriteString("declare i8* @fopen(i8*, i8*)\n")
	sb.WriteString("declare i8* @fgets(i8*, i32, i8*)\n")
	sb.WriteString("declare i32 @fclose(i8*)\n")
	// stdin 全域變數：macOS 為 __stdinp，Linux 為 stdin
	// Windows: stdin/stderr are macros, not global variables.
	// Zig's MinGW runtime doesn't export _iob, __iob_func, or _acrt_iob_func.
	// We provide null-returning stubs so the build links successfully.
	// Functions using stdin/stderr will get null FILE* and handle it gracefully.
	if goos == "darwin" {
		sb.WriteString("@__stdinp = external global i8*\n")
	} else if goos == "windows" {
		sb.WriteString("define internal i8* @nolang.win_stdin() {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i8* null\n")
		sb.WriteString("}\n\n")
		sb.WriteString("define internal i8* @nolang.win_stderr() {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i8* null\n")
		sb.WriteString("}\n\n")
	} else {
		sb.WriteString("@stdin = external global i8*\n")
	}
	// stderr 全域變數：macOS 為 __stderrp，Linux 為 stderr
	// Windows: handled via nolang.win_stderr above
	if goos == "darwin" {
		sb.WriteString("@__stderrp = external global i8*\n")
	} else if goos == "windows" {
		// nolang.win_stderr already defined above
	} else {
		sb.WriteString("@stderr = external global i8*\n")
	}
	sb.WriteString("declare void @llvm.lifetime.start.p0i8(i64, i8* nocapture)\n")
	sb.WriteString("declare void @llvm.lifetime.end.p0i8(i64, i8* nocapture)\n\n")

	sb.WriteString("@.os-buf = private global [1024 x i8] zeroinitializer\n")
	sb.WriteString("@.str.true = private unnamed_addr constant [5 x i8] c\"true\\00\"\n")
	sb.WriteString("@.str.false = private unnamed_addr constant [6 x i8] c\"false\\00\"\n")
	sb.WriteString("@.str.empty = private unnamed_addr constant [1 x i8] c\"\\00\"\n")
	sb.WriteString("@.str.fmt = private unnamed_addr constant [3 x i8] c\"%s\\00\"\n")
	sb.WriteString("@.str.sh = private unnamed_addr constant [3 x i8] c\"sh\\00\"\n")
	sb.WriteString("@.str.dashc = private unnamed_addr constant [3 x i8] c\"-c\\00\"\n")
	sb.WriteString("@.str.r = private unnamed_addr constant [2 x i8] c\"r\\00\"\n")
	sb.WriteString("@.str.oob = private unnamed_addr constant [36 x i8] c\"runtime error: index out of bounds\\0A\\00\"\n")
	sb.WriteString("@.str.slice.oob = private unnamed_addr constant [42 x i8] c\"runtime error: slice bounds out of range\\0A\\00\"\n\n")

	// Global storage for argc/argv, set by @main and read by args-count/args-get builtins.
	// These must be globals (not local allocas) because builtins are called from
	// user functions, not from @main itself.
	sb.WriteString("@.argc.addr = global i32 0\n")
	sb.WriteString("@.argv.addr = global i8** null\n\n")

	// nolang.bounds_check: runtime array/slice/string bounds check
	// If idx < 0 || idx >= len, writes error to stderr and exits.
	// Marked alwaysinline so opt -O3 inlines at every call site, enabling
	// dead-branch elimination when the index is provably in range.
	sb.WriteString("define internal void @nolang.bounds_check(i64 %idx, i64 %len) alwaysinline {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%lo = icmp slt i64 %idx, 0\n")
	sb.WriteString("\t%hi = icmp sge i64 %idx, %len\n")
	sb.WriteString("\t%oob = or i1 %lo, %hi\n")
	sb.WriteString("\tbr i1 %oob, label %err, label %ok\n")
	sb.WriteString("err:\n")
	if goos == "windows" {
		sb.WriteString("\tcall i64 @_write(i32 2, i8* getelementptr inbounds ([36 x i8], [36 x i8]* @.str.oob, i64 0, i64 0), i64 36)\n")
	} else {
		// 非 Windows 平台：WASI 透過 @nolang.write wrapper（trunc i64→i32 並 sext i32→i64），
		// 其他平台直接使用 libc @write（i64 介面）。符號由 g.writeSymbol() 決定。
		sb.WriteString("\tcall i64 " + g.writeSymbol() + "(i32 2, i8* getelementptr inbounds ([36 x i8], [36 x i8]* @.str.oob, i64 0, i64 0), i64 36)\n")
	}
	sb.WriteString("\tcall void @exit(i32 1)\n")
	sb.WriteString("\tunreachable\n")
	sb.WriteString("ok:\n")
	sb.WriteString("\tret void\n")
	sb.WriteString("}\n\n")

	// nolang.slice_bounds_check: runtime slice view bounds check.
	// 語義：idx < 0 || idx > len 時 panic（idx == len 合法，對應空視圖）。
	// 與 @nolang.bounds_check 的 區別：後者用 sge (idx >= len) → OOB，
	// 此處用 sgt (idx > len)，因為 start==srcLen/end==srcLen 是合法的空切片。
	// nolang 支持反向 view（start > end 合法），故不檢查 start ≤ end。
	// alwaysinline 使 opt -O3 在調用點內聯並做常量傳播消除不可達分支。
	sb.WriteString("define internal void @nolang.slice_bounds_check(i64 %idx, i64 %len) alwaysinline {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%lo = icmp slt i64 %idx, 0\n")
	sb.WriteString("\t%hi = icmp sgt i64 %idx, %len\n")
	sb.WriteString("\t%oob = or i1 %lo, %hi\n")
	sb.WriteString("\tbr i1 %oob, label %err, label %ok\n")
	sb.WriteString("err:\n")
	if goos == "windows" {
		sb.WriteString("\tcall i64 @_write(i32 2, i8* getelementptr inbounds ([42 x i8], [42 x i8]* @.str.slice.oob, i64 0, i64 0), i64 42)\n")
	} else {
		sb.WriteString("\tcall i64 " + g.writeSymbol() + "(i32 2, i8* getelementptr inbounds ([42 x i8], [42 x i8]* @.str.slice.oob, i64 0, i64 0), i64 42)\n")
	}
	sb.WriteString("\tcall void @exit(i32 1)\n")
	sb.WriteString("\tunreachable\n")
	sb.WriteString("ok:\n")
	sb.WriteString("\tret void\n")
	sb.WriteString("}\n\n")

	// nolang.memcmp: runtime byte-by-byte comparison (replaces @llvm.memcmp intrinsic
	// which is not expanded by opt in LLVM 21). Returns -1, 0, or 1 (like C memcmp).
	sb.WriteString("define internal i32 @nolang.memcmp(i8* %a, i8* %b, i64 %n) {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\tbr label %loop\n")
	sb.WriteString("loop:\n")
	sb.WriteString("\t%i = phi i64 [0, %entry], [%i.next, %loop.cont]\n")
	sb.WriteString("\t%done = icmp eq i64 %i, %n\n")
	sb.WriteString("\tbr i1 %done, label %equal, label %body\n")
	sb.WriteString("body:\n")
	sb.WriteString("\t%a.ptr = getelementptr i8, i8* %a, i64 %i\n")
	sb.WriteString("\t%b.ptr = getelementptr i8, i8* %b, i64 %i\n")
	sb.WriteString("\t%a.byte = load i8, i8* %a.ptr\n")
	sb.WriteString("\t%b.byte = load i8, i8* %b.ptr\n")
	sb.WriteString("\t%cmp.eq = icmp eq i8 %a.byte, %b.byte\n")
	sb.WriteString("\tbr i1 %cmp.eq, label %loop.cont, label %diff\n")
	sb.WriteString("diff:\n")
	sb.WriteString("\t%lt = icmp ult i8 %a.byte, %b.byte\n")
	sb.WriteString("\t%result = select i1 %lt, i32 -1, i32 1\n")
	sb.WriteString("\tret i32 %result\n")
	sb.WriteString("loop.cont:\n")
	sb.WriteString("\t%i.next = add i64 %i, 1\n")
	sb.WriteString("\tbr label %loop\n")
	sb.WriteString("equal:\n")
	sb.WriteString("\tret i32 0\n")
	sb.WriteString("}\n\n")

	// zlib @inflateInit2_ / @inflate / @inflateEnd / @.zlib_ver / @nolang.inflate_raw
	// 已全部移除：archive/gzip.no 的 inflate-decompress 改為純 Nolang 實現
	// （RFC 1951 DEFLATE 解壓縮），不再依賴 zlib 串流 API。

	// POSIX socket functions for net module
	// Windows (MinGW-w64) also provides these via winsock2, but requires
	// WSAStartup initialization before any socket call.
	if goos == "windows" {
		sb.WriteString("declare i32 @WSAStartup(i16, i8*)\n")
		sb.WriteString("declare i32 @WSACleanup()\n")
	}
	sb.WriteString("declare i32 @socket(i32, i32, i32)\n")
	sb.WriteString("declare i32 @setsockopt(i32, i32, i32, i8*, i32)\n")
	sb.WriteString("declare i32 @bind(i32, i8*, i32)\n")
	sb.WriteString("declare i32 @listen(i32, i32)\n")
	sb.WriteString("declare i32 @accept(i32, i8*, i32*)\n")
	sb.WriteString("declare i32 @connect(i32, i8*, i32)\n")
	sb.WriteString("declare i64 @send(i32, i8*, i64, i32)\n")
	sb.WriteString("declare i64 @recv(i32, i8*, i64, i32)\n")
	// nolang_net_recv_nb / nolang_net_accept_nb: LLVM IR implementations.
	// Previously provided by src/build/runtime/process.c, now inlined as LLVM IR.
	// POSIX: uses recv with MSG_DONTWAIT flag (non-blocking).
	// Windows: returns -1 (stub). Non-blocking I/O on Windows requires
	// ioctlsocket + WSAEventSelect which is complex; the pure Nolang
	// implementation in net/ws.no uses async coroutines instead.
	if goos == "windows" {
		sb.WriteString("define internal i64 @nolang_net_recv_nb(i32 %fd, i8* %buf, i64 %n) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i64 -1\n")
		sb.WriteString("}\n")
		sb.WriteString("define internal i64 @nolang_net_accept_nb(i32 %listen_fd) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i64 -1\n")
		sb.WriteString("}\n")
	} else {
		sb.WriteString("define internal i64 @nolang_net_recv_nb(i32 %fd, i8* %buf, i64 %n) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\t%rr = call i64 @recv(i32 %fd, i8* %buf, i64 %n, i32 64)\n")
		sb.WriteString("\tret i64 %rr\n")
		sb.WriteString("}\n")
		sb.WriteString("define internal i64 @nolang_net_accept_nb(i32 %listen_fd) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\t%c = call i32 @accept(i32 %listen_fd, i8* null, i32* null)\n")
		sb.WriteString("\t%ext = zext i32 %c to i64\n")
		sb.WriteString("\tret i64 %ext\n")
		sb.WriteString("}\n")
	}
	sb.WriteString("declare i64 @sendto(i32, i8*, i64, i32, i8*, i32)\n")
	sb.WriteString("declare i64 @recvfrom(i32, i8*, i64, i32, i8*, i32*)\n")
	sb.WriteString("declare i32 @inet_pton(i32, i8*, i8*)\n")
	// DNS resolution (getaddrinfo fallback for net-dial)
	// macOS addrinfo layout: ai_addr at offset 32
	sb.WriteString("declare i32 @getaddrinfo(i8*, i8*, i8*, i8**)\n")
	sb.WriteString("declare void @freeaddrinfo(i8*)\n")

	// TLS 已由純 Nolang 實現（std/net/tls.no），不需要 OpenSSL 宣告。

	// 事件循环运行时（无栈协程）：LLVM IR 内联实现
	// %task = { void (i8*)* resume_fn, i8* data, i1 done }
	// resume_fn 接受 task_ptr (i8*)，通过 task_ptr 访问 data 和 done。
	// wrapper 执行完毕后设置 done=true；coro_resume.N yield 时不设置 done。
	sb.WriteString("@nolang_ready_q = global [256 x i8*] zeroinitializer\n")
	sb.WriteString("@nolang_ready_head = global i32 0\n")
	sb.WriteString("@nolang_ready_tail = global i32 0\n")
	sb.WriteString("@nolang_current_task = global i8* null\n")
	sb.WriteString("@nolang_waiters = global [256 x i8*] zeroinitializer\n")
	// nolang_async_enqueue(task): 入就绪队列
	sb.WriteString("define void @nolang_async_enqueue(i8* %task) {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%tail = load i32, i32* @nolang_ready_tail\n")
	sb.WriteString("\t%gep = getelementptr [256 x i8*], [256 x i8*]* @nolang_ready_q, i32 0, i32 %tail\n")
	sb.WriteString("\tstore i8* %task, i8** %gep\n")
	sb.WriteString("\t%next = add i32 %tail, 1\n")
	sb.WriteString("\t%mod = urem i32 %next, 256\n")
	sb.WriteString("\tstore i32 %mod, i32* @nolang_ready_tail\n")
	sb.WriteString("\tret void\n")
	sb.WriteString("}\n")
	// nolang_async_yield(): 当前协程让出，重新入队
	sb.WriteString("define void @nolang_async_yield() {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%cur = load i8*, i8** @nolang_current_task\n")
	sb.WriteString("\tcall void @nolang_async_enqueue(i8* %cur)\n")
	sb.WriteString("\tret void\n")
	sb.WriteString("}\n")
	// nolang_async_wait(waited_task): 当前协程等待 task 完成
	// 当前 task 不入队，只在 waited_task 完成时被 nolang_async_done 唤醒。
	sb.WriteString("define void @nolang_async_wait(i8* %waited) {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%cur = load i8*, i8** @nolang_current_task\n")
	sb.WriteString("\t%idx = ptrtoint i8* %waited to i64\n")
	sb.WriteString("\t%idx8 = and i64 %idx, 255\n")
	sb.WriteString("\t%idx32 = trunc i64 %idx8 to i32\n")
	sb.WriteString("\t%gep = getelementptr [256 x i8*], [256 x i8*]* @nolang_waiters, i32 0, i32 %idx32\n")
	sb.WriteString("\tstore i8* %cur, i8** %gep\n")
	sb.WriteString("\tret void\n")
	sb.WriteString("}\n")
	// nolang_async_done(task): 标记完成，唤醒等待者
	sb.WriteString("define void @nolang_async_done(i8* %task) {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%idx = ptrtoint i8* %task to i64\n")
	sb.WriteString("\t%idx8 = and i64 %idx, 255\n")
	sb.WriteString("\t%idx32 = trunc i64 %idx8 to i32\n")
	sb.WriteString("\t%gep = getelementptr [256 x i8*], [256 x i8*]* @nolang_waiters, i32 0, i32 %idx32\n")
	sb.WriteString("\t%waiter = load i8*, i8** %gep\n")
	sb.WriteString("\t%is_null = icmp eq i8* %waiter, null\n")
	sb.WriteString("\tbr i1 %is_null, label %ret, label %wake\n")
	sb.WriteString("wake:\n")
	sb.WriteString("\tcall void @nolang_async_enqueue(i8* %waiter)\n")
	sb.WriteString("\tstore i8* null, i8** %gep\n")
	sb.WriteString("\tbr label %ret\n")
	sb.WriteString("ret:\n")
	sb.WriteString("\tret void\n")
	sb.WriteString("}\n")
	// nolang_async_run(main_task): 事件循环主函数
	// 循环：取 task → 调用 resume_fn(task_ptr) → 检查 done → done 则唤醒等待者
	sb.WriteString("define void @nolang_async_run(i8* %main_task) {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\tcall void @nolang_async_enqueue(i8* %main_task)\n")
	sb.WriteString("\tbr label %loop\n")
	sb.WriteString("loop:\n")
	sb.WriteString("\t%head = load i32, i32* @nolang_ready_head\n")
	sb.WriteString("\t%tail = load i32, i32* @nolang_ready_tail\n")
	sb.WriteString("\t%eq = icmp eq i32 %head, %tail\n")
	sb.WriteString("\tbr i1 %eq, label %exit, label %run_one\n")
	sb.WriteString("run_one:\n")
	sb.WriteString("\t%gep = getelementptr [256 x i8*], [256 x i8*]* @nolang_ready_q, i32 0, i32 %head\n")
	sb.WriteString("\t%task = load i8*, i8** %gep\n")
	sb.WriteString("\tstore i8* %task, i8** @nolang_current_task\n")
	sb.WriteString("\t%next = add i32 %head, 1\n")
	sb.WriteString("\t%mod = urem i32 %next, 256\n")
	sb.WriteString("\tstore i32 %mod, i32* @nolang_ready_head\n")
	// 从 task 取 resume_fn (field 0)
	sb.WriteString("\t%task_typed = bitcast i8* %task to { void (i8*)*, i64, i1, i1 }*\n")
	sb.WriteString("\t%fn_gep = getelementptr { void (i8*)*, i64, i1, i1 }, { void (i8*)*, i64, i1, i1 }* %task_typed, i32 0, i32 0\n")
	sb.WriteString("\t%resume_fn = load void (i8*)*, void (i8*)** %fn_gep\n")
	// 调用 resume_fn(task_ptr) — 传 task_ptr，resume_fn 通过它访问 data 和 done
	sb.WriteString("\tcall void %resume_fn(i8* %task)\n")
	// 检查 done (field 2)
	sb.WriteString("\t%done_gep = getelementptr { void (i8*)*, i64, i1, i1 }, { void (i8*)*, i64, i1, i1 }* %task_typed, i32 0, i32 2\n")
	sb.WriteString("\t%done_val = load i1, i1* %done_gep\n")
	sb.WriteString("\tbr i1 %done_val, label %done_handler, label %loop\n")
	sb.WriteString("done_handler:\n")
	sb.WriteString("\tcall void @nolang_async_done(i8* %task)\n")
	sb.WriteString("\tbr label %loop\n")
	sb.WriteString("exit:\n")
	sb.WriteString("\tret void\n")
	sb.WriteString("}\n")

	// Windows stub functions: POSIX functions that have no direct Windows
	// equivalent are routed to these no-op/internal stubs on Windows.
	// Return types align with the CLibCall RetType declared in os.go/process.go
	// (i32 for all of these; RetExt sexts to i64 at the call site where needed).
	if goos == "windows" {
		sb.WriteString("define internal i32 @nolang.win_getuid() {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i32 0\n")
		sb.WriteString("}\n\n")

		sb.WriteString("define internal i32 @nolang.win_getgid() {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i32 0\n")
		sb.WriteString("}\n\n")

		sb.WriteString("define internal i32 @nolang.win_getppid() {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i32 0\n")
		sb.WriteString("}\n\n")

		sb.WriteString("define internal i32 @nolang.win_chown(i8* %path, i32 %uid, i32 %gid) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i32 0\n")
		sb.WriteString("}\n\n")

		sb.WriteString("define internal i32 @nolang.win_utimensat(i32 %dirfd, i8* %path, i8* %times, i32 %flag) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i32 0\n")
		sb.WriteString("}\n\n")

		sb.WriteString("define internal i32 @nolang.win_kill(i32 %pid, i32 %sig) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i32 -1\n")
		sb.WriteString("}\n\n")

		// symlink/link: no direct Windows C equivalent (would need
		// CreateSymbolicLinkA/CreateHardLinkA). Return -1 (failure).
		sb.WriteString("define internal i32 @nolang.win_symlink(i8* %target, i8* %linkpath) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i32 -1\n")
		sb.WriteString("}\n\n")

		sb.WriteString("define internal i32 @nolang.win_link(i8* %oldpath, i8* %newpath) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i32 -1\n")
		sb.WriteString("}\n\n")

		// nolang.win_waitpid: POSIX waitpid-compatible wrapper around Windows _cwait.
		// waitpid(pid, int* status, int options) → returns pid or -1.
		// _cwait(int* termstat, int procid, int action) → returns handle or -1.
		// Two differences are normalized here:
		//  1. Parameter order: _cwait takes (status*, pid, action) — swapped vs waitpid.
		//  2. Status encoding: _cwait stores the exit code in the low byte of termstat,
		//     whereas POSIX waitpid encodes it in bits 8-15 (WEXITSTATUS = (status>>8)&0xFF).
		//     Shift left by 8 so the existing WEXITSTATUS extraction yields the exit code.
		sb.WriteString("define internal i32 @nolang.win_waitpid(i32 %pid, i32* %status, i32 %options) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\t%ret = call i32 @_cwait(i32* %status, i32 %pid, i32 0)\n")
		sb.WriteString("\t%ld = load i32, i32* %status\n")
		sb.WriteString("\t%shl = shl i32 %ld, 8\n")
		sb.WriteString("\tstore i32 %shl, i32* %status\n")
		sb.WriteString("\t%iserr = icmp eq i32 %ret, -1\n")
		sb.WriteString("\t%result = select i1 %iserr, i32 -1, i32 %pid\n")
		sb.WriteString("\tret i32 %result\n")
		sb.WriteString("}\n\n")

		// nolang.win_chroot: chroot is not supported on Windows.
		// Return -1 (failure) to indicate the operation is not supported.
		sb.WriteString("define internal i32 @nolang.win_chroot(i8* %path) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i32 -1\n")
		sb.WriteString("}\n\n")

		// nolang.win_mknod: mknod is not supported on Windows.
		sb.WriteString("define internal i32 @nolang.win_mknod(i8* %path, i32 %mode, i64 %dev) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i32 -1\n")
		sb.WriteString("}\n\n")

		// nolang.win_truncate: truncate is not directly available on Windows.
		// Could use SetEndOfFile, but for now return -1 (failure).
		sb.WriteString("define internal i32 @nolang.win_truncate(i8* %path, i64 %length) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i32 -1\n")
		sb.WriteString("}\n\n")

		// nolang.win_mkfifo: mkfifo (named pipes) not supported via POSIX API on Windows.
		sb.WriteString("define internal i32 @nolang.win_mkfifo(i8* %path, i32 %mode) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i32 -1\n")
		sb.WriteString("}\n\n")

		// nolang.win_signal: signal handling is different on Windows.
		// Return 0 (SIG_DFL) as a no-op.
		sb.WriteString("define internal i64 @nolang.win_signal(i32 %sig, i64 %handler) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i64 0\n")
		sb.WriteString("}\n\n")

		// nolang.win_gethostid: gethostid is not available on Windows.
		sb.WriteString("define internal i32 @nolang.win_gethostid() {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i32 0\n")
		sb.WriteString("}\n\n")

		// nolang.win_setpriority: setpriority is not available on Windows.
		sb.WriteString("define internal i32 @nolang.win_setpriority(i32 %which, i32 %who, i32 %prio) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i32 -1\n")
		sb.WriteString("}\n\n")

		// nolang.win_setsid: setsid is not available on Windows.
		sb.WriteString("define internal i32 @nolang.win_setsid() {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i32 -1\n")
		sb.WriteString("}\n\n")

		// nolang.win_flock: flock is not available on Windows.
		// Could use LockFileEx, but for now return 0 (success) as a no-op.
		sb.WriteString("define internal i32 @nolang.win_flock(i32 %fd, i32 %op) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret i32 0\n")
		sb.WriteString("}\n\n")

		// nolang.win_setenv: Windows doesn't have setenv, use _putenv_s instead.
		// _putenv_s(name, value) returns 0 on success.
		sb.WriteString("define internal i32 @nolang.win_setenv(i8* %name, i8* %value, i32 %overwrite) {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\t%ret = call i32 @_putenv_s(i8* %name, i8* %value)\n")
		sb.WriteString("\tret i32 %ret\n")
		sb.WriteString("}\n\n")

		// sync: POSIX sync() is not available on Windows.
		// Provide a no-op stub so that any generated call links successfully.
		sb.WriteString("define internal void @sync() {\n")
		sb.WriteString("entry:\n")
		sb.WriteString("\tret void\n")
		sb.WriteString("}\n\n")

		// Declare _putenv_s for the setenv wrapper
		sb.WriteString("declare i32 @_putenv_s(i8*, i8*)\n")
	}
}

// StatLayout 描述各平台的 struct stat 佈局（大小與欄位位移）。
// 用於取代硬編碼的 macOS arm64 位移，使 is-dir/is-file/stat-* 等內建跨平台相容。
//
// 佈局來源：
//
//	macOS (arm64/amd64): <sys/stat.h> struct stat (st_mtimespec)
//	Linux x86_64:        glibc <bits/struct_stat.h> struct stat (st_mtim)
//	Linux arm64:         glibc aarch64 struct stat (st_mtim)
type StatLayout struct {
	Size     int64 // struct stat 總大小（bytes）
	ModeOff  int64 // st_mode 位移
	UidOff   int64 // st_uid 位移
	GidOff   int64 // st_gid 位移
	MtimeOff int64 // st_mtime.tv_sec 位移
	SizeOff  int64 // st_size 位移
}

// statLayout 返回當前編譯目標平台的 struct stat 佈局。
// 若平台未知，回退到 macOS arm64 佈局（既有行為）。
func statLayout() StatLayout {
	return statLayoutFor(runtime.GOOS, runtime.GOARCH)
}

// statLayout 方法版本：返回 Generator 當前編譯目標平台的 struct stat 佈局。
// 使用 g.goos()/g.goarch() 而非 runtime.GOOS/GOARCH，支援跨平台編譯（如 wasm32-wasi）。
func (g *Generator) statLayout() StatLayout {
	return statLayoutFor(g.goos(), g.goarch())
}

// statLayoutFor 返回指定平台的 struct stat 佈局。
// 接受 goos/goarch 參數以方便測試注入，驗證所有平台分支。
func statLayoutFor(goos, goarch string) StatLayout {
	switch goos {
	case "darwin":
		// macOS arm64/amd64: struct stat (st_mtimespec at offset 48)
		// dev_t st_dev@0, mode_t st_mode@4, nlink_t st_nlink@6, ino_t st_ino@8,
		// uid_t st_uid@16, gid_t st_gid@20, dev_t st_rdev@24,
		// st_atimespec@32, st_mtimespec@48, st_ctimespec@64, st_birthtimespec@80,
		// off_t st_size@96
		return StatLayout{Size: 144, ModeOff: 4, UidOff: 16, GidOff: 20, MtimeOff: 48, SizeOff: 96}
	case "linux":
		if goarch == "arm64" {
			// Linux arm64 (aarch64): glibc struct stat
			// dev_t st_dev@0, ino_t st_ino@8, mode_t st_mode@16, nlink_t st_nlink@20,
			// uid_t st_uid@24, gid_t st_gid@28, dev_t st_rdev@32, __pad1@40,
			// off_t st_size@48, st_atim@72, st_mtim@88, st_ctim@104
			return StatLayout{Size: 128, ModeOff: 16, UidOff: 24, GidOff: 28, MtimeOff: 88, SizeOff: 48}
		}
		// Linux x86_64/amd64: glibc struct stat
		// dev_t st_dev@0, ino_t st_ino@8, nlink_t st_nlink@16,
		// mode_t st_mode@24, uid_t st_uid@28, gid_t st_gid@32, __pad0@36,
		// dev_t st_rdev@40, off_t st_size@48, st_atim@72, st_mtim@88, st_ctim@104
		return StatLayout{Size: 144, ModeOff: 24, UidOff: 28, GidOff: 32, MtimeOff: 88, SizeOff: 48}
	case "windows":
		// Windows (MinGW-w64) _stat64: struct _stat64
		// 注意：st_uid@20/st_gid@22 在 Windows 上恆為 0（Windows _stat64 結構中
		// 這些欄位存在但恆 0），但位移仍需正確以對齊既有 stat-* 內建欄位讀取。
		return StatLayout{Size: 64, ModeOff: 16, UidOff: 20, GidOff: 22, MtimeOff: 48, SizeOff: 32}
	default:
		// 未知平台：回退到 macOS arm64 佈局
		return StatLayout{Size: 144, ModeOff: 4, UidOff: 16, GidOff: 20, MtimeOff: 48, SizeOff: 96}
	}
}

// openWriteFlags 返回當前平台的 O_WRONLY|O_CREAT|O_TRUNC 組合值。
//
//	macOS:   1 | 512 | 1024 = 1537
//	Linux:   1 | 64  | 512  = 577
//	Windows: 1 | 256 | 512  = 769  (_O_WRONLY | _O_CREAT | _O_TRUNC)
//
// 用於 open-write 與 write-file 內建，取代硬編碼的 1537。
func openWriteFlags() int {
	return openWriteFlagsFor(runtime.GOOS)
}

// openWriteFlags 方法版本：返回 Generator 當前編譯目標平台的 O_WRONLY|O_CREAT|O_TRUNC 組合值。
// 使用 g.goos() 而非 runtime.GOOS，支援跨平台編譯。
func (g *Generator) openWriteFlags() int {
	return openWriteFlagsFor(g.goos())
}

// openWriteFlagsFor 返回指定平台的 O_WRONLY|O_CREAT|O_TRUNC 組合值。
// 接受 goos 參數以方便測試注入，驗證所有平台分支。
func openWriteFlagsFor(goos string) int {
	switch goos {
	case "darwin":
		return 1537 // O_WRONLY(1) | O_CREAT(0x200) | O_TRUNC(0x400)
	case "linux":
		return 577 // O_WRONLY(1) | O_CREAT(0x40) | O_TRUNC(0x200)
	case "windows":
		return 769 // _O_WRONLY(1) | _O_CREAT(0x100) | _O_TRUNC(0x200)
	default:
		return 1537 // 回退到 macOS 值
	}
}

// libcFn 返回當前編譯目標平台的 libc 函式名稱。
// 在 Windows (MinGW-w64) 上，多数 POSIX 函式加上底線前綴；
// stat 以 _stat64 取代，waitpid 以 _cwait 取代。
// utimensat 不在此處處理（在 Windows 路由到 @nolang.win_utimensat stub）。
func libcFn(posixName string) string {
	return libcFnFor(runtime.GOOS, posixName)
}

// libcFn 方法版本：返回 Generator 當前編譯目標平台的 libc 函式名稱。
// 使用 g.goos() 而非 runtime.GOOS，支援跨平台編譯（如 wasm32-wasi）。
func (g *Generator) libcFn(posixName string) string {
	return libcFnFor(g.goos(), posixName)
}

// libcFnFor 返回指定平台的 libc 函式名稱。
// 接受 goos 參數以方便測試注入。
func libcFnFor(goos, posixName string) string {
	if goos == "windows" {
		switch posixName {
		case "stat":
			return "_stat64"
		case "waitpid":
			return "_cwait"
		case "open", "read", "write", "close", "mkdir", "chmod", "unlink",
			"getcwd", "chdir", "dup2", "pipe", "execlp", "rmdir":
			return "_" + posixName
		// Unix-only functions routed to nolang.win_* stubs on Windows.
		// These stubs are defined in writeWindowsStubs (decl.go).
		case "chown":
			return "nolang.win_chown"
		case "symlink":
			return "nolang.win_symlink"
		case "link":
			return "nolang.win_link"
		case "kill":
			return "nolang.win_kill"
		case "getuid", "geteuid":
			return "nolang.win_getuid"
		case "getgid", "getegid":
			return "nolang.win_getgid"
		case "getppid":
			return "nolang.win_getppid"
		case "utimensat":
			return "nolang.win_utimensat"
		case "chroot":
			return "nolang.win_chroot"
		case "mknod":
			return "nolang.win_mknod"
		case "truncate":
			return "nolang.win_truncate"
		case "mkfifo":
			return "nolang.win_mkfifo"
		case "signal":
			return "nolang.win_signal"
		case "gethostid":
			return "nolang.win_gethostid"
		case "setpriority":
			return "nolang.win_setpriority"
		case "setsid":
			return "nolang.win_setsid"
		case "flock":
			return "nolang.win_flock"
		case "setenv":
			return "nolang.win_setenv"
		}
	}
	return posixName
}
