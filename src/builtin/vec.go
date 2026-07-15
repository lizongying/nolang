package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// vec-eq: compare two slices for equality
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "vec-eq",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Compare two slices for equality",
		ForwardFunc:  "vec-eq",
	})

	// .len: get slice length (method on []t)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverVec,
		MethodName:   "len",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Return the length of the slice",
		ForwardFunc:  "vec-len",
	})

	// vec.push: append element to slice (with auto-grow)
	// Expansion: cap==0→4, cap<1024→cap*2, cap>=1024→cap*5/4
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverVec,
		MethodName:   "vec.push",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Push an element to the end of the slice (auto-grow)",
		ForwardFunc:  "vec-push",
	})

	// vec.clear: clear slice in-place (set len=0, no free/shrink)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverVec,
		MethodName:   "vec.clear",
		Params:       []parser.Type{},
		Return:       []parser.Type{},
		Doc:          "Clear slice in-place (set len=0, cap/data unchanged)",
		ForwardFunc:  "vec-clear",
	})

	// .pop: remove and return last element
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverVec,
		MethodName:   "pop",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Pop the last element from the slice",
		ForwardFunc:  "vec-pop",
	})

	// vec-sort: sort the slice in-place
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "vec-sort",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Sort the slice in ascending order",
		ForwardFunc:  "vec-sort",
	})

	// vec-reverse: reverse the slice in-place
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "vec-reverse",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Reverse the slice in-place",
		ForwardFunc:  "vec-reverse",
	})

	// arr-eq: compare two fixed-size arrays for equality
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "arr-eq",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Compare two fixed-size arrays for equality",
		ForwardFunc:  "arr-eq",
	})

	// .sort-asc: sort slice in ascending order (method on []t)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverVec,
		MethodName:   "sort-asc",
		Params:       []parser.Type{},
		Return:       []parser.Type{},
		Doc:          "Sort the slice in ascending order in-place (insertion sort)",
		ForwardFunc:  "vec-sort-asc",
	})

	// .sort-desc: sort slice in descending order (method on []t)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverVec,
		MethodName:   "sort-desc",
		Params:       []parser.Type{},
		Return:       []parser.Type{},
		Doc:          "Sort the slice in descending order in-place (insertion sort)",
		ForwardFunc:  "vec-sort-desc",
	})
}
