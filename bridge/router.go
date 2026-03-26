package bridge

import (
	"strconv"
	"strings"
)

func ParseInput(text string) ParsedInput {
	trimmed := strings.TrimSpace(text)

	switch {
	case trimmed == "/help":
		return ParsedInput{Kind: InputHelp}
	case trimmed == "/status":
		return ParsedInput{Kind: InputStatus}
	case trimmed == "/new":
		return ParsedInput{Kind: InputNew}
	case trimmed == "/list":
		return ParsedInput{Kind: InputList}
	case strings.HasPrefix(trimmed, "/use"):
		fields := strings.Fields(trimmed)
		if len(fields) == 2 && fields[0] == "/use" {
			if useIndex, err := strconv.Atoi(fields[1]); err == nil {
				return ParsedInput{Kind: InputUse, UseIndex: &useIndex}
			}
		}
	}

	return ParsedInput{Kind: InputPrompt, Text: text}
}

func BuildHelpText() string {
	lines := []string{
		"Local commands:",
		"/help - show local command help",
		"/status - show bridge runtime status",
		"/new - start a new Codex CLI thread",
		"/list - list Codex CLI threads for the current project",
		"/use N - switch to the thread numbered by /list",
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
		"backend state: " + status.BackendState,
		"active session: " + activeSession,
		"permission mode: " + status.PermissionMode,
	}

	if strings.TrimSpace(status.LastErrorSummary) != "" {
		lines = append(lines, "last error summary: "+status.LastErrorSummary)
	}

	return strings.Join(lines, "\n")
}
