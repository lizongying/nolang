package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// str.eq: compare two strings for equality (method: a.eq(b, n))
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "str.eq",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Compare two strings for equality (method)",
		ForwardFunc:  "eq-raw",
	})

	// str.len: get string length
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "len",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Return the length of the string",
		ForwardFunc:  "str-len",
	})

	// str.len-bytes: return the BYTE length of the underlying UTF-8 buffer.
	// This is a compiler built-in (the same value as the old .len struct field);
	// it is intercepted by the codegen and never lowered as a real std function.
	// NOTE: str.len() (codepoint count) is a real std function that calls
	// .len-bytes() internally — keep this entry separate from "str-len" which
	// the compiler must NOT route s.len() to (that would return byte length).
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "len-bytes",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Return the byte length of the string",
		ForwardFunc:  "str-len-bytes",
	})

	// str.byte / txt.byte: raw-byte accessors (`s.byte(i)` / `t.byte(i)`).
	// NOT real functions — intercepted by the codegen (legacy generateRawByteAt
	// and MIR emitBuiltinRawByteAt) and expanded inline to a GEP+load+zext on the
	// underlying byte buffer. Registered here SOLELY so the MIR lowerer
	// (hir2mir.lowerCall) learns the result type is i64 and allocates a
	// destination value; without this entry `resultTypeOfCallee` returns void and
	// the call is emitted with Dst=0, discarding the result (the f64↔str
	// conversions in str.no depend on mstr.byte(j) and were broken under MIR=3).
	// ReceiverType is ReceiverStr only for table-matching purposes: FindBuiltinMethod
	// matches by bare MethodName "byte", and lookupBuiltin's bare-name fallback
	// makes this entry resolve both "str.byte" and "txt.byte".
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "byte",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Return the byte at index i of the underlying UTF-8 buffer (raw byte access)",
		Intercepted:  true,
	})

	// txt.set-byte / str.set-byte: raw-byte WRITER, the paired escape hatch of
	// the `.byte` reader. NOT a real function — intercepted by the MIR codegen
	// (emitBuiltinRawBytePut) and expanded inline to a GEP+store on the
	// receiver's underlying byte buffer, leaving the length untouched. Needed
	// once txt[i]/txt[i]= became CODE-POINT indexed: std routines that fill a
	// txt byte by byte must no longer go through `out[i] = b` (which would
	// re-encode and shift), they write raw bytes with set-byte instead.
	// ReceiverType is ReceiverStr only for table-matching purposes: like the
	// "byte" entry above, lookupBuiltin's bare-name fallback resolves both
	// "txt.set-byte" and "str.set-byte".
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "set-byte",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Write raw byte b at BYTE index i of the underlying UTF-8 buffer (raw byte access, length unchanged)",
		Intercepted:  true,
	})

	// str.clear: clear string in-place (set len=0, no storage switch)
	// SSO: store 0x80 (0 | SSO tag) to len byte
	// Long: store i64 0 to len field, cap/ptr unchanged
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "str.clear",
		Params:       []parser.Type{},
		Return:       []parser.Type{},
		Doc:          "Clear string in-place (set len=0, no storage switch)",
		ForwardFunc:  "str-clear",
	})

	// str.truncate: truncate string in-place to at most n bytes (len = min(len, n))
	// Replaces the low-level `s.len = n` pattern which is rejected by the validator.
	// cap/ptr unchanged; only the logical length is adjusted.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "str.truncate",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Truncate string in-place to at most n bytes (len = min(len, n))",
		ForwardFunc:  "str-truncate",
	})

	// with-cap: create a new str or vec with specified capacity (len=0)
	// Builtin syntax: with-cap(cap) — type inferred from assignment LHS
	//   s str = with-cap(256)   → str-long with cap=256
	//   v []i64 = with-cap(100) → vec with cap=100
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "with-cap",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Create a new str or vec with specified capacity (type inferred from LHS)",
		ForwardFunc:  "with-cap",
	})

	// with-len: create a new str or vec with specified length (len=cap=n)
	// Builtin syntax: with-len(len) — type inferred from assignment LHS
	//   v []i64 = with-len(100) → vec with len=100, cap=100
	// Unlike with-cap (len=0), with-len sets len=cap so direct index
	// reads/writes pass bounds checks without needing push() first.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "with-len",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Create a new str or vec with specified length (type inferred from LHS)",
		ForwardFunc:  "with-len",
	})

	// with-cap-len: create a new str or vec with specified capacity and length
	// Builtin syntax: with-cap-len(cap, len) — type inferred from assignment LHS
	//   v []i64 = with-cap-len(200, 100) → vec with cap=200, len=100
	// Combines with-cap (reserve capacity) and with-len (set length) in one call.
	// Useful when you need more capacity than the current length (pre-allocation).
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "with-cap-len",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Create a new str or vec with specified capacity and length (type inferred from LHS)",
		ForwardFunc:  "with-cap-len",
	})

	// vec.with-cap: method form of with-cap for vec/slice receivers
	//   v []i64 = [].with-cap(100) → %vec { len=0, cap=100, data=malloc(100*8) }
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverVec,
		MethodName:   "vec.with-cap",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Create a new vec with specified capacity (method form, type inferred from LHS)",
		ForwardFunc:  "with-cap",
	})

	// vec.with-len: method form of with-len for vec/slice receivers
	//   v []i64 = [].with-len(100) → %vec { len=100, cap=100, data=malloc(100*8) }
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverVec,
		MethodName:   "vec.with-len",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Create a new vec with specified length (method form, type inferred from LHS)",
		ForwardFunc:  "with-len",
	})

	// vec.with-len-cap: method form of with-cap-len for vec/slice receivers
	//   v []i64 = [].with-len-cap(100, 200) → %vec { len=100, cap=200 }
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverVec,
		MethodName:   "vec.with-len-cap",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Create a new vec with specified length and capacity (method form, type inferred from LHS)",
		ForwardFunc:  "with-cap-len",
	})
}
