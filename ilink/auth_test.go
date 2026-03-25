package ilink

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xiongzheX/weCodex/config"
)

func TestFetchQRCodeCallsExpectedEndpoint(t *testing.T) {
	var gotPath string
	var gotQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"qrcode":"qr-123","qrcode_img_content":"img-data"}`))
	}))
	defer server.Close()

	resp, err := FetchQRCode(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("fetch QR code: %v", err)
	}

	if gotPath != "/ilink/bot/get_bot_qrcode" {
		t.Fatalf("expected get_bot_qrcode endpoint, got %q", gotPath)
	}
	if gotQuery != "bot_type=3" {
		t.Fatalf("expected bot_type=3 query, got %q", gotQuery)
	}
	if resp.QRCode != "qr-123" {
		t.Fatalf("expected QRCode to decode, got %q", resp.QRCode)
	}
}

func TestSaveCredentialsWrites0600File(t *testing.T) {
	cfg := config.Config{WechatAccountsDir: filepath.Join(t.TempDir(), "accounts")}
	creds := Credentials{
		BotToken:    "bot-token",
		ILinkBotID:  "bot-id",
		BaseURL:     "https://example.com",
		ILinkUserID: "user-id",
	}

	if err := SaveCredentials(cfg, creds); err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	path, err := config.CredentialsPath(cfg)
	if err != nil {
		t.Fatalf("credentials path: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat credentials: %v", err)
	}

	perm := info.Mode().Perm()
	if runtime.GOOS == "windows" {
		if perm&0o077 != 0 {
			t.Fatalf("expected credentials file to remain owner-only on windows, got %o", perm)
		}
	} else if perm != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", perm)
	}

	loaded, err := LoadCredentials(cfg)
	if err != nil {
		t.Fatalf("load credentials: %v", err)
	}
	if loaded != creds {
		t.Fatalf("expected loaded creds %#v, got %#v", creds, loaded)
	}
}

func TestLoadCredentialsReturnsErrNotExistWhenMissing(t *testing.T) {
	cfg := config.Config{WechatAccountsDir: filepath.Join(t.TempDir(), "missing")}

	got, err := LoadCredentials(cfg)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
	if got != (Credentials{}) {
		t.Fatalf("expected zero credentials, got %#v", got)
	}
}

func TestPollQRStatusReturnsCredentialsOnConfirmedState(t *testing.T) {
	var requestPaths []string
	calls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPaths = append(requestPaths, r.URL.Path)
		calls++
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{"status":"waiting"}`))
			return
		case 2:
			_, _ = w.Write([]byte(`{"status":"scaned"}`))
			return
		default:
			_, _ = w.Write([]byte(`{"status":"confirmed","bot_token":"bot-token","ilink_bot_id":"bot-id","baseurl":"https://example.com","ilink_user_id":"user-id"}`))
		}
	}))
	defer server.Close()

	var statuses []string
	got, err := pollQRStatus(context.Background(), server.URL, "qr-123", func(status string) {
		statuses = append(statuses, status)
	}, time.Millisecond)
	if err != nil {
		t.Fatalf("poll QR status: %v", err)
	}

	wantCreds := Credentials{
		BotToken:    "bot-token",
		ILinkBotID:  "bot-id",
		BaseURL:     "https://example.com",
		ILinkUserID: "user-id",
	}
	if got != wantCreds {
		t.Fatalf("expected credentials %#v, got %#v", wantCreds, got)
	}

	if !slices.Equal(statuses, []string{"waiting", "scaned", "confirmed"}) {
		t.Fatalf("expected status callbacks [waiting scaned confirmed], got %#v", statuses)
	}
	for _, path := range requestPaths {
		if path != "/ilink/bot/get_qrcode_status" {
			t.Fatalf("expected get_qrcode_status endpoint, got %q", path)
		}
	}
}

func TestPollQRStatusUsesGETWithQRCodeQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/ilink/bot/get_qrcode_status" {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		if r.URL.RawQuery != "qrcode=qr-123" {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"confirmed","bot_token":"bot-token","ilink_bot_id":"bot-id","baseurl":"https://example.com","ilink_user_id":"user-id"}`))
	}))
	defer server.Close()

	got, err := pollQRStatus(context.Background(), server.URL, "qr-123", nil, time.Millisecond)
	if err != nil {
		t.Fatalf("poll QR status: %v", err)
	}

	want := Credentials{
		BotToken:    "bot-token",
		ILinkBotID:  "bot-id",
		BaseURL:     "https://example.com",
		ILinkUserID: "user-id",
	}
	if got != want {
		t.Fatalf("expected credentials %#v, got %#v", want, got)
	}
}

func TestPollQRStatusReturnsErrorWhenExpired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"expired"}`))
	}))
	defer server.Close()

	_, err := PollQRStatus(context.Background(), server.URL, "qr-123", nil)
	if !errors.Is(err, ErrQRCodeExpired) {
		t.Fatalf("expected ErrQRCodeExpired, got %v", err)
	}
}

func TestPollQRStatusReturnsErrorForUnknownStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"mystery"}`))
	}))
	defer server.Close()

	_, err := PollQRStatus(context.Background(), server.URL, "qr-123", nil)
	if err == nil {
		t.Fatal("expected error for unknown status, got nil")
	}
	if !strings.Contains(err.Error(), "unknown QR status") {
		t.Fatalf("expected unknown status error, got %v", err)
	}
}

func TestPollQRStatusReturnsErrorForConfirmedWithMissingCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"confirmed","bot_token":"bot-token","baseurl":"https://example.com"}`))
	}))
	defer server.Close()

	_, err := PollQRStatus(context.Background(), server.URL, "qr-123", nil)
	if err == nil {
		t.Fatal("expected error for incomplete confirmed credentials, got nil")
	}
	if !strings.Contains(err.Error(), "missing required credentials") {
		t.Fatalf("expected missing required credentials error, got %v", err)
	}
}

func TestPollQRStatusStopsOnContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&map[string]any{})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"waiting"}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := pollQRStatus(ctx, server.URL, "qr-123", nil, time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
}
