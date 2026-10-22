package storage

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

const BookingNoticeCursorKey = "booking_notice_cursor"

type OutboxItem struct {
	PlanningID       int64
	PatientID        int64
	UserID           int64
	BranchID         int
	BranchCode       string
	DoctorName       string
	Service          string
	DateConsultation string
	MessengerNotify  *int
	NoSMSNotify      bool
	IsWaitlist       bool
	ApptVersion      string
	Attempts         int
	Status           string
	DropReason       string
	CreatedAt        time.Time
	NextTryAt        time.Time
}

func (s *Storage) EnqueueNoticesAndCursor(items []OutboxItem, cursorJSON string, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, it := range items {
		var notify any
		if it.MessengerNotify != nil {
			notify = *it.MessengerNotify
		}
		noSMS := 0
		if it.NoSMSNotify {
			noSMS = 1
		}
		waitlist := 0
		if it.IsWaitlist {
			waitlist = 1
		}
		_, err := tx.Exec(`
INSERT INTO notice_outbox (
  planning_id, patient_id, user_id, branch_id, branch_code, doctor_name, service,
  date_consultation, messenger_notify, no_sms_notify, is_waitlist, appt_version, created_at, attempts, next_try_at, status
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, 'pending')
ON CONFLICT(planning_id) DO NOTHING`,
			it.PlanningID, it.PatientID, it.UserID, it.BranchID, it.BranchCode, it.DoctorName, it.Service,
			it.DateConsultation, notify, noSMS, waitlist, it.ApptVersion, now.UTC().Format("2006-01-02 15:04:05"),
			now.UTC().Format("2006-01-02 15:04:05"),
		)
		if err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`
INSERT INTO kv (k, v) VALUES (?, ?)
ON CONFLICT(k) DO UPDATE SET v = excluded.v`, BookingNoticeCursorKey, cursorJSON); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Storage) DueOutbox(now time.Time, limit int) ([]OutboxItem, error) {
	rows, err := s.db.Query(`
SELECT planning_id, patient_id, user_id, branch_id, branch_code, doctor_name, service,
       date_consultation, messenger_notify, no_sms_notify, is_waitlist, appt_version, attempts, status, created_at, next_try_at
FROM notice_outbox
WHERE status = 'pending' AND next_try_at <= ?
ORDER BY next_try_at, planning_id
LIMIT ?`, now.UTC().Format("2006-01-02 15:04:05"), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxItem
	for rows.Next() {
		var it OutboxItem
		var notify sql.NullInt64
		var noSMS, waitlist int
		var createdAt, nextTry string
		if err := rows.Scan(&it.PlanningID, &it.PatientID, &it.UserID, &it.BranchID, &it.BranchCode, &it.DoctorName, &it.Service,
			&it.DateConsultation, &notify, &noSMS, &waitlist, &it.ApptVersion, &it.Attempts, &it.Status, &createdAt, &nextTry); err != nil {
			return nil, err
		}
		if notify.Valid {
			v := int(notify.Int64)
			it.MessengerNotify = &v
		}
		it.NoSMSNotify = noSMS != 0
		it.IsWaitlist = waitlist != 0
		it.CreatedAt = parseOutboxTime(createdAt)
		it.NextTryAt = parseOutboxTime(nextTry)
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Storage) MarkOutboxSent(planningID int64) error {
	_, err := s.db.Exec(`UPDATE notice_outbox SET status='sent' WHERE planning_id=?`, planningID)
	return err
}

func (s *Storage) MarkOutboxRetry(planningID int64, attempts int, next time.Time) error {
	_, err := s.db.Exec(`UPDATE notice_outbox SET attempts=?, next_try_at=?, status='pending' WHERE planning_id=?`,
		attempts, next.UTC().Format("2006-01-02 15:04:05"), planningID)
	return err
}

func (s *Storage) MarkOutboxDropped(planningID int64, reason string) error {
	return s.MarkOutboxDroppedAttempts(planningID, -1, reason)
}

func (s *Storage) MarkOutboxDroppedAttempts(planningID int64, attempts int, reason string) error {
	if attempts < 0 {
		_, err := s.db.Exec(`UPDATE notice_outbox SET status='dropped', drop_reason=? WHERE planning_id=?`, reason, planningID)
		return err
	}
	_, err := s.db.Exec(`UPDATE notice_outbox SET status='dropped', drop_reason=?, attempts=? WHERE planning_id=?`, reason, attempts, planningID)
	return err
}

func parseOutboxTime(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (s *Storage) OutboxLookup(planningID int64) (status, reason string, attempts int, err error) {
	err = s.db.QueryRow(`SELECT status, drop_reason, attempts FROM notice_outbox WHERE planning_id=?`, planningID).
		Scan(&status, &reason, &attempts)
	return
}

func EncodeCursor(createdAt string, planningID int64) (string, error) {
	b, err := json.Marshal(map[string]any{"created_at": createdAt, "planning_id": planningID})
	return string(b), err
}
