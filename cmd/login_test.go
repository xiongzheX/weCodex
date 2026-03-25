package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiongzhe/weCodex/config"
	"github.com/xiongzhe/weCodex/ilink"
)

func withStubbedLoginDeps(t *testing.T) {
	t.Helper()

	origLoad := loginLoadRuntimeConfig
	origFetch := loginFetchQRCode
	origPoll := loginPollQRStatus
	origSave := loginSaveCredentials
	origRender := loginRenderTerminalQRCode
	origOut := loginOutputWriter

	t.Cleanup(func() {
		loginLoadRuntimeConfig = origLoad
		loginFetchQRCode = origFetch
		loginPollQRStatus = origPoll
		loginSaveCredentials = origSave
		loginRenderTerminalQRCode = origRender
		loginOutputWriter = origOut
	})
}

func runLoginCommand(t *testing.T) (string, error) {
	t.Helper()

	var out bytes.Buffer
	loginCmd.SetOut(&out)
	loginCmd.SetErr(&out)
	loginCmd.SetArgs(nil)
	defer loginCmd.SetOut(nil)
	defer loginCmd.SetErr(nil)
	defer loginCmd.SetArgs(nil)

	err := loginCmd.RunE(loginCmd, nil)
	return out.String(), err
}

func TestRunLoginBootstrapsMissingConfigAndPrintsNotice(t *testing.T) {
	withStubbedLoginDeps(t)

	loginLoadRuntimeConfig = func(out io.Writer) (config.Config, error) {
		fmt.Fprintln(out, defaultConfigCreatedNotice)
		return config.Config{WechatAccountsDir: filepath.Join(t.TempDir(), "accounts")}, nil
	}
	loginFetchQRCode = func(ctx context.Context, baseURL string) (ilink.QRCodeResponse, error) {
		return ilink.QRCodeResponse{QRCode: "qr-code", QRCodeImgContent: "qr-text"}, nil
	}
	loginPollQRStatus = func(ctx context.Context, baseURL string, qrCode string, onStatus func(string)) (ilink.Credentials, error) {
		onStatus("waiting")
		onStatus("confirmed")
		return ilink.Credentials{BotToken: "bot-token", ILinkBotID: "bot-id", BaseURL: "https://example.com", ILinkUserID: "user-id"}, nil
	}
	saved := 0
	loginSaveCredentials = func(cfg config.Config, creds ilink.Credentials) error {
		saved++
		return nil
	}
	loginRenderTerminalQRCode = func(out io.Writer, payload string) error {
		return nil
	}

	output, err := runLoginCommand(t)
	if err != nil {
		t.Fatalf("expected login to succeed, got error: %v", err)
	}
	if saved != 1 {
		t.Fatalf("expected credentials save to be called once, got %d", saved)
	}
	assertContains(t, output, defaultConfigCreatedNotice)
}

func TestRunLoginReturnsBootstrapErrorForInvalidExistingConfig(t *testing.T) {
	withStubbedLoginDeps(t)

	bootstrapErr := errors.New("permission_mode must be readonly")
	loginLoadRuntimeConfig = func(out io.Writer) (config.Config, error) {
		return config.Config{WechatAccountsDir: filepath.Join(t.TempDir(), "accounts")}, bootstrapErr
	}
	loginFetchQRCode = func(ctx context.Context, baseURL string) (ilink.QRCodeResponse, error) {
		t.Fatalf("fetch should not be called when runtime config bootstrap fails")
		return ilink.QRCodeResponse{}, nil
	}
	loginPollQRStatus = func(ctx context.Context, baseURL string, qrCode string, onStatus func(string)) (ilink.Credentials, error) {
		t.Fatalf("poll should not be called when runtime config bootstrap fails")
		return ilink.Credentials{}, nil
	}
	loginSaveCredentials = func(cfg config.Config, creds ilink.Credentials) error {
		t.Fatalf("save should not be called when runtime config bootstrap fails")
		return nil
	}
	loginRenderTerminalQRCode = func(out io.Writer, payload string) error {
		t.Fatalf("render should not be called when runtime config bootstrap fails")
		return nil
	}

	_, err := runLoginCommand(t)
	if !errors.Is(err, bootstrapErr) {
		t.Fatalf("expected bootstrap error %v, got %v", bootstrapErr, err)
	}
}

func TestRunLoginDisplaysQRCodeAndPollingStatus(t *testing.T) {
	withStubbedLoginDeps(t)

	loginLoadRuntimeConfig = func(out io.Writer) (config.Config, error) {
		return config.Config{}, nil
	}
	loginFetchQRCode = func(ctx context.Context, baseURL string) (ilink.QRCodeResponse, error) {
		return ilink.QRCodeResponse{QRCode: "qr-code", QRCodeImgContent: "qr-text"}, nil
	}
	loginPollQRStatus = func(ctx context.Context, baseURL string, qrCode string, onStatus func(string)) (ilink.Credentials, error) {
		onStatus("waiting")
		onStatus("scaned")
		onStatus("confirmed")
		return ilink.Credentials{BotToken: "bot-token", ILinkBotID: "bot-id", BaseURL: "https://example.com", ILinkUserID: "user-id"}, nil
	}
	loginSaveCredentials = func(cfg config.Config, creds ilink.Credentials) error {
		return nil
	}
	loginRenderTerminalQRCode = func(out io.Writer, payload string) error {
		_, _ = io.WriteString(out, "rendered:"+payload+"\n")
		return nil
	}

	output, err := runLoginCommand(t)
	if err != nil {
		t.Fatalf("expected login to succeed, got error: %v", err)
	}
	assertContains(t, output, "rendered:qr-text")
	assertContains(t, output, "QR status: waiting")
	assertContains(t, output, "QR status: scaned")
	assertContains(t, output, "QR status: confirmed")
}

func TestRunLoginUsesDefaultRendererAndDoesNotFallbackToURLText(t *testing.T) {
	withStubbedLoginDeps(t)

	loginLoadRuntimeConfig = func(out io.Writer) (config.Config, error) {
		return config.Config{}, nil
	}
	loginFetchQRCode = func(ctx context.Context, baseURL string) (ilink.QRCodeResponse, error) {
		return ilink.QRCodeResponse{QRCode: "qr-code", QRCodeImgContent: "https://example.com/login-qr"}, nil
	}
	loginPollQRStatus = func(ctx context.Context, baseURL string, qrCode string, onStatus func(string)) (ilink.Credentials, error) {
		return ilink.Credentials{BotToken: "bot-token", ILinkBotID: "bot-id", BaseURL: "https://example.com", ILinkUserID: "user-id"}, nil
	}
	loginSaveCredentials = func(cfg config.Config, creds ilink.Credentials) error {
		return nil
	}

	output, err := runLoginCommand(t)
	if err != nil {
		t.Fatalf("expected login to succeed, got error: %v", err)
	}
	assertContains(t, output, "Scan the QR code to login:")
	if strings.Contains(output, "https://example.com/login-qr") {
		t.Fatalf("expected default terminal QR renderer output, got URL fallback text: %s", output)
	}
}

func TestRunLoginPrintsQRCodeTextWhenRendererUnavailable(t *testing.T) {
	withStubbedLoginDeps(t)

	loginLoadRuntimeConfig = func(out io.Writer) (config.Config, error) {
		return config.Config{}, nil
	}
	loginFetchQRCode = func(ctx context.Context, baseURL string) (ilink.QRCodeResponse, error) {
		return ilink.QRCodeResponse{QRCode: "qr-code", QRCodeImgContent: "fallback-qr-text"}, nil
	}
	loginPollQRStatus = func(ctx context.Context, baseURL string, qrCode string, onStatus func(string)) (ilink.Credentials, error) {
		return ilink.Credentials{BotToken: "bot-token", ILinkBotID: "bot-id", BaseURL: "https://example.com", ILinkUserID: "user-id"}, nil
	}
	loginSaveCredentials = func(cfg config.Config, creds ilink.Credentials) error {
		return nil
	}
	loginRenderTerminalQRCode = func(out io.Writer, payload string) error {
		return errors.New("renderer unavailable")
	}

	output, err := runLoginCommand(t)
	if err != nil {
		t.Fatalf("expected login to succeed, got error: %v", err)
	}
	assertContains(t, output, "fallback-qr-text")
}

func TestRunLoginFallsBackToDefaultCredentialsPathWhenConfigMissing(t *testing.T) {
	withStubbedLoginDeps(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	loginLoadRuntimeConfig = func(out io.Writer) (config.Config, error) {
		return config.Config{}, nil
	}
	loginFetchQRCode = func(ctx context.Context, baseURL string) (ilink.QRCodeResponse, error) {
		return ilink.QRCodeResponse{QRCode: "qr-code", QRCodeImgContent: "qr-text"}, nil
	}
	loginPollQRStatus = func(ctx context.Context, baseURL string, qrCode string, onStatus func(string)) (ilink.Credentials, error) {
		return ilink.Credentials{BotToken: "bot-token", ILinkBotID: "bot-id", BaseURL: "https://example.com", ILinkUserID: "user-id"}, nil
	}
	usedCfg := config.Config{}
	loginSaveCredentials = func(cfg config.Config, creds ilink.Credentials) error {
		usedCfg = cfg
		return nil
	}
	loginRenderTerminalQRCode = func(out io.Writer, payload string) error {
		return nil
	}

	_, err := runLoginCommand(t)
	if err != nil {
		t.Fatalf("expected login to succeed, got error: %v", err)
	}

	path, err := config.CredentialsPath(usedCfg)
	if err != nil {
		t.Fatalf("expected default credentials path to resolve, got error: %v", err)
	}
	want := filepath.Join(home, ".weCodex", "account.json")
	if path != want {
		t.Fatalf("expected default credentials path %q, got %q", want, path)
	}
}

func TestRunLoginReturnsFetchQRCodeError(t *testing.T) {
	withStubbedLoginDeps(t)

	wantErr := errors.New("fetch failed")
	loginLoadRuntimeConfig = func(out io.Writer) (config.Config, error) {
		return config.Config{}, nil
	}
	loginFetchQRCode = func(ctx context.Context, baseURL string) (ilink.QRCodeResponse, error) {
		return ilink.QRCodeResponse{}, wantErr
	}
	loginPollQRStatus = func(ctx context.Context, baseURL string, qrCode string, onStatus func(string)) (ilink.Credentials, error) {
		t.Fatalf("poll should not be called after fetch failure")
		return ilink.Credentials{}, nil
	}
	loginSaveCredentials = func(cfg config.Config, creds ilink.Credentials) error {
		t.Fatalf("save should not be called after fetch failure")
		return nil
	}
	loginRenderTerminalQRCode = func(out io.Writer, payload string) error {
		t.Fatalf("render should not be called after fetch failure")
		return nil
	}

	_, err := runLoginCommand(t)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected fetch error %v, got %v", wantErr, err)
	}
}

func TestRunLoginReturnsErrorForIncompleteFetchQRCodeResponse(t *testing.T) {
	withStubbedLoginDeps(t)

	loginLoadRuntimeConfig = func(out io.Writer) (config.Config, error) {
		return config.Config{}, nil
	}
	loginFetchQRCode = func(ctx context.Context, baseURL string) (ilink.QRCodeResponse, error) {
		return ilink.QRCodeResponse{QRCode: "", QRCodeImgContent: "qr-text"}, nil
	}
	loginPollQRStatus = func(ctx context.Context, baseURL string, qrCode string, onStatus func(string)) (ilink.Credentials, error) {
		t.Fatalf("poll should not be called for incomplete QR response")
		return ilink.Credentials{}, nil
	}
	loginSaveCredentials = func(cfg config.Config, creds ilink.Credentials) error {
		t.Fatalf("save should not be called for incomplete QR response")
		return nil
	}
	renderCalled := 0
	loginRenderTerminalQRCode = func(out io.Writer, payload string) error {
		renderCalled++
		return nil
	}

	_, err := runLoginCommand(t)
	if err == nil {
		t.Fatalf("expected error for incomplete QR response")
	}
	assertContains(t, err.Error(), "missing required fields")
	assertContains(t, err.Error(), "qrcode")
	if renderCalled != 0 {
		t.Fatalf("expected renderer not to be called, got %d calls", renderCalled)
	}
}

func TestRunLoginReturnsPollError(t *testing.T) {
	withStubbedLoginDeps(t)

	wantErr := errors.New("poll failed")
	loginLoadRuntimeConfig = func(out io.Writer) (config.Config, error) {
		return config.Config{}, nil
	}
	loginFetchQRCode = func(ctx context.Context, baseURL string) (ilink.QRCodeResponse, error) {
		return ilink.QRCodeResponse{QRCode: "qr-code", QRCodeImgContent: "qr-text"}, nil
	}
	loginPollQRStatus = func(ctx context.Context, baseURL string, qrCode string, onStatus func(string)) (ilink.Credentials, error) {
		return ilink.Credentials{}, wantErr
	}
	loginSaveCredentials = func(cfg config.Config, creds ilink.Credentials) error {
		t.Fatalf("save should not be called after poll failure")
		return nil
	}
	loginRenderTerminalQRCode = func(out io.Writer, payload string) error {
		return nil
	}

	_, err := runLoginCommand(t)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected poll error %v, got %v", wantErr, err)
	}
}

func TestRunLoginReturnsSaveError(t *testing.T) {
	withStubbedLoginDeps(t)

	wantErr := errors.New("save failed")
	loginLoadRuntimeConfig = func(out io.Writer) (config.Config, error) {
		return config.Config{}, nil
	}
	loginFetchQRCode = func(ctx context.Context, baseURL string) (ilink.QRCodeResponse, error) {
		return ilink.QRCodeResponse{QRCode: "qr-code", QRCodeImgContent: "qr-text"}, nil
	}
	loginPollQRStatus = func(ctx context.Context, baseURL string, qrCode string, onStatus func(string)) (ilink.Credentials, error) {
		return ilink.Credentials{BotToken: "bot-token", ILinkBotID: "bot-id", BaseURL: "https://example.com", ILinkUserID: "user-id"}, nil
	}
	loginSaveCredentials = func(cfg config.Config, creds ilink.Credentials) error {
		return wantErr
	}
	loginRenderTerminalQRCode = func(out io.Writer, payload string) error {
		return nil
	}

	_, err := runLoginCommand(t)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected save error %v, got %v", wantErr, err)
	}
}

func TestRunLoginRegistersCommandOnRoot(t *testing.T) {
	root := newRootCmd()

	found := false
	for _, sub := range root.Commands() {
		if sub.Name() == "login" {
			found = true
			break
		}
	}

	if !found {
		names := make([]string, 0, len(root.Commands()))
		for _, sub := range root.Commands() {
			names = append(names, sub.Name())
		}
		t.Fatalf("expected login command to be registered, got commands: %s", strings.Join(names, ", "))
	}
}
