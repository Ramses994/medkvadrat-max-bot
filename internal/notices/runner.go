package notices

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/reminders"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/storage"
)

const (
	tickEvery   = 60 * time.Second
	minLead     = 3 * time.Hour
	pageLimit   = 200
	maxAudience = 500
	maxAttempts = 8
	apptLayout  = "2006-01-02 15:04"
)

type Gateway interface {
	NewBookings(ctx context.Context, patientIDs []int64, cursor *gateway.BookingCursor, limit int) (gateway.NewBookingsPage, error)
	BookingByPlanningID(ctx context.Context, planningID int64) (gateway.BookingSnapshot, error)
}

type Sender interface {
	SendText(ctx context.Context, userID int64, text string) error
}

type Runner struct {
	Gateway       Gateway
	Storage       *storage.Storage
	Sender        Sender
	LinkAllow     []int64
	ReminderAllow []int64
	Now           func() time.Time
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now().In(reminders.MoscowLocation())
}

func (r *Runner) Tick(ctx context.Context) {
	now := r.now()
	linked, err := r.Storage.DistinctPatientIDs()
	if err != nil {
		log.Printf("booking notice: DistinctPatientIDs: %v", err)
		r.flushOutbox(ctx, now)
		return
	}
	targets := reminders.BookingAudience(linked, r.LinkAllow, r.ReminderAllow)
	if len(targets) == 0 {
		r.flushOutbox(ctx, now)
		return
	}
	if len(targets) > maxAudience {
		log.Printf("WARNING: booking notice audience %d exceeds 500 — cycle skipped", len(targets))
		r.flushOutbox(ctx, now)
		return
	}

	raw, err := r.Storage.GetKV(storage.BookingNoticeCursorKey)
	if err != nil {
		log.Printf("booking notice: read cursor: %v", err)
		r.flushOutbox(ctx, now)
		return
	}
	if raw == "" {
		cur, _ := storage.EncodeCursor(now.Format("2006-01-02 15:04:05.000"), 0)
		if err := r.Storage.SetKV(storage.BookingNoticeCursorKey, cur); err != nil {
			log.Printf("booking notice: init cursor: %v", err)
		}
		r.flushOutbox(ctx, now)
		return
	}

	cursor := parseCursor(raw)
	for {
		page, err := r.Gateway.NewBookings(ctx, targets, cursor, pageLimit)
		if err != nil {
			log.Printf("booking notice: gateway: %v", err)
			r.flushOutbox(ctx, now)
			return
		}
		var items []storage.OutboxItem
		for _, row := range page.Items {
			users, err := r.Storage.UsersByPatientID(row.PatientID)
			if err != nil || len(users) == 0 {
				continue
			}
			items = append(items, storage.OutboxItem{
				PlanningID:       row.PlanningID,
				PatientID:        row.PatientID,
				UserID:           users[0].UserID,
				BranchID:         row.BranchID,
				BranchCode:       row.BranchCode,
				DoctorName:       row.DoctorName,
				Service:          row.Service,
				DateConsultation: row.DateConsultation,
				MessengerNotify:  row.MessengerNotify,
				NoSMSNotify:      row.NoSMSNotify,
				IsWaitlist:       row.IsWaitlist,
				ApptVersion:      row.ApptVersion,
			})
		}
		next := raw
		if page.NextCursor != nil {
			next, _ = storage.EncodeCursor(page.NextCursor.CreatedAt, page.NextCursor.PlanningID)
			cursor = page.NextCursor
		}
		if err := r.Storage.EnqueueNoticesAndCursor(items, next, now); err != nil {
			log.Printf("booking notice: outbox: %v", err)
			r.flushOutbox(ctx, now)
			return
		}
		raw = next
		if !page.HasMore {
			break
		}
	}

	r.flushOutbox(ctx, now)
}

func (r *Runner) flushOutbox(ctx context.Context, now time.Time) {
	items, err := r.Storage.DueOutbox(now, 100)
	if err != nil {
		log.Printf("booking notice: due outbox: %v", err)
		return
	}
	loc := reminders.MoscowLocation()
	for i := range items {
		r.sendOutbox(ctx, &items[i], now, loc)
	}
}

func (r *Runner) sendOutbox(ctx context.Context, it *storage.OutboxItem, now time.Time, loc *time.Location) {
	if it.Attempts >= maxAttempts {
		log.Printf("booking notice dropped planning=%d reason=attempts", it.PlanningID)
		_ = r.Storage.MarkOutboxDropped(it.PlanningID, "attempts")
		return
	}
	reason, hold := r.outboxRecheck(ctx, it, now, loc)
	if hold {
		return
	}
	if reason != "" {
		log.Printf("booking notice dropped planning=%d reason=%s", it.PlanningID, reason)
		_ = r.Storage.MarkOutboxDropped(it.PlanningID, reason)
		return
	}
	appt, _ := time.ParseInLocation(apptLayout, it.DateConsultation, loc)
	text := reminders.FormatBookingNotice(it.BranchID, it.BranchCode, it.DoctorName, it.Service, appt)
	if err := r.Sender.SendText(ctx, it.UserID, text); err != nil {
		next := now.Add(backoff(it.Attempts + 1))
		log.Printf("booking notice: send planning=%d patient=%d: %v", it.PlanningID, it.PatientID, err)
		_ = r.Storage.MarkOutboxRetry(it.PlanningID, it.Attempts+1, next)
		return
	}
	if err := r.Storage.MarkOutboxSent(it.PlanningID); err != nil {
		log.Printf("booking notice: mark planning=%d: %v", it.PlanningID, err)
		return
	}
	log.Printf("booking notice sent planning=%d patient=%d", it.PlanningID, it.PatientID)
}

func (r *Runner) outboxRecheck(ctx context.Context, it *storage.OutboxItem, now time.Time, loc *time.Location) (reason string, hold bool) {
	users, err := r.Storage.UsersByPatientID(it.PatientID)
	if err != nil || len(users) == 0 || users[0].UserID != it.UserID {
		return "unlinked", false
	}
	if len(reminders.BookingAudience([]int64{it.PatientID}, r.LinkAllow, r.ReminderAllow)) == 0 {
		return "not_allowed", false
	}
	live, err := r.Gateway.BookingByPlanningID(ctx, it.PlanningID)
	if err != nil {
		var ge *gateway.Error
		if errors.As(err, &ge) && ge.Status == 404 {
			return "cancelled", false
		}
		log.Printf("booking notice: refresh planning=%d: %v", it.PlanningID, err)
		return "", true
	}
	if live.Cancelled || live.Status != 0 {
		return "cancelled", false
	}
	if live.PatientID != it.PatientID {
		return "changed", false
	}
	if live.ApptVersion != it.ApptVersion {
		return "changed", false
	}
	if !reminders.CanNotify(live.MessengerNotify, live.NoSMSNotify, live.IsWaitlist, false) {
		return "consent", false
	}
	appt, err := time.ParseInLocation(apptLayout, live.DateConsultation, loc)
	if err != nil {
		return "changed", false
	}
	if appt.Sub(now) < minLead {
		return "too_late", false
	}
	it.BranchID = live.BranchID
	it.BranchCode = live.BranchCode
	it.DoctorName = live.DoctorName
	it.Service = live.Service
	it.DateConsultation = live.DateConsultation
	return "", false
}

func backoff(attempts int) time.Duration {
	switch attempts {
	case 1:
		return time.Minute
	case 2:
		return 5 * time.Minute
	case 3:
		return 15 * time.Minute
	default:
		return time.Hour
	}
}

func parseCursor(raw string) *gateway.BookingCursor {
	var c gateway.BookingCursor
	if err := json.Unmarshal([]byte(raw), &c); err != nil || c.CreatedAt == "" {
		return nil
	}
	return &c
}

func Start(ctx context.Context, runner *Runner) {
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()
	runner.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runner.Tick(ctx)
		}
	}
}
