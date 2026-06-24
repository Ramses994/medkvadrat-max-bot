package apptconfirm

import "testing"

func TestParsePayload_OK(t *testing.T) {
	cases := []struct {
		in        string
		action    string
		planningID int64
	}{
		{"confirm:11737097", "confirm", 11737097},
		{"reschedule:42", "reschedule", 42},
		{"DECLINE:99", "decline", 99},
	}
	for _, c := range cases {
		action, id, ok := ParsePayload(c.in)
		if !ok || action != c.action || id != c.planningID {
			t.Fatalf("ParsePayload(%q) = (%q, %d, %v), want (%q, %d, true)", c.in, action, id, ok, c.action, c.planningID)
		}
	}
}

func TestParsePayload_Invalid(t *testing.T) {
	for _, in := range []string{"", "bad", "confirm:abc", "confirm:", ":1", "confirm:0"} {
		if _, _, ok := ParsePayload(in); ok {
			t.Fatalf("expected invalid: %q", in)
		}
	}
}

func TestActionToStatus(t *testing.T) {
	m := map[string]string{
		"confirm": "confirmed", "decline": "declined", "reschedule": "reschedule",
	}
	for action, want := range m {
		got, ok := ActionToStatus(action)
		if !ok || got != want {
			t.Fatalf("ActionToStatus(%q) = (%q, %v)", action, got, ok)
		}
	}
	if _, ok := ActionToStatus("maybe"); ok {
		t.Fatal("unknown action should fail")
	}
}
