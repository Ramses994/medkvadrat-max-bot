package apptconfirm

import "testing"

func TestParsePayload_OK(t *testing.T) {
	action, id, ver, ok := ParsePayload("confirm:11737097")
	if !ok || action != "confirm" || id != 11737097 || ver != "" {
		t.Fatalf("old format %q %d %q %v", action, id, ver, ok)
	}
	action, id, ver, ok = ParsePayload("decline:42:abc123def456")
	if !ok || action != "decline" || id != 42 || ver != "abc123def456" {
		t.Fatalf("new format %q %d %q %v", action, id, ver, ok)
	}
}

func TestParsePayload_Invalid(t *testing.T) {
	for _, in := range []string{"", "bad", "confirm:abc", "confirm:", ":1", "confirm:0"} {
		if _, _, _, ok := ParsePayload(in); ok {
			t.Fatalf("expected invalid: %q", in)
		}
	}
}

func TestActionToStatus(t *testing.T) {
	got, ok := ActionToStatus("confirm")
	if !ok || got != "confirmed" {
		t.Fatal(got, ok)
	}
}
