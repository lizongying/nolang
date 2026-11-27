package mir

// rawWindowsShimsIR is appended to the target-gated Windows prelude. Raw file
// handles remain i64 values in the existing fs.fd newtype. Files are opened with
// FILE_FLAG_OVERLAPPED so positional I/O never depends on or mutates the current
// file pointer.
const rawWindowsShimsIR = `
; --- Raw VFS helpers (Win64 / Windows ARM64) -------------------------------
declare i32 @FlushFileBuffers(i64)
declare i32 @GetFileSizeEx(i64, i8*)
declare i64 @CreateEventA(i8*, i32, i32, i8*)
declare i32 @GetOverlappedResult(i64, i8*, i32*, i32)

define internal void @nolang.win_raw_error(i32 %code) {
entry:
  %nofile2 = icmp eq i32 %code, 2
  %nofile3 = icmp eq i32 %code, 3
  %nofile = or i1 %nofile2, %nofile3
  %denied5 = icmp eq i32 %code, 5
  %share32 = icmp eq i32 %code, 32
  %denied = or i1 %denied5, %share32
  %lock33 = icmp eq i32 %code, 33
  %exists80 = icmp eq i32 %code, 80
  %full112 = icmp eq i32 %code, 112
  %many4 = icmp eq i32 %code, 4
  %mem8 = icmp eq i32 %code, 8
  %invalid87 = icmp eq i32 %code, 87
  %name123 = icmp eq i32 %code, 123
  %badname = or i1 %invalid87, %name123
  %notdir145 = icmp eq i32 %code, 145
  %isdir267 = icmp eq i32 %code, 267
  %long206 = icmp eq i32 %code, 206
  %e1 = select i1 %nofile, i32 2, i32 22
  %e2 = select i1 %denied, i32 13, i32 %e1
  %e3 = select i1 %lock33, i32 11, i32 %e2
  %e4 = select i1 %exists80, i32 17, i32 %e3
  %e5 = select i1 %full112, i32 28, i32 %e4
  %e6 = select i1 %many4, i32 24, i32 %e5
  %e7 = select i1 %mem8, i32 12, i32 %e6
  %e8 = select i1 %badname, i32 22, i32 %e7
  %e9 = select i1 %notdir145, i32 20, i32 %e8
  %e10 = select i1 %isdir267, i32 21, i32 %e9
  %e11 = select i1 %long206, i32 36, i32 %e10
  call void @nolang.win_seterrno(i32 %e11)
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
  %good1 = or i1 %isread, %iswrite
  %goodaccess = or i1 %good1, %isrw
  %nogoodaccess = xor i1 %goodaccess, true
  %badaccess = or i1 %hasunknown, %nogoodaccess
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
  %bad1 = or i1 %badaccess, %createbad
  %bad2 = or i1 %bad1, %exclbad
  %bad3 = or i1 %bad2, %truncbad
  %modeneg = icmp slt i64 %mode, 0
  %modebig = icmp ugt i64 %mode, 4095
  %badmode = or i1 %modeneg, %modebig
  %bad = or i1 %bad3, %badmode
  br i1 %bad, label %einval, label %translate
einval:
  call void @nolang.win_seterrno(i32 22)
  ret i64 -1
translate:
  %rd = and i64 %flags, 1
  %wr = and i64 %flags, 2
  %hasrd = icmp ne i64 %rd, 0
  %haswr = icmp ne i64 %wr, 0
  %ar = select i1 %hasrd, i64 2147483648, i64 0
  %aw = select i1 %haswr, i64 1073741824, i64 0
  %desired = or i64 %ar, %aw
  %dispnew = select i1 %hasexcl, i64 1, i64 4
  %disp = select i1 %hascreate, i64 %dispnew, i64 3
  %attrs = or i64 128, 1073741824
  %desired32 = trunc i64 %desired to i32
  %disp32 = trunc i64 %disp to i32
  %attrs32 = trunc i64 %attrs to i32
  %h = call i64 @CreateFileA(i8* %path, i32 %desired32, i32 7, i8* null, i32 %disp32, i32 %attrs32, i8* null)
  %invalid = icmp eq i64 %h, -1
  br i1 %invalid, label %openerr, label %truncate
openerr:
  %oe = call i32 @GetLastError()
  call void @nolang.win_raw_error(i32 %oe)
  ret i64 -1
truncate:
  br i1 %hastrunc, label %do_truncate, label %done
do_truncate:
  %sp = call i32 @SetFilePointerEx(i64 %h, i64 0, i8* null, i32 0)
  %spok = icmp ne i32 %sp, 0
  br i1 %spok, label %setend, label %seekerr
seekerr:
  %spe = call i32 @GetLastError()
  call i32 @CloseHandle(i64 %h)
  call void @nolang.win_raw_error(i32 %spe)
  ret i64 -1
setend:
  %se = call i32 @SetEndOfFile(i64 %h)
  %seok = icmp ne i32 %se, 0
  %te = call i32 @GetLastError()
  br i1 %seok, label %done, label %truncerr
truncerr:
  call i32 @CloseHandle(i64 %h)
  call void @nolang.win_raw_error(i32 %te)
  ret i64 -1
done:
  ret i64 %h
}

define i32 @nolang.raw_close(i64 %handle) {
entry:
  %zero = icmp eq i64 %handle, 0
  %invalid = icmp eq i64 %handle, -1
  %bad = or i1 %zero, %invalid
  br i1 %bad, label %ebadf, label %close
ebadf:
  call void @nolang.win_seterrno(i32 9)
  ret i32 -1
close:
  %r = call i32 @CloseHandle(i64 %handle)
  %ok = icmp ne i32 %r, 0
  br i1 %ok, label %done, label %fail
done:
  ret i32 0
fail:
  %e = call i32 @GetLastError()
  call void @nolang.win_raw_error(i32 %e)
  ret i32 -1
}

define i64 @nolang.raw_seek(i64 %handle, i64 %offset, i64 %whence) {
entry:
  %zero = icmp eq i64 %handle, 0
  %invalid = icmp eq i64 %handle, -1
  %badhandle = or i1 %zero, %invalid
  %w0 = icmp eq i64 %whence, 0
  %w1 = icmp eq i64 %whence, 1
  %w2 = icmp eq i64 %whence, 2
  %w01 = or i1 %w0, %w1
  %goodwhence = or i1 %w01, %w2
  %badwhence = xor i1 %goodwhence, true
  %bad = or i1 %badhandle, %badwhence
  br i1 %bad, label %einval, label %seek
einval:
  call void @nolang.win_seterrno(i32 22)
  ret i64 -1
seek:
  %pos = alloca i64, align 8
  %pp = bitcast i64* %pos to i8*
  %wh = trunc i64 %whence to i32
  %r = call i32 @SetFilePointerEx(i64 %handle, i64 %offset, i8* %pp, i32 %wh)
  %ok = icmp ne i32 %r, 0
  br i1 %ok, label %done, label %fail
done:
  %v = load i64, i64* %pos
  ret i64 %v
fail:
  %e = call i32 @GetLastError()
  call void @nolang.win_raw_error(i32 %e)
  ret i64 -1
}

define internal i64 @nolang.win_raw_io(i64 %handle, i8* %buf, i64 %count, i64 %offset, i64 %length, i1 %writing) {
entry:
  %zero = icmp eq i64 %handle, 0
  %invalid = icmp eq i64 %handle, -1
  %badhandle = or i1 %zero, %invalid
  %negcount = icmp slt i64 %count, 0
  %negoffset = icmp slt i64 %offset, 0
  %over = icmp ugt i64 %count, %length
  %huge = icmp ugt i64 %count, 4294967295
  %badcount = or i1 %negcount, %negoffset
  %badcount2 = or i1 %badcount, %over
  %badcount3 = or i1 %badcount2, %huge
  %bad = or i1 %badhandle, %badcount3
  br i1 %bad, label %einval, label %start
einval:
  call void @nolang.win_seterrno(i32 22)
  ret i64 -1
start:
  %ov = alloca [32 x i8], align 8
  %ovp = bitcast [32 x i8]* %ov to i8*
  call void @llvm.memset.p0i8.i64(i8* %ovp, i8 0, i64 32, i1 false)
  %op = getelementptr i8, i8* %ovp, i64 16
  %op64 = bitcast i8* %op to i64*
  store i64 %offset, i64* %op64
  %ev = call i64 @CreateEventA(i8* null, i32 1, i32 0, i8* null)
  %noev = icmp eq i64 %ev, 0
  br i1 %noev, label %eventerr, label %event
event:
  %hp = getelementptr i8, i8* %ovp, i64 24
  %hp64 = bitcast i8* %hp to i64*
  store i64 %ev, i64* %hp64
  %n = alloca i32, align 4
  store i32 0, i32* %n
  %count32 = trunc i64 %count to i32
  br i1 %writing, label %write, label %read
write:
  %wr = call i32 @WriteFile(i64 %handle, i8* %buf, i32 %count32, i32* %n, i8* %ovp)
  br label %called
read:
  %rr = call i32 @ReadFile(i64 %handle, i8* %buf, i32 %count32, i32* %n, i8* %ovp)
  br label %called
called:
  %r = phi i32 [ %wr, %write ], [ %rr, %read ]
  %ok = icmp ne i32 %r, 0
  %le = call i32 @GetLastError()
  br i1 %ok, label %immediate, label %pending
immediate:
  call i32 @CloseHandle(i64 %ev)
  %nv = load i32, i32* %n
  %n64 = zext i32 %nv to i64
  ret i64 %n64
pending:
  %ispending = icmp eq i32 %le, 997
  br i1 %ispending, label %wait, label %fail
wait:
  call i32 @WaitForSingleObject(i64 %ev, i32 -1)
  %gr = call i32 @GetOverlappedResult(i64 %handle, i8* %ovp, i32* %n, i32 1)
  %gok = icmp ne i32 %gr, 0
  %gle = call i32 @GetLastError()
  call i32 @CloseHandle(i64 %ev)
  br i1 %gok, label %complete, label %fail
complete:
  %nv2 = load i32, i32* %n
  %n642 = zext i32 %nv2 to i64
  ret i64 %n642
fail:
  %err = phi i32 [ %le, %pending ], [ %gle, %wait ]
  call i32 @CloseHandle(i64 %ev)
  call void @nolang.win_raw_error(i32 %err)
  ret i64 -1
eventerr:
  %ee = call i32 @GetLastError()
  call void @nolang.win_raw_error(i32 %ee)
  ret i64 -1
}

define i64 @nolang.raw_pread(i64 %handle, i8* %buf, i64 %count, i64 %offset, i64 %length) {
entry:
  %r = call i64 @nolang.win_raw_io(i64 %handle, i8* %buf, i64 %count, i64 %offset, i64 %length, i1 false)
  ret i64 %r
}

define i64 @nolang.raw_pwrite(i64 %handle, i8* %data, i64 %count, i64 %offset, i64 %length) {
entry:
  %r = call i64 @nolang.win_raw_io(i64 %handle, i8* %data, i64 %count, i64 %offset, i64 %length, i1 true)
  ret i64 %r
}

define i32 @nolang.raw_ftruncate(i64 %handle, i64 %length) {
entry:
  %zero = icmp eq i64 %handle, 0
  %invalid = icmp eq i64 %handle, -1
  %badhandle = or i1 %zero, %invalid
  %neglen = icmp slt i64 %length, 0
  %bad = or i1 %badhandle, %neglen
  br i1 %bad, label %einval, label %save
einval:
  call void @nolang.win_seterrno(i32 22)
  ret i32 -1
save:
  %cur = alloca i64, align 8
  %cp = bitcast i64* %cur to i8*
  %sr = call i32 @SetFilePointerEx(i64 %handle, i64 0, i8* %cp, i32 1)
  %sok = icmp ne i32 %sr, 0
  br i1 %sok, label %seek, label %saveerr
saveerr:
  %se0 = call i32 @GetLastError()
  call void @nolang.win_raw_error(i32 %se0)
  ret i32 -1
seek:
  %pos = load i64, i64* %cur
  %mr = call i32 @SetFilePointerEx(i64 %handle, i64 %length, i8* null, i32 0)
  %mok = icmp ne i32 %mr, 0
  br i1 %mok, label %end, label %moverr
moverr:
  %me = call i32 @GetLastError()
  call void @nolang.win_raw_error(i32 %me)
  ret i32 -1
end:
  %er = call i32 @SetEndOfFile(i64 %handle)
  %eok = icmp ne i32 %er, 0
  %ee = call i32 @GetLastError()
  %rr = call i32 @SetFilePointerEx(i64 %handle, i64 %pos, i8* null, i32 0)
  %rok = icmp ne i32 %rr, 0
  %re = call i32 @GetLastError()
  %ok = and i1 %eok, %rok
  %e = select i1 %eok, i32 %re, i32 %ee
  %finale = select i1 %ok, i32 0, i32 %e
  call void @nolang.win_seterrno(i32 %finale)
  %r = select i1 %ok, i32 0, i32 -1
  ret i32 %r
}

define i64 @nolang.raw_fstat_size(i64 %handle) {
entry:
  %zero = icmp eq i64 %handle, 0
  %invalid = icmp eq i64 %handle, -1
  %bad = or i1 %zero, %invalid
  br i1 %bad, label %ebadf, label %size
ebadf:
  call void @nolang.win_seterrno(i32 9)
  ret i64 -1
size:
  %sz = alloca i64, align 8
  %p = bitcast i64* %sz to i8*
  %r = call i32 @GetFileSizeEx(i64 %handle, i8* %p)
  %ok = icmp ne i32 %r, 0
  br i1 %ok, label %done, label %fail
done:
  %v = load i64, i64* %sz
  ret i64 %v
fail:
  %e = call i32 @GetLastError()
  call void @nolang.win_raw_error(i32 %e)
  ret i64 -1
}

define i32 @nolang.raw_fsync(i64 %handle) {
entry:
  %zero = icmp eq i64 %handle, 0
  %invalid = icmp eq i64 %handle, -1
  %bad = or i1 %zero, %invalid
  br i1 %bad, label %ebadf, label %flush
ebadf:
  call void @nolang.win_seterrno(i32 9)
  ret i32 -1
flush:
  %r = call i32 @FlushFileBuffers(i64 %handle)
  %ok = icmp ne i32 %r, 0
  br i1 %ok, label %done, label %fail
done:
  ret i32 0
fail:
  %e = call i32 @GetLastError()
  call void @nolang.win_raw_error(i32 %e)
  ret i32 -1
}

define i32 @nolang.raw_fdatasync(i64 %handle) {
entry:
  %r = call i32 @nolang.raw_fsync(i64 %handle)
  ret i32 %r
}

define i32 @nolang.raw_sync_dir(i8* %path) {
entry:
  call void @nolang.win_seterrno(i32 40)
  ret i32 -1
}

define internal void @nolang.win_raw_set_overlapped(i8* %p, i64 %start) {
entry:
  ; OVERLAPPED on Win64: Offset @16, OffsetHigh @20, hEvent @24.
  %op = getelementptr i8, i8* %p, i64 16
  %op32p = bitcast i8* %op to i32*
  %lo = trunc i64 %start to i32
  store i32 %lo, i32* %op32p
  %hp = getelementptr i8, i8* %p, i64 20
  %hp32p = bitcast i8* %hp to i32*
  %shifted = lshr i64 %start, 32
  %hi = trunc i64 %shifted to i32
  store i32 %hi, i32* %hp32p
  %ep = getelementptr i8, i8* %p, i64 24
  %ep64p = bitcast i8* %ep to i64*
  store i64 0, i64* %ep64p
  ret void
}

define i32 @nolang.raw_lock_range(i64 %handle, i64 %start, i64 %length, i64 %mode, i1 %nonblocking) {
entry:
  %zero = icmp eq i64 %handle, 0
  %invalid = icmp eq i64 %handle, -1
  %badhandle = or i1 %zero, %invalid
  %negstart = icmp slt i64 %start, 0
  %neglen = icmp slt i64 %length, 0
  %shared = icmp eq i64 %mode, 1
  %exclusive = icmp eq i64 %mode, 2
  %goodmode = or i1 %shared, %exclusive
  %badmode = xor i1 %goodmode, true
  %bad1 = or i1 %badhandle, %negstart
  %bad2 = or i1 %bad1, %neglen
  %bad3 = or i1 %bad2, %badmode
  br i1 %bad3, label %einval, label %lock
einval:
  call void @nolang.win_seterrno(i32 22)
  ret i32 -1
lock:
  %ov = alloca [32 x i8], align 8
  %p = bitcast [32 x i8]* %ov to i8*
  call void @llvm.memset.p0i8.i64(i8* %p, i8 0, i64 32, i1 false)
  call void @nolang.win_raw_set_overlapped(i8* %p, i64 %start)
  ; LockFileEx uses a zero low/high pair to mean from start through EOF.
  %lo = trunc i64 %length to i32
  %shifted = lshr i64 %length, 32
  %hi = trunc i64 %shifted to i32
  %f0 = select i1 %exclusive, i32 2, i32 0
  ; Combine fail-immediately with exclusivity rather than replacing it.
  %nbflag = select i1 %nonblocking, i32 1, i32 0
  %f2 = or i32 %f0, %nbflag
  %r = call i32 @LockFileEx(i64 %handle, i32 %f2, i32 0, i32 %lo, i32 %hi, i8* %p)
  %ok = icmp ne i32 %r, 0
  br i1 %ok, label %done, label %fail
done:
  ret i32 0
fail:
  %e = call i32 @GetLastError()
  call void @nolang.win_raw_error(i32 %e)
  ret i32 -1
}

define i32 @nolang.raw_unlock_range(i64 %handle, i64 %start, i64 %length) {
entry:
  %zero = icmp eq i64 %handle, 0
  %invalid = icmp eq i64 %handle, -1
  %badhandle = or i1 %zero, %invalid
  %negstart = icmp slt i64 %start, 0
  %neglen = icmp slt i64 %length, 0
  %bad1 = or i1 %badhandle, %negstart
  %bad = or i1 %bad1, %neglen
  br i1 %bad, label %einval, label %unlock
einval:
  call void @nolang.win_seterrno(i32 22)
  ret i32 -1
unlock:
  %ov = alloca [32 x i8], align 8
  %p = bitcast [32 x i8]* %ov to i8*
  call void @llvm.memset.p0i8.i64(i8* %p, i8 0, i64 32, i1 false)
  call void @nolang.win_raw_set_overlapped(i8* %p, i64 %start)
  ; UnlockFileEx has the same zero-length through-EOF semantics.
  %lo = trunc i64 %length to i32
  %shifted = lshr i64 %length, 32
  %hi = trunc i64 %shifted to i32
  %r = call i32 @UnlockFileEx(i64 %handle, i32 0, i32 %lo, i32 %hi, i8* %p)
  %ok = icmp ne i32 %r, 0
  br i1 %ok, label %done, label %fail
done:
  ret i32 0
fail:
  %e = call i32 @GetLastError()
  call void @nolang.win_raw_error(i32 %e)
  ret i32 -1
}
`
