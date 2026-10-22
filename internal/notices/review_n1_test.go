package notices

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/reminders"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/storage"
)

func TestR1_SecondLinkedUserReceives(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_ = st.Link(1, 100, "79990000001", "A")
	_ = st.Link(2, 100, "79990000002", "B")
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, reminders.MoscowLocation())
	row := consentRow(70, 100)
	err = st.EnqueueNoticesAndCursor([]storage.OutboxItem{{
		PlanningID: 70, PatientID: 100, UserID: 2,
		DoctorName: row.DoctorName, Service: row.Service, DateConsultation: row.DateConsultation,
		MessengerNotify: row.MessengerNotify, ApptVersion: row.ApptVersion, BranchID: 3, BranchCode: "Куркино",
	}}, `{"created_at":"2026-06-23 10:00:00.000","planning_id":0}`, now)
	if err != nil {
		t.Fatal(err)
	}
	gw := &fakeGW{live: map[int64]gateway.BookingSnapshot{70: {NewBooking: row}}}
	send := &fakeSend{}
	r := &Runner{Gateway: gw, Storage: st, Sender: send, LinkAllow: []int64{100}, ReminderAllow: []int64{100}, Now: func() time.Time { return now }}
	r.Tick(context.Background())
	if send.n != 1 {
		t.Fatalf("second linked user must receive, n=%d", send.n)
	}
}

func TestR1_RecipientUnlinkedOtherRemains(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_ = st.Link(1, 100, "79990000001", "A")
	_ = st.Link(2, 100, "79990000002", "B")
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, reminders.MoscowLocation())
	row := consentRow(71, 100)
	err = st.EnqueueNoticesAndCursor([]storage.OutboxItem{{
		PlanningID: 71, PatientID: 100, UserID: 2,
		DoctorName: row.DoctorName, Service: row.Service, DateConsultation: row.DateConsultation,
		MessengerNotify: row.MessengerNotify, ApptVersion: row.ApptVersion,
	}}, `{"created_at":"2026-06-23 10:00:00.000","planning_id":0}`, now)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Unlink(2)
	gw := &fakeGW{live: map[int64]gateway.BookingSnapshot{71: {NewBooking: row}}}
	send := &fakeSend{}
	r := &Runner{Gateway: gw, Storage: st, Sender: send, LinkAllow: []int64{100}, ReminderAllow: []int64{100}, Now: func() time.Time { return now }}
	r.Tick(context.Background())
	if send.n != 0 {
		t.Fatal("unlinked recipient must not receive")
	}
	status, reason, _, err := st.OutboxLookup(71)
	if err != nil || status != "dropped" || reason != "unlinked" {
		t.Fatalf("status=%s reason=%s err=%v", status, reason, err)
	}
}

func TestR1_LinkReadErrorIsRetryNotDrop(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_ = st.Link(1, 100, "79990000000", "A")
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, reminders.MoscowLocation())
	row := consentRow(72, 100)
	seedPending(t, st, row, now)
	gw := &fakeGW{live: map[int64]gateway.BookingSnapshot{72: {NewBooking: row}}}
	send := &fakeSend{}
	r := &Runner{
		Gateway: gw, Storage: st, Sender: send,
		LinkAllow: []int64{100}, ReminderAllow: []int64{100},
		Now: func() time.Time { return now },
		Linked: func(int64, int64) (bool, error) {
			return false, errors.New("sqlite busy")
		},
	}
	r.Tick(context.Background())
	if send.n != 0 {
		t.Fatal("must not send on storage error")
	}
	status, reason, attempts, err := st.OutboxLookup(72)
	if err != nil || status != "pending" || reason == "unlinked" || attempts != 1 {
		t.Fatalf("status=%s reason=%s attempts=%d err=%v", status, reason, attempts, err)
	}
}

func TestR2_HealthyItemAfterHundred503(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_ = st.Link(1, 100, "79990000000", "A")
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, reminders.MoscowLocation())
	live := map[int64]gateway.BookingSnapshot{}
	items := make([]storage.OutboxItem, 0, 101)
	one := 1
	for i := 1; i <= 101; i++ {
		row := consentRow(int64(i), 100)
		live[int64(i)] = gateway.BookingSnapshot{NewBooking: row}
		items = append(items, storage.OutboxItem{
			PlanningID: int64(i), PatientID: 100, UserID: 1,
			DoctorName: row.DoctorName, Service: row.Service, DateConsultation: row.DateConsultation,
			MessengerNotify: &one, ApptVersion: row.ApptVersion,
		})
	}
	if err := st.EnqueueNoticesAndCursor(items, `{"created_at":"2026-06-23 10:00:00.000","planning_id":0}`, now); err != nil {
		t.Fatal(err)
	}
	gw := &fakeGW{
		live: live,
		statusFor: func(id int64) int {
			if id <= 100 {
				return 503
			}
			return 0
		},
	}
	send := &fakeSend{}
	r := &Runner{Gateway: gw, Storage: st, Sender: send, LinkAllow: []int64{100}, ReminderAllow: []int64{100}, Now: func() time.Time { return now }}
	r.Tick(context.Background())
	st1, _, a1, _ := st.OutboxLookup(1)
	if st1 != "pending" || a1 != 1 {
		t.Fatalf("item1 status=%s attempts=%d", st1, a1)
	}
	if send.n == 1 {
		return
	}
	r.Now = func() time.Time { return now.Add(time.Second) }
	r.Tick(context.Background())
	if send.n != 1 {
		t.Fatalf("item 101 must send on first or second cycle, n=%d", send.n)
	}
	st101, _, _, err := st.OutboxLookup(101)
	if err != nil || st101 != "sent" {
		t.Fatalf("101 status=%s err=%v", st101, err)
	}
}
