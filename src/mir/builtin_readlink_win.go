package mir

import "fmt"

// builtin_readlink_win.go — fs.readlink on Windows.
//
// Windows has no readlink(2): a symlink/junction is an NTFS *reparse point*,
// read back only through CreateFile(FILE_FLAG_OPEN_REPARSE_POINT) +
// DeviceIoControl(FSCTL_GET_REPARSE_POINT) into a REPARSE_DATA_BUFFER. The
// target name lives in the buffer's PrintName field as UTF-16, converted to
// UTF-8 with WideCharToMultiByte. All four entry points are in kernel32, which
// zig's mingw target links by default.
//
// Only the (path str) -> (target str, ok bool) shape is handled here; every
// other platform lowers through the generic C-call table (forward_call.go).
// The struct layout / constants match winnt.h / winioctl.h shipped with zig's
// libc (verified 0.16.0):
//
//	typedef struct _REPARSE_DATA_BUFFER {
//	  ULONG  ReparseTag;                                   // @0
//	  USHORT ReparseDataLength;                            // @4
//	  USHORT Reserved;                                     // @6
//	  union {
//	    struct { USHORT SubstituteNameOffset;              // @8
//	             USHORT SubstituteNameLength;              // @10
//	             USHORT PrintNameOffset;                   // @12
//	             USHORT PrintNameLength;                   // @14
//	             ULONG  Flags;                             // @16
//	             WCHAR  PathBuffer[1]; } SymbolicLinkReparseBuffer;
//	    ... MountPointReparseBuffer shares the same offsets ...
//	  };                                                    // PathBuffer @20
//	} REPARSE_DATA_BUFFER, *PREPARSE_DATA_BUFFER;
//
// We read PrintName (no "\??\" device-path prefix) which is the user-facing
// target and falls out cleanly for both IO_REPARSE_TAG_SYMLINK and
// IO_REPARSE_TAG_MOUNT_POINT.
//
//	IO_REPARSE_TAG_SYMLINK     = 0xA000000C  (signed i32 -1610612724)
//	IO_REPARSE_TAG_MOUNT_POINT = 0xA0000003  (signed i32 -1610612733)
//	FSCTL_GET_REPARSE_POINT    = 0x900A8     (589992)
//	FILE_FLAG_OPEN_REPARSE_POINT | FILE_FLAG_BACKUP_SEMANTICS = 0x02200000 (35651584)
//	OPEN_EXISTING = 3, FILE_SHARE_READ|WRITE|DELETE = 7, CP_UTF8 = 65001

func (c *codegen) emitBuiltinReadlinkWindows(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("readlink: needs a path argument")
	}
	// Declare the Win32 entry points (deduped by c.decl).
	c.decl("declare i64 @CreateFileA(i8*, i32, i32, i8*, i32, i32, i8*)")
	c.decl("declare i32 @DeviceIoControl(i64, i32, i8*, i32, i8*, i32, i32*, i8*)")
	c.decl("declare i32 @CloseHandle(i64)")
	c.decl("declare i32 @WideCharToMultiByte(i32, i32, i8*, i32, i8*, i32, i8*, i8*)")

	p := c.cstrOf(inst.Args[0])
	if p == "" {
		return fmt.Errorf("readlink: cannot marshal path as C string")
	}

	// out: UTF-8 result buffer. Zeroed so a failure path yields the empty
	// string (storeBufStrResult runs strlen on it).
	out := c.treg("rlw.out")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca i8, i64 4096, align 8\n", out))
	c.sb.WriteString(fmt.Sprintf("  store i8 0, i8* %s\n", out))
	// rbuf: REPARSE_DATA_BUFFER scratch (MAXIMUM_REPARSE_DATA_BUFFER_SIZE = 16384).
	rbuf := c.treg("rlw.rbuf")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca i8, i64 16384, align 8\n", rbuf))
	// retp: bytes-returned out param (never read, just required).
	retp := c.treg("rlw.retp")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca i32, align 4\n", retp))

	// h = CreateFileA(path, 0, 7, NULL, OPEN_EXISTING, 0x02200000, NULL)
	h := c.treg("rlw.h")
	c.sb.WriteString(fmt.Sprintf("  %s = call i64 @CreateFileA(i8* %s, i32 0, i32 7, i8* null, i32 3, i32 35651584, i8* null)\n", h, p))
	c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", p))
	inv := c.treg("rlw.inv")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i64 %s, -1\n", inv, h))

	failL := c.label("rlw.fail")
	docallL := c.label("rlw.docall")
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", inv, failL, docallL))

	c.sb.WriteString(fmt.Sprintf("%s:\n", docallL))
	// ok = DeviceIoControl(h, FSCTL_GET_REPARSE_POINT, NULL, 0, rbuf, 16384, &ret, NULL)
	okd := c.treg("rlw.okd")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @DeviceIoControl(i64 %s, i32 589992, i8* null, i32 0, i8* %s, i32 16384, i32* %s, i8* null)\n", okd, h, rbuf, retp))
	c.sb.WriteString(fmt.Sprintf("  call i32 @CloseHandle(i64 %s)\n", h))
	okc := c.treg("rlw.okc")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", okc, okd))

	tagL := c.label("rlw.tag")
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", okc, tagL, failL))

	c.sb.WriteString(fmt.Sprintf("%s:\n", tagL))
	// tag = *((i32*)(rbuf+0))
	tg0 := c.treg("rlw.tg0")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 0\n", tg0, rbuf))
	tg1 := c.treg("rlw.tg1")
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to i32*\n", tg1, tg0))
	tag := c.treg("rlw.tag")
	c.sb.WriteString(fmt.Sprintf("  %s = load i32, i32* %s\n", tag, tg1))
	isSym := c.treg("rlw.issym")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, -1610612724\n", isSym, tag))
	isMnt := c.treg("rlw.ismnt")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, -1610612733\n", isMnt, tag))
	isReparse := c.treg("rlw.isrep")
	c.sb.WriteString(fmt.Sprintf("  %s = or i1 %s, %s\n", isReparse, isSym, isMnt))

	convL := c.label("rlw.conv")
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", isReparse, convL, failL))

	c.sb.WriteString(fmt.Sprintf("%s:\n", convL))
	// PrintNameOffset @12 (i16), PrintNameLength @14 (i16)
	po0 := c.treg("rlw.po0")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 12\n", po0, rbuf))
	po1 := c.treg("rlw.po1")
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to i16*\n", po1, po0))
	poff := c.treg("rlw.poff")
	c.sb.WriteString(fmt.Sprintf("  %s = load i16, i16* %s\n", poff, po1))
	pl0 := c.treg("rlw.pl0")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 14\n", pl0, rbuf))
	pl1 := c.treg("rlw.pl1")
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to i16*\n", pl1, pl0))
	plen := c.treg("rlw.plen")
	c.sb.WriteString(fmt.Sprintf("  %s = load i16, i16* %s\n", plen, pl1))
	// wptr = rbuf + 20 + poff ; wlen (WCHAR count) = plen / 2
	poff64 := c.treg("rlw.poff64")
	c.sb.WriteString(fmt.Sprintf("  %s = zext i16 %s to i64\n", poff64, poff))
	wbase := c.treg("rlw.wbase")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 20\n", wbase, rbuf))
	wptr := c.treg("rlw.wptr")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", wptr, wbase, poff64))
	plen64 := c.treg("rlw.plen64")
	c.sb.WriteString(fmt.Sprintf("  %s = zext i16 %s to i64\n", plen64, plen))
	half := c.treg("rlw.half")
	c.sb.WriteString(fmt.Sprintf("  %s = lshr i64 %s, 1\n", half, plen64))
	wlen32 := c.treg("rlw.wlen")
	c.sb.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i32\n", wlen32, half))
	// n = WideCharToMultiByte(CP_UTF8, 0, wptr, wlen, out, 4095, NULL, NULL)
	n := c.treg("rlw.n")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @WideCharToMultiByte(i32 65001, i32 0, i8* %s, i32 %s, i8* %s, i32 4095, i8* null, i8* null)\n", n, wptr, wlen32, out))
	npos := c.treg("rlw.npos")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sgt i32 %s, 0\n", npos, n))

	sucL := c.label("rlw.suc")
	mergeL := c.label("rlw.merge")
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", npos, sucL, failL))

	c.sb.WriteString(fmt.Sprintf("%s:\n", sucL))
	// NUL-terminate at n so strlen/strlen-based copy is exact.
	n64 := c.treg("rlw.n64")
	c.sb.WriteString(fmt.Sprintf("  %s = sext i32 %s to i64\n", n64, n))
	ng := c.treg("rlw.ng")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", ng, out, n64))
	c.sb.WriteString(fmt.Sprintf("  store i8 0, i8* %s\n", ng))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", mergeL))

	c.sb.WriteString(fmt.Sprintf("%s:\n", failL))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", mergeL))

	// Merge: target is `out` (empty on the fail path), ok is the phi.
	c.sb.WriteString(fmt.Sprintf("%s:\n", mergeL))
	okphi := c.treg("rlw.okphi")
	c.sb.WriteString(fmt.Sprintf("  %s = phi i1 [ true, %%%s ], [ false, %%%s ]\n", okphi, sucL, failL))
	if err := c.storeBufStrResult(inst, 0, out); err != nil {
		return err
	}
	return c.storeResult(inst, 1, okphi, "i1")
}
