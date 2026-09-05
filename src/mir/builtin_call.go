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
func (c *codegen) decl(line string) {
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
	slot := c.valSlot[v]
	if slot == "" {
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
	return ""
}

// cstrOf materializes a NUL-terminated heap copy of a str/[]byte argument so it
// can be handed to a C function expecting a `char*`. The returned register MUST
// be freed by the caller once the C call has returned (Nolang strings are not
// NUL-terminated, so passing the raw data pointer would let libc read past the
// end of the buffer).
func (c *codegen) cstrOf(v ValueID) string {
	lt, _ := c.ptype(v)
	if lt != "%str-long" && lt != "%vec" {
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
		c.sb.WriteString(fmt.Sprintf("  call %s @%s(%s)\n", cRet, fn, argList))
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
		c.sb.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", r, cRet, fn, argList))
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
		c.sb.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", r, cRet, fn, argList))
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
			c.sb.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", r, cRet, fn, argList))
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
	case "str-len", "vec-len":
		return c.emitBuiltinLen(inst, bm.ForwardFunc)
	case "math-max", "math-min", "math-abs", "math-clamp", "math-degrees":
		return c.emitBuiltinMath(inst, bm.ForwardFunc)
	case "str-to-bool":
		return c.emitBuiltinStrToBool(inst)
	case "bool-to-str":
		return c.emitBuiltinBoolToStr(inst)
	case "vec-push":
		return c.emitBuiltinVecPush(inst)
	case "uname":
		return c.emitBuiltinUname(inst)
	case "utime":
		return c.emitBuiltinUtime(inst)
	case "get-priority":
		return c.emitBuiltinGetPriority(inst)
	case "sysctl":
		return c.emitBuiltinSysctl(inst)
	case "process-waitpid":
		return c.emitBuiltinWaitpid(inst)
	case "process-exec-shell":
		return c.emitBuiltinExecShell(inst)
	case "load-le-u16", "load-le-u32", "load-le-u64":
		return c.emitBuiltinLoadLE(f, inst, bm.ForwardFunc)
	case "store-le-u32":
		return c.emitBuiltinStoreLE(f, inst)
	case "rotate-left", "rotate-right":
		return c.emitBuiltinRotate(f, inst, bm.ForwardFunc)
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

// emitBuiltinStrToBool lowers `str.to-bool()`: true iff the receiver equals the
// literal "true". The table entry carries no body, so MIR must synthesize the
// comparison inline.
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
	trueV := c.strConst("true")
	eq := c.treg("stb")
	c.sb.WriteString(fmt.Sprintf("  %s = call i1 @str_eq(%s %s, %%str-long %s)\n", eq, rcvTy, rcvV, trueV))
	if dstLT == "%option" {
		// ?bool -> { tag=1 (some), inner=zext(i1) }
		inner := c.treg("stbi")
		c.sb.WriteString(fmt.Sprintf("  %s = zext i1 %s to i64\n", inner, eq))
		s0 := c.treg("stbs0")
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%option { i64 0, i64 0 }, i64 1, 0\n", s0))
		s1 := c.treg("stbs1")
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%option %s, i64 %s, 1\n", s1, s0, inner))
		c.sb.WriteString(fmt.Sprintf("  store %%option %s, %%option* %s\n", s1, dstSlot))
		return nil
	}
	v := c.coerce("i1", eq, dstLT)
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
	slot := c.valSlot[recv]
	if slot == "" {
		return fmt.Errorf("vec.push: no receiver slot")
	}
	elemTy, elemV := c.loadVal(elem)
	stride := int64(8)
	switch elemTy {
	case "%str-long":
		stride = 24
	case "i8":
		stride = 1
	}
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
func (c *codegen) emitBuiltinWaitpid(inst *Inst) error {
	if len(inst.Args) < 2 {
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
