package ilink

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNextBackoffCapsAtThirtySeconds(t *testing.T) {
	cases := []struct {
		failures int
		want     time.Duration
	}{
		{failures: 1, want: 3 * time.Second},
		{failures: 2, want: 6 * time.Second},
		{failures: 3, want: 12 * time.Second},
		{failures: 4, want: 24 * time.Second},
		{failures: 5, want: 30 * time.Second},
		{failures: 6, want: 30 * time.Second},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("failures_%d", tc.failures), func(t *testing.T) {
			got := nextBackoff(tc.failures)
			if got != tc.want {
				t.Fatalf("nextBackoff(%d) = %v, want %v", tc.failures, got, tc.want)
			}
		})
	}
}

func TestRunOnceResetsCursorOnlyForSessionInvalidError(t *testing.T) {
	t.Run("unknown error preserves cursor", func(t *testing.T) {
		cursorPath := filepath.Join(t.TempDir(), "cursor.json")
		mustWriteCursorFile(t, cursorPath, "cursor-keep")

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ret":1,"errcode":123,"errmsg":"unknown"}`))
		}))
		defer server.Close()

		monitor := NewMonitor(NewClient(Credentials{BotToken: "t", BaseURL: server.URL}), cursorPath)
		_, err := monitor.RunOnce(context.Background())
		if err == nil {
			t.Fatal("expected error for non-zero errcode")
		}

		if got := mustReadCursorFile(t, cursorPath); got != "cursor-keep" {
			t.Fatalf("expected cursor to be preserved, got %q", got)
		}
	})

	t.Run("session invalid resets cursor", func(t *testing.T) {
		cursorPath := filepath.Join(t.TempDir(), "cursor.json")
		mustWriteCursorFile(t, cursorPath, "cursor-old")

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ret":1,"errcode":-14,"errmsg":"session invalid"}`))
		}))
		defer server.Close()

		monitor := NewMonitor(NewClient(Credentials{BotToken: "t", BaseURL: server.URL}), cursorPath)
		_, err := monitor.RunOnce(context.Background())
		if err == nil {
			t.Fatal("expected error for session invalid response")
		}

		if got := mustReadCursorFile(t, cursorPath); got != "" {
			t.Fatalf("expected cursor reset to empty, got %q", got)
		}
	})
}

func TestRunOnceEmitsOnlyFinishedUserTextMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ret":0,
			"get_updates_buf":"cursor-next",
			"msgs":[
				{"from_user_id":"u1","message_type":1,"message_state":2,"context_token":"ctx-1","item_list":[{"type":1,"text_item":{"text":"hello"}}]},
				{"from_user_id":"u2","message_type":2,"message_state":2,"context_token":"ctx-2","item_list":[{"type":1,"text_item":{"text":"ignore bot"}}]},
				{"from_user_id":"u3","message_type":1,"message_state":1,"context_token":"ctx-3","item_list":[{"type":1,"text_item":{"text":"ignore generating"}}]},
				{"from_user_id":"u4","message_type":1,"message_state":2,"context_token":"ctx-4","item_list":[{"type":0}]},
				{"from_user_id":"u5","message_type":1,"message_state":2,"context_token":"ctx-5","item_list":[{"type":0},{"type":1,"text_item":{"text":"world"}}]}
			]
		}`))
	}))
	defer server.Close()

	cursorPath := filepath.Join(t.TempDir(), "cursor.json")
	monitor := NewMonitor(NewClient(Credentials{BotToken: "t", BaseURL: server.URL}), cursorPath)

	got, err := monitor.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 filtered messages, got %d", len(got))
	}

	if got[0].FromUserID != "u1" || got[0].ContextToken != "ctx-1" || got[0].Text != "hello" {
		t.Fatalf("unexpected first message: %#v", got[0])
	}
	if got[1].FromUserID != "u5" || got[1].ContextToken != "ctx-5" || got[1].Text != "world" {
		t.Fatalf("unexpected second message: %#v", got[1])
	}
}

func TestRunLogsWarningOnlyWhenCrossingThreeConsecutiveFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	cursorPath := filepath.Join(t.TempDir(), "cursor.json")
	monitor := NewMonitor(NewClient(Credentials{BotToken: "t", BaseURL: server.URL}), cursorPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var slept []time.Duration
	var logBuf bytes.Buffer
	rt := monitorRuntime{
		sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			if len(slept) >= 5 {
				cancel()
			}
			return nil
		},
		logf: func(format string, args ...any) {
			_, _ = fmt.Fprintf(&logBuf, format, args...)
		},
	}

	err := monitor.run(ctx, func(InboundMessage) error { return nil }, rt)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}

	want := []time.Duration{3 * time.Second, 6 * time.Second, 12 * time.Second, 24 * time.Second, 30 * time.Second}
	if !reflect.DeepEqual(slept, want) {
		t.Fatalf("unexpected backoff sequence: %#v", slept)
	}
	if got := strings.Count(logBuf.String(), "warning: ilink monitor poll failed"); got != 1 {
		t.Fatalf("expected exactly one warning log entry, got %d: %q", got, logBuf.String())
	}
}

func TestRunResetsFailureCounterAfterSuccessfulPoll(t *testing.T) {
	call := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		switch call {
		case 1, 2, 4:
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ret":0,"get_updates_buf":"cursor-ok"}`))
		}
	}))
	defer server.Close()

	cursorPath := filepath.Join(t.TempDir(), "cursor.json")
	monitor := NewMonitor(NewClient(Credentials{BotToken: "t", BaseURL: server.URL}), cursorPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var slept []time.Duration
	rt := monitorRuntime{
		sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			if len(slept) >= 3 {
				cancel()
			}
			return nil
		},
		logf: func(string, ...any) {},
	}

	err := monitor.run(ctx, func(InboundMessage) error { return nil }, rt)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}

	want := []time.Duration{3 * time.Second, 6 * time.Second, 3 * time.Second}
	if !reflect.DeepEqual(slept, want) {
		t.Fatalf("expected backoff %v, got %v", want, slept)
	}
}

func TestRunOncePersistsAndLoadsCursorJSON(t *testing.T) {
	cursorPath := filepath.Join(t.TempDir(), "cursor.json")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		calls++
		var req GetUpdatesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		switch calls {
		case 1:
			if req.GetUpdatesBuf != "" {
				t.Fatalf("first request expected empty cursor, got %q", req.GetUpdatesBuf)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ret":0,"get_updates_buf":"cursor-1"}`))
		case 2:
			if req.GetUpdatesBuf != "cursor-1" {
				t.Fatalf("second request expected cursor-1, got %q", req.GetUpdatesBuf)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ret":0,"get_updates_buf":"cursor-2"}`))
		default:
			t.Fatalf("unexpected call %d", calls)
		}
	}))
	defer server.Close()

	monitor := NewMonitor(NewClient(Credentials{BotToken: "t", BaseURL: server.URL}), cursorPath)
	if _, err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatalf("first RunOnce error: %v", err)
	}
	if _, err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatalf("second RunOnce error: %v", err)
	}

	if got := mustReadCursorFile(t, cursorPath); got != "cursor-2" {
		t.Fatalf("expected persisted cursor-2, got %q", got)
	}
}

func TestRunDoesNotAdvanceCursorWhenHandlerFails(t *testing.T) {
	cursorPath := filepath.Join(t.TempDir(), "cursor.json")
	mustWriteCursorFile(t, cursorPath, "cursor-old")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ret":0,
			"get_updates_buf":"cursor-next",
			"msgs":[{"from_user_id":"u1","message_type":1,"message_state":2,"context_token":"ctx-1","item_list":[{"type":1,"text_item":{"text":"hello"}}]}]
		}`))
	}))
	defer server.Close()

	monitor := NewMonitor(NewClient(Credentials{BotToken: "t", BaseURL: server.URL}), cursorPath)
	wantErr := errors.New("handler failed")
	err := monitor.Run(context.Background(), func(InboundMessage) error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected handler error, got %v", err)
	}

	if got := mustReadCursorFile(t, cursorPath); got != "cursor-old" {
		t.Fatalf("expected cursor unchanged on handler failure, got %q", got)
	}
}

func TestMonitorNilClientReturnsErrorInsteadOfPanicking(t *testing.T) {
	t.Run("RunOnce", func(t *testing.T) {
		monitor := NewMonitor(nil, filepath.Join(t.TempDir(), "cursor.json"))
		_, err := monitor.RunOnce(context.Background())
		if err == nil || !strings.Contains(err.Error(), "client is nil") {
			t.Fatalf("expected nil client error, got %v", err)
		}
	})

	t.Run("Run", func(t *testing.T) {
		monitor := NewMonitor(nil, filepath.Join(t.TempDir(), "cursor.json"))
		err := monitor.Run(context.Background(), func(InboundMessage) error { return nil })
		if err == nil || !strings.Contains(err.Error(), "client is nil") {
			t.Fatalf("expected nil client error, got %v", err)
		}
	})
}

func TestRunOnceCorruptCursorFallsBackToEmptyCursor(t *testing.T) {
	cursorPath := filepath.Join(t.TempDir(), "cursor.json")
	if err := os.WriteFile(cursorPath, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt cursor file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req GetUpdatesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.GetUpdatesBuf != "" {
			t.Fatalf("expected empty cursor fallback, got %q", req.GetUpdatesBuf)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":0,"get_updates_buf":"cursor-recovered"}`))
	}))
	defer server.Close()

	monitor := NewMonitor(NewClient(Credentials{BotToken: "t", BaseURL: server.URL}), cursorPath)
	if _, err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}

	if got := mustReadCursorFile(t, cursorPath); got != "cursor-recovered" {
		t.Fatalf("expected recovered cursor to persist, got %q", got)
	}
}

func mustReadCursorFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cursor file: %v", err)
	}
	var payload struct {
		GetUpdatesBuf string `json:"get_updates_buf"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode cursor file: %v", err)
	}
	return payload.GetUpdatesBuf
}

func mustWriteCursorFile(t *testing.T, path, cursor string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir cursor dir: %v", err)
	}
	payload := struct {
		GetUpdatesBuf string `json:"get_updates_buf"`
	}{GetUpdatesBuf: cursor}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal cursor file: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write cursor file: %v", err)
	}
}
