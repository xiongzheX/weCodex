package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/xiongzhe/weCodex/bridge"
	"github.com/xiongzhe/weCodex/codexacp"
	"github.com/xiongzhe/weCodex/config"
	"github.com/xiongzhe/weCodex/ilink"
)

func withStubbedStartDeps(t *testing.T) {
	t.Helper()

	origLoadCfg := startLoadConfig
	origLoadCreds := startLoadCredentials
	origNewACP := startNewACPClient
	origNewBridge := startNewBridgeService
	origNewClientMonitor := startNewILinkClientAndMonitor
	origOut := startOutputWriter
	origLogf := startLogf
	origHome := startUserHomeDir

	t.Cleanup(func() {
		startLoadConfig = origLoadCfg
		startLoadCredentials = origLoadCreds
		startNewACPClient = origNewACP
		startNewBridgeService = origNewBridge
		startNewILinkClientAndMonitor = origNewClientMonitor
		startOutputWriter = origOut
		startLogf = origLogf
		startUserHomeDir = origHome
	})
}

type stubStartACP struct {
	startErr   error
	stopErr    error
	startCalls int
	stopCalls  int

	mu    sync.Mutex
	calls []codexacp.PromptRequest
}

func (s *stubStartACP) Start(_ context.Context) error {
	s.startCalls++
	return s.startErr
}

func (s *stubStartACP) Stop() error {
	s.stopCalls++
	return s.stopErr
}

func (s *stubStartACP) Prompt(_ context.Context, req codexacp.PromptRequest) (codexacp.PromptResult, error) {
	s.mu.Lock()
	s.calls = append(s.calls, req)
	s.mu.Unlock()

	if strings.TrimSpace(req.SessionID) == "" {
		return codexacp.PromptResult{SessionID: "sess-1", ReplyText: "reply"}, nil
	}
	return codexacp.PromptResult{SessionID: req.SessionID, ReplyText: "reply-continued"}, nil
}

func (s *stubStartACP) Health() codexacp.HealthSnapshot {
	return codexacp.HealthSnapshot{State: codexacp.HealthReady}
}

func (s *stubStartACP) PromptCalls() []codexacp.PromptRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]codexacp.PromptRequest, len(s.calls))
	copy(out, s.calls)
	return out
}

type stubStartBridge struct {
	handle func(context.Context, ilink.InboundMessage) (bridge.OutboundReply, error)
}

func (s *stubStartBridge) HandleMessage(ctx context.Context, msg ilink.InboundMessage) (bridge.OutboundReply, error) {
	if s.handle != nil {
		return s.handle(ctx, msg)
	}
	return bridge.OutboundReply{ToUserID: msg.FromUserID, ContextToken: msg.ContextToken, Text: "ok"}, nil
}

type stubStartMonitor struct {
	run func(context.Context, func(ilink.InboundMessage) error) error
}

func (s *stubStartMonitor) Run(ctx context.Context, handle func(ilink.InboundMessage) error) error {
	if s.run != nil {
		return s.run(ctx, handle)
	}
	return nil
}

type stubStartSender struct {
	mu       sync.Mutex
	reqs     []ilink.SendMessageRequest
	responses []error
}

func (s *stubStartSender) SendMessage(_ context.Context, req ilink.SendMessageRequest) (ilink.SendMessageResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, req)
	idx := len(s.reqs) - 1
	if idx < len(s.responses) && s.responses[idx] != nil {
		return ilink.SendMessageResponse{}, s.responses[idx]
	}
	return ilink.SendMessageResponse{Ret: 0}, nil
}

func (s *stubStartSender) Requests() []ilink.SendMessageRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ilink.SendMessageRequest, len(s.reqs))
	copy(out, s.reqs)
	return out
}

func TestRunStartReturnsConfigLoadErrorImmediately(t *testing.T) {
	withStubbedStartDeps(t)

	wantErr := errors.New("config load failed")
	startLoadConfig = func() (config.Config, error) {
		return config.Config{}, wantErr
	}

	acp := &stubStartACP{}
	monitor := &stubStartMonitor{run: func(context.Context, func(ilink.InboundMessage) error) error {
		t.Fatalf("monitor should not run on config failure")
		return nil
	}}
	startNewACPClient = func(codexacp.Config) startACPClient { return acp }
	startNewILinkClientAndMonitor = func(ilink.Credentials, string) (startSender, startMonitorRunner) {
		return &stubStartSender{}, monitor
	}

	err := runStart(context.Background(), &cobra.Command{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected config error %v, got %v", wantErr, err)
	}
	if acp.startCalls != 0 {
		t.Fatalf("expected ACP not to start, got %d starts", acp.startCalls)
	}
}

func TestRunStartReturnsCredentialsLoadErrorImmediately(t *testing.T) {
	withStubbedStartDeps(t)

	wantErr := errors.New("credentials load failed")
	startLoadConfig = func() (config.Config, error) {
		return config.Config{}, nil
	}
	startLoadCredentials = func(config.Config) (ilink.Credentials, error) {
		return ilink.Credentials{}, wantErr
	}

	acp := &stubStartACP{}
	monitor := &stubStartMonitor{run: func(context.Context, func(ilink.InboundMessage) error) error {
		t.Fatalf("monitor should not run on credentials failure")
		return nil
	}}
	startNewACPClient = func(codexacp.Config) startACPClient { return acp }
	startNewILinkClientAndMonitor = func(ilink.Credentials, string) (startSender, startMonitorRunner) {
		return &stubStartSender{}, monitor
	}

	err := runStart(context.Background(), &cobra.Command{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected credentials error %v, got %v", wantErr, err)
	}
	if acp.startCalls != 0 {
		t.Fatalf("expected ACP not to start, got %d starts", acp.startCalls)
	}
}

func TestRunStartReturnsACPStartErrorAndDoesNotStartMonitorTraffic(t *testing.T) {
	withStubbedStartDeps(t)

	startLoadConfig = func() (config.Config, error) {
		return config.Config{CodexCommand: "codex", CodexArgs: []string{"acp"}, WorkingDirectory: "/tmp", PermissionMode: "readonly"}, nil
	}
	startLoadCredentials = func(config.Config) (ilink.Credentials, error) {
		return ilink.Credentials{}, nil
	}

	wantErr := errors.New("acp start failed")
	acp := &stubStartACP{startErr: wantErr}
	monitorRuns := 0
	clientMonitorConstructCalls := 0
	startNewACPClient = func(codexacp.Config) startACPClient { return acp }
	startNewILinkClientAndMonitor = func(ilink.Credentials, string) (startSender, startMonitorRunner) {
		clientMonitorConstructCalls++
		return &stubStartSender{}, &stubStartMonitor{run: func(context.Context, func(ilink.InboundMessage) error) error {
			monitorRuns++
			return nil
		}}
	}

	err := runStart(context.Background(), &cobra.Command{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected ACP start error %v, got %v", wantErr, err)
	}
	if monitorRuns != 0 {
		t.Fatalf("expected monitor not to run when ACP start fails, got %d runs", monitorRuns)
	}
	if clientMonitorConstructCalls != 0 {
		t.Fatalf("expected ilink client/monitor not to be constructed when ACP start fails, got %d constructions", clientMonitorConstructCalls)
	}
}

func TestRunStartPrintsForegroundNoticeAndProcessesMonitorMessages(t *testing.T) {
	withStubbedStartDeps(t)

	startLoadConfig = func() (config.Config, error) {
		return config.Config{CodexCommand: "codex", CodexArgs: []string{"acp"}, WorkingDirectory: "/tmp", PermissionMode: "readonly"}, nil
	}
	startLoadCredentials = func(config.Config) (ilink.Credentials, error) {
		return ilink.Credentials{}, nil
	}
	startUserHomeDir = func() (string, error) { return "/home/tester", nil }

	acp := &stubStartACP{}
	sender := &stubStartSender{}
	monitor := &stubStartMonitor{run: func(ctx context.Context, handle func(ilink.InboundMessage) error) error {
		if err := handle(ilink.InboundMessage{FromUserID: "u-1", ContextToken: "ctx-1", Text: "hello"}); err != nil {
			return err
		}
		return nil
	}}
	bridgeSvc := &stubStartBridge{handle: func(_ context.Context, msg ilink.InboundMessage) (bridge.OutboundReply, error) {
		return bridge.OutboundReply{ToUserID: msg.FromUserID, ContextToken: msg.ContextToken, Text: "reply-1"}, nil
	}}

	startNewACPClient = func(codexacp.Config) startACPClient { return acp }
	startNewILinkClientAndMonitor = func(ilink.Credentials, string) (startSender, startMonitorRunner) { return sender, monitor }
	startNewBridgeService = func(bridge.ACPClient) startBridgeService { return bridgeSvc }

	var out bytes.Buffer
	startOutputWriter = func(*cobra.Command) io.Writer { return &out }

	err := runStart(context.Background(), &cobra.Command{})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if got, want := out.String(), startForegroundNotice+"\n"; got != want {
		t.Fatalf("unexpected foreground notice output:\nwant: %q\ngot:  %q", want, got)
	}

	reqs := sender.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected one outbound send, got %d", len(reqs))
	}
	if reqs[0].ToUserID != "u-1" || reqs[0].ContextToken != "ctx-1" || reqs[0].Text != "reply-1" {
		t.Fatalf("unexpected send request: %+v", reqs[0])
	}
	if acp.stopCalls != 1 {
		t.Fatalf("expected ACP stop once, got %d", acp.stopCalls)
	}
}

func TestRunStartSendFailureLogsWarningWithoutRetryAndKeepsSessionState(t *testing.T) {
	withStubbedStartDeps(t)

	startLoadConfig = func() (config.Config, error) {
		return config.Config{CodexCommand: "codex", CodexArgs: []string{"acp"}, WorkingDirectory: "/tmp", PermissionMode: "readonly"}, nil
	}
	startLoadCredentials = func(config.Config) (ilink.Credentials, error) {
		return ilink.Credentials{}, nil
	}
	startUserHomeDir = func() (string, error) { return "/home/tester", nil }

	acp := &stubStartACP{}
	bridgeSvc := bridge.NewService(acp)
	sender := &stubStartSender{responses: []error{errors.New("send failed"), nil}}
	monitor := &stubStartMonitor{run: func(ctx context.Context, handle func(ilink.InboundMessage) error) error {
		if err := handle(ilink.InboundMessage{FromUserID: "u", Text: "first", ContextToken: "c1"}); err != nil {
			return err
		}
		if err := handle(ilink.InboundMessage{FromUserID: "u", Text: "second", ContextToken: "c2"}); err != nil {
			return err
		}
		return nil
	}}

	startNewACPClient = func(codexacp.Config) startACPClient { return acp }
	startNewBridgeService = func(bridge.ACPClient) startBridgeService { return bridgeSvc }
	startNewILinkClientAndMonitor = func(ilink.Credentials, string) (startSender, startMonitorRunner) { return sender, monitor }

	var logs bytes.Buffer
	startLogf = func(format string, args ...any) {
		logs.WriteString("warning")
	}

	err := runStart(context.Background(), &cobra.Command{})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	reqs := sender.Requests()
	if len(reqs) != 2 {
		t.Fatalf("expected exactly two send attempts with no retry, got %d", len(reqs))
	}
	if !strings.Contains(logs.String(), "warning") {
		t.Fatalf("expected warning log for send failure")
	}

	calls := acp.PromptCalls()
	if len(calls) != 2 {
		t.Fatalf("expected two prompt calls, got %d", len(calls))
	}
	if strings.TrimSpace(calls[0].SessionID) != "" {
		t.Fatalf("expected first prompt to start without session, got %q", calls[0].SessionID)
	}
	if calls[1].SessionID != "sess-1" {
		t.Fatalf("expected second prompt to keep existing session after send failure, got %q", calls[1].SessionID)
	}
}

func TestRunStartReturnsNilOnContextCanceledEvenWhenACPStopFails(t *testing.T) {
	withStubbedStartDeps(t)

	startLoadConfig = func() (config.Config, error) {
		return config.Config{CodexCommand: "codex", CodexArgs: []string{"acp"}, WorkingDirectory: "/tmp", PermissionMode: "readonly"}, nil
	}
	startLoadCredentials = func(config.Config) (ilink.Credentials, error) {
		return ilink.Credentials{}, nil
	}
	startUserHomeDir = func() (string, error) { return "/home/tester", nil }

	acp := &stubStartACP{stopErr: errors.New("stop failed")}
	startNewACPClient = func(codexacp.Config) startACPClient { return acp }
	startNewBridgeService = func(acp bridge.ACPClient) startBridgeService { return &stubStartBridge{} }
	startNewILinkClientAndMonitor = func(ilink.Credentials, string) (startSender, startMonitorRunner) {
		return &stubStartSender{}, &stubStartMonitor{run: func(context.Context, func(ilink.InboundMessage) error) error {
			return context.Canceled
		}}
	}

	err := runStart(context.Background(), &cobra.Command{})
	if err != nil {
		t.Fatalf("expected nil on context cancellation, got %v", err)
	}
	if acp.stopCalls != 1 {
		t.Fatalf("expected ACP stop once on cancellation, got %d", acp.stopCalls)
	}
}

func TestRunStartRegistersCommandOnRoot(t *testing.T) {
	root := newRootCmd()

	found := false
	for _, sub := range root.Commands() {
		if sub.Name() == "start" {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, 0, len(root.Commands()))
		for _, sub := range root.Commands() {
			names = append(names, sub.Name())
		}
		t.Fatalf("expected start command to be registered, got commands: %s", strings.Join(names, ", "))
	}
}
