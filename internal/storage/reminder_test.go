package storage

import "testing"

func TestReminderLog_Idempotent(t *testing.T) {
	s, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	const motID int64 = 12345
	const kind = "d1"

	sent, err := s.WasReminderSent(motID, kind)
	if err != nil {
		t.Fatalf("WasReminderSent: %v", err)
	}
	if sent {
		t.Fatal("expected not sent initially")
	}

	if err := s.MarkReminderSent(motID, kind); err != nil {
		t.Fatalf("MarkReminderSent: %v", err)
	}

	sent, err = s.WasReminderSent(motID, kind)
	if err != nil {
		t.Fatalf("WasReminderSent after mark: %v", err)
	}
	if !sent {
		t.Fatal("expected sent after mark")
	}

	// INSERT OR IGNORE — duplicate mark must not error.
	if err := s.MarkReminderSent(motID, kind); err != nil {
		t.Fatalf("MarkReminderSent duplicate: %v", err)
	}

	sent, err = s.WasReminderSent(motID, kind)
	if err != nil || !sent {
		t.Fatalf("still sent after duplicate mark: sent=%v err=%v", sent, err)
	}
}

func TestUsersByPatientID(t *testing.T) {
	s, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if err := s.Link(100, 5001, "79991111111", "Анна"); err != nil {
		t.Fatal(err)
	}
	if err := s.Link(101, 5001, "79992222222", "Мама Анны"); err != nil {
		t.Fatal(err)
	}

	users, err := s.UsersByPatientID(5001)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 {
		t.Fatalf("want 2 users, got %d", len(users))
	}

	if u, err := s.UsersByPatientID(0); err != nil || len(u) != 0 {
		t.Fatalf("patient_id=0: %v %d", err, len(u))
	}
}
