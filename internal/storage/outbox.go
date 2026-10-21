package storage

import (
	"database/sql"
	"encoding/json"
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
	Attempts         int
	Status           string
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
		_, err := tx.Exec(`
INSERT INTO notice_outbox (
  planning_id, patient_id, user_id, branch_id, branch_code, doctor_name, service,
  date_consultation, messenger_notify, no_sms_notify, attempts, next_try_at, status
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, 'pending')
ON CONFLICT(planning_id) DO NOTHING`,
			it.PlanningID, it.PatientID, it.UserID, it.BranchID, it.BranchCode, it.DoctorName, it.Service,
			it.DateConsultation, notify, noSMS, now.UTC().Format("2006-01-02 15:04:05"),
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
       date_consultation, messenger_notify, no_sms_notify, attempts, status
FROM notice_outbox
WHERE status = 'pending' AND next_try_at <= ?
ORDER BY planning_id
LIMIT ?`, now.UTC().Format("2006-01-02 15:04:05"), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxItem
	for rows.Next() {
		var it OutboxItem
		var notify sql.NullInt64
		var noSMS int
		if err := rows.Scan(&it.PlanningID, &it.PatientID, &it.UserID, &it.BranchID, &it.BranchCode, &it.DoctorName, &it.Service,
			&it.DateConsultation, &notify, &noSMS, &it.Attempts, &it.Status); err != nil {
			return nil, err
		}
		if notify.Valid {
			v := int(notify.Int64)
			it.MessengerNotify = &v
		}
		it.NoSMSNotify = noSMS != 0
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

func (s *Storage) MarkOutboxDropped(planningID int64) error {
	_, err := s.db.Exec(`UPDATE notice_outbox SET status='dropped' WHERE planning_id=?`, planningID)
	return err
}

func EncodeCursor(createdAt string, planningID int64) (string, error) {
	b, err := json.Marshal(map[string]any{"created_at": createdAt, "planning_id": planningID})
	return string(b), err
}
