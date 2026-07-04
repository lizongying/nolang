package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// net-listen: create TCP listening socket
	// Performs: socket(AF_INET, SOCK_STREAM, 0) + setsockopt(SO_REUSEADDR) + bind + listen
	// Args: host str (IP address), port i64
	// Returns: fd i64 (socket fd, -1 on error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-listen",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Create TCP listening socket. Returns fd (-1 on error)",
		ForwardFunc:  "net-listen",
	})

	// net-dial: create TCP client connection
	// Performs: socket(AF_INET, SOCK_STREAM, 0) + connect
	// Args: host str (IP address), port i64
	// Returns: fd i64 (socket fd, -1 on error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-dial",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Create TCP client connection. Returns fd (-1 on error)",
		ForwardFunc:  "net-dial",
	})

	// net-accept: accept TCP connection
	// Args: listen-fd i64
	// Returns: fd i64 (client socket fd, -1 on error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-accept",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Accept TCP connection. Returns client fd (-1 on error)",
		ForwardFunc:  "net-accept",
	})

	// net-send: send data on connected socket
	// Args: fd i64, data str, n i64
	// Returns: written i64 (bytes sent, -1 on error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-send",
		Params:       []parser.Type{parser.TypeI64, parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Send data on connected socket. Returns bytes sent (-1 on error)",
		ForwardFunc:  "net-send",
	})

	// net-recv: receive data on connected socket
	// Args: fd i64, buf str, n i64
	// Returns: read-n i64 (bytes received, -1 on error, 0 on connection closed)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-recv",
		Params:       []parser.Type{parser.TypeI64, parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Receive data on connected socket. Returns bytes received (-1 on error, 0 on closed)",
		ForwardFunc:  "net-recv",
	})
}
