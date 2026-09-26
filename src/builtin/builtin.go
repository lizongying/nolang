package builtin

import "github.com/lizongying/nolang/parser"

type LLVMArgType int

const (
	LLVMI64 LLVMArgType = iota
	LLVMF64
	LLVMI8Ptr
	LLVMI32
	LLVMStrPtr
	LLVMI64Ptr
)

type CLibCall struct {
	FuncName string
	// AltFuncs / AltArgTypes / AltFixedArgs override FuncName / ArgTypes /
	// FixedArgs per COMPILATION TARGET, keyed by GOOS ("windows", "linux",
	// "darwin").
	//
	// The C symbol a builtin stands for is a property of the platform the
	// binary will RUN on, not of the machine that builds it: `os.mkfifo` is
	// `mkfifo` on POSIX but has no Windows counterpart at all, `open`'s
	// O_CREAT|O_TRUNC|O_WRONLY literal is 1537 on macOS, 577 on Linux and 769
	// on Windows. When these choices were made with runtime.GOOS inside init(),
	// a cross build (e.g. `-target x86_64-windows-gnu` from a Linux runner)
	// baked the HOST's symbols and constants into the target binary, and the
	// link died on `undefined symbol: mkfifo/uname/setenv/...` because the
	// Windows C runtime never exports them. The registry cannot consult the
	// target itself — init() runs long before the driver has parsed
	// `-target` — so it records every variant here and the codegen picks one
	// through mir.targetGOOS() at emission time (see emitBuiltinCLib).
	//
	// Each Alt* map REPLACES its default wholesale — a per-index merge would
	// silently keep a host value (or a host arity) for a slot the target
	// variant forgot to list. set-set-env is the case that needs all three:
	// Windows spells it _putenv_s(key, value), two args instead of POSIX's
	// three (the overwrite flag disappears with the third parameter).
	AltFuncs     map[string]string
	AltArgTypes  map[string][]LLVMArgType
	AltFixedArgs map[string]map[int]string
	ArgTypes     []LLVMArgType
	RetType      LLVMArgType
	RetExt       *LLVMArgType
	SprintfFmt   string
	BufGlobal    string
	RetBuf       bool
	CmpRet       bool
	FixedArgs    map[int]string
	FixedArgGlobals map[int]string
	TruncArgs       map[int]LLVMArgType
	StrDataArg      map[int]bool
	// RetCStrToStr: C 函數返回 i8* (C 字串)，需轉換為 Nolang %str-long
	// 通過 strlen 計算長度，並把 (len, ptr) 寫入目標 %str-long。
	RetCStrToStr bool
}

// altWin is the shorthand for AltFuncs{"windows": fn}. Almost every target
// variant in the registry is this one: a POSIX entry point that MSVCRT spells
// differently (_getcwd) or does not export at all (mkfifo → nolang.win_mkfifo,
// see mir/builtin_win_shims.go).
func altWin(fn string) map[string]string {
	return map[string]string{"windows": fn}
}

type LLVMConvKind int

const (
	LLVMConvI64ToFP LLVMConvKind = iota
	LLVMConvFPToI64
	LLVMConvF64ToF32
	LLVMConvF32ToF64
)

type BuiltinMethod struct {
	ReceiverType ReceiverKind
	MethodName   string
	Params       []parser.Type
	// ElemParams marks, positionally, which entries of Params are the
	// RECEIVER'S ELEMENT TYPE rather than a plain scalar (an index, a length).
	//
	// The registry is element-type-agnostic — a slice method is registered once
	// and serves every []T — so Params can only record the LOWEST-LEVEL shape,
	// which is i64 for both `[]t.push` (element) and `[]t.remove` (index).
	// Without this marker a consumer cannot tell them apart, and substituting
	// the receiver's element type into every parameter rejects a legal
	// `[]str.remove(0)` as "expected 'str', got 'i64'".
	//
	// Len(ElemParams) may be shorter than len(Params); unmarked positions are
	// scalar. Only consumers that need a language-level signature read it
	// (LSP hover, the checker's argument check); codegen dispatches on the
	// method name and ignores Params entirely.
	ElemParams []bool
	Return     []parser.Type
	// Doc / ForwardFunc / LLVMIntrinsic / CLibCall / LLVMConv are aligned with
	// the block above; gofmt owns this spacing.
	Doc           string
	ForwardFunc   string
	LLVMIntrinsic string
	CLibCall      *CLibCall
	LLVMConv      *LLVMConvKind
	// Intercepted marks a pseudo-builtin that is never emitted as a real call:
	// the codegen intercepts the call site and expands it inline (e.g. str.byte /
	// txt.byte raw-byte accessors become a GEP+load+zext). It exists solely so the
	// MIR lowerer can learn the result type; such entries intentionally carry no
	// ForwardFunc / LLVMIntrinsic / CLibCall / LLVMConv.
	Intercepted bool
}

var BuiltinMethodList = []BuiltinMethod{}

func FindBuiltinMethod(name string) *BuiltinMethod {
	for i := range BuiltinMethodList {
		if BuiltinMethodList[i].MethodName == name {
			return &BuiltinMethodList[i]
		}
	}
	return nil
}

// optionReturnBuiltins lists builtins whose std declaration is a single option
// (?i64) even though the registry Return carries the raw C pair (T, ok):
//
//	stat-size = (p str) (size ?i64)      ; fs.no
//	fstat-size = (fd fd) (size ?i64)     ; fs.no
//	file-size = (p str) (size ?i64)      ; fs.no
//
// For these, the ok flag is carried out of the LLVM handler via
// Generator.lastBuiltinExtra (same channel as get-line's ok), and the
// assignment layer (generateOptionAssign) materializes the %option
// {tag, value} struct: tag 0 = ok, tag 1 = nil (C call failed).
// Callers that need the type must consult this set INSTEAD of m.Return,
// because Return[1] (the bool) is meaningless at the language level here —
// unlike genuinely pair-valued builtins (readlink, mkdtemp, get-priority, ...)
// whose std declarations are (T, ok bool) two-result contracts.
var optionReturnBuiltins = map[string]bool{
	"stat-size":  true,
	"file-size":  true,
	"fstat-size": true,
}

// IsOptionReturnBuiltin reports whether the builtin is std-declared as a
// single ?T option (see optionReturnBuiltins).
func IsOptionReturnBuiltin(name string) bool {
	return optionReturnBuiltins[name]
}
