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

	// process-run-capture: legacy subprocess runner (now a stub).
	// Previously delegated to @nolang_process_run in process.c C runtime.
	// Now replaced by pure Nolang implementations of cmd in process.no
	// (POSIX: process-fork/pipe/dup2/exec-shell/waitpid; Windows: Win32 API builtins).
	// This builtin is kept as a stub returning ("", -1) for backward compatibility.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "process-run-capture",
		Params: []parser.Type{
			&parser.SliceType{Elem: parser.TypeStr},
			&parser.SliceType{Elem: parser.TypeStr},
			parser.TypeStr,
			parser.TypeStr,
			parser.TypeI64,
			parser.TypeI64,
		},
		Return:      []parser.Type{parser.TypeStr, parser.TypeI64},
		Doc:         "Run a subprocess and capture its output. Returns (out, status)",
		ForwardFunc: "process-run-capture",
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

	// ═══════════════════════════════════════════════
	// Win32 process/pipe API — ForwardFunc builtins
	// These replace the C runtime (process.c) on Windows,
	// allowing pure Nolang implementation of process.cmd on Windows.
	// ═══════════════════════════════════════════════

	// win-create-pipe: create an anonymous pipe (CreatePipe)
	// Returns packed i64: (read_handle << 32) | write_handle, or 0 on failure
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "win-create-pipe",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Create an anonymous pipe (CreatePipe). Returns packed (read_handle << 32) | write_handle, 0 on failure",
		ForwardFunc:  "win-create-pipe",
	})

	// win-close-handle: close a Windows handle (CloseHandle)
	// Returns ok bool
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "win-close-handle",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Close a Windows handle (CloseHandle). Returns true on success",
		ForwardFunc:  "win-close-handle",
	})

	// win-write-pipe: write data to a pipe/handle (WriteFile)
	// Args: handle i64, data str (writes all bytes)
	// Returns written i64 (bytes written, -1 on error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "win-write-pipe",
		Params:       []parser.Type{parser.TypeI64, parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Write data to a pipe/handle (WriteFile). Returns bytes written, -1 on error",
		ForwardFunc:  "win-write-pipe",
	})

	// win-read-pipe: read data from a pipe/handle (ReadFile)
	// Args: handle i64, max_bytes i64
	// Returns (data str, n i64): n = bytes read, -1 on error
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "win-read-pipe",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Doc:          "Read data from a pipe/handle (ReadFile). Returns (data, bytes_read), bytes_read=-1 on error",
		ForwardFunc:  "win-read-pipe",
	})

	// win-create-process: spawn a child process (CreateProcessA)
	// Args: cmdline str, dir str, stdin_handle i64, stdout_handle i64, stderr_handle i64
	//   (pass 0 to inherit parent's corresponding std handle)
	// Returns (proc_handle i64, status i64):
	//   proc_handle > 0 = success (handle to process), status = 0
	//   proc_handle = 0 = failure, status = -1
	// The returned proc_handle must be closed with win-close-handle.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "win-create-process",
		Params:       []parser.Type{parser.TypeStr, parser.TypeStr, parser.TypeI64, parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Doc:          "Spawn a child process (CreateProcessA). Returns (proc_handle, status): handle>0=ok, 0=fail",
		ForwardFunc:  "win-create-process",
	})

	// win-wait-process: wait for a process to exit (WaitForSingleObject)
	// Args: proc_handle i64, timeout_ms i64 (0 = INFINITE)
	// Returns status i64:
	//   0 = still active (WAIT_TIMEOUT, only when timeout > 0)
	//   1 = exited normally
	//  -1 = error
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "win-wait-process",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Wait for a process (WaitForSingleObject). Returns 0=timeout, 1=exited, -1=error",
		ForwardFunc:  "win-wait-process",
	})

	// win-get-exit-code: get process exit code (GetExitCodeProcess)
	// Args: proc_handle i64
	// Returns (exit_code i64, ok bool): ok=false if still running
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "win-get-exit-code",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64, parser.TypeBool},
		Doc:          "Get process exit code (GetExitCodeProcess). Returns (code, ok): ok=false if still running",
		ForwardFunc:  "win-get-exit-code",
	})

	// win-terminate-process: kill a process (TerminateProcess)
	// Args: proc_handle i64, exit_code i64
	// Returns ok bool
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "win-terminate-process",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Terminate a process (TerminateProcess). Returns true on success",
		ForwardFunc:  "win-terminate-process",
	})

	// win-get-std-handle: get a standard handle (GetStdHandle)
	// Args: which i64 (0=stdin, 1=stdout, 2=stderr)
	// Returns handle i64
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "win-get-std-handle",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get standard handle (GetStdHandle). 0=stdin, 1=stdout, 2=stderr",
		ForwardFunc:  "win-get-std-handle",
	})
}
