package parser

import (
	"reflect"
	"testing"
)

// newSpec 构造一个 FormatSpec，Precision 默认 -1 表示无精度
func newSpec(fill, align, sign byte, altForm, zeroPad bool, width int, grouping byte, precision int, typ byte) *FormatSpec {
	return &FormatSpec{
		Fill:      fill,
		Align:     align,
		Sign:      sign,
		AltForm:   altForm,
		ZeroPad:   zeroPad,
		Width:     width,
		Grouping:  grouping,
		Precision: precision,
		Type:      typ,
	}
}

// defaultSpec 返回全默认值的 FormatSpec
func defaultSpec() *FormatSpec {
	return newSpec(0, 0, 0, false, false, 0, 0, -1, 0)
}

func TestParseFormatString(t *testing.T) {
	t.Run("simple_literal_hello", func(t *testing.T) {
		segs, err := ParseFormatString("hello")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(segs) != 1 {
			t.Fatalf("expected 1 segment, got %d", len(segs))
		}
		if segs[0].Field != nil {
			t.Errorf("expected literal segment, got field")
		}
		if segs[0].Literal != "hello" {
			t.Errorf("Literal = %q, want %q", segs[0].Literal, "hello")
		}
	})

	t.Run("single_field_id", func(t *testing.T) {
		// '{id}' → literal "" + field{Name:"id"} + literal ""
		segs, err := ParseFormatString("{id}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(segs) != 3 {
			t.Fatalf("expected 3 segments, got %d", len(segs))
		}
		if segs[0].Field != nil || segs[0].Literal != "" {
			t.Errorf("segment[0] = %+v, want literal \"\"", segs[0])
		}
		if segs[1].Field == nil {
			t.Fatalf("segment[1] should be a field")
		}
		if segs[1].Field.Name != "id" {
			t.Errorf("Name = %q, want %q", segs[1].Field.Name, "id")
		}
		if segs[1].Field.Spec != "" {
			t.Errorf("Spec = %q, want empty", segs[1].Field.Spec)
		}
		if segs[1].Field.Parsed != nil {
			t.Errorf("Parsed should be nil for no spec")
		}
		if segs[1].Field.Pos != 0 {
			t.Errorf("Pos = %d, want 0", segs[1].Field.Pos)
		}
		if segs[2].Field != nil || segs[2].Literal != "" {
			t.Errorf("segment[2] = %+v, want literal \"\"", segs[2])
		}
	})

	t.Run("chinese_named_fields", func(t *testing.T) {
		// '編號 {id} 金額 {money}' → 至少 4 个段落
		segs, err := ParseFormatString("編號 {id} 金額 {money}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(segs) < 4 {
			t.Fatalf("expected at least 4 segments, got %d", len(segs))
		}
		// 段落 0: literal "編號 "
		if segs[0].Field != nil || segs[0].Literal != "編號 " {
			t.Errorf("segment[0] = %+v, want literal \"編號 \"", segs[0])
		}
		// 段落 1: field {id}
		if segs[1].Field == nil || segs[1].Field.Name != "id" {
			t.Errorf("segment[1] = %+v, want field {id}", segs[1])
		}
		// 段落 2: literal " 金額 "
		if segs[2].Field != nil || segs[2].Literal != " 金額 " {
			t.Errorf("segment[2] = %+v, want literal \" 金額 \"", segs[2])
		}
		// 段落 3: field {money}
		if segs[3].Field == nil || segs[3].Field.Name != "money" {
			t.Errorf("segment[3] = %+v, want field {money}", segs[3])
		}
	})

	t.Run("field_with_spec_06", func(t *testing.T) {
		// '{id:06}' → field with spec "06"
		segs, err := ParseFormatString("{id:06}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// 至少要有字段段落
		var field *FormatField
		for _, seg := range segs {
			if seg.Field != nil {
				field = seg.Field
				break
			}
		}
		if field == nil {
			t.Fatalf("no field segment found")
		}
		if field.Name != "id" {
			t.Errorf("Name = %q, want %q", field.Name, "id")
		}
		if field.Spec != "06" {
			t.Errorf("Spec = %q, want %q", field.Spec, "06")
		}
		if field.Parsed == nil {
			t.Fatalf("Parsed should not be nil")
		}
		want := newSpec('0', '=', 0, false, true, 6, 0, -1, 0)
		if !reflect.DeepEqual(field.Parsed, want) {
			t.Errorf("Parsed = %+v, want %+v", field.Parsed, want)
		}
	})

	t.Run("escape_literal_braces", func(t *testing.T) {
		// '{{literal}}' → literal "{literal}"
		segs, err := ParseFormatString("{{literal}}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(segs) != 1 {
			t.Fatalf("expected 1 segment, got %d", len(segs))
		}
		if segs[0].Field != nil {
			t.Errorf("expected literal segment")
		}
		if segs[0].Literal != "{literal}" {
			t.Errorf("Literal = %q, want %q", segs[0].Literal, "{literal}")
		}
	})

	t.Run("escape_open_brace", func(t *testing.T) {
		// '{{' → literal "{"
		segs, err := ParseFormatString("{{")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(segs) != 1 {
			t.Fatalf("expected 1 segment, got %d", len(segs))
		}
		if segs[0].Literal != "{" {
			t.Errorf("Literal = %q, want %q", segs[0].Literal, "{")
		}
	})

	t.Run("escape_close_brace", func(t *testing.T) {
		// '}}' → literal "}"
		segs, err := ParseFormatString("}}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(segs) != 1 {
			t.Fatalf("expected 1 segment, got %d", len(segs))
		}
		if segs[0].Literal != "}" {
			t.Errorf("Literal = %q, want %q", segs[0].Literal, "}")
		}
	})

	t.Run("unmatched_open_brace", func(t *testing.T) {
		// '{id' → no error, '{' treated as literal
		segs, err := ParseFormatString("{id")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(segs) == 0 || segs[0].Literal != "{id" {
			t.Errorf("expected literal '{id', got %+v", segs)
		}
	})

	t.Run("unmatched_close_brace", func(t *testing.T) {
		// 'id}' → no error, '}' treated as literal
		segs, err := ParseFormatString("id}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(segs) == 0 || segs[0].Literal != "id}" {
			t.Errorf("expected literal 'id}', got %+v", segs)
		}
	})

	t.Run("empty_braces_error", func(t *testing.T) {
		// '{}' → error empty name
		_, err := ParseFormatString("{}")
		if err == nil {
			t.Fatalf("expected error for empty name")
		}
	})

	t.Run("empty_name_with_spec_error", func(t *testing.T) {
		// '{:06}' → error empty name
		_, err := ParseFormatString("{:06}")
		if err == nil {
			t.Fatalf("expected error for empty name")
		}
	})

	t.Run("invalid_name_with_space", func(t *testing.T) {
		// '{id name}' → error invalid name (space)
		_, err := ParseFormatString("{id name}")
		if err == nil {
			t.Fatalf("expected error for invalid name with space")
		}
	})

	t.Run("hyphen_allowed_in_name", func(t *testing.T) {
		// '{a-b}' → valid (hyphen allowed)
		segs, err := ParseFormatString("{a-b}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var field *FormatField
		for _, seg := range segs {
			if seg.Field != nil {
				field = seg.Field
				break
			}
		}
		if field == nil {
			t.Fatalf("no field segment found")
		}
		if field.Name != "a-b" {
			t.Errorf("Name = %q, want %q", field.Name, "a-b")
		}
	})

	t.Run("underscore_start_name", func(t *testing.T) {
		// '{_x}' → valid (underscore start)
		segs, err := ParseFormatString("{_x}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var field *FormatField
		for _, seg := range segs {
			if seg.Field != nil {
				field = seg.Field
				break
			}
		}
		if field == nil {
			t.Fatalf("no field segment found")
		}
		if field.Name != "_x" {
			t.Errorf("Name = %q, want %q", field.Name, "_x")
		}
	})

	t.Run("unicode_chinese_name", func(t *testing.T) {
		// '{數量}' → valid (Unicode)
		segs, err := ParseFormatString("{數量}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var field *FormatField
		for _, seg := range segs {
			if seg.Field != nil {
				field = seg.Field
				break
			}
		}
		if field == nil {
			t.Fatalf("no field segment found")
		}
		if field.Name != "數量" {
			t.Errorf("Name = %q, want %q", field.Name, "數量")
		}
	})

	t.Run("empty_string", func(t *testing.T) {
		// 空字符串 → 单个空字面段落
		segs, err := ParseFormatString("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(segs) != 1 {
			t.Fatalf("expected 1 segment, got %d", len(segs))
		}
		if segs[0].Field != nil {
			t.Errorf("expected literal segment")
		}
		if segs[0].Literal != "" {
			t.Errorf("Literal = %q, want empty", segs[0].Literal)
		}
	})

	t.Run("field_pos_for_chinese_prefix", func(t *testing.T) {
		// 验证带中文前缀时字段的 Pos 为字节位置
		segs, err := ParseFormatString("編號 {id}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var field *FormatField
		for _, seg := range segs {
			if seg.Field != nil {
				field = seg.Field
				break
			}
		}
		if field == nil {
			t.Fatalf("no field segment found")
		}
		// "編號 " = 3 + 3 + 1 = 7 字节
		if field.Pos != 7 {
			t.Errorf("Pos = %d, want 7", field.Pos)
		}
	})
}

func TestParseFormatSpec(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		want    *FormatSpec
		wantErr bool
	}{
		// 空规格 → 全默认
		{name: "empty", spec: "", want: defaultSpec()},

		// 零填充 + 宽度
		{name: "zero_pad_width", spec: "06", want: newSpec('0', '=', 0, false, true, 6, 0, -1, 0)},

		// 精度 + 类型
		{name: "precision_f", spec: ".2f", want: newSpec(0, 0, 0, false, false, 0, 0, 2, 'f')},

		// 对齐 + 宽度
		{name: "align_right", spec: ">10", want: newSpec(0, '>', 0, false, false, 10, 0, -1, 0)},
		{name: "align_left", spec: "<10", want: newSpec(0, '<', 0, false, false, 10, 0, -1, 0)},
		{name: "align_center", spec: "^10", want: newSpec(0, '^', 0, false, false, 10, 0, -1, 0)},
		{name: "align_equal", spec: "=10", want: newSpec(0, '=', 0, false, false, 10, 0, -1, 0)},

		// 替代形式
		{name: "alt_form_hex", spec: "#x", want: newSpec(0, 0, 0, true, false, 0, 0, -1, 'x')},

		// 符号
		{name: "sign_plus", spec: "+", want: newSpec(0, 0, '+', false, false, 0, 0, -1, 0)},
		{name: "sign_minus", spec: "-", want: newSpec(0, 0, '-', false, false, 0, 0, -1, 0)},
		{name: "sign_space", spec: " ", want: newSpec(0, 0, ' ', false, false, 0, 0, -1, 0)},

		// 精度 + 类型 e
		{name: "precision_e", spec: ".2e", want: newSpec(0, 0, 0, false, false, 0, 0, 2, 'e')},

		// 各类型字符
		{name: "type_b", spec: "b", want: newSpec(0, 0, 0, false, false, 0, 0, -1, 'b')},
		{name: "type_o", spec: "o", want: newSpec(0, 0, 0, false, false, 0, 0, -1, 'o')},
		{name: "type_c", spec: "c", want: newSpec(0, 0, 0, false, false, 0, 0, -1, 'c')},
		{name: "type_percent", spec: "%", want: newSpec(0, 0, 0, false, false, 0, 0, -1, '%')},

		// 宽度 + 精度 + 类型
		{name: "width_precision_f", spec: "8.2f", want: newSpec(0, 0, 0, false, false, 8, 0, 2, 'f')},

		// 填充 + 对齐 + 宽度
		{name: "fill_align_width", spec: "*^10", want: newSpec('*', '^', 0, false, false, 10, 0, -1, 0)},

		// 零填充标志 + 显式对齐
		{name: "zero_pad_explicit_align", spec: "0=8", want: newSpec(0, '=', 0, false, true, 8, 0, -1, 0)},

		// 符号 + 精度 + 类型
		{name: "sign_precision_e", spec: "+.3e", want: newSpec(0, 0, '+', false, false, 0, 0, 3, 'e')},

		// 替代形式 + 零填充 + 宽度 + 类型
		{name: "alt_zero_width_hex", spec: "#08x", want: newSpec('0', '=', 0, true, true, 8, 0, -1, 'x')},

		// 错误：无效规格
		{name: "invalid_abc", spec: "abc", wantErr: true},
		// 错误：精度缺少数字
		{name: "precision_no_digits", spec: ".", wantErr: true},
		// 错误：精度后非数字
		{name: "precision_non_digit", spec: ".x", wantErr: true},
		// 错误：类型后多余字符
		{name: "type_extra_char", spec: "xy", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFormatSpec(tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseFormatSpec(%q) expected error, got nil", tt.spec)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseFormatSpec(%q) unexpected error: %v", tt.spec, err)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseFormatSpec(%q) = %+v, want %+v", tt.spec, got, tt.want)
			}
		})
	}
}
