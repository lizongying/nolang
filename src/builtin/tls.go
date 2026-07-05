package builtin

import "github.com/lizongying/nolang/parser"

// TLS builtins (OpenSSL) — for HTTPS and HTTP/2 over TLS
func init() {
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "tls-init",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Initialize OpenSSL and create global SSL_CTX. Returns 1 on success, 0 on error",
		ForwardFunc:  "tls-init",
	})

	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "tls-connect",
		Params:       []parser.Type{parser.TypeI64, parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Wrap an existing socket fd with TLS. Returns SSL* as i64 (0 on error)",
		ForwardFunc:  "tls-connect",
	})

	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "tls-send",
		Params:       []parser.Type{parser.TypeI64, parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Send data over TLS connection. Returns bytes written (-1 on error)",
		ForwardFunc:  "tls-send",
	})

	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "tls-recv",
		Params:       []parser.Type{parser.TypeI64, parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Receive data over TLS connection. Returns bytes read (-1 on error, 0 on closed)",
		ForwardFunc:  "tls-recv",
	})

	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "tls-close",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Close TLS connection (SSL_shutdown + SSL_free). Returns 1 on success",
		ForwardFunc:  "tls-close",
	})
}
