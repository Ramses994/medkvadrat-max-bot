package handlers

import (
	"context"
	"fmt"
	"log"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
)

// OnMessageCallback logs callback payload and acknowledges the button press.
// Business logic (appointment confirm/cancel) arrives in PR #3b.
func (h *Handler) OnMessageCallback(ctx context.Context, u *maxclient.Update) error {
	if u.Callback == nil {
		return nil
	}
	cb := u.Callback
	var userID int64
	if cb.User != nil {
		userID = cb.User.UserID
	}
	chatID := int64(0)
	if u.Message != nil && u.Message.Recipient != nil {
		chatID = u.Message.Recipient.ChatID
	}
	log.Printf("message_callback chat_id=%d user_id=%d callback_id=%s payload=%q",
		chatID, userID, cb.CallbackID, cb.Payload)
	return h.max.AnswerCallback(ctx, cb.CallbackID, "Принято")
}

func (h *Handler) sendKeyboardSmokeTest(ctx context.Context, userID int64) error {
	if userID == 0 {
		return fmt.Errorf("keyboard smoke: user_id is 0")
	}
	log.Printf("keyboard smoke: SendToUser user_id=%d", userID)
	rows := [][]maxclient.CallbackButton{{
		{Type: "callback", Text: "Да", Payload: "test:yes", Intent: "positive"},
		{Type: "callback", Text: "Нет", Payload: "test:no", Intent: "negative"},
	}}
	return h.max.SendMessageWithKeyboard(ctx, userID, true,
		"Тест inline-клавиатуры MAX (холодная доставка по user_id). Нажмите кнопку — в логах должен появиться message_callback.",
		rows)
}
