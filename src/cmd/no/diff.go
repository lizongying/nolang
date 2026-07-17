package main

import (
	"fmt"
	"strings"
)

// ANSI color codes for diff output
const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorCyan   = "\033[36m"
	colorFaint  = "\033[2m"
)

// diffEntry represents a single line in the diff.
type diffEntry struct {
	typ   string // "equal", "add", "del"
	line  string
	oldNo int // 1-based line number in old text (0 if not present)
	newNo int // 1-based line number in new text (0 if not present)
}

// computeDiff performs a line-by-line diff using the LCS algorithm.
func computeDiff(oldText, newText string) []diffEntry {
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")

	m, n := len(oldLines), len(newLines)

	// LCS table
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}

	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if oldLines[i-1] == newLines[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrack to produce diff entries
	var entries []diffEntry
	i, j := m, n
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && oldLines[i-1] == newLines[j-1] {
			entries = append([]diffEntry{{"equal", oldLines[i-1], i, j}}, entries...)
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			entries = append([]diffEntry{{"add", newLines[j-1], 0, j}}, entries...)
			j--
		} else {
			entries = append([]diffEntry{{"del", oldLines[i-1], i, 0}}, entries...)
			i--
		}
	}
	return entries
}

// hunkRange represents a range [start, end) of entries that form a diff hunk.
type hunkRange struct {
	start, end int
}

// buildHunks groups diff entries into hunks with surrounding context lines.
func buildHunks(entries []diffEntry, contextSize int) []hunkRange {
	// Find all change indices
	var changeIdxs []int
	for i, e := range entries {
		if e.typ != "equal" {
			changeIdxs = append(changeIdxs, i)
		}
	}
	if len(changeIdxs) == 0 {
		return nil
	}

	var hunks []hunkRange
	curStart := changeIdxs[0] - contextSize
	if curStart < 0 {
		curStart = 0
	}
	curEnd := changeIdxs[0] + contextSize + 1
	if curEnd > len(entries) {
		curEnd = len(entries)
	}

	for i := 1; i < len(changeIdxs); i++ {
		idx := changeIdxs[i]
		if idx-contextSize <= curEnd {
			// Merge into current hunk
			newEnd := idx + contextSize + 1
			if newEnd > len(entries) {
				newEnd = len(entries)
			}
			curEnd = newEnd
		} else {
			hunks = append(hunks, hunkRange{curStart, curEnd})
			curStart = idx - contextSize
			if curStart < 0 {
				curStart = 0
			}
			curEnd = idx + contextSize + 1
			if curEnd > len(entries) {
				curEnd = len(entries)
			}
		}
	}
	hunks = append(hunks, hunkRange{curStart, curEnd})
	return hunks
}

// generateDiff produces a colored unified diff string.
// Returns empty string if there are no differences.
func generateDiff(filename, oldText, newText string) string {
	entries := computeDiff(oldText, newText)

	hasChanges := false
	for _, e := range entries {
		if e.typ != "equal" {
			hasChanges = true
			break
		}
	}
	if !hasChanges {
		return ""
	}

	const contextSize = 3
	hunks := buildHunks(entries, contextSize)
	if len(hunks) == 0 {
		return ""
	}

	var buf strings.Builder

	// Header
	buf.WriteString(fmt.Sprintf("%sdiff --git a/%s b/%s%s\n", colorFaint, filename, filename, colorReset))
	buf.WriteString(fmt.Sprintf("%s--- %s%s\n", colorBold, filename, colorReset))
	buf.WriteString(fmt.Sprintf("%s+++ %s%s\n", colorBold, filename, colorReset))

	for _, h := range hunks {
		// Compute old/new line ranges for the hunk header
		oldStart, oldCount, newStart, newCount := 0, 0, 0, 0
		for k := h.start; k < h.end; k++ {
			e := entries[k]
			if e.typ == "equal" || e.typ == "del" {
				if oldStart == 0 {
					oldStart = e.oldNo
				}
				oldCount++
			}
			if e.typ == "equal" || e.typ == "add" {
				if newStart == 0 {
					newStart = e.newNo
				}
				newCount++
			}
		}
		// Handle edge case: hunk has only additions (no old lines in range)
		if oldStart == 0 {
			if h.start > 0 {
				oldStart = entries[h.start-1].oldNo + 1
			} else {
				oldStart = 0
			}
		}
		// Handle edge case: hunk has only deletions (no new lines in range)
		if newStart == 0 {
			if h.start > 0 {
				newStart = entries[h.start-1].newNo + 1
			} else {
				newStart = 0
			}
		}

		buf.WriteString(fmt.Sprintf("%s@@ -%d,%d +%d,%d @@%s\n",
			colorCyan, oldStart, oldCount, newStart, newCount, colorReset))

		for k := h.start; k < h.end; k++ {
			e := entries[k]
			switch e.typ {
			case "equal":
				buf.WriteString(" " + e.line + "\n")
			case "del":
				buf.WriteString(fmt.Sprintf("%s-%s%s\n", colorRed, e.line, colorReset))
			case "add":
				buf.WriteString(fmt.Sprintf("%s+%s%s\n", colorGreen, e.line, colorReset))
			}
		}
	}

	return buf.String()
}
