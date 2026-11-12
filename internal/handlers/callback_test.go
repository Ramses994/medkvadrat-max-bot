package handlers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/storage"
)

type scriptGW struct {
	fakeGW
	stored  string
	already bool
	stale   bool
}

func (s *scriptGW) PostConfirmation(context.Context, int64, string, int64, string) (string, bool, bool, error) {
	return s.stored, s.already, s.stale, nil
}

func TestCallback_ReplacesKeyboard(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.LinkVerified(7, 1, "79990000000", "N", time.Now()); err != nil {
		t.Fatal(err)
	}
	fm := &fakeMax{}
	h := &Handler{max: fm, gateway: &scriptGW{stored: "declined"}, storage: st}
	u := &maxclient.Update{
		Callback: &maxclient.Callback{CallbackID: "cb1", Payload: "decline:10", User: &maxclient.User{UserID: 7}},
		Message:  &maxclient.Message{Recipient: &maxclient.Recipient{ChatType: "dialog"}, Body: &maxclient.MessageBody{Text: "Напоминание"}},
	}
	if err := h.OnMessageCallback(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fm.replaced, "Напоминание") || !strings.Contains(fm.replaced, "❌ Не приду") {
		t.Fatalf("replaced=%q", fm.replaced)
	}
	if strings.Contains(fm.replaced, "callback") {
		t.Fatal("keyboard text leaked")
	}
	want := "Спасибо, что предупредили. Мы внесли вас в лист ожидания — регистратура свяжется с вами, чтобы подобрать другое время."
	if fm.notification != want {
		t.Fatalf("notification=%q", fm.notification)
	}
}

func TestCallback_RescheduleWaitlistText(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.LinkVerified(7, 1, "79990000000", "N", time.Now()); err != nil {
		t.Fatal(err)
	}
	fm := &fakeMax{}
	h := &Handler{max: fm, gateway: &scriptGW{stored: "reschedule"}, storage: st}
	u := &maxclient.Update{
		Callback: &maxclient.Callback{CallbackID: "cb1", Payload: "reschedule:10", User: &maxclient.User{UserID: 7}},
		Message:  &maxclient.Message{Recipient: &maxclient.Recipient{ChatType: "dialog"}, Body: &maxclient.MessageBody{Text: "Напоминание"}},
	}
	if err := h.OnMessageCallback(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fm.replaced, "🔄 Перенести") {
		t.Fatalf("replaced=%q", fm.replaced)
	}
	want := "Передали в регистратуру — с вами свяжутся, чтобы подобрать другое время."
	if fm.notification != want {
		t.Fatalf("notification=%q", fm.notification)
	}
}

func TestCallback_StaleReminderKeepsF5Text(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.LinkVerified(7, 1, "79990000000", "N", time.Now()); err != nil {
		t.Fatal(err)
	}
	fm := &fakeMax{}
	h := &Handler{max: fm, gateway: &scriptGW{stale: true}, storage: st}
	u := &maxclient.Update{
		Callback: &maxclient.Callback{CallbackID: "cb1", Payload: "decline:10", User: &maxclient.User{UserID: 7}},
		Message:  &maxclient.Message{Recipient: &maxclient.Recipient{ChatType: "dialog"}, Body: &maxclient.MessageBody{Text: "Напоминание"}},
	}
	if err := h.OnMessageCallback(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if fm.replaced != "Напоминание" {
		t.Fatalf("replaced=%q", fm.replaced)
	}
	if fm.notification != "Запись изменилась — ориентируйтесь на новое напоминание или позвоните в регистратуру" {
		t.Fatalf("notification=%q", fm.notification)
	}
}

func TestCallback_AlreadyAnsweredStillClearsKeyboard(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_ = st.LinkVerified(7, 1, "79990000000", "N", time.Now())
	fm := &fakeMax{}
	h := &Handler{max: fm, gateway: &scriptGW{stored: "confirmed", already: true}, storage: st}
	u := &maxclient.Update{
		Callback: &maxclient.Callback{CallbackID: "cb1", Payload: "decline:10", User: &maxclient.User{UserID: 7}},
		Message:  &maxclient.Message{Recipient: &maxclient.Recipient{ChatType: "dialog"}, Body: &maxclient.MessageBody{Text: "Напоминание"}},
	}
	if err := h.OnMessageCallback(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fm.replaced, "✅ Приду") {
		t.Fatalf("replaced=%q", fm.replaced)
	}
}

func TestCallback_UnverifiedAsksForTheContactButton(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	fm := &fakeMax{}
	h := &Handler{max: fm, gateway: &scriptGW{}, storage: st}
	u := &maxclient.Update{
		Callback: &maxclient.Callback{CallbackID: "cb1", Payload: "decline:10", User: &maxclient.User{UserID: 7}},
		Message:  &maxclient.Message{Recipient: &maxclient.Recipient{ChatType: "dialog"}},
	}
	if err := h.OnMessageCallback(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if len(fm.answers) != 1 || fm.answers[0] != "Подтвердите номер кнопкой „Поделиться контактом“" {
		t.Fatalf("answers=%v", fm.answers)
	}
}

func TestCallback_WithoutMessageDoesNotReplaceTheReminder(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.LinkVerified(7, 1, "79990000000", "N", time.Now()); err != nil {
		t.Fatal(err)
	}
	fm := &fakeMax{}
	h := &Handler{max: fm, gateway: &scriptGW{stored: "declined"}, storage: st}
	u := &maxclient.Update{
		Callback: &maxclient.Callback{CallbackID: "cb1", Payload: "decline:10", User: &maxclient.User{UserID: 7}},
	}
	if err := h.OnMessageCallback(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	want := "Спасибо, что предупредили. Мы внесли вас в лист ожидания — регистратура свяжется с вами, чтобы подобрать другое время."
	if len(fm.answers) != 1 || fm.answers[0] != want {
		t.Fatalf("answers=%v", fm.answers)
	}
	if fm.replaced != "" || fm.notification != "" {
		t.Fatalf("replaced=%q notification=%q", fm.replaced, fm.notification)
	}
}
