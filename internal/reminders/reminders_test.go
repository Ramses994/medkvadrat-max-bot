package reminders

import (
	"strings"
	"testing"
	"time"
)

func msk(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, MoscowLocation())
}

func TestDueKinds(t *testing.T) {
	appt := msk(2026, time.June, 24, 10, 30)

	tests := []struct {
		name string
		now  time.Time
		want []Kind
	}{
		{
			name: "past appointment",
			now:  appt.Add(time.Minute),
			want: nil,
		},
		{
			name: "d3 at exact target",
			now:  appt.Add(-72 * time.Hour),
			want: []Kind{KindD3},
		},
		{
			name: "d3 within grace",
			now:  appt.Add(-72*time.Hour + 5*time.Hour),
			want: []Kind{KindD3},
		},
		{
			name: "d3 after grace",
			now:  appt.Add(-72*time.Hour + 7*time.Hour),
			want: nil,
		},
		{
			name: "d1 at exact target",
			now:  appt.Add(-24 * time.Hour),
			want: []Kind{KindD1},
		},
		{
			name: "d1 within grace",
			now:  appt.Add(-24*time.Hour + 5*time.Hour + 59*time.Minute),
			want: []Kind{KindD1},
		},
		{
			name: "d1 after grace",
			now:  appt.Add(-24*time.Hour + 6*time.Hour + time.Minute),
			want: nil,
		},
		{
			name: "h1 at exact target",
			now:  appt.Add(-1 * time.Hour),
			want: []Kind{KindH1},
		},
		{
			name: "h1 within grace",
			now:  appt.Add(-1*time.Hour + 19*time.Minute),
			want: []Kind{KindH1},
		},
		{
			name: "h1 after grace",
			now:  appt.Add(-1*time.Hour + 21*time.Minute),
			want: nil,
		},
		{
			name: "before any target",
			now:  appt.Add(-73 * time.Hour),
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DueKinds(appt, tc.now)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v want %v", got, tc.want)
				}
			}
		})
	}
}

func TestFormatMessage(t *testing.T) {
	text := FormatMessage(106, "Каширка", "Гусев П.", msk(2026, time.June, 24, 10, 30))
	if text == "" {
		t.Fatal("empty message")
	}
	if want := "24 июня, 10:30"; !strings.Contains(text, want) {
		t.Fatalf("missing %q in %q", want, text)
	}
	if want := "📍 Каширка, г. Москва, Каширское шоссе, 74к1"; !strings.Contains(text, want) {
		t.Fatalf("missing %q in %q", want, text)
	}
	if want := "Гусев П."; !strings.Contains(text, want) {
		t.Fatalf("missing %q in %q", want, text)
	}
	if strings.Contains(strings.ToLower(text), "ответьте 1") || strings.Contains(text, "1/2/3") {
		t.Fatalf("message must not mention 1/2/3: %q", text)
	}
}

func TestFormatMessage_KurkinoAddress(t *testing.T) {
	text := FormatMessage(3, "Куркино", "Гаевой Э.", msk(2026, time.June, 26, 19, 30))
	if !strings.Contains(text, "📍 Куркино, г. Москва, ул. Ландышевая, 14к1") {
		t.Fatalf("missing address in %q", text)
	}
}
