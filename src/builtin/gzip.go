package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "gzip-compress",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Doc:          "Compress data using zlib compress2, returns compressed data and its length",
		ForwardFunc:  "gzip-compress",
	})

	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "gzip-decompress",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Doc:          "Decompress data using zlib uncompress, returns decompressed data and its length",
		ForwardFunc:  "gzip-decompress",
	})
}
