package maxclient

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOutboundMessage_WithInlineKeyboard(t *testing.T) {
	rows := [][]CallbackButton{{
		{Type: "callback", Text: "Да", Payload: "test:yes", Intent: "positive"},
		{Type: "callback", Text: "Нет", Payload: "test:no", Intent: "negative"},
	}}
	msg := outboundMessage{
		Text:        "Тест кнопок",
		Attachments: []interface{}{newInlineKeyboardAttachment(rows)},
	}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`"type":"inline_keyboard"`,
		`"type":"callback"`,
		`"payload":"test:yes"`,
		`"intent":"positive"`,
		`"intent":"negative"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in %s", want, s)
		}
	}
}

func TestUpdate_MessageCallback_Unmarshal(t *testing.T) {
	raw := []byte(`{
		"update_type": "message_callback",
		"timestamp": 1739184000000,
		"callback": {
			"timestamp": 1739184000000,
			"callback_id": "cb-123",
			"payload": "test:yes",
			"user": {
				"user_id": 54321,
				"name": "User_Name"
			}
		},
		"message": {
			"recipient": {"chat_id": 100, "chat_type": "dialog", "user_id": 54321},
			"body": {"text": "Описание"}
		}
	}`)
	var u Update
	if err := json.Unmarshal(raw, &u); err != nil {
		t.Fatal(err)
	}
	if u.UpdateType != "message_callback" {
		t.Fatalf("update_type=%q", u.UpdateType)
	}
	if u.Callback == nil {
		t.Fatal("callback nil")
	}
	if u.Callback.CallbackID != "cb-123" {
		t.Fatalf("callback_id=%q", u.Callback.CallbackID)
	}
	if u.Callback.Payload != "test:yes" {
		t.Fatalf("payload=%q", u.Callback.Payload)
	}
	if u.Callback.User == nil || u.Callback.User.UserID != 54321 {
		t.Fatalf("user=%+v", u.Callback.User)
	}
}
