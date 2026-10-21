package reminders

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/branches"
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

func appointmentBody(branchID int, branchCode, doctorName, service string, appt time.Time) string {
	loc := MoscowLocation()
	t := appt.In(loc)
	month := monthsRuGenitive[int(t.Month())]
	var b strings.Builder
	fmt.Fprintf(&b, "📅 %d %s, %02d:%02d\n", t.Day(), month, t.Hour(), t.Minute())
	if locText := branches.DisplayLine(branchID, branchCode); locText != "" {
		fmt.Fprintf(&b, "📍 %s\n", locText)
	}
	if s := strings.TrimSpace(service); s != "" {
		fmt.Fprintf(&b, "🩺 %s\n", s)
	}
	if d := strings.TrimSpace(doctorName); d != "" {
		fmt.Fprintf(&b, "👨‍⚕️ %s\n", d)
	}
	return strings.TrimRight(b.String(), "\n")
}

func FormatMessage(branchID int, branchCode, doctorName, service string, appt time.Time) string {
	return fmt.Sprintf(
		"Напоминание о приёме в клинике МедКвадрат.\n\n%s\n\nЕсли планы изменились, свяжитесь с регистратурой: %s",
		appointmentBody(branchID, branchCode, doctorName, service, appt),
		registryPhone,
	)
}

func FormatBookingNotice(branchID int, branchCode, doctorName, service string, appt time.Time) string {
	return fmt.Sprintf(
		"Вы записаны на приём в клинике МедКвадрат.\n\n%s\n\nНапомним накануне. Если планы изменятся, позвоните в регистратуру: %s",
		appointmentBody(branchID, branchCode, doctorName, service, appt),
		registryPhone,
	)
}

func ConfirmationKeyboard(planningID int64, apptVersion string) [][]maxclient.CallbackButton {
	id := strconv.FormatInt(planningID, 10)
	suf := id
	if strings.TrimSpace(apptVersion) != "" {
		suf = id + ":" + apptVersion
	}
	return [][]maxclient.CallbackButton{
		{
			{Type: "callback", Text: "✅ Приду", Payload: "confirm:" + suf, Intent: "positive"},
			{Type: "callback", Text: "❌ Не приду", Payload: "decline:" + suf, Intent: "negative"},
		},
		{
			{Type: "callback", Text: "🔄 Перенести", Payload: "reschedule:" + suf, Intent: "default"},
		},
	}
}
