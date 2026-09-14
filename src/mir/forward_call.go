package mir

// forward_call.go — the generic C-call path for ForwardFunc builtins.
//
// Why this file exists
// --------------------
// `builtin.BuiltinMethod.ForwardFunc` is a *name*, not a description: the legacy
// backend resolves it through two ~4000-line hand-written switches
// (build/llvm/call.go genForwardFunc and call_stdlib.go callBuiltin). That is
// exactly the "semantics scattered through the emitter" the MIR project exists to
// eliminate.
//
// A large slice of that switch is nevertheless mechanical: the builtin really is
// "call this C function, marshal Nolang strings to NUL-terminated char*, convert
// the C result back" (realpath, getlogin, the whole stat family, sync, num-cpu,
// ...). Those are described here declaratively as a cCallSpec and emitted by one
// implementation, emitCCall.
//
// The spec is deliberately ABI-complete rather than minimal:
//
//   - args know whether they are a scalar, a NUL-terminated C string, a raw
//     container data pointer, or a caller-owned scratch buffer (pointer ABI);
//   - returns know whether they are void, a widened integer, a zext'd boolean, a
//     C string to be adopted as an owned %str-long, a string left in a caller
//     buffer, or an i64 field read out of that buffer at a byte offset (the
//     `struct stat` family);
//   - cRetSret covers the System V sret convention (hidden struct-return pointer
//     as argument 0) for C functions that genuinely return a struct by value;
//   - Pair describes the SECOND Nolang result that several of these builtins
//     expose (`stat-size` -> (size, ok), `mkstemp` -> (name, fd)).
//
// Anything that is NOT a plain C call (sort-asc, format, vec-push, socket setup,
// ...) keeps its dedicated handler in builtin_call.go; emitBuiltinForward consults
// this table only after those have had their chance.

import (
	"fmt"
	"runtime"
	"strings"
)

// cArgKind tells emitCCall how to marshal one argument for the C call.
type cArgKind int

const (
	// cArgI64 passes the value through as i64 (coerced from i8/i1/i32 as needed).
	cArgI64 cArgKind = iota
	// cArgI32 truncates to i32 (fds and other narrow C parameters).
	cArgI32
	// cArgDouble passes an f64 through.
	cArgDouble
	// cArgCStr hands over a NUL-terminated heap copy of a str/[]byte. The copy
	// is malloc'd; emitCCall frees it right after the call returns.
	cArgCStr
	// cArgRawPtr hands over the container's data pointer with NO terminator
	// (for C entry points that take an explicit length).
	cArgRawPtr
	// cArgBufPtr is a caller-owned scratch buffer of Size bytes: the pointer ABI
	// used by the whole `struct stat` family (C fills it, we read fields out).
	cArgBufPtr
	// cArgFixed is a literal supplied by the spec (buffer sizes, option flags).
	cArgFixed
// cArgNull is an explicit NULL pointer argument.
cArgNull
// cArgI64ToPtr converts an i64 value to i8* via inttoptr (e.g. dir handles).
cArgI64ToPtr
)

// KeepAlive marks a cArgCStr buffer that the C function rewrites in place
// (mkstemp/mkdtemp write the real name back into the template). The buffer must
// stay alive past the call so the name can be adopted as a Nolang string, and
// is freed only after the results have been converted.

// cArgSpec is one parameter of the C function.
type cArgSpec struct {
	Kind  cArgKind
	From  int    // index into inst.Args; -1 when the argument is compiler-supplied
	Size  int64  // cArgBufPtr: buffer size in bytes
	Fixed string // cArgFixed: literal text
	LLVM  string // cArgFixed: literal type
	// Temp is filled in by emitCCall for cArgBufPtr: the alloca register, so the
	// return conversion can read fields back out of it.
	Temp string
	// KeepAlive (cArgCStr only): do NOT free the NUL-terminated copy after the
	// call; the C function rewrites it in place (mkstemp) and it is adopted as a
	// Nolang string result. emitCCall frees it once results are converted.
	KeepAlive bool
}

// cRetKind tells emitCCall how to turn the C result into a Nolang value.
type cRetKind int

const (
	cRetVoid cRetKind = iota
	// cRetI64: scalar integer result, sext/zext to i64 per Signed.
	cRetI64
	// cRetDouble: f64 result.
	cRetDouble
	// cRetBool: C integer result -> i1 via `icmp eq <t> %r, 0`.
	cRetBool
	// cRetCStrToStr: i8* result -> owned %str-long. NULL -> empty string, so a
	// failed call keeps Nolang's empty semantics instead of crashing strlen.
	cRetCStrToStr
	// cRetBufStr: the result string was written into the scratch buffer at
	// BufIdx; adopt it as an owned %str-long.
	cRetBufStr
	// cRetStatBool: (stat == 0) && (st_mode & ModeMask != 0). Used by is-file
	// (S_IFREG = 32768) and is-dir (S_IFDIR = 16384); a plain stat-success check
	// would wrongly report a directory as a regular file.
	cRetStatBool
	// cRetField: read an i64 out of the scratch buffer at BufIdx + Offset. Used
	// for st_size / st_mode / st_uid / st_gid / st_mtime.
	cRetField
	// cRetSret: System V sret — the caller allocates RetType and passes it as
	// argument 0; C returns void. The filled struct is the Nolang result.
	cRetSret
	// cRetStrFromArg: result 0 is a Nolang string ADOPTED from a KeepAlive
	// cArgCStr buffer that the C call rewrote in place (mkstemp writes the real
	// name into its template). The buffer is freed after the copy.
	cRetStrFromArg
)

// cPairKind describes the SECOND Nolang result of a builtin whose C call already
// carries the information in its return register.
type cPairKind int

const (
	cPairNone cPairKind = iota
	// cPairOKRetCode: ok = (C return >= 0). readlink.
	cPairOKRetCode
	// cPairOKRetPtr: ok = (C return != NULL). mkdtemp.
	cPairOKRetPtr
	// cPairOKRetZero: ok = (C return == 0). stat-* (result 1).
	cPairOKRetZero
	// cPairI64FromRet: result 1 is the C return widened to i64. mkstemp's fd.
	cPairI64FromRet
	// cPairStrFromBuf: result 1 is the string now in the scratch buffer at
	// BufIdx. mkstemp's name (the template is rewritten in place).
	cPairStrFromBuf
)

// cPairSpec describes result index 1.
type cPairSpec struct {
	Kind   cPairKind
	BufIdx int
	Signed bool
}

// cRetSpec describes the C return value and its conversion.
type cRetSpec struct {
	Kind   cRetKind
	LLVM   string // C return type ("void", "i32", "i8*", "double")
	Signed bool   // cRetI64: sext when true, zext when false
	BufIdx int    // cRetBufStr / cRetField: which spec arg is the buffer
	Offset int64  // cRetField: byte offset inside the buffer
	// Width is the native bit-width of the field read by cRetField (16, 32, or
	// 64). struct stat fields are NOT all 64-bit: st_mode is 16-bit, st_uid /
	// st_gid are 32-bit, st_size / st_mtime.tv_sec are 64-bit. Reading the wrong
	// width silently corrupts the result (an i64 read of st_mode at offset 4
	// also swallowed st_nlink and part of st_ino). 0 means 64 (the historical
	// default, used by stat-size / stat-mtime).
	Width int
	// LenFromRet: for cRetBufStr, the string length is the C return value
	// (clamped at 0 on error) instead of strlen — the buffer is NOT
	// NUL-terminated in that case and strlen would run off the end.
	LenFromRet bool
	// TermBuf: for cRetBufStr with LenFromRet, NUL-terminate the buffer at
	// [len] so the generic string path can copy exactly len bytes safely.
	TermBuf bool
	RetType string // cRetSret: LLVM struct type of the returned value
	Pair    cPairSpec
	// ModeMask is the st_mode bit tested by cRetStatBool (e.g. 32768 = S_IFREG,
	// 16384 = S_IFDIR). Ignored by other return kinds.
	ModeMask int64
	// PtrToInt: for cRetI64 with a pointer return type (e.g. opendir -> i8*),
	// emit ptrtoint instead of sext/zext.
	PtrToInt bool
}

// cCallSpec is a complete, declarative description of one C call.
type cCallSpec struct {
	Func string
	Args []cArgSpec
	Ret  cRetSpec
}

// statLayout mirrors build/llvm statLayoutFor: struct stat differs per platform,
// so field offsets must never be hardcoded. Only the fields the nolang builtins
// actually read are listed.
type statLayout struct {
	Size     int64
	ModeOff  int64
	UidOff   int64
	GidOff   int64
	MtimeOff int64
	SizeOff  int64
}

func statLayoutFor() statLayout {
	if runtime.GOOS == "linux" {
		if runtime.GOARCH == "arm64" {
			return statLayout{Size: 128, ModeOff: 16, UidOff: 24, GidOff: 28, MtimeOff: 88, SizeOff: 48}
		}
		return statLayout{Size: 144, ModeOff: 24, UidOff: 28, GidOff: 32, MtimeOff: 88, SizeOff: 48}
	}
	// darwin (and any unknown target, matching legacy's default branch)
	return statLayout{Size: 144, ModeOff: 4, UidOff: 16, GidOff: 20, MtimeOff: 48, SizeOff: 96}
}

// statBufArg is the `struct stat*` scratch-buffer argument shared by the whole
// stat family: the caller owns it, C fills it.
func statBufArg() cArgSpec { return cArgSpec{Kind: cArgBufPtr, From: -1, Size: statLayoutFor().Size} }

func sysconfNProc() string {
	if runtime.GOOS == "linux" {
		return "84" // glibc _SC_NPROCESSORS_ONLN
	}
	return "58" // darwin _SC_NPROCESSORS_ONLN
}

// forwardCSpecs maps a ForwardFunc name to its C call. Adding a builtin is now a
// data change, not a new emitter.
//
// Entries whose semantics are NOT a plain C call (sort-asc, format, vec-push,
// socket setup, ...) are intentionally absent: they keep their dedicated
// handlers and this table must never claim them.
var forwardCSpecs = map[string]cCallSpec{
	// ------------------------------------------------- C-string -> str
	"realpath": {
		// macOS realpath does NOT support the glibc NULL-resolved-buffer
		// extension (it returns NULL -> empty on darwin, and under a clang
		// native binary that is instant garbage). Pass a PATH_MAX stack buffer
		// on every platform instead: realpath writes the resolved path into it
		// and returns the same pointer, which storeCStrResult copies into a
		// Nolang string. This is also strictly better on Linux, where the
		// NULL form malloc()s and would otherwise leak.
		Func: "realpath",
		Args: []cArgSpec{{Kind: cArgCStr, From: 0}, {Kind: cArgBufPtr, From: -1, Size: 4096}},
		Ret:  cRetSpec{Kind: cRetCStrToStr, LLVM: "i8*"},
	},
	"getlogin": {
		Func: "getlogin",
		Ret:  cRetSpec{Kind: cRetCStrToStr, LLVM: "i8*"},
	},
	"mkdtemp": {
		// mkdtemp(tmpl) -> (name, ok). Like mkstemp it rewrites the template
		// buffer in place and returns the same pointer (NULL on failure), so
		// the name is adopted from the KeepAlive buffer and ok = (ret != NULL).
		Func: "mkdtemp",
		Args: []cArgSpec{{Kind: cArgCStr, From: 0, KeepAlive: true}},
		Ret: cRetSpec{
			Kind:   cRetStrFromArg,
			LLVM:   "i8*",
			BufIdx: 0,
			Pair:   cPairSpec{Kind: cPairOKRetPtr},
		},
	},

	// ------------------------------------------------- scratch-buffer strings
	"getdomainname": {
		Func: "getdomainname",
		Args: []cArgSpec{{Kind: cArgBufPtr, From: -1, Size: 1024}, {Kind: cArgFixed, Fixed: "1024", LLVM: "i64"}},
		Ret:  cRetSpec{Kind: cRetBufStr, LLVM: "i32", BufIdx: 0},
	},
	"readlink": {
		// readlink(path, buf, n) -> byte count, or -1 on error. The buffer is NOT
		// NUL-terminated, so the length comes from the return value, not strlen.
		Func: "readlink",
		Args: []cArgSpec{{Kind: cArgCStr, From: 0}, {Kind: cArgBufPtr, From: -1, Size: 4096}, {Kind: cArgFixed, Fixed: "4096", LLVM: "i64"}},
		Ret: cRetSpec{
			Kind: cRetBufStr, LLVM: "i64", BufIdx: 1,
			LenFromRet: true, TermBuf: true,
			Pair: cPairSpec{Kind: cPairOKRetCode},
		},
	},
	"mkstemp": {
		// mkstemp(tmpl) -> (name, fd). The C call returns the fd and rewrites
		// tmpl IN PLACE with the real name, so result 0 is the string adopted
		// from the (KeepAlive) template buffer and result 1 is the fd.
		Func: "mkstemp",
		Args: []cArgSpec{{Kind: cArgCStr, From: 0, KeepAlive: true}},
		Ret: cRetSpec{
			Kind:   cRetStrFromArg,
			LLVM:   "i32",
			BufIdx: 0,
			Pair:   cPairSpec{Kind: cPairI64FromRet, Signed: true},
		},
	},

	// ------------------------------------------------- struct stat family
	"stat-file":   {Func: "stat", Args: []cArgSpec{{Kind: cArgCStr, From: 0}, statBufArg()}, Ret: cRetSpec{Kind: cRetStatBool, LLVM: "i32", BufIdx: 1, Offset: statLayoutFor().ModeOff, ModeMask: 32768}},
	"stat-dir":    {Func: "stat", Args: []cArgSpec{{Kind: cArgCStr, From: 0}, statBufArg()}, Ret: cRetSpec{Kind: cRetStatBool, LLVM: "i32", BufIdx: 1, Offset: statLayoutFor().ModeOff, ModeMask: 16384}},
	"stat-exists": {Func: "stat", Args: []cArgSpec{{Kind: cArgCStr, From: 0}, statBufArg()}, Ret: cRetSpec{Kind: cRetBool, LLVM: "i32"}},
	"lstat":       {Func: "lstat", Args: []cArgSpec{{Kind: cArgCStr, From: 0}, statBufArg()}, Ret: cRetSpec{Kind: cRetBool, LLVM: "i32"}},
	"stat-size": {
		Func: "stat", Args: []cArgSpec{{Kind: cArgCStr, From: 0}, statBufArg()},
		Ret: cRetSpec{Kind: cRetField, LLVM: "i32", BufIdx: 1, Offset: statLayoutFor().SizeOff, Pair: cPairSpec{Kind: cPairOKRetZero}},
	},
	"fstat-size": {
		Func: "fstat", Args: []cArgSpec{{Kind: cArgI32, From: 0}, statBufArg()},
		Ret: cRetSpec{Kind: cRetField, LLVM: "i32", BufIdx: 1, Offset: statLayoutFor().SizeOff, Pair: cPairSpec{Kind: cPairOKRetZero}},
	},
	"stat-mode": {
		Func: "stat", Args: []cArgSpec{{Kind: cArgCStr, From: 0}, statBufArg()},
		Ret: cRetSpec{Kind: cRetField, LLVM: "i32", BufIdx: 1, Offset: statLayoutFor().ModeOff, Width: 16, Pair: cPairSpec{Kind: cPairOKRetZero}},
	},
	"stat-uid": {
		Func: "stat", Args: []cArgSpec{{Kind: cArgCStr, From: 0}, statBufArg()},
		Ret: cRetSpec{Kind: cRetField, LLVM: "i32", BufIdx: 1, Offset: statLayoutFor().UidOff, Width: 32, Pair: cPairSpec{Kind: cPairOKRetZero}},
	},
	"stat-gid": {
		Func: "stat", Args: []cArgSpec{{Kind: cArgCStr, From: 0}, statBufArg()},
		Ret: cRetSpec{Kind: cRetField, LLVM: "i32", BufIdx: 1, Offset: statLayoutFor().GidOff, Width: 32, Pair: cPairSpec{Kind: cPairOKRetZero}},
	},
	"stat-mtime": {
		Func: "stat", Args: []cArgSpec{{Kind: cArgCStr, From: 0}, statBufArg()},
		Ret: cRetSpec{Kind: cRetField, LLVM: "i32", BufIdx: 1, Offset: statLayoutFor().MtimeOff, Pair: cPairSpec{Kind: cPairOKRetZero}},
	},

	// ------------------------------------------------- scalar / void
	"sync": {Func: "sync", Ret: cRetSpec{Kind: cRetVoid, LLVM: "void"}},
	"num-cpu": {
		Func: "sysconf",
		Args: []cArgSpec{{Kind: cArgFixed, Fixed: sysconfNProc(), LLVM: "i32"}},
		Ret:  cRetSpec{Kind: cRetI64, LLVM: "i64", Signed: true},
	},

	// ------------------------------------------------- process
	"process-fork": {Func: "fork", Ret: cRetSpec{Kind: cRetI64, LLVM: "i32", Signed: true}},

	// ------------------------------------------------- touch / dir
	"touch-file": {
		Func: "utimensat",
		Args: []cArgSpec{
			{Kind: cArgFixed, Fixed: "-2", LLVM: "i32"}, // AT_FDCWD
			{Kind: cArgCStr, From: 0},                    // path
			{Kind: cArgNull},                              // NULL (times = now)
			{Kind: cArgFixed, Fixed: "0", LLVM: "i32"},   // flags
		},
		Ret: cRetSpec{Kind: cRetBool, LLVM: "i32"},
	},
	"open-dir": {
		Func: "opendir",
		Args: []cArgSpec{{Kind: cArgCStr, From: 0}},
		Ret:  cRetSpec{Kind: cRetI64, LLVM: "i8*", PtrToInt: true},
	},
	"close-dir": {
		Func: "closedir",
		Args: []cArgSpec{{Kind: cArgI64ToPtr, From: 0}},
		Ret:  cRetSpec{Kind: cRetBool, LLVM: "i32"},
	},
}

// forwardCSpecOf resolves a ForwardFunc name against the C-call table. It
// returns nil when the builtin needs a bespoke lowering.
//
// The returned spec is a copy so emitCCall can stash per-call scratch registers
// in Args[i].Temp without mutating the shared table across call sites.
func forwardCSpecOf(ff string) *cCallSpec {
	sp, ok := forwardCSpecs[ff]
	if !ok {
		return nil
	}
	cp := sp
	cp.Args = append([]cArgSpec(nil), sp.Args...)
	return &cp
}

// emitCCall emits `declare` + argument marshalling + `call` + result conversion
// for one cCallSpec, storing the Nolang result(s) into inst.Results' slots.
//
// Result convention (Nolang out-parameter ABI): inst.Results[i] is the MIR value
// for the i-th declared result and each owns an alloca slot. A C call produces
// its value in a register, so the value is stored into that slot — the same
// shape emitCall uses for nolang functions, which keeps ownership uniform (the
// drop pass frees str results exactly like any other owned local).
func (c *codegen) emitCCall(inst *Inst, spec *cCallSpec) error {
	if spec == nil || spec.Func == "" {
		return fmt.Errorf("builtin %s: no C call spec", inst.Sym)
	}

	// --- declare -----------------------------------------------------------
	argTys := make([]string, 0, len(spec.Args))
	for _, a := range spec.Args {
		argTys = append(argTys, cArgLLVMType(a))
	}
	c.decl(fmt.Sprintf("declare %s @%s(%s)", spec.Ret.LLVM, spec.Func, strings.Join(argTys, ", ")))

	// --- marshal arguments -------------------------------------------------
	var callArgs []string
	var toFree []string
	var keepFree []string // KeepAlive C-string copies, freed only after results
	for i := range spec.Args {
		a := &spec.Args[i]
		switch a.Kind {
		case cArgFixed:
			callArgs = append(callArgs, a.LLVM+" "+a.Fixed)
		case cArgNull:
			callArgs = append(callArgs, "i8* null")
		case cArgBufPtr:
			r := c.treg("cbuf")
			// Allocate with 8-byte alignment. struct stat (and every other
			// scratch buffer we hand to libc) must be naturally aligned: a
			// 1-byte-aligned `alloca i8` lets the optimizer legalize an
			// `align 2` i16 / `align 8` i64 read as UB and fold it to poison,
			// which silently broke is-file/is-dir (mode bit always read as 0).
			c.sb.WriteString(fmt.Sprintf("  %s = alloca i8, i64 %d, align 8\n", r, a.Size))
			a.Temp = r
			callArgs = append(callArgs, "i8* "+r)
		case cArgCStr:
			v, err := c.argIndex(inst, a.From)
			if err != nil {
				return err
			}
			s := c.cstrOf(v)
			if s == "" {
				return fmt.Errorf("builtin %s: cannot marshal arg %d as C string", inst.Sym, a.From)
			}
			if a.KeepAlive {
				// mkstemp rewrites this buffer in place with the real name;
				// keep it so the name can be adopted, and free it last.
				a.Temp = s
				keepFree = append(keepFree, s)
			} else {
				toFree = append(toFree, s)
			}
			callArgs = append(callArgs, "i8* "+s)
		case cArgRawPtr:
			v, err := c.argIndex(inst, a.From)
			if err != nil {
				return err
			}
			s := c.dataPtrOf(v)
			if s == "" {
				return fmt.Errorf("builtin %s: cannot take data pointer of arg %d", inst.Sym, a.From)
			}
			callArgs = append(callArgs, "i8* "+s)
		case cArgI32:
			v, err := c.marshalScalar(inst, a.From, "i32")
			if err != nil {
				return fmt.Errorf("builtin %s: %v", inst.Sym, err)
			}
			callArgs = append(callArgs, "i32 "+v)
		case cArgDouble:
			v, err := c.marshalScalar(inst, a.From, "double")
			if err != nil {
				return fmt.Errorf("builtin %s: %v", inst.Sym, err)
			}
			callArgs = append(callArgs, "double "+v)
		case cArgI64ToPtr:
			v, err := c.marshalScalar(inst, a.From, "i64")
			if err != nil {
				return fmt.Errorf("builtin %s: %v", inst.Sym, err)
			}
			p := c.treg("i2p")
			c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", p, v))
			callArgs = append(callArgs, "i8* "+p)
		default:
			v, err := c.marshalScalar(inst, a.From, "i64")
			if err != nil {
				return fmt.Errorf("builtin %s: %v", inst.Sym, err)
			}
			callArgs = append(callArgs, "i64 "+v)
		}
	}

	// sret: the hidden struct-return pointer is argument 0 and the destination
	// is a caller-allocated slot of the struct type.
	var sretSlot string
	if spec.Ret.Kind == cRetSret {
		c.loadSeq++
		sretSlot = fmt.Sprintf("%%sret%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = alloca %s\n", sretSlot, spec.Ret.RetType))
		callArgs = append([]string{spec.Ret.RetType + "* " + sretSlot}, callArgs...)
	}

	// --- the call ----------------------------------------------------------
	var callReg, callTy string
	if spec.Ret.LLVM != "void" {
		callReg = c.treg("cc")
		callTy = spec.Ret.LLVM
		c.sb.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", callReg, callTy, spec.Func, strings.Join(callArgs, ", ")))
	} else {
		c.sb.WriteString(fmt.Sprintf("  call void @%s(%s)\n", spec.Func, strings.Join(callArgs, ", ")))
	}
	// Release the NUL-terminated copies now that C is done with them; keeping
	// them alive would leak one buffer per call.
	for _, s := range toFree {
		c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", s))
	}

	// --- convert the result ------------------------------------------------
	switch spec.Ret.Kind {
	case cRetVoid:
		return nil
	case cRetBool:
		v := c.cToBool(callReg, callTy)
		return c.storeResult(inst, 0, v, "i1")
	case cRetStatBool:
		// (stat == 0) && (st_mode & ModeMask != 0). Mirrors legacy is-file /
		// is-dir: stat-failure alone is not enough — a directory is not a file.
		buf := spec.Args[spec.Ret.BufIdx].Temp
		if buf == "" {
			return fmt.Errorf("builtin %s: scratch buffer not materialized", inst.Sym)
		}
		okCmp := c.treg("sfok")
		c.sb.WriteString(fmt.Sprintf("  %s = icmp eq %s %s, 0\n", okCmp, callTy, callReg))
		mg := c.treg("sfmg")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %d\n", mg, buf, spec.Ret.Offset))
		// Read st_mode as i32 (NOT i16): the bitcast i8*->i16* + load i16 pair
		// miscompiles on this backend (the mode bit reads as 0), whereas the
		// analogous i64 field read (cRetField) is reliable. 32768 (S_IFREG) and
		// 16384 (S_IFDIR) both fit in i32, so the mask is exact.
		mp := c.treg("sfmp")
		c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to i32*\n", mp, mg))
		ml := c.treg("sfml")
		c.sb.WriteString(fmt.Sprintf("  %s = load i32, i32* %s\n", ml, mp))
		an := c.treg("sfan")
		c.sb.WriteString(fmt.Sprintf("  %s = and i32 %s, %d\n", an, ml, spec.Ret.ModeMask))
		c2 := c.treg("sfc2")
		c.sb.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", c2, an))
		ext := c.treg("sfex")
		c.sb.WriteString(fmt.Sprintf("  %s = and i1 %s, %s\n", ext, okCmp, c2))
		return c.storeResult(inst, 0, ext, "i1")
	case cRetDouble:
		return c.storeResult(inst, 0, callReg, "double")
	case cRetI64:
		var v string
		if spec.Ret.PtrToInt {
			r := c.treg("p2i")
			c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint %s %s to i64\n", r, callTy, callReg))
			v = r
		} else {
			v = c.cToI64(callReg, callTy, spec.Ret.Signed)
		}
		return c.storeResult(inst, 0, v, "i64")
	case cRetCStrToStr:
		if err := c.storeCStrResult(inst, 0, callReg); err != nil {
			return err
		}
	case cRetStrFromArg:
		// result 0 is the name the C call rewrote into the KeepAlive input
		// buffer (mkstemp). Adopt it as an owned %str-long.
		buf := spec.Args[spec.Ret.BufIdx].Temp
		if buf == "" {
			return fmt.Errorf("builtin %s: keep-alive buffer not materialized", inst.Sym)
		}
		if err := c.storeBufStrResult(inst, 0, buf); err != nil {
			return err
		}
	case cRetBufStr:
		buf := spec.Args[spec.Ret.BufIdx].Temp
		if buf == "" {
			return fmt.Errorf("builtin %s: scratch buffer not materialized", inst.Sym)
		}
		if spec.Ret.LenFromRet {
			if err := c.storeBufStrLenResult(inst, 0, buf, callReg, callTy, spec.Ret.TermBuf); err != nil {
				return err
			}
		} else {
			if err := c.storeBufStrResult(inst, 0, buf); err != nil {
				return err
			}
		}
	case cRetField:
		buf := spec.Args[spec.Ret.BufIdx].Temp
		if buf == "" {
			return fmt.Errorf("builtin %s: scratch buffer not materialized", inst.Sym)
		}
		w := spec.Ret.Width
		if w == 0 {
			w = 64 // default: native 64-bit field (st_size / st_mtime.tv_sec)
		}
		v := c.loadFieldAtWidth(buf, spec.Ret.Offset, w)
		// stat-success is the i32 C return register.
		okCmp := ""
		if callReg != "" {
			okCmp = c.treg("fok")
			c.sb.WriteString(fmt.Sprintf("  %s = icmp eq %s %s, 0\n", okCmp, callTy, callReg))
		}
		// Option-returning builtins (stat-size / file-size / fstat-size): std
		// declares them as a single `?i64`, so the stat-success flag must
		// become the option discriminant (tag 0 = ok, tag 1 = nil) instead of
		// a second result — exactly what legacy's generateOptionAssign does
		// (build/llvm/stmt.go). Emitting a bare i64 instead leaves `?=`
		// comparisons against `err` untyped -> `icmp eq i64 %v, undef` ->
		// `unreachable` -> SIGTRAP (tests/test-open-read.no).
		if lt, ok := c.resultType(inst, 0); ok && isOptionType(lt) {
			return c.storeOptionFromPair(inst, v, okCmp)
		}
		// Mirror legacy stat-* exactly: when stat() fails the scratch buffer is
		// left uninitialized, so the field must return 0 rather than whatever
		// garbage the alloca held.
		if okCmp != "" {
			sel := c.treg("fsel")
			c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 0\n", sel, okCmp, v))
			v = sel
		}
		if err := c.storeResult(inst, 0, v, "i64"); err != nil {
			return err
		}
	case cRetSret:
		if sretSlot == "" {
			return fmt.Errorf("builtin %s: sret slot not allocated", inst.Sym)
		}
		lt, ok := c.resultType(inst, 0)
		if !ok || lt == "" {
			lt = spec.Ret.RetType
		}
		r := c.treg("srl")
		c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", r, lt, lt, sretSlot))
		if err := c.storeResult(inst, 0, r, lt); err != nil {
			return err
		}
	default:
		return fmt.Errorf("builtin %s: unhandled C return kind", inst.Sym)
	}

	// --- the paired second result -----------------------------------------
	if err := c.storePairResult(inst, spec, callReg, callTy); err != nil {
		return err
	}
	// Free any KeepAlive C-string copies now that their contents have been
	// adopted into Nolang strings (mkstemp's rewritten template buffer).
	for _, s := range keepFree {
		c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", s))
	}
	return nil
}

// storeOptionFromPair stores a (value, okFlag) pair into a `?T` result as the
// flat `%option { tag, data }`: tag 0 (ok/some) when the flag is set, tag 1
// (nil/none) otherwise; data is the value. This mirrors legacy's
// generateOptionAssign for option-returning builtins.
func (c *codegen) storeOptionFromPair(inst *Inst, val, okFlag string) error {
	tag := c.treg("optg")
	if okFlag != "" {
		c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 0, i64 1\n", tag, okFlag))
	} else {
		c.sb.WriteString(fmt.Sprintf("  %s = select i1 true, i64 0, i64 1\n", tag))
	}
	w1 := c.treg("optw")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%option { i64 0, i64 0 }, i64 %s, 0\n", w1, tag))
	w2 := c.treg("optw")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%option %s, i64 %s, 1\n", w2, w1, val))
	return c.storeResult(inst, 0, w2, "%option")
}

// storePairResult fills result index 1 when the C return register carries it.
func (c *codegen) storePairResult(inst *Inst, spec *cCallSpec, callReg, callTy string) error {
	if len(inst.Results) < 2 || inst.Results[1] <= NoVal {
		return nil
	}
	switch spec.Ret.Pair.Kind {
	case cPairNone:
		return nil
	case cPairOKRetCode:
		// ok = (ret >= 0)
		cmp := c.treg("pge")
		c.sb.WriteString(fmt.Sprintf("  %s = icmp sge %s %s, 0\n", cmp, callTy, callReg))
		return c.storeResult(inst, 1, cmp, "i1")
	case cPairOKRetPtr:
		// ok = (ret != NULL)
		cmp := c.treg("pnn")
		c.sb.WriteString(fmt.Sprintf("  %s = icmp ne i8* %s, null\n", cmp, callReg))
		return c.storeResult(inst, 1, cmp, "i1")
	case cPairOKRetZero:
		// ok = (ret == 0)
		cmp := c.treg("pez")
		c.sb.WriteString(fmt.Sprintf("  %s = icmp eq %s %s, 0\n", cmp, callTy, callReg))
		return c.storeResult(inst, 1, cmp, "i1")
	case cPairI64FromRet:
		v := c.cToI64(callReg, callTy, spec.Ret.Pair.Signed)
		return c.storeResult(inst, 1, v, "i64")
	case cPairStrFromBuf:
		buf := spec.Args[spec.Ret.Pair.BufIdx].Temp
		if buf == "" {
			return fmt.Errorf("builtin %s: pair buffer not materialized", inst.Sym)
		}
		return c.storeBufStrResult(inst, 1, buf)
	}
	return nil
}

// cArgLLVMType is the LLVM spelling of a spec argument's C type.
func cArgLLVMType(a cArgSpec) string {
	switch a.Kind {
	case cArgI32:
		return "i32"
	case cArgDouble:
		return "double"
case cArgCStr, cArgRawPtr, cArgBufPtr, cArgNull, cArgI64ToPtr:
	return "i8*"
	case cArgFixed:
		if a.LLVM != "" {
			return a.LLVM
		}
		return "i64"
	}
	return "i64"
}

func (c *codegen) argIndex(inst *Inst, i int) (ValueID, error) {
	if i < 0 || i >= len(inst.Args) {
		return NoVal, fmt.Errorf("missing arg %d", i)
	}
	return inst.Args[i], nil
}

// marshalScalar loads inst.Args[i] and coerces it to wantTy.
func (c *codegen) marshalScalar(inst *Inst, i int, wantTy string) (string, error) {
	v, err := c.argIndex(inst, i)
	if err != nil {
		return "", err
	}
	lt, val := c.loadVal(v)
	if r := c.coerce(lt, val, wantTy); r != "" {
		return r, nil
	}
	return "", fmt.Errorf("cannot coerce arg %d from %s to %s", i, lt, wantTy)
}

// cToBool turns a C integer result into i1 via `icmp eq <t> %r, 0`.
func (c *codegen) cToBool(reg, cTy string) string {
	if cTy == "i1" {
		return reg
	}
	r := c.treg("cbt")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq %s %s, 0\n", r, cTy, reg))
	return r
}

// cToI64 widens a C integer result to i64, sign-aware.
func (c *codegen) cToI64(reg, cTy string, signed bool) string {
	if cTy == "i64" {
		return reg
	}
	r := c.treg("c64")
	if signed {
		c.sb.WriteString(fmt.Sprintf("  %s = sext %s %s to i64\n", r, cTy, reg))
	} else {
		c.sb.WriteString(fmt.Sprintf("  %s = zext %s %s to i64\n", r, cTy, reg))
	}
	return r
}

// loadFieldAtWidth reads a field of native width (16/32/64 bits) out of a scratch
// buffer at a byte offset and zero-extends it to i64 — how the struct-stat
// builtins extract a field without MIR having to model a platform-specific C
// struct. The width MUST match the C field: st_mode is 16-bit, st_uid/st_gid are
// 32-bit, st_size/st_mtime.tv_sec are 64-bit. Reading more bytes than the field
// occupies silently folds neighbouring fields (st_nlink, st_ino, ...) into the
// value, which is exactly the bug that made stat-mode return garbage.
func (c *codegen) loadFieldAtWidth(buf string, off int64, width int) string {
	g := c.treg("fgep")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %d\n", g, buf, off))
	p := c.treg("fptr")
	switch width {
	case 16:
		c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to i16*\n", p, g))
		l := c.treg("fld")
		c.sb.WriteString(fmt.Sprintf("  %s = load i16, i16* %s\n", l, p))
		z := c.treg("fzxt")
		c.sb.WriteString(fmt.Sprintf("  %s = zext i16 %s to i64\n", z, l))
		return z
	case 32:
		c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to i32*\n", p, g))
		l := c.treg("fld")
		c.sb.WriteString(fmt.Sprintf("  %s = load i32, i32* %s\n", l, p))
		z := c.treg("fzxt")
		c.sb.WriteString(fmt.Sprintf("  %s = zext i32 %s to i64\n", z, l))
		return z
	default: // 64
		c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to i64*\n", p, g))
		l := c.treg("fld")
		c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", l, p))
		return l
	}
}

// resultType is the LLVM type of the i-th MIR result value. The bool reports
// whether a usable LLVM type was found (i.e. the result exists and is not
// void). Callers must NOT treat the bool as ownership — ptype's second return
// is "owned", and conflating the two previously made every non-owned second
// result (i64 fd, i1 ok, ...) silently drop its store (see mkstemp/mkdtemp).
func (c *codegen) resultType(inst *Inst, i int) (string, bool) {
	if i >= len(inst.Results) || inst.Results[i] <= NoVal {
		return "", false
	}
	lt, _ := c.ptype(inst.Results[i])
	if lt == "" {
		return "", false
	}
	return lt, true
}

// storeResult writes v (of LLVM type srcTy) into the slot of the i-th result,
// coercing as needed. A missing result or slot is not an error: Nolang lets a
// call be used purely for its side effect.
func (c *codegen) storeResult(inst *Inst, i int, v, srcTy string) error {
	lt, ok := c.resultType(inst, i)
	if !ok {
		return nil
	}
	slot := c.valSlot[inst.Results[i]]
	if slot == "" {
		return fmt.Errorf("builtin %s: result %d has no slot", inst.Sym, i)
	}
	if srcTy != lt {
		if r := c.coerce(srcTy, v, lt); r != "" {
			v = r
		} else {
			return fmt.Errorf("builtin %s: cannot store %s result into %s slot", inst.Sym, srcTy, lt)
		}
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", lt, v, lt, slot))
	return nil
}

// emptyStrGlobal is the shared 1-byte NUL terminator used whenever a C string
// comes back NULL: handing NULL to strlen/memcpy would be instant UB, and
// Nolang's convention is that a failed lookup yields the empty string.
func (c *codegen) emptyStrGlobal() string {
	c.global("@.mir.empty = private constant [1 x i8] zeroinitializer")
	return "getelementptr inbounds ([1 x i8], [1 x i8]* @.mir.empty, i64 0, i64 0)"
}

// storeCStrResult adopts a C string (i8*) as an owned %str-long, mapping NULL to
// the empty string.
func (c *codegen) storeCStrResult(inst *Inst, i int, ptr string) error {
	lt, ok := c.resultType(inst, i)
	if !ok {
		return nil
	}
	if lt != "%str-long" {
		return fmt.Errorf("builtin %s: string result stored into %s slot", inst.Sym, lt)
	}
	cmp := c.treg("scn")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ne i8* %s, null\n", cmp, ptr))
	safe := c.treg("scs")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i8* %s, i8* %s\n", safe, cmp, ptr, c.emptyStrGlobal()))
	return c.storeBufStrResult(inst, i, safe)
}

// storeBufStrResult copies a NUL-terminated buffer into a freshly malloc'd
// %str-long { len, cap, data }. The copy is mandatory: the source may be a libc
// static buffer (getlogin) or a caller scratch buffer that dies at frame exit,
// and a %str-long is freed by MIR's drop pass, so it must own heap memory.
func (c *codegen) storeBufStrResult(inst *Inst, i int, src string) error {
	lt, ok := c.resultType(inst, i)
	if !ok {
		return nil
	}
	if lt != "%str-long" {
		return fmt.Errorf("builtin %s: string result stored into %s slot", inst.Sym, lt)
	}
	slot := c.valSlot[inst.Results[i]]
	if slot == "" {
		return fmt.Errorf("builtin %s: result %d has no slot", inst.Sym, i)
	}
	lenR := c.treg("bsl")
	c.sb.WriteString(fmt.Sprintf("  %s = call i64 @strlen(i8* %s)\n", lenR, src))
	return c.copyStrToSlot(slot, src, lenR)
}

// storeBufStrLenResult is storeBufStrResult for C entry points that report the
// length instead of NUL-terminating (readlink). The length is clamped at 0 on
// error and, when term is set, the buffer is NUL-terminated in place first so
// the shared copy routine stays correct.
func (c *codegen) storeBufStrLenResult(inst *Inst, i int, buf, retReg, retTy string, term bool) error {
	lt, ok := c.resultType(inst, i)
	if !ok {
		return nil
	}
	if lt != "%str-long" {
		return fmt.Errorf("builtin %s: string result stored into %s slot", inst.Sym, lt)
	}
	slot := c.valSlot[inst.Results[i]]
	if slot == "" {
		return fmt.Errorf("builtin %s: result %d has no slot", inst.Sym, i)
	}
	// len = (ret >= 0) ? ret : 0
	okCmp := c.treg("blc")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sge %s %s, 0\n", okCmp, retTy, retReg))
	lenR := c.treg("bll")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, %s %s, %s 0\n", lenR, okCmp, retTy, retReg, retTy))
	widen := lenR
	if retTy != "i64" {
		widen = c.cToI64(lenR, retTy, false)
	}
	if term {
		// buf[len] = 0 — the buffer is over-allocated for exactly this.
		g := c.treg("btg")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", g, buf, widen))
		c.sb.WriteString(fmt.Sprintf("  store i8 0, i8* %s\n", g))
	}
	return c.copyStrToSlot(slot, buf, widen)
}

// copyStrToSlot builds %str-long { len, cap=len, data=malloc'd copy } into slot.
func (c *codegen) copyStrToSlot(slot, src, lenR string) error {
	sz := c.treg("bss")
	c.sb.WriteString(fmt.Sprintf("  %s = add i64 %s, 1\n", sz, lenR))
	buf := c.treg("bsb")
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", buf, sz))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 0)\n", buf, src, sz))
	r0 := c.treg("bs0")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long { i64 0, i64 0, i8* null }, i64 %s, 0\n", r0, lenR))
	r1 := c.treg("bs1")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long %s, i64 %s, 1\n", r1, r0, lenR))
	r2 := c.treg("bs2")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long %s, i8* %s, 2\n", r2, r1, buf))
	c.sb.WriteString(fmt.Sprintf("  store %%str-long %s, %%str-long* %s\n", r2, slot))
	return nil
}
