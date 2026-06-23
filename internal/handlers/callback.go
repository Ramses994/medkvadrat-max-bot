package handlers

import (
	"context"
	"fmt"
	"log"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/apptconfirm"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
)

func (h *Handler) OnMessageCallback(ctx context.Context, u *maxclient.Update) error {
	if u.Callback == nil {
		return nil
	}
	cb := u.Callback
	callbackID := cb.CallbackID
	answer := func(text string) error {
		return h.max.AnswerCallback(ctx, callbackID, text)
	}

	var userID int64
	if cb.User != nil {
		userID = cb.User.UserID
	}
	chatID := int64(0)
	if u.Message != nil && u.Message.Recipient != nil {
		chatID = u.Message.Recipient.ChatID
	}

	action, motconsuID, ok := apptconfirm.ParsePayload(cb.Payload)
	if !ok {
		log.Printf("message_callback bad payload chat_id=%d user_id=%d payload=%q", chatID, userID, cb.Payload)
		return answer("Не удалось обработать нажатие")
	}

	status, ok := apptconfirm.ActionToStatus(action)
	if !ok {
		log.Printf("message_callback unknown action chat_id=%d user_id=%d action=%q", chatID, userID, action)
		return answer("Не удалось обработать нажатие")
	}

	link, err := h.storage.GetByUserID(userID)
	if err != nil {
		log.Printf("message_callback storage user_id=%d: %v", userID, err)
		return answer("Произошла ошибка, попробуйте позже")
	}
	if link == nil {
		return answer("Профиль не найден, отправьте номер телефона")
	}

	if err := h.gateway.PostConfirmation(ctx, motconsuID, status, link.PatientID); err != nil {
		log.Printf("POST confirmations motconsu=%d status=%s patient=%d user=%d: %v",
			motconsuID, status, link.PatientID, userID, err)
		return answer("Не удалось сохранить, попробуйте позже или позвоните в регистратуру.")
	}
	log.Printf("POST confirmations ok motconsu=%d status=%s patient=%d user=%d",
		motconsuID, status, link.PatientID, userID)

	switch status {
	case "confirmed":
		return answer("Спасибо, ждём вас!")
	case "declined":
		return answer("Записали, что вы не придёте.")
	case "reschedule":
		return answer("Передали в регистратуру — с вами свяжутся для переноса.")
	default:
		return answer("Принято")
	}
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
