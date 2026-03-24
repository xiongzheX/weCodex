package codexacp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvaluateAllowsReadFileInsideWorkingDirectory(t *testing.T) {
	wd := t.TempDir()
	file := filepath.Join(wd, "notes.txt")
	if err := os.WriteFile(file, []byte("ok"), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	decision := EvaluatePermission(wd, PermissionRequest{ToolName: "read_file", TargetPath: file})
	if !decision.Allowed {
		t.Fatalf("expected allow, got deny: %s", decision.Reason)
	}
}

func TestEvaluateDeniesPathThatEscapesWorkingDirectory(t *testing.T) {
	wd := t.TempDir()
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	req := PermissionRequest{ToolName: "read_file", TargetPath: filepath.Join(wd, "..", filepath.Base(outsideDir), "secret.txt")}
	decision := EvaluatePermission(wd, req)
	if decision.Allowed {
		t.Fatalf("expected deny for path escape")
	}
	if !strings.Contains(strings.ToLower(decision.Reason), "working directory") {
		t.Fatalf("expected working directory reason, got %q", decision.Reason)
	}
}

func TestEvaluateDeniesSymlinkEscape(t *testing.T) {
	wd := t.TempDir()
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	linkPath := filepath.Join(wd, "linked")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	decision := EvaluatePermission(wd, PermissionRequest{ToolName: "read_file", TargetPath: filepath.Join(linkPath, "secret.txt")})
	if decision.Allowed {
		t.Fatalf("expected deny for symlink escape")
	}
}

func TestEvaluateDeniesWriteFile(t *testing.T) {
	wd := t.TempDir()
	decision := EvaluatePermission(wd, PermissionRequest{ToolName: "write_file", TargetPath: filepath.Join(wd, "out.txt")})
	if decision.Allowed {
		t.Fatalf("expected write tool to be denied")
	}
}

func TestEvaluateAllowsSearchToolInsideWorkingDirectory(t *testing.T) {
	wd := t.TempDir()
	decision := EvaluatePermission(wd, PermissionRequest{ToolName: "grep", TargetPath: wd})
	if !decision.Allowed {
		t.Fatalf("expected search tool allow, got deny: %s", decision.Reason)
	}
}

func TestEvaluateAllowsReadOnlyToolWithEmptyTargetPathUsingWorkingDirectoryScope(t *testing.T) {
	wd := t.TempDir()
	decision := EvaluatePermission(wd, PermissionRequest{ToolName: "grep", TargetPath: ""})
	if !decision.Allowed {
		t.Fatalf("expected read-only tool with empty target path to be scoped to working directory, got deny: %s", decision.Reason)
	}
}

func TestEvaluateAllowsSpacedReadAndListAliasesInsideWorkingDirectory(t *testing.T) {
	wd := t.TempDir()
	file := filepath.Join(wd, "notes.txt")
	if err := os.WriteFile(file, []byte("ok"), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cases := []struct {
		name string
		tool string
		path string
	}{
		{name: "read file", tool: "read file", path: file},
		{name: "read text file", tool: "read text file", path: file},
		{name: "list directory", tool: "list directory", path: wd},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := EvaluatePermission(wd, PermissionRequest{ToolName: tc.tool, TargetPath: tc.path})
			if !decision.Allowed {
				t.Fatalf("expected allow for %q, got deny: %s", tc.tool, decision.Reason)
			}
		})
	}
}

func TestEvaluateAllowsLiteralGlobMetacharactersInTargetPath(t *testing.T) {
	wd := t.TempDir()
	literalDir := filepath.Join(wd, "folder[abc")
	if err := os.MkdirAll(literalDir, 0o700); err != nil {
		t.Fatalf("mkdir literal dir: %v", err)
	}

	decision := EvaluatePermission(wd, PermissionRequest{ToolName: "read_file", TargetPath: filepath.Join("folder[abc", "missing.txt")})
	if !decision.Allowed {
		t.Fatalf("expected allow for literal metacharacter path, got deny: %s", decision.Reason)
	}
}

func TestNormalizePermissionRequestExtractsReadFileToolCall(t *testing.T) {
	raw := json.RawMessage(`{"name":"read_file","arguments":{"path":"/tmp/file.txt"}}`)
	req, err := NormalizePermissionRequest(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if req.ToolName != "read_file" {
		t.Fatalf("expected tool read_file, got %q", req.ToolName)
	}
	if req.TargetPath != "/tmp/file.txt" {
		t.Fatalf("expected target path, got %q", req.TargetPath)
	}
}

func TestNormalizePermissionRequestExtractsShellCommand(t *testing.T) {
	raw := json.RawMessage(`{"name":"bash","arguments":{"command":"git status"}}`)
	req, err := NormalizePermissionRequest(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if req.ToolName != "bash" {
		t.Fatalf("expected tool bash, got %q", req.ToolName)
	}
	if req.Command != "git status" {
		t.Fatalf("expected command git status, got %q", req.Command)
	}
}

func TestNormalizePermissionRequestLeavesUnknownToolForFailClosedEvaluation(t *testing.T) {
	raw := json.RawMessage(`{"name":"future_tool","arguments":{"path":"/tmp/something"}}`)
	req, err := NormalizePermissionRequest(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if req.ToolName != "future_tool" {
		t.Fatalf("expected unknown tool name preserved, got %q", req.ToolName)
	}
}

func TestNormalizePermissionRequestRejectsAmbiguousToolCall(t *testing.T) {
	raw := json.RawMessage(`{"name":"read_file","tool_name":"grep","arguments":{"path":"/tmp/x"}}`)
	_, err := NormalizePermissionRequest(raw)
	if err == nil {
		t.Fatal("expected ambiguous tool call error")
	}
}

func TestNormalizePermissionRequestAcceptsCamelCaseToolName(t *testing.T) {
	raw := json.RawMessage(`{"toolName":"read_file","arguments":{"path":"/tmp/file.txt"}}`)
	req, err := NormalizePermissionRequest(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if req.ToolName != "read_file" {
		t.Fatalf("expected tool read_file, got %q", req.ToolName)
	}
	if req.TargetPath != "/tmp/file.txt" {
		t.Fatalf("expected target path, got %q", req.TargetPath)
	}
}

func TestEvaluateAllowsPwdLsGitStatusAndGitDiff(t *testing.T) {
	wd := t.TempDir()
	commands := []string{"pwd", "ls", "git status", "git diff"}
	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			decision := EvaluatePermission(wd, PermissionRequest{ToolName: "bash", Command: cmd})
			if !decision.Allowed {
				t.Fatalf("expected allow for %q, got deny: %s", cmd, decision.Reason)
			}
		})
	}
}

func TestEvaluateAllowsGitLogWithBoundedCount(t *testing.T) {
	wd := t.TempDir()
	decision := EvaluatePermission(wd, PermissionRequest{ToolName: "bash", Command: "git log --oneline -n 20"})
	if !decision.Allowed {
		t.Fatalf("expected allow for bounded git log, got deny: %s", decision.Reason)
	}
}

func TestEvaluateDeniesGitLogBeyondBoundedCount(t *testing.T) {
	wd := t.TempDir()
	decision := EvaluatePermission(wd, PermissionRequest{ToolName: "bash", Command: "git log --oneline -n 21"})
	if decision.Allowed {
		t.Fatalf("expected deny for unbounded git log")
	}
}

func TestEvaluateDeniesShellWithPipe(t *testing.T) {
	wd := t.TempDir()
	decision := EvaluatePermission(wd, PermissionRequest{ToolName: "bash", Command: "git status | cat"})
	if decision.Allowed {
		t.Fatalf("expected deny for pipe")
	}
}

func TestEvaluateDeniesShellWithRedirect(t *testing.T) {
	wd := t.TempDir()
	decision := EvaluatePermission(wd, PermissionRequest{ToolName: "bash", Command: "git status > out.txt"})
	if decision.Allowed {
		t.Fatalf("expected deny for redirect")
	}
}

func TestEvaluateDeniesShellWithEnvMutation(t *testing.T) {
	wd := t.TempDir()
	decision := EvaluatePermission(wd, PermissionRequest{ToolName: "bash", Command: "FOO=1 git status"})
	if decision.Allowed {
		t.Fatalf("expected deny for env mutation")
	}
}

func TestEvaluateDeniesShellWithSeparator(t *testing.T) {
	wd := t.TempDir()
	decision := EvaluatePermission(wd, PermissionRequest{ToolName: "bash", Command: "git status; ls"})
	if decision.Allowed {
		t.Fatalf("expected deny for separator")
	}
}

func TestEvaluateDeniesShellWithCommandSubstitution(t *testing.T) {
	wd := t.TempDir()
	decision := EvaluatePermission(wd, PermissionRequest{ToolName: "bash", Command: "git status $(pwd)"})
	if decision.Allowed {
		t.Fatalf("expected deny for command substitution")
	}
}

func TestEvaluateDeniesShellWithSubshell(t *testing.T) {
	wd := t.TempDir()
	decision := EvaluatePermission(wd, PermissionRequest{ToolName: "bash", Command: "(git status)"})
	if decision.Allowed {
		t.Fatalf("expected deny for subshell")
	}
}

func TestEvaluateDeniesShellWithFileMutationFlag(t *testing.T) {
	wd := t.TempDir()
	decision := EvaluatePermission(wd, PermissionRequest{ToolName: "bash", Command: "git diff --output=patch.txt"})
	if decision.Allowed {
		t.Fatalf("expected deny for file mutation flag")
	}
}

func TestEvaluateDeniesNonAllowlistedShellCommand(t *testing.T) {
	wd := t.TempDir()
	decision := EvaluatePermission(wd, PermissionRequest{ToolName: "bash", Command: "git branch"})
	if decision.Allowed {
		t.Fatalf("expected non-allowlisted command to be denied")
	}
}

func TestEvaluateDeniesUnknownToolByDefault(t *testing.T) {
	wd := t.TempDir()
	decision := EvaluatePermission(wd, PermissionRequest{ToolName: "unknown_tool", TargetPath: wd})
	if decision.Allowed {
		t.Fatalf("expected unknown tool deny")
	}
}
