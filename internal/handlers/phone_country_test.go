package handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
)

func TestNormalizePhone_CountryCode(t *testing.T) {
	const want = "79265906994"
	for _, raw := range []string{"+47 1234 5678", "+4712345678", "+1 926 590-69-94", "0079265906994"} {
		if got := normalizePhone(raw); got != "" {
			t.Fatalf("%q normalized to %q", raw, got)
		}
	}
	for _, raw := range []string{"+7 (926) 590-69-94", "8 926 590 69 94", "9265906994"} {
		if got := normalizePhone(raw); got != want {
			t.Fatalf("%q = %q", raw, got)
		}
	}
}

func TestContact_ForeignNumberIsNotFoundWithoutSearch(t *testing.T) {
	gw := &fakeGW{patients: []gateway.Patient{{PatientID: 100, FullName: "Иванов Иван", BirthDate: "1985-01-01"}}}
	h, st := testHandler(t, []int64{100}, gw)
	vcf := "TEL:+4712345678"
	payload := contactJSON(vcf, hmacHex(testBotToken, vcf), 42)
	if err := h.OnMessageCreated(context.Background(), contactUpdate(42, payload)); err != nil {
		t.Fatal(err)
	}
	if gw.searchCalls != 0 {
		t.Fatalf("search calls=%d", gw.searchCalls)
	}
	if link, _ := st.GetByUserID(42); link != nil && link.Verified() {
		t.Fatal("foreign number was linked")
	}
	fm := h.max.(*fakeMax)
	if len(fm.messages) != 1 || !strings.Contains(fm.messages[0], "не нашли") {
		t.Fatalf("messages=%v", fm.messages)
	}
}
