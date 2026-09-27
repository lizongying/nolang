//go:build !wasm

package build

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
)

// TestMonomorphPreservesOverflowAnnotation 是单态化克隆丢失 #{overflow} 行注解的回归测试。
//
// 复现场景：test/std/arr.no 里 `a = [1,2,3]; a.sum()` 会单态化出 vec.no `[]t.sum` 的具体
// 实例；sum 体内 `acc = acc + .[i]` 上方带有 `#{overflow=wrap}` 行注解（解析期写入
// 语句节点的 OverflowMode 字段）。substituteStmt 克隆函数体时新建语句节点却未复制
// OverflowMode，导致 merged program 中的克隆体被 ovfhndld / ovf-int-default 误报
// （src/std/vec.no:396），与单档 vet（无克隆、字段完好）自相矛盾。
//
// 回归保障：substituteStmt 必须把 OverflowMode 携带到单态化克隆体。
func TestMonomorphPreservesOverflowAnnotation(t *testing.T) {
	path := filepath.Join("..", "..", "test", "std", "arr.no")
	lints, err := VetFileWithLints(path, BuildOptions{})
	if err != nil {
		t.Fatalf("VetFileWithLints(%s) 返回 error: %v", path, err)
	}
	for _, l := range lints {
		if l.Severity != checker.LintError || l.Source != "nolang-overflow" {
			continue
		}
		if strings.Contains(l.File, "std") {
			t.Errorf("std 已标 #{overflow} 的整数运算在单态化克隆中被误报: %s:%d:%d %s [%s]",
				l.File, l.Line, l.Column, l.Message, l.TraceID)
		}
	}
}
