package fmt

import "testing"

// `no fmt` 合併「先宣告型別、緊接一行賦值」的相鄰陳述。
//
// 語義等價性由 hir2mir 的宣告型別拓寬保證（見 src/build/decl_widen_test.go）：
// `v i64` + `v = .` 與 `v i64 = .` 產生同樣的存儲與算術域，因此這只是排版變化。
func TestFoldDeclAssignMergesAdjacentPair(t *testing.T) {
	src := "m = () (out str) {\n    v i64\n    v = .\n    out = v.to-str()\n}\n"
	want := "m = () (out str) {\n    v i64 = .\n    out = v.to-str()\n}\n"

	out, errs := FormatFileWithErrors(src)
	if len(errs) > 0 {
		t.Fatalf("FormatFileWithErrors errors: %v", errs)
	}
	if out != want {
		t.Fatalf("formatter output mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, out)
	}
	if out2, _ := FormatFileWithErrors(out); out2 != out {
		t.Fatalf("not idempotent:\n--- pass1 ---\n%s\n--- pass2 ---\n%s", out, out2)
	}
}

// 不合併的情形：中間有空行（宣告與初始化被刻意分開）、名字不同、型別為 option
// 或切片（NullableType / SliceType 不在本規則範圍內）、第二條帶 doc 註解。
func TestFoldDeclAssignLeavesSeparatedForms(t *testing.T) {
	src := "m = (a i32) (out str) {\n" +
		"    v i64\n" + // 有空行 ⇒ 不合併
		"\n" +
		"    v = a\n" +
		"    x i64\n" + // 名字不同
		"    y = a\n" +
		"    o ?i64\n" + // option 型別
		"    o = a\n" +
		"    s []i64\n" + // 切片
		"    s = [1, 2]\n" +
		"    z i64\n" + // 第二條帶 doc 註解
		"    ; 賦值說明\n" +
		"    z = a\n" +
		"    out = 'ok'\n" +
		"}\n"

	// 唯一的差異是節點頭規則在帶 doc 的陳述上方插入的空行（既有行為），
	// 其餘每一對都必須保持兩行。
	want := "m = (a i32) (out str) {\n" +
		"    v i64\n" +
		"\n" +
		"    v = a\n" +
		"    x i64\n" +
		"    y = a\n" +
		"    o ?i64\n" +
		"    o = a\n" +
		"    s []i64\n" +
		"    s = [1, 2]\n" +
		"    z i64\n" +
		"\n" +
		"    ; 賦值說明\n" +
		"    z = a\n" +
		"    out = 'ok'\n" +
		"}\n"

	out, errs := FormatFileWithErrors(src)
	if len(errs) > 0 {
		t.Fatalf("FormatFileWithErrors errors: %v", errs)
	}
	if out != want {
		t.Fatalf("formatter must leave these pairs alone:\n--- want ---\n%s\n--- got ---\n%s", want, out)
	}
}

// 頂層不合併：`x i64` + `x = 5` 在模組層是「全域宣告 + 一次寫入」兩個獨立語義，
// 合併會改變全域初始化的求值時機。
func TestFoldDeclAssignSkipsTopLevel(t *testing.T) {
	src := "x i64\nx = 5\nprint(x)\n"

	out, errs := FormatFileWithErrors(src)
	if len(errs) > 0 {
		t.Fatalf("FormatFileWithErrors errors: %v", errs)
	}
	if out != src {
		t.Fatalf("top-level pair must not be folded:\n--- want ---\n%s\n--- got ---\n%s", src, out)
	}
}
