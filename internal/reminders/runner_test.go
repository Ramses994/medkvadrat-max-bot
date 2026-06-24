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
	err        error
	rows       []gateway.DueReminder
	calls      int
	lastPatients []int64
}

func (f *fakeDueGateway) DueReminders(ctx context.Context, from, to time.Time, patientIDs []int64) ([]gateway.DueReminder, error) {
	f.calls++
	f.lastPatients = patientIDs
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

type fakeMessenger struct {
	sent []int64
	err  error
}

func (f *fakeMessenger) SendToUser(ctx context.Context, userID int64, text string) error {
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
	_ = store.Link(42, 6789, "79991234567", "Иванова")

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
		PlanningID:       11737097,
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

func TestRunner_Tick_AllowlistSkipsUnlistedPatient(t *testing.T) {
	store, err := storage.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_ = store.Link(42, 6789, "79991234567", "Иванова")
	_ = store.Link(43, 9999, "79990000000", "Другой")

	gw := &fakeDueGateway{} // gateway returns only allowlisted patients; none due in window
	msg := &fakeMessenger{}
	r := &Runner{
		Gateway:   gw,
		Storage:   store,
		Messenger: msg,
		Allowlist: []int64{6789},
		Now:       func() time.Time { return msk(2026, time.June, 23, 10, 30) },
	}
	r.Tick(context.Background())
	if len(gw.lastPatients) != 1 || gw.lastPatients[0] != 6789 {
		t.Fatalf("gateway patient_ids=%v", gw.lastPatients)
	}
	if len(msg.sent) != 0 {
		t.Fatalf("expected no sends, got %v", msg.sent)
	}
}
