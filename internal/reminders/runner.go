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

// Messenger sends a reminder to a MAX user (cold delivery by user_id).
type Messenger interface {
	SendToUser(ctx context.Context, userID int64, text string, motconsuID int64) error
}

// DueGateway fetches upcoming appointments from api-gateway.
type DueGateway interface {
	DueReminders(ctx context.Context, from, to time.Time) ([]gateway.DueReminder, error)
}

type Runner struct {
	Gateway   DueGateway
	Storage   *storage.Storage
	Messenger Messenger
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

	appointments, err := r.Gateway.DueReminders(ctx, from, to)
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
			log.Printf("reminders: panic processing motconsu_id=%d: %v", appt.MotconsuID, rec)
		}
	}()

	apptTime, err := time.ParseInLocation(apptTimeLayout, appt.DateConsultation, loc)
	if err != nil {
		log.Printf("reminders: parse date_consultation %q motconsu_id=%d: %v", appt.DateConsultation, appt.MotconsuID, err)
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

	text := FormatMessage(appt.DepartmentLabel, appt.DoctorName, apptTime)

	for _, kind := range kinds {
		sent, err := r.Storage.WasReminderSent(appt.MotconsuID, string(kind))
		if err != nil {
			log.Printf("reminders: WasReminderSent(%d,%s): %v", appt.MotconsuID, kind, err)
			continue
		}
		if sent {
			continue
		}

		// MVP: mark sent after the first successful delivery to any linked MAX user,
		// so we do not spam the same kind to every family member on the next tick.
		var delivered bool
		for _, u := range users {
			if err := r.Messenger.SendToUser(ctx, u.UserID, text, appt.MotconsuID); err != nil {
				log.Printf("reminders: send user=%d motconsu=%d kind=%s: %v", u.UserID, appt.MotconsuID, kind, err)
				continue
			}
			delivered = true
			break
		}
		if !delivered {
			continue
		}
		if err := r.Storage.MarkReminderSent(appt.MotconsuID, string(kind)); err != nil {
			log.Printf("reminders: MarkReminderSent(%d,%s): %v", appt.MotconsuID, kind, err)
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

// maxMessenger adapts maxclient.Client to Messenger (private chat: user_id == chat_id).
type maxMessenger struct {
	c *maxclient.Client
}

func (m maxMessenger) SendToUser(ctx context.Context, userID int64, text string, motconsuID int64) error {
	rows := ConfirmationKeyboard(motconsuID)
	return m.c.SendMessageWithKeyboard(ctx, userID, true, text, rows)
}

func NewMaxMessenger(c *maxclient.Client) Messenger {
	return maxMessenger{c: c}
}
