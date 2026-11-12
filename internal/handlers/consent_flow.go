package handlers

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/consenttext"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
)

type consentAPI interface {
	GetConsent(ctx context.Context, patientID int64) (gateway.ConsentView, error)
	PostConsent(ctx context.Context, patientID, maxUserID int64, kind, version, action string, expectedRevision *int64) error
}

func (h *Handler) maybeConsent(ctx context.Context, chatID, userID, patientID int64) error {
	if !h.ConsentEnabled || h.Consent == nil || patientID <= 0 {
		return nil
	}
	view, err := h.Consent.GetConsent(ctx, patientID)
	if err != nil {
		log.Printf("consent read user=%d patient=%d: %v", userID, patientID, err)
		return h.max.SendMessage(ctx, chatID, "Не удалось, попробуйте позже")
	}
	if view.MessengerNotify != nil || view.Service != "none" {
		return nil
	}
	return h.sendServiceConsent(ctx, chatID, userID, patientID)
}

const consentStale = "Это сообщение устарело. Отправьте „Включить уведомления“"

func (h *Handler) sendServiceConsent(ctx context.Context, chatID, userID, patientID int64) error {
	view, err := h.Consent.GetConsent(ctx, patientID)
	if err != nil {
		log.Printf("consent read user=%d patient=%d: %v", userID, patientID, err)
		return h.max.SendMessage(ctx, chatID, "Не удалось, попробуйте позже")
	}
	nonce, err := h.storage.SaveConsentPrompt(userID, patientID, "service", h.serviceVersion(), view.ServiceRevision, h.today())
	if err != nil {
		return err
	}
	rows := [][]maxclient.CallbackButton{{
		{Type: "callback", Text: "Согласен", Payload: "consent:" + nonce + ":accept", Intent: "positive"},
		{Type: "callback", Text: "Не сейчас", Payload: "consent:" + nonce + ":later", Intent: "negative"},
	}}
	return h.max.SendMessageWithKeyboard(ctx, chatID, false, consenttext.ServiceText, rows)
}

func (h *Handler) sendAdsConsent(ctx context.Context, userID, patientID int64) error {
	view, err := h.Consent.GetConsent(ctx, patientID)
	if err != nil {
		log.Printf("consent read user=%d patient=%d: %v", userID, patientID, err)
		return h.max.SendMessageWithKeyboard(ctx, userID, true, "Не удалось, попробуйте позже", nil)
	}
	nonce, err := h.storage.SaveConsentPrompt(userID, patientID, "ads", h.adsVersion(), view.AdsRevision, h.today())
	if err != nil {
		return err
	}
	rows := [][]maxclient.CallbackButton{{
		{Type: "callback", Text: "Да, хочу", Payload: "consent:" + nonce + ":accept", Intent: "positive"},
		{Type: "callback", Text: "Нет, спасибо", Payload: "consent:" + nonce + ":later", Intent: "negative"},
	}}
	return h.max.SendMessageWithKeyboard(ctx, userID, true, consenttext.AdsText, rows)
}

func (h *Handler) onConsentCallback(ctx context.Context, callbackID string, userID int64, payload string) error {
	parts := strings.Split(payload, ":")
	if len(parts) != 3 || parts[0] != "consent" || parts[2] != "accept" && parts[2] != "later" {
		return h.max.AnswerCallback(ctx, callbackID, consentStale)
	}
	action := parts[2]
	link, err := h.storage.GetByUserID(userID)
	if err != nil || !link.Verified() {
		return h.max.AnswerCallback(ctx, callbackID, consentStale)
	}
	prompt, used, err := h.storage.LookupConsentPrompt(parts[1])
	if err != nil {
		log.Printf("consent prompt user=%d: %v", userID, err)
		return h.max.AnswerCallback(ctx, callbackID, "Не удалось сохранить, попробуйте позже")
	}
	version := h.serviceVersion()
	if prompt != nil && prompt.Kind == "ads" {
		version = h.adsVersion()
	}
	if prompt == nil || used || !prompt.HasRevision || prompt.UserID != userID || prompt.PatientID != link.PatientID || prompt.TextVersion != version || h.today().Sub(prompt.CreatedAt) > 30*24*time.Hour {
		return h.max.AnswerCallback(ctx, callbackID, consentStale)
	}
	if (prompt.Kind == "service" && !h.ConsentEnabled) || (prompt.Kind == "ads" && !h.AdsEnabled) || h.Consent == nil {
		return h.max.AnswerCallback(ctx, callbackID, consentStale)
	}
	marked, err := h.storage.MarkConsentPromptUsed(parts[1], h.today())
	if err != nil || !marked {
		return h.max.AnswerCallback(ctx, callbackID, consentStale)
	}
	note := ""
	switch {
	case prompt.Kind == "service" && action == "later":
		note = "Хорошо, напоминания приходить не будут. Включить — командой „Включить уведомления“."
	case prompt.Kind == "ads" && action == "later":
		note = "Хорошо, сообщения об акциях приходить не будут."
	case action == "accept":
		expected := prompt.Revision
		if err := h.Consent.PostConsent(ctx, link.PatientID, userID, prompt.Kind, prompt.TextVersion, "accept", &expected); err != nil {
			var ge *gateway.Error
			if errors.As(err, &ge) && ge.Status == 409 && ge.Code == "CONSENT_CHANGED" {
				return h.max.AnswerCallback(ctx, callbackID, consentStale)
			}
			log.Printf("consent accept user=%d patient=%d kind=%s: %v", userID, link.PatientID, prompt.Kind, err)
			return h.max.AnswerCallback(ctx, callbackID, "Не удалось сохранить, попробуйте позже")
		}
		if prompt.Kind == "service" {
			note = "Спасибо! Будем напоминать о приёмах. Отключить — командой „Отключить уведомления“."
		} else {
			note = "Спасибо! Сообщения об акциях можно отключить командой „Отписаться от акций“."
		}
	default:
		return h.max.AnswerCallback(ctx, callbackID, consentStale)
	}
	if err := h.max.AnswerCallbackReplace(ctx, callbackID, note, note); err != nil {
		return h.max.AnswerCallback(ctx, callbackID, note)
	}
	if h.AdsEnabled && prompt.Kind == "service" && action == "accept" {
		return h.sendAdsConsent(ctx, userID, link.PatientID)
	}
	return nil
}

func (h *Handler) handleConsentCommand(ctx context.Context, chatID, userID int64, text string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "отключить уведомления":
		return true, h.revokeConsent(ctx, chatID, userID, "service", h.serviceVersion(), "Уведомления отключены. Включить — командой „Включить уведомления“.")
	case "отписаться от акций":
		return true, h.revokeConsent(ctx, chatID, userID, "ads", h.adsVersion(), "Сообщения об акциях отключены.")
	case "включить уведомления":
		link, err := h.storage.GetByUserID(userID)
		if err != nil || !link.Verified() {
			return true, h.askContact(ctx, chatID, contactGreeting)
		}
		if h.Consent != nil {
			view, err := h.Consent.GetConsent(ctx, link.PatientID)
			if err != nil {
				return true, h.max.SendMessage(ctx, chatID, "Не удалось проверить согласие. Попробуйте через минуту.")
			}
			if view.MessengerNotify != nil && *view.MessengerNotify == 0 {
				return true, h.max.SendMessage(ctx, chatID, "Чтобы включить уведомления, обратитесь в регистратуру.")
			}
		}
		if !h.ConsentEnabled {
			return true, h.max.SendMessage(ctx, chatID, "Включить уведомления в боте пока нельзя. Если отключили по ошибке — сообщите в регистратуру.")
		}
		if h.Consent == nil {
			return true, h.max.SendMessage(ctx, chatID, "Уведомления пока не настроены.")
		}
		return true, h.sendServiceConsent(ctx, chatID, userID, link.PatientID)
	default:
		return false, nil
	}
}

func (h *Handler) revokeConsent(ctx context.Context, chatID, userID int64, kind, version, note string) error {
	link, err := h.storage.GetByUserID(userID)
	if err != nil || !link.Verified() {
		return h.askContact(ctx, chatID, contactGreeting)
	}
	if h.Consent == nil {
		return h.max.SendMessage(ctx, chatID, "Не удалось сохранить, попробуйте позже")
	}
	if err := h.Consent.PostConsent(ctx, link.PatientID, userID, kind, version, "revoke", nil); err != nil {
		log.Printf("consent revoke user=%d patient=%d kind=%s: %v", userID, link.PatientID, kind, err)
		return h.max.SendMessage(ctx, chatID, "Не удалось сохранить, попробуйте позже")
	}
	if err := h.storage.ExpireConsentPrompts(link.PatientID, kind, h.today()); err != nil {
		log.Printf("consent prompts patient=%d kind=%s: %v", link.PatientID, kind, err)
	}
	return h.max.SendMessage(ctx, chatID, note)
}

func (h *Handler) serviceVersion() string {
	if h.ServiceVersion != "" {
		return h.ServiceVersion
	}
	return consenttext.ServiceVersion
}

func (h *Handler) adsVersion() string {
	if h.AdsVersion != "" {
		return h.AdsVersion
	}
	return consenttext.AdsVersion
}
