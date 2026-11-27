package parser

import (
	"regexp"
	"strings"
	"testing"
)

const enumIndexSubjectSrc = `cell {
    sql-null,
    integer(v i64),
    text(v str)
}

row {
    values []cell
}

read-cell = (source row, index i64) (out i64) {
    out = 0
    source.values[index]: {
        text(value) -> out = value.len-bytes()

        integer(value) -> out = value

        -> out = -1
    }
}

main = () {
    source = row{}
    source.values.push(integer(2))
    source.values.push(text('BETA'))
    print(read-cell(source, 1))
}
`

func TestSafeIndexEnumSubjectNormalisesAndBindsPayloads(t *testing.T) {
	dump := dumpHIR(t, enumIndexSubjectSrc)

	for _, want := range []string{
		"let s=__match_subj_",
		"t=?cell",
		"let s=__match_value_",
		"t=cell",
		"let s=value t=str",
		"let s=value t=i64",
	} {
		if !strings.Contains(dump, want) {
			t.Fatalf("safe-index enum match lost %q:\n--- HIR ---\n%s", want, dump)
		}
	}
	if got := len(regexp.MustCompile(`(?m)^\s*index @`).FindAllString(dump, -1)); got != 1 {
		t.Fatalf("safe-index enum subject must be evaluated once, got %d index nodes:\n--- HIR ---\n%s", got, dump)
	}
}

func TestNestedSafeIndexEnumMatchSeesOuterArmBinding(t *testing.T) {
	dump := dumpHIR(t, `cell {
    sql-null,
    integer(v i64),
    text(v str)
}

row {
    values []cell
}

result {
    rows []row
}

make-result = () (out result) {
    source = row{}
    source.values.push(integer(2))
    source.values.push(text('BETA'))
    source.values.push(integer(12))
    out.rows.push(source)
}

run = () {
    returned = make-result()
    returned.rows[0]: {
        ok -> {
            it.values[1]: {
                text(value) -> print('name=' - value)

                -> print('name-missing')
            }
            it.values[2]: {
                integer(value) -> print('score=' - value)

                -> print('score-missing')
            }
        }

        -> print('row-missing')
    }
}
`)

	for _, want := range []string{
		"let s=it t=row",
		"t=?cell",
		"let s=__match_value_",
		"t=cell",
		"let s=value t=str",
		"let s=value t=i64",
	} {
		if !strings.Contains(dump, want) {
			t.Fatalf("nested safe-index enum match lost %q:\n--- HIR ---\n%s", want, dump)
		}
	}
	seenIndexes := map[string]bool{}
	for _, site := range regexp.MustCompile(`index @\d+:\d+`).FindAllString(dump, -1) {
		seenIndexes[site] = true
	}
	if got := len(seenIndexes); got != 3 {
		t.Fatalf("nested enum matches must retain three distinct safe-index sites, got %d:\n--- HIR ---\n%s", got, dump)
	}
}

func TestSafeIndexEnumMatchEmitsFailureFallback(t *testing.T) {
	dump := dumpHIR(t, `cell {
    sql-null,
    integer(v i64),
    text(v str)
}

row {
    values []cell
}

read-cell = (source row, index i64) (out i64) {
    out = -1
    source.values[index]: {
        text(value) -> out = value.len-bytes()

        integer(number) -> out = number

        -> {
            print('range-miss')
            out = -1
        }
    }
}
`)

	if got := strings.Count(dump, "str s=range-miss"); got != 2 {
		t.Fatalf("safe-index enum match lost its index-failure fallback; "+
			"want the default arm in both the enum chain and the outer ok guard, got %d:\n--- HIR ---\n%s", got, dump)
	}
}

func TestLocalEnumConstructorSubjectBindsPayloads(t *testing.T) {
	dump := dumpHIR(t, `sample {
    integer(v i64),
    real(v f64),
    text(v str),
}

run = () {
    value = integer(5)
    value: {
        integer(iv) -> print('integer', iv)

        real(rv) -> print('real', rv)

        -> print('fallback')
    }
}
`)

	for _, want := range []string{
		"let s=value t=sample",
		"let s=iv t=i64",
		"let s=rv t=f64",
	} {
		if !strings.Contains(dump, want) {
			t.Fatalf("local enum constructor match lost %q:\n--- HIR ---\n%s", want, dump)
		}
	}
}

func TestSafeIndexScalarValueMatchStaysPlain(t *testing.T) {
	dump := dumpHIR(t, `read = (values []i64, index i64) (out i64) {
    out = 0
    values[index]: {
        5 -> out = 1

        -> out = -1
    }
}
`)
	if strings.Contains(dump, "__match_subj_") || strings.Contains(dump, "__match_value_") {
		t.Fatalf("scalar value match must keep element-comparison semantics:\n--- HIR ---\n%s", dump)
	}
}
