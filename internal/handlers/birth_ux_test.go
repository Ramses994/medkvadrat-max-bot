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

// birthDialog drives user 42 through the MAX dialog: contact() sends the signed contact, say() a text.
func birthDialog(t *testing.T, h *Handler) (contact func(), say func(string) string) {
	t.Helper()
	vcf := "TEL:+79991112233"
	contact = func() {
		t.Helper()
		if err := h.OnMessageCreated(context.Background(), contactUpdate(42, contactJSON(vcf, hmacHex(testBotToken, vcf), 42))); err != nil {
			t.Fatal(err)
		}
	}
	say = func(text string) string {
		t.Helper()
		err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
			Sender: &maxclient.User{UserID: 42}, Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"},
			Body: &maxclient.MessageBody{Text: text},
		}})
		if err != nil {
			t.Fatal(err)
		}
		fm := h.max.(*fakeMax)
		return fm.messages[len(fm.messages)-1]
	}
	return contact, say
}

func TestBirthFailures_SurviveLinkAndUnlink(t *testing.T) {
	gw := &fakeGW{patients: []gateway.Patient{
		{PatientID: 10, FullName: "Тест Тестовый", BirthDate: "1985-01-01"},
		{PatientID: 20, FullName: "Мартынов Дмитрий", BirthDate: "1981-03-05"},
	}}
	h, st := testHandler(t, []int64{10, 20}, gw)
	contact, say := birthDialog(t, h)
	contact()
	say("2")
	say("02.02.1985")
	if msg := say("03.03.1985"); !strings.Contains(msg, "Осталось попыток: 1") {
		t.Fatalf("second miss: %q", msg)
	}
	say("1")
	say("01.01.1985")
	if link, _ := st.GetByUserID(42); !link.Verified() || link.PatientID != 10 {
		t.Fatalf("expected link to profile 1, got %+v", link)
	}
	say("Сменить номер")
	contact()
	say("2")
	if msg := say("04.04.1985"); !strings.Contains(msg, "регистратуру") {
		t.Fatalf("third miss across link/unlink must block: %q", msg)
	}
	if blocked, err := st.BirthBlocked(42, h.today()); err != nil || !blocked {
		t.Fatalf("blocked=%v err=%v", blocked, err)
	}
	if link, _ := st.GetByUserID(42); link.Verified() {
		t.Fatalf("linked after block: %+v", link)
	}
}

func TestBirthDate_CounterErrorDoesNotCompareOrLink(t *testing.T) {
	gw := &fakeGW{patients: []gateway.Patient{
		{PatientID: 10, FullName: "Тест Тестовый", BirthDate: "1985-01-01"},
		{PatientID: 20, FullName: "Мартынов Дмитрий", BirthDate: "1981-03-05"},
	}}
	h, st := testHandler(t, []int64{10, 20}, gw)
	contact, say := birthDialog(t, h)
	contact()
	say("1")
	st.FailBirthCounter(errors.New("disk"))
	if msg := say("01.01.1985"); !strings.Contains(msg, "Попробуйте позже") {
		t.Fatalf("counter error reply: %q", msg)
	}
	if msg := say("02.02.1985"); !strings.Contains(msg, "Попробуйте позже") {
		t.Fatalf("counter error reply: %q", msg)
	}
	if link, _ := st.GetByUserID(42); link.Verified() {
		t.Fatalf("linked while the counter is broken: %+v", link)
	}
	st.FailBirthCounter(nil)
	if msg := say("03.03.1985"); !strings.Contains(msg, "Осталось попыток: 2") {
		t.Fatalf("failed writes must not count: %q", msg)
	}
}

func TestBirthFailures_WindowRestartsDayAfterFirstMiss(t *testing.T) {
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	gw := &fakeGW{patients: []gateway.Patient{
		{PatientID: 10, FullName: "Тест Тестовый", BirthDate: "1985-01-01"},
		{PatientID: 20, FullName: "Мартынов Дмитрий", BirthDate: "1981-03-05"},
	}}
	h, st := testHandler(t, []int64{10, 20}, gw)
	h.Now = func() time.Time { return now }
	contact, say := birthDialog(t, h)
	contact()
	say("1")
	say("02.02.1985")
	now = now.Add(time.Minute)
	if msg := say("03.03.1985"); !strings.Contains(msg, "Осталось попыток: 1") {
		t.Fatalf("second miss: %q", msg)
	}
	now = now.Add(24 * time.Hour)
	contact()
	say("1")
	if msg := say("04.04.1985"); !strings.Contains(msg, "Осталось попыток: 2") {
		t.Fatalf("counter must restart a day after the first miss: %q", msg)
	}
	if blocked, _ := st.BirthBlocked(42, h.today()); blocked {
		t.Fatal("blocked after the window expired")
	}
}

func TestBirthDate_BackWithOnePilotAdultShowsOneRow(t *testing.T) {
	gw := &fakeGW{patients: []gateway.Patient{
		{PatientID: 10, FullName: "Тест Тестовый", BirthDate: "1985-01-01"},
		{PatientID: 20, FullName: "Вне Списка", BirthDate: "1981-03-05"},
	}}
	h, st := testHandler(t, []int64{10}, gw)
	contact, say := birthDialog(t, h)
	contact()
	say("1")
	list := say("Назад")
	if !strings.Contains(list, "1. Т. Т., 1985") || strings.Contains(list, "2.") || strings.Contains(list, "недоступен") {
		t.Fatalf("list after back: %q", list)
	}
	if link, _ := st.GetByUserID(42); link.Verified() {
		t.Fatal("linked without the birth date")
	}
	if prompt := say("1"); !strings.Contains(prompt, "„Т. Т., 1985“") {
		t.Fatalf("birth prompt after back: %q", prompt)
	}
	say("01.01.1985")
	if link, _ := st.GetByUserID(42); !link.Verified() || link.PatientID != 10 {
		t.Fatalf("link=%+v", link)
	}
}

func TestBirthDate_UnparseableTextIsNotAnAttempt(t *testing.T) {
	gw := &fakeGW{patients: []gateway.Patient{
		{PatientID: 10, FullName: "Тест Тестовый", BirthDate: "1985-01-01"},
		{PatientID: 20, FullName: "Мартынов Дмитрий", BirthDate: "1981-03-05"},
	}}
	h, st := testHandler(t, []int64{10, 20}, gw)
	vcf := "TEL:+79991112233"
	say := func(text string) string {
		t.Helper()
		err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
			Sender: &maxclient.User{UserID: 42}, Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"},
			Body: &maxclient.MessageBody{Text: text},
		}})
		if err != nil {
			t.Fatal(err)
		}
		fm := h.max.(*fakeMax)
		return fm.messages[len(fm.messages)-1]
	}
	if err := h.OnMessageCreated(context.Background(), contactUpdate(42, contactJSON(vcf, hmacHex(testBotToken, vcf), 42))); err != nil {
		t.Fatal(err)
	}
	say("1")
	for _, bad := range []string{"Другой", "ДД.ММ.ГГГГ", "31.02.1981"} {
		msg := say(bad)
		if !strings.Contains(msg, "Введите дату рождения цифрами") {
			t.Fatalf("%q → %q", bad, msg)
		}
		if strings.Contains(msg, "Осталось попыток") || strings.Contains(msg, "регистратуру") {
			t.Fatalf("unparseable counted as attempt: %q → %q", bad, msg)
		}
	}
	msg := say("5-3-1981")
	if !strings.Contains(msg, "Дата не совпадает. Осталось попыток: 2") {
		t.Fatalf("parsed dash date: %q", msg)
	}
	say("02.02.1985")
	msg = say("03.03.1985")
	if !strings.Contains(msg, "регистратуру") {
		t.Fatalf("third wrong parsed date should block: %q", msg)
	}
	if link, _ := st.GetByUserID(42); link.Verified() {
		t.Fatal("linked after block")
	}
}

func TestBirthDate_BackAndReselectPreservesFailures(t *testing.T) {
	gw := &fakeGW{patients: []gateway.Patient{
		{PatientID: 10, FullName: "Тест Тестовый", BirthDate: "1985-01-01"},
		{PatientID: 20, FullName: "Мартынов Дмитрий", BirthDate: "1981-03-05"},
	}}
	h, st := testHandler(t, []int64{10, 20}, gw)
	vcf := "TEL:+79991112233"
	say := func(text string) string {
		t.Helper()
		err := h.OnMessageCreated(context.Background(), &maxclient.Update{Message: &maxclient.Message{
			Sender: &maxclient.User{UserID: 42}, Recipient: &maxclient.Recipient{ChatID: 1, ChatType: "dialog"},
			Body: &maxclient.MessageBody{Text: text},
		}})
		if err != nil {
			t.Fatal(err)
		}
		fm := h.max.(*fakeMax)
		return fm.messages[len(fm.messages)-1]
	}
	if err := h.OnMessageCreated(context.Background(), contactUpdate(42, contactJSON(vcf, hmacHex(testBotToken, vcf), 42))); err != nil {
		t.Fatal(err)
	}
	prompt := say("1")
	if !strings.Contains(prompt, "„Т. Т., 1985“") || !strings.Contains(prompt, "„Назад“") {
		t.Fatalf("birth prompt: %q", prompt)
	}
	msg := say("02.02.1985")
	if !strings.Contains(msg, "Осталось попыток: 2") {
		t.Fatalf("first miss: %q", msg)
	}
	list := say("Назад")
	if !strings.Contains(list, "1. Т. Т., 1985") || !strings.Contains(list, "2. М. Д., 1981") {
		t.Fatalf("list after back: %q", list)
	}
	prompt = say("2")
	if !strings.Contains(prompt, "„М. Д., 1981“") {
		t.Fatalf("reselected prompt: %q", prompt)
	}
	msg = say("01.01.1985")
	if !strings.Contains(msg, "Осталось попыток: 1") {
		t.Fatalf("counter must survive reselect: %q", msg)
	}
	say("05.03.1981")
	link, _ := st.GetByUserID(42)
	if !link.Verified() || link.PatientID != 20 {
		t.Fatalf("expected profile 2, got %+v", link)
	}
}
