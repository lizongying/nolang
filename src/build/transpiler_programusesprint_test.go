package build

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestProgramUsesPrintDetectsLoopAndBlockBodies 是針對 2026-09-07 修復的迴歸測試。
//
// 背景：nolang 已從 alwaysAutoLoadStd 整模組無條件載入遷移到 on-demand
// reachability 載入 std 模組。是否引入 fmt/io/str/byte 由 programUsesPrint
// 掃描 print 家族用法決定。該函數的 walker 曾漏掉 ForStatement 與
// BlockStatement，導致 for 迴圈本體或 { } 區塊內的 print 呼叫無法被偵測
// → fmt 模組不載入 → codegen 仍發出 @fmt-int 裸呼叫 → opt 報
// "use of undefined value '@fmt-int'"。
//
// 本測試直接驗證 programUsesPrint 能正確下鑽各類容器並偵測 print 用法；
// 若 walker 再次漏掉某種容器，對應子測試會失敗。
func TestProgramUsesPrintDetectsLoopAndBlockBodies(t *testing.T) {
	parse := func(src string) *parser.Program {
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Fatalf("parse errors for src %q: %v", src, errs)
		}
		return prog
	}
	usesPrint := func(src string) bool {
		prog := parse(src)
		tp := NewTranspiler(nil)
		return tp.programUsesPrint(prog)
	}

	cases := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "for-loop body calls print",
			src: `main = () {
    s = 0
    i <- [0..10): {
        s = s + i
        print(s)
    }
}`,
			want: true,
		},
		{
			name: "do-while body calls print",
			src: `main = () {
    i = 0
    {
        print(i)
        i = i + 1
    } (i < 5)
}`,
			want: true,
		},
		{
			name: "bare block statement calls print",
			src: `main = () {
    {
        print('hello')
        print('world')
    }
}`,
			want: true,
		},
		{
			name: "nested if inside for-loop calls print",
			src: `main = () {
    i <- [0..5): {
        if i == 3 {
            print(i)
        }
    }
}`,
			want: true,
		},
		{
			name: "print in function called from loop body",
			src: `main = () {
    i <- [0..5): {
        show(i)
    }
}
show = (x i64) {
    print(x)
}`,
			want: true,
		},
		{
			name: "no print anywhere",
			src: `main = () {
    s = 0
    i <- [0..10): {
        s = s + i
    }
}`,
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := usesPrint(tc.src)
			if got != tc.want {
				t.Errorf("programUsesPrint = %v, want %v", got, tc.want)
			}
		})
	}
}
