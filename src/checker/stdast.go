package checker

import (
	"bytes"
	"compress/gzip"
	"encoding/gob"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// std AST cache (Tier 0 of the incremental-build AST cache).
//
// The serialized std AST blob is produced by genstdsig and embedded into the
// binary by the separate `checker/stdastembed` package (so that genstdsig
// itself — which compiles this package — does not require the blob to already
// exist). The embed package populates Blob at program init; cmd/no hands it to
// checker via SetStdASTBlob before any build. A nil blob makes StdASTFor fall
// back to on-the-fly parsing, so the build still works.
//
// The format is parser.CachedProgram (Program + the flattened occurrence-keyed
// side tables);
// see parser/astcache.go. AST only — MIR/LLVM IR is never cached here, so the
// cache key needs no target/opt-level/bounds-check dimension.
var (
	stdASTMu     sync.Mutex
	stdASTBlob   []byte
	stdASTMap    map[string][]byte
	stdASTLoaded bool
)

// SetStdASTBlob installs the baked std AST blob into the checker at process
// start. Called by cmd/no from the stdastembed package. The blob's staleness is
// guarded by its parse-configuration fingerprint (see StdASTBlob.Config) and by
// the build-path StdSigKey emptiness check — there is no separate content key.
func SetStdASTBlob(blob []byte) {
	stdASTMu.Lock()
	stdASTBlob = blob
	stdASTLoaded = false // force re-decode on next use
	stdASTMap = nil
	stdASTMu.Unlock()
}

// StdASTEntry is one baked std module: its embed path and the (deterministic)
// gob encoding of its parser.CachedProgram.
type StdASTEntry struct {
	Path string
	Data []byte
}

// StdASTBlob is the Tier-0 payload: the parse-configuration fingerprint the
// blob was produced with, plus the module list. Mods is sorted by Path so the
// encoded bytes are reproducible.
//
// Config is the Tier-0 counterpart of build.astCacheSalt. Parsing is not
// inference-free (lowering consults the injected cross-module tables), so a
// baked AST is only valid for the configuration that produced it. Storing the
// fingerprint here — and comparing it at load time — is what makes "parse std
// with a bare parser.New and bake the result" fail loudly. That is not
// hypothetical: it is exactly how std/txt.no's `txt.replace-n` shipped an AST
// the build path would never have produced (`parts[i]` left as a bare element
// read, so `s = it` bound a txt where an option payload was expected, and
// `u.replace-n('one','1',1)` returned "1one" instead of "1 two one").
type StdASTBlob struct {
	Config string
	Mods   []StdASTEntry
}

// stdParseConfigFingerprint derives the expected Tier-0 configuration from the
// signature tables this binary would inject into the parser. The tables are the
// baked ones (embeddedStdFuncSigs / ...), projected to their RESULT types —
// which is the shape parser.SetExternSignatures consumes and therefore the
// shape the generator's fingerprint is computed from.
func stdParseConfigFingerprint(funcSigs, methodSigs map[string]StdSigEntry, structFields map[string]map[string]string, enumVariants map[string][]string) string {
	fs := make(map[string][]string, len(funcSigs))
	for k, e := range funcSigs {
		fs[k] = e.Results
	}
	ms := make(map[string][]string, len(methodSigs))
	for k, e := range methodSigs {
		ms[k] = e.Results
	}
	return parser.ExternSignatureFingerprint(fs, ms, structFields, enumVariants)
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
	zr, err := gzip.NewReader(bytes.NewReader(stdASTBlob))
	if err != nil {
		return
	}
	defer zr.Close()
	var blob StdASTBlob
	if err := gob.NewDecoder(zr).Decode(&blob); err != nil {
		return
	}
	// The blob's parse configuration must match the one this binary would use.
	// A mismatch means the blob was baked by a parser that was configured
	// differently (e.g. one that never received SetExternSignatures), so its
	// AST is not what the build path would have produced — silently using it
	// reintroduces the class of bug described on StdASTBlob.
	//
	// Skipped when the signature tables are not baked (no stdsig_gen.go), where
	// there is no expectation to compare against; that build already hard-fails
	// on the signature tables themselves (StdSigKey is empty).
	if blob.Config != "" && StdSigKey != "" {
		want := stdParseConfigFingerprint(embeddedStdFuncSigs, embeddedStdMethodSigs, embeddedStdStructFields, embeddedStdEnumVariants)
		if blob.Config != want {
			fmt.Fprintln(os.Stderr, "[no] ERROR: 內嵌 std AST blob 的「解析配置指紋」與本二進位不符（建構不一致）。")
			fmt.Fprintln(os.Stderr, "[no]   意即 blob 是用與建置路徑不同的 parser 配置烤出來的（例如未注入跨模組簽名表），其 AST 不等於建置路徑會產生的 AST。")
			fmt.Fprintln(os.Stderr, "[no]   修正：執行 `make gen`（或 `make no`）重新產生 blob。")
			os.Exit(1)
		}
	}
	dec := make(map[string][]byte, len(blob.Mods))
	for _, e := range blob.Mods {
		dec[e.Path] = e.Data
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
	return cp.Prog, true
}

// stdParseTables holds the parser inputs that shape a std module's parse.
// Lowering is NOT inference-free: buildMatchDesugar normalises an
// index-expression match subject (`parts[i]: { ok -> ... }`) into a synthetic
// `?elem` binding only when parser.isSafeIndexBase can resolve the base's
// container type, and that type comes from the injected cross-module signature
// tables. Parsing with a bare parser.New (no SetExternSignatures) therefore
// produces a DIFFERENT AST than the build path does for the same source.
type stdParseTables struct {
	FuncSigs     map[string][]string
	MethodSigs   map[string][]string
	StructFields map[string]map[string]string
	EnumVariants map[string][]string
}

// stdParseTablesFor collects the tables a std module body must be parsed with.
//
// CollectStdSigsFromFS is used rather than CollectStdModuleSignatures because
// the latter hard-fails when StdSigKey is unset, and genstdsig runs BEFORE the
// key exists (the Makefile injects it into the binary, not into the generator).
func stdParseTablesFor(fsys fs.FS) (stdParseTables, error) {
	funcEntries, methodEntries, structFields, _, _, enumVariants, err := CollectStdSigsFromFS(fsys)
	if err != nil {
		return stdParseTables{}, fmt.Errorf("collect std signatures: %w", err)
	}
	// The parser's inference tables are RESULT-only (see parser.funcSignatures).
	t := stdParseTables{
		FuncSigs:     make(map[string][]string, len(funcEntries)),
		MethodSigs:   make(map[string][]string, len(methodEntries)),
		StructFields: structFields,
		EnumVariants: enumVariants,
	}
	for k, e := range funcEntries {
		t.FuncSigs[k] = e.Results
	}
	for k, e := range methodEntries {
		t.MethodSigs[k] = e.Results
	}
	return t, nil
}

// newStdModuleParser builds the parser for a std module body with the SAME
// configuration build.parseEmbeddedProgram uses, because StdASTFor replaces
// that call at build time. Keep the two in sync: the parse result (not just its
// diagnostics) depends on Filename, the cross-module signature tables and the
// enum variants.
//
// Shared with the blob/fresh-parse consistency test so a future divergence
// cannot be hidden by the test reproducing the same wrong configuration the
// generator used — which is exactly how the `parts[i]` regression slipped
// through: the old test compared a bare-parsed blob against a bare-parsed
// baseline, so both sides were missing the safe-index normalisation and agreed.
func newStdModuleParser(filename, source string, t stdParseTables) *parser.Parser {
	p := parser.New(lexer.NewCached(filename, source))
	p.Filename = filepath.Base(filename)
	p.SetExternSignatures(t.FuncSigs, t.MethodSigs, t.StructFields)
	if len(t.EnumVariants) > 0 {
		p.SetExternEnumVariants(t.EnumVariants)
	}
	return p
}

// SerializeStdAST parses every std module under fsys and returns a
// map[embedPath][]byte of gob-encoded parser.CachedProgram blobs (the whole
// map is gzipped once by genstdsig), for the generator to bake into the binary.
// No per-module gzip: StdASTFor gob-decodes the inner bytes directly, so a
// second gzip layer would be redundant — and it was a latent bug, because the
// inner gzip was never undone on decode, so the baked blob always failed to
// decode and the build silently fell back to parsing std from source.
//
// Every module goes through newStdModuleParser so the baked AST equals what the
// build path would have parsed. Skipping that configuration used to bake a
// silently different AST, because the staleness key only covers src/std
// content, never the parser configuration. Concretely, std/txt.no's
// `txt.replace-n` was baked with `parts[i]` left as a bare element read, so
// `s = it` bound a `txt` where an option payload was expected and
// `u.replace-n('one', '1', 1)` returned "1one" instead of "1 two one"
// (test/std/txt.no: `replace-n limit 1` / `limit 2`).
func SerializeStdAST(fsys fs.FS) (*StdASTBlob, error) {
	known := ListStdModules(fsys)
	tables, err := stdParseTablesFor(fsys)
	if err != nil {
		return nil, fmt.Errorf("std AST: %w", err)
	}
	blob := &StdASTBlob{Mods: make([]StdASTEntry, 0, len(known))}
	for _, info := range known {
		embedPath := "std/" + info.FullPath + ".no"
		source, err := fs.ReadFile(fsys, embedPath)
		if err != nil {
			return nil, fmt.Errorf("std AST: read %s: %w", embedPath, err)
		}
		p := newStdModuleParser(embedPath, string(source), tables)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			return nil, fmt.Errorf("std AST: parse %s failed: %v", embedPath, p.Errors())
		}
		// Read the configuration back OFF THE PARSER instead of recomputing it
		// from `tables`. `tables` is what we MEANT to inject; the parser holds
		// what was actually injected. Only the latter can detect
		// newStdModuleParser losing its SetExternSignatures call — the exact
		// regression that shipped the txt.replace-n bug. Every module is parsed
		// with the same configuration, so the first one's fingerprint is the
		// blob's.
		if blob.Config == "" {
			blob.Config = p.ExternSignatureFingerprint()
		}
		cp := &parser.CachedProgram{
			Prog:      prog,
			Anns:      parser.SerializeAnns(prog),
			TypeFacts: parser.SerializeTypeFacts(prog),
			NodeFacts: parser.SerializeNodeFacts(prog),
		}
		var raw bytes.Buffer
		if err := gob.NewEncoder(&raw).Encode(cp); err != nil {
			return nil, fmt.Errorf("std AST: encode %s: %w", embedPath, err)
		}
		blob.Mods = append(blob.Mods, StdASTEntry{Path: embedPath, Data: raw.Bytes()})
	}
	// Sorted by path so the encoded blob is byte-reproducible run to run.
	sort.Slice(blob.Mods, func(i, j int) bool { return blob.Mods[i].Path < blob.Mods[j].Path })
	if blob.Config == "" {
		blob.Config = parser.ExternSignatureFingerprint(nil, nil, nil, nil)
	}
	return blob, nil
}
