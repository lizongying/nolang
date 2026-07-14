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
}
