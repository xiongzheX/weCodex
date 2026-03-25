package backend

import (
	"context"
	"fmt"
	"time"
)

type Client interface {
	Start(ctx context.Context) error
	Stop() error
	Prompt(ctx context.Context, req PromptRequest) (PromptResult, error)
	Health() HealthSnapshot
}

type PromptRequest struct {
	SenderID  string
	SessionID string
	Text      string
	Timeout   time.Duration
}

type PromptResult struct {
	SessionID string
	ReplyText string
}

type HealthState string

const (
	HealthReady       HealthState = "ready"
	HealthDegraded    HealthState = "degraded"
	HealthUnavailable HealthState = "unavailable"
)

type HealthSnapshot struct {
	State            HealthState
	LastErrorSummary string
}

type StartupError struct{ Err error }

func (e *StartupError) Error() string { return fmt.Sprintf("startup failure: %v", e.Err) }
func (e *StartupError) Unwrap() error { return e.Err }

type PromptTimeoutError struct{ Err error }

func (e *PromptTimeoutError) Error() string { return fmt.Sprintf("prompt timeout: %v", e.Err) }
func (e *PromptTimeoutError) Unwrap() error { return e.Err }

type PromptError struct{ Err error }

func (e *PromptError) Error() string { return fmt.Sprintf("prompt failure: %v", e.Err) }
func (e *PromptError) Unwrap() error { return e.Err }

type SessionError struct{ Err error }

func (e *SessionError) Error() string { return fmt.Sprintf("session failure: %v", e.Err) }
func (e *SessionError) Unwrap() error { return e.Err }

type PermissionError struct{ Err error }

func (e *PermissionError) Error() string { return fmt.Sprintf("permission failure: %v", e.Err) }
func (e *PermissionError) Unwrap() error { return e.Err }

type TransportError struct{ Err error }

func (e *TransportError) Error() string { return fmt.Sprintf("transport failure: %v", e.Err) }
func (e *TransportError) Unwrap() error { return e.Err }

type NotStartedError struct{}

func (e *NotStartedError) Error() string { return "client not started" }
