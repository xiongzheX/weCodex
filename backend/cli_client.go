package backend

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

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
	homeDirFn       func() (string, error)
	readFileFn      func(string) ([]byte, error)
	walkDirFn       func(string, fs.WalkDirFunc) error
}

func NewCLIClient(cfg config.Config) Client {
	return &cliClient{
		cfg:             cfg,
		health:          HealthSnapshot{State: HealthUnavailable, LastErrorSummary: "not started"},
		commandExistsFn: config.CodexCommandExists,
		runCommandFn:    runCLICommand,
		homeDirFn:       os.UserHomeDir,
		readFileFn:      os.ReadFile,
		walkDirFn:       filepath.WalkDir,
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

func (c *cliClient) ListSessions(ctx context.Context) (SessionListResult, error) {
	if !c.isStarted() {
		return SessionListResult{}, &NotStartedError{}
	}

	cwd, err := c.currentWorkingDirectory()
	if err != nil {
		return SessionListResult{}, err
	}
	infos, err := c.loadSessionInfos(ctx)
	if err != nil {
		return SessionListResult{}, err
	}

	result := SessionListResult{}
	for _, info := range infos {
		if !samePath(info.CWD, cwd) {
			continue
		}
		result.Sessions = append(result.Sessions, SessionInfo{SessionID: info.ID, DisplayName: info.ThreadName})
	}
	if len(result.Sessions) > 0 {
		result.ActiveSessionID = result.Sessions[0].SessionID
	}
	return result, nil
}

func (c *cliClient) CreateSession(ctx context.Context, req SessionCreateRequest) (SessionInfo, error) {
	if !c.isStarted() {
		return SessionInfo{}, &NotStartedError{}
	}

	res, err := c.runCodexSession(ctx, "", newSessionPrompt(req.SenderID), false)
	if err != nil {
		return SessionInfo{}, err
	}
	return SessionInfo{SessionID: res.SessionID, DisplayName: res.ReplyText}, nil
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

	res, err := c.runCodexSession(callCtx, req.SessionID, req.Text, true)
	if err != nil {
		return PromptResult{}, err
	}
	return PromptResult{SessionID: res.SessionID, ReplyText: res.ReplyText}, nil
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

type codexSessionRecord struct {
	ID         string
	ThreadName string
	UpdatedAt  time.Time
}

type codexSessionFileMeta struct {
	ID         string
	CWD        string
	ThreadName string
}

type codexSessionRunResult struct {
	SessionID string
	ReplyText string
}

func (c *cliClient) currentWorkingDirectory() (string, error) {
	cwd := strings.TrimSpace(c.cfg.WorkingDirectory)
	if cwd != "" {
		return filepath.Clean(cwd), nil
	}
	return os.Getwd()
}

func (c *cliClient) homeDir() (string, error) {
	fn := c.homeDirFn
	if fn == nil {
		fn = os.UserHomeDir
	}
	return fn()
}

func (c *cliClient) readFile(path string) ([]byte, error) {
	fn := c.readFileFn
	if fn == nil {
		fn = os.ReadFile
	}
	return fn(path)
}

func (c *cliClient) walkDir(root string, fn fs.WalkDirFunc) error {
	walker := c.walkDirFn
	if walker == nil {
		walker = filepath.WalkDir
	}
	return walker(root, fn)
}

func (c *cliClient) loadSessionInfos(ctx context.Context) ([]codexSessionFileMeta, error) {
	_ = ctx

	home, err := c.homeDir()
	if err != nil {
		return nil, err
	}
	roots := []string{
		filepath.Join(home, ".codex", "sessions"),
		filepath.Join(home, ".codex", "archived_sessions"),
	}

	infos := make([]codexSessionFileMeta, 0)
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		err = c.walkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
				return nil
			}
			meta, err := c.readSessionMeta(path)
			if err != nil {
				return err
			}
			if meta.ID != "" {
				infos = append(infos, meta)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	indexRecords, err := c.loadSessionIndexRecords()
	if err != nil {
		return nil, err
	}
	infoByID := make(map[string]codexSessionFileMeta, len(infos))
	for _, info := range infos {
		infoByID[info.ID] = info
	}

	sorted := make([]codexSessionRecord, 0, len(indexRecords))
	for _, rec := range indexRecords {
		if info, ok := infoByID[rec.ID]; ok {
			sorted = append(sorted, codexSessionRecord{
				ID:         info.ID,
				ThreadName: rec.ThreadName,
				UpdatedAt:  rec.UpdatedAt,
			})
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].UpdatedAt.Equal(sorted[j].UpdatedAt) {
			return sorted[i].ID > sorted[j].ID
		}
		return sorted[i].UpdatedAt.After(sorted[j].UpdatedAt)
	})

	result := make([]codexSessionFileMeta, 0, len(sorted))
	for _, rec := range sorted {
		if info, ok := infoByID[rec.ID]; ok {
			result = append(result, codexSessionFileMeta{
				ID:         info.ID,
				CWD:        info.CWD,
				ThreadName: rec.ThreadName,
			})
		}
	}
	return result, nil
}

func (c *cliClient) loadSessionIndexRecords() ([]codexSessionRecord, error) {
	home, err := c.homeDir()
	if err != nil {
		return nil, err
	}
	indexPath := filepath.Join(home, ".codex", "session_index.jsonl")
	data, err := c.readFile(indexPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	records := make([]codexSessionRecord, 0)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var payload struct {
			ID         string    `json:"id"`
			ThreadName string    `json:"thread_name"`
			UpdatedAt  time.Time `json:"updated_at"`
		}
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}
		if payload.ID == "" {
			continue
		}
		records = append(records, codexSessionRecord{ID: payload.ID, ThreadName: payload.ThreadName, UpdatedAt: payload.UpdatedAt})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func (c *cliClient) readSessionMeta(path string) (codexSessionFileMeta, error) {
	data, err := c.readFile(path)
	if err != nil {
		return codexSessionFileMeta{}, err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return codexSessionFileMeta{}, err
		}
		return codexSessionFileMeta{}, nil
	}
	line := strings.TrimSpace(scanner.Text())
	if line == "" {
		return codexSessionFileMeta{}, nil
	}
	var payload struct {
		Type    string `json:"type"`
		Payload struct {
			ID  string `json:"id"`
			CWD string `json:"cwd"`
		} `json:"payload"`
	}
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return codexSessionFileMeta{}, nil
	}
	if payload.Type != "session_meta" {
		return codexSessionFileMeta{}, nil
	}
	return codexSessionFileMeta{ID: payload.Payload.ID, CWD: filepath.Clean(payload.Payload.CWD)}, nil
}

func (c *cliClient) runCodexSession(ctx context.Context, sessionID, prompt string, reuse bool) (codexSessionRunResult, error) {
	tmpFile, err := os.CreateTemp("", "wecodex-last-message-*.txt")
	if err != nil {
		return codexSessionRunResult{}, err
	}
	defer func() {
		_ = os.Remove(tmpFile.Name())
	}()
	_ = tmpFile.Close()

	args := []string{"exec", "--json", "--output-last-message", tmpFile.Name()}
	args = append(args, c.cfg.CodexArgs...)
	if reuse && strings.TrimSpace(sessionID) != "" {
		args = append(args, "resume", sessionID)
	}
	if strings.TrimSpace(prompt) != "" {
		args = append(args, prompt)
	}

	runner := c.runCommandFn
	if runner == nil {
		runner = runCLICommand
	}
	stdout, stderr, err := runner(ctx, c.cfg.CodexCommand, args, c.cfg.WorkingDirectory)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			te := &PromptTimeoutError{Err: context.DeadlineExceeded}
			c.setErrorState(HealthDegraded, te)
			return codexSessionRunResult{}, te
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
		return codexSessionRunResult{}, pe
	}

	threadID := parseCodexThreadID(stdout)
	if threadID == "" {
		threadID = sessionID
	}
	replyData, err := c.readFile(tmpFile.Name())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			pe := &PromptError{Err: errors.New("empty output")}
			c.setErrorState(HealthDegraded, pe)
			return codexSessionRunResult{}, pe
		}
		return codexSessionRunResult{}, err
	}
	reply := strings.TrimSpace(string(replyData))
	if reply == "" {
		pe := &PromptError{Err: errors.New("empty output")}
		c.setErrorState(HealthDegraded, pe)
		return codexSessionRunResult{}, pe
	}
	if threadID == "" {
		threadID = sessionID
	}
	return codexSessionRunResult{SessionID: threadID, ReplyText: reply}, nil
}

func parseCodexThreadID(stdout string) string {
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var payload struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
		}
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}
		if payload.Type == "thread.started" && payload.ThreadID != "" {
			return payload.ThreadID
		}
	}
	return ""
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func newSessionPrompt(senderID string) string {
	prompt := "wecodex new session"
	if strings.TrimSpace(senderID) == "" {
		return prompt
	}
	return prompt + " for " + senderID
}
