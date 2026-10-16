package dump

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestS3Probe(t *testing.T) {
	path := os.Getenv("HDIFF")
	src, _ := os.ReadFile(path)
	a, _ := lowerToHIR(path, string(src), false)
	b, _ := lowerToHIRSkipOnly(path, string(src))
	la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
	fmt.Printf("shipped=%d target=%d\n", len(la), len(lb))
	n := len(la)
	if len(lb) < n { n = len(lb) }
	shown := 0
	for i := 0; i < n && shown < 10; i++ {
		if la[i] != lb[i] {
			fmt.Printf("L%-4d SHIP: %s\n      TGT : %s\n", i, la[i], lb[i])
			shown++
		}
	}
}
