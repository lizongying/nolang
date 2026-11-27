package mir

import (
	"regexp"
	"testing"
)

// A container mutation through two struct fields must update the original
// receiver, not the temporary produced by the first field read.
func TestVecPushNestedFieldReceiverWritesThrough(t *testing.T) {
	mod := lowerForTest(t, `
inner {
    values []i64
}

outer {
    inner inner
}

append-value = (state outer) {
    state.inner.values.push(7)
}

main = () {
    state outer
    append-value(state)
    print(state.inner.values.len())
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v\n%s", err, mod.String())
	}
	body := definedBody(t, ir, "append_value")
	outerGEPs := regexp.MustCompile(`(?m)^\s*(%\S+) = getelementptr inbounds %outer, %outer\* %p0, i32 0, i32 0$`).FindAllStringSubmatch(body, -1)
	if len(outerGEPs) == 0 {
		t.Fatalf("nested field receiver must project through the outer parameter\n%s", body)
	}
	innerAddr := ""
	for _, outer := range outerGEPs {
		inner := regexp.MustCompile(`(?m)^\s*(%\S+) = getelementptr inbounds %inner, %inner\* ` + regexp.QuoteMeta(outer[1]) + `, i32 0, i32 0$`).FindStringSubmatch(body)
		if len(inner) == 2 {
			innerAddr = inner[1]
			break
		}
	}
	if innerAddr == "" {
		t.Fatalf("inner field address must be rooted at the outer parameter projection\n%s", body)
	}
	store := regexp.MustCompile(`(?m)^\s*store %vec %[^,]+, %vec\* ` + regexp.QuoteMeta(innerAddr) + `$`)
	if !store.MatchString(body) {
		t.Fatalf("vec.push did not publish its descriptor through the nested field address\n%s", body)
	}
}
