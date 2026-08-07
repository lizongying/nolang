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
}
