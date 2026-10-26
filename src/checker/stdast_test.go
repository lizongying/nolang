package checker

import (
	"bytes"
	"compress/gzip"
	"encoding/gob"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	nolang "github.com/lizongying/nolang"
	"github.com/lizongying/nolang/checker/stdastembed"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestStdASTBlobCarriesParseConfig pins the Tier-0 parse-configuration guard.
//
// Parsing is not inference-free: lowering consults the injected cross-module
// tables, so the same std source parses into a DIFFERENT AST depending on the
// configuration. The blob therefore bakes a fingerprint of the configuration
// that produced it (parser.Parser.ExternSignatureFingerprint, read back off the
// parser — not recomputed from the tables the caller meant to inject) and
// loadStdAST compares it against the configuration this binary would use.
//
// Without it the only Tier-0 guard was the src/std content key, which does not
// move when the PARSER CONFIGURATION changes. That is how std/txt.no's
// `txt.replace-n` shipped a wrong AST: SerializeStdAST parsed std with a bare
// parser.New, the key was unaffected, and the divergent AST was used silently.
//
// The test is three-sided on purpose:
//   - the blob must carry a non-empty fingerprint;
//   - a parser configured the way build.parseEmbeddedProgram configures std
//     modules must reproduce it exactly;
//   - a bare parser must fingerprint DIFFERENTLY, otherwise the guard compares
//     two always-equal values and proves nothing.
func TestStdASTBlobCarriesParseConfig(t *testing.T) {
	zr, err := gzip.NewReader(bytes.NewReader(stdastembed.Blob))
	if err != nil {
		t.Fatalf("gunzip baked blob: %v", err)
	}
	defer zr.Close()
	var blob StdASTBlob
	if err := gob.NewDecoder(zr).Decode(&blob); err != nil {
		t.Fatalf("decode baked blob: %v", err)
	}
	if len(blob.Mods) == 0 {
		t.Fatal("baked blob carries no modules")
	}
	if blob.Config == "" {
		t.Fatal("baked blob carries no parse-configuration fingerprint; " +
			"loadStdAST's guard is disarmed (regenerate with `make gen`)")
	}

	want := stdParseConfigFingerprint(embeddedStdFuncSigs, embeddedStdMethodSigs, embeddedStdStructFields, embeddedStdEnumVariants)
	if blob.Config != want {
		t.Fatalf("baked blob's parse-configuration fingerprint is %s but this binary's\n"+
			"signature tables imply %s — the blob was produced with a different parser\n"+
			"configuration than the build path uses; regenerate with `make gen`",
			blob.Config, want)
	}

	// The guard must have teeth: an unconfigured parser (the shape of the bug
	// this fingerprint exists to catch) must NOT fingerprint the same.
	bare := parser.New(lexer.NewCached("bare.no", ""))
	if got := bare.ExternSignatureFingerprint(); got == want {
		t.Fatal("a parser with no injected tables fingerprints identically to the build " +
			"path's configuration — the Tier-0 guard could never fire")
	}

	// And the configuration the blob was baked with is the one std modules are
	// parsed with at build time.
	tables, err := stdParseTablesFor(nolang.StdFS)
	if err != nil {
		t.Fatalf("collect std parse tables: %v", err)
	}
	full := parser.New(lexer.NewCached("full.no", ""))
	full.SetExternSignatures(tables.FuncSigs, tables.MethodSigs, tables.StructFields)
	if len(tables.EnumVariants) > 0 {
		full.SetExternEnumVariants(tables.EnumVariants)
	}
	if got := full.ExternSignatureFingerprint(); got != blob.Config {
		t.Fatalf("a build-path-configured parser fingerprints %s, but the baked blob "+
			"carries %s — the blob is stale; regenerate with `make gen`", got, blob.Config)
	}
}

// buildPathStdParser reproduces the parser configuration build.parseEmbeddedProgram
// uses for a std module body — the call StdASTFor replaces at build time.
//
// It deliberately does NOT call the production newStdModuleParser helper. That
// helper is exactly what the generator must keep using, so sharing it here would
// make the test agree with the generator no matter what the helper does: remove
// the SetExternSignatures call and both sides fall back to a bare parse, the
// comparison passes, and the divergence ships. That is not hypothetical — it is
// how the `txt.replace-n` regression below reached a release. Spelling the
// configuration out independently is what gives these tests their teeth.
func buildPathStdParser(embedPath, src string, tables stdParseTables) *parser.Parser {
	p := parser.New(lexer.NewCached(embedPath, src))
	p.Filename = filepath.Base(embedPath)
	p.SetExternSignatures(tables.FuncSigs, tables.MethodSigs, tables.StructFields)
	if len(tables.EnumVariants) > 0 {
		p.SetExternEnumVariants(tables.EnumVariants)
	}
	return p
}

// indexSubjectMatchKeys 统计程式中「主體為安全索引」的 match 节点数量
// （`parts[i]: { ok -> ... }`）。S1 翻转后，ParseProgram 默认保留 SurfaceMatch，
// safe-index normalisation（产出 __match_subj_* 合成绑定）已移至消费期，blob 不再含
// 这些绑定。故这里改以「保留索引主體 match 节点」作为可观测标记：blob 与
// build-path 解析必须保留相同数量的此类节点。
func indexSubjectMatchKeys(prog *parser.Program) []string {
	n := 0
	seen := make(map[uintptr]bool)
	var rec func(v reflect.Value)
	rec = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Invalid:
			return
		case reflect.Interface:
			if !v.IsNil() {
				rec(v.Elem())
			}
		case reflect.Ptr:
			if v.IsNil() {
				return
			}
			if ptr := v.Pointer(); !seen[ptr] {
				seen[ptr] = true
				if sm, ok := v.Interface().(*parser.SurfaceMatch); ok {
					if _, isIdx := sm.Matched.(*parser.IndexExpression); isIdx {
						n++
					}
				}
				rec(v.Elem())
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				rec(v.Index(i))
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if f := v.Field(i); f.CanInterface() {
					rec(f)
				}
			}
		}
	}
	rec(reflect.ValueOf(prog))
	return []string{fmt.Sprintf("%d", n)}
}

// TestStdASTForDecodes proves Tier 0 actually decodes the baked std AST blob
// (not silently falling back to parsing std from source). It is a regression
// guard for the inner-gzip bug: SerializeStdAST used to gzip each module while
// StdASTFor gob-decoded the inner bytes without gunzipping, so every
// StdASTFor failed and the build always re-lexed+re-parsed src/std.
//
// The blob (stdastembed.Blob) is generated by genstdsig from the same src/std
// walk. StdSigKey only needs to be non-empty to mirror a make-built binary (the
// runtime gate is emptiness, not a content-key match), so loadStdAST's config
// guard runs against the baked signature tables exactly as it does in a real
// build.
func TestStdASTForDecodes(t *testing.T) {
	StdSigKey = "test" // non-empty: mirrors a make-injected build
	SetStdASTBlob(stdastembed.Blob)

	prog, ok := StdASTFor("std/math.no")
	if !ok {
		t.Fatal("StdASTFor returned false; baked std AST was not decoded (Tier 0 broken)")
	}
	if prog == nil {
		t.Fatal("StdASTFor returned a nil program")
	}
	if len(prog.Statements) == 0 {
		t.Fatal("decoded std/math.no program has no statements")
	}
}

// TestStdASTBlobMatchesFreshParse guards the Tier 0 invariant that the baked
// std AST blob must restore its node-keyed side tables onto the SAME nodes a
// fresh parse attaches them to.
//
// Each table is stored as a map[uint32]... keyed by the position of a node
// OCCURRENCE in a deterministic reflection walk (walkProgramNodes); the restore
// helpers replay that walk over the decoded tree. If the walk
// used at bake time (genstdsig) and at restore time (this binary) ever disagree
// — for instance because walkProgramNodes changed but the blob was not re-baked
// — every index after the first divergence points at the wrong node and the
// annotations silently land on unrelated statements.
//
// That is not hypothetical. walkProgramNodes used to deduplicate nodes by
// pointer identity, which encoding/gob breaks (it flattens pointers), so the
// blob's index space was SHORTER than the restore walk's. The consequence was a
// `no vet` false positive: `nolang-overflow-ineffective` reported for perfectly
// valid `#{overflow=wrap}` annotations (e.g. src/std/archive/bzip2.no:108)
// because the annotation had been restored onto the wrong statement.
//
// The baseline parse MUST reproduce build.parseEmbeddedProgram's configuration
// (see buildPathStdParser). Lowering is not inference-free — buildMatchDesugar's
// safe-index normalisation needs the cross-module signature tables — so a bare
// parser.New baseline agrees with a bare-parsed blob while BOTH differ from what
// the build actually compiles. That is precisely how the `txt.replace-n`
// regression shipped: the blob left `parts[i]` as a bare element read, the
// baseline reproduced the same mistake, and the comparison passed. The baseline
// is spelled out independently of the production helper so that removing the
// injection from the generator cannot make this test agree with it again.
//
// The comparison is walk-ORDER agnostic: one local walk is applied to both
// programs, so the test fails only when the blob's restored CONTENT differs —
// which is exactly the invariant. It also fails when the blob is stale because
// src/std was edited without re-running `make gen`.
func TestStdASTBlobMatchesFreshParse(t *testing.T) {
	StdSigKey = "test" // non-empty: mirrors a make-injected build
	SetStdASTBlob(stdastembed.Blob)

	tables, err := stdParseTablesFor(nolang.StdFS)
	if err != nil {
		t.Fatalf("collect std parse tables: %v", err)
	}

	checked := 0
	annotated := 0
	for _, info := range ListStdModules(nolang.StdFS) {
		embedPath := "std/" + info.FullPath + ".no"
		src, err := fs.ReadFile(nolang.StdFS, embedPath)
		if err != nil {
			t.Fatalf("read %s: %v", embedPath, err)
		}
		p := buildPathStdParser(embedPath, string(src), tables)
		fresh := p.ParseProgram()
		if fresh == nil || len(p.Errors()) > 0 {
			t.Fatalf("fresh parse %s failed: %v", embedPath, p.Errors())
		}
		blobProg, ok := StdASTFor(embedPath)
		if !ok {
			t.Fatalf("StdASTFor(%s) returned false; the baked blob is missing this module "+
				"(regenerate with `make gen`)", embedPath)
		}

		before := nodeSemFingerprint(fresh)
		after := nodeSemFingerprint(blobProg)
		checked++
		for _, s := range before {
			if strings.Contains(s, "|") {
				annotated++
				break
			}
		}

		if len(before) != len(after) {
			t.Fatalf("%s: node occurrence count differs between a fresh parse (%d) and "+
				"the baked blob (%d) — the blob is stale; run `make gen`",
				embedPath, len(before), len(after))
		}
		shown := 0
		mismatches := 0
		for i := range before {
			if before[i] != after[i] {
				if shown < 3 {
					t.Errorf("%s: occurrence %d differs\n  fresh=%s\n  blob =%s",
						embedPath, i, before[i], after[i])
					shown++
				}
				mismatches++
			}
		}
		if mismatches > 0 {
			t.Fatalf("%s: %d of %d node/annotation slots were restored onto the wrong "+
				"node — the baked std AST blob is stale; run `make gen`",
				embedPath, mismatches, len(before))
		}
	}

	if checked == 0 {
		t.Fatal("no std modules were checked")
	}
	if annotated == 0 {
		t.Fatal("no annotated std module was checked; the comparison is vacuous")
	}
	t.Logf("baked std AST blob agrees with a fresh parse on %d modules (%d annotated)",
		checked, annotated)
}

// TestStdASTBlobHasSafeIndexNormalisation pins the regression that made
// `no test test/std/txt.no` report
//
//	FAIL: str-eq - replace-n limit 1
//	FAIL: str-eq - replace-n limit 2
//
// only on the Tier-0 blob path (unset NOLANG_STD_SRC): SerializeStdAST parsed
// std with a bare parser.New, so `parts = .split(old)` in std/txt.no resolved to
// no type, parser.isSafeIndexBase reported false, and buildMatchDesugar left
// `parts[i]: { ok -> s = it ... }` as a bare element read. The `ok` arm then
// bound a `txt` where an option payload was expected, and replace-n emitted the
// separator for every part: `u.replace-n('one', '1', 1)` returned "1one"
// instead of "1 two one".
//
// S1 翻转后，ParseProgram 默认保留 SurfaceMatch，safe-index normalisation
// （产出 __match_subj_* 合成绑定）已不再是 AST 形态，而是消费期（desugar / checker /
// codegen）的步骤。故 SerializeStdAST 烘焙的 blob 是 surface 形态，不再含
// __match_subj_* 绑定。本测试改为验证：blob 与 build-path 解析都保留了相同数量
// 的「主體為安全索引」match 节点（`parts[i]: { ... }`）——这是 normalisation 的
// 作用对象，blob 必须原样保留。
//
// The assertion is deliberately two-sided. Comparing the blob against a
// build-path parse alone is not enough: a bare baseline is empty too, so both
// sides agree and the check passes vacuously — the exact false-pass shape that
// let this ship. Requiring the baseline to be NON-empty as well means the test
// can only pass when the index-subject matches are genuinely present, and
// comparing the two sets (rather than hard-coding a line number) keeps it
// robust to edits in src/std/txt.no.
func TestStdASTBlobHasSafeIndexNormalisation(t *testing.T) {
	StdSigKey = "test" // non-empty: mirrors a make-injected build
	SetStdASTBlob(stdastembed.Blob)

	const embedPath = "std/txt.no"
	tables, err := stdParseTablesFor(nolang.StdFS)
	if err != nil {
		t.Fatalf("collect std parse tables: %v", err)
	}
	src, err := fs.ReadFile(nolang.StdFS, embedPath)
	if err != nil {
		t.Fatalf("read %s: %v", embedPath, err)
	}
	p := buildPathStdParser(embedPath, string(src), tables)
	want := indexSubjectMatchKeys(p.ParseProgram())
	if len(want) == 0 {
		t.Fatalf("%s: a build-path parse produced no __match_subj_* bindings at all; "+
			"the safe-index normalisation did not run, so this test would be vacuous", embedPath)
	}

	blobProg, ok := StdASTFor(embedPath)
	if !ok {
		t.Fatalf("StdASTFor(%s) returned false; the baked blob is missing this module "+
			"(regenerate with `make gen`)", embedPath)
	}
	got := indexSubjectMatchKeys(blobProg)

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s: baked blob lost safe-index match-subject bindings\n  build path = %v\n  baked blob = %v\n"+
			"The Tier-0 AST blob must be produced with the same parser configuration as "+
			"build.parseEmbeddedProgram (see SerializeStdAST); regenerate with `make gen`.",
			embedPath, want, got)
	}
	t.Logf("%s: baked blob preserves %d safe-index match-subject bindings (%v)",
		embedPath, len(got), got)
}

// nodeSemFingerprint enumerates node OCCURRENCES (type@line:col, plus the
// annotations attached to each) using the same semantics as
// parser.walkProgramNodes — mirrored locally so the test stays independent of
// the unexported production walk, and so a change to that walk cannot make the
// comparison agree with itself.
//
// 名字沿用 Stage 3 之前「註解住在 nodeSem 裡」的時代；nodeSem 通道已於 Stage 7
// 刪除，AnnTable 亦於 2026-10-26 刪除 —— 這裡讀的是
// `Program.Sem.AnnotationsOf`，它現在直接查節點（`AnnotatedNode`）。跨包改名
// 成本高於收益，維持原名，只在此釘住事實。
func nodeSemFingerprint(prog *parser.Program) []string {
	var out []string
	onPath := make(map[parser.Node]bool)
	var rec func(v reflect.Value)
	rec = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Invalid:
			return
		case reflect.Interface:
			if !v.IsNil() {
				rec(v.Elem())
			}
		case reflect.Ptr:
			if v.IsNil() {
				return
			}
			if v.CanInterface() {
				if n, ok := v.Interface().(parser.Node); ok {
					if onPath[n] {
						return
					}
					onPath[n] = true
					var sb strings.Builder
					fmt.Fprintf(&sb, "%T@%d:%d", n, n.Pos().Line, n.Pos().Column)
					for _, e := range prog.Sem.AnnotationsOf(n) {
						fmt.Fprintf(&sb, "|%s@%d:%d", e.Key, e.Pos().Line, e.Pos().Column)
					}
					out = append(out, sb.String())
					rec(v.Elem())
					delete(onPath, n)
					return
				}
			}
			rec(v.Elem())
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				rec(v.Index(i))
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if f := v.Field(i); f.CanInterface() {
					rec(f)
				}
			}
		}
	}
	for _, s := range prog.Statements {
		rec(reflect.ValueOf(s))
	}
	return out
}
