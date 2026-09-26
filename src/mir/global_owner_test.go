package mir

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// THE RULE PINNED HERE
//
// A module binding belongs to the module that DECLARES it (hir.Package.Owners;
// "" = the main program), and a bare `name = expr` inside a function body binds
// to such a binding only when it is a binding of that function's OWN module.
//
// Without the scope, resolution is by bare name across the whole merged
// program, so a std module's private local resolves to — and silently
// overwrites — the main program's same-named top-level binding:
//
//	n = 7 / i = 9 / m = 11
//	print('n={n}') / print('i={i}') / print('m={m}')   ->  7 / 0 / 11
//
// std's fmt-parse-spec declares its own `i`, and the script's `i = 9` had been
// promoted to the module global `@i`, so the std function's `i = 0` stored
// straight into the user's binding. Measured on a pre-fix binary; the three
// tests below pin the scope, its same-module counterpart, and the main-program
// case that must keep working.

// lowerHIRWithOwners lowers src with the named top-level statements tagged as
// belonging to the given module (an entry of "" means the main program, which
// is also the default for anything not listed). This mirrors what
// build.loadStdModuleBody does with parser.SetModuleOwner when it merges a
// std module into the program.
func lowerHIRWithOwners(t *testing.T, src string, owners map[string]string) *Module {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v\n--- src ---\n%s", errs, src)
	}
	for _, stmt := range prog.Statements {
		var name string
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			name = s.Name
		case *parser.LetStatement:
			if s.Name != nil {
				name = s.Name.Value
			}
		}
		if owner, ok := owners[name]; ok && owner != "" {
			parser.SetModuleOwner(stmt, owner)
		}
	}
	pkg, idMap := parser.ASTToHIRWithMap(prog)
	if pkg == nil {
		t.Fatal("ASTToHIRWithMap returned nil")
	}
	parser.PopulateInferredTypes(pkg, idMap)
	mod, _, _ := LowerHIR(pkg, nil)
	if mod == nil {
		t.Fatal("LowerHIR returned nil")
	}
	return mod
}

// globalValueID returns the MIR value backing the module binding `name`, or
// NoVal when no such global was materialised.
func globalValueID(mod *Module, name string) ValueID {
	for i := range mod.Globals {
		if mod.Globals[i].Name == name {
			return mod.Globals[i].Init
		}
	}
	return NoVal
}

// funcRefsValue counts the instructions inside fnName that mention v, either
// as a destination or as an operand. A function that has been correctly scoped
// away from a foreign module binding mentions it exactly zero times.
func funcRefsValue(mod *Module, fnName string, v ValueID) int {
	fid, ok := mod.FuncByName[fnName]
	if !ok || v == NoVal {
		return -1
	}
	f := &mod.Funcs[fid]
	n := 0
	for _, b := range f.Blocks {
		for _, iid := range mod.Blocks[b].Insts {
			in := &mod.Insts[iid]
			if in.Dst == v {
				n++
			}
			for _, a := range in.Args {
				if a == v {
					n++
				}
			}
		}
	}
	return n
}

// TestForeignModuleFunctionCannotBindMainGlobal is the bug: a function owned by
// module `fmt` declares its own `i` while the main program has a top-level `i`.
// The std function must get a fresh local, never a store into the script's
// binding.
//
// The script binding IS a module global here (a main-owned helper reads it, so
// `referencedByFunc` is true and the promotion is legitimate) — which is the
// point: the scope has to hold even when the binding really is module storage.
func TestForeignModuleFunctionCannotBindMainGlobal(t *testing.T) {
	mod := lowerHIRWithOwners(t, `i = 9
use-i = () (r i64) {
    r = i
}
fmt-parse-spec = () (r i64) {
    i = 0
    r = i
}
print('i={i}')
print(use-i())
print(fmt-parse-spec())
`, map[string]string{"fmt-parse-spec": "fmt"})

	gv := globalValueID(mod, "i")
	if gv == NoVal {
		t.Fatalf("expected the script's `i` to be materialised as a module global "+
			"(a main-owned helper reads it); globals=%v", globalNames(mod))
	}
	if n := funcRefsValue(mod, "fmt-parse-spec", gv); n != 0 {
		t.Errorf("a function owned by module `fmt` must not touch the main program's "+
			"global `i`; it mentions that value %d time(s)", n)
	}
	// Control: the main-owned helper legitimately reads the same global, so the
	// assertion above is not vacuous.
	if n := funcRefsValue(mod, "use-i", gv); n == 0 {
		t.Errorf("control failed: the main-owned helper should read the global `i`")
	}
}

// TestSameModuleFunctionBindsOwnModuleGlobal is the counterpart that must keep
// working: std/log.no's `level` is written by set-level and read by
// debug/info/warn/error — all owned by `log` — so the binding must remain
// visible inside its own module. A scope that hid it from its own module would
// break the logger.
func TestSameModuleFunctionBindsOwnModuleGlobal(t *testing.T) {
	mod := lowerHIRWithOwners(t, `level = 1
set-level = (l i64) {
    level = l
}
read-level = () (r i64) {
    r = level
}
print('level={level}')
print(read-level())
`, map[string]string{"level": "log", "set-level": "log", "read-level": "log"})

	gv := globalValueID(mod, "level")
	if gv == NoVal {
		t.Fatalf("expected the module's own `level` to be materialised as a global; globals=%v",
			globalNames(mod))
	}
	if n := funcRefsValue(mod, "set-level", gv); n == 0 {
		t.Errorf("set-level (owned by `log`) must write the module's own `level`")
	}
	if n := funcRefsValue(mod, "read-level", gv); n == 0 {
		t.Errorf("read-level (owned by `log`) must read the module's own `level`")
	}
}

// TestMainProgramFunctionBindsMainGlobal is the main-program half of the same
// rule: a script helper must still reach a script-level binding (zip.no's
// `zw-data []byte` is written by add-file and read by create).
//
// The accumulator shape is deliberate — it is the one that a too-wide
// funcDeclaresName broke. Counting a plain `let` as a shadow made `add` look
// like it declared its own `total`, so the binding was not treated as
// function-shared and `add`'s write became a dead local; the program printed 0
// instead of 7. See funcDeclaresName.
//
// `#{overflow = wrap}` is required: without it `total + n` is `option<i64>` and
// the real checker rejects the program ([ovfhndld]). This test lowers HIR
// directly (bypassing the checker), so the annotation is what keeps the source
// an actually-compilable program rather than a fixture that only parses.
func TestMainProgramFunctionBindsMainGlobal(t *testing.T) {
	mod := lowerHIRWithOwners(t, `total = 0
add = (n i64) {
    #{overflow = wrap}
    total = total + n
}
print('total={total}')
print(add(3))
`, nil)

	gv := globalValueID(mod, "total")
	if gv == NoVal {
		t.Fatalf("expected the script's `total` to be a module global; globals=%v",
			globalNames(mod))
	}
	if n := funcRefsValue(mod, "add", gv); n == 0 {
		t.Errorf("a main-program helper must reach a main-program binding")
	}
}
