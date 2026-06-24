package reminders

import (
	"context"
	"log"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/storage"
)

const apptTimeLayout = "2006-01-02 15:04"

const d3Lead = 72 * time.Hour

// Messenger sends a reminder to a MAX user (private dialog).
type Messenger interface {
	SendToUser(ctx context.Context, userID int64, text string) error
}

// DueGateway fetches upcoming appointments from api-gateway.
type DueGateway interface {
	DueReminders(ctx context.Context, from, to time.Time, patientIDs []int64) ([]gateway.DueReminder, error)
}

type Runner struct {
	Gateway   DueGateway
	Storage   *storage.Storage
	Messenger Messenger
	Allowlist []int64
	Now       func() time.Time
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now().In(MoscowLocation())
}

// Tick fetches due appointments and sends reminders for this cycle.
func (r *Runner) Tick(ctx context.Context) {
	now := r.now()
	from := now
	to := now.Add(d3Lead + MaxGrace)

	linked, err := r.Storage.DistinctPatientIDs()
	if err != nil {
		log.Printf("reminders: DistinctPatientIDs: %v", err)
		return
	}
	targetPatients := TargetPatients(linked, r.Allowlist)
	if len(targetPatients) == 0 {
		return
	}

	appointments, err := r.Gateway.DueReminders(ctx, from, to, targetPatients)
	if err != nil {
		log.Printf("reminders: gateway.DueReminders: %v", err)
		return
	}

	loc := MoscowLocation()
	for i := range appointments {
		r.processOne(ctx, &appointments[i], now, loc)
	}
}

func (r *Runner) processOne(ctx context.Context, appt *gateway.DueReminder, now time.Time, loc *time.Location) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("reminders: panic processing planning_id=%d: %v", appt.PlanningID, rec)
		}
	}()

	apptTime, err := time.ParseInLocation(apptTimeLayout, appt.DateConsultation, loc)
	if err != nil {
		log.Printf("reminders: parse date_consultation %q planning_id=%d: %v", appt.DateConsultation, appt.PlanningID, err)
		return
	}

	kinds := DueKinds(apptTime, now)
	if len(kinds) == 0 {
		return
	}

	users, err := r.Storage.UsersByPatientID(appt.PatientID)
	if err != nil {
		log.Printf("reminders: UsersByPatientID(%d): %v", appt.PatientID, err)
		return
	}
	if len(users) == 0 {
		return
	}

	text := FormatMessage(appt.BranchID, appt.BranchCode, appt.DoctorName, apptTime)

	for _, kind := range kinds {
		sent, err := r.Storage.WasReminderSent(appt.PlanningID, string(kind))
		if err != nil {
			log.Printf("reminders: WasReminderSent(%d,%s): %v", appt.PlanningID, kind, err)
			continue
		}
		if sent {
			continue
		}

		var delivered bool
		for _, u := range users {
			if err := r.Messenger.SendToUser(ctx, u.UserID, text); err != nil {
				log.Printf("reminders: send user=%d planning=%d kind=%s: %v", u.UserID, appt.PlanningID, kind, err)
				continue
			}
			delivered = true
			break
		}
		if !delivered {
			continue
		}
		if err := r.Storage.MarkReminderSent(appt.PlanningID, string(kind)); err != nil {
			log.Printf("reminders: MarkReminderSent(%d,%s): %v", appt.PlanningID, kind, err)
		}
	}
}

// Start runs the reminder ticker until ctx is cancelled.
func Start(ctx context.Context, tick time.Duration, runner *Runner) {
	if tick <= 0 {
		tick = 5 * time.Minute
	}
	ticker := time.NewTicker(tick)
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

type maxMessenger struct {
	c *maxclient.Client
}

func (m maxMessenger) SendToUser(ctx context.Context, userID int64, text string) error {
	return m.c.SendMessage(ctx, userID, text)
}

func NewMaxMessenger(c *maxclient.Client) Messenger {
	return maxMessenger{c: c}
}
