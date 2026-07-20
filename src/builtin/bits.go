package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// rotate-left: rotate a u32/i64 value left by n bits.
	// Maps to llvm.fshl which LLVM lowers to ARM64 ROR instruction.
	// Usage: rotate-left(x, n) → (x << n) | (x >> (bits-n))
	// The result type follows the first argument's type (u32→i32, i64/u64→i64).
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "rotate-left",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Rotate integer left by n bits (maps to ARM64 ROR via llvm.fshl)",
		ForwardFunc:  "rotate-left",
	})

	// rotate-right: rotate a u32/i64 value right by n bits.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "rotate-right",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Rotate integer right by n bits (maps to ARM64 ROR via llvm.fshr)",
		ForwardFunc:  "rotate-right",
	})

	// load-le-u16: load little-endian u16 from byte slice at offset.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "load-le-u16",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeU16},
		Doc:          "Load little-endian u16 from byte array at given offset",
		ForwardFunc:  "load-le-u16",
	})

	// load-le-u32: load little-endian u32 from byte slice at offset.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "load-le-u32",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeU32},
		Doc:          "Load little-endian u32 from byte array at given offset",
		ForwardFunc:  "load-le-u32",
	})

	// load-le-u64: load little-endian u64 from byte slice at offset.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "load-le-u64",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeU64},
		Doc:          "Load little-endian u64 from byte array at given offset",
		ForwardFunc:  "load-le-u64",
	})

	// store-le-u32: store a u32 value to byte array at offset in little-endian format.
	// Replaces: buf[0]=v&255; buf[1]=(v>>8)&255; buf[2]=(v>>16)&255; buf[3]=(v>>24)&255 → single store.
	// Returns void (no return value).
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "store-le-u32",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64, parser.TypeU32},
		Return:       []parser.Type{},
		Doc:          "Store u32 value to byte array at given offset in little-endian format",
		ForwardFunc:  "store-le-u32",
	})
}
