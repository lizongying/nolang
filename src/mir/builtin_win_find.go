package mir

import "fmt"

// builtin_win_find.go — fs.list-dir on Windows (FindFirstFileA family).
//
// Windows has no opendir/readdir: a directory is enumerated through the Win32
// FindFirstFileA/FindNextFileA/FindClose trio. The stdlib `os.win-find-*`
// builtins (declared in std/os.no, registered in builtin/os.go with ForwardFunc
// win-find-first-file/…) carry that contract, but the MIR backend never lowered
// them, so any program reaching `fs.list-dir` on a win-amd64/win-arm64 target
// died at codegen with "unsupported builtin os.win-find-first-file". The
// `#{win-amd64, win-arm64}` list-dir body is compiled ONLY for Windows targets,
// which is why linux/darwin cross builds succeeded and only Windows failed.
//
// The three builtins share state through an opaque i64 `bufPtr` that the caller
// threads first→next→close. It points at a heap block laid out as:
//
//	@0   i64              hFind  (the FindFirstFileA HANDLE; INVALID = -1)
//	@8   WIN32_FIND_DATAA fd      (filled by first/next; cFileName @ +44, len 260)
//
// WIN32_FIND_DATAA (winnt.h, ANSI variant) is all 4-byte-or-char fields, so the
// offsets are exact (dwFileAttributes@0 … cFileName@44 … cAlternateFileName@304).
// The block is over-allocated to 520 bytes (8 + a 512-byte fd region) so the
// 318-byte struct plus slack always fits. All three entry points are kernel32,
// which zig's mingw target links by default.

// findDataCFileNameOff is the byte offset of WIN32_FIND_DATAA.cFileName from the
// start of the struct (which itself sits at bufPtr+8).
const findDataCFileNameOff = 44

// win-find-first-file: (path str) -> (bufptr i64)
// Opens a search over `path` (the caller has already appended "\*"). Returns the
// heap block pointer, or 0 on failure (INVALID_HANDLE_VALUE or out-of-memory).
func (c *codegen) emitBuiltinWinFindFirstFile(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("win-find-first-file: needs a path argument")
	}
	c.decl("declare i8* @malloc(i64)")
	c.decl("declare i64 @FindFirstFileA(i8*, i8*)")

	p := c.cstrOf(inst.Args[0])
	if p == "" {
		return fmt.Errorf("win-find-first-file: cannot marshal path as C string")
	}

	b := c.treg("wff.b")
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 520)\n", b))
	notnull := c.treg("wff.nn")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ne i8* %s, null\n", notnull, b))

	doL := c.label("wff.do")
	zeroL := c.label("wff.zero")
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", notnull, doL, zeroL))

	// do: FindFirstFileA(path, &fd) with fd at b+8; keep the handle at b+0.
	c.sb.WriteString(fmt.Sprintf("%s:\n", doL))
	fdPtr := c.treg("wff.fd")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 8\n", fdPtr, b))
	h := c.treg("wff.h")
	c.sb.WriteString(fmt.Sprintf("  %s = call i64 @FindFirstFileA(i8* %s, i8* %s)\n", h, p, fdPtr))
	c.sb.WriteString(fmt.Sprintf("  call void @nolang_free(i8* %s)\n", p))
	hSlot := c.treg("wff.hs")
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to i64*\n", hSlot, b))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", h, hSlot))
	// INVALID_HANDLE_VALUE == (HANDLE)-1 → treat as failure.
	valid := c.treg("wff.valid")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ne i64 %s, -1\n", valid, h))
	keepL := c.label("wff.keep")
	freeL := c.label("wff.free")
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", valid, keepL, freeL))

	c.sb.WriteString(fmt.Sprintf("%s:\n", keepL))
	bp := c.treg("wff.bp")
	c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", bp, b))
	endL := c.label("wff.end")
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", endL))

	c.sb.WriteString(fmt.Sprintf("%s:\n", freeL))
	c.sb.WriteString(fmt.Sprintf("  call void @nolang_free(i8* %s)\n", b))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", endL))

	c.sb.WriteString(fmt.Sprintf("%s:\n", zeroL))
	c.sb.WriteString(fmt.Sprintf("  call void @nolang_free(i8* %s)\n", p))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", endL))

	c.sb.WriteString(fmt.Sprintf("%s:\n", endL))
	res := c.treg("wff.res")
	c.sb.WriteString(fmt.Sprintf("  %s = phi i64 [ %s, %%%s ], [ 0, %%%s ], [ 0, %%%s ]\n",
		res, bp, keepL, freeL, zeroL))
	return c.storeResult(inst, 0, res, "i64")
}

// win-find-next-file: (bufptr i64) -> (name str, ok bool)
// Pulls the next entry into the block's WIN32_FIND_DATAA and returns its
// cFileName. ok=false (and the empty name) once enumeration is exhausted or the
// handle is invalid/null.
func (c *codegen) emitBuiltinWinFindNextFile(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("win-find-next-file: needs a bufptr argument")
	}
	c.decl("declare i32 @FindNextFileA(i64, i8*)")

	_, v := c.loadVal(inst.Args[0])
	b := c.treg("wfn.b")
	c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", b, v))
	notnull := c.treg("wfn.nn")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ne i8* %s, null\n", notnull, b))

	doL := c.label("wfn.do")
	nilL := c.label("wfn.nil")
	endL := c.label("wfn.end")
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", notnull, doL, nilL))

	c.sb.WriteString(fmt.Sprintf("%s:\n", doL))
	hSlot := c.treg("wfn.hs")
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to i64*\n", hSlot, b))
	h := c.treg("wfn.h")
	c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", h, hSlot))
	fdPtr := c.treg("wfn.fd")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 8\n", fdPtr, b))
	r := c.treg("wfn.r")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @FindNextFileA(i64 %s, i8* %s)\n", r, h, fdPtr))
	okc := c.treg("wfn.ok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", okc, r))
	namePtr := c.treg("wfn.name")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %d\n", namePtr, fdPtr, findDataCFileNameOff))
	// When exhausted cFileName is stale — read the empty string instead.
	safe := c.treg("wfn.safe")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i8* %s, i8* %s\n", safe, okc, namePtr, c.emptyStrGlobal()))
	if err := c.storeBufStrResult(inst, 0, safe); err != nil {
		return err
	}
	if err := c.storeResult(inst, 1, okc, "i1"); err != nil {
		return err
	}
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", endL))

	c.sb.WriteString(fmt.Sprintf("%s:\n", nilL))
	f := c.treg("wfn.f")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ne i8* %s, null\n", f, b))
	if err := c.storeBufStrResult(inst, 0, c.emptyStrGlobal()); err != nil {
		return err
	}
	if err := c.storeResult(inst, 1, f, "i1"); err != nil {
		return err
	}
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", endL))

	c.sb.WriteString(fmt.Sprintf("%s:\n", endL))
	return nil
}

// win-find-close: (bufptr i64) -> (ok bool)
// Closes the search handle and frees the heap block. ok=false on a bad handle;
// a null bufPtr is a no-op returning false (no double-free).
func (c *codegen) emitBuiltinWinFindClose(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("win-find-close: needs a bufptr argument")
	}
	c.decl("declare i32 @FindClose(i64)")

	_, v := c.loadVal(inst.Args[0])
	b := c.treg("wfc.b")
	c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", b, v))
	notnull := c.treg("wfc.nn")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ne i8* %s, null\n", notnull, b))

	doL := c.label("wfc.do")
	nilL := c.label("wfc.nil")
	endL := c.label("wfc.end")
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", notnull, doL, nilL))

	c.sb.WriteString(fmt.Sprintf("%s:\n", doL))
	hSlot := c.treg("wfc.hs")
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to i64*\n", hSlot, b))
	h := c.treg("wfc.h")
	c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", h, hSlot))
	r := c.treg("wfc.r")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @FindClose(i64 %s)\n", r, h))
	c.sb.WriteString(fmt.Sprintf("  call void @nolang_free(i8* %s)\n", b))
	okc := c.treg("wfc.ok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", okc, r))
	if err := c.storeResult(inst, 0, okc, "i1"); err != nil {
		return err
	}
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", endL))

	c.sb.WriteString(fmt.Sprintf("%s:\n", nilL))
	f := c.treg("wfc.f")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ne i8* %s, null\n", f, b))
	if err := c.storeResult(inst, 0, f, "i1"); err != nil {
		return err
	}
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", endL))

	c.sb.WriteString(fmt.Sprintf("%s:\n", endL))
	return nil
}
