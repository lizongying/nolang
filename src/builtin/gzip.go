package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// gzip-compress: compress data using zlib compress2
	// params: data []byte, n i64
	// return: out []byte, out-n i64
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "gzip-compress",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Doc:          "Compress data using zlib compress2, returns compressed data and its length",
		CLibCall: &CLibCall{
			FuncName:    "compress2",
			ArgTypes:    []LLVMArgType{LLVMI8Ptr, LLVMI64Ptr, LLVMI8Ptr, LLVMI64, LLVMI32},
			RetType:     LLVMI32,
			RetBuf:      true,
			BufGlobal:   "@.gzip-buf",
			FixedArgs:   map[int]string{4: "9"},
			StrDataArg:  map[int]bool{2: true},
		},
	})

	// gzip-decompress: decompress data using zlib uncompress
	// params: data []byte, n i64
	// return: out []byte, out-n i64
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "gzip-decompress",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Doc:          "Decompress data using zlib uncompress, returns decompressed data and its length",
		CLibCall: &CLibCall{
			FuncName:    "uncompress",
			ArgTypes:    []LLVMArgType{LLVMI8Ptr, LLVMI64Ptr, LLVMI8Ptr, LLVMI64},
			RetType:     LLVMI32,
			RetBuf:      true,
			BufGlobal:   "@.gzip-buf",
			StrDataArg:  map[int]bool{2: true},
		},
	})
}