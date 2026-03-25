package ilink

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetUpdatesSendsExpectedHeadersAndBody(t *testing.T) {
	var gotContentType string
	var gotAuthType string
	var gotAuth string
	var gotWechatUIN string
	var gotBuf string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		gotContentType = r.Header.Get("Content-Type")
		gotAuthType = r.Header.Get("AuthorizationType")
		gotAuth = r.Header.Get("Authorization")
		gotWechatUIN = r.Header.Get("X-WECHAT-UIN")

		var req GetUpdatesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotBuf = req.GetUpdatesBuf

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":0,"get_updates_buf":"cursor-2"}`))
	}))
	defer server.Close()

	creds := Credentials{
		BotToken:    "bot-token",
		ILinkBotID:  "bot-id",
		BaseURL:     server.URL,
		ILinkUserID: "user-id",
	}

	client := NewClient(creds)
	_, err := client.GetUpdates(context.Background(), "cursor-1")
	if err != nil {
		t.Fatalf("get updates: %v", err)
	}

	if gotContentType != "application/json" {
		t.Fatalf("expected content type header, got %q", gotContentType)
	}
	if gotAuthType != "ilink_bot_token" {
		t.Fatalf("expected AuthorizationType header, got %q", gotAuthType)
	}
	if gotAuth != "Bearer bot-token" {
		t.Fatalf("expected bearer token, got %q", gotAuth)
	}
	if gotWechatUIN == "" {
		t.Fatalf("expected X-WECHAT-UIN header to be set")
	}
	if gotBuf != "cursor-1" {
		t.Fatalf("expected cursor body, got %q", gotBuf)
	}
}

func TestNewClientFallsBackToDefaultBaseURL(t *testing.T) {
	client := NewClient(Credentials{BotToken: "bot-token"})
	if client.baseURL != "https://ilinkai.weixin.qq.com" {
		t.Fatalf("expected default base URL, got %q", client.baseURL)
	}
}

func TestSendMessageUsesWeclawStyleEnvelope(t *testing.T) {
	var gotBaseInfo BaseInfo
	var gotFromUserID string
	var gotClientID string
	var gotMessageType int
	var gotMessageState int
	var gotContextToken string
	var gotText string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req struct {
			BaseInfo BaseInfo `json:"base_info"`
			Msg struct {
				FromUserID   string    `json:"from_user_id"`
				ToUserID     string    `json:"to_user_id"`
				ClientID     string    `json:"client_id"`
				MessageType  int       `json:"message_type"`
				MessageState int       `json:"message_state"`
				ContextToken string    `json:"context_token"`
				ItemList     []Item    `json:"item_list"`
			} `json:"msg"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotBaseInfo = req.BaseInfo
		gotFromUserID = req.Msg.FromUserID
		gotClientID = req.Msg.ClientID
		gotMessageType = req.Msg.MessageType
		gotMessageState = req.Msg.MessageState
		gotContextToken = req.Msg.ContextToken
		if len(req.Msg.ItemList) > 0 && req.Msg.ItemList[0].TextItem != nil {
			gotText = req.Msg.ItemList[0].TextItem.Text
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	defer server.Close()

	client := NewClient(Credentials{BotToken: "bot-token", ILinkBotID: "bot-id", BaseURL: server.URL})
	_, err := client.SendMessage(context.Background(), SendMessageRequest{
		ToUserID:     "wx-user",
		ContextToken: "ctx-1",
		Text:         "hello",
	})
	if err != nil {
		t.Fatalf("send message: %v", err)
	}

	if gotFromUserID != "bot-id" {
		t.Fatalf("expected from_user_id to use bot id, got %q", gotFromUserID)
	}
	if gotClientID == "" {
		t.Fatalf("expected client_id to be set")
	}
	if gotMessageType != MessageTypeBot {
		t.Fatalf("expected message_type bot, got %d", gotMessageType)
	}
	if gotMessageState != MessageStateFinish {
		t.Fatalf("expected message_state finish, got %d", gotMessageState)
	}
	if gotContextToken != "ctx-1" {
		t.Fatalf("expected context token reuse, got %q", gotContextToken)
	}
	if gotText != "hello" {
		t.Fatalf("expected text payload, got %q", gotText)
	}
	_ = gotBaseInfo
}

func TestSendMessageUsesContextTokenAndTextOnlyPayload(t *testing.T) {
	var gotPath string
	var gotContextToken string
	var gotFirstItemType int
	var gotFirstItemText string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		gotPath = r.URL.Path

		var req struct {
			Msg struct {
				ContextToken string `json:"context_token"`
				ItemList     []Item `json:"item_list"`
			} `json:"msg"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		gotContextToken = req.Msg.ContextToken
		if len(req.Msg.ItemList) > 0 {
			gotFirstItemType = req.Msg.ItemList[0].Type
			if req.Msg.ItemList[0].TextItem != nil {
				gotFirstItemText = req.Msg.ItemList[0].TextItem.Text
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	defer server.Close()

	creds := Credentials{BotToken: "bot-token", BaseURL: server.URL}
	client := NewClient(creds)

	_, err := client.SendMessage(context.Background(), SendMessageRequest{
		ToUserID:     "wx-user",
		ContextToken: "ctx-1",
		Text:         "hello",
	})
	if err != nil {
		t.Fatalf("send message: %v", err)
	}

	if gotPath != "/ilink/bot/sendmessage" {
		t.Fatalf("expected sendmessage endpoint, got %q", gotPath)
	}
	if gotContextToken != "ctx-1" {
		t.Fatalf("expected context token reuse, got %q", gotContextToken)
	}
	if gotFirstItemType != ItemTypeText || gotFirstItemText != "hello" {
		t.Fatalf("expected plain-text payload, got type=%d text=%q", gotFirstItemType, gotFirstItemText)
	}
}

func TestSendMessageReturnsBusinessErrorWhenRetNonZero(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":1,"errmsg":"invalid context token"}`))
	}))
	defer server.Close()

	client := NewClient(Credentials{BotToken: "bot-token", BaseURL: server.URL})
	_, err := client.SendMessage(context.Background(), SendMessageRequest{
		ToUserID:     "wx-user",
		ContextToken: "ctx-bad",
		Text:         "hello",
	})
	if err == nil {
		t.Fatalf("expected send message business error when ret != 0")
	}
	if !strings.Contains(err.Error(), "sendmessage failed") {
		t.Fatalf("expected sendmessage failed error, got %v", err)
	}
	if !strings.Contains(err.Error(), "invalid context token") {
		t.Fatalf("expected errmsg to be included, got %v", err)
	}
}

func TestDoPostWithUnauthenticatedClientOmitsAuthHeaders(t *testing.T) {
	var gotContentType string
	var gotAuthType string
	var gotAuth string
	var gotWechatUIN string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		gotContentType = r.Header.Get("Content-Type")
		gotAuthType = r.Header.Get("AuthorizationType")
		gotAuth = r.Header.Get("Authorization")
		gotWechatUIN = r.Header.Get("X-WECHAT-UIN")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	defer server.Close()

	client := NewUnauthenticatedClient()
	client.baseURL = server.URL

	var resp struct {
		Ret int `json:"ret"`
	}
	if err := client.doPost(context.Background(), "/ilink/qrcode", map[string]string{"scene": "login"}, &resp); err != nil {
		t.Fatalf("do post: %v", err)
	}

	if gotContentType != "application/json" {
		t.Fatalf("expected content type header, got %q", gotContentType)
	}
	if gotAuthType != "" {
		t.Fatalf("expected AuthorizationType header to be omitted, got %q", gotAuthType)
	}
	if gotAuth != "" {
		t.Fatalf("expected Authorization header to be omitted, got %q", gotAuth)
	}
	if gotWechatUIN == "" {
		t.Fatalf("expected X-WECHAT-UIN header to be set")
	}
}

func TestDoPostReturnsTransportErrorForNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(Credentials{BotToken: "bot-token", BaseURL: server.URL})
	_, err := client.GetUpdates(context.Background(), "cursor-1")
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("expected non-200 error, got %v", err)
	}
}
