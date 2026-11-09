// Package docscan collects source-comment translations from nolang sources.
//
// It exists because a doc comment is the one kind of user-facing text that is
// NOT written at a Go call site: `; [get](#args.get)` lives in a .no file, so
// msgscan (which walks Go ASTs) cannot see it, and the catalog generator would
// otherwise have no way to learn the key.
//
// Why raw text and not the parsed AST: the key is the anchor the author wrote
// (`args.get`), so both sides only READ it — there is nothing to derive and
// therefore nothing that can disagree. What can still disagree is the BLOCK: the
// text the anchor stands for. That is why the marker is required to be the LAST
// line of a `;` run: "the lines above the link" is then the only possible reading
// on either side, with no doc-group merging rules to replicate.
//
// 兩種錨點，對應 parser 的兩種歸屬：
//   - `#module.member`（含 `.`）：成員 doc，緊貼宣告上方，掛在該陳述的 Doc；
//   - `#module`（無 `.`）：**模組 doc**，是檔案最前面那段註解，parser 掛在
//     Program.Doc（見 parser.takeModuleDoc）。它與下方程式碼之間照例隔一個空行，
//     故不受「緊貼陳述」規則約束。
//
// The failure modes are asymmetric, and deliberately so:
//   - over-collecting (a run that the parser would not attach as a Doc group)
//     yields a catalog entry no hover ever looks up — dead weight, caught by the
//     orphan check in the catalog gate;
//   - under-collecting would yield a missing translation that degrades to the
//     source text with no signal at all.
package docscan

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// Entry is one linked doc block.
type Entry struct {
	Anchor string // the `#anchor` the author wrote
	Text   string // the comment lines above the link, joined with "\n"
	File   string // for error messages and review; not part of the key
	Line   int    // 1-based line of the link itself
}

// Scan walks root for `.no` files and returns every anchored doc block.
// Directories whose name starts with "." or "_" are skipped, as are the
// generated `bin`/`dist`/`node_modules` trees.
func Scan(root string) ([]Entry, error) {
	var out []Entry
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
				name == "bin" || name == "dist" || name == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".no") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out = append(out, scanFile(path, string(data))...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Anchor != out[j].Anchor {
			return out[i].Anchor < out[j].Anchor
		}
		return out[i].File < out[j].File
	})
	return out, nil
}

// commentText reports the text of a `;` doc-comment line: everything after the
// marker, which is byte-for-byte what the lexer stores in Comment.Text.
//
// `;;` is rejected (see parser.CommentCarriesDocLink): a block comment's interior
// lines carry no marker at all in the raw source, so accepting them would make
// this scan see a different set of lines than the parser's Doc group does.
func commentText(line string) (string, bool) {
	t := strings.TrimLeft(line, " \t\r")
	if len(t) < 2 || t[0] != ';' || t[1] == ';' {
		return "", false
	}
	return strings.TrimSuffix(t[1:], "\r"), true
}

// leadingModuleDoc 抽出檔案開頭的**模組 doc**（`; [arr](#arr)` 收尾那段），與
// parser.takeModuleDoc 用同一套判定：從檔案頭開始、跳過空行、一路收集 `;` 行，
// 遇到第一條「無點」錨點行為止；碰到程式碼就表示這個檔案沒有模組 doc。
//
// 為什麼不能沿用下面那個 run 掃描：模組段允許空行分段，而 std 的檔案頭更常用
// **裸 `;` 行**當段落分隔（`;` 後什麼都沒有），commentText 對這種行回 false
// （它要求 marker 之後至少有一個字元）——照 run 規則走會在第二行就斷開，錨點
// 永遠看不到。這裡因此自己走行、把裸 `;` 視為空段落，不做 `;;` 判定以外的花樣。
func leadingModuleDoc(path string, lines []string) (Entry, bool) {
	var texts []string
	for idx := 0; idx < len(lines); idx++ {
		raw := strings.TrimRight(lines[idx], "\r")
		if strings.TrimSpace(raw) == "" {
			continue // 空行：模組段可以分段
		}
		t := strings.TrimLeft(raw, " \t")
		if len(t) < 1 || t[0] != ';' || strings.HasPrefix(t, ";;") {
			return Entry{}, false // 程式碼（或區塊註解）先到：沒有模組錨點
		}
		text := t[1:]
		texts = append(texts, text)
		_, anchor, ok := parser.ParseDocLinkLine(text)
		if !ok || strings.Contains(anchor, ".") {
			continue
		}
		// 錨點行必須是這一段的最後一行（「注解在下」），故到此為止。
		block, _, _ := parser.SplitDocBlock(texts)
		if block == "" {
			return Entry{}, false
		}
		return Entry{Anchor: anchor, Text: block, File: path, Line: idx + 1}, true
	}
	return Entry{}, false
}

// scanFile extracts anchored doc blocks from one source file.
func scanFile(path, src string) []Entry {
	lines := strings.Split(src, "\n")
	var out []Entry
	// 模組 doc 先收，且只認檔案開頭那一段。
	if e, ok := leadingModuleDoc(path, lines); ok {
		out = append(out, e)
	}
	for i := 0; i < len(lines); i++ {
		if _, ok := commentText(lines[i]); !ok {
			continue
		}
		// Collect the maximal run of `;` lines starting here.
		start := i
		var texts []string
		for i < len(lines) {
			t, ok := commentText(lines[i])
			if !ok {
				break
			}
			texts = append(texts, t)
			i++
		}
		block, anchor, hasLink := parser.SplitDocBlock(texts)
		if !hasLink || block == "" {
			continue
		}
		// 無點錨點只可能是模組 doc，且只認檔案開頭（見 leadingModuleDoc）；
		// 出現在其他位置就沒有節點會對應它，收進來只會是孤兒鍵。
		if !strings.Contains(anchor, ".") {
			continue
		}
		// A doc comment is attached to the statement below it. A run followed by
		// a blank line or by another comment is a free-standing note, not a doc
		// comment, and the parser would not hang it on a node — so neither do we.
		if i >= len(lines) {
			continue
		}
		if t := strings.TrimSpace(lines[i]); t == "" {
			continue
		}
		// 1-based line of the link itself, which is the run's last line.
		out = append(out, Entry{Anchor: anchor, Text: block, File: path, Line: start + len(texts)})
	}
	return out
}

// Anchors returns the set of anchors the corpus declares, anchor -> true. The
// alias catalogs (`<tag>-alias.noml`) are keyed by exactly this set restricted
// to the standard library, so a gate can demand every alias key exists without
// re-checking the block text (an anchor may legitimately map to two texts in
// different modules; Texts would reject that, aliases do not care).
func Anchors(entries []Entry) map[string]bool {
	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		out[e.Anchor] = true
	}
	return out
}

// Texts collapses entries into anchor -> text, failing when one anchor carries
// two different texts. Two identical blocks under one anchor in different files
// share one entry, which is the intended outcome: the same paragraph should be
// translated once.
func Texts(entries []Entry) (map[string]string, error) {
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		if prev, ok := out[e.Anchor]; ok && prev != e.Text {
			return nil, &ConflictError{Anchor: e.Anchor, A: prev, B: e.Text, File: e.File, Line: e.Line}
		}
		out[e.Anchor] = e.Text
	}
	return out, nil
}

// ConflictError reports two different texts claiming one anchor. It is a hard
// error rather than a warning: a translation is keyed by the anchor, so the two
// texts would silently share one translation and one of them would be wrong.
type ConflictError struct {
	Anchor string
	A, B   string
	File   string
	Line   int
}

func (e *ConflictError) Error() string {
	return "doc anchor #" + e.Anchor + " maps to two different texts:\n  " + e.A + "\n  " + e.B +
		"\n  (second at " + e.File + ":" + strconv.Itoa(e.Line) + ")"
}
