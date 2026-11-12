package handlers

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
)

func TestCallback_MissingChatTypeIsStillHandled(t *testing.T) {
	press := func(t *testing.T, u *maxclient.Update) []string {
		t.Helper()
		h, _ := testHandler(t, nil, &fakeGW{})
		if err := h.OnMessageCallback(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		return h.max.(*fakeMax).answers
	}
	secret := "secret-payload-value"
	noMessage := press(t, &maxclient.Update{Callback: &maxclient.Callback{
		CallbackID: "cb", Payload: secret, User: &maxclient.User{UserID: 7},
	}})
	if len(noMessage) != 1 || noMessage[0] != "Не удалось обработать нажатие" {
		t.Fatalf("no message %#v", noMessage)
	}
	noRecipient := press(t, &maxclient.Update{
		Message:  &maxclient.Message{},
		Callback: &maxclient.Callback{CallbackID: "cb", Payload: secret, User: &maxclient.User{UserID: 7}},
	})
	if len(noRecipient) != 1 {
		t.Fatalf("no recipient %#v", noRecipient)
	}
	dialog := press(t, &maxclient.Update{
		Message:  &maxclient.Message{Recipient: &maxclient.Recipient{ChatType: "dialog"}},
		Callback: &maxclient.Callback{CallbackID: "cb", Payload: secret, User: &maxclient.User{UserID: 7}},
	})
	if len(dialog) != 1 {
		t.Fatalf("dialog %#v", dialog)
	}
}

func TestCallback_OtherChatTypeIsSkippedAndLogged(t *testing.T) {
	var logs bytes.Buffer
	prev := log.Default().Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(prev) })
	h, _ := testHandler(t, nil, &fakeGW{})
	err := h.OnMessageCallback(context.Background(), &maxclient.Update{
		Message:  &maxclient.Message{Recipient: &maxclient.Recipient{ChatType: "chat"}},
		Callback: &maxclient.Callback{CallbackID: "cb", Payload: "secret-payload-value", User: &maxclient.User{UserID: 7}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fm := h.max.(*fakeMax)
	if len(fm.answers) != 0 || !strings.Contains(logs.String(), "message_callback skipped chat_type=chat") || strings.Contains(logs.String(), "secret-payload-value") {
		t.Fatalf("answers=%v log=%s", fm.answers, logs.String())
	}
}

func TestCallback_DebugShapeNamesFieldsNotValues(t *testing.T) {
	var logs bytes.Buffer
	prev := log.Default().Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(prev) })
	h, _ := testHandler(t, nil, &fakeGW{})
	h.KeyboardDebug = true
	err := h.OnMessageCallback(context.Background(), &maxclient.Update{
		Message:  &maxclient.Message{Recipient: &maxclient.Recipient{ChatType: "dialog", ChatID: 99}},
		Callback: &maxclient.Callback{CallbackID: "cb-secret", Payload: "secret-payload-value", User: &maxclient.User{UserID: 7}},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := logs.String()
	if !strings.Contains(text, "message_callback shape") || !strings.Contains(text, "chat_type=dialog") || !strings.Contains(text, "payload=string") {
		t.Fatalf("log=%s", text)
	}
	if strings.Contains(text, "secret-payload-value") || strings.Contains(text, "cb-secret") {
		t.Fatalf("values leaked: %s", text)
	}
}

func TestLinkAttempt_NamesFoundAdultsAndAllowedAdults(t *testing.T) {
	var logs bytes.Buffer
	prev := log.Default().Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(prev) })
	gw := &fakeGW{patients: []gateway.Patient{
		{PatientID: 100, FullName: "Тест Тестовый", BirthDate: "1985-01-01"},
		{PatientID: 200, FullName: "Вне Списка", BirthDate: "1980-05-02"},
	}}
	h, _ := testHandler(t, []int64{100}, gw)
	vcf := "TEL:+79991112233"
	if err := h.OnMessageCreated(context.Background(), contactUpdate(42, contactJSON(vcf, hmacHex(testBotToken, vcf), 42))); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "link attempt user=42 found=2 adults=2 allowed_adults=1") {
		t.Fatalf("log=%s", logs.String())
	}
}
