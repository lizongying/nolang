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
	"write":                          true,
	"malloc":                        true,
	"free":                          true,
	"memcmp":                        true,
	"llvm.memcpy.p0i8.p0i8.i64":     true,
	"str_free":                      true,
	"str_eq":                        true,
	"vec_free":                      true,
	"str_from_const":                true,
	"str_concat":                    true,
	"digits":                        true,
	"print_str":                     true,
	"print_i64":                     true,
	"print_double":                  true,
	"print_bool":                    true,
	"print_space":                   true,
	"print_option":                  true,
	"eprint_str":                    true,
	"eprint_i64":                    true,
	"eprint_double":                 true,
	"eprint_bool":                  true,
	"eprint_space":                  true,
	"eprint_nl":                     true,
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
	}
	c.fail("unsupported builtin %s (ForwardFunc=%q) in func %s", inst.Sym, bm.ForwardFunc, f.Name)
	return fmt.Errorf("unsupported builtin %s", inst.Sym)
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
