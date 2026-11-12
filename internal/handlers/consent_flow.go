package handlers

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/consenttext"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
)

type consentAPI interface {
	GetConsent(ctx context.Context, patientID int64) (gateway.ConsentView, error)
	PostConsent(ctx context.Context, patientID, maxUserID int64, kind, version, action string) error
}

func (h *Handler) maybeConsent(ctx context.Context, chatID, userID, patientID int64) error {
	if !h.ConsentEnabled || h.Consent == nil || patientID <= 0 {
		return nil
	}
	view, err := h.Consent.GetConsent(ctx, patientID)
	if err != nil {
		log.Printf("consent read user=%d patient=%d: %v", userID, patientID, err)
		return nil
	}
	if view.MessengerNotify != nil || view.Service != "none" {
		return nil
	}
	return h.sendServiceConsent(ctx, chatID, patientID)
}

func (h *Handler) sendServiceConsent(ctx context.Context, chatID, patientID int64) error {
	rows := [][]maxclient.CallbackButton{{
		{Type: "callback", Text: "Согласен", Payload: fmt.Sprintf("consent:service:accept:%d", patientID), Intent: "positive"},
		{Type: "callback", Text: "Не сейчас", Payload: fmt.Sprintf("consent:service:later:%d", patientID), Intent: "negative"},
	}}
	return h.max.SendMessageWithKeyboard(ctx, chatID, false, consenttext.ServiceText, rows)
}

func (h *Handler) sendAdsConsent(ctx context.Context, userID, patientID int64) error {
	rows := [][]maxclient.CallbackButton{{
		{Type: "callback", Text: "Да, хочу", Payload: fmt.Sprintf("consent:ads:accept:%d", patientID), Intent: "positive"},
		{Type: "callback", Text: "Нет, спасибо", Payload: fmt.Sprintf("consent:ads:later:%d", patientID), Intent: "negative"},
	}}
	return h.max.SendMessageWithKeyboard(ctx, userID, true, consenttext.AdsText, rows)
}

func (h *Handler) onConsentCallback(ctx context.Context, callbackID string, userID int64, payload string) error {
	parts := strings.Split(payload, ":")
	if len(parts) != 4 || parts[0] != "consent" {
		return h.max.AnswerCallback(ctx, callbackID, "Не удалось обработать нажатие")
	}
	kind, action := parts[1], parts[2]
	patientID, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || patientID <= 0 {
		return h.max.AnswerCallback(ctx, callbackID, "Не удалось обработать нажатие")
	}
	link, err := h.storage.GetByUserID(userID)
	if err != nil || !link.Verified() {
		return h.max.AnswerCallback(ctx, callbackID, "Сначала подтвердите номер контактом")
	}
	if link.PatientID != patientID {
		return h.max.AnswerCallback(ctx, callbackID, "Это согласие относится к другому профилю")
	}
	if action == "accept" && ((kind == "service" && !h.ConsentEnabled) || (kind == "ads" && !h.AdsEnabled)) {
		return h.max.AnswerCallback(ctx, callbackID, "Сейчас это недоступно")
	}
	if h.Consent == nil {
		return h.max.AnswerCallback(ctx, callbackID, "Не удалось обработать нажатие")
	}
	version := h.ServiceVersion
	if version == "" {
		version = consenttext.ServiceVersion
	}
	if kind == "ads" {
		version = h.AdsVersion
		if version == "" {
			version = consenttext.AdsVersion
		}
	}
	note := ""
	switch {
	case kind == "service" && action == "later":
		note = "Хорошо, напоминания приходить не будут. Включить — командой „Включить уведомления“."
	case kind == "ads" && action == "later":
		note = "Хорошо, сообщения об акциях приходить не будут."
	case action == "accept" && (kind == "service" || kind == "ads"):
		view, err := h.Consent.GetConsent(ctx, link.PatientID)
		if err != nil {
			log.Printf("consent read user=%d patient=%d: %v", userID, link.PatientID, err)
			return h.max.AnswerCallback(ctx, callbackID, "Не удалось сохранить, попробуйте позже")
		}
		already := (kind == "service" && view.Service == "active" && view.ServiceVersion == version) || (kind == "ads" && view.Ads == "active" && view.AdsVersion == version)
		if !already {
			if err := h.Consent.PostConsent(ctx, link.PatientID, userID, kind, version, "accept"); err != nil {
				log.Printf("consent accept user=%d patient=%d kind=%s: %v", userID, link.PatientID, kind, err)
				return h.max.AnswerCallback(ctx, callbackID, "Не удалось сохранить, попробуйте позже")
			}
		}
		if kind == "service" {
			note = "Спасибо! Будем напоминать о приёмах. Отключить — командой „Отключить уведомления“."
		} else {
			note = "Спасибо! Сообщения об акциях можно отключить командой „Отписаться от акций“."
		}
	default:
		return h.max.AnswerCallback(ctx, callbackID, "Не удалось обработать нажатие")
	}
	if err := h.max.AnswerCallbackReplace(ctx, callbackID, note, note); err != nil {
		return h.max.AnswerCallback(ctx, callbackID, note)
	}
	if h.AdsEnabled && kind == "service" {
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
		return true, h.sendServiceConsent(ctx, chatID, link.PatientID)
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
	if err := h.Consent.PostConsent(ctx, link.PatientID, userID, kind, version, "revoke"); err != nil {
		log.Printf("consent revoke user=%d patient=%d kind=%s: %v", userID, link.PatientID, kind, err)
		return h.max.SendMessage(ctx, chatID, "Не удалось сохранить, попробуйте позже")
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
