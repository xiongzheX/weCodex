package bridge

import (
	"strings"
	"testing"
)

func TestParseInputRecognizesHelpCommand(t *testing.T) {
	got := ParseInput("/help")

	if got.Kind != InputHelp {
		t.Fatalf("expected kind %q, got %q", InputHelp, got.Kind)
	}
	if got.Text != "" {
		t.Fatalf("expected empty text for local command, got %q", got.Text)
	}
}

func TestParseInputRecognizesStatusAndNewCommands(t *testing.T) {
	statusInput := ParseInput("/status")
	if statusInput.Kind != InputStatus {
		t.Fatalf("expected kind %q, got %q", InputStatus, statusInput.Kind)
	}
	if statusInput.Text != "" {
		t.Fatalf("expected empty text for local command, got %q", statusInput.Text)
	}

	newInput := ParseInput("/new")
	if newInput.Kind != InputNew {
		t.Fatalf("expected kind %q, got %q", InputNew, newInput.Kind)
	}
	if newInput.Text != "" {
		t.Fatalf("expected empty text for local command, got %q", newInput.Text)
	}
}

func TestParseInputTreatsUnknownSlashCommandAsPromptText(t *testing.T) {
	got := ParseInput("/unknown keep as-is")

	if got.Kind != InputPrompt {
		t.Fatalf("expected kind %q, got %q", InputPrompt, got.Kind)
	}
	if got.Text != "/unknown keep as-is" {
		t.Fatalf("expected unknown slash command forwarded unchanged, got %q", got.Text)
	}
}

func TestBuildHelpTextIncludesLocalCommands(t *testing.T) {
	help := BuildHelpText()

	for _, cmd := range []string{"/help", "/status", "/new"} {
		if !strings.Contains(help, cmd) {
			t.Fatalf("expected help text to include %q, got %q", cmd, help)
		}
	}
}

func TestBuildRuntimeStatusWithoutLastErrorSummary(t *testing.T) {
	status := RuntimeStatus{
		BridgeMode:       "running",
		ACPState:         "ready",
		HasActiveSession: true,
		PermissionMode:   "read-only",
	}

	got := BuildRuntimeStatus(status)

	assertContains(t, got, "bridge mode: running")
	assertContains(t, got, "acp state: ready")
	assertContains(t, got, "active session: yes")
	assertContains(t, got, "permission mode: read-only")
	assertNotContains(t, got, "last error summary:")
}

func TestBuildRuntimeStatusWithLastErrorSummary(t *testing.T) {
	status := RuntimeStatus{
		BridgeMode:       "running",
		ACPState:         "degraded",
		HasActiveSession: false,
		PermissionMode:   "read-only",
		LastErrorSummary: "rpc timeout",
	}

	got := BuildRuntimeStatus(status)

	assertContains(t, got, "bridge mode: running")
	assertContains(t, got, "acp state: degraded")
	assertContains(t, got, "active session: no")
	assertContains(t, got, "permission mode: read-only")
	assertContains(t, got, "last error summary: rpc timeout")
}

func assertContains(t *testing.T, got string, wantSubstring string) {
	t.Helper()
	if !strings.Contains(got, wantSubstring) {
		t.Fatalf("expected %q to contain %q", got, wantSubstring)
	}
}

func assertNotContains(t *testing.T, got string, unexpectedSubstring string) {
	t.Helper()
	if strings.Contains(got, unexpectedSubstring) {
		t.Fatalf("expected %q not to contain %q", got, unexpectedSubstring)
	}
}
