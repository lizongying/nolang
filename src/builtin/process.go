package builtin

import (
	"runtime"

	"github.com/lizongying/nolang/parser"
)

func init() {
	i64Type := LLVMI64

	// process-fork: fork current process
	// Returns: child=0, parent=child_pid, -1=error
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "process-fork",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Fork the current process. Returns 0 in child, child PID in parent, -1 on error",
		ForwardFunc:  "process-fork",
	})

	// process-pipe: create a pipe
	// Returns packed i64: (read_fd << 32) | write_fd
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "process-pipe",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Create a pipe. Returns packed (read_fd << 32) | write_fd",
		ForwardFunc:  "process-pipe",
	})

	// process-waitpid: wait for child process
	// Returns exit code (0-255)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "process-waitpid",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Wait for child process. Returns exit code (WEXITSTATUS)",
		ForwardFunc:  "process-waitpid",
	})

	// process-exec: replace current process with new program
	// Calls execlp(program, program, arg, NULL)
	// Returns only on failure (errno)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "process-exec",
		Params:       []parser.Type{parser.TypeStr, parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Replace current process with program. Returns only on failure",
		ForwardFunc:  "process-exec",
	})

	// process-exec-shell: replace current process with sh -c cmd
	// Calls execlp("sh", "sh", "-c", cmd, NULL)
	// Returns only on failure (errno)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "process-exec-shell",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Replace current process with sh -c cmd. Returns only on failure",
		ForwardFunc:  "process-exec-shell",
	})

	// process-kill: send signal to process
	killFn := "kill"
	if runtime.GOOS == "windows" {
		killFn = "nolang.win_kill"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "process-kill",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Send signal to process. Returns true on success",
		CLibCall:     &CLibCall{FuncName: killFn, ArgTypes: []LLVMArgType{LLVMI32, LLVMI32}, RetType: LLVMI32, RetExt: &i64Type, TruncArgs: map[int]LLVMArgType{0: LLVMI32, 1: LLVMI32}},
	})

	// process-dup2: duplicate file descriptor
	dup2Fn := "dup2"
	if runtime.GOOS == "windows" {
		dup2Fn = "_dup2"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "process-dup2",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Duplicate file descriptor oldfd to newfd. Returns newfd on success, -1 on error",
		CLibCall:     &CLibCall{FuncName: dup2Fn, ArgTypes: []LLVMArgType{LLVMI32, LLVMI32}, RetType: LLVMI32, RetExt: &i64Type, TruncArgs: map[int]LLVMArgType{0: LLVMI32, 1: LLVMI32}},
	})

	// process-getppid: get parent process ID
	getppidFn := "getppid"
	if runtime.GOOS == "windows" {
		getppidFn = "nolang.win_getppid"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "process-getppid",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get parent process ID",
		CLibCall:     &CLibCall{FuncName: getppidFn, ArgTypes: []LLVMArgType{}, RetType: LLVMI32, RetExt: &i64Type},
	})

	// process-system: execute shell command
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "process-system",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Execute shell command via system(). Returns exit status",
		CLibCall:     &CLibCall{FuncName: "system", ArgTypes: []LLVMArgType{LLVMStrPtr}, RetType: LLVMI32, RetExt: &i64Type},
	})
}
