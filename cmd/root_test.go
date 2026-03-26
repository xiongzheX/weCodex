package cmd

import (
	"strings"
	"testing"
)

func TestNewRootCmdUsesLowercaseCommandName(t *testing.T) {
	root := newRootCmd()

	if root.Use != "wecodex" {
		t.Fatalf("expected root command Use to be %q, got %q", "wecodex", root.Use)
	}
}

func TestNewRootCmdUsesGenericCodexRuntimeDescription(t *testing.T) {
	root := newRootCmd()

	if root.Short != "WeChat bridge for Codex runtime" {
		t.Fatalf("expected root command Short to be %q, got %q", "WeChat bridge for Codex runtime", root.Short)
	}
}

func TestNewRootCmdMentionsThreadCommandsInLongHelp(t *testing.T) {
	root := newRootCmd()

	if root.Long == "" {
		t.Fatal("expected root command Long help to be set")
	}
	if want := "/list"; !strings.Contains(root.Long, want) {
		t.Fatalf("expected root command Long help to mention %q, got %q", want, root.Long)
	}
	if want := "/use N"; !strings.Contains(root.Long, want) {
		t.Fatalf("expected root command Long help to mention %q, got %q", want, root.Long)
	}
}
