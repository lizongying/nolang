package builtin

import "github.com/lizongying/nolang/parser"

type LLVMArgType int

const (
	LLVMI64 LLVMArgType = iota
	LLVMF64
	LLVMI8Ptr
	LLVMI32
	LLVMStrPtr
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
}

type LLVMConvKind int

const (
	LLVMConvI64ToFP LLVMConvKind = iota
	LLVMConvFPToI64
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
