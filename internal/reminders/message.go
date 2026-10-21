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

func DoctorLine(doctorName, specialty string) string {
	line := "👨‍⚕️ " + doctorName
	if strings.TrimSpace(specialty) != "" {
		line += " — " + specialty
	}
	return line
}

// FormatMessage builds reminder text with branch address (keyboard attached separately in runner).
func FormatMessage(branchID int, branchCode, doctorName, specialty string, appt time.Time) string {
	loc := MoscowLocation()
	t := appt.In(loc)
	month := monthsRuGenitive[int(t.Month())]
	dateLine := fmt.Sprintf("📅 %d %s, %02d:%02d", t.Day(), month, t.Hour(), t.Minute())
	var locationLine string
	if locText := branches.DisplayLine(branchID, branchCode); locText != "" {
		locationLine = "📍 " + locText + "\n"
	}
	doctorLine := DoctorLine(doctorName, specialty)
	return fmt.Sprintf(
		"Напоминание о приёме в клинике МедКвадрат.\n\n%s\n%s%s\n\nЕсли планы изменились, свяжитесь с регистратурой: %s",
		dateLine,
		locationLine,
		doctorLine,
		registryPhone,
	)
}

func FormatBookingNotice(branchID int, branchCode, doctorName, specialty string, appt time.Time) string {
	loc := MoscowLocation()
	t := appt.In(loc)
	month := monthsRuGenitive[int(t.Month())]
	dateLine := fmt.Sprintf("📅 %d %s, %02d:%02d", t.Day(), month, t.Hour(), t.Minute())
	var locationLine string
	if locText := branches.DisplayLine(branchID, branchCode); locText != "" {
		locationLine = "📍 " + locText + "\n"
	}
	return fmt.Sprintf(
		"Вы записаны на приём в клинике МедКвадрат.\n\n%s\n%s%s\n\nНапомним накануне. Если планы изменятся, позвоните в регистратуру: %s",
		dateLine,
		locationLine,
		DoctorLine(doctorName, specialty),
		registryPhone,
	)
}

// ConfirmationKeyboard returns inline buttons for appointment confirmation (PR #3b-bot).
func ConfirmationKeyboard(planningID int64) [][]maxclient.CallbackButton {
	id := strconv.FormatInt(planningID, 10)
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
