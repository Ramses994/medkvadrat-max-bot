package reminders

import (
	"fmt"
	"strconv"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
)

const registryPhone = "+7 (499) 288-88-14"

var monthsRuGenitive = [...]string{
	"",
	"января",
	"февраля",
	"марта",
	"апреля",
	"мая",
	"июня",
	"июля",
	"августа",
	"сентября",
	"октября",
	"ноября",
	"декабря",
}

// FormatMessage builds reminder text (keyboard attached separately in runner).
func FormatMessage(departmentLabel, doctorName string, appt time.Time) string {
	loc := MoscowLocation()
	t := appt.In(loc)
	month := monthsRuGenitive[int(t.Month())]
	dateLine := fmt.Sprintf("📅 %d %s, %02d:%02d", t.Day(), month, t.Hour(), t.Minute())
	if departmentLabel != "" {
		dateLine += fmt.Sprintf(" (%s)", departmentLabel)
	}
	doctorLine := "👨‍⚕️ " + doctorName
	return fmt.Sprintf(
		"Напоминание о приёме в клинике МедКвадрат.\n\n%s\n%s\n\nЕсли планы изменились, свяжитесь с регистратурой: %s",
		dateLine,
		doctorLine,
		registryPhone,
	)
}

// ConfirmationKeyboard returns inline buttons for appointment confirmation (PR #3b-bot).
func ConfirmationKeyboard(motconsuID int64) [][]maxclient.CallbackButton {
	id := strconv.FormatInt(motconsuID, 10)
	return [][]maxclient.CallbackButton{
		{
			{Type: "callback", Text: "✅ Приду", Payload: "confirm:" + id, Intent: "positive"},
			{Type: "callback", Text: "❌ Не приду", Payload: "decline:" + id, Intent: "negative"},
		},
		{
			{Type: "callback", Text: "🔄 Перенести", Payload: "reschedule:" + id, Intent: "default"},
		},
	}
}
