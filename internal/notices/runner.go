package notices

import (
	"context"
	"encoding/json"
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
		return
	}
	targets := reminders.BookingAudience(linked, r.LinkAllow, r.ReminderAllow)
	if len(targets) == 0 {
		return
	}
	if len(targets) > maxAudience {
		log.Printf("WARNING: booking notice audience %d exceeds 500 — cycle skipped", len(targets))
		return
	}

	raw, err := r.Storage.GetKV(storage.BookingNoticeCursorKey)
	if err != nil {
		log.Printf("booking notice: read cursor: %v", err)
		return
	}
	if raw == "" {
		cur, _ := storage.EncodeCursor(now.Format("2006-01-02 15:04:05.000"), 0)
		if err := r.Storage.SetKV(storage.BookingNoticeCursorKey, cur); err != nil {
			log.Printf("booking notice: init cursor: %v", err)
		}
		return
	}

	cursor := parseCursor(raw)
	for {
		page, err := r.Gateway.NewBookings(ctx, targets, cursor, pageLimit)
		if err != nil {
			log.Printf("booking notice: gateway: %v", err)
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
			})
		}
		next := raw
		if page.NextCursor != nil {
			next, _ = storage.EncodeCursor(page.NextCursor.CreatedAt, page.NextCursor.PlanningID)
			cursor = page.NextCursor
		}
		if err := r.Storage.EnqueueNoticesAndCursor(items, next, now); err != nil {
			log.Printf("booking notice: outbox: %v", err)
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
	if !reminders.CanNotify(it.MessengerNotify, it.NoSMSNotify, it.Service, false) {
		log.Printf("skip planning=%d reason=consent", it.PlanningID)
		_ = r.Storage.MarkOutboxDropped(it.PlanningID)
		return
	}
	appt, err := time.ParseInLocation(apptLayout, it.DateConsultation, loc)
	if err != nil {
		log.Printf("booking notice: parse time planning=%d: %v", it.PlanningID, err)
		return
	}
	if appt.Sub(now) < minLead || it.Attempts >= maxAttempts {
		log.Printf("booking notice dropped planning=%d attempts=%d", it.PlanningID, it.Attempts)
		_ = r.Storage.MarkOutboxDropped(it.PlanningID)
		return
	}
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
