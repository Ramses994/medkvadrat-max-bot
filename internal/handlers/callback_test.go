package handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/storage"
)

type scriptGW struct {
	fakeGW
	stored  string
	already bool
}

func (s *scriptGW) PostConfirmation(context.Context, int64, string, int64) (string, bool, error) {
	return s.stored, s.already, nil
}

func TestCallback_ReplacesKeyboard(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Link(7, 1, "79990000000", "N"); err != nil {
		t.Fatal(err)
	}
	fm := &fakeMax{}
	h := &Handler{max: fm, gateway: &scriptGW{stored: "declined"}, storage: st}
	u := &maxclient.Update{
		Callback: &maxclient.Callback{CallbackID: "cb1", Payload: "decline:10", User: &maxclient.User{UserID: 7}},
		Message:  &maxclient.Message{Body: &maxclient.MessageBody{Text: "Напоминание"}},
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
}

func TestCallback_AlreadyAnsweredStillClearsKeyboard(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_ = st.Link(7, 1, "79990000000", "N")
	fm := &fakeMax{}
	h := &Handler{max: fm, gateway: &scriptGW{stored: "confirmed", already: true}, storage: st}
	u := &maxclient.Update{
		Callback: &maxclient.Callback{CallbackID: "cb1", Payload: "decline:10", User: &maxclient.User{UserID: 7}},
		Message:  &maxclient.Message{Body: &maxclient.MessageBody{Text: "Напоминание"}},
	}
	if err := h.OnMessageCallback(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fm.replaced, "✅ Приду") {
		t.Fatalf("replaced=%q", fm.replaced)
	}
}
