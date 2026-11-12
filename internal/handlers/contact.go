package handlers

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
)

const (
	contactButton     = maxclient.ContactButtonText
	contactGreeting   = "Здравствуйте! Я бот сети клиник МедКвадрат.\n\nЧтобы смотреть анализы и получать сообщения, подтвердите свой номер кнопкой ниже."
	contactTypedPhone = "Чтобы подтвердить номер, нажмите кнопку „Поделиться контактом“ ниже"
	contactNotOwn     = "Отправьте, пожалуйста, свой контакт кнопкой ниже"
	contactNotFound   = "По этому номеру не нашли пациента. Обратитесь в регистратуру: +7 (499) 288-88-14"
)

func (h *Handler) today() time.Time {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		loc = time.FixedZone("MSK", 3*3600)
	}
	now := time.Now()
	if h.Now != nil {
		now = h.Now()
	}
	return now.In(loc)
}

func (h *Handler) askContact(ctx context.Context, chatID int64, text string) error {
	return h.max.SendContactRequest(ctx, chatID, text, contactButton)
}

func (h *Handler) handleContact(ctx context.Context, chatID, userID int64, msg *maxclient.Message) error {
	if h.ContactDebug && msg.Body != nil {
		log.Printf("contact shape user=%d %s", userID, maxclient.DescribeAttachments(msg.Body.Attachments))
	}
	phone, outcome := contactPhone(msg, userID)
	if outcome != "ok" {
		h.logLink(userID, 0, outcome)
		return h.askContact(ctx, chatID, contactNotOwn)
	}
	return h.handleAuthentication(ctx, chatID, userID, phone)
}

func contactPhone(msg *maxclient.Message, senderID int64) (string, string) {
	if msg == nil || msg.Body == nil || senderID <= 0 {
		return "", "contact_not_own"
	}
	for _, att := range msg.Body.Attachments {
		if att.Type != "contact" || len(att.Payload) == 0 {
			continue
		}
		var payload struct {
			VcfInfo string `json:"vcf_info"`
			MaxInfo *struct {
				UserID int64 `json:"user_id"`
			} `json:"max_info"`
		}
		if err := json.Unmarshal(att.Payload, &payload); err != nil || payload.MaxInfo == nil || payload.MaxInfo.UserID == 0 {
			return "", "contact_not_own"
		}
		if payload.MaxInfo.UserID != senderID {
			return "", "contact_not_own"
		}
		numbers := distinctPhones(payload.VcfInfo)
		if len(numbers) != 1 {
			return "", "contact_not_own"
		}
		return numbers[0], "ok"
	}
	return "", "contact_not_own"
}

func distinctPhones(vcf string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, line := range strings.Split(vcf, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		if !strings.HasPrefix(upper, "TEL") {
			continue
		}
		_, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		phone := normalizePhone(value)
		if phone == "" {
			continue
		}
		if _, ok := seen[phone]; ok {
			continue
		}
		seen[phone] = struct{}{}
		out = append(out, phone)
	}
	return out
}

func filterAdults(today time.Time, patients []gateway.Patient) []gateway.Patient {
	out := make([]gateway.Patient, 0, len(patients))
	for _, p := range patients {
		if adult(p.BirthDate, today) {
			out = append(out, p)
		}
	}
	return out
}

func adult(birth string, today time.Time) bool {
	birth = strings.TrimSpace(birth)
	if len(birth) < 10 {
		return false
	}
	born, err := time.ParseInLocation("2006-01-02", birth[:10], today.Location())
	if err != nil {
		return false
	}
	age := today.Year() - born.Year()
	if today.Month() < born.Month() || (today.Month() == born.Month() && today.Day() < born.Day()) {
		age--
	}
	return age >= 18
}

func profileLabel(full, birth string) string {
	year := ""
	if len(birth) >= 4 {
		year = birth[:4]
	}
	return initials(full) + ", " + year
}

func initials(full string) string {
	parts := strings.Fields(full)
	if len(parts) == 0 {
		return ""
	}
	n := len(parts)
	if n > 2 {
		n = 2
	}
	bits := make([]string, 0, n)
	for i := 0; i < n; i++ {
		r := []rune(parts[i])
		if len(r) == 0 {
			continue
		}
		bits = append(bits, string(r[0])+".")
	}
	return strings.Join(bits, " ")
}

func (h *Handler) logLink(userID, patientID int64, outcome string) {
	log.Printf("link user=%d patient=%d outcome=%s", userID, patientID, outcome)
}
