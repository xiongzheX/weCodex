package backend

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xiongzhe/weCodex/codexacp"
)

type stubACPInner struct {
	promptFn func(ctx context.Context, req codexacp.PromptRequest) (codexacp.PromptResult, error)
	health   codexacp.HealthSnapshot
}

func (s *stubACPInner) Start(ctx context.Context) error { return nil }
func (s *stubACPInner) Stop() error                     { return nil }

func (s *stubACPInner) Prompt(ctx context.Context, req codexacp.PromptRequest) (codexacp.PromptResult, error) {
	if s.promptFn != nil {
		return s.promptFn(ctx, req)
	}
	return codexacp.PromptResult{}, nil
}

func (s *stubACPInner) Health() codexacp.HealthSnapshot {
	return s.health
}

func TestACPClientPromptDelegatesRequestAndResult(t *testing.T) {
	t.Parallel()

	var gotInnerReq codexacp.PromptRequest
	inner := &stubACPInner{
		promptFn: func(_ context.Context, req codexacp.PromptRequest) (codexacp.PromptResult, error) {
			gotInnerReq = req
			return codexacp.PromptResult{SessionID: "session-from-inner", ReplyText: "reply-from-inner"}, nil
		},
	}

	client := &acpClient{inner: inner}
	req := PromptRequest{
		SenderID:  "sender-1",
		SessionID: "session-1",
		Text:      "hello",
		Timeout:   2 * time.Second,
	}

	got, err := client.Prompt(context.Background(), req)
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}

	if got.SessionID != "session-from-inner" || got.ReplyText != "reply-from-inner" {
		t.Fatalf("Prompt() result = %+v, want mapped inner result", got)
	}

	wantInnerReq := codexacp.PromptRequest{
		SenderID:  req.SenderID,
		SessionID: req.SessionID,
		Text:      req.Text,
		Timeout:   req.Timeout,
	}
	if gotInnerReq != wantInnerReq {
		t.Fatalf("inner Prompt request = %+v, want %+v", gotInnerReq, wantInnerReq)
	}
}

func TestACPClientHealthTranslatesState(t *testing.T) {
	t.Parallel()

	client := &acpClient{inner: &stubACPInner{health: codexacp.HealthSnapshot{State: codexacp.HealthState("mystery"), LastErrorSummary: "inner-summary"}}}

	got := client.Health()
	if got.State != HealthUnavailable {
		t.Fatalf("Health() state = %q, want %q", got.State, HealthUnavailable)
	}
	if got.LastErrorSummary != "inner-summary" {
		t.Fatalf("Health() LastErrorSummary = %q, want %q", got.LastErrorSummary, "inner-summary")
	}
}

func TestTranslateACPError(t *testing.T) {
	t.Parallel()

	cause := errors.New("root cause")
	tests := []struct {
		name      string
		in        error
		assertErr func(t *testing.T, got error)
	}{
		{
			name: "nil stays nil",
			in:   nil,
			assertErr: func(t *testing.T, got error) {
				t.Helper()
				if got != nil {
					t.Fatalf("translateACPError(nil) = %v, want nil", got)
				}
			},
		},
		{
			name: "startup error",
			in:   &codexacp.StartupError{Err: cause},
			assertErr: func(t *testing.T, got error) {
				t.Helper()
				var typed *StartupError
				if !errors.As(got, &typed) {
					t.Fatalf("got %T, want *StartupError", got)
				}
				if !errors.Is(got, cause) {
					t.Fatalf("got %v does not unwrap to cause", got)
				}
			},
		},
		{
			name: "prompt timeout error",
			in:   &codexacp.PromptTimeoutError{Err: cause},
			assertErr: func(t *testing.T, got error) {
				t.Helper()
				var typed *PromptTimeoutError
				if !errors.As(got, &typed) {
					t.Fatalf("got %T, want *PromptTimeoutError", got)
				}
				if !errors.Is(got, cause) {
					t.Fatalf("got %v does not unwrap to cause", got)
				}
			},
		},
		{
			name: "prompt error",
			in:   &codexacp.PromptError{Err: cause},
			assertErr: func(t *testing.T, got error) {
				t.Helper()
				var typed *PromptError
				if !errors.As(got, &typed) {
					t.Fatalf("got %T, want *PromptError", got)
				}
				if !errors.Is(got, cause) {
					t.Fatalf("got %v does not unwrap to cause", got)
				}
			},
		},
		{
			name: "session error",
			in:   &codexacp.SessionError{Err: cause},
			assertErr: func(t *testing.T, got error) {
				t.Helper()
				var typed *SessionError
				if !errors.As(got, &typed) {
					t.Fatalf("got %T, want *SessionError", got)
				}
				if !errors.Is(got, cause) {
					t.Fatalf("got %v does not unwrap to cause", got)
				}
			},
		},
		{
			name: "permission error",
			in:   &codexacp.PermissionError{Err: cause},
			assertErr: func(t *testing.T, got error) {
				t.Helper()
				var typed *PermissionError
				if !errors.As(got, &typed) {
					t.Fatalf("got %T, want *PermissionError", got)
				}
				if !errors.Is(got, cause) {
					t.Fatalf("got %v does not unwrap to cause", got)
				}
			},
		},
		{
			name: "transport error",
			in:   &codexacp.TransportError{Err: cause},
			assertErr: func(t *testing.T, got error) {
				t.Helper()
				var typed *TransportError
				if !errors.As(got, &typed) {
					t.Fatalf("got %T, want *TransportError", got)
				}
				if !errors.Is(got, cause) {
					t.Fatalf("got %v does not unwrap to cause", got)
				}
			},
		},
		{
			name: "not started error",
			in:   &codexacp.NotStartedError{},
			assertErr: func(t *testing.T, got error) {
				t.Helper()
				var typed *NotStartedError
				if !errors.As(got, &typed) {
					t.Fatalf("got %T, want *NotStartedError", got)
				}
			},
		},
		{
			name: "unknown error passthrough",
			in:   cause,
			assertErr: func(t *testing.T, got error) {
				t.Helper()
				if got != cause {
					t.Fatalf("got %v, want same unknown error instance %v", got, cause)
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := translateACPError(tc.in)
			tc.assertErr(t, got)
		})
	}
}

func TestTranslateHealthState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   codexacp.HealthState
		want HealthState
	}{
		{name: "ready", in: codexacp.HealthReady, want: HealthReady},
		{name: "degraded", in: codexacp.HealthDegraded, want: HealthDegraded},
		{name: "unavailable", in: codexacp.HealthUnavailable, want: HealthUnavailable},
		{name: "unknown defaults to unavailable", in: codexacp.HealthState("unknown"), want: HealthUnavailable},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := translateHealthState(tc.in); got != tc.want {
				t.Fatalf("translateHealthState(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
