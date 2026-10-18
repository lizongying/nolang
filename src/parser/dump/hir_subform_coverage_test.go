package dump

// ─────────────────────────────────────────────────────────────────────────────
// S1 design doc §3: per-sub-form coverage of the match desugar.
//
// The corpus-level gate (hir_equivalence_test.go) proves that for every program
// that carries a *SurfaceMatch into H0, H0's desugar is byte-identical to the
// parser's own expansion. That is complete for whatever the corpus CONTAINS —
// but it is blind to a sub-form the corpus simply never writes. This file closes
// that half: it classifies every match in the corpus by sub-form and requires
// each of P1–P6 to be present, so a sub-form going untested fails here by name
// instead of passing unnoticed.
//
// Classification reads MatchArm's exported fields directly (Condition,
// IsWildcard, IsRawCond, IsTypeArm, SkipItBinding, BindingName, Multi*), not the
// dump text — the fields are public API, so there is no reason to sniff strings.
//
// ⚠️ Correction recorded (2026-10-18): while building this census I first
// concluded that P3 (range-pattern arms) had NO corpus fixture, based on
// `grep -E '^\s*\[.*\)\s*->'` returning nothing. That grep was wrong: macOS BSD
// grep's ERE does not support `\s`, so the pattern matched nothing at all. The
// census itself found the truth — P3 is covered by tests/fmt-heuristic.no
// ([0..60)/[60..80)/[80..90)) and tests/str-order.no ([1..10), ["a".."z")).
// tests/match-range-pattern.no was still added, but as REINFORCEMENT (range
// match inside a value-returning function, 4 arms incl. default), not to close a
// hole. This is the second time a grep-based survey has been wrong in this
// project: prefer the structural census over shell greps.
// ─────────────────────────────────────────────────────────────────────────────

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// s1Subforms are the six match sub-forms listed in the design doc §3, in the
// order the doc lists them.
var s1Subforms = []struct {
	id   string
	name string
}{
	{"P1", "值/通配 match（value / wildcard arms）"},
	{"P2", "option match（ok / nil / err arms）"},
	{"P3", "range match（[a..b) 型別臂）"},
	{"P4", "raw-cond / 安全索引合成 match"},
	{"P5", "型別解構 match（union 靜態派發）"},
	{"P6", "巢狀 / 獨立 guard match"},
}

// collectSurfaceMatches returns every *parser.SurfaceMatch reachable from prog,
// deduped by pointer. A node reachable from two parents must be reported once.
func collectSurfaceMatches(prog *parser.Program) []*parser.SurfaceMatch {
	if prog == nil {
		return nil
	}
	seen := map[uintptr]bool{}
	var out []*parser.SurfaceMatch
	var walk func(v reflect.Value, depth int)
	walk = func(v reflect.Value, depth int) {
		if !v.IsValid() || depth > 400 {
			return
		}
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() {
				return
			}
			addr := v.Pointer()
			if seen[addr] {
				return
			}
			seen[addr] = true
			// CanInterface guard: some nested AST nodes carry unexported pointer
			// fields, and reflect panics on Interface() for a value obtained
			// through one. countSurfaceMatches avoids this by reading
			// v.Elem().Type().Name() instead; we need the real pointer, so we
			// skip the (rare) case where it is unreachable — SurfaceMatch is only
			// ever stored in exported Expression/[]Statement fields anyway.
			if v.CanInterface() {
				if sm, ok := v.Interface().(*parser.SurfaceMatch); ok {
					out = append(out, sm)
				}
			}
			walk(v.Elem(), depth+1)
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem(), depth+1)
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), depth+1)
			}
		case reflect.Struct:
			isProgram := v.Type() == programReflectType
			for i := 0; i < v.NumField(); i++ {
				if isProgram {
					// Side tables are not part of the node tree; Sem in particular
					// is keyed by Node and would dominate the walk. Same carve-out
					// as countSurfaceMatches.
					switch v.Type().Field(i).Name {
					case "Sem", "Warnings", "BuiltinFuncNames":
						continue
					}
				}
				walk(v.Field(i), depth+1)
			}
		case reflect.Map:
			it := v.MapRange()
			for it.Next() {
				walk(it.Value(), depth+1)
			}
		}
	}
	walk(reflect.ValueOf(prog), 0)
	return out
}

// classifyMatchArm reports which sub-forms a single arm exhibits. It errs on the
// side of the NARROWER classification: a value arm is P1, and only IsTypeArm /
// an explicit union type name is P5, so a file is only credited with P5 when it
// really writes a typed arm.
func classifyMatchArm(a parser.MatchArm, out map[string]bool) {
	if a.IsWildcard {
		out["P1"] = true
	}
	if a.IsRawCond {
		// ok(cond) — the condition is a full boolean expression used directly.
		out["P4"] = true
	}
	if a.SkipItBinding {
		// ⚠️ DEAD IN THIS CENSUS — measured 2026-10-18, do not "fix" by deleting
		// without re-measuring: 535 programs / 1818 arms in the sugar-shape
		// census carry ZERO SkipItBinding arms.
		//
		// Why: every `SkipItBinding: true` arm is synthesised by a lowering pass
		// (maybeCaptureOptionAssign / maybeIndexOutAssign / lowerUnwrapAssign /
		// makePropagateGuard / desugarIndexOutReads — parser/lowering.go:1212,
		// 1469, 3719, 4272-4332, 4485, 4785) and is consumed **immediately** by
		// buildMatchDesugar at the same site: the SurfaceMatch that carries it
		// never reaches any tree. The census runs with SkipLowering=true, so
		// those passes do not even execute; the default path runs them but the
		// arms are transient inside lowerProgram. TestS1SubformsAreCovered
		// asserts the count stays zero, so if a future pass starts LEAVING such
		// an arm in a tree this branch stops being dead and that test fails.
		out["P4"] = true
	}
	if a.IsTypeArm {
		out["P5"] = true
	}
	switch c := a.Condition.(type) {
	case *parser.NilLiteral:
		out["P2"] = true
	case *parser.CallExpression:
		if id, ok := c.Function.(*parser.Identifier); ok {
			switch id.Value {
			case "ok", "err":
				out["P2"] = true
				if a.BindingName != "" {
					// ok(v) / err(e) destructuring — the binding the checker now
					// declares natively (checker/sugar_walk.go matchArmBindings).
					out["P2"] = true
				}
			}
		}
	case *parser.RangeExpression:
		out["P3"] = true
	case *parser.Identifier:
		switch c.Value {
		case "ok", "err", "nil":
			out["P2"] = true
		default:
			out["P1"] = true
		}
	default:
		out["P1"] = true
	}
	if len(a.MultiValuePatterns) > 0 || len(a.MultiOptionPatterns) > 0 {
		out["P1"] = true
	}
}

// classifyMatch folds a whole SurfaceMatch (arms + nesting) into sub-forms.
func classifyMatch(sm *parser.SurfaceMatch, out map[string]bool) {
	if sm == nil {
		return
	}
	for _, a := range sm.Arms {
		classifyMatchArm(a, out)
		if armBodyHasNestedMatch(a) {
			out["P6"] = true
		}
	}
}

// armBodyHasNestedMatch reports whether an arm's body contains another match —
// the nesting case design doc §3 P6 is about.
func armBodyHasNestedMatch(a parser.MatchArm) bool {
	if a.Body == nil {
		return false
	}
	for _, sm := range collectSurfaceMatchesFrom(a.Body) {
		// collectSurfaceMatchesFrom only looks INSIDE the body, so anything it
		// finds is by definition nested.
		_ = sm
		return true
	}
	return false
}

// collectSurfaceMatchesFrom is collectSurfaceMatches rooted at an arbitrary node
// rather than a whole program.
func collectSurfaceMatchesFrom(root any) []*parser.SurfaceMatch {
	seen := map[uintptr]bool{}
	var out []*parser.SurfaceMatch
	var walk func(v reflect.Value, depth int)
	walk = func(v reflect.Value, depth int) {
		if !v.IsValid() || depth > 400 {
			return
		}
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() {
				return
			}
			addr := v.Pointer()
			if seen[addr] {
				return
			}
			seen[addr] = true
			// CanInterface guard: some nested AST nodes carry unexported pointer
			// fields, and reflect panics on Interface() for a value obtained
			// through one. countSurfaceMatches avoids this by reading
			// v.Elem().Type().Name() instead; we need the real pointer, so we
			// skip the (rare) case where it is unreachable — SurfaceMatch is only
			// ever stored in exported Expression/[]Statement fields anyway.
			if v.CanInterface() {
				if sm, ok := v.Interface().(*parser.SurfaceMatch); ok {
					out = append(out, sm)
				}
			}
			walk(v.Elem(), depth+1)
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem(), depth+1)
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), depth+1)
			}
		case reflect.Struct:
			isProgram := v.Type() == programReflectType
			for i := 0; i < v.NumField(); i++ {
				if isProgram {
					switch v.Type().Field(i).Name {
					case "Sem", "Warnings", "BuiltinFuncNames":
						continue
					}
				}
				walk(v.Field(i), depth+1)
			}
		case reflect.Map:
			it := v.MapRange()
			for it.Next() {
				walk(it.Value(), depth+1)
			}
		}
	}
	walk(reflect.ValueOf(root), 0)
	return out
}

// TestS1SubformsAreCovered is the coverage half of the design doc §3 promise.
//
// It walks the whole corpus in the SUGAR shape (so matches stay SurfaceMatch),
// classifies every match, and requires each sub-form to appear in at least one
// parseable program. A sub-form with no fixture fails here with its id, which is
// how the missing P3 fixture was found in the first place.
func TestS1SubformsAreCovered(t *testing.T) {
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("no corpus at %s: %v", corpusRoot, err)
	}

	evidence := map[string][]string{} // sub-form -> files
	var programs, matches, arms, skipItBindingArms int

	walkErr := filepath.WalkDir(corpusRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".no") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		p := parser.New(lexer.NewCached(path, string(raw)))
		p.Filename = path
		// Sugar shape: keep SurfaceMatch so the arms are classifiable.
		p.SkipLowering = true
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			return nil // deliberate error cases classify nothing
		}
		programs++

		forms := map[string]bool{}
		sms := collectSurfaceMatches(prog)
		if len(sms) == 0 {
			return nil
		}
		matches += len(sms)
		for _, sm := range sms {
			for _, a := range sm.Arms {
				arms++
				// classifyMatchArm's SkipItBinding branch is dead in this census
				// (see the comment there). Measure it so the claim cannot rot:
				// a lowering pass that starts LEAVING such an arm in a tree
				// changes what P4 means and must be reviewed.
				if a.SkipItBinding {
					skipItBindingArms++
				}
			}
			classifyMatch(sm, forms)
		}
		rel, _ := filepath.Rel(corpusRoot, path)
		for id := range forms {
			if len(evidence[id]) < 3 {
				evidence[id] = append(evidence[id], rel)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %s: %v", corpusRoot, walkErr)
	}

	if programs < 100 {
		t.Fatalf("only %d programs parsed; the census would be vacuous", programs)
	}
	if matches == 0 {
		t.Fatal("no SurfaceMatch found anywhere in the corpus; the census is broken")
	}
	if skipItBindingArms != 0 {
		t.Errorf("%d arm(s) carry SkipItBinding=true in the sugar-shape census. "+
			"Every SkipItBinding synthesis in parser/lowering.go is consumed "+
			"immediately by buildMatchDesugar, so this was measured as 0 on "+
			"2026-10-18. A non-zero count means a lowering pass now leaves such "+
			"an arm in a surviving tree: revisit classifyMatchArm's SkipItBinding "+
			"branch (currently dead code) before trusting the P4 tally.",
			skipItBindingArms)
	}
	t.Logf("census: %d programs, %d matches, %d arms, %d SkipItBinding arms",
		programs, matches, arms, skipItBindingArms)

	var missing []string
	for _, sf := range s1Subforms {
		files, ok := evidence[sf.id]
		if !ok || len(files) == 0 {
			missing = append(missing, fmt.Sprintf("%s %s", sf.id, sf.name))
			continue
		}
		sort.Strings(files)
		t.Logf("%s %s: %d+ file(s), e.g. %v", sf.id, sf.name, len(files), files)
	}
	if len(missing) > 0 {
		t.Errorf("match sub-form(s) with NO fixture in the corpus — design doc §3 "+
			"treats these as separate phases, so an untested sub-form is a silent "+
			"gap: %v", missing)
	}
}

// TestS1RangePatternFixtureIsExercised pins P3 coverage from BOTH directions:
// the pre-existing fixtures the census found (fmt-heuristic.no / str-order.no)
// and the reinforcement fixture added with this gate. Deleting any one of them
// fails here, so P3 cannot silently regress to zero coverage.
func TestS1RangePatternFixtureIsExercised(t *testing.T) {
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("no corpus at %s: %v", corpusRoot, err)
	}
	for _, rel := range []string{
		"fmt-heuristic.no",       // [0..60) / [60..80) / [80..90) -> char grades
		"str-order.no",           // [1..10) and ["a".."z") -> string ranges
		"match-range-pattern.no", // reinforcement: range match in a value-returning fn
	} {
		path := filepath.Join(corpusRoot, rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("P3 fixture %s is missing: %v", rel, err)
			continue
		}
		p := parser.New(lexer.NewCached(path, string(raw)))
		p.Filename = path
		p.SkipLowering = true
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Errorf("P3 fixture %s no longer parses: %v", rel, errs)
			continue
		}
		forms := map[string]bool{}
		sms := collectSurfaceMatches(prog)
		if len(sms) == 0 {
			t.Errorf("P3 fixture %s carries no match, so it exercises nothing", rel)
			continue
		}
		for _, sm := range sms {
			classifyMatch(sm, forms)
		}
		if !forms["P3"] {
			t.Errorf("%s is no longer classified as a range match (P3); either the "+
				"fixture lost its [a..b) arms or the classifier stopped recognising "+
				"them. forms=%v", rel, keysOfBool(forms))
		}
	}
}

func keysOfBool(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// TestS1RawCondFixturesAreExercised pins P4 from BOTH directions: the
// pre-existing minimal fixture (minimal-ok.no — one ok(cond) arm plus a
// default) and the reinforcement fixture added with this gate
// (match-raw-cond.no — a raw-cond chain, compound boolean conditions, a
// raw-cond match nested inside another match's arm, and the ?= propagation
// source shape whose synthetic SkipItBinding arm never survives lowering).
// Deleting or defanging any one of them fails here, so P4 cannot silently
// regress to zero coverage.
func TestS1RawCondFixturesAreExercised(t *testing.T) {
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("no corpus at %s: %v", corpusRoot, err)
	}
	for _, rel := range []string{
		"minimal-ok.no",     // single ok(it > 127) + default arm
		"match-raw-cond.no", // ok(cond) chain / compound cond / nested / ?= source shape
	} {
		path := filepath.Join(corpusRoot, rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("P4 fixture %s is missing: %v", rel, err)
			continue
		}
		p := parser.New(lexer.NewCached(path, string(raw)))
		p.Filename = path
		p.SkipLowering = true
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Errorf("P4 fixture %s no longer parses: %v", rel, errs)
			continue
		}
		forms := map[string]bool{}
		rawCondArms := 0
		for _, sm := range collectSurfaceMatches(prog) {
			for i := range sm.Arms {
				if sm.Arms[i].IsRawCond {
					rawCondArms++
				}
			}
			classifyMatch(sm, forms)
		}
		if !forms["P4"] {
			t.Errorf("%s is no longer classified as a raw-cond match (P4); either the "+
				"fixture lost its ok(cond) arms or the classifier stopped recognising "+
				"them. forms=%v", rel, keysOfBool(forms))
		}
		if rawCondArms == 0 {
			t.Errorf("%s classifies as P4 but carries no IsRawCond arm — the classifier "+
				"and the fixture disagree about what a raw-cond arm is", rel)
		}
	}
}

// TestS1TypeArmFixturesAreExercised pins P5 from BOTH directions:
// type-match.no (the pre-existing fixture: bound arms over the flat union
// num = i32|i64|f64 plus bare arms and a default) and match-type-arm.no
// (the reinforcement added with this gate: the CHAINED union mixed = num|bool,
// a bound arm over the non-numeric member, bare arms, a type match nested
// inside a type arm, and a cross-function value match on bool).
//
// Beyond the sub-form tally it asserts the structural properties the file
// headers claim, because those are exactly what a careless edit would drop:
//   - at least one IsTypeArm arm (`v T ->`) — the only shape the census
//     classifier credits as P5;
//   - at least one BARE type arm (`i64 ->`) — classified as P1, so without
//     this assertion the bare shape could vanish with no census signal;
//   - the union alias itself is still declared (drop it and every arm stops
//     dispatching statically, yet the file would still parse);
//   - match-type-arm.no still nests a match inside a type arm (P6), the
//     P5×P6 interaction the fixture was written for.
func TestS1TypeArmFixturesAreExercised(t *testing.T) {
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("no corpus at %s: %v", corpusRoot, err)
	}
	for _, tc := range []struct {
		rel       string
		wantUnion string // union alias the fixture must still declare
		wantP6    bool   // fixture must nest a match inside a type arm
	}{
		{"type-match.no", "num", false},
		{"match-type-arm.no", "mixed", true},
	} {
		path := filepath.Join(corpusRoot, tc.rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("P5 fixture %s is missing: %v", tc.rel, err)
			continue
		}
		p := parser.New(lexer.NewCached(path, string(raw)))
		p.Filename = path
		p.SkipLowering = true
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Errorf("P5 fixture %s no longer parses: %v", tc.rel, errs)
			continue
		}
		if !declaresUnion(prog, tc.wantUnion) {
			t.Errorf("%s no longer declares the union alias %q; without it the type "+
				"arms have nothing to dispatch on (the file would still parse, so "+
				"nothing else would notice)", tc.rel, tc.wantUnion)
		}
		forms := map[string]bool{}
		members := unionMemberNames(prog)
		sms := collectSurfaceMatches(prog)
		// Matches nested inside another match's arm body must NOT count toward
		// the shape assertions below: match-type-arm.no's inner match carries
		// its own typed arms, so without this filter deleting the OUTER bound
		// arm (`v bool ->`) would still pass — measured, not assumed: breaking
		// exactly that arm passed the unfiltered version of this test.
		nested := map[uintptr]bool{}
		for _, sm := range sms {
			for i := range sm.Arms {
				for _, inner := range collectSurfaceMatchesFrom(sm.Arms[i].Body) {
					nested[reflect.ValueOf(inner).Pointer()] = true
				}
			}
		}
		var typedArms, bareTypeArms int
		for _, sm := range sms {
			if nested[reflect.ValueOf(sm).Pointer()] {
				continue
			}
			for i := range sm.Arms {
				a := &sm.Arms[i]
				if a.IsTypeArm {
					typedArms++
				}
				// A bare arm whose condition names a member of ANY union alias in
				// the file is a bare TYPE arm (the parser only sets IsTypeArm for
				// the `v T ->` shape; `i64 ->` stays a plain Identifier condition
				// until buildTypeMatchDesugar). Direct members of nested aliases
				// (mixed = num|bool ⇒ i64 via num) are covered because every alias
				// in the file contributes its own direct members.
				if id, ok := a.Condition.(*parser.Identifier); ok && members[id.Value] {
					bareTypeArms++
				}
			}
		}
		for _, sm := range sms {
			classifyMatch(sm, forms)
		}
		if !forms["P5"] {
			t.Errorf("%s is no longer classified as a type-destructuring match (P5); "+
				"either the fixture lost its `v T ->` arms or the classifier stopped "+
				"recognising them. forms=%v", tc.rel, keysOfBool(forms))
		}
		if typedArms == 0 {
			t.Errorf("%s classifies as P5 but carries no IsTypeArm arm — the classifier "+
				"and the fixture disagree about what a bound type arm is", tc.rel)
		}
		if bareTypeArms == 0 {
			t.Errorf("%s lost its bare type arms (`i64 ->`). The census credits those "+
				"as P1, so only this assertion keeps the bare shape covered", tc.rel)
		}
		if tc.wantP6 && !forms["P6"] {
			t.Errorf("%s no longer nests a match inside a type arm (P6); the P5×P6 "+
				"interaction it was written for is gone. forms=%v", tc.rel, keysOfBool(forms))
		}
	}
}

// declaresUnion reports whether prog declares a union alias with the given name.
func declaresUnion(prog *parser.Program, name string) bool {
	if prog == nil {
		return false
	}
	for _, stmt := range prog.Statements {
		if ta, ok := stmt.(*parser.TypeAlias); ok && ta.Name == name && ta.Union != nil {
			return true
		}
	}
	return false
}

// unionMemberNames collects the DIRECT member type names of every union alias
// declared in prog. Nested aliases are not expanded — but every alias in the
// file contributes its own direct members, so a member reached through a
// nested alias is present as long as that inner alias is declared too
// (mixed = num | bool ⇒ i64, because num = i32 | i64 | f64 is also declared).
func unionMemberNames(prog *parser.Program) map[string]bool {
	out := map[string]bool{}
	if prog == nil {
		return out
	}
	for _, stmt := range prog.Statements {
		ta, ok := stmt.(*parser.TypeAlias)
		if !ok || ta.Union == nil {
			continue
		}
		for _, t := range ta.Union.Types {
			if nt, ok := t.(*parser.NamedType); ok {
				out[nt.Value] = true
			}
		}
	}
	return out
}
