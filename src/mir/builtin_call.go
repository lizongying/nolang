package mir

// builtin_call.go — MIR lowering for the builtin signature table (src/builtin).
//
// A Nolang call may target something that has NO body in the HIR package: a
// builtin. The builtin table describes four flavours, and this file emits all
// four:
//
//	CLibCall     — a genuine C library call (getenv / getpid / mkdir / ...).
//	               This is the generic path: the C symbol is *declared* on
//	               demand and called with the ABI the table prescribes
//	               (sret-ish owned return via a heap copy, pointer args,
//	               trunc/zext fixups, bool via icmp, ...).
//	LLVMIntrinsic— a scalar LLVM intrinsic (sqrt / fabs / ...).
//	LLVMConv     — a conversion (i64 <-> f64, f32 <-> f64).
//	ForwardFunc  — a named, hand-written lowering (with-cap / with-len /
//	               eprint / str-len / get-arch / ...).
//
// Dependency rule (see MIR_DESIGN §3.6): mir imports builtin (which only
// imports parser) and never build/llvm, so it stays independently testable.

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/lizongying/nolang/builtin"
)

// clibLLVMType maps a builtin.LLVMArgType to its LLVM type spelling.
//
// Differences from the legacy table (build/llvm llvmLLVMType) are deliberate:
//   - LLVMI64Ptr is "i64*" (legacy falls through to "i64", which is wrong for
//     out-parameter style C functions);
//   - LLVMStrPtr is "i8*" (same as legacy).
func clibLLVMType(t builtin.LLVMArgType) string {
	switch t {
	case builtin.LLVMI64:
		return "i64"
	case builtin.LLVMF64:
		return "double"
	case builtin.LLVMI8Ptr, builtin.LLVMStrPtr:
		return "i8*"
	case builtin.LLVMI32:
		return "i32"
	case builtin.LLVMI64Ptr:
		return "i64*"
	}
	return "i64"
}

// clibZero is the LLVM literal used when a declared C argument has no
// corresponding Nolang argument (defensive: keeps the IR well-formed instead of
// silently dropping a parameter and shifting every later one).
func clibZero(t builtin.LLVMArgType) string {
	switch clibLLVMType(t) {
	case "double":
		return "0.0"
	case "i8*", "i64*":
		return "null"
	}
	return "0"
}

// rawWriteFns maps a synthetic raw-write callee (emitted only by the
// named-format lowering) to the LLVM type of its single argument; an empty
// string marks the no-argument newline helpers. See emitCall in codegen.go.
var rawWriteFns = map[string]string{
	"$print_str":  "%str-long",
	"$eprint_str": "%str-long",
	"$print_nl":   "",
	"$eprint_nl":  "",
}

// runtimeFns are declared in the MIR prelude (emitPrelude) and must NOT be
// re-declared by the generic C-forwarder (which would emit a conflicting
// signature, e.g. write with a different fd width, tripping opt's "invalid
// redefinition" error). Skip them in decl().
var runtimeFns = map[string]bool{
	"write":                     true,
	"malloc":                    true,
	"free":                      true,
	"memcmp":                    true,
	"llvm.memcpy.p0i8.p0i8.i64": true,
	"str_free":                  true,
	"str_eq":                    true,
	"vec_free":                  true,
	"str_from_const":            true,
	"str_concat":                true,
	"digits":                    true,
	"print_str":                 true,
	"print_i64":                 true,
	"print_double":              true,
	"print_bool":                true,
	"print_space":               true,
	"print_option":              true,
	"eprint_str":                true,
	"eprint_i64":                true,
	"eprint_double":             true,
	"eprint_bool":               true,
	"eprint_space":              true,
	"eprint_nl":                 true,
	// nolang.* runtime helpers are DEFINED in the prelude (see emitPrelude).
	// The generic C-forwarder must not re-declare them or LLVM rejects the
	// module with "invalid redefinition of function 'nolang.now_ms'".
	"nolang.now_s":    true,
	"nolang.now_ms":   true,
	"nolang.now_us":   true,
	"nolang.now_ns":   true,
	"nolang.sleep_s":  true,
	"nolang.sleep_us": true,
	"nolang.sleep_ns": true,
	"gettimeofday":    true,
	"clock_gettime":   true,
	"usleep":          true,
	"nanosleep":       true,
}

// variadicFixedArgs lists C library functions that take `...` together with the
// number of FIXED (non-variadic) parameters. `open` is the one that actually
// bit: its mode argument is variadic.
var variadicFixedArgs = map[string]int{
	"open":     2, // open(const char*, int, ...)
	"openat":   3, // openat(int, const char*, int, ...)
	"fcntl":    2, // fcntl(int, int, ...)
	"ioctl":    2, // ioctl(int, unsigned long, ...)
	"printf":   1,
	"fprintf":  2,
	"sprintf":  2,
	"snprintf": 3,
	"scanf":    1,
	"sscanf":   2,
	"execl":    2,
	"execlp":   2,
	"execle":   2,
	"syscall":  1,
}

// variadicCallType renders the callee function type a variadic C function must
// be called through, e.g. `i32 (i8*, i32, ...)`. Returns "" for non-variadic
// callees (or when the call passes no variadic argument), which keeps the plain
// `call <ret> @fn(...)` form. See the call-site note in emitClibCall for why
// spelling the type out is required.
func variadicCallType(fn, ret string, argTypes []string) string {
	nFixed, ok := variadicFixedArgs[fn]
	if !ok || len(argTypes) <= nFixed {
		return ""
	}
	fixed := append([]string{}, argTypes[:nFixed]...)
	fixed = append(fixed, "...")
	return ret + " (" + strings.Join(fixed, ", ") + ")"
}

// variadicDecl rewrites `declare <ret> @<name>(a, b, c)` into
// `declare <ret> @<name>(a, b, ...)` when <name> is a known variadic C
// function. Declarations that are already variadic, or that name something
// else, are returned unchanged.
func variadicDecl(line string) string {
	const marker = "@"
	if !strings.HasPrefix(line, "declare") {
		return line
	}
	at := strings.IndexByte(line, '@')
	op := strings.IndexByte(line, '(')
	if at < 0 || op < 0 || at > op {
		return line
	}
	name := line[at+1 : op]
	nFixed, ok := variadicFixedArgs[name]
	if !ok {
		return line
	}
	close := strings.LastIndexByte(line, ')')
	if close < op {
		return line
	}
	args := line[op+1 : close]
	if strings.Contains(args, "...") {
		return line
	}
	// Split top-level commas (types here are simple: no nested parens except
	// function pointers, which none of these signatures use).
	var parts []string
	depth := 0
	cur := ""
	for _, r := range args {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(cur))
				cur = ""
				continue
			}
		}
		cur += string(r)
	}
	if last := strings.TrimSpace(cur); last != "" {
		parts = append(parts, last)
	}
	if len(parts) <= nFixed {
		// Fewer args than the fixed arity: nothing to convert, keep as-is.
		return line
	}
	fixed := parts[:nFixed]
	if nFixed == 0 {
		return line[:op+1] + "..." + line[close:]
	}
	return line[:op+1] + strings.Join(fixed, ", ") + ", ..." + line[close:]
}

// declFuncName extracts the callee name from a `declare ... @name(...)` line.
func declFuncName(line string) string {
	at := strings.IndexByte(line, '@')
	if at < 0 {
		return ""
	}
	rest := line[at+1:]
	if p := strings.IndexByte(rest, '('); p >= 0 {
		return rest[:p]
	}
	return rest
}

// decl records an external declaration to be appended to the module. Duplicate
// symbols collapse: a module may call the same C function from many sites and
// LLVM rejects two identical declarations only in the sense of wasting text.
//
// Variadic C functions are rewritten to a variadic signature before recording:
// MIR derives declarations from the Nolang call site (one arg per argument),
// which yields a NON-variadic `declare i32 @open(i8*, i32, i32)`. The real
// libc `open` is variadic, and on AAPCS64 (Apple ARM64) variadic arguments are
// passed on the STACK while the first two go in registers — so a non-variadic
// declaration makes the callee read `mode` from the stack instead of x2 and the
// file is created with garbage permissions (test-open-read: 0140 instead of
// 0600, which then made the follow-up O_RDONLY open fail with EACCES).
// Legacy declares these as `declare i32 @open(ptr, i32, ...)`.
func (c *codegen) decl(line string) {
	line = variadicDecl(line)
	if c.extDecls == nil {
		c.extDecls = map[string]bool{}
	}
	if c.extDecls[line] {
		return
	}
	// Runtime-provided functions are already declared in the prelude. The generic
	// C-forwarder would re-declare them (possibly with a different signature), so
	// skip to avoid LLVM "invalid redefinition of function 'X'".
	if runtimeFns[declFuncName(line)] {
		c.extDecls[line] = true
		return
	}
	c.extDecls[line] = true
	c.extDeclOrder = append(c.extDeclOrder, line)
}

// structLLVMType resolves the LLVM type name of a Nolang struct definition by
// its (possibly module-qualified) name suffix. Structs from std dependencies
// are registered module-qualified (e.g. os.utsname), but builtin result types
// often carry only the bare name, so ptype cannot resolve them directly. The
// suffix match keeps the lookup robust to the module prefix.
func (c *codegen) structLLVMType(suffix string) string {
	// Exact match first: a bare struct name must resolve to ITS OWN definition,
	// not a module-qualified one that merely shares the suffix. Without this,
	// `structLLVMType("conn")` could return `%net_conn` (std's `net.conn`, a
	// single-field {i64}) instead of the user's `%conn` ({%str-long, i64})
	// depending on Go map iteration order — producing an option payload type
	// with the WRONG layout and a `getelementptr ... i32 0, i32 1` that indexes
	// a one-field struct (invalid getelementptr indices, opt-verify) for
	// `?conn.field` access — tests/test-opt-struct-field.no.
	if _, ok := c.mod.StructFields[suffix]; ok {
		return "%" + sanitize(suffix)
	}
	for raw := range c.mod.StructFields {
		if strings.HasSuffix(raw, suffix) {
			return "%" + sanitize(raw)
		}
	}
	return ""
}

// structKeyOf resolves a (possibly unqualified) struct raw name to the key
// under which it is actually registered in StructFields. std structs are
// module-qualified (e.g. `os.utsname`), so a bare reference like `utsname`
// won't hit the exact key; this falls back to a suffix match. Used by
// emitGetField/emitSetField so `uts.sysname` resolves its field layout even
// though the receiver type is recorded as the bare `utsname`.
func (c *codegen) structKeyOf(raw string) string {
	if raw == "" {
		return ""
	}
	if _, ok := c.mod.StructFields[raw]; ok {
		return raw
	}
	for k := range c.mod.StructFields {
		if k == raw || strings.HasSuffix(k, "."+raw) {
			return k
		}
	}
	return ""
}

// treg mints a unique virtual register with a readable prefix.
func (c *codegen) treg(prefix string) string {
	c.loadSeq++
	return fmt.Sprintf("%%%s%d", prefix, c.loadSeq)
}

// global adds a module-level global definition, emitted after the prelude.
func (c *codegen) global(line string) {
	for _, g := range c.extraGlobals {
		if g == line {
			return
		}
	}
	c.extraGlobals = append(c.extraGlobals, line)
}

// coerce materializes srcVal (of LLVM type srcTy) as wantTy, emitting a
// conversion when the types differ. Returns "" when no conversion exists — the
// caller then reports a gap so the transpiler falls back to legacy rather than
// emitting a type-mismatched call that `opt` would reject.
func (c *codegen) coerce(srcTy, srcVal, wantTy string) string {
	if srcTy == wantTy || srcTy == "void" {
		return srcVal
	}
	r := c.treg("cv")
	switch {
	case srcTy == "i64" && wantTy == "i32":
		c.sb.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i32\n", r, srcVal))
	case srcTy == "i64" && wantTy == "double":
		c.sb.WriteString(fmt.Sprintf("  %s = sitofp i64 %s to double\n", r, srcVal))
	case srcTy == "i64" && wantTy == "i8":
		c.sb.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i8\n", r, srcVal))
	case srcTy == "double" && wantTy == "i64":
		c.sb.WriteString(fmt.Sprintf("  %s = fptosi double %s to i64\n", r, srcVal))
	case srcTy == "i8" && wantTy == "i64":
		c.sb.WriteString(fmt.Sprintf("  %s = zext i8 %s to i64\n", r, srcVal))
	case srcTy == "i1" && wantTy == "i64":
		c.sb.WriteString(fmt.Sprintf("  %s = zext i1 %s to i64\n", r, srcVal))
	case srcTy == "i1" && wantTy == "i32":
		c.sb.WriteString(fmt.Sprintf("  %s = zext i1 %s to i32\n", r, srcVal))
	case srcTy == "i32" && wantTy == "i64":
		c.sb.WriteString(fmt.Sprintf("  %s = sext i32 %s to i64\n", r, srcVal))
	case srcTy == "i8*" && wantTy == "i64":
		c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", r, srcVal))
	case srcTy == "i64" && wantTy == "i8*":
		c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", r, srcVal))
	default:
		return ""
	}
	return r
}

// dataPtrOf returns an i8* register pointing at the backing bytes of an owned
// container (%str-long / %vec). It does NOT null-terminate: use cstrOf for C
// functions that expect a NUL-terminated string.
func (c *codegen) dataPtrOf(v ValueID) string {
	lt, _ := c.ptype(v)
	// An option-typed buffer (`?str` / `?[]byte`, LLVM `%option_str`) must be
	// peeled first: net-send / net-recv take the raw data pointer of the
	// PAYLOAD, and a `{tag, payload}` struct has no data field of its own.
	if strings.HasPrefix(lt, "%option") {
		_, sv := c.loadVal(v)
		pv, pt := c.optionPayloadOf(sv, lt)
		switch pt {
		case "%str-long":
			r := c.treg("dpl")
			c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%str-long %s, 2\n", r, pv))
			return r
		case "%vec":
			l := c.treg("dpl")
			c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 2\n", l, pv))
			r := c.treg("dpp")
			c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", r, l))
			return r
		}
		return ""
	}
	slot := c.valSlot[v]
	if slot == "" {
		if os.Getenv("NOLANG_MIR_DEBUG_CSTR") != "" {
			fmt.Fprintf(os.Stderr, "[dptr] v=%d lt=%q (no slot)\n", v, lt)
		}
		return ""
	}
	switch lt {
	case "%str-long":
		g := c.treg("dpg")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n", g, slot))
		r := c.treg("dpl")
		c.sb.WriteString(fmt.Sprintf("  %s = load i8*, i8** %s\n", r, g))
		return r
	case "%vec":
		g := c.treg("dpg")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g, slot))
		l := c.treg("dpl")
		c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", l, g))
		r := c.treg("dpp")
		c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", r, l))
		return r
	}
	if os.Getenv("NOLANG_MIR_DEBUG_CSTR") != "" {
		fmt.Fprintf(os.Stderr, "[dptr] v=%d lt=%q slot=%q\n", v, lt, slot)
	}
	// Fixed-size array `[N x T]` (a byte-array buffer or a short string kept
	// inline): GEP to element 0 and bitcast to i8*. `net-send(fd, data, n)` on
	// a `[5 x i8]` constant reached here and failed with "cannot take data
	// pointer of arg 1" (test-tls / test-https-server).
	if strings.HasPrefix(lt, "[") && strings.HasSuffix(lt, "]") {
		parts := strings.Fields(strings.TrimSuffix(strings.TrimPrefix(lt, "["), "]"))
		if len(parts) > 0 {
			elemT := parts[len(parts)-1]
			g := c.treg("dpg")
			c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 0, i64 0\n", g, lt, lt, slot))
			r := c.treg("dpp")
			c.sb.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", r, elemT, g))
			return r
		}
	}
	return ""
}

// cstrOf materializes a NUL-terminated heap copy of a str/[]byte argument so it
// can be handed to a C function expecting a `char*`. The returned register MUST
// be freed by the caller once the C call has returned (Nolang strings are not
// NUL-terminated, so passing the raw data pointer would let libc read past the
// end of the buffer).
func (c *codegen) cstrOf(v ValueID) string {
	lt, _ := c.ptype(v)
	// An option-typed string argument (`?str`, LLVM `%option_str`) carries its
	// payload inline; C-library entry points such as net-dial / net-listen take
	// the plain string, so peel field 1 first. Without this every
	// `net.net-dial(host, port)` whose `host` came from an option-typed binding
	// failed with "cannot marshal host as C string" (test-tls / test-http-rest /
	// test-net-http / test-https-server). A nil option has a zero payload, so
	// inet_pton simply fails and the builtin returns -1 — the same "cannot
	// connect" outcome as a bad host string.
	if strings.HasPrefix(lt, "%option") {
		_, sv := c.loadVal(v)
		pv, pt := c.optionPayloadOf(sv, lt)
		if pt == "%str-long" || pt == "%vec" {
			r := c.treg("cs")
			c.sb.WriteString(fmt.Sprintf("  %s = call i8* @str_cstr(%s %s)\n", r, pt, pv))
			return r
		}
		return ""
	}
	if lt != "%str-long" && lt != "%vec" {
		if os.Getenv("NOLANG_MIR_DEBUG_CSTR") != "" {
			fmt.Fprintf(os.Stderr, "[cstr] v=%d lt=%q\n", v, lt)
		}
		return ""
	}
	_, sval := c.loadVal(v)
	r := c.treg("cs")
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @str_cstr(%s %s)\n", r, lt, sval))
	return r
}

// ---------------------------------------------------------------------------
// CLibCall: the generic C call path
// ---------------------------------------------------------------------------

// emitBuiltinCLib lowers a call to a C library function described by a
// builtin.CLibCall. It is the single highest-leverage builtin path: one
// implementation covers every builtin that is "just a libc call with a type
// fixup", which is the majority of the os/fs/process/net tables.
//
// The argument walk deliberately does NOT consume an argument slot for entries
// that are supplied by the compiler itself (FixedArgs / FixedArgGlobals /
// RetBuf's scratch buffer): those are ABI padding, not Nolang values. Legacy
// (build/llvm genCLibCall) advances its argument cursor for them too, which
// silently misaligns any builtin that declares a fixed argument before a real
// one.
func (c *codegen) emitBuiltinCLib(f *Function, inst *Inst, bm *builtin.BuiltinMethod) error {
	cl := bm.CLibCall
	if cl == nil || cl.FuncName == "" {
		return fmt.Errorf("builtin %s: no CLibCall", inst.Sym)
	}
	fn := cl.FuncName

	// RetBuf builtins (get-wd / host-name) write into a static 1024-byte
	// scratch buffer owned by the module.
	if cl.RetBuf && cl.BufGlobal != "" {
		c.global("@.mir-os-buf = private global [1024 x i8] zeroinitializer")
	}

	cRet := clibLLVMType(cl.RetType)
	var declArgs []string
	var argStrs []string
	var frees []string // NUL-terminated temporaries to release after the call
	ev := 0
	for i, at := range cl.ArgTypes {
		lt := clibLLVMType(at)
		declArgs = append(declArgs, lt)
		switch {
		case cl.RetBuf && i == 0 && at == builtin.LLVMI8Ptr && cl.BufGlobal != "":
			argStrs = append(argStrs, "i8* getelementptr inbounds ([1024 x i8], [1024 x i8]* @.mir-os-buf, i64 0, i64 0)")
			continue
		}
		if fv, ok := cl.FixedArgs[i]; ok {
			argStrs = append(argStrs, lt+" "+fv)
			continue
		}
		if fg, ok := cl.FixedArgGlobals[i]; ok {
			argStrs = append(argStrs, fg)
			continue
		}
		if ev >= len(inst.Args) {
			argStrs = append(argStrs, lt+" "+clibZero(at))
			continue
		}
		av := inst.Args[ev]
		ev++
		if _, truncated := cl.TruncArgs[i]; truncated {
			srcTy, srcVal := c.loadVal(av)
			v := c.coerce(srcTy, srcVal, clibLLVMType(cl.TruncArgs[i]))
			if v == "" {
				c.fail("builtin %s: cannot coerce arg %d from %s to %s in func %s", inst.Sym, i, srcTy, lt, f.Name)
				return fmt.Errorf("builtin %s arg %d: %s", inst.Sym, i, strings.Join(c.errs, "; "))
			}
			argStrs = append(argStrs, clibLLVMType(cl.TruncArgs[i])+" "+v)
			continue
		}
		if cl.StrDataArg != nil && cl.StrDataArg[i] {
			p := c.dataPtrOf(av)
			if p == "" {
				c.fail("builtin %s: arg %d is not a byte container in func %s", inst.Sym, i, f.Name)
				return fmt.Errorf("builtin %s arg %d: %s", inst.Sym, i, strings.Join(c.errs, "; "))
			}
			argStrs = append(argStrs, "i8* "+p)
			continue
		}
		if at == builtin.LLVMStrPtr {
			p := c.cstrOf(av)
			if p == "" {
				srcTy, srcVal := c.loadVal(av)
				p = c.coerce(srcTy, srcVal, "i8*")
			} else {
				frees = append(frees, p)
			}
			if p == "" {
				c.fail("builtin %s: cannot pass arg %d as i8* in func %s", inst.Sym, i, f.Name)
				return fmt.Errorf("builtin %s arg %d: %s", inst.Sym, i, strings.Join(c.errs, "; "))
			}
			argStrs = append(argStrs, "i8* "+p)
			continue
		}
		srcTy, srcVal := c.loadVal(av)
		v := c.coerce(srcTy, srcVal, lt)
		if v == "" {
			c.fail("builtin %s: cannot coerce arg %d from %s to %s in func %s", inst.Sym, i, srcTy, lt, f.Name)
			return fmt.Errorf("builtin %s arg %d: %s", inst.Sym, i, strings.Join(c.errs, "; "))
		}
		argStrs = append(argStrs, lt+" "+v)
	}
	argList := strings.Join(argStrs, ", ")
	c.decl(fmt.Sprintf("declare %s @%s(%s)", cRet, fn, strings.Join(declArgs, ", ")))
	// A variadic C callee must ALSO be called through an explicit function
	// type: LLVM's textual parser builds a `call` from the printed return type
	// plus the argument list when no type is spelled out, so
	// `call i32 @open(i8*, i32, i32)` parses as NON-variadic even though @open
	// is declared with `...`. llc then passes the mode argument in x2 WITHOUT
	// setting up the AArch64 varargs register save area, and libc's va_arg
	// reads stack garbage as the file mode — every file was created with
	// garbage permissions (test-open-read: 0140 instead of 0600, which made
	// the follow-up O_RDONLY open fail with EACCES). Spelling the type out
	// (`call i32 (i8*, i32, ...) @open(...)`) restores the varargs ABI.
	cTy := cRet
	if vt := variadicCallType(fn, cRet, declArgs); vt != "" {
		cTy = vt
	}

	dstLT := ""
	dstSlot := ""
	if inst.Dst > NoVal {
		dstLT, _ = c.ptype(inst.Dst)
		dstSlot = c.valSlot[inst.Dst]
	}

	switch {
	case cl.RetBuf:
		// The C call's own return value is discarded: the interesting output is
		// what it wrote into the scratch buffer.
		c.sb.WriteString(fmt.Sprintf("  call %s @%s(%s)\n", cTy, fn, argList))
		if dstLT == "%str-long" {
			if dstSlot == "" {
				return fmt.Errorf("builtin %s: no result slot", inst.Sym)
			}
			buf := "getelementptr inbounds ([1024 x i8], [1024 x i8]* @.mir-os-buf, i64 0, i64 0)"
			r := c.treg("rb")
			c.sb.WriteString(fmt.Sprintf("  %s = call %%str-long @str_from_cstr(i8* %s)\n", r, buf))
			c.sb.WriteString(fmt.Sprintf("  store %%str-long %s, %%str-long* %s\n", r, dstSlot))
		}
	case cl.RetCStrToStr:
		// C returns `char*`; copy it onto the heap so MIR's drop can free it.
		// (libc static storage must never end up inside a %str-long.)
		p := c.treg("cp")
		c.sb.WriteString(fmt.Sprintf("  %s = call i8* @%s(%s)\n", p, fn, argList))
		if dstLT == "%str-long" && dstSlot != "" {
			r := c.treg("c2s")
			c.sb.WriteString(fmt.Sprintf("  %s = call %%str-long @str_from_cstr(i8* %s)\n", r, p))
			c.sb.WriteString(fmt.Sprintf("  store %%str-long %s, %%str-long* %s\n", r, dstSlot))
		}
	case cl.RetExt != nil:
		r := c.treg("cr")
		c.sb.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", r, cTy, fn, argList))
		if dstLT != "" && dstSlot != "" {
			v := c.coerce(cRet, r, dstLT)
			if v == "" {
				c.fail("builtin %s: cannot coerce result %s to %s in func %s", inst.Sym, cRet, dstLT, f.Name)
				return fmt.Errorf("builtin %s result: %s", inst.Sym, strings.Join(c.errs, "; "))
			}
			c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, v, dstLT, dstSlot))
		}
	case cl.CmpRet:
		// POSIX convention: 0 means success. Nolang models that as a bool.
		r := c.treg("cr")
		c.sb.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", r, cTy, fn, argList))
		if dstLT != "" && dstSlot != "" {
			cmp := c.treg("cc")
			c.sb.WriteString(fmt.Sprintf("  %s = icmp eq %s %s, 0\n", cmp, cRet, r))
			v := c.coerce("i1", cmp, dstLT)
			if v == "" {
				c.fail("builtin %s: cannot coerce bool result to %s in func %s", inst.Sym, dstLT, f.Name)
				return fmt.Errorf("builtin %s result: %s", inst.Sym, strings.Join(c.errs, "; "))
			}
			c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, v, dstLT, dstSlot))
		}
	default:
		if cRet == "void" {
			c.sb.WriteString(fmt.Sprintf("  call void @%s(%s)\n", fn, argList))
		} else {
			r := c.treg("cr")
			c.sb.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", r, cTy, fn, argList))
			if dstLT != "" && dstSlot != "" {
				v := c.coerce(cRet, r, dstLT)
				if v == "" {
					c.fail("builtin %s: cannot coerce result %s to %s in func %s", inst.Sym, cRet, dstLT, f.Name)
					return fmt.Errorf("builtin %s result: %s", inst.Sym, strings.Join(c.errs, "; "))
				}
				c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, v, dstLT, dstSlot))
			}
		}
	}
	for _, p := range frees {
		c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", p))
	}
	return nil
}

// ---------------------------------------------------------------------------
// LLVMConv: scalar conversions
// ---------------------------------------------------------------------------

// emitBuiltinConv lowers the LLVMConv flavour (i64<->f64, f32<->f64). The MIR
// type system has no f32, so both float kinds are spelled `double` and the
// f32<->f64 pair is a no-op copy — matching the legacy emitter, which also
// keeps f32 values in doubles.
func (c *codegen) emitBuiltinConv(f *Function, inst *Inst, bm *builtin.BuiltinMethod) error {
	if len(inst.Args) == 0 {
		return fmt.Errorf("builtin %s: missing operand", inst.Sym)
	}
	srcTy, srcVal := c.loadVal(inst.Args[0])
	// An option flowing into a scalar conversion carries the value in its
	// payload field (the nil/err arms have already returned at this point), so
	// peel it before coercing. See peelOptionValue.
	if pt, pv := c.peelOptionValue(srcTy, srcVal); pv != "" {
		srcTy, srcVal = pt, pv
	}
	if inst.Dst <= NoVal {
		return nil
	}
	dstLT, _ := c.ptype(inst.Dst)
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("builtin %s: no result slot", inst.Sym)
	}
	var op string
	switch *bm.LLVMConv {
	case builtin.LLVMConvI64ToFP:
		op = "sitofp"
	case builtin.LLVMConvFPToI64:
		op = "fptosi"
	case builtin.LLVMConvF64ToF32, builtin.LLVMConvF32ToF64:
		op = "" // no-op: f32 is represented as double throughout MIR
	}
	if op == "" {
		v := c.coerce(srcTy, srcVal, dstLT)
		if v == "" {
			return fmt.Errorf("builtin %s: cannot coerce %s to %s", inst.Sym, srcTy, dstLT)
		}
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, v, dstLT, dstSlot))
		return nil
	}
	r := c.treg("cnv")
	c.sb.WriteString(fmt.Sprintf("  %s = %s %s %s to %s\n", r, op, srcTy, srcVal, dstLT))
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, r, dstLT, dstSlot))
	return nil
}

// ---------------------------------------------------------------------------
// ForwardFunc: named, hand-written lowerings
// ---------------------------------------------------------------------------

// emitBuiltinForward lowers a ForwardFunc builtin. These have no uniform ABI —
// each is a small piece of inline IR — so they are handled by name. Unknown
// names report a gap (the caller's strangler-fig then falls back to legacy),
// which keeps MIR honest: it never emits a guess.
func (c *codegen) emitBuiltinForward(f *Function, inst *Inst, bm *builtin.BuiltinMethod) error {
	switch bm.ForwardFunc {
	case "with-cap", "with-len", "with-cap-len":
		return c.emitBuiltinAlloc(inst, bm.ForwardFunc)
	case "eprint":
		return c.emitBuiltinEprint(inst)
	case "get-arch":
		return c.emitBuiltinArch(inst)
	case "str-len", "vec-len", "str-len-bytes":
		return c.emitBuiltinLen(inst, bm.ForwardFunc)
	case "str-clear":
		return c.emitBuiltinStrClear(inst)
	case "math-max", "math-min", "math-abs", "math-clamp", "math-degrees":
		return c.emitBuiltinMath(inst, bm.ForwardFunc)
	case "str-to-bool":
		return c.emitBuiltinStrToBool(inst)
	case "bool-to-str":
		return c.emitBuiltinBoolToStr(inst)
	case "vec-push":
		return c.emitBuiltinVecPush(inst)
	case "vec-clear":
		return c.emitBuiltinVecClear(inst)
	case "vec-pop":
		return c.emitBuiltinVecPop(inst)
	case "vec-reverse":
		return c.emitBuiltinVecReverse(inst)
	case "vec-insert":
		return c.emitBuiltinVecInsert(inst)
	case "vec-remove":
		return c.emitBuiltinVecRemove(inst)
	case "vec-sort-asc":
		return c.emitBuiltinVecSort(inst, true)
	case "vec-sort-desc":
		return c.emitBuiltinVecSort(inst, false)
	case "vec-truncate":
		return c.emitBuiltinVecTruncate(inst)
	case "uname":
		return c.emitBuiltinUname(inst)
	case "net-icmp-open":
		return c.emitBuiltinNetIcmpOpen(inst)
	case "net-dial":
		return c.emitBuiltinNetDial(inst)
	case "net-listen":
		return c.emitBuiltinNetListen(inst)
	case "net-accept":
		return c.emitBuiltinNetAccept(inst)
	case "net-accept-nb":
		return c.emitBuiltinNetAcceptNb(inst)
	case "net-recv-nb":
		return c.emitBuiltinNetRecvNb(inst)
	case "net-udp-sendto":
		return c.emitBuiltinNetUdpSendTo(inst)
	case "net-udp-recvfrom":
		return c.emitBuiltinNetUdpRecvFrom(inst)
	case "net-udp-open":
		return c.emitBuiltinNetUdpOpen(inst)
	case "net-send":
		return c.emitBuiltinNetSend(inst)
	case "net-recv":
		return c.emitBuiltinNetRecv(inst)
	case "net-set-recv-timeout":
		return c.emitBuiltinNetSetRecvTimeout(inst)
	case "read-file":
		return c.emitBuiltinReadFile(inst)
	case "utime":
		return c.emitBuiltinUtime(inst)
	case "get-priority":
		return c.emitBuiltinGetPriority(inst)
	case "get-errno":
		return c.emitBuiltinGetErrno(inst)
	case "read-stdin-line":
		return c.emitBuiltinGetLine(inst)
	case "sysctl":
		return c.emitBuiltinSysctl(inst)
	case "process-waitpid":
		return c.emitBuiltinWaitpid(inst)
	case "process-waitpid-nohang":
		return c.emitBuiltinWaitpidNohang(inst)
	case "process-pipe":
		return c.emitBuiltinPipe(inst)
	case "process-exec-shell":
		return c.emitBuiltinExecShell(inst)
	case "load-le-u16", "load-le-u32", "load-le-u64":
		return c.emitBuiltinLoadLE(f, inst, bm.ForwardFunc)
	case "store-le-u32":
		return c.emitBuiltinStoreLE(f, inst)
	case "eq-raw":
		return c.emitBuiltinEqRaw(inst)
	case "rotate-left", "rotate-right":
		return c.emitBuiltinRotate(f, inst, bm.ForwardFunc)
	case "async-cancel":
		return c.emitBuiltinAsyncCancel(inst)
	case "async-cancelled":
		return c.emitBuiltinAsyncCancelled(inst)
	case "async-yield":
		return c.emitBuiltinAsyncYield(inst)
	case "write-file":
		return c.emitBuiltinWriteFile(inst)
	case "read-dir":
		return c.emitBuiltinReadDir(inst)
	case "str-truncate":
		return c.emitBuiltinStrTruncate(inst)
	}
	// Generic C call: the whole point of forward_call.go. Consulted LAST so a
	// bespoke handler always wins, but it turns "add a POSIX builtin" from a new
	// emitter into a single table entry.
	if spec := forwardCSpecOf(bm.ForwardFunc); spec != nil {
		return c.emitCCall(inst, spec)
	}
	c.fail("unsupported builtin %s (ForwardFunc=%q) in func %s", inst.Sym, bm.ForwardFunc, f.Name)
	return fmt.Errorf("unsupported builtin %s", inst.Sym)
}

// emitBuiltinEqRaw lowers `a.eq(b, n)` (builtin `str.eq`, ForwardFunc
// "eq-raw"): compare the first n bytes of two strings with memcmp and return
// whether they are equal. Mirrors the legacy backend (build/llvm/call.go
// "eq-raw"), which emits memcmp(a_data, b_data, n) == 0 and zero-extends the
// i1 to the boolean result slot. Without it the generic dispatch fell through
// to "unsupported builtin str.eq" (tests/test-str.no).
func (c *codegen) emitBuiltinEqRaw(inst *Inst) error {
	if len(inst.Args) < 3 {
		return fmt.Errorf("eq-raw: needs (receiver, b, n)")
	}
	ap := c.dataPtrOf(inst.Args[0])
	if ap == "" {
		return fmt.Errorf("eq-raw: cannot take data pointer of receiver")
	}
	bp := c.dataPtrOf(inst.Args[1])
	if bp == "" {
		return fmt.Errorf("eq-raw: cannot take data pointer of arg 1")
	}
	nT, nV := c.loadVal(inst.Args[2])
	n := c.coerce(nT, nV, "i64")
	if n == "" {
		n = nV
	}
	c.decl("declare i32 @memcmp(i8*, i8*, i64)")
	cmp := c.treg("eqc")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @memcmp(i8* %s, i8* %s, i64 %s)\n", cmp, ap, bp, n))
	eq := c.treg("eqr")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", eq, cmp))
	if inst.Dst <= NoVal {
		return nil
	}
	dstT, _ := c.ptype(inst.Dst)
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("eq-raw: no result slot")
	}
	if dstT == "" {
		dstT = "i1"
	}
	v := c.coerce("i1", eq, dstT)
	if v == "" {
		return fmt.Errorf("eq-raw: cannot store i1 into %s", dstT)
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstT, v, dstT, dstSlot))
	return nil
}

// emitBuiltinAsyncCancel lowers `async-cancel(h)`: set the task's cancelled flag
// (%task field 3 = true). `h` is MIR's opaque handle — the i64 that OpRun
// stored (ptrtoint of the heap %task i8*), so it is inttoptr'd back to %task*
// here. Mirrors legacy build/llvm/call_stdlib.go verbatim: the generated
// async_wrapper.N checks this flag on entry and, when set, marks the task done
// WITHOUT running the target (graceful abort), so a later `awy h` returns the
// zero value — exactly std/async.no's documented contract ("若已取消则立即返回，
// r 为 zero value").
func (c *codegen) emitBuiltinAsyncCancel(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("async-cancel: missing task handle")
	}
	lt, hreg := c.loadVal(inst.Args[0])
	taskT := hreg
	switch lt {
	case "i64":
		taskT = c.treg("acan.t")
		c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to %%task*\n", taskT, hreg))
	case "i8*":
		taskT = c.treg("acan.t")
		c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %%task*\n", taskT, hreg))
	}
	gep := c.treg("acan.gep")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 3\n", gep, taskT))
	c.sb.WriteString(fmt.Sprintf("  store i1 true, i1* %s\n", gep))
	return nil
}

// emitBuiltinAsyncCancelled lowers `async-cancelled()`: cooperative
// self-cancellation check. It reads @nolang_current_task (the task the
// scheduler is currently running) and returns its cancelled flag (field 3).
// When no task is current (e.g. a top-level `awy` sync drive, or a non-async
// context) current_task is null, so the null arm yields false — matching the
// legacy comment "若当前不在异步任务中...安全返回 false". The i1 result is
// materialized via phi across the null/load arms (never dereferencing null) and
// coerced into the destination slot's type (i1 or i64).
func (c *codegen) emitBuiltinAsyncCancelled(inst *Inst) error {
	if inst.Dst <= NoVal {
		return nil
	}
	dstLT, _ := c.ptype(inst.Dst)
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("async-cancelled: no result slot")
	}
	cur := c.treg("acur.cur")
	c.sb.WriteString(fmt.Sprintf("  %s = load i8*, i8** @nolang_current_task\n", cur))
	isNull := c.treg("acur.isnull")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i8* %s, null\n", isNull, cur))
	nullDef := c.label("acur.null")
	loadDef := c.label("acur.load")
	doneDef := c.label("acur.done")
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", isNull, nullDef, loadDef))
	c.sb.WriteString(nullDef + ":\n")
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", doneDef))
	c.sb.WriteString(loadDef + ":\n")
	cast := c.treg("acur.cast")
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %%task*\n", cast, cur))
	gep := c.treg("acur.gep")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 3\n", gep, cast))
	loaded := c.treg("acur.can")
	c.sb.WriteString(fmt.Sprintf("  %s = load i1, i1* %s\n", loaded, gep))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", doneDef))
	c.sb.WriteString(doneDef + ":\n")
	phi := c.treg("acur.phi")
	c.sb.WriteString(fmt.Sprintf("  %s = phi i1 [ %s, %%%s ], [ false, %%%s ]\n", phi, loaded, loadDef, nullDef))
	v := c.coerce("i1", phi, dstLT)
	if v == "" {
		return fmt.Errorf("async-cancelled: cannot store i1 into %s", dstLT)
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, v, dstLT, dstSlot))
	return nil
}

// emitBuiltinAsyncYield lowers `async-yield()`. In an `-async` function used as
// a top-level statement the legacy backend rewrites this into a coroutine
// suspend point (coro.go); MIR has no coroutine state transformation, so it
// always takes the degenerate path — call @nolang_async_yield, which re-enqueues
// the current task and returns. Returns void (no destination).
func (c *codegen) emitBuiltinAsyncYield(inst *Inst) error {
	c.sb.WriteString("  call void @nolang_async_yield()\n")
	return nil
}

// declareIntrinsic records a module-level `declare` for an LLVM intrinsic so it
// is emitted once (deduped by exact string) after the function bodies. MIR has
// no separate declaration pass, so builtins that need an intrinsic (e.g. the
// rotate-left/right fshl/fshr) register it lazily here.
func (c *codegen) declareIntrinsic(decl string) {
	if c.extDecls == nil {
		c.extDecls = map[string]bool{}
	}
	if !c.extDecls[decl] {
		c.extDecls[decl] = true
		c.extDeclOrder = append(c.extDeclOrder, decl)
	}
}

// rawTypeOf returns the nolang raw type string of a value (e.g. "u32", "i64",
// "[]byte", "[16]byte"), used to pick the correct integer width for operations
// whose LLVM representation (always i64 in MIR) does not carry the original
// width. Falls back to "" when the value/type is unavailable.
func (c *codegen) rawTypeOf(v ValueID) string {
	if val := c.mod.Value(v); val != nil {
		if t := c.mod.Type(val.Type); t != nil {
			return t.Raw
		}
	}
	return ""
}

// byteDataPtr returns an i8* register pointing at the backing bytes of a
// byte-array receiver, for the load-le-uXX / store-le-u32 builtins. The receiver
// may be a %vec ([]byte) — whose data pointer is field 2, stored as an i64
// intptr — or a fixed [N x i8] array — whose slot bitcasts directly to i8*. The
// legacy byteArrDataPtr handles both shapes identically.
func (c *codegen) byteDataPtr(v ValueID) (string, error) {
	slot := c.valSlot[v]
	if slot == "" {
		return "", fmt.Errorf("byte-array receiver has no slot")
	}
	lt, _ := c.ptype(v)
	switch {
	case lt == "%vec":
		g := c.treg("bd.g")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g, slot))
		l := c.treg("bd.l")
		c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", l, g))
		r := c.treg("bd.p")
		c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", r, l))
		return r, nil
	case strings.HasPrefix(lt, "["):
		r := c.treg("bd.p")
		c.sb.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", r, lt, slot))
		return r, nil
	default:
		c.fail("load-le/store-le: unsupported receiver type %s", lt)
		return "", fmt.Errorf("unsupported receiver type %s", lt)
	}
}

// emitBuiltinLoadLE lowers `arr.load-le-uXX(off)` / `load-le-uXX(arr, off)`:
// read a little-endian uXX integer from the byte array's data at offset and
// zero-extend it to i64 (MIR's representation of u16/u32/u64). Mirrors the
// legacy call.go load-le-uXX inliner exactly: GEP to the offset, bitcast to the
// narrow type, load, zext to i64. The receiver is always inst.Args[0] (the
// method receiver is prepended by lowerCall, matching vec.push).
func (c *codegen) emitBuiltinLoadLE(f *Function, inst *Inst, ff string) error {
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("load-le-%s: no dst slot", ff)
	}
	if len(inst.Args) < 2 {
		c.sb.WriteString(fmt.Sprintf("  store i64 0, i64* %s\n", dstSlot))
		return nil
	}
	dataPtr, err := c.byteDataPtr(inst.Args[0])
	if err != nil {
		return err
	}
	offT, offV := c.loadVal(inst.Args[1])
	off := offV
	if offT != "i64" {
		if r := c.coerce(offT, offV, "i64"); r != "" {
			off = r
		}
	}
	gep := c.treg("le.g")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", gep, dataPtr, off))
	lt := "i64"
	switch ff {
	case "load-le-u16":
		lt = "i16"
	case "load-le-u32":
		lt = "i32"
	default:
		lt = "i64"
	}
	typed := c.treg("le.t")
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", typed, gep, lt))
	val := c.treg("le.v")
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", val, lt, lt, typed))
	if lt != "i64" {
		z := c.treg("le.z")
		c.sb.WriteString(fmt.Sprintf("  %s = zext %s %s to i64\n", z, lt, val))
		c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", z, dstSlot))
	} else {
		c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", val, dstSlot))
	}
	return nil
}

// emitBuiltinStoreLE lowers `arr.store-le-u32(off, val)`: store val as a
// little-endian u32 into the byte array's data at offset. Mirrors the legacy
// call.go store-le-u32 inliner: truncate val to i32, GEP to the offset, bitcast
// to i32*, store. The receiver is inst.Args[0]; the result slot is unused (the
// builtin returns nothing).
func (c *codegen) emitBuiltinStoreLE(f *Function, inst *Inst) error {
	if len(inst.Args) < 3 {
		return nil
	}
	dataPtr, err := c.byteDataPtr(inst.Args[0])
	if err != nil {
		return err
	}
	offT, offV := c.loadVal(inst.Args[1])
	off := offV
	if offT != "i64" {
		if r := c.coerce(offT, offV, "i64"); r != "" {
			off = r
		}
	}
	valT, valV := c.loadVal(inst.Args[2])
	valI32 := valV
	if valT != "i32" {
		if r := c.coerce(valT, valV, "i32"); r != "" {
			valI32 = r
		} else {
			tr := c.treg("ls.tr")
			c.sb.WriteString(fmt.Sprintf("  %s = trunc %s %s to i32\n", tr, valT, valV))
			valI32 = tr
		}
	}
	gep := c.treg("ls.g")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", gep, dataPtr, off))
	typed := c.treg("ls.t")
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to i32*\n", typed, gep))
	c.sb.WriteString(fmt.Sprintf("  store i32 %s, i32* %s\n", valI32, typed))
	return nil
}

// emitBuiltinRotate lowers `number.rotate-left(x, n)` / `number.rotate-right(x,
// n)` via the LLVM funnel-shift intrinsics llvm.fshl / llvm.fshr. The rotation
// width follows the FIRST argument's nolang raw type (u16→i16, u32→i32, else
// i64), matching the legacy call.go select of i32 vs i64. Because MIR stores
// every integer as i64, the operand is coerced to the narrow width before the
// intrinsic (discarding the garbage high bits that 64-bit arithmetic may have
// produced) and the narrow result is zero-extended back to i64 for the
// destination — exactly the legacy 32-bit-wrap semantics.
func (c *codegen) emitBuiltinRotate(f *Function, inst *Inst, ff string) error {
	if inst.Dst <= NoVal || len(inst.Args) < 2 {
		return nil
	}
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("rotate: no dst slot")
	}
	xT, xV := c.loadVal(inst.Args[0])
	nT, nV := c.loadVal(inst.Args[1])
	raw := c.rawTypeOf(inst.Args[0])
	w := "i64"
	switch {
	case raw == "u8" || raw == "i8":
		w = "i8"
	case raw == "u16" || raw == "i16":
		w = "i16"
	case raw == "u32" || raw == "i32":
		w = "i32"
	default:
		w = "i64"
	}
	toW := func(fromT, fromV string) string {
		if fromT == w {
			return fromV
		}
		if r := c.coerce(fromT, fromV, w); r != "" {
			return r
		}
		tr := c.treg("rot.cv")
		if w == "i64" {
			c.sb.WriteString(fmt.Sprintf("  %s = zext %s %s to i64\n", tr, fromT, fromV))
		} else {
			c.sb.WriteString(fmt.Sprintf("  %s = trunc %s %s to %s\n", tr, fromT, fromV, w))
		}
		return tr
	}
	xw := toW(xT, xV)
	nw := toW(nT, nV)
	intrin := "llvm.fshl"
	if ff == "rotate-right" {
		intrin = "llvm.fshr"
	}
	c.declareIntrinsic(fmt.Sprintf("declare %s @%s.%s(%s, %s, %s)", w, intrin, w, w, w, w))
	res := c.treg("rot.r")
	c.sb.WriteString(fmt.Sprintf("  %s = call %s @%s.%s(%s %s, %s %s, %s %s)\n", res, w, intrin, w, w, xw, w, xw, w, nw))
	if w != "i64" {
		z := c.treg("rot.z")
		c.sb.WriteString(fmt.Sprintf("  %s = zext %s %s to i64\n", z, w, res))
		c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", z, dstSlot))
	} else {
		c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", res, dstSlot))
	}
	return nil
}

// emitBuiltinMath lowers the scalar math forwards (math-max / math-min /
// math-abs / math-clamp / math-degrees). The semantics mirror the legacy
// call.go inliners exactly: a handful of icmp/select/fp ops, no std dependency,
// result stored into the destination slot. The receiver (if any) is already the
// first argument in inst.Args, matching the legacy `args[]` layout.
func (c *codegen) emitBuiltinMath(inst *Inst, ff string) error {
	if inst.Dst <= NoVal {
		return nil
	}
	dstLT, _ := c.ptype(inst.Dst)
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("builtin %s: no result slot", ff)
	}
	loadI64 := func(i int) string {
		if i >= len(inst.Args) {
			return "0"
		}
		ty, v := c.loadVal(inst.Args[i])
		if ty == "i64" {
			return v
		}
		if r := c.coerce(ty, v, "i64"); r != "" {
			return r
		}
		return "0"
	}
	sel := func(cmp, a, b string) string {
		cmpReg := c.treg("mc")
		c.sb.WriteString(fmt.Sprintf("  %s = %s\n", cmpReg, cmp))
		sReg := c.treg("ms")
		c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 %s\n", sReg, cmpReg, a, b))
		return sReg
	}
	var res string
	switch ff {
	case "math-max":
		a, b := loadI64(0), loadI64(1)
		res = sel(fmt.Sprintf("icmp sgt i64 %s, %s", a, b), a, b)
	case "math-min":
		a, b := loadI64(0), loadI64(1)
		res = sel(fmt.Sprintf("icmp slt i64 %s, %s", a, b), a, b)
	case "math-abs":
		a := loadI64(0)
		cmpReg := c.treg("ac")
		c.sb.WriteString(fmt.Sprintf("  %s = icmp slt i64 %s, 0\n", cmpReg, a))
		subReg := c.treg("as")
		c.sb.WriteString(fmt.Sprintf("  %s = sub i64 0, %s\n", subReg, a))
		sReg := c.treg("asel")
		c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 %s\n", sReg, cmpReg, subReg, a))
		res = sReg
	case "math-clamp":
		val, lo, hi := loadI64(0), loadI64(1), loadI64(2)
		hiCmp := c.treg("ch")
		c.sb.WriteString(fmt.Sprintf("  %s = icmp sgt i64 %s, %s\n", hiCmp, val, hi))
		hiSel := c.treg("hs")
		c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 %s\n", hiSel, hiCmp, hi, val))
		loCmp := c.treg("cl")
		c.sb.WriteString(fmt.Sprintf("  %s = icmp slt i64 %s, %s\n", loCmp, hiSel, lo))
		loSel := c.treg("ls")
		c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 %s\n", loSel, loCmp, lo, hiSel))
		res = loSel
	case "math-degrees":
		// r * 180 / PI  (PI as double literal 0x400921FB54442D18)
		rdTy, rdV := c.loadVal(inst.Args[0])
		r := c.coerce(rdTy, rdV, "double")
		if r == "" {
			r = rdV
		}
		m := c.treg("dm")
		c.sb.WriteString(fmt.Sprintf("  %s = fmul double %s, 1.8e+02\n", m, r))
		d := c.treg("dd")
		c.sb.WriteString(fmt.Sprintf("  %s = fdiv double %s, 0x400921FB54442D18\n", d, m))
		res = d
		if dstLT == "double" && dstSlot != "" {
			c.sb.WriteString(fmt.Sprintf("  store double %s, double* %s\n", d, dstSlot))
			return nil
		}
		// store into an i64 slot: truncate the degrees value (legacy behaviour is
		// broken here anyway); coerce via fptosi.
		ri := c.coerce("double", d, dstLT)
		if ri == "" {
			return fmt.Errorf("builtin %s: cannot store double into %s", ff, dstLT)
		}
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, ri, dstLT, dstSlot))
		return nil
	}
	if res == "" {
		return fmt.Errorf("builtin %s: no result", ff)
	}
	v := c.coerce("i64", res, dstLT)
	if v == "" {
		return fmt.Errorf("builtin %s: cannot store i64 into %s", ff, dstLT)
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, v, dstLT, dstSlot))
	return nil
}

// emitBuiltinArch lowers `get-arch()`: the CPU architecture as a compile-time
// constant string. Legacy derives it from runtime.GOARCH and heap-allocates it
// so emitHeapFree can release it; MIR does the same through @str_from_const
// (which mallocs), keeping ownership uniform with every other MIR string.
func (c *codegen) emitBuiltinArch(inst *Inst) error {
	if inst.Dst <= NoVal {
		return nil
	}
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("builtin get-arch: no result slot")
	}
	arch := runtime.GOARCH
	c.global(fmt.Sprintf("@.mir.arch = private constant [%d x i8] c\"%s\"", len(arch), dataStr(arch)))
	r := c.treg("arch")
	c.sb.WriteString(fmt.Sprintf("  %s = call %%str-long @str_from_const(i8* getelementptr inbounds ([%d x i8], [%d x i8]* @.mir.arch, i64 0, i64 0), i64 %d)\n",
		r, len(arch), len(arch), len(arch)))
	c.sb.WriteString(fmt.Sprintf("  store %%str-long %s, %%str-long* %s\n", r, dstSlot))
	return nil
}

// emitBuiltinLen lowers `str-len` / `vec-len`: field 0 of the container is the
// element count. The receiver is args[0]; reading it through the slot avoids a
// by-value struct load.
func (c *codegen) emitBuiltinLen(inst *Inst, ff string) error {
	if len(inst.Args) == 0 {
		return fmt.Errorf("builtin %s: missing receiver", ff)
	}
	lt, _ := c.ptype(inst.Args[0])
	slot := c.valSlot[inst.Args[0]]
	if slot == "" {
		return fmt.Errorf("builtin %s: receiver has no slot", ff)
	}
	if strings.HasPrefix(lt, "[") {
		// Fixed array [N x E]: the length is the compile-time element count N
		// (no runtime length field exists). Mirrors emitLenCap's KindArray path
		// and the legacy backend, which returns N for `len([...])`. Without this,
		// `len(fixedArray)` was dispatched to str-len and rejected with
		// "unsupported receiver type [N x E]" (e.g. tests/1.no: `len(["a","b","c"])`).
		n, ok := arraySizeOf(lt)
		if !ok {
			c.fail("builtin %s: cannot compute size of fixed-array receiver %s", ff, lt)
			return fmt.Errorf("builtin %s fixed-array %s", ff, lt)
		}
		if inst.Dst <= NoVal {
			return nil
		}
		dstLT, _ := c.ptype(inst.Dst)
		dstSlot := c.valSlot[inst.Dst]
		if dstSlot == "" {
			return fmt.Errorf("builtin %s: no result slot", ff)
		}
		v := c.coerce("i64", fmt.Sprintf("%d", n), dstLT)
		if v == "" {
			return fmt.Errorf("builtin %s: cannot store i64 into %s", ff, dstLT)
		}
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, v, dstLT, dstSlot))
		return nil
	}
	if lt == "%txt" {
		// %txt = type { [255 x i8], i8 }; field 1 is the i8 length byte.
		// str-len-bytes on a %txt returns its byte length, mirroring
		// emitLenCap's txt.len path and the legacy backend (which reads the
		// i8 len field). Without this, `s.str-len-bytes()` on a %txt receiver
		// was rejected with "unsupported receiver type %txt" under MIR=3.
		g := c.treg("lg")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 1\n", g, lt, lt, slot))
		l := c.treg("ll")
		c.sb.WriteString(fmt.Sprintf("  %s = load i8, i8* %s\n", l, g))
		z := c.treg("lz")
		c.sb.WriteString(fmt.Sprintf("  %s = zext i8 %s to i64\n", z, l))
		if inst.Dst <= NoVal {
			return nil
		}
		dstLT, _ := c.ptype(inst.Dst)
		dstSlot := c.valSlot[inst.Dst]
		if dstSlot == "" {
			return fmt.Errorf("builtin %s: no result slot", ff)
		}
		v := c.coerce("i64", z, dstLT)
		if v == "" {
			return fmt.Errorf("builtin %s: cannot store i64 into %s", ff, dstLT)
		}
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, v, dstLT, dstSlot))
		return nil
	}
	if lt != "%str-long" && lt != "%vec" {
		c.fail("builtin %s: unsupported receiver type %s", ff, lt)
		return fmt.Errorf("builtin %s receiver %s: %s", ff, lt, strings.Join(c.errs, "; "))
	}
	if inst.Dst <= NoVal {
		return nil
	}
	dstLT, _ := c.ptype(inst.Dst)
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("builtin %s: no result slot", ff)
	}
	g := c.treg("lg")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n", g, lt, lt, slot))
	l := c.treg("ll")
	c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", l, g))
	v := c.coerce("i64", l, dstLT)
	if v == "" {
		return fmt.Errorf("builtin %s: cannot store i64 into %s", ff, dstLT)
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, v, dstLT, dstSlot))
	return nil
}

// strConst materializes a compile-time string literal as a heap %str-long via
// @str_from_const (which mallocs). The module-level global is deduped by content
// (a private constant may only be defined once), but the resulting VALUE register
// is always fresh: LLVM registers are function-scoped, so reusing one across
// functions would reference an undefined value in every function after the first.
func (c *codegen) strConst(s string) string {
	if c.strConsts == nil {
		c.strConsts = map[string]string{}
	}
	name, ok := c.strConsts[s]
	if !ok {
		name = fmt.Sprintf("@.mir.cs.%d", len(c.strConsts))
		c.strConsts[s] = name
		c.global(fmt.Sprintf("%s = private constant [%d x i8] c\"%s\"", name, len(s), dataStr(s)))
	}
	r := c.treg("csc")
	c.sb.WriteString(fmt.Sprintf("  %s = call %%str-long @str_from_const(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i64 0, i64 0), i64 %d)\n", r, len(s), len(s), name, len(s)))
	return r
}

// emitBuiltinStrToBool lowers `str.to-bool()`, replicating the nolang std body
// (src/std/str.no) inline:
//
//	""        -> nil      (tag=1)
//	"true"    -> ok(true) (tag=0, payload=1)
//	"false"   -> ok(false)(tag=0, payload=0)
//	anything else -> err  (tag=2)
//
// The option discriminant convention is tag 0 = some/ok, 1 = nil/none, 2 = err
// (see docs/MIR_DESIGN.md §runtime). The previous implementation emitted
// tag=1 for the `ok` case — turning every `ok(true)`/`ok(false)` into `nil`,
// which corrupted `== nil` / `== err` comparisons and `print` of `?bool`
// (the §16 bool-print DIVERGE family: test-bool-debug, test-bool-direct,
// test-min-u8-bool.minimal). It also only compared against "true", so "false"
// / empty / non-bool strings were misclassified. This version compares against
// both literals, inspects the receiver length for the empty case, and selects
// the correct tag/payload for all four outcomes.
func (c *codegen) emitBuiltinStrToBool(inst *Inst) error {
	if len(inst.Args) == 0 {
		return fmt.Errorf("str.to-bool: missing receiver")
	}
	if inst.Dst <= NoVal {
		return nil
	}
	dstLT, _ := c.ptype(inst.Dst)
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("str.to-bool: no result slot")
	}
	rcvTy, rcvV := c.loadVal(inst.Args[0])
	eqTrue := c.treg("stbt")
	c.sb.WriteString(fmt.Sprintf("  %s = call i1 @str_eq(%s %s, %%str-long %s)\n", eqTrue, rcvTy, rcvV, c.strConst("true")))
	eqFalse := c.treg("stbf")
	c.sb.WriteString(fmt.Sprintf("  %s = call i1 @str_eq(%s %s, %%str-long %s)\n", eqFalse, rcvTy, rcvV, c.strConst("false")))
	// Receiver length is the first i64 field of %str-long; empty string -> nil.
	rcvLen := c.treg("stbl")
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", rcvLen, rcvTy, rcvV))
	isEmpty := c.treg("stbe")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i64 %s, 0\n", isEmpty, rcvLen))
	// tag: 0=some/ok, 1=nil/none, 2=err.
	tagElse := c.treg("stbtg0")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 1, i64 2\n", tagElse, isEmpty))
	tagNoTrue := c.treg("stbtg1")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 0, i64 %s\n", tagNoTrue, eqFalse, tagElse))
	tag := c.treg("stbtg")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 0, i64 %s\n", tag, eqTrue, tagNoTrue))
	// payload: 1 for "true", 0 otherwise (ok(false)/nil/err all use 0).
	payload := c.treg("stbp")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 1, i64 0\n", payload, eqTrue))
	optStore := func(lt string) {
		s0 := c.treg("stbs0")
		if lt == "%option" {
			c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%option { i64 0, i64 0 }, i64 %s, 0\n", s0, tag))
		} else {
			c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %s zeroinitializer, i64 %s, 0\n", s0, lt, tag))
		}
		s1 := c.treg("stbs1")
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i64 %s, 1\n", s1, lt, s0, payload))
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", lt, s1, lt, dstSlot))
	}
	if dstLT == "%option" || strings.HasPrefix(dstLT, "%option_") {
		optStore(dstLT)
		return nil
	}
	v := c.coerce("i1", eqTrue, dstLT)
	if v == "" {
		return fmt.Errorf("str.to-bool: cannot coerce i1 to %s", dstLT)
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, v, dstLT, dstSlot))
	return nil
}

// emitBuiltinBoolToStr lowers `bool.to-str()`: selects the "true"/"false"
// literal by the receiver's truth value.
func (c *codegen) emitBuiltinBoolToStr(inst *Inst) error {
	if len(inst.Args) == 0 {
		return fmt.Errorf("bool.to-str: missing receiver")
	}
	if inst.Dst <= NoVal {
		return nil
	}
	dstLT, _ := c.ptype(inst.Dst)
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("bool.to-str: no result slot")
	}
	rcvTy, rcvV := c.loadVal(inst.Args[0])
	var cond string
	if rcvTy == "i1" {
		cond = rcvV
	} else {
		cmp := c.treg("btsc")
		c.sb.WriteString(fmt.Sprintf("  %s = icmp ne %s %s, 0\n", cmp, rcvTy, rcvV))
		cond = cmp
	}
	trueV := c.strConst("true")
	falseV := c.strConst("false")
	sel := c.treg("btss")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, %%str-long %s, %%str-long %s\n", sel, cond, trueV, falseV))
	v := c.coerce("%str-long", sel, dstLT)
	if v == "" {
		return fmt.Errorf("bool.to-str: cannot coerce %%str-long to %s", dstLT)
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, v, dstLT, dstSlot))
	return nil
}

// resolveReceiverSlot returns the effective LLVM slot address for a builtin
// receiver value. In most cases this is simply c.valSlot[recv] — the alloca
// where the value lives. However, when the receiver is the result of an
// OpGetField (e.g. `m.rows.push(x)`), the alloca slot holds a *copy* of the
// field value; writing back to it silently discards the mutation (observed:
// `rows len: 0` after push). In that case we compute the GEP address of the
// struct field directly, so the builtin mutates the struct in place.
//
// The logic mirrors emitCallBody's OpGetField GEP optimization (see codegen.go
// around the "Exception: when the receiver is the result of OpGetField"
// comment). Returns the slot string (may be a GEP register) and a bool
// indicating whether a GEP was emitted (true) or the plain slot was used
// (false).
func (c *codegen) resolveReceiverSlot(recv ValueID) (string, bool) {
	slot := c.valSlot[recv]
	if slot == "" {
		return "", false
	}
	// Check whether this value was produced by OpGetField.
	defIID, ok := c.defInst[recv]
	if !ok {
		return slot, false
	}
	defInst := c.mod.Inst(defIID)
	if defInst == nil || defInst.Op != OpGetField {
		return slot, false
	}
	gfRecv := defInst.Args[0]
	gfFieldName := defInst.Str
	gfRecvSlot, ok2 := c.valSlot[gfRecv]
	if !ok2 || gfRecvSlot == "" {
		return slot, false
	}
	// Resolve the struct type and field index, mirroring emitGetField's lookup.
	gfRecvRaw := ""
	gfRecvLT := ""
	if fl := c.mod.Func(c.cf); fl != nil {
		if tid, ok3 := fl.LocalTypes[gfRecv]; ok3 {
			if t := c.mod.Type(tid); t != nil {
				gfRecvRaw = t.Raw
				gfRecvLT = c.llvmTypeOf(t)
			}
		}
	}
	if gfRecvLT == "" {
		if v := c.mod.Value(gfRecv); v != nil {
			if t := c.mod.Type(v.Type); t != nil {
				gfRecvRaw = t.Raw
				gfRecvLT = c.llvmTypeOf(t)
			}
		}
	}
	if gfRecvLT == "" {
		return slot, false
	}
	// Strip option wrapper if needed (mirrors emitGetField).
	if isOptionType(gfRecvLT) {
		elem, _ := parseOptionElem(gfRecvRaw)
		_, payloadLT := c.optionType(elem)
		innerRaw := elem
		structKey := c.structKeyOf(innerRaw)
		if structKey == "" {
			structKey = innerRaw
		}
		idx, ok3 := c.mod.FieldIndex(structKey, gfFieldName)
		if !ok3 {
			return slot, false
		}
		c.loadSeq++
		pg := fmt.Sprintf("%%brf%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 1\n", pg, gfRecvLT, gfRecvLT, gfRecvSlot))
		c.loadSeq++
		gep := fmt.Sprintf("%%brf%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", gep, payloadLT, payloadLT, pg, idx))
		return gep, true
	}
	gfRecvRaw = strings.TrimPrefix(gfRecvRaw, "?")
	structKey := c.structKeyOf(gfRecvRaw)
	if structKey == "" {
		structKey = gfRecvRaw
	}
	// Try container pseudo-fields first (len/cap/data).
	if idx, ok3 := containerFieldIndex(gfFieldName); ok3 {
		if t := c.mod.Type(c.mod.Value(gfRecv).Type); t != nil &&
			(t.Kind == KindSlice || t.Kind == KindStr) {
			c.loadSeq++
			gep := fmt.Sprintf("%%brf%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", gep, gfRecvLT, gfRecvLT, gfRecvSlot, idx))
			return gep, true
		}
	}
	// Regular struct field.
	if idx, ok3 := c.mod.FieldIndex(structKey, gfFieldName); ok3 {
		c.loadSeq++
		gep := fmt.Sprintf("%%brf%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", gep, gfRecvLT, gfRecvLT, gfRecvSlot, idx))
		return gep, true
	}
	// Field lookup failed: fall through to the normal slot.
	return slot, false
}

// emitBuiltinVecPush lowers `vec.push(x)`: append `x` to the receiver vector in
// place, growing the backing buffer (cap -> cap*2, or 1 when empty) when full.
// The receiver is `inst.Args[0]` (a %vec passed by its alloca slot so the
// mutation is visible to the caller) and the element is `inst.Args[1]`.
func (c *codegen) emitBuiltinVecPush(inst *Inst) error {
	if len(inst.Args) < 2 {
		return fmt.Errorf("vec.push: needs receiver and element")
	}
	recv := inst.Args[0]
	elem := inst.Args[1]
	slot, _ := c.resolveReceiverSlot(recv)
	if slot == "" {
		return fmt.Errorf("vec.push: no receiver slot")
	}
	elemTy, elemV := c.loadVal(elem)
	// 元素寬度與步長必須以**接收者宣告的元素型別**為準，不能以引數值的型別為準：
	// 整數字面量在 lowering 中一律降為 i64，若照引數型別推導，`[]byte.push(1)`
	// 會以 8 位元組步長 + `store i64` 寫入，而 []byte 的索引讀寫是 1 位元組步長
	// ——push 進去的資料整體錯位，只有第 0 個位元組看起來是對的（實測
	// `out.push(1); out.push(2); out.push(3)` → 01 00 00；全域與具名出參接收者
	// 皆然）。這正是「容器元素步長 = sizeof(元素型別)」不變量。
	//
	// 關鍵：這裡必須用**索引路徑的同一個解析函式** elemTypeOfReceiver
	// （emitIndex/emitIndexStore 用的就是它），而不是另寫一套映射。兩者不一致
	// 比「兩邊都錯」更糟：資料按 A 寬度寫、按 B 寬度讀，push 進去的元素會
	// 互相覆蓋。實測過的教訓：一度改用 elemInfoOfVec（它把 i16/i32 映射為
	// i16/i32），於是 []i16 變成 push 按 2 位元組、讀取按 8 位元組，w[0] 讀出
	// 0x02030102 這種「兩個相鄰元素拼在一起」的垃圾。MIR 後端的所有整數型別
	// 都是 i64（見 llvmTypeOf 的 KindInt 分支），elemTypeOfReceiver 正是這個
	// 事實的唯一出口，push 必須與它對齊。
	elemLL := c.elemTypeOfReceiver(recv)
	if elemTy != elemLL {
		if conv := c.coerce(elemTy, elemV, elemLL); conv != "" {
			elemV = conv
		} else {
			// 無法轉換的元素（結構體、%option 等）：沿用引數值型別，
			// 保持與索引路徑相同的位元組寬度假設。
			elemLL = elemTy
		}
	}
	// 步長 = sizeof(elemLL)。mirStaticTypeSize 覆蓋純量 + %str-long + %vec；
	// 其餘（使用者結構體）退回 8，與先前行為一致。
	stride := int64(8)
	if sz, ok := mirStaticTypeSize(elemLL); ok {
		stride = sz
	}
	elemTy = elemLL
	vv := c.treg("vpv")
	c.sb.WriteString(fmt.Sprintf("  %s = load %%vec, %%vec* %s\n", vv, slot))
	lenG := c.treg("vpl")
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 0\n", lenG, vv))
	capG := c.treg("vpc")
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 1\n", capG, vv))
	dataG := c.treg("vpd")
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 2\n", dataG, vv))
	// newCap = len==cap ? (cap==0 ? 1 : cap*2) : cap
	capZero := c.treg("vpcz")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i64 %s, 0\n", capZero, capG))
	capDouble := c.treg("vpcd")
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, 2\n", capDouble, capG))
	growCap := c.treg("vpgc")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 1, i64 %s\n", growCap, capZero, capDouble))
	needGrow := c.treg("vpng")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i64 %s, %s\n", needGrow, lenG, capG))
	newCap := c.treg("vpnc")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 %s\n", newCap, needGrow, growCap, capG))
	sz := c.treg("vpsz")
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", sz, newCap, stride))
	newBuf := c.treg("vpnb")
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", newBuf, sz))
	bytes := c.treg("vpby")
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", bytes, lenG, stride))
	srcPtr := c.treg("vpsp")
	c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", srcPtr, dataG))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", newBuf, srcPtr, bytes))
	c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", srcPtr))
	ebase := c.treg("vpeb")
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", ebase, newBuf, elemTy))
	// Owned-string element: the %str-long struct is stored by value into the
	// vec, so a plain `store` SHARES the temp's heap buffer. When the pushed
	// temp (e.g. a str.slice-bytes result) drops, the vec element dangles and
	// reads as garbage — the str.split -> parts[i] use-after-free corruption.
	// Deep-clone the data so the vec owns an independent buffer (mirrors the
	// legacy backend's vec.push, which takes ownership of a fresh copy).
	if elemTy == "%str-long" {
		cl := c.treg("vpc2")
		c.sb.WriteString(fmt.Sprintf("  %s = call %%str-long @str_clone(%%str-long %s)\n", cl, elemV))
		elemV = cl
	}
	eptr := c.treg("vpep")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 %s\n", eptr, elemTy, elemTy, ebase, lenG))
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", elemTy, elemV, elemTy, eptr))
	newLen := c.treg("vpnl")
	c.sb.WriteString(fmt.Sprintf("  %s = add i64 %s, 1\n", newLen, lenG))
	dataI64 := c.treg("vpdi")
	c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", dataI64, newBuf))
	s0 := c.treg("vps0")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec { i64 0, i64 0, i64 0 }, i64 %s, 0\n", s0, newLen))
	s1 := c.treg("vps1")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 1\n", s1, s0, newCap))
	s2 := c.treg("vps2")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 2\n", s2, s1, dataI64))
	c.sb.WriteString(fmt.Sprintf("  store %%vec %s, %%vec* %s\n", s2, slot))
	return nil
}

// label mints a fresh, %-free basic-block label name (labels are written
// without a leading %, unlike value registers).
func (c *codegen) label(prefix string) string {
	c.loadSeq++
	return fmt.Sprintf("%s%d", prefix, c.loadSeq)
}

// elemInfoOfVec returns the LLVM element type and byte stride for a %vec
// receiver, derived from its nolang element type. Used by the in-place slice
// mutators (reverse/insert/remove/sort) to index individual elements.
func (c *codegen) elemInfoOfVec(recv ValueID) (elemLL string, stride int64) {
	raw := c.rawTypeOf(recv)
	elem := strings.TrimPrefix(raw, "[]")
	elem = strings.TrimPrefix(elem, "?")
	switch elem {
	case "i8", "u8", "byte", "bool":
		return "i8", 1
	case "i16", "u16":
		return "i16", 2
	case "i32", "u32":
		return "i32", 4
	case "i64", "u64":
		return "i64", 8
	case "i128", "u128":
		return "i128", 16
	case "f32":
		return "float", 4
	case "f64":
		return "double", 8
	case "str":
		return "%str-long", 24
	default:
		return "i64", 8
	}
}

// emitLessThan emits `a < b` for two values of element LLVM type elemLL and
// returns the i1 register. Signed/unsigned/float/str are handled per type.
func (c *codegen) emitLessThan(a, b, elemLL string) string {
	r := c.treg("lt")
	switch elemLL {
	case "i8", "i16", "i32", "i64", "i128":
		c.sb.WriteString(fmt.Sprintf("  %s = icmp slt %s %s, %s\n", r, elemLL, a, b))
	case "u8", "u16", "u32", "u64":
		c.sb.WriteString(fmt.Sprintf("  %s = icmp ult %s %s, %s\n", r, elemLL, a, b))
	case "float", "double":
		c.sb.WriteString(fmt.Sprintf("  %s = fcmp olt %s %s, %s\n", r, elemLL, a, b))
	case "%str-long":
		cmp := c.treg("slc")
		c.sb.WriteString(fmt.Sprintf("  %s = call i64 @str_cmp(%s %s, %s %s)\n", cmp, elemLL, a, elemLL, b))
		c.sb.WriteString(fmt.Sprintf("  %s = icmp slt i64 %s, 0\n", r, cmp))
	default:
		c.sb.WriteString(fmt.Sprintf("  %s = icmp slt i64 %s, %s\n", r, a, b))
	}
	return r
}

// emitBuiltinVecClear lowers `vec.clear()`: set len=0 in place. cap/data are
// left unchanged, so the backing buffer is reused; only the logical length is
// reset. The receiver is read/written through its alloca slot, so the caller
// observes the mutation (this is the by-reference contract the by-ref fix
// restored for method receivers).
func (c *codegen) emitBuiltinVecClear(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("vec.clear: needs receiver")
	}
	slot, _ := c.resolveReceiverSlot(inst.Args[0])
	if slot == "" {
		return fmt.Errorf("vec.clear: no receiver slot")
	}
	vv := c.treg("clv")
	c.sb.WriteString(fmt.Sprintf("  %s = load %%vec, %%vec* %s\n", vv, slot))
	z := c.treg("clz")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 0, 0\n", z, vv))
	c.sb.WriteString(fmt.Sprintf("  store %%vec %s, %%vec* %s\n", z, slot))
	return nil
}

// emitBuiltinStrClear lowers `str.clear()`: set the receiver's logical length to
// 0 in place (field 0 of %str-long), keeping the existing data pointer/capacity
// (mirrors the builtin comment "no storage switch" — str.clear does not free or
// reallocate, it only hides the contents). The receiver is read/written through
// its alloca slot, so the caller observes the mutation (by-reference contract).
func (c *codegen) emitBuiltinStrClear(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("str.clear: needs receiver")
	}
	slot, _ := c.resolveReceiverSlot(inst.Args[0])
	if slot == "" {
		return fmt.Errorf("str.clear: no receiver slot")
	}
	vv := c.treg("clv")
	c.sb.WriteString(fmt.Sprintf("  %s = load %%str-long, %%str-long* %s\n", vv, slot))
	z := c.treg("clz")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long %s, i64 0, 0\n", z, vv))
	c.sb.WriteString(fmt.Sprintf("  store %%str-long %s, %%str-long* %s\n", z, slot))
	return nil
}

// emitBuiltinArrZero lowers `.zero()`: zero the receiver's backing memory.
//   - fixed array [N x E] (by-value): memset the whole slot (N*sizeof(E) bytes).
//   - slice []E (%vec): memset len*stride bytes of the heap data buffer.
//
// The receiver is inst.Args[0]; its alloca slot (c.valSlot) is the memory the
// caller observes, so mutating it in place matches the by-reference method
// contract (and the legacy genArrZero behaviour).
func (c *codegen) emitBuiltinArrZero(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("arr.zero: needs receiver")
	}
	recv := inst.Args[0]
	slot, _ := c.resolveReceiverSlot(recv)
	if slot == "" {
		return fmt.Errorf("arr.zero: no receiver slot")
	}
	raw := c.rawTypeOf(recv)
	if strings.HasPrefix(raw, "[]") {
		// Slice: zero len*stride bytes of the backing buffer (field 2), like
		// the legacy %vec branch of genArrZero.
		_, stride := c.elemInfoOfVec(recv)
		c.loadSeq++
		lg := fmt.Sprintf("%%azlg%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", lg, slot))
		c.loadSeq++
		lv := fmt.Sprintf("%%azlv%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", lv, lg))
		c.loadSeq++
		dg := fmt.Sprintf("%%azdg%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", dg, slot))
		c.loadSeq++
		dv := fmt.Sprintf("%%azdv%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", dv, dg))
		c.loadSeq++
		dp := fmt.Sprintf("%%azdp%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", dp, dv))
		c.loadSeq++
		tb := fmt.Sprintf("%%aztb%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", tb, lv, stride))
		c.sb.WriteString(fmt.Sprintf("  call void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 %s, i1 false)\n", dp, tb))
		return nil
	}
	// Fixed array (by-value [N x E]): the LLVM type (e.g. "[4 x i64]") is the
	// slot's alloca element type. Compute its byte size with a one-element GEP
	// (slot+1 minus slot) so we never have to parse N/E by hand, then memset.
	ll, _ := c.ptype(recv)
	c.loadSeq++
	stI := fmt.Sprintf("%%azst%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint %s* %s to i64\n", stI, ll, slot))
	c.loadSeq++
	endG := fmt.Sprintf("%%azeg%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr %s, %s* %s, i64 1\n", endG, ll, ll, slot))
	c.loadSeq++
	endI := fmt.Sprintf("%%azei%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint %s* %s to i64\n", endI, ll, endG))
	c.loadSeq++
	nb := fmt.Sprintf("%%aznb%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = sub i64 %s, %s\n", nb, endI, stI))
	c.loadSeq++
	bc := fmt.Sprintf("%%azbc%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", bc, ll, slot))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 %s, i1 false)\n", bc, nb))
	return nil
}

// emitBuiltinVecTruncate lowers `vec.truncate(n)`: len = min(len, n). The
// backing buffer (cap/data) is preserved.
func (c *codegen) emitBuiltinVecTruncate(inst *Inst) error {
	if len(inst.Args) < 2 {
		return nil
	}
	recv := inst.Args[0]
	slot, _ := c.resolveReceiverSlot(recv)
	if slot == "" {
		return fmt.Errorf("vec.truncate: no receiver slot")
	}
	nT, nV := c.loadVal(inst.Args[1])
	n := nV
	if r := c.coerce(nT, nV, "i64"); r != "" {
		n = r
	}
	vv := c.treg("tgv")
	c.sb.WriteString(fmt.Sprintf("  %s = load %%vec, %%vec* %s\n", vv, slot))
	lenG := c.treg("tgl")
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 0\n", lenG, vv))
	cmp := c.treg("tgc")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ult i64 %s, %s\n", cmp, n, lenG))
	newLen := c.treg("tgn")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 %s\n", newLen, cmp, n, lenG))
	s0 := c.treg("tgs")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 0\n", s0, vv, newLen))
	c.sb.WriteString(fmt.Sprintf("  store %%vec %s, %%vec* %s\n", s0, slot))
	return nil
}

// emitBuiltinVecPop lowers `vec.pop()`: remove and return the last element.
// On an empty slice the result is the element zero value and len stays 0.
func (c *codegen) emitBuiltinVecPop(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("vec.pop: needs receiver")
	}
	recv := inst.Args[0]
	slot, _ := c.resolveReceiverSlot(recv)
	if slot == "" {
		return fmt.Errorf("vec.pop: no receiver slot")
	}
	elemLL, _ := c.elemInfoOfVec(recv)
	vv := c.treg("ppv")
	c.sb.WriteString(fmt.Sprintf("  %s = load %%vec, %%vec* %s\n", vv, slot))
	lenG := c.treg("ppl")
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 0\n", lenG, vv))
	dataG := c.treg("ppd")
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 2\n", dataG, vv))
	base := c.treg("ppb")
	c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to %s*\n", base, dataG, elemLL))
	last := c.treg("ppla")
	c.sb.WriteString(fmt.Sprintf("  %s = sub i64 %s, 1\n", last, lenG))
	z := c.treg("ppz")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i64 %s, 0\n", z, lenG))
	maxIdx := c.treg("ppmi")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 0, i64 %s\n", maxIdx, z, last))
	eptr := c.treg("ppep")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 %s\n", eptr, elemLL, elemLL, base, maxIdx))
	elemV := c.treg("ppev")
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", elemV, elemLL, elemLL, eptr))
	dstV := c.treg("ppdv")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, %s zeroinitializer, %s %s\n", dstV, z, elemLL, elemLL, elemV))
	if inst.Dst > NoVal {
		if ds := c.valSlot[inst.Dst]; ds != "" {
			c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", elemLL, dstV, elemLL, ds))
		}
	}
	newLen := c.treg("ppnl")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 0, i64 %s\n", newLen, z, last))
	s0 := c.treg("pps")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 0\n", s0, vv, newLen))
	c.sb.WriteString(fmt.Sprintf("  store %%vec %s, %%vec* %s\n", s0, slot))
	return nil
}

// emitBuiltinVecReverse lowers `vec.reverse()`: swap elements in place via a
// single forward loop (i from 0 while i < len-1-i). The backing buffer is
// mutated through its inttoptr'd element pointer; len/cap/data are unchanged.
func (c *codegen) emitBuiltinVecReverse(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("vec.reverse: needs receiver")
	}
	recv := inst.Args[0]
	slot, _ := c.resolveReceiverSlot(recv)
	if slot == "" {
		return fmt.Errorf("vec.reverse: no receiver slot")
	}
	elemLL, _ := c.elemInfoOfVec(recv)
	vv := c.treg("rvv")
	c.sb.WriteString(fmt.Sprintf("  %s = load %%vec, %%vec* %s\n", vv, slot))
	lenG := c.treg("rvl")
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 0\n", lenG, vv))
	dataG := c.treg("rvd")
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 2\n", dataG, vv))
	base := c.treg("rvb")
	c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to %s*\n", base, dataG, elemLL))
	skip := c.treg("rvs")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ult i64 %s, 2\n", skip, lenG))
	lDone := c.label("rvd")
	lBody := c.label("rvb")
	lHead := c.label("rvh")
	lSwap := c.label("rvs")
	lTail := c.label("rvt")
	iNext := c.treg("rvn")
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", skip, lDone, lBody))
	c.sb.WriteString(fmt.Sprintf("%s:\n", lBody))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", lHead))
	c.sb.WriteString(fmt.Sprintf("%s:\n", lHead))
	iR := c.treg("rvi")
	c.sb.WriteString(fmt.Sprintf("  %s = phi i64 [ 0, %%%s ], [ %s, %%%s ]\n", iR, lBody, iNext, lTail))
	jR := c.treg("rvj")
	c.sb.WriteString(fmt.Sprintf("  %s = sub i64 %s, %s, 1\n", jR, lenG, iR))
	cmpI := c.treg("rvci")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ult i64 %s, %s\n", cmpI, iR, jR))
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", cmpI, lSwap, lDone))
	c.sb.WriteString(fmt.Sprintf("%s:\n", lSwap))
	pa := c.treg("rvpa")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 %s\n", pa, elemLL, elemLL, base, iR))
	pb := c.treg("rvpb")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 %s\n", pb, elemLL, elemLL, base, jR))
	va := c.treg("rvva")
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", va, elemLL, elemLL, pa))
	vb := c.treg("rvvb")
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", vb, elemLL, elemLL, pb))
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", elemLL, vb, elemLL, pa))
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", elemLL, va, elemLL, pb))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", lTail))
	c.sb.WriteString(fmt.Sprintf("%s:\n", lTail))
	iNext = c.treg("rvn")
	c.sb.WriteString(fmt.Sprintf("  %s = add i64 %s, 1\n", iNext, iR))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", lHead))
	c.sb.WriteString(fmt.Sprintf("%s:\n", lDone))
	return nil
}

// emitBuiltinVecInsert lowers `vec.insert(idx, x)`: grow (mirroring push) if the
// backing buffer is full, then copy [0..idx) and [idx..len) around the new
// element via two memcpy's. idx is clamped to [0, len].
func (c *codegen) emitBuiltinVecInsert(inst *Inst) error {
	if len(inst.Args) < 3 {
		return fmt.Errorf("vec.insert: needs receiver, index, element")
	}
	recv := inst.Args[0]
	slot, _ := c.resolveReceiverSlot(recv)
	if slot == "" {
		return fmt.Errorf("vec.insert: no receiver slot")
	}
	elemLL, stride := c.elemInfoOfVec(recv)
	idxT, idxV := c.loadVal(inst.Args[1])
	idx := idxV
	if r := c.coerce(idxT, idxV, "i64"); r != "" {
		idx = r
	}
	_, elemV := c.loadVal(inst.Args[2])
	vv := c.treg("ivv")
	c.sb.WriteString(fmt.Sprintf("  %s = load %%vec, %%vec* %s\n", vv, slot))
	lenG := c.treg("ivl")
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 0\n", lenG, vv))
	capG := c.treg("ivc")
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 1\n", capG, vv))
	dataG := c.treg("ivd")
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 2\n", dataG, vv))
	capZero := c.treg("ivcz")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i64 %s, 0\n", capZero, capG))
	capDouble := c.treg("ivcd")
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, 2\n", capDouble, capG))
	growCap := c.treg("ivgc")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 1, i64 %s\n", growCap, capZero, capDouble))
	needGrow := c.treg("ivng")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i64 %s, %s\n", needGrow, lenG, capG))
	newCap := c.treg("ivnc")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 %s\n", newCap, needGrow, growCap, capG))
	newLen := c.treg("ivnl")
	c.sb.WriteString(fmt.Sprintf("  %s = add i64 %s, 1\n", newLen, lenG))
	sz := c.treg("ivsz")
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", sz, newCap, stride))
	newBuf := c.treg("ivnb")
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", newBuf, sz))
	srcPtr := c.treg("ivsp")
	c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", srcPtr, dataG))
	idxLo := c.treg("ivil")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp slt i64 %s, 0\n", idxLo, idx))
	idxClamp := c.treg("ivic")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 0, i64 %s\n", idxClamp, idxLo, idx))
	idxHi := c.treg("ivih")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sgt i64 %s, %s\n", idxHi, idxClamp, lenG))
	idxClamp2 := c.treg("ivic2")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 %s\n", idxClamp2, idxHi, lenG, idxClamp))
	bytes1 := c.treg("ivb1")
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", bytes1, idxClamp2, stride))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", newBuf, srcPtr, bytes1))
	ebase := c.treg("iveb")
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", ebase, newBuf, elemLL))
	// Owned-string element: clone before storing into the vec (same reasoning
	// as emitBuiltinVecPush — a by-value store shares the temp's buffer, which
	// the temp's drop would free out from under the vec element).
	if elemLL == "%str-long" {
		cl := c.treg("ivc2")
		c.sb.WriteString(fmt.Sprintf("  %s = call %%str-long @str_clone(%%str-long %s)\n", cl, elemV))
		elemV = cl
	}
	eptr := c.treg("ivep")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 %s\n", eptr, elemLL, elemLL, ebase, idxClamp2))
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", elemLL, elemV, elemLL, eptr))
	restOff := c.treg("ivro")
	c.sb.WriteString(fmt.Sprintf("  %s = add i64 %s, 1\n", restOff, idxClamp2))
	srcRestOff := c.treg("ivsr")
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", srcRestOff, idxClamp2, stride))
	dstRestOff := c.treg("ivdr")
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", dstRestOff, restOff, stride))
	restCount := c.treg("ivrc")
	c.sb.WriteString(fmt.Sprintf("  %s = sub i64 %s, %s\n", restCount, lenG, idxClamp2))
	restBytes := c.treg("ivrb")
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", restBytes, restCount, stride))
	srcRest := c.treg("ivsr2")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", srcRest, srcPtr, srcRestOff))
	dstRest := c.treg("ivdr2")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", dstRest, newBuf, dstRestOff))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", dstRest, srcRest, restBytes))
	c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", srcPtr))
	dataI64 := c.treg("ivdi")
	c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", dataI64, newBuf))
	s0 := c.treg("ivs0")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec { i64 0, i64 0, i64 0 }, i64 %s, 0\n", s0, newLen))
	s1 := c.treg("ivs1")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 1\n", s1, s0, newCap))
	s2 := c.treg("ivs2")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 2\n", s2, s1, dataI64))
	c.sb.WriteString(fmt.Sprintf("  store %%vec %s, %%vec* %s\n", s2, slot))
	return nil
}

// emitBuiltinVecRemove lowers `vec.remove(idx)`: copy [0..idx) and [idx+1..len)
// around the removed element, free the old buffer, and return the element.
// idx is clamped to [0, len]; newLen = max(0, len-1).
func (c *codegen) emitBuiltinVecRemove(inst *Inst) error {
	if len(inst.Args) < 2 {
		return fmt.Errorf("vec.remove: needs receiver, index")
	}
	recv := inst.Args[0]
	slot, _ := c.resolveReceiverSlot(recv)
	if slot == "" {
		return fmt.Errorf("vec.remove: no receiver slot")
	}
	elemLL, stride := c.elemInfoOfVec(recv)
	idxT, idxV := c.loadVal(inst.Args[1])
	idx := idxV
	if r := c.coerce(idxT, idxV, "i64"); r != "" {
		idx = r
	}
	vv := c.treg("rmv")
	c.sb.WriteString(fmt.Sprintf("  %s = load %%vec, %%vec* %s\n", vv, slot))
	lenG := c.treg("rml")
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 0\n", lenG, vv))
	dataG := c.treg("rmd")
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 2\n", dataG, vv))
	base := c.treg("rmb")
	c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to %s*\n", base, dataG, elemLL))
	eptr := c.treg("rmep")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 %s\n", eptr, elemLL, elemLL, base, idx))
	removed := c.treg("rmrv")
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", removed, elemLL, elemLL, eptr))
	if inst.Dst > NoVal {
		if ds := c.valSlot[inst.Dst]; ds != "" {
			c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", elemLL, removed, elemLL, ds))
		}
	}
	z := c.treg("rmz")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i64 %s, 0\n", z, lenG))
	dec := c.treg("rmd2")
	c.sb.WriteString(fmt.Sprintf("  %s = sub i64 %s, 1\n", dec, lenG))
	newLen := c.treg("rmnl")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 0, i64 %s\n", newLen, z, dec))
	srcPtr := c.treg("rmsp")
	c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", srcPtr, dataG))
	sz := c.treg("rmsz")
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", sz, newLen, stride))
	newBuf := c.treg("rmnb")
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", newBuf, sz))
	idxLo := c.treg("rmil")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp slt i64 %s, 0\n", idxLo, idx))
	idxClamp := c.treg("rmic")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 0, i64 %s\n", idxClamp, idxLo, idx))
	idxHi := c.treg("rmih")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sgt i64 %s, %s\n", idxHi, idxClamp, lenG))
	idxClamp2 := c.treg("rmic2")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 %s\n", idxClamp2, idxHi, lenG, idxClamp))
	bytes1 := c.treg("rmb1")
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", bytes1, idxClamp2, stride))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", newBuf, srcPtr, bytes1))
	restCount := c.treg("rmrc")
	c.sb.WriteString(fmt.Sprintf("  %s = sub i64 %s, %s, 1\n", restCount, lenG, idxClamp2))
	restNeg := c.treg("rmrn")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp slt i64 %s, 0\n", restNeg, restCount))
	restCountC := c.treg("rmrcc")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 0, i64 %s\n", restCountC, restNeg, restCount))
	restBytes := c.treg("rmrb")
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", restBytes, restCountC, stride))
	srcRestIdx := c.treg("rmsri")
	c.sb.WriteString(fmt.Sprintf("  %s = add i64 %s, 1\n", srcRestIdx, idxClamp2))
	srcRestOff := c.treg("rmsr")
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", srcRestOff, srcRestIdx, stride))
	srcRest := c.treg("rmsr2")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", srcRest, srcPtr, srcRestOff))
	dstRestOff := c.treg("rmdr")
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", dstRestOff, idxClamp2, stride))
	dstRest := c.treg("rmdr2")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", dstRest, newBuf, dstRestOff))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", dstRest, srcRest, restBytes))
	c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", srcPtr))
	dataI64 := c.treg("rmdi")
	c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", dataI64, newBuf))
	s0 := c.treg("rms0")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec { i64 0, i64 0, i64 0 }, i64 %s, 0\n", s0, newLen))
	s1 := c.treg("rms1")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 1\n", s1, s0, newLen))
	s2 := c.treg("rms2")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 2\n", s2, s1, dataI64))
	c.sb.WriteString(fmt.Sprintf("  store %%vec %s, %%vec* %s\n", s2, slot))
	return nil
}

// emitBuiltinVecSort lowers `vec.sort-asc`/`vec.sort-desc`: an in-place bubble
// sort over the backing buffer. Elements are compared via c.emitLessThan and
// swapped with element-sized llvm.memcpy moves through a stack temp, so
// ownership is preserved for any element type (including %str-long). len/cap/data
// are unchanged (the buffer is merely reordered in place). asc=true yields
// ascending order.
func (c *codegen) emitBuiltinVecSort(inst *Inst, asc bool) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("vec.sort: needs receiver")
	}
	recv := inst.Args[0]
	slot, _ := c.resolveReceiverSlot(recv)
	if slot == "" {
		return fmt.Errorf("vec.sort: no receiver slot")
	}
	elemLL, stride := c.elemInfoOfVec(recv)
	vv := c.treg("stv")
	c.sb.WriteString(fmt.Sprintf("  %s = load %%vec, %%vec* %s\n", vv, slot))
	lenG := c.treg("stl")
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 0\n", lenG, vv))
	dataG := c.treg("std")
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 2\n", dataG, vv))
	base := c.treg("stb")
	c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", base, dataG))
	tmp := c.treg("stt")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca i8, i64 %d\n", tmp, stride))
	nMinus1 := c.treg("stn1")
	c.sb.WriteString(fmt.Sprintf("  %s = sub i64 %s, 1\n", nMinus1, lenG))
	small := c.treg("sts")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ult i64 %s, 2\n", small, lenG))
	// Pre-allocate the loop-increment register names so the phi nodes below can
	// reference them before their textual definition (matches the forward-phi
	// pattern used by emitBuiltinVecReverse).
	iInc := c.treg("stii")
	jInc := c.treg("stji")
	lSmall := c.label("sts")
	lOuterInit := c.label("sto")
	lOuterHead := c.label("sth")
	lInnerInit := c.label("sti")
	lInnerHead := c.label("stj")
	lInnerBody := c.label("stB")
	lInnerSwap := c.label("stS")
	lInnerTail := c.label("stT")
	lOuterLatch := c.label("stL")
	lDone := c.label("stD")
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", small, lSmall, lOuterInit))
	c.sb.WriteString(fmt.Sprintf("%s:\n", lSmall))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", lDone))
	c.sb.WriteString(fmt.Sprintf("%s:\n", lOuterInit))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", lOuterHead))
	c.sb.WriteString(fmt.Sprintf("%s:\n", lOuterHead))
	iPhi := c.treg("sti")
	c.sb.WriteString(fmt.Sprintf("  %s = phi i64 [ 0, %%%s ], [ %s, %%%s ]\n", iPhi, lOuterInit, iInc, lOuterLatch))
	outerDone := c.treg("stod")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp uge i64 %s, %s\n", outerDone, iPhi, nMinus1))
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", outerDone, lDone, lInnerInit))
	c.sb.WriteString(fmt.Sprintf("%s:\n", lInnerInit))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", lInnerHead))
	c.sb.WriteString(fmt.Sprintf("%s:\n", lInnerHead))
	jPhi := c.treg("stj")
	c.sb.WriteString(fmt.Sprintf("  %s = phi i64 [ 0, %%%s ], [ %s, %%%s ]\n", jPhi, lInnerInit, jInc, lInnerTail))
	maxJ := c.treg("stmj")
	c.sb.WriteString(fmt.Sprintf("  %s = sub i64 %s, %s\n", maxJ, nMinus1, iPhi))
	jOk := c.treg("stjo")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ult i64 %s, %s\n", jOk, jPhi, maxJ))
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", jOk, lInnerBody, lOuterLatch))
	c.sb.WriteString(fmt.Sprintf("%s:\n", lInnerBody))
	tbase := c.treg("sttb")
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", tbase, base, elemLL))
	jOff := c.treg("stof")
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", jOff, jPhi, stride))
	j1Off := c.treg("stof1")
	c.sb.WriteString(fmt.Sprintf("  %s = add i64 %s, %d\n", j1Off, jOff, stride))
	addrJ := c.treg("staj")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", addrJ, base, jOff))
	addrJ1 := c.treg("staj1")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", addrJ1, base, j1Off))
	jPlus1 := c.treg("stjp")
	c.sb.WriteString(fmt.Sprintf("  %s = add i64 %s, 1\n", jPlus1, jPhi))
	pa := c.treg("stpa")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 %s\n", pa, elemLL, elemLL, tbase, jPhi))
	pb := c.treg("stpb")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 %s\n", pb, elemLL, elemLL, tbase, jPlus1))
	aEl := c.treg("stea")
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", aEl, elemLL, elemLL, pa))
	bEl := c.treg("steb")
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", bEl, elemLL, elemLL, pb))
	var cond string
	if asc {
		cond = c.emitLessThan(bEl, aEl, elemLL)
	} else {
		cond = c.emitLessThan(aEl, bEl, elemLL)
	}
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", cond, lInnerSwap, lInnerTail))
	c.sb.WriteString(fmt.Sprintf("%s:\n", lInnerSwap))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %d, i1 false)\n", tmp, addrJ, stride))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %d, i1 false)\n", addrJ, addrJ1, stride))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %d, i1 false)\n", addrJ1, tmp, stride))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", lInnerTail))
	c.sb.WriteString(fmt.Sprintf("%s:\n", lInnerTail))
	c.sb.WriteString(fmt.Sprintf("  %s = add i64 %s, 1\n", jInc, jPhi))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", lInnerHead))
	c.sb.WriteString(fmt.Sprintf("%s:\n", lOuterLatch))
	c.sb.WriteString(fmt.Sprintf("  %s = add i64 %s, 1\n", iInc, iPhi))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", lOuterHead))
	c.sb.WriteString(fmt.Sprintf("%s:\n", lDone))
	return nil
}

// emitBuiltinUname lowers `os.uname()` -> utsname. The C struct utsname is a
// packed array of five NUL-terminated char fields (sysname, nodename, release,
// version, machine), each _UTSNAME_LENGTH bytes wide (256 on macOS, 65 on
// Linux). We alloca the C buffer, call uname(buf), then adopt each field as an
// owned %str-long via @str_from_cstr and store it into the result %utsname
// struct. This mirrors the legacy call_stdlib.go uname inliner exactly.
func (c *codegen) emitBuiltinUname(inst *Inst) error {
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("uname: no result slot")
	}
	// The result struct is registered under its module-qualified name
	// (e.g. os.utsname -> %os_utsname); resolve it from the collected struct
	// fields rather than ptype, which only sees the unqualified result type
	// "utsname" and would miss the lookup.
	dstLT := c.structLLVMType("utsname")
	if dstLT == "" {
		return fmt.Errorf("uname: utsname struct type not collected")
	}

	fieldLen := int64(256)
	if runtime.GOOS == "linux" {
		fieldLen = 65
	}
	totalSize := fieldLen * 5
	offsets := [5]int64{0, fieldLen, fieldLen * 2, fieldLen * 3, fieldLen * 4}

	// declare i32 @uname(i8*)
	c.decl("declare i32 @uname(i8*)")

	unBuf := c.treg("unbuf")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca [%d x i8]\n", unBuf, totalSize))
	unBufPtr := c.treg("unbufp")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [%d x i8], [%d x i8]* %s, i64 0, i64 0\n", unBufPtr, totalSize, totalSize, unBuf))
	unRet := c.treg("unret")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @uname(i8* %s)\n", unRet, unBufPtr))

	for i := 0; i < 5; i++ {
		fldGEP := c.treg("unfld")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [%d x i8], [%d x i8]* %s, i64 0, i64 %d\n", fldGEP, totalSize, totalSize, unBuf, offsets[i]))
		strReg := c.treg("unstr")
		c.sb.WriteString(fmt.Sprintf("  %s = call %%str-long @str_from_cstr(i8* %s)\n", strReg, fldGEP))
		dstGEP := c.treg("undst")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", dstGEP, dstLT, dstLT, dstSlot, i))
		c.sb.WriteString(fmt.Sprintf("  store %%str-long %s, %%str-long* %s\n", strReg, dstGEP))
	}
	return nil
}

// emitBuiltinNetIcmpOpen lowers `net.net-icmp-open()` -> fd i64: create an ICMP
// socket for ping.
//
//	macOS: socket(AF_INET, SOCK_DGRAM=2, IPPROTO_ICMP=1) — unprivileged.
//	Linux: socket(AF_INET, SOCK_RAW=3,  IPPROTO_ICMP=1) — needs CAP_NET_RAW.
//
// Returns the fd (>=0 on success, -1 on error). Mirrors the legacy backend
// (build/llvm/call_stdlib.go net-icmp-open), which emits the @socket call inline
// rather than through a C shim. The OS-specific sockType is chosen at codegen
// time from runtime.GOOS (the prelude is compiled for the host).
func (c *codegen) emitBuiltinNetIcmpOpen(inst *Inst) error {
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("net-icmp-open: no result slot")
	}
	dstLT, _ := c.ptype(inst.Dst)
	if dstLT == "" {
		dstLT = "i64"
	}
	sockType := int32(3) // SOCK_RAW (Linux)
	if runtime.GOOS == "darwin" {
		sockType = 2 // SOCK_DGRAM (macOS unprivileged ICMP)
	}
	c.decl("declare i32 @socket(i32, i32, i32)")
	sock := c.treg("icmp.sock")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @socket(i32 2, i32 %d, i32 1)\n", sock, sockType))
	fd := c.treg("icmp.fd")
	c.sb.WriteString(fmt.Sprintf("  %s = sext i32 %s to i64\n", fd, sock))
	if dstLT != "i64" {
		if r := c.coerce("i64", fd, dstLT); r != "" {
			fd = r
		}
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, fd, dstLT, dstSlot))
	return nil
}

// emitBuiltinNetDial lowers `net.net-dial(host, port)` -> fd i64: create a TCP
// client connection. Performs socket(AF_INET, SOCK_STREAM, 0) + inet_pton +
// connect. This mirrors the legacy backend's happy path for IP-literal hosts
// (build/llvm/call_stdlib.go net-dial); hostname DNS resolution via getaddrinfo
// is intentionally omitted for now — net-dial returns -1 for a non-IP-literal
// host until that fallback is ported. The sockaddr_in layout differs per OS:
//
//	darwin: sin_len@0=16, sin_family@1=AF_INET, sin_port@2, sin_addr@4
//	linux :                 sin_family@0=AF_INET, sin_port@2, sin_addr@4
func (c *codegen) emitBuiltinNetDial(inst *Inst) error {
	if len(inst.Args) < 2 {
		return fmt.Errorf("net-dial: needs (host, port)")
	}
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("net-dial: no result slot")
	}
	hostPtr := c.cstrOf(inst.Args[0])
	if hostPtr == "" {
		return fmt.Errorf("net-dial: cannot marshal host as C string")
	}
	_, portReg := c.loadVal(inst.Args[1])

	darwin := runtime.GOOS == "darwin"
	familyOff := int64(0)
	if darwin {
		familyOff = 1
	}
	portOff := int64(2)
	addrOff := int64(4)

	c.decl("declare i32 @socket(i32, i32, i32)")
	c.decl("declare i32 @connect(i32, i8*, i32)")
	c.decl("declare i32 @inet_pton(i32, i8*, i8*)")

	sock := c.treg("netd.sock")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @socket(i32 2, i32 1, i32 0)\n", sock))

	addr := c.treg("netd.addr")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca [16 x i8]\n", addr))
	addrp := c.treg("netd.addrp")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [16 x i8], [16 x i8]* %s, i64 0, i64 0\n", addrp, addr))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 16, i1 0)\n", addrp))

	if darwin {
		lenGEP := c.treg("netd.leng")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [16 x i8], [16 x i8]* %s, i64 0, i64 0\n", lenGEP, addr))
		c.sb.WriteString(fmt.Sprintf("  store i8 16, i8* %s\n", lenGEP))
	}

	famGEP := c.treg("netd.famg")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [16 x i8], [16 x i8]* %s, i64 0, i64 %d\n", famGEP, addr, familyOff))
	c.sb.WriteString(fmt.Sprintf("  store i8 2, i8* %s\n", famGEP))

	// sin_port = htons(port) = ((port & 0xff) << 8) | ((port >> 8) & 0xff)
	plo := c.treg("netd.plo")
	c.sb.WriteString(fmt.Sprintf("  %s = and i64 %s, 255\n", plo, portReg))
	plo8 := c.treg("netd.plo8")
	c.sb.WriteString(fmt.Sprintf("  %s = shl i64 %s, 8\n", plo8, plo))
	phi := c.treg("netd.phi")
	c.sb.WriteString(fmt.Sprintf("  %s = lshr i64 %s, 8\n", phi, portReg))
	phi8 := c.treg("netd.phi8")
	c.sb.WriteString(fmt.Sprintf("  %s = and i64 %s, 255\n", phi8, phi))
	pnet := c.treg("netd.pnet")
	c.sb.WriteString(fmt.Sprintf("  %s = or i64 %s, %s\n", pnet, plo8, phi8))
	pnet16 := c.treg("netd.pnet16")
	c.sb.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i16\n", pnet16, pnet))
	portGEP := c.treg("netd.portg")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [16 x i8], [16 x i8]* %s, i64 0, i64 %d\n", portGEP, addr, portOff))
	c.sb.WriteString(fmt.Sprintf("  store i16 %s, i16* %s\n", pnet16, portGEP))

	addrGEP := c.treg("netd.addrg")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [16 x i8], [16 x i8]* %s, i64 0, i64 %d\n", addrGEP, addr, addrOff))
	pton := c.treg("netd.pton")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @inet_pton(i32 2, i8* %s, i8* %s)\n", pton, hostPtr, addrGEP))
	c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", hostPtr))

	connRet := c.treg("netd.conn")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @connect(i32 %s, i8* %s, i32 16)\n", connRet, sock, addrp))

	// fd = (socket ok && connect ok) ? socket : -1
	sockOk := c.treg("netd.sok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sge i32 %s, 0\n", sockOk, sock))
	sock64 := c.treg("netd.s64")
	c.sb.WriteString(fmt.Sprintf("  %s = sext i32 %s to i64\n", sock64, sock))
	s1 := c.treg("netd.s1")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 -1\n", s1, sockOk, sock64))
	connOk := c.treg("netd.cok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sge i32 %s, 0\n", connOk, connRet))
	fd := c.treg("netd.fd")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 -1\n", fd, connOk, s1))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", fd, dstSlot))
	return nil
}

// emitBuiltinNetListen lowers `net.net-listen(host, port)` -> fd i64: create a
// TCP listening socket. Performs socket(AF_INET, SOCK_STREAM, 0) +
// setsockopt(SO_REUSEADDR) + bind + listen, mirroring the legacy backend
// (build/llvm/call_stdlib.go net-listen). The sockaddr_in layout is OS-specific
// exactly as in emitBuiltinNetDial. Returns -1 if any step fails.
func (c *codegen) emitBuiltinNetListen(inst *Inst) error {
	if len(inst.Args) < 2 {
		return fmt.Errorf("net-listen: needs (host, port)")
	}
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("net-listen: no result slot")
	}
	hostPtr := c.cstrOf(inst.Args[0])
	if hostPtr == "" {
		return fmt.Errorf("net-listen: cannot marshal host as C string")
	}
	_, portReg := c.loadVal(inst.Args[1])

	darwin := runtime.GOOS == "darwin"
	familyOff := int64(0)
	if darwin {
		familyOff = 1
	}
	portOff := int64(2)
	addrOff := int64(4)
	solSocket := int32(1) // Linux SOL_SOCKET
	if darwin {
		solSocket = 65535 // macOS SOL_SOCKET
	}

	c.decl("declare i32 @socket(i32, i32, i32)")
	c.decl("declare i32 @setsockopt(i32, i32, i32, i8*, i32)")
	c.decl("declare i32 @bind(i32, i8*, i32)")
	c.decl("declare i32 @listen(i32, i32)")
	c.decl("declare i32 @inet_pton(i32, i8*, i8*)")

	sock := c.treg("netl.sock")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @socket(i32 2, i32 1, i32 0)\n", sock))

	// SO_REUSEADDR = 1, so a restarted server can rebind a port in TIME_WAIT.
	reuse := c.treg("netl.reuse")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca i32\n", reuse))
	c.sb.WriteString(fmt.Sprintf("  store i32 1, i32* %s\n", reuse))
	reuseP := c.treg("netl.reusep")
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i32* %s to i8*\n", reuseP, reuse))
	c.sb.WriteString(fmt.Sprintf("  call i32 @setsockopt(i32 %s, i32 %d, i32 4, i8* %s, i32 4)\n", sock, solSocket, reuseP))

	addr := c.treg("netl.addr")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca [16 x i8]\n", addr))
	addrp := c.treg("netl.addrp")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [16 x i8], [16 x i8]* %s, i64 0, i64 0\n", addrp, addr))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 16, i1 0)\n", addrp))

	if darwin {
		lenGEP := c.treg("netl.leng")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [16 x i8], [16 x i8]* %s, i64 0, i64 0\n", lenGEP, addr))
		c.sb.WriteString(fmt.Sprintf("  store i8 16, i8* %s\n", lenGEP))
	}
	famGEP := c.treg("netl.famg")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [16 x i8], [16 x i8]* %s, i64 0, i64 %d\n", famGEP, addr, familyOff))
	c.sb.WriteString(fmt.Sprintf("  store i8 2, i8* %s\n", famGEP))

	// sin_port = htons(port)
	plo := c.treg("netl.plo")
	c.sb.WriteString(fmt.Sprintf("  %s = and i64 %s, 255\n", plo, portReg))
	plo8 := c.treg("netl.plo8")
	c.sb.WriteString(fmt.Sprintf("  %s = shl i64 %s, 8\n", plo8, plo))
	phi := c.treg("netl.phi")
	c.sb.WriteString(fmt.Sprintf("  %s = lshr i64 %s, 8\n", phi, portReg))
	phi8 := c.treg("netl.phi8")
	c.sb.WriteString(fmt.Sprintf("  %s = and i64 %s, 255\n", phi8, phi))
	pnet := c.treg("netl.pnet")
	c.sb.WriteString(fmt.Sprintf("  %s = or i64 %s, %s\n", pnet, plo8, phi8))
	pnet16 := c.treg("netl.pnet16")
	c.sb.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i16\n", pnet16, pnet))
	portGEP := c.treg("netl.portg")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [16 x i8], [16 x i8]* %s, i64 0, i64 %d\n", portGEP, addr, portOff))
	c.sb.WriteString(fmt.Sprintf("  store i16 %s, i16* %s\n", pnet16, portGEP))

	addrGEP := c.treg("netl.addrg")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [16 x i8], [16 x i8]* %s, i64 0, i64 %d\n", addrGEP, addr, addrOff))
	pton := c.treg("netl.pton")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @inet_pton(i32 2, i8* %s, i8* %s)\n", pton, hostPtr, addrGEP))
	c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", hostPtr))

	bindRet := c.treg("netl.bind")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @bind(i32 %s, i8* %s, i32 16)\n", bindRet, sock, addrp))
	listenRet := c.treg("netl.listen")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @listen(i32 %s, i32 128)\n", listenRet, sock))

	sockOk := c.treg("netl.sok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sge i32 %s, 0\n", sockOk, sock))
	sock64 := c.treg("netl.s64")
	c.sb.WriteString(fmt.Sprintf("  %s = sext i32 %s to i64\n", sock64, sock))
	s1 := c.treg("netl.s1")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 -1\n", s1, sockOk, sock64))
	ptonOk := c.treg("netl.pok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sgt i32 %s, 0\n", ptonOk, pton))
	s2 := c.treg("netl.s2")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 -1\n", s2, ptonOk, s1))
	bindOk := c.treg("netl.bok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sge i32 %s, 0\n", bindOk, bindRet))
	s3 := c.treg("netl.s3")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 -1\n", s3, bindOk, s2))
	listenOk := c.treg("netl.lok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sge i32 %s, 0\n", listenOk, listenRet))
	fd := c.treg("netl.fd")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 -1\n", fd, listenOk, s3))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", fd, dstSlot))
	return nil
}

// emitBuiltinNetAccept lowers `net.net-accept(listen-fd)` -> fd i64: accept one
// pending TCP connection. Mirrors the legacy backend (build/llvm/
// call_stdlib.go net-accept): accept(2) into a 16-byte sockaddr buffer with an
// in/out addrlen, sign-extended to i64 (-1 on error).
func (c *codegen) emitBuiltinNetAccept(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("net-accept: needs (listen-fd)")
	}
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("net-accept: no result slot")
	}
	fdReg, err := c.marshalScalar(inst, 0, "i32")
	if err != nil {
		return err
	}
	c.decl("declare i32 @accept(i32, i8*, i32*)")
	addr := c.treg("neta.addr")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca [16 x i8]\n", addr))
	addrp := c.treg("neta.addrp")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [16 x i8], [16 x i8]* %s, i64 0, i64 0\n", addrp, addr))
	alen := c.treg("neta.len")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca i32\n", alen))
	c.sb.WriteString(fmt.Sprintf("  store i32 16, i32* %s\n", alen))
	ret := c.treg("neta.ret")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @accept(i32 %s, i8* %s, i32* %s)\n", ret, fdReg, addrp, alen))
	fd := c.treg("neta.fd")
	c.sb.WriteString(fmt.Sprintf("  %s = sext i32 %s to i64\n", fd, ret))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", fd, dstSlot))
	return nil
}

// emitBuiltinNetUdpOpen lowers `net.net-udp-open()` -> fd i64: create a UDP
// socket via socket(AF_INET, SOCK_DGRAM=2, 0). Mirrors the legacy backend
// (build/llvm/call_stdlib.go net-udp-open).
func (c *codegen) emitBuiltinNetUdpOpen(inst *Inst) error {
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("net-udp-open: no result slot")
	}
	dstLT, _ := c.ptype(inst.Dst)
	if dstLT == "" {
		dstLT = "i64"
	}
	c.decl("declare i32 @socket(i32, i32, i32)")
	sock := c.treg("netu.sock")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @socket(i32 2, i32 2, i32 0)\n", sock))
	fd := c.treg("netu.fd")
	c.sb.WriteString(fmt.Sprintf("  %s = sext i32 %s to i64\n", fd, sock))
	if dstLT != "i64" {
		if r := c.coerce("i64", fd, dstLT); r != "" {
			fd = r
		}
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, fd, dstLT, dstSlot))
	return nil
}

// emitBuiltinNetSend lowers `net.net-send(fd, data, n)` -> written i64: a direct
// send(2) on the connected socket. data is a str/[]byte; its data pointer is
// passed with the explicit length n (no NUL termination required). Mirrors the
// legacy backend (build/llvm/call_stdlib.go net-send).
func (c *codegen) emitBuiltinNetSend(inst *Inst) error {
	if len(inst.Args) < 3 {
		return fmt.Errorf("net-send: needs (fd, data, n)")
	}
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("net-send: no result slot")
	}
	fdReg, err := c.marshalScalar(inst, 0, "i32")
	if err != nil {
		return err
	}
	dataPtr := c.dataPtrOf(inst.Args[1])
	if dataPtr == "" {
		return fmt.Errorf("net-send: cannot take data pointer of arg 1")
	}
	_, nReg := c.loadVal(inst.Args[2])
	c.decl("declare i64 @send(i32, i8*, i64, i32)")
	ret := c.treg("nets")
	c.sb.WriteString(fmt.Sprintf("  %s = call i64 @send(i32 %s, i8* %s, i64 %s, i32 0)\n", ret, fdReg, dataPtr, nReg))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", ret, dstSlot))
	return nil
}

// emitBuiltinNetRecv lowers `net.net-recv(fd, buf, n)` -> read-n i64: a direct
// recv(2) into buf's data buffer. Mirrors the legacy backend (build/llvm/
// call_stdlib.go net-recv). The received byte count is the result; the Nolang
// string length is NOT updated (matching legacy recv on a fixed buffer).
func (c *codegen) emitBuiltinNetRecv(inst *Inst) error {
	if len(inst.Args) < 3 {
		return fmt.Errorf("net-recv: needs (fd, buf, n)")
	}
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("net-recv: no result slot")
	}
	fdReg, err := c.marshalScalar(inst, 0, "i32")
	if err != nil {
		return err
	}
	bufPtr := c.dataPtrOf(inst.Args[1])
	if bufPtr == "" {
		return fmt.Errorf("net-recv: cannot take data pointer of arg 1")
	}
	_, nReg := c.loadVal(inst.Args[2])
	c.decl("declare i64 @recv(i32, i8*, i64, i32)")
	ret := c.treg("netr")
	c.sb.WriteString(fmt.Sprintf("  %s = call i64 @recv(i32 %s, i8* %s, i64 %s, i32 0)\n", ret, fdReg, bufPtr, nReg))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", ret, dstSlot))
	return nil
}

// emitBuiltinNetAcceptNb lowers `net.net-accept-nb(fd)` -> fd i64: a
// non-blocking accept. Legacy routes this through the runtime helper
// @nolang_net_accept_nb (build/llvm/decl.go); MIR emits the equivalent inline:
// accept(2) with the listen socket switched to O_NONBLOCK beforehand, so a
// pending-connection-free poll returns -2 (EWOULDBLOCK) instead of blocking the
// cooperative scheduler. Returns >=0 client fd, -2 = would block, -1 = error.
func (c *codegen) emitBuiltinNetAcceptNb(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("net-accept-nb: needs (listen-fd)")
	}
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("net-accept-nb: no result slot")
	}
	fdReg, err := c.marshalScalar(inst, 0, "i32")
	if err != nil {
		return err
	}
	c.decl("declare i32 @fcntl(i32, i32, i32)")
	c.decl("declare i32 @accept(i32, i8*, i32*)")
	c.decl("@.mir.errno = external global i32")
	// fcntl(fd, F_SETFL=4, O_NONBLOCK) — macOS 0x0004, Linux 0x800
	nb := "4"
	if runtime.GOOS == "linux" {
		nb = "2048"
	}
	c.sb.WriteString(fmt.Sprintf("  call i32 @fcntl(i32 %s, i32 4, i32 %s)\n", fdReg, nb))
	addr := c.treg("netanb.addr")
	addrp := c.treg("netanb.addrp")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca [16 x i8]\n", addr))
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [16 x i8], [16 x i8]* %s, i64 0, i64 0\n", addrp, addr))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memset.p0i8.p0i8.i64(i8* %s, i8 0, i64 16, i1 false)\n", addrp))
	alen := c.treg("netanb.alen")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca i32\n", alen))
	c.sb.WriteString(fmt.Sprintf("  store i32 16, i32* %s\n", alen))
	ret := c.treg("netanb.ret")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @accept(i32 %s, i8* %s, i32* %s)\n", ret, fdReg, addrp, alen))
	// -1 with EAGAIN/EWOULDBLOCK -> -2 (would block). macOS EAGAIN=35,
	// Linux EAGAIN=11.
	eagain := "35"
	if runtime.GOOS == "linux" {
		eagain = "11"
	}
	isErr := c.treg("netanb.iserr")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, -1\n", isErr, ret))
	en := c.treg("netanb.en")
	c.sb.WriteString(fmt.Sprintf("  %s = load i32, i32* @.mir.errno\n", en))
	isAgain := c.treg("netanb.again")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %s\n", isAgain, en, eagain))
	wouldBlock := c.treg("netanb.wb")
	c.sb.WriteString(fmt.Sprintf("  %s = and i1 %s, %s\n", wouldBlock, isErr, isAgain))
	fd := c.treg("netanb.fd")
	c.sb.WriteString(fmt.Sprintf("  %s = sext i32 %s to i64\n", fd, ret))
	r := c.treg("netanb.res")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 -2, i64 %s\n", r, wouldBlock, fd))
	dstLT, _ := c.ptype(inst.Dst)
	if dstLT == "" {
		dstLT = "i64"
	}
	if cv := c.coerce("i64", r, dstLT); cv != "" {
		r = cv
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, r, dstLT, dstSlot))
	return nil
}

// emitBuiltinNetRecvNb lowers `net.net-recv-nb(fd, buf, n)` -> read-n i64:
// recv(2) with MSG_DONTWAIT so the call never blocks. MSG_DONTWAIT is 0x40 on
// macOS and 0x40 (MSG_DONTWAIT) on Linux as well.
func (c *codegen) emitBuiltinNetRecvNb(inst *Inst) error {
	if len(inst.Args) < 3 {
		return fmt.Errorf("net-recv-nb: needs (fd, buf, n)")
	}
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("net-recv-nb: no result slot")
	}
	fdReg, err := c.marshalScalar(inst, 0, "i32")
	if err != nil {
		return err
	}
	bufPtr := c.dataPtrOf(inst.Args[1])
	if bufPtr == "" {
		return fmt.Errorf("net-recv-nb: cannot take data pointer of arg 1")
	}
	_, nReg := c.loadVal(inst.Args[2])
	c.decl("declare i64 @recv(i32, i8*, i64, i32)")
	ret := c.treg("netrnb")
	c.sb.WriteString(fmt.Sprintf("  %s = call i64 @recv(i32 %s, i8* %s, i64 %s, i32 64)\n", ret, fdReg, bufPtr, nReg))
	dstLT, _ := c.ptype(inst.Dst)
	if dstLT == "" {
		dstLT = "i64"
	}
	if cv := c.coerce("i64", ret, dstLT); cv != "" {
		ret = cv
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, ret, dstLT, dstSlot))
	return nil
}

// emitBuiltinNetUdpSendTo lowers `net.net-udp-sendto(fd, data, n, host, port)`
// -> written i64: sendto(2) to a sockaddr_in built exactly like
// emitBuiltinNetDial's. Returns -1 when the host is not an IP literal.
func (c *codegen) emitBuiltinNetUdpSendTo(inst *Inst) error {
	if len(inst.Args) < 5 {
		return fmt.Errorf("net-udp-sendto: needs (fd, data, n, host, port)")
	}
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("net-udp-sendto: no result slot")
	}
	fdReg, err := c.marshalScalar(inst, 0, "i32")
	if err != nil {
		return err
	}
	dataPtr := c.dataPtrOf(inst.Args[1])
	if dataPtr == "" {
		return fmt.Errorf("net-udp-sendto: cannot take data pointer of arg 1")
	}
	_, nReg := c.loadVal(inst.Args[2])
	hostPtr := c.cstrOf(inst.Args[3])
	if hostPtr == "" {
		return fmt.Errorf("net-udp-sendto: cannot marshal host as C string")
	}
	_, portReg := c.loadVal(inst.Args[4])

	// Build sockaddr_in exactly like emitBuiltinNetDial: 16 zeroed bytes with
	// (darwin) sin_len@0, sin_family@0|1 = AF_INET, sin_port@2 = htons(port)
	// and sin_addr@4 filled by inet_pton.
	darwin := runtime.GOOS == "darwin"
	familyOff := int64(0)
	if darwin {
		familyOff = 1
	}
	addr := c.treg("netus.addr")
	addrp := c.treg("netus.addrp")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca [16 x i8]\n", addr))
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [16 x i8], [16 x i8]* %s, i64 0, i64 0\n", addrp, addr))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memset.p0i8.p0i8.i64(i8* %s, i8 0, i64 16, i1 false)\n", addrp))
	if darwin {
		lenGEP := c.treg("netus.leng")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [16 x i8], [16 x i8]* %s, i64 0, i64 0\n", lenGEP, addr))
		c.sb.WriteString(fmt.Sprintf("  store i8 16, i8* %s\n", lenGEP))
	}
	famGEP := c.treg("netus.famg")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [16 x i8], [16 x i8]* %s, i64 0, i64 %d\n", famGEP, addr, familyOff))
	c.sb.WriteString(fmt.Sprintf("  store i8 2, i8* %s\n", famGEP))
	plo := c.treg("netus.plo")
	c.sb.WriteString(fmt.Sprintf("  %s = and i64 %s, 255\n", plo, portReg))
	plo8 := c.treg("netus.plo8")
	c.sb.WriteString(fmt.Sprintf("  %s = shl i64 %s, 8\n", plo8, plo))
	phi := c.treg("netus.phi")
	c.sb.WriteString(fmt.Sprintf("  %s = lshr i64 %s, 8\n", phi, portReg))
	phi8 := c.treg("netus.phi8")
	c.sb.WriteString(fmt.Sprintf("  %s = and i64 %s, 255\n", phi8, phi))
	pnet := c.treg("netus.pnet")
	c.sb.WriteString(fmt.Sprintf("  %s = or i64 %s, %s\n", pnet, plo8, phi8))
	pnet16 := c.treg("netus.pnet16")
	c.sb.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i16\n", pnet16, pnet))
	portGEP := c.treg("netus.portg")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [16 x i8], [16 x i8]* %s, i64 0, i64 2\n", portGEP, addr))
	c.sb.WriteString(fmt.Sprintf("  store i16 %s, i16* %s\n", pnet16, portGEP))
	addrGEP := c.treg("netus.addrg")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [16 x i8], [16 x i8]* %s, i64 0, i64 4\n", addrGEP, addr))
	c.decl("declare i32 @inet_pton(i32, i8*, i8*)")
	pton := c.treg("netus.pton")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @inet_pton(i32 2, i8* %s, i8* %s)\n", pton, hostPtr, addrGEP))

	c.decl("declare i64 @sendto(i32, i8*, i64, i32, i8*, i32)")
	ret := c.treg("netus")
	c.sb.WriteString(fmt.Sprintf("  %s = call i64 @sendto(i32 %s, i8* %s, i64 %s, i32 0, i8* %s, i32 16)\n",
		ret, fdReg, dataPtr, nReg, addrp))
	c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", hostPtr))
	dstLT, _ := c.ptype(inst.Dst)
	if dstLT == "" {
		dstLT = "i64"
	}
	if cv := c.coerce("i64", ret, dstLT); cv != "" {
		ret = cv
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, ret, dstLT, dstSlot))
	return nil
}

// emitBuiltinNetUdpRecvFrom lowers `net.net-udp-recvfrom(fd, buf, n)` ->
// read-n i64: recvfrom(2) with a scratch sockaddr the caller ignores.
func (c *codegen) emitBuiltinNetUdpRecvFrom(inst *Inst) error {
	if len(inst.Args) < 3 {
		return fmt.Errorf("net-udp-recvfrom: needs (fd, buf, n)")
	}
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("net-udp-recvfrom: no result slot")
	}
	fdReg, err := c.marshalScalar(inst, 0, "i32")
	if err != nil {
		return err
	}
	bufPtr := c.dataPtrOf(inst.Args[1])
	if bufPtr == "" {
		return fmt.Errorf("net-udp-recvfrom: cannot take data pointer of arg 1")
	}
	_, nReg := c.loadVal(inst.Args[2])
	c.decl("declare i64 @recvfrom(i32, i8*, i64, i32, i8*, i32*)")
	addr := c.treg("netur.addr")
	addrp := c.treg("netur.addrp")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca [16 x i8]\n", addr))
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [16 x i8], [16 x i8]* %s, i64 0, i64 0\n", addrp, addr))
	alen := c.treg("netur.alen")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca i32\n", alen))
	c.sb.WriteString(fmt.Sprintf("  store i32 16, i32* %s\n", alen))
	ret := c.treg("netur")
	c.sb.WriteString(fmt.Sprintf("  %s = call i64 @recvfrom(i32 %s, i8* %s, i64 %s, i32 0, i8* %s, i32* %s)\n",
		ret, fdReg, bufPtr, nReg, addrp, alen))
	dstLT, _ := c.ptype(inst.Dst)
	if dstLT == "" {
		dstLT = "i64"
	}
	if cv := c.coerce("i64", ret, dstLT); cv != "" {
		ret = cv
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, ret, dstLT, dstSlot))
	return nil
}

// emitBuiltinNetSetRecvTimeout lowers `net.net-set-recv-timeout(fd, ms)` ->
// ok i64: sets SO_RCVTIMEO via setsockopt(2), mirroring the legacy inliner
// (build/llvm/call_stdlib.go net-set-recv-timeout). Returns the raw
// setsockopt result sign-extended to i64 (0 = success, -1 = error).
//
// struct timeval is 16 bytes on 64-bit; the platform differs only in the
// SOL_SOCKET / SO_RCVTIMEO constants:
//
//	darwin: SOL_SOCKET=0xFFFF (65535), SO_RCVTIMEO=0x1006 (4102)
//	linux : SOL_SOCKET=1,              SO_RCVTIMEO=20
func (c *codegen) emitBuiltinNetSetRecvTimeout(inst *Inst) error {
	if len(inst.Args) < 2 {
		return fmt.Errorf("net-set-recv-timeout: needs (fd, timeout-ms)")
	}
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("net-set-recv-timeout: no result slot")
	}
	fdReg, err := c.marshalScalar(inst, 0, "i32")
	if err != nil {
		return err
	}
	_, msReg := c.loadVal(inst.Args[1])

	c.decl("declare i32 @setsockopt(i32, i32, i32, i8*, i32)")
	tv := c.treg("netto.tv")
	tvp := c.treg("netto.tvptr")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca [16 x i8]\n", tv))
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [16 x i8], [16 x i8]* %s, i64 0, i64 0\n", tvp, tv))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memset.p0i8.p0i8.i64(i8* %s, i8 0, i64 16, i1 false)\n", tvp))

	sec := c.treg("netto.sec")
	c.sb.WriteString(fmt.Sprintf("  %s = sdiv i64 %s, 1000\n", sec, msReg))
	rem := c.treg("netto.rem")
	c.sb.WriteString(fmt.Sprintf("  %s = srem i64 %s, 1000\n", rem, msReg))
	usec := c.treg("netto.usec")
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, 1000\n", usec, rem))

	secg := c.treg("netto.secg")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 0\n", secg, tvp))
	secc := c.treg("netto.secc")
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to i64*\n", secc, secg))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", sec, secc))

	usg := c.treg("netto.usg")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 8\n", usg, tvp))
	usc := c.treg("netto.usc")
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to i64*\n", usc, usg))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", usec, usc))

	solSocket := "65535"
	soRcvtimeo := "4102"
	if runtime.GOOS == "linux" {
		solSocket = "1"
		soRcvtimeo = "20"
	}
	ret := c.treg("netto.ret")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @setsockopt(i32 %s, i32 %s, i32 %s, i8* %s, i32 16)\n",
		ret, fdReg, solSocket, soRcvtimeo, tvp))
	ext := c.treg("netto.ext")
	c.sb.WriteString(fmt.Sprintf("  %s = sext i32 %s to i64\n", ext, ret))
	dstLT, _ := c.ptype(inst.Dst)
	if dstLT == "" {
		dstLT = "i64"
	}
	if r := c.coerce("i64", ext, dstLT); r != "" {
		ext = r
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, ext, dstLT, dstSlot))
	return nil
}

// emitBuiltinReadFile lowers `fs.read-file(path)` -> []byte: open the file,
// measure it with lseek, malloc a buffer, read it in one shot and close the fd.
// The result is a %vec {len, cap, data} whose data field is the malloc'd buffer
// (kept as an integer, like every other MIR slice), so the caller's drop /
// vec_free releases it. Any failure (open, lseek, read) yields an EMPTY slice
// rather than a negative length — a negative len would be read back as a huge
// unsigned count and cause buffer overreads. This mirrors the legacy
// call_stdlib.go read-file inliner, which also clamps to 0 on failure.
func (c *codegen) emitBuiltinReadFile(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("read-file: needs path")
	}
	dstLT, _ := c.ptype(inst.Dst)
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("read-file: no result slot")
	}
	if dstLT != "%vec" {
		return fmt.Errorf("read-file: result type %s is not a slice", dstLT)
	}
	pathV, err := c.argIndex(inst, 0)
	if err != nil {
		return err
	}
	pathPtr := c.cstrOf(pathV)
	if pathPtr == "" {
		return fmt.Errorf("read-file: cannot marshal path as C string")
	}

	// NOTE: `open` is already declared by the fs.open family as
	// `declare i32 @open(i8*, i32, i32)`. Emitting a variadic form here would
	// be a *different* signature for the same symbol and LLVM rejects it with
	// "invalid redefinition of function 'open'", so match it exactly (the third
	// argument is the creation mode, unused with O_RDONLY).
	c.decl("declare i32 @open(i8*, i32, i32)")
	c.decl("declare i64 @lseek(i32, i64, i32)")
	c.decl("declare i64 @read(i32, i8*, i64)")
	c.decl("declare i32 @close(i32)")

	fd := c.treg("rf.fd")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @open(i8* %s, i32 0, i32 0)\n", fd, pathPtr))
	openOk := c.treg("rf.fdok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sge i32 %s, 0\n", openOk, fd))
	end := c.treg("rf.end")
	c.sb.WriteString(fmt.Sprintf("  %s = call i64 @lseek(i32 %s, i64 0, i32 2)\n", end, fd))
	c.sb.WriteString(fmt.Sprintf("  call i64 @lseek(i32 %s, i64 0, i32 0)\n", fd))
	szOk := c.treg("rf.szok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sge i64 %s, 0\n", szOk, end))
	sz := c.treg("rf.sz")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 0\n", sz, szOk, end))
	buf := c.treg("rf.buf")
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", buf, sz))
	nr := c.treg("rf.n")
	c.sb.WriteString(fmt.Sprintf("  %s = call i64 @read(i32 %s, i8* %s, i64 %s)\n", nr, fd, buf, sz))
	c.sb.WriteString(fmt.Sprintf("  call i32 @close(i32 %s)\n", fd))
	readOk := c.treg("rf.nok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sge i64 %s, 0\n", readOk, nr))
	allOk := c.treg("rf.ok")
	c.sb.WriteString(fmt.Sprintf("  %s = and i1 %s, %s\n", allOk, openOk, readOk))
	ln := c.treg("rf.len")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 0\n", ln, allOk, nr))

	// %vec = { i64 len, i64 cap, i64 data } — data is a heap pointer as i64.
	lenGEP := c.treg("rf.lgep")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", lenGEP, dstSlot))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", ln, lenGEP))
	capGEP := c.treg("rf.cgep")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n", capGEP, dstSlot))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", ln, capGEP))
	dataGEP := c.treg("rf.dgep")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", dataGEP, dstSlot))
	dataInt := c.treg("rf.data")
	c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", dataInt, buf))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", dataInt, dataGEP))
	return nil
}

// emitBuiltinWriteFile lowers `write-file(path, data)` -> ok bool.
// Mirrors the legacy call_stdlib.go write-file inliner: open with
// O_WRONLY|O_CREAT|O_TRUNC, write(fd, data, len), close(fd), ok = (written == len).
func (c *codegen) emitBuiltinWriteFile(inst *Inst) error {
	if len(inst.Args) < 2 {
		return fmt.Errorf("write-file: needs path and data")
	}
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("write-file: no result slot")
	}
	pathV, err := c.argIndex(inst, 0)
	if err != nil {
		return err
	}
	pathPtr := c.cstrOf(pathV)
	if pathPtr == "" {
		return fmt.Errorf("write-file: cannot marshal path as C string")
	}

	// Extract len and data pointer from the []byte (%vec) argument.
	dataSlot := c.valSlot[inst.Args[1]]
	if dataSlot == "" {
		return fmt.Errorf("write-file: data arg has no slot")
	}
	lenGEP := c.treg("wf.lgep")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", lenGEP, dataSlot))
	wfLen := c.treg("wf.len")
	c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", wfLen, lenGEP))
	dataGEP := c.treg("wf.dgep")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", dataGEP, dataSlot))
	dataPtr := c.treg("wf.data")
	c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", dataPtr, dataGEP))
	dataPtrCast := c.treg("wf.dp")
	c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", dataPtrCast, dataPtr))

	c.decl("declare i32 @open(i8*, i32, i32)")
	c.decl("declare i64 @write(i32, i8*, i64)")
	c.decl("declare i32 @close(i32)")

	// open(path, O_WRONLY|O_CREAT|O_TRUNC, 0644=420)
	// macOS: 1 | 512 | 1024 = 1537; Linux: 1 | 64 | 512 = 577
	openFlags := 1537
	if runtime.GOOS == "linux" {
		openFlags = 577
	}
	fd := c.treg("wf.fd")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @open(i8* %s, i32 %d, i32 420)\n", fd, pathPtr, openFlags))
	openOk := c.treg("wf.ok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sge i32 %s, 0\n", openOk, fd))

	// write(fd, data, len)
	wr := c.treg("wf.wr")
	c.sb.WriteString(fmt.Sprintf("  %s = call i64 @write(i32 %s, i8* %s, i64 %s)\n", wr, fd, dataPtrCast, wfLen))
	// If open failed, use -1 for write result
	wrSel := c.treg("wf.wrs")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 -1\n", wrSel, openOk, wr))

	// close(fd)
	c.sb.WriteString(fmt.Sprintf("  call i32 @close(i32 %s)\n", fd))

	// ok = (written == len)
	cmp := c.treg("wf.cmp")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i64 %s, %s\n", cmp, wrSel, wfLen))
	c.sb.WriteString(fmt.Sprintf("  store i1 %s, i1* %s\n", cmp, dstSlot))
	return nil
}

// emitBuiltinReadDir lowers `read-dir(dirp)` -> (name str, ok bool).
// Mirrors the legacy call_stdlib.go read-dir inliner: readdir(dirp) returns
// a dirent* (NULL = no more entries); d_name is at offset 21 on macOS.
// The name is strlen'd, malloc'd, and memcpy'd into an owned %str-long.
func (c *codegen) emitBuiltinReadDir(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("read-dir: needs dirp")
	}
	// result 0 = name (str), result 1 = ok (bool)
	nameSlot := c.valSlot[inst.Results[0]]
	if nameSlot == "" {
		return fmt.Errorf("read-dir: no name result slot")
	}

	dirpV, err := c.argIndex(inst, 0)
	if err != nil {
		return err
	}
	dirpT, dirpVal := c.loadVal(dirpV)
	_ = dirpT

	c.decl("declare i8* @readdir(i8*)")
	// strlen is already declared globally by codegen.go

	// inttoptr i64 to i8* (DIR*)
	dirpPtr := c.treg("rd.dp")
	c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", dirpPtr, dirpVal))

	// readdir(dirp)
	entry := c.treg("rd.ent")
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @readdir(i8* %s)\n", entry, dirpPtr))

	// ok = (entry != NULL)
	okReg := c.treg("rd.ok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ne i8* %s, null\n", okReg, entry))

	// d_name at offset 21 on macOS (d_ino(8) + d_seekoff(8) + d_reclen(2)
	// + d_namlen(2) + d_type(1) = 21). Linux: d_ino(8) + d_off(8) +
	// d_reclen(2) + d_type(1) = 19.
	dnameOff := int64(21)
	if runtime.GOOS == "linux" {
		dnameOff = 19
	}
	nameGep := c.treg("rd.ng")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr i8, i8* %s, i64 %d\n", nameGep, entry, dnameOff))

	// Select: if not NULL, use d_name pointer; otherwise use empty string global
	c.global(`@.str.empty = private unnamed_addr constant [1 x i8] c"\00"`)
	emptyPtr := c.treg("rd.ep")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [1 x i8], [1 x i8]* @.str.empty, i64 0, i64 0\n", emptyPtr))
	safeName := c.treg("rd.sn")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i8* %s, i8* %s\n", safeName, okReg, nameGep, emptyPtr))

	// strlen on the safe pointer
	lenReg := c.treg("rd.len")
	c.sb.WriteString(fmt.Sprintf("  %s = call i64 @strlen(i8* %s)\n", lenReg, safeName))

	// malloc(len + 1) and memcpy (readdir returns static memory)
	bufSize := c.treg("rd.bs")
	c.sb.WriteString(fmt.Sprintf("  %s = add i64 %s, 1\n", bufSize, lenReg))
	nameBuf := c.treg("rd.nb")
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", nameBuf, bufSize))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", nameBuf, safeName, bufSize))

	// Build %str-long { len, cap, data } in the result slot
	lenGEP := c.treg("rd.lgep")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", lenGEP, nameSlot))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", lenReg, lenGEP))
	capGEP := c.treg("rd.cgep")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n", capGEP, nameSlot))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", lenReg, capGEP))
	dataGEP := c.treg("rd.dgep")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n", dataGEP, nameSlot))
	dataInt := c.treg("rd.di")
	c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", dataInt, nameBuf))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", dataInt, dataGEP))

	// Store ok into result 1
	if err := c.storeResult(inst, 1, okReg, "i1"); err != nil {
		return err
	}
	return nil
}

// emitBuiltinStrTruncate lowers `str.truncate(n)` — set len = max(0, min(len, n))
// in-place on the receiver's %str-long {len, cap, data}. cap and data pointer
// are unchanged. Mirrors the legacy genForwardFunc "str-truncate" case.
func (c *codegen) emitBuiltinStrTruncate(inst *Inst) error {
	if len(inst.Args) < 2 {
		return fmt.Errorf("str-truncate: needs receiver and n")
	}
	recvSlot, _ := c.resolveReceiverSlot(inst.Args[0])
	if recvSlot == "" {
		return fmt.Errorf("str-truncate: receiver has no slot")
	}
	nT, nV := c.loadVal(inst.Args[1])
	n := c.coerce(nT, nV, "i64")
	if n == "" {
		n = nV
	}

	// Load current len
	lenGEP := c.treg("st.lgep")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", lenGEP, recvSlot))
	curLen := c.treg("st.cl")
	c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", curLen, lenGEP))

	// newLen = min(curLen, max(0, n))
	// clamp n to >= 0
	zero := c.treg("st.z")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp slt i64 %s, 0\n", zero, n))
	clampedN := c.treg("st.cn")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 0, i64 %s\n", clampedN, zero, n))
	// min(curLen, clampedN)
	ltCmp := c.treg("st.lt")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp slt i64 %s, %s\n", ltCmp, curLen, clampedN))
	newLen := c.treg("st.nl")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 %s\n", newLen, ltCmp, curLen, clampedN))

	// Store new len
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", newLen, lenGEP))
	return nil
}

// emitBuiltinUtime lowers `os.utime(path, atime, mtime)` -> ok bool. The C
// entry point is utimes(2), which takes a struct timeval[2]; tv_sec holds the
// Unix seconds and tv_usec is zeroed. icmp eq ret 0 yields the success bool,
// matching the legacy call_stdlib.go utime inliner.
func (c *codegen) emitBuiltinUtime(inst *Inst) error {
	if len(inst.Args) < 3 {
		return fmt.Errorf("utime: needs path, atime, mtime")
	}
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return fmt.Errorf("utime: no result slot")
	}
	pathV, err := c.argIndex(inst, 0)
	if err != nil {
		return err
	}
	pathPtr := c.cstrOf(pathV)
	if pathPtr == "" {
		return fmt.Errorf("utime: cannot marshal path as C string")
	}
	atimeT, atimeV := c.loadVal(inst.Args[1])
	atime := c.coerce(atimeT, atimeV, "i64")
	if atime == "" {
		atime = "0"
	}
	mtimeT, mtimeV := c.loadVal(inst.Args[2])
	mtime := c.coerce(mtimeT, mtimeV, "i64")
	if mtime == "" {
		mtime = "0"
	}

	c.decl("declare i32 @utimes(i8*, i8*)")

	utBuf := c.treg("utbuf")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca [32 x i8]\n", utBuf))
	atimeGEP := c.treg("utat")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [32 x i8], [32 x i8]* %s, i64 0, i64 0\n", atimeGEP, utBuf))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", atime, atimeGEP))
	usec0GEP := c.treg("utus0")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [32 x i8], [32 x i8]* %s, i64 0, i64 8\n", usec0GEP, utBuf))
	c.sb.WriteString(fmt.Sprintf("  store i64 0, i64* %s\n", usec0GEP))
	mtimeGEP := c.treg("utmt")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [32 x i8], [32 x i8]* %s, i64 0, i64 16\n", mtimeGEP, utBuf))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", mtime, mtimeGEP))
	usec1GEP := c.treg("utus1")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [32 x i8], [32 x i8]* %s, i64 0, i64 24\n", usec1GEP, utBuf))
	c.sb.WriteString(fmt.Sprintf("  store i64 0, i64* %s\n", usec1GEP))
	timesPtr := c.treg("uttimes")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [32 x i8], [32 x i8]* %s, i64 0, i64 0\n", timesPtr, utBuf))
	utRet := c.treg("utret")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @utimes(i8* %s, i8* %s)\n", utRet, pathPtr, timesPtr))
	cmp := c.treg("utcmp")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", cmp, utRet))
	c.sb.WriteString(fmt.Sprintf("  store i1 %s, i1* %s\n", cmp, dstSlot))
	c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", pathPtr))
	return nil
}

// errnoFnName returns the platform-specific C function that yields a pointer to
// the thread-local errno. macOS/BSD use __error(); glibc uses __errno_location.
func (c *codegen) errnoFnName() string {
	if runtime.GOOS == "linux" {
		return "__errno_location"
	}
	return "__error"
}

// emitBuiltinGetPriority lowers `os.get-priority(which, who)` -> (prio i64, ok bool).
// getpriority(2) returns the negated priority on success but -1 on *both* success
// and failure, so the only reliable success signal is errno == 0: clear errno,
// call getpriority, then ok = (errno == 0). This mirrors call_stdlib.go exactly.
func (c *codegen) emitBuiltinGetPriority(inst *Inst) error {
	if len(inst.Args) < 2 {
		return fmt.Errorf("get-priority: needs which, who")
	}
	which, err := c.marshalScalar(inst, 0, "i32")
	if err != nil {
		return err
	}
	who, err := c.marshalScalar(inst, 1, "i32")
	if err != nil {
		return err
	}
	efn := c.errnoFnName()
	c.decl(fmt.Sprintf("declare i32* @%s()", efn))
	c.decl("declare i32 @getpriority(i32, i32)")
	ePtr := c.treg("gp.err")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32* @%s()\n", ePtr, efn))
	c.sb.WriteString(fmt.Sprintf("  store i32 0, i32* %s\n", ePtr))
	ret := c.treg("gp.ret")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @getpriority(i32 %s, i32 %s)\n", ret, which, who))
	ext := c.treg("gp.ext")
	c.sb.WriteString(fmt.Sprintf("  %s = sext i32 %s to i64\n", ext, ret))
	if err := c.storeResult(inst, 0, ext, "i64"); err != nil {
		return err
	}
	eLoad := c.treg("gp.eld")
	c.sb.WriteString(fmt.Sprintf("  %s = load i32, i32* %s\n", eLoad, ePtr))
	ok := c.treg("gp.ok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", ok, eLoad))
	return c.storeResult(inst, 1, ok, "i1")
}

// emitBuiltinGetErrno lowers `os.get-errno()` -> i64 (the last errno value from
// the C library). Mirrors call.go's get-errno case: resolve the platform errno
// pointer (__error on macOS/BSD, __errno_location on glibc) and load+sext the
// i32 to i64. Single-result builtin, so only inst.Results[0] is filled.
func (c *codegen) emitBuiltinGetErrno(inst *Inst) error {
	efn := c.errnoFnName()
	c.decl(fmt.Sprintf("declare i32* @%s()", efn))
	ePtr := c.treg("ge.err")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32* @%s()\n", ePtr, efn))
	eLoad := c.treg("ge.eld")
	c.sb.WriteString(fmt.Sprintf("  %s = load i32, i32* %s\n", eLoad, ePtr))
	ext := c.treg("ge.ext")
	c.sb.WriteString(fmt.Sprintf("  %s = sext i32 %s to i64\n", ext, eLoad))
	return c.storeResult(inst, 0, ext, "i64")
}

// emitBuiltinGetLine lowers `fs.get-line()` (ForwardFunc read-stdin-line): read
// one line from stdin into a freshly malloc'd 4096-byte buffer via fgets(3),
// strip a trailing '\n', and return (line str, ok bool). ok is false on EOF
// (fgets returns NULL). Mirrors build/llvm/call_stdlib.go get-line: malloc +
// fgets(stdin) + strlen + newline-strip + %str-long construction. The buffer is
// adopted as an owned %str-long (cap = len), so the drop pass frees it like any
// other owned string (the buffer is heap-owned via @malloc, matching @str_free).
func (c *codegen) emitBuiltinGetLine(inst *Inst) error {
	if len(inst.Results) < 1 || inst.Results[0] <= NoVal {
		if inst.Dst <= NoVal {
			c.fail("get-line: no result slot")
			return fmt.Errorf("get-line: no result")
		}
	}
	c.decl("declare i8* @fgets(i8*, i32, i8*)")
	c.decl("declare void @llvm.memset.p0i8.i64(i8*, i8, i64, i1)")
	// macOS uses __stdinp (i8**); Linux/others use stdin (i8**). The MIR prelude
	// is currently hardcoded to arm64-apple-macosx, so default to __stdinp and
	// only fall back to stdin for an explicit linux target.
	stdinSym := "@__stdinp"
	if runtime.GOOS == "linux" {
		stdinSym = "@stdin"
	}
	c.global(stdinSym + " = external global i8*")

	// buf = malloc(4096); memset(buf, 0, 4096) so a NULL (EOF) read leaves a
	// valid, zeroed buffer and the branchless strip below never touches
	// uninitialized memory.
	buf := c.treg("gl.buf")
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 4096)\n", buf))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 4096, i1 false)\n", buf))
	stdinReg := c.treg("gl.stdin")
	c.sb.WriteString(fmt.Sprintf("  %s = load i8*, i8** %s\n", stdinReg, stdinSym))
	fgetsReg := c.treg("gl.fgets")
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @fgets(i8* %s, i32 4096, i8* %s)\n", fgetsReg, buf, stdinReg))
	ok := c.treg("gl.ok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ne i8* %s, null\n", ok, fgetsReg))
	lenReg := c.treg("gl.len")
	c.sb.WriteString(fmt.Sprintf("  %s = call i64 @strlen(i8* %s)\n", lenReg, buf))

	// Branchless trailing-newline strip. The byte beyond newLen is never read
	// (the %str-long length caps it), so the buffer itself need not be mutated;
	// we only shorten newLen when the last byte is '\n'. (The buffer is zeroed,
	// so reading it when len == 0 is safe; the result is discarded via select.)
	hasLen := c.treg("gl.has")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sgt i64 %s, 0\n", hasLen, lenReg))
	sub1 := c.treg("gl.sub1")
	c.sb.WriteString(fmt.Sprintf("  %s = sub i64 %s, 1\n", sub1, lenReg))
	lastIdx := c.treg("gl.lidx")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 0\n", lastIdx, hasLen, sub1))
	nlPtr := c.treg("gl.nlptr")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", nlPtr, buf, lastIdx))
	lastByte := c.treg("gl.nlb")
	c.sb.WriteString(fmt.Sprintf("  %s = load i8, i8* %s\n", lastByte, nlPtr))
	isNL := c.treg("gl.isnl")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i8 %s, 10\n", isNL, lastByte))
	isNLz64 := c.treg("gl.isnlz64")
	c.sb.WriteString(fmt.Sprintf("  %s = zext i1 %s to i64\n", isNLz64, isNL))
	newLenRaw := c.treg("gl.nlr")
	c.sb.WriteString(fmt.Sprintf("  %s = sub i64 %s, %s\n", newLenRaw, lenReg, isNLz64))
	// newLen = len - (newline ? 1 : 0), guarded to 0 when len == 0.
	newLen := c.treg("gl.nl")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 0\n", newLen, hasLen, newLenRaw))

	// Adopt the malloc'd buffer as an owned %str-long {len, cap=len, data}.
	if err := c.storeRawStr(inst, 0, newLen, newLen, buf); err != nil {
		return err
	}
	if len(inst.Results) >= 2 && inst.Results[1] > NoVal {
		if err := c.storeResult(inst, 1, ok, "i1"); err != nil {
			return err
		}
	}
	return nil
}

// emitBuiltinSysctl lowers `os.sysctl(name)` -> (val str, ok bool) on macOS/BSD
// via sysctlbyname(3). Two calls: first with NULL buf to learn the size, then
// malloc(size+1) and refill. ok = (second call returned 0). The value adopts the
// malloc'd buffer as an owned %str-long (len = returned size, cap = size+1),
// matching call_stdlib.go's scLen2/scBufSize2 usage so the printed value agrees.
func (c *codegen) emitBuiltinSysctl(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("sysctl: needs a name")
	}
	nameV, err := c.argIndex(inst, 0)
	if err != nil {
		return err
	}
	namePtr := c.cstrOf(nameV)
	if namePtr == "" {
		return fmt.Errorf("sysctl: cannot marshal name as C string")
	}
	c.decl("declare i32 @sysctlbyname(i8*, i8*, i64*, i8*, i64)")
	lenBuf := c.treg("sc.len")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca i64\n", lenBuf))
	c.sb.WriteString(fmt.Sprintf("  store i64 0, i64* %s\n", lenBuf))
	ret1 := c.treg("sc.ret")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @sysctlbyname(i8* %s, i8* null, i64* %s, i8* null, i64 0)\n", ret1, namePtr, lenBuf))
	sz := c.treg("sc.sz")
	c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", sz, lenBuf))
	capR := c.treg("sc.cap")
	c.sb.WriteString(fmt.Sprintf("  %s = add i64 %s, 1\n", capR, sz))
	buf := c.treg("sc.buf")
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", buf, capR))
	ret2 := c.treg("sc.ret2")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @sysctlbyname(i8* %s, i8* %s, i64* %s, i8* null, i64 0)\n", ret2, namePtr, buf, lenBuf))
	cmp2 := c.treg("sc.cmp2")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", cmp2, ret2))
	if err := c.storeResult(inst, 1, cmp2, "i1"); err != nil {
		return err
	}
	len2 := c.treg("sc.len2")
	c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", len2, lenBuf))
	c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", namePtr))
	return c.storeRawStr(inst, 0, len2, capR, buf)
}

// storeRawStr builds an owned %str-long {len, cap, data} directly into the slot
// of result i, bypassing a NUL-terminated copy. Used when the backing bytes are
// already heap-owned (e.g. a freshly malloc'd sysctl buffer). The drop pass frees
// the data pointer exactly like any other owned %str-long result.
func (c *codegen) storeRawStr(inst *Inst, i int, lenReg, capReg, dataReg string) error {
	lt, ok := c.resultType(inst, i)
	if !ok {
		return nil
	}
	if lt != "%str-long" {
		return fmt.Errorf("builtin %s: raw str stored into %s slot", inst.Sym, lt)
	}
	slot := c.valSlot[inst.Results[i]]
	if slot == "" {
		return fmt.Errorf("builtin %s: result %d has no slot", inst.Sym, i)
	}
	lg := c.treg("rsl")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 0\n", lg, slot))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", lenReg, lg))
	cg := c.treg("rsc")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 1\n", cg, slot))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", capReg, cg))
	dg := c.treg("rsd")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 2\n", dg, slot))
	c.sb.WriteString(fmt.Sprintf("  store i8* %s, i8** %s\n", dataReg, dg))
	return nil
}

// emitBuiltinWaitpid lowers `process.waitpid(pid, options)` -> status i64.
// Returns WEXITSTATUS: (status >> 8) & 0xFF, where status is the i32 written by
// libc waitpid into an out-parameter. Mirrors call_stdlib.go process-waitpid.
func (c *codegen) emitBuiltinWaitpid(inst *Inst) error {	if len(inst.Args) < 2 {
		return fmt.Errorf("waitpid: needs pid, options")
	}
	pid, err := c.marshalScalar(inst, 0, "i32")
	if err != nil {
		return err
	}
	opt, err := c.marshalScalar(inst, 1, "i32")
	if err != nil {
		return err
	}
	c.decl("declare i32 @waitpid(i32, i32*, i32)")
	st := c.treg("wp.st")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca i32\n", st))
	ret := c.treg("wp.ret")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @waitpid(i32 %s, i32* %s, i32 %s)\n", ret, pid, st, opt))
	ld := c.treg("wp.ld")
	c.sb.WriteString(fmt.Sprintf("  %s = load i32, i32* %s\n", ld, st))
	sh := c.treg("wp.sh")
	c.sb.WriteString(fmt.Sprintf("  %s = lshr i32 %s, 8\n", sh, ld))
	code := c.treg("wp.code")
	c.sb.WriteString(fmt.Sprintf("  %s = and i32 %s, 255\n", code, sh))
	ext := c.treg("wp.ext")
	c.sb.WriteString(fmt.Sprintf("  %s = sext i32 %s to i64\n", ext, code))
	return c.storeResult(inst, 0, ext, "i64")
}

// emitBuiltinPipe lowers `process.pipe()` -> i64, packing the two file
// descriptors as `(read_fd << 32) | write_fd` — the exact encoding
// call_stdlib.go's process-pipe produces, so the Nolang side can unpack it with
// a shift/mask pair.
//
// Needed for std/process.no's POSIX `cmd`: it creates up to three pipes this
// way (stdout, stderr, stdin), so without it every process test failed at
// "unsupported builtin process-pipe" once the platform filter stopped resolving
// `process.cmd` to its Win32 body.
func (c *codegen) emitBuiltinPipe(inst *Inst) error {
	c.decl("declare i32 @pipe(i32*)")
	fds := c.treg("pp.fds")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca [2 x i32]\n", fds))
	ret := c.treg("pp.ret")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @pipe(i32* %s)\n", ret, fds))
	gep0 := c.treg("pp.gep0")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr [2 x i32], [2 x i32]* %s, i64 0, i64 0\n", gep0, fds))
	fd0 := c.treg("pp.fd0")
	c.sb.WriteString(fmt.Sprintf("  %s = load i32, i32* %s\n", fd0, gep0))
	gep1 := c.treg("pp.gep1")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr [2 x i32], [2 x i32]* %s, i64 0, i64 1\n", gep1, fds))
	fd1 := c.treg("pp.fd1")
	c.sb.WriteString(fmt.Sprintf("  %s = load i32, i32* %s\n", fd1, gep1))
	ext0 := c.treg("pp.ext0")
	c.sb.WriteString(fmt.Sprintf("  %s = sext i32 %s to i64\n", ext0, fd0))
	ext1 := c.treg("pp.ext1")
	c.sb.WriteString(fmt.Sprintf("  %s = sext i32 %s to i64\n", ext1, fd1))
	shl := c.treg("pp.shl")
	c.sb.WriteString(fmt.Sprintf("  %s = shl i64 %s, 32\n", shl, ext0))
	pack := c.treg("pp.pack")
	c.sb.WriteString(fmt.Sprintf("  %s = or i64 %s, %s\n", pack, shl, ext1))
	return c.storeResult(inst, 0, pack, "i64")
}

// emitBuiltinWaitpidNohang lowers `process.waitpid-nohang(pid)` -> i64: -1
// while the child is still running (waitpid returns <= 0), otherwise the exit
// code. Mirrors call_stdlib.go process-waitpid-nohang, including the WNOHANG=1
// option so the poll never blocks.
func (c *codegen) emitBuiltinWaitpidNohang(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("waitpid-nohang: needs pid")
	}
	pid, err := c.marshalScalar(inst, 0, "i32")
	if err != nil {
		return err
	}
	c.decl("declare i32 @waitpid(i32, i32*, i32)")
	st := c.treg("wn.st")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca i32\n", st))
	ret := c.treg("wn.ret")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @waitpid(i32 %s, i32* %s, i32 1)\n", ret, pid, st))
	rext := c.treg("wn.retext")
	c.sb.WriteString(fmt.Sprintf("  %s = sext i32 %s to i64\n", rext, ret))
	still := c.treg("wn.still")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sle i64 %s, 0\n", still, rext))
	ld := c.treg("wn.ld")
	c.sb.WriteString(fmt.Sprintf("  %s = load i32, i32* %s\n", ld, st))
	sh := c.treg("wn.sh")
	c.sb.WriteString(fmt.Sprintf("  %s = lshr i32 %s, 8\n", sh, ld))
	code := c.treg("wn.code")
	c.sb.WriteString(fmt.Sprintf("  %s = and i32 %s, 255\n", code, sh))
	cext := c.treg("wn.codeext")
	c.sb.WriteString(fmt.Sprintf("  %s = sext i32 %s to i64\n", cext, code))
	res := c.treg("wn.result")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 -1, i64 %s\n", res, still, cext))
	return c.storeResult(inst, 0, res, "i64")
}

// emitBuiltinExecShell lowers `process.exec-shell(cmd)` -> replaces the current
// process image with `sh -c cmd` via execlp(3). It returns only on failure
// (parent never sees a usable value because the child's image is replaced); the
// Nolang `spawn` uses it inside the child branch and calls os.exit(127) after,
// so the result is ignored. The "sh" and "-c" literals are emitted as private
// module constants.
func (c *codegen) emitBuiltinExecShell(inst *Inst) error {
	if len(inst.Args) < 1 {
		return fmt.Errorf("exec-shell: needs a command")
	}
	cmdV, err := c.argIndex(inst, 0)
	if err != nil {
		return err
	}
	cmdPtr := c.cstrOf(cmdV)
	if cmdPtr == "" {
		return fmt.Errorf("exec-shell: cannot marshal command as C string")
	}
	c.global("@.mir.str.sh = private constant [3 x i8] c\"sh\\00\"")
	c.global("@.mir.str.dashc = private constant [3 x i8] c\"-c\\00\"")
	c.decl("declare i32 @execlp(i8*, i8*, ...)") // variadic
	sh := "getelementptr inbounds ([3 x i8], [3 x i8]* @.mir.str.sh, i64 0, i64 0)"
	dc := "getelementptr inbounds ([3 x i8], [3 x i8]* @.mir.str.dashc, i64 0, i64 0)"
	ret := c.treg("es.ret")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 (i8*, i8*, ...) @execlp(i8* %s, i8* %s, i8* %s, i8* %s, i8* null)\n", ret, sh, sh, dc, cmdPtr))
	c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", cmdPtr))
	return nil
}
