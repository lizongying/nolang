package checker

import (
	"bytes"
	"compress/gzip"
	"encoding/gob"
	"fmt"
	"io/fs"
	"os"
	"sync"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// std AST cache (Tier 0 of the incremental-build AST cache).
//
// The serialized std AST blob is produced by genstdsig and embedded into the
// binary by the separate `checker/stdastembed` package (so that genstdsig
// itself — which compiles this package — does not require the blob to already
// exist). The embed package populates Blob/Key at program init; cmd/no hands
// them to checker via SetStdASTBlob before any build. A nil blob makes
// StdASTFor fall back to on-the-fly parsing, so the build still works.
//
// The format is parser.CachedProgram (Program + flattened nodeSem side-table);
// see parser/astcache.go. AST only — MIR/LLVM IR is never cached here, so the
// cache key needs no target/opt-level/bounds-check dimension.
var (
	stdASTMu     sync.Mutex
	stdASTBlob   []byte
	stdASTKey    string
	stdASTMap    map[string][]byte
	stdASTLoaded bool
)

// SetStdASTBlob installs the baked std AST blob (and its key) into the checker
// at process start. Called by cmd/no from the stdastembed package.
func SetStdASTBlob(blob []byte, key string) {
	stdASTMu.Lock()
	stdASTBlob = blob
	stdASTKey = key
	stdASTLoaded = false // force re-decode on next use
	stdASTMap = nil
	stdASTMu.Unlock()
}

// loadStdAST decodes stdASTBlob into stdASTMap exactly once.
func loadStdAST() {
	stdASTMu.Lock()
	defer stdASTMu.Unlock()
	if stdASTLoaded {
		return
	}
	stdASTLoaded = true
	if len(stdASTBlob) == 0 {
		return
	}
	// Stale blob (regenerated signatures but not AST, or vice versa): error
	// instead of silently falling back to parsing. A key mismatch means the
	// blob was built from a different src/std than the binary's injected key —
	// a build inconsistency. Mirror the std signature tables' policy
	// (CollectStdModuleSignatures / knownStdModules hard-fail) and force
	// `make no` (which runs `make gen` first) so the blob and key stay in sync.
	if stdASTKey != "" && StdSigKey != "" && stdASTKey != StdSigKey {
		fmt.Fprintln(os.Stderr, "[no] ERROR: 內嵌 std AST blob 與建構時注入的簽名 key 不符（建構不一致）。")
		fmt.Fprintln(os.Stderr, "[no]   修正：執行 `make no`（它會先跑 `make gen` 再建構並注入 key）。")
		os.Exit(1)
	}
	zr, err := gzip.NewReader(bytes.NewReader(stdASTBlob))
	if err != nil {
		return
	}
	defer zr.Close()
	dec := make(map[string][]byte)
	if err := gob.NewDecoder(zr).Decode(&dec); err != nil {
		return
	}
	stdASTMap = dec
}

// StdASTFor returns the baked std module Program for embedPath (e.g.
// "std/math.no"), deserializing it on demand. The second result is false when
// the module was not baked (or the blob is stale/undecodable), in which case
// the caller should parse from StdFS as before. Each call returns a FRESH
// decode, preserving the current "parse fresh per load" codegen semantics.
func StdASTFor(embedPath string) (*parser.Program, bool) {
	loadStdAST()
	stdASTMu.Lock()
	raw, ok := stdASTMap[embedPath]
	stdASTMu.Unlock()
	if !ok {
		return nil, false
	}
	var cp parser.CachedProgram
	if err := gob.NewDecoder(bytes.NewReader(raw)).Decode(&cp); err != nil {
		return nil, false
	}
	if os.Getenv("NOLANG_DEBUG_IT") != "" {
		fmt.Fprintf(os.Stderr, "[debug-it] StdASTFor HIT: %s\n", embedPath)
	}
	parser.RestoreNodeSem(cp.Prog, cp.NodeSem)
	return cp.Prog, true
}

// SerializeStdAST parses every std module under fsys and returns a
// map[embedPath][]byte of gob-encoded parser.CachedProgram blobs (the whole
// map is gzipped once by genstdsig), for the generator to bake into the binary.
// No per-module gzip: StdASTFor gob-decodes the inner bytes directly, so a
// second gzip layer would be redundant — and it was a latent bug, because the
// inner gzip was never undone on decode, so the baked blob always failed to
// decode and the build silently fell back to parsing std from source.
func SerializeStdAST(fsys fs.FS) (map[string][]byte, error) {
	known := ListStdModules(fsys)
	out := make(map[string][]byte, len(known))
	for _, info := range known {
		embedPath := "std/" + info.FullPath + ".no"
		source, err := fs.ReadFile(fsys, embedPath)
		if err != nil {
			return nil, fmt.Errorf("std AST: read %s: %w", embedPath, err)
		}
		l := lexer.NewCached(embedPath, string(source))
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			return nil, fmt.Errorf("std AST: parse %s failed: %v", embedPath, p.Errors())
		}
		cp := &parser.CachedProgram{Prog: prog, NodeSem: parser.SerializeNodeSem(prog)}
		var raw bytes.Buffer
		if err := gob.NewEncoder(&raw).Encode(cp); err != nil {
			return nil, fmt.Errorf("std AST: encode %s: %w", embedPath, err)
		}
		out[embedPath] = raw.Bytes()
	}
	return out, nil
}
