// Package stdastembed carries the baked std AST blob into the binaries that
// consume the Tier-0 std AST cache.
//
// genstdsig writes std-ast.bin (gzip+gob StdASTBlob whose Mods is [][]byte —
// one gob(CachedProgram) per std module, each carrying its embed path on
// Program.SourcePath / ModulePath) into this directory; //go:embed pulls the
// blob in at compile time. cmd/no and cmd/lsp each read Blob and hand it to
// checker.SetStdASTBlob before any build.
//
// Splitting the embed into its own package avoids a chicken-egg: genstdsig
// compiles the checker package, so checker itself must not //go:embed the blob
// (the file does not exist until genstdsig runs). The embed lives here, where
// only the final consumer binary builds reach it.
//
// Embedding (rather than reading a shared on-disk cache) is what keeps each
// shipped binary consistent with its own parser config: the blob compiled into
// a binary is the one that binary's loadStdAST fingerprint guard was built to
// accept, so a stale cache left by an older binary can never make it hard-exit.
package stdastembed

import _ "embed"

//go:embed std-ast.bin
var Blob []byte
