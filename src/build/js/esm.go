package js

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// Third-party ESM module imports for the JS backend.
//
// Syntax (reuses the `#` use directive; `npm/` is a reserved prefix):
//
//	# npm/lodash                 ; namespace import  -> import * as
//	# npm/lodash.get             ; named import      -> module object property
//	# npm/lodash.get as gt       ; named import with alias
//	# npm/lodash.default as _    ; default import (module object)
//	# npm/lodash/fp.get as get   ; subpath specifier "lodash/fp"
//	# npm/@scope/pkg             ; scoped package
//
// Because Node.js only guarantees a `default` export for CommonJS packages
// (named ESM imports from a CJS package are a link-time SyntaxError), every
// import is emitted as a namespace import plus a module-object binding:
//
//	import * as __ns_lodash from 'lodash';
//	const lodash = __ns_lodash.default ?? __ns_lodash;
//	const get = lodash.get;
//
// The module object resolves to `default` for CJS packages and to the module
// namespace for native ESM packages, so property access works for both.

// esmUsePrefix is the reserved use-path prefix for third-party ESM imports.
const esmUsePrefix = "npm/"

// ESMNamed is one named import: the exported name (From, "default" for the
// default export) and the local binding name as written in Nolang.
type ESMNamed struct {
	From  string
	Local string
}

// ESMImport describes one external ESM module referenced by the program.
type ESMImport struct {
	Specifier string     // "lodash" / "lodash/fp" / "@scope/pkg"
	ShortName string     // last specifier segment, the Nolang call prefix
	Named     []ESMNamed // named/default imports (deduplicated)
	Namespace bool       // whole-module import (no function name)
	// Bind marks a native-package wasm import (`#{bind = wasm, wasm = ...}`).
	// Such an import is emitted as a wasm bootstrap (init + ready Promise) rather
	// than a plain module-object const, and its same-named #{c} externs are
	// forwarded to its exports.
	Bind string // "" for ordinary ESM, "wasm" for a native wasm module
	// Init is the wasm init/export name (defaults to "default").
	Init string
}

// IsESMUse reports whether a UseStatement imports a third-party ESM module.
func IsESMUse(use *parser.UseStatement) bool {
	return use != nil && (strings.HasPrefix(use.Path, esmUsePrefix) || use.Path == "npm")
}

// ESMSpecifier returns the package specifier of an ESM use statement
// ("lodash", "lodash/fp", "@scope/pkg").
func ESMSpecifier(use *parser.UseStatement) (string, bool) {
	if !IsESMUse(use) {
		return "", false
	}
	return strings.TrimPrefix(use.Path, esmUsePrefix), true
}

// SanitizeSpecifier validates an npm package specifier. Only
// [A-Za-z0-9@/._-] is accepted; ".", ".." segments, absolute paths and
// backslashes are rejected so a specifier can never escape into an import
// statement as a path.
func SanitizeSpecifier(spec string) (string, error) {
	if spec == "" || spec == "npm" {
		return "", fmt.Errorf("empty ESM package specifier in import %q", esmUsePrefix+spec)
	}
	if strings.HasPrefix(spec, "/") || strings.HasSuffix(spec, "/") || strings.Contains(spec, "//") {
		return "", fmt.Errorf("ESM package specifier %q must not start or end with '/'", spec)
	}
	for _, r := range spec {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '@' || r == '/' || r == '.' || r == '_' || r == '-':
		default:
			return "", fmt.Errorf("invalid character %q in ESM package specifier %q", r, spec)
		}
	}
	for _, seg := range strings.Split(spec, "/") {
		if seg == "." || seg == ".." {
			return "", fmt.Errorf("relative path segments are not allowed in ESM package specifier %q", spec)
		}
	}
	return spec, nil
}

// IsRelativeSpecifier reports whether a module specifier is a relative file
// path ("./x", "../x", "/abs/x") rather than a bare npm package name. A
// native wasm= specifier may be either: a bare specifier is an npm package
// (declared in dependencies, resolved through the browser import map's CDN),
// while a relative one is a local file next to the emitted JS (node) or next
// to the HTML document (browser) and needs neither of those.
func IsRelativeSpecifier(spec string) bool {
	return strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") || strings.HasPrefix(spec, "/")
}

// esmShortName returns the last path segment of a specifier — the short name
// cross-module call prefixes use (checker.ModuleShortName uses the same rule).
func esmShortName(spec string) string {
	if i := strings.LastIndex(spec, "/"); i >= 0 {
		return spec[i+1:]
	}
	return spec
}

// ExtractESMImports scans the program's UseStatements for ESM imports and
// aggregates them per specifier. The same specifier imported by several use
// statements becomes one ESMImport with a merged named list.
func ExtractESMImports(program *parser.Program) ([]ESMImport, error) {
	if program == nil {
		return nil, nil
	}
	return ExtractESMImportsFromStmts(program.Statements)
}

// ExtractESMImportsFromStmts is ExtractESMImports over an already
// platform-filtered statement list (the JS backend filters before importing so
// a #{js-browser}-only import never leaks into a node build).
func ExtractESMImportsFromStmts(stmts []parser.Statement) ([]ESMImport, error) {
	var imports []ESMImport
	index := make(map[string]int)
	for _, stmt := range stmts {
		use, ok := stmt.(*parser.UseStatement)
		if !ok || !IsESMUse(use) {
			continue
		}
		spec, _ := ESMSpecifier(use)
		spec, err := SanitizeSpecifier(spec)
		if err != nil {
			return nil, err
		}
		short := esmShortName(spec)
		if short == "" || short == "@" {
			return nil, fmt.Errorf("invalid ESM package specifier %q", spec)
		}
		i, seen := index[spec]
		if !seen {
			imports = append(imports, ESMImport{Specifier: spec, ShortName: short})
			i = len(imports) - 1
			index[spec] = i
		}
		if use.Function == "" {
			imports[i].Namespace = true
			continue
		}
		local := use.Function
		if use.Alias != "" {
			local = use.Alias
		}
		named := ESMNamed{From: use.Function, Local: local}
		dup := false
		for _, n := range imports[i].Named {
			if n == named {
				dup = true
				break
			}
		}
		if !dup {
			imports[i].Named = append(imports[i].Named, named)
		}
	}
	return imports, nil
}

// ESMBinding is one ESMImport with its generated JS identifiers resolved.
type ESMBinding struct {
	ESMImport
	NSName  string            // import * as NSName from 'specifier'
	ObjName string            // module object binding (const ObjName = NSName.default ?? NSName)
	Locals  map[string]string // exported name -> local binding identifier
}

// jsLiteral renders s as a JS double-quoted string literal.
func jsLiteral(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString("\\\"")
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// jsProp renders a module-object property access for an exported name. Names
// that are not valid JS identifiers (hyphenated CJS keys) use bracket access.
func jsProp(name string) string {
	ok := name != ""
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_', r == '$':
		case r >= '0' && r <= '9' && i > 0:
		default:
			ok = false
		}
		if !ok {
			break
		}
	}
	if ok {
		return "." + name
	}
	return "[" + jsLiteral(name) + "]"
}

// sanitizeJSIdent maps arbitrary text to a base JS identifier by replacing
// every character outside [A-Za-z0-9_] with '_'.
func sanitizeJSIdent(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// ResolveESMBindings assigns generated JS identifiers to the ESM imports,
// avoiding collisions with the program's own top-level names (function,
// struct, enum and global variable names) and with each other. A colliding
// binding gets a "$esm" suffix (then "$esm2", ... if still taken).
func ResolveESMBindings(imports []ESMImport, taken map[string]bool) ([]ESMBinding, error) {
	if len(imports) == 0 {
		return nil, nil
	}
	unique := func(name string) string {
		candidate := name
		for i := 2; taken[candidate]; i++ {
			if i == 2 {
				candidate = name + "$esm"
			} else {
				candidate = fmt.Sprintf("%s$esm%d", name, i)
			}
		}
		taken[candidate] = true
		return candidate
	}
	bindings := make([]ESMBinding, 0, len(imports))
	seenShort := make(map[string]string, len(imports))
	for _, imp := range imports {
		if prev, dup := seenShort[imp.ShortName]; dup {
			// Two specifiers sharing a short name cannot be told apart at
			// the call site (module.fn uses the short name), so refuse.
			return nil, fmt.Errorf("ambiguous ESM short name %q: specifiers %q and %q both resolve to call prefix %q", imp.ShortName, prev, imp.Specifier, imp.ShortName)
		}
		seenShort[imp.ShortName] = imp.Specifier
		b := ESMBinding{ESMImport: imp}
		b.NSName = unique("__ns_" + sanitizeJSIdent(imp.Specifier))
		b.ObjName = unique(jsIdent(imp.ShortName))
		b.Locals = make(map[string]string, len(imp.Named))
		for _, n := range imp.Named {
			b.Locals[n.From] = unique(jsIdent(n.Local))
		}
		bindings = append(bindings, b)
	}
	return bindings, nil
}

// emitESMImports writes the ESM import header. Every specifier becomes one
// namespace import plus a module-object binding; named imports are const
// bindings resolved off the module object (namespace first, default second,
// so CommonJS packages without statically-detectable named exports work).
func (g *Generator) emitESMImports() {
	for _, b := range g.esmBindings {
		if b.Bind == "wasm" {
			g.emitWASMImport(b)
			continue
		}
		g.writeRaw("import * as " + b.NSName + " from " + jsLiteral(b.Specifier) + ";\n")
		g.writeRaw("const " + b.ObjName + " = " + b.NSName + ".default ?? " + b.NSName + ";\n")
		for _, n := range b.Named {
			local := b.Locals[n.From]
			if n.From == "default" {
				// default import binds the module object itself.
				g.writeRaw("const " + local + " = " + b.ObjName + ";\n")
				continue
			}
			g.writeRaw("const " + local + " = " + b.NSName + jsProp(n.From) + " ?? " + b.ObjName + jsProp(n.From) + ";\n")
		}
	}
}

// esmCallLocal returns the local binding for an ESM module member
// (short.from), if the program imports it by name.
func (g *Generator) esmCallLocal(short, from string) (string, bool) {
	local, ok := g.esmLocalByKey[short+"."+from]
	return local, ok
}

// generateESMCall rewrites calls into third-party ESM modules. A named import
// (module.fn with fn registered in esmLocalByKey) emits the local binding; a
// namespace import emits module-object property access. Returns handled=false
// when the receiver is not a registered ESM module.
func (g *Generator) generateESMCall(de *parser.DotExpression, args []parser.Expression) (string, bool) {
	ident, ok := de.Receiver.(*parser.Identifier)
	if !ok {
		return "", false
	}
	obj, ok := g.esmObjByShort[ident.Value]
	if !ok {
		return "", false
	}
	if local, handled := g.esmCallLocal(ident.Value, de.Property); handled {
		return local + "(" + g.joinExpressions(args) + ")", true
	}
	return obj + jsProp(de.Property) + "(" + g.joinExpressions(args) + ")", true
}

// ESMBindings collects the program's taken top-level identifiers (function,
// struct, enum and global-variable names) so ESM bindings can avoid them.
func (g *Generator) collectTakenNames(stmts []parser.Statement) map[string]bool {
	taken := make(map[string]bool)
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			if !s.IsMethodDef && s.Name != "" {
				taken[jsIdent(s.Name)] = true
			}
		case *parser.StructDefinition:
			if s.Name != "" {
				taken[jsIdent(s.Name)] = true
			}
		case *parser.EnumDefinition:
			if s.Name != "" {
				taken[jsIdent(s.Name)] = true
			}
		case *parser.LetStatement:
			if s.Name != nil && s.Name.Value != "" {
				taken[jsIdent(s.Name.Value)] = true
			}
		}
	}
	return taken
}
