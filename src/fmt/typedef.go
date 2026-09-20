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

// variantAnnotations 返回 parser 語義副表為標籤列舉變體記錄的 `#{...}` 條目。
// 變體不是陳述式，attachedAnnotations（僅切換陳述式型別）看不到它們，故獨立查詢。
func (f *formatter) variantAnnotations(v *parser.TaggedEnumVariant) []*parser.AnnotationEntry {
	if f.sem == nil || v == nil {
		return nil
	}
	return f.sem.AnnotationsOf(v)
}

// valueAnnotations 返回 parser 語義副表為 C 風格列舉值記錄的 `#{...}` 條目。
// 列舉值不是陳述式，attachedAnnotations（僅切換陳述式型別）看不到它們，故獨立查詢。
func (f *formatter) valueAnnotations(v *parser.EnumValue) []*parser.AnnotationEntry {
	if f.sem == nil || v == nil {
		return nil
	}
	return f.sem.AnnotationsOf(v)
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
		// A field's own `#{...}` annotations (`#{inline}`) are written as a
		// trailing annotation at the end of the field line. They live in the
		// semantic side-table keyed by the field node; attachedAnnotations does
		// NOT cover them because a field is not a statement. Omitting them here
		// dropped `#{inline}` on every `no fmt -w`, which silently changed the
		// field's layout on the next build. The leading form (`#{inline} a pt`)
		// is a parse error now, so the trailing form is the only spelling to emit.
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
		// 欄位註解以「同一行尾隨」輸出（`a pt #{inline}`）：位置規則只允許獨立
		// 成行置於目標上方或寫在目標同一行後方，前綴寫法已是解析錯誤。
		if anns := f.fieldAnnotations(field); len(anns) > 0 {
			f.write(" #{")
			for i, e := range anns {
				if i > 0 {
					f.write(", ")
				}
				f.write(e.String())
			}
			f.write("}")
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
		// 列舉值註解以「同一行尾隨」輸出（`red #{a},`）：位置規則只允許獨立
		// 成行置於值上方或寫在值同一行後方，前綴寫法已是解析錯誤。
		if anns := f.valueAnnotations(v); len(anns) > 0 {
			f.write(" #{")
			for i, e := range anns {
				if i > 0 {
					f.write(", ")
				}
				f.write(e.String())
			}
			f.write("}")
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
		// 變體註解以「同一行尾隨」輸出（`ok(v i64) #{inline},`）：位置規則只允許
		// 獨立成行置於變體上方或寫在變體同一行後方，前綴寫法已是解析錯誤。
		if anns := f.variantAnnotations(v); len(anns) > 0 {
			f.write(" #{")
			for i, e := range anns {
				if i > 0 {
					f.write(", ")
				}
				f.write(e.String())
			}
			f.write("}")
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
