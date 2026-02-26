package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type AccessMode int

const (
	AccessRead AccessMode = iota
	AccessWrite
)

type allowedRoot struct {
	abs      string
	real     string
	readOnly bool
}

// validatePath ensures the given path is within the workspace if restrict is true.
func validatePath(path, workspace string, restrict bool) (string, error) {
	return validatePathWithPolicy(path, workspace, restrict, AccessRead, "", false)
}

func validatePathWithPolicy(path, workspace string, restrict bool, mode AccessMode, sharedWorkspace string, sharedWorkspaceReadOnly bool) (string, error) {
	if workspace == "" {
		return path, nil
	}

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace path: %w", err)
	}

	var absPath string
	if filepath.IsAbs(path) {
		absPath = filepath.Clean(path)
	} else {
		absPath, err = filepath.Abs(filepath.Join(absWorkspace, path))
		if err != nil {
			return "", fmt.Errorf("failed to resolve file path: %w", err)
		}
	}

	if !restrict {
		return absPath, nil
	}

	roots := []allowedRoot{makeAllowedRoot(absWorkspace, false)}
	if strings.TrimSpace(sharedWorkspace) != "" {
		if absShared, err := filepath.Abs(strings.TrimSpace(sharedWorkspace)); err == nil {
			sharedRoot := makeAllowedRoot(absShared, sharedWorkspaceReadOnly)
			if !samePath(roots[0].abs, sharedRoot.abs) {
				roots = append(roots, sharedRoot)
			}
		}
	}

	root := rootForPath(absPath, roots, false)
	if root == nil {
		return "", outsideAllowedRootsError(absPath, roots)
	}
	if root.readOnly && mode == AccessWrite {
		return "", fmt.Errorf("access denied: shared workspace is read-only")
	}

	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		resolvedRoot := rootForPath(resolved, roots, true)
		if resolvedRoot == nil {
			return "", outsideAllowedRootsError(resolved, roots)
		}
		if resolvedRoot.readOnly && mode == AccessWrite {
			return "", fmt.Errorf("access denied: shared workspace is read-only")
		}
	} else if os.IsNotExist(err) {
		if parentResolved, err := resolveExistingAncestor(filepath.Dir(absPath)); err == nil {
			resolvedRoot := rootForPath(parentResolved, roots, true)
			if resolvedRoot == nil {
				return "", outsideAllowedRootsError(parentResolved, roots)
			}
			if resolvedRoot.readOnly && mode == AccessWrite {
				return "", fmt.Errorf("access denied: shared workspace is read-only")
			}
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to resolve path: %w", err)
		}
	} else {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}

	return absPath, nil
}

func outsideAllowedRootsError(resolvedPath string, roots []allowedRoot) error {
	labels := make([]string, 0, len(roots))
	for _, root := range roots {
		label := root.abs
		if root.readOnly {
			label += " (read-only)"
		}
		labels = append(labels, label)
	}
	return fmt.Errorf(
		"access denied: path is outside allowed roots (path=%s; allowed=%s). Use a routed workspace path, copy/symlink files into that workspace, or ask an admin to add this path to allowed roots",
		resolvedPath,
		strings.Join(labels, ", "),
	)
}

func makeAllowedRoot(path string, readOnly bool) allowedRoot {
	abs := filepath.Clean(path)
	real := abs
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		real = filepath.Clean(resolved)
	}
	return allowedRoot{
		abs:      abs,
		real:     real,
		readOnly: readOnly,
	}
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func rootForPath(path string, roots []allowedRoot, useReal bool) *allowedRoot {
	candidate := filepath.Clean(path)
	for i := range roots {
		root := roots[i]
		base := root.abs
		if useReal {
			base = root.real
		}
		if isWithinWorkspace(candidate, base) {
			return &roots[i]
		}
	}
	return nil
}

func resolveExistingAncestor(path string) (string, error) {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			return resolved, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		if filepath.Dir(current) == current {
			return "", os.ErrNotExist
		}
	}
}

func isWithinWorkspace(candidate, workspace string) bool {
	rel, err := filepath.Rel(filepath.Clean(workspace), filepath.Clean(candidate))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

type ReadFileTool struct {
	workspace               string
	restrict                bool
	sharedWorkspace         string
	sharedWorkspaceReadOnly bool
}

func NewReadFileTool(workspace string, restrict bool) *ReadFileTool {
	return &ReadFileTool{workspace: workspace, restrict: restrict}
}

func (t *ReadFileTool) SetSharedWorkspacePolicy(sharedWorkspace string, sharedWorkspaceReadOnly bool) {
	t.sharedWorkspace = strings.TrimSpace(sharedWorkspace)
	t.sharedWorkspaceReadOnly = sharedWorkspaceReadOnly
}

func (t *ReadFileTool) Name() string {
	return "read_file"
}

func (t *ReadFileTool) Description() string {
	return "Read the contents of a file"
}

func (t *ReadFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to read",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	path, ok := args["path"].(string)
	if !ok {
		return ErrorResult("path is required")
	}

	resolvedPath, err := validatePathWithPolicy(path, t.workspace, t.restrict, AccessRead, t.sharedWorkspace, t.sharedWorkspaceReadOnly)
	if err != nil {
		return UserErrorResult(err.Error())
	}

	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read file: %v", err))
	}

	return NewToolResult(string(content))
}

type WriteFileTool struct {
	workspace               string
	restrict                bool
	sharedWorkspace         string
	sharedWorkspaceReadOnly bool
}

func NewWriteFileTool(workspace string, restrict bool) *WriteFileTool {
	return &WriteFileTool{workspace: workspace, restrict: restrict}
}

func (t *WriteFileTool) SetSharedWorkspacePolicy(sharedWorkspace string, sharedWorkspaceReadOnly bool) {
	t.sharedWorkspace = strings.TrimSpace(sharedWorkspace)
	t.sharedWorkspaceReadOnly = sharedWorkspaceReadOnly
}

func (t *WriteFileTool) Name() string {
	return "write_file"
}

func (t *WriteFileTool) Description() string {
	return "Write content to a file"
}

func (t *WriteFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to write",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Content to write to the file",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	path, ok := args["path"].(string)
	if !ok {
		return ErrorResult("path is required")
	}

	content, ok := args["content"].(string)
	if !ok {
		return ErrorResult("content is required")
	}

	resolvedPath, err := validatePathWithPolicy(path, t.workspace, t.restrict, AccessWrite, t.sharedWorkspace, t.sharedWorkspaceReadOnly)
	if err != nil {
		return UserErrorResult(err.Error())
	}

	dir := filepath.Dir(resolvedPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create directory: %v", err))
	}

	if err := os.WriteFile(resolvedPath, []byte(content), 0644); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write file: %v", err))
	}

	return SilentResult(fmt.Sprintf("File written: %s", path))
}

type ListDirTool struct {
	workspace               string
	restrict                bool
	sharedWorkspace         string
	sharedWorkspaceReadOnly bool
}

func NewListDirTool(workspace string, restrict bool) *ListDirTool {
	return &ListDirTool{workspace: workspace, restrict: restrict}
}

func (t *ListDirTool) SetSharedWorkspacePolicy(sharedWorkspace string, sharedWorkspaceReadOnly bool) {
	t.sharedWorkspace = strings.TrimSpace(sharedWorkspace)
	t.sharedWorkspaceReadOnly = sharedWorkspaceReadOnly
}

func (t *ListDirTool) Name() string {
	return "list_dir"
}

func (t *ListDirTool) Description() string {
	return "List files and directories in a path"
}

func (t *ListDirTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to list",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ListDirTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	path, ok := args["path"].(string)
	if !ok {
		path = "."
	}

	resolvedPath, err := validatePathWithPolicy(path, t.workspace, t.restrict, AccessRead, t.sharedWorkspace, t.sharedWorkspaceReadOnly)
	if err != nil {
		return UserErrorResult(err.Error())
	}

	entries, err := os.ReadDir(resolvedPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read directory: %v", err))
	}

	result := ""
	for _, entry := range entries {
		if entry.IsDir() {
			result += "DIR:  " + entry.Name() + "\n"
		} else {
			result += "FILE: " + entry.Name() + "\n"
		}
	}

	return NewToolResult(result)
}
