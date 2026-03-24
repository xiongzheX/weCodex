package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiongzhe/weCodex/config"
	"github.com/xiongzhe/weCodex/ilink"
)

func withStubbedLoginDeps(t *testing.T) {
	t.Helper()

	origLoad := loginLoadConfig
	origFetch := loginFetchQRCode
	origPoll := loginPollQRStatus
	origSave := loginSaveCredentials
	origRender := loginRenderTerminalQRCode
	origOut := loginOutputWriter

	t.Cleanup(func() {
		loginLoadConfig = origLoad
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

func TestRunLoginPersistsCredentialsWithoutStartingCodex(t *testing.T) {
	withStubbedLoginDeps(t)

	loginLoadConfig = func() (config.Config, error) {
		return config.Config{WechatAccountsDir: filepath.Join(t.TempDir(), "accounts")}, errors.New("codex_command is required")
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

	_, err := runLoginCommand(t)
	if err != nil {
		t.Fatalf("expected login to succeed, got error: %v", err)
	}
	if saved != 1 {
		t.Fatalf("expected credentials save to be called once, got %d", saved)
	}
}

func TestRunLoginUsesWechatAccountsDirEvenWhenRuntimeConfigIsOtherwiseInvalid(t *testing.T) {
	withStubbedLoginDeps(t)

	accountsDir := filepath.Join(t.TempDir(), "accounts")
	loginLoadConfig = func() (config.Config, error) {
		return config.Config{WechatAccountsDir: accountsDir}, errors.New("permission_mode must be readonly")
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
	if usedCfg.WechatAccountsDir != accountsDir {
		t.Fatalf("expected WechatAccountsDir %q, got %q", accountsDir, usedCfg.WechatAccountsDir)
	}
}

func TestRunLoginDoesNotTrustDecodedConfigForCredentialPathWhenConfigLoadUnreliable(t *testing.T) {
	withStubbedLoginDeps(t)

	for _, tc := range []struct {
		name   string
		cfgErr error
	}{
		{name: "decode error", cfgErr: errors.New("decode config file: invalid character")},
		{name: "read error", cfgErr: errors.New("read config file: permission denied")},
		{name: "home resolution error", cfgErr: errors.New("resolve home dir: unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accountsDir := filepath.Join(t.TempDir(), "accounts")
			loginLoadConfig = func() (config.Config, error) {
				return config.Config{WechatAccountsDir: accountsDir}, tc.cfgErr
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
			if usedCfg.WechatAccountsDir != "" {
				t.Fatalf("expected WechatAccountsDir to be cleared for unreliable config errors, got %q", usedCfg.WechatAccountsDir)
			}
		})
	}
}

func TestRunLoginDisplaysQRCodeAndPollingStatus(t *testing.T) {
	withStubbedLoginDeps(t)

	loginLoadConfig = func() (config.Config, error) {
		return config.Config{}, os.ErrNotExist
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

func TestRunLoginPrintsQRCodeTextWhenRendererUnavailable(t *testing.T) {
	withStubbedLoginDeps(t)

	loginLoadConfig = func() (config.Config, error) {
		return config.Config{}, os.ErrNotExist
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

	loginLoadConfig = func() (config.Config, error) {
		return config.Config{}, os.ErrNotExist
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
	loginLoadConfig = func() (config.Config, error) {
		return config.Config{}, os.ErrNotExist
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

	loginLoadConfig = func() (config.Config, error) {
		return config.Config{}, os.ErrNotExist
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
	loginLoadConfig = func() (config.Config, error) {
		return config.Config{}, os.ErrNotExist
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
	loginLoadConfig = func() (config.Config, error) {
		return config.Config{}, os.ErrNotExist
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
