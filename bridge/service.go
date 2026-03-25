package bridge

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/xiongzheX/weCodex/backend"
	"github.com/xiongzheX/weCodex/ilink"
)

const (
	defaultPromptTimeout = 120 * time.Second
	busyReplyText        = "上一条请求还在处理中，请稍后再试。"
)


type OutboundReply struct {
	ToUserID     string
	ContextToken string
	Text         string
}

type Service struct {
	acp backend.Client

	mu              sync.Mutex
	senderSession   map[string]string
	promptLockHeld  bool
	promptLockOwner string
}

func NewService(acp backend.Client) *Service {
	if acp == nil {
		panic("bridge.NewService: nil backend.Client")
	}
	return &Service{acp: acp, senderSession: make(map[string]string)}
}

func (s *Service) HasActiveSession(userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.senderSession[userID]
	return ok
}

func (s *Service) HandleMessage(ctx context.Context, msg ilink.InboundMessage) (OutboundReply, error) {
	input := ParseInput(msg.Text)

	switch input.Kind {
	case InputHelp:
		return s.replyFor(msg, BuildHelpText()), nil
	case InputStatus:
		h := s.acp.Health()
		status := BuildRuntimeStatus(RuntimeStatus{
			BridgeMode:       "running",
			BackendState:     string(h.State),
			HasActiveSession: s.HasActiveSession(msg.FromUserID),
			PermissionMode:   "read-only",
			LastErrorSummary: h.LastErrorSummary,
		})
		return s.replyFor(msg, status), nil
	case InputNew:
		if s.isBusyForSender(msg.FromUserID) {
			return s.replyFor(msg, busyReplyText), nil
		}
		s.clearSession(msg.FromUserID)
		return s.replyFor(msg, "已开始新会话。"), nil
	case InputPrompt:
		return s.handlePrompt(ctx, msg, input.Text)
	default:
		return s.replyFor(msg, "请求处理失败，请稍后再试。"), nil
	}
}

func (s *Service) handlePrompt(ctx context.Context, msg ilink.InboundMessage, text string) (OutboundReply, error) {
	sessionID, ok := s.tryAcquirePromptLock(msg.FromUserID)
	if !ok {
		return s.replyFor(msg, busyReplyText), nil
	}
	defer s.releasePromptLock(msg.FromUserID)

	res, err := s.acp.Prompt(ctx, backend.PromptRequest{
		SenderID:  msg.FromUserID,
		SessionID: sessionID,
		Text:      text,
		Timeout:   defaultPromptTimeout,
	})
	if err != nil {
		return s.replyFor(msg, s.handlePromptError(msg.FromUserID, err)), nil
	}

	if strings.TrimSpace(res.ReplyText) == "" {
		s.clearSession(msg.FromUserID)
		return s.replyFor(msg, "请求失败：响应为空，会话已重置，请重试。"), nil
	}

	s.storeSession(msg.FromUserID, res.SessionID)
	return s.replyFor(msg, res.ReplyText), nil
}

func (s *Service) handlePromptError(senderID string, err error) string {
	var timeoutErr *backend.PromptTimeoutError
	if errors.As(err, &timeoutErr) {
		s.clearSession(senderID)
		return "请求超时，会话已重置，请稍后重试。"
	}

	var sessionErr *backend.SessionError
	if errors.As(err, &sessionErr) {
		s.clearSession(senderID)
		return "会话异常，已重置，请重试。"
	}

	var permissionErr *backend.PermissionError
	if errors.As(err, &permissionErr) {
		s.clearSession(senderID)
		return strings.TrimSpace(permissionErr.Error())
	}

	var promptErr *backend.PromptError
	if errors.As(err, &promptErr) {
		s.clearSession(senderID)
		return "请求失败，会话已重置，请重试。"
	}

	s.clearSession(senderID)
	return "请求失败，请稍后再试。"
}

func (s *Service) tryAcquirePromptLock(senderID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.promptLockHeld {
		return "", false
	}
	s.promptLockHeld = true
	s.promptLockOwner = senderID
	return s.senderSession[senderID], true
}

func (s *Service) releasePromptLock(senderID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.promptLockHeld && s.promptLockOwner == senderID {
		s.promptLockHeld = false
		s.promptLockOwner = ""
	}
}

func (s *Service) isBusyForSender(senderID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.promptLockHeld && s.promptLockOwner == senderID
}

func (s *Service) clearSession(senderID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.senderSession, senderID)
}

func (s *Service) storeSession(senderID string, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(sessionID) == "" {
		delete(s.senderSession, senderID)
		return
	}
	s.senderSession[senderID] = sessionID
}

func (s *Service) replyFor(msg ilink.InboundMessage, text string) OutboundReply {
	return OutboundReply{ToUserID: msg.FromUserID, ContextToken: msg.ContextToken, Text: text}
}
