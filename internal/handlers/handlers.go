package handlers

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/storage"
)

const (
	registryPhone = "+7 (499) 288-88-14"
	labDaysBack   = 90
	maxPanelsShow = 5
)

type Handler struct {
	max           *maxclient.Client
	gateway       *gateway.Client
	storage       *storage.Storage
	KeyboardDebug bool
}

func New(mc *maxclient.Client, gw *gateway.Client, st *storage.Storage) *Handler {
	return &Handler{max: mc, gateway: gw, storage: st}
}

// ===== Входные события =====

func (h *Handler) OnBotStarted(ctx context.Context, u *maxclient.Update) error {
	if u.Payload != "" {
		log.Printf("bot_started chat=%d payload=%q", u.ChatID, u.Payload)
	}

	if h.KeyboardDebug {
		return h.sendKeyboardSmokeTest(ctx, u.ChatID)
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
			return h.sendKeyboardSmokeTest(ctx, chatID)
		}
		return h.sendWelcome(ctx, chatID, link)
	}

	if h.KeyboardDebug && strings.EqualFold(text, "тест-кнопки") {
		return h.sendKeyboardSmokeTest(ctx, chatID)
	}

	// ЛОГИКА ОТВЯЗКИ НОМЕРА (работает на любом этапе)
	lowerText := strings.ToLower(text)
	if lowerText == "/logout" || lowerText == "сменить номер" || lowerText == "отвязать" {
		if link != nil {
			if err := h.storage.Unlink(userID); err != nil {
				log.Printf("ошибка отвязки user=%d: %v", userID, err)
				return h.max.SendMessage(ctx, chatID, "Произошла ошибка при отвязке номера. Попробуйте позже.")
			}
			log.Printf("пользователь %d отвязал номер", userID)
		}
		
		return h.max.SendMessage(ctx, chatID, 
			"Ваш профиль успешно отвязан 🔄\n\n" +
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

	patients, err := h.gateway.SearchByPhone(ctx, phone)
	if err != nil {
		log.Printf("gateway.SearchByPhone(%s): %v", phone, err)
		return h.max.SendMessage(ctx, chatID,
			"Не удалось связаться с системой клиники. Попробуйте через минуту.")
	}

	switch len(patients) {
	case 0:
		return h.max.SendMessage(ctx, chatID,
			"Пациент с таким номером не найден.\n"+
				"Проверьте номер или обратитесь в регистратуру: "+registryPhone)
	case 1:
		p := patients[0]
		if err := h.storage.Link(userID, int64(p.PatientID), phone, p.FullName); err != nil {
			return fmt.Errorf("storage.Link: %w", err)
		}
		log.Printf("идентификация: user=%d → patient=%d (%s)", userID, p.PatientID, p.FullName)
		return h.max.SendMessage(ctx, chatID, fmt.Sprintf(
			"Здравствуйте, %s!\n\n"+
				"Отправьте «Мои анализы», чтобы посмотреть последние результаты.",
			p.FullName))
	default:
		// Сохраняем "режим ожидания выбора" (patient_id = 0)
		if err := h.storage.Link(userID, 0, phone, "pending_selection"); err != nil {
			return fmt.Errorf("storage.Link pending: %w", err)
		}
		
		var b strings.Builder
		b.WriteString("По этому номеру зарегистрировано несколько пациентов.\n")
		b.WriteString("Отправьте цифру, чтобы выбрать, чьи данные вы хотите посмотреть:\n\n")
		
		for i, p := range patients {
			// Выводим: 1. Иванов Иван Иванович
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, p.FullName))
		}
		b.WriteString("\n(Если вы хотите ввести другой номер, напишите «Сменить номер»).")
		
		return h.max.SendMessage(ctx, chatID, b.String())
	}
}

func (h *Handler) handlePatientSelection(ctx context.Context, chatID, userID int64, phone, text string) error {
	// Пытаемся получить число из ответа пользователя
	choice, err := strconv.Atoi(text)
	if err != nil || choice < 1 {
		return h.max.SendMessage(ctx, chatID, "Пожалуйста, отправьте просто цифру из списка (например, 1).")
	}

	// Снова запрашиваем список (это надежнее и быстрее, чем хранить массивы в БД)
	patients, err := h.gateway.SearchByPhone(ctx, phone)
	if err != nil {
		return h.max.SendMessage(ctx, chatID, "Ошибка связи с сервером. Попробуйте позже.")
	}

	if choice > len(patients) {
		return h.max.SendMessage(ctx, chatID, "Такого номера нет в списке. Пожалуйста, отправьте правильную цифру.")
	}

	// Пользователь выбрал корректного пациента
	selected := patients[choice-1]
	if err := h.storage.Link(userID, int64(selected.PatientID), phone, selected.FullName); err != nil {
		return fmt.Errorf("storage.Link final: %w", err)
	}

	log.Printf("идентификация завершена: user=%d выбрал patient=%d (%s)", userID, selected.PatientID, selected.FullName)
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