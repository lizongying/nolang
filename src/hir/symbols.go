package hir

import (
	"strconv"
	"strings"
)

// SymbolTable is the flat, cross-module resolved view of a set of packages.
// It is produced by walking HIR slices only: no lexing, no parsing, no regex
// scanning over source text.
//
// Key conventions and merge policies mirror checker.collectStdSigsFromFS
// exactly so the two can be compared table by table (see the equivalence test
// in hir/golden_test.go). The policies are NOT uniform, which is why this
// package writes straight into one table instead of merging per-module maps:
//
//	Funcs, Methods, Structs  last definition wins
//	StructMod, Aliases, Enums  first definition wins
type SymbolTable struct {
	// Funcs maps "module.fn" to its result type list. Receiver-generic
	// slice/array methods (names starting with "[") are registered here
	// under their bare name instead, matching the checker.
	Funcs map[string][]string
	// Methods maps "module.name" to its result type list, plus the bare
	// dotted name when the method name already carries a receiver prefix.
	Methods map[string][]string
	// FuncParams maps "module.fn" to its PARAMETER type list, keyed exactly
	// like Funcs. The receiver is never included: a method's `self` is a
	// KResult, not a KParam (see the KFuncDef case in collectInto).
	//
	// This is what lets the checker type-check std call ARGUMENTS. The result
	// tables alone only tell it what a call returns, so a call like
	// `t.eq([1,2,3])` on a `[]i64` went completely unchecked.
	FuncParams map[string][]string
	// MethodParams maps "module.name" to its parameter type list, keyed
	// exactly like Methods.
	MethodParams map[string][]string
	// Structs maps a struct name to its field name -> type string map.
	// Names that several modules define are additionally registered under
	// "module.name".
	Structs map[string]map[string]string
	// Aliases maps a single-concrete-type alias name to its target type.
	Aliases map[string]string
	// StructMod maps a struct name to the module short name that defines it.
	StructMod map[string]string
	// Enums maps an enum type name to its variant names.
	Enums map[string][]string
}

func NewSymbolTable() *SymbolTable {
	return &SymbolTable{
		Funcs:        make(map[string][]string),
		Methods:      make(map[string][]string),
		FuncParams:   make(map[string][]string),
		MethodParams: make(map[string][]string),
		Structs:      make(map[string]map[string]string),
		Aliases:      make(map[string]string),
		StructMod:    make(map[string]string),
		Enums:        make(map[string][]string),
	}
}

// ModulePackage pairs a built HIR package with the short module name that
// owns it. CollectModuleSignatures takes an ordered slice rather than a map so
// that the last-wins / first-wins rules above stay deterministic.
type ModulePackage struct {
	Short string
	Pkg   *Package
}

// CollectModuleSignatures is the HIR replacement for
// checker.collectStdSigsFromFS: instead of lexing and parsing every module, it
// walks already-built HIR arenas.
//
// Pass 1 counts how many modules define each struct name; pass 2 extracts the
// per-module symbols and qualifies ambiguous struct references in result types
// with their owning module, because after module merging a bare name would
// resolve to whichever module happened to be merged last.
func CollectModuleSignatures(mods []ModulePackage) *SymbolTable {
	structCount := make(map[string]int)
	for _, m := range mods {
		for _, id := range m.Pkg.Top {
			if n := m.Pkg.Node(id); n.Kind == KStructDef {
				structCount[m.Pkg.Str(n.S)]++
			}
		}
	}

	out := NewSymbolTable()
	for _, m := range mods {
		own := make(map[string]bool)
		for _, id := range m.Pkg.Top {
			if n := m.Pkg.Node(id); n.Kind == KStructDef {
				own[m.Pkg.Str(n.S)] = true
			}
		}
		collectInto(out, m.Pkg, m.Short, structCount, own)
	}
	// Pass 3: function aliases. `@ <path>.<fn> <alias>` declares that the bare
	// name <alias> in this module calls <path>'s <fn>. Registering the alias
	// under the declaring module's own key ("global.err" for global.no) makes
	// it visible to every consumer of this table — the checker's bare-name
	// gate, argument checking, and the parser's return-type inference — without
	// the compiler having to know any specific alias by name. Runs LAST because
	// it reads the target's signature out of the same table.
	registerAliasSigs(out, collectAliasDecls(mods))
	return out
}

// AliasDecl is one `@ <path>.<fn> <alias>` declaration found in a module.
//
// The AST/HIR statement is ExportStatement / KExport: in a package's lib.no it
// narrows what the package exports, and in an ordinary module (today only
// src/std/global.no) it declares a CALLABLE ALIAS — a second name for an
// existing function. Both readings share one syntax; the difference is the
// module the statement lives in, which the caller already knows.
type AliasDecl struct {
	Module string // short name of the declaring module ("global", "err", ...)
	Path   string // target module path as written ("std/err", "/pkg/src/mysql")
	Fn     string // target function name
	Alias  string // new bare name in the declaring module
}

// ModuleShortName reduces a module path to the short name the HIR merge and the
// std tables use: the last path segment ("std/err" -> "err", "/a/b/c" -> "c").
func ModuleShortName(path string) string {
	path = strings.TrimSuffix(strings.TrimSpace(path), "/")
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		path = path[i+1:]
	}
	return path
}

// slotChild returns the value id of a named KSlot child, or NoID.
func slotChild(p *Package, id int32, label string) int32 {
	for _, c := range p.Children(id) {
		n := p.Node(c)
		if n != nil && n.Kind == KSlot && p.Str(n.S) == label {
			return n.First
		}
	}
	return NoID
}

// GlobalModuleShort is the short name of std/global.no, the one module whose
// body is DECLARATION-ONLY: it declares the bare names a program may call
// without a module prefix, and its own statements are never compiled.
const GlobalModuleShort = "global"

// aliasTargetOf recognises the value side of the ASSIGNMENT form of a callable
// alias — `err = err.new` — and returns the target's module and function.
//
// Two shapes can reach here depending on whether the module-qualified-call
// rewriter has already run over the module: a KDot (`err.new`, the raw parse)
// or a dotted KIdent (`err.new` flattened). Everything else is refused, which
// is what keeps `PI = math.PI` (a constant read) from being mistaken for an
// alias: a constant target is not in the function table, and registerAliasSigs
// drops the declaration when the key is absent.
func aliasTargetOf(p *Package, id int32) (mod, fn string) {
	n := p.Node(id)
	if n == nil {
		return "", ""
	}
	switch n.Kind {
	case KDot:
		recv := p.Node(n.First)
		if recv == nil || recv.Kind != KIdent {
			return "", ""
		}
		return p.Str(recv.S), p.Str(n.S)
	case KIdent:
		name := p.Str(n.S)
		if i := strings.LastIndexByte(name, '.'); i > 0 {
			return name[:i], name[i+1:]
		}
	}
	return "", ""
}

// collectAliasDecls gathers every callable-alias declaration in mods. Two
// spellings declare the same thing:
//
//	@ std/err.new err        ; export-style statement
//	err = err.new            ; plain assignment (KLet)
//
// The assignment form is accepted ONLY in std/global.no. Everywhere else a
// top-level `name = mod.fn` is an ordinary variable holding a function value,
// and reading it as an alias would silently change what the program means (and
// delete the binding). In global.no there is no program to change: its body is
// never compiled, it exists only to declare bare names.
func collectAliasDecls(mods []ModulePackage) []AliasDecl {
	var out []AliasDecl
	for _, m := range mods {
		for _, id := range m.Pkg.Top {
			n := m.Pkg.Node(id)
			if n == nil {
				continue
			}
			switch n.Kind {
			case KExport:
				path, fn := m.Pkg.Str(n.S), m.Pkg.Str(n.S2)
				alias := ""
				if aid := slotChild(m.Pkg, id, "alias"); aid != NoID {
					if an := m.Pkg.Node(aid); an != nil {
						alias = m.Pkg.Str(an.S)
					}
				}
				if path == "" || fn == "" || alias == "" {
					continue
				}
				out = append(out, AliasDecl{Module: m.Short, Path: path, Fn: fn, Alias: alias})
			case KLet:
				if m.Short != GlobalModuleShort {
					continue
				}
				// A TYPED binding (`f fn = …`), a synthetic one, an arm
				// binding or a module constant is a real variable, not a
				// declaration.
				if n.Type != NoID || n.Has(FlagSynthetic) || n.Has(FlagArmBinding) || n.Has(FlagModuleConst) {
					continue
				}
				alias := m.Pkg.Str(n.S)
				mod, fn := aliasTargetOf(m.Pkg, n.First)
				if alias == "" || mod == "" || fn == "" {
					continue
				}
				out = append(out, AliasDecl{Module: m.Short, Path: mod, Fn: fn, Alias: alias})
			}
		}
	}
	return out
}

// registerAliasSigs copies each alias target's signature onto
// "<declaring module>.<alias>".
//
// A REAL definition of the same name always wins: the alias is only a second
// name, never an override (that mirrors `collectInto`'s last-definition-wins
// rule, where an actual function must beat a mere alias).
func registerAliasSigs(st *SymbolTable, decls []AliasDecl) {
	for _, d := range decls {
		key := ModuleShortName(d.Path) + "." + d.Fn
		if _, ok := st.Funcs[key]; !ok {
			// Unknown target (typo'd path, a name filtered out of the module,
			// or a module this build never loaded). Leave it unresolved so the
			// call site reports the ordinary "unknown callee" diagnostic
			// instead of silently aliasing to nothing.
			continue
		}
		target := d.Module + "." + d.Alias
		if _, exists := st.Funcs[target]; exists {
			continue
		}
		// PARAMETERS ONLY — the RESULT type is deliberately NOT registered.
		//
		// The signature table's result type is what makes the CHECKER type
		// every bare `err(...)` call as `err.err`, and that reading is wrong
		// for the builtin option's err variant: `x ?i64` + `x = err('boom')`
		// is the option constructor (tag 2), not an error-struct build. With
		// the result registered, the option's err payload lowering changed
		// shape and every `err ->` arm read the wrong bytes (tests/
		// option-test.no printed `1` instead of `error msg`; a `?str` arm
		// failed to compile at all).
		//
		// Nothing is lost: the alias's result type comes from the TARGET at the
		// call site, because the call is lowered to the target function itself
		// (mir/hir2mir.go resolveAlias) and the call's value type is that
		// function's actual result. The table still answers the two questions it
		// must: is this name callable, and what arguments does it take.
		st.Funcs[target] = nil
		if params, ok := st.FuncParams[key]; ok {
			st.FuncParams[target] = params
		}
	}
}

// ExtractSignatures collects the symbols of a single package. A package is
// always unambiguous relative to itself, so no struct qualification applies.
func ExtractSignatures(pkg *Package, moduleShort string) *SymbolTable {
	out := NewSymbolTable()
	collectInto(out, pkg, moduleShort, nil, nil)
	registerAliasSigs(out, collectAliasDecls([]ModulePackage{{Short: moduleShort, Pkg: pkg}}))
	return out
}

// collectInto appends one module's symbols to st. Statement handling and map
// write policies follow checker.collectStdSigsFromFS PASS 2 statement for
// statement; deviations are bugs, and the equivalence test is what proves it.
func collectInto(st *SymbolTable, pkg *Package, moduleShort string, structCount map[string]int, ownStructs map[string]bool) {
	qualify := func(typeStr string) string {
		bare := strings.TrimPrefix(typeStr, "?")
		if !ownStructs[bare] || structCount[bare] <= 1 {
			return typeStr
		}
		if bare != typeStr {
			return "?" + moduleShort + "." + bare
		}
		return moduleShort + "." + bare
	}

	for _, id := range pkg.Top {
		n := pkg.Node(id)
		switch n.Kind {

		case KFuncDef:
			name := pkg.Str(n.S)
			// Always a non-nil slice, even for a method with no results
			// (e.g. sort's "[]ord.sort-asc"), so that a registered symbol
			// with zero results stays distinguishable from an absent one.
			//
			// A method carries `self` as its FIRST KResult: the parser moves
			// the receiver out of the parameter list and re-emits it ahead of
			// the other results (see funcLike in parser/tohir.go). It is an
			// out-param receiver, not a return value, and the tables built
			// here feed let type inference (parser/type.go), which reads a
			// result list of length 1 as "the call returns this type". Left
			// in, a void method like `path.dir` would be inferred as returning
			// its own receiver type, and a one-result method would export a
			// two-entry list that the len==1 guard hides entirely.
			//
			// This mirrors parser.DeclaredResults for the AST; keep the two in
			// step, and see its comment for the full rationale.
			isMethod := n.Has(FlagMethod)
			rets := make([]string, 0)
			selfPending := isMethod
			for c := n.First; c != NoID; c = pkg.Nodes[c].Next {
				if pkg.Nodes[c].Kind != KResult {
					continue
				}
				if selfPending {
					// Only the first result can be the receiver, and only it
					// is named "self"; anything else falls through as a real
					// result so a method with a first result of another name
					// keeps all its values.
					selfPending = false
					if pkg.Str(pkg.Nodes[c].S) == "self" {
						continue
					}
				}
				rets = append(rets, qualify(pkg.Type(pkg.Nodes[c].Type)))
			}
			// Parameter type list, collected over the SAME child iteration
			// order as the results so the two stay index-aligned with the
			// declaration. A KParam is never the receiver (that is the
			// "self" KResult skipped above), so params[0] is the first real
			// argument. Always non-nil so a zero-arg function stays
			// distinguishable from an absent key.
			params := make([]string, 0)
			for c := n.First; c != NoID; c = pkg.Nodes[c].Next {
				if pkg.Nodes[c].Kind != KParam {
					continue
				}
				params = append(params, qualify(pkg.Type(pkg.Nodes[c].Type)))
			}
			switch {
			case !isMethod:
				st.Funcs[moduleShort+"."+name] = rets
				st.FuncParams[moduleShort+"."+name] = params
			case strings.HasPrefix(name, "["):
				// Receiver-generic array/slice method: the receiver is part
				// of the name, so the bare name is already unique.
				st.Funcs[name] = rets
				st.FuncParams[name] = params
			default:
				st.Methods[moduleShort+"."+name] = rets
				st.MethodParams[moduleShort+"."+name] = params
				// A name that already carries a receiver prefix
				// ("str.starts-with") is also registered bare so that type
				// inference can look it up as receiverType + "." + property.
				if strings.Contains(name, ".") {
					st.Methods[name] = rets
					st.MethodParams[name] = params
				}
			}

		case KStructDef:
			name := pkg.Str(n.S)
			fields := make(map[string]string)
			for c := n.First; c != NoID; c = pkg.Nodes[c].Next {
				fn := &pkg.Nodes[c]
				if fn.Kind != KStructField {
					continue
				}
				if ts := FieldType(pkg, fn); ts != "" {
					fields[pkg.Str(fn.S)] = ts
				}
			}
			st.Structs[name] = fields
			// An ambiguous struct is registered a second time under
			// "module.name" so that a binding annotated "tls.server-conn"
			// still resolves its fields after merging.
			if structCount[name] > 1 {
				st.Structs[moduleShort+"."+name] = fields
			}
			if _, ok := st.StructMod[name]; !ok {
				st.StructMod[name] = moduleShort
			}

		case KTypeAlias:
			// Only single concrete types participate. Unions are skipped, and
			// so are function types: their rendered form is a signature, not
			// a type name a newtype could refer to.
			if n.Has(FlagUnion) || n.Has(FlagFuncType) || n.Type == NoID {
				continue
			}
			name := pkg.Str(n.S)
			if _, ok := st.Aliases[name]; !ok {
				st.Aliases[name] = pkg.Type(n.Type)
			}

		case KEnumDef:
			name := pkg.Str(n.S)
			if _, ok := st.Enums[name]; ok {
				continue
			}
			// Appending leaves a variant-less enum absent from the map rather
			// than mapping it to an empty slice, matching the checker.
			for c := n.First; c != NoID; c = pkg.Nodes[c].Next {
				if pkg.Nodes[c].Kind == KEnumValue {
					st.Enums[name] = append(st.Enums[name], pkg.Str(pkg.Nodes[c].S))
				}
			}
		}
	}
}

// Merge copies every entry of src into dst, skipping keys already present.
// Used to layer module-local symbols over std-wide ones, where the local
// package must win; it is deliberately not used by CollectModuleSignatures,
// whose per-table policies differ (see SymbolTable).
func (dst *SymbolTable) Merge(src *SymbolTable) {
	for k, v := range src.Funcs {
		if _, ok := dst.Funcs[k]; !ok {
			dst.Funcs[k] = v
		}
	}
	for k, v := range src.Methods {
		if _, ok := dst.Methods[k]; !ok {
			dst.Methods[k] = v
		}
	}
	for k, v := range src.FuncParams {
		if _, ok := dst.FuncParams[k]; !ok {
			dst.FuncParams[k] = v
		}
	}
	for k, v := range src.MethodParams {
		if _, ok := dst.MethodParams[k]; !ok {
			dst.MethodParams[k] = v
		}
	}
	for k, v := range src.Structs {
		if _, ok := dst.Structs[k]; !ok {
			dst.Structs[k] = v
		}
	}
	for k, v := range src.Aliases {
		if _, ok := dst.Aliases[k]; !ok {
			dst.Aliases[k] = v
		}
	}
	for k, v := range src.StructMod {
		if _, ok := dst.StructMod[k]; !ok {
			dst.StructMod[k] = v
		}
	}
	for k, v := range src.Enums {
		if _, ok := dst.Enums[k]; !ok {
			dst.Enums[k] = v
		}
	}
}

// FieldType returns the declared type string of a KStructField. The "[N]" /
// "[]" prefix is already folded into the interned string by the AST -> HIR
// lowering, which applies the same rule as checker.structFieldTypeString: the
// legacy ArraySize / IsSlice modifiers only apply when the underlying type is
// not itself an array or slice type carrying its own prefix.
func FieldType(p *Package, n *Node) string {
	if n == nil || n.Type == NoID {
		return ""
	}
	return p.Type(n.Type)
}

// Export is one name a package makes visible to importers. It mirrors
// checker.ModuleExport field for field.
type Export struct {
	Name  string
	Value string // rendered literal for constant lets, "" otherwise
	Type  string // declared type for lets, "" otherwise
}

// ExportedSymbols returns the top-level names a package makes visible: let
// bindings, functions, non-private externs, and enum / tagged-enum variants.
//
// This is the HIR-side replacement for checker.parseModuleExportsFromSource,
// which today re-lexes and re-parses every std module on every build purely to
// recover these names. Order and multiplicity match it exactly so the two can
// be compared with DeepEqual.
func ExportedSymbols(pkg *Package) []Export {
	var out []Export
	for _, id := range pkg.Top {
		n := pkg.Node(id)
		switch n.Kind {
		case KLet:
			// A let with no name is not reachable from the surface syntax,
			// but the checker guards on it, so mirror the guard.
			if n.S == NoID {
				continue
			}
			out = append(out, Export{
				Name:  pkg.Str(n.S),
				Value: literalValue(pkg, n.First),
				Type:  pkg.Type(n.Type),
			})
		case KFuncDef:
			out = append(out, Export{Name: pkg.Str(n.S)})
		case KExtern:
			name := pkg.Str(n.S)
			if name == "" || strings.HasPrefix(name, "_") {
				continue // underscore-prefixed FFI declarations stay private
			}
			out = append(out, Export{Name: name})
		case KEnumDef, KTaggedEnumDef:
			for c := n.First; c != NoID; c = pkg.Nodes[c].Next {
				if k := pkg.Nodes[c].Kind; k == KEnumValue || k == KVariant {
					out = append(out, Export{Name: pkg.Str(pkg.Nodes[c].S)})
				}
			}
		}
	}
	return out
}

// ExportedNames projects ExportedSymbols down to the name list that
// checker.collectModuleExports feeds to undefined-variable validation.
func ExportedNames(pkg *Package) []string {
	exps := ExportedSymbols(pkg)
	if len(exps) == 0 {
		return nil
	}
	out := make([]string, 0, len(exps))
	for _, e := range exps {
		out = append(out, e.Name)
	}
	return out
}

// literalValue renders a constant initialiser the way
// checker.moduleExprValue does. Non-literal initialisers render as "".
func literalValue(p *Package, id int32) string {
	n := p.Node(id)
	if n == nil {
		return ""
	}
	switch n.Kind {
	case KIntLit:
		// Prefer the original token literal: values above int64 range wrap
		// when stored in Val and would render as -1.
		if lit := p.Str(n.S2); lit != "" {
			return lit
		}
		return strconv.FormatInt(n.Val, 10)
	case KFloatLit:
		if raw := p.Str(n.S); raw != "" {
			return raw
		}
		return strconv.FormatFloat(n.Float(), 'g', -1, 64)
	case KStrLit:
		return "\"" + p.Str(n.S) + "\""
	case KBoolLit:
		if n.Bool() {
			return "true"
		}
		return "false"
	case KNilLit:
		return "nil"
	}
	return ""
}
