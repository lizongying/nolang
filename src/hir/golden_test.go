// Package hir_test holds the equivalence tests that prove the HIR-based
// symbol extraction is a drop-in replacement for the checker's parse-based
// one. It is an external test package because it has to import checker and
// parser, both of which depend on hir.
//
// The point of these tests is that they are not toy cases: they run the whole
// std corpus (~119 modules, ~49k lines) through both implementations and
// compare the resulting tables entry by entry. Any semantic drift between
// checker.collectStdSigsFromFS and hir.CollectModuleSignatures fails here with
// the offending key named, which is the only way a swap of this size can be
// made safely.
package hir_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	nolang "github.com/lizongying/nolang"
	"github.com/lizongying/nolang/build/llvm"
	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/lexer"
	pkgutil "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)

// stdModules parses every std module exactly the way
// checker.collectStdSigsFromFS does (same embed path, same cached lexer, same
// parser construction) and lowers each AST to HIR. Modules that fail to parse
// are skipped, mirroring the checker's behaviour.
func stdModules(t *testing.T) []hir.ModulePackage {
	t.Helper()
	infos := checker.GetStdModules()
	if len(infos) < 100 {
		t.Fatalf("only %d std modules found; the corpus is supposed to be ~119, "+
			"so this test would be vacuous", len(infos))
	}

	out := make([]hir.ModulePackage, 0, len(infos))
	for _, info := range infos {
		embedPath := "std/" + info.FullPath + ".no"
		source, err := nolang.StdFS.ReadFile(embedPath)
		if err != nil {
			t.Fatalf("reading %s: %v", embedPath, err)
		}
		p := parser.New(lexer.NewCached(embedPath, string(source)))
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			continue // the checker skips these too
		}
		pkg := parser.ASTToHIR(prog)
		pkg.Name = info.ShortName
		out = append(out, hir.ModulePackage{Short: info.ShortName, Pkg: pkg})
	}
	if len(out) < 100 {
		t.Fatalf("only %d of %d std modules parsed cleanly", len(out), len(infos))
	}
	return out
}

// TestSignatureTablesMatchChecker is the load-bearing test of the HIR
// migration: all six signature tables, produced from HIR slices, must equal
// the ones the checker produces by parsing.
func TestSignatureTablesMatchChecker(t *testing.T) {
	wantFuncs, wantMethods, wantStructs, wantAliases, wantStructMod, wantEnums, err :=
		checker.CollectStdSigsFromFS(nolang.StdFS)
	if err != nil {
		t.Fatalf("checker.CollectStdSigsFromFS: %v", err)
	}

	got := hir.CollectModuleSignatures(stdModules(t))

	// Sanity floors: a silently empty table would make every diff below pass.
	if len(wantFuncs) < 500 {
		t.Fatalf("reference func table has only %d entries; expected 1000+", len(wantFuncs))
	}
	if len(wantStructs) < 50 {
		t.Fatalf("reference struct table has only %d entries; expected 90+", len(wantStructs))
	}

	diffStringSliceMap(t, "Funcs", wantFuncs, got.Funcs)
	diffStringSliceMap(t, "Methods", wantMethods, got.Methods)
	diffStringMap(t, "Aliases", wantAliases, got.Aliases)
	diffStringMap(t, "StructMod", wantStructMod, got.StructMod)
	diffStringSliceMap(t, "Enums", wantEnums, got.Enums)
	diffStructFields(t, wantStructs, got.Structs)

	t.Logf("compared %d funcs, %d methods, %d structs, %d aliases, %d structMod, %d enums",
		len(wantFuncs), len(wantMethods), len(wantStructs),
		len(wantAliases), len(wantStructMod), len(wantEnums))
}

// TestModuleExportsMatchChecker covers the other extraction path: the export
// name list that ValidateUndefinedVars needs. Today the checker recovers it by
// re-lexing and re-parsing every std module on every build; hir.ExportedSymbols
// reads it off the HIR arena instead, and must agree exactly.
func TestModuleExportsMatchChecker(t *testing.T) {
	infos := checker.GetStdModules()

	// checker.parseModuleExports resolves a module name by scanning the module
	// list and taking the first entry whose ShortPath, FullPath or ShortName
	// matches. Only compare modules whose ShortPath resolves back to
	// themselves, so an ambiguous short path is not misread as a mismatch.
	resolves := func(name string) int {
		for i, info := range infos {
			if info.ShortPath == name || info.FullPath == name || info.ShortName == name {
				return i
			}
		}
		return -1
	}

	mods := stdModules(t)
	byShort := map[string][]*hir.Package{}
	for i := range mods {
		byShort[mods[i].Short] = append(byShort[mods[i].Short], mods[i].Pkg)
	}

	var compared, skipped int
	for i, info := range infos {
		if resolves(info.ShortPath) != i {
			skipped++
			continue
		}
		pkgs := byShort[info.ShortName]
		if len(pkgs) != 1 {
			skipped++ // ambiguous short name; not this test's subject
			continue
		}

		want := checker.GetModuleExports([]string{info.ShortPath})
		got := dedupExports(hir.ExportedSymbols(pkgs[0]))

		if len(want) != len(got) {
			t.Errorf("module %s: %d exports from checker, %d from HIR\n  checker: %v\n  hir:     %v",
				info.ShortPath, len(want), len(got), exportNames(want), got)
			continue
		}
		for j := range want {
			if want[j].Name != got[j].Name || want[j].Value != got[j].Value || want[j].Type != got[j].Type {
				t.Errorf("module %s export #%d: checker {%q %q %q} != hir {%q %q %q}",
					info.ShortPath, j,
					want[j].Name, want[j].Value, want[j].Type,
					got[j].Name, got[j].Value, got[j].Type)
			}
		}
		compared++
	}
	if compared < 80 {
		t.Fatalf("only %d modules compared (%d skipped); too few to be meaningful",
			compared, skipped)
	}
	t.Logf("export lists match for %d modules (%d skipped as ambiguously named)", compared, skipped)
}

// TestNoUnknownNodesInStdCorpus is the runtime counterpart to the static
// exhaustiveness tests in the parser package: it proves that lowering the real
// corpus never falls through to the KUnknown placeholder. A static switch can
// be complete while still mishandling a node that only appears in real code.
func TestNoUnknownNodesInStdCorpus(t *testing.T) {
	mods := stdModules(t)

	total := map[hir.Kind]int{}
	var nodes int
	for _, m := range mods {
		for k, n := range m.Pkg.KindCounts() {
			total[k] += n
			nodes += n
		}
		if n := m.Pkg.KindCounts()[hir.KUnknown]; n > 0 {
			// Name the offending AST types: the fallback stores "%T" in S.
			offenders := map[string]int{}
			m.Pkg.WalkTop(func(_ int32, nd *hir.Node) bool {
				if nd.Kind == hir.KUnknown {
					offenders[m.Pkg.Str(nd.S)]++
				}
				return true
			})
			t.Errorf("module %s lowered %d node(s) to KUnknown: %v", m.Short, n, offenders)
		}
	}

	if nodes < 100000 {
		t.Errorf("only %d HIR nodes built from the std corpus; expected 100k+, "+
			"so this test may not be exercising what it claims", nodes)
	}

	// Report which kinds the corpus actually exercises. Kinds at zero are not
	// a failure (some only arise in user code), but the list makes it obvious
	// what the corpus does and does not cover.
	var unused []string
	for k := hir.Kind(1); k < hir.Kind(len(hir.KindNames)); k++ {
		if total[k] == 0 {
			unused = append(unused, k.String())
		}
	}
	sort.Strings(unused)
	t.Logf("%d nodes across %d modules; %d/%d kinds exercised, absent: %v",
		nodes, len(mods), len(hir.KindNames)-1-len(unused), len(hir.KindNames)-1, unused)
}

// userCorpus lowers every .no file under the repository's tests/ tree that
// parses cleanly. std is a deliberately narrow corpus — it contains no imports
// (`#`), no exports (`@`), no map literals and no casts — so coverage measured
// on std alone overstates how much of the lowering switch is untested.
func userCorpus(t *testing.T) []*hir.Package {
	t.Helper()
	root := filepath.Join("..", "..", "tests")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("user corpus not present at %s: %v", root, err)
	}

	var out []*hir.Package
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".no") {
			return nil //nolint:nilerr // unreadable entries are simply skipped
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		p := parser.New(lexer.NewCached(path, string(source)))
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			return nil // several files in tests/ are deliberate error cases
		}
		pkg := parser.ASTToHIR(prog)
		pkg.Name = filepath.Base(path)
		out = append(out, pkg)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if len(out) < 200 {
		t.Fatalf("only %d files in the user corpus parsed cleanly; expected 300+", len(out))
	}
	return out
}

// snippetCorpus lowers the constructs neither file corpus happens to contain.
// std has no imports/exports/casts; tests/ has no byte literals, no `as` casts
// and no anonymous functions. Rather than exempt those kinds as "untested",
// produce them here so the coverage guardrail below stays a real assertion.
func snippetCorpus(t *testing.T) []*hir.Package {
	t.Helper()
	snippets := map[string]string{
		"byte-literal":  "a = x1f\n",
		"pointer-cast":  "p = nil\nq = p as *byte\n",
		"export":        "@ std/math.add\n",
		"export-alias":  "@ std/math.add plus\n",
		"function-lit":  "g((a i64) { a })\n",
		"map-literal":   "m [str]i64 = {\"a\": 1, \"b\": 2}\n",
		"regex-literal": "r = /ab+/i\n",
		"async":         "h = run w()\nr = awy h\n",
		"extern-c":      "#{c}\nputs = (s *byte) (r i32)\n",
	}
	out := make([]*hir.Package, 0, len(snippets))
	for name, src := range snippets {
		p := parser.New(lexer.NewCached(name+".no", src))
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Fatalf("snippet %q no longer parses: %v", name, errs)
		}
		pkg := parser.ASTToHIR(prog)
		pkg.Name = name
		out = append(out, pkg)
	}
	return out
}

// TestKindCoverageAcrossBothCorpora is a guardrail rather than a report. Every
// Kind must either be exercised by one of the three corpora (std, tests/,
// inline snippets) or appear in the exemption list below with a reason. That
// way a kind that is silently never produced cannot sit in the enum pretending
// to be implemented — and a kind that starts being produced makes the list
// visibly stale, because the test fails in that direction too.
func TestKindCoverageAcrossBothCorpora(t *testing.T) {
	// Kinds that cannot appear, by construction rather than by accident.
	exempt := map[hir.Kind]string{
		hir.KUnknown: "the fallback; its absence is the point",
		hir.KProgram: "no node is built for the root; Package.Top is the root list",

		// ASTToHIR stores declared types as interned strings, so the type
		// kinds are only reachable when a type appears in *expression*
		// position. Only NullableType and PointerType implement
		// expressionNode, and no parser path routes one there today: `x as
		// *byte` keeps its target in CastExpression.Type, which is interned.
		hir.TNamed:    "declared types are interned strings, not nodes",
		hir.TArray:    "declared types are interned strings, not nodes",
		hir.TSlice:    "declared types are interned strings, not nodes",
		hir.TMap:      "declared types are interned strings, not nodes",
		hir.TFunc:     "declared types are interned strings, not nodes",
		hir.TUnion:    "declared types are interned strings, not nodes",
		hir.TNullable: "no parser path puts a nullable type in expression position; defensive case",
		hir.TPointer:  "no parser path puts a pointer type in expression position; defensive case",

		// Two AST features exist but cannot reach a parse-time lowering. Both
		// are load-bearing findings for the HIR migration, not curiosities:
		hir.KUnwrapAssign: "`x ?= e` is desugared into if/let/return by lowerProgram " +
			"before ParseProgram returns, so the node never survives to lowering",
		hir.KUnionName: "FuncSignature.GenericUnion is back-filled by the checker " +
			"after parsing (checker.go:679), so it is always empty at lowering time",
	}

	total := map[hir.Kind]int{}
	tally := func(pkgs []*hir.Package) int {
		n := 0
		for _, p := range pkgs {
			for k, c := range p.KindCounts() {
				total[k] += c
				n += c
			}
		}
		return n
	}

	var stdPkgs []*hir.Package
	for _, m := range stdModules(t) {
		stdPkgs = append(stdPkgs, m.Pkg)
	}
	stdNodes := tally(stdPkgs)
	user := userCorpus(t)
	userNodes := tally(user)
	snips := snippetCorpus(t)
	snipNodes := tally(snips)

	var missing []string
	for k := hir.Kind(1); k < hir.Kind(len(hir.KindNames)); k++ {
		if total[k] > 0 {
			if why, ok := exempt[k]; ok {
				t.Errorf("kind %s is exempted (%q) but the corpora produced %d of them; "+
					"remove the exemption", k, why, total[k])
			}
			continue
		}
		if _, ok := exempt[k]; !ok {
			missing = append(missing, k.String())
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d kind(s) never produced by any corpus and not exempted: %v\n"+
			"either add a snippet that produces them or exempt them with a reason",
			len(missing), missing)
	}

	t.Logf("std %d nodes / tests %d files %d nodes / snippets %d nodes; %d of %d kinds exercised, %d exempt",
		stdNodes, len(user), userNodes, snipNodes,
		len(hir.KindNames)-1-len(exempt), len(hir.KindNames)-1, len(exempt))
}

// TestNoUnknownNodesInUserCorpus is the tests/ counterpart of the std check.
func TestNoUnknownNodesInUserCorpus(t *testing.T) {
	for _, pkg := range userCorpus(t) {
		if n := pkg.KindCounts()[hir.KUnknown]; n > 0 {
			offenders := map[string]int{}
			pkg.WalkTop(func(_ int32, nd *hir.Node) bool {
				if nd.Kind == hir.KUnknown {
					offenders[pkg.Str(nd.S)]++
				}
				return true
			})
			t.Errorf("%s lowered %d node(s) to KUnknown: %v", pkg.Name, n, offenders)
		}
	}
}

// TestLoweringIsDeterministic checks that lowering the same source twice yields
// byte-identical trees. Node ids are the key into every side table (TypeMap in
// stage 3, the rewrite maps in lowering), so unstable ids would silently
// corrupt those tables rather than fail loudly.
func TestLoweringIsDeterministic(t *testing.T) {
	infos := checker.GetStdModules()
	var checked, nonEmpty int
	for _, info := range infos {
		path := "std/" + info.FullPath + ".no"
		source, err := nolang.StdFS.ReadFile(path)
		if err != nil {
			continue
		}
		dumps := make([]string, 2)
		stmts, ok := 0, true
		for i := range dumps {
			p := parser.New(lexer.NewCached(path, string(source)))
			prog := p.ParseProgram()
			if len(p.Errors()) > 0 {
				ok = false
				break
			}
			stmts = len(prog.Statements)
			dumps[i] = parser.ASTToHIR(prog).Dump()
		}
		if !ok {
			continue
		}
		if dumps[0] != dumps[1] {
			t.Errorf("module %s lowered to two different trees across runs", info.FullPath)
		}
		// An empty tree is only correct for a module with no statements at
		// all. std ships several documentation-only files (any.no is empty,
		// bool.no and async.no are entirely comments), so emptiness has to be
		// checked against the AST rather than asserted away.
		switch {
		case stmts == 0 && dumps[0] != "":
			t.Errorf("module %s has no statements yet lowered to a non-empty tree", info.FullPath)
		case stmts > 0 && dumps[0] == "":
			t.Errorf("module %s has %d statements but lowered to an empty tree", info.FullPath, stmts)
		}
		checked++
		if stmts > 0 {
			nonEmpty++
		}
	}
	if checked < 100 {
		t.Fatalf("only %d modules checked for determinism; expected ~119", checked)
	}
	if nonEmpty < 90 {
		t.Fatalf("only %d of %d checked modules had any statements; the run would be "+
			"vacuous", nonEmpty, checked)
	}
	t.Logf("%d modules lowered deterministically (%d with statements)", checked, nonEmpty)
}

// TestAnnotationsSurviveLowering pins the annotation side table down.
//
// #{...} entries are not AST statements; the parser files them in
// prog.Sem keyed by AST pointer. HIR is pointer-free, so an early version of
// ASTToHIR simply lost them — and because nothing downstream of parsing looks
// at them until codegen, nothing failed. This test compares, statement by
// statement, the keys the AST side table holds against the ones reachable from
// the HIR arena.
func TestAnnotationsSurviveLowering(t *testing.T) {
	// fs.no and process.no are the modules that actually carry platform
	// annotations; assert the corpus still contains them so the test cannot
	// quietly become vacuous.
	var withAnns, entries int

	for _, info := range checker.GetStdModules() {
		path := "std/" + info.FullPath + ".no"
		source, err := nolang.StdFS.ReadFile(path)
		if err != nil {
			continue
		}
		p := parser.New(lexer.NewCached(path, string(source)))
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			continue
		}
		pkg := parser.ASTToHIR(prog)

		if len(prog.Statements) != len(pkg.Top) {
			t.Fatalf("module %s: %d statements lowered to %d top-level nodes; the "+
				"index-wise comparison below assumes they line up",
				info.FullPath, len(prog.Statements), len(pkg.Top))
		}
		for i, stmt := range prog.Statements {
			want := prog.Sem.AnnotationsOf(stmt)
			gotKeys, gotHasValue := pkg.AnnotationKeys(pkg.Top[i])
			if len(want) != len(gotKeys) {
				t.Errorf("module %s stmt #%d (%T): %d annotations in the AST side table, "+
					"%d reachable from HIR (%v)",
					info.FullPath, i, stmt, len(want), len(gotKeys), gotKeys)
				continue
			}
			for j := range want {
				if want[j].Key != gotKeys[j] {
					t.Errorf("module %s stmt #%d annotation %d: key %q in AST != %q in HIR",
						info.FullPath, i, j, want[j].Key, gotKeys[j])
				}
				// A bare key (#{linux-amd64}) has a nil Value; that is the
				// only thing matchesPlatform looks at, so the distinction has
				// to round-trip.
				if hadValue := want[j].Value != nil; hadValue != gotHasValue[j] {
					t.Errorf("module %s stmt #%d annotation %q: hasValue %v in AST != %v in HIR",
						info.FullPath, i, want[j].Key, hadValue, gotHasValue[j])
				}
			}
			if len(want) > 0 {
				withAnns++
				entries += len(want)
			}
		}
	}

	if withAnns < 20 {
		t.Fatalf("only %d annotated statements found in the std corpus; fs.no alone "+
			"has ~31, so this test is not exercising what it claims", withAnns)
	}
	t.Logf("%d annotated statements, %d entries round-tripped", withAnns, entries)
}

// TestPlatformFilteringMatchesAST is the payoff of the previous test: it runs
// the real AST-based platform filter and an HIR-based reimplementation of the
// same rule over every target triple, and requires them to keep or drop
// exactly the same statements.
//
// This is the check that would have caught the dropped side table as a
// user-visible bug rather than as missing metadata: without annotations in
// HIR, every #{win-amd64} wrapper in std/fs.no survives into a macOS build.
func TestPlatformFilteringMatchesAST(t *testing.T) {
	targets := []struct{ goos, goarch string }{
		{"darwin", "arm64"}, {"darwin", "amd64"},
		{"linux", "amd64"}, {"linux", "arm64"},
		{"windows", "amd64"}, {"windows", "arm64"},
		{"wasip1", "wasm"},
	}

	// hirMatches mirrors build/llvm.matchesPlatform, reading from HIR instead
	// of the AST side table.
	hirMatches := func(pkg *hir.Package, id int32, goos, goarch string) bool {
		keys, hasValue := pkg.AnnotationKeys(id)
		if len(keys) == 0 {
			return true
		}
		hasPlatform := false
		for i, k := range keys {
			if hasValue[i] {
				continue // only bare keys can be platform keys
			}
			m, isPlatform := pkgutil.PlatformKeys[k]
			if !isPlatform {
				continue
			}
			hasPlatform = true
			if goos == m.GOOS && goarch == m.GOARCH {
				return true
			}
		}
		return !hasPlatform
	}

	var dropped int
	for _, info := range checker.GetStdModules() {
		path := "std/" + info.FullPath + ".no"
		source, err := nolang.StdFS.ReadFile(path)
		if err != nil {
			continue
		}
		p := parser.New(lexer.NewCached(path, string(source)))
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			continue
		}
		pkg := parser.ASTToHIR(prog)
		if len(prog.Statements) != len(pkg.Top) {
			t.Fatalf("module %s: statement/top-level count mismatch", info.FullPath)
		}

		for _, tgt := range targets {
			keptAST := llvm.FilterByPlatform(prog.Sem, prog.Statements, tgt.goos, tgt.goarch)
			var keptHIR []int
			for i := range prog.Statements {
				if hirMatches(pkg, pkg.Top[i], tgt.goos, tgt.goarch) {
					keptHIR = append(keptHIR, i)
				}
			}
			if len(keptAST) != len(keptHIR) {
				t.Errorf("module %s on %s/%s: AST filter keeps %d statements, HIR filter keeps %d",
					info.FullPath, tgt.goos, tgt.goarch, len(keptAST), len(keptHIR))
				continue
			}
			for j, idx := range keptHIR {
				if keptAST[j] != prog.Statements[idx] {
					t.Errorf("module %s on %s/%s: kept statement #%d differs between filters",
						info.FullPath, tgt.goos, tgt.goarch, j)
					break
				}
			}
			dropped += len(prog.Statements) - len(keptAST)
		}
	}

	if dropped == 0 {
		t.Fatal("platform filtering dropped nothing across the whole corpus and every " +
			"target, so an HIR that ignores annotations would pass this test")
	}
	t.Logf("%d statement-drops agreed on across %d targets", dropped, len(targets))
}

// TestExtractSignaturesMatchesSingleModuleCollect pins down that the
// single-package entry point agrees with the cross-module one when there is
// only one module, i.e. that qualification is the only difference between them.
func TestExtractSignaturesMatchesSingleModuleCollect(t *testing.T) {
	mods := stdModules(t)
	for _, m := range mods[:min(12, len(mods))] {
		one := hir.CollectModuleSignatures([]hir.ModulePackage{m})
		direct := hir.ExtractSignatures(m.Pkg, m.Short)
		if !reflect.DeepEqual(one.Funcs, direct.Funcs) {
			t.Errorf("module %s: Funcs differ between CollectModuleSignatures and ExtractSignatures", m.Short)
		}
		if !reflect.DeepEqual(one.Methods, direct.Methods) {
			t.Errorf("module %s: Methods differ", m.Short)
		}
		if !reflect.DeepEqual(one.Structs, direct.Structs) {
			t.Errorf("module %s: Structs differ", m.Short)
		}
		if !reflect.DeepEqual(one.Enums, direct.Enums) {
			t.Errorf("module %s: Enums differ", m.Short)
		}
	}
}

// ---- diff helpers ----

// maxReported bounds how many mismatches each diff prints, so a systematic
// break produces a readable failure instead of thousands of lines.
const maxReported = 12

func diffStringSliceMap(t *testing.T, label string, want, got map[string][]string) {
	t.Helper()
	reported := 0
	report := func(format string, args ...any) {
		if reported >= maxReported {
			return
		}
		reported++
		t.Errorf("%s: %s", label, fmt.Sprintf(format, args...))
	}
	for _, k := range sortedKeys(want) {
		g, ok := got[k]
		if !ok {
			report("key %q present in checker output, missing from HIR", k)
			continue
		}
		if !reflect.DeepEqual(want[k], g) {
			// nil vs empty slice is a real difference here: an entry with zero
			// results must stay distinguishable from an absent one.
			report("key %q: checker %#v != hir %#v", k, want[k], g)
		}
	}
	for _, k := range sortedKeys(got) {
		if _, ok := want[k]; !ok {
			report("key %q present in HIR output, missing from checker", k)
		}
	}
	if reported >= maxReported {
		t.Errorf("%s: additional mismatches suppressed (checker %d keys, hir %d keys)",
			label, len(want), len(got))
	}
}

func diffStringMap(t *testing.T, label string, want, got map[string]string) {
	t.Helper()
	reported := 0
	for _, k := range sortedKeys(want) {
		if reported >= maxReported {
			t.Errorf("%s: additional mismatches suppressed", label)
			return
		}
		if g, ok := got[k]; !ok {
			reported++
			t.Errorf("%s: key %q missing from HIR output", label, k)
		} else if g != want[k] {
			reported++
			t.Errorf("%s: key %q: checker %q != hir %q", label, k, want[k], g)
		}
	}
	for _, k := range sortedKeys(got) {
		if _, ok := want[k]; !ok && reported < maxReported {
			reported++
			t.Errorf("%s: key %q present only in HIR output", label, k)
		}
	}
}

func diffStructFields(t *testing.T, want, got map[string]map[string]string) {
	t.Helper()
	reported := 0
	for _, name := range sortedKeys(want) {
		if reported >= maxReported {
			t.Error("Structs: additional mismatches suppressed")
			return
		}
		g, ok := got[name]
		if !ok {
			reported++
			t.Errorf("Structs: %q missing from HIR output", name)
			continue
		}
		if !reflect.DeepEqual(want[name], g) {
			reported++
			t.Errorf("Structs: %q fields differ\n  checker: %v\n  hir:     %v",
				name, sortedPairs(want[name]), sortedPairs(g))
		}
	}
	for _, name := range sortedKeys(got) {
		if _, ok := want[name]; !ok && reported < maxReported {
			reported++
			t.Errorf("Structs: %q present only in HIR output", name)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedPairs(m map[string]string) string {
	var sb strings.Builder
	for i, k := range sortedKeys(m) {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%s=%s", k, m[k])
	}
	return sb.String()
}

// dedupExports applies checker.GetModuleExports's first-wins name dedup so the
// two export lists are compared on equal footing.
func dedupExports(in []hir.Export) []hir.Export {
	seen := make(map[string]bool, len(in))
	out := make([]hir.Export, 0, len(in))
	for _, e := range in {
		if seen[e.Name] {
			continue
		}
		seen[e.Name] = true
		out = append(out, e)
	}
	return out
}

func exportNames(in []checker.ModuleExport) []string {
	out := make([]string, 0, len(in))
	for _, e := range in {
		out = append(out, e.Name)
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
