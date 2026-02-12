package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func registerFSTools(r *ToolRegistry) {
	r.Register(ToolDef{
		Name:        "read_file",
		Description: "Read file contents. Returns content with line numbers.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":   map[string]any{"type": "string", "description": "File path to read"},
				"offset": map[string]any{"type": "integer", "description": "Starting line number (1-based, optional)"},
				"limit":  map[string]any{"type": "integer", "description": "Number of lines to read (optional)"},
			},
			"required": []string{"path"},
		},
	}, toolReadFile, false)

	r.Register(ToolDef{
		Name:        "write_file",
		Description: "Write content to a file. Creates parent directories if needed.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "File path to write"},
				"content": map[string]any{"type": "string", "description": "File content to write"},
			},
			"required": []string{"path", "content"},
		},
	}, toolWriteFile, true)

	r.Register(ToolDef{
		Name:        "edit_file",
		Description: "Edit a file by replacing exact text. old_text must match exactly.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":     map[string]any{"type": "string", "description": "File path to edit"},
				"old_text": map[string]any{"type": "string", "description": "Exact text to find and replace"},
				"new_text": map[string]any{"type": "string", "description": "Replacement text"},
			},
			"required": []string{"path", "old_text", "new_text"},
		},
	}, toolEditFile, true)

	r.Register(ToolDef{
		Name:        "list_dir",
		Description: "List directory contents.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":      map[string]any{"type": "string", "description": "Directory path"},
				"recursive": map[string]any{"type": "boolean", "description": "List recursively (default false)"},
			},
			"required": []string{"path"},
		},
	}, toolListDir, false)

	r.Register(ToolDef{
		Name:        "delete",
		Description: "Delete a file or directory.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":      map[string]any{"type": "string", "description": "Path to delete"},
				"recursive": map[string]any{"type": "boolean", "description": "Delete directories recursively (default false)"},
			},
			"required": []string{"path"},
		},
	}, toolDelete, true)

	r.Register(ToolDef{
		Name:        "move",
		Description: "Move or rename a file or directory.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source": map[string]any{"type": "string", "description": "Source path"},
				"dest":   map[string]any{"type": "string", "description": "Destination path"},
			},
			"required": []string{"source", "dest"},
		},
	}, toolMove, true)

	r.Register(ToolDef{
		Name:        "copy",
		Description: "Copy a file or directory.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source":    map[string]any{"type": "string", "description": "Source path"},
				"dest":      map[string]any{"type": "string", "description": "Destination path"},
				"recursive": map[string]any{"type": "boolean", "description": "Copy directories recursively (default false)"},
			},
			"required": []string{"source", "dest"},
		},
	}, toolCopy, true)

	r.Register(ToolDef{
		Name:        "file_info",
		Description: "Get file metadata: size, permissions, modification time, type, symlink target.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "File path"},
			},
			"required": []string{"path"},
		},
	}, toolFileInfo, false)

	r.Register(ToolDef{
		Name:        "make_dir",
		Description: "Create a directory, including parent directories as needed.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Directory path to create"},
				"mode": map[string]any{"type": "string", "description": "Permissions in octal (default 0755)"},
			},
			"required": []string{"path"},
		},
	}, toolMakeDir, true)

	r.Register(ToolDef{
		Name:        "chmod",
		Description: "Change file or directory permissions.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "File or directory path"},
				"mode": map[string]any{"type": "string", "description": "Permissions in octal (e.g. 0644, 0755)"},
			},
			"required": []string{"path", "mode"},
		},
	}, toolChmod, true)
}

func toolReadFile(args json.RawMessage) (string, error) {
	var params struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	data, err := os.ReadFile(params.Path)
	if err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}

	lines := strings.Split(string(data), "\n")

	start := 0
	if params.Offset > 0 {
		start = params.Offset - 1
	}
	if start > len(lines) {
		start = len(lines)
	}

	end := len(lines)
	if params.Limit > 0 {
		end = start + params.Limit
	}
	if end > len(lines) {
		end = len(lines)
	}

	var sb strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&sb, "%4d\t%s\n", i+1, lines[i])
	}
	return sb.String(), nil
}

func toolWriteFile(args json.RawMessage) (string, error) {
	var params struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	dir := filepath.Dir(params.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Sprintf("error creating directory: %v", err), nil
	}

	if err := os.WriteFile(params.Path, []byte(params.Content), 0644); err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(params.Content), params.Path), nil
}

func toolEditFile(args json.RawMessage) (string, error) {
	var params struct {
		Path    string `json:"path"`
		OldText string `json:"old_text"`
		NewText string `json:"new_text"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	data, err := os.ReadFile(params.Path)
	if err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}

	content := string(data)
	count := strings.Count(content, params.OldText)

	if count == 0 {
		return "error: old_text not found in file", nil
	}
	if count > 1 {
		return fmt.Sprintf("error: old_text found %d times, must be unique", count), nil
	}

	newContent := strings.Replace(content, params.OldText, params.NewText, 1)
	if err := os.WriteFile(params.Path, []byte(newContent), 0644); err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}
	return fmt.Sprintf("edited %s", params.Path), nil
}

func toolListDir(args json.RawMessage) (string, error) {
	var params struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if params.Path == "" {
		params.Path = "."
	}

	var sb strings.Builder

	if params.Recursive {
		filepath.Walk(params.Path, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			prefix := ""
			if info.IsDir() {
				prefix = "d "
			} else {
				prefix = "f "
			}
			fmt.Fprintf(&sb, "%s%s\n", prefix, path)
			return nil
		})
	} else {
		entries, err := os.ReadDir(params.Path)
		if err != nil {
			return fmt.Sprintf("error: %v", err), nil
		}
		for _, e := range entries {
			prefix := "f "
			if e.IsDir() {
				prefix = "d "
			}
			fmt.Fprintf(&sb, "%s%s\n", prefix, e.Name())
		}
	}

	return sb.String(), nil
}

func toolDelete(args json.RawMessage) (string, error) {
	var params struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	info, err := os.Lstat(params.Path)
	if err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}

	if info.IsDir() && !params.Recursive {
		return "error: path is a directory, set recursive=true to delete", nil
	}

	if params.Recursive {
		err = os.RemoveAll(params.Path)
	} else {
		err = os.Remove(params.Path)
	}
	if err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}
	return fmt.Sprintf("deleted %s", params.Path), nil
}

func toolMove(args json.RawMessage) (string, error) {
	var params struct {
		Source string `json:"source"`
		Dest   string `json:"dest"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if err := os.Rename(params.Source, params.Dest); err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}
	return fmt.Sprintf("moved %s -> %s", params.Source, params.Dest), nil
}

func toolCopy(args json.RawMessage) (string, error) {
	var params struct {
		Source    string `json:"source"`
		Dest     string `json:"dest"`
		Recursive bool   `json:"recursive"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	srcInfo, err := os.Lstat(params.Source)
	if err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}

	if srcInfo.IsDir() {
		if !params.Recursive {
			return "error: source is a directory, set recursive=true to copy", nil
		}
		if err := copyDir(params.Source, params.Dest); err != nil {
			return fmt.Sprintf("error: %v", err), nil
		}
		return fmt.Sprintf("copied directory %s -> %s", params.Source, params.Dest), nil
	}

	if err := copyFile(params.Source, params.Dest, srcInfo.Mode()); err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}
	return fmt.Sprintf("copied %s -> %s", params.Source, params.Dest), nil
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

func toolFileInfo(args json.RawMessage) (string, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	info, err := os.Lstat(params.Path)
	if err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "name: %s\n", info.Name())
	fmt.Fprintf(&sb, "size: %d\n", info.Size())
	fmt.Fprintf(&sb, "mode: %s\n", info.Mode())
	fmt.Fprintf(&sb, "modified: %s\n", info.ModTime().Format(time.RFC3339))

	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(params.Path)
		if err == nil {
			fmt.Fprintf(&sb, "symlink_target: %s\n", target)
		}
	}

	if info.IsDir() {
		fmt.Fprintf(&sb, "type: directory\n")
	} else if info.Mode()&os.ModeSymlink != 0 {
		fmt.Fprintf(&sb, "type: symlink\n")
	} else {
		fmt.Fprintf(&sb, "type: file\n")
	}

	return sb.String(), nil
}

func toolMakeDir(args json.RawMessage) (string, error) {
	var params struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	mode := fs.FileMode(0755)
	if params.Mode != "" {
		parsed, err := strconv.ParseUint(params.Mode, 8, 32)
		if err != nil {
			return fmt.Sprintf("error: invalid mode %q: %v", params.Mode, err), nil
		}
		mode = fs.FileMode(parsed)
	}

	if err := os.MkdirAll(params.Path, mode); err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}
	return fmt.Sprintf("created directory %s", params.Path), nil
}

func toolChmod(args json.RawMessage) (string, error) {
	var params struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	parsed, err := strconv.ParseUint(params.Mode, 8, 32)
	if err != nil {
		return fmt.Sprintf("error: invalid mode %q: %v", params.Mode, err), nil
	}

	if err := os.Chmod(params.Path, fs.FileMode(parsed)); err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}
	return fmt.Sprintf("chmod %s %s", params.Mode, params.Path), nil
}
