package checker

import (
	"strings"
	"testing"
)

// structTypeNamingViolations 回傳 ValidateNaming 對 struct 字面量型別名
// 報出的違規段（2dhoris2 的 message 形如 "'Conn' should use ..."）。
func structTypeNamingViolations(t *testing.T, src string) []string {
	t.Helper()
	prog := mustParse(t, src)
	var got []string
	for _, r := range ValidateNaming(prog) {
		if r.TraceID != "2dhoris2" {
			continue
		}
		if i := strings.Index(r.Message, "'"); i >= 0 {
			if j := strings.Index(r.Message[i+1:], "'"); j >= 0 {
				got = append(got, r.Message[i+1:i+1+j])
			}
		}
	}
	return got
}

func hasAll(got, want []string) bool {
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// TestValidateStructLiteralTypeNaming 釘住「型別名引用也要守命名規範」。
// 動機：compiler/tests/fixtures/t21-struct-dotted.no 的
// net.Conn / a.b.C / pkg.Sub.K 過去完全逃過 ValidateNaming（它只查宣告名），
// no vet 顯示 No issues found。
func TestValidateStructLiteralTypeNaming(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string // 期望被標記的違規段；nil 代表零違規
	}{
		{
			"dotted-upper",
			"c = net.Conn {\n    host: \"x\"\n}\n",
			[]string{"Conn"},
		},
		{
			"dotted-upper-empty-literal",
			"e = net.Conn {}\n",
			[]string{"Conn"},
		},
		{
			"single-letter-upper-tail",
			"d = a.b.C {\n    x: 1\n}\n",
			[]string{"C"},
		},
		{
			"two-upper-segments",
			"f = pkg.Sub.K {\n    x: 1\n    y: 2\n}\n",
			[]string{"Sub", "K"},
		},
		{
			"single-segment-upper",
			"c = MyData {}\n",
			[]string{"MyData"},
		},
		{
			"inside-function-body",
			"make-conn = () {\n    c = net.Conn {}\n}\n",
			[]string{"Conn"},
		},
		{
			"valid-kebab-single",
			"c = net.conn {\n    host: \"x\"\n}\n",
			nil,
		},
		{
			"valid-kebab-dotted",
			"f = pkg.sub.k {\n    x: 1\n    y: 2\n}\n",
			nil,
		},
		{
			"inside-call-argument",
			"v = consume(net.Conn {})\n",
			[]string{"Conn"},
		},
		{
			"all-caps-module-const-not-a-type",
			"v = base64.BASE64-STD\n",
			nil,
		},
		{
			"uppercase-method-call-not-a-type",
			"v = i64.MAX\n",
			nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := structTypeNamingViolations(t, tc.src)
			if len(tc.want) == 0 {
				if len(got) != 0 {
					t.Fatalf("expected no naming violations, got %v", got)
				}
				return
			}
			if !hasAll(got, tc.want) {
				t.Fatalf("expected violations %v, got %v", tc.want, got)
			}
			// 不允許把合法前綴（net / pkg / a / b / __gen）也報進去：
			// 違規數必須恰好等於期望段數。
			if len(got) != len(tc.want) {
				t.Fatalf("expected exactly %d violations %v, got %v", len(tc.want), tc.want, got)
			}
		})
	}
}

// TestValidateTypeNameNamingDefinitionsAndAnnotations 釘住第二個缺口：
// struct 定義名與所有源碼顯式型別標註（let / 參數 / 回傳 / 欄位 / 複合
// 型別 / cast）都要守 2dhoris2，推斷型別與內建型別不得誤報。
func TestValidateTypeNameNamingDefinitionsAndAnnotations(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{
			"struct-definition-name",
			"MyData {\n    x i64\n}\n",
			[]string{"MyData"},
		},
		{
			"struct-field-type",
			"data {\n    x SomeType\n    n i64\n}\n",
			[]string{"SomeType"},
		},
		{
			"let-annotation-single",
			"x MyData = 1\n",
			[]string{"MyData"},
		},
		{
			"let-annotation-dotted",
			"y net.Conn = nil\n",
			[]string{"Conn"},
		},
		{
			"array-annotation",
			"z [3]MyType = [1, 2, 3]\n",
			[]string{"MyType"},
		},
		{
			"nullable-annotation",
			"q ?Other = nil\n",
			[]string{"Other"},
		},
		{
			"parameter-and-result-annotations",
			"f = (p MyType, v net.Conn) (r OtherType) {\n}\n",
			[]string{"MyType", "Conn", "OtherType"},
		},
		{
			// `as` 只接受 FFI 指標型別，無法拿大寫型別名當正例；
			// 這條釘住 cast 子樹被走過且合法型別不誤報。
			"cast-ffi-pointer-not-reported",
			"v = x as *byte\n",
			nil,
		},
		{
			"struct-literal-field-type",
			"c = data {\n    x SomeType = 1\n}\n",
			[]string{"SomeType"},
		},
		{
			"valid-builtin-annotations",
			"x i64 = 1\ny str = 'a'\nz []byte = [1]\n",
			nil,
		},
		{
			"valid-kebab-annotation",
			"x my-data = 1\n",
			nil,
		},
		{
			"valid-generic-single-letter",
			"f = (v t) (r u) {\n}\n",
			nil,
		},
		{
			"inferred-types-not-reported",
			"x = 1\ny = 'a'\nz = net.conn {}\n",
			nil,
		},
		{
			"all-caps-module-const-not-a-type",
			"v = base64.BASE64-STD\n",
			nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := structTypeNamingViolations(t, tc.src)
			if len(tc.want) == 0 {
				if len(got) != 0 {
					t.Fatalf("expected no naming violations, got %v", got)
				}
				return
			}
			if !hasAll(got, tc.want) {
				t.Fatalf("expected violations %v, got %v", tc.want, got)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("expected exactly %d violations %v, got %v", len(tc.want), tc.want, got)
			}
		})
	}
}
