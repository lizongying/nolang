// Package ty defines Nolang's structural type representation.
//
// A Type is IMMUTABLE once interned by a Pool. Its identity key is the original
// source spelling (raw), so diagnostics, the AST cache and LLVM symbol names
// keep producing exactly the text they produced before this package existed.
// The structural fields (Kind/Bits/Signed/Elem/...) are the single place that
// knows what a spelling MEANS, replacing the string-prefix sniffing that used
// to be duplicated in parser.buildType, mir's kind/ownership classifiers,
// checker and codegen.
//
// Layering: ty imports only the standard library. parser/checker/hir/mir/build
// may import ty; ty never imports them. Semantic registries (struct layouts,
// std aliases, enums) stay in their stage and are exposed to the pool through
// the Resolver interface.
package ty

import "strings"

// Kind is the structural shape of a type.
type Kind uint8

const (
	// Invalid is the zero value: an empty or unusable spelling.
	Invalid Kind = iota
	// Void is the no-value type (`void`, function result absence).
	Void
	// Bool is the i1 boolean.
	Bool
	// Int is a fixed-width integer (signed or unsigned).
	Int
	// Float is a floating-point scalar (f32/f64/`float`).
	Float
	// Char is a Unicode scalar value; storage i32, displayed as code point.
	Char
	// Str is the owned string (%str-long).
	Str
	// Txt is the bounded inline text (%txt, [255 x i8] + i8 len).
	Txt
	// Slice is `[]T` (%vec descriptor).
	Slice
	// Array is `[N]T` / `[n]T`.
	Array
	// Map is `[K]V` (hashmap-<K>-<V>).
	Map
	// Option is `?T` (%option).
	Option
	// View is `&T` — a non-owning borrow bound to the receiver's lifetime.
	View
	// Ptr is `*T` / `ptr T` — a raw pointer.
	Ptr
	// Func is `fn(p...)(r...)`.
	Func
	// Union is `a | b` (std aliases such as `num`, `string`).
	Union
	// Named is a struct, enum, newtype or an unresolved alias name.
	Named
)

// String returns the spelling name of the kind (diagnostics only).
func (k Kind) String() string {
	switch k {
	case Void:
		return "void"
	case Bool:
		return "bool"
	case Int:
		return "int"
	case Float:
		return "float"
	case Char:
		return "char"
	case Str:
		return "str"
	case Txt:
		return "txt"
	case Slice:
		return "slice"
	case Array:
		return "array"
	case Map:
		return "map"
	case Option:
		return "option"
	case View:
		return "view"
	case Ptr:
		return "ptr"
	case Func:
		return "func"
	case Union:
		return "union"
	case Named:
		return "named"
	}
	return "invalid"
}

// Type is a structural view of one nolang type spelling.
//
// Only a Pool creates Type values (or Make for stage-derived types); the raw
// text is unexported so the spelling cannot drift away from the structure that
// was derived from it.
type Type struct {
	Kind   Kind
	Bits   int16 // Int/Float width in bits; 0 means "unspecified" (Int defaults to 64)
	Signed bool  // Int only; char/bool are unsigned
	Elem   *Type // Slice/Array/Option/View/Ptr element (nil when unknown)
	Key    *Type // Map key type
	Len    int64 // Array literal length; -1 when symbolic/unknown
	// LenText is the array's symbolic size spelling (`[N]T`, `[n]T`) when Len
	// is not a literal number. Empty for literal arrays, `"?"` for `[?]T`.
	LenText string
	Params  []*Type // Func parameters
	Results []*Type // Func results
	Members []*Type // Union members, in source order
	Name    string  // Named: struct/enum/newtype spelling (may be module-qualified)
	// Under is the alias target resolved through Pool.Resolver (e.g. `fd` -> i64).
	// nil when the name is not an alias (or no resolver was supplied).
	Under *Type

	raw string
}

// Raw returns the exact source spelling this Type was parsed from. It is the
// identity key used by every stage's type table.
func (t *Type) Raw() string {
	if t == nil {
		return ""
	}
	return t.raw
}

// String implements fmt.Stringer with the original spelling.
func (t *Type) String() string { return t.Raw() }

// IsValid reports whether t carries a usable type.
func (t *Type) IsValid() bool { return t != nil && t.Kind != Invalid }

// Canonical follows the alias chain (`Under`) to the first structural type.
// It never mutates and is cycle-safe.
func (t *Type) Canonical() *Type {
	cur := t
	for i := 0; i < 32 && cur != nil && cur.Kind == Named && cur.Under != nil; i++ {
		cur = cur.Under
	}
	return cur
}

// IsOptionRaw reports whether a type SPELLING is an option type (`?T`).
//
// This file is the ONE place allowed to look at the prefix directly: everywhere
// else the question "is this an option?" must be asked through Kind, either of a
// parsed type or of this predicate. The prefix test and Parse's Kind test are the
// same predicate by construction (build() classifies a leading '?' as Option), so
// migrating a call site from `strings.HasPrefix(x, "?")` to this helper is
// behaviour-preserving — while migrating it to Kind on an existing *Type is the
// better move when one is at hand (it needs no re-parse at all).
func IsOptionRaw(raw string) bool { return raw != "" && raw[0] == '?' }

// IsViewRaw reports whether a spelling is a view type (`&T`).
func IsViewRaw(raw string) bool { return raw != "" && raw[0] == '&' }

// StripOptionRaw removes one leading option marker: `?T` -> `T`, `T` -> `T`.
func StripOptionRaw(raw string) string {
	if IsOptionRaw(raw) {
		return raw[1:]
	}
	return raw
}

// StripViewRaw removes one leading view marker: `&T` -> `T`, `T` -> `T`.
func StripViewRaw(raw string) string {
	if IsViewRaw(raw) {
		return raw[1:]
	}
	return raw
}

// IsPtrRaw reports whether a spelling is a raw pointer (`*T` or `ptr T`).
// Both spellings mean the same thing; asking the pool is what makes that true
// for every caller at once (a bare prefix scan only ever saw `*`).
func IsPtrRaw(raw string) bool { return Parse(raw).Kind == Ptr }

// StripPtrRaw removes ONE pointer layer: `*T` -> `T`, `ptr T` -> `T`, else raw.
func StripPtrRaw(raw string) string { return Parse(raw).ElemRaw() }

// StripPtrAllRaw removes every leading pointer layer: `**T` -> `T`.
func StripPtrAllRaw(raw string) string {
	t := Parse(raw)
	for i := 0; t != nil && t.Kind == Ptr && i < 32; i++ {
		t = t.Elem
	}
	return t.Raw()
}

// ElemRaw returns the element spelling for a wrapper type (Slice/Array/Option/
// View/Ptr) and the spelling itself otherwise. It is the structural form of
// `strings.TrimPrefix` / bracket scanning on a type text.
func (t *Type) ElemRaw() string {
	if t == nil {
		return ""
	}
	if t.Elem == nil {
		return t.Raw()
	}
	return t.Elem.Raw()
}

// MembersRaw returns the union member spellings in source order, or nil when t
// is not a union. It replaces splitting a type text on "|".
func (t *Type) MembersRaw() []string {
	if t == nil || t.Kind != Union || len(t.Members) == 0 {
		return nil
	}
	out := make([]string, 0, len(t.Members))
	for _, m := range t.Members {
		out = append(out, m.Raw())
	}
	return out
}

// UnionMembersRaw is the package-level form of (*Type).MembersRaw.
func UnionMembersRaw(raw string) []string { return Parse(raw).MembersRaw() }

// MembersOrSelfRaw is MembersRaw for a union and a one-element slice holding the
// spelling otherwise. It is the drop-in replacement for splitting a type text on
// "|": the split always yields at least one part, and so does this.
func (t *Type) MembersOrSelfRaw() []string {
	if ms := t.MembersRaw(); ms != nil {
		return ms
	}
	return []string{t.Raw()}
}

// UnionMembersOrSelfRaw is the package-level form of (*Type).MembersOrSelfRaw.
func UnionMembersOrSelfRaw(raw string) []string { return Parse(raw).MembersOrSelfRaw() }

// IsSliceRaw reports whether a spelling is a slice type (`[]T`).
func IsSliceRaw(raw string) bool { return Parse(raw).Kind == Slice }

// IsArrayRaw reports whether a spelling is a fixed array type (`[N]T`).
func IsArrayRaw(raw string) bool { return Parse(raw).Kind == Array }

// IsMapRaw reports whether a spelling is a map type (`[K]V`).
func IsMapRaw(raw string) bool { return Parse(raw).Kind == Map }

// IsContainerRaw reports whether a spelling is a slice, array or map.
func IsContainerRaw(raw string) bool {
	switch Parse(raw).Kind {
	case Slice, Array, Map:
		return true
	}
	return false
}

// Unwrap drops the transparent wrappers — Option (`?T`) and View (`&T`) — and
// returns the type they wrap. It is the structural replacement for trimming
// "?" / "&" prefixes by hand, and it answers the question those trims were
// really asking: which type do methods and name lookups dispatch on?
func (t *Type) Unwrap() *Type {
	for i := 0; t != nil && i < 32; i++ {
		switch t.Kind {
		case Option, View:
			if t.Elem == nil {
				return t
			}
			t = t.Elem
		default:
			return t
		}
	}
	return t
}

// OptionInner returns the payload of an option type, or the type itself when it
// is not an option. Unlike Unwrap it does not cross a View layer.
func (t *Type) OptionInner() *Type {
	if t != nil && t.Kind == Option && t.Elem != nil {
		return t.Elem
	}
	return t
}

// Underlying returns the alias target, or nil when t is not an alias.
func (t *Type) Underlying() *Type {
	if t == nil || t.Kind != Named {
		return nil
	}
	return t.Under
}

// IntWidth reports the integer width in bits for Int/Char/Bool. The second
// result is false for every other kind.
func (t *Type) IntWidth() (int, bool) {
	c := t.Canonical()
	if c == nil {
		return 0, false
	}
	switch c.Kind {
	case Int:
		if c.Bits > 0 {
			return int(c.Bits), true
		}
		return 64, true
	case Char:
		return 32, true
	case Bool:
		return 1, true
	}
	return 0, false
}

// IsUnsigned reports whether the integer-like type extends with zext.
// `byte`/`u8`/`char`/`bool` are unsigned; `i8` is signed.
func (t *Type) IsUnsigned() bool {
	c := t.Canonical()
	if c == nil {
		return false
	}
	switch c.Kind {
	case Int:
		return !c.Signed
	case Char, Bool:
		return true
	}
	return false
}

// IsInteger reports Int/Char/Bool.
func (t *Type) IsInteger() bool {
	c := t.Canonical()
	if c == nil {
		return false
	}
	return c.Kind == Int || c.Kind == Char || c.Kind == Bool
}

// IsFloat reports the f32/f64/`float` family.
func (t *Type) IsFloat() bool {
	c := t.Canonical()
	return c != nil && c.Kind == Float
}

// IsNumeric reports integer or float scalars.
func (t *Type) IsNumeric() bool { return t.IsInteger() || t.IsFloat() }

// IsBool reports the boolean type.
func (t *Type) IsBool() bool {
	c := t.Canonical()
	return c != nil && c.Kind == Bool
}

// IsStr / IsTxt / IsOption / ... are the structural predicates that replace
// strings.HasPrefix(raw, "?") / HasSuffix(raw, "[]i64") style checks.
func (t *Type) IsStr() bool {
	c := t.Canonical()
	return c != nil && c.Kind == Str
}

// IsTxt reports the bounded inline text type.
func (t *Type) IsTxt() bool {
	c := t.Canonical()
	return c != nil && c.Kind == Txt
}

// IsOption reports `?T`.
func (t *Type) IsOption() bool {
	c := t.Canonical()
	return c != nil && c.Kind == Option
}

// IsSlice reports `[]T`.
func (t *Type) IsSlice() bool {
	c := t.Canonical()
	return c != nil && c.Kind == Slice
}

// IsArray reports `[N]T`.
func (t *Type) IsArray() bool {
	c := t.Canonical()
	return c != nil && c.Kind == Array
}

// IsMap reports `[K]V`.
func (t *Type) IsMap() bool {
	c := t.Canonical()
	return c != nil && c.Kind == Map
}

// IsFunc reports `fn(...)`.
func (t *Type) IsFunc() bool {
	c := t.Canonical()
	return c != nil && c.Kind == Func
}

// IsUnion reports `a | b`.
func (t *Type) IsUnion() bool {
	c := t.Canonical()
	return c != nil && c.Kind == Union
}

// IsNamed reports a struct/enum/newtype reference.
func (t *Type) IsNamed() bool {
	c := t.Canonical()
	return c != nil && c.Kind == Named
}

// IsPtr reports `*T` / `ptr T`.
func (t *Type) IsPtr() bool {
	c := t.Canonical()
	return c != nil && c.Kind == Ptr
}

// IsView reports `&T`.
func (t *Type) IsView() bool {
	c := t.Canonical()
	return c != nil && c.Kind == View
}

// IsVoid reports `void`.
func (t *Type) IsVoid() bool {
	c := t.Canonical()
	return c != nil && c.Kind == Void
}

// IsAggregate reports a type that is passed by pointer in the MIR ABI.
func (t *Type) IsAggregate() bool {
	c := t.Canonical()
	if c == nil {
		return false
	}
	switch c.Kind {
	case Str, Txt, Slice, Array, Map, Option, Func, View, Named:
		return true
	}
	return false
}

// LLVMScalar returns the LLVM scalar type for scalar kinds (Int/Char/Bool/
// Float/Void). Composites return ok=false because their LLVM type needs the
// stage's layout context (element type, registered struct names, ...).
func (t *Type) LLVMScalar() (string, bool) {
	c := t.Canonical()
	if c == nil {
		return "", false
	}
	switch c.Kind {
	case Void:
		return "void", true
	case Bool:
		return "i1", true
	case Char:
		return "i32", true
	case Float:
		// f32 and f64 both travel as double in MIR; narrowing uses an explicit
		// fptrunc round-trip (see builtin LLVMConvF64ToF32).
		return "double", true
	case Int:
		w := c.Bits
		if w <= 0 {
			w = 64
		}
		switch w {
		case 1:
			return "i1", true
		case 8:
			return "i8", true
		case 16:
			return "i16", true
		case 32:
			return "i32", true
		case 64:
			return "i64", true
		case 128:
			return "i128", true
		}
	}
	return "", false
}

// LLVM returns the LLVM type for the kinds that have a fixed representation,
// and "" for kinds that need stage context.
func (t *Type) LLVM() string {
	c := t.Canonical()
	if c == nil {
		return ""
	}
	if s, ok := c.LLVMScalar(); ok {
		return s
	}
	switch c.Kind {
	case Str:
		return "%str-long"
	case Txt:
		return "%txt"
	case Slice:
		return "%vec"
	case Option:
		return "%option"
	case Ptr, View:
		return "i8*"
	case Func:
		return "void ()*"
	}
	return ""
}

// Equal compares two types by identity spelling. It is the same relation the
// existing raw-string type tables use (`TypeMap[raw]`), with a pointer fast
// path for the common interned case.
func Equal(a, b *Type) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.raw == b.raw
}

// HasMember reports whether a union type (or its alias chain) contains a
// member whose spelling equals raw.
func (t *Type) HasMember(raw string) bool {
	c := t.Canonical()
	if c == nil || c.Kind != Union {
		return false
	}
	for _, m := range c.Members {
		if m != nil && m.raw == raw {
			return true
		}
	}
	return false
}

// splitTopLevel splits s on sep at bracket depth 0. It understands (), [], {}
// and <> so `fn([]i64)(str)` and `[]a|b` inside a container stay intact.
func splitTopLevel(s string, sep byte) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{', '<':
			depth++
		case ')', ']', '}', '>':
			if depth > 0 {
				depth--
			}
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

// matchBracket returns the index of the ']' closing the '[' at s[open], or -1.
func matchBracket(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// allDigits reports whether s is a non-empty run of ASCII digits.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// looksLikeConst reports a symbolic array length (`N`, `MAX_LEN`, `BUF2`).
// The convention mirrors the std corpus: constants are upper-case, type keys
// are lower-case type names.
func looksLikeConst(s string) bool {
	if s == "" {
		return false
	}
	hasLetter := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			hasLetter = true
		case c >= '0' && c <= '9', c == '_':
		default:
			return false
		}
	}
	return hasLetter
}

// trimSpace is strings.TrimSpace without importing text-specific helpers twice.
func trimSpace(s string) string { return strings.TrimSpace(s) }
