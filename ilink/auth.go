package ilink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiongzhe/weCodex/config"
)

var (
	ErrQRCodeExpired = errors.New("qr code expired")
)

const defaultQRPollInterval = 2 * time.Second

func FetchQRCode(ctx context.Context, baseURL string) (QRCodeResponse, error) {
	client := NewUnauthenticatedClient()
	if normalized := normalizeBaseURL(baseURL); normalized != "" {
		client.baseURL = normalized
	}

	var resp QRCodeResponse
	if err := client.doPost(ctx, "/ilink/bot/get_bot_qrcode?bot_type=3", struct{}{}, &resp); err != nil {
		return QRCodeResponse{}, err
	}
	return resp, nil
}

func PollQRStatus(ctx context.Context, baseURL string, qrCode string, onStatus func(string)) (Credentials, error) {
	return pollQRStatus(ctx, baseURL, qrCode, onStatus, defaultQRPollInterval)
}

func pollQRStatus(ctx context.Context, baseURL string, qrCode string, onStatus func(string), pollInterval time.Duration) (Credentials, error) {
	client := NewUnauthenticatedClient()
	if normalized := normalizeBaseURL(baseURL); normalized != "" {
		client.baseURL = normalized
	}

	for {
		var resp QRStatusResponse
		err := client.doPost(ctx, "/ilink/bot/get_qrcode_status", QRStatusRequest{QRCode: qrCode}, &resp)
		if err != nil {
			if ctx.Err() != nil {
				return Credentials{}, ctx.Err()
			}
			return Credentials{}, err
		}

		if onStatus != nil {
			onStatus(resp.Status)
		}

		switch resp.Status {
		case "confirmed":
			creds := Credentials{
				BotToken:    resp.BotToken,
				ILinkBotID:  resp.ILinkBotID,
				BaseURL:     resp.BaseURL,
				ILinkUserID: resp.ILinkUserID,
			}
			if missing := missingCredentialFields(creds); len(missing) > 0 {
				return Credentials{}, fmt.Errorf("confirmed status missing required credentials: %s", strings.Join(missing, ", "))
			}
			return creds, nil
		case "expired":
			return Credentials{}, ErrQRCodeExpired
		case "waiting", "wait", "scaned":
			// Transitional states; continue polling.
		default:
			return Credentials{}, fmt.Errorf("unknown QR status: %q", resp.Status)
		}

		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return Credentials{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func missingCredentialFields(creds Credentials) []string {
	var missing []string
	if strings.TrimSpace(creds.BotToken) == "" {
		missing = append(missing, "bot_token")
	}
	if strings.TrimSpace(creds.ILinkBotID) == "" {
		missing = append(missing, "ilink_bot_id")
	}
	if strings.TrimSpace(creds.BaseURL) == "" {
		missing = append(missing, "baseurl")
	}
	if strings.TrimSpace(creds.ILinkUserID) == "" {
		missing = append(missing, "ilink_user_id")
	}
	return missing
}

func SaveCredentials(cfg config.Config, creds Credentials) error {
	path, err := config.CredentialsPath(cfg)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create credentials directory: %w", err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credentials file: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, "account.json.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp credentials file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("set temp credentials permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp credentials file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp credentials file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp credentials file: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("set credentials file permissions: %w", err)
	}

	return nil
}

func LoadCredentials(cfg config.Config) (Credentials, error) {
	path, err := config.CredentialsPath(cfg)
	if err != nil {
		return Credentials{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Credentials{}, os.ErrNotExist
		}
		return Credentials{}, fmt.Errorf("read credentials file: %w", err)
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return Credentials{}, fmt.Errorf("decode credentials file: %w", err)
	}

	return creds, nil
}

func normalizeBaseURL(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if trimmed == "" {
		return ""
	}
	return trimmed
}
