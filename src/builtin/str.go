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
	// NOT real functions — intercepted by the codegen (MIR emitBuiltinRawByteAt)
	// and expanded inline to a BOUNDS-CHECKED GEP+load+zext on the underlying
	// byte buffer. Registered here SOLELY so the MIR lowerer
	// (hir2mir.lowerCall) learns the result type; without this entry
	// `resultTypeOfCallee` returns void and the call is emitted with Dst=0,
	// discarding the result (the f64↔str conversions in str.no depend on
	// mstr.byte(j) and were broken under MIR=3).
	// ReceiverType is ReceiverStr only for table-matching purposes: FindBuiltinMethod
	// matches by bare MethodName "byte", and lookupBuiltin's bare-name fallback
	// makes this entry resolve both "str.byte" and "txt.byte".
	//
	// 2026-10-21: the result is now `?byte` (option), NOT a bare i64. The raw
	// accessor used to read the buffer with NO bounds check — out-of-range
	// indices were undefined behaviour. `byte` is a PUBLIC method (user code
	// calls `s.byte(i)`), so it now fails like `arr[i]`: out of range returns
	// nil instead of reading past the buffer. Callers must handle the option
	// (match / `?=` / `_ =`). Codegen emits `tag = (i < len) ? ok : nil` and
	// only dereferences the buffer when the index is in range (see
	// emitBuiltinRawByteAt).
	//
	// 🔴 `#{index-out = DEF}` does NOT unwrap (D1, docs/design/option-unified.md):
	// the annotation only picks the value used when the range check fails, the
	// result type stays `?byte`, and a plain target still has to go through
	// match / `?=`. The checker enforces that on an explicitly plain-annotated
	// let (`b i64 = s.byte(0)` is trace 15w45dqk, see optionResultOfCallTarget).
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "byte",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{&parser.NullableType{Type: parser.TypeByte}},
		Doc:          "Return the byte at index i of the underlying UTF-8 buffer as ?byte (nil when out of range)",
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
	//
	// Params are `(i64, byte)` — the value is a real `byte`, mirroring src/std/
	// str.no / txt.no. Option-typed arguments are accepted by the builtin checker
	// (see the `strings.HasPrefix(argType, "?")` skip in stdMethodParamTypes,
	// funcargs.go), so the many `out.set-byte(i, s.byte(i))` sites (a `?byte`)
	// pass unchanged. Only genuinely-plain i64/char call-site args (a `HEX[..]`
	// char, an `bv` i64) would trip `stdargtyp`; those are narrowed at the call
	// site with a bitwise mask (`& 255`), which the checker recognises as safe
	// narrowing to `byte`.
	//
	// Return is `?bool` (fallible, paired with the `?byte` reader above): the
	// emitter bounds-checks the BYTE index against the receiver's PHYSICAL
	// capacity (a `txt` is a fixed 255-byte buffer, a `%str-long` uses its `cap`
	// field) — NOT the logical length, so fill-then-set-len builders keep
	// working — and returns nil (skipping the store) when out of range, some(true)
	// on success (see emitBuiltinRawBytePut). Call sites that discard the `?bool`
	// as a bare statement are legal: optunuse only fires on bound names.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "set-byte",
		Params:       []parser.Type{parser.TypeI64, parser.TypeByte},
		Return:       []parser.Type{&parser.NullableType{Type: parser.TypeBool}},
		Doc:          "Write raw byte b at BYTE index i of the underlying UTF-8 buffer, returns ?bool (nil when i is out of capacity, store skipped)",
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

	// new: build a str or txt directly from a []char (module-level constructor).
	// Builtin syntax: txt.new(cs) / str.new(cs) — result type inferred from LHS.
	// Each char (i32 code point) is written as ONE raw byte and the length is set
	// in a single pass (no UTF-8 re-encode, no per-byte std loop): txt/str are
	// built-in types whose bytes/length cannot be manipulated from user code, so
	// the whole conversion is lowered by the compiler (ForwardFunc "chars-new").
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "new",
		Params:       []parser.Type{&parser.SliceType{Elem: parser.TypeChar}},
		Return:       []parser.Type{},
		Doc:          "Build a str or txt from a []char (raw bytes + length in one pass, type inferred from LHS)",
		ForwardFunc:  "chars-new",
	})

	// ------------------------------------------------------------------
	// str / txt PRIVATE field accessors / mutators (OWNER-MODULE ONLY).
	//
	// These underscore-prefixed compiler builtins read/write the INTERNAL
	// layout fields of %str-long / %txt directly — there is no other way for
	// std code to touch them, because str/txt bytes+length are opaque to user
	// code:
	//
	//   str (%str-long = { i64 len, i64 cap, i8* data }):
	//     _len()  -> i64      read field 0 (logical byte length)
	//     _cap()  -> i64      read field 1 (capacity)
	//     _data() -> *byte    read field 2 (i8* data pointer)
	//     _set-len(n i64)     write field 0
	//     _set-cap(n i64)     write field 1
	//     _set-data(p *byte)  write field 2
	//
	//   txt (%txt = { [255 x i8] data, i8 len }) — no capacity; the 255-byte
	//   buffer is inline:
	//     _len()  -> i64      read field 1 (i8 byte length, zext to i64)
	//     _data() -> *byte    read field 0 (inline buffer start, bitcast i8*)
	//     _set-len(n i64)     write field 1 (trunc i64 to i8)
	//
	// The underscore prefix is a PRIVATE marker: the checker rejects ANY call
	// from outside the owning std module (src/std/str.no / src/std/txt.no) at
	// compile time — see checkPrivateBuiltinMethodCall in checker/funcargs.go.
	// The ForwardFuncs expand inline to GEP+load/store on the receiver
	// aggregate (emitBuiltinPrivateField) and never lower as real std functions.
	// ReceiverType is ReceiverStr only for table-matching purposes (there is
	// no ReceiverTxt); dispatch is by the qualified MethodName.
	// ------------------------------------------------------------------
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "str._len",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "PRIVATE(str): read the logical length field of %str-long",
		ForwardFunc:  "str-_len",
	})
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "str._cap",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "PRIVATE(str): read the capacity field of %str-long",
		ForwardFunc:  "str-_cap",
	})
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "str._data",
		Params:       []parser.Type{},
		Return:       []parser.Type{&parser.PointerType{Type: parser.TypeByte}},
		Doc:          "PRIVATE(str): read the data pointer (i8*) of %str-long",
		ForwardFunc:  "str-_data",
	})
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "str._set-len",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "PRIVATE(str): write the logical length field of %str-long",
		ForwardFunc:  "str-_set-len",
	})
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "str._set-cap",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "PRIVATE(str): write the capacity field of %str-long",
		ForwardFunc:  "str-_set-cap",
	})
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "str._set-data",
		Params:       []parser.Type{&parser.PointerType{Type: parser.TypeByte}},
		Return:       []parser.Type{},
		Doc:          "PRIVATE(str): write the data pointer (i8*) of %str-long",
		ForwardFunc:  "str-_set-data",
	})
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "txt._len",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "PRIVATE(txt): read the byte-length field of %txt",
		ForwardFunc:  "txt-_len",
	})
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "txt._data",
		Params:       []parser.Type{},
		Return:       []parser.Type{&parser.PointerType{Type: parser.TypeByte}},
		Doc:          "PRIVATE(txt): read the inline buffer start (i8*) of %txt",
		ForwardFunc:  "txt-_data",
	})
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "txt._set-len",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "PRIVATE(txt): write the byte-length field of %txt",
		ForwardFunc:  "txt-_set-len",
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
