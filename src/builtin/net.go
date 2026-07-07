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
	// Args: fd i64, data str|[]byte, n i64
	// Returns: written i64 (bytes sent, -1 on error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-send",
		Params:       []parser.Type{parser.TypeI64, parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Send data on connected socket. Accepts str or []byte. Returns bytes sent (-1 on error)",
		ForwardFunc:  "net-send",
	})

	// net-recv: receive data on connected socket
	// Args: fd i64, buf str|[]byte, n i64
	// Returns: read-n i64 (bytes received, -1 on error, 0 on connection closed)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-recv",
		Params:       []parser.Type{parser.TypeI64, parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Receive data on connected socket. Accepts str or []byte buffer. Returns bytes received (-1 on error, 0 on closed)",
		ForwardFunc:  "net-recv",
	})

	// net-udp-open: create a UDP socket
	// Performs: socket(AF_INET, SOCK_DGRAM, 0)
	// Args: none
	// Returns: fd i64 (socket fd, -1 on error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-udp-open",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Create UDP socket (SOCK_DGRAM). Returns fd (-1 on error)",
		ForwardFunc:  "net-udp-open",
	})

	// net-udp-sendto: send UDP datagram to address
	// Args: fd i64, data str|[]byte, n i64, host str, port i64
	// Returns: written i64 (bytes sent, -1 on error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-udp-sendto",
		Params:       []parser.Type{parser.TypeI64, parser.TypeStr, parser.TypeI64, parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Send UDP datagram to host:port. Returns bytes sent (-1 on error)",
		ForwardFunc:  "net-udp-sendto",
	})

	// net-udp-recvfrom: receive UDP datagram
	// Args: fd i64, buf str|[]byte, n i64
	// Returns: read-n i64 (bytes received, -1 on error, 0 on timeout/closed)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-udp-recvfrom",
		Params:       []parser.Type{parser.TypeI64, parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Receive UDP datagram. Returns bytes received (-1 on error, 0 on timeout)",
		ForwardFunc:  "net-udp-recvfrom",
	})

	// net-set-recv-timeout: set socket recv timeout (SO_RCVTIMEO)
	// Args: fd i64, timeout-ms i64
	// Returns: ok i64 (0=success, -1=error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-set-recv-timeout",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Set socket recv timeout in milliseconds. Returns 0 on success, -1 on error",
		ForwardFunc:  "net-set-recv-timeout",
	})

	// unix-listen: create Unix domain listening socket
	// Performs: unlink(path) + socket(AF_UNIX, SOCK_STREAM, 0) + bind + listen
	// Args: path str (socket file path)
	// Returns: fd i64 (socket fd, -1 on error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "unix-listen",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Create Unix domain listening socket. Returns fd (-1 on error)",
		ForwardFunc:  "unix-listen",
	})

	// unix-dial: connect to Unix domain socket
	// Performs: socket(AF_UNIX, SOCK_STREAM, 0) + connect
	// Args: path str (socket file path)
	// Returns: fd i64 (socket fd, -1 on error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "unix-dial",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Connect to Unix domain socket. Returns fd (-1 on error)",
		ForwardFunc:  "unix-dial",
	})
}
