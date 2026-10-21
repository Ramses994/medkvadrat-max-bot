package reminders

import "testing"

func TestCanNotify(t *testing.T) {
	one, zero := 1, 0
	if !CanNotify(&one, false, "Прием", false) {
		t.Fatal("consent 1 must send")
	}
	if CanNotify(&zero, false, "Прием", false) {
		t.Fatal("consent 0 must skip")
	}
	if CanNotify(nil, false, "Прием", false) {
		t.Fatal("null consent must skip")
	}
	if CanNotify(&one, true, "Прием", false) {
		t.Fatal("no_sms must skip")
	}
	if CanNotify(&one, false, "Лист ожидания", false) {
		t.Fatal("waitlist must skip")
	}
}
