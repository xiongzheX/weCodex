package backend

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xiongzheX/weCodex/config"
)

func TestCLIClientPromptRunsCodexExec(t *testing.T) {
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
			return "  assistant reply  \n", "", nil
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
	wantArgs := []string{"exec", "--model", "gpt-5", "hello"}
	if len(got.args) != len(wantArgs) {
		t.Fatalf("Prompt() args = %v, want %v", got.args, wantArgs)
	}
	for i := range wantArgs {
		if got.args[i] != wantArgs[i] {
			t.Fatalf("Prompt() args[%d] = %q, want %q (all args: %v)", i, got.args[i], wantArgs[i], got.args)
		}
	}
	if got.dir != "/tmp/workdir" {
		t.Fatalf("Prompt() dir = %q, want %q", got.dir, "/tmp/workdir")
	}
	if res.ReplyText != "assistant reply" {
		t.Fatalf("Prompt() ReplyText = %q, want %q", res.ReplyText, "assistant reply")
	}
	if res.SessionID != "" {
		t.Fatalf("Prompt() SessionID = %q, want empty", res.SessionID)
	}
}

func TestCLIClientPromptTimeoutMapsToPromptTimeoutError(t *testing.T) {
	t.Parallel()

	client := &cliClient{
		cfg: config.Config{CodexCommand: "codex"},
		health: HealthSnapshot{State: HealthUnavailable, LastErrorSummary: "not started"},
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
		cfg: config.Config{CodexCommand: "codex"},
		health: HealthSnapshot{State: HealthUnavailable, LastErrorSummary: "not started"},
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
		cfg: config.Config{CodexCommand: "codex"},
		health: HealthSnapshot{State: HealthUnavailable, LastErrorSummary: "not started"},
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
		cfg: config.Config{CodexCommand: "codex"},
		health: HealthSnapshot{State: HealthUnavailable, LastErrorSummary: "not started"},
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
