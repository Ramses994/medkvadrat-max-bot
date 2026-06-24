package reminders

import (
	"strconv"
	"testing"
)

func TestConfirmationKeyboard_Payloads(t *testing.T) {
	rows := ConfirmationKeyboard(11737097)
	if len(rows) != 2 {
		t.Fatalf("rows=%d", len(rows))
	}
	want := map[string]string{
		"confirm:11737097":    "positive",
		"decline:11737097":    "negative",
		"reschedule:11737097": "default",
	}
	seen := 0
	for _, row := range rows {
		for _, btn := range row {
			if btn.Type != "callback" {
				t.Fatalf("type=%q", btn.Type)
			}
			intent, ok := want[btn.Payload]
			if !ok {
				t.Fatalf("unexpected payload %q", btn.Payload)
			}
			if btn.Intent != intent {
				t.Fatalf("payload %q intent=%q want %q", btn.Payload, btn.Intent, intent)
			}
			seen++
		}
	}
	if seen != len(want) {
		t.Fatalf("seen %d buttons", seen)
	}
}

func TestConfirmationKeyboard_LargeID(t *testing.T) {
	id := int64(9876543210)
	rows := ConfirmationKeyboard(id)
	payload := rows[0][0].Payload
	if payload != "confirm:"+strconv.FormatInt(id, 10) {
		t.Fatalf("payload=%q", payload)
	}
}
