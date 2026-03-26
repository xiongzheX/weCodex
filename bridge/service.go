package bridge

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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
	case InputList:
		text, err := s.handleList(ctx)
		if err != nil {
			return s.replyFor(msg, s.handleListError(err)), nil
		}
		return s.replyFor(msg, text), nil
	case InputUse:
		if input.UseIndex == nil {
			return s.replyFor(msg, "编号无效，请先使用 /list 查看线程。"), nil
		}
		text, err := s.handleUse(ctx, msg.FromUserID, *input.UseIndex)
		if err != nil {
			return s.replyFor(msg, err.Error()), nil
		}
		return s.replyFor(msg, text), nil
	case InputNew:
		if s.isBusyForSender(msg.FromUserID) {
			return s.replyFor(msg, busyReplyText), nil
		}
		text, err := s.handleNew(ctx, msg.FromUserID)
		if err != nil {
			return s.replyFor(msg, err.Error()), nil
		}
		return s.replyFor(msg, text), nil
	case InputPrompt:
		return s.handlePrompt(ctx, msg, input.Text)
	default:
		return s.replyFor(msg, "请求处理失败，请稍后再试。"), nil
	}
}

func (s *Service) handleList(ctx context.Context) (string, error) {
	res, err := s.acp.ListSessions(ctx)
	if err != nil {
		return "", err
	}
	return buildSessionListText(res), nil
}

func (s *Service) handleUse(ctx context.Context, senderID string, index int) (string, error) {
	list, err := s.acp.ListSessions(ctx)
	if err != nil {
		return "", err
	}
	if index < 1 || index > len(list.Sessions) {
		return "", fmt.Errorf("编号无效，请先使用 /list 查看线程。")
	}
	session := list.Sessions[index-1]
	s.storeSession(senderID, session.SessionID)
	return fmt.Sprintf("已切换到线程 %d：%s", index, sessionLabel(session, index)), nil
}

func (s *Service) handleNew(ctx context.Context, senderID string) (string, error) {
	session, err := s.acp.CreateSession(ctx, backend.SessionCreateRequest{SenderID: senderID})
	if err != nil {
		return "", err
	}
	s.storeSession(senderID, session.SessionID)
	if strings.TrimSpace(session.DisplayName) == "" {
		return "已切换到新线程。", nil
	}
	return fmt.Sprintf("已切换到新线程：%s", session.DisplayName), nil
}

func (s *Service) handleListError(err error) string {
	var notStartedErr *backend.NotStartedError
	if errors.As(err, &notStartedErr) {
		return "Codex CLI 不可用。"
	}
	return "读取线程列表失败，请稍后再试。"
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

func buildSessionListText(list backend.SessionListResult) string {
	if len(list.Sessions) == 0 {
		return "当前项目目录下暂无线程。"
	}

	lines := []string{"当前项目目录线程："}
	for i, session := range list.Sessions {
		number := i + 1
		marker := ""
		if session.SessionID == list.ActiveSessionID {
			marker = " [当前]"
		}
		lines = append(lines, fmt.Sprintf("%d.%s %s", number, marker, sessionLabel(session, number)))
	}
	return strings.Join(lines, "\n")
}

func sessionLabel(session backend.SessionInfo, fallbackIndex int) string {
	label := strings.TrimSpace(session.DisplayName)
	if label != "" {
		return label
	}
	if strings.TrimSpace(session.SessionID) != "" {
		return session.SessionID
	}
	return strconv.Itoa(fallbackIndex)
}
