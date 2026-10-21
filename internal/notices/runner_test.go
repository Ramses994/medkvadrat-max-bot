package notices

import (
	"context"
	"testing"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/reminders"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/storage"
)

type fakeGW struct {
	calls int
	since time.Time
	ids   []int64
	rows  []gateway.NewBooking
}

func (f *fakeGW) NewBookings(_ context.Context, since time.Time, patientIDs []int64) ([]gateway.NewBooking, error) {
	f.calls++
	f.since = since
	f.ids = patientIDs
	return f.rows, nil
}

type fakeSend struct{ n int }

func (f *fakeSend) SendText(context.Context, int64, string) error {
	f.n++
	return nil
}

func TestFirstTickDoesNotSendHistory(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_ = st.Link(1, 100, "79990000000", "A")
	gw := &fakeGW{rows: []gateway.NewBooking{{DueReminder: gateway.DueReminder{PlanningID: 1, PatientID: 100, DateConsultation: "2026-06-26 19:30"}, CreatedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}}}
	r := &Runner{
		Gateway: gw, Storage: st, Sender: &fakeSend{},
		LinkAllow: []int64{100}, ReminderAllow: []int64{100},
		Now: func() time.Time { return time.Date(2026, 6, 23, 12, 0, 0, 0, reminders.MoscowLocation()) },
	}
	r.Tick(context.Background())
	if gw.calls != 0 {
		t.Fatalf("first tick must not query history, calls=%d", gw.calls)
	}
	v, _ := st.GetKV(storage.BookingNoticeSinceKey)
	if v == "" {
		t.Fatal("since not initialized")
	}
}

func TestSecondTickNoDuplicateAndEmptyReminderList(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_ = st.Link(1, 100, "79990000000", "A")
	_ = st.Link(2, 0, "79991111111", "pending")
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, reminders.MoscowLocation())
	_ = st.SetKV(storage.BookingNoticeSinceKey, now.Add(-time.Hour).Format(time.RFC3339))
	created := now.Add(-30 * time.Minute)
	gw := &fakeGW{rows: []gateway.NewBooking{{
		DueReminder: gateway.DueReminder{
			PlanningID: 9, PatientID: 100, DoctorName: "Иванов", Specialty: "Терапия",
			BranchID: 3, BranchCode: "Куркино", DateConsultation: "2026-06-26 19:30",
		},
		CreatedAt: created,
	}}}
	send := &fakeSend{}
	r := &Runner{
		Gateway: gw, Storage: st, Sender: send,
		LinkAllow: []int64{100}, ReminderAllow: []int64{100},
		Now: func() time.Time { return now },
	}
	r.Tick(context.Background())
	if send.n != 1 {
		t.Fatalf("sent=%d ids=%v", send.n, gw.ids)
	}
	if len(gw.ids) != 1 || gw.ids[0] != 100 {
		t.Fatalf("ids=%v", gw.ids)
	}
	r.Tick(context.Background())
	if send.n != 1 {
		t.Fatalf("duplicate sent=%d", send.n)
	}

	gw.calls = 0
	r.ReminderAllow = nil
	r.Tick(context.Background())
	if gw.calls != 0 {
		t.Fatal("empty reminder allowlist must not query")
	}
}
