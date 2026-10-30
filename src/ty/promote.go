package ty

// Conv names the conversion a code generator must perform when a value of one
// type is stored, compared, or passed where another type is expected.
//
// This is the single table that replaces the duplicated decision points in
// mir/codegen.go (`coerce`, `coerceIntSigned`, `intSlotCastOp`, `intWidth`).
// The extension direction (SExt vs ZExt) is derived from the SOURCE type's
// signedness, never from the spelling of its name: that is what makes `[]i8`
// reads sign-extend while `[]byte`/`[]u8` reads zero-extend.
type Conv uint8

const (
	// ConvNone means the value already has the right representation.
	ConvNone Conv = iota
	// ConvTrunc narrows an integer (or a float) to fewer bits.
	ConvTrunc
	// ConvSExt sign-extends a narrower signed integer.
	ConvSExt
	// ConvZExt zero-extends a narrower unsigned integer, bool or char.
	ConvZExt
	// ConvSIToFP converts a signed integer to floating point (sitofp).
	ConvSIToFP
	// ConvUIToFP converts an unsigned integer to floating point (uitofp).
	ConvUIToFP
	// ConvFPToSI converts floating point to a signed integer (fptosi).
	ConvFPToSI
	// ConvFPToUI converts floating point to an unsigned integer (fptoui).
	ConvFPToUI
	// ConvSatFPToI is the saturating float-to-integer conversion nolang uses by
	// default: out-of-range values clamp instead of being undefined.
	ConvSatFPToI
	// ConvFPExt widens f32 to f64.
	ConvFPExt
	// ConvFPTrunc narrows f64 to f32.
	ConvFPTrunc
	// ConvBitcast reinterprets identical-width bits (i64 <-> double).
	ConvBitcast
	// ConvClone deep-copies an aggregate (str/slice/map/option/struct). MIR never
	// converts aggregates numerically; a differing aggregate type needs a copy.
	ConvClone
)

// String renders the conversion name for diagnostics.
func (c Conv) String() string {
	switch c {
	case ConvNone:
		return "none"
	case ConvTrunc:
		return "trunc"
	case ConvSExt:
		return "sext"
	case ConvZExt:
		return "zext"
	case ConvSIToFP:
		return "sitofp"
	case ConvUIToFP:
		return "uitofp"
	case ConvFPToSI:
		return "fptosi"
	case ConvFPToUI:
		return "fptoui"
	case ConvSatFPToI:
		return "sat-fptosi"
	case ConvFPExt:
		return "fpext"
	case ConvFPTrunc:
		return "fptrunc"
	case ConvBitcast:
		return "bitcast"
	case ConvClone:
		return "clone"
	}
	return "invalid"
}

// Convert reports how to turn a `from` value into a `to` value.
func Convert(from, to *Type) Conv {
	if from == nil || to == nil {
		return ConvNone
	}
	if Equal(from, to) {
		return ConvNone
	}
	f, t := from.Canonical(), to.Canonical()
	if f == nil || t == nil || f.Kind == Invalid || t.Kind == Invalid {
		return ConvNone
	}
	if Equal(f, t) {
		// Two spellings of the same underlying type (`fd` and `i64`): the value
		// already has the right representation.
		return ConvNone
	}
	if f.IsAggregate() || t.IsAggregate() {
		if f.Kind == t.Kind {
			return ConvClone
		}
		return ConvNone
	}

	fw, fInt := f.IntWidth()
	tw, tInt := t.IntWidth()
	if fInt && tInt {
		switch {
		case tw > fw:
			if f.IsUnsigned() {
				return ConvZExt
			}
			return ConvSExt
		case tw < fw:
			return ConvTrunc
		}
		// Same width: one LLVM type, so nothing to emit. Signedness only affects
		// which opcode later consumes the value.
		return ConvNone
	}
	if fInt && t.Kind == Float {
		if f.IsUnsigned() {
			return ConvUIToFP
		}
		return ConvSIToFP
	}
	if f.Kind == Float && tInt {
		return ConvSatFPToI
	}
	if f.Kind == Float && t.Kind == Float {
		switch {
		case f.Bits > t.Bits:
			return ConvFPTrunc
		case f.Bits < t.Bits:
			return ConvFPExt
		}
		return ConvNone
	}
	return ConvNone
}

// ConvertExact is Convert but reports the LLVM instruction spelling instead of
// the saturating default, for callers that need fptosi/fptoui directly. The
// signedness comes from the DESTINATION, which is what the result is
// interpreted as.
func ConvertExact(from, to *Type) Conv {
	c := Convert(from, to)
	if c != ConvSatFPToI {
		return c
	}
	if d := to.Canonical(); d != nil && d.IsUnsigned() {
		return ConvFPToUI
	}
	return ConvFPToSI
}

// Promote returns the type an arithmetic operation on l and r produces.
//
// Rules (single authority, replacing the per-operand guessing in
// mir/hir2mir.go):
//   - equal types keep the left spelling (no gratuitous renaming);
//   - the wider integer wins;
//   - integer combined with float yields the float;
//   - char combined with an integer yields the code-point slot (Int32, unsigned);
//   - bool combined with bool yields a 1-bit integer (the same LLVM i1);
//   - anything else (aggregates, unions, named layouts) does not promote.
//
// A nil result means "no promotion rule applies"; the caller must then reject
// the operation instead of silently flattening both sides to i64.
func (p *Pool) Promote(l, r *Type) *Type {
	if l == nil {
		return r
	}
	if r == nil {
		return l
	}
	a, b := l.Canonical(), r.Canonical()
	if a == nil {
		return r
	}
	if b == nil {
		return l
	}
	if a.Kind == Invalid || b.Kind == Invalid {
		return nil
	}
	// Two booleans promote to the 1-bit integer they are stored as, not back to
	// `bool`: the operation is arithmetic from here on.
	if a.Kind == Bool && b.Kind == Bool {
		return p.Int(1, false)
	}
	if Equal(a, b) {
		return l
	}

	// A float always wins over an integer; between two floats the wider wins.
	if a.Kind == Float || b.Kind == Float {
		switch {
		case a.Kind == Float && b.Kind == Float:
			if a.Bits >= b.Bits {
				return l
			}
			return r
		case a.Kind == Float && b.IsNumeric(), b.Kind == Float && a.IsNumeric():
			if a.Kind == Float {
				return l
			}
			return r
		}
		return nil
	}

	aw, aok := a.IntWidth()
	bw, bok := b.IntWidth()
	if !aok || !bok {
		return nil
	}

	// char carries a code point: its slot is 32 bits wide and unsigned, and a
	// char mixed with an integer stays in that slot.
	if a.Kind == Char || b.Kind == Char {
		if a.Kind == Char && b.Kind == Char {
			return l
		}
		return p.Int(32, false)
	}
	if a.Kind == Bool && b.Kind == Bool {
		return p.Int(1, false)
	}
	if a.Kind == Bool {
		return r
	}
	if b.Kind == Bool {
		return l
	}
	if aw >= bw {
		return l
	}
	return r
}
