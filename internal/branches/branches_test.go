package branches

import "testing"

func TestShortLabel_Kurkino(t *testing.T) {
	if got := ShortLabel(3, "Куркино"); got != "Куркино" {
		t.Fatalf("got %q", got)
	}
}

func TestShortLabel_FallbackCode(t *testing.T) {
	if got := ShortLabel(0, "Неизвестный"); got != "Неизвестный" {
		t.Fatalf("got %q", got)
	}
}
