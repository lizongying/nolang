package checker

import (
	"bytes"
	"compress/gzip"
	"encoding/gob"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	pkg "github.com/lizongying/nolang/package"
)

// On-disk std AST cache (the file form of the Tier-0 blob).
//
// The `no` binary carries the std AST embedded (checker/stdastembed) and hands
// it to SetStdASTBlob at startup. A consumer that does NOT link that package has
// no Tier-0 blob at all — today that is the language server (cmd/lsp), which
// would otherwise re-lex+re-parse every std module with a BARE parser
// (see src/lsp/documents.go). That is not a cosmetic difference: parsing is not
// inference-free, so a bare parser produces a DIFFERENT AST than the build path
// does for the same source (see the StdASTBlob doc comment for the shipped
// `txt.replace-n` bug that came from exactly that divergence).
//
// Rather than making every consumer embed its own ~2.6 MB copy, the LSP reads
// the same payload from this cache file and materialises it on first use by
// serialising the std sources with SerializeStdAST — the function genstdsig
// itself uses, so the file is the same payload the `no` binary embeds.
//
// Layout: $NOLANG_STD_AST_CACHE, default <NoHomeDir()>/cache/std_ast.bin
// (i.e. ~/no/cache/std_ast.bin — NoHomeDir is documented as the base directory
// for all nolang data: source, cache, bin).
//
// Regeneration policy: the file is written only when it is missing, empty or
// unreadable. A STALE file is not detected here — the Tier-0 staleness guards
// (StdASTBlob.Config's parse-configuration fingerprint, plus ASTVersion folded
// into parser.ExternSignatureFingerprint) still run at load time, and the
// remedy is to delete the file. Detecting staleness would need the src/std
// content key, which is only meaningful for the binary's own embedded StdFS.

// StdASTCacheEnv overrides the std AST cache file path. It exists so tests (and
// a user with several installations) can keep the file out of $HOME.
const StdASTCacheEnv = "NOLANG_STD_AST_CACHE"

// StdASTCachePath returns the std AST cache file path.
func StdASTCachePath() string {
	if p := os.Getenv(StdASTCacheEnv); p != "" {
		return p
	}
	return filepath.Join(pkg.NoHomeDir(), "cache", "std_ast.bin")
}

// LoadStdASTCache reads the cache file, reporting false when it is absent,
// empty or unreadable. The bytes are ready for SetStdASTBlob.
func LoadStdASTCache() ([]byte, bool) {
	raw, err := os.ReadFile(StdASTCachePath())
	if err != nil || len(raw) == 0 {
		return nil, false
	}
	return raw, true
}

// EnsureStdASTCache returns the Tier-0 blob for this process, materialising the
// cache file from the std sources resolved by pkg.GetStdSourceDir when it does
// not exist yet.
//
// It never fails on a cache WRITE problem: the payload is already in memory and
// is strictly better than the caller's fallback (parse std from source), so a
// write failure only costs the next process a regeneration. Only a failure to
// BUILD the payload (std sources unreachable, a module that does not parse) is
// reported — the caller then keeps its previous behaviour.
func EnsureStdASTCache() ([]byte, error) {
	stdDir, _ := pkg.GetStdSourceDir()
	return ensureStdASTCacheFromDir(stdDir)
}

// ensureStdASTCacheFromDir is EnsureStdASTCache with an explicit std source
// directory. It is the testable core: the exported wrapper's only extra job is
// the process-wide GetStdSourceDir resolution, which a test cannot redirect
// (its sync.Once may already have been primed by another test).
func ensureStdASTCacheFromDir(stdDir string) ([]byte, error) {
	if raw, ok := LoadStdASTCache(); ok {
		return raw, nil
	}
	raw, err := stdASTBlobForDir(stdDir)
	if err != nil {
		return nil, err
	}
	writeStdASTCache(raw)
	return raw, nil
}

// writeStdASTCache persists the blob, best-effort. The write goes to a
// per-process temp file and is renamed into place, so a concurrent LSP (or one
// killed mid-write) can never leave a half-written blob for the next reader:
// a reader sees either no file or a complete one.
func writeStdASTCache(raw []byte) {
	path := StdASTCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
	}
}

// stdASTBlobForDir builds the Tier-0 payload (gzip of gob of StdASTBlob) from
// the std sources under stdDir, in exactly the format genstdsig bakes into
// stdastembed/std_ast.bin. Taking the directory as a parameter keeps the
// generation path testable without depending on GetStdSourceDir's process-wide
// sync.Once resolution.
func stdASTBlobForDir(stdDir string) ([]byte, error) {
	if stdDir == "" {
		return nil, fmt.Errorf("std AST cache: std source directory is not resolvable")
	}
	if fi, err := os.Stat(stdDir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("std AST cache: std source directory %s is not readable", stdDir)
	}
	blob, err := SerializeStdAST(stdDirFS{root: stdDir})
	if err != nil {
		return nil, err
	}
	return encodeStdASTBlob(blob)
}

// encodeStdASTBlob gzips and gob-encodes a StdASTBlob — the exact framing
// genstdsig writes to stdastembed/std_ast.bin and loadStdAST reads back. Shared
// so the runtime-generated cache file and the build-baked blob cannot drift
// apart in their container format.
func encodeStdASTBlob(blob *StdASTBlob) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := gob.NewEncoder(zw).Encode(blob); err != nil {
		return nil, fmt.Errorf("std AST cache: encode: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("std AST cache: gzip: %w", err)
	}
	return buf.Bytes(), nil
}

// stdDirFS presents a std source directory as an fs.FS rooted one level ABOVE
// it, so the "std/<FullPath>.no" paths ListStdModules / SerializeStdAST /
// CollectStdSigsFromFS use resolve against it.
//
// A wrapper rather than os.DirFS(filepath.Dir(stdDir)) because $NOLANG_STD_SRC
// may point at a directory that is not literally named "std"; deriving the root
// from the parent would then map "std/x.no" onto the wrong file (or nothing).
type stdDirFS struct{ root string }

func (f stdDirFS) Open(name string) (fs.File, error) {
	if name != "std" && !strings.HasPrefix(name, "std/") {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	rel := strings.TrimPrefix(name[3:], "/") // "" for "std", "x.no" for "std/x.no"
	if rel == "" {
		rel = "."
	}
	return os.DirFS(f.root).Open(rel)
}
