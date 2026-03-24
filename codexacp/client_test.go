package codexacp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStartPerformsInitializeHandshake(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)

	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if !rt.started {
		t.Fatal("expected runtime start to be called")
	}
	if rt.callCount("initialize") != 1 {
		t.Fatalf("expected one initialize call, got %d", rt.callCount("initialize"))
	}
}

func TestStartUsesConfiguredWorkingDirectoryAndReadonlyMode(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)

	client := newClientWithRuntimeForTest(Config{
		Command:          "codex",
		Args:             []string{"run"},
		WorkingDirectory: "/tmp/project",
		PermissionMode:   "danger-full-access",
	}, rt)

	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if rt.startCfg.WorkingDirectory != "/tmp/project" {
		t.Fatalf("expected working dir propagated, got %q", rt.startCfg.WorkingDirectory)
	}
	if rt.startCfg.PermissionMode != "readonly" {
		t.Fatalf("expected readonly mode, got %q", rt.startCfg.PermissionMode)
	}
}

func TestStartStopsRuntimeWhenInitializeHandshakeFails(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", nil, errors.New("initialize failed"))

	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)

	err := client.Start(context.Background())
	if err == nil {
		t.Fatal("expected startup error")
	}
	var startupErr *StartupError
	if !errors.As(err, &startupErr) {
		t.Fatalf("expected StartupError, got %T (%v)", err, err)
	}
	if rt.callCount("initialize") != 1 {
		t.Fatalf("expected initialize handshake attempt, got %d", rt.callCount("initialize"))
	}
	if rt.stopCalls != 1 {
		t.Fatalf("expected failed Start to stop runtime once, got %d", rt.stopCalls)
	}
	if stopErr := client.Stop(); stopErr != nil {
		t.Fatalf("expected cleaned-up failed Start to leave Stop as no-op, got %v", stopErr)
	}
	if rt.stopCalls != 1 {
		t.Fatalf("expected no extra stop after failed Start cleanup, got %d stops", rt.stopCalls)
	}
}

func TestPromptCreatesNewSessionWhenSessionIDMissing(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.queueResult("session/new", SessionNewResult{SessionID: "s-new"}, nil)
	rt.onCall("session/prompt", func() {
		rt.emitUpdate("s-new", "hello")
	})
	rt.queueResult("session/prompt", SessionPromptResult{Completed: true}, nil)

	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)
	mustStart(t, client)

	res, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u1", Text: "hi"})
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if res.SessionID != "s-new" {
		t.Fatalf("expected new session id, got %q", res.SessionID)
	}
	if res.ReplyText != "hello" {
		t.Fatalf("expected assembled reply, got %q", res.ReplyText)
	}
}

func TestPromptReusesProvidedSessionID(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.onCall("session/prompt", func() {
		rt.emitUpdate("existing", "ok")
	})
	rt.queueResult("session/prompt", SessionPromptResult{Completed: true}, nil)

	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)
	mustStart(t, client)

	res, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u1", SessionID: "existing", Text: "hi"})
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if rt.callCount("session/new") != 0 {
		t.Fatalf("expected no session/new call, got %d", rt.callCount("session/new"))
	}
	if res.SessionID != "existing" {
		t.Fatalf("expected session reuse, got %q", res.SessionID)
	}
}

func TestPromptTimeoutSuppressesLateUpdates(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)

	gate := make(chan struct{})
	rt.queueResult("session/new", SessionNewResult{SessionID: "s-timeout"}, nil)
	rt.queueBlockedResult("session/prompt", SessionPromptResult{Completed: true}, nil, gate)

	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)
	mustStart(t, client)

	_, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u1", Text: "first", Timeout: 20 * time.Millisecond})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	var timeoutErr *PromptTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("expected PromptTimeoutError, got %T (%v)", err, err)
	}

	rt.emitUpdate("s-timeout", "LATE")
	close(gate)

	rt.onCall("session/prompt", func() {
		rt.emitUpdate("s-timeout", "fresh")
	})
	rt.queueResult("session/prompt", SessionPromptResult{Completed: true}, nil)

	res, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u1", SessionID: "s-timeout", Text: "second"})
	if err != nil {
		t.Fatalf("second prompt: %v", err)
	}
	if res.ReplyText != "fresh" {
		t.Fatalf("expected only fresh text, got %q", res.ReplyText)
	}
}

func TestPromptIgnoresUpdatesFromOtherSessions(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.queueResult("session/new", SessionNewResult{SessionID: "target"}, nil)
	rt.onCall("session/prompt", func() {
		rt.emitUpdate("other", "skip")
		rt.emitUpdate("target", "keep")
	})
	rt.queueResult("session/prompt", SessionPromptResult{Completed: true}, nil)

	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)
	mustStart(t, client)

	res, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u", Text: "t"})
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if res.ReplyText != "keep" {
		t.Fatalf("expected filtered update, got %q", res.ReplyText)
	}
}

func TestPromptReturnsErrorWhenNoTextIsAssembled(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.queueResult("session/new", SessionNewResult{SessionID: "s"}, nil)
	rt.queueResult("session/prompt", SessionPromptResult{Completed: true}, nil)

	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)
	mustStart(t, client)

	_, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u", Text: "t"})
	if err == nil {
		t.Fatal("expected empty reply error")
	}
	var promptErr *PromptError
	if !errors.As(err, &promptErr) {
		t.Fatalf("expected PromptError, got %T (%v)", err, err)
	}
}

func TestPromptDrainsBufferedUpdatesAfterPromptReturns(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.queueResult("session/new", SessionNewResult{SessionID: "s"}, nil)
	rt.onCall("session/prompt", func() {
		rt.emitUpdate("s", "tail")
	})
	rt.queueResult("session/prompt", SessionPromptResult{Completed: true}, nil)

	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)
	mustStart(t, client)

	res, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u", Text: "t"})
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if res.ReplyText != "tail" {
		t.Fatalf("expected drained buffered tail, got %q", res.ReplyText)
	}
}

func TestPromptTruncatesLongReplies(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.queueResult("session/new", SessionNewResult{SessionID: "s"}, nil)
	long := strings.Repeat("你", 4100)
	rt.onCall("session/prompt", func() {
		rt.emitUpdate("s", long)
	})
	rt.queueResult("session/prompt", SessionPromptResult{Completed: true}, nil)

	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)
	mustStart(t, client)

	res, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u", Text: "t"})
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if runeCount(res.ReplyText) != 4000 {
		t.Fatalf("expected 4000 runes, got %d", runeCount(res.ReplyText))
	}
	if !strings.HasSuffix(res.ReplyText, "\n\n[回复已截断]") {
		t.Fatalf("expected truncation suffix, got %q", res.ReplyText[len(res.ReplyText)-24:])
	}
}

func TestHealthStartsReadyWithoutErrorSummary(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)
	mustStart(t, client)

	h := client.Health()
	if h.State != HealthReady {
		t.Fatalf("expected ready, got %q", h.State)
	}
	if h.LastErrorSummary != "" {
		t.Fatalf("expected empty summary, got %q", h.LastErrorSummary)
	}
}

func TestPromptReturnsTypedPromptError(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.queueResult("session/new", SessionNewResult{SessionID: "s"}, nil)
	rt.queueResult("session/prompt", nil, errors.New("prompt rpc failed"))

	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)
	mustStart(t, client)

	_, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u", Text: "t"})
	if err == nil {
		t.Fatal("expected prompt error")
	}
	var promptErr *PromptError
	if !errors.As(err, &promptErr) {
		t.Fatalf("expected PromptError, got %T (%v)", err, err)
	}
}

func TestHealthReportsDegradedAfterRecoverablePromptError(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.queueResult("session/new", SessionNewResult{SessionID: "s"}, nil)
	rt.queueResult("session/prompt", nil, errors.New("prompt rpc failed"))
	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)
	mustStart(t, client)

	_, _ = client.Prompt(context.Background(), PromptRequest{SenderID: "u", Text: "t"})

	h := client.Health()
	if h.State != HealthDegraded {
		t.Fatalf("expected degraded, got %q", h.State)
	}
	if strings.TrimSpace(h.LastErrorSummary) == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestPromptReturnsTypedSessionError(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.queueResult("session/new", nil, errors.New("session create failed"))
	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)
	mustStart(t, client)

	_, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u", Text: "t"})
	if err == nil {
		t.Fatal("expected session error")
	}
	var sessionErr *SessionError
	if !errors.As(err, &sessionErr) {
		t.Fatalf("expected SessionError, got %T (%v)", err, err)
	}
}

func TestHealthReportsDegradedAfterRecoverableSessionError(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.queueResult("session/new", nil, errors.New("session create failed"))
	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)
	mustStart(t, client)

	_, _ = client.Prompt(context.Background(), PromptRequest{SenderID: "u", Text: "t"})

	h := client.Health()
	if h.State != HealthDegraded {
		t.Fatalf("expected degraded, got %q", h.State)
	}
	if strings.TrimSpace(h.LastErrorSummary) == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestHealthReportsDegradedAfterPromptTimeout(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.queueResult("session/new", SessionNewResult{SessionID: "s"}, nil)
	gate := make(chan struct{})
	rt.queueBlockedResult("session/prompt", SessionPromptResult{Completed: true}, nil, gate)
	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)
	mustStart(t, client)

	_, _ = client.Prompt(context.Background(), PromptRequest{SenderID: "u", Text: "t", Timeout: 15 * time.Millisecond})
	close(gate)

	h := client.Health()
	if h.State != HealthDegraded {
		t.Fatalf("expected degraded, got %q", h.State)
	}
	if strings.TrimSpace(h.LastErrorSummary) == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestPromptDeniesPermissionAndContinuesWhenACPRecovers(t *testing.T) {
	rt := newStubRuntime()
	wd := t.TempDir()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.queueResult("session/new", SessionNewResult{SessionID: "s"}, nil)
	rt.onCall("session/prompt", func() {
		rt.emitPermissionRequest("s", `{"name":"write_file","arguments":{"path":"x.txt"}}`)
		rt.emitUpdate("s", "final")
	})
	rt.queueResult("session/respond_permission", nil, nil)
	rt.queueResult("session/prompt", SessionPromptResult{Completed: true}, nil)
	client := newClientWithRuntimeForTest(Config{WorkingDirectory: wd}, rt)
	mustStart(t, client)

	res, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u", Text: "t"})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if res.ReplyText != "final" {
		t.Fatalf("expected final reply, got %q", res.ReplyText)
	}
	if rt.callCount("session/respond_permission") == 0 {
		t.Fatal("expected permission response call")
	}
}

func TestPromptReturnsPermissionErrorAfterDeniedPermissionFailure(t *testing.T) {
	rt := newStubRuntime()
	wd := t.TempDir()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.queueResult("session/new", SessionNewResult{SessionID: "s"}, nil)
	rt.onCall("session/prompt", func() {
		rt.emitPermissionRequest("s", `{"name":"write_file","arguments":{"path":"x.txt"}}`)
	})
	rt.queueResult("session/respond_permission", nil, nil)
	rt.queueResult("session/prompt", nil, errors.New("prompt failed"))
	client := newClientWithRuntimeForTest(Config{WorkingDirectory: wd}, rt)
	mustStart(t, client)

	_, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u", Text: "t"})
	if err == nil {
		t.Fatal("expected permission error")
	}
	var permErr *PermissionError
	if !errors.As(err, &permErr) {
		t.Fatalf("expected PermissionError, got %T (%v)", err, err)
	}
	if !strings.Contains(strings.ToLower(permErr.Error()), "not allowed") && !strings.Contains(strings.ToLower(permErr.Error()), "deny") {
		t.Fatalf("expected denial reason in error, got %q", permErr.Error())
	}
}

func TestHealthReportsDegradedAfterPermissionNormalizationFailure(t *testing.T) {
	rt := newStubRuntime()
	wd := t.TempDir()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.queueResult("session/new", SessionNewResult{SessionID: "s"}, nil)
	rt.onCall("session/prompt", func() {
		rt.emitRawNotification("session/request_permission", `{"sessionId":"s","toolCall":"{not-json}"}`)
	})
	rt.queueResult("session/respond_permission", nil, nil)
	rt.queueResult("session/prompt", nil, errors.New("prompt failed after permission denial"))

	client := newClientWithRuntimeForTest(Config{WorkingDirectory: wd}, rt)
	mustStart(t, client)

	_, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u", Text: "t"})
	if err == nil {
		t.Fatal("expected permission error")
	}
	var permErr *PermissionError
	if !errors.As(err, &permErr) {
		t.Fatalf("expected PermissionError, got %T (%v)", err, err)
	}
	if rt.callCount("session/respond_permission") != 1 {
		t.Fatalf("expected immediate deny response, got %d", rt.callCount("session/respond_permission"))
	}
	if call := rt.lastCall("session/respond_permission"); call == nil {
		t.Fatal("expected respond_permission call payload")
	} else {
		params, ok := call.params.(SessionRespondPermissionParams)
		if !ok {
			t.Fatalf("unexpected params type: %T", call.params)
		}
		if params.Allowed {
			t.Fatal("expected permission denial")
		}
		if strings.TrimSpace(params.Reason) == "" {
			t.Fatal("expected denial reason")
		}
	}

	h := client.Health()
	if h.State != HealthDegraded {
		t.Fatalf("expected degraded, got %q", h.State)
	}
	if strings.TrimSpace(h.LastErrorSummary) == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestPromptReturnsPermissionErrorWhenNormalizationFailsEvenIfPromptSucceeds(t *testing.T) {
	rt := newStubRuntime()
	wd := t.TempDir()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.queueResult("session/new", SessionNewResult{SessionID: "s"}, nil)
	rt.onCall("session/prompt", func() {
		rt.emitRawNotification("session/request_permission", `{"sessionId":"s","toolCall":"{not-json}"}`)
		rt.emitUpdate("s", "should not be returned")
	})
	rt.queueResult("session/respond_permission", nil, nil)
	rt.queueResult("session/prompt", SessionPromptResult{Completed: true}, nil)

	client := newClientWithRuntimeForTest(Config{WorkingDirectory: wd}, rt)
	mustStart(t, client)

	_, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u", Text: "t"})
	if err == nil {
		t.Fatal("expected permission error")
	}
	var permErr *PermissionError
	if !errors.As(err, &permErr) {
		t.Fatalf("expected PermissionError, got %T (%v)", err, err)
	}
	if rt.callCount("session/respond_permission") != 1 {
		t.Fatalf("expected immediate deny response, got %d", rt.callCount("session/respond_permission"))
	}

	h := client.Health()
	if h.State != HealthDegraded {
		t.Fatalf("expected degraded, got %q", h.State)
	}
	if strings.TrimSpace(h.LastErrorSummary) == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestStartUsesRealDefaultRuntimeInsteadOfNoop(t *testing.T) {
	client := NewClient(Config{Command: "/definitely/missing/codex-binary", WorkingDirectory: t.TempDir()})

	err := client.Start(context.Background())
	if err == nil {
		t.Fatal("expected startup error")
	}
	var startupErr *StartupError
	if !errors.As(err, &startupErr) {
		t.Fatalf("expected StartupError, got %T (%v)", err, err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "runtime not configured") {
		t.Fatalf("expected real subprocess startup failure, got noop failure: %v", err)
	}
}

func TestStartFailureMarksHealthUnavailable(t *testing.T) {
	rt := newStubRuntime()
	rt.startErr = errors.New("spawn failed")
	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)

	err := client.Start(context.Background())
	if err == nil {
		t.Fatal("expected startup error")
	}
	var startupErr *StartupError
	if !errors.As(err, &startupErr) {
		t.Fatalf("expected StartupError, got %T (%v)", err, err)
	}
	h := client.Health()
	if h.State != HealthUnavailable {
		t.Fatalf("expected unavailable, got %q", h.State)
	}
	if strings.TrimSpace(h.LastErrorSummary) == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestPromptReturnsTypedTransportErrorAfterIOFailure(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.queueResult("session/new", SessionNewResult{SessionID: "s"}, nil)
	rt.queueBlockedResult("session/prompt", SessionPromptResult{Completed: true}, nil, make(chan struct{}))
	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)
	mustStart(t, client)

	rt.emitRuntimeError(errors.New("stdout closed"))
	_, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u", Text: "t", Timeout: 200 * time.Millisecond})
	if err == nil {
		t.Fatal("expected transport error")
	}
	var transportErr *TransportError
	if !errors.As(err, &transportErr) {
		t.Fatalf("expected TransportError, got %T (%v)", err, err)
	}
}

func TestStopMarksHealthUnavailableWithSummary(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)
	mustStart(t, client)

	if err := client.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	h := client.Health()
	if h.State != HealthUnavailable {
		t.Fatalf("expected unavailable, got %q", h.State)
	}
	if strings.TrimSpace(h.LastErrorSummary) == "" {
		t.Fatal("expected non-empty last error summary after stop")
	}
}

func TestFailPendingDoesNotBlockWhenResponseChannelIsFull(t *testing.T) {
	rt := newStdioRuntime()
	respCh := make(chan RPCResponseEnvelope, 1)
	respCh <- RPCResponseEnvelope{ID: "1"}
	rt.pending["1"] = respCh

	done := make(chan struct{})
	go func() {
		rt.failPending(errors.New("process exited"))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("failPending blocked on full response channel")
	}
}

func TestPromptSupportsNestedSessionUpdatePayload(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.queueResult("session/new", SessionNewResult{SessionID: "s"}, nil)
	rt.onCall("session/prompt", func() {
		rt.emitRawNotification("session/update", `{"session":{"id":"s"},"update":{"text":"nested"}}`)
	})
	rt.queueResult("session/prompt", SessionPromptResult{Completed: true}, nil)

	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)
	mustStart(t, client)

	res, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u", Text: "t"})
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if res.ReplyText != "nested" {
		t.Fatalf("expected nested update text, got %q", res.ReplyText)
	}
}

func TestPromptSupportsMixedSessionAndNestedUpdatePayload(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.queueResult("session/new", SessionNewResult{SessionID: "s"}, nil)
	rt.onCall("session/prompt", func() {
		rt.emitRawNotification("session/update", `{"sessionId":"s","update":{"text":"hello"}}`)
	})
	rt.queueResult("session/prompt", SessionPromptResult{Completed: true}, nil)

	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)
	mustStart(t, client)

	res, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u", Text: "t"})
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if res.ReplyText != "hello" {
		t.Fatalf("expected mixed payload update text, got %q", res.ReplyText)
	}
}

func TestPromptSupportsNestedSessionPermissionPayload(t *testing.T) {
	rt := newStubRuntime()
	wd := t.TempDir()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.queueResult("session/new", SessionNewResult{SessionID: "s"}, nil)
	rt.onCall("session/prompt", func() {
		rt.emitRawNotification("session/request_permission", `{"session":{"id":"s"},"request":{"toolCall":{"name":"write_file","arguments":{"path":"x.txt"}}}}`)
	})
	rt.queueResult("session/respond_permission", nil, nil)
	rt.queueResult("session/prompt", nil, errors.New("prompt failed after deny"))

	client := newClientWithRuntimeForTest(Config{WorkingDirectory: wd}, rt)
	mustStart(t, client)

	_, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u", Text: "t"})
	if err == nil {
		t.Fatal("expected permission error")
	}
	var permErr *PermissionError
	if !errors.As(err, &permErr) {
		t.Fatalf("expected PermissionError, got %T (%v)", err, err)
	}
	if rt.callCount("session/respond_permission") != 1 {
		t.Fatalf("expected one permission response, got %d", rt.callCount("session/respond_permission"))
	}
}

func TestPromptSupportsMixedSessionAndNestedPermissionPayload(t *testing.T) {
	rt := newStubRuntime()
	wd := t.TempDir()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	rt.queueResult("session/new", SessionNewResult{SessionID: "s"}, nil)
	rt.onCall("session/prompt", func() {
		rt.emitRawNotification("session/request_permission", `{"sessionId":"s","request":{"toolCall":{"name":"write_file","arguments":{"path":"x.txt"}}}}`)
	})
	rt.queueResult("session/respond_permission", nil, nil)
	rt.queueResult("session/prompt", nil, errors.New("prompt failed after deny"))

	client := newClientWithRuntimeForTest(Config{WorkingDirectory: wd}, rt)
	mustStart(t, client)

	_, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u", Text: "t"})
	if err == nil {
		t.Fatal("expected permission error")
	}
	var permErr *PermissionError
	if !errors.As(err, &permErr) {
		t.Fatalf("expected PermissionError, got %T (%v)", err, err)
	}
	if rt.callCount("session/respond_permission") != 1 {
		t.Fatalf("expected one permission response, got %d", rt.callCount("session/respond_permission"))
	}
	if call := rt.lastCall("session/respond_permission"); call == nil {
		t.Fatal("expected respond_permission call payload")
	} else {
		params, ok := call.params.(SessionRespondPermissionParams)
		if !ok {
			t.Fatalf("unexpected params type: %T", call.params)
		}
		if !strings.Contains(strings.ToLower(params.Reason), "tool not allowed") {
			t.Fatalf("expected parsed tool denial reason, got %q", params.Reason)
		}
	}
}

func TestSubprocessFailureAfterStartMarksHealthUnavailable(t *testing.T) {
	rt := newStubRuntime()
	rt.queueResult("initialize", InitializeResult{Server: "ok"}, nil)
	client := newClientWithRuntimeForTest(Config{WorkingDirectory: t.TempDir()}, rt)
	mustStart(t, client)

	rt.emitRuntimeError(errors.New("process exited"))
	_, _ = client.Prompt(context.Background(), PromptRequest{SenderID: "u", SessionID: "s", Text: "t"})

	h := client.Health()
	if h.State != HealthUnavailable {
		t.Fatalf("expected unavailable, got %q", h.State)
	}
	if strings.TrimSpace(h.LastErrorSummary) == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestPromptReturnsNotStartedErrorBeforeStart(t *testing.T) {
	client := NewClient(Config{WorkingDirectory: t.TempDir()})
	_, err := client.Prompt(context.Background(), PromptRequest{SenderID: "u", Text: "t"})
	if err == nil {
		t.Fatal("expected not started error")
	}
	var ns *NotStartedError
	if !errors.As(err, &ns) {
		t.Fatalf("expected NotStartedError, got %T (%v)", err, err)
	}
}

func mustStart(t *testing.T, c *Client) {
	t.Helper()
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
}

func runeCount(s string) int {
	return len([]rune(s))
}

type stubRuntime struct {
	mu          sync.Mutex
	started     bool
	startCfg    Config
	startErr    error
	stopErr     error
	stopCalls   int
	calls       []stubCall
	callHooks   map[string]func()
	responses   map[string][]stubResponse
	notifs      chan runtimeNotification
	errCh       chan error
}

type stubCall struct {
	method string
	params any
}

type stubResponse struct {
	result any
	err    error
	gate   <-chan struct{}
}

func newStubRuntime() *stubRuntime {
	return &stubRuntime{
		callHooks: make(map[string]func()),
		responses: make(map[string][]stubResponse),
		notifs:    make(chan runtimeNotification, 128),
		errCh:     make(chan error, 16),
	}
}

func (s *stubRuntime) Start(_ context.Context, cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = true
	s.startCfg = cfg
	return s.startErr
}

func (s *stubRuntime) Stop() error {
	s.mu.Lock()
	s.stopCalls++
	s.started = false
	s.mu.Unlock()
	return s.stopErr
}

func (s *stubRuntime) Notifications() <-chan runtimeNotification {
	return s.notifs
}

func (s *stubRuntime) Errors() <-chan error {
	return s.errCh
}

func (s *stubRuntime) Call(_ context.Context, method string, params any, result any) error {
	s.mu.Lock()
	s.calls = append(s.calls, stubCall{method: method, params: params})
	hook := s.callHooks[method]
	responses := s.responses[method]
	if len(responses) == 0 {
		s.mu.Unlock()
		return fmt.Errorf("no scripted response for method %s", method)
	}
	resp := responses[0]
	s.responses[method] = responses[1:]
	s.mu.Unlock()

	if hook != nil {
		hook()
	}
	if resp.gate != nil {
		<-resp.gate
	}
	if resp.err != nil {
		return resp.err
	}
	if result == nil || resp.result == nil {
		return nil
	}
	buf, err := json.Marshal(resp.result)
	if err != nil {
		return err
	}
	return json.Unmarshal(buf, result)
}

func (s *stubRuntime) queueResult(method string, result any, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses[method] = append(s.responses[method], stubResponse{result: result, err: err})
}

func (s *stubRuntime) queueBlockedResult(method string, result any, err error, gate <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses[method] = append(s.responses[method], stubResponse{result: result, err: err, gate: gate})
}

func (s *stubRuntime) onCall(method string, fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callHooks[method] = fn
}

func (s *stubRuntime) callCount(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.calls {
		if c.method == method {
			n++
		}
	}
	return n
}

func (s *stubRuntime) lastCall(method string) *stubCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.calls) - 1; i >= 0; i-- {
		if s.calls[i].method == method {
			cp := s.calls[i]
			return &cp
		}
	}
	return nil
}

func (s *stubRuntime) emitUpdate(sessionID, text string) {
	params := SessionUpdateParams{SessionID: sessionID, Text: text}
	s.emitNotification("session/update", params)
}

func (s *stubRuntime) emitPermissionRequest(sessionID, rawToolCall string) {
	params := SessionRequestPermissionParams{SessionID: sessionID, ToolCall: json.RawMessage(rawToolCall)}
	s.emitNotification("session/request_permission", params)
}

func (s *stubRuntime) emitNotification(method string, params any) {
	payload, _ := json.Marshal(params)
	s.notifs <- runtimeNotification{Method: method, Params: payload}
}

func (s *stubRuntime) emitRawNotification(method, raw string) {
	s.notifs <- runtimeNotification{Method: method, Params: json.RawMessage(raw)}
}

func (s *stubRuntime) emitRuntimeError(err error) {
	s.errCh <- err
}

func newClientWithRuntimeForTest(cfg Config, rt *stubRuntime) *Client {
	c := NewClient(cfg)
	c.runtimeFactory = func() runtimeClient { return rt }
	return c
}
