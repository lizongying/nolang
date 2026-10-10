package dump

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// corpusRoot is the test corpus, relative to src/parser/dump.
const corpusRoot = "../../../tests"

// parseCorpus parses every *.no under root, returning the programs that parsed
// cleanly. Files with parse errors are skipped: `no ast` exits non-zero on them
// and the L2 harness never compares them.
func parseCorpus(t *testing.T) map[string]*parser.Program {
	t.Helper()
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("corpus not found at %s: %v", corpusRoot, err)
	}
	out := map[string]*parser.Program{}
	err := filepath.WalkDir(corpusRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".no") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		l := lexer.New(string(src))
		p := parser.New(l)
		p.Filename = filepath.Base(path)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			return nil
		}
		out[path] = prog
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", corpusRoot, err)
	}
	if len(out) == 0 {
		t.Skipf("no parseable .no files under %s", corpusRoot)
	}
	return out
}

// TestCorpusHasNoUnknownNodes is the tripwire for dump rule 1: a node type
// missing from the dumpNode switch prints `<unknown *parser.X>` and its whole
// subtree vanishes, which silently weakens the L2 oracle (see
// docs/design/nolang-selfhost.md §5.3.1). If this test fails, add the missing
// case — do NOT relax the test.
func TestCorpusHasNoUnknownNodes(t *testing.T) {
	programs := parseCorpus(t)

	var offenders []string
	badTypes := map[string]int{}

	for path, prog := range programs {
		for _, opts := range []Options{
			{},
			{Annotations: true, Sem: prog.Sem},
		} {
			out := DumpWithOptions(prog, opts)
			if !strings.Contains(out, "<unknown ") {
				continue
			}
			offenders = append(offenders, path)
			for _, line := range strings.Split(out, "\n") {
				if i := strings.Index(line, "<unknown "); i >= 0 {
					badTypes[strings.TrimSpace(line[i:])]++
				}
			}
		}
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		types := make([]string, 0, len(badTypes))
		for k, n := range badTypes {
			types = append(types, k+" ("+strconv.Itoa(n)+")")
		}
		sort.Strings(types)
		shown := offenders
		if len(shown) > 10 {
			shown = shown[:10]
		}
		t.Errorf("dumpNode fell through to `default` on %d file(s); "+
			"those nodes' entire subtrees are invisible to L2.\n"+
			"distinct types: %s\nfirst offenders:\n  %s",
			len(offenders), strings.Join(types, ", "), strings.Join(shown, "\n  "))
	}
}

// TestDumpIsDeterministic pins rule 3. The annotation side-table is a
// map[Node]*NodeSemantics; ranging over it would make the output depend on Go's
// randomised map iteration order and the L2 comparison would flake. Dumping the
// same program twice must be byte-identical.
func TestDumpIsDeterministic(t *testing.T) {
	programs := parseCorpus(t)

	for path, prog := range programs {
		for _, opts := range []Options{
			{},
			{Annotations: true, Sem: prog.Sem},
		} {
			a := DumpWithOptions(prog, opts)
			b := DumpWithOptions(prog, opts)
			if a != b {
				t.Fatalf("dump of %s is not reproducible across two calls "+
					"(annotations=%v)", path, opts.Annotations)
			}
		}
	}
}

// TestAnnotationsFlagIsWired proves the -annotations flag is not a no-op: for
// at least one corpus file the annotation dump must add content, and every
// added line must be an annotation node.
func TestAnnotationsFlagIsWired(t *testing.T) {
	programs := parseCorpus(t)

	changed, entries := 0, 0
	for _, prog := range programs {
		plain := DumpWithOptions(prog, Options{})
		full := DumpWithOptions(prog, Options{Annotations: true, Sem: prog.Sem})
		if plain == full {
			continue
		}
		changed++
		entries += strings.Count(full, "AnnotationEntry{")
		if strings.Contains(plain, "AnnotationEntry{") {
			t.Fatalf("default dump contains annotation entries; " +
				"the side-table must only be dumped with Options.Annotations")
		}
	}

	if changed == 0 {
		t.Skip("no corpus file carries #{...} annotations; flag wiring not exercised")
	}
	if entries == 0 {
		t.Errorf("%d file(s) gained output under Options.Annotations but none of "+
			"it is an AnnotationEntry", changed)
	}
	t.Logf("annotations added output for %d file(s), %d entries", changed, entries)
}

// annotationContent reports whether a dump line carries annotation *content*
// (as opposed to the structural AnnotationStatement header, which is a normal
// AST node and must be dumped by default).
func annotationContent(line string) bool {
	return strings.Contains(line, "AnnotationEntry{") ||
		strings.Contains(line, "AnnotationBoolValue{") ||
		strings.Contains(line, "AnnotationIntValue{") ||
		strings.Contains(line, "AnnotationStringValue{") ||
		strings.Contains(line, "AnnotationIdentValue{") ||
		strings.Contains(line, "AnnotationArrayValue{") ||
		strings.Contains(line, "AnnotationStructValue{") ||
		strings.Contains(line, "AnnotationRangeValue{") ||
		strings.Contains(line, "Annotations{entries")
}

// TestNoAnnotationsByDefault pins the P0-③ acceptance criterion: the default
// `no ast` output must not leak the annotation side-table. The structural
// AnnotationStatement node (the `#{...}` line itself) is still dumped — it is
// an ordinary AST node — but no entry or value may appear.
func TestNoAnnotationsByDefault(t *testing.T) {
	programs := parseCorpus(t)
	for path, prog := range programs {
		out := DumpWithOptions(prog, Options{})
		for _, l := range strings.Split(out, "\n") {
			if annotationContent(l) {
				t.Errorf("%s: default dump leaks annotation content: %q", path, strings.TrimSpace(l))
				break
			}
		}
	}
}
