package handlers

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/linkallow"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/storage"
)

const (
	registryPhone   = "+7 (499) 288-88-14"
	botTestModeText = "Бот пока работает в тестовом режиме. Обратитесь в регистратуру: +7 (499) 288-88-14"
	labDaysBack     = 90
	maxPanelsShow   = 5
)

type Handler struct {
	max            maxAPI
	gateway        gatewayAPI
	storage        *storage.Storage
	LinkAllow      linkallow.Set
	KeyboardDebug  bool
	ContactDebug   bool
	Consent        consentAPI
	ConsentEnabled bool
	AdsEnabled     bool
	ServiceVersion string
	AdsVersion     string
	Now            func() time.Time
}

func New(mc *maxclient.Client, gw *gateway.Client, st *storage.Storage) *Handler {
	return &Handler{max: mc, gateway: gw, storage: st}
}

func (h *Handler) sendTestMode(ctx context.Context, chatID int64) error {
	return h.max.SendMessage(ctx, chatID, botTestModeText)
}

func filterAllowedPatients(allow linkallow.Set, patients []gateway.Patient) []gateway.Patient {
	if len(patients) == 0 {
		return nil
	}
	out := make([]gateway.Patient, 0, len(patients))
	for _, p := range patients {
		if allow.Allowed(int64(p.PatientID)) {
			out = append(out, p)
		}
	}
	return out
}

// ===== Входные события =====

func (h *Handler) OnBotStarted(ctx context.Context, u *maxclient.Update) error {
	if u.Payload != "" {
		log.Printf("bot_started chat=%d payload=%q", u.ChatID, u.Payload)
	}

	if h.KeyboardDebug {
		userID := int64(0)
		if u.User != nil {
			userID = u.User.UserID
		}
		return h.sendKeyboardSmokeTest(ctx, userID)
	}

	var link *storage.UserLink
	if u.User != nil {
		link, _ = h.storage.GetByUserID(u.User.UserID)
	}
	return h.sendWelcome(ctx, u.ChatID, link)
}

func (h *Handler) OnMessageCreated(ctx context.Context, u *maxclient.Update) error {
	if u.Message == nil || u.Message.Body == nil || u.Message.Sender == nil {
		return nil
	}
	text := strings.TrimSpace(u.Message.Body.Text)
	userID := u.Message.Sender.UserID
	chatID := extractChatID(u.Message)

	if hasContact(u.Message) {
		return h.handleContact(ctx, chatID, userID, u.Message)
	}
	if text == "" {
		return nil
	}

	link, err := h.storage.GetByUserID(userID)
	if err != nil {
		return fmt.Errorf("storage.GetByUserID: %w", err)
	}

	if strings.HasPrefix(strings.ToLower(text), "/start") {
		if h.KeyboardDebug {
			return h.sendKeyboardSmokeTest(ctx, userID)
		}
		return h.sendWelcome(ctx, chatID, link)
	}

	if h.KeyboardDebug && strings.EqualFold(text, "тест-кнопки") {
		return h.sendKeyboardSmokeTest(ctx, userID)
	}

	lowerText := strings.ToLower(text)
	if lowerText == "/logout" || lowerText == "сменить номер" || lowerText == "отвязать" {
		if err := h.storage.DeletePendingChoice(userID); err != nil {
			log.Printf("pending choice delete user=%d: %v", userID, err)
			return h.max.SendMessage(ctx, chatID, "Произошла ошибка при отвязке номера. Попробуйте позже.")
		}
		if link != nil {
			if err := h.storage.Unlink(userID); err != nil {
				log.Printf("ошибка отвязки user=%d: %v", userID, err)
				return h.max.SendMessage(ctx, chatID, "Произошла ошибка при отвязке номера. Попробуйте позже.")
			}
			log.Printf("пользователь %d отвязал номер", userID)
		}
		return h.askContact(ctx, chatID, "Ваш профиль успешно отвязан 🔄\n\n"+contactGreeting)
	}

	if link.Verified() {
		if done, err := h.handleConsentCommand(ctx, chatID, userID, text); done {
			return err
		}
		return h.handleAuthenticated(ctx, chatID, link, text)
	}

	if _, _, ok, err := h.storage.PendingChoice(userID, h.today()); err != nil {
		return err
	} else if ok && normalizePhone(text) == "" {
		phone := ""
		if link != nil {
			phone = link.Phone
		}
		return h.handlePatientSelection(ctx, chatID, userID, phone, text)
	}

	if normalizePhone(text) != "" {
		return h.askContact(ctx, chatID, contactTypedPhone)
	}
	return h.askContact(ctx, chatID, contactGreeting)
}

func hasContact(m *maxclient.Message) bool {
	if m == nil || m.Body == nil {
		return false
	}
	for _, att := range m.Body.Attachments {
		if att.Type == "contact" {
			return true
		}
	}
	return false
}

// ===== Приветствие =====

func (h *Handler) sendWelcome(ctx context.Context, chatID int64, link *storage.UserLink) error {
	if link.Verified() {
		if !h.LinkAllow.Allowed(link.PatientID) {
			return h.sendTestMode(ctx, chatID)
		}
		return h.max.SendMessage(ctx, chatID, fmt.Sprintf(
			"Здравствуйте, %s!\n"+
				"Отправьте «Мои анализы», чтобы посмотреть последние результаты.\n"+
				"(Для смены профиля отправьте «Сменить номер»)",
			link.FullName))
	}
	return h.askContact(ctx, chatID, contactGreeting)
}

// ===== Авторизация и Выбор пациента =====

func (h *Handler) handleAuthentication(ctx context.Context, chatID, userID int64, text string) error {
	phone := normalizePhone(text)
	if phone == "" {
		return h.max.SendMessage(ctx, chatID,
			"Не похоже на номер телефона.\n"+
				"Пожалуйста, отправьте в формате +79991234567")
	}

	found, err := h.gateway.SearchByPhone(ctx, phone)
	if err != nil {
		log.Printf("gateway.SearchByPhone user=%d: %v", userID, err)
		return h.max.SendMessage(ctx, chatID,
			"Не удалось связаться с системой клиники. Попробуйте через минуту.")
	}

	patients := filterAllowedPatients(h.LinkAllow, found)
	log.Printf("link attempt user=%d found=%d allowed=%d", userID, len(found), len(patients))
	if len(found) == 0 {
		h.logLink(userID, 0, "no_patient")
		return h.max.SendMessage(ctx, chatID, contactNotFound)
	}
	if len(patients) == 0 {
		h.logLink(userID, 0, "not_allowed")
		return h.sendTestMode(ctx, chatID)
	}
	adults := filterAdults(h.today(), patients)
	if len(adults) == 0 {
		h.logLink(userID, 0, "minor_hidden")
		return h.max.SendMessage(ctx, chatID, contactNotFound)
	}
	switch len(adults) {
	case 1:
		p := adults[0]
		if err := h.storage.LinkVerified(userID, int64(p.PatientID), phone, p.FullName, h.today()); err != nil {
			return fmt.Errorf("storage.Link: %w", err)
		}
		_ = h.storage.DeletePendingChoice(userID)
		h.logLink(userID, int64(p.PatientID), "linked")
		if err := h.max.SendMessage(ctx, chatID, fmt.Sprintf(
			"Здравствуйте, %s!\n\n"+
				"Отправьте «Мои анализы», чтобы посмотреть последние результаты.",
			p.FullName)); err != nil {
			return err
		}
		return h.maybeConsent(ctx, chatID, userID, int64(p.PatientID))
	default:
		sortPatients(adults)
		ids := make([]int64, len(adults))
		for i, p := range adults {
			ids[i] = int64(p.PatientID)
		}
		if err := h.storage.SavePendingChoice(userID, phone, ids, h.today()); err != nil {
			return fmt.Errorf("storage.SavePendingChoice: %w", err)
		}
		if err := h.storage.Link(userID, 0, phone, ""); err != nil {
			_ = h.storage.DeletePendingChoice(userID)
			return fmt.Errorf("storage.Link pending: %w", err)
		}

		var b strings.Builder
		b.WriteString("По этому номеру зарегистрировано несколько пациентов.\n")
		b.WriteString("Отправьте цифру, чтобы выбрать свой профиль:\n\n")
		for i, p := range adults {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, profileLabel(p.FullName, p.BirthDate)))
		}
		b.WriteString("\n(Если вы хотите подтвердить другой номер, нажмите «Сменить номер»).")
		h.logLink(userID, 0, "several")
		return h.max.SendMessage(ctx, chatID, b.String())
	}
}

func sortPatients(ps []gateway.Patient) {
	sort.Slice(ps, func(i, j int) bool { return ps[i].PatientID < ps[j].PatientID })
}

func (h *Handler) handlePatientSelection(ctx context.Context, chatID, userID int64, phone, text string) error {
	choice, err := strconv.Atoi(text)
	if err != nil || choice < 1 {
		return h.max.SendMessage(ctx, chatID, "Пожалуйста, отправьте просто цифру из списка (например, 1).")
	}

	savedPhone, ids, ok, err := h.storage.PendingChoice(userID, h.today())
	if err != nil {
		log.Printf("pending choice user=%d: %v", userID, err)
		return h.max.SendMessage(ctx, chatID, "Ошибка связи с сервером. Попробуйте позже.")
	}
	if !ok || savedPhone != phone {
		_ = h.storage.Unlink(userID)
		return h.askContact(ctx, chatID, "Список устарел. "+contactTypedPhone)
	}
	if choice > len(ids) {
		return h.max.SendMessage(ctx, chatID, "Такого номера нет в списке. Пожалуйста, отправьте правильную цифру.")
	}
	selectedID := ids[choice-1]

	found, err := h.gateway.SearchByPhone(ctx, phone)
	if err != nil {
		log.Printf("gateway.SearchByPhone user=%d: %v", userID, err)
		return h.max.SendMessage(ctx, chatID, "Ошибка связи с сервером. Попробуйте позже.")
	}
	var selected *gateway.Patient
	for _, p := range filterAllowedPatients(h.LinkAllow, found) {
		if int64(p.PatientID) == selectedID {
			cp := p
			selected = &cp
			break
		}
	}
	if selected == nil || !h.LinkAllow.Allowed(selectedID) {
		return h.max.SendMessage(ctx, chatID, "Этот профиль больше недоступен по этому номеру. Отправьте другую цифру или «Сменить номер».")
	}
	if err := h.storage.LinkVerified(userID, selectedID, phone, selected.FullName, h.today()); err != nil {
		return fmt.Errorf("storage.Link final: %w", err)
	}
	if err := h.storage.DeletePendingChoice(userID); err != nil {
		return fmt.Errorf("storage.DeletePendingChoice: %w", err)
	}

	h.logLink(userID, selectedID, "linked")
	if err := h.max.SendMessage(ctx, chatID, fmt.Sprintf(
		"Вы успешно авторизовались как %s! ✅\n\n"+
			"Отправьте «Мои анализы», чтобы посмотреть результаты.\n"+
			"(Для выбора другого профиля отправьте «Сменить номер»)",
		selected.FullName)); err != nil {
		return err
	}
	return h.maybeConsent(ctx, chatID, userID, selectedID)
}

// ===== Команды авторизованного пользователя =====

func (h *Handler) handleAuthenticated(ctx context.Context, chatID int64, link *storage.UserLink, text string) error {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "анализ"):
		return h.sendLabPanels(ctx, chatID, link)
	default:
		return h.max.SendMessage(ctx, chatID,
			"Пока я умею только отправлять результаты анализов.\n"+
				"Напишите «Мои анализы».\n"+
				"(Для отвязки текущего профиля напишите «Сменить номер»).")
	}
}

func (h *Handler) sendLabPanels(ctx context.Context, chatID int64, link *storage.UserLink) error {
	if !link.Verified() {
		return h.askContact(ctx, chatID, contactGreeting)
	}
	if !h.LinkAllow.Allowed(link.PatientID) {
		return h.sendTestMode(ctx, chatID)
	}
	found, err := h.gateway.SearchByPhone(ctx, link.Phone)
	if err != nil {
		log.Printf("gateway.SearchByPhone user patient=%d: %v", link.PatientID, err)
		return h.max.SendMessage(ctx, chatID, "Не удалось загрузить анализы. Попробуйте через минуту.")
	}
	still := false
	for _, p := range filterAdults(h.today(), filterAllowedPatients(h.LinkAllow, found)) {
		if int64(p.PatientID) == link.PatientID {
			still = true
			break
		}
	}
	if !still {
		_ = h.storage.Unlink(link.UserID)
		h.logLink(link.UserID, link.PatientID, "no_patient")
		return h.askContact(ctx, chatID, contactNotFound+"\n\n"+contactGreeting)
	}
	panels, err := h.gateway.GetLabPanels(ctx, int(link.PatientID), labDaysBack)
	if err != nil {
		log.Printf("gateway.GetLabPanels(patient=%d): %v", link.PatientID, err)
		return h.max.SendMessage(ctx, chatID,
			"Не удалось загрузить анализы. Попробуйте через минуту.")
	}
	if len(panels) == 0 {
		return h.max.SendMessage(ctx, chatID, fmt.Sprintf(
			"За последние %d дней результатов анализов не найдено.", labDaysBack))
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("🔬 Ваши анализы за последние %d дней:\n\n", labDaysBack))
	for i, p := range panels {
		if i >= maxPanelsShow {
			b.WriteString(fmt.Sprintf("\n...и ещё %d результатов", len(panels)-maxPanelsShow))
			break
		}
		name := panelDisplayName(&p)
		marker := "✓"
		if p.HasOutOfRange {
			marker = "⚠️"
		}
		b.WriteString(fmt.Sprintf("%s %s\n    %s · тестов: %d\n\n",
			marker, name, p.ReadyAt, p.TestsCount))
	}
	return h.max.SendMessage(ctx, chatID, b.String())
}

// ===== Хелперы =====

func panelDisplayName(p *gateway.LabPanel) string {
	if p.PanelName != "" {
		return p.PanelName
	}
	if len(p.Tests) > 0 && p.Tests[0].Name != "" {
		return p.Tests[0].Name
	}
	return "Результат исследования"
}

func extractChatID(m *maxclient.Message) int64 {
	if m.Recipient != nil && m.Recipient.ChatID != 0 {
		return m.Recipient.ChatID
	}
	if m.Sender != nil {
		return m.Sender.UserID
	}
	return 0
}

var nonDigits = regexp.MustCompile(`\D`)

func normalizePhone(raw string) string {
	digits := nonDigits.ReplaceAllString(raw, "")

	switch {
	case len(digits) == 11 && digits[0] == '8':
		digits = "7" + digits[1:]
	case len(digits) == 10:
		digits = "7" + digits
	}

	if len(digits) != 11 || digits[0] != '7' {
		return ""
	}
	return digits
}
