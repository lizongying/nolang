package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// get-env: get environment variable
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "get-env",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Get the value of an environment variable",
		ForwardFunc:  "getenv",
	})

	// set-env: set environment variable
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "set-env",
		Params:       []parser.Type{parser.TypeStr, parser.TypeStr},
		Return:       []parser.Type{},
		Doc:          "Set the value of an environment variable",
		ForwardFunc:  "setenv",
	})

	// get-wd: get current working directory
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "get-wd",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Get the current working directory",
		ForwardFunc:  "getcwd",
	})

	// ch-dir: change current working directory
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "ch-dir",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{},
		Doc:          "Change the current working directory",
		ForwardFunc:  "chdir",
	})

	// mkdir: create a directory
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "mkdir",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Create a directory with the given mode",
		ForwardFunc:  "mkdir",
	})

	// remove: remove a file
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "remove",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Remove (unlink) a file",
		ForwardFunc:  "unlink",
	})

	// rename: rename a file
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "rename",
		Params:       []parser.Type{parser.TypeStr, parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Rename a file from old to new",
		ForwardFunc:  "rename",
	})

	// is-file: check if path is a regular file
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "is-file",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Check if the path points to a regular file",
		ForwardFunc:  "stat_file",
	})

	// open-read: open a file for reading
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "open-read",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Open a file for reading, returns file descriptor",
		ForwardFunc:  "open_read",
	})

	// open-write: open a file for writing
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "open-write",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Open a file for writing, returns file descriptor",
		ForwardFunc:  "open_write",
	})

	// read: read from a file descriptor
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "read",
		Params:       []parser.Type{parser.TypeI64, parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Read n bytes from a file descriptor into buf",
		ForwardFunc:  "read",
	})

	// write: write to a file descriptor
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "write",
		Params:       []parser.Type{parser.TypeI64, parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Write n bytes to a file descriptor",
		ForwardFunc:  "write",
	})

	// close: close a file descriptor
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "close",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Close a file descriptor",
		ForwardFunc:  "close",
	})

	// exit: exit the process with status code
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "exit",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Exit the process with the given status code",
		ForwardFunc:  "exit",
	})

	// get-pid: get process ID
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "get-pid",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get the current process ID",
		ForwardFunc:  "getpid",
	})

	// host-name: get the hostname
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "host-name",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Get the system hostname",
		ForwardFunc:  "gethostname",
	})

	// now: get current Unix timestamp
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "now",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get the current Unix timestamp in seconds",
		ForwardFunc:  "time",
	})

	// sleep: sleep for n seconds
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "sleep",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Sleep for the given number of seconds",
		ForwardFunc:  "sleep",
	})

	// args: number of command-line arguments
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "args",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Return the number of command-line arguments (including program name)",
		ForwardFunc:  "args_count",
	})

	// arg: get i-th command-line argument
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "arg",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Return the i-th command-line argument",
		ForwardFunc:  "args_get",
	})

	// is-dir: check if path is a directory
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "is-dir",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Check if the path points to a directory",
		ForwardFunc:  "stat_dir",
	})

	// stat-size: get file size via stat
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "stat-size",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Doc:          "Get the size of a file (returns size, ok)",
		ForwardFunc:  "stat_size",
	})

	// file-size: get file size (same as stat-size)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "file-size",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Doc:          "Get the size of a file (returns size, ok)",
		ForwardFunc:  "stat_size",
	})

	// get-line: read a line from stdin
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "get-line",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Doc:          "Read a line from standard input",
		ForwardFunc:  "read_stdin_line",
	})
}
