// Package stdastembed carries the baked std AST blob into the no binary.
//
// genstdsig writes std_ast.bin (the gzip+gob map[embedPath][]byte of every std
// module's serialized Program) and key.go (the std-content key) into this
// directory; //go:embed pulls the blob in at compile time. cmd/no reads Blob
// and Key and hands them to checker.SetStdASTBlob before any build.
//
// Splitting the embed into its own package avoids a chicken-egg: genstdsig
// compiles the checker package, so checker itself must not //go:embed the blob
// (the file does not exist until genstdsig runs). The embed lives here, where
// only the final `no` binary build reaches it.
package stdastembed

import _ "embed"

//go:embed std_ast.bin
var Blob []byte
