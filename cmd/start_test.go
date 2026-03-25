package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/xiongzhe/weCodex/backend"
	"github.com/xiongzhe/weCodex/bridge"
	"github.com/xiongzhe/weCodex/codexacp"
	"github.com/xiongzhe/weCodex/config"
	"github.com/xiongzhe/weCodex/ilink"
)

func withStubbedStartDeps(t *testing.T) {
	t.Helper()

	origLoadCfg := startLoadConfig
	origLoadRuntimeCfg := startLoadRuntimeConfig
	origLoadCreds := startLoadCredentials
	origNewACP := startNewACPClient
	origNewCLI := startNewCLIClient
	origNewBridge := startNewBridgeService
	origNewClientMonitor := startNewILinkClientAndMonitor
	origOut := startOutputWriter
	origLogf := startLogf
	origHome := startUserHomeDir

	startLoadRuntimeConfig = func(io.Writer) (config.Config, error) {
		return startLoadConfig()
	}

	t.Cleanup(func() {
		startLoadConfig = origLoadCfg
		startLoadRuntimeConfig = origLoadRuntimeCfg
		startLoadCredentials = origLoadCreds
		startNewACPClient = origNewACP
		startNewCLIClient = origNewCLI
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
	calls []backend.PromptRequest
}

func (s *stubStartACP) Start(_ context.Context) error {
	s.startCalls++
	return s.startErr
}

func (s *stubStartACP) Stop() error {
	s.stopCalls++
	return s.stopErr
}

func (s *stubStartACP) Prompt(_ context.Context, req backend.PromptRequest) (backend.PromptResult, error) {
	s.mu.Lock()
	s.calls = append(s.calls, req)
	s.mu.Unlock()

	if strings.TrimSpace(req.SessionID) == "" {
		return backend.PromptResult{SessionID: "sess-1", ReplyText: "reply"}, nil
	}
	return backend.PromptResult{SessionID: req.SessionID, ReplyText: "reply-continued"}, nil
}

func (s *stubStartACP) Health() backend.HealthSnapshot {
	return backend.HealthSnapshot{State: backend.HealthReady}
}

func (s *stubStartACP) PromptCalls() []backend.PromptRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]backend.PromptRequest, len(s.calls))
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
	startNewACPClient = func(codexacp.Config) startBackendClient { return acp }
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

func TestRunStartBootstrapsMissingConfigAndUsesCLIBackend(t *testing.T) {
	withStubbedStartDeps(t)

	startLoadRuntimeConfig = func(io.Writer) (config.Config, error) {
		return config.Config{BackendType: "cli", CodexCommand: "codex", WorkingDirectory: "/tmp", PermissionMode: "readonly"}, nil
	}

	loadedCredsCfgs := make([]config.Config, 0, 1)
	startLoadCredentials = func(cfg config.Config) (ilink.Credentials, error) {
		loadedCredsCfgs = append(loadedCredsCfgs, cfg)
		return ilink.Credentials{}, nil
	}
	startUserHomeDir = func() (string, error) { return "/home/tester", nil }

	acpConstructCalls := 0
	cliConstructCalls := 0
	startNewACPClient = func(codexacp.Config) startBackendClient {
		acpConstructCalls++
		return &stubStartACP{}
	}
	startNewCLIClient = func(config.Config) backend.Client {
		cliConstructCalls++
		return &stubStartACP{}
	}
	startNewBridgeService = func(backend.Client) startBridgeService { return &stubStartBridge{} }
	startNewILinkClientAndMonitor = func(ilink.Credentials, string) (startSender, startMonitorRunner) {
		return &stubStartSender{}, &stubStartMonitor{}
	}

	err := runStart(context.Background(), &cobra.Command{})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if cliConstructCalls != 1 {
		t.Fatalf("expected CLI backend constructor once, got %d", cliConstructCalls)
	}
	if acpConstructCalls != 0 {
		t.Fatalf("expected ACP backend constructor not called, got %d", acpConstructCalls)
	}
	if len(loadedCredsCfgs) != 1 {
		t.Fatalf("expected credentials loaded once, got %d", len(loadedCredsCfgs))
	}
	if loadedCredsCfgs[0].BackendType != "cli" {
		t.Fatalf("expected credentials load with cli backend config, got %q", loadedCredsCfgs[0].BackendType)
	}
}

func TestRunStartPrintsBootstrapNoticeBeforeForegroundNotice(t *testing.T) {
	withStubbedStartDeps(t)

	var out bytes.Buffer
	startOutputWriter = func(*cobra.Command) io.Writer { return &out }
	startLoadRuntimeConfig = func(w io.Writer) (config.Config, error) {
		if _, err := fmt.Fprintln(w, defaultConfigCreatedNotice); err != nil {
			return config.Config{}, err
		}
		return config.Config{CodexCommand: "codex", CodexArgs: []string{"acp"}, WorkingDirectory: "/tmp", PermissionMode: "readonly"}, nil
	}
	startLoadCredentials = func(config.Config) (ilink.Credentials, error) {
		return ilink.Credentials{}, nil
	}
	startUserHomeDir = func() (string, error) { return "/home/tester", nil }
	startNewACPClient = func(codexacp.Config) startBackendClient { return &stubStartACP{} }
	startNewBridgeService = func(backend.Client) startBridgeService { return &stubStartBridge{} }
	startNewILinkClientAndMonitor = func(ilink.Credentials, string) (startSender, startMonitorRunner) {
		return &stubStartSender{}, &stubStartMonitor{}
	}

	err := runStart(context.Background(), &cobra.Command{})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if got, want := out.String(), defaultConfigCreatedNotice+"\n"+startForegroundNotice+"\n"; got != want {
		t.Fatalf("unexpected output order:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestRunStartUsesSingleWriterForBootstrapAndForegroundNotices(t *testing.T) {
	withStubbedStartDeps(t)

	var first bytes.Buffer
	var second bytes.Buffer
	calls := 0
	startOutputWriter = func(*cobra.Command) io.Writer {
		calls++
		if calls == 1 {
			return &first
		}
		return &second
	}
	startLoadRuntimeConfig = func(w io.Writer) (config.Config, error) {
		if _, err := fmt.Fprintln(w, defaultConfigCreatedNotice); err != nil {
			return config.Config{}, err
		}
		return config.Config{CodexCommand: "codex", CodexArgs: []string{"acp"}, WorkingDirectory: "/tmp", PermissionMode: "readonly"}, nil
	}
	startLoadCredentials = func(config.Config) (ilink.Credentials, error) {
		return ilink.Credentials{}, nil
	}
	startUserHomeDir = func() (string, error) { return "/home/tester", nil }
	startNewACPClient = func(codexacp.Config) startBackendClient { return &stubStartACP{} }
	startNewBridgeService = func(backend.Client) startBridgeService { return &stubStartBridge{} }
	startNewILinkClientAndMonitor = func(ilink.Credentials, string) (startSender, startMonitorRunner) {
		return &stubStartSender{}, &stubStartMonitor{}
	}

	err := runStart(context.Background(), &cobra.Command{})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if got, want := first.String(), defaultConfigCreatedNotice+"\n"+startForegroundNotice+"\n"; got != want {
		t.Fatalf("expected both notices on the first writer:\nwant: %q\ngot:  %q", want, got)
	}
	if second.Len() != 0 {
		t.Fatalf("expected second writer to stay unused, got %q", second.String())
	}
}

func TestRunStartReturnsBootstrapErrorImmediately(t *testing.T) {
	withStubbedStartDeps(t)

	wantErr := errors.New("invalid existing config")
	startLoadRuntimeConfig = func(io.Writer) (config.Config, error) {
		return config.Config{}, wantErr
	}

	credsCalls := 0
	startLoadCredentials = func(config.Config) (ilink.Credentials, error) {
		credsCalls++
		return ilink.Credentials{}, nil
	}

	acpConstructCalls := 0
	cliConstructCalls := 0
	startNewACPClient = func(codexacp.Config) startBackendClient {
		acpConstructCalls++
		return &stubStartACP{}
	}
	startNewCLIClient = func(config.Config) backend.Client {
		cliConstructCalls++
		return &stubStartACP{}
	}

	err := runStart(context.Background(), &cobra.Command{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected bootstrap error %v, got %v", wantErr, err)
	}
	if credsCalls != 0 {
		t.Fatalf("expected credentials not loaded, got %d calls", credsCalls)
	}
	if acpConstructCalls != 0 || cliConstructCalls != 0 {
		t.Fatalf("expected no backend construction, got acp=%d cli=%d", acpConstructCalls, cliConstructCalls)
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
	startNewACPClient = func(codexacp.Config) startBackendClient { return acp }
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

func TestRunStartSelectsACPBackendWhenConfigured(t *testing.T) {
	withStubbedStartDeps(t)

	startLoadConfig = func() (config.Config, error) {
		return config.Config{BackendType: "acp", CodexCommand: "codex", CodexArgs: []string{"acp"}, WorkingDirectory: "/tmp", PermissionMode: "readonly"}, nil
	}
	startLoadCredentials = func(config.Config) (ilink.Credentials, error) {
		return ilink.Credentials{}, nil
	}
	startUserHomeDir = func() (string, error) { return "/home/tester", nil }

	acpClient := &stubStartACP{}
	acpConstructCalls := 0
	cliConstructCalls := 0
	startNewACPClient = func(codexacp.Config) startBackendClient {
		acpConstructCalls++
		return acpClient
	}
	startNewCLIClient = func(config.Config) backend.Client {
		cliConstructCalls++
		return &stubStartACP{}
	}
	startNewBridgeService = func(backend.Client) startBridgeService { return &stubStartBridge{} }
	startNewILinkClientAndMonitor = func(ilink.Credentials, string) (startSender, startMonitorRunner) {
		return &stubStartSender{}, &stubStartMonitor{}
	}

	err := runStart(context.Background(), &cobra.Command{})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if acpConstructCalls != 1 {
		t.Fatalf("expected ACP backend constructor once, got %d", acpConstructCalls)
	}
	if cliConstructCalls != 0 {
		t.Fatalf("expected CLI backend constructor not called, got %d", cliConstructCalls)
	}
}

func TestRunStartSelectsCLIBackendWhenConfigured(t *testing.T) {
	withStubbedStartDeps(t)

	startLoadConfig = func() (config.Config, error) {
		return config.Config{BackendType: "cli", CodexCommand: "codex", WorkingDirectory: "/tmp", PermissionMode: "readonly"}, nil
	}
	startLoadCredentials = func(config.Config) (ilink.Credentials, error) {
		return ilink.Credentials{}, nil
	}
	startUserHomeDir = func() (string, error) { return "/home/tester", nil }

	cliClient := &stubStartACP{}
	acpConstructCalls := 0
	cliConstructCalls := 0
	startNewACPClient = func(codexacp.Config) startBackendClient {
		acpConstructCalls++
		return &stubStartACP{}
	}
	startNewCLIClient = func(config.Config) backend.Client {
		cliConstructCalls++
		return cliClient
	}
	startNewBridgeService = func(backend.Client) startBridgeService { return &stubStartBridge{} }
	startNewILinkClientAndMonitor = func(ilink.Credentials, string) (startSender, startMonitorRunner) {
		return &stubStartSender{}, &stubStartMonitor{}
	}

	err := runStart(context.Background(), &cobra.Command{})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if cliConstructCalls != 1 {
		t.Fatalf("expected CLI backend constructor once, got %d", cliConstructCalls)
	}
	if acpConstructCalls != 0 {
		t.Fatalf("expected ACP backend constructor not called, got %d", acpConstructCalls)
	}
}

func TestRunStartReturnsACPStartErrorAndDoesNotStartMonitorTraffic(t *testing.T) {
	withStubbedStartDeps(t)

	startLoadConfig = func() (config.Config, error) {
		return config.Config{BackendType: "acp", CodexCommand: "codex", CodexArgs: []string{"acp"}, WorkingDirectory: "/tmp", PermissionMode: "readonly"}, nil
	}
	startLoadCredentials = func(config.Config) (ilink.Credentials, error) {
		return ilink.Credentials{}, nil
	}

	wantErr := errors.New("acp start failed")
	acp := &stubStartACP{startErr: wantErr}
	monitorRuns := 0
	clientMonitorConstructCalls := 0
	startNewACPClient = func(codexacp.Config) startBackendClient { return acp }
	startNewCLIClient = func(config.Config) backend.Client { return &stubStartACP{} }
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

	startNewACPClient = func(codexacp.Config) startBackendClient { return acp }
	startNewILinkClientAndMonitor = func(ilink.Credentials, string) (startSender, startMonitorRunner) { return sender, monitor }
	startNewBridgeService = func(backend.Client) startBridgeService { return bridgeSvc }

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

	startNewACPClient = func(codexacp.Config) startBackendClient { return acp }
	startNewBridgeService = func(backend.Client) startBridgeService { return bridgeSvc }
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
	startNewACPClient = func(codexacp.Config) startBackendClient { return acp }
	startNewBridgeService = func(acp backend.Client) startBridgeService { return &stubStartBridge{} }
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
