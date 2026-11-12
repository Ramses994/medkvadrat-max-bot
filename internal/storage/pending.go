package storage

import (
	"database/sql"
	"strconv"
	"strings"
	"time"
)

const pendingChoiceTTL = 15 * time.Minute

// SavePendingChoice stores the ordered patient ids the user was shown.
func (s *Storage) SavePendingChoice(userID int64, phone string, patientIDs []int64, now time.Time) error {
	_, err := s.db.Exec(`
		INSERT INTO pending_choices (user_id, phone, patient_ids, created_at, step, selected_id)
		VALUES (?, ?, ?, ?, 'choose', 0)
		ON CONFLICT(user_id) DO UPDATE SET
			phone = excluded.phone,
			patient_ids = excluded.patient_ids,
			created_at = excluded.created_at,
			step = 'choose',
			selected_id = 0`,
		userID, phone, formatPatientIDs(patientIDs), now.UTC().Format(time.RFC3339))
	return err
}

// SaveBirthStep moves the pending choice to the birth-date step for patientID.
// The full patient_ids list is kept so the user can switch rows or go «Назад».
func (s *Storage) SaveBirthStep(userID int64, phone string, patientID int64, now time.Time) error {
	res, err := s.db.Exec(`
		UPDATE pending_choices SET
			phone = ?,
			created_at = ?,
			step = 'birth',
			selected_id = ?
		WHERE user_id = ?`,
		phone, now.UTC().Format(time.RFC3339), patientID, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		_, err = s.db.Exec(`
			INSERT INTO pending_choices (user_id, phone, patient_ids, created_at, step, selected_id)
			VALUES (?, ?, ?, ?, 'birth', ?)`,
			userID, phone, strconv.FormatInt(patientID, 10), now.UTC().Format(time.RFC3339), patientID)
		return err
	}
	return nil
}

// PendingChoice returns the saved list when it is younger than 15 minutes.
func (s *Storage) PendingChoice(userID int64, now time.Time) (phone string, patientIDs []int64, step string, selected int64, ok bool, err error) {
	var ids, created string
	err = s.db.QueryRow(`
		SELECT phone, patient_ids, created_at, step, selected_id FROM pending_choices WHERE user_id = ?`, userID).
		Scan(&phone, &ids, &created, &step, &selected)
	if err == sql.ErrNoRows {
		return "", nil, "", 0, false, nil
	}
	if err != nil {
		return "", nil, "", 0, false, err
	}
	at, perr := time.Parse(time.RFC3339, created)
	if perr != nil || now.Sub(at) > pendingChoiceTTL {
		_ = s.DeletePendingChoice(userID)
		return "", nil, "", 0, false, nil
	}
	return phone, parsePatientIDs(ids), step, selected, true, nil
}

// DeletePendingChoice removes the saved list after a bind or a number change.
func (s *Storage) DeletePendingChoice(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM pending_choices WHERE user_id = ?`, userID)
	return err
}

func formatPatientIDs(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

func parsePatientIDs(raw string) []int64 {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		out = append(out, id)
	}
	return out
}
