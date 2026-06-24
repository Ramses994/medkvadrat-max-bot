package maxclient

import "encoding/json"

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

// marshalOutboundBody builds the JSON body for POST /messages.
// Plain text uses the legacy shape {"text":"..."} with no attachments key.
func marshalOutboundBody(text string, rows [][]CallbackButton) ([]byte, error) {
	if len(rows) > 0 {
		msg := outboundMessage{
			Text:        text,
			Attachments: []interface{}{newInlineKeyboardAttachment(rows)},
		}
		return json.Marshal(msg)
	}
	return json.Marshal(sendMessageBody{Text: text})
}

type outboundMessage struct {
	Text        string        `json:"text"`
	Attachments []interface{} `json:"attachments,omitempty"`
}

type callbackAnswerBody struct {
	Notification string `json:"notification,omitempty"`
}
