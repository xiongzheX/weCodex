package bridge

import (
	"strings"
)

func ParseInput(text string) ParsedInput {
	trimmed := strings.TrimSpace(text)

	switch trimmed {
	case "/help":
		return ParsedInput{Kind: InputHelp}
	case "/status":
		return ParsedInput{Kind: InputStatus}
	case "/new":
		return ParsedInput{Kind: InputNew}
	default:
		return ParsedInput{Kind: InputPrompt, Text: text}
	}
}

func BuildHelpText() string {
	lines := []string{
		"Local commands:",
		"/help - show local command help",
		"/status - show bridge runtime status",
		"/new - start a new local session",
	}
	return strings.Join(lines, "\n")
}

func BuildRuntimeStatus(status RuntimeStatus) string {
	activeSession := "no"
	if status.HasActiveSession {
		activeSession = "yes"
	}

	lines := []string{
		"bridge mode: " + status.BridgeMode,
		"acp state: " + status.ACPState,
		"active session: " + activeSession,
		"permission mode: " + status.PermissionMode,
	}

	if strings.TrimSpace(status.LastErrorSummary) != "" {
		lines = append(lines, "last error summary: "+status.LastErrorSummary)
	}

	return strings.Join(lines, "\n")
}
