package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
)

func TestBirthDate_RequiredWhenAnotherAdultIsOutsideThePilot(t *testing.T) {
	gw := &fakeGW{patients: []gateway.Patient{
		{PatientID: 100, FullName: "Тест Тестовый", BirthDate: "1985-01-01"},
		{PatientID: 200, FullName: "Вне Списка", BirthDate: "1980-05-02"},
	}}
	h, st := testHandler(t, []int64{100}, gw)
	vcf := "TEL:+79991112233"
	if err := h.OnMessageCreated(context.Background(), contactUpdate(42, contactJSON(vcf, hmacHex(testBotToken, vcf), 42))); err != nil {
		t.Fatal(err)
	}
	if link, _ := st.GetByUserID(42); link.Verified() {
		t.Fatal("linked without the birth date")
	}
	say := func(text string) {
		t.Helper()
		err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
			Sender: &maxclient.User{UserID: 42}, Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"},
			Body: &maxclient.MessageBody{Text: text},
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	say("1")
	say("01.01.1985")
	link, _ := st.GetByUserID(42)
	if !link.Verified() || link.PatientID != 100 {
		t.Fatalf("link=%+v", link)
	}
}

func TestContactPhone_UsesTheSignedVCard(t *testing.T) {
	raw := `BEGIN:VCARD\r\nTEL:+79991112233`
	verified := "BEGIN:VCARD\nTEL:+79991112233"
	gw := &fakeGW{patients: []gateway.Patient{{PatientID: 100, FullName: "Тест Тестовый", BirthDate: "1985-01-01"}}}
	h, st := testHandler(t, []int64{100}, gw)
	if err := h.OnMessageCreated(context.Background(), contactUpdate(42, contactJSON(raw, hmacHex(testBotToken, verified), 42))); err != nil {
		t.Fatal(err)
	}
	link, _ := st.GetByUserID(42)
	if !link.Verified() || gw.searchCalls != 1 {
		t.Fatalf("link=%+v searches=%d", link, gw.searchCalls)
	}

	two := "TEL:+79991112233\nTEL:+79991112233" + `\r\n` + "TEL:+79990000000"
	gw2 := &fakeGW{patients: gw.patients}
	h2, st2 := testHandler(t, []int64{100}, gw2)
	signed := strings.ReplaceAll(two, `\r\n`, "\n")
	if err := h2.OnMessageCreated(context.Background(), contactUpdate(42, contactJSON(two, hmacHex(testBotToken, signed), 42))); err != nil {
		t.Fatal(err)
	}
	if link, _ := st2.GetByUserID(42); link.Verified() || gw2.searchCalls != 0 {
		t.Fatalf("two numbers link=%+v searches=%d", link, gw2.searchCalls)
	}
}

func TestBirthFailures_RestartAfterTheBlockEnds(t *testing.T) {
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	gw := &fakeGW{patients: []gateway.Patient{
		{PatientID: 10, FullName: "Тест Тестовый", BirthDate: "1985-01-01"},
		{PatientID: 20, FullName: "Мартынов Дмитрий", BirthDate: "1980-05-02"},
	}}
	h, st := testHandler(t, []int64{10, 20}, gw)
	h.Now = func() time.Time { return now }
	vcf := "TEL:+79991112233"
	send := func(text string) {
		t.Helper()
		var body *maxclient.MessageBody
		if text == "" {
			body = &maxclient.MessageBody{Attachments: []maxclient.Attachment{{Type: "contact", Payload: []byte(contactJSON(vcf, hmacHex(testBotToken, vcf), 42))}}}
		} else {
			body = &maxclient.MessageBody{Text: text}
		}
		err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
			Sender: &maxclient.User{UserID: 42}, Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"}, Body: body,
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	send("")
	send("1")
	send("02.02.1985")
	send("02.02.1985")
	send("02.02.1985")
	fm := h.max.(*fakeMax)
	if !strings.Contains(fm.messages[len(fm.messages)-1], "регистратуру") {
		t.Fatalf("block=%q", fm.messages[len(fm.messages)-1])
	}
	now = now.Add(24*time.Hour + time.Minute)
	send("")
	send("1")
	send("02.02.1985")
	last := fm.messages[len(fm.messages)-1]
	link, _ := st.GetByUserID(42)
	if link.Verified() || !strings.Contains(last, "Дата не совпадает") || strings.Contains(last, "регистратуру") {
		t.Fatalf("linked=%v msg=%q", link.Verified(), last)
	}
}

func TestConsentButton_CannotUndoAnotherAccountsRevoke(t *testing.T) {
	h, st := testHandler(t, []int64{100}, &fakeGW{})
	if err := st.LinkVerified(1, 100, "79991112233", "Аккаунт А", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := st.LinkVerified(2, 100, "79991112233", "Аккаунт Б", time.Now()); err != nil {
		t.Fatal(err)
	}
	cons := &scriptConsent{view: gateway.ConsentView{Service: "none"}}
	h.Consent = cons
	h.ConsentEnabled = true
	h.ServiceVersion = "service-v1"
	if err := h.sendServiceConsent(context.Background(), 1, 2, 100); err != nil {
		t.Fatal(err)
	}
	payload := h.max.(*fakeMax).keyboards[0].rows[0][0].Payload
	if err := cons.PostConsent(context.Background(), 100, 1, "service", "service-v1", "accept"); err != nil {
		t.Fatal(err)
	}
	err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
		Sender: &maxclient.User{UserID: 1}, Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"},
		Body: &maxclient.MessageBody{Text: "Отключить уведомления"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	before := len(cons.posts)
	if err := h.OnMessageCallback(context.Background(), dialogCallback(2, payload)); err != nil {
		t.Fatal(err)
	}
	if len(cons.posts) != before || cons.view.Service != "revoked" {
		t.Fatalf("posts=%v state=%s", cons.posts, cons.view.Service)
	}
}

func TestConsentButton_StaleWhenExpiryFails(t *testing.T) {
	h, st := testHandler(t, []int64{100}, &fakeGW{})
	if err := st.LinkVerified(1, 100, "79991112233", "Аккаунт А", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := st.LinkVerified(2, 100, "79991112233", "Аккаунт Б", time.Now()); err != nil {
		t.Fatal(err)
	}
	cons := &scriptConsent{view: gateway.ConsentView{Service: "none"}}
	h.Consent = cons
	h.ConsentEnabled = true
	h.ServiceVersion = "service-v1"
	if err := h.sendServiceConsent(context.Background(), 1, 2, 100); err != nil {
		t.Fatal(err)
	}
	payload := h.max.(*fakeMax).keyboards[0].rows[0][0].Payload
	st.FailConsentExpiry(errors.New("disk"))
	if err := cons.PostConsent(context.Background(), 100, 1, "service", "service-v1", "accept"); err != nil {
		t.Fatal(err)
	}
	err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
		Sender: &maxclient.User{UserID: 1}, Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"},
		Body: &maxclient.MessageBody{Text: "Отключить уведомления"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	before := len(cons.posts)
	if err := h.OnMessageCallback(context.Background(), dialogCallback(2, payload)); err != nil {
		t.Fatal(err)
	}
	fm := h.max.(*fakeMax)
	if len(cons.posts) != before || cons.view.Service != "revoked" || fm.answers[len(fm.answers)-1] != consentStale {
		t.Fatalf("posts=%v state=%s answers=%v", cons.posts, cons.view.Service, fm.answers)
	}
}
