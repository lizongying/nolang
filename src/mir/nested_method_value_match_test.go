package mir_test

import (
	"testing"

	"github.com/lizongying/nolang/build"
)

// A value match inside nested method traversals must preserve the payload value
// when that value is consumed by a later call in the same block.
func TestNestedMethodTraversalValueMatchFeedsPush(t *testing.T) {
	src := `column {
    name str
}

empty-column = () (out column) {
    out = column{
        name: ''
    }
}

holder {
    columns []column
}

empty-holder = () (out holder) {
    out = holder{
        columns: []
    }
}

engine {
    sources []holder
    output-names []str
}

engine.expand = (source-index i64, column-index i64) {
    i <- [0.. .sources.len()): {
        source = .sources[i]: {
            ok -> it

            -> empty-holder()
        }
        j <- [0..source.columns.len()): {
            column = source.columns[j]: {
                ok -> it

                -> empty-column()
            }
            .output-names.push(column.name)
        }
    }
}

run = () {
    e = engine{
        sources: []
        output-names: []
    }
    e.expand(0, 0)
}

run()
`
	tr := build.NewTranspiler(nil)
	if _, err := tr.Compile(src); err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
}
