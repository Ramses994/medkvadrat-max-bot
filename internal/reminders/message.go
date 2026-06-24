package reminders

import (
	"fmt"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/branches"
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

// FormatMessage builds a plain-text reminder (no inline keyboard — PR #3).
func FormatMessage(branchID int, branchCode, doctorName string, appt time.Time) string {
	loc := MoscowLocation()
	t := appt.In(loc)
	month := monthsRuGenitive[int(t.Month())]
	dateLine := fmt.Sprintf("📅 %d %s, %02d:%02d", t.Day(), month, t.Hour(), t.Minute())
	if label := branches.ShortLabel(branchID, branchCode); label != "" {
		dateLine += fmt.Sprintf(" (%s)", label)
	}
	doctorLine := "👨‍⚕️ " + doctorName
	return fmt.Sprintf(
		"Напоминание о приёме в клинике МедКвадрат.\n\n%s\n%s\n\nЕсли планы изменились, свяжитесь с регистратурой: %s",
		dateLine,
		doctorLine,
		registryPhone,
	)
}
