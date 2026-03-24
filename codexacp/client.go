package codexacp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

const replyTruncationSuffix = "\n\n[回复已截断]"
const maxReplyRunes = 4000

type Client struct {
	mu      sync.RWMutex
	cfg     Config
	rt      runtimeClient
	started bool
	health  HealthSnapshot

	runtimeFactory func() runtimeClient

	transportErr error
	stashedNotifs []runtimeNotification
}

func NewClient(cfg Config) *Client {
	return &Client{
		cfg:    cfg,
		health: HealthSnapshot{State: HealthUnavailable, LastErrorSummary: "not started"},
	}
}

func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return nil
	}
	factory := c.runtimeFactory
	if factory == nil {
		factory = func() runtimeClient { return newStdioRuntime() }
	}
	rt := factory()
	startCfg := c.cfg
	startCfg.PermissionMode = "readonly"
	c.rt = rt
	c.mu.Unlock()

	if err := rt.Start(ctx, startCfg); err != nil {
		se := &StartupError{Err: err}
		c.setHealth(HealthUnavailable, se.Error())
		return se
	}

	if err := c.callRPC(ctx, "initialize", InitializeParams{PermissionMode: "readonly"}, &InitializeResult{}); err != nil {
		_ = rt.Stop()
		c.mu.Lock()
		if c.rt == rt {
			c.rt = nil
		}
		c.mu.Unlock()
		se := &StartupError{Err: err}
		if isTransportErr(err) {
			se = &StartupError{Err: &TransportError{Err: err}}
		}
		c.setHealth(HealthUnavailable, se.Error())
		return se
	}

	c.mu.Lock()
	c.started = true
	c.transportErr = nil
	c.health = HealthSnapshot{State: HealthReady, LastErrorSummary: ""}
	c.mu.Unlock()
	return nil
}

func (c *Client) Stop() error {
	c.mu.RLock()
	rt := c.rt
	c.mu.RUnlock()
	if rt == nil {
		return nil
	}
	if err := rt.Stop(); err != nil {
		te := &TransportError{Err: err}
		c.setHealth(HealthUnavailable, te.Error())
		return te
	}
	c.mu.Lock()
	c.started = false
	c.mu.Unlock()
	c.setHealth(HealthUnavailable, "subprocess stopped")
	return nil
}

func (c *Client) Prompt(ctx context.Context, req PromptRequest) (PromptResult, error) {
	if !c.isStarted() {
		return PromptResult{}, &NotStartedError{}
	}
	if err := c.currentTransportError(); err != nil {
		te := &TransportError{Err: err}
		c.setHealth(HealthUnavailable, te.Error())
		return PromptResult{}, te
	}

	sessionID := req.SessionID
	if strings.TrimSpace(sessionID) == "" {
		var sessionRes SessionNewResult
		if err := c.callRPC(ctx, "session/new", SessionNewParams{SenderID: req.SenderID}, &sessionRes); err != nil {
			if isTransportErr(err) {
				te := &TransportError{Err: err}
				c.setHealth(HealthUnavailable, te.Error())
				return PromptResult{}, te
			}
			se := &SessionError{Err: err}
			c.setHealth(HealthDegraded, se.Error())
			return PromptResult{}, se
		}
		sessionID = sessionRes.SessionID
	}

	c.drainBufferedSessionUpdates(sessionID)

	callCtx := ctx
	cancel := func() {}
	if req.Timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, req.Timeout)
	}
	defer cancel()

	timedOut := false
	deniedReason := ""
	normalizationFailure := false
	builder := strings.Builder{}

	handleNotification := func(n runtimeNotification) {
		switch n.Method {
		case "session/update":
			var up SessionUpdateParams
			if err := unmarshalParams(n.Params, &up); err != nil {
				return
			}
			if timedOut || up.SessionID != sessionID {
				return
			}
			builder.WriteString(up.Text)
		case "session/request_permission":
			var prm SessionRequestPermissionParams
			if err := unmarshalParams(n.Params, &prm); err != nil {
				return
			}
			if prm.SessionID != sessionID {
				return
			}
			norm, err := NormalizePermissionRequest(prm.ToolCall)
			if err != nil {
				deniedReason = err.Error()
				normalizationFailure = true
				c.setHealth(HealthDegraded, (&PermissionError{Err: err}).Error())
				_ = c.callRPC(context.Background(), "session/respond_permission", SessionRespondPermissionParams{SessionID: sessionID, Allowed: false, Reason: deniedReason}, nil)
				return
			}
			decision := EvaluatePermission(c.cfg.WorkingDirectory, norm)
			if !decision.Allowed {
				deniedReason = decision.Reason
			}
			_ = c.callRPC(context.Background(), "session/respond_permission", SessionRespondPermissionParams{SessionID: sessionID, Allowed: decision.Allowed, Reason: decision.Reason}, nil)
		}
	}

	drainMatching := func() {
		for {
			if n, ok := c.popStashedNotification(); ok {
				handleNotification(n)
				continue
			}
			select {
			case n := <-c.rt.Notifications():
				handleNotification(n)
			default:
				return
			}
		}
	}

	done := make(chan error, 1)
	go func() {
		var promptRes SessionPromptResult
		err := c.callRPC(callCtx, "session/prompt", SessionPromptParams{SessionID: sessionID, Text: req.Text}, &promptRes)
		done <- err
	}()

	for {
		if n, ok := c.popStashedNotification(); ok {
			handleNotification(n)
			continue
		}
		select {
		case err := <-done:
			if err != nil {
				drainMatching()
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(callCtx.Err(), context.DeadlineExceeded) {
					timedOut = true
					te := &PromptTimeoutError{Err: context.DeadlineExceeded}
					c.setHealth(HealthDegraded, te.Error())
					return PromptResult{}, te
				}
				if isTransportErr(err) {
					te := &TransportError{Err: err}
					c.setHealth(HealthUnavailable, te.Error())
					return PromptResult{}, te
				}
				if deniedReason != "" {
					pe := &PermissionError{Err: fmt.Errorf("%s", deniedReason)}
					c.setHealth(HealthDegraded, pe.Error())
					return PromptResult{}, pe
				}
				pe := &PromptError{Err: err}
				c.setHealth(HealthDegraded, pe.Error())
				return PromptResult{}, pe
			}
			drainMatching()
			if normalizationFailure {
				pe := &PermissionError{Err: fmt.Errorf("%s", deniedReason)}
				c.setHealth(HealthDegraded, pe.Error())
				return PromptResult{}, pe
			}
			reply := builder.String()
			if strings.TrimSpace(reply) == "" {
				pe := &PromptError{Err: errors.New("empty reply")}
				c.setHealth(HealthDegraded, pe.Error())
				return PromptResult{}, pe
			}
			reply = truncateReply(reply)
			return PromptResult{SessionID: sessionID, ReplyText: reply}, nil
		case n := <-c.rt.Notifications():
			handleNotification(n)
		case err := <-c.rt.Errors():
			if err == nil {
				continue
			}
			c.setTransportError(err)
			te := &TransportError{Err: err}
			c.setHealth(HealthUnavailable, te.Error())
			return PromptResult{}, te
		case <-callCtx.Done():
			timedOut = true
			te := &PromptTimeoutError{Err: callCtx.Err()}
			c.setHealth(HealthDegraded, te.Error())
			return PromptResult{}, te
		}
	}
}

func (c *Client) Health() HealthSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.health
}

func (c *Client) isStarted() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.started
}

func (c *Client) callRPC(ctx context.Context, method string, params any, result any) error {
	c.mu.RLock()
	rt := c.rt
	c.mu.RUnlock()
	if rt == nil {
		return &NotStartedError{}
	}
	if err := rt.Call(ctx, method, params, result); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return err
		}
		return err
	}
	return nil
}

func (c *Client) setHealth(state HealthState, summary string) {
	c.mu.Lock()
	c.health = HealthSnapshot{State: state, LastErrorSummary: summary}
	c.mu.Unlock()
}

func (c *Client) setTransportError(err error) {
	c.mu.Lock()
	c.transportErr = err
	c.mu.Unlock()
}

func (c *Client) currentTransportError() error {
	c.mu.RLock()
	rt := c.rt
	c.mu.RUnlock()
	if rt != nil {
		select {
		case err := <-rt.Errors():
			if err != nil {
				c.setTransportError(err)
			}
		default:
		}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.transportErr
}

func (c *Client) stashNotification(n runtimeNotification) {
	c.mu.Lock()
	c.stashedNotifs = append(c.stashedNotifs, n)
	c.mu.Unlock()
}

func (c *Client) popStashedNotification() (runtimeNotification, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.stashedNotifs) == 0 {
		return runtimeNotification{}, false
	}
	n := c.stashedNotifs[0]
	c.stashedNotifs = c.stashedNotifs[1:]
	return n, true
}

func (c *Client) drainBufferedSessionUpdates(sessionID string) {
	for {
		select {
		case n := <-c.rt.Notifications():
			if n.Method != "session/update" {
				c.stashNotification(n)
				continue
			}
			var up SessionUpdateParams
			if err := unmarshalParams(n.Params, &up); err != nil {
				continue
			}
			if up.SessionID != sessionID {
				c.stashNotification(n)
			}
		default:
			return
		}
	}
}

func unmarshalParams(data []byte, v any) error {
	if len(data) == 0 {
		return errors.New("empty params")
	}
	return jsonUnmarshal(data, v)
}

var jsonUnmarshal = func(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func truncateReply(s string) string {
	if utf8.RuneCountInString(s) <= maxReplyRunes {
		return s
	}
	suffixRunes := utf8.RuneCountInString(replyTruncationSuffix)
	keep := maxReplyRunes - suffixRunes
	if keep < 0 {
		keep = 0
	}
	r := []rune(s)
	if len(r) > keep {
		r = r[:keep]
	}
	return string(r) + replyTruncationSuffix
}

type stdioRuntime struct {
	mu       sync.Mutex
	writeMu  sync.Mutex
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	pending  map[string]chan RPCResponseEnvelope
	nextID   uint64
	notifCh  chan runtimeNotification
	errCh    chan error
	doneCh   chan struct{}
	stopping bool
	cancel   context.CancelFunc
	stderr   bytes.Buffer
}

func newStdioRuntime() *stdioRuntime {
	return &stdioRuntime{
		pending: make(map[string]chan RPCResponseEnvelope),
		notifCh: make(chan runtimeNotification, 256),
		errCh:   make(chan error, 32),
		doneCh:  make(chan struct{}),
	}
}

func (r *stdioRuntime) Start(_ context.Context, cfg Config) error {
	if strings.TrimSpace(cfg.Command) == "" {
		return errors.New("missing command")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil {
		return nil
	}

	procCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(procCtx, cfg.Command, cfg.Args...)
	cmd.Dir = cfg.WorkingDirectory

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stdin pipe: %w", err)
	}
	cmd.Stderr = &r.stderr

	if err := cmd.Start(); err != nil {
		cancel()
		if msg := strings.TrimSpace(r.stderr.String()); msg != "" {
			return fmt.Errorf("subprocess start failed: %w: %s", err, msg)
		}
		return fmt.Errorf("subprocess start failed: %w", err)
	}

	r.cmd = cmd
	r.stdin = stdin
	r.cancel = cancel
	r.stopping = false

	go r.readLoop(stdout)
	go r.waitLoop()
	return nil
}

func (r *stdioRuntime) Stop() error {
	r.mu.Lock()
	cmd := r.cmd
	stdin := r.stdin
	cancel := r.cancel
	if cmd == nil {
		r.mu.Unlock()
		return nil
	}
	r.stopping = true
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	<-r.doneCh
	return nil
}

func (r *stdioRuntime) Call(ctx context.Context, method string, params any, result any) error {
	r.mu.Lock()
	stdin := r.stdin
	if stdin == nil {
		r.mu.Unlock()
		return errors.New("transport closed")
	}
	id := strconv.FormatUint(atomic.AddUint64(&r.nextID, 1), 10)
	respCh := make(chan RPCResponseEnvelope, 1)
	r.pending[id] = respCh
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		delete(r.pending, id)
		r.mu.Unlock()
	}()

	req := RPCRequestEnvelope{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}

	r.writeMu.Lock()
	_, err = stdin.Write(append(payload, '\n'))
	r.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("stdin write failed: %w", err)
	}

	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return fmt.Errorf("rpc error (%d): %s", resp.Error.Code, resp.Error.Message)
		}
		if result == nil || len(resp.Result) == 0 {
			return nil
		}
		return json.Unmarshal(resp.Result, result)
	case <-ctx.Done():
		return ctx.Err()
	case <-r.doneCh:
		return errors.New("process exited")
	}
}

func (r *stdioRuntime) Notifications() <-chan runtimeNotification { return r.notifCh }
func (r *stdioRuntime) Errors() <-chan error                      { return r.errCh }

func (r *stdioRuntime) readLoop(stdout io.Reader) {
	reader := bufio.NewReader(stdout)
	for {
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			r.handleIncoming(bytes.TrimSpace(line))
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				r.sendTransportError(fmt.Errorf("stdout read failed: %w", err))
			}
			return
		}
	}
}

func (r *stdioRuntime) handleIncoming(line []byte) {
	var msg struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id,omitempty"`
		Method  string          `json:"method,omitempty"`
		Params  json.RawMessage `json:"params,omitempty"`
		Result  json.RawMessage `json:"result,omitempty"`
		Error   *RPCError       `json:"error,omitempty"`
	}
	if err := json.Unmarshal(line, &msg); err != nil {
		r.sendTransportError(fmt.Errorf("invalid json-rpc message: %w", err))
		return
	}
	if msg.Method != "" {
		r.notifCh <- runtimeNotification{Method: msg.Method, Params: msg.Params}
		return
	}
	id, ok := msg.ID.(string)
	if !ok || id == "" {
		return
	}

	r.mu.Lock()
	respCh := r.pending[id]
	r.mu.Unlock()
	if respCh == nil {
		return
	}
	respCh <- RPCResponseEnvelope{JSONRPC: msg.JSONRPC, ID: msg.ID, Result: msg.Result, Error: msg.Error}
}

func (r *stdioRuntime) waitLoop() {
	err := r.cmd.Wait()
	if err != nil {
		r.mu.Lock()
		stopping := r.stopping
		stderr := strings.TrimSpace(r.stderr.String())
		r.mu.Unlock()
		if !stopping {
			if stderr != "" {
				r.sendTransportError(fmt.Errorf("process exited: %w: %s", err, stderr))
			} else {
				r.sendTransportError(fmt.Errorf("process exited: %w", err))
			}
		}
	}
	r.failPending(errors.New("process exited"))
	close(r.doneCh)

	r.mu.Lock()
	if r.cancel != nil {
		r.cancel()
	}
	r.cmd = nil
	r.stdin = nil
	r.cancel = nil
	r.mu.Unlock()
}

func (r *stdioRuntime) failPending(err error) {
	r.mu.Lock()
	pending := make(map[string]chan RPCResponseEnvelope, len(r.pending))
	for id, ch := range r.pending {
		pending[id] = ch
		delete(r.pending, id)
	}
	r.mu.Unlock()

	for id, ch := range pending {
		resp := RPCResponseEnvelope{ID: id, Error: &RPCError{Code: -32000, Message: err.Error()}}
		select {
		case ch <- resp:
		default:
		}
	}
}

func (r *stdioRuntime) sendTransportError(err error) {
	if err == nil {
		return
	}
	select {
	case r.errCh <- err:
	default:
	}
}

func isTransportErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "transport") || strings.Contains(msg, "stdout") || strings.Contains(msg, "stdin") || strings.Contains(msg, "i/o") || strings.Contains(msg, "broken pipe") || strings.Contains(msg, "process exited")
}
