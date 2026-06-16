package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// vec-create: create a slice of length n filled with val
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "vec-create",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Create a slice of length n, fill all elements with val",
		ForwardFunc:  "vec_create",
	})

	// vec-eq: compare two slices for equality
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "vec-eq",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Compare two slices for equality",
		ForwardFunc:  "vec_eq",
	})

	// .len: get slice length (method on []t)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverVec,
		MethodName:   "len",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Return the length of the slice",
		ForwardFunc:  "vec_len",
	})

	// .push: append element to slice
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverVec,
		MethodName:   "push",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Push an element to the end of the slice",
		ForwardFunc:  "vec_push",
	})

	// .pop: remove and return last element
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverVec,
		MethodName:   "pop",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Pop the last element from the slice",
		ForwardFunc:  "vec_pop",
	})

	// vec-sort: sort the slice in-place
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "vec-sort",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Sort the slice in ascending order",
		ForwardFunc:  "vec_sort",
	})

	// vec-reverse: reverse the slice in-place
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "vec-reverse",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Reverse the slice in-place",
		ForwardFunc:  "vec_reverse",
	})

	// arr-eq: compare two fixed-size arrays for equality
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "arr-eq",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Compare two fixed-size arrays for equality",
		ForwardFunc:  "arr_eq",
	})

	// .sort-asc: sort slice in ascending order (method on []t)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverVec,
		MethodName:   "sort-asc",
		Params:       []parser.Type{},
		Return:       []parser.Type{},
		Doc:          "Sort the slice in ascending order in-place (insertion sort)",
		ForwardFunc:  "vec_sort_asc",
	})

	// .sort-desc: sort slice in descending order (method on []t)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverVec,
		MethodName:   "sort-desc",
		Params:       []parser.Type{},
		Return:       []parser.Type{},
		Doc:          "Sort the slice in descending order in-place (insertion sort)",
		ForwardFunc:  "vec_sort_desc",
	})
}
