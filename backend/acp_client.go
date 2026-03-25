package backend

import (
	"context"
	"errors"

	"github.com/xiongzheX/weCodex/codexacp"
)

type acpInner interface {
	Start(ctx context.Context) error
	Stop() error
	Prompt(ctx context.Context, req codexacp.PromptRequest) (codexacp.PromptResult, error)
	Health() codexacp.HealthSnapshot
}

type acpClient struct {
	inner acpInner
}

func NewACPClient(cfg codexacp.Config) Client {
	return &acpClient{inner: codexacp.NewClient(cfg)}
}

func (c *acpClient) Start(ctx context.Context) error {
	if err := c.inner.Start(ctx); err != nil {
		return translateACPError(err)
	}
	return nil
}

func (c *acpClient) Stop() error {
	if err := c.inner.Stop(); err != nil {
		return translateACPError(err)
	}
	return nil
}

func (c *acpClient) Prompt(ctx context.Context, req PromptRequest) (PromptResult, error) {
	res, err := c.inner.Prompt(ctx, codexacp.PromptRequest{
		SenderID:  req.SenderID,
		SessionID: req.SessionID,
		Text:      req.Text,
		Timeout:   req.Timeout,
	})
	if err != nil {
		return PromptResult{}, translateACPError(err)
	}
	return PromptResult{SessionID: res.SessionID, ReplyText: res.ReplyText}, nil
}

func (c *acpClient) Health() HealthSnapshot {
	h := c.inner.Health()
	return HealthSnapshot{
		State:            translateHealthState(h.State),
		LastErrorSummary: h.LastErrorSummary,
	}
}

func translateHealthState(state codexacp.HealthState) HealthState {
	switch state {
	case codexacp.HealthReady:
		return HealthReady
	case codexacp.HealthDegraded:
		return HealthDegraded
	case codexacp.HealthUnavailable:
		return HealthUnavailable
	default:
		return HealthUnavailable
	}
}

func translateACPError(err error) error {
	if err == nil {
		return nil
	}

	var startupErr *codexacp.StartupError
	if errors.As(err, &startupErr) {
		return &StartupError{Err: startupErr.Unwrap()}
	}

	var timeoutErr *codexacp.PromptTimeoutError
	if errors.As(err, &timeoutErr) {
		return &PromptTimeoutError{Err: timeoutErr.Unwrap()}
	}

	var promptErr *codexacp.PromptError
	if errors.As(err, &promptErr) {
		return &PromptError{Err: promptErr.Unwrap()}
	}

	var sessionErr *codexacp.SessionError
	if errors.As(err, &sessionErr) {
		return &SessionError{Err: sessionErr.Unwrap()}
	}

	var permissionErr *codexacp.PermissionError
	if errors.As(err, &permissionErr) {
		return &PermissionError{Err: permissionErr.Unwrap()}
	}

	var transportErr *codexacp.TransportError
	if errors.As(err, &transportErr) {
		return &TransportError{Err: transportErr.Unwrap()}
	}

	var notStartedErr *codexacp.NotStartedError
	if errors.As(err, &notStartedErr) {
		return &NotStartedError{}
	}

	return err
}
