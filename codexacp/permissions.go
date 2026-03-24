package codexacp

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type PermissionRequest struct {
	ToolName   string
	Command    string
	TargetPath string
}

type PermissionDecision struct {
	Allowed bool
	Reason  string
}

var envMutationPrefix = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

func NormalizePermissionRequest(toolCall json.RawMessage) (PermissionRequest, error) {
	if len(strings.TrimSpace(string(toolCall))) == 0 {
		return PermissionRequest{}, errors.New("malformed tool call")
	}

	var payload map[string]any
	if err := json.Unmarshal(toolCall, &payload); err != nil {
		return PermissionRequest{}, fmt.Errorf("malformed tool call: %w", err)
	}

	var toolName string
	foundToolName := false
	for _, key := range []string{"name", "tool_name", "toolName"} {
		name, has, err := getString(payload, key)
		if err != nil {
			return PermissionRequest{}, errors.New("ambiguous tool call")
		}
		if !has {
			continue
		}
		if !foundToolName {
			toolName = name
			foundToolName = true
			continue
		}
		if name != toolName {
			return PermissionRequest{}, errors.New("ambiguous tool call")
		}
	}
	if !foundToolName {
		return PermissionRequest{}, errors.New("malformed tool call")
	}

	arguments, err := parseArguments(payload)
	if err != nil {
		return PermissionRequest{}, err
	}

	cmd := firstNonEmpty(arguments, "command", "cmd")
	path := firstNonEmpty(arguments, "path", "target_path", "targetPath", "file_path", "filePath", "directory", "dir")

	return PermissionRequest{
		ToolName:   toolName,
		Command:    cmd,
		TargetPath: path,
	}, nil
}

func EvaluatePermission(workingDirectory string, req PermissionRequest) PermissionDecision {
	tool := strings.ToLower(strings.TrimSpace(req.ToolName))
	if tool == "" {
		return deny("missing tool name")
	}

	if isReadOnlyTool(tool) {
		targetPath := strings.TrimSpace(req.TargetPath)
		if targetPath == "" {
			targetPath = workingDirectory
		}
		inside, err := pathInsideWorkingDirectory(workingDirectory, targetPath)
		if err != nil {
			return deny("invalid target path")
		}
		if !inside {
			return deny("target outside working directory")
		}
		return allow()
	}

	if isShellTool(tool) {
		return evaluateShell(req.Command)
	}

	return deny("tool not allowed")
}

func evaluateShell(command string) PermissionDecision {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return deny("missing shell command")
	}

	if containsShellMeta(cmd) {
		return deny("shell syntax not allowed")
	}

	if looksLikeEnvMutation(cmd) {
		return deny("environment mutation not allowed")
	}

	if cmd == "pwd" || cmd == "ls" || cmd == "git status" || cmd == "git diff" {
		return allow()
	}

	if n, ok := parseBoundedGitLog(cmd); ok {
		if n <= 20 {
			return allow()
		}
		return deny("git log count exceeds limit")
	}

	return deny("shell command not allowlisted")
}

func parseBoundedGitLog(cmd string) (int, bool) {
	parts := strings.Fields(cmd)
	if len(parts) != 5 {
		return 0, false
	}
	if parts[0] != "git" || parts[1] != "log" || parts[2] != "--oneline" || parts[3] != "-n" {
		return 0, false
	}
	n, err := strconv.Atoi(parts[4])
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func containsShellMeta(cmd string) bool {
	bad := []string{"|", ">", "<", ";", "&&", "||", "`", "$(", "("}
	for _, marker := range bad {
		if strings.Contains(cmd, marker) {
			return true
		}
	}
	return false
}

func looksLikeEnvMutation(cmd string) bool {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return false
	}
	return envMutationPrefix.MatchString(parts[0])
}

func isShellTool(tool string) bool {
	switch tool {
	case "bash", "shell", "sh", "zsh", "command", "run_shell_command":
		return true
	default:
		return false
	}
}

func isReadOnlyTool(tool string) bool {
	switch tool {
	case "read_file", "read_text_file", "read", "search", "grep", "glob", "list_directory", "list_dir", "stat", "read file", "read text file", "list directory":
		return true
	default:
		return false
	}
}

func pathInsideWorkingDirectory(workingDirectory, target string) (bool, error) {
	resolvedWD, err := canonicalPath(workingDirectory, workingDirectory)
	if err != nil {
		return false, err
	}
	resolvedTarget, err := canonicalPath(target, workingDirectory)
	if err != nil {
		return false, err
	}
	return hasPathPrefix(resolvedTarget, resolvedWD), nil
}

func canonicalPath(pathValue, base string) (string, error) {
	if strings.TrimSpace(pathValue) == "" {
		return "", errors.New("empty path")
	}

	path := pathValue
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)

	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return filepath.Clean(resolved), nil
	}

	current := abs
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		if evalParent, evalErr := filepath.EvalSymlinks(parent); evalErr == nil {
			rel, relErr := filepath.Rel(parent, abs)
			if relErr != nil {
				return "", relErr
			}
			return filepath.Clean(filepath.Join(evalParent, rel)), nil
		}
		current = parent
	}
}

func hasPathPrefix(target, base string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func parseArguments(payload map[string]any) (map[string]any, error) {
	argsRaw, ok := payload["arguments"]
	if !ok || argsRaw == nil {
		return map[string]any{}, nil
	}

	switch v := argsRaw.(type) {
	case map[string]any:
		return v, nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return map[string]any{}, nil
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return nil, errors.New("malformed tool call")
		}
		return parsed, nil
	default:
		return nil, errors.New("malformed tool call")
	}
}

func firstNonEmpty(m map[string]any, keys ...string) string {
	for _, key := range keys {
		v, ok, _ := getString(m, key)
		if ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func getString(m map[string]any, key string) (string, bool, error) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", false, nil
	}
	s, ok := v.(string)
	if !ok {
		return "", false, errors.New("not a string")
	}
	return s, true, nil
}

func allow() PermissionDecision {
	return PermissionDecision{Allowed: true}
}

func deny(reason string) PermissionDecision {
	return PermissionDecision{Allowed: false, Reason: reason}
}
