package ilink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const sessionInvalidErrCode = -14

type monitorRuntime struct {
	sleep func(context.Context, time.Duration) error
	logf  func(string, ...any)
}

func defaultMonitorRuntime() monitorRuntime {
	return monitorRuntime{
		sleep: sleepWithContext,
		logf:  log.Printf,
	}
}

type Monitor struct {
	client     *Client
	cursorPath string
}

type cursorFile struct {
	GetUpdatesBuf string `json:"get_updates_buf"`
}

func NewMonitor(client *Client, cursorPath string) *Monitor {
	return &Monitor{client: client, cursorPath: cursorPath}
}

func (m *Monitor) Run(ctx context.Context, handle func(InboundMessage) error) error {
	return m.run(ctx, handle, defaultMonitorRuntime())
}

func (m *Monitor) run(ctx context.Context, handle func(InboundMessage) error, rt monitorRuntime) error {
	if err := m.validateClient(); err != nil {
		return err
	}
	if handle == nil {
		return errors.New("handle function is required")
	}

	consecutiveFailures := 0
	for {
		msgs, nextCursor, err := m.poll(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			consecutiveFailures++
			if consecutiveFailures == 3 {
				rt.logf("warning: ilink monitor poll failed %d times: %v", consecutiveFailures, err)
			}

			if sleepErr := rt.sleep(ctx, nextBackoff(consecutiveFailures)); sleepErr != nil {
				return sleepErr
			}
			continue
		}

		consecutiveFailures = 0
		for _, msg := range msgs {
			if err := handle(msg); err != nil {
				return err
			}
		}
		if nextCursor != "" {
			if err := m.persistCursor(nextCursor); err != nil {
				return err
			}
		}
	}
}

func (m *Monitor) RunOnce(ctx context.Context) ([]InboundMessage, error) {
	if err := m.validateClient(); err != nil {
		return nil, err
	}

	msgs, nextCursor, err := m.poll(ctx)
	if err != nil {
		return nil, err
	}
	if nextCursor != "" {
		if err := m.persistCursor(nextCursor); err != nil {
			return nil, err
		}
	}
	return msgs, nil
}

func (m *Monitor) validateClient() error {
	if m == nil || m.client == nil {
		return errors.New("ilink monitor client is nil")
	}
	return nil
}

func (m *Monitor) poll(ctx context.Context) ([]InboundMessage, string, error) {
	cursor, err := m.loadCursor()
	if err != nil {
		return nil, "", err
	}

	resp, err := m.client.GetUpdates(ctx, cursor)
	if err != nil {
		return nil, "", err
	}

	if resp.Ret != 0 {
		if resp.ErrCode == sessionInvalidErrCode {
			if err := m.persistCursor(""); err != nil {
				return nil, "", err
			}
		}
		return nil, "", fmt.Errorf("ilink getupdates failed: errcode=%d errmsg=%s", resp.ErrCode, strings.TrimSpace(resp.ErrMsg))
	}

	nextCursor := ""
	if resp.GetUpdatesBuf != "" && resp.GetUpdatesBuf != cursor {
		nextCursor = resp.GetUpdatesBuf
	}

	filtered := filterFinishedUserTextMessages(resp.Msgs)
	return filtered, nextCursor, nil
}

func filterFinishedUserTextMessages(msgs []InboundMessage) []InboundMessage {
	out := make([]InboundMessage, 0, len(msgs))
	for _, msg := range msgs {
		if msg.MessageType != MessageTypeUser || msg.MessageState != MessageStateFinish {
			continue
		}

		text := firstText(msg.ItemList)
		if text == "" {
			continue
		}

		msg.Text = text
		out = append(out, msg)
	}
	return out
}

func firstText(items []Item) string {
	for _, item := range items {
		if item.Type != ItemTypeText || item.TextItem == nil {
			continue
		}
		if strings.TrimSpace(item.TextItem.Text) == "" {
			continue
		}
		return item.TextItem.Text
	}
	return ""
}

func nextBackoff(consecutiveFailures int) time.Duration {
	if consecutiveFailures <= 1 {
		return 3 * time.Second
	}
	if consecutiveFailures == 2 {
		return 6 * time.Second
	}
	if consecutiveFailures == 3 {
		return 12 * time.Second
	}
	if consecutiveFailures == 4 {
		return 24 * time.Second
	}
	return 30 * time.Second
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (m *Monitor) loadCursor() (string, error) {
	if strings.TrimSpace(m.cursorPath) == "" {
		return "", nil
	}

	data, err := os.ReadFile(m.cursorPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read cursor file: %w", err)
	}

	var payload cursorFile
	if err := json.Unmarshal(data, &payload); err != nil {
		log.Printf("warning: invalid ilink cursor file %q, resetting cursor: %v", m.cursorPath, err)
		return "", nil
	}
	return payload.GetUpdatesBuf, nil
}

func (m *Monitor) persistCursor(cursor string) error {
	if strings.TrimSpace(m.cursorPath) == "" {
		return nil
	}

	dir := filepath.Dir(m.cursorPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create cursor directory: %w", err)
	}

	data, err := json.MarshalIndent(cursorFile{GetUpdatesBuf: cursor}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cursor file: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, "cursor.json.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp cursor file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("set temp cursor permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp cursor file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp cursor file: %w", err)
	}

	if err := os.Rename(tmpPath, m.cursorPath); err != nil {
		return fmt.Errorf("rename temp cursor file: %w", err)
	}
	if err := os.Chmod(m.cursorPath, 0o600); err != nil {
		return fmt.Errorf("set cursor file permissions: %w", err)
	}

	return nil
}
