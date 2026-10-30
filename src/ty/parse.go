package ty

import (
	"strconv"
	"strings"
)

// maxDepth bounds recursion so a pathological spelling can never blow the
// stack. Real nolang types nest a handful of levels deep.
const maxDepth = 48

// builtinScalars is the single semantic table for scalar spellings. It replaces
// the scattered prefix queries in mir's kind classifier, parser.buildType and
// codegen's int/float classifiers, so `byte`/`u8`/`char`/`bool` signedness is
// decided in exactly one place.
//
// Entries are value templates: build copies one out and the pool stamps the raw
// spelling onto the copy, because identity stays the source text (`byte` and
// `u8` are the same semantics but two distinct types).
var builtinScalars = map[string]Type{
	"void": {Kind: Void},
	"bool": {Kind: Bool, Bits: 1},
	"char": {Kind: Char, Bits: 32},

	"byte": {Kind: Int, Bits: 8},

	"i8":   {Kind: Int, Bits: 8, Signed: true},
	"i16":  {Kind: Int, Bits: 16, Signed: true},
	"i32":  {Kind: Int, Bits: 32, Signed: true},
	"i64":  {Kind: Int, Bits: 64, Signed: true},
	"i128": {Kind: Int, Bits: 128, Signed: true},
	"u8":   {Kind: Int, Bits: 8},
	"u16":  {Kind: Int, Bits: 16},
	"u32":  {Kind: Int, Bits: 32},
	"u64":  {Kind: Int, Bits: 64},
	"u128": {Kind: Int, Bits: 128},

	"f32": {Kind: Float, Bits: 32},
	"f64": {Kind: Float, Bits: 64},

	"str": {Kind: Str},
	"txt": {Kind: Txt},

	// std-defined aliases. They are NOT language keywords, but every stage must
	// agree that `int` is a signed 64-bit integer (it used to fall out of the
	// "bare name that is not a struct -> integer" fallback, which is exactly the
	// silent guesswork this table removes).
	"int":   {Kind: Int, Bits: 64, Signed: true},
	"uint":  {Kind: Int, Bits: 64},
	"float": {Kind: Float, Bits: 64},
}

// reservedUnions are the std aliases whose meaning is genuinely a union. They
// are parsed here rather than through a Resolver so the structure is available
// even in stages that have no std registry loaded (codegen, tests).
var reservedUnions = map[string][]string{
	"num":    {"i64", "f64"},
	"string": {"str", "txt"},
}

// parse is the depth-tracked entry point used both by Parse and by every nested
// component, so a deeply nested spelling cannot recurse past maxDepth.
func (p *Pool) parse(raw string, depth int) *Type {
	if t := p.lookup(raw); t != nil {
		return t
	}
	var built *Type
	if depth <= maxDepth {
		built = p.build(raw, depth)
	}
	if built == nil {
		built = &Type{Kind: Invalid}
	}
	built.raw = raw
	return p.intern(built)
}

// build classifies one spelling. It always returns a FRESH *Type (never a
// pointer that is already interned) because the caller stamps raw onto it.
func (p *Pool) build(raw string, depth int) *Type {
	s := trimSpace(raw)
	if s == "" {
		return nil
	}

	// Top-level `|` is a union. Splitting first is what makes `?A|B` mean
	// `(?A)|B` and keeps `fn(a|b)(c)` intact (the split is depth aware).
	if parts := splitTopLevel(s, '|'); len(parts) > 1 {
		var members []*Type
		for _, part := range parts {
			part = trimSpace(part)
			if part == "" {
				continue
			}
			members = append(members, p.parse(part, depth+1))
		}
		switch len(members) {
		case 0:
			return nil
		case 1:
			// A trailing/leading `|` produced a single member: keep the shape of
			// the member but hand back a private copy (raw differs from it).
			c := *members[0]
			return &c
		}
		return &Type{Kind: Union, Members: members}
	}

	// Prefix forms. `?` `&` `*` bind tighter than `[]`/brackets and are the
	// only spellings that can start a type with a punctuation byte.
	switch s[0] {
	case '?':
		if len(s) == 1 {
			return nil
		}
		return &Type{Kind: Option, Elem: p.parse(s[1:], depth+1)}
	case '&':
		if len(s) == 1 {
			return nil
		}
		return &Type{Kind: View, Elem: p.parse(s[1:], depth+1)}
	case '*':
		if len(s) == 1 {
			return nil
		}
		return &Type{Kind: Ptr, Elem: p.parse(s[1:], depth+1)}
	}
	// `ptr T` is the long spelling of `*T`. Both are Ptr; the raw text keeps the
	// author's spelling because it is the type's identity.
	if rest, ok := cutPrefixFold(s, "ptr "); ok {
		return &Type{Kind: Ptr, Elem: p.parse(rest, depth+1)}
	}

	// `[]T` slice. Checked before the general bracket form.
	if strings.HasPrefix(s, "[]") {
		if len(s) == 2 {
			return &Type{Kind: Slice}
		}
		return &Type{Kind: Slice, Elem: p.parse(s[2:], depth+1)}
	}

	// `[N]T` array vs `[K]V` map — one rule, shared by every stage.
	if s[0] == '[' && len(s) > 2 {
		if end := matchBracket(s, 0); end > 0 && end < len(s)-1 {
			inner := trimSpace(s[1:end])
			valRaw := s[end+1:]
			if inner == "" {
				return nil
			}
			if p.isArraySize(inner) {
				t := &Type{Kind: Array, Len: -1, Elem: p.parse(valRaw, depth+1)}
				if allDigits(inner) {
					if n, err := strconv.ParseInt(inner, 10, 64); err == nil {
						t.Len = n
					} else {
						t.LenText = inner
					}
				} else {
					t.LenText = inner
				}
				return t
			}
			return &Type{Kind: Map, Key: p.parse(inner, depth+1), Elem: p.parse(valRaw, depth+1)}
		}
	}

	// `fn(p0,p1)(r0,r1)?` — the spelling produced by parser.FunctionType.String().
	if s == "fn()" {
		return &Type{Kind: Func}
	}
	if strings.HasPrefix(s, "fn(") {
		return p.buildFunc(s, depth)
	}

	// Scalars.
	if tmpl, ok := builtinScalars[s]; ok {
		c := tmpl
		return &c
	}
	if members, ok := reservedUnions[s]; ok {
		out := &Type{Kind: Union, Members: make([]*Type, 0, len(members))}
		for _, m := range members {
			out.Members = append(out.Members, p.parse(m, depth+1))
		}
		return out
	}
	// `vec` is the legacy spelling of `[]i64`; every consumer treats it as an
	// owned i64 sequence.
	if s == "vec" {
		return &Type{Kind: Slice, Elem: p.parse("i64", depth+1)}
	}
	// Width spellings outside the builtin table (`u1` produced by promotion
	// of two bools) still have to intern as integers, not as opaque names.
	if k, bits, ok := parseIntSpelling(s); ok {
		return &Type{Kind: Int, Bits: bits, Signed: k == 'i'}
	}

	// Anything else is a named type: a struct, an enum, a newtype, a
	// module-qualified name (`fs.fd`) or a leftover monomorphization name. An
	// alias target resolved through the stage's Resolver is recorded in Under so
	// Canonical() can reach the structural type without re-parsing strings.
	n := &Type{Kind: Named, Name: s}
	if r := p.Resolver(); r != nil {
		if target, ok := r.Alias(s); ok && target != "" && target != s {
			n.Under = p.parse(trimSpace(target), depth+1)
		}
	}
	return n
}

// buildFunc parses `fn(params)(results)` with the result group optional.
func (p *Pool) buildFunc(s string, depth int) *Type {
	paramsEnd := matchParen(s, 2)
	if paramsEnd < 0 {
		return nil
	}
	paramRaw := s[3:paramsEnd]
	out := &Type{Kind: Func}

	rest := paramsEnd + 1
	if rest < len(s) {
		if s[rest] != '(' {
			return nil
		}
		resultsEnd := matchParen(s, rest)
		if resultsEnd < 0 || resultsEnd != len(s)-1 {
			return nil
		}
		for _, r := range splitTopLevel(s[rest+1:resultsEnd], ',') {
			if r = trimSpace(r); r != "" {
				out.Results = append(out.Results, p.parse(r, depth+1))
			}
		}
	}
	for _, q := range splitTopLevel(paramRaw, ',') {
		if q = trimSpace(q); q != "" {
			out.Params = append(out.Params, p.parse(q, depth+1))
		}
	}
	return out
}

// isArraySize decides whether bracket content is a length (`[32]`, `[n]`,
// `[?]`) instead of a key type (`[str]`, `[fd]`). This is the single authority
// for the array/map ambiguity that parser.buildType, parser.parseMapTypeString
// and the old MIR helper used to answer three different ways.
func (p *Pool) isArraySize(inner string) bool {
	switch inner {
	case "?", "_":
		// `[?]T` is an inferred length; `[_]T` is the discard spelling.
		return true
	}
	if allDigits(inner) {
		return true
	}
	// A single letter is an implicit generic size parameter (`[n]t`, `[N]byte`).
	// The parser injects these as generics (see parser.addImplicitGeneric), so
	// they are lengths, not key types.
	if isSingleLetter(inner) {
		return true
	}
	// A known type name is a key. This is the case the three old parsers
	// disagreed about: `[fd]i64` is a map keyed by the `fd = i64` alias.
	if _, ok := builtinScalars[inner]; ok {
		return false
	}
	if r := p.Resolver(); r != nil && r.IsTypeName(inner) {
		return false
	}
	// Unknown identifiers default to a key type, matching the historical MIR
	// behaviour (non-numeric bracket content => map).
	return false
}

// isSingleLetter reports an identifier of exactly one ASCII letter.
func isSingleLetter(s string) bool {
	if len(s) != 1 {
		return false
	}
	c := s[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// parseIntSpelling recognizes `iN`/`uN` for any width, so promotion results
// (`u1`, `i7`, ...) intern as integers instead of opaque named types.
func parseIntSpelling(s string) (kind byte, bits int16, ok bool) {
	if len(s) < 2 || (s[0] != 'i' && s[0] != 'u') {
		return 0, 0, false
	}
	digits := s[1:]
	if !allDigits(digits) {
		return 0, 0, false
	}
	w, err := strconv.Atoi(digits)
	if err != nil || w < 1 || w > 1048576 {
		return 0, 0, false
	}
	return s[0], int16(w), true
}

// cutPrefixFold strips an already-trimmed prefix, tolerating the lower-case
// `ptr` spelling with either a space or a tab after it.
func cutPrefixFold(s, prefix string) (string, bool) {
	if !strings.HasPrefix(s, prefix) {
		return "", false
	}
	return trimSpace(s[len(prefix):]), true
}

// matchParen returns the index of the ')' closing the '(' at s[open], or -1.
func matchParen(s string, open int) int {
	if open < 0 || open >= len(s) || s[open] != '(' {
		return -1
	}
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
