package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func registerDiffTools(r *ToolRegistry) {
	r.Register(ToolDef{
		Name:        "diff",
		Description: "Compute a unified diff between two files, or between a file and provided content.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_a":  map[string]any{"type": "string", "description": "Path to first file (or the original file)"},
				"file_b":  map[string]any{"type": "string", "description": "Path to second file (optional if content_b is provided)"},
				"content_b": map[string]any{"type": "string", "description": "Content to compare against file_a (alternative to file_b)"},
				"context": map[string]any{"type": "integer", "description": "Number of context lines (default 3)"},
			},
			"required": []string{"file_a"},
		},
	}, toolDiff, false)

	r.Register(ToolDef{
		Name:        "patch",
		Description: "Apply a unified diff patch to a file.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":  map[string]any{"type": "string", "description": "File path to patch"},
				"patch": map[string]any{"type": "string", "description": "Unified diff patch content"},
			},
			"required": []string{"path", "patch"},
		},
	}, toolPatch, true)
}

func toolDiff(args json.RawMessage) (string, error) {
	var params struct {
		FileA    string `json:"file_a"`
		FileB    string `json:"file_b"`
		ContentB string `json:"content_b"`
		Context  int    `json:"context"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	dataA, err := os.ReadFile(params.FileA)
	if err != nil {
		return fmt.Sprintf("error reading file_a: %v", err), nil
	}
	linesA := splitLines(string(dataA))

	var linesB []string
	nameB := params.FileB
	if params.ContentB != "" {
		linesB = splitLines(params.ContentB)
		if nameB == "" {
			nameB = "(provided content)"
		}
	} else if params.FileB != "" {
		dataB, err := os.ReadFile(params.FileB)
		if err != nil {
			return fmt.Sprintf("error reading file_b: %v", err), nil
		}
		linesB = splitLines(string(dataB))
	} else {
		return "error: must provide either file_b or content_b", nil
	}

	ctx := 3
	if params.Context > 0 {
		ctx = params.Context
	}

	diff := unifiedDiff(params.FileA, nameB, linesA, linesB, ctx)
	if diff == "" {
		return "(files are identical)", nil
	}
	return diff, nil
}

func toolPatch(args json.RawMessage) (string, error) {
	var params struct {
		Path  string `json:"path"`
		Patch string `json:"patch"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	data, err := os.ReadFile(params.Path)
	if err != nil {
		return fmt.Sprintf("error reading file: %v", err), nil
	}

	lines := splitLines(string(data))
	hunks, err := parseUnifiedDiff(params.Patch)
	if err != nil {
		return fmt.Sprintf("error parsing patch: %v", err), nil
	}

	result, err := applyHunks(lines, hunks)
	if err != nil {
		return fmt.Sprintf("error applying patch: %v", err), nil
	}

	output := strings.Join(result, "\n")
	if err := os.WriteFile(params.Path, []byte(output), 0644); err != nil {
		return fmt.Sprintf("error writing file: %v", err), nil
	}
	return fmt.Sprintf("patched %s (%d hunks applied)", params.Path, len(hunks)), nil
}

// splitLines splits content into lines, preserving empty trailing line semantics.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// --- LCS-based unified diff ---

func unifiedDiff(nameA, nameB string, a, b []string, ctx int) string {
	edits := computeEdits(a, b)
	if len(edits) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n", nameA)
	fmt.Fprintf(&sb, "+++ %s\n", nameB)

	hunks := groupEdits(edits, len(a), len(b), ctx)
	for _, h := range hunks {
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", h.origStart+1, h.origCount, h.newStart+1, h.newCount)
		for _, line := range h.lines {
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

type editOp int

const (
	editKeep editOp = iota
	editDelete
	editInsert
)

type edit struct {
	op   editOp
	line string
	posA int // position in A
	posB int // position in B
}

func computeEdits(a, b []string) []edit {
	n, m := len(a), len(b)
	// Build LCS table
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrack to produce edits
	var edits []edit
	i, j := n, m
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			edits = append(edits, edit{op: editKeep, line: a[i-1], posA: i - 1, posB: j - 1})
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			edits = append(edits, edit{op: editInsert, line: b[j-1], posB: j - 1})
			j--
		} else {
			edits = append(edits, edit{op: editDelete, line: a[i-1], posA: i - 1})
			i--
		}
	}

	// Reverse
	for l, r := 0, len(edits)-1; l < r; l, r = l+1, r-1 {
		edits[l], edits[r] = edits[r], edits[l]
	}

	return edits
}

type hunk struct {
	origStart int
	origCount int
	newStart  int
	newCount  int
	lines     []string
}

func groupEdits(edits []edit, lenA, lenB, ctx int) []hunk {
	// Find ranges of changes with context
	type changeRange struct{ start, end int } // indices into edits
	var changes []changeRange
	for i, e := range edits {
		if e.op != editKeep {
			changes = append(changes, changeRange{i, i})
		}
	}
	if len(changes) == 0 {
		return nil
	}

	// Merge nearby changes
	var groups []changeRange
	cur := changes[0]
	for i := 1; i < len(changes); i++ {
		if changes[i].start-cur.end <= 2*ctx+1 {
			cur.end = changes[i].end
		} else {
			groups = append(groups, cur)
			cur = changes[i]
		}
	}
	groups = append(groups, cur)

	var hunks []hunk
	for _, g := range groups {
		// Expand with context
		start := g.start - ctx
		if start < 0 {
			start = 0
		}
		end := g.end + ctx
		if end >= len(edits) {
			end = len(edits) - 1
		}

		var h hunk
		h.origCount = 0
		h.newCount = 0
		firstOrig := true
		firstNew := true

		for i := start; i <= end; i++ {
			e := edits[i]
			switch e.op {
			case editKeep:
				h.lines = append(h.lines, " "+e.line)
				if firstOrig {
					h.origStart = e.posA
					firstOrig = false
				}
				if firstNew {
					h.newStart = e.posB
					firstNew = false
				}
				h.origCount++
				h.newCount++
			case editDelete:
				h.lines = append(h.lines, "-"+e.line)
				if firstOrig {
					h.origStart = e.posA
					firstOrig = false
				}
				h.origCount++
			case editInsert:
				h.lines = append(h.lines, "+"+e.line)
				if firstNew {
					h.newStart = e.posB
					firstNew = false
				}
				h.newCount++
			}
		}

		hunks = append(hunks, h)
	}
	return hunks
}

// --- Patch parser and applier ---

type patchHunk struct {
	origStart int // 0-based
	origCount int
	newStart  int // 0-based
	newCount  int
	lines     []string // each prefixed with ' ', '+', or '-'
}

func parseUnifiedDiff(patch string) ([]patchHunk, error) {
	lines := strings.Split(patch, "\n")
	var hunks []patchHunk

	for i := 0; i < len(lines); {
		line := lines[i]
		if !strings.HasPrefix(line, "@@") {
			i++
			continue
		}

		h, err := parseHunkHeader(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		i++

		for i < len(lines) && !strings.HasPrefix(lines[i], "@@") {
			l := lines[i]
			if len(l) == 0 {
				// Treat blank lines as context
				h.lines = append(h.lines, " ")
				i++
				continue
			}
			prefix := l[0]
			if prefix == ' ' || prefix == '+' || prefix == '-' {
				h.lines = append(h.lines, l)
			} else if strings.HasPrefix(l, "---") || strings.HasPrefix(l, "+++") {
				// skip file headers
			} else if strings.HasPrefix(l, "\\") {
				// "\ No newline at end of file" — skip
			} else {
				break
			}
			i++
		}
		hunks = append(hunks, h)
	}

	if len(hunks) == 0 {
		return nil, fmt.Errorf("no hunks found in patch")
	}
	return hunks, nil
}

func parseHunkHeader(line string) (patchHunk, error) {
	// @@ -A,B +C,D @@
	line = strings.TrimPrefix(line, "@@")
	idx := strings.Index(line, "@@")
	if idx >= 0 {
		line = line[:idx]
	}
	line = strings.TrimSpace(line)
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return patchHunk{}, fmt.Errorf("invalid hunk header")
	}

	origStart, origCount, err := parseRange(parts[0])
	if err != nil {
		return patchHunk{}, err
	}
	newStart, newCount, err := parseRange(parts[1])
	if err != nil {
		return patchHunk{}, err
	}

	return patchHunk{
		origStart: origStart - 1, // convert to 0-based
		origCount: origCount,
		newStart:  newStart - 1,
		newCount:  newCount,
	}, nil
}

func parseRange(s string) (int, int, error) {
	s = strings.TrimPrefix(s, "-")
	s = strings.TrimPrefix(s, "+")
	parts := strings.SplitN(s, ",", 2)
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("bad range %q: %w", s, err)
	}
	count := 1
	if len(parts) == 2 {
		count, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, fmt.Errorf("bad range %q: %w", s, err)
		}
	}
	return start, count, nil
}

func applyHunks(original []string, hunks []patchHunk) ([]string, error) {
	result := make([]string, 0, len(original))
	origIdx := 0

	for hi, h := range hunks {
		targetStart := h.origStart

		// Try fuzzy matching: search nearby if context doesn't match at exact position
		offset, err := findHunkOffset(original, h, targetStart)
		if err != nil {
			return nil, fmt.Errorf("hunk %d: %w", hi+1, err)
		}
		targetStart += offset

		// Copy lines before this hunk
		for origIdx < targetStart {
			if origIdx < len(original) {
				result = append(result, original[origIdx])
			}
			origIdx++
		}

		// Apply hunk
		for _, line := range h.lines {
			if len(line) == 0 {
				continue
			}
			switch line[0] {
			case ' ':
				if origIdx < len(original) {
					result = append(result, original[origIdx])
					origIdx++
				}
			case '-':
				origIdx++
			case '+':
				result = append(result, line[1:])
			}
		}
	}

	// Copy remaining lines
	for origIdx < len(original) {
		result = append(result, original[origIdx])
		origIdx++
	}

	return result, nil
}

func findHunkOffset(original []string, h patchHunk, start int) (int, error) {
	// Extract context/delete lines from hunk for matching
	var matchLines []string
	for _, line := range h.lines {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '-') {
			matchLines = append(matchLines, line[1:])
		}
	}
	if len(matchLines) == 0 {
		return 0, nil // pure insertion
	}

	// Try exact position first
	if matchAt(original, matchLines, start) {
		return 0, nil
	}

	// Search nearby (up to 50 lines in each direction)
	for delta := 1; delta <= 50; delta++ {
		if start+delta+len(matchLines) <= len(original) && matchAt(original, matchLines, start+delta) {
			return delta, nil
		}
		if start-delta >= 0 && matchAt(original, matchLines, start-delta) {
			return -delta, nil
		}
	}

	return 0, fmt.Errorf("could not find matching context at line %d", start+1)
}

func matchAt(original, matchLines []string, pos int) bool {
	if pos < 0 || pos+len(matchLines) > len(original) {
		return false
	}
	for i, ml := range matchLines {
		if original[pos+i] != ml {
			return false
		}
	}
	return true
}
