package mir

import (
	"fmt"
	"strings"
)

// rawPOSIXSupportDecls lists the libc entry points used by the raw VFS helpers.
// open and fcntl are variadic; codegen.decl rewrites their declarations and the
// helper call sites spell the variadic function type explicitly.
func rawPOSIXSupportDecls() []string {
	return []string{
		"declare i32 @open(i8*, i32, i32)",
		"declare i32 @close(i32)",
		"declare i64 @lseek(i32, i64, i32)",
		"declare i64 @pread(i32, i8*, i64, i64)",
		"declare i64 @pwrite(i32, i8*, i64, i64)",
		"declare i32 @ftruncate(i32, i64)",
		"declare i32 @fstat(i32, i8*)",
		"declare i32 @fsync(i32)",
		"declare i32 @fdatasync(i32)",
		"declare i32 @fcntl(i32, i32, i8*)",
	}
}

func rawPOSIXSupportIR() string {
	st := statLayoutFor()
	flockSize, typeOff, whenceOff, startOff, lengthOff := int64(32), int64(0), int64(2), int64(8), int64(16)
	if targetGOOS() == "darwin" {
		flockSize, typeOff, whenceOff, startOff, lengthOff = 24, 20, 22, 0, 8
	}
	oCreate, oExcl, oTrunc, oDirectory := int64(512), int64(2048), int64(1024), int64(0x100000)
	setlk, setlkw := int64(8), int64(9)
	// F_RDLCK / F_WRLCK / F_UNLCK are OS-specific. They are NOT the same
	// across Darwin and Linux: the old code hard-coded Darwin's values, so on
	// Linux F_SETLK with type 3 (Darwin F_WRLCK) was EINVAL and every exclusive
	// range lock failed. Keep them alongside the FLOCK layout constants above,
	// which are already selected per target.
	rdlck, wrlck, unlck := int64(1), int64(3), int64(2) // Darwin: F_RDLCK=1, F_WRLCK=3, F_UNLCK=2
	if targetGOOS() == "linux" {
		oCreate, oExcl, oTrunc, oDirectory = 64, 128, 512, 0x10000
		setlk, setlkw = 6, 7
		rdlck, wrlck, unlck = 0, 1, 2 // Linux: F_RDLCK=0, F_WRLCK=1, F_UNLCK=2
	}
	r := strings.NewReplacer(
		"@ERRNO@", "@"+errnoFnNameForRawTarget(),
		"@O_CREAT@", fmt.Sprint(oCreate),
		"@O_EXCL@", fmt.Sprint(oExcl),
		"@O_TRUNC@", fmt.Sprint(oTrunc),
		"@O_DIRECTORY@", fmt.Sprint(oDirectory),
		"@FCNTL_SETLK@", fmt.Sprint(setlk),
		"@FCNTL_SETLKW@", fmt.Sprint(setlkw),
		"@FLOCK_RDLCK@", fmt.Sprint(rdlck),
		"@FLOCK_WRLCK@", fmt.Sprint(wrlck),
		"@FLOCK_UNLCK@", fmt.Sprint(unlck),
		"@STAT_SIZE@", fmt.Sprint(st.Size),
		"@STAT_SIZE_OFF@", fmt.Sprint(st.SizeOff),
		"@FLOCK_SIZE@", fmt.Sprint(flockSize),
		"@FLOCK_TYPE_OFF@", fmt.Sprint(typeOff),
		"@FLOCK_WHENCE_OFF@", fmt.Sprint(whenceOff),
		"@FLOCK_START_OFF@", fmt.Sprint(startOff),
		"@FLOCK_LEN_OFF@", fmt.Sprint(lengthOff),
	)
	return r.Replace(rawPOSIXSupportConst)
}

func errnoFnNameForRawTarget() string {
	if targetGOOS() == "linux" {
		return "__errno_location"
	}
	return "__error"
}

// The helpers return -1 on failure and leave a meaningful errno. Scalar
// validation happens here, before a negative offset or count can be truncated
// into a plausible size_t/off_t argument.
const rawPOSIXSupportConst = `
define internal void @nolang.raw_posix_errno(i32 %v) {
entry:
  %e = call i32* @ERRNO@()
  store i32 %v, i32* %e
  ret void
}

define i64 @nolang.raw_open(i8* %path, i64 %flags, i64 %mode) {
entry:
  %unknown = and i64 %flags, -32
  %hasunknown = icmp ne i64 %unknown, 0
  %access = and i64 %flags, 3
  %isread = icmp eq i64 %access, 1
  %iswrite = icmp eq i64 %access, 2
  %isrw = icmp eq i64 %access, 3
  %goodaccess1 = or i1 %isread, %iswrite
  %goodaccess = or i1 %goodaccess1, %isrw
  %nogoodaccess = xor i1 %goodaccess, true
  %badflags = or i1 %hasunknown, %nogoodaccess
  %cre = and i64 %flags, 4
  %ex = and i64 %flags, 8
  %tr = and i64 %flags, 16
  %hascreate = icmp ne i64 %cre, 0
  %hasexcl = icmp ne i64 %ex, 0
  %hastrunc = icmp ne i64 %tr, 0
  %writeflag = and i64 %flags, 2
  %canwrite = icmp ne i64 %writeflag, 0
  %cannotwrite = xor i1 %canwrite, true
  %createbad = and i1 %hascreate, %cannotwrite
  %nocreate = xor i1 %hascreate, true
  %exclbad = and i1 %hasexcl, %nocreate
  %truncbad = and i1 %hastrunc, %cannotwrite
  %bad1 = or i1 %badflags, %createbad
  %bad2 = or i1 %bad1, %exclbad
  %bad3 = or i1 %bad2, %truncbad
  %modeneg = icmp slt i64 %mode, 0
  %modebig = icmp ugt i64 %mode, 4095
  %badmode = or i1 %modeneg, %modebig
  %bad = or i1 %bad3, %badmode
  br i1 %bad, label %einval, label %translate
einval:
  call void @nolang.raw_posix_errno(i32 22)
  ret i64 -1
translate:
  %wr = and i64 %flags, 2
  %haswr = icmp ne i64 %wr, 0
  %rdwr = and i64 %flags, 3
  %isrdwr = icmp eq i64 %rdwr, 3
  %acc1 = select i1 %haswr, i64 1, i64 0
  %acc = select i1 %isrdwr, i64 2, i64 %acc1
  %createflag = select i1 %hascreate, i64 @O_CREAT@, i64 0
  %exclflag = select i1 %hasexcl, i64 @O_EXCL@, i64 0
  %truncflag = select i1 %hastrunc, i64 @O_TRUNC@, i64 0
  %f1 = or i64 %acc, %createflag
  %f2 = or i64 %f1, %exclflag
  %oflags = or i64 %f2, %truncflag
  %flags32 = trunc i64 %oflags to i32
  %mode32 = trunc i64 %mode to i32
  %fd = call i32 (i8*, i32, ...) @open(i8* %path, i32 %flags32, i32 %mode32)
  %fd64 = sext i32 %fd to i64
  ret i64 %fd64
}

define i32 @nolang.raw_close(i64 %handle) {
entry:
  %neg = icmp slt i64 %handle, 0
  %big = icmp ugt i64 %handle, 2147483647
  %bad = or i1 %neg, %big
  br i1 %bad, label %einval, label %close
einval:
  call void @nolang.raw_posix_errno(i32 22)
  ret i32 -1
close:
  %fd = trunc i64 %handle to i32
  %r = call i32 @close(i32 %fd)
  ret i32 %r
}

define i64 @nolang.raw_seek(i64 %handle, i64 %offset, i64 %whence) {
entry:
  %neg = icmp slt i64 %handle, 0
  %big = icmp ugt i64 %handle, 2147483647
  %badfd = or i1 %neg, %big
  %w0 = icmp eq i64 %whence, 0
  %w1 = icmp eq i64 %whence, 1
  %w2 = icmp eq i64 %whence, 2
  %w01 = or i1 %w0, %w1
  %badwhence = or i1 %w01, %w2
  %badwhence2 = xor i1 %badwhence, true
  %bad = or i1 %badfd, %badwhence2
  br i1 %bad, label %einval, label %seek
einval:
  call void @nolang.raw_posix_errno(i32 22)
  ret i64 -1
seek:
  %fd = trunc i64 %handle to i32
  %wh = trunc i64 %whence to i32
  %r = call i64 @lseek(i32 %fd, i64 %offset, i32 %wh)
  ret i64 %r
}

define i64 @nolang.raw_pread(i64 %handle, i8* %buf, i64 %count, i64 %offset, i64 %length) {
entry:
  %negfd = icmp slt i64 %handle, 0
  %bigfd = icmp ugt i64 %handle, 2147483647
  %badfd = or i1 %negfd, %bigfd
  %negcount = icmp slt i64 %count, 0
  %negoffset = icmp slt i64 %offset, 0
  %over = icmp ugt i64 %count, %length
  %badcount = or i1 %negcount, %negoffset
  %badcount2 = or i1 %badcount, %over
  %bad = or i1 %badfd, %badcount2
  br i1 %bad, label %einval, label %read
einval:
  call void @nolang.raw_posix_errno(i32 22)
  ret i64 -1
read:
  %fd = trunc i64 %handle to i32
  %r = call i64 @pread(i32 %fd, i8* %buf, i64 %count, i64 %offset)
  ret i64 %r
}

define i64 @nolang.raw_pwrite(i64 %handle, i8* %data, i64 %count, i64 %offset, i64 %length) {
entry:
  %negfd = icmp slt i64 %handle, 0
  %bigfd = icmp ugt i64 %handle, 2147483647
  %badfd = or i1 %negfd, %bigfd
  %negcount = icmp slt i64 %count, 0
  %negoffset = icmp slt i64 %offset, 0
  %over = icmp ugt i64 %count, %length
  %badcount = or i1 %negcount, %negoffset
  %badcount2 = or i1 %badcount, %over
  %bad = or i1 %badfd, %badcount2
  br i1 %bad, label %einval, label %write
einval:
  call void @nolang.raw_posix_errno(i32 22)
  ret i64 -1
write:
  %fd = trunc i64 %handle to i32
  %r = call i64 @pwrite(i32 %fd, i8* %data, i64 %count, i64 %offset)
  ret i64 %r
}

define i32 @nolang.raw_ftruncate(i64 %handle, i64 %length) {
entry:
  %negfd = icmp slt i64 %handle, 0
  %bigfd = icmp ugt i64 %handle, 2147483647
  %badfd = or i1 %negfd, %bigfd
  %neglen = icmp slt i64 %length, 0
  %bad = or i1 %badfd, %neglen
  br i1 %bad, label %einval, label %truncate
einval:
  call void @nolang.raw_posix_errno(i32 22)
  ret i32 -1
truncate:
  %fd = trunc i64 %handle to i32
  %r = call i32 @ftruncate(i32 %fd, i64 %length)
  ret i32 %r
}

define i64 @nolang.raw_fstat_size(i64 %handle) {
entry:
  %neg = icmp slt i64 %handle, 0
  %big = icmp ugt i64 %handle, 2147483647
  %bad = or i1 %neg, %big
  br i1 %bad, label %einval, label %stat
einval:
  call void @nolang.raw_posix_errno(i32 22)
  ret i64 -1
stat:
  %fd = trunc i64 %handle to i32
  %buf = alloca [@STAT_SIZE@ x i8], align 8
  %p = bitcast [@STAT_SIZE@ x i8]* %buf to i8*
  %r = call i32 @fstat(i32 %fd, i8* %p)
  %ok = icmp eq i32 %r, 0
  br i1 %ok, label %size, label %fail
size:
  %sp = getelementptr i8, i8* %p, i64 @STAT_SIZE_OFF@
  %spp = bitcast i8* %sp to i64*
  %sz = load i64, i64* %spp
  ret i64 %sz
fail:
  ret i64 -1
}

define i32 @nolang.raw_fsync(i64 %handle) {
entry:
  %neg = icmp slt i64 %handle, 0
  %big = icmp ugt i64 %handle, 2147483647
  %bad = or i1 %neg, %big
  br i1 %bad, label %einval, label %sync
einval:
  call void @nolang.raw_posix_errno(i32 22)
  ret i32 -1
sync:
  %fd = trunc i64 %handle to i32
  %r = call i32 @fsync(i32 %fd)
  ret i32 %r
}

define i32 @nolang.raw_fdatasync(i64 %handle) {
entry:
  %neg = icmp slt i64 %handle, 0
  %big = icmp ugt i64 %handle, 2147483647
  %bad = or i1 %neg, %big
  br i1 %bad, label %einval, label %sync
einval:
  call void @nolang.raw_posix_errno(i32 22)
  ret i32 -1
sync:
  %fd = trunc i64 %handle to i32
  %r = call i32 @fdatasync(i32 %fd)
  ret i32 %r
}

define i32 @nolang.raw_sync_dir(i8* %path) {
entry:
  %flags = or i32 @O_DIRECTORY@, 0
  %fd = call i32 (i8*, i32, ...) @open(i8* %path, i32 %flags, i32 0)
  %bad = icmp slt i32 %fd, 0
  br i1 %bad, label %fail, label %sync
sync:
  %fr = call i32 @fsync(i32 %fd)
  %ep = call i32* @ERRNO@()
  %fe = load i32, i32* %ep
  %cr = call i32 @close(i32 %fd)
  %ce = load i32, i32* %ep
  %fok = icmp eq i32 %fr, 0
  %cok = icmp eq i32 %cr, 0
  %ok = and i1 %fok, %cok
  %e = select i1 %fok, i32 %ce, i32 %fe
  %finale = select i1 %ok, i32 0, i32 %e
  store i32 %finale, i32* %ep
  %r = select i1 %ok, i32 0, i32 -1
  ret i32 %r
fail:
  ret i32 -1
}

define internal void @nolang.raw_posix_set_flock(i8* %p, i64 %start, i64 %length, i32 %type) {
entry:
  %tp = getelementptr i8, i8* %p, i64 @FLOCK_TYPE_OFF@
  %tp16 = bitcast i8* %tp to i16*
  %t16 = trunc i32 %type to i16
  store i16 %t16, i16* %tp16
  %wp = getelementptr i8, i8* %p, i64 @FLOCK_WHENCE_OFF@
  %wp16 = bitcast i8* %wp to i16*
  store i16 0, i16* %wp16
  %sp = getelementptr i8, i8* %p, i64 @FLOCK_START_OFF@
  %sp64 = bitcast i8* %sp to i64*
  store i64 %start, i64* %sp64
  %lp = getelementptr i8, i8* %p, i64 @FLOCK_LEN_OFF@
  %lp64 = bitcast i8* %lp to i64*
  store i64 %length, i64* %lp64
  ret void
}

define i32 @nolang.raw_lock_range(i64 %handle, i64 %start, i64 %length, i64 %mode, i1 %nonblocking) {
entry:
  %negfd = icmp slt i64 %handle, 0
  %bigfd = icmp ugt i64 %handle, 2147483647
  %badfd = or i1 %negfd, %bigfd
  %negstart = icmp slt i64 %start, 0
  %neglen = icmp slt i64 %length, 0
  %shared = icmp eq i64 %mode, 1
  %exclusive = icmp eq i64 %mode, 2
  %goodmode = or i1 %shared, %exclusive
  %badmode = xor i1 %goodmode, true
  %bad1 = or i1 %badfd, %negstart
  %bad2 = or i1 %bad1, %neglen
  %bad3 = or i1 %bad2, %badmode
  br i1 %bad3, label %einval, label %lock
einval:
  call void @nolang.raw_posix_errno(i32 22)
  ret i32 -1
lock:
  %type = select i1 %exclusive, i32 @FLOCK_WRLCK@, i32 @FLOCK_RDLCK@
  %buf = alloca [@FLOCK_SIZE@ x i8], align 8
  %p = bitcast [@FLOCK_SIZE@ x i8]* %buf to i8*
  call void @llvm.memset.p0i8.i64(i8* %p, i8 0, i64 @FLOCK_SIZE@, i1 false)
  call void @nolang.raw_posix_set_flock(i8* %p, i64 %start, i64 %length, i32 %type)
  %fd = trunc i64 %handle to i32
  %cmd = select i1 %nonblocking, i32 @FCNTL_SETLK@, i32 @FCNTL_SETLKW@
  %r = call i32 (i32, i32, ...) @fcntl(i32 %fd, i32 %cmd, i8* %p)
  ret i32 %r
}

define i32 @nolang.raw_unlock_range(i64 %handle, i64 %start, i64 %length) {
entry:
  %negfd = icmp slt i64 %handle, 0
  %bigfd = icmp ugt i64 %handle, 2147483647
  %badfd = or i1 %negfd, %bigfd
  %negstart = icmp slt i64 %start, 0
  %neglen = icmp slt i64 %length, 0
  %bad1 = or i1 %badfd, %negstart
  %bad = or i1 %bad1, %neglen
  br i1 %bad, label %einval, label %unlock
einval:
  call void @nolang.raw_posix_errno(i32 22)
  ret i32 -1
unlock:
  %buf = alloca [@FLOCK_SIZE@ x i8], align 8
  %p = bitcast [@FLOCK_SIZE@ x i8]* %buf to i8*
  call void @llvm.memset.p0i8.i64(i8* %p, i8 0, i64 @FLOCK_SIZE@, i1 false)
  call void @nolang.raw_posix_set_flock(i8* %p, i64 %start, i64 %length, i32 @FLOCK_UNLCK@)
  %fd = trunc i64 %handle to i32
  %r = call i32 (i32, i32, ...) @fcntl(i32 %fd, i32 @FCNTL_SETLK@, i8* %p)
  ret i32 %r
}
`

func rawUnsupportedSupportIR() string {
	return `
define internal void @nolang.raw_unsupported_errno() {
entry:
  %e = call i32* @` + errnoFnNameForRawTarget() + `()
  store i32 40, i32* %e
  ret void
}
define i64 @nolang.raw_open(i8* %path, i64 %flags, i64 %mode) {
entry:
  call void @nolang.raw_unsupported_errno()
  ret i64 -1
}
define i32 @nolang.raw_close(i64 %handle) {
entry:
  call void @nolang.raw_unsupported_errno()
  ret i32 -1
}
define i64 @nolang.raw_seek(i64 %handle, i64 %offset, i64 %whence) {
entry:
  call void @nolang.raw_unsupported_errno()
  ret i64 -1
}
define i64 @nolang.raw_pread(i64 %handle, i8* %buf, i64 %count, i64 %offset, i64 %length) {
entry:
  call void @nolang.raw_unsupported_errno()
  ret i64 -1
}
define i64 @nolang.raw_pwrite(i64 %handle, i8* %data, i64 %count, i64 %offset, i64 %length) {
entry:
  call void @nolang.raw_unsupported_errno()
  ret i64 -1
}
define i32 @nolang.raw_ftruncate(i64 %handle, i64 %length) {
entry:
  call void @nolang.raw_unsupported_errno()
  ret i32 -1
}
define i64 @nolang.raw_fstat_size(i64 %handle) {
entry:
  call void @nolang.raw_unsupported_errno()
  ret i64 -1
}
define i32 @nolang.raw_fsync(i64 %handle) {
entry:
  call void @nolang.raw_unsupported_errno()
  ret i32 -1
}
define i32 @nolang.raw_fdatasync(i64 %handle) {
entry:
  call void @nolang.raw_unsupported_errno()
  ret i32 -1
}
define i32 @nolang.raw_sync_dir(i8* %path) {
entry:
  call void @nolang.raw_unsupported_errno()
  ret i32 -1
}
define i32 @nolang.raw_lock_range(i64 %handle, i64 %start, i64 %length, i64 %mode, i1 %nonblocking) {
entry:
  call void @nolang.raw_unsupported_errno()
  ret i32 -1
}
define i32 @nolang.raw_unlock_range(i64 %handle, i64 %start, i64 %length) {
entry:
  call void @nolang.raw_unsupported_errno()
  ret i32 -1
}
`
}
