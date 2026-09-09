package fmt

import (
	"os"
	"strings"
	"testing"
)

func DumpZstdF1() {
	src, _ := os.ReadFile("../std/archive/zstd.no")
	f1 := Format(string(src))
	a := strings.Split(f1, "\n")
	out, _ := os.Create("/tmp/zstd_f1.txt")
	defer out.Close()
	for i := 385; i < 402; i++ {
		out.WriteString(strings.Repeat(" ", 4-len(itoa(i+1))) + itoa(i+1) + ": " + a[i] + "\n")
	}
}
func itoa(n int) string {
	if n == 0 { return "0" }
	neg := n < 0
	if neg { n = -n }
	var b []byte
	for n > 0 { b = append([]byte{byte('0' + n%10)}, b...); n /= 10 }
	if neg { b = append([]byte{'-'}, b...) }
	return string(b)
}

func TestRunDumpZstdF1(t *testing.T) { DumpZstdF1() }
