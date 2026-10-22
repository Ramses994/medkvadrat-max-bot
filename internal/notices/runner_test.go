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

type fakeGW struct {
	calls      int
	ids        []int64
	pages      []gateway.NewBookingsPage
	live       map[int64]gateway.BookingSnapshot
	bookingErr error
	statusFor  func(planningID int64) int
}

func (f *fakeGW) NewBookings(_ context.Context, patientIDs []int64, cursor *gateway.BookingCursor, limit int) (gateway.NewBookingsPage, error) {
	f.calls++
	f.ids = patientIDs
	if len(f.pages) == 0 {
		return gateway.NewBookingsPage{}, nil
	}
	p := f.pages[0]
	if len(f.pages) > 1 {
		f.pages = f.pages[1:]
	}
	if f.live == nil {
		f.live = map[int64]gateway.BookingSnapshot{}
	}
	for _, it := range p.Items {
		f.live[it.PlanningID] = gateway.BookingSnapshot{NewBooking: it}
	}
	return p, nil
}

func (f *fakeGW) BookingByPlanningID(_ context.Context, planningID int64) (gateway.BookingSnapshot, error) {
	if f.bookingErr != nil {
		return gateway.BookingSnapshot{}, f.bookingErr
	}
	if f.statusFor != nil {
		if st := f.statusFor(planningID); st >= 500 {
			return gateway.BookingSnapshot{}, &gateway.Error{Op: "booking", Status: st}
		}
	}
	if f.live != nil {
		if s, ok := f.live[planningID]; ok {
			return s, nil
		}
	}
	return gateway.BookingSnapshot{}, &gateway.Error{Op: "booking", Status: 404, Code: "NOT_FOUND"}
}

type fakeSend struct {
	n     int
	tries int
	err   error
}

func (f *fakeSend) SendText(context.Context, int64, string) error {
	f.tries++
	if f.err != nil {
		return f.err
	}
	f.n++
	return nil
}

func consentRow(planningID, patientID int64) gateway.NewBooking {
	one := 1
	return gateway.NewBooking{
		DueReminder: gateway.DueReminder{
			PlanningID: planningID, PatientID: patientID, DoctorName: "Иванов", Service: "Прием",
			MessengerNotify: &one, ApptVersion: "ver-a", BranchID: 3, BranchCode: "Куркино",
			DateConsultation: "2026-06-26 19:30",
		},
		CreatedAt: "2026-06-23 11:30:00.123",
	}
}

func TestFirstTickDoesNotSendHistory(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_ = st.Link(1, 100, "79990000000", "A")
	gw := &fakeGW{pages: []gateway.NewBookingsPage{{Items: []gateway.NewBooking{consentRow(1, 100)}}}}
	r := &Runner{
		Gateway: gw, Storage: st, Sender: &fakeSend{},
		LinkAllow: []int64{100}, ReminderAllow: []int64{100},
		Now: func() time.Time { return time.Date(2026, 6, 23, 12, 0, 0, 0, reminders.MoscowLocation()) },
	}
	r.Tick(context.Background())
	if gw.calls != 0 {
		t.Fatalf("first tick must not query history, calls=%d", gw.calls)
	}
}

func TestRetryThenSuccessAndEmptyAllowlist(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_ = st.Link(1, 100, "79990000000", "A")
	_ = st.Link(2, 0, "79991111111", "pending")
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, reminders.MoscowLocation())
	cur, _ := storage.EncodeCursor("2026-06-23 10:00:00.000", 0)
	_ = st.SetKV(storage.BookingNoticeCursorKey, cur)
	row := consentRow(9, 100)
	gw := &fakeGW{pages: []gateway.NewBookingsPage{{
		Items:      []gateway.NewBooking{row},
		NextCursor: &gateway.BookingCursor{CreatedAt: row.CreatedAt, PlanningID: row.PlanningID},
	}}}
	send := &fakeSend{err: errors.New("max down")}
	r := &Runner{
		Gateway: gw, Storage: st, Sender: send,
		LinkAllow: []int64{100}, ReminderAllow: []int64{100},
		Now: func() time.Time { return now },
	}
	r.Tick(context.Background())
	if send.n != 0 {
		t.Fatal("first send must fail")
	}
	send.err = nil
	r.Now = func() time.Time { return now.Add(2 * time.Minute) }
	r.Tick(context.Background())
	if send.n != 1 {
		t.Fatalf("retry sent=%d", send.n)
	}
	r.Tick(context.Background())
	if send.n != 1 {
		t.Fatalf("duplicate sent=%d", send.n)
	}
	if len(gw.ids) != 1 || gw.ids[0] != 100 {
		t.Fatalf("ids=%v", gw.ids)
	}

	gw.calls = 0
	r.ReminderAllow = nil
	r.Tick(context.Background())
	if gw.calls != 0 {
		t.Fatal("empty reminder allowlist must not query")
	}
}

func TestAudienceOver500Skipped(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	allow := make([]int64, 501)
	for i := range allow {
		id := int64(i + 1)
		allow[i] = id
		_ = st.Link(id, id, "79990000000", "A")
	}
	gw := &fakeGW{}
	r := &Runner{
		Gateway: gw, Storage: st, Sender: &fakeSend{},
		LinkAllow: allow, ReminderAllow: allow,
		Now: func() time.Time { return time.Date(2026, 6, 23, 12, 0, 0, 0, reminders.MoscowLocation()) },
	}
	cur, _ := storage.EncodeCursor("2026-06-23 10:00:00.000", 0)
	_ = st.SetKV(storage.BookingNoticeCursorKey, cur)
	r.Tick(context.Background())
	if gw.calls != 0 {
		t.Fatalf("501 patients must skip, calls=%d", gw.calls)
	}
}

func TestPaginationTwoHundredOne(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_ = st.Link(1, 100, "79990000000", "A")
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, reminders.MoscowLocation())
	cur, _ := storage.EncodeCursor("2026-06-23 10:00:00.000", 0)
	_ = st.SetKV(storage.BookingNoticeCursorKey, cur)
	page1 := make([]gateway.NewBooking, 200)
	for i := range page1 {
		page1[i] = consentRow(int64(i+1), 100)
	}
	gw := &fakeGW{pages: []gateway.NewBookingsPage{
		{Items: page1, HasMore: true, NextCursor: &gateway.BookingCursor{CreatedAt: "2026-06-23 11:00:00.001", PlanningID: 200}},
		{Items: []gateway.NewBooking{consentRow(201, 100)}, HasMore: false, NextCursor: &gateway.BookingCursor{CreatedAt: "2026-06-23 11:00:00.002", PlanningID: 201}},
	}}
	r := &Runner{
		Gateway: gw, Storage: st, Sender: &fakeSend{},
		LinkAllow: []int64{100}, ReminderAllow: []int64{100},
		Now: func() time.Time { return now },
	}
	r.Tick(context.Background())
	if gw.calls != 2 {
		t.Fatalf("calls=%d", gw.calls)
	}
}

func TestWaitlistNoticeDropped(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_ = st.Link(1, 100, "79990000000", "A")
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, reminders.MoscowLocation())
	cur, _ := storage.EncodeCursor("2026-06-23 10:00:00.000", 0)
	_ = st.SetKV(storage.BookingNoticeCursorKey, cur)
	row := consentRow(11, 100)
	row.IsWaitlist = true
	row.Service = "Прием терапевта"
	gw := &fakeGW{pages: []gateway.NewBookingsPage{{
		Items:      []gateway.NewBooking{row},
		NextCursor: &gateway.BookingCursor{CreatedAt: row.CreatedAt, PlanningID: row.PlanningID},
	}}}
	send := &fakeSend{}
	r := &Runner{
		Gateway: gw, Storage: st, Sender: send,
		LinkAllow: []int64{100}, ReminderAllow: []int64{100},
		Now: func() time.Time { return now },
	}
	r.Tick(context.Background())
	if send.n != 0 {
		t.Fatalf("waitlist notice must not send, n=%d", send.n)
	}
}

func seedPending(t *testing.T, st *storage.Storage, row gateway.NewBooking, now time.Time) {
	t.Helper()
	one := 1
	if row.MessengerNotify == nil {
		row.MessengerNotify = &one
	}
	err := st.EnqueueNoticesAndCursor([]storage.OutboxItem{{
		PlanningID: row.PlanningID, PatientID: row.PatientID, UserID: 1,
		BranchID: row.BranchID, BranchCode: row.BranchCode, DoctorName: row.DoctorName,
		Service: row.Service, DateConsultation: row.DateConsultation,
		MessengerNotify: row.MessengerNotify, IsWaitlist: row.IsWaitlist, ApptVersion: row.ApptVersion,
	}}, `{"created_at":"2026-06-23 10:00:00.000","planning_id":0}`, now)
	if err != nil {
		t.Fatal(err)
	}
}

func TestOutboxRecheckDrops(t *testing.T) {
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, reminders.MoscowLocation())
	cases := []struct {
		name   string
		mutate func(*storage.Storage, *fakeGW, gateway.NewBooking)
		reason string
	}{
		{"unlinked", func(st *storage.Storage, gw *fakeGW, row gateway.NewBooking) {
			_ = st.Unlink(1)
			gw.live = map[int64]gateway.BookingSnapshot{row.PlanningID: {NewBooking: row}}
		}, "unlinked"},
		{"not_allowed", func(st *storage.Storage, gw *fakeGW, row gateway.NewBooking) {
			gw.live = map[int64]gateway.BookingSnapshot{row.PlanningID: {NewBooking: row}}
		}, "not_allowed"},
		{"consent", func(st *storage.Storage, gw *fakeGW, row gateway.NewBooking) {
			zero := 0
			row.MessengerNotify = &zero
			gw.live = map[int64]gateway.BookingSnapshot{row.PlanningID: {NewBooking: row}}
		}, "consent"},
		{"cancelled", func(st *storage.Storage, gw *fakeGW, row gateway.NewBooking) {
			snap := gateway.BookingSnapshot{NewBooking: row, Cancelled: true}
			gw.live = map[int64]gateway.BookingSnapshot{row.PlanningID: snap}
		}, "cancelled"},
		{"changed", func(st *storage.Storage, gw *fakeGW, row gateway.NewBooking) {
			row.ApptVersion = "ver-b"
			gw.live = map[int64]gateway.BookingSnapshot{row.PlanningID: {NewBooking: row}}
		}, "changed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, err := storage.New(t.TempDir() + "/b.db")
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			_ = st.Link(1, 100, "79990000000", "A")
			row := consentRow(40, 100)
			seedPending(t, st, row, now)
			gw := &fakeGW{}
			tc.mutate(st, gw, row)
			send := &fakeSend{}
			r := &Runner{
				Gateway: gw, Storage: st, Sender: send,
				LinkAllow: []int64{100}, ReminderAllow: []int64{100},
				Now: func() time.Time { return now },
			}
			if tc.name == "not_allowed" {
				r.ReminderAllow = []int64{999}
			}
			r.Tick(context.Background())
			if send.n != 0 {
				t.Fatalf("sent=%d", send.n)
			}
			status, reason, _, err := st.OutboxLookup(40)
			if err != nil || status != "dropped" || reason != tc.reason {
				t.Fatalf("status=%s reason=%s err=%v", status, reason, err)
			}
		})
	}
}

func TestOutboxTooLateDropped(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_ = st.Link(1, 100, "79990000000", "A")
	now := time.Date(2026, 6, 26, 18, 0, 0, 0, reminders.MoscowLocation())
	row := consentRow(50, 100)
	row.DateConsultation = "2026-06-26 19:30"
	seedPending(t, st, row, now)
	gw := &fakeGW{live: map[int64]gateway.BookingSnapshot{50: {NewBooking: row}}}
	send := &fakeSend{}
	r := &Runner{Gateway: gw, Storage: st, Sender: send, LinkAllow: []int64{100}, ReminderAllow: []int64{100}, Now: func() time.Time { return now }}
	r.Tick(context.Background())
	if send.n != 0 {
		t.Fatal("must drop too_late")
	}
	status, reason, _, _ := st.OutboxLookup(50)
	if status != "dropped" || reason != "too_late" {
		t.Fatalf("%s %s", status, reason)
	}
}

func TestOutboxEightFailuresDropped(t *testing.T) {
	st, err := storage.New(t.TempDir() + "/b.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_ = st.Link(1, 100, "79990000000", "A")
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, reminders.MoscowLocation())
	row := consentRow(60, 100)
	seedPending(t, st, row, now)
	gw := &fakeGW{live: map[int64]gateway.BookingSnapshot{60: {NewBooking: row}}}
	send := &fakeSend{err: errors.New("max down")}
	r := &Runner{Gateway: gw, Storage: st, Sender: send, LinkAllow: []int64{100}, ReminderAllow: []int64{100}, Now: func() time.Time { return now }}
	for i := 0; i < 8; i++ {
		r.Now = func() time.Time { return now.Add(time.Duration(i) * time.Hour) }
		r.Tick(context.Background())
	}
	status, reason, attempts, err := st.OutboxLookup(60)
	if err != nil || status != "dropped" || attempts != 8 || reason != "attempts" {
		t.Fatalf("status=%s attempts=%d reason=%s err=%v", status, attempts, reason, err)
	}
	if send.tries != 8 {
		t.Fatalf("http tries=%d want 8 (no ninth)", send.tries)
	}
	if send.n != 0 {
		t.Fatal("no successful sends")
	}
}
