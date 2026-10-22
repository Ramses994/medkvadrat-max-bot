package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestOutboxReopenThenDue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bot.db")
	st, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	one := 1
	if err := st.EnqueueNoticesAndCursor([]OutboxItem{{
		PlanningID: 1, PatientID: 100, UserID: 7, Service: "Прием",
		DateConsultation: "2026-06-26 19:30", MessengerNotify: &one, ApptVersion: "ver-a",
	}}, `{"created_at":"2026-06-23 10:00:00.000","planning_id":0}`, now); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st2, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	items, err := st2.DueOutbox(now, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
	if items[0].ApptVersion != "ver-a" || items[0].UserID != 7 {
		t.Fatalf("%+v", items[0])
	}
}

func TestOutboxInsertFailureDoesNotMoveCursor(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SetKV(BookingNoticeCursorKey, `{"created_at":"old","planning_id":1}`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`CREATE TRIGGER fail_outbox BEFORE INSERT ON notice_outbox BEGIN SELECT RAISE(FAIL, 'boom'); END`); err != nil {
		t.Fatal(err)
	}
	one := 1
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	err = st.EnqueueNoticesAndCursor([]OutboxItem{{
		PlanningID: 2, PatientID: 100, UserID: 1, MessengerNotify: &one,
	}}, `{"created_at":"new","planning_id":2}`, now)
	if err == nil {
		t.Fatal("expected insert failure")
	}
	got, err := st.GetKV(BookingNoticeCursorKey)
	if err != nil || got != `{"created_at":"old","planning_id":1}` {
		t.Fatalf("cursor moved: %q err=%v", got, err)
	}
}
