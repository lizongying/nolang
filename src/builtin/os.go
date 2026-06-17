package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	i64Type := LLVMI64

	// get-env: get environment variable
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "get-env",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Get the value of an environment variable",
		CLibCall:     &CLibCall{FuncName: "getenv", ArgTypes: []LLVMArgType{LLVMStrPtr}, RetType: LLVMI8Ptr},
	})

	// set-env: set environment variable
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "set-env",
		Params:       []parser.Type{parser.TypeStr, parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Set the value of an environment variable",
		CLibCall:     &CLibCall{FuncName: "setenv", ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMStrPtr, LLVMI32}, RetType: LLVMI32, RetExt: &i64Type, FixedArgs: map[int]string{2: "1"}},
	})

	// get-wd: get current working directory (uses @.os-buf)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "get-wd",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Get the current working directory",
		CLibCall:     &CLibCall{FuncName: "getcwd", ArgTypes: []LLVMArgType{LLVMI8Ptr, LLVMI64}, RetType: LLVMI8Ptr, RetBuf: true, BufGlobal: "@.os-buf", FixedArgs: map[int]string{1: "1024"}},
	})

	// ch-dir: change current working directory
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "ch-dir",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Change the current working directory",
		CLibCall:     &CLibCall{FuncName: "chdir", ArgTypes: []LLVMArgType{LLVMStrPtr}, RetType: LLVMI32, RetExt: &i64Type},
	})

	// exit: exit the process with status code
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "exit",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Exit the process with the given status code",
		CLibCall:     &CLibCall{FuncName: "exit", ArgTypes: []LLVMArgType{LLVMI32}, RetType: LLVMI32},
	})

	// get-pid: get process ID
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "get-pid",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get the current process ID",
		CLibCall:     &CLibCall{FuncName: "getpid", ArgTypes: []LLVMArgType{}, RetType: LLVMI32, RetExt: &i64Type},
	})

	// host-name: get the hostname (uses @.os-buf)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "host-name",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Get the system hostname",
		CLibCall:     &CLibCall{FuncName: "gethostname", ArgTypes: []LLVMArgType{LLVMI8Ptr, LLVMI64}, RetType: LLVMI32, RetBuf: true, BufGlobal: "@.os-buf", FixedArgs: map[int]string{1: "1024"}},
	})

	// mkdir: create a directory
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "mkdir",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Create a directory with the given mode",
		CLibCall:     &CLibCall{FuncName: "mkdir", ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI32}, RetType: LLVMI32, RetExt: &i64Type},
	})

	// remove: remove a file
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "remove",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Remove (unlink) a file",
		CLibCall:     &CLibCall{FuncName: "unlink", ArgTypes: []LLVMArgType{LLVMStrPtr}, RetType: LLVMI32, RetExt: &i64Type},
	})

	// rename: rename a file
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "rename",
		Params:       []parser.Type{parser.TypeStr, parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Rename a file from old to new",
		CLibCall:     &CLibCall{FuncName: "rename", ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMStrPtr}, RetType: LLVMI32, RetExt: &i64Type},
	})

	// is-file: check if path is a regular file
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "is-file",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Check if the path points to a regular file",
		CLibCall:     &CLibCall{FuncName: "stat", ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI8Ptr}, RetType: LLVMI32, CmpRet: true, FixedArgs: map[int]string{1: "null"}},
	})

	// open-read: open a file for reading
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "open-read",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Open a file for reading, returns file descriptor",
		CLibCall:     &CLibCall{FuncName: "open", ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI32, LLVMI32}, RetType: LLVMI32, RetExt: &i64Type, FixedArgs: map[int]string{1: "0", 2: "0"}},
	})

	// open-write: open a file for writing
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "open-write",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Open a file for writing, returns file descriptor",
		CLibCall:     &CLibCall{FuncName: "open", ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI32, LLVMI32}, RetType: LLVMI32, RetExt: &i64Type, FixedArgs: map[int]string{1: "1537", 2: "420"}},
	})

	// close: close a file descriptor
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "close",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Close a file descriptor",
		CLibCall:     &CLibCall{FuncName: "close", ArgTypes: []LLVMArgType{LLVMI32}, RetType: LLVMI32, RetExt: &i64Type},
	})

	// read: read from a file descriptor (uses @.os-buf)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "read",
		Params:       []parser.Type{parser.TypeI64, parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Read n bytes from a file descriptor into buf",
		CLibCall:     &CLibCall{FuncName: "read", ArgTypes: []LLVMArgType{LLVMI32, LLVMI8Ptr, LLVMI64}, RetType: LLVMI64, TruncArgs: map[int]LLVMArgType{0: LLVMI32}, FixedArgGlobals: map[int]string{1: "i8* getelementptr inbounds ([1024 x i8], [1024 x i8]* @.os-buf, i64 0, i64 0)"}},
	})

	// write: write to a file descriptor
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "write",
		Params:       []parser.Type{parser.TypeI64, parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Write n bytes to a file descriptor",
		CLibCall:     &CLibCall{FuncName: "write", ArgTypes: []LLVMArgType{LLVMI32, LLVMI8Ptr, LLVMI64}, RetType: LLVMI64, TruncArgs: map[int]LLVMArgType{0: LLVMI32}, StrDataArg: map[int]bool{1: true}},
	})

	// now: get current Unix timestamp
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "now",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get the current Unix timestamp in seconds",
		CLibCall:     &CLibCall{FuncName: "time", ArgTypes: []LLVMArgType{LLVMI8Ptr}, RetType: LLVMI64, FixedArgs: map[int]string{0: "null"}},
	})

	// sleep: sleep for n seconds
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "sleep",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Sleep for the given number of seconds",
		CLibCall:     &CLibCall{FuncName: "sleep", ArgTypes: []LLVMArgType{LLVMI32}, RetType: LLVMI32, RetExt: &i64Type},
	})

	// args: number of command-line arguments
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "args",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Return the number of command-line arguments (including program name)",
		ForwardFunc:  "args-count",
	})

	// arg: get i-th command-line argument
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "arg",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Return the i-th command-line argument",
		ForwardFunc:  "args-get",
	})

	// is-dir: check if path is a directory
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "is-dir",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Check if the path points to a directory",
		ForwardFunc:  "stat-dir",
	})

	// stat-size: get file size via stat
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "stat-size",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Doc:          "Get the size of a file (returns size, ok)",
		ForwardFunc:  "stat-size",
	})

	// file-size: get file size (same as stat-size)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "file-size",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Doc:          "Get the size of a file (returns size, ok)",
		ForwardFunc:  "stat-size",
	})

	// get-line: read a line from stdin
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "get-line",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Doc:          "Read a line from standard input",
		ForwardFunc:  "read-stdin-line",
	})
}
