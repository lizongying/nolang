package checker

import "testing"

// builtin_struct_decl_test.go — 兩條門禁的行為釘：
//
//	① 用內建佈局名（str / vec / txt / arr）宣告 struct 卻沒帶 `#{buildin}` ⇒ ERROR
//	② 給非內建佈局名貼 `#{buildin}` ⇒ ERROR
//
// 兩條都是「成對」規則的兩半：build 的模組前綴與 MIR 的 collectStructFields
// 靠「`#{buildin}` + 內建名」這個組合識別佈局文檔聲明。缺一半，識別就失效，
// 一份影子佈局會悄悄蓋掉編譯器的真實佈局（%txt_txt / unknown field）。

func fireBuiltinStructDecl(t *testing.T, src string) []ValidateResult {
	t.Helper()
	return ValidateBuiltinStructDecls(parseProg(t, src))
}

func TestValidateBuiltinStructDecls_RejectsReservedName(t *testing.T) {
	cases := map[string]string{
		"str": `str {
    len u32
    cap u32
    data []byte
}`,
		"txt": `txt {
    data [255]byte
    len u8
}`,
		// vec / arr 同理：名字是保留的內建佈局名。
		"vec": `vec {
    len u32
    cap u32
    data i64
}`,
		// arr 同理：名字是保留的內建佈局名。
		"arr": `arr {
    len i64
}`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			res := fireBuiltinStructDecl(t, src)
			if len(res) != 1 {
				t.Fatalf("got %d diagnostics, want 1: %+v", len(res), res)
			}
			if res[0].TraceID != builtinStructRedeclTraceID {
				t.Errorf("trace = %q, want %q", res[0].TraceID, builtinStructRedeclTraceID)
			}
			if res[0].Line != 1 {
				t.Errorf("line = %d, want 1 (the struct declaration line)", res[0].Line)
			}
		})
	}
}

func TestValidateBuiltinStructDecls_RejectsMeaninglessAnnotation(t *testing.T) {
	src := `#{buildin}
foo {
    a i64
}`
	res := fireBuiltinStructDecl(t, src)
	if len(res) != 1 {
		t.Fatalf("got %d diagnostics, want 1: %+v", len(res), res)
	}
	if res[0].TraceID != builtinStructAnnotTraceID {
		t.Errorf("trace = %q, want %q", res[0].TraceID, builtinStructAnnotTraceID)
	}
}

// TestBareTypeName 釘住「判定只看最後一段」：模組前綴階段之後，使用者寫的
// `struct str` 會變成 `m.str`（見 build/module_prefix.go），此時裸名仍是
// 內建名，兩條門禁都必須照樣命中。這個形態無法用源碼文本構造（parser 不接受
// 點分 struct 名），所以直接測 helper。
func TestBareTypeName(t *testing.T) {
	cases := map[string]string{
		"str":     "str",
		"m.str":   "str",
		"net.txt": "txt",
		"a.b.c":   "c",
		"foo":     "foo",
		"":        "",
	}
	for in, want := range cases {
		if got := bareTypeName(in); got != want {
			t.Errorf("bareTypeName(%q) = %q, want %q", in, got, want)
		}
	}
	if !reservedStructName(bareTypeName("m.txt")) {
		t.Error("m.txt must still be recognised as the reserved builtin name txt")
	}
	if reservedStructName(bareTypeName("m.err")) {
		t.Error("m.err must NOT be reserved: err is a real std struct")
	}
}

// TestValidateBuiltinStructDecls_AcceptsValidPairs 是「不誤報」的一半：
// src/std 的四份佈局聲明、以及完全不相關的普通 struct 都必須放行。
func TestValidateBuiltinStructDecls_AcceptsValidPairs(t *testing.T) {
	cases := map[string]string{
		"str_decl": `#{buildin}
str {
    len u32
    cap u32
    data []byte
}`,
		"txt_decl": `#{buildin}
txt {
    data [255]byte
    len u8
}`,
		"plain_struct": `foo {
    a i64
}`,
		// err 是本模組定義的真實 struct（std/err.no），佈局來自自己的欄位，
		// 因此既不帶 `#{buildin}` 也不算冒用內建名。這兩條同時把
		// reservedStructName 的「不含 err / option / any / hashmap-*」釘住。
		"err_struct": `err {
    code i64
    msg str
}`,
		"hashmapish": `hashmap-str-i64 {
    len i64
}`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if res := fireBuiltinStructDecl(t, src); len(res) != 0 {
				t.Fatalf("got %d unexpected diagnostics: %+v", len(res), res)
			}
		})
	}
}

// TestLintStepsIncludeBuiltinStructDecl 確認這條檢查真的掛在 lint 軌上：
// 只寫了 ValidateXxx 而忘了登記 lintSteps，`no vet` / LSP 就永遠看不到它。
func TestLintStepsIncludeBuiltinStructDecl(t *testing.T) {
	found := false
	for _, step := range lintSteps {
		if step.name == "builtin-struct-decl" {
			found = true
			if !step.surface {
				t.Error("builtin-struct-decl must be on the surface track: it only looks " +
					"at declaration shape, and the LSP/vet path needs it without THIR")
			}
		}
	}
	if !found {
		t.Fatal(`lintSteps has no "builtin-struct-decl" step; the check would be dead code`)
	}

	prog := parseProg(t, `#{buildin}
foo {
    a i64
}`)
	if res := RunSurfaceLints(prog, LintOptions{}); len(res) == 0 {
		t.Fatal("RunSurfaceLints produced no diagnostic for a meaningless `#{buildin}`")
	} else {
		seen := false
		for _, r := range res {
			if r.TraceID == builtinStructAnnotTraceID {
				seen = true
			}
		}
		if !seen {
			t.Fatalf("RunSurfaceLints did not surface %s: %+v", builtinStructAnnotTraceID, res)
		}
	}
}
