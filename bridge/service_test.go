package bridge

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiongzhe/weCodex/codexacp"
	"github.com/xiongzhe/weCodex/ilink"
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
	acp.promptFn = func(_ context.Context, req codexacp.PromptRequest) (codexacp.PromptResult, error) {
		switch len(acp.PromptCalls()) {
		case 1:
			if req.SessionID != "" {
				t.Fatalf("first prompt should not carry session, got %q", req.SessionID)
			}
			if req.Timeout != 120*time.Second {
				t.Fatalf("first prompt should use 120s timeout, got %s", req.Timeout)
			}
			return codexacp.PromptResult{SessionID: "s1", ReplyText: "r1"}, nil
		case 2:
			if req.SessionID != "s1" {
				t.Fatalf("second prompt should reuse s1, got %q", req.SessionID)
			}
			return codexacp.PromptResult{SessionID: "s1", ReplyText: "r2"}, nil
		default:
			t.Fatalf("unexpected extra prompt call")
			return codexacp.PromptResult{}, nil
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
	acp.promptFn = func(_ context.Context, req codexacp.PromptRequest) (codexacp.PromptResult, error) {
		switch req.SenderID {
		case "u1":
			if len(callsBySender(acp.PromptCalls(), "u1")) == 1 {
				if req.SessionID != "" {
					t.Fatalf("u1 first should be empty session, got %q", req.SessionID)
				}
				return codexacp.PromptResult{SessionID: "s-u1", ReplyText: "ok"}, nil
			}
			if req.SessionID != "s-u1" {
				t.Fatalf("u1 should reuse s-u1, got %q", req.SessionID)
			}
			return codexacp.PromptResult{SessionID: "s-u1", ReplyText: "ok"}, nil
		case "u2":
			if req.SessionID != "" {
				t.Fatalf("u2 first should be empty session, got %q", req.SessionID)
			}
			return codexacp.PromptResult{SessionID: "s-u2", ReplyText: "ok"}, nil
		default:
			t.Fatalf("unexpected sender %q", req.SenderID)
			return codexacp.PromptResult{}, nil
		}
	}

	svc := NewService(acp)
	_, _ = svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u1", Text: "a"})
	_, _ = svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u2", Text: "b"})
	_, _ = svc.HandleMessage(context.Background(), ilink.InboundMessage{FromUserID: "u1", Text: "c"})
}

func TestHandleMessageReusesInboundAddressingOnPromptReply(t *testing.T) {
	acp := &stubACPClient{}
	acp.promptFn = func(_ context.Context, _ codexacp.PromptRequest) (codexacp.PromptResult, error) {
		return codexacp.PromptResult{SessionID: "s1", ReplyText: "reply"}, nil
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
	acp := &stubACPClient{health: codexacp.HealthSnapshot{State: codexacp.HealthReady, LastErrorSummary: ""}}
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
	if !strings.Contains(status.Text, "acp state: ready") {
		t.Fatalf("status should include health state, got %q", status.Text)
	}
	if !strings.Contains(status.Text, "permission mode: read-only") {
		t.Fatalf("status should include read-only permission mode, got %q", status.Text)
	}

	acp.promptFn = func(_ context.Context, _ codexacp.PromptRequest) (codexacp.PromptResult, error) {
		return codexacp.PromptResult{SessionID: "s1", ReplyText: "ok"}, nil
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

func TestHandleMessageGlobalBusyLockOnNormalPrompts(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})

	acp := &stubACPClient{}
	acp.promptFn = func(_ context.Context, req codexacp.PromptRequest) (codexacp.PromptResult, error) {
		if req.Text == "first" {
			entered <- struct{}{}
			<-release
			return codexacp.PromptResult{SessionID: "s1", ReplyText: "done"}, nil
		}
		return codexacp.PromptResult{SessionID: "s2", ReplyText: "other"}, nil
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
	acp := &stubACPClient{health: codexacp.HealthSnapshot{State: codexacp.HealthDegraded, LastErrorSummary: "x"}}
	acp.promptFn = func(_ context.Context, req codexacp.PromptRequest) (codexacp.PromptResult, error) {
		if req.Text == "hold" {
			entered <- struct{}{}
			<-release
		}
		return codexacp.PromptResult{SessionID: "s", ReplyText: "ok"}, nil
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
	if !strings.Contains(statusOut.Text, "acp state:") {
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
	acp.promptFn = func(_ context.Context, req codexacp.PromptRequest) (codexacp.PromptResult, error) {
		switch req.SenderID {
		case "u1":
			if req.Text == "prep" {
				return codexacp.PromptResult{SessionID: "s-u1", ReplyText: "ok"}, nil
			}
			if req.Text == "hold" {
				entered <- struct{}{}
				<-release
				return codexacp.PromptResult{SessionID: "s-u1", ReplyText: "ok"}, nil
			}
		case "u2":
			return codexacp.PromptResult{SessionID: "s-u2", ReplyText: "ok"}, nil
		}
		return codexacp.PromptResult{SessionID: "s", ReplyText: "ok"}, nil
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
	acp.promptFn = func(_ context.Context, req codexacp.PromptRequest) (codexacp.PromptResult, error) {
		if req.Text == "first" {
			if req.SessionID != "" {
				t.Fatalf("first prompt session should be empty, got %q", req.SessionID)
			}
			return codexacp.PromptResult{SessionID: "s1", ReplyText: "ok"}, nil
		}
		if req.Text == "second" {
			if req.SessionID != "" {
				t.Fatalf("second prompt should start fresh after /new, got %q", req.SessionID)
			}
			return codexacp.PromptResult{SessionID: "s2", ReplyText: "ok"}, nil
		}
		return codexacp.PromptResult{}, nil
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
		{name: "timeout", err: &codexacp.PromptTimeoutError{Err: context.DeadlineExceeded}, wantText: "超时"},
		{name: "session", err: &codexacp.SessionError{Err: errors.New("broken session")}, wantText: "会话"},
		{name: "permission", err: &codexacp.PermissionError{Err: errors.New("tool not allowed")}, wantText: "tool not allowed", wantNot: "超时"},
		{name: "prompt", err: &codexacp.PromptError{Err: errors.New("prompt failed")}, wantText: "失败"},
		{name: "generic", err: errors.New("boom"), wantText: "失败"},
		{name: "empty_reply_success", successText: "   ", wantText: "失败"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			acp := &stubACPClient{}
			call := 0
			acp.promptFn = func(_ context.Context, req codexacp.PromptRequest) (codexacp.PromptResult, error) {
				call++
				if call == 1 {
					return codexacp.PromptResult{SessionID: "s1", ReplyText: "ok"}, nil
				}
				if req.SessionID != "s1" {
					t.Fatalf("expected to reuse s1 before failure, got %q", req.SessionID)
				}
				if tc.err != nil {
					return codexacp.PromptResult{}, tc.err
				}
				return codexacp.PromptResult{SessionID: "s1", ReplyText: tc.successText}, nil
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
	acp.promptFn = func(_ context.Context, _ codexacp.PromptRequest) (codexacp.PromptResult, error) {
		return codexacp.PromptResult{SessionID: "s", ReplyText: "final answer"}, nil
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
	mu       sync.Mutex
	calls    []codexacp.PromptRequest
	promptFn func(ctx context.Context, req codexacp.PromptRequest) (codexacp.PromptResult, error)
	health    codexacp.HealthSnapshot
}

func (s *stubACPClient) Prompt(ctx context.Context, req codexacp.PromptRequest) (codexacp.PromptResult, error) {
	s.mu.Lock()
	s.calls = append(s.calls, req)
	fn := s.promptFn
	s.mu.Unlock()

	if fn == nil {
		return codexacp.PromptResult{SessionID: "default", ReplyText: "ok"}, nil
	}
	return fn(ctx, req)
}

func (s *stubACPClient) Health() codexacp.HealthSnapshot {
	return s.health
}

func (s *stubACPClient) PromptCalls() []codexacp.PromptRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]codexacp.PromptRequest, len(s.calls))
	copy(out, s.calls)
	return out
}

func callsBySender(calls []codexacp.PromptRequest, sender string) []codexacp.PromptRequest {
	var out []codexacp.PromptRequest
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
	acp.promptFn = func(_ context.Context, req codexacp.PromptRequest) (codexacp.PromptResult, error) {
		if req.Text == "first" {
			entered <- struct{}{}
			<-release
			return codexacp.PromptResult{SessionID: "s1", ReplyText: "done"}, nil
		}
		return codexacp.PromptResult{SessionID: "s2", ReplyText: "done2"}, nil
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
