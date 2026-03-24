package ilink

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const defaultBaseURL = "https://ilinkai.weixin.qq.com"

type Client struct {
	baseURL    string
	botToken   string
	botID      string
	wechatUIN  string
	httpClient *http.Client
}

func NewClient(creds Credentials) *Client {
	baseURL := strings.TrimRight(creds.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	return &Client{
		baseURL:    baseURL,
		botToken:   creds.BotToken,
		botID:      creds.ILinkBotID,
		wechatUIN:  generateWechatUIN(),
		httpClient: &http.Client{},
	}
}

func NewUnauthenticatedClient() *Client {
	return &Client{
		baseURL:    defaultBaseURL,
		wechatUIN:  generateWechatUIN(),
		httpClient: &http.Client{},
	}
}

func (c *Client) GetUpdates(ctx context.Context, cursor string) (GetUpdatesResponse, error) {
	var resp GetUpdatesResponse
	if err := c.doPost(ctx, "/ilink/bot/getupdates", GetUpdatesRequest{GetUpdatesBuf: cursor}, &resp); err != nil {
		return GetUpdatesResponse{}, err
	}
	return resp, nil
}

func (c *Client) SendMessage(ctx context.Context, req SendMessageRequest) (SendMessageResponse, error) {
	payload := sendMessagePayload{
		Msg: sendMessageBody{
			ToUserID:     req.ToUserID,
			ContextToken: req.ContextToken,
			MessageType:  MessageTypeBot,
			ItemList: []Item{{
				Type:     ItemTypeText,
				TextItem: &TextItem{Text: req.Text},
			}},
		},
	}

	var resp SendMessageResponse
	if err := c.doPost(ctx, "/ilink/bot/sendmessage", payload, &resp); err != nil {
		return SendMessageResponse{}, err
	}
	return resp, nil
}

func (c *Client) doPost(ctx context.Context, path string, body any, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	if err := json.Unmarshal(respBody, result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-WECHAT-UIN", c.wechatUIN)
	if c.botToken != "" {
		c.setAuthenticatedHeaders(req)
	}
}

func (c *Client) setAuthenticatedHeaders(req *http.Request) {
	req.Header.Set("AuthorizationType", "ilink_bot_token")
	req.Header.Set("Authorization", "Bearer "+c.botToken)
}

func generateWechatUIN() string {
	var n uint32
	if err := binary.Read(rand.Reader, binary.LittleEndian, &n); err != nil {
		return base64.StdEncoding.EncodeToString([]byte("0"))
	}
	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%d", n)))
}
