package codexacp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Config struct {
	Command          string
	Args             []string
	WorkingDirectory string
	PermissionMode   string
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

type SessionInfo struct {
	SessionID   string `json:"sessionId"`
	DisplayName string `json:"displayName,omitempty"`
}

type SessionListResult struct {
	ActiveSessionID string        `json:"activeSessionId,omitempty"`
	Sessions        []SessionInfo `json:"sessions,omitempty"`
}

type SessionCreateRequest struct {
	SenderID string `json:"senderId"`
}

type SessionCreateResult struct {
	Session SessionInfo `json:"session"`
}

type RPCRequestEnvelope struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type RPCResponseEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type InitializeParams struct {
	PermissionMode string `json:"permissionMode"`
}

type InitializeResult struct {
	Server string `json:"server,omitempty"`
}

type SessionNewParams struct {
	SenderID string `json:"senderId"`
}

type SessionNewResult struct {
	SessionID string `json:"sessionId"`
}

type SessionPromptParams struct {
	SessionID string `json:"sessionId"`
	Text      string `json:"text"`
}

type SessionPromptResult struct {
	Completed bool `json:"completed"`
}

type SessionUpdateParams struct {
	SessionID string `json:"sessionId"`
	Text      string `json:"text"`
}

func (p *SessionUpdateParams) UnmarshalJSON(data []byte) error {
	var payload struct {
		SessionID string `json:"sessionId"`
		Text      string `json:"text"`
		Session   struct {
			ID string `json:"id"`
		} `json:"session"`
		Update struct {
			Text string `json:"text"`
		} `json:"update"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}

	p.SessionID = payload.SessionID
	if strings.TrimSpace(p.SessionID) == "" {
		p.SessionID = payload.Session.ID
	}
	p.Text = payload.Text
	if p.Text == "" {
		p.Text = payload.Update.Text
	}
	return nil
}

type SessionRequestPermissionParams struct {
	SessionID string          `json:"sessionId"`
	ToolCall  json.RawMessage `json:"toolCall"`
}

func (p *SessionRequestPermissionParams) UnmarshalJSON(data []byte) error {
	var payload struct {
		SessionID string          `json:"sessionId"`
		ToolCall  json.RawMessage `json:"toolCall"`
		Session   struct {
			ID string `json:"id"`
		} `json:"session"`
		Request struct {
			ToolCall json.RawMessage `json:"toolCall"`
		} `json:"request"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}

	p.SessionID = payload.SessionID
	if strings.TrimSpace(p.SessionID) == "" {
		p.SessionID = payload.Session.ID
	}
	p.ToolCall = payload.ToolCall
	if len(p.ToolCall) == 0 {
		p.ToolCall = payload.Request.ToolCall
	}
	return nil
}

type SessionRespondPermissionParams struct {
	SessionID string `json:"sessionId"`
	Allowed   bool   `json:"allowed"`
	Reason    string `json:"reason,omitempty"`
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

type runtimeNotification struct {
	Method string
	Params json.RawMessage
}

type runtimeClient interface {
	Start(ctx context.Context, cfg Config) error
	Stop() error
	Call(ctx context.Context, method string, params any, result any) error
	Notifications() <-chan runtimeNotification
	Errors() <-chan error
}

