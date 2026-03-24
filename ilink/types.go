package ilink

const (
	MessageTypeNone = 0
	MessageTypeUser = 1
	MessageTypeBot  = 2
)

const (
	MessageStateNew        = 0
	MessageStateGenerating = 1
	MessageStateFinish     = 2
)

const (
	ItemTypeNone = 0
	ItemTypeText = 1
)

type Credentials struct {
	BotToken    string `json:"bot_token"`
	ILinkBotID  string `json:"ilink_bot_id"`
	BaseURL     string `json:"baseurl"`
	ILinkUserID string `json:"ilink_user_id"`
}

type QRCodeResponse struct {
	QRCode           string `json:"qrcode"`
	QRCodeImgContent string `json:"qrcode_img_content"`
}

type QRStatusResponse struct {
	Status      string `json:"status"`
	BotToken    string `json:"bot_token"`
	ILinkBotID  string `json:"ilink_bot_id"`
	BaseURL     string `json:"baseurl"`
	ILinkUserID string `json:"ilink_user_id"`
}

type QRStatusRequest struct {
	QRCode string `json:"qrcode"`
}

type GetUpdatesRequest struct {
	GetUpdatesBuf string `json:"get_updates_buf"`
}

type GetUpdatesResponse struct {
	Ret           int              `json:"ret"`
	ErrCode       int              `json:"errcode,omitempty"`
	ErrMsg        string           `json:"errmsg,omitempty"`
	Msgs          []InboundMessage `json:"msgs,omitempty"`
	GetUpdatesBuf string           `json:"get_updates_buf,omitempty"`
}

type InboundMessage struct {
	FromUserID   string `json:"from_user_id"`
	ToUserID     string `json:"to_user_id,omitempty"`
	MessageType  int    `json:"message_type"`
	MessageState int    `json:"message_state"`
	ItemList     []Item `json:"item_list,omitempty"`
	ContextToken string `json:"context_token,omitempty"`
	Text         string `json:"-"`
}

type Item struct {
	Type     int       `json:"type"`
	TextItem *TextItem `json:"text_item,omitempty"`
}

type TextItem struct {
	Text string `json:"text"`
}

type SendMessageRequest struct {
	ToUserID     string `json:"-"`
	ContextToken string `json:"-"`
	Text         string `json:"-"`
}

type sendMessagePayload struct {
	Msg sendMessageBody `json:"msg"`
}

type sendMessageBody struct {
	ToUserID     string `json:"to_user_id"`
	ContextToken string `json:"context_token,omitempty"`
	MessageType  int    `json:"message_type"`
	ItemList     []Item `json:"item_list"`
}

type SendMessageResponse struct {
	Ret    int    `json:"ret"`
	ErrMsg string `json:"errmsg,omitempty"`
}
