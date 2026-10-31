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

// scanFile extracts anchored doc blocks from one source file.
func scanFile(path, src string) []Entry {
	lines := strings.Split(src, "\n")
	var out []Entry
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
		// A doc comment is attached to the statement below it. A run followed by
		// a blank line or by another comment is a free-standing note, not a doc
		// comment, and the parser would not hang it on a node — so neither do we.
		if i >= len(lines) {
			continue
		}
		if t := strings.TrimSpace(lines[i]); t == "" {
			continue
		}
		block, anchor, hasLink := parser.SplitDocBlock(texts)
		if !hasLink || block == "" {
			continue
		}
		// 1-based line of the link itself, which is the run's last line.
		out = append(out, Entry{Anchor: anchor, Text: block, File: path, Line: start + len(texts)})
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
