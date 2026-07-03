package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "gzip-compress",
		Params:       []parser.Type{&parser.SliceType{Elem: parser.TypeByte}},
		Return:       []parser.Type{&parser.SliceType{Elem: parser.TypeByte}},
		Doc:          "Compress []byte data using zlib compress2, returns compressed []byte",
		ForwardFunc:  "gzip-compress",
	})

	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "gzip-decompress",
		Params:       []parser.Type{&parser.SliceType{Elem: parser.TypeByte}},
		Return:       []parser.Type{&parser.SliceType{Elem: parser.TypeByte}},
		Doc:          "Decompress []byte data using zlib uncompress, returns decompressed []byte",
		ForwardFunc:  "gzip-decompress",
	})

	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "inflate-decompress",
		Params:       []parser.Type{&parser.SliceType{Elem: parser.TypeByte}, parser.TypeI64},
		Return:       []parser.Type{&parser.SliceType{Elem: parser.TypeByte}},
		Doc:          "Decompress raw DEFLATE data (RFC 1951, ZIP method 8), out_size is expected uncompressed size, returns decompressed []byte",
		ForwardFunc:  "inflate-decompress",
	})
}
