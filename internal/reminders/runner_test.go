package reminders

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/storage"
)

type fakeDueGateway struct {
	err   error
	rows  []gateway.DueReminder
	calls int
}

func (f *fakeDueGateway) DueReminders(ctx context.Context, from, to time.Time) ([]gateway.DueReminder, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

type fakeMessenger struct {
	sent []int64
	err  error
}

func (f *fakeMessenger) SendToUser(ctx context.Context, userID int64, text string, motconsuID int64) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, userID)
	return nil
}

func TestRunner_Tick_GatewayErrorDoesNotPanic(t *testing.T) {
	store, err := storage.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	gw := &fakeDueGateway{err: errors.New("gateway down")}
	msg := &fakeMessenger{}

	r := &Runner{Gateway: gw, Storage: store, Messenger: msg, Now: func() time.Time {
		return msk(2026, time.June, 21, 10, 30)
	}}

	r.Tick(context.Background())
	r.Tick(context.Background())

	if gw.calls != 2 {
		t.Fatalf("expected 2 gateway calls, got %d", gw.calls)
	}
	if len(msg.sent) != 0 {
		t.Fatalf("expected no sends on gateway error, got %d", len(msg.sent))
	}
}

func TestRunner_Tick_NoDuplicateAfterMark(t *testing.T) {
	store, err := storage.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Link(42, 6789, "79991234567", "Иванова"); err != nil {
		t.Fatal(err)
	}

	now := msk(2026, time.June, 23, 10, 30)
	row := gateway.DueReminder{
		MotconsuID:       12345,
		PatientID:        6789,
		DoctorName:       "Смирнов Иван",
		DepartmentLabel:  "Каширка",
		DateConsultation: "2026-06-24 10:30",
	}
	gw := &fakeDueGateway{rows: []gateway.DueReminder{row}}
	msg := &fakeMessenger{}

	r := &Runner{Gateway: gw, Storage: store, Messenger: msg, Now: func() time.Time { return now }}

	r.Tick(context.Background())
	if len(msg.sent) != 1 {
		t.Fatalf("first tick: want 1 send, got %d", len(msg.sent))
	}

	msg.sent = nil
	r.Tick(context.Background())
	if len(msg.sent) != 0 {
		t.Fatalf("second tick: duplicate send %v", msg.sent)
	}
}
