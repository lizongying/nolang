package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// db-open: open SQLite database, returns handle (0 = failed)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "db-open",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Open SQLite database, returns handle (0 = failed)",
		ForwardFunc:  "db-open",
	})

	// db-close: close database handle
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "db-close",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Close SQLite database",
		ForwardFunc:  "db-close",
	})

	// db-exec: execute SQL (no results)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "db-exec",
		Params:       []parser.Type{parser.TypeI64, parser.TypeStr},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Execute SQL statement (CREATE/INSERT/UPDATE/DELETE)",
		ForwardFunc:  "db-exec",
	})

	// db-prepare: prepare SQL statement, returns stmt handle (0 = failed)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "db-prepare",
		Params:       []parser.Type{parser.TypeI64, parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Prepare SQL statement, returns stmt handle (0 = failed)",
		ForwardFunc:  "db-prepare",
	})

	// db-step: step statement, returns rc (100=ROW, 101=DONE)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "db-step",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Step statement, returns rc (100=ROW, 101=DONE)",
		ForwardFunc:  "db-step",
	})

	// db-column-count: get column count
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "db-column-count",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get column count of prepared statement",
		ForwardFunc:  "db-column-count",
	})

	// db-column-int64: get column value as int64
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "db-column-int64",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get column value as int64",
		ForwardFunc:  "db-column-int64",
	})

	// db-column-text: get column value as string
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "db-column-text",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Get column value as string",
		ForwardFunc:  "db-column-text",
	})

	// db-column-double: get column value as f64
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "db-column-double",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeF64},
		Doc:          "Get column value as f64",
		ForwardFunc:  "db-column-double",
	})

	// db-finalize: finalize statement
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "db-finalize",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Finalize prepared statement",
		ForwardFunc:  "db-finalize",
	})

	// db-last-insert-rowid: get last insert rowid
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "db-last-insert-rowid",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get last insert rowid",
		ForwardFunc:  "db-last-insert-rowid",
	})

	// db-changes: get number of changes
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "db-changes",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get number of rows changed by last statement",
		ForwardFunc:  "db-changes",
	})

	// db-bind-int64: bind int64 parameter
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "db-bind-int64",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Bind int64 parameter to prepared statement",
		ForwardFunc:  "db-bind-int64",
	})

	// db-bind-text: bind text parameter
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "db-bind-text",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64, parser.TypeStr},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Bind text parameter to prepared statement",
		ForwardFunc:  "db-bind-text",
	})

	// db-errmsg: get error message
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "db-errmsg",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Get error message from database handle",
		ForwardFunc:  "db-errmsg",
	})
}
