package fmt

import (
	"strconv"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// fieldAnnotations returns the `#{...}` entries the parser filed for a struct
// field. Fields are not statements, so attachedAnnotations (which switches on
// Statement types) never sees them.
func (f *formatter) fieldAnnotations(field *parser.StructField) []*parser.AnnotationEntry {
	if f.sem == nil || field == nil {
		return nil
	}
	return f.sem.AnnotationsOf(field)
}

func (f *formatter) formatStructDefinition(s *parser.StructDefinition) {
	f.write(s.Name)
	if len(s.Implements) > 0 {
		f.write(" ")
		f.write(strings.Join(s.Implements, ", "))
	}
	f.write(" {")
	f.indent++
	for _, field := range s.Fields {
		f.newline()
		// A field's own `#{...}` annotations (`#{inline}`) are written inline,
		// exactly as they are spelled in source. They live in the semantic
		// side-table keyed by the field node; attachedAnnotations does NOT cover
		// them because a field is not a statement. Omitting them here dropped
		// `#{inline}` on every `no fmt -w`, which silently changed the field's
		// layout on the next build.
		if anns := f.fieldAnnotations(field); len(anns) > 0 {
			f.write("#{")
			for i, e := range anns {
				if i > 0 {
					f.write(", ")
				}
				f.write(e.String())
			}
			f.write("} ")
		}
		f.write(field.Name)
		f.write(" ")
		if field.IsSlice {
			// When the field type is itself a SliceType (e.g. [][]i64), Type.String()
			// already includes the leading "[]". Writing an extra "[]" here would
			// cause non-idempotent formatting: [][]i64 → [][][]i64 → [][][][]i64.
			if _, isSliceType := field.Type.(*parser.SliceType); isSliceType {
				f.write(field.Type.String())
			} else {
				f.write("[]")
				if field.Type != nil {
					f.write(field.Type.String())
				}
			}
		} else if field.ArraySize > 0 {
			// When the field type is itself an ArrayType (e.g. [16][16]byte),
			// Type.String() already includes the leading "[N]". Writing an extra
			// "[N]" would cause non-idempotent formatting.
			if _, isArrayType := field.Type.(*parser.ArrayType); isArrayType {
				f.write(field.Type.String())
			} else {
				f.writef("[%d]", field.ArraySize)
				if field.Type != nil {
					f.write(field.Type.String())
				}
			}
		} else {
			if field.Type != nil {
				f.write(field.Type.String())
			}
		}
		if field.ReadOnly {
			f.write(" read-only")
		}
		if field.Sealed {
			f.write(" sealed")
		}
	}
	f.indent--
	f.newline()
	f.write("}")
}

func (f *formatter) formatEnumDefinition(s *parser.EnumDefinition) {
	f.write(s.Name)
	f.write(" {")
	f.indent++
	for _, v := range s.Values {
		f.newline()
		f.write(v.Name)
		// 只在源碼確實寫了 `= <int>` 時輸出值；自動編號（red, green, blue）不輸出，
		// 以免 formatter 把簡單枚舉篡改成 red, green = 1, blue = 2。
		if v.Explicit {
			f.write(" = ")
			f.write(strconv.FormatInt(v.Value, 10))
		}
		f.write(",")
	}
	f.indent--
	f.newline()
	f.write("}")
}

func (f *formatter) formatTaggedEnumDefinition(s *parser.TaggedEnumDefinition) {
	f.write(s.Name)
	f.write(" {")
	f.indent++
	for _, v := range s.Variants {
		f.newline()
		f.write(v.Name)
		if len(v.Fields) > 0 {
			// 括號載荷欄位：ok(v t) / err(e str) / rect(w f64, h f64)
			f.write("(")
			for i, fld := range v.Fields {
				if i > 0 {
					f.write(", ")
				}
				if fld.Name != "" {
					f.write(fld.Name)
					f.write(" ")
				}
				if fld.Type != nil {
					f.write(fld.Type.String())
				}
			}
			f.write(")")
		} else if v.Type != nil {
			// 舊式空格分隔型別：val i64
			f.write(" ")
			f.write(v.Type.String())
		}
		f.write(",")
	}
	f.indent--
	f.newline()
	f.write("}")
}

func (f *formatter) formatInterfaceDefinition(s *parser.InterfaceDefinition) {
	f.write(s.Name)
	if len(s.Implements) > 0 {
		f.write(" ")
		f.write(strings.Join(s.Implements, ", "))
	}
	f.write(" {")
	f.indent++
	for _, m := range s.Methods {
		f.newline()
		// Generic-receiver form: t.method(...)
		if m.IsGenericReceiver {
			f.write(m.Receiver)
			f.write(".")
		}
		f.write(m.Name)
		f.write("(")
		f.formatParameters(m.Parameters, m.IsVariadic)
		f.write(")")
		// Optional result declaration: (res type)
		if len(m.Results) > 0 {
			f.write(" (")
			f.formatParameters(m.Results, false)
			f.write(")")
		}
	}
	f.indent--
	f.newline()
	f.write("}")
}
