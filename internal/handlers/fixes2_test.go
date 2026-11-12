package handlers

import (
	"bytes"
	"context"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/storage"
)

func TestContactHash_OnlyASignedContactSearches(t *testing.T) {
	const vcf = "TEL;TYPE=CELL:+7 (999) 111-22-33"
	patient := gateway.Patient{PatientID: 100, FullName: "Иванов Иван", BirthDate: "1985-01-01"}
	cases := []struct {
		name   string
		body   string
		search bool
		linked bool
	}{
		{"signed", contactJSON(vcf, hmacHex(testBotToken, vcf), 42), true, true},
		{"wrong hash", contactJSON(vcf, strings.Repeat("ab", 32), 42), false, false},
		{"missing hash", contactJSON(vcf, "", 42), false, false},
		{"other vcard", contactJSON(vcf, hmacHex(testBotToken, "TEL:+70000000000"), 42), false, false},
		{"other token", contactJSON(vcf, hmacHex("other-token", vcf), 42), false, false},
		{"other user", contactJSON(vcf, hmacHex(testBotToken, vcf), 99), false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw := &fakeGW{patients: []gateway.Patient{patient}}
			h, st := testHandler(t, []int64{100}, gw)
			if err := h.OnMessageCreated(context.Background(), contactUpdate(42, tc.body)); err != nil {
				t.Fatal(err)
			}
			link, _ := st.GetByUserID(42)
			if tc.search != (gw.searchCalls == 1) || tc.linked != (link.Verified()) {
				t.Fatalf("search=%d linked=%v", gw.searchCalls, link.Verified())
			}
		})
	}
}

func TestGroupChat_DoesNotAnswerOrCallTheGateway(t *testing.T) {
	vcf := "TEL:+79991112233"
	gw := &fakeGW{patients: []gateway.Patient{{PatientID: 100, FullName: "Иванов Иван", BirthDate: "1985-01-01"}}, labPanels: []gateway.LabPanel{{PanelName: "CBC"}}}
	h, st := testHandler(t, []int64{100}, gw)
	contact := contactUpdate(42, contactJSON(vcf, hmacHex(testBotToken, vcf), 42))
	contact.Message.Recipient.ChatType = "chat"
	if err := h.OnMessageCreated(context.Background(), contact); err != nil {
		t.Fatal(err)
	}
	if err := st.LinkVerified(42, 100, "79991112233", "Иванов Иван", time.Now()); err != nil {
		t.Fatal(err)
	}
	err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
		Sender: &maxclient.User{UserID: 42}, Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "chat"},
		Body: &maxclient.MessageBody{Text: "Мои анализы"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	err = h.OnMessageCallback(context.Background(), &maxclient.Update{
		Message:  &maxclient.Message{Recipient: &maxclient.Recipient{ChatType: "chat"}},
		Callback: &maxclient.Callback{CallbackID: "cb", Payload: "decline:10", User: &maxclient.User{UserID: 42}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fm := h.max.(*fakeMax)
	if gw.searchCalls != 0 || gw.labCalls != 0 || len(fm.messages) != 0 || len(fm.answers) != 0 {
		t.Fatalf("search=%d labs=%d messages=%v answers=%v", gw.searchCalls, gw.labCalls, fm.messages, fm.answers)
	}
}

func TestBirthDate_ConfirmsTheChosenProfile(t *testing.T) {
	gw := &fakeGW{patients: []gateway.Patient{
		{PatientID: 10, FullName: "Тест Тестовый", BirthDate: "1985-01-01"},
		{PatientID: 20, FullName: "Мартынов Дмитрий", BirthDate: "1980-05-02"},
	}}
	h, st := testHandler(t, []int64{10, 20}, gw)
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
	vcf := "TEL:+79991112233"
	if err := h.OnMessageCreated(context.Background(), contactUpdate(42, contactJSON(vcf, hmacHex(testBotToken, vcf), 42))); err != nil {
		t.Fatal(err)
	}
	say("1")
	say("01.01.1985")
	link, _ := st.GetByUserID(42)
	if !link.Verified() || link.PatientID != 10 {
		t.Fatalf("link=%+v", link)
	}
	_, _, step, selected, ok, err := st.PendingChoice(42, time.Now())
	if err != nil || ok || step != "" || selected != 0 {
		t.Fatalf("pending left step=%s selected=%d ok=%v", step, selected, ok)
	}
}

func TestBirthDate_ThreeMissesBlockTheFourth(t *testing.T) {
	gw := &fakeGW{patients: []gateway.Patient{
		{PatientID: 10, FullName: "Тест Тестовый", BirthDate: "1985-01-01"},
		{PatientID: 20, FullName: "Мартынов Дмитрий", BirthDate: "1980-05-02"},
	}}
	h, st := testHandler(t, []int64{10, 20}, gw)
	vcf := "TEL:+79991112233"
	if err := h.OnMessageCreated(context.Background(), contactUpdate(42, contactJSON(vcf, hmacHex(testBotToken, vcf), 42))); err != nil {
		t.Fatal(err)
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
	for i := 0; i < 3; i++ {
		say("02.02.1985")
	}
	before := gw.searchCalls
	say("01.01.1985")
	link, _ := st.GetByUserID(42)
	fm := h.max.(*fakeMax)
	last := fm.messages[len(fm.messages)-1]
	if link.Verified() || gw.searchCalls != before || !strings.Contains(last, "регистратуру") || strings.Contains(last, "1985") {
		t.Fatalf("linked=%v searches=%d→%d msg=%q", link.Verified(), before, gw.searchCalls, last)
	}
}

func TestBirthDate_RefusesAProfileThatLeftOrIsAMinor(t *testing.T) {
	patients := []gateway.Patient{
		{PatientID: 10, FullName: "Тест Тестовый", BirthDate: "1985-01-01"},
		{PatientID: 20, FullName: "Мартынов Дмитрий", BirthDate: "1980-05-02"},
	}
	start := func(t *testing.T) (*Handler, *storage.Storage, *fakeGW) {
		t.Helper()
		gw := &fakeGW{patients: append([]gateway.Patient(nil), patients...)}
		h, st := testHandler(t, []int64{10, 20}, gw)
		vcf := "TEL:+79991112233"
		if err := h.OnMessageCreated(context.Background(), contactUpdate(42, contactJSON(vcf, hmacHex(testBotToken, vcf), 42))); err != nil {
			t.Fatal(err)
		}
		err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
			Sender: &maxclient.User{UserID: 42}, Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"},
			Body: &maxclient.MessageBody{Text: "1"},
		}})
		if err != nil {
			t.Fatal(err)
		}
		return h, st, gw
	}
	t.Run("gone", func(t *testing.T) {
		h, st, gw := start(t)
		gw.patients = []gateway.Patient{{PatientID: 20, FullName: "Мартынов Дмитрий", BirthDate: "1980-05-02"}}
		err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
			Sender: &maxclient.User{UserID: 42}, Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"},
			Body: &maxclient.MessageBody{Text: "01.01.1985"},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if link, _ := st.GetByUserID(42); link.Verified() {
			t.Fatal("gone profile was linked")
		}
	})
	t.Run("minor", func(t *testing.T) {
		h, st, gw := start(t)
		gw.patients[0].BirthDate = "2015-01-01"
		err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
			Sender: &maxclient.User{UserID: 42}, Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"},
			Body: &maxclient.MessageBody{Text: "01.01.2015"},
		}})
		if err != nil {
			t.Fatal(err)
		}
		fm := h.max.(*fakeMax)
		if link, _ := st.GetByUserID(42); link.Verified() || strings.Contains(fm.messages[len(fm.messages)-1], "Тест") {
			t.Fatal("minor was confirmed")
		}
	})
}

func TestConsentNonce_IsSingleUseAndBoundToTheShownText(t *testing.T) {
	open := func(t *testing.T) (*Handler, *fakeMax, *scriptConsent) {
		t.Helper()
		h, st := testHandler(t, []int64{100}, &fakeGW{})
		if err := st.LinkVerified(42, 100, "79991112233", "Иванов Иван", time.Now()); err != nil {
			t.Fatal(err)
		}
		cons := &scriptConsent{view: gateway.ConsentView{Service: "none", Ads: "none"}}
		h.Consent = cons
		h.ConsentEnabled = true
		h.ServiceVersion = "service-v1"
		return h, h.max.(*fakeMax), cons
	}
	t.Run("repeat", func(t *testing.T) {
		h, fm, cons := open(t)
		if err := h.sendServiceConsent(context.Background(), 1, 42, 100); err != nil {
			t.Fatal(err)
		}
		payload := fm.keyboards[0].rows[0][0].Payload
		if err := h.OnMessageCallback(context.Background(), dialogCallback(42, payload)); err != nil {
			t.Fatal(err)
		}
		if err := h.OnMessageCallback(context.Background(), dialogCallback(42, payload)); err != nil {
			t.Fatal(err)
		}
		if len(cons.posts) != 1 || fm.answers[len(fm.answers)-1] != consentStale {
			t.Fatalf("posts=%v answers=%v", cons.posts, fm.answers)
		}
	})
	t.Run("after revoke", func(t *testing.T) {
		h, fm, cons := open(t)
		if err := h.sendServiceConsent(context.Background(), 1, 42, 100); err != nil {
			t.Fatal(err)
		}
		payload := fm.keyboards[0].rows[0][0].Payload
		err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
			Sender: &maxclient.User{UserID: 42}, Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"},
			Body: &maxclient.MessageBody{Text: "Отключить уведомления"},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.OnMessageCallback(context.Background(), dialogCallback(42, payload)); err != nil {
			t.Fatal(err)
		}
		for _, post := range cons.posts {
			if strings.HasPrefix(post, "service:") && strings.HasSuffix(post, ":accept") {
				t.Fatalf("old button accepted %v", cons.posts)
			}
		}
	})
	t.Run("version", func(t *testing.T) {
		h, fm, cons := open(t)
		if err := h.sendServiceConsent(context.Background(), 1, 42, 100); err != nil {
			t.Fatal(err)
		}
		h.ServiceVersion = "service-v2"
		if err := h.OnMessageCallback(context.Background(), dialogCallback(42, fm.keyboards[0].rows[0][0].Payload)); err != nil {
			t.Fatal(err)
		}
		if len(cons.posts) != 0 {
			t.Fatalf("posts=%v", cons.posts)
		}
	})
	t.Run("other user", func(t *testing.T) {
		h, fm, cons := open(t)
		st := h.storage
		if err := st.LinkVerified(99, 100, "79991112233", "Иванов Иван", time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := h.sendServiceConsent(context.Background(), 1, 42, 100); err != nil {
			t.Fatal(err)
		}
		if err := h.OnMessageCallback(context.Background(), dialogCallback(99, fm.keyboards[0].rows[0][0].Payload)); err != nil {
			t.Fatal(err)
		}
		if len(cons.posts) != 0 {
			t.Fatalf("posts=%v", cons.posts)
		}
	})
	t.Run("other profile", func(t *testing.T) {
		h, fm, cons := open(t)
		if err := h.sendServiceConsent(context.Background(), 1, 42, 100); err != nil {
			t.Fatal(err)
		}
		if err := h.storage.LinkVerified(42, 200, "79991112233", "Другой", time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := h.OnMessageCallback(context.Background(), dialogCallback(42, fm.keyboards[0].rows[0][0].Payload)); err != nil {
			t.Fatal(err)
		}
		if len(cons.posts) != 0 {
			t.Fatalf("posts=%v", cons.posts)
		}
	})
}

func TestBotStarted_DoesNotLogThePayload(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	h, _ := testHandler(t, nil, &fakeGW{})
	err := h.OnBotStarted(context.Background(), &maxclient.Update{
		User:    &maxclient.User{UserID: 7},
		Payload: "79991112233",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "79991112233") || !strings.Contains(logs.String(), "bot_started user=7 payload=есть") {
		t.Fatalf("log=%s", logs.String())
	}
}
