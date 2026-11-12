package handlers

import (
	"context"
	"fmt"
	"log"
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
	BotToken       string
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
	userID := int64(0)
	if u.User != nil {
		userID = u.User.UserID
	}
	payload := "нет"
	if strings.TrimSpace(u.Payload) != "" {
		payload = "есть"
	}
	log.Printf("bot_started user=%d payload=%s", userID, payload)

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

func dialogMessage(m *maxclient.Message) bool {
	return m != nil && m.Recipient != nil && m.Recipient.ChatType == "dialog"
}

func (h *Handler) OnMessageCreated(ctx context.Context, u *maxclient.Update) error {
	if u.Message == nil || u.Message.Body == nil || u.Message.Sender == nil || !dialogMessage(u.Message) {
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
	if blocked, err := h.storage.BirthBlocked(userID, h.today()); err != nil {
		return err
	} else if blocked {
		return h.max.SendMessage(ctx, chatID, "Обратитесь в регистратуру: "+registryPhone)
	}

	if _, _, _, _, ok, err := h.storage.PendingChoice(userID, h.today()); err != nil {
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
	if blocked, err := h.storage.BirthBlocked(userID, h.today()); err != nil {
		return err
	} else if blocked {
		return h.max.SendMessage(ctx, chatID, "Обратитесь в регистратуру: "+registryPhone)
	}
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

	allAdults := filterAdults(h.today(), found)
	adults := filterAllowedPatients(h.LinkAllow, allAdults)
	log.Printf("link attempt user=%d found=%d adults=%d allowed_adults=%d", userID, len(found), len(allAdults), len(adults))
	if len(found) == 0 {
		h.logLink(userID, 0, "no_patient")
		return h.max.SendMessage(ctx, chatID, contactNotFound)
	}
	if len(allAdults) >= 2 {
		if len(adults) == 0 {
			h.logLink(userID, 0, "not_allowed")
			return h.sendTestMode(ctx, chatID)
		}
		return h.askWhichAdult(ctx, chatID, userID, phone, adults)
	}
	patients := filterAllowedPatients(h.LinkAllow, found)
	if len(patients) == 0 {
		h.logLink(userID, 0, "not_allowed")
		return h.sendTestMode(ctx, chatID)
	}
	adults = filterAdults(h.today(), patients)
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
		return h.askWhichAdult(ctx, chatID, userID, phone, adults)
	}
}

func (h *Handler) askWhichAdult(ctx context.Context, chatID, userID int64, phone string, adults []gateway.Patient) error {
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

func sortPatients(ps []gateway.Patient) {
	sort.Slice(ps, func(i, j int) bool { return ps[i].PatientID < ps[j].PatientID })
}

func (h *Handler) handlePatientSelection(ctx context.Context, chatID, userID int64, phone, text string) error {
	if blocked, err := h.storage.BirthBlocked(userID, h.today()); err != nil {
		return err
	} else if blocked {
		return h.max.SendMessage(ctx, chatID, "Обратитесь в регистратуру: "+registryPhone)
	}
	savedPhone, ids, step, selectedID, ok, err := h.storage.PendingChoice(userID, h.today())
	if err != nil {
		log.Printf("pending choice user=%d: %v", userID, err)
		return h.max.SendMessage(ctx, chatID, "Ошибка связи с сервером. Попробуйте позже.")
	}
	if !ok || savedPhone != phone {
		_ = h.storage.Unlink(userID)
		return h.askContact(ctx, chatID, "Список устарел. "+contactTypedPhone)
	}
	trimmed := strings.TrimSpace(text)
	if step == "birth" {
		if strings.EqualFold(trimmed, "назад") {
			return h.reshowPendingAdults(ctx, chatID, userID, savedPhone)
		}
		if choice, err := strconv.Atoi(trimmed); err == nil && choice >= 1 && choice <= len(ids) {
			return h.beginBirthStep(ctx, chatID, userID, savedPhone, ids[choice-1])
		}
		return h.confirmBirth(ctx, chatID, userID, savedPhone, selectedID, trimmed)
	}
	choice, err := strconv.Atoi(trimmed)
	if err != nil || choice < 1 {
		return h.max.SendMessage(ctx, chatID, "Пожалуйста, отправьте просто цифру из списка (например, 1).")
	}
	if choice > len(ids) {
		return h.max.SendMessage(ctx, chatID, "Такого номера нет в списке. Пожалуйста, отправьте правильную цифру.")
	}
	return h.beginBirthStep(ctx, chatID, userID, savedPhone, ids[choice-1])
}

func (h *Handler) beginBirthStep(ctx context.Context, chatID, userID int64, phone string, selectedID int64) error {
	if err := h.storage.SaveBirthStep(userID, phone, selectedID, h.today()); err != nil {
		return fmt.Errorf("storage.SaveBirthStep: %w", err)
	}
	return h.askBirthDate(ctx, chatID, userID, phone, selectedID)
}

func (h *Handler) reshowPendingAdults(ctx context.Context, chatID, userID int64, phone string) error {
	found, err := h.gateway.SearchByPhone(ctx, phone)
	if err != nil {
		log.Printf("gateway.SearchByPhone user=%d: %v", userID, err)
		return h.max.SendMessage(ctx, chatID, "Ошибка связи с сервером. Попробуйте позже.")
	}
	adults := filterAdults(h.today(), filterAllowedPatients(h.LinkAllow, found))
	if len(adults) < 2 {
		_ = h.storage.DeletePendingChoice(userID)
		return h.max.SendMessage(ctx, chatID, "Этот профиль больше недоступен по этому номеру. Отправьте другую цифру или «Сменить номер».")
	}
	return h.askWhichAdult(ctx, chatID, userID, phone, adults)
}

func (h *Handler) askBirthDate(ctx context.Context, chatID, userID int64, phone string, selectedID int64) error {
	found, err := h.gateway.SearchByPhone(ctx, phone)
	if err != nil {
		log.Printf("gateway.SearchByPhone user=%d: %v", userID, err)
		return h.max.SendMessage(ctx, chatID, "Ошибка связи с сервером. Попробуйте позже.")
	}
	for _, p := range filterAllowedPatients(h.LinkAllow, found) {
		if int64(p.PatientID) != selectedID || !adult(p.BirthDate, h.today()) {
			continue
		}
		label := profileLabel(p.FullName, p.BirthDate)
		return h.max.SendMessage(ctx, chatID, fmt.Sprintf(
			"Введите дату рождения профиля „%s“ в формате ДД.ММ.ГГГГ, например 05.03.1981. Чтобы выбрать другой профиль — „Назад“",
			label))
	}
	// Profile may have disappeared between list and prompt; confirmBirth still validates.
	return h.max.SendMessage(ctx, chatID,
		"Введите дату рождения выбранного профиля в формате ДД.ММ.ГГГГ, например 05.03.1981. Чтобы выбрать другой профиль — „Назад“")
}

func (h *Handler) confirmBirth(ctx context.Context, chatID, userID int64, phone string, selectedID int64, text string) error {
	born, dateOK := parseBirthDate(text, h.today().Location())
	if !dateOK {
		return h.max.SendMessage(ctx, chatID, "Введите дату рождения цифрами, например 05.03.1981")
	}
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
		_ = h.storage.DeletePendingChoice(userID)
		return h.max.SendMessage(ctx, chatID, "Этот профиль больше недоступен по этому номеру. Отправьте другую цифру или «Сменить номер».")
	}
	if !adult(selected.BirthDate, h.today()) {
		_ = h.storage.DeletePendingChoice(userID)
		h.logLink(userID, 0, "minor_hidden")
		return h.max.SendMessage(ctx, chatID, contactNotFound)
	}
	want := ""
	if len(selected.BirthDate) >= 10 {
		want = selected.BirthDate[:10]
	}
	if born.Format("2006-01-02") != want {
		remaining, blocked, err := h.storage.NoteBirthFailure(userID, h.today())
		if err != nil {
			return err
		}
		if blocked {
			_ = h.storage.DeletePendingChoice(userID)
			return h.max.SendMessage(ctx, chatID, "Обратитесь в регистратуру: "+registryPhone)
		}
		return h.max.SendMessage(ctx, chatID, fmt.Sprintf("Дата не совпадает. Осталось попыток: %d", remaining))
	}
	if err := h.storage.LinkVerified(userID, selectedID, phone, selected.FullName, h.today()); err != nil {
		return fmt.Errorf("storage.Link final: %w", err)
	}
	if err := h.storage.DeletePendingChoice(userID); err != nil {
		return fmt.Errorf("storage.DeletePendingChoice: %w", err)
	}
	_ = h.storage.ClearBirthFailures(userID)
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

func parseBirthDate(text string, loc *time.Location) (time.Time, bool) {
	text = strings.TrimSpace(text)
	text = strings.NewReplacer("-", ".", "/", ".").Replace(text)
	parts := strings.Split(text, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	day, errDay := strconv.Atoi(parts[0])
	month, errMonth := strconv.Atoi(parts[1])
	year, errYear := strconv.Atoi(parts[2])
	if errDay != nil || errMonth != nil || errYear != nil {
		return time.Time{}, false
	}
	if year < 1000 || year > 9999 || month < 1 || month > 12 || day < 1 {
		return time.Time{}, false
	}
	born := time.Date(year, time.Month(month), day, 0, 0, 0, 0, loc)
	if born.Year() != year || int(born.Month()) != month || born.Day() != day {
		return time.Time{}, false
	}
	return born, true
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

// normalizePhone matches the cabinet rule: a leading plus is a country code,
// and only a Russian number becomes 7XXXXXXXXXX.
func normalizePhone(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "+") {
		body, ok := phoneDigits(s[1:])
		if !ok || len(body) != 11 || body[0] != '7' {
			return ""
		}
		return body
	}
	if strings.HasPrefix(s, "00") {
		return ""
	}
	body, ok := phoneDigits(s)
	if !ok {
		return ""
	}
	switch {
	case len(body) == 11 && (body[0] == '7' || body[0] == '8'):
		if body[0] == '8' {
			return "7" + body[1:]
		}
		return body
	case len(body) == 10:
		return "7" + body
	default:
		return ""
	}
}

func phoneDigits(s string) (string, bool) {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '-', '(', ')', '.':
			continue
		}
		if r < '0' || r > '9' {
			return "", false
		}
		b.WriteRune(r)
	}
	return b.String(), true
}
