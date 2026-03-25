package backend

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/xiongzheX/weCodex/config"
)

type commandExistsFunc func(command string) (bool, error)
type runCommandFunc func(ctx context.Context, command string, args []string, dir string) (stdout string, stderr string, err error)

type cliClient struct {
	mu      sync.RWMutex
	cfg     config.Config
	started bool
	health  HealthSnapshot
	err     error

	commandExistsFn commandExistsFunc
	runCommandFn    runCommandFunc
}

func NewCLIClient(cfg config.Config) Client {
	return &cliClient{
		cfg:             cfg,
		health:          HealthSnapshot{State: HealthUnavailable, LastErrorSummary: "not started"},
		commandExistsFn: config.CodexCommandExists,
		runCommandFn:    runCLICommand,
	}
}

func runCLICommand(ctx context.Context, command string, args []string, dir string) (string, string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func (c *cliClient) Start(ctx context.Context) error {
	_ = ctx

	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return nil
	}
	command := c.cfg.CodexCommand
	existsFn := c.commandExistsFn
	if existsFn == nil {
		existsFn = config.CodexCommandExists
	}
	c.mu.Unlock()

	exists, err := existsFn(command)
	if err != nil {
		se := &StartupError{Err: err}
		c.setErrorState(HealthUnavailable, se)
		return se
	}
	if !exists {
		se := &StartupError{Err: fmt.Errorf("command not found: %s", command)}
		c.setErrorState(HealthUnavailable, se)
		return se
	}

	c.mu.Lock()
	c.started = true
	c.health = HealthSnapshot{State: HealthReady, LastErrorSummary: ""}
	c.err = nil
	c.mu.Unlock()
	return nil
}

func (c *cliClient) Stop() error {
	c.mu.Lock()
	c.started = false
	c.health = HealthSnapshot{State: HealthUnavailable, LastErrorSummary: "subprocess stopped"}
	c.err = nil
	c.mu.Unlock()
	return nil
}

func (c *cliClient) Prompt(ctx context.Context, req PromptRequest) (PromptResult, error) {
	if !c.isStarted() {
		return PromptResult{}, &NotStartedError{}
	}

	callCtx := ctx
	cancel := func() {}
	if req.Timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, req.Timeout)
	}
	defer cancel()

	args := make([]string, 0, 2+len(c.cfg.CodexArgs))
	args = append(args, "exec")
	args = append(args, c.cfg.CodexArgs...)
	args = append(args, req.Text)

	runner := c.runCommandFn
	if runner == nil {
		runner = runCLICommand
	}
	stdout, stderr, err := runner(callCtx, c.cfg.CodexCommand, args, c.cfg.WorkingDirectory)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			te := &PromptTimeoutError{Err: context.DeadlineExceeded}
			c.setErrorState(HealthDegraded, te)
			return PromptResult{}, te
		}
		msg := strings.TrimSpace(err.Error())
		if trimmedStderr := strings.TrimSpace(stderr); trimmedStderr != "" {
			if msg != "" {
				msg = msg + ": " + trimmedStderr
			} else {
				msg = trimmedStderr
			}
		}
		pe := &PromptError{Err: errors.New(msg)}
		c.setErrorState(HealthDegraded, pe)
		return PromptResult{}, pe
	}

	reply := strings.TrimSpace(stdout)
	if reply == "" {
		pe := &PromptError{Err: errors.New("empty output")}
		c.setErrorState(HealthDegraded, pe)
		return PromptResult{}, pe
	}

	return PromptResult{SessionID: "", ReplyText: reply}, nil
}

func (c *cliClient) Health() HealthSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.health
}

func (c *cliClient) isStarted() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.started
}

func (c *cliClient) setErrorState(state HealthState, err error) {
	summary := ""
	if err != nil {
		summary = err.Error()
	}
	c.mu.Lock()
	c.health = HealthSnapshot{State: state, LastErrorSummary: summary}
	c.err = err
	c.mu.Unlock()
}

func (c *cliClient) lastError() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.err
}
