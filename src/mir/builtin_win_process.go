package mir

import "fmt"

// builtin_win_process.go — Windows subprocess builtins (process.cmd's #{win-*} body).
//
// The nine win-* ForwardFunc builtins declared in std/process.no and registered
// in builtin/process.go had no MIR lowering, so notools / noagent died at
// codegen with "unsupported builtin win-create-pipe" once fs.list-dir's win-find
// gap was closed. The heavy Win32 sequences (CreatePipe / CreateProcessA /
// ReadFile / WriteFile / WaitForSingleObject / GetExitCodeProcess / CloseHandle /
// TerminateProcess / GetStdHandle) live as `nolang.win_*` shims in the
// target-gated prelude (builtin_win_shims.go); each emitter here just marshals
// arguments, calls the shim, and writes the results into the builtin's out-slots.
//
// Handles are i64; str arguments use str_cstr / strHeaderOf so binary payloads
// (write data, read output) stay byte-exact rather than NUL-terminated.

// win-create-pipe: () -> (handles i64)
func (c *codegen) emitBuiltinWinCreatePipe(inst *Inst) error {
	c.decl("declare i64 @nolang.win_create_pipe()")
	r := c.treg("wcp.r")
	c.sb.WriteString(fmt.Sprintf("  %s = call i64 @nolang.win_create_pipe()\n", r))
	return c.storeResult(inst, 0, r, "i64")
}

// win-close-handle: (handle i64) -> (ok bool)
func (c *codegen) emitBuiltinWinCloseHandle(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("win-close-handle: needs a handle argument")
	}
	c.decl("declare i32 @nolang.win_close_handle(i64)")
	_, h := c.loadVal(inst.Args[0])
	r := c.treg("wch.r")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @nolang.win_close_handle(i64 %s)\n", r, h))
	ok := c.treg("wch.ok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", ok, r))
	return c.storeResult(inst, 0, ok, "i1")
}

// win-write-pipe: (handle i64, data str) -> (written i64)
func (c *codegen) emitBuiltinWinWritePipe(inst *Inst) error {
	if len(inst.Args) < 2 {
		return fmt.Errorf("win-write-pipe: needs (handle, data)")
	}
	c.decl("declare i64 @nolang.win_write_pipe(i64, i8*, i64)")
	_, h := c.loadVal(inst.Args[0])
	data, blen := c.strHeaderOf(inst.Args[1])
	r := c.treg("wwp.r")
	c.sb.WriteString(fmt.Sprintf("  %s = call i64 @nolang.win_write_pipe(i64 %s, i8* %s, i64 %s)\n", r, h, data, blen))
	return c.storeResult(inst, 0, r, "i64")
}

// win-read-pipe: (handle i64, max-bytes i64) -> (data str, n i64)
func (c *codegen) emitBuiltinWinReadPipe(inst *Inst) error {
	if len(inst.Args) < 2 {
		return fmt.Errorf("win-read-pipe: needs (handle, max-bytes)")
	}
	c.decl("declare i64 @nolang.win_read_pipe(i64, i8*, i64)")
	_, h := c.loadVal(inst.Args[0])
	_, maxv := c.loadVal(inst.Args[1])
	buf := c.treg("wrp.buf")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca i8, i64 %s, align 8\n", buf, maxv))
	n := c.treg("wrp.n")
	c.sb.WriteString(fmt.Sprintf("  %s = call i64 @nolang.win_read_pipe(i64 %s, i8* %s, i64 %s)\n", n, h, buf, maxv))
	// data: bytes read, clamped to 0 when n<0 (error) — storeBufStrLenResult does that.
	if err := c.storeBufStrLenResult(inst, 0, buf, n, "i64", false); err != nil {
		return err
	}
	return c.storeResult(inst, 1, n, "i64")
}

// win-get-std-handle: (which i64) -> (handle i64)
func (c *codegen) emitBuiltinWinGetStdHandle(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("win-get-std-handle: needs a which argument")
	}
	c.decl("declare i64 @nolang.win_get_std_handle(i64)")
	_, w := c.loadVal(inst.Args[0])
	r := c.treg("wgsh.r")
	c.sb.WriteString(fmt.Sprintf("  %s = call i64 @nolang.win_get_std_handle(i64 %s)\n", r, w))
	return c.storeResult(inst, 0, r, "i64")
}

// win-wait-process: (proc-handle i64, timeout-ms i64) -> (status i64)
func (c *codegen) emitBuiltinWinWaitProcess(inst *Inst) error {
	if len(inst.Args) < 2 {
		return fmt.Errorf("win-wait-process: needs (proc-handle, timeout-ms)")
	}
	c.decl("declare i64 @nolang.win_wait_process(i64, i64)")
	_, h := c.loadVal(inst.Args[0])
	_, ms := c.loadVal(inst.Args[1])
	r := c.treg("wwp.r")
	c.sb.WriteString(fmt.Sprintf("  %s = call i64 @nolang.win_wait_process(i64 %s, i64 %s)\n", r, h, ms))
	return c.storeResult(inst, 0, r, "i64")
}

// win-get-exit-code: (proc-handle i64) -> (exit-code i64, ok bool)
// ok=false when the call failed or the process is still running (STILL_ACTIVE=259).
func (c *codegen) emitBuiltinWinGetExitCode(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("win-get-exit-code: needs a proc-handle argument")
	}
	c.decl("declare i32 @nolang.win_get_exit_code(i64, i32*)")
	_, h := c.loadVal(inst.Args[0])
	outp := c.treg("wgec.out")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca i32, align 4\n", outp))
	r := c.treg("wgec.r")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @nolang.win_get_exit_code(i64 %s, i32* %s)\n", r, h, outp))
	code32 := c.treg("wgec.code")
	c.sb.WriteString(fmt.Sprintf("  %s = load i32, i32* %s\n", code32, outp))
	code64 := c.treg("wgec.code64")
	c.sb.WriteString(fmt.Sprintf("  %s = zext i32 %s to i64\n", code64, code32))
	got := c.treg("wgec.got")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", got, r))
	active := c.treg("wgec.active")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 259\n", active, code32))
	notActive := c.treg("wgec.notact")
	c.sb.WriteString(fmt.Sprintf("  %s = xor i1 %s, true\n", notActive, active))
	ok := c.treg("wgec.ok")
	c.sb.WriteString(fmt.Sprintf("  %s = and i1 %s, %s\n", ok, got, notActive))
	if err := c.storeResult(inst, 0, code64, "i64"); err != nil {
		return err
	}
	return c.storeResult(inst, 1, ok, "i1")
}

// win-terminate-process: (proc-handle i64, exit-code i64) -> (ok bool)
func (c *codegen) emitBuiltinWinTerminateProcess(inst *Inst) error {
	if len(inst.Args) < 2 {
		return fmt.Errorf("win-terminate-process: needs (proc-handle, exit-code)")
	}
	c.decl("declare i32 @nolang.win_terminate_process(i64, i32)")
	_, h := c.loadVal(inst.Args[0])
	_, code := c.loadVal(inst.Args[1])
	code32 := c.treg("wtp.c32")
	c.sb.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i32\n", code32, code))
	r := c.treg("wtp.r")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @nolang.win_terminate_process(i64 %s, i32 %s)\n", r, h, code32))
	ok := c.treg("wtp.ok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", ok, r))
	return c.storeResult(inst, 0, ok, "i1")
}

// win-create-process: (cmdline str, dir str, stdin-handle i64, stdout-handle i64, stderr-handle i64)
//
//	-> (proc-handle i64, status i64)   status: >0 = ok, 0 = fail
func (c *codegen) emitBuiltinWinCreateProcess(inst *Inst) error {
	if len(inst.Args) < 5 {
		return fmt.Errorf("win-create-process: needs (cmdline, dir, stdin, stdout, stderr)")
	}
	c.decl("declare i32 @nolang.win_create_process(i8*, i8*, i64, i64, i64, i64*)")
	cl := c.cstrOf(inst.Args[0])
	if cl == "" {
		return fmt.Errorf("win-create-process: cannot marshal cmdline as C string")
	}
	dir := c.cstrOf(inst.Args[1])
	if dir == "" {
		return fmt.Errorf("win-create-process: cannot marshal dir as C string")
	}
	_, si := c.loadVal(inst.Args[2])
	_, so := c.loadVal(inst.Args[3])
	_, se := c.loadVal(inst.Args[4])
	procOut := c.treg("wproc.out")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca i64, align 8\n", procOut))
	r := c.treg("wproc.r")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @nolang.win_create_process(i8* %s, i8* %s, i64 %s, i64 %s, i64 %s, i64* %s)\n",
		r, cl, dir, si, so, se, procOut))
	c.sb.WriteString(fmt.Sprintf("  call void @nolang_free(i8* %s)\n", cl))
	c.sb.WriteString(fmt.Sprintf("  call void @nolang_free(i8* %s)\n", dir))
	proc := c.treg("wproc.h")
	c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", proc, procOut))
	status := c.treg("wproc.s")
	c.sb.WriteString(fmt.Sprintf("  %s = zext i32 %s to i64\n", status, r))
	if err := c.storeResult(inst, 0, proc, "i64"); err != nil {
		return err
	}
	return c.storeResult(inst, 1, status, "i64")
}
