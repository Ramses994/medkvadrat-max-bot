package reminders

import "testing"

func TestCanNotify(t *testing.T) {
	one, zero := 1, 0
	if !CanNotify(&one, false, false, false) {
		t.Fatal("consent 1 must send")
	}
	if CanNotify(&zero, false, false, false) {
		t.Fatal("consent 0 must skip")
	}
	if CanNotify(nil, false, false, false) {
		t.Fatal("null consent must skip")
	}
	if CanNotify(&one, true, false, false) {
		t.Fatal("no_sms must skip")
	}
	if CanNotify(&one, false, true, false) {
		t.Fatal("is_waitlist must skip")
	}
}
