package build

import (
	"regexp"
	"strings"
	"testing"
)

func TestZZPC7IR(t *testing.T) {
	src := `v = (data []byte) (r i64) {
    n = data.len() / 16
    print('before')
    n: {
        ok -> print('okarm')

        nil -> print('nilarm')

        err -> print('errarm')
    }
    print('after')
    r = 0
}

d []byte = with-len(176)
v(d)
`
	tr := NewTranspiler(nil)
	ir, err := tr.Compile(src)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	for _, want := range []string{"before", "after", "okarm", "nilarm", "errarm"} {
		t.Logf("%-8s present=%v", want, strings.Contains(ir, want))
	}
	// 印出 v 函数体
	re := regexp.MustCompile(`(?s)define [^\n]*@v\(.*?\n\}`)
	m := re.FindString(ir)
	t.Logf("\n--- v() IR ---\n%s", m)
}
