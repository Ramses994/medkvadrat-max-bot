package branches

import "testing"

func TestDisplayLine_Kurkino(t *testing.T) {
	got := DisplayLine(3, "Куркино")
	want := "Куркино, г. Москва, ул. Ландышевая, 14к1"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDisplayLine_Kashirka(t *testing.T) {
	got := DisplayLine(106, "Каширка")
	if got != "Каширка, г. Москва, Каширское шоссе, 74к1" {
		t.Fatalf("got %q", got)
	}
}

func TestDisplayLine_FallbackCode(t *testing.T) {
	if got := DisplayLine(0, "Неизвестный"); got != "Неизвестный" {
		t.Fatalf("got %q", got)
	}
}
