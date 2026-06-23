package maxclient

// CallbackButton is an inline keyboard button (type=callback).
// Shape matches github.com/max-messenger/max-bot-api-client-go/schemes.CallbackButton.
type CallbackButton struct {
	Type    string `json:"type"` // "callback"
	Text    string `json:"text"`
	Payload string `json:"payload"`
	Intent  string `json:"intent,omitempty"` // default | positive | negative
}

// InlineKeyboardAttachment is the inline_keyboard attachment request body.
type InlineKeyboardAttachment struct {
	Type    string `json:"type"` // inline_keyboard
	Payload struct {
		Buttons [][]CallbackButton `json:"buttons"`
	} `json:"payload"`
}

func newInlineKeyboardAttachment(rows [][]CallbackButton) InlineKeyboardAttachment {
	var att InlineKeyboardAttachment
	att.Type = "inline_keyboard"
	att.Payload.Buttons = rows
	return att
}

type outboundMessage struct {
	Text        string        `json:"text"`
	Attachments []interface{} `json:"attachments,omitempty"`
}

type callbackAnswerBody struct {
	Notification string `json:"notification,omitempty"`
}
