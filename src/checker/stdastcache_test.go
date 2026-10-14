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
	"strings"
	"testing"

	nolang "github.com/lizongying/nolang"
	"github.com/lizongying/nolang/checker/stdastembed"
	"github.com/lizongying/nolang/lexer"
	pkg "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)

// decodeStdASTBlob undoes the gzip+gob framing, so a test can assert on the
// payload's content rather than on its bytes.
func decodeStdASTBlob(t *testing.T, raw []byte) StdASTBlob {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gunzip blob: %v", err)
	}
	defer zr.Close()
	var blob StdASTBlob
	if err := gob.NewDecoder(zr).Decode(&blob); err != nil {
		t.Fatalf("decode blob: %v", err)
	}
	return blob
}

// TestStdASTCachePath pins the cache location contract: $NOLANG_STD_AST_CACHE
// wins, otherwise <NoHomeDir()>/cache/std_ast.bin (NoHomeDir is documented as
// the base directory for all nolang data — source, cache, bin).
func TestStdASTCachePath(t *testing.T) {
	override := filepath.Join(t.TempDir(), "elsewhere.bin")
	t.Setenv(StdASTCacheEnv, override)
	if got := StdASTCachePath(); got != override {
		t.Errorf("with %s set, StdASTCachePath() = %q, want %q", StdASTCacheEnv, got, override)
	}

	t.Setenv(StdASTCacheEnv, "")
	want := filepath.Join(pkg.NoHomeDir(), "cache", "std_ast.bin")
	if got := StdASTCachePath(); got != want {
		t.Errorf("StdASTCachePath() = %q, want %q", got, want)
	}
}

// TestLoadStdASTCacheMiss covers the two shapes that must read as "no cache":
// the file does not exist, and it exists but is empty.
func TestLoadStdASTCacheMiss(t *testing.T) {
	dir := t.TempDir()

	missing := filepath.Join(dir, "missing.bin")
	t.Setenv(StdASTCacheEnv, missing)
	if raw, ok := LoadStdASTCache(); ok || raw != nil {
		t.Errorf("missing file: LoadStdASTCache() = (%d bytes, %v), want (nil, false)", len(raw), ok)
	}

	empty := filepath.Join(dir, "empty.bin")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatalf("write empty file: %v", err)
	}
	t.Setenv(StdASTCacheEnv, empty)
	if raw, ok := LoadStdASTCache(); ok || raw != nil {
		t.Errorf("empty file: LoadStdASTCache() = (%d bytes, %v), want (nil, false)", len(raw), ok)
	}
}

// TestEnsureStdASTCacheReusesExistingFile is the "只在不存在時生成" half of the
// contract. The sentinel is a VALID but unmistakable payload (a blob carrying no
// modules), so a regenerating implementation is caught even if the regenerated
// bytes would otherwise be similar.
func TestEnsureStdASTCacheReusesExistingFile(t *testing.T) {
	raw, err := encodeStdASTBlob(&StdASTBlob{Config: "sentinel"})
	if err != nil {
		t.Fatalf("encode sentinel blob: %v", err)
	}
	path := filepath.Join(t.TempDir(), "cache", "std_ast.bin")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	t.Setenv(StdASTCacheEnv, path)

	got, err := EnsureStdASTCache()
	if err != nil {
		t.Fatalf("EnsureStdASTCache: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("EnsureStdASTCache regenerated a present cache file (%d bytes returned, %d written)",
			len(got), len(raw))
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Equal(onDisk, raw) {
		t.Fatalf("EnsureStdASTCache rewrote the present cache file (%d bytes on disk, %d before)",
			len(onDisk), len(raw))
	}
}

// TestEnsureStdASTCacheWritesMissingFile is the generation half: with no cache
// file, Ensure must produce a payload that is a usable Tier-0 blob (decodes,
// carries a parse-configuration fingerprint, and answers StdASTFor) and must
// persist it so the next process skips the work.
//
// It drives the explicit-directory core rather than the exported wrapper: the
// wrapper's only extra job is pkg.GetStdSourceDir, whose sync.Once a test cannot
// redirect. The std directory is passed in, and its absence FAILS (not skips) so
// this can never degrade into a vacuous pass.
func TestEnsureStdASTCacheWritesMissingFile(t *testing.T) {
	stdDir, err := filepath.Abs(filepath.Join("..", "std"))
	if err != nil {
		t.Fatalf("resolve ../std: %v", err)
	}
	if fi, err := os.Stat(stdDir); err != nil || !fi.IsDir() {
		t.Fatalf("std source directory %s is not readable (%v); the generation path "+
			"cannot be exercised from here", stdDir, err)
	}

	path := filepath.Join(t.TempDir(), "cache", "std_ast.bin")
	t.Setenv(StdASTCacheEnv, path)
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("precondition: %s already exists", path)
	}

	got, err := ensureStdASTCacheFromDir(stdDir)
	if err != nil {
		t.Fatalf("ensureStdASTCacheFromDir(%s): %v", stdDir, err)
	}
	if len(got) == 0 {
		t.Fatal("ensureStdASTCacheFromDir returned an empty payload")
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cache file was not written: %v", err)
	}
	if !bytes.Equal(onDisk, got) {
		t.Fatalf("persisted payload differs from the returned one (%d vs %d bytes)", len(onDisk), len(got))
	}

	blob := decodeStdASTBlob(t, got)
	if blob.Config == "" {
		t.Error("generated blob carries no parse-configuration fingerprint; " +
			"loadStdAST's guard would be disarmed")
	}
	if len(blob.Mods) < 100 {
		t.Errorf("generated blob carries %d modules; expected the whole std library", len(blob.Mods))
	}
	if want := modulePathSet(decodeStdASTBlob(t, stdastembed.Blob)); len(want) > 0 {
		got := modulePathSet(blob)
		for p := range want {
			if !got[p] {
				t.Errorf("generated blob is missing %s (the build-baked blob has it)", p)
			}
		}
	}

	// And it must actually drive Tier 0: install it and ask for a module.
	StdSigKey = "test" // non-empty: mirrors a make-injected build
	SetStdASTBlob(got)
	prog, ok := StdASTFor("std/math.no")
	if !ok || prog == nil || len(prog.Statements) == 0 {
		t.Fatalf("StdASTFor(\"std/math.no\") = (%v, %v) after installing the generated blob", prog, ok)
	}
}

// TestEnsureStdASTCacheExportedWrapper covers the exported entry point's dir
// resolution end to end. It is skipped — never silently passed — when the std
// sources are not reachable through pkg.GetStdSourceDir, which is the normal
// case for a `go test` binary running from a temp directory with no
// $NOLANG_STD_SRC.
func TestEnsureStdASTCacheExportedWrapper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "std_ast.bin")
	t.Setenv(StdASTCacheEnv, path)

	blob, err := EnsureStdASTCache()
	if err != nil {
		t.Skipf("std sources not reachable via GetStdSourceDir in this environment: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("EnsureStdASTCache returned a payload but did not persist it: %v", statErr)
	}
	if len(decodeStdASTBlob(t, blob).Mods) < 100 {
		t.Errorf("EnsureStdASTCache returned a payload with too few modules")
	}
}

// modulePathSet returns the embed paths a blob carries, as a set.
func modulePathSet(blob StdASTBlob) map[string]bool {
	set := make(map[string]bool, len(blob.Mods))
	for _, e := range blob.Mods {
		set[e.Path] = true
	}
	return set
}

// TestStdASTBlobGeneratorMatchesBakedBlob proves the runtime generator produces
// a payload SEMANTICALLY identical to the one the build bakes into the binary —
// which is what makes the cache file a legitimate substitute for
// stdastembed.Blob rather than merely a similar-looking blob. Both sides derive
// from the same embedded std sources (nolang.StdFS), so this is immune to a
// concurrent session editing src/std on disk.
//
// 🔴 The comparison is deliberately NOT byte-level. gob's wire output for a
// CachedProgram depends on process-global encoder state: encoding ANY
// StdASTBlob earlier in the same process makes every module's stream ~0.3%
// larger (measured: 2 605 108 vs 2 603 484 bytes for the whole payload) while
// the DECODED programs stay identical (0 of 118 modules differ in content). A
// byte assertion therefore tests the order the tests happened to run in, not
// the invariant. The invariant is: same module set, same parse-configuration
// fingerprint, and — per module — the same restored Program.
//
// The per-module comparison goes through the production path (SetStdASTBlob +
// StdASTFor), so it also proves the generated payload survives loadStdAST's
// fingerprint guard.
func TestStdASTBlobGeneratorMatchesBakedBlob(t *testing.T) {
	genBlob, err := SerializeStdAST(nolang.StdFS)
	if err != nil {
		t.Fatalf("SerializeStdAST(StdFS): %v", err)
	}
	genBytes, err := encodeStdASTBlob(genBlob)
	if err != nil {
		t.Fatalf("encodeStdASTBlob: %v", err)
	}
	bakedBlob := decodeStdASTBlob(t, stdastembed.Blob)

	if genBlob.Config != bakedBlob.Config {
		t.Fatalf("parse-configuration fingerprint differs: generated %q, baked %q",
			genBlob.Config, bakedBlob.Config)
	}
	bakedSet := modulePathSet(bakedBlob)
	if len(bakedSet) == 0 {
		t.Fatal("the baked blob carries no modules; the comparison would be vacuous")
	}
	genSet := modulePathSet(*genBlob)
	for p := range bakedSet {
		if !genSet[p] {
			t.Errorf("generated blob is missing %s", p)
		}
	}
	if len(genSet) != len(bakedSet) {
		t.Errorf("generated blob has %d modules, baked has %d", len(genSet), len(bakedSet))
	}

	paths := make([]string, 0, len(genSet))
	for p := range genSet {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	StdSigKey = "test" // non-empty: mirrors a make-injected build
	fingerprint := func(blob []byte) map[string]string {
		t.Helper()
		SetStdASTBlob(blob)
		out := make(map[string]string, len(paths))
		for _, p := range paths {
			prog, ok := StdASTFor(p)
			if !ok {
				t.Fatalf("StdASTFor(%s) failed after installing the blob", p)
			}
			out[p] = strings.Join(programFingerprint(prog), "\n")
		}
		return out
	}

	want := fingerprint(stdastembed.Blob)
	got := fingerprint(genBytes)

	mismatch := 0
	for _, p := range paths {
		if got[p] == want[p] {
			continue
		}
		mismatch++
		if mismatch <= 3 {
			t.Errorf("%s: the generated blob's program differs from the baked one\n  baked:\n%s\n  generated:\n%s",
				p, want[p], got[p])
		}
	}
	if mismatch > 0 {
		t.Fatalf("%d of %d std modules differ between the generated and the baked blob",
			mismatch, len(paths))
	}
	t.Logf("generated blob matches the baked one on %d modules (config %s)",
		len(paths), genBlob.Config[:12])
}

// TestProgramFingerprintDiscriminates proves programFingerprint can tell two
// parses apart, so TestStdASTBlobGeneratorMatchesBakedBlob is not comparing two
// always-equal values.
//
// The two inputs are the real regression, not a synthetic one: the SAME std
// source parsed with the build-path configuration (which injects the
// cross-module signature tables) versus a BARE parser — the configuration the
// pre-fix generator used, and the reason std/txt.no's `txt.replace-n` shipped a
// wrong AST.
func TestProgramFingerprintDiscriminates(t *testing.T) {
	const embedPath = "std/txt.no"
	tables, err := stdParseTablesFor(nolang.StdFS)
	if err != nil {
		t.Fatalf("collect std parse tables: %v", err)
	}
	src, err := fs.ReadFile(nolang.StdFS, embedPath)
	if err != nil {
		t.Fatalf("read %s: %v", embedPath, err)
	}

	configured := strings.Join(programFingerprint(buildPathStdParser(embedPath, string(src), tables).ParseProgram()), "\n")

	bare := parser.New(lexer.NewCached(embedPath, string(src)))
	bareProg := bare.ParseProgram()
	if len(bare.Errors()) > 0 {
		t.Fatalf("bare parse %s failed: %v", embedPath, bare.Errors())
	}
	got := strings.Join(programFingerprint(bareProg), "\n")

	if got == configured {
		t.Fatal("a bare-parsed std module fingerprints identically to a build-path-parsed one — " +
			"programFingerprint cannot discriminate, so the blob comparison it backs is vacuous")
	}
}

// programFingerprint is a content fingerprint of a decoded std Program that does
// not go through gob: the node-occurrence walk (types, positions, attached
// annotations) plus every SemanticContext side-table and the builtin-name set.
// The side-tables matter — lowering consumes them, so a blob that dropped one
// would still fingerprint equal on the AST alone.
func programFingerprint(prog *parser.Program) []string {
	out := append([]string{}, nodeSemFingerprint(prog)...)
	if prog == nil {
		return append(out, "<nil program>")
	}
	out = append(out, "warnings="+strings.Join(prog.Warnings, "\x1f"))
	names := make([]string, 0, len(prog.BuiltinFuncNames))
	for k := range prog.BuiltinFuncNames {
		names = append(names, k)
	}
	sort.Strings(names)
	out = append(out, "builtinFuncNames="+strings.Join(names, ","))
	if prog.Sem == nil {
		return append(out, "sem=<nil>")
	}
	sem := prog.Sem
	out = append(out, "varTypes="+joinSorted(sem.VarTypes))
	out = append(out, "enumVariants="+joinSortedStrs(sem.EnumVariants))
	out = append(out, "declaredVars="+joinSortedBool(sem.DeclaredVars))
	out = append(out, "ifaceImpls="+joinSortedStrs(sem.IfaceImpls))
	out = append(out, "enumVariantPayload="+joinSortedMapStr(sem.EnumVariantPayload))
	out = append(out, "enumVariantFields="+joinSortedMapStrs(sem.EnumVariantFields))
	out = append(out, "funcVarTypes="+joinSortedMapStr(sem.FuncVarTypes))
	out = append(out, "funcDeclaredVars="+joinSortedMapBool(sem.FuncDeclaredVars))
	out = append(out, "idxLocalTypes="+joinSortedMapStr(sem.IdxLocalTypes))
	return out
}

func joinSorted(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k+"="+m[k])
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func joinSortedStrs(m map[string][]string) string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		keys = append(keys, k+"="+strings.Join(v, "\x1f"))
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func joinSortedBool(m map[string]bool) string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		keys = append(keys, fmt.Sprintf("%s=%v", k, v))
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func joinSortedMapStr(m map[string]map[string]string) string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		keys = append(keys, k+"="+joinSorted(v))
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func joinSortedMapStrs(m map[string]map[string][]string) string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		keys = append(keys, k+"="+joinSortedStrs(v))
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func joinSortedMapBool(m map[string]map[string]bool) string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		keys = append(keys, k+"="+joinSortedBool(v))
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// TestStdDirFSMapsEmbedPaths pins the fs.FS wrapper's path mapping, including
// the case it exists for: a std directory that is NOT literally named "std", so
// os.DirFS(filepath.Dir(dir)) would resolve "std/x.no" to the wrong file (or to
// nothing).
func TestStdDirFSMapsEmbedPaths(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "stdlib-v2") // deliberately not "std"
	if err := os.MkdirAll(filepath.Join(dir, "net"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "math.no"), []byte("a = 1\n"), 0o644); err != nil {
		t.Fatalf("write math.no: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "net", "tls.no"), []byte("b = 2\n"), 0o644); err != nil {
		t.Fatalf("write net/tls.no: %v", err)
	}

	fsys := stdDirFS{root: dir}
	if b, err := fs.ReadFile(fsys, "std/math.no"); err != nil || string(b) != "a = 1\n" {
		t.Errorf("std/math.no = (%q, %v), want (\"a = 1\\n\", nil)", b, err)
	}
	if b, err := fs.ReadFile(fsys, "std/net/tls.no"); err != nil || string(b) != "b = 2\n" {
		t.Errorf("std/net/tls.no = (%q, %v), want (\"b = 2\\n\", nil)", b, err)
	}
	entries, err := fs.ReadDir(fsys, "std")
	if err != nil {
		t.Fatalf("read std: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("std listing has %d entries, want 2", len(entries))
	}
	if _, err := fs.ReadFile(fsys, "js/console-log.no"); err == nil {
		t.Error("a non-std path must not resolve")
	}
	if _, err := fs.ReadFile(fsys, "stdlib-v2/math.no"); err == nil {
		t.Error("a path that leaks the real directory name must not resolve")
	}
}
