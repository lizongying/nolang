package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// net-listen: create TCP listening socket
	// Performs: socket(AF_INET, SOCK_STREAM, 0) + setsockopt(SO_REUSEADDR) + bind + listen
	// Args: host str (IP address), port i64
	// Returns: fd (socket fd, -1 on error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-listen",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeFd},
		Doc:          "Create TCP listening socket. Returns fd (-1 on error)",
		ForwardFunc:  "net-listen",
	})

	// net-dial: create TCP client connection
	// Performs: socket(AF_INET, SOCK_STREAM, 0) + connect
	// Args: host str (IP address), port i64
	// Returns: fd (socket fd, -1 on error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-dial",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeFd},
		Doc:          "Create TCP client connection. Returns fd (-1 on error)",
		ForwardFunc:  "net-dial",
	})

	// net-accept: accept TCP connection
	// Args: listen-fd fd
	// Returns: fd (client socket fd, -1 on error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-accept",
		Params:       []parser.Type{parser.TypeFd},
		Return:       []parser.Type{parser.TypeFd},
		Doc:          "Accept TCP connection. Returns client fd (-1 on error)",
		ForwardFunc:  "net-accept",
	})

	// net-recv-nb: non-blocking receive on a connected socket (MSG_DONTWAIT).
	// Unlike net-recv (which blocks until data arrives), this returns
	// immediately when no data is available.
	// Args: fd, buf str|[]byte, n i64
	// Returns: read-n i64
	//   > 0 : bytes received
	//     0 : connection closed (peer sent FIN)
	//    -1 : hard error
	//    -2 : would block (no data available right now)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-recv-nb",
		Params:       []parser.Type{parser.TypeFd, parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Non-blocking receive (MSG_DONTWAIT). Returns bytes (>0), 0=closed, -1=error, -2=would-block",
		ForwardFunc:  "net-recv-nb",
	})

	// net-send: send data on connected socket
	// Args: fd, data str|[]byte, n i64
	// Returns: written i64 (bytes sent, -1 on error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-send",
		Params:       []parser.Type{parser.TypeFd, parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Send data on connected socket. Accepts str or []byte. Returns bytes sent (-1 on error)",
		ForwardFunc:  "net-send",
	})

	// net-recv: receive data on connected socket
	// Args: fd, buf str|[]byte, n i64
	// Returns: read-n i64 (bytes received, -1 on error, 0 on connection closed)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-recv",
		Params:       []parser.Type{parser.TypeFd, parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Receive data on connected socket. Accepts str or []byte buffer. Returns bytes received (-1 on error, 0 on closed)",
		ForwardFunc:  "net-recv",
	})

	// net-udp-open: create a UDP socket
	// Performs: socket(AF_INET, SOCK_DGRAM, 0)
	// Args: none
	// Returns: fd (socket fd, -1 on error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-udp-open",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeFd},
		Doc:          "Create UDP socket (SOCK_DGRAM). Returns fd (-1 on error)",
		ForwardFunc:  "net-udp-open",
	})

	// net-udp-sendto: send UDP datagram to address
	// Args: fd, data str|[]byte, n i64, host str, port i64
	// Returns: written i64 (bytes sent, -1 on error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-udp-sendto",
		Params:       []parser.Type{parser.TypeFd, parser.TypeStr, parser.TypeI64, parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Send UDP datagram to host:port. Returns bytes sent (-1 on error)",
		ForwardFunc:  "net-udp-sendto",
	})

	// net-udp-recvfrom: receive UDP datagram
	// Args: fd, buf str|[]byte, n i64
	// Returns: read-n i64 (bytes received, -1 on error, 0 on timeout/closed)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-udp-recvfrom",
		Params:       []parser.Type{parser.TypeFd, parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Receive UDP datagram. Returns bytes received (-1 on error, 0 on timeout)",
		ForwardFunc:  "net-udp-recvfrom",
	})

	// net-set-recv-timeout: set socket recv timeout (SO_RCVTIMEO)
	// Args: fd, timeout-ms i64
	// Returns: ok i64 (0=success, -1=error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-set-recv-timeout",
		Params:       []parser.Type{parser.TypeFd, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Set socket recv timeout in milliseconds. Returns 0 on success, -1 on error",
		ForwardFunc:  "net-set-recv-timeout",
	})

	// unix-listen: create Unix domain listening socket
	// Performs: unlink(path) + socket(AF_UNIX, SOCK_STREAM, 0) + bind + listen
	// Args: path str (socket file path)
	// Returns: fd (socket fd, -1 on error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "unix-listen",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeFd},
		Doc:          "Create Unix domain listening socket. Returns fd (-1 on error)",
		ForwardFunc:  "unix-listen",
	})

	// unix-dial: connect to Unix domain socket
	// Performs: socket(AF_UNIX, SOCK_STREAM, 0) + connect
	// Args: path str (socket file path)
	// Returns: fd (socket fd, -1 on error)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "unix-dial",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeFd},
		Doc:          "Connect to Unix domain socket. Returns fd (-1 on error)",
		ForwardFunc:  "unix-dial",
	})

	// net-icmp-open: create an ICMP socket for ping
	// macOS:  socket(AF_INET, SOCK_DGRAM, IPPROTO_ICMP)  — unprivileged
	// Linux:  socket(AF_INET, SOCK_RAW,  IPPROTO_ICMP)  — requires root or CAP_NET_RAW
	// Returns: fd (-1 on error)
	// Use net-udp-sendto / net-udp-recvfrom for I/O (port=0).
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "net-icmp-open",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeFd},
		Doc:          "Create ICMP socket for ping. Returns fd (-1 on error, requires root on Linux)",
		ForwardFunc:  "net-icmp-open",
	})

	// win-wsa-startup: initialize Winsock 2.2 on Windows (WSAStartup)
	// Args: none
	// Returns: ok bool (true on success)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "win-wsa-startup",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Initialize Winsock 2.2 on Windows (WSAStartup). Returns true on success",
		ForwardFunc:  "win-wsa-startup",
	})
}
