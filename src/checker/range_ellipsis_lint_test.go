package checker

import "testing"

// countEllipsis 回傳 ValidateRangeSelfEllipsis 對 src 產出的 rng-ellipsis 提示數。
func countEllipsis(t *testing.T, src string) int {
	t.Helper()
	prog := parseProg(t, src)
	n := 0
	for _, r := range ValidateRangeSelfEllipsis(prog, "src/app.no") {
		if r.TraceID == rngEllipsisTraceID {
			n++
		}
	}
	return n
}

// TestRangeSelfEllipsisHint：`i <- [0...count)` 的上界是隱式 self 的 `.count`，
// 與 `..` 連寫成 `...`（歧義），應發一條 rng-ellipsis HINT。
func TestRangeSelfEllipsisHint(t *testing.T) {
	src := `jar { count i64 }
jar.set = () {
    i <- [0...count): {
        i = i
    }
}
`
	if got := countEllipsis(t, src); got != 1 {
		t.Fatalf("expected 1 rng-ellipsis hint for `[0...count)`, got %d", got)
	}
}

// TestRangeSelfEllipsisMethodCall：`.len()`（隱式 self 方法）作為上界同樣形成 `...`。
func TestRangeSelfEllipsisMethodCall(t *testing.T) {
	src := `f = () {
    i <- [0...len()): {
        i = i
    }
}
`
	if got := countEllipsis(t, src); got != 1 {
		t.Fatalf("expected 1 rng-ellipsis hint for `[0...len())`, got %d", got)
	}
}

// TestRangeSelfEllipsisNoHintExplicitSelf：顯式寫 `self.count` 是建議寫法，不得提示。
func TestRangeSelfEllipsisNoHintExplicitSelf(t *testing.T) {
	src := `jar { count i64 }
jar.set = () {
    i <- [0..self.count): {
        i = i
    }
}
`
	if got := countEllipsis(t, src); got != 0 {
		t.Fatalf("explicit `..self.count` must not be hinted, got %d", got)
	}
}

// TestRangeSelfEllipsisNoHintOtherReceiver：上界以其它具名接收者（`a.len()`）開頭，
// 前面不是 `.`，無 `...` 歧義，不得提示。
func TestRangeSelfEllipsisNoHintOtherReceiver(t *testing.T) {
	src := `f = (a []i64) {
    i <- [0..a.len()): {
        i = i
    }
}
`
	if got := countEllipsis(t, src); got != 0 {
		t.Fatalf("named-receiver upper bound must not be hinted, got %d", got)
	}
}

// TestRangeSelfEllipsisWiredIntoLints 確認規則已接入 RunAllLints（no vet / LSP 路徑），
// 且來源標記為 nolang-range、嚴重度為 HINT。
func TestRangeSelfEllipsisWiredIntoLints(t *testing.T) {
	src := `jar { count i64 }
jar.set = () {
    i <- [0...count): {
        i = i
    }
}
`
	found := false
	for _, l := range RunAllLints(parseProg(t, src), LintOptions{SourcePath: "src/app.no"}) {
		if l.TraceID == rngEllipsisTraceID && l.Source == "nolang-range" && l.Severity == LintHint {
			found = true
		}
	}
	if !found {
		t.Fatal("rng-ellipsis hint must be wired into RunAllLints as nolang-range/HINT")
	}
}
