package builtin

import "github.com/lizongying/nolang/parser"

type BuiltinMethod struct {
	ReceiverType  ReceiverKind
	MethodName    string
	Params        []parser.Type
	Return        []parser.Type
	Doc           string
	ForwardFunc   string
	LLVMIntrinsic string
}

var BuiltinMethodList = []BuiltinMethod{}
