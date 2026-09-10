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
	FuncName        string
	ArgTypes        []LLVMArgType
	RetType         LLVMArgType
	RetExt          *LLVMArgType
	SprintfFmt      string
	BufGlobal       string
	RetBuf          bool
	CmpRet          bool
	FixedArgs       map[int]string
	FixedArgGlobals map[int]string
	TruncArgs       map[int]LLVMArgType
	StrDataArg      map[int]bool
	// RetCStrToStr: C 函數返回 i8* (C 字串)，需轉換為 Nolang %str-long
	// 通過 strlen 計算長度，並把 (len, ptr) 寫入目標 %str-long。
	RetCStrToStr bool
}

type LLVMConvKind int

const (
	LLVMConvI64ToFP LLVMConvKind = iota
	LLVMConvFPToI64
	LLVMConvF64ToF32
	LLVMConvF32ToF64
)

type BuiltinMethod struct {
	ReceiverType  ReceiverKind
	MethodName    string
	Params        []parser.Type
	Return        []parser.Type
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
