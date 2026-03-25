package cmd

import "testing"

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
