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
		CLibCall:     &CLibCall{FuncName: chdirFn, ArgTypes: []LLVMArgType{LLVMStrPtr}, RetType: LLVMI32, CmpRet: true},
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

	// symlink: create a symbolic link
	symlinkFn := "symlink"
	if runtime.GOOS == "windows" {
		symlinkFn = "nolang.win_symlink"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "symlink",
		Params:       []parser.Type{parser.TypeStr, parser.TypeStr},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Create a symbolic link (target, linkpath). Returns true on success",
		CLibCall:     &CLibCall{FuncName: symlinkFn, ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMStrPtr}, RetType: LLVMI32, CmpRet: true},
	})

	// link: create a hard link
	linkFn := "link"
	if runtime.GOOS == "windows" {
		linkFn = "nolang.win_link"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "link",
		Params:       []parser.Type{parser.TypeStr, parser.TypeStr},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Create a hard link (oldpath, newpath). Returns true on success",
		CLibCall:     &CLibCall{FuncName: linkFn, ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMStrPtr}, RetType: LLVMI32, CmpRet: true},
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

	// exists: check if path exists (follows symlinks)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "exists",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Check if the path exists (follows symlinks). Returns true if stat succeeds",
		ForwardFunc:  "stat-exists",
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
		Return:       []parser.Type{parser.TypeFd},
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
		Return:       []parser.Type{parser.TypeFd},
		Doc:          "Open a file for writing, returns file descriptor",
		CLibCall:     &CLibCall{FuncName: openFn, ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI32, LLVMI32}, RetType: LLVMI32, RetExt: &i64Type, FixedArgs: map[int]string{1: openWriteFlagVal, 2: "420"}},
	})

	// open-file: open a file with custom flags and mode
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "open-file",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeFd},
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
		Params:       []parser.Type{parser.TypeFd},
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
		Params:       []parser.Type{parser.TypeFd, parser.TypeStr, parser.TypeI64},
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
		Params:       []parser.Type{parser.TypeFd, parser.TypeStr, parser.TypeI64},
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

	// ═══════════════════════════════════════════════
	// 擴展 fs / os 底層能力（notools Unix 工具集支援）
	// ═══════════════════════════════════════════════

	// realpath: resolve absolute path (POSIX realpath(3))
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "realpath",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Resolve path to an absolute canonical path (returns empty string on failure)",
		ForwardFunc:  "realpath",
	})

	// readlink: read symbolic link target (POSIX readlink(2))
	// Returns (target str, ok bool)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "readlink",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeStr, parser.TypeBool},
		Doc:          "Read target of a symbolic link. Returns (target, ok)",
		ForwardFunc:  "readlink",
	})

	// mkstemp: create and open a unique temporary file (POSIX mkstemp(3))
	// Returns (name, fd). Empty name and fd=-1 on failure.
	// Return order is (name, fd) — not (fd, name) — because lastBuiltinExtra is
	// hardcoded to i64 in the multi-assignment path; str must be the primary return.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "mkstemp",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeStr, parser.TypeFd},
		Doc:          "Create and open a unique temp file from template (ending with XXXXXX). Returns (name, fd)",
		ForwardFunc:  "mkstemp",
	})

	// mkdtemp: create a unique temporary directory (POSIX mkdtemp(3))
	// Returns (name, ok). Empty name and ok=false on failure.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "mkdtemp",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeStr, parser.TypeBool},
		Doc:          "Create a unique temp directory from template (ending with XXXXXX). Returns (name, ok)",
		ForwardFunc:  "mkdtemp",
	})

	// mkfifo: create a named pipe (POSIX mkfifo(3))
	// Returns ok bool.
	mkfifoFn := "mkfifo"
	if runtime.GOOS == "windows" {
		mkfifoFn = "nolang.win_mkfifo"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "mkfifo",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Create a named pipe (FIFO). Returns true on success",
		CLibCall:     &CLibCall{FuncName: mkfifoFn, ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI32}, RetType: LLVMI32, CmpRet: true, TruncArgs: map[int]LLVMArgType{1: LLVMI32}},
	})

	// utime: set file access and modification times (POSIX utimes(2))
	// Returns ok bool.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "utime",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Set file access and modification times (Unix seconds). Returns true on success",
		ForwardFunc:  "utime",
	})

	// rmdir: remove empty directory (POSIX rmdir(2))
	// Returns ok bool.
	rmdirFn := "rmdir"
	if runtime.GOOS == "windows" {
		rmdirFn = "_rmdir"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "rmdir",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Remove an empty directory. Returns true on success",
		CLibCall:     &CLibCall{FuncName: rmdirFn, ArgTypes: []LLVMArgType{LLVMStrPtr}, RetType: LLVMI32, CmpRet: true},
	})

	// mknod: create special file (POSIX mknod(2))
	// Returns ok bool. Not supported on Windows.
	mknodFn := "mknod"
	if runtime.GOOS == "windows" {
		mknodFn = "nolang.win_mknod"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "mknod",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Create a special file (FIFO, device node). Returns true on success",
		CLibCall:     &CLibCall{FuncName: mknodFn, ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI32, LLVMI64}, RetType: LLVMI32, CmpRet: true, TruncArgs: map[int]LLVMArgType{1: LLVMI32}},
	})

	// truncate: truncate/extend file to specified length (POSIX truncate(2))
	// Returns ok bool.
	truncateFn := "truncate"
	if runtime.GOOS == "windows" {
		truncateFn = "nolang.win_truncate"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "truncate",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Truncate or extend a file to the given length. Returns true on success",
		CLibCall:     &CLibCall{FuncName: truncateFn, ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI64}, RetType: LLVMI32, CmpRet: true},
	})

	// sync: flush filesystem buffers to disk (POSIX sync(2))
	// No return value.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "sync",
		Params:       []parser.Type{},
		Return:       []parser.Type{},
		Doc:          "Flush filesystem buffers to disk",
		ForwardFunc:  "sync",
	})

	// lstat: get link itself info without following (POSIX lstat(2))
	// Returns ok bool. Distinguishes from stat by not following symlinks.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "lstat",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Get info about the symlink itself (does not follow). Returns true on success",
		ForwardFunc:  "lstat",
	})

	// geteuid: get effective user ID (POSIX geteuid(2))
	geteuidFn := "geteuid"
	if runtime.GOOS == "windows" {
		geteuidFn = "nolang.win_getuid" // Windows: reuse getuid stub (returns 0)
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "geteuid",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get the effective user ID",
		CLibCall:     &CLibCall{FuncName: geteuidFn, ArgTypes: []LLVMArgType{}, RetType: LLVMI32, RetExt: &i64Type},
	})

	// getegid: get effective group ID (POSIX getegid(2))
	getegidFn := "getegid"
	if runtime.GOOS == "windows" {
		getegidFn = "nolang.win_getgid" // Windows: reuse getgid stub (returns 0)
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "getegid",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get the effective group ID",
		CLibCall:     &CLibCall{FuncName: getegidFn, ArgTypes: []LLVMArgType{}, RetType: LLVMI32, RetExt: &i64Type},
	})

	// getgroups: get supplementary group IDs (POSIX getgroups(2))
	// Returns (gids []i64, n i64)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "getgroups",
		Params:       []parser.Type{},
		Return:       []parser.Type{&parser.SliceType{Elem: parser.TypeI64}, parser.TypeI64},
		Doc:          "Get supplementary group IDs. Returns (gids, count)",
		ForwardFunc:  "getgroups",
	})

	// sysconf: query system configuration limit (POSIX sysconf(3))
	// Returns i64 value (-1 on error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "sysconf",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Query system configuration value (POSIX _SC_* constant). Returns -1 on error",
		CLibCall:     &CLibCall{FuncName: "sysconf", ArgTypes: []LLVMArgType{LLVMI32}, RetType: LLVMI64, TruncArgs: map[int]LLVMArgType{0: LLVMI32}},
	})

	// num-cpu: get number of online CPUs (wraps sysconf(_SC_NPROCESSORS_ONLN))
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "num-cpu",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get the number of online CPUs",
		ForwardFunc:  "num-cpu",
	})

	// uname: get kernel/architecture info (POSIX uname(2))
	// Returns utsname struct with sysname/nodename/release/version/machine fields.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "uname",
		Params:       []parser.Type{},
		Return:       []parser.Type{&parser.NamedType{Value: "utsname"}},
		Doc:          "Get system information (sysname/nodename/release/version/machine)",
		ForwardFunc:  "uname",
	})

	// signal: set signal handler (simplified: SIG_DFL=0, SIG_IGN=1)
	// Returns previous handler value (i64)
	signalFn := "signal"
	if runtime.GOOS == "windows" {
		signalFn = "nolang.win_signal"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "signal",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Set signal handler (0=SIG_DFL, 1=SIG_IGN). Returns previous handler value",
		CLibCall:     &CLibCall{FuncName: signalFn, ArgTypes: []LLVMArgType{LLVMI32, LLVMI64}, RetType: LLVMI64, TruncArgs: map[int]LLVMArgType{0: LLVMI32}},
	})

	// ttyname: get terminal name (POSIX ttyname(3))
	// Returns name str (empty on failure or if fd is not a tty)
	ttynameFn := "ttyname"
	if runtime.GOOS == "windows" {
		ttynameFn = "nolang.win_ttyname"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "ttyname",
		Params:       []parser.Type{parser.TypeFd},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Get terminal name for a file descriptor. Returns empty string if not a tty",
		ForwardFunc:  ttynameFn,
	})

	// ═══════════════════════════════════════════════
	// 擴展 os 系統資訊能力（notools Unix 工具集支援）
	// ═══════════════════════════════════════════════

	// get-login: get login name (POSIX getlogin(3))
	// Returns name str (empty on failure)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "get-login",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Get the login name of the current user. Returns empty string on failure",
		ForwardFunc:  "getlogin",
	})

	// get-host-id: get host identifier (POSIX gethostid(3))
	// Returns i64 id.
	gethostidFn := "gethostid"
	if runtime.GOOS == "windows" {
		gethostidFn = "nolang.win_gethostid"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "get-host-id",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get the host identifier (32-bit integer)",
		CLibCall:     &CLibCall{FuncName: gethostidFn, ArgTypes: []LLVMArgType{}, RetType: LLVMI32, RetExt: &i64Type},
	})

	// set-priority: set process priority (POSIX setpriority(2))
	// Returns ok bool.
	setpriorityFn := "setpriority"
	if runtime.GOOS == "windows" {
		setpriorityFn = "nolang.win_setpriority"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "set-priority",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Set process priority (which, who, prio). Returns true on success",
		CLibCall:     &CLibCall{FuncName: setpriorityFn, ArgTypes: []LLVMArgType{LLVMI32, LLVMI32, LLVMI32}, RetType: LLVMI32, CmpRet: true, TruncArgs: map[int]LLVMArgType{0: LLVMI32, 1: LLVMI32, 2: LLVMI32}},
	})

	// get-priority: get process priority (POSIX getpriority(2))
	// Returns (prio i64, ok bool). Uses errno to distinguish error from -1.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "get-priority",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64, parser.TypeBool},
		Doc:          "Get process priority (which, who). Returns (prio, ok)",
		ForwardFunc:  "get-priority",
	})

	// set-sid: create new session (POSIX setsid(2))
	// Returns pid i64 (>0 on success, -1 on failure).
	setsidFn := "setsid"
	if runtime.GOOS == "windows" {
		setsidFn = "nolang.win_setsid"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "set-sid",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Create a new session and detach from controlling terminal. Returns new pgid or -1",
		CLibCall:     &CLibCall{FuncName: setsidFn, ArgTypes: []LLVMArgType{}, RetType: LLVMI32, RetExt: &i64Type},
	})

	// flock: apply advisory lock on fd (POSIX flock(2))
	// Returns ok bool.
	flockFn := "flock"
	if runtime.GOOS == "windows" {
		flockFn = "nolang.win_flock"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "flock",
		Params:       []parser.Type{parser.TypeFd, parser.TypeI64},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Apply or release an advisory lock on a file descriptor. Returns true on success",
		CLibCall:     &CLibCall{FuncName: flockFn, ArgTypes: []LLVMArgType{LLVMI32, LLVMI32}, RetType: LLVMI32, CmpRet: true, TruncArgs: map[int]LLVMArgType{0: LLVMI32, 1: LLVMI32}},
	})

	// sysctl: query system control parameter (reads string value)
	// Returns (val str, ok bool). Platform-specific implementation.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "sysctl",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeStr, parser.TypeBool},
		Doc:          "Query a system control parameter by name. Returns (value, ok)",
		ForwardFunc:  "sysctl",
	})

	// get-domain-name: get NIS domain name (POSIX getdomainname(2))
	// Returns name str (empty on failure).
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "get-domain-name",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Get the NIS domain name. Returns empty string on failure",
		ForwardFunc:  "getdomainname",
	})

	// syslog: write a system log entry (POSIX syslog(3))
	// No return value.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "syslog",
		Params:       []parser.Type{parser.TypeI64, parser.TypeStr},
		Return:       []parser.Type{},
		Doc:          "Write a message to the system logger with the given priority",
		ForwardFunc:  "syslog",
	})

	// chroot: change root directory (POSIX chroot(2))
	// Returns ok bool. Not supported on Windows.
	chrootFn := "chroot"
	if runtime.GOOS == "windows" {
		chrootFn = "nolang.win_chroot"
	}
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "chroot",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Change root directory to the given path. Returns true on success",
		CLibCall:     &CLibCall{FuncName: chrootFn, ArgTypes: []LLVMArgType{LLVMStrPtr}, RetType: LLVMI32, CmpRet: true},
	})
}
