package bridge

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiongzheX/weCodex/backend"
	"github.com/xiongzheX/weCodex/ilink"
)

func TestNewServicePanicsOnNilACP(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected NewService to panic on nil ACP client")
		}
	}()

	_ = NewService(nil)
}

func TestHandleMessageReusesSessionPerSender(t *testing.T) {
	acp := &stubACPClient{}
	acp.promptFn = func(_ context.Context, req backend.PromptRequest) (backend.PromptResult, error) {
		switch len(acp.PromptCalls()) {
		case 1:
			if req.SessionID != "" {
				t.Fatalf("first prompt should not carry session, got %q", req.SessionID)
			}
			if req.Timeout != 120*time.Second {
				t.Fatalf("first prompt should use 120s timeout, got %s", req.Timeout)
			}
			return backend.PromptResult{SessionID: "s1", ReplyText: "r1"}, nil
		case 2:
			if req.SessionID != "s1" {
				t.Fatalf("second prompt should reuse s1, got %q", req.SessionID)
			}
			return backend.PromptResult{SessionID: "s1", ReplyText: "r2"}, nil
		default:
			t.Fatalf("unexpected extra prompt call")
			return backend.PromptResult{}, nil
		}
	}

	svc := NewService(acp)
	msg := func(text string) ilink.InboundMessage {
		return ilink.InboundMessage{FromUserID: "u1", ContextToken: "ctx", Text: text}
	}

	if _, err := svc.HandleMessage(context.Background(), msg("hello")); err != nil {
		t.Fatalf("first prompt: %v", err)
	}
	if _, err := svc.HandleMessage(context.Background(), msg("again")); err != nil {
		t.Fatalf("second prompt: %v", err)
	}
}

func TestHandleMessageKeepsSeparateSessionsPerSender(t *testing.T) {
	acp := &stubACPClient{}
	acp.promptFn = func(_ context.Context, req backend.PromptRequest) (backend.PromptResult, error) {
		switch req.SenderID {
		case "u1":
			if len(callsBySender(acp.PromptCalls(), "u1")) == 1 {
				if req.SessionID != "" {
					t.Fatalf("u1 first should be empty session, got %q", req.SessionID)
				}
				return backend.PromptResult{SessionID: "s-u1", ReplyText: "ok"}, nil
			}
			if req.SessionID != "s-u1" {
				t.Fatalf("u1 should reuse s-u1, got %q", req.SessionID)
			}
			return backend.PromptResult{SessionID: "s-u1", ReplyText: "ok"}, nil
		case "u2":
			if req.SessionID != "" {
				t.Fatalf("u2 first should be empty session, got %q", req.SessionID)
			}
			return backend.PromptResult{SessionID: "s-u2", ReplyText: "ok"}, nil
		default:
			t.Fatalf("unexpected sender %q", req.SenderID)
			return backend.PromptResult{}, nil
		}
	}

	svc := NewService(acp)
	_, _ = svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u1", Text: "a"})
	_, _ = svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u2", Text: "b"})
	_, _ = svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u1", Text: "c"})
}

func TestHandleMessageReusesInboundAddressingOnPromptReply(t *testing.T) {
	acp := &stubACPClient{}
	acp.promptFn = func(_ context.Context, _ backend.PromptRequest) (backend.PromptResult, error) {
		return backend.PromptResult{SessionID: "s1", ReplyText: "reply"}, nil
	}

	svc := NewService(acp)
	in := ilink.InboundMessage{FromUserID: "sender-1", ContextToken: "ctx-1", Text: "hello"}
	out, err := svc.HandleMessage(context.Background(), in)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.ToUserID != in.FromUserID {
		t.Fatalf("to_user mismatch: want %q got %q", in.FromUserID, out.ToUserID)
	}
	if out.ContextToken != in.ContextToken {
		t.Fatalf("context token mismatch: want %q got %q", in.ContextToken, out.ContextToken)
	}
	if out.Text != "reply" {
		t.Fatalf("reply text mismatch: %q", out.Text)
	}
}

func TestHandleMessageHelpStatusAndNew(t *testing.T) {
	acp := &stubACPClient{health: backend.HealthSnapshot{State: backend.HealthReady, LastErrorSummary: ""}}
	svc := NewService(acp)

	help, err := svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u", ContextToken: "c", Text: "/help"})
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(help.Text, "/help") || !strings.Contains(help.Text, "/status") || !strings.Contains(help.Text, "/new") {
		t.Fatalf("help text missing commands: %q", help.Text)
	}

	status, err := svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u", ContextToken: "c", Text: "/status"})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(status.Text, "backend state: ready") {
		t.Fatalf("status should include health state, got %q", status.Text)
	}
	if !strings.Contains(status.Text, "permission mode: read-only") {
		t.Fatalf("status should include read-only permission mode, got %q", status.Text)
	}

	acp.promptFn = func(_ context.Context, _ backend.PromptRequest) (backend.PromptResult, error) {
		return backend.PromptResult{SessionID: "s1", ReplyText: "ok"}, nil
	}
	_, _ = svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u", Text: "hello"})
	if !svc.HasActiveSession("u") {
		t.Fatal("expected active session after prompt")
	}

	_, err = svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u", Text: "/new"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if svc.HasActiveSession("u") {
		t.Fatal("expected /new to clear active session when idle")
	}
}

func TestHandleMessageThreadCommandsUseBackendSessionState(t *testing.T) {
	acp := &stubACPClient{}
	acp.listFn = func(context.Context) (backend.SessionListResult, error) {
		return backend.SessionListResult{
			ActiveSessionID: "s2",
			Sessions: []backend.SessionInfo{
				{SessionID: "s1", DisplayName: "alpha"},
				{SessionID: "s2", DisplayName: "beta"},
			},
		}, nil
	}
	acp.createFn = func(context.Context, backend.SessionCreateRequest) (backend.SessionInfo, error) {
		return backend.SessionInfo{SessionID: "s3", DisplayName: "gamma"}, nil
	}
	acp.promptFn = func(_ context.Context, req backend.PromptRequest) (backend.PromptResult, error) {
		if req.SessionID != "s2" && req.SessionID != "s3" {
			t.Fatalf("unexpected session id %q", req.SessionID)
		}
		return backend.PromptResult{SessionID: req.SessionID, ReplyText: "ok"}, nil
	}

	svc := NewService(acp)

	listOut, err := svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u", Text: "/list"})
	if err != nil {
		t.Fatalf("/list: %v", err)
	}
	if !strings.Contains(listOut.Text, "1.") || !strings.Contains(listOut.Text, "2.") || !strings.Contains(listOut.Text, "[当前]") {
		t.Fatalf("list output missing numbering or current marker: %q", listOut.Text)
	}

	useOut, err := svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u", Text: "/use 2"})
	if err != nil {
		t.Fatalf("/use: %v", err)
	}
	if !strings.Contains(useOut.Text, "已切换到线程 2") {
		t.Fatalf("unexpected /use confirmation: %q", useOut.Text)
	}

	promptOut, err := svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u", Text: "hello"})
	if err != nil {
		t.Fatalf("prompt after /use: %v", err)
	}
	if promptOut.Text != "ok" {
		t.Fatalf("unexpected prompt reply after /use: %q", promptOut.Text)
	}

	newOut, err := svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u", Text: "/new"})
	if err != nil {
		t.Fatalf("/new: %v", err)
	}
	if !strings.Contains(newOut.Text, "已切换到新线程") {
		t.Fatalf("unexpected /new confirmation: %q", newOut.Text)
	}
	if !svc.HasActiveSession("u") {
		t.Fatal("expected active session after /new")
	}
}

func TestHandleMessageGlobalBusyLockOnNormalPrompts(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})

	acp := &stubACPClient{}
	acp.promptFn = func(_ context.Context, req backend.PromptRequest) (backend.PromptResult, error) {
		if req.Text == "first" {
			entered <- struct{}{}
			<-release
			return backend.PromptResult{SessionID: "s1", ReplyText: "done"}, nil
		}
		return backend.PromptResult{SessionID: "s2", ReplyText: "other"}, nil
	}

	svc := NewService(acp)
	firstDone := make(chan OutboundReply, 1)
	go func() {
		out, _ := svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u1", Text: "first"})
		firstDone <- out
	}()

	<-entered
	busyOut, err := svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u2", ContextToken: "ctx2", Text: "second"})
	if err != nil {
		t.Fatalf("busy call: %v", err)
	}
	if !strings.Contains(busyOut.Text, "还在处理中") {
		t.Fatalf("expected busy text, got %q", busyOut.Text)
	}
	if len(acp.PromptCalls()) != 1 {
		t.Fatalf("busy request should not queue prompt call, got %d calls", len(acp.PromptCalls()))
	}

	close(release)
	<-firstDone
}

func TestHandleMessageHelpAndStatusBypassBusyLock(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	acp := &stubACPClient{health: backend.HealthSnapshot{State: backend.HealthDegraded, LastErrorSummary: "x"}}
	acp.promptFn = func(_ context.Context, req backend.PromptRequest) (backend.PromptResult, error) {
		if req.Text == "hold" {
			entered <- struct{}{}
			<-release
		}
		return backend.PromptResult{SessionID: "s", ReplyText: "ok"}, nil
	}

	svc := NewService(acp)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u1", Text: "hold"})
	}()
	<-entered

	helpOut, err := svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u2", Text: "/help"})
	if err != nil {
		t.Fatalf("help during busy: %v", err)
	}
	if !strings.Contains(helpOut.Text, "/help") {
		t.Fatalf("unexpected help text: %q", helpOut.Text)
	}

	statusOut, err := svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u2", Text: "/status"})
	if err != nil {
		t.Fatalf("status during busy: %v", err)
	}
	if !strings.Contains(statusOut.Text, "backend state:") {
		t.Fatalf("unexpected status text: %q", statusOut.Text)
	}

	if len(acp.PromptCalls()) != 1 {
		t.Fatalf("help/status should not call prompt, got %d prompt calls", len(acp.PromptCalls()))
	}
	close(release)
	<-done
}

func TestHandleMessageNewRejectedOnlyForBusyLockHolder(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})

	acp := &stubACPClient{}
	acp.promptFn = func(_ context.Context, req backend.PromptRequest) (backend.PromptResult, error) {
		switch req.SenderID {
		case "u1":
			if req.Text == "prep" {
				return backend.PromptResult{SessionID: "s-u1", ReplyText: "ok"}, nil
			}
			if req.Text == "hold" {
				entered <- struct{}{}
				<-release
				return backend.PromptResult{SessionID: "s-u1", ReplyText: "ok"}, nil
			}
		case "u2":
			return backend.PromptResult{SessionID: "s-u2", ReplyText: "ok"}, nil
		}
		return backend.PromptResult{SessionID: "s", ReplyText: "ok"}, nil
	}

	svc := NewService(acp)
	_, _ = svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u1", Text: "prep"})
	_, _ = svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u2", Text: "prep"})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u1", Text: "hold"})
	}()
	<-entered

	out1, err := svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u1", Text: "/new"})
	if err != nil {
		t.Fatalf("u1 /new: %v", err)
	}
	if !strings.Contains(out1.Text, "还在处理中") {
		t.Fatalf("expected busy for lock holder, got %q", out1.Text)
	}
	if !svc.HasActiveSession("u1") {
		t.Fatal("u1 session should not be cleared while holder is busy")
	}

	out2, err := svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u2", Text: "/new"})
	if err != nil {
		t.Fatalf("u2 /new: %v", err)
	}
	if strings.Contains(out2.Text, "还在处理中") {
		t.Fatalf("u2 /new should not be blocked by u1 busy lock, got %q", out2.Text)
	}
	if svc.HasActiveSession("u2") {
		t.Fatal("u2 session should be cleared during u1 busy lock")
	}

	close(release)
	<-done
}

func TestHandleMessageIdleNewCreatesFreshSessionOnNextPrompt(t *testing.T) {
	acp := &stubACPClient{}
	acp.promptFn = func(_ context.Context, req backend.PromptRequest) (backend.PromptResult, error) {
		if req.Text == "first" {
			if req.SessionID != "" {
				t.Fatalf("first prompt session should be empty, got %q", req.SessionID)
			}
			return backend.PromptResult{SessionID: "s1", ReplyText: "ok"}, nil
		}
		if req.Text == "second" {
			if req.SessionID != "" {
				t.Fatalf("second prompt should start fresh after /new, got %q", req.SessionID)
			}
			return backend.PromptResult{SessionID: "s2", ReplyText: "ok"}, nil
		}
		return backend.PromptResult{}, nil
	}

	svc := NewService(acp)
	_, _ = svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u", Text: "first"})
	if !svc.HasActiveSession("u") {
		t.Fatal("expected active session after first prompt")
	}
	_, _ = svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u", Text: "/new"})
	if svc.HasActiveSession("u") {
		t.Fatal("expected cleared session after /new")
	}
	_, _ = svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u", Text: "second"})
}

func TestHandleMessageFailurePathsClearSessionAndReuseAddressing(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		successText string
		wantText    string
		wantNot     string
	}{
		{name: "timeout", err: &backend.PromptTimeoutError{Err: context.DeadlineExceeded}, wantText: "超时"},
		{name: "session", err: &backend.SessionError{Err: errors.New("broken session")}, wantText: "会话"},
		{name: "permission", err: &backend.PermissionError{Err: errors.New("tool not allowed")}, wantText: "tool not allowed", wantNot: "超时"},
		{name: "prompt", err: &backend.PromptError{Err: errors.New("prompt failed")}, wantText: "失败"},
		{name: "generic", err: errors.New("boom"), wantText: "失败"},
		{name: "empty_reply_success", successText: "   ", wantText: "失败"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			acp := &stubACPClient{}
			call := 0
			acp.promptFn = func(_ context.Context, req backend.PromptRequest) (backend.PromptResult, error) {
				call++
				if call == 1 {
					return backend.PromptResult{SessionID: "s1", ReplyText: "ok"}, nil
				}
				if req.SessionID != "s1" {
					t.Fatalf("expected to reuse s1 before failure, got %q", req.SessionID)
				}
				if tc.err != nil {
					return backend.PromptResult{}, tc.err
				}
				return backend.PromptResult{SessionID: "s1", ReplyText: tc.successText}, nil
			}

			svc := NewService(acp)
			_, _ = svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u", Text: "first"})
			if !svc.HasActiveSession("u") {
				t.Fatal("expected active session before failure")
			}

			in := ilink.InboundMessage{FromUserID: "u", ContextToken: "ctx", Text: "second"}
			out, err := svc.HandleMessage(context.Background(), in)
			if err != nil {
				t.Fatalf("handle failure path: %v", err)
			}
			if out.ToUserID != in.FromUserID || out.ContextToken != in.ContextToken {
				t.Fatalf("failure reply must keep addressing, got %+v", out)
			}
			if !strings.Contains(strings.ToLower(out.Text), strings.ToLower(tc.wantText)) {
				t.Fatalf("expected text to contain %q, got %q", tc.wantText, out.Text)
			}
			if tc.wantNot != "" && strings.Contains(strings.ToLower(out.Text), strings.ToLower(tc.wantNot)) {
				t.Fatalf("expected text not to contain %q, got %q", tc.wantNot, out.Text)
			}
			if svc.HasActiveSession("u") {
				t.Fatal("expected session to clear after unusable failure path")
			}
		})
	}
}

func TestHandleMessagePermissionDenyThenFinalAnswerReturnsFinalOnly(t *testing.T) {
	acp := &stubACPClient{}
	acp.promptFn = func(_ context.Context, _ backend.PromptRequest) (backend.PromptResult, error) {
		return backend.PromptResult{SessionID: "s", ReplyText: "final answer"}, nil
	}

	svc := NewService(acp)
	out, err := svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u", Text: "hello"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Text != "final answer" {
		t.Fatalf("expected final answer only, got %q", out.Text)
	}
	if strings.Contains(strings.ToLower(out.Text), "deny") || strings.Contains(strings.ToLower(out.Text), "permission") {
		t.Fatalf("should not append denial text when final answer exists, got %q", out.Text)
	}
}

type stubACPClient struct {
	mu        sync.Mutex
	calls     []backend.PromptRequest
	listCalls []struct{}
	promptFn  func(ctx context.Context, req backend.PromptRequest) (backend.PromptResult, error)
	listFn    func(ctx context.Context) (backend.SessionListResult, error)
	createFn  func(ctx context.Context, req backend.SessionCreateRequest) (backend.SessionInfo, error)
	health    backend.HealthSnapshot
}

func (s *stubACPClient) Start(_ context.Context) error { return nil }

func (s *stubACPClient) Stop() error { return nil }

func (s *stubACPClient) ListSessions(ctx context.Context) (backend.SessionListResult, error) {
	s.mu.Lock()
	s.listCalls = append(s.listCalls, struct{}{})
	fn := s.listFn
	s.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return backend.SessionListResult{}, nil
}

func (s *stubACPClient) CreateSession(ctx context.Context, req backend.SessionCreateRequest) (backend.SessionInfo, error) {
	fn := s.createFn
	if fn != nil {
		return fn(ctx, req)
	}
	return backend.SessionInfo{}, nil
}

func (s *stubACPClient) Prompt(ctx context.Context, req backend.PromptRequest) (backend.PromptResult, error) {
	s.mu.Lock()
	s.calls = append(s.calls, req)
	fn := s.promptFn
	s.mu.Unlock()

	if fn == nil {
		return backend.PromptResult{SessionID: "default", ReplyText: "ok"}, nil
	}
	return fn(ctx, req)
}

func (s *stubACPClient) Health() backend.HealthSnapshot {
	return s.health
}

func (s *stubACPClient) PromptCalls() []backend.PromptRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]backend.PromptRequest, len(s.calls))
	copy(out, s.calls)
	return out
}

func callsBySender(calls []backend.PromptRequest, sender string) []backend.PromptRequest {
	var out []backend.PromptRequest
	for _, c := range calls {
		if c.SenderID == sender {
			out = append(out, c)
		}
	}
	return out
}

func TestHandleMessageBusyDoesNotQueueAndSecondCanRunAfterFirstFinishes(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})

	acp := &stubACPClient{}
	acp.promptFn = func(_ context.Context, req backend.PromptRequest) (backend.PromptResult, error) {
		if req.Text == "first" {
			entered <- struct{}{}
			<-release
			return backend.PromptResult{SessionID: "s1", ReplyText: "done"}, nil
		}
		return backend.PromptResult{SessionID: "s2", ReplyText: "done2"}, nil
	}

	svc := NewService(acp)
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u1", Text: "first"})
	}()
	<-entered

	busyOut, _ := svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u2", Text: "second"})
	if !strings.Contains(busyOut.Text, "还在处理中") {
		t.Fatalf("expected busy text, got %q", busyOut.Text)
	}
	if len(acp.PromptCalls()) != 1 {
		t.Fatalf("expected no queued second prompt, got %d calls", len(acp.PromptCalls()))
	}

	close(release)
	<-firstDone
	out2, _ := svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u2", Text: "second"})
	if out2.Text != "done2" {
		t.Fatalf("expected second prompt to run after unlock, got %q", out2.Text)
	}
	if len(acp.PromptCalls()) != 2 {
		t.Fatalf("expected second prompt call after unlock, got %d", len(acp.PromptCalls()))
	}
}
