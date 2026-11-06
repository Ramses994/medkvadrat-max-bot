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
	max           maxAPI
	gateway       gatewayAPI
	storage       *storage.Storage
	LinkAllow     linkallow.Set
	KeyboardDebug bool
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
	if text == "" {
		return nil
	}

	userID := u.Message.Sender.UserID
	chatID := extractChatID(u.Message)

	link, err := h.storage.GetByUserID(userID)
	if err != nil {
		return fmt.Errorf("storage.GetByUserID: %w", err)
	}

	// /start works always
	if strings.HasPrefix(strings.ToLower(text), "/start") {
		if h.KeyboardDebug {
			return h.sendKeyboardSmokeTest(ctx, userID)
		}
		return h.sendWelcome(ctx, chatID, link)
	}

	if h.KeyboardDebug && strings.EqualFold(text, "тест-кнопки") {
		return h.sendKeyboardSmokeTest(ctx, userID)
	}

	// ЛОГИКА ОТВЯЗКИ НОМЕРА (работает на любом этапе)
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

		return h.max.SendMessage(ctx, chatID,
			"Ваш профиль успешно отвязан 🔄\n\n"+
				"Чтобы продолжить работу, отправьте новый номер телефона в формате +79991234567")
	}

	// Если нет привязки вообще -> Запускаем поиск по номеру
	if link == nil {
		return h.handleAuthentication(ctx, chatID, userID, text)
	}

	// Если привязка есть, но PatientID = 0 -> Значит мы ждем выбора из списка
	if link.PatientID == 0 {
		return h.handlePatientSelection(ctx, chatID, userID, link.Phone, text)
	}

	// Если полноценная привязка есть -> Обрабатываем рабочие команды
	return h.handleAuthenticated(ctx, chatID, link, text)
}

// ===== Приветствие =====

func (h *Handler) sendWelcome(ctx context.Context, chatID int64, link *storage.UserLink) error {
	if link != nil && link.PatientID != 0 {
		if !h.LinkAllow.Allowed(link.PatientID) {
			return h.sendTestMode(ctx, chatID)
		}
		return h.max.SendMessage(ctx, chatID, fmt.Sprintf(
			"Здравствуйте, %s!\n"+
				"Отправьте «Мои анализы», чтобы посмотреть последние результаты.\n"+
				"(Для смены профиля отправьте «Сменить номер»)",
			link.FullName))
	}
	return h.max.SendMessage(ctx, chatID,
		"Здравствуйте! Я бот сети клиник МедКвадрат.\n\n"+
			"Я умею:\n"+
			"• показывать результаты анализов\n"+
			"• напоминать о визитах (скоро)\n\n"+
			"Для начала отправьте ваш номер телефона, "+
			"по которому вы зарегистрированы в клинике.\n"+
			"Формат: +79991234567")
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
	switch len(patients) {
	case 0:
		return h.sendTestMode(ctx, chatID)
	case 1:
		p := patients[0]
		if err := h.storage.Link(userID, int64(p.PatientID), phone, p.FullName); err != nil {
			return fmt.Errorf("storage.Link: %w", err)
		}
		_ = h.storage.DeletePendingChoice(userID)
		log.Printf("идентификация: user=%d patient=%d", userID, p.PatientID)
		return h.max.SendMessage(ctx, chatID, fmt.Sprintf(
			"Здравствуйте, %s!\n\n"+
				"Отправьте «Мои анализы», чтобы посмотреть последние результаты.",
			p.FullName))
	default:
		sortPatients(patients)
		ids := make([]int64, len(patients))
		for i, p := range patients {
			ids[i] = int64(p.PatientID)
		}
		if err := h.storage.SavePendingChoice(userID, phone, ids, time.Now()); err != nil {
			return fmt.Errorf("storage.SavePendingChoice: %w", err)
		}
		if err := h.storage.Link(userID, 0, phone, "pending_selection"); err != nil {
			_ = h.storage.DeletePendingChoice(userID)
			return fmt.Errorf("storage.Link pending: %w", err)
		}

		var b strings.Builder
		b.WriteString("По этому номеру зарегистрировано несколько пациентов.\n")
		b.WriteString("Отправьте цифру, чтобы выбрать, чьи данные вы хотите посмотреть:\n\n")

		for i, p := range patients {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, p.FullName))
		}
		b.WriteString("\n(Если вы хотите ввести другой номер, напишите «Сменить номер»).")

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

	savedPhone, ids, ok, err := h.storage.PendingChoice(userID, time.Now())
	if err != nil {
		log.Printf("pending choice user=%d: %v", userID, err)
		return h.max.SendMessage(ctx, chatID, "Ошибка связи с сервером. Попробуйте позже.")
	}
	if !ok || savedPhone != phone {
		_ = h.storage.Unlink(userID)
		return h.max.SendMessage(ctx, chatID, "Список устарел, отправьте номер телефона ещё раз")
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
	if err := h.storage.Link(userID, selectedID, phone, selected.FullName); err != nil {
		return fmt.Errorf("storage.Link final: %w", err)
	}
	if err := h.storage.DeletePendingChoice(userID); err != nil {
		return fmt.Errorf("storage.DeletePendingChoice: %w", err)
	}

	log.Printf("идентификация завершена: user=%d patient=%d", userID, selectedID)
	return h.max.SendMessage(ctx, chatID, fmt.Sprintf(
		"Вы успешно авторизовались как %s! ✅\n\n"+
			"Отправьте «Мои анализы», чтобы посмотреть результаты.\n"+
			"(Для выбора другого профиля отправьте «Сменить номер»)",
		selected.FullName))
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
	if !h.LinkAllow.Allowed(link.PatientID) {
		return h.sendTestMode(ctx, chatID)
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
