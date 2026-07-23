package builtin

import (
	"runtime"

	"github.com/lizongying/nolang/parser"
)

func init() {
	i64Type := LLVMI64

	// get-env: get environment variable
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "get-env",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Get the value of an environment variable",
		CLibCall:     &CLibCall{FuncName: "getenv", ArgTypes: []LLVMArgType{LLVMStrPtr}, RetType: LLVMI8Ptr, RetCStrToStr: true},
	})

	// set-env: set environment variable
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "set-env",
		Params:       []parser.Type{parser.TypeStr, parser.TypeStr},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Set the value of an environment variable",
		CLibCall:     &CLibCall{FuncName: "setenv", ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMStrPtr, LLVMI32}, RetType: LLVMI32, RetExt: &i64Type, FixedArgs: map[int]string{2: "1"}},
	})

	// get-wd: get current working directory (uses @.os-buf)
	getcwdFn := "getcwd"
	if runtime.GOOS == "windows" {
		getcwdFn = "_getcwd"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "get-wd",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Get the current working directory",
		CLibCall:     &CLibCall{FuncName: getcwdFn, ArgTypes: []LLVMArgType{LLVMI8Ptr, LLVMI64}, RetType: LLVMI8Ptr, RetBuf: true, BufGlobal: "@.os-buf", FixedArgs: map[int]string{1: "1024"}},
	})

	// ch-dir: change current working directory
	chdirFn := "chdir"
	if runtime.GOOS == "windows" {
		chdirFn = "_chdir"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "ch-dir",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Change the current working directory",
		CLibCall:     &CLibCall{FuncName: chdirFn, ArgTypes: []LLVMArgType{LLVMStrPtr}, RetType: LLVMI32, RetExt: &i64Type},
	})

	// exit: exit the process with status code
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "exit",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Exit the process with the given status code",
		CLibCall:     &CLibCall{FuncName: "exit", ArgTypes: []LLVMArgType{LLVMI32}, RetType: LLVMI32, TruncArgs: map[int]LLVMArgType{0: LLVMI32}},
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

	// get-arch: get current CPU architecture (compile-time constant)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "get-arch",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Get the current CPU architecture (e.g. arm64, amd64)",
		ForwardFunc:  "get-arch",
	})

	// mkdir: create a directory
	mkdirFn := "mkdir"
	if runtime.GOOS == "windows" {
		mkdirFn = "_mkdir"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "mkdir",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Create a directory with the given mode",
		CLibCall:     &CLibCall{FuncName: mkdirFn, ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI32}, RetType: LLVMI32, CmpRet: true, TruncArgs: map[int]LLVMArgType{1: LLVMI32}},
	})

	// ch-mod: change file permissions
	chmodFn := "chmod"
	if runtime.GOOS == "windows" {
		chmodFn = "_chmod"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "ch-mod",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Change file permissions with the given mode",
		CLibCall:     &CLibCall{FuncName: chmodFn, ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI32}, RetType: LLVMI32, CmpRet: true, TruncArgs: map[int]LLVMArgType{1: LLVMI32}},
	})

	// remove: remove a file
	unlinkFn := "unlink"
	if runtime.GOOS == "windows" {
		unlinkFn = "_unlink"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "remove",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Remove (unlink) a file",
		CLibCall:     &CLibCall{FuncName: unlinkFn, ArgTypes: []LLVMArgType{LLVMStrPtr}, RetType: LLVMI32, CmpRet: true},
	})

	// rename: rename a file
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "rename",
		Params:       []parser.Type{parser.TypeStr, parser.TypeStr},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Rename a file from old to new",
		CLibCall:     &CLibCall{FuncName: "rename", ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMStrPtr}, RetType: LLVMI32, CmpRet: true},
	})

	// is-file: check if path is a regular file
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "is-file",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Check if the path points to a regular file",
		ForwardFunc:  "stat-file",
	})

	// open-read: open a file for reading
	openFn := "open"
	if runtime.GOOS == "windows" {
		openFn = "_open"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "open-read",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Open a file for reading, returns file descriptor",
		CLibCall:     &CLibCall{FuncName: openFn, ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI32, LLVMI32}, RetType: LLVMI32, RetExt: &i64Type, FixedArgs: map[int]string{1: "0", 2: "0"}},
	})

	// open-write: open a file for writing
	openWriteFlagVal := "1537" // macOS default: O_WRONLY|O_CREAT|O_TRUNC
	if runtime.GOOS == "linux" {
		openWriteFlagVal = "577"
	} else if runtime.GOOS == "windows" {
		// Windows _O_WRONLY(1) | _O_CREAT(256) | _O_TRUNC(512) = 769
		openWriteFlagVal = "769"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "open-write",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Open a file for writing, returns file descriptor",
		CLibCall:     &CLibCall{FuncName: openFn, ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI32, LLVMI32}, RetType: LLVMI32, RetExt: &i64Type, FixedArgs: map[int]string{1: openWriteFlagVal, 2: "420"}},
	})

	// open-file: open a file with custom flags and mode
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "open-file",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Open a file with given flags and mode, returns file descriptor",
		CLibCall:     &CLibCall{FuncName: openFn, ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI32, LLVMI32}, RetType: LLVMI32, RetExt: &i64Type, TruncArgs: map[int]LLVMArgType{1: LLVMI32, 2: LLVMI32}},
	})

	// close: close a file descriptor
	closeFn := "close"
	if runtime.GOOS == "windows" {
		closeFn = "_close"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "close",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Close a file descriptor",
		CLibCall:     &CLibCall{FuncName: closeFn, ArgTypes: []LLVMArgType{LLVMI32}, RetType: LLVMI32, RetExt: &i64Type, TruncArgs: map[int]LLVMArgType{0: LLVMI32}},
	})

	// read: read from a file descriptor (uses @.os-buf)
	readFn := "read"
	if runtime.GOOS == "windows" {
		readFn = "_read"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "read",
		Params:       []parser.Type{parser.TypeI64, parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Read n bytes from a file descriptor into buf",
		CLibCall:     &CLibCall{FuncName: readFn, ArgTypes: []LLVMArgType{LLVMI32, LLVMI8Ptr, LLVMI64}, RetType: LLVMI64, TruncArgs: map[int]LLVMArgType{0: LLVMI32}, FixedArgGlobals: map[int]string{1: "i8* getelementptr inbounds ([1024 x i8], [1024 x i8]* @.os-buf, i64 0, i64 0)"}},
	})

	// write: write to a file descriptor
	writeFn := "write"
	if runtime.GOOS == "windows" {
		writeFn = "_write"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "write",
		Params:       []parser.Type{parser.TypeI64, parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Write n bytes to a file descriptor",
		CLibCall:     &CLibCall{FuncName: writeFn, ArgTypes: []LLVMArgType{LLVMI32, LLVMI8Ptr, LLVMI64}, RetType: LLVMI64, TruncArgs: map[int]LLVMArgType{0: LLVMI32}, StrDataArg: map[int]bool{1: true}},
	})

	// now: get current Unix timestamp (uses internal @nolang.now_s, replaces libc @time)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "now",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get the current Unix timestamp in seconds",
		CLibCall:     &CLibCall{FuncName: "nolang.now_s", ArgTypes: []LLVMArgType{}, RetType: LLVMI64},
	})

	// sleep: sleep for n seconds (uses internal @nolang.sleep_s, replaces libc @sleep)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "sleep",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Sleep for the given number of seconds",
		CLibCall:     &CLibCall{FuncName: "nolang.sleep_s", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI64},
	})

	// now-ms: current Unix timestamp in milliseconds
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "now-ms",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get the current Unix timestamp in milliseconds",
		CLibCall:     &CLibCall{FuncName: "nolang.now_ms", ArgTypes: []LLVMArgType{}, RetType: LLVMI64},
	})

	// now-us: current Unix timestamp in microseconds
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "now-us",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get the current Unix timestamp in microseconds",
		CLibCall:     &CLibCall{FuncName: "nolang.now_us", ArgTypes: []LLVMArgType{}, RetType: LLVMI64},
	})

	// now-ns: current timestamp in nanoseconds
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "now-ns",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get the current timestamp in nanoseconds",
		CLibCall:     &CLibCall{FuncName: "nolang.now_ns", ArgTypes: []LLVMArgType{}, RetType: LLVMI64},
	})

	// sleep-us: sleep for n microseconds
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "sleep-us",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Sleep for the given number of microseconds",
		CLibCall:     &CLibCall{FuncName: "nolang.sleep_us", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// sleep-ns: sleep for n nanoseconds
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "sleep-ns",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Sleep for the given number of nanoseconds",
		CLibCall:     &CLibCall{FuncName: "nolang.sleep_ns", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// strerror: convert errno to error message string
	// Implemented in Nolang (src/std/os.no) with a POSIX errno lookup table.

	// get-errno: get the last errno value
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "get-errno",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get the last errno value from C library",
		ForwardFunc:  "get-errno",
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

	// chown: change file owner and group
	chownFn := "chown"
	if runtime.GOOS == "windows" {
		chownFn = "nolang.win_chown"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "chown",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Change file owner and group (uid, gid)",
		CLibCall:     &CLibCall{FuncName: chownFn, ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI32, LLVMI32}, RetType: LLVMI32, CmpRet: true, TruncArgs: map[int]LLVMArgType{1: LLVMI32, 2: LLVMI32}},
	})

	// getuid: get current user ID
	getuidFn := "getuid"
	if runtime.GOOS == "windows" {
		getuidFn = "nolang.win_getuid"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "getuid",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get the current user ID",
		CLibCall:     &CLibCall{FuncName: getuidFn, ArgTypes: []LLVMArgType{}, RetType: LLVMI32, RetExt: &i64Type},
	})

	// getgid: get current group ID
	getgidFn := "getgid"
	if runtime.GOOS == "windows" {
		getgidFn = "nolang.win_getgid"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "getgid",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get the current group ID",
		CLibCall:     &CLibCall{FuncName: getgidFn, ArgTypes: []LLVMArgType{}, RetType: LLVMI32, RetExt: &i64Type},
	})

	// is-dir: check if path is a directory
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "is-dir",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Check if the path points to a directory",
		ForwardFunc:  "stat-dir",
	})

	// stat-size: get file size via stat
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "stat-size",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64, parser.TypeBool}, // size, ok
		Doc:          "Get the size of a file (returns size, ok)",
		ForwardFunc:  "stat-size",
	})

	// file-size: get file size (same as stat-size)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "file-size",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64, parser.TypeBool},
		Doc:          "Get the size of a file (returns size, ok)",
		ForwardFunc:  "stat-size",
	})

	// stat-mode: get file mode (st_mode) via stat
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "stat-mode",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64, parser.TypeBool},
		Doc:          "Get file mode (st_mode) including permission and type bits (returns mode, ok)",
		ForwardFunc:  "stat-mode",
	})

	// stat-uid: get file owner uid via stat
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "stat-uid",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64, parser.TypeBool},
		Doc:          "Get file owner uid (returns uid, ok)",
		ForwardFunc:  "stat-uid",
	})

	// stat-gid: get file group gid via stat
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "stat-gid",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64, parser.TypeBool},
		Doc:          "Get file group gid (returns gid, ok)",
		ForwardFunc:  "stat-gid",
	})

	// stat-mtime: get file modification time via stat
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "stat-mtime",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64, parser.TypeBool},
		Doc:          "Get file modification time in Unix seconds (returns mtime, ok)",
		ForwardFunc:  "stat-mtime",
	})

	// read-file: read entire file into a string
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "read-file",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Read entire file contents into a string (empty on error)",
		ForwardFunc:  "read-file",
	})

	// write-file: write []byte data to a file (overwrite)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "write-file",
		Params:       []parser.Type{parser.TypeStr, &parser.SliceType{Elem: parser.TypeByte}},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Write []byte data to a file (overwrite). Returns true on success",
		ForwardFunc:  "write-file",
	})

	// get-line: read a line from stdin
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "get-line",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr, parser.TypeBool},
		Doc:          "Read a line from standard input",
		ForwardFunc:  "read-stdin-line",
	})

	// open-dir: open a directory for reading entries
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "open-dir",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Open a directory for reading entries, returns directory handle (0 on failure)",
		ForwardFunc:  "open-dir",
	})

	// read-dir: read next directory entry name
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "read-dir",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeStr, parser.TypeBool},
		Doc:          "Read next directory entry name, returns (name, ok)",
		ForwardFunc:  "read-dir",
	})

	// close-dir: close a directory handle
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "close-dir",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Close a directory handle",
		ForwardFunc:  "close-dir",
	})

	// win-find-first-file: Windows directory search (FindFirstFileA)
	// Args: path str (caller appends "\\*" wildcard)
	// Returns: bufPtr i64 (0 on failure — INVALID_HANDLE_VALUE)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "win-find-first-file",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Open a directory search on Windows (FindFirstFileA). Returns bufPtr (0 on failure)",
		ForwardFunc:  "win-find-first-file",
	})

	// win-find-next-file: read next Windows directory entry (FindNextFileA)
	// Args: bufPtr i64 (from win-find-first-file)
	// Returns: name str, ok bool (ok=false when no more entries)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "win-find-next-file",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeStr, parser.TypeBool},
		Doc:          "Read next Windows directory entry name. Returns (name, ok)",
		ForwardFunc:  "win-find-next-file",
	})

	// win-find-close: close a Windows directory search (FindClose + free)
	// Args: bufPtr i64 (from win-find-first-file)
	// Returns: ok bool (true on success)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "win-find-close",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Close a Windows directory search handle",
		ForwardFunc:  "win-find-close",
	})

	// touch-file: update file timestamps to current time
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "touch-file",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Update file access and modification times to current time",
		ForwardFunc:  "touch-file",
	})
}
