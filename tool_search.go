package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func registerSearchTools(r *ToolRegistry) {
	r.Register(ToolDef{
		Name:        "grep",
		Description: "Search file contents by regex pattern. Returns matching lines with file paths and line numbers.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string", "description": "Regex pattern to search for"},
				"path":    map[string]any{"type": "string", "description": "File or directory to search in (default: current dir)"},
				"include": map[string]any{"type": "string", "description": "Glob pattern to filter files (e.g. *.go)"},
			},
			"required": []string{"pattern"},
		},
	}, toolGrep, false)

	r.Register(ToolDef{
		Name:        "find_files",
		Description: "Find files by glob/name pattern. Returns matching paths with type and size.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":  map[string]any{"type": "string", "description": "Glob pattern to match (e.g. **/*.go, *.txt)"},
				"path":     map[string]any{"type": "string", "description": "Directory to search in (default: current dir)"},
				"type":     map[string]any{"type": "string", "description": "Filter by type: file, dir, or symlink"},
				"max_size": map[string]any{"type": "integer", "description": "Maximum file size in bytes"},
				"min_size": map[string]any{"type": "integer", "description": "Minimum file size in bytes"},
			},
			"required": []string{"pattern"},
		},
	}, toolFindFiles, false)
}

func toolGrep(args json.RawMessage) (string, error) {
	var params struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Include string `json:"include"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	re, err := regexp.Compile(params.Pattern)
	if err != nil {
		return fmt.Sprintf("error: invalid regex: %v", err), nil
	}

	searchPath := params.Path
	if searchPath == "" {
		searchPath = "."
	}

	var results strings.Builder
	matchCount := 0
	const maxMatches = 200

	filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if matchCount >= maxMatches {
			return filepath.SkipAll
		}

		// Skip binary-looking files and hidden dirs
		if strings.Contains(path, "/.") || strings.Contains(path, "/node_modules/") ||
			strings.Contains(path, "/.git/") || strings.Contains(path, "/vendor/") {
			return nil
		}

		// Apply include filter
		if params.Include != "" {
			matched, _ := filepath.Match(params.Include, filepath.Base(path))
			if !matched {
				return nil
			}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		// Skip likely binary files
		if len(data) > 0 {
			sample := data
			if len(sample) > 512 {
				sample = sample[:512]
			}
			nullCount := 0
			for _, b := range sample {
				if b == 0 {
					nullCount++
				}
			}
			if nullCount > 0 {
				return nil
			}
		}

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if matchCount >= maxMatches {
				break
			}
			if re.MatchString(line) {
				fmt.Fprintf(&results, "%s:%d: %s\n", path, i+1, line)
				matchCount++
			}
		}
		return nil
	})

	if matchCount == 0 {
		return "no matches found", nil
	}
	if matchCount >= maxMatches {
		fmt.Fprintf(&results, "\n... [truncated at %d matches]", maxMatches)
	}
	return results.String(), nil
}

func toolFindFiles(args json.RawMessage) (string, error) {
	var params struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Type    string `json:"type"`
		MaxSize int64  `json:"max_size"`
		MinSize int64  `json:"min_size"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	searchPath := params.Path
	if searchPath == "" {
		searchPath = "."
	}

	var results strings.Builder
	matchCount := 0
	const maxMatches = 500

	filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if matchCount >= maxMatches {
			return filepath.SkipAll
		}

		// Skip hidden dirs
		if info.IsDir() && strings.HasPrefix(info.Name(), ".") && path != searchPath {
			return filepath.SkipDir
		}

		// Match pattern against base name and relative path
		baseName := info.Name()
		matched, _ := filepath.Match(params.Pattern, baseName)
		if !matched {
			rel, _ := filepath.Rel(searchPath, path)
			matched, _ = filepath.Match(params.Pattern, rel)
		}
		if !matched {
			return nil
		}

		// Type filter
		if params.Type != "" {
			switch params.Type {
			case "file":
				if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					return nil
				}
			case "dir":
				if !info.IsDir() {
					return nil
				}
			case "symlink":
				if info.Mode()&os.ModeSymlink == 0 {
					return nil
				}
			}
		}

		// Size filter (only for files)
		if !info.IsDir() {
			if params.MaxSize > 0 && info.Size() > params.MaxSize {
				return nil
			}
			if params.MinSize > 0 && info.Size() < params.MinSize {
				return nil
			}
		}

		prefix := "f"
		if info.IsDir() {
			prefix = "d"
		} else if info.Mode()&os.ModeSymlink != 0 {
			prefix = "l"
		}

		fmt.Fprintf(&results, "%s %8d %s\n", prefix, info.Size(), path)
		matchCount++
		return nil
	})

	if matchCount == 0 {
		return "no matching files found", nil
	}
	if matchCount >= maxMatches {
		fmt.Fprintf(&results, "\n... [truncated at %d matches]", maxMatches)
	}
	return results.String(), nil
}
