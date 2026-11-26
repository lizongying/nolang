package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	byteSlice := &parser.SliceType{Elem: parser.TypeByte}
	optionFd := &parser.NullableType{Type: parser.TypeFd}
	optionBool := &parser.NullableType{Type: parser.TypeBool}
	optionI64 := &parser.NullableType{Type: parser.TypeI64}

	BuiltinMethodList = append(BuiltinMethodList,
		BuiltinMethod{
			ReceiverType: ReceiverGlobal,
			MethodName:   "raw-open",
			Params:       []parser.Type{parser.TypeStr, parser.TypeI64, parser.TypeI64},
			Return:       []parser.Type{optionFd},
			Doc:          "Open a file with portable raw VFS flags (?fs.fd)",
			ForwardFunc:  "raw-open",
		},
		BuiltinMethod{
			ReceiverType: ReceiverGlobal,
			MethodName:   "raw-close",
			Params:       []parser.Type{parser.TypeFd},
			Return:       []parser.Type{optionBool},
			Doc:          "Close a raw VFS descriptor (?bool)",
			ForwardFunc:  "raw-close",
		},
		BuiltinMethod{
			ReceiverType: ReceiverGlobal,
			MethodName:   "raw-seek",
			Params:       []parser.Type{parser.TypeFd, parser.TypeI64, parser.TypeI64},
			Return:       []parser.Type{optionI64},
			Doc:          "Set or query the current raw file position (?i64)",
			ForwardFunc:  "raw-seek",
		},
		BuiltinMethod{
			ReceiverType: ReceiverGlobal,
			MethodName:   "raw-pread",
			Params:       []parser.Type{parser.TypeFd, byteSlice, parser.TypeI64, parser.TypeI64},
			Return:       []parser.Type{optionI64},
			Doc:          "Positional raw read that preserves the current file position (?i64)",
			ForwardFunc:  "raw-pread",
		},
		BuiltinMethod{
			ReceiverType: ReceiverGlobal,
			MethodName:   "raw-pwrite",
			Params:       []parser.Type{parser.TypeFd, byteSlice, parser.TypeI64, parser.TypeI64},
			Return:       []parser.Type{optionI64},
			Doc:          "Positional raw write that preserves the current file position (?i64)",
			ForwardFunc:  "raw-pwrite",
		},
		BuiltinMethod{
			ReceiverType: ReceiverGlobal,
			MethodName:   "raw-ftruncate",
			Params:       []parser.Type{parser.TypeFd, parser.TypeI64},
			Return:       []parser.Type{optionBool},
			Doc:          "Truncate or extend a raw file (?bool)",
			ForwardFunc:  "raw-ftruncate",
		},
		BuiltinMethod{
			ReceiverType: ReceiverGlobal,
			MethodName:   "raw-fstat-size",
			Params:       []parser.Type{parser.TypeFd},
			Return:       []parser.Type{optionI64},
			Doc:          "Return the current raw file size (?i64)",
			ForwardFunc:  "raw-fstat-size",
		},
		BuiltinMethod{
			ReceiverType: ReceiverGlobal,
			MethodName:   "raw-fsync",
			Params:       []parser.Type{parser.TypeFd},
			Return:       []parser.Type{optionBool},
			Doc:          "Durably flush a raw file (?bool)",
			ForwardFunc:  "raw-fsync",
		},
		BuiltinMethod{
			ReceiverType: ReceiverGlobal,
			MethodName:   "raw-fdatasync",
			Params:       []parser.Type{parser.TypeFd},
			Return:       []parser.Type{optionBool},
			Doc:          "Durably flush raw file data (?bool)",
			ForwardFunc:  "raw-fdatasync",
		},
		BuiltinMethod{
			ReceiverType: ReceiverGlobal,
			MethodName:   "raw-sync-dir",
			Params:       []parser.Type{parser.TypeStr},
			Return:       []parser.Type{optionBool},
			Doc:          "Durably flush directory entries when supported (?bool)",
			ForwardFunc:  "raw-sync-dir",
		},
		BuiltinMethod{
			ReceiverType: ReceiverGlobal,
			MethodName:   "raw-lock-range",
			Params:       []parser.Type{parser.TypeFd, parser.TypeI64, parser.TypeI64, parser.TypeI64, parser.TypeBool},
			Return:       []parser.Type{optionBool},
			Doc:          "Acquire a raw byte-range lock (?bool)",
			ForwardFunc:  "raw-lock-range",
		},
		BuiltinMethod{
			ReceiverType: ReceiverGlobal,
			MethodName:   "raw-unlock-range",
			Params:       []parser.Type{parser.TypeFd, parser.TypeI64, parser.TypeI64},
			Return:       []parser.Type{optionBool},
			Doc:          "Release a raw byte-range lock (?bool)",
			ForwardFunc:  "raw-unlock-range",
		},
		BuiltinMethod{
			ReceiverType: ReceiverGlobal,
			MethodName:   "raw-capabilities",
			Params:       []parser.Type{},
			Return:       []parser.Type{parser.TypeI64},
			Doc:          "Return the raw VFS capability bitmask for the target",
			ForwardFunc:  "raw-capabilities",
		},
	)
}
