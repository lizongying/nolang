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

	// load-le-u16/u32/u64: load little-endian integer from byte array at offset.
	// Registered both as global functions (load-le-u32(arr, off)) and as
	// []byte methods (arr.load-le-u32(off)) for ergonomic usage.
	// When used as a method, the receiver is the byte array, and the first
	// argument is the offset. Type inference determines the return type
	// (u16/u32/u64) from the method name.
	loadSpecs := []struct {
		name     string
		retType  parser.Type
		retNolang string
	}{
		{"load-le-u16", parser.TypeU16, "u16"},
		{"load-le-u32", parser.TypeU32, "u32"},
		{"load-le-u64", parser.TypeU64, "u64"},
	}
	for _, spec := range loadSpecs {
		// Global function form: load-le-u32(arr, offset)
		BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
			ReceiverType: ReceiverGlobal,
			MethodName:   spec.name,
			Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
			Return:       []parser.Type{spec.retType},
			Doc:          "Load little-endian " + spec.retNolang + " from byte array at given offset",
			ForwardFunc:  spec.name,
		})
		// []byte method form: arr.load-le-u32(offset)
		BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
			ReceiverType: ReceiverVec,
			MethodName:   "[]byte." + spec.name,
			Params:       []parser.Type{parser.TypeI64},
			Return:       []parser.Type{spec.retType},
			Doc:          "Load little-endian " + spec.retNolang + " from byte array at given offset",
			ForwardFunc:  spec.name,
		})
		// [N]byte method form (fixed array)
		BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
			ReceiverType: ReceiverArr,
			MethodName:   "[n]byte." + spec.name,
			Params:       []parser.Type{parser.TypeI64},
			Return:       []parser.Type{spec.retType},
			Doc:          "Load little-endian " + spec.retNolang + " from fixed byte array at given offset",
			ForwardFunc:  spec.name,
		})
	}

	// store-le-u32: store a u32 value to byte array at offset in little-endian format.
	// Global function form: store-le-u32(arr, offset, value)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "store-le-u32",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64, parser.TypeU32},
		Return:       []parser.Type{},
		Doc:          "Store u32 value to byte array at given offset in little-endian format",
		ForwardFunc:  "store-le-u32",
	})
	// []byte method form: arr.store-le-u32(offset, value)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverVec,
		MethodName:   "[]byte.store-le-u32",
		Params:       []parser.Type{parser.TypeI64, parser.TypeU32},
		Return:       []parser.Type{},
		Doc:          "Store u32 value to byte array at given offset in little-endian format",
		ForwardFunc:  "store-le-u32",
	})
	// [N]byte method form
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverArr,
		MethodName:   "[n]byte.store-le-u32",
		Params:       []parser.Type{parser.TypeI64, parser.TypeU32},
		Return:       []parser.Type{},
		Doc:          "Store u32 value to fixed byte array at given offset in little-endian format",
		ForwardFunc:  "store-le-u32",
	})

	// clear: zero all elements of a byte array/slice using llvm.memset.
	// Registered as []byte.clear() and [n]byte.clear() — an alias for zero().
	// Usage: buf.clear()  →  equivalent to  j <- [0..64): buf[j] = 0
	// but generates a single llvm.memset intrinsic call.
	for _, prefix := range []string{"[]byte.", "[n]byte."} {
		BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
			ReceiverType: ReceiverVec,
			MethodName:   prefix + "clear",
			Params:       []parser.Type{},
			Return:       []parser.Type{},
			Doc:          "Zero all elements using llvm.memset (alias for zero)",
			ForwardFunc:  "arr-zero",
		})
	}

	// f64.to-bits: reinterpret the 64 IEEE-754 bits of an f64 as a u64
	// (LLVM bitcast double->i64). Needed by src/std/number.no's shortest
	// round-trip f64.to-str (no libm, pure Nolang bigint expansion of the
	// exact m×2^E value, then shortest decimal selection). Mirrors the
	// project's "禁 libm、纯 .no 实现" constraint — the bitcast is a native
	// LLVM op, not a C math call.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverF64,
		MethodName:   "f64.to-bits",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeU64},
		Doc:          "Reinterpret f64 bits as u64 (bitcast double->i64)",
		ForwardFunc:  "f64-to-bits",
	})

	// u64.from-bits: inverse of f64.to-bits (bitcast i64->double). Pairs with
	// f64.to-bits so the exact bit pattern can be recovered — useful for
	// high-precision decimal parsing and round-trip checks.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverU64,
		MethodName:   "u64.from-bits",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeF64},
		Doc:          "Reinterpret u64 bits as f64 (bitcast i64->double)",
		ForwardFunc:  "u64-from-bits",
	})
}
