package lsp

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lizongying/nolang/checker"
)

// TestLSPStressMemoryLeak simulates a long editing session: every "keystroke"
// updates the document text, re-parses (ParseDocument — the same call the
// didOpen/didChange goroutines make) and runs the lint pipeline (the
// diagnostics half of publishDocumentDiagnostics). If any cache or index on
// this path grows without bound, heap-in-use after GC keeps climbing across
// rounds and this test reports it.
func TestLSPStressMemoryLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory stress test in short mode")
	}

	const (
		rounds      = 40
		strokes     = 25 // parse+lint calls per round
		heapLimitMB = 512
	)

	base := strings.Repeat("x", 4096)

	// One "keystroke": mutate doc text, reparse, re-lint.
	keystroke := func(m *DocumentManager, uri string, i int) {
		text := base + "\nfn leak-check-\n\tv i64 = " + strings.Repeat("9", i%9+1) + "\n"
		if _, err := m.UpdateDocument(uri, []TextDocumentContentChange{{Text: text}}, i+1); err != nil {
			t.Fatalf("UpdateDocument: %v", err)
		}
		ast, _, err := m.ParseDocument(uri)
		if err != nil {
			t.Fatalf("ParseDocument: %v", err)
		}
		if ast != nil {
			checker.RunAllLints(ast, checker.LintOptions{
				SourcePath:      "/tmp/leak-session.no",
				RootDir:         "/tmp",
				LightweightMode: true,
			})
		}
	}

	// Warm up: pay one-time init (std sigs, HIR caches) before measuring.
	{
		m := NewDocumentManager()
		uri := "file:///tmp/leak-warmup.no"
		if _, err := m.OpenDocument(uri, base); err != nil {
			t.Fatalf("OpenDocument: %v", err)
		}
		for i := 0; i < 3; i++ {
			keystroke(m, uri, i)
		}
	}

	var prev uint64
	for r := 0; r < rounds; r++ {
		// A fresh editor session per round: new manager, open, edit, close.
		m := NewDocumentManager()
		uri := "file:///tmp/leak-session.no"
		if _, err := m.OpenDocument(uri, base); err != nil {
			t.Fatalf("OpenDocument: %v", err)
		}
		for i := 0; i < strokes; i++ {
			keystroke(m, uri, r*strokes+i)
		}
		m.RemoveDocument(uri)

		runtime.GC()
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		heapMB := ms.HeapAlloc / (1024 * 1024)
		t.Logf("round %d: HeapAlloc=%dMB (+%dMB vs prev)", r, heapMB, int64(heapMB)-int64(prev/(1024*1024)))
		if heapMB > heapLimitMB {
			t.Fatalf("heap-in-use grew to %dMB (> %dMB limit) after round %d — unbounded growth", heapMB, heapLimitMB, r)
		}
		if r >= rounds/2 && prev > 0 && heapMB > (prev/(1024*1024))+50 {
			t.Fatalf("heap keeps climbing in the plateau phase: round %d +%dMB over previous", r, heapMB-(prev/(1024*1024)))
		}
		prev = heapMB * (1024 * 1024)
	}
}
