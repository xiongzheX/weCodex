package backend

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xiongzheX/weCodex/config"
)

func TestCLIClientPromptResumesExistingSessionAndUsesCodexArgs(t *testing.T) {
	t.Parallel()

	type call struct {
		command string
		args    []string
		dir     string
	}
	var got call

	client := &cliClient{
		cfg: config.Config{
			CodexCommand:     "codex",
			CodexArgs:        []string{"--model", "gpt-5"},
			WorkingDirectory: "/tmp/workdir",
		},
		health: HealthSnapshot{State: HealthUnavailable, LastErrorSummary: "not started"},
		commandExistsFn: func(command string) (bool, error) {
			if command != "codex" {
				t.Fatalf("Start() checked command %q, want %q", command, "codex")
			}
			return true, nil
		},
		runCommandFn: func(_ context.Context, command string, args []string, dir string) (string, string, error) {
			got = call{command: command, args: append([]string(nil), args...), dir: dir}
			return "{\"type\":\"thread.started\",\"thread_id\":\"thread-123\"}\n", "", nil
		},
		readFileFn: func(string) ([]byte, error) {
			return []byte("  assistant reply  \n"), nil
		},
	}

	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	res, err := client.Prompt(context.Background(), PromptRequest{Text: "hello", SessionID: "incoming-session"})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}

	if got.command != "codex" {
		t.Fatalf("Prompt() command = %q, want %q", got.command, "codex")
	}
	wantArgs := []string{"exec", "--json", "--output-last-message"}
	if len(got.args) != 9 {
		t.Fatalf("Prompt() args = %v, want 9 args", got.args)
	}
	for i := range wantArgs {
		if got.args[i] != wantArgs[i] {
			t.Fatalf("Prompt() args[%d] = %q, want %q (all args: %v)", i, got.args[i], wantArgs[i], got.args)
		}
	}
	if got.args[3] == "" {
		t.Fatal("Prompt() args[3] is empty temp file path")
	}
	if got.args[4] != "--model" || got.args[5] != "gpt-5" {
		t.Fatalf("Prompt() CodexArgs = %v, want --model gpt-5", got.args[4:6])
	}
	if got.args[6] != "resume" || got.args[7] != "incoming-session" || got.args[8] != "hello" {
		t.Fatalf("Prompt() session args = %v, want resume incoming-session hello", got.args[6:])
	}
	if got.dir != "/tmp/workdir" {
		t.Fatalf("Prompt() dir = %q, want %q", got.dir, "/tmp/workdir")
	}
	if res.ReplyText != "assistant reply" {
		t.Fatalf("Prompt() ReplyText = %q, want %q", res.ReplyText, "assistant reply")
	}
	if res.SessionID != "thread-123" {
		t.Fatalf("Prompt() SessionID = %q, want %q", res.SessionID, "thread-123")
	}
}

func TestCLIClientListSessionsReturnsEmptyWhenNoSessionsFound(t *testing.T) {
	t.Parallel()

	client := &cliClient{
		cfg:             config.Config{CodexCommand: "codex"},
		health:          HealthSnapshot{State: HealthUnavailable, LastErrorSummary: "not started"},
		commandExistsFn: func(string) (bool, error) { return true, nil },
	}

	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	list, err := client.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(list.Sessions) != 0 {
		t.Fatalf("ListSessions() Sessions = %v, want empty", list.Sessions)
	}
}

func TestCLIClientCreateSessionUsesCodexExec(t *testing.T) {
	t.Parallel()

	type call struct {
		command string
		args    []string
		dir     string
	}
	var got call

	client := &cliClient{
		cfg: config.Config{
			CodexCommand:     "codex",
			WorkingDirectory: "/tmp/workdir",
		},
		health:          HealthSnapshot{State: HealthUnavailable, LastErrorSummary: "not started"},
		commandExistsFn: func(string) (bool, error) { return true, nil },
		runCommandFn: func(_ context.Context, command string, args []string, dir string) (string, string, error) {
			got = call{command: command, args: append([]string(nil), args...), dir: dir}
			return "{\"type\":\"thread.started\",\"thread_id\":\"thread-abc\"}\n", "", nil
		},
		readFileFn: func(string) ([]byte, error) {
			return []byte("new thread reply"), nil
		},
	}

	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	res, err := client.CreateSession(context.Background(), SessionCreateRequest{SenderID: "sender-1"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if got.command != "codex" {
		t.Fatalf("CreateSession() command = %q, want %q", got.command, "codex")
	}
	wantArgs := []string{"exec", "--json", "--output-last-message"}
	if len(got.args) != 5 {
		t.Fatalf("CreateSession() args = %v, want 5 args", got.args)
	}
	for i := range wantArgs {
		if got.args[i] != wantArgs[i] {
			t.Fatalf("CreateSession() args[%d] = %q, want %q (all args: %v)", i, got.args[i], wantArgs[i], got.args)
		}
	}
	if got.args[3] == "" {
		t.Fatal("CreateSession() args[3] is empty temp file path")
	}
	if got.args[4] != "wecodex new session for sender-1" {
		t.Fatalf("CreateSession() prompt = %q, want %q", got.args[4], "wecodex new session for sender-1")
	}
	if got.dir != "/tmp/workdir" {
		t.Fatalf("CreateSession() dir = %q, want %q", got.dir, "/tmp/workdir")
	}
	if res.SessionID != "thread-abc" {
		t.Fatalf("CreateSession() SessionID = %q, want %q", res.SessionID, "thread-abc")
	}
	if res.DisplayName != "new thread reply" {
		t.Fatalf("CreateSession() DisplayName = %q, want %q", res.DisplayName, "new thread reply")
	}
}

func TestCLIClientPromptTimeoutMapsToPromptTimeoutError(t *testing.T) {
	t.Parallel()

	client := &cliClient{
		cfg:             config.Config{CodexCommand: "codex"},
		health:          HealthSnapshot{State: HealthUnavailable, LastErrorSummary: "not started"},
		commandExistsFn: func(string) (bool, error) { return true, nil },
		runCommandFn: func(ctx context.Context, _ string, _ []string, _ string) (string, string, error) {
			<-ctx.Done()
			return "", "", ctx.Err()
		},
	}

	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	_, err := client.Prompt(context.Background(), PromptRequest{Text: "hello", Timeout: 20 * time.Millisecond})
	if err == nil {
		t.Fatal("Prompt() error = nil, want timeout error")
	}
	var timeoutErr *PromptTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("Prompt() error = %T, want *PromptTimeoutError", err)
	}

	h := client.Health()
	if h.State != HealthDegraded {
		t.Fatalf("Health().State = %q, want %q", h.State, HealthDegraded)
	}
}

func TestCLIClientPromptEmptyOutputReturnsPromptError(t *testing.T) {
	t.Parallel()

	client := &cliClient{
		cfg:             config.Config{CodexCommand: "codex"},
		health:          HealthSnapshot{State: HealthUnavailable, LastErrorSummary: "not started"},
		commandExistsFn: func(string) (bool, error) { return true, nil },
		runCommandFn: func(_ context.Context, _ string, _ []string, _ string) (string, string, error) {
			return "  \n\t ", "", nil
		},
	}

	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	_, err := client.Prompt(context.Background(), PromptRequest{Text: "hello"})
	if err == nil {
		t.Fatal("Prompt() error = nil, want prompt error")
	}
	var promptErr *PromptError
	if !errors.As(err, &promptErr) {
		t.Fatalf("Prompt() error = %T, want *PromptError", err)
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("Prompt() error = %q, want to mention empty output", err.Error())
	}
}

func TestCLIClientPromptNonZeroExitIncludesStderr(t *testing.T) {
	t.Parallel()

	client := &cliClient{
		cfg:             config.Config{CodexCommand: "codex"},
		health:          HealthSnapshot{State: HealthUnavailable, LastErrorSummary: "not started"},
		commandExistsFn: func(string) (bool, error) { return true, nil },
		runCommandFn: func(_ context.Context, _ string, _ []string, _ string) (string, string, error) {
			return "", "permission denied", errors.New("exit status 1")
		},
	}

	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	_, err := client.Prompt(context.Background(), PromptRequest{Text: "hello"})
	if err == nil {
		t.Fatal("Prompt() error = nil, want prompt error")
	}
	var promptErr *PromptError
	if !errors.As(err, &promptErr) {
		t.Fatalf("Prompt() error = %T, want *PromptError", err)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("Prompt() error = %q, want stderr text", err.Error())
	}
}

func TestCLIClientHealthTracksStartupAndLastError(t *testing.T) {
	t.Parallel()

	commandExists := false
	client := &cliClient{
		cfg:             config.Config{CodexCommand: "codex"},
		health:          HealthSnapshot{State: HealthUnavailable, LastErrorSummary: "not started"},
		commandExistsFn: func(string) (bool, error) { return commandExists, nil },
		runCommandFn: func(_ context.Context, _ string, _ []string, _ string) (string, string, error) {
			return "", "backend unavailable", errors.New("exit status 1")
		},
	}

	if err := client.Start(context.Background()); err == nil {
		t.Fatal("Start() error = nil, want startup error when command is missing")
	}
	var startupErr *StartupError
	if !errors.As(client.lastError(), &startupErr) {
		t.Fatalf("lastError() = %T, want *StartupError", client.lastError())
	}
	h := client.Health()
	if h.State != HealthUnavailable {
		t.Fatalf("Health().State after failed Start = %q, want %q", h.State, HealthUnavailable)
	}
	if h.LastErrorSummary == "" {
		t.Fatal("Health().LastErrorSummary after failed Start is empty")
	}

	commandExists = true
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() retry error = %v", err)
	}
	h = client.Health()
	if h.State != HealthReady {
		t.Fatalf("Health().State after successful Start = %q, want %q", h.State, HealthReady)
	}
	if h.LastErrorSummary != "" {
		t.Fatalf("Health().LastErrorSummary after successful Start = %q, want empty", h.LastErrorSummary)
	}

	_, err := client.Prompt(context.Background(), PromptRequest{Text: "hello"})
	if err == nil {
		t.Fatal("Prompt() error = nil, want prompt failure")
	}
	h = client.Health()
	if h.State != HealthDegraded {
		t.Fatalf("Health().State after failed Prompt = %q, want %q", h.State, HealthDegraded)
	}
	if !strings.Contains(h.LastErrorSummary, "prompt failure") {
		t.Fatalf("Health().LastErrorSummary = %q, want prompt failure summary", h.LastErrorSummary)
	}

	if err := client.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	h = client.Health()
	if h.State != HealthUnavailable {
		t.Fatalf("Health().State after Stop = %q, want %q", h.State, HealthUnavailable)
	}
	if !strings.Contains(strings.ToLower(h.LastErrorSummary), "stopped") {
		t.Fatalf("Health().LastErrorSummary after Stop = %q, want stopped summary", h.LastErrorSummary)
	}
}
