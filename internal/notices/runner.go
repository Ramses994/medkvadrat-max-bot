package notices

import (
	"context"
	"log"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/reminders"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/storage"
)

const (
	tickEvery     = 60 * time.Second
	minLead       = 3 * time.Hour
	bookedKind    = "booked"
	sinceLayout   = time.RFC3339
	apptLayout    = "2006-01-02 15:04"
)

type Gateway interface {
	NewBookings(ctx context.Context, since time.Time, patientIDs []int64) ([]gateway.NewBooking, error)
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
	sinceRaw, err := r.Storage.GetKV(storage.BookingNoticeSinceKey)
	if err != nil {
		log.Printf("booking notice: read since: %v", err)
		return
	}
	if sinceRaw == "" {
		if err := r.Storage.SetKV(storage.BookingNoticeSinceKey, now.Format(sinceLayout)); err != nil {
			log.Printf("booking notice: init since: %v", err)
		}
		return
	}
	since, err := time.Parse(sinceLayout, sinceRaw)
	if err != nil {
		log.Printf("booking notice: parse since: %v", err)
		return
	}

	linked, err := r.Storage.DistinctPatientIDs()
	if err != nil {
		log.Printf("booking notice: DistinctPatientIDs: %v", err)
		return
	}
	targets := reminders.BookingAudience(linked, r.LinkAllow, r.ReminderAllow)
	if len(targets) == 0 {
		return
	}

	rows, err := r.Gateway.NewBookings(ctx, since, targets)
	if err != nil {
		log.Printf("booking notice: gateway: %v", err)
		return
	}

	loc := reminders.MoscowLocation()
	var maxCreated time.Time
	for i := range rows {
		row := &rows[i]
		if row.CreatedAt.After(maxCreated) {
			maxCreated = row.CreatedAt
		}
		r.sendOne(ctx, row, now, loc)
	}
	if !maxCreated.IsZero() {
		if err := r.Storage.SetKV(storage.BookingNoticeSinceKey, maxCreated.Format(sinceLayout)); err != nil {
			log.Printf("booking notice: save since: %v", err)
		}
	}
}

func (r *Runner) sendOne(ctx context.Context, row *gateway.NewBooking, now time.Time, loc *time.Location) {
	appt, err := time.ParseInLocation(apptLayout, row.DateConsultation, loc)
	if err != nil {
		log.Printf("booking notice: parse time planning=%d: %v", row.PlanningID, err)
		return
	}
	if appt.Sub(now) < minLead {
		return
	}
	sent, err := r.Storage.WasReminderSent(row.PlanningID, bookedKind)
	if err != nil || sent {
		return
	}
	users, err := r.Storage.UsersByPatientID(row.PatientID)
	if err != nil || len(users) == 0 {
		return
	}
	text := reminders.FormatBookingNotice(row.BranchID, row.BranchCode, row.DoctorName, row.Specialty, appt)
	if err := r.Sender.SendText(ctx, users[0].UserID, text); err != nil {
		log.Printf("booking notice: send planning=%d patient=%d: %v", row.PlanningID, row.PatientID, err)
		return
	}
	if err := r.Storage.MarkReminderSent(row.PlanningID, bookedKind); err != nil {
		log.Printf("booking notice: mark planning=%d: %v", row.PlanningID, err)
		return
	}
	log.Printf("booking notice sent planning=%d patient=%d", row.PlanningID, row.PatientID)
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
